---
title: "headless_test.go fixtures still written in the retired hooks:/events:/command: schema"
status: done
type: task
priority: 3
aliases: [apogee-d8q]
created: 2026-09-09T03:23:31Z
closed: 2026-09-11T10:28:50Z
reason: delivered by docs/plans/archived/2026-09-09 - 00 - deferred-findings-plan.md (17c105c1 090b0f63 480095e0 4cb5922e f3f9b321 94978659 5fe4fcc4 300336ff edb2bbe4 9ba9619e)
---

Reaction core stage 2 (plan 2026-09-08 - 00): `cmd/apogee/headless_test.go:3102` (hookHomeRecording) and `:3198` (TestHeadlessReportsAFailingHookOnStderr) still write their config fixture in the retired `hooks:`/`events:`/`command:` schema — they pass only via the startup migration, so they no longer exercise the live `reactions:` schema the run delivered.
