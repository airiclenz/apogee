# Sessions

Every conversation is a session, saved continuously: after each completed turn the
session is written to `~/.apogee/sessions/` (asynchronously, best-effort), so a
crash or `kill -9` costs at most the turn in flight. Stopping a run with `esc` twice
costs only the step under way: the steps that finished before the stop are saved with
the session as it goes idle. A closing save also runs when you
quit — `⌃c` twice mid-answer included, which waits for the worker to unwind and then
writes what it had — and when `/clear` or `/new` closes the session into history. A turn that hands work to a
sub-agent is saved as that work runs — a **progress save** fires when the
delegation is issued, each time the sub-agent finishes a tool, when a sub-agent finishes
(unless its bracket was cancelled and rolled back, which lands nothing to save), and when
a delegation's generated name arrives — so a long
delegation is on disk while it happens, not only once its turn ends. A saved
session stores the engine's conversation **and** the TUI scrollback, so
resuming repaints the transcript you actually saw — tool cards included — and
relights the context gauge, instead of opening an empty view over a model that
still remembers.

- `apogee --continue` resumes this workspace's most recent session; `--resume`
  takes a session id (from `/sessions`) or a file path. The two are mutually exclusive —
  naming both is a flag error — and `--continue` in a workspace with no saved session
  refuses, naming the workspace and pointing at `--resume <id>`. Resuming also re-opens
  the session's snapshot store, so
  [`/undo`](commands.md#undoing-the-agents-file-writes--undo-and-redo) still reaches what
  an earlier process wrote.
- `/sessions` opens the in-TUI browser (newest first): typing filters the list,
  `⏎` resumes, `^r` renames inline, `^d` deletes after a confirm, `^a` toggles
  between this workspace and all workspaces. The verbs are chords precisely so
  the letters are free to type with — every selector pop-up filters as you type.
- A session is **held** by the apogee that has it open, from the moment its record
  first reaches disk until that apogee exits, so two instances can never write one
  record. Opening a held session anywhere else — `--resume`, `--continue`, the
  browser's `⏎` — or deleting it with `^d` is refused with one line:
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
  same task, still read as one history and the branch that left it.
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
  and the sum its sub-agents reported — and the browser row shows their **sum**,
  the whole session's spend. A run that hands most of its work to delegates costs
  most of its tokens in windows that closed with those runs, so a row reporting
  only the conversation you steered would understate it by a wide margin; `/usage`
  is where the two halves are read apart again.
- A record also keeps **which models actually answered** — every distinct id the
  server put on a reply, in the order first seen, a sub-agent's included — beside the
  profile the session was bound to, because the two can differ: an alias the server
  resolves, a launcher that swapped what a profile serves, a delegation routed to a
  server of its own. `/usage` names them on a `served:` line above its rows once a
  reply has carried one; a server that names no model leaves the record and the pane
  as they were.
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
  started it, and a new message discards that step alone. A run you stopped yourself with
  `esc` twice is different: it is saved closed, with every step that finished before the
  stop, so it resumes as an ordinary session with nothing to continue. The stop leaves no
  mark in the saved session — the note that tells the model the run was stopped lives in
  the running session only.
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

