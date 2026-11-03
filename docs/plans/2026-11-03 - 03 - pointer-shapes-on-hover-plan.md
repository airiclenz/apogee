# Pointer shapes on hover — OSC 22, probe-gated, default off

**Goal:** while the mouse hovers, the terminal pointer shows what a click there does — `text` over the editable prompt, settings text fields and transcript lines, `pointer` over every clickable row and toggleable block, `default` elsewhere — via OSC 22. It runs only when `ui.pointer-shapes` is on AND the terminal answers an in-session capability query; otherwise apogee writes no OSC 22 byte and keeps cell-motion tracking.
**Date:** 2026-11-03
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** c8feea33
**Closes:** pointer-shapes-on-hover

**Sources:**
- `.capcom/issues/pointer-shapes-on-hover.md` (design sketch of 2026-11-02)
- kitty pointer-shapes spec, https://sw.kovidgoyal.net/kitty/pointer-shapes/ — set `OSC 22 ; <name> ST`, reset `OSC 22 ; ST` (empty name), query `OSC 22 ; ?a,b,c ST` answered `OSC 22 ; 1,0,1 ST` (1 = supported, positional), separate stacks per main/alternate screen
- `layout.md` — mouse sections ("What \"height\" means…", "Collapsed and expanded blocks")
- ADR 0011 (value-copied Model), ADR 0025 (`inputEditable`), ADR 0040 (OSC 11 auto-detection rejected), ADR 0031 (Driver-sufficient engine)

**Ratified design calls:**
- **Transcript body:** `text` (I-beam) over transcript lines that are not toggle targets — they are drag-selectable (owner, 2026-11-03).
- **tmux:** query and shapes are written raw, never wrapped in `ansi.TmuxPassthrough`; under tmux the query goes unanswered and the feature stays off (owner, 2026-11-03).
- **ADR:** the gating is recorded as a new ADR, item 1 (owner, 2026-11-03).
- **Config:** `ui.pointer-shapes`, bool, default `false`, editable in /settings (issue sketch; owner, 2026-11-03).
- **Gate:** supported only when the query `?default,text,pointer` is answered `1,1,1`; a DA1 reply arriving first means unsupported; no timer backstop (slow-box rule: a silent terminal just keeps the feature off) (writer, 2026-11-03).
- **Query only when on:** with the setting off apogee sends no query; turning it on in /settings sends the query then; one answer per session is kept (writer, 2026-11-03).
- **Reset on release:** apogee writes the reset (`OSC 22 ; ST`) on every terminal release — exit (both screen stacks) and the `settingsExternalEdit` handoff — and when the setting turns off; no suspend path exists at base (writer, 2026-11-03; suspend dropped at the regression check, 2026-11-03).

**Standing requirements:**
- skills: coding-standards
- This box (Raspberry Pi, OOM-prone): run tests only as `GOMEMLIMIT=2GiB go test -race -count=1 -run '<TestX>' ./internal/tui/` (one package, one test pattern); never a package pattern, never two test runs at once. Pass this on to every sub-agent.
- Bubble Tea is v2 (`charm.land/bubbletea/v2`): `tea.MouseMotionMsg`, `tea.Raw`, `tea.MouseModeAllMotion`; ultraviolet events (`uv.UnknownOscEvent`, `uv.PrimaryDeviceAttributesEvent`) reach `Update` raw.
- Any authorized deviation from item text lands as a dated NOTES line under the item.

**Out of scope:**
- X11 cursor names, shapes beyond `default|text|pointer`, tmux passthrough, any model-visible hover information.
- The text caret (`cursor-shape`, DECSCUSR) — unchanged.

**Regression check (2026-11-03, c8feea33):**
- 1: guard folded
- 2: guard folded
- 3: guard folded (writer's decision plus the report's test guards)
- 4: recast (writer's decision); yields to `handleMouseClick`'s outside-click rule
- 5: guard folded (writer's decision plus the report's guards)
- 6: guard folded (writer's decision plus the report's guards)
- 7: recast (writer's decision)
- 8: guard folded
- 4 (round 2): guard folded (Goal's outside-box rule rewritten; `onRow` struck from Approach; settings `kind == settingsKeyList` gate; click tests added to Acceptance)
- 7 (round 2): recast (writer's decision: Bubble Tea v2.0.8 flushes queued `tea.Raw` on release and shutdown, premise corrected; exit reset on both screen stacks; editor-handoff reset kept; suspend dropped) plus the report's guards (written flag, `reassertCount` prefix match, `TestClaimTerminalScreen` in Acceptance)

## 1. ADR 0106 — probe-gated OSC 22 pointer shapes — ✅ DONE (2026-11-03)

**What:**
**Goal:** the `decisions` tree holds `adr-0106` with context, decision, rejected and consequences children recording this plan's ratified calls.
**Approach (assumed at the header base):** `capcom know add --id adr-0106 --parent decisions` plus child Notes `adr-0106-context`, `adr-0106-decision`, `adr-0106-rejected`, `adr-0106-consequences`, shaped like `adr-0105` (Status line, reading-order list). Decision states, numbered: (1) three CSS shape names `default|text|pointer`, mapped per region as in the plan Goal; (2) gate = setting on AND query `?default,text,pointer` answered `1,1,1` before the DA1 sentinel's reply; all-motion tracking (`?1003h`) only while gated on; (3) query sent only while the setting is on, once per session; (4) raw writes, no tmux passthrough; (5) hover is a side effect — motion never repaints, a shape is written only on change; (6) the shape is re-asserted beside mouse tracking and reset on every terminal release; (7) presentational only — no Event, no model-visible change, ADR 0031/0071/0076 untouched. Rejected: tmux passthrough (shape leaks across panes); always-on all-motion; a timer backstop (slow-box rule); probing while off; X11 names. Context cites ADR 0040: unlike OSC 11 auto-detection, a silent terminal here costs nothing — the opt-in just stays off.
**Regression guard.** The `decisions` index Note lists every record by hand (`.capcom/knowledge/decisions.md`, ending at `adr-0105`; ADR 0105's commit 378f2eee added its own line): add ``- `adr-0106` — <title>`` after the `adr-0105` line.
**Files:** .capcom/knowledge/decisions/adr-0106.md, .capcom/knowledge/decisions/adr-0106/, .capcom/knowledge/decisions.md
**Read first:** .capcom/knowledge/decisions/adr-0105.md — Status line, reading-order list; .capcom/knowledge/decisions/adr-0105/adr-0105-context.md — context shape; .capcom/knowledge/decisions.md — record index list; .capcom/knowledge/decisions/adr-0040/adr-0040-rejected.md — OSC 11 auto-detection rejection
**Tests:** none (docs only).
**Acceptance:**
- `capcom know show "ADR 0106" --with-children` prints the record and four children.
- `grep -n 'adr-0106' .capcom/knowledge/decisions.md` prints the index line.
- `capcom check` reports no problems.
**Commit:** `docs(adr): record ADR 0106, probe-gated OSC 22 pointer shapes`

## 2. `ui.pointer-shapes` setting — ✅ DONE (2026-11-03)

NOTES (2026-11-03): consequential edit — internal/config/config_test.go: made necessary by the new registry row — TestRegistrySetIsTheInverseOfRead refuses a key `everyKeyFileConfig` leaves at its default, so the fixture gains `PointerShapes: boolptr(true)`; the plan's "minus config_test.go" held only for the default-true `wantUIDefault` literals, which a default-false key leaves untouched.
NOTES (2026-11-03): placed `ui.pointer-shapes` after `ui.tools-fold-over` (the last visual/mouse key) and before `ui.notify` in `uiKeys`, the starter template and `KeyRegistry` alike; the manual paragraph sits after the Tools-umbrella paragraph.

**What:**
**Goal:** `ui.pointer-shapes` is a registry key (bool, default false, editable), read from config into `UIPrefs`, listed in the starter template and the manual, shown in /settings, and landed live through `settingsApplyLocal`; nothing in the TUI acts on it yet.
**Approach (assumed at the header base):** follow `ui.show-scrollbar`: `domain.UIKeyPointerShapes` + `uiKeys` order + `UIPrefs.PointerShapes` + `Set` case (`landBool`) in `internal/domain/uiprefs.go`; `uiConfig` `*bool` field in `internal/config/config.go`; `KeyRegistry` row (`KindBool`, `uiBool(...)`) in `internal/config/registry.go`; the `ui:` block and its prose in `internal/config/defaults/config.yaml`; an entry in docs/manual/configuration.md "The terminal UI — `ui:`" stating: off by default, needs a terminal that answers OSC 22 queries (kitty, Ghostty, WezTerm, foot), stays off under tmux, purely visual. Place the key after the other mouse/visual keys; template and registry order must agree.
**Regression guard.** Follow commit ffd35982 (`ui.git-fetch`) file for file, minus `config_test.go` (a default-true key's only): append `"ui.pointer-shapes"` to `settingKeysAppliedByTheRenderer` (cmd/apogee/wire_settings_test.go); set `PointerShapes: true` in `fabricatedSettings` with a `"ui.pointer-shapes": "true"` want (cmd/apogee/settingsrows_test.go); add `"ui.pointer-shapes": "UI"` to the owns table and documented `"true"` (off the default) in internal/config/registry_test.go; add `{settingTrue, "maybe"}` for the key in internal/tui/settingsapply_test.go. configuration.md's "Twelve more keys … Eight of them" becomes thirteen / nine; internal/domain/doc.go's "its thirteen keys" becomes "fourteen keys". Never edit internal/config/testdata/settings-edited.yaml: every `settings-edited.*.golden` splices it whole, and the round-trip test inserts a missing key itself.
**Files:** internal/domain/uiprefs.go, internal/domain/uiprefs_test.go, internal/domain/doc.go, internal/config/config.go, internal/config/registry.go, internal/config/registry_test.go, internal/config/defaults/config.yaml, internal/tui/settingsapply.go, internal/tui/settingsapply_test.go, cmd/apogee/wire_settings_test.go, cmd/apogee/settingsrows_test.go, docs/manual/configuration.md
**Read first:** internal/domain/uiprefs.go — uiKeys, UIPrefs.land, landBool, DefaultUIPrefs; internal/config/registry.go — KeyRegistry ui.git-fetch row, uiBool; internal/tui/settingsapply.go — Model.settingsApplyLocal (UIKeyGitFetch case); cmd/apogee/wire_settings_test.go — settingKeysAppliedByTheRenderer, TestEveryEditableSettingKeyHasAnApply;
cmd/apogee/settingsrows_test.go — fabricatedSettings, TestSettingsRowsFormatEffectiveValues; internal/config/registry_test.go — TestRegistrySetIsTheInverseOfRead, TestUIRowsLandThroughUIPrefsSet; internal/tui/settingsapply_test.go — TestSettingsApplyLocalLandsEveryUIKeyThroughSet; internal/domain/uiprefs_test.go — TestUIKeysAreTheTemplateOrderAndEveryKeyHasACase, uiKeyCases
**Tests:** extend `TestUIKeysAreTheTemplateOrderAndEveryKeyHasACase` (`uiKeyCases`) and the four test tables the guard names; the registry/template drift tests must pass unchanged.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestUIKeysAreTheTemplateOrderAndEveryKeyHasACase' ./internal/domain/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestRegistrySetIsTheInverseOfRead|TestUIRowsLandThroughUIPrefsSet|TestRegistryFollowsTheTemplateOrder|TestTemplateMentionsEveryRegistryKey|TestRegistryDefaultsFollowTheStarterTemplate|TestSpliceScalarSettingRoundTripsEveryEditableKey' ./internal/config/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestSettingsApplyLocalLandsEveryUIKeyThroughSet' ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestManualDocumentsEverySettingsKey|TestSettingsRowsMatchTheRegistryOrder|TestEveryEditableSettingKeyHasAnApply|TestSettingsRowsFormatEffectiveValues' ./cmd/apogee/`
**Commit:** `feat(config): add the ui.pointer-shapes setting, default off`

## 3. Pointer-shape classifier — prompt, footer, transcript

**What:**
**Goal:** a pure method answers, for a frame point, which of `default|text|pointer` a click there implies, for the regions outside the panes; it has no side effects and is unit-tested per region.
**Approach (assumed at the header base):** new `internal/tui/pointershape.go`: type `pointerShape` (`shapeDefault`, `shapeText`, `shapePointer`) with `name()` returning the CSS name; `func (m Model) pointerShapeAt(pre Model, x, y int) pointerShape` where `pre = m.withFrameSpans()`. It walks, in the click chain's order: a `hover func(pre Model, x, y int) (pointerShape, bool)` field added to `paneSpec` (panes.go) over `pointerPanes`, skipping nil (item 4 fills them); the footer mode marker → pointer, through a pure `footerModeHit(pre, x, y) bool` extracted from `handleFooterModeClick` and called by both (one predicate, two callers — never restated); `inputEditable() && pre.pointInputRow(...)` → text; transcript: `contentLineAt(y)` with `lineTargets[line].kind != targetNone` → pointer, any other content line → text, rows past the content → default. Binding: no click-handler logic is restated; geometry comes only from the helpers the click chain already uses.
**Regression guard.** geometry alone never decides a shape — every region the classifier answers goes through a pure hit predicate extracted from the matching click handler (geometry AND its state gates: footer entitlement, renaming/confirming, typed input, field kind), called by both the click handler and the classifier; never restated. The footer predicate therefore carries `handleFooterModeClick`'s entitlement gate (`state.live() && !picker.open && !sessionBrowser.open && !settingsOwnsInput()`) beside the marker span.
**Files:** internal/tui/pointershape.go, internal/tui/pointershape_test.go, internal/tui/panes.go, internal/tui/mouse.go
**Read first:** internal/tui/mouse.go — handleMouseClick, handleFooterModeClick, footerRowY, pointInputRow, inputEditable, pointTranscriptRow, toggleBlockAt; internal/tui/model.go — contentLineAt, withFrameSpans, footerModeSpan; internal/tui/blocktarget.go — targetKind, lineTarget; internal/tui/panes.go — paneSpec, paneSpecs init, pointerPanes;
internal/tui/mouse_test.go — footerMarkerCells, modelWithToolBlock, markedLine, screenRow, modelWithTranscript, TestClickOnTheFooterModeMarkerIsRefusedWhereThePickerCannotBeAnswered
**Tests:** `TestPointerShapeAt*` table: prompt content rect → text; prompt while not editable → not text; footer mode marker → pointer; footer mode marker while awaiting approval (or with /settings or /sessions up) → not pointer; a tool-block header line → pointer; plain transcript line → text; blank row below the transcript → default. Existing footer-click tests still pass.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestPointerShapeAt|TestFooterMode|TestClickOnTheFooterModeMarker' ./internal/tui/`
- `go vet ./internal/tui/`
**Commit:** `feat(tui): classify a frame point into a pointer shape`

## 4. Pointer-shape answers for the panes

**What:**
Recast at the regression check (2026-11-03).
Depends on item 3.
**Goal:** every pane in `pointerPanes` answers hover: `pointer` on a row its click would act on, `text` on the settings pane's editable value field, `default` elsewhere inside its box; a pane whose click dismisses and claims outside its box (browser, picker, workflows, and browser/workflows while renaming/confirming/naming) answers default there; every other pane leaves outside points to the chain.
**Approach (assumed at the header base):** fill the `hover` field of each `paneSpecs` row (settings, the four reports, /sessions browser, picker, workflows, ask/approval prompt, dropdown). Every answer lives in `pointershape.go` and calls `popupPaneHit` with the SAME render func the pane's click handler passes (`settingsPaint.render`/`rowAt`, `renderSessionBrowserPlaced`, `renderPickerPlaced`, `renderWorkflowsPlaced`, `renderAutocompletePlaced`, the ask/approval closures; reports through whatever `handleReportClick` hit-tests with): `ok` → pointer, `inRect && !ok` → default. Where a click handler builds its render inline, extract it to a named func both call. Fast path: skip the pane loop when no pointer pane is open.
**Regression guard.** same rule as item 3 for every pane — each pane's hover calls a pure predicate shared with its click handler (render func plus the handler's state gates), never popupPaneHit alone where the handler gates on state. Outside its box a pane answers `default` only where its handler dismisses-and-claims outside (browser, picker, workflows), never keyed off `paneSpec.modal`; prompt (ask/approval), settings, reports and dropdown leave outside points to the chain — the item yields to `handleMouseClick`'s outside-click rule (mouse.go: a DECISION pane does not claim, the click falls through to footer, prompt and transcript). A report has no row hit: `default` inside `reportPaneRect`. Settings mirrors `handleSettingsClick`'s branch order: value buffer/text editor → `text` only where `settingsFieldCaretAt` is on, `default` on the rest of the box; key list → `pointer` only where `m.settings.kind == settingsKeyList` and `settingKeyAt` is a key (under `settingsResetArmed`/`settingsSaveTarget` the list is still painted but `handleSettingsClick` moves no selection: `default`); `onRow` only decides claim versus chain, never `text`.
**Files:** internal/tui/pointershape.go, internal/tui/pointershape_test.go, internal/tui/panes.go, internal/tui/panes_test.go, internal/tui/reportpane.go, internal/tui/mouse.go, internal/tui/sessions.go, internal/tui/picker.go, internal/tui/workflows.go
**Read first:** internal/tui/mouse.go — handleMouseClick, popupPaneHit, handleSettingsClick, settingsPaint, settingsFieldPaint, settingsFieldCaretAt, handleBrowserClick, handlePickerClick, handleAskClick, handleApprovalClick, handleDropdownClick, promptPointerClick; internal/tui/panes.go — paneSpec, paneSpecs init, reportPaneRow, pointerPanes;
internal/tui/reportpane.go — handleReportClick, reportPaneRect; internal/tui/workflows.go — handleWorkflowsClick, renderWorkflowsPlaced; internal/tui/settings.go — settingsKind, settingsSteps; internal/tui/panes_test.go — TestEveryFramePaneHasASpec;
internal/tui/mouse_test.go — browserPaneModel, pickerPaneModel, askPaneModel, approvalPaneModel, dropdownPaneModel, usageReportModel, inspectorPaneModel, settingsTextEditModel; internal/tui/workflows_test.go — workflowsPaneModel
**Tests:** per pane, open it at 80x24 and assert a row its click acts on → pointer, the box border/blank → default; browser, picker, workflows: a transcript point → default; ask up: a transcript line → text and the editable input row → text; each report: a point inside → default; settings: the field's caret cells → text, another value-buffer key row → default, a key-list section label → default, armed reset (backspace on a row): a key row → default; each state gate (browser renaming/confirming or empty, picker empty, workflows naming/empty, ask with typed input or no choices, approval `editingProjectRule`) → no pointer; `TestEveryFramePaneHasASpec` fails a `pointerPanes` row whose `hover` is nil. Existing click tests still pass.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestPointerShapeAt|TestEveryFramePaneHasASpec|Click|TestSettingsPointerActsOnTheFilteredRows|TestPopupPaneHit' ./internal/tui/`
**Commit:** `feat(tui): answer pointer shapes for every pane`

## 5. Terminal capability probe and the all-motion gate

**What:**
Depends on item 2.
**Goal:** with `ui.pointer-shapes` on, the TUI writes `"\x1b]22;?default,text,pointer\x1b\\"` followed by a DA1 request once per session; an OSC 22 reply of exactly `1,1,1` (ST- or BEL-terminated) marks pointer shapes supported, a DA1 reply first marks them unsupported; `View` sets `tea.MouseModeAllMotion` only while on AND supported, else `tea.MouseModeCellMotion`; the mouse re-assert sequence follows the live mode; the outcome is recorded in the diag log.
**Approach (assumed at the header base):** the query is a `tea.Raw` from `Init` when `m.opts.UI.PointerShapes`, and from `settingsApplyLocal` when the key turns on and no answer is kept; probe state `pointerProbe` (`unasked|pending|supported|unsupported`) on the Model. `fold` gains arms for `uv.UnknownOscEvent` (prefix `"\x1b]22;"`, parsed while pending) and `uv.PrimaryDeviceAttributesEvent` (pending → unsupported); add both to `watchRouteOf`. Verify Bubble Tea v2.0.8 sends no DA1 of its own; if it does, the sentinel counts DA1 replies seen after the query write. `mouseTrackingSeq` becomes a func of the live mode (`?1003h` vs `?1002h`, plus `?1006h`); update `reassertCount`'s matcher. Diag key `pointer-probe` with the outcome (follow `mouse-reassert`'s recording).
**Regression guard.** item 5 owns every code comment in internal/tui stating apogee tracks cell motion only or that View always sets CellMotion (find with grep -rn -i 'cell.motion\|CellMotion\|1002' internal/tui/*.go) and adds the gate caveat. `Init` has a value receiver and cannot set state: `newModel` sets `pointerProbe = pending` when `opts.UI.PointerShapes`, and `Init` only returns the query Raw. Keep the cell-motion `mouseTrackingSeq` const (sessionsave_test.go compares against it) and add the all-motion const beside it, a func of the live mode choosing between them. Under the gate every hovered cell costs an Update plus a full `View()` composition (today only mid-drag): name that cost in ADR 0106 (item 1) and measure it on the Pi; the off path is untouched.
**Files:** internal/tui/pointerprobe.go, internal/tui/pointerprobe_test.go, internal/tui/model.go, internal/tui/mouse.go, internal/tui/mousereassert.go, internal/tui/mousereassert_test.go, internal/tui/watch.go, internal/tui/settingsapply.go
**Read first:** internal/tui/model.go — Model.Init, newModel, Model.fold, Model.View; internal/tui/mousereassert.go — reassertMouse, mouseTrackingSeq; internal/tui/mousereassert_test.go — reassertCount, TestResizeReassertsMouseTrackingVerbatim; internal/tui/watch.go — watchRouteOf;
internal/tui/watch_test.go — TestWatchIsolatesRoutesEveryFoldedMsg; internal/tui/settingsapply.go — settingsApplyLocal; internal/tui/sessionsave_test.go — TestProgressSaveTriggersCoalesceBehindAnInFlightSave; ultraviolet decoder.go — OSC parse (UnknownOscEvent is the full sequence incl. `\x1b]22;` and terminator)
**Tests:** setting off → no query, CellMotion; at startup with the setting on, newModel + Init + `1,1,1` reply → AllMotion; on + `1,1,1` reply → AllMotion; on + `1,0,1` → CellMotion; on + DA1 first → CellMotion, a late OSC 22 reply ignored; toggle on in /settings → query written once; reassert under AllMotion writes `?1003h`; `TestWatchIsolatesRoutesEveryFoldedMsg` passes.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestPointerProbe|Reassert|TestWatchIsolatesRoutesEveryFoldedMsg|TestSettingsApplyLocal' ./internal/tui/`
**Commit:** `feat(tui): probe OSC 22 support and gate all-motion tracking on it`

## 6. Write the pointer shape on hover

**What:**
Depends on items 4 and 5.
**Goal:** while pointer shapes are on and supported, a button-less motion whose classified shape differs from the last written one returns exactly one `tea.Raw(ansi.SetPointerShape(<name>))`; an unchanged shape, a pressed button, or the gate off writes nothing; no motion ever repaints.
**Approach (assumed at the header base):** in `handleMouseMotion`, after the button-less-release branch and before the `Button != tea.MouseLeft` return: when gated on and `!m.mousePressed`, classify with `m.pointerShapeAt(m.withFrameSpans(), x, y)`; on change store `m.pointerShape` and emit. `settleFrame`'s motion skip stays untouched. A drag keeps the shape it started with.
**Regression guard.** item 6 owns every code comment in internal/tui stating button-less motion is ignored (find with grep -rn -i 'button-less\|buttonless\|motion' internal/tui/mouse.go internal/tui/model.go) and states the hover exception. `mousePressed` latches only a LEFT press, so the hover runs only on `msg.Button == tea.MouseNone && !m.mousePressed`: a right or middle drag under all-motion classifies and writes nothing.
**Files:** internal/tui/mouse.go, internal/tui/mouse_test.go, internal/tui/model.go, internal/tui/model_test.go, internal/tui/pointershape.go
**Read first:** internal/tui/mouse.go — handleMouseMotion, handleMouseClick, trackDragEdge; internal/tui/model.go — withFrameSpans, frameSpans, settleFrame, Model.mousePressed; internal/tui/model_test.go — TestMouseMotionNeverRepaints, expandBatch, sameLines; github.com/charmbracelet/x/ansi cursor.go — SetPointerShape (BEL-terminated)
**Tests:** gated on: moving prompt → transcript header → transcript line emits text, pointer, text once each; 50 motions inside one region emit once; a right-button drag emits nothing; gate off → zero OSC 22 writes. Extend `TestMouseMotionNeverRepaints` with 1000 button-less motions under the gate on (same `m.lines` identity and viewport height).
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestMouseMotionNeverRepaints|TestHoverPointerShape' ./internal/tui/`
**Commit:** `feat(tui): show the pointer shape a click implies on hover`

## 7. Re-assert and reset the pointer shape

**What:**
Recast at the regression check (2026-11-03).
Depends on item 6.
**Goal:** while gated on, every mouse re-assert (resize, tool result) also re-writes the current shape; the reset `ansi.SetPointerShape("")` is written on program exit (both screen stacks), before the `settingsExternalEdit` handoff (both screen stacks), and when `ui.pointer-shapes` turns off in /settings (which also returns tracking to CellMotion); with the gate off none of these writes an OSC 22 byte.
**Approach (assumed at the header base):** `reassertMouse` batches the shape write with the tracking sequence. Exit: write the both-stacks reset in `claimTerminalScreen`'s release (tui.go), only when a shape was ever written. Editor handoff: sequence the both-stacks reset Raw before `settingsExternalEdit`'s `tea.ExecProcess`; on return the next motion re-writes the shape (clear `m.pointerShape`).
**Regression guard.** Bubble Tea v2.0.8 DOES flush queued `tea.Raw` bytes on release and shutdown (`restoreTerminalState` → `p.flush`, tty.go), after `renderer.close` has left the alternate screen; never state Raw is flushed only on the renderer tick. The exit release writes the reset on both screen stacks directly in `claimTerminalScreen`'s release: `SetPointerShape("")` + `ansiEnterAltScreen` + `SetPointerShape("")` + `ansiExitAltScreen` (constant written directly, never via `claimAltScreen`: `TestClaimTerminalScreenPreparesTheConsoleBeforeTheSwitch` requires exactly one `claimAltScreen` call in the body, and `claimAltScreen` also writes `\x1b[3J`, erasing the user's scrollback). The `settingsExternalEdit` handoff keeps its reset (the ratified "reset on every terminal release" call stands): a `tea.Raw` sequenced before the `ExecProcess` carrying the same both-stacks sequence, so whichever screen is active when the flush lands both stacks end at default; pinned by a test that the reset Raw is sequenced before the exec. No suspend path exists at base (apogee never returns `tea.Suspend`): none is added. The item owns every comment in internal/tui/tui.go that lists what the release writes (grep -n 'on its own behalf\|release' internal/tui/tui.go) — it supersedes tui.go's "The three screen-control sequences apogee sends on its own behalf" and `claimTerminalScreen`'s release doc, which a direct OSC 22 write makes untrue. `Run` reads `program.Run()`'s final Model and passes the "shape was written" flag to the release; list conpty_windows_test.go if `claimTerminalScreen`'s signature moves. The editor handoff with the gate off returns the bare exec, unchanged. The re-assert under the gate is ONE `tea.Raw(trackingSeq + shape)`, and the single tracking Raw with it off (`TestRunWritesLandsARecordWriteBatchedWithTheReassert` pins `len(batch) == 2`); append the shape only once one has been written (a written flag, not the zero `shapeDefault`), and change `reassertCount` (exact `raw.Msg == mouseTrackingSeq` today) to a `strings.HasPrefix` match on the live-mode tracking seq.
**Files:** internal/tui/mousereassert.go, internal/tui/mousereassert_test.go, internal/tui/tui.go, internal/tui/settingsapply.go, internal/tui/settingswatcher.go, internal/tui/model.go, internal/tui/settings_test.go, internal/tui/pointershape.go, internal/tui/pointershape_test.go (internal/tui/conpty_windows_test.go only if `claimTerminalScreen`'s signature moves)
**Read first:** internal/tui/tui.go — Run, claimTerminalScreen, claimAltScreen, ansiEnterAltScreen/ansiExitAltScreen; internal/tui/mousereassert.go — reassertMouse, mouseTrackingSeq; internal/tui/settingswatcher.go — settingsExternalEdit;
internal/tui/mousereassert_test.go — reassertCount, TestRunWritesLandsARecordWriteBatchedWithTheReassert, TestResizeReassertsMouseTrackingVerbatim; internal/tui/altscreen_test.go — TestClaimTerminalScreenPreparesTheConsoleBeforeTheSwitch; internal/tui/settings_test.go — TestSettingsPaneForegroundEditorTakesTheTerminal;
bubbletea v2.0.8 tea.go — exec, releaseTerminal, stopRenderer, shutdown; tty.go — restoreTerminalState
**Tests:** resize/tool result under the gate on writes one Raw carrying the tracking seq then the shape; gate off writes only the tracking seq (existing reassert tests unchanged); toggle off → reset + CellMotion; editor handoff under the gate → the both-stacks reset Raw is sequenced before the exec (opening the `sequenceMsg`; `expandBatch` opens only `BatchMsg`); reassert under AllMotion before any shape is written → the bare tracking seq; gate off → `TestSettingsPaneForegroundEditorTakesTheTerminal` unchanged; exit release writes reset + enter-alt + reset + exit-alt only when a shape was written, nothing extra otherwise, and `TestClaimTerminalScreenPreparesTheConsoleBeforeTheSwitch` stays green.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'Reassert|TestPointerShapeReset|TestSettingsApplyLocal|TestSettingsPaneForegroundEditorTakesTheTerminal|TestClaimTerminalScreen' ./internal/tui/`
**Commit:** `feat(tui): re-assert the pointer shape and reset it on release`

## 8. Document pointer shapes in the layout spec and probe manual

**What:**
Depends on item 7.
**Goal:** `layout.md` states the hover-shape rule per region and its gate; `docs/manual/probe.md` names the `pointer-probe` diag key; no doc claims hover is ignored without the gate caveat.
**Approach (assumed at the header base):** a short subsection in `layout.md`'s mouse section ("The pointer shows what a click does"): region → shape table, gate, reset on release, tmux off. `docs/manual/probe.md` `--tui-diag` list gains `pointer-probe`. Rule for prose guards: every doc sentence stating that button-less motion is ignored or that apogee tracks cell motion only gets the gate caveat — find them with `grep -rn -i 'motion\|cell-motion\|1002' layout.md docs/`.
**Regression guard.** The prose-guard rule and its grep cover live docs only: `grep -rn -i 'motion\|cell-motion\|1002' layout.md docs/layout docs/manual docs/design/*.md README.md`. Historical dirs (docs/plans/archived/, docs/reviews/, docs/design/archived/, docs/handoffs/) are never edited; the live code comments stating cell motion are items 5's and 6's.
**Files:** layout.md, docs/manual/probe.md
**Read first:** layout.md — "Collapsed and expanded blocks" (What a click means), "What \"height\" means" (the wheel follows the pointer); docs/manual/probe.md — `--tui-diag` paragraph (mouse-kind, mouse-reassert); internal/tui/mousereassert.go — diagMouseReassert; internal/tui/diagnostics.go — diagMouseKind; cmd/apogee/docs_env_test.go — TestDocsEnvTraceFlagsWriteFilesAndStayOutOfHelp
**Tests:** none (docs only); `go test` drift tests for manual counts still pass.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestManual' ./cmd/apogee/`
- `grep -n 'pointer-probe' docs/manual/probe.md` and `grep -n 'OSC 22' layout.md` each print a line.
- The scoped live-doc grep above shows no sentence claiming motion or hover is ignored without the gate caveat.
**Commit:** `docs(layout): describe hover pointer shapes`
