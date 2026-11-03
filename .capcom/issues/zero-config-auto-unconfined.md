---
title: A zero `domain.Config` runs Auto unconfined though `ConfineToWorkspace` is documented as defaulting to true
status: done
type: bug
priority: 2
labels: [security, driver-parity]
plan: 2026-11-03 - 00 - code-audit-fixes-plan
created: 2026-11-03T14:20:29Z
closed: 2026-11-03T16:07:40Z
reason: Resolved by plan '2026-11-03 - 00 - code-audit-fixes-plan' (archived in c3346711)
---

From docs/reviews/code-audit-2026-11-03.md; fixed by item 19 of `docs/plans/2026-11-03 - 00 - code-audit-fixes-plan.md`. `Config.ConfineToWorkspace` (internal/domain/config.go:157-163, read at internal/agent/construct.go:116, branch at internal/agent/dispatch.go:1154) is a plain bool whose zero value is false, which dispatch treats as the unfenced Auto row. TUI, daemon and probe set it explicitly; headless, the bench or a library embedder that omits it gets fail-open Auto.

## Why

Unlike the Floor guard fields, the zero value is the unsafe one, against the North star that the engine serves every Driver.

## Acceptance

- `domain.Config` takes `Unconfined bool` (zero confines); the host maps `confine-to-workspace:` by negation; Engine/Agent `ConfineToWorkspace()` keep their names.
- Every Driver's Config literal is checked; CHANGELOG names the API change for embedders (apogee-sim sets `Unconfined: true` at its next bump).
