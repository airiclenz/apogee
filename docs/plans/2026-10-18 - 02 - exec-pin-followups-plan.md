# Exec-pin follow-ups plan

**Goal:** Fix the eight defects deferred out of `2026-10-18 - 00 - project-config-exec-keys-plan`: pin gaps, refusals that abort or stay silent, a shallow options copy, a duplicated security fence, and an adoption pane that hides an answer on a short window.
**Date:** 2026-10-18
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 12dc9fab

**Sources:**
- ADR 0104 (`capcom know show "ADR 0104"`; `.capcom/knowledge/decisions/adr-0104/`)
- ADR 0031 (wire-silent engine), ADR 0101 (regressions never deferred)
- `docs/manual/configuration.md`, `docs/layout/user-questions-layout.md`
- Issues: `capcom show <id>` for each id on a `**Closes:**` line

**Ratified design calls** (owner, 2026-10-18):
- **Scope:** the eight exec-keys follow-up defects only; features and the Windows-only check stay out.
- **argv pins:** argv elements get the same in-root file search as `env:` values (whole text, each word, the part after a word's `=`, each path-list element); ADR 0104 decision 4 is amended.
- **Short adoption pane:** glosses give up wrapped lines (down to one truncated line, then none) before any answer label leaves the pane's window.
- **Re-ask cue:** a pin-mismatch refusal (reaction spawn, or repo MCP server at connect) fires a wire-silent callback that cmd/apogee turns into a config-change wake; the existing reload → adoption path re-asks at the next idle moment. No watchers on pinned files.

**Standing requirements:**
- skills: coding-standards
- Pi 5 box: run tests only as single-package, single-test `GOMEMLIMIT=2GiB go test -race -count=1 -run TestX ./pkg/`; never a package pattern, never two test runs at once. Brief every sub-agent with this.
- Bubble Tea v2 names only (`tea.KeyPressMsg`, `msg.String()`).
- Any authorised deviation from item text lands as a dated NOTES line under the item.

**Out of scope:**
- Feature issues (`lsp-symbol-navigation`, `serve-http-driver`, `context-fill-bench-arm`, `mcp-handler-posture`, `non-tool-advise-slot`, `user-view-reaction-cell`, `sub-agent-shared-files`, `per-server-idle-timeout`).
- `windows-journal-remove-check` (needs a real Windows host).
- Watching pinned script files for edits.

**Regression check (2026-10-18, 12dc9fab):**
- 3: guard folded (argv-search docs incl. user-questions-layout.md §Adoption pane; CHANGELOG re-ask note)
- 5: guard folded (Firing config wake or dated out-of-scope; test on projectRuleWiring, bite on AwaitConfigChange)
- 6: recast (interim startup abort in connectMCPServers so no commit leaves a pin skip silent)
- 7: guard folded (replaces item 6's interim abort; tests edit the pin between options and dial)
- 8: guard folded (collection rule, nil-preserving clone, bounded reflective filler)
- 9: guard folded (one row composition for paint/height/hit; never-elide prose amended; pane-scoped assertions); supersedes docs/layout/user-questions-layout.md:284 on a too-short window
- 6 (re-check): guard folded (reconnect interim in the wire_live.go closure; both interims Close the client; all-or-nothing/fatal comment-and-doc rule via grep, Denied docs widened; cmd/apogee acceptance run)
- 7 (re-check): guard folded (replaces both interims; reconnect note via mcpSession.Skipped + reconnect (note, error) to its three callers, worded from Denied.Name + mcpSentence; redial cost skipped or accepted; wake asserted via AwaitConfigChange; startup test on startupWiring + httptest server)

## 1. One shared pinned-program admission in internal/security — ✅ DONE (2026-10-18)

NOTES (2026-10-18): no CHANGELOG entry — a behaviour-preserving refactor with nothing a user observes; both callers' admission semantics are unchanged (a Pin.Root() of "" now skips the join instead of joining "", which yields the same name).

**What:**
**Goal:** a single `internal/security` helper admits a pinned in-root project program past the exec fence and is the only such check; `isPinnedProgram` (internal/mcp) and `isAdmittedProgram` (internal/tools) no longer exist.
**Approach (assumed at the header base):** add to `internal/security/execsafety.go` `AdmitPinnedProgram(program string, pinned []string) bool` (the body both copies share: absolute-only, `EvalRealPath` equality) and `ResolveProjectProgram(name, projectRoot, workspaceRoot string, box *domain.ConfinementBox, pinned []string) (string, error)` — join a relative multi-segment `name` to `projectRoot` when `projectRoot != ""`, call `ResolveProgram`, admit on `ErrExecFromWritablePath` via `AdmitPinnedProgram`. Call it from the tail of `resolveStdioProgram` (internal/mcp/transport.go, box `nil`, root `cfg.Pin.Root()`) and from `resolveHookProgram` (internal/tools/exec_common.go, box `confinementBox(ctx)`). Delete both private copies. The pin `Changed()` check stays where it is in each caller. Do not collide with `userexec.ResolveProgram`.
**Files:** internal/security/execsafety.go, internal/security/execsafety_test.go, internal/mcp/transport.go, internal/tools/exec_common.go
**Read first:** internal/mcp/transport.go — resolveStdioProgram, isPinnedProgram, changedPinRefusal; internal/tools/exec_common.go — resolveHookProgram, isAdmittedProgram, AdmitProjectPrograms, hookOptions; internal/security/execsafety.go — ResolveProgram, RefuseExecFromWritablePath; internal/security/pathsafety.go — EvalRealPath; internal/mcp/mcp_test.go — fakeProjectPin, repoStdioServer, TestConnect_AdmitsARelativeRepoCommand, TestConnect_RefusesAnUnpinnedOrChangedRepoProgram; internal/tools/exec_common_test.go — TestRunHookSubprocessAdmitsAPinnedProjectProgram; internal/security/execsafety_test.go — TestResolveProgram, plantProgram; internal/agent/syncexec.go — AdmitProjectPrograms call site
**Tests:** new table test `TestAdmitPinnedProgram` (absolute pinned, relative program refused, symlinked root resolves equal, not pinned) and `TestResolveProjectProgram` (relative multi-segment joined to root; bare name not joined; admitted vs refused) in execsafety_test.go. Existing admission tests pass unchanged.
**Acceptance:**
- `go build ./...`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestAdmitPinnedProgram|TestResolveProjectProgram|TestResolveProgram' ./internal/security/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestConnect_' ./internal/mcp/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run TestRunHookSubprocessAdmitsAPinnedProjectProgram ./internal/tools/`
- `! grep -rn 'func isPinnedProgram\|func isAdmittedProgram' internal/`
**Closes:** project-program-admit-dup
**Commit:** `refactor(security): one pinned-program admission serves repo servers and reaction runs`

## 2. ADR 0104 amendment: env display and file search in argv and env — ✅ DONE (2026-10-18)

**What:**
**Goal:** ADR 0104 carries an amendment Note recording that (a) the adoption pane shows each `env:` entry in full (`KEY=VALUE`), and (b) every argv element and every `env:` value is searched for in-root files — the whole text, each word, the part after a word's `=`, each path-list element — and each such existing regular file is pinned by path and SHA-256; `capcom check` passes.
**Approach (assumed at the header base):** add `.capcom/knowledge/decisions/adr-0104/adr-0104-amend-1018.md` (front matter `title: "ADR 0104 amendment of 2026-10-18: …"`, short prose naming decisions 4 and 11 and how each changes), following `.capcom/knowledge/decisions/adr-0096/adr-0096-amend-1018.md`. Leave `adr-0104-decision.md` unedited; in `adr-0104.md` extend the Status line ("decisions 4 and 11 amended by …") and end the intro's reading list with `adr-0104-amend-1018`. Note that an entry adopted before the change whose argv embeds a file (`--x=./file`) is re-proposed once.
**Files:** .capcom/knowledge/decisions/adr-0104.md, .capcom/knowledge/decisions/adr-0104/adr-0104-amend-1018.md
**Read first:** .capcom/knowledge/decisions/adr-0104.md — Status line, intro reading list; .capcom/knowledge/decisions/adr-0104/adr-0104-decision.md — decisions 4 and 11; .capcom/knowledge/decisions/adr-0096/adr-0096-amend-1018.md — amendment Note shape; .capcom/knowledge/decisions/adr-0096.md — Status/reading-list amendment precedent; docs/manual/configuration.md — "Every project file the entry names" bullet (what shipped)
**Tests:** none (record only).
**Acceptance:**
- `capcom check`
- `capcom know show adr-0104-amend-1018` prints both decisions' changes
- `grep -n 'adr-0104-amend-1018' .capcom/knowledge/decisions/adr-0104.md`
**Closes:** adr-0104-pin-record
**Commit:** `docs(adr): amend ADR 0104 for the env display and the argv/env file search`

## 3. An argv element pins every in-root file it names — ✅ DONE (2026-10-18)

NOTES (2026-10-18): CHANGELOG.md is on the item's Files line but is written through this sidecar's CHANGELOG heading, as the run protocol requires; the entry carries the regression-guard upgrade note (re-proposed once).

**What:**
**Goal:** a project file named inside an argv element — e.g. `args: ["--require=./hook.js"]` on a repo MCP server, or the same in a reaction's `run:` — is pinned by content exactly as the same text in an `env:` value is; its change re-proposes the entry and refuses the launch/spawn; the manual's pin bullet says so.
Fix of a pin gap from the exec-keys plan (ADR 0104 decision 4 as amended by item 2).
**Approach (assumed at the header base):** in `internal/config/execpin.go` `fileCandidates`, run each `pinArgvKeys` element through the same search `envValueCandidates` performs; rename that helper to a neutral name (e.g. `nameCandidates`) and update the comments on `pinArgvKeys`, `pinnedFiles`, `fileCandidates`. `VerifyExecPins` rebuilds via `pinText`, so spawn-time checks follow; `adoptedPin.Programs()` widens to the new files (harmless — content-checked). Update the "Every project file the entry names" bullet in `docs/manual/configuration.md` to state one search for argv elements and `env:` values.
**Regression guard.** Every doc and comment that describes argv file search as whole-element follows the new search — find them with `grep -rn 'argv element' docs/ internal/config/` (outside `docs/plans/`), including §Adoption pane "Pinned files" in `docs/layout/user-questions-layout.md` — not only the three named comments. The item's `CHANGELOG.md` entry (under `[Unreleased]`) states that an adopted entry whose argv embeds an in-root file (`--require=./hook.js`) is proposed again once after the upgrade.
**Files:** internal/config/execpin.go, internal/config/execpin_test.go, docs/manual/configuration.md, docs/layout/user-questions-layout.md, CHANGELOG.md
**Read first:** internal/config/execpin.go — fileCandidates, envValueCandidates, pinArgvKeys, pinEnvKey, pinnedFiles, VerifyExecPins, adoptedPin.Programs, changedFiles; internal/config/execpin_test.go — TestExecPinFoldsAFileAnEnvValueNames, TestExecPinResolvesAgainstTheProjectRoot, TestVerifyExecPinsNamesAChangedFile, serverPin, reactionPin, pinnedPaths, writeProjectFile; docs/manual/configuration.md — "Every project file the entry names" bullet; docs/layout/user-questions-layout.md — §Adoption pane "Pinned files"; internal/agent/syncexec_test.go — TestRepoRunRefusesAChangedScript
**Tests:** in execpin_test.go, `TestExecPinFoldsAFileAnArgvElementNames` (`--require=./hook.js` in a server's args and in a reaction `run:`; the file's path appears in `pinnedPaths`; editing it makes `VerifyExecPins` name it). Existing argv/env pin tests pass.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestExecPin|TestVerifyExecPins|TestChangedSince' ./internal/config/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run TestRepoRunRefusesAChangedScript ./internal/agent/`
- `grep -n 'argv element' docs/manual/configuration.md`
**Depends on:** item 2
**Closes:** argv-embedded-file-unpinned
**Commit:** `fix(config): an argv element pins every in-root file it names, as an env value does`

## 4. Drift test: every mcpServerConfig field is in its adoption pin — ✅ DONE (2026-10-18)

**What:**
**Goal:** a test in `internal/config/` fails when `mcpServerConfig` gains a yaml field that `mcpServerConfig.execPin` does not pin (or that `pinFieldOrder` does not order), with an explicit, empty-today exemption list.
**Approach (assumed at the header base):** set every field of an `mcpServerConfig` to a non-empty value via reflection, call `execPin`, read the pinned keys with `splitPin`, and compare against `schemaKeys(reflect.TypeOf(mcpServerConfig{}))` (internal/config/unknownkeys.go), the pattern of `TestRegistryIsBijectionWithFileConfig`. Also assert every key is in `pinFieldOrder`. Test-only.
**Files:** internal/config/execpin_test.go
**Read first:** internal/config/config.go — mcpServerConfig; internal/config/execpin.go — mcpServerConfig.execPin, pinFieldOrder, splitPin, pinFilesKey; internal/config/unknownkeys.go — schemaKeys, schemaKey; internal/config/registry_test.go — TestRegistryIsBijectionWithFileConfig; internal/config/execpin_test.go — serverPin
**Tests:** `TestServerExecPinCoversEveryField`; bite check: temporarily drop one field from `execPin` and confirm the test fails (do not commit that).
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run TestServerExecPinCoversEveryField ./internal/config/`
**Depends on:** item 3
**Closes:** server-pin-field-drift
**Commit:** `test(config): every repo MCP server field is held in its adoption pin`

## 5. A refused reaction spawn re-asks in-session — ✅ DONE (2026-10-18)

NOTES (2026-10-18): the cue fires for a pin whose files moved or could not be checked (Changed error), not for a Project reaction carrying no pin — no file moved, so a re-read has nothing new to ask; checkProjectPin became an Agent method to reach Config.PinMoved.
NOTES (2026-10-18): consequential edit — cmd/apogee/wire_options.go: made necessary by awaitConfigChangeOn growing its wake parameter (configHost.AwaitConfigChange is its production call site, beside the two test sites the plan names).
NOTES (2026-10-18): Firings threaded per the regression guard (firingInputs.pinMoved beside report, wire_firing.go); carrying the session's wake to it needed scheduleWiring.pinMoved (cmd/apogee/schedule.go) and its literal in cmd/apogee/wire_live.go — two files the plan's Files line did not name. Headless and daemon Firings pass none (no pane): out of scope.
NOTES (2026-10-18): consequential edit — docs/manual/reactions.md: made necessary by the refusal now re-asking in-session (the "proposed again" sentence says when and where).
NOTES (2026-10-18): the cmd/apogee test fires the adopted `run:` entry through the Agent's fenced runner (lateEngine.bound().RunProjectReaction — the runner BindProjectRun hands a Driver's Runner), since projectRuleWiring builds no observe Runner; bite checked by dropping PinMoved from projectRuleWiring (AwaitConfigChange then times out). No composition test pins the Firing's cfg.PinMoved.

**What:**
**Goal:** after a pinned reaction script changes mid-session, its next refused spawn wakes the config reload, so the adoption pane re-asks at the next idle moment without a Project config edit or a restart, and adopting the new content arms the reaction.
Fix of a silent refusal from the exec-keys plan.
**Approach (assumed at the header base):** add a wire-silent `func()` hook on `domain.Config` beside `Report`/`BindProjectRun` (e.g. `PinMoved`), called by `checkProjectPin`'s failure path in `internal/agent/syncexec.go` (covers the observe lane, which shares `spawnReactionArgv`). In cmd/apogee wire it to a non-blocking send on a buffered (cap 1) channel that `awaitConfigChangeOn` (wire_server.go) also selects on; the existing `ReloadConfig` → `followReloadedProposals` → `refreshAdoption` path re-classifies and re-asks. internal/agent imports nothing new.
**Regression guard.** A TUI Firing builds its own Agent config (`firingConfig`/`firingInputs`, cmd/apogee/wire_firing.go, which copy `BindProjectRun`/`Report`): thread the session's wake into `firingInputs` beside `report`, or put Firings (and headless/daemon, which have no pane) out of scope in a dated NOTES line. The cmd/apogee test is built on `projectRuleWiring` (projectrules_test.go) with the hand-built `apogee.Config`'s `PinMoved` set to the wiring's wake and a `run: [./scripts/x.sh]` fixture — not `watchedConfig` or `projectNotifyReactionYAML`; its bite is `configHost.AwaitConfigChange` returning true with no watcher moved, since `ProposedRules()` re-resolves from disk. If `awaitConfigChangeOn`'s signature grows, update its two call sites in configwatch_apply_test.go.
**Files:** internal/domain/config.go, internal/agent/syncexec.go, internal/agent/syncexec_test.go, cmd/apogee/wire_boot.go, cmd/apogee/wire_server.go, cmd/apogee/wire.go, cmd/apogee/wire_firing.go, cmd/apogee/projectrules_test.go, cmd/apogee/configwatch_apply_test.go
**Read first:** internal/agent/syncexec.go — spawnReactionArgv, checkProjectPin, RunProjectReaction; internal/domain/config.go — Config.Report, Config.BindProjectRun; cmd/apogee/wire_boot.go — resolveConfig; cmd/apogee/wire_server.go — awaitConfigChangeOn, changesOf; cmd/apogee/wire_options.go — configHost.AwaitConfigChange, ReloadConfig; cmd/apogee/projectrules.go — followReloadedProposals, proposedRules; cmd/apogee/projectrules_test.go — projectRuleWiring, awaitAndReload; internal/agent/syncexec_test.go — fakePin, TestRepoRunRefusesAChangedScript
**Tests:** agent: `TestRepoRunRefusalPokesPinMoved` (changed script → hook called once per refusal). cmd/apogee: `TestChangedReactionScriptReasksInSession` (on `projectRuleWiring`: adopt a `run: [./scripts/x.sh]` repo reaction, edit the script, fire it → `configHost.AwaitConfigChange` returns true with no watcher moved; `ProposedRules()` then holds the entry with the file in `Changed`; adopting re-arms it).
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestRepoRun' ./internal/agent/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestChangedReactionScriptReasksInSession|TestWatchedAdoptionRecordReloadsARepoReaction' ./cmd/apogee/`
**Closes:** reaction-script-edit-silent
**Commit:** `fix(agent): a refused pinned reaction run wakes the reload so the pane asks again`

## 6. Connect skips a repo server whose pin moved — ✅ DONE (2026-10-18)

NOTES (2026-10-18): consequential edit — cmd/apogee/wire_live_test.go: made necessary by the pin-skip exception to "Connect is all-or-nothing" (the all-or-nothing grep finds two test comments here; both now say "all-or-nothing over an endpoint refusal")
NOTES (2026-10-18): consequential edit — cmd/apogee/e2e_egress_test.go: made necessary by the pin-skip exception to "Connect is all-or-nothing" (a test comment the all-or-nothing grep finds)
NOTES (2026-10-18): consequential edit — docs/design/mcp-client.md: made necessary by the pin-skip exception to "Connect is all-or-nothing" (§2 stdio bullet and §3 Connect bullet; the plan's grep omitted docs/design, its stated rule covers it)
NOTES (2026-10-18): both interims share one helper, abortOnSkippedMCP (cmd/apogee/mcp_cmd.go): it Closes the client and returns errors.Join of every skipped server's Denied.Err — with one skip that is exactly today's abort sentence ("mcp: stdio server %q: refusing to launch — <files> changed since you adopted it; it is proposed again for adoption"); item 7 replaces both call sites
NOTES (2026-10-18): the plan's transport.go:712 anchor sits at :688 in the tree (the checkEndpoint comment); the client.go caps comment ("no Client report or notice path") now names Client.Skipped as the one report path, for whole servers only
NOTES (2026-10-18): docs/manual/configuration.md's login paragraph now states the pin-skip exception; at this commit startup still aborts on such a skip (item 6's interim), which item 7's announce-and-continue makes user-visible — no CHANGELOG entry here, item 7 carries it

**What:**
Recast at the regression check (2026-10-18).
**Goal:** in `internal/mcp`, a repo stdio server whose pin no longer matches at connect is skipped — the other servers still connect, `ConnectWith` succeeds — and the skip is reported to the caller with the changed files.
Fix of an all-or-nothing abort from the exec-keys plan.
**Approach (assumed at the header base):** make the `changedPinRefusal` path in `resolveStdioProgram` return a typed error (e.g. `*PinChangedError{Server string; Files []string}`, message unchanged). In `ConnectWith`, an error from `connectOne` that `errors.As` that type is recorded instead of closing the client; expose the records as `Client.Skipped() []Denied` (reuse `Denied`). Every other error keeps today's all-or-nothing behaviour. Update the client.go comment that says there is no report path.
**Regression guard.** No commit may leave a startup pin skip silent — item 6 also makes rootWiring.connectMCPServers (cmd/apogee/mcp_cmd.go) return an error naming the skipped server and its changed files when Client.Skipped() is non-empty (today's abort wording), as an interim that item 7 replaces with the announce-and-continue path; add cmd/apogee/mcp_cmd.go to item 6's Files.
The startup interim calls `client.Close()` before it returns the error (wireSession returns before w.mcpSet holds the client, wire_live.go:140-143,161, so nothing else closes it; today ConnectWith rolls back itself, client.go:162-166). The same interim applies to a live reconnect: in the wire_live.go `newLiveMCP` reconnect closure, a non-empty `client.Skipped()` Closes the client and returns the skip error (today's "reconnect failed: … changed since you adopted it … — previous connections kept"); item 7 replaces both interims.
State the rule, not a list: every comment or doc line saying Connect / the dial is all-or-nothing or fatal gains the pin-skip exception — find them with `grep -rn 'all-or-nothing\|failure is fatal' internal/mcp cmd/apogee docs/manual` (incl. client.go:71,164,187, transport.go:301,712, wire_mcp.go:36,75, wire_settings.go:1548, wire_live.go:127, docs/manual/configuration.md:828) — and the `Denied` / `Denied.Err` docs (client.go:172-180) widen to cover a pin skip; this supersedes the Approach's single "no report path" comment edit.
**Files:** internal/mcp/transport.go, internal/mcp/client.go, internal/mcp/mcp_test.go, cmd/apogee/mcp_cmd.go, cmd/apogee/wire_live.go, cmd/apogee/wire_mcp.go, cmd/apogee/wire_settings.go, docs/manual/configuration.md (plus any further file the all-or-nothing grep finds)
**Read first:** internal/mcp/client.go — ConnectWith, connectOne, Denied, Admit, listServerTools caps comment; internal/mcp/transport.go — resolveStdioProgram, changedPinRefusal, buildStdioTransport; cmd/apogee/mcp_cmd.go — rootWiring.connectMCPServers; cmd/apogee/wire_live.go — wireSession, newLiveMCP closure
internal/mcp/mcp_test.go — TestConnect_RefusesAnUnpinnedOrChangedRepoProgram, repoStdioServer, connectOnly, fakeProjectPin, stdioServerConfig
**Tests:** `TestConnect_SkipsARepoServerWhosePinMoved` (one global + two repo servers, one pin changed → client holds the other two, `Skipped()` names the changed server and file). Update `TestConnect_RefusesAnUnpinnedOrChangedRepoProgram` only where its changed-pin case now asserts a skip. The cmd/apogee startup path (now carrying the interim abort) is re-run through `TestStartupMCPLoginYesLogsInThenConnects`.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestConnect_' ./internal/mcp/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run TestStartupMCPLoginYesLogsInThenConnects ./cmd/apogee/` (run after the internal/mcp line, never alongside it)
**Depends on:** item 1
**Commit:** `fix(mcp): a repo server whose pin moved is skipped at connect, not every server`

## 7. Startup and reconnect announce a skipped repo server and re-ask — ✅ DONE (2026-10-18)

NOTES (2026-10-18): both item-6 interims are gone (abortOnSkippedMCP deleted): connectMCPServers announces each Client.Skipped() entry through w.announce ("apogee: mcp server <name> skipped — <mcpSentence(Denied.Err)>", mcpSkipClause in wire_mcp.go) and pokes w.reloadWake; the newLiveMCP reconnect closure pokes the same wake (cueSkippedMCP) and the skips reach liveMCP.reconnect through mcpSession.Skipped(), which returns (note, err) — the wake fires in the composition-root closure rather than inside liveMCP, which holds no wake
NOTES (2026-10-18): applyAdoptedKinds relays a skip note as part of its returned error ("the adoption is saved, but this session could not apply it: mcp server … skipped — …"), not via w.announce as the item text says: w.announce is a startup-only seam (stderr plus startupNotices, already handed to the TUI at boot), so mid-session it would write over the alternate screen and never reach the transcript; the AdoptRules error is what the answering row shows
NOTES (2026-10-18): the wake's extra redial is accepted, not skipped: the reload it cues diffs `mcp-servers:` against a baseline still holding the skipped server and runs reconnectMCP once more over the remaining servers (startup included); a skip needs a pinned file to move between the config read and the dial, so the cost is rare, and comparing names alone (sameServerNames) would wrongly skip a genuine same-name edit
NOTES (2026-10-18): consequential edit — cmd/apogee/wire_settings_test.go: made necessary by reconnectMCP now returning (note, error)
NOTES (2026-10-18): consequential edit — cmd/apogee/wire_options.go: made necessary by the MCP skip notice joining startupNotices (the StartupNotices comment named only the confinement posture)
NOTES (2026-10-18): consequential edit — docs/manual/configuration.md: made necessary by the skip now being announced and re-proposed rather than aborting (the login paragraph's pin-skip parenthetical)
NOTES (2026-10-18): the reconnect test adopts the repo server before wireSession starts the config watchers, so only the skip can wake the wait; both tests were seen to fail on AwaitConfigChange with the poke removed

**What:**
**Goal:** at startup and on a live reconnect, a repo server skipped for a moved pin yields a notice naming the changed file, the global and other adopted servers stay connected, startup proceeds, and the entry is proposed again in-session.
**Approach (assumed at the header base):** in `rootWiring.connectMCPServers` (cmd/apogee/mcp_cmd.go) announce each `Client.Skipped()` entry via `w.announce` with the `changedPinRefusal` wording, then fire item 5's pin-moved wake. In `liveMCP.reconnect` (cmd/apogee/wire_mcp.go) add the same note to the reconnect report (pattern: `readmitMCP`'s "; mcp server X disconnected — …") and fire the wake.
**Regression guard.** Item 7 replaces item 6's interim abort in connectMCPServers; its tests edit the pinned file after the options are resolved and before the dial (a file moved before the config read is already classified Proposed by the config layer and never reaches connect).
Item 7 names and replaces BOTH item-6 interims: the startup abort in connectMCPServers and the reconnect abort in the wire_live.go `newLiveMCP` closure. The reconnect note path: `mcpSession` (wire.go:422) gains `Skipped() []mcp.Denied` (fakeMCPSession, wire_helpers_test.go:154, implements it) so the closure's skips reach `liveMCP.reconnect`, which returns `(note string, err error)`; its three callers relay the note — the mcp-servers apply (wire_settings.go:1164) as its row note, `readmitMCP` (:1588) appended to its "; mcp server X disconnected — …" report, `applyAdoptedKinds` (projectrules.go:163) via `w.announce` — and the reconnect callers in wire_live_test.go:151,165 and mcp_cmd_test.go:725 follow the new signature. Word every note from `Denied.Name` + `mcpSentence(Denied.Err)`, never `deniedReason` or the unexported `changedPinRefusal` (supersedes the Approach's "changedPinRefusal wording").
Cost: the wake's reload diffs `mcp-servers` against a baseline still holding the skipped server as adopted, so the mcp-servers apply (wire_settings.go:1946) runs reconnectMCP — one extra full redial per skip, startup included. Either skip that redial when the admitted names equal the live set's (readmitMCP's `sameServerNames` pattern, wire_settings.go:1585) or accept the cost in a dated NOTES line under this item.
Tests assert the wake directly — `configHost.AwaitConfigChange` (or the item-5 channel) returns true with no watcher moved — not "ProposedRules() holds the entry", which proposedRules' re-resolve from disk yields with or without the wake. The startup test builds on mcp_cmd_test.go's `startupWiring` + `wireSession` with an httptest MCP server (`newFakeOAuthMCP` pattern) for the global server; the skipped repo server needs no live server (the pin check precedes exec, transport.go:383-396).
**Files:** cmd/apogee/mcp_cmd.go, cmd/apogee/wire_mcp.go, cmd/apogee/wire.go, cmd/apogee/wire_settings.go, cmd/apogee/wire_live.go, cmd/apogee/projectrules.go, cmd/apogee/wire_helpers_test.go, cmd/apogee/wire_live_test.go, cmd/apogee/mcp_cmd_test.go, cmd/apogee/configwatch_apply_test.go
**Read first:** cmd/apogee/mcp_cmd.go — rootWiring.connectMCPServers; cmd/apogee/wire_mcp.go — liveMCP.reconnect, mcpReconnectFailed, mcpSentence; cmd/apogee/wire_settings.go — readmitMCP, deniedReason, mcpNoteFor, reconnectMCP, mcp-servers apply
cmd/apogee/projectrules.go — applyAdoptedKinds; cmd/apogee/wire_live.go — wireSession, newLiveMCP closure; cmd/apogee/wire_boot.go — announce, startupNotices
cmd/apogee/mcp_cmd_test.go — startupWiring, newFakeOAuthMCP, TestStartupMCPLoginReconnectNeverPrompts; cmd/apogee/wire_helpers_test.go — fakeMCPSession, newMCPFixture
**Tests:** `TestStartupSkipsARepoServerWhosePinMoved` and `TestReconnectSkipsARepoServerWhosePinMoved` (each edits the pinned file after the options are resolved and before the dial; notice text names the file; global server connected; the wake fires — `AwaitConfigChange` returns true with no watcher moved). `TestWireSessionConnectsMCPUnderTheConfiguredURLGuard`, `TestMCPReconnectUsesTheLiveURLSafetyLists` and `TestStartupMCPLoginReconnectNeverPrompts` keep passing on the new reconnect signature.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'SkipsARepoServerWhosePinMoved|TestWatchedProjectConfigAdoptedServerConnects|TestWireSessionConnectsMCPUnderTheConfiguredURLGuard|TestMCPReconnectUsesTheLiveURLSafetyLists|TestStartupMCPLoginReconnectNeverPrompts' ./cmd/apogee/`
**Depends on:** items 5, 6
**Closes:** repo-pin-connect-abort
**Commit:** `fix(apogee): a repo server whose pin moved is skipped with a notice and asked again`

## 8. cloneOptions deep-copies every collection

**What:**
**Goal:** `cloneOptions` returns a `config.Options` sharing no slice or map backing storage with its source, at any depth, and a reflection test fails when any collection field of `config.Options` is not deep-copied.
Fix of a shallow copy: `Proposals`, `AllowRules`, and also `UnconfinedHosts`, `MCPServers`, `ToolsEnabled`, `DangerousRules`, `Overrides`, `ProjectKeys`, `GlobalKeys` are not copied.
**Approach (assumed at the header base):** each owning type gets its own clone (binding: the deep copy of a type lives with the type) — `AllowRules.Clone` (internal/config/allow.go), `ProjectProposals.Clone` with a recursive `ProposalField` copy (internal/config/execlayer.go, helper on `domain.Proposal` in internal/domain/config.go), `DangerousRuleSet.Clone` (options.go); nested slices in `mcp.ServerConfig` cloned element-wise. `cloneOptions` calls them; its doc comment drops the hand count for a pointer to the test. Interface and func fields are shared by design.
**Regression guard.** Scope is a rule: every slice, map and pointer-to-collection reachable from `config.Options`, element-wise through `Servers`, `StartupEntry`, `Reactions`, `ModelProfiles`, `MCPServers`, `SystemPrompt`, `Sessions` (e.g. `SystemPrompt.Layers`, `Sessions.WatchHomes`, `Reactions[i].On`, `ModelProfiles[i].Profile.Tools.Disabled/Enabled`, `MCPServers[i].EnvAllowlist`), each clone with its type. A clone keeps nil exactly where the source was nil (pointer, slice, map), and a non-nil `EnvAllowlist` gets a fresh pointer to `slices.Clone(*p)` (`env-allowlist: []` scrubs the env; nil does not). The test's filler caps recursion (one nested level of a self-referential type such as `ProposalField.Fields`), follows pointers to the collections behind them, exempts interface/func/scalar-pointer fields by kind, and skips unexported fields.
**Files:** cmd/apogee/wire_settings.go, cmd/apogee/wire_settings_test.go, internal/config/allow.go, internal/config/execlayer.go, internal/config/options.go, internal/domain/config.go, internal/domain/reaction.go, internal/mcp/transport.go, internal/profiles/entry.go
**Read first:** cmd/apogee/wire_settings.go — cloneOptions, newLiveSettings, liveSettings.options, firingBinding, rebindInputs; cmd/apogee/wire_settings_test.go — TestLiveSettingsOptionsFollowEveryApply; internal/config/options.go — Options, DangerousRuleSet; internal/config/execlayer.go — ProjectProposals; internal/config/allow.go — AllowRules; internal/domain/config.go — Proposal, ProposalField, ToolRosterDelta; internal/mcp/transport.go — ServerConfig.EnvAllowlist; internal/config/config.go — ServerEntry, SystemPromptSettings, SessionSettings
**Tests:** `TestCloneOptionsSharesNoCollection` in wire_settings_test.go: reflection fills every slice/map in `config.Options` (recursively, bounded and through pointers as the guard says), clones, and asserts no shared backing array/map at any depth (interfaces/funcs exempt) and that `&[]string{}` survives as non-nil. `TestLiveSettingsOptionsFollowEveryApply` passes.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestCloneOptionsSharesNoCollection|TestLiveSettingsOptionsFollowEveryApply' ./cmd/apogee/`
- `go build ./...`
**Closes:** clone-options-shallow-proposals
**Commit:** `fix(apogee): cloneOptions deep-copies every collection config.Options carries`

## 9. The adoption pane keeps every answer on a short window

**What:**
**Goal:** when the adoption / Allow-rule pane's answers with wrapped glosses do not fit, glosses give up wrapped lines (to one truncated line, then none) before any answer label leaves the window; at 50x21 every answer label is visible without scrolling; the layout doc states the rule.
Regression from `e2d1ad58` (before it, all three answers showed at 50x21 with glosses cut).
**Approach (assumed at the header base):** in `Model.answersBudget` (internal/tui/listsurface.go), after the pad-drop retry still leaves `shown < demand`, cap each answer row's gloss height (step down the wrapped gloss to one line with an ellipsis, then label only) until the block fits; render through the shared row painter without changing `popupWrappedRowHeights` for the ask/settings callers. Add a "Too short for the answers" paragraph under §Adoption pane in `docs/layout/user-questions-layout.md`.
**Regression guard.** `answersBudget` returns the capped rows and `listSpec` puts them in `popupSpec.rows`, so paint, `listHeight` and `popupPaneHit` read one composition (a `popupSpec` field instead adds internal/tui/popup.go to Files). Every comment or doc line saying an answer row wraps whole / never elides gains the short-window exception (`grep -rn -i 'elid\|wrap whole\|wraps under the gloss' internal/tui docs/layout/user-questions-layout.md` — `listContent.answers`, `pickerOffering.answers`, the `answersBudget` doc, user-questions-layout.md "Too tall for the window" and :284); on a too-short window this supersedes docs/layout/user-questions-layout.md:284's gloss wrap (owner's "Short adoption pane" call). The test asserts no `glyphScrollThumb` inside the pane's rows (`paneInteriors`) and each label at the start of a pane interior row, not `strings.Contains(frame, …)`.
**Files:** internal/tui/listsurface.go, internal/tui/picker.go, internal/tui/adoptionbody_test.go, docs/layout/user-questions-layout.md
**Read first:** internal/tui/listsurface.go — answersBudget, listSpec, listContent, renderListPlaced, listHeight; internal/tui/popup.go — popupWrappedRowHeights, popupRowBlocks, popupRowHangingIndent, popupRowLines, popupRowLineCount; internal/tui/adoption.go — adoptionRows, adoptionOfferingRows; internal/tui/adoptionbody_test.go — adoptionFrame, paneInteriors, docsSearchServer, TestAdoptionPaneKeepsItsAnswersOnAShortTerminal; internal/tui/picker.go — pickerAdoption offering (answers: true); internal/tui/model.go — popupBudget
**Tests:** `TestAdoptionPaneKeepsEveryAnswerAtFiftyByTwentyOne` via `adoptionFrame(t, 50, 21, …)` (each label at the start of a pane interior row via `paneInteriors`; no `glyphScrollThumb` inside the pane's rows) plus a 50x22 case; `TestAdoptionPaneKeepsItsAnswersOnAShortTerminal` passes.
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestAdoptionPane' ./internal/tui/`
- `grep -n 'Too short for the answers' docs/layout/user-questions-layout.md`
**Closes:** adoption-pane-short-window
**Commit:** `fix(tui): the adoption pane drops gloss lines before an answer on a short window`
