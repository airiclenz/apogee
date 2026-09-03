<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="graphics/apogee-logo-light.svg">
    <source media="(prefers-color-scheme: light)" srcset="graphics/apogee-logo-dark.svg">
    <img src="graphics/apogee-logo-light.svg" alt="apogee — a terminal AI coding agent for local LLMs" width="200">
  </picture>
</p>

<p align="center">
  <a href="https://github.com/airiclenz/apogee/releases/latest"><img src="https://img.shields.io/github/v/release/airiclenz/apogee?label=release" alt="Latest release"></a>
  <img src="https://img.shields.io/badge/platforms-Windows%20%C2%B7%20macOS%20%C2%B7%20Linux-blue" alt="Runs on Windows, macOS and Linux">
  <a href="https://go.dev"><img src="https://img.shields.io/github/go-mod/go-version/airiclenz/apogee" alt="Go version"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-green" alt="MIT licence"></a>
</p>

**apogee is an open-source AI coding agent that runs in your terminal and works with
local LLMs.** Point it at a local model server — llama.cpp, Ollama, LM Studio, vLLM —
and your code never leaves your machine: no API key, no cloud, works offline. Point it
at any OpenAI-compatible cloud endpoint instead and the same agent runs there.

<p align="center">
  <img src="graphics/demo.gif" alt="apogee, an AI coding agent in the terminal, finding a failing Go test, fixing the bug and proving the tests pass — with a follow-up instruction typed mid-run, queued and delivered at the next tool boundary, the fix shown as a side-by-side diff, and a closing /undo preview that reverts nothing">
</p>

Either way you get a real coding agent: it reads your code, edits files, runs commands
and tests, uses git, searches the web, and hands work to sub-agents — in a loop, until
the task is done. It is a single binary on Windows, macOS and Linux, and it runs in any
terminal, including the one inside VS Code, Zed, or your own IDE.

## Why apogee

Three things set it apart from other AI coding assistants.

- **Small local models do real work here.** Most agents quietly assume a frontier
  model. apogee gives every model a *floor*: six always-on guards that catch what a
  model gets wrong on its own. A malformed tool call is repaired and retried, a call
  the model keeps repeating is broken out of, an empty reply is retried, a model that
  narrates instead of acting is told to act, a pointless re-read of a file it already
  read is cut short, and a huge stale tool result is trimmed on its way back to the
  model rather than in the conversation itself. Each only changes what the model sees *after its own mistake*, so they lift a
  small model without getting in a big one's way — and each can be switched off. The
  rule behind them is the whole project: nothing apogee puts in front of a model may
  make that model perform worse than the bare loop, and anything more opinionated than
  the floor has to earn its place on an eval bench before it ships turned on.
- **Autonomy fenced by the operating system, not by a prompt.** Four autonomy modes run
  from read-only Plan up to unsupervised Auto — and Auto is confined at the OS level on
  all three platforms (Linux landlock, macOS seatbelt, a restricted Windows token), so
  an unsupervised agent *cannot* write outside your workspace, rather than being asked
  nicely not to. Where the OS genuinely cannot enforce the fence, apogee asks before
  each command instead of running it unbounded.
- **A complete agent, in a UI that gets out of your way.** The whole loop is here —
  file edits, shell, git, tests, web, MCP servers, skills, parallel sub-agents — inside
  a terminal UI built with care: type your next message while the model streams and
  queue it into the running task, recall any prompt you have sent, fold away what you
  are done reading, click every path it prints, and undo the agent's file writes one
  exchange at a time.

Under the hood apogee is an embeddable Go engine, and the terminal UI is its first
front-end rather than its identity: `apogee headless`, `apogee daemon` and the eval
bench are further front-ends over the same core.

## Install

Three ways in, and all three land the same thing: one static binary, no runtime beside
it.

**Homebrew — macOS and Linux:**

```bash
brew tap airiclenz/tap
brew trust --tap airiclenz/tap   # once per machine, on Homebrew 5.1+
brew install apogee
apogee --version
```

The formula installs the prebuilt binary for your platform — nothing is compiled, no Go
toolchain needed; `brew upgrade apogee` moves you to the next release. The `brew trust`
line is what Homebrew 5.1+ wants before loading a third-party tap;
`brew untrust --tap airiclenz/tap` revokes it.

**A prebuilt archive — Windows, macOS, or Linux, `amd64` or `arm64`.** Every release
carries all six targets on the
[releases page](https://github.com/airiclenz/apogee/releases/latest), each archive with
a `SHA256SUMS` file beside it.

```bash
# macOS / Linux — resolves the latest release; to pin one, set VERSION=<x.y.z> instead
VERSION=$(curl -fsSL https://api.github.com/repos/airiclenz/apogee/releases/latest | sed -n 's/.*"tag_name": *"v\([^"]*\)".*/\1/p')
PLATFORM=darwin_arm64   # or darwin_amd64 · linux_amd64 · linux_arm64

curl -fsSLO "https://github.com/airiclenz/apogee/releases/download/v$VERSION/apogee_${VERSION}_${PLATFORM}.tar.gz"
tar -xzf "apogee_${VERSION}_${PLATFORM}.tar.gz"
sudo install -m 0755 "apogee_${VERSION}_${PLATFORM}/apogee" /usr/local/bin/apogee
apogee --version
```

On Windows, download `apogee_<version>_windows_arm64.zip` (or `_amd64`), unpack it, and
put `apogee.exe` somewhere on your `PATH`.

The binaries are **not code-signed** yet: on macOS, a *browser* download is quarantined —
`xattr -d com.apple.quarantine ./apogee` clears that (the `curl` above never sets it) —
and Windows SmartScreen may warn about an unrecognised publisher. `SHA256SUMS` is the
check actually worth making.

**From source:** a clone plus `make build` — see
[Building from source](docs/manual/building.md). One warning: `go install …@main` works,
but **never `@latest`** — proxy.golang.org immutably retains retired `v1.x` versions, so
`@latest` resolves to a stale build.

## Quick start

```bash
apogee
```

On first run apogee writes a documented starter config to `~/.apogee/` and asks which
server to talk to. Or skip the config entirely and point one session straight at a
server — no GPU needed, a free OpenRouter model will do (a key is at
[openrouter.ai/keys](https://openrouter.ai/keys)):

```bash
export APOGEE_API_KEY=sk-or-your-key
apogee --endpoint https://openrouter.ai/api --model google/gemma-4-31b-it:free
```

or a local one:

```bash
apogee --endpoint http://localhost:8080 --model qwen3-coder
```

Then just describe what you want done. `Shift+Tab` cycles the autonomy mode — Plan
(read-only) → Ask-Before → Allow-Edits → Auto — `/` opens the command menu, `@`
references a file, and a double-tap of `esc` — twice within one second, so a stray key
cannot do it — stops a run. The full tour is in [the manual](docs/manual/README.md).

## What apogee can do

### Models and servers

- **Any OpenAI-compatible endpoint**, local or remote. A local llama.cpp, Ollama,
  LM Studio or vLLM server keeps every byte on your machine and needs no key.
- **Keys stay out of your config file.** A server entry can pull its key from a command
  or an environment variable, so the token lives in your password manager or keychain;
  apogee offers to move a plaintext key into your OS secret store on startup.
- **Model profiles** adapt to models that don't speak native tool calls: tool menus
  injected as text, fenced or custom-regex calls parsed back out, thinking channels
  stripped — while native models stay byte-identical on the wire. A profile can carry
  its own tool list too, so a small model sees fewer, clearer tools.
- **Switch without restarting.** `/model` and `/server` move the session; `/effort` sets
  how hard the model thinks, from the levels your model actually reports.
- **Sub-agents can run on a different server than you do** — a small model steering
  while a bigger one does the heavy reading, or the reverse. You choose the server, or
  let the model pick per job.
- **[llama-launcher](https://github.com/airiclenz/llama-launcher) integration** — load,
  switch and stop local model servers from `/model`, and remember your pick per server.

### The agent loop

- **28 built-in tools**: read, write and edit files, grep and find, git, terminal,
  Python, test runners, web fetch and web search, and delegation to sub-agents.
- **Parallel sub-agents**, each with a context window of its own. Open one as its own
  full screen to watch it work, and type to it while it runs.
- **Skills** — short markdown playbooks you invoke with `/name`. apogee ships a few
  (debugging, planning, code review, commit hygiene), reads your own from
  `~/.apogee/skills`, and picks up skills a repository ships to everyone working in it.
  As you type, it names the closest few above the input box; `Tab` picks one.
- **MCP servers** over stdio, SSE, or streamable-http, for tools apogee doesn't ship.
- **A Console family, off by default** — the REPLs, shells and dev servers a model
  keeps alive across turns, for models that ask for them.
- **Long jobs don't fall off the context window.** apogee compacts the conversation,
  trims stale tool output, skips a re-read of a file that hasn't changed, and folds a
  sub-agent's own history while it works.
- **PDFs read as text**, page by page, whether the model opens one or you attach it.

### Safety and control

- **Four autonomy modes** — read-only Plan, Ask-Before, Allow-Edits, and OS-confined
  Auto. `Shift+Tab` cycles them at any time, mid-run included, and `/confine` reports or
  changes [Auto's blast radius](docs/manual/configuration.md#auto-modes-blast-radius).
- **A dangerous-action guard in every mode** — the genuinely destructive commands are
  refused outright, and the merely alarming ones are put in front of you first.
- **Approvals you grant once mean what you think they mean**: they are scoped to the
  call you approved and honoured across the whole sub-agent tree, and the prompt shows
  the path a call really resolves to before you answer.
- **Allow and deny lists for anything that reaches the network** — the web tools, MCP
  endpoints, and the model endpoint itself. Subprocesses never see your API key.
- **`/undo`** — put back the files the agent wrote, one exchange at a time, with a
  preview before anything is touched.

### The terminal UI

- **Type — and select — while it works.** The prompt box stays live during a run: queue
  your next message into the running task, walk back through every prompt you have sent,
  select transcript text mid-stream.
- **Read what you want, hide what you don't.** Fold any block or group of tool calls,
  scroll with the keyboard or the mouse, and click any path apogee prints to open it.
- **Side-by-side diffs** for every file the agent writes.
- **`/thinking`** shows the model's reasoning as plain text, **`/inspect`** shows the
  raw traffic, and **`/usage`** shows what the session cost — the main agent and each
  sub-agent, cache hits included.
- **Colour schemes** as single YAML files, switchable live, with your own beside the
  built-in `dark` and `light`.

### Sessions, schedules and scripts

- **Sessions that survive anything** — every completed turn autosaves;
  `apogee --continue` resumes where you left off, `/sessions` browses, renames and
  deletes, and an interrupted task picks up with `/continue`. Optional retention rules
  keep the store from growing for ever. See [Sessions](docs/manual/sessions.md).
- **Scheduled prompts** — `/schedule` runs a prompt on a cycle while apogee is open;
  [`apogee daemon`](docs/manual/daemon.md) keeps standing schedules running under your
  OS's supervisor, every firing saved as a session you can browse.
- **Scriptable** — [`apogee headless`](docs/manual/headless.md) runs one prompt
  unattended with clean stdout and meaningful exit codes.
- **[`apogee probe`](docs/manual/probe.md)** reports what this host, model and terminal
  can actually do, without running an agent or calling a model.
- **Deliverables you actually see** — a finished report is opened on your desktop, or
  served over a one-off link when apogee runs on a remote box.

### Configuration

- **A settings screen** — `/settings` shows every resolved setting, says where each
  value came from, and writes one key at a time with your comments and layout intact.
- **A watched config** — edits to `~/.apogee/config.yaml` from anywhere apply to the
  running session; nothing waits for a restart.
- **Your own system prompt**, replacing apogee's or appended to it, globally or per
  model.
- **Turn any tool off** — or on — for every model or for one, because a shorter tool
  list is often the thing that makes a small model better.

## Documentation

The [manual](docs/manual/README.md) carries the full reference:

| Page | Covers |
|---|---|
| [Commands](docs/manual/commands.md) | Every in-chat command, skills, `@file` references, the keys, `/undo`, `/settings` |
| [Sessions](docs/manual/sessions.md) | Saving, resuming, browsing, renaming conversations |
| [Configuration](docs/manual/configuration.md) | `config.yaml` end to end: servers, API keys, model profiles, tools, the floor guards, the system prompt, confinement |
| [`apogee probe`](docs/manual/probe.md) | Diagnosing what a host, model, and terminal can do |
| [`apogee headless`](docs/manual/headless.md) | One unattended prompt, for scripts |
| [`apogee daemon`](docs/manual/daemon.md) | Standing schedules that outlive the session |
| [Building from source](docs/manual/building.md) | Prerequisites, Makefile targets, cross-compilation |

Working on this repo *with* a coding agent? [`AGENTS.md`](AGENTS.md) is the agent-facing
map — where the docs live, and the conventions that aren't derivable from the code.

## Status

**Pre-production `0.x` on `main`.** Under SemVer a `0.x` version makes no API-stability
promise — the Go API may still move while the tool hardens — but every release ships
prebuilt binaries for all six targets and a Homebrew formula. Functionally the loop is
complete: full tool suite, MCP client, parallel sub-agents, skills, sessions, schedules,
and OS-confined Auto mode on all three platforms. What changed lately lives in the
[CHANGELOG](CHANGELOG.md); what is next lives in the issue register (`bd`).

## Why Go

Portability is the point. Go cross-compiles to a single static binary with no runtime —
the gold standard for "drop into any terminal on any OS" — and it lets one language
cover both the agent and the bench that evaluates it. The TUI is built on the Charm
stack (Bubble Tea, Lipgloss, Bubbles) with Cobra for the CLI.

## License

MIT — see [LICENSE](LICENSE).
