# Tool layout

This file is the canonical tool-block layout spec (grill session 2026-08-10),
**implemented 2026-08-11** by `docs/plans/2026-08-10 - 04 - tool-display-overhaul-plan.md`.
`layout.md` keeps the global grammar — widths, colors, path shortening, body
quoting — and its tool sections point here.

**As implemented**, four things below read differently on screen. Each was
settled at implementation time and argued in that plan's item notes: labels are
Title Case (`Diff Preview`, `Find Files`, `Git Status`, `Ask User`, `Sub-Agent`)
rather than sentence case; no stat carries a duration, because no result exposes
one and design call 14 rules out growing the engine for presentation;
`ask_user`'s right slot keeps the human's own answer rather than
`answered`/`pending`; and `git_diff_range`'s target keeps git's three-dot
`base...head`, which is the diff the tool actually takes.

## Rules

- clicking anywhere in the tool body must expand/collapse it. A press+release
  without movement is a click; any drag stays text selection + copy. The
  deepest element under the pointer wins: a child row toggles the child, the
  umbrella header acts on the umbrella.
- grouped tool calls should display the group-count
- expanded tools should be printed with a brighter gray than collapsed
  (existing roles: `toolDetail` / `toolDetailBright`)
- Sub-agent calls group with **each other** (`✦ Sub-Agent (N)`, one row per
  agent: name left, stat right; expanding a member opens its span) but never
  join a super-group — a sub-agent block or group breaks the run.
- dotted lines like `⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯` must be painted in a damped color
  (new `tool-leader` scheme role, dark + light values)
- **time order is always kept.** Calls are never reordered to merge same-type
  calls; a super-group gets one row per *consecutive run* of the same type
  (`read_file, terminal, read_file` = three rows).

## Vocabulary

- **group** — 2+ consecutive calls of the same tool type.
- **super-group** — 2+ adjacent runs of different tool types (a lone call
  counts as a run of 1). Breakers are today's group breakers: any non-tool
  entry between calls (narration, note, approval, error), and any sub-agent
  block. The umbrella header reads `✦ Tools (N calls)` — N = total calls,
  painted in the faint count tone. It forms **live**, the moment a second
  different-label run starts; the running call is its last row, wearing the
  spinner star.
- `<tool-header>` — short human label of one call ("Read", "Terminal", …).
- `<tool-type-header>` — the same label for a run of same-type calls; plurality is carried by `(<group-count>)`.
- `<tool-details>` — the one-line collapsed summary of one call. Usually the key argument (path, pattern, command). It may differ from the expanded rows: a sub-agent shows its *name* here but its task/result in the rows.
- `<tool-details-row-1..n>` — the expanded content of one call (diff, output, listing, …).
- `<tool-top-level-details>` — the right-aligned outcome slot. It carries the
  **whole summary**, whatever kind: a typed stat ("12 lines", "exit 0 · 1.2s",
  "+8 −3"), a promoted one-line output (quoted), or a red `error: …` /
  `denied` / `cancelled`. On a type row it aggregates the run (below).

## Width and overflow

- The dotted filler flexes first, down to a floor of **1** `⋯`.
- Then the **left** `<tool-details>` truncates with `…`.
- The right slot always prints whole — with one guard: a one-line output is
  **promoted** into it only if the row keeps ≥ ~15 cells of target + 1 dot;
  otherwise it stays a body line and the right slot shows the typed stat
  (e.g. `1 line`).

## Fold states and interaction

- Exactly **two states** per call: collapsed (capped preview + count, as
  today) and expanded (the whole body) with a `see less…` footer as an extra
  collapse target. No third stage; scrollback handles long bodies.
- The umbrella's floor is its type rows — it never folds to one line.
  Clicking the umbrella header **closes all open children**.
- Failure marking: the red right-slot summary only; no glyph or header color
  changes. Failures propagate upward as red `N errors` on type rows.
- Keyboard: a **modal block cursor**. `alt+up`/`alt+down` enters transcript
  navigation and moves a highlight across the same toggle targets the mouse
  has (deepest visible level); inside the mode plain `up`/`down` move,
  `enter` toggles, `esc` or any printable key returns focus to the prompt.
- Type-row aggregate (`<tool-top-level-details>` on a run): any failed member
  → red `N errors`; else the sum where the tool's stat sums naturally (lines,
  `+A −R`, hits · files, entries, changes); else blank — dots run to the `▶`.

# General tool formatting

```text
available space / width: ||||||||||||||||||||||||||||||||||||||||||||||||||||||
```

## Single tool collapsed

```text
✦ <tool-header>
  ┕ <tool-details> ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ <tool-top-level-details> ▶
```

## Single tool expanded

```text
✦ <tool-header>
  ┕ <tool-details-row-1>                                                      ▼
    <tool-details-row-2>
    <...>
    <tool-details-row-n>
                                                                      see less…
```

## Grouped tools collapsed (same type)

```text
✦ <tool-type-header> (<group-count>)
  ┝ <tool-details> ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ <tool-top-level-details> ▶
  ┝ <tool-details> ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ <tool-top-level-details> ▶
  ┕ <tool-details> ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ <tool-top-level-details> ▶
```

## Grouped tools partly expanded (same type)

```text
✦ <tool-type-header> (<group-count>)
  ┝ <tool-details> ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ <tool-top-level-details> ▶
  ┝ <tool-details-row-1>                                                      ▼
  │ <tool-details-row-2>
  │ <...>
  │ <tool-details-row-n>
  │                                                                   see less…
  ┕ <tool-details> ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ <tool-top-level-details> ▶
```

## Grouped tools collapsed (different types / super-group)

One row per consecutive same-type run, in time order. A row's
`<tool-top-level-details>` aggregates its run.

```text
✦ Tools (N calls)
  ┝ <tool-type-header> (<group-count>) ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ <tool-top-level-details> ▶
  ┝ <tool-type-header> (<group-count>) ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ <tool-top-level-details> ▶
  ┕ <tool-type-header> (<group-count>) ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ <tool-top-level-details> ▶
```

## Grouped tools expanded 1st step (different types / super-group)

```text
✦ Tools (N calls)
  ┝ <tool-type-header> (<group-count>) ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ <tool-top-level-details> ▶
  ┝ <tool-type-header> (<group-count>) ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ <tool-top-level-details> ▼
  │ ┝ <tool-details> ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ <tool-top-level-details> ▶
  │ ┝ <tool-details> ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ <tool-top-level-details> ▶
  │ ┕ <tool-details> ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ <tool-top-level-details> ▶
  ┕ <tool-type-header> (<group-count>) ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ <tool-top-level-details> ▶
```

## Grouped tools expanded 2nd step (different types / super-group)

```text
✦ Tools (N calls)
  ┝ <tool-type-header> (<group-count>) ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ <tool-top-level-details> ▶
  ┝ <tool-type-header> (<group-count>) ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ <tool-top-level-details> ▼
  │ ┝ <tool-details> ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ <tool-top-level-details> ▶
  │ ┝ <tool-details-row-1>                                                    ▼
  │ │ <tool-details-row-2>
  │ │ <...>
  │ │ <tool-details-row-n>
  │ │                                                                 see less…
  │ ┕ <tool-details> ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ <tool-top-level-details> ▶
  ┕ <tool-type-header> (<group-count>) ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ <tool-top-level-details> ▶
```



# Display details per tool

Ratified 2026-08-10. Paths render workspace-relative. `—` = nothing to show
there: an empty right slot means dots run to the `▶`; a row with nothing to
expand carries no indicator at all.

| Tool | `<tool-header>` | `<tool-details>` (collapsed) | `<tool-top-level-details>` | `<tool-details-row-*>` (expanded) |
|---|---|---|---|---|
| read_file | Read | path (`:12–80` when ranged) | `N lines` | the returned content |
| open_file | Open | path (`· locate "…"` when set) | `N lines` | content + located line numbers |
| write_file | Write | path | `N lines` | the written content |
| edit_existing_file | Edit | path | `+A −R` | diff of the change |
| single_find_and_replace | Replace | path | `+A −R` | diff of the change |
| multi_find_and_replace | Replace | path | `N changes` | diff of the changes |
| copy_file | Copy | source `→` destination | — | full paths |
| move_file | Move | source `→` destination | — | full paths |
| delete_file | Delete | path | — | — |
| list_dir | List | path (`· recursive` when set) | `N entries` | the listing |
| find_files | Find files | pattern | `N files` | matched paths |
| grep | Grep | pattern (`· include` glob) | `N hits · M files` | `path:line` matches + context |
| terminal | Terminal | the command line | `exit 0 · 1.2s` | command + output |
| python_exec | Python | first code line | `exit 0 · 0.4s` | code + output |
| run_tests | Tests | path (`· filter` when set) | `PASS/FAIL · 3.1s` | runner summary + failing tests |
| git_status | Git status | — | `N changed` | changed-file list |
| git_log | Git log | ref | `N commits` | one line per commit |
| git_branch | Git branch | action + branch name | — | command output |
| git_commit | Git commit | message subject | short hash | full message + committed files |
| git_diff_range | Git diff | `base..head` | `+A −R` | stat list / diff |
| view_diff | Diff preview | path | `+A −R` | the diff |
| diagnostics | Diagnostics | path | `N issues` / `clean` | one line per issue |
| http_request | HTTP | METHOD + URL | `status · size` | response headers + body head |
| web_fetch | Fetch | URL | size | extracted text head |
| web_search | Search | the query | `N results` | result titles + URLs |
| present_document | Present | document title (path fallback) | — | path + title |
| ask_user | Ask user | the question | `answered` / `pending` | question + choices + the answer |
| sub_agent | Sub-agent | its name (task head fallback) | `N steps · done/failed` | task text + result summary |

Notes:
- ask_user renders as the live prompt while pending; this table describes its
  transcript form after the answer.
- sub_agent deliberately shows *different* data collapsed (its name) vs
  expanded (task + result). Its `N steps` stat is wired only if the engine
  already exposes a step count; otherwise the slot shows `done`/`failed`
  alone.
- Expansion state lives on the transcript entry, so it survives scrolling and
  new messages arriving. New blocks start collapsed.
