---
title: "`go vet` diagnostics report a timeout as a code problem under a fixed 30 s deadline"
status: done
type: bug
priority: 2
labels: [slow-box]
plan: 2026-11-03 - 00 - code-audit-fixes-plan
created: 2026-11-03T14:20:29Z
closed: 2026-11-03T16:07:40Z
reason: Resolved by plan '2026-11-03 - 00 - code-audit-fixes-plan' (archived in c3346711)
---

From docs/reviews/code-audit-2026-11-03.md; fixed by item 11 of `docs/plans/2026-11-03 - 00 - code-audit-fixes-plan.md`. `runGoVet` (internal/tools/diagnostics.go:55, :284-318) never checks `res.TimedOut` or `res.DrainWedged`; a killed vet (exit -1, empty output) is reported as "go vet reported problems (exit code -1)" plus a claim the package was checked.

## Why

On a slow box with a cold build cache vet routinely exceeds 30 s; the model is told clean code has problems and may "fix" it.

## Acceptance

- A timed-out or wedged vet reports "go vet skipped: timed out after N s" with no "checked" line.
- The vet budget is sized for slow hardware (300 s).
