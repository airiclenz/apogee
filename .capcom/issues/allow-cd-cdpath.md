---
title: Rule-covered cd honours an inherited CDPATH
status: done
type: bug
priority: 2
plan: 2026-10-11 - 00 - allow-rule-hardening-plan
aliases: [apogee-allow-cd-cdpath]
created: 2026-10-10T17:01:10Z
closed: 2026-10-11T19:52:26Z
reason: "Resolved by allow-rule-hardening-plan (2026-10-11 - 00), archived at 881ccd04: item 2 scrubs CDPATH from rule-answered terminal lines (TestAllowRuleCdIgnoresCDPATH)."
---

Deferred from plan 'docs/plans/2026-10-10 - 00 - project-config-and-allow-rules-plan.md' item 9. A covered `cd sub` (internal/security/allowmatch.go:229 allowedCdDestination) still honours an inherited CDPATH in the child shell — bash searches CDPATH before ./sub — so a CDPATH env entry could redirect it outside Root; the pure matcher cannot see the env. The terminal tool should scrub CDPATH, or the engine's rule answer (item 11, internal/agent/dispatch.go:1589) refuse cd lines when it is set.
