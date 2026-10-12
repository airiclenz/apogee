---
title: ADR 0039 consequences
---

- **ADR 0013 §5 and ADR 0014 §3 are amended** (dated 2026-08-07): per-child atomicity and
  cancel-rolls-back-the-Turn survive verbatim; "the driver runs the nested Agent in one
  shot" becomes per-child, not per-Turn; "one delegation per Turn" becomes "one batch of up
  to the cap per Turn". ADR 0014's rejection of *unbounded* all-N-in-one-Turn stands — a
  batch is bounded and keeps the quiescent boundary between batches.
- **The changed guided-decomposition stack must re-pass the ADR 0009 gate** before it can
  default on; until then it ships default-off as ever. Cancel granularity coarsens from one
  child to one batch — recorded, accepted.
- **`serverEntry` gains `parallel-agents`** (per-entry, like the per-server `llama-launcher`
  key of the 2026-08-07 plan); validation refuses negative values, and `0` reads as unset
  (the `context-window` idiom — yaml cannot distinguish an explicit `0` from an absent
  key); the settings surface live-applies it per
  ADR 0037.
- **`EventBase` grows one additive field** (spawning call-ID), persisted as an **additive
  transcript-blob member** — *not* a blob version bump (corrected at implementation,
  2026-08-08; this bullet first read "the transcript blob version bumps additively"): the
  codec's own additive rule makes an `omitempty` member invisible to an older build, while
  a bump would make every blob this build writes unreadable to one, on ADR 0022's
  reject-forward rule, for no gain. Bench and headless consumers read the field or ignore it.
- **Concurrent children share one workspace.** Two children editing the same file can race;
  that is the model's (or the enumeration's) choice, as in other agent tools. Worktree
  isolation is explicitly out of scope for v1 — parked.
- **The window trade is the server operator's.** `--parallel N` shrinks the per-slot
  window; Apogee's numbers were already per-slot-honest (ADR 0024) and stay so. The docs
  say it plainly: more parallel agents = smaller window each.
- **CONTEXT.md** gains **Parallel agents** and updates **Sub-agent** and **Guided
  decomposition**; the `sub_agent` events' call-ID join the transcript-blob description.
