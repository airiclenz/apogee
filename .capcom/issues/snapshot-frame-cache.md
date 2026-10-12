---
title: tuitest Screen.Snapshot rebuilds the full frame and styled render on every 20 ms WaitFor poll
status: open
type: task
priority: 3
created: 2026-10-12T12:28:47Z
---

Every `WaitFor` poll (every 20 ms) calls `Screen.Snapshot()` (`internal/tuitest/screen.go:141`), which builds a full ~3,000-cell frame plus a styled `term.Render()` while holding the emulator lock, even when no bytes arrived since the last poll. `holding` does the same.

**Fix:** cache the frame until `Write` changes the screen, and build the styled render only on demand (a failing wait prints it). Measure first: profile a driven test under `make test` slow mode before and after.

**Acceptance:** driven tests in `internal/tui` and `cmd/apogee` pass unchanged; the profile shows the Snapshot cost gone from idle polls.

Source: option C of the archived handoff `docs/handoffs/archived/2026-10-11 - 01 - make-check-slowness-and-oom.md` (local only).
