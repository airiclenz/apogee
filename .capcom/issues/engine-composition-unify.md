---
title: "One engine composition for the session and the Firing (architecture review 2026-11-02 #11)"
status: dropped
type: task
priority: 3
created: 2026-11-02T20:02:20Z
closed: 2026-11-21T16:14:32Z
reason: Shared rules already exist (projectConfig, resolveModelBindings); the rest differs in kind (ADR 0083 parts 4/5). Triage 2026-11-21.
---

Found by docs/reviews/archived/architecture-review-2026-11-02.html, verified 2026-11-02 at 00d5bf9f. Verified PARTLY, not plan-ready: wireSession is 489 lines and main-only, but the prompt/profile rules are already library code. Revisit with a short design pass after plans '2026-11-02 - 04 - seat-and-resolved-entry-plan.md' and '2026-11-02 - 07 - observe-reaction-host-plan.md' land.

## Acceptance

- Design pass done: a plan written or the issue closed.

## Log

- 2026-11-21 Airic Lenz: Triage 2026-11-21 at 7869cca7 (read-only sub-agent check, not re-verified): DOUBTFUL — shared rules already exist (wire_config.go:37 projectConfig, resolveModelBindings); rest differs in kind per ADR 0083 parts 4/5
