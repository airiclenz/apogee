<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="graphics/apogee-logo-light.svg">
    <source media="(prefers-color-scheme: light)" srcset="graphics/apogee-logo-dark.svg">
    <img src="graphics/apogee-logo-light.svg" alt="Apogee" width="350">
  </picture>
</p>

A terminal-based coding agent built for **small, locally-run LLMs** (~4B–35B).

Apogee is a single, cross-platform tool that drops into any IDE's integrated
terminal — or any standalone terminal — on Windows, macOS, and Linux. It runs
against any OpenAI-compatible LLM server (llama.cpp, Ollama, LM Studio, vLLM), so
your code stays on your machine, a local model needs no API key (and a keyed
server is one `api-key:` away), and you get a full agentic tool-use loop with
sensible guardrails.

## What this repo is

Apogee brings together two things most coding agents keep separate:

- **A complete agentic coding assistant** — the *agent loop*, with provider
  abstraction, a ~21-tool suite (file ops, grep/glob, git, terminal, web,
  sub-agents, showing you a finished document), an MCP client, sessions, four
  autonomy modes (Plan / Ask-Before / Allow-Edits / Auto), and security
  guardrails.
- **Self-regulating mechanisms for small models** — features that make small,
  locally-run models measurably better at sustained agentic coding: context
  compression, tool-call validation + auto-retry, behavioural nudges, and a
  cross-session learning *Library*. Each is gated so it only fires when the model
  needs it.

These mechanisms run *inside* the agent loop, where they have the most leverage —
not in a separate proxy. And nothing is carried forward on faith: every mechanism
is measured and A/B-tested against real local models with an eval/simulation
harness before it earns a place in the loop.

## Why Go

Portability is the primary goal. Go cross-compiles to a single static binary with
no runtime — the gold standard for "drop into any terminal on any OS." It also lets
us use **one language for both the agent and the bench that evaluates it**. The TUI
is built on the Charm stack (Bubble Tea + Lipgloss + Bubbles) with Cobra for the CLI.

## Status

**`v1.7.0` shipped (2026-07-21); cross-platform hardening landed on `main`
(2026-07-22).** The embeddable agent core is stable — the public Go API follows
semver from `v1.0.0` — with the full tool suite, MCP client, sub-agents, and
OS-confined Auto mode on **all three** platforms: Linux landlock, macOS seatbelt,
and — new — Windows, where the fence is a restricted low-integrity token (Windows 10
1809 / build 17763 / Server 2019 and newer). Auto still falls back to asking before
each shell/subprocess call wherever the facility is genuinely missing rather than
unimplemented: an older Windows build, or most containers, where landlock reports
`ENOSYS` whatever the kernel version. Apogee says so at startup, `apogee probe`
answers "what would Auto do on this box?" without running an agent, and `/confine`
is the way out (see [Auto mode's blast radius](#auto-modes-blast-radius) and
[Diagnosing a host](#diagnosing-a-host--apogee-probe)). Current work is per-model
bench validation of the mechanism catalogue: the full catalogue is ported, and
the first Validated set (gemma-4-E4B) ships with the binary.
See [`docs/plans/`](docs/plans/) and the [`CHANGELOG`](CHANGELOG.md) for what's
next.

## Key capabilities

- **Model-agnostic, local-first** — any OpenAI-compatible endpoint; zero data leaves
  your machine with a local model.
- **Agentic tool use** — multi-step loop with file edits, shell, search, git, web,
  and sub-agents.
- **Type — and select — while it works** — the prompt box stays live during a run: write
  your next message while the model streams, and press `⏎` to *queue* it. A queued message
  is delivered **into the running task** at the next tool boundary, so a remark like "also
  check the tests" lands while there are still turns left to act on it; anything still
  queued when the task finishes goes out as the next message. `esc` stops everything and
  holds the queue. Transcript text stays selectable in every state, mid-stream included.
- **Deliverables you actually see** — `present_document` ends a report-producing task
  by showing the file: opened on your desktop when apogee runs locally, served over a
  one-off link when it runs on a remote box, and always printed as a clickable path
  in the transcript. See [Showing a finished document](#showing-a-finished-document).
- **Four autonomy modes** — Plan (read-only), Ask-Before (writes need approval),
  Allow-Edits (workspace-scoped writes auto-approved), Auto (autonomous, confined
  at the OS level via Linux landlock / macOS seatbelt / a Windows low-integrity
  token; where the OS cannot fence a command, Auto asks before it rather than
  running it unbounded).
- **Diagnosable without running an agent** — `apogee probe` reports what this host can
  do (backend, capability matrix, Auto verdict, roots, endpoint reachability) for free
  and offline; `apogee probe model` runs a capability battery against the model. See
  [Diagnosing a host](#diagnosing-a-host--apogee-probe).
- **MCP support** — connect external tool servers over stdio / SSE / streamable-http.
- **Model profiles** — adapt to models that don't speak native tool-calls: the tool
  menu and format instructions are injected as text on the request side, markdown-fenced
  or custom-regex tool calls are parsed back out of the reply, and inline thinking /
  harmony channels are stripped — all driven by a per-model profile (native models
  stay byte-identical on the wire).
- **Small-model mechanisms** — context compaction is built in; tool-call
  validation/auto-retry, syntax + autofix, behavioural nudges, and the cross-session
  Library are all catalogued — each default-off, gated so it only fires when the
  model needs it, and enabled per model via Validated sets backed by bench evidence.
- **Validated, not assumed** — every mechanism is A/B-tested against real local models
  via an eval/simulation bench (which imports Apogee as a Go library and drives
  the real loop in-process) before it earns a place in the loop.

## In-chat commands

Typing `/` in the prompt opens the command menu; `@` completes a workspace file
path, and an `@path` in a message hands that file to the model. A path containing
spaces is written quoted — `@"docs/my plan.md"` (single quotes are accepted too) —
and the autocomplete keeps completing across the spaces and inserts the quotes for
you.

| Command | Does |
|---|---|
| `/clear` (or `/new`) | Reset the model's memory of this session |
| `/compact` | Summarise the conversation to reclaim context |
| `/continue` | Ask the model to keep going |
| `/confine` | Report or change Auto's blast radius — see [below](#auto-modes-blast-radius) |
| `/skill` | Attach a skill to your next message |

## Configuration

Settings resolve by precedence, highest first: a command-line flag overrides an
`APOGEE_*` environment variable, which overrides `~/.apogee/config.yaml`, which
overrides the built-in default. A documented starter `config.yaml` is written to
`~/.apogee` on first run (your edits are never overwritten): every setting is
there as a commented example, with one exception — `system-prompt-text:`, the
default system prompt, ships active. Some settings are **file-only** (no flag or
env) — the system prompt, the model profile, MCP servers, web-search endpoint,
and the small-model mechanisms — and one, the upstream `api-key:`, has a file key
and an environment variable but deliberately **no flag** (see
[The upstream API key](#the-upstream-api-key)).

Catalogued mechanisms are opt-in by canonical ID. Every mechanism ships **off**
until its A/B bench run proves it a win, so enabling one is a deliberate config
choice:

```yaml
# ~/.apogee/config.yaml
mechanisms:
  validate: true   # tool-call validation + auto-retry
  syntax: true     # write-content syntax check
  autofix: true    # formatter pass on tool-call payloads
```

An unknown ID is a startup error that lists the IDs this build knows; `--bypass`
still wins (an enabled non-off-ramp mechanism does not fire under bypass). The same
catalogued mechanisms are enabled by ID from the Go API through
`Config.EnableMechanisms` (with `apogee.CataloguedMechanisms()` to enumerate them), so
a library embedder arms the identical stack without the config file. The
catalogue fills in as the port waves land — see
[`docs/design/mechanism-catalogue.md`](docs/design/mechanism-catalogue.md).

Automatic context **Compaction** keeps a long session from overflowing the model's
window: when the conversation history outgrows its budgeted share, apogee folds the
older turns into a summary (the same reducer as the `/compact` command) before the
next request. The same fold is also apogee's **overflow recovery**: when a request
does not fit the window after all — or the estimate already says it cannot — the
history is folded mid-task and the turn is re-sent once, so a long task survives
instead of dying on "context window exceeded". It is structural and load-bearing —
it stays on even under `--bypass` — so it is on by default; set `auto-compact: false`
(a file-only key) to manage the window yourself with `/compact` instead, which opts
out of the recovery too.

The context **window** these budgets are measured against is discovered from the
server — live, not once: apogee asks every ten seconds, so switching the loaded model
under a running session re-binds the window with it. Set `context-window:` (a file-only
key, in tokens) only when your server does not advertise a window, or when its number is
wrong for how you run it; that key is a **pin** the heartbeat never overrides. With no
window known, the Budget and automatic compaction stay inactive and apogee says so in the
transcript the moment it binds a model without one.

The prompt's caret is the **real terminal cursor**, and it never blinks. Set
`cursor-shape:` (a file-only key) to `block` (the default), `underline`, or `bar` to say
which shape it takes; your terminal's own cursor comes back when apogee exits. A
full-screen terminal program has to name a cursor shape on every frame, so inheriting the
one your terminal is configured with is not something apogee can express while it runs —
this key is the honest substitute. The cursor is shown wherever the box is editable
(including while the model works) and hidden where it is not, such as at an approval
prompt.

### The upstream API key

A local server usually wants no credentials, but some do: llama.cpp started with
`--api-key`, LM Studio, a remote vLLM, any keyed OpenAI-compatible proxy. Give
apogee that token and it rides **every** wire to the endpoint as
`Authorization: Bearer <key>` — your conversation, the ten-second heartbeat, and
both halves of `apogee probe` — so a keyed server never leaves the footer stuck
on a `401` while the session works.

```yaml
# ~/.apogee/config.yaml
api-key: sk-my-server-token
```

```console
$ APOGEE_API_KEY=sk-my-server-token apogee
```

The environment variable **overrides** the file, and there is **no `--api-key`
flag** on purpose: a secret typed on the command line lands in your shell history
and in `ps` output on every OS. Leave the key unset — the local default — and no
`Authorization` header is sent at all, exactly as before this key existed.

The value is never displayed: `apogee probe` reports only *whether* a key was
resolved (`api key: configured (sent as a bearer token)`), and the provider client
redacts it from any error text the server echoes back. One caveat is yours to
weigh: `config.yaml` is plain text, so on a shared machine prefer the environment
variable, or restrict the file's permissions yourself.

### The system prompt

The system prompt is the standing instruction apogee sends ahead of your first
message, as the first system message of every request. A fresh install starts with
a short default one; **delete it or comment it out to send no system prompt at
all**. An existing `~/.apogee/config.yaml` — which an upgrade never touches —
carries no such key, so nothing changes for a setup you already run.

```yaml
# ~/.apogee/config.yaml
system-prompt-text: |
  You are apogee, a coding agent working in the workspace at {{workspace}}.
  Today's date is {{datetime}}. You are operating in {{mode}} mode.

# ...or keep it in a file of its own instead (never both at once):
# system-prompt-file: ~/prompts/apogee.md

# ...and optionally override it for one model:
# system-prompt-models:
#   qwen2.5-coder:
#     system-prompt-text: "Be terse. Use tools; do not narrate."
```

Three placeholders are substituted fresh on every request: `{{workspace}}` (the
workspace path), `{{datetime}}` (today's **date** — not a timestamp, which would
change the prompt every turn and throw away your server's prefix cache), and
`{{mode}}` (the autonomy mode, so a Shift+Tab shows up from the next request on).
The spelling is strict and the set is closed — anything else in double braces,
`{{ workspace }}` included, is a startup error listing the three.

A `system-prompt-models:` entry keyed by the model name apogee resolves at startup
**replaces** the global prompt for that model, whole; an entry naming a model you
are not running is simply inert — never selected, its file never read — so one
config can carry a prompt per model across every machine it travels to. Sub-agents
inherit your prompt; apogee's own internal calls (the conversation summariser,
`apogee probe`'s battery) keep their dedicated prompts and never see it; and the
prompt never enters your conversation history or a saved session.

### Showing a finished document

When the model finishes a deliverable — a report, a review, an HTML summary — it calls
`present_document` and hands apogee nothing but the path. **Apogee decides how to show
it; the model never reasons about your platform.** Whatever it decides, the document's
workspace-relative path is always printed in the transcript, which most terminals (Zed,
VS Code, iTerm2, WezTerm, kitty) make cmd/ctrl+clickable. Above that baseline: on your
own desktop the file is opened in its associated application (HTML in your default
browser) — documents, images and text only, because anything the OS would *run* rather
than show (a `.bat`, a `.command`, a `.desktop`) is left as a path for you to open
deliberately; over SSH — a devbox, a VM, a container — browser-renderable documents
(`.html`, `.htm`, `.svg`, `.pdf`) are served from a small built-in server and the URL is
printed beside the path, so one cmd+click opens the document in the browser on *your*
machine. Apogee never auto-opens on the remote box: there is no display there to open
into. If a rung fails, the transcript says so and falls back to the path.

The built-in server hands out one random-token URL per presented document — no directory
listing, no other file reachable — re-reads the file per request, starts only when a
document is actually served, and stops when apogee exits. Four **file-only** keys tune
all of this:

```yaml
# ~/.apogee/config.yaml
present:
  auto-open: true        # open documents on a LOCAL desktop run; false = only print the path
  command: "zed {path}"  # open with THIS application instead of the OS default
  port: 0                # the built-in server's port; 0 (default) picks a free one per session
  host: ""               # address the printed URL advertises; empty = detected
```

`host` is a fallback, not an override: over SSH the address you connected to this box on
is used, because it is known-routable. If a printed URL is unreachable on **macOS
Sequoia or later**, the first browser connection to a local-network address needs Local
Network permission — Chrome fails with a generic "this site can't be reached" until you
allow it in System Settings → Privacy & Security → Local Network, while Safari tends to
work straight away. The path line works regardless.

### Auto mode's blast radius

Auto is the one unsupervised mode, so it is fenced: filesystem writes are confined to
the workspace at the OS level, the network is open, and MCP still asks. All three
platforms have a backend — landlock on Linux, `sandbox-exec` on macOS, a restricted
low-integrity token on Windows. Where the OS cannot fence a command — a Windows build
older than 10 1809 (17763), and most containers, where landlock reports `ENOSYS`
regardless of kernel version — Auto keeps the promise the honest way and asks before
each shell call instead of running it unbounded ("confine if you can, gate if you
can't"). That is not a fault, so Apogee says so at startup rather than letting Auto
look broken.

**On Windows the fence is a token, and the box is a mark on your disk.** No Windows
facility takes "these paths are writable" as an argument, so the command runs under a
restricted, *low-integrity* token — the kernel then denies it any write to an object
that is not explicitly marked low, and the denial is inherited by every process it
spawns. The workspace is what carries that mark for the session, and it is reverted on
exit; an interrupted run leaves a journal behind, which `apogee probe` reports. Two
things worth knowing before you use it: network egress is **not** claimed on Windows
(the network is open there exactly as elsewhere, and a box that asks for network *deny*
is refused rather than silently ignored), and the marking pass costs roughly a
millisecond per file or directory — with a large `.git` or `node_modules` in the
workspace, the first confined command of a session visibly pauses while it runs
(measured: ~5 s to mark a 5,000-object tree, ~2 s to revert it), after which every later
command in that session pays nothing. And one limit: what the Windows fence covers is
workspace-scoped writes. A low-integrity process cannot write to an unmarked directory
at all, so a confined `go build`, `pip install` or `npm ci` fails when it reaches its
cache or `%TEMP%` outside the workspace — giving the toolchain a box-local temp and
cache directory is a recorded follow-on (`TODO.md`), not something Apogee does yet.

If the machine is disposable and you would rather have Auto unfenced there, `/confine`
is the route. `/confine` (or `/confine status`) reports the backend, what it can
actually enforce here, this host's id, and the effective setting. `/confine off` runs
Auto unconfined **for this session** and writes nothing; `/confine off --save` also
records this machine in `~/.apogee/config.yaml`, comments and formatting intact:

```yaml
# ~/.apogee/config.yaml
unconfined-hosts:
  - id: "devbox-a1b2c3"                # this machine's id — /confine reports it
    acknowledged: "2026-07-21"
    note: "disposable container, landlock unavailable"
```

The acknowledgement is **host-scoped on purpose**: "this machine is disposable" is a
claim about one machine, so it must not travel with your config file onto a laptop. The
id is a safety interlock, not authentication — it fails closed, so an unrecognised
machine is simply confined again. Delete the entry to re-confine a host; `/confine on`
does the same for the running session.

`confine-to-workspace: false` remains the global blanket loosen and still means *every*
host. Both keys are **global-config-file-only** — no flag, no environment variable, and
no project config — because editing that file is the deliberate acknowledgement, and a
repo you cloned must never be able to make that claim for you.

## Diagnosing a host — `apogee probe`

`apogee probe` answers "what would Auto do on this machine?" without running an agent.
It reads `config.yaml` and the `APOGEE_*` environment exactly as a session would, and
reports the OS/arch, the confinement backend and what it can *actually* enforce here,
the Auto verdict, the effective `confine-to-workspace` after any host acknowledgement,
the workspace root and config home, and whether the configured endpoint answers
(`/v1/models`, plus llama.cpp's `/props`). It is free, offline and **read-only** — no
model is called, no starter config is seeded, nothing is written. `apogee probe host`
is the same report under a named child, for scripts.

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

`apogee probe model` is the other half, and it is deliberately an **explicit act**
rather than something the bare noun triggers, because it costs live model calls *and*
writes. It runs a three-part capability battery — a native tool call, JSON/structured
output, and a multi-step tool chain — then prints what it observed, an ordinal
capability tier, and the model-profile knobs the findings suggest as paste-ready YAML
(your `config.yaml` is never edited). It also records a **behavioral fingerprint**: the
model keeps its advertised name — probing never renames it, so Validated-set entries,
aliases and Library observations keyed on that name keep matching — but its identity
rises from *low* to *medium* confidence, which is what promotes a matching Validated
set from offered to auto-applied on later runs. `--no-save` runs the whole battery and
records nothing; the record's path is printed either way, so deleting that file undoes
it.

## Building from source

**Prerequisites:** Go 1.26+ (the toolchain version pinned in `go.mod`).

```bash
git clone https://github.com/airiclenz/apogee.git
cd apogee
make build      # compiles ./apogee
./apogee --help
```

A `Makefile` wraps the common Go invocations:

| Command | Does |
|---|---|
| `make build` | Compile the binary to `./apogee` |
| `make run ARGS="--help"` | Build-and-run, passing flags via `ARGS` |
| `make test` | Run the test suite with the race detector |
| `make cross` | Cross-build all six release targets (Linux/macOS/Windows × amd64/arm64) |
| `make check` | The full acceptance gate — gofmt, vet, build, race tests, cross-build |
| `make help` | List every target |

Prefer the raw toolchain? `go build -o apogee ./cmd/apogee` does the same thing — the
Makefile just gives the common commands one-word names. Releases are cross-compiled to
all **six** targets — Linux, macOS and Windows × `amd64` and `arm64` — from any one of
them: the tree is CGO-free, so `make cross` (or six `GOOS=… GOARCH=… go build ./...`
invocations) is the whole release matrix, and every OS-specific backend is behind a
build tag rather than a separate artifact.

> **Note:** launch the TUI with `apogee --endpoint <openai-compatible-url> --model <name>`
> to hold a real coding conversation with a local model. All four autonomy modes, the
> full tool suite, MCP, sub-agents, and skills are live; Auto mode runs fully unattended
> where OS confinement is actually available — Linux landlock, macOS seatbelt, and a
> Windows low-integrity token on build 17763 or newer — and where it is not (an older
> Windows, or a container with neither), Auto gates each shell/subprocess call through
> approval and tells you why. `apogee probe` says which case this machine is in. See
> [Auto mode's blast radius](#auto-modes-blast-radius).

## License

MIT — see [LICENSE](LICENSE).
