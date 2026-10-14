# Architecture-review defects — fix what the 2026-10-14 scans found in passing

**Goal:** fix every defect the 2026-10-14 architecture review's scans found in passing and the follow-up verification confirmed, each with a regression test. Regressions first.
**Date:** 2026-10-14
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 313f03e3

**Regression check (2026-10-14, 313f03e3):**
- 1: guard folded
- 3: recast — guard folded (decision); yields to ADR 0096's decision (the /settings Allow rules section adopts or removes adopted, proposed and rejected rules)
- 5: guard folded
- 8: guard folded
- 9: guard folded
- 11: recast — Goal sentence and cancel test replaced (decision)
- 12: guard folded
- 14: guard folded; supersedes the in-code record of the `textarea.Paste` fallback (`internal/tui/clipboard.go`, `internal/tui/doc.go`, `internal/tui/model.go`)
- 16: guard folded
- 17: guard folded
- 18: recast — guard folded (decision)
- 19: guard folded
- 21: guard folded
- (re-check) former 16 (TUI workflow totals fallback): removed (decision) — split out to capcom `workflow-totals-fallback-kinds`; items 17–22 renumbered 16–21
- (re-check) 3: guard folded — the Goal admits proposed or rejected rules; the unresolvable-global-config test asserts the record and expects the settle error
- (re-check) 11: guard folded — the never-started interjection test replaced by a reason-mapping table test; supersedes the whole-Turn "was cancelled" / single-stop readings in `layout.md`, `internal/domain/events.go` and the glossary Interjection note, as ADR 0088 D2 did for delegations
- (re-check) 12: guard folded (decision) — `skipped` excluded from the delegations note's trigger
- (re-check) 18: Read first corrected (decision) — `seatChoiceOffered` lives in `cmd/apogee/wire_settings_test.go`

**Sources:**
- `docs/handoffs/2026-10-14 - 00 - architecture-review-in-passing-defects.md` (claims #1–#14 and the minor list)
- ADR 0089 (workflows), ADR 0095 (MCP OAuth), ADR 0096 (Project config, Adoption), ADR 0088/0086 (delegation cancel/stop)
- `layout.md`, `docs/layout/tool-layout.md`, `docs/manual/headless.md`, `docs/manual/probe.md`

**Ratified design calls** (owner, 2026-10-14):
- **Queued workflow stop (#6):** the stop answer says honestly that the run was queued, never started, and was dropped and marked stopped; no finish note, no events.
- **Stage-error phase (#5):** a workflow whose stage returns an error records phase `failed`.
- **Workflow-item human stop:** undelivered messages read `stopped`, as a delegation's do.
- **Skipped delegation:** `preemptDelegation` books a ledger row with outcome `skipped`.
- **`probe config` (#3):** honours `APOGEE_CONFIG`; the Project root stays the working directory, as `docs/manual/probe.md` documents.
- **"Blocked" (#7):** the re-run predicate is renamed for what it answers (re-runnable work); the tally stays fan-out-only (ADR 0089 2026-09-30 amendment).
- **Skipped row and the note:** a `skipped` row never triggers the delegations note; it keeps numbering contiguous and shows when a note fires for another reason.
- **Totals fallback:** split out to capcom `workflow-totals-fallback-kinds`.

**Standing requirements:**
- skills: coding-standards
- Raspberry Pi 5 box: never `go test` a whole heavy package or a pattern (`./internal/tui/`, `./cmd/apogee/`, `./...`) without `-run`; always `GOMEMLIMIT=2GiB go test -race -count=1 -parallel 1 -run '<regex>' <one package>`; never two test runs at once; full suite only as `APOGEE_TEST_SLOW=1 GOMEMLIMIT=2GiB make check`. Pass this to every sub-agent.
- Bubble Tea v2: `tea.KeyPressMsg` matched on `msg.String()`; paste is `tea.PasteMsg`.
- Each fix gets a `CHANGELOG.md` `[Unreleased]` entry (through the item's sidecar).

**Out of scope:**
- `APOGEE_WORKSPACE` for `probe config` (documented behaviour).
- pty exit-code drift between `internal/tuitest` and `cmd/demorig` (wording only).
- `delegationResult` vs `workflowOutcome` check order (never both flags set).
- The TUI workflow item line's `ItemName` and missing `verdict=` (deliberate / unreachable).
- One shared lock for project-rule add and adopt (a "yes" is keyed to exact text; the re-check in item 3 suffices).
- The TUI workflow totals fallback counting verify/merge items — capcom `workflow-totals-fallback-kinds` (needs stage kinds carried to the TUI).

## 1. Keep `[exit code N]` the last line of a confined terminal failure — ✅ DONE (2026-10-14)

NOTES (2026-10-14): consequential edit — internal/tools/exec_common.go: made necessary by moving the labels above the exit-code line (the confinementDenialLabel / confinementDenialStopLabel docs said "appended", which no longer describes where they sit)
NOTES (2026-10-14): the denial label goes after the shell-hint and fail-fast stopped-command lines and directly above the marker; the Console family (renderConsoleTail) already writes the stop label at the top and is unchanged

**What:**
**Goal:** a terminal result carrying a confinement-denial (or stop) label ends with its `[exit code N…]` line, so the TUI slot reads `error: exit N` (`docs/layout/tool-layout.md`) and the label stays in the body.
**Approach (assumed at the header base):** regression from `6ae21654` (label appended after the marker, which `d59bd1c7`'s end-anchored `exitCodeMarker` in `internal/tui/toolregistry.go` misses; `2da3291e` added the stop label in the same place). In `subprocessToolResult` (`internal/tools/terminal.go`) write the denial / stop label above the `[exit code N]` line, the way the fail-fast "stopped at" line already is. The recipe's unanchored regex (`internal/agent/recipe.go`) keeps working unchanged.
**Regression guard.** Rewrite every comment that places a denial/stop label after the exit-code marker (`grep -rn 'after the exit-code line\|follow on their own line' internal/tools` — the `subprocessToolResult` doc in `terminal.go` included). `python_exec` shares `subprocessToolResult` (`python_exec.go`), so its failures change too; the CHANGELOG entry says so.
**Files:** internal/tools/terminal.go, internal/tools/terminal_test.go, internal/tui/toolpresent_test.go
**Read first:** internal/tools/terminal.go — subprocessToolResult, failFastStoppedLine; internal/tools/exec_common.go — confinementDenialLabel, confinementDenialStopLabel; internal/tools/terminal_test.go — TestSubprocessToolResultFailFastNote; internal/tui/toolregistry.go — exitCodeMarker; internal/tui/toolpresent_test.go — TestPresentToolCallFailedSubprocessNamesItsExitCode; internal/agent/recipe.go — exitCodeMarker
**Tests:** flip the two `terminal_test.go` expectations that pin `"[exit code 1]\n" + confinementDenialStopLabel(box)`; add a tools-side assertion that a labelled result ends with the marker; add a `toolpresent_test.go` table case with a label above `[exit code 1]` → `wantSummary "error: exit 1"`, label kept in the body.
**Acceptance:**
- `go build ./... && go vet ./internal/tools/ ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -parallel 1 -run 'Confinement|ExitCode|Subprocess' ./internal/tools/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -parallel 1 -run 'TestToolPresent|ExitCode' ./internal/tui/`
**Commit:** `fix(tools): keep the exit-code line last under a confinement label`

## 2. Bind the scratch mount before the TUI builds its boot tools — ✅ DONE (2026-10-14)

**What:**
**Goal:** the tools the TUI's startup Agent holds resolve the announced scratch directory from the first Exchange on, before any heartbeat rebind.
**Approach (assumed at the header base):** live defect in the announced-path class: `w.cfg.Tools` is built in `cmd/apogee/wire_live.go` before `w.cfg.ReadMounts.Scratch` is set, and `HostToolsOf` copies `ReadMounts` by value; with a pinned `model:` a send before the first beat runs a whole Exchange without the scratch mount. Set `w.cfg.ReadMounts.Scratch = func() string { return w.engine.ScratchDir() }` before the tool build (a closure: `w.engine` is assigned later), matching the Firing's order in `wire_firing.go`.
**Files:** cmd/apogee/wire_live.go, cmd/apogee/wire_live_test.go
**Read first:** cmd/apogee/wire_live.go — rootWiring.wireSession; cmd/apogee/wire_engine.go — lateEngine.ScratchDir, lateEngine.SetScratchDir; internal/tools/registry.go — HostToolsOf; internal/tools/path_read.go — read scope Scratch lookup; cmd/apogee/readfence_test.go — readFenceWiring, readFenceExecute; cmd/apogee/wire_session.go — SessionScratchDir
**Tests:** a wiring test: after `wireSession`, before any rebind, a `read_file`/`list_dir` call on the exact scratch path the session announces is accepted by the bound Agent's registry.
**Acceptance:**
- `go build ./... && go vet ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -parallel 1 -run 'Scratch' ./cmd/apogee/`
**Commit:** `fix(wire): bind the scratch mount before building the boot tools`

## 3. Adopt only rule text the Project config still holds — ✅ DONE (2026-10-14)

NOTES (2026-10-14): the held texts are read with `config.LoadLayeredConfig("", projectRoot, …)` (project layer only, per the Regression guard) and unioned over Rules ∪ Proposed ∪ Rejected filtered to the project layer; a Project config that is the global file itself (os.SameFile against the global path) holds no project rule, as the layered load treats it.
NOTES (2026-10-14): a partial batch through `AdoptRules` adopts the held rules and returns one joined refusal per gone rule (plus the settling's error, if any); the CLI prints a per-rule refusal line, continues with the next question, exits 0, and its closing line gains ", N no longer in the file" only when N > 0 (`reportProjectAnswers` became `projectAnswerTally.report`).
NOTES (2026-10-14): `answerProjectRules`' entry loop was extracted into `projectRuleEntries` so the new `adoptProjectRules` shares it; RejectRules is unchanged (the Goal covers adoption only).
NOTES (2026-10-14): consequential edit — docs/manual/configuration.md: made necessary by the adoption re-check (user-facing refusal of a rule the file no longer holds); one sentence in the `apogee project adopt` paragraph.

**What:**
Recast at the regression check (2026-10-14).
**Goal:** adopting project rules — from the adoption pane (`answerProjectRules`) or `apogee project adopt` (`askProjectRules`) — records a "yes" only for project rules (proposed or rejected) whose exact text the Project config holds at the moment of adoption; an entry the file no longer holds is refused with a message naming it, and nothing is recorded for it.
**Approach (assumed at the header base):** before `Adopt`, re-classify from the file (every rule text the Project config holds — see the Regression guard; fingerprints from `adoption.Fingerprint`) and drop entries absent from it, in both `cmd/apogee/projectrules.go` and `cmd/apogee/project_cmd.go`. Rewrite the comment in `projectrules.go` that claims the two locks guarantee "never a recorded yes to text no file holds" to state what does (exact-text fingerprints plus this re-check). No lock change.
**Regression guard.** Fold every G of regression-2's item-3 REGRESSION as binding — the re-check reads every rule text the Project config holds (project-layer Rules ∪ Proposed ∪ Rejected), never Proposed alone, so adopting a rejected rule from /settings (ADR 0096 decision: the /settings Allow rules section adopts or removes adopted, proposed and rejected rules) and RejectRules of an in-force rule keep working and TestProjectRuleRejectAndAdoptMoveOnlyTheRecord stays green; read the held project texts without failing on a global config that does not resolve (e.g. LoadLayeredConfig with no global path against roots.project); projectAdoptTarget gains what askProjectRules needs to re-read (Project root and global config path, or a re-read closure) filled in resolveProjectAdoption; the re-check reads the same boot-fixed roots.project item 5 pins; comment guard is a rule — every comment claiming the write order prevents a yes to text no file holds, or that AdoptRules cannot be refused (`grep -rn 'no file holds\|already holds them' cmd/apogee internal/tui`, incl. internal/tui/tui.go); Acceptance -run becomes 'ProjectRule|ProjectCmdAdopt' plus the new tests' names; add internal/tui/tui.go to Files. The item yields to ADR 0096's decision (`.capcom/knowledge/decisions/adr-0096/adr-0096-decision.md`).
Re-check: the Goal admits rejected rules (`settingsAdoptRule`: "proposed, or rejected earlier"). Through `AdoptRules` the record lands before `settleAllowRules` fails on a global config that does not resolve ("the answer is saved, but…"), so that test asserts the record (`assertRuleRecord` … `adoption.Adopted`) and expects the settle error, never a nil return.
**Files:** cmd/apogee/projectrules.go, cmd/apogee/project_cmd.go, cmd/apogee/projectrules_test.go, cmd/apogee/project_cmd_test.go, internal/tui/tui.go
**Read first:** cmd/apogee/projectrules.go — answerProjectRules, recordAnswer, layeredOptions, settleAllowRules; cmd/apogee/project_cmd.go — resolveProjectAdoption, projectAdoptTarget, askProjectRules; internal/config/layer.go — LoadLayeredConfig, parseLayeredConfig, readProjectLayer; internal/config/allow.go — classifyProjectRules, workspacesDirOf; internal/tui/settingsapply.go — settingsAdoptRule; internal/tui/tui.go — ConfigHost.AdoptRules, ConfigHost.AddProjectRule;
cmd/apogee/projectrules_test.go — TestProjectRuleRejectAndAdoptMoveOnlyTheRecord, projectRuleWiring, assertRuleRecord; cmd/apogee/project_cmd_test.go — runProjectAdopt, writeProjectAllow, recordedAnswers
**Tests:** offer a proposed rule, remove it from the file, then `AdoptRules` → refused and `Classify` does not show it adopted; the same through `askProjectRules` with a `y` reply after the file changed; a still-present rule adopts as before; adopting a rejected rule still adopts and `TestProjectRuleRejectAndAdoptMoveOnlyTheRecord` stays green; with a global config that does not resolve, `AdoptRules` of a held proposed rule records it (`assertRuleRecord` … `adoption.Adopted`) and returns the settle error.
**Acceptance:**
- `go build ./... && go vet ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -parallel 1 -run 'ProjectRule|ProjectCmdAdopt|<new test names>' ./cmd/apogee/`
**Commit:** `fix(project): adopt only rule text the Project config still holds`

## 4. Never migrate the config file on a live re-read — ✅ DONE (2026-10-14)

NOTES (2026-10-14): re-derived from `Options` being declared in internal/config/config.go — the struct and `GlobalConfigOnly` live in internal/config/options.go, where the new `LiveReread` switch sits beside it.
NOTES (2026-10-14): the config.go comments also named a non-existent `parseConfigFile(…, true)` call as the migrating read (ResolveOptions calls `parseLayeredConfig`); corrected while rewording them.

**What:**
**Goal:** the `/settings` and file-watcher re-read never rewrites the config file or writes a `config.yaml.bak-*`; a retired shape (`hooks:`, top-level `endpoint:`) saved mid-session is refused with `config.ErrRetiredShape`, as `LoadFileConfig`'s doc states.
**Approach (assumed at the header base):** `externalEdit.projection` (`cmd/apogee/settingsedit.go`) resolves through `config.ApplyConfig` → `ResolveOptions` → `parseLayeredConfig(…, mayMigrate=true)`. Add an `Options` switch beside `GlobalConfigOnly` (e.g. `LiveReread`) that makes `ResolveOptions` pass `mayMigrate=false`; `projection` sets it. Correct the comments in `internal/config/config.go` that say the live re-read does not migrate (rule: every comment describing the re-read's migration behaviour; `grep -n 'migrat' internal/config/config.go cmd/apogee/settingsedit.go`).
**Files:** internal/config/config.go, cmd/apogee/settingsedit.go, cmd/apogee/settingsedit_test.go
**Read first:** cmd/apogee/settingsedit.go — externalEdit.projection, changed; internal/config/config.go — ResolveOptions, ApplyConfig, LoadFileConfig; internal/config/layer.go — parseLayeredConfig; internal/config/configmigrate.go — migrateLegacyConfig, ErrRetiredShape; cmd/apogee/settingsedit_test.go — TestExternalEditReloadRefusesAConfigItCannotResolve
**Tests:** in `settingsedit_test.go`, after `newExternalEdit`, write a config carrying `hooks:` (and a case with top-level `endpoint:`), call `changed()`; assert `errors.Is(err, config.ErrRetiredShape)`, file bytes unchanged, no `config.yaml.bak-*`.
**Acceptance:**
- `go build ./... && go vet ./internal/config/ ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -parallel 1 -run 'Migrat|Retired|ResolveOptions' ./internal/config/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -parallel 1 -run 'ExternalEdit|SettingsEdit' ./cmd/apogee/`
**Commit:** `fix(config): never migrate the config file on a live re-read`

## 5. Pin the live re-read to the session's Project root — ✅ DONE (2026-10-14)

NOTES (2026-10-14): re-derived from `Options` being declared in internal/config/config.go — the struct lives in internal/config/options.go, where the `ProjectRoot` / `ProjectRootPinned` pair sits beside `GlobalConfigOnly` and `LiveReread`; config.go's ResolveOptions only switches its call to layer.go's new `layerProjectRoot`.
NOTES (2026-10-14): the config-package test (`TestResolveOptionsReadsThePinnedProjectRoot`, pinned boot root and pinned empty root) went into internal/config/layer_test.go, beside the other ResolveOptions layer tests, which the plan's Files list did not name; cmd/apogee also gained `TestRunRootPinsTheReReadToTheBootProjectRoot`, which drives runRoot so wire_live.go's pin itself is covered. Both cmd/apogee tests fail with the pin dropped from `projection` (mutation-checked).

**What:**
**Goal:** the live re-read resolves the project layer against the Project root fixed at boot (`roots.project`), so a `.apogee/` or `git init` appearing in a subfolder workspace mid-session never moves, drops or adds the project layer the re-read applies.
**Approach (assumed at the header base):** depends on item 4. Add a pinned-root field to `config.Options`, distinct from the empty value meaning "no layer"; `ResolveOptions` (via `internal/config/layer.go`'s root walk) uses it when set; `newExternalEdit` passes `roots.project`. Every other reader already uses `roots.project`.
**Regression guard.** Carry the pin on the `opts` the caller passes — `wire_live.go`'s `newExternalEdit(w.opts, …)` call hands a copy of `w.opts` carrying the pinned root — so `newExternalEdit`'s signature and its 20 test call sites stay unchanged and an unset pin keeps today's walk; `projection()` copies the pin into `next`. Test fixture: `<root>/.git` (an empty dir suffices) and `<root>/.apogee/config.yaml`, workspace `<root>/sub` — `walkToProjectRoot` counts an `.apogee/` above the workspace only below a `.git`.
**Files:** internal/config/config.go, internal/config/layer.go, cmd/apogee/settingsedit.go, cmd/apogee/settingsedit_test.go, cmd/apogee/wire_live.go
**Read first:** cmd/apogee/settingsedit.go — newExternalEdit, externalEdit.projection; cmd/apogee/wire_live.go — newExternalEdit call; internal/config/config.go — ResolveOptions; internal/config/layer.go — resolveProjectRoot; cmd/apogee/wire.go — resolveRoots; internal/projectroot/projectroot.go — walkToProjectRoot; internal/config/options.go — Options.GlobalConfigOnly
**Tests:** repo root (`<root>/.git`, an empty dir) with `.apogee/config.yaml` setting a project-param key, workspace `<root>/sub`; boot, `mkdir <ws>/.apogee`, call `changed()`; assert no key change is applied and the row's source is still project.
**Acceptance:**
- `go build ./... && go vet ./internal/config/ ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -parallel 1 -run 'ProjectRoot|Layer' ./internal/config/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -parallel 1 -run 'ExternalEdit|SettingsEdit' ./cmd/apogee/`
**Commit:** `fix(config): pin the live re-read to the session's Project root`

## 6. Treat an MCP client-id change as a missing login — ✅ DONE (2026-10-14)

NOTES (2026-10-14): the same client check also guards the handler's refresh path (`refreshLocked` re-reads the record from disk under its lock), so a record another apogee saved for a different client is never adopted or refreshed; one shared `clientMatches` rule serves both, covered by `TestHandlerRefreshRefusesARecordOfAnotherClient`.
NOTES (2026-10-14): "the store's doc comment" placed on `HandlerConfig.Store` (and the new `ClientID` field and `NewHandler`'s doc) in `internal/mcpauth/handler.go`, the listed file — the rule lives in the handler, not in `Store.Load`.
NOTES (2026-10-14): consequential edit — docs/manual/configuration.md: made necessary by the handler now refusing a token issued to another client; the startup-offer paragraph names that case beside the unrefreshable token.

**What:**
**Goal:** when a server's configured `client-id:` differs from the stored token record's client id, the OAuth handler returns `ErrLoginRequired` and sends no request; a matching id, or no configured id (dynamic registration), loads the record as before.
**Approach (assumed at the header base):** add `ClientID` to `mcpauth.HandlerConfig` (`internal/mcpauth/handler.go`), pass `cfg.ClientID` from `newOAuthHandler` (`internal/mcp/transport.go`), and in `NewHandler` treat `cfg.ClientID != "" && record.Client.ID != cfg.ClientID` as a store miss. Within ADR 0095 D1's purpose ("never sends an old token to a new place"); no amendment. Note the rule in the store's doc comment.
**Files:** internal/mcpauth/handler.go, internal/mcpauth/handler_test.go, internal/mcp/transport.go
**Read first:** internal/mcpauth/handler.go — HandlerConfig, NewHandler; internal/mcp/transport.go — newOAuthHandler, ServerConfig.ClientID; internal/mcpauth/login.go — loginFlow.resolveClient; internal/mcpauth/store.go — Store.Load; internal/mcpauth/handler_test.go — TestNewHandlerWithoutARecordNeedsALogin, fakeTokenServer.storedRecord
**Tests:** beside `TestNewHandlerWithoutARecordNeedsALogin`: record with `Client.ID "a"`, `NewHandler` with `ClientID "b"` → `ErrLoginRequired`, token endpoint never hit; same id loads; empty configured id loads.
**Acceptance:**
- `go build ./... && go vet ./internal/mcpauth/ ./internal/mcp/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -parallel 1 ./internal/mcpauth/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -parallel 1 -run 'OAuth' ./internal/mcp/`
**Commit:** `fix(mcpauth): treat a client-id change as a missing login`

## 7. Keep a received finish reason and usage when an OpenAI stream ends without `[DONE]` — ✅ DONE (2026-10-14)

**What:**
**Goal:** an OpenAI-wire stream that ends at EOF without `[DONE]` finishes with the `finish_reason` and usage it already received (so `length` reaches the loop's FinishLength paths); a bare EOF with no finish reason still reads as `stop`.
**Approach (assumed at the header base):** the EOF path in `internal/provider/wire_openai.go` calls `finish("stop", nil)`; make it do what the `[DONE]` path does with `pendingFinish` / `pendingUsage` (defaulting the reason to `stop`), and fix the comment above it.
**Files:** internal/provider/wire_openai.go, internal/provider/stream_test.go
**Read first:** internal/provider/wire_openai.go — openaiCodec.parseSSE, finish, pendingFinish, pendingUsage; internal/provider/stream_test.go — TestStream_TerminatesWithoutDone, TestStream_FlushesOpenCallsWithoutDone, sseServer; internal/agent/loop.go — FinishLength paths
**Tests:** a body with a `finish_reason:"length"` chunk and a usage chunk, no `[DONE]` → `Done{FinishReason:"length", Usage: …}`; `TestStream_TerminatesWithoutDone` and `TestStream_FlushesOpenCallsWithoutDone` stay green.
**Acceptance:**
- `go build ./... && go vet ./internal/provider/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -parallel 1 -run 'TestStream' ./internal/provider/`
**Commit:** `fix(provider): keep the finish reason and usage when a stream ends without [DONE]`

## 8. A workflow whose stage errors records phase `failed` — ✅ DONE (2026-10-14)

NOTES (2026-10-14): consequential edit — internal/agent/doc.go: made necessary by RerunFailed now accepting a failed workflow (the package map named only a finished one's re-run).
NOTES (2026-10-14): the stage whose error ends the run keeps its own status.json phase `running` (only the workflow phase becomes `failed`), as before this item — pre-existing; the Goal names the workflow's phase only.

**What:**
**Goal:** when `workflow.Runner.Run` returns a stage error, the workflow's recorded phase is `failed` (`workflow.PhaseFailed`) and its items are written, so listings never show it `running`; `RerunFailed` accepts a `failed` workflow and re-runs it from its folder.
**Approach (assumed at the header base):** on the stage-error exit in `Runner.Run` (`internal/workflow/runner.go`), best-effort `setWorkflowPhase(PhaseFailed)` plus `WriteItems`, returning the original error joined with any write error. In `RerunFailed` (`internal/agent/background.go`) accept `PhaseFailed` as well as `PhaseDone`; for `failed`, skip the nothing-blocked refusal (the run never finished; `resetStatus` replays it). Update `rerunUnfinishedFormat`'s wording if it names only done.
**Regression guard.** The Runner never touches a plain fan-out item's output, so the test's trigger is: `Runner.Open` the folder, make `<folder>/report.md` a non-empty directory under a fan-out+merge plan (or a sub_agent-path stage's `items/<key>/output.md` a directory), then `Run`. `markStopped` leaves `PhaseFailed` as it leaves `PhaseDone` (its doc comment says so), so a stopped queued re-run of a failed folder stays re-runnable. Every prose naming what `RerunFailed` / `^r` accepts follows (`grep -rn 'finished workflow' internal/ cmd/ docs/manual/`).
**Files:** internal/workflow/runner.go, internal/workflow/runner_test.go, internal/agent/background.go, internal/agent/background_test.go, internal/tui/tui.go, cmd/apogee/wire_engine.go, docs/manual/workflows.md, docs/manual/commands.md
**Read first:** internal/workflow/runner.go — Runner.Run, runState.setWorkflowPhase, Runner.resetStatus, finishSubAgentItem; internal/workflow/stages.go — runMerge, removeReport; internal/agent/background.go — RerunFailed, rerunUnfinishedFormat, markStopped; internal/agent/background_test.go — TestBackground_StoppingAQueuedRerunLeavesTheFolderDone
**Tests:** `runner_test.go`: after `Runner.Open`, make the merge's `<folder>/report.md` a non-empty directory (or a sub_agent-path item's output a directory) → `Run` errors and `Store.ReadStatus(id).Phase == PhaseFailed`; `background_test.go`: `RerunFailed` on a `failed` workflow launches; the failed twin of `TestBackground_StoppingAQueuedRerunLeavesTheFolderDone` — a stopped queued re-run of a `failed` folder leaves it `failed`.
**Acceptance:**
- `go build ./... && go vet ./internal/workflow/ ./internal/agent/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -parallel 1 -run 'TestRunner' ./internal/workflow/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -parallel 1 -run 'TestBackground' ./internal/agent/`
**Commit:** `fix(workflow): record a failed phase when a stage errors`

## 9. Stopping a queued workflow says it never started — ✅ DONE (2026-10-14)

**What:**
**Goal:** `StopWorkflow` on a queued (never-started) workflow answers the model that the run was queued, never started, and was dropped and marked stopped — never "its result reaches you when it ends"; a running workflow's stop answer is unchanged.
**Approach (assumed at the header base):** depends on item 8 (same file). The queued branch in `StopWorkflow` (`internal/agent/background.go`) is `dropLocked` + `markStopped`; have it report that it was queued, and have the `workflow` tool's stop answer (`internal/agent/workflowcall.go`) pick the honest text. No finish note, no events (ratified).
**Regression guard.** Keep `StopWorkflow(id string) error` exported as is and add an unexported `stopWorkflow(id) (queued bool, err error)` it wraps, which `workflowStopResult` calls — so `tui.Engine`, `lateEngine`, `fakeEngine`, the `^x` caller and `background_test.go`'s callers stay untouched. The test asserts no held note names the queued id (or takes the notes before releasing the running workflow), never that the notes are empty: the running workflow's finish note is held by then.
**Files:** internal/agent/background.go, internal/agent/workflowcall.go, internal/agent/background_test.go
**Read first:** internal/agent/background.go — Agent.StopWorkflow, markStopped, dropLocked, driveBackground; internal/agent/workflowcall.go — workflowStopResult; internal/agent/background_test.go — TestBackground_FolderStampsFollowTheAgentClock, launchBackground; internal/tui/tui.go — Engine.StopWorkflow
**Tests:** next to `TestBackground_FolderStampsFollowTheAgentClock`: stop a queued workflow → answer carries the queued text, no held note names the queued id; a running workflow's stop answer still promises the result.
**Acceptance:**
- `go build ./... && go vet ./internal/agent/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -parallel 1 -run 'TestBackground|StopWorkflow' ./internal/agent/`
**Commit:** `fix(agent): say a stopped queued workflow never started`

## 10. Name the re-run predicate for what it answers — ✅ DONE (2026-10-14)

NOTES (2026-10-14): the new test runs the script stage in auto mode with a fake FSWrite confiner, since a background workflow's ask-before approval would queue for a Driver the test does not have; the RerunFailed doc comment's refusal list now reads "nothing re-runnable (hasRerunnableItem)" in place of "nothing blocked".

**What:**
**Goal:** the predicate `RerunFailed` uses is named and documented as "has re-runnable work" (any stage kind's blocked or faulted receipt), distinct from the fan-out-only Blocked tally; no behaviour change.
**Approach (assumed at the header base):** depends on item 9 (same file). Rename `hasBlockedItem` in `internal/agent/background.go` (e.g. `hasRerunnableItem`) and write its doc comment contrasting it with `workflow.TallyOf`.
**Files:** internal/agent/background.go, internal/agent/background_test.go
**Read first:** internal/agent/background.go — hasBlockedItem, RerunFailed, rerunNothingFormat; internal/workflow/tally.go — TallyOf, TallyOfStatus; internal/workflow/stages.go — runScript; internal/agent/recipe_test.go — scriptRecipe; internal/agent/background_test.go — TestBackground_RerunFailedRunsOnlyTheBlockedAndFaultedItems
**Tests:** a done workflow whose only blocked receipt is a script stage: `TallyOfStatus(...).Blocked == 0` and `RerunFailed` still launches.
**Acceptance:**
- `go build ./... && go vet ./internal/agent/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -parallel 1 -run 'TestBackground|Rerun' ./internal/agent/`
**Commit:** `refactor(agent): name the re-run predicate for re-runnable work`

## 11. A human stop of a workflow item reports undelivered messages as stopped

**What:**
Recast at the regression check (2026-10-14).
**Goal:** when a human `StopChild`s a workflow item's child, its undelivered messages carry the `stopped` reason (the TUI reads "was stopped by you…"), as a delegation's do; the undelivered reason for a workflow item follows the delegation rule exactly (`undeliveredReason` in `internal/agent/children.go`): a child that was running when stopped — singly by the human or by the cancel of the whole Turn — reads `stopped`; a child that never started reads `cancelled`.
**Approach (assumed at the header base):** in `internal/agent/workflowspawn.go` the `stopped` flag from `runChild` is collapsed into `EndStopped` before `undeliveredWorkflowReason` reads it; carry the reason from `stopped`, matching `undeliveredReason` in `children.go`.
**Regression guard.** A never-started item ends `EndStopped` before any child or reporting defer exists and is registered only inside `runChild`, so nothing is undelivered there: test the reason mapping with a table test instead (stopped→`UndeliveredStopped`; `EndStopped` without stopped, `workflowOutcome`'s `StatusCancelled` case→`UndeliveredCancelled`; other endings unchanged), modelled on `stop_test.go`'s `TestUndeliveredReason_Stopped`. Supersedes every doc mapping `cancelled` to the whole Turn or `stopped` to a single human stop (`grep -rn 'cancelled before your message landed\|stopped singly'`): `layout.md`'s undelivered-notes list, `UndeliveredStopped`/`UndeliveredCancelled`'s docs in `internal/domain/events.go`, the glossary Interjection note — delegations already read this way (ADR 0088 D2, revising ADR 0086's "cancel = whole Turn").
**Files:** internal/agent/workflowspawn.go, internal/agent/workflowspawn_test.go, layout.md, internal/domain/events.go, .capcom/knowledge/glossary/glossary-turns/interjection.md
**Read first:** internal/agent/workflowspawn.go — workflowSpawner.Spawn, undeliveredWorkflowReason, Agent.workflowOutcome; internal/agent/childrun.go — runChild, childRunStopped; internal/agent/children.go — undeliveredReason, interjectChild; internal/agent/subagent.go — classifyDelegation;
internal/agent/workflowspawn_test.go — TestWorkflowSpawn_AHumanStopOfAnItemChildEndsItStopped, spawnItem; internal/agent/stop_test.go — stopResponder, runningChildRunID; layout.md — undelivered interjection notes
**Tests:** flip `TestWorkflowSpawn_AHumanStopOfAnItemChildEndsItStopped`'s assertion to `UndeliveredStopped`; a table test `TestUndeliveredWorkflowReason` pins the mapping: stopped→`stopped`, `EndStopped` not stopped→`cancelled`, completed/capped/faulted unchanged.
**Acceptance:**
- `go build ./... && go vet ./internal/agent/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -parallel 1 -run 'TestWorkflowSpawn|TestUndeliveredWorkflowReason' ./internal/agent/`
**Commit:** `fix(agent): report a human stop of a workflow item as stopped`

## 12. A skipped delegation books a `skipped` ledger row

**What:**
**Goal:** a delegation `preemptDelegation` skips books a ledger row with outcome `skipped`, so the delegations note's indices stay contiguous and every sub_agent call has a row.
**Approach (assumed at the header base):** add `delegationSkipped delegationOutcome = "skipped"` beside the others in `internal/agent/children.go` (doc comment in their style), and book the row in `preemptDelegation` (`internal/agent/dispatch.go`) the way `settleCancelledDelegation` does. Check every switch over `delegationOutcome` (`grep -n 'delegationOutcome\|delegationCancelled' internal/agent/*.go`) handles the new word.
**Regression guard.** Book `outcome: delegationSkipped` with no cause directly in `preemptDelegation`, or give `classifyDelegation` an explicit skip case ahead of its `IsError` rows — copying `settleCancelledDelegation` books `refused`. Every comment or doc that enumerates ledger outcomes or ledger write sites follows (the "six words" comment, `delegationLedger.open`'s doc, `recordCeilingRefusal`'s "second site", `docs/manual/configuration.md`'s outcome list, the glossary's `sub-agent.md`). Ratified by the owner 2026-10-14: exclude `skipped` from `delegationLedger.note()`'s "did not complete" trigger, so a ctrl+g pre-emption adds no new model-facing note; a `skipped` row keeps numbering contiguous and is listed when a note fires for another outcome.
**Files:** internal/agent/children.go, internal/agent/dispatch.go, internal/agent/children_test.go, internal/agent/fanout_test.go, docs/manual/configuration.md, .capcom/knowledge/glossary/glossary-identity/sub-agent.md
**Read first:** internal/agent/dispatch.go — preemptDelegation, settleCancelledDelegation, recordCeilingRefusal, prepareCall; internal/agent/children.go — delegationOutcome, delegationLedger.note; internal/agent/subagent.go — classifyDelegation; internal/agent/fanout_test.go — threeWayFanOutParentSeamed
**Tests:** a pooled group with a message queued mid-dispatch: the skipped call has a `skipped` row (spawnIndex 3) and the row count is right; for contiguity, a delegation after the skip in the same Exchange numbers on without a gap; the width-1 path (`prepareCall` with preempt=true, a fresh `open()`) books its row too; a ledger holding only ok rows plus a `skipped` row produces no note; a `skipped` row is listed when a note fires for another outcome (name the note tests so `Ledger` matches them).
**Acceptance:**
- `go build ./... && go vet ./internal/agent/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -parallel 1 -run 'Ledger|Delegation|Preempt' ./internal/agent/`
**Commit:** `fix(agent): book a ledger row for a skipped delegation`

## 13. `/workflows` claims a paste

**What:**
**Goal:** a paste while `/workflows` is open never reaches the prompt draft: while the recipe-name field is open it lands there folded to one line; otherwise it is claimed and dropped (`layout.md`: nothing is typed or pasted into the prompt box while a pop-up is up).
**Approach (assumed at the header base):** add a `paneSpecs[paneWorkflows].keyOpen(m)` case to `listPaste` (`internal/tui/listsurface.go`): while `naming`, `nameBuf.edit(paste)` folded the way `sessionBrowserPaste` folds a rename; else claimed, no effect.
**Files:** internal/tui/listsurface.go, internal/tui/workflows.go, internal/tui/workflows_test.go
**Read first:** internal/tui/listsurface.go — listPaste; internal/tui/sessions.go — sessionBrowserPaste; internal/tui/workflows.go — workflowsPane, openWorkflowSave; internal/tui/panes.go — paneSpecs[paneWorkflows].keyOpen; internal/tui/lineeditor.go — copyOnWriteField.edit; internal/tui/workflows_test.go — savePaneModel; internal/tui/sessions_test.go — TestSessionBrowserPasteLandsInTheRename
**Tests:** after `TestSessionBrowserPasteLandsInTheRename`'s shape: `m.input.SetValue("keep")`, paste at the list → draft still "keep"; after `keyCtrlS()` (via `savePaneModel`) paste → name buffer holds the folded text, draft "keep".
**Acceptance:**
- `go build ./... && go vet ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -parallel 1 -run 'TestWorkflows|Paste' ./internal/tui/`
**Commit:** `fix(tui): keep a paste over /workflows out of the prompt draft`

## 14. A ctrl+v text paste recomputes autocomplete

**What:**
**Goal:** a ctrl+v paste of clipboard text runs the same edit path as a bracketed paste: autocomplete recomputes (a pasted `/comp` opens the command menu) and the skill-hint band is rescheduled.
**Approach (assumed at the header base):** `foldClipboardImage` (`internal/tui/clipboard.go`) falls back to `textarea.Paste`, whose reply reaches `foldWidgetMsg`, which never recomputes. Read the text through a typed message of our own (e.g. `clipboardTextMsg`) and fold it through `foldPaste`'s edit path in `internal/tui/prompteditor.go`.
**Regression guard.** Fold the text through `foldPaste`'s editable tail only (inputEditable re-check, sel reset, dropRecall, fitWrapMemo, input.Update, recomputeAutocomplete), extracted as one helper `foldPaste` also calls — never `attachPastedImage`, so ctrl+v of text naming an image path stays typed text. Read the text through a `readClipboardText` package var in `clipboard.go` whose production value is atotto `clipboard.ReadAll`, read at Cmd-build time like `readClipboardImage`; on its error paste nothing and flash nothing. Supersedes the in-code record of the `textarea.Paste` fallback: rewrite every comment `grep -n 'textarea.Paste\|textarea.s.*paste' internal/tui/*.go` finds.
**Files:** internal/tui/clipboard.go, internal/tui/prompteditor.go, internal/tui/prompteditor_test.go, internal/tui/doc.go, internal/tui/model.go
**Read first:** internal/tui/clipboard.go — foldClipboardImage, clipboardImageCmd, readClipboardImage; internal/tui/prompteditor.go — foldPaste, attachPastedImage; internal/tui/model.go — handleKey ctrl+v branch; internal/tui/prompteditor_test.go — TestPasteCtrlVFallsBackToText, stubClipboardImage
**Tests:** rewrite `TestPasteCtrlVFallsBackToText` (it asserts the fallback is `textarea.Paste` by pointer); stub `readClipboardImage` and `readClipboardText`, step ctrl+v, run the reply through `stepCmd`, assert `m.autocomplete.active` with `kind == acCommand` for "/comp"; clipboard text that is an image path lands as text on a vision model; a `readClipboardText` error pastes nothing.
**Acceptance:**
- `go build ./... && go vet ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -parallel 1 -run 'Paste|Autocomplete' ./internal/tui/`
**Commit:** `fix(tui): recompute autocomplete after a ctrl+v text paste`

## 15. A click on `/workflows` never cancels the recipe-name field

**What:**
**Goal:** while the `/workflows` recipe-name field is open, every click is claimed and ignored (pane stays open, name kept); `layout.md`'s outside-click table gains its `/workflows` row.
**Approach (assumed at the header base):** depends on item 13 (same test file). Add `if m.workflowsPane.naming { return m, nil, true }` before the hit test in `handleWorkflowsClick` (`internal/tui/workflows.go`), as `handleBrowserClick` guards rename/confirm. Add the row to the outside-click table in `layout.md`.
**Files:** internal/tui/workflows.go, internal/tui/workflows_test.go, layout.md
**Read first:** internal/tui/workflows.go — handleWorkflowsClick, workflowsPane.naming; internal/tui/mouse.go — handleBrowserClick; internal/tui/mouse_test.go — TestBrowserClickIsSwallowedByARenameOrAConfirm, leftClick; internal/tui/workflows_test.go — savePaneModel; layout.md — outside-click table
**Tests:** after `TestBrowserClickIsSwallowedByARenameOrAConfirm`: `savePaneModel`, `keyCtrlS()`, type part of a name, `leftClick(0, 0)` → pane open, `naming` true, buffer unchanged.
**Acceptance:**
- `go build ./... && go vet ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -parallel 1 -run 'TestWorkflows|Click' ./internal/tui/`
**Commit:** `fix(tui): ignore clicks on /workflows while a recipe is being named`

## 16. `apogee probe config` honours `APOGEE_CONFIG`

**What:**
**Goal:** with no `--config`, `apogee probe config` reads and reports the config home named by `APOGEE_CONFIG`, as every other verb does; help text and `docs/manual/probe.md` say so.
**Approach (assumed at the header base):** in `cmd/apogee/probeconfig.go`, `if configDir == "" { configDir = os.Getenv(config.EnvConfig) }` before `config.FilePath`, as `undo.go` does.
**Regression guard.** Amend every sentence saying probe config ignores `APOGEE_*` or takes `--config` only (`grep -n 'APOGEE_\*\|--config. only\|not layered in' cmd/apogee/probeconfig*.go docs/manual/probe.md` — the `probeConfigCommand` doc, its Long help and both `probe.md` sentences), naming `APOGEE_CONFIG` as the one exception. The new test uses `t.Setenv`, so it is not `t.Parallel` and drives `newProbeCommand` directly, bypassing `runProbeConfig` (which always passes `--config`).
**Files:** cmd/apogee/probeconfig.go, cmd/apogee/probeconfig_test.go, docs/manual/probe.md
**Read first:** cmd/apogee/probeconfig.go — probeConfigCommand, configReport, probeProjectRoot; cmd/apogee/undo.go — the configDir/EnvConfig fallback; cmd/apogee/probeconfig_test.go — runProbeConfig, seedConfigHome; cmd/apogee/main_test.go — ambientApogeeEnv; docs/manual/probe.md — probe config paragraph
**Tests:** `APOGEE_CONFIG` (via `t.Setenv`, no `t.Parallel`) → a temp home whose config has an unknown key; run `newProbeCommand` with `config` and no `--config`; the report names that key and that path.
**Acceptance:**
- `go build ./... && go vet ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -parallel 1 -run 'ProbeConfig' ./cmd/apogee/`
**Commit:** `fix(probe): read the config home APOGEE_CONFIG names`

## 17. Headless narrates `RefClippedEvent`

**What:**
**Goal:** `apogee headless` prints a `RefClippedEvent`'s `Notice()` to stderr like the prune and malformed-chunks notices, silenced under `--format json`; `docs/manual/headless.md` lists it with them.
**Approach (assumed at the header base):** add a `domain.RefClippedEvent` case to `narrate` (`cmd/apogee/headless.go`): `fmt.Fprintln(s.out, sanitize.StripEscapesToLine(ev.Notice()))`.
**Regression guard.** No `sanitize` function exists — `sanitize` is the package; the call is `sanitize.StripEscapesToLine(ev.Notice())`, as `narrationSink.remember` uses it. Update every place that lists what the narration sink prints (`grep -n 'MalformedChunks\|malformed stream chunks\|prune notice' cmd/apogee/headless.go docs/manual/headless.md` — the `narrationSink` doc and both `headless.md` paragraphs).
**Files:** cmd/apogee/headless.go, cmd/apogee/headless_test.go, docs/manual/headless.md
**Read first:** cmd/apogee/headless.go — narrationSink, narrationSink.narrate, narrationSink.Emit; internal/domain/events.go — RefClippedEvent.Notice; cmd/apogee/headless_test.go — TestHeadlessPrintsTheMalformedChunksNotice, TestHeadlessFormatJSONSuppressesTheNarration, TestNarrationSinkForwardsEveryEvent, headlessRun
**Tests:** sibling of `TestHeadlessPrintsTheMalformedChunksNotice` asserting stderr carries the notice; extend `TestHeadlessFormatJSONSuppressesTheNarration`.
**Acceptance:**
- `go build ./... && go vet ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -parallel 1 -run 'TestHeadless.*(Notice|Narrat)' ./cmd/apogee/`
**Commit:** `fix(headless): narrate a clipped reference`

## 18. `apogee probe context` builds the tool menu a Firing builds

**What:**
Recast at the regression check (2026-10-14).
**Goal:** under `sub-agents-choice: model`, the probe's context estimate counts the same tool schemas (`sub_agent` with `run_on`) a headless Firing sends, as `docs/manual/probe.md` promises.
**Approach (assumed at the header base):** the `SubAgentsChoice` tool block lives in `firingConfig` (`cmd/apogee/wire_firing.go`) while `probeContextConfig` (`cmd/apogee/probecontext.go`) calls `bindFiringConfig`; move that block into one shared helper both call (one owner, one reason to change) — see the Regression guard.
**Regression guard.** Fold regression-1's G as binding — the seat-choice tool registry is built by one shared helper (e.g. firingSeatTools(cfg, opts, workspace)) called by firingConfig only after cfg.ReadMounts.Scratch is set and by probeContextConfig after its own scratch binding, never inside bindFiringConfig before the scratch mount exists (registries copy ReadMounts by value; same invariant as item 2); add a test that a sub-agents-choice model Firing's cfg.Tools read_file accepts a path under the run's announced scratch dir; add wire_firing_test.go to Files if that is where it lands.
**Files:** cmd/apogee/wire_firing.go, cmd/apogee/probecontext.go, cmd/apogee/probecontext_test.go, cmd/apogee/wire_firing_test.go
**Read first:** cmd/apogee/wire_firing.go — firingConfig, bindFiringConfig; cmd/apogee/probecontext.go — probeContextConfig, measureContextCost, readContextCost; cmd/apogee/wire_tools.go — registryWithMCP; internal/tools/registry.go — HostToolsOf; internal/agent/construct.go — resolveTools, composesDefaultRoster;
cmd/apogee/wire_firing_test.go — TestFiringConfigLeavesTheDriverSeamsNil, firingRoots, firingBeat; cmd/apogee/wire_settings_test.go — seatChoiceOffered; cmd/apogee/probecontext_test.go — contextCostRowBytes, TestProbeContextNeitherDialsNorWrites
**Tests:** with `SubAgentsChoice=model`, the probe's tool-menu bytes equal a `firingConfig`-built agent's; a `model` Firing's `cfg.Tools` `read_file` accepts a path under the run's announced scratch dir (`cfg.ScratchDir`).
**Acceptance:**
- `go build ./... && go vet ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -parallel 1 -run 'ProbeContext|Firing' ./cmd/apogee/`
**Commit:** `fix(probe): estimate the tool menu a Firing sends`

## 19. The stubllm Recorder records a mid-reply cut as `Cut`

**What:**
**Goal:** when the recorded upstream drops mid-reply (body ends on a non-EOF error), the `Recorder` writes that Turn with `Cut{AfterRunes: <content runes seen>}`, never as a clean Turn.
**Approach (assumed at the header base):** `recordingBody.Read` (`internal/stubllm/record.go`) calls `done` on any error; pass the error through and set `Cut` on a non-EOF end, as `CassetteRecorder`'s `timedBody` marks `truncated`.
**Regression guard.** Set `Cut` only when the ending error is not EOF, is not `context.Canceled`, and no `[DONE]` event was captured (the provider cancels and closes the body at `[DONE]`; an esc mid-stream is no drop), and only on a 2xx reply; on a `Cut` Turn clear `Usage` and `FinishReason`, which `Turn.validateTerminators` refuses beside a cut. The test reads through origin `New(t, Script{Turns: []Turn{{Text: …, Cut: &Cut{AfterRunes: n}}}})` behind `recorderProxy` with a raw client read that tolerates the error (`observe`/`postTo` fail the test on it).
**Files:** internal/stubllm/record.go, internal/stubllm/record_test.go
**Read first:** internal/stubllm/record.go — recordingBody.Read, recordingBody.Close, Recorder.capture, capture.fillFromStream; internal/stubllm/script.go — Cut, Turn.validateTerminators; internal/stubllm/capture.go — timedBody.Read; internal/stubllm/record_test.go — recorderProxy
**Tests:** an upstream that drops mid-stream yields a Script Turn with `Cut` set and no `Usage`/`FinishReason`, and the Script validates; a clean end — including a body closed by the client after `[DONE]` — still yields none.
**Acceptance:**
- `go build ./... && go vet ./internal/stubllm/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -parallel 1 ./internal/stubllm/`
**Commit:** `fix(stubllm): record a mid-reply cut as Cut`

## 20. Pin `cmd/release`'s publish steps to its actions

**What:**
**Goal:** a test fails whenever `publishSteps` and `publishActions` (`cmd/release`) name different steps or order them differently.
**Approach (assumed at the header base):** test-only; compare the two name lists in `cmd/release/publish_test.go`.
**Files:** cmd/release/publish_test.go
**Read first:** cmd/release/probes.go — publishSteps, probeSteps; cmd/release/publish.go — publishActions, publishAction; cmd/release/status_test.go — len(publishSteps) assertions; cmd/release/publish_test.go
**Tests:** the new equality test.
**Acceptance:**
- `go vet ./cmd/release/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -parallel 1 ./cmd/release/`
**Commit:** `test(release): pin the publish steps to their actions`

## 21. Tidy: shared diff hardening in secretsguard, stale test comment

**What:**
**Goal:** `internal/agent/secretsguard.go` takes its diff-hardening flags from `gitexec.DiffHardeningArgs`; `TestApplySettingAcceptsTheStartupOnlyKeys`'s comment states no key count.
**Approach (assumed at the header base):** replace the hand-written `--no-textconv --no-ext-diff` with the shared slice; reword the "five START-UP-only keys" comment in `cmd/apogee/wire_settings_test.go`.
**Regression guard.** No test can see either change, so Acceptance greps them: the literals are gone from `secretsguard.go` and the "five START-UP" comment is gone from `wire_settings_test.go`.
**Files:** internal/agent/secretsguard.go, cmd/apogee/wire_settings_test.go
**Read first:** internal/agent/secretsguard.go — scanStagedSecrets, shadowIndex.git; internal/gitexec/gitexec.go — DiffHardeningArgs; internal/agent/secretsguard_test.go — TestCommitSecretsCleanTreeStaysSilent, TestGuardrails_CommitSecretsForcesApprovalEvenInAuto; cmd/apogee/wire_settings_test.go — TestApplySettingAcceptsTheStartupOnlyKeys
**Tests:** existing secretsguard tests.
**Acceptance:**
- `go build ./... && go vet ./internal/agent/ ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -parallel 1 -run 'Secrets' ./internal/agent/`
- `! grep -n 'no-textconv\|no-ext-diff' internal/agent/secretsguard.go`
- `! grep -n 'five START-UP' cmd/apogee/wire_settings_test.go`
**Commit:** `refactor(agent): use the shared diff-hardening flags in secretsguard`
