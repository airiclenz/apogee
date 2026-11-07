# MCP servers reconnect one by one, off the Update loop — plan

**Goal:** Every MCP server has its own connection lifecycle (ADR 0109): a save dials only the servers it adds or changes, on a Cmd goroutine, and the registry takes the finished dials in at the next idle point in one rebuild. The wait shows on the busy indicator and each outcome arrives as a per-server transcript note, so a slow or dead server never freezes the TUI or blocks a healthy one.
**Date:** 2026-11-07
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 4163c85d
**Closes:** mcp-reconnect-freezes-tui
**Sources:** ADR 0109 (`capcom know show "ADR 0109" --with-children`); `adr-0037-amend-1107`; ADR 0011 (C1, the 2026-09-30 engine-holds note, the 2026-10-19 `p.Send` note); ADR 0031; ADR 0095 (decisions 4, 7); `.capcom/issues/mcp-reconnect-freezes-tui.md`; `.capcom/issues/mcp-swap-midturn-race.md`; `docs/design/mcp-client.md`; `docs/design/test-drivers.md` §Waiting; glossary "Busy indicator".
**Ratified design calls** (owner, 2026-11-07):
- **Lifecycle:** per server (ADR 0109); one `mcp.Client` per server, held in `liveMCP`'s name→entry map.
- **Order:** the `mcp-swap-midturn-race` fix lands first, as its own item.
- **Startup:** stays all-or-nothing and fatal.
- **Commit:** one registry rebuild and one `SwapTools` per idle point, in config order.
- **Failed re-dial:** keeps the old session; a new server whose first dial fails stays absent, with no auto-retry.
- **Removal / deny:** the entry is marked closed on the keystroke (calls refused `mcp server "X" is not connected`), its process closes off the loop, its tools leave at the next idle rebuild; a deny also cancels that server's in-flight dial.
- **Notes:** one ephemeral transcript note per server: `mcp <server> reconnected`, `mcp <server> connected`, `mcp reconnect failed: <server>: … — previous connection kept`, `mcp connect failed: <server>: …`.
- **Row:** the `mcp-servers` row shows `reconnecting MCP…`, then `saved` or `saved — live apply failed: <server>: …` if the surface is still open.
- **Supersede:** a generation per server; the newest request cancels that server's in-flight dial; a stale result closes silently (no note for a superseded `context canceled`).
- **Diff:** only servers whose resolved `ServerConfig` differs are re-dialled (custom equality, pin, `headers-env` names and auth included).
- **Re-admit:** `mcp.Admit` runs again under the live url-safety lists at commit.
- **Pin / login:** `PinMovedError` and `LoginRequiredError` follow the keep-old rule with today's cause text; a moved pin still cues the config re-read.
- **Adoption:** adopt/reject return at once and report nothing about MCP; the pin-skip note moves to the transcript.
- **Quit:** cancels dials, closes every live and pending-close server in parallel, and waits for all.
- **url-safety:** the deny clause stays on the row; a newly admitted server's dial reports by note.
- **Busy word:** `reconnecting MCP…`, one word whatever the count, ranked just below `connecting…` and above a pop-up's loading row.
- **Timing:** dials start at once, mid-Turn included; only the commit waits for idle.
- **Scope:** TUI only (headless builds no `rootWiring`); a `/mcp` status view is out (`mcp-live-status-view`).
- **Mid-Turn url-safety deny:** the MCP deny half runs first, mid-Turn included, before and regardless of the idle-only host-list swap (owner, 2026-11-07).
- **Retry:** an $EDITOR round trip on the mcp-servers row, unchanged included, re-dials every failed server; nothing else re-dials an untouched failed server (owner, 2026-11-07; ADR 0109 D7).
**Regression check (2026-11-07, 4163c85d):**
- 1: guard folded (driven test in-process under -race; readiness waits on the held stubllm request).
- 2: guard folded (a nil retire flag reads live; an httptest call-counting observer).
- 3: recast (per-server connect seam taking the caller's ctx and guard lists; re-pin scope is a rule; `wire_mcp_test.go` new); guards folded (login test, adopted-server and broken-Project re-pins, `projectrules.go`, comment rule); supersedes ADR 0037 decision 7 via `adr-0037-amend-1107`. Re-check: guards folded (session ctx, never `defer cancel()`; a failed or skipped server holds no live entry and a kept session keeps its OLD config; widened comment rule and grep; `LeavesMCPAloneWhenNoVerdictMoved` re-pinned through a `session(name)` accessor; the `offerUnsaved` pass stays ahead of the first dial).
- 4: recast (commit's distinct "refused — engine busy, still pending" outcome; an Admit-denied result is noted `mcp server <name> disconnected — its endpoint is denied`); guards folded (dial ctx lives until its session closes, sync `reconnect` joins its closes, `isClosed()` accessor, `takeWork()` store, newest-intent order); the guard's "reconnect discards the pending results" half yields to that decision. Re-check: recast (decision: diff against the last-requested configured set, a failed new server re-dialled only when its own config or admit verdict moves, per ADR 0109 D7); guards folded (decision: `drainMCP` joins every queued close and `fakeMCPSession.closed` is read only through the synchronized accessor; sync `reconnect` keeps item 1's `CanSwapTools` pre-check ahead of `request`). Re-check (retry): recast (decision: the Goal states the last-requested-set diff — each name's resolved `ServerConfig` and admit verdict, failed servers included — plus a retry-failed flag set only by the `mcp-servers` row commit; the newest-intent order is dropped).
- 5: guard folded (decision: "still pending" retried at the next idle point; `holdPrebound` in `canSwapTools`; `CommitMCP` reports pending; `canStartMCPDials` is no `holdQuitting` only, per ADR 0109 decision 3).
- 6: guard folded (a `stopped` latch on every exit of `quitSession`).
- 7: guard folded (item 1's driven test and the busy-engine test re-pinned; success leaves the note cell empty; the unparseable-file test re-pointed; `CommitMCP` pre-checks `CanSwapTools`; widened comment grep). Re-check (retry): guard folded (decision: the row commit passes the retry-failed flag; a watched save with unchanged config re-dials no failed server).
- 8: guard folded (decision: depends on item 7; supersede test via `TakeMCPWork`; drain before the stale-adoption check).
- 9: recast (mid-Turn url-safety deny runs before the host-list swap; depends on item 8); guards folded (`.reconnect(` caller rule, login-test re-pin, the label test keeps a real `broken` refusal). Re-check: guards folded (decision: item 4's last-requested diff; deny half before `move`, `request` after it; a refused `move` carries the deny clause, so the manual's "the row names it" stays true; `TestMCPReconnectRefusesBeforeDiallingWhileAnExchangeRuns` deleted with `reconnect`; the driven test answers the MCP approval pane; deny and failed-reconnect tests re-seeded with a live `docs` entry; the unparseable-file test kept and run).
- 3: Re-check (retry): guard folded (decision, replacing the rejected re-send finding: an explicit `mcp-servers` row commit re-dials every server whose last dial failed, per ADR 0109 decision 7 and ADR 0037 decision 1; `TestLiveMCPRecommitRedialsOnlyTheFailedServer`).
- 10: guard folded (each model turn answers the MCP approval pane; the gate closes through one `sync.Once`).
- 11: guard folded (busy-list acceptance limited to enumerations; comment grep covers `internal/mcp` and `swaptools.go`; `-run 'Drift|Doc|Manual'`).
- 7: Re-check (retry gesture): recast (decision: the `$EDITOR` round trip on the `mcp-servers` row is the retry gesture — an unchanged round trip requests the retry-failed flag when any server's last dial failed and stays a no-op when none did, a changed one passes it too; route `foldSettingsEdit` → `MCPHost.RetryFailedMCP` → `requestMCP`).
**Standing requirements:**
- skills: coding-standards
- This box: never race-run a whole heavy package; narrow to `GOMEMLIMIT=2GiB go test -race -count=1 -run <TestX> ./<pkg>/`, one run at a time.
- Durations in tests go through `internal/testtime`; `WaitQuiet` never stands in for readiness.
- No dial timeout is added (slow-box rule); nothing called from Update waits on a goroutine blocked in `p.Send`.
**Out of scope:**
- Startup MCP connect semantics; the OAuth login flow (ADR 0095 D4: a mid-session dial never prompts).
- headless / daemon Drivers; the `/mcp` live status view.

## 1. Mid-Turn MCP reconnect never races the Agent — ✅ DONE (2026-11-07)
NOTES (2026-11-07): the TryLock-and-inExchange check is one helper, `Agent.holdIdleForSwap`, shared by `SwapTools` and `CanSwapTools`. The plan describes the check, not where it lives.
NOTES (2026-11-07): added `TestSwapToolsCanSwapAtIdle` (the probe answers nil at idle) and a `CanSwapTools` assertion inside the existing `TestSwapToolsRefusedMidExchange`. Both are extra coverage, inside the plan's `TestSwapTools` acceptance pattern.
NOTES (2026-11-07): the e2e test's request-counting server is a local helper, `newCountingPingMCPServer`, in `e2e_mcpmidturn_test.go`, because `newPingMCPServer` (mcp_cmd_test.go, not in this item's Files) exposes no counter. The test was checked to be discriminating: with the `CanSwapTools` pre-check disabled it fails (the second server saw 5 requests).
NOTES (2026-11-07): the `agent.go` call-class comment now states that SwapTools alone checks its boundary under stepMu and may be called from any goroutine. The `swaptools.go` "discipline IS the synchronization" paragraph is rewritten per the item's comment rule.

**What:**
**Goal:** `Agent.SwapTools` and a new `Agent.CanSwapTools() error` read the Agent's Exchange state race-free against the worker, and no swap takes effect while a Step or Run is in progress or an Exchange is open. A watched `mcp-servers` save folded mid-Turn dials nothing and swaps nothing; its outcome is today's text, `mcpReconnectFailed(errSwapToolsInExchange)` byte for byte, and the old tools stay.
**Approach (assumed at the header base):** in `internal/agent`, the Agent gains a `stepMu sync.Mutex` that the exported `Step` and `Run` hold for the whole call (the private `step` and `finishAtStepCap` never lock). `SwapTools` takes it with `TryLock`: when it is held, or `turns.inExchange` reads true under it, the swap is refused with `errSwapToolsInExchange`. `CanSwapTools` gives the same answer without swapping. A bare atomic is not enough: it leaves a window between the check and the swap in which a Step can start on the worker. In `cmd/apogee`, `settingsEngine` (`wire.go`) gains `CanSwapTools() error`. `lateEngine` forwards it, answering `errNoServerBound` when unbound, and `applySettingSpy` answers `swapErr`. `liveMCP.reconnect` asks `engine.CanSwapTools()` before it calls `connect`, and a refusal returns `mcpReconnectFailed(err)` with nothing dialled. Rule: every comment that calls the idle-only boundary SwapTools' synchronisation is rewritten; find them with `grep -n "synchroni\|happens-before" internal/agent/swaptools.go internal/agent/agent.go`.
**Regression guard.**
- `TestE2EWatchedMCPSaveMidTurnDialsNothing` is driven in-process (`launchTUIConfigured` + `tuitest.Driver`), under the suite's `-race`; the PTY binary is built without `-race` (`main_test.go` `buildE2EBinary`), so the race half of this item bites only there and in the agent test.
- Readiness: before `renameHomeConfig`, the test waits until `stub.Requests()` shows the held request (logged before the `Await` gate), not only for the painted `queue a message…` placeholder, which paints before the worker's Step takes `stepMu`.
**Files:** internal/agent/agent.go; internal/agent/swaptools.go; internal/agent/swaptools_test.go; cmd/apogee/wire.go; cmd/apogee/wire_engine.go; cmd/apogee/wire_mcp.go; cmd/apogee/wire_helpers_test.go; cmd/apogee/wire_settings_test.go; cmd/apogee/e2e_mcpmidturn_test.go
**Read first:** internal/agent/swaptools.go — SwapTools, errSwapToolsInExchange; internal/agent/agent.go — Step, Run, finishAtStepCap; cmd/apogee/wire_mcp.go — liveMCP.reconnect; cmd/apogee/wire_engine.go — lateEngine.SwapTools, errNoServerBound;
cmd/apogee/wire_helpers_test.go — applySettingSpy; cmd/apogee/wire_settings_test.go — TestApplySettingMCPReconnectKeepsEverythingWhenTheEngineIsBusy; internal/tui/settingswatcher.go — foldConfigChanged, configWatchAppliedNote; cmd/apogee/mcp_cmd_test.go — newPingMCPServer
**Tests:**
- `TestSwapToolsRefusedWhileAStepRuns` (internal/agent): a Step is held on a gated provider. SwapTools and CanSwapTools, called from another goroutine, both answer `errors.Is(err, domain.ErrInputPending)`, and the registry is unchanged.
- `TestMCPReconnectRefusesBeforeDiallingWhileAnExchangeRuns`: the spy sets `swapErr`, `connect` is never entered, and the error equals `mcpReconnectFailed(swapErr).Error()`.
- `TestApplySettingMCPReconnectKeepsEverythingWhenTheEngineIsBusy`, re-pinned: no dial, and the old session is open.
- Driven in-process `TestE2EWatchedMCPSaveMidTurnDialsNothing`:
  - the session starts on a ping server (`newPingMCPServer`), and the prompt's reply is held at a stubllm `Await` gate;
  - with `queue a message…` painted and the held request in `stub.Requests()`, `renameHomeConfig` repoints the server at a second, request-counting ping server;
  - it waits for the note opening `config changed on disk — applied: `, asserts that the second server saw no request, then releases the gate and waits idle.
**Acceptance:**
- `go build ./...`; `go vet ./internal/agent/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestSwapTools' ./internal/agent/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestMCPReconnectRefuses|TestApplySettingMCPReconnect|TestE2EWatchedMCPSaveMidTurn' ./cmd/apogee/`
**Closes:** mcp-swap-midturn-race
**Commit:** `fix(agent): a mid-Turn MCP reconnect never races the worker`

## 2. internal/mcp: per-server primitives — ✅ DONE (2026-11-07)
NOTES (2026-11-07): consequential edit — internal/mcp/mcp_test.go: made necessary by listServerTools taking the Client's retire switch (the one direct test call passes nil, which reads live).
NOTES (2026-11-07): tool_test.go is unchanged: newServerTool keeps its signature and the retire switch is set by listServerTools beside `project`, so every direct construction carries a nil switch that reads live; its existing tests pass as they stand.
NOTES (2026-11-07): TestClientRetireRefusesEveryToolCall makes one control call before retiring (so the counter is shown to count) and asserts the count does not move after retirement, rather than staying at zero; it also runs a Close row (Close implies Retire). TestServerConfigEqual_CoversEveryField pins ServerConfig's field count (14) so a new field forces Equal to be revisited.
NOTES (2026-11-07): the refusal text is one constant, mcpNotConnectedFormat, shared by the retired-tool failure and Resolver's not-connected error (same words as before).

**What:**
**Goal:** `internal/mcp` offers:
- `ServerConfig.Equal(other ServerConfig) bool`, true exactly when a re-dial would connect the same way: every field by value, lists and maps by content, `EnvAllowlist` nil distinct from empty, `HeadersEnv` by its env-var names, `Pin` by `reflect.DeepEqual` (per `domain.ProjectPin`'s contract);
- `ValidateServers(servers []ServerConfig) error`, the exported whole-set check that `ConnectWith` runs today;
- `(*Client).Retire()`, after which every tool the Client surfaced refuses a call with the failure text `mcp server "<alias>" is not connected` without touching its session.

`Close` implies Retire. The one-client-per-set shape is unchanged.
**Approach (assumed at the header base):** `Equal` lives beside `ServerConfig` and `Clone` in `transport.go`. `ValidateServers` wraps `validateServers` in `client.go`, which `ConnectWith` keeps calling. `Retire` sets an `atomic.Bool` that the Client shares with each `serverTool` `listServerTools` builds. `serverTool.call` checks it before the `caller == nil` arm and returns the refusal as its `failure` string (the same channel the not-connected arm uses), so `Execute` and `CallText` both carry it. No new package.
**Regression guard.**
- A nil retire flag reads as live, so `tool_test.go`'s direct `newServerTool(…, caller, nil)` constructions (no Client) keep passing and never panic in `serverTool.call`.
- `TestClientRetireRefusesEveryToolCall`'s observer is an `httptest` streamable-http server whose tool handler counts calls, built as `TestConnect_ConfiguredHeadersRideEveryRequest` builds one; the re-exec'd stdio fixture (`stdioServerConfig`) has no counter the test can read.
**Files:** internal/mcp/transport.go; internal/mcp/client.go; internal/mcp/tool.go; internal/mcp/transport_test.go; internal/mcp/tool_test.go
**Read first:** internal/mcp/tool.go — serverTool, serverTool.call, newServerTool, Resolver; internal/mcp/client.go — listServerTools, Client.Close, validateServers, ConnectWith; internal/mcp/transport.go — ServerConfig, ServerConfig.Clone;
internal/domain/reaction.go — ProjectPin; internal/mcp/tool_test.go — fakeCaller; internal/mcp/transport_test.go — TestConnect_ConfiguredHeadersRideEveryRequest
**Tests:**
- `TestServerConfigEqual`: a table with one changed field per row (args order, an env entry, a header value, a `HeadersEnv` name, auth, ClientID, a nil vs an empty EnvAllowlist, a pin with different files, Project) plus an equal clone.
- `TestValidateServersRefusesADuplicateName`.
- `TestClientRetireRefusesEveryToolCall`: over an `httptest` streamable-http server with a call-counting tool handler, a retired client's tool answers exactly `mcp server "<alias>" is not connected` and the server's count stays zero.
- `tool_test.go`'s existing tests pass unchanged (nil retire flag reads live).
**Acceptance:**
- `go build ./...`; `go vet ./internal/mcp/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestServerConfigEqual|TestValidateServers|TestClientRetire' ./internal/mcp/`
**Commit:** `feat(mcp): server equality, whole-set validation and a retire switch`

## 3. liveMCP holds one connection per server — ✅ DONE (2026-11-07)
NOTES (2026-11-07): `newLiveMCP` takes a leading `ctx` (the wiring's session ctx) beside the entries and the per-server connect: `reconnect` takes no ctx, so the holder keeps the one every dial runs under. The seam's guard lists are `allowHosts, denyHosts []string`, which `reconnect` reads from `set.built()`. That is the same read the old closure made.
NOTES (2026-11-07): `mcpOutcome` carries a third field, `err` (the failed dial's cause), so the `mcp-servers` row error (`mcpRowFailures`, type `mcpRowError`) still wraps each cause for errors.Is/As, as `mcpReconnectFailed` did. Its text is the rowFailures joined by "; ", as planned.
NOTES (2026-11-07): when no dial landed and no server was removed, `reconnect` skips the rebuild and `SwapTools`, because the registry would be rebuilt onto the tools it already carries. This keeps `TestApplySettingMCPReconnectKeepsTheOldSessionsWhenTheDialFails`'s "a failed dial moves nothing" assertion.
NOTES (2026-11-07): startup now runs `mcp.ValidateServers` before the `offerUnsaved` login pass, not inside the first `ConnectWith` after it. A set it refuses aborts before any login offer. The login pass still runs before the first dial.
NOTES (2026-11-07): `reconnectMCP`/`reconnectMCPFrom` now return only `error`: the old note is gone, since a pin skip is now a rowFailure. `applyMovedKinds` joins that error unchanged. The pin-skip note `mcpSkipNote` is deleted, and `mcpSkipClause` stays for the startup notice.
NOTES (2026-11-07): the fixture's seed (`mcpFixtureSeed`) and a `newMCPFixtureWith(entries, …)` variant were added to `wire_helpers_test.go`, and `fakeApplier` seeds the same `seed` entry. Re-seeded so that each test keeps its intent: `…KeepsTheOldSessionsWhenTheDialFails` (a live `docs` on a stale config), `…DropsAnMCPServerTheNewListDenies` and `…LeavesMCPAloneWhenNoVerdictMoved` (a live `docs`; the identity check goes through `session("docs")`), and `…ReportsAFailedReconnectInTheNote` (a second server `wiki` on another host, live on a stale config, so the url-safety edit re-dials a CHANGED server).
NOTES (2026-11-07): consequential edit — cmd/apogee/doc.go: made necessary by the per-server reconnect replacing the "validate-then-commit reconnect" its file map named (the item's comment rule lists doc.go).
NOTES (2026-11-07): consequential edit — cmd/apogee/wire_settings_test.go `TestSettingsApplierReloadsRefuseAnUnparseableFile`: made necessary by `reconnectMCP` now returning one value. Also `TestMCPReconnectUsesTheLiveURLSafetyLists` (wire_live_test.go) was re-pinned to read the refusal from the outcome's row failure.

**What:** Recast at the regression check (2026-11-07).
**Goal:** `liveMCP` holds a name→entry map, and each entry has its resolved `ServerConfig` and its own one-server session. Startup dials server by server: still all-or-nothing and fatal, with the same error text and the same pin-skip notice. `liveMCP.reconnect` is per server and still synchronous on its caller:
- unchanged servers are left alone, removed ones are dropped, and added or changed ones are dialled;
- a failed re-dial keeps that server's old session, and a failed new server stays absent;
- one rebuild and one `SwapTools` follow, after which replaced and removed sessions close.

User-visible failure text becomes per server (ADR 0109 D7).
**Approach (assumed at the header base):**
- **Entries:** `newLiveMCP(entries []mcpEntry, connect)` takes a per-server `connect` that dials one `mcp.ServerConfig` under the caller's `ctx` (plus the guard lists `Admit` needs). `tools()` concatenates the entries in config order, so the reactions `mcp:` resolver (`mcp.Resolver` over `w.mcpSet.tools()`, `wire_live.go`) reads live entries unchanged.
- **Startup:** `rootWiring.connectMCPServers` (`mcp_cmd.go`) runs `mcp.ValidateServers` over the whole set, then `mcp.ConnectWith` per server in config order. It keeps the OAuth login-retry loop per server and closes every dialled entry before returning an error. `announceSkippedMCP` still runs over the skips.
- **Reconnect:** `reconnect(servers, set, engine) ([]mcpOutcome, error)` keeps item 1's pre-check and runs `ValidateServers` first; both refusals are the whole-call `mcpReconnectFailed` error, as today. It diffs by name and `Equal`, dials, and folds the results into one `set.rebuild`. A refused swap closes the new sessions and returns the refusal with nothing moved.
- **Outcomes:** `mcpOutcome{note, rowFailure}` lives in `wire_mcp.go`:
  - `note` is exactly one of the four ratified texts, with `…` = `mcpSentence(err)`;
  - `rowFailure` is `<server>: <mcpSentence(err)>`, empty on success;
  - a pin skip (`Skipped()` non-empty) counts as a failure whose cause is its `Denied.Err`, and still runs `cueSkippedMCP`;
  - the `mcp-servers` arm's error joins the rowFailures with `"; "`, and the `url-safety` clause is `"; " + note` per failure.
- **Fixture:** `newMCPFixture` keeps its signature and seeds `start` as one entry named `seed` with an empty config, so any re-read removes and closes it.
**Regression guard.**
- Decision: the liveMCP connect seam is per server and takes the caller's context — it dials one mcp.ServerConfig under a ctx (plus whatever guard lists Admit needs), so items 4–6 can cancel, supersede and quit a dial; cmd/apogee/wire_mcp_test.go is a NEW file, not a re-pin; the re-pin scope is a rule, not a list — every test that calls newMCPFixture or liveMCP.reconnect (grep both) is re-pinned, and a test whose start session the new seed would turn into a REMOVED server is re-seeded so it keeps its original intent (e.g. "the old session stays open after a failed dial" stays asserted on a changed, not removed, server).
- Re-pins the rule covers, as found at the check: `TestStartupMCPLoginReconnectNeverPrompts` (err is nil; the outcome's rowFailure is `remote: …apogee mcp login remote…`; the terminal is still never read); `TestWatchedProjectConfigAdoptedServerConnects` (two one-server dials, docs (global) then wiki (Project)); `TestWatchedBrokenProjectConfigDropsAdoptedServerUntilItParses` (dials `[[docs],[wiki],[wiki]]`; the wiki session `sessions[1]` closed, docs' `sessions[0]` not).
- `projectrules.go` `applyMovedKinds` consumes `reconnectMCPFrom`'s `(note, err)`: after this item an adoption returns the joined rowFailures, a pin skip among them as a rowFailure (item 8 later empties it).
- Rule: every comment describing a whole-set reconnect or the previous-connections-kept sentence, every comment that calls a pin skip the reconnect's note, and every comment that calls the reconnect a whole-set validate-then-commit is rewritten; find them with `grep -n "all-or-nothing\|whole set\|previous connections\|tear.* down\|rest of the set\|reconnect's note\|row's note\|validate-then-commit reconnect" cmd/apogee/*.go` (startup's all-or-nothing stays; found at the check: `wire_settings.go`, `projectrules.go`, `wire_live.go`, `wire.go`, `wire_mcp.go`, `doc.go`). The whole-set validate-then-commit those comments record is ADR 0037 decision 7, superseded by `adr-0037-amend-1107`; cite the amendment.
- The per-server seam dials under wireSession's session ctx (today's closure, `wire_live.go`), or a child whose cancel is held until that entry closes — never a ctx that ends when `reconnect` returns, never `defer cancel()`: go-sdk v1.6.1 `SSEClientTransport.Connect` binds the SSE GET stream to the Connect ctx.
- A server whose dial failed or was pin-skipped (at startup or on reconnect) holds no live entry, and a kept old session keeps its OLD config — a failed re-dial never overwrites the entry's config (item 4 adds the last-requested record that keeps an unrelated save from re-dialling it).
- `TestApplySettingURLSafetyHostsLeavesMCPAloneWhenNoVerdictMoved` is re-pinned under the rule: its fixture is re-seeded with a live `docs` entry, and its identity assertion (today `fixture.set.swap(serving)`, which a name→entry map cannot keep) reads a new entry accessor, `liveMCP.session(name)`.
- Login: the `offerUnsaved` pass over every `auth: oauth` server stays ahead of the first dial (`mcp_cmd.go`); only the `LoginRequired` retry is per server, at most once each.
- Decision: an explicit commit of the `mcp-servers` settings row re-dials every server whose last dial failed (a new-and-absent server, or a changed server whose old session was kept), plus whatever the commit changed — keeping ADR 0037 decision 1's "re-committing the row retries the apply" (ADR 0109 decision 7 now says so); a watched save, an adoption or a url-safety edit that leaves a failed server untouched never re-dials it; the item's test re-commits the row with the same config after a failure and sees exactly one more dial of the failed server and none of the healthy one.
**Files:** cmd/apogee/wire_mcp.go; cmd/apogee/wire_live.go; cmd/apogee/mcp_cmd.go; cmd/apogee/wire_settings.go; cmd/apogee/projectrules.go; cmd/apogee/wire.go; cmd/apogee/wire_helpers_test.go; cmd/apogee/settingsapplier_helper_test.go; cmd/apogee/wire_mcp_test.go (new); cmd/apogee/configwatch_apply_test.go; cmd/apogee/mcp_cmd_test.go; cmd/apogee/wire_settings_test.go; cmd/apogee/wire_live_test.go; cmd/apogee/projectrules_test.go (the last five only re-pin tests)
**Read first:** cmd/apogee/wire_mcp.go — liveMCP, newLiveMCP, reconnect, swap, mcpSkipNote, mcpReconnectFailed; cmd/apogee/mcp_cmd.go — connectMCPServers, announceSkippedMCP, cueSkippedMCP; cmd/apogee/wire_live.go — wireSession (newLiveMCP connect closure, mcp.Resolver);
internal/mcp/client.go — ConnectWith, validateServers, Client.Skipped; cmd/apogee/wire_helpers_test.go — newMCPFixture, fakeMCPSession; cmd/apogee/projectrules.go — applyMovedKinds;
cmd/apogee/mcp_cmd_test.go — TestStartupMCPLoginOffersEveryServerBeforeTheConnect, TestReconnectSkipsARepoServerWhosePinMoved; cmd/apogee/wire_live_test.go — TestApplySettingURLSafetyHostsLeavesMCPAloneWhenNoVerdictMoved
**Tests:**
- New, in the new `wire_mcp_test.go`: `TestLiveMCPReconnectLeavesAnUnchangedServerAlone`, `TestLiveMCPFailedRedialKeepsTheOldSession` (the kept entry still carries its OLD config), `TestLiveMCPBrokenServerSparesTheHealthyOne` (the failed server holds no entry), `TestLiveMCPToolsFollowConfigOrder`, `TestLiveMCPRecommitRedialsOnlyTheFailedServer` (a `reconnect` with the same config after a failure dials the failed server exactly once more and the healthy one not at all; covers a failed new server and a changed server whose old session was kept).
- Re-pinned to per-server semantics: every test that calls `newMCPFixture` or `liveMCP.reconnect` (grep both), among them `TestWatchedBrokenProjectConfigDropsAdoptedServerUntilItParses` (dials `[[docs],[wiki],[wiki]]`), `TestWatchedProjectConfigAdoptedServerConnects`, `TestStartupMCPLoginReconnectNeverPrompts`, `TestReconnectSkipsARepoServerWhosePinMoved` (the rowFailure opens `wiki: stdio server "wiki": refusing to launch — bin/wiki.sh changed since you adopted it`, and `ping` stays live), `TestApplySettingMCPReconnect*`, `TestMCPReconnect*`, `TestApplySettingURLSafety*` (`…LeavesMCPAloneWhenNoVerdictMoved` re-seeded with a live `docs` entry, its identity check through `session("docs")`), `TestProjectRule*`; a test whose start session the seed turns into a removed server is re-seeded to keep its intent.
- The other startup tests in `mcp_cmd_test.go` pass unchanged, `TestStartupMCPLoginOffersEveryServerBeforeTheConnect` among them.
**Acceptance:**
- `go build ./...`; `go vet ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestLiveMCP|TestMCPReconnect|TestApplySettingMCP|TestApplySettingURLSafety|TestWatched|TestReconnectSkips|TestStartup|TestProjectRule' ./cmd/apogee/`
**Depends on:** item 1, item 2
**Commit:** `refactor(mcp): the live set holds one connection per server`

## 4. liveMCP async API (cmd/apogee side) — ✅ DONE (2026-11-07)
NOTES (2026-11-07): liveMCP keeps two WaitGroups, `dials` (running jobs) and `closes` (off-loop closes), not the one the approach names. close() waits on both. The sync `reconnect` waits on `closes` only, so it never blocks on an unrelated dial that hangs.
NOTES (2026-11-07): the rebuild flag is named `needsRebuild` (the boolean-prefix rule), not `rebuild`.
NOTES (2026-11-07): `request` takes a `security.URLGuard`. The `mcpConnect` seam still takes host lists, so each job passes `guard.AllowHosts` and `guard.DenyHosts`, and the item-3 seam and `settingsapplier_helper_test.go` stay as they are.
NOTES (2026-11-07): deny at request time. A live entry whose own dialled config the guard now denies is disconnected at once (retired, dropped from the set, closed off the loop). A not-live server whose verdict alone moves to denied has its pending work dropped and is not dialled. A config edit onto a denied endpoint still queues a dial: connect refuses it with today's text and the old session is kept, so the sync callers see no change.
NOTES (2026-11-07): when a commit re-admits a result and the lists now deny it, the server's live old session is closed too, so the `mcp server <name> disconnected — its endpoint is denied` note is true. The still-pending outcome is the `mcpPendingError` type, and the sync `reconnect` maps it to `mcpReconnectFailed(cause)`.
NOTES (2026-11-07): consequential edit — cmd/apogee/configwatch_apply_test.go: made necessary by fakeMCPSession's state now being read only through isClosed() (the off-loop close).
NOTES (2026-11-07): consequential edit — cmd/apogee/wire_live_test.go: made necessary by fakeMCPSession's state now being read only through isClosed().
NOTES (2026-11-07): consequential edit — cmd/apogee/wire_settings_test.go: made necessary by fakeMCPSession's state now being read only through isClosed().
NOTES (2026-11-07): consequential edit — cmd/apogee/doc.go: made necessary by wire_mcp.go's role changing from the sync reconnect to the request/dial/commit lifecycle (one map line).
NOTES (2026-11-07): `grep -n '\.closed\b' cmd/apogee/*_test.go` still matches `headless_test.go:3587,3595`. That is the `closed` counter of a different fake, not fakeMCPSession, and it is left alone.
NOTES (2026-11-07): wire_mcp.go is now about 700 lines, past the ~400-line guidance. It stays one file, as the item's Where line says.

**What:** Recast at the regression check (2026-11-07).
**Goal:**
- **request:** `liveMCP.request(servers, guard, retryFailed bool)` runs on the caller. It validates the set, then marks every removed server's entry closed at once (`Retire`, queues its close off the loop, cancels its in-flight dial, bumps its generation). It diffs against the last-requested configured set — each name's resolved `ServerConfig` and admit verdict, failed servers included — and queues one `mcpDialJob` per server whose config or verdict moved, plus, when `retryFailed` is set, one per server whose last dial failed. The jobs wait on liveMCP, handed out once by `takeWork()` (which item 7's `TakeMCPWork` wraps), and each cancels any dial it supersedes. It also sets `rebuild` when anything was marked closed.
- **dial job:** each job runs off the loop and stores its result for commit, or closes it at once if its generation is stale.
- **commit:** `liveMCP.commit(set, engine) ([]mcpOutcome, error)` runs on the caller. It takes the current-generation results and re-runs `mcp.Admit` under `set.built()`'s lists; a denied result is closed off the loop and noted `mcp server <name> disconnected — its endpoint is denied`. Everything folds into one config-order rebuild and one `SwapTools`; replaced and closed-marked sessions then close off the loop. A refused swap is the distinct "refused — engine busy, still pending" outcome (the non-nil error): every result stays pending for the next commit and no note is written.
- **close:** `close()` cancels every dial, waits for the started ones, then closes every live entry, finished result and pending close in parallel and waits for all.

The synchronous `reconnect` is item 1's `CanSwapTools` pre-check, then `request` + the jobs run inline + `commit`, and it waits for the closes it queued before it returns, so behaviour is unchanged for its callers.
**Approach (assumed at the header base):**
- **Where:** extends `liveMCP` in `cmd/apogee/wire_mcp.go`; no new package.
- **mcpSession:** gains `Retire()` (`wire.go`); `fakeMCPSession` implements it and gains mutex-guarded `isClosed()` and `isRetired()` accessors (named apart from its `closed` field).
- **Jobs:** `mcpDialJob{server string; gen uint64; run func(ctx context.Context)}` derives its own cancellable ctx from the one passed in and registers on liveMCP's WaitGroup inside `run`, under the mutex, refusing to start once closed, so a Cmd the program never ran cannot hang teardown.
- **Guard and closes:** the guard is built on the caller from `set.built()`; every off-loop close is a goroutine added to the same WaitGroup.
- **Test helper:** `drainMCP(t, set, tools, engine)` in `wire_helpers_test.go` takes the pending jobs through `takeWork()`, runs them inline, commits, and joins every close liveMCP queued before it returns.
**Regression guard.**
- Decision: the commit step has a distinct "refused — engine busy, still pending" outcome: the results stay pending for the next idle point, are never discarded and write no note; fix that signature once here; a dial result that mcp.Admit denies at commit is closed off-loop and noted `mcp server <name> disconnected — its endpoint is denied` (owner, 2026-11-07, ADR 0109 grill Q10), never reported as a failure with a denial cause.
- Sync `reconnect` maps the "still pending" outcome to `mcpReconnectFailed(err)`, so its row never reads `saved` over a refused swap; per the decision it keeps the results pending rather than discarding them.
- The dial ctx's cancel is kept until that session closes, never `defer cancel()`: an SSE stream lives on the connect ctx (go-sdk v1.6.1 `sse.go` `SSEClientTransport.Connect`), so cancelling at dial return would kill every SSE server.
- Sync `reconnect` joins the closes it queued (a WaitGroup wait over its own closes), so the existing `.closed` reads after it (e.g. `wire_settings_test.go`, `configwatch_apply_test.go`, `wire_live_test.go`) stay race-free; off-loop tests read `isClosed()`.
- When a request's config for a server equals its live entry's, that server's in-flight or pending work is cancelled or discarded and no job is emitted (ADR 0109 decision 2: no re-dial, no tool churn).
- Decision: the test drain helper (drainMCP) also joins every queued off-loop close before it returns, and fakeMCPSession's closed state is read through a synchronized accessor, so no test after a request races the off-loop Close; fix the helper and the accessor once here, and items 7, 8 and 9 use them (never a raw `closed` field read).
- Decision: liveMCP diffs a request against the last-requested configured set (each name's resolved ServerConfig and its admit verdict, a failed new server included as configured-but-failed), never against the live sessions alone, so a server whose first dial failed is re-dialled only when its own config changes, a url-safety edit moves its own admit verdict or a request carries the retry-failed flag — never by a watched save, an adoption or a url-safety edit that leaves it untouched (ADR 0109 D7, owner grill Q14 2026-11-07); add a test that a second unrelated save does not re-dial a failed new server.
- Sync `reconnect` runs item 1's `CanSwapTools` pre-check before `request` touches any entry: a busy engine returns `mcpReconnectFailed(err)` with nothing retired, closed or dialled, so `TestE2EWatchedMCPSaveMidTurnDialsNothing` and `TestApplySettingMCPReconnectKeepsEverythingWhenTheEngineIsBusy` stay green.
- Decision: the request API carries an explicit "retry failed" flag set only by the row-commit path (item 7 sets it, items 8 and 9 never do). Until items 7–9 move its callers, sync `reconnect` passes it set, so item 3's `TestLiveMCPRecommitRedialsOnlyTheFailedServer` stays green.
**Files:** cmd/apogee/wire_mcp.go; cmd/apogee/wire.go; cmd/apogee/wire_live.go; cmd/apogee/wire_helpers_test.go; cmd/apogee/wire_mcp_test.go
**Read first:** cmd/apogee/wire_mcp.go — liveMCP, reconnect, close, closeSession; cmd/apogee/wire.go — mcpSession, rootWiring teardown (mcpSet.close); cmd/apogee/wire_tools.go — liveTools.rebuild, liveTools.built;
internal/mcp/transport.go — buildTransport, buildStdioTransport; internal/mcp/client.go — ConnectWith, connectOne, Admit; cmd/apogee/wire_helpers_test.go — fakeMCPSession, newMCPFixture;
cmd/apogee/wire_settings_test.go — TestApplySettingMCPReconnectKeepsEverythingWhenTheEngineIsBusy, TestApplySettingMCPReconnectSwapsTheToolsAndClosesTheOldSessions
**Tests:** `TestLiveMCPRequestDialsOnlyChangedServers`; `TestLiveMCPUnrelatedSaveNeverRedialsAFailedNewServer` (a new server's first dial fails; a second save that changes only another server queues no job for it, and a save that changes its own config does); `TestLiveMCPRetryFailedRedialsEveryFailedServer` (an unchanged request with `retryFailed` re-dials a failed new server and a changed server whose old session was kept, never the healthy one; without the flag it queues nothing); `TestLiveMCPReconnectRefusedWhileBusyTouchesNothing` (the spy's `swapErr`: no entry retired or closed, no dial); `TestLiveMCPRevertToTheLiveConfigDialsNothing` (a revert while a dial is in flight or pending cancels or discards it and queues no job); `TestLiveMCPRemovalRefusesItsToolsAtOnce` (retired before `request` returns, no swap); `TestLiveMCPNewerRequestCancelsTheInFlightDial` (the stale result is closed and yields no outcome); `TestLiveMCPCommitIsOneSwapInConfigOrder`; `TestLiveMCPCommitReadmitsUnderTheLiveLists` (a denied result is closed and noted `mcp server <name> disconnected — its endpoint is denied`); `TestLiveMCPRefusedSwapLeavesResultsPending` (commit returns the still-pending error, writes no note, and a later commit lands the results); `TestLiveMCPCloseCancelsAndJoinsEverything` (a gated dial seen cancelled through the fake connect's ctx, and a slow close, are both waited for); `TestLiveMCPJobNeverRunAfterCloseIsInert`; `TestApplySettingMCPReconnectSwapsTheToolsAndClosesTheOldSessions` passes under `-race` (the sync reconnect joined its closes); every `closed` read in a test reached after `request` goes through `isClosed()` (`grep -n '\.closed\b' cmd/apogee/*_test.go` finds only the accessor).
**Acceptance:**
- `go build ./...`; `go vet ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestLiveMCP|TestMCPReconnect|TestApplySettingMCP|TestWatched|TestApplySettingURLSafety' ./cmd/apogee/`
**Depends on:** item 3
**Commit:** `feat(mcp): request, dial and commit MCP servers one by one`

## 5. TUI: MCPHost seam, dial Cmds and the idle-point commit — ✅ DONE (2026-11-07)
NOTES (2026-11-07): the commit is not gated on `inFlight == 0`: each finished dial's done Msg sets `uncommitted`, and the next fold that finds `canSwapTools` true commits whatever has finished, so a slow server never holds back a healthy one's commit (the header's per-server lifecycle).
NOTES (2026-11-07): `TakeMCPWork`'s `commit` flag sets `uncommitted` directly (removal-only work). A started dial sets it only when its done Msg folds back.
NOTES (2026-11-07): `foldMCPDialDone` is inert when `Options.MCP` is nil, so the nil host leaves `mcpState` zero.
NOTES (2026-11-07): `commitMCP` writes every non-empty `Note` even on a pending answer and sets `uncommitted = pending`. Item 4's pending commit returns no outcomes, so a pending commit writes no note.
NOTES (2026-11-07): consequential edit — internal/tui/doc.go: made necessary by the two new engineHolds questions (the engineholds.go map line lists every named question) and the new mcpreconnect.go file (map entry, with the tail hook's place between wakeAfterFold and busyAfterFold).
NOTES (2026-11-07): the `engineholds_test.go` table has no watching or git-action row, so `canSwapTools`' holdWatching and holdGitAction terms are covered by `TestMCPDialStartsWhileWatching` (watching) and not by the table. The `fakeMCPHost.CancelMCPDials` is a no-op until item 6's quit tests need it to record calls.

**What:**
**Goal:**
- **Seam:** `internal/tui` has a nil-safe `Options.MCP MCPHost` holding `TakeMCPWork() (dials []MCPDial, commit bool)`, `CommitMCP() (outcomes []MCPOutcome, pending bool)` and `CancelMCPDials()`, with `type MCPDial func(ctx context.Context)`, `MCPOutcome{Note, RowFailure string}` and the exported constant `MCPReconnectingNote = "reconnecting MCP…"`.
- **Every fold:** while dials may start, the Model takes the work, runs each dial as a Cmd with `m.parent` (its done Msg counting the dial in) and marks a commit wanted.
- **Commit:** while the engine is free for a swap, it commits and writes each non-empty `Note` verbatim with `addEphemeralNote`: no label added, no blank note.
**Approach (assumed at the header base):**
- **State:** a value-safe `mcpState{inFlight int; uncommitted bool}` on Model, in a new `internal/tui/mcpreconnect.go` that holds the hook, the Cmd, `mcpDialDoneMsg` and its fold.
- **Hook:** `mcpAfterFold` runs in Update's deferred chain after `wakeAfterFold` and before `busyAfterFold`.
- **Gates:** two named engineHolds questions in `engineholds.go`:
  - `canStartMCPDials` is no `holdQuitting`;
  - `canSwapTools` is no `holdsTakingAgent|holdSessionLoad|holdRecordWrite|holdQuitting|holdWatching|holdPrebound`.
- **Why the tail:** the commit is asked at every fold's tail, which covers every `releaseEngine` (each runs inside a fold) as well as the releases that never call it. `releaseEngine` stays as it is.
- **Routing:** `mcpDialDoneMsg` routes `watchRouteOwner` in `watchRouteOf`. The fold decrements `inFlight` and sets `uncommitted`.
- **Doc map:** `doc.go` gets a file-map line for `mcpreconnect.go` and the tail hook in its Update narration.
**Regression guard.**
- Decision: on item 4's "still pending" outcome the TUI keeps the results and retries the commit at the next idle point.
- `CommitMCP` returns `(outcomes, pending bool)`, and `uncommitted` stays set exactly while results wait; `canSwapTools` includes `holdPrebound`, so a pre-bound session never commits into `errNoServerBound` on every fold.
- `canStartMCPDials` is no `holdQuitting` only: a watched save folded into the parked session (`configChangedMsg` → `watchRouteOwner`) starts its dial at once, per ADR 0109 decision 3 and the header's Timing call; `holdWatching` stays on `canSwapTools`, because `foldForOwner` swaps in an inert engine.
- The two new questions are added to `engineholds_test.go`'s `engineAnswers` table.
**Files:** internal/tui/tui.go; internal/tui/mcpreconnect.go; internal/tui/model.go; internal/tui/engineholds.go; internal/tui/engineholds_test.go; internal/tui/watch.go; internal/tui/doc.go; internal/tui/mcpreconnect_test.go
**Read first:** internal/tui/model.go — Model.Update (deferred chain: adoptAfterFold, offerAfterFold, wakeAfterFold, busyAfterFold), Model.fold; internal/tui/engineholds.go — engineHoldSources, canRunDeferred, releaseEngine; internal/tui/watch.go — watchRouteOf, foldWhileWatching, foldForOwner;
internal/tui/transcript.go — transcript.addEphemeralNote; internal/tui/engineholds_test.go — engineAnswers table; internal/tui/watch_test.go — TestWatchIsolatesRoutesEveryFoldedMsg; internal/tui/gitactions_test.go — TestGitActionPromptWaitsMidAction; cmd/apogee/wire_engine.go — lateEngine.SwapTools
**Tests:** a fake MCPHost whose dial blocks on a channel. `TestMCPDialKeysStillHandledWhileDialling` (modelled on `TestGitActionPromptWaitsMidAction`); `TestMCPCommitWaitsForTheWorker` (no CommitMCP until the worker's done Msg); `TestMCPCommitWritesEachOutcomeAsAnEphemeralNote` (pins `entry.ephemeral`, an empty Note writes nothing); `TestMCPPendingCommitRetriesAtTheNextIdlePoint` (a `pending` answer keeps `uncommitted` set and writes no note; the next idle fold commits again); `TestMCPCommitWaitsWhilePrebound`; `TestMCPDialStartsWhileWatching` (a watched save folded into the parked session starts its dial; no commit until the watch ends); `TestMCPRemovalOnlyWorkCommitsWithNoDial`; `TestMCPNilHostIsInert`; the `engineholds_test.go` answers table covers both new questions.
**Acceptance:**
- `go build ./...`; `go vet ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestMCP|TestEngineHolds_Questions|TestWatchIsolatesRoutesEveryFoldedMsg|TestDocMapNamesEveryFile|TestModelNoBuilderByValue|TestModelFitsTheStack' ./internal/tui/`
**Depends on:** item 4
**Commit:** `feat(tui): dial MCP servers on Cmds and commit them at the idle point`

## 6. TUI: busy word and quit for in-flight dials — ✅ DONE (2026-11-07)
NOTES (2026-11-07): consequential edit — internal/tui/doc.go: made necessary by the new busy wait (busy.go narration lists every wait) and the quit latch (mcpreconnect.go map entry now names `Model.stopMCP`).
NOTES (2026-11-07): `Model.stopMCP` (mcpreconnect.go) is called once at the head of `quitSession`, after the workflow-prompt dismissal and ahead of all three exits, so a single call covers every exit; it calls `CancelMCPDials` and sets `mcpState.stopped`, which `mcpAfterFold` checks ahead of both gates.
NOTES (2026-11-07): the busy word follows the goal literally (`inFlight > 0`); it is not suppressed after a quit, so a cancelled dial still folding back during a deferred quit may show `reconnecting MCP…` until its done Msg lands.
NOTES (2026-11-07): `fakeMCPHost.CancelMCPDials` now counts calls (`cancelCount`), and `queuedCount` reports untaken dials; `TestBusyMCPRanksBelowConnecting` also pins the rank above a pop-up's loading row.

**What:**
**Goal:** while `inFlight > 0` the idle status line shows the busy indicator with `MCPReconnectingNote`, ranked below `connecting…` and above a pop-up's loading row. A quit calls `CancelMCPDials` on every exit of `quitSession`; after it no dial starts and nothing commits.
**Approach (assumed at the header base):**
- **Busy word:** in `busyWait` (`internal/tui/busy.go`), a source line after the `connectingLabel` arm and before the pop-up loading arm; `busyWait`'s ranked list in busy.go's doc comments follows. Because item 5's hook runs ahead of `busyAfterFold`, the fold that starts a dial arms the spinner chain.
- **Quit:** `Model.quitSession` is in `model.go`; the gates from item 5 already stop starts and commits once quitting. Teardown's wait is `liveMCP.close` (item 4), which `rootWiring.close` already reaches after `Program.Run` returns, with no fixed deadline.
**Regression guard.**
- `quitSession` has three exits; the idle, nothing-pending one (`return m, tea.Quit`) never sets `m.quitting`, so the `holdQuitting` gates do not cover it. A dedicated `stopped` latch in `mcpState` is set wherever `CancelMCPDials` is called, and `mcpAfterFold` checks it ahead of the gates. The immediate exit does not set `m.quitting`.
**Files:** internal/tui/busy.go; internal/tui/model.go; internal/tui/mcpreconnect.go; internal/tui/busy_test.go; internal/tui/mcpreconnect_test.go
**Read first:** internal/tui/model.go — Model.quitSession, Model.statusLeft; internal/tui/busy.go — busyWait, restampBusy, busyAfterFold; internal/tui/spinner.go — Model.spinning; internal/tui/busy_test.go — TestBusyGitAction, busyLeft, plainGlyph;
internal/tui/doc.go — busy.go narration; cmd/apogee/wire.go — rootWiring.close
**Tests:** `TestBusyMCPReconnect` (modelled on `TestBusyGitAction`, asserting the rendered `reconnecting MCP…`); `TestBusyMCPRanksBelowConnecting`; `TestMCPQuitCancelsDialsAndCommitsNothing` (on each of `quitSession`'s exits, the idle immediate one included: the fake records CancelMCPDials; a done Msg after the quit makes no CommitMCP; a later fold takes no work).
**Acceptance:**
- `go build ./...`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestBusy|TestMCP' ./internal/tui/`
**Depends on:** item 5
**Commit:** `feat(tui): MCP dials show the busy indicator and yield to quit`

## 7. Route the `mcp-servers` row and the watched save through the async path — ✅ DONE (2026-11-07)
NOTES (2026-11-07): the retry-failed flag crosses the `Settings.Apply` seam as a new `watched bool` parameter on `tui.SettingsHost.Apply` (the TUI passes `settingEdit.watched`). `settingsHost.Apply` sets the new `settingsApplier.retryFailedMCP` to `!watched`, and the `mcp-servers` table entry calls `requestMCP(a.retryFailedMCP)`.
NOTES (2026-11-07): `MCPHost.RetryFailedMCP` returns `(note string, err error)`, not the plan's `(note string)`: a refused request (an unreadable layered file, a set `mcp.ValidateServers` refuses) is shown as the row's failure instead of being dropped silently.
NOTES (2026-11-07): the unchanged round trip's `reconnecting MCP…` goes into the pane's answer slot (`settingAnswer`), not the journal. That round trip changed no key, so the row gets no ` *`. The settle clears the note from the answer slot and from the journaled edit alike. The `settingAnswer` doc now names this as a third act of that shape.
NOTES (2026-11-07): a refused commit (`CommitMCP` answering pending) returns one outcome whose RowFailure is `mcpReconnectFailed(refusal)` and whose Note is empty, so the row resolves on the refusal. The `MCPHost.CommitMCP` doc is updated to match. The TUI settles the row when nothing is in flight and either nothing is uncommitted or the commit was refused.
NOTES (2026-11-07): renamed `TestE2EWatchedMCPSaveMidTurnDialsNothing` to `TestE2EWatchedMCPSaveMidTurnSwapsOnlyAtIdle`, because the old name is now false. The re-pinned test checks that the repointed server is dialled mid-Turn, that no `mcp docs reconnected` note appears while the reply is held, and that the note appears after idle. "The old tools still serve" is checked only through that missing note, never by calling a tool.
NOTES (2026-11-07): `settingsApplier.reconnectMCP` is deleted (no caller left). `reconnectMCPFrom` stays synchronous for `applyMovedKinds` (item 8). liveMCP gains `wantsRebuild`, `isDialQueued`, `hasFailedDial` and `cancelDials`, and `close` now starts with `cancelDials`. The rootWiring `tui.MCPHost` methods and `mcpHost()` (nil without an MCP holder or tool set) live in wire_mcp.go.
NOTES (2026-11-07): `TestApplySettingMCPReconnectKeepsEverythingWhenTheEngineIsBusy` now seeds a live `docs` server on a stale config instead of the `seed` server. A removed server is closed at the request itself (ADR 0109), so it could never show an old session kept open through a refused swap.
NOTES (2026-11-07): consequential edit — internal/tui/settings_test.go: made necessary by the `SettingsHost.Apply` signature change (the fake also gains an optional `applyWatched` that sees the flag).
NOTES (2026-11-07): consequential edit — cmd/apogee/settingsrows_test.go: made necessary by the `settingsHost.apply` func type change.
NOTES (2026-11-07): consequential edit — cmd/apogee/upstream_test.go: made necessary by the `SettingsHost.Apply` signature change.
NOTES (2026-11-07): consequential edit — internal/tui/doc.go: made necessary by the row watch in mcpreconnect.go (the file-map entry now names settleMCPRow).

**What:** Recast at the regression check (2026-11-07).
**Goal:**
- **No dial on the caller:** applying `mcp-servers`, from a pane edit or a watched save, records a request through `settingsApplier.requestMCP` and dials nothing on its caller. The row's note is `tui.MCPReconnectingNote` when the request started a dial, and "" otherwise.
- **Seam:** `rootWiring` implements `tui.MCPHost` over `w.mcpSet`, `w.toolSet` and `w.engine`, and `rootWiring.options` wires it.
- **Row outcome:** the row note arms a row watch, and the outcome replaces the note once no dial is in flight and nothing is uncommitted. The journaled note always clears, so on success the row's note cell is empty (value + ` *`): that is the header's `saved`. If any RowFailure was collected and the settings surface is still open, the row's failure is `settingsApplyFailedNote + strings.Join(failures, "; ")`.
**Approach (assumed at the header base):**
- **Request:** `requestMCP` and `requestMCPFrom(layered)` in `wire_settings.go` widen the secret env on the loop as today, build the guard from `a.tools.built()`, and call `a.mcp.request`. The pending jobs and `rebuild` flag wait on liveMCP for `TakeMCPWork`.
- **Kept for now:** `reconnectMCPFrom` stays synchronous for `applyMovedKinds` until item 8.
- **TUI side:** `settingsApplied` (`settingsapply.go`) arms `mcpState.row` for the `mcp-servers` path when the note equals `MCPReconnectingNote`, and item 5's hook resolves it.
- **Unchanged round trip:** `foldSettingsEdit` (`internal/tui/settingswatcher.go`), when `msg.path` is `mcp-servers` and `reload.Applied` carries no `mcp-servers` entry (`externalEdit.changed`, `settingsedit.go`, reports none), calls a new `MCPHost.RetryFailedMCP() (note string)`. `rootWiring` answers it through `settingsApplier.requestMCP` with the retry-failed flag set, and only when some server's last dial failed; otherwise it requests nothing and returns "". A `MCPReconnectingNote` answer arms `mcpState.row` as `settingsApplied` does. A changed round trip reaches `requestMCP` through `settingsApplied` with the flag set too (`settingEdit.watched` false).
- **Comments:** rule: every comment that says the `mcp-servers` apply dials or reports on the row is rewritten. Find them with `grep -n 'dialled\|row.s note\|reconnect' cmd/apogee/wire_settings.go cmd/apogee/wire_mcp.go`.
**Regression guard.**
- `requestMCP` has no `CanSwapTools` pre-check (ADR 0109 decision 3), so item 1's `TestE2EWatchedMCPSaveMidTurnDialsNothing` is re-pinned: a mid-Turn save dials, nothing is swapped until idle, and the old tools still serve; `TestApplySettingMCPReconnectKeepsEverythingWhenTheEngineIsBusy` is re-pinned to dialled, result pending, old session open.
- `TestSettingsApplierReloadsRefuseAnUnparseableFile`'s `reconnectMCP` row is re-pointed to `requestMCP`, which returns `layeredConfig`'s error unwrapped.
- `rootWiring.CommitMCP` asks `w.engine.CanSwapTools()` (item 1) before any rebuild. On a refusal it rebuilds nothing and answers `pending` (the results stay, per items 4–5); the row resolves once no dial is in flight, with the refusal as its RowFailure.
- Comment rule widened to the in-progress note's contracts: `grep -rn 'boundary sentence\|stays true\|second dial' internal/tui/tui.go internal/tui/settings.go cmd/apogee/wire_options.go cmd/apogee/settingsedit.go`, plus `settingsedit.go`'s `a second` / `dial of every server`, which a line break splits.
- Decision: the `mcp-servers` row commit passes the retry-failed flag; add a test that a watched save with unchanged config does not re-dial a failed server.
- Both triggers reach `Settings.Apply(path, value)` through `settingsApplied` (`settingsapply.go`); only `settingEdit.watched` tells the row commit (`foldSettingsEdit`, false) from the watched save (`foldConfigChanged`, true), so the flag crosses that seam.
- Decision (owner, 2026-11-07): the retry gesture is the `$EDITOR` round trip on the `mcp-servers` row: a round trip that leaves `mcp-servers:` unchanged (today `externalEdit.changed` yields no AppliedSetting, cmd/apogee/settingsedit.go) still requests a retry with the retry-failed flag when any server's last dial failed — row shows `reconnecting MCP…`, then the outcome — and stays a no-op exactly as today when nothing failed; a changed round trip passes the flag too.
**Files:** cmd/apogee/wire_settings.go; cmd/apogee/wire_options.go; cmd/apogee/wire_mcp.go; cmd/apogee/settingsedit.go; cmd/apogee/wire_settings_test.go; cmd/apogee/configwatch_apply_test.go; cmd/apogee/e2e_mcpmidturn_test.go; internal/tui/tui.go; internal/tui/settings.go; internal/tui/settingsapply.go; internal/tui/settingswatcher.go; internal/tui/mcpreconnect.go; internal/tui/mcpreconnect_test.go
**Read first:** internal/tui/settingsapply.go — settingsApplied, settingsApplyLive, recordSettingEdit, settingsApplyFailedNote; internal/tui/settingswatcher.go — applyReloaded, foldConfigChanged, foldSettingsEdit; internal/tui/settings.go — settingsNote, settingEdit, settingFailure;
cmd/apogee/wire_settings.go — settingsTable "mcp-servers" entry, reconnectMCP, reconnectMCPFrom, widenSecretEnv; cmd/apogee/wire_options.go — rootWiring.options, settingsApplier; cmd/apogee/wire_settings_test.go — TestApplySettingMCPReconnectSwapsTheToolsAndClosesTheOldSessions, TestRunRootWiresTheLiveApplySeam, TestSettingsApplierReloadsRefuseAnUnparseableFile;
cmd/apogee/configwatch_apply_test.go — TestWatchedBrokenProjectConfigDropsAdoptedServerUntilItParses, reloadAndApply, layeredApplier
**Tests:**
- `TestApplySettingMCPServersRecordsADialWithoutDialling` (a gated connect is never entered during Apply; the note is `reconnecting MCP…`).
- `TestApplySettingMCPServersRemovalOnlyNotesNothing`.
- `TestApplySettingMCPServersRowCommitRetriesTheFailedServer` (after a failed dial, a changed round trip on the row passes the flag: it re-dials the failed server once besides what it changed, and an untouched healthy server not at all).
- `TestMCPUnchangedRoundTripRedialsOnlyTheFailedServer` (cmd/apogee: after a failed dial, an unchanged `$EDITOR` round trip on the `mcp-servers` row — `RetryFailedMCP` — answers `reconnecting MCP…`, and `drainMCP` sees exactly one dial, of the failed server).
- `TestMCPUnchangedRoundTripWithNothingFailedDialsNothing` (cmd/apogee: with no failed dial, `RetryFailedMCP` answers "" and queues no work).
- `TestMCPRowUnchangedRoundTrip` (tui: `foldSettingsEdit` on `mcp-servers` with no `mcp-servers` entry in `reload.Applied` calls `RetryFailedMCP`; a `reconnecting MCP…` answer arms the row and resolves to the outcome, a "" answer writes no row annotation).
- `TestApplySettingMCPServersWatchedSaveNeverRedialsAFailedServer` (a watched save with unchanged config queues no dial of the failed server).
- `TestApplySettingMCP*`, `TestWatchedConfigRepointedMCPServerReconnects`, `TestWatchedProjectConfigAdoptedServerConnects`, `TestWatchedBrokenProjectConfigDropsAdoptedServerUntilItParses` and `TestMCPReconnectRebuildsWithTheEndpointTheSessionIsOn` drive `drainMCP` after each apply.
- `TestRunRootWiresTheLiveApplySeam` asserts a non-nil `Options.MCP`.
- Re-pinned: `TestE2EWatchedMCPSaveMidTurnDialsNothing` (dials, swaps nothing until idle, old tools serve), `TestApplySettingMCPReconnectKeepsEverythingWhenTheEngineIsBusy` (dialled, result pending, old session open), `TestSettingsApplierReloadsRefuseAnUnparseableFile` (the `requestMCP` row).
- `TestMCPCommitRefusedByAnUnboundEngineRebuildsNothing` (CommitMCP answers `pending` with no rebuild; the row resolves with the refusal as its failure).
- `TestMCPRowShowsTheOutcomeWhenTheDialsLand` (tui; on success the note cell is empty (value + ` *`), and the joined failure, the failure only while the surface is open).
**Acceptance:**
- `go build ./...`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestApplySettingMCP|TestWatched|TestMCPReconnectRebuilds|TestRunRootWiresTheLiveApplySeam|TestE2EWatchedMCPSaveMidTurn|TestSettingsApplierReloadsRefuseAnUnparseableFile|TestMCPCommitRefused|TestMCPUnchangedRoundTrip' ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestMCPRow' ./internal/tui/`
**Depends on:** item 6
**Commit:** `fix(settings): an mcp-servers save dials off the Update loop`

## 8. Adoption and rejection request per-server dials — ✅ DONE (2026-11-07)
NOTES (2026-11-07): re-derived from the assumption that a dial job taken with `TakeMCPWork` before `RejectRules` and run after it still connects: `liveMCP.startDial` refuses a stale-generation job before it reaches the connect seam, so `TestProjectRuleRejectSupersedesAnInFlightAdoptionDial` runs the taken dial on a goroutine, holds it inside the connect seam, rejects (asserting the dial's context is cancelled), then releases it and calls `CommitMCP` — the returned session is closed, and no wiki tool or outcome lands.
NOTES (2026-11-07): consequential edit — cmd/apogee/wire_mcp.go: made necessary by deleting `reconnectMCPFrom` (the `mcpRowFailures` doc named it as its caller; it now names `liveMCP.reconnect`).
NOTES (2026-11-07): `answerProjectRules`' doc in cmd/apogee/projectrules.go reworded too (its installSnapshot error is now a refused server-set request, not a server the session could not move onto) — the grep the item names found it.

**What:**
**Goal:** `applyMovedKinds` puts a moved MCP set into force through `requestMCPFrom`. It never turns an MCP outcome or note into an `AdoptRules` / `RejectRules` error, the answer returns at once, and a moved-pin skip reaches the transcript as that server's note. `reconnectMCPFrom` is gone. The reactions reload in the same answer stays synchronous, in its current order.
**Approach (assumed at the header base):**
- **Errors:** `applyMovedKinds` (`cmd/apogee/projectrules.go`) joins only `requestMCPFrom`'s validation refusal and the reload's error into its error.
- **Superseded call:** this supersedes the call that a skip rides the AdoptRules error (`docs/plans/archived/2026-10-18 - 02 - exec-pin-followups-plan.md` NOTES), per ADR 0109 D8.
- **Comments:** rule: every comment that names the reconnect's error or note as the answer's report is rewritten. Find them with `grep -n 'reconnect\|re-dialled\|could not move onto\|one surface' cmd/apogee/projectrules.go` and the `ConfigHost.AdoptRules` doc in `internal/tui/tui.go` ("a server dialled" becomes a dial started).
**Regression guard.**
- Decision: Depends on item 7 (shares wire_settings.go / wire_mcp.go).
- `TestProjectRuleRejectSupersedesAnInFlightAdoptionDial` cannot observe the close through `drainMCP` (it takes, runs and commits in one call): it takes the adoption's work with `w.TakeMCPWork()` before `RejectRules`, runs the dial after it, then calls `w.CommitMCP()`, and asserts that the fake session the dial returned is closed and that no wiki tool or outcome lands.
- In `TestProjectRuleAdoptsARepoServerTheFileHolds`, `drainMCP` runs before the "a refused adoption dialled" check as well as after the good adoption, so the check cannot pass vacuously.
**Files:** cmd/apogee/projectrules.go; cmd/apogee/wire_settings.go; cmd/apogee/projectrules_test.go; internal/tui/tui.go
**Read first:** cmd/apogee/projectrules.go — applyMovedKinds, installSnapshot, answerProjectRules, settleAllowRules; cmd/apogee/wire_settings.go — reconnectMCPFrom, reloadReactionsFrom, widenSecretEnv; internal/tui/tui.go — ConfigHost.AdoptRules, ConfigHost.RejectRules;
internal/tui/adoption.go — answerProposalNote call site; cmd/apogee/projectrules_test.go — TestProjectRuleAdoptsARepoServerTheFileHolds, TestProjectRuleRejectingAnAdoptedServerWithNoMCPSetReachesNoDoor, projectRuleWiring
**Tests:** `TestProjectRuleAdoptionRequestsADialWithoutDialling` (AdoptRules returns nil with a gated connect never entered); `TestProjectRuleRejectSupersedesAnInFlightAdoptionDial` (work taken with `TakeMCPWork` before `RejectRules`, dialled after it, then `CommitMCP`: the returned fake session is closed and no wiki tool or outcome lands); `TestProjectRuleAdoptsARepoServerTheFileHolds` drives `drainMCP` before the refused-adoption check and after the good adoption; `TestProjectRuleCLIAdoptionAppliesLiveWithoutARestart` and `TestProjectRuleRejectingAnAdoptedServerWithNoMCPSetReachesNoDoor` are unchanged.
**Acceptance:**
- `go build ./...`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestProjectRule' ./cmd/apogee/`
**Depends on:** item 7
**Commit:** `feat(projectrules): an adoption's MCP dials run off the Update loop`

## 9. url-safety re-admit is per server

**What:** Recast at the regression check (2026-11-07).
**Goal:**
- **Deny:** an edit to a `url-safety` host list that denies a live server marks it closed on the keystroke and cancels its dial, mid-Turn included, before and regardless of the idle-only host-list `SwapTools`. The row keeps `toolRosterNote + "; mcp server <name> disconnected — " + deniedReason(d)`.
- **Admit:** a newly admitted server is dialled through the async path, and its outcome arrives as its note.
- **Unchanged:** an edit that leaves the admitted set unchanged records nothing.
- **Clean-up:** `liveMCP.reconnect` has no production caller left and is deleted. `docs/manual/configuration.md`'s "disconnected there and then, and the row names it" stays true.
**Approach (assumed at the header base):**
- **Request:** `settingsApplier.readmitMCP` (`wire_settings.go`) keeps the `sameServerNames` check and calls `a.mcp.request(admitted, guard)`. It names the denied servers that were in `was`, and its unparseable-file clause is unchanged.
- **Deny first:** in `applyURLSafetyHosts`, the MCP deny half (retire and cancel the newly denied servers) runs before the host-list `move`, so a `move` refused mid-Turn no longer skips it; the rest of the apply reports as today.
- **Tests:** the direct `w.mcpSet.reconnect` callers in tests (`mcp_cmd_test.go`, `wire_live_test.go`) call `request` + `drainMCP`.
- **Comments:** rule: every comment that names the re-admit's dial, its note or its timing is rewritten. Find them with `grep -n 'readmit\|re-admi\|there and then\|note rides\|synchronously\|row relays' cmd/apogee/wire_settings.go cmd/apogee/wire_mcp.go cmd/apogee/wire_live.go`.
**Regression guard.**
- Decision: Depends on item 8 (shares wire_settings.go / wire_mcp.go); owner call 2026-11-07: a url-safety edit that newly denies an MCP server marks that server closed and cancels its in-flight dial on the keystroke, mid-Turn included, before and regardless of the idle-only host-list SwapTools; the rest of the url-safety apply reports as today; add to the header's "Ratified design calls": `**Mid-Turn url-safety deny:** the MCP deny half runs first, mid-Turn included, before and regardless of the idle-only host-list swap (owner, 2026-11-07).`; add a test driving a deny-hosts watched save mid-Turn that asserts the MCP server's tools are refused before the Turn ends.
- Rule: every test calling `.reconnect(` (`grep -n '\.reconnect(' cmd/apogee/*_test.go`, `wire_mcp_test.go` included) moves to `request` + `drainMCP`; `TestStartupMCPLoginReconnectNeverPrompts` is re-pinned to the drained note `mcp connect failed: remote: …apogee mcp login remote…`.
- `TestApplySettingURLSafetyRowCarriesTheMCPLabelOnce` dials through the real `mcp.ConnectWith` under `context.Background()`: it keeps the real refusal on `broken` and pins `mcp connect failed: broken: server "broken" has no transport configured`; docs goes through a fake connect failing with a fixed error, or stays out of the newly admitted set.
- Decision: uses item 4's last-requested diff, so a url-safety edit re-dials only servers whose own admit verdict moved.
- Split around `move`: the deny half runs before it, partitioning with `before` and the landed key's list swapped in, and retires only the newly denied servers through `request`'s removal path (cancel, bump generation); `request(admitted, guard)` runs only after a successful `move`, its guard from the moved `set.built()`, so a widening edit never dials under the old lists.
- A refused `move` (mid-Turn) returns an error that carries the deny clause (`…; mcp server <name> disconnected — its endpoint is denied`), so `docs/manual/configuration.md`'s "disconnected there and then, and the row names it" and "a commit made mid-turn is reported on the row" both stay true; a unit test pins it.
- `TestMCPReconnectRefusesBeforeDiallingWhileAnExchangeRuns` (item 1) is deleted with `reconnect`, since `request` dials mid-Turn by design (ADR 0109 D3) and `TestLiveMCPRefusedSwapLeavesResultsPending` covers a busy commit; it leaves the `-run` regex.
- Fixtures: the deny and failed-reconnect tests are re-seeded with a live `docs` entry carrying `mcpServersFixture`'s resolved config; `TestSettingsApplierReloadsRefuseAnUnparseableFile` keeps pinning `mcpNoteFor(mcpReconnectFailed(loadErr))` through the new `readmitMCP` signature and joins the `-run` regex; the driven test answers the MCP approval pane (`awaitArmed` + the allow key, in-process driver) before reading `mcp server "<name>" is not connected` from `stub.Requests()`.
**Files:** cmd/apogee/wire_settings.go; cmd/apogee/wire_mcp.go; cmd/apogee/wire_live.go; cmd/apogee/wire_live_test.go; cmd/apogee/wire_settings_test.go; cmd/apogee/mcp_cmd_test.go; cmd/apogee/wire_mcp_test.go; cmd/apogee/e2e_mcpmidturn_test.go
**Read first:** cmd/apogee/wire_settings.go — applyURLSafetyHosts, readmitMCP, deniedReason, mcpNoteFor, sameServerNames; cmd/apogee/wire_tools.go — liveTools.setDenyHosts, liveTools.setAllowHosts, liveTools.built;
cmd/apogee/wire_live_test.go — TestApplySettingURLSafetyHostsDropsAnMCPServerTheNewListDenies, TestApplySettingURLSafetyHostsReportsAFailedReconnectInTheNote, TestMCPReconnectUsesTheLiveURLSafetyLists; cmd/apogee/wire_settings_test.go — TestApplySettingURLSafetyRowCarriesTheMCPLabelOnce, TestSettingsApplierReloadsRefuseAnUnparseableFile;
cmd/apogee/mcp_cmd_test.go — newPingMCPServer, TestSessionResolvesReactionMCPThroughTheLiveSet; internal/tui/settingswatcher.go — foldConfigChanged, configWatchAppliedNote
**Tests:**
- `TestApplySettingURLSafetyHostsDropsAnMCPServerTheNewListDenies` (seeded with a live `docs` entry): `docs` retired as denied before Apply returns, the row clause kept, closed after `drainMCP` (`isClosed()`).
- `TestApplySettingURLSafetyDenyRefusedMidTurnNamesTheServer`: a busy engine refuses the `move`; the server is still retired, and the returned error carries `mcp server docs disconnected — its endpoint is denied`.
- `TestApplySettingURLSafetyWideningDialsUnderTheNewLists`: a re-admitted server's dial job runs under the moved lists and connects.
- `…LeavesMCPAloneWhenNoVerdictMoved`: no request.
- `…ReportsAFailedReconnectInTheNote`, re-fixtured as a widening (docs starts denied and the edit lifts it, with the fixture's connect failing docs). It pins the drained note `mcp connect failed: docs: <fixture error>`, composed exactly.
- `TestMCPReconnectUsesTheLiveURLSafetyLists`, `TestReconnectSkipsARepoServerWhosePinMoved`.
- `TestApplySettingURLSafetyRowCarriesTheMCPLabelOnce` pins `mcp connect failed: broken: server "broken" has no transport configured` (the real refusal); docs never makes a real dial.
- Every `.reconnect(` caller re-pinned to `request` + `drainMCP`, `TestStartupMCPLoginReconnectNeverPrompts` to `mcp connect failed: remote: …apogee mcp login remote…`.
- `TestSettingsApplierReloadsRefuseAnUnparseableFile` keeps its `mcpNoteFor(mcpReconnectFailed(loadErr))` pin; `TestMCPReconnectRefusesBeforeDiallingWhileAnExchangeRuns` is deleted.
- Driven in-process `TestE2EWatchedURLSafetyDenyMidTurnRefusesTheMCPTools` (`e2e_mcpmidturn_test.go`): a `deny-hosts` watched save lands while a Turn is held at a stubllm gate; the test answers the MCP approval pane, and the MCP server's tools are refused (`mcp server "<name>" is not connected`) before the Turn ends.
**Acceptance:**
- `go build ./...`; `go vet ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestApplySettingURLSafety|TestMCPReconnectUsesTheLiveURLSafetyLists|TestReconnectSkips|TestSessionResolvesReactionMCP|TestStartupMCPLoginReconnect|TestLiveMCP|TestSettingsApplierReloadsRefuseAnUnparseableFile|TestE2EWatchedURLSafetyDenyMidTurn' ./cmd/apogee/`
**Depends on:** item 8
**Commit:** `feat(settings): a url-safety re-admit dials MCP servers one by one`

## 10. Driven end-to-end: a slow MCP dial keeps the TUI live

**What:**
**Goal:** two driven `cmd/apogee` tests over a real streamable-HTTP MCP server:
- **Slow dial:** a watched save adds a server whose handler is gated. Before release the status line carries `reconnecting MCP…` and a typed key is echoed in the prompt. After release the transcript carries `mcp slow connected`, the busy word is gone, and a model turn calling the new server's tool gets `pong`.
- **Broken plus healthy:** a save adds a broken server and a healthy one. The transcript carries a note opening `mcp connect failed: broken: ` and `mcp healthy connected`, and the healthy server's tool answers.
**Approach (assumed at the header base):**
- **Session:** a driven session dials through `wireSession`'s real `mcp.ConnectWith` closure (rootDeps has no connect seam). It launches with no MCP servers and adds them with `appendHomeConfig`.
- **Gate:** the gated handler is the `newPingMCPServer` shape and selects on the gate and `r.Context().Done()`. The gate closes through one `release()` over a `sync.Once`, which the test calls mid-test and a `t.Cleanup` registered after the server's also calls, so LIFO runs it first and a second close never panics.
- **Broken server:** an `httptest` server answering 500.
- **Model turn:** scripted inline as a `stubllm.Script{…}` literal, as `confinement_e2e_test.go` does. Waits go through `testtime.Eventually`/`drv.WaitText`.
**Regression guard.**
- MCP tools always ask for approval, auto mode included (`internal/config/defaults/config.yaml`, the `mcp-servers` block), so each model turn that calls `slow__ping` / `healthy__ping` answers the pane (`awaitArmed` + the allow key, the shape of `allowAndAwait` in `e2e_egress_test.go`, adapted from `*ptySession` to the in-process driver) before the `pong` assertion.
- The gate closes through one `sync.Once` (a `release()` the test and the cleanup both call); a second close of the channel would panic in every passing run.
**Files:** cmd/apogee/e2e_mcpreconnect_test.go
**Read first:** cmd/apogee/mcp_cmd_test.go — newPingMCPServer; cmd/apogee/e2e_support_test.go — launchTUI, appendHomeConfig, e2eGuards; cmd/apogee/e2e_egress_test.go — TestE2EEgress (watched `mcp-servers:` save, renameHomeConfig, awaitRefusedCalls, allowAndAwait);
cmd/apogee/e2e_approval_test.go — awaitArmed, waitIdle, promptPlaceholder; cmd/apogee/confinement_e2e_test.go — scriptedTerminalModel; cmd/apogee/wire_live.go — wireSession (newLiveMCP closure over mcp.ConnectWith); internal/tui/busy.go — busyWait
**Tests:** `TestE2EMCPSlowDialKeepsTheTUILive`, `TestE2EMCPBrokenServerSparesTheHealthyOne` (each answers the MCP approval pane before asserting `pong`).
**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestE2EMCP' ./cmd/apogee/`
**Depends on:** item 8, item 9
**Commit:** `test(e2e): a slow or broken MCP server leaves the TUI and the healthy servers working`

## 11. Docs: per-server MCP reconnect

**What:**
**Goal:** every doc and comment that describes MCP live reconnect states the per-server, off-loop, idle-commit, per-server-note behaviour, and every list of busy-indicator waits includes `reconnecting MCP…`.
**Approach (assumed at the header base):**
- **Reconnect rule:** every site that names the MCP reconnect as whole-set, synchronous, row-reported or "there and then" for a dial. Find them with `grep -rni "reconnect\|disconnected\|there and then\|on the row\|row relays\|all-or-nothing" docs/manual docs/layout docs/design/mcp-client.md layout.md .capcom/knowledge/glossary` and the same grep over the non-test `cmd/apogee/*.go` and `internal/tui/*.go`. Startup's all-or-nothing stays.
- **Busy-list rule:** every enumeration of busy waits. Find them with `grep -rn "naming call\|/rename\|busy indicator" layout.md docs/manual .capcom/knowledge/glossary internal/tui/busy.go internal/tui/doc.go`.
- **Settings layout:** the row annotations in `docs/layout/settings-screen-layout.md` ("the one deferral wording", "in force by the time the row repaints") gain `reconnecting MCP…`.
- **Glossary:** Notes `busy-indicator`, `settings-surface` and `mcp-client` (all under `.capcom/knowledge/glossary/glossary-safety/`).
- **Pinned phrases:** "Both lists are live." and "rebuilds the session's" stay (`cmd/apogee/docs_env_test.go`).
- **ADRs:** ADR 0109 and `adr-0037-amend-1107` already exist. This item only checks that they match what shipped, and corrects a drift by a dated amendment.
**Regression guard.**
- The busy-list acceptance counts only files whose hit is an enumeration of busy waits — at the check `layout.md` ("Outside a Turn: the busy indicator" and the status-line tiers), `docs/manual/configuration.md` (the `ui.spinner` paragraph), the `busy-indicator` Note, `internal/tui/busy.go` and `internal/tui/doc.go`; the `/rename` and naming-call hits in `docs/manual/sessions.md`, `docs/manual/commands.md` and `glossary-context/system-prompt.md` are not busy lists.
- The reconnect-rule grep also runs over the non-test `internal/mcp/*.go` and `internal/agent/swaptools.go` (e.g. `transport.go`'s `vetEndpoint` comment, "the settings row's reconnect note has no other source for it").
- The `cmd/apogee` drift run is `-run 'Drift|Doc|Manual'`, so `TestManualListsEveryEnvironmentOverride` and `TestManualListsEveryEventLineKind` (both read `docs/manual`) run.
**Files:** docs/manual/configuration.md; docs/manual/commands.md; docs/design/mcp-client.md; docs/layout/settings-screen-layout.md; layout.md; .capcom/knowledge/glossary/glossary-safety/busy-indicator.md; .capcom/knowledge/glossary/glossary-safety/settings-surface.md; .capcom/knowledge/glossary/glossary-safety/mcp-client.md; plus every non-test `cmd/apogee/*.go` / `internal/tui/*.go` / `internal/mcp/*.go` / `internal/agent/swaptools.go` the rule's grep hits
**Read first:** docs/manual/configuration.md — the `mcp-servers:` live paragraph, the url-safety live paragraph, the ui.spinner busy list; docs/manual/commands.md — the /settings editor-jump paragraph ("reconnects … the reason on the row"); docs/design/mcp-client.md — Connect all-or-nothing, Resume reconnects fresh;
cmd/apogee/docs_env_test.go — TestDocsEnvURLSafetyProseIsLiveAndCoversMCP, TestManualListsEveryEnvironmentOverride; layout.md — "Outside a Turn: the busy indicator"; internal/tui/busy.go — busyWait doc comment; internal/mcp/transport.go — vetEndpoint comment;
.capcom/knowledge/glossary/glossary-safety/settings-surface.md, busy-indicator.md, mcp-client.md
**Tests:** the doc drift tests, one package per run.
**Acceptance:**
- `capcom check`
- `grep -c "reconnecting MCP…"` is ≥ 1 in each file whose busy-list-rule hit is an enumeration of busy waits (see the guard)
- `GOMEMLIMIT=2GiB go test -count=1 -run 'Drift|Doc' ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -count=1 -run 'Drift|Doc|Manual' ./cmd/apogee/`
**Depends on:** item 10
**Commit:** `docs(mcp): servers reconnect one by one, off the Update loop`
