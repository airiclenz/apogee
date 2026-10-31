# Reactions — commands, webhooks and MCP tools on what a session did

`reactions:` in `~/.apogee/config.yaml` is a list of Reactions apogee runs when something happens
in a session — what other tools call a hook. Each entry names the moments it fires on and the
action it takes: `run:` — an argv list run directly, a URL the moment is POSTed to as JSON, or a
tool on one of your MCP servers — after the moment; `advise:` — a command whose standard output, a
webhook whose reply, or an MCP tool whose answer the model reads inside a fence apogee writes: as a
trailer on a tool result, or ahead of your next message when a session starts or after a compaction
fold; or `gate:` — a command, webhook or MCP tool asked whether a tool call may run — before it.
Every key takes the same three shapes, an argv list, a webhook mapping or an MCP mapping; the key
decides what the answer is worth. The list is empty by default, so a fresh install runs
nothing and reports nothing.

A `run:` entry is told what **already happened**. It cannot delay a turn, answer an approval, or
change anything about the run it is watching, and nothing it prints, returns or answers reaches
the model, the conversation or the saved session — its failure is yours to see and nobody else's.
It is a [Reaction](../../.capcom/knowledge/glossary.md) of **user** origin and **observe** class: it watches. The
other two keys are the same origin at the two classes the loop **waits** on. An `advise:` entry
(class **advise**) runs after a tool call finished, and what it prints — or, as a webhook or an MCP tool, answers —
is appended to that call's result inside a fence apogee writes, so the model reads it as advice from you and not as the
tool's own output; on `session-started` or `compaction-finished` it runs in the background instead,
and its answer is fenced ahead of the text of the next user message the model reads. A `gate:` entry (class **gate**) is asked, before a tool call runs, whether the
call may — and it can say `deny`, or `ask` to put the call to you, but never approve a call on
your behalf. A Floor guard is the engine's own `shape (view)` Reaction — it fires on a Moment
inside the loop and rewrites what the model sees — and the engine's own `advise` Reaction is the
[context-fill notice](configuration.md#context-fill-notice), which appends its line to a tool
result the way an `advise:` entry does; yours fires on your own machine, as your
configuration rather than a model action. Of the five in-loop seams, a `gate:` entry reacts at
`pre-tool-exec` and an `advise:` entry at `post-tool-result` (or its `file-changed` narrowing);
the other three take no user entry. Beyond the seams, an `advise:` entry takes two notices as well —
`session-started`, to brief a session, and `compaction-finished`, to brief it again after its
history was folded. Every notice below reports a thing that is over.
([ADR 0076](../../.capcom/knowledge/decisions/adr-0076.md) is the
decision — the origin and class vocabulary, and which cells a user entry may take; ADR 0073,
which it supersedes in part, is the reasoning behind the `run:` posture.)

The same list is fired by every front-end — the interactive TUI, a `/schedule` firing inside it,
[`apogee headless`](headless.md) and an [`apogee daemon`](daemon.md) firing — from one library, so
an entry means the same thing wherever the run was started from — save an MCP mapping, which only
the interactive TUI's own session calls (see [Calling an MCP tool](#calling-an-mcp-tool)).

A repository's own [Project config](configuration.md#a-projects-own-config--apogeeconfigyaml)
(`.apogee/config.yaml` at its Project root) may carry a `reactions:` list too, in the same shape.
Each of its entries is **proposed** and arms nothing until you adopt it, and adoption pins its `id`,
its `run:`, `advise:` and `gate:` values and the content of every project file they name — so an
edit to one of those, or to a script it runs, asks you again, while an edit to `on:`, `timeout:`,
`enabled:` or `workspace:` does not. An entry whose `id` is one of yours is dropped with a notice:
yours wins. The sections below say where an adopted project reaction is held to more than yours is;
[A project's own config](configuration.md#a-projects-own-config--apogeeconfigyaml) has the whole
list, and what adopting a script means. A `session-started` entry armed before the session's first
prompt — a project entry adopted in the start-up pane, in `/settings` or with `apogee project adopt`
from another terminal, or one of your own added by a [live reload](#live-reload) — fires for that
session, once, with its `start`; one armed after the first prompt fires from the next session
boundary on. Re-arming an entry that already fired for the session does not fire it again. The same
holds for an `advise:` entry on `session-started`: armed before the first prompt, it starts at once
and its briefing lands in that prompt; armed later, it briefs the next session. A
`compaction-finished` entry has no window — it fires at the next fold after it is armed.

## An entry

```yaml
# ~/.apogee/config.yaml
reactions:
  - id: notify                          # unique; it is what a failure notice reports
    on: [approval-requested]            # one or more of the thirteen notices
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
  - id: no-force-push
    on: [pre-tool-exec]                 # a gate: reacts here and nowhere else
    gate: ["check-tool-call"]           # answers allow, deny or ask on its first stdout line
    timeout: 2s                         # optional; a gate's default is 5s
  - id: unformatted
    on: [file-changed]                  # an advise: also takes post-tool-result, session-started, compaction-finished
    advise: ["sh", "-c", "gofmt -l \"$APOGEE_REACTION_PATH\""]  # its stdout reaches the model
    timeout: 5s                         # optional; an advise's default is 10s
  - id: policy-server
    on: [pre-tool-exec]
    gate:                               # the same mapping run: takes; the reply body is the answer
      url: https://example.invalid/apogee/gate
      headers-env:
        Authorization: MY_WEBHOOK_TOKEN
```

| Key | Meaning |
|---|---|
| `id:` | Required, and unique in the list. It is the payload's `reaction` field and what every failure notice reports, so two entries called `notify` would report as one. One of the seven Floor-guard keys is refused as an id, and so is `context-fill-notice`, the engine's own built-in advise reaction — it is switched with its own top-level key, not with an entry here. |
| `on:` | Required, at least one. A `run:` entry reacts to the thirteen notices below; an `advise:` entry reacts at `post-tool-result`, `file-changed`, `session-started` or `compaction-finished`; a `gate:` entry reacts at the `pre-tool-exec` **seam** and nowhere else. A spelling outside that vocabulary is refused at startup, and so is a Moment a key cannot take — a seam under `run:`, a notice other than `file-changed`, `session-started` or `compaction-finished` under `advise:`, anything but `pre-tool-exec` under `gate:` — by a sentence naming the key and what it does take: `advise: reacts at post-tool-result, file-changed, session-started or compaction-finished; "turn-finished" is none of them`. The list is shared by every action key the entry spells, so each of them must be able to take all of it. |
| `run:` | The entry's observe action, in any of three shapes. A **list** is an argv: `run[0]` is the program, the rest are its arguments, passed word for word. A **mapping** `{url:, headers:, headers-env:}` is a webhook the payload is POSTed to. A mapping `{mcp: {server:, tool:, args:, payload-arg:}}` calls a tool on one of your MCP servers (see [Calling an MCP tool](#calling-an-mcp-tool)). `advise:` and `gate:` take the same three shapes; anything else — a bare string, a number — is refused at startup by a sentence naming the key: `reaction <id>: run: is an argv list, a webhook mapping {url:, headers:, headers-env:} or an mcp mapping {mcp: {server:, tool:, args:, payload-arg:}}`. |
| `workspace:` | Optional. Scopes the entry to one workspace; unset means every workspace. |
| `timeout:` | Optional Go duration (`10s`, `2m`). Default `30s` for `run:`, `10s` for `advise:`, `5s` for `gate:`. Bounds the command run and the POST alike, and one `timeout:` binds every Reaction its entry arms. |
| `enabled:` | Optional. `enabled: false` **parks** an entry — it stays in the file and is dropped when the file is read, so nothing arms it and the `/settings` summary does not count it. |
| `gate:` | The entry's gate action, in the three `run:` shapes: an argv list, run directly like a `run:` list, a webhook mapping `{url:, headers:, headers-env:}` POSTed to like a `run:` webhook, or an MCP mapping called like a `run:` one — each is asked before a tool call runs, and its `on:` is `[pre-tool-exec]`. The call reaches a command as JSON on **stdin** and a webhook as the **POST body** (`event`, `reaction`, `tool`, `arguments` and the shared fields below), and it answers on **stdout**, in the **reply body** or in the **tool's text**: the first line is `allow`, `deny` or `ask`, and any later lines are the reason, kept to its first 240 runes. It runs as an approval stage ahead of you — under `bypass:` too — with a default `timeout:` of `5s`. The first `deny` ends the call: the model reads `tool call denied by reaction <id>` and never the reason. The first `ask` forces the approval prompt, which reads `reaction <id> asks: <reason>`; in [`apogee headless`](headless.md) and the [daemon](daemon.md) nobody is there to answer, so the call is denied as `tool call denied by approver`. A gate that printed nothing, printed something else, exited non-zero, timed out or crashed — or a webhook that answered with anything but a 2xx, or an MCP call that failed — counts as `ask`, never as `allow`, and an `allow` changes nothing about what the mode already decided. On a `sub_agent` call an `ask` is deferred: the child inherits the gate and is asked on the calls that actually do something. |
| `advise:` | The entry's third action, in the three `run:` shapes: an argv list run directly like the others, a webhook mapping `{url:, headers:, headers-env:}` or an MCP mapping — each fires **after** a tool call finished (`post-tool-result`, `file-changed`), at a session's start (`session-started`) or after a compaction fold (`compaction-finished`), and its `on:` is any of those four. The firing reaches a command as JSON on **stdin**, a webhook as the **POST body** and an MCP tool under its `payload-arg:` (`event`, `reaction`, the shared fields below, and `tool`, `arguments` and `result` after a tool call, `start` at a session's start, `fold` after a fold), and the command's **stdout**, the webhook's **reply body** or the MCP tool's **text** — redacted, capped at 8 KiB — is fenced where the model reads it and the saved session never carries it: appended to that call's result after a tool call, set ahead of the next user message's text at a session's start or after a fold. A bare string is refused: `reaction <id>: advise: is an argv list, a webhook mapping {url:, headers:, headers-env:} or an mcp mapping {mcp: {server:, tool:, args:, payload-arg:}}`. Default `timeout:` `10s`; switched off under `bypass:`. See [Advising the model](#advising-the-model). |

An entry may spell more than one action key. Each arms a Reaction of its own under the entry's one
`id:`, and the whole `on:` list has to suit each of them: `run:` beside `advise:` on
`[file-changed]` is the useful pair — a notifier and an adviser on one edit — and so is the pair on
`[session-started, compaction-finished]`, a logger beside a briefing, while `run:` beside
`gate:` is refused, because `pre-tool-exec` is a seam no `run:` takes and no notice is one. An
entry that spells no action key at all earns the `run:` sentence. Inside the
webhook mapping — under whichever key carries it — the three keys above are the whole vocabulary:
a misspelt `header-env:` is a startup refusal naming that key (`advise: is an argv list, a webhook
mapping {url:, headers:, headers-env:} or an mcp mapping {mcp: {server:, tool:, args:,
payload-arg:}}`) and not a token that never gets sent; the same holds for the four keys inside an
MCP mapping's `mcp:`. The whole block is checked when the file is
read, and a malformed entry is a startup refusal naming the entry rather than a Reaction that
silently never fires: a Moment listed twice under `on:`, an argv whose first element is blank, a
`url:` that is not an absolute `http://` or `https://` URL with a host, and a `headers-env:` value
that is blank are each refused.

The block is file-only: there is no flag and no environment variable for it. `/settings` shows a
read-only `reactions:` row counting what is armed, and `⏎` on it opens your editor, because no
settings row can write a list this shape.

## The thirteen notices

Eight report something the session did; five report that one of the loop's own **seams** finished
passing.

| Notice | Fires when | Depth |
|---|---|---|
| `exchange-finished` | A top-level exchange closed — the model produced a final answer with no tool call, or the loop abandoned or capped it. `faulted` and `step_capped` in the payload say which. | Top-level only |
| `turn-finished` | Every top-level turn boundary, whatever its outcome. A turn that closed its exchange fires **both** this and `exchange-finished`, in that order. | Top-level only |
| `file-changed` | A workspace write tool succeeded. A delete, copy or move reports its **destination**, because that is the path whose content changed. A refused or failed write fires nothing. | Any, sub-agents included |
| `approval-requested` | An approval was **raised** and is waiting on you — before you answer, not after. | Any, sub-agents included |
| `approval-decided` | That same approval reached its verdict, which the payload carries as `decision` — or an [allow rule](configuration.md) answered an ordinary approval in your place, with no `approval-requested` before it. | Any, sub-agents included |
| `error` | A localised, recovered engine fault. A Reaction's own failure never becomes one of these, because an entry subscribed to `error` would then fire on itself and loop. | Any, sub-agents included |
| `session-started` | A session started, once per start: a fresh session, a `--resume`, every [`apogee headless`](headless.md), [daemon](daemon.md) or `/schedule` run, `/clear`, `/new`, a `/sessions` resume and `/fork`. The payload's `start` says which — `new`, `resume`, `clear` or `fork`. A sub-agent is not a session start and never fires it. An `advise:` entry on it briefs the session (see [Advising the model](#advising-the-model)). | Top-level only |
| `compaction-finished` | A [compaction](configuration.md#keeping-the-window-from-overflowing--compaction-and-pruning) fold ran and replaced the conversation, once per fold — whether the context estimate, an emergency (a request that would not fit the window) or `/compact` started it. The payload's `fold` says which — `auto`, `emergency` or `manual`. A fold that failed, was cancelled or replaced nothing fires none, and a sub-agent's fold never fires it. An `advise:` entry on it briefs the model again after the fold. | Top-level only |
| `pre-request-finished` | The `pre-request` seam's pass finished: the outgoing request is as the loop will send it. Full working value; only serialized when you subscribe. | Top-level only |
| `post-response-finished` | The `post-response` seam's pass finished, on the model's answer. Full working value; only serialized when you subscribe. | Top-level only |
| `pre-tool-exec-finished` | The `pre-tool-exec` seam's pass finished, on a call about to run. Full working value; only serialized when you subscribe. | Top-level only |
| `post-tool-result-finished` | The `post-tool-result` seam's pass finished, on a result before the model sees it. Full working value; only serialized when you subscribe. | Top-level only |
| `history-rewrite-finished` | The `history-rewrite` seam's pass finished, on the conversation. Full working value; only serialized when you subscribe. | Top-level only |

The turn and seam notices are top-level only on purpose: a sub-agent runs the same loop, so one at
every depth would fire once per step of every delegation.

`session-started` is top-level for a different reason: a delegation is a turn of the session that
spawned it, not a session of its own, so a sub-agent never fires it. It fires at the boundary,
ahead of the session's first turn, so its payload reads `depth` `0` and `turn` `0` and carries no
`call_id`. Its `start` is `new` for a session apogee built — a fresh start, a `--resume` start, and
every [`apogee headless`](headless.md), [daemon](daemon.md) and `/schedule` run — `resume` for a
`/sessions` resume, `clear` for `/clear` and `/new`, and `fork` for `/fork`. A `/schedule` or daemon
run carries its `schedule` like every other notice it fires. A webhook on it is POSTed the same
document; a command finds `start` in `APOGEE_REACTION_START` as well:

```yaml
reactions:
  - id: session-log
    on: [session-started]
    run: ["sh", "-c", "echo \"$(date -u +%FT%TZ) $APOGEE_REACTION_START $APOGEE_REACTION_WORKSPACE\" >> ~/apogee-sessions.log"]
```

`compaction-finished` is top-level for the same reason a turn notice is: a sub-agent folds its own
history, and that is no event of the session's. It fires once the fold has replaced the
conversation, so its payload reads `depth` `0` and carries no `call_id`; its `fold` says what
started it — `auto` when the context estimate outgrew its allocation between exchanges,
`emergency` when a request would not fit the window mid-exchange, `manual` for `/compact` — and a
command finds it in `APOGEE_REACTION_FOLD` as well.

A seam-closing notice reports a pass that is **over** — reading one changes nothing about it. The
projection of the working value is built while the engine is still handing the moment out, and only
when an entry actually subscribes: a seam nobody watches costs one map lookup and no work at all.

The set is deliberately small and additive. There is no notice for a token, a tool call, a
sub-agent phase, a session save, a prune or a usage total.

## The payload

The same JSON document reaches a command on **stdin**, a webhook as the **POST body** and an MCP
tool under its `payload-arg:`. Its
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
| `call_id` | The id of the `sub_agent` call that spawned the emitting agent. Absent at depth 0. It names the spawning call, not a unique run: the id is the model's or the server's to choose, and two delegations of one fan-out can share it. |
| `schedule` | `{"id": …, "name": …}` — the schedule this firing ran for. Present only on a `/schedule` or daemon firing; `name` is omitted for an unnamed schedule, and `APOGEE_REACTION_SCHEDULE_NAME` is then unset. |

Per notice, added to that block:

| Field | On | Meaning |
|---|---|---|
| `status` | `turn-finished`, `exchange-finished` | The turn's disposition. On `turn-finished` it is `turn-complete`, `exchange-complete` or `cancelled`; on `exchange-finished`, which fires only when the exchange closed, it is always `exchange-complete`. |
| `faulted` | `turn-finished`, `exchange-finished` | The loop abandoned the turn rather than completing it. Written only when true, so a script treats its absence as `false`. |
| `step_capped` | `turn-finished`, `exchange-finished` | The step cap ended the exchange, not the model. Written only when true, like `faulted`. |
| `tool` | `file-changed`, the two approval notices | The tool that wrote the file, or whose call the approval is about. |
| `path` | `file-changed` | The absolute, symlink-resolved path the write landed on. |
| `reason` | The two approval notices | Why the approval was required, in the engine's own words. |
| `remedy` | The two approval notices | The optional one-line route out of the condition that forced it. |
| `sub_agent_name` | The two approval notices | The display name of the child whose call it is, when it has one. |
| `scope` | The two approval notices | What the call reaches beyond what its arguments name, when that is stated. |
| `decision` | `approval-decided` | The verdict: `allow`, `deny` or `allow-for-session`, or `allowed-by-rule` when an allow rule answered the approval instead of you. |
| `start` | `session-started` | How the session started: `new`, `resume`, `clear` or `fork` (see [The thirteen notices](#the-thirteen-notices)). |
| `fold` | `compaction-finished` | Which trigger ran the fold: `auto` (the context estimate), `emergency` (a request that would not fit the window) or `manual` (`/compact`). |
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

### The seam document

An `advise:` or `gate:` command reads — and a webhook under either key is POSTed, and an MCP tool
under either is sent under its `payload-arg:` — the **same**
document cut to the call itself rather than to a notice about it — or, for an `advise:` entry on
`session-started` or `compaction-finished`, where there is no call, cut to the boundary or the fold.
It opens with the shared block above — `event`, `reaction`,
`time`, `workspace`, `depth`, `turn` and `call_id`, spelled the same — and then carries:

| Field | On | Meaning |
|---|---|---|
| `tool` | every firing on a tool call | The tool whose call is pending (`pre-tool-exec`) or whose result came back (`post-tool-result`, `file-changed`). |
| `arguments` | every firing on a tool call | The JSON object the model produced for the call, as it stands. |
| `result` | `post-tool-result`, `file-changed` | `{"content": …, "is_error": …}` — what the tool returned, as the model would read it; `is_error` is always written, so a script can branch on it. Absent at `pre-tool-exec`, where nothing has run yet. |
| `path` | `file-changed` | The absolute, symlink-resolved path the write landed on. |
| `start` | `session-started` | How the session started: `new`, `resume`, `clear` or `fork`. |
| `fold` | `compaction-finished` | What started the fold: `auto`, `emergency` or `manual`. |

```json
{"event":"file-changed","reaction":"unformatted","time":"2026-09-12T09:41:00Z",
 "workspace":"/work/repo","depth":0,"turn":3,
 "tool":"write_file","arguments":{"path":"main.go","content":"…"},
 "result":{"content":"…","is_error":false},"path":"/work/repo/main.go"}
```

`event` is the Moment the **entry** subscribed to, not the seam it ran at: a `file-changed` entry
reads `file-changed` and a `path`, an entry on `post-tool-result` reads that Moment and no path.
A `session-started` or `compaction-finished` document carries no `tool`, `arguments` or `result` —
nothing has been called — and `depth` is always `0`, since neither fires below the top level:

```json
{"event":"session-started","reaction":"prime","time":"2026-10-21T09:41:00Z",
 "workspace":"/work/repo","depth":0,"turn":0,"start":"new"}
```

There is no `value`, no `seam` and no `schedule` — the call, the boundary or the fold is the
document. The same headline
facts are in the command's environment as `APOGEE_REACTION_EVENT`, `APOGEE_REACTION_NAME`,
`APOGEE_REACTION_WORKSPACE` and, on `file-changed`, `APOGEE_REACTION_PATH`, on `session-started`,
`APOGEE_REACTION_START`, and on `compaction-finished`, `APOGEE_REACTION_FOLD`; the two schedule
variables belong to the notices alone. Nothing is interpolated into the argv: the document is on
stdin and in the environment, and nowhere else.

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
`APOGEE_REACTION_PATH` · `APOGEE_REACTION_SCHEDULE_ID` · `APOGEE_REACTION_SCHEDULE_NAME` ·
`APOGEE_REACTION_START` · `APOGEE_REACTION_FOLD`

The rest of the posture, which is the one an `api-key-cmd:` already runs under:

- **Outside confinement.** An entry here is your configuration rather than anything the model
  chose, so it is not sandboxed — even in Auto mode.
- **In apogee's own working directory**, never the workspace — the payload names the workspace by
  path, and so does `APOGEE_REACTION_WORKSPACE`. This holds for `advise:` and `gate:` too.
- **Fenced at the program.** apogee refuses to run a program that lives somewhere the model could
  have written it, so a `file-changed` entry cannot end up executing the script the agent just
  produced. A bare name is looked up on `PATH`; a name carrying a path separator is resolved
  against apogee's own working directory, exactly as running it would.
- **The environment is inherited whole** — a notifier needs `HOME`, `DISPLAY`, a D-Bus address and
  its agents' sockets. The `APOGEE_REACTION_*` entries are appended last, so they win over an
  inherited variable of the same name.
- **Stdout is discarded**, because nothing an observe Reaction prints may reach the model, the
  conversation or the session. Stderr is kept only to quote back: at most 4 KiB is read, and the
  first 240 runes of it, folded onto one line and ending in `…` when cut, are appended to the
  failure notice.
- **A non-zero exit is a failure** and is reported to you. So is a program that cannot be found or
  is refused by the fence.
- **`timeout:` (default `30s`) ends the run.** The command's whole process group is killed, so a
  wrapper's grandchild dies with it rather than surviving the deadline — and a process the command
  leaves behind dies when the command does, on a clean exit too; `setsid` detaches it from the
  group and is the one way to leave something running. A detached process still holding the stderr
  pipe gets a further two seconds before apogee stops waiting on it.

That posture is for your own entries. A `run:` command an adopted
[Project config](configuration.md#a-projects-own-config--apogeeconfigyaml) entry spells is the
repository's text, not yours, so it runs as an `advise:` or `gate:` command does (see
[Where an `advise:` or `gate:` command runs](#where-an-advise-or-gate-command-runs)): inside the
workspace fence when `confine-to-workspace` is on and not at all on a host that cannot build it,
with the execution tools' credential scrub, and under that door's ceilings — so its failure reads
`apogee: <program> exited 3: …`. It runs in the **Project root**, not apogee's working directory,
and a relative program resolves against that root. It may run a script inside the root —
`run: ["./scripts/notify.sh"]` — only because adoption pinned that script's content: the pinned
files are checked again before every spawn, and one that changed, went missing or is newly named
refuses that run with a notice naming the file, and the entry is proposed again: in the TUI that
refusal re-reads the config, so the adoption pane asks about the new content at the next idle
moment — no Project config edit or restart needed — and adopting it arms the reaction again. A pinned file that cannot be read is refused the same
way, but the entry is then dropped with a notice naming the file instead of being asked about again. An
adopted script runs whatever it contains, so read it, and what it calls, before you adopt it.

## Sending a webhook

A webhook `run:` POSTs the same JSON document to its `url:`, **once**. There is no retry: this is a
post-hoc notification, a retry would fire your endpoint twice for one moment, and a queue of failed
firings would outlive the run they belong to. A webhook under `advise:` or `gate:` sends the
identical request — the [seam document](#the-seam-document) as the body, the same headers, the
same single attempt — while the loop waits on it; what differs is that its **reply body is read**,
as the sections below describe.

apogee sets `Content-Type: application/json` and `User-Agent: apogee` first, then your `headers:`,
then your `headers-env:` — so your own entries override apogee's defaults if you name them.
`headers-env:` reads the variable at **send** time, so a token rotated in the shell that launched
apogee is picked up by the next firing, and the secret never has to sit in the config file. This
is the `api-key-env:` spelling: the value is the **name** of the variable, and there is no `${VAR}`
interpolation anywhere in this file.

Headers are resolved before the request goes out, so a `headers-env:` variable that is not set
fails without your endpoint ever hearing from us; the failure names the header and the variable and
never the value. Anything that is not a 2xx is reported as `HTTP <code>`. A `run:` webhook's
response body is drained (up to 64 KiB), and then dropped — nothing
a `run:` webhook answers reaches the model; an `advise:` webhook's body is the advice and a
`gate:` webhook's first line is the verdict, read on the same terms as a command's stdout. A
transport failure is reported **without** the URL, because a webhook URL is exactly the kind of
thing that carries a token in its path or query. `timeout:` bounds the whole POST.

Every webhook — `run:`, `advise:` and `gate:` alike — is fenced by your
[`url-safety:`](configuration.md#what-the-network-tools-may-reach--url-safety) lists: a host
`deny-hosts` names, or one off a non-empty `allow-hosts`, is refused before anything is dialled, and
the failure says it was refused by url-safety. The private-range floor is **off** for a webhook,
as it is for an MCP endpoint: the address is one you wrote in your own config — or adopted from a
Project config whose pane named it — so a loopback or LAN receiver still gets its POST, and the
connection is pinned to that endpoint's own resolved addresses. A redirect is **never followed**: a
3xx comes back as itself and is reported as `HTTP <code>` — a failed post — so neither the body nor
your `headers-env:` secrets ever reach a host you did not name. A webhook that redirects must be
configured at the URL it redirects to. A running session's own webhooks keep the lists it started
with: an edit to `url-safety:` reaches them at the next start. A `/schedule` firing raised after the
edit composes its webhooks from the settings the session is running then, so it follows the edit.
`apogee headless` reads the lists once, at start, like the rest of its configuration, and that
includes a project's `url-safety.deny-hosts`, which a [Project
config](configuration.md#a-projects-own-config--apogeeconfigyaml) may add to. `apogee daemon` reads
the global lists once, at start, and each daemon firing adds the `url-safety.deny-hosts` of its own
workspace's Project config, read at that firing.

## Calling an MCP tool

A mapping whose one key is `mcp:` calls a tool on one of your
[MCP servers](configuration.md#external-mcp-servers--mcp-servers) when its Moment fires. It takes
the same place a command or a webhook would, under any of the three keys, and it is how a session
can open knowing something only a server can tell it — here, which model llama-launcher has
loaded:

```yaml
# ~/.apogee/config.yaml
reactions:
  - id: loaded-model
    on: [session-started]
    advise:
      mcp:
        server: llama-launcher
        tool: status
```

`server:` is the `name` of an `mcp-servers:` entry and `tool:` is that server's own name for the
tool — `status`, not the prefixed `llama-launcher__status` the model is offered it as. Both are
required. `args:` is an optional mapping sent as the tool's arguments, word for word, on every
call: nothing in it is filled in from the Moment. `payload-arg:` is optional too, and names the
argument the firing's document travels under — the same JSON a command reads on stdin, sent as an
object, not as a string. Leave it out and the tool sees `args:` alone. A `payload-arg:` that names a
key `args:` already holds is refused at startup, so the document never overwrites a static
argument; so is a blank `server:` or `tool:`, and a key inside `mcp:` other than those four.

**Writing the entry is the approval.** The call never reaches the Approver: you wrote it, so no
prompt is raised for it in any mode, and an MCP tool that would ask before the model could call it
in Auto is called here without asking. It is the trust you already give a command you put in
`run:` — the entry is yours, not the model's. It spawns nothing, so `confine-to-workspace` has
nothing to fence: the call travels over the connection apogee already holds to that server, under
the entry's `timeout:` (or its key's default).

**The answer is the tool's text.** The text blocks the tool returns, joined with newlines, stand
where a command's stdout would: dropped under `run:`, the advice under `advise:` (redacted, capped
and fenced like any other — see [Advising the model](#advising-the-model)), the verdict's first line
under `gate:`. A block that is not text is left out, and a tool that returns no text gives empty
advice, which injects nothing. A call that fails is the handler failing, as a command's non-zero
exit is: the tool's own `isError` result (reported with its text), a call that outlived the
deadline, a server that is not connected — never dialled, or left undialled because its adoption
pin moved — and a tool that server does not advertise. An advise that failed contributes nothing
and a gate that failed asks; [Where a failure shows](#where-a-failure-shows) says where you read
it.

**A project's entry may name only its own servers.** A
[Project config](configuration.md#a-projects-own-config--apogeeconfigyaml) reaction may call a tool
only on a server that same Project config declares — not on one of yours, and not on a project
server dropped because one of yours already has its name. An entry that names any other server is
dropped when the file is read, with a notice saying `it names mcp server "<name>", which this
Project config does not declare`, and is never offered for adoption; the rest of the Project config
still applies. Once adopted, the entry's call reaches its server only after you adopted that server
too — until then it fails as not connected. One of your own entries may name any server the session connected, a project's included. Adoption pins the
whole `mcp:` mapping, so an edit to its server, tool or arguments asks you again. The mapping
names no file, so no file change re-asks it; if the server's own pin moves, apogee asks about the
server again, and the entry's calls fail as not connected until you adopt it.

**Only the interactive TUI's session calls one.** It is the one front-end that connects your MCP
servers. [`apogee headless`](headless.md), an [`apogee daemon`](daemon.md) firing, a `/schedule`
firing raised inside the TUI and `apogee probe context` connect none, and there an MCP entry is
inert rather than a startup refusal, with one notice per entry: a `run:` or `advise:` one is
dropped before anything arms it — `reaction "loaded-model": its mcp: handler is inert here — MCP
servers are not connected in this run` — and a `gate:` one stays armed and asks on every call it
sees, which an unattended run denies: `reaction "<id>": its mcp: gate cannot run here — MCP servers
are not connected in this run, so every call it gates is refused`. In the TUI, a `run:` entry that
fires before the session has connected its servers fails as `MCP servers are not connected yet`.

## Advising the model

An `advise:` entry is the one kind of Reaction whose output the model reads. It reacts at four
Moments, and `on:` takes any of them. Two follow a tool call: there the entry runs **while the loop
waits** — after the call has finished and before the model sees the result — and what it prints on
stdout is appended to that result:

- `post-tool-result` — every tool call, once its result is in hand; `result.is_error` says whether
  the call failed.
- `file-changed` — `post-tool-result` narrowed to a successful workspace write: the entry fires on
  the `write_file` that landed and not on the `list_dir` after it, and `path` names the file. An
  entry listing `file-changed` alone is silent on every other call.

The other two have no tool result to append to. There the entry runs **in the background**, and
its answer is fenced **ahead of the text of the next user message** the model reads — inside that
message, never a message of its own, and never in the system prompt:

- `session-started` — the session's briefing. The entry starts at the
  [session boundary](#the-thirteen-notices) — a fresh start or `--resume`, `/clear`, `/new`, a
  `/sessions` resume, `/fork`, and every [`apogee headless`](headless.md), [daemon](daemon.md) and
  `/schedule` run — and its answer lands in the session's next user message, ahead of your prompt.
- `compaction-finished` — the briefing again, after a [compaction](configuration.md#keeping-the-window-from-overflowing--compaction-and-pruning)
  fold took the history it was in. The entry starts as the fold **begins**, so it runs while the
  summary is being written, and once the fold has replaced the conversation its answer lands ahead
  of the next user-role message: your next prompt after a fold between exchanges (`auto` or
  `manual`), or — after an `emergency` fold mid-exchange — the note apogee appends to tell the
  model its history was compacted, so the request that is sent again already carries it. Never the
  summary. A fold that failed, was cancelled or folded nothing drops the run's answer unread.
  Because the fold keeps the session's first user message as it was, a briefing an earlier
  boundary or fold left there would still stand; the new answer **replaces** that same entry's
  earlier fence rather than landing beside it, and leaves every other entry's fence alone. An
  entry whose answers on both Moments land in one message keeps only the later one.

Either way that message waits only for what is left of each entry's `timeout:` — the run started
at the boundary or the fold, not when you sent the prompt — so a quick script costs the prompt
nothing. A cancel (`Esc`) during that wait ends it and leaves the runs for your next prompt, and
an exchange that is aborted puts the answers it carried back for the prompt after it.

An `advise:` entry on these two notices is apogee's counterpart of another agent's session-start
hook. [capcom](https://github.com/airiclenz/capcom)'s `prime` prints an
issue register's briefing — the ready issues, the ones in progress, the locks held — and one entry
briefs every session with it and briefs it again after every fold:

```yaml
reactions:
  - id: prime
    on: [session-started, compaction-finished]
    advise: ["capcom", "prime"]
  - id: recent-commits
    on: [session-started]
    advise: ["sh", "-c", "git -C \"$APOGEE_REACTION_WORKSPACE\" log --oneline -10"]
```

The second entry is the generic shape: any script that prints a few facts about where the work
stands. Both run in apogee's own working directory, like every reaction command (see
[Running a command](#running-a-command)), which is why the generic one names the workspace through
`APOGEE_REACTION_WORKSPACE`; `capcom prime` finds its register from the directory it runs in, so
it suits a session started inside the repository — or a repository's own
[Project config](configuration.md#a-projects-own-config--apogeeconfigyaml), whose adopted entries
run in the Project root.

The command is run like a `run:` list — directly, no shell — with the
[seam document](#the-seam-document) on **stdin** and the `APOGEE_REACTION_*` facts in its
environment. A webhook mapping under `advise:` is POSTed like a `run:` webhook, the same document
as the body, and its **reply body** stands where the command's stdout would — read up to the cap
below, so a server that streams without end cannot hold the turn's memory. An MCP mapping under
`advise:` is called with the document under its `payload-arg:`, and the tool's **text** stands
there in turn (see [Calling an MCP tool](#calling-an-mcp-tool)). What any of them answers goes
through three steps before the model sees it:

1. **Redaction.** Every value of a configured secret — the variables your `api-key-env:` entries,
   your webhook `headers-env:` entries and your `mcp-servers:` entries' `headers-env:` and
   `client-secret-env:` keys name — is replaced by `[redacted]`, so a script that
   echoes its own config file cannot hand the model a token the credential scrub kept out of the
   child's environment. apogee's own `APOGEE_API_KEY` is scrubbed from that environment but is
   not among the values redacted here.
2. **The cap.** Output past **8 KiB** is cut on a character boundary and ends in
   `[advice truncated at 8 KiB]`; nothing spills to a file.
3. **The fence.** After a tool call the text is appended to the tool result, after a blank line,
   as

   ```
   [advice — reaction unformatted (user origin) at post-tool-result, turn 3]
   main.go
   [end advice — unformatted]
   ```

   Every word of the header is written by apogee from the firing — the entry's id, its origin,
   the seam, the turn — and never from what the command printed, so a script that prints a
   header of its own lands inside the fence, not beside it. The header names the **seam** the
   advice was fenced at, which is `post-tool-result` for a `file-changed` entry too; the stdin
   document's `event` is what names the entry's own Moment (`file-changed`). At a session's start
   or after a fold the same fence, naming `session-started` or `compaction-finished`, stands at
   the front of the user message, a blank line before your own text.

The fence is what the model reads, and only the model: the **session record is written from the
result, or the message, as it stood before the fence**, so a resumed session carries no advice, a
replay never re-reads a stale timestamp, and the transcript you save is the tool's own words and
your own prompt. The firing is
booked all the same — in [`apogee headless`](headless.md) a `reaction_fired` line with
`"action": "advise"` carries the text as its `detail`, and the TUI's `/advice` pane lists it under
the turn it landed in — so what the model was told is always
attributable to the entry that told it. A gate's every answer is booked the same way, with
`action` the verdict — `allow`, `deny` or `ask` — and `detail` the reason, or
`deferred to the child's calls` for an `ask` on a delegation.

Advise is **fail-open in every direction**, and each of them costs the turn nothing: a command
that prints nothing — or a webhook whose reply body is empty, or an MCP tool that returns no
text — injects no fence, so an entry that
only sometimes has something to say is silent the rest of the time; one that failed, timed out
(default `timeout:` `10s`) or could not be spawned — or a webhook that answered with anything but
a 2xx, could not be reached, or names a `headers-env:` variable that is not set, or an MCP call
that failed — contributes nothing and is reported to you, each time it fails; a `file-changed` entry on a call that changed
no file neither runs nor reports. A `session-started` or `compaction-finished` run is reported when
the message it was due on joins it, not when it fails: a run still going then is a timeout. Under
`bypass:` the
entry is switched off entirely, like every other Reaction that shapes what the model sees — the
engine's own [context-fill notice](configuration.md#context-fill-notice) included, even with its
key on — so a Bypass run is the bare loop: the Floor guards and the structural reducers, with
nothing a Reaction shaped. (A sub-agent's step-budget
notice is not a Reaction but part of its cap, so it stays.)

Write a script that **returns facts, not imperatives**: the files `gofmt` would rewrite, the
failing test's name, the line a linter flagged. The fence tells the model who spoke; it does not
make the model obey, and a smaller model handed an order inside a tool result will often follow
it at the cost of the task it was on. Advice is also text the model must read, and pay tokens
for, on every call the entry fires on — print nothing when there is nothing to say. A briefing
costs one fenced block per entry at each session boundary and each fold, up to 8 KiB, and it stays
in the context window — every later request carries it — until a fold takes it or replaces it, so
a short briefing is a cheap one.

## Gating a tool call

A `gate:` entry is an approval stage of your own, run **before** a tool call and ahead of any
question apogee would put to you. It reacts at `pre-tool-exec` and nowhere else, and is run like a
`run:` list — directly, no shell — with the [seam document](#the-seam-document) on **stdin**
(`tool` and `arguments` name the pending call; there is no `result` yet) and the
`APOGEE_REACTION_*` facts in its environment; a webhook mapping under `gate:` is POSTed like a
`run:` webhook with the same document as the body, and its **reply body** (read up to 64 KiB) is
read exactly as a command's stdout is; an MCP mapping under `gate:` is called with the document
under its `payload-arg:`, and the tool's **text** is read the same way. It answers on **stdout**,
in the reply body or in the tool's text:

- the **first line** is the verdict — `allow`, `deny` or `ask`;
- every line after it is the **reason**: blank lines dropped, the rest folded onto one line and
  kept to its first 240 runes, shown to the human, never to the model.

`deny` ends the call: the model reads `tool call denied by reaction <id>` and nothing else — the
reason is yours, so a script cannot write into the conversation through a refusal it manufactured.
`ask` forces the approval prompt whatever the mode had decided, skipping the approval cache, and
the prompt reads `reaction <id> asks: <reason>` — or `reaction <id> asks` when no reason was
given; in [`apogee headless`](headless.md) and the
[daemon](daemon.md) nobody is there to answer, so the call is denied as
`tool call denied by approver`. `allow` means **nothing**: the mode ladder's own verdict stands,
because a script that could pre-approve a call would be a second autonomy ladder nobody audited —
a gate can say No, never Yes.

Every answer apogee cannot read **escalates to `ask`**, never to `allow`: an empty stdout, a first
line that is none of the three words, a non-zero exit, a timeout (default `timeout:` `5s`), a
command that could not be spawned, a webhook that answered with anything but a 2xx or could not be
reached — a status code is never a verdict — an MCP call that failed, and the prompt then reads
`reaction <id> asks: did not answer (<error>)`. A gate you armed and apogee cannot run is a
question, never a silent pass — which is why this is the one user Reaction `bypass:` leaves
armed: Bypass switches off what shapes the model's view, and a gate shapes nothing the model sees.

Gates run in the order the list gives them, on every call the mode has not already refused — a
call that will never run is not put to them. The first `deny` ends the call; otherwise the first
`ask` is what the prompt shows. On a `sub_agent` call an `ask` is deferred rather than raised: the
delegation proceeds, the child inherits the gate, and the question is asked on the child's own
calls, the ones that actually do something; a `deny` still refuses the delegation.

## Where an `advise:` or `gate:` command runs

The two waited-on keys share one posture, and it is not quite the `run:` one. The command is
yours, not the model's, so the mode ladder has no verdict about it: it runs in Plan exactly as it
runs in Auto. What the ladder leaves behind is the fence. With `confine-to-workspace` **off** the
command runs unfenced, as a `run:` does. With it **on**, the command runs inside the same
workspace box a subprocess tool would be confined to — and on a host that cannot build that box
it does not run at all: `workspace confinement is unavailable on this host` is reported to you, an
advise contributes nothing and a gate asks. As with `run:`, the program is resolved before it
starts and refused when it lives somewhere the model could have written it — save, for an adopted
project reaction, a script its pin holds (see [Running a command](#running-a-command)), and such a
command runs in the Project root. Unlike `run:`, the
command is spawned through the same door every execution tool uses, so it gets that door's
credential scrub — apogee's own key, every `api-key-env:` variable, every webhook
`headers-env:` variable and every `mcp-servers:` entry's `headers-env:` and `client-secret-env:`
variable are **absent** from its environment — along with the process-tree teardown
and that door's ceilings: combined stdout and stderr are capped at 256 KiB before the 8 KiB advice
cap is applied, and a `timeout:` above 3600 s is clamped to 3600 s. A `timeout: 0s` on an entry that
spells only `advise:` or `gate:` falls back to the class default, where the same value on a `run:`
entry is a startup refusal. A `run:` command of your own alone inherits the environment whole; an
adopted project entry's `run:` goes through this door too. None of this
reaches a **webhook** under either key: nothing is spawned, so there is no permit, no box and no
scrub — the request leaves over the network under the class deadline, exactly as a `run:` webhook
does, and `headers-env:` is read at send time from apogee's own environment. Nor does any of it
reach an **MCP mapping** under either key: it too spawns nothing, so it runs with
`confine-to-workspace` on as it does with it off, on a host that cannot build the box as well — the
call goes over the connection apogee already holds to the server, under the class deadline, and
never reaches the Approver (see [Calling an MCP tool](#calling-an-mcp-tool)).

Both keys are inherited by every sub-agent, unconditionally, because a Reaction the parent runs
with is part of the posture a delegation inherits: an `advise:` fence lands on the child's tool
results and a `gate:` is asked on the child's calls, each with the child's `depth` and `call_id`
stamped on the document. An `advise:` entry on `session-started` or `compaction-finished` is the
exception the notices make: a delegation starts no session and its folds are its own, so such an
entry briefs the top-level model alone.

## `workspace:` — scoping an entry

`workspace:` matches **exactly**, not by prefix: an entry scoped to `~/code/apogee` fires for runs
rooted there and not for a run rooted in a subdirectory of it. Both sides of the comparison — your
filter and the run's own workspace — go through the same resolution first: a leading `~` expanded,
the path made absolute, every symlink evaluated. That is why `/tmp/w` and `/private/tmp/w` are one
workspace on macOS rather than two, and why a filter written either way still matches.

The filter scopes every action key the entry spells: a scoped `gate:` is not asked and a scoped
`advise:` adds nothing outside its workspace, exactly as a scoped `run:` does not fire there.

Leave the key out and the entry is active in every workspace.

A Project config's reaction never leaves its repository: it exists only in sessions under that
Project root, so leaving `workspace:` out confines it there already. A `workspace:` it does spell
must name the Project root or a folder inside it, and a relative one resolves against that root;
one outside it refuses that entry with a notice, and the rest of the Project config still
applies. `workspace:` is a parameter, not part of the pin, so changing it does not ask again.

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

An `advise:` or `gate:` command that fails — a non-zero exit, a timeout, a refused permit — reaches
you at the same place under the same prefix, `reaction <id> (<moment>): <error>`, each time it
fails; the once-only fold above is the `run:` lane's. The error itself is worded by the door the
command ran through, so it differs from a `run:` failure's: `apogee: <program> exited 3: <last
256 bytes of stderr>` or `apogee: <program> timed out`, where a `run:` reports `exit 3: <first 240
runes>` or `timed out after 30s`. It is booked as a `reaction_fired` line with `"action": "failed"`
and the error as its `detail`, never as an `error` notice, and the turn goes on: the advise
contributes nothing, the gate escalates to `ask`. A `session-started` or `compaction-finished`
advise is reported when the message it was due on joins it, under that Moment —
`reaction prime (session-started): no answer within 10s: context deadline exceeded` for one that
had not answered by its deadline.

An MCP mapping fails the same way, at the same place and under the same prefix. A call that
reached the tool and failed — its `isError` text, a missed deadline — is reported each time under
`advise:` or `gate:`, as a command is; one that never reached it — `mcp server "<name>" is not
connected`, `mcp server "<name>" has no tool "<tool>"` or `mcp server "<name>" is not this
project's own server` — is reported once per entry, the first time it fires, because a server that is down stays
down a while and would otherwise report on every call. Under `run:` the once-only fold above holds
for both. In a front-end that connects no MCP server, the one setup notice
[Calling an MCP tool](#calling-an-mcp-tool) quotes is the whole report: the entry's firings add
none.

## Live reload

Inside the TUI the list is **live**: save `~/.apogee/config.yaml` (or use `⏎` on the `reactions:`
row in [`/settings`](commands.md#the-settings-screen--settings), which opens your editor) and the
running session swaps its Reactions over in one generation, draining the old workers in the
background. The Floor guards and `bypass:` cross that edit untouched, and a `/schedule` firing
raised afterwards fires the list the session is running now, not the one apogee launched with. A
list the session will not take — a malformed entry, an unresolvable `workspace:` — is refused
whole, and the session keeps firing the Reactions it already had.

A Project config's reactions ride the same swap. A save to the project's `.apogee/config.yaml`, or
an answer to one of its proposed reactions — in the adoption pane, in `/settings`, or with
`apogee project adopt` from another terminal — re-reads both files and swaps the lanes over at once,
your entries plus the adopted project ones. A project file that stops parsing takes its reactions
out of the lanes, leaving yours, until it parses again. A project entry the session refuses — one
whose `id` is yours, a `workspace:` outside the Project root — is dropped alone with a notice, never
the whole list.

`apogee headless` reads the list **once**, at start — yours and its workspace's adopted project
reactions together. `apogee daemon` reads yours once, at start, and layers each firing's own
workspace's adopted project reactions over them at that firing, so an adoption or a project edit
reaches the next firing without a restart; an edit to `config.yaml` itself reaches the daemon at its
next restart, since it watches its `schedules.yaml` and not `config.yaml`. Neither ever asks: a
project reaction still proposed is skipped, and the run says so in one line naming it — on stderr
for a headless run, once in the daemon's log —
``apogee: project config <path> proposes reaction `fmt`; this run skips it until it is adopted —
answer with `apogee project adopt` in that project``. A repository you clone therefore adds no
Reaction to your sessions until you have read and adopted it
(see [A project's own config](configuration.md#a-projects-own-config--apogeeconfigyaml)).

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
