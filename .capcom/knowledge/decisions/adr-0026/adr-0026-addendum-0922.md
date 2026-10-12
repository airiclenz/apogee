---
title: "ADR 0026 addendum of 2026-09-22: the oversize ceiling is ADVISORY, no longer the reserved share"
---

§8 measures the whole standing system content against `Budget.SystemPrompt`, and the Considered
option above refuses a configurable size limit "because the Budget already allocates a
system-prompt share". The Budget now reserves what the standing content **measures** (the
measurement plus 10% headroom), so that reservation grows with the very content §8 weighs against
it, and reading it as the ceiling would mean the warning could never fire. The comparison is
against `Budget.StandingAdvisory` instead — the unchanged 15% of working room, carried on the
Budget as an advisory field of its own beside the reservations. Trigger, wording and the "never a
cap" posture are untouched, and so is the option's rejection: the ceiling is still a number the
Budget already knows, it is simply no longer the room the Budget holds.
