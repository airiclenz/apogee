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
which case the menu completes it to `/command ` and waits for you to type them; `/model`,
`/server`, `/sub-agents-server` and `/skills` are the exception to that exception and run straight
away, since bare they only open a picker or print a report. The menu stays open while the model is working, too —
commands that need a quiet engine wear a `— runs at idle` tag for as long as the engine
is busy, and picking one anyway **queues** it — it shows as a `queued command: /…` row above
the box and runs as soon as the model is done — while `/version`, `/help`, `/usage`,
`/inspect`, `/thinking`, `/advice`, `/effort`, `/schedule`, `/schedule-stop`, `/sub-agents-server`, `/skills`' listing and
`/confine`'s status report answer immediately. Once the engine is idle that tag is gone from the menu entirely — there
is nothing left for it to warn about. A token
lights up in the box exactly when it resolves — the `skill` role for a skill your catalog
has, the `file-ref` role for a file your workspace has (violet and green under `dark`) — so
a typo is visible before you send.

| What you type | Does | While the model works (✅ runs now; ⧖ queued, runs at idle) |
|---|---|---|
| `/<skill-id>` | Invoke a skill — type its id anywhere in your message | ✅ rides the queued message |
| `@<path>` | Hand a workspace file to the model | ✅ rides the queued message |
| `/skills` | List the discovered skills — id, name, summary, any declared `triggers:`, and where each came from; `/skills export <id>` copies a skill apogee [ships](configuration.md#skills-apogee-ships--use-shipped-skills) into `~/.apogee/skills/<id>/` so you can edit it | ✅ listing only |
| `/version` | Show the apogee version | ✅ |
| `/help` | List every command with its one-line summary, then the key legend — `⏎ send`, the newline chord your terminal delivers (`⌥⏎`, or `⇧⏎/⌥⏎` once the enhanced keyboard protocol is negotiated), `↑/↓ recall`, `esc×2 stop`, `⌃c quit`, `⇧⇥ mode`, `PgUp/PgDn scroll` — as a transcript note | ✅ |
| `/usage` | What this session has spent — one row for the main agent, one per sub-agent, and a session total; a `cached` column joins them when the server reports how much of a prompt it answered from its own cache, and a `served:` line above the rows names the models the server actually answered with once a reply has carried one | ✅ |
| `/inspect` | The request and response traffic of the recent model calls, **readable** by default — each request summarised as `N messages · N tools · model …` (`system + N messages` when the wire hoists the system prompt), each response as the passages its stream spells, thinking and reply as wrapped prose and every tool call named — on the anthropic wire also the served model, the stop reason and the token counts, which arrive as events of their own; `ctrl+r` flips the pane to the raw pretty-printed protocol and back. It opens on the newest record and follows it, so traffic arriving while the pane is open is shown until you scroll up off the end. With a sub-agent's run view open the pane shows that run's traffic alone and names it in its title — close the view for the whole ring. Armed by `ui.inspector` (off by default) | ✅ |
| `/thinking` | The model's thinking as plain text — the reasoning it streams beside its answer, one record per completed turn, newest last, with no protocol and no prefixes. Opens on the newest record and follows it, so reasoning arriving while the pane is open is shown until you scroll up off the end; with a sub-agent's run view open it shows that run's thinking alone and names it in its title, and at the top level the main agent's alone. Always recorded, nothing to arm, nothing saved with the session | ✅ |
| `/advice` | What advice the model saw, by Turn — every `advise:` reaction's fenced text and the context-fill notice's rung the engine handed the model inside a tool result, which the transcript never shows. The whole session, every run's firings in arrival order, grouped under a `turn N` heading (`<run label> · turn N` for a sub-agent's), each firing named `<reaction> (<origin> origin) @ <moment>` with its text below; the newest 256 firings are kept. Opens on the newest firing and follows it, so advice arriving while the pane is open is shown until you scroll up off the end; a second `/advice` re-opens it on the newest rather than closing it — `esc` or a click outside closes. Engine notes (the step and token-budget notices, the delegations ledger) fire no reaction and are not shown — `/inspect` has the bytes. Always recorded, nothing to arm, nothing saved with the session | ✅ |
| `/confine` | Report or change Auto's blast radius — `/confine [status]`, `/confine off [--save]`, `/confine on`; `--save` records the choice for later sessions too — see [below](configuration.md#auto-modes-blast-radius) | ✅ report only |
| `/effort` | Set how hard the model thinks this session — opens a picker of the levels this model supports, plus `auto` (back to the profile); the resolved effort reads in the footer, and the command is hidden when the model reports no dial — see [below](configuration.md) | ✅ |
| `/schedule` | Run a prompt on a cycle — bare lists what is live, `/schedule <prompt>` asks for the cycle and mode, `/schedule <cycle> [auto] <prompt>` creates one outright. A firing that comes due while the footer says the server is **not there at all** — the dial refused, the name unresolvable, the connection timed out — is refused before the prompt is sent, with the same sentence a send earns: `cannot send — server offline (<endpoint>)`, no record written and no tokens spent; a server that answers anything at all still runs | ✅ |
| `/schedule-stop` | Take a schedule off the clock — the only one straight away, a picker when several are live | ✅ |
| `/clear` (or `/new`) | Close this session into history and start a fresh one | ⧖ |
| `/compact` | Summarise the conversation to reclaim context | ⧖ |
| `/continue` | Ask the model to keep going | ⧖ |
| `/undo` | Put back what the last exchange changed — every write to your workspace, apogee's own file tools and a `terminal`, Python or MCP write alike; bare previews it, `/undo confirm` applies it, and the record survives a relaunch — see [below](#undoing-the-agents-file-writes--undo-and-redo) | ⧖ |
| `/redo` | Put back what the last `/undo confirm` took away — same two steps, bare previews, `/redo confirm` applies it — see [below](#undoing-the-agents-file-writes--undo-and-redo) | ⧖ |
| `/sessions` | Browse saved sessions — resume, rename, or delete | ⧖ |
| `/fork` | Branch a new session from one of this session's prompts — a picker lists them, ⏎ keeps the history through the chosen one and switches to the new session (the one you were in stays saved, and the browser tags the fork with its parent) | ⧖ |
| `/rename` | Rename this session — `/rename <name>` sets it, bare `/rename` asks the model for one | ⧖ |
| `/model` | Switch model — the Launch profiles [llama-launcher](configuration.md#local-servers--llama-launcher) defines when one is configured, what this server serves when not; picker, or `/model <name>` | ⧖ |
| `/server` | Move this session to another server you configured — picker, or `/server <name>` | ⧖ |
| `/sub-agents-server` | Pick the `servers:` entry this session's delegations run on — picker, or `/sub-agents-server <name>`; the choice is recorded and the next delegation goes there. Each row names that entry's endpoint and, where it has one, its `description:`. The picker's last row is `auto`, and taking it — or `/sub-agents-server auto` — clears the recorded key and runs delegations on this session's own server — see [below](configuration.md#the-servers-you-run-models-on) | ✅ |
| `/unload-model` | Free the model of the server this session is on — see [below](configuration.md#local-servers--llama-launcher) | ⧖ |
| `/stop-server` | Stop the server this session is on — see [below](configuration.md#local-servers--llama-launcher) | ⧖ |
| `/color-scheme` | Recolour the screen — bare lists what you can switch to, `/color-scheme <name>` switches and saves, `/color-scheme export <name>` writes an editable copy of a built-in to `~/.apogee/schemes/` | ⧖ |
| `/settings` | Browse and change every setting, live — see [below](#the-settings-screen--settings) | ⧖ |

A lone `/word` that names neither a command nor a skill is **not** sent to the model:
apogee says `unknown command or skill: /… — nothing sent` and leaves your line in the box to fix.
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
full of files still fits — and no single reference ever takes more than 32k tokens, however
large the window, so a megabyte file cannot spend the context on one message. When a
reference is clipped you see a note (`@notes.md clipped to 32k tokens — read_file ranges for
the rest`), not an error: the message went ahead. A PDF's text is joined back into sentences
where the document broke a line at every word, so the model reads prose rather than a
column of words; paragraph breaks are kept.

The keys are few, and the empty prompt box advertises them: `⏎` sends — *queues*, while
the model works, and a queued message does not wait for sub-agents that have not started yet:
those are skipped, the model is told so, and your message lands once the running ones finish.
A command that needs a quiet engine queues the same way — `⏎` on `/clear` mid-run stages a
`queued command: /clear` row above the box, below any queued messages, and the queued commands
run in the order you typed them the moment the model is idle, **before** any queued message is
sent, so a `/clear` typed ahead of a message clears first. `⌫` on an empty box takes the newest
row back into the editor — a queued command first, then a queued message. Stopping the run does
not drop a queued command: it runs at that idle, while queued messages are held for your next
`⏎` — `⇧⏎`/`⌥⏎` opens a new line, `↑`/`↓` walk back and forward through the
prompts you have already sent in this workspace, `esc` twice stops a run, `⌃c` quits.
Stopping is a double-tap, like quitting: the first `esc` arms the gesture for one second —
the status line says `press esc again to stop` for as long as it is armed — and a second
`esc` inside that window stops the run. Let the window lapse and the gesture disarms
itself, so a stray `esc` never kills a turn that is under way; a run that ends on its
own inside the window disarms it too, so the hint never outlives what it offered to
stop. A stop keeps what the run had finished: the steps completed before it — the tool
calls and their results — stay in the conversation and in the saved session, only the step
under way is dropped, and the model is told at its next request that you stopped the run
there, so it neither redoes that work nor mistakes its silence for an answer. A stop that
finds nothing finished — the model had not completed a single step — leaves nothing behind
instead: the prompt itself comes back out of the conversation, as if it had never been sent.
Only `/clear` throws a stopped exchange away. The box
advertises `⇧⏎` only on terminals that negotiated the enhanced (kitty) keyboard
protocol — the thing that makes that chord arrive as anything other than a plain `⏎`;
everywhere else the legend names `⌥⏎` alone, which works on every terminal. Beyond
the box, `⇧⇥` cycles the autonomy mode — Plan (read-only, except for the session's own
scratch directory) → Ask-Before → Allow-Edits → Auto — at any time, mid-run included, and `PgUp`/`PgDn` scroll the transcript. Clicking the mode
marker in the footer — the glyph, the word, and on Auto its blast-radius word, all one
target — opens a picker listing the four rungs, so you can name the one you want instead
of cycling to it; the picker takes the rung through exactly the path `⇧⇥` does. The
marker answers a click only when the picker could actually be answered — not while
another picker, the `/sessions` browser or the `/settings` screen is up, and not while a
call is awaiting approval or a question from the model is up — and `⇧⇥` stays the route that works in every state. When a mode gates a
call, the approval prompt's decision keys — `a`, `s`, `d`, and the `⏎` that takes the
highlighted row — take effect a moment after the prompt appears, so a keystroke already
in flight cannot answer a call you have not read; `esc` is live from the instant the
prompt is up, and stops the run on the second press within the window, exactly as it does
anywhere else while the model works — the pane's own `[esc]` Cancel row is the one-press
spelling of the same stop. A question from the model is the one carve-out: there a single `esc`
cancels — the pane's hint reads `esc cancel` — because backing out of a question is not
abandoning a turn you lost track of; and on a multi-select question `space` ticks and un-ticks
the highlighted row. `⌥↑`/`⌥↓` light a
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
a message to it never widens any of that. When a reply asked for more delegations than
the server's width allowed ([several sub-agents at
once](configuration.md#the-servers-you-run-models-on)), the last result of that group
carries one extra line for the parent model — `[1 of this group's 3 delegations ran
after the others finished — the width is 2]` — saying the results did not all arrive at
once and what the width was; the delegations' own rows and run views are unchanged. `⌃l` is the
readline redraw: it forces a full repaint, which is the way back from a terminal that
has smeared or eaten part of the frame. It sends nothing, edits nothing and interrupts
nothing — the only thing it takes with it is a mouse drag-selection's highlight, which
every keypress drops.

## The status line — what a live run reports

The dark row just above the prompt box is the status line, and while the model works its left
slot answers one question: what is happening *right now*. It reads as an activity phrase and an
elapsed clock — `thinking · 4s`, and past a minute `thinking · 1m 12s`. Seven phrases cover
everything a run does: **thinking** while a request is in flight or reasoning is arriving,
**responding** while the visible reply streams, the **tool's own label** while a call is open
(`reading`, `writing`, `searching`, `running` — the verb alone, since the tool-call block a line
above already names what it is working on), **retrying** when the turn is being re-streamed from
the top, **compacting** while `/compact` folds the conversation, **stopping** between the second
`esc` and the worker actually unwinding, and **working**, which belongs to delegations.

With delegations running the row is **theirs, not the parent's** — a parent sitting inside a
delegation is doing nothing of its own to report. Exactly one live delegate keeps its own phrase
under its own name: `repo-scout · reading`. Two or more merge into a single count,
`2 sub-agents · working`, because the row has space for one sentence and naming whichever
delegate spoke last only made it flicker. That merged clock counts from the **oldest** live
child, so the number you are reading never restarts when a sibling emits. Open a delegation's
run view and the row speaks for that one run again, as above.

After the clock comes the throughput readout, `· N tok/s`: the last completion's server-reported
token count over that turn's generation window, shown as a whole number. Below one token per
second it is hidden entirely — which is also how an unmeasurably short window reads. Such a
window is dominated by scheduling jitter rather than by generation, so it is shown as no reading
at all rather than as an invented one.

A run that goes silent for longer than `ui.stall-after` adds a `quiet` qualifier to whatever
phrase the row is holding; see [the terminal UI](configuration.md#the-terminal-ui--ui).

## Approving a call — how far an approval reaches

An ordinary gated call offers the same four rows — `Allow`, `Always allow this session`, `Deny` and
`Cancel` — and the decision keys behind them arm a moment after the prompt appears, exactly as the
keys above describe. What that second row actually remembers is worth knowing before you press `s`
— and which prompts never offer it at all (the last paragraph of this section).

**The mouse answers it too, in two clicks.** A click on one of the four rows moves the `❯` onto it,
the way `↑`/`↓` do; a **second** click on that same row takes it, the way `⏎` does. It is always two,
and the row a second click can take is the row *you* clicked onto — the `Allow` the prompt opens on
is never one press away from being granted, and the arming delay gates the deciding click exactly as
it gates `⏎`. The same two-click rule runs every other pane that asks you something: the `/sessions`
browser, the `/model` and `/server` pickers, a question from the model, and the `/` | `@` menu, where
the second click completes the token the way `⇥` does. A click **outside** one of these boxes never
answers or cancels it — that stays `esc`'s job. It closes the pickers and the browser, closes the `/`
menu, and leaves a question or an approval standing, so while one is up you can still click into the
prompt or drag a selection out of the transcript behind it.

**It is scoped to the call, not the tool.** apogee remembers the answer under the tool's name *plus a
digest of that call's arguments*, so it authorises the call you actually read and nothing wider:
allowing `npm test` leaves `npm run build` still asking. That is deliberate. Keying on the tool name
alone let one answer stand for every later call of the same tool — an "always allow" on `terminal`
pre-cleared every shell command for the rest of the session — so the arguments are part of the
identity now, and the price is a prompt you will sometimes answer twice for what looks like one tool.

**It is honoured across the whole sub-agent tree.** There is one such memory per agent tree, hanging
off the approver a parent and all its delegates share, so an allow granted inside a sub-agent clears
that same call for its parent and its siblings, and outlives the child that earned it. The memory
lives in the process and is never written to disk — it survives a `/clear`, but a session you resume
from the store starts with an empty memory of its own. The direction that costs you is always the
safe one: a prompt too many, never an unapproved call.

**An MCP grant is server-grain.** Approving one tool of an [MCP
server](configuration.md#external-mcp-servers--mcp-servers) clears its siblings on that same server
for the rest of the session — the server, not the tool and not the arguments, is the grain an
external tool surface is granted at. The pane discloses that on the frame, in its own words, wherever
it applies:

```
Note: "Always allow" covers every tool of MCP server "github" for this session
```

**A forced look offers no `Always allow`.** A prompt the [dangerous-action
guard](configuration.md#the-dangerous-action-guard) forced, a runtime demote, or a gate a Reaction
asked for is remembered nowhere — a yes authorises that one call — so the pane paints three rows,
`Allow`, `Deny` and `Cancel`, with no `s` behind them, and closes on one faint line under the menu:
`a forced look is asked every time`. The next call that trips the same rule asks again.

## Reading a tool's answer — diffs and near misses

Some tool blocks answer with a **diff**, and which ones do is a rule rather than a list: every block
whose tool recorded or printed the regions it changed paints one. That is the four writing tools —
`write_file`, `edit_existing_file`, `single_find_and_replace` and `multi_find_and_replace`, which
attach those regions as they apply the change — plus `view_diff` and `git_diff_range`, which cut
them out of the diff they printed. An overwrite or a fresh create through `write_file` therefore
reads exactly the way an edit does, and `view_diff` on a path that does not exist yet diffs against
empty, so the preview of a new file is every line added.

The same regions paint **two ways**. Given room they go side by side: before on the left with its
own line numbers, after on the right with its own. Below that room they stack instead, each region's
removed lines above its added ones in a single column. The information is identical — the same
lines, the same numbers, the same markers — and only the arrangement differs. The room is measured
per pane: the two panes appear when each one can still give the code about 40 columns after its
number gutter and its marker, which in practice means a terminal around 100 columns wide. The
question is asked again at every paint, of *this* body at *this* width — a diff deep in a long file
spends more of the row on its line numbers than a diff near the top of a short one — so two diffs on
the same screen can genuinely read differently, and widening or narrowing the terminal can flip a
block from one reading to the other under you. What never happens is a mixture: a body paints whole
in one reading or whole in the other, never half of each.

Removed rows wear `-` on a red band, added rows `+` on a turquoise band. Turquoise rather than green
is deliberate — paired with red it survives red-green-weak vision — and in any case the **marker is
what says which way a line went**, never the colour alone; the bands are there to find the change
quickly, not to carry it. In the side-by-side reading a line wider than its pane wraps onto further
rows rather than being clipped, so nothing is hidden off the right edge, and a continuation row
carries no number and no marker, which is how you tell it from a line of its own. The stacked
reading clips instead: a row is cut at 160 characters with a `…`, its number and marker kept. The full layout — the alignment of the two
sides, the `⋯` rule between regions that do not touch, the per-file headers a multi-file diff paints
— is specified in [`docs/layout/split-diff-layout.md`](../layout/split-diff-layout.md).

**How much a read returns.** A `read_file` call that names no end — no `end_line` and no
`max_lines` — comes back bounded: the first 400 lines, or the first 40 KiB of them, whichever runs
out first (always whole lines, never a line cut in the middle), and when that bound bit, the body
ends with a tail saying so and how to get the rest:

```
[showing lines 1-400 of 1180 — pass start_line/end_line for the rest]
```

The header names both the file's length and the lines that actually came back
(`[File: src/main.go, 1180 lines total, showing lines 1-400]`); the block's line count is the
latter, not the file's length. A `start_line` on its own is still open-ended and is bounded from where it starts, so
"the rest" is paged the same way; an explicit `end_line` or `max_lines` is honoured as written,
however large. Two ranges are refused rather than answered with nothing: an `end_line` before its
`start_line` (`read_file: end_line (2) is before start_line (3)`) and a `start_line` past the
file's last line (`read_file: start_line (500) is past the end of the file (120 lines)`). A
`locate` with no range no longer returns the whole file beneath its `Located …` line: the content
is the ten lines around each hit, windows that overlap merged, and a lone `…` line between windows
that do not meet — a term that occurs nowhere renders the bounded body as a plain read would. Those
windows are bounded the same way: as many as fit the 400-line / 40 KiB cap come back, in file
order and never cut in the middle, and when some were dropped the body ends with
`[showing the windows around 18 of 80 hits — pass start_line/end_line for the rest]`; a single
window that alone runs past the cap (hits every twenty lines or fewer merge into one) is cut like a
plain read, with the plain read's tail. The `Located …` line itself spells out the first 40 line
numbers and counts the rest (`… and 40 more`). A
file whose leading bytes hold a NUL — an executable, an archive, an image — is refused in one line
(`read_file: build/apogee is a binary file (10094249 bytes)`) rather than served as text; `grep`
walks past such a file on the same test. A PDF is the one exception: it is detected by its content
and returned as extracted text.

**A file at a revision reads the same way.** `git_show` reads one file as it was at a commit,
branch or tag — the committed version beside your edits, an older one, or a file a later commit
deleted — and renders it exactly as `read_file` would: the same header (`[File: README.md @ HEAD~1,
…]`), the same `start_line`/`end_line`/`max_lines`/`locate` arguments, the same 400-line default
bound and tail, and a one-line refusal of its own for a binary (`git_show: build/apogee at HEAD is
a binary file (10094249 bytes)`). A path that does not exist at that
revision is refused naming both (`git_show: cannot read docs/old.md at HEAD~5: …`). Its companions
in the read-only git set: `git_log` takes an optional `path` to list only the commits that touched
it, and `git_branch`'s list marks every remote-tracking branch ` (remote)`.

**A wide match row is clipped.** A `grep` hit on a minified bundle or a `.jsonl` record can sit on
a line thousands of characters wide, and the match is worth its neighbourhood, not the whole row:
a matched line wider than 512 characters comes back as the 512 characters around its first match,
with `…` marking each cut end, and the header counts what it clipped —
`[2 total matches in the workspace, showing 1-2 (1 rows clipped at 512 chars)]`. A line within the
bound is untouched, and `read_file` still serves the whole row. A search that hits the 1000-match
cap says how to get under it: its header ends `— narrow with include, exclude or path`.

**A search can name several paths, skip names and count instead of listing.** `grep` takes a
`paths` list beside the single `path` — `paths: ["src", "docs"]`, or an absolute path under a
read-only root alongside a workspace one — and searches them together: the header names each
(`[5 total matches in src, docs, showing 1-5]`), every row is prefixed with the path it came from
as it was spelled — once more than one target resolves; a `paths` list of one reads exactly as the
single form does — and a path that is refused refuses the call with the same wording the single
form gives. `exclude` is a comma-separated list of file-name or directory-name globs to skip
(`exclude: "*_test.go,vendor"`) on top of the directories a search never enters (`node_modules`,
`.git`, `dist`, `build`, `.next`, `coverage`, `__pycache__`); it narrows that one call and nothing
else. `include` stays a file-name filter, and a glob with a slash in it is refused rather than
silently matching nothing (`grep: include matches file names only — use paths or exclude for
directories`). `count_only: true` returns one `file:count` row per matching file and
`files_only: true` one path per matching file, both under a header that carries the match total
and the file count (`[12 total matches in src across 3 files, showing 1-3]`) — the page is counted
in files then, and `count_only` wins when both are set.

**A fetched page arrives as text.** `web_fetch` renders an HTML response — by its `Content-Type`,
or by its own doctype when the server sends none — as the page's readable text: `<script>`,
`<style>` and `<noscript>` bodies and comments are dropped, each paragraph, heading, list item and
table row is a line of its own, a `<pre>` keeps its lines and indentation, and every other tag
becomes a space. The status line and `Content-Type` header still come first. To see the markup
itself — to find a link's `href`, a form's fields, a meta tag — pass `raw: true`; a non-HTML body
(plain text, JSON, a raw file) is never touched either way.

**A near miss gets a suggestion.** When a tool is handed a path that is not there — the read tools
(`read_file`, `list_dir`, `grep`, `find_files`), the editing tools (`edit_existing_file`,
`single_find_and_replace`, `multi_find_and_replace`), a `copy_file` or `move_file` source,
`delete_file`, `diagnostics`, `present_document` or a `run_tests` path — the refusal does not stop
at saying so: it adds a `did you mean:` clause naming up to five entries of the named parent
directory whose names begin with the name that is missing. Matching ignores case, the suggestions
come back sorted, a directory carries a trailing `/`, several are joined with `; `, and each is
spelled onto the parent the call named, so one can be handed straight back as the next call —
`read_file` alone spells them onto the root it actually looked in, the symlink-resolved one, since
its refusal quotes the path it examined:

```
file not found: docs/adr/0025 — did you mean: docs/adr/0025-interjections-commit-at-the-between-steps-boundary.md
```

A refusal from the workspace fence — a path outside the roots apogee may read — never gains that
clause. A suggestion there would read as absence, as though the file were simply not present, and
hide the fact that the answer was *not allowed* rather than *not found*. A path under a shipped
mount (`shipped:…`) has no host directory to list, so a miss there keeps the bare refusal.

The same courtesy reaches a tool *name*: with the tool-call repair guard switched off, a call to
a tool that is not on the menu is answered `unknown tool "read_fil" — did you mean: read_file`
when a registered name begins with what was written or sits within three edits of it. Nothing is
re-routed; the model re-issues the call.

**A failed edit says nothing landed.** `single_find_and_replace` and `multi_find_and_replace` apply
their replacements atomically, and every refusal they give after reading the file — old text not
found, found more than once, a multi-edit that would push the file past the size cap — is prefixed
`no changes were written — `, so a failure at replacement #2 is never taken for a file that already
carries #1. `write_file` handed a directory answers `write_file: target is a directory: <path>`
rather than the filesystem's own rename error.

**`terminal` runs `sh`, not bash.** The command goes to the platform's POSIX shell — `dash` on
Debian and Ubuntu — so a bash-only construct (`${var//x/y}`, `<(cmd)`, `arr=(a b)`, `shopt`) fails
with the shell's own complaint, `Bad substitution` or `Syntax error: "(" unexpected`, and the failed
result gains one line above its exit code, `hint: the shell is sh, not bash`, so the model rewrites
the line rather than retrying it.

**What the read tools may read.** The roots those tools accept are the workspace, the session's
scratch directory, the skill libraries, and — when `go` is on your PATH — the Go toolchain's own two
trees: `GOROOT` and the module cache (`GOMODCACHE`). apogee asks `go env` for the two once at
start-up, in the system's temp directory rather than your project and with `GOTOOLCHAIN=local`,
`GOWORK=off` and `GOFLAGS=-mod=readonly` pinned (so neither a `go.mod` asking for a newer
toolchain nor an exported `GOTOOLCHAIN` can make the question download one, and nothing it does
edits a `go.mod`) and off the start-up path, and lists whichever of them exist
on the orientation's `Read-only library roots:` line beside the skill folders — so a model asked
about a standard-library function or a dependency's source can `read_file`, `grep`, `list_dir`,
`find_files` or `copy_file` it rather than being refused the very trees its `go build` had just
read. Both are mounted read-only; nothing under them is writable through any tool.

## Suggested skills

A library you cannot recall is a library you do not use, so apogee ranks your skills against the
message you are typing and names the skills that clearly fit, up to three, in **one row directly
above the input box**:

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
from `/settings` — see [Skill suggestions](configuration.md#the-terminal-ui--ui).
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
[configuration](configuration.md#the-terminal-ui--ui).

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
the **source** of a `copy_file`, which is how you take a bundled file out into your project —
one file at a time: directories under a shipped mount are not supported as a `copy_file` source
(a folder on disk copies whole — recursively, up to 2000 files, journalled so `/undo` takes the
copy back as one step, and with `overwrite: true` merged into an existing destination, replacing
the files it already holds under the same names and leaving the rest; `/skills export` is the way
to take a whole shipped skill out).
Neither is writable: `shipped:` is refused by every write, and the skill folders on disk are
mounted read-only. To edit a shipped skill, take a copy of it first: `/skills export <id>` writes
the whole folder — SKILL.md and everything bundled beside it — to `~/.apogee/skills/<id>/`, and
from the next scan on that copy is what `/<id>` resolves to, since your library outranks the
shipped source. It never overwrites: if the folder is already there apogee says so and changes
nothing, so a copy you have been editing is safe.

## Undoing the agent's file writes — `/undo` and `/redo`

`/undo` takes back what the agent changed, **one exchange at a time** — one instruction
you gave, however many tool calls it took, sub-agents included. Bare `/undo` only
**previews**: it names the exchange and every file the revert would touch, at its full
resolved path, each marked *restore*, *delete* (the agent created it, so putting things
back means removing it) or *skip*; `/undo confirm` then applies exactly the step you just
read, and anything else leaves your files alone. Repeat it to walk further back. A file
you edited yourself after the agent wrote it is **skipped**, not overwritten — your edit
wins, the rest of the exchange is still put back, and the note says which files were left
and why.

**`/redo` puts back what an undo took away.** It is `/undo`'s mirror in every respect —
bare previews, `/redo confirm` applies it, and a file you have edited in between is
skipped the same way. The stack holds exactly what `/undo confirm` has taken back, and
the next exchange that actually changes a file clears it: a redo reaching across newer
work would lay an old tree over the work you just asked for.

**It covers the whole workspace, not just apogee's own writes.** Around every exchange
apogee images your workspace into a small object database of that session's own under
`~/.apogee/snapshots/`, and the revert reaches every path those two images disagree on.
So a file a `terminal` command wrote, what a `python_exec` script left behind, the
working-tree changes a git checkout made and a write by an MCP server are all inside
`/undo`'s reach, alongside the writes apogee's own file tools make. Three kinds of change
are the residue: paths your workspace's own `.gitignore` excludes and anything else git's
own `add` will not take (a nested repository, a path a filter driver rewrites); a write
outside the workspace that you approved, which stays covered only for the run that made
it; and everything that never was a file — a `terminal` command that dropped a database
table dropped it for real, and no revert here brings it back.

**It survives a relaunch.** The images and the small index beside them live on disk under
the session's own id, so a session you resume can still put back what an earlier process
wrote, and an unattended run can be reverted from a fresh process with [`apogee
undo`](headless.md). The store is keyed to the workspace it imaged: resume the same
session against a different tree and apogee loads none of it and says so.

**Without the store, undo is narrower — and says which.** With
[`undo-snapshots: false`](configuration.md#keeping-the-session-store-bounded--sessions),
or on a host with no `git` on its PATH, apogee keeps the record of its own file tools'
writes that it has always kept — this process's alone, held in memory — and `/undo` still
works on those. When there is nothing to take back it names the reason in the same breath,
so the narrower answer never reads as a broken one.

Both verbs **run at idle** — typed while the model works, they queue and run once it has finished.

## The settings screen — `/settings`

`/settings` opens a **full-height pane** over your whole configuration: one row per setting,
in registry order — roughly the order the starter `config.yaml` documents them — and grouped under section headings,
each row showing the value **this run resolved** for it. Where a higher-precedence source
beat the file, the row says which — `(env)` or `(flag)` — so a key that reads one way in the
file and another on screen explains itself. Two rows answer from the **running session**
instead of that resolution — `mode:` and `confine-to-workspace:` show what apogee is running
right now — and the rows are re-derived at every paint, not read once when the pane opens, so a
`shift+tab` or a `/confine off` is already on the row whenever the pane paints, and
the `mode:` list opens on the rung you are actually on. The conversation gives way
entirely while the pane is up, because sixty-odd keys are a screen to read rather
than a choice to scan: `↑/↓` move the `❯`, a fixed two-line `Description:` header above the
list says what the key under the cursor is for, and `esc` closes the pane and hands the
transcript back. Section labels stand in white above the rows they open, the row being typed
into is lit, and the mouse works where the keys do — a click selects a row, the wheel walks
the list one row per notch. It needs a quiet engine, so typed mid-run it queues and **opens at idle**.

**Editing writes one key, when you ask.** `⏎` on a true/false row toggles it, `⏎` on a row
with a fixed set of values — such as `mode:`, `server:`, `ui.spinner:` or `cursor-shape:` — opens that list to pick from, `⏎` on a
string or a number opens a buffer on the row itself, and `⏎` on the inline system prompt
opens a multi-line field over the list, where `⏎` makes a new line, `ctrl+s` saves and `esc`
discards. A buffer is a real field: the arrow keys, `home`/`end` and word jumps move the
caret, and the mouse seats it and drags a selection exactly as it does in the prompt box. Each
committed edit is spliced straight into `~/.apogee/config.yaml` — your
comments, your layout and every other key untouched, the result re-parsed and compared
against the original before it replaces the file — and a top-level key that was still one of the
commented examples lands directly below it; a nested key joins the end of its block when the
block is there, and lands below the whole commented example block when it is not. A value the
key cannot hold is refused before anything is written, with the reason on the row and your text
still in the buffer. The file is written only on your own act — a pane edit, a verb that records
its choice such as `/server`, `/sub-agents-server` or `/color-scheme`, the click that folds or
opens the task-list card (`ui.task-list-open`) or a large Tools umbrella (`ui.tools-open`) — never
on one you did not ask for.

**And what is saved is applied — to the session you are in.** The `⏎` that persists a key
also puts it into effect, so as a rule a setting does not wait for a restart: change `mode:`, `bypass:`, the
web-search endpoint, the presentation keys or the model profile and the next thing apogee does
uses it. The row keeps a ` *` after its value — `false *` — which says
*you changed this here, this session*; it is cleared only by a relaunch. A ` ~` in that same
place — `false ~` — says the other thing: *a save of the config file moved this key under this
session*, with no keypress here at all, and whichever of the two happened last is the one the row
wears. A save that moved anything also leaves one line in the transcript naming the keys it
applied (`config changed on disk — applied: ui.spinner, auto-title`), because the pane is very
likely not open when it happens; a re-read that found nothing changed says nothing. One pair
lands at a boundary the session crosses anyway rather than mid-conversation, and says so on
the row: the `context-files:` keys are part of the prefix every request is cached against, so they take
effect at the next `/clear` — `· applies at next clear`. A few keys are read only while apogee
starts — `ui.inspector`, `undo-snapshots`, `delegate-timeout`, `sessions.max-age` and
`sessions.max-count` — so an edit there is written and takes effect at the next start; the row's
`Description:` says so, and the value cell shows what was written. On a key an environment variable or
a flag is overriding, the edit still applies and is still written, and the row adds that the
override will win again the next time apogee starts — startup precedence is unchanged. If a
write lands but the live apply refuses it, the row says exactly that
(`saved — live apply failed: …`) rather than leaving you to guess which half happened.

**`backspace` unsets.** On a row you have set, `backspace` arms a reset, the hint line asks
for a confirming `⏎`, and what that sends **removes the key's line** from the file rather than
writing today's default into it — so the key goes back to following the built-in default
instead of being pinned to a copy of it. The default is applied on the same keypress, and the
row reports it with the same marker: `default *`.

**What no row can write opens your editor.** `servers:`, `sub-agents-server:`, `mcp-servers:`,
`reactions:`, `system-prompt-models:`, `system-prompt-layers:`, `tools.enabled:` and the model
profile render as a summary with an `· ⏎ opens $EDITOR` pointer, and that is what `⏎` does — in the
editor the
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

**The seven [Floor guards](configuration.md) are ordinary rows.** They are not switches you arm —
they are on already — so each is an `on`/`off` row in the pane's **Session** section, edited in place
like any other boolean and applied to the running session: `tool-call-repair`, `tool-call-salvage`,
`tool-loop-breaker`, `empty-response-recovery`, `tool-use-enforcer`, `read-cache` and
`tool-result-cap`. The [`context-fill-notice`](configuration.md#context-fill-notice) row beside
them is the same kind of row — `on`/`off`, applied to the running session the moment you commit it
— but it is not a Floor guard and starts `off`.

