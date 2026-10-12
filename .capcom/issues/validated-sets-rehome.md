---
title: "Re-home or remove validated-sets: after the Reaction core (inert since stage 1)"
status: done
type: task
priority: 3
aliases: [apogee-jwf]
created: 2026-09-07T20:11:07Z
closed: 2026-09-11T10:28:51Z
reason: "delivered: 6553a121 deleted validated-sets: and internal/validated; cbb400a4 CONTEXT.md; 38521ded layout"
---

Ratified 2026-09-07 (stage 1 plan, owner call): validated-sets: and internal/validated keep loading but arm nothing once Config.EnableMechanisms is deleted (item 13 of docs/plans/2026-09-07 - 01). Stage 2 decides: per-model Reaction rosters (ADR 0016 restated over reaction ids) or removal of the surface (key, package, shipped.json, /settings row, docs/manual/configuration.md §validated-sets) with an ADR 0016 amendment.

## Log

- 2026-09-08 beads: DECIDED by the stage-2 grill: DELETE. ADR 0076 amendment A9 + ADR 0016 amendment (0016 is now superseded in full). Removes validated-sets:, internal/validated, shipped.json, both /settings rows, docs/manual/configuration.md 795-833; the stage-2 config migration strips the key from the user's file. Owned by the stage-2 plan (apogee-pjx).
- 2026-09-08 beads: (stage 1 closeout, deferred finding): CONTEXT.md:1819 **Validated set** still says a user's ~/.apogee/validated/*.json entry "still resolves, still applies" — item 13 of docs/plans/2026-09-07 - 01 made that surface inert, so the deletion sweep must rewrite or drop that CONTEXT.md entry as well as the manual's sections.
