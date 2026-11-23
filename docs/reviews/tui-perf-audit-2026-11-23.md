# TUI performance audit — 2026-11-23

**Scope:** performance and responsiveness of apogee's terminal UI: `internal/tui` plus the render helpers it calls (`internal/format`, `internal/present`, `internal/sanitize`, `internal/sidepanel`, `internal/scheme`). Correctness, security and style were out of scope. Driven by capcom issue `tui-perf-responsiveness`.
**Mission:** apogee is a terminal coding agent built for small, locally hosted models on modest hardware (a Raspberry Pi 5 is a first-class host, often running the model on the same box), so the TUI must stay responsive through hours-long sessions with hundreds of tool blocks, and every fix must leave the drawn screen byte-identical.
**Files reviewed:** 145

## How to read this report

A few terms used throughout:

- **Update / View.** Bubble Tea runs one loop: each message (a key, a mouse wheel notch, a spinner tick, a batch of streamed text) goes through `Model.Update`, then `Model.View` builds the whole screen as one string. Both run on the same goroutine, so time spent in either delays the next key.
- **Repaint.** `refreshViewport` (`internal/tui/model.go:3381`) rebuilds the full list of transcript lines (`transcript.renderView`) and hands it to the scrolling widget (bubbles' `viewport`). It runs whenever `settle` sees the frame key (`frameKey`, `internal/tui/paintcache.go:435`) change.
- **Paint cache.** `paintCache` remembers each settled block's drawn lines, so a repaint where nothing changed is "all-hit": no block is re-drawn. A width change makes every block miss ("cold repaint").
- **N** is the number of tool blocks in a session. In the benchmark fixture N=300 is about 3,700 transcript lines and N=900 about 11,000.
- **Scenarios:** `stream` (a long streamed reply), `blocks` (many tool blocks), `fold` (opening/closing a block), `scroll`, `type` (typing in the prompt box), `idle` (spinner ticks with nothing else happening), `resize`.

All timings below were measured on a Raspberry Pi 5 (see **Profile**).

## Executive Summary

The single biggest problem is that every repaint costs time proportional to the whole session, even when the paint cache serves every block: bubbles' `viewport.SetContentLines` re-measures the display width of every transcript line on each repaint (61% of an all-hit repaint), and the paint walk itself still visits every entry. That repaint then runs on every keystroke (16 ms per key at N=300), every streamed text batch (16 ms at N=300), twice a second while a tool call is running (the blinking star), and once per column while the side-panel separator is dragged (70 ms per column at N=300, 200 ms at N=900, with nothing changed except width). On top of that, every frame pays a fixed View cost of about 2.2 ms and 720 KB, more than half of whose allocations come from asking "is the prompt pane open?" the expensive way. One disk walk (the `@file` autocomplete listing) still runs inside `Update` and can freeze key handling. The good news: nothing grows faster than linearly with session length, scrolling is already flat in N, the paint cache and the token coalescing both work, and most fixes are local and output-preserving.

## Intent & Architecture Findings

### High — The `@file` autocomplete walks the workspace on disk inside Update `[Input latency]`

- **Where:** `internal/tui/filecache.go:48` (reached from `internal/tui/model.go:2325` `handleKey` → `recomputeAutocomplete` `autocomplete.go:216` → `computeAutocomplete` → `fileSuggestions` `autocomplete.go:667`) — scenario `type`
- **What:** When the user types an `@` token, `fileCache.suggest` calls `walkWorkspaceFiles` (`filecache.go:82`: `os.OpenRoot` + `fs.WalkDir` over every non-dot directory, including `node_modules`, `vendor` and `build`, up to 4,096 files, then a sort) directly on the Update goroutine whenever its 3-second cache has expired. That is the first `@` keystroke of a session and the first keystroke after any 3-second pause inside an `@` token. The code's own comment (`autocomplete.go:194-202`) states the rule this breaks: the skill catalogue re-scan was moved off the loop for exactly this reason.
- **Why it matters:** On a Pi with the workspace on an SD card and a cold page cache, the walk takes hundreds of milliseconds to seconds, and no key is handled until it ends. The profile's typing scenario typed no `@`, so it is not in the numbers. *(independently verified)*
- **Fix:** Run the walk as a `tea.Cmd`, the way `skillRescanCmd` (`autocomplete.go:252`) does. While the listing is stale or empty, filter what the cache holds now (empty on first use); mark a walk in flight on the shared `*fileCache` so a burst of keys dispatches one walk; land a `filesListedMsg` that swaps in the listing and re-derives the open menu with `reselectRow` (the `foldSkillsReloaded` pattern, `autocomplete.go:283`). Once the walk lands, the rows are the same as today. Pin it with a test that fails if `walkWorkspaceFiles` runs on the Update goroutine, plus a `tuitest` that types `@` and waits for the rows.
- **Status:** open

## Critical & High Findings

### High — Every keystroke repaints the whole transcript `[Profile]`

- **Where:** `internal/tui/model.go:2328` (`handleKey` → `layout`) → `model.go:3082` (`refreshViewport`) — scenario `type`
- **What:** Typing changes nothing in the transcript, yet each key ends in a full repaint: the whole-list paint walk plus the viewport's re-measure of every line.
- **Why it matters:** One key costs 3.36 ms with an empty session, 5.99 ms at N=48 and 16.21 ms at N=300 (about +40 µs per block); 42.5% of the typing profile is under `refreshViewport`. In a long session, fast typing on a Pi where the model also needs the CPU shows as visible key lag. *(confirmed by `go test -bench` + `pprof`)*
- **Fix:** Split `layout`'s geometry step from its repaint: recompute the input box's height and the viewport's height on every key, but call `refreshViewport` only when `m.frameKey() != m.painted` (a draft edit never moves the key). When only the input's row count changes, resize the viewport and re-park its offset without rebuilding the lines. Output is identical because an unchanged key renders identical lines.
- **Status:** open

### High — The viewport re-measures every transcript line on every repaint `[Profile]`

- **Where:** `internal/tui/model.go:3401` and `:3413` (`refreshViewport` → bubbles v2.1.0 `viewport.SetContentLines`, `viewport.go:233` → `maxLineWidth`, `viewport.go:759`) — scenarios `stream`, `type`, `fold`, `idle`, `blocks`
- **What:** `SetContentLines` runs `ansi.StringWidth` and a `strings.ContainsAny` scan over every line it is given, and `refreshViewport` gives it the whole scrollback on every repaint. apogee already knows these widths: `paintBlock` stores each block's widget widths (`cells`), and `reserveWidgetCells` guarantees no stored line is wider than the widget.
- **Why it matters:** This is why an all-hit repaint is linear in session length (4.38 → 14.82 → 40.57 ms at N=48 → 300 → 900, about 3.3 µs per painted line) even though the paint cache serves every block. It is 61% of the all-hit repaint, 45% of streaming, 37.5% of a fold toggle and 26% of typing. Every other repaint finding below shrinks once this is fixed. *(confirmed by `go test -bench` + `pprof`)*
- **Fix:** Stop handing the whole transcript to bubbles' width scan on every repaint. Either keep the scroll state in a small in-package scroller that takes the widest line from the stored per-block `cells` (adding only the separators, breadcrumb and streaming preview, which carry no stored width), or give the bubbles viewport only the visible window (the View-side change in "Each visible row is width-measured five to six times per frame" composes the visible rows from `m.lines` directly). Pin with `BenchmarkPerfAuditBlocks/repaint` at N=900 (expect the slope to fall well below 41 µs per block) and a byte-equality test of `View()` against today's path.
- **Status:** open

### High — Every streamed text batch repaints the whole transcript `[Profile]`

- **Where:** `internal/tui/model.go:3151` (`settleFrame`) → `layout` → `refreshViewport` — scenario `stream`
- **What:** Streamed text arrives in batches (the sink merges deltas into at most one message per 30 ms). Each batch changes only the live preview at the bottom, yet each one rebuilds the whole transcript's lines and re-measures them all.
- **Why it matters:** 3.84 ms per batch with an empty session, 16.31 ms behind 300 tool blocks; 69% of streaming CPU is under `refreshViewport`. At up to ~33 batches per second that is about half of one Pi core spent redrawing an unchanged scrollback while the local model is generating on the same CPU. *(confirmed by `go test -bench` + `pprof`)*
- **Fix:** Make the stream repaint incremental: keep the settled part of `m.lines` (and its widths) from the previous paint and replace only the preview's lines at the tail when the only thing that moved is the streaming buffer. This is the same "splice only what changed" seam the live-star finding below needs, so design it once. Pin with `BenchmarkPerfAuditStream` (session=300 should approach session=0) and an equality test against a cold full repaint.
- **Status:** open

### High — With /thinking open, every frame rebuilds and re-wraps the whole thinking board `[Streaming]`

- **Where:** `internal/tui/thinkingpane.go:154` (`thinkingRows`), `:253` (wrap memo), `reportpane.go:301` and `:316` — scenario `stream` (a reasoning model thinking, /thinking open and following the live record, which is what the pane is for)
- **What:** Three costs stack. (1) `thinkingRows` copies the wrapped rows of every retained record (up to 64 records) into fresh `rows`/`kinds` slices on every build, though the pane shows only 16 rows. (2) Every reasoning chunk changes the live record's text, so the wrap memo misses and `wrapThinkingText` re-wraps the whole live record (up to 64 KB) through `wrapReadable`. (3) The content is built twice per message: once for the height query in Update's tail (`reportHeight`) and once to draw it (`renderReport`). Also, `thinkingHeading` → `runLabel` → `runHead` walks the transcript once per sub-agent record per build.
- **Why it matters:** The existing `BenchmarkThinkingPaneUpdateAndView` costs 15.87 ms, 9.86 MB and 130,840 allocations per chunk at the cap; `thinkingRows` alone allocates 1.61 GB in that benchmark. At ~33 chunks per second that is roughly half of one Pi core while the model thinks. (The per-frame copy is of string headers, so it scales with the number of retained rows rather than with 64 KB per record.) *(independently verified)*
- **Fix:** (1) `wrapThinkingText` already wraps each `\n`-separated line on its own, so memoise per line: keep the rows of the record's completed lines and re-wrap only the trailing partial line (and the first line when `keepLastBytes` trims the front). (2) Memoise the composed `rows`/`kinds` in `thinkingRowCache`, checked against the same record texts plus column and scope, so the height query and the paint in one frame share one build. (3) Cache the run label per run. Output unchanged. Pin with the existing `wraps` counter and `BenchmarkThinkingPaneUpdateAndView`.
- **Status:** open

### High — Dragging the side-panel separator does a cold whole-transcript repaint per column `[Session scale + Profile]`

- **Where:** `internal/tui/model.go:3156` (`settleFrame`), `internal/tui/paintcache.go:444` (`frameKey.sidePanelWidth`) and `:466` (width in every block key), `internal/tui/sidepanel.go:1302` (`panelDragResized`) — scenario `resize`
- **What:** Each mouse-motion event that moves the separator by one column changes the transcript width. Width is part of every block's cache key, so every block misses and the whole transcript is re-drawn from scratch, once per column. Bubble Tea does not merge motion messages, and nothing in apogee does either (only token deltas are coalesced).
- **Why it matters:** A cold repaint costs 13.9 / 69.8 / 200.3 ms at N=48 / 300 / 900 (44% of it in `renderSuperGroup`, 26% in `leaderRowIn`). A 20-column drag at N=300 blocks Update for about 1.4 s, at N=900 about 4 s: the separator trails the pointer and keys queue behind it. Dragging is an occasional deliberate gesture, which is why this sits below the per-key and per-delta items. *(confirmed by `go test -bench` + `pprof`)* *(independently verified)*
- **Fix:** Coalesce, do not remove, the live re-wrap. On a width-changing motion, record the target width only, keep drawing at `m.painted.sidePanelWidth`, and return a Cmd that sends a `panelDragSettle` message; that message lays out at the *latest* recorded width, and later settles find the key already current and do nothing. A burst of queued motion then costs one cold repaint instead of one per column; every frame drawn is still one of today's frames, only intermediate widths are skipped. Check this against `adr-0110-amend-1108` (e), which ratifies live re-wrap per column (pinned by `TestMouseMotionRepaintsADraggedSeparator`); if skipping intermediate widths is judged to break that rule, the rule warrants revisiting. The cheaper cold paint from "Paint helpers pay for width passes they do not need" also lowers each step. The structural remedy (paint the visible rows at the new width first, the rest lazily) is a candidate for `/improve-codebase-architecture`.
- **Status:** open

### High — The footer walks every transcript entry on every frame to sum sub-agent spend `[Render frame + Input latency]`

- **Where:** `internal/tui/usage.go:278` (`delegateUsageHeads`) via `delegateUsageTotal` (`usage.go:302`) ← `footerLeftText` (`internal/tui/model.go:4493`) ← `footerContent` (`model.go:4189`) ← `View` — scenarios `idle`, `type`, `scroll`, `blocks`
- **What:** To show sub-agent spend in the footer, `delegateUsageHeads` walks every entry in the transcript, copies each one by value (`head := m.transcript.entries[i]`; an `entry` embeds a whole `toolView`) and appends matches to two fresh slices, just to add up their usage. It runs once per footer row per View and again for each mouse hit-test (`footerModeSpan`/`footerGitSpan`, `model.go:4550`/`4562`). The total changes only when a usage reading lands.
- **Why it matters:** It is the one transcript-sized walk on View's path, so it is the part of the fixed frame cost that grows with session length (View alone: 2.11 → 2.20 → 2.67 ms at N=48 / 300 / 900). At N=300 it is 4.5% flat / 5.5% cumulative of View (about 0.12 ms per frame), paid at the spinner's 10–20 frames per second for the whole of every turn. Small today, but it never stops growing. *(independently verified)*
- **Fix:** Keep the delegate total as a value updated where `applyUsage` and workflow-spend writes land, or memo it behind a pointer keyed on `transcript.generation` plus the workflows' spend state (the ADR 0011 pointer-memo pattern), and have `footerLeftText` read it. Where a walk remains, iterate by index without copying entries and without building slices. Same sum, same output. Pin with `BenchmarkPerfAuditBlocks/view` at N=900 and an allocation count on `footerContent`.
- **Status:** open

## Medium Findings

### Medium — Asking "is the prompt pane open?" allocates over half of every frame's memory `[Render frame + Profile]`

- **Where:** `internal/tui/model.go:3342` (`draftRowsCeiling`), `internal/tui/panes.go:345` (`reportPaneRow` open closure), `model.go:5504` (`openPanes`) — scenarios `idle`, `scroll`, `type`, `stream`
- **What:** `draftRowsCeiling` checks one pane with `m.openPanes().has(panePrompt)`, which evaluates all eleven panes' `open` closures; the five report-pane closures each copy the whole `Model` to the heap. View reaches it at least three times per frame (`inputView` → `inputScrollbarShown`; `hiddenDraftRows` → `inputInnerWidth`; `promptCursor` → `inputContentRect`), and `inputScrollbarShown` asks it even for a one-row draft that cannot overflow.
- **Why it matters:** With no pane open, `reportPaneRow.func1` is 55% of View's 723 KB per frame, 42% of all streaming bytes and 50–56% in typing, scrolling and idle; `inputScrollbarShown` is 6.2% and `draftRowsCeiling` 5.9% of View's CPU. On a Pi, that garbage is collector work competing with the local model at 10–20 frames per second. *(confirmed by `go test -bench` + `pprof`)*
- **Fix:** Replace `m.openPanes().has(panePrompt)` with `m.promptOpen()` (the pane row's own predicate, `panes.go:131`/`371`; same boolean, no escaping closure). In `inputScrollbarShown`, return false before computing the ceiling when `contentRows(full) <= minInputRows` (the ceiling never goes below `minInputRows`, `model.go:3347`, so the comparison cannot be true). Separately, make the report-pane `open` closures stop copying the Model (they only read `m.reportState(r).open`). Pin with an allocs-per-op assertion on a View of an unchanged model.
- **Status:** open

### Medium — Each visible row is width-measured five to six times per frame `[Render frame + Leaf primitives + Profile]`

- **Where:** `internal/tui/model.go:3764` (`vp.View()`), `internal/tui/boxdraw.go:183-224` (`joinScrollbar`, `joinColumn`), `boxdraw.go:237-245` (`joinFrame`), `boxdraw.go:276` (`sealRowEdges`) — scenarios `idle`, `scroll`, `stream`, `blocks`
- **What:** On the way to the screen each visible transcript row is measured again and again: bubbles `viewport.View` pads the visible lines through `lipgloss.NewStyle().Width().Height().Render` plus a second `Style.Render` (wrap, align and two margin passes, each a full width measure); `joinScrollbar` squares every row to width; `joinColumn` squares it again when the side panel is shown; `joinFrame` squares the whole frame once more and `sealRowEdges` re-parses every row carrying a background colour. All of this although `refreshViewport` already held every stored line to the viewport width.
- **Why it matters:** This sets the fixed View cost of about 2.2 ms per frame, paid on every message even when nothing changed (a settled spinner tick costs 2.7 ms). In the View profile: `lipgloss.Style.Render` 36.7%, `viewport.Model.View` 24.6%, `joinFrame` 14.9%, `squareLine` 13.2%, `lipgloss.Wrap` 12.1%, `sealRowEdges` 8.3%, `joinScrollbar` 8.0%. *(confirmed by `go test -bench` + `pprof`)*
- **Fix:** (a) Have `joinFrame` skip `squareLine` for blocks whose rows `joinScrollbar`/`joinColumn` already squared to exactly `m.width` (pass the known width or a "pre-squared" flag with the block; `squareLine` on an exactly-wide row returns it unchanged). (b) Memo `sealRowEdges` for chrome rows that repeat frame to frame (status, footer, input box) in a pointer-held memo keyed by line and width. (c) Compose the transcript body from the visible slice of `m.lines` (`YOffset` to `YOffset+height`, padding a short tail with blank rows) with one `squareLine` per row, instead of `vp.View()`. Step (c) must be proven byte-identical on rows with VS16 emoji (lipgloss pads in grapheme width, then `squareLine` corrects it): add a test comparing `View()` frames before and after over a transcript with a `⚠️` row and a short tail, and pin the gain with `BenchmarkPerfAuditBlocks/view`.
- **Status:** open

### Medium — The blinking live star repaints the whole transcript twice a second `[Render frame + Session scale + Input latency]`

- **Where:** `internal/tui/paintcache.go:445` (`frameKey.blink`), `internal/tui/spinner.go:358` and `:464-484` (`starBlinkHalfPeriod`, `foldSpinnerTick`), `internal/tui/transcript.go:2093-2119` (`hasOpenToolCall`/`hasLiveStar`) — scenario `idle` with a tool call open (a build, `go test`, a working sub-agent: often minutes on a Pi)
- **What:** While a call runs, its header star blinks with a 500 ms half-period. The blink phase is part of the frame key, so every flip makes `settle` run a full repaint (`layout` → `refreshViewport`: whole-list walk, `reserveWidgetCells`, `SetContentLines` re-measure) to change one glyph on one header. On top of that, `frameKey()` itself scans every entry backwards twice (`hasOpenToolCall`, then the workflow scan) on every Update while the phase is on.
- **Why it matters:** An average spinner tick with a live call costs 3.28 → 5.89 ms at N=48 → 300, against a flat 2.72 ms with nothing live; each flip is a full all-hit repaint, about 40 ms at N=900, twice a second for as long as the call runs (about 8% of a Pi core, plus a 40 ms key stall each time). Fixing the viewport re-measure removes most of this, but the whole-list walk (about 15% of a repaint) stays per flip. *(confirmed by `go test -bench` + `pprof`)*
- **Fix:** When the only frame-key field that moved is `blink`, repaint only the blocks whose paint key carries `live` and splice their lines into `m.lines` in place (the star and its blank are the same width, so no line count or width changes and the viewport needs no re-measure); or apply the phase as a View-time overlay on the live header's line, the way `highlightBlockCursor` overlays the cursor bar. Cache `hasLiveStar` against `transcript.generation`. The splice is the general incremental-repaint seam (shared with the streaming finding) and a candidate for `/improve-codebase-architecture`. Pin with paint-cache miss and `widgetMeasures` counters across a flip tick on a 300-block session.
- **Status:** open

### Medium — The streaming preview is re-rendered as markdown from scratch on every batch `[Streaming + Leaf primitives]`

- **Where:** `internal/tui/render.go:441-445` (`paintPreview`) → `internal/tui/markdown.go:40` (`renderMarkdownBody`) — scenario `stream`
- **What:** The live preview is the one block the paint cache cannot serve. On every repaint it hands up to 256 raw lines (`previewTailLines`) to `renderMarkdownBody`, which splits, collapses blank runs, styles inline spans and wraps every line, then `withMarker` and `railLines` copy every line again; its lines carry no stored widths, so `reserveWidgetCells` re-measures them too. A batch usually changes only the last raw line.
- **Why it matters:** `renderMarkdownBody` is 9.2% of streaming CPU (about 1.1 ms per batch), `renderCodeBlock` 4.6%. With an empty session, the cost per batch rises from 3.10 ms to 3.87 ms as the preview fills to its 256-line cap, so this is about 30% of a batch's cost early in a session.
- **Fix:** Keep a pointer-held preview memo of rendered rows (ADR 0011 pointer rule, like `paints`), cleared when the preview restarts. Key it per raw line, or per segment split at blank lines outside a code fence, by its text, the fence state it opens in, width, depth and theme. Re-render only the growing last segment, any line that could start a pipe table (`matchTableBlock` looks ahead), and the first segment when the 256-line cut moves. Store the preview's measured cells with it so the reserve stops re-measuring. Pin with an equality test against a cold `renderMarkdownBody` over a streamed reply with fences, a table and a sliding window, and extend `BenchmarkRenderViewStreaming` to a growing 32 KB tail.
- **Status:** open

### Medium — Each keystroke arms its own debounce tick, doubling the frame count while typing `[Input latency]`

- **Where:** `internal/tui/suggestband.go:76-85` (`scheduleSkillHints`, called from `recomputeAutocomplete`, `autocomplete.go:225`) — scenario `type`
- **What:** Every edit arms a fresh `tea.Tick(skillHintDelay)` (150 ms), so every keystroke produces a second message 150 ms later. All but the last of a burst are stale and `foldSkillHintTick` (`:124`) changes nothing, but each still runs Update's whole tail and a full View.
- **Why it matters:** Typing N keys costs N key frames plus N tick frames, each tick frame at least the ~2.2–2.7 ms View floor (more in a long session). The profile's typing benchmark drops returned commands, so these ticks never landed there.
- **Fix:** Keep one debounce timer in flight: an edit records the due time (`now + skillHintDelay`) and bumps the generation, and arms a tick only when none is pending; a tick that lands before the due time re-arms for the remainder, otherwise it ranks. The ranking still runs once, at least 150 ms after the last edit, so the band is unchanged; a burst costs about (duration / 150 ms) tick frames instead of one per key. Pin with a test counting `skillHintTickMsg`s from a 20-key burst.
- **Status:** open

### Medium — A long draft is re-read and re-wrapped in full many times per keystroke `[Input latency]`

- **Where:** `internal/tui/editorgeometry.go:200-210` (`inputContentRows`) via `internal/tui/prompteditor.go:488-503` (`rowCountMemo.rows`); `internal/tui/model.go:3264-3358` (`m.input.Value()` call sites); `internal/tui/inputaccent.go:94-130` and `:146-176` (`accentTokens` → `inputCellSpans`) — scenario `type` with a long draft (a pasted log or stack trace), and `idle` while a draft sits in the box during a run
- **What:** Three costs that all scale with the whole draft. (1) The row-count memo is keyed on the whole draft string, so every edit misses it and re-wraps every logical line, once per width asked (two widths when the box's scroll bar shows). (2) The draft is rebuilt from the textarea with `m.input.Value()` (one allocation per logical line) roughly 15–20 times per keystroke across `handleKey`, autocomplete, `layout`, `freshenTranscriptClamp` and View; each memo hit then byte-compares the fresh copy. (3) On every View, for each `/skill` or `@file` token in the draft, `inputCellSpans` splits the whole value and re-wraps every line before the token to find its row, even for tokens scrolled out of the box.
- **Why it matters:** `BenchmarkPromptKeyLongDraft` costs 23.25 ms and 29,859 allocations for one key at the end of a 400-line draft; `BenchmarkInputContentRows` costs 8.75 ms per recount of a 64 KiB draft. A draft with a dozen `@` references re-wraps a dozen times per frame, 10–20 frames a second while a run spins.
- **Fix:** Keep the row count per logical line (a pointer memo of line content → rows at a width, or a slice aligned with the textarea's lines) so an edit re-wraps only the changed line and the total is a sum; build the draft's row-start table once per View from it and map every accent token through it; read `m.input.Value()` once per Update and once per View and pass it down (or hold it in a pointer memo keyed by an edit counter bumped at the textarea write sites). Output identical. Pin with a `wrapRowStarts` call counter per key in a 400-line draft (expect 1–2 per width, not 400) and per View for a draft with N tokens.
- **Status:** open

### Medium — Paint helpers pay for width passes they do not need `[Leaf primitives]`

- **Where:** `internal/tui/markdown.go:127`, `:156`, `:178`/`:186`/`:194`; `internal/tui/toolleader.go:199-206`; `internal/tui/wrap.go:58` and `:229` (`wrapText` cap pass); `internal/tui/toolblock.go:528-529` (`indicatorRow`); theme roles at `internal/tui/theme.go:588` — scenarios `blocks` (every cold paint: resize, separator drag, live-block repaint), `stream` (the preview)
- **What:** Two kinds of wasted measuring. (1) The theme roles are bare colour styles (`lipgloss.NewStyle().Foreground(code)`, no width, padding, margin or border), yet each `Style.Render` runs lipgloss's whole block pipeline, whose `applyMargins` → `getLines` measures the full display width of the output unconditionally (lipgloss v2.0.4), even with zero margins. This is paid per styled span, per line. (2) `indicatorRow` re-measures a leader row that `leaderRowIn` built to an exact, already-computed width, and `wrapText` measures every wrapped line to enforce the cap even when its byte length already proves it fits.
- **Why it matters:** `Style.Render` is 39.7% of a cold repaint and 14.2% of streaming; `applyMargins` → `getLines` alone is 14.3% of resize and 6.1% of idle. `renderCodeBlock` spends 87% of its time in `Render`. The `indicatorRow` re-measure is 5.8% of the resize profile (45% of all width-measuring there); the `wrapText` cap pass is 34–41% of `wrapText`'s own time. These multiply directly into the 70 ms / 200 ms cold repaints.
- **Fix:** (1) Give the bare colour roles a precomputed open/close escape pair built once with the shared `*themeStyles`, and paint single-line spans through a small helper; or set `.Inline(true)` on roles whose input is always one line (with no width, padding or border, Inline skips only the border/margin passes, so the bytes match). Prove it with a test rendering every theme role both ways over a corpus including tabs and pre-styled text. (2) Return the cell count from `leaderRowIn`/`leaderRow` and pass it to `indicatorRow`; in `wrapText`, test `len(ln) <= limit` before calling `th.measure.Width(ln)` (exact, since a display cell needs at least one byte, the same argument `measuredCells`, `render.go:662`, relies on). Pin with `BenchmarkPerfAuditBlocks/resize` and `BenchmarkRenderViewStreaming` cold.
- **Status:** open

### Medium — Every repaint regrows five line slices from nil `[Session scale]`

- **Where:** `internal/tui/render.go:250-256` and `:320-335` (`renderView`'s `appendJoined`) — scenarios `blocks`, `stream`, `type`, `fold`
- **What:** Each repaint rebuilds `lines`, `targets`, `cells`, `pads` and `padSeps` from nil with no capacity hint, so each grows by repeated doubling and copying over the whole transcript's line count.
- **Why it matters:** `renderView.func1` is 44.9% of an all-hit repaint's allocated bytes at N=300 (about 0.7 MB per repaint) and 21.8% of all streaming bytes; every repaint's garbage is collector work on the CPU the model needs.
- **Fix:** Allocate all five with `make(..., 0, hint)`, where the hint is the previous paint's line count (`len(m.lines)`, passed in or kept on the transcript beside `paints`). Identical output. Pin with `-benchmem` on `BenchmarkPerfAuditBlocks/repaint`.
- **Status:** open

### Medium — A tool group's shape is worked out three to four times per repaint `[Session scale]`

- **Where:** `internal/tui/render.go:908`, `:959`; `internal/tui/blockfold.go:178`, `:223`, `:233`, `:252`; `internal/tui/blockshape.go:78`, `:84` — scenario `blocks` (all-hit repaint of a tool-heavy session)
- **What:** For each Tools group, the shape (`toolBlockAt`) is derived in `resolveBlock`, again in `umbrellaIsLarge`, again via `blockFolded` → `blockFoldBy`, and again via `blockFoldBy` → `umbrellaIsLarge`. Each derivation re-walks the run through `sameLabelRun`, which copies whole `entry` values, and allocates a fresh `superGroup` slice; `blockSettled` then walks the span once more.
- **Why it matters:** In the N=300 repaint profile, `resolveBlock` is 5.7%, `toolBlockAt` 4.5% and `sameLabelRun` 3.0% flat: about a third of `renderView`'s own cost, and the largest part of it once the viewport re-measure is gone.
- **Fix:** Add variants of `umbrellaIsLarge`, `blockFoldBy` and `blockFolded` that take the already-resolved `toolBlock` (`resolveBlock` and `resolveGroup` hold it as `b`), and iterate `&entries[j]` in `sameLabelRun`, `subAgentSpan` and `blockSettled` instead of copying entries. Pure refactor, identical output. Pin with `BenchmarkPerfAuditBlocks/repaint`.
- **Status:** open

### Medium — Moving the block cursor or jumping from the side panel repaints an unchanged transcript `[Session scale]`

- **Where:** `internal/tui/blockcursor.go:359` and `:364` (`followBlockCursor`), `internal/tui/sidepanel.go:1435-1444` (`revealEntry`), `internal/tui/model.go:3439` (`refreshViewportAnchored`) — scenarios `scroll`, `fold` (navigation-mode arrow keys walking past the screen edge; a side-panel jump to a block)
- **What:** A move that changes only the scroll offset goes through `refreshViewportAnchored`, which always calls `refreshViewport` even though `frameKey()` still equals `m.painted`. One cursor step off-screen costs one or two whole repaints (the sticky-header retry is the second); `revealEntry` costs three where one is needed.
- **Why it matters:** An all-hit repaint is 14.8 ms at N=300 and 40.6 ms at N=900, so one panel jump at N=900 is about 120 ms, about 80 ms of it repainting an unchanged paint.
- **Fix:** Split out a `parkAt(line, row)` that only sets the offset (`SetYOffset`, `floorShortScreenOffset`, the `detached` re-derivation), and have `refreshViewportAnchored` call `refreshViewport` only when `m.frameKey() != m.painted` or the widget height is stale; `followBlockCursor` and the second and third calls in `revealEntry` then only park. Identical output: an unchanged key renders identical lines. Pin with a repaint counter across a cursor step and a panel jump.
- **Status:** open

### Medium — The side panel re-derives and repaints every section on every frame `[Render frame]`

- **Where:** `internal/tui/sidepanel.go:622-646` (`panelSectionsIn`), `internal/sidepanel/sidepanel.go:602-633` (`State.ChangedFiles`), `internal/tui/panelframe.go:184` (`newPanelFrame`, called from `model.go:3802`) — scenarios `idle`, `blocks`, `stream`, whenever the panel is shown (`ui.side-panel.show: auto` is the default on wide windows)
- **What:** Every View builds a fresh panel frame: it re-parses the `ui.side-panel.sections` setting, calls `State.ChangedFiles`, which walks every write the session ever landed (the log is append-only) and builds two maps, then paints every row of every section (`stripEscapes`, path shortening, `trimPathLeft` re-measuring each path component, `lipgloss` renders) before `panelLayout` keeps only the rows that fit. A mouse hit-test (`panelHitAt`, `panelframe.go:263`) builds the frame again.
- **Why it matters:** The cost scales with session state (landed writes, changed files, tasks, sub-agent runs: typically tens to hundreds) on every frame, spinner ticks included, with no memo. It was not measured: the profile ran with an empty panel. *(independently verified; severity lowered from High because the multiplier is session state, not transcript size)*
- **Fix:** Memo the painted sections behind a pointer keyed on the panel state's identity (`m.panel` is replaced copy-on-write on every write, so compare the pointer), the content width, the fold/opened lists, the spinner frame (sub-agent rows only) and the viewed run; and/or have `State` maintain the per-path rows incrementally instead of re-deriving them from the whole write log. Paint only the rows `panelLayout` keeps. First measure it: add ~300 edit calls over ~150 paths to the benchmark fixture with the panel shown at 160 columns and compare `BenchmarkPerfAuditBlocks/view` panel-on against panel-off; pin with the existing `panelSectionPaints` counter.
- **Status:** open

### Medium — The scroll bar renders the same two glyphs once per row `[Leaf primitives]`

- **Where:** `internal/tui/boxdraw.go:168-170` (`scrollbarCells`; called for the transcript bar from `model.go:4032` and the prompt-box bar from `model.go:4089`) — scenarios `scroll`, `stream`, `idle`, `blocks`
- **What:** The bar is made of two constant strings, yet `th.scrollThumb.Render(glyphScrollThumb)` / `th.scrollTrack.Render(glyphScrollTrack)` run once per bar row per View, each through lipgloss's full pipeline including a width measure.
- **Why it matters:** About 40 lipgloss renders per frame on every message; `scrollbarCells` is 9.4% of the frame's `Style.Render` callers, about 3.5% of View's CPU.
- **Fix:** Render thumb and track once before the loop (or once per theme in `*themeStyles`) and reuse the two strings. Byte-identical by construction; a one-line change. Pin with an allocation assertion on a View with a 40-row bar.
- **Status:** open

### Medium — Collapsed sub-agent runs build cache keys over every hidden entry on every repaint `[Session scale]`

- **Where:** `internal/tui/render.go:957`, `:1002`, `:1145` (`appendInputs` in `resolveBlock`/`resolveGroup`), `internal/tui/paintcache.go:124` (`func (e entry) painted()`, value receiver), `paintcache.go:474-475` (`spanFlags`/`spanFills`) — scenarios `blocks`, `stream`, `fold` (any repaint in a session that used sub-agents)
- **What:** Before it knows whether a block is a cache hit, every repaint builds the block's key by creating one ~832-byte `paintInput` per entry the block covers, each from a full by-value copy of the `entry`. A collapsed Sub-agent run or Sub-agents group covers its delegates' whole hidden spans, so all their entries are copied and scanned, and `spanFlags` allocates a string as long as the span, although none of them draws a row.
- **Why it matters:** Cost is proportional to the number of entries inside collapsed delegations, per keystroke, per streamed batch and per blink flip. The magnitude is unmeasured: the benchmark fixture had no sub-agents. *(independently verified)*
- **Fix:** Compute the key's per-entry terms (head kind and depth, span length, flag bytes, fill readings) by a pointer walk over `t.entries[head:end]` with no record copy, make `painted` take `*entry`, and build the `paintInput` records only inside `draw`, i.e. on a cache miss. Key fields unchanged, so output identical. Measure first with a repaint benchmark over a resumed session holding a Sub-agents group of 4 delegates × 300 nested calls.
- **Status:** open

Not listed (checked and judged minor): `reserveWidgetCells`' slow branch (`internal/tui/render.go:557`) re-copies the whole line list on every repaint once any scrollback row overruns the widget width (rows full of VS16 emoji on a terminal without mode 2027); it is real but costs about one extra copy of the line assembly and needs a rare trigger.

## Profile

Measured on a Raspberry Pi 5 (4× Cortex-A76, 8 GB), Go 1.26.11 linux/arm64, `GOMAXPROCS=4`, one benchmark process at a time, no race detector. Every operation goes through the real `Model.Update` and `Model.View` over a session loaded through the `--resume` path in a 120×40 window. The benchmark source is kept, for reuse as the "before" baseline, at `docs/skill-runs/code-audit/2026-11-23/profile/zz_perfaudit_bench_test.go.txt`, next to the raw `.prof` files and `pprof` text for every scenario; that directory is gitignored, so it exists only in the checkout where the audit ran. To reuse it, copy it to `internal/tui/zz_perfaudit_bench_test.go` and run one benchmark at a time, e.g. `go test -run '^$' -bench PerfAuditBlocks -benchmem ./internal/tui/` (single package, no `-race`).

| scenario | cost per op | allocated per op | note |
|---|---|---|---|
| stream, empty session / 300 blocks | 3.84 / **16.31 ms** per delta | 1.51 / 2.36 MB | one delta per message (the uncoalesced upper bound) |
| all-hit repaint, N=48 / 300 / 900 | 4.38 / 14.82 / **40.57 ms** | 0.88 / 1.63 / 3.51 MB | linear, ~41 µs per block |
| cold repaint (resize), N=48 / 300 / 900 | 13.9 / 69.8 / **200.3 ms** | 3.0 / 7.4 / 17.7 MB | linear, ~220 µs per block |
| keystroke, N=0 / 48 / 300 | 3.36 / 5.99 / **16.21 ms** | 2.22 / 2.40 / 3.12 MB | short draft |
| fold toggle, N=48 / 300 | 6.79 / **17.03 ms** | 2.68 / 3.42 MB | one `enter` on a tool header |
| View alone, N=48 / 300 / 900 | 2.11 / 2.20 / 2.67 ms | ~720 KB | unchanged model |
| PgUp/PgDn, wheel | 3.0–3.4 ms | 1.7–2.1 MB | flat in N: no repaint |
| spinner tick, settled / live call (N=300) | 2.72 / 5.89 ms | 1.36 / 1.55 MB | live ticks include blink repaints |
| /thinking open, one chunk at cap | 15.87 ms | 9.86 MB | existing `BenchmarkThinkingPaneUpdateAndView` |
| key at end of 400-line draft | 23.25 ms | 4.15 MB | existing `BenchmarkPromptKeyLongDraft` |

Top entries by scenario (share of that profile's CPU unless marked):

- **stream:** `refreshViewport` 69.1% cumulative; `viewport.SetContentLines` 48.8%; `viewport.maxLineWidth` 44.9%; `renderView` 19.6%; `Model.View` 18.1%; `renderMarkdownBody` 9.2%. Flat: grapheme iteration and `ansi.stringWidth` dominate. Bytes: `reportPaneRow.func1` 42.0%, `renderView.func1` 21.8%.
- **all-hit repaint, N=300:** `refreshViewport` 81.1%; `SetContentLines` 66.4%; `maxLineWidth` 61.1%; `renderView` 14.7%; `resolveBlock` 5.7%; `toolBlockAt` 4.5%; `sameLabelRun` 3.0% flat. Bytes: `renderView.func1` 44.9%, `reportPaneRow.func1` 26.3%.
- **View alone, N=300:** `lipgloss.Style.Render` 36.7%; `viewport.Model.View` 24.6%; `inputView` 14.9%; `joinFrame` 14.9%; `squareLine` 13.2%; `lipgloss.Wrap` 12.1%; `sealRowEdges` 8.3%; `joinScrollbar` 8.0%; `footerContent` 7.3%; `inputScrollbarShown` 6.2%; `draftRowsCeiling` 5.9%; `delegateUsageHeads` 4.5% flat. Bytes: `reportPaneRow.func1` 55.3%.
- **cold repaint (resize), N=300:** `refreshViewport` 91.3%; `renderView` 76.4%; `paintBlock` 73.6%; `renderSuperGroup` 43.6%; `lipgloss.Style.Render` 39.7%; `leaderRowIn` 26.4%; `renderEntryLines` 20.0%; `SetContentLines` 14.9%.
- **typing:** `handleKey` 47.9%; `layout` 45.8%; `refreshViewport` 42.5%; `SetContentLines` 28.9%; `Model.View` 26.9%. Bytes: `reportPaneRow.func1` 50.5%.
- **idle:** `Model.View` 53.9%; `lipgloss.Style.Render` 28.1%; `settle` 19.8%; `refreshViewport` 16.5% (the blink repaints). Bytes: `reportPaneRow.func1` 55.9%.

Not covered by the profile: the terminal renderer's own frame diff and write; the real 30 ms coalescing in front of the stream (each delta was its own message, the upper bound); sub-agent blocks and run views; the side panel with live content; open report panes other than the existing /thinking benchmarks; approval panes, the autocomplete menu, drag-selection and wide-character content. No scenario grew faster than linearly with session length.

## Suggested plan items

One line per finding group; every fix must keep the drawn output byte-identical, proven against a cold render (a nil paint cache is the reference).

1. **Baseline** — restore the benchmark from `profile/zz_perfaudit_bench_test.go.txt` as a kept `internal/tui` benchmark file so every item below has a before/after number.
2. **`type`: no repaint on a draft edit** — run `refreshViewport` only when the frame key moved; pin with a `tuitest` + `stubllm` session of 300 blocks asserting zero paint-cache lookups across a typed key, and `BenchmarkPerfAuditType/N=300`.
3. **`stream`/`type`/`fold`/`idle`: no whole-scrollback width scan** — take the widest line from stored block `cells` (or hand the widget only the visible window); pin with `BenchmarkPerfAuditBlocks/repaint/N=900` and a `View()` byte-equality test.
4. **`stream` + `idle`: incremental repaint seam** — splice only the preview tail on a stream batch and only `live` blocks on a blink flip; pin with `BenchmarkPerfAuditStream` (session=300 near session=0), a miss counter across a flip, and an equality test against a full repaint. Design first (candidate for `/improve-codebase-architecture`).
5. **`type`: `@file` listing off the Update goroutine** — Cmd plus `filesListedMsg`; pin with a test that fails if `walkWorkspaceFiles` runs inside Update and a `tuitest` that types `@` and waits for the rows (via `internal/testtime`, no fixed sleep).
6. **`stream`: /thinking per-line wrap memo and shared row build** — pin with the `wraps` counter and `BenchmarkThinkingPaneUpdateAndView`.
7. **`resize`: coalesce separator-drag repaints** — after checking `adr-0110-amend-1108` (e); pin with a `tuitest` drag of 20 columns asserting one cold repaint per settled burst, keeping `TestMouseMotionRepaintsADraggedSeparator` green.
8. **`idle`/`type`/`scroll`: frame-floor quick wins** — `promptOpen()` instead of `openPanes().has`, early return in `inputScrollbarShown`, scroll-bar glyphs rendered once, footer delegate total memoised; pin with an allocs-per-op assertion on a View of an unchanged model and `BenchmarkPerfAuditBlocks/view/N=900` flat.
9. **`idle`/`scroll`: one width measure per visible row** — pre-squared rows through `joinFrame`, chrome `sealRowEdges` memo, body composed from `m.lines`; pin with a VS16 byte-equality test and `BenchmarkPerfAuditBlocks/view`.
10. **`stream`: memoised preview markdown** — pin with an equality test against a cold `renderMarkdownBody` (fences, table, sliding window) and `BenchmarkRenderViewStreaming` with a growing tail.
11. **`blocks`/`resize`: cheaper paint leaves** — bare-colour span painter or `Inline(true)`, `indicatorRow` width passed in, `wrapText` byte-length check, capacity hints on the five line slices, resolved `toolBlock` passed to the fold helpers; pin with a theme-role byte-equality corpus test and `BenchmarkPerfAuditBlocks/resize` and `/repaint`.
12. **`type`: per-line draft geometry** — per-line row-count memo, one row-start table per View, one `Value()` per Update/View, one debounce timer in flight; pin with `wrapRowStarts` call counters, a `skillHintTickMsg` count over a 20-key burst, and `BenchmarkPromptKeyLongDraft`.
13. **`scroll`/`fold`: park without repainting** — `parkAt` split from `refreshViewportAnchored`; pin with a repaint counter across a cursor step and a side-panel jump.
14. **`idle`: side-panel section memo** — measure first with a panel-on fixture, then memo on the panel state pointer; pin with `panelSectionPaints`.
15. **`blocks`: key building without entry copies for collapsed sub-agent runs** — measure first with a 4×300 delegation fixture, then pointer walk and `*entry` receiver; pin with `-benchmem` on that benchmark.

## Recommended Action Order

1. **Restore the benchmark baseline** (plan item 1) so every change has a before/after number on the Pi.
2. **Quick wins, one small commit each:** `promptOpen()` and the `inputScrollbarShown` early return, scroll-bar glyphs rendered once, capacity hints on the five slices, the `wrapText` byte check and `indicatorRow` width, the footer delegate memo. Together they remove over half of every frame's allocations and trim the repaint.
3. **Stop repainting on keystrokes**, then **remove the whole-scrollback width scan.** These two cut typing, streaming, folding and the blink by the largest margin, and they shrink every later item, so re-measure before deciding how far to take items 4–5.
4. **Move the `@file` walk off the Update goroutine** and **memoise the /thinking pane**: both are local changes following existing patterns in the same files.
5. **Design the incremental repaint seam** (stream preview splice, live-star splice, park-without-repaint) once, as a candidate for `/improve-codebase-architecture`, rather than three ad hoc splices; the separator-drag structural remedy belongs to the same discussion, and the drag coalescing should be checked against `adr-0110-amend-1108` (e) first.
6. **The View composition rework** (one measure per visible row) needs the VS16 byte-equality test written first.
7. **Measure before fixing** the side-panel memo and the sub-agent key building: both are verified on the code path but unmeasured.

## What Looked Good

Scrolling (PgUp/PgDn and the wheel) never repaints and costs the same at any session length. The paint cache works as designed: a warm transcript render is 0.066 ms against 1.56 ms cold, and a fold toggle invalidates only the toggled block. The token path is sound: the sink coalesces deltas into at most one message per 30 ms with natural back-pressure, `streamBuf` replaced the old quadratic string append, the preview is bounded to 256 lines, and tool output lands whole rather than streaming. The tick chains (spinner, git status, heartbeat, watch poll) are generation-guarded so none duplicate, and git, the clipboard probe and the skill re-scan already stay off the loop. The leaf helpers do no per-call regex compiles or renderer construction, sanitisation happens once at ingest, and diffs are parsed once and painted only on a cache miss. Nothing measured grows faster than linearly with session length.
