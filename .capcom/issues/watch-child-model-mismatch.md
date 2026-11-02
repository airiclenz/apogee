---
title: A watch measures the watched run's sub-agents against the watcher's model and window
status: open
type: bug
priority: 2
plan: 2026-11-02 - 06 - watch-run-projection-plan.md
created: 2026-11-02T20:02:19Z
---

Found by docs/reviews/architecture-review-2026-11-02.html, verified 2026-11-02 at 00d5bf9f. internal/tui/fold.go calls applyUsage(e, m.opts.ContextWindow, m.opts.Model); m.opts is not swapped in a watch, so unrouted delegations show '· <watched model>' and a routed child on the watcher's model shows none. Not a regression: wrong since watch shipped (2026-10-17).

## Acceptance

- In a watch, every sub-agent row's model cell and window come from the watched run; a driven test watching a session on a different model proves it.
