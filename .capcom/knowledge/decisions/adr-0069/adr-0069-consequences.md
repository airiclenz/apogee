---
title: ADR 0069 consequences
---

- The `sub_agent` tool grows an optional `run_on` enum (`session` | `sub-agents-server`), present
  in the schema only at depth 0 and only under `sub-agents-choice: model`; the roster surface
  (ADR 0057) is
  untouched — this is a variant of one tool's schema, not a second tool.
- `ServerEntry` grows `description:` (free text, valid on any entry, no validation beyond its
  type); `fileConfig`/`Options` grow `SubAgentsChoice`, defaulting to `fixed`, and the settings
  registry grows a row for it. `/sub-agents-server`'s picker rows show the description; `/server`'s
  do not — that picker answers a different question.
- The engine resolves the seat at spawn, at ADR 0066 decision 7's single consultation point, which
  is now used rather than reserved; the recursion path is untouched, and a child spawns with no
  seat parameter in its own schema.
- Fan-out width gains one case: a depth-0 reply whose `sub_agent` calls do not all share a seat is
  batched at `min(session cap, target cap)`. Everything else about ADR 0039 — the pin-else-
  discover-else-floor cap (the floor 4 keyed, 1 otherwise since 2026-09-19), one width per reply,
  the depth-0 bound, per-child failure independence — stands.
- The Orientation block gains a Delegations line under `sub-agents-choice: model`, built from
  session-constant facts and omitted entirely under `fixed`, so ADR 0023 §6's per-session-constant
  and omit-what-you-do-not-have rules both hold.
- A delegation result may carry one appended note line ahead of ADR 0063 D3's trailer; readers
  that assert on the last line of a result assert on the trailer where one exists, and on the note
  otherwise.
- The invariant is untouched: seat choice steers nothing, injects nothing and executes only calls
  the model already made, so it is on under **Bypass** and owes no
  ADR 0009 gate — ADR 0045 decision 6's reasoning verbatim. The
  gate's default (`fixed`) means no default moves either way.
- ADR 0045's deferred **Model-chosen routing** item is ratified here rather than left open; its
  "plus prompt-surface bench evidence" precondition is what `description:` and the Delegations
  line answer, and the bench arm for the prompt surface belongs to apogee-sim. **Launcher
  actuation stays deferred**, unchanged.
- **Out of scope, unchanged:** routing to arbitrary entries or tiers (one Sub-agent server stays),
  per-entry heartbeat monitors, launcher actuation, and any per-call choice below depth 0.
