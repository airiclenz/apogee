---
title: Trailers appended after the fresh-result clamp push a committed result past its bound
status: done
type: bug
priority: 2
plan: 2026-11-02 - 05 - request-room-and-delegate-bounds-plan.md
created: 2026-11-02T20:02:19Z
closed: 2026-11-03T06:38:43Z
reason: "Resolved by plan '2026-11-02 - 05 - request-room-and-delegate-bounds-plan' item 8 (archived at d14d774d): trailers are weighed and subtracted inside the fresh-result bound; TestClampedResultWithAdviceFitsTheRoom, TestClampedDelegateResultWithStepNoticeFitsTheRoom and TestUnknownWindowClampCountsTrailers prove it."
---

Found by docs/reviews/architecture-review-2026-11-02.html, verified 2026-11-02 at 00d5bf9f. advise, step and token notes are appended after clampToBound and are not counted by freshResultBound, so a clamped result lands the next request at room + trailer and forces a predictive fold before the model reads it. Trigger: a delegate at the step notice reads a big file into a crowded window, or any advise: span on a clamped result.

## Acceptance

- A committed tool result plus its trailers stays within the fresh-result bound; a test with advice and a step notice on a clamped result proves it.
