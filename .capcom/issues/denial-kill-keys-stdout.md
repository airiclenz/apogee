---
title: "Confinement denial kill keys on stdout text too: a command that merely prints 'permission denied' is killed"
status: done
type: bug
priority: 3
aliases: [apogee-1mj]
created: 2026-09-14T20:28:24Z
closed: 2026-09-16T17:57:54Z
reason: "fix(platform): the confinement denial kill watches stderr on pipes and matches a line-anchored signature"
---

Session-mining §1 / item 4: fc413fb5 [86],[100],[111] false kills from a log that printed the signature; 0a4546d8 curl 23 / useradd EACCES missed. internal/platform/denialkill.go watches one DenialKillWriter on both stdout and stderr (subprocess.Run). ADR 0056 D2 ratifies the signature watch and accepts the false positive, so fixing it (stderr-only watch, or a line-anchored '<prog>: <path>: Permission denied' shape) needs a dated ADR 0056 amendment. Deferred from the 2026-09-14 session-mining plans by owner call.

## Log

- 2026-09-14 beads: spec: docs/reviews/session-mining-2026-09-14.md
