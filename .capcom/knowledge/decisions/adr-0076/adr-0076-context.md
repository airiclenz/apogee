---
title: ADR 0076 context
---

apogee reacts to the loop through three subsystems that are one abstraction in three coats: seven
**Floor guards** (engine Go, in-loop, edit the working value; seven flat booleans), the **Mechanism**
lab layer (Go, in-loop, retry / inject / edit; a `mechanisms:` map governing zero shipped rows since
ADR 0071), and five
observe-only **Hooks** (user argv / webhook, post-hoc, return nothing;
ADR 0073). They mutate the
same working values at the same five seams through three vocabularies, two firing events, a
guards-then-hooks double ladder at every seam, three config idioms and three live-swap idioms —
roughly 5.5k production lines of which the ~1.5k of policy in `internal/floor` is the value.

Bead `apogee-575` asked whether a Hook may advise the model. The research
([hook-talkback-findings.md](../../../../docs/design/hook-talkback-findings.md)) placed that at tier C of a six-tier
space, found the machinery already exists, and named the obstacle: ADR 0031 invariant 4 ("benchable
all the way up"), whose worked examples include trigger-injected prompts. The greenfield design
([reaction-core-greenfield.md](../../../../docs/design/reaction-core-greenfield.md)) reframed the question from
"supersede five ADRs" to "one core, one matrix, which user cells on day one". This ADR records the
grill of 2026-09-07 over that design's §3 and §8. It replaces ADR 0071 decisions 4 and 6, ADR 0073
decisions 2, 4, 5 and 7 and its second and third rejections, and rewrites the `Reaction surface`
glossary entry. ADR 0031 is **not** superseded: invariant 4 is satisfied by construction (decision 5).
