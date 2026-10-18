---
title: An edited pinned reaction script is not re-proposed in-session
status: open
type: bug
priority: 2
created: 2026-10-18T18:44:06Z
---

When a pinned project script an adopted repo reaction runs is edited mid-session, every spawn is refused (`internal/agent/syncexec.go:141-155`, `changedPinNotice`/`checkProjectPin`), and the notice says the entry is proposed again. But the entry is re-proposed only when the config layer next reads the Project config: a script edit triggers no reload, so the adoption pane does not re-ask in-session until the Project config itself (or the adoption record) changes or apogee restarts. The reaction stays refused on every Moment meanwhile.

Deferred out of plan `2026-10-18 - 00 - project-config-exec-keys-plan`.

## Acceptance

- After a pinned reaction script changes mid-session, the user is asked again in that session (at the next idle moment) without editing the Project config or restarting, and adopting the new content arms the reaction.
- A test pins it (`internal/agent` or `cmd/apogee`).
