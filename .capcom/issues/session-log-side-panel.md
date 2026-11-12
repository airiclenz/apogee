---
title: Show the start-up Session log in the side panel
status: open
type: feature
priority: 4
deps: [startup-session-log]
created: 2026-11-12T14:52:32Z
---

The start-up **Session log** (`startup-session-log`; spec `layout.md` §"The start-up box") ships transcript-only: one collapsed `✦ Session log (N notes)` block under the start-up box. The original request asked that, when the side panel is available, the log may live there instead. This issue is that move: decide whether the panel shows the log, and whether the transcript block stays once it does.

v1 left it out on purpose (plan `docs/plans/2026-11-12 - 00 - go127-gate-and-session-log-plan.md`, side-panel design call, 2026-11-12). [ADR 0110](.capcom/knowledge/decisions/adr-0110.md) D4 renders panel sections from engine Events only, and start-up notes are not Events; D1 makes the panel a glance at live state, while the log is a record of how the session started. A panel section for it therefore needs either an Event the engine emits for the notes (ADR 0031: Driver-neutral, wire-silent engine) or an amendment to ADR 0110 D4.

## Why

On a wide terminal the panel is the place for session state, and a log there would leave the first screen of the transcript to the conversation (owner request, 2026-11-12).

## Acceptance

- A decision recorded: the panel shows the Session log (with the ADR 0110 D4 route it takes) or the idea is declined with the reason.
- If shown: a driven TUI test covers the panel section, and `docs/layout/side-panel-layout.md` specifies it.
