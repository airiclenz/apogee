---
title: Three internal/tui sweep tests set the Pi suite's critical path
status: done
type: bug
priority: 3
plan: 2026-10-12 - 02 - test-suite-stability-plan
created: 2026-10-12T18:10:59Z
closed: 2026-10-13T05:10:15Z
reason: Resolved by plan '2026-10-12 - 02 - test-suite-stability-plan' (item 21), archived at c6b5e859.
---

TestOverlayHeightQueryMatchesItsRender (141 s), TestInputBoxWindowFollowsTheCaret (136 s), TestFrameNeverExceedsTheTerminalHeight (76 s) cannot be split below the top level by the shard packer. Review §5.

Findings: `docs/reviews/2026-10-12 - test-suite-stability-speed-memory.md`.
