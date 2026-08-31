# In-chat commands, skills, and file references

Typing `/` in the prompt opens **one menu of commands and skills**; `@` completes a
workspace file path, and an `@path` in a message hands that file to the model. A path
containing spaces is written quoted — `@"docs/my plan.md"` (single quotes are accepted
too) — and the autocomplete keeps completing across the spaces and inserts the quotes
for you.

Both work **anywhere in the line and at any time**: the menu completes the token your
cursor is on, so you can start typing a message and reach for a command halfway
through, or go back and fix a misspelled name. Accepting a command from the menu
**runs it and keeps the rest of your draft** — unless the command takes arguments, in
which case the menu completes it to `/command ` and waits for you to type them; `/model`
and `/server` are the exception to that exception and run straight away, since bare
they only open a picker. The menu stays open while the model is working, too —
commands that need a quiet engine wear an `— idle only` tag for as long as the engine
is busy, and say so if you pick one anyway, while `/version`, `/skills`, `/usage`,
`/inspect`, `/effort`, `/schedule`, `/schedule-stop` and `/confine`'s status report answer
immediately. Once the engine is idle that tag is gone from the menu entirely — there
is nothing left for it to warn about. A token
lights up in the box exactly when it resolves — the `skill` role for a skill your catalog
has, the `file-ref` role for a file your workspace has (violet and green under `dark`) — so
a typo is visible before you send.

| What you type | Does | While the model works |
|---|---|---|
| `/<skill-id>` | Invoke a skill — type its id anywhere in your message | ✅ rides the queued message |
| `@<path>` | Hand a workspace file to the model | ✅ rides the queued message |
| `/skills` | List the discovered skills — id, name, summary, any declared `triggers:`, and where each came from | ✅ |
| `/version` | Show the apogee version | ✅ |
| `/usage` | What this session has spent — one row for the main agent, one per sub-agent, and a session total; a `cached` column joins them when the server reports how much of a prompt it answered from its own cache | ✅ |
| `/inspect` | The raw request and response traffic of the recent model calls — armed by `ui.inspector` (off by default) | ✅ |
| `/confine` | Report or change Auto's blast radius — see [below](configuration.md#auto-modes-blast-radius) | ✅ report only |
| `/effort` | Set how hard the model thinks this session — opens a picker of the levels this model supports, plus `auto` (back to the profile); the resolved effort reads in the footer, and the command is hidden when the model reports no dial — see [below](configuration.md) | ✅ |
| `/schedule` | Run a prompt on a cycle — bare lists what is live, `/schedule <prompt>` asks for the cycle and mode, `/schedule <cycle> [auto] <prompt>` creates one outright | ✅ |
| `/schedule-stop` | Take a schedule off the clock — the only one straight away, a picker when several are live | ✅ |
| `/clear` (or `/new`) | Close this session into history and start a fresh one | — |
| `/compact` | Summarise the conversation to reclaim context | — |
| `/continue` | Ask the model to keep going | — |
| `/undo` | Put back the files the agent wrote in the last exchange — bare previews it, `/undo confirm` applies it — see [below](#undoing-the-agents-file-writes--undo) | — |
| `/sessions` | Browse saved sessions — resume, rename, or delete | — |
| `/rename` | Rename this session — `/rename <name>` sets it, bare `/rename` asks the model for one | — |
| `/model` | Switch model — the Launch profiles [llama-launcher](configuration.md#local-servers--llama-launcher) defines when one is configured, what this server serves when not; picker, or `/model <name>` | — |
| `/server` | Move this session to another server you configured — picker, or `/server <name>` | — |
| `/unload-model` | Free the model of the server this session is on — see [below](configuration.md#local-servers--llama-launcher) | — |
| `/stop-server` | Stop the server this session is on — see [below](configuration.md#local-servers--llama-launcher) | — |
| `/color-scheme` | Recolour the screen — bare lists what you can switch to, `/color-scheme <name>` switches and saves, `/color-scheme export <name>` writes an editable copy of a built-in to `~/.apogee/schemes/` | — |
| `/settings` | Browse and change every setting, live — see [below](#the-settings-screen--settings) | — |

A lone `/word` that names neither a command nor a skill is **not** sent to the model:
apogee says `unknown command or skill: /…` and leaves your line in the box to fix.
Anywhere else in a message a slash is just text, so paths like `/usr/bin` travel
untouched.

An `@` reference hands the model what the file **says**, not its bytes. A PDF is read for
its text — page by page, with a `[Page N]` marker before each — and the header above it
says as much: `(PDF, 27 pages; extracted text, read-only)`, so the model knows it is
looking at a transcription and not at something it can edit in place. A scanned PDF has no
text to read: apogee says so, sends your message without that reference, and the turn goes
ahead — ask for a text version of that one. A very large reference is not dropped either.
The model is shown its head and its tail with a note in between saying the middle was cut
to fit the context budget, and it can pull back the parts it needs with `read_file` on the
same path. Several references in one message share that room between them, so a message
full of files still fits.

The keys are few, and the empty prompt box advertises them: `⏎` sends — *queues*, while
the model works — `⇧⏎`/`⌥⏎` opens a new line, `↑`/`↓` walk back and forward through the
prompts you have already sent in this workspace, `esc` twice stops a run, `⌃c` quits.
Stopping is a double-tap, like quitting: the first `esc` arms the gesture for one second —
the status line says `press esc again to stop` for as long as it is armed — and a second
`esc` inside that window stops the run. Let the window lapse and the gesture disarms
itself, so a stray `esc` never kills a turn that is under way; a run that ends on its
own inside the window disarms it too, so the hint never outlives what it offered to
stop. The box
advertises `⇧⏎` only on terminals that negotiated the enhanced (kitty) keyboard
protocol — the thing that makes that chord arrive as anything other than a plain `⏎`;
everywhere else the legend names `⌥⏎` alone, which works on every terminal. Beyond
the box, `⇧⇥` cycles the autonomy mode — Plan → Ask-Before → Allow-Edits → Auto — at
any time, mid-run included, and `PgUp`/`PgDn` scroll the transcript. When a mode gates a
call, the approval prompt's decision keys — `a`, `s`, `d`, and the `⏎` that takes the
highlighted row — take effect a moment after the prompt appears, so a keystroke already
in flight cannot answer a call you have not read; `esc` is live from the instant the
prompt is up, and stops the run on the second press within the window, exactly as it does
anywhere else while the model works — the pane's own `[esc]` Cancel row is the one-press
spelling of the same stop. `⌥↑`/`⌥↓` light a
bar on the transcript and hand the arrows to it: `↑`/`↓` walk from one foldable block to
the next — a tool call, a group member, a type row — `⏎` opens or closes the one under
the bar, and `esc`, or simply typing your next message, gives the keys back. `⏎` on a
**sub-agent** does something else: it opens that delegation's **run view**, which gives the
whole transcript area over to that one run — its task at the top, its own tool calls and
its answer below, following its latest line as it works. A click on the run's row opens the
same thing. The first row of the view is the way back: `← main › scout`, and `esc` — or a
click on that row — goes one level up, one press per level. While a view is open the status
line says `esc back` in place of the stop hint, because stopping is the whole run's and
belongs to the top level: back out first, then `esc` twice. Inside the view of a run that is
still working the prompt box addresses **that sub-agent** — the box reads
`Message scout…` and `⏎` sends your message to the delegate, which picks it up between its
own steps, exactly as a message to the main agent is picked up between its. A run that has
already finished (or has not started yet) opens read-only and says so in the box. Nothing
else changes: the sub-agent keeps the tools, the mode and the confinement it was given, and
a message to it never widens any of that. `⌃l` is the
readline redraw: it forces a full repaint, which is the way back from a terminal that
has smeared or eaten part of the frame. It sends nothing, edits nothing and interrupts
nothing — the only thing it takes with it is a mouse drag-selection's highlight, which
every keypress drops.

## Suggested skills

A library you cannot recall is a library you do not use, so apogee ranks your skills against the
message you are typing and names the closest few in **one row directly above the input box**:

```
  ✦ skills: /grill-me · /code-audit · /handoff   tab to pick
```

At most three, each written as the `/id` that invokes it. The row appears as soon as the draft holds
enough words for the ranking to mean anything — one or two words get no row rather than a guess —
and it goes again the moment the draft no longer does.

The band is apogee talking to **you**, never to the model. The ranking runs here, over the catalog
apogee already loaded; no part of your library travels with a request, and a skill becomes prompt
text only when you put its `/id` in a message, exactly as before
([ADR 0061](../adr/0061-skill-suggestions-are-driver-side-over-an-engine-matcher.md)). A message
sent with the row on screen is sent unchanged — the band never takes `⏎`.

**`⇥` picks one.** With the row showing and no `/` or `@` menu open, Tab opens the menu you already
know, filtered to exactly those skills, titled `suggested skills`, top match highlighted. `⏎` (or
`⇥` again) takes the highlighted row and **inserts** its `/id ` where the cursor stands, leaving the
rest of the draft untouched on both sides; `esc` closes the menu, and so does typing the next
character. With nothing suggested, Tab keeps whatever meaning it has today.

**Each skill is offered once.** The skills on the row when a message goes out — a plain send, or a
message staged while the model works — are spent for the rest of the session and are not suggested
again; one the draft already invokes is never suggested at all. `/clear` (or `/new`) starts a new
session and a clean slate. The band is `ui.skill-suggestions:`, on by default and switchable live
from `/settings` — see [Skill suggestions](configuration.md#skill-suggestions--uiskill-suggestions).
Off, the row never paints and Tab stays inert.

**What a skill author can steer.** The ranking reads a skill's id, display name, summary and its
optional `triggers:` — a top-level frontmatter list of the phrases the author expects in a prompt
this skill fits. A body is never read:

```yaml
triggers:
  - cut a release
  - publish to homebrew
```

A comma-separated string does the same (`triggers: cut a release, publish to homebrew`), and
`/skills` lists back what each skill declared under its row. Authoring advice is in
[configuration](configuration.md#skill-suggestions--uiskill-suggestions).

## `{{SKILL_DIR}}` in skill bodies

A skill's SKILL.md may write the literal token `{{SKILL_DIR}}` anywhere in its body; when
the skill is attached, apogee replaces every occurrence with the skill's **directory
address** — the folder holding the SKILL.md and the files bundled beside it. That lets a
skill's instructions name exact paths ("read `{{SKILL_DIR}}/prompts/recon.md`") instead of
asking the model to find the folder first. The expansion happens only when apogee knows the
skill's directory — a skill resolved without one keeps the token as written. Other hosts
leave the token literal too, so a skill meant to travel should not lean on it exclusively:
phrase the surrounding text so it still reads sensibly unexpanded.

For a skill on disk that address is the absolute path of its folder. For one apogee
[ships](configuration.md#skills-apogee-ships--use-shipped-skills) there is no folder on your
machine, so the address is `shipped:<id>` — `shipped:debugging/checklist.md` names a file
inside the binary. Both spellings work in `read_file`, `list_dir`, `grep`, `find_files` and as
the **source** of a `copy_file`, which is how you take a bundled file out into your project.
Neither is writable: `shipped:` is refused by every write, and the skill folders on disk are
mounted read-only.

## Undoing the agent's file writes — `/undo`

`/undo` takes back the files the agent wrote, **one exchange at a time** — one instruction
you gave, however many tool calls it took, sub-agents included. Bare `/undo` only
**previews**: it names the exchange and every file the revert would touch, at its full
resolved path, each marked *restore*, *delete* (the agent created it, so putting things
back means removing it) or *skip*; `/undo confirm` then applies exactly the step you just
read, and anything else leaves your files alone. Repeat it to walk further back. A file
you edited yourself after the agent wrote it is **skipped**, not overwritten — your edit
wins, the rest of the exchange is still put back, and the note says which files were left
and why. There is no redo.

Two limits are worth knowing before you rely on it. The journal is **memory, not storage**:
it starts empty each time apogee launches, so a resumed session cannot put back writes made
before that run — `/undo` says so rather than pretending there was nothing to undo. And it
covers only the writes apogee's own file tools make (`write_file`, the edit and
find-and-replace verbs, `copy_file`, `move_file`, `delete_file`). Everything else that can
change your workspace is **not** undone: whatever a `terminal`, Python or test-runner
command wrote, git working-tree changes from a branch checkout, and writes by MCP servers
or other tools an embedder added. `/undo` is idle-only — it waits until the model has
finished.

## The settings screen — `/settings`

`/settings` opens a **full-height pane** over your whole configuration: one row per setting,
in the order the starter `config.yaml` documents them and grouped under section headings,
each row showing the value **this run resolved** for it. Where a higher-precedence source
beat the file, the row says which — `(env)` or `(flag)` — so a key that reads one way in the
file and another on screen explains itself. Two rows answer from the **running session**
instead of that resolution — `mode:` and `confine-to-workspace:` show what apogee is running
right now, so a `shift+tab` or a `/confine off` shows up the next time you open the pane, and
the `mode:` list opens on the rung you are actually on. The conversation gives way
entirely while the pane is up, because thirty-odd keys are a screen to read rather
than a choice to scan: `↑/↓` move the `❯`, a fixed two-line `Description:` header above the
list says what the key under the cursor is for, and `esc` closes the pane and hands the
transcript back. Section labels stand in white above the rows they open, the row being typed
into is lit, and the mouse works where the keys do — a click selects a row, the wheel walks
the list one row per notch. It needs a quiet engine, so it is **idle only**.

**Editing writes one key, when you ask.** `⏎` on a true/false row toggles it, `⏎` on a row
with a fixed set of values — `mode:`, `server:` — opens that list to pick from, `⏎` on a
string or a number opens a buffer on the row itself, and `⏎` on the inline system prompt
opens a multi-line field over the list, where `⏎` makes a new line, `ctrl+s` saves and `esc`
discards. A buffer is a real field: the arrow keys, `home`/`end` and word jumps move the
caret, and the mouse seats it and drags a selection exactly as it does in the prompt box. Each
committed edit is spliced straight into `~/.apogee/config.yaml` — your
comments, your layout and every other key untouched, the result re-parsed and compared
against the original before it replaces the file — and a key that was still one of the
commented examples lands directly below it. A value the key cannot hold is refused before
anything is written, with the reason on the row and your text still in the buffer. Nothing
else is ever written: apogee still makes no edit to that file you did not ask for.

**And what is saved is applied — to the session you are in.** The `⏎` that persists a key
also puts it into effect, so no setting waits for a restart: change `mode:`, `bypass:`, a
mechanism switch, the web-search endpoint, the presentation keys or the model profile and the
next thing apogee does uses it. The row keeps a ` *` after its value — `false *` — which says
*you changed this here, this session*; it is cleared only by a relaunch. A ` ~` in that same
place — `false ~` — says the other thing: *a save of the config file moved this key under this
session*, with no keypress here at all, and whichever of the two happened last is the one the row
wears. A save that moved anything also leaves one line in the transcript naming the keys it
applied (`config changed on disk — applied: ui.spinner, auto-title`), because the pane is very
likely not open when it happens; a re-read that found nothing changed says nothing. One pair
lands at a boundary the session crosses anyway rather than mid-conversation, and says so on
the row: the `context-files:` keys are part of the prefix every request is cached against, so they take
effect at the next `/clear` — `· applies at next clear`. On a key an environment variable or
a flag is overriding, the edit still applies and is still written, and the row adds that the
override will win again the next time apogee starts — startup precedence is unchanged. If a
write lands but the live apply refuses it, the row says exactly that
(`saved — live apply failed: …`) rather than leaving you to guess which half happened.

**`backspace` unsets.** On a row you have set, `backspace` arms a reset, the hint line asks
for a confirming `⏎`, and what that sends **removes the key's line** from the file rather than
writing today's default into it — so the key goes back to following the built-in default
instead of being pinned to a copy of it. The default is applied on the same keypress, and the
row reports it with the same marker: `default *`.

**The blocks no row can hold open your editor.** `servers:`, `mcp-servers:`,
`validated-sets: alias:`, `system-prompt-models:` and the model profile render as a summary with an
`· ⏎ opens $EDITOR` pointer, and that is what `⏎` does — in the editor the
[four-rung ladder](configuration.md) `editor:` heads, with the cursor on that key's line where the
editor takes a line argument. A **terminal** editor (`vi`, `vim`, `nvim`, `nano`, `pico`,
`emacs`, `micro`, `hx`, `kak`) has to own the terminal, so apogee suspends into it and re-reads
the file when it exits; a non-zero exit (`:cq`) discards that re-read. Anything else — a GUI
editor, your desktop's opener — is started **detached**: the pane stays up, nothing waits on the
window that opened somewhere else, and the row says `· opened in your editor`. Either way the
edit lands the same way, because what applies it is the file being **saved**, not the editor
exiting. Every key that changed is applied the way an in-pane edit is — a changed `mcp-servers:`
**reconnects**, connecting the new set first and swapping the tools over only once it is up, so a
server that will not come back leaves the old connections serving and the reason on the row; a
changed `model-profiles:` swaps the parser and re-composes the tool set under the bound model's
`tools:` roster. The jump is offered between runs only — mid-run the
row asks you to wait, while in-pane edits stay open. The confinement keys are the one pair that
goes nowhere near it: they carry `· use /confine`, because switching Auto's fence off asks for an
acknowledgement that belongs with [that verb](configuration.md#auto-modes-blast-radius). And the `server:` row
**moves the session** — the same switch `/server` performs, chosen from the same list, recorded
the same way.

**`mechanisms:` is the one block the pane opens itself.** `⏎` on that row opens a list of every
catalogued mechanism with `on`/`off` beside each; `⏎` or `space` flips the highlighted one, writing
and applying it on that keypress, and the list **stays open** so a posture is set in one visit. `esc`
goes back. Switching one off writes `<id>: false` rather than deleting the line, and — as ever — a
non-empty `mechanisms:` block means manual control, so the Validated set measured for the bound model
is no longer applied on top.

