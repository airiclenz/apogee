---
title: ADR 0070 considered options
---

**A — Leave D1 as it is and let the user enable the off-ramps.** Rejected: it keeps the defect for
everyone who never reads the catalogue, and it asks a user to opt into a recovery from a failure
they have not had yet. It also leaves the absurdity intact — `--bypass` remaining the more robust
posture.

**B — Bench the two off-ramps and flip them on the evidence.** Rejected: D1's gate measures a
Mechanism across a Turn distribution, and an off-ramp is off that distribution — it fires only on a
Turn that already failed. The measurement would be dominated by noise from Turns where the row
never ran, and the arm that withholds it is not a posture anyone would want to ship even if it
scored level. ADR 0006 made the same judgment when it exempted them from Bypass.

**C — Stop calling them Mechanisms; make them structure.** Rejected: they *are* gated,
descriptor-declared, hook-attached behaviour with a catalogue row, an ID and a config key, and
users can still turn one off. Moving them out of the catalogue would cost the `/settings` row, the
config key and the descriptor discipline to buy a wording change.

**D — Default them on, with no way to turn them off.** Rejected: an off-ramp injects text, and an
operator debugging their own upstream may want a genuinely silent floor. The explicit `false` costs
one line and keeps the guarantee an opinion rather than a lock.
