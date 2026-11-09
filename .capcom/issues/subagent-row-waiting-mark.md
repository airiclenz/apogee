---
title: A sub-agent row gives no sign its run waits on an approval
status: done
type: feature
priority: 3
created: 2026-11-08T21:33:39Z
closed: 2026-11-09T09:25:18Z
reason: "Resolved by plan '2026-11-08 - 03 - adr-0111-navigation-and-mcp-status-plan' items 3-4 (archived at 82491d30): gated sub-agent rows read waiting, cleared per gate, also in watch mode."
---

Every gate opens the shared approval pane, but gates queued behind the one showing have no
indicator, and the side panel's sub-agent rows show no wait. ADR 0111 decision 11: while a
run has an `ApprovalEvent` requested and not yet decided (showing or queued), its row's
phase word reads `waiting` with its own glyph. The fold closes the gate on decided, on an
ErrorEvent for that gate, or when the run or Turn finishes. `waiting` must fit the 7-column
phase field.

Design: [ADR 0111](.capcom/knowledge/decisions/adr-0111.md).

## Acceptance

- Two sub-agents gated at once: both rows read `waiting` while one approval shows and the other queues; each clears when its own gate is decided.
- A gate ended by a cancel or an Approver error clears the mark when the run or Turn finishes.
- Watch mode replaying the journal shows the same marks.
