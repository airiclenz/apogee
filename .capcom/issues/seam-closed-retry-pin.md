---
title: Pin SeamClosedEvent firing once per attempt across the post-response retry hand-back
status: done
type: task
priority: 3
aliases: [apogee-boe]
created: 2026-09-09T03:23:30Z
closed: 2026-09-11T10:28:50Z
reason: delivered by docs/plans/archived/2026-09-09 - 00 - deferred-findings-plan.md (17c105c1 090b0f63 480095e0 4cb5922e f3f9b321 94978659 5fe4fcc4 300336ff edb2bbe4 9ba9619e)
---

Reaction core stage 2 (plan 2026-09-08 - 00): no test drives the post-response retry hand-back path — `internal/agent/reactions.go:153` (`if retried && seam.retryable`). The deferred emit makes double-emission structurally impossible, but the once-per-attempt claim is unpinned: a Turn that faults transiently and re-streams should close the seam exactly once per attempt.
