---
title: "Tool-result capping: the Floor guard truncating any tool result over its share of the Budget"
---

**Tool-result capping**:
Per-tool-result truncation of any single result that exceeds its fraction of the Budget,
with head/tail preservation, protecting the most recent Turn. A **Floor guard**
(`tool-result-cap`) since ADR 0071,
on in every arm and applied to the **request projection** — the committed conversation is never
rewritten — implemented **once** (the surviving half of the predecessor's `compress`). Beneath it
sits a **structural floor** — not even a guard, so no key turns it off: a single result
whose estimate exceeds the *entire* History allocation is clamped as it enters the
conversation, because a result that large survives no reducer and would overflow even the
emergency fold that exists to rescue the Turn. Both render the same head/tail-plus-marker
elision (`context.TruncateToolResult`), so the model reads one idiom; the guard's tighter
cap fires first unless `tool-result-cap: false` stands it down.
_Avoid_: "compression", "compaction" (capping is per-result and non-generative), "Mechanism"
(the cap was a catalogued row until ADR 0071; it is engine behaviour now).
