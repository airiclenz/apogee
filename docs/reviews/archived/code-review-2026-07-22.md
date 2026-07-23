# Code Review — Phase 5 Cross-Platform Hardening — 2026-07-22

**Scope:** Commit range `e762282..HEAD` on `main` — the Phase 5 implementation (Windows Confiner per ADR 0020, Shell/Path widening, Job-Object tree teardown, `apogee probe` per ADR 0021, provider logprobs, Cobra subcommand seam). Excludes the unrelated prompt-box plan doc (`1c69de8`) and the merge commit. ~7,100 inserted Go lines across `internal/platform`, `internal/probe`, `internal/library`, `internal/tools`, `internal/provider`, `cmd/apogee`.
**Mission:** A single-binary, cross-platform terminal coding agent for small local LLMs, whose Auto mode is OS-confined on all three platforms — on Windows via a restricted low-integrity token with the box expressed as revertible mandatory labels on the disk.
**Files reviewed:** ~70 Go files plus the governing docs (ADR 0020, ADR 0021 + Amendment, confinement execution contract, archived Phase 5 plan).

**Ground truth established before review:** `go build`, `go vet`, and `go test` pass for all touched packages on Linux; all six cross-targets build CGO-free; `GOOS=windows go vet` is clean.

## Executive Summary

The core of Phase 5 is faithful to its paperwork: the token backend implements ADR 0020's fence, guardrails, and fail-closed decisions nearly clause-for-clause; ADR 0021's binding requirements (promotion statement, `--no-save`, printed undo path, no config write) are implemented and tested; layering is clean; and the security lens found **no** finding that clears the bar under the ADR 0012 threat model — the box is built solely from config, `Confine` fails closed on every branch, reparse points are skipped on both the label and clear walks, and the journal/probe-record homes sit outside any realistic box.

The defects cluster in one place: **the Windows label-journal lifecycle — the failure and cleanup paths that have never run live.** The journal that exists to make the disk mutation revertible is deleted even when the revert fails, can record apogee's *own* Low label as the user's prior state (making teardown restore the label instead of removing it, self-perpetuatingly), is skipped entirely when `%USERPROFILE%` is unresolvable, and is written non-atomically. Compounding this, `apogee probe host` heals-or-destroys the very residue it promises to report before reading it. Since none of this code has run natively on Windows beyond the happy-path battery, these paths should be fixed **before** the owner's live-enforcement proofs. A second, unrelated High: the terminal tool gates every command through a POSIX `shlex` parse even when the target shell is cmd.exe, rejecting ordinary Windows command lines.

## Intent & Architecture Findings

### High — `probe host` destroys or heals the residue it promises to report, and writes on a read-only command `[Intent + Correctness]`

- **Where:** `cmd/apogee/probe.go:79` and `:91`; `internal/platform/confiner_windows.go:105–116, 130`
- **What:** In the `probe.Inputs` composite literal, `platform.NewConfiner()` (field at line 79) is evaluated before `platform.ConfinementResidue()` (line 91). On Windows the constructor's `recoverLabelJournals` reverts labels, writes ACLs, and deletes journal files left by a crashed run — *then* the residue line is read. So the interrupted-run case ADR 0020 §2 explicitly assigns to `probe host` ("`apogee probe host` reports an outstanding journal so the state is visible off-session") can never be reported for a dead run; only another *live* apogee's journal can appear. Meanwhile ADR 0021 §1, README, and the command's own `Long` text ("It never writes") all pin the host report as read-only, and the comment at `probe.go:85–90` claims "the host half stays read-only" on the very call that follows the recovery writes.
- **Why it matters:** The one surface built to diagnose an interrupted cleanup silently repairs (or — see the journal finding below — destroys the record of) the state it was asked to describe. Combined with the failed-revert journal deletion, a probe run after a crash can leave labels on disk with no record and report a clean machine.
- **Fix:** Compute `residue := platform.ConfinementResidue()` into a local *before* constructing the Confiner in both `probe` and `probe host` paths (or give the probe a constructor variant that skips recovery). Then amend ADR 0021 §1 / README with the recovery exception, or make the probe path truly read-only.

### High — the terminal tool validates cmd.exe lines with a POSIX splitter `[Intent + Correctness]`

- **Where:** `internal/tools/terminal.go:74–80`
- **What:** `shlex.Split(args.Command)` gates every command "before handing it to the shell", unconditionally — but on Windows the shell is cmd.exe (the tool's own schema says "POSIX sh on Unix, cmd on Windows", and this range wired `shellHost.Command/CommandLine` for exactly that). Valid cmd.exe lines are rejected before reaching the shell: `echo don't panic` (odd apostrophe → "EOF found when expecting closing quote"), `dir "C:\Program Files\"` (`\"` read as an escaped quote), any trailing bare backslash.
- **Why it matters:** Ordinary model-written Windows commands fail with "could not parse command line" on one of the three shipped platforms — in normal use, not an edge case. Phase 5's point was making this tool real on Windows.
- **Fix:** Apply the shlex gate only when the platform shell is POSIX sh (e.g. skip when the host's `CommandLine()` form is in use, or via a Host predicate); if a pre-flight is wanted on Windows, a balanced-double-quote check is the cmd.exe-shaped equivalent. Also update the P3.8 "shlex-validated" note in `docs/design/technical-design.md` §5, which now documents a defect.

### Medium — the Windows box has no `%TEMP%`/toolchain-cache story, and nothing populates `ConfineWritablePaths` — warrants revisiting contract §7 / ADR 0020 §2

- **Where:** `internal/agent/dispatch.go:121–125` (sole reader of `ConfineWritablePaths`; repo-wide grep finds no writer); ADR 0020 §2 ("a Low child with an unlabelled `%TEMP%` cannot run `go build` at all … a hard prerequisite")
- **What:** ADR 0020 names box-local `%TEMP%` / labelled toolchain caches a *hard prerequisite* for real work under the Windows fence. Item 6 built `ScopeEnv`, but only `git` uses it; `terminal`/`python_exec` inherit the user's (Medium-labelled) `%TEMP%`, and no code path ever sets `Config.ConfineWritablePaths`.
- **Why it matters:** On a capable Windows host in Auto, any confined `go build` / `go test` / `pip` / `npm` fails with access-denied *inside* the fence. The README/CHANGELOG headline "Auto confined on all three platforms" holds only for workspace-only writes (the live evidence was an `echo` redirect). The gap is acknowledged only in a deferred cell of technical-design §5; `TODO.md` carries nothing.
- **Fix:** At minimum a named `TODO.md` follow-on plus a README caveat in the Windows Auto section. The cheap remedy ADR 0020 itself names: a box-local `%TEMP%` via `ScopeEnv` on the confined path.

### Medium — `probe model`'s auto-apply prediction is a diverged duplicate of the startup ladder

- **Where:** `cmd/apogee/probemodel.go` (`autoApplyKeys`) vs `cmd/apogee/validatedsets.go:83`
- **What:** `autoApplyKeys` re-implements `resolveValidatedSet`'s decision (bypass/enable checks, `Shipped`+`LoadUserDir`+`Merge`+`Match`, mechanisms-block precedence) but omits the `validated.Validate(entry, mechanisms.Descriptors())` step. For an entry that fails catalogue validation (e.g. a stale user-local set naming a removed mechanism ID), `probe model` prints "Validated set X now AUTO-APPLIES for this model" while the next startup prints "skipping validated-set entry X" and applies nothing.
- **Why it matters:** This is precisely the false-effect claim the ADR 0021 Amendment built the `Promoted`/`Suppressed` reporting to prevent.
- **Fix:** Add the `Validate` check to `autoApplyKeys`. The twin ladders themselves are a candidate for `/improve-codebase-architecture` — one shared "would this entry apply" function consulted by both startup and the probe report.

### Medium — `Path.ExecExt` is dead surface, against the plan's own acceptance rule

- **Where:** `internal/platform/platform.go:63–67`, `internal/platform/host.go:96–97`
- **What:** Repo-wide grep finds only the definition and its tests — zero production callers. The archived Phase 5 plan item 6 states: "No dead surface: the verifier rejects any added method with no caller landed by the end of this plan."
- **Why it matters:** A public-ish seam with nothing behind it decays; the plan explicitly forbade this outcome.
- **Fix:** Delete the method (re-add with its first real caller), or land the caller now.

## Critical & High Findings

### High — the label journal is destroyed even when the revert FAILS, and the composition root swallows the only surviving error `[Correctness + Intent — cross-validated]`

- **Where:** `internal/platform/confiner_windows.go:289–297` (`restoreLabels`), `:356–369` (`recoverLabelJournals`); `cmd/apogee/wire.go:136–138`
- **What:** Both undo paths delete the journal unconditionally: `restoreLabels` runs `revertLabelJournal` and then `os.Remove(c.journalPath)` regardless of the revert error; `recoverLabelJournals` does `_ = revertLabelJournal(j); _ = os.Remove(path)`. The composition root then discards the one remaining signal: `defer func() { _ = closer.Close() }()`.
- **Why it matters:** A box root on an offline share at shutdown, or any `SetNamedSecurityInfo` failure mid-restore — routine territory per ADR 0020 §3's own words — leaves the user's tree permanently carrying apogee's Low labels (writable by every Low-integrity process on the machine: the cost ADR 0020 accepted *because* labels are reverted). The record needed for the documented `icacls` remedy is gone, and the `probe host` residue line can never fire. This contradicts ADR 0020 §2 ("journalled against a crash … visible off-session"; "a tool that mutates ACLs and does not clean up after itself has not earned `--mode auto`").
- **Fix:** Remove the journal only when `revertLabelJournal` returned nil — otherwise retain it (or rewrite it with just the failed entries) so the next `NewConfiner` retries and `ConfinementResidue` reports it. Print the `Close()` error to stderr in `wire.go` instead of discarding it.

### High — the journal can record apogee's own Low label as the user's "prior" state; teardown then RESTORES the label instead of removing it `[Correctness]`

- **Where:** `internal/platform/confiner_windows.go:217–232` (`labelBox`), `:271–276` (`labelTree`); `internal/platform/winconfine.go:101–109` (`priorLabels`)
- **What:** Journal entries are appended with no per-path dedupe and no recognition of the backend's own label SDDL, and `priorLabels()` is a map build — last entry wins (entries with empty `PriorSDDL` are skipped). Two realistic triggers: **(a)** a label pass fails after `setLabelSDDL(root, Low)` succeeded (a `WalkDir` error on the root, or a `flushJournal` failure mid-walk) — `labelled[root]` stays false, so the next `Confine` of the same box re-reads the root, sees the Low label apogee just applied, and journals it as the prior state, overriding the first entry's true prior of "no label"; **(b)** two concurrent apogee sessions in the same workspace — session 2 reads session 1's transient Low label as "prior".
- **Why it matters:** Teardown then clears the tree and *restores the Low label onto the root*, so a "clean" `Close` leaves the workspace carrying an inheritable Low label. It is self-perpetuating: every later session journals the residue as prior state and faithfully re-restores it; files created between sessions inherit Low; any Low-integrity process on the machine can write into the workspace. Breaks the stated invariant "teardown reverts labels."
- **Fix:** When appending a journal entry, skip paths already journalled (case-folded compare — the `labelled` memo map should fold too), so the first-recorded prior wins; additionally never record a prior that is exactly `windowsDirLabelSDDL`/`windowsFileLabelSDDL` (apogee's own spelling).

### High — the journal-write-fails ⇒ refuse-to-confine contract has no test, and journal-before-first-label ordering is unproven `[Tests]`

- **Where:** `internal/platform/confiner_windows.go:226–228` and `:273–275`
- **What:** `labelBox` deliberately fails closed when the journal cannot be flushed (`ErrConfinementUnavailable`). No test — untagged or windows-tagged — exercises a failing flush; `TestWindowsInterruptedRunIsRecoveredFromTheJournal` only observes a journal after a successful pass.
- **Why it matters:** A regression that labels first and journals second, or swallows the flush error and labels unjournalled, passes the entire suite *including the owner's live battery* — silently deleting the crash-recovery guarantee that legitimises the disk mutation.
- **Fix:** A windows-tagged test: construct `newTokenConfiner(home)` where `home/confinement` pre-exists as a *file* (so `MkdirAll` fails), call `Confine` with a valid box, assert `errors.Is(err, ErrConfinementUnavailable)` **and** `readLabelSDDL(root) == ""` (nothing was labelled without a journal).

## Medium Findings

### Medium — labelling proceeds journal-less when the user profile is unresolvable `[Correctness]`

- **Where:** `internal/platform/confiner_windows.go:121–131`, `:300–303`; `internal/platform/winconfine.go:251–257`
- **What:** When `os.UserHomeDir()` fails, `confinementJournalHome()` returns "", `journalPath` stays empty, `flushJournal` becomes a silent no-op — yet `Confine` still labels the disk. This violates the stated invariant "the journal is written BEFORE any label is applied": a crash leaves Low labels with no record and no recovery path.
- **Fix:** In `labelBox`, return `ErrConfinementUnavailable` when no journal path is available (the doc comment's "home may be \"\" in a test" carve-out can stay, but production labelling must not ride on it).

### Medium — journal writes are truncate-in-place, and an unreadable journal is invisible to both recovery and the residue report `[Correctness]`

- **Where:** `internal/platform/winconfine.go:270–282` (`writeLabelJournal`), `:334–347` (`confinementResidue`); `internal/platform/confiner_windows.go:359–362` (`recoverLabelJournals`)
- **What:** `writeLabelJournal` uses in-place `os.WriteFile` (no temp+rename), and the journal is rewritten on every pre-labelled descendant found during the walk, so the corruption window recurs. A journal that fails `readLabelJournal` is silently `continue`d by both `recoverLabelJournals` (never reverted, never removed) and `confinementResidue` (never reported).
- **Why it matters:** A crash or power loss mid-flush yields a truncated journal → invisible permanent labels, with the residue reporter claiming a clean disk.
- **Fix:** Write to a temp file and rename. Treat an unreadable journal as reportable residue ("journal present but unreadable: <path>") rather than skipping it.

### Medium — Job Object handles leak on the two routine early-exit paths `[Correctness]`

- **Where:** `internal/tools/exec_common.go:125–138`; `internal/tools/exec_teardown.go:74–79`; `internal/tools/exec_pgroup_other.go:45–54`
- **What:** `setProcessGroupTeardown` creates the Job Object handle immediately, before `Confine` runs. (a) A `Confine` failure returns from `runSubprocess` before `runWithTeardown` is ever called; (b) `runWithTeardown` returns on a `cmd.Start()` error *before* `defer td.release()` is installed. Neither path closes the handle.
- **Why it matters:** Both triggers are routine on Windows by the project's own docs — per-run `ErrConfinementUnavailable` is "routine" (ADR 0020 §3), and a Start failure is any mistyped command a model emits. One leaked kernel handle per occurrence for the session's lifetime.
- **Fix:** In `runSubprocess`, `defer teardown.release()` immediately after `setProcessGroupTeardown` — `release` is already idempotent via the `InvalidHandle` guard.

### Medium — the below-floor build gate has no test and no seam to ever get one `[Tests]`

- **Where:** `internal/platform/confiner_windows.go:112`
- **What:** The floor check is an inline ambient read (`windows.RtlGetNtVersionNumbers()` inside `NewConfiner`). The windows-tagged capability test only errors if the *host itself* is below floor, so the below-floor ⇒ `denyConfiner` branch is unreachable even on the owner's machine. ADR 0020's "below-floor is UNTESTED" carve-out covers live enforcement, not this selector.
- **Fix:** Extract `selectConfiner(build uint32)` (or `belowWindowsFloor(build) bool`) and table-test 17762 ⇒ deny / 17763 ⇒ token on any OS.

### Medium — the startup half of probe-record promotion is only tested with an empty probe dir `[Tests]`

- **Where:** `cmd/apogee/validatedsets.go:42–46`; `internal/agent/loop.go:161–165`; `cmd/apogee/validatedsets_test.go`
- **What:** Both call sites gained the Medium rung (`ResolveFingerprintFrom{…, ProbeDir}`) in this range, but every `resolveValidatedSet` test threads a fresh `t.TempDir()` as the probe dir. The user-visible payoff of `apogee probe model` — a matching Validated set flips from offered to APPLIED at next session start — is verified only via `autoApplyKeys`, the probe command's own mirror of the ladder (which, per the finding above, already drifts).
- **Fix:** One test: `SaveProbeRecord` for (endpoint, shipped-set key), then `resolveValidatedSet(baseOpts(key), userDir, probeDir)` asserting the set now **applies** where the existing `DirectLowMatchOffers` case only offers.

### Medium — `probe model`'s pre-spend refusal gates are untested `[Tests]`

- **Where:** `cmd/apogee/probemodel.go:98–110`
- **What:** The no-endpoint refusal is tested, but not the discovery-failure return nor the `errProbeModelNeedsLabel` gate (a server advertising no active model) — the gates that stop the command spending tokens and "minting identity from absent evidence."
- **Fix:** An `httptest` server whose `/v1/models` returns `{"data":[]}`; run `probe model` without `--model`; assert the "advertises no active model" error, zero `/chat/completions` hits, and an untouched config home.

### Medium — `TestGatherModelWritesNothing` asserts nothing `[Tests]`

- **Where:** `internal/probe/model_test.go:60–71`
- **What:** The test creates `home := t.TempDir()`, runs `gatherModel` — which is never told about `home` (`GatherModel` takes no path) — then asserts the unrelated directory is empty. It passes even if `GatherModel` wrote to CWD or a real `~/.apogee`.
- **Fix:** Assert against a redirected `HOME`/CWD, or delete it — the cmd-level `TestProbeModelNoSaveWritesNothing` covers the real contract for the configured home.

### Medium — `windowsQuote` lacks adversarial rows on a security-relevant function `[Tests]`

- **Where:** `internal/platform/host_test.go:78–87`; `internal/platform/host.go:328–332`
- **What:** The quoting table covers plain/space/embedded-quote/trailing-backslash/empty only. Missing: a backslash run immediately before an embedded quote (`a\"b` — where cmd.exe's `""` rule and `CommandLineToArgvW`'s `\"` rule collide), multiple trailing backslashes, cmd metacharacters inside quotes, and a row pinning the documented non-guarantee that `%VAR%` is not neutralised (currently comment-only).
- **Why it matters:** `Quote`'s consumer is the confinetest escape battery's write lines — a quoting-mangled command also yields "non-zero exit AND no file", the exact signature `assertDenied` reads as a kernel denial. A quoting bug could make the battery pass vacuously.
- **Fix:** Add the adversarial rows; where behaviour is deliberately unguaranteed (`%VAR%`), pin it with a test row naming the intent.

## Recommended Action Order

1. **Fix the journal lifecycle as one unit** (journal kept on failed revert + own-label/dedupe guard + journal-less labelling refusal + atomic write + unreadable-journal residue): these five findings share ~3 functions in `confiner_windows.go`/`winconfine.go`, and each one alone still leaves permanent-label scenarios open. Add the fail-closed flush test in the same change.
2. **Reorder `probe.go` residue-before-constructor** — a two-line fix that makes the diagnosis surface real; decide at the same time whether construction-time recovery is amended into ADR 0021 or suppressed on the probe path.
3. **Gate the terminal shlex check by shell family** — small fix, user-visible on every Windows session.
4. **Release the Job Object teardown via a defer at the call site** — one-line fix.
5. **Add `validated.Validate` to `autoApplyKeys`** and the promotion-with-record startup test; consider `/improve-codebase-architecture` for the twin apply-ladders afterwards.
6. **Decide the `%TEMP%`/writable-paths story** (needs a design call: box-local `%TEMP%` via `ScopeEnv` vs labelled caches) — at minimum record the TODO and README caveat now, before an owner-run live Auto session trips over `go build`.
7. Remaining test gaps (floor-gate seam, probe-model refusals, vacuous test, quoting rows) and the `ExecExt` deletion — mechanical, any order.

## What Looked Good

The security posture of the fence itself is solid against the stated threat model: the box is built solely from config (never from model input), every `Confine` branch fails closed into the gate-not-unconfined disposition, reparse points are skipped on both the label and clear walks (so a model-planted junction can't redirect labelling), the journal and probe-record homes are guardrail-protected out of any realistic box, git tools pass argv arrays with `--` termination and leading-dash rejection, and the battery parses but never dispatches a hostile endpoint's tool calls. The portable decision logic — the path rule table (component-boundary `Contains`, 8.3, `\\?\`, UNC, case-folding), box-root collapse, guardrails, SDDL decisions, probe-record defect ladder, fingerprint rungs, and the ADR 0021 Amendment's promote-don't-mint semantics — is well-factored, genuinely table-tested on Linux, and matches its ADRs nearly clause-for-clause. The windows-tagged battery rows assert real behaviour (live PID checks, label-revert verification), not tautologies.
