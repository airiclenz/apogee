---
title: ADR 0020 decision
---

**1. The facility is a restricted, Low-integrity primary token handed to
`SysProcAttr.Token`.**

The **fence is the mandatory integrity check**. The child runs at **Low**; every object that
carries no explicit label is implicitly **Medium with `NO_WRITE_UP`**, so every write outside
the box is denied by the kernel before the DACL is even consulted. The denial covers the whole
descendant tree for free — a child created by the confined process inherits its token — which is
the Windows equivalent of "the domain survives `execve`".

`CreateRestrictedToken(…, DISABLE_MAX_PRIVILEGE, …)` is **defence in depth, not the fence**: it
strips the privileges (`SeBackup`/`SeRestore`/`SeTakeOwnership`/`SeDebug` where present) with
which a child could otherwise walk around a label. **No restricting SIDs and no deny-only SIDs
in v1** — restricting SIDs force a double access check that breaks ordinary programs (they must
still be able to read system DLLs), and they buy nothing the integrity level does not already
give under ADR 0012's threat model.

**There is no helper process, no argv sentinel, and no argv rewrite.** `Confine` sets
`cmd.SysProcAttr.Token` (and nothing else on the cmd) and returns; `cmd.Path` and `cmd.Args` are
untouched. Linux needs its 42-line helper because the only CGO-free way to run code between
`fork` and `execve` is to *be* a separate process that restricts **itself** and then
`syscall.Exec`s in place (`cmd/apogee/confined_exec_linux.go:37`). Windows has no
"restrict-myself-into-a-box" API to mirror: the restriction is expressed as a token *handed to
the process-creation call*, which is precisely what `SysProcAttr` exposes. So `main.go`'s
`maybeDispatchConfinedExec` gains **no** Windows arm, and `confined_exec_windows.go` is **not
written**.

The token is minted **once, at construction, and reused for every confined command**. It carries
no path policy, so it is box-independent; one process-lifetime handle also answers the "who
closes it, given `Confine` returns before `Start`" question that prepare-in-place would otherwise
raise. Minting is simultaneously the capability probe (§3).

**2. The box is expressed on the DISK: `WorkspaceRoot ∪ WritablePaths` are labelled Low for the
run and the label is reverted on teardown.**

Because the token cannot carry the box, the only place a `ConfinementBox`'s *writable* half can
be expressed is on the objects themselves. Each root gets `S:(ML;;NW;;;LW)` — a mandatory
label ACE, `NO_WRITE_UP`, Low, **no inheritance flags** — via
`SecurityDescriptorFromString` → `SACL()` → `SetNamedSecurityInfo(…, LABEL_SECURITY_INFORMATION,
…)`. *(Amended 2026-09-27: the write is `SetSecurityInfo` on a handle opened with
`FILE_FLAG_OPEN_REPARSE_POINT` and refused when it is a reparse point — see §6.)*

- **It must recurse over existing contents.** The label is not inheritable, so nothing but the
  walk reaches a file that predates the labelling; such a file is implicitly Medium, and a Low
  child editing an existing source file would be denied — which is the single most common
  thing an agent does. Item 8 walks the roots.
- **Why not inheritable** (amended 2026-08-30; the label was `OICI` until then). The premise
  that inheritance "covers newly created objects only" was wrong for `SetNamedSecurityInfo`,
  which propagates an inheritable ACE to every *existing* descendant the moment the root is
  labelled — before the walk's hard-link skip can refuse anything. The first run of the
  Windows-tagged tests (CI job added 2026-08-29) showed a hard-linked file carrying
  `S:AI(ML;ID;NW;;;LW)` at its name *outside* the box: a pnpm store labelled Low for the run.
  Objects the confined child creates are labelled Low by the kernel from its own token, so the
  flags bought nothing there. The accepted cost: an object a *Medium* subject creates inside
  the box mid-run (apogee's own write tool, the user's editor) stays implicitly Medium until
  the next label pass — readable by the child, not editable.
- **Apogee's own writes and the user's editor are unaffected.** Medium subjects writing Low
  objects is a *write-down*, permitted by default, and `NO_READ_UP` is deliberately not set, so
  reads are untouched.
- **This is a side effect on the user's disk that landlock and seatbelt do not have.** It is the
  one place the Windows model differs structurally, and it is recorded here rather than
  discovered by a user.
- **Cost is paid once per box, not per command.** `Confine` memoises the label pass on the box's
  roots; the first confined command of a session pays it, the rest are free.
- **Guardrails.** Item 8 refuses to label — returning `ErrConfinementUnavailable` — a root that
  is a volume root, `%SystemRoot%`, `%ProgramFiles%`/`%ProgramFiles(x86)%`, or the user-profile
  root itself. Labelling those Low would be a catastrophic and near-unrevertable mutation.
- **Restore.** The backend implements `io.Closer`; `cmd/apogee/wire.go` defers it beside the
  existing `defer rungs.Docs.Close()` / `defer mcpClient.Close()` / `defer agent.Close()`.
  `domain.Confiner` **does not grow a method** — it is a public interface (ADR 0010) and must not
  sprout a lifecycle hook for one OS; an optional-interface type assertion is the idiom.
- **Interrupted cleanup.** A **journal** is written under the apogee home *before* any label is
  applied, recording each path and whether it previously carried a label. A later `NewConfiner()`
  finishes an outstanding restore, and `apogee probe host` reports an outstanding journal so the
  state is visible off-session (ADR 0021's host report is exactly the right surface). Those are
  two different constructors, and deliberately so: **the report path builds the backend through
  `NewReportConfiner()`, which skips the recovery pass** — a report that recovered would revert
  and delete the journal before it could name it, so the very line written for an interrupted run
  could never fire, and ADR 0021 §1's read-only pledge would be false besides. Nothing is lost by
  deferring: the journal survives until a real session's constructor finishes the restore. The
  documented manual remedy is `icacls <root> /setintegritylevel (OI)(CI)M /T /C` — an explicit
  Medium label is behaviourally identical to no label at all.
- **Accepted, recorded cost: a Low-labelled directory is writable by every Low-integrity process
  on the machine**, not only by Apogee's child. Under ADR 0012 the fence bounds an autonomous
  small model's *mistakes* and is explicitly not a boundary against a hostile local process (the
  same posture the dangerous-action guard is given). It is nonetheless a reason to revert rather
  than leave labels behind, and a second reason to keep the labelled surface small.
- **Consequence for the box builder (contract §7).** "Include the toolchain's cache/temp dirs"
  stops being an ergonomic nicety on Windows and becomes a hard prerequisite: a Low child with an
  unlabelled `%TEMP%` cannot run `go build` at all. Preferring a **box-local `%TEMP%`** over
  labelling the user's temp tree is the cheaper answer — but that is environment-scoped execution
  (`platform.Shell`, item 6) plus box construction, **not** a `Confine` responsibility.

**3. Capability honesty splits in two on Windows — the one structural difference from
Linux/macOS.**

- **`Capabilities()` probes the FACILITY, once at construction** (contract §5, unchanged in
  shape): at or above the version floor (§5 below) **and** the restricted Low token minted ⇒
  `{FSWrite: true, NetworkEgress: false}`. A mint failure ⇒ `{false, false}`.
- **Construction must not touch the disk.** ADR 0021 §1 makes `apogee probe host` free, offline
  and read-only, item 3 pinned that with a test, and `cmd/apogee/probe.go` constructs a real
  backend to describe the host. Labelling therefore belongs to `Confine` and never to the
  constructor. The **one** thing a constructor does write — finishing an interrupted run's
  restore (§2) — is why the selector has two spellings: `NewConfiner()` for a session, which
  recovers, and `NewReportConfiner()` for the host report, which does not. **No exception to the
  read-only pledge is carved for Windows**, on this or any other surface.
- **A per-run labelling failure returns `ErrConfinementUnavailable` from `Confine`**, which
  contract §4's precomputed fallback demotes to a forced `Gate`. On Linux/macOS that path is
  nearly unreachable (an argv rewrite can only fail on `os.Executable()`); on Windows it is a
  **routine** outcome — a read-only root, a filesystem with no SACL support (FAT32/exFAT, many
  network shares), a root that does not exist, a guardrailed root. Contract §5's "probed once at
  construction" stays true *of the facility*; §2.2's runtime safety net becomes load-bearing
  rather than theoretical.
- **One failure lands in neither place, by construction: `CreateProcessAsUser` refused at
  `cmd.Start()`** (e.g. `ERROR_PRIVILEGE_NOT_HELD`). `Confine` has already returned, so it
  surfaces as the tool's own run error — the command **fails**; it does **not** run unconfined.
  That is fail-closed and acceptable. If it proves common on standard-user hosts, the additive
  fix is a one-shot spawn inside the construction probe; it is named here so item 8 does not
  discover it late.

**4. `NetworkEgress` is reported FALSE, always, and a network-deny box fails closed.**

`ConfinementBox.NetworkAllow` is a per-host *tightening* list
(`internal/domain/confinement.go:65-70`). No token or integrity facility can express per-host
egress, and the Windows facilities that can (WFP filters, firewall rules) are machine-scoped and
admin-requiring — a Phase 5 non-goal. Claiming `true` would be a lie under box semantics, so the
backend reports `{FSWrite: true, NetworkEgress: false}` and is **Auto-eligible** anyway, because
`AutoEligible()` is `FSWrite`-only (ADR 0012 / contract §5) — the same position a 5.13–6.6 Linux
kernel occupies. A box arriving with a **non-empty `NetworkAllow` fails closed**: `Confine`
returns `ErrConfinementUnavailable` rather than running network-open silently, mirroring
`landlock_linux.go`'s `networkDenyDecision` verbatim, for the identical reason — a fence the user
believes is in place must never be a silent no-op.

**5. The floor is Windows 10 1809 / build 17763 / Server 2019.**

It is the oldest branch still under any servicing (LTSC 2019) and sits at or above Go's own
supported-Windows floor, so no supported toolchain realistically targets a host below it. The
floor is **not** driven by an API: MIC, restricted tokens and mandatory-label SACLs have existed
since Vista. It is driven by what this project is willing to claim it has tested and can service.

Detection is `windows.RtlGetNtVersionNumbers()` — the un-shimmed build number; `GetVersionEx`
lies without an application manifest. **Below the floor, `NewConfiner()` returns
`NewDenyConfiner()`** and the existing degradation notice fires unchanged
(`probe.DegradedNotice`, `cmd/apogee/wire.go:178`). No new user-facing surface, no new wording,
no special case: a below-floor Windows host is exactly today's Windows host.

**6. What the backend needs from `platform.Path` (this is item 6's surface).**

- **`Contains(root, target string) bool`** — case-folded on Windows, exact on POSIX;
  separator- and `.`/`..`-normalised; matching on **component boundaries** so `C:\Work2` is not
  inside `C:\Work`. Two callers, both in this backend: collapsing `WorkspaceRoot ∪ WritablePaths`
  to a **minimal set of non-overlapping roots** before labelling (a nested root labelled twice
  would be double-journalled and restored inconsistently), and evaluating the §2 guardrails.
- The fold must be a **pure function taking the fold flag as a parameter**, so Windows semantics
  are table-testable on Linux (the `internal/present` seam pattern the plan mandates).
- Named edges for item 6's tests, beyond the plan's own `C:\Work` vs `c:\work`: 8.3 short names
  (`PROGRA~1`) and the `\\?\` long-path prefix. Both must normalise or be rejected — never
  silently mismatch.
- **A box root that is itself a reparse point (a junction or symlink) is refused** —
  `SetNamedSecurityInfo` follows it, so labelling one would mutate its target; every other root
  is resolved to its final on-disk form before the guardrails run, and trailing dots and spaces
  (which Win32 canonicalization strips) fold off in the component comparison, so `C:\Windows.`
  compares equal to `C:\Windows`.
- **The refusal holds at the write, not only at the check** (amended 2026-09-27, code audit
  2026-09-26). The up-front refusal runs once, before the label pass, so a root swapped for a
  junction after it was vetted would still have been labelled through to the junction's
  target. Every mandatory-label read and write — roots and descendants, in the label pass, the
  clear walk and the revert — therefore opens the object ONCE with
  `FILE_FLAG_OPEN_REPARSE_POINT`, refuses it when the handle's attributes carry
  `FILE_ATTRIBUTE_REPARSE_POINT`, and reads (`GetSecurityInfo`) and writes (`SetSecurityInfo`)
  the label on that handle. A root that became a reparse point after resolution fails the label
  pass with confinement unavailable, exactly as an up-front reparse root does; a descendant
  that did so takes the tolerated-descendant rung; and the revert's judgement reads a reparse
  point as carrying no label of apogee's, so it neither clears nor restores through one. A revert
  write that reaches a reparse point anyway — a journal entry with no recorded identity whose
  verdict was persisted before its path became a link — is refused at the handle and settles
  as a vanished path does (amended 2026-09-27): the object the journal names is no longer at
  that path, so the entry is retired rather than retried, and nothing is written through the
  link.

**7. Probe expectations (the escape battery on Windows).**

`internal/platform/confinetest` is POSIX-shaped today and item 8 must widen it, not assume it:

- **The shell.** The battery drives every write through `sh -c` (`confinetest.go:130`, `:143`,
  `:160`, `:170`), which does not exist on stock Windows. Item 8 adds a `cmd /c` arm — ideally by
  asking `platform.Current().Command(line)` for the argv (item 6) instead of hard-coding a shell,
  with a per-OS write line (`printf x > <p>` vs `echo x> <p>`) and platform quoting. The
  assertions are unchanged: MIC denies the redirect's `CreateFile` with `ERROR_ACCESS_DENIED`,
  `cmd` prints "Access is denied." and exits non-zero, so `assertDenied`'s "non-zero exit AND no
  file" holds as written.
- **The `$HOME/.ssh` row (`:60`) ports as code, not as intent.** `os.UserHomeDir()` already
  resolves `%USERPROFILE%` on Windows, so the target needs no change; what needs changing is the
  *naming*, since `.ssh` is not a meaningful Windows credential path. The row's claim is "a path
  under the user profile, outside the box", and it should say so.
- **Row #6 (exec inheritance) must be ASSERTED, not assumed.** It is Linux-tagged in contract
  §6.2 because it proves a landlock domain survives `execve`. The token backend's equivalent
  claim — a descendant created by the confined child inherits the restricted token — is exactly
  as load-bearing and exactly as unproven until a test says so. It gains a Windows arm.
- **Rows #7/#8 (network) skip**, because `ProbeNetwork` guards on `NetworkEgress`
  (`confinetest.go:96`). The *positive* control #8 therefore goes unproven on Windows; acceptable,
  since nothing is being enforced there.
- **Row #5 (parent unrestricted) is free on Windows**: the restricted token is a *copy*; the
  parent's own token is never modified.
- **Two Windows-only rows are added** (contract §9): the labels are back to their prior state
  after teardown, and a `Confine` of a box with non-empty `NetworkAllow` returns
  `ErrConfinementUnavailable`.
- **A landmine that is not one:** `t.TempDir()` cleanup still works after the harness labels the
  workspace Low, because the test process is Medium and writing down is permitted.

> **Pointers (2026-10-29).** The file:line references above are as of the decision's date. Today
> `probe.DegradedNotice` is called from `cmd/apogee/wire_boot.go` (was `wire.go:178`).
> `ConfinementBox.NetworkAllow` is declared at `internal/domain/confinement.go:157` (was `:65-70`).
> The §2 restore is an optional-interface `Close()` assertion in the composition root's close path,
> `cmd/apogee/wire.go`, worded by `platform.ConfinementTeardownNotice`, rather than a `defer`. §7's
> `cmd /c` arm landed as the per-OS `writeLine`/`nestedWriteLine` in
> `internal/platform/confinetest/lines_windows.go` and `lines_other.go`, so the battery no longer
> spells `sh -c` itself. The profile row is `write_under_user_profile_denied`, and `ProbeNetwork`'s
> `NetworkEgress` guard sits in `probeNetwork`. The decision itself is unchanged.
