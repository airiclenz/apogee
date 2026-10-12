---
title: Startup notices are stderr-only — the in-transcript banner
status: done
type: feature
priority: 4
aliases: [apogee-2sj]
created: 2026-09-03T18:40:20Z
closed: 2026-09-16T21:56:34Z
reason: startup confinement notices repeat in the transcript as ephemeral notes (6d160f1e)
---

**Status:** extracted 2026-07-27 from the closed *Auto-mode confinement degradation* entry (its
one still-open residue) so the closed body could leave this file. The underlying parked idea is
the validated-set in-transcript banner — deferred follow-up 04 of the validated-set work
(`docs/plans/archived/validated-set-runtime-surface.md`: "no TUI in-transcript banner in v1,
revisit if the stderr line proves easy to miss").

Every startup notice (the validated-set notice, the Auto-confinement degradation notice) prints
pre-alt-screen on **stderr only**; `/confine status` renders the same facts in the transcript on
demand, but the startup moment does not. Folding startup notices into the transcript means
building the banner rendering the degradation plan deliberately did **not** build (its item 7
used the existing notice path and named follow-up 04 the owner of any framework). No wrong
behaviour — pick it up when a stderr notice proves easy to miss.
