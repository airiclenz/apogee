---
title: ADR 0014 considered options
---

- **Structural reducer (Compaction's peer, on under Bypass)** — *rejected*: the bench would
  be structurally blind to whether decomposition helps or hurts; fragmented fan-outs
  plausibly lose cross-file coherence, and only a control arm can tell.
- **Silent internal Upstream call + direct orchestrator invocation** — *rejected*: creates a
  Mechanisms-may-call-the-model capability class, a second spawn path outside the ADR 0013
  recursion point, and fabricated history explaining where N results came from.
- **All N children in one Turn** — *rejected*: cancel during child 9 destroys nine child
  Exchanges; all N reports pile into the one *protected* most-recent Turn where no reducer
  may touch them (the explosion returns); no adaptation between children.
- **A fourth Capability value (`task-shaping`)** — *rejected*: a one-member class, an
  ADR 0006 touch, and the catalogue already reads `proactive-nudge` as "proactively
  intervenes", not "injects prompt text".
- **Prompt-shape (verb) gating** — *rejected*: "review this function" would hijack a fitting
  task into enumeration; misfires are hard-constraint violations, and both false directions
  are unavoidable when guessing semantics from keywords.
- **Depth-1 firing in v1** — *rejected*: multiplies the acknowledged one-child atomicity
  coarseness by an entire nested fan-out; additive to enable later with evidence.
