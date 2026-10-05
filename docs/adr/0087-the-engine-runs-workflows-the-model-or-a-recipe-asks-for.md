---
Status: accepted
Amends: ADR 0039 (the 2026-09-20 fan-out ceiling now binds `sub_agent` alone), ADR 0022 D8 (a workflow item's conversation is kept in its workflow folder), ADR 0034 (`run: workflow:` arrives), ADR 0031 invariant 4 (the thin workflow layer is built)
---

# The engine runs workflows the model or a recipe asks for

## Context

The handoff `docs/handoffs/2026-09-26 - 00 - engine-run-sub-agent-orchestration-planning.md`
(bead `apogee-engine-run-delegation`) sketched engine-run orchestration, and the owner's grilling
session of 2026-09-27 settled it.

Today a parent delegates with one `sub_agent` call per helper, reads every helper's full report
into its own context, and holds the multi-stage plan itself. The owner's skills (`code-audit`,
`release-notes`, `security-audit`, `implement-plan`) are hand-written orchestrations in exactly that
shape: a context-greedy coordinator dispatches "read this prompt" briefs, routes on six-line
receipts, never opens the output files, retries once, and resumes by skipping phases whose output
exists. Two costs follow. The coordinator's window fills with reports. And a small model cannot hold
the plan: the `-sequential` skill variants exist because a 4B–35B coordinator stalls between phases,
so they drop adversarial verification, rollups and machine checks outright.

The owner's standing direction: sub-agents exist for **context management, not speed**, for local
and frontier models alike, and old decisions are retired when they block the best design.

ADR 0031 invariant 4 already asks for a "thin workflow layer" as an embeddable library the bench can
drive, and ADR 0034 reserved `run: workflow:` for it. No record rejected the idea.

## Decision

**The engine runs a Workflow — stages of items, each item done by a fresh child that hands back a
Receipt — and the parent reads one line per item plus a report path.** A workflow has exactly two
sources, and in both the work is *asked for*: the top-level model's `fan_out` call, or a Recipe a
human wrote.

> **Amended 2026-10-05 ([ADR 0094](0094-sub-agent-may-run-as-a-one-item-background-workflow.md)).** A workflow now has three sources, and in all three the work
> is still asked for: a `fan_out` call, a Recipe, and a `sub_agent` call carrying `background: true`,
> which runs as a one-item Background workflow. That item's child is a `sub_agent` child: it hands
> back a prose report and never calls `finish`, and the engine records the item's Receipt itself.
> "Each item done by a fresh child that hands back a Receipt" holds for `fan_out` and Recipe items.
> D10's entry points gain that `sub_agent` call as a fifth.

**D1 — The model describes one fan-out; recipes describe everything bigger.** A `fan_out` call
carries a brief template (with `{item}` / `{out}` placeholders), the list to fan out over, the
receipt fields it wants back, a per-item output path, shared context files, an optional tool
narrowing, and optionally one `verify` stage (an adversarial check of the items a condition on
receipt fields selects) and one `merge` stage (a single agent over the outputs). Retries, waves,
width and continuation are configuration, never parameters. Anything longer — chains, branches,
questions to the user, bounded repeats — exists only in a **Recipe**. Rejected: letting the model
compose arbitrary stage chains. That is exactly where a small model fills a plan in badly, and a bad
plan is worse than one plain call.

**D2 — `fan_out` is a new tool beside `sub_agent`, and `sub_agent` is unchanged.** The two differ
in result shape (one prose report against one line per item plus a report path), and "one helper"
against "many helpers over a list" is a verb choice a small model gets right. Keeping `sub_agent`
byte-identical keeps today's behaviour and gives the bench a clean arm. `fan_out` also starts a
named Recipe: `fan_out{recipe, inputs}`, where the model fills in only the recipe's declared inputs (for `audit`: scope and focus).

> **Amended 2026-10-05 ([ADR 0094](0094-sub-agent-may-run-as-a-one-item-background-workflow.md) D1).** `sub_agent` stays byte-identical wherever ADR 0089's
> background gate is closed. Where it is open, `sub_agent` gains one optional `background` field;
> its blocking result shape is unchanged.

**D3 — A receipt is a `finish` tool call the engine checks on the spot.** Only workflow children
carry `finish`. Its schema is a fixed core — `status: ok | partial | blocked` and a one-line
`summary` — plus the typed fields the workflow asked for (an integer count, an enum verdict). A
malformed call is refused with a specific, fixable error, and the child corrects it in its own loop
without a restart. A child at its step cap gets one closing Turn in which `finish` is the only tool.
This works on both wires today without a new carrier. Rejected for the first cut: parsing
`STATUS:` text lines, and token-level constrained decoding (llama.cpp grammar, Anthropic forced
tool choice). The second stays open as a bench option.

**D4 — A workflow lives in a folder under the session's Scratch dir.** That folder is
`<scratch>/workflows/<id>/`. It holds the plan, one folder per item (detail output, receipt, and
the child's conversation), and the final report. The Confinement box already allows writes there in
every Mode, Plan included, so a read-only audit works in Plan mode with no new fence rule. An item is
keyed by a hash of its brief, item and context-file contents, so re-issuing the same workflow skips
finished items. An item unfinished when the workflow stopped restarts fresh; it is never resumed
mid-conversation, and ADR 0007's suspended-sub-agent slot stays empty. Reuse across sessions
(incremental re-runs a week later) is deferred: it needs a per-workspace folder and a fence rule of
its own.

> **Amended 2026-09-27 ([ADR 0012](0012-confinement-attaches-to-blast-radius-and-confine-to-workspace-flag.md) amendment 2026-09-27; D6).** "No new fence rule" held for the folder's writes
> but not for a Recipe's `script` stage, a `terminal` call Plan refused like any subprocess — so
> the shipped `audit`'s `split` could not run in Plan, the default of headless runs and daemon
> Firings. One rule is added, for that engine-built call only: in Plan it runs inside the
> Confinement box with its own workflow folder as the only writable root (a write anywhere else,
> the workspace included, fails in the sandbox), and with no confinement backend it is refused.
> A model's own `terminal` call in Plan stays refused, and the other modes run script stages as
> before.

> **Amended 2026-09-30.** Re-issuing the same workflow skips its finished items only when no
> background run (ADR 0089) is still driving its folder. Every launch now goes through one launch
> builder, and a blocking one — a `fan_out` plan, a `fan_out` recipe form, a typed `/<id>` or a
> foreground recipe launch, from the top-level agent, a sub-agent or a background item child alike
> — that resolves to a folder a background run still drives is refused with
> `apogee: workflow <id> is already running in the background`: nothing is created, spawned or
> written there, so two runs never share one folder's status file. Once the background run ends,
> the same re-issue resumes the folder as before.

**D5 — ADR 0022 D8 is amended for workflow items.** A workflow item's conversation is saved in its
workflow folder, for `/workflows` inspection. It is still never a Session record, and a plain
`sub_agent` child stays ephemeral.

**D6 — Recipes are declarative stage lists in a Skill's header.** A skill may carry a recipe block:
named stages of seven kinds — `fanout`, `merge`, `pick` (a stage's output or a receipt field becomes
the next stage's items), `verify`, `script`, `ask` (a question to the user) and `repeat` (bounded) —
with `when:` conditions on receipt fields between them. Anything computed is a `script` stage,
never a language feature; the owner's skills already split the work this way (`split.sh`). Script
stages obey the Mode and approval rules of the agent's own shell tool — except in Plan, where they
run confined to their workflow folder rather than being refused (amended 2026-09-27, see D4). The user starts a recipe by
invoking its skill, whether or not `fan_out` is enabled. Rejected: recipes as programs in an embedded
scripting language (the Claude Code workflow shape). That means a new runtime and safety surface, and
plans that cannot be checked before they run. apogee ships a built-in recipe, `audit` (a user's own `code-audit` skill keeps its name), as the
bench subject and the reference example.

**D7 — The fan-out ceiling binds `sub_agent` alone.** A `fan_out` runs its items in waves of the
Parallel-agents width and has no ceiling: receipts are one line each, so the ceiling's premise (a
coordinator committing to more reports than it can read) does not hold for it. `sub_agent` keeps the
ceiling, because its children still return full reports. When `fan_out` is enabled, the ceiling's
refusal names it.

**D8 — `fan_out` ships off; the bench switches it on per model class.** The Floor invariant decides
this. The tool only executes what the model asked for, but it adds a whole new tool to every
request, which no earlier structural addition did (ADR 0069 and ADR 0086 changed the shape of an
existing tool). It is off the menu by default and the owner can switch it on by hand. The flagship
experiment runs the audit on a small model three ways: prose orchestrator, prose-sequential, and
recipe. A second experiment runs a task set with `fan_out` on and off. Recipes are not gated: the
user invokes them.

**D9 — Workflow children do not delegate.** `delegate-max-depth` stays 1 (ADR 0013 §4 as amended
2026-09-15). `verify` and `merge` are sibling stages the engine runs, never grandchildren. A
child's privileges stay bounded by the parent's (ADR 0005). A stage may run on the other Delegation
seat under the existing `sub-agents-choice` gate (ADR 0069).

> **Amended 2026-10-05 ([ADR 0094](0094-sub-agent-may-run-as-a-one-item-background-workflow.md) D2).** This binds `fan_out` and Recipe items. A background
> `sub_agent`'s child keeps `sub_agent`'s own depth rule, so it may delegate wherever a blocking
> child may.

**D10 — The workflow engine is a Driver-free library.** It lives in its own engine package, is
re-exported through the root facade for the bench, and is started by four entry points: the
`fan_out` call, a recipe skill, the daemon's `run: workflow: {recipe, inputs}` (ADR 0034's reserved
key), and headless `--recipe`. In a Driver with no human, an `ask` stage takes its declared default
and the report says so.

**This is not Guided decomposition.** ADR 0071 retired a mechanism that told the model how to plan
its own work and then dispatched delegations it never asked for. Here the engine plans nothing. It
executes a fan-out the model asked for in one call, or a recipe a human wrote and a human (or, with
`fan_out` enabled, the model) chose to start. ADR 0014's single-spawn-path rule still binds: every
workflow child is spawned through the recursion point, and no history is fabricated. The parent's
conversation holds its own `fan_out` call and the engine's answer to it.

## Considered options

- **Extend `sub_agent` with optional `over` / `returns` / `then` fields.** One tool, but two result
  shapes under one name, and a small model sees ten optional fields it may fill wrongly. Rejected
  for D2.
- **A per-workspace workflow folder** (`~/.apogee/runs/<workspace>/`). It survives sessions and
  enables incremental re-runs, at the cost of a new writable location and a pruning policy. Deferred;
  D4 can move later without touching the rest.
- **Retiring the fan-out ceiling outright.** Plain helpers would queue in waves too, and an eager
  model could again flood its context with dozens of full reports. Rejected for D7.

## Consequences

- New CONTEXT.md entries: **Workflow**, **Recipe**, **Receipt** (and **Background workflow**, ADR
  0089). **Fan-out ceiling** now names `sub_agent`.
- The tool roster grows by `fan_out` (off by default), `finish` (workflow children only), and
  `workflow` (ADR 0089). `internal/tools/registry.go`'s pinned counts and the README move with them.
- `/implement-plan`'s per-item implement → verify → commit loop is not a recipe. It stays the
  model's own work.
- ADR 0088 settles what `esc` does to a workflow and to plain helpers. ADR 0089 settles background
  workflows.
