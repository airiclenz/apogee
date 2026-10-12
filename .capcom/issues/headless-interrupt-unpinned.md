---
title: "headless: second-interrupt notice text and the skipped Confiner Close are unpinned"
status: done
type: task
priority: 3
aliases: [apogee-dsr]
created: 2026-09-07T14:29:06Z
closed: 2026-09-07T17:22:49Z
reason: Closed by test-only additions; make check green
---

Deferred from plan 'docs/plans/2026-09-07 - 00 - headless-event-lines-plan.md'. The second-interrupt test asserts through the production `secondInterruptNotice` constant, so no test pins the literal user-facing text. Nothing pins that the hard exit skips the Confiner `Close` either — only that `run_finished` is absent.
