---
title: Receipt
---

**Receipt**:
The short, fixed-shape note a **Workflow** item's child hands back in place of its full report —
a status (`ok | partial | blocked`), a one-line summary, and the typed fields the workflow asked
for (a count, a verdict) — delivered by calling the `finish` tool only workflow children carry.
The engine checks it on the spot and bounces a malformed one back to the child to fix; routing
between stages reads receipts alone, never the detail files. Ratified 2026-09-27
(ADR 0087 D3).
A background `sub_agent`'s one item is the exception: its child is a `sub_agent` child that writes a
prose report and never carries `finish`, and the engine builds that item's Receipt itself
(ADR 0094 D2, 2026-10-05).
_Avoid_: "result" (a `sub_agent` call's full prose report), "summary" (one field of the receipt),
"report" (the detail file, or the workflow's final merge).
