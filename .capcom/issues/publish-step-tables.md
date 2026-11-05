---
title: "A publish step is one row (architecture review 2026-10-14 #11)"
status: parked
type: task
priority: 3
created: 2026-11-05T19:56:22Z
---

Found by docs/reviews/archived/architecture-review-2026-10-14.html (#11), re-verified 2026-11-05 at f7ec72b6. cmd/release still keeps two tables joined by name: publishSteps (probes.go, read by status) and publishActions (publish.go), with publishRun.probe looking up each action's probe by slices.IndexFunc on the name, so a new channel is edits in two files. The review proposes one publishStep row carrying name, probe, describe, converge and doneDetail, with status reading only the probe, so status and publish agree by construction. Its symptom (an unmatched name panicking on index -1, untested) was guarded by item 20 (pin publish steps to its actions) of docs/plans/archived/2026-10-14 - 01 - architecture-review-defects-plan.md; it fits ADR 0098, and the step names stay as they are (a JSON contract with the release skills).

## Acceptance

- Decided: pursued as a plan or closed as not worth it.
