---
title: Windows CI runs the ConPTY packages concurrently in one raw go test
status: open
type: bug
priority: 2
plan: 2026-10-12 - 02 - test-suite-stability-plan
created: 2026-10-12T18:11:06Z
---

internal/platform and internal/console run their 50-iteration cmd.exe/conhost loops side by side at -parallel 4 under a 10m alarm, widening every final-frame window. Review §3.

Findings: `docs/reviews/2026-10-12 - test-suite-stability-speed-memory.md`.
