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
(`@"a b.md"`) — read from the workspace and attached to the message as in a session; a
missing ref is skipped without notice — a Firing has no event sink. `/id` tokens are
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
The run is saved to
`~/.apogee/sessions` and shows up in `/sessions` like any other; `--no-save` runs it and
records nothing. Either way the startup sweep still applies whatever bound the `sessions:`
block names — `--no-save` drops this run's own record, not the retention policy, so a host
driven only headlessly still keeps its store within `max-age` / `max-count`.

`--mode` takes `plan` (the default, read-only) or `auto` — the two modes that never need
a human. `ask-before` and `allow-edits` are refused, and so is `auto` on a host whose
confinement backend cannot fence the filesystem: there the interactive fallback is
approval, and an unattended run has nobody to approve (see
[Auto mode's blast radius](configuration.md#auto-modes-blast-radius)). Whatever the mode, every gated
action is refused rather than parked — the refusals are the `denied:` count — `ask_user`
and `present_document` are not registered, and no MCP server is contacted.

Under the default `--format text`, only the model's answer goes to **stdout**; resolution
notices and the one-line summary go to **stderr**, so a pipeline reads the text and
nothing else. `--format json` replaces that stdout wholesale with the machine-readable
Event lines; the section at the foot of this page is their contract, and everything said
about stderr here holds under both formats bar the one line that section names as
suppressed. Where the workspace carries context files, that
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
(the `auto-title:` switch, on by default), else the delegated task's first line.
Beside those, and on the same terms,
comes what the run **spent**: `usage: calls 3 · prompt 18k · completion 1k · total 19k`
for the run itself and one such line per delegated run (labelled the same way), counting
every model call the agent made — the compaction folds included, which no context reading
shows. They are the addends and never a sum: an agent that made no call prints no line.
A server that reports how much of a prompt it answered from its own prefix cache adds a
`· cached 12k` column to that agent's line — a subset of the prompt count, never a
replacement for it; a server that says nothing about caching leaves the column off rather
than printing a zero that would read as a cache miss.
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
the reason — and `apogee undo <session-id> confirm` applies exactly that step and reports what
it did. Run it again to walk further back; each `confirm` takes one more exchange. A file that
no longer holds what the agent left is skipped rather than overwritten, so your own edits since
the run are safe. There is no `--workspace` flag and it is refused as unknown: the tree the
revert belongs to is recorded in the session's own snapshot index, and a workspace given on the
command line could only disagree with it. A session recorded without snapshots has nothing to
revert here and says so. The full account of what undo covers is on the
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
`text`, with one exception: the prune notice goes quiet, because the `prune` line on stdout already
carries the same fact. The contract is
[ADR 0075](../adr/0075-the-headless-event-stream-is-a-versioned-driver-protocol.md).

```json
{"event":"tool_call","v":2,"seq":3,"time":"2026-09-07T14:12:33.884201Z","session":"20260907-141233-7f2a","turn":0,"depth":0,"call_id":null,"data":{"call":{"id":"call_1","tool":"read_file","arguments":{"path":"a.txt"}},"resolved_path":""}}
```

### The envelope

Every line is that same envelope — nine members, the variant's own content nested under `data`
rather than flattened beside it:

| Member | What it carries |
|---|---|
| `event` | the line kind: one of the eighteen names below |
| `v` | the contract version — `2` today, on **every** line |
| `seq` | 1-based, counting every line the run wrote, the two frames included |
| `time` | RFC3339Nano, stamped as the line is written |
| `session` | the run's session id, `null` on a line written before the run had one |
| `turn` | the Turn the event belongs to, `null` on the frames |
| `depth` | `0` for the run itself, `1` and deeper for a delegated run, `null` on the frames |
| `call_id` | the delegating call the event came from, `null` when there is none |
| `data` | the kind's own members, snake_case, an object on every line |

Every member is **always present**, and is `null` where the line has no value for it, so a consumer
never has to test for a missing key. `depth` is what separates the run's own events from a
sub-agent's: the lines carry every depth, not just the top.

### The eighteen line kinds

Sixteen of them are engine events, and the two frames are not. The names are snake_case on
purpose — a [Hook event](hooks.md)'s kebab-case name for a neighbouring moment is a *different*
moment, and the case difference is the signal.

| `event` | What it marks |
|---|---|
| `token` | one streamed chunk of the assistant's text |
| `reasoning` | one newly revealed chunk of the model's reasoning channel |
| `stream_reset` | the text streamed so far was discarded — an accumulator starts again here |
| `message` | a completed assistant message |
| `tool_call` | a tool call the model requested, with its arguments and any resolved path |
| `tool_result` | that call's outcome after execution |
| `sub_agent_phase` | one delegation crossing a lifecycle boundary; `data.cancelled` marks a `finished` that closes a rolled-back bracket rather than reporting a result |
| `sub_agent_named` | the name a delegated run was given |
| `child_interjection` | input steered into a running delegation, and whether it landed |
| `approval` | an approval request: its phase, the request, the decision |
| `turn` | a Turn boundary, at every depth: its status, whether it faulted, whether it hit the step cap |
| `reaction_fired` | a Reaction acted: builtin Floor guard or armed Reaction, at which Moment, and what it did |
| `error` | something failed, named by its source |
| `prune` | the context was pruned: how many results, how many tokens |
| `usage` | one model call's token accounting and the run's cumulative totals |
| `audit` | a tool call's allow/deny decision and its reason |
| `run_started` | the opening frame — not an event |
| `run_finished` | the closing frame — not an event |

The Inspector's raw provider protocol is the one thing never written here: putting a wire format on
a documented stdout contract would make it a public surface.

### The two frames

`run_started` says what the run was asked to **be**, once every refusal is behind it and before it
has done anything: `session`, `workspace`, `model`, `server`, `mode`, `bypass`, `confined`,
`version` (the full build string `apogee --version` prints, so a stream read back later names the
binary that wrote it).

`run_finished` is the whole outcome: `exit_code`, `turns`, `denied`, `faulted`, `fault`, `error`
(the run's error text, `null` when there was none), `title`, `final_text`, `wrote`,
`context_files`, `undo_note`, `saved`, `usage` and `sub_agents`.

The rule is **exactly one `run_finished` on every exit path** — so stdout is never empty for a
consumer to interpret. A run refused before it started writes that frame **alone**, with the exit
code the prose path would have given it; a run cancelled by Ctrl-C writes it too, with exit `1`.

Two members are worth reading together with `apogee undo` above: `session` is present exactly
when the run minted an id — which `--no-save` does as well — and `saved` says whether a record was
actually written. Feed `apogee undo` the id of a run whose `saved` is `false` and there is nothing
behind it.

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
  is written. A **second** interrupt is taken literally — one stderr line, `apogee headless: second
  interrupt — exiting without waiting for the run`, and the process ends with exit `1` and **no
  `run_finished`**. It is the escape hatch for a stream nobody is draining, and the one path on
  which the closing frame is not written.

### Where `--format` reaches

Here and nowhere else. [`apogee daemon`](daemon.md) keeps its prose log — its stdout multiplexes
many Firings, which needs a per-stream identity this envelope has no field for — and
[`apogee probe`](probe.md) stays a prose report; neither takes `--format`. A `--format` value that
is neither `text` nor `json` is refused in prose, exit `2`: no stream exists yet to carry the news,
and a JSONL stream whose single line said *that is not a format* would be the worse answer.
