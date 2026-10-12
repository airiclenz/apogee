---
title: "The reworded run:/run: url: refusals are asserted only on their unchanged tails"
status: done
type: task
priority: 3
aliases: [apogee-yk9]
created: 2026-09-09T03:23:32Z
closed: 2026-09-11T10:28:49Z
reason: delivered by docs/plans/archived/2026-09-09 - 00 - deferred-findings-plan.md (17c105c1 090b0f63 480095e0 4cb5922e f3f9b321 94978659 5fe4fcc4 300336ff edb2bbe4 9ba9619e)
---

Reaction core stage 2 (plan 2026-09-08 - 00): `TestHookValidateRefusesEachRule` (`internal/reactions/hooks_test.go:182,:187,:189,:191`) asserts on 'must not be blank' and 'absolute http:// or https:// URL' alone, so the three refusals reworded onto `run:` and `run: url:` have no test naming the new key — only 2 of the 5 reworded refusals are pinned.
