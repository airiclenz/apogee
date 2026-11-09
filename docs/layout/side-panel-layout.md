> **Status:** shipped (2026-11-08, `docs/plans/archived/2026-11-08 - 00 - tui-side-panel-plan.md`), from the
> 2026-11-07 grill ([ADR 0110](../../.capcom/knowledge/decisions/adr-0110.md), issue `tui-side-panel`).
> This file describes the panel as built. `layout.md` §"The side panel" holds the rule that binds the
> rest of the frame; the user-facing account is `docs/manual/configuration.md` (the `ui:` section).

# The side panel

The side panel is a column on the right of the transcript slot, 36 columns wide by default and
resized by dragging its separator. It keeps live session state in
view: sub-agents, the task list, files changed this session, and usage. It is a glance. The detail
views (Run view, the task-list card, `/usage`) stay where they are, and a click on a section opens
its detail.

## Where it sits

```
 ❯ refactor the parser                                    │ ◆ Sub-agents 1 running           ⊙
                                                          │ ⠋ repo-scout   running 01:12s  41%
 I'll start by mapping the call sites…                    │ ✓ planner      done       38s  27%
                                                          │ ◆ Tasks 1/3                      ⊙
 ⏺ Read internal/parse/lexer.go                           │ ✓ map call sites
 ⏺ Edit internal/parse/parser.go  +12 −4                  │ ▸ split the tokenizer
                                                          │ ○ port the tests
                                                          │ ◆ Changed files 2                ⊙
                                                          │ …/parse/parser.go           +12 −4
                                                          │ …/parse/lexer_test.go     ↳ +30 −0
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
- **Columns:** `ui.side-panel.width` painted columns, the separator `│` included, measured by the
  width authority (ADR 0030): 36 by default. The painted width is clamped to at least 24 and at most
  the window less the transcript floor (100 under `auto`, 80 under `on`) less the scroll-bar column
  ([adr-0110-amend-1108](../../.capcom/knowledge/decisions/adr-0110/adr-0110-amend-1108.md) (a)).
  The clamp never rewrites the setting, so a window grown back paints the stored width again. The
  transcript's scroll bar stays on the transcript's own right edge, left of the separator.
- **The slot's panes** (`/settings`, approvals, report panes, Run view) paint in the narrowed slot
  while the panel shows. Popups keep their own placement rules.

## When it shows

| `ui.side-panel.show` | Shows when |
|---|---|
| `auto` (default) | the transcript keeps ≥ 100 columns beside it: window ≥ 137 |
| `on` | the transcript keeps ≥ 80 columns beside it: window ≥ 117; never below that floor |
| `off` | never |

The thresholds always count the scroll-bar column, painted or not, so a bar appearing never moves
the panel across a threshold (`layout.md`, "The scroll bar and the column it hangs in"). They are
computed with the default width 36 whatever `ui.side-panel.width` says, so a wider or narrower panel
never moves the window it shows from.

- The column appears or vanishes **only** when the window crosses that row's threshold, or when the
  setting changes (`/panel on|off|auto`, or `/settings`).
- Its width changes **only** on a deliberate act of the user's — a separator drag, a double-click on
  the separator, `/panel width`, or a `/settings` edit of `ui.side-panel.width` — and, within the
  clamp, on a terminal resize. Nothing a section shows or hides moves the column.
- With no section to show, the column stays and paints one dim line, `nothing live`, at its top.
- Crossing a threshold re-wraps the transcript once, like any deliberate width change. A separator
  drag re-wraps the panel and the transcript live, on every column the separator moves.

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
| `sub-agents` | any run is queued or running, and for 30 s after the last one ends | phase glyph, Delegation name, phase word, elapsed, context fill |
| `tasks` | the list has an unfinished task | status glyph and task text; the first unfinished task marked `▸` |
| `changed-files` | the session (any depth) has written a file | path trimmed from the left, `↳` when a sub-agent wrote it, `+N −M` from Edit regions where present |
| `usage` | context fill ≥ 50% | fill bar with `used/window`; `sys · tools · hist` split (history = estimate − standing rows); `in · out` tokens; cache hit %; spend |

The linger (30 s) and the usage threshold (50%) are this spec's numbers to tune; the rules are ADR
0110's.

**Sub-agent rows.** The phase glyph is the status line's spinner frame while the run is running
(`ui.spinner`, so it turns with the status line), `○` while it is queued, the glyph set's done mark
(`✓`, `√` under `ui.glyphs: safe`) once it has finished, and its failure mark (`✗`, `x` under `safe`) when it failed.
The phase word is `queued`, `running`, `done` or `failed`. Elapsed time is the status line's clock
form (`38s`, `01:12s`), blank while queued; the fill is the run's latest reading against its own
window, blank before it has one. Phase, elapsed and fill sit in fixed-width columns at the right
edge, so the facts of every row line up.

**Task rows.** A finished task is the done mark, dim; the current task (the first unfinished one) is
`▸` in the current-task style; any other open task is `○`. The task list stores only done / not
done, so there is no separate in-progress state.

**Changed-file rows.** One row per path, in first-write order; several writes to one path merge
into its row. A path inside the workspace is shown relative to it, one outside it absolute, then
trimmed from the left a whole component at a time behind `…/`. Only apogee's file tools count —
the ones that name the file they write, a deletion included — and a failed write is not counted. A
file changed through the `terminal` tool is not shown (ADR 0110 decision 13).

**Header row:** `◆ <Title>` with a summary wherever the section has one — `Sub-agents 1 running`
(`2 running · 1 queued`, `3 queued`, or `3 done` once all have ended), `Tasks 2/5`,
`Changed files 4` (the file count), `Usage 62%` — and the pin glyph `⊙` (auto) or `●` (pinned) at
the right edge.

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
- While a modal pane is open — an approval, an ask, `/settings`, the sessions browser, a picker or
  the workflows view — a click on a body row or a pin glyph is claimed and does nothing, and the
  hover there shows the default arrow. The grip stays live under them.
- While watching a session, a click on a pin glyph writes nothing: it flashes the watch refusal, and
  the hover there shows the default arrow.
- A left-button press on the **grip** — the separator column or the gutter column right of it, on
  any row above the session-name rule — starts a resize drag. The separator keeps its grab offset
  from the pointer and moves with it, clamped to the limits under "Where it sits"; the panel and the
  transcript re-wrap on every column it moves, and the separator is drawn in the accent tone, bold,
  while the drag holds it. The release writes `ui.side-panel.width` to the global config once, and
  only when the width differs from the one at the press; a failed save keeps the width for the
  session and says `not saved`. A window narrowed until the column hides ends the drag with no write.
- A **double-click** on the grip (two presses within 500 ms, the first moving nothing) resets the
  width to the default 36 and starts no drag.
- The hover shape over the grip is the default arrow: there is no resize cursor (ADR 0106 admits
  only `default`, `text` and `pointer`). A drag starts nowhere else, and the grip is inert while
  watching a session.
- A drag-select that starts in the transcript stops at the separator; one that starts in the panel
  away from the grip selects nothing in v1.

## Run view

While Run view shows a run, `tasks`, `changed-files` and `usage` show that run's state, and the
usage fill reads exactly what the status-line gauge reads. `sub-agents` keeps listing every run, and
the viewed one is marked with `▸` before its name, both in the current-task style. `esc` back to the
conversation gives the panel back to the top level.

## Commands

- `/panel on|off|auto` sets `ui.side-panel.show`.
- `/panel pin|unpin|hide <section>` sets one section's mode (`unpin` → `auto`).
- `/panel width <n>` sets `ui.side-panel.width` to `<n>` columns (24 or more; a window too narrow
  for it paints the clamped width without changing the setting), and `/panel width reset` returns it
  to the default 36.
- Bare `/panel` reports the mode and every section's mode
  (`side panel: auto — sub-agents auto · tasks pinned · …`), adding
  `(not drawn: the window is narrower than N columns)` when the mode would draw the column but the
  window is too narrow. Every form ends with that report.
- Each write is saved to the global config as a pin-glyph click or a separator drag is; a failed save keeps the change
  for the session and says `not saved`. The report answers mid-run, the writing forms wait for idle,
  and the command is refused while watching a session.
- No key binding in v1 (ADR 0110 decision 8), for the panel or for its width.

## Replay

Watch mode folds the Event journal, so it paints the live session's panel. `/clear` empties the
panel. A resumed session has
no journal and folds its stored record instead: the task list, changed files and usage come back,
and the sub-agents section starts empty. The visibility rule is the viewer's own window and
setting.
