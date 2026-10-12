---
title: ConPTY Console may lose a short-lived command's final frame
status: done
type: bug
priority: 3
labels: [windows]
plan: 2026-10-04 - 00 - windows-console-confinement-plan
aliases: [apogee-conpty-final-frame-lost]
created: 2026-10-03T18:24:42Z
closed: 2026-10-04T21:18:03Z
reason: "Resolved by plan 'windows-console-confinement-plan' item 1 (archived at d5012578): reap drains the reader before closing the ConPTY output pipe."
---

Deferred from plan 'docs/plans/archived/2026-10-03 - 00 - table-stakes-and-image-input-plan.md' item 15 (e9103644); unverified on a Windows host. internal/console/process_windows.go:133 reap closes the pseudoconsole as soon as the process exits, and internal/platform/conpty_windows.go:330-332 releaseConsole closes c.out right after ClosePseudoConsole, so a short-lived command's final painted frame may be lost before the reader drains it.
