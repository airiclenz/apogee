---
title: Typing in /settings should filter the settings list to matches; groups stay when one of their rows matches
status: done
type: feature
priority: 2
plan: 2026-10-14 - 00 - list-filter-contract-plan
created: 2026-10-13T11:41:49Z
closed: 2026-10-14T10:55:47Z
reason: "Resolved by plan 2026-10-14 - 00 - list-filter-contract-plan (archived at b94203cd): /settings type-to-filter over key path and section heading, headings kept while a row matches, filter display, zero-match row, legends and paste."
---

Feature idea: when /settings is open, typing should filter the list of settings down to the matches. Category groups stay visible while at least one setting in the group matches the filter.

Today the /settings pane is a full-height modal list of every config key (internal/tui/settings.go, rendered through renderPopup; rows come from the key registry across the [SettingsHost.Rows] seam). Typing any printable key currently does nothing until ⏎ opens the row's edit buffer. The idea: keystrokes while the pane is open act as an incremental filter over the row keys (and possibly their section headers), hiding non-matching rows while keeping each group header visible if at least one of its rows matches — the standard "type to filter" interaction.

Open design questions for planning:

- Filter target: key name only, or the description line too?
- How the filter state is shown and cleared (a filter prompt line in the pane, esc to clear the filter, backspace on an empty buffer, …), and how it coexists with ⏎-to-edit and backspace-unsets.
- Whether typing filters directly (keystrokes go to the filter rather than the edit buffer) or via a dedicated filter row.
- How /settings row-count visibility and the pane's frame budget interact with a filtered subset.

Acceptance: with /settings open, typing a string narrows the list to matching settings; a category header stays on screen while at least one of its settings matches; clearing the filter restores the full list.
