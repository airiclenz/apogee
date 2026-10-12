---
title: Every cmd/apogee chunk builds its own e2e apogee binary
status: open
type: bug
priority: 3
plan: 2026-10-12 - 02 - test-suite-stability-plan
created: 2026-10-12T18:10:59Z
---

TestMain runs go build in each of ~13 chunk processes per suite run. Review §5.

Findings: `docs/reviews/2026-10-12 - test-suite-stability-speed-memory.md`.
