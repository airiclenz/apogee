---
title: "Decide the mcp: handler posture for Reactions (ADR 0076 D3, reserved)"
status: open
type: task
priority: 3
plan: 2026-10-29 - 01 - mcp-reaction-handler-plan
aliases: [apogee-03a]
created: 2026-09-07T19:50:52Z
---

ADR 0076 reserved the mcp: handler key (a Reaction whose handler is an MCP server/tool). Obstacle: a reaction calling an MCP tool at a seam collides with the Approver — a tool needing approval in the current mode would prompt inside a prompt. Options recorded: inherit the tool's posture and skip when approval would be needed; or approval-free tools only. Decide when a case arrives (llama-launcher MCP was the candidate). Day one is argv + webhook only.
