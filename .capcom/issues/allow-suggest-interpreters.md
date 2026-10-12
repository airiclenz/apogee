---
title: Allow-rule suggestion misses unlisted interpreters
status: done
type: bug
priority: 3
plan: 2026-10-11 - 00 - allow-rule-hardening-plan
aliases: [apogee-allow-suggest-interpreters]
created: 2026-10-10T17:01:11Z
closed: 2026-10-11T19:52:27Z
reason: "Resolved by allow-rule-hardening-plan (2026-10-11 - 00), archived at 881ccd04: item 4 widens allowInterpreters and strips trailing versions in the suggestion lookup."
---

Deferred from plan 'docs/plans/2026-10-10 - 00 - project-config-and-allow-rules-plan.md' item 9. SuggestAllowRules (internal/security/allowmatch.go:88) knows only the plan's listed interpreters (allowInterpreters, internal/security/allowmatch.go:123) — dash -c, ksh -c, python3.12 -c, nodejs -e, php -r, awk '…' get their bare name suggested as a rule, which would allow any script. Extend allowInterpreters, or match version-suffixed names.
