---
title: Per-server stream-idle-timeout override
status: done
type: feature
priority: 4
plan: 2026-10-19 - 00 - sub-agent-context-and-server-idle-timeout-plan.md
aliases: [apogee-per-server-idle-timeout]
created: 2026-09-23T20:22:39Z
closed: 2026-10-19T16:16:29Z
reason: "Resolved by plan '2026-10-19 - 00 - sub-agent-context-and-server-idle-timeout-plan' (archived 1a6c5c70): a servers: entry's stream-idle-timeout overrides the root for that server, applied on bind, /server move and delegation, and documented."
---

stream-idle-timeout is a root key (internal/config/config.go); users cannot set a shorter window on one entry. Owner call 2026-09-23: out of scope of 'docs/plans/2026-09-23 - 00 - upstream-passthrough-and-measurement-plan.md'; revisit on Layer-2 evidence. Note: keepalive comments reset the timer (apogee-keepalive-starvation), so a lower value alone does not catch OpenRouter starvation.
