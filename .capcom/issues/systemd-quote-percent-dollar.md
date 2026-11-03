---
title: systemd unit quoting does not escape `%` and `$`
status: done
type: bug
priority: 2
plan: 2026-11-03 - 00 - code-audit-fixes-plan
created: 2026-11-03T14:20:29Z
closed: 2026-11-03T16:07:40Z
reason: Resolved by plan '2026-11-03 - 00 - code-audit-fixes-plan' (archived in c3346711)
---

From docs/reviews/code-audit-2026-11-03.md; fixed by item 16 of `docs/plans/2026-11-03 - 00 - code-audit-fixes-plan.md`. `systemdQuote` (cmd/apogee/daemoninstall.go:426-432) quotes on `$` and `%` but escapes only `\` and `"`; systemd expands `%` specifiers and `$VAR` inside double quotes.

## Why

A binary or `--config` path containing `%` or `$` yields a unit that starts a different path or fails, while `install` reports success.

## Acceptance

- `%` is written `%%` and `$` is written `$$` (in argv[1:]); a test pins both.
