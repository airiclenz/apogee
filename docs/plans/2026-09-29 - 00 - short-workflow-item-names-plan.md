# Short workflow item names

**Goal:** A workflow item's child is shown under a short name — never an absolute path under the
apogee home — on every Driver surface, and the status line never loses the context gauge to a long
name. The item's label stays the identity key (resume hashes, `{item}` output paths, model-facing
result lines).
**Date:** 2026-09-29
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 5135ef98

**Regression check (2026-09-29, 5135ef98):**
- 1: recast — an engine-set label (a merge stage's) is kept; resumed items asserted; the short name persisted as `ItemStatus.Name`
- 2: guard folded — `docs/manual/workflows.md` states the short-name rule, shared short names, and the full label or run id as the tiebreak
- 3: guard folded — a merge-shaped phase still reads the stage name
- 4: recast — the pane shows `ItemStatus.Name`, falling back to `ItemStatus.Label`
- 5: guard folded — the gauge's room comes out of the phrase only; yields to layout.md "And what the left slot sheds" and "And the background workflows' readout"
- 2 (re-check): guard folded — status-detail "running items" lines and "message queued for" reply show the short name; approvals carry it as `sub_agent_name` (headless line, reaction payload); the label recorded on `workflowChild` for `runningItems`
- 4 (re-check): guard folded — shared `workflowsFixture` left as is; Goal restated and the `ItemName`-from-units Approach struck; output path pinned through a stage `out:`; script/ask lines take the `Label` fallback

**Sources:**
- `internal/skills/shipped/audit/SKILL.md` — the producer: `split.sh` writes absolute `<RUN>`, `<RUN>/part-*`, `<RUN>/group-*` entries its `pick` stages read
- `internal/workflow/items.go` (`itemLabel`), `internal/workflow/store.go` (`ItemKey`, `PlanHash`, `Store.Dir`)
- `layout.md` — "The workflow block", "The status line's right slot"
- ADR 0031 (Driver sufficiency), ADR 0075 (event-line shape), ADR 0087 (engine runs workflows)

**Ratified design calls** (owner, 2026-09-29):
- **Where:** the engine derives the short name; `WorkflowPhaseEvent` carries it beside `Item`, and the item's child agent is named with it, so every Driver shows it.
- **Rule:** each unit that is an absolute path is respelled relative to the workflow's folder; the folder itself reads as the stage name; an absolute path outside it reads as its basename; any other unit is unchanged. A multi-unit item joins the respelled units the way `itemLabel` joins units.
- **Gauge:** the status line's left slot is composed to leave the context gauge its room; the gauge is dropped only when the window cannot fit it at all.
- **Label unchanged:** `Item.Label`, `ItemKey`, `PlanHash`, `{item}` expansion and the model-facing result lines keep the full label (follows from the identity-key finding, 2026-09-29).
- **Headless line:** the `workflow_phase` JSON line keeps its shape — the short name is a Driver display field, not on the line (ADR 0075 precedent for `Stages`/`Rounds`/`Run`).

**Standing requirements:**
- skills: coding-standards
- The uncommitted stage-outcome edits in the working tree (`internal/domain/events.go`, `internal/agent/workflowcall.go`, `internal/tui/workflowblock.go`, …) are committed before this plan runs.

**Out of scope:**
- Changing the audit recipe's `split.sh` to write relative entries (would change `ItemKey`s and break resume of in-flight runs).
- Shortening the model-facing workflow result lines (`internal/workflow/format.go`).

## 1. Engine derives a short item name — ✅ DONE (2026-09-29)

NOTES (2026-09-29): ItemStatus.Name is tagged `json:"name,omitempty"` rather than a bare `"name"`, so script/ask lines and pre-plan folders write no empty key; the store test pins that the named line reads "name": "part-a" and an unnamed line has no name key.
NOTES (2026-09-29): the runner keeps the workflow folder on runState (a new `dir` field set in Run from Store.Dir) and the short name on itemJob (`name`), computed once in prepareItems; ItemSpec.Name, ItemEvent.Name and ItemStatus.Name all read it, so a resumed item's event carries it too.
NOTES (2026-09-29): ItemName also falls back to Label for an item with no units, and when the respelled name is blank (the folder itself under a blank stage name); a unit that is a sibling sharing the folder's prefix (`<dir>-other`) reads as its basename, not a relative `../` path.
NOTES (2026-09-29): the store test pins ItemKey and PlanHash golden values, computed identically on the base tree and the changed tree.
NOTES (2026-09-29): the full-package Acceptance run (`go test -race -count=1 ./internal/workflow/`) was refused by this session's permission classifier; only `go build ./...`, `go vet ./internal/workflow/` and a narrowed `-race -run` over the new and adjacent resume/round-trip tests were run (all pass). The verifier needs to run the full Acceptance command.

**What:**
Recast at the regression check (2026-09-29).
**Goal:** `internal/workflow` exports a function giving an item's short name from its units, the workflow's folder and its stage name per the ratified **Rule**; `ItemSpec` and `ItemEvent` carry that name for every item the runner spawns or reports, a resumed item included.
**Approach (assumed at the header base):** add `ItemName(item Item, dir, stage string) string` beside `itemLabel` in `items.go` (it reuses `itemLabel`'s join over respelled units; a non-absolute unit passes through; a blank result falls back to `item.Label`). Add a `Name` field to `ItemSpec` and `ItemEvent` in `runner.go`; the runner fills both from the workflow folder it already resolves via `Store.Dir`. `Label` and `Key` are untouched.

**Regression guard.** ItemName returns item.Label unchanged whenever Label != itemLabel(Units) (an engine-set label, e.g. a merge stage's Item{Label: stage.Name, Units: [<dir>/stages/<stage>/manifest.md]}), respelling only derived labels; the ItemName table test gains a merge row ("report" stays "report"). The runner test also re-runs the same plan over the same store so the item resumes and asserts the resumed ItemEvent.Name is the short name. Item 1 also persists the short name in status.json as ItemStatus.Name (json "name"; ItemKey and PlanHash unchanged — they hash label/items only), so the /workflows pane can read it; add internal/workflow/store.go and its test to Files.
**Files:** internal/workflow/items.go, internal/workflow/runner.go, internal/workflow/store.go, internal/workflow/items_test.go, internal/workflow/runner_test.go, internal/workflow/store_test.go
**Read first:** internal/workflow/items.go — itemLabel; internal/workflow/runner.go — Runner.Run (Store.Dir local `dir`, runState has no dir field), runState.prepareItems (ItemStatus literal), runState.runItem (ItemSpec literal), runState.notifyItem;
internal/workflow/store.go — ItemStatus; internal/workflow/stages.go — runMerge (Item{Label: stage.Name}); internal/workflow/runner_test.go — recordingSpawner
**Tests:** table test for `ItemName`: `<dir>` → stage name; `<dir>/part-foo` → `part-foo`; `<dir>/a/b` → `a/b`; `/elsewhere/x.md` → `x.md`; `src/main.go` and `alpha` unchanged; three absolute units → `part-a … part-c (3)`; a merge row (`Label` `report`, unit `<dir>/stages/report/manifest.md`) → `report`. A runner test asserting the `ItemSpec.Name` a Spawner sees and the `ItemEvent.Name` an Observer sees for an item whose unit is the workflow folder itself, then re-running the same plan over the same store and asserting the resumed item's `ItemEvent.Name` is the short name. A store test: `ItemStatus.Name` round-trips through status.json as `"name"`, and `ItemKey` / `PlanHash` are unchanged.
**Acceptance:** `go build ./... && go test -race -count=1 ./internal/workflow/`
**Commit:** `feat(workflow): derive a short display name for each item`

## 2. Children and phase events carry the short name — ✅ DONE (2026-09-29)

NOTES (2026-09-29): the spawn test drives the finished phase by calling the observer's `ItemPhase` with the item's event rather than through a Runner, since the item's unit has to be the workflow folder and that folder's id is only known once a run has started; the Runner filling `ItemEvent.Name` is item 1's runner test.
NOTES (2026-09-29): the eventjson "no new key" pin sets `ItemName` on the existing item_finished row of the Encode table and leaves its expected `data` unchanged, rather than adding a new test.
NOTES (2026-09-29): `workflowMessageResult`'s matching moved into a new `namedItems` helper: run id, else every item with that short name, else every item with that full label; a short name therefore wins over another item's label that reads the same.
NOTES (2026-09-29): per this machine's rules the Acceptance `go test -race -count=1 ./internal/agent/` was run as a non-race whole-package run plus `-race -run 'TestWorkflowSpawn|TestWorkflowControl|TestWorkflowCall|TestDelegationName'`; eventjson and domain were race-run whole, one package at a time.
NOTES (2026-09-29): ADR 0089 ("by the run id or item name") and the `workflow` tool's `item` description ("run id or name, as status lists it") were left as written; both still hold, since status now lists the short name.

Depends on item 1.

**What:**
**Goal:** a workflow item's child agent is named with the item's short name (so approval and ask prompts name it so), `WorkflowPhaseEvent` carries it as `ItemName` on `WorkflowItemStarted` and `WorkflowItemFinished`, the workflow message control finds a running item by run id, short name or label, and the `workflow_phase` JSON line is byte-identical to before. The model-facing `workflow` status detail's "running items" lines and its "message queued for %s" reply (built from `child.displayName()` in `workflowcall.go`) then show the short name, while the full label is still accepted for addressing. A workflow child's approval carries the short name as `sub_agent_name` on the headless approval line (`internal/eventjson/encode.go`) and in the reaction payload (`internal/domain/seampayload.go`, set in `internal/agent/dispatch.go`); the item's CHANGELOG entry says "an approval from a workflow item names the item's short name". Fix for the reported defect: children named `/home/…/.apogee/scratch/<session>/workflows/<id>` crowd the TUI.
**Approach (assumed at the header base):** add `ItemName string` to `domain.WorkflowPhaseEvent` (`internal/domain/events.go`, documented beside `Item`); `workflowObserver.itemStarted` / `ItemPhase` (`internal/agent/workflowcall.go`) set it from `spec.Name` / `event.Name`; `workflowSpawner.Spawn` (`internal/agent/workflowspawn.go`) passes `delegationName(spec.Name)` to `newChildAgentOn`; `runningItem` gains the label so `workflowMessageResult` matches run id, then name, then label. `internal/eventjson/encode.go` leaves `workflowPhaseData` alone — add `ItemName` to its comment listing the display fields not on the line. Re-export in `apogee.go` only if the event's fields are mirrored there.

**Regression guard.** docs/manual/workflows.md states that items are shown by their short name (relative to the workflow folder, the folder itself as the stage name, a path outside it as its basename), that two items can share a short name (e.g. /a/x.go and /b/x.go both read x.go), and that the workflow message control then accepts the full label or run id to tell them apart. (a) The status detail's "running items" lines and the "message queued for %s" reply show the short name while the full label is still accepted for addressing; the message-control test pins one status-detail line with the short name. (b) A workflow child's approval carries the short name as `sub_agent_name` on the headless approval line (`internal/eventjson/encode.go`) and in the reaction payload (`internal/domain/seampayload.go`, `internal/agent/dispatch.go`); the CHANGELOG entry says "an approval from a workflow item names the item's short name". (c) `runningItems` builds from `a.children.all()` (`*Agent`), which holds no label: record `spec.Item.Label` on the child (a `label` field on `workflowChild`, set in `workflowSpawner.Spawn`) and read it in `runningItems`; the new spawn tests set `spec.Name` explicitly (or the `itemSpec` test helper gains a name parameter).
**Files:** internal/domain/events.go, internal/agent/workflowcall.go, internal/agent/workflowspawn.go, internal/agent/workflowcall_test.go, internal/agent/workflowspawn_test.go, internal/eventjson/encode.go, docs/manual/workflows.md
**Read first:** internal/agent/workflowspawn.go — workflowSpawner.Spawn, workflowChild; internal/agent/workflowcall.go — workflowObserver.itemStarted, workflowObserver.ItemPhase, workflowMessageResult, runningItems, runningItem;
internal/agent/workflowspawn_test.go — itemSpec
**Tests:** spawn test (setting `spec.Name` explicitly, or via an `itemSpec` name parameter): an item whose unit is the workflow folder yields a child whose name is the stage name and phases whose `ItemName` is it while `Item` is the full path; an approval raised by that child carries the short `SubAgentName`; `workflow` message control addressed by the short name and by the full label both queue to the same run, and the reply and one status-detail "running items" line read the short name. An eventjson test pins that the `workflow_phase` line has no new key.
**Acceptance:** `go build ./... && go test -race -count=1 ./internal/agent/` then `go test -race -count=1 ./internal/eventjson/ ./internal/domain/`
**Commit:** `fix(agent): name a workflow item's child by its short name`

## 3. TUI workflow rows and run heads show the short name — ✅ DONE (2026-09-29)

NOTES (2026-09-29): one helper, `itemShownName(e)` (ItemName, else Item), feeds the item head (`addWorkflowItem` → `workflowItemView`, hence breadcrumbs, status phrase, legend and `fan_out` card rows), the block's finished-item `label` and `workflowItemLine`; `e.Item` is no longer displayed anywhere in internal/tui, and no identity match in the package used it.
NOTES (2026-09-29): layout.md "The workflow block" did not say items are shown by label; a sentence stating the short-name rule and the older-record label fallback was added to the "A stage row opens its stage's work" paragraph. The `workflowItem` struct comment was updated to say its `label` now holds the shown name.
NOTES (2026-09-29): `fan_out` card rows are not tested separately — they are the same item heads `addWorkflowItem` seats, whose Target/agentName the new run-view test pins.
NOTES (2026-09-29): Acceptance was run as `go build ./... && GOMEMLIMIT=2GiB go test -race -count=1 -run 'Workflow|FanOut|RunView|Breadcrumb|StageView|ShortName' ./internal/tui/` (the plan's regex plus `ShortName`, since the new stage-view test's name matches `StageView` and the block test's `Workflow` anyway); both new tests fail with the workflowblock.go change stashed.

Depends on item 2.

**What:**
**Goal:** every TUI surface fed by a workflow item phase — the item run head (`workflowItemView`), hence breadcrumbs, status line phrase, run-view legend and notices; the workflow block's not-ok item lines; `fan_out` card rows — shows `ItemName`, falling back to `Item` when a phase carries none (an older transcript).
**Approach (assumed at the header base):** in `internal/tui/workflowblock.go`, read one helper (e.g. `itemShownName(e)`) wherever `e.Item` is displayed: the item-head construction that calls `workflowItemView(label)`, the view fold that records an item's `label`, and `workflowItemLine`. Keys/matching that use `e.Item` for identity stay on `e.Item`. Persisted `Target` then holds the short name (`transcriptbridge.go` round-trips it unchanged). Update `layout.md` "The workflow block" if it says items are shown by label.

**Regression guard.** A merge stage's item keeps its stage name (`Item{Label: stage.Name, Units: [<dir>/stages/<stage>/manifest.md]}`, `runMerge` in `internal/workflow/stages.go`): item 1's `ItemName` returns an engine-set label unchanged, so the merge head, status phrase and legend still read the stage name. The tests add a merge-shaped phase (`ItemName` == `Item` == stage name) that reads the stage name.
**Files:** internal/tui/workflowblock.go, internal/tui/workflowblock_test.go, internal/tui/runview_test.go, layout.md
**Read first:** internal/tui/workflowblock.go — addWorkflowItem, workflowItemView, workflowView.fold, workflowItemLine; internal/tui/subagentblock.go — headCrumbs, stageTrail;
internal/tui/workflowblock_test.go — itemStartedUnder, itemFinishedUnder
**Tests:** a workflow block fed item phases with `Item` = an absolute path and `ItemName` = `part-foo`: the item head's target, the stage view row, the breadcrumb trail and a not-ok item line read `part-foo` and never contain the path; a phase with empty `ItemName` shows `Item`; a merge-shaped phase (`ItemName` == `Item` == the stage name) reads the stage name.
**Acceptance:** `go build ./... && go test -race -count=1 -run 'Workflow|FanOut|RunView|Breadcrumb|StageView' ./internal/tui/`
**Commit:** `fix(tui): show a workflow item by its short name`

## 4. `/workflows` pane shows the short name

Depends on item 1.

**What:**
Recast at the regression check (2026-09-29).
**Goal:** the `/workflows` pane's item list and item title show `ItemStatus.Name`, else `ItemStatus.Label`; `workflow.ItemOutputPath` keeps `item.Label`.

**Regression guard.** the pane shows ItemStatus.Name, falling back to ItemStatus.Label when empty (a status.json written before this plan); it does not recompute the name from units (ItemStatus has none). Script and ask lines write no ItemStatus.Name (only the runner's `prepareItems` does), so the Label fallback is the normal path for them, not only for pre-plan folders — the test covers one such item. `workflowsFixture`'s items stay as they are (`TestWorkflowsViewWalksDownToAnItemAndEscGoesBackUp` and `TestWorkflowsViewClickOpensAnItemAndAClickOutsideCloses` pin `#1 internal/a.go` / `#2 internal/b.go`); the new test mutates its own copy of the returned infos, as `TestWorkflowsViewRefreshesOnAWorkflowPhase` does. The new test's `find` stage has `out: "{item}/tools.md"` in the plan.json `ItemOutputPath` reads, and the item level reads `output: <label>/tools.md`, not the Name's.
**Files:** internal/tui/workflows.go, internal/tui/workflows_test.go
**Read first:** internal/tui/workflows.go — workflowDetailRows, workflowItemTitle, workflowOutputLines (ItemOutputPath keeps item.Label); internal/workflow/store.go — ItemStatus, ItemOutputPath;
internal/tui/workflows_test.go — workflowsFixture, workflowsPaneModel, TestWorkflowsViewRefreshesOnAWorkflowPhase
**Tests:** a new test over its own copy of `workflowsFixture`'s infos (the shared fixture unchanged) whose `find` items carry absolute-path labels and `Name` = the stage name and `part-a`, with the `find` stage's `out: "{item}/tools.md"` in that folder's plan.json: the list and the item title read the stage name and `part-a`; the script stage's `flags` line (no `Name`) reads its `Label`; the item level reads `output: <label>/tools.md`, not the Name's. The existing pane tests stay green unchanged.
**Acceptance:** `go build ./... && go test -race -count=1 -run 'Workflows' ./internal/tui/`
**Commit:** `fix(tui): name items by their short name in the workflows pane`

## 5. The status line keeps the context gauge

**What:**
**Goal:** whenever the window is wide enough to hold the gauge beside a one-column gap and the left slot's lead, the status line shows the gauge; the left slot's phrase is trimmed to leave it that room. Fix for the reported defect: a long run name dropped the gauge.
**Approach (assumed at the header base):** `Model.statusLine` (`internal/tui/model.go`) composes `statusLeft` to the full width and drops `statusRight` whole when `gap < 1`. Compose the left slot to the width less the gauge occupant's width (plus margin and gap) when the right slot holds the gauge; other right-slot occupants keep today's drop rule. Keep `statusLeft`'s spend order (queued count last to go). Update `layout.md` "The status line's right slot" / "And what the left slot sheds" to state the reservation.

**Regression guard.** The gauge's room comes out of the PHRASE only: the gauge shows whenever lead + trail (queued count and workflow readout, kept whole) + one-column gap + gauge + margin fit, otherwise it is dropped as today; the idle / trail-only slot is never trimmed for it. This yields to layout.md "And what the left slot sheds" (count last to go) and "And the background workflows' readout" (whole or not at all). Keep `statusLeft()` a no-argument wrapper over a new width-taking composer (`leftStatus`, `internal/tui/workflow_test.go`, calls it).
**Files:** internal/tui/model.go, internal/tui/model_test.go, layout.md
**Read first:** internal/tui/model.go — Model.statusLine, Model.statusLeft, Model.statusRight, Model.contextGauge; internal/tui/workflow.go — Model.statusTrail; internal/tui/interject.go — Model.queuedSegment;
internal/tui/interject_test.go — TestSuppressedBandKeepsItsCountOnTheStatusLine; internal/tui/workflow_test.go — leftStatus
**Tests:** a running frame whose single live child is named by a 150-character name at widths 80 and 120: the status row contains the gauge and is exactly one window wide; at a width too narrow for the gauge the row still squares onto the band. A narrow running frame with queued messages and a lit gauge where lead + queued count + gap + gauge do not fit keeps the count whole and drops the gauge. Existing `TestStatusLineGaugeEndsShortOfEdge` and the queued-count tests stay green.
**Acceptance:** `go build ./... && go test -race -count=1 -run 'StatusLine|StatusPhrase|Gauge|Queued' ./internal/tui/`
**Commit:** `fix(tui): keep the context gauge on the status line beside a long name`
