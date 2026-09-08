# Reactions — commands and webhooks on what a session did

`reactions:` in `~/.apogee/config.yaml` is a list of **observe-only** Reactions apogee runs when
something happens in a session — what other tools call a hook. Each entry names the moments it
fires on and the one action it takes: an argv list run directly, or a URL the moment is POSTed to
as JSON. The list is empty by default, so a fresh install runs nothing and reports nothing.

An entry is told what **already happened**. It cannot veto a tool call, delay a turn, answer an
approval, or change anything about the run it is watching, and nothing it prints, returns or
answers reaches the model, the conversation or the saved session — its failure is yours to see and
nobody else's. It is a [Reaction](../../CONTEXT.md) of **user** origin and **observe** class: it
watches. A Floor guard is the engine's own `shape (view)` Reaction — it fires on a Moment inside
the loop and changes what the model sees — while yours fires after the fact, on your own machine,
as your configuration rather than a model action. There are deliberately no `pre-*` moments here:
every notice below reports a thing that is over.
([ADR 0073](../adr/0073-hooks-are-observe-only-driver-side-reactions-to-engine-events.md) is the
decision and its reasoning;
[ADR 0076](../adr/0076-one-reaction-core-with-an-origin-by-class-policy-matrix.md) is the origin and
class vocabulary.)

The same list is fired by every front-end — the interactive TUI, a `/schedule` firing inside it,
[`apogee headless`](headless.md) and an [`apogee daemon`](daemon.md) firing — from one library, so
an entry means the same thing wherever the run was started from.

## An entry

```yaml
# ~/.apogee/config.yaml
reactions:
  - id: notify                          # unique; it is what a failure notice reports
    on: [approval-requested]            # one or more of the eleven notices
    run: ["notify-send", "apogee is waiting for an answer"]
    timeout: 10s                        # optional; default 30s
  - id: ci-bell
    on: [file-changed, exchange-finished]
    run:
      url: https://example.invalid/apogee
      headers:
        X-Source: apogee                # a literal header value
      headers-env:
        Authorization: MY_WEBHOOK_TOKEN # the NAME of the variable holding the value
    workspace: ~/code/apogee            # optional; fires in this workspace only
```

| Key | Meaning |
|---|---|
| `id:` | Required, and unique in the list. It is the payload's `reaction` field and what every failure notice reports, so two entries called `notify` would report as one. One of the seven Floor-guard keys is refused as an id — a guard is switched off with its own top-level key, not with an entry here. |
| `on:` | Required, at least one, from the eleven notices below. A spelling outside that vocabulary is refused at startup, and so is one of the five **seams**: a `run:` entry reacts to notices, and the classes that act on a seam (`advise:`, `gate:`) are not yet shipped. |
| `run:` | The entry's one action, in either of two shapes. A **list** is an argv: `run[0]` is the program, the rest are its arguments, passed word for word. A **mapping** `{url:, headers:, headers-env:}` is a webhook the payload is POSTed to. |
| `workspace:` | Optional. Scopes the entry to one workspace; unset means every workspace. |
| `timeout:` | Optional Go duration (`10s`, `2m`). Default `30s`. Bounds the command run and the POST alike. |
| `enabled:` | Optional. `enabled: false` **parks** an entry — it stays in the file and is dropped when the file is read, so nothing arms it and the `/settings` summary does not count it. |
| `advise:` / `gate:` | Reserved for the classes a later release ships. An entry that spells either is refused today, by a sentence that says so, rather than being silently ignored. |

An entry takes **exactly one** action, and `run:` is the only one this release ships. Inside the
webhook mapping the three keys above are the whole vocabulary — a misspelt `header-env:` is a
startup refusal and not a token that never gets sent. The whole block is checked when the file is
read, and a malformed entry is a startup refusal naming the entry rather than a Reaction that
silently never fires.

The block is file-only: there is no flag and no environment variable for it. `/settings` shows a
read-only `reactions:` row counting what is armed, and `⏎` on it opens your editor, because no
settings row can write a list this shape.

## The eleven notices

Six report something the session did; five report that one of the loop's own **seams** finished
passing.

| Notice | Fires when | Depth |
|---|---|---|
| `exchange-finished` | A top-level exchange closed — the model produced a final answer with no tool call, or the loop abandoned or capped it. `faulted` and `step_capped` in the payload say which. | Top-level only |
| `turn-finished` | Every top-level turn boundary, whatever its outcome. A turn that closed its exchange fires **both** this and `exchange-finished`, in that order. | Top-level only |
| `file-changed` | A workspace write tool succeeded. A delete, copy or move reports its **destination**, because that is the path whose content changed. A refused or failed write fires nothing. | Any, sub-agents included |
| `approval-requested` | An approval was **raised** and is waiting on you — before you answer, not after. | Any, sub-agents included |
| `approval-decided` | That same approval reached its verdict, which the payload carries as `decision`. | Any, sub-agents included |
| `error` | A localised, recovered engine fault. A Reaction's own failure never becomes one of these, because an entry subscribed to `error` would then fire on itself and loop. | Any, sub-agents included |
| `pre-request-finished` | The `pre-request` seam's pass finished: the outgoing request is as the loop will send it. Full working value; only serialized when you subscribe. | Top-level only |
| `post-response-finished` | The `post-response` seam's pass finished, on the model's answer. Full working value; only serialized when you subscribe. | Top-level only |
| `pre-tool-exec-finished` | The `pre-tool-exec` seam's pass finished, on a call about to run. Full working value; only serialized when you subscribe. | Top-level only |
| `post-tool-result-finished` | The `post-tool-result` seam's pass finished, on a result before the model sees it. Full working value; only serialized when you subscribe. | Top-level only |
| `history-rewrite-finished` | The `history-rewrite` seam's pass finished, on the conversation. Full working value; only serialized when you subscribe. | Top-level only |

The turn and seam notices are top-level only on purpose: a sub-agent runs the same loop, so one at
every depth would fire once per step of every delegation.

A seam-closing notice reports a pass that is **over** — reading one changes nothing about it. The
projection of the working value is built while the engine is still handing the moment out, and only
when an entry actually subscribes: a seam nobody watches costs one map lookup and no work at all.

The set is deliberately small and additive. There is no notice for a token, a tool call, a
sub-agent phase, a session save, a prune or a usage total.

## The payload

The same JSON document reaches a command on **stdin** and a webhook as the **POST body**. Its
field spellings are a documented contract — a script reads them one by one — and a field that does
not apply to the notice is omitted rather than sent empty.

Present on every notice:

| Field | Meaning |
|---|---|
| `event` | The notice that fired, spelled exactly as `on:` spells it. |
| `reaction` | The `id:` of the entry that fired. |
| `time` | When the firing was matched, RFC 3339. |
| `workspace` | The run's workspace: absolute, with symlinks resolved. |
| `depth` | The emitting agent's sub-agent nesting level; `0` is the top-level agent. |
| `turn` | The turn index the notice belongs to. |
| `call_id` | The emitting agent's run identity — the id of the `sub_agent` call that spawned it. Absent at depth 0. |
| `schedule` | `{"id": …, "name": …}` — the schedule this firing ran for. Present only on a `/schedule` or daemon firing. |

Per notice, added to that block:

| Field | On | Meaning |
|---|---|---|
| `status` | `turn-finished`, `exchange-finished` | The turn's disposition: `turn-complete`, `exchange-complete` or `cancelled`. |
| `faulted` | `turn-finished`, `exchange-finished` | The loop abandoned the turn rather than completing it. |
| `step_capped` | `turn-finished`, `exchange-finished` | The step cap ended the exchange, not the model. |
| `tool` | `file-changed`, the two approval notices | The tool that wrote the file, or whose call the approval is about. |
| `path` | `file-changed` | The absolute, symlink-resolved path the write landed on. |
| `reason` | The two approval notices | Why the approval was required, in the engine's own words. |
| `remedy` | The two approval notices | The optional one-line route out of the condition that forced it. |
| `sub_agent_name` | The two approval notices | The display name of the child whose call it is, when it has one. |
| `scope` | The two approval notices | What the call reaches beyond what its arguments name, when that is stated. |
| `decision` | `approval-decided` | The verdict: `allow`, `deny` or `allow-for-session`. |
| `source` | `error` | What faulted — a tool name, a reaction id, or `loop`. |
| `error` | `error` | The fault's message. |
| `seam` | The five seam-closing notices | The seam whose pass closed, in its own spelling — `pre-request`, `post-response`, `pre-tool-exec`, `post-tool-result` or `history-rewrite`. |
| `reactions` | The five seam-closing notices | The ids of the Reactions that acted during the pass, in the order they fired. Absent when none did, which is the ordinary pass. |
| `value` | The five seam-closing notices | The working value as the pass left it, projected per seam (below). |

```json
{"event":"file-changed","reaction":"ci-bell","time":"2026-09-08T09:41:00Z",
 "workspace":"/work/repo","depth":1,"turn":2,"call_id":"call-7",
 "tool":"write_file","path":"/work/repo/main.go"}
```

`value` is one shape per seam, and each is a **copy** taken before the loop moved on:

| `seam` | `value` |
|---|---|
| `pre-request` | `{"messages": [{"role", "content", "tool_calls", "tool_call_id"}], "tools": ["…"]}` — the tool menu is reduced to the names it offered, because the schemas would be kilobytes of JSON on every request. |
| `post-response` | `{"text", "tool_calls": [{"id", "name", "arguments"}], "retryable"}`. |
| `pre-tool-exec` | `{"id", "name", "arguments"}` — the call as it is about to run. |
| `post-tool-result` | `{"call": {"id", "name", "arguments"}, "content", "is_error"}`. |
| `history-rewrite` | `{"messages": […]}` — the conversation after the rewrite. |

The engine's own bookkeeping on a message is left out, and so is anything the projection cannot
recognise: a value apogee could not have produced costs the firing an absent `value` rather than a
fault on the loop's goroutine.

The payload is **not** scrubbed. It goes to your own command or your own URL, which is the same
trust as your screen — so a `reason`, an `error` or a `value` may quote whatever the run was
working on.

## Running a command

An argv `run:` is run **directly**: no shell, no interpolation, no word splitting, no glob, so a
character in a file path can never mean something. When you want a pipeline, write it out:

```yaml
run: ["sh", "-c", "echo \"$APOGEE_REACTION_PATH\" >> ~/changed.log"]
```

Alongside stdin, the payload's headline facts are in the environment, so a one-line script need
not parse JSON at all. A fact the firing does not carry is **omitted** rather than set empty, so
`[ -n "$APOGEE_REACTION_PATH" ]` is a real test:

`APOGEE_REACTION_EVENT` · `APOGEE_REACTION_NAME` · `APOGEE_REACTION_WORKSPACE` ·
`APOGEE_REACTION_PATH` · `APOGEE_REACTION_SCHEDULE_ID` · `APOGEE_REACTION_SCHEDULE_NAME`

The rest of the posture, which is the one an `api-key-cmd:` already runs under:

- **Outside confinement.** An entry here is your configuration rather than anything the model
  chose, so it is not sandboxed — even in Auto mode.
- **Fenced at the program.** apogee refuses to run a program that lives somewhere the model could
  have written it, so a `file-changed` entry cannot end up executing the script the agent just
  produced. A bare name is looked up on `PATH`; a name carrying a path separator is resolved
  against apogee's own working directory, exactly as running it would.
- **The environment is inherited whole** — a notifier needs `HOME`, `DISPLAY`, a D-Bus address and
  its agents' sockets. The `APOGEE_REACTION_*` entries are appended last, so they win over an
  inherited variable of the same name.
- **Stdout is discarded**, because nothing an observe Reaction prints may reach the model, the
  conversation or the session. Stderr is kept only to quote back: at most 4 KiB is read, and the
  first ~240 characters of it, folded onto one line, are appended to the failure notice.
- **A non-zero exit is a failure** and is reported to you. So is a program that cannot be found or
  is refused by the fence.
- **`timeout:` (default `30s`) ends the run.** The process is killed, and a wrapper that left a
  grandchild holding the stderr pipe gets a further two seconds before apogee stops waiting on it.

## Sending a webhook

A webhook `run:` POSTs the same JSON document to its `url:`, **once**. There is no retry: this is a
post-hoc notification, a retry would fire your endpoint twice for one moment, and a queue of failed
firings would outlive the run they belong to.

apogee sets `Content-Type: application/json` and `User-Agent: apogee` first, then your `headers:`,
then your `headers-env:` — so your own entries override apogee's defaults if you name them.
`headers-env:` reads the variable at **send** time, so a token rotated in the shell that launched
apogee is picked up by the next firing, and the secret never has to sit in the config file. This
is the `api-key-env:` spelling: the value is the **name** of the variable, and there is no `${VAR}`
interpolation anywhere in this file.

Headers are resolved before the request goes out, so a `headers-env:` variable that is not set
fails without your endpoint ever hearing from us; the failure names the header and the variable and
never the value. Anything that is not a 2xx is reported as `HTTP <code>`. The response body is
drained (up to 64 KiB) so the connection can be reused, and then discarded unread — nothing a
webhook answers reaches the model either. A transport failure is reported **without** the URL,
because a webhook URL is exactly the kind of thing that carries a token in its path or query.
`timeout:` bounds the whole POST.

## `workspace:` — scoping an entry

`workspace:` matches **exactly**, not by prefix: an entry scoped to `~/code/apogee` fires for runs
rooted there and not for a run rooted in a subdirectory of it. Both sides of the comparison — your
filter and the run's own workspace — go through the same resolution first: a leading `~` expanded,
the path made absolute, every symlink evaluated. That is why `/tmp/w` and `/private/tmp/w` are one
workspace on macOS rather than two, and why a filter written either way still matches.

Leave the key out and the entry is active in every workspace.

## When a reaction is slow

Each entry gets a worker of its own and a queue of its own, so one slow script cannot delay
another, and each entry sees its own firings in order. Firing never blocks the agent: matching
happens on the engine's goroutine, everything slow happens on the worker.

The queue holds **64** pending firings. An entry that is being outrun loses the newest firings
rather than growing without limit — an unbounded queue behind a wedged script is a memory leak.
The first drop is reported once, as `reaction ci-bell: dropped 1 event (queue full)`, and the total
is stated when the run ends.

At shutdown — the session closing, a firing ending, an edit replacing the list — an entry is given
**five seconds** to finish what it is already running, after which what is still going is killed.
A run killed that way is apogee's own doing and is not reported as a failure.

## Where a failure shows

The trouble reaches you and nothing else — never the model, never an `error` notice, never the
saved session:

| Front-end | Where the notice lands |
|---|---|
| The interactive TUI, and a `/schedule` firing raised inside it | A note in the transcript, **ephemeral**: it is not stored in the session, so it does not come back on a resume claiming a failure nobody has seen since. |
| [`apogee headless`](headless.md) | A line on **stderr**, leaving stdout clean for the model's answer. |
| [`apogee daemon`](daemon.md) | A timestamped line in the daemon's log, like every other line it writes. |

A notice names the entry and the notice it was firing on:
`reaction notify (approval-requested): exit 3: …`. The same failure line for one entry is reported
**once** — an entry failing every turn would otherwise bury everything else — and again only after
that entry has succeeded in between.

## Live reload

Inside the TUI the list is **live**: save `~/.apogee/config.yaml` (or use `⏎` on the `reactions:`
row in [`/settings`](commands.md#the-settings-screen--settings), which opens your editor) and the
running session swaps its Reactions over in one generation, draining the old workers in the
background. The Floor guards and `bypass:` cross that edit untouched, and a `/schedule` firing
raised afterwards fires the list the session is running now, not the one apogee launched with. A
list the session will not take — a malformed entry, an unresolvable `workspace:` — is refused
whole, and the session keeps firing the Reactions it already had.

`apogee headless` and `apogee daemon` read the list **once**, at start. A daemon watches its
`schedules.yaml`, but not `config.yaml`, so an edit here reaches it at its next restart.

## Migrating from `hooks:`

`hooks:` was this key's earlier name, and apogee folds one into `reactions:` for you — **at
startup, once**. It backs the file up first, rewrites it, and says so:

```
apogee: rewrote /home/you/.apogee/config.yaml — hooks: became reactions: (2 entries);
approval-waiting is now approval-requested; the retired mechanisms: key was dropped
(tool_use_enforcer → tool-use-enforcer:); the inert validated-sets: key was dropped; comments
inside the old block did not survive; backup at /home/you/.apogee/config.yaml.bak-….
Scripts must read APOGEE_REACTION_* (was APOGEE_HOOK_*) and the payload's "reaction" field
(was "hook").
```

The rename is mechanical: `name:` → `id:`, `events:` → `on:`, `command:` or `webhook:` (with its
`headers:` and `headers-env:`) → `run:`, and the retired `approval-waiting` spelling → the
`approval-requested` notice. `workspace:` and `timeout:` are carried across unchanged. Comments
**inside** the old block are lost — the block is re-rendered from its entries — which is what the
backup named in the note is for.

Two things your own scripts must change, because apogee cannot change them for you:

- the environment is `APOGEE_REACTION_*`, where it was `APOGEE_HOOK_*`;
- the payload's field is `reaction`, where it was `hook`.

A file that carries **both** `hooks:` and `reactions:` is refused rather than merged: `reactions:`
is the single list, and apogee will not guess which of two blocks you meant. Move the entries
across yourself and delete `hooks:`. A file that goes back to `hooks:` while a session is running is
refused too, with nothing written — the fold is a startup act, because apogee does not rewrite a
config file out from under a running session. Restart it and the fold runs.

The retired `mechanisms:` and `validated-sets:` keys are stripped by the same rewrite. Neither is
read by anything any more: the mechanism catalogue was retired into the seven top-level Floor-guard
keys, and the note names the successor key for each id it dropped.
