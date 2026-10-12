---
title: Rule-answered lines inherit BASH_FUNC_ and PATH-dot env vectors
status: done
type: bug
priority: 2
plan: 2026-10-11 - 00 - allow-rule-hardening-plan
aliases: [apogee-allow-rule-env-vectors]
created: 2026-10-10T17:01:12Z
closed: 2026-10-11T19:52:25Z
reason: "Resolved by allow-rule-hardening-plan (2026-10-11 - 00), archived at 881ccd04: items 1-3 mark rule-answered calls, scrub CDPATH/BASH_ENV/ENV/BASH_FUNC_* from their env, and a fully scoped PATH falls back to /usr/bin:/bin instead of PATH=."
---

Deferred from plan 'docs/plans/2026-10-10 - 00 - project-config-and-allow-rules-plan.md' item 9. Env-level vectors beyond CDPATH — BASH_FUNC_* exported functions, a PATH holding `.` — sit outside the pure allow matcher's reach (internal/security/allowmatch.go:55 MatchAllowRules sees only the command text). The terminal tool should scrub or refuse them when a line is answered from rules (item 11, internal/agent/dispatch.go:1589).
