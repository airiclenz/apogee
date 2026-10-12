---
title: ADR 0086 consequences
---

- **ADR 0013 §5 gains a third ending.** A child either completes inside the parent's Turn, or a
  cancel rolls that Turn back with no partial result — or, from this ADR, the human stops it and
  the parent receives a partial result and goes on. (Amended 2026-09-30: a cancel no longer rolls
  the Turn back; ADR 0088 D1-D2 settle it
  and stop each running child as a stop does.) §5's coarse resume and "no snapshot mid-child"
  are unchanged; its 2026-09-18 amendment's "engine memory that dies with the Exchange" no longer
  holds.
- **ADR 0022's 2026-09-18 addendum is reversed.** "A `continue` after a resume is refused with
  `retained: none`, by design" no longer holds. D8 itself stands: a child's `Session` is still never
  a record, and the retained entry is the parent's engine state, not the child's.
- **ADR 0063's deferrals close.** D4's "stopping stays whole-run from the top level" and the
  "What stays out" lines on per-child stop and on prompting a finished child are amended. A run view
  of a finished child stays read-only: the human messages a running child, and it is the parent that
  continues a finished one. `layout.md`'s "`esc` means back before it means stop" section is
  restated against `^x`.
- **ADR 0039 D4 stands.** A stop is not a cancel, and a pool still joins every child before the
  parent continues; a stopped child simply reaches that join sooner. The 2026-09-14 amendment's
  "running children are never cancelled by a message" also stands — a stop is a key, not a message.
- **Model-facing text moves slightly, and ships on.** The `sub_agent` schema's `continue` description
  widens to named delegations of this session (fixing its current "in this conversation" drift), a
  stopped result is new, and the refusal's listing is capped. None of it is a Reaction: it is the
  shape of an existing tool, structural like the continue line it extends, and on under Bypass.
- **Glossary.** `CONTEXT.md` gains **Stop (a delegation)** and **Retained delegation**. The
  **Sub-agent** entry's "for the rest of its Exchange, in memory only" is restated when this lands.
