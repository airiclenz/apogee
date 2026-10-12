---
title: POSIX Ask-opened console_send test does not prove the line ran
status: done
type: task
priority: 4
plan: 2026-10-03 - 00 - table-stakes-and-image-input-plan
aliases: [apogee-console-send-test-echo]
created: 2026-10-03T18:24:43Z
closed: 2026-10-03T23:28:50Z
reason: "Resolved by refocus-followups-plan (docs/plans/archived/2026-10-03 - 01 - refocus-followups-plan.md, db1550b0) item 2: POSIX send tests assert on output only execution produces"
---

Deferred from plan 'docs/plans/archived/2026-10-03 - 00 - table-stakes-and-image-input-plan.md' item 16 (7ece887a). internal/tools/console_send_test.go:271-277: TestConsoleSend_AskOpenedConsoleStillSendsUnderABoxOnPOSIX sends 'echo still-sends' and checks the result contains 'still-sends', which the echoed input also matches, so it does not prove the line ran. Check for output only the executed command produces.
