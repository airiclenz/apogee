---
title: A side-panel click may act while an approval or /settings is open
status: done
type: bug
priority: 2
created: 2026-11-08T21:33:39Z
closed: 2026-11-09T09:25:18Z
reason: "Resolved by plan '2026-11-08 - 03 - adr-0111-navigation-and-mcp-status-plan' item 1 (archived at 82491d30): panel clicks under modal panes are claimed and inert; a watched pin click flashes the watch refusal."
---

The panel click is resolved before every pane (`internal/tui/mouse.go`) and
`followPanelTarget` (`internal/tui/sidepanel.go`) checks no state, so a click on a panel row
can apparently open a Run view or `/usage` while an approval or `/settings` is up. The pin
glyph's click also seems not to be gated in watch mode, unlike the grip and `/panel`.
Unverified; found in the ADR 0111 code survey, 2026-11-08. Navigation mode will call the same
path, so the gating rule matters for it too.

## Acceptance

- A driven test shows what a panel click does while an approval, an ask question and `/settings` are each open; it is refused or deferred, never acted under the modal surface.
- A pin click in watch mode is refused like the grip, or the record says why it is allowed.
