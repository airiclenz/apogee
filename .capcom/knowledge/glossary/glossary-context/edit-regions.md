---
title: "Edit regions: the typed summary of changed regions an edit tool records at apply time"
---

**Edit regions**:
The typed summary an edit tool records **at apply time**: each changed region's before/after
start lines, its removed and inserted lines, and up to three unchanged lines of context each side —
neighbouring changes stay separate regions whose context tiles the lines between them (they merged
until the ADR 0052 amendment of 2026-08-22) — counted from the applied change itself, so a view
never re-reads the file or
re-derives positions from arguments. It rides the **Tool summary** contract unchanged — display
data, never sent to the model, and no wire form for the summary value outside the eventjson mirror
that live **Watch mode** decodes (amended 2026-10-17, ADR 0103) — and the region facts are
the one part of a summary the neutral codec in `internal/session` mirrors onto a wire type of
its own, so a resumed session renders the same split diffs
(ADR 0052 §5;
a facts-only fold that drops the verdicts beside them is a standing denial,
ADR 0083 §1).
A result carrying none renders the argument-derived list exactly as before, which is what keeps
tools an open extension point (ADR 0002).
The Split diff is its consumer. See
ADR 0052.
_Avoid_: "hunks" (a hunk is a unit of the *patch input format* `edit_existing_file` accepts;
a region is a unit of the *recorded outcome*), "edit summary" (every Tool summary summarizes;
this one is the regions).
