---
title: "ADR 0071 decision 6: Rejected alternatives"
---

**A — Keep the six as Mechanisms under a new `structural` Capability, exempt from D1.** Rejected:
it is ADR 0070's carve-out done a second time and it keeps every cost the carve-out was working
around — a descriptor, an ID, a strikes posture that has to be exempted row by row, a `/settings`
row that invites a user to switch off a guarantee, and a catalogue that reads as twenty candidates
when six are settled behaviour and fourteen are gone. The wording change ADR 0070 declined to buy
is now the smaller half of the change: the substance is the strikes posture, the config shape and
the frozen catalogue.

**B — Delete the Mechanism layer entirely.** Rejected: the hook API and registry are the bench's
only way to arm an intervention without patching the engine, and ADR 0001
makes external drivability a standing commitment. Freezing the shipped catalogue costs nothing the
lab needs; deleting the surface would cost it everything.

**C — Promote the six as engine code with no config keys at all.** Rejected: a guard that injects
text into a model's context with no way to stop it is exactly the footgun ADR 0070's Option D
rejected. Six booleans are cheap, and they keep the floor an opinion rather than a lock.
