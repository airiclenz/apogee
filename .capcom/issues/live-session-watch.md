---
title: Watch a running session live, read-only, from /sessions
status: open
type: feature
priority: 2
plan: 2026-10-17 - 03 - live-session-watch-plan
created: 2026-10-17T17:13:38Z
---

A person cannot watch a running apogee session. Headless and daemon runs save their record once,
at the end, so while they run `/sessions` does not list them, and `--resume <id>` refuses a session
that another live instance holds. The only view of an unattended run (Seqcom: `apogee headless
--mode auto` under systemd as its own user, with its own home) is the stderr narration in the
journal.

Wanted: a read-only **watch mode** in the TUI. It follows another instance's live session as the
TUI renders it: streamed text, tool cards, sub-agents (read-only run views), and thinking in the
`/thinking` pop-up. It works for every Driver (TUI, headless, daemon). Every Driver writes a
per-session event journal while a session runs. `/sessions` marks live sessions, and opening one,
or `--resume <id>` on one, enters watch mode. Configured homes of other users (`sessions.watch-homes`,
shared through `sessions.share-group`) are listed and watchable read-only.

Design calls (owner, 2026-10-17) and the items are in the plan. The research is in
`docs/handoffs/2026-10-17 - 00 - live session watch mode.md`, which is local only.

## Acceptance

- While a headless run, a daemon Firing or a TUI session is running, a second TUI lists it in
  `/sessions` as live, and opening it (or `--resume <id>`) paints that session's transcript live and
  read-only. Thinking is visible in `/thinking`. The watcher never saves, holds or writes.
- When the owner finishes, the watcher shows that the session finished, or that the owner is gone
  without a final save. In your own home, ⏎ then resumes through the normal door.
- A session under a home listed in `sessions.watch-homes`, whose owner shares it through
  `sessions.share-group`, is listed, tagged and watchable read-only by another user in that group.
- The decision is recorded as an ADR. The manual documents watch mode, the journal and both keys.
  `make check` passes.
