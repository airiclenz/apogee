---
title: cloneOptions shares Proposals and AllowRules with its source
status: done
type: bug
priority: 3
created: 2026-10-18T18:44:06Z
closed: 2026-10-18T20:56:55Z
reason: resolved by plan 2026-10-18 - 02 - exec-pin-followups-plan (archived at 86afb7c1), item 8
---

`cloneOptions` (`cmd/apogee/wire_settings.go:192-208`) is documented as the one deep copy of a `config.Options`, listing every collection it carries, but it does not copy `Options.Proposals` (added by this run, `internal/config/options.go:392-397`) or `Options.AllowRules` (`internal/config/options.go:386-390`). The copy is safe only while every writer replaces those fields whole; one that appends or sorts in place would edit the set the session runs, across goroutines (a Firing composes on the Scheduler's). `TestLiveSettingsOptionsFollowEveryApply`'s clobber, which the comment names as the guard, does not catch them.

Deferred out of plan `2026-10-18 - 00 - project-config-exec-keys-plan`.

## Acceptance

- `cloneOptions` deep-copies `Proposals` and `AllowRules` (their slices and maps), and its comment's collection count matches.
- The clobber test (or a new one) fails when a collection field of `config.Options` is not deep-copied.
