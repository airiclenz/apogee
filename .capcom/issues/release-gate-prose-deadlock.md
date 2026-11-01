---
title: "A prose-only push leaves the release gate blocked: ci.yml never runs on it"
status: done
type: bug
priority: 2
plan: 2026-11-01 - 00 - tui-row-detail-gauge-and-notify-plan
created: 2026-11-01T10:43:53Z
closed: 2026-11-01T14:08:29Z
reason: Item 8 resolved by plan 2026-11-01 - 00 - tui-row-detail-gauge-and-notify-plan (archived at 639c3bfc)
---

The release gate (cmd/release/gate.go `peelBumps`) peels only trailing CHANGELOG.md/VERSION commits, then needs a green ci.yml run for the gate commit. But ci.yml `on.push.paths` skips a push that touches only prose (`.capcom/**`, `docs/plans/**`, …). Such a commit gets no run, so the gate stays blocked until a code commit lands on top. Seen 2026-11-01: HEAD 6385f944 touches only `.capcom/issues/` and has no ci.yml run.

## Acceptance

- The gate peels a trailing commit whose every path ci.yml filters out, and clears on the green run below it.
- A take-back path (`.capcom/knowledge/decisions/adr-0098.md`, `graphics/demo/stage/**`) is not peeled.
- A drift test fails when ci.yml `on.push.paths` and the gate copy disagree.
- docs/manual/building.md, the `status` help and ADR 0098 state the widened rule.
