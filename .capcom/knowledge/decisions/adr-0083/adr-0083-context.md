---
title: ADR 0083 context
---

An architecture review (`/improve-codebase-architecture`, saved under `docs/reviews/`) reads the
tree afresh each time, and a candidate the owner has already declined is found again by the next
one: the shape is still there, the explorers still see it, and nothing in the tree says it was
weighed and refused. The 2026-09-14 review raised a live *Posture* value, a host-side bound entry,
a host-boot module and a Client-owned effort dialect; the owner ruled on each in the two
2026-09-15 plans (`docs/plans/archived/2026-09-15 - 00 - engine-shape-deepening-plan.md`,
`… - 01 - host-config-and-tools-deepening-plan.md`), where a denied row was "recorded here only" —
a verdict table and an *Out of scope* line in a plan that was archived when its items landed. The
2026-09-16 review found the delegate's flat fields again and listed the bound entry's residue
under *Adjudicated earlier*; the 2026-09-20 review (`docs/reviews/archived/architecture-review-2026-09-20.html`)
listed all five under the same heading, added a sixth candidate that contradicts two ADRs, and
said the quiet part: "None has an ADR — if a ruling is meant to be permanent, an ADR is the
cheapest way to stop the next review re-finding it."

A plan is a design artefact for the work it contains, not a register of what was refused; once
archived, a denial in it is invisible to the next reviewer and to the next agent, and the
refusal's *reason* — the part that decides whether new evidence reopens it — is a table cell.
This record moves the six standing denials into the place settled questions live. It ratifies
the owner's call of 2026-09-20 (plan `2026-09-20 - 02`, *Ratified design calls*: "the denial and
the five *Adjudicated earlier* rulings become one ADR").
