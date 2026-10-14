# `/settings` — screen layout

## The owner's requirements (2026-08-05, verbatim)

- Items marked with **XXX** are just printed bold.
- When a row is in edit mode (after pressing enter), it should be printed in a highlighted color
- Section headers like UPSTREAM or AUTONOMY should be printed in white
- before a section header needs to be a free spacer line
- all multi select fields like `server` or `mode` should get question popo up to select one of the available options (serve does current just enter write mode)
- settings must take effect immediatelly when leaving the settings screen. I do not want "→ new value (next launch)". Also the new value should be displayed in place marked with a ` *` e.g. `false *`
- text edit mode must allow using the cursors to move in the text (same functionality like in the prompt edit window incl. usage of mouse)

## As shipped (2026-08-06)

The requirements above were built as part of `docs/plans/archived/2026-08-06 - 00 - settings-live-apply-plan.md`
and ratified in [ADR 0037](../../.capcom/knowledge/decisions/adr-0037.md). The
mockups below are the pane as it now paints, captured from the renderer, and they replace the
hand-drawn sketch the requirements were written against. Where the two differed:

- **An edit applies on the `⏎` that commits it**, not on leaving the screen (ADR 0037 decision 1) —
  which is what the requirement asked for, arrived at one keypress earlier. Closing the pane is
  dismissal only, and nothing is batched. The ` *` marker is exactly as drawn.
- **The pane's title is a row inside the box**, not a name spliced into the top border: `/settings`
  is a full-height screen rather than one of the frame's short menus, and its rows already carry the
  border-title machinery's budget on the description header (`layout.md`, "One pane may claim the
  whole budget").
- **Key rows carry no `·` lead.** The sketch's `·` reads as a bullet on every row; in the built pane
  the `· ` prefix means one specific thing — the row's last cell, which is the note or the pointer —
  so a lead on the key would have made the two unreadable against each other.
- **Section labels are title case** (`Upstream`, `System prompt`), white over the faint rows they
  open, with the free spacer line above each one except the first, which the description header's own
  closing blank already sets off.
- **`system-prompt-file` and `context-files.names` are editable in the pane** (ADR 0037 decision 5),
  so neither carries the `edit in config.yaml` label the sketch showed; the label exists nowhere any
  more. What replaced it on the blocks no row can hold is `· ⏎ opens $EDITOR`.
- **A row's value cell is blank when the key holds nothing** and it is a row that seeds an edit
  field (`system-prompt-file`). `none` is reserved for the structured rows, whose
  `⏎` opens an editor rather than a field.

### The key list

```
╭──────────────────────────────────────────────────────────────────────────────────────────────╮
│ Settings                                                                                     │
│ Description: The servers you run models on — name, endpoint, and what each one needs.        │
│                                                                                              │
│                                                                                              │
│   Upstream                                                                                   │
│ ❯ servers               2 servers    (global)   · ⏎ opens $EDITOR                            │
│   server                macStudio    (global)                                                │
│                                                                                              │
│   Autonomy                                                                                   │
│   mode                  ask-before   (global)                                                │
│                                                                                              │
│   System prompt                                                                              │
│   system-prompt-text    7 lines      (global)                                                │
│   system-prompt-file                 (default)                                               │
│   system-prompt-models  none         (default)  · ⏎ opens $EDITOR                            │
│   context-files.enable  true         (default)                                               │
│   context-files.names   [AGENTS.md]  (project)                                               │
│                                                                                              │
│   Confinement                                                                                │
│   confine-to-workspace  true         (default)  · use /confine                               │
│   unconfined-hosts      none         (default)  · use /confine                               │
│                                                                                              │
│   Model profiles                                                                             │
│   model-profiles        native       (default)  · ⏎ opens $EDITOR                            │
│                                                                                              │
│ type to filter · ↑/↓ select · ⏎ edit · ⌫ reset · esc close                                   │
╰──────────────────────────────────────────────────────────────────────────────────────────────╯
```

The `Description:` header is a **fixed two-line region** with a blank line closing it, so walking the
list moves the highlight and nothing else; a longer description loses its tail to an `…` rather than
the list losing a row. That closing blank is also this pane's **breathing row** above the list — the
one every other boxed pane spends on itself, which is why the pane asks for none of its own — and the
blank above the key legend is its counterpart below. Both are bought out of the row window rather
than out of the pane's height, so a short terminal gives them up and keeps the keys. Rows are four
cells — key, value, a source mark, and the note or pointer. The mark names where the row's value
comes from: `(default)`, `(global)` for the global config file, `(project)` for the Project config
layered over it, `(env)` or `(flag)` for an override — so every row carries one, and the column
collapses away only on rows a provider reported without a source. An override is the one fixed at
launch; which file supplies a key is read off the files as they stand now, so the mark — and whether
the row can be written here — follows every save and reset this pane makes and every outside save
the config watcher reloads. The note column collapses away on
a configuration with nothing read-only and nothing edited yet. A commit on a key a project may set
(a project-param or tighten-only key, ADR 0096 §6) asks which file it lands in before anything is
written — the legend becomes `Save to: [global] / this project · ←/→ choose · ⏎ save · esc back`
(`Reset in:` for a reset), the bracket on the file that supplies the key now, and the row's value
cell shows the value being saved. The legend names `←/→`, but `↑/↓`, `tab` and `shift+tab` move the
bracket too: there are two files, so every one of those keys swaps it to the other. The row then reports the file that supplies the key and the value
both files resolve to together. A row the Project config sets that this pane cannot write there
points at it (`· set in the project config`).

The mockup is **abridged**: it shows five of the pane's ten sections. In the built pane
`Tools & skills`, `Session`, `Presentation`, `Interface` and `Reactions` sit between `Confinement`
and `Model profiles`, in that order, and a section is a run over the registry's own order rather than
a per-key label — so a key added to the registry inherits the section it was inserted into. The
five sections it does show are **abridged too**: `Upstream`, for one, also holds `currency`,
`sub-agents-server` and `sub-agents-choice`, and `System prompt` holds `use-default-prompt` and
`system-prompt-layers`. The registry (`internal/config/registry.go`) is the row list; the mockup is
the shape.

The list is taller than any terminal, so it **scrolls**: the row window follows the highlight, and a
section heading is a row like any other — it scrolls away with the rows above the window rather than
pinning to its top, so a window opened mid-section starts on a key row, not on a label. While the
list overflows its window and `ui.show-scrollbar` is on, a scroll bar runs down the pane's right-hand
column, its thumb standing for the rows in view; a list that fits paints no bar and gives up no
column. The mouse wheel walks the highlight one row per notch, clamping at the first and the last key
where `↑/↓` wrap round.

After `Model profiles` comes one more section, `Allow rules`, whose rows are no registry key's: one
per Allow rule the session holds (ADR 0096 §4) — the rules in force first, then the proposed and the
rejected project rules — keyed by list and text (`terminal: go test`), with where the rule stands as
the value (`live` for a global rule, `adopted`, `proposed`, `rejected`) and its layer as the source
mark. ⏎ adopts a proposed or rejected rule; ⌫ (or ⌦, whatever the filter holds) arms its removal
from the file that holds it and ⏎ confirms (`⏎ confirm remove · esc cancel`). The legend on a
rule's row reads `type to filter · ↑/↓ select · ⏎ adopt · ⌫ remove · esc close`, dropping
`⏎ adopt` on a rule already in force. A session with no rules shows no such section.

### The filter

```
╭──────────────────────────────────────────────────────────────────────────────────────────────╮
│ Settings · filter: mode▌                                                                     │
│ Description: Autonomy mode: how tool calls are gated, from least to most autonomous.         │
│                                                                                              │
│                                                                                              │
│   Autonomy                                                                                   │
│ ❯ mode                                ask-before  (global)                                   │
│                                                                                              │
│   System prompt                                                                              │
│   system-prompt-models                none        (default)  · ⏎ opens $EDITOR               │
│                                                                                              │
│   Session                                                                                    │
│   remember-model                      true        (default)                                  │
│                                                                                              │
│   Presentation                                                                               │
│   present.command-on-model-documents  false       (default)  · ⏎ opens $EDITOR               │
│                                                                                              │
│   Model profiles                                                                             │
│   model-profiles                      native      (default)  · ⏎ opens $EDITOR               │
│                                                                                              │
│ ↑/↓ select · ⏎ edit · ⌫ trim · ⌦ reset · esc close                                           │
╰──────────────────────────────────────────────────────────────────────────────────────────────╯
```

The key list is a **searchable list**, and it keeps the one filter contract every such list keeps
(ADR 0053, 2026-10-14 amendment). There is no key that starts a search: **every printable key types
into the filter**, and a paste is typing — it lands in the filter folded onto one line, never in the
chat draft under the pane. What a filter matches is what the row *says it is*: a case-insensitive
substring of the **key path** or of its **section heading**, matched apart so a filter never spans a
path and the heading after it. A heading match keeps its whole section (`interface` keeps every row
under `Interface`, whatever its key is called), and a heading stays on screen while any of its rows match. The value, the
source mark and the note are never matched — a filter that found rows by their live state would
answer differently from one moment to the next — and nothing is ranked, reordered or highlighted.

The filter **rides the title row** — `Settings · filter: mode▌` — and only while it holds text, so
no row is taken from the list and nothing moves on the first key: the description header and the
list stay where they were. When the title row is too narrow the filter stays whole, or shows its
caret end behind a leading `…`. The highlight **stays on its row** while that row still matches,
else it moves to the first match, and it stays where it is when the filter empties; `↑/↓` walk and
wrap over the matching rows only.

**`⌫` trims** a filter that holds text. On an empty filter it keeps the meaning it had before the
pane filtered — it arms the selected row's reset (or an Allow rule's removal). **`⌦` arms that
reset whatever the filter holds**, so a filtered view never has to be thrown away to reset the row
it found. **`esc` closes the pane**, filter and all; `⌫` is the filter's only undo.

The legend follows the filter's state. Empty, it leads with `type to filter`, as in the key list's
mockup. Once text is typed `type to filter` drops, `⌫ trim` stands before `esc close`, and the row's
backspace verb moves to `⌦` just behind it — `⌦ reset`, or `⌦ remove` on a rule's row; the `server`
row, which takes no reset, gets no `⌦` entry. A filter that matches nothing paints one faint,
unselectable row in the pane's noun, the description header stays as tall as before and blank (no
row is highlighted for it to describe), and the legend keeps only the trim and the way out:

```
╭──────────────────────────────────────────────────────────────────────────────────────────────╮
│ Settings · filter: zzz▌                                                                      │
│                                                                                              │
│                                                                                              │
│                                                                                              │
│   no setting matches "zzz"                                                                   │
│                                                                                              │
│ ⌫ trim · esc close                                                                           │
╰──────────────────────────────────────────────────────────────────────────────────────────────╯
```

**The filter survives the pane's sub-steps.** `⏎` on a filtered row opens the same step it opens
unfiltered, and the keys inside that step are the step's — none of them reaches the filter. A step
drawn over the list — the single-line field, the armed reset, `Save to:` — keeps the filter text on
the title row with no caret, because the next keystroke lands in the step and not in the filter. A
step that replaces the list — the value sub-list and the multi-line field — hides it, since the list
it narrowed is not on screen. Either way the step ends back on the filtered list, the filter as it
was.

### An edited row

```
│ ❯ context-files.enable  false *      (global)   · applies at next clear                      │
```

The ` *` says *this session changed this key here* and is cleared only by a relaunch; the mark beside
it already names the file the save landed in. The note beside
it is the one deferral wording the surface has: `context-files:` is part of the prefix every request
is cached against, so it lands at the next `/clear`. Every other key is already in force by the time
the row repaints. Where an environment variable or a flag outranks the file the note reads
`· APOGEE_MODE outranks at next launch` — about the next start, not about this edit, which applied;
a global save under a value the Project config sets reads `· saved to global; the project config
outranks it`, since the project's value stays in force —
and a write whose apply then failed reads `✗ saved — live apply failed: …`.

### The selection popup (`mode`, `server`, every 3-plus-option key)

```
╭──────────────────────────────────────────────────────────────────────────────────────────────╮
│ Settings                                                                                     │
│ mode — Autonomy mode: how tool calls are gated, from least to most autonomous.               │
│                                                                                              │
│ · plan                                                                                       │
│ ❯ ask-before   (current)                                                                     │
│ · allow-edits                                                                                │
│ · auto                                                                                       │
│                                                                                              │
│ ↑/↓ select · ⏎ set · esc back                                                                │
╰──────────────────────────────────────────────────────────────────────────────────────────────╯
```

The sub-list opens **on the value the key already holds**, so pressing `⏎` twice confirms rather than
silently changes. The rows it does not use go back to the transcript while the question is open. The
`server` row's vocabulary is this config's own `servers:` block, and committing it performs the full
live switch — the same move `/server` makes. Confirming the server the session is **already on**
switches nothing, and the row says so — `· already on macStudio` — because the answer `/server` gives
in the transcript is behind this pane.

The `server` row is also the one row whose **kind** takes no reset: `⌫` and `⌦` do nothing on it,
and its hint line reads `type to filter · ↑/↓ select · ⏎ edit · esc close` to say so. `server:` is
not a value the pane writes but the *recording* of a switch (ADR 0036 decision 2), and the only door
onto it is the switch itself (ADR 0037 decision 5): deleting the line would leave the session running
against a server the file no longer names. Choosing a different server is how this key changes.

It is not the only row the reset keys pass over silently — they arm nothing, and say nothing, on
three kinds of row:

- a **non-editable** row — one whose last cell already names where it is edited (`· use /confine`,
  `· ⏎ opens $EDITOR`);
- a row **already at its default** that this session has not edited — there is no line to remove;
- the **`server`** row, for the reason above.

Only the last drops `⌫ reset` from its legend. The other two keep it, because the distinction is the
row's kind and not the moment's: a default-valued row is one edit away from having something to
reset, and a read-only row's own cell already says why nothing happens there.

### The single-line field (string and int keys)

```
│ ❯ system-prompt-file    ~/prompts/apogee.md▏  (global)                                       │
```

The field opens **in place**, on the row and in the value's own column, seeded with what the key
holds. It is a real editor: cursor keys, `home`/`end` and word jumps move the caret, and the mouse
seats it and drags a selection exactly as in the prompt box — `backspace` or `delete` over a
selection removes the selected text and leaves the caret where it began. A paste lands **in the
field** — never in the chat box the pane is drawn over — folded onto the one line the row can show,
each newline becoming the space that stood where the break was. The hint reads `⏎ save · esc cancel`.

### The multi-line field (`system-prompt-text`)

```
╭──────────────────────────────────────────────────────────────────────────────────────────────╮
│ Settings                                                                                     │
│ Description: The standing system prompt, written inline.                                     │
│                                                                                              │
│                                                                                              │
│   You are apogee.                                                                            │
│                                                                                              │
│ ❯ Be brief.▏                                                                                 │
│                                                                                              │
│ ctrl+s save · esc discard                                                                    │
╰──────────────────────────────────────────────────────────────────────────────────────────────╯
```

The field replaces the row list, keeping the border, the title and the description header — which is
already about the key being written. `⏎` inserts a newline, so the commit moves to `ctrl+s` and the
hint says so. Lines wrap rather than truncate, and the scroll window follows the caret's line.

The mouse reaches this field too, over all of it rather than over one row: a click seats the caret at
the glyph under the pointer — on a wrapped continuation as readily as on a line's own row — a drag
selects across the lines and its release copies exactly those runes, the newlines among them, and the
wheel walks the prose a line at a time (the window follows the caret, so moving it *is* the scroll).
`backspace` or `delete` over a selection removes it, newlines and all, as in the one-line field.
A paste lands here with its lines intact, which is what this field is for.

### The external edit (`· ⏎ opens $EDITOR`)

The fifth key class has no field at all: every key no row writes bar the confinement pair (which
points at `/confine` instead) — the six nested structures `servers`, `system-prompt-models`,
`system-prompt-layers`, `mcp-servers`, `reactions` and `model-profiles`, and the file-only scalars
`sub-agents-server`, `tools.enabled`, `workflow-retries`, `workflow-continuations`, `workflow-wake` and
`present.command-on-model-documents` — carry the `· ⏎ opens $EDITOR` pointer in their last cell, and `⏎` opens the file
itself on that key's line where the editor takes a line argument.

Which editor is a **four-rung ladder** (ADR 0041): the `editor` config key, then `$VISUAL`, then
`$EDITOR`, then the platform's default opener (`open`, `xdg-open`, `cmd /c start`). The pointer's
wording stays `⏎ opens $EDITOR` — it names the affordance in the spelling a terminal user reads it
in, and the row for `editor` is where a command set for apogee is shown.

**The pane's own behaviour splits on what that resolves to.**

- A **terminal** editor — `vi`, `vim`, `nvim`, `nano`, `pico`, `emacs`, `micro`, `hx`, `kak` — takes
  the terminal. The pane goes away, the editor draws over the whole screen, and on its exit the file
  is re-read: exactly the round trip ADR 0037 shipped, unchanged.
- Everything else is started **detached**. The pane does not go away, nothing is suspended, no frame
  is torn down — the row simply gains `· opened in your editor` in the same last cell, and the
  highlight stays where it was. That sentence is the whole of what the screen has to show for a
  keypress whose window opened behind the terminal or on another desktop, which is why it is painted
  at all.

Either way nothing is applied by the launch. What applies an edit is the **saved file**, which the
config watcher reports; a detached launch therefore leaves the pane exactly as it found it, and the
rows repaint — each changed one wearing its ` *` — whenever the save happens, with no keypress in
between. A launch that could not start at all (no such program on this machine) lands on the
launching row as a refusal in the `✗` slot, naming all three ways to set an editor.

The `editor` key itself is an ordinary editable string row in the **Interface** section, between
`cursor-shape` and the `Reactions` heading. Its value cell is **blank when unset** — the blank
that seeds an edit field, not the `none` reserved for the structured rows — because unset is a real
state here (the ladder falls through to the OS opener) and the row is one `⏎` from being a field.
