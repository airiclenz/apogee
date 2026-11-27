---
title: TUI performance leftovers from the 2026-11-23 audit
status: open
type: task
priority: 3
created: 2026-11-27T18:35:35Z
---

`docs/reviews/tui-perf-audit-2026-11-23-results.md` §What stays open lists eight costs that plans
07 and 08 left out of scope and that had no issue. Each child is one of them. Every fix must
keep the drawn output byte-identical (golden frames, or equality against a nil paint cache) and
add no fixed deadline to shipped code. Close this issue when its children are closed.
