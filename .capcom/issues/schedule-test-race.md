---
title: Data race between schedule_test.go:1387 and schedule.go:206 seen once in CI (2026-09-11)
status: done
type: bug
priority: 2
aliases: [apogee-ku1]
created: 2026-09-15T17:26:56Z
closed: 2026-09-16T17:38:11Z
reason: "test(cmd/apogee): guard that no parallel test swaps a package-level seam — race fixed at 52e0d677, guard pins the shape"
---

CI run 34590415624 (main, 2026-09-11) reported WARNING: DATA RACE — write at cmd/apogee/schedule_test.go:1387 by the test goroutine, previous read at cmd/apogee/schedule.go:206 (via internal/schedule/schedule.go:498/468) by a firing goroutine — and ten tests in cmd/apogee failed in cascade. Seen once; not in the five later failing runs (those were load: fixed 2026-09-15 by dropping the wall-clock ceiling in internal/doctext, redacting the live star in t17-run-view, bounding the rest process's -parallel in scripts/test-shards.sh, and un-deadlocking TestCheckLeaksBlamesOnlyTheLeakingSubtest under -parallel 1). schedule.go was touched again on 2026-09-15 (6d413e8e), so re-check the lines before chasing: a package-level seam the test swaps while a scheduled firing may still read it is the shape to look for.
