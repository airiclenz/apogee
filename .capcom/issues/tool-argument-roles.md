---
title: "tools: per-tool argument-role declaration replaces payloadKeys' global list; MCP tools need a stated default"
status: done
type: task
priority: 3
aliases: [apogee-ws7]
created: 2026-09-15T06:08:11Z
closed: 2026-09-27T06:09:16Z
reason: "Resolved by plan 2026-09-26 - 00 - code-audit-fixes-plan items 1-3 (archived a501c480): one per-tool ArgRoles declaration replaces PromptTool/ReadSourceTool/ShellCommandTool and the global payloadKeys; MCP/undeclared tools are fully inspected (owner design call, ADR 0012 amendment)."
---

Deferred from docs/reviews/architecture-review-2026-09-14.html candidate 15 on 2026-09-15 (plan 2026-09-15 - 01 lands the derived KnownToolNames / one HostToolsOf / Classify half). Remaining: internal/security/dangerous.go payloadKeys is a global deny-list keyed by OTHER tools' parameter names and applies to MCP tools too — a third-party tool whose action rides content/body/message/query is invisible to the footgun guard (precision loss, not a bypass: the fold can only over-exempt). Proposed: one argument-role declaration per tool (payload | action | read-source | prompt) replacing ReadSourceTool + PromptTool + payloadKeys. DESIGN CALL NEEDED before implementing: the default for an MCP tool with no declaration (fully inspected ⇒ the ADR 0012 ~/.ssh-in-a-document false positive returns for MCP content/body).
