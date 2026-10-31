---
title: "An observe mcp: project entry with a moved pin fires no re-ask cue"
status: done
type: bug
priority: 2
plan: 2026-10-31 - 00 - observe-mcp-pin-cue-plan
created: 2026-10-29T19:17:54Z
closed: 2026-10-31T10:55:13Z
reason: "Resolved by plan 2026-10-31 - 00 - observe-mcp-pin-cue-plan item 1 (archived at 95ff3379; fix 57bba370): observe mcp: repo entry refused for a moved pin now fires Config.PinMoved once; pinned by TestMCPObserve* in internal/reactions."
---

An adopted repo observe (`run:`) entry whose handler is `mcp:` and whose adoption pin has moved is refused and reported (`admitProjectPin`, `internal/reactions/exec.go:139`, called from `mcpCaller.Run`, `internal/reactions/exec.go:98`), but it does not fire `Config.PinMoved` (`internal/domain/config.go:322`), the Driver's re-ask cue. The sync-lane path (`checkProjectPin` -> `a.notePinMoved()`, `internal/agent/syncexec.go:162`) and the argv project path (through the Agent's fenced `ProjectRun`) do fire it, because the async executor has no Agent to cue.

Deferred out of plan `2026-10-29 - 01 - mcp-reaction-handler-plan`.

## Acceptance

- An adopted repo observe `mcp:` entry refused for a moved or unreadable pin fires `Config.PinMoved` once, as the sync-lane and argv project paths do, so the Driver re-asks in-session.
- A test pins it (`internal/reactions` or `cmd/apogee`).
