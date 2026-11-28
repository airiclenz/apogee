---
title: The rm -rf hard refusal judges only the first operand
status: open
type: task
priority: 3
labels: [security]
plan: 2026-11-28 - 00 - audit-bounds-guards-deadlines-plan.md
created: 2026-11-27T21:27:15Z
---

`internal/security/rules.go:122`: the `rm-rf-root-home-system` and `rm-fr-root-home-system`
patterns require the protected target to be the very next token after the flags.

`rm -rf /etc` is refused, but `rm -rf build /etc`, `rm -rf ./out /` and `rm -rf node_modules /etc`
slip through — the same operation judged differently by operand position.

Fix: insert `(?:[^\s;|&]+\s+)*?` ahead of `quoteOpen + deleteTargetAnchor` in both patterns; the
class excludes separators, so it never crosses into the next command.

Source: `docs/reviews/code-audit-2026-11-27.md` (Medium, independently verified).

## Acceptance

- A protected target in any operand position is refused; a table test covers first, middle and last positions and a following separate command.
