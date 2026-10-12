---
title: CI shards run without a .test-timings cache — cmd/apogee shards unbalanced (185s vs 117s)
status: done
type: task
priority: 3
plan: 2026-09-17 - 02 - ci-flake-causes-plan
aliases: [apogee-2c0]
created: 2026-09-17T10:46:22Z
closed: 2026-09-19T16:00:10Z
reason: "Implemented by docs/plans/archived/2026-09-17 - 02 - ci-flake-causes-plan.md (items ✅ DONE 2026-09-17); CHANGELOG [Unreleased] carries the entries. Closed late: bd had no database on this host at closeout."
---

scripts/test-shards.sh packs by the gitignored .test-timings cache; on a fresh runner it falls back to equal costs so the two cmd/apogee shards ran 185s and 117s in the last green run (35191168918) while the e2e tests in the heavy shard fight 4 other race binaries for 4 vCPUs. Options: commit a seed timings file (e.g. scripts/test-timings.seed, refreshed on demand), or restore it via actions/cache keyed on the run and let each run rebuild it. Speed/flake-margin only — never correctness.

## Acceptance

Last-run shard walls within ~20% of each other on CI.
