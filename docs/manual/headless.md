# Running one prompt — `apogee headless`

`apogee headless` runs a single prompt to completion with nobody watching and prints
the answer to stdout. It is the same unattended run a `/schedule` firing performs, over
the same shared runner — the second front-end onto the engine rather than a second
agent.

```console
$ apogee headless "list the exported types in apogee.go, one per line"
Agent
Config
...
session: 20260805-141233-7f2a · turns: 3 · denied: 0
```

The prompt is the single **quoted** argument; with no argument the whole of stdin is
the prompt, so `cat task.md | apogee headless` works too. Empty from both is a usage
error. `@path` tokens in the prompt are **file references** — bare or quoted
(`@"a b.md"`) — read from the workspace and attached to the message as in a session. A
missing or unreadable ref is skipped and the run goes on with the rest: the engine reports it
as an error event, which under `--format text` nothing prints — the live narration carries no
error line — and under `--format json` arrives as an `error` line; the saved record keeps it
either way. `/id` tokens are
**skill references** on the same terms: a token naming a skill in this run's catalog
attaches that skill's instructions to the message exactly as typing it into a session
would, and any other slash word — a path, a typo — stays plain text.
`--endpoint`, `--model`, `--server`, `--bypass`, `--workspace` and `--config` resolve exactly as a
session's do — flag over `APOGEE_*` environment over `config.yaml` — so the run has the
shape a session on this host would have; which listed entry it starts on comes from
`--server`, `APOGEE_SERVER` or the `server:` key, in that order, with `--endpoint`
overriding all three.
Nobody is there to answer a picker, so a run those sources leave with no server to start
on is refused before anything is composed — the config file and the fix named, exit `2`.
A run whose server is **not there at all** — the dial refused, the name unresolvable, the
connection timed out — is refused the same way, before the prompt is sent: `cannot send —
server offline (<endpoint>)`, exit `2`, no record written and no tokens spent. A server that
answers anything at all still runs: a rate-limited or unreadable model list is not a dead
server, and an endpoint that serves completions without advertising a list never was one.
A run whose bound entry has no context window — the server did not advertise one and no
`context-window:` pins it — says so once on stderr, `context window unknown — automatic
compaction and the Budget are inactive; set context-window: in config.yaml` (for a model the
server does not list, the same fact rides the not-advertised notice as a clause), and runs on
with both inactive.
The run is saved to
`~/.apogee/sessions` and shows up in `/sessions` like any other; `--no-save` runs it and
records nothing — no session record, and no undo snapshot store either, since a store filed
under a record nothing keeps is one `apogee undo` could never reach. Either way the startup
sweep still applies whatever bound the `sessions:`
block names — `--no-save` drops this run's own record, not the retention policy, so a host
driven only headlessly still keeps its store within `max-age` / `max-count`.

`--mode` takes `plan` (the default — read-only, except for the run's own scratch directory)
or `auto` — the two modes that never need a human. `--mode ask-before` and `--mode allow-edits` are refused, and so is `auto` on a host whose
confinement backend cannot fence the filesystem: there the interactive fallback is
approval, and an unattended run has nobody to approve (see
[Auto mode's blast radius](configuration.md#auto-modes-blast-radius)). The two modes that
need a human are not treated alike when they come from `APOGEE_MODE` or the `mode:` key
rather than the flag: `ask-before` from either — the interactive ladder's own default, which
a host that never spelled a mode out would otherwise carry — is silently replaced by `plan`,
while `allow-edits` from either is refused exactly as the flag is. Whatever the mode, every gated
action is refused rather than parked — the refusals are the `denied:` count — `ask_user`
and `present_document` are not registered, and no MCP server is contacted.

Under the default `--format text`, only the model's answer goes to **stdout**, once, when
the run is done; everything else — resolution notices, the live narration below, and the
one-line summary — goes to **stderr**, so a pipeline reads the text and nothing else.
`--format json` replaces that stdout wholesale with the machine-readable
Event lines; the section at the foot of this page is their contract, and everything said
about stderr here holds under both formats bar the lines that section names as
suppressed.

While the run works, stderr **narrates it live**, one line per event as it happens, so a
ten-minute run never looks hung: `→ <tool> <summary>` when the model calls a tool — the
summary is the call's first string argument, the path or the command, clipped to one
line — then `← <tool> ok` or `← <tool> error: <first line of the failure>` when its result
comes back, and `sub-agent <name>: started` / `finished` / `cancelled` as each delegation
crosses those boundaries, named as the delegating call named it (or as apogee named it
while it ran, the moment that name lands). A delegation's own calls are not narrated — its
`sub-agent` lines stand in for them. A pruning pass mid-run prints
`pruned N tool results (~T tokens)` in the same stream, at every depth. None of this is
the run's outcome: the block described next is still composed after the run, from what it
reported, in the wording and order it has always had.

Where the workspace carries context files, that
stderr stream opens with what they contributed: one `context:` line naming every file that
loaded and its size, one `context: <name> unreadable — <reason>` line per file that is present
but could not be read, and — when the standing system content has outgrown its share of the
Budget — the advisory line that says so. Those lines print even for a run that never started,
because the files were read before the refusal; a workspace carrying none of the configured
names prints none of them. A run that delegated
adds one stderr line per sub-agent run just ahead of that summary —
`sub-agent: 12k/32k · <the name it was given, else the task>`, in the order the runs
finished — because
each child fills a context window of its own that the run's own figures say nothing
about; a run that delegated nothing prints none. That name is the one the delegating
call supplied, else the short one apogee generated for an unnamed run while it worked
(the `auto-title:` switch, on by default), else the delegated task's first line. A
delegation that ran on a model other than the run's own adds a trailing `· <model>` column
naming it; one on the same model adds nothing.
Beside those, and on the same terms,
comes what the run **spent**: `usage: calls 3 · prompt 18k · completion 1k · total 19k`
for the run itself and one such line per delegated run (labelled the same way), counting
every model call the agent made — the compaction folds included, which no context reading
shows. They are the addends and never a sum: an agent that made no call prints no line.
A server that reports how much of a prompt it answered from its own prefix cache adds a
`· cached 12k` column to that agent's line — a subset of the prompt count, never a
replacement for it; a server that says nothing about caching leaves the column off rather
than printing a zero that would read as a cache miss.
Below the usage lines comes what apogee itself put in front of the model at Turn 1, before
the prompt: `context cost: ~812 tokens (prompt 640 · orientation 90 · tool menu 82)` — the
estimate over the standing system content and the tool menu, one column per piece present —
and, once the server has counted the first call, that count leads and the estimate follows:
`context cost: 1234 tokens measured at turn 1 (estimate ~812)`. It is the same report
`run_finished.context_cost` carries under `--format json`; a run that never built a session
prints no such line.
A run that **changed files** says which — on **stderr**, like every other narration on this
Driver, just below the answer: a
`changed — 2 file(s) this run:` header and one indented path per file, in the order the run
first touched each. The list names everything the run's writes touched — files it deleted and
the source side of a move as much as files it created — which is why the header says *changed*
rather than *wrote*. A run that changed nothing prints no such line. The block ends with the
offer — `  undo with: apogee undo <session-id>` — naming the exact command that puts the run's
last exchange back. That line is printed only when there is something behind it: the run changed
files, its record was saved, and the session's snapshots were taken. A run whose undo was the
narrower in-memory one — `undo-snapshots: false`, or no `git` on the host — prints the changed
list and no offer, because nothing outlived the process to revert from.

**`apogee undo <session-id>` is that revert, from any directory and long after the run.** It is
the same two steps as `/undo` inside a session: `apogee undo <session-id>` previews, listing
every recorded path with what the revert would do to it — *restore*, *delete*, or *skip* with
the reason — and closes with the exact line that applies it,
`apogee undo <session-id> confirm <generation>`, where the generation is the journal's stamp at
the moment of the preview. Run that line and it applies exactly that step and reports what it
did. The stamp is required — a bare `confirm` answers `preview first: apogee undo <id>, then
run the line it prints` — and it is what ties the confirm to the listing you read: a journal
that has moved since (another exchange recorded, an earlier confirm applied) refuses the stale
stamp, touches nothing and prints the fresh preview with the line that now applies. Run it
again to walk further back; each `confirm` takes one more exchange. A file that no longer holds
what the agent left is skipped rather than overwritten, so your own edits since the run are
safe. The verb holds the session for its run, so a session that is open in a live apogee is
refused with `session <id> is open in another apogee — fork it to work alongside` rather than
rewritten under it — and so is a headless or daemon run still in flight, which holds its session
the same way until it finishes. There
is no `--workspace` flag and it is refused as unknown: the tree the revert belongs to is
recorded in the session's own snapshot index, and a workspace given on the command line could
only disagree with it. A session recorded without snapshots has nothing to revert here and says
so. The full account of what undo covers is on the
[commands page](commands.md#undoing-the-agents-file-writes--undo-and-redo).

A run whose final turn was **abandoned** says so on that same summary line — the stats
segment ends `· faulted` — so the one line a script greps reports it even where the exit
status is not read.
The exit status says which kind of
thing happened:

| Exit | Means |
|---|---|
| `0` | the run completed |
| `1` | the run started and failed — model or tool error, cancellation, a record that would not save |
| `2` | the run never started — usage, configuration, a refused mode, a server that did not answer |
| `3` | the run started and reached its boundary, but its final turn was abandoned (a model or upstream fault the loop could not recover) — stdout holds the run's last text, not an answer; the record is saved |

## Machine-readable output — `--format json`

`--format` decides what **stdout** carries, and nothing else about the run: `text` is the default
and is exactly the output described above, byte for byte. `--format json` replaces that stdout with
the **Event lines** — the engine's own event stream rendered one JSON object per line, JSONL, no
answer and no prose among them. stderr keeps every notice, warning and summary it prints under
`text`, with one exception: the prune notice and the live narration go quiet, because the `prune`,
`tool_call`, `tool_result` and `sub_agent_phase` lines on stdout already carry the same facts. The
contract is
[ADR 0075](../adr/0075-the-headless-event-stream-is-a-versioned-driver-protocol.md).

```json
{"event":"tool_call","v":2,"seq":3,"time":"2026-09-07T14:12:33.884201Z","session":"20260907-141233-7f2a","turn":0,"depth":0,"call_id":null,"run_id":null,"data":{"call":{"id":"call_1","tool":"read_file","arguments":{"path":"a.txt"}},"resolved_path":"","spawn_run_id":""}}
```

### The envelope

Every line is that same envelope — ten members, the variant's own content nested under `data`
rather than flattened beside it:

| Member | What it carries |
|---|---|
| `event` | the line kind: one of the twenty-one names below |
| `v` | the contract version — `2` today, on **every** line |
| `seq` | 1-based, counting every line the run wrote, the two frames included |
| `time` | RFC3339Nano, stamped as the line is written |
| `session` | the run's session id, `null` on a line written before the run had one |
| `turn` | the Turn the event belongs to, `null` on the frames |
| `depth` | `0` for the run itself, `1` and deeper for a delegated run, `null` on the frames |
| `call_id` | the id of the `sub_agent` call that spawned the emitting agent, `null` when there is none. It is the model's or the server's id and may repeat — two delegations of one fan-out can share it — so it names a call, never a run |
| `run_id` | the delegated run the event came from: an id apogee mints once per delegation, unique however the call ids repeat, `null` at depth `0`, on the frames and on a delegated event recorded without one. This is the key that tells two sub-agents' lines apart |
| `data` | the kind's own members, snake_case, an object on every line |

Every member is **always present**, and is `null` where the line has no value for it, so a consumer
never has to test for a missing key. `depth` is what separates the run's own events from a
sub-agent's: the lines carry every depth, not just the top. `run_id` is what separates one
sub-agent from another, and the parent's `tool_call` and `tool_result` for a delegation carry the
same id as `data.spawn_run_id` (`""` on any other call), which is how a delegated run is paired with
the call that spawned it.

### The twenty-one line kinds

Nineteen of them are engine events, and the two frames are not. The names are snake_case on
purpose — a [Reaction notice](reactions.md)'s kebab-case name for a neighbouring moment is a *different*
moment, and the case difference is the signal.

| `event` | What it marks |
|---|---|
| `token` | one streamed chunk of the assistant's text |
| `reasoning` | one newly revealed chunk of the model's reasoning channel |
| `stream_reset` | the text streamed so far was discarded — an accumulator starts again here |
| `message` | a completed assistant message |
| `tool_call` | a tool call the model requested, with its arguments and any resolved path |
| `tool_result` | that call's outcome after execution, with `data.tool` — the tool it ran under, as the pre-tool-exec Reactions left the call — and `data.write_target`, the resolved path the call wrote (the same resolution `tool_call`'s `resolved_path` comes from; `""` for a call that is not a write, and a value that is a *changed* file only together with `is_error: false`) |
| `sub_agent_phase` | one delegation crossing a lifecycle boundary; `data.cancelled` marks a `finished` that closes a rolled-back bracket rather than reporting a result |
| `sub_agent_named` | the name a delegated run was given |
| `child_interjection` | input steered into a running delegation, whether it landed, and — as `data.reason` on one that did not — why: `completed`, `capped`, `faulted`, `stopped` or `cancelled` (the child ended that way before the boundary the message waited for — `stopped` is a delegation the human stopped singly while the parent's turn went on) or `refused` (the child, still running, refused it there); `""` on a landed message. The set is open: read an unknown value as `completed` |
| `approval` | an approval request: its phase, the request, the decision |
| `turn` | a Turn boundary, at every depth: its status, whether it faulted, whether it hit the step cap |
| `reaction_fired` | a Reaction acted: an engine builtin (a Floor guard or the context-fill notice) or armed Reaction, at which Moment, and what it did |
| `error` | something failed, named by its source |
| `prune` | the context was pruned: how many results, how many tokens |
| `ref_clipped` | an `@file` or attached skill entered the conversation clipped to its bound: which reference, the bound in tokens, whether the absolute per-reference cap or its share of the window bound it |
| `usage` | one model call's token accounting and the run's cumulative totals; `data.model` is the id the call asked for and `data.served_model` the id the server answered with (empty when it named none) |
| `audit` | a tool call's allow/deny decision and its reason |
| `seam_closed` | one in-loop seam finished passing: `data.seam` is its closing notice's name (`post-response-finished`, …) and `data.fired` the reactions that acted there, in order — **opt-in**, absent from the stream unless `--seams` asks for it |
| `upstream_attempt` | one HTTP attempt a model call made against its server, at every depth and for compaction's summary call too — a retried, failed or cancelled attempt is a line of its own: `data.server` (the server entry's name), `data.endpoint` (scheme, host and path only — no credentials, no query), `data.model` (the id the server answered with, else the one asked for), `data.request_id` (shared by every attempt of one call) and `data.index` (0-based within it), the clocks `ttfb_ms` (send → first body byte), `ttft_ms` (send → first model delta), `last_ms` (send → last model delta) and `duration_ms` (send → the attempt's end) in whole milliseconds, `0` where not reached, `data.output_tokens` (`0` when the server reported none) and `data.outcome` — `ok`, a fault class (`http_<code>`, `overflow`, `in_band`, `transport`, `idle`, `stream_fault`) or `cancelled` |
| `run_started` | the opening frame — not an event |
| `run_finished` | the closing frame — not an event |

The Inspector's raw provider protocol is the one thing never written here: putting a wire format on
a documented stdout contract would make it a public surface.

### `--seams` — the seam closures, on request

`--format json --seams` adds the `seam_closed` lines: one per in-loop seam that finished passing,
named by the seam's closing notice — `pre-request-finished`, `post-response-finished`,
`pre-tool-exec-finished`, `post-tool-result-finished` and `history-rewrite-finished` — with the
reactions that acted there under `data.fired`. They are off by default for two reasons. Volume:
the request and response seams close once per streamed Turn and the two tool seams once per tool
call, at every depth, so a run that used tools has more seam lines than tool lines. And the
default stream is what [ADR 0076](../adr/0076-one-reaction-core-with-an-origin-by-class-policy-matrix.md)
A5 promised: the seam closures are sink-only there, so a consumer that never asked sees exactly
the stream it saw before the kind existed. `--seams` reaches only the Event lines; given without
`--format json` it is a usage error — `apogee headless: --seams needs --format json`, exit `2`,
no run started.

### The two frames

`run_started` says what the run was asked to **be**, once every refusal is behind it and before it
has done anything: `session`, `workspace`, `model`, `server`, `mode`, `bypass`, `confined`,
`version` (the full build string `apogee --version` prints, so a stream read back later names the
binary that wrote it).

`run_finished` is the whole outcome: `exit_code`, `turns`, `denied`, `faulted`, `fault`, `error`
(the run's error text, `null` when there was none), `title`, `final_text`, `wrote`,
`context_files`, `context_cost`, `undo_note`, `saved`, `usage`, `sub_agents`,
`turn1_prompt_tokens` and `turn1_cached_prompt_tokens`.

`context_cost` is what apogee itself put in front of the model at Turn 1, before the prompt — the
standing system content and the tool menu — as `rows` (one `{name, bytes, tokens}` per piece, in
wire order), the total `bytes`, the token estimate `tokens` over that total, and `calibrated`,
which is `false` on a headless run: the estimate is taken once the session is built and before
anything is sent, so it is a `~` number. `turn1_prompt_tokens` and `turn1_cached_prompt_tokens`
are the measured twin — the server's own count for the run's first call, prompt included — and
stay `0` when the run never made one or the server reported no usage. A run refused before its
session existed carries the zero report, `rows` `null`.

The rule is **exactly one `run_finished` on every exit path** — so stdout is never empty for a
consumer to interpret. A run refused before it started writes that frame **alone**, with the exit
code the prose path would have given it; a run cancelled by Ctrl-C writes it too, with exit `1`.

Two members are worth reading together with `apogee undo` above: `session` is present exactly
when the run minted an id — which `--no-save` does as well — and `saved` says whether a record was
actually written. Feed `apogee undo` the id of a run whose `saved` is `false` and there is nothing
behind it: such a run opened no snapshot store, and its `undo_note` says so as `no record kept`.
`undo_note` is otherwise the reason the run's undo was the narrower in-memory one
(`undo-snapshots is off`, `git not found`, …) and empty when the session's snapshots were taken.

`final_text` duplicates the last top-level `message` on purpose. A consumer reading `message` lines
needs no accumulator; but the simplest consumer of all reads `token`s, and that one would otherwise
have to implement both an accumulator and the `stream_reset` rule to reach the answer.

### `v` is `2`, and growth is additive

The version rides every line, because JSONL is tailed, split, grepped and merged across runs — a
version living only in a frame is invisible in all four. Within a version, new line kinds and new
`data` members may appear in any release, and today's enum values (a Turn's status, an approval's
phase, a delegation's phase) are open sets. **A consumer must ignore names, members and values it
does not recognise.** A removal, a rename or a changed meaning bumps `v` and is a CHANGELOG entry.
v 2 (this release) folded `mechanism_fired` and `floor_guard` into `reaction_fired`.

### What is on that stdout

The lines are full fidelity and are **not** scrubbed: `tool_call`'s arguments are the model's own
argument JSON and `tool_result`'s content is the whole result the model was handed. Bounding either
would make the live stream strictly worse than the transcript already on disk. It is the same trust
posture the rest of this Driver takes — the stream goes to your own pipe — but the pipe is the
thing to watch: **redirect `--format json` into a CI log and that log publishes every file the run
read.**

### Lossless, blocking, and readers that walk away

A line is written as its event happens and is never dropped: a lost `token` costs a character, a
lost `tool_result` silently corrupts what the consumer believes the run did. The cost of that
choice is that a reader which stops draining **stalls the run**, so two behaviours exist to keep
that survivable:

- A write to stdout that fails — the classic `apogee headless --format json | head -1` — is
  reported once on stderr as `apogee headless: event lines stopped — <reason>`, after which nothing
  more is emitted and **the run continues to its own end** with its own exit code. A run that has
  already edited files is not half-killed because a reader closed the pipe; on Unix `SIGPIPE` is
  ignored for the duration of a `--format json` run so that the write returns an error instead of
  ending the process.
- The first Ctrl-C is the polite stop: the run unwinds, its record is saved and its closing frame
  is written. A Ctrl-C that lands **before the run starts** — while the server is still being
  asked whether it is there — is the offline refusal instead: `cannot send — server offline
  (<endpoint>): …`, its suffix the discovery error verbatim and so ending in `context canceled`,
  exit `2`, the closing frame written and nothing sent. A
  **second** interrupt is taken literally — one stderr line, `apogee headless: second
  interrupt — exiting without waiting for the run`, and the process ends with exit `1` and **no
  `run_finished`**. It is the escape hatch for a stream nobody is draining, and the one path on
  which the closing frame is not written.

### Where `--format` reaches

Here and nowhere else. [`apogee daemon`](daemon.md) keeps its prose log — its stdout multiplexes
many Firings, which needs a per-stream identity this envelope has no field for — and
[`apogee probe`](probe.md) stays a prose report; neither takes `--format`. A `--format` value that
is neither `text` nor `json` is refused in prose, exit `2`: no stream exists yet to carry the news,
and a JSONL stream whose single line said *that is not a format* would be the worse answer.
