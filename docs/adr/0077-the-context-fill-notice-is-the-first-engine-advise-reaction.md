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

4. **Rungs.** Fixed at **50, 75 and 90** percent of the line, each fired **once per climb** and
   re-armed when a fold drops the fill back under it. The 50 and 75 rungs carry the fact line only.
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
