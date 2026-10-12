---
title: TestStartPseudoConsoleRunsTheCommand closes the ConPTY before draining it
status: open
type: bug
priority: 1
plan: 2026-10-12 - 02 - test-suite-stability-plan
created: 2026-10-12T18:11:06Z
---

The test calls pty.Close() before its drain reaches EOF; CloseOutput cancels the in-flight read and loses conhost's final frame. Failed CI on 7f22d7df (2026-10-12). Review §3.

Findings: `docs/reviews/2026-10-12 - test-suite-stability-speed-memory.md`.
