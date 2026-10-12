---
title: ADR 0065 context
---

Skills are an **open extension point**: apogee discovers folders holding a `SKILL.md` from three
layered dirs and ships none of its own
(ADR 0027,
ADR 0032). `internal/skills/doc.go` states
that as a decision and attributes it to
ADR 0002's open-extension-point
posture — "no builtins shipped".

Two facts have accumulated against it.

**A fresh install has an empty catalog.** `/skills` lists nothing, the `/` menu offers nothing, and
the suggestion band (ADR 0061)
has nothing to suggest — every one of those surfaces is dead until the user writes a skill folder by
hand. The features exist; the content that makes them worth anything does not, and the audience
least likely to author it is the one apogee is built for.

**The guidance that would go in one has nowhere else to live.**
ADR 0064 gives *standing* steering a
maintained home in the binary and, in its §5 placement rule, sends **task-shaped** instruction — the
paragraphs you want while debugging and not while writing a commit message — to a Skill instead.
That rule is unusable while no skill can ship.

**And discovery stops at the human.** ADR 0061 Decision 4 named two model-facing shapes and built
neither: **B1**, auto-attaching a body to a message carrying no `/id`; **B2**, a model-callable
`load_skill` tool. B2 was deferred on two grounds — that it needed a Mechanism's gating-and-bench
apparatus, and that it "contradicts CONTEXT.md's *Skill* entry directly (*Avoid*: 'tool')". The
first ground was wrong on inspection: a tool is not a Mechanism (ADR 0002), and the deferral
inherited B1's argument rather than making its own. The second is a real domain-language question,
answered in §6 below. That deferral required an **explicit** supersession, which this record is.

This ADR records the design the owner ratified on 2026-08-31, implemented by
`docs/plans/2026-08-31 - 00 - base-guidance-and-shipped-skills-plan.md`. It **supersedes ADR 0061
Decision 4's B2 deferral only**; B1 stays deferred on its own terms.
