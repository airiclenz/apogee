---
title: "probe config accepts values startup refuses (sessions.max-age: 30d resolves 0s, exit 0)"
status: open
type: bug
priority: 2
plan: 2026-11-04 - 00 - config-refusal-parity-plan.md
created: 2026-11-04T20:46:42Z
---

Seqcom docs-a3-v0310 (apogee v0.31.0). `apogee probe config` reads through `config.LoadLayeredOffer`, which stops before the block validators `resolveOptions` runs (sessions, ui, present, system-prompt, context-files, servers, ValidateHost). So `sessions: {max-age: 30d}` prints `0s`, notices (none), exit 0, while `apogee probe host` and startup fail with `invalid sessions.max-age "30d"`. docs/manual/probe.md promises the reader's own sentence.

## Why

probe config exists to catch a broken config before a session starts.

## Acceptance

- probe config fails, with startup's sentence, on every value startup refuses (StartupUndetermined excepted)
- Its notices include those startup's later pass announces
