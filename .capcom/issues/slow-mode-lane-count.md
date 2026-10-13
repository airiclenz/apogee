---
title: Slow-mode make test runs 3 lanes; measure whether 4 is safe and faster on an 8 GB box
status: done
type: task
priority: 4
created: 2026-10-12T12:28:48Z
closed: 2026-10-13T19:47:14Z
reason: "Measured on the 8 GB Pi 5: 4 lanes 398s vs 3 lanes 475s, min MemAvailable 3245 vs 3342 MB, green. Slow mode now runs 4 lanes at >= 7 GiB RAM, 3 below (scripts/test-shards.sh, building.md, Makefile)."
---

`APOGEE_TEST_SLOW=1` runs 3 lanes at `-parallel 1` (`scripts/test-shards.sh`). The gate at `405e5654` peaked at 733 MB per process with free RAM never below 3089 MB on an 8 GB box, so a fourth lane may fit.

**Work:** run `APOGEE_TEST_SLOW=1 GOMEMLIMIT=2GiB APOGEE_TEST_LANES=4 make test` on an 8 GB machine, record wall time and minimum free RAM, and change the slow-mode default only if both improve safely. Update `docs/manual/building.md` if the default moves.

Source: follow-ups of the archived handoff `docs/handoffs/archived/2026-10-11 - 01 - make-check-slowness-and-oom.md` (local only).
