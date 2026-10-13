---
title: Windows reap joins the ConPTY reader for only 5 s before CloseOutput
status: open
type: bug
priority: 3
plan: 2026-10-13 - 00 - open-bugs-sweep-plan
created: 2026-10-12T21:51:20Z
---

Production `reap` (`internal/console/process_windows.go`) uses the safe teardown order — Release, join the reader, then CloseOutput — but joins the reader only until `closeJoinTimeout` (5 s, `internal/console/process_shared.go`) passes before calling `CloseOutput` anyway. conhost flushes a short command's final frame as the pseudoconsole closes; under heavy load that flush can arrive after the 5 s, and `CloseOutput` then cancels the read carrying it (`CancelIoEx`) — the same final-frame loss the ConPTY test fix (plan item 1) closed on the test side.

This is a product edge, not only a test one: `TestProcessKeepsTheFinalFrameOfAShortCommandOnWindows` (`internal/console/process_windows_test.go`) is the test that would show it.

**Acceptance:** the join's bound is justified against a loaded conhost (or the join waits on a signal that conhost has finished flushing rather than on a fixed time), and a loaded run keeps the final frame.

Source: `docs/reviews/2026-10-12 - test-suite-stability-speed-memory.md` §3, "The other Windows risks are all possible, none confirmed".
