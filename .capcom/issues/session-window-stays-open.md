---
title: A session resumed mid-Exchange keeps its session-started window open
status: done
type: bug
priority: 2
plan: 2026-10-21 - 00 - user-session-advice-plan
created: 2026-10-19T18:51:04Z
closed: 2026-10-21T12:34:38Z
reason: step() shuts the session-started window on a resumed mid-Exchange conversation (item 2); resolved by plan 2026-10-21 - 00 - user-session-advice-plan (archived at 51b3a10d)
---

The session-started pre-first-prompt window (`sessionWindow`, `internal/agent/agent.go:2146`) is opened by a boundary's announcement and shut only by `Submit` (`internal/agent/agent.go:918`) or by a Wake that queues its notes (`internal/agent/background.go:1177`). A resumed session that is still mid-Exchange and continues through `Step` never passes either, so its window stays open after the conversation has resumed. A later reaction reload (pane or `/settings` adoption, `apogee project adopt`, a `config.yaml` edit) that arms a `session-started` entry then fires it for that session through `SessionStartedFor`, although the session is no longer before its first prompt.

Found deferred at item 5 of the session-started-reactions plan.

## Acceptance

- A session resumed mid-Exchange that continues through `Step` shuts its pre-first-prompt window when the conversation resumes; a `session-started` entry armed after that waits for the next boundary.
- A test in `internal/agent/sessionstart_test.go` drives that resume-and-Step path and pins that the later reload fires nothing.
