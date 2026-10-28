---
title: One Adoption answer re-reads the layered config up to five times
status: open
type: task
priority: 2
deps: [pin-launch-protocol-copied]
plan: 2026-10-27 - 01 - project-pin-admit-and-adoption-snapshot-plan
created: 2026-10-27T20:58:58Z
---

The ADR 0096 §4 held check lives in cmd/apogee (projectRulesHeld, adoptHeldRule) and each answer runs several full layered loads (held check, settleAllowRules, reconnectMCP, reloadReactions, then the adoptionWatch reload). Proposals ride inside config.Options, so cloneOptions had to clone them (3ff0d39b). The semantics are tested only in the heavy cmd/apogee package. Architecture review 2026-10-27 item 6.

## Why

Every Driver re-implements the answer path and the composition root holds domain rules.

## Acceptance

- A module in internal/project produces an immutable Adoption snapshot and an Answer call that does the held check, writes the record and returns the next snapshot
- The TUI/settings answer path and apogee project adopt go through Answer; the doors take the loaded set and run only for kinds the snapshot diff moved
- config.Options carries no proposal collections; held-check semantics are tested in internal/project
