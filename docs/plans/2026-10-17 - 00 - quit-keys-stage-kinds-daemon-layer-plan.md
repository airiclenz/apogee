# Quit keys under modals, workflow stage kinds, daemon Project layer — plan

**Goal:** `ctrl+c` and `ctrl+l` reach their frame handlers under every modal pane; a workflow
block's no-tally fallback counts fan-out items only, as `workflow.TallyOf` does; each
`apogee daemon` firing layers its own workspace's Project config.
**Date:** 2026-10-17
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** a02d2332

**Sources:**
- `.capcom/issues/settings-swallows-quit-keys.md`, `.capcom/issues/workflow-totals-fallback-kinds.md`, `.capcom/issues/daemon-project-layer.md`
- ADR 0096 (`capcom know show "ADR 0096"`, §4 and §6), ADR 0089 (2026-09-30 amendment: tally counts fan-out only), ADR 0055 (daemon reads config once), ADR 0011 (Model copied by value)
- `docs/design/test-drivers.md` (Model-level reducer tests; `internal/testtime` for durations)

**Ratified design calls** (owner, 2026-10-17):
- **Frame chords:** `ctrl+c` (quit double-tap) and `ctrl+l` (repaint) pass through EVERY modal claimant — one pre-check ahead of the key claims, not a per-pane case.
- **Daemon layering:** the startup resolve stays global-only and read once; per firing, that workspace's project layer is read fresh and overlaid on a copy of the startup Options; project-layer notices are latched per Project root and notice text.
- **Stage kinds** (writer, mechanical): kinds ride `WorkflowStarted` as a list parallel to `Stages`, so every row knows its kind before any item arrives (resumed items precede their stage's start).
- **Scope:** `project-config-exec-keys` is out — it needs a grill and an ADR amendment first (open calls logged on the issue).

**Standing requirements:**
- skills: coding-standards
- Bubble Tea v2: keys are `tea.KeyPressMsg` matched on `msg.String()` (`"ctrl+c"`, `"ctrl+l"`).
- Tests on this box: `GOMEMLIMIT=2GiB go test -race -count=1 -run <TestX> <one package>` — never a package pattern, never two runs at once.

**Out of scope:**
- `project-config-exec-keys` (mcp-servers / reactions in the Project config).
- In-session `/schedule` firings (they use the session's live options).
- Allow-rule adoption in the daemon: proposals stay inert and silent there (ADR 0096 §4).

**Regression check (2026-10-17, a02d2332):**
- 1: guard folded (comment-rule grep widened, quit-gesture and rule-field tests named, settings drag-selection cleared by the pre-check, save-target fixture re-cited)
- 3: recast (fallback counts every item but those of a row with a non-fan-out kind) and guard folded (fixtures carry kinds, `Kind` named in the strip docs, Goal counts every round)
- 3 (re-check): guard folded (Goal worded as the binding rule: every finished item except those of a non-fan-out kind, so a kindless row counts)
- 4: guard folded (merge into the startup global document, fresh-slice unions)
- 5: guard folded (startup global document captured once on `daemonWiring`, repo-wide comment grep, startup test kept)
- 6: guard folded (manual-only stale grep, ADR checked by pointer)

## 1. Frame chords pass through every modal pane

**What:**
**Goal:** with any modal claimant open — sessions browser, `/settings` (every `settingsKind` state), model picker, workflows view — a `ctrl+c` arms the quit double-tap and a second one quits, and `ctrl+l` returns `tea.ClearScreen`, the pane's own state unchanged by either key. The prompt pane and the approval rule field keep that behaviour.
**Approach (assumed at the header base):** `Model.handleKey` (`internal/tui/model.go`) walks `m.claimKey(keyClaimOrder, msg)` before its global `switch msg.String()`; `modalClaim` claims every key. Add one frame-chord check ahead of `claimKey` that routes `"ctrl+c"` and `"ctrl+l"` to the existing global cases (extract them into a helper both paths call — one owner for each chord). The pre-check also precedes the `editingProjectRule` → `projectRuleKey` check, so remove that function's now-redundant `case "ctrl+c", "ctrl+l"` carve-out (`internal/tui/approval.go`). Rewrite every comment that says a modal swallows ctrl chords / every key (the rule: grep `internal/tui` for `swallow` near `ctrl`), including the ctrl+l comment in `handleKey`, the settings row in `panes.go`, and `Model.settingsKey`'s doc.
Fix of `settings-swallows-quit-keys` (not a regression: the behaviour is as old as the modal claim).
**Regression guard.** The comment rule is every prose claim that a modal pane takes, swallows or claims every key or keypress: `grep -rniE 'swallow|every key(press)?|claims every|belongs to it|owns the keyboard' internal/tui/*.go layout.md docs/layout docs/manual` (at base also layout.md:354 and :2500, `modalClaim`, panes.go:39/147/199/214, sessions.go:23, settings.go:20, workflows.go:317, model.go:2955). The frame-chord pre-check clears `m.settings.sel` as `handleKey` clears `m.sel`, keeping the every-keypress contract (settings.go:609, `docs/manual/commands.md` "the only thing it takes with it"), so "pane state unchanged" excepts the drag-selection.
**Files:** internal/tui/model.go, internal/tui/approval.go, internal/tui/panes.go, internal/tui/settings.go, internal/tui/sessions.go, internal/tui/workflows.go, layout.md, internal/tui/clearscreen_test.go, internal/tui/framechords_test.go, docs/layout/settings-screen-layout.md
**Read first:** internal/tui/model.go — Model.handleKey, Model.claimKey, modalClaim; internal/tui/approval.go — Model.projectRuleKey;
internal/tui/settings.go — Model.settingsKey; internal/tui/model_test.go — ctrlCQuit, TestModelStopKeys; internal/tui/settings_test.go — TestSettingsFilterSurvivesEverySubStep
**Closes:** settings-swallows-quit-keys
**Tests:**
- New `internal/tui/framechords_test.go`: a table over the four modal claimants and all six `settingsKind` states (`settingsKeyList`, `settingsEnumList`, `settingsValueBuffer`, `settingsResetArmed`, `settingsTextEditor`, `settingsSaveTarget` — the last via a ProjectCapable row built with `settingsProjectBoolRow` / `settingsEditModel` / `pressSettings` in `settings_test.go`, the six rows reusing the case table of `TestSettingsFilterSurvivesEverySubStep`): first `ctrl+c` sets `lastCtrlC` and leaves the pane open and in the same state, the second quits (`ctrlCQuit`); `ctrl+l` yields `tea.ClearScreen` with the pane state unchanged (a drag-selection excepted: `ctrl+l` over a drag-selected `/settings` value buffer drops its highlight).
- A rule-field row in `framechords_test.go`: `projectRulePane(t, terminalGate("go test ./..."), nil)` then `p` (`approval_test.go`); `ctrl+c` and `ctrl+l` leave the field open with its value unchanged, and the quit arms.
- `TestCtrlLKeySwallowedByModalOverlay` inverted and renamed to `TestCtrlLKeyRepaintsUnderModalOverlay`, workflows view added.
**Acceptance:**
- `go build ./... && go vet ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestFrameChord|TestCtrlL|TestCtrlC|TestModelStopKeys|TestModelApprovalRuleField|TestSettings' ./internal/tui/`
- `docs/layout/settings-screen-layout.md` states that `ctrl+c` and `ctrl+l` keep their frame meaning while the pane is open.
**Commit:** `fix(tui): let ctrl+c and ctrl+l through every modal pane`

## 2. Carry stage kinds on WorkflowStarted

**What:**
**Goal:** a `domain.WorkflowPhaseEvent` of phase `WorkflowStarted` carries `StageKinds []string`, index-aligned with `Stages`, each the plan stage's `workflow.StageKind` as a string; the NDJSON line is unchanged.
**Approach (assumed at the header base):** `observeWorkflow` (`internal/agent/workflowcall.go`) builds `workflowObserver{stages: stageNames(plan), …}`; add the kinds from the same plan walk and emit them in `startLocked` beside `slices.Clone(o.stages)` (resume included). `domain` cannot import `workflow`, so the field is plain `[]string` (as `Origin` is a string). Document it in the `WorkflowPhaseEvent` doc paragraph that describes `Stages` (`internal/domain/events.go`) and name it in `internal/eventjson/encode.go`'s in-process-only member comment; `workflowPhaseData` stays unchanged.
**Files:** internal/domain/events.go, internal/agent/workflowcall.go, internal/agent/workflowcall_test.go, internal/eventjson/encode.go
**Read first:** internal/agent/workflowcall.go — observeWorkflow, workflowObserver, stageNames, startLocked; internal/domain/events.go — WorkflowPhaseEvent;
internal/eventjson/encode.go — workflowPhaseData; internal/workflow/plan.go — StageKind; internal/agent/workflowcall_test.go — workflowPhaseEvents
**Tests:** a `workflowcall_test.go` case: a plan with a fan-out and a verify stage emits `WorkflowStarted` with `StageKinds == ["fanout","verify"]`, also on a resumed run.
**Acceptance:**
- `go build ./... && go vet ./internal/agent/ ./internal/domain/ ./internal/eventjson/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestWorkflow' ./internal/agent/`
- `GOMEMLIMIT=2GiB go test -race -count=1 ./internal/eventjson/`
**Commit:** `feat(workflow): carry stage kinds on the WorkflowStarted event`

## 3. Fallback totals count fan-out items only

**What:** Recast at the regression check (2026-10-17).
**Goal:** a workflow block without a tally (running, failed) shows "items N" counting every finished item (every round) except those of a stage whose kind is not fan-out; a session record written before stage kinds were kept still loads and still counts every item.
**Approach (assumed at the header base):** `workflowStage` (`internal/tui/workflowblock.go`) gains `kind string`, set from `StageKinds` where `addWorkflowPhase` opens the block via `workflowStagesOf`, and preserved where `withStage` rebuilds a row for a new round. `workflowView.totals()`: with no tally, count every item except those whose stage row carries a non-empty kind other than `string(workflow.StageFanout)`. Persist the kind: `session.WorkflowStage` gains `Kind string \`json:"kind,omitempty"\`` (`internal/session/transcript.go`), copied in `toWireWorkflow` / `fromWireWorkflow` (`internal/tui/transcriptbridge.go`).
Depends on item 2.
**Regression guard.** The fallback counting rule is binding as: with no tally, count every item EXCEPT those whose stage row carries a non-empty kind other than string(workflow.StageFanout) — so a kindless row (an older record, or a row withStage appends for a stage the started phase did not name, as fixture-built test views do) still counts, and no "any row has a kind" switch exists.
`fold` has no `WorkflowStarted` case, so the test fixtures carry kinds: `verifiedOneItem`'s started phase carries `StageKinds ["fanout","verify"]`, and `liveWorkflowView` opens its stages (names + kinds) from a leading `WorkflowStarted` phase when given one, keeping the scan/confirm default for the star tests. `Kind` joins End and the stage states as the closed enums the `session.Workflow` and `stripWorkflow` docs exempt from stripping, and the `WorkflowStage` doc names it. The count is every round's fan-out items, not `workflow.TallyOf`'s latest round (`workflowItem` carries no round; no test pins a repeat).
**Files:** internal/tui/workflowblock.go, internal/tui/workflowblock_test.go, internal/tui/transcriptbridge.go, internal/session/transcript.go, internal/session/transcript_test.go
**Read first:** internal/tui/workflowblock.go — withStage, fold, totals; internal/tui/workflowblock_test.go — verifiedOneItem, liveWorkflowView;
internal/tui/transcriptbridge.go — toWireWorkflow, fromWireWorkflow; internal/session/transcript.go — stripWorkflow
**Closes:** workflow-totals-fallback-kinds
**Tests:**
- `TestWorkflowBlockWithoutATallyCountsItsItems` expects the fan-out count ("items 1 · ok 1" for `verifiedOneItem`) running and failed; its helpers open the block with kinds (`verifiedOneItem`'s started phase carries `StageKinds`, `liveWorkflowView` folds a leading `WorkflowStarted`).
- A kindless row (a stage the started phase did not name) still counts its items beside a kinded verify row whose items are not counted.
- `TestWorkflowBlockReplaysTheEndPhaseTally`: the "older record" case clears each row's kind as well as the tally and still expects every item counted.
- A round-trip test: kinds survive `toWireWorkflow` → `fromWireWorkflow`; a JSON record without `kind` decodes.
**Acceptance:**
- `go build ./... && go vet ./internal/tui/ ./internal/session/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestWorkflowBlock' ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 ./internal/session/`
**Commit:** `fix(tui): count only fan-out items in a workflow block's fallback totals`

## 4. Config: overlay one Project layer onto resolved Options

**What:**
**Goal:** `internal/config` exports one function that, given already-resolved Options and a Project root, returns a copy with that root's project layer applied — project-param and tighten-only keys with the same semantics `ResolveOptions` gives them, granting (`allow:`) entries classified but never made live beyond what adoption grants — and reports the layer's notices (skipped file, global-only keys, unreadable adoption record) through a notify callback. The input Options are not mutated.
**Approach (assumed at the header base):** extends `internal/config/layer.go` (no new package): reuse `readProjectLayer` / `projectWalk` and the per-key `fromFile` appliers that `parseLayeredConfig` + `applyFile` already run, scoped to non-global-only rows of the registry (`KeyClass`); the tighten-only merges (`tools.disabled`, `url-safety.deny-hosts`, `DangerousRules.ProjectAdd`) go through the same code as the startup path, not a restatement. A key that also has an env var or flag keeps env/flag above the project layer (precedence default < global < project < env < flag).
**Regression guard.** The overlay merges the project layer into the startup GLOBAL document (`mergeProjectLayer` over global bytes captured once at startup and passed in — never a per-call re-read of config.yaml, ADR 0055), then re-runs the project-capable rows' `fromFile` on the merged `fileConfig`; overlaying onto resolved Options would lose the global `context-files` block (both its rows write one field through `fileContextFiles`). Tighten-only unions build a fresh slice (`slices.Clone`), never appending into the base's spare capacity, which concurrent firings share.
**Files:** internal/config/layer.go, internal/config/layer_test.go
**Read first:** internal/config/layer.go — parseLayeredConfig, readProjectLayer, mergeProjectLayer; internal/config/config.go — ResolveOptions, applyFile, fileContextFiles;
internal/config/allow.go — classifyProjectRules; internal/config/layer_test.go — resolveLayeredStartup
**Tests:** in `layer_test.go`: overlay of a project file setting `context-files.names`, `workflow-retries` and `tools.disabled` yields the same values `ResolveOptions` gives for that root; global `names: [A.md]` + project `enable: true`, and global `enable: false` + project `names: [X]`, each match `resolveLayeredStartup`; a global-only key logs one notice and is dropped; a broken project file yields the base Options plus one skipped notice; the input Options are unchanged afterwards, the base `tools.disabled` / `url-safety.deny-hosts` lists built with cap > len and checked as `base[:cap(base)]`.
**Acceptance:**
- `go build ./... && go vet ./internal/config/`
- `GOMEMLIMIT=2GiB go test -race -count=1 ./internal/config/`
**Commit:** `feat(config): overlay a single Project layer onto resolved options`

## 5. Daemon firings layer their workspace's Project config

**What:**
**Goal:** every `apogee daemon` firing runs with Options that carry its own workspace's project layer, read at that firing; two schedules in two workspaces never see each other's layer; a broken or global-only-key project file logs its notice to the daemon log once per root and notice text, not on every firing; the daemon startup resolve stays global-only.
**Approach (assumed at the header base):** `daemonWiring.fire` (`cmd/apogee/daemonfire.go`) already computes `roots := resolveRoots(w.opts.ConfigDir, entry.Run.Workspace)`; call item 4's overlay with `roots.project` and pass the result as `firingInputs.opts` to `raise` in place of the shared `w.opts`. Latch notices beside `latchPrewarm` / `latchWindowUnknown` under `w.mu`. `runDaemonWith` (`cmd/apogee/daemon.go`) keeps `opts.GlobalConfigOnly = true`. Rewrite every comment that says the daemon reads no Project config or that a firing's `ProjectRoot` is empty (grep `cmd/apogee/daemon*.go`, `cmd/apogee/wire_config.go`, and the `GlobalConfigOnly` doc in `internal/config/options.go`).
Depends on item 4.
**Regression guard.** If item 4's overlay function takes the startup global config document as input, runDaemonWith/newDaemonWiring capture that document once at daemon start and hold it on daemonWiring, and fire passes it to the overlay — the global file is never re-read per firing (ADR 0055 read-once kept).
The comment rule is repo-wide: `grep -rn -i -E 'daemon.{0,80}(project config|project layer)' --include=*.go .` (at base also `cmd/apogee/project_cmd_test.go:268`, `internal/config/allow_test.go:269`). `TestDaemonTakesNoProjectLayer` stays as the startup guard, renamed `TestDaemonStartupTakesNoProjectLayer` with a reworded comment; the firing test is a new one.
**Files:** cmd/apogee/daemonfire.go, cmd/apogee/daemon.go, cmd/apogee/wire_config.go, internal/config/options.go, cmd/apogee/daemonfire_test.go, cmd/apogee/daemon_test.go, cmd/apogee/project_cmd_test.go, internal/config/allow_test.go
**Read first:** cmd/apogee/daemonfire.go — daemonWiring.fire, daemonWiring.latchPrewarm, newDaemonWiring; cmd/apogee/daemon.go — runDaemonWith; cmd/apogee/wire.go — resolveRoots;
cmd/apogee/daemonfire_test.go — newDaemonFireHarness, entryFor; cmd/apogee/daemon_test.go — TestDaemonTakesNoProjectLayer
**Tests:**
- `TestDaemonTakesNoProjectLayer` kept, renamed `TestDaemonStartupTakesNoProjectLayer` (daemon start prints no project-config notice); new `TestDaemonFiringTakesItsWorkspacesProjectLayer` in `daemonfire_test.go` on `newDaemonFireHarness` / `entryFor`, reading `spec.Config.ContextFiles`.
- `daemonfire_test.go`: two workspaces, one wiring, each with a different `context-files.names` — each firing's `run.Spec` carries its own; a project file edited between two firings is read fresh; a global-only key in a project file logs once across two firings.
**Acceptance:**
- `go build ./... && go vet ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestDaemon' ./cmd/apogee/`
**Commit:** `feat(daemon): layer each firing's own Project config`

## 6. Docs and ADR 0096 amendment for per-firing layering

**What:**
**Goal:** no shipped doc or decision record says the daemon reads no Project config; ADR 0096 records the per-firing layering as an amendment.
**Approach (assumed at the header base):** add an amendment Note under `adr-0096` (`capcom know add --parent adr-0096`, shaped like `adr-0076-amend-1010`) recording: per-firing overlay of the workspace's project layer, startup resolve global-only and read once (ADR 0055 kept), notices latched per root, proposals still inert and silent. Point §4's daemon sentence in `adr-0096-decision.md` at it. Update every manual passage that says the daemon reads no Project config or only the global file (the rule: grep `docs/manual/` for `daemon` near `Project config` / `global file`) — at base: `configuration.md` "Who reads it", `daemon.md`, `reactions.md`'s Project-config paragraph.
Depends on item 5.
**Regression guard.** The stale-statement grep is `grep -rn -i 'daemon reads\|only config the daemon\|global file alone' docs/manual/`, and every hit is reviewed (at base also `daemon.md:139-140` and `reactions.md:298-299`; `probe.md:148` is not about the daemon). The ADR keeps its original §4 sentence, as the house amendment shape does (`adr-0076-decision.md:86`), so it is checked by a pointer grep — the amendment Note's id appears in `adr-0096-decision.md` §4 — never by the stale-text grep.
**Files:** .capcom/knowledge/decisions/adr-0096/adr-0096-decision.md, .capcom/knowledge/decisions/adr-0096/ (new amendment Note), docs/manual/configuration.md, docs/manual/daemon.md, docs/manual/reactions.md
**Read first:** .capcom/knowledge/decisions/adr-0096/adr-0096-decision.md — §4 Adoption; .capcom/knowledge/decisions/adr-0076/adr-0076-amend-1010.md — amendment Note shape; docs/manual/configuration.md — "Who reads it";
docs/manual/daemon.md — "config.yaml is read once" paragraph; docs/manual/reactions.md — url-safety.deny-hosts paragraph; .capcom/knowledge/decisions/adr-0055/adr-0055-consequences.md — config.yaml read once
**Closes:** daemon-project-layer
**Tests:** none (docs); the drift tests guard counts only.
**Acceptance:**
- `capcom check` reports no problems.
- `grep -rn -i 'daemon reads\|only config the daemon\|global file alone' docs/manual/` — every hit reviewed, none a stale statement about the daemon.
- `grep -n 'adr-0096-amend' .capcom/knowledge/decisions/adr-0096/adr-0096-decision.md` finds the §4 pointer to the amendment Note.
**Commit:** `docs(daemon): record per-firing Project layering`
