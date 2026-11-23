---
title: Typing an @ file mention walks the workspace inside Update
status: open
type: task
priority: 2
labels: [perf, tui, slow-box]
plan: 2026-11-23 - 07 - tui-perf-frame-and-local-fixes-plan.md
created: 2026-11-23T19:29:39Z
---

## Problem

Scenario: typing. When the caret is in an `@` file token, each keystroke re-derives the
autocomplete menu, and the file listing behind it is read from disk synchronously inside
`Update`:

`handleKey` (`internal/tui/model.go:2325`) → `recomputeAutocomplete`
(`internal/tui/autocomplete.go:207-216`) → `computeAutocomplete` (`autocomplete.go:162`,
`caretFileToken`) → `fileSuggestions` (`autocomplete.go:667`) → `fileCache.suggest`
(`internal/tui/filecache.go:48-50`) → `walkWorkspaceFiles` (`filecache.go:82`).

The walk is `os.OpenRoot` + `fs.WalkDir` over every non-dot directory (`node_modules`,
`vendor`, `build` included) until 4,096 files (the only bound, the `SkipAll` at `filecache.go:108`),
then a sort. It runs whenever the cache is cold: on first use, on a root change, or when its 3 s
TTL has lapsed (`filecache.go:23`). So the first `@` keystroke of a session, and the first
keystroke inside an `@` token after any 3-second pause, blocks key handling for a whole disk
walk. There is no Cmd/worker hop — unlike the skill-catalog re-scan in the same function, which
the code's own comment (`autocomplete.go:194-202`, `reloadSkillsCmd`) moves off the loop for
exactly this reason.

Not measured: the audit's typing benchmark typed no `@`. On a Pi's SD card with a cold page
cache a walk of up to 4,096 files is expected to take hundreds of milliseconds to seconds.

## Why

On a Raspberry Pi 5–class host the prompt freezes mid-word: keys typed during the walk queue
behind it and echo late, and the spinner and stream stop updating, while the human is doing
nothing more than naming a file.

## Suggested fix

Suggestion from the audit: dispatch the walk as a Cmd the way `skillRescanCmd`
(`autocomplete.go:252`) does. On a stale or empty listing, filter what the cache holds now
(empty on first use), mark a walk in flight on the shared `*fileCache` so a burst of keys
dispatches one walk, and land a `filesListedMsg` that swaps the listing and re-derives the open
menu with `reselectRow` (the `foldSkillsReloaded` pattern, `autocomplete.go:283`). The rows are
identical once the walk lands; check the menu's not-yet-listed state against
`docs/layout/` so no new visual state is introduced.

## Acceptance

- [ ] `walkWorkspaceFiles` never runs on the `Update` goroutine — a test fails if it does.
- [ ] A test driving the TUI through `internal/tuitest` + `internal/stubllm` (never an
      `httptest` upstream closure) types `@` and waits on a condition for the file rows, which
      match today's rows.
- [ ] A burst of keystrokes while a walk is in flight starts no second walk.
- [ ] No tight deadline added: the test waits through `internal/testtime` with a generous
      backstop, and the shipped code has no fixed clock on the walk (slow-box rule).
- [ ] `APOGEE_TEST_SLOW=1 GOMEMLIMIT=2GiB make test` passes.

## Source

TUI performance audit 2026-11-23, verified claim 2 (High). Report:
`docs/reviews/tui-perf-audit-2026-11-23.md`.
