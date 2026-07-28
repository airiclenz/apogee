# Apogee — agent guide

Apogee is a terminal coding agent (Go, Bubble Tea TUI) built to run small, locally
hosted LLMs (~4B–35B) against a full agentic tool-use loop. Its hard invariant: the
gated **Mechanisms** that keep small models on track must never make a model perform
worse than the same agent with Mechanisms off (**Bypass mode** is that floor). This
file is the agent-facing counterpart to `README.md`: it maps where knowledge lives
and states the conventions you cannot derive from the code.

## Where knowledge lives

- `CONTEXT.md` — the domain language and concept map (Mechanisms, Bypass, Steps,
  Turns, …). Terms used in code and docs are defined here; read the relevant
  section before design work. Large — read sections, don't load it wholesale.
- `docs/adr/` — 25+ architectural decision records. Settled questions live here;
  check for an ADR before re-opening one.
- `docs/design/` — design contracts (confinement execution contract, hook
  mutation API, MCP client, mechanism catalogue, technical design).
- `docs/plans/` — implementation plans in the house format: numbered `## N.` H2
  items with What/Tests/Acceptance/commit. Plans are saved repo docs, executed
  item-by-item; completed plans get archived.
- `docs/handoffs/` — multi-session work-in-flight handoffs (superseded ones move
  to `docs/handoffs/archived/`).
- `docs/reviews/` — saved review reports.
- `layout.md` — the TUI layout/rendering spec in prose.
- `ISSUES.md` / `TODO.md` — known issues and deferred work.
- `CHANGELOG.md` + `VERSION` — keep both in step; versioning is deliberately 0.x
  while pre-production.

## Conventions not derivable from the code

- **Pre-production policy (current phase):** commit directly to `main` — no
  feature branches or PRs. Commit and push only when the owner asks.
- **No AI attribution trailers** in commit messages (no Co-Authored-By /
  "generated with" lines); a local commit-msg hook strips them as a backstop.
- Run `make check` before committing.
- Distribution is build-from-source for now (no Homebrew formula, no prebuilt
  binaries published yet), and never `go install` — proxy.golang.org still serves
  the deleted v1.x tags from its immutable cache, so `@latest` resolves to stale
  `v1.7.0`; known, not a bug.
- Config home is a single `~/.apogee` dotdir on every OS (like `~/.aws`). Settled
  decision — do not propose XDG / `os.UserConfigDir`.
- The Bubble Tea `Model` is copied by value on every `Update`: never let a
  `strings.Builder` (or any no-copy type) be held by value anywhere it reaches.
  Rule and guard test are in `internal/tui/` (see `doc.go`, ADR 0011).
- Tests that need a live LLM are gated by `APOGEE_LIVE_ENDPOINT`; without it they
  skip. `make live-eval` drives the live path.
- Early-development stance: prefer the best long-term architecture over lowest
  churn; the owner reshapes now to get it right.

## Tool-specific loading

Claude Code auto-loads only `CLAUDE.md`, not this file. Create a local, untracked
one-line `CLAUDE.md` containing exactly `@AGENTS.md` (it is gitignored in this
repo). Tools that read `AGENTS.md` natively need nothing extra.
