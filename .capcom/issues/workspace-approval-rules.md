---
title: Persisted per-workspace approval rules
status: done
type: feature
priority: 3
plan: 2026-10-10 - 00 - project-config-and-allow-rules-plan
aliases: [apogee-zlg]
created: 2026-09-07T10:48:55Z
closed: 2026-10-10T17:41:43Z
reason: "Resolved by plan 'project-config-and-allow-rules' (docs/plans/archived/2026-10-10 - 00 - project-config-and-allow-rules-plan.md, archived at 96463ed4): persisted adoption-gated allow rules for terminal and MCP; hardening follow-ups in apogee-allow-cd-cdpath, apogee-allow-rule-env-vectors, apogee-allow-suggest-interpreters."
---

Gap 7 of the 2026-09-06 contender gap assessment. DEFERRED by the owner 2026-09-07 (no design work, no plan yet).

Today approvals are in-process digests only — no persisted allow rules, so ask-before mode re-asks every relaunch. opencode persists rules like '"git *": allow' plus path globs, per agent; Claude Code has a settings allowlist.

Security surface — must be grilled before any plan. Related: apogee-9d8 (configurable tool x mode security matrix). Effort M.
