---
title: ADR 0052 context
---

`IDEAS.md` carried "better UI for diffs": the expanded body of an edit block is a flat `-`/`+`
list today, and the owner — red-green weak — reads its green additions against red removals as
near-neighbours. The sketch (`docs/layout/siff-layout.md`, retired into
`docs/layout/split-diff-layout.md`) asked for a two-pane before/after reading with line numbers,
falling back to a vertical list where the terminal is narrow.

Two facts about the codebase bound the design. First, an edit block's body is **derived from the
call's arguments alone** (`changedLines`, `internal/tui/diffbody.go`): no file is read, no
token is spent — and therefore no line numbers and no unchanged context exist anywhere in the
view. Second, of the five diff-bodied blocks (`edit_existing_file`, `single_find_and_replace`,
`multi_find_and_replace`, `view_diff`, `git_diff_range`), only the last two carry position data a
renderer could recover: `view_diff`'s body is a whole-file diff countable from line 1, and
`git_diff_range` emits git's `@@` headers. The three edit tools return prose only.

The owner resolved the open questions on 2026-08-19 by question round; this record ratifies
them. It supersedes nothing, but it does **revise one documented stance**: `changedLines`'s
"derived from the arguments and goes nowhere near the wire" stays true of the *fallback*
rendering, while the primary rendering now consumes data the tool records.
