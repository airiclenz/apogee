# ADR 0111 — navigation mode, narrow overlay, MCP status, sub-agent row fixes — plan

- **Goal:** ship ADR 0111: the block cursor becomes one navigation mode over the transcript and the
  side panel (with a non-colour mark and a narrow-window overlay), MCP server lifecycle gets a
  journaled Event feeding `/mcp` and an `mcp` panel section, and sub-agent rows read `waiting`.
- **Date:** 2026-11-08
- **Status:** unexecuted
- **sized for:** ~200k-context host
- **base:** b302274a
- **Sources:** ADR 0111 (`capcom know show "ADR 0111" --with-children`); ADR 0110 (+ amendments);
  ADR 0109; ADR 0031; ADR 0075 (+ amendments); ADR 0052 D4; ADR 0011; `docs/design/side-panel-findings.md`;
  `docs/layout/side-panel-layout.md`; `layout.md`; recipe for a new Event: items 1 and 3 of
  `docs/plans/archived/2026-11-08 - 00 - tui-side-panel-plan.md`; issues named on `**Closes:**` lines.
- **Ratified design calls** (owner, 2026-11-08):
  - **MCP emit seam:** the composition root emits through the engine's own serialised sink (exported as
    `apogee.SerializeEvents`), from an ordered off-loop outbox in `cmd/apogee`; never under `liveMCP.mu`,
    never on the Update goroutine. The engine stays MCP-blind. Glossary `Event` amended to allow it.
  - **Modal click:** a panel click under any modal pane (approval, ask, `/settings`, sessions browser,
    picker, workflows) is claimed and does nothing, default hover; a pin click or pin `⏎` in watch mode
    is refused with `watchRefusedFlash`. The transcript-click-under-approval asymmetry stays.
  - **`⏎` opening a detail:** navigation mode ends (as on a transcript delegation row); a pin toggle keeps it.
  - **Transcript `▌`:** overwrites column 0 (the marker cell) on the cursor row only; the panel's `▌` is its
    blank gutter cell right of `│`. A fixed constant, no safe-set entry (half blocks are fixed).
  - **Panel elapsed:** panel-local formatter: `formatElapsed` below an hour, then `1h02m` to `23h59m`,
    then `1d02h`, clamped at `99d23h`. `formatElapsed` is unchanged.
  - **Waiting glyph:** `◷`, safe `?`, warning foreground without background; word stays faint; only the
    gated run's own row; the section header summary is unchanged.
  - **Failed-kept:** `MCPServerEvent` carries `kept: true` on a `failed` while a live session still serves;
    `/mcp` reads `failed (previous connection kept)` and keeps the tool count.
  - **Smaller calls:** adjacent panel rows with one target are one stop (tasks body, usage); a region that
    loses every stop hands the cursor to the other region, else the mode ends; `→` into a panel with no
    stops does nothing; a click outside the overlay dismisses it, passes through and ends the mode; the
    inactive region's remembered row is unpainted; `esc` in the panel region inside a Run view leaves the
    mode (`runViewKey` steps aside); the key-claim rung is renamed `navigation mode`; MCP names
    `MCPServerEvent`, kind `mcp_server`, states `dialling|connected|failed|disconnected`, reasons
    `removed|denied`; a reconnect goes `dialling`→`connected|failed` with no `disconnected`; startup state
    is seeded by a snapshot into the TUI and into each journal at its birth.
- **Standing requirements:**
  - skills: coding-standards
  - Tests only as `GOMEMLIMIT=2GiB go test -race -count=1 -run '<TestX>' ./<one package>/`; never a package
    pattern, never two test runs at once; pass this rule to every sub-agent.
  - Bubble Tea v2 names (`tea.KeyPressMsg`, `msg.String()`); the Model stays under `TestModelFitsTheStack`.
  - A new file in `internal/tui`, `internal/sidepanel` or `internal/domain` is named in its package `doc.go`.
  - MCP server names, transports and causes are untrusted text: escapes stripped at render.
- **Out of scope:** `mcp-session-death-undetected` (parked); third-party panel sections; LSP status;
  tokens/spend on rows; a docked session switcher; a footer navigation region; key remapping; names vanishing
  below width 26 and the viewed-row clip at width 24.
- **Regression check (2026-11-08, b302274a):**
  - 1: guard folded — the grip stays live under modal panes; watch-mode pin hover default; settings/approval fixtures named.
  - 3: guard folded — agent half runs `TestDispatch_ApproverErrorRefuses`.
  - 5: guard folded — a stop carries a row span; header stops tested at x = m.width-1.
  - 6: guard folded (decision: shared panel-shown builder) — region-gated transcript readers, decision lockout, esc by region, re-resolve at key time; supersedes the `model.go` run-view "one press, one level" and `runview.go` "the one place it steps aside" comments for the panel region.
  - 7: guard folded — paint passes `belowSlot` rows into `panelStops`; the modal test pins item 1's gate.
  - 8: guard folded — panel-shown fixture with a held engine; Run-view case on a sibling.
  - 9: guard folded — every View-reading cursor-row test updated; `highlightBlockCursor`'s comment rewritten.
  - 10: guard folded — the `/help` legend row and the `⌥↑`/`⌥↓` paragraph are the sites.
  - 11: guard folded — widened rewrite grep; Acceptance greps the focus-less sentences away.
  - 12: guard folded (decision: stops resolve against the paint geometry; depends on item 5) — overlay width `min(configuredPanelWidth(), m.width)`; no hidden-panel entry by `⌥↑`/`⌥↓`; bite checks assert the overlay first.
  - 13: guard folded — bare `/panel` enters only when the panel can show, is hidden and has stops.
  - 14: guard folded — count rule widened to `twenty-[a-z]*` over `cmd/apogee` too.
  - 15: guard folded (decision: an `MCPServerEvent` is not engine activity) — item 15 defines the seed entry point.
  - 16: guard folded (decision: the outbox's two-lock contract) — wire test unwraps the serial sink; emit rule plus grep; named serialiser test.
  - 17: guard folded — seed on item 16's contract; skipped servers via a seed-only list; `stateEvents()`; `fakeMCPHost`; TUI half as a unit test.
  - 18: guard folded — `command_test.go`, `mouse_test.go`, `watch.go` listed; ranks stated (five pointer panes shift, not four); `/m` change in CHANGELOG.
  - 19: guard folded — bare `/panel` note and unknown-section lists pin `mcp`.
  - 20: guard folded — `mcp` stop identity; `⏎` ends the mode.
  - 21: guard folded — watch/report verb lists in either order and "four reports"; `sessions.md` and `interjection` listed; grep scoped off historical docs.

## 1. Refuse side-panel clicks under modal panes and the watched pin — ✅ DONE (2026-11-09)

NOTES (2026-11-09): the click and the hover share one predicate, `panelTargetLive` (sidepanel.go), which `followPanelTarget` and `pointerShapeAt` both ask; the item's later `⏎` on a panel stop goes through `followPanelTarget` and inherits the gate.
NOTES (2026-11-09): the pointer-shape cases (default arrow over each target under each modal pane, and over the watched pin) live inside `TestSidePanelClickRefusedUnderModalPanes` and `TestSidePanelPinClickRefusedInWatchMode` in sidepanel_test.go rather than in pointershape_test.go, which is left untouched; the Acceptance pattern runs both.

**What:** fix the defect `panel-click-ignores-modal`: a panel click acts under an approval, ask, `/settings`,
sessions browser, picker or workflows view, and the pin click writes config in watch mode.
**Regression guard.** The resize grip stays live under modal panes as today (`handleSidePanelClick` handles it before
`followPanelTarget`; `panelGripAt` is inert only while watching), so the Goal's "no config write" means no target's
write and `side-panel-layout.md` §Pointer's grip "on any row" stands. In watch mode the pin shows the default shape,
gated in `pointerShapeAt` beside the modal check (`pre.watch.isActive && target.kind == panelTargetPin`); the refusal
is `m.showFlash(watchRefusedFlash)` (a const). The settings case seats `fakeSettingsHost{rows: …}` (`settingsOpts` /
`settingsTestRows`) and a write spy (`settingsSpy`); the approval case sends `approvalReqMsg` before `armApproval`, as
`TestSidePanelClickTakesNoApprovalDecision` does.
**Goal:** a panel click while any modal pane is open is claimed and changes nothing (no Run view, no report,
no config write, no settings-pane state cleared), the hover shows the default shape there; a pin click in
watch mode flashes the watch refusal and writes nothing.
**Approach (assumed at the header base):** gate inside `followPanelTarget` (`internal/tui/sidepanel.go`) so
the click and the later `⏎` share one rule: refuse when a modal pane is open (the panes table's `modal`
flag), and for `panelTargetPin` under `m.watch.isActive` call `watchRefusedFlash`. `handleSidePanelClick`
still claims the click. `pointerShapeAt` (`internal/tui/pointershape.go`) returns the default shape over the
panel while a modal pane is open. Document the rule in `docs/layout/side-panel-layout.md` §Pointer.
**Files:** internal/tui/sidepanel.go; internal/tui/pointershape.go; internal/tui/sidepanel_test.go; internal/tui/pointershape_test.go; docs/layout/side-panel-layout.md
**Read first:** internal/tui/sidepanel.go — followPanelTarget, handleSidePanelClick, panelGripAt; internal/tui/workflow.go — modalPaneOpen;
internal/tui/pointershape.go — pointerShapeAt; internal/tui/sidepanel_test.go — TestSidePanelClickTakesNoApprovalDecision, settingsSpy;
internal/tui/watch.go — watchRefusedFlash
**Tests:** `TestSidePanelClickRefusedUnderModalPanes` (approval via `armApproval`, ask via `askReqMsg`,
settings via `openSettingsPane` on `settingsOpts(settingsTestRows(…))` with a write spy, approval sent as
`approvalReqMsg` before `armApproval`; assert no `pushView`, no report pane, no settings write; a grip drag still
starts under each); `TestSidePanelPinClickRefusedInWatchMode` (`watchRefusedFlash` raised, nothing written); a
pointer-shape case for each, the watched pin reading default.
**Acceptance:** `go build ./internal/tui/ && GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestSidePanelClick|TestSidePanelPin|TestPointerShape' ./internal/tui/`
**Closes:** panel-click-ignores-modal
**Commit:** `fix(tui): refuse side-panel clicks under modal panes and the watched pin`

## 2. Keep sub-agent row columns aligned past an hour of elapsed time — ✅ DONE (2026-11-09)

**What:** fix the defect `subagent-row-elapsed-overflow`: `formatElapsed` renders `01:02:03s` (9 columns)
into the row's 6-column field of `subAgentFactsFormat`.
**Goal:** a sub-agent row's elapsed field is at most 6 columns for any duration, so a 1h 2m 3s row keeps
every column aligned with a row under a minute at panel widths 36 and 24; the status line and `[total …]`
render as before.
**Approach (assumed at the header base):** add a panel-local `panelElapsed(d)` in `internal/tui/sidepanel.go`:
`formatElapsed` below an hour, `1h02m` to `23h59m`, `1d02h` beyond, clamped at `99d23h`; use it only in
`panelSubAgentRow`. Leave `formatElapsed` (`internal/tui/activity.go`) and its callers untouched. Document the
form in `docs/layout/side-panel-layout.md` §Sections "Sub-agent rows".
**Files:** internal/tui/sidepanel.go; internal/tui/sidepanel_test.go; docs/layout/side-panel-layout.md
**Read first:** internal/tui/sidepanel.go — panelSubAgentRow, subAgentFactsFormat; internal/tui/activity.go — formatElapsed, TestFormatElapsed (activity_test.go);
internal/tui/sidepanel_test.go — TestSidePanelNarrowestColumnPaintsLongRows, panelModel; internal/domain/sidepanelprefs.go — MinSidePanelWidth;
docs/layout/side-panel-layout.md — "Sub-agent rows"
**Tests:** `TestPanelElapsedForms` (table: 38s, 59:59, 1h02m03s, 23h59m, 1d02h, 120d); `TestSubAgentRowAlignsPastAnHour`
(non-viewed rows, widths 36 and 24, fact columns at equal offsets).
**Acceptance:** `go build ./internal/tui/ && GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestPanelElapsed|TestSubAgentRowAligns|TestFormatElapsed' ./internal/tui/`
**Closes:** subagent-row-elapsed-overflow
**Commit:** `fix(tui): keep sub-agent row columns aligned past an hour`

## 3. Fold approval gates into the sub-agent section state — ✅ DONE (2026-11-09)

NOTES (2026-11-09): no CHANGELOG entry — the fold changes nothing a user sees until item 4 paints `waiting`; that item carries the entry.
NOTES (2026-11-09): `Reads` answers an ErrorEvent true only for the loop's approver failure (Source "loop" + `domain.ApproverErrorPrefix`), keeping its "Fold would change something" contract; an `ApprovalAllowedByRule` decided phase closes no gate (it never had a requested one).
NOTES (2026-11-09): added two small sibling tests beside the planned table (`TestSubAgentWaiting_TopLevelGateAddsNoRow`, `TestSubAgentWaiting_FinishedRowIsNeverWaiting`); both match the Acceptance `-run` pattern.

**What:** the fold half of ADR 0111 D11; item 4 paints it.
**Regression guard.** The agent half of Acceptance runs `-run 'TestDispatch_ApproverErrorRefuses'` — no test in
`internal/agent` matches `TestApprove`, and that test pins the approver-error emit (`"approver: prompt closed"`) this
item rewires onto the exported prefix constant.
**Goal:** `sidepanel.SubAgentRow` has a `Waiting` flag that is true exactly while the row's own run has an
`ApprovalEvent` requested and not yet closed — closed by decided on the same run, by the approver's
ErrorEvent for that run, by the run finishing, or by its Turn ending (a depth-0 Turn end clears all) — and
only on active delegation rows.
**Approach (assumed at the header base):** in `internal/sidepanel/sidepanel.go` add `ApprovalEvent`,
`ErrorEvent` and `TurnEvent` to `Reads` and `Fold`; key the gate by `RunOf(e.EventBase)` through `s.run(key)`
(never `s.delegation`, so a top-level or unseen run gains no row); `runState.isWaiting` bool, cleared in
`runState.finish`; `SubAgents()` sets `Waiting` only while `phase.isActive()`. Match the approver failure by
an exported `domain` constant for the `"approver: "` prefix, used by `Agent.approve`'s emit in
`internal/agent/dispatch.go` and by the fold (RunKey + `Source == "loop"` + prefix). `Phase` gains no value.
**Files:** internal/sidepanel/sidepanel.go; internal/sidepanel/sidepanel_test.go; internal/domain/events.go; internal/agent/dispatch.go
**Read first:** internal/sidepanel/sidepanel.go — Reads, Fold, State.run, runState.finish, SubAgents; internal/agent/dispatch.go — Agent.approve (requested/decided/ErrorEvent emits);
internal/agent/dispatch_test.go — TestDispatch_ApproverErrorRefuses; internal/sidepanel/sidepanel_test.go — TestReads_NamesExactlyTheVariantsFoldReads
**Tests:** `TestReads_NamesExactlyTheVariantsFoldReads` updated; `TestSubAgentWaitingOpensAndCloses` (two runs
gated, each cleared by its own decided; approver error; finish; depth-0 TurnEvent; allow-rule decided with no
request clears nothing; top-level gate adds no row).
**Acceptance:** `go build ./internal/sidepanel/ ./internal/agent/ && GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestReads_|TestSubAgentWaiting' ./internal/sidepanel/ && GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestDispatch_ApproverErrorRefuses' ./internal/agent/`
**Commit:** `feat(sidepanel): fold outstanding approval gates per sub-agent run`

## 4. Paint `waiting` on sub-agent rows — ✅ DONE (2026-11-09)

**What:** the paint half of ADR 0111 D11. Depends on item 3.
**Goal:** a sub-agent row whose `Waiting` is set reads phase word `waiting` with glyph `◷` (safe `?`) in the
warning foreground with no background, in place of the spinner; done and failed rows are unchanged; watch
mode replaying a journal shows the same marks; the layout spec and the configuration manual name the word
and both glyphs.
**Approach (assumed at the header base):** `internal/tui/theme.go`: `glyphSet.waiting` (default `◷`, safe
`?`) and a `panelWaiting` style (warning fg, no bg — `statusWarning`/`footerWarning` carry the bar surface).
`panelSubAgentRow` (`internal/tui/sidepanel.go`) checks `row.Waiting` before the `row.Phase` switch; the word
stays inside `subAgentFactsFormat`. Docs: `docs/layout/side-panel-layout.md` §Sections "Sub-agent rows"
(glyphs, safe glyphs, words; a header folded section shows no wait) and the `sub-agents` row plus the
`ui.glyphs: safe` sentence in `docs/manual/configuration.md`.
**Files:** internal/tui/theme.go; internal/tui/sidepanel.go; internal/tui/sidepanel_test.go; internal/tui/watch_test.go; docs/layout/side-panel-layout.md; docs/manual/configuration.md
**Read first:** internal/tui/sidepanel.go — panelSubAgentRow, subAgentFactsFormat; internal/tui/theme.go — glyphSet, safeGlyphs, statusWarning;
internal/tui/watch_test.go — newWatchFixture, watchFixture.emit; docs/layout/side-panel-layout.md — "Sub-agent rows"
**Tests:** `TestSidePanelTwoGatedSubAgentsReadWaiting` (one approval showing, one queued; each clears on its own
decided); `TestSidePanelWaitingClearsOnCancel`; `TestWatchShowsWaitingMarks` (`newWatchFixture`, `f.emit`);
`TestSafeGlyphsMeasureAsTheDefaults` still passes.
**Acceptance:** `go build ./internal/tui/ && GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestSidePanelTwoGated|TestSidePanelWaiting|TestWatchShowsWaiting|TestSafeGlyphs' ./internal/tui/`
**Closes:** subagent-row-waiting-mark
**Commit:** `feat(tui): mark a sub-agent row waiting while its approval is outstanding`

## 5. Give the side panel a stop list with identities — ✅ DONE (2026-11-09)

NOTES (2026-11-09): `panelStops` takes the row count (`panelStops(rows int)`) rather than none, so `panelTargetAt` passes `frameSpans().slotBottom` and item 7's paint can pass `belowSlot`, per the plan's regression guard.
NOTES (2026-11-09): `panelBodyTarget(kind, row)` is replaced by `panelBodyStop(section, row)` (target plus identity), reading the source rows `panelSection` now carries (`subAgents`, `files`) so a stop answers for exactly the painted row; items 20's Read-first anchors that name `panelBodyTarget` now resolve to `panelBodyStop` in `internal/tui/panelstops.go`.

**What:** structural groundwork for ADR 0111 D4–5; no behaviour change.
**Regression guard.** A `panelStop` carries a row span (first `y` plus height), not one row: `panelTargetAt` matches any
row in the span (a tasks or usage stop spans several rows, e.g. usage's "cache hit"), and the stop's paint and cursor
`y` is its first row. `panelStops` lays out over the same row count as `panelTargetAt` and `sidePanelColumn`
(`frameSpans().slotBottom` / `belowSlot`). `TestPanelStopsMatchClickTargets` asks header stops at x = m.width-1 (the
pin glyph's cell) and body stops at `panelBodyX` on every row of the span.
**Goal:** one pure function of the Model returns the panel's stops in paint order, each with its row `y`, its
`panelTarget` and an identity (section header; sub-agent `RunKey`; changed-file path; task card; usage);
rows with no target are not stops and adjacent rows sharing one target are one stop; click and hover
resolve body rows through it, so every stop's target equals the click at its row.
**Approach (assumed at the header base):** new `internal/tui/panelstops.go` with `panelStops() []panelStop`,
built from `panelLayout` and the source rows (`SubAgentRow.Run`, `FileRow.Path/.CallID`, the card entry)
rather than the painted `[]string`; `panelTargetAt` (`internal/tui/sidepanel.go`) resolves body rows through
it and keeps the x test for the header's pin glyph. Name the file in `internal/tui/doc.go`.
**Files:** internal/tui/panelstops.go; internal/tui/sidepanel.go; internal/tui/sidepanel_test.go; internal/tui/doc.go
**Read first:** internal/tui/sidepanel.go — panelTargetAt, panelBodyTarget, panelLayout, panelSections; internal/tui/sidepanel_test.go — TestSidePanelClickOpensTheRowsDetail, panelRowY, panelBodyX;
internal/tui/pointershape.go — pointerShapeAt
**Tests:** `TestPanelStopsMatchClickTargets` (every stop's target equals `panelTargetAt` on every row of its span —
header stops at x = m.width-1, body stops at `panelBodyX` — every section kind); `TestPanelStopsSkipTargetlessRows` (`+N more`, file row with no CallID); existing
`TestSidePanelClickOpensTheRowsDetail` passes unchanged.
**Acceptance:** `go build ./internal/tui/ && GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestPanelStops|TestSidePanelClick|TestDocMap' ./internal/tui/`
**Commit:** `refactor(tui): derive side-panel stops with identities in paint order`

## 6. Navigation mode spans the transcript and the side panel — ✅ DONE (2026-11-09)

NOTES (2026-11-09): `refreshViewport` now seats the cursor through a new `Model.seatBlockCursor` (blockcursor.go) instead of calling `blockCursor.clamp` directly, so a transcript-standing cursor whose transcript loses every stop is handed to the panel there; the panel region is re-judged at key time by `reseatPanelCursor` (also called from `runViewKey`'s esc so a stale panel cursor is not stepped aside for).
NOTES (2026-11-09): the shared panel-shown builder is `navModel(t, sections, events...) (Model, *fakeEngine)` in navmode_test.go, with `onlySections`, `panelCursorStop` and `transcriptCursorLine` helpers for items 7, 8, 9 and 20.
NOTES (2026-11-09): `⏎` in the panel region is left unclaimed per the plan, so until item 7 it falls through to the prompt (an empty draft sends nothing); `^x` in the panel region inside a Run view therefore reaches the view's own stop until item 8.
NOTES (2026-11-09): consequential edit — internal/tui/panes.go: made necessary by renaming the `block cursor` key-claim rung to `navigation mode` (keyClaimRungs comment names the rung).
NOTES (2026-11-09): consequential edit — internal/tui/sessionview.go: made necessary by the blockCursor gaining a region and a remembered panel stop (the `cursor` field comment said "two plain fields").

**What:** ADR 0111 D1–3 (region moves); `⏎` and anchoring are item 7, `^x` item 8. Depends on item 5.
**Regression guard.** the existing cursor fixtures (modelWithRunningFanOut, modelWithToolBlock, newTestModel) run with the panel hidden, so navmode_test.go gains one shared builder with the side panel shown (panelModel-style, wide enough to dock, with an engine handle) that items 6, 7, 8, 9 and 20 reuse.
`blockCursorRow`, `highlightBlockCursor`, `toggleAtBlockCursor`, `cursorRunHead` and `followBlockCursor` gate on
region == transcript; `⏎` and `^x` in the panel region stay unclaimed until items 7 and 8. The panel highlight paints
only while `m.blockCursorOwnsKeys()`, as the transcript bar does. Inside a Run view transcript-region `esc` still goes
one level up (`upRun` drops the mode); only panel-region `esc` leaves just the mode — this supersedes, for the panel
region only, the run-view claimant comment in `model.go` ("one press, one level") and `runViewKey`'s "the one place it
steps aside" comment in `runview.go`, both rewritten to say so. `blockCursorKey` re-resolves the panel region against
`panelStops()` at its top, so a region losing every stop (e.g. the sub-agents linger ending) hands off or ends before
any claim, not in `clamp` alone.
**Goal:** in navigation mode `→` moves the cursor to the panel (the stop nearest the row it left, or the
remembered one) and `←` back to the transcript's remembered row; `↑`/`↓`/`alt+↑`/`alt+↓` move within the
current region, clamped; `alt+↑`/`alt+↓` from the prompt land in the panel when the transcript has no stop;
`esc` leaves the mode from either region, also inside a Run view; `←`/`→` never move the prompt caret while
the mode is on; `→` into a panel with no stops does nothing; a region losing every stop hands the cursor to
the other, else the mode ends; the panel cursor row wears the selection tone; the key-claim rung reads
`navigation mode`.
**Approach (assumed at the header base):** extend `blockCursor` (`internal/tui/blockcursor.go`) with a region,
a panel stop identity plus a fallback hint and the remembered transcript line; `blockCursorKey` claims
`left`/`right` and routes by region; `blockCursor.clamp` and `moveBlockCursor` honour panel stops; the panel
highlight paints in `sidePanelColumn`. `runViewKey` (`internal/tui/runview.go`) steps aside for `esc` while
the region is the panel. Rename the `transcriptClaimants` entry in `internal/tui/model.go`.
**Files:** internal/tui/blockcursor.go; internal/tui/sidepanel.go; internal/tui/model.go; internal/tui/runview.go; internal/tui/keyclaim_test.go; internal/tui/navmode_test.go
**Read first:** internal/tui/blockcursor.go — blockCursorKey, blockCursor.clamp, blockCursorOwnsKeys, toggleAtBlockCursor, cursorRunHead;
internal/tui/runview.go — runViewKey, upRun; internal/tui/sidepanel.go — foldPanelLinger
**Tests:** new `internal/tui/navmode_test.go`: `TestNavModeRightEntersPanelLeftReturns`, `TestNavModeEntryLandsInPanelWithoutTranscriptStops`,
`TestNavModeArrowsLeaveThePromptCaret`, `TestNavModeEscLeavesFromEitherRegion` (inside a Run view: transcript esc goes
one level up, panel esc leaves only the mode), `TestNavModeRightIntoEmptyPanelDoesNothing`, `TestNavModeRegionLosingStops`
(incl. a panel-only change such as the linger end, judged at the next key), `TestNavModePanelKeysLeaveTheTranscriptRow`
(`⏎`/`^x` in the panel touch no remembered transcript block), `TestNavModePanelHighlightYieldsToADecision` (under
`armApproval`); `TestKeyClaimOrderMatchesTheDocumentedPrecedence`; `TestRunViewEscGoesOneLevelUp` still passes.
**Acceptance:** `go build ./internal/tui/ && GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestNavMode|TestKeyClaimOrder|TestBlockCursor|TestRunViewEsc' ./internal/tui/`
**Commit:** `feat(tui): navigation mode moves between the transcript and the side panel`

## 7. `⏎` on panel stops acts as the click; the panel cursor anchors to identity — ✅ DONE (2026-11-09)

NOTES (2026-11-09): the paint path already passed `belowSlot` into `sidePanelColumn` → `panelCursorRow(rows)` → `panelStops(rows)` (item 6), so no paint change was needed; ⏎ follows through the new `Model.activatePanelCursor` (blockcursor.go), which calls `followPanelTarget` and ends the mode only for a live, non-pin target. The vanished-row fallback keeps item 6's `panelY` hint, now restricted to the cursor's own section (`nearestPanelStop` gained a candidate filter), then the column's first stop.
NOTES (2026-11-09): `TestNavModePanelKeysLeaveTheTranscriptRow` (item 6) now walks the panel cursor onto a header before ⏎, since ⏎ on a body stop now legitimately opens that stop's detail; its assertion (no remembered transcript row acted on) is unchanged.

**What:** ADR 0111 D4–5. Depends on item 6 (and item 1's gate, which `followPanelTarget` now carries).
**Regression guard.** The paint path passes the rows `View` already has (`belowSlot`, into `sidePanelColumn`) to
`panelStops(rows)`; only keys and the pointer derive the rows from `frameSpans()`, so no frame recomposes its overlays
to find the cursor. `TestNavModeEnterRefusedUnderModal` asserts only that no panel target was followed (no `pushView`,
no usage pane, no section-mode write) — under approval or ask `⏎` answers the pane, under the other modals a
`modalClaim` rung takes it — and names item 1's gate, not item 7, as what it pins.
**Goal:** `⏎` on each panel stop kind does exactly what a click on its row does, through one call path;
`⏎` on a section header toggles its pin and keeps the mode; a `⏎` that opens a detail (Run view, `/usage`,
tool block) ends the mode; a row appearing or vanishing above the panel cursor leaves it on its row; when
its own row vanishes it lands on the nearest stop in the same section, else the panel's first stop.
**Approach (assumed at the header base):** the panel branch of `blockCursorKey` resolves the cursor's identity
against `panelStops()` on each key and paint, and calls `followPanelTarget` with that stop's target; header
stops carry `panelTargetPin`. No stashed y.
**Files:** internal/tui/blockcursor.go; internal/tui/panelstops.go; internal/tui/sidepanel.go; internal/tui/navmode_test.go
**Read first:** internal/tui/sidepanel.go — followPanelTarget, setPanelSectionMode; internal/tui/blockcursor.go — blockCursorKey;
internal/tui/runview.go — pushView (drops cursor), openRunAt; internal/tui/model.go — frameSpans, View; internal/tui/mouse_test.go — clickCell
**Tests:** `TestNavModeEnterMatchesClickPerKind` (one subtest per kind: sub-agent, changed file, tasks, usage,
header pin — each compared to `clickCell` on the same row); `TestNavModeEnterRefusedUnderModal` (no panel target
followed; pins item 1's gate);
`TestNavModePanelCursorAnchorsToRow` (row added above; own row removed → same-section neighbour → first stop).
**Acceptance:** `go build ./internal/tui/ && GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestNavMode|TestSidePanelClick' ./internal/tui/`
**Commit:** `feat(tui): enter on a side-panel stop acts as its click`

## 8. `^x` on a panel sub-agent row stops that run — ✅ DONE (2026-11-09)

NOTES (2026-11-09): runViewKey's `ctrl+x` step-aside now asks `m.reseatPanelCursor().cursorRunHead()` (the same re-judging its `esc` case and blockCursorKey already do), so the view steps aside for exactly the row blockCursorKey then stops. cursorRunHead's entry lookup is factored into `runHeadAt`, which both the transcript and panel branches share.
NOTES (2026-11-09): the watch-mode test drives a real watch (enterWatchOn + journal emit + pollWatch) with the panel shown, rather than flipping `m.watch.isActive`. A third subtest pins that `^x` on a sub-agents header stops nothing.

**What:** ADR 0111 D3's `^x`. Depends on item 7.
**Regression guard.** The test fixture is item 6's shared panel-shown builder (as `panelModel`: auto, 140 cols,
`foldEvent`, with a held `*fakeEngine`), never `modelWithRunningFanOut` or `modelViewingStampedChild` (panel hidden at
width 80; `stampedDelegation` leaves `m.panel` empty). Fold `panelDelegationCall` + `delegationPhase(Started)` so the
head is stoppable, then assert `eng.stoppedChildren()`. The Run-view case opens a sibling's view, since the viewed
run's own row is no stop (`runOpenableAt`).
**Goal:** `^x` with the cursor on a panel sub-agent row stops that run, at top level and inside a Run view,
as `^x` on a transcript delegation row does; elsewhere in the panel `^x` keeps its prompt meaning; in watch
mode it is refused as the transcript `^x` is.
**Approach (assumed at the header base):** make `cursorRunHead` (`internal/tui/blockcursor.go`) answer for a
panel sub-agent stop, so `runViewKey`'s `ctrl+x` step-aside (`internal/tui/runview.go`) follows.
**Files:** internal/tui/blockcursor.go; internal/tui/runview.go; internal/tui/navmode_test.go
**Read first:** internal/tui/blockcursor.go — cursorRunHead, blockCursorKey; internal/tui/runview.go — runViewKey, runOpenableAt;
internal/tui/sidepanel_test.go — panelModel, panelDelegationCall, delegationPhase; internal/tui/runview_test.go — TestRunViewCtrlXStopsTheCursorsRowInsteadOfTheView
**Tests:** `TestNavModeCtrlXStopsPanelSubAgent` (top level and inside a sibling's Run view, on item 6's panel-shown
builder with `panelDelegationCall` + `delegationPhase`, `keyCtrlX`, asserting `eng.stoppedChildren()`);
`TestNavModeCtrlXInWatchModeRefused`.
**Acceptance:** `go build ./internal/tui/ && GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestNavModeCtrlX|TestRunView' ./internal/tui/`
**Commit:** `feat(tui): ctrl+x on a side-panel sub-agent row stops that run`

## 9. Mark both cursors with a non-colour `▌` — ✅ DONE (2026-11-09)

NOTES (2026-11-09): both regions paint through one new helper, `markCursorRow` (blockcursor.go) — `▌` over the row's first cell and the selection tone across the rest; the panel hands it its gutter cell plus content, so the separator (and the grip hit test) is untouched. The mark is rendered in the selection tone with the row.
NOTES (2026-11-09): `blockCursorKey`'s doc comment no longer says `^x` in the panel "is not the mode's and goes on to the frame" (false since item 8); it now says `^x` on a panel sub-agent row stops that run.
NOTES (2026-11-09): navmode_test.go's `navModel` now delegates to a new `navModelWithGlyphs` fixture (same construction plus `opts.UI.Glyphs`), used by `TestCursorMarkUnderSafeGlyphs`; the shared assertions `assertTranscriptCursorMark` / `assertPanelCursorMark` live beside it. Bite-checked: both new tests fail with the mark replaced by a blank.
NOTES (2026-11-09): no other View-reading cursor-row test needed a change — the eight tests in blocktarget/mouse/runview/subagentblock_test.go that enter the mode were run and pass; `TestNavModePanelHighlightYieldsToADecision` passes unchanged (the mark is painted in the same selection render).

**What:** fix the defect `cursor-colour-only-mark` (ADR 0052 D4), ADR 0111 D6. Depends on item 6.
**Regression guard.** Every test reading the painted `View` row the cursor stands on is updated for the column-0 `▌`
(`grep -n 'blockCursorRow()' internal/tui/*_test.go`, the View-reading ones only) —
`TestBlockCursorHighlightsPaddedPromptRow` then reads `▌ alpha`. `highlightBlockCursor`'s doc comment ("The bar is the
theme's SELECTION field") is rewritten to name the column-0 `▌` beside the selection tone.
**Goal:** under an Ascii colour profile the cursor row is still visibly marked: the transcript row carries
`▌` in column 0 and the panel row carries `▌` in its gutter cell right of `│`; the glyph is the same under
`ui.glyphs: safe`; nothing re-wraps.
**Approach (assumed at the header base):** a fixed constant `glyphCursorMark` in `internal/tui/theme.go`
(beside the other fixed half blocks); `highlightBlockCursor` (`internal/tui/blockcursor.go`) overwrites cell 0
of the cursor row after shading; `sidePanelColumn` (`internal/tui/sidepanel.go`) paints the gutter cell on
the panel cursor row (the grip hit test is unchanged).
**Files:** internal/tui/theme.go; internal/tui/blockcursor.go; internal/tui/sidepanel.go; internal/tui/blockcursor_test.go; internal/tui/colorprofile_test.go; internal/tui/navmode_test.go
**Read first:** internal/tui/blockcursor.go — highlightBlockCursor, blockCursorRow; internal/tui/sidepanel.go — sidePanelColumn, panelGripAt;
internal/tui/theme.go — glyphSet; internal/tui/blockcursor_test.go — TestBlockCursorHighlightsTheRowItStandsOn, TestBlockCursorHighlightsPaddedPromptRow;
internal/tui/colorprofile_test.go — withPrompt
**Tests:** `TestBlockCursorHighlightsTheRowItStandsOn` and `TestBlockCursorHighlightsPaddedPromptRow` (`▌ alpha`) updated
for the column-0 mark, with every other View-reading cursor-row test;
`TestCursorMarkSurvivesAsciiProfile` (transcript and panel, `tea.ColorProfileMsg{Profile: colorprofile.Ascii}`,
`strip`); `TestCursorMarkUnderSafeGlyphs`.
**Acceptance:** `go build ./internal/tui/ && GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestBlockCursor|TestCursorMark|TestColorProfile' ./internal/tui/`
**Closes:** cursor-colour-only-mark
**Commit:** `fix(tui): mark the cursor row with a glyph as well as colour`

## 10. Key legend and manual name navigation mode — ✅ DONE (2026-11-09)

NOTES (2026-11-09): the navigation keys are one legend cell (`helpKeyNavigate`, built from `helpKeyStopRun`), appended after `PgUp/PgDn scroll`, because the in-mode keys mean something else outside the mode; configuration.md §The side panel gains a **The keyboard.** paragraph after **From the prompt.**, and its "no key binding" sentence now reads that the width has no key of its own.

**What:** user-facing docs for items 6–9. Depends on items 6–9.
**Regression guard.** Every manual sentence that lists the `/help` legend gains the navigation cell
(`grep -rn 'PgUp/PgDn scroll' docs/manual/` — the `/help` row of `docs/manual/commands.md`); and `commands.md`'s
`⌥↑`/`⌥↓` paragraph is the site for `←`/`→`, `⏎` on panel rows and `^x` on a panel sub-agent row (the Approach's
grep reaches only `configuration.md`).
**Goal:** the help key legend shows navigation mode's keys (`⌥↑/⌥↓` enter, `←/→` region, `⏎` act, `^x`
stop, `esc` leave); `docs/manual/commands.md` and `docs/manual/configuration.md` §The side panel say the
panel is reached by keyboard through navigation mode and no longer say it takes no focus or has no key.
**Approach (assumed at the header base):** a legend cell in `internal/tui/help.go` (pinned by
`TestHelpNoteListsEveryVerb` in `help_test.go`); in the manuals, rewrite every sentence that says the panel
is mouse-only, takes no keyboard focus or has no key binding (`grep -n 'keyboard focus\|no key binding\|mouse-only' docs/manual/`).
**Files:** internal/tui/help.go; internal/tui/help_test.go; docs/manual/commands.md; docs/manual/configuration.md
**Read first:** internal/tui/help.go — helpKeyLegend, helpKeyStopRun; internal/tui/help_test.go — TestHelpNoteListsEveryVerb; internal/tui/sidepanel_test.go — panelHelpWords;
docs/manual/commands.md — /help row, ⌥↑/⌥↓ paragraph; docs/manual/configuration.md — §The side panel intro, "The mouse."
**Tests:** `TestHelpNoteListsEveryVerb` and a legend assertion for the navigation cell.
**Acceptance:** `go build ./internal/tui/ && GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestHelp' ./internal/tui/ && ! grep -n 'keyboard focus\|no key binding' docs/manual/commands.md docs/manual/configuration.md`
**Commit:** `docs(manual): describe navigation mode across the transcript and side panel`

## 11. Layout specs and glossary describe navigation mode — ✅ DONE (2026-11-09)

**What:** spec and glossary for items 6–9. Depends on item 10.
**Regression guard.** The rewrite rule is every sentence describing the block cursor's paint or keys, or the panel as
focus-less, found by `grep -rn 'selection field\|moves a highlight\|block cursor\|keyboard focus\|no key binding'
layout.md docs/layout/ internal/tui/doc.go .capcom/knowledge/glossary/` (`selection tone` matches nothing today).
Acceptance also runs `! grep -rn 'takes no keyboard focus\|never takes keyboard focus' layout.md docs/layout/
.capcom/knowledge/glossary/glossary-identity/side-panel.md`.
**Goal:** `layout.md`, `docs/layout/tool-layout.md` and `docs/layout/side-panel-layout.md` (Pointer, Commands,
a new Keyboard section, the cursor mark) describe navigation mode and no sentence says the panel takes no
keyboard focus; the glossary `side-panel` Note agrees, a new glossary Note `Navigation mode` defines the term,
and `internal/tui/doc.go`'s blockcursor paragraph names it.
**Approach (assumed at the header base):** rewrite by rule — every sentence naming the panel as mouse-only,
focus-less or key-less, and every sentence describing the block cursor's highlight as a tone only
(`grep -rn 'keyboard focus\|no key binding\|selection tone' layout.md docs/layout/ internal/tui/doc.go .capcom/knowledge/glossary/`);
add the Note with `capcom know add --parent glossary-identity`; run `capcom check`.
**Files:** layout.md; docs/layout/tool-layout.md; docs/layout/side-panel-layout.md; internal/tui/doc.go; .capcom/knowledge/glossary/glossary-identity/side-panel.md; .capcom/knowledge/glossary/glossary-identity/navigation-mode.md
**Read first:** docs/layout/side-panel-layout.md — Pointer, Commands; docs/layout/tool-layout.md — "Keyboard: a modal block cursor"; internal/tui/doc.go — blockcursor.go paragraph, sidepanel.go entry;
.capcom/knowledge/glossary/glossary-identity/side-panel.md; layout.md — The side panel; internal/tui/docmap_test.go — TestDocMapNamesEveryFile
**Tests:** none beyond the doc-map test.
**Acceptance:** `capcom check && capcom know show "Navigation mode" && GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestDocMap' ./internal/tui/ && ! grep -rn 'takes no keyboard focus\|never takes keyboard focus' layout.md docs/layout/ .capcom/knowledge/glossary/glossary-identity/side-panel.md`
**Closes:** panel-keyboard-navigation
**Commit:** `docs(layout): specify navigation mode and the cursor mark`

## 12. Narrow-window overlay: geometry, paint and `→`

**What:** ADR 0111 D7, opening by `→`. Depends on item 6.
**Regression guard.** panelStops and panelTargetAt resolve against the paint geometry (docked or overlay), so item 12 moves panelCell's panelShown()/slotWidth() gate onto that geometry and adds a test that every stop's target equals the click at its row while the overlay is shown — item 12 depends on item 5.
The overlay's width is `min(configuredPanelWidth(), m.width)`; `clampPanelWidth` stays the docked/drag clamp only (at
100 cols under `auto` it returns −1). A hidden panel offers no entry stops to `⌥↑`/`⌥↓` — the overlay opens only by
`→` or bare `/panel` — pinned in a test. `TestOverlayLeavesTranscriptLinesByteIdentical` and `TestOverlayHasNoGrip`
each first assert the overlay is painted (a panel header at x ≥ m.width−ow in the View) before their own claim.
**Goal:** at a window under the panel's gate with `ui.side-panel.show` not `off`, `→` in navigation mode
paints the panel over the transcript's right edge at the panel's width clamped to the window, with the
cursor on a panel stop; the transcript's wrapped lines are byte-identical before and after; the overlay
cannot be resized; under `off`, `→` leaves the screen unchanged.
**Approach (assumed at the header base):** no new flag: overlay shown ⇔ cursor active ∧ panel region ∧
`!panelShown()` ∧ show not `off` ∧ `blockCursorOwnsKeys()`. Split a paint width (docked or overlay) from
`panelWidth()`, which stays 0 under the overlay so `slotWidth`, `layout()` and `frameKey` do not change;
`panelContentWidth`, `panelCell` (also used by the wheel) and hover read the paint geometry; `panelGripAt`
refuses in the overlay; `View` composes it via `joinColumn(rows, m.width-ow, …)` over every row above ▔.
**Files:** internal/tui/sidepanel.go; internal/tui/model.go; internal/tui/blockcursor.go; internal/tui/sidepaneloverlay_test.go
**Read first:** internal/tui/sidepanel.go — panelWidth, clampPanelWidth, panelCell, panelTargetAt, panelGripAt; internal/tui/model.go — View, panelShown;
internal/tui/boxdraw.go — joinColumn
**Tests:** new `internal/tui/sidepaneloverlay_test.go`: `TestOverlayOpensOnRightAtNarrowWidth` (100 cols, `auto`),
`TestOverlayLeavesTranscriptLinesByteIdentical`, `TestOverlayHasNoGrip` (both first assert the overlay is painted),
`TestOverlayOffDoesNothing`, `TestOverlayStopsMatchClickTargets` (every stop's target equals the click at its row while
the overlay shows), `TestOverlayNotOpenedByAltArrows` (80 cols, no transcript stop: `⌥↑` stays a no-op).
**Acceptance:** `go build ./internal/tui/ && GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestOverlay|TestSidePanel' ./internal/tui/`
**Commit:** `feat(tui): open the side panel as an overlay on a narrow window`

## 13. Overlay close paths and bare `/panel`

**What:** ADR 0111 D7's close and command paths, plus docs. Depends on item 12.
**Regression guard.** Bare `/panel` enters navigation mode only when `sidePanelLeastWindow(show)` is ok ∧ `!panelShown()` ∧
`panelStops()` is non-empty; otherwise it prints the report alone, so under `off`, the zero UIPrefs or a panel with
nothing live it parks no invisible cursor.
**Goal:** `←`, `esc` and a click outside the overlay close it (the outside click passes through and ends the
mode); clicks inside act as on the docked panel; a `⏎` that opens a detail closes it; bare `/panel` on a
narrow window opens it in navigation mode with the cursor on the first stop and still prints its report;
the manuals and layout specs describe the overlay.
**Approach (assumed at the header base):** close follows from item 12's derived predicate; `handleMouseClick`
(`internal/tui/mouse.go`) ends the mode on a click outside the overlay, then continues; `runPanelCommand`'s
`panelReport` case enters navigation mode on the panel's first stop when the panel is not drawn; reword
`panelReportNote` only if it promises otherwise. Docs: the "When it shows" and Commands sections of
`docs/layout/side-panel-layout.md`, the `/panel` row of `docs/manual/commands.md`, `docs/manual/configuration.md`
§The side panel, `layout.md` §side panel.
**Files:** internal/tui/mouse.go; internal/tui/sidepanel.go; internal/tui/sidepanel_test.go; internal/tui/sidepaneloverlay_test.go; docs/layout/side-panel-layout.md; docs/manual/commands.md; docs/manual/configuration.md; layout.md
**Read first:** internal/tui/sidepanel.go — runPanelCommand, panelReportNote, sidePanelLeastWindow, handleSidePanelClick; internal/tui/mouse.go — handleMouseClick;
internal/tui/sidepanel_test.go — TestSidePanelCommandBareReports, noteHolding, sidePanelModel
**Tests:** `TestOverlayClosesOnLeftEscAndOutsideClick`, `TestOverlayInsideClickActsAsDocked`,
`TestSidePanelCommandBareReports` updated (at 100 cols: report printed and overlay open on the first stop,
asserting the exact report text the command emits; `off` and nothing-live subtests assert `!m.cursor.active`).
**Acceptance:** `go build ./internal/tui/ && GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestOverlay|TestSidePanelCommand' ./internal/tui/`
**Closes:** panel-narrow-overlay
**Commit:** `feat(tui): close the side-panel overlay and open it from bare /panel`

## 14. `MCPServerEvent` joins the Event vocabulary and the journal

**What:** ADR 0111 D8's type; nothing emits it yet.
**Regression guard.** The amend-by-rule covers every count of line kinds, default-stream variants or Event variants, found
by `grep -rn 'twenty-[a-z]*' internal/eventjson cmd/apogee docs/manual .capcom/knowledge/decisions/adr-0075*` (it
replaces the Approach's grep, which misses `cmd/apogee/headless_test.go`'s "twenty-five default-stream variants").
**Goal:** `domain.MCPServerEvent{EventBase; Server, Transport string; Project bool; Phase MCPServerPhase;
Tools int; Cause string; Kept bool; Reason MCPDisconnectReason}` exists with phases `dialling|connected|failed|disconnected`
and reasons `removed|denied`, aliased in the `apogee` facade, encoded and decoded by `internal/eventjson` as
kind `mcp_server` (snake_case members) with a round trip; the headless manual lists the kind and says only
the TUI's journal writes it; ADR 0075 carries a dated amendment naming the new kind.
**Approach (assumed at the header base):** follow items 1 and 3 of the archived side-panel plan: `Kinds()`
entry before the frames, `encode` case, `mcpServerData`, `eventDecoders` entry, header-comment counts; its
`EventBase` is zero (documented). Update every test pinning the kind count or Event list (`TestKindsAreTwentyEight`
→ `TwentyNine`, `TestEncodeMapsEveryEventVariant`, `TestEncodeJSONGolden`, `roundTripEvents`,
`TestEveryDomainEventVariantIsAliased`, `TestHeadlessFormatJSONStreamsEveryEvent`, `TestManualListsEveryEventLineKind`,
`foldCases` in `internal/tui/fold_test.go` as a no-op row). Amend by rule: every count of line kinds or engine
Events (`grep -rn 'twenty-\(six\|seven\|eight\)' internal/eventjson docs/manual .capcom/knowledge/decisions/adr-0075*`).
Add Note `adr-0075-amend-MMDD` (run date; letter suffix if taken), named on ADR 0075's Status line and reading list.
**Files:** internal/domain/events.go; apogee.go; internal/eventjson/encode.go; internal/eventjson/decode.go; internal/eventjson/encode_test.go; internal/eventjson/decode_test.go; cmd/apogee/headless_test.go; internal/tui/fold_test.go; docs/manual/headless.md; .capcom/knowledge/decisions/adr-0075.md; .capcom/knowledge/decisions/adr-0075/
**Read first:** internal/eventjson/encode.go — Kinds, Encode; internal/eventjson/decode.go — eventDecoders; internal/eventjson/encode_test.go — TestKindsAreTwentyEight, TestEncodeJSONGolden;
cmd/apogee/headless_test.go — TestHeadlessFormatJSONStreamsEveryEvent; cmd/apogee/docs_eventlines_test.go — TestManualListsEveryEventLineKind;
internal/tui/fold_test.go — foldCases
**Tests:** the pinned tests above, plus a golden for `failed` with `kept: true`.
**Acceptance:** `go build ./... && GOMEMLIMIT=2GiB go test -race -count=1 ./internal/eventjson/ && GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestEveryDomainEventVariantIsAliased' . && GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestHeadlessFormatJSON|TestManualListsEveryEventLineKind' ./cmd/apogee/ && GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestFoldEventCoversEveryEventVariant' ./internal/tui/ && capcom check`
**Commit:** `feat(domain): add the MCP server lifecycle Event`

## 15. Fold MCP server state for every Driver view

**What:** the Driver-neutral fold ADR 0111 D9–10 read. Depends on item 14.
**Regression guard.** an MCPServerEvent reaching the TUI's eventMsg path is folded into the MCP fold but never counts as engine activity (noteEngineHeard and any busy/progress or turn state stay untouched), with a test.
Item 15 also defines the seed entry point — a Model method folding `[]domain.MCPServerEvent` into the fold — with a
unit test; item 17 folds its seed through it.
**Goal:** `internal/sidepanel` holds an `MCPServers` fold (`Fold`, `Rows`, `IsLive`) keyed by server name:
last phase, transport, source, tool count, cause, kept; `disconnected/removed` drops the row; `IsLive` is true
while any row is not `connected`. The TUI routes `MCPServerEvent` into it from live, watched and seeded
streams; the fold survives `/clear` and a resume, and watch mode swaps in the watched process's own fold.
**Approach (assumed at the header base):** new `internal/sidepanel/mcp.go`; a field on `sessionView`
(`internal/tui/sessionview.go`) that `resetSessionView` and `resumeLoaded` never write
(`TestSessionViewHoldsEveryFieldTheBoundariesReset` allows it); route in `internal/tui/fold.go` (the no-op
row of item 14 becomes a fold); name `mcp.go` in `internal/sidepanel/doc.go`.
**Files:** internal/sidepanel/mcp.go; internal/sidepanel/mcp_test.go; internal/sidepanel/doc.go; internal/tui/model.go; internal/tui/sessionview.go; internal/tui/fold.go; internal/tui/fold_test.go; internal/tui/watch_test.go
**Read first:** internal/tui/model.go — Update eventMsg case, noteEngineHeard; internal/tui/fold.go — foldEventAgainst, foldSidePanel;
internal/tui/sessionview.go — sessionView.panel (copy-on-write note); internal/tui/commandrun.go — resetSessionView; internal/tui/watch.go — swapSessionView, foldWatchBatch
**Tests:** `TestMCPServersFold` (each phase, kept, removed drops, IsLive); `TestMCPFoldSurvivesClearAndResume`;
`TestWatchFoldsMCPServerLines` (journal `mcp_server` line reaches the watched fold); `TestMCPFoldEventIsNotEngineActivity`
(a mid-Turn `MCPServerEvent` leaves `m.lastEvent` and busy/turn state unchanged); `TestMCPFoldSeedsFromASnapshot`.
**Acceptance:** `go build ./internal/sidepanel/ ./internal/tui/ && GOMEMLIMIT=2GiB go test -race -count=1 ./internal/sidepanel/ && GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestMCPFold|TestWatchFoldsMCP|TestFoldEvent|TestSessionView' ./internal/tui/`
**Commit:** `feat(sidepanel): fold MCP server lifecycle state`

## 16. The composition root emits MCP lifecycle Events

**What:** ADR 0111 D8's emission, by the ratified seam. Depends on item 14.
**Regression guard.** item 16 fixes the outbox's locking contract once — append order under liveMCP.mu, a separate emit-order lock held while draining and while item 17 writes a journal seed, emit never under liveMCP.mu nor on Update — and states it in a comment on the outbox type; item 17 builds its seed on that contract and changes no lock.
`TestRootWiringEmitsThroughTheHookRunner` (`cmd/apogee/wire_settings_test.go`) unwraps the serial sink before it
checks the journal → stats → Runner chain. Emission is a rule: wherever a server's live entry is dropped or its pending
dial superseded with nothing replacing it (`grep -n 'dropLiveLocked\|supersedeLocked' cmd/apogee/wire_mcp.go` —
`requestServerLocked`'s `isDropped` drop and `!isAdmitted && wasAdmittedAsIs` supersede included). The serialiser's
idempotence is pinned by `TestSerializeEventsReusesTheEngineSink` (wrapping twice returns the same sink; `New` reuses it).
**Goal:** `cmd/apogee`'s MCP dial owner emits `MCPServerEvent` through the same serialised sink the engine
uses: `dialling` when a dial starts, `connected(tools)` or `failed(cause, kept)` when the current-generation
dial finishes (before the idle-point commit), `disconnected(removed|denied)` on removal or a url-safety/
admission deny, `connected` with the live count when a reverted edit supersedes a dial; a stale or never-
started dial emits nothing; Events leave in state order, never under `liveMCP.mu`, never on Update; the
glossary `Event` Note says a Driver's composition root may emit an Event for a fact it owns.
**Approach (assumed at the header base):** export the idempotent serialiser from `internal/agent/construct.go`
as `apogee.SerializeEvents`; `wire_boot.go` wraps `w.events` once and keeps the handle; `liveMCP`
(`cmd/apogee/wire_mcp.go`) appends to an ordered outbox under `mu` at `startDial`, `storeDial`, `disconnectLocked`,
`dropDenied`/`retire`, the commit re-admit deny and the supersede case; one goroutine drains it outside `mu`.
Wire it in `cmd/apogee/wire_live.go`.
**Files:** internal/agent/construct.go; internal/agent/construct_test.go; apogee.go; cmd/apogee/wire_boot.go; cmd/apogee/wire_mcp.go; cmd/apogee/wire_live.go; cmd/apogee/wire_mcp_test.go; cmd/apogee/wire_settings_test.go; .capcom/knowledge/glossary/glossary-identity/event.md
**Read first:** cmd/apogee/wire_mcp.go — startDial, storeDial, requestServerLocked, dropLiveLocked, supersedeLocked; internal/agent/construct.go — serializedEvents;
cmd/apogee/wire_boot.go — resolveConfig (w.events, w.cfg.Events); cmd/apogee/wire_settings_test.go — TestRootWiringEmitsThroughTheHookRunner
**Tests:** `TestLiveMCPEmitsLifecycleEvents` (recording sink: reconnect, failed with and without kept, removed,
denied, reverted edit, a closed session or cancelled dial in `requestServerLocked`, stale result silent, order);
`TestLiveMCPNeverEmitsUnderLock`; `TestSerializeEventsReusesTheEngineSink`; `TestRootWiringEmitsThroughTheHookRunner`
updated to unwrap the serial sink.
**Acceptance:** `go build ./... && GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestLiveMCP|TestMCP|TestRootWiringEmitsThroughTheHookRunner' ./cmd/apogee/ && GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestSerializeEventsReusesTheEngineSink' ./internal/agent/ && capcom check`
**Commit:** `feat(apogee): emit MCP server lifecycle Events from the dial owner`

## 17. Seed MCP state at startup and at each journal's birth

**What:** completes ADR 0111 D8 (startup dials before any listener; the journal is born late and rotates on
`/clear`). Depends on items 15 and 16.
**Regression guard.** The seed builds on item 16's outbox contract and changes no lock: it takes the emit-order lock and
builds from the outbox's own last-queued per-server state, never taking `liveMCP.mu` while holding an outbox lock (the
Approach's "outbox lock" is that emit-order lock). A repo server skipped at startup reaches the seed through a
seed-only list returned from `connectMCPServers` and kept outside `m.servers`, so a later request with that config
still dials it. The state method is `stateEvents()` (`liveMCP.snapshot()` already exists). Seeding through `MCPHost`
means `fakeMCPHost` (`internal/tui/mcpreconnect_test.go`) gains the method. The TUI half is checked by an
`internal/tui` unit test (seeded `fakeMCPHost` → Model fold, through item 15's seed entry point); e2e asserts only the
journal file.
**Goal:** after startup the TUI's MCP fold holds every configured server's state (a repo server skipped for a
moved pin seeded `failed` with its cause), and every journal — fresh, resumed or rotated by `/clear` — carries
the current per-server state right after `run_started`, before any live `mcp_server` line; a startup connect,
a settings reconnect and a failed dial are each observable in the TUI and the journal; a dial that finishes
mid-Turn shows its Event before the idle-point commit.
**Approach (assumed at the header base):** `liveMCP.snapshot() []MCPServerEvent`; the TUI receives it at
construction through `MCPHost` (`internal/tui/mcpreconnect.go`) and folds it through item 15's fold; the
journal seed is written at `openEventJournalLocked` (`cmd/apogee/wire_session.go`) under the outbox lock
before `swap`, so no live line interleaves.
**Files:** cmd/apogee/wire_mcp.go; cmd/apogee/mcp_cmd.go; cmd/apogee/wire_session.go; cmd/apogee/wire_live.go; internal/tui/mcpreconnect.go; internal/tui/mcpreconnect_test.go; cmd/apogee/e2e_mcpreconnect_test.go; cmd/apogee/e2e_mcpmidturn_test.go; .capcom/knowledge/glossary/glossary-identity/event-journal.md
**Read first:** cmd/apogee/wire_session.go — openEventJournalLocked, liveJournalSink.swap; cmd/apogee/mcp_cmd.go — connectMCPServers, announceSkippedMCP;
cmd/apogee/wire_mcp.go — newLiveMCP, snapshot; internal/tui/mcpreconnect.go — MCPHost; internal/tui/mcpreconnect_test.go — fakeMCPHost
**Tests:** e2e, asserting the journal file: startup connect seeded; settings reconnect emits `dialling` then `connected`;
failed dial carries cause; mid-Turn finish emits before commit; `/clear` rotation reseeds; unit:
`TestMCPSeedReachesTheTUIFold` (seeded `fakeMCPHost`, a skipped repo server seeded `failed`) in `internal/tui`.
**Acceptance:** `go build ./... && GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestE2EMCP|TestMCPSeed' ./cmd/apogee/ && GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestMCPSeed|TestMCPReconnect' ./internal/tui/ && capcom check`
**Closes:** mcp-lifecycle-event
**Commit:** `feat(apogee): seed MCP server state into the TUI and each journal`

## 18. `/mcp`, a read-only report pane

**What:** ADR 0111 D9. Depends on item 15.
**Regression guard.** `"mcp"` joins `wantParsed` (`TestCommandTableDrivesParserAndMenu`) and
`TestOnlyResetAndPureUIVerbsAreNotRecallable`'s want in `internal/tui/command_test.go`. Ranks: `paneMCP` key 10 (after
advice 9), pointer 6, and the five panes after it in the pointer chain (browser, picker, workflows, prompt, dropdown)
shift +1, which `TestPointerPanesWalkInTheClickChainOrder` (`internal/tui/mouse_test.go`) pins. `watchVerbs` lives in
`internal/tui/watch.go`, where `"mcp"` is added. `/m` plus tab or enter now accepts `/mcp` (`/model` needs `/mo`) and a
skill named `mcp` is reachable only mid-line: the item's CHANGELOG entry says so, and item 21's `/mcp` row states it.
**Goal:** `/mcp` opens a non-modal, scrollable report pane (`esc` closes) listing every server in the fold
with transport, source (`global`/`project`), state word, tool count and, for a failed one, its cause
(`failed (previous connection kept)` when kept); it updates while open; it works while a Turn runs and in
watch mode; with no servers it says so.
**Approach (assumed at the header base):** copy `/usage` through `internal/tui/reportpane.go`: `mcpReport`
kind, `paneMCP` in the `framePane` enum (`model.go`), `mcpPane reportPane` on `Model`, a `reportPaneRow` in
`panes.go`, content in new `internal/tui/mcppane.go` derived at render from the fold, a `commandSpecs` row in
`command.go` (`whileRunning`, `noRecall`, `bareVerb`), and `"mcp"` in `watchVerbs`. Name `mcppane.go` in `doc.go`.
**Files:** internal/tui/mcppane.go; internal/tui/mcppane_test.go; internal/tui/reportpane.go; internal/tui/panes.go; internal/tui/model.go; internal/tui/command.go; internal/tui/watch.go; internal/tui/doc.go; internal/tui/keyclaim_test.go; internal/tui/command_test.go; internal/tui/mouse_test.go
**Read first:** internal/tui/reportpane.go — reportRows, reportKinds; internal/tui/panes.go — paneSpecs, reportPaneRow; internal/tui/command.go — commandSpecs;
internal/tui/watch.go — watchVerbs; internal/tui/command_test.go — TestOnlyResetAndPureUIVerbsAreNotRecallable; internal/tui/mouse_test.go — TestPointerPanesWalkInTheClickChainOrder
**Tests:** `TestMCPPaneListsEveryServer` (driving `/mcp` typed at the prompt; exact rows), `TestMCPPaneUpdatesWhileOpen`,
`TestMCPPaneStripsEscapes`, `TestMCPPaneInWatchMode`; `TestKeyClaimOrderMatchesTheDocumentedPrecedence`,
`TestPointerPanesWalkInTheClickChainOrder`, `TestHelpNoteListsEveryVerb`, `TestCommandTableDrivesParserAndMenu` and
`TestOnlyResetAndPureUIVerbsAreNotRecallable` (both gain `"mcp"`).
**Acceptance:** `go build ./internal/tui/ && GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestMCPPane|TestKeyClaimOrder|TestPointerPanes|TestHelpNote|TestReportKinds|TestDocMap|TestCommandTableDrivesParserAndMenu|TestOnlyResetAndPureUIVerbs' ./internal/tui/`
**Commit:** `feat(tui): add the /mcp report pane`

## 19. `mcp` joins the side-panel section names

**What:** ADR 0111 D10's config half.
**Regression guard.** `TestSidePanelCommandBareReports`'s pinned bare `/panel` note gains ` · mcp auto` (`panelReportNote` walks
the full resolved list); the unknown-section refusal tests (`internal/tui/sidepanel_test.go`'s `/panel` refusal and
`TestSidePanelSectionsRefuseEveryBadList` in `internal/domain/sidepanelprefs_test.go`) pin the grown known list
"…, usage, mcp".
**Goal:** `ui.side-panel.sections` accepts `mcp`, last in the default order in `auto`; a list that does not
name it loads with `mcp: auto` appended; `/panel pin|unpin|hide mcp` is accepted; the TUI renders the `mcp`
section as empty and never live until item 20 fills it.
**Approach (assumed at the header base):** `SidePanelMCP` in `internal/domain/sidepanelprefs.go`, appended to
`sidePanelSectionNames` and `DefaultSidePanelSections`; registry help text and the defaults
`config.yaml` comments. Tests that use `"mcp"` as their unknown-section example switch to another name.
**Files:** internal/domain/sidepanelprefs.go; internal/domain/sidepanelprefs_test.go; internal/config/registry.go; internal/config/defaults/config.yaml; internal/config/config_test.go; internal/config/configwrite_sections_test.go; cmd/apogee/wire_options_test.go; internal/tui/sidepanel.go; internal/tui/sidepanel_test.go
**Read first:** internal/domain/sidepanelprefs.go — sidePanelSectionNames, DefaultSidePanelSections, sidePanelEntries; internal/config/config.go — fileSidePanelSections;
internal/tui/sidepanel.go — parsePanel, panelReportNote; internal/config/config_test.go — wantSidePanelSectionsDefault; cmd/apogee/wire_options_test.go
**Tests:** sidepanelprefs default/order/append cases with `mcp`; `wantSidePanelSectionsDefault`; written lists
gain `mcp: auto`; the TUI's unknown-section test and `TestSidePanelSectionsRefuseEveryBadList` use a still-unknown
name and pin the known list "…, usage, mcp"; `TestSidePanelCommandBareReports` ends " · mcp auto".
**Acceptance:** `go build ./... && GOMEMLIMIT=2GiB go test -race -count=1 ./internal/domain/ && GOMEMLIMIT=2GiB go test -race -count=1 -run 'SidePanel|Sections' ./internal/config/ && GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestWireOptions' ./cmd/apogee/ && GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestSidePanel' ./internal/tui/`
**Commit:** `feat(config): add mcp to the side-panel section names`

## 20. The `mcp` side-panel section

**What:** ADR 0111 D10's TUI half. Depends on items 5, 15, 18, 19.
**Regression guard.** `panelStop` gains an `mcp` identity, and `⏎` on the `mcp` stop ends navigation mode as `/usage` does
(item 7's detail-opening list grows by `/mcp`); item 7's `TestNavModeEnterMatchesClickPerKind` gains an `mcp` subtest,
and `internal/tui/blockcursor.go`, where the end-mode rule lives, is listed.
**Goal:** the `mcp` section (title `MCP`) shows rows `name · state · N tools` (`1 tool`; count blank while no
session serves), is live while any server is not `connected`, summarises the count not connected (e.g.
`1 failed`), and a click or `⏎` on it opens `/mcp`; names are escape-stripped.
**Approach (assumed at the header base):** a `SidePanelMCP` case in `panelSection` (`internal/tui/sidepanel.go`)
reading item 15's fold; `panelTargetMCP` in `panelBodyTarget` and `followPanelTarget` → `runMCPCommand`; one
stop for the section body in `panelStops()`.
**Files:** internal/tui/sidepanel.go; internal/tui/panelstops.go; internal/tui/blockcursor.go; internal/tui/sidepanel_test.go; internal/tui/navmode_test.go
**Read first:** internal/tui/sidepanel.go — panelSection, panelBodyTarget, panelTargetKind, followPanelTarget; internal/tui/pointershape.go — panel hover branch;
internal/tui/usage.go — runUsageCommand; internal/tui/sidepanel_test.go — TestSidePanelClickOpensTheRowsDetail; internal/tui/mouse_test.go — clickCell
**Tests:** `TestSidePanelMCPSectionRows` (each state, `1 tool`, blank count), `TestSidePanelMCPSectionAuto`
(hidden when all connected), `TestSidePanelMCPClickOpensReport`, `TestNavModeEnterOnMCPOpensReport` (the mode ends); an `mcp` subtest in
`TestNavModeEnterMatchesClickPerKind`.
**Acceptance:** `go build ./internal/tui/ && GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestSidePanelMCP|TestNavModeEnter|TestPanelStops' ./internal/tui/`
**Commit:** `feat(tui): show MCP server state in the side panel`

## 21. Document `/mcp` and the `mcp` section

**What:** docs for items 14–20. Depends on item 20.
**Regression guard.** The rule also covers every list of watch or report verbs in either order and every "four reports"
sentence, by `grep -rn 'sub-agents.*usage\|/thinking.*/inspect\|/inspect.*/thinking\|four reports\|ephemeral'
docs/manual docs/layout docs/design layout.md .capcom/knowledge/glossary/`, excluding `docs/design/archived/` (plans,
reviews and handoffs stay as written). `docs/manual/sessions.md` §While you watch and the glossary `interjection` Note
are in scope; the `/mcp` row states that `/model` now needs `/mo` and a skill named `mcp` is reachable only mid-line.
**Goal:** `docs/manual/commands.md` has a `/mcp` row and the `/panel` section list names `mcp`;
`docs/layout/side-panel-layout.md` (default list, section table, pointer list), `docs/manual/configuration.md`,
`layout.md` (the report-pane lists and key-precedence prose), the glossary `side-panel` and `mcp-client` Notes
and `docs/design/mcp-client.md` describe the Event-fed state, `/mcp` and the section.
**Approach (assumed at the header base):** amend by rule — every list of side-panel sections, every list of
report panes, and every sentence saying MCP outcomes reach the user only as transcript notes
(`grep -rn 'sub-agents.*usage\|/inspect.*/thinking\|ephemeral' docs/ layout.md .capcom/knowledge/glossary/`); run `capcom check`.
**Files:** docs/manual/commands.md; docs/manual/sessions.md; docs/layout/side-panel-layout.md; docs/manual/configuration.md; layout.md; .capcom/knowledge/glossary/glossary-turns/interjection.md; .capcom/knowledge/glossary/glossary-identity/side-panel.md; .capcom/knowledge/glossary/glossary-safety/mcp-client.md; docs/design/mcp-client.md
**Read first:** docs/manual/commands.md — /panel row; docs/manual/sessions.md — While you watch; docs/layout/side-panel-layout.md — default list, section table, Pointer;
layout.md — report panes, four reports, key precedence
**Tests:** none beyond docs drift tests.
**Acceptance:** `capcom check && GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestManual|TestDocs' ./cmd/apogee/`
**Closes:** mcp-live-status-view
**Commit:** `docs: describe /mcp and the mcp side-panel section`
