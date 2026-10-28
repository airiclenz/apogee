# Project pin admits the launch; Adoption answers from one snapshot

**Goal:** A Project pin has one operation that verifies a launch, and both launch kinds take its one typed refusal. Adoption then lives in `internal/project`: one snapshot per event, one `Answer` call that does the ADR 0096 §4 held check and writes the record, and Drivers apply only the difference.
**Date:** 2026-10-27
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 7ad25267
**Sources:**
- `docs/reviews/architecture-review-2026-10-27.html` — items 3 (Project pin) and 6 (Adoption snapshot)
- ADR 0104 (§4, §8, amend-1018) · ADR 0096 (§2, §4) · ADR 0010 · ADR 0031
- Glossary: Adoption, Project config, Allow rule, Driver

**Ratified design calls** (owner, 2026-10-27):
- **Unreadable pin:** treated as moved on both sides. An MCP repo server whose pin cannot be checked is skipped and cues a re-read, not a connect failure (the re-read drops it with its refused notice).
- **Admit scope:** `Admit` verifies the pinned files and returns the launch dir and admitted programs. argv[0] resolution stays where it is (MCP's `ResolveProjectProgram` call; the reaction funnel, after the permit).
- **Placement:** a new package `internal/project` owns the snapshot, `Answer` and the held check. The record store stays in `internal/adoption`.
- **Snapshot shape:** proposals leave `config.Options`. The snapshot owns `Proposals` and the inert Allow lists (`Proposed`, `Rejected`); `cloneOptions` loses them.
- **Wording** (writer, preserves behaviour): every refusal and notice string stays byte-identical; each caller wraps the one typed refusal with its own prefix.
- **Re-ask cue** (writer, preserves behaviour): MCP stays pull-based (`Client.Skipped`); the agent pokes `PinMoved` on `errors.As(*domain.PinMovedError)`. Nil pin: callers refuse, no cue.
- **Held check** (writer, preserves behaviour): project-only layered load with the record off, so a broken global config still records an answer.
- **Out of the snapshot:** the adoption watcher's self-triggered reload stays (idempotent today).

**Closes:** pin-launch-protocol-copied; adoption-rereads-config

**Standing requirements:**
- skills: coding-standards
- Test-run rules for this machine (pass them to every sub-agent): run only `GOMEMLIMIT=2GiB go test -count=1 -run '<pattern>' ./<one pkg>/`. Never `-race` or `-cover` over a whole package. Never two test runs at once.
- User- and model-facing bytes stay identical, except the ratified unreadable-pin change.

**Out of scope:**
- Moving argv[0] resolution into the pin.
- Suppressing the adoption watcher's reload of a record this process wrote.
- Review item 9 (one repo-entry pipeline) and the Driver host (item 7).

**Regression check (2026-10-27, 7ad25267):**
- 1: guard folded (writer decision: an unreadable pin is dropped on the re-read; Note acceptance; portable unreadable row)
- 2: guard folded (writer decision on the unreadable wording; docs/design/mcp-client.md; unreadable fixture and skips)
- 4: guard folded (getter grep widened to docs; Admit named in ResolveProjectProgram's doc)
- 5: guard folded (offer returned beside *StartupUndetermined; Acceptance widened)
- 6: guard folded (config.Options.Clone; engineAllowRules moves with proposalsOf)
- 7: recast (writer decision: project.Record; cmd copies stay until items 9/10) plus guards folded
- 8: guard folded (loader error unwrapped; direct door tests in Acceptance)
- 9: recast (writer decision: addProjectRule through project.Record; Diff by fingerprint over in-force snapshots) plus guards folded
- 10: guard folded (writer decision: deletes projectRulesHeld; ApplyConfigOffer; non-fatal post-write load error; Acceptance names fixed)
- 11: recast (writer decision: owns the offer seam to the registry `allow` key) plus guards folded
- 12: guard folded (writer decision: Files rule and field-declaration greps); yields to internal/config/registry.go's allow-row doc
- 7 (second round): recast (writer decision: project.Record only writes adopt/reject/forget, project.Load takes every next snapshot; removeGlobalRule writes no record) plus guards folded (readFile in Answer's Goal signature; no write when nothing is held)
- 9 (second round): recast (writer decision: removeGlobalRule never records; removeProjectRule records Forget then loads after the file write; Diff's Allow compares the whole effective Rules; holder replaced whole after every answer; store-failure path still refreshes and installs; door nil guards kept)
- 11 (second round): guard folded (writer decision per regression-5: `allow` keeps Read/Structure(Options) plus an offer-aware projection field; settingsRows(opts) stays a wrapper; settingsHost.Rows reads the boot offer)
- 12 (second round): guard folded (writer decision: newLiveSettings' boot seed takes its offer from ApplyConfigOffer)

## 1. ProjectPin gains Admit and one typed refusal — ✅ DONE (2026-10-28)

NOTES (2026-10-28): adoptedPin.Admit builds PinLaunch.Programs through the existing Programs method (which reads splitPin), so the sorted-programs logic stays in one place.

**What:**
**Goal:** `domain.ProjectPin` declares `Admit() (domain.PinLaunch, error)`. On success `PinLaunch{Dir, Programs}` holds the Project root and the absolute pinned programs. A changed or unreadable pin returns `*domain.PinMovedError` (`Files []string`, `Err error`, `Unwrap`). Its `Error()` is `"<files joined \", \"> changed since you adopted it; it is proposed again for adoption"` for changed files and `Err.Error()` for an unreadable pin. `config`'s pin implements it. A glossary Note `Project pin` defines the term.
**Approach (assumed at the header base):**
- Add `Admit`, `PinLaunch` and `PinMovedError` in `internal/domain/reaction.go`. The getters stay on the interface for now. Keep the DeepEqual note.
- `config.adoptedPin.Admit` (`internal/config/execpin.go`) calls `VerifyExecPins` once, then builds `PinLaunch` from `root` and `splitPin`.
- Add `Admit` to the test fakes `fakeProjectPin` (`internal/mcp/mcp_test.go`) and `fakePin` (`internal/agent/syncexec_test.go`), built from their existing fields.
- Add the Note with `capcom know add --parent glossary-safety`, beside `adoption`.
**Regression guard.** An unreadable pin is refused as moved and cues a re-read, but on that re-read config drops the entry with its refused notice (internal/config/execlayer.go pinRepoServers/pinRepoReactions) rather than re-proposing it — the `Project pin` Note says so.
The unreadable row is built portably from `adoptedPin{root, text: "not a pin"}` (VerifyExecPins refuses it), or by chmod 0 with the windows and `os.Geteuid() == 0` skips (as `configsplice_test.go` does). Acceptance also reads the Note back: `capcom check` alone passes without it.
**Files:** internal/domain/reaction.go, internal/config/execpin.go, internal/config/execpin_test.go, internal/mcp/mcp_test.go, internal/agent/syncexec_test.go, .capcom/knowledge/glossary/glossary-safety/
**Read first:** internal/domain/reaction.go — ProjectPin; internal/config/execpin.go — adoptedPin, VerifyExecPins, splitPin; internal/config/execpin_test.go — TestVerifyExecPinsNamesAChangedFile;
internal/mcp/mcp_test.go — fakeProjectPin; internal/agent/syncexec_test.go — fakePin; internal/config/configsplice_test.go — Geteuid skip
**Tests:** `TestAdoptedPinAdmit*` in `execpin_test.go` over real temp files: intact → dir and sorted programs; changed file → `PinMovedError.Files` and the exact sentence; unreadable (`adoptedPin{root, text: "not a pin"}`, or chmod 0 with the windows/root skips) → `PinMovedError.Err`, `errors.As` matches.
**Acceptance:** `go build ./... && GOMEMLIMIT=2GiB go test -count=1 -run 'TestAdoptedPin|TestVerifyExecPins' ./internal/config/ && capcom check && capcom know show "Project pin"`
**Commit:** `feat(domain): a Project pin admits a launch or refuses it as moved`

## 2. The MCP launcher admits through the pin — ✅ DONE (2026-10-28)

NOTES (2026-10-28): resolveStdioProgram now returns the start directory beside the program (PinLaunch.Dir for a Project server, empty for a global one), so buildStdioTransport sets cmd.Dir from the admitted launch rather than reading the pin again.
NOTES (2026-10-28): the manual line changed is docs/manual/configuration.md's MCP login parenthetical (the only manual line describing a repo server's pin skip); the per-spawn bullet there speaks of mismatches only and stays.

**What:** Depends on item 1. Ratified behaviour change: an unreadable pin now skips the server and cues a re-ask.
**Goal:** `internal/mcp` launches a repo stdio server only through `Pin.Admit`. `ConnectWith` skips a server whose error matches `*domain.PinMovedError` (changed or unreadable) and connects the rest; `mcp.PinChangedError` no longer exists. The startup notice for a changed file is byte-identical: `apogee: mcp server <name> skipped — stdio server "<name>": refusing to launch — <file> changed since you adopted it; it is proposed again for adoption`.
**Approach (assumed at the header base):**
- `resolveStdioProgram` (`internal/mcp/transport.go`) keeps its nil-pin refusal, calls `Admit`, wraps any refusal as `"refusing to launch — %w"`, then calls `security.ResolveProjectProgram(cfg.Command, launch.Dir, ws, nil, launch.Programs)`.
- `buildStdioTransport` sets `cmd.Dir = launch.Dir`.
- `ConnectWith` (`internal/mcp/client.go`) matches `*domain.PinMovedError`. Delete `PinChangedError` and `changedPinRefusal`.
- Every manual line describing a repo server whose pin cannot be checked (`grep -rn 'proposed again\|pin' docs/manual/`) follows the new behaviour.
**Regression guard.** The manual line and the new unreadable-pin test say the server is skipped and the re-read drops it with its refused notice, never "proposed again" for the unreadable case.
`docs/design/mcp-client.md`'s Connect bullet (`*PinChangedError`, moved-only skip) follows too: every non-archived doc naming `PinChangedError` or the moved-only exception. The unreadable test chmods 0 `bin/wiki.sh` after `adoptedRepoWikiServer`'s options resolve (config load drops an unreadable file itself), restores the mode in Cleanup, and skips on windows and `os.Geteuid() == 0`.
**Files:** internal/mcp/transport.go, internal/mcp/client.go, internal/mcp/mcp_test.go, cmd/apogee/mcp_cmd_test.go, docs/manual/, docs/design/mcp-client.md
**Read first:** internal/mcp/transport.go — resolveStdioProgram, buildStdioTransport, PinChangedError; internal/mcp/client.go — ConnectWith; internal/mcp/mcp_test.go — TestConnect_SkipsARepoServerWhosePinMoved;
cmd/apogee/mcp_cmd_test.go — adoptedRepoWikiServer, awaitPinWake; docs/design/mcp-client.md — Connect bullet
**Tests:** `TestConnect_RefusesAnUnpinnedOrChangedRepoProgram`'s "pin unreadable" row now skips; `TestConnect_SkipsARepoServerWhosePinMoved` matches `*domain.PinMovedError` and drops its `.Server` assert (PinMovedError has no Server); new `TestStartupSkipsARepoServerWhosePinIsUnreadable` (chmod 0 after the options resolve, mode restored in Cleanup, windows/root skips; skip notice + `reloadWake` poke; the re-read drops the server with its refused notice). `TestStartupSkipsARepoServerWhosePinMoved` and `TestReconnectSkipsARepoServerWhosePinMoved` unchanged.
**Acceptance:** `! grep -rn --exclude-dir=archived 'PinChangedError\|changedPinRefusal' internal/ cmd/ docs/design/`; `go build ./... && GOMEMLIMIT=2GiB go test -count=1 -run 'TestConnect' ./internal/mcp/`, then `GOMEMLIMIT=2GiB go test -count=1 -run 'SkipsARepoServer' ./cmd/apogee/`
**Commit:** `fix(mcp): an unreadable repo pin skips the server instead of failing connect`

## 3. The reaction spawner admits through the pin — ✅ DONE (2026-10-28)

NOTES (2026-10-28): internal/agent/syncexec_test.go needed no change — its fakePin already implements Admit (item 1) and every TestRepoRun* test passes unchanged, as the item's Tests line requires.
NOTES (2026-10-28): .capcom/issues/reaction-script-edit-silent.md still cites `changedPinNotice`/`checkProjectPin` at syncexec.go:141-155; left untouched (issue files are not the implementer's to edit) — its line reference is now stale.

**What:** Depends on item 1.
**Goal:** `internal/agent` spawns a repo reaction only through `Pin.Admit`, and pokes `PinMoved` exactly when the refusal matches `*domain.PinMovedError`. Every refusal string is byte-identical: `apogee: reaction "<id>": refusing to run — <PinMovedError text>`, and the nil-pin refusal unchanged with no poke.
**Approach (assumed at the header base):**
- `checkProjectPin` (`internal/agent/syncexec.go`) calls `Admit` and returns the `PinLaunch`. It pokes `notePinMoved` on `errors.As(*domain.PinMovedError)`. `changedPinNotice` goes.
- `spawnReactionArgv` takes `dir` and `AdmitProjectPrograms` from the `PinLaunch`. The permit-first order stays.
**Files:** internal/agent/syncexec.go, internal/agent/syncexec_test.go
**Read first:** internal/agent/syncexec.go — spawnReactionArgv, checkProjectPin, changedPinNotice, notePinMoved; internal/agent/syncexec_test.go — fakePin, TestRepoRunRefusalPokesPinMoved;
internal/config/execlayer.go — pinRepoReactions, resolveRepoReactions
**Tests:** `TestRepoRunRefusesAChangedScript` and `TestRepoRunRefusalPokesPinMoved` unchanged (changed → 2 pokes, unreadable → 2, nil → 0); `TestRepoRunRunsInsideTheExecFence` unchanged.
**Acceptance:** `go build ./... && GOMEMLIMIT=2GiB go test -count=1 -run 'TestRepoRun' ./internal/agent/`, then `GOMEMLIMIT=2GiB go test -count=1 -run 'TestChangedReactionScriptReasksInSession' ./cmd/apogee/`
**Commit:** `refactor(agent): the reaction spawner admits through the Project pin`

## 4. ProjectPin is Admit only — ✅ DONE (2026-10-28)

NOTES (2026-10-28): adoptedPin's Root and Changed getters are inlined away (Admit already read p.root and called VerifyExecPins directly); Programs is kept as the unexported helper programs() that Admit calls.
NOTES (2026-10-28): TestRepoRunRefusalPokesPinMoved's unreadable-pin row reads the root through a type assertion on its own fakePin (r.Pin.(fakePin).root) rather than through Admit, since that row's pin is already refusing as changed and its Admit returns no launch.

**What:** Depends on items 2 and 3.
**Goal:** `domain.ProjectPin` declares only `Admit`. No non-test code outside `internal/config` reads a pin's root, changed set or program list. `ResolveProjectProgram`'s doc names `Admit` as the freshness check, instead of saying the caller owns it.
**Approach (assumed at the header base):**
- Drop `Root`, `Changed` and `Programs` from the interface and the fakes. Make `adoptedPin`'s getters unexported or inline them.
- Rewrite `TestAdoptedRepoMCPServerCarriesItsPin` and `TestAdoptedRepoReactionCarriesItsPin` (`internal/config/execlayer_test.go`) and `TestRepoRunRefusalPokesPinMoved`'s `r.Pin.Root()` use over `Admit`.
- Rewrite every comment naming the getters (`grep -rn 'Root()\|Changed()\|Programs()' internal/ cmd/`, pin context only).
**Regression guard.** Every comment or doc naming a ProjectPin getter, with or without parens, follows: `grep -rn 'ProjectPin\.\(Root\|Changed\|Programs\)\|Pin\.\(Root\|Changed\|Programs\)' internal cmd docs --exclude-dir=archived --exclude-dir=plans` is empty (`docs/design/confinement-execution-contract.md:1509` names `domain.ProjectPin.Changed`).
The Goal is checked as: ProjectPin's method set is {Admit} (mcp/agent reading the returned `PinLaunch.Programs` is not a getter); `ResolveProjectProgram`'s doc names `Admit` as the freshness check.
**Files:** internal/domain/reaction.go, internal/config/execpin.go, internal/config/execlayer_test.go, internal/security/execsafety.go, internal/mcp/mcp_test.go, internal/agent/syncexec_test.go, docs/design/confinement-execution-contract.md
**Read first:** internal/domain/reaction.go — ProjectPin; internal/config/execpin.go — adoptedPin; internal/config/execlayer_test.go — TestAdoptedRepoMCPServerCarriesItsPin, TestAdoptedRepoReactionCarriesItsPin;
internal/security/execsafety.go — ResolveProjectProgram; internal/agent/syncexec_test.go — TestRepoRunRefusalPokesPinMoved; docs/design/confinement-execution-contract.md — Repo reactions amendment
**Tests:** the rewritten execlayer pin tests assert through `Admit`.
**Acceptance:** `! grep -rn 'ProjectPin\.\(Root\|Changed\|Programs\)\|Pin\.\(Root\|Changed\|Programs\)' internal cmd docs --exclude-dir=archived --exclude-dir=plans`; `grep -q 'Admit' internal/security/execsafety.go`; `go build ./... && go vet ./internal/... && GOMEMLIMIT=2GiB go test -count=1 -run 'CarriesItsPin|TestAdoptedPin' ./internal/config/`, then `-run TestRepoRun ./internal/agent/`, then `-run TestConnect ./internal/mcp/`
**Commit:** `refactor(domain): a Project pin exposes only Admit`

## 5. config returns the project offer beside Options — ✅ DONE (2026-10-28)

NOTES (2026-10-28): ProjectOffer, its Clone and the offerOf helper live in execlayer.go beside ProjectProposals; allow.go needed no change (an AllowRule list clones with slices.Clone).
NOTES (2026-10-28): OverlayProjectOffer answers the zero offer whenever no project layer is taken (no file, nothing stated, or a skipped layer), rather than echoing base's own Proposals; for a global-only base the two are the same.
NOTES (2026-10-28): any error other than *StartupUndetermined answers the zero offer from ApplyConfigOffer and LoadLayeredOffer.

**What:** Depends on item 4.
**Goal:** `internal/config` exports a `ProjectOffer` value: the structured `ProjectProposals` plus the Allow lists `Proposed` and `Rejected`. `LoadLayeredOffer`, `OverlayProjectOffer` and `ApplyConfigOffer` return it beside the resolved `Options`, from the same read. The existing functions still fill `Options.Proposals` and `Options.AllowRules.Proposed/Rejected`, and no caller changes.
**Approach (assumed at the header base):**
- In `internal/config/layer.go` and `config.go`, build the offer where `ResolveOptions` sets `opts.Proposals` (`stated.proposals()`) and the effective Allow lists.
- The old names wrap the new ones.
- `ProjectOffer.Clone` reuses `ProjectProposals.Clone` and `AllowRule` cloning.
**Regression guard.** `ApplyConfigOffer` returns the filled offer beside `*StartupUndetermined`, as `opts` stays filled (`ApplyConfig` returns that error LAST; `root.go` and `project_cmd.go` go on with the options). The Acceptance runs the existing layered tests (`TestLayeredConfig*`; no `TestLoadLayered*` exists) and every test pinning the old names' proposal fields.
**Files:** internal/config/layer.go, internal/config/config.go, internal/config/execlayer.go, internal/config/allow.go, internal/config/layer_test.go, internal/config/config_test.go
**Read first:** internal/config/layer.go — LoadLayeredConfig, OverlayProjectLayer, layerKeys.proposals; internal/config/config.go — ResolveOptions, ApplyConfig, StartupUndetermined;
internal/config/execlayer.go — ProjectProposals.Clone; cmd/apogee/root.go — the StartupUndetermined branch
**Tests:** `TestLoadLayeredOffer*`: the offer equals the `Options` fields for a project holding proposed, adopted and rejected entries of each kind; an overlay with no project file yields an empty offer. A `TestApplyConfigOffer` row for a config naming no startup server: `*StartupUndetermined` and the filled offer.
**Acceptance:** `go build ./... && GOMEMLIMIT=2GiB go test -count=1 -run 'Offer|TestLayeredConfig|TestOverlayProject|TestRepo|TestAdoptedRepo|TestAllowRules|TestResolveOptions' ./internal/config/`
**Commit:** `feat(config): the layered loaders return the project offer`

## 6. internal/project: the Adoption snapshot and its diff — ✅ DONE (2026-10-28)

NOTES (2026-10-28): the pane-order builder is exported as `project.EntriesOf(config.ProjectOffer)` (it reads the offer, not Options, so it survives item 12); cmd's `proposalsOf(opts)` stays as a thin caller that builds the offer from the options' Proposals and AllowRules.Proposed. Entries come back deep-copied (domain.Proposal.Clone), otherwise byte-identical.
NOTES (2026-10-28): `projectRuleEntry` and `configAllowKind` exported as `project.RuleEntry` and `project.AllowKind`; their three cmd call sites (all in projectrules.go) call the exports directly, and cmd keeps only `proposalEntry` as a one-line caller over `project.ProposalEntry` (still used by project_cmd.go, which this item leaves untouched).
NOTES (2026-10-28): `Diff` takes `(before, after)` instead of the Goal's `(old, new)` so `new` does not shadow the builtin. Per the header's item-9 decision, `Moved.Allow` compares the whole effective `AllowRules.Rules`; `Servers` and `Reactions` compare the adopted entries' fingerprints as sets (order ignored), so an entry proposed or rejected without ever being in force moves nothing.
NOTES (2026-10-28): `cloneEach` moved into internal/config with `Options.Clone` (cloneOptions was its only caller). `cloneOptions` now delegates and `TestCloneOptionsSharesNoCollection` passes over it; `wire_settings.go` dropped its now-unused `maps` import.

**What:** Depends on item 5.
**Goal:** Package `internal/project` exports `Snapshot` (immutable; accessors return copies) built from one resolution: `Options()`, `Offer()` and `Entries()`, the answerable proposals in the pane's order. `Load(globalPath, root, readFile, notify)` builds one. `Diff(old, new) Moved` reports which kinds moved: `Allow`, `Servers`, `Reactions`. The `Adoption` glossary Note names the snapshot.
**Approach (assumed at the header base):**
- Move `proposalsOf`, `proposalEntry`, `projectRuleEntry` and `configAllowKind` down from `cmd/apogee/projectrules.go`. cmd keeps thin callers.
- `Load` wraps `config.LoadLayeredOffer`.
- `Diff` compares adopted sets per kind.
- No cmd behaviour changes.
**Regression guard.** `Snapshot.Options()` copies through a new `config.Options.Clone`, moved from `cmd/apogee/wire_settings.go` `cloneOptions` (which now delegates; `TestCloneOptionsSharesNoCollection` stays on it). `engineAllowRules` (`cmd/apogee/wire_config.go`, called by `proposalsOf`) moves with it: `internal/project` exports it, and cmd keeps a thin `engineAllowRules` over the export so `settingsrows.go` and `allowRulesFromOptions` are unchanged.
**Files:** internal/project/doc.go, internal/project/snapshot.go, internal/project/snapshot_test.go, cmd/apogee/projectrules.go, .capcom/knowledge/glossary/glossary-safety/adoption.md, internal/config/options.go, cmd/apogee/wire_settings.go, cmd/apogee/wire_config.go
**Read first:** cmd/apogee/projectrules.go — proposalsOf, proposalEntry, projectRuleEntry, configAllowKind; cmd/apogee/wire_config.go — engineAllowRules; cmd/apogee/wire_settings.go — cloneOptions;
cmd/apogee/wire_settings_test.go — TestCloneOptionsSharesNoCollection; internal/domain/config.go — Proposal.Clone
**Tests:** `TestSnapshot*` in `internal/project`: entry order and kinds; accessors (`Options()` through `config.Options.Clone`) share no backing array; `TestCloneOptionsSharesNoCollection` passes over the delegating `cloneOptions`; `Diff` table over each kind adopted, rejected and unchanged.
**Acceptance:** `go build ./... && GOMEMLIMIT=2GiB go test -count=1 ./internal/project/ && capcom check`
**Commit:** `feat(project): an Adoption snapshot of the project offer`

## 7. Answer: the held check, the record write and the next snapshot — ✅ DONE (2026-10-28)

NOTES (2026-10-28): consequential edit — internal/project/doc.go: made necessary by the new answer.go (the package doc's file map and summary name Answer and Record).
NOTES (2026-10-28): cmd/apogee/projectrules.go is listed in **Files:** but needed no edit — per the item's regression guard its projectRulesHeld/keepHeldEntries/recordAnswer copies stay until items 9 and 10 move their callers, and its prose stays true; `isSameFile` is copied (unexported) into internal/project for the held check rather than moved.
NOTES (2026-10-28): `project.Record(store, entries, reply Reply)` takes a named `Reply` (today's adoptionAnswer shape: `(*adoption.Store).Adopt/Reject/Forget`) and writes nothing for an empty entry list, so Answer's "no entry held, no record file" holds through it; a nil store is a refusal.
NOTES (2026-10-28): the three outcomes are told apart by type: a held re-read failure is `*project.HeldCheckError` (its text is cmd's "apogee: the project config could not be re-read: …" byte for byte), a store error comes back raw, and a recorded answer whose next load failed is `*project.NextLoadError`; when nothing is recorded `next` is the zero Snapshot (the caller's stands). Answer's loads drop notices (as cmd's loadLayeredOptions does), since the Goal's signature carries no notify. No held-re-read-failure test row: the project-only load turns every project-file defect into a notice, so the path is unreachable from files.

**What:** Recast at the regression check (2026-10-27). Depends on item 6.
**Goal:** `project.Answer(globalPath, root, readFile, store, entries, adopt)` returns `(next Snapshot, notHeld []domain.Proposal, err error)`. On adopt it re-reads the project file alone, with the record off, and records only the held entries. On reject it records without a held check. It returns the snapshot loaded after the write. A broken global config still records the answer and returns it with the load error. The held-check semantics are tested in `internal/project` over real temp files.
**Approach (assumed at the header base):**
- Move `projectRulesHeld`, `keepHeldEntries` and `recordAnswer`'s store calls down from `cmd/apogee/projectrules.go`.
- `notHeldError` stays in cmd, because each Driver words `notHeld` itself.
- Port the held tests from `projectrules_test.go` as pure tests: refuses a rule the file no longer holds; records with a broken global; a shadowed repo server still counts as held.
**Regression guard.** project also exports `Record(store, entries, answer)` with no held check, used by reject, by addProjectRule (apogee itself just wrote that entry) and by removeProjectRule's Forget. `project.Record` only writes the record (adopt/reject/forget, today's `adoptionAnswer`) and returns no snapshot; `project.Load` is the one way to take the next snapshot, so each caller takes it after its own file writes; `Answer` stays held check → record → Load. The cmd copies of projectRulesHeld/keepHeldEntries/recordAnswer stay until items 9 and 10 remove their last callers (project_cmd.go:272 calls projectRulesHeld; removeProjectRule calls recordAnswer; removeGlobalRule writes no record — projectrules.go removeGlobalRule calls only RemoveGlobalAllowRule then settleAllowRules) — item 7 adds the project versions, it does not delete the cmd ones.
`Answer` takes `readFile`, as `Load` does, for both the held re-read and the next-snapshot load, and reports whether the record was written: a held re-read failure (nothing recorded), a store error (nothing recorded, returned raw) and a post-write load error (recorded; typed) are distinguishable, so each cmd wrapper keeps its bytes. When the held check keeps no entry, Answer opens no write and returns `notHeld` with nothing recorded (today adoptProjectRules returns before any write; `Store.update` always writes, store.go). The shadowed-repo-server case is a new row, not a port: no such test exists in `projectrules_test.go`.
**Files:** internal/project/answer.go, internal/project/answer_test.go, cmd/apogee/projectrules.go
**Read first:** cmd/apogee/projectrules.go — projectRulesHeld, isSameFile, keepHeldEntries, recordAnswer, adoptProjectRules; internal/adoption/store.go — New, Adopt, Reject, Forget, update;
cmd/apogee/project_cmd.go — adoptHeldRule, projectAdoptTarget; internal/config/layer.go — LoadLayeredConfig; internal/config/allow.go — workspacesDirOf;
cmd/apogee/projectrules_test.go — TestProjectRuleAdoptRecordsAHeldRuleWhenTheGlobalConfigDoesNotResolve, TestProjectRuleAdoptRefusesARuleTheFileNoLongerHolds
**Tests:** `TestAnswer*` table: adopt held/not held per kind; adopt with no entry held writes no record file; reject and forget record without a held check (through `Record`, which returns no snapshot); broken global → recorded + the typed post-write load error, told apart from a store failure (nothing recorded); returned snapshot reflects the write; new row: a global server of the same name as a repo server, the repo server adopted because the held check reads with no global.
**Acceptance:** `go build ./... && GOMEMLIMIT=2GiB go test -count=1 ./internal/project/`
**Commit:** `feat(project): Answer does the held check and returns the next snapshot`

## 8. The settings doors take a loaded set — ✅ DONE (2026-10-28)

**What:** Depends on item 5.
**Goal:** `reconnectMCP`, `reloadReactions` and `installAllowRules` each have a form that takes an already-resolved `config.Options` and does not read the disk. The loading forms are thin wrappers over them, and behaviour is unchanged.
**Approach (assumed at the header base):** Split `settingsApplier.reconnectMCP`, `reloadReactions` (`cmd/apogee/wire_settings.go`) and `installAllowRules` (`projectrules.go`) into `…From(layered)` plus the existing loading wrapper.
**Regression guard.** The loading wrappers return the loader error unwrapped (`TestSettingsApplierReloadsRefuseAnUnparseableFile` pins that `reconnectMCP`/`reloadReactions` return the loader's own sentence), and the Acceptance runs the tests that call the doors directly.
**Files:** cmd/apogee/wire_settings.go, cmd/apogee/projectrules.go
**Read first:** cmd/apogee/wire_settings.go — settingsApplier.reconnectMCP, reloadReactions, layeredConfig; cmd/apogee/projectrules.go — installAllowRules, layeredOptions;
cmd/apogee/wire_options.go — rootWiring.settingsApplier; cmd/apogee/wire_settings_test.go — TestSettingsApplierReloadsRefuseAnUnparseableFile, fakeApplier
**Tests:** existing door tests pass unchanged, `TestSettingsApplierReloadsRefuseAnUnparseableFile`, `TestHookRunnerReplaceNeverReportsOnTheCallersGoroutine` and `TestSessionStartedAdopted*` among them.
**Acceptance:** `go build ./... && GOMEMLIMIT=2GiB go test -count=1 -run 'Reconnect|ReloadReactions|AllowRule|SettingsApplierReloads|HookRunnerReplace|SessionStartedAdopted' ./cmd/apogee/`
**Commit:** `refactor(cmd): the settings doors take a loaded set`

## 9. The session answers through project.Answer — ✅ DONE (2026-10-28)

NOTES (2026-10-28): re-derived from "rootWiring is declared in cmd/apogee/wire_options.go" — the struct lives in cmd/apogee/wire.go, so the `readFile` seam field is added there (gofmt re-aligned the neighbouring `reloadWake` field); wire_options.go carries only a comment edit.
NOTES (2026-10-28): consequential edit — cmd/apogee/wire_options.go: made necessary by applyAdoptedKinds → applyMovedKinds (settingsApplier's doc named the old function).
NOTES (2026-10-28): consequential edit — cmd/apogee/wire_settings.go: made necessary by installSnapshot (liveSettings.allowRules/proposals docs said only installAllowRules writes the holder).
NOTES (2026-10-28): adoptProjectRules is folded into answerProjectRules(proposals, adopt bool), which AdoptRules and RejectRules both call — one project.Answer per answer; old is rootWiring.inForce (project.SnapshotOf over the live holder's options and offer), the doors are fed next.Options() and gated on project.Diff, and the holder's AllowRules/Proposals are replaced whole from next after every recorded answer or file write (installSnapshot).
NOTES (2026-10-28): the adoption store is opened before project.Answer (Answer takes the store), so a store that cannot be opened now refuses ahead of the proposal and held checks — the order differs only when both fail.
NOTES (2026-10-28): Diff's Allow kind still compares whole effective rules (kind, text and layer) — a superset of kind+text, so a rule that changes layer still reaches the engine; rows added: a global rule removed moves Allow, the same handle under a new pin moves Servers, and SnapshotOf matches the load it was built from.
NOTES (2026-10-28): a door failure on a reject reuses the existing "apogee: the adoption is saved, but this session could not apply it: …" wording (reject reaching a door is new; no prior bytes existed).
NOTES (2026-10-28): cmd copies deleted with their last caller: applyAdoptedKinds (now applyMovedKinds), hasEntryKind, keepHeldEntries, proposalEntries and the adoptionAnswer type (project.Reply); projectRulesHeld and isSameFile stay (project_cmd.go's caller, item 10); recordAnswer (store + project.Record) and settleAllowRules(old) stay live, reworked; installAllowRules/installAllowRulesFrom/holdProposals stay for the watched refresh (item 11).
NOTES (2026-10-28): TestProjectRuleAdoptRefusesARuleTheFileNoLongerHolds trimmed to the exact refusal wording (its record assertions are TestAnswerAdoptRefusesWhatTheFileNoLongerHolds's); TestProjectRuleAdoptRecordsAHeldRuleWhenTheGlobalConfigDoesNotResolve now asserts the exact settling prefix and that no door ran (engine pendingAllowRules/pendingGeneration unchanged, holder empty); new: TestProjectRuleAdoptReadsTheLayeredConfigTwice, TestProjectRuleAnswersReplaceTheHoldersRules, TestProjectRuleRejectingAnAdoptedServerWithNoMCPSetReachesNoDoor, and TestChangedReactionScriptReasksInSession asserts the engine's generation carries the new pin.

**What:** Recast at the regression check (2026-10-27). Depends on items 7 and 8.
**Goal:** `rootWiring.adoptProjectRules`, `answerProjectRules` and `addProjectRule` call `project.Answer` once per answer. They run each door only for a kind `project.Diff(old, next)` reports moved, feeding the door `next.Options()`. One adopt reads the layered config twice: the held check and the next snapshot. The pane and settings wording is byte-identical. `projectrules_test.go` keeps only the wiring tests; the held-check semantics tests that item 7 ported are deleted here.
**Approach (assumed at the header base):** Replace `projectRulesHeld` → `recordAnswer` → `settleAllowRules` → `applyAdoptedKinds` with Answer → Diff → the `…From` doors. The live holder takes `next`'s offer.
**Regression guard.** addProjectRule records through project.Record, so its read count is unchanged and the two-layered-reads assertion covers adoptProjectRules only; item 9 deletes each cmd copy (projectRulesHeld excepted, item 10 owns its last caller) whose last caller it removes; Diff compares by fingerprint over snapshots built from the in-force Options, except that Diff's Allow kind compares the whole effective Rules (global included) by kind+text.
removeGlobalRule never records: after the global write it takes the next snapshot with project.Load and installs Allow from it. removeProjectRule records the Forget with project.Record, then takes the next snapshot after RemoveProjectAllowRule, on both the success and the file-failure paths. After every recorded answer or file write the live holder's AllowRules and Proposals are replaced whole from next's offer, and only engine.SetAllowRules and the MCP/reaction doors are gated on Diff. When addProjectRule's store write fails it still refreshes the baseline, loads and installs from the files, joined to the kept "%s holds the rule…" error. The doors' nil guards (w.mcpSet/w.toolSet, w.engine/w.live) wrap every Diff-gated door call. TestProjectRuleRemoveTakesAGlobalRuleOutOfTheGlobalConfig stays green.
`old` is the session's in-force state (`w.live`'s options and offer) through a Snapshot constructor from resolved Options plus offer (added here in `internal/project`). On Answer's post-write load error no door runs, nothing is installed, and the error reads "apogee: the answer is saved, but the rules in force could not be refreshed: %w"; a store error keeps "%s holds the rule, but its adoption was not recorded…". The external-edit baseline (`w.externalEdits.refresh`) is re-taken after every recorded answer, whatever Diff reports.
`rootWiring` gains a `readFile` seam threaded through `project.Load`/`Answer`; the counting test counts resolutions through it. Every hit of `grep -rn 'applyAdoptedKinds\|projectRulesHeld\|settleAllowRules\|recordAnswer' cmd/apogee` either follows or stays live.
**Files:** cmd/apogee/projectrules.go, cmd/apogee/projectrules_test.go, cmd/apogee/wire_settings.go, cmd/apogee/wire_options.go, internal/project/snapshot.go, internal/project/snapshot_test.go
**Read first:** cmd/apogee/projectrules.go — adoptProjectRules, answerProjectRules, addProjectRule, removeProjectRule, removeGlobalRule, settleAllowRules, applyAdoptedKinds, installAllowRules; internal/adoption/store.go — New, key, update;
cmd/apogee/settingsrows.go — allowRuleRows; cmd/apogee/wire_settings.go — settingsApplier.reconnectMCP, reloadReactions, liveSettings.allowRules, proposals; internal/projectroot/projectroot.go — Resolve;
cmd/apogee/projectrules_test.go — projectRuleWiring, writeGlobalRule, assertNoReloadedKey, TestProjectRuleRemoveTakesAGlobalRuleOutOfTheGlobalConfig, TestProjectRuleSettingsSectionListsFromTheLiveHolder, TestChangedReactionScriptReasksInSession
**Tests:** `TestProjectRuleAdoptsARepoServerTheFileHolds`, `TestProjectRuleAdoptArmsTheRepoReaction`, `TestProjectRuleAdoptRefusesARuleTheFileNoLongerHolds` (wording), `TestProjectRuleRejectAndAdoptMoveOnlyTheRecord` and `TestProjectRuleRemoveTakesAGlobalRuleOutOfTheGlobalConfig` stay, with `assertNoReloadedKey`; new rows: rejecting a proposed Allow rule replaces the holder's AllowRules (the /settings row no longer reads "proposed project"); removing a project rule leaves no "proposed project" row; rejecting an adopted server in `projectRuleWiring` (no MCP set) reaches no door; a `TestSnapshot*` Diff row: a global rule removed → Allow moved; `TestProjectRuleAdoptRecordsAHeldRuleWhenTheGlobalConfigDoesNotResolve` stays in cmd and asserts no door ran; `TestChangedReactionScriptReasksInSession` asserts `w.engine.pendingGeneration` carries the new pin after the re-adopt; a counting `readFile` test asserts two layered reads per `adoptProjectRules` adopt; a `TestSnapshot*` Diff row: same handle, new pin text → moved.
**Acceptance:** `go build ./... && GOMEMLIMIT=2GiB go test -count=1 -run 'TestProjectRule|TestChangedReactionScript|TestE2EAdoption' ./cmd/apogee/`
**Commit:** `refactor(cmd): the session answers Adoption through one snapshot`

## 10. apogee project adopt answers through project.Answer

**What:** Depends on item 7.
**Goal:** `apogee project adopt` resolves through `project.Load` and answers each question through `project.Answer`. Every CLI string is byte-identical, including the summary line.
**Approach (assumed at the header base):** `resolveProjectAdoption` and `adoptHeldRule` (`cmd/apogee/project_cmd.go`) call the project package. The per-yes re-read stays.
**Regression guard.** Deletes cmd's projectRulesHeld once adoptHeldRule no longer calls it.
`resolveProjectAdoption` resolves through `config.ApplyConfigOffer` (item 5) for the options and the offer in one read — `LoadLayeredConfig` never migrates and drops the flag/env/`StartupUndetermined` handling — and any `project.Load` call takes a no-op notify, so no loader notice prints twice. Answer's post-write load error is non-fatal in the CLI: the remaining questions and the summary go on.
**Files:** cmd/apogee/project_cmd.go, cmd/apogee/project_cmd_test.go, cmd/apogee/projectrules.go
**Read first:** cmd/apogee/project_cmd.go — resolveProjectAdoption, adoptHeldRule, askProjectRules, projectAnswerTally.report; cmd/apogee/projectrules.go — projectRulesHeld;
cmd/apogee/project_cmd_test.go — runProjectAdopt, TestProjectCmdAdoptRefusesARuleTheFileNoLongerHolds; cmd/apogee/wire_live.go — projectConfigWatchPath
**Tests:** `project_cmd_test.go` unchanged, plus one `TestProjectCmd*` row: a global config broken while questions wait still records the yes, asks the rest and prints the summary.
**Acceptance:** `! grep -n 'func projectRulesHeld' cmd/apogee/*.go`; `go build ./... && GOMEMLIMIT=2GiB go test -count=1 -run 'TestProjectCmd|TestProjectRuleCLIAdoption|TestHeadlessFiresOnlyAdoptedRepoReactions' ./cmd/apogee/`
**Commit:** `refactor(cmd): apogee project adopt answers through project.Answer`

## 11. The live settings hold the snapshot

**What:** Recast at the regression check (2026-10-27). Depends on item 9.
**Goal:** The live settings holder keeps a `project.Snapshot` in place of `Options.Proposals` and the inert Allow lists. `/settings` rows, `tui.Options.ProposedRules` and the reload followers read the snapshot. Row text is byte-identical.
**Approach (assumed at the header base):** Repoint `liveSettings.proposals()`, `holdProposals`, `spliceRepoEntryRows`/`repoEntryRows`/`allowRuleRows` (`settingsrows.go`), `followReloadedProposals`/`followReloadedAllowRules` and `wire_options.go`'s `ProposedRules`.
**Regression guard.** Item 11 owns the seam that brings the offer to the registry `allow` key — `allow` keeps its Read/Structure(Options) (registry.go `Key.Read`/`Key.Structure`; `TestRegistryRowsProjectEveryValue` needs both non-nil) and gains an offer-aware projection field that settingsrows.go:130 (settingsRows), settingsedit.go:357 (externalEdit.projection) and probeconfig.go:138 (via ApplyConfigOffer/LoadLayeredOffer) prefer when set, the newExternalEdit fallback (settingsedit.go:177-178) included, so the "N rules, M proposed" summary in /settings and `apogee probe config` and the reload diff on a proposed-only addition or a rejection stay byte-identical; pin it with an externalEdit.changed test where a proposed-only addition still reports `allow`.
`holdProposals`, `followReloaded*` and `installAllowRules` (`projectrules.go`) are repointed; the boot seed is a snapshot from the boot resolution (no second `project.Load` disk read at startup), and `newLiveSettings(opts)` keeps its signature. `settingsHost`'s `allowRules`/`proposals` func fields keep their types; the holder answers them from the snapshot.
Keep `settingsRows(opts)` as a wrapper over an offer-taking form, or add projectrules_test.go to **Files:** (`TestSettingsHostRowsAppendTheAllowRulesSection` calls `settingsRows(config.Options{})`). `settingsHost.Rows` hands settingsRows the offer of the resolution its opts come from (the boot snapshot), never the live holder's.
**Files:** cmd/apogee/wire_settings.go, cmd/apogee/settingsrows.go, cmd/apogee/wire_options.go, cmd/apogee/settingsedit.go, cmd/apogee/settingsrows_test.go, cmd/apogee/projectrules.go, cmd/apogee/wire_live.go, cmd/apogee/settingsedit_test.go, internal/config/registry.go, cmd/apogee/probeconfig.go
**Read first:** internal/config/registry.go — Key.Read, Key.Structure, `allow` entry; internal/config/registry_test.go — TestRegistryRowsProjectEveryValue; cmd/apogee/wire_options.go — settingsHost.Rows, layeredOpts, ProposedRules;
cmd/apogee/settingsedit.go — newExternalEdit, projection, settingChanged; cmd/apogee/settingsrows.go — settingsRows, allowRuleRows; cmd/apogee/probeconfig.go — resolvedValue;
cmd/apogee/wire_settings.go — newLiveSettings, liveSettings.allowRules, proposals; cmd/apogee/projectrules_test.go — TestSettingsHostRowsAppendTheAllowRulesSection
**Tests:** existing settings-row and watcher tests pass; `TestWatchedAdoptionRecordReloadsARepoReaction`, `TestSettingsHostRowsAppendTheAllowRulesSection`, `TestRegistryRowsProjectEveryValue` and `TestAllowRulesSummary` unchanged; settingsHost.Rows after an in-session adopt still reads the boot "N rules, M proposed" summary; new `TestExternalEditReloadReportsAllowForAProposedOnlyAddition` (externalEdit.changed still reports `allow`).
**Acceptance:** `go build ./... && GOMEMLIMIT=2GiB go test -count=1 -run 'Settings|Watched|Adoption|ExternalEdit' ./cmd/apogee/`, then `GOMEMLIMIT=2GiB go test -count=1 -run 'TestAllowRulesSummary|TestRegistry' ./internal/config/`
**Commit:** `refactor(cmd): live settings read the Adoption snapshot`

## 12. config.Options carries no proposals

**What:** Depends on items 10 and 11.
**Goal:** `config.Options` has no `Proposals` field, and `AllowRules` carries only the effective `Rules`. `mcp login`/`mcp list`, headless and the daemon read the offer from the `…Offer` loaders. `cloneOptions` clones neither, and its reflection test passes. Every user-facing string is byte-identical, including "proposes reaction \`lint\`; this run skips it".
**Approach (assumed at the header base):**
- Repoint `refuseUnadoptedRepoServer`/`mcpServerRows`/`proposalTransport` (`mcp_cmd.go`), `skippedReactionsNotice` (`headless.go`, `daemonfire.go`).
- Drop the fields and the old wrappers' filling.
- Repoint the config tests reading `opts.Proposals` and `AllowRules.Proposed`.
**Regression guard.** Depends on item 11's allow seam; Files become the rule "every reader and writer of Options.Proposals and AllowRules.Proposed/Rejected" plus `grep -rnE '\.Proposals\b|Proposals:|AllowRules\.(Proposed|Rejected)|(Proposed|Rejected): \[\]config\.AllowRule' --include=*.go cmd internal` (filter the offer's own fields); Acceptance greps the field declarations only: `! go doc -all ./internal/config Options | grep -nE '^\s*Proposals '` and `! go doc -all ./internal/config AllowRules | grep -nE '^\s*(Proposed|Rejected) '`; the registry.go allow-row doc ("how many rules are live and how many wait on an answer") stays true and every comment naming Options.Proposals (config/doc.go, execlayer.go) is rewritten.
The item yields to `internal/config/registry.go`'s allow-row doc (the row is "summarized by how many rules are live and how many wait on an answer").
newLiveSettings' boot seed takes its offer from ApplyConfigOffer, not from the removed Options fields (after item 11, `newLiveSettings(opts)` builds it from `opts.Proposals`/`AllowRules.Proposed`; its caller is wire_live.go:315).
**Files:** every reader and writer of `Options.Proposals` and `AllowRules.Proposed/Rejected` — `grep -rnE '\.Proposals\b|Proposals:|AllowRules\.(Proposed|Rejected)|(Proposed|Rejected): \[\]config\.AllowRule' --include=*.go cmd internal`, the offer's own fields filtered out — among them internal/config/options.go, internal/config/allow.go, internal/config/config.go, internal/config/layer.go, internal/config/execlayer_test.go, internal/config/allow_test.go, cmd/apogee/mcp_cmd.go, cmd/apogee/headless.go, cmd/apogee/daemonfire.go, cmd/apogee/wire_settings.go, cmd/apogee/wire_settings_test.go, cmd/apogee/wire_live.go; plus the comments in internal/config/doc.go and internal/config/execlayer.go
**Read first:** internal/config/registry.go — KeyRegistry `allow` entry; internal/config/allow.go — AllowRules, allowRulesSummary; internal/config/layer.go — LoadLayeredConfig;
cmd/apogee/settingsedit.go — settingChanged, externalEdit.projection; cmd/apogee/mcp_cmd.go — refuseUnadoptedRepoServer; cmd/apogee/headless.go — skippedReactionsNotice
**Tests:** `TestCloneOptionsSharesNoCollection`; `TestMCPLoginRefusesAProposedRepoServer`; the daemon and headless skipped-reaction tests unchanged.
**Acceptance:** `! go doc -all ./internal/config Options | grep -nE '^\s*Proposals '`; `! go doc -all ./internal/config AllowRules | grep -nE '^\s*(Proposed|Rejected) '`; `go build ./... && GOMEMLIMIT=2GiB go test -count=1 -run 'Offer|Allow|Exec' ./internal/config/`, then `-run 'CloneOptions|MCPLogin|Skipped|Daemon' ./cmd/apogee/`
**Commit:** `refactor(config): proposals leave Options for the project offer`
