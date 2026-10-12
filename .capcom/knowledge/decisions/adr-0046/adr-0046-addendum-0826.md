---
title: "ADR 0046 addendum of 2026-08-26: on a CHILD, a capped reply with no tool call is a fault, text or not"
---

Decision 4's "no visible text" qualifier assumed a human reader who can see a cut answer and ask for
the rest. A delegate has no such reader: its reply becomes one tool result for a parent MODEL. On
2026-08-25 a capped delegate reply flowed back to a coordinator as a 223K-character "finding" that
stopped mid-sentence and was acted on as complete. So at depth > 0 a `finish: length` reply carrying
no tool call faults whatever text it holds, and the parent's error result names that cause; depth 0
and any capped reply that still asked for a tool are unchanged.
