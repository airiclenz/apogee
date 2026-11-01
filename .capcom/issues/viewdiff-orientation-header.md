---
title: view_diff output does not say which side is current and which is proposed
status: open
type: bug
priority: 2
labels: [bug]
created: 2026-11-01T19:26:29Z
---

view_diff (internal/tools/diff.go, unifiedLineDiff) prints "- " for the file's current content and "+ " for the proposed newContent, with no header naming either side. A model that passes the *before* text as newContent to "show what changed" gets a mirrored diff and nothing in the output tells it so. Reported by Seqcom (request apogee-viewdiff-orientation, runs 20261030T2300Z and 20261031T1754Z, apogee v0.29.0/v0.29.1): misread three times in one run, then re-derived with a Python difflib.

The TUI reads this output back (internal/tui/diffbody.go viewDiffRegions / viewDiffBody), so a header line must be skipped there, not rendered as a context line.

## Why

A headless run that trusts the signs is silently wrong.

## Acceptance

- view_diff output opens with a line naming the orientation (e.g. `--- current` / `+++ proposed`, with the path), above the diff lines
- The tool description states the same orientation in the same words
- The TUI body for a view_diff block renders unchanged apart from the header (no header line rendered as a context row); a driven test pins it
- "No changes detected" and the over-budget diffstat path still read correctly
