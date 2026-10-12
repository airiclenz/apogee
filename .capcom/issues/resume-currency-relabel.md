---
title: Resumed session is relabelled with the configured currency on its next save
status: done
type: bug
priority: 3
plan: 2026-10-03 - 00 - table-stakes-and-image-input-plan
aliases: [apogee-resume-currency-relabel]
created: 2026-10-03T18:24:38Z
closed: 2026-10-03T23:28:54Z
reason: "Resolved by refocus-followups-plan (docs/plans/archived/2026-10-03 - 01 - refocus-followups-plan.md, db1550b0) items 9-10: resumed session keeps its own currency label on save and in the TUI"
---

Deferred from plan 'docs/plans/archived/2026-10-03 - 00 - table-stakes-and-image-input-plan.md' item 4. cmd/apogee/wire_session.go:158 writes the configured currency: label onto Meta.Currency on every Save, so a resumed session whose record carries a different Meta.Currency is relabelled with the configured currency on its next TUI save, against ADR 0093 decision 6. No plan item covered resume-time currency handling.
