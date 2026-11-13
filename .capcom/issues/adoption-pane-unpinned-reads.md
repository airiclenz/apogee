---
title: Adoption pane does not say launcher reads are unpinned
status: done
type: bug
priority: 3
plan: 2026-11-13 - 00 - urgent-defects-and-doc-truth-plan
created: 2026-11-13T09:11:48Z
closed: 2026-11-13T11:14:35Z
reason: resolved by 2026-11-13 - 00 - urgent-defects-and-doc-truth-plan (archived at 14fe8a72), item 9
---

An adopted MCP server or reaction whose launcher reads in-root files the pin does not name (package.json, Makefile, modules) can change after adoption without a re-ask. The manual and ADR 0104 say so; the adoption pane does not (docs/reviews/code-audit-2026-11-10.md).

## Acceptance

- The TUI pane and CLI adopt render show 'not pinned: anything it reads or runs beyond these'
- ADR 0104 carries a dated amendment rejecting manifest pinning and tree hashing
