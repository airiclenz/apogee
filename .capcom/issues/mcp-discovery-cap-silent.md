---
title: MCP discovery drops tools past its caps without a word
status: done
type: bug
priority: 3
plan: 2026-11-13 - 00 - urgent-defects-and-doc-truth-plan
created: 2026-11-13T09:11:49Z
closed: 2026-11-13T11:14:35Z
reason: resolved by 2026-11-13 - 00 - urgent-defects-and-doc-truth-plan (archived at 14fe8a72), item 11
---

listServerTools (internal/mcp/client.go) stops at 64 pages / 512 tools and skips schemas over 64 KiB silently; the manual names only three other limits.

## Acceptance

- A notice per server names each cap hit
- docs/manual/configuration.md lists the three discovery caps
