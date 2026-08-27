---
Status: accepted
---

# The user's skill library outranks the workspace

## Context

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
([`docs/reviews/2026-08-01 - code-audit.md`](../reviews/2026-08-01%20-%20code-audit.md), "Medium
— Repo-supplied skills outrank the user's global library and shadow it silently") names the
attack that composes them. A hostile repo ships `.apogee/skills/<id>/SKILL.md` carrying the
`displayName` and `summary` of a skill the user already has, and an attacker-authored body. The
user clones, starts apogee, and types `/<id>` — the id they have used for months. The repo's
instructions are prepended to that turn with full agent authority
([ADR 0027](0027-one-slash-namespace-with-inline-skill-tokens.md)); in Auto that is arbitrary
command execution. The `/skill` picker shows exactly one row, with the user's own display name
and summary on it, and no collision notice anywhere.

The order was adopted as **apogee-code oracle parity** and is documented as such in the
`sourceDirs` comment — a code comment, weighing interoperability, not the trust consequence.
That consequence runs against the project's own repo-trust posture: under
[ADR 0012](0012-confinement-attaches-to-blast-radius-and-confine-to-workspace-flag.md) a project
may only *add* to the dangerous-action rules (tighten), never remove — a repo is treated as
untrusted input everywhere else in the system, and here it silently replaces a user's
instructions.

The silence is a second, independent defect, and it is not limited to the cross-source case:
two skill folders with colliding ids inside **one** source dir also lose one without a word,
against this package's own stated contract that "soft must not mean silent, or a broken skill is
indistinguishable from an absent one" (`internal/skills/doc.go:23-25`).

## Decision

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

## Considered options

- **Keep the oracle order** — rejected: parity is worth real weight for *portability of skill
  files*, but not at the price of letting a clone redefine a command the user invokes by muscle
  memory. Nothing about a shared file format requires a shared collision rule.
- **Gate `.apogee/skills` behind `use-project-skills`** — rejected: it answers a different
  question. The problem is silent *replacement*, not contribution; a repo shipping genuinely new
  skill ids is a feature ([ADR 0002](0002-tools-are-an-open-extension-point-mechanisms-are-curated.md)),
  and defaulting it off would break the ordinary case to fix the adversarial one.
- **Warn but keep workspace priority** — rejected: it leaves the dangerous default in place and
  charges the user for it, on a notice they see once at load and not at the moment they type
  `/<id>`.
- **Refuse to load a colliding workspace skill entirely** — rejected: harsher than needed and it
  loses information. Recording the pair keeps the diagnosis (`/skills` can name both files) while
  the resolution alone decides what runs.

## Consequences

- **A deliberate, documented deviation from oracle parity.** A `SKILL.md` written for either tool
  still loads in both; only collision *resolution* differs. The `sourceDirs` comment stops
  selling the order as parity and states this rule instead, citing this ADR.
- **A repo can still contribute skills, but never substitute one.** New ids from
  `.apogee/skills` load exactly as before; a colliding id now loses and is reported.
- **Shadowing becomes visible.** The `/skills` report gains a section distinct from load
  failures, naming the shadowed file and the winner's path. The same-source case, silent today,
  is covered by the same mechanism.
- **[ADR 0002](0002-tools-are-an-open-extension-point-mechanisms-are-curated.md)'s
  open-extension-point posture is unchanged**, as is
  [ADR 0001](0001-agent-loop-is-an-embeddable-library-driven-by-an-external-bench.md)'s injected-roots
  rule — the source dirs are still supplied by the caller, only their precedence is fixed here.
- **This supersedes no ADR.** The prior order was never ratified in a decision record; it lived
  in a code comment, which is the reason the trade-off went unweighed.

## Amendment (2026-08-26) — the walk runs highest-priority first and a collision keeps the first copy

The 2026-08-25 security audit (F-06) found the precedence above enforceable only where every
source is actually read. The global cap (`maxSkills`) is first-come across all source dirs while
priority was last-write, so a repo shipping `maxSkills` skill folders filled the catalog *before*
the user's library was walked at all — and no write-order rule can hand back an id that was never
loaded. The repo then owned both the ids and the cap.

Both halves invert together: `sourceAnchors` now returns the sources in **decreasing** priority —
the global library first, then the bare `skills/` dir, then `.apogee/skills` — and `Catalog.set`
**keeps the first** copy of an id, recording the newcomer as shadowed. This reaches the identical
"home wins, bare `skills/` beats `.apogee/skills`" outcome from the other end, while the cap can
now only ever cut into the lowest-priority source. Precedence is unchanged; only the mechanism
enforcing it moved. The Context above describes `sourceDirs` as returning *increasing* priority:
that describes the pre-amendment code.
