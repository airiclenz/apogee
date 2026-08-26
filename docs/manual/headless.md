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
error. `--endpoint`, `--model`, `--workspace` and `--config` resolve exactly as a
session's do — flag over `APOGEE_*` environment over `config.yaml` — so the run has the
shape a session on this host would have; which listed entry it starts on comes from
`APOGEE_SERVER` or the `server:` key, there being no `--server` flag on this command.
Nobody is there to answer a picker, so a run those sources leave with no server to start
on is refused before anything is composed — the config file and the fix named, exit `2`. It is saved to
`~/.apogee/sessions` and shows up in `/sessions` like any other; `--no-save` runs it and
records nothing.

`--mode` takes `plan` (the default, read-only) or `auto` — the two modes that never need
a human. `ask-before` and `allow-edits` are refused, and so is `auto` on a host whose
confinement backend cannot fence the filesystem: there the interactive fallback is
approval, and an unattended run has nobody to approve (see
[Auto mode's blast radius](configuration.md#auto-modes-blast-radius)). Whatever the mode, every gated
action is refused rather than parked — the refusals are the `denied:` count — `ask_user`
and `present_document` are not registered, and no MCP server is contacted.

Only the model's answer goes to **stdout**; resolution notices and the one-line summary
go to **stderr**, so a pipeline reads the text and nothing else. A run that delegated
adds one stderr line per sub-agent run just ahead of that summary —
`sub-agent: 12k/32k · <the name it was given, else the task>`, in the order the runs
finished — because
each child fills a context window of its own that the run's own figures say nothing
about; a run that delegated nothing prints none. Beside those, and on the same terms,
comes what the run **spent**: `usage: calls 3 · prompt 18k · completion 1k · total 19k`
for the run itself and one such line per delegated run (labelled the same way), counting
every model call the agent made — the compaction folds included, which no context reading
shows. They are the addends and never a sum: an agent that made no call prints no line.
A server that reports how much of a prompt it answered from its own prefix cache adds a
`· cached 12k` column to that agent's line — a subset of the prompt count, never a
replacement for it; a server that says nothing about caching leaves the column off rather
than printing a zero that would read as a cache miss.
A run whose final turn was **abandoned** says so on that same summary line — the stats
segment ends `· faulted` — so the one line a script greps reports it even where the exit
status is not read.
The exit status says which kind of
thing happened:

| Exit | Means |
|---|---|
| `0` | the run completed |
| `1` | the run started and failed — model or tool error, cancellation, a record that would not save |
| `2` | the run never started — usage, configuration, a refused mode |
| `3` | the run started and reached its boundary, but its final turn was abandoned (a model or upstream fault the loop could not recover) — stdout holds the run's last text, not an answer; the record is saved |

