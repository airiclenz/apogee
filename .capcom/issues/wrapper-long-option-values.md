---
title: Wrapper long value options (`env --unset NAME`, `sudo --user root`) still strip bare, so the next word is taken as the leader
status: done
type: bug
priority: 2
labels: [security]
plan: 2026-11-03 - 01 - open-bugs-plan
created: 2026-11-03T15:55:58Z
closed: 2026-11-03T17:58:59Z
reason: "Resolved by item 1 of plan '2026-11-03 - 01 - open-bugs-plan' (archived in b3ffc752): wrapper long options resolve by name or unique prefix, value-taking ones consume their next word, unknown ones fail closed."
---

Deferred from item 5 of `docs/plans/archived/2026-11-03 - 00 - code-audit-fixes-plan.md` (wrapper-option-values-leader). `takesNextWordValue` (internal/security/shellwrites.go:1484-1485) returns false for every `--` option, so `stripBareOptions` (internal/security/shellwrites.go:1470) drops a wrapper long option bare and leaves its separate value word as the wrapped leader. The plan's value table (`wrapperValueLetters`, internal/security/shellwrites.go:863) covers only short options, so `env --unset echo rm -f .git/hooks/pre-commit`, `sudo --user root …`, `nice --adjustment 5 …` and `xargs --max-args 1 …` keep the leader confusion item 5 fixed for the short forms. A `--opt=value` word is unaffected.

## Why

A hostile model can still slip a hook write past `write-git-control-plane` through the long spelling of the same wrapper options.

## Acceptance

- Each wrapper's value-taking long options (`env --unset/--chdir/--split-string`, `sudo --user/--group/--host/--prompt/--close-from/--chdir/--chroot/--command-timeout/--other-user`, `nice --adjustment`, `xargs --replace/--max-args/--max-chars/--max-procs/--delimiter/--eof/--arg-file/--max-lines` where they take a separate word) consume their next word, or the strip fails closed.
- `env --unset echo rm -f .git/hooks/pre-commit` reports the hook target.
