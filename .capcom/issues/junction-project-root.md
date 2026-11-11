---
title: Project root under a Windows junction breaks project config and adoption
status: open
type: bug
priority: 2
labels: [windows]
created: 2026-11-11T20:14:10Z
---

On Windows, `filepath.EvalSymlinks` fails with `ERROR_PATH_NOT_FOUND` for any path that passes under an NTFS junction (Go >= 1.23, `winsymlink=1`; golang/go#82447, #63703). Several project-config paths resolve the Project root with it and so fail or degrade for a workspace that sits under a junction:

- `internal/config/projectwrite.go` `prepareProjectFile` — hard error `cannot write the project config: resolve Project root`.
- `internal/config/projectwrite.go` `insideRoot` — hard error resolving `<root>/.apogee/config.yaml`.
- `internal/adoption/store.go` `key` — hard error `adoption store: resolve Project root` (reached from `editProject` via `adoption.ConfigLockPath`).
- `internal/config/layer.go` `readProjectLayer` — the project layer is skipped (silently or with a skipped notice).
- `internal/config/layer.go` `sameConfigFile`, `internal/config/config.go` `samePath`, `internal/projectroot/projectroot.go` `realPath` — degrade or fail.

These are confinement / identity canonicalisations, not write-throughs, so the fix needs its own design (e.g. a junction-crossing resolver such as `GetFinalPathNameByHandle`, which `internal/platform` already uses as `finalPathName`). Split out of `evalsymlink-junction-save`, which fixes only the write-through in `writeConfigAtomically`.

## Why

A Windows user whose repos live under a junction cannot adopt or save project settings, and the project layer may silently not load.

## Acceptance

- With a Project root under a junction: adopting the project, saving a project setting and loading the project layer all work as they do for a plain directory.
- A Windows test creates a junction (`mklink /J`, no elevation) and covers each listed path.
