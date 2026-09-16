# Host, config and TUI deepening (2026-09-16 architecture review, plan A) — plan

**Goal:** land the host-, config- and TUI-side candidates of
`docs/reviews/architecture-review-2026-09-16.html` that survived fact-checking, reduced to what the
code and the ADRs support: two confirmed defects fixed first; `liveSettings` as boot + one live
overlay (1); one Options→Config projection (2); one Firing account with `Wrote`/undo on `Outcome`
(7); a stored-session journal opener (21); the registry row's `Set` (9); one worker launch (5); the
legend derived at paint (12); repaint as a consequence of `Update` (10); the `/settings` step table
(11); name-keyed card facts on the registry row (24). Sibling plan B (`2026-09-16 - 01`) holds the
engine, tools and provider half and is gated on this plan archiving.

**Date:** 2026-09-16
**Status:** unexecuted
**sized for:** ~200k-context host

**Regression check (2026-09-16, 1da6d6fb):**
- 2: recast — the `registry.go` comment ride-along dropped (decision); guards folded (config.go's two live-fold comments; the test gains a refusal table).
- 3: guard folded — acceptance grep spelled on the `s` receiver; the context-files names' home while off stated.
- 4: guard folded — the holder is constructed before the `seedReactions` seed.
- 5: guard folded — `rebindInputs(bound upstreamBinding)` signature; the three outside callers added to Files.
- 6: guard folded — the two `openSessionJournal` test literals gain `cfg: apogee.Config{UndoSnapshots: true}`.
- 7: guard folded — every `newDelegationWiring` caller by grep (wire_settings_test.go added); `respondDroppingThinkingOff` and `requestTimeout` stay.
- 8: acceptance grep reworded as the summed non-test count (decision).
- 10: guard folded — acceptance grep excludes tests; `OpenStored`'s missing-index contract stated.
- 11: guard folded — `cursor-shape` exempt from the defaults pin; the live-apply wording change named.
- 12: guard folded — the one refusal-sentence rule for items 11/12/14 (decision); round-trip fixture and exclusions; `mode`'s env-time refusal named.
- 13: guard folded — every `fromFile:` implementer (`reactions.go` added to Files) plus the test helpers.
- 14: recast — overlay written after the seam door returned, `a.live != nil` posture kept, pure-Options keys enumerated (decision); yields to the recordToolSet/setToolSet comments and item 3.
- 15: guard folded — state write ≤ 2 sites; `resumeRunning` lays out; cmd/apogee E2E acceptance line.
- 16: guard folded — spinner generation is Model-lifetime monotonic; test Files by rule + grep (ask_test.go dropped); cmd/apogee E2E acceptance line.
- 17: guard folded — the comment sweep is a rule + grep, not the closed file list; cmd/apogee E2E acceptance line.
- 18: recast — blink keyed with an open tool call, `layout()` alone on a miss, generation rule over every renderView input, settle's input-height half, the motion test's observable (decision); yields to spinner.go's flip-repaint decision.
- 19: recast — strip precondition (no editor change or pane open between), recall.go dropped, input-write layouts stay until item 18 (d) (decision); cmd/apogee E2E acceptance line.
- 20: guard folded — cmd/apogee E2E acceptance line; the `-popup-design` sentence dropped (decision).
- 21: recast — input-write rule, `refreshViewport` in runview.go never stripped, acceptance grep on phrases that exist, layout.md sentence (decision); yields to approval.go's layout-purpose comments (FOLLOW-UP-K, `draftRowsCeiling`) until item 18 (d); cmd/apogee E2E acceptance line.
- 22: guard folded — existing wrapper names and signatures kept over the table; cmd/apogee E2E acceptance line.
- 23: guard folded — `paint(m, row, rows) string` column; `settingsPaint` reads the probes off the table; cmd/apogee E2E acceptance line.
- 24: guard folded — the schema cross-check test retargeted and renamed; the task_list comment reworded.
- 14 (round 2, 4d128f7a): recast — the re-parse sites recounted as measured (23: `context-files.names`' ParseSettingList excluded as a hand-written apply, `cmd/apogee/wire_present.go`'s two `livePresentation.apply` parses included; wire_present.go added to Files) (decision).
- 18 (round 2, 4d128f7a): guard folded — `settle` is a no-op while `!m.ready` (the WindowSizeMsg arm lays out and stores the key on the first sized frame; `foldModeReport`'s guard); recast — states ONCE that every input-write and pane-open site is carried by settle's height half (decision).
- 21 (round 2, 4d128f7a): guard folded — `internal/tui/model.go` added to Files (comment sweep only: `keyClaimOrder`, `claimKey`, `freshenTranscriptClamp` docs); sites sized to the 14 that exist; recast — one strip rule shared with item 19, the input-write/pane-open calls carried by item 18 (d), never a second rule (decision).

## Authoritative sources

- `docs/reviews/architecture-review-2026-09-16.html` — fact-checked 2026-09-16 by six read-only
  explorers at `1da6d6fb`; **where an item disagrees with the review, the item wins**.
- ADR 0011 (single worker, C1/C4; View pure), 0033 D6, 0035, 0037 (2026-08-24/25 amendments), 0043,
  0044 D8, 0052, 0053 D3/D9, 0074 D8, 0076 A8; `docs/plans/archived/2026-08-23 - 00` call 3
  (`layout()` is the single setter of the widget height); `docs/plans/archived/2026-09-15 - 01`
  (verdict table — its denials stand).

## Review verdicts (fact-check 2026-09-16; HEAD 1da6d6fb)

| # | Candidate | Verdict |
|---|---|---|
| 1 | liveSettings one live Options | **IN** (items 3–5) — latches and the context-files pair stay hand-written |
| 2 | One Options→Config projection | **IN, reduced** (6) — ~22 shared keys; binding keys stay per site |
| 5 | One worker launch | **IN** (15–16) |
| 7 | One Firing account | **IN, reduced** (8–9) — headless keeps its exit ladder |
| 9 | Registry row Set | **IN** (11–14) — file pass stays yaml-typed; wordings unchanged |
| 10 | Repaint tail | **IN** (18–21) — strips gated on the motion invariant |
| 11 | /settings step adapter | **IN, reduced** (22–23) — pointer axis stays with bead apogee-agk |
| 12 | Legend derived at paint | **IN** (17) |
| 15 | FloorConfig keyed set | **DENIED** — plan `2026-09-15 - 01` item 11 + ADR 0076 D11; breaks `!=` on `Floor` |
| 21 | Undo stored opener | **IN** (10) |
| 24 | Card name-keyed facts | **IN** (24) — ask_user's result-time arm stays |
| 27 | Retirement table | **DENIED** — the sub-agents retirement is interactive; the live-fold defect is IN (2) |
| 28, 29a | Namer fold, delegation `base` | ride along (7) |

## Ratified design calls (owner, 2026-09-16)

- **Scope:** the table above and every promoted deferral; denied rows recorded here only.
- **Two plans by area;** plan B gated on this plan archiving.
- **Rebind (1):** `rebindInputs` reads the live overlay — a rebind sees what the session sees; named as a behaviour change with a test (item 5).
- **Registry Set (9):** every refusal sentence stays byte-identical — the row carries it; block-mapped keys re-run the block validator.
- **Repaint (10):** the tail never repaints on `tea.MouseMotionMsg`; strips (19–21) run only after item 18's invariant test is green.
- **ADR text:** dated in-place amendments, never rewrites.

## Standing requirements

- `skills: coding-standards`.
- Deviations land as a dated `NOTES:` line under the item.
- No item changes `VERSION`, a CHANGELOG release heading or a tag.
- Items cite symbols, never line numbers.

## Out of scope

- Candidates 15, 27 (denied); `liveSettings` latches (`observedWindow`, `effortDialect`, `entry*`) — not derivable from Options, stay; `/confine` keeps bypassing the holder (ADR 0037 2026-08-25 note); `mouse.go`'s settings comparisons (bead apogee-agk); the seven-key Floor list homed in `internal/domain` (bead to file at closeout); plan B's items.

---

## 1. `probeToolchainRoots` resolves `go` through the argv[0] fence

**What.** Defect (security, confirmed): `cmd/apogee/toolchain_roots.go` resolves `go` with bare
`exec.LookPath` and spawns it at boot — the one production exec site outside the fence every other
site uses (`security.ResolveProgram(look, name, root, box)`, cf. `internal/tools/diagnostics.go`
`diagnoseGo`). Replace the lookup with `security.ResolveProgram(nil, "go", workspace, nil)` so a
program resolving inside the workspace (an activated `.venv/bin` on PATH) is refused and the roots
come back nil. Existing tests plant the fake `go` in a separate `t.TempDir()` and stay green.

**Files:** `cmd/apogee/toolchain_roots.go`, `cmd/apogee/toolchain_roots_test.go`
**Read first:** cmd/apogee/toolchain_roots.go — probeToolchainRoots, toolchainProbeEnv, toolchainLibrary.start; internal/security/execsafety.go — ResolveProgram, RefuseExecFromWritablePath;
cmd/apogee/toolchain_roots_test.go — installFakeGo (plants in its own t.TempDir, takes no dir — the inside-workspace case needs a dir parameter), fakeGoRecords (fatals on a missing log; "no spawn" is os.Stat of the log);
internal/tools/diagnostics.go — diagnoseGo

**Tests.** New case: fake `go` planted INSIDE the workspace → nil roots, no spawn (record via the
fake's side effect file). Existing cases unchanged.

**Acceptance.**
- `go build ./cmd/apogee && go test ./cmd/apogee -run 'Toolchain'`
- `grep -n 'exec.LookPath' cmd/apogee/toolchain_roots.go` → no match.

**Commit:** `fix(cmd): resolve the boot go probe through the argv[0] fence`

## 2. A live re-read never folds the retired top-level keys

**What.** Recast at the regression check (2026-09-16). Defect (confirmed): `internal/config/configmigrate.go` `migrateLegacyConfig(…,
mayFoldReactions=false)` refuses `hooks:` but still folds, backs up and rewrites a file carrying the
retired top-level quadruple — contradicting `config.go`'s "every live re-read … refuses rather than
rewriting" and the caller comment at `settingsApplier.fileConfig` (`cmd/apogee/wire_settings.go`).
Fix: when `!mayFoldReactions && !lc.isEmpty()`, refuse exactly as the `hooks:` branch refuses —
bytes untouched, no backup, the same refusal wording the startup pass uses. Rename the parameter
`mayMigrate` to say what it gates. Ride-along: `internal/tui/settingsapply.go`'s stale
"internal/config imports this package" sentence reworded (neither imports the other since ADR 0043's
2026-08-21 amendment; the local parse stays).

**Regression guard.** Drop the `internal/config/registry.go` comment ride-along (the reviewer showed the sentence is true — it speaks of the row's Validate func, not the block validators); remove registry.go from Files; item 13 owns any comment change the file pass makes. `internal/config/config.go`'s `LoadFileConfig` doc ("A config still written in the retired schema is migrated here… This is the one place the loader can WRITE") and `ApplyConfig`'s "The one-time legacy migration announces itself the same way (LoadFileConfig)" record the live fold this item removes: reword both to name `parseConfigFile(…, true)` under `ResolveOptions` as the one migrating read. `TestLoadFileConfigLeavesTheRetiredKeysToStartup` is a `[]string` loop asserting `err == nil` for every input, so the refusing case cannot be "a table case" as it stands: the test gains a `{given, wantRefusal}` table (or a sibling `TestLoadFileConfigRefusesTheRetiredQuadrupleWithoutRewritingIt` pinned like `TestLoadFileConfigRefusesTheHooksBlockWithoutRewritingIt`).

**Files:** `internal/config/configmigrate.go`, `internal/config/configmigrate_test.go`,
`internal/config/config.go`, `internal/tui/settingsapply.go`
**Read first:** internal/config/configmigrate.go — migrateLegacyConfig, legacyRefusal, liveReactionsRefusal, legacyFileConfig.isEmpty; internal/config/config.go — parseConfigFile, LoadFileConfig, ResolveOptions (the "one reader that may MIGRATE" comment above its parseConfigFile call);
internal/config/configmigrate_test.go — TestLoadFileConfigLeavesTheRetiredKeysToStartup, TestLoadFileConfigRefusesTheHooksBlockWithoutRewritingIt, assertMigrationWroteNothing, writeMigrationConfig; cmd/apogee/wire_settings.go — settingsApplier.fileConfig; internal/tui/settingsapply.go — parseStallAfter

**Tests.** `TestLoadFileConfigLeavesTheRetiredKeysToStartup` becomes a `{given, wantRefusal}` table (or
gains the sibling `TestLoadFileConfigRefusesTheRetiredQuadrupleWithoutRewritingIt`) with an
`endpoint: http://x\n` case asserting `assertMigrationWroteNothing` and the refusal. Every
refusal-text test in `configmigrate_test.go` unchanged.

**Acceptance.**
- `go test ./internal/config -run 'Migrat|LoadFileConfig|Registry'`
- `go vet ./internal/tui`

**Commit:** `fix(config): a live re-read refuses the retired top-level keys instead of folding them`

## 3. `liveSettings` holds boot + one live overlay

**What.** `cmd/apogee/wire_settings.go`: `liveSettings` keeps `boot config.Options` (immutable) and
`now config.Options` (the overlay) under the existing lock, with one door
`update(func(*config.Options))` and `options()` returning `cloneOptions(now)` — the clone covers
the eight lists `optionsLocked` deep-copies today (ToolsDisabled, URLAllowHosts, URLDenyHosts,
Reactions, Servers, ModelProfiles, ContextFiles, SystemPrompt.Models). The 18 non-Generation mirror
fields and their setters are rewritten to write `now` (setters stay as thin named doors where a
seam commit must precede the write: `setToolSet`, `setReactionLanes` keep their call order);
`optionsLocked`'s 35 assignments go. The context-files pair (`contextFilesEnable` +
`contextFileNames`, two spellings of off) stays as one named side bool inside the holder — the
review's "named exception". Latches stay untouched (Out of scope). Generation mirrors (`gen`) are
item 4's — leave them in place here.

**Regression guard.** The holder's setters use the `s` receiver (`func (s *liveSettings) set…`, 24 at HEAD; the `l` spelling matches nothing), so the acceptance grep is spelled on the receiver the rewrite uses and both counts are recorded. `options() = cloneOptions(now)` alone drops today's off-collapse (`optionsLocked`: `ContextFiles` nil while off, the names while on) and `setContextFilesEnable(on)` returning the names kept aside — `TestLiveSettingsOptionsFollowEveryApply/context-files.enable` (len 0) and `/context-files.names` pin both, and a re-enable after off must reinstall the names. State where the names live while off: a side list beside the side bool with both setters writing `now.ContextFiles` (nil off / names on), or the names in `now.ContextFiles` plus the off-collapse kept inside `options()`.

**Files:** `cmd/apogee/wire_settings.go`, `cmd/apogee/wire_settings_test.go`
**Read first:** cmd/apogee/wire_settings.go — liveSettings, newLiveSettings, optionsLocked, options, setContextFilesEnable, setContextFileNames, setToolSet;
cmd/apogee/wire_settings_test.go — TestLiveSettingsOptionsFollowEveryApply

**Tests.** `TestLiveSettingsOptionsFollowEveryApply` (14 keys + `clobberOptions` deep-copy, its
`context-files.enable` and `context-files.names` rows included) stays green; `options()` still
returns copies.

**Acceptance.**
- `go build ./cmd/apogee && go test ./cmd/apogee -run 'LiveSettings|ApplySetting|Reload'`
- `grep -c 'func (s \*liveSettings) set' cmd/apogee/wire_settings.go` (or the receiver the rewrite uses) is lower than at HEAD~1 (record both counts in NOTES).

**Commit:** `refactor(cmd): liveSettings keeps boot plus one live overlay behind one door`

## 4. The Generation is derived from the overlay, not stored

**What.** Depends on item 3. The `gen apogee.Generation` mirror (11 fields) goes:
`setFloorGuard`, `setBypass`, `setContextFillNotice`, `setStepBudgetNotice`, `setReactionLanes`
write `now` and the holder exposes `generation() apogee.Generation` = `generationOf(now)` —
`floorFromOptions(now)` + `domain.SplitLanes(now.Reactions)` + the two notices + Bypass. The
Generation is still handed whole to `SetReactions` (ADR 0076 A8). `cmd/apogee/wire_live.go`'s seed
reads `w.live.generation()`; the seed literal in `newLiveSettings` goes. `optionsFromFloor` and
`TestOptionsFromFloorInvertsFloorFromOptions` are deleted; `floorFromOptions` and
`floorGuardFields` stay (the key-set test needs the map; `setFloorGuard` keeps its key→field write
— plan `2026-09-15 - 01` item 11's "ONE negation seam").

**Regression guard.** `cmd/apogee/wire_live.go` seeds `w.engine.seedReactions(w.hooks, apogee.Generation{…})` before it constructs `w.live = newLiveSettings(w.opts)`; reading `w.live.generation()` at the seed would RLock a nil `*liveSettings` — a panic on every boot. Name the move: construct the holder before the seed (it depends only on `w.opts`) or seed after it.

**Files:** `cmd/apogee/wire_settings.go`, `cmd/apogee/wire_live.go`, `cmd/apogee/wire_settings_test.go`, `cmd/apogee/wire_engine_test.go`
**Read first:** cmd/apogee/wire_settings.go — generation, generationLocked, setFloorGuard, floorGuardFields, floorFromOptions, setReactionLanes; cmd/apogee/wire_live.go — seedReactions call, newLiveSettings call

**Tests.** `TestFloorGuardTableMatchesTheConfigKeys`, `TestFloorRowAppliesOneGeneration`, the
`wire_engine_test.go` generation-equality tests and the four `generation()` Observe/Sync reads stay
green; a new test asserts `generation()` after `setBypass` + `setFloorGuard` equals the Generation
`SetReactions` received.

**Acceptance.**
- `go test ./cmd/apogee -run 'Floor|Generation|Bypass|Reaction|Notice'`
- `grep -n 'optionsFromFloor' cmd/apogee/*.go` → no match.

**Commit:** `refactor(cmd): derive the engine Generation from the live overlay`

## 5. Firings and rebinds read the overlay; every live key is asserted through `options()`

**What.** Depends on items 3–4. `firingBinding` and `rebindInputs` take `now`. **Behaviour change
(ratified):** `rebindInputs` today starts from the boot `w.opts`; it now starts from the overlay,
so a rebind after `/tools` or `/servers` carries the live lists — it stays a distinct projection
(entry-over-pin window and reserve). Add the six write-alone keys (`delegate-max-steps`,
`delegate-max-depth`, `delegate-max-tokens`, `delegate-timeout`, `working-window`,
`undo-snapshots`) as rows of `TestLiveSettingsOptionsFollowEveryApply` (boot value ≠ applied value).
Rewrite the holder's doc comment to the boot + overlay model ("the exception list" sentence goes).

**Regression guard.** `rebindInputs(base, bound)` has a production caller outside this item's first draft — `cmd/apogee/wire_verbs.go` `rootWiring.rebind` passes `w.opts` — and test callers in `cmd/apogee/wire_server_test.go` (`TestRebindInputsOverlayTheBoundUpstream`) and `cmd/apogee/modelprofile_test.go` beside `wire_settings_test.go`; "take now" either changes the signature or leaves `base` a dead parameter. Fix the signature to `rebindInputs(bound upstreamBinding)` and update every caller.

**Files:** `cmd/apogee/wire_settings.go`, `cmd/apogee/wire_verbs.go`, `cmd/apogee/wire_settings_test.go`, `cmd/apogee/wire_server_test.go`, `cmd/apogee/modelprofile_test.go`, `cmd/apogee/schedule_test.go`
**Read first:** cmd/apogee/wire_settings.go — rebindInputs, firingBinding, rebindSpecFor, recordToolSet; cmd/apogee/wire_verbs.go — rootWiring.rebind; cmd/apogee/schedule.go — scheduleWiring.fire;
cmd/apogee/wire_server_test.go — TestRebindInputsOverlayTheBoundUpstream; cmd/apogee/wire_settings_test.go — TestLiveSettingsOptionsFollowEveryApply

**Tests.** New: a `/tools` toggle then a rebind — `rebindInputs` carries the toggled list. Existing:
`TestScheduleFiringKeepsTheBootFenceAfterConfineOff`, `TestScheduleFiringCarriesTheSessionsSyncLane`,
the `firingBinding` and reload-projection tests.

**Acceptance.**
- `go test ./cmd/apogee -run 'LiveSettings|Rebind|ScheduleFiring|firingBinding'`

**Commit:** `refactor(cmd): Firings and rebinds read the live overlay; every live key tested through options()`

## 6. One `projectConfig` for the session and the Firing

**What.** Depends on item 4. New `cmd/apogee/wire_config.go`: `projectConfig(opts config.Options,
roots hostRoots, confiner, mode, skills) apogee.Config` fills the ~22 keys `resolveConfig`
(`wire_boot.go`) and `firingConfig` (`wire_firing.go`) spell identically (Mode, Bypass, ConfigDir,
WorkspaceDir, Confiner, ConfineToWorkspace, WebSearchEndpoint, Disabled/EnabledTools, URL lists,
Inspector, SecretEnvVars, ContextFiles, Skills, SkillLookup, ExtraReadRoots, VirtualReadRoots,
Context.CompactionEnabled/PruneToolResults, Delegation×4, Floor, the two notices, UndoSnapshots).
Both callers overlay only what differs: boot's seven human delegates and the binding-derived keys
(Endpoint, Model, APIKey, Profile, SystemPrompt, Context.MaxContextTokens/WorkingWindow/
ResponseReserveFraction/MaxOutputTokens) stay per site. `UndoSnapshots` becomes a shared key:
`resolveConfig` sets it too and `openSessionJournal` reads `w.cfg.UndoSnapshots` (the
`urlGuardWiring` test helper sets `w.cfg`). Call `projectConfig` after `hostToolchain.start`.

**Regression guard.** `TestOpenSessionJournalWithNoApogeeHome` and `TestOpenSessionJournalWithoutGit` (`cmd/apogee/wire_live_test.go`) build `&rootWiring{opts: config.Options{UndoSnapshots: true}, …}` with a zero `cfg` and never run `resolveConfig`; once `openSessionJournal` reads `w.cfg.UndoSnapshots`, `snapshot.OpenJournal` answers `reasonDisabled` first and both tests (which want "no apogee home" / "git not found") go red. Give those two literals `cfg: apogee.Config{UndoSnapshots: true}`, beside the `urlGuardWiring` helper's `w.cfg`.

**Files:** `cmd/apogee/wire_config.go`, `cmd/apogee/wire_boot.go`, `cmd/apogee/wire_firing.go`, `cmd/apogee/wire_live.go`, `cmd/apogee/wire_boot_test.go`, `cmd/apogee/wire_firing_test.go`, `cmd/apogee/wire_live_test.go`
**Read first:** cmd/apogee/wire_boot.go — rootWiring.resolveConfig, composeReadRoots; cmd/apogee/wire_firing.go — firingConfig; cmd/apogee/wire_live.go — rootWiring.openSessionJournal;
cmd/apogee/wire_live_test.go — urlGuardWiring, TestOpenSessionJournalWithNoApogeeHome, TestOpenSessionJournalWithoutGit; cmd/apogee/wire_firing_test.go — TestFiringConfigSetsEveryUnattendedField

**Tests.** The four paired tests and `TestEveryDriverHandsTheRosterRungsToTheConfig` stay (they
assert Driver outputs); `TestFiringConfigSetsEveryUnattendedField` unchanged;
`TestOpenSessionJournalWithNoApogeeHome` and `TestOpenSessionJournalWithoutGit` stay green through
their `cfg` literal; a new table test drives `projectConfig` once and asserts both Drivers carry its
keys.

**Acceptance.**
- `go test ./cmd/apogee -run 'ResolveConfig|FiringConfig|Roster|UndoSnapshots|Journal'`

**Commit:** `refactor(cmd): one projectConfig fills the keys both Drivers share`

## 7. Host trims: delegation `base`, the namers, the dead TUI seam

**What.** (a) `cmd/apogee/delegation.go`: drop `delegationWiring.base`, `newSubAgentServer`'s
unread `base`, the never-produced error return of `newDelegationWiring`, and the `base` parameter
threaded through `resolveFiringRouting` (`wire_firing.go`) and checked in `wire_live.go` — the
stage-2 reservation the 2026-09-08 NOTES kept is stale (ADR 0076 A2 ships the global file only).
(b) `cmd/apogee/title.go` + `naming.go`: one `namingCall(ctx, binding, timeout, req) (string,
error)` owning the client policy (`WithMaxRetries(0)`, request timeout, API key), the
drop-thinking-off re-send and `title.ErrTruncated`; both namers pick a binding + prompt and call it.
(c) `internal/tui/tui.go` `Options.Bypass` (unread) and `Engine.Close` (never called by the
package) go, with `cmd/apogee/wire_options.go`'s write and `fakeEngine.Close`.

**Regression guard.** `newDelegationWiring` has eleven callers, not nine (`delegation_test.go` ×8, `naming_test.go`, `wire_live.go`, `wire_settings_test.go`) — the rule is "every caller — `grep -n 'newDelegationWiring(' cmd/apogee/*.go`", and `cmd/apogee/wire_settings_test.go` joins Files. `TestTitleGeneratorDropsOnlyTheThinkingOffAsk` calls `respondDroppingThinkingOff` directly and `TestTitleGeneratorSurfacesADeadline` sets `wiring.requestTimeout` (`title_test.go`, not in Files): keep `respondDroppingThinkingOff` as the named function `namingCall` calls and keep `requestTimeout` a field on both holders (`namingCall` already takes `timeout`).

**Files:** `cmd/apogee/delegation.go`, `cmd/apogee/wire_firing.go`, `cmd/apogee/wire_live.go`, `cmd/apogee/delegation_test.go`, `cmd/apogee/naming_test.go`, `cmd/apogee/wire_settings_test.go`, `cmd/apogee/title.go`, `cmd/apogee/naming.go`, `cmd/apogee/wire_options.go`, `internal/tui/tui.go`, `internal/tui/seam_test.go`
**Read first:** cmd/apogee/delegation.go — delegationWiring, newDelegationWiring, newSubAgentServer; cmd/apogee/wire_firing.go — resolveFiringRouting; cmd/apogee/wire_live.go — rootWiring.wireSession (newDelegationWiring call);
cmd/apogee/title.go — titleWiring.generate, respondDroppingThinkingOff; cmd/apogee/naming.go — delegationNamer.NameDelegation; cmd/apogee/title_test.go — TestTitleGeneratorDropsOnlyTheThinkingOffAsk, TestTitleGeneratorSurfacesADeadline

**Tests.** `TestTitleGeneratorDoesNotRetry`, `…NamesATruncatedReply`, `…DropsOnlyTheThinkingOffAsk`,
`…SurfacesADeadline`, `…FallsBackAtMostOnce`, `TestDelegationNamerNamesATruncatedReply`; every
`newDelegationWiring` caller — `grep -n 'newDelegationWiring(' cmd/apogee/*.go` — drops `err`.

**Acceptance.**
- `go build ./... && go test ./cmd/apogee -run 'Title|Namer|Delegation' && go test ./internal/tui -run 'Seam|Engine'`
- `grep -n 'WithMaxRetries(0)' cmd/apogee/*.go | grep -v _test | wc -l` → 1.

**Commit:** `refactor(cmd): drop the stale delegation base, fold the two namers, retire the dead TUI seam`

## 8. One Firing account: `Outcome` gains `Wrote` and the undo verb

**What.** `internal/schedule/schedule.go` `Outcome` gains `Wrote []string` and `UndoCommand string`
(ADR 0033 D6 — passed-through data, same posture as `ContextAnomalies`). `cmd/apogee/wire_firing.go`
`firingOutcome` fills them, `UndoCommand` under the exact gate `undoVerbLine` applies (non-empty
`Wrote`, session id, `UndoNote == ""`). `daemonfire.go` and `schedule.go` share one
`firingRefusal(prefix, errNotStarted) string` (the byte-identical compose/offline stage map) and one
`partialRunSuffix(id) string` ("(partial run saved as %s)"); `headless.go` uses the suffix helper
and keeps its own exit ladder, printing ALL context notices above the `Turns==0` exit as today.
Every emitted sentence stays byte-identical: "changed — N file(s) this run:", "  undo with: apogee
undo <id>", "apogee: daemon: resolve the %q schedule's reactions/bindings: %w", "apogee: resolve the
firing's reactions/bindings: %w"; daemon lines stay `StripEscapesToLine`d.

**Files:** `internal/schedule/schedule.go`, `cmd/apogee/wire_firing.go`, `cmd/apogee/headless.go`, `cmd/apogee/daemonfire.go`, `cmd/apogee/schedule.go`, `cmd/apogee/daemonfire_test.go`, `cmd/apogee/headless_test.go`, `cmd/apogee/schedule_test.go`
**Read first:** cmd/apogee/wire_firing.go — firingOutcome, errNotStarted, stageCompose, stageOffline; cmd/apogee/headless.go — runHeadless, undoVerbLine, writtenFilesLines; cmd/apogee/daemonfire.go — daemonWiring.fire;
cmd/apogee/schedule.go — scheduleWiring.fire, firingSpend; internal/schedule/schedule.go — Outcome; cmd/apogee/daemonfire_test.go — TestDaemonFireReportsWhatTheRunDid; cmd/apogee/undo_test.go — TestDaemonFireLogsTheUndoVerb, TestHeadlessOffersTheUndoVerbForItsWrites

**Tests.** `TestDaemonFireReportsWhatTheRunDid` (DeepEqual Outcome — gains the two fields),
`TestDaemonFireLogsTheUndoVerb`, `TestDaemonFireLogsTheFilesTheFiringWrote`,
`TestHeadlessOffersTheUndoVerbForItsWrites`, `TestHeadlessReportsAnUndoTheVerbCanActuallyPerform`,
`TestHeadlessRefusesAServerThatAnsweredNothing`, `TestDaemonFireRefusesOnlyAServerThatAnsweredNothing`,
`TestHeadlessExitCodes`, `TestHeadlessFormatJSONFramesEveryExit`; new: `firingOutcome` fills
`Wrote`/`UndoCommand`, and empty `UndoCommand` when `UndoNote` is set.

**Acceptance.**
- `go test ./internal/schedule ./cmd/apogee -run 'Outcome|DaemonFire|Headless|ScheduleFiring|Firing'`
- the summed non-test count of 'partial run saved as' across cmd/apogee/*.go → 1.

**Commit:** `refactor(cmd): one Firing account — Outcome carries what the Firing wrote and how to undo it`

## 9. The in-session Firing block shows what an Auto Firing changed

**What.** Depends on item 8. `internal/tui/schedule.go` (`enrichWithFiring` and the block renderer)
renders `Outcome.Wrote` as the changed-files list and `Outcome.UndoCommand` as the undo line, in the
block's existing voice, after the anomalies; escapes stripped at the TUI's own seam as the anomaly
lines are. No line when `Wrote` is empty.

**Files:** `internal/tui/schedule.go`, `internal/tui/schedule_test.go`
**Read first:** internal/tui/schedule.go — toolView.enrichWithFiring, presentFiring, firingStats, firingFaultLine, firingRecordLine; internal/tui/schedule_test.go — TestScheduleFiringReportsTheContextFilesItCouldNotRead, TestScheduleFiringStripsEscapesInTheContextAnomalies; internal/schedule/schedule.go — Outcome

**Tests.** New golden/assertion: a Firing with two written paths and an undo command renders both
lines; one with `UndoNote` renders the files and no undo line. Existing
`TestScheduleFiringReportsTheContextFilesItCouldNotRead`, `…StripsEscapesInTheContextAnomalies`,
`…StatsReportWhatTheRunCost` unchanged.

**Acceptance.**
- `go test ./internal/tui -run 'ScheduleFiring|Firing'`

**Commit:** `feat(tui): the Firing block lists the files an Auto Firing wrote and the undo verb`

## 10. `snapshot.OpenStored` — the undo verb stops reading the index

**What.** `internal/snapshot/journal.go` gains `OpenStored(ctx, home, id string) (*undo.Journal,
reason string, err error)`: reads the session's own index (its recorded workspace is the workspace
— ADR 0074 D5 trivially), never `git init`s a store that has an index but no HEAD differently than
`OpenJournal` does today. `cmd/apogee/undo.go` loses `undoIndexFile`, `undoIndexWorkspace` and the
decode; the verb keeps its order — no index ⇒ "nothing to undo" BEFORE the git-availability check —
and its wordings ("undo snapshots unavailable: git not found", "names no workspace", "decode the
session's undo index"). `TestUndoVerbReadsTheWorkspaceFromTheIndex` moves to `internal/snapshot` as
a test of `OpenStored`. The two-line "journal, reason = undo.New(), err.Error()" fallback in
`wire_live.go` and `internal/run/run.go` stays (two lines, two Drivers — not worth a seam).

**Regression guard.** State the missing-index contract: today `undoIndexWorkspace` answers `""` for no index and the verb maps it to "nothing to undo for session %s" (pinned by `TestUndoVerbOnAnUnknownSessionSaysNothingToUndo`), while `OpenJournal`'s `!Available()` check runs before any index read. `OpenStored` stats the index BEFORE `Available()` and answers `(nil, "", nil)` (or a `snapshot.ErrNoIndex` sentinel) for none; `runUndoVerb` maps that to `undoNothingToDo`, keeping the corrupt-index and no-workspace wordings on `err`. The test files `wire_live_test.go`, `wire_session_test.go` and `undo_test.go` name `journal.json` and this item changes none of them — the acceptance grep excludes tests.

**Files:** `internal/snapshot/journal.go`, `internal/snapshot/journal_test.go`, `cmd/apogee/undo.go`, `cmd/apogee/undo_test.go`
**Read first:** cmd/apogee/undo.go — runUndoVerb, undoIndexWorkspace, undoNothingToDo; internal/snapshot/journal.go — OpenJournal, Dir, journalFileName; internal/snapshot/store.go — Open, Available;
internal/undo/persist.go — Load, Index; cmd/apogee/undo_test.go — TestUndoVerbReadsTheWorkspaceFromTheIndex, TestUndoVerbOnAnUnknownSessionSaysNothingToUndo

**Tests.** `TestUndoVerbOnAnUnknownSessionSaysNothingToUndo`, `TestUndoVerbWithoutGitNamesTheReason`,
`TestUndoVerbPreviewsThenReverts`, `TestUndoVerbRestoresAFileTheExchangeChanged`,
`TestUndoVerbHonoursTheApogeeConfigVariable`, `TestUndoVerbRefusesAWorkspaceFlag` unchanged.

**Acceptance.**
- `go test ./internal/snapshot ./cmd/apogee -run 'Undo|Journal|OpenStored'`
- `grep -n 'journal.json' cmd/apogee/*.go | grep -v _test` → no match.

**Commit:** `refactor(snapshot): a stored-session opener; the undo verb loses the index layout`

## 11. One delegate-timeout parser and a registry-wide defaults pin

**What.** `internal/config/registry.go`: export `ParseDelegateTimeout` (today unexported, shared by
the accessor and the validator); `cmd/apogee/wire_settings.go` `applyDelegateTimeout` calls it
instead of its own `time.ParseDuration` (one empty-value rule: `""` = default, as the parser says;
unreachable from the pane, which hands `row.Default`). New `TestRegistryDefaultsReadBackFromAnEmptyFile`
in `internal/config`: for every row with a non-empty `Default`, `LoadFileConfig` on an absent file
then `Read` equals `Default` through the row's own parse (durations compared as `time.Duration`, not
strings — `Read` prints `1m30s` for `ui.stall-after`'s `"90s"`). This pins the two spellings the
review found and every block-mapped default at once.

**Regression guard.** The pin exempts `cursor-shape` with the reason: its row declares `Default: "block"` but `Read` returns `o.CursorShape`, which an absent file resolves to `""` (`config_test.go` "cursor-shape and editor are file-only (default empty)"; `wantDefaults` has no `CursorShape`) — the renderer applies the default (`internal/tui/prompteditor.go`) and the pane's fallback rule shows it (`cmd/apogee/settingsrows.go`, pinned in `settingsrows_test.go`). Do NOT default it in `fromFile`: that would take the settingsrows fallback rule's only subject away. Named live-apply wording change: `applyDelegateTimeout` today refuses with `apogee: delegate-timeout is a length of time of 0 or more, not %q`; calling `ParseDelegateTimeout` swaps it for the parser's `apogee: invalid delegate-timeout %q: want a length of time like 2h or 30m, …` — unpinned and unreachable from the pane (Validate runs first). Under item 12's one refusal-sentence rule the row's sentence for `delegate-timeout` IS the parser's.

**Files:** `internal/config/registry.go`, `internal/config/registry_test.go`, `cmd/apogee/wire_settings.go`, `cmd/apogee/wire_settings_test.go`
**Read first:** internal/config/registry.go — parseDelegateTimeout, validateDelegateTimeout, defaultDelegateTimeoutText, KeyRegistry; cmd/apogee/wire_settings.go — applyDelegateTimeout;
internal/config/config_test.go — TestApplyConfigDelegateTimeout, wantDefaults; cmd/apogee/settingsrows.go — settingsRows; cmd/apogee/settingsrows_test.go — the cursor-shape "block" expectation

**Tests.** `TestApplyConfigDelegateTimeout`, `TestApplySettingOnAnEmptyValueResolvesTheBuiltInDefault`,
`TestRegistryModeDefaultIsTheLadderDefault` unchanged; the new pin (with `cursor-shape` exempt and the
reason in its comment).

**Acceptance.**
- `go test ./internal/config ./cmd/apogee -run 'Registry|DelegateTimeout|Default'`
- `grep -n 'time.ParseDuration' cmd/apogee/wire_settings.go` → no match.

**Commit:** `refactor(config): one delegate-timeout parser; every registry default read back from an empty file`

## 12. `Key.Set` — the inverse of `Read`

**What.** Depends on item 11. `internal/config/registry.go` `Key` gains `Set func(string, *Options)
error`, built per kind from the switch `configwrite_scalar.go` `renderSettingValue` already owns
(bool, int, duration, enum, list, string) — the per-row refusal sentence is carried on the row so
every wording stays byte-identical ("is a count of 0 or more", "want a boolean", …). Block-mapped keys (`ui.*`, `present.*`, `sessions.*`) edit one field of the
block then re-run the block validator (`UI.Validate` etc.). `config.go`'s three hand-written
`fromEnv` closures become `row.Set` for the three env keys, keeping `apogee: invalid APOGEE_BYPASS
%q: want a boolean` verbatim. New registry-wide round-trip test: for every editable row,
`Set(Read(o), &o2)` yields `o2 == o` on the row's field (compare Options fields, never strings), and
`Set` refuses exactly what `Validate` refuses.

**Regression guard.** ONE refusal-sentence rule for items 11/12/14 — the row carries the writer's sentence (the one renderSettingValue/Validate emit today) and the announcing sites (the env pass's "apogee: invalid APOGEE_X %q: " lead, the startup loader's lead) add their own lead around it, so every wording stays byte-identical. The round-trip test states its fixture — the Options `LoadFileConfig` of `everyKeyFileConfig()` resolves — and its exclusions: rows with nil `Set` (`context-files.enable`, whose `Read` derives from `len(o.ContextFiles) > 0` with no Options field of its own; the structured rows) are skipped, and `KindText` round-trips through `Text` (`system-prompt-text`'s `Read` is a line-count summary). `mode`'s `Set` refuses through `validateSettingMode` (ParseMode's sentence — "invalid" is what `TestApplySettingRefusesWhatItCannotApply` pins); the item names the earlier, wrapped env-time refusal: `mode`'s env value is carried raw today and refused later by `domain.ParseMode` (`wire.go`, `headless.go`), a validating `row.Set` in `fromEnv` refuses at `applyEnv` with the wrapped sentence instead.

**Files:** `internal/config/registry.go`, `internal/config/config.go`, `internal/config/configwrite_scalar.go`, `internal/config/registry_test.go`, `internal/config/config_test.go`
**Read first:** internal/config/registry.go — Key, KeyRegistry, validateSettingMode, validateSubAgentsChoice; internal/config/configwrite_scalar.go — renderSettingValue, ParseSettingList; internal/config/config.go — keyAccessor, applyEnv;
internal/config/config_test.go — TestKeyAccessorsBindDescribedKeys, TestApplyConfigBadBypassEnvErrors, everyKeyFileConfig; cmd/apogee/wire_settings_test.go — TestApplySettingRefusesWhatItCannotApply

**Tests.** `TestKeyAccessorsBindDescribedKeys`, `TestRegistryRowsProjectEveryValue`,
`TestRegistryValidateHooksSitOnEditableKeys`, `TestRegistryEnumValuesMatchParseSites`,
`TestApplyConfigBadBypassEnvErrors`, `TestDocsEnvBadValuesNameTheVariableAndTheValue`, the
`registry_test.go` accepted-values table unchanged; the round-trip test new (fixture and exclusions
as the guard states).

**Acceptance.**
- `go test ./internal/config -run 'Registry|Env|Bypass|RoundTrip'`

**Commit:** `feat(config): the registry row carries Set, the inverse of Read`

## 13. The file pass validates through the rows

**What.** Depends on item 12. `internal/config/config.go`: the three `ResolveOptions` validation
branches that call registry validators (`validateCursorShapeName`, `validateDelegateTimeout`,
`validateSubAgentsChoice`) move into the file pass — `fromFile` accessors return an error and
`LoadFileConfig` surfaces it with the startup wording unchanged. The file pass stays yaml-typed
(no string re-parse of decoded bools/ints); only string-spelled keys route through `row.Set`'s
parse. Consequence to name in the item's doc comment: a bad live-edited value now REFUSES on the
live re-read through the existing loader-notice path instead of silently resolving to the default
(the `parseDelegateTimeout` accessor swallowed its error) — consistent with `config.go`'s live
re-read contract. This item owns any comment change the file pass makes to `internal/config/registry.go`
(item 2 no longer touches that file).

**Regression guard.** `fromFile` gaining an error return changes every implementer's signature, and one lives outside the listed files: `projectReactions` in `internal/config/reactions.go` is the `reactions` row's `fromFile`. The rule is "every function assigned to a `fromFile:` field" (`grep -n 'fromFile:' internal/config/config.go`), which also names `fileSystemPrompt`, `fileContextFiles`, `filePresent`, `fileUI`, `fileSessions`, plus the test helper `resolveSources` and the two direct `applyFile(` calls in `config_test.go`.

**Files:** `internal/config/config.go`, `internal/config/reactions.go`, `internal/config/registry.go`, `internal/config/config_test.go`, `internal/config/registry_test.go`
**Read first:** internal/config/config.go — keyAccessor, applyFile, LoadFileConfig, parseConfigFile, ResolveOptions, fileUI; internal/config/reactions.go — projectReactions; internal/config/config_test.go — resolveSources, TestApplyConfigDelegateTimeout

**Tests.** Every `ResolveOptions` refusal test keeps its wording; new: a file with
`delegate-timeout: 5x` refuses at `LoadFileConfig` with the same sentence at startup and on a live
re-read.

**Acceptance.**
- `go test ./internal/config`

**Commit:** `refactor(config): the file pass refuses through the registry rows`

## 14. Live applies land through `row.Set`

**What.** Recast at the regression check (2026-09-16). Depends on items 5 and 12.
`cmd/apogee/wire_settings.go` and `cmd/apogee/wire_present.go`: the 23 re-parse sites, counted as
measured at 4d128f7a (`settingInt`/`settingBool` ×18 — 16 in `wire_settings.go`, 2 in
`wire_present.go`'s `livePresentation.apply`, the `present.auto-open` and `present.port` arms —
`ParseSubAgentsChoice`, `domain.ParseMode`, `ParseSettingList` ×2 (`tools.disabled` and the
url-safety hosts; `context-files.names`' `ParseSettingList` is a hand-written apply per this item's
guard and is not counted), the item-11 `ParseDelegateTimeout` call) parse through `row.Set` into a
scratch `Options` for pure-Options keys;
a seam commit precedes the write (item 3's order) and the apply keeps its own post-write
projection. Keys whose apply is not an Options edit (`/confine`, latches, the context-files pair,
the skills gates) stay hand-written and are listed in the holder doc. `settingInt`/`settingBool`
are deleted when no caller remains.

**Regression guard.** Parse through `row.Set` into a SCRATCH Options and write the overlay only AFTER the seam door returned (swap-door keys sub-agents-choice, tools.disabled, url-safety.* keep recordToolSet/recordSeatChoice order — "a seam commit precedes the write", matching item 3); keep the `a.live != nil` posture (reachesWithoutAMember / settingKeysWithNoMemberToReach) — mirror only when a holder is composed; enumerate the pure-Options keys the item converts, and list mode, use-project-skills, use-shipped-skills (seam = skills.Sources), /confine, the latches and context-files-enable among the hand-written applies. This yields to the documented decision it first reversed: `recordToolSet`'s comment ("called only after the door has RETURNED, so a refused swap leaves the overlay on the set the session is still running"), `setToolSet`'s comment, and item 3's "a seam commit must precede the write". Round 2 (4d128f7a): recount the re-parse sites — exclude `context-files.names`' ParseSettingList (a hand-written apply per its own guard) and include `cmd/apogee/wire_present.go`'s two sites, adding wire_present.go to Files; state the count as measured.

**Files:** `cmd/apogee/wire_settings.go`, `cmd/apogee/wire_present.go`, `cmd/apogee/wire_settings_test.go`
**Read first:** cmd/apogee/wire_settings.go — settingsTable, applySettingFor, recordToolSet, recordSeatChoice, setToolSet, applyFloorGuard, settingInt, settingBool; cmd/apogee/wire_present.go — livePresentation.apply;
internal/config/configwrite_scalar.go — renderSettingValue, validateSettingValue; internal/tui/settingswatcher.go — applyReloaded (the second apply caller, values from the file's own projection);
cmd/apogee/wire_settings_test.go — TestApplySettingRefusesWhatItCannotApply, TestApplySettingSubAgentsChoiceSwapRefusalKeepsTheGate, TestApplySettingAcceptsTheStartupOnlyKeys, settingKeysWithNoMemberToReach

**Tests.** `TestEveryEditableSettingKeyHasAnApply`, `TestSettingsTableIsInRegistryOrder`,
`TestLiveSettingsOptionsFollowEveryApply` (now 20 rows),
`TestApplySettingSubAgentsChoiceSwapRefusalKeepsTheGate` (a refused mid-run swap leaves the overlay
on the set the session runs), `TestApplySettingAcceptsTheStartupOnlyKeys` (applies through
`settingsApplier{}` with no holder) and every apply-refusal wording test unchanged.

**Acceptance.**
- `go test ./cmd/apogee -run 'Apply|LiveSettings|Setting'`
- `grep -c 'settingInt\|settingBool' cmd/apogee/wire_settings.go cmd/apogee/wire_present.go` → 0 for each.

**Commit:** `refactor(cmd): live applies land through the registry row's Set`

## 15. One launch verb: "an Exchange is in flight" is written once

**What.** `internal/tui`: `enterRunning(cmd tea.Cmd, cancel, box, activity)` and `resumeRunning()`
on `Model` perform every write the four launch sites (`launchExchange`, the two `/continue` arms,
`/compact` in `commandrun.go`) and the two decision returns (`approval.go` `sendApproval`, `ask.go`
`submitAnswer`) spell today — boundary, box, cancel, state, placeholder, activity, spin — in the
load-bearing order (`cacheBoundaryAtIdle` before `startExchange`; `installBox(nil)` before
`startCompact`; activity is a parameter: `/compact` passes `actCompacting`). Drift fix: the verb
commits the thinking board (`thinking.commitAll`) on every launch, closing the `/continue` and
`/compact` omission `finishWorker` masked. `finishWorker` and `worker.go` docs name the verb.

**Regression guard.** Two verbs (`enterRunning`, `resumeRunning`) both write the state — the resume half is `sendApproval` (`approval.go`) and `submitAnswer` (`ask.go`) today — so either give both one shared unexported tail that owns the write, or accept ≤ 2 sites, both in the verb's file (record which in NOTES). `resumeRunning`'s writes name `m.layout()` (resume half only): `sendApproval` and `submitAnswer` call it today so a draft the pane clamped grows back (`draftRowsCeiling`); no launch site lays out inside the verb — `/continue`'s canned arm and `/compact` lay out BEFORE it (`commandrun.go`).

**Files:** `internal/tui/commandrun.go`, `internal/tui/approval.go`, `internal/tui/ask.go`, `internal/tui/model.go`, `internal/tui/worker.go`, `internal/tui/thinking_test.go`
**Read first:** internal/tui/commandrun.go — launchExchange, runCommand ("continue" and "compact" arms); internal/tui/approval.go — sendApproval; internal/tui/ask.go — submitAnswer; internal/tui/model.go — finishWorker, stopWorker;
internal/tui/worker.go — startExchange, startResume, startCompact; internal/tui/sessionsave.go — cacheBoundaryAtIdle; internal/tui/thinking_test.go — TestThinkingBoardEndsAtEveryBoundary; internal/tui/interject_test.go — runningModel

**Tests.** `TestThinkingBoardEndsAtEveryBoundary` becomes table-driven over launch, `/continue`
(both arms) and `/compact`; `runningModel` precondition (box non-nil after ⏎), the interject ADR 0025
suite, approval/ask "back to running" tests, `TestE2E*` goldens unchanged.

**Acceptance.**
- `go test ./internal/tui -run 'Thinking|Running|Continue|Compact|Approval|Ask|E2E'`
- `go test ./cmd/apogee -run 'TestE2E'` (the E2E frame goldens live in `cmd/apogee`, not `internal/tui`).
- `grep -n 'm.state = stateRunning' internal/tui/*.go | grep -v _test` → ≤ 2 sites, both in the verb's file (record which in NOTES).

**Commit:** `refactor(tui): one launch verb writes the in-flight worker`

## 16. The in-flight worker is one value

**What.** Depends on item 15. `internal/tui`: `cancel`, `box` and the spinner generation move under
one `worker` value on `Model` (held by value — the Model is value-copied on every `Update`; no
`strings.Builder`, no self-pointer), with `start`/`resume`/`finish` as its verbs; the four-state
`state` enum stays on the Model and the CancelFunc stays reachable to the stop key (ADR 0011 C1/C4).
Readers (`interject.go`, `spinner.go`, `activity.go`, `model.go`) go through the value. Test pokes
(`m.state = stateRunning`, `m.cancel = func(){}`) become one `startStubWorker(t, m)` helper in
`interject_test.go` beside `runningModel`.

**Regression guard.** The spinner generation is a Model-lifetime monotonic counter: `start`/`resume` bump it, `finish` NEVER resets it — a `finish` that zeroed the worker value would let a natural completion's `flushAfterCompletion` re-arm from 0 onto the generation whose last tick is still in flight, and `foldSpinnerTick` would accept the stale tick (two chains, 2× spin — the bug `spinner.go`'s header says the generation exists to prevent). `TestSpinnerTickChainGeneration`'s retired-chain case stays green across a finish→start pair. Test Files are a rule, not a closed list: every `_test.go` that names `m.cancel`, `m.box` or `spin.gen`, or writes `m.state = stateRunning` — `grep -ln 'm\.cancel\b\|\.box\b\|spin\.gen\|m.state = stateRunning' internal/tui/*_test.go` (`ask_test.go` does not exist; ask tests live in `model_test.go`, `runview_test.go`, `fold_test.go`).

**Files:** `internal/tui/model.go`, `internal/tui/worker.go`, `internal/tui/interject.go`, `internal/tui/spinner.go`, `internal/tui/activity.go`, `internal/tui/interject_test.go`, `internal/tui/model_test.go`, `internal/tui/spinner_test.go`, and every other `_test.go` the guard's grep names
**Read first:** internal/tui/model.go — Model (cancel, box, spin fields), finishWorker, stopWorker; internal/tui/spinner.go — spinnerAnim, arm, tick, foldSpinnerTick; internal/tui/interject.go — installBox, flushAfterCompletion;
internal/tui/settingsapply.go — the spin.style / spin.color writes (stay on the Model); internal/tui/spinner_test.go — TestSpinnerTickChainGeneration; internal/tui/interject_test.go — runningModel; internal/tui/model_test.go — TestModelNoBuilderByValue

**Tests.** `TestModelNoBuilderByValue` (the guard), `TestSpinnerTickChainGeneration` (retired-chain case
across finish→start), the whole `internal/tui` suite unchanged in outcome.

**Acceptance.**
- `go test ./internal/tui`
- `go test ./cmd/apogee -run 'TestE2E'` (the E2E frame goldens live in `cmd/apogee`, not `internal/tui`).
- `grep -c 'm.cancel = func' internal/tui/*_test.go` → 0.

**Commit:** `refactor(tui): the in-flight worker is one value with three verbs`

## 17. The prompt legend is derived at paint

**What.** `internal/tui`: `legend() string` in `runview.go` derives the legend from state —
awaitingAsk → `idleLegend`; awaitingApproval → the running legend; running →
`legendFor(runningPlaceholder)`; idle/errored → `legendFor(idleLegend())` — with
`keyDisambiguation` and `viewedChild()` read per frame. `inputView` (`model.go`) sets `Placeholder`
on its LOCAL textarea copy as `vp.SetHeight` does today (ADR 0011: View stays a value receiver).
`setPlaceholder`, `setKeyDisambiguation`'s read-back, `fold.go`'s three-Event re-derivation and
the 12 call sites go; the construction seed in `prompteditor.go` goes with them. `doc.go`'s "state
the Model SETS, not a render-time choice" paragraph is rewritten to the derivation. No announced
string changes.

**Regression guard.** The comment rewrite is a rule, not the closed file list: every comment in `internal/tui` naming `setPlaceholder`, "render-time", "per frame" or "per-frame" that records the placeholder as state set on a transition — the `prompteditor.go` placeholder const block doc and `setPlaceholder`'s doc, `runview.go` `openRun`'s comment, `fold.go`'s three-event comment, and `doc.go`'s "legend funnel every setPlaceholder site routes through" sentence beside the SETS paragraph — is rewritten to the derivation. Sweep: `grep -n 'setPlaceholder\|render-time\|per frame\|per-frame' internal/tui/*.go` (comments about other per-frame derivations stay).

**Files:** `internal/tui/runview.go`, `internal/tui/prompteditor.go`, `internal/tui/fold.go`, `internal/tui/approval.go`, `internal/tui/ask.go`, `internal/tui/commandrun.go`, `internal/tui/model.go`, `internal/tui/doc.go`, `internal/tui/fold_test.go`, `internal/tui/runview_test.go`, `internal/tui/interject_test.go`, `internal/tui/model_test.go`, `internal/tui/prompteditor_test.go`, `internal/tui/subagentblock_test.go`
**Read first:** internal/tui/runview.go — legendFor, topLegend, openRun, backHint; internal/tui/prompteditor.go — childLegend, setPlaceholder, idleLegend, setKeyDisambiguation; internal/tui/fold.go — foldEvent (the three-event re-derivation);
internal/tui/model.go — inputView, finishWorker; internal/tui/fold_test.go — TestFoldSubAgentNamedEventReResolvesThePlaceholder; internal/tui/runview_test.go — TestRunViewPlaceholderNamesTheChild, TestRunViewDecisionPaneOwnsTheLegend

**Tests.** The 31 `m.input.Placeholder` assertions become `m.legend()` (or rendered-input)
assertions; `TestFoldSubAgentNamedEventReResolvesThePlaceholder` and `…LeavesABorrowedBoxAlone` are
recast as derivation tests (scrambling a stored value no longer means anything); the
`runview_test.go` legend table and `TestE2E*` goldens unchanged.

**Acceptance.**
- `go test ./internal/tui -run 'Legend|Placeholder|E2E|Fold|Interject'`
- `go test ./cmd/apogee -run 'TestE2E'` (the E2E frame goldens live in `cmd/apogee`, not `internal/tui`).
- `grep -n 'setPlaceholder' internal/tui/*.go` → no match.

**Commit:** `refactor(tui): the prompt legend is one derivation at paint`

## 18. Repaint is a consequence of `Update`: the dirty mark and the tail

**What.** Recast at the regression check (2026-09-16). `internal/tui`: a transcript generation
counter bumped by every `func (t *transcript)` that writes any field `renderView` reads — entries,
pending, root, taskListOpen (`place` and the ~15 in-place pointer-receiver entry mutators —
addToolResult, addSubAgentPhase/Name, appendToken, refreshStartup, stamp, park/unpark, the expanded
toggles — beside setRoot, reset, replay, setTaskListOpen, discardPending, takePending; grep
`func (t \*transcript)` to find them), plus a paint key {generation, theme id, width,
HideScrollbar, `blink && m.transcript.hasOpenToolCall()`, backHint} — every input of `renderView` —
stored by `refreshViewport`. `Update` gains one deferred `settle()` beside `reportActivity`: if the
key differs → `layout()` ALONE (it ends in `refreshViewport`, which stores the key); height half =
`freshenTranscriptClamp`, which ALSO lays out when `m.input.Height() != m.inputRows()`, with ONE
exemption, `tea.MouseMotionMsg` (ratified; motion moves the selection only and View overlays the
shade). `layout()` stays the single setter of the widget height and of the input-box height (plan
`2026-08-23 - 00` call 3). Stated once, here: every input-write and pane-open `layout()` site
the strip items (19, 20, 21) meet is carried by settle's height half — (d)'s
`m.input.Height() != m.inputRows()` plus the widget-height compare
(`viewport.Height() != transcriptWidgetRows()`) — not by the strip rule; items 19 and 21 share ONE
strip rule (item 19's) and this sentence is the only ground on which such a call goes. `settle` is a
no-op while `!m.ready`, beside the MouseMotionMsg exemption: the WindowSizeMsg arm lays out and
stores the key on the first sized frame. No call site is stripped here.

**Regression guard.** (f) `settle` is a no-op while `!m.ready` (the WindowSizeMsg arm lays out and stores the key on the first sized frame) — a Msg delivered before the initial WindowSizeMsg (bubbletea v2 sends it as `go p.Send(resizeMsg)`, racing Init's Cmd and the terminal's ModeReportMsg) must not find `viewport.Height()` 0 ≠ `transcriptWidgetRows()` 1 and lay out at width 0; `foldModeReport` (`internal/tui/width.go`) guards exactly this with `&& m.ready`. Round 2 (4d128f7a) decision: state ONCE, in item 18's What, that every input-write and pane-open site is carried by settle's height half (18 (d) `m.input.Height() != m.inputRows()` plus the widget-height compare), and items 19 and 21 share ONE strip rule — "strip only a call a generation-bumping transcript method precedes in the same arm with no editor write or pane open between" — so item 21's guard must reference that rule, never imply a second one. (a) blink enters the paint key only as `blink && m.transcript.hasOpenToolCall()`; (b) on a key miss call `layout()` ALONE (it ends in refreshViewport, which stores the key); (c) the generation rule is "every `func (t *transcript)` that writes any field renderView reads — entries, pending, root, taskListOpen", naming setRoot, reset, replay, setTaskListOpen, discardPending, takePending beside the entry mutators; (d) settle's height half ALSO lays out when `m.input.Height() != m.inputRows()` (layout() stays the single setter of the input-box height); (e) TestMouseMotionNeverRepaints names its observable — slice identity of `m.lines` and `m.viewport.Height()` before/after 1000 motion Msgs — and is described as an invariant pin whose bite is an injected key miss under motion. (a) yields to the documented decision at `spinner.go` `foldSpinnerTick` — the blink flip repaints only while a call is open ("re-rendering the whole scrollback ten to twenty times a second for an identical result would be work for its own sake").

**Files:** `internal/tui/model.go`, `internal/tui/transcript.go`, `internal/tui/render.go`, `internal/tui/paintcache.go`, `internal/tui/model_test.go`, `internal/tui/transcript_test.go`
**Read first:** internal/tui/model.go — Update (the reportActivity defer, the WindowSizeMsg/eventMsg arms), layout, freshenTranscriptClamp, refreshViewport, transcriptWidgetRows, inputRows, ready; internal/tui/render.go — renderView, renderLines; internal/tui/paintcache.go — blockKey;
internal/tui/transcript.go — transcript (fields entries/pending/streaming/pendingRun/root/taskListOpen), place, setRoot, reset, replay, setTaskListOpen, hasOpenToolCall; internal/tui/spinner.go — foldSpinnerTick; internal/tui/width.go — foldModeReport (the ready guard); internal/tui/mouse.go — handleMouseMotion;
internal/tui/settingsapply.go — applyColorScheme (theme swap + paints.clear), settingsApplyLocal (HideScrollbar arm); internal/tui/model_test.go — assertClampFresh, TestPaneHeightChangeReachesLayout, TestANewPaneClaimingAKeyStillReachesLayout, step

**Tests.** `TestPaneHeightChangeReachesLayout` and `TestANewPaneClaimingAKeyStillReachesLayout`
extended over eventMsg, interjectedMsg, a click and a settingsapply arm; new invariant
`TestMouseMotionNeverRepaints` — an invariant pin: 1000 motion Msgs with the settings pane open leave
`m.lines` the same slice (identity) and `m.viewport.Height()` unchanged; its bite is an injected key
miss under motion, not HEAD — `TestSettleWaitsForTheFirstWindowSize` (a Msg folded before any
WindowSizeMsg leaves `m.lines` and `m.viewport.Height()` untouched — no width-0 layout) and
`BenchmarkUpdateMotionWithSettingsOpen`. `assertClampFresh`,
`TestPublishedFrameSpansMatchAFreshComposition`, `TestTranscriptLayoutGolden`, `TestE2E*` unchanged.

**Acceptance.**
- `go test ./internal/tui -run 'Layout|Repaint|Motion|Clamp|Golden|E2E'`
- `go test ./internal/tui -run xxx -bench MotionWithSettingsOpen -benchtime 1000x` (record ns/op in NOTES).

**Commit:** `refactor(tui): Update settles layout and repaint from a dirty mark`

## 19. Strip the transcript-mutating arms

**What.** Recast at the regression check (2026-09-16). Depends on item 18 (its invariant test green,
and its (d) input-height half in). Remove the explicit `m.layout()` / `m.refreshViewport()` calls
that follow a transcript mutation in `commandrun.go`, `interject.go`, `model.go`, `autotitle.go`,
`schedule.go`, `actuation.go`, `heartbeat.go`, `sessionsave.go`, `skillscmd.go`, `effort.go`,
`keymigration.go` (~50 sites; `recall.go` is not in scope — its two sites, `showRecall` and
`recallPastNewest`, follow editor changes only and mutate no transcript). Rule: a call is stripped
only when a generation-bumping transcript method precedes it in the same arm AND no input-editor
change (`SetValue`/`Reset`/insert/`restoreAskDraft`) or picker/pane open sits between, and nothing
after it in the same arm reads paint state (`m.lines`, `m.lineTargets`, `m.cursor`, `viewport.*`);
a call followed by such a read stays, with a one-line comment naming the read.
`refreshViewportAnchored` is never stripped (it positions). Tests that call an arm directly then
read paint state route through `step` or call `settle()` explicitly.

**Regression guard.** Strip only a call that a generation-bumping transcript method precedes in the same arm AND no input-editor change (SetValue/Reset/insert/restoreAskDraft) or picker/pane open sits between; drop recall.go from Files (its two sites follow editor changes only) and say so in the item; a `layout()` following a write to m.input, m.pending or m.pendingAsk STAYS with the comment "layout() is the single setter of the input box height" until item 18's (d) is in, after which it strips — state that dependency.

**Files:** `internal/tui/commandrun.go`, `internal/tui/interject.go`, `internal/tui/model.go`, `internal/tui/autotitle.go`, `internal/tui/schedule.go`, `internal/tui/actuation.go`, `internal/tui/heartbeat.go`, `internal/tui/sessionsave.go`, `internal/tui/skillscmd.go`, `internal/tui/effort.go`, `internal/tui/keymigration.go`
**Read first:** internal/tui/model.go — submit (reset → recordSend → addUser → layout), Update (eventMsg arm), finishWorker, foldLoopError, foldHookNotice; internal/tui/commandrun.go — runCommand (startNewSession, "continue", "compact", "skills" arms), runDeferredCommands; internal/tui/interject.go — stageInterjection, foldInterjected, flushInterjections, the withdraw/restore arms;
internal/tui/actuation.go — startProfileLoad (footer-only), foldActuationEvent, foldActuationDone; internal/tui/heartbeat.go — the note-then-refresh arms, the layout before armBeat; internal/tui/sessionsave.go — the save-result fold (addNote + refreshViewport);
internal/tui/model_test.go — TestInputAutoGrowReflowsViewport, step; internal/tui/e2e_test.go + cmd/apogee/e2e_*_test.go — TestE2E* goldens

**Tests.** The `internal/tui` suite; `TestInputAutoGrowReflowsViewport` unchanged; every `TestE2E*`
golden byte-identical.

**Acceptance.**
- `go test ./internal/tui`
- `go test ./cmd/apogee -run 'TestE2E'` (the E2E frame goldens live in `cmd/apogee`, not `internal/tui`).
- `grep -c 'm.layout()\|m.refreshViewport()' <the eleven files>` lower than at HEAD~1 (record before/after in NOTES).

**Commit:** `refactor(tui): transcript arms stop calling layout and repaint by hand`

## 20. Strip the pane arms

**What.** Depends on item 18. Same rule as item 19 over the pane files: `settings.go`,
`settingsapply.go`, `settingswatcher.go`, `picker.go`, `sessions.go`, `colorscheme.go` (~70
sites). A theme swap (`colorscheme.go`) and `HideScrollbar` changes are in item 18's key, so their
calls strip too; pane-open arms whose next line reads geometry keep the call with the comment.

**Files:** `internal/tui/settings.go`, `internal/tui/settingsapply.go`, `internal/tui/settingswatcher.go`, `internal/tui/picker.go`, `internal/tui/sessions.go`, `internal/tui/colorscheme.go`
**Read first:** internal/tui/model.go — layout, freshenTranscriptClamp, transcriptWidgetRows, refreshViewport, frameOverlays; internal/tui/settings.go — settingsKey, settingsEnter, settingsAbandonStep; internal/tui/settingsapply.go — settingsApplyLocal, applyColorScheme;
internal/tui/sessions.go — foldSessionList, sessionBrowserKey; internal/tui/sessions_test.go — withDraft, TestDecisionSurfaceStaysOnTheFrame; cmd/apogee/e2e_popups_test.go — TestE2EPopupFramesLists

**Tests.** The `internal/tui` suite; every `TestE2EPopupFrames*` / `TestE2E*` golden in `cmd/apogee`
byte-identical (never `-update` them).

**Acceptance.**
- `go test ./internal/tui`
- `go test ./cmd/apogee -run 'TestE2E'` (the E2E frame goldens live in `cmd/apogee`, not `internal/tui`).
- `grep -c 'm.layout()\|m.refreshViewport()' <the six files>` lower than at HEAD~1 (NOTES).

**Commit:** `refactor(tui): pane arms stop calling layout and repaint by hand`

## 21. Strip the input arms and state the rule

**What.** Recast at the regression check (2026-09-16). Depends on item 18. Same rule as item 19
(its input-write and generation preconditions included) over `mouse.go`, `autocomplete.go`,
`approval.go`, `ask.go`, `runview.go` (the 14 sites at 4d128f7a: mouse.go:599/1482/1526/1719,
autocomplete.go:289/697/938/987, approval.go:137/278, ask.go:41/127, runview.go:200/234 — none
transcript-preceded, so the strip rule itself strips none of them; the input-write and pane-open
`layout()` calls go on item 18's ground alone, stated once in item 18's What: settle's height half
carries them, and any followed by a geometry read stays with the comment). Then the prose: `internal/tui/doc.go` states the
rule ("an arm mutates; `Update`'s tail lays out and repaints; an arm calls `layout()` itself only
when it reads geometry afterwards, and says so") and `layout.md` gets one new sentence in its
leading paragraph stating the rule; every comment naming the old obligation — find them with
`grep -n 'layout()' internal/tui/*.go layout.md | grep '//\|^layout'` — is rewritten or deleted.

**Regression guard.** ONE strip rule, item 19's — "strip only a call a generation-bumping transcript method precedes in the same arm with no editor write or pane open between" — and no second one: this item's input-write and pane-open sites (approval.go foldApprovalRequest/sendApproval, ask.go foldAskRequest/submitAnswer, autocomplete.go removeCompletionToken/spliceCompletion, the mouse.go click arms) are not stripped by that rule; they are removed only because item 18's settle height half carries them (18 (d) `m.input.Height() != m.inputRows()` plus the widget-height compare — stated once, in item 18's What), keeping any followed by a geometry read. `internal/tui/model.go` joins Files for the comment sweep only (`keyClaimOrder`, `claimKey`, `freshenTranscriptClamp` docs carry "the layout() rule" and "own path did not lay out" — the acceptance grep cannot pass without it). `refreshViewport` in runview.go openRun/upRun is never stripped (named beside refreshViewportAnchored); acceptance grep replaced with phrases that exist at HEAD — `grep -n 'the layout() rule\|own path did not lay out\|moves the scroll clamp with them' internal/tui/*.go` → no match — and layout.md gets one new sentence in its leading paragraph stating the rule. Those prompt-boundary `layout()` calls yield to the documented decision at `approval.go` `foldApprovalRequest`/`sendApproval` ("the pane the decision turns on outranks the draft's extra rows"; "a draft the prompt had clamped grows back (draftRowsCeiling)" — the FOLLOW-UP-K fix at `model.go` `draftRowsCeiling`) until item 18's (d) is in.

**Files:** `internal/tui/mouse.go`, `internal/tui/autocomplete.go`, `internal/tui/approval.go`, `internal/tui/ask.go`, `internal/tui/runview.go`, `internal/tui/model.go` (comment sweep only), `internal/tui/doc.go`, `layout.md`
**Read first:** internal/tui/approval.go — foldApprovalRequest, sendApproval; internal/tui/ask.go — foldAskRequest, submitAnswer; internal/tui/model.go — layout, inputRows, draftRowsCeiling, freshenTranscriptClamp, claimKey, keyClaimOrder; internal/tui/runview.go — openRun, upRun, openRunAt, backHint;
internal/tui/autocomplete.go — foldSkillsReloaded, removeCompletionToken, spliceCompletion, dismiss arm; internal/tui/mouse.go — handleFooterModeClick, handleBrowserClick, handlePickerClick, handleDropdownClick;
internal/tui/sessions_test.go — TestDecisionSurfaceStaysOnTheFrame, withDraft; internal/tui/runview_test.go — TestRunViewEscGoesOneLevelUp

**Tests.** The `internal/tui` suite; `TestDecisionSurfaceStaysOnTheFrame` (every subtest) and
`TestRunViewEscGoesOneLevelUp` unchanged.

**Acceptance.**
- `go test ./internal/tui`
- `go test ./cmd/apogee -run 'TestE2E'` (the E2E frame goldens live in `cmd/apogee`, not `internal/tui`).
- `grep -n 'the layout() rule\|own path did not lay out\|moves the scroll clamp with them' internal/tui/*.go` → no match.

**Commit:** `refactor(tui): input arms stop calling layout by hand; the repaint rule is stated once`

## 22. The `/settings` second step is one table in `settings.go`

**What.** `internal/tui/settings.go`: a package-level `settingsSteps` table keyed by `settingsKind`
with `target(row) bool`, `key(m, msg)`, `hint()`, `editing() bool`, `editorMsg(m, msg)` — nil where
a kind has no arm (reset has no editor; the buffer has no renderer of its own). `settingsKey`'s four
`kind ==` arms, `settingsPaneHint`, `settingsEditing`, `settingsEditorMsg` become one lookup each.
Binding: the target predicates keep re-asking the row under an open step (a row that can no longer
hold the step abandons — never cache a row); the enum fallback keeps SWALLOWING the key while
buffer/text/reset go through `settingsAbandonStep` (state both once, in the table's doc); each
kind's key func keeps its own verdict spend (ADR 0053 D3); the table is over the EXISTING `sub` /
`editor` fields — never per-kind state (D9). `mouse.go` is not touched (bead apogee-agk).

**Regression guard.** The existing `settingsEnumTarget(rows)` / `settingsTextTarget(rows)` (called from `mouse.go` and the renderers), `settingsEditing(row)` (per-row, called by `settings_test.go` and the row painter), `settingsPaneHint(rows)` and `settingsEditorMsg` keep their names and signatures as the one-lookup wrappers over the table; the table's columns are `target(m, rows) (SettingRow, bool)`, `key(m, msg, row)`, `editing(m, row) bool`; the reset row's key func is `settingsResetKey`, referenced from `settingsapply.go` (no edit there).

**Files:** `internal/tui/settings.go`, `internal/tui/settings_test.go`
**Read first:** internal/tui/settings.go — settingsKey, settingsEditorMsg, settingsPaneHint, settingsEditing, settingsEnumTarget, settingsTextTarget; internal/tui/settingsapply.go — settingsResetKey;
internal/tui/mouse.go — settingsPaint, settingsTextPaint (callers of the targets, read-only); internal/tui/prompteditor.go — foldPaste (settingsEditorMsg caller)

**Tests.** The 93 `settings_test.go` tests unchanged; hint strings (`settingsBufferHint` …) are
announced text pinned by goldens — unchanged.

**Acceptance.**
- `go test ./internal/tui -run 'Settings|E2E'`
- `go test ./cmd/apogee -run 'TestE2E'` (the E2E frame goldens live in `cmd/apogee`, not `internal/tui`).
- `grep -c 'settings.kind ==' internal/tui/settings.go` → lower than at HEAD~1 (NOTES; target ≤ 4).

**Commit:** `refactor(tui): the /settings second step is one table keyed by kind`

## 23. The `/settings` renderer dispatches through the table

**What.** Depends on item 22. `renderSettings` and `settingsPaint`'s two target probes read the
step table (`paint(m) []string` column, nil for kinds painted as a row substitution); geometry
stays where it is — the pointer axis is apogee-agk's. If item 22 leaves no clean seam for the
renderer, this item is closed by a dated NOTES line saying so and a bead extending apogee-agk (an
authorised outcome, not a deferral of a regression).

**Regression guard.** The renderers return one composed string (`renderSettingsEnum(row) string`, `renderSettingsText(rows) string`; `renderSettings` returns `""` when unseated), so the column is `paint(m, row, rows) string` (nil for buffer/reset/key-list), not `paint(m) []string`; `settingsPaint` needs only its two "no pointer here" probes and reads "has own renderer" off the table (`paint != nil`), keeping `frameOverlays` purity (`model.go`) — View stays a pure reader.

**Files:** `internal/tui/settings.go`, `internal/tui/mouse.go`, `internal/tui/settings_test.go`
**Read first:** internal/tui/settings.go — renderSettings, settingsKeyListSpec, renderSettingsEnum, renderSettingsText; internal/tui/mouse.go — settingsPaint, settingsTextPaint, popupPaneHit; internal/tui/model.go — frameOverlays;
internal/tui/mouse_test.go — TestPopupPaneHitAnswersNothingWithThePaneShut; cmd/apogee/e2e_popups_test.go — TestE2EPopupFramesLists

**Tests.** `settings_test.go`, `mouse_test.go` `*Settings*` (8), `TestE2EPopupFrames*` (in `cmd/apogee`) as
at HEAD.

**Acceptance.**
- `go test ./internal/tui -run 'Settings|Popup|E2E'`
- `go test ./cmd/apogee -run 'TestE2E'` (the E2E frame goldens live in `cmd/apogee`, not `internal/tui`).

**Commit:** `refactor(tui): the /settings renderer reads the step table`

## 24. A card's name-keyed facts live on the registry row

**What.** `internal/tui/toolregistry.go`: `toolPresenter` rows gain `solo bool` and `wireDropped
[]string`; rows for `sub_agent`, `load_skill` and the write/edit tools carry them.
`toolview.go` `presentToolCall` and `transcriptbridge.go` `fromWireToolView` read `solo` off the
row — the `ask_user` arm (`done && details && !failed`) is a RESULT-time rule and stays as code in
the bridge; `wireargs.go`'s `contentArgs` table goes, `wireArgs` reads the row's `wireDropped`
(also on the unregistered path: a nil row means `solo=false`, full args). The `internal/run`
`boundArgs` mirror stays as documented (plan `2026-09-15 - 01` denied that fold).

**Regression guard.** The acceptance grep needs the test helpers and one comment renamed, not just retargeted: the schema cross-check test is retargeted at `toolRegistry[*].wireDropped` and renamed (`TestContentArgsMatchToolSchemas` → `TestWireDroppedMatchToolSchemas`, `contentArgsProblems` → `wireDroppedProblems`, `checkedContentArgs` → `checkedWireDropped`, `TestContentArgsProblemsReportsBothHalves` likewise), and `toolregistry.go`'s task_list row comment "No contentArgs row either" says "no wireDropped".

**Files:** `internal/tui/toolregistry.go`, `internal/tui/toolview.go`, `internal/tui/transcriptbridge.go`, `internal/tui/wireargs.go`, `internal/tui/wireargs_test.go`, `internal/tui/transcriptbridge_test.go`
**Read first:** internal/tui/wireargs.go — wireArgs, contentArgs; internal/tui/toolregistry.go — toolPresenter, toolRegistry (sub_agent, load_skill, write_file, edit_existing_file, task_list rows); internal/tui/toolview.go — presentToolCall, toolView.solo;
internal/tui/transcriptbridge.go — fromWireToolView (solo re-derivation); internal/tui/wireargs_test.go — TestContentArgsMatchToolSchemas, contentArgsProblems; internal/run/transcript.go — boundArgs (documented mirror, untouched)

**Tests.** `TestTranscriptCodecReDerivesSubAgentSolo`, `…AnsweredQuestionSolo`, `…SkillFetchSolo`,
the `wireargs_test.go` cases (retargeted at the row and renamed as the guard states),
`TestToolRegistryTaskListReplaysCollapsesToHeader`.

**Acceptance.**
- `go test ./internal/tui -run 'Registry|Wire|Codec|Solo|Present'`
- `grep -n 'contentArgs' internal/tui/*.go` → no match.

**Commit:** `refactor(tui): name-keyed card facts are registry-row columns`
