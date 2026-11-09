---
title: ADR 0111 consequences
---

- `blockcursor.go` grows a region and a panel stop list; the key-claim order gains no new rung (the
  mode is still one `transcriptClaimants` entry). `TestKeyClaimOrderMatchesTheDocumentedPrecedence`
  and the help legend change with it.
- The panel's layout pass must expose its stops in paint order with identities, shared by click,
  hover and cursor, so the three stay one map.
- The overlay is a second paint position for the same panel content; the docked panel's width
  setting governs both, and the overlay never writes it.
- A new domain Event (and its `eventjson` encoding) is additive. The MCP dial owner in
  `cmd/apogee` must keep the last failure's cause, which it drops today. A mid-session server death
  is still not detected (`internal/mcp` has no session watcher); the Event reports only what the
  dial lifecycle knows.
- `ui.side-panel.sections` gains a fifth name, `mcp`; an older config without it still loads, with
  `mcp` appended in `auto`.
- The `waiting` phase word must fit the sub-agent row's 7-column phase field.
- The IDEAS item "complete key-based navigation" is covered by this record.
- Work: issues `panel-keyboard-navigation`, `cursor-colour-only-mark`, `panel-narrow-overlay`,
  `mcp-lifecycle-event`, `mcp-live-status-view`, `subagent-row-waiting-mark`; defects found on the
  way: `subagent-row-elapsed-overflow`, `panel-click-ignores-modal`; parked:
  `mcp-session-death-undetected`.
