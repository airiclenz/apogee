---
title: ADR 0111 context
---

ADR 0110 shipped the side panel mouse-only on purpose: decision 3 left focus and ←/→ navigation to
"the key-navigation work, which this panel unblocks", and decision 8 left the panel's key binding
"after an audit of free keys". That work is the IDEAS item "Complete key-based navigation —
including side-ways arrow keys" (IDEAS.md:12).

A survey of six peer agents (opencode, pi, Crush, Hermes, Claude Code, Warp), verified claim by
claim on 2026-11-08 (`docs/design/side-panel-findings.md`), ranked apogee's remaining gaps:
keyboard access, third-party sections, MCP/LSP status, a narrow-terminal fallback, a docked
session switcher, a needs-attention mark, and row-level detail. It also showed that several gaps
the first cut listed do not exist: sub-agent stop and steer is one click away in Run view,
per-sub-agent spend is in `/usage`, and the default footer already carries git and model.

Facts the design rests on (code survey, 2026-11-08):

- **Keys.** No `focus` field exists; keys are claimed in `keyClaimOrder` (panes, then
  `transcriptClaimants`: Run view, then the block cursor). The block cursor (`blockcursor.go`) is
  entered by `alt+↑`/`alt+↓`, walks the paint's own click map, and claims `↑`/`↓`, `⏎`, `esc` and
  `^x`; `←`/`→` are unclaimed there and move the prompt caret unseen. `ctrl+b` is the textarea's
  character-back key. No key-remapping config exists.
- **Panel targets.** `panelTargetAt` resolves a cell to a target and `followPanelTarget` acts on
  it: Run view for a sub-agent row, the latest write's tool block for a file, the task card, `/usage`,
  and a pin toggle on the header's pin glyph.
- **Narrow windows.** Below 137 columns (`auto`) or 117 (`on`) the panel is not drawn; bare `/panel`
  only says why.
- **MCP.** The engine emits no MCP Event. Per-server state lives in `cmd/apogee`'s wiring
  (`wire_mcp.go`), outcomes reach the TUI only as ephemeral notes at the idle-point commit
  (ADR 0109), the last failure's cause is not kept, and there is no `/mcp` command. apogee has no
  LSP.
- **Attention.** `ApprovalEvent` (requested, decided) is journaled and carries the asking run's
  RunID; its requested phase fires before the prompt slot is taken, so "requested, not decided"
  means showing or queued. Every gate opens the shared approval pane at once, but gates queued
  behind it have no indicator. `ask_user` is top-level only and has no Event.
- **Row width.** At the default width a sub-agent row leaves 12 columns for its name; an elapsed
  time past an hour overflows its 6-column field.
- **Accessibility.** ADR 0052 decision 4: colour never carries a meaning alone. Today's block cursor
  is a selection-tone bar only.
