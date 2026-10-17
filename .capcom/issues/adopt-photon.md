---
title: Film the hero clip with photon; retire cmd/demorig
status: open
type: chore
priority: 3
created: 2026-10-17T08:52:56Z
---

`cmd/demorig` is being extracted into photon (github.com/airiclenz/photon), a standalone demo
recorder that other projects use too. Once photon v1 ships, apogee films its hero clip with
photon and `cmd/demorig` goes. photon is one generic CLI with no Go API; a project plugs in
through commands and services in its `photon.yaml`, never through Go code.

photon's first version is ported from `cmd/demorig` at `bf5a37c2` (2026-10-14; ~5.5k lines
without tests, documented in `graphics/demo/README.md`). This map is what that port, and this
migration, rest on.

**Generic, ported nearly as is:** `term.go` + `ttyowner_*.go` + `term_windows.go`, `take.go`,
`typing.go`, `schedule.go`, `raster.go`, `compose.go`, `render.go`, most of `engine.go` and
`storyboard.go`, the `seen` clause of `check.go`, `main.go`'s exit-code mapping.

**Made configurable in photon:**

| demorig piece | where | in photon |
|---|---|---|
| areas `footer`/`status`/`transcript` read off `▁`/`▔` hairlines | `engine.go` `areaRows` | areas declared in `photon.yaml` |
| `env.sh`, `reset.sh`, `rig.env` PORT, `exec apogee`, the `taskman` stage | `record.go` `rig`, `runTake` | the config's setup, reset, launch and stage |
| `APOGEE_*` stripped from the take's env | `record.go` `ambientApogeeEnv` | the config's env strip list |
| ⌃c⌃c to quit | `record.go` `endTake`, `quitKeys` | the config's end-of-take keys |
| `expect: {stage: dirty}` | `check.go` `judgeStage` | a `run:` expect |
| `APOGEE_DEMO_WORK`, `~/.cache/apogee-demo` | `main.go` `workDir` | `PHOTON_WORK`, a per-project default |
| `graphics/demo/fonts` | `raster.go` | fonts embedded in photon |
| `internal/tuitest` keys, SGR click, `ClampMargins` | `engine.go`, `term.go` | photon's own code |

**Not in photon; apogee supplies them as hooks:**
- stubllm cassette replay (`record`) and recording proxy (`capture`): photon **services**,
  selected by profile (`replay` default, `capture`), with an `on_success` command saving the
  cassette.
- session-entry expects (`anchors.go`, `session.go`, the entry clauses of `check.go`): a small
  apogee helper command called from `run:` expects.

## Acceptance

- `graphics/demo/` holds a `photon.yaml`, and `hero.yaml` runs under `photon record` and
  `photon render`, producing a clip equivalent to today's `graphics/demo.gif`.
- `cmd/demorig` and its Makefile target are deleted; `graphics/demo/README.md` points at photon.
