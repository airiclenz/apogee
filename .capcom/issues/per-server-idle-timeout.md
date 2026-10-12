---
title: Per-server stream-idle-timeout override
status: open
type: feature
priority: 4
aliases: [apogee-per-server-idle-timeout]
created: 2026-09-23T20:22:39Z
---

stream-idle-timeout is a root key (internal/config/config.go); users cannot set a shorter window on one entry. Owner call 2026-09-23: out of scope of 'docs/plans/2026-09-23 - 00 - upstream-passthrough-and-measurement-plan.md'; revisit on Layer-2 evidence. Note: keepalive comments reset the timer (apogee-keepalive-starvation), so a lower value alone does not catch OpenRouter starvation.
