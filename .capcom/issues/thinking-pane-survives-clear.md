---
title: /thinking pane keeps the closed session's reasoning after /clear or /new
status: done
type: bug
priority: 2
plan: 2026-09-24 - 00 - thinking-pane-session-boundary-plan
aliases: [apogee-thinking-pane-survives-clear]
created: 2026-09-24T09:46:03Z
closed: 2026-09-24T14:13:15Z
reason: "Resolved by plan 2026-09-24 - 00 - thinking-pane-session-boundary-plan (archived a9e947f4): /clear, /new, resume and fork reset the thinking and advice boards"
---

Starting a new session with /clear (or its alias /new) leaves the /thinking pane showing the prior session's thinking. Expected: the pane opens empty in the fresh session, like a launch.

Cause (read, not yet test-confirmed): Model.resetSessionView (internal/tui/commandrun.go:195) wipes transcript, usage, run views, title state etc. but never resets m.thinking (thinkingBoard, internal/tui/model.go:451). No code path resets the board at all.

Siblings to check in the same fix: m.advice (/advice board, model.go:235) and m.wire (Inspector ring, model.go:246) are also never reset — decide per board whether it belongs to the session. Also check the session-switch / resume path in internal/tui/sessions.go (~line 618), which mirrors /clear's reset.

Test: driven TUI test — stream thinking, /clear, open /thinking, assert the empty-pane row (thinkingEmptyRow).
