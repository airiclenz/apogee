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

The requirements above were built as part of `docs/plans/2026-08-06 - 00 - settings-live-apply-plan.md`
and ratified in [ADR 0037](../adr/0037-every-settings-edit-applies-to-the-running-session.md). The
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
  field (`llama-launcher`, `system-prompt-file`). `none` is reserved for the structured rows, whose
  `⏎` opens an editor rather than a field.

### The key list

```
╭──────────────────────────────────────────────────────────────────────────────────────────────╮
│ Settings                                                                                     │
│ Description: The upstream servers this workspace can talk to.                                │
│                                                                                              │
│                                                                                              │
│   Upstream                                                                                   │
│ ❯ servers               2 servers    · ⏎ opens $EDITOR                                       │
│   server                macStudio                                                            │
│   llama-launcher                                                                             │
│                                                                                              │
│   Autonomy                                                                                   │
│   mode                  ask-before                                                           │
│                                                                                              │
│   System prompt                                                                              │
│   system-prompt-text    7 lines                                                              │
│   system-prompt-file                                                                         │
│   system-prompt-models  none         · ⏎ opens $EDITOR                                       │
│   context-files.enable  true                                                                 │
│   context-files.names   [AGENTS.md]                                                          │
│                                                                                              │
│   Confinement                                                                                │
│   confine-to-workspace  true         · use /confine                                          │
│   unconfined-hosts      none         · use /confine                                          │
│                                                                                              │
│   Model profile                                                                              │
│   model-profile         native       · ⏎ opens $EDITOR                                       │
│ ↑/↓ select · ⏎ edit · ⌫ reset · esc close                                                    │
╰──────────────────────────────────────────────────────────────────────────────────────────────╯
```

The `Description:` header is a **fixed two-line region** with a blank line closing it, so walking the
list moves the highlight and nothing else; a longer description loses its tail to an `…` rather than
the list losing a row. Rows are four cells — key, value, an `(env)`/`(flag)` mark, and the note or
pointer — and the last two columns collapse away entirely on a configuration with nothing overridden
and nothing read-only.

### An edited row

```
│ ❯ context-files.enable  false *      · applies at next clear                                 │
```

The ` *` says *this session changed this key here* and is cleared only by a relaunch. The note beside
it is the one deferral wording the surface has: `context-files:` is part of the prefix every request
is cached against, so it lands at the next `/clear`. Every other key is already in force by the time
the row repaints. Where an environment variable or a flag outranks the file the note reads
`· APOGEE_MODE outranks at next launch` — about the next start, not about this edit, which applied —
and a write whose apply then failed reads `✗ saved — live apply failed: …`.

### The selection popup (`mode`, `server`, every 3-plus-option key)

```
╭──────────────────────────────────────────────────────────────────────────────────────────────╮
│ Settings                                                                                     │
│ mode — How much the agent may do without asking.                                             │
│ · plan                                                                                       │
│ ❯ ask-before   (current)                                                                     │
│ · allow-edits                                                                                │
│ · auto                                                                                       │
│ ↑/↓ select · ⏎ set · esc back                                                                │
╰──────────────────────────────────────────────────────────────────────────────────────────────╯
```

The sub-list opens **on the value the key already holds**, so pressing `⏎` twice confirms rather than
silently changes. The rows it does not use go back to the transcript while the question is open. The
`server` row's vocabulary is this config's own `servers:` block, and committing it performs the full
live switch — the same move `/server` makes. Confirming the server the session is **already on**
switches nothing, and the row says so — `· already on macStudio` — because the answer `/server` gives
in the transcript is behind this pane.

The `server` row is also the one row **`⌫` does nothing on**, and its hint line reads
`↑/↓ select · ⏎ edit · esc close` to say so. `server:` is not a value the pane writes but the
*recording* of a switch (ADR 0036 decision 2), and the only door onto it is the switch itself
(ADR 0037 decision 5): deleting the line would leave the session running against a server the file no
longer names. Choosing a different server is how this key changes.

### The single-line field (string and int keys)

```
│ ❯ llama-launcher        http://192.168.64.1:7331/mcp▏                                        │
```

The field opens **in place**, on the row and in the value's own column, seeded with what the key
holds. It is a real editor: cursor keys, `home`/`end` and word jumps move the caret, and the mouse
seats it and drags a selection exactly as in the prompt box. A paste lands **in the field** — never
in the chat box the pane is drawn over — folded onto the one line the row can show, each newline
becoming the space that stood where the break was. The hint reads `⏎ save · esc cancel`.

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
A paste lands here with its lines intact, which is what this field is for.
