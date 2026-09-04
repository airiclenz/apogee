# apogee — agent guide

"apogee" (spelled lower-case) is a terminal coding agent (Go, Bubble Tea TUI) built for smaller, locally hosted LLMs — while working even better with bigger ones — running a full agentic tool-use loop. Its hard invariant: nothing apogee puts in front of a model may make that model perform worse than the bare loop — the **Floor guards** and structural reducers that ship on for every model are held to it, and any model-facing behaviour above them (the **Mechanism** lab surface; **Bypass mode** is its off-switch) ships off until bench evidence turns it on. This file is the agent-facing counterpart to `README.md`: it maps where knowledge lives and states the conventions you cannot derive from the code.

## Where knowledge lives

- `CONTEXT.md` — the domain language and concept map (Floor guards, Mechanisms, Bypass, Steps,   Turns, …). Terms used in code and docs are defined here; read the relevant   section before design work. Large — read sections, don't load it wholesale.
- `docs/manual/` — the user-facing reference manual (commands, sessions, configuration, probe, headless, daemon, building). `README.md` is the plain-language front door; detail lives here, not there.
- `docs/adr/` — architectural decision records. Settled questions live here; check for an ADR before re-opening one.
- `docs/design/` — design contracts (confinement execution contract, MCP client) plus the `tool-surface-findings.md` record; the mechanism catalogue is archived under `docs/design/archived/` and carries the retirement wave's per-row verdicts (ADR 0071).
- `docs/design/test-drivers.md` — how to drive the TUI, script an upstream and judge a frame in `go test`; a test step is manual only where its table says so.
- `docs/plans/` — implementation plans in the house format: numbered `## N.` H2 items with What/Tests/Acceptance/commit. Plans are saved repo docs, executed item-by-item; completed plans get archived.
- `docs/handoffs/` — multi-session work-in-flight handoffs (superseded ones move to `docs/handoffs/archived/`).
- `docs/reviews/` — saved review reports.
- `layout.md` (repo root, not `docs/`) — the TUI layout/rendering spec in prose.
- **Issue register — `bd` (beads), not a file.** Open defects and parked work live in the beads tracker (`bd ready`, `bd show <id>`, `bd create`); `.beads/` holds the local Dolt DB, and `.beads/issues.jsonl` is a passive export — committed so a clone without `bd` can still read the register, but never the source of truth. `CHANGELOG.md` (under `[Unreleased]`) remains the sole closed trail — closing a bead does not write a changelog entry, so do both. `ISSUES.md` was migrated into beads on 2026-09-03 and deleted; the mentions of it that survive in `CHANGELOG.md`, `docs/adr/`, `docs/reviews/` and archived plans are historical and stay.
- `CHANGELOG.md` + `VERSION` — `VERSION` micro-bumps per shipped feature; `CHANGELOG.md` collects those under `[Unreleased]` and only gains a release heading when a release is cut. Versioning is deliberately 0.x while pre-production. Pushing a `VERSION` change to `main` auto-creates the annotated `vX.Y.Z` tag at that commit — every bump in the push, each at its own commit — so a bump is always a commit of its own and, at a release cut, the **last** one: the `CHANGELOG.md` rollup lands first, so the tagged tree already carries it.

## Conventions not derivable from the code

- **Pre-production policy (current phase):** commit directly to `main` — no feature branches or PRs. Commit and push only when the owner asks.
- **No AI attribution trailers** in commit messages (no Co-Authored-By / "generated with" lines); a local commit-msg hook strips them as a backstop.
- **Git hooks live in `.beads/hooks/`, not `.git/hooks/`.** `core.hooksPath` points there, so that directory is the live path; `.git/hooks/commit-msg` is a deliberate dormant fallback — byte-identical to `.beads/hooks/commit-msg`, kept for a checkout that leaves `core.hooksPath` unset. bd manages five of the six hooks (`pre-commit`, `post-merge`, `pre-push`, `post-checkout`, `prepare-commit-msg`) inside `BEGIN/END BEADS INTEGRATION` markers and preserves anything written outside them; `commit-msg` carries no markers and is absent from `bd hooks list`, so the attribution stripper survives a `bd hooks install`. `export.git-add` is `false` (bd's default), so the pre-commit hook writes `.beads/issues.jsonl` and stages nothing — and it exports at all only when a `.beads/` path is already staged (`pre-commit: skipping JSONL export — no staged .beads paths` otherwise). **The staged-file sweep does not reproduce** (2026-09-04, throwaway clone hydrated with `bd init --prefix apogee` + `bd import` until `.beads/embeddeddolt` existed and the hook was seen rewriting `.beads/issues.jsonl`; bd shim 1.2.2): commits carrying a staged deletion plus a staged edit — with and without a staged `.beads/` path, and with unstaged and untracked files present — landed exactly the index and nothing more, so ordinary commits need no `--no-verify`. What did reproduce is **`bd init`'s own auto-commit**: run in a clone with a deletion and an edit already staged, its "bd init: initialize beads issue tracking" commit swallowed both — that, not the pre-commit hook, is what took the retirement wave's 22 staged deletions. Never run `bd init` with a dirty index. One consequence to know: because the hook re-exports *after* git has snapshotted the index, committing a staged `.beads/issues.jsonl` commits the pre-hook content and leaves the fresh export unstaged — re-stage and amend when the export itself is the point of the commit.
- Run `make check` before committing; a multi-item skill run satisfies this once, at its closeout — per-item commits run the item's targeted acceptance instead.
- **Regressions are never deferred.** A regression — behaviour that worked at the previous commit or release and does not now, or a path, name or value apogee itself announces to the user or model (an orientation line, a skill header, a `{{…}}` expansion) that a tool, mode or guard refuses or prompts on — is fixed in the run that finds it or blocks that run's closeout. It never becomes a deferred bead and never ships; the 2026-08-26 read-root residual that shipped in v0.18.2 is the case this rule closes.
- Distribution: a Homebrew tap (`airiclenz/tap`, binary formula) plus six prebuilt archives per release (`make dist`); building from source stays fully supported. Never `go install …@latest` — proxy.golang.org still serves the deleted v1.x tags from its immutable cache, so `@latest` resolves to stale `v1.7.0`; known, not a bug (`@main` and `@<sha>` are fine).
- Config home is a single `~/.apogee` dotdir on every OS (like `~/.aws`). Settled decision — do not propose XDG / os.UserConfigDir`.
- **Stack facts:** the TUI is Bubble Tea **v2** (`charm.land/bubbletea/v2`): key events are `tea.KeyPressMsg` matched on `msg.String()` (`"esc"`, `"ctrl+c"`); the v1 names (`tea.KeyMsg`, `tea.KeyCtrlC`) do not exist here. Sub-agent briefs that name an API must use the v2 names.
- The Bubble Tea `Model` is copied by value on every `Update`: never let a `strings.Builder` (or any no-copy type) be held by value anywhere it reaches. Rule and guard test are in `internal/tui/` (see `doc.go`, ADR 0011).
- Tests that need a live LLM are gated by `APOGEE_LIVE_ENDPOINT`; without it they skip. `make live-eval` drives the live path.
- Early-development stance: prefer the best long-term architecture over lowest churn; the owner reshapes now to get it right.
- **What beads owns — and what it does not.** `bd` is the issue and task register (see *Where knowledge lives*), and the managed Beads blocks at the foot of this file are bd's own guidance. It does **not** displace the house documents: `docs/plans/` keeps the numbered What/Tests/Acceptance/commit format (a plan is a design artefact, not a TODO list), `docs/adr/` keeps settled decisions, `docs/handoffs/` keeps work-in-flight handoffs, and `CHANGELOG.md` keeps the closed trail. Where a managed block's blanket wording — "no markdown TODO lists", "do NOT use MEMORY.md files" — appears to contradict those, this section wins; the block itself states it is subordinate to repository instructions.
- **A bead a plan has designed is labelled, never re-statused.** When a plan under `docs/plans/` takes ownership of a bead, mark it `bd update <id> --add-label planned --spec-id "<plan path>"` — the label says the design work is done and the bead is ready to implement, the spec-id says which plan owns it (`bd list --spec 'docs/plans/2026-09-03 - 01'`). Do **not** model this as a bd status: `bd ready` matches `status == open` literally, so a custom status — even one declared in the `active` category — drops the bead out of `bd ready` and hides exactly the work that is most ready. A status is also single-valued and loses the fact on the move to `in_progress`, where a label survives. The two queries this buys: `bd ready --label planned` (designed — hand to `/implement-plan`) and `bd ready --exclude-label planned` (still needs design). Established 2026-09-04 over the two `2026-09-03` plans; a plan's own *Out of scope* list disowns beads it merely mentions, so read ownership from the item sections, never from a grep for ids.
- **`.beads/issues.jsonl` can lag the register after a batch write.** `export.auto` is on, but a multi-id `bd update` has been seen landing in the Dolt DB while the passive export kept the old rows (2026-09-04: 21 of 35 updates missing, silently). After any batch write, compare the file against `bd list` and re-export with `bd export -o .beads/issues.jsonl` if they disagree — that committed export is what a clone without `bd` reads.
- **Be context-greedy, and spend that context through sub-agents.** Never act on a guess where the answer is readable: before you design, edit or judge, read the sections of `CONTEXT.md`, the ADR, the plan, the handoff and the neighbouring code that bear on the change, and read enough of each file to see how the surrounding code already does it. Thin context is the failure mode here, not slow work. But the main agent's own window is the scarce resource, so buy that breadth with **sub-agents**: any task that is large, open-ended, or whose size you cannot predict — a repo-wide sweep, "find every call site", "does this pattern exist anywhere", a multi-file audit, a survey of docs before a design — is dispatched to a sub-agent that reads widely and returns only its conclusion. Default to delegating when in doubt about a task's size; a sub-agent that turns out to be overkill costs far less than a main context that fills up mid-run. Do the reading inline only when you already know the file and want one fact from it. Brief a sub-agent with the same discipline you would want yourself: name the paths and documents it must read, state the house facts it cannot derive (see *Stack facts* above), and ask for the finding, not a transcript.
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
