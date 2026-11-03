---
title: MCP model-facing names collide when an alias or tool name contains `__`
status: done
type: bug
priority: 2
plan: 2026-11-03 - 00 - code-audit-fixes-plan
created: 2026-11-03T14:20:29Z
closed: 2026-11-03T16:07:40Z
reason: Resolved by plan '2026-11-03 - 00 - code-audit-fixes-plan' (archived in c3346711)
---

From docs/reviews/code-audit-2026-11-03.md; fixed by item 14 of `docs/plans/2026-11-03 - 00 - code-audit-fixes-plan.md`. internal/mcp/tool.go:84-127 builds `<alias>__<tool>` and alters it only when out of pattern or over 64 characters. Alias `a` + tool `b__c` and alias `a__b` + tool `c` both yield `a__b__c`; the hash disambiguation never applies.

## Why

One tool silently shadows the other in the flat registry; snake_case names with `__` are common.

## Acceptance

- Server names that make tool names ambiguous are refused (or hashed); a test with two such servers lists both tools distinctly or fails validation.
