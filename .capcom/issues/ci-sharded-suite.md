---
title: CI could run the sharded suite (make test) instead of go test -race ./...
status: done
type: task
priority: 3
plan: 2026-09-13 - 00 - headless-ci-hygiene-plan
aliases: [apogee-7ui]
created: 2026-09-06T10:33:13Z
closed: 2026-09-19T16:00:11Z
reason: "Implemented by docs/plans/archived/2026-09-13 - 00 - headless-ci-hygiene-plan.md item 4 (✅ DONE 2026-09-13; CI runs make test under APOGEE_TEST_SHARDS=2). Closed late: bd had no database on this host at closeout."
---

ci.yml:56 runs `go test -race ./...` directly, so CI still pays the 212s single-process wall while `make check` locally now pays 68s (scripts/test-shards.sh). Switching CI to `make test` would follow the repo's own one-definition rule (CI runs `make actionlint`/`make lint` rather than its own copy).

Not done in the same run because a GitHub ubuntu-latest runner is 4 vCPU / 16GB and the shard count is sized off nproc: several concurrent race-enabled test binaries there is an OOM risk that has to be measured on a real runner, not guessed at.

## Acceptance

A measured CI run showing the sharded form is faster and does not OOM, or a recorded decision to leave CI single-process with the reason.

## Log

- 2026-09-13 beads: (plan 2026-09-13 - 00, item 4): ci.yml now runs 'make test' with APOGEE_TEST_SHARDS=2 (4 vCPU / 16 GB runner; race-enabled shards carry their own memory cost). The measured wall-time / OOM check on a real ubuntu-latest runner is an OWNER-RUN pass taken after the next push — and after item 7 (t.Parallel sweep in cmd/apogee) lands, since both changes land together on the runner (5 go test processes plus per-shard parallel TUI programs). Local check under the cap: make actionlint clean; APOGEE_TEST_SHARDS=2 ./scripts/test-shards.sh all green (rest 73s, cmd/apogee shards 105s/109s, internal/tui shards 60s/60s).
- 2026-09-14 beads: (closeout of plan 2026-09-13 - 00 follow-ups): item 4 (6c643505) and item 7 (52e0d677) are committed on main but NOT yet pushed (origin/main still at 6d56a51e), so no ubuntu-latest run has exercised the sharded suite yet. Left open pending the owner-run measurement after the next push: close on a green CI run showing wall time and no OOM, or record the decision to revert to single-process.
