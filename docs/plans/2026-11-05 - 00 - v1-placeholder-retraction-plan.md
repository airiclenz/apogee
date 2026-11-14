# v1 placeholder retraction — make v1.0.0 a releasable number

**Goal:** Make v1.0.0 releasable on every channel, `go install …@latest` included. The range retraction becomes an exact list of burned versions owned by `internal/update`. `publish` places the self-retracting placeholder tag on the release commit, and the scheme is proven offline in CI. Cutting the bridge release and v1.0.0 stays the owner's act.
**Date:** 2026-11-05
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 2f57f04b

**Sources:**
- go.mod (`retract [v1.0.0, v1.8.0]`); proxy.golang.org `@v/v1.8.0.mod` (same line, immutable)
- docs/manual/building.md §Releasing and the go-install paragraph; `docs/design/test-drivers.md` §Waiting
- ADR 0001 (versioning history, consequences amendment of 2026-09-30); ADR 0098 (release pipeline); ADR 0031 (Drivers)
- commit 8febcc90 (the range guards this plan replaces)
- Offline rehearsal, 2026-11-05: GOPROXY=file:// with the real tag history. Bridge only → `@latest` is the bridge. Bridge plus v1.0.0 alone, or the placeholder alone → still the bridge. Both, with the placeholder on v1.0.0's commit → v1.0.0. A placeholder that does not retract itself → `@latest` is the placeholder, still building v1.0.0's code. A higher placeholder with the extended list → v1.0.0. Later v1.2.0 and v1.9.0 resolve normally.
- sum.golang.org lookups, 2026-11-04: v1.1.0, v1.7.0 and v1.8.0 are recorded. v1.0.0, v1.0.1, v1.2.0–v1.6.0, v1.7.1, v1.8.1 and v1.9.0 are not.

**Ratified design calls** (owner, 2026-11-05):
- **Burned set:** v1.1.0, v1.7.0, v1.8.0, v1.8.1; the placeholder is the highest burned version; recovery from a bad placeholder = append the next v1.8.x to the list.
- **Placeholder mechanism:** a probed, idempotent `placeholder` step in `publish`, between `release` and `homebrew` (writer amendment, 2026-11-05, from the re-check: VCS stamp).
- **Single source:** `internal/update` owns and exports the burned list; `cmd/release` imports it; drift tests tie go.mod and `.github/scripts/version-bump.sh` to it.
- **Rehearsal:** a committed, offline Go test.
- **ADR scope:** numbering decision only (ADR 0107); v1 criteria and stability promise go to a capcom issue.
- **Real-proxy smoke (writer, slow-box rule):** an exact-version `go list -m` that reports the release retracted fails; an `@latest` that lags only warns (proxy caching), no polling.

**Standing requirements:**
- skills: coding-standards
- Any deviation from item text lands as a dated NOTES line under the item.
- Raspberry Pi 5 host: never `-race`/`-cover` over a whole heavy package or a pattern; targeted `GOMEMLIMIT=2GiB go test -race -count=1 -run TestX ./pkg/`; full suite only as `APOGEE_TEST_SLOW=1 GOMEMLIMIT=2GiB make check`; one test run at a time. Forward to every sub-agent.
- No item changes `VERSION`, a CHANGELOG release heading or a tag.

**Out of scope:**
- Cutting the bridge release or v1.0.0, and running the placeholder step for real.
- v1 criteria (`windows-auto-temp-caches`, `phase5-owner-verification`, signing) and the stability promise, beyond filing the issue.
- pkg.go.dev display of retracted versions.

**Regression check (2026-11-05, 2f57f04b):**
- 1: guard folded (TestNewer rows replaced, not extended).
- 2: guard folded (prepare fixtures and refusal cases move; help text and publish comment; stale local v1.x tags as a runbook precondition).
- 3: guard folded (same push as item 2; script accepts checkRetractKept's forms; drift test named).
- 4: recast (pending only for v1.0.0 <= vX < P; pre-change HEAD:go.mod check; fetch, failed-push resume); yields to runPublish's "refuses before changing anything" contract (cmd/release/publish.go:151).
- 5: guard folded (go list in r.workDir; missing go is SKIP; smoke fixtures and help move).
- 6: recast (retraction read from the `Retracted` field, not a wording; P on v1.0.0's commit only; GOPRIVATE-family pins and a temp cmd.Dir).
- 7: guard folded (ADR index line in decisions.md; reading-order sentence).
- 8: guard folded (`rg --hidden` and wider alternatives; [Unreleased] CHANGELOG bullet; gofmt acceptance that can fail).
- 4 (re-check): recast (placeholder step moves between `release` and `homebrew`; buildReleaseAssets deletes the burned tags from its clone before the build, so the VCS stamp reads vX); guard folded (Goal's pending window reworded; step-list and status tests; cmd/release help text and comments, status.go added to Files; v1.0.0 test route).
- 4 (re-check): ADR 0098 Decision 3–4 and building.md:331 ("never creates a tag itself") are superseded for the placeholder tag only (writer, 2026-11-05: the owner's publish-step choice is a tag-creating step); item 7 amends ADR 0098, item 8 rewrites building.md:331.
- 6 (re-check): guard folded (`@latest` queries without `-retracted`; Goal reads a non-empty `Retracted` field, not a wording).
- 8 (re-check): guard folded (the step-list rule covers every text listing publish's steps, building.md's included).

## 1. internal/update owns the burned-version list — ✅ DONE (2026-11-14)

NOTES (2026-11-14): consequential edit — internal/update/latest.go: made necessary by the new exported list; the package doc (which lives in latest.go, not version.go) now names `RetractedVersions` and `IsRetracted` beside `Newer`.
NOTES (2026-11-14): added `TestIsRetracted` (exact match, build metadata dropped, malformed tags never retracted) and a build-metadata row in `TestNewer` beyond the plan's listed cases; `version` gained an unexported `String()` used by `IsRetracted`.
NOTES (2026-11-14): heads-up for item 2 — the package doc in latest.go says "cmd/apogee is the only package that reaches it"; once cmd/release imports `update.IsRetracted`/`RetractedVersions`, that sentence needs to name cmd/release too.

**What:**
**Goal:** `internal/update` exports the burned list and a retraction predicate. `Newer` never announces a burned version, and does announce an unburned v1.x (v1.0.0, v1.2.0) above the current 0.x.
**Approach (assumed at the header base):** In `internal/update/version.go`, replace `retractedLow`/`retractedHigh` and `version.isRetracted` with an unexported ordered list of the four burned tags. Export `RetractedVersions() []string` (returns a copy, ascending) and `IsRetracted(tag string) bool` (exact match after parsing, so build metadata is dropped). `Newer` calls `IsRetracted`. The package doc and `Newer`'s doc name the list, not a range.
**Regression guard.** `TestNewer`'s range rows (internal/update/version_test.go:21 v1.2.0, :22 v1.0.0, :24 v1.8.1) contradict the new rule: replace them, do not extend around them. v1.0.0 and v1.2.0 flip to want true, v1.8.1 flips to want false; the new cases cover the rest.
**Files:** internal/update/version.go, internal/update/version_test.go
**Read first:** internal/update/version.go — Newer, parseVersion, version.isRetracted, retractedLow; internal/update/version_test.go — TestNewer; cmd/apogee/update.go — runUpdate; cmd/apogee/wire_update.go — updateHost.CheckForUpdate
**Tests:** `TestNewer`: replace the three range rows (v1.2.0 and v1.0.0 now want true, v1.8.1 wants false), then add: v1.0.0, v1.2.0, v1.7.1 and v1.8.2 are newer than v0.32.0; v1.1.0, v1.7.0, v1.8.0 and v1.8.1 are not. Add `TestRetractedVersionsIsACopy`.
**Acceptance:** `go vet ./internal/update/ && GOMEMLIMIT=2GiB go test -race -count=1 ./internal/update/`
**Commit:** `feat(update): own the exact list of burned v1.x versions`

## 2. Release tool and go.mod switch from the range to the list — ✅ DONE (2026-11-14)

NOTES (2026-11-14): consequential edit — internal/update/latest.go: made necessary by cmd/release importing internal/update; the package doc now says cmd/apogee is the only package that reaches it for update traffic, and that cmd/release imports only the burned-version list (carried as the run's DECISION from item 1's handoff).
NOTES (2026-11-14): `checkRetractKept` reads retractions through a new `goModRetractions` helper (file order, comments stripped, one-line and block forms) and compares the set ignoring order and repeats; `retractBlock()` builds the expected block for the refusal message. The repo drift test keeps its name `TestRepoKeepsTheRetractLineAndAReleasableVersion` (item 3's `-run 'TestRepo'` and Read-first name it) and pins the live go.mod to the list in order and as one block.
NOTES (2026-11-14): renamed `TestCheckNotRetractedRefusesTheWholeSeriesOnly` → `TestCheckNotRetractedRefusesTheBurnedVersionsOnly` and `TestHighestReleaseTagOrdersNumericallyAndSetsAsideTheRetractedSeries` → `…SetsAsideTheBurnedVersions`; added `TestCheckRetractKeptAcceptsTheBurnedListOnly` (block, one-line, mixed, range, missing, extra, commented-out forms). `TestPrepareRefuses` keeps a v1.8.0 case beside the plan's v1.1.0 and v1.8.1, and gains a no-directive/commented-out pair re-pinned to the new message.
NOTES (2026-11-14): the go.mod comment, which Go shows as the retraction rationale, says `publish` tags v1.8.1 on v1.0.0's commit — behaviour item 4 adds in this same run.

**What:** Depends on item 1.
**Goal:** The live `go.mod` carries one `retract ( … )` block listing exactly `update.RetractedVersions()`, under a comment explaining the placeholder. `prepare` and `publish` refuse only burned versions. `prepare` refuses a `go.mod` whose retract block is not exactly that list. `highestReleaseTag` sets aside only burned tags.
**Approach (assumed at the header base):**
- In `cmd/release/version.go`, drop `retractedLow`/`retractedHigh`, `isRetracted` and `retractDirective`. `checkNotRetracted` calls `update.IsRetracted` (cmd/release's first `internal/` import).
- `checkRetractKept` parses go.mod's retract lines into a version list. It accepts the block form and the one-line `retract vX` form; any range, or any other list, is refused, with the expected block in the message. No new module dependency.
- Error wording names the burned set rather than "the v1.0.0 to v1.8.0 series".
- Rework `TestRepoKeepsTheRetractLineAndAReleasableVersion` to compare the live go.mod's parsed list with `update.RetractedVersions()`.
**Regression guard.** The prepare fixtures move with the rule: `preparedTags` (cmd/release/prepare_test.go:53) carries burned v1.x tags only (v1.1.0, v1.8.0) beside the 0.x ones, or v1.0.0 wins `highestReleaseTag` and every prepare test is refused; its comment at :50-51 follows. `preparedGoMod` (:55) becomes the new block. Rewrite `newPrepareCommand`'s Long text (cmd/release/prepare.go:50, "inside the retracted v1.0.0 to v1.8.0 series") and the comment above `checkNotRetracted` in `runPublish` (publish.go:158) to name the burned set. A clone that kept local tags of the deleted v1.x series (fetch does not prune tags) would now see them win `highestReleaseTag`: item 8's runbook names `git fetch --prune --prune-tags` as the precondition before `prepare`.
**Files:** go.mod, cmd/release/version.go, cmd/release/version_test.go, cmd/release/prepare.go, cmd/release/prepare_test.go, cmd/release/publish.go, cmd/release/publish_test.go
**Read first:** cmd/release/version.go — checkNotRetracted, checkRetractKept, highestReleaseTag; cmd/release/version_test.go — TestRepoKeepsTheRetractLineAndAReleasableVersion; cmd/release/prepare_test.go — preparedTags, preparedGoMod, TestPrepareRefuses; cmd/release/prepare.go — newPrepareCommand
**Tests:**
- `TestCheckNotRetracted…`: v1.0.0 and v1.2.0 are allowed; the four burned versions are refused.
- prepare fixtures: `preparedTags` holds v0.24.10, v0.24.11, v1.1.0, v1.8.0; `preparedGoMod` holds the new block.
- `TestPrepareRefuses`: the v1.0.0 and v1.4.2 cases (:270-275) become v1.1.0 and v1.8.1; the two go.mod refusal wordings (:281, :285) are re-pinned to the new message.
- prepare cases: a go.mod still holding the old range, and one missing v1.8.1, are both refused.
- `TestPublishRefusesARetractedVersionBeforeAnyProbe` runs over v1.1.0 and v1.8.1.
- The `highestReleaseTag` table: a v1.0.0 tag now wins over 0.x.
**Acceptance:** `go vet ./cmd/release/ && GOMEMLIMIT=2GiB go test -race -count=1 ./cmd/release/`
**Commit:** `feat(release): retract an exact list of burned versions, not a range`

## 3. CI tagging script refuses the burned list

**What:** Depends on item 2. Items 2 and 3 land in the same push — the run pushes once at closeout and never between them.
**Goal:** `.github/scripts/version-bump.sh` refuses to tag a burned `VERSION` or a bump whose go.mod retract block is not exactly the burned list. A drift test fails when the script's list and `update.RetractedVersions()` differ.
**Approach (assumed at the header base):**
- One `burned='v1.1.0 v1.7.0 v1.8.0 v1.8.1'` line replaces `retract_line` and `is_retracted`.
- `keeps_retract_line` becomes a check that extracts the retract block of `git show <sha>:go.mod`, comments stripped, and compares the version set with `$burned`.
- `::error::` messages name the burned set.
- The drift test lives in `cmd/release/version_test.go`. It reads `../../.github/scripts/version-bump.sh`, parses the `burned=` line and compares it with `update.RetractedVersions()`.
**Regression guard.** Items 2 and 3 land in the same push — the run pushes once at closeout and never between them. The script accepts exactly the go.mod forms item 2's `checkRetractKept` accepts (one-line `retract vX` lines plus block lines, comments stripped): a refusal is only an `::error::` annotation (version-bump.sh:29-32), so a form prepare passes but the script refuses leaves the bump untagged with CI green. The drift test is named `TestRepoVersionBumpScriptBurnsTheSameList`, so the Acceptance's `-run 'TestRepo'` runs it.
**Files:** .github/scripts/version-bump.sh, cmd/release/version_test.go
**Read first:** .github/scripts/version-bump.sh — retract_line, is_retracted, keeps_retract_line, the bumps loop; .github/workflows/tag-on-version-bump.yml — detect step; cmd/release/version_test.go — TestRepoKeepsTheRetractLineAndAReleasableVersion
**Tests:** the drift test, `TestRepoVersionBumpScriptBurnsTheSameList`. A scratch-repo run of the script, as done on 2026-11-04, is the manual half: v1.0.0, v1.2.0 and v1.8.2 are tagged; v1.1.0, v1.8.1 and a go.mod with the old range are refused; a go.mod carrying the list as one-line `retract vX` lines is tagged.
**Acceptance:** `bash -n .github/scripts/version-bump.sh && make actionlint && GOMEMLIMIT=2GiB go test -count=1 -run 'TestRepo' ./cmd/release/`
**Commit:** `feat(ci): tag-on-version-bump refuses the burned version list`

## 4. publish places the self-retracting placeholder tag

**What:** Recast at the regression check (2026-11-05). Depends on item 2.
**Goal:** `publish vX` runs a `placeholder` step between `release` and `homebrew`. The placeholder P is the highest of `update.RetractedVersions()`. The step is pending only when v1.0.0 <= vX < P and vX is not burned; otherwise it is done ("not needed: <reason>") with no command issued. Inside that window it is done when origin already holds P as an annotated tag; otherwise converging it creates annotated tag P on vX's commit and pushes it. Before it does, it refuses unless `git show vX:go.mod` retracts exactly the burned list, and never moves an existing P.
**Approach (assumed at the header base):**
- Add `stepPlaceholder` with `probePlaceholder` to `publishSteps` in probes.go, directly after `stepRelease`. Probe with `git ls-remote --tags origin refs/tags/P`, then the `gh api …/git/ref/tags/P --jq .object.type` used by `probeTag`; a lightweight P is unknown.
- Add the matching `publishActions` entry: `describePlaceholder` / `convergePlaceholder`. Converge runs `git tag -a P -m P vX^{commit}` then `git push origin refs/tags/P`.
- Add `git tag -a` to `mutatingCommands`.
- The `stepReport` JSON gains one step name; status shows it.
**Regression guard.** The placeholder step is pending only when v1.0.0 <= vX < P and vX is not burned; every 0.x vX and every vX above P is done ("not needed: <reason>") with no command issued, so P lands on v1.0.0's commit, never the bridge's. When that window applies and the tag step is pending, runPublish refuses BEFORE any change unless `git show HEAD:go.mod` retracts exactly the burned list (keeps the "refuses before changing anything" contract); converge re-checks `git show vX:go.mod`. convergePlaceholder first fetches refs/tags/vX:refs/tags/vX as verifyTag does; it accepts a local annotated P already on vX's commit (left by a failed push) and just pushes it, and refuses a local P on any other commit. Tests name the existing fixtures that move: TestPublishFirstRunConvergesTheTagThenTheRelease (exact mutating list unchanged for v0.24.11 since 0.x is done), TestStatusFailsOnAStepItCouldNotProbe (index of homebrew becomes 3), TestProbesNeverReadAFailedCallAsDone (skip a placeholder that runs no command, as smoke), fakeRemote gains a separate P tag field; add cases "vX tagged, P absent", "tag step done, vX not local", "local P left by a failed push", "local P on another commit refused", and a v1.0.0 fixture.
The placeholder step moves to directly AFTER `release` (before `homebrew`), so the release assets are built before P exists on origin; and buildReleaseAssets deletes every burned tag (`update.RetractedVersions()`) from its temporary clone before `go build`, so the binaries' VCS stamp (`go version -m`, Main.Version) reads vX, never P — add a test that the clone's tag deletion precedes `go build` and a scratch-verifiable note.
Every help text or comment in cmd/release that lists the publish steps or says publish never creates a tag names the placeholder step — status --help (status.go:66), publish --help (publish.go:123-124), convergeTag's comment (publish.go:313); find them with `rg -n 'tag, release|never creates|one tagger' cmd/release`. Route the v1.0.0 refusal cases through runPublish directly, as TestPublishRefusesARetractedVersionBeforeAnyProbe does, with a `git show …:go.mod` answer added to the fake (runTestPublishWith forces publishVersion; fakeRepo and fakeRemote answer no `git show …:go.mod`); pin the fetch→tag→push order by calling convergePlaceholder directly, or give publishFake a version field.
The item yields to runPublish's documented contract "refuses before changing anything when a precondition fails" (cmd/release/publish.go:151). It supersedes ADR 0098 Decision 3–4 and building.md:331 ("It never creates a tag itself") for the placeholder tag only: CI still creates every release tag, and publish creates no other tag. Item 7 records the amendment and item 8 rewrites the manual line.
**Files:** cmd/release/probes.go, cmd/release/probes_test.go, cmd/release/publish.go, cmd/release/publish_test.go, cmd/release/status.go, cmd/release/status_test.go
**Read first:** cmd/release/probes.go — publishSteps, probeTag; cmd/release/publish.go — runPublish, publishActions, convergeTag; cmd/release/publish_test.go — mutatingCommands, publishFake.respond, TestPublishRefusesARetractedVersionBeforeAnyProbe
**Tests:**
- Placeholder pending (a v1.0.0 fixture): the commands are a fetch of vX's tag, a tag on vX's commit, then a push, in that order.
- 0.x vX (v0.24.11) and above P: the step is done ("not needed: <reason>") and issues none of its commands; `TestPublishFirstRunConvergesTheTagThenTheRelease` keeps its exact mutating list.
- P already annotated on origin: done. P lightweight: unknown, so publish refuses.
- `fakeRemote` gains a separate P tag field: "vX tagged, P absent" is pending.
- Tag step pending and `HEAD:go.mod` not retracting exactly the burned list: refused before any change.
- go.mod at vX lacking P: refused, nothing pushed.
- "tag step done, vX not local": the fetch runs before `git show vX:go.mod`.
- "local P left by a failed push": pushed, not re-tagged. "local P on another commit": refused.
- `TestStatusFailsOnAStepItCouldNotProbe`: homebrew's index becomes 3. `TestProbesNeverReadAFailedCallAsDone` skips a placeholder that runs no command, as it skips smoke.
- `TestProbeStepsRunsEveryStepInPublishOrder` and `TestStatusReportsTheGateBaselineAndHighestTag`: "placeholder" goes after "release" in both lists (the status state is "placeholder done" for v0.24.14). `TestStatusTextNamesTheBlockingConditions`: re-pad its two step rows, or switch them to `hasRow` ("placeholder" widens the steps name column, status.go:257).
- buildReleaseAssets: the clone's deletion of every burned tag precedes the build.
- Dry run prints the step's plan.
- `TestPublishActionsMatchThePublishSteps` passes.
**Acceptance:** `go vet ./cmd/release/ && GOMEMLIMIT=2GiB go test -race -count=1 ./cmd/release/`
**Commit:** `feat(release): publish places the self-retracting placeholder tag`

## 5. smoke checks the Go module proxy

**What:** Depends on item 2.
**Goal:** `smoke vX` reports a `goproxy` check: `go list -m -json -retracted github.com/airiclenz/apogee@vX` against proxy.golang.org must succeed and not mark vX retracted, else fail. `go list -m github.com/airiclenz/apogee@latest` differing from vX is a warn naming both, never a fail.
**Approach (assumed at the header base):** A new `smokeRun` method after `checkTag`. The commands go through the runner (`commands.run(ctx, dir, env, "go", …)`) with the env `GOPROXY=https://proxy.golang.org`, `GOFLAGS=`, `GOWORK=off`, `GONOSUMDB=` and `GOTOOLCHAIN=local`, so tests stay fake (runner.go's no-toolchain invariant). No polling or deadline of its own.
**Regression guard.** Both `go list` calls run in r.workDir (an empty temp dir outside any module), keeping GOTOOLCHAIN=local; test that the recorded call's dir is not options.dir. A missing `go` is SKIP via isMissingTool(err), as checkAssets/checkHomebrew do, with a "go missing" fake case. Retraction is read from the presence of the JSON `Retracted` field, never its wording. newSmokeFake answers both go list lines with a passing default and every TestSmokeChecks wantTally gains one check; update smoke's Long help listing the checks.
**Files:** cmd/release/smoke.go, cmd/release/smoke_test.go
**Read first:** cmd/release/smoke.go — runSmoke, smokeRun.checkTag, smokeRun.skip, isMissingTool, newSmokeCommand; cmd/release/smoke_test.go — newSmokeFake, TestSmokeChecks; cmd/release/runner.go — runner
**Tests:**
- Fake answers covering: a pass; a vX marked retracted (a `Retracted` field present), which fails; an `@latest` that lags, which warns; a failing `go list`, which fails with its output; and `go` missing, which skips.
- The env pins appear on the recorded call, and its dir is not options.dir.
- `newSmokeFake` answers both `go list` lines with a passing default; every `TestSmokeChecks` wantTally gains one check.
**Acceptance:** `go vet ./cmd/release/ && GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestSmoke' ./cmd/release/`
**Commit:** `feat(release): smoke checks the release on the Go module proxy`

## 6. Offline rehearsal test of the retraction scheme

**What:** Recast at the regression check (2026-11-05). Depends on items 1 and 2.
**Goal:** A committed test in `internal/update` proves Go's own resolution against local file proxies, from the live go.mod retract block and the immutable v1.8.0 range block.
- Bridge only, v1.0.0 alone, and the placeholder alone each resolve `@latest` to the bridge.
- Both v1.0.0 and the placeholder resolve to v1.0.0.
- Every burned version's `go list -m -json -retracted <mod>@<v>` carries a non-empty `Retracted` field.
- It skips when `go` is not on PATH, and never reaches the network.
**Approach (assumed at the header base):**
- New file `internal/update/retraction_scheme_test.go`, modelled on the 2026-11-05 scratch rehearsal (`mkproxy.py`). It writes `<module>/@v/{list,vX.info,vX.mod,vX.zip}` for a fake module `example.com/apogee` under `t.TempDir()`. The zips carry a `go.mod` and a `cmd/apogee/main.go`.
- It execs `go list -m` with the pins `GOPROXY=file://…`, `GOSUMDB=off`, `GOFLAGS=-modcacherw`, `GOWORK=off`, `GOTOOLCHAIN=local`, `GOENV=off`, plus a temp `GOMODCACHE`/`GOPATH`.
- No `go install`; that is not needed for resolution.
- Exec budget through `internal/testtime`.
**Regression guard.** Assert retraction by the presence of the `Retracted` field in `go list -m -json -retracted` output, never a fixed wording (the go.mod comment above the block becomes the rationale); rehearse P on v1.0.0's commit only (item 4 never places it on the bridge). `GOENV=off` ignores only the go env file, so also pin `GOPRIVATE=`, `GONOPROXY=`, `GONOSUMDB=` and `GOINSECURE=` (empty) — a box with `GOPRIVATE=*` otherwise reaches example.com — and run with `cmd.Dir` a temp dir so apogee's go.mod is not the main module. Run the `@latest` queries without `-retracted` (with it, `@latest` may resolve to a retracted version); use `-json -retracted` only on exact-version queries (`@v1.1.0` …), where the `Retracted` field is checked.
**Files:** internal/update/retraction_scheme_test.go
**Read first:** go.mod — retract block and its comment; internal/update/version.go — Newer, isRetracted; internal/testtime/testtime.go — Budget; internal/testtime/guard_test.go — TestNoFixedTimingInTests, timedCalls; internal/tools/diagnostics_test.go — realGo
**Tests:** this item is the test.
**Acceptance:** `GOMEMLIMIT=2GiB go test -count=1 -run 'TestRetractionScheme' -v ./internal/update/`
**Commit:** `test(update): rehearse the placeholder retraction offline`

## 7. ADR 0107 and the v1 criteria issue

**What:**
**Goal:** ADR 0107 records the numbering decision, and ADR 0001 names it. A capcom issue holds the v1 criteria and the stability promise. `capcom check` is clean.
**Approach (assumed at the header base):**
- ADR 0107 (`capcom know add --parent decisions`) in the house shape: a Status line, a one-paragraph summary, and children `-context`, `-decision`, `-rejected`, `-consequences`.
- The decision:
  - the burned set and the placeholder rule; v1.0.0 is cut with P on its commit;
  - the bridge release must ship the list-based update check first;
  - the 1.x numbers to skip;
  - recovery by appending the next v1.8.x.
- Rejected: v1.9.0 as first stable; a `/v2` path; a vanity import path.
- ADR 0001: a dated `> **Amended 2026-11-05 (…plan).**` blockquote under its consequences' retract note, plus its Status line.
- ADR 0098: a dated amendment child `adr-0098-amend-1105` (named on its Status line and reading-order list): publish creates exactly one tag, the placeholder, after `release`; CI still creates every release tag.
- Then `capcom create --id v1-release-criteria` with an `## Acceptance` naming `windows-auto-temp-caches`, `phase5-owner-verification`, `unsigned-release-binaries` and a stability-promise ADR.
**Regression guard.** `.capcom/knowledge/decisions.md` keeps the hand-kept ADR index (`- adr-0106 — …` at :110) and `capcom check` does not catch a missing line: add `- adr-0107 — <title>` there. ADR 0107's body names its four children in a reading-order sentence, as adr-0106.md does ("Read `adr-0106-context`, … `adr-0106-consequences`.").
**Files:** .capcom/knowledge/decisions.md, .capcom/knowledge/decisions/adr-0098.md, .capcom/knowledge/decisions/adr-0098/adr-0098-amend-1105.md, .capcom/knowledge/decisions/adr-0107.md, .capcom/knowledge/decisions/adr-0107/, .capcom/knowledge/decisions/adr-0001.md, .capcom/knowledge/decisions/adr-0001/adr-0001-consequences.md, .capcom/issues/v1-release-criteria.md
**Read first:** .capcom/knowledge/decisions.md — ADR index list; .capcom/knowledge/decisions/adr-0106.md — Status line, reading-order sentence (house shape); .capcom/knowledge/decisions/adr-0001.md — Status line, reading-order sentence;
  .capcom/knowledge/decisions/adr-0001/adr-0001-consequences.md — "Amended 2026-09-30" retract blockquote; .capcom/issues/unsigned-release-binaries.md, windows-auto-temp-caches.md, phase5-owner-verification.md — ids exist
**Tests:** none (docs).
**Acceptance:** `capcom check && capcom know show "ADR 0107" && capcom show v1-release-criteria`
**Commit:** `docs(decisions): ADR 0107 — v1.0.0 via a self-retracting placeholder`

## 8. Docs, skill and package doc speak the list

**What:** Depends on items 2, 4, 5 and 7.
**Goal:** No non-archived text describes the retraction as the v1.0.0–v1.8.0 range, or says the first stable release cannot be v1.0.0. `docs/manual/building.md` carries a "Cutting v1.0.0" runbook: the bridge first; then `prepare`, then `publish` with its `placeholder` step, then `smoke`'s goproxy check; and the recovery. The release skill never proposes a burned version and skips burned minors (v1.0.x → v1.2.0).
**Approach (assumed at the header base):** Rule for every site: text naming `v1.0.0–v1.8.0`, `v1.0.0 to v1.8.0`, `retract [v1.0.0, v1.8.0]` or "cannot be v1.0.0". Find them with `rg -n 'v1\.0\.0.{0,6}v1\.8\.0|retract \[|cannot be v1' --glob '!**/archived/**' --glob '!CHANGELOG.md' --glob '!.capcom/**'`. Known at write time: AGENTS.md (Releases bullet), docs/manual/building.md, apogee.go, `.apogee/skills/release-apogee/SKILL.md` steps 4 and 5 (a new `placeholder` row in status/publish output). The CHANGELOG preamble rewording travels in this item's sidecar.
**Regression guard.** Every rule grep over dot-dirs uses `rg --hidden` (and the Acceptance's negative grep too). The Approach's grep is `rg --hidden -n 'v1\.0\.0.{0,6}v1\.8\.0|retract \[|cannot be v1|above v1\.8\.0|retracted .?v1\.x' --glob '!.git/**' --glob '!**/archived/**' --glob '!CHANGELOG.md' --glob '!.capcom/**'`, which also finds .apogee/skills/release-apogee/SKILL.md:145,155, docs/manual/building.md:62 and AGENTS.md:27 ("never publish a version above v1.8.0"); the Goal's "no non-archived text" means what that grep checks (`.github/` is item 3's). The runbook names `git fetch --prune --prune-tags` before `prepare` (item 2's stale-tag precondition). The sidecar also rewrites the [Unreleased] bullet "Release tooling refuses the retracted v1.0.0–v1.8.0 numbers" (CHANGELOG.md:14) to the burned-list behaviour. `gofmt -l` exits 0 when it lists a file, so the Acceptance tests its output. building.md:331 ("It never creates a tag itself") becomes: publish creates no release tag, only the placeholder (ADR 0098 amendment of 2026-11-05). The runbook and step-list rule also covers docs/manual/building.md's list of publish steps (the six-step list near the publish description) — the rule is "every text listing publish's steps names `placeholder` after `release`", with a grep for `homebrew` near `smoke` in docs/manual and the skill to find them.
**Files:** AGENTS.md, docs/manual/building.md, apogee.go, .apogee/skills/release-apogee/SKILL.md
**Read first:** docs/manual/building.md — go-install paragraph, "Versions and tags"; .apogee/skills/release-apogee/SKILL.md — step 4 version proposal, step 5 status/prepare; AGENTS.md — Releases bullet; apogee.go — package doc "Stability" paragraph;
  CHANGELOG.md — preamble, [Unreleased] "Release tooling refuses the retracted" bullet
**Tests:** none (docs); `go vet .` for apogee.go.
**Acceptance:** `test -z "$(gofmt -l apogee.go)" && go vet . && ! rg --hidden -n 'v1\.0\.0.{0,6}v1\.8\.0|cannot be v1|above v1\.8\.0|retracted .?v1\.x' AGENTS.md docs/manual apogee.go .apogee/skills`
**Commit:** `docs(release): describe the burned list and the v1.0.0 runbook`
