# apogee — agent guide

"apogee" (spelled lower-case) is a terminal AI coding agent (Go, Bubble Tea v2 TUI) built for smaller, locally hosted LLMs — and working even better with bigger ones — running a full agentic tool-use loop over any OpenAI-compatible server or the Anthropic Messages API. Its hard invariant: nothing apogee puts in front of a model may make that model perform worse than the bare loop. The seven **Floor guards** and the structural reducers that ship on for every model form that bare loop — **Bypass mode** ([ADR 0006](.capcom/knowledge/decisions/adr-0006.md)) — and a Floor guard is admitted to it only if it cannot regress it ([ADR 0071](.capcom/knowledge/decisions/adr-0071.md) Decision 1, clause (c)); any model-facing behaviour above them (the **Reaction** core — [ADR 0076](.capcom/knowledge/decisions/adr-0076.md); Bypass switches its model-shaping classes off) is measured against that baseline and ships off until bench evidence turns it on. This file is the agent-facing counterpart to `README.md`: it maps where knowledge lives and states the conventions you cannot derive from the code.

## Where knowledge lives

- **The glossary — the `glossary` Note tree in capcom Knowledge** (`.capcom/knowledge/glossary/`, plain markdown, edited in place) — the domain language and concept map (Floor guards, Reactions, Moments, Bypass, Drivers, Steps, Turns, …). Every term used in code and docs is one Note, found by its name: `capcom know show "<Term>"` reads its definition, and `capcom know tree glossary --depth 1` lists the sections; a code comment's `CONTEXT: <Term>` citation resolves the same way. Read the terms that bear on a change before design work — one Note at a time, never the whole tree.
- `docs/manual/` — the user-facing reference manual (commands, sessions, configuration, reactions, workflows, probe, headless, daemon, updating, building). `README.md` is the plain-language front door; detail lives here, not there.
- **The decision records — the `decisions` Note tree in capcom Knowledge** — architectural decision records, one `adr-NNNN` Note per record with each of its sections as a child Note. Settled questions live here; check for one before re-opening a question: `capcom know show "ADR NNNN"` reads a record, and a new record takes the next number (`capcom know add --parent decisions`).
- `docs/design/` — design contracts (confinement execution contract, MCP client, the `reaction-core-greenfield.md` collapse) plus the `tool-surface-findings.md` and `hook-talkback-findings.md` records and the `workflow-bench-experiment.md` design (its runs live in `apogee-sim`); the mechanism catalogue under `docs/design/archived/` carries the retirement wave's per-row verdicts (ADR 0071) — historical, since the Reaction core superseded that surface (ADR 0076).
- `docs/design/test-drivers.md` — how to drive the TUI, script an upstream and judge a frame in `go test`; a test step is manual only where its table says so.
- `docs/plans/` — implementation plans in the house format: numbered `## N.` H2 items with What/Tests/Acceptance/commit. Plans are saved repo docs, executed item-by-item; completed plans get archived.
- `docs/handoffs/` — multi-session work-in-flight handoffs (superseded ones move to `docs/handoffs/archived/`). **Local only:** `docs/handoffs/.gitignore` ignores everything but itself, so a handoff never reaches a commit or another clone — durable follow-up work belongs in an issue, settled decisions in an ADR.
- `docs/reviews/` — saved review reports.
- `layout.md` (repo root, not `docs/`) — the TUI layout/rendering spec in prose. The per-surface specs it delegates to live under `docs/layout/`: `tool-layout.md` (tool blocks), `split-diff-layout.md` (diff bodies), `prompt-box-layout.md`, `settings-screen-layout.md` and `user-questions-layout.md` (the two decision surfaces).
- **Issue register — capcom (`.capcom/`), not a file.** Open defects and parked work live in capcom, one markdown file per issue under `.capcom/issues/`, versioned and committed with the code (`capcom ready`, `capcom show <id>`, `capcom create --id <slug> --title <title>`). `CHANGELOG.md` (under `[Unreleased]`) remains the sole closed trail — closing an issue writes no changelog entry, so do both. Mentions of `ISSUES.md` or beads (`bd`) in `CHANGELOG.md`, the decision records, `docs/reviews/` and archived plans are historical and stay. Issue files merge through capcom's `capcom-log` driver, defined once per machine in the **global** git config, never repo-local (`docs/manual/building.md` §Setting up a clone).
- `CHANGELOG.md` + `VERSION` — `VERSION` micro-bumps per shipped feature; `CHANGELOG.md` collects those under `[Unreleased]` and only gains a release heading when a release is cut. Versioning is deliberately 0.x while pre-production. Pushing a `VERSION` change to `main` auto-creates the tag at that commit, so a bump is always a commit of its own and, at a release cut, the **last** one (`docs/manual/building.md`, "Versions and tags").

## Conventions not derivable from the code

- **Pre-production policy (current phase):** commit directly to `main` — no feature branches or PRs. Commit and push only when the owner asks.
- **No AI attribution trailers** in commit messages (no Co-Authored-By / "generated with" / session-link lines); a commit-msg hook is the backstop, never the rule.
- **The commit-msg hook in `.githooks/` is that rule's backstop**, active only where `git config --get core.hooksPath` reads `.githooks`; setup and its trust caveat are in `docs/manual/building.md` §Setting up a clone.
- Run `make check` before committing; a multi-item skill run satisfies this once, at its closeout — per-item commits run the item's targeted acceptance instead. **`make test` is the supported way to run the suite**; a raw multi-package `go test` (`./...`, or two heavy packages on one line) is not — its packages load each other and the driven tests in `cmd/apogee` and `internal/tui` report that load as a failure. To debug, narrow to one package and one test (`go test -race -count=1 -run TestX ./cmd/apogee/`). The `APOGEE_TEST_*` knobs for a slow box are in `docs/manual/building.md` §Testing. A duration in a test goes through `internal/testtime` (`Budget`, `Fixed`, `Eventually`), and `WaitQuiet` never stands in for readiness (`docs/design/test-drivers.md` §Waiting).
- **Regressions are never deferred** ([ADR 0101](.capcom/knowledge/decisions/adr-0101.md)). A regression — behaviour that worked at the previous commit or release and does not now, or a path, name or value apogee itself announces to the user or model that a tool, mode or guard refuses or prompts on — is fixed in the run that finds it or blocks that run's closeout.
- Releases: cut by the `release-apogee` skill or by hand with `go run ./cmd/release` ([ADR 0098](.capcom/knowledge/decisions/adr-0098.md); channels, update check and runbook in `docs/manual/building.md` §Releasing and `docs/manual/updating.md`). The repo's own project skills (`release-apogee`, `release-notes-apogee`) live under `.apogee/skills/`, reached from Claude Code through pointers in `.claude/skills/`. Keep the `retract [v1.0.0, v1.8.0]` block in `go.mod`, and never publish a version above v1.8.0 without it (why: `docs/manual/building.md`).
- The global config home is a single `~/.apogee` dotdir on every OS — do not propose XDG or `os.UserConfigDir` ([ADR 0100](.capcom/knowledge/decisions/adr-0100.md)). A **Project config** layers over it, live only by **Adoption** ([ADR 0096](.capcom/knowledge/decisions/adr-0096.md)).
- **Stack facts:** the TUI is Bubble Tea **v2** (`charm.land/bubbletea/v2`): key events are `tea.KeyPressMsg` matched on `msg.String()` (`"esc"`, `"ctrl+c"`); the v1 names (`tea.KeyMsg`, `tea.KeyCtrlC`) do not exist here. Sub-agent briefs that name an API must use the v2 names.
- The Bubble Tea `Model` is copied by value on every `Update`: never let a `strings.Builder` (or any no-copy type) be held by value anywhere it reaches. Rule and guard test are in `internal/tui/` (see `doc.go`, ADR 0011).
- Tests that need a live LLM are gated by `APOGEE_LIVE_ENDPOINT`; without it they skip. `make live-eval` drives the live path.
- Early-development stance: prefer the best long-term architecture over lowest churn; the owner reshapes now to get it right.
- **Public docs speak the shipped vocabulary.** `reactions:` (never `hooks:`, except in the migration notes), `wire: anthropic`, the four modes `plan | ask-before | allow-edits | auto`. The counts the public docs state (tools, Floor guards, Moments) are pinned by drift tests against the live code, so a change to either side fails until the other follows.
- **What capcom owns — and what it does not.** capcom is the issue and task register and, in its Knowledge tree, the home of the glossary and the decision records (see *Where knowledge lives*); the managed capcom block at the foot of this file is capcom's own guidance. It does **not** displace the house documents: `docs/plans/` keeps the numbered What/Tests/Acceptance/commit format (a plan is a design artefact, not a TODO list), `docs/handoffs/` keeps work-in-flight handoffs, and `CHANGELOG.md` keeps the closed trail. Where the block's blanket wording — "track work there and nowhere else" — appears to contradict those, this section wins.
- **Every issue id is a chosen slug: `capcom create --id <slug>`.** capcom refuses a create without `--id`, so every id is one you pick: two to four kebab-case words naming the defect or the work, never the fix (`net-deny-egress-gap`) — `capcom show <slug>` first if it might collide. A bad id is renamed with `capcom mv <id> <new-id>`, which keeps the old id as an alias. Old beads ids (`apogee-qi3`, `apogee-t57.10`) are aliases too: an old id in a plan, a changelog or a commit is correct as written, not a stale reference to fix.
- **An issue a plan has designed names that plan, never a status** ([ADR 0102](.capcom/knowledge/decisions/adr-0102.md)): `capcom update <id> --plan '<plan basename>'`. `capcom ready --planned` lists designed issues (hand to `/implement-plan`), `capcom ready --unplanned` lists issues still needing design. Read ownership from a plan's item sections, never from a grep for ids.
- **Be context-greedy, and spend that context through sub-agents.** Never act on a guess where the answer is readable: before you design, edit or judge, read the glossary terms, the decision record, the plan, the handoff and the neighbouring code that bear on the change, and read enough of each file to see how the surrounding code already does it. Thin context is the failure mode here, not slow work. But the main agent's own window is the scarce resource, so buy that breadth with **sub-agents**: any task that is large, open-ended, or whose size you cannot predict — a repo-wide sweep, "find every call site", "does this pattern exist anywhere", a multi-file audit, a survey of docs before a design — is dispatched to a sub-agent that reads widely and returns only its conclusion. Default to delegating when in doubt about a task's size; a sub-agent that turns out to be overkill costs far less than a main context that fills up mid-run. Do the reading inline only when you already know the file and want one fact from it. Brief a sub-agent with the same discipline you would want yourself: name the paths and documents it must read, state the house facts it cannot derive (see *Stack facts* above), and ask for the finding, not a transcript.
- **North star (ADR 0031, tiebreaker force):** the embeddable engine must stay sufficient for any **Driver** (the TUI, `apogee daemon`, `apogee headless`, the bench, and any future one — `capcom know show "Driver"`). Its door-keeping invariants — wire-silent engine, wait-tolerant Approver, no first-party connectors, benchable-all-the-way-up — bind design work; a change that closes one of those doors must supersede ADR 0031 explicitly.

<!-- BEGIN CAPCOM -->
<!-- Managed by capcom: run "capcom agents" to refresh. Edits inside these markers are overwritten. -->
## Issues and knowledge: capcom

capcom is the issue register of record for this repository. Issues and Knowledge Notes are
markdown files under `.capcom/`, versioned with the code; track work there and nowhere else.

Without a `capcom` binary, install it from https://github.com/airiclenz/capcom#install.

- `capcom ready`: the issues ready to work on, most urgent first
- `capcom show <id>`: one issue in full, with what blocks it
- `capcom update <id> --status active`: claim it before you start
- `capcom log <id> <text>`: note progress as work lands
- `capcom close <id> --reason <text>`: close an issue; it names the issues this made ready
- `capcom reopen <id> --reason <text>`: undo a close
- `capcom create --id <slug> --title <title> [--dep <id>]`: file a new issue
- `capcom dep add <id> <dep-id>`: make an issue wait for another; `capcom dep list <id>` shows both sides
- `capcom list --tree --depth 2`: the outline of the issues
- `capcom know tree`: the outline of the Knowledge tree; `capcom know show <id>` reads a Note

Each command's flags are in `capcom <cmd> --help`.

An issue's body says what is wrong or wanted, why, and how to tell it is done (`## Acceptance`),
readable on its own; a plan says how and in what order.

Where things are: an issue is `.capcom/issues/<id>.md`, and its children sit in the directory
`.capcom/issues/<id>/` beside it; Notes follow the same shape under `.capcom/knowledge/`.
Prefer capcom commands; after a hand edit run `capcom check`.

A standing issue (`standing: true` in its file) only groups its children: never claim or close it.

Tree lock: before a change that spans several files or commits, it is recommended to claim this
worktree with `capcom lock acquire --purpose <text>` and keep the token it prints.
`capcom lock touch --token <token>` renews it during long work,
`capcom lock release --token <token>` frees it when done, and acquiring again with `--token <token>`
resumes it after a restart or compaction. When `capcom lock check --token <token>` exits 3, or
`capcom lock check` does while you hold no token, another session holds this worktree: ask the user
before editing it.

Knowledge tree, top level:

- `decisions`: apogee's decision records: one Note per record, with each of its sections as a child
- `glossary`: apogee's domain glossary: the terms to use in code, docs and messages, one Note per term
<!-- END CAPCOM -->
