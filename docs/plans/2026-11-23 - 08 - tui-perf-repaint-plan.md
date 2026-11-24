# TUI performance — repaint path — plan

**Goal:** stop paying for the whole scrollback on every keystroke, streamed batch, frame and separator drag: no repaint on a draft edit, no per-repaint width pass over every line, a streamed batch re-renders only the preview tail, a cheaper frame composition and cold repaint, coalesced drag repaints. Then record the audit's before/after. Drawn output stays byte-identical (the drag skips queued intermediate widths, as ratified).

**Date:** 2026-11-23
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 47d9e204

**Regression check (2026-11-23, 47d9e204):**
- 2: guard folded
- 3: guard folded (writer decision + reviewer guards)
- 4: guard folded
- 5: guard folded (writer decision + reviewer guards)
- 6: guard folded (writer decision + reviewer guards)
- 7: guard folded (writer decision + reviewer guards)
- 8: guard folded

**Sources:**
- `docs/reviews/tui-perf-audit-2026-11-23.md` (Profile, Suggested plan items 2–4, 7, 9, 11)
- `.capcom/issues/tui-perf-responsiveness/` — keystroke-repaints-transcript, viewport-remeasures-lines, view-recompose-floor, reserve-overrun-recut, delta-repaints-transcript, resize-cold-repaint-cost, panel-drag-cold-repaints; parent `tui-perf-responsiveness`
- ADR 0011 + `internal/tui/doc.go`; ADR 0110 with `adr-0110-amend-1108` (b), (e); `capcom know show adr-0062-amend-1118`; `docs/design/test-drivers.md` §Waiting
- bubbles `viewport` v2.1.0 (`SetContentLines`, `maxLineWidth`, `View`) in the module cache
- Prerequisite plan: `2026-11-23 - 07 - tui-perf-frame-and-local-fixes-plan.md` (kept audit benchmarks)

**Ratified design calls:**
- **Split:** this plan follows plan 07 (owner, 2026-11-23).
- **Viewport:** the bubbles viewport keeps the scroll state but is handed blank placeholder lines of the same count; apogee composes the visible body from its own `m.lines` window. No fork, no dependency change (owner, 2026-11-23).
- **Stream:** an incremental splice seam in this plan — a stream-only generation; when only the preview tail changed and it is the last block, keep the settled prefix and re-render the tail; anything else repaints fully (owner, 2026-11-23).
- **Drag:** queued separator motions coalesce to one repaint at the latest width; a dated `adr-0110` amendment narrows "every column" to every settled burst; each drawn frame is one of today's frames (owner, 2026-11-23).
- **Resize:** leaf fixes only (span painter, known row widths); lazy visible-first painting stays out (writer, 2026-11-23).
- **One stash:** every repaint, splice included, still restashes only inside `refreshViewport` (writer, 2026-11-23).

**Standing requirements:**
- skills: coding-standards
- Byte identity is proven against the pre-change output: golden frames captured before the change (item 3) or a nil paint cache / fresh build.
- Model-needing tests script `internal/stubllm`; waits go through `internal/testtime`; no fixed clock in shipped code (slow-box rule).
- One test process at a time, narrowed to one package and `-run`; benchmarks without `-race`, one at a time; before/after numbers in each commit body.
- Bubble Tea v2 names only. `TestArmsLeaveLayoutToTail` stays green; a new repaint helper is added to its name list.

**Out of scope:**
- Plan 07's items; unfiled audit findings (blink splice, preview markdown memo, debounce, long-draft geometry, cursor-move park).
- Lazy / visible-first cold painting; forking bubbles.

## 1. Verify plan 07 is archived — ✅ DONE (2026-11-24)

NOTES (2026-11-24): gate passed — `docs/plans/archived/2026-11-23 - 07 - tui-perf-frame-and-local-fixes-plan.md` exists (no unarchived copy under docs/plans/), and internal/tui/perfaudit_bench_test.go defines BenchmarkPerfAuditStream (l.118), BenchmarkPerfAuditBlocks (l.161), BenchmarkPerfAuditType (l.266), plus Fold, Scroll and Idle (grep count 6); no tree change, nothing to commit.

**What:** test-only gate. Confirm `docs/plans/archived/2026-11-23 - 07 - tui-perf-frame-and-local-fixes-plan.md` exists and `internal/tui/perfaudit_bench_test.go` defines `BenchmarkPerfAuditStream`, `BenchmarkPerfAuditBlocks`, `BenchmarkPerfAuditType`. Stop the run if not.
**Files:** none
**Read first:** docs/skill-runs/code-audit/2026-11-23/profile/zz_perfaudit_bench_test.go.txt — BenchmarkPerfAuditBlocks (view/repaint/resize subs), BenchmarkPerfAuditType, paModel, paStep; docs/plans/2026-11-23 - 07 - tui-perf-frame-and-local-fixes-plan.md — item 1 (creates internal/tui/perfaudit_bench_test.go, absent at 47d9e204)
**Tests:** none.
**Acceptance:**
- `test -f "docs/plans/archived/2026-11-23 - 07 - tui-perf-frame-and-local-fixes-plan.md"`
- `grep -c 'func BenchmarkPerfAudit' internal/tui/perfaudit_bench_test.go`
**Commit:** none (gate only)

## 2. A draft edit does not repaint the transcript — ✅ DONE (2026-11-24)

NOTES (2026-11-24): benchmark `go test -run '^$' -bench 'PerfAuditType' -benchmem ./internal/tui/` (dev box, no -race) — before: N=0 2.07 ms/op 382 KB 3296 allocs; N=48 4.50 ms/op 575 KB 5486 allocs; N=300 14.76 ms/op 1.39 MB 7200 allocs. After: N=0 1.83 ms/op 371 KB 3192 allocs; N=48 2.37 ms/op 429 KB 5023 allocs; N=300 2.29 ms/op 421 KB 5021 allocs — flat in N.

NOTES (2026-11-24): the shared re-seat helper is `Model.seatViewportOffset` (model.go); refreshViewport ends in it after SetContentLines, layout's no-repaint branch clamps a past-bottom offset (as SetContentLines does) and then calls it unconditionally. The no-repaint branch does not re-seat the block cursor (seatBlockCursor is "against the paint that just landed", and no paint landed).

NOTES (2026-11-24): renderView's inputs (transcript generation, theme scheme/glyphs/measure/padPrompts, width via transcriptWidth, blink, backHint) plus the widget measure's viewport width are all in frameKey or the explicit viewport-width check; nothing joined the key. layout repaints when `m.lines == nil`, the viewport width changed, or the key moved.

NOTES (2026-11-24): consequential edit — internal/tui/settled_test.go: made necessary by the new helper seatViewportOffset (standing requirement: a new repaint helper joins TestArmsLeaveLayoutToTail's name list; fixture line and expected finding added to TestArmsLeaveLayoutToTailBites).

NOTES (2026-11-24): consequential edit — cmd/apogee/testdata/stubllm/typing.yaml: made necessary by TestE2ETypingWrapsAndRefitsTheTranscript (a new stubllm fixture with a reply long enough to overflow the transcript).

NOTES (2026-11-24): runview.go:318's `// geometry: ... read only by refreshViewport` comment stays as written: that arm calls refreshViewport directly, which still ends at the GotoBottom it names.

**What:**
**Goal:** typing a printable rune causes no transcript repaint (`m.lines` stays the same slice, no paint-cache lookup); the input box still auto-grows and the transcript re-fits above it; frames byte-identical.
**Approach (assumed at the header base):** `layout()` (`model.go`) ends in `refreshViewport()` only when `m.frameKey() != m.painted`, the viewport width changed, or `m.lines` is nil; otherwise, when only the widget height moved, it re-seats the offset exactly as `refreshViewport`'s tail does (following: `GotoBottom` + `floorShortScreenOffset`; detached: clamp, floor, clear `detached` when `AtBottom`) through one shared helper both call. Before relying on the key, list every input `renderView` reads; any not covered by `frameKey` either joins the key or keeps `layout` repainting.
**Regression guard.** `layout`'s no-repaint branch runs the re-seat tail unconditionally (it is O(1)), not only when the widget height moved: `stageChildMessage` (`interject.go`) clears `detached` and calls `layout()` with the frame key and height unchanged, and its view must still land at the bottom as `refreshViewport`'s `GotoBottom` lands it today.
The comments stating that `layout()` repaints are superseded — `doc.go`'s positioning-repaint list (`stageChildMessage`), `model.go`'s `freshenTranscriptClamp` ("layout() re-renders the whole transcript") and `settle` ("ends in refreshViewport") docs, `interject.go`'s `// geometry:` comment: update every one (`grep -n 'layout() re-renders\|ends in refreshViewport\|read only by refreshViewport' internal/tui/*.go`).
**Files:** internal/tui/model.go, internal/tui/paintcache.go, internal/tui/interject.go, internal/tui/doc.go, internal/tui/model_test.go, cmd/apogee/e2e_typing_test.go
**Read first:** internal/tui/model.go — layout, refreshViewport, settle, freshenTranscriptClamp; internal/tui/paintcache.go — Model.frameKey; internal/tui/interject.go — stageChildMessage;
  internal/tui/settled_test.go — assertSettled, TestArmsLeaveLayoutToTail
**Closes:** keystroke-repaints-transcript
**Tests:** `TestTypingDoesNotRepaintTheTranscript` (300-block resumed model: `sameLines` and `paints` hit/miss counters unchanged across a rune); `TestTypingReseatsWhenTheBoxGrows` (`assertSettled` after a wrap to a second row, following and detached); `TestLayoutReseatsADetachedChildSend` (detached run view, draft rows == band rows, send → `YOffset` at the bottom); `TestE2ETypingWrapsAndRefitsTheTranscript` (tuitest + stubllm).
**Acceptance:**
- `go test -race -count=1 -run 'TestTyping|TestArmsLeaveLayoutToTail|TestSettled|TestLayout' ./internal/tui/`
- `go test -race -count=1 -run 'TestE2ETyping' ./cmd/apogee/`
- `go test -run '^$' -bench 'PerfAuditType' -benchmem ./internal/tui/` — flat in N (record before/after)
**Commit:** `perf(tui): skip the transcript repaint when a draft edit leaves the frame key unchanged`

## 3. The viewport gets placeholder lines; the body is composed from m.lines — ✅ DONE (2026-11-24)

NOTES (2026-11-24): benchmark `go test -run '^$' -bench 'PerfAuditBlocks/repaint' -benchmem ./internal/tui/` (dev box, no -race) — before: N=48 3.97 ms/op 318 KB 5698 allocs; N=300 14.21 ms/op 1.13 MB 7407 allocs; N=900 38.57 ms/op 3.09 MB 11426 allocs. After: N=48 2.45 ms/op 317 KB 5697 allocs; N=300 4.04 ms/op 1.11 MB 7404 allocs; N=900 8.09 ms/op 3.07 MB 11422 allocs.

NOTES (2026-11-24): the eight golden frames under internal/tui/testdata/frames were captured with `-update` at the pre-change tree (HEAD 7bf66fefb) before model.go was touched, and pass unchanged after it.

NOTES (2026-11-24): the placeholder store is the package-level `atomic.Pointer` option (`placeholderStore`, swapped only by CAS to a larger fully-built array; slices handed out are capped `[:n:n]`), not the per-Model pointer — no Model field and no newModel change were needed.

NOTES (2026-11-24): `transcriptBody` keeps the widget's clip as a backstop but cuts only a visible row measured over the width (the widget cut every visible row once ANY stored line was over); the painter's reserve leaves no such row, and the over-wide corpus case of TestTranscriptBodyMatchesTheViewportPass matches the widget byte for byte.

NOTES (2026-11-24): consequential edit — internal/tui/render.go: made necessary by the widget no longer drawing the body (the file-header paragraph naming viewport.SetContentLines as the line consumer, and reserveWidgetCells' comments naming the widget's clip, now name transcriptBody).

NOTES (2026-11-24): the composed body is not a repaint helper, so TestArmsLeaveLayoutToTail's name list is unchanged.

**What:**
**Goal:** a repaint performs no width pass over unchanged scrollback lines, and the frame (body, scroll bar, sticky header, highlights) is byte-identical to the pre-change frame on every golden fixture; follow, detach, PgUp/PgDn, wheel and the block cursor behave as before.
**Approach (assumed at the header base):** first, at the pre-change tree, add `TestViewFrameGolden` writing `internal/tui/testdata/frames/*.golden` (`-update` flag) over fixtures holding VS16 and wide graphemes, a short tail, a detached offset, scroll bar on, side panel on/off; commit them with this item. Then `refreshViewport` hands `SetContentLines` a slice of a shared, never-written `""` placeholder backing array of `len(m.lines)` (the real slice when `len(m.lines) <= 1`, preserving the widget's single-zero-width-line rule). `View` builds the transcript body from `m.lines[YOffset : YOffset+height]`, padded to the viewport's width and height exactly as `vp.View()`'s lipgloss pass did, before `applyStickyHeader` / `highlightTranscript` / `highlightBlockCursor` / `joinScrollbar`. A test asserts no `m.lines` entry contains `\r` or `\n` (the widget would split it and the counts would differ).
**Regression guard.** refreshViewport's tail keeps calling item 2's shared offset re-seat helper after swapping SetContentLines' argument to placeholders — there is one re-seat path, used by both layout's no-repaint branch and refreshViewport.
`TestViewportRowsStayOneForOneWithStoredLines`, `TestPainterReservesTheCellsTheWidgetMeasuresOver`, `TestFullWidthLineKeepsItsTrailingWideMeasuredGlyph` and their helper `drawnRow` read glyphs out of `m.viewport.View()`, which goes blank: repoint them at the composed transcript body (one helper `View` also calls), keeping their `TotalLineCount` / `XOffset` assertions as they are.
The placeholder store is per-Model behind a pointer allocated in `newModel` (grown copy-on-grow, nil-safe), or a package-level `atomic.Pointer` swapped only to a larger fully-built slice — never a bare global var reassigned on growth (parallel tests' `refreshViewport` would write it: `doc.go`'s parallel-subtest memo invariant).
The comments recording the widget's clip as the reserve's backstop are superseded (`model.go` `newModel` and `refreshViewport`, `render.go`'s "the widget would cut"): update every one (`grep -n "widget's clip\|widget would\|widget measures" internal/tui/*.go`).
**Files:** internal/tui/model.go, internal/tui/render.go, internal/tui/view_golden_test.go, internal/tui/testdata/frames/, internal/tui/model_test.go
**Read first:** internal/tui/model.go — refreshViewport, View, newModel; bubbles/v2@v2.1.0/viewport/viewport.go — SetContentLines, View; internal/tui/render.go — reserveWidgetCells;
  internal/tui/model_test.go — TestViewportRowsStayOneForOneWithStoredLines, drawnRow
**Closes:** viewport-remeasures-lines
**Tests:** `TestViewFrameGolden`; `TestTranscriptBodyMatchesTheViewportPass` (real bubbles viewport over the real lines vs the composed body, corpus incl. VS16, wide, tab-bearing, SGR, short tail); `TestTranscriptLinesHoldNoNewlines`; `TestViewportRowsStayOneForOneWithStoredLines`, `TestPainterReservesTheCellsTheWidgetMeasuresOver`, `TestFullWidthLineKeepsItsTrailingWideMeasuredGlyph` (and `drawnRow`) repointed at the composed body; existing scroll/cursor/mouse tests green.
**Acceptance:**
- `go test -race -count=1 -run 'TestViewFrameGolden|TestTranscriptBody|TestTranscriptLines|TestScroll|TestBlockCursor|TestPaint|TestViewportRows|TestFullWidthLine' ./internal/tui/`
- `go test -race -count=1 -run 'TestE2EToolsUmbrellaFold' ./cmd/apogee/`
- `go test -run '^$' -bench 'PerfAuditBlocks/repaint' -benchmem ./internal/tui/` — N=900 well below 40.6 ms (record before/after)
**Commit:** `perf(tui): stop the viewport re-measuring every scrollback line on repaint`

## 4. View measures each visible row once — ✅ DONE (2026-11-24)

NOTES (2026-11-24): benchmark `go test -run '^$' -bench 'PerfAuditBlocks/view' -benchmem ./internal/tui/` (dev box, no -race) — before (HEAD 6adcc3713, already past item 3): N=48 1.87 ms/op 172 KB 5232 allocs; N=300 1.82 ms/op 165 KB 5230 allocs; N=900 1.93 ms/op 163 KB 5228 allocs. After: N=48 1.41 ms/op 151 KB 4954 allocs; N=300 1.39 ms/op 147 KB 4953 allocs; N=900 1.39 ms/op 146 KB 4951 allocs. The Goal's 2.2 ms / 720 KB at N=300 was the header base; item 3 had already brought View under it, this item takes a further ~24%.

NOTES (2026-11-24): the known widths travel in a new `squaredRows` value (rows + per-row painted width): `joinScrollbar` returns it, `joinColumn` takes one as its head and returns one, and `joinFrame(head, blocks)` pads or cuts the head rows by their carried width (`squareKnown`); View holds the head separately from its block list. A joined row's width is the sum of its parts only where no grapheme cluster can span the seam (`seamHolds`: left ends and right starts on an ASCII byte); otherwise, and after a cut that drops a straddling wide glyph, the joined row is measured — so the carried width is always exact and the frame byte-identical (`TestJoinFrameSkipsSquaredRows` fails if the seam guard is forced true).

NOTES (2026-11-24): the seal memo (`sealMemo`, Model field `seals`) has one slot per frame row by row index rather than a fixed list of chrome rows: it stores only rows that carry a background SGR (in practice the bottom chrome, hairlines and sent prompt blocks), empties a slot whose row this frame does not seal, and drops slots past the frame's last row, so it holds the last frame only. Keyed on line, width and width method; nil-safe.

NOTES (2026-11-24): `TestScrollbarGlyphsRenderedOnce` proves the one-render-per-bar by string backing identity (`unsafe.StringData`) — `testing.AllocsPerRun` panics in a parallel test and counts other goroutines' allocations.

**What:** Depends on item 3.
**Goal:** a `View` of an unchanged model costs measurably less than 2.2 ms / 720 KB per frame at N=300, with every golden frame unchanged.
**Approach (assumed at the header base):** in `boxdraw.go`, `joinScrollbar` / `joinColumn` return rows already squared to a known width and `joinFrame` skips re-squaring rows whose width it was handed; `scrollbarCells` renders the thumb and track glyphs once per frame, not per row; `sealRowEdges` results for chrome rows (status, footer, input box) are memoised in a pointer-held memo keyed by (line, width), allocated in `newModel`.
**Regression guard.** The chrome-row `sealRowEdges` memo is bounded to the last frame — one slot per chrome row (row index → last line, width, sealed result) or a map rebuilt each `View` and swapped — never a map that grows with every spinner tick and keystroke (today `sealRowEdges` keeps nothing between frames, only a `sync.Pool` scratch buffer); it stays nil-safe for `Model` literals.
**Files:** internal/tui/boxdraw.go, internal/tui/model.go, internal/tui/boxdraw_test.go
**Read first:** internal/tui/boxdraw.go — joinFrame, joinScrollbar, joinColumn, scrollbarCells, sealRowEdges; internal/tui/model.go — View, newModel;
  internal/tui/paint_test.go — TestPaintFrameMatchesTheViewOnASCII
**Closes:** view-recompose-floor
**Tests:** `TestViewFrameGolden` (unchanged goldens); `TestJoinFrameSkipsSquaredRows` (old vs new over VS16/wide rows); `TestScrollbarGlyphsRenderedOnce`; `TestSealMemoHoldsOnlyTheLastFrame` (many distinct status lines → the memo holds one entry per chrome row); `TestPaintFrameMatchesTheViewOnASCII`, `TestPaintedWidthMeasuresDisagreeOnVS16` green.
**Acceptance:**
- `go test -race -count=1 -run 'TestViewFrameGolden|TestJoin|TestScrollbar|TestPaint|TestSeal' ./internal/tui/`
- `go test -run '^$' -bench 'PerfAuditBlocks/view' -benchmem ./internal/tui/` (record before/after)
**Commit:** `perf(tui): measure each visible row once per frame`

## 5. Overrunning lines are split when a block's paint is stored — ✅ DONE (2026-11-24)

NOTES (2026-11-24): benchmark `go test -run '^$' -bench 'ReserveWidgetCellsAllHit' -benchmem -count=3 ./internal/tui/` (dev box, no -race) — before (HEAD f79dff178, new twin added first): AllHit ~246 µs/op 111 KB 294 allocs; AllHitNoOverrun ~117 µs/op 59 KB 176 allocs. After: AllHit ~121 µs/op 69 KB 177 allocs; AllHitNoOverrun ~117 µs/op 59 KB 176 allocs (unchanged). The twin is `widgetCellsFixtureEnding(width, "!!")` — same scrollback with an ASCII ending the two measures agree on; both benchmarks share `benchmarkReserveAllHit`.

NOTES (2026-11-24): the store-time break is gated by `storedBreakLimit(width)` = width + bodyRightGutter, or 0 at width ≤ 1: transcriptWidth's floor makes a 1-column paint ambiguous (viewport 1 or 2 wide), so such a paint is stored whole and the reserve breaks it cold at whatever limit; `TestStoredPaintsArrivePreBroken/floor` covers limits 1 and 2 and fails if the gate is dropped. The shared per-line rule is `widgetRows` (= `dropBlankTail(splitAtWidgetWidth(...))`), used by both `reserveWidgetCells` and the new `blockPaint.brokenAt`; cut rows carry cells `min(limit, width-k*limit)`, never -1.

NOTES (2026-11-24): `TestWidgetCellsReserveAsTheWidgetMeasures` drops the `limit = width` case and now compares the warm and rooted paints against a cold render's reserve (the warm paint is pre-broken, so `baseReserve` of it would prove nothing); `TestWidgetCellsAreMeasuredOncePerPaintedBlock` takes its break premise and its new-block measure count from `coldRender`, and asserts every byte-overrunning warm line stores cells in [0, limit].

NOTES (2026-11-24): consequential edit — internal/tui/model.go: made necessary by the store-time break (refreshViewport's comment said overrunning lines are broken only in the reserve).

**What:**
**Goal:** an all-hit repaint whose scrollback holds an overrunning line costs about what a reserve with no overrun costs, and its lines are byte-identical to today's.
**Approach (assumed at the header base):** `transcript.paintBlock` (`paintcache.go`) splits lines whose stored `cells` exceed `key.width + bodyRightGutter` with the reserve's own per-line rule (`dropBlankTail(splitAtWidgetWidth(...))`, one shared helper) when the paint is stored, splitting `targets` (copying the mark), `cells` and pads alongside, so cached blocks arrive pre-broken. `reserveWidgetCells` (`render.go`) stays for unmeasured lines (separators, breadcrumb, preview) and the nil-cache cold path, which remains the reference.
**Regression guard.** After this item a warm renderView and a nil-cache renderView differ before the reserve, so byte-identity comparisons against a nil paint cache compare after reserveWidgetCells(width + bodyRightGutter); update any existing test that compares warm vs cold before the reserve to compare after it.
`TestWidgetCellsAreMeasuredOncePerPaintedBlock` is restated: its "the reserve breaks a line" premise comes from `coldRender`, and split pieces carry cells ≤ limit without calling `widgetWidth` (never -1, or the all-hit repaint measures them again).
The reserve relies on the store-time split only when its limit equals `key.width + bodyRightGutter`; any other limit (the 1-column viewport, `transcriptWidth`'s floor) must still yield the cold path's lines, and the `limit = width` case leaves `TestWidgetCellsReserveAsTheWidgetMeasures`. The shared helper keeps `dropBlankTail`, so a squared line overrunning only in padding gains no blank row (`TestReserveWidgetCellsMovesTargetsAndSpansWithTheRows`).
`TestStoredPaintsArrivePreBroken` adds the bite: a warm `renderView` (before the reserve) holds no line wider than `width + bodyRightGutter`; the Goal's "about what a reserve with no overrun costs" is read off a no-overrun twin, `BenchmarkReserveWidgetCellsAllHitNoOverrun`, recorded with the before/after.
**Files:** internal/tui/paintcache.go, internal/tui/render.go, internal/tui/paintcache_test.go
**Read first:** internal/tui/paintcache.go — transcript.paintBlock, paintCache.store; internal/tui/render.go — renderedTranscript.reserveWidgetCells, splitAtWidgetWidth, dropBlankTail;
  internal/tui/paintcache_test.go — TestWidgetCellsAreMeasuredOncePerPaintedBlock, TestWidgetCellsReserveAsTheWidgetMeasures, coldRender
**Closes:** reserve-overrun-recut
**Tests:** `TestStoredPaintsArrivePreBroken` (warm vs cold over the `⚠️⚠️` full-width fixture, compared after the reserve: lines, targets, pads, padSeps, userBlocks equal; and no pre-reserve warm line wider than `width + bodyRightGutter`); `TestWidgetCellsAreMeasuredOncePerPaintedBlock` restated; `TestWidgetCellsReserveAsTheWidgetMeasures` without the `limit = width` case; `BenchmarkReserveWidgetCellsAllHit` and `BenchmarkReserveWidgetCellsAllHitNoOverrun` before/after; existing reserve and VS16 tests green.
**Acceptance:**
- `go test -race -count=1 -run 'TestStoredPaints|TestReserve|TestPaintCache|TestWidgetCells' ./internal/tui/`
- `go test -run '^$' -bench 'ReserveWidgetCellsAllHit' -benchmem ./internal/tui/` (record before/after)
**Commit:** `perf(tui): split overrunning lines once, when a block's paint is stored`

## 6. A streamed batch re-renders only the preview tail — ✅ DONE (2026-11-24)

NOTES (2026-11-24): benchmark `go test -run '^$' -bench 'PerfAuditStream' -benchtime=1x -benchmem ./internal/tui/` (Pi 5, no -race, one op = 4096 deltas) — before (HEAD 6335abe58): session=0 2.86 ms/delta, 1.50 GB/op, 23.95 M allocs/op; session=300 5.16 ms/delta, 5.21 GB/op, 32.38 M allocs/op. After: session=0 2.46 ms/delta, 1.44 GB/op, 23.47 M allocs/op; session=300 2.64 ms/delta, 2.24 GB/op, 23.54 M allocs/op.

NOTES (2026-11-24): the splice keeps the preview's SEPARATOR in the kept prefix rather than re-rendering it (the plan's approach recorded the start "separator included"): the separator and the half-row stand-in depth it may write onto the row above are functions of the block above and of the preview's depth, which a pure append cannot move, so the stash is `renderedTranscript.previewRows` / `Model.previewRows` — the count of the preview's own trailing rows after the reserve — and 0 (never a valid-looking index) means "no splice". A preview with no rows also counts as invalid.

NOTES (2026-11-24): shape of the seam — `transcript.previewAt` / `previewBlock` are extracted from renderView (same code), `eachBlockRow` is renderView's per-row loop extracted so the whole walk and `transcript.renderPreview` lay rows one way; `Model.splicedPaint` decides and builds fresh slices with `slices.Concat`; refreshViewport keeps the one restash, cursor seat and selection rule. Besides the key moving only in streamGen and `previewRows > 0`, the splice also requires the stashed widget width (`Model.paintedLimit`) to match and refuses a forgotten frame (`painted == frameKey{}`, watch.go's forgetPaintedFrame).

NOTES (2026-11-24): readers of `transcript.generation` checked — only frameKey (now with streamGen) and usage.go's delegate-spend memo, which reads entries only, so neither needs streamGen.

NOTES (2026-11-24): `TestTranscriptWritersBumpTheGeneration` — the `appendToken` row of the writes table (a pure append on the streaming seed) becomes two subtests (pure append: streamGen moved, generation held; first token after a commit: generation moved), the table gains "appendToken for another run" (a park), and the holds now also assert streamGen held.

NOTES (2026-11-24): `TestE2EStreamIntoALongSessionMatchesAColdRender` builds its stubllm Script in Go inside the test file (as e2e_copy/e2e_advise do) rather than as a testdata fixture, keeping to the item's file list; the stream is held open after its visible text by 30000 whitespace-only deltas (a trailing blank line the preview holds back), so the spliced frame and the full repaint after a resize away and back are of the same live state, and the test cancels the run once it has both.

NOTES (2026-11-24): consequential edit — internal/tui/doc.go: made necessary by the pure-append counter (the "an arm mutates" invariant named only the transcript generation as the transcript's paint input).

NOTES (2026-11-24): consequential edit — internal/tui/settled_test.go: made necessary by the new repaint helper `splicedPaint` (the plan's standing requirement adds it to `layoutCallNames`).

NOTES (2026-11-24): retry fix — `steadyTranscriptRows` in cmd/apogee/e2e_stream_splice_test.go now also waits on `paintedAtWidth`: every transcript row from the top to the reply's last visible line must reach the frame's last column (the rules and scrollbar track of a paint at that width), so the widened-but-stale 70-column paint after the resize back is no longer accepted by the two-equal-reads check; the e2e passed 10/10 under -race and 5/5 without.

**What:** Depends on items 3 and 5.
**Goal:** a streamed delta at 300 tool blocks costs close to the empty-session cost, and every frame while streaming is byte-identical to a full repaint.
**Approach (assumed at the header base):** `transcript.appendToken`'s pure-append path bumps a new `streamGen` instead of `generation` (`displace`, `unpark`, `passBlock` and every structural change still `touch()`); `frameKey` carries `streamGen`. `refreshViewport` records the post-reserve start of the preview block (separator included) in its stash; when the key moved only in `streamGen`, the preview is the last block and the stashed start is valid, it builds fresh slices (never appending in place: the viewport and `sameLines` alias `m.lines`) from the stashed prefix plus the reserved preview block, for lines, targets, pads and padSeps; otherwise it renders fully. The restash, cursor seat and selection keep-rule stay in `refreshViewport`. Grep every reader of `transcript.generation` and confirm none needs `streamGen`.
**Regression guard.** Byte-identity against a full / nil-cache repaint compares post-reserve lines (see item 5).
A pure append is `t.streaming && t.pendingRun == run` on entry with no `passBlock` move; every other path — the first token (streaming false→true) included — still calls `touch()`, and a stash with no preview counts as invalid.
The prose recording the live buffer under `generation` is superseded — `renderView`'s doc (`render.go`), `frameKey.generation` (`paintcache.go`), `touch()`'s doc (`transcript.go`): update every comment saying the live buffer, or "every writer", moves generation (`grep -rn 'generation' internal/tui/*.go`).
`TestE2EStreamIntoALongSessionMatchesAColdRender`'s oracle is the frame after a forced full repaint mid-stream (a resize away and back moves `frameKey.width`): `cmd/apogee` cannot reach the unexported nil-cache renderer.
**Files:** internal/tui/transcript.go, internal/tui/paintcache.go, internal/tui/render.go, internal/tui/model.go, internal/tui/paintcache_test.go, internal/tui/transcript_test.go, cmd/apogee/e2e_stream_splice_test.go
**Read first:** internal/tui/transcript.go — transcript.appendToken, transcript.touch, transcript.displace; internal/tui/blockfold.go — transcript.passBlock; internal/tui/paintcache.go — Model.frameKey;
  internal/tui/render.go — transcript.renderView; internal/tui/model.go — Model.refreshViewport; internal/tui/transcript_test.go — TestTranscriptWritersBumpTheGeneration
**Closes:** delta-repaints-transcript
**Tests:** `TestStreamSpliceMatchesAFullRepaint` (per delta, spliced vs nil-cache full render, both after the reserve; parallel-sibling preview mid-list falls back); `TestTranscriptWritersBumpTheGeneration` restated (the `appendToken` case on its already-streaming seed asserts `streamGen` moved and `generation` held; a new first-token case must move `generation`); `TestE2EStreamIntoALongSessionMatchesAColdRender` (oracle: the forced full repaint); `TestPaintCacheRepaintsOnlyTheStreamingTail` green.
**Acceptance:**
- `go test -race -count=1 -run 'TestStreamSplice|TestPaintCache|TestStreaming|TestTranscriptWriters|TestArmsLeaveLayoutToTail' ./internal/tui/`
- `go test -race -count=1 -run 'TestE2EStreamInto' ./cmd/apogee/`
- `go test -run '^$' -bench 'PerfAuditStream' -benchmem ./internal/tui/` — session=300 near session=0 (record before/after)
**Commit:** `perf(tui): re-render only the preview tail on a streamed batch`

## 7. Cheaper cold paint leaves

**What:**
**Goal:** a cold repaint (one-column resize) at 900 tool blocks is measurably below 200 ms, every changed paint helper byte-identical to the lipgloss path.
**Approach (assumed at the header base):** in `theme.go`, a span painter for the bare foreground roles used by `renderSuperGroup`, `leaderRowIn`, `umbrellaHeader` and `renderToolBlock`: SGR open/close derived once per role from lipgloss itself, used only for single-line, tab-free input, falling back to `Style.Render` otherwise. `leaderRowIn` (`toolleader.go`) returns its cell count and `indicatorRow` (`toolblock.go`) uses it instead of re-measuring.
**Regression guard.** Byte-identity against the cold render compares post-reserve lines (see item 5).
`leaderRowIn`, `leaderRow` and `indicatorRow` keep their signatures (callers in `sidepanel.go`, `subagentblock.go`, `toolbranch.go`, `workflowblock.go`, `toolleader_test.go`, `toolbranch_test.go`, `wrap_test.go`); the cell count comes through a sibling (e.g. `leaderRowCells`), and any caller moved to it is added to **Files:**.
The returned count equals `th.measure.Width(row)`: lipgloss expands a tab to 4 cells, so a segment holding `\t` falls back to measuring; a test asserts count == measure over the leader corpus (tabs, VS16, the finished ✓ split, the panel title paint).
**Files:** internal/tui/theme.go, internal/tui/toolleader.go, internal/tui/toolblock.go, internal/tui/theme_test.go, internal/tui/toolleader_test.go
**Read first:** internal/tui/toolleader.go — leaderRowIn, leaderRow; internal/tui/toolblock.go — indicatorRow, renderSuperGroup, umbrellaHeader; internal/tui/theme.go — newTheme;
  internal/tui/subagentblock.go — groupHeader; internal/tui/sidepanel.go — panelHeaderRow
**Closes:** resize-cold-repaint-cost
**Tests:** `TestSpanPainterMatchesLipgloss` (every role × corpus with tabs, newlines, SGR-carrying, wide and VS16 text); `TestLeaderRowCellsMatchTheMeasure` (count == `th.measure.Width(row)` over tabs, VS16, the finished ✓ split, the panel title paint); `TestViewFrameGolden` and the cold-render tests green.
**Acceptance:**
- `go test -race -count=1 -run 'TestSpanPainter|TestViewFrameGolden|TestToolBlock|TestLeader|TestTheme' ./internal/tui/`
- `go test -run '^$' -bench 'PerfAuditBlocks/resize' -benchmem ./internal/tui/` (record before/after)
**Commit:** `perf(tui): paint bare-colour spans without the lipgloss block pipeline`

## 8. Coalesce side-panel separator drag repaints

**What:**
**Goal:** a burst of K queued separator motions costs one cold repaint at the latest width; the separator still re-wraps while dragged; the drag's final width persists; `adr-0110` records the narrowed rule.
**Approach (assumed at the header base):** `dragPanelSeparator` (`sidepanel.go`) records the target width on `m.panelDrag` (not `opts.UI.SidePanelWidth`) and returns a `panelDragSettleMsg` Cmd only when none is pending; that Msg applies the latest target and Update's tail lays out; `endPanelDrag` applies any pending target. `settleFrame` no longer lets drag motions through. Add `.capcom/knowledge/decisions/adr-0110/adr-0110-amend-1123.md` (`capcom know add --id adr-0110-amend-1123 --parent adr-0110`) narrowing (b)'s "every column" and (e)'s motion exception to the settle Msg, name it on ADR 0110's Status line and reading-order list. Update every comment or doc stating that a drag motion relayouts (`grep -rn 'panelDragResized\|every column\|drag.*relayout' internal/tui docs layout.md`).
**Regression guard.** `handleMouseMotion` (`mouse.go`) returns the drag's Cmd instead of dropping it (`m.dragPanelSeparator(msg.X), nil` today). The settle Msg is inert unless `m.panelDrag.active` and a target is pending: it can land after the release applied the target, or after `abandonHiddenPanelDrag`.
`TestSidePanelSeparatorDrag`'s "drag moves live and writes once" and "a drag past either limit clamps" subtests are restated to run the settle Msg after each motion (`stepCmd`, then feed its Msg), keeping their assertions.
The comment rule is every comment or doc that says a drag motion re-wraps, relayouts or settles; it replaces the Approach's grep with `grep -rn 'amend-1108 (e)\|panelDragResized\|every column\|re-wraps\? live\|lays the frame out for' internal/tui docs layout.md` (it reaches `mouse.go`'s `handleMouseMotion` doc, `doc.go`'s "the frame re-wraps live", `layout.md` and `side-panel-layout.md`).
**Files:** internal/tui/sidepanel.go, internal/tui/mouse.go, internal/tui/model.go, internal/tui/doc.go, internal/tui/model_test.go, internal/tui/sidepanel_test.go, layout.md, docs/layout/side-panel-layout.md, .capcom/knowledge/decisions/adr-0110.md, .capcom/knowledge/decisions/adr-0110/adr-0110-amend-1123.md
**Read first:** internal/tui/sidepanel.go — Model.dragPanelSeparator, Model.endPanelDrag, Model.abandonHiddenPanelDrag; internal/tui/mouse.go — Model.handleMouseMotion; internal/tui/model.go — settleFrame;
  internal/tui/model_test.go — TestMouseMotionRepaintsADraggedSeparator; internal/tui/sidepanel_test.go — TestSidePanelSeparatorDrag; .capcom/knowledge/decisions/adr-0110/adr-0110-amend-1108.md — (b), (e)
**Closes:** panel-drag-cold-repaints
**Tests:** `TestMouseMotionRepaintsADraggedSeparator` updated to run the settle Cmd; `TestSidePanelSeparatorDrag` subtests restated to feed the settle Msg; `TestDragBurstCostsOneColdRepaint` (`paints.misses` across K motions + one settle); `TestMouseMotionNeverRepaints` green; `TestE2ESidePanelDragResizesAndPersists` green.
**Acceptance:**
- `go test -race -count=1 -run 'TestMouseMotion|TestDragBurst|TestPanelDrag|TestSidePanel' ./internal/tui/`
- `go test -race -count=1 -run 'TestE2ESidePanelDrag' ./cmd/apogee/`
- `capcom check`
**Commit:** `perf(tui): coalesce side-panel separator drag repaints`

## 9. Record the audit's before/after

**What:** Depends on items 2–8. Docs-only. Run each `BenchmarkPerfAudit*` scenario, `BenchmarkThinkingPaneUpdateAndView`, `BenchmarkSidePanelView`, `BenchmarkRenderViewDelegationsAllHit` and `BenchmarkReserveWidgetCellsAllHit` one at a time, and write `docs/reviews/tui-perf-audit-2026-11-23-results.md`: the audit's Profile table with an "after" column, each dominant cost with its fix status and issue id (plans 07 and 08), and what stays open.
**Files:** docs/reviews/tui-perf-audit-2026-11-23-results.md
**Read first:** docs/reviews/tui-perf-audit-2026-11-23.md — Profile table, Suggested plan items; internal/tui/paintcache_test.go — BenchmarkThinkingPaneUpdateAndView, BenchmarkReserveWidgetCellsAllHit;
  .capcom/issues/tui-perf-responsiveness.md — Acceptance; .capcom/issues/tui-perf-responsiveness/ — the 13 children (07 + 08 split)
**Closes:** tui-perf-responsiveness
**Tests:** none.
**Acceptance:**
- `test -s docs/reviews/tui-perf-audit-2026-11-23-results.md`
- `grep -c 'after' docs/reviews/tui-perf-audit-2026-11-23-results.md`
**Commit:** `docs(reviews): record the TUI perf audit's before/after`
