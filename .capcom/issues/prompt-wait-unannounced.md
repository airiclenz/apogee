---
title: apogee never signals that it is waiting on the user (approval, ask, finished Turn)
status: done
type: feature
priority: 3
plan: 2026-11-01 - 00 - tui-row-detail-gauge-and-notify-plan
created: 2026-11-01T10:43:53Z
closed: 2026-11-01T14:08:29Z
reason: Items 9-10 resolved by plan 2026-11-01 - 00 - tui-row-detail-gauge-and-notify-plan (archived at 639c3bfc)
---

An approval or ask prompt opens silently, and a Turn that finishes while the terminal is in the background says nothing. A user who switched windows has no cue that apogee is blocked on them (IDEAS.md: "Send notification on questions / needed user input").

Owner calls 2026-11-01: key `ui.notify: off | bell | osc9 | both`, default `bell`; fires when an approval or ask prompt opens (pickers and re-offered dismissed prompts stay silent); a finished or errored Turn notifies only while the window is unfocused (focus reporting on); OSC 9 is wrapped for tmux passthrough when `$TMUX` is set.

## Acceptance

- With the default config, opening an approval or ask prompt writes BEL once; `off` writes nothing; `osc9` writes OSC 9; `both` writes both.
- A Turn ending while the window is blurred notifies; while focused it does not.
- `ui.notify` is documented in docs/manual/configuration.md and editable in /settings; IDEAS.md item ticked.
