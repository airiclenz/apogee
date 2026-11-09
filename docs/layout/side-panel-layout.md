> **Status:** shipped (2026-11-08, `docs/plans/archived/2026-11-08 - 00 - tui-side-panel-plan.md`), from the
> 2026-11-07 grill ([ADR 0110](../../.capcom/knowledge/decisions/adr-0110.md), issue `tui-side-panel`).
> Keyboard navigation joined it 2026-11-09 ([ADR 0111](../../.capcom/knowledge/decisions/adr-0111.md),
> which supersedes ADR 0110 decisions 3 and 8), and the `mcp` section with it (ADR 0111 decision 10). This file describes the panel as built. `layout.md` §"The side panel" holds the rule that binds the
> rest of the frame; the user-facing account is `docs/manual/configuration.md` (the `ui:` section).

# The side panel

The side panel is a column on the right of the transcript slot, 44 columns wide by default and
resized by dragging its separator. It keeps live session state in
view: sub-agents, the task list, files changed this session, usage, and the MCP servers. It is a
glance. The detail views (Run view, the task-list card, `/usage`, `/mcp`) stay where they are, and a click on a section opens
its detail — as does `⏎` on it in navigation mode, the keyboard's way into the column.

## Where it sits

```
▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄ ││  ○ Sub-agents (1) ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ ▼
❯ refactor the parser                                ││    ⣾ repo-scout      running 01:12s  41%
▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀ ││    ✓ planner         done       38s  27%
✦ I'll start by mapping the call sites…              ││
                                                     ││  ○ Tasks (1/3) ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ ▼
✦ Tools (1 Read, 1 Edit) ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ ▼ ┃│    ✓ map call sites
  ┝ Read ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ line one ▶ ┃│    ▸ split the tokenizer
  ┕ Edit ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ +12 −4 ▶ ┃│    ○ port the tests
                                                     ┃│
✦ Sub-agents (2)                                     ┃│  ○ Changed files (2) ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ ▼
  ┝ repo-scout ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ 0 tool calls · 13k/31k ▶ ┃│      internal/parse/parser.go     +12 −4
  ┕ planner ✓ ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ done ▶ ┃│    ↳ internal/parse/lexer_test.go +30 −0
                                                     ┃│
✦ The scout maps the tests while                     ┃│  ○ Usage (62%) ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ ▼
  I split the tokenizer into its                     ┃│      20k/32k 62% ██████▎
  own file.                                          ┃│      prompt 4k · completion 300
                                                      │      cached 25%
▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔ refactor the parser ▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔
  ⣾ responding · 0s [total 0s]                                            20k/32k 62% ██████▎
╭────────────────────────────────────────────────────────────────────────────────────────────────╮
│ queue a message…  ⏎ queue · ↑ recall · esc×2 cancel                                            │
╰────────────────────────────────────────────────────────────────────────────────────────────────╯
  test-host ✦ test-model                                                            ◐ ask before
▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁
```

*(Drawn from a frame rendered at 150 columns while a Turn runs, the column at its default 44. The
column's 44 cells are copied as painted; the transcript and the full-width rows below the rule are
narrowed to fit, and on screen the transcript keeps at least 100 columns. Left of the separator
stand the transcript's scroll-bar track `│` and thumb `┃`.)*

- **Rows:** every row above the session-name rule, the same rows the transcript slot spans. The
  session-name rule, status line, prompt box and footer keep the full window width.
- **Columns:** `ui.side-panel.width` painted columns, the separator `│` included, measured by the
  width authority (ADR 0030): 44 by default. The painted width is clamped to at least 24 and at most
  the window less the transcript floor (100 under `auto`, 80 under `on`) less the scroll-bar column
  ([adr-0110-amend-1108](../../.capcom/knowledge/decisions/adr-0110/adr-0110-amend-1108.md) (a)).
  The clamp never rewrites the setting, so a window grown back paints the stored width again. The
  transcript's scroll bar stays on the transcript's own right edge, left of the separator. Inside
  the column come the separator, then a blank left gutter as wide as the transcript's body
  indent (two cells), so the content starts as far right of the separator as transcript text does
  of its marker column; then the content; then a right margin of the same two cells, so a header's
  fold indicator and a row's right-aligned facts sit in the column of the status line's last cell.
  Where the content would be narrower than a full sub-agent row — its body indent, its lead cell
  and the space after it, the one-cell gap and its facts ("Rows and spacing") — the chrome yields a
  cell at a time before any fact is cut: the right margin first, then the gutter's second cell, then
  the body indent, down to nothing; the gutter's first cell, where the cursor's `▌` stands, never
  yields, and past that the sub-agent's name is clipped to make room. The minimum width 24 paints a
  full sub-agent row's facts whole under every spinner style.
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
computed with a fixed gate width of 36 whatever `ui.side-panel.width` says, so a wider or narrower
panel never moves the window it shows from. The gate width is not the default width: between 137
and 145 columns under `auto` the default-width panel docks clamped narrower than 44.

- The column appears or vanishes **only** when the window crosses that row's threshold, or when the
  setting changes (`/panel on|off|auto`, or `/settings`).
- Its width changes **only** on a deliberate act of the user's — a separator drag, a double-click on
  the separator, `/panel width`, or a `/settings` edit of `ui.side-panel.width` — and, within the
  clamp, on a terminal resize. Nothing a section shows or hides moves the column.
- With no section to show, the column stays and paints one dim line, `nothing live`, at its top.
- Crossing a threshold re-wraps the transcript once, like any deliberate width change. A separator
  drag re-wraps the panel and the transcript live, on every column the separator moves.

### Below the threshold: the overlay

On a window under its row's threshold, with `ui.side-panel.show` not `off`, the column is not
docked, but it can still be opened as an **overlay** (ADR 0111 D7): `→` in navigation mode
("Keyboard"), or bare `/panel` ("Commands"), paints it over the transcript's right edge, on every row
above the session-name rule, with the cursor on one of its stops.

- **Width:** `ui.side-panel.width` clamped to the window alone — the overlay keeps no transcript
  floor beside it. The overlay never writes the setting, and it has no grip: its separator and the
  gutter beside it are inert, so it cannot be resized or reset by a double-click.
- **A blank column beside it.** One blank column stands between the transcript's text and the
  overlay's separator, so the text never runs into the `│`. It hangs left of the overlay where the
  window has a column to spare, and comes out of the overlay's own width where the overlay is as
  wide as the window. It is not a column cell: a click there is a click outside, and closes the
  overlay.
- **The transcript is not re-wrapped.** The slot, the viewport and every wrapped line stay exactly
  what they were; the overlay only paints over the rows' right-hand cells.
- **It lives exactly while navigation mode stands in the panel.** `←` closes it (the cursor goes back
  to the transcript row it left, or the mode ends where the transcript has no stop), and so do `esc`,
  a `⏎` that opens a detail, and anything else that ends the mode. A click outside it closes it too:
  the mode ends and the click goes on to whatever it landed on — the transcript, the prompt, a pane.
- **Inside, it is the docked column.** A click acts as it does on the docked panel ("Pointer"): a pin
  glyph pins or unpins its section and the overlay stays, a body row opens its detail. The keys are
  navigation mode's, as in "Keyboard".
- While an approval or an `ask_user` question owns the keys the mode is suspended, so the overlay is
  not painted; it comes back on the same stop once the decision is taken.
- Under `off`, or with nothing in the column to stand on, there is no overlay: `→` does nothing and
  bare `/panel` only reports.

## Sections

Each section is one entry in the ordered `ui.side-panel.sections` list (shaped like
`ui.footer.rows`), in a mode:

| Mode | Shows |
|---|---|
| `pinned` | always |
| `auto` | while its live rule holds |
| `hidden` | never |

Default list, all `auto`: `sub-agents`, `tasks`, `changed-files`, `usage`, `mcp`. A list that leaves a
section out gets it appended in `auto`, so a list saved before `mcp` existed shows it last.

| Section | Live while (auto) | Row content |
|---|---|---|
| `sub-agents` | any run is queued or running, and for 30 s after the last one ends | phase glyph, Delegation name, phase word, elapsed, context fill |
| `tasks` | the list has an unfinished task | status glyph and task text; the first unfinished task marked `▸` |
| `changed-files` | the session (any depth) has written a file | `↳` (`»` under `ui.glyphs: safe`) in the lead when a sub-agent wrote it (blank otherwise), path trimmed from the left, `+N −M` from Edit regions where present, right-aligned in the `tool-marker` role |
| `usage` | context fill ≥ 50% | the fill as the status-line gauge reads it, `used/window N% bar` (`~` before an estimate, the percent clamped at 100), the bar in the gauge colour on no background — a column too narrow for the whole bar keeps the figures alone; `sys · tools · hist` split (history = estimate − standing rows); `prompt · completion` tokens; `cached N%` (the cached share of the prompt); `cost`. The words are `/usage`'s, and as there a zero count is left out rather than printed as `0`, a row with every count zero not drawn |
| `mcp` | any MCP server is `dialling`, `failed` or `disconnected` | the name, then `state · N tools` right-aligned, one row per server in first-reported order |

The linger (30 s) and the usage threshold (50%) are this spec's numbers to tune; the rules are ADR
0110's.

**Rows and spacing.** One blank spacer row precedes every section header but the first, as one
blank line parts the transcript's blocks; the overflow drops the spacers first ("Overflow"). A
header's pin glyph stands on the content's first cell. Every body row of every section starts
`bodyIndent` (two cells) right of it, then a **lead cell** as wide as the panel-wide lead width —
the widest of the active spinner style's frames and the one-cell marks a lead can hold (`○`, `◷`,
`✓`, `✗`, `▸`, `↳`): two cells under `snake` and `glitter`, one under `classic` and
`ui.glyphs: safe` — holding the row's status glyph, `↳`, or nothing, padded on the right, then one
blank cell, then the row's text. So the text of a sub-agent, a task, a changed file, a usage row and
an MCP server starts in one column across the column. There are no `┝`/`┕` branches. The indent
and lead come out of a row's room before its text is clipped or its path trimmed, never out of its
facts. On a column too narrow for a full sub-agent row the body indent yields ("Where it sits",
Columns); with no indent cell left, Run view's `▸` stands after the lead instead ("Run view").

**Sub-agent rows.** The phase glyph is the status line's spinner frame while the run is running
(`ui.spinner`, so it turns with the status line, in the same `ui.spinner-color` loop as a
foreground on no background), `○` while it is queued, the glyph set's done mark
(`✓`, `√` under `ui.glyphs: safe`) once it has finished, and its failure mark (`✗`, `x` under `safe`) when it failed.
While a queued or running run has an approval requested and not yet decided — the one the approval
pane shows or one queued behind it — its row reads `waiting` under the waiting mark `◷` (`?` under
`safe`) in the `warning` foreground with no background, in place of its spinner or `○`; the phase
word `waiting` stays faint, as `queued` and `running` do, and only the gated run's own row is
marked (ADR 0111 decision 11). The gate closes on that run's decision, on the Approver's failure on it, or when the run or
the Turn finishes, so a done or failed row never reads `waiting`.
The phase word is `queued`, `running`, `waiting`, `done` or `failed`, in the transcript's outcome
tones (`adr-0111-amend-1109c`): `done` in the `success` role as a finished delegation's outcome
slot paints it, `failed` in the error tone like its `✗` and the transcript's failure, the
unfinished phases faint like the elapsed time and fill after them. Elapsed time is the status line's clock
form (`38s`, `01:12s`) below an hour, then hours and minutes (`1h02m` to `23h59m`), then days and
hours (`1d02h`), clamped at `99d23h` — never wider than six columns, where the status line's own
`01:02:03s` would push the row's facts out of line — and blank while queued; the fill is the run's latest reading against its own
window, blank before it has one. The glyph sits in the panel-wide lead cell ("Rows and spacing"),
the narrower marks padded on the right, so every row's name starts in one column. Phase, elapsed
and fill sit in fixed-width columns at the right edge, so the facts of every row line up.

**MCP rows.** The section reads the process's MCP server state — the fold of every
`MCPServerEvent` (ADR 0111 decision 8): emitted by the composition root as each server's dial starts
and ends and as a live session is closed on purpose or lost on its own, and seeded at startup — not the viewed run's,
since no run owns a server. A row is written behind a blank lead as the server's name, then
`state · N tools` (`1 tool`) right-aligned at the content's edge like every other section's facts,
the name clipped first so the state and the count stay in view; the state is faint, `failed` in the
error tone — the tone `/mcp` paints `failed` in too. The count is left off while no connection serves, and kept while a dial is under way
with the previous connection's tools in hand or a failed reconnect's previous connection still
serves. A server removed from the configuration has no row; one a `url-safety` edit closed, or whose
session ended on its own (reason `lost`), reads `disconnected`, with no count. Server names are untrusted text, escape-stripped at the fold and again at paint.

**Glyphs.** Every mark the panel paints comes from the theme's glyph set, so `ui.glyphs: safe`
swaps them as it swaps the transcript's: `●` → `•`, `▸` → `>`, `↳` → `»`, `▶` → `►`, `✓` → `√`,
`✗` → `x`, `◷` → `?`, the `⋯` leader → `·`, and the running spinner turns ASCII; `○` is the same in
both sets. Only the shapes the manual's `ui.glyphs` list names keep their form here: the open
fold's `▼`, the `│` separator, navigation mode's `▌` cursor and the usage gauge's block cells.

**Task rows.** A finished task is the done mark, dim; the current task (the first unfinished one) is
`▸` (`>` under `ui.glyphs: safe`) in the current-task style; any other open task is `○`. The task list stores only done / not
done, so there is no separate in-progress state.

**Changed-file rows.** One row per path, in first-write order; several writes to one path merge
into its row. A path inside the workspace is shown relative to it, one outside it absolute, then
trimmed from the left a whole component at a time behind `…/`. Only apogee's file tools count —
the ones that name the file they write, a deletion included — and a failed write is not counted. A
file changed through the `terminal` tool is not shown (ADR 0110 decision 13).

**Header row:** `<pin> <Title> (<summary>) ⋯ ▼`. The pin glyph leads it, on the content's first
cell — `○` while the section is `auto`, `●` (`•` under `ui.glyphs: safe`) while it is `pinned` — and a dotted leader in the
transcript's `tool-leader` role runs from the title to the fold indicator at the content's right
edge, in the `tool-indicator` role: `▶` (`►` under `ui.glyphs: safe`) while the section has body
rows the column is not showing — the human folded it, or the overflow squeezed it — and `▼`
otherwise, so a section with no body rows never reads `▶`. A folded header carries no hidden-row
count. It is the transcript's header shape (`tool-layout.md`, "Fold
states and interaction"). The folded sections are `ui.side-panel.folded`, read on every frame. The
title is painted as the transcript paints a block's label, bold in the `tool-header` role; the
summary after it is in the `tool-indicator` role's muted tone, not bold; the pin glyph keeps its
faint tone and is never bold. The summary, in parentheses, appears wherever the section has one:
- `Sub-agents (2)` — the running count stands bare; `(2 · 1 queued)` while some are queued too;
  `(1 queued)` while only queued runs exist; `(3 done)` once nothing is active, a failed run
  counted there and never named, since its row carries `✗`; no parentheses with no runs. A waiting
  run counts as the running or queued run it is, so a section folded to its header shows no wait.
- `Tasks (2/5)`, `Changed files (4)` (the file count), `Usage (62%)`, `MCP (1 failed)` (the servers not connected,
  by state in the order dialling, failed, disconnected — `2 dialling · 1 failed` — and no summary
  while every one is connected).

## Overflow

The spacers between sections ("Rows and spacing") go first, all together: they are painted only
while the overflow below squeezes no section and every one of them fits in the rows the bodies leave
over, so a spacer never costs a section its body. A section the human folded is not squeezed, so it
leaves the spacers standing.

When the shown sections need more rows than the slot has:

1. Pinned sections take rows first, then the **opened tier**, then auto sections, each in list
   order. A section the human folded costs its header row alone and takes no body rows, however
   many are spare: the overflow never opens it.
2. A section that does not get its full height folds to its header row alone, which carries the
   summary (`○ Tasks (2/5)`, `○ Sub-agents (2)`) and the `▶` fold indicator.
3. A click on a squeezed section's header (or `⏎` on its title stop) opens it into the opened
   tier: it takes its rows ahead of the auto sections, which share what is left in list order, so
   the last auto section in list order is squeezed instead. Nothing is written to config for it. A
   click on an opened section folds it, writes `ui.side-panel.folded` and drops it from the tier;
   unfolding a section the human folded also puts it in the tier, so the overflow cannot squeeze it
   straight back and one click always shows a change. The tier lasts until apogee exits: a
   restarted session starts without it (ADR 0110 decision 11, `adr-0110-amend-1109`).
4. A section that would get no row at all is still drawn as its header row; when not even every
   header fits, the last ones in list order are left out, and the bottom row reads `+N more`, in
   the transcript's `tool-marker` role.
5. Nothing scrolls inside the panel in v1. The mouse wheel over the panel does nothing.

## Pointer

- A click on a section's **body** opens its detail view:
  - a sub-agent row → that run's Run view;
  - a task row → the task-list card, unfolded and scrolled to;
  - a changed-file row → that write's tool block, unfolded and scrolled to;
  - usage → `/usage`;
  - an mcp row → `/mcp`.
- A click on the **pin glyph** — the header's lead cell — toggles `pinned` ↔ `auto` and writes the
  list back to the setting.
- A click anywhere else on a **header** — its title, summary, leader or fold indicator — folds the
  section to its header row, or opens a folded one, and writes `ui.side-panel.folded` back to the
  global config silently, as a task-list card's fold writes `ui.task-list-open`. A section only the
  overflow squeezed opens into the opened tier instead and writes nothing ("Overflow", rule 3). A
  failed write says `side-panel.folded: not saved: …` in the transcript and the fold stands for the
  session. The hover
  there is the pointer hand. The keyboard reaches every one of these targets — body, pin and fold —
  through navigation mode (Keyboard, below).
- While a modal pane is open — an approval, an ask, `/settings`, the sessions browser, a picker or
  the workflows view — a click on a body row, a pin glyph or a header is claimed and does nothing,
  and the hover there shows the default arrow. The grip stays live under them.
- While watching a session, a click on a pin glyph writes nothing: it flashes the watch refusal, and
  the hover there shows the default arrow. A fold is the viewer's own: it toggles and writes while
  watching too.
- A left-button press on the **grip** — the separator column or the one column right of it, on
  any row above the session-name rule — starts a resize drag. The grip is those two columns however
  the gutter yields, so a press on a header's pin glyph is never one. The separator keeps its grab offset
  from the pointer and moves with it, clamped to the limits under "Where it sits"; the panel and the
  transcript re-wrap on every column it moves, and the separator is drawn in the accent tone, bold,
  while the drag holds it. The release writes `ui.side-panel.width` to the global config once, and
  only when the width differs from the one at the press; a failed save keeps the width for the
  session and says `not saved`. A window narrowed until the column hides ends the drag with no write.
- A **double-click** on the grip (two presses within 500 ms, the first moving nothing) resets the
  width to the default 44 and starts no drag.
- The hover shape over the grip is the default arrow: there is no resize cursor (ADR 0106 admits
  only `default`, `text` and `pointer`). A drag starts nowhere else, and the grip is inert while
  watching a session.
- A drag-select that starts in the transcript stops at the separator; one that starts in the panel
  away from the grip selects nothing in v1.

## Keyboard

The panel is the second region of **navigation mode** (ADR 0111), the modal cursor that also walks
the transcript (`docs/layout/tool-layout.md`, "Keyboard"). Its keys are the same in both regions.

- **Stops** are the click targets of "Pointer", in paint order: every section header, as two stops —
  its pin glyph's cell first, then its title — each sub-agent row and each changed-file row
  ([adr-0111-amend-1109b](../../.capcom/knowledge/decisions/adr-0111/adr-0111-amend-1109b.md)); adjacent
  rows that share one target are one stop, so the tasks body
  is one stop (the task-list card), the usage rows are one (`/usage`) and the mcp rows are one
(`/mcp`). The `+N more` row, a spacer and a body
  row with nothing to open are none.
- **Entering.** `⌥↑`/`⌥↓` start the mode in the transcript, or in the panel when the transcript has
  no stop — `⌥↑` at the last stop, `⌥↓` at the first. Inside the mode `→` moves the cursor from the
  transcript into the panel, onto the stop it stood on there before or, on the first visit, the stop
  nearest the transcript row it leaves; `←` takes it back to the transcript row it left. On a window
  too narrow to dock the column, `→` opens it as the overlay ("Below the threshold: the overlay"),
  and `←` closes it. `→` into a column `off` hides or with no stop, and `←` from the docked column
  towards a transcript with no stop, do nothing, and both keys are claimed even then, so the
  prompt's caret never moves under a reader who is navigating. `⌥↑`/`⌥↓` never open the overlay.
- **Moving.** `↑`/`↓` walk the column's stops, clamped at either end, never wrapping.
- **`⏎` is the click** on the stop the cursor stands on, through the click's own path, so the rules
  under "Pointer" hold for it unchanged: under a modal pane it does nothing, and on a pin while
  watching a session it flashes the watch refusal. On a header's pin stop it toggles `pinned` ↔
  `auto`; on its title stop it folds the section, or opens a folded one, and writes
  `ui.side-panel.folded`, as the header click does. Either way the cursor stays on the stop it acted
  on; a stop that opens a detail — a run's Run view, the task-list card, a
  changed file's tool block, `/usage`, `/mcp` — ends the mode, the keys going back to the prompt.
- **`^x`** on a sub-agent row stops that run (ADR 0086 D5), with no confirmation, and leaves the
  cursor where it was so the row can say how the run ended; while watching it flashes the watch
  refusal. On any other stop `^x` is not the mode's.
- **Leaving.** `esc` leaves the mode from either region, and so does a printable key, which still
  lands in the prompt. Inside a Run view, `esc` with the cursor in the panel leaves the mode and
  keeps the view open; with the cursor on the view's transcript it goes one level up, as before.
- **Identity.** The column re-lays out under every Event, so the cursor holds its stop by identity —
  a header's pin or title by its section, a sub-agent row by its run, a changed-file row by its path, the task card,
  the usage report, the mcp report — rather than by row. A stop that has gone falls back to the stop of the same
  section nearest the row it was on, then to the column's first stop; a column left with no stop
  hands the cursor to the transcript, or ends the mode where the transcript has none either.
- **The mark.** The cursor's row wears a `▌` in the gutter's first cell, right of the separator, plus the
  selection tone across its content, so a terminal that paints no colour still shows it (ADR 0111
  D6, ADR 0052 D4); the separator is untouched, so the grip stays put. A stop of several rows is
  marked on its first. A header's two stops share its row, so the tone tells them apart: on the pin
  stop it covers the pin glyph's cell alone, on the title stop the rest of the row past the pin. Only the region the cursor stands in is painted: the transcript's remembered
  row is not while the cursor is in the panel, and the panel's is not while it is in the transcript.
- **Decisions come first.** While an approval or an `ask_user` question owns `↑`/`↓`/`⏎`, the mode is
  suspended, mark and keys together, and comes back on the same stop once the decision is taken.

## Run view

While Run view shows a run, `tasks`, `changed-files` and `usage` show that run's state, and the
usage fill reads exactly what the status-line gauge reads. `sub-agents` keeps listing every run, and
the viewed one is marked with `▸` in the first cell of its row's body indent, so the mark costs no
width and moves no column (on a column so narrow the indent has yielded, it stands after the lead),
and its name, both in the current-task style; `mcp` keeps
showing every server, which belongs to the process rather than to a run. `esc` back to the
conversation gives the panel back to the top level.

## Commands

- `/panel on|off|auto` sets `ui.side-panel.show`.
- `/panel pin|unpin|hide <section>` sets one section's mode (`unpin` → `auto`).
- `/panel width <n>` sets `ui.side-panel.width` to `<n>` columns (24 or more; a window too narrow
  for it paints the clamped width without changing the setting), and `/panel width reset` returns it
  to the default 44.
- Bare `/panel` reports the mode and every section's mode
  (`side panel: auto — sub-agents auto · tasks pinned · …`), adding
  `(not docked: the window is narrower than N columns)` when the mode would draw the column but the
  window is too narrow to dock it. Every form ends with that report. On such a window bare `/panel`
  also opens the column as the overlay, in navigation mode with the cursor on its first stop
  ("Below the threshold: the overlay") — unless the column has no stop, when it only reports.
- Each write is saved to the global config as a pin-glyph click or a separator drag is; a failed save keeps the change
  for the session and says `not saved`. The report answers mid-run, the writing forms wait for idle,
  and the command is refused while watching a session.
- No key shows or hides the panel or sets its width: the keyboard reaches the panel's stops through
  navigation mode ("Keyboard"; ADR 0111 supersedes ADR 0110 decision 8), and the width changes by a
  drag or `/panel width`.

## Replay

Watch mode folds the Event journal, so it paints the live session's panel — its `mcp` section from
the journal's `mcp_server` lines, which open with the watched process's state at the journal's birth.
`/clear` empties the panel but for `mcp`: the servers stay connected across it, so their rows stay.
A resumed session has
no journal and folds its stored record instead: the task list, changed files and usage come back,
the sub-agents section starts empty, and `mcp` keeps this process's own servers. The visibility rule is the viewer's own window and
setting.
