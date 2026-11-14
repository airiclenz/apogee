---
title: Replace the v1.0.0–v1.8.0 range retraction with the burned-version list
status: done
type: feature
priority: 2
plan: 2026-11-05 - 00 - v1-placeholder-retraction-plan
created: 2026-11-14T10:11:11Z
closed: 2026-11-14T11:01:40Z
reason: Resolved by plan '2026-11-05 - 00 - v1-placeholder-retraction-plan' (archived in aa2ad554)
---

go.mod retracts the whole range [v1.0.0, v1.8.0] because of accidental v1.x tags, so v1.0.0 cannot be released. Replace the range with the exact burned versions owned by internal/update, have `cmd/release publish` place a self-retracting placeholder tag, check the Go module proxy in `smoke`, prove the scheme offline, record ADR 0107 and write a "Cutting v1.0.0" runbook. Designed in docs/plans/2026-11-05 - 00 - v1-placeholder-retraction-plan.md; cutting the bridge release and v1.0.0 stays the owner's act.

## Why

The road to v1.0.0 (and `go install …@latest` picking it) waits on it.

## Acceptance

- Every item of the plan is done and verified; go.mod retracts exactly update.RetractedVersions()
