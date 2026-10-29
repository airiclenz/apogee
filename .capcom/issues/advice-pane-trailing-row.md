---
title: /advice pane paints an empty row after a Detail ending in a newline
status: done
type: bug
priority: 2
plan: 2026-10-29 - 00 - p2-fixes-plan
created: 2026-10-29T08:33:23Z
closed: 2026-10-29T11:56:23Z
reason: "Resolved by plan 2026-10-29 - 00 - p2-fixes-plan (item 5), archived at 979a6d7d: the /advice pane trims a Detail's trailing newlines."
---

The `/advice` pane (internal/tui/advicepane.go, adviceRows) splits each firing's Detail on "\n" with no trim and wraps every piece, so the empty piece after a final newline becomes an empty row. An advise handler's output keeps its trailing newline in Detail (an `echo` handler always ends in one), so every such firing — post-tool-result, session-started and compaction-finished alike — paints a blank row after its text. Seen 2026-10-21 (docs/plans/archived/2026-10-21 - 00 - user-session-advice-plan.md item NOTES) and left as is.

## Acceptance

- A Detail ending in one or more newlines paints no empty row after its last line in the `/advice` pane; blank lines inside the Detail still paint.
- A pane test covers a Detail that ends in "\n".
