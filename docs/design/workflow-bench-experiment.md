# The workflow bench experiment

**Date:** 2026-09-28 · **Status:** 📐 **Designed, not run** — the runs live in `apogee-sim` ·
**Owner ADRs:** [ADR 0087](../adr/0087-the-engine-runs-workflows-the-model-or-a-recipe-asks-for.md)
D8 (`fan_out` ships off; the flagship experiment) /
[ADR 0089](../adr/0089-a-workflow-may-run-in-the-background-and-wakes-the-agent-when-it-ends.md)
D1 (the background switch and `workflow` are shown only to model classes the bench has approved) /
[ADR 0009](../adr/0009-the-ab-decision-rule.md) (the decision rule) ·
**Realised by:** `docs/plans/archived/2026-09-27 - 00 - engine-run-workflows-plan.md` (the design; the
engine and facade it measures)

> **How to read this file.** It states *which* experiments gate turning the model's workflow tools
> on, *what* each one measures and *how* its result is read. It does not run anything, and it
> does not turn anything on: running a Campaign is `apogee-sim`'s job, and lifting a tool in a
> shipped model profile is a separate, evidence-cited change. Start at
> [What the evidence decides](#what-the-evidence-decides).

## Why

Three model-facing switches ship off, because of the Floor invariant: nothing apogee puts in front
of a model may make it perform worse than the bare loop.

- **`fan_out`** adds a whole new tool to every request. It is a cost a small model pays whether or
  not it calls the tool, and no earlier structural addition did that (ADR 0087 D8).
- **`fan_out`'s `background` field** and **the `workflow` tool** add a second new tool, a wider
  schema, and a new kind of reply — the **wake** — that opens on an engine note rather than on the
  user (ADR 0089 D1, D3).

Recipes are not gated. A human chooses to start one, whether or not `fan_out` is on (ADR 0087
D8). They are still the subject of the flagship experiment, because the premise of the whole
workflow engine is a claim about them: a small coordinator cannot hold a multi-stage plan, but the
engine can hold it on its behalf.

## What the evidence decides

| Switch | Ships | Lifted per model by | Gated by |
|---|---|---|---|
| `fan_out` | off | `tools.enabled:` or a model profile's `tools:` | [Experiment 2](#experiment-2--fan_out-on-vs-off) |
| `background` + `workflow` | off (and never offered without `fan_out`) | `workflow` on the same roster | [Experiment 3](#experiment-3--background-and-workflow) |
| the `audit` recipe | on (user-invoked) | — | none. [Experiment 1](#experiment-1--audit-three-ways) is the flagship read, not a gate |

The evidence is **per model**. A Campaign's verdict is stamped with that model's fingerprint and
does not transfer to another model. A superior verdict is what licenses a later change that lifts
the tool in that model's shipped profile roster. That change cites the Campaign bundle, and it is
out of scope here.

## Shared ground

Every experiment below is an `apogee-sim` **Campaign** under the **aggregate** Protocol, and it
inherits that instrument's whole statistical engine unchanged:

- **The unit of analysis is the task**, paired across arms. N is the number of distinct tasks,
  never tasks × runs (ADR 0009). Each task's per-arm score is the mean of its R reps.
- **The gate is non-inferiority**: one-sided, per contrast, uncorrected, α = 0.025, against the
  margin −δ. An inconclusive result **fails**.
- **The selection is superiority**: closed/hierarchical after the gate, with Benjamini–Hochberg
  FDR at q = 0.05 across the contrasts one Campaign pre-registers.
- **δ is measured**: the 95th percentile of the split-half A/A null over the reference arm's own
  reps (apogee-sim ADR 0012 §3).
- **Tasks are measured, never hand-picked.** Each Campaign reads its **Band** at the
  Discrimination checkpoint, at the pre-registered `SliceDepth` with the sampler recorded
  (apogee-sim ADR 0014 §2, ADR 0016). A Band below the power floor K kills the Campaign.
- **Engagement must be verified.** A run that executed no tools graded the seed, not the agent.
- **Production temperature** comes from the manifest; the sampler is recorded and never pinned.

The disposition is ADR 0009's table, read once per contrast:

| CI lower bound of the paired delta | Verdict | What happens to the switch on that model |
|---|---|---|
| `> 0` | superior | eligible to be lifted in that model's shipped profile |
| `−δ < lower ≤ 0` | non-inferior, benefit unproven | stays off by default; a user may lift it by hand |
| `≤ −δ`, or straddling `−δ` | gate fails or inconclusive | stays off; the Campaign is recorded as the reason |

**Arms differ by one bit.** In every contrast the two arms share the model, the server and its
Parallel-agents width, the step and time caps, the workflow config keys (`workflow-retries`,
`workflow-continuations`, `workflow-wake`) at their defaults, and the Reaction posture — apogee's
shipped posture, identical in both arms. What differs is only the thing under test: the launch
route in Experiment 1, and the tool roster in Experiments 2 and 3.

## The metrics

| Metric | Definition | Role |
|---|---|---|
| **Findings confirmed** (Experiment 1) | The fraction of a subject's seeded defects that the run's final report names, as the subject's author-owned matcher decides (defect id, file, line window). A run that writes no report scores 0. | primary ordinal: the ADR 0009 gate endpoint |
| **Acceptance pass fraction** (Experiments 2, 3) | The Task Pool's own primary outcome: author-owned acceptance checks passed / checks (apogee-sim ADR 0015). | primary ordinal: the gate endpoint |
| **Context tokens at the orchestrator** | The peak `PromptTokens` of any depth-0 `UsageEvent` in the run, i.e. the most context the top-level model had to read in one request. Also reported: the depth-0 total. Children's requests carry `Depth ≥ 1` and never count here. | pre-registered secondary, reported per task and per arm, never gate-participating |
| **Completion rate** | Experiment 1: the share of reps that end with the report written in the report format. Experiments 2, 3: the share that end on a final reply, not a step cap, a stall or a provider error. | pre-registered secondary; available as ADR 0009's intersection-union tightening (no α cost) if a primary result reshuffles completion |
| Findings reported but unmatched (Experiment 1) | The number of report findings that name no seeded defect. | descriptive precision guard. A drop in confirmed findings is never offset by it |
| Wall time, total tokens across depths, tool executions | Per arm, per task. | descriptive only (apogee-sim ADR 0015 §3) |

Context tokens and completion rate stay secondary. The reason is apogee-sim ADR 0015 §3: the
primary endpoint stays pure correctness. The owner's standing claim — that sub-agents exist for
context management — is exactly what the depth-0 figure shows. It is reported loudly beside every
verdict, but it cannot rescue a gate that correctness fails.

## Experiment 1 — audit three ways

**The question:** on a small model, does the engine-run `audit` recipe find at least as many real
defects as the same audit orchestrated in prose, and does it get there without filling the
orchestrator's context?

**The arms.** All three run the same audit, with the same lenses and the same verification, on
the same scope, with focus fixed to `all` wherever an arm takes one, so that no arm stops on a
question:

| Arm | Launch | Who holds the plan |
|---|---|---|
| **prose orchestrator** | the owner's `code-audit` skill attached, scope and focus in the message | the top-level model, dispatching `sub_agent` batches in waves under the Fan-out ceiling |
| **prose-sequential** | the owner's `code-audit-sequential` skill attached, the same scope (it takes no focus) | the top-level model, one `sub_agent` at a time, no verification phase, no rollup |
| **recipe** (candidate) | `Agent.StartRecipe{SkillID: "audit", Text: "<scope> all"}` | the engine; the model reads one line per item and a report path |

Neither prose skill ships with apogee. The bench vendors a pinned copy of each and records its
content hash in the manifest.

**Prompt parity is a precondition.** The `audit` recipe's prompts were derived from
`code-audit`'s. If the two copies drift in anything but dispatch mechanics, the comparison measures
the drift and not the orchestration. Before the Campaign is pre-registered, the vendored
`code-audit` prompts and `internal/skills/shipped/audit/prompts/` are diffed. Every difference
that is not dispatch mechanics is reconciled, or else named in the pre-registration.

**The subjects.** An audit subject is a frozen codebase snapshot plus a hidden, author-owned list
of seeded defects and a matcher for each one. The shape follows apogee-sim ADR 0015 §1–2: every
seeded defect is one a careful reviewer would report, traceable to the code, never a trick. The
subjects form their own frozen, versioned pool (an audit pool), because a Task Pool task is a
coding job with an acceptance suite, not a review target. The pool spans the class by size (from a
single package up to a scope `split.sh` cuts into several parts) and by defect kind (a correctness
bug, a security hole, a concurrency defect, a missing test on a critical path, drift from the
project's own rules). The Band is read over it as for any Campaign.

**The contrasts**, both pre-registered, with the recipe as the candidate:

1. recipe vs prose orchestrator;
2. recipe vs prose-sequential.

Each contrast is gated on its own. Superiority is FDR-controlled across the pair.

**What it decides.** Recipes carry no switch, so no verdict here turns anything on. What it
settles:

- **A gate failure** on either contrast is a defect in the recipe or the engine. It is fixed and
  re-measured before Experiment 2 spends model-hours on that model. With `fan_out` on, the model
  can start `audit` itself (`fan_out{recipe, inputs}`), and a recipe that loses to prose is not a
  route worth offering.
- **Superiority over prose-sequential** is the direct test of ADR 0087's premise: the engine holds
  the plan that the small coordinator dropped.
- The depth-0 context figure is the flagship number the design was built to move. It is reported
  per arm, whatever the verdict.

## Experiment 2 — `fan_out` on vs off

**The question:** does putting `fan_out` on a model's menu leave its ordinary work no worse — and
does the model do better with it?

**The arms:** off (the reference: the shipped roster, `sub_agent` present) and on (the candidate:
the same roster plus `fan_out` through `Config.EnabledTools`; `workflow` absent, so `fan_out` is
blocking-only). The Driver is the Campaign's ordinary in-process loop. Nothing else changes.

**The tasks:** the frozen Task Pool, read to the model's Band exactly as for any Campaign. It is
never a set picked because it suits fan-outs. Per-arm task picking is the bench-overfitting ADR
0009 forbids. The gate's question needs the ordinary Pool in any case: the tool is paid for on
every request, including every request that has nothing to fan out over.

**Power for the selection read.** If the Pool holds too few list-shaped tasks (work over many
files or many independent items), the verdict can at best be non-inferior, and `fan_out` stays
available but off. The honest remedy is a versioned Pool revision that adds such tasks. That
revision is class-wide, agnostic to what a Campaign arms, and pre-registered before this Campaign.
It is never a subset chosen for this contrast.

**The firing subpopulation**, the Band tasks on which the on-arm called `fan_out` at all, is
reported descriptively beside the verdict. It shows where the tool was used. It is not a second
gate, because the tool's cost lands on the whole Pool (unlike an off-ramp's, ADR 0009).

## Experiment 3 — background and `workflow`

**The question:** for a model that `fan_out` already helps, does adding the background switch and
the `workflow` tool leave its work no worse — and does it finish more of it?

**It runs only on a model whose Experiment 2 verdict was superior.** Background is offered only
beside `fan_out` (ADR 0089 D1). A model that should not see `fan_out` has nothing to background.

**The arms:** `fan_out` alone (the reference: Experiment 2's candidate roster) and `fan_out` +
`workflow` (the candidate: the roster that publishes `fan_out`'s `background` field and the
`workflow` tool).

**The Driver** must have a conversation to go on, as ADR 0089 D1 requires. It is the in-process
equivalent of the TUI's wake loop:

- between Steps of an open Exchange, it commits any `TakeWorkflowNotes` note through `Interject`;
- when the Exchange ends with a background workflow still running, it waits for the finish, then
  calls `Wake`;
- the run ends when the agent is idle with no workflow running.

**The tasks:** the same Band as the model's Experiment 2. The contrast is gated and selected like
Experiment 2's. Wall time is reported beside it, never counted in it.

## Order, and when to stop

For each model:

1. Experiment 1.
2. Experiment 2, unless Experiment 1 gated a defect that is still unfixed.
3. Experiment 3, only after a superior Experiment 2.

A killed Campaign (a Band below K, or engagement not verified) yields no verdict at all. The
switch stays as it was.

## Instrument prerequisites

These must exist before the Campaigns can be pre-registered. None of it is design work this
document leaves open.

**In apogee:** nothing outstanding. An embedder outside this module opts in to the background
switch with `Config.OffersBackground` (`internal/agent/construct.go`, `defaultRoster`), so
Experiment 3's Driver can publish `background` on `fan_out`. Headless and daemon keep it off
(ADR 0089 D1).

**In apogee-sim:**

- **A roster arm axis.** A Campaign arm today is the set of Reactions it arms. Experiments 2 and 3
  need an arm that differs only in `Config.EnabledTools`. Experiment 1 needs an arm that differs
  only in its launch route: an attached skill or `StartRecipe`.
- **The audit pool and its matcher**, frozen and versioned, as described in Experiment 1.
- **The wake-capable Driver loop** for Experiment 3.
- **The depth-0 usage read**, taken from the `UsageEvent` stream's `Depth`, recorded per run.

## Out of scope

- Running any of this. The Campaigns, their bundles and their verdicts live in `apogee-sim`.
- Lifting `fan_out` or `workflow` in any shipped model profile. That is a later change citing a
  verdict from here.
- Token-level constrained receipts (llama.cpp grammar, Anthropic forced tool choice). These stay a
  bench option under ADR 0087 D3, to be designed if receipt refusals show up as a completion-rate
  cost.
