# Tool layout

This file is the canonical tool-block layout spec (grill session 2026-08-10),
**implemented 2026-08-11** by `docs/plans/archived/2026-08-10 - 04 - tool-display-overhaul-plan.md`.
`layout.md` keeps the global grammar — widths, colors, path shortening, body
quoting — and its tool sections point here.

**As implemented**, four things read differently from the ratified sketch. Each was
settled at implementation time and argued in that plan's item notes, and the
per-tool table below states the shipped form (corrected 2026-10-02): labels are
Title Case (`Diff Preview`, `Find Files`, `Git Status`, `Ask User`) rather than
sentence case — except the two the side panel also names, which take the panel's
words (2026-11-09): a delegation is `Sub-agent`, a folded list of them
`Sub-agents (N)`, and the task list `Tasks`; no stat carries a duration, because no result exposes
one and design call 14 rules out growing the engine for presentation;
`ask_user`'s right slot keeps the human's own answer rather than
`answered`/`pending`; and `git_diff_range`'s target keeps git's three-dot
`base...head`, which is the diff the tool actually takes.

The **Grouped Sub-agents** section below landed separately, later the same day,
by `docs/plans/archived/2026-08-11 - 01 - grouped-sub-agent-display-plan.md`, and two
things there read differently on screen, each argued in that plan's item notes.
The prompt opens the span but stands **below** the head's own report rows where
that report was long enough to lay out as a body, because the head's rows are
painted before the span begins. And the done `✓` takes a new green `success`
scheme role, which the sketches below leave uncoloured.

**Since 2026-08-30 the delegation has no expanded shape at all**: expanding a
sub-agent opens its **run view**, a surface of its own that takes the whole
transcript slot, and the collapsed row is the only shape a run wears in the
conversation ([ADR 0063](../../.capcom/knowledge/decisions/adr-0063.md),
`layout.md`, "Run view"). The `┊` rule below is stated as it was written — the
closer drawn only where another grouped sub-agent follows an expanded one, and
emitted by the seam that joins two blocks rather than by the group painter — and
no DELEGATION reaches it any more, since nothing opens a span in place. The rule
itself is live rather than the record of a shape the screen no longer has: the
**Grouped Skills** list below opens its members INLINE, and that is where the
closer is drawn today.

## Rules

- clicking anywhere in the tool body must expand/collapse it. A press+release
  without movement is a click; any drag stays text selection + copy. The
  deepest element under the pointer wins: a child row toggles the child, the
  umbrella header acts on the umbrella.
- grouped tool calls should display the group-count
- expanded tools should be printed with a brighter gray than collapsed
  (scheme roles `muted` / `muted-bright`)
- Sub-agent calls group with **each other** (`✦ Sub-agents (N)`, one row per
  agent: name left, stat right; opening a member opens its run view) but never
  join a super-group — a sub-agent block or group breaks the run.
- Skill fetches group with **each other** the same way (`✦ Skill (N)`, one row
  per fetch: the loaded skill left, the outcome slot blank; expanding a member
  opens its body inline) and never join a super-group either — a skill block or
  group breaks the run exactly as a sub-agent one does. That is one rule stated
  twice, not two: a call that groups with its OWN kind never joins the mixed
  super-group.
- dotted lines like `⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯` must be painted in a damped color
  (the `tool-leader` scheme role, dark + light values)
- **time order is always kept.** Calls are never reordered to merge calls of
  one tool type that were not adjacent; a super-group gets one row per
  *consecutive run* (`read_file, terminal, read_file` = three rows).

## Vocabulary

- **run** — the maximal stretch of adjacent calls of one tool type; a lone
  call is a run of 1. A run is never a block of its own: it is one **type row**
  of the super-group below.
- **super-group** — 2+ groupable calls, whether that is one run of a single
  tool type or adjacent runs of different types. Breakers: any non-tool
  entry between calls (narration, note, approval, error), and any block that
  groups with its own kind instead — a sub-agent block or group, a skill block
  or group. The umbrella header is ONE leader row,
  `✦ Read 3 files, ran 4 commands ⋯⋯ ▼`: the star, then the umbrella's
  **Group summary** (below) — what its calls did, in plain words — and a
  dotted leader of at least one dot from it to the fold's `▶`/`▼` at the
  block's right edge, in the type rows' indicator column; open and folded
  differ only in that glyph, so every umbrella — open, folded, running or
  settled — reads the same one line (2026-11-18; until then the header was the
  tool-type breakdown `✦ Tools (3 Read, 4 Terminal)`). It forms **live**, the
  moment the second groupable call is placed; the running call is its last
  row, wearing the spinner star. Only a LONE groupable call stays a standalone
  block — which, settled and untouched under `ui.tools-auto-collapse`, folds
  to the Group summary an umbrella of that one call would wear ("Single tool
  folded (auto-collapsed lone call)" below).
- **Group summary** — the umbrella header's text, built in the TUI from the
  members' views and their Tool summaries (nothing is added to the engine or
  the record), in this order: the **verb phrases**, then the **diffstat**,
  then the **outcome counts**:

  ```text
  ✦ Read 3 files, searched 1 pattern, ran 4 commands, edited 2 files +18 −5 ⋯ ▶
  ✦ Read 3 files, ran 4 commands · 1 failed ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ ▶
  ✦ Reading 2 files, ran 1 command ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ ▼
  ```

  - **Phrases.** Each tool's presenter row names the phrase its calls count
    under, and the summary merges calls by phrase, never by label, so tools
    doing the same kind of work read as one phrase:

    | Phrase (settled / live) | Tools | Counts |
    |---|---|---|
    | read / reading N files | `read_file` | distinct paths |
    | edited / editing N files | `write_file`, `edit_existing_file`, `single_find_and_replace`, `multi_find_and_replace` | distinct paths |
    | listed / listing N dirs | `list_dir` | distinct paths |
    | searched / searching N patterns | `grep`, `find_files` | calls |
    | ran / running N commands | `terminal`, `python_exec` | calls |
    | fetched / fetching N pages | `web_fetch`, `http_request` | calls |
    | copied / moved / deleted N files | `copy_file`, `move_file`, `delete_file` (one phrase each) | calls |
    | made / making N commits | `git_commit` | calls |
    | checked / checking git | `git_status`, `git_log`, `git_show`, `git_branch`, `git_diff_range` | no number |

    A write and an edit both read "edited" — there is no "created" (owner,
    2026-11-18). A **distinct** phrase counts the distinct `path` arguments
    of its calls, so three reads of one file read `1 file`; the others count
    calls. One is `1 file`, never `1 files`. Any other tool — a built-in with
    no phrase, or an MCP tool — counts as `<Label> ×N` (`Diff Preview ×2`,
    `github · search ×1`). The phrases follow the order each one **first
    ran**, joined by `, `, and the first word is capitalised.
  - **Live verb.** A phrase with a call still in flight takes its present
    participle (`Reading 2 files`) until that call's result lands; the `✦`
    blinks as on any live block.
  - **Diffstat.** The `+A −R` summed over the `edited` phrase's members whose
    slot carries a diffstat, painted on the diff colours its edits' bodies use
    (`diff-added` / `diff-removed`). A write's line count, a multi-replace's
    `N changes` and a failed edit's verdict carry none and are skipped; no
    diffed edit, no diffstat.
  - **Outcome.** `· N failed` and `· N denied`, each only when non-zero, in
    the failure red, so a group that went wrong shows it without being
    opened. A call the approver or a Reaction refused counts as **denied**,
    never as failed. All succeeded: no marker.
  - **Width fallback.** Where the whole summary leaves no room for one dot,
    detail goes in a fixed order: the phrases after the first two give way to
    ` and K more` (`Read 3 files, ran 4 commands and 2 more`), then the
    diffstat goes, then the phrases give way to the bare total `Tools (N
    calls)` — `Tools` in the tool label's bold gold, the parenthetical in the
    faint count tone — and last that head is clipped the way a type row's
    label is. The outcome counts survive every step and are the last thing
    the clip takes, so the header is always one line and a failure stays on
    it as long as anything does.
- `<tool-header>` — short human label of one call ("Read", "Terminal", …).
- `<tool-type-header>` — the tool's label on a type row, standing for the whole run; plurality is carried by `(<group-count>)`.
- `<tool-details>` — the one-line collapsed summary of one call. Usually the key argument (path, pattern, command). It may differ from what opening the call shows: a sub-agent shows its *name* here and its task/result inside its run view.
- `<tool-details-row-1..n>` — the expanded content of one call (diff, output, listing, …).
- `<tool-top-level-details>` — the right-aligned outcome slot. It carries the
  **whole summary**, whatever kind: a typed stat ("12 lines", "exit 0",
  "+8 −3"), a promoted one-line output (quoted), or a red verdict — the bare
  word `error`, `denied`, `cancelled`, or the `error: …` a tool that words its
  own short failure carries (`error: exit 3`, the terminal, python_exec and a
  steered delegation). A failed call whose tool words no such verdict says the
  bare word and nothing more; the message it wrote goes to the body, where it
  wraps whole — however long its longest line, that body being the only place
  the message is said at all — instead of eating the target off the row. On a type row the slot
  aggregates the run (below).
  **Failed and empty runs:** a row with a verdict always has a body behind its
  ▶. A call whose tool worded its own failure (`error: exit 1`) but printed
  nothing beneath it opens onto the failure text the model received, the `cwd:`
  line taken off and everything else verbatim — `[exit code 2 — fail-fast: …]`
  for a terminal line that stopped early; a steered delegation with nothing
  after its cause opens onto its whole result. A terminal or python_exec run
  that exited cleanly without printing keeps `exit 0` in the slot and opens onto
  `(no output)`. Standing alone and collapsed, either counts that one line in
  the remainder wording below: `exit 0 · +1 more line`, `error: exit 1 · +1 more
  line`.
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
  marker rather than in the detail gray of the body it summarises. Two
  verdicts override that pair. A failed summary is red (`error`) and that red
  wins over both. The other is a **finished delegation's `done`**, which takes
  the `success` role — the same green its done ✓ wears, so a run marks its
  finishing once in one colour. That exception is anchored on the delegation
  vocabulary alone: a run `capped at its step cap` did not finish and stays in
  the marker tone, as does a run `stopped by you` (the human stopped it with
  `^x`; it did not finish either, and wears no ✓) and a run that `ended without a report` (it reached its
  boundary with nothing to hand back, and wears no ✓), and so do a tool's own readings of its own work (`clean`,
  `PASS`, `exit 0`) and every promoted or quoted line, which carry no verdict
  at all. Nothing else overrides the role, and every other kind takes it.

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
  (e.g. `1 line`). The **failure** slot is the one kind that now always does
  print whole, and always has the room to: an unhooked failure says the bare
  word `error` with its message in the body, so there is no unbounded sentence
  left to reserve the row's whole width before the target is given a budget.

## Fold states and interaction

- Exactly **two states** per call: collapsed and expanded (the whole body)
  with a `see less…` footer as an extra collapse target. Collapsed, a call
  with a target shows no body line at all — its header and its one target
  row, the hidden body counted in the outcome slot — and a call with no
  target shows its first two lines and counts the rest the same way. No third stage; scrollback handles long
  bodies. This holds for the delegation row too, and the **run view** is no
  exception to it: a framed sub-agent has one state, collapsed, and opening it
  opens a different *surface* rather than a third fold stage of the row
  ([ADR 0063](../../.capcom/knowledge/decisions/adr-0063.md)).
  `task_list` is the first of four blocks whose collapsed state is its
  **header alone**: it folds to `✦ Tasks (done/total)` — no task row
  beneath, no `+N more lines`, because the header's own count already says
  what the fold holds — and opens to every row uncapped with no `see less…`
  footer, the header's `▼` being the fold. The `▶`/`▼` sits on the header; a
  click on the card, or `enter` at the block cursor on it, toggles EVERY
  task-list block in the transcript together, and the state is remembered
  across sessions in `ui.task-list-open` (2026-09-14; the table row below says
  so). A card with no task row to fold over — an errored call, a cleared list —
  takes the ordinary targetless shape instead, so a failure is never folded
  away.
- The umbrella is the second block that folds to its **header alone**, and
  EVERY umbrella does (2026-09-18). Every umbrella header wears `▶`/`▼` at
  its leader's edge; a click on it, or `enter` at the block cursor on it, folds
  the umbrella to its Group summary line, `✦ Read 3 files, ran 4 commands ⋯ ▶`
  — no type row beneath, no `+N more lines`, the summary saying what the fold
  holds — or opens it again onto its type rows under a `▼`, painted exactly as
  before. The fold covers the rows: whatever type rows and members a reader
  had opened stay open beneath the header and are there again when it unfolds.
  Which fold holds an umbrella is decided in this order (2026-11-18,
  [`adr-0035-amend-1118`](../../.capcom/knowledge/decisions/adr-0035/adr-0035-amend-1118.md)):
  (1) **Auto-collapse.** With `ui.tools-auto-collapse` on (the default), a
  **settled** umbrella the reader has not **touched** is folded, whatever its
  size and whatever `ui.tools-open` says — a finished group is one row unless
  you opened it. *Settled* is derived at paint and stored nowhere: every call
  in it has returned, AND either a later entry of its own run follows it — the
  model's next narration or reply, a note, the next user message, a further
  block — or nothing follows and its run is over: the Turn, for the
  conversation; the child, inside a sub-agent's run view. An umbrella still
  running, or the last entry of a running Turn, is not settled; a replayed one
  always is, so a resumed session shows every umbrella collapsed. *Touched*
  is in-session view state, never written and never serialized: the reader
  clicked or `enter`ed its header, opened a type row, a member or a member's
  body in it, or jumped to one of its members from the side panel. A click on
  an auto-collapsed umbrella opens **that one alone**, on a fold of its own,
  and writes nothing — not `ui.tools-open`, whatever the umbrella's size —
  and every later click flips that same fold. The same rule reaches three
  more tool blocks — a lone groupable call, a Sub-agent group and a Skill
  group (2026-11-20; the two bullets after the session log below). (2)
  Everything else — a live
  umbrella, a touched one, and every umbrella with `ui.tools-auto-collapse`
  off — folds **by size**, the rule as it stood before: an umbrella is
  **large** when it has more type rows than `ui.tools-fold-over` (default `5`;
  `0` makes no umbrella large) — and nothing else enters that answer, so it is
  the same at every depth, in every view, live or replayed. A large
  umbrella's fold is ONE state shared by every large umbrella in the
  transcript: the gesture on any large header toggles them all, and the state
  is remembered across sessions in `ui.tools-open` (default `false` — a large
  umbrella starts folded; "Grouped tools folded (large umbrella)" below). A
  **small** umbrella folds on its own flag — session-only, never written to
  `config.yaml`, never serialized, so every small umbrella starts a session
  open ("Grouped tools folded (small umbrella)" below); one that grows large
  follows the shared preference from then on, its own flag ignored while it
  is large. A touched umbrella falls back to its size's fold unless the reader
  gave it a fold of its own (the click above), which it then keeps at any
  size. A folded umbrella with a call in flight paints its header exactly as
  an open one with a call in flight does — the blinking `✦`, the live verb,
  the counts climbing — and nothing else on the line. (Supersedes the
  2026-09-17 rule, plan `docs/plans/2026-09-17 - 01`, under which only a
  large umbrella whose Turn had settled folded, and a small one or one with a
  call in flight wore a bare header whose click closed its open children —
  plan `docs/plans/2026-09-18 - 01`. Design call 9, which had the umbrella
  never fold to one line, was superseded then. The 2026-09-18 "size alone"
  rule now governs only live and touched umbrellas — plan
  `docs/plans/2026-11-18 - 00`.)
- The start-up **session log** is the third block that folds to its **header
  alone** (2026-11-12; `layout.md`, "The start-up box"). It is no tool call —
  it gathers the routine notes a session starts with — but it wears the
  `task_list` card's header-alone grammar: collapsed, `✦ Session log (N
  notes)` (`(1 note)` for one) under a `▶`, no note row beneath and no `+N
  more lines`, the count saying what the fold holds; open, the header's `▼` is
  the fold and every note is its own `· <text>` row, uncapped, with no `see
  less…` footer, each note one paragraph — a line break it was worded with
  folds to one space as it joins the log — soft-wrapped and hung at its text
  column:

  ```text
  ✦ Session log (4 notes) ▶
  ```

  ```text
  ✦ Session log (4 notes) ▼
    · <note>
    · <a note too long for one row, soft-wrapped at the window's width
      and hung at its text column>
    · <note>
    · <note>
  ```

  Unlike `task_list`'s, its fold is **per-block and session-only**: a click on
  the log, or `enter` at the block cursor on it, flips that one log and no
  other block — never every task card, never `ui.task-list-open` — and the
  state is written nowhere, so every log starts collapsed. A start with no
  routine note paints no log at all.
- A **Sub-agent or Skill group** is the fourth block that folds to its
  **header alone** (2026-11-20,
  [`adr-0035-amend-1120`](../../.capcom/knowledge/decisions/adr-0035/adr-0035-amend-1120.md)).
  Its header is ONE leader row in the umbrella header's shape —
  `✦ Sub-agents (3) ⋯⋯ ▼`, `✦ Skill (2) ⋯⋯ ▼`: the star, the group's label
  and its count, and a dotted leader to the fold's `▶`/`▼` at the block's
  right edge — and it is ALWAYS a fold: a click on it, or `enter` at the block
  cursor on it, folds the group to that header line, no member row beneath,
  and opens it again with every member as it was, on a flag of the group's
  own: session-only, written nowhere, live or settled, auto-collapse on or
  off — a small umbrella's model, with no size and no shared preference. A
  folded group with a member still working keeps its blinking `✦`. With
  `ui.tools-auto-collapse` on, a **settled** group — every member finished (a
  delegation's child reported, a skill fetch returned) and a later entry of
  its run placed after it or its run over — that the reader has not
  **touched** is folded to its header, and a click on it opens that one
  alone, on a fold of its own, writing nothing. Touching a group is moving its
  header's fold, opening a member — a sub-agent's run view, a skill's body
  inline — or jumping to a member from the side panel.
- A **lone groupable call** — one that could be an umbrella's member row yet
  heads none and joins none — has the auto-collapse fold and no other
  (2026-11-20): with `ui.tools-auto-collapse` on, a settled one (it returned,
  and a later entry of its run follows it or its run is over) that the reader
  has not touched folds to the Group summary an umbrella of that one call
  paints folded, `✦ Read 1 file ⋯ ▶`, `✦ Ran 1 command ⋯ ▶` ("Single tool
  folded (auto-collapsed lone call)" below). A click or `enter` on that line
  opens it to its standalone block exactly as it painted before, and it stays
  open — touched — for the session; it grows no header toggle, so every click
  on the open block keeps the meaning it always had. Live, touched, or with
  auto-collapse off, it paints as it always did. A call that is not groupable
  — a lone skill fetch, a lone delegation, an answered question, a task-list
  card — and a workflow block never auto-collapse.
- Every tool block's leading `✦` — an umbrella's, a lone call's (a task-list
  card and a lone Skill or delegation block included), a Sub-agent or Skill
  group's — paints in the scheme's `tool-header` role, not bold, so a folded
  line reads as a tool block rather than narration; the live star's blink and
  a Firing's `⟳` are unchanged (2026-11-20; `layout.md`).
- Failure marking: the red right-slot summary only; no glyph or header color
  changes. Failures propagate upward as red `N errors` on type rows.
- Keyboard: a **modal block cursor**, the transcript region of **navigation
  mode** (ADR 0111). `alt+up`/`alt+down` enters the mode and moves the cursor
  across the same toggle targets the mouse has (deepest visible level) — or
  starts it in the side panel when the transcript has none; inside the mode
  plain `up`/`down` move, `enter` toggles, `esc` or any printable key returns
  the keys to the prompt. While the side panel shows, `right` moves the cursor
  into it and `left` back to the transcript row it left
  (`docs/layout/side-panel-layout.md`, "Keyboard"); the row the cursor stands
  on is marked by a `▌` over column 0 — the marker cell — as well as by the
  selection tone across the row, so a terminal that paints no colour still
  shows it (ADR 0111 D6), and nothing re-wraps. `enter` on a framed sub-agent
  opens its run view instead of toggling it, and from inside a view `esc` goes
  **one level back** up before it means anything else — the block cursor is
  exited on the way in, and the breadcrumb header row is the mouse's spelling
  of the same move. With the cursor standing in the side panel, `esc` there
  leaves the mode and the view stays open.
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

## Single tool folded (auto-collapsed lone call)

With `ui.tools-auto-collapse` on (the default), a LONE groupable call that has
**settled** — it returned, and a later entry of its run placed after it or its
run over — and that the reader has not **touched** folds to the one line an
umbrella of that one call paints folded: its Group summary, a dotted leader
and the fold's `▶` ("Fold states and interaction" above). A click or `enter`
on it opens the block above, exactly, and it stays open for the session.

```text
✦ Ran 1 command ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ ▶
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

## Grouped tools collapsed (super-group)

One row per consecutive run, in time order. A row's
`<tool-top-level-details>` aggregates its run.

```text
✦ <group-summary> ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ ▼
  ┝ <tool-type-header> (<group-count>) ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ <tool-top-level-details> ▶
  ┝ <tool-type-header> (<group-count>) ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ <tool-top-level-details> ▶
  ┕ <tool-type-header> (<group-count>) ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ <tool-top-level-details> ▶
```

## Grouped tools collapsed (one run, super-group)

A run of 2+ calls of ONE tool type takes that same shape: the umbrella header
over a single type row. It keeps the row count of the retired `✦ Terminal (4)`
group it replaced — one header, one row — and reads the same everywhere.

```text
✦ Ran 4 commands ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ ▼
  ┕ Terminal (4) ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ exit 0 ▶
```

## Grouped tools expanded 1st step (super-group)

```text
✦ <group-summary> ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ ▼
  ┝ <tool-type-header> (<group-count>) ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ <tool-top-level-details> ▶
  ┝ <tool-type-header> (<group-count>) ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ <tool-top-level-details> ▼
  │ ┝ <tool-details> ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ <tool-top-level-details> ▶
  │ ┝ <tool-details> ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ <tool-top-level-details> ▶
  │ ┕ <tool-details> ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ <tool-top-level-details> ▶
  ┕ <tool-type-header> (<group-count>) ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ <tool-top-level-details> ▶
```

## Grouped tools expanded 2nd step (super-group)

```text
✦ <group-summary> ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ ▼
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

## Grouped tools folded (large umbrella)

A LARGE umbrella — more type rows than `ui.tools-fold-over`, whatever the Turn
is doing and wherever the umbrella sits — that auto-collapse is not holding
(live, touched, or `ui.tools-auto-collapse` off) folds to its header alone
under the shared `ui.tools-open`; the `▶` sits on the header and no type row is painted
beneath it. Open, it is the "Grouped tools collapsed (super-group)" shape
above with its `▼` at the leader's edge. With a call in flight the `✦` blinks as it
does on an open header; the line carries nothing else.

```text
✦ <group-summary> ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ ▶
```

## Grouped tools folded (small umbrella)

A SMALL umbrella — type rows at or under `ui.tools-fold-over` — that
auto-collapse is not holding folds to the same one line from a click or `enter`
on its header, on a flag of its own: in-session only, written nowhere, so it
starts every session open. The three folded shapes — this one, the large
umbrella above and the auto-collapsed one below — are indistinguishable on
screen; only whose state the fold is differs.

```text
✦ Read 1 file, searched 1 pattern ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ ▶
```

## Grouped tools folded (auto-collapsed umbrella)

With `ui.tools-auto-collapse` on (the default), an umbrella of ANY size folds
to its Group summary line on its own once it is **settled** — every call
returned, and a later entry of its run placed after it or its run over — and
the reader has not **touched** it ("Fold states and interaction" above). It
needs no click and writes nothing; `ui.tools-open` does not reach it. A
failure stays visible on the one row, and a click or `enter` on it opens that
umbrella alone, as view state, leaving every other umbrella and the shared
preference as they were.

```text
✦ Read 3 files, ran 4 commands, edited 2 files +18 −5 · 1 failed ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ ▶
```

## Grouped Sub-agents

- opening a sub-agent — from a click on its row or `enter` on the block cursor — opens its **run view** (`layout.md`, "Run view"), which takes the whole transcript slot under a `← main › <name>` breadcrumb and is left with `esc`. A member row of an `✦ Sub-agents (N)` list opens the same way; the umbrella itself still opens **inline** to those rows.
- `^x` stops ONE running sub-agent (ADR 0086 D5; `layout.md`, "Run view"): inside its run view it stops the run on screen — the view's header and the status line then read `esc back · ^x stop` while that run can still be stopped — and on the block cursor it stops the delegation whose row the cursor stands on, a member row of an `✦ Sub-agents (N)` list included — or, with the cursor in the side panel, the sub-agent row it stands on there — leaving the cursor where it was. It needs no confirmation and the parent's Turn goes on: the engine folds what the run had done into the partial result the parent reads, and the row settles on `· stopped by you` once the run reports. On a finished run it does nothing. The whole Turn is cancelled by `esc×2` at the top level, never by `^x`.
- the inline expanded shape that used to open a delegation's span in place — no `⤷ sub-agent` label on the open row, a colored vertical line down its left — was REMOVED on 2026-08-30 with the run view (ADR 0063), and its sketch went with it.
- `┊` was only displayed if another grouped sub-agent followed after the expanded sub-agent, and the last sub-agent in the group (if expanded) did not show it. No DELEGATION draws it any more — a member opens its run view rather than a span in place, so every grouped sub-agent is followed by the ordinary separator — but the rule is the group's, not the delegation's, and **Grouped Skills** below is where it fires: a member that opens INLINE is closed by `┊` wherever another member of its list resumes after it, and the list's last member shows none however it ends.
- a RUNNING sub-agent's `<tool-top-level-details>` reads `N tool calls · <used>/<window> · 80 steps` and names no ongoing action. The call in flight is deliberately not spelled there: it changed several times a second while the two cells beside it held still, and each of those calls already has a block of its own inside the run, one click away. The one live word the slot adds is `· delegating`, and only while the most recent call open inside the run is itself a sub-agent — work the child has handed on, which its own blocks cannot show since the nested run is collapsed too. The trailing cell is the STEP CAP the child runs under, as its started phase carried it (`domain.SubAgentPhaseEvent`): `· 80 steps` for the configured delegate cap or the call's own lower `max_steps`, `· 80 steps (120 asked)` where the call asked for more than the configured cap and was clamped to it, and no cell at all for an unbounded child (`delegate-max-steps: 0`). The row wears it from the child's FIRST TOOL CALL — a started child with nothing behind it has no summary line to carry it — and only while the run works: a finished row ends on its gist, the bound being history once the child has reported. A finished sub-agent's slot is its report's first line, or `· done` where the report became a body — unless the engine wrapped that report in an outcome envelope, which takes the slot instead: `· capped at its step cap` where the step cap cut the run off mid-task (`· capped at its token budget` / `· capped at its time limit` for its sibling bounds — "capped", because on screen "stopped" is the human's word alone, whatever wording the engine's result head carries for the parent model), `· stopped by you` where the human stopped the run with `^x` (ADR 0086 D4) — a queued pooled run stopped before it started, or one a turn cancel caught queued, included — `· not started · your message` where a queued message pre-empted the run before it started (ADR 0025), both of which wear no ✓ and reads in the marker tone, since the run did not finish and nothing failed, `· ended without a report` where the child reached its own boundary but handed back narration of a next step, a paste or the `[delegate returned no report]` marker rather than a report (the row's reading alone — the parent model gets the result exactly as the engine wrote it), and `· steered by 2 messages` appended to whichever verdict stands where the human addressed the child while it ran (ADR 0063 D3); a failed run's red slot carries that same steering cell after its cause. Where the width leaves the row too little of its target to be worth reading, the promote-guard drops the GIST and nothing else: the row reads the same line with the engine's verdict in the gist's place — `1 tool call · done`, `2 tool calls · 12k/32k · capped at its step cap` — so the count and the fill survive every width, and nothing lands in a body, a run's row growing none in place (ADR 0063) and its report being one click away in the run's own view. A run routed to the Sub-agent server (ADR 0045) closes the slot with the model it ran on — `2 tool calls · 12k/32k · Found 4 gaps · qwen3-4b` — and only when that is not the session's own model; a same-model delegation shows no such cell. `<window>` is the CHILD's own window, so a routed run's fill is spelled against the Sub-agent server's window (`7k/8k`) rather than the session's (`7k/128k`); a run reporting no window of its own is spelled against the session's, which is what an unrouted child inherited. This holds for a lone sub-agent exactly as for a grouped member.
- opening a sub-agent takes nothing back either: the row the reader leaves behind keeps the very `<tool-top-level-details>` it wore — the count, the fill, and the gist or the verdict (`· done`, `· ended without a report`, …) — and the task, the report and the child's own blocks are what the run view shows. This holds for a lone sub-agent exactly as for a grouped member, running and finished alike; a view of a finished or queued run opens read-only.
- a sub-agent the engine has not started yet is QUEUED: the model asked for it and it is queued behind the `parallel-agents` cap, holding no slot. Its `<tool-top-level-details>` says exactly `queued` (the side panel's word for the same phase) — no tool-call count, no context fill, no gist, none of which exist yet — and its row carries no indicator and no click target, because there is nothing behind it to open. The moment its child starts, the row becomes an ordinary live sub-agent row and opens like any other. A lone sub-agent starts immediately and never shows this.
- the group's header is a FOLD (2026-11-20; "Fold states and interaction" above): its leader row runs to a `▼` while the member rows show and a `▶` once a click or `enter` on it has folded the group to that header line alone — or `ui.tools-auto-collapse` has, the group being settled (every member reported) and untouched. The flag is the group's own, session-only and written nowhere; a member still running keeps the folded header's star blinking. A lone sub-agent is no group and never folds this way.

```text
✦ Sub-agents (<group-count>) ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ ▼
  ┝ first-sub-agent-collapsed ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ ▶
  ┝ sub-agent-whose-row-opens-its-run-view ⋯⋯⋯⋯⋯⋯⋯⋯⋯ <tool-top-level-details> ▶
  ┝ another-collapsed-sub-agent ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ ▶
  ┕ last-grouped-and-collapsed-sub-agent ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ ▶
```

Example for how finished sub agents should be displayed. The first sub-agent is done and receives a `✓` after the sub agent name ("done" is also printed in the <tool-top-level-details>, The second sub agent is still running in this example.

Each member is marked PER MEMBER, the moment that sub-agent finishes — not when the group as a whole is done. A grouped run reports its results in one burst once every member has joined, so the display follows each delegation's own start/finish instead: the first to report wears its `✓` and its `done` while its siblings are still running, and opening its run view shows the report it returned right then. A member that finished with a FAILURE wears no `✓` — its red `<tool-top-level-details>` is the whole of the marking — and neither does one that `ended without a report` or one `stopped by you`, whose verdicts read in the marker tone. Each row is matched to its own child by the delegation's run id, never by its call id, which a model or server may repeat across one fan-out: a sibling that finishes first ticks its own row and no other.

```text
✦ Sub-agents (<group-count>) ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ ▼
  ┝ <tool-type-header> ✓ ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ <tool-top-level-details> ▶
  ┕ <tool-type-header> ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ <tool-top-level-details> ▶
```

Example for a folded group — by a click on its header, or by auto-collapse once it has settled:

```text
✦ Sub-agents (<group-count>) ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ ▶
```

Example for a fan-out wider than the `parallel-agents` cap: the first sub-agent holds a slot and is working, the second is queued behind it. The queued row states `queued` and nothing else, and it wears no `▶` — clicking it does nothing until it starts.

```text
✦ Sub-agents (<group-count>) ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ ▼
  ┝ <tool-type-header> ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ <tool-top-level-details> ▶
  ┕ <tool-type-header> ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ queued
```


## Grouped Skills

- skill fetches group with **each other** exactly as sub-agent calls do — `✦ Skill (<group-count>)` over one `┝`/`┕` row per fetch — and never join the mixed `✦ Tools` super-group. Two adjacent fetches are the floor, as they are for a fan-out; a lone fetch keeps the standalone `✦ Skill` block it always had.
- a member row leads with the **loaded skill** and its `<tool-top-level-details>` is deliberately blank, so the dots run to the `▶`. A fetch that loaded nothing keeps the query the model searched by on the row, which is the same slot saying the same thing: what this call is about.
- expanding a member opens its body **INLINE**, under its own row, and that is the ONE way this group differs from the delegation group whose shape it borrows. A fetch heads no run, so there is no run view to take the transcript slot, no `← main › <name>` breadcrumb and nothing for `esc` to leave; the member's rows sit inside the list, and the list resumes below them.
- because a member opens in place, the `┊` closer has its occasion here (Grouped Sub-agents, above): the seam between an expanded member and the row resuming the list after it is the closer, and the list's LAST member draws none however it ends.
- the header folds exactly as a Sub-agent group's does (Grouped Sub-agents, above): `▼` open, `▶` folded to the header line alone, by a click or `enter` on it or — settled (every fetch returned) and untouched — by `ui.tools-auto-collapse`; opening a member's body inline touches the group. A lone fetch keeps its standalone `✦ Skill` block, which never auto-collapses.

```text
✦ Skill (<group-count>) ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ ▼
  ┝ <tool-details> ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ ▶
  ┝ <tool-details> ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ ▼
  │ <tool-details-row-1>
  │ <tool-details-row-2>
  │ <...>
  │                                                                 see less…
┊
  ┕ <tool-details> ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ ▶
```

Folded — by a click on its header, or by auto-collapse once it has settled:

```text
✦ Skill (<group-count>) ⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯⋯ ▶
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
| write_file | Write | path | `N lines` | the recorded diff of what it wrote, numbered — split/stacked diff, see `split-diff-layout.md` |
| edit_existing_file | Edit | path | `+A −R` | split/stacked diff, see `split-diff-layout.md` |
| single_find_and_replace | Replace | path | `+A −R` | split/stacked diff, see `split-diff-layout.md` |
| multi_find_and_replace | Replace | path | `+A −R` (`N changes` when the result records no regions) | split/stacked diff, see `split-diff-layout.md` |
| copy_file | Copy | source `→` destination | — | full paths |
| move_file | Move | source `→` destination | — | full paths |
| delete_file | Delete | path | — | — |
| list_dir | List | path (`· recursive` when set) | `N entries` | the listing |
| find_files | Find Files | pattern | `N files` | matched paths |
| grep | Grep | pattern (`· include` glob) | `N hits · M files` | `path:line` matches + context |
| terminal | Terminal | the command line | `exit 0` | command + output; `(no output)` for a clean run that printed nothing, the failure text the model received for a failed one |
| python_exec | Python | first code line | `exit 0` | code + output; `(no output)` for a clean run that printed nothing, the failure text the model received for a failed one |
| console_open | Console | the command line | `console N` (`exit N` when the program was already over) | the program's first output |
| console_send | Console Send | `console N` (`· what was typed` when the input is not empty) | `alive` / `exit N` / `killed` | what the program printed back |
| console_read | Console Read | `console N` | `alive` / `exit N` / `killed` | the output since the last read |
| console_close | Console Close | `console N` | `exit N` / `killed` | the unread tail |
| run_tests | Tests | path, else `whole project` (`· filter` when set) | `PASS` / `FAIL` | runner summary + failing tests |
| git_status | Git Status | `working tree` | `N changed` | changed-file list |
| git_log | Git Log | ref | `N commits` | one line per commit |
| git_show | Git Show | path (`:12–80` when ranged) `@ ref` (`· locate "…"` when set) | `N lines` | the located lines (`Located "…" on lines: …`) when locate is set, else — |
| git_branch | Git Branch | action + branch name | — | command output |
| git_commit | Git Commit | message subject | short hash | full message + committed files |
| git_diff_range | Git Diff | `base...head` | `+A −R` | split/stacked diff, one header row per file section, see `split-diff-layout.md` |
| view_diff | Diff Preview | path | `+A −R` | split/stacked diff, see `split-diff-layout.md` |
| diagnostics | Diagnostics | path | `N issues` / `clean` | one line per issue |
| http_request | HTTP | METHOD + URL | `HTTP <status> · <size> · ~<n> tok` — `<size>` the bytes of what the model received, `~<n> tok` the engine's estimate of their tokens (left out on a record with none); `· truncated` last when the body hit the response cap | response headers + body head |
| web_fetch | Fetch | URL | `HTTP <status> · <size> · ~<n> tok`, `· truncated` last when cut — as http_request | extracted text head |
| web_search | Search | the query | `N results` | result titles + URLs |
| present_document | Present | document title (path fallback) | — | path + title |
| ask_user | Ask User | the question | the human's own answer | question + choices + the answer |
| sub_agent | Sub-agent | its name (task head fallback) | `queued` before it starts; while it works `N tool calls · <used>/<window> · 80 steps` (`· delegating` before the cap while its newest open call is a sub-agent); once it reports `N tool calls · <used>/<window> · <gist>`, the gist being the report's first line or the verdict (`done` / `capped at its step cap` / `stopped by you` / `ended without a report` / …), and a routed run's model last — see **Grouped Sub-agents** above | task text + result summary |
| fan_out | Fan-Out | the brief's first line (`task`) | the result's first line | the card's item rows, one delegation-shaped row per item run, each opening its run view |
| workflow | Workflow | the action (`status` / `stop` / `message`) | the result's first line | — |
| task_list | Tasks (done/total) | — | — | the list, one row per task; collapsed = header only |
| load_skill | Skill | the loaded skill (the query until one is) | — | the skill body |

Notes:
- **2026-08-19** — the five diff-bodied rows above (six since `write_file` joined, 2026-09-04) now render through
  `split-diff-layout.md` (Split diff where the width allows, Stacked diff below
  it), delivered by
  `docs/plans/archived/2026-08-19 - 03 - split-diff-display-plan.md` and ratified by
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
- **2026-09-03** — the `task_list` row above arrived with the tool itself
  (ADR 0072). It carries no target because its one argument IS the list, and
  its count rides the header rather than the outcome slot: `✦ Tasks
  (done/total)` — done rows over all rows of the list the tool echoed back,
  painted in the same faint count tone as the umbrella's breakdown — while
  the slot stays blank. A result with no rows at all — a cleared list, or a
  refusal — drops the count and reads plain `✦ Tasks`.
  **2026-09-14** — the block folds to that counted header (the `Fold states`
  exception above; the state is shared by every task-list card and remembered
  in `ui.task-list-open`), and the header sentence the tool writes for the
  model (`Task list — yours to maintain; call task_list with the COMPLETE
  list…`) is stripped display-side: the card paints the task rows alone under
  its label. The result text is the model's and reaches it unchanged
  (ADR 0031) — the strip is a render-time act on a retained result, like
  every extractor's.
- **2026-09-30** — the `fan_out` (ADR 0087) and `workflow` (ADR 0089) rows
  above complete the table: it now lists all 36 built-in tools the build
  carries (`KnownToolNames`). Both ship default-off, and both take the plain
  presenter floor in `internal/tui/toolregistry.go` — a target, the result's
  first line as the slot, no typed stat. `fan_out` heads a run like
  `sub_agent` and never joins a list; its card paints the Workflow's item
  rows (`workflowblock.go`), and its fold hides the card's own body, never
  those rows.
- **2026-11-12** — the `git_status` row's target is the fixed words
  `working tree` (it was `—`). The tool takes no arguments, and the branch is
  only known from the result, so a fixed phrase is the target the row can
  carry while the call runs. Having one is what lets it join a grouped Tools
  block and count there like any other read; its report stays behind the
  member row's own indicator.
- **2026-11-12** — a `run_tests` call that names no `path` targets the words
  `whole project` (it was empty): the tool then runs the workspace's whole
  suite, and the common call carries no argument at all. A filter still
  qualifies it (`whole project · TestX`). The target is what lets a bare run
  join a grouped Tools block and count there; the live status line keeps its
  plain `running tests`, which never reads the target.
- git_commit never promotes its one-line output into the slot at any width: the
  line repeats the subject the row already leads with, so the slot holds the
  short hash above and the line lays out in the body.
- ask_user renders as the live prompt while pending; this table describes its
  transcript form after the answer.
- sub_agent deliberately shows *different* data on its row (its name) than
  inside its run view (task + result). Its `N steps` stat is wired only if the
  engine already exposes a step count; otherwise the slot shows `done`/`failed`
  alone. A delegation queued behind the `parallel-agents` cap shows
  `queued` and opens nothing at all (see Grouped Sub-agents).
- Expansion state lives on the transcript entry, so it survives scrolling and
  new messages arriving. New blocks start collapsed. A run view is not an
  expansion state and lives on no entry: it is the Driver's own stack of open
  runs, dropped at a resume (ADR 0063).
