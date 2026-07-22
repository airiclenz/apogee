---
Status: accepted
---

# Windows confinement is a restricted low-integrity token, and the box is a label on the disk

## Context

Windows is the last Phase-0 stub in the confinement story. `internal/platform/confiner_other.go`
hands it `denyConfiner`, so `--mode auto` on Windows reports `{FSWrite:false,
NetworkEgress:false}`, every subprocess call takes the Approval path, and the degradation
notice fires on every session. That is *correct* under
[ADR 0012](0012-confinement-attaches-to-blast-radius-and-confine-to-workspace-flag.md)
("confine if you can, gate if you can't"), not a bug — and it is exactly what Phase 5 exists to
end. This ADR is the design session the merge plan's risk table asked for: which facility, what
it may honestly claim, what it costs, and where the floor is.

**The two shipped backends fence by path policy. Windows has no facility of that shape.**
Landlock takes a ruleset of path-beneath allow rules and applies it to the subject; seatbelt
takes a profile with `allow file-write*` under the box's roots. Both are *handed the box* and
enforce it, and **neither touches the user's disk**. Windows' mandatory access control (MIC)
fences by **identity**: a process token carries an integrity level, a securable object may carry
a mandatory label in its SACL, and the kernel's mandatory check runs *before* the DACL. Nothing
in that model takes "these paths are writable" as an argument. Everything below follows from
that one asymmetry.

**What the pinned toolchain actually offers** (verified against `go 1.26` and
`golang.org/x/sys v0.45.0`, both pinned in `go.mod`):

- `syscall.SysProcAttr` on Windows has a `Token syscall.Token` field — `os/exec` starts the
  child through `CreateProcessAsUser` with a caller-supplied token. It is the **only**
  confinement hook `os/exec` exposes on Windows.
- `x/sys/windows` has the identity half in full: `OpenProcessToken` / `DuplicateTokenEx`,
  `SetTokenInformation` + `TokenIntegrityLevel` + `Tokenmandatorylabel` + `SIDAndAttributes`,
  `CreateWellKnownSid(WinLowLabelSid)`, `GetNamedSecurityInfo` / `SetNamedSecurityInfo` +
  `LABEL_SECURITY_INFORMATION` + `SE_FILE_OBJECT`, `SecurityDescriptorFromString` (SDDL) and
  `RtlGetNtVersionNumbers`. It does **not** bind `CreateRestrictedToken` — one `advapi32`
  `LazyProc` to declare, the same shape the landlock backend already takes with raw
  `unix.Syscall`.
- It has **no AppContainer surface at all** (only the `KF_FLAG_*_APPCONTAINER` /
  `CLSCTX_APPCONTAINER` / `ERROR_*_APPCONTAINER` constants), and `SysProcAttr` has no field for
  a `PROC_THREAD_ATTRIBUTE_LIST`, so `PROC_THREAD_ATTRIBUTE_SECURITY_CAPABILITIES` — the only
  way to *launch into* an AppContainer — is unreachable through `os/exec`.

## Decision

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
be expressed is on the objects themselves. Each root gets `S:(ML;OICI;NW;;;LW)` — a mandatory
label ACE, object- and container-inheritable, `NO_WRITE_UP`, Low — via
`SecurityDescriptorFromString` → `SACL()` → `SetNamedSecurityInfo(…, LABEL_SECURITY_INFORMATION,
…)`.

- **It must recurse over existing contents.** Inheritance applies to *newly created* objects
  only. A file that predates the labelling is implicitly Medium, so a Low child editing an
  existing source file would be denied — which is the single most common thing an agent does.
  Item 8 walks the roots.
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

## Considered options

- **AppContainer.** The better fence in the long run — a per-app SID, capability-based, with the
  network itself a capability, so `NetworkEgress` could one day be honest. Rejected for v1 on
  three counts. *Behaviour:* it denies network by default and, worse, **blocks loopback** unless
  `CheckNetIsolation LoopbackExempt` is run **as administrator** — a real regression for an agent
  whose routine work is starting a local server and curling it. *Plumbing:* `x/sys/windows`
  v0.45.0 has no AppContainer surface and Go's `SysProcAttr` has no hook for
  `PROC_THREAD_ATTRIBUTE_SECURITY_CAPABILITIES`, so it needs hand-rolled `mksyscall` bindings
  **plus** a resident wrapper parent process calling `CreateProcess` with a `STARTUPINFOEX` —
  i.e. exactly the helper process the token design deletes. *And it does not even buy back the
  disk mutation:* an AppContainer still needs DACL ACEs for its package SID on the box's paths.
  **Recorded as the post-v1 tightening**, with the loopback/network problem named as what must be
  solved first, before the bindings are worth writing.
- **Job-Object-only.** A Job Object bounds resources, UI access and process count, and is how the
  whole tree gets killed (item 7) — but `JOB_OBJECT_LIMIT_*` has no path or write restriction
  whatsoever. It can never make `FSWrite` true. Rejected as a *confinement* facility; retained,
  unchanged, as the **teardown** mechanism it actually is.
- **A restricted token with restricting SIDs, Chromium-style, and no integrity label.** Rejected
  for v1: it fences through a double DACL check, so *reads* of system DLLs break unless the
  restricting SID set is tuned per program, and the allow half still has to be written onto the
  box's DACLs — the same disk mutation, more of it, and more brittle.
- **A helper process mirroring the Linux 42-liner.** Rejected: there is nothing for it to do. No
  "restrict myself" API exists, and `SysProcAttr.Token` already expresses the restriction at
  process-creation time. It would add a sentinel, an argv encoding, a second process and a second
  failure mode, for zero additional enforcement.
- **Reporting `NetworkEgress: true` because the box's default is network-open anyway.** Rejected:
  capabilities describe what the backend **can enforce**, not what today's box happens to ask for
  (contract §5). Claiming true would leave a non-empty `NetworkAllow` silently unenforced — the
  precise defect `networkDenyDecision` fails closed on.
- **Not labelling at all — run Low and accept a read-only workspace.** Rejected: a confined agent
  that cannot write to its workspace is not confined, it is broken. `FSWrite` would have to be
  reported false and Windows would stay gated, which is the status quo Phase 5 exists to end.
- **Labelling but never restoring, for speed.** Rejected: it would permanently and invisibly
  lower the integrity of the user's project tree. A tool that mutates ACLs and does not clean up
  after itself has not earned `--mode auto`.

## Consequences

- **Windows joins Linux and macOS as Auto-eligible.** The degradation notice vanishes on a
  capable host and persists below the floor (item 8's acceptance).
- **Apogee mutates the user's disk on Windows and on no other OS.** Reverted on teardown,
  journalled against a crash, reported by `probe host`, and documented in README/CHANGELOG at
  item 10. This is the headline consequence and it must not be soft-pedalled anywhere it is
  described.
- **`domain.Confiner` is unchanged.** Teardown rides an optional `io.Closer`, asserted at the
  composition root.
- **Contract §2.2's "performs no I/O and blocks on nothing" is amended** (contract §9): the
  Windows backend performs bounded, idempotent, once-per-box label I/O inside `Confine`. It still
  never runs the command and never blocks on it.
- **Contract §4's precomputed `Confine`-fallback becomes a routine path**, not a theoretical one.
  Every surface that describes it as a rare safety net is now describing the common Windows case
  of a box root on a share or a read-only path.
- **The below-floor path is UNTESTED.** The execution machine is build 26200 and no below-floor
  Windows host exists here; the plan's owner-run checklist keeps the item, and this ADR is where
  the gap is recorded rather than assumed away.
- **`NetworkEgress` stays false on Windows** until an AppContainer or WFP backend exists, so
  `--mode auto` with a non-empty `NetworkAllow` gates on Windows. That is the honest outcome, and
  it is identical to Linux below kernel 6.7.
- **Item 6 owes `Path.Contains`** with case folding as a parameterised pure function; item 7's
  job objects are untouched by this ADR and remain teardown, never a fence.
- **ADR 0021's `probe host` gains its first Windows-specific line** — the outstanding-label
  journal — which is the surface that makes an interrupted cleanup diagnosable off-session.
