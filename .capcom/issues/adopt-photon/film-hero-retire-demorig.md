---
title: Film hero with photon, compare with demo.gif, retire cmd/demorig
status: done
type: task
priority: 3
deps: [hero-photon-config]
plan: 2026-10-17 - 02 - film-hero-retire-demorig-plan.md
created: 2026-10-17T10:34:01Z
closed: 2026-10-17T16:56:41Z
reason: "Resolved by plan 2026-10-17 - 02 - film-hero-retire-demorig-plan (archived in e9b14d71): cmd/demorig retired, hero.yaml is photon's storyboard, photon review take approved by owner and committed as graphics/demo.gif."
---

Record and render hero under photon, compare with `graphics/demo.gif`, then delete
`cmd/demorig`, its `make demorig` target, the `APOGEE_DEMO_WORK` fallback in the rig scripts,
and `graphics/demo/fonts/` (photon embeds its fonts; `photon.yaml` sets no `fonts:`). Rename
`storyboards/hero.photon.yaml` over `hero.yaml`. Point `graphics/demo/README.md` at photon.

Waits on photon plan items 17 (`record`/`setup` with services and profiles) and 23 (`render`),
outside this register.

## Acceptance

- `photon record` and `photon render` produce a clip equivalent to `graphics/demo.gif`.
- `cmd/demorig`, its Makefile target and `graphics/demo/fonts/` are gone; `make check` passes.
