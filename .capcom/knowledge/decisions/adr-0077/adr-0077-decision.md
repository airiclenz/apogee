---
title: ADR 0077 decision
---

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
