# Hooks deferred findings — test-hygiene closeout

**Goal:** close the six test-hygiene findings deferred out of the archived hooks plan
(beads `apogee-apz.1`–`.6`). Every item is test-only; no production code changes.
Three beads mis-cite their file — the corrected locations are binding here.

**Date:** 2026-09-06 · **Status:** unexecuted · **sized for:** ~200k-context host

**Sources**
- `docs/plans/archived/2026-09-06 - 01 - hooks-plan.md` (the run that deferred these)
- `docs/adr/0073-hooks-are-observe-only-driver-side-reactions-to-engine-events.md`
- Base commit `f5315fc2`; all file:line references verified against it.

**Ratified design calls** (Airic Lenz, 2026-09-06, via /implement-plan Write mode)
- **apz.2 recast:** make both halves live — force real drops past `queueDepth`, assert `offCaller`.
- **apz.4 absence check:** shape regexp over the test's own hook names, plus a positive control.
- **apz.5 scope:** bind *both* sites — `TestDaemonFiringFiresHooks` and `headlessHooksAgainst`.
- **apz.1 / apz.6 scope:** close the whole class in each, not only the symbols the beads name.
- **apz.3 name:** `TestHeadlessDerivesTheFileChangedHookFromItsOwnRoster` (writer's call, mechanical).

**Bead path corrections** (the bead text is wrong; the plan is right)
- `apz.2` → `cmd/apogee/wire_settings_test.go:2992`, not `internal/hooks/runner_test.go`.
- `apz.3` → `cmd/apogee/headless_test.go:2398`, not `cmd/apogee/e2e_hooks_test.go`.
- `apz.5` → the unbound write is `cmd/apogee/e2e_hooks_test.go:294`; `headlessHooksAgainst`
  never touches `runOnce` today.

**Regression check (2026-09-06, f5315fc2):**
- `apz.1`: guard folded — the closed list of nine becomes a binding enumeration rule over the
  facade re-exports.
- `apz.2`: guard folded — arm the `default:` `t.Error` only after the emit loop, emit
  `2*queueDepth`; the item yields to the documented `Report` contract (`internal/hooks/runner.go:62–64`).
- `apz.3`: guard folded — the Acceptance grep is scoped to code so the plan document cannot
  match itself.
- `apz.4`: guard folded — the stated bite is false (`hook sink: dropped 3 events` contains the
  existing entry); the real gap is prose-pinning, proved on a renamed hook.
- `apz.6`: guard folded — the closed list of three becomes a binding enumeration rule over
  `externallyEdited`.

**Standing requirements**
- `skills: coding-standards`
- Closeout closes beads `apogee-apz.1`–`.6` and the epic `apogee-apz` with `bd close`.

**Out of scope**
- Any production (non-`_test.go`) code change; `internal/hooks/runner.go` is read, never edited.
- Hand-editing `.beads/issues.jsonl` — bd owns it.
- Version identifiers of any kind.

## 1. Guard every unguarded facade re-export (apogee-apz.1) — ✅ DONE (2026-09-06)

NOTES (2026-09-06): the item named nine symbols; the binding enumeration rule found 27
unguarded re-exports (18 types, 9 consts) and all 27 are guarded — the extra 18 are
`DelegationConfig`, `FloorConfig`, `ContextFilesReport`, `ContextFileNote`,
`ToolSummary`, `ReadSpan`, `ListedEntries`, `MatchedLines`, `DiffStat`,
`ChangedFiles`, `EditRegion`, `EditRegions`, `SearchHits`, `ToolCallEdit`,
`ToolResultEdit`, `SeatFallbackNote`, `DelegateReportBlock`, `TaskListFence`.

NOTES (2026-09-06): each new line was inserted at the slot mirroring its declaration
order in `apogee.go`; no existing guard line was moved or reordered.

NOTES (2026-09-06): bite proved — renaming `ApprovalPhase` in `apogee.go` made
`go test . -count=1` fail with `./example_test.go:70:11: undefined: apogee.ApprovalPhase`;
reverted, `apogee.go` is untouched in the diff.

NOTES (2026-09-06): `go vet ./...` fails in `cmd/apogee` (`e2e_hooks_test.go:67:42:
undefined: regexp`) from another item's in-flight edit to a file this item does not
own; `go vet .` on the changed package is clean.

**What.** `example_test.go` carries the facade compile guard in two blocks: type aliases as
`_ apogee.X` (lines 25–116) and re-exported consts/sentinels as `_ = apogee.X` (lines
132–211). Add each missing symbol to the block matching its kind — a type alias never takes
`= `. Types: `ApprovalPhase` (`apogee.go:287`, natural slot after `_ apogee.ApprovalDecision`,
line 63), `FloorGuardEvent` (`apogee.go:244`), `PruneEvent` (`apogee.go:246`). Consts:
`ApprovalRequested` (`apogee.go:291`), `ApprovalDecided` (`apogee.go:292`) — both beside the
existing Approval consts at lines 159–161 — plus `SubAgentStarted` (`apogee.go:257`),
`SubAgentFinished` (`apogee.go:258`), `WireDirectionRequest` (`apogee.go:263`),
`WireDirectionResponse` (`apogee.go:264`). The guard is a compile-time assertion; no new test
function, no behaviour change.

**Regression guard.** The nine symbols named are the write-time result of the rule, not its
boundary. Binding rule: EVERY exported symbol re-exported by `apogee.go` carries exactly one
guard line in `example_test.go`, in the block matching its kind — a type alias as `_ apogee.X`
in the 25–116 block, a const/var/sentinel as `_ = apogee.X` in the 132–211 block. Enumerate the
re-exports against the guard blocks at implementation time and guard every symbol the
enumeration finds, including any added since `f5315fc2`; the named nine are what the write-time
enumeration found, not the scope boundary.

**Files:** `example_test.go`

**Tests.** The guard itself, over the full enumeration. Prove it bites: temporarily rename
`ApprovalPhase` in `apogee.go`, confirm `go test . -count=1` fails to compile, revert. Re-run
the enumeration after the edit — every exported identifier declared in `apogee.go` that
`example_test.go` does not name — and it must return nothing.

**Acceptance.**
```
go vet ./...
go test . -count=1
```

**Commit.** `test(facade): guard every unguarded apogee re-export in the compile guard`

## 2. Make the Replace off-caller test assert something (apogee-apz.2) — ✅ DONE (2026-09-06)

NOTES (2026-09-06): emitted `2*hookQueueDepth` = 128 events, the item's binding formula; the section's parenthetical "(≥130)" is arithmetically inconsistent with it and was not followed. 128 leaves 63 real drops (64 queued + 1 parked in the gated Run).

NOTES (2026-09-06): `queueDepth` is unexported in `internal/hooks`, so the count enters the test as a local `hookQueueDepth = 64` const documented as mirroring `internal/hooks/runner.go:19`.

NOTES (2026-09-06): the report barrier is fed only from the off-caller arm, so releasing it IS the off-goroutine promise; `noteDrop`'s first, synchronous, on-caller report therefore cannot satisfy it. The `default:` arm's `t.Error` is armed by an `atomic.Bool` set just before `Replace`, exactly as the regression guard requires.

NOTES (2026-09-06): the package did not compile in the shared tree — concurrent items had `cmd/apogee/e2e_hooks_test.go` mid-edit (`undefined: regexp`). Build and acceptance were therefore run in a throwaway detached worktree at HEAD carrying only this item's file; the worktree was removed and the shared tree left untouched.

**What.** `cmd/apogee/wire_settings_test.go:2992`
`TestHookRunnerReplaceNeverReportsOnTheCallersGoroutine` passes vacuously: `Replace`
(`internal/hooks/runner.go:254`) drains the retired generation on a fresh goroutine and
`reportDrops` (`:394`) emits only when `dropped > 0`, so with no events emitted `Report` is
never called and both `select` arms are dead; `offCaller` is stored at `:3000` and read into
`_` at `:3017`, asserting nothing. Recast so both halves are live: add a gating `Executor`
double beside `recordingHookExec` (`:2861`) whose `Run` blocks on a `gate chan struct{}` until
closed; install it, emit more than `queueDepth` (64, `internal/hooks/runner.go:19`)
`domain.TurnEvent{Status: domain.StatusExchangeComplete}` values so the boot generation carries
real drops, call `Replace`, `close(caller)`, release the gate, then block on a report barrier
(a buffered channel the `Report` func sends its line to) with a `time.After(5 * time.Second)`
fail-fast. Keep the `default:` arm's `t.Error`, and add the missing positive assertion:
`offCaller.Load()` must be true — a report that never arrives is now a failure, not a pass.
Binding: the barrier is the report/executor channel, never a `time.Sleep`.

**Regression guard.** `noteDrop` reports the FIRST drop synchronously on the emitting goroutine
(`internal/hooks/runner.go:227` → `:238–241`) — the contract `Report`'s own doc pins at `:62–64`,
which this item yields to — so arm the `default:` arm's `t.Error` only after the emit loop (an
`atomic.Bool` set just before `Replace`), leaving only the drain's report judged for goroutine
identity. Name the count, not the threshold: emit `2*queueDepth` (≥130) events, because the queue
buffers `queueDepth` (`:328`) with one more in flight under the gate, so 65 would drop nothing and
the 5s barrier would fail the run.

**Files:** `cmd/apogee/wire_settings_test.go`

**Tests.** The recast test. Prove it bites: with the `offCaller` assertion in place but the
`2*queueDepth` emit loop removed, the test must FAIL.

**Acceptance.**
```
go test ./cmd/apogee -run '^TestHookRunnerReplaceNeverReportsOnTheCallersGoroutine$' -race -count=5
```

**Commit.** `test(hooks): give the Replace off-caller test a real barrier and a live assertion`

## 3. Rename the file-changed roster test into the hook family (apogee-apz.3) — ✅ DONE (2026-09-06)

**What.** `cmd/apogee/headless_test.go:2398` `TestHeadlessDerivesFileChangedFromItsOwnRoster`
is not matched by `Headless.*Hook` — the pattern the archived hooks plan's item 8 Acceptance
ran — so the file-changed roster case never ran under that command, though it passes when run
explicitly. Rename it to `TestHeadlessDerivesTheFileChangedHookFromItsOwnRoster`, which the
pattern matches, joining `TestHeadlessFiresAHookAtTheExchangeBoundary` (`:2368`) and
`TestHeadlessReportsAFailingHookOnStderr` (`:2434`) in the same file. Repo-wide the old name
has exactly two occurrences — the definition, and the bead text in `.beads/issues.jsonl` — so
the rename is confined to one line. Do not edit `.beads/issues.jsonl`.

**Regression guard.** Scope the verification grep so this plan document cannot match itself —
search Go sources and build files only, excluding `docs/` and `.beads/` (e.g.
`grep -rn 'TestHeadlessDerivesFileChangedFromItsOwnRoster' --include='*.go' --include='Makefile*' .`).
Item 3's own **What** text contains the old identifier, so the drafted repo-wide `--include='*.md'`
grep would always return a hit and fail Acceptance.

**Files:** `cmd/apogee/headless_test.go`

**Tests.** The renamed test, and the family pattern that must now select it.

**Acceptance.**
```
go test ./cmd/apogee -run 'Headless.*Hook' -count=1 -v
grep -rn 'TestHeadlessDerivesFileChangedFromItsOwnRoster' --include='*.go' --include='Makefile*' .
```
The `-v` output must list all three names; the `grep` must return nothing.

**Commit.** `test(headless): rename the file-changed roster test into the Headless.*Hook family`

## 4. Assert hook-report absence on the emission shape (apogee-apz.4) — ✅ DONE (2026-09-06)

NOTES (2026-09-06): `hookMarkers` keeps its name and its `APOGEE_HOOK`/event-literal entries; the
two report spellings move to the derived `hookReportPattern`, and the comment that named
`hookMarkers` (the unattended halves' conversation) was reworded to name both checks, as the item
permits.

NOTES (2026-09-06): the pattern is derived from constants, so `hookBlock`'s two inline names became
`hooksSinkName` and a new `hooksBellName`, and the smoke script's write reply became
`smokeWriteReply` — without that the "not from hardcoded prose" requirement cannot hold.

**What.** `cmd/apogee/e2e_hooks_test.go:45` `hookMarkers` is a six-spelling whitelist, so hook
text under a different wording — a queue-drop line, say — reaches the final frame unnoticed.
Every report the hooks path can put on screen begins `hook <configured name>`:
`internal/hooks/runner.go:240` (`hook %s: dropped 1 event (queue full)`), `:363`
(`hook %s (%s): %v`), `:397` (`hook %s: dropped %d events`), reaching the TUI via `Report` →
`Bridge.NotifyHook` (`internal/tui/bridge.go:141`) → `hookNoticeMsg` → `addEphemeralNote`
(`internal/tui/model.go:1925`). Replace the whitelist with a shape check derived from the hook
names the test itself configures — a `regexp.MustCompile` of `hook (sink|bell)[ :(]` built from
those names, not from hardcoded prose, so a renamed hook cannot silently un-arm it — retaining
`APOGEE_HOOK` and the `string(hooks.…)` event literals as separate contains-checks. Add a
positive control at each absence site: before asserting the silence, require a string the
successful run definitely emits (the stub model's reply in the frame; the reply on stdout for
the headless run), so an empty capture cannot pass as silence. Both sites: the frame check at
`:141–148` (`TestE2EHooksFireFromTheTUI`) and the stream twin at `:266–275`
(`TestE2EHooksFireFromAHeadlessRun`).

**Regression guard.** The stated bite cannot fire: `hook sink: dropped 3 events` CONTAINS the
existing entry `"hook sink"` (`:49`), and both configured names — `sink` (`:399`), `bell` (`:402`)
— are already whitelisted. The gap that is real is that the whitelist is pinned to PROSE, not to
the names the test configures, so prove the bite on a RENAMED hook, which the derived pattern
matches and the hardcoded `"hook sink"` spelling does not. Keep the name `hookMarkers` for the
retained `APOGEE_HOOK`/event-literal contains-list, or reword the comment at `:208–210` that
names it.

**Files:** `cmd/apogee/e2e_hooks_test.go`

**Tests.** The two recast absence sites. Prove the shape check bites: assert in the same file
that a RENAMED hook's line — `hook watcher: dropped 3 events` — matches the derived pattern where
the hardcoded `"hook sink"` spelling does not.

**Acceptance.**
```
go test ./cmd/apogee -run 'TestE2EHooks' -count=1
```

**Commit.** `test(hooks): assert hook-report absence on the emission shape, with a positive control`

## 5. Bind and restore runOnce at both dependent sites (apogee-apz.5) — ✅ DONE (2026-09-06)

NOTES (2026-09-06): `TestDaemonFiringFiresHooks` keeps the first half of its comment (the harness
installs a stub runner; this test wants the composition) and drops only the sentence deferring to
the harness's cleanup, as the item asks.

**What.** Depends on item 4 (same file). `runOnce` (`cmd/apogee/headless.go:92`) is the
package-level seam onto `run.Once`. `cmd/apogee/e2e_hooks_test.go:294–296`, in
`TestDaemonFiringFiresHooks`, assigns `runOnce = run.Once` without capturing a prior value,
leaning on `cmd/apogee/daemonfire_test.go:64–71`'s harness cleanup — correct today only through
test ordering, latent under `go test -shuffle`. Give it the in-repo idiom
(`cmd/apogee/headless_test.go:2226–2229`): `prev := runOnce; runOnce = run.Once;
t.Cleanup(func() { runOnce = prev })`, and drop the comment that defers to the harness. Give
`headlessHooksAgainst` (`:366`) its own binding of the same shape, so its claim to drive the
production `run.Once` rests on its own code rather than on whatever ran before it; update its
doc comment at `:364–365`, which currently states it leaves `runOnce` untouched.

**Files:** `cmd/apogee/e2e_hooks_test.go`

**Tests.** The two bound sites, run under shuffle.

**Acceptance.**
```
go test ./cmd/apogee -run 'TestE2EHooks|TestDaemonFiringFiresHooks' -count=1 -shuffle=on
```

**Commit.** `test(hooks): bind and restore runOnce at both sites that depend on it`

## 6. Complete the external-edit key list (apogee-apz.6)

**What.** `cmd/apogee/settingsrows_test.go:550–551`, in
`TestSettingsRowsPointReadOnlyKeysAtTheirEditor` (`:511`), pins an explicit list of registry
keys whose row opens `$EDITOR`. Three keys satisfy `externallyEdited`
(`cmd/apogee/settingsrows.go:310–311`) yet are absent — `hooks`
(`internal/config/registry.go:345`), `sub-agents-server` (`:205`) and `tools.enabled` (`:371`),
none of which sets `Editable` or `GlobalOnly`. Add all three to that literal. Keep the list
literal: the enclosing test already asserts the general
`ExternalEdit == (EditPointer == pointerExternalEdit)` invariant over all rows at `:533–536`,
so a list derived from the same predicate would be tautological against the code it guards.

**Regression guard.** The three keys named are the write-time result of the rule, not its
boundary. Binding rule: EVERY registry key in `internal/config/registry.go` for which
`externallyEdited` (`cmd/apogee/settingsrows.go:310–311` — `!k.Editable && !k.GlobalOnly &&
k.Path != settingKeyMechanisms`) returns true must appear in the literal list at
`cmd/apogee/settingsrows_test.go:550–551`. Enumerate the registry at implementation time and add
every key the enumeration finds, not only `hooks`, `sub-agents-server` and `tools.enabled`.

**Files:** `cmd/apogee/settingsrows_test.go`

**Tests.** The extended list, over the full enumeration. Prove it bites: temporarily set
`Editable: true` on the `hooks` row in `internal/config/registry.go`, confirm the test fails,
revert. Re-run the enumeration afterwards: no key `externallyEdited` accepts may be missing from
the literal.

**Acceptance.**
```
go test ./cmd/apogee -run '^TestSettingsRowsPointReadOnlyKeysAtTheirEditor$' -count=1 -v
```

**Commit.** `test(settings): add hooks, sub-agents-server and tools.enabled to the external-edit list`
