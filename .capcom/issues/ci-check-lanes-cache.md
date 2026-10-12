---
title: CI check job oversubscribes lanes and rarely caches its race build
status: open
type: bug
priority: 3
plan: 2026-10-12 - 02 - test-suite-stability-plan
created: 2026-10-12T18:11:06Z
---

APOGEE_TEST_LANES=5 on 4 vCPU; the setup-go cache key is shared with lint/vulncheck/cross, so the check job's race objects are rarely saved. Review §5, §7.

Findings: `docs/reviews/2026-10-12 - test-suite-stability-speed-memory.md`.
