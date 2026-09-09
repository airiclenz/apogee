# Deferred findings — the eleven live register entries

**Goal.** Close every deferred finding still open in beads that a plan item can deliver today: the seven
test-coverage residuals the Reaction core stage-2 run deferred, the `wire_settings.go` re-read
consolidation, the `go vet` redundancy, the ADR 0071 D5 / `/settings` disagreement, and the last
`validated-sets`/`mechanisms` mention in the layout docs.

**Date:** 2026-09-09 · **Status:** unexecuted · **Base:** `cebd60fd` · **sized for:** ~200k-context host

**Sources.**
- issue register (`bd show`) → `apogee-yk9`, `apogee-370`, `apogee-3xx`, `apogee-zqs`, `apogee-d8q`, `apogee-boe`, `apogee-fso`, `apogee-o60`, `apogee-ilh`, `apogee-tbs`, `apogee-jwf`
- `docs/adr/0076-one-reaction-core-with-an-origin-by-class-policy-matrix.md` (A5 seam-closing notices, A8 one generation swap)
- `docs/adr/0071-floor-guards-are-engine-behaviour-and-the-nudge-catalogue-retires.md` Decision 5 and the 2026-09-07 amendment
- `docs/plans/archived/2026-09-08 - 00 - reaction-core-stage-2-plan.md` (the run that deferred items 1–7)
- `CONTEXT.md` §Floor guard, §Reactions and Moments

**Ratified design calls.**
- **Scope (owner, 2026-09-09):** the eleven beads above; `ed5`, `7ui`, `3h4`, `5cf` and every `parked`/bench bead stay out.
- **Stale beads (owner, 2026-09-09):** `m67`, `v00`+`.3–.7`, `y9g`, `at8` were already delivered by archived plan `2026-09-05 - 00`; closed in the register 2026-09-09, not items here.
- **ADR 0071 D5 (owner, 2026-09-09):** the ADR yields — D5 is amended to admit the `/settings` row; the seven `Editable: true` registry rows and CONTEXT.md stay as shipped.
- **`go vet` (owner, 2026-09-09):** the plain `go vet ./...` step leaves `make check` AND `ci.yml`; the `GOOS=windows go vet` step and the standalone `make vet` target stay.
- **Retired-schema fixtures (writer, 2026-09-09):** the headless fixtures move to the live `reactions:` schema; exactly ONE headless test keeps a `hooks:` fixture, renamed to say it pins the startup fold.

**Regression check (2026-09-09, `cebd60fd`):**
- 1: guard folded — the 11-row table grows to 12 (the six handler/webhook rows gain full sentences, the id/moments/event/timeout rows stay); the `url.Parse` row's `wantText` is built from `url.Parse`'s own error, not a literal.
- 2: guard folded — Observe is asserted through the Runner's `Replace` witness (`recordingRunner`), Floor/Bypass through `bound().Generation()`; the `pendingGeneration` acceptance grep is scoped to the rewritten test (`:288`, `:318` keep their reads).
- 3: guard folded — the rewritten test binds an Agent (the neighbours do not, so `bound()` is nil there); Observe through the recording exec / Runner `Replace`, Floor/Bypass through `bound().Generation()`.
- 4: guard folded — the premise sentence now reads that `LoadFileConfig` refuses and only `ResolveOptions` under `ApplyConfig` folds; the fixture carries `servers:`/`server:` (or the test accepts `*StartupUndetermined`), (a) reads `opts.Reactions`, (c) reads `ApplyConfig`'s `notify`.
- 6: guard folded — the armed reaction is a `postResponseReaction` returning an acting Outcome with Bypass off; the Exchange is scripted as one Turn (bad call, then `contentScript("done")`); `seamClosings` is at `:754`.
- 7: guard folded — every table row mutates its value after `Emit` and still asserts the golden projection.
- 9: guard folded — the rule covers every sentence or job name enumerating CI's or `check`'s steps (`ci.yml:3,:19`); the job is renamed only if the literal name is not a required status check.
- 10: guard folded (writer's decision) — the new section restates the seven-key count the 2026-09-07 amendment (ADR 0071:219-225) already carries, in its first line, and changes nothing else.
- 11: guard folded — the whole `### The Mechanism list` section (`:128-159`) is deleted too; the pointer class is read from `cmd/apogee/settingsrows.go` (`editPointer`, `externallyEdited`), not `wire_settings.go`; the `mechanisms` sentence is deleted.
- 5, 8: SAFE.

**Standing requirements.**
- `skills: coding-standards`
- Any authorised deviation from item text lands as a dated NOTES line under the item.
- Announced strings (refusals, notices, `--help` text) stay byte-identical unless an item says otherwise.
- Each item's **What** names the bead it closes; the closeout runs `bd close` for them and writes the CHANGELOG entries from the sidecars.

**Out of scope.**
- `apogee-ed5` (parallel e2e — its own plan), `apogee-7ui` (needs a measured CI run), `apogee-3h4` (unreproduced flake), `apogee-5cf` (open until a consumer asks), every `parked` bead and the `apogee-304` bench arms.
- Any change to what a Floor guard, Reaction or Driver announces; any new config key; any version identifier.

## 1. The five `run:` / `run: url:` / `headers-env:` refusals are pinned with their key prefix — ✅ DONE (2026-09-09)

NOTES (2026-09-09): the new `url.Parse` row sits before the `relative webhook` row so the six webhook rows
follow `validateWebhook`'s own switch order; no existing row moved.

**What.** Closes `apogee-yk9`. `TestHookValidateRefusesEachRule` (`internal/reactions/hooks_test.go:154-227`)
pins only message tails — `must not be blank`, `absolute http:// or https:// URL`, … — so the key
prefixes stage 2 reworded onto `run:` and `run: url:` are unpinned, and the `url.Parse` error branch
(`hooks.go:138`, `run: url: %q is not a URL: %v`) has no case at all. Make every `wantText` the FULL
sentence the emitting line produces, read from `internal/reactions/hooks.go:123,138,140,142,146`
(the `%q`/`%v` operands rendered for the case's input), and add the missing `url.Parse` case with an
input that fails parsing (e.g. `http://[::1` — an unclosed bracket) so all five sites have one case each.
Rule: a refusal test compares the whole announced sentence, never a substring that survives a rewording.

**Regression guard.** The table (`hooks_test.go:158-211`, 11 rows) never shrinks: the six handler/webhook
rows (empty argv, blank argv[0], relative, non-http, hostless, blank headers-env) keep their inputs and gain
full-sentence `wantText`; the id, moments, event and timeout rows stay as they are; one `url.Parse` row is
added — 12 rows. That row's `wantText` embeds net/url's own text, so it is built at test time from
`url.Parse(input)`'s error (`fmt.Sprintf("run: url: %q is not a URL: %v", in, err)`), never a literal; the
other four full sentences stay literal.

**Files:** `internal/reactions/hooks_test.go`

**Tests.** `TestHookValidateRefusesEachRule` — 12 rows: the six handler/webhook rows plus the new `url.Parse`
row carry a `wantText` equal to the full emitted sentence (the `url.Parse` one computed from the stdlib
error), the other five rows unchanged; the new `url.Parse` case fails against the pre-item test table (it
does not exist there).

**Acceptance.**
- `go test ./internal/reactions/ -run 'TestHookValidateRefusesEachRule' -count=1`
- `grep -c 'run: url:' internal/reactions/hooks_test.go` ≥ 3 and `grep -c 'headers-env:' internal/reactions/hooks_test.go` ≥ 1

**Commit:** `test(reactions): pin the entry refusals on their full run:/run: url:/headers-env: sentences`

## 2. The late-engine Floor replay test asserts what the bind observes, not `pendingGeneration`

**What.** Closes `apogee-370`. `TestLateEngineReplaysTheFloorGatesAtTheBind`
(`cmd/apogee/wire_engine_test.go:118-149`) reads the unexported `engine.pendingGeneration`
(`wire_engine.go:98`) at :131 and :146, and its second swap hands `SetReactions(apogee.Generation{})`
— an empty generation — so the property the test is named for, "a Floor-only swap leaves Bypass and
Observe alone at the holder", is never checked. Rewrite the test to assert ONLY through the exported
surface: after the bind, `engine.bound().Generation()` (`internal/agent/agent.go:1050`) carries the
Floor bits set before the bind; then arm Bypass and an Observe roster, swap a Floor-only generation
that changes one guard bit, and assert `.Bypass` and `.Observe` are unchanged while `.Floor` moved.
Every read of `pendingGeneration` leaves the test; the field stays (it is the holder's own state).

**Regression guard.** Observe is NOT carried by `Agent.Generation()` — assert the Observe half through the
Runner's `Replace` witness (a recordingRunner, as the reviewer's report names it) and only Floor/Bypass
through `engine.bound().Generation()`; a Floor-only swap must leave the recorded Observe roster and
`.Bypass` untouched while `.Floor` moves. A generation is applied WHOLE (`wire_engine.go:194-198`,
`agent.go:1044`), so the swap hands the ARMED generation back with one Floor bit flipped, Bypass and
Observe carried — never `Generation{Floor: X}` alone. `TestLateEngineReplaysThePendingGeneration`
(`:288-289`) and `TestSetReactionsWithoutARunner` (`:318-319`) keep their `pendingGeneration` reads; the
acceptance grep is scoped to the rewritten test's body.

**Files:** `cmd/apogee/wire_engine_test.go`

**Tests.** `TestLateEngineReplaysTheFloorGatesAtTheBind` rewritten as above; the retained-bits property
is pinned through a `recordingRunner` whose `lists` stay empty (Observe) and `bound().Generation()`
(Floor/Bypass) — as a subtest `…FloorSwapLeavesBypassAndObserveAlone` that extends
`TestSetReactionsSkipsTheRunnerWhenObserveIsUnchanged` (`:225-266`), which already pins it, rather
than a duplicate of it.

**Acceptance.**
- `go test ./cmd/apogee/ -run 'TestLateEngine|TestSetReactionsSkipsTheRunnerWhenObserveIsUnchanged' -count=1`
- `sed -n '/^func TestLateEngineReplaysTheFloorGatesAtTheBind/,/^}/p' cmd/apogee/wire_engine_test.go | grep -c pendingGeneration` → 0

**Commit:** `test(wire): the Floor replay test reads the bound Generation, not pendingGeneration`

## 3. `TestReactionsRowReloadSwapsObserveOnly` drives the real `lateEngine`

**What.** Closes `apogee-3xx`. `cmd/apogee/wire_settings_test.go:3151-3199` drives
`apply("reactions", …)` against `applySettingSpy`, so its one-swap-door claim is never made against
the real holder. Rewrite it on the pattern of its neighbours
`TestApplySettingReactionsReplacesTheRunnerAndTheProjection` (`:3045`) and
`TestApplySettingReactionsRefusesABrokenFileWithoutMovingAnything` (`:3096`):
`newLateEngine(domain.ModePlan, false)`, `engine.seedReactions(runner, apogee.Generation{Observe: boot})`,
a real `reactions.New(boot, reactions.Options{…})` Runner over `newRecordingHookExec()`. Assert that the
reload swaps ONLY the Observe roster: `Floor` and `Bypass` on the bound `Generation()` are unchanged
and the recording exec sees the new roster fire. The spy stays for the tests that legitimately use it.

**Regression guard.** Same witness rule — the Observe swap is asserted through the recording exec /
Runner `Replace`, `.Floor` and `.Bypass` through `bound().Generation()`; the phrase "`Observe` on the
bound Generation" leaves the item. Neither neighbour (`:3045`, `:3096`) binds an Agent, so `engine.bound()`
is nil there (`wire_engine.go:269-273`) and `Generation()` would panic: this test seeds
`apogee.Generation{Floor: {DisableReadCache: true}, Bypass: true, Observe: boot}` and then binds —
`engine.Bind(func() (*apogee.Agent, error) { return apogee.New(validCfg(t)) })` as
`wire_settings_test.go:2144` does — so the bound Agent runs the same Floor/Bypass the `config.Options`
handed to `newLiveSettings` reports.

**Files:** `cmd/apogee/wire_settings_test.go`

**Tests.** `TestReactionsRowReloadSwapsObserveOnly` rewritten against the real engine, seeded then
bound; asserts the Observe roster moved (recording exec) and Floor/Bypass did not (`bound().Generation()`).

**Acceptance.**
- `go test ./cmd/apogee/ -run 'TestReactionsRowReloadSwapsObserveOnly|TestApplySettingReactions' -count=1`
- `sed -n '3140,3210p' cmd/apogee/wire_settings_test.go | grep -c applySettingSpy` → 0

**Commit:** `test(wire): the reactions-row reload test swaps Observe on the real late engine`

## 4. `ApplyConfig` itself folds a `hooks:` block

**What.** Closes `apogee-zqs`. The fold is pinned at `migrateLegacyConfig` directly
(`configmigrate_test.go:788,906,1020`), `LoadFileConfig` REFUSES a `hooks:` file (`parseConfigFile(..., false)`,
`config.go:2650`; `liveReactionsRefusal`, pinned at `:1126`), and only `ResolveOptions` (`config.go:2897`)
under `ApplyConfig` folds — nothing drives it there: `TestApplyConfigMigratesTheRetiredKeys` (`:478`) covers only `endpoint:`/`api-key:`/`host-alias:`/`model:`.
Add `TestApplyConfigFoldsTheHooksBlock` beside it: a file carrying the retired
`hooks:`/`name:`/`events:`/`command:` shape, `ApplyConfig` run with migration allowed, asserting (a) the
resulting `Config` carries the equivalent `reactions:` entries (`id:`/`on:`/`run:`), (b) the file on disk
was rewritten to the live schema, and (c) the fold notice is announced through the same channel
`TestMigrateLegacyConfigAnnouncesTheFold` (`:942`) reads. Follow that test's fixture and assertion helpers.

**Regression guard.** A fixture carrying only the `hooks:` block makes `ApplyConfig` return
`*StartupUndetermined` (`config.go:3267-3268`, returned last at `:3116`), so a `t.Fatalf` on err fails on a
correct fold: give the fixture a `servers:` entry plus `server:` beside the `hooks:` block (or accept
`errors.As(err, new(*StartupUndetermined))`). (a) is read from `opts.Reactions []domain.Reaction`
(`options.go:333`); (c)'s channel is `ApplyConfig`'s `notify` callback — `TestMigrateLegacyConfigAnnouncesTheFold`
reads a returned `note`, so the assertion helpers are followed, not the channel.

**Files:** `internal/config/configmigrate_test.go`

**Tests.** `TestApplyConfigFoldsTheHooksBlock` (new).

**Acceptance.**
- `go test ./internal/config/ -run 'TestApplyConfig' -count=1`

**Commit:** `test(config): ApplyConfig itself is driven through the hooks: fold`

## 5. The headless fixtures speak the live `reactions:` schema; one test keeps the fold journey

**What.** Closes `apogee-d8q`. `hookHomeRecording` (`cmd/apogee/headless_test.go:3099-3104`) and the
fixture in `TestHeadlessReportsAFailingHookOnStderr` (`:3198`) still write
`hooks:`/`name:`/`events:`/`command:`, so the headless Reaction tests exercise the migration, not the
schema the run ships. Rewrite both fixtures as `reactions:` / `id:` / `on:` / `run:` (shape per
`docs/manual/reactions.md` §configuration and `internal/reactions/payload.go:36`). Binding call: keep
EXACTLY ONE headless test on the retired shape — a new `TestHeadlessFoldsARetiredHooksBlockOnStart`
whose fixture is the old `hookHomeRecording` text inlined, asserting the run still fires the folded
Reaction and the home file was rewritten — so the ApplyConfig-in-headless journey stays pinned
end-to-end after item 4 pins it in-package.

**Files:** `cmd/apogee/headless_test.go`

**Tests.** `hookHomeRecording` and `TestHeadlessReportsAFailingHookOnStderr` on the live schema (both
callers at `:3138`, `:3173` unchanged); `TestHeadlessFoldsARetiredHooksBlockOnStart` (new).

**Acceptance.**
- `go test ./cmd/apogee/ -run 'TestHeadless.*Hook|TestHeadlessFoldsARetiredHooksBlockOnStart' -count=1`
- `grep -c '^\s*hooks:\|"hooks:' cmd/apogee/headless_test.go` → exactly 1

**Commit:** `test(headless): the Reaction fixtures use the live reactions: schema; one test keeps the fold`

## 6. `SeamClosedEvent` closes once per attempt across the post-response retry hand-back

**What.** Closes `apogee-boe`. `internal/agent/reactions.go:153` (`if retried && seam.retryable`) hands
a retried post-response Turn back to the loop; the deferred emit at `:141-148` makes the seam close
once per `fire` call, i.e. once per ATTEMPT. Retry-then-succeed is driven by
`TestFloorGuard_ToolCallRepairRetriesUnderBypass` (`floorguards_test.go:52`) and
`TestFloorGuard_ToolUseEnforcerRetriesUnderBypass` (`:293`) via `captureAllResponder{scripts: …}`, but
neither reads `SeamClosedEvent`. Add `TestPostResponseSeamClosesOncePerAttemptAcrossARetry` in
`internal/agent/reactions_test.go` reusing that responder and the `seamClosings` helper (`:829`): a Turn
whose first response trips a retrying guard and whose second succeeds yields exactly TWO
`MomentPostResponse` closings, the first with `Fired` holding only the builtin's id (the armed leg
was skipped), the second with the armed leg's ids too; a single-pass Turn still yields one.

**Regression guard.** `Fired` books only reactions whose Outcome `acted` (`reactions.go:214-218,321-323`),
and under `cfg.Bypass = true` every ShapeView reaction is skipped (`:346-355`): the armed leg is a
`postResponseReaction` (`retryexchange_test.go:65`, ClassShapeView) returning an acting Outcome (`Defer: "…"`
or `Edited: true`) with Bypass OFF in the new test's cfg. The reused three-response script yields THREE
closings (`floorguards_test.go:58-62`, each response passes post-response once): script a one-Turn Exchange
instead — the bad call, then `contentScript("done")` — so the Exchange holds exactly the two attempts.
The `seamClosings` helper is at `reactions_test.go:754`, not `:829`.

**Files:** `internal/agent/reactions_test.go`

**Tests.** `TestPostResponseSeamClosesOncePerAttemptAcrossARetry` (new): one-Turn script, Bypass off, an
acting `postResponseReaction` armed; two closings, `Fired` = builtin id then builtin-free armed id.

**Acceptance.**
- `go test ./internal/agent/ -run 'TestPostResponseSeamClosesOncePerAttemptAcrossARetry|TestFloorGuard_.*RetriesUnderBypass' -count=1`

**Commit:** `test(agent): the post-response seam closes once per attempt across a retry`

## 7. Runner-level coverage for all five seam value projections

**What.** Closes `apogee-fso`. `TestSeamClosedProjectionIsTakenBeforeEmitReturns`
(`internal/reactions/runner_test.go:658-699`) is the only Runner-level test of the `value` projection
and covers `MomentHistoryRewrite` alone; `MomentPreRequest`, `MomentPostResponse`, `MomentPreToolExec`
and `MomentPostToolResult` (`internal/domain/reaction.go:30-34`, `domain.Seams()` at `:137`) reach a fired
entry's payload only through `projectSeamValue` unit tests (`payload_test.go:171,271`). Turn the
Runner-level test into a table over `domain.Seams()`: for each seam, one entry subscribed on it,
`Runner.Emit` with that seam's representative `SeamClosedEvent.Value` (the same inputs
`TestSeamClosedPayloadJSONGolden` uses), and an assertion that the `fakeExecutor` received a
`Payload.Value` equal to the golden projection. Rule: every seam `domain.Seams()` returns has a row;
a seam added later without a row fails the test (assert the table length equals `len(domain.Seams())`).

**Regression guard.** The test's one proof today is mutating `conversation`/`fired` AFTER `Emit` and
asserting the pre-mutation projection (`runner_test.go:672-697`); every row keeps it: after `Emit`, mutate
the row's value (`SetMessageContent`/`Append` on the Conversation, an `Edit` on the `ToolCallEdit`/
`ToolResultEdit`, `fired[0] = …`) and still assert the golden projection — the copy semantics stay pinned per seam.

**Files:** `internal/reactions/runner_test.go`

**Tests.** `TestSeamClosedProjectionIsTakenBeforeEmitReturns` → table over the five seams, each row
mutating its value after `Emit` and asserting the golden projection unchanged.

**Acceptance.**
- `go test ./internal/reactions/ -run 'TestSeamClosedProjection|TestSeamClosedPayloadJSONGolden' -count=1`

**Commit:** `test(reactions): the Runner projects every seam's value onto the fired entry`

## 8. One re-read of the config file per `/settings` apply

**What.** Closes `apogee-o60`. `cmd/apogee/wire_settings.go` calls
`config.LoadFileConfig(a.configPath, os.ReadFile, func(string) {})` at six sites — `readmitMCP` (:1715),
`reloadSystemPrompt` (:2031), `reloadServers` (:2055), `reconnectMCP` (:2100), `reloadReactions` (:2125),
`reloadModelProfiles` (:2161). Extract ONE unexported method `settingsApplier.fileConfig() (*config.FileConfig, error)`
(the deep module: one place that knows how the pane re-reads the file it just wrote) and route all six
through it. Each caller keeps its own error handling and announced text byte-for-byte —
`readmitMCP` still returns `mcpNoteFor(mcpReconnectFailed(err))`, the others still return `err` —
and the applier's behaviour on a parse failure is unchanged. No caching across applies: a `/settings`
row applies against the file as it is at that moment.

**Files:** `cmd/apogee/wire_settings.go`, `cmd/apogee/wire_settings_test.go`

**Tests.** One new test that points `configPath` at an unparseable file and drives each of the six
reloads, asserting the same refusal each produced before the item; existing `TestApplySetting*` tests
unchanged and green.

**Acceptance.**
- `go test ./cmd/apogee/ -run 'TestApplySetting|TestReactionsRow|TestSettings' -count=1`
- `grep -c 'config.LoadFileConfig(a.configPath' cmd/apogee/wire_settings.go` → exactly 1

**Commit:** `refactor(wire): the settings applier re-reads the config file through one method`

## 9. The plain `go vet ./...` leaves `make check` and CI

**What.** Closes `apogee-ilh`. `.golangci.yml` runs `default: standard`, which includes `govet`, so
`Makefile:286-287` (`==> go vet` in `check`) and `.github/workflows/ci.yml:49-50` duplicate `make lint`.
Remove both steps. Keep `Makefile:288-289` (`GOOS=windows go vet ./internal/platform/... ./internal/probe/...`
— golangci-lint does not run under that tag), the standalone `vet` target (`Makefile:214-217`) and
ci.yml's Windows compile note (`:159`). The `check` target's `##` comment (`Makefile:281`) and
`docs/manual/building.md:27` drop "vet" from the step list; a one-line Makefile comment beside the
Windows step records that plain vet is covered by the linter. Rule: every sentence that enumerates
`make check`'s steps names the same steps `check` runs — `grep -rn "vet" Makefile docs/manual/building.md .github/workflows/ci.yml`
finds them all.

**Regression guard.** The rule extends to every sentence or job name that enumerates CI's or `check`'s
steps: `.github/workflows/ci.yml:3` ("Gates formatting + vetting now") and `:19` (job name
`fmt / vet / build / test`) drop "vet" too. Before renaming the job, check GitHub's required-status-check
list for the literal `fmt / vet / build / test` and keep the old name if it is pinned there.

**Files:** `Makefile`, `.github/workflows/ci.yml` (`:3`, `:19`, `:49-50`), `docs/manual/building.md`

**Tests.** None (build surface); the acceptance commands are the test.

**Acceptance.**
- `make lint` passes and `make -n check | grep -c '^go vet ./\.\.\.$'` → 0
- `make -n check | grep -c 'GOOS=windows go vet'` → 1
- `make actionlint`

**Commit:** `build: drop the go vet step golangci-lint already runs from check and CI`

## 10. ADR 0071 Decision 5 admits the `/settings` row

**What.** Closes `apogee-tbs`. `docs/adr/0071-floor-guards-are-engine-behaviour-and-the-nudge-catalogue-retires.md:148-153` says the Floor guard
keys are "File-only: no flag, no `/settings` toggle", while `internal/config/registry.go` ships all seven
`Editable: true` and CONTEXT.md §Floor guard records "editable live in /settings" as intended. Ratified:
the ADR yields. Add an `## Amendment (2026-09-09) — the /settings row` section after the 2026-09-07
amendment: D5's "no `/settings` toggle" is withdrawn — the row is admitted because a toggle a user has
to open `/settings` and walk to is a deliberate act, the reasoning D5 was protecting; "no flag, no env"
stands; the key count reads seven everywhere in D5's wording (the 2026-09-07 amendment already says
so — say it again in the new section's first line). Do not edit D5's body: amendments append, per the
file's own precedent. No code, registry or CONTEXT.md change.

**Regression guard.** The new amendment section restates the seven-key count that the 2026-09-07
amendment (ADR 0071:219-225) already carries, in its first line, and changes nothing else.

**Files:** `docs/adr/0071-floor-guards-are-engine-behaviour-and-the-nudge-catalogue-retires.md`

**Tests.** None (ADR prose).

**Acceptance.**
- `grep -n 'Amendment (2026-09-09)' docs/adr/0071-floor-guards-are-engine-behaviour-and-the-nudge-catalogue-retires.md` → 1 match
- `grep -c 'Editable: *true' internal/config/registry.go` unchanged from the pre-item tree

**Commit:** `docs(adr): ADR 0071 D5 admits the /settings row for the seven Floor guard keys`

## 11. The settings-screen layout spec stops naming the retired `validated-sets.alias` and `mechanisms` keys

**What.** Closes `apogee-jwf` (its deletion sweep landed in stage 2; CONTEXT.md:1830 already reads
"Validated set → gone"). The one live mention left is the layout spec:
`docs/layout/settings-screen-layout.md:204-208` lists `validated-sets.alias` among the `⏎ opens $EDITOR`
keys and calls `mechanisms` "the one structured key that is not among" them — both keys are gone
(`mechanisms:` is stripped by the startup migration, `configmigrate.go:982`). Drop `validated-sets.alias`
from the list and delete the `mechanisms` sentence — no key today opens a toggle list; the `⏎ opens $EDITOR`
set is decided by `editPointer`/`externallyEdited` in `cmd/apogee/settingsrows.go:279-292` and pinned by
`cmd/apogee/settingsrows_test.go:519-551`, which is where the pointer class is read. Rule: no layout or manual page names a config key the registry no longer
carries — `grep -rn 'validated-sets\|mechanisms' docs/layout docs/manual` after the edit returns only
the migration notices in `docs/manual/configuration.md:160` and `docs/manual/reactions.md:275,298`.

**Regression guard.** `:204-208` is not the one live mention: `docs/layout/settings-screen-layout.md:128-159`
is a whole `### The Mechanism list (mechanisms)` section (frame + 4 paragraphs) describing a sub-pane the
shipped `/settings` no longer has — its "one structured key the pane opens itself" has no counterpart
(`settingsrows.go:279-288` knows only editable / `⏎ opens $EDITOR` / `pointerConfine`). Delete that section
too; the acceptance grep is the guard.

**Files:** `docs/layout/settings-screen-layout.md` (`:128-159`, `:204-208`)

**Tests.** None (spec prose).

**Acceptance.**
- `grep -rn 'validated-sets\|mechanisms' docs/layout` → no matches
- `grep -rln 'validated-sets' docs/manual` → only `docs/manual/configuration.md`, `docs/manual/reactions.md`

**Commit:** `docs(layout): the settings-screen spec drops the retired validated-sets.alias and mechanisms keys`
