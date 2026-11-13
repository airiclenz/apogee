---
title: MCP discovery drops tools past its caps without a word
status: open
type: bug
priority: 3
plan: 2026-11-13 - 00 - urgent-defects-and-doc-truth-plan
created: 2026-11-13T09:11:49Z
---

listServerTools (internal/mcp/client.go) stops at 64 pages / 512 tools and skips schemas over 64 KiB silently; the manual names only three other limits.

## Acceptance

- A notice per server names each cap hit
- docs/manual/configuration.md lists the three discovery caps
