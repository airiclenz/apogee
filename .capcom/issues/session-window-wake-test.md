---
title: No test drives the Wake path that shuts the session-started window
status: open
type: task
priority: 3
created: 2026-10-19T18:51:04Z
---

`Agent.Wake` shuts the session-started pre-first-prompt window when it queues its background notes and opens an Exchange without a typed prompt (`internal/agent/background.go:1177`), but no test drives that path: the window tests in `internal/agent/sessionstart_test.go` shut it only through `Submit`. A regression that left the window open across a Wake would go unseen, and a later reload would fire `session-started` for a session already past its first Exchange.

Found deferred at item 5 of the session-started-reactions plan (it needs held background notes to queue).

## Acceptance

- A test holds background notes, Wakes the Agent so they queue, then arms a `session-started` entry by a live swap and pins that it does not fire for the current session.
