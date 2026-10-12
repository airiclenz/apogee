---
title: The unattended context-files report has a zero Budget share, so its oversize warning never fires
status: done
type: bug
priority: 3
aliases: [apogee-m67]
created: 2026-09-04T11:05:16Z
closed: 2026-09-09T05:37:44Z
reason: Already delivered by archived plan docs/plans/archived/2026-09-05 - 00 - solvable-now-residues-plan.md (executed 2026-09-06; commits 8871cbec, 49271d23, a5fc1820 among them); verified against HEAD cebd60fd on 2026-09-09 while writing the deferred-findings plan.
---

Deferred from plan 'docs/plans/2026-09-03 - 01 - driver-parity-and-residues-plan.md' (items 3/4/11).

ContextFilesReport.SystemShare is filled from the Agent's Budget at internal/agent/contextfiles.go:281 (report.SystemShare = budget.SystemPrompt), and internal/domain/contextfile.go:42 gates the oversize warning on SystemShare > 0. On the unattended path the Budget derives from configuration alone, so a headless or daemon run against a server whose context window is not pinned in config reports SystemShare 0 — the standing-content warning items 4 and 11 render can then never fire there, however far past its share the standing content grows. An interactive session, which measures after the window is observed, does warn. Either the report must be taken (or re-taken) after the bind observes a window, or the unattended Drivers must say the share is unknown rather than silently print nothing.
