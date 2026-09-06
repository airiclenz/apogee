# Hooks — commands and webhooks on what a session did

`hooks:` in `~/.apogee/config.yaml` is a list of **observe-only** reactions apogee runs when
something happens in a session. Each entry names the events it fires on and the one action it
takes: a `command:` argv list run directly, or a `webhook:` the event is POSTed to as JSON. The
list is empty by default, so a fresh install runs nothing and reports nothing.

A hook is told what **already happened**. It cannot veto a tool call, delay a turn, answer an
approval, or change anything about the run it is watching, and nothing it prints, returns or
answers reaches the model, the conversation or the saved session — the failure of a hook is yours
to see and nobody else's. It is not a [Mechanism](../../CONTEXT.md): a Mechanism runs inside the
loop at a hook point and shapes what the model sees, while a hook runs after the fact, on your own
machine, as your configuration rather than a model action. There are deliberately no `pre-*`
events: every one of the five below reports a thing that is over.
([ADR 0073](../adr/0073-hooks-are-observe-only-driver-side-reactions-to-engine-events.md) is the
decision and its reasoning.)

The same list is fired by every front-end — the interactive TUI, a `/schedule` firing inside it,
[`apogee headless`](headless.md) and an [`apogee daemon`](daemon.md) firing — from one library, so
a hook means the same thing wherever the run was started from.

## An entry

```yaml
# ~/.apogee/config.yaml
hooks:
  - name: notify                       # unique; it is what a failure notice names
    events: [approval-waiting]         # one or more of the five events
    command: ["notify-send", "apogee is waiting for an answer"]
    timeout: 10s                       # optional; default 30s
  - name: ci-bell
    events: [file-changed, exchange-finished]
    webhook: https://hooks.example.com/apogee
    headers:
      X-Source: apogee                 # a literal header value
    headers-env:
      Authorization: APOGEE_HOOK_TOKEN # the NAME of the variable holding the value
    workspace: ~/code/apogee           # optional; fires in this workspace only
```

| Key | Meaning |
|---|---|
| `name:` | Required, and unique in the list. It is the payload's `hook` field and the name every failure notice reports, so two entries called `notify` would report as one. |
| `events:` | Required, at least one, from the five names below. An unknown name is refused at startup. |
| `command:` | An argv list. `command[0]` is the program; the rest are its arguments, passed word for word. |
| `webhook:` | An absolute `http://` or `https://` URL the payload is POSTed to. |
| `headers:` | Webhook only. Literal header values. |
| `headers-env:` | Webhook only. Header name → the **name** of an environment variable holding its value. |
| `workspace:` | Optional. Scopes the entry to one workspace; unset means every workspace. |
| `timeout:` | Optional Go duration (`10s`, `2m`). Default `30s`. Bounds the command run and the POST alike. |

An entry takes **exactly one** action: `command:` or `webhook:`, never both and never neither.
`headers:` and `headers-env:` belong to a webhook — set either on a command entry and the entry is
refused. The whole block is checked when the file is read, and a malformed entry is a startup
refusal naming the entry rather than a hook that silently never fires.

The block is file-only: there is no flag and no environment variable for it. `/settings` shows the
`hooks:` row and `⏎` on it opens your editor, because no settings row can write a list this shape.

## The five events

| Event | Fires when | Depth |
|---|---|---|
| `exchange-finished` | A top-level exchange closed — the model produced a final answer with no tool call, or the loop abandoned or capped it. `faulted` and `step_capped` in the payload say which. | Top-level only |
| `turn-finished` | Every top-level turn boundary, whatever its outcome. A turn that closed its exchange fires **both** this and `exchange-finished`, in that order. | Top-level only |
| `file-changed` | A workspace write tool succeeded. A delete, copy or move reports its **destination**, because that is the path whose content changed. A refused or failed write fires nothing. | Any, sub-agents included |
| `approval-waiting` | An approval was **raised** and is waiting on you — before you answer, not after. The verdict is deliberately not an event: a hook that learned the answer could do nothing with it. | Any, sub-agents included |
| `error` | A localised, recovered engine fault. A hook's own failure never becomes one of these, because a hook subscribed to `error` would then fire on itself and loop. | Any, sub-agents included |

The turn events are top-level only on purpose: a sub-agent runs the same loop, so a
`turn-finished` hook at every depth would fire once per step of every delegation.

The set is deliberately small and additive. There is no event for a token, a tool call, a
sub-agent phase, a session save, a prune or a usage total.

## The payload

The same JSON document reaches a command on **stdin** and a webhook as the **POST body**. Its
field names are a documented contract — a script reads them by name — and a field that does not
apply to the event is omitted rather than sent empty.

Present on every event:

| Field | Meaning |
|---|---|
| `event` | The event that fired, spelled exactly as `events:` spells it. |
| `hook` | The `name:` of the entry that fired. |
| `time` | When the firing was matched, RFC 3339. |
| `workspace` | The run's workspace: absolute, with symlinks resolved. |
| `depth` | The emitting agent's sub-agent nesting level; `0` is the top-level agent. |
| `turn` | The turn index the event belongs to. |
| `call_id` | The emitting agent's run identity — the id of the `sub_agent` call that spawned it. Absent at depth 0. |
| `schedule` | `{"id": …, "name": …}` — the schedule this firing ran for. Present only on a `/schedule` or daemon firing. |

Per event, added to that block:

| Field | On | Meaning |
|---|---|---|
| `status` | `turn-finished`, `exchange-finished` | The turn's disposition: `turn-complete`, `exchange-complete` or `cancelled`. |
| `faulted` | `turn-finished`, `exchange-finished` | The loop abandoned the turn rather than completing it. |
| `step_capped` | `turn-finished`, `exchange-finished` | The step cap ended the exchange, not the model. |
| `tool` | `file-changed`, `approval-waiting` | The tool that wrote the file, or whose call is waiting. |
| `path` | `file-changed` | The absolute, symlink-resolved path the write landed on. |
| `reason` | `approval-waiting` | Why the approval was required, in the engine's own words. |
| `remedy` | `approval-waiting` | The optional one-line route out of the condition that forced it. |
| `sub_agent_name` | `approval-waiting` | The display name of the child whose call is waiting, when it has one. |
| `scope` | `approval-waiting` | What the call reaches beyond what its arguments name, when that is stated. |
| `source` | `error` | What faulted — a tool name, a mechanism id, or `loop`. |
| `error` | `error` | The fault's message. |

```json
{"event":"file-changed","hook":"fmt","time":"2026-09-06T09:41:00Z",
 "workspace":"/work/repo","depth":1,"turn":2,"call_id":"call-7",
 "tool":"write_file","path":"/work/repo/main.go"}
```

The payload is **not** scrubbed. It goes to your own command or your own URL, which is the same
trust as your screen — so a `reason` or an `error` may quote whatever the run was working on.

## Running a command

A command hook's argv is run **directly**: no shell, no interpolation, no word splitting, no glob,
so a character in a file path can never mean something. When you want a pipeline, write it out:

```yaml
command: ["sh", "-c", "echo \"$APOGEE_HOOK_PATH\" >> ~/changed.log"]
```

Alongside stdin, the payload's headline facts are in the environment, so a one-line script need
not parse JSON at all. A fact the firing does not carry is **omitted** rather than set empty, so
`[ -n "$APOGEE_HOOK_PATH" ]` is a real test:

`APOGEE_HOOK_EVENT` · `APOGEE_HOOK_NAME` · `APOGEE_HOOK_WORKSPACE` · `APOGEE_HOOK_PATH` ·
`APOGEE_HOOK_SCHEDULE_ID` · `APOGEE_HOOK_SCHEDULE_NAME`

The rest of the posture, which is the one an `api-key-cmd:` already runs under:

- **Outside confinement.** A hook is your configuration rather than anything the model chose, so
  it is not sandboxed — even in Auto mode.
- **Fenced at the program.** apogee refuses to run a program that lives somewhere the model could
  have written it, so a `file-changed` hook cannot end up executing the script the agent just
  produced. A bare name is looked up on `PATH`; a name carrying a path separator is resolved
  against apogee's own working directory, exactly as running it would.
- **The environment is inherited whole** — a notifier needs `HOME`, `DISPLAY`, a D-Bus address and
  its agents' sockets. The `APOGEE_HOOK_*` entries are appended last, so they win over an
  inherited variable of the same name.
- **Stdout is discarded**, because nothing a hook prints may reach the model, the conversation or
  the session. Stderr is kept only to quote back: at most 4 KiB is read, and the first ~240
  characters of it, folded onto one line, are appended to the failure notice.
- **A non-zero exit is a failure** and is reported to you. So is a program that cannot be found or
  is refused by the fence.
- **`timeout:` (default `30s`) ends the run.** The process is killed, and a wrapper that left a
  grandchild holding the stderr pipe gets a further two seconds before apogee stops waiting on it.

## Sending a webhook

A webhook hook POSTs the same JSON document to its URL, **once**. There is no retry: a hook is a
post-hoc notification, a retry would fire your endpoint twice for one event, and a queue of failed
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

`workspace:` matches **exactly**, not by prefix: a hook scoped to `~/code/apogee` fires for runs
rooted there and not for a run rooted in a subdirectory of it. Both sides of the comparison — your
filter and the run's own workspace — go through the same resolution first: a leading `~` expanded,
the path made absolute, every symlink evaluated. That is why `/tmp/w` and `/private/tmp/w` are one
workspace on macOS rather than two, and why a filter written either way still matches.

Leave the key out and the entry is active in every workspace.

## When a hook is slow

Each hook gets a worker of its own and a queue of its own, so one slow script cannot delay another
hook, and each hook sees its own events in order. Firing never blocks the agent: matching happens
on the engine's goroutine, everything slow happens on the worker.

The queue holds **64** pending firings. A hook that is being outrun loses the newest firings rather
than growing without limit — an unbounded queue behind a wedged script is a memory leak. The first
drop for a hook is reported once, and the total is stated when the run ends.

At shutdown — the session closing, a firing ending, a `hooks:` edit replacing the list — a hook is
given **five seconds** to finish what it is already running, after which what is still going is
killed. A hook killed that way is apogee's own doing and is not reported as a failure.

## Where a failure shows

A hook's trouble reaches you and nothing else — never the model, never an `error` event, never the
saved session:

| Front-end | Where the notice lands |
|---|---|
| The interactive TUI, and a `/schedule` firing raised inside it | A note in the transcript, **ephemeral**: it is not stored in the session, so it does not come back on a resume claiming a failure nobody has seen since. |
| [`apogee headless`](headless.md) | A line on **stderr**, leaving stdout clean for the model's answer. |
| [`apogee daemon`](daemon.md) | A timestamped line in the daemon's log, like every other line it writes. |

A notice names the hook and the event it was firing on: `hook notify (approval-waiting): exit 3: …`.
The same failure line for one hook is reported **once** — a hook failing every turn would otherwise
bury everything else — and again only after that hook has succeeded in between.

## Live reload

Inside the TUI the list is **live**: save `~/.apogee/config.yaml` (or use `⏎` on the `hooks:` row
in [`/settings`](commands.md#the-settings-screen--settings), which opens your editor) and the
running session swaps its hooks over, draining the old workers in the background. A `/schedule`
firing raised afterwards fires the list the session is running now, not the one apogee launched
with.

`apogee headless` and `apogee daemon` read the list **once**, at start. A daemon watches its
`schedules.yaml`, but not `config.yaml`, so a `hooks:` edit reaches it at its next restart.
