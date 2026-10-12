---
title: "Resolution: the single complete verdict for one tool call, computed before it runs"
---

**Resolution**:
The single, complete verdict for one tool call, computed in full before anything executes —
covering *every* rule that decides the call's fate: the tighten-only guardrail floor, the
autonomy-ladder × blast-radius table, confinement capabilities, and the contingencies for what
can only be discovered at run time. Dispatch *executes* a Resolution; it never decides.
Subsumes the Phase-3 term **"per-call disposition"**, which named only the ladder-table stage
that runs *after* the guard clears (ADR 0012/0013 and the confinement-execution contract use
"disposition" in that narrower sense).
_Avoid_: "disposition" in new code and docs (retired — it under-claimed what the verdict
covers); "policy decision" (vague).
