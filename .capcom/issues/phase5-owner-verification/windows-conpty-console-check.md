---
title: Owner-run check of the ConPTY Console on Windows
status: parked
type: task
priority: 3
labels: [owner-run, windows]
plan: 2026-10-04 - 00 - windows-console-confinement-plan
aliases: [apogee-2uh.6]
created: 2026-10-03T18:11:27Z
---

Real-TUI tail of the Windows Console check (everything a Windows-host go test can drive is automated in internal/tools/console_windows_test.go: open cmd in Ask-Before, non-raw send ending in \r runs, console_read strips escapes, console_close reaps the job tree, and in Auto a confined console_open needs no approval while a console_send to an unconfined Console is demoted with the close-and-reopen advice). Owner-run in the real TUI on a Windows host: (1) open a Console in Ask-Before, then /mode auto and send to it — confirm the visible Approval prompt appears and its wording carries the close-it-and-reopen-it-fenced advice; (2) confirm the Console pane renders the ConPTY output legibly (no stray escape sequences, final frame of a short command kept). Needs a Windows host.
