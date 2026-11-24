# TUI performance audit — results after plans 07 and 08

**Audit:** `docs/reviews/tui-perf-audit-2026-11-23.md` (the "before" numbers below are its Profile table).
**Fixes:** plan `2026-11-23 - 07 - tui-perf-frame-and-local-fixes-plan.md` (archived) and plan `2026-11-23 - 08 - tui-perf-repaint-plan.md`, both under the capcom issue `tui-perf-responsiveness`.
**Measured:** 2026-11-24 at commit `33035dea4` (plan 08 item 8 landed), on the same kind of host as the audit: Raspberry Pi 5 (4× Cortex-A76, 8 GB), Go 1.26.11 linux/arm64, `GOMAXPROCS=4`, one benchmark process at a time, no race detector, `-benchmem`, `-count 1`.

## How the numbers were taken

The audit's benchmarks are kept code since plan 07 item 1: `internal/tui/perfaudit_bench_test.go` (`BenchmarkPerfAudit*`, the same workloads the audit profiled: a resumed session of N tool blocks in a 120×40 window, driven through the real `Model.Update` and `Model.View`). The other rows come from the benchmarks that already existed next to the code they measure. One test binary was built (`go test -c ./internal/tui/`) and each benchmark run from it on its own, for example:

```
go test -run '^$' -bench 'PerfAuditBlocks' -benchmem ./internal/tui/
```

Each number is one run: expect a few percent of noise between runs, so read small differences (under ~5%) as "unchanged".

## Profile, before and after

"before" is the audit (2026-11-23, before any fix); "after" is this run. All rows are per operation.

| scenario | before: cost | after: cost | before: allocated | after: allocated | fixed by |
|---|---|---|---|---|---|
| stream, empty session / 300 blocks, per delta | 3.84 / **16.31 ms** | 2.47 / **2.66 ms** | 1.51 / 2.36 (see note 1) | 1.44 / 2.24 GB per 4,096 deltas | 08 items 3, 6 |
| all-hit repaint, N=48 / 300 / 900 | 4.38 / 14.82 / **40.57 ms** | 1.97 / 4.07 / **7.61 ms** | 0.88 / 1.63 / 3.51 MB | 0.30 / 1.09 / 3.05 MB | 08 items 3, 5 |
| cold repaint (resize), N=48 / 300 / 900 | 13.9 / 69.8 / **200.3 ms** | 8.33 / 38.4 / **110.6 ms** | 3.0 / 7.4 / 17.7 MB | 1.30 / 5.35 / 15.07 MB | 08 items 3, 7 |
| keystroke, N=0 / 48 / 300 | 3.36 / 5.99 / **16.21 ms** | 1.60 / 1.94 / **1.85 ms** | 2.22 / 2.40 / 3.12 MB | 0.36 / 0.41 / 0.40 MB | 07 item 2; 08 item 2 |
| fold toggle, N=48 / 300 | 6.79 / **17.03 ms** | 2.87 / **5.15 ms** | 2.68 / 3.42 MB | 0.55 / 1.35 MB | 08 items 3, 5 |
| View alone, N=48 / 300 / 900 | 2.11 / 2.20 / 2.67 ms | 1.41 / 1.45 / 1.40 ms | ~720 KB | ~148 KB | 07 items 2, 3; 08 item 4 |
| PgUp/PgDn, wheel (N=48 and 300) | 3.0–3.4 ms | 1.77–1.93 ms | 1.7–2.1 MB | 0.24–0.39 MB | 07 item 2; 08 item 4 (frame floor) |
| spinner tick, settled / live call (N=300) | 2.72 / 5.89 ms | 1.66 / 2.32 ms | 1.36 / 1.55 MB | 0.24 / 0.43 MB | frame floor + cheaper repaint; blink still repaints (see "What stays open") |
| /thinking open, one chunk at cap | 15.87 ms | 9.42 ms | 9.86 MB | 6.38 MB | 07 item 4 |
| key at end of 400-line draft | 23.25 ms | 21.50 ms | 4.15 MB | 2.94 MB | not targeted (see "What stays open") |

Note 1: the audit labels the stream allocations "MB" per delta, but the benchmark reports bytes per op of 4,096 deltas, and the session=0 figure taken just before plan 08 item 6 was 1.50 GB per op, so the audit's column most likely means GB per op. The session=300 allocation fell from 5.21 GB per op just before item 6 to 2.24 GB now.

The shape changed as well as the size: a keystroke is now flat in session length (1.85 ms at N=300 against 1.94 ms at N=48), and so are View alone, scrolling and the stream (2.66 ms per delta at 300 blocks against 2.47 ms in an empty session). An all-hit repaint and a cold repaint are still linear in N, at about 7 µs and 120 µs per block (the audit measured ~41 µs and ~220 µs).

### Benchmarks next to the code

| benchmark | before | after | fixed by |
|---|---|---|---|
| `ThinkingPaneUpdateAndView` | 15.15–15.79 ms, 8.41 MB, 130,750 allocs | 9.42 ms, 6.38 MB, 46,562 allocs | 07 item 4 |
| `SidePanelView` panel=auto | 4.24 ms, 244 KB, 5,437 allocs | 1.47 ms, 129 KB, 3,486 allocs | 07 item 6 (and the frame floor) |
| `SidePanelView` panel=off | 1.68 ms, 141 KB, 3,911 allocs | 1.35 ms, 126 KB, 3,800 allocs | frame floor |
| `RenderViewDelegationsAllHit` 4×30 calls | 178 µs, 324 KB, 32 allocs | 8.2 µs, 2.7 KB, 25 allocs | 07 item 7 |
| `RenderViewDelegationsAllHit` 4×300 calls | 2.21 ms, 3.72 MB, 36 allocs | 35.4 µs, 5.0 KB, 25 allocs | 07 item 7 |
| `ReserveWidgetCellsAllHit` | ~246 µs, 111 KB, 294 allocs | 121 µs, 69 KB, 177 allocs | 08 item 5 |
| `ReserveWidgetCellsAllHitNoOverrun` | ~117 µs, 59 KB, 176 allocs | 143 µs, 59 KB, 176 allocs | unchanged by design (no line overruns); the time is run-to-run noise, allocations are identical |

"before" in this table is the number in the fixing commit's body, measured just before that commit on the same Pi; the audit did not run these benchmarks except `ThinkingPaneUpdateAndView`.

## Dominant costs and their fix status

Every fix keeps the drawn output byte-identical (golden frames, or equality against a nil paint cache), except the separator drag, which by the ratified design now draws only the latest width of each burst of queued motions (`adr-0110-amend-1123`).

| audit finding | issue | status |
|---|---|---|
| The `@file` autocomplete walks the workspace inside Update | `at-file-walk-blocks` | fixed, plan 07 item 5 (`6cfc9fe39`): the walk runs on a Cmd goroutine |
| Every keystroke repaints the whole transcript | `keystroke-repaints-transcript` | fixed, plan 08 item 2 (`7bf66fefb`): a draft edit that leaves the frame key unchanged does not repaint |
| The viewport re-measures every transcript line on every repaint | `viewport-remeasures-lines` | fixed, plan 08 item 3 (`6adcc3713`): the widget gets blank placeholder lines; View composes the visible body from `m.lines` |
| Every streamed text batch repaints the whole transcript | `delta-repaints-transcript` | fixed, plan 08 item 6 (`3884de13d`): a pure append re-renders only the preview tail |
| /thinking rebuilds and re-wraps the whole board every frame | `thinking-pane-rebuilds-rows` | fixed, plan 07 item 4 (`5fe1bbf99`) |
| Dragging the side-panel separator cold-repaints per column | `panel-drag-cold-repaints` | fixed, plan 08 item 8 (`33035dea4`): one repaint per burst of queued motions, at the latest width |
| The footer walks every transcript entry per frame for sub-agent spend | `footer-spend-walks-transcript` | fixed, plan 07 item 3 (`f493b4f41`) |
| "Is the prompt pane open?" heap-copies the Model per frame | `open-panes-heap-copies` | fixed, plan 07 item 2 (`ca9035616`): over half of every frame's allocations gone |
| Each visible row is width-measured five to six times per frame; the scroll bar renders its glyphs once per row | `view-recompose-floor` | fixed, plan 08 item 4 (`f79dff178`) |
| One overrunning line puts every repaint on the slow reserve path | `reserve-overrun-recut` | fixed, plan 08 item 5 (`6335abe58`): overrunning lines are split once, when a block's paint is stored |
| Paint helpers pay for width passes they do not need (cold repaint at 200 ms for 900 blocks) | `resize-cold-repaint-cost` | fixed, plan 08 item 7 (`c6379e852`): bare-colour span painters, known row widths; 110.6 ms at N=900 |
| The side panel re-derives and repaints every section every frame | `panel-sections-every-frame` | fixed for the changed-files section, plan 07 item 6 (`f7a5a2e14`); the other sections still repaint (see below) |
| Collapsed sub-agent runs build cache keys over every hidden entry | `delegation-key-copies-span` | fixed, plan 07 item 7 (`2d8ce43ff`) |

## What stays open

None of these has an issue; the audit raised them, and plans 07 and 08 put them out of scope.

- **The blinking live star repaints the whole transcript twice a second.** A spinner tick with a live call costs 2.32 ms against 1.66 ms settled at N=300. The repaint is now cheap (no width scan), but it still walks every block; a splice of only the live blocks would remove it.
- **The streaming preview is re-rendered as markdown from scratch on every batch.** Item 6 cut the batch to the preview tail, but within that tail `renderMarkdownBody` runs cold each time.
- **Each keystroke arms its own debounce tick**, doubling the frame count while typing.
- **A long draft is re-read and re-wrapped many times per keystroke:** 21.5 ms per key at the end of a 400-line draft, barely below the audit's 23.25 ms.
- **Moving the block cursor or jumping from the side panel repaints an unchanged transcript** (park without repainting).
- **The side panel's tasks, sub-agents, usage and MCP sections** still repaint every frame; only changed-files is memoised.
- **Smaller repaint leaves from the audit's item 11:** a tool group's shape still worked out several times per repaint, capacity hints on the line slices, and the detail tone (`detailTone`, `paintRowMarker`), which still goes through lipgloss (about 4% of the resize profile).
- **Repaints stay linear in session length.** An all-hit repaint is ~7.6 ms and a cold repaint ~111 ms at 900 blocks. Painting only the visible part first (lazy, visible-first painting) is the structural remedy, kept out of plan 08 by design.

## The parent issue's acceptance

- A before/after profile exists (this report and the audit) and lists the dominant costs with their fix status.
- The behaviour fixes are pinned by tests in `internal/tui`, and the driven end-to-end ones, in `cmd/apogee`, go through `internal/tuitest` + `internal/stubllm` (`TestE2EStreamIntoALongSessionMatchesAColdRender`, `TestE2ETypingWrapsAndRefitsTheTranscript`, `TestE2EAtFileMenuListsWorkspaceFiles`, `TestE2ESidePanelChangedFilesFollowWrites`). The rest step the Model directly (for example `TestTypingDoesNotRepaintTheTranscript`, `TestDragBurstCostsOneColdRepaint`). Byte identity is pinned by golden frames (`TestViewFrameGolden`) and equality tests against a nil paint cache.
- No fixed deadline was added to shipped code: the drag coalescing waits on a settle Msg, not a clock.
- `layout.md` and `docs/layout/` need no change: every drawn frame is one the screen drew before.
- The full suite (`APOGEE_TEST_SLOW=1 GOMEMLIMIT=2GiB make test`) runs at the plan's closeout.
