---
title: ADR 0072 considered options
---

**A — A Mechanism that decomposes the request into a checklist.** Rejected, and it is the thing the
original denial was right to refuse: it injects a decomposition the model did not ask for, which is
tuning under the project's hard invariant and would ship off pending a bench arm. It also gets the
ownership backwards — the value here is that the model wrote the list.

**B — Item ids with add/update/remove verbs.** Rejected per decision 2: it makes the model carry
state across turns that compaction is most likely to eat, and every failure it introduces is
silent. The token cost of resending the list is real and is the price of never being wrong about
which row was meant.

**C — Live state, dropped on resume, like a Console.** Rejected: it would delete the list at exactly
the boundary a long run most needs it, and the ADR 0022 §8 reason for dropping a Console — a
running process cannot be serialized — does not apply to a list of sentences.

**D — Render it into the user message, or as a tool result the model re-reads.** Rejected: both
live in the conversation, which is what compaction rewrites. The standing content is re-seeded per
request and is the only position that survives, which is the whole point of the feature.

**E — A config key to turn it on or off, or default-off/profile-enabled like the Console family.**
Rejected per decision 6: `tools.disabled:` already expresses it, and the Console family's default-off
posture was bought by its blast radius, which a checklist does not have.

**F — One list shared across a delegation tree.** Rejected per decision 7: it turns the standing
block into an inter-agent channel and puts rows on a list held by an agent that cannot act on them.
