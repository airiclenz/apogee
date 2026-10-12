---
title: "daemon: api-key-cmd resolving inside a schedule's Auto workspace is not fenced"
status: done
type: bug
priority: 4
aliases: [apogee-add]
created: 2026-09-15T06:08:11Z
closed: 2026-09-16T21:50:05Z
reason: daemon fences api-key-cmd against the Firing workspace (e0650310)
---

From docs/reviews/architecture-review-2026-09-14.html in-passing defects, triaged 2026-09-15: the key-resolver exec-fence root is '' in cmd/apogee/daemonfire.go and probemodel.go — DELIBERATE and documented (no single workspace; keys memoised per entry; probe reads no workspace), so NOT the review's 'drift'. The narrow real gap: a daemon api-key-cmd: that resolves while a Firing runs in Auto inside that schedule's workspace is not refused the way the TUI/headless fence would refuse it. Decide whether the daemon should fence per Firing workspace at resolve time.
