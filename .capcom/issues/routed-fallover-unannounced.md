---
title: A routed fallover is never announced on a Driver whose resolver sets no Announce
status: open
type: bug
priority: 2
plan: 2026-11-02 - 04 - seat-and-resolved-entry-plan.md
created: 2026-11-02T20:02:19Z
---

Found by docs/reviews/architecture-review-2026-11-02.html, verified 2026-11-02 at 00d5bf9f. announcesRehome returns false when FallbackAnswer.Announce is nil, so any Driver other than the TUI (bench, embedder) gets no upstream_switched Event for a routed fallover.

## Acceptance

- A routed fallover emits exactly one upstream_switched Event per seat move on every Driver, with no Driver-side announce hook; an engine test drives it.
