---
title: TestHookRunnerReplaceNeverReportsOnTheCallersGoroutine is timing-coupled and half-dead
status: done
type: task
priority: 3
aliases: [apogee-apz.2]
created: 2026-09-06T19:16:51Z
closed: 2026-09-06T20:08:58Z
reason: delivered by docs/plans/archived/2026-09-06 - 03 - hooks-deferred-findings-plan.md
---

internal/hooks/runner_test.go: a background-drain Report arriving before `close(caller)` would hit the `default` arm and t.Error — it cannot fire today, but the test rests on timing rather than on a barrier. The 'off-caller' half asserts nothing: `offCaller` is only stored and discarded. Give the test a deterministic barrier and an assertion on the off-caller channel. Cite: internal/hooks/runner_test.go.
