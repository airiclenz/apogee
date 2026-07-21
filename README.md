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
your code stays on your machine, no API key is required for local models, and you
get a full agentic tool-use loop with sensible guardrails.

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

**`v1.7.0` shipped (2026-07-21).** The embeddable agent core is stable — the public
Go API follows semver from `v1.0.0` — with the full tool suite, MCP client,
sub-agents, and OS-confined Auto mode (Linux landlock / macOS seatbelt; Windows
confinement comes in a later phase, so Auto there still runs — it just falls back
to asking before each shell/subprocess call instead of confining it). The same
fallback applies wherever the facility is missing rather than unimplemented — most
containers, where landlock reports `ENOSYS` whatever the kernel version. Apogee
says so at startup and offers `/confine` as the way out (see
[Auto mode's blast radius](#auto-modes-blast-radius)). Current work is per-model
bench validation of the mechanism catalogue: the full catalogue is ported, and
the first Validated set (gemma-4-E4B) ships with the binary.
See [`docs/plans/`](docs/plans/) and the [`CHANGELOG`](CHANGELOG.md) for what's
next.

## Key capabilities

- **Model-agnostic, local-first** — any OpenAI-compatible endpoint; zero data leaves
  your machine with a local model.
- **Agentic tool use** — multi-step loop with file edits, shell, search, git, web,
  and sub-agents.
- **Deliverables you actually see** — `present_document` ends a report-producing task
  by showing the file: opened on your desktop when apogee runs locally, served over a
  one-off link when it runs on a remote box, and always printed as a clickable path
  in the transcript. See [Showing a finished document](#showing-a-finished-document).
- **Four autonomy modes** — Plan (read-only), Ask-Before (writes need approval),
  Allow-Edits (workspace-scoped writes auto-approved), Auto (autonomous, confined
  at the OS level via Linux landlock / macOS seatbelt; where the OS cannot fence a
  command, Auto asks before it rather than running it unbounded).
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
path, and an `@path` in a message hands that file to the model.

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
overrides the built-in default. A fully-commented starter `config.yaml` is written
to `~/.apogee` on first run (your edits are never overwritten). Some settings are
**file-only** (no flag or env) — the model profile, MCP servers, web-search
endpoint, and the small-model mechanisms.

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
server at startup — for a pinned `model:` too. Set `context-window:` (a file-only
key, in tokens) only when your server does not advertise a window, or to start a
pinned model offline; with no window known, the Budget and automatic compaction stay
inactive and apogee says so once at startup.

### Showing a finished document

When the model finishes a deliverable — a report, a review, an HTML summary — it calls
`present_document` and hands apogee nothing but the path. **Apogee decides how to show
it; the model never reasons about your platform.** Whatever it decides, the document's
workspace-relative path is always printed in the transcript, which most terminals (Zed,
VS Code, iTerm2, WezTerm, kitty) make cmd/ctrl+clickable. Above that baseline: on your
own desktop the file is opened in its associated application (HTML in your default
browser); over SSH — a devbox, a VM, a container — browser-renderable documents
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
the workspace at the OS level, the network is open, and MCP still asks. Where the OS
cannot fence a command — Windows today, and most containers, where landlock reports
`ENOSYS` regardless of kernel version — Auto keeps the promise the honest way and asks
before each shell call instead of running it unbounded ("confine if you can, gate if
you can't"). That is not a fault, so Apogee says so at startup rather than letting Auto
look broken.

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
Makefile just gives the common commands one-word names.

> **Note:** launch the TUI with `apogee --endpoint <openai-compatible-url> --model <name>`
> to hold a real coding conversation with a local model. All four autonomy modes, the
> full tool suite, MCP, sub-agents, and skills are live; Auto mode runs fully unattended
> where OS confinement is actually available (Linux landlock and macOS seatbelt today —
> Windows lands in a later phase, and a container may have neither), and where it is not,
> Auto gates each shell/subprocess call through approval and tells you why. See
> [Auto mode's blast radius](#auto-modes-blast-radius).

## License

MIT — see [LICENSE](LICENSE).
