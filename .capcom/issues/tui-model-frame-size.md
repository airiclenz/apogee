---
title: tui.Model is ~121 KB by value; Update/handleKey frames reach 1.1-1.2 MB
status: open
type: task
priority: 3
plan: 2026-10-13 - 01 - test-readiness-and-model-size-plan
aliases: [apogee-tui-model-frame-size]
created: 2026-10-11T15:01:21Z
---

Found while fixing apogee-test-gate-oom. The Model is copied by value on every Update (ADR 0011). The theme (32 KB), session browser (27 KB) and four ~13.7 KB panes dominate it. Making the theme a pointer would shrink every frame by ~27%. Big frames inflate goroutine stacks in production and tests (the parallel-subtest OOM was this).
