---
title: "tuitest: GoldenText's own path split is unpinned"
status: done
type: task
priority: 3
aliases: [apogee-70d]
created: 2026-09-07T14:29:06Z
closed: 2026-09-07T17:22:49Z
reason: Closed by test-only additions; make check green
---

Deferred from plan 'docs/plans/2026-09-07 - 00 - headless-event-lines-plan.md'. TestGoldenTextRoundTrips drives compareGolden directly, so nothing in internal/tuitest pins GoldenText's own path split (dir/ext); only the cmd/apogee e2e tests exercise it.
