# Panel and tool-shape fixes

**Goal:** Fix four open capcom issues: the unknown-key walk's false alarms on data-keyed lists, the sub_agent / fan_out `tools` shapes small models get wrong, the side panel's drag ceiling, and the side panel's missing top row.
**Date:** 2026-11-10
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 51429916

**Sources:**
- `.capcom/issues/section-walk-false-alarm.md`, `.capcom/issues/subagent-tools-arg-shape.md`, `.capcom/issues/transcript-min-width-floor.md`, `.capcom/issues/side-panel-top-gap.md`
- ADR 0110 with its amendments (`capcom know show "ADR 0110" --with-children`), ADR 0111 D7
- `layout.md` §The side panel, `docs/layout/side-panel-layout.md`, `docs/manual/configuration.md` §The side panel

**Ratified design calls (owner, 2026-11-10):**
- **Transcript minimum:** a separator drag or a set width may narrow the transcript to 60 columns, one value for `auto` and `on`. The dock gate (137 / 117) is unchanged.
- **Who chose the width:** an unset `ui.side-panel.width` (the default 44) keeps the gate floor (100 `auto` / 80 `on`) as its clamp; a set width, dragged or written in config, clamps only to the 60-column minimum.
- **tools shape:** lenient decoding, no schema change (Bypass-safe), shared by sub_agent and fan_out / workflow stages. Unknown names and keywords stay refused.
- **Unknown-key walk:** the derived rule: never descend into an entry struct that declares no yaml keys. No per-key exemption.
- **Top row:** the blank row lives in the shared column, so the docked column, the ADR 0111 D7 overlay and the run view all get it. ADR 0110 states no content start row, so no amendment for it.
- **Unset width carried end to end:** an unset `ui.side-panel.width` is a real state from config to renderer.

**Standing requirements:**
- skills: coding-standards
- Tests run one package and named tests only (`go test -race -count=1 -run '<names>' ./<pkg>/`); never a package pattern.
- A deviation from item text lands as a dated NOTES line under the item.

**Out of scope:**
- Reshaping the `tools` schema (needs bench evidence).
- Changing the dock gate or `domain.MinSidePanelWidth`.
- Any version bump.

**Regression check (2026-11-10, 51429916):**
- 1: guard folded (only `sections` misfires today; the watch-homes row is a guard row).
- 3: guard folded (up-front roster check in `workflowCallResult`; roster error leads its own refusal; narrowed workflow run).
- 4: new item, inserted by the owner's decision (unset width carried end to end); former items 4–6 renumbered 5–7.
- 5: recast (depends on item 4; three `TestSidePanelPaintedWidth` wants; comment-site rule).
- 6: guard folded (biting Acceptance greps, one package per run, starter template); yields to adr-0110-amend-1109b (b) for an unset width.
- 7: guard folded (row-shift rule for tests, prose rule by grep, overlay row-0 assertion).
- 4 (re-check): guard folded (`/settings` reset and deleted-key reload land 0, one test each; Tests bullet pointed at the `applyUIRows` rows, not `parseConfigFile`).
- 5 (re-check): guard folded (`on`-mode twin of `separatorDragModel`; the Goal's unset-width clause relies on item 4).

## 1. Unknown-key walk skips entry structs with no yaml keys — ✅ DONE (2026-11-10)

NOTES (2026-11-10): the "one parseConfigFile case" landed as its own test, TestParseConfigFileAnnouncesAnUnknownKeyBesideTheSectionsList (inside the TestParseConfigFile Acceptance pattern), not as a row of an existing test.
NOTES (2026-11-10): the unknownKeys doc comment's list of where the walk does not descend now also names data-keyed entry lists, kept true beside the descendUnknownKeys doc comment the item names.

**What:** fix: `ui.side-panel.sections` and `sessions.watch-homes` entries draw a false "unknown key" notice per entry.
**Regression guard.** Only `ui.side-panel.sections` misfires today: `sessions.watch-homes` is a slice type whose file node is a mapping, so `descendUnknownKeys` already returns before walking it. Its test row is a guard row that passes at the base; the bite comes from the flow and block `sections` rows.
**Goal:** the unknown-key walk reports no key inside a list or map entry whose element struct declares no yaml keys, and still reports a misspelled key beside such a list.
**Approach (assumed at the header base):** in `descendUnknownKeys` (`internal/config/unknownkeys.go`), the slice and map branches return without walking when the element struct has no non-empty key in `schemaKeys(elem)`. Extract that check as one small helper used by both branches. `schemaKeys` and `walkUnknownKeys` (and its two exemptions) stay unchanged. Update the `descendUnknownKeys` doc comment to state the rule. `domain.SidePanelEntry` and `watchHomeConfig` stay untagged.
**Files:** internal/config/unknownkeys.go, internal/config/unknownkeys_test.go
**Read first:** internal/config/unknownkeys.go — descendUnknownKeys, walkUnknownKeys, schemaKeys; internal/config/config.go — sidePanelSectionsConfig.UnmarshalYAML, watchHomesConfig.UnmarshalYAML; internal/domain/sidepanelprefs.go — SidePanelEntry; internal/config/unknownkeys_test.go — TestUnknownKeysWalksTheSchemaAsDeepAsItGoes, TestParseConfigFileAnnouncesUnknownKeysThroughNotify
**Closes:** section-walk-false-alarm

**Tests:** new `TestUnknownKeysLeavesDataKeyedEntriesAlone` in `internal/config/unknownkeys_test.go`, table-driven through `unknownKeys`:
- the documented flow list `sections: [ usage: pinned, sub-agents: auto, tasks: auto, changed-files: auto, mcp: auto ]` → no keys;
- the same list in block style → no keys;
- `sessions.watch-homes` with one label → no keys (guard row: passes at the base);
- `ui.side-panel.widht: 80` and `ui.side-panel.bogus: 1` beside the list → exactly those two keys, at their lines.
Plus one `parseConfigFile` case that the notify text for `ui.side-panel.bogus` is unchanged.

**Acceptance:**
- `go build ./internal/config/`
- `go test -race -count=1 -run 'TestUnknownKeys|TestParseConfigFile|TestRegistry' ./internal/config/`

**Commit:** `fix(config): stop the unknown-key walk flagging data-keyed list entries`

## 2. sub_agent tools: decode the two shapes models send — ✅ DONE (2026-11-10)

NOTES (2026-11-10): deviation from the Approach's string-branch decode: a string is tried as a JSON array only when it starts with `[` (after trimming), so `"null"` stays an unknown keyword rather than decoding to no roster; a `[`-string that is not an array of strings gets its own refusal (`tools: "[1,2]" is not an array of tool names — use "read-only" or an array of tool names`) instead of the unknown-keyword text.
NOTES (2026-11-10): the agent-level wrapped-keyword test compares the `["read-only"]` child's menu to a `"read-only"` child's menu in the same run (default registry), rather than recomputing the set TestSubAgent_ReadOnlyRosterIsThePlanFloorMinusTheHumanSeat derives.

**What:** fix: `"tools": ["read-only"]` and a JSON array sent as a string are refused (6 of 6 first calls from glm-5.3-flash in November).
**Goal:** `SubAgentRoster` decodes `["read-only"]` as the read-only set and a string holding a JSON array of strings as that array. An array that mixes `"read-only"` with tool names, an unknown keyword, or a string that is not an array of strings is refused with a message naming both accepted shapes. The tool schema text is byte-identical to the base.
**Approach (assumed at the header base):** in `internal/tools/sub_agent.go`, add `RosterFromNames(names []string) (SubAgentRoster, error)`: a lone `SubAgentToolsReadOnly` element → `{ReadOnly: true}`; `"read-only"` beside other names → error `tools: pass "read-only" alone as the whole value, not inside a list of tool names`; otherwise `{Names: names}`. `SubAgentRoster.UnmarshalJSON`'s array branch calls it. Its string branch, before the unknown-keyword refusal, tries `json.Unmarshal` of the keyword into `[]string` and, on success, returns `RosterFromNames` of that. Follow the `consoleID.UnmarshalJSON` precedent (`internal/tools/console_common.go`): its comment states why it is lenient; this one says the same. In `requestedChildTools` (`internal/agent/subagent.go`), the unknown-tool refusal becomes `sub_agent tools: unknown tool <names> — name tools from your own menu, or pass "read-only" as the whole value`. `subAgentSchemaTemplate` does not change.
**Files:** internal/tools/sub_agent.go, internal/tools/sub_agent_test.go, internal/agent/subagent.go, internal/agent/subagent_test.go
**Read first:** internal/tools/sub_agent.go — SubAgentRoster.UnmarshalJSON, SubAgentRoster.MarshalJSON, SubAgentToolsReadOnly; internal/agent/subagent.go — requestedChildTools; internal/tools/console_common.go — consoleID.UnmarshalJSON; internal/agent/subagent_test.go — TestSubAgent_UnknownToolNameIsRefusedBeforeAnyChildRuns, TestSubAgent_ReadOnlyRosterIsThePlanFloorMinusTheHumanSeat; internal/agent/state.go — retainedEntryJSON

**Tests:**
- extend `TestSubAgentArgsParsesTheOptionalTools`: `["read-only"]` → ReadOnly; `"[\"read_file\",\"list_dir\"]"` → those Names; `"[\"read-only\"]"` → ReadOnly; `["read-only","read_file"]` refused; `"readonly"` still refused with its pinned text; `"[1,2]"` refused.
- `TestSubAgentPlainSchemaIsTheOneShippedBeforeSeatChoice` passes unchanged (schema untouched).
- new agent-level test: a sub_agent call with `"tools": ["read-only"]` runs its child with the same set `TestSubAgent_ReadOnlyRosterIsThePlanFloorMinusTheHumanSeat` pins; one with the string-encoded array runs with exactly those tools.
- update the pinned refusal text in `TestSubAgent_ARefusedContinueKeepsTheRetainedDelegate` and `TestSubAgent_UnknownToolNameIsRefusedBeforeAnyChildRuns`.

**Acceptance:**
- `go build ./internal/tools/ ./internal/agent/`
- `go test -race -count=1 -run 'TestSubAgentArgs|TestSubAgentPlainSchema|TestSubAgentSchema' ./internal/tools/`
- `go test -race -count=1 -run 'TestSubAgent_|TestState' ./internal/agent/`

**Commit:** `fix(tools): accept ["read-only"] and a string-encoded array for sub_agent tools`

## 3. fan_out and workflow stages: same tools decoding — ✅ DONE (2026-11-10)

NOTES (2026-11-10): the up-front roster check in `workflowCallResult` runs after `ValidateModelPlan` (not straight after `parseFanOutPlan`), so a blank `tools` entry keeps its structured "blank entry" problem rather than being named as an unknown tool; it still runs before `startBackground` / `runBlocking`.
NOTES (2026-11-10): one helper, `(*Agent).stageChildTools` (workflowspawn.go), reads a stage's `tools` through `tools.RosterFromNames` and `requestedChildTools`; both `Spawn` and the up-front check in `workflowCallResult` call it, so the two paths cannot disagree.
NOTES (2026-11-10): an unknown fan_out tool's refusal reads `fan_out was not run: sub_agent tools: unknown tool <names> — …`: the inner text is `requestedChildTools`' own (item 2), which already named fan_out stage refusals "sub_agent tools" before this run; left as is, since rewording it would change item 2's pinned sub_agent text.
NOTES (2026-11-10): docs updated — docs/manual/workflows.md fan_out argument table: `tools` row now says `read-only` alone (also in a workflow file's stage) asks for the read-only set and an unknown name refuses the call before any helper runs.

**What:** fix: fan_out's `tools: ["read-only"]` is refused as an unknown tool and a string-encoded array fails the whole call. Depends on item 2.
**Regression guard.** fan_out has no roster check before its children start (the `requestedChildTools(args.Tools)` check in `backgroundSubAgentResult` is sub_agent's path). Add `a.requestedChildTools(roster)` in `workflowCallResult` after `parseFanOutPlan`, before `startBackground` / `runBlocking`, for the non-recipe form. Decode `fanOutArgs.Tools` as `json.RawMessage` and unmarshal it into `tools.SubAgentRoster` separately, so a roster error is refused as `fan_out was not run: <err>`, never under `fanOutArgumentsFormat`'s "not valid JSON".
**Goal:** a fan_out call's `tools` (and a workflow stage's `tools`) accepts what sub_agent's does: `["read-only"]` (and the bare `"read-only"`) runs each child with the read-only set, a string holding a JSON array of names runs with those names, and unknown names stay refused before any child runs. The fan_out schema text is byte-identical to the base.
**Approach (assumed at the header base):** `fanOutArgs.Tools` (`internal/agent/workflowcall.go`) becomes `json.RawMessage`, decoded into a `tools.SubAgentRoster` through item 2's `UnmarshalJSON` (see the guard). `parseFanOutPlan` stores it into `workflow.Stage.Tools []string` as `[]string{"read-only"}` for a ReadOnly roster and the names otherwise; `workflow.Stage` keeps its `[]string` field (no `internal/tools` import in `internal/workflow`). `workflowspawn.go` builds the roster for `requestedChildTools` with `tools.RosterFromNames(spec.Stage.Tools)`, so a saved workflow file's `tools: [read-only]` resolves the same way. `workflowCallResult` refuses an unknown name with `requestedChildTools` before any child runs (see the guard).
**Files:** internal/agent/workflowcall.go, internal/agent/workflowspawn.go, internal/agent/workflowcall_test.go
**Read first:** internal/agent/workflowcall.go — workflowCallResult, parseFanOutPlan, fanOutArgs, fanOutArgumentsFormat; internal/agent/workflowspawn.go — workflowSpawner.Spawn; internal/agent/subagent.go — requestedChildTools; internal/workflow/plan.go — Stage.Tools; internal/workflow/keyscheme.go — stage hash (Tools omitempty)
**Closes:** subagent-tools-arg-shape

**Tests:** in `internal/agent/workflowcall_test.go` (or the existing fan_out test file the implementer finds): fan_out with `"tools": ["read-only"]` spawns children with the read-only set; with a string-encoded array spawns them with exactly those tools; with `["frobnicate"]` is refused before any child runs, naming the tool; with `["read-only","read_file"]` is refused with text starting `fan_out was not run: ` and carrying item 2's roster error (pinned).

**Acceptance:**
- `go build ./internal/agent/ ./internal/workflow/`
- `go test -race -count=1 -run 'TestFanOut|TestWorkflow' ./internal/agent/`
- `go test -race -count=1 -run 'TestValidate|TestKeyScheme|TestPlan' ./internal/workflow/`

**Commit:** `fix(agent): give fan_out and workflow stages sub_agent's tools decoding`

## 4. Side panel width: carry an unset width end to end — ✅ DONE (2026-11-10)

NOTES (2026-11-10): re-derived from the assumption that `externalEdit.changed` (cmd/apogee/settingsedit.go) and `settingsapply.go` alone carry the reload's reset — the fact has to cross the host/TUI seam, so `tui.AppliedSetting` gains `Reset` (internal/tui/tui.go), `applyReloaded` passes it on (internal/tui/settingswatcher.go) and `settingEdit` gains `unstated` (internal/tui/settings.go).
NOTES (2026-11-10): the reload reports `Reset` for every changed key it finds stated in no file, but only `ui.side-panel.width`'s apply changes (`settingsApplyValue` lands `""` for a width reset or unstated re-read); the journal still records a re-read as a re-read, so no other key's row, marker or dispatch moves (`TestSettingsPaneEmptyValueFromTheEditorIsNotApplied` keeps holding).
NOTES (2026-11-10): consequential edit — internal/config/config.go: made necessary by uiSidePanelWidth's zeroWhenUnstated (the `Width` field comment said an absent key keeps the default, and named 36).
NOTES (2026-11-10): consequential edit — internal/config/configwrite_sections_test.go: made necessary by the absent-key width resolving 0 (`TestSidePanelSectionsBlockWritesAndResetsWidth` read back the default after a reset; the plan's Tests bullet on absent-key expectations covers it).
NOTES (2026-11-10): the settings row's spelling of 0 as 44 lives in a new `sidePanelWidthText` format in keyfield.go; `ParseSidePanelWidth("")` still returns 44, and `UIPrefs.land` lands the block default (0) for `""` before the parser.

**What:** Recast at the regression check (2026-11-10). refactor: an unset `ui.side-panel.width` never reaches the renderer — an absent key, `Set("")` and the width reset all land 44 — so item 5's unset/set split would collapse to "set" for every real user.
**Regression guard.** Ratified call (owner, 2026-11-10), **Unset width carried end to end:** an unset `ui.side-panel.width` is a real state from config to renderer. No user-visible change.
**Regression guard.** Two more producers land a set 44 at the base: a `/settings` reset (`settingsReset` / `settingsSaveToTarget` → `settingsApplied` applies `row.Default`, "44") and a reload after the key is deleted on disk (`externalEdit.changed` → `appliedValue` of the row's read, "44"). A reset edit of `ui.side-panel.width` applies "" (`UIPrefs.Set` lands 0), and the reload treats a key it now finds unstated as a reset; one test per path.
**Goal:** a config with no `ui.side-panel.width` key, a `Set("")` of that key, the panel's width reset (`resetPanelWidth` in `internal/tui/sidepanel.go`), a `/settings` reset of the key (`settingsReset` / `settingsSaveToTarget` → `settingsApplied` in `internal/tui/settingsapply.go`) and a reload after the key is deleted on disk (`externalEdit.changed` in `cmd/apogee/settingsedit.go`) each leave the resolved UI prefs width at 0, meaning unset. Every reader that paints, clamps, reports or shows the width (settings row, `/panel` report, layout) resolves 0 to `domain.DefaultSidePanelWidth`, so what the user sees at the base is unchanged. The reset never persists a numeric width: it removes the key or writes the empty value the parser already treats as default.
**Approach (verify against the tree; name by symbol):** `domain.DefaultUIPrefs` leaves `SidePanelWidth` 0, and `UIPrefs.Set` / `UIPrefs.land` of `UIKeySidePanelWidth` land 0 for an empty value (`ParseSidePanelWidth` still accepts it). In `internal/config/keyfield.go`, `uiSidePanelWidth()` takes the `zeroWhenUnstated` path so `fileProjection` lands 0 for an absent key, and its format resolves 0 to the default so `scalarField.read` (the settings row) shows 44; the registry entry for the key in `internal/config/registry.go` keeps its `Default` text. `resetPanelWidth` stores 0 and keeps `Settings.Reset`. `configuredPanelWidth` stays the one TUI resolver of 0 for layout and paint; the `/panel` report reads through it.
**Files:** internal/domain/uiprefs.go, internal/domain/sidepanelprefs_test.go, internal/config/keyfield.go, internal/config/registry.go, internal/config/config_test.go, internal/tui/sidepanel.go, internal/tui/sidepanel_test.go, internal/tui/settingsapply.go, internal/tui/settings_test.go, cmd/apogee/settingsedit.go, cmd/apogee/settingsedit_test.go
**Read first:** internal/domain/uiprefs.go — DefaultUIPrefs, UIPrefs.land; internal/config/keyfield.go — uiSidePanelWidth, zeroWhenUnstated; internal/tui/sidepanel.go — resetPanelWidth; internal/tui/settingsapply.go — settingsReset; cmd/apogee/settingsedit.go — externalEdit.changed;
internal/config/config_test.go — TestApplyConfigUISidePanelWidthLoadsFromTheGlobalFile

**Tests:**
- `config_test.go` / settings-rows tests that expect `SidePanelWidth: 44` for an absent key (`wantUIDefault` and the `TestApplyConfigUI*` rows) expect 0, and the settings row for the key still shows 44.
- `TestSidePanelWidthValidateAndSetJudgeTheStatedWidth` (`internal/domain`): `DefaultUIPrefs().SidePanelWidth` is 0; `Set("")` lands 0; a stated width lands as stated.
- the `applyUIRows` rows of `TestApplyConfigUISidePanelWidthLoadsFromTheGlobalFile`: "no width key" and "no side-panel block" resolve 0, "a stated width" resolves 48 (`parseConfigFile` returns a `*WholeCount`, nil when absent, and resolves nothing).
- `resetPanelWidth` leaves `m.opts.UI.SidePanelWidth` 0, writes no numeric width, and the panel paints at 44 where it did at the base.
- `internal/tui/settings_test.go`: a `/settings` reset of `ui.side-panel.width` (direct, and through the save-target question) leaves `m.opts.UI.SidePanelWidth` 0 and the row still shows 44.
- `cmd/apogee/settingsedit_test.go` (`TestExternalEditReload…`): a reload after `ui.side-panel.width` is deleted from the file reports the key as a reset and lands 0, not 44.

**Acceptance:**
- `go build ./internal/domain/ ./internal/config/ ./internal/tui/ ./cmd/apogee/`
- `go test -race -count=1 -run 'TestSidePanelWidth|TestUIPrefs' ./internal/domain/`
- `go test -race -count=1 -run 'TestApplyConfig|TestResolvePrecedence|TestUIRows|TestParseConfigFile|TestRegistry' ./internal/config/`
- `go test -race -count=1 -run 'TestSidePanel|TestSettings' ./internal/tui/`
- `go test -race -count=1 -run 'TestExternalEditReload' ./cmd/apogee/`

**Commit:** `refactor(config): carry an unset side-panel width from config to renderer`

## 5. Side panel: a 60-column transcript minimum for set widths — ✅ DONE (2026-11-10)

NOTES (2026-11-10): TestSidePanelPaintedWidth's "auto floor caps" / "on floor caps" rows (60@140, 60@130, now 60) are renamed "auto/on stated fits beside the transcript minimum", since their width no longer caps; the capped rows are "auto/on transcript minimum caps" (100@140 → 79, 100@130 → 69). The "set width wider than the window allows" Tests bullet is the row "a width wider than the window caps" (300@200 → 139), its no-rewrite check the table's shared one.
NOTES (2026-11-10): the new frame test is TestSidePanelDockedAtTheTranscriptMinimumFitsTheFrame; the `on`-mode twin of separatorDragModel is separatorDragModelUnder(t, show, failure), which separatorDragModel now calls with `auto`.
NOTES (2026-11-10): consequential edit — internal/tui/sidepaneloverlay_test.go: made necessary by the new clamp rule (TestOverlayOpensOnRightAtNarrowWidth's comment named "the transcript floor the docked column keeps").

**What:** Recast at the regression check (2026-11-10). the separator drag stops 100 (80 under `on`) columns short of the window edge; lower it for set widths per the ratified calls. Depends on item 4.
**Regression guard.** Item 4 makes an unset width (0) a production state, so the unset/set split reaches real users. Every comment naming the transcript floor as the clamp or as what a docked column leaves gets the new rule, found by `grep -rn "transcript floor\|transcript's floor\|keeps beside\|− 100 −" internal/tui cmd/apogee` (at the base this includes `cmd/apogee/e2e_sidepanel_drag_test.go`'s "160 − 100 − 1 = 59").
**Regression guard.** The Tests name an `on`-mode twin of the `separatorDragModel` test helper (auto-only at the base) for the "under both `auto` and `on`" drag-limit case, and the Goal's unset-width clause relies on item 4 routing the `/settings` reset and the deleted-key reload to 0 (Depends on item 4 stays).
**Goal:** with the panel docked, a set width (dragged or configured) clamps to at most the window less 60 less the scroll-bar column, in both show modes; an unset width clamps to the gate floor (100 / 80) exactly as at the base (relies on item 4); the dock gate (`sidePanelLeastWindow`) is unchanged; a clamped stored width is never written back.
**Approach (assumed at the header base):** in `internal/tui/sidepanel.go`, add `sidePanelTranscriptLeast = 60` beside `sidePanelAutoFloor` / `sidePanelOnFloor`, documented as the drag/layout minimum. `clampPanelWidth` takes whether the width was set: set → `m.width - sidePanelTranscriptLeast - scrollbarWidth`; unset → `sidePanelTranscriptFloor` as today. `panelWidth` passes set when `ui.side-panel.width` is non-zero; `dragPanelSeparator` always passes set. Update every comment the guard's grep finds that names 100/80 as the clamp (in `internal/tui/sidepanel.go`, `internal/tui/model.go`, `cmd/apogee/e2e_sidepanel_drag_test.go`).
**Files:** internal/tui/sidepanel.go, internal/tui/model.go, internal/tui/sidepanel_test.go, cmd/apogee/e2e_sidepanel_drag_test.go
**Read first:** internal/tui/sidepanel.go — clampPanelWidth, panelWidth, dragPanelSeparator, sidePanelTranscriptFloor; internal/tui/sidepanel_test.go — TestSidePanelPaintedWidth, TestSidePanelSeparatorDrag, separatorDragModel;
cmd/apogee/e2e_sidepanel_drag_test.go — TestE2ESidePanelDragResizesAndPersists

**Tests:** in `internal/tui/sidepanel_test.go`:
- `TestSidePanelSeparatorDrag` "a drag past either limit clamps": the left limit is `m.width - sidePanelTranscriptLeast - scrollbarWidth` under both `auto` and `on` (the `on` case through an `on`-mode twin of `separatorDragModel`, which is auto-only at the base).
- "a clamped stored width survives a drag that comes back": pick a stored width still clamped under the new minimum (e.g. 100 at a 140 window).
- `TestSidePanelPaintedWidth`: "auto floor caps" 60@140 → 60, "on floor caps" 60@130 → 60, "auto gate ignores a wide width" 60@137 → 60; keep one capped case per mode (100@140 `auto` → 79, 100@130 `on` → 69); a case seeded from `domain.DefaultUIPrefs()` (not a hand-stored 0) paints 36 at a 137-column `auto` window; a set width wider than the window allows is clamped and `ui.side-panel.width` is not rewritten.
- new frame test: docked at the 60-column transcript minimum with a tool block and the prompt box, every frame line fits the window width and the tool row and prompt render whole.
- `TestSidePanelGate` unchanged.

**Acceptance:**
- `go build ./internal/tui/`
- `go test -race -count=1 -run 'TestSidePanel' ./internal/tui/`

**Commit:** `feat(tui): let a dragged side panel leave the transcript 60 columns`

## 6. Docs and ADR 0110 amendment for the transcript minimum — ✅ DONE (2026-11-10)

NOTES (2026-11-10): consequential edit — .capcom/knowledge/decisions/adr-0110/adr-0110-amend-1108.md: made necessary by the new amendment superseding (a)'s upper limit for set widths (a dated `> **Amended 2026-11-10**` pointer blockquote under (a), as adr-0110-amend-1109b's own pointer there).
NOTES (2026-11-10): consequential edit — .capcom/knowledge/decisions/adr-0110/adr-0110-amend-1109b.md: made necessary by the new amendment superseding (b)'s "docks clamped narrower than 44" for set widths (a dated pointer blockquote under (b)).
NOTES (2026-11-10): the unset-width gate floor is written "100 columns in `auto`, 80 in `on`" rather than "100 under `auto`" so the Acceptance grep for "100 under|80 under" prints nothing while the unset-width rule is still stated; the side-panel-layout.md show-mode table and the gate sentences in configuration.md and config.yaml are restated as window widths (137 / 117).

**What:** state item 5's rule where the clamp is documented. Depends on item 5.
**Regression guard.** Every "the transcript keeps at least N" sentence (docs and the starter template `internal/config/defaults/config.yaml`) is restated as the window gate (≥ 137 / 117) plus the 60-column set-width minimum. adr-0110-amend-1109b (b) ("the default-width column docks clamped narrower than 44") stands for an unset width, which item 4 makes a production state; the amendment supersedes it for set widths only.
**Goal:** ADR 0110 carries a dated amendment naming the 60-column minimum for set widths and the gate floor for an unset width, named on its Status line and reading-order list; `layout.md`, `docs/layout/side-panel-layout.md` and `docs/manual/configuration.md` state the same rule, and no doc states 100/80 as the drag or set-width clamp.
**Approach (assumed at the header base):** add `adr-0110-amend-MMDD` (MMDD = the run's date) with `capcom know add --id adr-0110-amend-MMDD --parent adr-0110`; it supersedes the clamp half of adr-0110-amend-1108 (a) and the "docks clamped narrower than 44" sentence of adr-0110-amend-1109b (b) for set widths. Name it on ADR 0110's Status line and reading-order list. Every doc sentence naming 100/80 as the clamp or the transcript floor during a drag gets the new rule; gate-only statements stay. Find them with `grep -rn "100 under\|80 under\|transcript.s floor\|keeps at least 100" layout.md docs/ internal/config/defaults/`.
**Files:** .capcom/knowledge/decisions/adr-0110.md, .capcom/knowledge/decisions/adr-0110/adr-0110-amend-MMDD.md, layout.md, docs/layout/side-panel-layout.md, docs/manual/configuration.md, internal/config/defaults/config.yaml
**Read first:** .capcom/knowledge/decisions/adr-0110.md — Status line, reading-order list; .capcom/knowledge/decisions/adr-0110/adr-0110-amend-1108.md — (a); .capcom/knowledge/decisions/adr-0110/adr-0110-amend-1109b.md — (a), (b); layout.md — The side panel; docs/layout/side-panel-layout.md — width limits, show-mode table; docs/manual/configuration.md — The side panel; internal/config/defaults/config.yaml — side-panel comment block
**Closes:** transcript-min-width-floor

**Tests:** docs only; the drift tests that pin public-doc counts must still pass.

**Acceptance:**
- `capcom check`
- `grep -n "adr-0110-amend-MMDD" .capcom/knowledge/decisions/adr-0110.md` (MMDD as filed)
- `capcom know show "ADR 0110" --with-children | grep -n "60 columns"`
- `grep -rn "keeps at least 100\|keeps at least 80\|keeps ≥ 100\|keeps ≥ 80\|100 under\|80 under" layout.md docs/layout docs/manual internal/config/defaults/config.yaml` prints nothing
- `go test -race -count=1 -run 'Drift|Doc' ./cmd/apogee/`
- `go test -race -count=1 -run 'Drift|Doc' ./internal/tui/`

**Commit:** `docs(adr): amend ADR 0110 with the 60-column transcript minimum`

## 7. Side panel: blank top row — ✅ DONE (2026-11-10)

NOTES (2026-11-10): the `docs/layout/side-panel-layout.md` §Where it sits sketch gained one transcript row at its foot so its 17 content rows still fit, unsqueezed and spaced, under the new blank top row; the sketch's panel column was shifted down one row rather than re-rendered.
NOTES (2026-11-10): the shifted exact-sequence tests read through a new test helper, `belowPanelTopRow` (internal/tui/sidepanel_test.go), instead of a leading `""` written into every table case; the overflow-table and `+N more` cases grew their row count by one so they keep exercising the same allocation they did at the base.

**What:** the column's first section heading paints flush on the window's top row; the owner wants one blank row above it.
**Regression guard.** Every test that reads `panelText` / `columnText` / `sidePanelContent` as an exact sequence or by index moves one row (`grep -n "panelText(t, \|columnText(\|sidePanelContent(" internal/tui/*_test.go`); keep `panelText` frame-aligned — do not strip row 0 in the helper, since `panelRowY` feeds click y's. Every sentence stating where the column's content starts, which header a blank row precedes, or the overflow's row budget moves with it, found by `grep -n "at its top\|top row\|but the first\|from the top\|rows than the slot" internal/tui/sidepanel.go docs/layout/side-panel-layout.md layout.md docs/manual/configuration.md`.
**Goal:** in the docked column, the overlay and the run view, row 0 of the panel content is blank and not a panel stop, the first heading paints on row 1, the column is exactly the slot's height, overflow budgets one row fewer, clicks and keyboard stops land on the row painted under them, and the separator stays full height.
**Approach (assumed at the header base):** in `panelLayout` (`internal/tui/sidepanel.go`), emit a leading `panelLine{section: -1, isSpacer: true}` and allocate the sections over `max(rows-1, 0)`; `sidePanelContent`'s `nothing live` branch gets a leading blank line too. `panelStops`, `panelTargetAt`, `navPanelStops`, `panelCursorRow` and the pointer shape read `panelLayout`'s indices, so they move with it — do not add a second offset. Redraw the sketch and every sentence the guard's grep finds in `docs/layout/side-panel-layout.md` (§Where it sits, §When it shows, §Sections "Rows and spacing", §Overflow), the matching `layout.md` section and the `internal/tui/sidepanel.go` comments.
**Files:** internal/tui/sidepanel.go, internal/tui/sidepanel_test.go, internal/tui/sidepaneloverlay_test.go, docs/layout/side-panel-layout.md, layout.md
**Read first:** internal/tui/sidepanel.go — panelLayout, sidePanelContent; internal/tui/panelstops.go — panelStops; internal/tui/blockcursor.go — panelCursorRow, navPanelStops; internal/tui/sidepanel_test.go — panelText, panelRowY; internal/tui/sidepaneloverlay_test.go — overlayModel
**Closes:** side-panel-top-gap

**Tests:** in `internal/tui/sidepanel_test.go`:
- `TestSidePanelColumnFillsTheSlotRows`: row 0 blank, `nothing live` on row 1, line count equals the slot rows.
- `TestPanelStopsMatchClickTargets`: no stop at y 0; the first heading's stop is y 1 and a click there hits it.
- shift the rows of every test the guard's grep finds (at the base these include `TestSidePanelOverflowFoldsToHeaders`, `TestSidePanelOverflowMarksAndOpens`, `TestPanelStopsSkipTargetlessRows`, `TestSidePanelSpacesSectionsApart`, `TestSidePanelPaintsEachSection`, `TestSidePanelSectionModes`, `TestPanelAllocationDropsSpacersBeforeFolding`, `TestSidePanelRunViewUsageMatchesTheGauge`).
- in a `TestOverlay*` test in `internal/tui/sidepaneloverlay_test.go` (via `overlayModel` + `panelStopEvents`): the overlay's first stop is at y 1 and the content cells of row 0 are blank; the other overlay tests and the panel nav tests in `internal/tui/navmode_test.go` pass unchanged.

**Acceptance:**
- `go build ./internal/tui/`
- `go test -race -count=1 -run 'TestSidePanel|TestPanel' ./internal/tui/`
- `go test -race -count=1 -run 'TestOverlay|TestNav' ./internal/tui/`

**Commit:** `feat(tui): start the side panel's content one row below the top edge`
