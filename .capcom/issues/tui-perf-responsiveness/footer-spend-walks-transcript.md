---
title: The footer's sub-agent spend walks the whole transcript every frame
status: open
type: task
priority: 2
labels: [perf, tui]
created: 2026-11-23T19:29:39Z
---

## Problem

Scenario: idle, typing, scrolling — any frame. On every `View` (each spinner tick at 10–20 fps
while a Turn runs, each keystroke, each wheel notch) the footer's spend segment re-sums the
sub-agent (delegate) spend by walking every transcript entry:

`Model.View` (`internal/tui/model.go:3823`) → `footerView` → `footerContent`
(`model.go:4189`, once per footer row) → `footerRowContent` → `footerRow` (`model.go:4419`) →
`footerLeftText` (`model.go:4493`) → `delegateUsageTotal` (`internal/tui/usage.go:302`) →
`delegateUsageHeads` (`usage.go:278-290`).

That loop copies each entry by value (`head := m.transcript.entries[i]`, `usage.go:281`; an
`entry` embeds a whole `toolView`) and appends matches into two fresh slices, only to add up their
usage. The same walk runs again for the footer's pointer hit-tests (`footerModeSpan` /
`footerGitSpan`, `model.go:4550` / `:4562`). Nothing memoises it: the `m.delegateUsage` field is
only the fallback for a resumed record. The sum changes only when a usage reading lands.

Measured (Raspberry Pi 5, `View` alone on an unchanged model, resumed session of 300 tool
blocks ≈ 3,700 transcript lines): `delegateUsageHeads` is 4.5% flat / 5.5% cum of `View`, 90 ms
of its 160 ms samples on the by-value copy at `usage.go:281`; `entry.carriesWorkflowSpend` adds
1.0% flat. It is the one term of `View` that grows with the session: `View` alone costs
2.11 → 2.20 → 2.67 ms at 48 / 300 / 900 tool blocks while every other part of `View` is bounded
by the screen size. About 0.12 ms of a 2.2 ms frame at 300 blocks, linear in session length.

## Why

Sessions run for hours and reach hundreds of tool blocks. On a Pi 5–class host the TUI shares
the CPU with the local model, and this cost is paid on every frame — including the spinner's
10–20 frames a second for the whole of every Turn — to recompute a number that has not changed.

## Suggested fix

Suggestion from the audit, not a settled design: keep the delegate sum as a folded value,
updated where `applyUsage` and the workflow-spend writes land, or memo it behind a pointer keyed
on `transcript.generation` plus the workflows' spend state (the ADR 0011 pointer-memo pattern),
and have `footerLeftText` read it. Where a walk remains, iterate by index (`&entries[i]`) and sum
in place without copying entries or building slices. It is the same sum, so the footer's bytes
do not change (no visual change; `layout.md`, `docs/layout/`).

## Acceptance

- [ ] A `View` of an unchanged model does no walk over the transcript entries for the footer —
      pinned by an allocs/op (or call-counter) assertion on `footerContent`, and a `View`
      benchmark in `internal/tui` at 900 tool blocks whose cost no longer grows with N.
- [ ] The footer's spend still moves when a sub-agent's usage lands, shown by a test driving the
      TUI through `internal/tuitest` + `internal/stubllm` (never an `httptest` upstream closure;
      `capcom know show adr-0062-amend-1118`).
- [ ] Rendered output is byte-identical to today's.
- [ ] No tight deadline or fixed clock added; test waits go through `internal/testtime`
      (slow-box rule).
- [ ] `APOGEE_TEST_SLOW=1 GOMEMLIMIT=2GiB make test` passes.

## Source

TUI performance audit 2026-11-23, verified claim 1 (High). Report:
`docs/reviews/tui-perf-audit-2026-11-23.md`.
