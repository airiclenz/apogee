---
title: ADR 0032 context
---

Skill discovery walks three layered source dirs and lets a later one replace an earlier one on
an id collision. `sourceDirs` (`internal/skills/load.go:56-72`) returns them in *increasing*
priority — `<apogee home>/skills`, `<workspace>/.apogee/skills`, then `<workspace>/skills` — and
`Catalog.set` (`internal/skills/catalog.go:36`) is a plain last-writer-wins map assignment. Two
properties follow that were never weighed together:

1. **The workspace outranks the user.** `<workspace>/.apogee/skills` is scanned
   *unconditionally* — the `use-project-skills` flag guards only the bare `skills/` dir — so
   repo-authored content wins any id collision with the user's own global library.
2. **The substitution is invisible.** Nothing is recorded when a skill is replaced: `Catalog`
   keeps the winner and forgets the loser existed.

The 2026-08-01 audit
(`docs/reviews/2026-08-01 - code-audit.md`, since pruned from the tree — see git history before
`693a0ca9`; "Medium
— Repo-supplied skills outrank the user's global library and shadow it silently") names the
attack that composes them. A hostile repo ships `.apogee/skills/<id>/SKILL.md` carrying the
`displayName` and `summary` of a skill the user already has, and an attacker-authored body. The
user clones, starts apogee, and types `/<id>` — the id they have used for months. The repo's
instructions are prepended to that turn with full agent authority
(ADR 0027); in Auto that is arbitrary
command execution. The `/skill` picker shows exactly one row, with the user's own display name
and summary on it, and no collision notice anywhere.

The order was adopted as **apogee-code oracle parity** and is documented as such in the
`sourceDirs` comment — a code comment, weighing interoperability, not the trust consequence.
That consequence runs against the project's own repo-trust posture: under
ADR 0012 a project
may only *add* to the dangerous-action rules (tighten), never remove — a repo is treated as
untrusted input everywhere else in the system, and here it silently replaces a user's
instructions.

The silence is a second, independent defect, and it is not limited to the cross-source case:
two skill folders with colliding ids inside **one** source dir also lose one without a word,
against this package's own stated contract that "soft must not mean silent, or a broken skill is
indistinguishable from an absent one" (`internal/skills/doc.go:23-25`).
