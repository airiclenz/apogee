---
title: "A key's layer and source answered config-side (architecture review 2026-10-14 #15)"
status: parked
type: task
priority: 3
created: 2026-11-05T19:56:22Z
---

Found by docs/reviews/archived/architecture-review-2026-10-14.html (#15), re-verified 2026-11-05 at f7ec72b6. Which layer states a key, which source wins and whether a project may state it are still answered half in cmd/apogee (settingsHost.layeredOpts and landed in wire_options.go, settingSource and savableToProject in settingsrows.go) and half in the TUI (settingsSourceOf and settingsOutranksNote in internal/tui/settings.go), beside internal/config's own Options.SourceOf, overrideSources and projectWritableKey. The review proposes that internal/config answer per key — may a project state it, which layer states it, which source wins, overrides included — with the settings host and pane only reading, so precedence is spelled once and tested in config. No item of docs/plans/archived/2026-10-14 - 01 - architecture-review-defects-plan.md touched it; it fits ADR 0096 D7 and changes no live setter (not ADR 0083 §2).

## Acceptance

- Decided: pursued as a plan or closed as not worth it.
