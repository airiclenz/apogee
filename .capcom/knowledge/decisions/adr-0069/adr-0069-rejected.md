---
title: ADR 0069 considered and rejected
---

- **Let the model name a `servers:` entry.** The obvious generalisation, and the one that turns
  the tool schema into a second reader of the config file: entry names change, an entry the model
  names may not exist, and the model would need the whole list in its prompt to choose from it.
  It also quietly grants a delegation a machine the session was never bound to, which is a
  privilege question ADR 0045 never opened. Two seats are the two things that already exist.
- **A closed set of tiers (`fast` / `smart` / `local`) mapped to entries.** A vocabulary the
  owner would have to translate their servers into, kept in agreement with the `servers:` list
  forever, and wrong the first time someone runs three boxes that are all "fast". The owner's own
  `description:` says more and needs no mapping.
- **An error result when an ask cannot be honoured.** It burns a Turn to tell the model something
  it can be told in one line of a result that also contains the finished work. ADR 0045 §4
  rejected error-or-wait for the static case on the same grounds; a per-call ask does not change
  the arithmetic, and the parent's own server is by definition alive.
- **Two pools, one per seat, each running to its own cap.** More throughput on a mixed reply, at
  the price of slot accounting across two servers and two widths inside one Turn — the shape
  ADR 0039 avoided by staying depth-0 and single-width. Revisit only with evidence that mixed
  replies are common AND that the smaller cap is the bottleneck.
- **Spelling the parameter `seat:` (or the key `sub-agent-seat:`).** "Seat" is the right word for
  the CONTEXT.md term — the pair of places a delegation may run — and the wrong word on the wire,
  where the model reads a single line of schema text and `run_on: session` states an action while
  `seat: session` states a noun the model has to have been taught. The domain keeps the term; the
  tool and the config keys do not spell it.
- **A per-call model or endpoint override.** The far end of the same road: a delegation would
  stop being "a child of this run on one of two known boxes" and become an arbitrary outbound
  request the engine composes from model-supplied strings — the wire-silent engine
  (ADR 0031)
  inverted.
