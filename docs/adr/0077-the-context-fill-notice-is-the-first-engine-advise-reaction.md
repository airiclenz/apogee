---
Status: accepted
---

# The context-fill notice is the first engine advise Reaction: a top-level boolean, scaled to the compaction line

## Context

A model running in apogee learns nothing numeric about its context window. The host knows the fill
exactly — prompt tokens from every upstream response, the window from llama.cpp `/props` or the
`context-window:` pin — and shows it to the human in three places (the TUI gauge, the sub-agent run
heads, `/usage`), but the only model-facing signals are after the fact: a truncation marker, a prune
stub, the post-compaction bridge, and the step-cap wrap-up directive, which counts steps rather than
tokens. A context-limited model, and a delegate above all, cannot therefore stop in time and report
what is left to its parent; it works until the fold takes the conversation from under it.

[ADR 0076](0076-one-reaction-core-with-an-origin-by-class-policy-matrix.md) D2 gives the engine row
an **advise** cell — text the model sees, fenced, capped, fail-open, landed as a trailer on the
closing tool result (D6) — but nothing has yet taken it: the class constant and the Bypass skip
exist, the slot, the provenance-derived fence, the ledger and the resume strip do not. Stage 3
(`apogee-rxj`) owns the *user* advise cell and waits on the same slot.

Two facts fixed the design. First, the fold does not wait for the window: the Budget holds 20% back
for the reply and gives History 60% of what remains, so the automatic Compaction line sits at roughly
**48% of the working ceiling** (`internal/context/budget.go`, `internal/domain/budget.go`). A child
agent folds at the first Turn boundary past it; the main agent defers to the next Exchange opening
and can climb past it mid-Exchange. A notice in "% of window" would put every rung above 50 out of a
child's reach. Second, the hard invariant (`AGENTS.md`): a model-facing behaviour above the Floor
ships **off** until bench evidence turns it on — which decides the default, and nothing else.

## Decision

1. **Cell.** `context-fill-notice` is an **engine**-origin **advise** Reaction on `post-tool-result`,
   inherited by every child agent (`TopLevelOnly` false), switched off by **Bypass** like every
   advise reaction, and **off by default**. It is not a Floor guard: a guard changes what the model
   sees after its own failure or shapes the request without steering it, and this notice exists to
   steer. Flipping the default is a one-line change once a bench arm shows it beats the bare loop.

2. **Key.** One top-level, file-only boolean, `context-fill-notice: false`, beside the seven Floor
   keys and live-editable in `/settings` like them. This **amends ADR 0076 D10** ("builtins appear by
   id with `enabled:`"): an engine builtin whose whole configuration is on/off may take a top-level
   boolean, on the condition that its manual entry says it is **not** a Floor guard. D10's form has
   no live editing today and no builtin has exercised it; a one-line switch is what the owner asked
   for and what a user will find. The seven Floor booleans stay the only top-level keys that are
   *on* by default, which keeps ADR 0076 D11's reading — top-level boolean, on everywhere — true of
   every Floor guard while no longer being the definition of one.

3. **Scale.** The percentage measures the **compaction line** — the Budget's History allocation,
   through the same `HistoryExceedsFraction` compare the automatic trigger reads — never the
   advertised window or the working ceiling. Beside it the notice prints the tokens used and the
   window, so a user who would rather steer by an absolute count ("stop above 24k") can, without
   knowing where the line sits. When no window is known the Budget carries a zero allocation and the
   notice is **silent** — the standing posture, never fire on a guess.

4. **Rungs.** Fixed at **50, 75 and 90** percent of the line, each fired **once per climb**. A fold
   ends the climb and re-arms the whole ladder — the first post-fold result fires whichever rung its
   fill reaches, as a fresh session's first result would, so no rung is ever marked fired without
   its notice (amended 2026-09-13: the first cut re-armed only the rungs the fill fell under, and a
   post-fold result landing at 50–74 kept the 50 rung "fired", silent until 75). The 50 and 75
   rungs carry the fact line only.
   The 90 rung adds, for a **child agent** alone, one engine-authored sentence: stop, and report what
   remains to the parent. The main agent's 90 rung stays a fact: its fold waits for the Exchange
   boundary and its wrap-up is the human's call.

5. **Slot.** The notice builds ADR 0076 D6's advise slot rather than appending through
   `SetContent`: an engine-owned append on the tool-result edit that fences the span from its
   provenance `{reaction, origin, moment, turn}`, records it on the ledger, and is **stripped on
   resume** so a replayed session never re-reads "90%, wrap up" as current. Stage 3 hands the same
   slot to user advise entries unchanged.

## Rejected

- **A Floor guard, on everywhere.** Cheaper to ship, but it would put steering text under the floor
  the hard invariant holds and break the guard's definition.
- **A sixth orientation bullet.** The orientation block sits in the standing system message; a
  changing number there invalidates the upstream prefix cache every Turn.
- **A user hook only.** The user advise cell is stage 3 and needs the slot anyway; a shell script
  holding the ladder gives the bench nothing to attribute.
- **A configurable ladder** (`true | false | [rungs]`). Six or more spans per climb is real noise on
  a small model, and the key stops being a switch.
- **Percent of the window.** Matches the human's gauge, but the model would be folded at ~48% with
  no rung reached; naming the line beside it means two numbers per notice.

## Consequences

- ADR 0076 D10 is amended as decision 2 states; D6's slot exists after this ships, and stage 3
  (`apogee-rxj`) depends on it.
- The bench arm that could flip the default on is apogee-sim work and is not part of this decision.
- The notice is measured where the fold is measured, so the two can drift only if the Budget does.

## Addendum (2026-09-15): the step-budget notice is the second engine advise Reaction

The session-mining review of 2026-09-14 found delegates discovering their step cap only on the
tool-less wrap-up Turn, with output they could no longer write. `step-budget-notice` is the second
engine-origin **advise** Reaction, on this decision's plumbing byte for byte: engine origin, class
advise, on `post-tool-result`, its own top-level file-only boolean (`step-budget-notice: false`,
the decision 2 amendment's form, live in `/settings`, refused as a `reactions:` id), off by default
under the hard invariant, switched off by Bypass, inherited by every child, its firings booked under
the `notice` action (`Detail: "step 60 of 80"`), its text landed as the decision 5 trailer and
stripped on resume. It fires for a **child agent alone** — only a delegate carries a step cap — once
per Exchange, on the tool result that closes the Turn reaching **ceil(0.75 × cap)**, with a fixed
threshold for decision 4's reason: `steps: N of M used — K left before the wrap-up Turn; write your
output now`. It is silent at depth 0 and for an unbounded delegation. Plan `2026-09-14 - 03` item 7.

## Addendum (2026-09-19): the step-budget notice leaves the Reaction ladder — superseding the 2026-09-15 addendum

The 2026-09-15 addendum is superseded. The step-budget notice is **not** an engine advise Reaction
and has no switch: it is a **structural floor** of every bounded delegation, fired by the engine
at depth ≥ 1 — on everywhere, Bypass included — as an engine note on the closing tool result
(`[engine — step budget]` … `[end engine — step budget]`, the wrap-up directive's fence, never
the advice fence). The `step-budget-notice` key, its `/settings` row, the `Options`, `Config` and
`Generation` fields and the reserved Reaction id are removed; a home config still carrying the key
is exempted from the unknown-key walk, read-only — nothing is stripped or rewritten. The notice
books no firing, and a compaction fold re-arms it as it re-arms the fill notice. Evidence: the
2026-09-18 capped-delegate handoff §3 F3 — a delegate read the notice's advice fence on a
`read_file` result as part of the file it read, so the fence header must say *engine*, not
*reaction* — and the 2026-09-14 session-mining review's headline 3: announcing the cap is part of
the bound's contract, which a model-shaping switch cannot be allowed to withhold. The
`context-fill-notice` Reaction and this decision's other parts stand untouched. Plan `2026-09-18 -
00` items 6 and 7.

## Addendum (2026-09-19): the token-budget notice is the second structural bound notice — the ladder is unchanged

Defect `apogee-zuu` (session `20260918T143011Z`): a delegate at 10.7M cumulative prompt tokens
under the default 20M `delegate-max-tokens` budget heard nothing, because the context-fill ladder
measures **one request's fill against the working window** — a 1.3M window kept every rung silent
— and the step-budget notice counts Turns. The two bounds are independent, so the step notice gains
a **twin**: the token-budget notice, a second structural engine note fired at depth ≥ 1 on the tool
result that closes the Turn at which the child's cumulative prompt tokens (its own usage tally, the
figure the bound is enforced against) reach **ceil(0.75 × delegate-max-tokens)** — 15M at the
default — fenced `[engine — token budget]` … `[end engine — token budget]`:
`tokens: 15.2M of 20.0M spent — 4.8M left before the wrap-up Turn; write your output now`. It
rides the same latch and re-arm seams as the step notice (once while its copy survives; a fold, a
prune stub or the rollback of the Turn it rode re-arms it), lands beside it in `appendToolResult`
so the fence order on a closing result is fixed (tool output, advice, step note, token note, the
wrap-up directive last), is silent at depth 0 and for an unbounded budget, books no firing and
stays on under Bypass. It is **not a rung of this decision's ladder** — the ladder, its three
rungs, its switch and the `context-fill-notice` Reaction stand untouched — and the default budget
stays 20M: the per-call lever for a child whose single requests run large is `working-window:`.
The token renderer shared with the fill notice gains an M tier, so a fill notice over a window of
a million tokens or more now reads `1.3M` where it read `1300k`. Plan `2026-09-19 - 01` item 3.
