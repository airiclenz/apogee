---
title: "First beat prints 'context window changed: unknown → N' under a box that already shows N"
status: done
type: bug
priority: 2
created: 2026-11-12T08:19:33Z
closed: 2026-11-12T08:23:56Z
reason: quiet first-contact beat no longer notes an unknown → known window (TestFirstContactWindowNote)
---

On launch with a configured model and an unknown window, the first clean beat binds the window and `rebindNote` (`internal/tui/heartbeat.go`) prints `context window changed: unknown → 1M`. The start-up box is restated in place on that same beat and already shows `context 1M`, so the line repeats it. The quiet first-contact rule (`rebindIntent.quietSeed`) silences only the `oldModel == ""` seed, not an unknown → known window.

## Why

The line reads as a change the human did not make and adds noise to every cold start (owner screenshot, 2026-11-12).

## Acceptance

- A quiet first-contact beat that binds a window where none was known prints no window note.
- A known window that changes (32k → 16k), and an unknown → known change after a failed beat or a server switch, still print one.
- A table test in `internal/tui/heartbeat_test.go` pins both.
