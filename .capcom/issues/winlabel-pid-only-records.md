---
title: Decide whether PID-only legacy winlabel journal records count as not-alive
status: done
type: task
priority: 3
aliases: [apogee-winlabel-pid-only-records]
created: 2026-09-24T18:17:15Z
closed: 2026-09-30T16:30:58Z
reason: "Owner decision 2026-09-30: keep PID-only liveness for started == 0 records. Only journals written before Record.Started existed (or a failed creation-time read) hit it; the cost is a recycled PID keeping a dead run's labels (leftover label, not an escape), while treating them as not-alive could clear labels under a still-running confined run. SECURITY.md already documents the behaviour."
---

From handoff 2026-09-23 - 00 (refocus-follow-through). internal/platform/winlabel/walk_windows.go: a journal record written before creation times were journalled (started == 0) is judged alive by PID alone, so a recycled PID can keep a dead run's labels. SECURITY.md (lines ~54-57) documents this behaviour as shipped. Owner decision: keep it, or treat started == 0 as not-alive (change the code and revert the SECURITY.md sentence).
