---
title: ADR 0045 context
---

A smart model orchestrating a session pays smart-model tokens for every delegated sub-task,
because a sub-agent inherits the parent's Upstream verbatim — the whole inheritance is one
line, `newAgent(childCfg, a.upstream)` (`internal/agent/subagent.go`). Meanwhile the pieces a
cheaper arrangement needs already exist: the `servers:` list is the single definition of every
server the session can talk to (ADR 0036),
the Parallel-agents cap is already per-server
(ADR 0039),
and the heartbeat already knows how to observe a server and apply its facts at a boundary
(ADR 0024). What
was missing was a decision: which server takes delegations, how apogee learns that server's
facts, and what a child runs *as* when it lands there — a small model doing grunt work wants
the Mechanisms a smart parent runs without (the parent may sit in Bypass; the child may need
guided decomposition). A grill on 2026-08-11 settled the shape; this record is that
ratification. The child model's thinking-tag profile rides the per-model profile resolution
ratified the same day (ADR 0044, reserved by the per-model-profiles plan, which executes
first — that is why this record is 0045 yet may land in the tree before 0044).
