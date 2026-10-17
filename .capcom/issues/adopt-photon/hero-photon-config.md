---
title: graphics/demo/photon.yaml and hero.photon.yaml
status: open
type: task
priority: 3
deps: [stubllm-cassette-subcommands, session-expect-helper, rig-scripts-photon-work]
plan: 2026-10-17 - 01 - photon-adoption-prep-plan.md
created: 2026-10-17T10:34:01Z
---

apogee's photon project config and the hero storyboard in photon's schema.

- `photon.yaml`: `setup`/`reset` → the rig scripts; launch as
  `bash -c '. "$PHOTON_WORK/env.sh" >/dev/null && exec apogee'` (photon launches with
  `sh -c 'exec <cmd>'`, so demorig's compound launch breaks) or the env moved into `launch.env`;
  `APOGEE_*` in the strip list; ⌃c⌃c as end keys; the theme set explicitly; services
  `stubllm replay`/`capture` on fixed port 18181, profiles `replay` (default) and `capture`
  whose `on_success` copies the saved cassette into `storyboards/hero.cassette`.
- Areas `footer`/`status`/`transcript` ported from demorig's `areaRows`/`lowestRowOpeningWith`
  as nested anchors, checked against `layout.md`.
- `storyboards/hero.photon.yaml`, a sibling of `hero.yaml` so `demorig` keeps filming until
  retirement (owner, 2026-10-17): `expect: {stage: dirty}` becomes a take-level
  `run: test -n "$(git -C … status --porcelain)"`; entry expects become `sessionexpect` runs.

Needs photon plan item 8 (`photon lint`) — landed.

## Acceptance

- `photon lint` passes on `graphics/demo/storyboards/hero.photon.yaml` with `graphics/demo/photon.yaml`.
- `setup.sh` builds `stubllm` and `sessionexpect` into `$WORK/bin`; `demorig` still lints `hero.yaml`.
- Every demorig expect in `hero.yaml` has a photon equivalent.
