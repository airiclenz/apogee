---
title: RunHookSubprocess never checks the SubprocessPermit
status: done
type: bug
priority: 2
plan: 2026-09-24 - 00 - review-findings-follow-through-plan
aliases: [apogee-subprocess-permit-unread]
created: 2026-09-24T09:16:53Z
closed: 2026-09-24T12:47:48Z
reason: "Resolved by review-findings-follow-through-plan (30ad3bbc) item 9: RunHookSubprocess refuses to spawn without a subprocess permit"
---

domain.SubprocessPermitFromContext has no production reader; the contract (internal/domain/confinement.go) says a hook that reads false must not spawn, but tools.RunHookSubprocess spawns regardless. Not exploitable today: the one caller (syncexec) installs the permit right before spawning. Owner call 2026-09-24: enforce it in RunHookSubprocess.
