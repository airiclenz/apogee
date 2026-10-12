---
title: "tuitest PTYDriver: Kill() on an already-exited child skips awaitExit's drain, leaving Bytes() completeness unpinned"
status: done
type: bug
priority: 3
aliases: [apogee-4al]
created: 2026-09-14T05:21:08Z
closed: 2026-09-14T05:42:22Z
reason: fixed in e7c555c0
---

Deferred from the 2026-09-13 - 00 headless/CI hygiene run's follow-up D-3 (8b966d78, fix(tuitest): drain the PTY output pump before awaitExit reports the child gone).

`Kill()` on a child that already exited by itself returns on its `<-d.reaped` early path (internal/tuitest/pty.go:262-266) without reaching `awaitExit` (internal/tuitest/pty.go:307), so the pump drain that commit added is skipped on that path and docs/design/test-drivers.md:541's 'complete once Quit() or Kill() has returned' guarantee for `Bytes()` has one unpinned corner. No caller hits it today: every e2e Kill is on a live child. `TestPTYDriverBytesAreCompleteAfterExit` (internal/tuitest/pty_test.go) covers Quit on a live child only.
