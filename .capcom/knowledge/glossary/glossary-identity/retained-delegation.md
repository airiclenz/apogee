---
title: Retained delegation
---

**Retained delegation**:
A finished **Sub-agent** run the parent can pick up again with `sub_agent`'s `continue: "<name>"`,
kept under its **Delegation name** for the rest of the **Session** — saved with it, restored on
resume, dropped by `/clear` and cut by a fork; a cancel keeps it — what a finished or stopped child
left stands (ADR 0088 D2). A run
completed normally is retained only when its call **named** it; a capped, faulted or **stopped**
one is retained under any name it ended wearing. A continuation re-spawns a fresh child from the
original task and the run's rounds of reports, never resuming the old one. Ratified 2026-09-25
(ADR 0086).
_Avoid_: "suspended sub-agent" (ADR 0007's reserved slot, which stays empty), "resumed child".
