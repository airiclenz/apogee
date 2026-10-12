---
title: Tests use fixed sleeps and deadlines with no shared, scalable budget
status: open
type: bug
priority: 1
plan: 2026-10-12 - 02 - test-suite-stability-plan
created: 2026-10-12T18:10:59Z
---

361 timing sites in 124 test files; ~25 point fixes for wall-clock budget flakes; no guard; APOGEE_TEST_SLOW scales no budget. Review §4, class A.

Findings: `docs/reviews/2026-10-12 - test-suite-stability-speed-memory.md`.
