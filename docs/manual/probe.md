# Diagnosing a host — `apogee probe`

`apogee probe` answers "what would Auto do on this machine?" without running an agent.
It reads `config.yaml` and the `APOGEE_*` environment exactly as a session would, and
reports the OS/arch, the confinement backend and what it can *actually* enforce here,
the Auto verdict, the effective `confine-to-workspace` after any host acknowledgement,
the workspace root and config home, and whether the configured endpoint answers
(`/v1/models`, plus llama.cpp's `/props` on the `openai` wire; an entry with
`wire: anthropic` is asked only `/v1/models`, under that wire's own headers, and the
`/props` line says so). It is free, offline and **read-only** — no
model is called, no starter config is seeded, nothing is written. `apogee probe host`
is the same report under a named child, for scripts. Both take `--endpoint`, `--workspace`
and `--config`, so the report can describe a server, a tree or a home other than the ones the
current directory and `~/.apogee` would give.

```console
$ apogee probe
apogee probe — host report
  (no agent runs, no model is called, nothing is written)

host
  os/arch:       windows/arm64
  ...
confinement (ADR 0012)
  backend:       token (fs-write: available · network: unavailable)
  auto:          eligible — the backend can fence terminal commands, so auto runs them confined
```

The `backend:` line gains a third field where the fence is real but incomplete —
`landlock (fs-write: available · network: unavailable · unfenced: truncate(2))` — naming each
access the backend knows it cannot cover on this host. On Linux that is truncation on a kernel
older than 6.2 (landlock ABI 1–2: Ubuntu 22.04, Debian 12, RHEL 9), where a confined command
cannot create or write outside the workspace but can still *empty* a file that is already there.
Auto stays eligible; the field exists so the report never claims a fence it does not have.

`apogee probe model` is the other half, and it is deliberately an **explicit act**
rather than something the bare noun triggers, because it costs live model calls *and*
writes. It runs a three-part capability battery — a native tool call, JSON/structured
output, and a multi-step tool chain — then prints what it observed, an ordinal
capability tier, and the `model-profiles:` entry the findings suggest — keyed by the
model it probed, and paste-ready as YAML
(your `config.yaml` is never edited). It also records a **behavioral fingerprint**: the
model keeps its advertised name — probing never renames it, so `model-profiles:` overrides
keyed on that name keep matching — and the signature it observed is stored so the next
`probe model` of the same server and model can compare against it; a signature that differs
reports that the model behind the label changed since the earlier record's date. That
comparison is all the record buys — nothing reads it at startup and it switches nothing on.
`--no-save` runs the whole battery and records nothing; when the battery completed, the
record's path is printed either way, so deleting that file undoes it. A battery that did not
complete derives no identity, records nothing and prints no path — its `record` block reads
`written: no — an incomplete battery derives no identity to record`. It takes `--endpoint`, `--model` and
`--config` as well, so you can point the battery at a server and a model this host has
never been configured for — with no `--model` the server is asked which one it is
serving. Both `apogee probe` and `apogee probe model` resolve the entry's API key before
they look at the server, and a source that refuses — an `api-key-cmd:` that fails, an
`api-key-env:` naming a variable that is not set — fails the command with that source's own
message rather than probing unauthenticated: *unreachable* would be the wrong finding.

`apogee probe terminal` is the third subject, and it is free like the host report. It
**measures** the terminal instead of trusting it: it writes real escape sequences to your
terminal and reads the answers back, then prints what it found — how it answers about
synchronized output and grapheme clustering (modes 2026 and 2027), how many cells it
really advances for an emoji or a combining sequence with that mode off and on, where its
tab stops are and whether a tab erases what it passes over, what happens when a glyph
lands in the last column (a pending wrap or an immediate one), and the capabilities it
actually has beside the ones apogee's renderer assumes from `TERM`. A section whose answer
disagrees with what was assumed is marked `MISMATCH` and
sets the exit status, so the report can be checked by a script and not only read. It needs
a real terminal on both stdin and stdout — a redirect or a pipe leaves nobody to answer —
and it calls no model and writes nothing.

```console
$ apogee probe terminal
apogee probe terminal — measured, not assumed
  (nothing is written; the screen is restored)

  size:          120 columns × 30 rows
  TERM:          (unset)
  ...
last-column wrap
    step                    cursor (CPR)  console API  deferred wrap would be
    wrote the final column  6,120         6,120        6,120 (pending)
    wrote one more          7,2           7,2          7,2
  OK — the terminal holds a pending wrap at the last column — the semantics the renderer emits against
```

`apogee probe config` is the fourth subject, free like the host report: it answers "what does
`config.yaml` say?" — the file alone, without the flags, `APOGEE_*` variables and host
acknowledgement that `apogee probe` layers on top. It reads the file the way a **live reload**
reads it (the read every `/settings` apply makes), never the way startup does, so it never
migrates or rewrites the file it describes. The report has three sections: `notices` lists
everything the reader announced — an unknown key it ignored, spelled exactly as the startup
notice spells it — or `(none)`; `migration` says whether the file is still written in a retired
shape (the old top-level `endpoint:`/`api-key:`/`host-alias:`/`model:` keys, or a `hooks:`
block) that a startup would fold for you, quoting the same refusal a live reload gives; and
`resolved` lists every key of the configuration at the value the file resolves it to — the
file's own value where it states one, the built-in default where it does not — spelled the way
the file spells it and in the order the starter file presents them. A structured block reads as
a summary (`servers: 2 servers`), so no `api-key` ever reaches the report. When the file awaits
migration the `resolved` section is replaced by `(start apogee once to migrate the file, then
re-run)`. It takes `--config` only. A file the reader refuses for any other reason — a
malformed one, a value a key rejects — fails the command with the reader's own sentence.

```console
$ apogee probe config
apogee probe — config report
  (nothing is written; the file is read the way a live reload reads it)

notices
  apogee: config /home/me/.apogee/config.yaml: unknown key "auto-compct" at line 12 is ignored

migration
  (none)

resolved
  servers:                 1 server
  server:                  local
  mode:                    ask-before
  ...
```

`apogee probe context` is the fifth subject, free like the host report by default: it answers
"what does apogee itself put in front of the model at Turn 1?" — the **Context cost** of
`CONTEXT.md`, [ADR 0079](../adr/0079-context-cost-is-a-first-class-engine-report.md). It
composes the configuration a session on this host would start with, under the mode the session
would start in (`--mode` picks another — the mode matters, since Plan filters the tool menu and
adds a bullet to the orientation), constructs an idle agent from it and reads what that agent
would send before your first message: one row per piece — the system prompt, the orientation
block, the workspace context files when the tree has any, the tool menu (or the tool-instruction
block on a profile without native tool calling) — with its bytes and its tokens, and a total. The
bytes are exact; the tokens are an **estimate** through the default chars-per-token ratio, which
is why every token figure is spelled with a `~` and the header names the ratio. Nothing is sent
and nothing is written. The total is the same number a headless run in the same mode reports on
`run_finished.context_cost` — the probe reads the engine's own report off an agent built the way
`apogee headless` builds one, so the two cannot disagree. It takes `--endpoint`, `--model`,
`--mode`, `--workspace` and `--config`.

```console
$ apogee probe context
Context cost — what apogee puts in front of the model at Turn 1 (mode ask-before; estimate, ~4.0 chars/token)
  prompt              2,457 B   ~615
  orientation           658 B   ~165
  tool menu          23,850 B  ~5963
  total              26,965 B  ~6742
```

`--live` is the paid side of the same report, and like `probe model` it is an explicit act
([ADR 0021](../adr/0021-probe-is-two-halves-the-host-report-is-free-the-model-battery-is-an-explicit-act.md)): it sends **one** fixed one-word request
(`Reply with the single word OK.`, the reply capped at 8 tokens) to the configured endpoint and
adds a `measured` column — the server's own `prompt_tokens` for that Turn 1, with the share it
answered from its prefix cache in brackets when it reported one — while the estimate rows are
re-rendered through the ratio that measurement calibrated, which the header then labels
`calibrated`. It says so on stderr before the call, and it still writes nothing. Advise and shape
Reactions add their directives only once a request is in flight, so an idle estimate cannot see
them: with none armed the table ends on the total row; with one or more armed the offline report
adds a line saying so, and `--live` sends **twice** — as configured, then under Bypass — and
prints both counts as `as configured` and `bypass` columns, with the line `Reactions add N tokens
at Turn 1` stating their difference. On a stock install nothing is armed, so `--live` costs one
request.

All five reports are printed with terminal control characters and bidi overrides removed: a
server you are probing *because* you distrust it — a terminal that answers the measurement
with escape sequences, or a config file whose text the report quotes — must not be able to
repaint the diagnostic that judges it.

**When a frame comes out wrong**, two hidden flags on `apogee` itself —
they sit on the root command, not on `apogee probe` — record the evidence a rendering bug is
argued from — `--tui-trace <file>` writes the exact bytes the renderer emitted, one quoted
string per write, so a corrupted frame can be replayed rather than only described, and
`--tui-diag <file>` writes what the terminal told apogee about itself: the environment the
renderer read, the width method it started on, the window size, the colour profile,
every mode report the terminal sent, the kind of each mouse event it delivered
(`mouse-kind`: `press`, `motion`, `release`, `wheel`) and every re-assertion of mouse
tracking apogee sent after a tool child (`mouse-reassert`, a running count) — each written
once and again only when it changes, so the file stays short enough to paste into a bug
report. A terminal that stopped reporting the mouse shows as `mouse-kind: press` with no
`motion` or `release` line ever following it.
Both default off and cost nothing unless you name a file, both work on every OS, and
neither appears in `--help`: they are for a bug report, not for a session. `⌃l` is the
in-session counterpart — it forces a full repaint and is usually all a smeared frame needs.

