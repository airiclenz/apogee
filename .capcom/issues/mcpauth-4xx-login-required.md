---
title: Any 4xx from the MCP token endpoint is reported as "login required"
status: done
type: bug
priority: 2
plan: 2026-11-03 - 00 - code-audit-fixes-plan
created: 2026-11-03T14:20:29Z
closed: 2026-11-03T16:07:40Z
reason: Resolved by plan '2026-11-03 - 00 - code-audit-fixes-plan' (archived in c3346711)
---

From docs/reviews/code-audit-2026-11-03.md; fixed by item 15 of `docs/plans/2026-11-03 - 00 - code-audit-fixes-plan.md`. internal/mcpauth/handler.go:274 returns every 4xx, including 408, 429 and a misconfigured-secret 400/401, as `ErrLoginRequired`, telling the user to run `apogee mcp login <name>`.

## Why

A rate-limited or timed-out refresh leaves the refresh token valid, but the connection fails instead of retrying.

## Acceptance

- `ErrLoginRequired` only for OAuth `invalid_grant`/`invalid_client`, or a 400/401 without a parseable code.
- 408, 429 and other 4xx return a plain error naming the status; tests cover each.
