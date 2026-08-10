# Structure-split plan — model.go and wire.go stop being junk drawers

- **Goal:** cure the two file-level junk drawers the 2026-08-10 doc-landscape audit
  found (`internal/tui/model.go`, `cmd/apogee/wire.go`), move the config cluster to
  `internal/config`, tidy `internal/mechanisms` naming, and put test-enforced doc.go
  file maps on every large package. Behaviour-preserving throughout: no rendering or
  wiring semantics change.
- **Date:** 2026-08-10. **Status:** not started.
- **sized for:** ~200k-context host.
- **Authoritative sources:**
  - `docs/handoffs/2026-08-10 - 00 - structure-refactor-model-wire-split.md` (mission + cluster findings)
  - `docs/reviews/2026-08-10 - 00 - doc-landscape-audit.md` §Flags 3–4
  - ADR 0010 (layering; `internal/*` never imports root), ADR 0011 (value-copied Model;
    no held-by-value no-copy types — `TestModelNoBuilderByValue`), ADR 0031 (door-keeping
    invariants), ADR 0043 (written by item 2 of this plan; later items cite it — the plan text says
    0042, but that number was taken on 2026-08-10 before this plan ran; see item 2's NOTES)
  - coding-standards skill structure rules (airiclenz/skills `da76213`): coordinator
    types split by concern cluster; composition roots split by seam with a map comment;
    packages past ~10 files carry a test-enforced doc.go file map.
- **Ratified design calls (owner, 2026-08-10, via AskUserQuestion in the planning session):**
  1. Config cluster moves to `internal/config` — in this plan, not deferred.
  2. Mechanisms tidy-up — in this plan.
  3. model.go: lift exactly the three known clusters (session-save queue, approval
     handling, command-running/refusal); Model stays the coordinator; `internal/tui`
     stays flat (no sub-packages).
  4. wire.go: fine-grained split, ~6–7 `wire_<seam>.go` files; `wire.go` keeps
     `runRoot` plus a file-top map comment.
  5. doc.go file-map guard tests: every package with ≥ ~10 non-test files (10 counts).
  6. Config boundary: core 4 files + `registry.go` + the `options` struct move;
     `settingsrows.go` / `settingsedit.go` stay in `cmd/apogee` (their file headers
     claim composition-root residency as the /settings display seam, ADR 0011).
  7. Mechanisms file naming: compact (no underscores) wins — 5 production renames,
     matching the package's own majority and `internal/tui`.
- **skills: coding-standards**
- **Standing requirements (every item):**
  - Behaviour-preserving refactor only. `make check` green before commit; one commit
    per item; commit directly to `main`; no AI attribution trailers.
  - Never change VERSION, CHANGELOG release headings, or tags (VERSION-SUGGESTION only).
  - Never stage or commit `docs/layout/tool-layout.md` — it carries an uncommitted
    owner design sketch. If the Phase 0 dirty-tree gate fires on it, the recommended
    handling is: proceed, leaving that file untouched and unstaged.
  - Any file split or rename that touches a package with a doc.go file map updates
    that map in the same commit.
  - Any authorized deviation from item text lands as a dated NOTES line under the item.
  - Line numbers cited below are anchors from the 2026-08-10 working tree; the
    **member name lists are the binding scope** — re-locate by name if lines drifted.
- **Out of scope:** any behaviour or rendering change; package reshuffles beyond those
  named here (the package layout is exemplary — audit §Flag 3); the
  `docs/layout/tool-layout.md` rendering redesign; items of the doc-landscape cleanup
  plan (`2026-08-10 - 01`); version bumps.

## 1. Verify the tool-surface plan is archived — ✅ DONE (2026-08-10)

**What:** This plan touches `internal/tools` (item 14) and `cmd/apogee` files that the
in-flight plan `docs/plans/2026-08-10 - 00 - tool-surface-improvements-plan.md` borders.
Verify that plan is complete and archived: the file exists under `docs/plans/archived/`
and no longer under `docs/plans/`. If it is not archived, report BLOCKED — do not work
around it.

**Tests:** none (verification item).

**Acceptance:** `ls "docs/plans/archived/" | grep -F "2026-08-10 - 00 - tool-surface-improvements-plan.md"`
succeeds and `ls "docs/plans/" | grep -F "tool-surface"` finds nothing.

**Commit:** `chore(plans): confirm tool-surface plan archived before structure split`
(plan-file mark only).

## 2. ADR 0042 — file-level structure rules and internal/config — ✅ DONE (2026-08-10)

NOTES (2026-08-10): landed as **ADR 0043**, not 0042 — `0042-external-programs-are-optional-enhancements-never-prerequisites.md`
was taken earlier the same day by the doc-landscape cleanup plan's ADR salvage (`5a2bbe0`). The
item's own parenthetical rule ("next free number") governs, so the record is
`docs/adr/0043-files-split-by-concern-and-config-gets-a-package.md`; the intro bullet's ADR
reference is updated to 0043 so later items cite the right number. A CHANGELOG `[Unreleased]` →
`Changed` entry was added per the repo's convention for ADR-ratifying commits.

**What:** Write `docs/adr/0042-files-split-by-concern-and-config-gets-a-package.md`
(next free number after 0041), status accepted, recording the owner-ratified calls
above as the durable decision: (a) coordinator files split by concern cluster
(`internal/tui/model.go`; house pattern `prompteditor.go` / `fold.go`); (b) composition
roots split by subsystem seam into `wire_<seam>.go` files with a file-top map;
(c) the config cluster (core 4 + key registry + `options`) becomes `internal/config`,
while the /settings display projection stays in the binary (ADR 0011 thin-renderer
rationale); (d) packages past ~10 non-test files carry a test-enforced doc.go file
map. Record what does NOT change: ADR 0010 layering (the moved package imports
`internal/domain`, never root), ADR 0024/0028 "the binary owns" behaviour stances
(unaffected — behaviour stays in the binary; only file homes move), flat
`internal/tui`.

**Tests:** none (doc item).

**Acceptance:** file exists; `make check` green.

**Commit:** `docs(adr): ADR 0042 — file-level structure rules and internal/config`

## 3. Lift the session-save write queue into internal/tui/sessionsave.go — ✅ DONE (2026-08-10)

**What:** Pure same-package move out of `model.go` (~lines 1957–2331) into a new
`internal/tui/sessionsave.go`: `savePayload`, `Model.snapshotPayload`, `Model.persist`,
`Model.saveAtIdle`, the queue design banner comment, `recordWriteKind` + its 5 consts
(`writeSave`…`writeActivate`) + `retargets`, `recordWrite`, `Model.scheduleSave`,
`Model.scheduleWrite`, `Model.queueWrite`, `Model.pumpWrites`, `Model.writeCmd`,
`Model.saveComplete`, `Model.foldRecordWrite`, `Model.pumpOrQuit`, `Model.restashTitle`,
`sessionTitleMax`, `sessionTitle`. The Model struct fields the cluster owns
(`writeBusy`, `saveFailing`, `pendingWrites`) STAY in the `Model` struct in `model.go`.
No signature, name, or behaviour changes. Update `internal/tui/doc.go`'s file map in the
same commit (add `sessionsave.go`, adjust the `model.go` line). ADR 0011 bind: a pure
move introduces no held-by-value no-copy type; `TestModelNoBuilderByValue` must stay
green.

**Tests:** existing coverage carries (`model_test.go`, `sessions_test.go`,
`autotitle_test.go`, `e2e_test.go`, `contextfiles_test.go`); no new tests — the change
is a move.

**Acceptance:** `go build ./... && go test ./internal/tui/` green; `make check` green;
`grep -c '^func ' internal/tui/model.go` decreased by ≥ 14 vs before;
`grep -q sessionsave.go internal/tui/doc.go`.

**Commit:** `refactor(tui): lift the session-save write queue into sessionsave.go`

## 4. Lift approval handling into internal/tui/approval.go

**What:** Pure same-package move out of `model.go` into a new `internal/tui/approval.go`,
both halves of the concern: the key-handling block (~1212–1302) — `approvalOption`,
`approvalMenu`, `approvalKeys`, `approvalMenuKeys`, `Model.handleApprovalKey`,
`Model.resolveApproval`, `Model.sendApproval` — and the render trio (~4353–4578) —
`Model.approvalPrompt`, `subAgentPromptLine`, `Model.approvalArgsBlock`. Model fields
`pending` and `approvalSel` STAY in the `Model` struct. Popup layout helpers it calls
(`popupRow`, `popupBudget`, `clampInt`) stay where they are (same package, reachable).
Update `doc.go`'s map in the same commit.

**Tests:** existing `model_test.go` coverage carries; no new tests.

**Acceptance:** `go build ./... && go test ./internal/tui/` green; `make check` green;
`grep -q approval.go internal/tui/doc.go`; `grep -c 'approvalPrompt\|handleApprovalKey' internal/tui/model.go` = 0 definitions remaining (call sites may remain).

**Commit:** `refactor(tui): lift approval keys and approval prompt rendering into approval.go`

## 5. Lift command-running/refusal into internal/tui/commandrun.go

**What:** Pure same-package move out of `model.go` (~1406–1747) into a new
`internal/tui/commandrun.go` (`command.go` already exists — do not touch it):
`Model.refuseUnknownSlash`, `Model.commandRunnable`, `Model.refuseIdleOnlyCommand`,
`Model.launchExchange`, `Model.startNewSession`, `Model.runCommand`. `Model.submit`
STAYS in `model.go` (input concern, separate); `parsedInput` stays in `command.go`.
External callers (`interject.go`, `autocomplete.go`) are unaffected — same package.
Update `doc.go`'s map in the same commit.

**Tests:** existing coverage carries (`model_test.go`, `minilang_test.go`,
`autotitle_test.go`); no new tests.

**Acceptance:** `go build ./... && go test ./internal/tui/` green; `make check` green;
`grep -q commandrun.go internal/tui/doc.go`; `runCommand` no longer defined in
`model.go`.

**Commit:** `refactor(tui): lift command running and refusal into commandrun.go`

## 6. Split wire.go — live-apply seams

**What:** Pure same-package moves out of `cmd/apogee/wire.go` into four new files
(function bodies unchanged):

- `wire_settings.go` — `newLiveSettings` + all `liveSettings` methods,
  `settingsApplier` + all its methods (`applySettingFor`, `cannotApply`, `unreachable`,
  `rideTheRebind`, `reloadSystemPrompt`, `reloadServers`, `reloadMechanisms`,
  `reloadValidatedSets`, `reconnectMCP`, `reloadProfile`), `settingInt`, `settingBool`,
  `rebindSpecFor`.
- `wire_tools.go` — `newLiveTools` + `liveTools` methods, `registryWithMCP`,
  `mechanismIDs`, `knownMechanismList`.
- `wire_mcp.go` — `newLiveMCP` + `liveMCP` methods, `closeSession`,
  `mcpReconnectFailed`.
- `wire_present.go` — `presentationRungs`, `newLivePresentation` + `livePresentation`
  methods, `sameDocServer`.

Each new file gets a short file-top comment saying which seam it wires.

**Tests:** existing `cmd/apogee` tests carry; no new tests.

**Acceptance:** `go build ./... && go test ./cmd/...` green; `make check` green; the
four files exist; none of the moved identifiers remain defined in `wire.go`.

**Commit:** `refactor(cmd): split wire.go live-apply seams into wire_settings, wire_tools, wire_mcp, wire_present`

## 7. Split wire.go — hosts, engine, server; add the file-top map

Depends on item 6.

**What:** Same-package moves out of `wire.go` into three more files:

- `wire_session.go` — `newSessionHost` + `sessionHost` methods, `newRecallHost` +
  `recallHost` methods, `resolveResume`, `resolveResumeArg`, `resolveContinue`,
  `resumedSession`.
- `wire_engine.go` — `buildAgent`, `newLateEngine` + all `lateEngine` methods,
  `friendlyConstructErr`.
- `wire_server.go` — `startupEntry`, `serverBinder` + `bind`, `awaitConfigChangeOn`.

`wire.go` keeps `runRoot` and the small top-level helpers (`shouldPrewarmLabelWalk`,
`parseMode`, `resolveColorScheme`, `apogeeHome`, `resolveRoots`) and gains a file-top
map comment naming every `wire_<seam>.go` with a one-line description of its seam.

**Tests:** existing tests carry; no new tests.

**Acceptance:** `go build ./... && go test ./cmd/...` green; `make check` green;
`wire.go`'s head comment names all seven `wire_*.go` files;
`grep -c '^func ' cmd/apogee/wire.go` ≤ 15.

**Commit:** `refactor(cmd): split wire.go hosts, engine, and server seams; add file-top map`

## 8. Config move prep — retype onto internal/domain, extract shared test fixtures

**What:** (a) In `cmd/apogee/config.go`, replace the five root-package type references
(`apogee.Config`, `apogee.ModelProfile`, `apogee.ThinkingProfile`,
`apogee.ToolCallFormat`, `apogee.ThinkingStyle` — ~18 uses) with their
`internal/domain` equivalents (all are plain aliases in `apogee.go`; zero semantic
change) and drop the root import from `config.go`. (b) Extract the four fixture
helpers other test files depend on — `noNotify`, `testConfigHome`,
`assertHomeHoldsOnlyConfig`, `upstreamHome` (currently in `config_test.go`) — into a
new `cmd/apogee/testsupport_test.go` that stays behind after the move (consumers:
`confinement_e2e_test.go`, `defaults_test.go`, `headless_test.go`, `probe_test.go`,
`probemodel_test.go`, `root_test.go`).

**Tests:** existing tests carry; the extraction is proven by the suite compiling and
passing.

**Acceptance:** `go build ./... && go test ./cmd/...` green; `make check` green (its
ADR-0010 import check must stay green); `config.go` no longer imports
`github.com/airiclenz/apogee` (the root module).

**Commit:** `refactor(cmd): retype config core onto internal/domain; extract shared test fixtures`

## 9. Config move I — registry, options, migrate, watch move behind a bridge

Depends on item 8.

**What:** Create `internal/config` (package `config`). `git mv` these `cmd/apogee`
files into it: `registry.go` (key registry: `configKey`, `keyRegistry`, `fileConfig`
rows), `configmigrate.go` (+ `configmigrate_test.go`), `configwatch.go`
(+ `configwatch_test.go`), and move the `options` struct out of `root.go` into
`internal/config/options.go` as `Options`. Export what crosses the seam (registry
surface, `Watcher`/`NewWatcher`, `Options`, migrate is self-contained — nothing).
Add `cmd/apogee/configbridge.go` — a temporary alias file
(`type options = config.Options`, `type configKey = config.Key`, alias vars/consts as
needed) so every staying file, including `config.go`/`configwrite.go` (which move in
item 10), compiles UNCHANGED. Moved test files change package to `config`.
`configwatch_apply_test.go` STAYS in `cmd/apogee` (it exercises the binary's apply
seam and uses binary-side fixtures `newMCPFixture`/`writeSettingsFixture`); adjust it
to drive the watcher through the exported surface. Give `internal/config` a `doc.go`
stub now (real map lands with item 10). ADR 0010 bind: `internal/config` imports
`internal/domain` and siblings (`internal/tui` for `SpinnerStyle`/`ParseCursorShape`
validation is allowed — sibling import, no cycle), never the root module.

**Tests:** moved tests pass in their new package; staying tests pass unchanged.

**Acceptance:** `go build ./... && go test ./...` green; `make check` green;
`internal/config` exists and does not import the root module; `configbridge.go` exists.

**Commit:** `refactor(config): move key registry, options, migrate, and watch into internal/config behind a bridge`

## 10. Config move II — config.go and configwrite.go follow

Depends on item 9.

**What:** `git mv` `cmd/apogee/config.go`, `cmd/apogee/configwrite.go`,
`config_test.go` (minus the fixtures extracted in item 8), `configwrite_test.go`,
`configwrite_scalar_test.go` into `internal/config`; change package to `config`.
Export the identifiers the staying binary files reference (audit-time list, 28 total —
from `config.go`: `settings`, `serverEntry`, `applyConfig`, `resolveParallelAgents`,
`presentSettings`, `resolveSystemPrompt`, `loadFileConfig`, `contextFilesSettings`,
`systemPromptSettings`, `uiSettings`, `unconfinedHost`, `configFilePath`,
`expandUserPath`, `configSource`, `resolveConfigDir`, `startupUndetermined`,
`validateServers`, `unknownToolNames`; from `configwrite.go`: `parseSettingList`,
`saveConfigSetting`, `resetConfigSetting`, `readConfigForWrite`, `configDocument`,
`scalarTargetIn`, `splitConfigLines`, `commentedExampleLine`,
`hostAcknowledgementSaver`; re-derive the exact list by grep before renaming) and
extend `configbridge.go` with matching aliases so staying files still compile
unchanged. Write `internal/config/doc.go` with the real file map (options.go, keys.go
naming if renamed, config.go, configwrite.go, configmigrate.go, configwatch.go).

**Tests:** the full moved suite passes in package `config`; staying tests pass.

**Acceptance:** `go build ./... && go test ./...` green; `make check` green; no
`config*.go` production file remains in `cmd/apogee` except `configbridge.go`;
`internal/config/doc.go` names every non-test file in the package.

**Commit:** `refactor(config): move config core and configwrite into internal/config`

## 11. Config move III — drop the bridge, qualify call sites

Depends on item 10.

**What:** Delete `cmd/apogee/configbridge.go`. Update every staying `cmd/apogee` file
that used the aliases to call `config.X` directly (audit-time consumers: `root.go`,
`wire.go` + the `wire_*.go` files from items 6–7, `settingsedit.go`, `settingsrows.go`,
`headless.go`, `upstream.go`, `launcher.go`, `probe.go`, `probemodel.go`,
`defaults.go`, `schedule.go`, plus staying test files). Mechanical qualification only —
no signature or behaviour changes.

**Tests:** full suite.

**Acceptance:** `go build ./... && go test ./...` green; `make check` green;
`configbridge.go` gone; `grep -rn 'type options = \|= config\.Options$' cmd/apogee/`
finds nothing.

**Commit:** `refactor(cmd): drop the config bridge; call internal/config directly`

## 12. Mechanisms tidy — compact naming, syntax pair, real doc.go

**What:** In `internal/mechanisms`: (a) `git mv` the five snake_case production files
to compact names — `empty_response.go` → `emptyresponse.go`,
`guided_decomposition.go` → `guideddecomposition.go`, `tool_result_cap.go` →
`toolresultcap.go`, `tool_use_enforcer.go` → `tooluseenforcer.go`,
`truncate_history.go` → `truncatehistory.go` — plus their `_test.go` twins and the
orphan test files `read_list_families_test.go` → `readlistfamilies_test.go`,
`write_detection_test.go` → `writedetection_test.go`. (b) Cure the confusable pair:
`syntax.go` keeps the Mechanism (matches the `validate.go`/`autofix.go` pattern);
rename `syntaxcheck.go` → `syntaxengine.go` and give both file-top comments stating
the split (Mechanism registration vs pure checker engine). (c) Replace the 14-line
`doc.go` stub with a real file map: one line per non-test file (27 files), grouped
sensibly (mechanisms vs shared plumbing).

**Tests:** existing suite carries (renames only); no new tests.

**Acceptance:** `go build ./... && go test ./internal/mechanisms/` green; `make check`
green; `ls internal/mechanisms/*_*.go` matches only `*_test.go` files;
`doc.go` names every non-test file.

**Commit:** `refactor(mechanisms): compact file naming, split syntax pair naming, real doc.go map`

## 13. doc.go map guard test — helper plus first wave (tui, mechanisms, config, cmd/apogee)

Depends on items 3–12.

**What:** (a) New test helper package `internal/docmap`: `Check(t *testing.T)` reads
the calling package's directory (`os.Getwd` — `go test` runs in the package dir),
globs non-test `.go` files, and fails naming any file whose base name does not appear
in that directory's `doc.go` (missing `doc.go` = failure). Pattern precedent:
`TestFoldEventCoversEveryEventVariant`. (b) Fix `internal/tui/doc.go` to name the five
currently-unmapped files explicitly: `colorscheme.go`, `altscreen_other.go`,
`environ_other.go`, `syncoutput_other.go`, `syncoutput_windows.go`. (c) Create
`cmd/apogee/doc.go` with a file map of all its non-test files (including the
`wire_*.go` seam files) — if the doc-landscape cleanup plan's item 10 already created
it, update instead of create, keeping its prose. (d) Add a `docmap_test.go` calling
`docmap.Check` in: `internal/tui`, `internal/mechanisms`, `internal/config`,
`cmd/apogee`.

**Tests:** the four guard tests are the tests; plus one unit test of `docmap.Check`
against a temp-dir fixture (one mapped file, one unmapped → expect failure).

**Acceptance:** `go test ./internal/tui/ ./internal/mechanisms/ ./internal/config/ ./cmd/... ./internal/docmap/`
green; `make check` green; deleting any one line from `internal/tui/doc.go`'s map and
re-running the tui guard test fails (verifier may spot-check and revert).

**Commit:** `test(structure): doc.go file-map guard; maps enforced for tui, mechanisms, config, cmd/apogee`

## 14. File maps wave 2 — tools, domain, platform, agent

Depends on items 1 and 13.

**What:** Rewrite `doc.go` in `internal/tools` (32 files), `internal/domain` (22),
`internal/platform` (18), `internal/agent` (18) so each names every non-test file with
a one-line description (derive from each file's head comment; keep existing doc.go
prose above the map). Add a `docmap_test.go` (calling `internal/docmap.Check`) to each.

**Tests:** the four new guard tests.

**Acceptance:** `go test ./internal/tools/ ./internal/domain/ ./internal/platform/ ./internal/agent/`
green; `make check` green; each doc.go names every non-test file in its package.

**Commit:** `docs(structure): file maps and guard tests for tools, domain, platform, agent`

## 15. File maps wave 3 — processing, security, probe

Depends on item 13.

**What:** Same treatment for the remaining at-threshold packages: `internal/processing`
(11 files), `internal/security` (10), `internal/probe` (10) — doc.go names every
non-test file; add `docmap_test.go` to each. (These sit at the ~10-file threshold the
owner ratified as included.)

**Tests:** the three new guard tests.

**Acceptance:** `go test ./internal/processing/ ./internal/security/ ./internal/probe/`
green; `make check` green.

**Commit:** `docs(structure): file maps and guard tests for processing, security, probe`

---

**Suggested version bump:** none required — the plan is a behaviour-preserving
structure refactor with no user-visible change. If the owner wants the house
per-feature micro-bump, a patch-level bump after item 15 is the natural point; the
owner decides.
