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
  abstraction, a ~30-tool suite (file ops, grep/glob, git, terminal, web,
  sub-agents), an MCP client, sessions, three autonomy modes (Plan / Ask-Before /
  Auto), and security guardrails.
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

🚧 **Early / in active construction.** This is a fresh repository; subsystems are
being built out deliberately. The embeddable agent core comes first (so the
eval/simulation bench can drive it throughout the rewrite), with the TUI layered
on top. See [`docs/plans/`](docs/plans/) for the implementation plan.

## Key capabilities (target)

- **Model-agnostic, local-first** — any OpenAI-compatible endpoint; zero data leaves
  your machine with a local model.
- **Agentic tool use** — multi-step loop with file edits, shell, search, git, web,
  and sub-agents.
- **Three autonomy modes** — Plan (read-only), Ask-Before (writes need approval),
  Auto (autonomous within configured limits).
- **MCP support** — connect external tool servers over stdio / SSE / streamable-http.
- **Model profiles** — adapt to models that don't speak native tool-calls: parse
  markdown-fenced or custom-regex tool calls and strip inline thinking / harmony
  channels, all driven by a per-model profile (native models stay byte-identical).
- **Small-model mechanisms** — compression, validation/auto-retry, syntax + autofix,
  behavioural nudges, and the cross-session Library, each gated so it only fires when
  the model needs it.
- **Validated, not assumed** — every mechanism is A/B-tested against real local models
  via an eval/simulation bench (which imports Apogee as a Go library and drives
  the real loop in-process) before it earns a place in the loop.

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

> **Note:** the interactive TUI now runs (Phase 2 complete) — launch it with
> `apogee --endpoint <openai-compatible-url> --model <name>` to hold a real coding
> conversation with a local model: assistant text streams, tool calls appear, and writes
> prompt for approval (Plan + Ask-Before modes, over the local file toolset). Auto mode,
> the full 30-tool suite, and MCP land in Phase 3. Track progress in
> [`docs/plans/`](docs/plans/).

## License

MIT — see [LICENSE](LICENSE).
