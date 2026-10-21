---
title: No test drives the Wake path that shuts the session-started window
status: done
type: task
priority: 3
plan: 2026-10-21 - 00 - user-session-advice-plan
created: 2026-10-19T18:51:04Z
closed: 2026-10-21T12:34:38Z
reason: Wake-queues-held-notes test pins the window shut (item 2); resolved by plan 2026-10-21 - 00 - user-session-advice-plan (archived at 51b3a10d)
---

`Agent.Wake` shuts the session-started pre-first-prompt window when it queues its background notes and opens an Exchange without a typed prompt (`internal/agent/background.go:1177`), but no test drives that path: the window tests in `internal/agent/sessionstart_test.go` shut it only through `Submit`. A regression that left the window open across a Wake would go unseen, and a later reload would fire `session-started` for a session already past its first Exchange.

Found deferred at item 5 of the session-started-reactions plan (it needs held background notes to queue).

## Acceptance

- A test holds background notes, Wakes the Agent so they queue, then arms a `session-started` entry by a live swap and pins that it does not fire for the current session.
