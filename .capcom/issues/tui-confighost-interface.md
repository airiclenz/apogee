---
title: "TUI: ConfigHost interface for the config-file callbacks on tui.Options (review 2026-09-14 speculative)"
status: done
type: task
priority: 4
aliases: [apogee-970]
created: 2026-09-15T06:08:10Z
closed: 2026-09-16T22:25:17Z
reason: "refactor(tui): ConfigHost groups the eight config-file acts on Options (c78faf06)"
---

Deferred from docs/reviews/architecture-review-2026-09-14.html speculative list on 2026-09-15: tui.Options carries 12 bare func fields; 8–9 are one concept (the config file) and fit a ConfigHost on ADR 0054's own pattern ('a FAMILY of acts … one named interface, not as N bare funcs'). GenerateTitle, ReportActivity, ReloadSkills are NOT config-file acts and stay bare. Win is small; do when tui.Options is next reshaped.
