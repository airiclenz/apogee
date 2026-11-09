# Side panel visual alignment

**Goal:** The side panel speaks the transcript's visual vocabulary — header shape, fold markers, tones, spacing, glyph sets — while keeping its behaviour (sections, modes, stops, width rules, overflow order). Sections fold like transcript blocks and the fold persists. Two panel bugs (unclamped Usage percent, misaligned spinner rows) are fixed on the way.
**Date:** 2026-11-09
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 60ce8b92
**Closes:** side-panel-visual-alignment

**Sources:**
- `.capcom/issues/side-panel-visual-alignment.md` and its children under `.capcom/issues/side-panel-visual-alignment/`
- ADR 0110, ADR 0111 (`capcom know show "ADR 0111" --with-children`); glossary `Side panel`, `Sub-agent`, `Navigation mode`
- `docs/layout/side-panel-layout.md`; `layout.md` §"The side panel" and the fold-indicator rules; `docs/layout/tool-layout.md` §Fold states
- `docs/manual/configuration.md` §`ui.glyphs`, §`ui.side-panel`, §The side panel

**Ratified design calls (owner, 2026-11-09):**
- **Scope:** headers, glyphs, spacing and tones may change freely; behaviour stays; ADR 0110/0111 and the spec are amended where a visual rule changes. Framing borders are out.
- **Header shape:** `<pin> Title (summary) ⋯ ▶/▼`; fold indicator via `stateIndicator`/`toolIndicator`, leader in `toolLeader`.
- **Pin:** the header's lead glyph, `●` pinned / `○` auto, replacing `◆`; `⊙` retires.
- **Pin keyboard path:** a second navigation stop on the lead pin cell; `⏎` there toggles pinned/auto. `⏎` on the header title stop folds. ADR 0111 D4 amended.
- **Fold persistence:** a new `ui.side-panel.folded` list (e.g. `[usage, mcp]`), written silently per toggle like `ui.task-list-open`; `ui.side-panel.sections` is untouched. While watching, a fold toggles and writes, as `ui.task-list-open` does.
- **Overflow:** a squeezed section wears `▶`. Opening one puts it in a session-only "opened" tier between pinned and auto (never written to config); the last auto section in list order is squeezed instead. Folded headers carry no hidden-row count.
- **Vocabulary:** panel words win — `Sub-agents`, `Tasks`, and `queued` for a not-yet-started sub-agent run, on both surfaces.
- **Summary:** the Sub-agents summary reads `(2 · 1 queued)` — running count bare, other phases named only when non-zero.
- **Body rows:** indented `bodyIndent` under the header with a consistent one-cell lead per row; no `┝`/`┕` branches.
- **Mark position:** the sub-agent status mark stays the row's lead (`✓ planner`); the transcript is unchanged.
- **Failure:** `errorText` everywhere — panel, `/mcp` and transcript.
- **Separator:** painted in the scroll track's `chrome` role; the drag state keeps `panelDragRule`.
- **Safe glyphs:** `●`→`•`, `○` stays `○`, `▸`→`>`, `↳`→`»`.
- **Narrow panel:** right margin, then the extra gutter cell, then the body indent, yield before any content when the content would be narrower than a full sub-agent row (indent + panel lead + gap + facts); MinSidePanelWidth unchanged.
- **Default width:** `ui.side-panel.width` default 36 → 44, restoring body text room; explicit widths unchanged.
- **Dock gate:** thresholds stay 137 (`auto`) / 117 (`on`), decoupled from the 44 default width.
- **Transcript labels:** lone/background block `Sub-agent`, group heading `Sub-agents (N)`, task list `Tasks (d/n)`; resumed records re-derive labels from the registry.

**Standing requirements:**
- skills: coding-standards
- Narrow test runs only: one package, `-run` filtered; never two runs at once.
- An ADR amendment is the Note `adr-NNNN-amend-MMDD` for the execution date (next free letter suffix if taken), named on the record's Status line and reading-order list.
- Each item updates the spec and manual sections its change makes false.

**Out of scope:**
- Borders around the panel; scrolling inside it; reordering sections by importance.
- Any `ui.side-panel.sections` schema change.

**Regression check (2026-11-09, 60ce8b92):**
- 1: guard folded — the panel usage row carries no percent; the clamp test asserts header, sub-agent fill, `usageFillCell` and gauge.
- 2: guard folded — no `spinnerAnim.view` (surface background); foreground-only helper honouring `ui.spinner-color`.
- 3: guard folded — writer decision: KindStructured row, dedicated `WriteSidePanelFolded` seam; template, settings-rows fixture and fake host follow.
- 4: guard folded — pin cell from the painted column's left edge, so the overlay pin click lands.
- 5: guard folded — writer decision: fold held as the canonical string on `m.opts.UI` (ADR 0011); title hover/click tests flipped.
- 6: guard folded — writer decision: overlay test file and `Overlay` run; two visibly distinct header stops; ADR 0111 D4 amended as `adr-0111-amend-1109b`.
- 7: guard folded — writer decision: overlay test file; unfold enters the opened tier; `▶` only on a section with body rows.
- 8: recast — writer decision: separate left-chrome and right-margin constants, overlay slot unchanged (ADR 0111 D7 holds), narrow-panel yield order.
- 9: recast — writer decision: one panel-wide lead width (supersedes the one-cell lead); file and MCP rooms subtract it.
- 10: guard folded — every summary form spelled out in Goal and spec.
- 11: guard folded — e2e tests, resumed labels, lone/background delegation, widened prose grep; supersedes tool-layout.md's Title Case label note.
- 12: guard folded — writer decision: `fold_test.go` listed; `/mcp` cells stay ANSI-free; supersedes the spec's faint phase word and flush-left MCP row.
- 13: guard folded — writer decision: fill row `used/limit N% bar` via item 1's helper; all three gauge backgrounds dropped; reverses `panelTokens`' zero-as-`0` rule.
- 15: guard folded — named glyph exceptions; safe frame test on a populated panel.
- 16: guard folded — acceptance proves `ctx 62%`, `⊙`, `◆` gone and `▼` drawn.
- 8 (round 2): recast — owner call: default width 44, body indent as the third narrow-width yield; guards folded: a yielding left-chrome method, the clamped-overlay hang, `SubAgentRow` in the filter, the configuration.md gutter rule; the grip yields to adr-0110-amend-1108 (b).
- 9 (round 2): recast — writer decision: Goal states the panel-wide lead; `▸` sits in the body indent; a 12-character name and the usage split row paint whole at 44; `MCPLost` in the filter; `stripPanelRows` trims indent and lead.
- 10 (round 2): recast — writer decision: binding summary forms `(2)`, `(2 · 1 queued)`, `(1 queued)`, `(3 done)`, none; replaces the "spell out" guard.
- 11 (round 2): recast — owner call: `Sub-agent` / `Sub-agents (N)` / `Tasks (d/n)`; resumed records re-derive labels; replaces the "choose/state" guard.
- 8 (round 3): guard folded — owner call: the show/dock gate keeps its own 36-column constant (137 `auto` / 117 `on`), decoupled from the 44 default width; a test docks the default-width config at 137 under `auto`.

## 1. Clamp the panel's Usage percent through one shared helper — ✅ DONE (2026-11-09)

NOTES (2026-11-09): added TestUsage_WithFillClampsAnOverfullSummary in internal/sidepanel beside the planned helper table test, pinning the bug at UsageView.WithFill itself; percentCap stays (the cache-hit row still uses it) with its comment trimmed to drop the fill-clamp role it no longer has.

**What:** Fixes bug `panel-usage-unclamped-percent`: `UsageView.WithFill` builds its header summary from the unclamped `Fill.Percent`, so a fill over the window reads `Usage 137%` while the gauge reads 100%.
**Regression guard.** `panelUsageRows` paints `<bar> used/window` with no percent (`internal/tui/sidepanel.go`), so the clamp test asserts `100%` in the panel header, in a sub-agent row's fill (a `SubAgentRow` whose `Fill` is over its window), in `usageFillCell` and in the status-line gauge — never in a panel usage row.
**Goal:** One exported clamped-percent helper (result in [0,100], 0 for a non-positive limit) serves the panel's Usage header, the panel's sub-agent fill, the status-line gauge figure and `/usage`'s fill cell; a fill above the window renders `100%` in all of them.
**Approach (assumed at the header base):** add the helper in `internal/sidepanel` (imported by every site already, or the lowest shared package if one exists); make `Fill.Percent` return it; replace the inline `min(...,100)` / `min(..., percentCap)` clamps in `contextUsage.view` (`internal/tui/model.go`), `usageFillCell` (`internal/tui/usage.go`) and `panelSubAgentRow` (`internal/tui/sidepanel.go`) with calls to it.
**Closes:** panel-usage-unclamped-percent
**Files:** internal/sidepanel/sidepanel.go; internal/sidepanel/sidepanel_test.go; internal/tui/model.go; internal/tui/usage.go; internal/tui/sidepanel.go; internal/tui/sidepanel_test.go
**Read first:** internal/sidepanel/sidepanel.go — Fill.Percent, UsageView.WithFill; internal/tui/sidepanel.go — panelSubAgentRow, percentCap;
internal/tui/model.go — contextUsage.view; internal/tui/usage.go — usageFillCell; internal/tui/heartbeat_test.go — TestGaugePercentClamped;
internal/tui/sidepanel_test.go — TestSidePanelPaintsEachSection
**Tests:** a table test of the helper (under, at, over the limit; zero and negative limit); `TestSidePanelUsageHeaderClampsOverfullFill` — a fill above the window reads `100%` at the four sites the guard names.
**Acceptance:**
- `go test -race -count=1 ./internal/sidepanel/`
- `go test -race -count=1 -run 'SidePanel|Usage|Gauge' ./internal/tui/`
**Commit:** `fix(tui): clamp the side panel's Usage percent like the gauge`

## 2. Align running sub-agent rows under every spinner and colour their spinner — ✅ DONE (2026-11-09)

NOTES (2026-11-09): consequential edit — docs/layout/side-panel-layout.md: made necessary by the coloured panel spinner and the spinner-wide lead cell (§Sub-agent rows now states both).
NOTES (2026-11-09): added `spinnerAnim.tinted` (foreground-only colour loop, bare glyph when `ui.spinner-color` is off) and `spinnerAnim.width` (declared style width, one cell under `ui.glyphs: safe`); the lead cell width is `Model.panelSubAgentLeadWidth`, the max of that and the measured queued/waiting/done/failure marks.

**What:** Fixes bug `panel-subagent-lead-misalign`: the default `snake` spinner's frames are two cells wide, so a running row's name sits one column right of queued/done/failed/waiting rows; and the panel's spinner is the bare `drawn` frame, uncoloured.
**Regression guard.** Do not call `spinnerAnim.view`: it renders on `th.spinnerBase` (`Background(surface)`), which would notch the panel's background-free rows and the cursor row's selection tone. Add a helper in `spinner.go` that layers `spinnerColor`'s foreground on a style with no background, and pass `m.opts.UI.SpinnerColor`, so `ui.spinner-color: false` leaves the panel spinner uncoloured as it leaves the status line.
**Goal:** In the Sub-agents section the name column of running, queued, done, failed and waiting rows is the same column under every spinner style and under `ui.glyphs: safe`; the running glyph carries the same colour loop as the status line's spinner.
**Approach (assumed at the header base):** in `panelSubAgentRow`, size the lead cell to the widest frame of the active spinner style (and the widest one-cell status glyph), padding narrower leads on the right; paint the running lead through the guard's foreground-only helper instead of `drawn`.
**Closes:** panel-subagent-lead-misalign
**Files:** internal/tui/sidepanel.go; internal/tui/spinner.go; internal/tui/sidepanel_test.go
**Read first:** internal/tui/spinner.go — spinnerAnim.view, spinnerAnim.drawn, spinnerSpecs; internal/tui/theme.go — spinnerBase, panelWaiting;
internal/tui/sidepanel.go — panelSubAgentRow; internal/tui/sidepanel_test.go — TestSidePanelRunningDelegationSpinsWithTheStatusLine;
internal/tui/model_test.go — testUIPrefs (Spinner unset resolves to classic, so every test must set the style explicitly)
**Tests:** `TestSidePanelSubAgentNamesAlignUnderEverySpinner` — a run of every phase, looped over each spinner style plus safe glyphs, asserts one name column (`factColumns`-style measurement); a style assertion, with `SpinnerColor=true`, that the running lead carries the status-line spinner's foreground colour and no background; every test sets the spinner style explicitly.
**Acceptance:** `go test -race -count=1 -run 'SidePanel|Spinner|Snake' ./internal/tui/`
**Commit:** `fix(tui): line up running sub-agent rows and colour the panel spinner`

## 3. Add the `ui.side-panel.folded` setting — ✅ DONE (2026-11-09)

NOTES (2026-11-09): the seam is `SettingsHost.WriteSidePanelFolded(section, folded bool) (domain.SidePanelFolded, error)` — one section's fold per call, edited inside the write transaction (config.SaveSidePanelFold), answering the canonical list for item 5 to land on `m.opts.UI`; the file line is written in canonical spelling, `[]` when nothing is folded (the zero value `""` is the one in-memory spelling of "none").
NOTES (2026-11-09): consequential edit — internal/tui/settingsapply.go: made necessary by registering the key — `domain.UIKeySidePanelFolded` joins the side-panel case label of `settingsApplyLocal`, the renderer arm `cmd/apogee/wire_settings_test.go`'s `settingKeysAppliedByTheRenderer` list mirrors, so a hand-edited fold list re-read by the watcher lands live instead of being refused at the binary's dispatcher.
NOTES (2026-11-09): `ResetSidePanelSetting` refuses the folded key as it refuses `ui.side-panel.sections` (structured, never reset from a settings surface); `SaveSidePanelSetting` takes the whole list too, for the hand-edit/`Write` path.
NOTES (2026-11-09): the manual and template prose already say that folding or opening a section in the panel writes the list back — the TUI behaviour item 5 delivers through this seam.

**What:** First half of `panel-section-user-fold`: the persisted fold list, with no TUI behaviour yet.
**Regression guard.** The registry row is KindStructured, like ui.side-panel.sections. Writes go through a dedicated SettingsHost.WriteSidePanelFolded seam that mirrors WriteSidePanelSection (internal/tui/tui.go, cmd/apogee/wire_options.go) and its test spy. cmd/apogee/wire_settings_test.go treats the key exactly as it treats ui.side-panel.sections. Item 5 uses this seam. The row is not Editable and the key stays out of `domain.uiKeys`, as `UIKeySidePanelSections` is. The seam method lands on `fakeSettingsHost` (`internal/tui/settings_test.go`) in this item, or the tui test package stops compiling. The registry row sits right after the `ui.side-panel.sections` row, and `internal/config/defaults/config.yaml` gains a commented `# folded: []` after the `# sections:` line with a prose paragraph. The zero value `""` means no folds (its Read spells it `[]`), so no resolved-`UIPrefs` literal in `internal/config/config_test.go` moves.
**Goal:** `ui.side-panel.folded` is a registered, documented setting holding a list of section names (default `[]`), parsed and spelled canonically in `internal/domain`, refusing unknown section names, round-tripped by a silent writer the TUI can call, and listed in `docs/manual/configuration.md` §`ui.side-panel`.
**Approach (assumed at the header base):** mirror `SidePanelSections`: a comparable string type with `Parse…`/`Spell…` in `internal/domain/sidepanelprefs.go`, a `UIPrefs` field and key in `internal/domain/uiprefs.go`, YAML read in `internal/config/config.go`, a registry row in `internal/config/registry.go`, a save path beside `SaveSidePanelSection` in `internal/config/configwrite_sections.go`, and the guard's `WriteSidePanelFolded` seam.
**Files:** internal/domain/sidepanelprefs.go; internal/domain/sidepanelprefs_test.go; internal/domain/uiprefs.go; internal/config/config.go; internal/config/registry.go; internal/config/configwrite_sections.go; internal/config/configwrite_sections_test.go; internal/tui/tui.go; cmd/apogee/wire_options.go; docs/manual/configuration.md; internal/config/defaults/config.yaml; internal/tui/settings_test.go; cmd/apogee/wire_options_test.go; cmd/apogee/wire_settings_test.go; cmd/apogee/settingsrows_test.go
**Read first:** internal/config/configwrite_sections.go — IsSidePanelKey, SaveSidePanelSetting; internal/config/registry.go — ui.side-panel.sections row;
internal/domain/sidepanelprefs.go — ParseSidePanelSections, SpellSidePanelSections; internal/domain/uiprefs.go — UIKeySidePanelSections, uiKeys;
cmd/apogee/wire_options.go — settingsHost.Write; cmd/apogee/settingsrows_test.go — TestSettingsRowsFormatEffectiveValues
**Tests:** parse/spell round trip, unknown name refused, duplicates collapsed; config save/reload round trip; a `WriteSidePanelFolded` round trip in `wire_options_test.go`; `TestSettingsRowsFormatEffectiveValues` pins the folded row's value; `TestTemplateMentionsEveryRegistryKey`, `TestRegistryFollowsTheTemplateOrder` and `TestManualDocumentsEverySettingsKey` pass.
**Acceptance:**
- `go test -race -count=1 ./internal/domain/ ./internal/config/`
- `go test -race -count=1 -run 'TestManualDocumentsEverySettingsKey|TestSettingsRows|TestEveryEditableSettingKeyHasAnApply|TestSettingsTableIsInRegistryOrder|TestSettingsHost' ./cmd/apogee/`
- `go build ./...`
- `go vet ./internal/tui/` (compiles the tui test package against the widened `SettingsHost`)
**Commit:** `feat(config): add the ui.side-panel.folded setting`

## 4. Move the pin to the header's lead glyph — ✅ DONE (2026-11-09)

NOTES (2026-11-09): the shared pin-cell helper is the production method `Model.panelPinX` (`m.width - m.panelPaintWidth() + sidePanelChromeWidth`), which `panelTargetAt` and every rewritten docked, overlay and nav-parity test read; no test-only twin.
NOTES (2026-11-09): pointershape.go needed no change — `pointerShapeAt` asks `panelTargetAt`, so the hover follows the moved pin cell; it is not in FILES.
NOTES (2026-11-09): the pin glyph keeps its old `statusFaint` tone as the header lead; header tones are item 10's. Tests reading the old summary form (`Usage 100%`, `Changed files 1`, `MCP 1 dialling`) now read the parenthesised form.

**What:** Second step of `panel-section-user-fold`: the header takes the transcript's lead shape; the right-edge pin glyph retires.
**Regression guard.** Take the lead pin cell from the painted column's own left edge, `m.width - m.panelPaintWidth() + sidePanelChromeWidth` (`panelCell`'s geometry), never from `slotWidth()`: under the narrow-window overlay `panelWidth()` is 0, so `slotWidth()` is the whole window and an overlay pin click would miss. The rewritten docked, overlay and nav-parity tests share one helper for that cell.
**Goal:** A section header renders `● Title (summary)` when pinned and `○ Title (summary)` when auto, with no glyph at the right edge; a click on the lead pin cell toggles pinned ↔ auto and persists exactly as the right-edge pin did (watch refusal and `not saved` unchanged); the header's navigation stop still toggles the pin; `◆` and `⊙` are no longer drawn.
**Approach (assumed at the header base):** in `panelHeaderRow` paint `glyphPanelPinned`/`glyphPanelOpen` as the lead and wrap the summary in parentheses; drop the right-hand pin; move the pin hit cell in `panelTargetAt` from `x == m.width-1` to the lead cell (the guard's geometry); remove `glyphPanelHeader` and `glyphPanelUnpinned`. Update `pointerShapeAt` if it keys on the old cell.
**Files:** internal/tui/sidepanel.go; internal/tui/theme.go; internal/tui/pointershape.go; internal/tui/sidepanel_test.go; internal/tui/sidepaneloverlay_test.go; internal/tui/navmode_test.go; docs/layout/side-panel-layout.md; docs/manual/configuration.md
**Read first:** internal/tui/sidepanel.go — panelHeaderRow, panelTargetAt, panelCell, panelPaintWidth; internal/tui/panelstops.go — panelStops;
internal/tui/sidepaneloverlay_test.go — TestOverlayInsideClickActsAsDocked; internal/tui/sidepanel_test.go — TestPanelStopsMatchClickTargets;
internal/tui/navmode_test.go — TestNavModeEnterMatchesClickPerKind
**Tests:** rewrite the tests that click `m.width-1` to click the lead cell through one shared helper; `TestSidePanelHeaderLeadsWithPin` asserts `●`/`○` lead and an empty right edge; overlay pin click.
**Acceptance:** `go test -race -count=1 -run 'Panel|Overlay|NavMode|CursorMark|PointerShape' ./internal/tui/`
**Commit:** `feat(tui): lead each side-panel header with its pin glyph`

## 5. Fold side-panel sections by click and persist the fold

**What:** Third step of `panel-section-user-fold`. Depends on items 3 and 4.
**Regression guard.** The fold state is held as the canonical domain string on m.opts.UI, as SidePanelSections is. It is never a map or other no-copy state seeded into the value-copied Model (ADR 0011); writes go through item 3's `WriteSidePanelFolded` seam. A title click now folds: flip `TestSidePanelClickHoverShapeMatches`' header-title case to `shapePointer`, drop the header-title cell from `TestSidePanelClickHeaderTitleBlankAndWheelDoNothing` and pin it in the new fold test; update the `panelTargetNone` comment (`sidepanel.go`) and the spec's Pointer section to match.
**Goal:** Every section header ends in a `toolLeader` leader and a right-edge `stateIndicator` (`▼` open, `▶` folded; `►` under safe) painted in `toolIndicator`; a click anywhere on the header except the lead pin cell folds/unfolds the section; a folded section shows its header row only, costs exactly one row in the overflow allocation and is never unfolded by allocation; each toggle writes `ui.side-panel.folded` silently (failure: `side-panel.folded: not saved: …`, the flip stands) and the fold survives a restart; a fold while watching toggles and writes.
**Approach (assumed at the header base):** read the fold from `m.opts.UI` (re-read on the settings-apply path that re-seeds `toolsOpen`); `panelAllocation` treats a folded section as header-only; `panelHeaderRow` renders the leader and indicator via `indicatorRow`/`stateIndicator`; add a fold `panelTargetKind` resolved by `panelTargetAt` on the header outside the pin cell, handled in `handleSidePanelClick` with a toggle mirroring `toggleTaskListFold`.
**Files:** internal/tui/sidepanel.go; internal/tui/model.go; internal/tui/settingsapply.go; internal/tui/sidepanel_test.go; internal/tui/settingsapply_test.go; docs/layout/side-panel-layout.md; docs/manual/configuration.md
**Read first:** internal/tui/sidepanel.go — panelAllocation, panelHeaderRow, panelTargetAt, setPanelSectionMode; internal/tui/blockstate.go — stateIndicator;
internal/tui/settingsapply.go — toggleTaskListFold; internal/tui/sidepanel_test.go — settingsSpy.host, TestSidePanelClickHoverShapeMatches
**Tests:** header renders `▼`/`▶` in `toolIndicator`; title click folds and writes `[name]`; second click unfolds; failed write says `not saved`; a model built from `folded: [usage]` starts folded; overflow never unfolds a user fold; safe set shows `►`; the header title hovers `shapePointer`; `TestSidePanelClickHeaderTitleBlankAndWheelDoNothing` no longer claims the title.
**Acceptance:** `go test -race -count=1 -run 'Panel|Overlay|SettingsApply' ./internal/tui/`
**Commit:** `feat(tui): fold side-panel sections and keep the fold`

## 6. Give the pin its own navigation stop and amend ADR 0111 D4

**What:** Last step of `panel-section-user-fold`. Depends on item 5.
**Regression guard.** `panelCursorRow`/`markCursorRow` mark a whole row, so the two stops on one header must differ on screen (e.g. the pin stop marks only the lead pin cell in selection, the title stop the rest of the row); pin it in `TestNavModePanelHeaderHasFoldAndPinStops` by comparing the two frames. `panelTargetAt` chooses between a header's two stops by x instead of taking the first stop that spans y, and `TestOverlayStopsMatchClickTargets` and `TestPanelStopsMatchClickTargets` click each stop at its kind's x. Rule: every doc or Note that says what ⏎ does on a panel header — `grep -rn -i "header.*toggles\|toggles.*pin" docs .capcom/knowledge/glossary` (archived/historical excluded). The item supersedes ADR 0111 D4 by its amendment; `adr-0111-amend-1109` exists, so a 2026-11-09 amendment is `adr-0111-amend-1109b`.
**Goal:** In navigation mode each section header has two stops: the title stop, whose `⏎` folds/unfolds like the header click, and the lead pin-cell stop, whose `⏎` toggles pinned/auto; the cursor stays on the stop it acted on; ADR 0111 carries a dated D4 amendment saying so; the spec's Pointer and Keyboard sections, the manual's side-panel keyboard text and the glossary Note `side-panel` state the fold and pin rules.
**Approach (assumed at the header base):** `panelStops` emits a pin stop and a fold stop per header (`panelStopID` gains a field distinguishing them so `reseatPanelCursor`/`nearestPanelStop` anchor by identity); `activatePanelCursor` keeps navigation mode for both header targets; `panelCursorRow`/`markCursorRow` mark each stop as the guard says.
**Closes:** panel-section-user-fold
**Files:** internal/tui/panelstops.go; internal/tui/blockcursor.go; internal/tui/sidepanel.go; internal/tui/navmode_test.go; internal/tui/sidepanel_test.go; docs/layout/side-panel-layout.md; docs/manual/configuration.md; .capcom/knowledge/decisions/adr-0111.md; .capcom/knowledge/decisions/adr-0111/; .capcom/knowledge/glossary/glossary-identity/side-panel.md; internal/tui/sidepaneloverlay_test.go; docs/manual/commands.md; .capcom/knowledge/glossary/glossary-identity/navigation-mode.md
**Read first:** internal/tui/panelstops.go — panelStopID, panelStops; internal/tui/blockcursor.go — activatePanelCursor, nearestPanelStop, panelCursorRow, markCursorRow;
internal/tui/sidepanel.go — panelTargetAt; internal/tui/sidepaneloverlay_test.go — TestOverlayStopsMatchClickTargets;
internal/tui/sidepanel_test.go — TestPanelStopsMatchClickTargets
**Tests:** `TestNavModePanelHeaderHasFoldAndPinStops` — `→` enters, stops visit pin then title per header; `⏎` on title folds and writes, `⏎` on pin toggles pin; cursor stays; identity reseat after a fold; the pin-stop and title-stop frames differ; docked and overlay clicks at each stop's x match its target.
**Acceptance:**
- `go test -race -count=1 -run 'NavMode|PanelStops|Panel|Overlay' ./internal/tui/`
- `capcom check`
**Commit:** `feat(tui): give side-panel pins their own navigation stop`

## 7. Mark overflow folds with `▶` and keep an opened section open

**What:** Closes `panel-overflow-fold-marks`. Depends on item 5.
**Regression guard.** Unfolding a user-folded section also puts it in the opened tier, and folding a tier member writes the fold and leaves the tier, so one click always shows a change. `▶` derives from "has body rows and not expanded", not from "not expanded" alone (`panelAllocation` skips an empty body): a shown section with an empty body reads `▼` (or no indicator).
**Goal:** On a short window every header folded by overflow shows `▶` and every open one `▼`; clicking (or `⏎` on) a squeezed section opens it into a session-only opened tier that takes rows after pinned sections and before auto ones, squeezing the last auto section in list order instead; folding it again drops it from the tier; nothing is written to config for the tier; `+N more` paints in `toolMarker`; the spec's Overflow section and an ADR 0110 D11 amendment state the rules.
**Approach (assumed at the header base):** `panelAllocation` returns per-section expanded state and allocates pinned → opened tier → auto, in list order within each, with user folds header-only; `panelHeaderRow` derives `▶` as the guard says; the fold toggle on a squeezed (not user-folded) section adds it to the model's opened set rather than writing config; `sidePanelContent` paints `+N more` in `toolMarker`.
**Closes:** panel-overflow-fold-marks
**Files:** internal/tui/sidepanel.go; internal/tui/sidepanel_test.go; docs/layout/side-panel-layout.md; docs/manual/configuration.md; .capcom/knowledge/decisions/adr-0110.md; .capcom/knowledge/decisions/adr-0110/; internal/tui/sidepaneloverlay_test.go
**Read first:** internal/tui/sidepanel.go — panelAllocation, panelLayout, panelHeaderRow, sidePanelContent; internal/tui/blockstate.go — stateIndicator;
internal/tui/theme.go — toolMarker; internal/tui/sidepanel_test.go — TestSidePanelOverflowFoldsToHeaders, TestPanelStopsSkipTargetlessRows
**Tests:** height-20 frame with squeezed sections shows `▶` on each; opening one shows its body and squeezes the last auto section; pinned sections keep rows; a restart (new model) forgets the tier; `+N more` style; on a height-20 frame one click on a user-folded, squeezed section opens it and one click on a tier member folds it and writes the fold; a section with an empty body (`/panel pin changed-files` before any write) never reads `▶`.
**Acceptance:**
- `go test -race -count=1 -run 'Panel|Overlay' ./internal/tui/`
- `capcom check`
**Commit:** `feat(tui): mark overflow-folded panel sections and honour an opened one`

## 8. Set the panel's gutters and right margin to the chrome's columns

**What:** Recast at the regression check (2026-11-09). Column half of `panel-spacing-and-margins`, and the panel's default width. Depends on item 7.
**Regression guard.** (a) The left chrome (separator plus the `bodyIndent` gutter) is a Model method that yields with the content: the content start, `sidePanelColumn`, the pin/fold hit cells, `columnText` and the test x helpers all read it. `panelGripAt` keeps the grip at the separator and the one cell right of it — the item yields to adr-0110-amend-1108 (b) — so a press on the lead pin cell never arms a drag at any width. The right margin is a separate value, subtracted only by `panelContentWidth` and the row squaring. (b) Under the overlay, `slotWidth` and `panelWidth` stay unchanged, so the transcript never re-wraps (ADR 0111 D7 holds). The overlay's blank column is hung one column further left in View's `joinColumn` only when `m.width-panelPaintWidth() >= 1`; otherwise it comes out of the overlay's own width. The spec says whether that blank cell is a column cell (`TestOverlayStopsMatchClickTargets` and the outside-click case click there). (c) Owner call 2026-11-09: `domain.DefaultSidePanelWidth` becomes 44. The registry default derives from it. Update every doc and test that states or pins 36 as the default; an explicitly configured width is unchanged. Extend the narrow-width yield chain: when the content is narrower than a full sub-agent row, yield the right margin first, then the extra gutter cell, then the body indent, before any fact is cut. A full sub-agent row counts indent + panel lead + gap + facts. The width-24 test runs under every spinner style. MinSidePanelWidth and its refusal wording are unchanged; the spec's Columns text states the chain. Rule: every passage stating 36 as the default or a one-cell gutter — `grep -rn -E '\b36\b' layout.md docs/manual docs/layout internal/config/defaults .capcom/knowledge/glossary` and `grep -n -i 'gutter\|blank column beside' docs/layout/side-panel-layout.md docs/manual/configuration.md`; ADR 0110's stated default is corrected by a dated amendment, never by rewriting its Decision text. (d) Owner call 2026-11-09: the show/dock gate is decoupled from `domain.DefaultSidePanelWidth`. Today `sidePanelWidth = domain.DefaultSidePanelWidth` (internal/tui/sidepanel.go) feeds the gate. It becomes its own constant holding today's 36 (named for the gate, e.g. `sidePanelGateWidth`), so the `auto` threshold stays at 137 columns and `on` at 117. Between 137 and 145 columns the panel docks clamped narrower than 44 by `clampPanelWidth`, and the yield chain applies. `sidepanel_test.go`'s threshold pin, side-panel-layout.md's threshold lines and configuration.md's threshold line keep their numbers. Add a test asserting that the default-width config docks at 137 under `auto`.
**Goal:** `ui.side-panel.width` defaults to 44, explicit widths unchanged. Where the content holds a full sub-agent row, the panel's left gutter is `bodyIndent` wide and its right-edge cells (fold indicator, right-aligned facts) sit in the status line's last cell's column (`bodyIndent` short of the window edge); narrower, the guard's (c) chain yields. Every hit cell (pin, fold, body rows, grip) and the `▌` cursor mark follow; the overlay leaves one blank column between the transcript text and its `│`; the spec's Rows/Columns text states the geometry.
**Approach (assumed at the header base):** `sidePanelGutter` becomes `bodyIndent`; the left-chrome method and the right margin replace `sidePanelChromeWidth`'s readers, and `panelRow` stops short by the margin; `panelTargetAt`/`panelGripAt` and `panelBodyX`-style test helpers derive from them, never `m.width-1`; `DefaultSidePanelWidth` in `internal/domain/sidepanelprefs.go` becomes 44.
**Files:** internal/tui/sidepanel.go; internal/tui/model.go; internal/tui/boxdraw.go; internal/tui/sidepanel_test.go; internal/tui/sidepaneloverlay_test.go; internal/tui/navmode_test.go; internal/tui/mouse_test.go; internal/domain/sidepanelprefs.go; internal/domain/sidepanelprefs_test.go; internal/config/config_test.go; internal/config/defaults/config.yaml; cmd/apogee/e2e_sidepanel_drag_test.go; cmd/apogee/settingsrows_test.go; docs/layout/side-panel-layout.md; docs/manual/configuration.md; docs/manual/commands.md; layout.md; .capcom/knowledge/glossary/glossary-identity/side-panel.md; .capcom/knowledge/decisions/adr-0110.md; .capcom/knowledge/decisions/adr-0110/
**Read first:** internal/tui/sidepanel.go — sidePanelChromeWidth, panelContentWidth, panelGripAt, panelTargetAt, panelRow; internal/tui/model.go — View (joinColumn hang);
internal/tui/boxdraw.go — joinColumn; internal/tui/sidepanel_test.go — TestSubAgentRowAlignsPastAnHour
**Tests:** `TestSidePanelRightEdgeMeetsStatusLine` — at 170 wide the indicator's x equals the status line's last non-blank cell's x; hit tests at the new cells, and a press on the lead pin cell is no grip at any width down to 24; overlay blank-column test including the clamped 40-column case, with `TestOverlayOpensOnRightAtNarrowWidth` and `TestOverlayLeavesTranscriptLinesByteIdentical` unchanged and green; at `ui.side-panel.width` 24, under every spinner style, a full sub-agent row's facts paint whole (margin, gutter cell, then body indent yield); `TestSubAgentRowAlignsPastAnHour` green; an unset width, `DefaultUIPrefs`, `/panel width reset` and a separator double-click read 44, and every test pinning 36 as the default follows; `TestSidePanelGate` keeps its 137/117 pin, and an unset width (default 44) under `auto` docks at a 137-column window, clamped by `clampPanelWidth`.
**Acceptance:**
- `go test -race -count=1 -run 'Panel|Overlay|NavMode|CursorMark|Separator|Grip|SubAgentRow' ./internal/tui/`
- `go test -race -count=1 ./internal/domain/ ./internal/config/`
- `go test -race -count=1 -run 'TestSettingsRows|TestE2ESidePanelDrag' ./cmd/apogee/`
- `capcom check`
**Commit:** `feat(tui): line the side panel's edges up with the status line`

## 9. Space sections apart and indent their bodies

**What:** Recast at the regression check (2026-11-09). Row half of `panel-spacing-and-margins`. Depends on item 8.
**Regression guard.** One panel-wide lead width is the widest of the active spinner style's frames and the one-cell status glyphs, the width item 2 sizes. Every section's body rows use it, so the text column is equal across sections; this supersedes the header's Body rows call's one-cell lead. The cross-section column test loops over every spinner style, as item 2's does. `panelFileRow`'s `trimPathLeft` room and `panelMCPRow`'s name room subtract the indent and lead (or route both rows through `panelRow`'s lead). The viewed run's `▸` sits in the first cell of that row's body indent, so it costs no width and moves no column. At the default width 44, a sub-agent name of 12 characters and the usage split row `sys 12k · tools 13k · hist 40k` paint whole; a test pins both. The body indent yields per item 8's chain. `stripPanelRows` (`fold_test.go`) trims the indent and lead, so `TestMCPLostStripsEscapes`' `files · disconnected` check holds.
**Goal:** A spacer row precedes every section header but the first; the overflow allocation counts spacers and drops them before folding any section; body rows of every section start `bodyIndent` right of the header's lead, each led by the panel-wide lead width — the widest frame of the active spinner style, at least 1 cell (status glyph, `↳`, or blank) — so the text column is the same across sections; hit rows and stops follow; the spec states the rule.
**Approach (assumed at the header base):** `panelLayout` emits a spacer `panelLine` kind before headers; `panelAllocation` budgets spacers last; `panelSubAgentRow`, `panelTaskRow`, `panelFileRow`, `panelMCPRow` and `panelUsageRows` prefix `bodyIndent` and a lead cell; `panelStops` skips spacers.
**Closes:** panel-spacing-and-margins
**Files:** internal/tui/sidepanel.go; internal/tui/panelstops.go; internal/tui/sidepanel_test.go; internal/tui/navmode_test.go; docs/layout/side-panel-layout.md; internal/tui/fold_test.go
**Read first:** internal/tui/sidepanel.go — panelAllocation, panelRow, panelSubAgentRow, panelFileRow, panelMCPRow, panelUsageRows;
internal/tui/spinner.go — spinnerSpecs; internal/tui/fold_test.go — stripPanelRows
**Tests:** wide frame: one blank row between sections, none before the first; short frame: spacers gone before any `▶`; body text columns equal across Sub-agents, Tasks, Changed files, Usage, MCP under every spinner style; a changed-file row and an MCP row near the room limit keep their facts; at width 44 a 12-character sub-agent name and the split row `sys 12k · tools 13k · hist 40k` paint whole; the viewed run's `▸` moves no column; `TestMCPLostStripsEscapes` green; row clicks still open their targets.
**Acceptance:** `go test -race -count=1 -run 'Panel|Overlay|NavMode|MCPLost' ./internal/tui/`
**Commit:** `feat(tui): space side-panel sections and indent their bodies`

## 10. Paint panel headers in the transcript's heading tones

**What:** Recast at the regression check (2026-11-09). Tone and summary half of `panel-header-style`. Depends on item 9.
**Regression guard.** Binding summary forms, extending today's `subAgentSummary` under the ratified rule. Running only: `(2)`. Running and queued: `(2 · 1 queued)`. Queued only: `(1 queued)`. Nothing active but runs exist: `(3 done)`; failed runs are not named in the summary, since their rows carry `✗`. No runs: no parentheses. Waiting runs count as running, as today. State these forms in the Goal and in the spec's Header row list (`docs/layout/side-panel-layout.md`).
**Goal:** The header label is bold in `toolLabel`; the `(summary)` is in the muted count tone, not bold; the lead pin glyph is not bold; the Sub-agents summary reads `(2)`, `(2 · 1 queued)`, `(1 queued)` or `(3 done)` (failed runs unnamed; waiting counts as running), and nothing with no runs; at panel width 36 the default Sub-agents header with 2 running and 1 queued renders whole.
**Approach (assumed at the header base):** retire `panelHeader` (Bold + `userText`) in favour of `toolLabel` for the title and the tone `typeRowPaint` uses for its count; reshape `subAgentSummary` in `internal/sidepanel/sidepanel.go`.
**Files:** internal/tui/sidepanel.go; internal/tui/theme.go; internal/sidepanel/sidepanel.go; internal/sidepanel/sidepanel_test.go; internal/tui/sidepanel_test.go; docs/layout/side-panel-layout.md; docs/manual/configuration.md
**Read first:** internal/sidepanel/sidepanel.go — subAgentSummary; internal/tui/sidepanel.go — panelHeaderRow; internal/tui/toolblock.go — typeRowPaint;
internal/tui/theme.go — panelHeader, toolLabel, toolIndicator; docs/layout/side-panel-layout.md — Header row
**Tests:** dark and light style assertions on title, summary and lead; width-36 header test; summary table with one case per binding form (running only, running and queued, queued only, nothing active with a failed run unnamed, no runs, a waiting run counted as running).
**Acceptance:**
- `go test -race -count=1 ./internal/sidepanel/`
- `go test -race -count=1 -run 'Panel|Theme' ./internal/tui/`
**Commit:** `feat(tui): paint side-panel headers in the transcript's heading tones`

## 11. Use one vocabulary for section names and run phases

**What:** Recast at the regression check (2026-11-09). Vocabulary half of `panel-header-style`; closes it with item 10. Depends on item 10.
**Regression guard.** Non-live cmd/apogee e2e tests pin the old words: switch `drv.WaitText("Sub-Agent")` (`e2e_naming_test.go`) and `"Task List"` (`e2e_tasklist_test.go`) to the new labels, and retarget `e2e_subagent_preempt_test.go`'s absent-word check to `queued` on the `beta` row. Owner calls 2026-11-09. A lone or background sub-agent block's header reads `Sub-agent` (singular). The folded group heading is the constant `Sub-agents (N)`, not the member label. The task-list block reads `Tasks (d/n)`. `backgroundSubAgentPresenter` matches. A resumed record re-derives its Label from the registry on decode for a known tool name, so old sessions show the new words; pin it with a test. This supersedes `docs/layout/tool-layout.md`'s Title Case label note (`Sub-Agent`, `Task List`); correct it. The prose rule widens to `grep -rniE 'Sub-Agent|Task List|\bscheduled\b'` over layout.md docs/layout docs/manual .capcom/knowledge/glossary graphics internal/config/defaults internal/scheme/schemes and internal/tui's comments.
**Goal:** The transcript's sub-agent group heading reads `Sub-agents (N)`, a lone or background sub-agent block `Sub-agent`, its task-list heading `Tasks (d/n)`, and a not-yet-started sub-agent run's phase word is `queued`, matching the panel; `layout.md`, `docs/layout/tool-layout.md`, `docs/manual/` and the glossary use the same words. `scheduled` keeps its other meaning (a Schedule's Firing).
**Approach (assumed at the header base):** make `subAgentGroupLabel` a constant heading; change the registry labels `"Sub-Agent"`/`"Task List"` in `toolregistry.go` (display labels only — check no model-facing or config key reads them), `fromWireToolView`'s Label decode and `scheduledSummary` in `subagentblock.go`; fix the tests that pin the old strings.
**Closes:** panel-header-style
**Files:** internal/tui/subagentblock.go; internal/tui/toolregistry.go; internal/tui/*_test.go that pin the strings; layout.md; docs/layout/tool-layout.md; docs/manual/commands.md; docs/manual/configuration.md; .capcom/knowledge/glossary/; internal/tui/toolview.go; internal/tui/transcriptbridge.go; cmd/apogee/e2e_naming_test.go; cmd/apogee/e2e_tasklist_test.go; cmd/apogee/e2e_subagent_preempt_test.go; graphics/demo/storyboards/hero.yaml; internal/config/defaults/config.yaml
**Read first:** internal/tui/toolregistry.go — sub_agent/task_list labels; internal/tui/subagentblock.go — subAgentGroupLabel, groupLabelOf, scheduledSummary;
internal/tui/toolview.go — backgroundSubAgentPresenter; internal/tui/transcriptbridge.go — fromWireToolView; internal/tui/transcript.go — sameLabelRun;
graphics/demo/storyboards/hero.yaml
**Tests:** existing transcript heading tests updated to the new words; a test that a not-started run's block reads `queued`; lone and background delegation headers read `Sub-agent`; a resumed old-label record decodes to the new labels; the e2e waits read the new labels.
**Acceptance:**
- `go test -race -count=1 -run 'SubAgent|TaskList|ToolRegistry|Umbrella|Present|RunView|BlockCursor' ./internal/tui/`
- `go test -race -count=1 -run 'TestE2ENaming|TestE2ETaskList|TestE2EQueuedMessagePreempts' ./cmd/apogee/`
- `capcom check`
**Commit:** `feat(tui): name sub-agents and tasks alike in transcript and panel`

## 12. Route panel facts through the transcript's outcome tones

**What:** Closes `panel-fact-tones`. Depends on item 9.
**Regression guard.** `mcpServerCells` stays plain and `mcpRowTexts` ANSI-free: `/mcp` paints the failed state as adjacent runs at paint time (`popupLoadingLine`'s cut-and-paint shape, or a per-cell tone the popup module applies), not as SGR inside a cell, whose reset would strip `popupRowLine`'s faint from the cells after it. Rule: every prose mention of the MCP row shape — `grep -rn 'name · state\|connected · 12 tools' layout.md docs/`. This supersedes the spec's "the phase word stays faint like every other" and "written flush left"; rewrite both.
**Goal:** In the panel `+N −M` paints in `toolMarker`, a finished sub-agent's `done` in `successMark`, and every failure (`✗`, `failed`) in `errorText`; `/mcp` paints `failed` in `errorText`; MCP rows right-align `state · N tools` through `panelRow` like the other sections; an ADR 0111 D10 amendment and the spec say so.
**Approach (assumed at the header base):** `panelFileRow` uses `toolMarker` for `diffCounts`; `panelSubAgentRow` paints the phase word by outcome; `panelMCPRow` goes through `panelRow`; style `mcpStateWord`'s failed forms at its call site in `internal/tui/mcppane.go`.
**Closes:** panel-fact-tones
**Files:** internal/tui/sidepanel.go; internal/tui/mcppane.go; internal/tui/sidepanel_test.go; internal/tui/mcppane_test.go; docs/layout/side-panel-layout.md; .capcom/knowledge/decisions/adr-0111.md; .capcom/knowledge/decisions/adr-0111/; internal/tui/fold_test.go; internal/tui/popup.go; layout.md; docs/manual/configuration.md
**Read first:** internal/tui/sidepanel.go — panelMCPRow, panelRow; internal/tui/mcppane.go — mcpStateWord, mcpServerCells; internal/tui/popup.go — popupRowLine, popupLoadingLine;
internal/tui/mcppane_test.go — TestMCPPaneStripsEscapes; internal/tui/fold_test.go — stripPanelRows
**Tests:** style assertions for `+N −M`, `done`, `failed` (panel and `/mcp` equal); MCP row's facts end at the right-edge column; `TestSidePanelMCPSectionRows`, `TestSidePanelMCPClickOpensReport` and `fold_test.go`'s two `files · disconnected` checks re-pinned to the new row shape; `TestMCPPaneStripsEscapes` and `TestMCPPaneListsEveryServer` stay green.
**Acceptance:**
- `go test -race -count=1 -run 'Panel|MCP' ./internal/tui/`
- `capcom check`
**Commit:** `feat(tui): colour side-panel facts like the transcript's outcomes`

## 13. Match the panel's usage rows to the gauge and `/usage`

**What:** Closes `panel-usage-row-style`. Depends on items 1 and 9.
**Regression guard.** The panel's usage fill row reads `used/limit N% bar` like the status-line gauge. Its percent comes from item 1's clamped helper. The no-background variant drops all three backgrounds — `gaugeFill`'s surface, the partial cell's chrome paper and `gaugeTrack`'s chrome; `renderGaugeBar` stays as is for the status line (`TestBottomChromePaintsOneBackground`). This reverses `panelTokens`' recorded rule that a zero reads `0` ("a usage row states a split, where none is a value"); rewrite its comment. If the new words drop `cache hit`, update the tests' row locators that find a row by it.
**Goal:** Panel usage rows read figures then bar (as the status line does), the bar cells carry no background colour, and the rows use `/usage`'s words (`prompt`/`completion` …) and its rule that a zero figure is left blank.
**Approach (assumed at the header base):** give `renderGaugeBar` the guard's no-background variant, used by `panelUsageRows`; reorder the row; reword and blank zeros in `panelTokens`. Take the words from `layout.md` §/usage.
**Closes:** panel-usage-row-style
**Files:** internal/tui/sidepanel.go; internal/tui/model.go; internal/tui/sidepanel_test.go; docs/layout/side-panel-layout.md; docs/manual/configuration.md
**Read first:** internal/tui/sidepanel.go — panelUsageRows, panelTokens; internal/tui/model.go — renderGaugeBar, contextUsage.view; internal/tui/theme.go — gaugeFill, gaugeTrack;
internal/tui/background_test.go — TestBottomChromePaintsOneBackground; layout.md — §The `/usage` popup
**Tests:** dark and light frames: panel bar SGR has no background; row order; zero-blank and wording assertions; the fill row reads `used/limit N% bar` and clamps at `100%`; the status-line gauge's rendering is unchanged; `TestSidePanelPaintsEachSection`, `TestSidePanelResumeRebuildsThroughBothDoors`, `TestSidePanelClickOpensTheRowsDetail`, `TestPanelStopsMatchClickTargets` and `TestSidePanelClickRefusedUnderModalPanes` re-pinned to the new words; the spec's usage table row and the manual's usage row restated.
**Acceptance:** `go test -race -count=1 -run 'Panel|Gauge|Usage|BottomChrome' ./internal/tui/`
**Commit:** `feat(tui): show panel usage like the gauge and /usage`

## 14. Paint the panel separator in the scroll track's role

**What:** Closes `panel-separator-double-stroke`.
**Goal:** The panel separator renders in the `chrome` role the transcript's scroll track uses; while dragging it still renders `panelDragRule`.
**Approach (assumed at the header base):** `sidePanelColumn` swaps `statusFaint` for a style on `th.chrome`; update the separator style assertion.
**Closes:** panel-separator-double-stroke
**Files:** internal/tui/sidepanel.go; internal/tui/sidepanel_test.go
**Read first:** internal/tui/sidepanel.go — sidePanelColumn; internal/tui/theme.go — scrollTrack, panelDragRule, statusFaint; internal/tui/boxdraw.go — scrollTrack.Render(glyphScrollTrack);
internal/tui/sidepanel_test.go — TestSidePanelSeparatorDrag, separatorDragModel
**Tests:** separator style equals the scroll track's; drag state unchanged.
**Acceptance:** `go test -race -count=1 -run 'Panel|Separator|Drag' ./internal/tui/`
**Commit:** `feat(tui): paint the side-panel separator like the scroll track`

## 15. Draw the panel's glyphs from the glyph set

**What:** Closes `panel-glyphs-skip-safe`: under `ui.glyphs: safe` the panel's own glyphs never fall back, contradicting the manual. Depends on items 4–13.
**Regression guard.** "Every glyph from `glyphSet`" holds with named exceptions, as the issue's Acceptance allows: `▼` (`glyphExpanded`, fixed, no safe twin — or give it a safe field, transcript-wide, and say so), the `│` separator, the `▌` cursor and the gauge's block cells; the manual's keep-their-shape list names them. The safe panel frame test builds on `panelModel`-style setup, since `glyphFrameModel`'s `testOpts` paint no panel. Rule: every sentence stating a panel glyph's safe form — ``grep -n "ui.glyphs: safe\|under `safe`" docs/manual/configuration.md docs/layout/side-panel-layout.md``.
**Goal:** Every glyph the panel paints comes from `glyphSet`; the safe set draws `•` pinned, `○` auto/queued/open, `>` current/viewed, `»` delegated; a safe frame of a populated panel shows only safe-set glyphs; the manual's `ui.glyphs` list (swaps and exceptions) matches the code.
**Approach (assumed at the header base):** move the remaining `glyphPanel*` constants into `glyphSet` fields with `defaultGlyphs`/`safeGlyphs` values; read them via `th.glyphs`; extend `TestSafeGlyphFrameDrawsNoUnicodeMarks` with the panel's unicode glyphs.
**Closes:** panel-glyphs-skip-safe
**Files:** internal/tui/theme.go; internal/tui/sidepanel.go; internal/tui/theme_test.go; internal/tui/sidepanel_test.go; docs/manual/configuration.md; docs/layout/side-panel-layout.md
**Read first:** internal/tui/theme.go — glyphSet, defaultGlyphs, safeGlyphs, glyphExpanded; internal/tui/theme_test.go — TestSafeGlyphsMeasureAsTheDefaults, TestSafeGlyphFrameDrawsNoUnicodeMarks, glyphFrameModel;
internal/tui/sidepanel_test.go — panelModel
**Tests:** `TestSafeGlyphsMeasureAsTheDefaults` covers the new fields; safe panel frame test on `panelModel`-style setup (`ui.glyphs: safe`, a pinned and an auto section, a queued sub-agent, a current task, a delegated file) showing only safe-set glyphs and the named exceptions.
**Acceptance:** `go test -race -count=1 -run 'Glyph|Panel' ./internal/tui/`
**Commit:** `fix(tui): draw side-panel glyphs from the safe set under ui.glyphs safe`

## 16. Redraw the side-panel spec's sketch from a rendered frame

**What:** Closes `panel-spec-sketch-drift`; checks the epic's rendered-frame acceptance. Depends on items 1–15.
**Regression guard.** The acceptance also proves the old panel shapes are gone: no `ctx 62%`, `⊙` or `◆` remains in `docs/layout/side-panel-layout.md` (items 4 and 10 retire them file-wide), and the sketch draws `▼`.
**Goal:** The "Where it sits" sketch in `docs/layout/side-panel-layout.md` matches a frame rendered at the current tree: the `▔` session-name hairline, the untitled prompt box, the `used/limit N% bar` gauge, and the panel's headers, folds, spacers and indents.
**Approach (assumed at the header base):** render a wide populated frame in-process (as the panel tests do), copy its geometry into the sketch, and check dark, light and safe frames against the epic's acceptance.
**Closes:** panel-spec-sketch-drift
**Files:** docs/layout/side-panel-layout.md
**Read first:** docs/layout/side-panel-layout.md — "Where it sits" sketch, Header row, MCP rows; internal/tui/sidepanel_test.go — panelModel, panelText;
internal/tui/model.go — contextUsage.view, renderGaugeBar
**Tests:** none (docs).
**Acceptance:** `grep -n '▔' docs/layout/side-panel-layout.md` finds the hairline in the sketch, and `grep -c '╭─ prompt' docs/layout/side-panel-layout.md` prints 0; `grep -c 'ctx 62%\|⊙\|◆' docs/layout/side-panel-layout.md` prints 0, and `grep -n '▼' docs/layout/side-panel-layout.md` finds the fold indicator in the sketch.
**Commit:** `docs(layout): redraw the side-panel sketch from a rendered frame`
