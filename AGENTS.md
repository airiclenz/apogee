# apogee — agent guide

"apogee" (spelled lower-case) is a terminal coding agent (Go, Bubble Tea TUI) built for smaller, locally hosted LLMs — while working even better with bigger ones — running a full agentic tool-use loop. Its hard invariant: the gated **Mechanisms** that give smaller models the help they need must never make any model perform worse than the same agent with Mechanisms off (**Bypass mode** is that floor). This file is the agent-facing counterpart to `README.md`: it maps where knowledge lives and states the conventions you cannot derive from the code.

## Where knowledge lives

- `CONTEXT.md` — the domain language and concept map (Mechanisms, Bypass, Steps,   Turns, …). Terms used in code and docs are defined here; read the relevant   section before design work. Large — read sections, don't load it wholesale.
- `docs/manual/` — the user-facing reference manual (commands, sessions, configuration, probe, headless, daemon, building). `README.md` is the plain-language front door; detail lives here, not there.
- `docs/adr/` — architectural decision records. Settled questions live here; check for an ADR before re-opening one.
- `docs/design/` — design contracts (confinement execution contract, MCP client, mechanism catalogue) plus the `tool-surface-findings.md` record.
- `docs/design/test-drivers.md` — how to drive the TUI, script an upstream and judge a frame in `go test`; a test step is manual only where its table says so.
- `docs/plans/` — implementation plans in the house format: numbered `## N.` H2 items with What/Tests/Acceptance/commit. Plans are saved repo docs, executed item-by-item; completed plans get archived.
- `docs/handoffs/` — multi-session work-in-flight handoffs (superseded ones move to `docs/handoffs/archived/`).
- `docs/reviews/` — saved review reports.
- `layout.md` (repo root, not `docs/`) — the TUI layout/rendering spec in prose.
- `ISSUES.md` - known issues and deferred work; **open only**. A resolved or executed item is REMOVED from it and recorded in `CHANGELOG.md` (under `[Unreleased]`) — the changelog is the sole closed trail. Never leave a done item, or a narration of one, in `ISSUES.md` — never a regression (see Conventions).
- `CHANGELOG.md` + `VERSION` — `VERSION` micro-bumps per shipped feature; `CHANGELOG.md` collects those under `[Unreleased]` and only gains a release heading when a release is cut. Versioning is deliberately 0.x while pre-production. Pushing a `VERSION` change to `main` auto-creates the annotated `vX.Y.Z` tag at that commit — every bump in the push, each at its own commit — so a bump is always a commit of its own and, at a release cut, the **last** one: the `CHANGELOG.md` rollup lands first, so the tagged tree already carries it.

## Conventions not derivable from the code

- **Pre-production policy (current phase):** commit directly to `main` — no feature branches or PRs. Commit and push only when the owner asks.
- **No AI attribution trailers** in commit messages (no Co-Authored-By / "generated with" lines); a local commit-msg hook strips them as a backstop.
- Run `make check` before committing; a multi-item skill run satisfies this once, at its closeout — per-item commits run the item's targeted acceptance instead.
- **Regressions are never deferred.** A regression — behaviour that worked at the previous commit or release and does not now, or a path, name or value apogee itself announces to the user or model (an orientation line, a skill header, a `{{…}}` expansion) that a tool, mode or guard refuses or prompts on — is fixed in the run that finds it or blocks that run's closeout. It never enters `ISSUES.md` as deferred work and never ships; the 2026-08-26 read-root residual that shipped in v0.18.2 is the case this rule closes.
- Distribution: a Homebrew tap (`airiclenz/tap`, binary formula) plus six prebuilt archives per release (`make dist`); building from source stays fully supported. Never `go install …@latest` — proxy.golang.org still serves the deleted v1.x tags from its immutable cache, so `@latest` resolves to stale `v1.7.0`; known, not a bug (`@main` and `@<sha>` are fine).
- Config home is a single `~/.apogee` dotdir on every OS (like `~/.aws`). Settled decision — do not propose XDG / os.UserConfigDir`.
- **Stack facts:** the TUI is Bubble Tea **v2** (`charm.land/bubbletea/v2`): key events are `tea.KeyPressMsg` matched on `msg.String()` (`"esc"`, `"ctrl+c"`); the v1 names (`tea.KeyMsg`, `tea.KeyCtrlC`) do not exist here. Sub-agent briefs that name an API must use the v2 names.
- The Bubble Tea `Model` is copied by value on every `Update`: never let a `strings.Builder` (or any no-copy type) be held by value anywhere it reaches. Rule and guard test are in `internal/tui/` (see `doc.go`, ADR 0011).
- Tests that need a live LLM are gated by `APOGEE_LIVE_ENDPOINT`; without it they skip. `make live-eval` drives the live path.
- Early-development stance: prefer the best long-term architecture over lowest churn; the owner reshapes now to get it right.
- **North star (ADR 0031, tiebreaker force):** the embeddable engine must stay sufficient for any **Driver** (TUI, bench, a future daemon — see `CONTEXT.md`). Its door-keeping invariants — wire-silent engine, wait-tolerant Approver, no first-party connectors, benchable-all-the-way-up — bind design work; a change that closes one of those doors must supersede ADR 0031 explicitly.


<!-- BEGIN BEADS INTEGRATION v:1 profile:minimal hash:970c3bf2 -->
## Beads Issue Tracker

This project uses **bd (beads)** for issue tracking. Run `bd prime` to see full workflow context and commands.

### Quick Reference

```bash
bd ready              # Find available work
bd show <id>          # View issue details
bd update <id> --claim  # Claim work
bd close <id>         # Complete work
```

### Rules

- Use `bd` for ALL task tracking — do NOT use TodoWrite, TaskCreate, or markdown TODO lists
- Run `bd prime` for detailed command reference and session close protocol
- Use `bd remember` for persistent knowledge — do NOT use MEMORY.md files

**Architecture in one line:** issues live in a local Dolt DB; sync uses `refs/dolt/data` on your git remote; `.beads/issues.jsonl` is a passive export. See https://github.com/gastownhall/beads/blob/main/docs/SYNC_CONCEPTS.md for details and anti-patterns.

## Agent Context Profiles

The managed Beads block is task-tracking guidance, not permission to override repository, user, or orchestrator instructions.

- **Conservative (default)**: Use `bd` for task tracking. Do not run git commits, git pushes, or Dolt remote sync unless explicitly asked. At handoff, report changed files, validation, and suggested next commands.
- **Minimal**: Keep tool instruction files as pointers to `bd prime`; use the same conservative git policy unless active instructions say otherwise.
- **Team-maintainer**: Only when the repository explicitly opts in, agents may close beads, run quality gates, commit, and push as part of session close. A current "do not commit" or "do not push" instruction still wins.

## Session Completion

This protocol applies when ending a Beads implementation workflow. It is subordinate to explicit user, repository, and orchestrator instructions.

1. **File issues for remaining work** - Create beads for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update issue status** - Close finished work, update in-progress items
4. **Handle git/sync by active profile**:
   ```bash
   # Conservative/minimal/default: report status and proposed commands; wait for approval.
   git status

   # Team-maintainer opt-in only, unless current instructions forbid it:
   git pull --rebase
   bd dolt push
   git push
   git status
   ```
5. **Hand off** - Summarize changes, validation, issue status, and any blocked sync/commit/push step

**Critical rules:**
- Explicit user or orchestrator instructions override this Beads block.
- Do not commit or push without clear authority from the active profile or the current user request.
- If a required sync or push is blocked, stop and report the exact command and error.
<!-- END BEADS INTEGRATION -->

<!-- BEGIN BEADS CODEX SETUP: generated by bd setup codex -->
## Beads Issue Tracker

Use Beads (`bd`) for durable task tracking in repositories that include it. Use the `beads` skill at `.agents/skills/beads/SKILL.md` (project install) or `~/.agents/skills/beads/SKILL.md` (global install) for Beads workflow guidance, then use the `bd` CLI for issue operations.

### Quick Reference

```bash
bd ready                # Find available work
bd show <id>            # View issue details
bd update <id> --claim  # Claim work
bd close <id>           # Complete work
bd prime                # Refresh Beads context
```

### Rules

- Use `bd` for all task tracking; do not create markdown TODO lists.
- Run `bd prime` when Beads context is missing or stale. Codex 0.129.0+ can load Beads context automatically through native hooks; use `/hooks` to inspect or toggle them.
- Keep persistent project memory in Beads via `bd remember`; do not create ad hoc memory files.

**Architecture in one line:** issues live in a local Dolt DB; sync uses `refs/dolt/data` on your git remote; `.beads/issues.jsonl` is a passive export. See https://github.com/gastownhall/beads/blob/main/docs/SYNC_CONCEPTS.md for details and anti-patterns.
<!-- END BEADS CODEX SETUP -->
