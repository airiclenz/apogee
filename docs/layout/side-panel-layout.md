> **Status:** design of record from the 2026-11-07 grill ([ADR 0110](../../.capcom/knowledge/decisions/adr-0110.md),
> issue `tui-side-panel`). **Not yet implemented.** The plan that builds it amends this file where the
> two differ. `layout.md` §"The side panel" holds the rule that binds the rest of the frame.

# The side panel

The side panel is a fixed column on the right of the transcript slot. It keeps live session state in
view: sub-agents, the task list, files changed this session, and usage. It is a glance. The detail
views (Run view, the task-list card, `/usage`) stay where they are, and a click on a section opens
its detail.

## Where it sits

```
 ❯ refactor the parser                                    │ ◆ Sub-agents                     ⊙
                                                          │ ⠋ repo-scout   explore  1m12  41%
 I'll start by mapping the call sites…                    │ ✔ planner      done       38s
                                                          │ ◆ Tasks 2/5                      ⊙
 ⏺ Read internal/parse/lexer.go                           │ ✔ map call sites
 ⏺ Edit internal/parse/parser.go  +12 −4                  │ ▸ split the tokenizer
                                                          │ ○ port the tests
                                                          │ ◆ Changed files                  ⊙
                                                          │ …/parse/parser.go         +12 −4
                                                          │ …/parse/lexer_test.go   ↳  +30 −0
                                                         ▐│ ◆ Usage 62%                      ⊙
 ── session name ────────────────────────────────────────────────────────────────────────────────
 ⠋ thinking…                                                                     ctx 62% ▕████▍▏
 ╭─ prompt ─────────────────────────────────────────────────────────────────────────────────────╮
 │                                                                                              │
 ╰──────────────────────────────────────────────────────────────────────────────────────────────╯
  footer
```

*(The sketch is narrowed to fit; on screen the transcript keeps at least 100 columns.)*

- **Rows:** every row above the session-name rule, the same rows the transcript slot spans. The
  session-name rule, status line, prompt box and footer keep the full window width.
- **Columns:** 36 painted columns, the separator `│` included, measured by the width authority (ADR
  0030). The transcript's scroll bar stays on the transcript's own right edge, left of the
  separator.
- **The slot's panes** (`/settings`, approvals, report panes, Run view) paint in the narrowed slot
  while the panel shows. Popups keep their own placement rules.

## When it shows

| `ui.side-panel.show` | Shows when |
|---|---|
| `auto` (default) | the transcript keeps ≥ 100 columns beside it: window ≥ 137 |
| `on` | the transcript keeps ≥ 80 columns beside it: window ≥ 117; never below that floor |
| `off` | never |

The thresholds always count the scroll-bar column, painted or not, so a bar appearing never moves
the panel across a threshold (`layout.md`, "The scroll bar and the column it hangs in").

- The column appears or vanishes **only** when the window crosses that row's threshold, or when the
  setting changes (`/panel on|off|auto`, or `/settings`). Nothing inside the panel moves the column.
- With no section to show, the column stays and paints one dim line, `nothing live`, at its top.
- Crossing a threshold re-wraps the transcript once, like any deliberate width change.

## Sections

Each section is one entry in the ordered `ui.side-panel.sections` list (shaped like
`ui.footer.rows`), in a mode:

| Mode | Shows |
|---|---|
| `pinned` | always |
| `auto` | while its live rule holds |
| `hidden` | never |

Default list, all `auto`: `sub-agents`, `tasks`, `changed-files`, `usage`.

| Section | Live while (auto) | Row content |
|---|---|---|
| `sub-agents` | any run is running, and for 30 s after the last one ends | spinner or end glyph, Delegation name, phase, elapsed, context fill |
| `tasks` | the list has an unfinished task | status glyph and task text; the first unfinished task marked `▸` |
| `changed-files` | the session (any depth) has written a file | path trimmed from the left, `+N −M` from Edit regions where present, `↳` when a sub-agent wrote it |
| `usage` | context fill ≥ 50% | fill bar; system / tools / history split (history = estimate − standing rows); tokens in/out; cache hit %; spend |

The linger (30 s) and the usage threshold (50%) are this spec's numbers to tune; the rules are ADR
0110's.

**Header row:** `◆ <Title>` with a summary where it has one (`Tasks 2/5`, `Usage 62%`), and the pin
glyph `⊙` (unpinned) or `●` (pinned) at the right edge.

## Overflow

When the shown sections need more rows than the slot has:

1. Pinned sections take rows first, then auto sections, both in list order.
2. A section that does not get its full height folds to its header row alone, which carries the
   summary (`◆ Tasks 2/5`, `◆ Sub-agents 2 running`).
3. A section that would get no row at all is still drawn as its header row; when not even every
   header fits, the last ones in list order are left out, and the bottom row reads `+N more`.
4. Nothing scrolls inside the panel in v1. The mouse wheel over the panel does nothing.

## Pointer

- A click on a section's **body** opens its detail view:
  - a sub-agent row → that run's Run view;
  - a task row → the task-list card, unfolded and scrolled to;
  - a changed-file row → that write's tool block, unfolded and scrolled to;
  - usage → `/usage`.
- A click on the **pin glyph** toggles `pinned` ↔ `auto` and writes the list back to the setting.
- A click on the header title does nothing. The panel never takes keyboard focus in v1.
- A drag-select that starts in the transcript stops at the separator; one that starts in the panel
  selects nothing in v1.

## Run view

While Run view shows a run, `tasks`, `changed-files` and `usage` show that run's state, matching
the status-line gauge. `sub-agents` keeps listing every run, and the viewed one is marked.

## Commands

- `/panel on|off|auto` sets `ui.side-panel.show`.
- `/panel pin|unpin|hide <section>` sets one section's mode (`unpin` → `auto`).
- No key binding in v1 (ADR 0110 decision 8).

## Replay

Watch mode folds the Event journal, so it paints the live session's panel. A resumed session has
no journal and folds its stored record instead: the task list, changed files and usage come back,
and the sub-agents section starts empty. The visibility rule is the viewer's own window and
setting.
