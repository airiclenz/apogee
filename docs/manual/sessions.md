# Sessions

Every conversation is a session, saved continuously: after each completed turn the
session is written to `~/.apogee/sessions/` (asynchronously, best-effort), so a
crash or `kill -9` costs at most the turn in flight. Cancelling a run with `esc` twice
costs nothing that finished: the steps that finished before the cancel, and the step under way
with the results the cancel gave its tool calls and sub-agents, are saved with the session as it
goes idle. A closing save also runs when you
quit — `⌃c` twice mid-answer included, which waits for the worker to unwind and then
writes what it had — and when `/clear` or `/new` closes the session into history. A turn that hands work to a
sub-agent is saved as that work runs — a **progress save** fires when the
delegation is issued, each time the sub-agent finishes a tool, when a sub-agent finishes
(one your cancel stopped included — it finishes on the summary the stop gave it), and when
a delegation's generated name arrives — so a long
delegation is on disk while it happens, not only once its turn ends. A saved
session stores the engine's conversation **and** the TUI scrollback, so
resuming repaints the transcript you actually saw — tool cards included — and
relights the context gauge, instead of opening an empty view over a model that
still remembers.

- `apogee --continue` resumes this workspace's most recent session; `--resume`
  takes a session id (from `/sessions`) or a file path. The two are mutually exclusive —
  naming both is a flag error — and `--continue` in a workspace with no saved session
  refuses, naming the workspace and pointing at `--resume <id>`. Either refusal names the
  `<home>/sessions` directory it searched, so a wrong `--config` or `APOGEE_CONFIG` home
  shows as the wrong directory rather than reading as a wrong id. Resuming also re-opens
  the session's snapshot store, so
  [`/undo`](commands.md#undoing-the-agents-file-writes--undo-and-redo) still reaches what
  an earlier process wrote. `--resume` on a session another apogee is running watches it
  instead (see [watching a live session](#watching-a-live-session)).
- `/sessions` opens the in-TUI browser (newest first): typing filters the list,
  `⏎` resumes (or watches a `● live` row), `^r` renames inline, `^d` deletes after a confirm, `^a` toggles
  between this workspace and all workspaces. The verbs are chords precisely so
  the letters are free to type with — every pop-up that lists things to search
  (`/sessions`, `/settings`, and the `/model`, `/server`, `/sub-agents-server`,
  `/effort`, `/fork`, `/schedule` and mode pickers) filters as you type. A paste
  is typing too, folded onto one line. The filter shows on the pane's title row
  (`saved sessions  (this workspace) · filter: mo▌`) and matches the session's
  title with its qualifiers — the workspace name in the all-workspaces view, a
  schedule's `⟳` tag — never its age, message count or spend; `backspace` trims
  it, `esc` closes the browser, filter and all, and `^a` keeps it while it
  switches scope. A filter that matches nothing says so
  (`no session matches "zzz"`). While you rename, your keys and a paste go to the
  name, and the filter waits, unchanged, on the title row. The panes that ask a
  question with fixed answers — `stop running workflows? (y/n)`, the start-up
  offers about a plaintext key or a retired flag, and the offer to adopt a rule,
  MCP server or reaction the project proposes — do not filter.
- A session is **held** by the apogee that has it open, from its first prompt until
  that apogee exits, so two instances can never write one record. A held session that
  is running opens elsewhere only to be **watched**: `--resume <id>` and the browser's
  `⏎` open it in [watch mode](#watching-a-live-session), read-only, instead of loading
  it. Everything that would take it over is refused with one line —
  `--continue`, renaming it with `^r`, deleting it with `^d`, and `--resume` or `⏎` on
  a held session that writes no Event journal (one `apogee undo` is reverting, say):
  `session <id> is open in another apogee (pid N) — fork it to work alongside`
  (the pid is omitted when it is not known). `--continue` refuses rather than
  quietly opening the workspace's next-newest session; in the browser the refusal
  is a transcript note and the conversation you are in is untouched. To work on a
  held session's history from a second terminal, [`/fork`](commands.md) it from the
  instance that holds it — the fork is a new record nobody holds — or `--resume` the
  fork's id. The hold is the kernel's own lock on a `<id>.lock` file beside the
  record; it dies with the process, so a crash or `kill -9` leaves nothing to clean up.
  A new session names itself: on its first prompt apogee asks the
  model, in a single call off to the side of the conversation, for a short title
  (`auto-title:`, a key with no flag and no environment variable, on by default). With that off — or when the
  call fails or answers with nothing usable — the title falls back to the first
  user message, or to a dated `Session <date>` when that message is empty or
  opens a code fence. A bare `/rename` later re-reads the session — your opening
  request plus the most recent ones — and names it for what it has become, so
  one that moved on to another task gets named for where it ended up.
- Nothing removes a saved session but you: the store keeps every record for ever
  unless the `sessions:` block gives it a bound. Name a `max-age`, a `max-count`,
  or both and a silent sweep at startup discards what falls outside them — never
  the session you are resuming, and never a file it cannot read. See
  [keeping the session store bounded](configuration.md#keeping-the-session-store-bounded--sessions)
  for the two keys.
- That same `auto-title:` switch also names **delegations**. A sub-agent the model
  spawned without giving it a name gets a short one from the same kind of call off to
  the side — asked of the server that child itself runs on, while it works — and the
  generated name then stands wherever that run is shown: its collapsed block, the
  `✦ Sub-Agent` rows, the breadcrumb when you open the run, and the saved record, so a
  resumed session shows it too. A name the model supplied is never overwritten, a name
  that arrives after the run has already finished is dropped, and anything that goes
  wrong is silent — the run keeps reading as the first line of its task, which is what
  it always did.
- A run of a `/schedule` saves its own session, so it browses like every other:
  the browser tags it `⟳ <schedule>` beside its title, so a run reads as one of a
  series rather than as a session nobody remembers starting. Ordering, resume,
  rename and delete treat it exactly like a session you held yourself.
- `/fork` branches a new session off this one: a picker lists the session's prompts,
  `⏎` keeps the history through the chosen one and switches you to the new session,
  which starts under its parent's title. The session you were in stays saved as it
  was, and the browser tags the fork `⑂ <parent title>` beside its title — by the
  parent's id once the parent has been deleted — so the two rows, both named for the
  same task, still read as one history and the branch that left it. The new
  session starts at zero spend: the sub-agent and workflow cards it keeps show
  how full each run's context got, but their spend stays counted in the parent,
  so `/usage`, the footer and the new session's record count only the calls it
  makes itself.
- Every tool card in a record keeps a bounded copy of the arguments the model sent
  with that call — the main agent's and every sub-agent's alike — so a finished
  run's tool use can still be read back off the file. It stays a summary and not a
  transport: a write or edit's file content is left out (the card's own diff already
  carries it), and any other oversize value is stored as its size instead of its bytes.
- Every entry in a record carries **when it was committed** (`at`, a UTC timestamp),
  and every tool card the **size of what came back** (`chars`, the byte length of the
  result before the card summarised it) — so a saved run can answer how long a step
  took and how much each call put into the context. Neither is shown on screen; the
  order of a record stays the order things happened per run, not clock order, because
  concurrent sub-agents each keep their own stretch. A record written before these
  fields existed reads with them empty. A **compaction** leaves a `compacted` entry at
  the depth it folded — in the conversation for a `/compact` or the main agent's own
  fold, inside a sub-agent's block for that sub-agent's — painted as the same dim
  `context compacted` note it always was, so the record says whose context was folded
  and when. A headless run (`apogee headless`, a `/schedule` firing) writes none of
  these three.
- A record stores what the session spent in two halves — the main agent's tokens
  and the sum its delegates reported, sub-agents and workflows' runs alike — and
  the browser row shows their **sum**, the whole session's spend. A run that hands
  most of its work to delegates costs most of its tokens in windows that closed
  with those runs, so a row reporting only the conversation you steered would
  understate it by a wide margin; `/usage` is where the two halves are read apart
  again. When any call of the session ran on a server with a `price:`, the row
  appends what the session cost after the tokens — `· 1M · 0.42 USD`, in the
  currency label the record was saved under, led by `≥ ` when part of the session
  ran unpriced. A session with no priced call, including one saved before pricing
  existed, shows tokens only. A session keeps the label it first priced a call
  under: resume a session priced in `EUR` while `currency: USD` is configured
  and it is still saved as `EUR`, so its amount is never relabelled. A session
  with nothing priced yet takes the configured label, and `/clear` or `/new`
  starts the next session under it.
- A record also keeps **which models actually answered** — every distinct id the
  server put on a reply, in the order first seen, a sub-agent's or a workflow's
  included — beside the profile the session was bound to, because the two can
  differ: an alias the server resolves, a launcher that swapped what a profile
  serves, a delegation routed to a server of its own. `/usage` names them on a
  `served:` line above its rows once a reply has carried one; a server that names
  no model leaves the record and the pane as they were.
- `/clear` (or `/new`) closes the current session into history and starts a fresh
  one — neither deletes; discarding is an explicit `^d` in the browser. The closed
  session keeps its token accounting and the models that answered it; the fresh one
  starts from zero, in `/usage` and in its own record alike.
- A session killed mid-task resumes to the last completed turn and says so;
  `/continue` then picks the unfinished work back up, while sending a new message
  instead keeps the finished steps and continues from there — the transcript notes that
  the interrupted work was closed and its finished steps stand. A delegation that was still
  running when the session was written comes back marked **interrupted**, with a note
  saying the sub-agent's unfinished work was not kept: `/continue` re-runs the step that
  started it, and a new message discards that step alone. A run you cancelled yourself with
  `esc` twice is different: it is saved closed, with every step that finished before the
  cancel and the results the cancel gave the step under way, so it resumes as an ordinary
  session with nothing to pick back up. The cancel leaves no mark in the saved session — the note that tells the model the run was cut short lives in
  the running session only. A single delegation you stopped with `^x` is not interrupted work
  either: its result — the engine's summary of what it had done — is saved like any other.
- The delegations your agent can **continue** — every one its call named, and every one
  that was capped, faulted or stopped (see
  [configuration](configuration.md#delegated-sub-agents--what-one-may-spend)) — are saved with the session, so
  `--continue` and `--resume` bring them back and `continue: "<name>"` works on a session
  reopened months later. `/clear` drops them with the session it closes; a `/fork` keeps
  only the ones that ran within the history it keeps; and a turn you cancel puts them back
  as they were when that turn began.
- The session's **name is written on the top rule**, the hairline above the status
  line — `▔▔▔▔ the name ▔▔▔▔` — so a screen full of panes says which conversation
  each one is. It shows whatever named the session, from `/rename` or from the automatic
  naming call, and a session with no name yet shows a plain unbroken rule. Nothing
  needs configuring in your terminal for it: it is a row apogee paints itself.

Autonomy mode, tool approvals, confinement, and MCP connections are deliberately
**not** part of a saved session — they are re-established or re-confirmed on
resume, so yesterday's approvals never silently apply to today's run, and any
Console the previous conversation left open is closed at the switch.

Nor is what you had **open** on screen. Which blocks you had unfolded is the
view's own state, and so is a **run view** — the full-transcript reading of one
sub-agent you get by opening its row. A resumed session opens at the top level
with everything folded shut, however deep into a delegation you were when it was
written; the run itself is in the record, and one click opens it again. The two
exceptions are the task-list card and the large Tools umbrella: their folds are
preferences rather than view state, remembered in
[`ui.task-list-open`](configuration.md#the-terminal-ui--ui) and
[`ui.tools-open`](configuration.md#the-terminal-ui--ui) — so a resumed session
paints every task-list card, and every `✦ Tools (N calls)` umbrella with more
type rows than `ui.tools-fold-over`, the way you last left them, not the way the
record found them.


## Watching a live session

A session another apogee is running — a TUI session in another terminal, an
[`apogee headless`](headless.md) run, a [daemon](daemon.md) firing or a `/schedule` run — can
be followed live from a second terminal, read-only. While it runs, the Driver running it writes an
**Event journal** beside the record (`~/.apogee/sessions/<id>.events.jsonl`), and watch mode paints
the record's committed entries and then follows that journal: the streamed answer, the tool cards
as they run, the delegations and their run views, and the model's thinking. The owner never learns
that anyone is watching, and nothing flows back to it. The design is
[ADR 0103](../../.capcom/knowledge/decisions/adr-0103.md).

- **Finding one.** In `/sessions`, a row whose session is running now ends in a `● live` cell. A
  run that has not saved a record yet gets a row of its own, named and dated from its Event
  journal. Your own running session is never marked live, so `⏎` on its row reloads it as
  before. If the live list cannot be read, the saved rows still open, with a
  `could not list live sessions: …` note.
- **Opening one.** `⏎` on a live row opens it in watch mode without loading it: nothing is held,
  nothing is saved, and your background workflows are not asked to stop. `apogee --resume <id>`
  on a running session does the same at start-up, a headless run or firing that has not saved yet
  included.
- **While you watch**, the prompt box reads `watching <title> · read-only · esc back`. Typing to
  the session, sending, a command that would act on it, `shift+tab` and every picker are refused
  with a flash: `read-only — you are watching this session; esc goes back`. The view-only
  commands still work — `/thinking`, `/help`, `/usage`, `/inspect` and `/version`. Thinking is
  shown only in the `/thinking` pop-up, never inline. Opening a delegation's row opens its run
  view read-only, as it would in your own session. If the record cannot be read, the view says
  `the session record could not be read — showing the live events only` and follows the journal
  alone.
- **Leaving.** `esc` returns to the session you were in, exactly as you left it: watching parks
  your own session and changes nothing in it. After `--resume`, `esc` lands in a fresh session.
  Quitting while you watch quits as usual.
- **When the owner ends**, a banner joins the legend and the view stays read-only:
  `watching <title> · finished · read-only · ⏎ resume · esc back` once the owner saved and let
  go. `⏎` then resumes the session through the normal door. If the owner let go without a final
  save — a crash or a `kill -9` — the legend reads
  `watching <title> · owner gone without a final save · read-only · esc back`, and `⏎` stays
  refused.

A watch reads files another process writes, so everything it shows is treated as untrusted and
reaches the screen through the same escape stripping as every other text. The Event journal is
removed when its owner releases the session, and a journal left behind by a crash is swept at the
next startup once its lock is free, so the record stays the only history: a finished session is
read from its record, never replayed from a journal.

### Watching another user's sessions

The same view reaches the sessions of another user on the same machine, once both sides opt in.
The usual arrangement is one account that runs the work — a service account running
`apogee daemon`, say — and the people who watch it. Both keys are global-only and described with
the [rest of the `sessions:` block](configuration.md#keeping-the-session-store-bounded--sessions):

1. **A group both accounts are in.** Create it and add the owner and every watcher to it — on
   Linux `sudo groupadd apogee-watch` and `sudo usermod -aG apogee-watch <user>` for each. A new
   membership takes effect at the user's next login.
2. **The owner shares its session files** with that group in its own `~/.apogee/config.yaml`:

   ```yaml
   sessions:
     share-group: apogee-watch
   ```

   From then on every session file apogee writes belongs to the group: the apogee home becomes
   group-traversable (`0710`), `sessions/` group-listable (`0750`), and records, Event journals and
   `.lock` files group-readable (`0640`). apogee sets the home's and `sessions/`'s group and mode
   only when they differ from these, so a home another user owns (root, say) or one on a
   read-only mount works too, once it is set up that way in advance —
   `sudo chown root:apogee-watch /srv/svc/.apogee && sudo chmod 0710 /srv/svc/.apogee` — and
   apogee then changes neither. Before the home goes to another owner, the running user must
   already own `sessions/` (group `apogee-watch`, `0750`) and every other directory apogee
   creates there that the run uses — `scratch/`, `snapshots/`, `workspaces/`, `daemon/` for
   `apogee daemon`: a `0710` home it does not own refuses their creation. A file written owner-private before is re-moded the next time it is
   written. Scratch, snapshots and everything else under the home stay owner-private.
   If the owner's `config.yaml` is itself group-readable, apogee warns at startup: the shared home
   lets the group reach it, and with it any plaintext `api-key:` it holds.
3. **Each watcher names the owner's home** in its own `~/.apogee/config.yaml`, under a label:

   ```yaml
   sessions:
     watch-homes:
       builds: /home/apogee-svc/.apogee
   ```

Both keys are read at startup, so restart the owner's apogee — a running daemon included — and
the watcher's. The watcher's `/sessions` then mixes the owner's sessions into its own list by time,
each tagged `· builds`, and they obey the browser's workspace filter like any other row — one
recorded against another workspace shows under `^a`. `⏎` on a live one watches it as it runs; `⏎`
on a finished one opens it read-only, painting its record under the `finished` banner. A watched
home's session is never resumable: the finished banner offers no `⏎ resume`, and `^d` and `^r`
refuse with `session <id> is in watched home builds — read-only`. `apogee --resume <id>` finds
ids in watched homes too and opens them in watch mode; where an id is in your own home as well,
yours wins. A watched home is only ever read: nothing in it is created, written, swept or held, and
no config or skill is read from it. One that cannot be read shows a single
`could not read watched home <label>: …` notice instead of failing the list.

Removing `share-group` later makes records and Event journals owner-private again on their next
write, but leaves the directories and the existing `.lock` files as the shared run left them; to
close the share at once, `chmod 700 ~/.apogee ~/.apogee/sessions` and
`chmod 600 ~/.apogee/sessions/*`. What sharing exposes is set out in
[SECURITY.md](../../SECURITY.md). `apogee undo` reads no config file, so it never applies
`share-group`: it leaves an existing `.lock` file's mode and group as they are, and a `.lock` it has
to create stays owner-private until the owner's next run re-shares it. (2026-10-17,
docs/plans/archived/2026-10-17 - 03 - live-session-watch-plan.md.)
