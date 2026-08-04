# Click-toggle responsiveness + streaming render performance

- **Goal:** Fix ISSUES.md item "Clicking on the header of a tool for collapsing/expanding
  is not very responsive … connected to 100% GPU usage when an agent is running." Two
  logical click bugs make the toggle silently miss while streaming; separately, every SSE
  token re-renders the entire transcript from scratch, which is the 100% CPU/GPU. Items
  1–2 fix the misses outright; items 3–4 remove the hot loop; item 5 closes out the docs.
- **Date:** 2026-08-03 · **Status:** not started
- **Pinned baseline:** HEAD `02c49bf` — all file:line references below are against this
  commit and may drift; treat symbol names as authoritative over line numbers.
- **Authoritative sources (precedence over item text — deviations leave a dated NOTES
  line under the item):**
  - ADR 0011 + `internal/tui/doc.go` — the Bubble Tea `Model` is copied by value on every
    `Update`; shared mutable state lives behind the entries backing array / pointers, and
    no `strings.Builder` (or other no-copy type) may be held by value.
  - `internal/tui/sink.go:9-19` doc comment — the pre-authorized coalescing design:
    "coalesce adjacent TokenEvents within this sink (concatenate their text in a short
    window) — coalescing, never dropping — behind this same interface."
  - `internal/tui/mouse.go:71-94` — selection design docs: content-anchored coordinates;
    `spanUnchanged` exists so a highlight never shows stale text.
  - ADR 0031 — wire-silent engine: nothing here may add engine-side events or behavior.
  - `layout.md` § "Collapsed and expanded blocks" — toggling stays mouse-only.
- **Standing requirements:** invoke Execute mode `with skills: coding-standards`.
- **Out of scope:** keyboard collapse/expand path (ISSUES.md:15 — deliberately deferred),
  sub-agent context usage (ISSUES.md:7), inline skill-chip rendering (ISSUES.md:17), the
  `/skill` idle-only tag (ISSUES.md:22), Bubble Tea FPS/renderer options, any new
  `domain.Event` variant (coalescing stays entirely inside the TUI sink, so
  `TestFoldEventCoversEveryEventVariant` and the engine wire are untouched), transcript
  text-selection semantics beyond the one exemption item 2 defines.
- **Version policy:** no item touches VERSION or a CHANGELOG release heading; see the
  suggested-bump note at the end.

## 1. Resolve a motionless click from the press anchor, not the release point — ✅ DONE (2026-08-04)

NOTES (2026-08-04): the kept `line >= len(m.lineTargets)` guard gained a `line < 0` lower bound —
`pointTranscriptRow`'s `ok` used to cover the low end, and without it a negative content line would
index the slice rather than return.

**What.** In `handleMouseRelease` (`internal/tui/mouse.go:395`), the motionless-click
branch (`anchor == head`) calls `m.toggleBlockUnder(msg.X, msg.Y)` — re-resolving the
target from *screen* coordinates at release time via `pointTranscriptRow` →
`drawnLineAt`, which reads the live `m.viewport.YOffset()`. While a reply streams,
`refreshViewport` ends in `GotoBottom()` on every event, so in the 50–150 ms between
press and release the content has scrolled and the release resolves a different content
line — almost always `targetNone`, so nothing happens. The press already stored the
scroll-immune answer: `m.transcriptSel.anchor` is in content coordinates
(`contentCell{line, col}`, `mouse.go:75`), and content coordinates are append-stable by
design (`mouse.go:71-74`).

Change the motionless-click path to toggle at `m.transcriptSel.anchor.line` directly:
rework `toggleBlockUnder(x, y int)` (`mouse.go:441`, sole caller is
`handleMouseRelease`) into a form that takes the content line plus the release screen
row (still needed for `refreshViewportAnchored(line, y)`), e.g.
`toggleBlockAt(line, releaseRow int)`. Keep the `line >= len(m.lineTargets)` guard (the
anchor line can exceed the current paint if lines shrank) and the `targetKind` switch
unchanged — this fix covers every toggle target (`targetHeader`, `targetMarker`,
user-prompt see-more blocks) identically.

**Tests** (`internal/tui/mouse_test.go`, house helpers `modelWithToolBlock`,
`leftClick`/`leftRelease`, `step`, `blockExpanded`):
- New: press on a tool-block header, `step` several `eventMsg{domain.TokenEvent{…}}` so
  the viewport scrolls (`GotoBottom`), release at the *same screen position* → the
  pressed block toggles. This is the regression test for the streaming miss; it fails
  against the current code.
- New: same sequence, release resolves nothing under the *screen* point anymore —
  assert no other block toggled (only the pressed one).
- Existing must stay green: `TestTranscriptClickTogglesTheBlock`,
  `TestTranscriptToggleKeepsTheClickedHeaderRow` (idle-case anchoring behavior),
  `TestTranscriptSelectionSurvivesStreamAppend`, `TestTranscriptMidDragSurvivesRepaint`.

**Acceptance.**
- `go test -race -count=1 ./internal/tui/ -run 'Transcript|Mouse|Toggle'` passes,
  including the new streaming-click regression test.
- `go build ./...` clean.

**Commit:** `fix(tui): resolve a motionless click from the press anchor, not the release point`

## 2. A motionless press survives live-block repaints — ✅ DONE (2026-08-04)

NOTES (2026-08-04): the regression test's live block is a live SUB-AGENT RUN rather than a plain
in-flight tool call — a call still waiting for its result hides no body, so `blockHidesWhenCollapsed`
leaves its header unmarked and it is no toggle target at all; a run elides its span
(`blockState.elides`), which keeps its header clickable while it is live. The exemption itself is
kind-blind, as the item asks.

Depends on item 1 (the release path must consume the anchor for keeping it alive to
matter).

**What.** `refreshViewport` (`internal/tui/model.go:2592`) drops `transcriptSel`
whenever a spanned line changed (`spanUnchanged`, `mouse.go:95`). A *live* tool block's
header star alternates ✦/✧ with the spinner phase (`blockState.star`, `render.go:777`),
and `spinnerTickMsg` repaints every 50–100 ms while anything is open — so a press on a
running tool's header is zeroed before the release arrives, and the item-1 toggle never
fires. The comment at `model.go:668-671` anticipated the selection drop but not that
the click-toggle rides the same state.

Exempt the *collapsed* selection (`anchor == head`) from the drop rule: a collapsed
selection paints no highlight, so the rule's documented purpose — never show a highlight
over stale text (`mouse.go:86-94`) — does not apply to it; collapsed, it is purely a
click-in-progress marker. Amend `spanUnchanged` (or its call site) so an active
collapsed selection always survives a repaint, and extend the function's doc comment
with this rationale. A dragged (non-collapsed) selection keeps today's behavior
exactly. Note the out-of-bounds case: a surviving anchor may exceed the new paint's
line count after a collapse elsewhere; item 1's `len(m.lineTargets)` guard already
makes the release a no-op then.

**Tests** (`internal/tui/mouse_test.go`):
- New: model with a *live* (not-done) tool block; press its header; `step` a
  `spinnerTickMsg` (blink flip repaints the header line — this currently kills the
  selection); release → the block toggles. Fails against current code even with item 1
  applied.
- Extend `TestSpanUnchangedTable`: an active collapsed selection over changed lines →
  survives; an active dragged selection over changed lines → still drops.
- Existing must stay green: `TestTranscriptSelectionDropsWhenSpanChanges` (its selection
  is dragged, not collapsed).

**Acceptance.**
- `go test -race -count=1 ./internal/tui/ -run 'SpanUnchanged|Transcript|Toggle'`
  passes, including the live-header regression test.

**Commit:** `fix(tui): a motionless press survives live-block repaints`

## 3. Coalesce adjacent TokenEvents in the event sink — ✅ DONE (2026-08-04)

NOTES (2026-08-04): branch (1) of the owner's ruling — a SYNCHRONOUS flush at the Step boundary —
was taken, so the coalescing window never outlives the Step that opened it and the invariant stated
at `worker.go` (the per-Turn snapshot's ordering) and `messages.go` (`turnSnapshotMsg`) stays TRUE
as written; branch (2)'s deferred flush was not needed. It cost the one deviation from "all inside
`internal/tui/sink.go`": `teaSink.flush()` has to be REACHED from where a Step returns, so the
worker's drive functions take a `flush func()` (called on every path out of `eng.Step`, the cancel
path included — a Turn Esc interrupted emits nothing further, so no boundary event could flush it),
the `Model` carries it as `flushEvents`, and `Run` wires it to the Bridge's sink. Existing call
sites pass `nil` (a drive with no sink behind it). Two comments were made precise rather than
amended-for-falseness — `worker.go`'s "delivered synchronously inside the Step" and the same claim
on `turnSnapshotMsg` — because the flush, not Emit, is now what makes them true.

NOTES (2026-08-04): the `tea.Program.Send`-after-shutdown question is settled by reading the module
(charm.land/bubbletea/v2@v2.0.7): `Send` is `select { case <-p.ctx.Done(): case p.msgs <- msg: }`
(tea.go:1183), `p.ctx` is created in `NewProgram` (tea.go:614) — so it is never nil — and `Run`
cancels it unconditionally via `defer p.cancel()` (tea.go:1005), as do `shutdown`/`Kill`. A send
after the program stopped therefore returns immediately as a documented no-op: no panic, no block.
No shutdown guard was added — a window that fires after shutdown already drops its buffered text,
which is exactly what a guard would do, and the sink still has no shutdown seam to close.

NOTES (2026-08-04): one existing test outside the item's list needed adapting —
`TestBridgeBindRoutesSinkAndApprover` (`bridge_test.go`) emits one lone token and read the
program's messages inside the same call; that token now arrives when its window closes, so the test
waits for the delivery before asserting. No assertion was weakened.

**What.** `teaSink.Emit` (`internal/tui/sink.go:29`) forwards one `eventMsg` per
provider SSE delta (`internal/agent/loop.go:511` emits per visible byte-run), and every
message costs a full transcript re-render. Implement exactly the coalescing the sink's
own doc comment defers: concatenate adjacent `TokenEvent` text inside the sink within a
short window, never dropping, behind the unchanged `Emit` interface. This is TUI-side
only — no engine or `domain` change.

Design (all inside `internal/tui/sink.go`):
- `teaSink` gains a `sync.Mutex`, a pending buffer (the accumulated `Text` plus the
  `EventBase` — a plain string field, never a `strings.Builder`, per ADR 0011 hygiene in
  this package), and a flush timer; add a `window time.Duration` field so tests can
  shrink it (default a named constant ≈30 ms — about two 60 fps frames, imperceptible
  latency, caps token-driven repaints near ~33/s).
- `Emit(TokenEvent)`: under the lock — same `(Depth, Turn)` as pending → append text;
  different → flush pending, then start a new buffer; arm the timer (`time.AfterFunc`)
  if not armed.
- `Emit(any other variant)`: under the lock — flush pending first, then send the event.
  Ordering rationale (verified at baseline): `StreamResetEvent` discards pending tokens,
  `MessageEvent`/`ToolCallEvent` commit the buffer as narration, `UsageEvent` reads
  `genStart` set by the first token — so *every* non-token variant forces a flush-before.
- Timer fire: take the lock, flush. All `prog.send` calls happen under the lock, so
  relative order is preserved across the Emit goroutine and the timer goroutine.
- Downstream folds are coalescing-safe at baseline (re-verify, do not re-plumb):
  `foldStats` only latches the first token (`fold.go:47`), `transcript.appendToken`
  concatenates (`transcript.go:384`, escape-stripping is per-chunk-safe),
  `foldActivity.setActivity` is idempotent (`activity.go:159`). No per-token side
  effects exist besides `refreshViewport` itself.
- Note: `programRef.send` is a no-op before bind and `tea.Program.Send` is safe after
  the program ends — confirm the latter against the vendored Bubble Tea version and
  record a NOTES line if it needs guarding, since the sink has no shutdown seam.

**Tests** (`internal/tui/sink_test.go`, `stubProgram` from `seam_test.go`; drive the
sink directly — the mouse/model tests inject `eventMsg` past the sink and are
unaffected):
- Rework `TestTeaSinkEmitsEventsInOrder`: its adjacent `"he"`+`"llo"` tokens are
  precisely what now merges — assert the merged `"hello"` arrives *before* the
  following `StreamResetEvent`, and the overall variant order is preserved.
- New: adjacent same-`(Depth, Turn)` tokens coalesce into one msg; a token with a
  different `Depth` (sub-agent) flushes the previous buffer first and never merges
  across the boundary.
- New: any non-token event flushes pending tokens ahead of itself (order asserted).
- New: a lone token with no follow-up is delivered by the timer within the (test-shrunk)
  window; concatenation of all delivered token text equals concatenation of all emitted
  token text (nothing dropped, nothing reordered).
- `TestTeaSinkUnboundIsNoOp` stays green.

**Acceptance.**
- `go test -race -count=1 ./internal/tui/ -run 'TeaSink'` passes (the race detector
  covers the Emit-goroutine/timer-goroutine seam).
- `go test -race -count=1 ./internal/tui/` passes in full.
- `grep -n 'domain\.' internal/domain/events.go | wc -l` unchanged vs baseline — no new
  event variant (keeps `TestFoldEventCoversEveryEventVariant` and ADR 0031 untouched).

**Commit:** `perf(tui): coalesce adjacent TokenEvents in the event sink`

## 4. Reuse unchanged block paints across transcript renders — ✅ DONE (2026-08-04)

NOTES (2026-08-04): theme identity — searched, and there is NO runtime theme switch in apogee: no `/theme`
command, no key binding, no config key, `newTheme()` is called from `newModel` and from tests only, and
`Model.th` is never reassigned. One part of the theme DOES move mid-session, though: `th.measure`
(`widthAuthority`), which `Update`'s `tea.ModeReportMsg` case switches from WcWidth to GraphemeWidth on the
terminal's mode-2027 answer (`model.go:459-473`) and which re-wraps everything. So the measure is folded into
`paintKey`, and the assumption that the rest of the theme is construction-fixed is named at that field's
declaration so a future theme switch has to confront it.

NOTES (2026-08-04): the item's prune-by-entry-count is NOT sufficient for the reset path it names.
`transcript.reset()` empties the list and its caller re-fills it (fresh start-up box, replayed scrollback)
inside the SAME Update, so the next render sees an entry count that still covers the old head indices and
hands back the previous session's paint — reproduced as a failing assertion before the fix
(`TestPaintCacheDoesNotSurviveAReset`). `reset()` therefore clears the cache outright: one line, the only
mutator touched, every other mutator left untouched as the item requires.

NOTES (2026-08-04): the ⤷ sub-agent descent labels `renderView` emits ahead of a block are not cached —
several can share one head index, so they do not fit a head-keyed row, and each is one wrapped line.

NOTES (2026-08-04): the reuse property is asserted as ZERO cached-block repaints after a token append, not
as "the tail region misses". The streaming tail is painted straight from `t.pending` and never enters the
cache at all, so it costs no miss — the honest instrument for "only the tail moved" is that the miss counter
does not move while the hit counter covers every committed block.

NOTES (2026-08-04): mutation-testing the key against the matrix (drop one field, re-run) showed `flags`
catches everything `shape` and `span` catch — a head that changes painter branch or span length also changes
its flags string, so both fields are redundant today. They are KEPT (the key is deliberately generous, as its
own doc says), but `blockShape`'s doc comment claimed the shape changes "without changing anything else the
key holds", which is false; it is reworded to state the redundancy and why the field stays. Comment only.

NOTES (2026-08-04): checkpoint — done: `internal/tui/paintcache.go` (`paintKey`/`paintRow`/`paintCache`,
`transcript.blockKey`, `transcript.paintBlock`), its wiring into all three of `renderView`'s painter branches
plus the prune call, the `transcript.paints` pointer field, the `reset()` clear, the `newModel` construction,
and `internal/tui/paintcache_test.go`'s hit/miss + key-moves + reset tests. Remaining: the full equivalence
matrix (the item's 8 scripted mutation cases), the reuse-property test (~50 done entries + streaming tail,
asserted through the miss counter), and `BenchmarkRenderViewStreaming` cold vs warm.

**What.** `transcript.renderView` (`internal/tui/render.go:126`) re-paints every entry
on every call — full markdown re-parse, ANSI styling, and wrap of the whole scrollback
per message — and its only non-test caller is `refreshViewport`. Cache finished block
paints so a steady-state streaming repaint costs O(live tail), not O(scrollback).

Design — a **validation-based per-block cache** (no invalidation hooks: mutators stay
untouched; a stale key simply misses):
- The cache unit is the `blockPaint` (`render.go:76`) exactly as produced *before*
  `appendBlock` — head-index stamping, `railSpacer` separators, and `userBlocks`
  assembly stay in the outer loop, because they depend on neighbours
  (`prevBlockDepth`) and absolute position. Blocks may span multiple entries
  (folded tool runs via `toolCallRun`, sub-agent spans via `subAgentSpan`), so the
  cache is keyed by **head entry index**, storing the span length, the `blockPaint`,
  and the key it was built under.
- The key holds everything a paint depends on besides immutable entry content: `width`,
  per-entry `expanded` and `done` across the span, span length, derived liveness, and
  `blink` *only when the block is live* (`renderEntryLines` is documented as a pure
  function of `(th, entry, width, blink)`, and only a live block's star varies with
  blink). Committed entry text/tool innards are immutable except `enrichWithResult`
  (which flips `done` — captured by the key) and `refreshStartup` (which rewrites
  `entries[0].startup` with **no** flag change) — so **never cache `entryStartup`
  blocks**. The synthetic pending-tail entry (`t.pending`, painted at
  `render.go:213-217`) is never cached. If theme identity can change at runtime,
  include it in the key or clear the cache on change — verify whether any live theme
  switch exists and note the finding.
- Storage: a pointer field on `transcript` (e.g. `paints *paintCache` in a new
  `internal/tui/paintcache.go`) so every by-value `Model` copy shares one cache — the
  same write-through pattern as the entries backing array; say so in the doc comment
  (ADR 0011). Keep `hits`/`misses` counters on the cache, unexported, for tests.
- `renderView`'s loop per block: compute the cheap key (flag reads over the span),
  compare against the cached row, reuse on match, re-paint and store on miss. Prune
  rows whose head index falls outside the entry count (truncation/reset paths, e.g.
  session switch).

**Tests** (new `internal/tui/paintcache_test.go` + additions to `render_test.go`;
house style is plain unit tests, `render.go`'s pure-renderer tests as the model):
- Equivalence property, the core guard: over a scripted mutation sequence — token
  append, new tool call extending a folded run, tool *result arriving for an older
  entry*, expand/collapse toggle, width change, blink flip with a live block, sub-agent
  span, `refreshStartup` — after every step, `renderView` through the warm cache
  produces byte-identical `lines` and `targets` to a fresh render with a cold cache.
- Reuse property: a transcript of ~50 done entries plus a streaming tail; appending one
  token and re-rendering re-paints only the tail region (assert via the miss counter),
  and a second identical render is all hits.
- Benchmark `BenchmarkRenderViewStreaming` (precedent: `BenchmarkRenderTable`,
  `mdtable_test.go:846`): a large transcript with a streaming tail, cold vs warm — the
  evidence line for the perf claim.

**Acceptance.**
- `go test -race -count=1 ./internal/tui/` passes in full (equivalence + reuse tests
  included).
- `go test ./internal/tui/ -run '^$' -bench BenchmarkRenderViewStreaming -benchtime 10x`
  runs, and the warm-cache case shows a large reduction (report the numbers in the
  verifier's evidence line).
- `TestModelNoBuilderByValue` stays green (no no-copy type reached the value model).

**Commit:** `perf(tui): reuse unchanged block paints across transcript renders`

## 5. Close out the issue and record the change — ✅ DONE (2026-08-04)

Depends on items 1–4.

**What.**
- `ISSUES.md`: flip the click-responsiveness item (currently line 9) to `[X]`.
- `CHANGELOG.md` `[Unreleased]`: add entries in the house narrative style (bold
  user-facing lead + story prose, as in the existing prompt-collapse entry) — under
  *Fixed*: a click on a tool header now lands while a reply streams (press-anchor
  resolution + live-repaint survival); under *Changed* or *Performance*: token
  coalescing in the sink and block-paint reuse end the 100%-GPU full-transcript
  re-render per token.
- `layout.md` § "Collapsed and expanded blocks": verify the prose still matches (it
  should — toggling stays mouse-only and click semantics are unchanged, only made
  reliable); amend only if it describes release-point hit-testing.
- `internal/tui/sink.go`: the lines 9-19 doc comment defers coalescing as a future
  option — rewrite it to describe the now-present behavior.

**Tests.** None new; this is a docs item.

**Acceptance.**
- `make check` passes (full gate: gofmt, vet, build, race tests, ADR-0010 grep, cross,
  --help).
- `grep -n 'Clicking on the header' ISSUES.md` shows the item marked `[X]`.
- `grep -n 'coalesce' internal/tui/sink.go` shows present-tense doc, not the deferred
  note.

**Commit:** `docs: close out the click-toggle responsiveness issue`

---

**Suggested version bump** (not performed — owner's call): patch, `v0.10.14` →
`v0.10.15`. Two user-visible bug fixes plus internal performance work; no new surface,
no API change (the sink's `Emit` interface and the event wire are unchanged).
