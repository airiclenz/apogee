# Slow-box follow-ups and stale-state defects

**Goal:** Close the six follow-ups the slow-box deadlines plan left behind, and four latent defects
where a piece of state outlives the moment it described: a reset tick clearing a fresher arm, an undo
walk interleaved by a snapshot call, a closed undo group re-closed by a later exchange, and a
no-repository answer memoised past a `git init`.

**Date:** 2026-09-23
**Status:** unexecuted
**sized for:** ~200k-context host
**skills:** coding-standards
**base:** `b8d74481`

**Regression check (2026-09-23, b8d74481):**
- 4: guard folded (the measured case is named under the `TestExtractPDF_RefusesAnAbsurd` prefix, serial, preflight cannot refuse first).
- 6: guard folded (writer decision: `effectiveBatteryTimeout` lives in `cmd/apogee/probemodel.go`).
- 7: guard folded (the flash case is reachable by the Acceptance filter; a zero-gen filler is harmless, not stale).
- 8: guard folded (serialised MarkPre/Close assertions; every walk-lock comment updated, `internal/undo/doc.go` added).
- 9: guard folded (every memo / no-repository comment updated, `internal/gitexec/doc.go` added).
- 10: guard folded (Close awaits the walk before reading `pending`; comment sweep widened to `internal/agent/*.go`, `loop.go` added).

## Authoritative sources

- The ten beads: `bd show apogee-undo-chat-turn-recloses apogee-adr-0080-shadow-timeout apogee-probe-host-nil-cause
  apogee-tool-schema-render-untested apogee-pdf-ceiling-not-discriminating apogee-lak9
  apogee-probe-model-timeout-zero apogee-ukw apogee-eykc apogee-se0`.
- `docs/plans/archived/2026-09-22 - 01 - slow-box-deadlines-plan.md` — items 2, 3, 6, 7, 10.
- `docs/plans/archived/2026-09-22 - 00 - audit-design-findings-plan.md` — item 1 (the lock-free walk).
- `docs/plans/archived/2026-09-20 - 02 - fence-sanitize-and-defects-plan.md` — item 7b (the memo rule).
- `docs/adr/0051` decision 7, `docs/adr/0056` decision 4, `docs/adr/0080` decision 6.

## Ratified design calls

- **`--timeout 0` (owner, 2026-09-23):** zero means the five-minute default, never unbounded.
- **apogee-se0 (owner, 2026-09-23):** a probe that reaches no repository is not memoised; no `.git` path is assumed and no directory is fingerprinted.
- **apogee-eykc (owner, 2026-09-23):** `Close`, `MarkPre`, `Preview` and `RedoPreview` wait for a running walk to land (context-aware), never refuse.
- **apogee-ukw scope (owner, 2026-09-23):** `flashClearMsg` is stamped in the same item as the esc and ctrl+c resets.

## Standing requirements

- skills: coding-standards
- Every budget changed here keeps its reason in the comment beside it.

## Out of scope

- `apogee-winlabel-journal-secret` (Windows-only remainder of `apogee-73s`).
- The esc window being measured at fold time rather than at the keypress (Bubble Tea v2's
  `tea.KeyPressMsg` carries no timestamp); `apogee-ukw` covers the stale tick only.
- `treeSnapshotTimeout` and the tree-snapshot floor's silent skip — unchanged; ADR 0056 decision 4
  still governs them.

## 1. ADR 0080 and ADR 0056 record the shipped commit-secrets budget — ✅ DONE (2026-09-23)

NOTES (2026-09-23): the stale-site sweep (`rg -n '2 ?s' docs/manual CONTEXT.md docs/design internal/agent/secretsguard.go` plus a commit-secrets skip/timeout grep) found no other live prose stating a 2 s or skip-on-any-failure commit-secrets contract.

**What.**
**Goal:** ADR 0080 decision 6 and its Consequences carry a dated amendment stating the shipped
contract: one budget (`commitSecretsTimeout`, 30 s) bounds all four shadow runs, and an expired budget
or a git failure after the repository resolved forces the approval look; "not a repository" still
skips. ADR 0056 decision 4 carries a dated note that its silent-skip rule no longer governs the
commit-secrets pre-check while still governing the tree snapshot. The code comment beside
`commitSecretsTimeout` says the same thing.

**Approach (assumed at the header base):** add `## Amendment (2026-09-23) — …` to
`docs/adr/0080-git-commit-forces-a-look-at-staged-secret-material.md` (the house heading form, as in
ADR 0002), pointing at decision 6 and the Consequences sentence ("bounded by the timeout … skipped the
moment git is unavailable"); add an inline `**Amended 2026-09-23:**` note to ADR 0056 decision 4 beside
its existing 2026-08-26 amendment. In `internal/agent/secretsguard.go`, the comment that says the
change "supersedes ADR 0080 decision 6 and ADR 0056 decision 4" is corrected: it supersedes 0080 d6 and
narrows 0056 d4 (which still governs the tree snapshot). Rule for any other stale site: every prose
line outside the historical record (`docs/adr/` bodies above the amendment, `docs/plans/archived/`,
`docs/reviews/`, `CHANGELOG.md`) stating a 2 s commit-secrets timeout or a skip-on-any-failure
commit-secrets contract — find with `rg -n '2 ?s' docs/manual CONTEXT.md docs/design internal/agent/secretsguard.go`.

**Files:** docs/adr/0080-git-commit-forces-a-look-at-staged-secret-material.md, docs/adr/0056-terminal-fail-fast-and-session-scratch.md, internal/agent/secretsguard.go
**Read first:** internal/agent/secretsguard.go — robustness-contract header comment, commitSecretsTimeout, commitSecretsCheck; docs/adr/0080-git-commit-forces-a-look-at-staged-secret-material.md — Decision 6, Consequences;
docs/adr/0056-terminal-fail-fast-and-session-scratch.md — decision 4 and its "Amended 2026-08-26" note; docs/adr/0002 — "## Amendment (2026-08-20)" heading form;
docs/manual/configuration.md — commit-secrets paragraph (already states the incomplete-scan look); internal/agent/treesnapshot.go — treeSnapshotTimeout
**Closes:** apogee-adr-0080-shadow-timeout

**Tests.** Docs and comment only — no Go test.

**Acceptance.** `rg -q 'Amendment \(2026-09-23\)' docs/adr/0080-git-commit-forces-a-look-at-staged-secret-material.md && rg -q 'Amended 2026-09-23' docs/adr/0056-terminal-fail-fast-and-session-scratch.md && go build ./internal/agent/`

**Commit:** `docs(adr): ADR 0080 and 0056 record the one-budget commit-secrets contract`

## 2. A host probe with no Confiner reports the backend absent — ✅ DONE (2026-09-23)

**What.** Fixes the one site breaking `domain.ConfinementCaps.Cause`'s invariant (a cause is set
exactly when `FSWrite` is false), introduced by slow-box plan item 3.
**Goal:** `probe.GatherHost` with a nil `Confiner` returns caps whose `Cause` is
`domain.CauseBackendAbsent` and whose `FSWrite` is false; rendered `apogee probe host` output is
unchanged.

**Approach (assumed at the header base):** in `internal/probe/host.go` `GatherHost`, the zero-value
`domain.ConfinementCaps{}` used when `in.Confiner == nil` gains `Cause: domain.CauseBackendAbsent`
(the same cause the platform no-backend stub returns in `internal/platform/platform.go`). Leave
`Unavailable` as today — the rendered lines read only `Unavailable`.

**Files:** internal/probe/host.go, internal/probe/host_test.go
**Read first:** internal/probe/host.go — GatherHost, Host.Report; internal/probe/confinement.go — CapabilityLine, DegradedNotice (read Unavailable only, never Cause);
internal/domain/confinement.go — ConfinementCaps.Cause, CauseBackendAbsent; internal/platform/platform.go — no-backend stub Capabilities;
internal/probe/host_test.go — fakeConfiner, TestReportCapableHostAndLlamaCppEndpoint
**Closes:** apogee-probe-host-nil-cause

**Tests.** New `TestGatherHostWithoutAConfinerReportsTheBackendAbsent` in
`internal/probe/host_test.go`: nil `Confiner` ⇒ `Caps.FSWrite == false` and
`Caps.Cause == domain.CauseBackendAbsent`. Must fail against the pre-item tree.

**Acceptance.** `go test -count=1 ./internal/probe/`

**Commit:** `fix(probe): a host probe with no Confiner reports the backend absent`

## 3. terminal and python_exec pin the timeout figures their schemas render — ✅ DONE (2026-09-23)

**What.** Test-only. Mirror `TestRunTestsSchemaAdvertisesTheTimeoutItApplies`
(`internal/tools/run_tests_test.go`) for the two tools whose `timeout_seconds` description is
rendered from `subprocess.DefaultSubprocessTimeout` and `subprocess.MaxSubprocessTimeout`.

**Files:** internal/tools/terminal_test.go, internal/tools/python_exec_test.go
**Read first:** internal/tools/run_tests_test.go — TestRunTestsSchemaAdvertisesTheTimeoutItApplies; internal/tools/terminal.go — terminal spec timeout_seconds render, NewTerminal;
internal/tools/python_exec.go — pythonExecSpec render, NewPythonExec; internal/tools/terminal_test.go — imports (no encoding/json yet); internal/tools/python_exec_test.go — imports;
internal/subprocess — DefaultSubprocessTimeout, MaxSubprocessTimeout
**Closes:** apogee-tool-schema-render-untested

**Tests.** New `TestTerminalSchemaAdvertisesTheTimeoutItApplies` (terminal_test.go, via
`NewTerminal(t.TempDir(), nil).Schema()`) and `TestPythonExecSchemaAdvertisesTheTimeoutItApplies`
(python_exec_test.go, via `NewPythonExec`): unmarshal into
`struct{ Properties map[string]map[string]any }`, assert `timeout_seconds` is `integer` and its
description contains `fmt.Sprintf("default %d", int(subprocess.DefaultSubprocessTimeout.Seconds()))`
and the `max %d` counterpart from `MaxSubprocessTimeout`.

**Acceptance.** `go test -count=1 -run 'SchemaAdvertisesTheTimeout' ./internal/tools/`

**Commit:** `test(tools): terminal and python_exec pin the timeout figures their schemas render`

## 4. The absurd-/Size allocation ceiling bites on a parse that would allocate — ✅ DONE (2026-09-23)

NOTES (2026-09-23): the classic-trailer case TestExtractPDF_RefusesAnAbsurdXrefSize now runs under t.Parallel() — with its TotalAlloc ceiling dropped it measures nothing, so the file's serial rule for allocation-measured cases no longer applies to it.
NOTES (2026-09-23): bite check run — with refuseAbsurdObjectCount short-circuited to "" the new case fails on the ceiling (64026808 bytes allocated against 8388608) as well as on the message; pdf.go restored afterwards.

**What.** Test-only. `TestExtractPDF_RefusesAnAbsurdXrefSize` measures an 8 MiB `TotalAlloc` ceiling,
but its fixture (`testdata/minimal.pdf`, a classic `xref`/`trailer`) makes an unrefused parse allocate
nothing proportional to `/Size` — `ledongthuc/pdf`'s classic table grows by `append`. Only
`readXrefStream` preallocates (`make([]xref, size)`, ~32 bytes per entry).
**Goal:** an allocation-measured case exists whose fixture's `startxref` points at a `/Type /XRef`
stream object declaring a `/Size` above `len(data)` (so `refuseAbsurdObjectCount` refuses it) yet
small enough that an unrefused parse allocates well above the ceiling and still fits in memory
(e.g. `/Size 2000000` ⇒ ~64 MB against the 8 MiB ceiling); the classic-table case no longer claims an
allocation bite it cannot have.

**Regression guard.** Name the new case `TestExtractPDF_RefusesAnAbsurdSizeInAnXrefStreamAllocation`
(any `TestExtractPDF_RefusesAnAbsurd…` name, so the Acceptance filter runs it), with no `t.Parallel`.
Keep `/W [1 2 1]` and the stream uncompressed or small, so `preflightPDF` / `refuseAbsurdXrefWidths`
cannot refuse first during the bite check.

**Approach (assumed at the header base):** in `internal/doctext/pdf_test.go`, build the xref-stream
fixture inline (a small builder beside `hostilePDF`) and add the measured case serially (the file's
recorded no-parallel rule for allocation-measured cases), asserting the refusal message
(`"declares 2000000 objects in"`) and the ceiling. Drop the `TotalAlloc` ceiling from the classic
case, keeping its refusal-message assertions, and say why in its comment.

**Files:** internal/doctext/pdf_test.go
**Read first:** internal/doctext/pdf_test.go — TestExtractPDF_RefusesAnAbsurdXrefSize, TestExtractPDF_RefusesAnAbsurdSizeInAnXrefStreamDictionary, hostilePDF;
internal/doctext/pdf.go — ExtractPDF, refuseAbsurdObjectCount, preflightPDF, refuseAbsurdXrefWidths, pdfAbsurdSizeFormat;
ledongthuc/pdf read.go — NewReaderEncrypted, readXrefStream, readXrefTableData

**Tests.** The new case, `TestExtractPDF_RefusesAnAbsurdSizeInAnXrefStreamAllocation` (serial); bite
check: with `refuseAbsurdObjectCount` short-circuited to `""` the new case fails on the ceiling (not
only on the message).

**Acceptance.** `go test -count=1 -run 'TestExtractPDF_RefusesAnAbsurd' ./internal/doctext/`

**Closes:** apogee-pdf-ceiling-not-discriminating

**Commit:** `test(doctext): the absurd-/Size allocation ceiling bites on an xref stream`

## 5. A git diff or show gets at least the ordinary git budget

**What.** Fixes `gitDiffTimeout` (10 s) being shorter than `gitTimeout` (15 s) while its comment
says a diff "can be larger".
**Goal:** `gitDiffTimeout >= gitTimeout`, pinned by a test; the comments on `gitDiffTimeout` and
`gitReadTimeout` agree with the values and no longer claim to match an external oracle's ceiling.

**Approach (assumed at the header base):** in `internal/tools/git.go` set
`gitDiffTimeout = 30 * time.Second` (twice the ordinary budget: a diff-range or a blob read on slow
storage is the large case), rewrite its comment and `gitReadTimeout`'s to state that reason.

**Files:** internal/tools/git.go, internal/tools/git_test.go
**Read first:** internal/tools/git.go — gitTimeout, gitDiffTimeout, gitReadTimeout, gitRead, runGit; internal/tools/git_test.go — package-internal tests using gitTimeout (RunGitQuery cases)
**Closes:** apogee-lak9

**Tests.** New `TestGitReadTimeoutGivesDiffAndShowAtLeastTheOrdinaryBudget` in `git_test.go`:
`gitReadTimeout("diff")` and `gitReadTimeout("show")` are `>= gitTimeout`, and `gitReadTimeout("log")`
equals `gitTimeout`. Must fail against the pre-item tree.

**Acceptance.** `go test -count=1 -run 'TestGitReadTimeout' ./internal/tools/`

**Commit:** `fix(tools): a git diff or show gets at least the ordinary git budget`

## 6. apogee probe model --timeout 0 keeps the default

**What.** Fixes `--timeout 0` (and any negative value) silently unbounding each battery attempt,
because `provider.WithRequestTimeout` maps zero to "the caller's context governs".
**Goal:** `apogee probe model --timeout 0` and a negative `--timeout` both bound each battery attempt
by `batteryRequestTimeout` (five minutes), following `provider.WithDiscoveryTimeout`'s "zero or
negative keeps the default"; the flag's help text and `docs/manual/probe.md` say so.

**Regression guard.** the effectiveBatteryTimeout helper lives in cmd/apogee/probemodel.go (production code the client construction calls), not in the test file; the test table-tests it.

**Approach (assumed at the header base):** in `cmd/apogee/probemodel.go` RunE, before
`provider.NewClient(… provider.WithRequestTimeout(batteryTimeout) …)`, a non-positive
`batteryTimeout` becomes `batteryRequestTimeout`. Extend the flag's usage string and the `--timeout`
sentence in `docs/manual/probe.md`.

**Files:** cmd/apogee/probemodel.go, cmd/apogee/probemodel_test.go, docs/manual/probe.md
**Read first:** cmd/apogee/probemodel.go — batteryRequestTimeout, newProbeModelCommand RunE (provider.NewClient with WithRequestTimeout), --timeout flag registration;
internal/provider/client.go — WithRequestTimeout, WithDiscoveryTimeout, attemptContext; cmd/apogee/probemodel_test.go — TestProbeModelTimeoutFlagBoundsTheBatteryAttempt, stalledModelUpstream, upstreamHome;
docs/manual/probe.md — the --timeout sentence
**Closes:** apogee-probe-model-timeout-zero

**Tests.** The value the client receives is factored into a small pure
`effectiveBatteryTimeout(d time.Duration) time.Duration` in `probemodel.go`, which RunE calls; in
`probemodel_test.go` table-test it with `0`, `-1s`, `200ms`, `10m`. Keep `TestProbeModelTimeoutFlagBoundsTheBatteryAttempt` passing.

**Acceptance.** `go test -count=1 -run 'TestProbeModel|BatteryTimeout' ./cmd/apogee/ && rg -q -- '--timeout 0' docs/manual/probe.md`

**Commit:** `fix(probe): apogee probe model --timeout 0 keeps the five-minute default`

## 7. A late reset tick never clears a fresher esc, ctrl+c or flash

**What.** Fixes `apogee-ukw`: `escStopResetMsg`, `ctrlCResetMsg` and `flashClearMsg` are empty
structs and their handlers in `internal/tui/model.go` `Update` clear the state for ANY such message,
so a tick scheduled for arm N and delivered late clears arm N+1 (and its status hint) and the second
press re-arms instead of confirming.
**Goal:** each of the three messages carries the generation of the arm or flash that scheduled it;
a message whose generation is not the current one is a no-op.

**Regression guard.** The flash case is a subtest of `TestModelStopKeys` or carries "Flash" in its
name, so the Acceptance filter runs it. A zero-gen filler message is harmless (it clears state that is
already zero), not "stale" — gen 0 matches while no arm has happened; add no `gen != 0` special case.

**Approach (assumed at the header base):** follow the house `gen` pattern (`spinnerTickMsg{gen}`,
`heartbeatTickMsg{gen}`): add `gen int` to each message and a counter per gesture on the Model
(plain ints — the Model is copied by value, ADR 0011), bumped at each arm in `handleKey`'s `"esc"`
and `"ctrl+c"` cases and at each flash set (the senders in `mouse.go` and `interject.go`); the
handler clears only when `msg.gen` matches. The worker-finish disarm of `lastEsc` is unchanged. The
three settle tests using `ctrlCResetMsg{}` as a neutral filler keep working (a zero-gen message is
harmless: it clears state that is already zero).

**Files:** internal/tui/model.go, internal/tui/mouse.go, internal/tui/interject.go, internal/tui/model_test.go
**Read first:** internal/tui/model.go — Model.handleKey ("esc"/"ctrl+c" cases), Model.Update (ctrlCResetMsg/escStopResetMsg/flashClearMsg arms), Model.statusRight, escStopHintPlain;
internal/tui/mouse.go — flashClearMsg, Model.copyFlash, flashDuration; internal/tui/interject.go — Model.refuseChildMessage;
internal/tui/model_test.go — TestModelStopKeys, step, stepCmd, keyEsc, keyCtrlC, startStubWorker; internal/tui/runview_test.go — clearsTheFlash

**Tests.** New subtests in `TestModelStopKeys` (`model_test.go`, via `step`/`stepCmd`, `keyEsc`,
`keyCtrlC`): arm, lapse the window by rewinding `lastEsc`, re-arm, deliver the FIRST arm's reset
message ⇒ `lastEsc` still set and `statusRight` still shows `escStopHintPlain`; then a second esc
stops the worker. Same shape for ctrl+c ("press ctrl+c again to quit"), and a flash test (a
`TestModelStopKeys` subtest or a top-level test with "Flash" in its name): set flash A, set flash B,
deliver A's clear ⇒ B still shown. Each must fail against the pre-item tree.

**Acceptance.** `go test -count=1 -run 'TestModelStopKeys|Flash' ./internal/tui/`

**Closes:** apogee-ukw

**Commit:** `fix(tui): a late reset tick never clears a fresher esc, ctrl+c or flash`

## 8. Snapshot calls wait for a running undo walk to land

**What.** Fixes `apogee-eykc`, opened by commit `9f684b83` (audit plan item 1): `Journal.Revert` and
`Journal.Redo` walk lock-free after `takeWalk`, and `Close`, `MarkPre`, `Preview` and `RedoPreview`
can take the lock mid-walk — a Close re-captures the previous group's post-image from a half-walked
tree and clears `redo`; a MarkPre opens a group over a torn pre-image. Latent today (`/undo` is
idle-only, ADR 0051 decision 7).
**Goal:** while a walk is in flight, `Close`, `MarkPre`, `Preview` and `RedoPreview` block until it
lands (or their context ends, returning `ctx.Err()`), then act on the post-walk journal; `Record` and
`Generation` still never wait on a walk; a second Revert or Redo also waits.

**Regression guard.** Serialise the test: one parked walk for `MarkPre` (then `Close` sequentially after
release) asserts redo survives; a separate parked walk for `Close` alone asserts only the unchanged post
tree id — never assert redo after a lone `Close` over a prior snapshot group (`closeGroup` re-closes it).
Update every comment on what may run mid-walk (`grep -n -i 'walk\|lock released' internal/undo/*.go`).

**Approach (assumed at the header base):** in `internal/undo/journal.go`, a `walkDone chan struct{}`
field (non-nil while walking) set by `takeWalk` and closed-and-nilled by `landReverted`/`landRedone`
on every exit path; a helper `awaitWalk(ctx)` called with `j.mu` held loops: release, select on the
channel or `ctx.Done()`, re-lock. Previews take no context today — give them `context.Background()`
internally rather than widening their signatures. Channel, not `sync.Cond`: `Close` runs under
`undoCloseTimeout`.

**Files:** internal/undo/journal.go, internal/undo/snapshot.go, internal/undo/redo.go, internal/undo/doc.go, internal/undo/snapshot_test.go
**Read first:** internal/undo/journal.go — takeWalk, takeTop, landReverted, Preview, Journal; internal/undo/redo.go — takeRedoTop, landRedone, RedoPreview;
internal/undo/snapshot.go — MarkPre, Close, closeGroup, dropIfEmpty; internal/undo/snapshot_test.go — gatedSnapshotter, gatedJournal, exchange;
internal/undo/journal_test.go — waitOn, parkableExchange, TestRevert_WhileItWalks_AnswersRecordAndGeneration; internal/agent/agent.go — closeUndoGroup, UndoRevert

**Tests.** In `snapshot_test.go`, with `gatedJournal`/`gatedSnapshotter.arm()`/`unblock()`: (a) park a
Revert mid-walk; start `MarkPre` on a goroutine; assert it has not returned while parked (bounded
wait); `unblock()`; then `Close` sequentially; assert the reverted group lands on redo (not dropped).
(b) Park a separate Revert mid-walk; start `Close` alone on a goroutine; assert it has not returned
while parked; `unblock()`; assert only that the previous group's post tree id is unchanged. A ctx-cancelled `Close` during a parked walk returns
`context.Canceled`. Must fail against the pre-item tree.

**Acceptance.** `go test -count=1 ./internal/undo/`

**Closes:** apogee-eykc

**Commit:** `fix(undo): snapshot calls wait for a running undo walk to land`

## 9. A command-config probe that reaches no repository is never memoised

**What.** Fixes `apogee-se0`: in a root no repository reaches, `configFiles` gets no path from
`git rev-parse --git-path …`, so the memoised `commandConfigProbe` carries no prints and `holds()`
for the process lifetime; after a mid-session `git init` (in the root or an ancestor) plus a
command-valued key, `Capture` still serves the empty answer and git runs the driver unconfined.
**Goal:** a probe whose `rev-parse` printed no path AND exited non-zero is used for the call that
ran it but not stored, and runs no config listing; every other answer memoises as today.

**Regression guard.** Update every comment describing when a probe is memoised or what a no-repository
root yields — the `Capture` doc, `probeCommandConfig`, `repoLocalCommandConfig`, `internal/gitexec/doc.go`
— found with `grep -n -i 'memo\|no repository\|printed nothing\|holds trivially' internal/gitexec/*.go`.

**Approach (assumed at the header base):** in `internal/gitexec/gitexec.go`, `configFiles` also
reports whether a repository was reached (the exit status is discarded today); `probeCommandConfig`
skips `commandConfigProbes.Store` and `repoLocalCommandConfig` skips both `FilterConfigScopes`
listings when it was not. The item-7b rule stands: no `.git` path assumed, no directory printed.
Update the rule's comment beside `configFiles`/`holds`.

**Files:** internal/gitexec/gitexec.go, internal/gitexec/doc.go, internal/gitexec/gitexec_test.go
**Read first:** internal/gitexec/gitexec.go — probeCommandConfig, repoLocalCommandConfig, configFiles, commandConfigProbe.holds, commandConfigProbes, Capture;
internal/gitexec/gitexec_test.go — TestCapture_ServesTheCacheWhileTheConfigHolds, TestCapture_ReprobesWhenTheConfigChanges, captureStatus, realGit;
internal/snapshot/store.go — OpenStore init call (GIT_DIR on a not-yet-created store is an unreached probe); internal/agent/treesnapshot.go — treeSnapshotter.active, git
**Closes:** apogee-se0

**Tests.** Real-git test beside the `TestCapture_Reprobes…` family: Capture in a non-repo temp root;
`git init` it and set `filter.x.clean`; Capture again ⇒ refused with the `CommandConfigRefusal`
text. `TestCapture_ServesTheCacheWhileTheConfigHolds` passes unchanged (its fake exits 0). Must fail
against the pre-item tree.

**Acceptance.** `go test -count=1 ./internal/gitexec/`

**Commit:** `fix(gitexec): a command-config probe that reaches no repository is never memoised`

## 10. An exchange that opened no group closes none

**What.** Fixes `apogee-undo-chat-turn-recloses` (latent since `ac9cbcf6`, not a regression):
`Journal.Close` → `closeGroup` never consults `pending`, so an exchange that made no write-capable
tool call (no `MarkPre`, no `Record`, hence no `openGroup`) re-closes `groups[len-1]` — the PREVIOUS,
already-closed group. It re-captures that group's post-image and re-diffs against its pre: after
`/undo` the diff is non-empty and `redo` is cleared (`/redo` lost); without undo, edits the human made
between exchanges are absorbed into the previous group, so a later `/undo` reverts them too.
Depends on item 8 (same files).
**Goal:** a `Close` while `pending` is true (a `BeginGroup`, a walk, or a load is outstanding and no
group was opened since) leaves every group's images, the `redo` stack and the generation untouched;
a `Close` after `MarkPre` or `Record` opened a group closes that group exactly as today.

**Regression guard.** `Close` runs item 8's `awaitWalk` first, then reads `j.pending` (the post-walk
value) to decide on `closeGroup`, then persists — the wait never short-circuits on `pending` (`takeWalk`
sets it). The comment sweep covers `internal/agent/*.go`, not only `agent.go`: `loop.go`'s `BeginGroup`
comment ("at the Exchange's close when the workspace tree moved at all") gains the pre-image condition.

**Approach (assumed at the header base):** in `internal/undo/snapshot.go` `Journal.Close`, skip
`closeGroup` when `j.pending` is set (still `persist`); `pending` is set by `BeginGroup`, `takeWalk`
and the loader in `persist.go`, and cleared only by `openGroup`. Update `closeGroup`'s and `Close`'s
doc comments and the package doc's close description to state the rule ("an exchange that opened no
group closes none"); every comment describing what `Close` acts on is in scope —
`rg -n 'Close|closeGroup' internal/undo/*.go internal/agent/agent.go`.

**Files:** internal/undo/snapshot.go, internal/undo/snapshot_test.go, internal/undo/doc.go, internal/agent/loop.go
**Read first:** internal/undo/snapshot.go — Close, closeGroup, dropIfEmpty, MarkPre; internal/undo/journal.go — BeginGroup, openGroup, takeWalk;
internal/undo/persist.go — Load (loaded.pending); internal/undo/snapshot_test.go — exchange, TestRedo_MaterialisedGroupClearsTheStack_BareBeginGroupDoesNot;
internal/agent/agent.go — closeUndoGroup; internal/agent/loop.go — BeginGroup call site comment

**Tests.** In `internal/undo/snapshot_test.go`, with real snapshots (the `exchange(t, j, body)`
helper family): (a) two write exchanges, `Revert`, then `BeginGroup` + `Close` with no write ⇒
`RedoPreview` still offers the reverted group and `Generation()` is unchanged; (b) one write exchange
touching file A, then a human edit to tracked file B outside any exchange, then `BeginGroup` +
`Close` with no write ⇒ the group's `touched` set is still `{A}`, and `Revert` restores A and leaves
the human's B edit in place. Both must fail against
the pre-item tree.

**Acceptance.** `go test -count=1 ./internal/undo/`

**Closes:** apogee-undo-chat-turn-recloses

**Commit:** `fix(undo): an exchange that opened no group closes none`
