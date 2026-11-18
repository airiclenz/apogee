# Settled tool groups collapse; panel width while watching — plan

**Goal:** A settled, untouched `✦ Tools` umbrella folds to one Group summary row (verb phrases, diffstat, failures), every umbrella header reads as that summary, and `ui.tools-auto-collapse` (default `true`) switches the collapse. While watching a session, the side panel's width (drag, double-click reset, `/panel width`) works as outside a watch; pins stay refused.
**Date:** 2026-11-18
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** b705b7ed

**Sources:**
- `.capcom/issues/settled-tool-group-clutter.md` (owner decisions 2026-11-18); `.capcom/issues/panel-drag-blocked-watching.md`
- `docs/layout/tool-layout.md` §Vocabulary, §Grouped tools folded (large / small umbrella); ADR 0035 addenda `adr-0035-addendum-0917`, `adr-0035-addendum-0918`; glossary `tool-summary`
- ADR 0110 + `adr-0110-amend-1108` (c); ADR 0111; `docs/layout/side-panel-layout.md`

**Ratified design calls:**
- **Write verbs (owner, 2026-11-18):** write_file and the three edit tools all read "edited N files" (distinct paths); no "created".
- **Large-umbrella click (owner, 2026-11-18):** opening an auto-collapsed large umbrella opens that one only, as in-session view state; `ui.tools-open` is not written. A click on a live or not-auto-collapsed large umbrella keeps today's global toggle + save.
- **Run views (owner, 2026-11-18):** the same rule applies inside a sub-agent run view; "running" there means the child is still running.
- **Unlisted tools (writer, from the issue's fallback):** a tool with no phrase in the item 3 table renders `<Label> ×N`, built-in or MCP alike.
- **Last-resort header (writer):** the count fallback is today's `Tools (N calls)` text; a failure/denied count survives it.
- **Watch scope (issue):** only the width forms (`/panel width <n>|reset`, drag, double-click) join the watch-allowed set; bare `/panel`, `on|off|auto`, `pin|unpin|hide` stay refused.

**Standing requirements:**
- skills: coding-standards
- Machine rule (forward to every sub-agent): run tests only as `GOMEMLIMIT=2GiB go test -race -count=1 -run TestX ./<one package>/`; never a package pattern, never two test runs at once.
- Bubble Tea v2 names only (`tea.KeyPressMsg`, `msg.String()`); driven tests use `internal/tuitest` + `internal/stubllm`, never an `httptest` upstream.
- Durations in tests go through `internal/testtime`.

**Out of scope:**
- Sub-agent groups, Skill groups and lone tool blocks (`settled-group-collapse-scope`).
- Persisting "touched" or a per-umbrella fold to the session record.
- Any engine or `domain.ToolSummary` change; the summary is built in the TUI from `toolView`.
- `/panel on|off|auto|hide` while watching.

**Regression check (2026-11-18, b705b7ed):**
- 1: recast — the reset restores the registry default of `ui.side-panel.width` (44), not 36; guards folded (watch-gate comment sweep, transcript note kept while watching, reset awaited by the line's removal).
- 2: guard folded — the default named as the registry default; Acceptance line for "separator does not drag"; the finder matches line text, not paths.
- 3: guard folded — `settingsApplyLocal` arm, value pair and renderer mirror land here; Acceptance runs real tests, one package per run.
- 4: guard folded — the builder's input carries each call's done state; denied from a real engine source; diffstat sums diffed edits only; distinct paths from `argsWire`.
- 5: guard folded — every caller of the umbrella header helpers, the `mouse_test.go` locators and the stale Go comments.
- 6: guard folded — one fold-resolution helper; turn-live set in `markRunning`, cleared in `finishWorker` and `watchIdle`; setters `touch()`; size-only comments swept; supersedes `adr-0035-addendum-0918` via item 10.
- 7: guard folded — click and panel paths branch on item 6's helper; a touched∧ownFold head flips its own flag; the panel jump opens the member's type row and never writes.
- 8: guard folded — item 3 owns the apply arm and its mirrors; item 8 adds only the transcript setter.
- 9: guard folded — the `false` clause stated as behaviour.
- 10: guard folded — sweep covers the registry Desc and starter template; `side-panel-layout.md` mock-up; `adr-0035-addendum-0917` named partly superseded; Acceptance greps for the Goal's docs.
- 1 (re-check): guard folded — the e2e reset is a typed `/panel width reset`, the watching double-click pinned at Model level on a stubbed clock; comment finder widened to `internal/tui/*.go`, `interject.go` in Files; Approach's conditional `showFlash` sentence deleted.
- 4 (re-check): guard folded — the denied count has no fallback: flag set live in `absorbFailure` from the two engine denial prefixes, re-derived in `fromWireToolView` from `w.Details`; live and wire round-trip test.

## 1. Side panel width works while watching — ✅ DONE (2026-11-18)

NOTES (2026-11-18): added a `panelVerb = "panel"` constant beside `panelWidthVerb` in sidepanel.go and used it in the command table's `/panel` row (command.go) as well as the new watch-gate helper `isWatchPanelWidth` (watch.go), since the verb name now carries meaning in two places.
NOTES (2026-11-18): the watching double-click test enters the watch with `m.enterWatch(fixture.src)` on `doubleClickModel`'s model rather than `enterWatchOn`, which builds a fresh model and would drop the stubbed clock; `TestSidePanelCommandIsRefusedWhileWatching` now covers `/panel`, `/panel on` and `/panel pin tasks`.
NOTES (2026-11-18): user docs still say `/panel` is unavailable while watching (docs/manual/commands.md, docs/manual/configuration.md) — left for item 2, which owns the docs.

**What:** Recast at the regression check (2026-11-18).
**Goal:** While a watch is active, a press on the side panel grip starts a drag that reflows live and writes `ui.side-panel.width` on release, a grip double-click resets it to the registry default of `ui.side-panel.width`, and `/panel width <n>` and `/panel width reset` run as outside a watch; a pin click and every other `/panel` form stay refused with `watchRefusedFlash`.
**Approach (assumed at the header base):** In `internal/tui/sidepanel.go`, drop `m.watch.isActive` from `panelGripAt`'s guard (keep the overlay exclusion) and its "inert while watching" doc line. In `internal/tui/watch.go`, `watchRefuses(parsed)` admits `command == "panel"` only when `parsed.verbArgs` is a `panelArgs` whose action is `panelSetWidth` or `panelResetWidth`; `watchVerbs` is unchanged (autocomplete's arg-less accept stays refused). Correct the `/panel` doc comment in `internal/tui/command.go`. Width stays read from `m.opts.UI.SidePanelWidth` (global key, ADR 0111).
**Regression guard.** The double-click and `/panel width reset` restore the registry default of ui.side-panel.width (44 at base, per adr-0110-amend-1109b), never a pinned 36 — tests read the default from the registry/defaults rather than hard-coding it. Reset REMOVES the key (`Settings.Reset`), so the e2e awaits the separator at W−default and the `width:` line gone from config.yaml with `drv.WaitFor`, not the presence-only `awaitConfigCarries`. The e2e drives that reset with a typed `/panel width reset`, never a double-click (the process has no clock seam, so under -race a second press past the 500 ms `panelDoubleClick` arms a drag); the grip double-click while watching is pinned at Model level with `doubleClickModel`'s stubbed clock after `enterWatchOn`. No `showFlash` branch: `runPanelCommand` keeps its transcript note while watching, as `/version` does (saves are gated by the nil `m.sessions`). Every comment saying the watch gate admits only `watchVerbs`, or the grip / `/panel width` is inert or refused while watching, is corrected — find with `grep -rn -i 'every other verb\|view-only set\|view-only verb\|inert' internal/tui/*.go` (excluding `inertEngine`), which reaches commandrun.go's "Watch mode runs only its view-only verbs" and interject.go's "only a view-only verb runs".
**Files:** internal/tui/sidepanel.go; internal/tui/watch.go; internal/tui/command.go; internal/tui/commandrun.go; internal/tui/interject.go; internal/tui/sidepanel_test.go; cmd/apogee/e2e_sidepanel_watch_test.go
**Read first:** internal/tui/watch.go — watchRefuses, watchVerbs; internal/tui/sidepanel.go — panelGripAt, handleSidePanelClick, runPanelCommand, setPanelWidth, resetPanelWidth, panelAction; internal/tui/commandrun.go — runCommand, queueCommand; internal/tui/interject.go — sendAtIdle, stageWhileRunning, stageCommand;
internal/tui/sidepanel_test.go — TestSidePanelSeparatorDragIsInertWhileWatching, TestSidePanelCommandIsRefusedWhileWatching, doubleClickModel; internal/tui/watch_test.go — enterWatchOn;
cmd/apogee/e2e_sidepanel_drag_test.go — TestE2ESidePanelDragResizesAndPersists, separatorAt; cmd/apogee/wire_session_test.go — runElsewhere
**Tests:** invert `TestSidePanelSeparatorDragIsInertWhileWatching` into a test that the drag resizes and writes; in `TestSidePanelCommandIsRefusedWhileWatching` keep `/panel on`, move the two width lines to a new test asserting they apply (the reset landing the registry default, read from `domain.DefaultSidePanelWidth`); `TestSidePanelPinClickRefusedInWatchMode` stays. New Model-level test: `doubleClickModel` (stubbed clock) then `enterWatchOn`, two grip presses within `panelDoubleClick` reset the width to the registry default and start no drag. New driven `TestE2ESidePanelDragResizesWhileWatching` (model on `TestE2ESidePanelDragResizesAndPersists`; enter the watch with `runElsewhere` + `--resume <id>`): drag persists via `awaitConfigCarries`, a typed `/panel width reset` restores the registry default (separator at W−`domain.DefaultSidePanelWidth`, `width:` line gone, both awaited with `drv.WaitFor`).
**Acceptance:**
- `go build ./... && go vet ./internal/tui/ ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestSidePanel' ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestE2ESidePanelDrag' ./cmd/apogee/`
**Commit:** `fix(tui): let the side panel width change while watching a session`

## 2. Record the watch-mode width rule in the ADR and docs — ✅ DONE (2026-11-18)

NOTES (2026-11-18): consequential edit — .capcom/knowledge/decisions/adr-0110/adr-0110-amend-1108.md: made necessary by adr-0110-amend-1118 superseding (c)'s watch-mode sentence; a dated `> **Amended 2026-11-18**` pointer blockquote under (c), as 1109b/1110 already do under (a), with (c)'s own text left as written.
NOTES (2026-11-18): no CHANGELOG entry for this docs item — item 1's entry already announces the user-facing change.
NOTES (2026-11-18): layout.md states no watch-mode panel rule, so it needed no edit; the side-panel-layout.md pin bullet ("a click on a pin glyph writes nothing … flashes the watch refusal") already held and stays as written.

**What:** Depends on item 1.
**Goal:** `adr-0110-amend-1118` exists, supersedes `adr-0110-amend-1108` (c)'s watch-mode sentence (width is the viewer's own preference, like a section fold; pins stay refused), and is named on ADR 0110's Status line and reading-order list; no doc states the drag or `/panel width` is inert or refused while watching.
**Approach (assumed at the header base):** `capcom know add --id adr-0110-amend-1118 --parent adr-0110`. Rule for docs: every sentence saying the separator does not drag, the grip is inert, or `/panel` is refused while watching — find with `grep -rn -i 'watch' docs/manual docs/layout layout.md | grep -i -E 'panel|drag|separator|grip'` — now names the width forms as allowed. Known sites at base: `docs/manual/configuration.md` watch paragraph, `docs/layout/side-panel-layout.md` grip and `/panel` bullets, `docs/manual/commands.md` `/panel` row, `docs/manual/sessions.md` view-only command list. Archived plans and the ADR's own Decision text stay as written.
**Regression guard.** The amendment and docs name the width default as the registry default (not 36). The rule's finder matches the line text, never the `path:line` prefix (a path containing "panel" is no hit): `grep -rh -i -E 'watch.*(panel|drag|separator|grip)|(panel|drag|separator|grip).*watch' docs/manual docs/layout layout.md`; the separator sentence without "watch" on its line (`configuration.md` "the separator does not drag") is pinned by the Acceptance line below.
**Files:** .capcom/knowledge/decisions/adr-0110.md; .capcom/knowledge/decisions/adr-0110/adr-0110-amend-1118.md; docs/manual/configuration.md; docs/layout/side-panel-layout.md; docs/manual/commands.md; docs/manual/sessions.md
**Read first:** .capcom/knowledge/decisions/adr-0110.md — Status line, reading-order sentence; .capcom/knowledge/decisions/adr-0110/adr-0110-amend-1108.md — (c); .capcom/knowledge/decisions/adr-0110/adr-0110-amend-1109b.md — default 44; docs/manual/configuration.md — "Watching and resuming" paragraph;
docs/layout/side-panel-layout.md — grip bullets, /panel bullets; docs/manual/commands.md — /panel row; docs/manual/sessions.md — "While you watch" bullet
**Tests:** none new; docs drift tests.
**Acceptance:**
- `capcom check`
- `! grep -rn -E 'separator does not drag|grip is inert while' docs/manual docs/layout layout.md`
- `capcom know show "ADR 0110" | grep -q adr-0110-amend-1118`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestManual|TestDocs' ./cmd/apogee/`
**Closes:** panel-drag-blocked-watching
**Commit:** `docs(panel): record that the panel width changes while watching`

## 3. Add the `ui.tools-auto-collapse` setting — ✅ DONE (2026-11-18)

NOTES (2026-11-18): the key sits right after `ui.tools-fold-over` (not between it and `ui.tools-open`) in `uiKeys`, the registry, the template and every enumerating test, so the `tools-open`/`tools-fold-over` pair that refers to each other stays adjacent; gofmt realigned the `UIKey*` constant block and `DefaultUIPrefs` literal for the longer name.
NOTES (2026-11-18): `registry_test.go`'s owns table and documented-value table (`TestRegistrySetIsTheInverseOfRead`, `TestUIRowsLandThroughUIPrefsSet`) and `config_test.go`'s `TestEveryConfigKeyReachesTheOptions` fixture plus a load case gained the key; the manual and template prose describe the rule items 6–8 implement, ahead of item 10's full write-up.

**What:**
**Goal:** `ui.tools-auto-collapse` is a registry-declared, editable bool defaulting to `true`, present in `domain.UIPrefs` (`ToolsAutoCollapse`), the file config, the starter template and `docs/manual/configuration.md`'s tools section; the registry and docs drift tests pass. No TUI behaviour reads it yet.
**Approach (assumed at the header base):** Mirror `ui.tools-open` end to end: `UIKeyToolsAutoCollapse`, `uiKeys` order, `UIPrefs.Set` case via `landBool`, default `true` in the defaults, `uiConfig.ToolsAutoCollapse *bool \`yaml:"tools-auto-collapse"\``, a registry row next to `ui.tools-open` (`KindBool`, `Editable: true`), `internal/config/defaults/config.yaml`. Update every `UIPrefs` literal a test compares against the default (`config_test.go`, `cmd/apogee/settingsrows_test.go`, `wire_boot_test.go`, `wire_settings_test.go`).
**Regression guard.** A new `uiKeys` entry with an Editable row and no apply turns `TestSettingsApplyLocalLandsEveryUIKeyThroughSet` and `TestEveryEditableSettingKeyHasAnApply` red, so item 3 also adds a `settingsApplyLocal` arm that only lands `m.opts.UI.Set` (item 8 later adds the transcript setter), that test's value pair (accepted `"true"`: `testUIPrefs` leaves it false), and `"ui.tools-auto-collapse"` in `settingKeysAppliedByTheRenderer`.
**Files:** internal/domain/uiprefs.go; internal/domain/uiprefs_test.go; internal/config/config.go; internal/config/registry.go; internal/config/registry_test.go; internal/config/defaults/config.yaml; internal/config/config_test.go; docs/manual/configuration.md; cmd/apogee/settingsrows_test.go; cmd/apogee/wire_boot_test.go; cmd/apogee/wire_settings_test.go; internal/tui/settingsapply.go; internal/tui/settingsapply_test.go
**Read first:** internal/domain/uiprefs.go — uiKeys, UIPrefs.land, DefaultUIPrefs, UIKeyToolsOpen; internal/config/registry.go — ui.tools-open row, uiBool; internal/config/config.go — uiConfig.ToolsOpen; internal/config/registry_test.go — TestToolsOpenRowIsAnEditableBoolDefaultingOff;
internal/config/defaults/config.yaml — ui: tools-open line; cmd/apogee/settingsrows_test.go — fabricatedSettings, TestSettingsRowsFormatEffectiveValues; cmd/apogee/wire_settings_test.go — settingKeysAppliedByTheRenderer, TestEveryEditableSettingKeyHasAnApply;
internal/tui/settingsapply_test.go — TestSettingsApplyLocalLandsEveryUIKeyThroughSet
**Tests:** a `TestToolsAutoCollapseRowIsAnEditableBoolDefaultingOn` modelled on `TestToolsOpenRowIsAnEditableBoolDefaultingOff`; the uiprefs table rows; the `ui.tools-auto-collapse` value pair in `TestSettingsApplyLocalLandsEveryUIKeyThroughSet`.
**Acceptance:**
- `go build ./...`
- `GOMEMLIMIT=2GiB go test -race -count=1 ./internal/domain/`
- `GOMEMLIMIT=2GiB go test -race -count=1 ./internal/config/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestManualDocumentsEverySettingsKey|TestSettingsRows|TestRunRootCarriesTheUIPrefsWhole|TestEveryEditableSettingKeyHasAnApply|TestSettingsTableIsInRegistryOrder' ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run TestSettingsApplyLocalLandsEveryUIKeyThroughSet ./internal/tui/`
**Commit:** `feat(config): add the ui.tools-auto-collapse setting`

## 4. Build the Group summary text — ✅ DONE (2026-11-18)

NOTES (2026-11-18): consequential edit — internal/tui/doc.go: made necessary by the new groupsummary.go (the package file map, pinned by TestDocMapNamesEveryFile).
NOTES (2026-11-18): groupPhrase carries one field beyond the approach's list, `uncounted`, for "checked git" (a phrase with no number); the phrase values live as shared vars in groupsummary.go and each registry row points at one (`toolPresenter.group`), so phrase equality is what merges grep+find and the four writing tools.
NOTES (2026-11-18): the builder's input is `[]groupCall{view toolView; done bool}` (item 5 builds it from the umbrella's paint inputs); its output is `groupSummary` segments tagged with a `groupTone` (phrase, calls fallback, added, removed, outcome) that the painter maps to styles — no theme styles are applied here.
NOTES (2026-11-18): first-word capitalisation applies only when the list opens with a verb phrase; a leading `<Label> ×N` keeps the label's own spelling (an MCP `github · search` is a name, not a word of the sentence).
NOTES (2026-11-18): the clip step clips the `Tools (N calls)` head into the room the failed/denied segments leave; only where no clipped head would fit beside them are they dropped and the head clipped to the whole budget. The count text is composed from superGroupLabel/superCallNoun, so umbrellaHeaderText's own copy remains until item 5 replaces that header.
NOTES (2026-11-18): the two engine denial sentences are restated in the TUI (approverDenialPrefix, reactionDenialPrefix) as the delegation markers are; no test pins them to internal/agent, whose reaction prefix is unexported and whose approver sentence is a literal. Replay reads the first plain-kind body line only when the slot carries a failure, so an edit's argument-derived -/+ lines above the refusal do not hide it.

**What:**
**Goal:** A pure TUI function turns an umbrella's runs into its Group summary header text at a width budget: verb phrases from a per-tool phrase table, in first-run order of each phrase, first word capitalised; distinct-path counts for read/list/edit phrases; summed `+A −R` diffstat; `· N failed` and `· N denied` counted apart; the present-participle verb on a phrase with a call in flight; the width fallback. Nothing calls it yet.
**Approach (assumed at the header base):** The phrase is presenter knowledge: extend `toolPresenter` in `internal/tui/toolregistry.go` with a group phrase (past verb, live verb, singular/plural noun, distinct-by-path flag); no parallel table. Phrases: read_file "read/reading file(s)" distinct; grep, find_files "searched/searching pattern(s)"; terminal, python_exec "ran/running command(s)"; write_file, edit_existing_file, single_ and multi_find_and_replace "edited/editing file(s)" distinct; web_fetch, http_request "fetched/fetching page(s)"; list_dir "listed/listing dir(s)" distinct; copy/move/delete_file "copied/moved/deleted file(s)"; git_status, git_log, git_show, git_diff_range, git_branch "checked/checking git" (no count); git_commit "made/making N commit(s)"; anything else `<Label> ×N`. Distinct key is the call's path argument, never a parse of the rendered `Target`. Group by phrase, not by `Label`. Failed = `branchSummary.failed` minus denied (`Summary.Text == deniedSummary`); diffstat reuses `sumStats`/`statValue`. Width fallback, in order: keep first two phrases + `and K more`; drop the diffstat; `Tools (N calls)`; clip. The failure/denied segment is dropped last. New file `internal/tui/groupsummary.go`; the segment paint (diff colours, red outcome) is returned as styled segments for item 5.
**Regression guard.** The Group summary builder's input carries each call's done state (and whatever else the live verb needs) so item 5 can call it without changing its signature — item 4 settles that input shape, item 5 feeds it from the state renderSuperGroup already holds. Denied has a real source and the denied count has no fallback: no live path writes `deniedSummary` (a denial arrives as an error result). The denied flag is set live in `absorbFailure` from the two engine denial prefixes ("tool call denied by approver" in internal/agent/dispatch.go, `gateDenialPrefix` "tool call denied by reaction " in internal/agent/gate.go) and re-derived on replay in `fromWireToolView` (internal/tui/transcriptbridge.go) from the persisted failure body (`w.Details`), following the existing precedent there that re-derives a verdict from the stored body; the row's own slot text is unchanged. A test drives a denied result live and through a wire round-trip (`toWireEntry` → `fromWireToolView`) and asserts `· 1 denied` both times. The diffstat sums only edit-phrase members whose `Summary.stat` is diffed and skips the rest (`sumStats` fails on the first mismatch). Distinct paths read the decoded `argsWire` (`args` is nil for most presenters); a distinct-flagged call with no readable path counts as its own item, never 0.
**Files:** internal/tui/toolregistry.go; internal/tui/toolview.go; internal/tui/transcriptbridge.go; internal/tui/groupsummary.go; internal/tui/groupsummary_test.go
**Read first:** internal/tui/toolregistry.go — toolPresenter, toolRegistry; internal/tui/toolview.go — sumStats, statValue.add, absorbFailure, toolView.argsWire, deniedSummary; internal/tui/toolblock.go — toolRunView, superRunViews, umbrellaBreakdown; internal/tui/wireargs.go — wireArgs, decodeArgsPreservingNumbers;
internal/tui/blockstate.go — anyOpenCall; internal/tui/paintcache.go — entryState.done; internal/agent/dispatch.go — gate denial result
**Tests:** table tests: mixed kinds and order, distinct files (3 reads of one file → "Read 1 file"), singular/plural, merged grep+find, diffstat sum (one write_file + one failed edit beside two diffed edits), failed vs denied (a real denied result driven through `enrichWithResult`, then through `toWireEntry` → `fromWireToolView`, `· 1 denied` both times), live verb from the per-call done input, a distinct call with `argsWire` nil, `<Label> ×N` for MCP and an unlisted built-in, each fallback step at shrinking budgets.
**Acceptance:**
- `go build ./... && go vet ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestGroupSummary' ./internal/tui/`
**Commit:** `feat(tui): build a verb-phrase Group summary for tool umbrellas`

## 5. Render every umbrella header as its Group summary — ✅ DONE (2026-11-18)

NOTES (2026-11-18): re-derived from "renderSuperGroup already holds" the per-call done state: toolRunView carried no done state, so it gains a `done []bool` (memberDone, bounded like memberExpanded) filled in superRunViews by a new `doneFlags` in internal/tui/blockstate.go, next to memberFlags, its twin; umbrellaGroupCalls turns the runs into item 4's `[]groupCall`.
NOTES (2026-11-18): umbrellaHeaderText and umbrellaBreakdown are removed; umbrellaHeaderPaint keeps its name but now paints the summary segment by segment: phrases in th.toolHeader (the header's own tone), the count fallback as before (bold-gold `Tools` + faint parenthetical, now umbrellaCountPaint), failed/denied in th.errorText. "Diff colours" maps to the diffAdded/diffRemoved bands (composed under the header foreground, the leading gap left unbanded), because the scheme has no foreground diff role and the failure red must stay the only red text on the line.
NOTES (2026-11-18): TestUmbrellaHeaderBreaksDownByToolKind is renamed TestUmbrellaHeader_ShowsTheGroupSummary and re-pinned; it gains the narrow-width fallback subtest on a real four-kind umbrella with a failure (whole → "and 2 more" → `Tools (4 calls) · 1 failed` → clip before the failure count), plus paint subtests for the failure red, the count fallback and the diffstat bands.
NOTES (2026-11-18): the mouse_test.go locators umbrellaHeaderLine/umbrellaHeaders now go through isUmbrellaHeaderLine: the star or blank star cell, a letter straight after it (not a branch marker), and the leader dot running straight into the ▶/▼ with no outcome slot between. No targetUmbrella mark is read.
NOTES (2026-11-18): consequential edit — internal/tui/toolleader.go: made necessary by removing umbrellaHeaderText (the leaderTargetBudget comment named it).
NOTES (2026-11-18): consequential edit — internal/tui/doc.go: made necessary by the header change (the package map spelled the old `✦ Tools (3 Read, 4 Terminal)` header).
NOTES (2026-11-18): consequential edit — internal/tui/blockstate.go: made necessary by the done state the summary's live verb needs (doneFlags beside memberFlags; see the re-derive note).
NOTES (2026-11-18): consequential edit — internal/tui/settingsapply_test.go: made necessary by the header change (TestGlyphsApplyRepaintsThePaintedBlocks pinned the umbrella as "✦ Tools"/"♦ Tools"; it is now pinned as "✦ Read 1 file"/"♦ Read 1 file"). This falls under the item's Goal that no test pins the old header.
NOTES (2026-11-18): consequential edit — internal/tui/theme_test.go: made necessary by the header change (TestSafeGlyphFrameDrawsNoUnicodeMarks and TestUnicodeGlyphFrameIsTheDefaultLook pinned "♦ Tools"/"✦ Tools"; re-pinned the same way).

**What:** Depends on item 4.
**Goal:** Every `✦` tools umbrella header (open, folded, live, settled; main transcript and run view) renders the Group summary, keeping the `✦`, its blink while live, and the fold glyph; no test or golden pins the old `Tools (3 Read, …)` breakdown.
**Approach (assumed at the header base):** `umbrellaHeader` / `umbrellaHeaderText` in `internal/tui/toolblock.go` call item 4's builder with the existing `leaderTargetBudget` budget; remove `umbrellaBreakdown` and its paint once unused. Update the helpers `umbrellaHeaderRow` / `blinkedUmbrellaHeaderRow` (`render_test.go`) and the literal header strings in `toolblock_test.go`, `mouse_test.go`, `render_test.go`; in `cmd/apogee`, the header constants in `e2e_toolsfold_test.go`, `e2e_toolsfold_subagent_test.go` and the goldens `testdata/frames/t19-tools-umbrella-folded.txt`, `t18-run-view-finished.txt` (regenerate with the repo's golden-update flag).
**Regression guard.** Every caller of `grep -rn 'umbrellaHeaderRow(\|blinkedUmbrellaHeaderRow(' internal/tui` is re-pinned, not only the files named above. The `mouse_test.go` locators `umbrellaHeaderLine` and `umbrellaHeaders` (keyed on `superGroupLabel+" ("`) are re-keyed on text the summary carries (the leading ✦/blank cell plus the fold glyph), kept off the `targetUmbrella` mark they exist to check. Every Go comment that spells the breakdown header or names `umbrellaBreakdown` — find with `grep -rn 'Tools (\|umbrellaBreakdown' --include=*.go internal cmd apogee.go` — now describes the Group summary.
**Files:** internal/tui/toolblock.go; internal/tui/render_test.go; internal/tui/toolblock_test.go; internal/tui/mouse_test.go; internal/tui/blocktarget_test.go; internal/tui/toolbranch_test.go; internal/tui/toolpresent_test.go; internal/tui/toolshape_test.go; internal/tui/transcriptbridge_test.go; internal/tui/transcript.go; internal/tui/toolregistry.go; internal/domain/uiprefs.go; cmd/apogee/e2e_toolsfold_test.go; cmd/apogee/e2e_toolsfold_subagent_test.go; cmd/apogee/testdata/frames/t19-tools-umbrella-folded.txt; cmd/apogee/testdata/frames/t18-run-view-finished.txt
**Read first:** internal/tui/toolblock.go — umbrellaHeader, umbrellaHeaderText, umbrellaHeaderPaint, renderSuperGroup, superRunViews; internal/tui/toolleader.go — leaderRowIn, leaderTargetBudget; internal/tui/render_test.go — umbrellaHeaderRow, blinkedUmbrellaHeaderRow; internal/tui/mouse_test.go — umbrellaHeaderLine, umbrellaHeaders;
internal/tui/toolblock_test.go — TestUmbrellaHeaderBreaksDownByToolKind; cmd/apogee/e2e_toolsfold_test.go — toolsFoldHeader, toolsFoldLiveHeader; cmd/apogee/e2e_subagent_view_test.go — tuitest.Golden "t18-run-view-finished"; internal/tui/render.go — resolveBlock super-group arm
**Tests:** existing header tests re-pinned to the summary; one narrow-width render test showing the fallback on a real umbrella.
**Acceptance:**
- `go build ./... && go vet ./internal/tui/ ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'Umbrella|SuperGroup|ToolBlock|GroupSummary' ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestLiveBlockHeaderStarBlinks|TestRenderMarksTheWholeBlock|TestAnsweredAskUserBlocksNeverGroup|TestRenderGroup|TestRenderSingleCallSharesTheGroupShape|TestRunTestsGroupsAndCounts|TestSkillGroupSitsBetweenTwoToolUmbrellas|TestUnregisteredCallsGroupAndCount|TestTranscriptCodecReDerivesAnsweredQuestionSolo|TestTranscriptLayoutGolden' ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestE2EToolsUmbrella|TestE2EToolsFold|TestE2ESubAgentView' ./cmd/apogee/`
**Commit:** `feat(tui): show the Group summary on every tools umbrella header`

## 6. Derive settled and touched umbrellas and fold settled ones — ✅ DONE (2026-11-18)

NOTES (2026-11-18): the one fold-resolution helper is `transcript.umbrellaFoldBy`, returning an `umbrellaFoldRule` (foldByNone / foldBySettled / foldByShared / foldByHead) so item 7's click and panel paths can branch on which rule holds the fold; `umbrellaFolded` switches on it. Settledness is `transcript.umbrellaSettled` (all calls done, and the first later entry at the run's depth or shallower is in the umbrella's own run; else top run: !turnLive, delegated run: its head done/finished via runHeadAt, falling back to !turnLive with no head).
NOTES (2026-11-18): paintcache.go needed no code change — `paintKey.folded` already carries the resolved answer from resolveBlock, so a settle turns the key over (pinned by TestAutoCollapseSettleRepaints); only its comments were swept, as were render.go, toolblock.go and blocktarget.go.
NOTES (2026-11-18): `transcript.reset` clears `turnLive` (not preserved like toolsOpen): clearWatchView builds the watch's fresh view from the watcher's transcript with state stateIdle, so a preserved flag from a running own Turn would hold the watched session's last umbrella open; /clear runs at idle. `toolsAutoCollapse` is preserved across reset like toolsOpen.
NOTES (2026-11-18): no setters for `entry.umbrellaTouched` / `entry.umbrellaOwnFold` were added — writing them is item 7's; the touched tests set the fields directly.
NOTES (2026-11-18): consequential edit — internal/tui/transcript_test.go: made necessary by the two new transcript setters (setToolsAutoCollapse, setTurnLive), added to TestTranscriptWritersBumpTheGeneration's writer enumeration.

**What:**
**Goal:** With the transcript's auto-collapse flag on, an umbrella whose calls are all done and that is followed by a later entry in its run (or whose Turn — in a run view, whose child — is no longer running) and that the reader has not touched renders folded, whatever its size and `ui.tools-open`; a live umbrella, the last entry of a running Turn, or a touched one folds as today. With the flag off, folding is exactly today's. The flag defaults off; nothing in the binary sets it yet.
**Approach (assumed at the header base):** In `internal/tui/transcript.go`: `toolsAutoCollapse` field + `setToolsAutoCollapse`, alongside `toolsOpen`; a transcript-level turn-live flag the model sets where a Turn starts and in `finishWorker` (`internal/tui/model.go`); `entry.umbrellaTouched` and `entry.umbrellaOwnFold` on the head entry, view state like `umbrellaFolded` (never in `toWireEntry`). Settledness is derived on paint like membership (`toolSuperGroup`): no undone call (`anyOpenCall`) and a later entry after the umbrella's last call in the same run/depth, or turn not live. `umbrellaFolded(head)` order: auto-collapse ∧ settled ∧ ¬touched → folded; touched ∧ ownFold → the head's `umbrellaFolded`; else today's large/small rule. `umbrellaFold` in the paint key (`paintcache.go`, `resolveBlock` in `render.go`) carries the resolved fold so a settle repaints. Resume and watch replay need nothing extra: replayed umbrellas are settled and untouched.
**Regression guard.** The fold resolution order (auto-collapse ∧ settled ∧ ¬touched → folded; touched ∧ ownFold → head flag; else today's size rule) lives in one transcript helper that both paint and item 7's interaction paths call. The turn-live flag is set in `markRunning` (the one writer of `stateRunning`) and cleared in `finishWorker` AND `watchIdle` (a watched Turn never reaches `finishWorker`). Each new setter bumps the generation via `t.touch()` as `setToolsOpen` does, and the turn-live field joins the generation comment's field list. Every `internal/tui` comment stating an umbrella's fold is decided by its size / `toolsOpen` / head flag alone names the settled rule first — `grep -n 'by its size\|umbrella.s size\|size alone\|umbrellaFolded' internal/tui/*.go` (`doc.go` is item 10's). This reverses `adr-0035-addendum-0918` ("not whether the Turn is running, not whether every call has returned"), superseded by item 10's `adr-0035-amend-1118`.
**Files:** internal/tui/transcript.go; internal/tui/model.go; internal/tui/watch.go; internal/tui/render.go; internal/tui/paintcache.go; internal/tui/toolblock.go; internal/tui/blocktarget.go; internal/tui/autocollapse_test.go
**Read first:** internal/tui/transcript.go — umbrellaFolded, umbrellaIsLarge, setUmbrellaFolded, toolSuperGroup, touch; internal/tui/render.go — resolveBlock; internal/tui/paintcache.go — blockKey, umbrellaFold, paintKey; internal/tui/blockstate.go — anyOpenCall; internal/tui/subagentblock.go — subAgentReported;
internal/tui/model.go — markRunning, enterRunning, finishWorker; internal/tui/watch.go — watchRunning, watchIdle, foldWatchItem; internal/tui/transcript_test.go — TestUmbrellaFoldedFollowsSizeThenFlag, TestUmbrellaIsLargeBySizeAlone
**Tests:** unit tests with the flag set: settles on a following narration, a note, a user message, and on turn end; a live one and a last-entry-of-running-turn one stay open; a large one folds despite `toolsOpen`; touched exempt; flag off unchanged; replay folds; a run view umbrella folds once its child reports; while watching, the running Turn's last umbrella stays open and folds once the watched Turn goes idle.
**Acceptance:**
- `go build ./... && go vet ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestAutoCollapse|Umbrella' ./internal/tui/`
**Commit:** `feat(tui): fold settled, untouched tool umbrellas`

## 7. Mark umbrellas touched from every reader action

**What:** Depends on item 6.
**Goal:** Any reader action inside or on an umbrella — opening a type row, a member, a member's body, or folding/unfolding its header, by mouse or keyboard — marks that umbrella touched; opening an auto-collapsed umbrella opens it alone without writing `ui.tools-open`; a side-panel jump to a member of a folded umbrella unfolds it, marks it touched and reveals the member.
**Approach (assumed at the header base):** The seam is `Model.toggleBlockAt` (`internal/tui/mouse.go`), shared by mouse release and `toggleAtBlockCursor`. Add a transcript helper mapping any entry index to its umbrella head (walk back via `toolSuperGroup`), used for `targetUmbrella`, `targetType` and `targetHeader` hits. On `targetUmbrella`: if the umbrella is auto-collapsed (settled ∧ untouched ∧ flag on), set touched + ownFold + unfolded; else large → today's `toggleToolsFold` (`settingsapply.go`) and touched; small → today's `setUmbrellaFolded` and touched. `panelTargetBlock` (`internal/tui/sidepanel.go`) unfolds and touches the member's umbrella before `revealEntry`.
**Regression guard.** The click and side-panel paths branch on item 6's single resolution helper, never re-deriving the order, so the head flag and toolsOpen never disagree about who owns a large umbrella's fold. On `targetUmbrella`, a head with touched∧ownFold flips its own flag via `setUmbrellaFolded` (no write); only touched-without-ownFold large heads keep `toggleToolsFold` + save. The panel jump unfolds through touched + ownFold + `setUmbrellaFolded(head, false)`, never `toggleToolsFold` (`followPanelTarget` forbids a writing fold), and also opens the member's type row (`setTypeExpanded` on its run head) before `revealEntry`, since a closed type row paints no member line.
**Files:** internal/tui/mouse.go; internal/tui/blocktarget.go; internal/tui/transcript.go; internal/tui/settingsapply.go; internal/tui/sidepanel.go; internal/tui/autocollapse_touch_test.go
**Read first:** internal/tui/mouse.go — toggleBlockAt, blockTargetAt; internal/tui/settingsapply.go — toggleToolsFold, Model.setToolsOpen; internal/tui/sidepanel.go — followPanelTarget, revealEntry; internal/tui/toolblock.go — renderSuperGroup; internal/tui/blockcursor.go — toggleAtBlockCursor;
internal/tui/mouse_test.go — TestLargeUmbrellasShareOneFold, modelWithLargeUmbrellas; internal/tui/settings_test.go — fakeSettingsHost, settingsWriteLog
**Tests:** each target kind marks touched (mouse and ⏎); opening an auto-collapsed large umbrella leaves `ui.tools-open` unwritten (settings spy) and other settled ones folded; a second click on that opened umbrella folds it alone and still writes nothing; a live large click still toggles and writes; panel jump reveals a member of a folded umbrella — the member second in a multi-call run — with no settings write.
**Acceptance:**
- `go build ./... && go vet ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestAutoCollapse|Umbrella|SidePanelTarget' ./internal/tui/`
**Commit:** `feat(tui): exempt tool umbrellas the reader has touched from auto-collapse`

## 8. Turn auto-collapse on from the setting

**What:** Depends on items 3, 7.
**Goal:** The TUI's auto-collapse flag follows `ui.tools-auto-collapse` at start-up, on a `/settings` edit and on a hand-edit reload; existing driven tests that assert a settled umbrella starts open pass, either pinned to `tools-auto-collapse: false` or re-expected.
**Approach (assumed at the header base):** Seed `transcript.toolsAutoCollapse` in `newModel` next to `toolsOpen`; add the key to `settingsApplyLocal` (`internal/tui/settingsapply.go`). Existing e2e tests whose subject is the size rule or `ui.tools-open` (`TestE2EToolsFoldOverZeroNeverFolds`, `…SmallFoldsAlone`, `…FoldIsRememberedAcrossAResume`, the run-view fold tests, t18) set `tools-auto-collapse: false` in their config; regenerate a golden only where its subject is the collapse.
**Regression guard.** Item 3 already lands the `settingsApplyLocal` arm (`m.opts.UI.Set` only), the test's value pair and the `settingKeysAppliedByTheRenderer` entry; item 8 only adds `transcript.setToolsAutoCollapse` to that existing arm, so no new arm and no `cmd/apogee/wire_settings_test.go` edit here.
**Files:** internal/tui/model.go; internal/tui/settingsapply.go; internal/tui/settingsapply_test.go; cmd/apogee/e2e_toolsfold_test.go; cmd/apogee/e2e_toolsfold_subagent_test.go; cmd/apogee/e2e_subagent_view_test.go
**Read first:** internal/tui/settingsapply.go — Model.settingsApplyLocal (UIKeyToolsOpen arm); internal/tui/model.go — newModel (toolsOpen/toolsFoldOver seed); internal/tui/settingsapply_test.go — TestSettingsApplyLocalLandsEveryUIKeyThroughSet; cmd/apogee/wire_settings_test.go — settingKeysAppliedByTheRenderer;
cmd/apogee/e2e_toolsfold_test.go — toolsFoldOverTwoConfig, TestE2EToolsUmbrellaSmallFoldsAlone; cmd/apogee/e2e_subagent_view_test.go — runViewHome, TestE2ESubAgentView; internal/tui/model_test.go — testUIPrefs
**Tests:** a unit test that the live apply flips the flag and repaints.
**Acceptance:**
- `go build ./... && go vet ./internal/tui/ ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'SettingsApply' ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestE2EToolsFold|TestE2EToolsUmbrella|TestE2ESubAgentView' ./cmd/apogee/`
**Commit:** `feat(tui): apply ui.tools-auto-collapse live`

## 9. Drive auto-collapse end to end

**What:** Depends on item 8.
**Goal:** Driven tests through `internal/tuitest` + `internal/stubllm` pin, in the real binary with the default config: a settled umbrella collapses to its Group summary row; one the reader opened something in stays open after the next reply; a resumed session shows its settled umbrellas collapsed; a failed call shows `· 1 failed` on the collapsed header; `tools-auto-collapse: false` keeps today's fold.
**Approach (assumed at the header base):** New `cmd/apogee/e2e_toolsautocollapse_test.go` + stub script `cmd/apogee/testdata/stubllm/toolsautocollapse.yaml`, modelled on `e2e_toolsfold_test.go` (`launchTUIConfigured`, `submit`, `click`, `umbrellaHeaderMatch`, `sess.RelaunchWith("--continue")`). Waits on conditions (`WaitFor`/`WaitText`), never `WaitQuiet` for readiness.
**Regression guard.** The Goal's last clause binds as behaviour, not "today's": under `tools-auto-collapse: false` a settled small umbrella stands open (`▼`) and a large one follows `ui.tools-open`, the size rule by `ui.tools-fold-over`.
**Files:** cmd/apogee/e2e_toolsautocollapse_test.go; cmd/apogee/testdata/stubllm/toolsautocollapse.yaml
**Read first:** cmd/apogee/e2e_toolsfold_test.go — umbrellaHeaderMatch, toolsFoldFrame, TestE2EToolsUmbrellaFoldIsRememberedAcrossAResume; cmd/apogee/testdata/stubllm/toolsfold.yaml — when/repeat turns, title turn; cmd/apogee/e2e_support_test.go — launchTUIConfigured, e2eSession.RelaunchWith, e2eWorkspace (seeds a.txt only);
internal/stubllm/log.go — lastToolResultName (`tool_result` matches the LAST message only); internal/tui/transcript.go — toolSuperGroup (two-call floor); cmd/apogee/e2e_popups_test.go — click; cmd/apogee/e2e_smoke_test.go — submit
**Tests:** the five scenarios above.
**Acceptance:**
- `go vet ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestE2EToolsAutoCollapse' ./cmd/apogee/`
**Commit:** `test(e2e): drive settled tool umbrella auto-collapse`

## 10. Document the Group summary and the auto-collapse rule

**What:** Depends on item 9.
**Goal:** `docs/layout/tool-layout.md` specifies the Group summary header (phrases, distinct counts, diffstat, failed/denied, live verb, width fallback) and the settled/touched auto-collapse rule; `docs/manual/configuration.md` explains `ui.tools-auto-collapse`; `adr-0035-amend-1118` records that auto-collapse sits above the size rule for settled umbrellas (partly superseding `adr-0035-addendum-0918`'s "size alone") and the three owner calls, named on ADR 0035's Status line and reading list; a **Group summary** glossary Note exists under `glossary-context` beside `tool-summary`.
**Approach (assumed at the header base):** Rule for prose: every doc or code comment that states an umbrella's header reads `Tools (N Kind, …)` or that fold is decided by size alone — find with `grep -rn -E 'Tools \(|tools-fold-over|size alone' docs layout.md internal/tui/doc.go internal/domain/doc.go .capcom/knowledge/glossary` — now describes the Group summary and the settled rule. Historical text (CHANGELOG, ADR Decision bodies, archived plans) stays.
**Regression guard.** The sweep also covers the ui.tools-open / ui.tools-fold-over registry Desc strings in internal/config/registry.go and the starter-template comments in internal/config/defaults/config.yaml (add both to Files); the rule's grep adds those two files and the term `tools-open`. The `docs/layout/side-panel-layout.md` mock-up's open settled `✦ Tools (1 Read, 1 Edit) ⋯ ▼` is redrawn too. The amendment also names `adr-0035-addendum-0917` as partly superseded (its click folding every large umbrella and writing `ui.tools-open`): for an auto-collapsed umbrella, the click opens that one alone and writes nothing.
**Files:** docs/layout/tool-layout.md; docs/manual/configuration.md; docs/manual/sessions.md; docs/layout/side-panel-layout.md; layout.md; internal/tui/doc.go; internal/config/registry.go; internal/config/defaults/config.yaml; .capcom/knowledge/decisions/adr-0035.md; .capcom/knowledge/decisions/adr-0035/adr-0035-amend-1118.md; .capcom/knowledge/glossary/glossary-context/group-summary.md
**Read first:** docs/layout/tool-layout.md — umbrella header paragraph (Vocabulary), §Fold states and interaction, umbrella mock-ups; .capcom/knowledge/decisions/adr-0035.md — Status line, reading-order list; .capcom/knowledge/decisions/adr-0035/adr-0035-addendum-0917.md, adr-0035-addendum-0918.md; docs/manual/configuration.md — `ui.tools-open`/`ui.tools-fold-over` paragraph;
docs/manual/sessions.md — resume fold-exceptions paragraph; layout.md — Tools umbrella fold paragraph; internal/tui/doc.go — toolSuperGroup and umbrella-fold paragraphs; internal/config/registry.go — ui.tools-open and ui.tools-fold-over Desc
**Tests:** none new; docs drift tests.
**Acceptance:**
- `capcom check`
- `capcom know show "Group summary"` succeeds; `capcom know show "ADR 0035" | grep -q adr-0035-amend-1118`
- `grep -q 'tools-auto-collapse' docs/layout/tool-layout.md docs/manual/configuration.md`
- `grep -qi 'group summary' docs/layout/tool-layout.md layout.md`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestManual|TestDocs' ./cmd/apogee/`
**Closes:** settled-tool-group-clutter
**Commit:** `docs(tools): specify the Group summary and settled umbrella auto-collapse`
