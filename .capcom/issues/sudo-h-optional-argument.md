---
title: "`sudo -h` is listed as a required-value letter, but sudo takes its host only when attached (`-hHOST`)"
status: done
type: bug
priority: 4
labels: [security]
created: 2026-11-03T18:09:10Z
closed: 2026-11-03T18:37:42Z
reason: "Resolved by plan '2026-11-03 - 02 - wrapper-option-bugs-plan' item 2 (archived at 1b9580fb): sudo -h modelled as an optional-argument option"
---

Found during plan `2026-11-03 - 01 - open-bugs-plan` (item 1, after `74189cf7`), not introduced by it. `wrapperValueLetters["sudo"]` (internal/security/shellwrites.go:867) includes `h`, so `stripWrappers` takes the next word as `-h`'s value. sudo declares `-h` with an optional argument (getopt `h::`): `-hHOST` sets the host, while a bare `-h` is `--help`, and sudo then runs nothing. So `sudo -h rm .git/hooks/x` is parsed as host `rm` with leader `.git/hooks/x`, when sudo actually prints help. The long form `--host` does need its argument and is right in `wrapperLongOptions`.

Today the misparse is harmless: `writeTargetsOf("sudo -h rm .git/hooks/x")` gives `""`, and sudo runs nothing either. The view still differs from what sudo does, and any later rule that reads the wrapped leader would act on the wrong word.

## Acceptance

- sudo `-h` is modelled as an optional argument: `-hHOST` consumes only its own word, and a bare `-h` consumes no next word
- A `TestWriteTargetsOf` or `stripWrappers` row covers bare `sudo -h` followed by a word
