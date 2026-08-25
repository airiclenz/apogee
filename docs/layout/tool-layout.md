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

The **Grouped Sub-agents** section below landed separately, later the same day,
by `docs/plans/2026-08-11 - 01 - grouped-sub-agent-display-plan.md`, and two
things there read differently on screen, each argued in that plan's item notes.
The prompt opens the span but stands **below** the head's own report rows where
that report was long enough to lay out as a body, because the head's rows are
painted before the span begins. And the done `✓` takes a new green `success`
scheme role, which the sketches below leave uncoloured.

The `┊` rule holds as written: the closer is drawn only where another grouped
sub-agent follows the expanded one. It is still emitted by the seam that joins
two blocks rather than by the group painter — the seam is the only place that
can put a row after an open member's span — but that seam is told whether the
list resumes, so a group's last expanded member, a **lone** expanded
delegation, and a delegation still streaming at the foot of the transcript all
end on the ordinary separator instead.

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
  **Remainder:** while a lone call is collapsed the slot also carries the count
  of the body behind it, after the same middle dot the stats use —
  `exit 0 · +3 more lines` — so the block is its header and one row and the
  count never spends a row of its own. It is the first thing the row gives up
  when the width will not seat it (below), and an open block drops it, having
  nothing left to count. A grouped member and a type row never carry one: a
  member's body is reached by opening the member.
  **Colour:** the `tool-marker` role while the block is collapsed and
  `tool-marker-bright` once it is open — the slot is apogee's reading of what
  the call came to, so it speaks in the same voice as the `+N more lines`
  marker rather than in the detail gray of the body it summarises. A failed
  summary is red (`error`) and that red wins; nothing else overrides the role,
  and every kind takes it, promoted and quoted ones included.

## Width and overflow

- The `+N more lines` remainder goes first: it joins the slot only while the row
  can seat it and still show ~15 cells of target (a shorter target: all of it).
  A row that gave up its path or its command to count what it is not showing
  would be a row about nothing — the same floor the promote-guard holds below.
- The dotted filler flexes next, down to a floor of **1** `⋯`.
- Then the **left** `<tool-details>` truncates with `…`.
- The right slot always prints whole — with one guard: a one-line output is
  **promoted** into it only if the row keeps ≥ ~15 cells of target + 1 dot;
  otherwise it stays a body line and the right slot shows the typed stat
  (e.g. `1 line`).

## Fold states and interaction

- Exactly **two states** per call: collapsed (capped preview + count in the
  outcome slot, as today) and expanded (the whole body) with a `see less…`
  footer as an extra collapse target. No third stage; scrollback handles long
  bodies.
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

where a collapsed call that hides body counts it in that same slot:

```text
✦ Terminal
  ┕ go test ./... ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ exit 0 · +3 more lines ▶
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

## Grouped Sub-agents

- expanded sub-agents carry no `⤷ sub-agent` label.
- the vertical line on the very left of an expanded sub-agent is colored.
- `┊` is only displayed if another grouped sub-agent follows after the expanded sub-agent. The last sub-agent in the group (if expanded) does not show this.
- a RUNNING sub-agent's `<tool-top-level-details>` reads `N tool calls · <used>/<window>` and names no ongoing action. The call in flight is deliberately not spelled there: it changed several times a second while the two cells beside it held still, and each of those calls already has a block of its own inside the run, one click away. The one live word the slot adds is `· delegating`, and only while the most recent call open inside the run is itself a sub-agent — work the child has handed on, which its own blocks cannot show since the nested run is collapsed too. A finished sub-agent's slot is unchanged: its report's first line, or `· done` where the report became a body. A run routed to the Sub-agent server (ADR 0045) closes the slot with the model it ran on — `2 tool calls · 12k/32k · Found 4 gaps · qwen3-4b` — and only when that is not the session's own model; a same-model delegation shows no such cell. `<window>` is the CHILD's own window, so a routed run's fill is spelled against the Sub-agent server's window (`7k/8k`) rather than the session's (`7k/128k`); a run reporting no window of its own is spelled against the session's, which is what an unrouted child inherited. This holds for a lone sub-agent exactly as for a grouped member.
- expanding a sub-agent only ADDS: the open row keeps the very `<tool-top-level-details>` its collapsed row wore — the count, the fill, and the gist or `· done` — and the report, the prompt and the railed span come out beneath it. Opening never takes back what the shut row said, so the two fold states differ in the BODY alone. This holds for a lone sub-agent exactly as for a grouped member, running and finished alike.
- a sub-agent the engine has not started yet is SCHEDULED: the model asked for it and it is queued behind the `parallel-agents` cap, holding no slot. Its `<tool-top-level-details>` says exactly `scheduled` — no tool-call count, no context fill, no gist, none of which exist yet — and its row carries no indicator and no click target, because there is nothing behind it to open. The moment its child starts, the row becomes an ordinary live sub-agent row and expands like any other. A lone sub-agent starts immediately and never shows this.

```text
✦ Sub-Agent (<group-count>)
  ┝ first-sub-agent-collapsed ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ ▶
┌─┶ sub-agent-expanded ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ <tool-top-level-details> ▼
│
│ The initial prompt for the sub agent should be displayed here. If it contains
│ more than one row, it needs to be wrapped. Markdown needs to be properly
│ formatted.
│
│ ✦ Tools (N calls)
│   ┝ <tool-type-header> (<group-count>) ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ <tool-top-level-details> ▶
│   ┕ <tool-type-header> (<group-count>) ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ <tool-top-level-details> ▶
│
│ ✦ Normal sub-agent response...
┊
  ┝ another-collapsed-sub-agent ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ ▶
  ┕ last-grouped-and-collapsed-sub-agent ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ ▶
```

Example for how finished sub agents should be displayed. The first sub-agent is done and receives a `✓` after the sub agent name ("done" is also printed in the <tool-top-level-details>, The second sub agent is still running in this example.

Each member is marked PER MEMBER, the moment that sub-agent finishes — not when the group as a whole is done. A grouped run reports its results in one burst once every member has joined, so the display follows each delegation's own start/finish instead: the first to report wears its `✓` and its `done` while its siblings are still running, and expanding it shows the report it returned right then. A member that finished with a FAILURE wears no `✓` — its red `<tool-top-level-details>` is the whole of the marking.

```text
✦ Sub-Agent (<group-count>)
  ┝ <tool-type-header> ✓ ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ <tool-top-level-details> ▶
  ┕ <tool-type-header> ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ <tool-top-level-details> ▶
```

Example for a fan-out wider than the `parallel-agents` cap: the first sub-agent holds a slot and is working, the second is queued behind it. The queued row states `scheduled` and nothing else, and it wears no `▶` — clicking it does nothing until it starts.

```text
✦ Sub-Agent (<group-count>)
  ┝ <tool-type-header> ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ <tool-top-level-details> ▶
  ┕ <tool-type-header> ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ scheduled
```


# Display details per tool

Ratified 2026-08-10. Paths render workspace-relative. `—` = nothing to show
there: an empty right slot means dots run to the `▶`; a row with nothing to
expand carries no indicator at all.

A path that resolves somewhere other than where it reads — a symlinked
component — keeps the argument in its column and gains
`→ resolves to <where it lands>` after it, the same line the approval pane
draws. It rides the target rather than the body because a targeted block hides
its body whole while collapsed, and it appears only when the two differ.

| Tool | `<tool-header>` | `<tool-details>` (collapsed) | `<tool-top-level-details>` | `<tool-details-row-*>` (expanded) |
|---|---|---|---|---|
| read_file | Read | path (`:12–80` when ranged, `· locate "…"` when set) | `N lines` | the located lines (`Located "…" on lines: …`) when locate is set, else — |
| write_file | Write | path | `N lines` | the written content |
| edit_existing_file | Edit | path | `+A −R` | split/stacked diff, see `split-diff-layout.md` |
| single_find_and_replace | Replace | path | `+A −R` | split/stacked diff, see `split-diff-layout.md` |
| multi_find_and_replace | Replace | path | `+A −R` (`N changes` when the result records no regions) | split/stacked diff, see `split-diff-layout.md` |
| copy_file | Copy | source `→` destination | — | full paths |
| move_file | Move | source `→` destination | — | full paths |
| delete_file | Delete | path | — | — |
| list_dir | List | path (`· recursive` when set) | `N entries` | the listing |
| find_files | Find files | pattern | `N files` | matched paths |
| grep | Grep | pattern (`· include` glob) | `N hits · M files` | `path:line` matches + context |
| terminal | Terminal | the command line | `exit 0 · 1.2s` | command + output |
| python_exec | Python | first code line | `exit 0 · 0.4s` | code + output |
| console_open | Console | the command line | `console N` (`exit N` when the program was already over) | the program's first output |
| console_send | Console Send | `console N` (`· what was typed` when the input is not empty) | `alive` / `exit N` / `killed` | what the program printed back |
| console_read | Console Read | `console N` | `alive` / `exit N` / `killed` | the output since the last read |
| console_close | Console Close | `console N` | `exit N` / `killed` | the unread tail |
| run_tests | Tests | path (`· filter` when set) | `PASS/FAIL · 3.1s` | runner summary + failing tests |
| git_status | Git status | — | `N changed` | changed-file list |
| git_log | Git log | ref | `N commits` | one line per commit |
| git_branch | Git branch | action + branch name | — | command output |
| git_commit | Git commit | message subject | short hash | full message + committed files |
| git_diff_range | Git diff | `base..head` | `+A −R` | split/stacked diff, one header row per file section, see `split-diff-layout.md` |
| view_diff | Diff preview | path | `+A −R` | split/stacked diff, see `split-diff-layout.md` |
| diagnostics | Diagnostics | path | `N issues` / `clean` | one line per issue |
| http_request | HTTP | METHOD + URL | `status · size` | response headers + body head |
| web_fetch | Fetch | URL | size | extracted text head |
| web_search | Search | the query | `N results` | result titles + URLs |
| present_document | Present | document title (path fallback) | — | path + title |
| ask_user | Ask user | the question | `answered` / `pending` | question + choices + the answer |
| sub_agent | Sub-agent | its name (task head fallback) | `scheduled` before it starts, else `N steps · done/failed` | task text + result summary |

Notes:
- **2026-08-19** — the five diff-bodied rows above now render through
  `split-diff-layout.md` (Split diff where the width allows, Stacked diff below
  it), delivered by
  `docs/plans/2026-08-19 - 03 - split-diff-display-plan.md` and ratified by
  ADR 0052. Three things read differently than this table did before:
  `git_diff_range`'s body is parsed and coloured — it was plain, uncoloured tool
  output — and a body the parser cannot read in full falls back to that plain
  rendering; expanded `view_diff` no longer paints the whole file, it
  paints the changed regions with up to 3 context lines each side (ADR 0052 §2,
  deliberate); and `multi_find_and_replace`'s stat moved from `N changes` to
  `+A −R`, because the tool now records Edit regions and the slot reads their
  stat, keeping the argument-derived `N changes` only when a result carries none.
- **2026-08-25** — the four Console rows above arrived with the family itself
  (ADR 0059). They are the one family whose slot is a process's LIVENESS rather
  than an exit alone: `alive` is what a console says while it is still running,
  and none of the three verdicts is an error result — a dev server the model
  asked to close exited exactly as asked. `console_open`'s slot holds the id
  instead, because the command it started is already the row's target and the
  id is nowhere else on the card. The header line and the status line are read
  off the body they were taken from, so no Console card states a fact twice.
- git_commit never promotes its one-line output into the slot at any width: the
  line repeats the subject the row already leads with, so the slot holds the
  short hash above and the line lays out in the body.
- ask_user renders as the live prompt while pending; this table describes its
  transcript form after the answer.
- sub_agent deliberately shows *different* data collapsed (its name) vs
  expanded (task + result). Its `N steps` stat is wired only if the engine
  already exposes a step count; otherwise the slot shows `done`/`failed`
  alone. A delegation queued behind the `parallel-agents` cap shows
  `scheduled` and is not expandable at all (see Grouped Sub-agents).
- Expansion state lives on the transcript entry, so it survives scrolling and
  new messages arriving. New blocks start collapsed.
