---
title: "sub_agent: shared context files injected once (a files: argument rendered as one fenced prefix)"
status: open
type: feature
priority: 3
plan: 2026-10-19 - 00 - sub-agent-context-and-server-idle-timeout-plan.md
aliases: [apogee-vi5]
created: 2026-09-14T20:28:24Z
---

Session-mining item 12 / quant §5: children re-read bundle.md/_shared.md/tools.md 140× each per /code-audit run (~525 reads the harness could inject once). Shape: files: []string on the sub_agent schema, read-fenced and size-capped, rendered once after the context files and before the task (user-role prefix keeps ADR 0023 §6 unchanged; a system part would be an ADR 0023 addendum and a better cache prefix). New model-facing surface — bench evidence before default-on. Deferred by owner call.

## Log

- 2026-09-14 beads: spec: docs/reviews/archived/session-mining-2026-09-14.md
