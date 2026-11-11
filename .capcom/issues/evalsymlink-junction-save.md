---
title: "Settings save refused: EvalSymlinks fails through a Windows junction"
status: open
type: bug
priority: 2
labels: [windows]
plan: 2026-11-11 - 03 - junction-save-and-panel-deleted-file-plan
created: 2026-11-11T20:06:01Z
---

Saving a setting (e.g. `side-panel.width` from the settings panel) fails with:

```
✦ side-panel.width: not saved: apogee: resolve config "C:\\Users\\ERES\\.apogee\\config.yaml": The system cannot find the path specified.
```

The config file exists and is readable. `writeConfigAtomically`
(internal/config/configsplice.go:219) begins with
`filepath.EvalSymlinks(path)` to write through a dotfiles symlink, and that
call fails even though the file is there — the save is refused before any
write is attempted.

## Why

On this machine `C:\Users\ERES\.apogee` is an NTFS **junction** into the
dotfiles repo (`C:\Users\ERES\Repos\Airic\dotfiles\apogee`) — the exact
dotfiles-link layout `writeConfigAtomically` was designed for. Since Go 1.23
(`winsymlink=1`), mount points are not treated as symlinks, and
`filepath.EvalSymlinks` on Windows fails with `ERROR_PATH_NOT_FOUND` for paths
that pass **under** a junction (open reports: golang/go#82447, golang/go#63703).

Probed in the failing environment (go1.26.11 windows/arm64):

```
EvalSymlinks C:\Users\ERES\.apogee\config.yaml -> err=The system cannot find the path specified.
Stat         C:\Users\ERES\.apogee\config.yaml -> size=32402 err=<nil>
EvalSymlinks C:\Users\ERES\Repos\Airic\dotfiles\apogee\config.yaml -> ok   (the real target)
```

Every file and subdirectory under the junction fails `EvalSymlinks` the same
way; the junction directory itself resolves (to itself, unchanged); `os.Stat`,
`os.Open` and reads all work. So the junction case is written through fine on
Linux/macOS dotfile-symlink setups and silently broken on Windows junctions.

## Acceptance

- `writeConfigAtomically` (and any other config writer that resolves the path
  first) completes the write when the path exists but `EvalSymlinks` fails:
  the config's new content is on disk, written through the junction (the file
  at the junction target holds the new bytes), with the previous mode carried
  over.
- The existing symlink write-through and read-only-target refusal behaviours
  are unchanged (their tests still pass).
- A test covers the junction case (Windows, skipped where a junction cannot be
  created — `mklink /J` needs no elevation, so the test can create one).
- Saving a setting on a junctioned config home no longer reports
  "not saved: … resolve config …".
