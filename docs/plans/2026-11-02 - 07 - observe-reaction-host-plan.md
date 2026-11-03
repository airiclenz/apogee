# The observe-lane Reaction host is one library module

**Goal:** One `reactions.Host` in `internal/reactions` owns a Driver root's observe lane: the MCP disarm and its notices, the url guard, the fenced `DefaultExecutor`, the project-run and MCP late binds, the Runner and its close grace. Every in-binary Driver composes through it. The facade re-exports it as `apogee.NewReactionHost`, so an embedder gets the same fence, guard and one-notice-per-entry as the binary (ADR 0073 §9).
**Date:** 2026-11-02
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 00d5bf9f
**Sources:**
- `docs/reviews/architecture-review-2026-11-02.html` — card #5
- Verification of card #5 (2026-11-02). There is no live safety gap; every path fails closed. It found three latent gaps: a facade-only Driver gets no MCP inert notice; `DefaultExecutor`'s fence and url guard cannot be reached from the facade; and `hookCloseGrace` and `drainGrace` are kept equal only by a comment.
- ADR 0073 (§6, §7, §9, context) · ADR 0076 (A8, `adr-0076-amend-1029`) · ADR 0104 §8 · ADR 0031 · ADR 0033 D6 · ADR 0075 D9 · ADR 0103
- Glossary: Reaction, Driver ("nothing safety-relevant may exist only in one Driver's surface")
- `docs/plans/archived/2026-10-29 - 01 - mcp-reaction-handler-plan.md` · `docs/plans/archived/2026-10-31 - 00 - observe-mcp-pin-cue-plan.md`
- `docs/manual/reactions.md` §Calling an MCP tool (the pinned notice texts)

**Ratified design calls:**
- **Facade export:** `apogee.NewReactionHost`, with the `ReactionHost` and `ReactionHostOptions` aliases, becomes new public API (owner, 2026-11-02).
- **Placement:** in `internal/reactions`, beside Runner and DefaultExecutor. Not in `internal/run` (ADR 0033 D6), and no new package (owner, 2026-11-02).
- **Shape:** a new `reactions.Host` type, documented by a new glossary Note "Reaction host". `Runner` is not widened, so it stays usable with an embedder's own Executor (owner, 2026-11-02).
- **Install:** fills `Events`, `ObserveRunner`, `Observe`, `BindProjectRun`, `Report` and `PinMoved`. The Driver keeps its sink wrapping and `cfg.MCP`; Install never sets `cfg.MCP`, because the engine treats a nil resolver as silent and an unbound one as reported (owner, 2026-11-02).
- **Instantiation:** each Driver root builds its own host, one per Firing. The observe lane stays Driver-side (ADR 0073, ADR 0076 A8) (owner, 2026-11-02).
- **Grace:** one exported constant, `reactions.CloseGrace`, replaces both `drainGrace` and `hookCloseGrace` (owner's "one shared constant" option, 2026-11-02).
- **Citation:** the "only in one Driver" rule is cited from the glossary Driver Note and ADR 0073's context, not ADR 0031 (verification, 2026-11-02).

**Standing requirements:**
- skills: coding-standards
- Test-run rules (pass to every sub-agent): run only `GOMEMLIMIT=2GiB go test -count=1 -run '<pattern>' ./<one pkg>/`. Never run `-race` or `-cover` over a whole package or a pattern. Never run two test runs at once.
- These texts stay byte-identical. Tests and `docs/manual/reactions.md` pin them: `reaction %q: its mcp: handler is inert here — MCP servers are not connected in this run`; `reaction %q: its mcp: gate cannot run here — MCP servers are not connected in this run, so every call it gates is refused`; `refusing to run a project reaction before its session's engine is built`; `MCP servers are not connected yet`; `MCP servers are not connected in this run`; `refusing to run a project reaction outside the workspace exec fence`.
- Behaviour stays unchanged in the TUI, `apogee headless`, the daemon, `/schedule` and `apogee probe context`.
- Use Bubble Tea v2 names only (`tea.KeyPressMsg`, `msg.String()`). The tui `Model` is copied by value: never hold a `strings.Builder` by value (ADR 0011).
- Any deviation from item text lands as a dated NOTES line under the item.

**Out of scope:**
- Review card #11, engine composition (issue `engine-composition-unify`).
- Changes to apogee-sim (a separate repo), including the bench arming user `mcp:` entries.
- Moving `cmd/apogee` from `reactions.NewHost` to the facade alias.

**Regression check (2026-11-02, 00d5bf9f):**
- 1: guard folded (nil-receiver `MCPSlot.Resolver`; Goal no longer claims a test pins `CloseGrace`).
- 2: trimmed to ≤ 35 non-blank lines (narration only).
- 3: recast — moves only the Firing path; `projectRunSlot` stays for item 4; the `isMCPReaction` test site is named.
- 4: recast — also deletes `projectRunSlot` and `errProjectRunUnbound`; guards folded (field `reactionHost`, not `host`; the bare `hooks` comment rule).
- 5: guard folded (the Acceptance grep matches import paths only).
- 6: guard folded (Acceptance drops `TestDocs` for `capcom` checks).
- 4 (re-check): guard folded (the prose rule covers every comment naming a local or field item 4 deletes, incl. `resolveConfig`'s `projectRuns` comment; `\bprojectRuns\b` joins the Acceptance grep).

## 1. The late binds, the no-MCP disarm and the close grace live in internal/reactions — ✅ DONE (2026-11-03)

NOTES (2026-11-03): doc.go's import-direction paragraph is unchanged (bind.go adds only standard-library imports and internal/domain); its per-file map gains a bind.go line and names CloseGrace on runner.go's line.
NOTES (2026-11-03): the "nil report drops the notices" case is its own test, TestDisarmUnconnectedMCPNilReport, beside the TestDisarmUnconnectedMCP table.

**What:**
**Goal:** `internal/reactions` exports `ProjectRunSlot`, `ErrProjectRunUnbound`, `MCPSlot`, `ErrMCPResolverUnbound`, `DisarmUnconnectedMCP` and `CloseGrace`, and its own tests pin each one but `CloseGrace`. Replace's drain uses `CloseGrace`, and its doc names it as the grace a root gives its Runner at close.
**Approach (assumed at the header base):**
- New `internal/reactions/bind.go`, ported from package main with behaviour unchanged:
  - `projectRunSlot` (`bind`/`runner` over `atomic.Pointer[domain.ProjectRun]`) and `errProjectRunUnbound`, from `cmd/apogee/wire_firing.go`, become `ProjectRunSlot` with `Bind`/`Runner`, and `ErrProjectRunUnbound`.
  - `mcpResolverSlot` and `errMCPResolverUnbound`, from `cmd/apogee/wire_mcp.go`, become `MCPSlot` with `Bind`/`Resolver`, and `ErrMCPResolverUnbound`.
  - `disarmUnconnectedMCP`, `isMCPReaction` and the consts `inertMCPNotice` and `inertMCPGateNotice` become `DisarmUnconnectedMCP`. The consts stay unexported.
  - A nil slot still answers nil.
- `runner.go`: `drainGrace` becomes the exported `CloseGrace = 5 * time.Second`. Its doc says it is the one grace a root gives its Runner at close, and that a reload gives a retired generation (ADR 0073 §7).
- `doc.go`: update the import-direction paragraph only if `bind.go` adds a package import.
- The package-main copies stay in place. Items 3 and 4 delete them when their callers switch.
- These texts stay byte-identical: the two inert-notice formats, `ErrProjectRunUnbound`'s text and `ErrMCPResolverUnbound`'s text (see Standing requirements).

**Regression guard.** `MCPSlot.Resolver` gains a nil-receiver check returning nil — new, not ported: today's `mcpResolverSlot.resolver()` (`cmd/apogee/wire_mcp.go`) has none, and its closure panics on `s.resolve.Load()` for a nil slot. No caller passes a nil slot today (`wire_boot.go`), so `DefaultExecutor` reads the nil as "not connected in this run". No test of this item pins `CloseGrace`: nothing here closes a root (`Host.Close` is item 2); the `drainGrace` grep checks it.

**Files:** internal/reactions/bind.go, internal/reactions/bind_test.go, internal/reactions/runner.go, internal/reactions/doc.go
**Read first:** cmd/apogee/wire_firing.go — projectRunSlot, errProjectRunUnbound, disarmUnconnectedMCP, inertMCPNotice; cmd/apogee/wire_mcp.go — mcpResolverSlot, errMCPResolverUnbound;
internal/reactions/runner.go — drainGrace; cmd/apogee/wire_firing_test.go — TestFiringLeavesAdviseAndObserveMCPEntriesInert
**Tests:**
- `TestProjectRunSlot…`:
  - a nil slot's `Runner()` is nil;
  - an unbound slot refuses with the exact `ErrProjectRunUnbound` text;
  - a bound slot runs the bound func;
  - a rebind replaces the bound func.
- `TestMCPSlot…`: the same four cases, with `ErrMCPResolverUnbound`.
- `TestDisarmUnconnectedMCP…`:
  - advise and observe `mcp:` entries are dropped, gate `mcp:` entries are kept, and other entries are kept in order;
  - each entry gets one notice, with the exact texts;
  - notices are deduped by (id, text) for a `run:`+`advise:` pair under one id;
  - the input slice is never modified;
  - a nil report drops the notices;
  - a list with no MCP entries comes back unchanged.

**Acceptance:**
- `go build ./...`
- `go vet ./internal/reactions/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestProjectRunSlot|TestMCPSlot|TestDisarmUnconnectedMCP|TestReplace' ./internal/reactions/`
- `grep -n drainGrace internal/reactions/*.go` prints nothing.

**Commit:** `refactor(reactions): own the observe lane's late binds, MCP disarm and close grace`

## 2. `reactions.Host`: one Driver-built owner of the observe lane — ✅ DONE (2026-11-03)

NOTES (2026-11-03): NewHost refuses a HostMCP value outside HostMCPNone/HostMCPLate with an error (fail fast at entry); TestHostRefusesAnUnknownMCPMode pins it beside the six lettered tests.
NOTES (2026-11-03): doc.go's import-direction paragraph and per-file map were rewrapped to the file's ~100-column width (the 144-char line and the orphan "match.go is the pure" line); the text is unchanged except the new host.go entry naming the Host as what a Driver root composes.
NOTES (2026-11-03): no CHANGELOG entry — the Host is internal until item 5 exports it through the facade, and no Driver uses it yet (items 3 and 4).

Depends on item 1.
**What:**
**Goal:** `reactions.NewHost(list, HostOptions)` builds one Driver root's observe lane; `Install` fills exactly the six observe-lane keys of a `domain.Config`; `host_test.go` pins every behaviour below.
**Approach (assumed at the header base):** new `internal/reactions/host.go`:
- `type HostMCP int`: `HostMCPNone` (the Driver dials no MCP server) and `HostMCPLate` (the Driver binds a resolver after it dials).
- `HostOptions` fields: `Inner domain.EventSink`, `Workspace string`, `Schedule *ScheduleRef`, `Report func(string)`, `PinMoved func()`, `URLAllowHosts, URLDenyHosts []string`, `MCP HostMCP`.
- `NewHost`, in order: (1) under `HostMCPNone`, `DisarmUnconnectedMCP(list, Report)`; (2) keep the settled list and its `domain.LanesOf`; (3) build the Runner over the observe lane with `Options{Inner, Workspace, Schedule, Report}` and `Exec: DefaultExecutor(Workspace, security.NewURLGuard(URLAllowHosts, URLDenyHosts), projectSlot.Runner(), r, p)` — under `HostMCPLate` `r` is the MCPSlot's `Resolver()` and `p` is `PinMoved`; under `HostMCPNone` both are nil, as today's Firing executor has them; (4) if the Runner fails, return the error and leave nothing running.
- Methods: `Reactions() []domain.Reaction` (the settled list); `Lanes() domain.Generation` (its `LanesOf`, Observe and Sync only); `Runner() *Runner`; `BindMCP(domain.MCPResolver)` (a no-op under `HostMCPNone`); `MCPResolver() domain.MCPResolver` (the slot's resolver, or nil under `HostMCPNone`); `Close() error` (drains the Runner within `CloseGrace`).
- `Install(*domain.Config)`: sets `Events` and `ObserveRunner` to the Runner, `Observe` to the settled observe lane, `BindProjectRun` to the project slot's `Bind`, and `Report` and `PinMoved` only when the options gave them; never touches `MCP`, `Reactions` or `Sync`.
- Godoc states that the Driver keeps its sink wrapping and `cfg.MCP`. `doc.go` names the Host as what a Driver composes.

**Files:** internal/reactions/host.go, internal/reactions/host_test.go, internal/reactions/doc.go
**Read first:** internal/reactions/exec.go — DefaultExecutor, errMCPNotConnected; internal/reactions/command.go — errNoProjectRunner; internal/reactions/runner.go — New, Close;
internal/domain/config.go — Config; cmd/apogee/wire_firing.go — firingHooks; internal/reactions/runner_test.go — mcpResolverStub
**Tests:** `TestHost…` in `host_test.go`:
- (a) `HostMCPNone` over an advise `mcp:`, an observe `mcp:`, a gate `mcp:` and a command entry: it reports each inert text once, and `Reactions()` keeps only the gate and the command.
- (b) `HostMCPLate` keeps all four entries. An observe `mcp:` firing before `BindMCP` reports `MCP servers are not connected yet`. After `BindMCP` with a stub, the firing calls the stub.
- (c) A repo argv `run:` firing before `BindProjectRun` reports `refusing to run a project reaction before its session's engine is built`. After `Install` and `cfg.BindProjectRun(stub)`, the stub runs.
- (d) `Install` on a Config with `MCP` set leaves `MCP`, `Reactions` and `Sync` untouched and fills the six keys. A nil `Report` or `PinMoved` keeps the Config's own value.
- (e) A webhook to a host in `URLDenyHosts` is refused.
- (f) `Close` on an idle host returns nil.
- Every wait goes through `internal/testtime`.

**Acceptance:**
- `go vet ./internal/reactions/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestHost' ./internal/reactions/`

**Commit:** `feat(reactions): add the Host a Driver composes its observe lane through`

## 3. Every Firing composes its observe lane through the Host — ✅ DONE (2026-11-03)

NOTES (2026-11-03): firingInputs' `hooks` and `projectRuns` fields became one `host *reactions.Host`; raise passes `PinMoved: in.pinMoved` to the Host too (Install then sets the same Config.PinMoved firingConfig already did), and drains via `host.Close()` under `reactions.CloseGrace`. The two test sites that drive the Runner directly still close it under `testtime.Budget(10s)` through `host.Runner().Close(ctx)`, keeping their slow-box budget.
NOTES (2026-11-03): `projectRunSlot`, `errProjectRunUnbound` (wire_firing.go) and `hookCloseGrace` (wire.go) stay for the TUI per the regression guard; item 4 deletes them. hookCloseGrace's comment in wire.go still names a headless run and a daemon Firing as its users — left for item 4, which removes the const.

Depends on item 2.
**What:** Recast at the regression check (2026-11-02).
**Goal:**
- `raise` builds its observe lane only through `reactions.NewHost` (`HostMCPNone`). `raise` serves `apogee headless`, a daemon Firing and the TUI's `/schedule`.
- `apogee probe context` disarms through `reactions.DisarmUnconnectedMCP`.
- `cmd/apogee` no longer defines `firingHooks`, `disarmUnconnectedMCP`, `isMCPReaction` or the inert-notice consts.
**Approach (assumed at the header base):**
- `raise` (`wire_firing.go`): replace the block that runs disarm, `LanesOf`, `NewURLGuard`, `projectRunSlot` and `firingHooks` with `reactions.NewHost(in.opts.Reactions, HostOptions{Workspace: in.roots.workspace, Schedule: ref, Report: in.report, PinMoved: in.pinMoved, URLAllowHosts/URLDenyHosts from in.opts, MCP: HostMCPNone})`. A host error stays `errNotStarted{Stage: stageCompose}`. `defer func() { _ = host.Close() }()` stays ahead of both gates, so a refused Firing still drains its Runner. Set `in.opts.Reactions = host.Reactions()` and `Generation: host.Lanes()`.
- `firingInputs`: the `hooks` and `projectRuns` fields become `host *reactions.Host`.
- `firingConfig`: call `in.host.Install(&cfg)` when `in.host` is non-nil, replacing the Events/ObserveRunner/Observe/BindProjectRun block. The stats wrap then follows, as now. The `in.report` and `in.pinMoved` assignments stay, for host-less composition tests.
- `probecontext.go`: call `reactions.DisarmUnconnectedMCP`.
- Test sites in `wire_firing_test.go` that build `firingHooks` or `projectRunSlot` switch to `reactions.NewHost` or `reactions.ProjectRunSlot`; `errProjectRunUnbound` there becomes `reactions.ErrProjectRunUnbound`. The assertions do not change.
- Prose rule: re-point at the Host every comment in `cmd/apogee` that names a deleted symbol, `in.hooks`, `in.projectRuns` or `hookCloseGrace` in `wire_firing.go`. Find them with the grep in Acceptance.
- These texts stay byte-identical: both inert-notice texts and `ErrProjectRunUnbound`'s text.

**Regression guard.** item 3 moves only the Firing path (raise / firingHooks) onto the Host and leaves the TUI's `projectRunSlot` and its uses in place — item 3 does not delete it. `TestFiringLeavesAdviseAndObserveMCPEntriesInert`'s `slices.ContainsFunc(armed, isMCPReaction)` (`wire_firing_test.go`) switches to a test-local predicate under another name (e.g. `callsMCP`, a `domain.MCPHandler` type assertion); its assertion is unchanged.

**Files:** cmd/apogee/wire_firing.go, cmd/apogee/probecontext.go, cmd/apogee/wire_firing_test.go
**Read first:** cmd/apogee/wire_firing.go — raise, firingConfig, firingInputs, firingHooks, disarmUnconnectedMCP;
cmd/apogee/wire_firing_test.go — TestFiringHooksFenceARepoRun (fire helper: firingConfig with the host, then cfg.BindProjectRun), TestFiringLeavesAdviseAndObserveMCPEntriesInert;
cmd/apogee/probecontext.go — probeContextCommand (adds the internal/reactions import)
**Tests:** These stay green: `TestFiringLeavesAdviseAndObserveMCPEntriesInert`, `TestFiringMCPGateRefusesEveryCallItGates`, `TestFiringHooksFenceARepoRun`, `TestFiringHooksRefusesADenyListedWebhookEndpoint`, `TestFiringConfigInstallsTheHookRunner`, `TestFiringConfigWiresTheSyncLanesReporter`,
`TestNoWiringSiteWritesConfigReactions`, `TestAScheduleFiringReportsAnInertMCPEntry`, `TestProbeContextLeavesAnMCPAdviseEntryInert`, `TestDaemonFiringFiresAnAdoptedRepoReaction`, `TestE2EHooksFireFromAHeadlessRun`, `TestDaemonFiringFiresHooks`.

**Acceptance:**
- `go build ./...`
- `go vet ./cmd/apogee/`
- `grep -n "firingHooks\|disarmUnconnectedMCP\|isMCPReaction\|inertMCP\|in\.hooks\|in\.projectRuns" cmd/apogee/*.go` prints nothing.
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestFiringLeavesAdviseAndObserveMCPEntriesInert|TestFiringMCPGateRefusesEveryCallItGates|TestFiringHooks|TestFiringConfigInstallsTheHookRunner|TestFiringConfigWiresTheSyncLanesReporter|TestNoWiringSiteWritesConfigReactions|TestAScheduleFiringReportsAnInertMCPEntry|TestProbeContextLeavesAnMCPAdviseEntryInert|TestDaemonFiringFires|TestE2EHooksFireFromAHeadlessRun' ./cmd/apogee/`

**Commit:** `refactor(apogee): compose every Firing's observe lane through reactions.Host`

## 4. The TUI session composes its observe lane through the Host — ✅ DONE (2026-11-03)

NOTES (2026-11-03): wireSession guards `w.reactionHost.BindMCP` with a nil check: the deleted `mcpResolverSlot.bind` was nil-safe ("a wiring whose boot built no Runner"), while a nil `*reactions.Host` would panic in BindMCP, so the guard keeps that behaviour.
NOTES (2026-11-03): resolveConfig wraps `w.cfg.Events` (the Runner Install set) rather than the Runner directly, keeping the chain journal(stats(Runner(bridge))); the comments over the old six assignments were folded into one comment over `Install`. The two test cleanups that closed `w.hooks` with `context.Background()` (TestRootWiringEmitsThroughTheHookRunner, TestRootWiringScrubsHookHeaderVariables) now call `w.reactionHost.Close()`, bounded by reactions.CloseGrace.

Depends on items 2 and 3. Item 3 removes `raise`'s use of `hookCloseGrace`.
**What:** Recast at the regression check (2026-11-02).
**Goal:**
- The TUI session's observe lane is one `reactions.Host` (`HostMCPLate`). `resolveConfig` builds it, `wireSession` binds it to the live MCP resolver, and teardown closes it.
- Outside tests, `cmd/apogee` has no `reactions.New(` or `reactions.DefaultExecutor(` call.
- `cmd/apogee` no longer defines `mcpResolverSlot`, `errMCPResolverUnbound`, `hookCloseGrace`, `projectRunSlot` or `errProjectRunUnbound`.
**Approach (assumed at the header base):**
- `resolveConfig` (`wire_boot.go`):
  - Replace the block that builds the observe lane, `NewURLGuard`, `projectRunSlot`, `mcpResolverSlot` and `reactions.New` with `w.reactionHost, err = reactions.NewHost(w.opts.Reactions, HostOptions{Inner: w.bridge.Sink(), Workspace: w.roots.workspace, Report: w.bridge.NotifyHook, PinMoved: w.reloadWake.poke, URLAllowHosts/URLDenyHosts from w.opts, MCP: HostMCPLate})`.
  - After `projectConfig` builds `w.cfg`, `w.reactionHost.Install(&w.cfg)` replaces the six assignments.
  - Then `w.events = newLiveJournalSink(w.stats.wrap(w.cfg.Events))` and `w.cfg.Events = w.events`. The chain order stays journal(stats(Runner(bridge))) (ADR 0075 D9, ADR 0103).
- `wireSession` (`wire_live.go`): `w.cfg.MCP = resolveMCP` stays. `w.reactionHost.BindMCP(resolveMCP)` replaces the slot bind.
- `wire.go`:
  - The `hooks` and `mcpReactions` fields become `reactionHost *reactions.Host`.
  - Teardown calls `_ = w.reactionHost.Close()` when the host is non-nil, in the same position: after the Firings, before the engine.
  - Delete `hookCloseGrace`.
- `wire_mcp.go`: delete `mcpResolverSlot` and `errMCPResolverUnbound`.
- `wire_firing.go`: delete `projectRunSlot` and `errProjectRunUnbound`.
- Tests: `wire_settings_test.go` reads `w.reactionHost.Runner()`, and `mcp_cmd_test.go` reads `w.reactionHost.MCPResolver()`.
- Prose rule: in `cmd/apogee`, every comment naming a local or field item 4 deletes (`projectRuns`, `mcpReactions`, `hooks`, …), `hookCloseGrace` or `mcpResolverSlot`, bare or as `w.hooks`, names the Host instead.
- This text stays byte-identical: `MCP servers are not connected yet`.

**Regression guard.** item 4 moves the TUI boot onto the Host and deletes `projectRunSlot` (and any package-main copy left unused after items 3 and 4). `rootWiring` already has `host *sessionHost` (`cmd/apogee/wire.go`; assigned in `wire_live.go`, closed in `close`), so the new field is `reactionHost` everywhere — `resolveConfig`, `wireSession`, `close`, `wire_settings_test.go`, `mcp_cmd_test.go` — never `host`. The stats field doc "It decorates hooks as Config.Events" (`wire.go`) names the deleted field without `w.`; the prose rule and grep cover it. So does `resolveConfig`'s comment "reaches the session's Agent through projectRuns instead of running on the global posture" (`wire_boot.go`), whose local item 4 deletes: the prose rule covers every comment naming a local or field item 4 deletes, and the Acceptance grep carries `\bprojectRuns\b`.

**Files:** cmd/apogee/wire_boot.go, cmd/apogee/wire_live.go, cmd/apogee/wire.go, cmd/apogee/wire_mcp.go, cmd/apogee/wire_firing.go, cmd/apogee/wire_settings_test.go, cmd/apogee/mcp_cmd_test.go
**Read first:** cmd/apogee/wire_boot.go — resolveConfig (observe/projectRuns/mcpReactions block, the Events/ObserveRunner/Observe/BindProjectRun/Report/PinMoved assignments, w.stats/w.events chain);
cmd/apogee/wire.go — rootWiring (hooks, mcpReactions, stats doc, host *sessionHost), close, hookCloseGrace; cmd/apogee/wire_live.go — wireSession (w.mcpReactions.bind); cmd/apogee/wire_mcp.go — mcpResolverSlot;
cmd/apogee/wire_settings_test.go — TestRootWiringEmitsThroughTheHookRunner; cmd/apogee/mcp_cmd_test.go — startupWiring
**Tests:** These stay green:
- `TestSessionResolvesReactionMCPThroughTheLiveSet`
- `TestRootWiringEmitsThroughTheHookRunner`
- `TestRootWiringScrubsHookHeaderVariables`
- `TestWatchedAdoptionRecordReloadsARepoReaction`
- `TestE2EHooksFireFromTheTUI`
- `TestE2EReactionsReloadSwapsTheArmedList`

**Acceptance:**
- `go build ./...`
- `go vet ./cmd/apogee/`
- `grep -n "reactions\.New(\|reactions\.DefaultExecutor(\|mcpResolverSlot\|errMCPResolverUnbound\|hookCloseGrace\|mcpReactions\|projectRunSlot\|errProjectRunUnbound\|w\.hooks\b\|\bhooks as\b\|built with hooks\|\bprojectRuns\b" cmd/apogee/*.go | grep -v _test.go` prints nothing.
- `grep -n "w\.reactionHost\.Close" cmd/apogee/wire.go` prints the one teardown line.
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestSessionResolvesReactionMCPThroughTheLiveSet|TestRootWiringEmitsThroughTheHookRunner|TestRootWiringScrubsHookHeaderVariables|TestWatchedAdoptionRecordReloadsARepoReaction|TestE2EHooksFireFromTheTUI|TestE2EReactionsReloadSwapsTheArmedList' ./cmd/apogee/`

**Commit:** `refactor(apogee): compose the TUI session's observe lane through reactions.Host`

## 5. The facade exports the Reaction host

Depends on item 2.
**What:**
**Goal:** The public facade exports `apogee.NewReactionHost`, `ReactionHost`, `ReactionHostOptions`, `ReactionHostMCP`, `ReactionHostMCPNone` and `ReactionHostMCPLate`. A test in an external package that imports no `internal/reactions`, `internal/domain` or `internal/security` proves that an embedder gets the inert MCP notice and the workspace exec fence through them. This meets ADR 0073 §9.
**Approach (assumed at the header base):**
- `apogee.go`, in the Reactions section beside `NewReactionRunner`:
  - Aliases: `ReactionHost = reactions.Host`, `ReactionHostOptions = reactions.HostOptions`, `ReactionHostMCP = reactions.HostMCP`.
  - The two consts.
  - `func NewReactionHost(list []Reaction, o ReactionHostOptions) (*ReactionHost, error)`.
  - Godoc says the host carries:
    - the stock executor: the workspace exec fence (ADR 0073 §6) and the url-safety guard;
    - under `ReactionHostMCPNone`, the MCP disarm with one notice per entry;
    - the late binds.
    It also says `Install` fills the observe-lane Config keys and never `Config.MCP`, and that Close runs after the run.
- `NewReactionRunner`'s godoc gains one sentence pointing an embedder that wants the stock executor at `NewReactionHost`.
- `example_test.go`: add the new names to the compile-time facade pin lists.

**Regression guard.** The Acceptance grep matches import paths only, so a doc comment restating this Goal ("imports no internal/reactions …") cannot fail a correct test file: `grep -n '"github.com/airiclenz/apogee/internal/\(reactions\|domain\|security\)"' reactionhost_test.go` prints nothing.

**Files:** apogee.go, example_test.go, reactionhost_test.go
**Read first:** apogee.go — NewReactionRunner, ReactionRunner, MCPHandler, TurnEvent; example_test.go — the type-alias and forwarding-constructor pin lists;
internal/reactions/command_test.go — TestCommandExecutorRefusesAProgramInsideTheWorkspace (marker-file and windows-skip pattern); internal/reactions/match.go — matchTurn (Depth 0 only);
internal/userexec/userexec.go — ResolveProgram's "refusing to run %q" wording
**Tests:** New `reactionhost_test.go` (`package apogee_test`), `TestReactionHostFromTheFacade…`:
- (a) A user advise entry with an `apogee.MCPHandler`, under `ReactionHostMCPNone`, reports `reaction "<id>": its mcp: handler is inert here — MCP servers are not connected in this run` once. The entry is absent from `host.Lanes().Sync`.
- (b) A user observe argv `run:` that names a program inside the workspace, fired by emitting a turn-finished Event into `host.Runner()`, reports a line that contains `refusing to run`. Its marker file never appears. The test skips where no POSIX shell exists.
- Waits go through `internal/testtime`.

**Acceptance:**
- `go build ./...`
- `go vet .`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'TestReactionHostFromTheFacade|Example' .`
- `grep -n '"github.com/airiclenz/apogee/internal/\(reactions\|domain\|security\)"' reactionhost_test.go` prints nothing.
- `go doc . NewReactionHost` renders.

**Commit:** `feat(apogee): export NewReactionHost so an embedder gets the stock fence and MCP notices`

## 6. Knowledge: the Reaction host in the decision records and the glossary

Depends on items 3, 4 and 5.
**What:**
**Goal:** ADR 0073 carries the dated amendment `adr-0073-amend-1102`, named on its Status line and in its reading-order sentence. The glossary has a "Reaction host" Note under `glossary-reactions`, and the Reaction and Driver Notes link to it. The "one notice per entry" clause in `adr-0076-amend-1029` names the host as where the notice comes from.
**Approach (assumed at the header base):**
- Run `capcom know add --id adr-0073-amend-1102 --parent adr-0073`. The amendment says that §9's "one library, every root" now covers a Driver root's whole observe lane through the Reaction host:
  - the MCP disarm and its notices;
  - the url guard and the fenced DefaultExecutor;
  - the project-run and MCP late binds;
  - `reactions.CloseGrace`.

  It also says that each Driver root builds the host and the observe lane stays Driver-side (ADR 0076 A8), that the facade re-exports the host as `apogee.NewReactionHost`, and that the "nothing safety-relevant in only one Driver" rule (ADR 0073 context, glossary Driver) is met for the observe lane. Then edit `adr-0073.md`: the Status line and the "Read `adr-0073-context` …" sentence.
- Run `capcom know add --id reaction-host --parent glossary-reactions`, titled "Reaction host: …". The Note defines the term, says where it lives (`internal/reactions`, `apogee.NewReactionHost`), describes the two MCP modes and lists what Install fills and what it leaves to the Driver.
- `glossary-reactions/reaction.md`: the sentence about a Driver that injects no `Config.MCP` names the **Reaction host** as the owner of the one-notice-per-entry rule.
- `glossary-identity/driver.md`: add one sentence that each Driver composes its observe lane through the **Reaction host**.
- `adr-0076-amend-1029`: add a dated `> **Amended 2026-11-02 (this plan).**` blockquote under "Drivers without MCP". It says the notice is given by the Reaction host under `HostMCPNone`, and that a facade Driver that arms user `mcp:` entries without the host gets none. Name the blockquote on `adr-0076.md`'s Status line.
- `docs/manual/` has no embedder section at the base, so this item adds none.

**Regression guard.** item 6's Acceptance drops `TestDocs` (it runs only cmd/apogee/docs_env_test.go and checks nothing item 6 changes) and uses `capcom check` plus `capcom know show "ADR 0073" --with-children` and `capcom know show "Reaction host"` showing the new Notes, and `go test -count=1 -run 'Drift|Doc' ./...` is NOT used (machine rule) — use a targeted drift test only if one exists for the touched doc.

**Files:** .capcom/knowledge/decisions/adr-0073.md, .capcom/knowledge/decisions/adr-0073/adr-0073-amend-1102.md, .capcom/knowledge/glossary/glossary-reactions/reaction-host.md, .capcom/knowledge/glossary/glossary-reactions/reaction.md, .capcom/knowledge/glossary/glossary-identity/driver.md, .capcom/knowledge/decisions/adr-0076/adr-0076-amend-1029.md, .capcom/knowledge/decisions/adr-0076.md
**Read first:** .capcom/knowledge/decisions/adr-0073.md — Status line, reading-order sentence; .capcom/knowledge/decisions/adr-0073/adr-0073-decision.md — §9 One library, every root; .capcom/knowledge/decisions/adr-0073/adr-0073-context.md — one-Driver rule;
.capcom/knowledge/decisions/adr-0076/adr-0076-amend-1029.md — Drivers without MCP; .capcom/knowledge/decisions/adr-0076.md — Status line; .capcom/knowledge/glossary/glossary-reactions/reaction.md — MCPHandler sentence;
.capcom/knowledge/glossary/glossary-identity/driver.md — Driver definition
**Tests:** none (docs).
**Acceptance:**
- `capcom check`
- `capcom know show "ADR 0073" --with-children` shows the `adr-0073-amend-1102` Note.
- `capcom know show "Reaction host"` shows the new Note.

**Commit:** `docs(knowledge): record the Reaction host in ADR 0073 and the glossary`
