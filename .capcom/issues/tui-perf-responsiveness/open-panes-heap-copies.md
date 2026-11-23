---
title: Asking which panes are open heap-copies the Model on every frame
status: open
type: task
priority: 3
labels: [perf, tui]
plan: 2026-11-23 - 07 - tui-perf-frame-and-local-fixes-plan.md
created: 2026-11-23T19:29:39Z
---

## Problem

Scenario: every frame — idle, typing, scrolling, streaming — with no pane open.
`openPanes` (`internal/tui/model.go:5504`) evaluates every pane-table row's `open` predicate.
The five report rows' predicate is the closure `func(m Model) bool { return
m.reportState(r).open }` (`internal/tui/panes.go:345`), and `reportState` has a pointer receiver
(`internal/tui/reportpane.go:184`), so `m` escapes and every call heap-copies the whole Model.
`View` and `Update`'s tail reach `openPanes` several times per frame, e.g. `draftRowsCeiling`
(`model.go:3339`/`:3342`, asking only `has(panePrompt)`) via `inputScrollbarShown`
(`model.go:3294`, `:4064`), `hiddenDraftRows` → `inputInnerWidth` (`model.go:3357`, `:4068`) and
`promptCursor` → `inputContentRect` (`internal/tui/mouse.go:236`).

Measured (Raspberry Pi 5, no pane open): `reportPaneRow.func1` is 55.3% of `View`'s 723 KB/op
(300 tool blocks), 42% of all streaming bytes (6.23 GB of 14.8 GB), and 50–56% of the bytes in
the typing, scroll and idle profiles. `reportHeight`, `renderReport` and `reportKey` add another
7–19% each in those profiles, also with no report open. CPU: `inputScrollbarShown` 6.2% and
`draftRowsCeiling` 5.9% of `View`.

## Why

Roughly 400 KB of garbage per frame, at 10–20 frames a second while a Turn runs: GC work on a
Pi 5–class host that competes with the local model, for a question whose answer is "no pane".

## Suggested fix

Suggestion from the audit, the same booleans so output is identical: (a) make the report rows'
`open` (and `height`/`render`/`key`) predicates read their state without the Model escaping, in
line with ADR 0011's by-value Model rules; (b) callers that ask about one pane use that pane's
own predicate (`m.promptOpen()`, `panes.go:131`/`:371`) instead of building the whole set, and
`inputScrollbarShown` returns false before computing the ceiling when the draft's rows are at
most `minInputRows` (`model.go:3347`); (c) check that `reportHeight`, `renderReport` and
`reportKey` return early and allocate nothing when their report is closed.

## Acceptance

- [ ] A `View` of an unchanged model with no pane open makes no heap copy of the Model — pinned
      by an allocs/op or B/op assertion; the `View` benchmark's B/op roughly halves; before/after
      recorded.
- [ ] Pane open/close behaviour is unchanged, shown by tests driving the TUI through
      `internal/tuitest` + `internal/stubllm` (never an `httptest` upstream closure); frames are
      byte-identical; `TestModelFitsTheStack` still passes.
- [ ] No tight deadline or fixed clock added (slow-box rule).
- [ ] `APOGEE_TEST_SLOW=1 GOMEMLIMIT=2GiB make test` passes.

## Source

TUI performance audit 2026-11-23, tool-confirmed profile finding ("`openPanes()` allocates about
55% of every View's bytes with no pane open"). Report:
`docs/reviews/tui-perf-audit-2026-11-23.md`.
