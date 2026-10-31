> **Status:** the owner's pinned mockup for the two decision surfaces — implemented by
> `docs/plans/archived/2026-08-04 - 03 - user-questions-menu-layout-plan.md`, whose ratified design calls
> amend it where the two differ; `layout.md` carries the rules that came out of it. The adoption
> pane — the third surface that asks before something runs — is specified whole in
> [§Adoption pane](#adoption-pane) at the foot of this file (2026-10-18, ADR 0104 §11).

Arrow keys (up and down) navigate.
Enter selects the item and sends the answer.
~~If shortcuts are given, the directly select the corresponding item and send the answer.~~
The rules both menus follow on screen — height, pointer, chrome — are `layout.md`'s.

> Amended 2026-08-04 by `docs/plans/archived/2026-08-04 - 03 - user-questions-menu-layout-plan.md`,
> whose ratified design calls struck the digit shortcuts out ("no digit shortcuts"): a question's
> choices are reached with `↑↓` and sent with `⏎`, and typing a digit types a character into the
> custom answer like any other key. So the `[1]` / `[2]` / `[3]` cells the multi-option sketch below
> draws are **not** painted — the always-painted hint row under it is. The approval prompt's `[a]` /
> `[s]` / `[d]` / `[esc]` cells are unaffected: those shortcuts are live (`approvalMenu`).
> *(2026-09-30: the multi-option sketch below is redrawn without the digit cells, its options
> re-wrapped to the width they free, so it now shows what `askChoiceRows` paints.)*

> Amended 2026-09-06 by `docs/plans/archived/2026-09-06 - 00 - popup-breathing-rows-and-click-to-select-plan.md`:
> the arrows are no longer the only way onto a row. **Both prompts take a POINTER**, on the terms
> `layout.md` states for every boxed pane ("What "height" means"). A click on a row moves the `❯`
> onto it exactly as `↑↓` do — and on a multi-select question it ticks that row's `[✔]`, exactly as
> `␣` does — and a **second** click on that same row is the `⏎`: it sends the answer, or takes the
> approval decision the row names, `Cancel` included. It is always two clicks. The row an activating
> click may take is the row the *pointer* put the `❯` on, so the highlight both sketches below open
> with — the first choice, and `Allow` — is never one a single click can act on, and no approval is
> ever granted by one press. Each prompt's arming delay gates that second click just as it gates
> `⏎`; a click on a choice is swallowed while the input box holds a custom answer being typed,
> the same empty-box rule the arrows follow; and a click on the question text, a blank, the hint row
> or the border belongs to the box and does nothing.
>
> A click **outside** the box is not the prompt's at all. It never cancels the question — cancelling
> stays `esc`'s alone — and it is not swallowed either: it falls through to whatever it was aimed at,
> seating the caret in the input box or starting a transcript selection, because the prompt below an
> ask question stays live and the transcript behind an approval is where its context is read.

> Amended 2026-09-27 by `docs/plans/archived/2026-09-26 - 00 - code-audit-fixes-plan.md` item 19: the ask
> question **arms like the approval prompt**, on the one latch the two prompts share. Its `⏎` — and
> the second click that sends a choice — is dead from the moment the question is folded in until the
> terminal has answered the drain marker asked from behind the prompt's frame (or the two-second
> backstop lands), so a keystroke already in flight when the question appeared cannot send an answer
> nobody read. What sends nothing stays live at once: typing a custom answer, `↑↓`, a multi-select
> `␣` (and a first click that ticks or highlights), and `esc`. A stale arm — a backstop tick or a
> drain answer left over from an earlier approval or question — never arms a later prompt.

> Amended 2026-10-11 by the P1/P2 bugs plan (`2026-10-11 - 01 - p1-p2-bugs-plan.md`, items 1–3):
> the latch is no longer the two prompts' alone. Every pane that answers a question the human has
> to have read and that apogee raises unasked over the shared picker — the two start-up offers (a
> plaintext key, a retired `sub-agents: true` flag) and the adoption pane — arms on it too: its `⏎`
> and its deciding second click are dead until the same second drain answer (or the backstop),
> while `↑↓`, the filter and `esc` are live at once. *(2026-10-18: the adoption pane's half of this
> amendment — its arming and its opening on `not now` — is folded into
> [§Adoption pane](#adoption-pane) below, which states it whole.)*

> Amended 2026-10-14 by `docs/plans/2026-10-14 - 00 - list-filter-contract-plan.md` item 3 (ADR 0053,
> 2026-10-14 amendment rule 1): the two start-up offers no longer filter — they are questions with
> fixed answers, not lists to search — so of the keys the 2026-10-11 amendment above calls live at
> once only `↑↓` and `esc` remain; a typed letter does nothing, a paste over them is dropped, and
> their legends carry no `type to filter`. *(2026-10-18: the same holds for the adoption pane, whose
> half of this amendment is folded into [§Adoption pane](#adoption-pane) below.)*


# User Approval:

> Amended 2026-09-06 by `docs/plans/archived/2026-09-06 - 00 - popup-breathing-rows-and-click-to-select-plan.md`,
> whose ratified call F makes every PART of the approval body a paragraph: the sub-agent line, the
> `Reason:`, the `Fix:`, the `Scope:`, the labelled arguments and the notes under them are each set
> off from the next by one blank line, and a part that wraps keeps the blank after its last line.
> The sketch below draws that blank between `Reason:` and `command:`; the earlier mockup ran the two
> adjacent. Nothing else about the box changes — the menu is still set off by one blank and the last
> option still closes on the bottom border.

> Amended 2026-10-10 by `docs/plans/2026-10-10 - 00 - project-config-and-allow-rules-plan.md` item 14
> (ADR 0096 §4): an ordinary gate whose call a saved **Allow rule** could answer gains a fifth
> option, `Always in this project…` `[p]`, between `Always allow this session` and `Deny`. It is
> offered only for a `terminal` line the rule matcher could ever match (no redirect, substitution,
> wrapper, Windows `cmd`, …) and for a named MCP server — where it reads
> ``Always allow server `<alias>` in this project`` and saves the server at once — and never on a
> forced pane or where no config file is wired. The sketch below shows no such row: its line starts
> with a `cd` out of the workspace root, which no rule can match. Taking the `terminal` row turns
> the menu into one editable field seeded with the suggested rule (one per simple command, joined by
> `; `); every key but `ctrl+c` and `ctrl+l` edits it, `⏎` saves it to the Project config and allows
> the call, `esc` goes back to the menu, and an empty field or a refused save is said on the hint row:
>
> ```
> ╭─────────────── Approve terminal? ───────────────╮
> │ Reason: subprocess execution                    │
> │                                                 │
> │ command:                                        │
> │   go test ./...                                 │
> │                                                 │
> │ ❯ Rule: go test▏  [⏎]                           │
> │                                                 │
> │ ⏎ save to this project's config · esc back      │
> ╰─────────────────────────────────────────────────╯
> ```

╭────────────────────────── Approve terminal? ───────────────────────────╮
│ Reason: subprocess execution                                           │
│                                                                        │
│ command:                                                               │
│   cd /workspace/repos/apogee && git status                             │
│                                                                        │
│ ❯ Allow                     [a]                                        │
│ · Always allow this session [s]                                        │
│ · Deny                      [d]                                        │
│ · Cancel                    [esc]                                      │
╰────────────────────────────────────────────────────────────────────────╯


# Multi option question:

╭────────────────────────────────────────────────────────────────────────╮
│ How to continue with the implementation of the feature "The best.      │
│ Feature in the world"?                                                 │ 
│                                                                        │
│ ❯ Just do it all in one shot and commit once.                          │
│                                                                        │
│ · Commit each piece as you go and run make check after every commit.   │
│                                                                        │
│ · Implement the config redesign first, commit it, then do the TUI part │
│   in a separate commit. Run make check after each. The config change   │
│   is the riskier part — get it stable first.                           │
│                                                                        │
│  ↑↓ select · ⏎ send · type for a custom answer · esc cancel            │
╰────────────────────────────────────────────────────────────────────────╯


# Multi-select question:

> Pinned 2026-08-05 by `docs/plans/archived/2026-08-05 - 00 - ask-user-multi-select-plan.md`, whose ratified
> design calls amend this file for questions the model marks `multi_select`: `␣` toggles the
> highlighted row, `⏎` sends every checked row (the highlighted one alone when none is checked), and
> the marker glyphs are `[✔]` / `[ ]`. The pointer and dim rows are the menu style already drawn
> above; the gap between the marker and the label is the pop-up module's own two-space column
> gutter, where the sketch below draws one.

╭──────────────────────────────────────────────╮
│ Which findings should I fix?                 │
│                                              │
│ ❯ [✔] Fix the nil-check in submitAnswer      │
│                                              │
│ · [ ] Add the missing layout() call          │
│                                              │
│ · [✔] Guard the empty-choices path           │
│                                              │
│  ↑↓ select · ␣ toggle · ⏎ send · …           │
╰──────────────────────────────────────────────╯


## Adoption pane

> Specified 2026-10-18 by `docs/plans/2026-10-18 - 00 - project-config-exec-keys-plan.md` item 1
> (ADR 0104 §11, ADR 0096 §4). It folds in the adoption-pane halves of the 2026-10-11 and 2026-10-14
> amendments at the head of this file, and `layout.md`'s paragraph on the pane now points here: this
> section is the one place the pane is specified.

**What it asks about.** A Project config (`<Project root>/.apogee/config.yaml`) may propose three
kinds of entry, and each is inert until the human adopts it: an **Allow rule** (a `terminal` prefix
or an `allow.mcp-servers` name), an **MCP server entry** (one `mcp-servers:` item, Adoption kind
`mcp-server`, handle its `name:`) and a **reaction** (one `reactions:` item, kind `reaction`, handle
its `id:`). An entry no adoption or rejection is recorded for — or one whose pin has moved since —
is *proposed*, and each proposed entry gets one pane of its own, over the shared picker.

**When it comes up.** It gives way to everything: the pre-bound ask and both start-up offers are
asked first, and so is whatever else holds the keyboard or the engine — a turn, an approval, an open
picker, the `/settings` pane, a `/sessions` load. The entries simply wait, and the pane comes up at
the first fold that finds the session idle with nothing else up. That fold is also how the pane
returns mid-session: a config file or adoption record that changes while apogee runs is re-read for
what it proposes, and an entry no pane has raised this session — a new one, or one whose pin moved
— is asked about at once after an idle save and after the turn ends on a save that landed
mid-turn, never in the middle of a turn it would have answered for. An entry a pane has raised is
not raised again in the same session with the same pin, answered or not — a watched save of some
unrelated key, or apogee's own write of an `Always in this project…` rule, does not put a `not now`
back up — so the next start-up is where a deferred entry is asked about again. With no config file
wired (a Driver that composes none) nothing is proposed and the pane never opens.

**How an entry is named.** One spelling, used by the note, the title and the answer lines alike: a
`terminal` rule is the backticked prefix it matches (`` `go test` ``), an Allow rule for a server is
``MCP server `<alias>` ``, a repo server is ``MCP server entry `<name>` `` and a repo reaction is
``reaction `<id>` ``. The word `entry` is what keeps a project that proposes both a server and an
Allow rule for it from asking two questions that read alike. Every one of these is a repository
file's text, so it is escape-stripped and flattened onto its line wherever it is shown.

**The note.** One note comes first, naming every entry in the round:
``this project's .apogee/config.yaml proposes `go test`, MCP server entry `docs` and reaction `fmt`
— nothing it proposes applies until you adopt it``. A round of Allow rules alone keeps its
wording, `— no proposed rule applies until you adopt it`.

**The title.** The first row names the entry THIS pane is asking about, so a round of several can
never be answered for the wrong one:

- an Allow rule — `allow <rule> without asking in this project?`
- an MCP server entry — ``use MCP server entry `<name>` in this project?``
- a reaction — ``use reaction `<id>` in this project?``

Like every pane's title it is cut to the pane's width, never wrapped (`layout.md`), so at a narrow
width it ends in `…` — the note above the pane carries the name whole, and the body below still
shows every field whole.

**The body.** Between the title and the rows, a server or reaction pane shows what Adoption pins
(ADR 0104 §2–§4), **verbatim** — the values exactly as the file spells them, so the human rules on
the bytes that will run, not a summary of them. An Allow rule's pane has no body: its title already
is the whole of what it pins, and it is unchanged from before this section. The body is read in
parts, one blank line apart, each part a run of labelled fields:

- **The re-ask reason**, first, and only for an entry that was adopted before and whose pin has
  moved since: one sentence naming each field and then each pinned file that changed —
  `` `scripts/fmt.sh` changed since you adopted it``, `` `args` changed since you adopted
  it``, `` `env` and `tools/docs-server.js` changed since you adopted it``. A file the entry named
  that is now gone is named the same way, as changed. A first proposal, and an entry that was only
  ever rejected or deferred, carries no such line: apogee remembers what it adopted, not what it was
  shown. There is no diff (out of scope by ADR 0104 §11): the reason says *where* to look, and the
  fields below say what is there now.
- **For an MCP server entry**, what it runs or where it connects — `transport:`, then `command:` and
  `args:` for a stdio server or `endpoint:` for an HTTP one — and then what it carries: `env:`,
  `env-allowlist:`, `headers:`, `headers-env:`, `auth:`, `client-id:` and `client-secret-env:`. The
  whole entry is pinned, so every field it sets is shown, in that fixed order; a field the entry does
  not set is not drawn, with one exception — a stdio server with no `env-allowlist:` reads
  `env-allowlist: not set — the platform floor only`, because that is what a repo server gets
  (ADR 0104 §6) and the absence is itself a fact the human is ruling on. **`env:` entries are shown
  in full, as the `KEY=VALUE` the child receives**, never as bare names: a repo
  `env: [NODE_OPTIONS=--require ./hook.js]` decides what the launched program loads, and is on the
  screen before it is adopted. `env-allowlist:` lists the inherited names; `headers:` shows each
  header with its literal value; `headers-env:` and `client-secret-env:` show the **names** of the
  variables whose values are sent — the variable's value is never read for the pane — and the
  `endpoint:` is where those values go.
- **For a reaction**, its pinned actions — `run:`, `advise:` and `gate:`, whichever it sets, in that
  fixed order, each action a part of its own. An argv action shows every element, the program
  first; a webhook action shows its `url:`, `headers:` and `headers-env:`, in that order; an MCP action
  shows `mcp:` with `args:`, `payload-arg:`, `server:` and `tool:` under it, sorted by name, and
  `args:` lists its own entries one level further in. `on:`, `timeout:`, `enabled:` and `workspace:` are not
  shown: they are parameters that change without asking (ADR 0104 §2), and a pane that drew them
  would claim a pin they do not have.
- **Pinned files**, last: under `pinned files:`, the path (relative to the Project root,
  slash-separated) of every project file whose content the pin holds — each in-root regular file an
  argv element, the program included, or an `env:` value names, by one search over its whole text,
  its words, the part after a word's `=` and its path-list elements (`--require=./hook.js` names
  `hook.js`). The digest is not drawn; the
  label says what it means, and the adopt row's gloss says what moves it. An entry that names no
  project file draws no such part.

**How a value is spelled.** A field with one scalar value reads on one line, `label: value`. A list
— `args:`, `env:`, `env-allowlist:`, an argv action — hangs under its label, one element per line
behind `- `, as the file's own block sequence spells it, so where one element ends and the next
begins is never a guess. A map — `headers:`, `headers-env:` — hangs under its label as
`key: value` lines, sorted by key as the pin is, and a webhook action hangs its own fields the same
way, one level in. The pane's own words (the labels, the reason sentence, `not set`) start at column
zero; **no line that starts with the file's text ever does**: a scalar follows its label on the
label's own line, and every element, map line and continuation hangs at least two spaces in, so a
value crafted to look like a label reads as the value it is.

**Wrapping, never cutting.** A body line too long for the pane wraps — at a space where there is
one, at the cell where there is none — and continues under its own leading indent: a list element
under its text after the `- `, a scalar or the reason sentence two spaces in. A value is never cut
across the width: a command, an argument, an `env:` value or a URL never ends at the pane's edge or
in an ellipsis mid-line, however many rows its wrap takes. Width never costs a value a character:
what a value can give up is height alone — its own lines past eight, and the rows a window too short
for the whole body cannot seat — and every one of those is counted on a marker (below). A value's own
line breaks are painted as lines under that same indent. On a pane too narrow to hold an indent and
text beside it the hang is shed whole and the line wraps flat, as every hanging surface does
(`layout.md`).

**Too tall for the window.** The three answers are the pane's floor (`popupFloor.rows`): a window
too short for the whole body never costs a row (one too short for the answers themselves costs
their glosses lines first: "Too short for the answers", below). The body loses lines exactly as the
approval prompt's does (`layout.md`, "What shrinking never buys is silence" and "And what it keeps is the
head *and* the tail"): one field's value is bounded at eight of its own lines, before any wrap —
head, `… (+N more lines)`, tail — so a padded `args:` cannot push `env:` off the surface the
decision is read from, and the body as a whole keeps its head and its last line around one marker
counting the rest. The re-ask reason is the body's first line, so it is the last thing the body
gives up. What the pane cannot seat it always counts, so the human knows there is more to read: a
taller window shows it, and `apogee project adopt` prints the same body whole for each entry it asks
about.

**Too short for the answers.** Where the window cannot seat the three answers with their glosses
wrapped even once the body is down to its marker and the blanks around the rows are gone, the
glosses give up lines before an answer does: every gloss is cut to the one line its column holds
beside the labels, ending in `…`, and on a window too short for that the labels stand alone. So
every answer label stays on the screen without scrolling — at fifty columns and twenty-one rows the
pane shows `adopt`, `not now` and `reject` each on one cut line — and only a window too short for
the three labels themselves puts one behind the scroll bar. A gloss is cut across the width only
here; a taller window shows it whole, and the cut gloss is what the pane measures and hit-tests, so
a click lands on the answer it was aimed at.

**The rows.** Under the body, three rows in two columns, with `not now` highlighted when the pane
opens — the one answer that changes nothing, because `adopt` makes a granting entry live:

| row | an Allow rule's gloss | a server's or reaction's gloss |
| --- | --- | --- |
| `adopt` | `— it runs without asking here, until the entry changes` | `— it runs here until the entry or a pinned file changes` |
| `not now` | `— it stays inert; the offer comes back at the next start-up` | the same |
| `reject` | `— it stays inert, and is not offered again until the entry changes` | `— it stays inert, and is not offered again until it changes` |

— where "it changes" is the entry's pinned fields or a pinned file's content, either of which moves
the pin. A gloss too long for its column wraps under the gloss column, never under the label —
except on a window too short for the answers (below). The
hint reads `↑/↓ select · ⏎ choose · esc close`, because nothing here switches anything.

**Arming, keys and pointer.** The pane arms on the approval prompt's latch, as the start-up offers
do: its `⏎` and the second click that takes a row answer nothing until the terminal has answered
the drain marker asked from behind the pane's frame (or the two-second backstop lands), so a `⏎`
already in flight when the pane rose — the one that answered the entry before it, above all —
adopts, defers and rejects nothing, and the deliberate `⏎` that follows takes `not now` unless the
highlight was moved. `↑`/`↓` and `esc` are live at once. The pane does not filter — it is a
question with three fixed answers, not a list to search — so a typed letter does nothing, a paste
over it is dropped and its legend carries no `type to filter`.

**The answers.** Every answer prints one line —
`adopted <entry> — it runs without asking in this project` for an Allow rule and
`adopted <entry> — it runs in this project until it changes` for a server or reaction;
`<entry> stays proposed — the offer comes back at the next start-up`;
`rejected <entry> — it is not offered again until the entry changes` — and a failed adopt or
reject says so with the reason, `could not adopt <entry>: <why>`, leaving the entry proposed, which
is exactly where `not now` leaves it. An adopted server or reaction applies at once, without a
restart. The next entry's pane then opens where the last one closed. `esc` ends the round outright,
and so does a click outside the pane, which is modal like every picker: whatever is left is a
`not now`, no line is printed and nothing is written. The same entries are listed with their state,
and can be adopted later, in `/settings` — Allow rules in their `Allow rules` section, repo servers
and reactions read-only beside the `mcp-servers` and `reactions` keys
([`settings-screen-layout.md`](settings-screen-layout.md)).

**At eighty columns.** A repo stdio server proposed for the first time, whose `env:` loads a project
file the program never names:

```
╭──────────────────────────────────────────────────────────────────────────────╮
│ use MCP server entry `docs` in this project?                                 │
│ transport: stdio                                                             │
│ command: node                                                                │
│ args:                                                                        │
│   - tools/docs-server.js                                                     │
│   - --root                                                                   │
│   - ./docs                                                                   │
│                                                                              │
│ env:                                                                         │
│   - NODE_OPTIONS=--require ./hook.js                                         │
│ env-allowlist: not set — the platform floor only                             │
│                                                                              │
│ pinned files:                                                                │
│   - hook.js                                                                  │
│   - tools/docs-server.js                                                     │
│                                                                              │
│   adopt    — it runs here until the entry or a pinned file changes           │
│ ❯ not now  — it stays inert; the offer comes back at the next start-up       │
│   reject   — it stays inert, and is not offered again until it changes       │
│                                                                              │
│ ↑/↓ select · ⏎ choose · esc close                                            │
╰──────────────────────────────────────────────────────────────────────────────╯
```

A reaction adopted earlier whose script has since been edited — the reason line leads, the webhook
`gate:` hangs as a map, and the variable it sends is named, not read:

```
╭──────────────────────────────────────────────────────────────────────────────╮
│ use reaction `fmt` in this project?                                          │
│ `scripts/fmt.sh` changed since you adopted it                                │
│                                                                              │
│ run:                                                                         │
│   - sh                                                                       │
│   - scripts/fmt.sh                                                           │
│                                                                              │
│ gate:                                                                        │
│   url: https://hooks.example.com/apogee/gate                                 │
│   headers-env:                                                               │
│     Authorization: HOOK_TOKEN                                                │
│                                                                              │
│ pinned files:                                                                │
│   - scripts/fmt.sh                                                           │
│                                                                              │
│   adopt    — it runs here until the entry or a pinned file changes           │
│ ❯ not now  — it stays inert; the offer comes back at the next start-up       │
│   reject   — it stays inert, and is not offered again until it changes       │
│                                                                              │
│ ↑/↓ select · ⏎ choose · esc close                                            │
╰──────────────────────────────────────────────────────────────────────────────╯
```

**At fifty columns.** The title is cut, the reason sentence and the `env-allowlist:` fact wrap two
spaces in, the `env:` value wraps at its space, after `--require`, and continues under its own `- `,
and the glosses wrap under the gloss column. No value here runs past the height bound, so every one
is still on the screen in full:

```
╭────────────────────────────────────────────────╮
│ use MCP server entry `docs-search` in this pr… │
│ `env` and `tools/docs-server.js` changed since │
│   you adopted it                               │
│                                                │
│ transport: stdio                               │
│ command: node                                  │
│ args:                                          │
│   - tools/docs-server.js                       │
│   - --root                                     │
│   - ./docs                                     │
│                                                │
│ env:                                           │
│   - NODE_OPTIONS=--require                     │
│     ./tools/hooks/trace-every-call.js          │
│ env-allowlist: not set — the platform floor    │
│   only                                         │
│                                                │
│ pinned files:                                  │
│   - tools/docs-server.js                       │
│   - tools/hooks/trace-every-call.js            │
│                                                │
│   adopt    — it runs here until the entry or a │
│            pinned file changes                 │
│ ❯ not now  — it stays inert; the offer comes   │
│            back at the next start-up           │
│   reject   — it stays inert, and is not        │
│            offered again until it changes      │
│                                                │
│ ↑/↓ select · ⏎ choose · esc close              │
╰────────────────────────────────────────────────╯
```

An HTTP server's body, for the same rules applied to the other transport:

```
transport: streamable-http
endpoint: https://mcp.example.com/v1

headers:
  X-Team: platform
headers-env:
  Authorization: DOCS_TOKEN
auth: oauth
client-id: apogee-docs
client-secret-env: DOCS_CLIENT_SECRET
```
