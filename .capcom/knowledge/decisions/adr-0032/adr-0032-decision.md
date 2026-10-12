---
title: ADR 0032 decision
---

**The user's global library wins any cross-source id collision.** `<apogee home>/skills` is the
highest-priority source: a workspace skill can no longer replace a skill the user already has.

**The workspace sources keep their relative order among themselves.** The bare `skills/` dir
(still gated by `use-project-skills`) still outranks `.apogee/skills`. Only the global library's
position moves; the intra-workspace rule is unchanged.

**Every collision is recorded, cross-source and same-source alike.** The losing `SKILL.md` is
recorded on the catalog through the existing skip channel, carrying a typed cause that names the
winning file's path, so `/skills` can report both sides: which copy is live and which was
shadowed. This is **not** a load failure — the loser parsed fine and simply lost a collision —
and the report must say so rather than filing it under "could not load".

**Discovery still scans all three dirs.** `.apogee/skills` is **not** newly gated behind
`use-project-skills`. A repo may still contribute skills; it may only no longer replace one of
the user's silently.
