---
title: Step
---

**Step**:
The bench/embedder primitive that advances the loop **one Turn** and returns at a
**quiescent boundary** — no in-flight stream or tool call, conversation state fully
serializable. Approval and streaming happen *inside* a Step; **snapshot, resume, and the
bench's fork are valid only at the quiescent boundary**. Cancellation is delivered through
Step and takes effect cleanly at that boundary. Sub-agent stepping is **top-level-only for
v1** (designed swappable for nested stepping later). See
ADR 0007.
_Avoid_: "tick", "cycle" (Turn is the loop iteration; Step is the externally-driven advance
of one Turn).
