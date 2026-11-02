---
title: "Tool-result capping: the Floor guard truncating any tool result over its share of the Budget"
---

**Tool-result capping**:
Per-tool-result truncation of any single result that exceeds its fraction of the Budget,
with head/tail preservation, protecting the most recent Turn. A **Floor guard**
(`tool-result-cap`) since ADR 0071,
on in every arm and applied to the **request projection** — the committed conversation is never
rewritten — implemented **once** (the surviving half of the predecessor's `compress`). Beneath it
sits a **structural floor** — not even a guard, so no key turns it off: a fresh result
is clamped as it enters the conversation to its share of the **room left in the request** —
the advertised window less the reply reserve less the estimate of the next request without
it, divided across the Turn's results still to append — never above the *entire* History
allocation and never below 1/8 of it (with no window known, the History allocation), because
a result that does not fit the next request would overflow it and even the emergency fold
that exists to rescue the Turn (ADR 0018 `adr-0018-amend-1102`). Both render the same
head/tail-plus-marker elision (`context.TruncateToolResult`), so the model reads one idiom;
a body the line elision cannot bring under the floor's bound — a few very long lines — is
cut by bytes instead (`context.ElideToBytes`, the same marker). The guard's tighter
cap fires first on older results unless `tool-result-cap: false` stands it down.
_Avoid_: "compression", "compaction" (capping is per-result and non-generative), "Mechanism"
(the cap was a catalogued row until ADR 0071; it is engine behaviour now).
