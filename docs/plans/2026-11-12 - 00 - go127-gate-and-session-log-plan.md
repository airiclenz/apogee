# Go 1.27 gate and start-up session log — plan

**Goal:** Make `make check` pass under Go 1.27 as well as go.mod's 1.26, and have CI guard the newest stable Go. Then gather the routine start-up notes into one `✦ Session log (N notes)` transcript block that is collapsed by default, while notices that need the human keep their own line.
**Date:** 2026-11-12
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 67c5af4c

**Sources:** `.capcom/issues/go127-local-check-fails.md`; `.capcom/issues/startup-session-log.md`; ADR 0112 (residual disclosure); ADR 0024 (+amend-1111, rebind); ADR 0011 (Model by value); ADR 0031 (Driver-neutral engine); ADR 0110 D1/D4 (side panel); closed issue `startup-notices-stderr`; `layout.md` §"Collapsed and expanded blocks", §"The start-up box"; `docs/layout/tool-layout.md`; `docs/design/test-drivers.md`.

**Root cause (go127, verified 2026-11-12 with `GOTOOLCHAIN=go1.27.4`):** Go 1.27's `encoding/json` runs on json/v2. It marshals the `map[any]any{1:"x"}` that yaml.v3 decodes a non-string-keyed mapping into as `{"1":"x"}`; 1.26 refused it with `unsupported type`. `RequestExtra.UnmarshalYAML` relied on that refusal. golangci-lint v2.13.2 (x/tools v0.49) fails typecheck on 1.27 export data; v2.14.0 and v2.15.0 lint `./internal/testtime/` clean under 1.27.

**Ratified design calls** (owner, 2026-11-12):
- **Header:** house grammar `✦ Session log (4 notes) ▶`/`▼` (`(1 note)` singular), painted like the Tasks header-alone card.
- **Anomalies:** a notice that needs the human keeps its own `· …` note line outside the log. The log holds routine notes only, and no routine notes means no block.
- **Window:** each session boundary (launch, `/clear`, `/new`, a `/sessions` restore) opens a log. It absorbs routine notes until the session's first Exchange launch (`Model.enterRunning`: a submitted prompt, `/continue`, a workflow wake, `/compact`); later notes print as today. Log notes are ephemeral (never saved). (close point refined at regression check, 2026-11-12)
- **CI:** a new CI job runs `make lint` and `make test` on setup-go `stable`; go.mod stays 1.26.11.
- **Side panel (writer, 2026-11-12):** v1 is transcript-only. ADR 0110 D4 renders panel sections from engine Events only, and start-up notes are not Events; D1 makes the panel a glance at live state. The panel move is filed as follow-up `session-log-side-panel` (item 4).
- **Residual banner (brief, 2026-11-12):** the banner collapses into the log. A residual inside the log counts as disclosed, recorded as an ADR 0112 amendment (item 4).

**Regression check (2026-11-12, 67c5af4c):**
- 1: guard folded (writer decision: the six v2.15.0 errcheck findings are fixed at the site).
- 3: guard folded; yields to ADR 0098 and `docs/manual/building.md` release step 1 ("There is no bypass").
- 4: guard folded (writer decision on Tests; header-alone counting prose; `--id session-log`); yields to closed issue `startup-notices-stderr`.
- 5: guard folded (writer decision: `tui.Notice`, the open-state flag; own fold mark; five `announce` callers; resume test; explicit open).
- 6: recast (writer decision: first-contact beat only, loud roster failure, close in `enterRunning`); yields to the `rebindNote` doc in `internal/tui/heartbeat.go`.
- 5 (re-check): guard extended (writer decision: Approach rewritten to the guard — `tui.Notice`, explicit open flag, close in `enterRunning`/`reset`; `stripEscapes` at the `addSessionLogNote` seam; e2e stub matches no profile, hint or mandatory reasoning; Acceptance covers every named test).
- 6 (re-check): guard extended (writer decision: Approach rewritten to the guard — no submit-path close, first-contact `quietSeed` beat only, `foldRoutingNotice` untouched, `[]tui.Notice`; item 5's e2e wait rewritten; Acceptance covers every named test); yields to ADR 0045 D4 (routing notices stay loud).
- 6 (re-check 2): guard folded (writer decision on regression-4's three remaining G: lines: absorption keys on a new `rebindIntent.firstBeat` set only in `observeBinding` from `firstContact`, never `quietSeed` — `followFallback` also sets it — with a fallback-settle test; manual Acceptance grep added; `TestSwitchEmits…` moved to keep-green and the `[]tui.Notice` test edit scoped to the named reads); supersedes the `quietSeed` key in the line above.

**Standing requirements:**
- skills: coding-standards
- This box: tests only as `APOGEE_TEST_SLOW=1 GOMEMLIMIT=2GiB make test`, or one package and one test (`GOMEMLIMIT=2GiB go test -race -count=1 -run TestX ./pkg/`). Never two test runs at once. Pass this on to every sub-agent.
- Bubble Tea v2: `tea.KeyPressMsg`, matched on `msg.String()`. No `strings.Builder` held by value anywhere the Model reaches (ADR 0011).
- Durations in tests go through `internal/testtime`; `WaitQuiet` never stands in for readiness.
- The closeout gate runs `make check` twice: under the local toolchain and with `GOTOOLCHAIN=go1.27.4`.

**Out of scope:**
- headless and daemon stderr lines (unchanged), and stderr-only launch notices (model profile at launch, starter config, key migration, MCP tool skip).
- `resumed:`/interrupted replay notes, the prebound notice, launcher restore notes, adoption and migration panes: all unchanged.
- The side-panel placement (follow-up issue only); an expand-all toggle; a persisted fold preference.
- Bumping go.mod's `go` line.

## 1. Pin golangci-lint to a release that reads Go 1.27 export data — ✅ DONE (2026-11-12)

NOTES (2026-11-12): each discarded receive is written `case err := <-ch:` / `err := <-ch` then `_ = err // <reason>`, not `_ = <-ch`, because staticcheck S1005 (on under `checks: all`) flags a blank assignment from a receive as unnecessary.

**What:**
**Goal:** `make lint` passes under the go.mod toolchain and under `GOTOOLCHAIN=go1.27.4`.
**Approach (assumed at the header base):** set `GOLANGCI_LINT_VERSION` in `Makefile` to v2.15.0, the newest release at write time and verified to typecheck under 1.27. CI's `lint` job runs `make lint`, so the pin lives once. Fix every finding the new release's standard set raises in the tree; never silence one in `.golangci.yml` without a one-line reason there. The fix is for the issue's lint half: v2.13.2 stops at typecheck with `export data version 5 is greater than maximum supported version 4`.
**Regression guard.** Measured 2026-11-12 (writer): `golangci-lint@v2.15.0 run ./...` at base raises exactly 6 findings, all errcheck on a discarded receive from an error-typed channel — cmd/apogee/e2e_support_test.go (`<-s.drv.Done()`), cmd/apogee/headless_test.go (`<-r.done`), internal/mcp/mcp_test.go (`<-waited`), internal/subprocess/subprocess_unix_test.go (`<-done`), internal/tuitest/driver.go (two `<-d.done`). Fix each at the site (receive into a variable and handle or explicitly discard it with a one-line reason); never disable errcheck. Add those five paths to Files.
**Files:** `Makefile`; `cmd/apogee/e2e_support_test.go`; `cmd/apogee/headless_test.go`; `internal/mcp/mcp_test.go`; `internal/subprocess/subprocess_unix_test.go`; `internal/tuitest/driver.go`; any Go file a v2.15.0 finding names; `.golangci.yml` only if a check is retuned
**Read first:** Makefile — GOLANGCI_LINT_VERSION, GOLANGCI_LINT, lint target, check target; .golangci.yml — linters.settings.staticcheck.checks, exclusions.paths; .github/workflows/ci.yml — lint job (golangci-lint step comment)
**Tests:** none new (tooling).
**Acceptance:**
- `GOMEMLIMIT=2GiB make lint`
- `GOTOOLCHAIN=go1.27.4 GOMEMLIMIT=2GiB make lint`
- `grep -n 'GOLANGCI_LINT_VERSION := v2.15.0' Makefile`
**Commit:** `fix(build): pin golangci-lint v2.15.0 so lint reads Go 1.27 export data`

## 2. Refuse a non-string request-extra key without relying on encoding/json — ✅ DONE (2026-11-12)

**What:**
**Goal:** under Go 1.26 and Go 1.27, `request-extra:` is refused, naming the entry and `JSON`, when any mapping inside it has a key yaml.v3 does not decode as a string, at any depth including inside a list. A quoted numeric key (`"1": x`) is accepted.
**Approach (assumed at the header base):** this fixes a toolchain regression: Go 1.27's json/v2-backed `encoding/json` marshals `map[any]any` with an int key, so `TestServerEntryRequestExtraLoads/a_nested_non-string_key` fails under 1.27. In `RequestExtra.UnmarshalYAML` (`internal/config/config.go`), after `node.Decode(&value)`, walk `value` with one private recursive helper. yaml.v3 yields `map[string]any` when every key is a string and `map[any]any` otherwise, so any `map[any]any` met inside a map or a `[]any` is refused with the existing `requestExtraError` reason (`cannot be sent as JSON — every key must be a string and every number finite`), minus the json error suffix. `json.Marshal` stays for the canonical form and the NaN/Inf refusal. Rewrite the doc comment so it no longer credits encoding/json with the key refusal.
**Files:** `internal/config/config.go`; `internal/config/config_test.go`
**Read first:** internal/config/config.go — RequestExtra.UnmarshalYAML (doc comment), requestExtraError, ServerEntry.UnmarshalYAML, validateRequestExtra, RequestExtra type comment; internal/config/config_test.go — TestServerEntryRequestExtraLoads, TestServerEntryRequestExtraRoundTrips, testConfigHome
**Tests:** new rows in `TestServerEntryRequestExtraLoads`: a top-level non-string key `{1: x}`; one inside a list `{a: [{1: x}]}`; a bool key `{provider: {true: x}}`; a mixed mapping `{provider: {1: x, a: y}}`; and accepted `{provider: {"1": x}}` → `{"provider":{"1":"x"}}`. Bite check under `GOTOOLCHAIN=go1.27.4` (the existing row fails before the fix).
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestServerEntryRequestExtra' ./internal/config/`
- `GOTOOLCHAIN=go1.27.4 GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestServerEntryRequestExtra' ./internal/config/`
**Commit:** `fix(config): refuse a non-string request-extra key under Go 1.27's encoding/json`

## 3. CI job on the newest stable Go

**What:**
**Goal:** `.github/workflows/ci.yml` has a job that runs `make lint` and `make test` on setup-go `go-version: stable`, beside the existing jobs, and `make actionlint` and `./scripts/check-pins.sh` pass.
**Approach (assumed at the header base):** add a job (name `latest-go`, `runs-on: ubuntu-26.04`) copying the `lint` job's checkout and setup-go steps, both SHA-pinned with the tag comment, `persist-credentials: false`. Use `go-version: stable` in place of `go-version-file`, and give it a `cache-dependency-path` of its own so it never shares the check job's cache key. Steps: `make lint`, then `make test`. Add a comment saying it guards the next Go release ahead of dev boxes and that go.mod's `go` line stays the minimum. Add one line to `docs/manual/building.md`'s CI description naming the job. Depends on items 1 and 2; with them it resolves `go127-local-check-fails`.
**Regression guard.** The release gate needs a green run of the whole `ci.yml` (`cmd/release/gate.go` `ciWorkflow`, `hasGreenRun`), so a red `latest-go` job blocks every cut once the next Go breaks the pinned golangci-lint or a test, though the tree is green on go.mod's toolchain. Pick one and state it in the commit and in `building.md`: put the job in its own workflow file (the gate reads `ci.yml` only; the Acceptance grep then reads that file), or mark it `continue-on-error: true`, or say plainly that it gates releases and add that to `building.md`'s release-gate paragraph beside the CI line. The item yields to ADR 0098 and `docs/manual/building.md` release step 1 ("There is no bypass"): it never weakens the gate itself.
**Files:** `.github/workflows/ci.yml` (or a new `.github/workflows/<name>.yml` if the job gets its own workflow); `docs/manual/building.md`
**Read first:** .github/workflows/ci.yml — lint job, check job (setup-go cache-dependency-path comment); cmd/release/gate.go — hasGreenRun, ciWorkflow; docs/manual/building.md — release gate step 1, the make check/CI paragraph (make lint, vulncheck, windows job); scripts/check-pins.sh — pinned regex
**Tests:** none new (workflow).
**Acceptance:**
- `make actionlint`
- `./scripts/check-pins.sh`
- `grep -n 'go-version: stable' .github/workflows/ci.yml`
**Commit:** `ci: lint and test on the newest stable Go`
**Closes:** go127-local-check-fails

## 4. Session-log contract: layout spec, glossary, ADR 0112 amendment, panel follow-up

**What:**
**Goal:** the session log is specified before code. `layout.md` and `docs/layout/tool-layout.md` sketch it collapsed and expanded. The glossary has a `Session log` Note. ADR 0112 carries a dated amendment saying a residual inside the collapsed log is disclosed. The issue `session-log-side-panel` exists.
**Approach (assumed at the header base):**
- `layout.md` §"Collapsed and expanded blocks" and §"The start-up box":
  - The log sits below the box (and below any resume replay). Its header is `✦ Session log (N notes) ▶`/`▼` in the Tasks card's header-alone grammar.
  - Expanded, each note is its own `· <text>` row (multi-line notes keep their lines) in the expanded body tone.
  - Loud notes print outside it. It opens at each session boundary and closes at the session's first Exchange launch (`Model.enterRunning`).
- `docs/layout/tool-layout.md` gains a collapsed/expanded sketch under §"Fold states and interaction".
- Glossary Note `Session log` goes beside the Session Note (`capcom know add --parent glossary-identity`), and the Session Note's list of ephemeral entries names it.
- ADR 0112 amendment `adr-0112-amend-1112`:
  - In the TUI, the residual notice is a routine Session log note: present in the transcript, one keypress from view. That satisfies "the operator is told".
  - stderr, headless and daemon are unchanged.
  - Named on the record's Status line and its reading-order list.
- `capcom create --id session-log-side-panel` (P4, feature): the reason from the header's side-panel call, and a dependency on `startup-session-log`.
**Regression guard.** No test reads layout.md or docs/layout/ (reviewer finding), so Tests reads "none (docs only)" and the `go test … -run 'Layout|Doc|Drift'` Acceptance line is removed.
Revise every sentence that counts or enumerates the header-alone blocks (`grep -n 'header alone\|first of two\|second block\|one block that spends none' layout.md docs/layout/tool-layout.md`), and say the log's fold is per-block and session-only, unlike `task_list`'s shared `ui.task-list-open`. Add the Note with its id — `capcom know add --id session-log --title "Session log: …" --parent glossary-identity` — since `capcom know show "Session log"` resolves only through the id or an alias slug `session-log`. The item yields to closed issue `startup-notices-stderr` (reason 6d160f1e: the confinement notices moved into the transcript because the stderr line was easy to miss): `adr-0112-amend-1112` cites it.
**Files:** `layout.md`; `docs/layout/tool-layout.md`; `.capcom/knowledge/glossary/glossary-identity/session-log.md` (new Note) and `session.md` (Session Note); `.capcom/knowledge/decisions/adr-0112.md`; `.capcom/knowledge/decisions/adr-0112/adr-0112-amend-1112.md`; `.capcom/issues/session-log-side-panel.md`
**Read first:** layout.md — §"Collapsed and expanded blocks" (task_list/umbrella header-alone paragraph), §"The start-up box"; docs/layout/tool-layout.md — §"Fold states and interaction" (task_list and umbrella bullets); .capcom/knowledge/glossary/glossary-identity/session.md — ephemeral-entries list;
.capcom/knowledge/decisions/adr-0112.md — Status line, reading-order sentence; .capcom/knowledge/decisions/adr-0112/adr-0112-decision.md — decision 1 ("the operator is told"); .capcom/knowledge/decisions/adr-0081.md — Status line naming adr-0081-amend-1111 (amendment model)
**Tests:** none (docs only).
**Acceptance:**
- `capcom check`
- `capcom know show "Session log"`
- `capcom know show "ADR 0112" | grep -n 'amend-1112'`
- `capcom show session-log-side-panel`
**Commit:** `docs(layout): specify the start-up session log`

## 5. Session-log block: entry kind, painter, construction-time notes

**What:**
**Goal:** at construction the TUI puts every routine start-up note into one ephemeral `entrySessionLog` block, collapsed, with header `✦ Session log (N notes) ▶`. ⌥↑ then ⏎, or a click, expands it to show each note verbatim. Loud notes stay their own `entryNote` lines. stderr output is byte-for-byte unchanged.
**Approach (assumed at the header base):**
- **Entry kind:** add `entrySessionLog` to `entryKindRules` (`internal/tui/entrykind.go`): `carriesBlockState: true`, `cacheable: false` (it grows in place, as `entryStartup` does), empty `persistedName`, `isHostNote: true`.
- **Painter:** paint it in `renderEntryLines` through `renderToolBlock` with a header-alone fold like the `task_list` card, every row marked `targetHeader`. The existing `toggleBlockAt`/`setExpanded`/block-cursor path then serves it; no new key or handler.
- **Ownership:** the transcript owns the open log, with `addSessionLogNote(text)` (creates on first routine note, appends after; applies `stripEscapes` at its seam, as `addNote`/`addEphemeralNote` do) and `closeSessionLog()`.
- **Classification:** the routing is binding and is a changed representation:
  - `tui.Options.StartupNotices` becomes `[]tui.Notice{Text string; Loud bool}`.
  - Producer: `rootWiring.announce` (`cmd/apogee/wire_boot.go`) takes the loudness. `unconfinedAutoWarning` and `probe.DegradedNotice` are loud; `probe.ResidualNotice` is routine.
  - Consumers: `wire_options.go` `options()`, `Model.noteStartupNotices`, and `TestRunRootConfinementStartupNotices`, which still joins `.Text` and compares it to stderr.
  - `noteContextFiles`: `ContextNotice.Anomaly` ⇒ loud, else routine.
  - `noteColorSchemeWarnings` stays loud.
- **Session boundaries:** construction, `/clear`, `/new` and a `/sessions` restore open a fresh log through the explicit open flag the guard defines; `Model.enterRunning` and `transcript.reset` close it.
**Regression guard.** Define one notice type `tui.Notice{Text string; Loud bool}` (not `StartupNotice`); `tui.Options.StartupNotices` becomes `[]tui.Notice`. The log's "open" state is a transcript flag, distinct from whether a log entry exists: opened at each session boundary (construction, `/clear`, `/new`, a `/sessions` restore), closed by `closeSessionLog()` called from `Model.enterRunning` (the seam every Exchange launch shares — submit, `/continue` startResume, workflow wake, flushInterjections) and cleared by `transcript.reset`. `addSessionLogNote` creates the entry on the first routine note while open; while closed, callers use `addNote`/`addEphemeralNote` as at base. Add `cmd/apogee/wire_verbs.go`'s not-yet-touched representation only in item 6.
Give the log its own fold mark (a `toolView` flag `collapsedCall` honours without `hasTaskRows`, or its own painter), never `collapsesToHeader`, which would make a click flip every task card and write `ui.task-list-open` (`toggleBlockAt` → `foldsWithTaskLists`) and put the log in `setTaskListOpen`'s and `panelTaskCardAt`'s reach. `announce` has five callers: also `shareWarning` (`wire_live.go`) and `announceSkippedMCP` (`mcp_cmd.go`), both loud — the out-of-scope "MCP tool skip" reaches `StartupNotices`; the type change reaches `wire.go` (`startupNotices`), `mcp_cmd_test.go` and `confinement_e2e_test.go`, which keep a `.Text` join wherever stderr is compared.
Open the log explicitly at `newModel`, `resetSessionView` and `resumeLoaded`, before `noteContextFiles` (which returns early with no context files); find the open log by scanning (as `refreshStartup` finds `entryStartup`) or shift its index in `placeBehind`; `addSessionLogNote` calls `touch()`.
`addSessionLogNote` applies `stripEscapes` at its seam, exactly as `addNote`/`addEphemeralNote` do. The item-5 e2e uses an upstream stub whose model matches no built-in model profile, no hint and no mandatory reasoning, so its first beat adds no routine note and the `Session log (1 note)` wait cannot be repainted.
**Files:** `internal/tui/entrykind.go`; `internal/tui/render.go`; `internal/tui/transcript.go`; `internal/tui/model.go`; `internal/tui/tui.go`; `internal/tui/toolbranch.go`; `internal/tui/commandrun.go`; `internal/tui/sessions.go`; `cmd/apogee/wire_boot.go`; `cmd/apogee/wire_options.go`; `cmd/apogee/wire.go`; `cmd/apogee/wire_live.go`; `cmd/apogee/mcp_cmd.go`; tests below, plus `internal/tui/sessions_test.go`, `cmd/apogee/mcp_cmd_test.go`, `cmd/apogee/confinement_e2e_test.go`
**Read first:** internal/tui/toolbranch.go — collapsedCall; internal/tui/transcript.go — foldsWithTaskLists, placeBehind, reset, refreshStartup; internal/tui/model.go — noteContextFiles, noteStartupNotices; cmd/apogee/wire_boot.go — announce
**Tests:**
- Update `TestStartupNoticesPostAsEphemeralNotes` and the `TestContextFilesNotice*` set (`internal/tui/contextfiles_test.go`).
- Update `TestSessionBrowserResumeNotesDoNotAccumulate` (`internal/tui/sessions_test.go`) to assert the `context: AGENTS.md (3.1 KiB)` note inside the log's notes; `TestStartupSkipsARepoServerWhosePin{Moved,IsUnreadable}` (`cmd/apogee/mcp_cmd_test.go`) and `TestE2EAutoDegradationJourneyOnAnIncapableHost` (`cmd/apogee/confinement_e2e_test.go`) read `.Text`.
- New unit tests, each named `TestSessionLog…`: an anomaly stays its own note; no routine notes means no block; the log is never encoded; a `\x1b` in a log note is stripped.
- New `TestSessionLog…` unit tests: a toggle on the log leaves `opts.UI.TaskListOpen` and every task card unmoved and writes nothing; the log opens at a boundary with no context files; `enterRunning` closes it and `transcript.reset` clears it.
- New driven test `TestE2ESessionLogCollapsedThenExpanded` in `cmd/apogee/`. It launches with an `AGENTS.md` against a stub whose model matches no built-in profile, hint or mandatory reasoning, waits for `Session log (1 note)` with no `context:` row visible, then `expandLastBlock` and waits for the `context: AGENTS.md` row. Use `testtime` budgets.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestStartupNotices|TestContextFilesNotice|TestSessionLog|TestEntryKindRules|TestSessionBrowserResumeNotesDoNotAccumulate' ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestRunRootConfinementStartupNotices|TestE2ESessionLog|TestE2EAutoDegradation|TestStartupSkipsARepoServerWhosePin' ./cmd/apogee/`
**Commit:** `feat(tui): gather routine start-up notes into a collapsed session log`
Depends on item 4.

## 6. Session log absorbs first-beat notes until the session's first Exchange launch; manual

**What:** Recast at the regression check (2026-11-12).
**Goal:**
- While a session log is open (until the session's first Exchange launch, `enterRunning`), the routine notes of the first-contact beat join it and raise its count: `mandatoryEffortNote`, `rebindNote`, the rebind `result.Notices` (model profile, roster delta, `hintNotice`).
- Loud ones stay own lines: `rebindFailNote`, `offlineNote`/`onlineNote`, `unknownWindowNote`, `effortClearedNote`, `foldHookNotice`, every `foldRoutingNotice` (`sub-agents: …`), and a fallback settle's notes.
- After the log closes, every note prints exactly as at base. The manual describes the log.

**Approach (assumed at the header base):**
- Add a `firstBeat` field to `rebindIntent`, set only in `observeBinding` from `firstContact`. In `Model.applyRebind` (`internal/tui/heartbeat.go`), only when `intent.firstBeat` and the log is open, route each routine note through `transcript.addSessionLogNote`; every other rebind and beat keeps `addNote` as at base. `foldRoutingNotice` is unchanged.
- `RebindResult.Notices` becomes `[]tui.Notice`; `rootWiring.rebind` marks loudness per guard (b).
- Nothing new closes the log: item 5's `closeSessionLog()` in `Model.enterRunning` and `transcript.reset` close it. Absorbed notes are ephemeral.
- Manual: `docs/manual/commands.md`, beside the navigation-mode paragraph (what the log holds, how to open it, which notices stay loud); `docs/manual/configuration.md` §"Auto mode's blast radius" (the TUI shows the residual inside the log; stderr still prints it).

**Regression guard.** (a) Absorb on `firstBeat && log open` — never on `quietSeed`, because `followFallback` (`internal/tui/heartbeat.go`) also sets `quietSeed: true`, and a `/bg` workflow started before any prompt (`foldBgStarted`, no `enterRunning`) leaves the log open. A `/model` pick (picker.go), the post-`/server` seed (`foldServerSwitch`), a late seed after a failed beat, a fallback settle and every later beat keep `addNote` as at base, honouring the `rebindNote` doc that such seeds are genuine news. (b) `RebindResult.Notices` becomes `[]tui.Notice` (item 5's type); `rootWiring.rebind` (`cmd/apogee/wire_verbs.go`) marks `bindings.Notices` and `hintNotice` routine and the `tools: the profile roster for … was not applied` failure Loud. (c) The log closes in `Model.enterRunning` and `transcript.reset` (item 5), never in submit alone. (d) The e2e first-beat case waits for `Session log (2 notes)` directly — never a "(1 note)" wait on a launch whose first beat repaints it. (e) Red-test grep: `grep -n 'always reasons\|connected:\|model changed\|context window changed\|model profile' internal/tui/*_test.go cmd/apogee/*_test.go`.
**Files:** `internal/tui/heartbeat.go`; `internal/tui/model.go`; `internal/tui/tui.go`; `cmd/apogee/wire_verbs.go`; `internal/tui/heartbeat_test.go`; `cmd/apogee/wire_server_test.go`; `internal/tui/picker_test.go`; `internal/tui/e2e_test.go`; `internal/tui/watch_test.go`; `cmd/apogee/` e2e test file of item 5; `docs/manual/commands.md`; `docs/manual/configuration.md`
**Read first:** internal/tui/heartbeat.go — applyRebind, observeBinding, foldBeat, followFallback, rebindNote, foldRoutingNotice; cmd/apogee/wire_verbs.go — rootWiring.rebind; internal/tui/tui.go — RebindResult;
internal/tui/heartbeat_test.go — wireRebind, fakeRebind, countNotes, noteTexts, TestRebindNoticesSurfaceAsNotes; cmd/apogee/wire_server_test.go — TestRebindReportsARefusedRosterSwapAsANotice;
cmd/apogee/e2e_seat_test.go — awaitNotice, TestE2ESeatChoiceSubAgentsAskRunsTheChildOnTheTarget; internal/tui/model.go — enterRunning; internal/tui/commandrun.go — foldBgStarted
**Tests:**
- Update the `heartbeat_test.go` rows asserting a routine note as its own entry before any prompt (`TestRebindNoticesSurfaceAsNotes` and every site guard (e)'s grep finds). Update or keep green under guard (a) `TestModelPickerAcceptRebindsThroughTheSeam` and its neighbours (`picker_test.go`) and the `e2e_test.go` and `watch_test.go` sites.
- The `[]tui.Notice` test edit covers only the reads the report names: the `RebindResult{… Notices: …}` constructor in `heartbeat_test.go` (:1458) and the `result.Notices` reads in `cmd/apogee/wire_server_test.go` TestRebind* (:91, :126). `ProfileLoadResult.Notices` (:600) stays `[]string`.
- New tests, each named `TestSessionLog…`: with one routine `Options.StartupNotices` entry seeded, a first-beat mandatory-reasoning note joins the log (count 2); the same note after a submitted prompt is its own persisted entry; a rebind failure stays loud; a `/model` pick and the post-`/server` seed before the first prompt keep their own line; a fallback settle (`followFallback`) with the log open keeps its own line; the `tools: the profile roster for … was not applied` notice stays loud.
- Extend `TestE2ESessionLog…`, rewriting item 5's `(1 note)` wait where it extends that test: a second launch whose stub serves `minimax/minimax-m3:exacto` (`cmd/apogee/modelprofile_test.go:45-47`) so the first beat adds a `model profile:` note; wait for `Session log (2 notes)` directly.
- Keep green: `TestSwitchEmitsBothEffortNotesInAStableOrder` (a switch, not a first beat), the `sub-agents: …` `awaitNotice` waits in `TestE2ESeat*` and `TestE2EAnnouncedDelegation*`, and `TestTUIFallback*`.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestRebind|TestSwitch|TestSessionLog|TestUnknownWindow|TestFirstContact|TestModelPicker|TestServerSwitch|TestModelChange|TestWindowOnly|TestBeatScript|TestDelayedConnect|TestLateSeed|TestModellessStart|TestPinnedWindow|TestWatchIsolatesOwner|TestE2EColdStart|TestFallback' ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestE2ESessionLog|TestRebind|TestE2ESeat|TestE2EAnnouncedDelegation|TestTUIFallback' ./cmd/apogee/`
- `grep -n 'Session log' docs/manual/commands.md docs/manual/configuration.md`
**Commit:** `feat(tui): the session log absorbs first-beat notes until the session's first Exchange launch`
**Closes:** startup-session-log
Depends on item 5.
