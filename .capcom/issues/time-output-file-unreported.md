---
title: "`time -o FILE` / `time --output FILE`: the file time writes is never reported as a write target, and `-o`'s value is taken as the leader"
status: done
type: bug
priority: 3
labels: [security]
created: 2026-11-03T18:09:10Z
closed: 2026-11-03T18:37:42Z
reason: "Resolved by plan '2026-11-03 - 02 - wrapper-option-bugs-plan' item 1 (archived at 1b9580fb): time -o/-oFILE/--output[=]/prefix report the output file and -o/-f values are read"
---

Found during plan `2026-11-03 - 01 - open-bugs-plan` (item 1, after `74189cf7`), not introduced by it. GNU `time -o FILE` / `--output=FILE` writes its report to FILE, but `writeTargetsOf` (internal/security/shellwrites.go:57) never reports it:

| command | `writeTargetsOf` | expected |
|---|---|---|
| `time --output=out.txt ls` | `""` | `out.txt` |
| `time --output out.txt rm .git/hooks/x` | `.git/hooks/x` | `out.txt .git/hooks/x` |
| `time -o out.txt ls` | `ls` | `out.txt` |
| `time -o out.txt rm .git/hooks/x` | `rm .git/hooks/x` | `out.txt .git/hooks/x` |

Two causes: `stripWrappers` drops the `time` output option with its value without emitting it, and `time` has no entry in `wrapperValueLetters` (shellwrites.go:865), so `-o` / `-f` strip bare and their value word is taken as the wrapped leader (the comment at shellwrites.go:891 says these "stay unread"). The leader confusion here over-reports, so it hides no write, but the output file itself is a missed write target.

## Why

A Floor guard that checks shell write targets (e.g. `write-git-control-plane`) cannot see a write that `time` makes, so `time -o .git/hooks/pre-commit true` would get past it.

## Acceptance

- `time -o FILE`, `time -oFILE`, `time --output FILE`, `time --output=FILE` and unique prefixes (`--out FILE`) all report FILE as a write target
- `time -o FILE cmd …` and `time -f FMT cmd …` take `cmd` as the wrapped leader
- Rows for the cases above in `TestWriteTargetsOf`, failing before the fix
