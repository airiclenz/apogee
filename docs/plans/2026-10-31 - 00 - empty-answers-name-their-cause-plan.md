# Empty answers name their cause

**Goal:** `apogee undo`, `--resume` and `--continue` say why they came up empty and name the apogee home they read, so an unattended Driver pointed at the wrong home, id or snapshot setting can see which one it is. A read-only watch of a live session shows the watched run's context gauge.
**Date:** 2026-10-31
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 1b1294b1
**Closes:** undo-empty-unexplained
**Sources:**
- Issues `undo-empty-unexplained`, `watch-context-gauge-missing` (`capcom show <id>`)
- ADR 0074 decision 2 (a thinner undo answer names its reason); ADR 0103 decision 5 (watching a live session)
- `docs/manual/headless.md`, `docs/manual/sessions.md`, `docs/manual/configuration.md`, `docs/manual/commands.md`

**Ratified design calls:**
- **No-index answer** (owner, 2026-10-31): two messages, told apart by whether the session store holds a record for the id.
- **Empty-journal answer** (owner, 2026-10-31): `session <id>: ` followed by the `undo.NothingLines("")` line, taken from that function; the verb still exits non-zero.
- **Siblings** (owner, 2026-10-31): `--resume` and `--continue` misses name the sessions dir they read.
- **Watch gauge** (owner, 2026-10-31): a read-only watch shows the gauge; the acceptance of `watch-context-gauge-missing` is the spec.

**Standing requirements:**
- skills: coding-standards
- Test-run rules for this machine (pass them to every sub-agent): run only `GOMEMLIMIT=2GiB go test -count=1 -run '<pattern>' ./<one pkg>/`. Never `-race` or `-cover` over a whole package. Never two test runs at once.
- Every user- and model-facing string not named in an item stays byte-identical.

**Regression check (2026-10-31, 1b1294b1):**
- 1 — guard folded (every new or renamed test keeps the `TestUndoVerb` prefix)
- 2 — recast (writer decision: the `--no-save` paragraph names item 1's no-record answer; scope extended to `internal/config/defaults/config.yaml`)
- 3 — guard folded (the sessions root reaches the builders through `wire_live.go` or a `Store.Dir()` accessor)
- 4 — guard folded (one window helper for the gauge and `/usage`; the fixture's signature stays)
- 2 — re-check round: guard folded (the round-1 decision paste replaced by a clean guard; headless.md must carry item 1's no-record answer and every site must name `<home>/snapshots`; round 1's two dropped Acceptance greps restored; the `config.yaml` edit stays comment-only)

**Out of scope:**
- Persisting the snapshot reason (`run.Result.UndoNote`) into the session record.
- The `.lock` file `session.Store.Hold` creates for an unknown id.
- `mcp logout`'s no-token message; ADR 0074 decision 1's `~/.apogee/snapshots/` spelling (correct for the default home).

## 1. apogee undo names the cause and the home of an empty answer

**What:**
Fix for `undo-empty-unexplained`: every empty answer from `apogee undo` is today the bare `nothing to undo for session <id>`.
**Goal:** `apogee undo <id>` answers each empty case with its own error, `<home>` being the resolved apogee home path the verb read:
- no undo index, and the session store holds a record for `<id>`: `session <id> was recorded without undo snapshots (undo-snapshots off, or no git when it ran) — nothing under <home>/snapshots`
- no undo index and no record: `no session <id> under <home> — check the id, or pass --config / APOGEE_CONFIG for the home the run used`
- an index whose journal has no step left (a run that wrote nothing, or every exchange undone): `session <id>: ` + `undo.NothingLines("")[0]`, built from that function.
`apogee undo --help` gives the store as `<home>/snapshots` with `--config` / `APOGEE_CONFIG` choosing the home (default `~/.apogee`), and its sentence on sessions recorded without snapshots says what the verb now answers.
**Regression guard.** Every new or renamed test in this item keeps the `TestUndoVerb` prefix (e.g. `TestUndoVerbHelpNamesTheHomeSnapshots`, `TestUndoVerbOnAnUnknownSessionNamesTheHome`), so the Acceptance `-run 'TestUndoVerb'` runs each of them, the `--help` check included.
**Approach (assumed at the header base):** in `cmd/apogee/undo.go`, the `errors.Is(err, snapshot.ErrNoIndex)` branch of `runUndoVerb` asks `session.NewStore(roots.sessions).Load(id)`; `errors.Is(err, fs.ErrNotExist)` means no record, any other outcome means a record exists. Two error builders replace `undoNothingToDo` for that branch; the three empty-journal sites (`previewUndoVerb`, `applyUndoVerb` on `undo.ErrNothingToUndo`, `undoVerbMoved`) share one builder over `undo.NothingLines`. Rewrite the `Long:` text of `newUndoCommand` to match.
**Files:** cmd/apogee/undo.go; cmd/apogee/undo_test.go
**Read first:** cmd/apogee/undo.go — runUndoVerb, undoNothingToDo, previewUndoVerb, applyUndoVerb, undoVerbMoved, newUndoCommand; internal/undo/notes.go — NothingLines; internal/session/store.go — Store.Load
**Tests:** in `cmd/apogee/undo_test.go`, every name prefixed `TestUndoVerb`: no-index-no-record under a `--config` home (asserts the exact message with that home's path; renames `TestUndoVerbOnAnUnknownSessionSaysNothingToUndo` to e.g. `TestUndoVerbOnAnUnknownSessionNamesTheHome`); no-index-with-record (seed `<home>/sessions/<id>.json` through `session.Store`, no snapshot store); a journal walked back to its start (the second confirm in `TestUndoVerbPreviewsThenReverts`); a help test (e.g. `TestUndoVerbHelpNamesTheHomeSnapshots`) asserting `<home>/snapshots` and no `~/.apogee/snapshots`.
**Acceptance:**
- `go build ./cmd/apogee/ && go vet ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestUndoVerb' ./cmd/apogee/`
- `grep -n 'nothing to undo for session' cmd/apogee/` prints nothing
**Commit:** `fix(undo): name the cause and the home of an empty apogee undo answer`

## 2. The manual spells the snapshot store as <home>/snapshots

**What:**
Recast at the regression check (2026-10-31).
Depends on item 1.
**Goal:** no page under `docs/manual/`, and no comment in `internal/config/defaults/config.yaml`, gives the snapshot store as a fixed `~/.apogee/snapshots`; each names `<home>/snapshots` with the home chosen by `--config` / `APOGEE_CONFIG` (default `~/.apogee`). `docs/manual/headless.md` §undo says what `apogee undo` answers for a session recorded without snapshots and for an id it cannot find, in item 1's wording.
**Regression guard.** `docs/manual/headless.md`'s `--no-save` paragraph says that `apogee undo` on such a run's id answers with item 1's no-record message, since the run leaves neither a record nor an undo index; item 1's wording stays as ratified. The `internal/config/defaults/config.yaml` edit is comment-only, and no rewrapped comment line may start `# <registry-key>:` (`templateKeyLine` would read it as a key mention). headless.md must carry item 1's no-record answer (`check the id, or pass --config`) and each of `configuration.md`, `commands.md` and `config.yaml` must name `<home>/snapshots`, not merely drop `~/.apogee/snapshots`.
**Approach (assumed at the header base):** edit the `undo-snapshots:` entry in `docs/manual/configuration.md`, the `/undo` section of `docs/manual/commands.md`, the "undo with:" paragraph of `docs/manual/headless.md` (its "has nothing to revert here and says so" sentence) and its `--no-save` paragraph, and the `undo-snapshots:` comment in `internal/config/defaults/config.yaml`. Every page that states the store's location is in scope: `grep -rn 'apogee/snapshots' docs/manual/ README.md internal/config/defaults/` finds them.
**Files:** docs/manual/configuration.md; docs/manual/commands.md; docs/manual/headless.md; internal/config/defaults/config.yaml
**Read first:** docs/manual/headless.md — "`apogee undo <session-id>` is that revert" paragraph, `--no-save` paragraph (records nothing), `saved`/`undo_note` paragraph; docs/manual/commands.md — /undo "It covers the whole workspace" paragraph;
docs/manual/configuration.md — `undo-snapshots:` paragraph; internal/config/defaults/config.yaml — `undo-snapshots:` comment block;
internal/config/defaults_test.go — TestEmbeddedDefaultConfigSetsOnlyTheSystemPrompt, templateKeyLine
**Tests:** none (docs and a template comment only).
**Acceptance:**
- `grep -rn '~/.apogee/snapshots' docs/manual/ README.md internal/config/defaults/` prints nothing
- `grep -n 'recorded without undo snapshots' docs/manual/headless.md` prints a line
- `grep -n 'no session <id> under' docs/manual/headless.md` prints a line
- `grep -n '<home>/snapshots' docs/manual/configuration.md docs/manual/commands.md` prints a line
- `grep -n 'check the id, or pass --config' docs/manual/headless.md` prints a line
- `grep -n '<home>/snapshots' docs/manual/configuration.md docs/manual/commands.md internal/config/defaults/config.yaml` prints at least one line per file
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestEmbeddedDefaultConfig' ./internal/config/` passes (`TestEmbeddedDefaultConfigSetsOnlyTheSystemPrompt` parses the embedded `config.yaml` through `LoadFileConfig`)
**Commit:** `docs(manual): spell the undo snapshot store as <home>/snapshots`

## 3. --resume and --continue misses name the sessions dir they read

**What:**
Fix: both misses hide which apogee home they searched, so a wrong `--config` / `APOGEE_CONFIG` reads as a wrong id.
**Goal:** with `<sessions>` the resolved `<home>/sessions` path:
- `--resume <id>` that is neither a known id nor a readable file: `apogee: --resume "<id>": not a known session id under <sessions> (see /sessions) nor a readable session file`
- `--continue` with nothing saved for the workspace: `apogee: no saved sessions for this workspace (<ws>) under <sessions> — start one, or resume another with --resume <id> (see /sessions)`
**Regression guard.** `session.Store` exposes no root (no `Dir` accessor), so the sessions root reaches the builders either as a new `resolveResume` parameter — then `cmd/apogee/wire_live.go` passes `w.roots.sessions` at its `resolveResume` call and every `resolveResume` (20) and `resolveContinue` (3) call in `wire_session_test.go` is updated — or through a new `Store.Dir()` accessor in `internal/session/store.go`. Either way `cmd/apogee` compiles at the item's commit.
**Approach (assumed at the header base):** `resolveResumeArg` and `resolveContinue` in `cmd/apogee/wire_session.go` build these errors; pass them the sessions root they already search.
**Files:** cmd/apogee/wire_session.go; cmd/apogee/wire_session_test.go; cmd/apogee/wire_live.go (parameter route) or internal/session/store.go (accessor route)
**Read first:** cmd/apogee/wire_session.go — resolveResume, resolveResumeArg, resolveContinue; cmd/apogee/wire_live.go — the resolveResume call beside session.NewStore(w.roots.sessions);
cmd/apogee/root_test.go — newRootCommand; cmd/apogee/testsupport_test.go — upstreamHome; cmd/apogee/wire_session_test.go — TestResolveResumeMissingArg; docs/manual/sessions.md — the `--continue` refusal sentence
**Tests:** one test per message driving the root command with `--config <tmp home>` and asserting the exact text with that home's sessions path.
**Acceptance:**
- `go build ./cmd/apogee/ && go vet ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'Resume|Continue' ./cmd/apogee/`
**Commit:** `fix(cli): name the sessions dir a --resume or --continue miss searched`

## 4. A watch shows the watched run's context gauge

**What:**
Fix for `watch-context-gauge-missing`: the watch starts with zeroed live stats, never reads the record's fill, and measures against the watcher's own window.
**Goal:** in a read-only watch (`internal/tui/watch.go`), the status line's gauge lights from the watched record's `Meta.CtxUsed` on entry; a depth-0, non-maintenance journal `usage` line moves it, and its `context_window`, when non-zero, is the gauge's limit for the rest of the watch, the watcher's own `opts.ContextWindow` being the fallback. Maintenance and sub-agent readings leave the gauge as they do outside a watch. esc restores the watcher's own reading and window exactly as parked; the watcher's own sessions never see the watched window.
**Regression guard.** `/usage` stays live in a watch (`watchVerbs`) and its main row reads `m.opts.ContextWindow` (`usageRows`), so route the top-level limit through one `Model` helper — the watched window while `watch.isActive`, else `opts.ContextWindow` — read by both `contextGauge` and `usageRows`' main row, and update `contextGauge`'s doc comment. Keep `newWatchFixture`'s signature (`sessions_test.go` calls it 9 times): seed `CtxUsed` with a separate helper or by rewriting the fixture's record in the new test, with head 3 over `recordHeadEntries()` — at head 0 `loadWatchStart` never reads the record.
**Approach (assumed at the header base):** `loadWatchStart` / `recordHead` also return `Meta.CtxUsed`, and `paintWatchStart` sets `m.ctxUsed` after `clearWatchView`. A watched-window field on `watchState`, set in `foldWatchedEvent` from top-level non-maintenance usage events with `ContextWindow > 0`, is the limit `Model.contextGauge` uses while `watch.isActive`. `leaveWatch` resets `watchState`; `parkedSession.stats` already restores `liveStats`.
**Closes:** watch-context-gauge-missing
**Files:** internal/tui/watch.go; internal/tui/model.go; internal/tui/usage.go; internal/tui/watch_test.go
**Read first:** internal/tui/watch.go — paintWatchStart, loadWatchStart, recordHead, foldWatchedEvent, watchState; internal/tui/model.go — contextGauge; internal/tui/usage.go — usageRows;
internal/tui/watch_test.go — newWatchFixture
**Tests:** in `internal/tui/watch_test.go`: drive a watch over a record with `CtxUsed` (head 3 over `recordHeadEntries()`, `newWatchFixture`'s signature unchanged) and a journal whose `usage` line carries a `context_window` different from the watcher's; assert the gauge text in the rendered status line before the line folds, after it folds, and after esc, and that `/usage`'s main row measures against the same window as the gauge while watching.
**Acceptance:**
- `go build ./internal/tui/ && go vet ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'Watch' ./internal/tui/`
**Commit:** `fix(tui): light the context gauge in a watch from the watched run`
