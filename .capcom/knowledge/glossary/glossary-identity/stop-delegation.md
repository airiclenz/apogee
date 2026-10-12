---
title: Stop (a delegation)
---

**Stop (a delegation)**:
The human ending **one** running **Sub-agent** — and everything under it — while the parent's Turn
goes on: nothing rolls back, and the parent receives a partial result with the engine's summary of
the work so far. The engine call is `Agent.StopChild(runID)`, addressed by the run id as an
**Interjection** to a child is; the TUI binds it to `^x`, on the viewed run of a **Run view** or on
the cursor's member row of the `✦ Sub-Agent (N)` umbrella, with no confirmation since nothing rolls
back. The engine folds the child at the stop — no wrap-up Turn — and the non-error result opens
`[stopped by the user — engine summary follows]`; a pooled child stopped before it started runs
and folds nothing, and returns `sub-agent not started: the user stopped it before it started;
delegate again if the task is still needed`. A stopped run is a **Retained delegation** under its
name. Its delegate-ledger outcome is `stopped`. **Cancel** (`esc×2`) stops every running child
the same way and settles the whole Turn instead of letting it go on
(ADR 0088 D2); **capped** is the engine ending a run at a
bound. Ratified 2026-09-25 (ADR 0086).
_Avoid_: "stopped" for a run the engine capped (say **capped**), "kill", "abort".
