# Changelog

All notable changes to Apogee are recorded here. The public Go API follows
[Semantic Versioning](https://semver.org/spec/v2.0.0.html) from `v1.0.0`
onward (ADR 0001 §consequences, as amended at the Phase-3 cut): Events and
hook points stay **additively extensible**, so a new Event variant or hook
point is a **minor** bump, not a breaking change.

## [Unreleased]

*Merge-plan **Phase 5 — cross-platform hardening & retirement** — the last open phase — closed
2026-07-22 (`docs/plans/2026-07-22 - 00 - phase5-cross-platform-hardening-plan.md`). Its
deliverable was "cross-compiled binaries for Win/Mac/Linux, **Auto confined on all three**", and
that is now true.*

### Added

- **Auto mode is confined on Windows: the fence is a restricted low-integrity token, and the box
  is a label on the disk.** Windows was the last Phase-0 stub — `denyConfiner`, so `--mode auto`
  reported `{FSWrite:false, NetworkEgress:false}`, every terminal/`python_exec` call took the
  Approval path, and the degradation notice fired on every session. That was *correct* under
  ADR 0012 ("confine if you can, gate if you can't"), never a bug, and it is what this release
  ends. The two shipped backends fence by **path policy** — landlock takes a ruleset of
  path-beneath rules, seatbelt a profile with `allow file-write*` under the box's roots, and
  neither touches your disk. Windows has no facility of that shape: mandatory integrity control
  fences by **identity**, and nothing in that model takes "these paths are writable" as an
  argument. Everything below follows from that one asymmetry. **The fence** is a restricted,
  Low-integrity primary token handed to `SysProcAttr.Token`: the child runs at Low, every object
  carrying no explicit label is implicitly Medium with `NO_WRITE_UP`, so a write outside the box
  is denied by the kernel *before* the DACL is consulted — and because a process inherits its
  creator's token, the denial covers the whole descendant tree for free (the Windows equivalent of
  "the domain survives `execve`"). `CreateRestrictedToken(…, DISABLE_MAX_PRIVILEGE, …)` is defence
  in depth, not the fence — no restricting SIDs and no deny-only SIDs, which break ordinary
  programs and buy nothing the integrity level does not already give. **The box** is a mandatory
  label written on `WorkspaceRoot ∪ WritablePaths` for the run and **reverted on teardown** — a
  side effect on the user's disk that landlock and seatbelt do not have, accepted deliberately
  because it is the only way the box's writable half can be expressed at all, journalled per-PID
  under `<apogee home>/confinement/` against a crash, recovered at construction, and reported by
  `apogee probe host` when an interrupted run left one outstanding. There is **no helper process,
  no argv sentinel and no argv rewrite**: Linux needs its 42-line re-exec helper only because the
  CGO-free way to run code between `fork` and `execve` is to *be* a process that restricts itself,
  and Windows has no "restrict myself" API to mirror — so `Confine` sets the token and returns,
  `cmd.Path`/`cmd.Args` are untouched, and `maybeDispatchConfinedExec` gains no Windows arm.
  (`internal/platform/{confiner_windows.go,winconfine.go}`;
  [ADR 0020](docs/adr/0020-windows-confinement-is-a-low-integrity-token-and-the-box-is-a-disk-label.md);
  `docs/design/confinement-execution-contract.md` §9.)
- **What the Windows backend honestly claims, and where it stops.** Capabilities are
  `{FSWrite: true, NetworkEgress: false}` — **network egress is not claimed on Windows**, because
  the token fences the filesystem and nothing else, and a `ConfinementBox` carrying a non-empty
  `NetworkAllow` **fails closed** with `ErrConfinementUnavailable` (mirroring landlock's
  `networkDenyDecision`) rather than pretending a requested tightening happened. `AutoEligible()`
  stays FSWrite-only per ADR 0012, so Windows is Auto-eligible on that basis alone. The supported
  floor is **Windows 10 1809 / build 17763 / Server 2019** — the oldest branch under any
  servicing, at or above Go's own floor — read from the un-shimmed `RtlGetNtVersionNumbers`;
  **below it nothing changes**: `NewConfiner()` returns the deny backend and the degradation
  notice fires exactly as before. Honesty is split across two moments, the one structural
  difference from Linux/macOS: `Capabilities()` probes the facility **once at construction**,
  while a per-run labelling failure is a `Confine`-time `ErrConfinementUnavailable` that feeds the
  execution contract's forced-Gate path. A box root that cannot be labelled — or cannot even be
  *compared* (an unresolvable 8.3 short name, a device path, a drive-relative `C:work`) — fails
  closed; a failure on an individual descendant is tolerated, because one locked file becoming
  read-only to the child must not gate a whole session; symlinks and reparse points are skipped,
  since `SetNamedSecurityInfo` follows them and labelling one would mutate a target outside the
  box.
- **`apogee probe` — the diagnosis command, in two halves with deliberately asymmetric cost.**
  Promised twice (as the confinement-diagnosis subcommand and as model capability probing) and
  blocked on a CLI that had no subcommands at all. `apogee probe` now prints the **host report**
  from the parent's own `RunE` — OS/arch, the Confiner backend and its capability matrix, the
  `AutoEligible()` verdict, the effective `confine-to-workspace` *after* the host acknowledgement
  is resolved, the workspace root and config home, endpoint reachability and the `/v1/models` +
  llama.cpp `/props` discovery outcomes reported **separately**, and an outstanding Windows label
  journal if there is one. It runs no agent, calls no model, and writes nothing — unlike the root
  command it does not even seed a starter config, which is pinned by a test — and it resolves
  flags, `APOGEE_*` and `config.yaml` exactly as a session would, so what it reports is what a
  session on this host would run with. `apogee probe host` is the same report under a named child,
  so a script never has to rely on a bare parent's semantics staying put. Because `/confine
  status` (TUI) and `apogee probe` (CLI) answer the same question, the selection and notice logic
  was **extracted, not duplicated**: `internal/probe`'s `BackendName` / `DegradedNotice` /
  `CapabilityLine` are the single source both render, so two views of one verdict cannot drift,
  and the host report closes with the startup degradation notice verbatim.
  (`cmd/apogee/probe.go`, `internal/probe`;
  [ADR 0021](docs/adr/0021-probe-is-two-halves-the-host-report-is-free-the-model-battery-is-an-explicit-act.md).)
- **`apogee probe model` — the capability battery, and the `ConfidenceMedium` fingerprint slot
  finally filled.** The model half is an **explicit act**, never a side effect of typing the bare
  noun, because it spends live model calls *and* writes: it asks the model to emit a native tool
  call, return a JSON object, and carry a tool result into a second call, then reports what it
  observed, an ordinal **capability tier**, and the model-profile knobs the findings suggest as
  **paste-ready YAML** (printed in the `offerNotice` tradition — `config.yaml` is never edited).
  It then records a versioned, owner-private (0700/0600) probe record keyed on endpoint +
  advertised label + timestamp, which `internal/library`'s resolver consults as the middle rung of
  the ladder its `fingerprint.go:40` comment reserved: **weights hash (high) → stored probe record
  (medium) → metadata label (low)**. Persistence is the point rather than a convenience — identity
  resolves through a pure offline call at startup, so a Medium tier that was never written down
  could never be observed. **Probing does not rename your model.** The behavioral tier promotes the
  **advertised label** to medium confidence and files the observed feature vector beside it as a
  separate **behavioral signature** (`probe:<battery>:<features>[:lp-<digest>]`) — evidence, never
  a match key. The first implementation minted a synthesised label from the features, which matched
  no Validated-set entry, no user alias and no Library key, so the command advertised as the
  promotion from *offered* to *auto-applied* silently did the opposite; that is recorded as a dated
  Amendment to ADR 0021 and the signature keeps ADR 0021 §6's substance (a fuzzy feature match over
  observed capabilities, logprobs preferred where the server exposes them, **never** a hash of
  response text) — only its role moved, from identity to evidence, and drift detection now rests on
  it. Consequence worth knowing before you run it: at medium confidence a matching Validated set
  **auto-applies** instead of being offered (ADR 0016 §5), so probing is the act that switches that
  automatism on — `--no-save` runs the whole battery and records nothing, and the record's path is
  printed either way so deleting the file undoes it. An **incomplete** battery mints no fingerprint
  at all (a hole in the evidence must not become an identity), and `probe model` refuses when
  neither `--model` nor the server names a model, since with no label there is nothing to key a
  claim on. *Adaptive prompt complexity is deliberately NOT built*: the probe ships the capability
  tier as a **signal with no automatism**, and the transform is recorded as a `TODO.md` follow-on,
  because a model-facing transform is a Mechanism by definition and earns its place on the ADR 0009
  non-inferiority gate with a bench campaign behind it — validated, not assumed.
- **Cobra subcommands, with bare `apogee` byte-identical.** The root command now accepts children
  (the seam shipped empty, so the Commands section in `--help` appeared only when `probe` landed).
  Everything load-bearing is unchanged: `maybeDispatchConfinedExec` is still the first thing `main`
  does, before Cobra parses anything, `Args: cobra.NoArgs` is retained on the root `RunE`, and no
  existing flag or environment path moved. `apogee headless` remains **deferred** — the skeleton
  merely makes it possible later.
- **Windows kills the whole process tree, not just the leader.** `internal/tools`' teardown stub
  killed only the process it started, so a cancelled `terminal` call could leave a grandchild
  running. The container is now an unnamed **Job Object** created between `Start` and `Wait` (a
  process can only be assigned to a job *after* `CreateProcess` returns, so `runSubprocess` runs
  Start → contain → Wait instead of `cmd.Run()`; POSIX takes a no-op teardown and the path is
  byte-for-byte what `Run` did) with `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`, terminated explicitly by
  `cmd.Cancel`, honouring the same contract §2.4 obligations as the POSIX `Setpgid` +
  negative-PID-kill path, `WaitDelay` unchanged. A **clean** run clears the kill-on-close limit
  before closing the handle, so a process the command deliberately backgrounded outlives it exactly
  as it does under a POSIX group leader — the limit's real job is the crash path. Both halves are
  pinned by native tests **verified by negative control** (stub out the containment and the tree
  test fails; remove the limit-clear and the survival test fails), and the shared decision function
  `planTreeKill` is untagged and table-tested on every OS.
- **The `platform` `Shell`/`Path` seam is real on both hosts.** The two Phase-0 widening `TODO`s
  are retired: `Shell{Command, CommandLine, Quote, ScopeEnv}` and `Path{Contains}` now
  live in **one untagged rule table** compiled on every target — only `Current()`'s choice is
  build-tagged — so Windows semantics are table-tested from a Linux run and executed natively on
  Windows. `CommandLine` exists because Windows has no argv at the syscall boundary: `os/exec`
  joins arguments with `syscall.EscapeArg`, which escapes an embedded quote as `\"`, a form
  `cmd.exe` does not understand (measured: a `cmd /c` of `echo "hello world"` prints `\"hello
  world\"`, and a redirect to a quoted spaced path dies with "The filename, directory name, or
  volume label syntax is incorrect"), so the verbatim command line goes to `SysProcAttr.CmdLine`
  and is `""` on POSIX, where `execve` takes a real argv. `Quote` answers to **both** parsers that
  read that line: `CommandLineToArgvW`'s backslash rules in the child (every backslash run touching
  a quote is doubled) and `cmd.exe`'s quote-toggling in front of it — so a value carrying a quote
  of its own is caret-escaped, which is what stops a quote plus an `&` from reaching `cmd` as a
  live command separator. Both halves are proven by a native round-trip through a real `cmd /c`,
  not by a golden string. `Contains` is the case-folded containment
  the Windows Confiner needs, and it is **"resolve, else refuse"**: `\\?\` and `\\?\UNC\` are
  normalised, drive-relative (`C:work`) and device (`\\.\…`) paths are refused as non-locations,
  and an 8.3-shaped component is expanded through `GetLongPathNameW` (walking up to the longest
  *existing* prefix, since the API is undefined for a path that does not exist yet) or the answer
  is `false`. There is deliberately **no `LookPath` wrapper** — `os/exec` already implements per-OS
  lookup including `%PATHEXT%`, so one would be dead surface. Two existing callers were adopted:
  the `terminal` tool carries the verbatim command line, and `safeGitEnv` runs through `ScopeEnv`,
  so a Windows `git` child finally gets `%SystemRoot%`/`%ComSpec%`/`%PATHEXT%`/the profile paths
  its POSIX-shaped allowlist never named (POSIX output is byte-identical — that floor is empty by
  design).

### Fixed

*The Phase 5 code review (`docs/reviews/code-review-2026-07-22.md` — 5 High, 11 Medium) found the
implementation faithful to its ADRs on the happy path and concentrated its defects in the paths
that had never run live. All of them are fixed below, before the owner's live-enforcement proofs
(`docs/plans/2026-07-22 - 02 - phase5-review-fixes-plan.md`).*

- **The Windows label journal now survives every path that could lose it.** The journal exists so
  the one disk mutation apogee performs — the Low mandatory labels on the box roots (ADR 0020 §2) —
  is always revertible, and four defects could leave it wrong or absent. (a) It was deleted
  *regardless* of whether the revert succeeded: `restoreLabels` and `recoverLabelJournals` now
  remove the file **only** when the revert returned nil, so a failed teardown leaves the record on
  disk for the next `NewConfiner()` to retry and for `apogee probe host` to report; the decision is
  an untagged helper, table-tested off Windows, and the surviving `Close()` error — previously
  swallowed at the composition root — prints one stderr line naming the journal path and the
  `icacls` remedy (`platform.ConfinementTeardownNotice`, sharing its wording with the residue
  notice). (b) The journal could record apogee's **own** Low label as the user's prior state, so
  teardown *restored* the label it was meant to remove, self-perpetuatingly: entries are now
  deduped per path (case-folded, first prior wins) and any prior naming the LOW level — `LW` or
  `S-1-16-4096`, in any descriptor spelling — is recorded as *empty*, so the revert clears to
  unlabelled. Where a prior is ambiguous, restoring toward **less** privilege is the safe
  direction, and ADR 0020's own manual remedy says an explicit Medium label and no label are
  behaviourally identical. (c) An unresolvable `%USERPROFILE%` meant no journal home and labelling
  proceeded anyway: `labelBox` now returns `ErrConfinementUnavailable` **before any label read or
  write**, which the execution contract demotes to a Gate — nothing is ever labelled without an
  undo record. (d) Writes were truncate-in-place and re-issued for every pre-labelled descendant
  found during the walk: journals are now published atomically (temp file, `Sync`, `os.Rename`) and
  a journal that cannot be decoded is no longer silently skipped by `confinementResidue` — it is
  reported as "journal present but unreadable", with the manual remedy stated as the only one.
  (`internal/platform/{winconfine.go,confiner_windows.go}`, `cmd/apogee/wire.go`.)
- **`apogee probe` reports outstanding confinement residue instead of healing it away.** In the
  `probe.Inputs` literal the Confiner was constructed *before* the residue was read, and on Windows
  that constructor reverts labels and deletes dead journals — so the interrupted-run case ADR 0020
  §2 assigns to `probe host` could never be reported, on a command three surfaces pin as read-only.
  The residue is now captured into a local first, and the probe path constructs through a new
  `platform.NewReportConfiner()` whose Windows variant performs **no** recovery: the read-only
  pledge (ADR 0021 §1, the README, the command's own `Long` text) stays absolute and unamended, the
  session constructor still heals on the next real run, and — since the journal now survives a
  failed revert — nothing is lost by reporting rather than repairing. The stale "the host half
  stays read-only" comment and the two docs that claimed construction touches no disk (ADR 0020
  §2/§3, `docs/design/confinement-execution-contract.md` §9.2) were corrected in the same change.
- **Ordinary cmd.exe command lines are no longer rejected by a POSIX parser.** The `terminal` tool
  ran `shlex.Split` over every command before handing it to the shell — including on Windows, where
  the shell is cmd.exe — so `echo don't panic` and `dir "C:\Program Files\"` died with "could not
  parse command line" on one of the three shipped platforms. The pre-flight now runs only where the
  platform hands the shell a real argv (the established raw-command-line convention:
  `shellHost.CommandLine(line) == ""`), and cmd reports its own errors, which is the honest
  arrangement — cmd has no stable quoting grammar worth pre-parsing, and no malformed-input class
  survives a check that would be truthful. POSIX behaviour is byte-identical. The
  "shlex-validated" claim in `docs/design/technical-design.md` §5 (P3.8) documented the defect and
  is corrected.
- **The Job Object handle is released on the two routine early-exit paths.** The teardown handle
  was created before `Confine` ran, but a `Confine` error returned before `runWithTeardown` was
  ever called, and a `cmd.Start()` failure returned before that function installed its own
  `defer`. Ownership moves to `runSubprocess`, which defers `release()` immediately after creating
  the teardown; the redundant inner `defer` is gone and `release` stays idempotent. A clean run is
  pinned to release exactly once, so the fix cannot silently become a double-release.
- **`probe model` can no longer claim an auto-apply that the next session start will refuse.**
  `autoApplyKeys` mirrored the startup ladder but omitted its catalogue-validation step, so a
  user-local validated-set entry naming a nonexistent mechanism ID was reported as `AUTO-APPLIES`
  and then skipped with a warning at startup. It now runs the same `validated.Validate` step and
  names the skip in the report's `suppressed` wording, carrying the catalogue's own reason
  verbatim. Consolidating the two ladders into one shared "would this entry apply" function is
  recorded in `TODO.md` for `/improve-codebase-architecture` rather than done here.
- **The review's remaining findings landed as tests and seams, not behaviour changes**: the
  pre-spend refusal gates in `probe model` are covered (empty `/v1/models`, discovery failure —
  zero `/chat/completions` hits, config home untouched) and the vacuous
  `TestGatherModelWritesNothing` — which asserted an unrelated temp dir was empty — is deleted in
  favour of the honest cmd-level coverage; the Windows build-floor decision moved into the untagged
  `belowWindowsFloor` predicate so the deny-vs-token selection is provable on every OS; the
  `windowsQuote` table gained its adversarial rows (which surfaced the quoting fix recorded under
  *Added* above) and the caller-less `Path.ExecExt` is gone; and the confined-`%TEMP%` /
  `ConfineWritablePaths` gap is now recorded in `TODO.md` with its anchors and called out in the
  README, rather than left implicit.

*The post-fixes review (`docs/reviews/code-review-2026-07-22-phase5-post-fixes.md` — 2 High,
12 Medium) found the hardened journal correct on its happy path and concentrated its defects at
the lifecycle's **edges**, plus one recurrence of the twin-ladder defect class. All of it is
fixed below (`docs/plans/2026-07-22 - 03 - phase5-second-review-fixes-plan.md`).*

- **The label journal's fail-closed rule now holds at every edge, in both directions** — no label
  without a journalled prior, no retirement while labels remain — where the first pass had proven
  it only for the happy path. Five edges were open. (a) A descendant whose prior label could not
  be *read* was relabelled anyway, unjournalled — a foreign security label destroyed with no
  record; the walk's three-way choice is now the untagged `descendantLabelDecision`, and a read
  error skips the path entirely (no label, no entry — the same tolerated-descendant posture as
  any other locked file). (b) `clearLabelTree` swallowed every descendant failure, so teardown
  retired the journal while Low labels remained on disk with no residue report; below-root
  failures are now counted into `clearTreeOutcome` — tolerating only `os.IsNotExist` — and any
  remainder keeps the journal for the next session's retry. (c) A prior-labelled file the agent
  deleted (routine workspace activity) failed the revert *forever* — `Close` warned every
  session, recovery failed silently every startup; a vanished path is now a completed revert,
  the "restored, not reconstructed" posture the root already took. (d) A failed ROOT label write
  refused the box but stranded its just-journalled entry, turning every later `Close` into a
  failing no-op and `apogee probe` into a permanent false alarm over a disk carrying no label;
  `labelBox` now unwinds the entry (`unwindLabelEntry`) when it recorded no foreign prior — an
  entry *with* one is kept, ambiguity resolving toward the record. (e) Two concurrent sessions
  on one workspace un-fenced each other: A's teardown stripped the shared root while B ran, and
  B's labelled-memo never re-labels; teardown and recovery now leave any root named in a live
  sibling's journal in place (`revertibleRoots`, untagged and liveness-injected) — the sibling
  owns the clear obligation, so the retiring journal still retires. And the single behaviour the
  machinery exists to deliver — a foreign prior label actually restored end-to-end — is now
  pinned natively and verified by negative control against both silent regressions (prior-restore
  loop deleted; clear/restore order swapped), alongside a construction test proving session
  recovery never deletes a journal it cannot decode.
- **A foreign prior label on a shared root now survives concurrent sibling sessions.** Follow-on
  to (e) above, found by its own implementation pass: when session A's teardown spared the shared
  root for live session B, A still *restored* the root's foreign prior — overwriting the Low
  label B was fenced by — retired its journal, and B's later clear wiped the restored label; the
  only record of the foreign prior was destroyed. A prior at or under a root ANY sibling journal
  still claims (live or dead — the file is the undischarged claim either way) is now **handed
  off** instead of restored: the retiring journal survives, rewritten to exactly the deferred
  prior entries under its original owner (`restorablePriors`, untagged and table-tested;
  `retireLabelJournal` grew the third fate beside "retired" and "kept whole"), and the first
  construction after the claiming journals are gone completes the restore — recovery now sweeps
  until no journal retires, so a handoff whose claimant died is finished in the same pass rather
  than the next session. The A-Close-then-B-Close sequence is pinned natively end-to-end and
  verified by negative control against both regressions (the pre-fix restore-now revert; the
  handoff record dropped at retirement).
  (`internal/platform/{winconfine.go,confiner_windows.go}`.)
- **`probe model` and startup no longer run twin identity ladders — the claim IS startup's
  decision.** The defect class closed for catalogue validation recurred at two more rungs:
  with no `model:` pinned, the report claimed `AUTO-APPLIES` for a record startup's empty model
  id can never resolve, and a `--model` naming a reachable weights file resolves at High so the
  Medium behavioural record is inert — both now suppressed lines naming the reason. The
  consolidation recorded in `TODO.md` was pulled forward rather than patched around: one shared
  ladder, `startupSetDecision` (`cmd/apogee/validatedsets.go` — off-switches,
  `ResolveFingerprintFrom`, `Match`, explicit-`mechanisms:` precedence, catalogue validation, in
  startup's order), which `resolveValidatedSet` enacts and `autoApplyKeys` reports — computed
  against the disk as the probe run leaves it, the promotion computed counterfactually with the
  record rung removed. The three off-switch branches each carry a test; parity is now by
  construction, not by parallel re-implementation.
- **The probe report's remaining honesty gaps are closed.** The no-record branch claimed
  "identity stays at the label tier" even when an earlier saved record survives and still
  applies — exactly the drift-check scenario `--no-save` serves; it now names the surviving
  record's date ("none new — the record from <date> continues to apply"). The v1-record warning
  the `Long` text promises was silently discarded on the model path's only read; it now prints
  on stderr. And `--workspace` changed nothing `probe model` reports or writes — dropped, per
  the probe commands' own flag rule (only `probe host` reads it).
- **A directory genuinely named like a short name is containable.** For a directory literally
  named `demo~1`, `GetLongPathName` returns its input — it *is* the long name — and that
  authoritative answer was indistinguishable from the nothing-resolvable fallback, so `Contains`
  refused a perfectly resolvable workspace into Gate. The `longPath` seam now signals authority
  (`(string, ok)`): `split` trusts an answered resolution without re-running the shape test,
  while an 8.3-shaped component the resolver could not verify still rejects, and POSIX rules are
  untouched.
- **The label guardrail sees through reparse-point roots and trailing-dot spellings.** The
  guardrail was lexical while `SetNamedSecurityInfo` is not: a root spelled `C:\Windows.` (OS
  canonicalisation strips the dot) or a junction targeting a protected location passed
  `Contains` and would have labelled the target Low. A box root that is itself a reparse point
  is now refused outright, every root is resolved to its final on-disk form
  (`GetFinalPathNameByHandle`) before the guardrail judges it — so the journal names the
  location the OS actually mutates — and the untagged rule table folds trailing dots and spaces
  off Windows components. Invariant hygiene, not an emergency: the roots come from trusted
  config and the confined Low child cannot create the precondition (ADR 0020 §6 gains the
  refusal sentence).

### Changed

- **The confinement degradation notice narrows to the hosts where it was always the honest
  answer.** Its trigger cell is unchanged (Auto **and** confinement asked for **and**
  `FSWrite == false`); what changed is that Windows ≥ build 17763 no longer lands in it. It still
  fires on an older Windows and in the containers where `landlock_create_ruleset` returns `ENOSYS`
  regardless of kernel version. Verified live on the execution host: `apogee probe host` prints
  `backend: token (fs-write: available · network: unavailable)` / `auto: eligible` and **no
  notice**, and the real `Terminal` tool under `platform.NewConfiner()` writes inside the box and
  is denied outside it with the Job Object and the verbatim command line composing unchanged. The
  escape battery (`internal/platform/confinetest`) now runs natively on Windows through
  `cmd /c` — in-box and writable-path writes land; out-of-box, user-profile and **nested-exec**
  writes all die with "Access is denied." and no file, so token inheritance across a second `exec`
  is **asserted, not assumed**. The below-floor path stays **untested** — no such host exists here.
- **Measured cost of the Windows disk mutation, which ADR 0020 accepted but did not quantify:
  ~1 ms per object.** A synthetic 5,051-object tree took **5.2 s to label and 2.2 s to revert**.
  It is paid once per box — the first confined command of a session — and once at shutdown, but a
  workspace with a large `.git` or `node_modules` will make that first `Confine` visibly block.
  Recorded rather than tuned: pruning the walk changes the ratified box semantics, so the cheap
  remedies (a startup notice, excluding ignored trees) are an owner call, not a silent one.
- **`internal/provider` gained an opt-in logprobs pair and a separate runtime context window.**
  `Request.LogProbs` and `RawResponse.TopCandidates` let the battery prefer logprobs where a server
  exposes them; the request fields are emitted as omitted pointers, so **every existing caller's
  bytes are unchanged**. `ModelInfo.RuntimeContextWindow` is new because the host report must state
  the `/v1/models` and llama.cpp `/props` outcomes separately, and `Discover` previously folded the
  `/props` window into `ContextWindow` with no way to tell which probe answered.
- **Fingerprint resolution grew its full ladder without breaking its callers.**
  `ResolveFingerprint(modelID)` is kept as the two-rung wrapper; the three-rung form is
  `ResolveFingerprintFrom(Sources{ModelID, Endpoint, ProbeDir})`, because the middle rung needs the
  endpoint and the home the old signature cannot carry. `internal/agent`'s call site adopts it too,
  deriving the probe dir from the injected `Config.ConfigDir` (an empty one simply removes the
  rung — never an ambient `~/.apogee`, per ADR 0001), so the Library's keying and the Validated-set
  match key **identically**; if only the wire-time call site could reach Medium, ADR 0021's
  consequences would be false in-loop.
- **Probe records written by an earlier build of `main` are not readable.** The ADR 0021 Amendment
  moved the record's `fingerprint` field to `behavior` and bumped `ProbeRecordVersion` 1 → 2. Old
  records are **skipped with a warning** naming the one-command fix — re-run `apogee probe model`
  once per model — and no migration tooling is built, deliberately: nothing was released with
  version 1. The same note ships in `apogee probe model --help`.

### Removed

- **The proxy and the OpenCode plugin / transform-server bridge — retired, on the record.** The
  decision was taken with the merge itself (merge plan §6 #4, 2026-06-22) and has governed every
  phase since, but nothing in *this* repo ever said so — which left a reader who met the
  `internal/proxy/…` breadcrumbs scattered through `internal/mechanisms` no way to tell a dead
  ancestor from a live dependency. Recorded here in the form it was actually executed: apogee-sim's
  OpenAI-compatible **reverse proxy**, the **transform-server bridge** and the **OpenCode plugin**
  are **not ported forward**, they remain in apogee-sim's git history as reference, and **apogee
  *is* the integration** — one integrated tool, not a Core exposed through peer integrations
  (`CONTEXT.md`'s retired-vocabulary section). Nothing is deleted and no behaviour changes, because
  none of that code was ever ported: this repo's only references to it are the **`@pin` provenance
  comments** on the ported Mechanisms (seventeen files in `internal/mechanisms` — `toolloop.go`
  pins `internal/proxy/tool_loop_interceptor.go`, `grammar.go` pins `proxy.go`'s
  `injectGrammarConstraint`, and so on), which say where a behaviour came from and what its A/B
  measured. Those are **history pins, not live references, and they stay verbatim**; the word's
  other occurrences in the tree — self-regulation's "proxy signals" and `internal/tools`' refusal
  of the `Proxy-*` hop-by-hop headers — are unrelated senses. Archival on the apogee-sim side is
  that repo's business, not this one's.
  (Merge plan §4 "Phase 5 — Cross-platform hardening & retirement", §6 #4.)

## [1.7.0] — 2026-07-21

### Added

- **`present_document` — the tool that shows a finished document to the user.** A Skill that
  produces a report — an architecture review, a research summary, a migration plan — used to end
  with a file on disk the user never saw: `write_file` renders as a one-line
  `Write File <path> +N bytes` card and nothing else, so the deliverable an Exchange spent its
  whole Budget producing was, from the user's seat, invisible. The model now closes that work with
  one dumb, explicitly named affordance — `present_document {path[, title]}` — and supplies
  nothing but a path and an optional title: the **host** picks the mechanism, so a ~4B–35B model
  never reasons about platforms, which is the thing it is worst at. The tool is the exact shape of
  `ask_user`: mode-**independent** (it never routes through the Approval gate), `ReadOnly()` so it
  runs in **every** mode including Plan (presenting writes nothing), **not** a safety gate, and
  **not** an `ExternalEffectTool` — the user's own display is not a non-forkable remote the bench
  must stub, any more than the human answering `ask_user` is. It is registered **only** when the
  host supplies a `Presenter`, so a headless embedder is unaffected by construction. The path is
  resolved inside the workspace root and must be an existing regular file; that, and a Turn
  cancelled mid-presentation, are the only ways the call fails.
  (`internal/tools`; [ADR 0019](docs/adr/0019-documents-are-presented-not-opened.md).)
- **The presentation ladder — the host decides how, and the baseline is never skipped.** The new
  `internal/present` package carries the host-side mechanisms and the TUI walks them per call, the
  highest applicable rung running *in addition to* rung 0, never instead of it. **Rung 0
  (always)** is the transcript entry carrying the workspace-relative path; it is the rung that is
  never wrong. **Rung 1** auto-opens the document when the session is **local** (no
  `SSH_CONNECTION` / `SSH_TTY` / `SSH_CLIENT`) and a desktop is detected (darwin/windows always;
  linux `DISPLAY` or `WAYLAND_DISPLAY`) — `open`, `cmd /c start "" <path>`, `xdg-open` — launched
  detached with its streams on the null device so an opener can never scribble on the Bubble Tea
  screen. **Rung 2** covers the remote case, which for this project is normal rather than exotic:
  a browser-renderable deliverable (`.html`, `.htm`, `.svg`, `.pdf`) is registered with an
  embedded **doc server** and its URL joins the entry, so one cmd+click opens it in the browser on
  the user's *own* machine, with **no host back-channel** anywhere in the path (rejected on
  security grounds). The server is a **capability-token allowlist, not a file server**: only
  explicitly presented files, each under a random 32-hex token at `/d/<token>/<basename>`, no
  directory listing, the only 200 a **GET or HEAD** whose path matches a grant exactly, and the
  same bare 404 for everything else — prefix walks, `..`, and every other method, refused as
  not-found rather than not-allowed because a 405 would confirm a real token — content-type from
  the extension and never sniffed, the file **re-read from disk per request** (so re-presenting
  after an edit shows fresh content), started lazily on the first served presentation, request
  logging discarded (net/http would log the token), and closed on app shutdown. Its advertised
  address is the server IP from `$SSH_CONNECTION` (known-routable, so it outranks the config key),
  else the `present.host` fallback, else an outbound-dial probe (no packets need to arrive), else
  `127.0.0.1`. **Rung 3** swaps rung 1's OS opener for the application named in
  `present.command`. Everything above rung 0 **fails visible**: an opener that errors, a server
  that cannot bind, a machine with nothing to open into — none of them fail the call, the outcome
  degrades to the baseline, the transcript says what happened, and the tool result names the rung
  (`opened` / `served` / `shown`) so the model tells the user the truth instead of asserting a
  success it cannot observe. The opener runs host-side, **outside** tool confinement, deliberately
  and for the reason ADR 0012 gives: it is the host's own act on the user's own desktop session,
  not a model-chosen subprocess. (`internal/present`, `internal/tui`.)
- **A first-class presentation entry in the transcript.** A deliverable is not plumbing, so it
  does not render as a tool card: the entry carries its own glyph (`▤`, deliberately not the tool
  `✦`), the optional title, and then the path — and, when served, the URL — as **plain text on
  their own lines**, unstyled, unwrapped and unclipped, one token per line. That is not a
  cosmetic choice: terminal linkification *is* the mechanism (Zed, VS Code, iTerm2, WezTerm and
  kitty all detect plain paths and URLs; Zed's cmd+click even opens the file through its remote
  server), and markup or a mid-token wrap is what breaks it. The line closes with what actually
  happened — "opened on your machine", "cmd+click to open", or a degraded "<reason> — path shown".
  The `Present` tool card keeps the one-line grammar the rest of the suite uses. (`internal/tui`.)
- **A file-only `present:` config block** — `auto-open` (default true; false disables rung 1 and
  **never** rung 0), `command` (a `{path}` template; the path is appended when the template does
  not mention it), `port` (default 0 — ephemeral, because the URL is printed fresh per
  presentation, so a stable port buys nothing) and `host` (the advertised address; a *fallback*
  for topologies `$SSH_CONNECTION` cannot describe, not an override). No flag and no environment
  variable, matching the newer keys' posture. The shipped `config.yaml` template documents all
  four, states plainly that apogee never auto-opens on a remote box (there is no display there to
  open into), and carries the macOS **Local Network permission** gotcha as the first thing to
  check when a served URL is unreachable — Chrome fails it with a generic "this site can't be
  reached" while Safari tends to work straight away. (`cmd/apogee`.)
- **`Presenter` — a new host delegate on `Config`, and additive public API surface.** `Presenter`
  joins `Approver` / `Asker` / `Confiner` on `domain.Config` and is re-exported from the facade
  with `PresentRequest` / `PresentOutcome` / `PresentMethod` and the three method constants
  (`PresentOpened`, `PresentServed`, `PresentShown`), so an out-of-module embedder can implement
  the interface and supply its own presentation mechanisms. Structs rather than bare strings, for
  the same freeze-safety reason `AskRequest` documents. Nothing exported is removed or re-typed
  and a nil `Presenter` leaves the tool unregistered, so the bench and every headless embedder are
  unaffected by construction: **additive ⇒ minor**. (`internal/domain`, `apogee.go`.)

## [1.6.0] — 2026-07-21

Post-`v1.5.0`, **additive** (minor) — two features end to end, plus a presentation pass over how
the chat reads. First: **Auto mode no longer degrades in silence.** On a host that cannot
fence a subprocess, ADR 0012's ladder ("confine if you can, gate
if you can't") sends every terminal command to Approval. That is correct, and it is the *common*
case rather than an exotic one — `landlock_create_ruleset` returns **`ENOSYS`** in most containers
whatever the kernel version — but nothing anywhere said so, so Auto simply read as broken
(`ISSUES.md`, 2026-07-21). Apogee now says so at startup, and offers the decision as a command:
`/confine off` for this session, `/confine off --save` to record *this machine* as disposable in
`~/.apogee/config.yaml` — a **host-scoped** acknowledgement, so a throwaway container's "I am the
sandbox" never follows the config file onto a laptop. **The ladder itself is untouched** and
auto-loosening stays forbidden: what shipped is visibility plus a signposted route to a decision
only the user may take (ADR 0012, amendment 2026-07-21). **No breaking change** — the public facade
(`apogee.go`) only *gains* methods, `Agent.SetConfineToWorkspace` / `Agent.ConfineToWorkspace`
(additive ⇒ **minor**, the same shape as the `Budget` methods in `v1.4.0`); nothing exported is
removed or re-typed. The whole journey is pinned by an acceptance test driven against a Confiner
that reports no filesystem confinement, so it reproduces identically on a machine that *can* fence.

Alongside it, a **presentation-only pass over the transcript layout** (`layout.md` is the amended
spec of record): assistant text no longer drags the model's own padding blank lines into the
scrollback, a tool header trades its `[brackets]` for a bold-orange label carrying nothing but
that label, a batch of same-label tool calls folds into one aligned block instead of five
stacked ones — with a lone call taking the very same shape — and a call's outcome is now split
into the one line that rides its branch and the body beneath it, which is what finally lets a
`View Diff` show `+2 -2` beside the path *and* the diff underneath. The four land as
separate **Changed** entries below because each is separately visible, but they are one change to
how a session *reads* — nothing the model sees is touched: no tool result, no event payload, no
upstream conversation, and nothing exported. `TestTranscriptLayoutGolden` pins the whole rendered
scrollback of a mixed session — prompt, narration, a grouped batch, a standalone `Run`, a diff
under its diffstat, an approval note, a sub-agent read — blank lines included, so a regression in
any one of the four shows up as a layout diff rather than a subtly taller chat.

Second: **a session no longer dies when the context window fills mid-task.** A `/refocus` against a
32k-window model hit `request (57546 tokens) exceeds the available context size (32768 tokens)` and
lost the whole Exchange — automatic Compaction is Exchange-boundary-only by design, so it could not
reach a doc-heavy Exchange growing past the window, and the only reducer that could act there was a
default-off Mechanism. Recovery is now **structural**, sitting with Budget and Compaction rather
than in the catalogue ([ADR 0018](docs/adr/0018-context-overflow-recovers-structurally-the-emergency-fold-and-one-retry.md),
per ADR 0006's rule that the floor stays on in the baseline and must be *functional*): an overflow
folds the history and re-sends the same Turn once, and a request the estimate already says cannot
fit is folded before it is sent at all. It runs under `--bypass`; `auto-compact: false` opts out of
it exactly as it opts out of boundary folding; nothing exported changed and no Event variant was
added, so this is behaviour-only.

### Added

- **Structural context-overflow recovery: the emergency fold and one retry.** A request the model's
  context window rejects is no longer a terminal fault. It is now its own Turn outcome, and the loop
  answers it by folding the conversation — the same generative Compaction `/compact` runs, keeping
  the protected prefix and replacing the rest with one summary — then re-sending the *same* Turn
  once. The fold is the one that may run **mid-Exchange**, deliberately amending the
  Exchange-boundary-only rule for this path alone (the estimate-driven trigger and the on-demand
  `/compact` still wait for the boundary: their caller can wait, a dying Turn cannot). It closes
  with a user-role bridge message — "the conversation above was compacted … continue the task from
  the summary" — so the retried request ends `…first-user | assistant-summary | user-bridge`: strict
  role alternation, no dangling tool calls, template-legal for any chat format; the open Exchange's
  rollback boundary is re-anchored to that bridge. Recovery is **structural** (it consults
  `auto-compact`, never `--bypass`) and bounded to **one fold per Turn**: a second overflow gives up
  exactly as before — the same sanitized `ErrorEvent` from source `"loop"`, the same abandoned
  Exchange — as does `auto-compact: false`, nothing left past the protected prefix to shed, or a
  failed summary call. Success is **quiet**: no new Event variant, the fold showing up only as the
  context gauge dropping on the next `UsageEvent`, and a cancel after a fold *keeps* the fold (it is
  history maintenance, not part of the Turn's attempt). (`internal/agent`; ADR 0018.)
- **A predictive guard that folds before sending a request that cannot fit.** Between building a
  request and sending it, the loop estimates it with the measure the whole engine shares
  (`PromptChars` through the calibrated chars→token ratio) against the full working room
  (`ContextLimit − ResponseReserve`) — never a softer fraction, because a fold is a lossy rewrite of
  the user's history. It saves the round-trip on a predictable overflow and covers the one case the
  reactive path cannot: a server whose 400 body cannot be classified as an overflow, where the
  stream yields a plain error and no recovery would ever fire. While the Budget is **uncalibrated**
  (no server usage reported yet — Turn 1, every sub-agent, and the first Turn after a resume, where
  the estimator is not serialized but the restored history may already sit near the window) the
  guard is **damped**, not disabled: it demands twice the working room, which is exactly the
  estimator's clamp band (8.0/4.0), so a false positive is impossible inside it while a pathological
  case still fires with room to spare. The asymmetry is the reason — an under-estimate costs
  nothing (the wire overflow routes to the reactive path) while an over-estimate spends an
  irreversible fold on a request that would have fit. A predictive fold spends the Turn's one fold,
  and a refused one simply sends the request as before. (`internal/agent`.)
- **A structural floor on a single oversized tool result.** A tool result whose estimate exceeds the
  *entire* History allocation is now clamped — head/tail plus an elision marker pointing at a
  `start_line`/`end_line` re-read — as it enters the conversation, at the one seam every result
  crosses, so no route (a plain call, a confined run, an approved gate, a sub-agent delegation, an
  error result) can bypass it. A result that large survives no reducer and can doom the Turn
  outright: the emergency fold's own summary call keeps the most recent message unconditionally, so
  a fresh giant result *is* that message and overflows the fold meant to rescue the Turn. It is
  structural rather than a Mechanism because ADR 0006 requires the baseline's reducers to be on and
  functional, and because `tool_result_cap` is default-off, Bypass-disabled, withdrawable, and caps
  only the turns *before* the most recent tool call — never the result that overflows; that
  Mechanism stays the tighter, A/B-able valve above the floor and, when enabled, fires first. The
  clamp rewrites the conversation, so the raw result never reaches history, a snapshot, or the
  transcript, and both reducers now share one rendering (`context.TruncateToolResult`) so the model
  learns a single elision idiom. (`internal/agent`, `internal/context`.)
- **A startup notice when Auto degrades to approval on an unfenceable host.** Entering `--mode
  auto` with confinement asked for (the default) on a host whose Confiner backend reports
  `FSWrite == false` now prints one stderr notice naming the backend, saying plainly that commands
  cannot be fenced here and therefore fall back to Approval, and pointing at `/confine off` (this
  session) and `/confine off --save` (remember this host). Nothing is worded as repairing a
  malfunction, because nothing is broken — a host that cannot fence is the ladder working as
  specified. It is the mirror of the existing unconfined-Auto warning and the two never both fire;
  the three lower modes make no confinement promise and stay silent, so this is never a general
  startup nag. (`cmd/apogee`.)
- **`/confine` — report and change Auto's blast radius from the chat.** A new verb in the TUI
  mini-language, and the first one that takes arguments: `/confine` (or `/confine status`) reports
  the backend, what it can actually enforce here, the host id an acknowledgement is matched
  against, and the effective setting — read live, so it reflects an earlier toggle — closing with
  the two remedy lines only on the host that prompts the question (Auto, confined, no fs-write
  fencing). `/confine off` and `/confine on` swap the blast radius on the running Agent,
  synchronous and idle-safe like `/clear` and taking effect on the next tool call, each recording a
  transcript confirmation that states the radius in plain words (`off` → "auto runs every command
  unfenced, with your full privileges") and says whether it was session-only or written to disk.
  Turning confinement off where it works, or off when it is already off, is allowed and simply says
  so: a legitimate choice, just not the degraded case. An argument the grammar does not understand
  — an unknown subcommand, an unknown flag, or a `--save` that is not persisting an `off` — is a
  parse **error carrying the usage line**, never a silent no-op: the one command that can widen what
  Auto may touch must never leave a user believing a mistyped line took effect. A slash command was
  chosen over a startup y/N prompt or an extra choice on the Approval prompt precisely to keep the
  accept away from the moment of peak frustration. (`internal/tui`, plus the composition root
  handing the TUI the backend/capability/host-id facts it must not derive itself.)
- **`Agent.SetConfineToWorkspace` / `Agent.ConfineToWorkspace` — Auto's blast radius is now a live,
  runtime-swappable setting.** `confine-to-workspace` was read from the construction Config on every
  tool call, so changing it meant restarting Apogee. It is now a live field on the Agent — seeded
  from `Config.ConfineToWorkspace`, read by the per-call Resolution through the accessor, and
  swappable at any time — exactly mirroring the `SetMode`/`Mode` pair behind Shift+Tab. Both methods
  are goroutine-safe (their own `RWMutex`, a sibling of `modeMu`), so the UI may toggle while the
  worker is mid-Step; the change lands on the **next** tool call with no rebuild and no registry
  churn. A sub-agent spawned after a toggle inherits the parent's live value, as it already did for
  the mode; one already mid-flight keeps what it was spawned with, so a toggle can neither loosen
  nor tighten a running delegation. The toggle changes only the running Session — nothing is written
  to disk — and the engine never flips the flag on its own initiative; it only carries out the
  user's explicit act. (`internal/agent`.)
- **`unconfined-hosts:` — the host-scoped confinement acknowledgement.** A new global-config-only
  list in `~/.apogee/config.yaml` recording *which machines* you have acknowledged as disposable, so
  Auto may run unconfined **there** without that claim following the file onto every other host.
  Each entry carries an `id` (matched against the new `platform.HostID()`), plus a free-form
  `acknowledged` date and `note` for the human reading the file back later. Resolution runs in the
  order the ADR fixes: an explicit `confine-to-workspace: false` still wins and still means *every*
  host (it is unchanged and not deprecated); else an entry naming **this** machine yields an
  effective `false`; else the secure default `true`. An explicit `confine-to-workspace: true` does
  not veto a matching entry — the flag states the global default, the entry states a fact about one
  machine, and the more specific claim wins. Like the flag, the list is settable **only** from the
  global config file — no flag, no environment variable — so a hostile repo's invocation environment
  cannot name your host. An id matching no machine is simply "not this host", never an error (the
  list is meant to accumulate machines), and an entry with no `id` is skipped with one startup
  notice rather than blocking the run. A host that can supply **no identity of its own** — no
  hostname *and* no machine-id file — never matches either, however the entry is spelled: the id
  such a host computes is the same on every one of them, so honouring it would let a single saved
  acknowledgement loosen the lot. That match is reported and ignored, and `--save` refuses to
  record it in the first place, so nothing is written that could quietly travel. The template
  `config.yaml` documents the block beside `confine-to-workspace`. (`cmd/apogee`.)
- **`platform.HostID()` — the machine interlock the acknowledgement is matched against.** A stable
  per-machine id shaped `<sanitized hostname>-<first 6 hex of sha256(machine identifier)>` (e.g.
  `devbox-a1b2c3`), where the identifier is the first available of `/etc/machine-id`,
  `/var/lib/dbus/machine-id`, else the hostname itself — no shelling out, so it stays
  dependency-free and correct on hosts (and future Windows builds) where neither file exists. It is
  a **safety interlock, not an authentication mechanism**: it stops an acknowledgement travelling
  between machines unnoticed, it does not resist forgery (anyone who can edit the config can write
  any id), and it fails closed — an ephemeral container with a fresh machine-id per run simply does
  not match its stored entry and is confined again. The value is deterministic within a process and
  across runs, never empty (a failing `os.Hostname()` yields `unknown-<hash>`), and restricted to
  `[A-Za-z0-9_.-]` so it is safe as an unquoted YAML scalar. Exactly one composed value is *not*
  per-machine — the one a host with neither a hostname nor a machine id computes, which is identical
  on every such host — so `platform.IsUnidentifiedHostID` names it and both callers refuse it as an
  identity: it never matches during resolution and it is never written to disk. (`internal/platform`.)
- **A comment-preserving config writer behind `/confine off --save`.** Saving appends this host's
  `unconfined-hosts:` entry (id, today's date, and a note saying what put the line there and that
  deleting it re-confines the machine) to `~/.apogee/config.yaml`, and reports the file back so the
  confirmation can name it. Your config survives intact. The file is edited as **text**, guided by
  the parsed node positions, never round-tripped through unmarshal→marshal: `yaml.v3` hangs comments
  off nodes, and the seeded template is *entirely* comments — it parses to no nodes at all — so a
  re-marshal would have handed you back a file with one setting in it and every word of
  documentation deleted. Comments, key order, indentation, and your own edits come back
  byte-identical. Because the key ships commented out, the writer **inserts** rather than
  substitutes: it appends to an existing list (matching that list's own indentation), starts one
  under a bare `unconfined-hosts:` key, or adds a documented block at the end of the file — so it
  stays correct against a config you have since reordered or rewritten by hand. Saving the same host
  twice records it once (the second call reports the entry already on disk and writes nothing). An
  absent config is seeded from the embedded template first, so `--save` never leaves a bare fragment
  where a documented file belongs. The write is atomic (temp + rename in the same directory) and
  preserves the file's mode, since a config may hold endpoint details. Every splice is re-parsed and
  compared against the original *before* anything is written — the result must be the old list plus
  exactly this entry, with no other setting touched — so an exotic file shape (a flow-style list, a
  second YAML document apogee would never read) is refused with a "add the entry by hand" message
  rather than quietly mangled, and a failed write surfaces as an error instead of a save that did
  not happen. A save that fails never invalidates the session toggle that already happened, and the
  confirmation says so. (`cmd/apogee`.)

- **`apogee.AuditEvent` — the facade's Event re-export is complete.** `apogee.go` aliased every
  Event variant except `AuditEvent`, which has shipped in `internal/domain` since the Phase-3
  security remediation. Because `internal/` is unimportable from outside the module, an embedder
  could *receive* an audit observation through the `Event` interface but had no way to name the
  type in a switch — the variant was effectively unreachable across the facade. The alias closes
  that; the variant's own contract is untouched, so this is additive. `example_test.go`'s
  compile-time facade guard now names every variant, `ReasoningEvent` and `UsageEvent` included, so
  the next omission is a build failure rather than a silent gap. (`apogee.go`.)

### Changed

- **Blank-line hygiene in the transcript — one empty line between blocks, never three.** Models pad
  their replies: a trailing `\n\n` (and often a leading one) survived the commit verbatim, and the
  renderer drew every one of those empty lines *on top of* its own one-line block separator, so a
  chat session grew two- and three-row gaps between every answer and whatever came next. Committed
  assistant text — both a `MessageEvent` and the pre-tool narration the first `ToolCall` finalises —
  is now trimmed of its leading and trailing blank lines, and interior runs of two or more blank
  lines collapse to a single one, so `layout.md`'s "exactly one empty line between blocks" is
  finally true rather than aspirational. **Fenced code blocks are exempt**: a blank line between two
  statements is part of the code and comes back verbatim. Text that is blank all the way through now
  commits **no entry at all**, where it used to leave a bare `✦` marker line sitting in the
  scrollback — and a blank *canonical* message still falls back to the streamed tokens, so nothing
  that arrived is lost. The live streaming preview trims only its trailing blanks, and only for
  display: the buffer itself keeps them, because a mid-stream `\n\n` may be a paragraph break the
  model is about to continue, while a just-opened empty buffer still shows its lone marker so you
  can see streaming has begun. Presentation only — the tool results, event payloads, and the
  upstream conversation the model sees are untouched. (`internal/tui`.)
- **Tool labels lost their `[brackets]` and gained colour.** A tool call's header now reads
  `✦ Read File main.go` instead of `✦ [Read File] main.go`, with the label alone rendered **bold in
  orange `#f0883e`** — the tone inline code and the auto-mode marker already carry — and the target
  left plain. The brackets were doing the work of making the label stand out from the target;
  weight and colour do it better and cost no columns. The styling is deliberately **uniform**: a
  known friendly label, an unregistered tool's raw name, and the stray-result `result` header all
  look the same. That retires the old "a bare name means the tool has no presentation entry"
  signal, which was the brackets' side effect rather than anything a reader could name — an unknown
  tool still falls back to its raw name and its verbatim pretty-printed arguments, so nothing about
  what the model asked for is hidden. The style is baked into the header before it is wrapped
  (the `markdown.go` posture — `ansi.Wrap` is SGR-aware and `lipgloss.Width` strips ANSI), so the
  soft-wrap and sticky-header arithmetic are unperturbed. Presentation only. (`internal/tui`.)
- **A batch of same-label tool calls is now one block, not five — and every tool call takes that
  same shape whether it is alone or in a batch.** Five file reads used to render as five separate
  headers, each with its own branch line and its own blank separator — a tall, noisy column for
  what the reader thinks of as one action. Consecutive tool calls at the same sub-agent depth
  carrying the same label now fold into a single block: one `✦ Read File` header, then one
  `┝`/`┕` branch per call whose target is **padded to the block's widest** so the detail column
  lines up (`┝ README.md 1 - 154` / `┝ TODO.md   1 - 408` / `┕ ISSUES.md 1 - 8`). The header
  carries **the label alone and never a target** — for a group, a lone call, a call still in
  flight and the stray-result `result` header alike — and the target always leads the first
  branch instead (`✦ Read File` / `┕ main.go 1 - 154`). That is what stops a block from visually
  reshaping the moment a second call joins it: a block of one is byte-identical in shape to a
  block of many, and growing one only ever *adds a line*. What a branch carries follows from which
  halves of the call's outcome are filled (see the entry below), and nothing else: the one-line
  summary follows the target on the branch, the body lays out **beneath** it at the branch
  marker's width rather than sprouting `┝`/`┕` branches of its own (a `Run` and its `… +N more
  lines` remainder, a red/green diff body under its diffstat); an in-flight call has neither yet
  and shows the bare target until its result lands; and a call with no target at all is the one
  shape with no target line — the header stands alone and the lines are the branches, as an
  unregistered tool's pretty-printed arguments and a stray result still render. Two different
  tools sharing a label — a single and a multi find-and-replace are both "Edit File" — do group,
  because the reader groups by what the header says, not by tool id; anything between two calls
  (narration, a note, an approval, an error) ends the run, and a call carrying a body or no target
  keeps its own block. **Grouping is render-time only** — the transcript's
  append-only entry list, its call/result pairing, and the open-call signal the status line reads
  are untouched, so a call arriving mid-stream joins its group on the next repaint for free.
  Nothing is clipped for alignment's sake; an overlong branch soft-wraps as before. Presentation
  only. (`internal/tui`.)
- **A tool call's outcome is now a summary line plus a body — and `View Diff` finally renders the
  shape `layout.md` has always sketched.** The presentation model carried one flat list of detail
  lines, and the renderer picked the block's shape by *counting* it: exactly one line joined the
  target on the branch, two or more laid out beneath a bare target. That made the sketch's diff
  block unreachable — a `+2 -2` diffstat plus its diff body was simply a three-line list, so the
  summary lost its place on the branch and the target lost its. The outcome is now split at the
  source: a one-line **summary** that rides the branch beside the target (`1 - 154`, `+2 -2`,
  `error: …`) and a **body** that hangs beneath it (a command's output, a diff's lines). The shape
  follows from which halves are filled and **never from how many body lines there are** — a body
  of one lays out exactly like a body of ten. `View Diff` is the one producer filling both, and it
  reads as the sketch always promised: `┕ main.go +2 -2` with the red/green diff beneath. The
  diffstat is counted over the **whole** diff, not the lines that survive the 20-line body cap —
  it is the one number a truncated body cannot tell you — and always names both counts (`+2 -0`
  for an addition-only change). `No changes detected` is not a diff and stays the single plain
  line it was. **Every other block renders byte-for-byte as before**, which is the point of the
  rule that any outcome fitting on one line is a summary: a one-line `Run`, `Git Branch` or
  `Diagnostics` result still rides its branch beside the command (`┕ pwd /workspace/repos/apogee`)
  and still folds into a group, while only output needing the `… +N more lines` remainder becomes
  a body beneath the command, exactly as it already did. Presentation only. (`internal/tui`.)

## [1.5.0] — 2026-07-21

Post-`v1.4.0`, **additive** (minor) — one TUI affordance and the one Event variant it needed to
be honest. The status line's left slot no longer reports the turn index (a number that answered
nothing the human was asking) but **what the worker is doing right now**, with an elapsed clock:
`thinking · 12s`, `reading · main.go · 3s`, `running · npm test · 8s`, `sub-agent · searching ·
6s`. Making `thinking` a fact rather than a guess required the engine to reveal that it is
reasoning, which is the new `domain.ReasoningEvent` — an observation-only variant on both the
native reasoning channel and the inline `<think>`/harmony spans that stay held off the visible
stream. **No breaking change**: per this changelog's own rule (ADR 0001 §consequences), a new
Event variant is a **minor** bump, and the public facade (`apogee.go`) only *gains* the
`ReasoningEvent` alias — nothing exported is removed or re-typed. The loop's visible token
stream, the committed assistant message, and history are byte-identical; ADR 0011's renderer
contract is untouched (no new `uiState`, no agent logic in the TUI), so no new ADR.

### Added

- **`domain.ReasoningEvent` — the observability seam for the model's reasoning channel.** A new
  Event variant beside `TokenEvent`, re-exported on the facade as `apogee.ReasoningEvent`,
  carrying one newly-revealed chunk of reasoning. It is **observation only**: it never changes
  history (the channel is already preserved as `reasoning_content` on the assistant message) and
  a consumer may treat its arrival alone as a liveness signal and ignore `Text` entirely, which
  is what the TUI does. The engine emits it on both paths — natively from
  `provider.DeltaThinking`, and inline from `emitReasoningDelta`, `emitVisibleDelta`'s mirror
  that runs *while* the stripper is mid-channel (the same prefix-stability guard: an unclosed
  span's tail is what the stripper routes into reasoning, and closed spans never change, so the
  accumulation only ever grows a suffix). `Text` is untrusted model output — any consumer that
  ever *displays* it must escape-strip exactly as the TUI's token path does. Nothing else moved:
  the `TokenEvent` sequence, the reply, and the recorded conversation are unchanged.
  (`internal/domain`, `internal/agent`, root facade.)
- **A live activity status line with an elapsed clock.** While a worker runs, the status line's
  left slot renders what is happening instead of `turn N`: `thinking` (a request in flight, or
  reasoning chunks arriving), `responding` (visible text streaming, keeping its `tok/s` suffix),
  `<verb> · <target>` for an open tool call, `retrying`, `compacting`, `stopping` — each with an
  elapsed clock that restarts only when the phrase itself changes, and prefixed with `sub-agent ·`
  at nesting depth > 0. Idle renders nothing at all. The whole left slot — spinner, phrase,
  clock, `tok/s` — hangs in the transcript's own text column (the two columns a `✦`/`❯` marker
  occupies), so it lines up with the body text above it instead of sitting flush left
  (`layout.md`); the indent is inside the width budget, so a narrow window still clips the line
  rather than wrapping it. The vocabulary lives in a new pure,
  table-tested `internal/tui/activity.go`: `foldActivity` derives the phrase from the same Event
  stream the transcript folds, and the handful of transitions no Event announces (a submit,
  `/continue`, `/compact`, an Esc stop, the worker's terminal message) set it directly; `stopping`
  is sticky until the worker actually unwinds. The per-tool active verb (`reading`, `editing`,
  `running python`, `delegating`, …) is one new column in the existing name-keyed presentation
  registry rather than a second parallel switch, so an unregistered dynamic MCP tool inherits the
  same `running <raw name>` fallback the transcript header already uses. No new `uiState` —
  `compacting` and `stopping` are activities, not lifecycle states (ADR 0011). (`internal/tui`.)

### Changed

- **The chat body now breaks two columns short of the scroll bar.** The transcript's text no
  longer wraps flush against whatever sits at its right edge: the body is rendered to a
  `bodyRightGutter`-narrower column than the viewport, so two columns stay free between the last
  painted glyph and the scroll bar, and three between the glyph and the window edge while the bar
  is blank — the mirror of the `bodyIndent` gutter on the left. The gutter is deliberately a
  constant rather than a function of whether the bar is currently painted: the scroll-bar column
  is reserved unconditionally, and the bar appears inside it the moment the content overflows, so
  a wrap width that tracked its visibility would re-wrap the whole visible transcript mid-reply.
  The viewport keeps its full width — only the content is narrower — so the sticky-to-top offset
  (`wrappedOffset`) and the mouse mapping still measure against the viewport, and the wrap width
  is floored at one column, so a window too narrow to hold the gutter still renders rather than
  going negative. (`internal/tui`, `layout.md`.)

### Removed

- **The `turn N` readout and the transcript turn counter behind it.** The status line's turn index
  is replaced by the activity phrase, and with its last reader gone the `turn` field on the TUI
  transcript and the eight assignments that maintained it are deleted — keeping a field alive for
  a test assertion is dead state, and the resumed turn index is already asserted from its
  authoritative source (`Resume`'s `TurnIndex`). Internal to `internal/tui`; no public-surface
  change. (`internal/tui`.)

## [1.4.0] — 2026-07-21

Post-`v1.3.0`, **additive** (minor) — three strands plus a TUI affordance. The **Validated-set
runtime surface** (ADR 0016): a per-model Mechanism set that passed the non-inferiority gate now
reaches users at startup, shipped with the binary (`internal/validated/shipped.json`) and
user-local (`~/.apogee/validated/`), whole-set-or-nothing and off under an explicit
`mechanisms:` block or Bypass. The **guided-decomposition hardening** (F1–F7): the fan-out's
Exchange scoping, marker handling, and remainder cursor are corrected, a deferred correction now
dies with its Exchange (F6), and `guided_decomposition + truncate_history` is refused at startup
as incompatible (F7). And the **architecture-deepening consolidations** (D1–D7, ADR 0017): the
Exchange boundary, the chars→token arithmetic, the history-scan shapes, the read/list tool-name
spelling families (F8), and the per-tool spec ritual each fold to one implementation. Plus `/new`
in the TUI, an alias of `/clear`. **No breaking change** (sanity-checked against the
`v1.3.0..HEAD` diff): the public facade (`apogee.go`) is untouched, and the types it aliases only
*gain* methods — `Budget.EstimateTokens` / `Budget.HistoryExceedsAllocation` (D4) and
`Conversation.ClearDeferred` / `TruncateDeferred` / `DeferredLen` (F6); nothing exported is
removed or re-typed. `domain.ExchangeView` (D1) stays internal per ADR 0017 §1.

### Added

- **`/new` — start a fresh conversation (alias of `/clear`).** The TUI chat mini-language now
  recognises `/new` as an alias of `/clear`: the parser accepts it as its own verb and `runCommand`
  routes it through the same synchronous context reset (`Engine.ClearContext`, staying idle, no
  worker), and the `/` autocomplete menu offers it. Purely additive — `/clear`'s behaviour is
  unchanged. (`internal/tui`.)
- **The Validated-set runtime surface (ADR 0016).** A per-model Mechanism set that passed the
  non-inferiority gate on a model now reaches users at startup: `cmd/apogee` matches the resolved
  model fingerprint against Validated-set entries — shipped with the binary
  (`internal/validated/shipped.json`, first entry: the gemma-4-e4b-it-qat pruned 16) and
  user-local (`~/.apogee/validated/*.json`, one entry per file, user wins a key collision) — and
  folds an applying set into `Config.EnableMechanisms` at wire time (the engine and bench arms are
  untouched; ADR 0015's single enable path stands). Semantics per the ADR's 2026-07-19
  realisation: auto-apply at ≥ medium fingerprint confidence; at low (name-only) confidence the
  per-session notice **offers** the set, applied only by the explicit `validated-sets: alias:`
  config (an identity alias is the confirm, a differing one the §3 transfer — consulted at any
  confidence); whole-set-or-nothing (a non-empty `mechanisms:` block or Bypass suppresses the
  apply; a defective entry — unknown ID, invalid stacking, malformed file — is skipped with a
  warning, never partially applied, never a blocked startup); a dangling alias is a loud startup
  error. New config block `validated-sets:` (`enable` off-switch, default on; `alias` map),
  file-only. New package `internal/validated`; shipped entries are pinned against the live
  catalogue by test. (`internal/validated`, `cmd/apogee`.)
- **ADR 0017 — the Exchange is a derived domain working value.** Ratifies the architecture
  deepening plan's D1–D3 (docs only; the code lands in that plan's items 3–4): the Exchange
  boundary is derived from the conversation — the messages strictly after the last `RoleUser`
  message, stable across injections — as an `internal/domain` `ExchangeView` working value shared
  by the loop and the hooks; the engine's cached `exchangeStart` and its S2 repair arithmetic are
  to be replaced by that derivation (`inExchange` stays; snapshot `ExchangeStart` becomes
  ignored-on-read, old snapshots stay resumable); Exchange end concentrates into one engine-side
  `closeExchange` owner of the F6 "a deferral dies with its Exchange" invariant; `ExchangeView`
  stays unexported at the root until an external consumer exists. CONTEXT.md's **Exchange** entry
  now names the code home. (`docs/adr/`, `CONTEXT.md`.)
- **`internal/domain/domaintest` — the hook seam's second adapter (test support, D6).** A fluent
  `ConversationBuilder` returning `[]domain.Message`, canned message/tool-call constructors
  (including the `read_file` call shape the read-counting Mechanisms inspect), and a settable
  `FakeLoopView` implementing `domain.LoopView` (zero value usable; its conversation view is built
  through the domain's own engine seam, so the pairing helpers cannot drift from production). The
  four package-shared `internal/mechanisms` fixture helpers (`readCall`/`userMsg`/`assistantText`/
  `assistantCall`) are now thin delegates — signatures unchanged, no test rewrites. Internal test
  support only; no public-surface change. (`internal/domain/domaintest`, `internal/mechanisms`.)
- **`domain.ExchangeView` — the Exchange boundary derived in one place (ADR 0017 §1, D1).** A new
  working value plus `CurrentExchange` constructor over the minimal unexported `Len()/At(i)` read
  surface (satisfied by both `*Conversation` and the hooks' `conversationView`): `Found`,
  `UserIndex`, `After` (copies), `RangeAfter` (allocation-free). The derivation — the current
  Exchange is the messages strictly after the last `RoleUser` message — now has exactly one
  implementation (`lastRoleIndex`); `InjectContext` and `conversationView.LastUser` route through
  it via `lastIndex`, public behaviour unchanged. No callers yet (the engine and Mechanisms follow
  in the deepening plan's items 4–5); not exported at the root. Internal only; no public-surface
  change. (`internal/domain`.)
- **`Budget.EstimateTokens` and `Budget.HistoryExceedsAllocation` — one token arithmetic (D4).**
  The chars→token conversion now has a single implementation: two methods on the `Budget` struct
  (root-aliased, so the public surface gains them — **additive → minor**). `EstimateTokens(chars)`
  rounds up (the estimator's calibrated ground truth) and yields 0 on a non-positive
  `CharsPerToken`, keeping token-gated behaviour inert until calibration;
  `HistoryExceedsAllocation(msgs)` is the single compare behind both the engine's auto-Compaction
  trigger (`Agent.historyExceedsAllocation`) and any hook reading the Budget, so the two can never
  disagree. The pure character measure `PromptChars` moved to `internal/domain` (ADR 0010's
  lowest-layer rule; `internal/context` keeps a thin delegate), `context.TokenEstimator` keeps its
  calibration and default-ratio fallback and delegates the rounding, guided decomposition's signal
  thresholds and the Library's injection-budget cap estimate through the shared method (the D4
  authorized delta: truncation → ceil, at most one token per comparison; no fixtures were
  boundary-exact), and `capMaxChars` stays the documented tokens→chars inverse. (`internal/domain`,
  `internal/context`, `internal/agent`, `internal/mechanisms`.)

### Fixed

- **Guided decomposition accepts only majority-marked enumerations.** The case-1 intercept now
  treats a steered reply as an enumeration only when the parsed list is in-bounds (2..12) **and** a
  strict majority of its lines carried an explicit ordered/bullet marker (F4). A compliant numbered
  list still fans out; multi-line prose, a clarifying question, a refusal, or an empty reply is
  declined whole. Unmarked lines are still kept as subtasks when the majority test passes
  (small-model tolerance). `guidedDecompositionStripMarker` now reports whether a marker was present.
  (`internal/mechanisms`.)
- **Guided decomposition anchors the remainder cursor on the delegation-bearing enumeration.** The
  cursor now scans only the current Exchange (the messages after the last user message) and anchors on
  the first assistant message that **both** parses as an in-bounds subtask list **and** carries a
  `sub_agent` call (F3) — the pair that uniquely identifies the real enumeration. A prior-Exchange
  answer, mid-Exchange narration, or a compaction summary can no longer shadow it, and a previous
  Exchange's fan-out cannot consume the current one's items or resume across the boundary.
  (`internal/mechanisms`.)
- **Guided decomposition consumes dispatched subtasks by exact match, once each.** The remainder
  cursor now removes an enumeration item only when a dispatched `sub_agent` task equals the item
  itself or the item plus the appended report-hygiene ask, and each dispatched task consumes at most
  one item occurrence. Duplicate items each need their own dispatch, and dispatching a longer
  prefix-nested item ("Add tests for the CLI") no longer also drops a shorter one ("Add tests"). An
  off-script task matching no item still leaves the remainder intact (§5). (`internal/mechanisms`.)
- **Guided decomposition re-defers the directive across off-script tool Turns.** When a directive is
  steering and the model answers a mid-fan-out Turn with a tool call other than `sub_agent`, the
  post-response intercept now re-defers the remaining-items directive (with the remainder intact)
  instead of letting it drain away — one off-script tool call no longer silently drops the fan-out
  queue (F2). A no-tool final answer still ends the Exchange and is never re-deferred.
  (`internal/mechanisms`.)
- **Guided decomposition steers at most once per Exchange.** The pre-request gate now stays quiet for
  the rest of an Exchange once a fan-out has begun in it, judged from committed history — any
  assistant message after the last user ask that carries a `sub_agent` call (F1). This stops the gate
  re-steering on the synthesis Turn (where the request-scoped steer/directive markers have drained but
  signal B still reads oversized), which had looped the decomposition; a model that delegated
  unprompted this Exchange is likewise left alone. A new user ask re-arms the gate. The marker-based
  check remains as the same-request double-steer guard. (`internal/mechanisms`.)
- **Guided decomposition markers are line-anchored and role-scoped.** The steer/directive marker scan
  now matches only in `RoleUser` and `RoleSystem` messages (the only places the loop injects) and only
  where the marker starts a line (F5). An assistant echo of the phrase, a tool result, or `@file`-style
  user content carrying it mid-line no longer counts as an outstanding steer or a fan-out directive; the
  real injected steer and drained directive (marker at a line start) still do. The marker strings are
  unchanged (the loop-level tests' wire contract). (`internal/mechanisms`; ADR 0014 Realisation addendum.)
- **Deferred Response Actions expire at the Exchange boundary.** A deferred correction (an
  `ActionDefer` decision, e.g. a guided-decomposition remaining-items directive) is now cleared
  whenever an Exchange ends — a completed final answer, a terminal fault (`abandonTurn`), or an
  `AbortExchange` — so a stale fan-out directive can no longer survive a fault or abort into the next
  Exchange (F6). A cancelled Turn now truncates the queue back to its pre-hooks floor before restoring
  the drained injections, so a re-attempt or snapshot carries exactly the one restored directive
  rather than two contradictory copies. `domain.Conversation` gains `ClearDeferred`, `TruncateDeferred`,
  and `DeferredLen`. (`internal/agent`, `internal/domain`; CONTEXT.md.)
- **Guided decomposition is incompatible with `truncate_history`.** The `guided_decomposition`
  descriptor now declares `IncompatibleWith: [decompose, truncate_history]` (F7): a mid-Exchange
  truncation longer than its keep window can drop the enumeration message the fan-out cursor
  re-derives the remainder from, destroying the fan-out mid-flight. Co-enabling the two is refused at
  startup with `ErrIncompatibleMechanisms`; the valid `guided_decomposition + tool_result_cap` stack is
  unaffected. (`internal/mechanisms`; `docs/design/mechanism-catalogue.md`, ADR 0014 Realisation.)

### Changed

- **Single-sourced read/list tool-name spelling families.** The read trio
  (`read_file`/`readFile`/`open_file`) and the five list spellings
  (`list_files`/`listFiles`/`list_dir`/`listDir`/`list_directory`) are now hoisted as two spelling
  families beside the write side's `wave4WriteTools`, and every read/list set composes from them
  instead of hand-copying (F8) — closing the drift class that had shipped defects in two prior review
  rounds. Each set keeps its own documented membership; search/exec spellings stay out of scope. Four
  diverged sets are corrected as part of the composition (each a behaviour change with a mutation-pinned
  test): `cotReadOnlyTools` now counts `list_directory`, `libraryListTools` and `toolFilterAnalysisKeep`
  now carry `listFiles`/`listDir`, and `fileHintListTools` now carries `listDir`. Three stale wiring
  comments that still pointed at `cmd/apogee/wire.go` are repointed at the engine's single build path
  (`buildEnabledMechanisms`, `internal/agent/loop.go`) after the ADR 0015 wire.go collapse.
  (`internal/mechanisms`.)
- **Exchange end has one engine-side owner; the rollback boundary reads through one seam (ADR
  0017 §§2–3).** The three Exchange-end sites (`completeTurn`'s exchange-complete branch,
  `abandonTurn`, `AbortExchange`) now route through one private `closeExchange` owning the F6
  "a deferral dies with its Exchange" invariant (`cancelTurn` stays distinct by design — the
  Exchange remains open there). The planned deletion of the cached `exchangeStart` did NOT land:
  item-4 verification showed a mid-Exchange `truncate_history` fold can drop the open Exchange's
  opening user message (the gap note would anchor the derivation and be over-dropped on abort —
  pinned by `TestExchangeStartRepairedAfterMidExchangeTruncation`), so per the ADR's recorded
  fallback the cache and its S2 repair stay, with all readers routed through the one
  `exchangeBoundary()` helper and the snapshot's `exchangeStart` still round-tripping (newly
  pinned by `TestSnapshot_RoundTripsExchangeBoundaryForAbort`). Behaviour-preserving; internal
  only, no public-surface change. (`internal/agent`; ADR 0017 + CONTEXT.md record the fallback.)
- **Guided decomposition reads the Exchange through the domain seam (ADR 0017 §1).** The
  Mechanism's three current-Exchange scans — the F1 fan-out-begun check, the enumeration anchor,
  and the dispatched-task window — now derive the boundary via `domain.CurrentExchange`, routed
  through the one `guidedDecompositionCurrentExchangeStart` accessor; the Mechanism-local
  "last `RoleUser`" derivation (`conv.LastUser()`) is deleted. Marker handling, list parsing, and
  every threshold are unchanged, and the whole suite passes unchanged; a new drift pin
  (`TestGuidedDecompositionAgreesWithDomainCurrentExchange`) asserts the cursor helpers agree with
  the seam's own output on a two-Exchange history. Behaviour-preserving; internal only, no
  public-surface change. (`internal/mechanisms`.)
- **One copy of each shared history-scan shape, beside the spelling families (D5).** The
  hand-rolled `conv.Range(...)` walks the history-inspecting Mechanisms each carried now live once
  in `internal/mechanisms/historyscan.go`, composing with the F8 spelling families: read-attempt
  path counting with successes and failures separate (`readAttemptCounts` — readloop's two
  detectors shrink to threshold-plus-sort wrappers), successful-read paths over the latest read
  episode (`recentSuccessfulReadPaths` — readrepeat's private scan deleted), and written paths
  since an index (`writtenPathsSince` — `writtenPaths` is now a thin delegate over the whole
  conversation). filehint's private copies are deleted onto the already-shared helpers: its
  duplicate write set and write scan (`fileHintWriteTools`/`fileHintHasWrittenFiles`) fold into
  `hasWrittenFiles` over `wave4WriteTools`, and its marker scan (`fileHintAlreadyInjected`) into
  `requestContains`. Per-Mechanism membership and thresholds stay at the call sites (D5);
  readloop's `isGreenfieldContext` stays local as a composite write/read/list early-exit scan no
  shared shape expresses. The three Mechanisms' suites pass unchanged (the behaviour contract);
  the helpers gain their own table-driven suite over the family spellings via `domaintest`.
  Behaviour-preserving; internal only, no public-surface change. (`internal/mechanisms`.)
- **Embedded tool spec and typed arg decoding fold the per-tool ritual (D7).** Each of the 20
  built-in tools hand-rolled the same shape: a package-var schema string, three metadata methods
  (`Name`/`Description`/`Schema`), and a decode-and-error preamble in `Execute`. The identity now
  lives in one embeddable `toolSpec` value per tool (name + description + the raw JSON schema
  string, still visible and reviewable — no schema generation, D7/ADR 0002) providing the three
  methods via embedding, and the preamble folds into one generic `decodeToolArgs[A]` helper
  wrapping `decodeArgs`, so the standard "invalid arguments: …" result is built in exactly one
  place (all 20 sites already shared that wording verbatim — nothing begged unification).
  `internal/mcp`'s `serverTool` is untouched: it does not share the ritual (its identity arrives
  per-server at runtime, its description carries a fallback, and its arguments pass through raw).
  Tool names, schemas, results, and error strings are unchanged — the whole `internal/tools`
  suite passes untouched, plus a new test pinning that a tool built from a spec reports exactly
  the spec's name/description/schema bytes. Behaviour-preserving; internal only, no
  public-surface change. (`internal/tools`.)

### Tested

- **Sub-agent spawn under the production `EnableMechanisms` arm.** New loop-level tests arm the
  `guided_decomposition + tool_result_cap` stack by ID with `Config.Mechanisms` left nil (the engine
  builds it), drive one real delegation, and prove a spawned sub-agent inherits the parent's
  already-built registry (ADR 0015): the spawn succeeds, the child nests at `Depth == 1`, and the
  child fires a catalogued Mechanism (`tool_result_cap`) from the inherited stack — through both `New`
  and `Resume`. Reverting subagent.go's `EnableMechanisms` clear makes them fail with the
  already-registered rejection. (`internal/agent`.)
- **Corrupt-store degrade and the descriptor clone contract.** A new loop-level test seeds
  `LibraryDir/library.json` with garbage bytes and arms `EnableMechanisms=["library"]`: construction
  still succeeds, the build path emits the degrade notice to `os.Stderr` exactly once, and the armed
  library Mechanism runs over the resulting empty store — with the model fingerprint forced
  high-confidence (a reachable weight file) so the empty store, not the confidence gate, is the sole
  barrier, proving no injection leaks from the corrupt content. A root test mutates a returned
  `CataloguedMechanisms()` descriptor's `Requires` / `IncompatibleWith` slices and re-queries, pinning
  the documented per-call clone contract (ADR 0015 §3). (`internal/agent`, root.)

## [1.3.0] — 2026-07-05

Post-`v1.2.0`, **additive** (minor) — two features. The `guided_decomposition` Mechanism
(ADR 0014), built up item-by-item behind the Mechanism catalogue and shipped **default-off**
(the bench flips it on, not this work). And the public Mechanism enable surface (ADR 0015) —
the 2026-07-05 handoff's path (b): an external module (apogee-sim, the ADR 0001 consumer) can
now arm any catalogued Mechanism stack in-process by ID. **No breaking change**
(sanity-checked against the `v1.2.0..HEAD` diff): the public facade (`apogee.go`) only
*gains* symbols — `Config.EnableMechanisms`, `CataloguedMechanisms()`, the
`MechanismDescriptor` / `Capability` / `SuppressionPolicy` aliases with their constant
values, and the `ErrMissingRequirement` / `ErrUnknownMechanism` re-exports; nothing exported
is removed or re-typed.

### Guided decomposition (`guided_decomposition`, default-off)

- **The `Requires` stacking relation.** `MechanismDescriptor` gains a `Requires []MechanismID`
  field — the dual of `IncompatibleWith` — and `New` now runs the new
  `MechanismRegistry.ValidateRequirements` gate alongside the ordering and incompatibility
  checks: every registered Mechanism's required peers must also be registered, else the new
  `ErrMissingRequirement` sentinel refuses construction ("X requires Y — enable both or neither;
  they are benched as a stack"), the same startup-gate posture as `ErrOrderingCycle`. It is
  enable-time only (ADR 0014 §4): live suppression of a required peer mid-Session is not
  re-checked. (`internal/domain`, `internal/agent`.)
- **Hook-visible loop depth and post-response tool-call synthesis.** `LoopView` gains a
  `Depth()` method — 0 for a top-level Agent, parent+1 for a sub-agent (ADR 0013) — so a gate
  can steer only the primary call, never a nested delegation (ADR 0014 §5); the loop stamps it
  from the Agent's nesting level through the new engine-seam `Request.SetDepth`. `Response`
  gains `AppendToolCall`, letting a post-response Mechanism add a `sub_agent` delegation the
  model never emitted: the loop reads it back through `ToolCalls()`, commits it on the assistant
  message, and dispatches it through the full per-call Resolution (the ADR 0013 recursion point,
  driving a real nested child) exactly like a model-emitted call. An in-place response mutation
  combined with a returned `ActionDefer` both take effect. (`internal/domain`, `internal/agent`.)
- **The `guided_decomposition` gate and enumeration steer (pre-request half).** The new
  `guided_decomposition` Mechanism (`internal/mechanisms`, catalogue-registered, ordered `After
  toolfilter`) lands its pre-request half: a strikes-3 proactive-nudge, `IncompatibleWith`
  `decompose` and `Requires` `tool_result_cap`. On an oversized PRIMARY call — a known window,
  top-level depth, a `sub_agent` on the final menu, and a measured size signal (a fresh user message
  over the `FileContext` budget, or mid-Exchange history over the `History` budget with the model
  still calling tools) — it injects an enumeration steer asking for ONLY a numbered list of at most 7
  self-contained subtasks. The steer is marker-idempotent and stays quiet once a fan-out directive is
  already steering (no double-steer); the post-response intercept and serialized follow-through land
  next. (`internal/mechanisms`.)
- **The intercept and serialized follow-through (post-response half).** `guided_decomposition` gains
  its `PostResponse` half on the same struct. On the enumeration Turn — the steer outstanding, the
  model's reply a bounded (2..12) subtask list with no tool calls — it parses the list, synthesizes
  the FIRST `sub_agent` delegation onto the response (the enumeration text left verbatim), and defers
  a remaining-items directive carrying the rest plus a compact-report hygiene ask (ADR 0014 §4). Each
  following Turn re-derives the remainder from honest history — the model's own list message and the
  `sub_agent` CALLS in the conversation (never the child results, so a report capped by
  `tool_result_cap` leaves the cursor exact) — minus the just-delegated task, and re-defers the
  shrunken directive until none remain. It carries no per-Mechanism state (snapshot/resume-safe and
  suppression-clean), declines an out-of-bounds list whole, and no-ops on anything else.
  (`internal/mechanisms`.)
- **Wire-up proof and end-to-end fan-out acceptance.** Loop-level tests drive the whole stack through
  the REAL loop with nothing of the Mechanism mocked: an oversized primary call gets the enumeration
  steer, the model's list is intercepted into a REAL nested `sub_agent` fan-out serialized one
  delegation per Turn (child events nesting at `Depth == 1`), the remaining-items directive rides each
  following request and shrinks, and the Exchange ends on a no-tool synthesis with the enumeration
  verbatim and all three child reports in honest history. A snapshot taken mid-fan-out round-trips the
  pending directive (`conversationJSON.Deferred`) and a resumed Agent completes the fan-out; Bypass is
  the silent control arm (ADR 0014 §1); and a cancel during a child rolls back only that parent Turn
  (ADR 0013 §5). Config-surface tests pin the ADR 0014 §4 stacking gates: enabling `guided_decomposition`
  without `tool_result_cap` is the `ErrMissingRequirement` startup error, the stack boots, and adding
  `decompose` is the incompatibility error. The commented `mechanisms:` example in the config template
  gains the stack. (`internal/agent`, `cmd/apogee`.)
- **Docs close-out.** The feature's cross-cutting doc edits are reconciled under this one heading:
  CONTEXT.md's Guided decomposition entry now disambiguates it from the shipped `decompose` Mechanism
  (a prompt-shaping nudge — steers wording, not delegation; the two are declared incompatible), and
  ADR 0014 gains a dated Realisation note recording the decisions locked at implementation — queue
  delivery as one re-derived deferred directive per Turn, `IncompatibleWith: [decompose]`,
  registry-level `Requires` validation, verbatim enumeration text, and the 7/12 subtask bounds — plus
  the authorized per-item deviations. (Docs: `CONTEXT.md`, `docs/adr/0014`.)

### The public Mechanism enable surface (`Config.EnableMechanisms`, ADR 0015)

- **Catalogued descriptors become static, queryable data + a matchable unknown-ID sentinel.** Each
  catalogued Mechanism's `MechanismDescriptor` is now a single package-level value that both the
  built instance's `Descriptor()` returns and the catalogue registers beside the constructor
  (equality by construction), and a new `mechanisms.Descriptors()` returns every row — sorted by ID,
  duplicate-free, slice fields cloned — so a Mechanism's metadata is available without building one
  (the backing for the forthcoming public `CataloguedMechanisms()` query, ADR 0015 §3). A new
  `domain.ErrUnknownMechanism` sentinel is wrapped by `mechanisms.Build`'s unknown-ID error (which
  still names the known IDs), so a typo'd or deferred ID fails loudly AND matchably via `errors.Is`
  (ADR 0015 §4). (`internal/mechanisms`, `internal/domain`.)
- **`Config.EnableMechanisms` arms catalogued Mechanisms by ID at construction.** `Config` gains an
  `EnableMechanisms []MechanismID` field: `New` and `Resume` build each named catalogued Mechanism
  and merge it INTO `Config.Mechanisms` (a fresh registry when nil), so catalogued Mechanisms and
  bench experimental hooks coexist in one arm (ADR 0015 §1–2). The engine derives the build `Deps`
  the way `cmd/apogee/wire.go` does — a Library store rooted at `Config.LibraryDir` and Loaded only
  when `library` is enabled (never an ambient root; a corrupt/absent store degrades to empty and
  never blocks construction), the model fingerprint resolved once, and the grammar seam left inert —
  entirely internal (no `Deps` type on the public surface). IDs build in sorted order for a
  deterministic error surface, then the existing ordering/incompatibility/requirements gates run over
  the merged registry unchanged: an unknown ID (`ErrUnknownMechanism`), an ID listed twice or already
  pre-built (the already-registered rejection), and a half-armed `Requires` stack
  (`ErrMissingRequirement`) each fail `New`/`Resume`; an empty/nil list arms nothing (default-off).
  A spawned sub-agent inherits the parent's already-built registry, so it fires the same Mechanisms
  without re-building them. `cmd/apogee`'s own YAML→registry path is unchanged for now (it collapses
  onto this engine path in a follow-up). (`internal/domain`, `internal/agent`.)
- **`cmd/apogee` collapses to a YAML→ID-list producer.** `cmd/apogee/wire.go` no longer builds a
  registry: `buildMechanismRegistry` and the cmd-side `Deps` derivation (the Library store /
  fingerprint / `LookPath` wiring, now dead) are deleted. The composition root still validates EVERY
  `mechanisms:` key — enabled AND disabled — against the known catalogue at the startup boundary (a
  typo'd DISABLED key, which the engine never sees, must still fail loudly there), then hands the
  sorted enabled IDs to `Config.EnableMechanisms` and lets `New`/`Resume` build them (ADR 0015 §1).
  The YAML `mechanisms:` surface, the config template, and every user-visible behaviour are unchanged
  — the same loud errors refuse to boot at the same startup boundary (unknown key, half-stack,
  incompatibility), only the `%w` chain behind some of them moved from the cmd path onto the engine
  path. (`cmd/apogee`.)
- **The public Mechanism surface: descriptors, catalogue query, and matchable enable errors.** The
  root facade now exposes the enable surface an embedder needs: `MechanismDescriptor`, `Capability`,
  and `SuppressionPolicy` (with their constant values) are re-exported, and a new
  `apogee.CataloguedMechanisms()` returns every catalogued Mechanism's descriptor — sorted by ID,
  duplicate-free, slice fields cloned — so a host can read each Mechanism's Capability, suppression
  policy, and `IncompatibleWith` / `Requires` stacking relations and plan an `EnableMechanisms` arm
  (e.g. a leave-one-out arm by `Requires` traversal) WITHOUT building any Mechanism (ADR 0015 §3). The
  enable-time sentinels `ErrMissingRequirement` (the dual of `ErrIncompatibleMechanisms`) and
  `ErrUnknownMechanism` are re-exported so `errors.Is` matches them through the root (ADR 0015 §4).
  `Config.Mechanisms`' doc comment is reframed as the experimental-hook carrier that points at
  `EnableMechanisms` for catalogued enablement (the field keeps its name under v1 semver — no
  rename). Runnable godoc Examples arm the `guided_decomposition + tool_result_cap` stack and compute
  a leave-one-out arm from the catalogue query. (`apogee.go`, `internal/domain`.)
- **The bench-readiness contract becomes a true external-surface consumer.** `benchreadiness_test.go`
  now arms every arm through the PUBLIC enable surface — catalogued Mechanisms by ID via
  `Config.EnableMechanisms`, experimental hooks via `AddExperimental` — and no longer imports
  `internal/mechanisms` or `internal/library` or builds the catalogue by hand, so a separate module
  (apogee-sim) can now do everything this test does (ADR 0015 Consequences). It adds the acceptance the
  bench campaign needs, all through the root API: a half-armed `Requires` stack refuses construction
  (`apogee.ErrMissingRequirement`), a bogus ID refuses (`apogee.ErrUnknownMechanism`), the
  catalogued+experimental combined arm still co-fires both in deterministic order, and a leave-one-out
  arm set computed from `apogee.CataloguedMechanisms()` — the full compatible stack and every
  member-omitted arm — constructs successfully. (`benchreadiness_test.go`.)
- **Docs close-out.** The enable surface's cross-cutting doc edits are reconciled under this one
  heading: ADR 0015 gains a dated Realisation note recording the authorized implementation
  deviation — a spawned sub-agent inherits the parent's already-built registry (clearing
  `EnableMechanisms`) rather than rebuilding, and a degraded Library store degrades to empty
  rather than failing construction — and the README's Configuration section now names the public
  library enable surface (`Config.EnableMechanisms` / `apogee.CataloguedMechanisms()`) alongside
  the unchanged `mechanisms:` YAML path. CONTEXT.md is unchanged — the grill crystallised no new
  term (ADR 0015 Consequences / locked decision 7). (Docs: `docs/adr/0015`, `README.md`.)

## [1.2.0] — 2026-07-04

Post-`v1.1.0`, **additive** (minor) — Phase 4 merges the apogee-sim Mechanisms into the
loop (`docs/plans/archived/phase-4-detail-plan.md`; ratified catalogue at
`docs/design/mechanism-catalogue.md`). **No breaking change** (sanity-checked against the
`v1.1.0..HEAD` diff): the public facade (`apogee.go`) only *gains* symbols — the sole new
top-level export is `ErrIncompatibleMechanisms`; nothing exported is removed or re-typed. Every
other new surface is additive — new `Config` fields (the `mechanisms:` block and the `auto-compact`
key) plus the now-consumed `LibraryDir` root (a pre-`v1.1.0` field Phase 4 finally reads, not a new
field), new advisory `domain.Budget` fields, and new `domain` types
(`ModelFingerprint`, `FingerprintResolver`) that are *not* re-exported at the root. The one
changed signature (`domain.NewRequest`'s fired-ledger argument) is an internal engine seam, never
on the public surface — so this is a **minor** bump, not a major one.

### Catalogued Mechanisms now dispatch in a deterministic order behind the Bypass gate

- **Registered Mechanisms finally run.** A Mechanism added to the `MechanismRegistry` via
  `Add` used to be validated but never dispatched — only the bench's experimental hooks
  fired. Now, at each of the five hook points, the loop dispatches the catalogued
  Mechanisms **first**, in a deterministic total order (`MechanismRegistry.Ordered` — a
  topological sort of each Mechanism's `Before`/`After` `OrderingConstraints` with a stable
  tiebreak by canonical `MechanismID`, so a shuffled registration order yields identical
  output, ADR 0003), then the experimental hooks in registration order (unchanged). Each
  fires under the same recover boundary and emits a `MechanismFiredEvent` under its **real**
  `MechanismID` (experimental hooks keep the synthetic `experimental` attribution).
- **`Config.Bypass` now gates dispatch (ADR 0006).** Under Bypass, every catalogued
  non-`off-ramp` Mechanism is skipped — proactive-nudge and response-repair go silent —
  while `off-ramp` recovery guarantees still run; experimental hooks are never Bypass-gated
  (they are the bench's own instruments), and the structural context machinery (Budget,
  Compaction) is unaffected.
- **Incompatible Mechanisms fail loudly at construction.** `New` now also runs
  `MechanismRegistry.ValidateIncompatibilities`, returning the new
  `ErrIncompatibleMechanisms` sentinel when two registered Mechanisms declare each other via
  `MechanismDescriptor.IncompatibleWith` — the same startup-gate posture as
  `ErrOrderingCycle`, so a config that enables two mutually-exclusive Mechanisms is refused
  rather than silently running both. (`internal/domain`, `internal/agent`, root re-exports.)

### Mechanisms now self-regulate: effectiveness tracking, Adaptive Suppression, the Turn Budget

- **A catalogued Mechanism that is not helping is now withdrawn for the rest of the
  Session.** A per-Session tracker judges each Turn on proxy signals — a Turn is
  **productive** when it reads a new file or writes one (a tool error or an empty/no-op
  response is not). **Adaptive Suppression** (per Mechanism): a Mechanism that fires through
  three consecutive non-productive Turns is skipped at dispatch for the rest of the Session,
  with a clear-path that re-opens every Mechanism on the next productive Turn. **The Turn
  Budget** (global): after eight consecutive non-productive Turns every non-exempt Mechanism
  is withdrawn until productive activity resumes. A `SuppressionPolicy: exempt` off-ramp
  bypasses both — suppressing it would leave a failed Turn with no way out (ADR 0006).
- **`LoopView.Fired` finally answers.** The declared-but-inert per-Session fire counter now
  reports real fires, read live within a hook pass (a Mechanism sees a peer's fire from
  earlier in the same pass — the cross-Mechanism coupling seam). No new public surface: the
  tracker is internal to `internal/agent`; `domain.NewRequest` gains a `fired` ledger
  argument on the engine seam only.
- **Reset on Resume.** The tracker is per-Session and not serialized: a resumed Agent starts
  with clean suppression state (the accepted v1 posture — fresh state can only cause a
  withdrawn Mechanism to be re-tried, never wrongly withheld). (`internal/agent`,
  `internal/domain`.)

### A file-only `mechanisms:` config block wires the catalogue into the loop

- **Catalogued Mechanisms are now opt-in from `config.yaml`.** A new file-only `mechanisms:`
  block (no flag/env, like `mcp-servers` / `model-profile`) maps a canonical mechanism ID to
  `enabled: true|false`. Every Mechanism defaults **off** (D1 — default-off until bench-proven);
  a `true` entry turns one on. An **unknown ID is a loud startup error** listing the catalogue
  this build knows, so a typo'd key never silently disables a Mechanism. `--bypass` still wins:
  an enabled non-off-ramp Mechanism is not dispatched under bypass (ADR 0006 / the item-2 gate).
- **The catalogue constructor seam.** `internal/mechanisms` gains `Build(id, deps)` over a
  constructor table (`Deps` carries the construction-injected collaborators — D3; the Library
  store is nil until it lands). The composition root (`cmd/apogee`) drives the table for each
  enabled ID and folds the built Mechanisms into `Config.Mechanisms` before construction. The
  table ships **empty** — the port waves fill one row per Mechanism — so a config with no
  `mechanisms:` block behaves exactly as before. (`cmd/apogee`, `internal/mechanisms`, README +
  starter `config.yaml`.)

### Wave 1: the `validate` / `syntax` / `autofix` response-robustness Mechanisms

- **The measured-win response cascade is ported.** Three post-response Mechanisms — dispatched in
  the deterministic order `validate` → `autofix` → `syntax` (catalogue Table A as amended by the
  reorder entry below; originally shipped `validate` → `syntax` → `autofix`) — now ship in the
  `internal/mechanisms` catalogue (default **off**, D1). `validate` checks each requested tool call
  against the tool menu the model was shown and its own arguments (unknown tool name, empty/malformed
  JSON, missing required parameter); `syntax` checks a file-writing call's content (Go through the
  real parser, other languages through a bracket/string/truncation heuristic); `autofix` repairs
  syntax-broken write content and writes the improved payload back to the call the loop will dispatch.
- **Corrections retry in place (amended C5 — R1; superseding this entry's original ActionDefer
  delivery).** `validate`/`syntax` return `ActionRetry` with the sim's correction message — the
  loop re-streams the corrected request in the same Turn (see the delivery-switch entry below).
  `autofix` intercepts in place via `Response.SetToolCallArguments`, which is effective because a
  Response's tool calls are dispatched only after post-response review.
- **`gofmt` is always in-process; other formatters are construction-probed and gracefully absent
  (superseding this entry's original fire-time PATH-gating — see the autofix entry below).** Go is
  formatted with the standard library's `go/format` — no external dependency — with `goimports`
  preferred when found; `black` / `prettier` / `rustfmt` repair only when their executable was
  resolved at construction, and a formatter's absence, failure, or timeout leaves the payload
  untouched (standing requirement #2). What no formatter can improve is left for `syntax` to
  correct. (`internal/mechanisms`.)

### Wave 1: the `empty_response_recovery` / `tool_use_enforcer` off-ramps

- **The two recovery guarantees are ported (catalogue Table A).** Both are post-response Mechanisms
  with Capability **off-ramp** and SuppressionPolicy **exempt**, so they run even under Bypass (D5)
  and are never withdrawn by Adaptive Suppression or the Turn Budget — without them a failed Turn has
  no way out (CONTEXT "Off-ramp"). They ship in the `internal/mechanisms` catalogue, default **off**
  (D1). `empty_response_recovery` fires when the model returns nothing — no text and no tool call —
  mid-task with tools available and recent progress; `tool_use_enforcer` fires when the user asked for
  an action but the model answered with prose twice running, having never used a tool (the sim's
  intent classifier, folded in inline per catalogue C6).
- **Empty replies and narration both retry in place (amended C5 — R1; superseding this entry's
  original retry/defer split).** `empty_response_recovery` returns `ActionRetry` carrying the sim's
  first-attempt completion-check nudge verbatim; `tool_use_enforcer` returns `ActionRetry` with the
  sim's "use a tool" correction, the retried request carrying the superseded narration (the sim's
  `retryForToolUse` exchange). Both stay bounded by the loop's existing `maxPostResponseRetries`
  cap so an always-empty model still terminates. (`internal/mechanisms`.)

### ActionRetry now carries the corrective exchange onto the retried request

- **A post-response retry delivers its correction in the same Turn (R1, amending catalogue
  C5).** `PostResponseDecision.Inject` now rides `ActionRetry` too: when a post-response
  Mechanism retries with a correction, the loop appends the superseded assistant message
  (text + tool calls, when non-empty) and then the correction as a role-safe user message
  to the in-flight request before re-streaming — request-scoped, never committed to history
  — mirroring apogee-sim's own retry builders. Corrections accumulate across attempts (the
  sim's escalating re-asks), bounded by the existing `maxPostResponseRetries` cap; at the
  cap the last response passes through. An `Inject`-less retry stays a bare re-stream, and
  `ActionDefer` keeps its next-request semantics unchanged. (`internal/domain`,
  `internal/agent`.)
- **The retry appendage is hidden from post-response scanners (second-review fix, sim
  parity).** On a retry cycle the request-scoped superseded attempt + correction no longer
  masquerade as committed history to the history-aware Mechanisms: `Request.View()` is now
  bounded to the length frozen at the first `AppendSupersededAssistant`, so `read_repeat`
  never counts a never-executed superseded read as already-read and `tool_loop_interceptor`
  compares the retried response against the last **committed** turn, not the superseded
  attempt. The appendage still reaches the model through `Request.State()` — only the
  mechanism view changes — matching apogee-sim, whose retry builders ran their detectors
  against the unmutated request. (`internal/domain`, `internal/agent`.)
- **The retry-view boundary now survives an empty superseded response (third-review fix).**
  When a retried response is wholly empty, nothing is appended, so the correction lands
  *below* the frozen `committedLen` rather than after it — and the boundary was static, so
  `Request.View()` evicted the real user ask (the insert-before-last-user shape) or the newest
  tool result (the system-prepend shape) from the post-response scanners. `committedLen` is now
  MAINTAINED, not just frozen: a below-boundary `InjectContext` insert and an
  `appendOrCreateSystem` prepend each advance it, so `View()` stays pinned to the same committed
  history. `Request.State()` (the model-facing projection) is byte-identical — the correction
  still reaches the model unchanged; only the mechanism view is corrected. (`internal/domain`;
  tests in `internal/agent`.)

### Wave 1 rides the retry seam: corrections deliver in the same Turn

- **The four shipped Mechanisms switch `ActionDefer` → `ActionRetry` (amended C5, R1).**
  `validate` and `syntax` now short-circuit the response-repair cascade on a failing call —
  the correction re-streams the corrected request in the same Turn instead of waiting for the
  next request — so the catalogue's "short-circuits cascade on fail" holds for real.
  `tool_use_enforcer` re-calls in-cycle exactly like the sim's `retryForToolUse`: the retried
  request carries the superseded narration plus the "use a tool" correction, fixing the review
  finding that the correction sat until the next user Submit. `empty_response_recovery`
  upgrades its bare re-stream to carry the sim's first-attempt completion-check nudge verbatim
  (`empty_recovery.go` @pin); the attempt-2 nudge ladder, system directive, and temperature
  escalation stay recorded bench-pending divergences (R2). Everything remains bounded by
  `maxPostResponseRetries` — an always-empty model terminates, its final reply passing through.
  Proven loop-level through the scripted-responder harness, including both off-ramps firing at
  dispatch (registry-built) under Bypass and through a tripped Turn Budget.
  (`internal/mechanisms`; tests in `internal/agent`.)

### autofix repairs like the sim: construction-probed formatters, issue-count gating, repair-before-correct

- **The formatter table is resolved once at construction (D3).** `mechanisms.Deps` gains
  `LookPath` (nil ⇒ `exec.LookPath`); `newAutofix` probes goimports/black/prettier/rustfmt
  through it exactly once and caches the resolved paths — the sim's LookPath-cached formatter
  table — so a fire never touches PATH. The package-var-at-fire-time probe is deleted, and
  `cmd/apogee` wires the production `exec.LookPath`.
- **Repair only, gated on improvement.** autofix now acts only on syntax-broken write content
  and keeps a formatter's output only when it *reduces* the `checkSyntax` issue count (the
  sim's `AttemptFix` gate) — clean content is never beautified, and a "fix" that fixes nothing
  is discarded. The sim's `sanitizePath` NUL/CR/LF guard is restored alongside the kept `-`
  prefix hardening on formatter argv.
- **Cascade reorder: `validate` → `autofix` → `syntax`.** The sim runs detect → `tryAutoFix` →
  correct-the-remainder (`response_analysis.go:72-88` @pin), so repair now precedes the
  correction stage — `syntax`'s retry covers only what a formatter could not fix, ending the
  review's double-correction finding. Catalogue Table A and the post-response cascade section
  record the amendment. (`internal/mechanisms`, `cmd/apogee`.)

### Self-regulation judges the NEXT Turn on four proxy signals, and only acted fires count

- **Next-Turn judgment (R3).** Fires recorded in Turn N are now judged by Turn N+1's outcome —
  a Mechanism's intervention can only show up in what the model does next — instead of by the
  Turn they fired in. Each completed Turn is classified **three-way**: *productive* (a novel
  file read, or a successful write/action), *harmful* (a tool-result error, or an empty final
  response — both newly-recognized harmful signals; they used to merely be "not productive"),
  or *neutral* (neither — e.g. a substantive text-only answer), with productive winning when
  signals mix. Adaptive-Suppression strikes and the Turn-Budget streak advance **only on a
  harmful Turn**; a neutral Turn freezes both; a productive Turn stays the global clear-path.
  Consequence (the review's point): a pure Q&A session neither strikes Mechanisms nor trips
  the Turn Budget. A cancelled Turn's rollback now also restores the novelty credit of the
  reads it booked, so the mandated re-attempt is not penalized as a wasted re-read.
- **Fired means acted (R4).** A catalogued Mechanism is booked (`recordFire` +
  `MechanismFiredEvent` + the judgment set) only when its invocation **intervened**: it
  returned a non-zero post-response Action, or it mutated its working value —
  `Request`/`Response`/`Conversation` gain an internal revision counter with an engine-seam
  `Revision()` accessor, and the tool-stage hooks compare call/result snapshots. An
  inspect-and-do-nothing invocation is no longer a fire, matching apogee-sim's `FiredCounts`
  (interventions, not invocations); `LoopView.Fired` therefore counts actions. Experimental
  hooks keep the always-booked behaviour under the synthetic ID (bench observability).
- **The experimental sentinel ID is now reserved in domain (R5).** The `"experimental"`
  constant moves to `domain.ExperimentalMechanismID`, and `MechanismRegistry.Add` refuses a
  catalogued Mechanism claiming it — a real Mechanism can no longer masquerade as the bench's
  own instrument. (`internal/agent`, `internal/domain`.)

### Registry + config hardening: duplicate IDs refused, every `mechanisms:` key validated

- **`MechanismRegistry.Add` refuses a duplicate `MechanismID`.** Two Mechanisms registered
  under the same ID used to pass `Add` and be silently collapsed to one by the dispatch
  order's ID map; the second `Add` is now a loud error naming the ID — the same startup-gate
  posture as the reserved-sentinel refusal above.
- **A typo'd `mechanisms:` key now fails startup even when mapped to `false`.** README and
  the starter `config.yaml` always promised a loud unknown-ID error, but only *enabled* keys
  were checked (through the build path) — a misspelled `false` entry was silently accepted.
  Every key is now validated against the catalogue's known IDs (`mechanisms.KnownIDs`):
  disabled keys are checked by name — a disabled Mechanism is still never constructed — and
  the error lists the known catalogue exactly like the enabled-key path.
  (`internal/domain`, `cmd/apogee`.)

### The Phase-4 wave-1 review pass is closed out

- **The 2026-07-04 review of Phase-4 items 1–6 landed as five corrective fixes plus a docs
  close-out** (`docs/plans/phase-4-review-fixes-plan.md`), each detailed in its own entry
  above. The behaviour changes in one line: post-response corrections **retry in place**
  within the same Turn (amended catalogue C5, R1); `autofix` probes formatters **at
  construction** and repairs only when it reduces the issue count, running **before**
  `syntax`; self-regulation judges the **next Turn** three-way on four proxy signals and
  books only **acted** fires; the registry and config **refuse duplicate, reserved, and
  unknown** mechanism IDs loudly. The deliberate divergences from the sim (the R2 retry-
  ladder refinements and per-mechanism throttle counters — bench-pending) are recorded in
  the catalogue, and the Phase-4 detail plan carries the review's NOTES trail under items
  3, 5, and 6. (Docs: `docs/design/mechanism-catalogue.md`,
  `docs/plans/archived/phase-4-detail-plan.md`.)

### Wave 2: the `truncate_history` drop-the-middle history rewrite (`correct_tool_result` deferred)

- **A cheap, structural alternative to generative Compaction is ported (catalogue Table A).**
  `truncate_history` is a history-rewrite Mechanism that drops the middle of the conversation,
  keeping the protected prefix (leading system messages + the first user message,
  `Conversation.PrefixEnd`) and the last few assistant-anchored exchanges, cutting **only** at
  `Conversation.AssistantBoundaries()` so a tool result never gets separated from the assistant
  call that produced it (strict chat templates reject an orphaned tool message). At the cut it
  inserts a single static gap note; when fewer exchanges exist than the keep window it is a
  no-op (and books no fire — the loop keys acted fires on `Conversation.Revision`, R4). Ported
  verbatim from apogee-sim `internal/sim/intervention.go` `truncateHistory` @pin. Capability
  **proactive-nudge** (a context-shaper — disabled under Bypass, D5, while the structural Budget
  and Compaction stay on, D6), SuppressionPolicy **strikes-3**, default **off** (D1). It ships in
  the `internal/mechanisms` catalogue, buildable via the `mechanisms:` config block.
- **No phantom acted-fire on an ungrown, already-truncated history (second-review fix).** Re-running
  `truncate_history` when the conversation has not grown a new assistant boundary since the last cut
  used to re-drop and re-insert the same gap note — rebuilding the identical shape but bumping
  `Conversation.Revision`, which the loop reads as an acted fire (R4). The rewrite now detects that the
  only pending drop is the gap note it inserted last time and returns without mutating, so Revision
  stays put and no `MechanismFiredEvent` is booked. The truncation content stays sim-faithful and the
  grown-history path (real middle to shed) still truncates and books normally. (`internal/mechanisms`.)
- **`correct_tool_result` is deferred, not ported (owner-ratified 2026-07-04).** The pinned sim
  defines no production trigger for it — it is a lab-only intervention with an operator-supplied
  correction — so inventing gating logic would ship behaviour with no evidence. The loop already
  exposes the lab surface (an experimental post-tool-result hook can replace a result via the
  mutation API), so the bench plays the operator without a catalogued Mechanism; a bench-discovered
  trigger would motivate a new plan item. (`internal/mechanisms`; catalogue Table A/B.)

### The Budget allocator + usage-calibrated token accounting make `LoopView.Budget()` honest

- **`LoopView.Budget()` now reports honest token accounting.** The loop's former trivial
  `defaultCharsPerToken = 4.0` estimate is replaced by a per-Session `TokenEstimator`
  (`internal/context`) the loop **calibrates against server-reported usage**: each Turn, the
  reported prompt tokens snap `Budget.Used` to the real context fill, and prompt-tokens vs the
  characters actually sent recompute the chars→token ratio — bounded to a sane range `[2, 8]` and
  smoothed (an exponential moving average) so the ratio converges toward the model's real
  tokenizer across Turns while a single anomalous report cannot swing it. Uncalibrated (a fresh
  or resumed Agent, before its first `UsageEvent`) it reports the default ratio and a zero `Used`.
- **The Budget is now the single authority on how much room each part gets (CONTEXT: Budget).**
  `internal/context.Allocate` splits the discovered context window (`n_ctx`) across a response
  reserve and the prompt's parts — system prompt, file context, conversation history — with the
  parts summing to the window exactly; an unknown window yields the zero allocation (treated as
  unbounded). `domain.Budget` gains the advisory `ResponseReserve`/`SystemPrompt`/`FileContext`/
  `History` fields (additive; the root `apogee.Budget` alias picks them up), which the item-9
  context reducers will consume. It is **structural**, not a Mechanism: it stays live under
  Bypass (D5/D6). Nothing in the request path is reshaped by it yet — the allocation is advisory
  until the reducers land. (`internal/context`, `internal/agent`, `internal/domain`.)

### Tool-result capping + the automatic Compaction trigger — the two Budget consumers

- **`tool_result_cap`: a config-gated tool-result capping Mechanism.** The surviving half of
  apogee-sim's `compress` (catalogue C3 SPLIT), ported as a pre-request Mechanism: any single tool
  result whose content exceeds its fraction of the Budget (40% of the working window — the window
  less the response reserve — in characters, via the calibrated chars→token ratio) is trimmed to a
  head/tail-plus-elision-marker form through `Request.SetMessageContent` (an in-place edit), while
  the **most recent tool-call Turn is always protected**. Default-off (D1); `proactive-nudge` /
  `strikes-3`, so Bypass disables it and it self-regulates like its peers. (`internal/mechanisms`.)
- **Automatic, budget-driven Compaction.** The generative `Compact` (the `/compact` reducer) now
  also fires **automatically**: at a quiescent boundary, before a Turn's request is built, the loop
  folds the conversation when `internal/context.HistoryExceedsAllocation` reports the history has
  outgrown its Budget `History` allocation. It runs the same fold (protected prefix, `Replace`
  write-back) before it consumes new input, so a just-submitted message survives as its own turn;
  it is non-reentrant, and a fold fault surfaces as an `ErrorEvent` leaving history untouched. It is
  **structural**, not a Mechanism: on by default and **on even under Bypass** (a naked model still
  overflows its window — decision 12), with a file-only `auto-compact: false` opt-out
  (`ContextConfig.CompactionEnabled`). The on-demand `/compact` is unaffected by the gate.
  (`internal/context`, `internal/agent`, `cmd/apogee`.)
- **Auto-compaction is Exchange-boundary-only and saturates on an oversized prefix (second-review
  fix).** The automatic trigger now also requires **not** `inExchange`: a mid-Exchange over-budget
  Turn (a tool continuation) defers the fold to the next Exchange opening rather than folding a
  half-finished Turn into a summary (`tool_result_cap` is the mid-Exchange relief valve). A fold that
  still cannot bring the history under its `History` allocation — the protected prefix (system prompt
  + first user message) alone exceeds it — emits exactly one `compaction` `ErrorEvent` and then
  **stands down** until the estimate drops back under the allocation (growth alone no longer thrashes
  the fold every Turn); the on-demand `/compact` ignores saturation. And a mid-Exchange history
  rewrite (`truncate_history`) now **repairs `exchangeStart`** by the drop delta, floored just past
  the prefix + gap note, so `AbortExchange` (Esc) rolls back to exactly the Exchange boundary with no
  orphaned tool results. (`internal/agent`.)
- **The saturation latch is now gated on a fold that ran (third-review fix).** A `Compact` that
  **skips** (too few messages past the protected prefix to be worth folding) folds nothing, so it
  proves nothing about whether folding can help — yet the auto-trigger used to run its post-fold
  saturation check on the skip too, latching off (one `ErrorEvent`) and permanently disabling
  auto-compaction whenever the history was over its allocation but too short to fold. `autoCompact`
  now returns on `Result.Skipped` before the saturation check, so only a fold that **ran** and still
  left the history (protected prefix + summary) over its allocation can saturate; a skipped boundary
  re-checks for free at the next opening. (`internal/agent`.)
- **Context-window discovery for pinned models + a `context-window:` key (second-review fix).** A
  configured `model:` no longer silently disables the Budget and automatic Compaction. Window
  discovery is split out of `resolveModel` and now runs for a pinned model too — keeping the pinned
  id but adopting the server's advertised window — and is **non-fatal**: a failed probe leaves the
  window unknown with a one-line notice, so an offline pinned-model start still works (the no-model
  path keeps its existing fatal semantics). A new file-only `context-window:` key (tokens) overrides
  discovery and skips the probe. When the window is still unknown while Compaction is on, startup
  prints one notice naming the consequence and the key. (`cmd/apogee`, `internal/domain` comment.)
- **No redundant context-window probe on the no-model path (third-review fix).** When the server
  advertised no window on a zero-config (no-model) startup, `resolveModel`'s discovery probe left the
  window at 0, so the separate `resolveContextWindow` self-guard (`opts.contextWindow > 0`) did not
  fire and it probed the server a second time. `resolveModel` now reports whether it probed and the
  root skips `resolveContextWindow` when model discovery already ran — one probe for the whole
  no-model startup, regardless of the advertised window. The pinned-model path is unchanged (still
  probes for its window; a failed probe stays non-fatal). (`cmd/apogee`.)
- **`context-window` precedence and the `ContextConfig` threading are now pinned by tests
  (third-review fix, Tests).** A test proves a `context-window:` key wins over the server-advertised
  window on the no-model path (`resolveModel` keeps the discovered id but not the advertised window),
  and a `runRoot` test proves `opts.contextWindow` reaches `Config.Context.MaxContextTokens` (via the
  loud-zero notice) — closing the mutation gap the pinned-model-only coverage left open. (`cmd/apogee`
  tests.)
- **`cached_content_intercept`'s schema-gate conservative fallbacks are now pinned by tests
  (third-review fix, Tests).** A redundant re-read that would otherwise be capped is proven left
  byte-identical (no fire, R4) when the pending read tool is absent from the (toolfilter-narrowed)
  menu, carries an empty schema, or carries a schema that does not parse — closing the mutation gap
  the earlier coverage left silent. (`internal/mechanisms` tests.)

### Wave 3: the `toolfilter` / `filehint` / `grammar` request shapers

- **`toolfilter`: relevance-scored tool-menu narrowing.** A pre-request Mechanism that trims the
  tool menu for small models, ported from apogee-sim `internal/toolfilter` @pin. It activates
  reactively — only when the menu is large (30+ tools) or the model has hallucinated a tool absent
  from the menu — and never when the menu is already within the keep limit (10). It scores each tool
  against the last user message's keywords (exact name > name-part > description match), keeps every
  recently-used tool whole (plus the read-only exploration tools when the request is analysis-focused),
  and re-sets the menu to the top-scored subset via `Request.SetTools`. The narrowing is
  **request-scoped** (the loop rebuilds the full menu each Turn, so it never mutates the menu
  globally) and deterministic (stable score-tie ordering). It declares `Before decompose` (item 12).
- **`filehint`: role-safe workspace file hints.** A pre-request Mechanism ported from apogee-sim
  `internal/filehint` + `file_hint_detector` @pin. After the model lists a directory but before it
  reads anything, it scores the listed files against the user prompt (a TF-IDF-ish weight plus a
  language-extension boost) and injects a hint suggesting the most relevant files to read, through
  the role-safe `Request.InjectContext` (which folds into the system prompt when the conversation
  ends in a tool result). A stable marker makes the inject **idempotent** (no double-inject), and a
  greenfield-creation task with no files written yet is suppressed.
- **`grammar`: a backend-capability-gated json_schema constraint.** A pre-request Mechanism ported
  from apogee-sim `internal/grammar` + `injectGrammarConstraint` @pin: it derives a `json_schema`
  from the current tool menu and sets it as the request's `response_format` so a model that cannot
  emit native tool calls is constrained to a valid tool-call shape. It is **capability-gated** by the
  new D3-injected `mechanisms.Deps.GrammarConstraint` — false on every current apogee backend (no
  such probe is wired, and the provider wire does not yet carry request extras), so grammar **no-ops
  today** (catalogue Table B). An existing `response_format` always wins.
- All three ship default **off** (D1), `proactive-nudge` / `strikes-3` (disabled under Bypass, D5;
  self-regulating), buildable via the `mechanisms:` config block. (`internal/mechanisms`.)
- **`toolfilter` / `filehint` carry the sim's camelCase spellings (second-review fix).** The
  analysis-keep set (`toolfilter`) now also holds the sim's `readFile`, and the directory-listing set
  (`filehint`) holds the sim's `listFiles` — completing the item-10 "plus the sim spellings" claim so
  a mixed MCP menu with camelCase tool names still narrows and hints. (The write-tool and file-read
  sets already carried every sim spelling.) (`internal/mechanisms`.)
- **The sim-seeded pre-request ordering edges are now declared (second-review fix).** The catalogue's
  §Ordering seeds are now live `OrderingConstraints`, not just prose: the `cot` nudges (`stall_nudge`,
  `list_nudge`, `tool_use_directive`) and `library` inject `Before toolfilter`, and `tool_result_cap`
  runs `After decompose` — so it sorts last among the pre-request shapers, trimming tool results after
  context is assembled. Previously the order rested on the D4 ID tiebreak alone, which matched the sim
  for the nudges/library but sorted `tool_result_cap` *before* `toolfilter`. Table A's "none" cells
  were amended per D7 to record the edges, so §Ordering, Table A, and the code now agree, and a
  regression test pins the resulting order. (`internal/mechanisms`, `docs/design/mechanism-catalogue.md`.)

### Wave 3: the history-aware `error_enrichment` / `read_loop` / `read_repeat` / `tool_loop_interceptor` / `cached_content_intercept` family

The cross-turn aggregators, ported from the pinned apogee-sim source (catalogue Table A/B), each
deciding by scanning the conversation across Turns at its **relocated** hook point. All ship default
**off** (D1), `strikes-3` and non-exempt (so disabled under Bypass, D5), buildable via the
`mechanisms:` block. (`internal/mechanisms`.)

- **`error_enrichment`: repeated-error clarification at post-tool-result.** Ported from apogee-sim
  `internal/proxy/error_enrichment` @pin and relocated to post-tool-result: when a write-tool call
  fails, and the same file already failed the same way earlier this Session, it appends
  category-specific guidance (syntax / import / type / build / permission / runtime) to the failing
  result the model reads next. The current failure uses the authoritative `ToolResult.IsError`; prior
  failures in history are string-classified (a committed tool-result message no longer carries the
  flag). A marker keeps one hint per repeated-error episode.
- **`read_loop`: the consolidated read-loop detector at pre-request.** Ported from apogee-sim
  `internal/proxy/read_loop_detector` @pin, folding the sim's three variants (normal / greenfield /
  successful) into one Mechanism (catalogue C2): a role-safe hint fires on repeated failed reads of
  the same file (threshold 1 on an empty workspace, 2 otherwise) or three successful re-reads without
  a write. The deterministic hint is its own idempotency marker.
- **`read_repeat`: redundant re-read retry at post-response.** Ported from apogee-sim
  `internal/proxy/read_repeat_interceptor` @pin: when the whole response only re-reads files already
  read successfully in a recent Turn, it retries in place (`ActionRetry`, R1) with a "you already
  read these, proceed" correction.
- **`tool_loop_interceptor`: identical-repeat-turn detector at post-response.** Ported from apogee-sim
  `internal/proxy/tool_loop_interceptor` @pin (inventory-missed, found in the checkout — catalogue
  Table B): when the response repeats the previous Turn's exact tool-call key, it retries with a
  loop-breaking directive. The sim's per-Session count threshold and 30s cooldown are dropped (R2 —
  self-regulation and the loop retry cap substitute).
- **`cached_content_intercept`: redundant-re-read cap at pre-tool-exec.** Ported from apogee-sim
  `internal/proxy/cached_content_intercept` @pin and relocated to pre-tool-exec: a re-read of a file
  already read successfully and unchanged since is capped to a header-only slice, reclaiming the
  window the full re-dump would cost (the content is already in context). The sim rewrote the result
  post-execution; pre-tool-exec has no result-substitution primitive, so the port expresses the same
  token-saving intent through the pending call's arguments.
- The re-read family (`read_loop` / `read_repeat` / `cached_content_intercept`) is pairwise
  **incompatible** — at most one is enabled at a time (the sim's per-request exclusivity as an apogee
  startup gate). In the post-response cascade the resolved dispatch order is
  `read_repeat → tool_loop_interceptor → validate → autofix → syntax` (the sim's response-side
  priority).
- **Write detection now sees apogee's own edit tools (second-review fix).** The history family's
  "did this call mutate a file / was it a write action" checks (`read_repeat`, `read_loop`,
  `cached_content_intercept`, `error_enrichment`, `tool_loop_interceptor`, the off-ramps,
  `deriveWriteTarget`) moved from the sim-only `isWriteTool` set to a new apogee-complete
  `isFileMutatingTool` predicate that also counts `edit_existing_file` /
  `single_find_and_replace` / `multi_find_and_replace`; the content-repair Mechanisms (`syntax`,
  `autofix`) stay on the narrower sim-only set (their payloads are file fragments, not full files).
  `open_file` joins the family read set (its result places file content in the conversation like
  `read_file`). And `read_repeat` now collects each turn's write paths **before** its reads, so a
  same-turn read-then-write to a path no longer counts that read as a redundant re-read.
- **`cached_content_intercept` gates its cap on the tool schema (second-review fix).** The read cap
  is now applied only when the pending tool's argument schema (via `view.Tools()`) declares a
  `max_lines` property; a read tool lacking it — e.g. a strict MCP server with
  `additionalProperties:false` — is inspected but never handed an argument it would reject, so the
  re-read proceeds uncapped and no fire is booked. This makes the mechanism's "benign no-op" fidelity
  note literally true instead of relying on the third-party tool tolerating an unknown field.
- **The `isFileMutatingTool` history-family sites now have edit-tool coverage (third-review fix, Tests).**
  Tests exercise `edit_existing_file` / `single_find_and_replace` at the three sites the earlier
  suite left untested and that can carry regression-detecting coverage: `empty_response_recovery`
  treats a recent edit as progress worth recovering (`hasRecentProgress`), the `tool_loop_interceptor`
  directive credits an edit as work already done (`extractConversationContext`), and the `read_loop`
  hint excludes an edit-written path from its "create X" suggestion (`writtenPaths`) — each test fails
  when its site is mutated to exclude the edit tools. The fourth site (`wroteRecently` in the
  `tool_use_enforcer`) cannot be pinned: `shouldEnforceToolUse`'s `!hasEverUsedTools` gate stands the
  enforcer down whenever any edit call is present, so the `wroteRecently` edit branch is never the
  deciding factor — documented in place rather than covered by a vacuous test. (`internal/mechanisms`
  tests.)

### Wave 4: the `decompose` request shaper + the `stall_nudge` / `list_nudge` / `tool_use_directive` completion nudges

The last of the request shapers, ported from the pinned apogee-sim source (catalogue Table A/B), each
a pre-request Mechanism shipping default **off** (D1), `proactive-nudge` / `strikes-3` (disabled under
Bypass, D5; self-regulating), buildable via the `mechanisms:` block. (`internal/mechanisms`.)

- **`decompose`: one-step focus + history collapse.** Ported from apogee-sim `internal/decompose`
  @pin. For a small model that stalls on long multi-step prompts it (1) collapses the complex
  multi-step user messages still sitting in conversation history to a short task summary (via
  `Request.SetMessageContent`) so the model cannot re-read a full step-by-step plan from an earlier
  turn, and (2) hints the single next actionable step of the current prompt into the system prompt
  (via the idempotent `Request.AppendToSystem`), leaving the full user message intact. It declares
  `After toolfilter` (trim the menu before the user-message rewrite — the mirror of toolfilter's
  `Before decompose`).
- **The read-loop coupling gates active decomposition (D2).** decompose's `RequestMeta.FiredCounts`
  peek in the sim becomes a live `LoopView.Fired("read_loop")` query: once the consolidated
  `read_loop` Mechanism has **acted** this Session (R4), active decomposition — which would override
  the focus to "step 1: …" and fight the read-loop hint — is muted, while the harmless history
  collapse still runs.
- **The completion nudges are the `cot` family, split three ways (catalogue C4).** apogee-sim's `cot`
  Transform is not itself a tracked Mechanism — it emits three tracked nudges, which apogee ships as
  three independent pre-request Mechanisms: `tool_use_directive` (an action was asked for but the
  model has not used a tool yet → "use a tool"), `stall_nudge` (read-only for the stall threshold of
  turns with a write tool available → "proceed with the modifications"), and `list_nudge` (an analysis
  request that listed directories but read no files → "read the files you found"). Each injects one
  system directive through the idempotent `AppendToSystem`; the "nudge cap" is a stateless window on
  the read-only turn count. `stall_nudge` ⊥ `list_nudge` (contradictory directives) — declared
  `IncompatibleWith`, so at most one is enabled per config (the apogee startup gate subsuming the
  sim's runtime `!wantListNudge` preference).
- **`intent` and `cot` are folded, not ported as Mechanisms (catalogue C4/C6).** The shared intent
  classifier (`hasActionIntent` / `hasAnalysisIntent`) already landed inline in wave 1 and is reused
  here; `cot` ships only as its three nudges. This closes the Phase-4 request-shaper catalogue —
  `library` (item 14) is the only remaining un-ported catalogue Mechanism.

### The Library learning substrate: a confidence-tagged `ModelFingerprint` and a file-backed store

The substrate the Library Mechanisms (item 14) build on — no Mechanism yet, so nothing observes or
injects until item 14 wires it. (`internal/domain`, new `internal/library`.)

- **`ModelFingerprint` — a confidence-tagged model identity.** New `domain.ModelFingerprint`
  (`Label` + `FingerprintConfidence`) and the `FingerprintResolver` seam. `internal/library`'s
  production resolver returns the best available tier: a **weights-hash (high)** when the model id is
  a reachable weight file (`.gguf` / `.ggml` / `.bin` / `.safetensors`) — a SHA-256 over the file size
  plus its head and tail, so two builds sharing a label but differing in weights diverge without
  hashing multi-gigabyte files at startup — else the **metadata label (low)** (the bare model id). The
  **behavioral-probe (medium)** tier is the Phase-5 `apogee probe`: the enum slot and the resolver
  interface exist so it slots in behind the same seam, but no resolver produces it yet (D8).
- **A file-backed, versioned Library store.** New `library.Store`, rooted at an injected directory
  (`Config.LibraryDir`) and **never** an ambient `~/.apogee` (ADR 0001) — the bench's isolated root
  falls out for free (decision 11). It holds per-fingerprint observations (`Entry`) with the sim's
  Bayesian confidence counts (`Score = (observations − successes + 1) / (observations + 2)`, capped at
  0.95), so a pattern the model grows out of stops qualifying for injection without being deleted. It
  persists to a single `library.json` with a schema `Version` (like `domain.Session`), is process-local
  (a mutex guards intra-process access; no cross-process locking claims in v1), and degrades a missing,
  corrupt, or too-new store to **empty-with-a-soft-error** (the skills-catalog posture — a broken
  Library never bricks a run). A zero fingerprint (unidentified model) is inert: nothing is recorded.

### The Library Mechanism: cross-session observe + confidence-gated inject

Item 14 wires the Library substrate (item 13) into the loop as the catalogued `library`
Mechanism — default-off (D1), fully inert under `--bypass` (it is `proactive-nudge`, so item 2's
dispatch gate skips both halves). The single `library` catalogue row is realized as ONE Mechanism
implementing BOTH hooks. Ported from apogee-sim's `library` observer/transform. (`internal/mechanisms`,
`cmd/apogee`.)

- **Observe (post-response).** After each response the Mechanism records completed-Turn outcomes into
  the store, keyed on the model fingerprint: tool-call validation failures (corrections),
  narration-instead-of-acting and shallow-exploration behavioural patterns, examples of valid complex
  tool calls, and the success signal that decays a pattern the model has grown out of. It is a pure
  observer — it never mutates the response and books no fire, so it does not skew self-regulation.
- **Inject (pre-request).** When the fingerprint clears the confidence gate — "prefer not to inject
  under uncertainty", so a low-confidence metadata-label identity does **not** inject — the Mechanism
  appends the highest-scoring qualifying observations to the system prompt (idempotent on a marker),
  intent-filtered and capped to a 200-token injection budget, and backs off when the window is nearly
  full.
- **Store + fingerprint injected at construction (D3).** `cmd/apogee/wire.go` constructs and Loads the
  store under `Config.LibraryDir` (never an ambient `~/.apogee`, ADR 0001) and resolves the model
  fingerprint once, wiring both into the constructor `Deps` only when `library` is enabled — so the
  inject and observe halves share one identity, and a config without `library` reads no store file.
  Two agents on two `LibraryDir`s stay isolated (decision 11). Longitudinal bench validation
  (improves-over-sessions AND never-below-baseline) stays **pending**.
- **Stored observations are now treated as untrusted data (second-review fix, Security).** Library
  entries persist model- and tool-result-derived text and re-inject it into a future system prompt, so
  the store is now hardened against a hostile-repo → store → system-prompt payload channel. A new
  `library.SanitizeContent` strips control characters, folds CR/LF (and any whitespace) into single
  spaces, and collapses runs; it runs at `Store.Record` time — so poison never lands on disk in
  directive-capable form — **and** again when the injection block is rendered, defending stores written
  before this landed. The complex-call "example" observer records only the call **shape** — the tool
  name and its sorted parameter **names** — never argument **values**. The injected block's header now
  opens with an explicit data-not-instructions frame so entries cannot read as directives. No store
  schema bump (entries stay compatible). (`internal/library`, `internal/mechanisms`.)
- **The sanitizer now strips Unicode format characters, and example param names are schema-filtered
  (third-review fix, Security).** `SanitizeContent` stripped only Cc controls (`unicode.IsControl`), so
  bidi overrides, zero-width characters, the BOM and soft hyphens rode through into the store and the
  injected block; the strip now also covers Cf/Co/Cs. And the complex-call "example" recorded the raw
  keys of the model's arguments object — free-form, model-controlled strings — so a junk key bearing
  directive text could land on a clean observation. The recorded names are now the **intersection** of
  the call's argument keys with the tool schema's declared `properties`, and a call whose schema yields
  no properties records no example at all (prefer not to record under uncertainty); the 5+-param
  complexity gate reads the schema, never the argument keys, so junk keys can never promote a simple
  call. (`internal/library`, `internal/mechanisms`.)
- **Bypass leaves a pre-seeded Library store byte-for-byte untouched (second-review fix, test-only).**
  A loop-level test seeds a populated `library.json`, wires a registry-backed agent with `library`
  enabled and `Config.Bypass` on, drives an observe-triggering Exchange, and asserts the store file's
  bytes are unchanged — the item-14 mandate now has its literal regression. (`internal/agent`.)

### Bench-readiness proof: the embeddable two-arm contract is now a permanent regression

Item 15 adds `benchreadiness_test.go`, the executable definition of "benchable" (ADR 0001): a
root-package consumer test that drives the real Agent exactly the way apogee-sim will — the public
`New` / `Resume` / `Submit` / `Step` / `Snapshot` / `Close` surface over the real provider client
dialing one scripted OpenAI-compatible httptest model, catalogued Mechanisms enabled via `Config`
(`toolfilter` / `decompose` / `truncate_history` / `library`), and experimental hooks at all five
hook points. It constructs a mechanisms-on arm and a **Bypass** arm against isolated temp state
roots, Steps both to their quiescent boundaries, then Snapshots and Resumes forks. It asserts: the
enabled shapers ACT in the registry's deterministic dispatch order visible in the
`MechanismFiredEvent` stream (`toolfilter` before `decompose`, then the experimental hook) while an
inspect-only Mechanism books no fire (R4); the Bypass arm fires no catalogued Mechanism yet runs all
five experimental hooks; agent-driven writes stay inside each injected root (the Library store lands
under the mechanisms-on arm's `LibraryDir`, the Bypass arm's stays empty); and forks resumed from one
snapshot diverge independently in their own roots. If a future change breaks the bench contract, this
test breaks first. Test-only — no product change. (root `apogee_test`.)

## [1.1.0] — 2026-07-03

Post-`v1.0.0`, **additive** (minor) — the start of the apogee-code TUI
feature-parity track. See
`docs/handoffs/2026-06-26 - 00 - chat-mini-language-core.md` and
`docs/handoffs/2026-06-26 - 01 - skills-system.md`.

### Drag-select-to-copy in the transcript (screen-space)

- **You can now drag-select text in the chat transcript and copy it to the clipboard**, the same
  gesture the prompt box already supported. A left-click-drag inside the transcript viewport
  highlights the span and, on release, copies the rendered text over OSC52 (`tea.SetClipboard` —
  cross-terminal and SSH-safe) with the usual "copied N chars" confirmation. The selection is
  **screen-space** ("copy what you see"): it anchors in content coordinates (rendered-line index +
  display cell) into the cached `m.lines`, so it survives a mid-drag wheel-scroll; on release it
  slices each spanned line with `ansi.Cut`, strips the styling, and trims the block's trailing pad.
  Markers, rail gutters, and soft-wrap breaks are copied verbatim (the accepted terminal-native
  semantics — the one-way render pipeline stays one-way, no line→entry reverse index). The mouse
  handlers arbitrate by region — a point in the input rectangle drives the prompt editor, a point
  in the viewport drives the transcript — so the two selections never coexist. The selection clears
  on any transcript change (a streamed token, a submit) and on resize; a bare click copies nothing.
  Drag auto-scroll at the viewport edge is deferred. (`internal/tui/mouse.go`, `model.go`.) Closes
  the "cannot select text in the transcript" ISSUES entry.

### Chat input lifted into a `promptEditor` module (internal refactor)

- **The chat input cluster now lives in its own type**, `promptEditor` (`internal/tui/prompteditor.
  go`), instead of scattered across the god-Model. It gathers the five loose input-side concerns the
  architecture review (candidate #3) called one coherent concept — the textarea, the autocomplete
  overlay (+ its `skillRegion` edge-trigger), the staged-skill chips, the workspace file cache, and
  the prompt drag-selection. The `Model` embeds it **anonymously**, so the fields and the
  self-contained methods promote onto the Model (`m.input`, `m.pendingSkills`, `m.caretTo(...)` all
  resolve through it) and every existing call site — and all package tests — stay unchanged. Model
  top-level field count drops **32 → 27**; the six input-cluster fields now have a single home.
- **Purely structural — no behaviour changes.** Only methods that touch nothing but the editor's own
  fields moved to it (`newPromptEditor`, `submitParse`, `reset`, `rows`, and the caret re-seat trio
  `caretTo`/`reseatCaret`/`reseatInput`); methods that also read Model-owned state (theme, window
  size, `Options`, lifecycle) deliberately stay on the Model rather than duplicate that state. The
  Model stays the coordinator (lifecycle state machine, transcript + render cache, stats/gauge,
  theme, layout); the editor never touches the engine — it only turns typed input into
  send-ingredients the Model routes. New editor-direct unit tests exercise the lifted logic without
  a Model or a fake engine (`internal/tui/prompteditor_test.go`).

### Model profile config surface (tool-call format + thinking channels)

- **`Config` gains a `Profile ModelProfile` seam** describing how the configured model speaks the
  wire (CONTEXT: Model profile) — its tool-call format (native / markdown-fenced / custom-regex)
  and its inline thinking-channel style (none / delimited `<think>…</think>` / gpt-oss harmony).
  The new public domain types are re-exported from the root facade (`apogee.ModelProfile`,
  `ToolCallFormat`, `ThinkingProfile`, `ThinkingStyle` and their consts) — an **additive minor**
  (decision #18). A **zero profile is native tool calls with no inline thinking**, so every
  shipped model behaves exactly as before (the byte-identical anchor).
- **Plumbed from `config.yaml`** as a file-only `model-profile:` block (a per-model concern, like
  `mcp-servers` — no flag/env), mapped to the domain type at the host boundary. **No loop consumer
  yet**: the loop's parse seam is crossed in a following change, so this is a pure, provably
  behaviour-neutral config-surface addition.

### Model profile wired into the loop (fenced/regex tool calls + thinking/harmony stripping)

- **The loop now consumes `Config.Profile` at the parse seam.** A new `processing.ParserFor(domain.
  ModelProfile)` translates the declarative profile onto `internal/processing`'s existing, frozen
  `ToolCallingConfig`/`ThinkingConfig` and returns the text-format `ToolCallParser` plus a unified
  `ContentStripper` (the `none`/`delimited`/`harmony` thinking styles behind one `Strip` +
  `IsMidChannel` interface). `internal/agent` selects both once in `newAgent`, so the oracle config
  types never surface in the loop and a bad profile (unknown format / thinking style) fails
  construction loudly rather than falling back to native.
- **At the seam:** the reply's inline thinking/harmony channel is stripped out of the visible
  content and preserved as `reasoning_content` in history (the harmony `commentary` channel folds
  into reasoning); when the structured **native** path produced no calls, a markdown-fenced or
  custom-regex tool call is recovered from the *stripped* visible content, its markup removed from
  the committed assistant text, and it is assigned a deterministic `text_call_<turn>` ID (not the
  oracle's wall-clock ID, so snapshot/resume and tests stay stable). Native calls always win when
  present.
- **A recorded, deliberate divergence from the apogee-code oracle:** a text-parsed call is stored
  **structurally** on the assistant message (`ToolCalls`), so dispatch, events, and snapshot/resume
  keep **one** path for every format; the oracle instead commits stripped text with only a
  tool-role result. Chat templates tolerate native-shaped history better than the loop tolerates two
  history shapes.
- **A zero profile is byte-identical** to the pre-change loop: the no-op stripper and no-op parser
  leave `reply.content` and the native calls untouched, so every shipped (native) model behaves
  exactly as before. The frozen `internal/processing` oracle types, parsers, and parity tests are
  unchanged — only the new `ParserFor`/`ContentStripper` and the loop caller were added. **Live
  in-flight token suppression while streaming is a following change; this fixes committed history
  and the final message.**

### In-flight thinking/harmony tokens held off the live stream (native unchanged)

- **`streamResponse` now emits a `TokenEvent` for the newly-revealed *visible* content**, not the
  raw content delta, using the same `ContentStripper`. While the accumulated content ends inside an
  unclosed inline reasoning span (`IsMidChannel`), token emission is held, so a model that inlines
  `<think>…</think>` or gpt-oss harmony channels no longer flashes that markup (or its reasoning)
  onto a live UI before the post-stream strip; the visible text is revealed once the span closes.
- **A native / no-inline-thinking profile is byte-identical, event-for-event:** the no-op stripper
  is never mid-channel and returns the content untouched, so every content delta emits verbatim and
  unbuffered exactly as before. A channel start token split across two deltas briefly reveals its
  partial prefix live (matching the oracle's `isThinking`); this recorded edge is accepted — the
  post-stream strip still removes it from the committed message and final `MessageEvent`.

### Fenced/regex models now receive a text tool menu + emission instructions (native unchanged)

- **A new `processing.InstructionsFor(domain.ModelProfile, []domain.ToolDef)` renders the emit
  side of a non-native profile:** the text tool menu (name, description, JSON-schema parameters)
  plus the format-specific tool-call instructions and a live example — ported from the apogee-code
  context builder, driven by the *same* profile knobs and defaults the parser reads, so what the
  model is told and what the loop parses cannot drift. It is the request-seam mirror of `ParserFor`.
- **`toProviderRequest` now injects the block and suppresses the native `tools` array for a
  non-native tool-call format.** The rendered menu + instructions are folded into the wire request's
  system channel (appended to a hook-seeded system message, else a sole system message at position
  0) and the native `tools` array is dropped — sending both would double-tell the model in two
  formats, and a chat template without tool support can error on the array. For a non-native profile
  the text menu is the **only** channel the model learns its tools from; before this change a
  fenced/regex model received a native array its template may not render and no instructions.
- **Wire-only, tracked per request:** the block never enters domain history, the snapshot, or any
  event — exactly like the native `tools` array, which is also rebuilt per request and never
  persisted. It is re-rendered over each request's **mode-filtered** menu, so a Plan-mode switch (or
  any menu change) is reflected on the next Turn with no history rewrite.
- **A native/zero profile is byte-identical:** `InstructionsFor` returns `""`, so there is no
  injection and no suppression — the native `tools` array and the message list are exactly today's.

### Dispatch decision collapsed into one Resolution verdict (internal refactor)

- **The per-call dispatch decision is now one `Resolution`**, computed by a single pure resolver
  (`internal/agent/resolution.go`): the tighten-only guard floor, the autonomy-ladder × blast-radius
  table, the confinement-capability check, and the precomputed runtime-demote contingency are all
  decided in full before anything executes. `internal/agent/dispatch.go` is now a thin executor that
  gathers facts, calls the resolver once, and carries the verdict out — it holds no ladder,
  guard-tier, or demote decision of its own. The old `disposition.go` decision path is retired.
  **No behavior change**: unexported and internal-only (no public API / semver impact). The term
  "disposition" is retired from code, surviving in prose only as the historical name of the
  post-guard ladder stage. `docs/design/confinement-execution-contract.md` §4 amended in place.

### MCP "allow for this session" now caches at server grain (ADR 0012 conformance)

- **Approving one of an MCP server's tools "for this session" now clears the whole server**, not
  just that one qualified tool: approving `github__search` pre-clears `github__create_issue` and
  every other `github__*` tool for the Session, honouring ADR 0012's server-grain promise (the
  cache had always keyed on the qualified tool name, so each `github__*` tool re-prompted). The
  allow-for-session cache key for an `mcp` gate is now `mcp-server:<alias>`; the `mcp-server:`
  prefix keeps that grain collision-proof against ordinary tool names, and a **different** server
  (`jira__*`) is never pre-cleared by another's approval. A **forced** gate (a Tier-2
  dangerous-action speed-bump) still skips the cache and re-prompts, unchanged. Every non-MCP
  class keeps the tighter tool-name grain, so nothing else loosens.

### Compact tool print-outs in the chat (full built-in coverage)

- **The TUI's tool-presentation registry now covers every built-in tool**, not just the
  Phase-2 four: the edit family, `view_diff`, `open_file`, `terminal`, `python_exec`, the
  git family, `diagnostics`, `web_fetch`, `http_request`, `web_search`, `sub_agent`, and
  `ask_user` each render as `✦ [Label] target` — no more raw tool names with JSON argument
  braces in the transcript. Only a dynamic (MCP) tool keeps the raw-name + JSON fallback.
- **Results no longer dump raw into the chat**: `web_search` shows "N results", the fetch/
  request tools their `HTTP 200 OK` status line, free-form output (a command run, a
  diagnostics or sub-agent report) its first line plus a "+N more lines" count, `open_file`
  its Located line or a line count. `view_diff` renders red/green diff lines (the reserved
  diff detail kinds get their first producer), capped at 20 lines.
- Detail and target lines are clipped at 160 runes so a minified blob cannot flood a row.
  The approval dialog still shows the full pretty-printed arguments — the security surface
  (the model's request is never hidden) is unchanged.

### Web search works out of the box (DuckDuckGo default)

- **`web_search` is now default-ON**: with no `web-search-endpoint` configured it uses a
  built-in DuckDuckGo HTML provider — no config, no API key (reverses the P3.11 default-off
  decision; the predecessor apogee-code shipped the same built-in). Set
  `web-search-endpoint: off` (or `none`/`disabled`) to disable the tool — a graceful
  "web search is disabled" result, no request made.
- **The DuckDuckGo provider POSTs the query** as a form field, the way DDG's own search
  form submits: the HTML front-end answers a plain GET with its bot-challenge ("anomaly")
  page — zero result anchors, so every search rendered "No results found". A custom
  endpoint keeps the `q` GET-parameter contract unchanged.
- **An explicitly configured DuckDuckGo endpoint selects the built-in provider**: an
  endpoint whose host is `html.duckduckgo.com` (with or without scheme) now gets the same
  POST + browser-header treatment as the default, instead of degrading to the
  custom-endpoint GET that DDG answers with the challenge page.
- **Results are auto-cleaned**: the DuckDuckGo page (and any custom endpoint's HTML
  response, by Content-Type or body sniff) is parsed into numbered `title / url / snippet`
  results; a custom endpoint's JSON/text response still passes through verbatim. A
  rate-limit/consent page degrades to "No results found", never a crash.
- **Non-2xx responses are now tool errors** naming only the status and endpoint host
  (previously the status + raw body passed through as a normal result). The M2 key
  redaction (`endpointHost`/`scrubURLError`) and the always-on SSRF floor are unchanged.
- **Scheme-less custom endpoints self-heal**: an endpoint like `search.example.com/s`
  (no `https://`) used to parse with an empty host and every request was rejected by
  url-safety; it now self-heals to `https://`. This repairs hand-edited configs — the
  shipped config template never carried a broken value (its endpoint line was always
  commented out), and first-run seeding never overwrites an existing config.

### Context compaction (`/compact`)

- **`/compact` now performs real generative compaction** (replaces the
  `ErrCompactionNotImplemented` stub). The new `internal/context.Compact` reducer
  summarizes the conversation through a single upstream call and replaces the folded
  history with one assistant summary message, keeping the protected prefix (leading
  system messages + the first user message, `Conversation.PrefixEnd`) verbatim so the
  original task framing survives. A conversation with too little past the prefix is
  skipped; a summary-call failure or cancellation leaves the history untouched.
- **Wired through `Agent.Compact`** (guarded to a quiescent boundary like `ClearContext`,
  returning `ErrInputPending` mid-Exchange). The summary call is *silent* — it reuses the
  loop's request projection but emits no `TokenEvent`/`UsageEvent`, so it neither streams
  into the transcript nor moves the live gauge; it runs at low temperature.
- **TUI** drives `/compact` on a worker goroutine (it is a real upstream call and must not
  block the `Update` loop — ADR 0011): the spinner runs, `Esc` cancels, and on success a
  "context compacted" note lands while the context-fill gauge resets so the next Turn
  re-measures the smaller fill.
- **Removed** the now-unused `ErrCompactionNotImplemented` sentinel (it was never in a
  released version).

### Fixes

- **Prompt box no longer scrolls the first line out of view as it auto-grows.** Typing past the
  wrap width grew the input box, but bubbles' `textarea.SetHeight` only repositions its internal
  view when the caret falls *outside* it — never when the box grows — so a stale downward scroll
  offset survived: the first line was hidden above and a phantom blank row showed below, with the
  caret pinned to the top visual row. `layout` (`internal/tui/model.go`) now re-seats the caret
  after a height change through the shared `reseatCaret` idiom (`MoveToBegin` "unscrolls" to the
  top, then the widget's own `CursorDown` walks back to the caret's real row, re-clamping the
  offset with none of the textarea's wrap re-derived); it runs only on an actual height change, so
  vertical caret navigation keeps the widget's sticky goal column. A companion fix corrects
  `inputContentRows` (`internal/tui/render.go`) to count the trailing row the textarea reserves for
  a logical line that exactly fills the width, so the box no longer sizes one row short at a
  wrap-fill boundary (which had stranded the same offset the re-seat could not then reach). At the
  `maxInputRows` cap the textarea's legitimate internal scrolling is preserved (offset =
  contentRows − height). Closes the prompt-scroll and auto-sizing ISSUES entries.

- **Auto mode now works on macOS — seatbelt fences the workspace correctly.** The
  `sandbox-exec` profile embedded the box's writable roots verbatim, but seatbelt
  matches a write against its *kernel-canonical* path; on macOS `/tmp` and `/var`
  are symlinks into `/private`, so a box rooted at `/var/folders/...` never matched
  the resolved `/private/var/folders/...` and seatbelt denied **every** in-workspace
  write — Auto mode could not write at all. `seatbeltProfile`
  (`internal/platform/seatbelt.go`) now resolves each writable root through symlinks
  (`filepath.EvalSymlinks`, falling back to the cleaned path for a not-yet-created
  root) before emitting the `(subpath ...)`, so the profile matches the kernel's view
  and agrees with path-safety (which already resolves the same way). Landlock is
  unaffected — it is fd-based (`unix.Open(root, O_PATH)`), so the kernel resolves
  symlinks to the inode the rule keys on. Closes the `v1.0.0` "Box-root
  canonicalization" post-release residual; verified on real macOS hardware
  (`TestSeatbeltProbe` in-box write rows now pass under live `sandbox-exec`).

- **Context window now reads the runtime size from llama.cpp `/props`.** Discovery
  (`internal/provider.Discover`) probes `GET /props` after `/v1/models` and prefers
  its `default_generation_settings.n_ctx` — the `-c`/`--ctx-size` the server was
  actually launched with — over the model's advertised *training* window
  (`context_length`, else `meta.n_ctx_train`), which is often far larger than the
  loaded window. This fixes the live context-fill gauge measuring usage against the
  wrong denominator (it barely moved on a server loaded well under its training
  context). Best-effort: a non-llama.cpp server (no `/props`) keeps the `/v1/models`
  value, and a probe failure never fails discovery. Ports the oracle's previously
  deferred `llamacpp-props` strategy; the `ollama-show`/`ollama-tags` strategies
  remain unported (additive, not needed yet).

- **`/compact` and the context gauge now tell the truth.** Four fixes to the
  compaction/gauge seam that had it reporting outcomes it did not produce:
  (a) an Esc landing *after* a compaction committed reported "cancelled" while the
  history had already folded — `startCompact` (`internal/tui/worker.go`) now
  classifies the outcome from `Compact`'s returned error (`context.Canceled`), not a
  post-hoc `ctx.Err()` read, so a committed fold reports as compacted;
  (b) a no-op compaction (conversation too small to fold — the reducer's
  `Result.Skipped`) printed "context compacted" and hid the gauge — `Agent.Compact`
  now returns the skip signal through the `Engine` seam and the TUI says "nothing to
  compact" and leaves the gauge untouched;
  (c) `/clear` left the gauge and tok/s readout lit from the discarded session —
  `ClearContext` now zeroes `ctxUsed`/`tokPerSec` like a fold does;
  (d) a cancelled or faulted stream emits no terminal `UsageEvent`, so the
  generation clock survived into the next turn and mistimed its tok/s — `finishWorker`
  now clears `genStart` on every terminal message.

- **A loop fault no longer risks re-wedging the engine.** The `errMsg` handler
  (`internal/tui/model.go`) now calls `AbortExchange` before returning to the errored
  state, mirroring the `cancelledMsg` recovery: if a `Step` ever faults mid-Exchange
  the interrupted Exchange is discarded so the next `/clear` or message is accepted
  rather than refused with `ErrInputPending`. A latent fix — `Step` surfaces faults as
  an `ErrorEvent` at a boundary today — but it closes the error flavour of the post-Esc
  un-wedge. The `/compact` failure/cancel spine (both `startCompact` outcomes and the
  reducer's overflow/cancel/silence faults) is now covered by tests.

- **`/compact` now survives high context fill.** The reducer sent the *entire* rendered
  transcript as one summary request, so near `n_ctx − compactMaxTokens` the summary call
  itself overflowed (`DeltaContextOverflow`) — compaction deterministically failed at exactly
  the fill it exists to relieve, leaving `/clear` as the only recovery. `internal/context.Compact`
  now bounds the rendered transcript to a character budget derived from the discovered context
  window: it keeps the protected prefix and a budgeted tail of the most recent messages (the
  latest is always kept) and elides the middle with a `[... N earlier message(s) omitted ...]`
  marker, so the summary call stays within the window. The budget is computed in
  `Agent.compactTranscriptChars` from `Context.MaxContextTokens` (now threaded from upstream
  discovery in `cmd/apogee/wire.go`) minus the response reserve and prompt overhead; it is 0
  (render everything, as before) when the window is unknown, since there is no safe basis to
  bound. The overflow test flips from "errors cleanly" to "succeeds via the budget"; the
  unbudgetable case (no discovered window, or a server that rejects even a minimal prompt) still
  surfaces the fault cleanly with the conversation untouched. This makes on-demand `/compact`
  robust; the automatic compaction trigger (which fires *at* high fill by definition) is still
  parked in `TODO.md`.

- **Mouse selection and bracketed paste now handle the prompt correctly.** Two input
  fixes on shipped TUI behaviour:
  (a) a click or drag on a prompt row with wide glyphs (CJK, emoji) landed the caret on
  the wrong rune — `caretTo` (`internal/tui/mouse.go`) fed a display-**cell** column into
  the textarea's rune-indexed `SetCursorColumn` (clamped by cell width, not rune count),
  so a drag-copy could put **different text on the clipboard than was highlighted**. It
  now converts the cell column to a rune offset by walking the visual sub-line's runes and
  accumulating `runewidth` (the same width the widget's own cursor math uses), clamped by
  rune count;
  (b) bracketed paste (default-on in bubbletea v2) fell into `Update`'s `default:` case,
  so the textarea inserted the text but skipped the post-edit refresh — a multi-line paste
  rendered unwrapped until the next keypress, the autocomplete overlay went stale, and a
  live drag-selection's cached offsets no longer matched the value (a later copy took the
  wrong runes). A new `tea.PasteMsg` case (`internal/tui/model.go`) mirrors the keypress
  edit path: it clears the selection, inserts, recomputes autocomplete, and re-lays out;
  a paste while a worker runs is dropped, as keystrokes are.

- **A sub-agent now sees a mid-delegation mode tightening (ADR 0013).** `newChildAgent`
  froze the parent's mode at spawn, so a Shift+Tab from Auto down to Plan while a sub-agent
  ran (many Turns on a small model) flipped the footer but left the child auto-approving
  writes until its Exchange ended — a tighten-direction ADR-0005 violation. The orchestrator
  now injects a tighten-only view of the parent's live mode into the child (`Agent.liveMode`,
  the parent's `modeMu`-guarded `Mode` accessor captured as a closure — never the shared field
  or mutex). The child's disposition (`effectiveMode`) takes `TighterMode(parentLive,
  spawnMode)` — a new ladder-index helper in `internal/domain/config.go` where Plan <
  Ask-Before < Allow-Edits < Auto — so a parent tightening below the child's spawn mode
  gates/refuses the child's next call, while a parent loosening can never loosen a child
  spawned tighter (loosening mid-flight stays impossible). A top-level agent (nil view)
  behaves exactly as before.

- **Cleanup batch — leaked cancels, bounded untrusted reads, escape hardening, quit race,
  dead code.** A sweep of small hardening fixes on shipped behaviour:
  - *Leaked cancels.* `finishWorker` (`internal/tui/model.go`) nil'd the worker's `CancelFunc`
    without calling it, leaking one cancellable child context (and its timer resources) per
    completed exchange for the session. It now cancels before clearing.
  - *Bounded reads of untrusted files.* Skills discovery read `SKILL.md` unbounded at startup
    (`.apogee/skills` is always scanned — a hostile-repo OOM), and the `@file` 10 MB cap was
    checked only *after* `SafeReadFile` had already slurped the whole file. Both now bound
    before materializing — skills via an `io.LimitReader` (1 MiB/file) plus a global skill-count
    cap, `@file` via a new `security.SafeStat` fenced size check — mirroring the read_file tool.
  - *Terminal-escape hardening.* Untrusted model text and skill display names are now
    escape-stripped at the transcript boundary (`internal/tui/transcript.go`), so a
    model- or `SKILL.md`-supplied `\x1b]52;…` (OSC 52 clipboard write) or CSI payload can never
    reach the terminal. Not exploitable in the current layout (verified empirically at review),
    but this closes it at the source rather than relying on the cellbuf and footer ordering.
  - *Quit-while-busy teardown race.* `quit()` returned `tea.Quit` without joining the in-flight
    worker, so `runRoot`'s deferred `Close()` teardown could race a worker still inside `Step`
    (benign while `Close` is a no-op, a use-after-close the moment it gains real teardown). The
    exit is now deferred until the worker's single terminal Msg arrives.
  - *Dead code.* Removed the zero-caller `Engine.Mode()` seam method, the unused `fitLeftRight`
    footer helper, and the standalone `workspaceFiles` walk plus its unreachable `m.files == nil`
    autocomplete fallback (`newModel` always installs the cache). The three skill-chip
    render/ID-resolution copies were merged onto one `renderSkillChip` renderer and the shared
    `skillDisplayNames` resolver.
  - *Test gaps.* Added coverage for the loop's `UsageEvent` emission hop (Delta.Usage → event
    fields/Depth, and no event when Usage is nil), the combined skills→files→text injection
    order in one Submit, the `@file` oversize refusal, the escape-strip boundary, and the
    bounded skill-file read.

### TUI

- **Context-fill gauge restyled** to match `llama-launcher`: a solid two-tone strip —
  full blocks for the filled cells, an eighth-block partial cell (`▏▎▍▌▋▊▉`) for
  sub-cell granularity, and a solid dark-gray track behind the remainder — replacing
  the old `█░` dotted bar. Periwinkle fill, a min-sliver floor so any nonzero usage
  shows at least `▏`, and a clamp at the window limit. Bar width is now 10 cells (was
  6). The status line composes the gauge raw rather than re-wrapping it in a
  background style, so the bar keeps its own per-cell backgrounds.

### Skills system + `/skill` (apogee-code feature-parity)

- **`internal/skills` package** discovers user-authored skills — a folder
  containing a `SKILL.md` (YAML frontmatter `id`|`name`, `displayName`,
  `summary`|`description`, plus a Markdown body; a no-frontmatter fallback sniffs
  the first lines) — from three layered dirs: `~/.apogee/skills`, the workspace's
  `.apogee/skills`, and (when `use-project-skills` is on) the workspace's bare
  `skills/`. Later source wins on an ID collision. Each dir is walked through
  `os.OpenRoot` so a symlink can't escape it; a missing dir is skipped and a
  malformed skill is skipped with a soft error (one bad file never blanks the
  catalog). No builtin/embedded skills and no auto-created `~/.apogee/skills` ship
  in v1 (additive future hooks).
- **`/skill` in the TUI** — the `/` menu offers `/skill`, which chains into a skill
  picker; a pick pops a chip above the input, and submit attaches the chosen IDs.
  An empty message with skills attached is a valid send. `/skill` is deliberately
  **not** a parser command (attachment is the only way it acts), so an unknown
  `/skill foo` is still sent as an ordinary message. `/clear` and `/compact` drop
  staged chips; `/continue` carries them.
- **Attached skills now resolve** (replaces the `SkillIDs` "reserved/ignored"
  stub): the loop maps each `UserInput.SkillIDs` entry through `Config.Skills` and
  prepends its body to the user message for that one Turn (order: skills → `@file`
  blocks → user text). An unknown ID, or any ID with no resolver wired, is reported
  via an `ErrorEvent` and dropped — never silently ignored.

### Configuration

- **`use-project-skills`** (config-file only, default **true**) gates discovery of
  the workspace's bare `skills/` folder (the global library and the project's
  `.apogee/skills` are always loaded). Documented in the seeded `config.yaml`.

### Chat input mini-language (core)

- **Parse/route layer** between the TUI input box and the agent: `/`-prefixed
  lines route to local command handlers, `@file` tokens are extracted as
  references, and an autocomplete overlay (commands + workspace files, the latter
  via a bounded `os.Root` walk) mirrors the approval-prompt overlay.
- **Commands**: `/clear` (drop the model's context, keep the visible transcript),
  `/continue` ("Please continue"), and `/compact` (generative compaction — the command
  surface and the `Agent.Compact` seam landed here; the reducer that folds the history
  through them shipped in the same track, see the "Context compaction (`/compact`)"
  section above).
- **`@file` references now resolve** (behaviour change): the loop reads each
  `UserInput.FileRefs` entry within the workspace fence (`security.SafeReadFile`,
  `os.Root`-pinned) and injects its content into the user message — replacing the
  prior "refs ignored" `ErrorEvent`. A missing, oversized, or escaping ref is
  reported and skipped; the Turn still proceeds.

### Public API (additive — minor)

- `Agent.ClearContext() error` — drop the conversation history at a quiescent
  boundary (the host's transcript is unaffected); refused mid-Exchange.
- `Agent.Compact(context.Context) (skipped bool, err error)` — on-demand generative
  Compaction: summarizes the conversation and folds the history at a quiescent boundary
  (refused mid-Exchange with `ErrInputPending`, like `ClearContext`). `skipped` is true
  when the conversation was too small past the protected prefix to fold — no upstream
  call, history untouched; always false on error.
- `UserInput.SkillIDs []string` — the skills attached in chat; the loop resolves
  each through `Config.Skills` and prepends its body to the Turn (was reserved).
- `Config.Skills SkillResolver` — host-supplied resolver for attached skill IDs
  (nil ⇒ attached IDs are reported and dropped). `SkillResolver` and its return
  type `ResolvedSkill` are re-exported on the root facade; the disk-backed catalog
  stays internal (`internal/skills`).

## [1.0.0] — 2026-06-25

The first stable release. `v1.0.0` cuts the public Go API after Phase 3 brought
the agent to feature-parity with apogee-code's non-UI behaviour, with **Auto
mode confined** on Linux (landlock) and macOS (seatbelt). Every consumer — the
TUI, the bench, and the embeddable library surface — has exercised the API, so
semver now begins (ADR 0001 §18, amended).

The public surface is the root `apogee` package: `Agent` (`New`/`Resume`),
`Config` and its host delegates (`EventSink`, `Approver`, `Asker`,
`ExternalEffects`), the four-rung `Mode` ladder, the `Tool`/`ToolRegistry`
extension point with the `ReadOnlyTool`/`ExternalEffectTool` markers, the
`Event` variants, and the hook points. Tools live behind the registry (an open
extension point, ADR 0002), not as root types.

### Confinement (Auto mode is real)

- **Blast-radius confinement model** (ADR 0012, supersedes ADR 0004): a tool
  call runs without a human gate only if its blast radius is bounded — by **OS
  confinement** for the unbounded subprocess/network surface, or by Apogee's own
  **path-safety-to-workspace** for its own in-process writes. Confinement
  attaches to blast radius, at a single **subprocess granularity** on every OS
  (no in-process per-thread landlock, no thread-discard).
- **Four-rung autonomy ladder**: Plan → Ask-Before → **Allow-Edits** → Auto.
  The new `ModeAllowEdits` rung auto-approves Apogee's own workspace-scoped
  writes (no confinement needed; identical on every OS) and gates everything
  else.
- **Linux landlock backend** (`//go:build linux`): ABI probed at startup; an
  honest capability matrix (`FSWrite` at ABI ≥1 / kernel ≥5.13, `NetworkEgress`
  at ABI ≥4 / kernel ≥6.7); a confined subprocess applies the landlock domain
  after fork, before `execve`, so the child is fenced and the parent stays
  unrestricted. Raw `golang.org/x/sys/unix` syscalls (now a direct dependency).
- **macOS seatbelt backend** (`//go:build darwin`): a `sandbox-exec` profile
  generated from the `ConfinementBox` (workspace-write-only + network-open by
  default), presence-probed, no new Go dependency.
- **`Confine(ctx, box, *exec.Cmd)`** prepare-in-place contract: the tool builds
  an idiomatic `*exec.Cmd`; the backend rewrites it to launch confined. The
  `confine-to-workspace` global-config key (default `true`) tunes Auto's blast
  radius; `confine-to-workspace=false` is the explicit "I am the sandbox"
  (VM-only) opt-out. `AutoEligible()` requires filesystem confinement only;
  where confinement is unavailable, subprocess tools gate through Approval
  ("confine if you can, gate if you can't") rather than refusing Auto.

### Tools (feature-parity with apogee-code's non-UI surface)

- **File-editing family**: find-replace (single + multi), `edit`/apply-edit,
  `diff`, `open-file` — pure-Go, stateless, carrying the unexported
  `workspaceScopedWriter` marker so Allow-Edits/Auto bound them by path-safety.
- **Execution tools**: `terminal` and `python-exec` — one-shot, stateless, the
  first `Confiner` consumers; process-group teardown on cancel
  (`Setpgid` + `cmd.Cancel` + `WaitDelay`).
- **`git` tool**: branch / commit / diff-range over the system `git`, detected
  and graceful-degrading when absent.
- **`diagnostics` tool**: in-process `go/parser` + optional `go vet`,
  read-only, graceful when the toolchain is absent.
- **Network + host tools**: `web_fetch`, `http_request`, `web_search`
  (external-effect, Approval-gated as MCP-kind / auto-run url-filtered as
  network-kind per the disposition table) and `ask_user` (the new `Asker` host
  delegate). These are routed through the `ExternalEffects.Do` boundary
  (ADR 0008) so the bench can stub them.
- The existing `read_file` / `write_file` / `list_dir` / `grep` built-ins carry
  forward; `write_file` carries the workspace-scoped-writer marker.

### Processing (parity-complete port)

- **All apogee-code tool-call formats parse**: native/JSON `tool_calls`,
  markdown-fenced, and custom-regex, each gated by **ported TS test vectors**.
- **Full harmony / thinking-channel set** handled, with a `processor-factory`
  that selects the format per model/response. The package stays `domain`-only.

### Security guardrails (the human-in-the-loop layer)

- **`internal/security`** consolidates the Phase-1 per-tool path-safety into one
  reusable guard and adds **url-safety**, an **arg-guard**, a **circuit-breaker**
  (halts a runaway tool-loop), and an **audit record** (bounded ring buffer with
  a dropped-count). These run in all modes and a sub-agent inherits them.
- **Two-tier dangerous-action guard** (a footgun-guard, NOT a security
  boundary): a hard-refuse tier (`rm -rf` of root/home/system, fork bombs,
  `~/.ssh`/credential/persistence writes) and a force-approval tier
  (`curl | bash`-class). It runs first and is **tighten-only**; project config
  may only add rules, never dissolve a floor rule by ID.
- **Default-on SSRF floor** for the network tools: loopback / private ranges /
  IMDS `169.254.169.254` / link-local / CGNAT / `0.0.0.0` / NAT64 denied by
  **resolved IP** (pre-flight and at dial time, closing DNS-rebinding),
  tighten-only.

### Sub-agents

- **Sub-agent orchestrator** (ADR 0013): a sub-agent is the embeddable `Agent`,
  constructed through an internal orchestrator that threads the parent's `Mode`,
  `Approver`, `Confiner`, and guardrails verbatim (or stricter) with a tool
  **`Subset` ≤ the parent's** (ADR 0005). It is exposed as a
  dispatch-transparent **`sub_agent`** recursion point — never confined or gated
  as a unit; each child tool call gets the full per-call disposition one level
  down.
- **Isolated live guard state** (`Guards.ForSubAgent`): a sub-agent gets a fresh
  circuit-breaker and audit log over a shared read-only dangerous ruleset.
- Nested events re-emit into the parent stream at **`Depth = parent.Depth + 1`**.
- Stepping is **top-level-only for v1** behind a swappable driver; a sub-agent
  runs atomically within the parent Turn (no mid-sub-agent snapshot; cancel
  rolls back to the parent's pre-`sub_agent` boundary).

### MCP

- **MCP client** on the official Go SDK (`modelcontextprotocol/go-sdk` v1.6.1):
  stdio / SSE / streamable-http transports. Server tools surface into the
  registry as `ExternalEffectTool` of kind `mcp`, so they **Approval-gate in
  Auto** under `confine-to-workspace=true` (an external server Apogee cannot
  fence). **Resume reconnects fresh** — no server-side-state promise (ADR 0008).

### TUI

- **Nested-event rendering**: `Depth > 0` sub-agent events render as a framed,
  labelled block (Phase-2's "tolerate" → "render").

### Notes

- Cross-build stays green on all 6 targets (linux/darwin/windows ×
  amd64/arm64, `CGO_ENABLED=0`); OS-specific confinement is build-tagged behind
  the `denyConfiner` (Windows/other) fallback. **Windows confinement is Phase 5**
  — Auto is simply unavailable on Windows until then.
- The `internal/` packages never import the root module path (ADR 0010).
- Direct dependency additions this release: `golang.org/x/sys` (landlock),
  `github.com/google/shlex` (terminal command splitting),
  `github.com/modelcontextprotocol/go-sdk` (MCP client).

### Known post-release verification (owner-run / CI)

These confinement **enforcement** proofs cannot run in the development
environment and are deferred to an owner-run / CI verification after the tag.
They are not acceptance failures — the hermetic disposition/logic tests (caps
honesty, generated profile strings, command rewriting, fail-closed paths) run
on every host and pass, and the live escape-probe batteries **self-skip loudly**
where the OS cannot enforce:

- **Linux landlock live enforcement** — ✅ **confirmed on a landlock-enabled
  kernel (2026-07-23).** Ran on an Ubuntu devbox, kernel **7.0.0-28-generic**
  aarch64 with `landlock` live in `/sys/kernel/security/lsm`; `apogee probe`
  reports `backend: landlock (fs-write: available · network: available)`, so
  `confinetest.Probe` runs live instead of self-skipping. Under a full
  `make check` (race detector on, cgo enabled) the landlock-tagged battery
  passes live: a confined subprocess's out-of-workspace and `~/`-profile writes
  are OS-denied, a non-allowlisted connect is denied while network-open connects,
  the domain is inherited across `exec`, and the parent stays unrestricted. The
  earlier caveat (dev-host kernel had `CONFIG_SECURITY_LANDLOCK` off) no longer
  applies to this box.
- **macOS seatbelt live enforcement** — ✅ **confirmed on macOS hardware
  (2026-07-02).** `confinetest.Probe` now runs under live `sandbox-exec` on a real
  Mac: a confined subprocess is fenced to the workspace, out-of-box and `~/.ssh`
  writes are OS-denied, the parent stays unrestricted, and network-deny tightens
  while network-open connects. (This surfaced and fixed the box-root canonicalization
  bug below.) The Linux landlock arm above is now closed too (2026-07-23).
- **Live Auto-confined deliverable run** — the opt-in `APOGEE_LIVE_ENDPOINT`
  end-to-end run (a real coding conversation in Auto, a shell write outside the
  workspace OS-denied, an MCP tool still raising Approval, a sub-agent delegated
  and its nested work rendered) is owner-run on Linux (landlock) and macOS
  (seatbelt). **Linux (landlock) arm ✅ confirmed (2026-07-23)** on an Ubuntu
  devbox (kernel 7.0.0-28-generic aarch64, landlock backend) against a real
  gemma-4-E4B endpoint: in `--mode auto`, step-1 out-of-workspace write
  (`echo … > ~/apogee-escape-test.txt`) was OS-denied with **no** approval prompt
  while the in-workspace write succeeded, the `demo__ping` MCP tool **still raised
  Approval**, and a delegated sub-agent's nested `NOTES.md` write **rendered** in
  the transcript; afterwards `~/apogee-escape-test.txt` was confirmed absent.
  macOS (seatbelt) arm still open.
- **Box-root canonicalization** — ✅ **resolved (2026-07-02).** Was a real bug, not
  just a verification gap: seatbelt embedded box roots verbatim and denied every
  in-workspace write when the root passed through a symlink (macOS `/var`, `/tmp`).
  Fixed by resolving each writable root through symlinks in `seatbeltProfile`; see
  the `[1.1.0]` Fixes entry.

[1.7.0]: https://github.com/airiclenz/apogee/releases/tag/v1.7.0
[1.6.0]: https://github.com/airiclenz/apogee/releases/tag/v1.6.0
[1.5.0]: https://github.com/airiclenz/apogee/releases/tag/v1.5.0
[1.4.0]: https://github.com/airiclenz/apogee/releases/tag/v1.4.0
[1.3.0]: https://github.com/airiclenz/apogee/releases/tag/v1.3.0
[1.2.0]: https://github.com/airiclenz/apogee/releases/tag/v1.2.0
[1.1.0]: https://github.com/airiclenz/apogee/releases/tag/v1.1.0
[1.0.0]: https://github.com/airiclenz/apogee/releases/tag/v1.0.0
