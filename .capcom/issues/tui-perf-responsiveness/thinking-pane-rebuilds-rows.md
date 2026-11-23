---
title: The /thinking pane rebuilds and re-wraps its whole board on every chunk
status: done
type: task
priority: 2
labels: [perf, tui]
plan: 2026-11-23 - 07 - tui-perf-frame-and-local-fixes-plan.md
created: 2026-11-23T19:29:39Z
closed: 2026-11-23T21:41:47Z
reason: item 4 resolved by plan '2026-11-23 - 07 - tui-perf-frame-and-local-fixes-plan' (archived at 2fb94a24b)
---

## Problem

Scenario: streaming, with the `/thinking` pane open while a reasoning model thinks (the pane
opens following the live record, `internal/tui/thinkingpane.go:293-302`).

- Every build of the pane's content runs `thinkingRows` (`thinkingpane.go:154-173`), which
  copies the wrapped rows of every retained record (up to 64 records, each capped at 64 KB:
  `thinkingRecordCap`, `internal/tui/thinking.go:56`) into fresh `rows`/`kinds` slices. The
  copy is of string headers, so the cost is O(all retained rows), not 64 KB of bytes per record.
- Each reasoning chunk grows the live record's text (`thinking.go:118`), so the per-record wrap
  memo misses (`thinkingpane.go:253`, `entry.text != rec.text`) and the whole live record is
  re-wrapped through `wrapReadable`.
- The content is built at least twice per Msg: once for the height query on `Update`'s tail
  (`settleFrame` → `freshenTranscriptClamp`/`layout`, `model.go:3107-3108` → `overlayRows`,
  `model.go:3504-3508` → `reportHeight`, `internal/tui/reportpane.go:316-320`) and once for the
  paint (`View` → `frameOverlays`, `model.go:3527-3531` → `renderReport`,
  `reportpane.go:301-305`); both go `reportContent` → `thinkingContent` → `thinkingRows`.
  The second build re-uses the wrap memo, so it is one re-wrap per chunk but two full copies.
- Each build also has `thinkingHeading` → `runLabel` → `runHead` walk the transcript entries
  once per delegate record (`thinkingpane.go:126-132`, `internal/tui/runview.go:165`).

All of this for a pane that shows only `maxInspectorRows` = 16 rows (`inspector.go:141`).

Measured (Raspberry Pi 5): the existing `BenchmarkThinkingPaneUpdateAndView`
(`internal/tui/paintcache_test.go:830`) costs 15.87 ms, 9.86 MB and 130,840 allocs per chunk
at cap; `BenchmarkThinkingPaneRender` 10.56 ms. In that profile `thinkingRows` is 3.62 s cum
(1.30 s under `reportHeight`, 2.34 s under `renderReport`) and 1.61 GB flat alloc_space;
`wrapReadable` adds 1.17 GB cum.

## Why

With the sink flushing up to ~33 chunks a second, this is about half of one Cortex-A76 core
while a model thinks with the pane open — CPU taken from the local model on the same box, and
GC pressure on every frame. Visible tokens streaming under the open pane pay the copy too.

## Suggested fix

Suggestion from the audit, output unchanged:

1. `wrapThinkingText` already wraps each `\n`-separated line on its own, so memoise per line:
   keep the rows of the record's completed lines (text up to its last `\n`) and re-wrap only the
   trailing partial line; when `keepLastBytes` trims the front, re-wrap only the first line.
2. Memoise the composed `rows`/`kinds` in `thinkingRowCache`, checked against the same record
   texts plus column and scope, so the height query and the paint in one frame share one build.
   Recompose only the ≤64 heading rows, or cache the run label per run.

## Acceptance

- [ ] One reasoning chunk at cap re-wraps only the changed line — pinned with the existing
      `wraps` counter.
- [ ] The pane's rows are composed once per frame, not once per ask.
- [ ] `BenchmarkThinkingPaneUpdateAndView` is measurably below 15.87 ms / 9.86 MB per chunk;
      before/after recorded.
- [ ] A test driving the TUI through `internal/tuitest` + `internal/stubllm` (never an
      `httptest` upstream closure) streams reasoning with `/thinking` open and shows the pane
      rows equal a cold build; rendered output is byte-identical.
- [ ] No tight deadline or fixed clock added (slow-box rule).
- [ ] `APOGEE_TEST_SLOW=1 GOMEMLIMIT=2GiB make test` passes.

## Source

TUI performance audit 2026-11-23, verified claim 3 (High). Report:
`docs/reviews/tui-perf-audit-2026-11-23.md`.
