---
title: Git config fingerprint omits ctime on the BSDs
status: done
type: bug
priority: 3
aliases: [apogee-gitexec-bsd-ctime]
created: 2026-09-29T15:18:59Z
closed: 2026-09-29T16:43:45Z
reason: ctime read on freebsd/netbsd/openbsd/dragonfly
---

Deferred from plan docs/plans/archived/2026-09-29 - 01 - code-audit-fixes-plan.md, item 6. The git config probe fingerprint's ctime check (internal/gitexec/gitexec.go:488 sameChangeTime) covers linux/darwin only (internal/gitexec/ctime_linux.go, internal/gitexec/ctime_darwin.go); the BSDs fall to internal/gitexec/ctime_other.go:1 (build tag !linux && !darwin, no ctime) though item 6's goal names BSD. Their syscall.Stat_t exposes one (Ctimespec on freebsd/netbsd, Ctim on openbsd/dragonfly), so one more build-tagged file adds it; on a BSD a same-length rewrite of a probed config with its mtime restored still keeps the stale probe answer.
