---
title: Slow-mode make test runs 3 lanes; measure whether 4 is safe and faster on an 8 GB box
status: open
type: task
priority: 4
created: 2026-10-12T12:28:48Z
---

`APOGEE_TEST_SLOW=1` runs 3 lanes at `-parallel 1` (`scripts/test-shards.sh`). The gate at `405e5654` peaked at 733 MB per process with free RAM never below 3089 MB on an 8 GB box, so a fourth lane may fit.

**Work:** run `APOGEE_TEST_SLOW=1 GOMEMLIMIT=2GiB APOGEE_TEST_LANES=4 make test` on an 8 GB machine, record wall time and minimum free RAM, and change the slow-mode default only if both improve safely. Update `docs/manual/building.md` if the default moves.

Source: follow-ups of the archived handoff `docs/handoffs/archived/2026-10-11 - 01 - make-check-slowness-and-oom.md` (local only).
