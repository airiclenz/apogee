---
title: A Firing keeps the primary's parallel-agents width after a session-seat fallover
status: done
type: bug
priority: 2
plan: 2026-11-02 - 04 - seat-and-resolved-entry-plan.md
created: 2026-11-02T20:02:19Z
closed: 2026-11-02T22:16:35Z
reason: "Resolved by 2026-11-02 - 04 - seat-and-resolved-entry-plan (archived at 66e4d894), item 9: FallbackAnswer.ParallelAgents filled at the Firing door and applied to the root on a landed session-seat rehome; TestHeadlessFallbackFansOutAtTheFallbackWidth drives it."
---

Found by docs/reviews/architecture-review-2026-11-02.html, verified 2026-11-02 at 00d5bf9f. FallbackAnswer carries no width; the TUI re-follows via arrive→caps.follow but a Firing has no equivalent, so after a session-seat fallover a Firing still fans out at the dead primary's parallel-agents width.

## Acceptance

- A Firing raised after a session-seat fallover binds the fallback entry's parallel-agents width; a test drives it.
