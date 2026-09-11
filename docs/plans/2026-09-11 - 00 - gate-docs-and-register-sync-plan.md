# Gate docs, register sync and three small test fixes — plan

**Goal:** Stop the manual, README, default template and CONTEXT.md from denying the shipped `gate:` cell while keeping `advise:` worded as reserved; bring the beads register back in step with what stage 2/3 and the archived deferred-findings plan delivered; close the three small test beads (`apogee-itk`, `apogee-330`, `apogee-3h4`).

**Date:** 2026-09-11
**Status:** unexecuted
**Sized for:** ~200k-context host
**Base:** `de2263c0`

**Sources:**
- `docs/plans/2026-09-09 - 01 - reaction-core-stage-3-plan.md` (items 1–11 shipped; items 15/16 own the advise-era doc rewrite)
- `docs/plans/archived/2026-09-09 - 00 - deferred-findings-plan.md` (delivered the ten beads of item 5)
- `internal/config/reactions.go`, `internal/agent/gate.go`, `internal/domain/reaction.go` (the shipped gate cell)
- `docs/adr/0076-one-reaction-core-with-an-origin-by-class-policy-matrix.md`
- `bd show apogee-itk apogee-330 apogee-3h4`

**Ratified design calls:**
- **advise: stays reserved (owner, 2026-09-11):** every `advise:` sentence keeps its "reserved / not yet shipped" wording so stage-3 items 15/16 remain a pure follow-on; only the `gate:` half and the "observe-only" claims are rewritten.
- **CONTEXT.md gate lines in scope (owner, 2026-09-11):** the domain doc's sentence that `gate:` is refused is a false fact and is fixed here; the rest of stage-3 item 16 stays with that plan.
- **rxj/4kb inversion (owner, 2026-09-11):** `apogee-4kb` depends on `apogee-rxj` (stage 3 built the slot first), not the reverse.
- **3h4 fix shape (plan author, 2026-09-11, mechanical):** the discovery deadline becomes a `Client` Option with the 5s default unchanged; the test passes a generous one. The bead recorded no failure text, so the deadline is the code's only failure path, not an observed cause — the bead closes with that reason.
- **330 fix shape (plan author, 2026-09-11, mechanical):** the backup path is asserted by pattern and by the backup file's bytes; the rewritten file is asserted byte-equal to `migrateLegacyConfig`'s fold of the same input — no clock injection through `ApplyConfig`.

**Regression check (2026-09-11, de2263c0):** items 2, 3, 4 SAFE; items 1 and 5 GUARD.
- 1: guard folded — the three acceptance greps corrected (ADR 0073 link filename excluded, the joint-spelling grep narrowed, the `advise:` count widened to `refused` and made file-wise).
- 3: writer's decision folded — the byte-equal fold runs on a second on-disk fixture copy; the "if it does, NOTES the fact" clause dropped (settles the reviewer's NOTES line).
- 5: guard folded — `apogee-rxj` also depends on the closed `apogee-pjx`, so the acceptance asserts the `apogee-4kb` line alone, not an absent `DEPENDS ON` header.

**Standing requirements:**
- `skills: coding-standards`
- `bd` is at `/home/linuxbrew/.linuxbrew/bin/bd` (not on PATH) — prefix `PATH=/home/linuxbrew/.linuxbrew/bin:$PATH`.
- Any authorized deviation from item text lands as a dated NOTES line under the item.

**Out of scope:**
- The `advise:` cell, its docs and CONTEXT.md §Reaction beyond the gate sentences (stage-3 items 13–16).
- The context-fill-notice re-base (`apogee-4kb`, plan `2026-09-09 - 02`).
- `apogee-ed5`, `apogee-7ui`, `apogee-5cf`, `apogee-304` (parked by the archived plan, still parked).
- Any `VERSION` / release change.

## 1. The user docs stop denying the shipped `gate:` cell

**What:** Regression from `c2f0c9d3`/`de2263c0`: four user-facing docs still say reactions are observe-only and `gate:` is unshipped. Rewrite only the gate half; `advise:` sentences keep their reserved wording (split any sentence that bundles the two). The facts to state, from the shipped code: an entry may carry `run:` and/or `gate:` (an argv list), one Reaction per action key sharing `id:`/`on:`/`workspace:`; a gate reacts at `pre-tool-exec` only (`internal/domain/reaction.go:488`); default timeout 5s (`DefaultGateTimeout`), `timeout:` overrides; it runs as an Approver stage before the human Approver, also under Bypass; stdout's first line is `allow`, `deny` or `ask`, later lines the reason (capped at 240 runes); a missing, malformed, timed-out or crashing answer counts as `ask`; the first `deny` ends the call with the engine-authored result `tool call denied by reaction <id>` (the reason never reaches the model); the first `ask` forces the human gate with `reaction <id> asks: <reason>`; in headless/daemon runs `ask` resolves as the unattended Approver denies (`tool call denied by approver`); a delegation's `ask` defers to the child's calls.
Sites (rule: every sentence in these files claiming observe-only, "can change nothing", "cannot veto", or that `gate:` is reserved/unshipped — grep below finds them):
- `README.md:224` — replace the observe-only clause with one sentence saying a `gate:` entry can deny or hand a tool call to the human before it runs.
- `docs/manual/reactions.md:3,7-14,48,53,55` — drop "observe-only" (the ADR 0073/0076 link sentence at :16-19 stays — its filename spells `observe-only`); the `pre-*` paragraph becomes "a `gate:` entry reacts at `pre-tool-exec` and answers allow/deny/ask; the other four seams take `advise:`, not yet shipped"; the action table gains a `gate:` row (protocol, timeout, deny/ask texts above) and keeps an `advise:` row worded reserved; :55 says an entry takes `run:`, `gate:` or both.
- `docs/manual/configuration.md:372,376-381` — same split: `gate:` shipped at `pre-tool-exec`, `advise:` still refused at startup.
- `internal/config/defaults/config.yaml:439,467-468` — the comment block: `gate:` documented with a commented example entry; `advise:` line stays "reserved for a later release and refused today". (`cmd/apogee/defaults/config.yaml` does not exist.)
- `CONTEXT.md:1191-1194` — `gate:` is no longer a refusing key; the generation set reads `{Floor, Bypass, Observe, Sync}`. Nothing else in §Reaction changes.
**Regression guard.** (2026-09-11) The ADR 0073 link at `docs/manual/reactions.md:16` carries `observe-only` in its filename and stays: grep 1 excludes it. The prescribed split sentences legitimately put `gate:` and "not yet shipped"/"refused" on one line, so grep 2 is narrowed to the joint spellings the item must remove (the five sites at BASE: reactions.md:48,53, configuration.md:380, config.yaml:468, CONTEXT.md:1191); the split is otherwise checked by reading those five sites. The template splits `advise:` and "reserved" across `config.yaml:467-468` and configuration.md's wording says "refused", so the `advise:` count is widened to `reserved|not yet shipped|refused` and each of the three files must keep at least one line spelling `advise:` beside its reserved word.
**Files:** `README.md`, `docs/manual/reactions.md`, `docs/manual/configuration.md`, `internal/config/defaults/config.yaml`, `CONTEXT.md`

**Tests:** none new; the template must still parse and the manual guards must still pass.

**Acceptance:**
- `grep -n -i -E 'observe-only|can change nothing|cannot veto|no .pre-' README.md docs/manual/reactions.md docs/manual/configuration.md internal/config/defaults/config.yaml | grep -v '0073-hooks-are-observe-only'` → empty
- `grep -n -i -E '`advise:`[ /,]+(and )?`gate:`|`gate:`\)? (is|are|actions?,? which are) (reserved|not (yet )?shipped|refused)' README.md docs/manual/reactions.md docs/manual/configuration.md internal/config/defaults/config.yaml CONTEXT.md` → empty (five hits at BASE); the five rewritten sites read as split by inspection
- for each of `docs/manual/reactions.md`, `docs/manual/configuration.md`, `internal/config/defaults/config.yaml`: `grep -n -i 'advise:' <file> | grep -c -i -E 'reserved|not yet shipped|refused'` ≥ 1
- `go test ./internal/config/ && go test ./cmd/apogee/ -run 'Docs' && go test ./internal/tools/ -run 'Manual'`

**Commit:** `docs(reactions): the manual, README, template and CONTEXT.md describe the shipped gate: cell; advise: stays reserved`

## 2. `reloadServers` row fails on its own subtest (`apogee-itk`)

**What:** Fixes `apogee-itk`: in `cmd/apogee/wire_settings_test.go` `TestSettingsApplierReloadsRefuseAnUnparseableFile` (:3494), the `reloadServers` row's closure (:3514-3520) calls `t.Error` on the parent `*testing.T` from inside the `t.Run` subtest (:3525). Change the table's `run` field to `func(t *testing.T) error`, pass the subtest's `t` at the call site, and wrap the bare method-value rows (`a.reloadSystemPrompt`, `a.reconnectMCP`, `a.reloadReactions`, `a.reloadModelProfiles`) so they ignore `t`. No behaviour change beyond attribution.
**Files:** `cmd/apogee/wire_settings_test.go`

**Tests:** the existing test; attribution is checked by reading `-v` output (the failure, if forced, names the `reloadServers` subtest).

**Acceptance:**
- `go vet ./cmd/apogee/`
- `go test ./cmd/apogee/ -run 'TestSettingsApplierReloadsRefuseAnUnparseableFile' -v` → every subtest PASS
- `grep -n 't.Error' cmd/apogee/wire_settings_test.go | sed -n '/351[0-9]\|352[0-9]/p'` → the only `t` in the reloadServers row is the closure parameter

**Commit:** `fix(cmd/apogee): the reloadServers refusal row reports on its own subtest`

## 3. The fold notice's backup path is asserted (`apogee-330`)

**What:** Fixes `apogee-330`: `TestApplyConfigFoldsTheHooksBlock` (`internal/config/configmigrate_test.go:520`, notes asserted at :588-601) never checks the backup path the one fold notice announces, and asserts the rewritten file by containment. Add, without changing production code: (a) the notice matches `^apogee: rewrote <path> — .*; backup at <path>\.bak-\d{8}-\d{6}\.` (regexp; `path` quoted with `regexp.QuoteMeta`), the captured backup file exists and holds the original fixture bytes; (b) the rewritten file's bytes equal what `migrateLegacyConfig` produces for the same input under `migrationClock` — compute that in the test rather than duplicating the golden, since the two paths share `reactionsFold` and must agree byte-for-byte (the backup suffix does not appear in the rewritten content). Keep the existing silent-second-launch assertion. Producer of the notice: `internal/config/configmigrate.go:1526` (`reactionsFold.note`); backup suffix from `:426`.
**Regression guard.** (2026-09-11) the byte-equal comparison folds a SECOND on-disk copy of the same fixture (written with writeMigrationConfig, configmigrate_test.go:21) through migrateLegacyConfig under migrationClock, never the ApplyConfig path's own file — migrateLegacyConfig stats, backs up and rewrites the path it is given (configmigrate.go:141-148), so folding the ApplyConfig file again would write a second backup; the rewritten bytes never embed the backup stamp (golden at configmigrate_test.go:917-937), so drop the "if it does, NOTES the fact" clause.
**Files:** `internal/config/configmigrate_test.go`

**Tests:** the amended test; bite check — the new backup-path regexp assertion must fail when the pattern's suffix is deliberately mistyped (verifier does this in a scratch copy).

**Acceptance:**
- `go vet ./internal/config/`
- `go test ./internal/config/ -run 'TestApplyConfigFoldsTheHooksBlock|TestMigrateLegacyConfigFoldsTheHooksBlock' -count=1 -v`
- `grep -n 'bak-' internal/config/configmigrate_test.go` shows the new pattern under `TestApplyConfigFoldsTheHooksBlock`

**Commit:** `fix(config): TestApplyConfigFoldsTheHooksBlock asserts the notice's backup path and the rewritten bytes`

## 4. Discovery's deadline is injectable so the rate-limited row cannot time out under shard load (`apogee-3h4`)

**What:** Fixes `apogee-3h4`: `TestDiscoverTransportFailureIsLabelled/rate_limited` (`internal/provider/discovery_test.go:928-995`) flaked once under `scripts/test-shards.sh` (race-instrumented, ~8 concurrent `go test` processes). The row's only failure path is `c.httpClient.Do` erroring — there are no retries, sleeps or shared limiters on the discovery path — and the only error a healthy loopback `httptest` server can yield is the hard-coded `discoveryTimeout = 5 * time.Second` (`discovery.go:15`) expiring under load. Make it a field on `Client` set by a new `WithDiscoveryTimeout(d time.Duration) Option` (zero/unset keeps 5s; the constant stays as the default), `Discover` (`:149-156`) reads the field. `TestDiscoverTransportFailureIsLabelled` builds its client with `WithDiscoveryTimeout(60 * time.Second)`. Add one unit test that `Discover` against a handler which sleeps past a 50ms configured deadline returns an error satisfying `errors.As(err, &TransportError)` and `errors.Is(err, context.DeadlineExceeded)` — pinning the timeout surface the flake lived on. The bead closes with reason "deadline path removed from the test; cause inferred, not observed" (item 5).
**Files:** `internal/provider/discovery.go`, `internal/provider/client.go`, `internal/provider/discovery_test.go`

**Tests:** the new deadline test; the existing `TestDiscoverTransportFailureIsLabelled` under `-race -count=20`.

**Acceptance:**
- `go vet ./internal/provider/`
- `go test ./internal/provider/ -race -count=20 -run 'TestDiscoverTransportFailureIsLabelled|TestDiscover.*Deadline'`
- `grep -n 'discoveryTimeout' internal/provider/*.go` → the constant is read once, as the default

**Commit:** `fix(provider): the discovery deadline is a Client option; the rate-limited discovery row no longer races a 5s constant`

## 5. The register matches the tree

**What:** Depends on items 2, 3, 4. With `PATH=/home/linuxbrew/.linuxbrew/bin:$PATH`:
- `bd close apogee-yk9 apogee-370 apogee-3xx apogee-zqs apogee-d8q apogee-boe apogee-fso apogee-o60 apogee-ilh apogee-tbs --reason "delivered by docs/plans/archived/2026-09-09 - 00 - deferred-findings-plan.md (17c105c1 090b0f63 480095e0 4cb5922e f3f9b321 94978659 5fe4fcc4 300336ff edb2bbe4 9ba9619e)"`
- `bd close apogee-jwf --reason "delivered: 6553a121 deleted validated-sets: and internal/validated; cbb400a4 CONTEXT.md; 38521ded layout"`
- `bd close apogee-itk apogee-330 --reason "fixed in plan 2026-09-11 - 00 items 2 and 3"`; `bd close apogee-3h4 --reason "fixed in plan 2026-09-11 - 00 item 4: discovery deadline injectable, test no longer races the 5s constant; cause inferred from the only failure path, not observed"`
- `bd dep remove apogee-rxj apogee-4kb` then `bd dep add apogee-4kb apogee-rxj`; `bd show apogee-4kb` must read `DEPENDS ON → apogee-rxj`.
- `bd export -o .beads/issues.jsonl`, then compare against `bd list --status=closed` for the eleven ids and `bd show apogee-4kb` — re-export if the file lags (known: batch writes can leave it stale).
- Stage `.beads/issues.jsonl` and commit. The pre-commit hook re-exports after git snapshots the index: if `git status` shows the file modified after the commit, `git add .beads/issues.jsonl && git commit --amend --no-edit`.
**Regression guard.** (2026-09-11) `apogee-rxj` also depends on the closed `apogee-pjx` (`bd show apogee-rxj` → `✓ apogee-pjx`), which this item does not remove, so its `DEPENDS ON` header survives the swap. Assert the swapped edge itself: `bd show apogee-rxj | grep -c '→ . apogee-4kb'` → 0 (at BASE the id appears only on that dependency line) and `bd show apogee-4kb | grep -c '→ . apogee-rxj'` → 1.
**Files:** `.beads/issues.jsonl`

**Tests:** none (register only).

**Acceptance:**
- `PATH=/home/linuxbrew/.linuxbrew/bin:$PATH bd list --status=open | grep -c -E 'apogee-(yk9|370|3xx|zqs|d8q|boe|fso|o60|ilh|tbs|jwf|itk|330|3h4)\b'` → 0
- `PATH=/home/linuxbrew/.linuxbrew/bin:$PATH bd show apogee-rxj | grep -c '→ . apogee-4kb'` → 0; `bd show apogee-4kb | grep -c '→ . apogee-rxj'` → 1 (under the `DEPENDS ON` header); `bd show apogee-rxj | grep -c '→ ✓ apogee-pjx'` → 1 (untouched)
- `grep -c '"id":"apogee-jwf"' .beads/issues.jsonl` = 1 and that row carries `"status":"closed"`; `git status --porcelain .beads/issues.jsonl` → empty after the commit

**Commit:** `chore(beads): close the eleven delivered beads and the three fixed here; 4kb depends on rxj`
