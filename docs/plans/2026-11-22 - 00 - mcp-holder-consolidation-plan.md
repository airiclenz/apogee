# MCP holder consolidation plan

**Goal:** Every MCP connection fact gets one owner. The `liveMCP` holder owns the url-safety move and startup. One fold serves both the journal seed and the side panel. "Dials queued" is a typed fact, not a note string. One `mcpauth.Binding` carries the OAuth facts for both login and connect.
**Date:** 2026-11-22
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 610fb47b

**Sources:**
- `capcom show mcp-oauth-assembly`, `mcp-state-folded-twice`, `mcp-deny-move-scattered`, `mcp-startup-outside-holder`
- ADR 0109 (`capcom know show "ADR 0109" --with-children`: D5, D7, D11); ADR 0095 (D1, amend-1111 D9)
- `docs/design/mcp-client.md`; `docs/manual/configuration.md` (the MCP sections)

**Ratified design calls** (Airic Lenz, 2026-11-22):
- **Startup-skipped server:** a repo server that startup skips enters the holder as an ordinary failed server (ADR 0109 D7). It is re-dialled only by re-adoption (a moved pin changes `ServerConfig.Equal`), the retry gesture, or a restart. Startup becomes the holder's first request. ADR 0109 D11 is amended.
- **Deny move:** two phases on the holder. `liveMCP.deny(after)` runs before the host-list swap, then a plain `request` of the whole layered set runs after it. The holder partitions its own last-requested set, never the config file. A denied server keeps one representation: known, `isAdmitted=false`.
- **Dials-queued fact:** a query on `tui.MCPHost` (`MCPDialQueued() bool`). `RetryFailedMCP` returns `(queued bool, err error)`. `SettingsHost.Apply` keeps its signature. The TUI words the note.
- **OAuth secret sentence:** connect's wording wins for both paths: `mcp: server %q: client-secret-env: the environment variable %s is not set`. Login also takes connect's store sentence.

**Regression check (2026-11-22, 610fb47b):**
- 1: guard folded (`Binding.validate` checks only the name and the secret's issuer; no sentence changes).
- 2: guard folded (writer's decision: deleted-symbol greps target `func EndpointClient` / `mcp\.EndpointClient`; plus the refusal-test fixtures).
- 3: guard folded (Acceptance proves the amendment exists and is named).
- 5: recast (writer's decision: item 5 owns the whole retry-gesture switch).
- 6: recast (writer's decision: scope is the settings-apply path only); guards folded (nil host, the fake's queued answer, `requestMCP`).
- 7: guards folded; the D5 note supersedes the "retired and dialled nowhere" sentence of the 2026-11-07 D7 amendment (adr-0109-decision.md).
- 8: guard folded (writer's decision: item 8 owns the helper-constructor edits; plus the skip's cause and a grep).
- 9: guard folded (the OAuth prelude runs once, before the first dial).
- 5: recast (second round, writer's decision: item 5 also owns the settingswatcher retry comparison; `fakeMCPHost.MCPDialQueued` answers `len(f.dials) > 0` from item 5, no static `dialQueued` field).
- 6: recast (second round, writer's decision: the settingswatcher retry comparison and the double's `MCPDialQueued` are item 5's); guards folded (the `requestMCP` row of `TestSettingsApplierReloadsRefuseAnUnparseableFile`, a watched-save arming subtest, every comment saying the host answers the note).
- 7: guard folded (writer's decision: Acceptance greps the deleted symbols, narrowed to `plan\.isMoved` because `isMoved` is also `requestServerLocked`'s own local, which item 7 keeps).

**Standing requirements:**
- skills: coding-standards
- This box is a Pi 5: never race-run a whole heavy package. Run one package and one `-run` pattern per command, with `GOMEMLIMIT=2GiB`. Never run two test runs at once. Pass these rules on to every sub-agent.
- Bubble Tea v2 (`charm.land/bubbletea/v2`) names only.
- Every doc or comment naming a deleted symbol (`retire`, `dropDenied`, `skipped`, `func EndpointClient` / `mcp.EndpointClient`, `mcpClientSecret`, `mcpServerStates`) is fixed by the item that deletes it. Find them with `grep -rn '<symbol>' --include='*.go' --include='*.md' cmd internal docs/design docs/manual`; for `EndpointClient` grep `'func EndpointClient\|mcp\.EndpointClient'`, never the bare word — it survives as a field of `mcpauth.Binding`. Historical text stays as written.

**Out of scope:**
- `offerUnsaved` and `logoutMCPServer` reading the store with the raw endpoint (logout must work offline; vetting resolves DNS).
- A per-request count of queued dials (`isDialQueued` stays holder-wide).
- The other open architecture-review issues.

## 1. One OAuth binding value in mcpauth — ✅ DONE (2026-11-22)

NOTES (2026-11-22): `requireSecretIssuer` stays in login.go and `clientMatches`/`issuerMatches` (now `Binding` methods) with their reason constants stay in handler.go, so no code moves between files; binding.go holds the type and `Binding.validate`. The per-field docs of the two configs merged into `Binding`'s.

NOTES (2026-11-22): `Binding.validate` runs first in both `LoginConfig.validate` and `NewHandler`, so the secret-without-issuer check now precedes the config's own endpoint/store/client/redirect checks; only a config with two faults at once reports a different (unchanged) sentence first.

NOTES (2026-11-22): the `ServerConfig.Issuer` comment in internal/mcp/transport.go now cites `mcpauth.Binding.Issuer` in place of the two configs' fields.

**What:**
**Goal:** `mcpauth.LoginConfig` and `mcpauth.HandlerConfig` both embed one `mcpauth.Binding` value. That value carries the name, endpoint, store, endpoint client, auth client, client id, client secret and issuer. One `Binding.validate` checks the shared facts for both configs.
**Approach (assumed at the header base):**
- Add `type Binding struct{Name, Endpoint string; Store *Store; EndpointClient, AuthClient *http.Client; ClientID, ClientSecret, Issuer string}` in a new `internal/mcpauth/binding.go`.
- `LoginConfig` keeps only `RedirectURL` and `FetchCode` beside the embedded `Binding`.
- `clientMatches` and `issuerMatches` become `Binding` methods.
- `requireSecretIssuer` stays called from both `Login` and `NewHandler`, because an embedder Host reaches mcpauth directly.
- Every `LoginConfig{`/`HandlerConfig{` composite literal spells `Binding:`. That covers the mcpauth tests, `loginMCPServer` and `newOAuthHandler`, which change mechanically so the build stays green.
- This item is a pure refactor: no sentence changes.

**Regression guard.** No sentence changes, so `Binding.validate` checks only the server name (`ValidateServerName`) and `requireSecretIssuer`. `LoginConfig.validate` keeps its own endpoint, store/client/`FetchCode` and `RedirectURL` checks with today's sentences (login.go:155-160); `NewHandler` keeps its own store/client check and sentence (handler.go:93-94). The two store sentences differ today and no test pins either.

**Files:** internal/mcpauth/binding.go, internal/mcpauth/handler.go, internal/mcpauth/login.go, internal/mcpauth/handler_test.go, internal/mcpauth/login_test.go, internal/mcp/transport.go, cmd/apogee/mcp_cmd.go
**Read first:** internal/mcpauth/login.go — LoginConfig, LoginConfig.validate, requireSecretIssuer; internal/mcpauth/handler.go — HandlerConfig, NewHandler, clientMatches;
internal/mcp/transport.go — newOAuthHandler; cmd/apogee/mcp_cmd.go — loginMCPServer

**Tests:**
- The existing mcpauth suites stay green.
- New `TestBindingValidateRefusesMissingFacts` in `internal/mcpauth/handler_test.go`: an unsafe name and a client secret without an issuer.

**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestLogin|TestNewHandler|TestHandler|TestBinding' ./internal/mcpauth/`
- `go build ./...`
- `go vet ./internal/mcpauth/ ./internal/mcp/ ./cmd/apogee/`

**Commit:** `refactor(mcpauth): login and connect configs embed one OAuth binding`

## 2. internal/mcp assembles the OAuth binding for login and connect

Depends on item 1.

**What:**
**Goal:** The OAuth facts are assembled in one function in `internal/mcp`. It covers the store presence check, the auth-client default, the secret read from `client-secret-env` at each dial, and the issuer. `apogee mcp login` and connect both use it. `mcp.EndpointClient` and `mcpClientSecret` no longer exist. An unset `client-secret-env` gives `mcp: server %q: client-secret-env: the environment variable %s is not set` on both paths.
**Approach (assumed at the header base):**
- Add an unexported `oauthBinding(host, cfg, guard, endpoint, endpointClient) (mcpauth.Binding, error)` in `internal/mcp/transport.go`.
- Add an exported `OAuthBinding(ctx, host, cfg, guard)`, which is `vetEndpoint` + `oauthBinding`, for login.
- `buildStreamableTransport` keeps its single `vetEndpoint` call (never vet twice: `GuardedClient` pins DNS) and passes the result on. `newOAuthHandler` shrinks to `NewHandler(HandlerConfig{Binding: b})`.
- `loginMCPServer` calls `mcp.OAuthBinding`.
- Delete `EndpointClient` and `mcpClientSecret`.
- Login reaches its auth client only through `host.NewOAuthClient`, with `mcpLoginDeps.mcpHost` setting it, so the test loopback client still arrives.
- Never cache the secret at config load.

**Regression guard.** `EndpointClient` survives as a field of mcpauth.Binding, so every "deleted symbol" grep targets `func EndpointClient` / `mcp\.EndpointClient` and `mcpClientSecret`, never the bare word `EndpointClient` — fix item 2's Acceptance grep and the header's deleted-symbol standing requirement accordingly.
The refusal tests must reach the secret read: `validateServers` (and config load) refuse `client-secret-env` without `client-id` and `issuer`. The new `TestConnect_OAuthServerRefusals` case adds `clientID`/`secretEnv` table fields and sets `ClientID`, a valid https `Issuer`, the unique `ClientSecretEnv` and `hasStore: true` (the store check runs first). `TestMCPCmdLoginRefusesAnUnsetClientSecret` sets `ClientID` and `Issuer` on the server `writeMCPConfig` writes, as `TestMCPCmdLoginRefusesAnotherIssuer` does, and proves "no request" with a request counter added to `fakeOAuthMCP.serveResource`.

**Files:** internal/mcp/transport.go, internal/mcp/transport_test.go, cmd/apogee/mcp_cmd.go, cmd/apogee/mcp_cmd_test.go
**Read first:** cmd/apogee/mcp_cmd.go — loginMCPServer, mcpClientSecret, mcpLoginDeps.mcpHost; internal/mcp/transport.go — buildStreamableTransport, newOAuthHandler, EndpointClient;
internal/mcp/transport_test.go — TestConnect_OAuthServerRefusals; cmd/apogee/mcp_cmd_test.go — newFakeOAuthMCP

**Tests:**
- New "unset client-secret-env" case in `TestConnect_OAuthServerRefusals`. It pins the exact sentence and uses a unique variable name that nobody sets; the table gains `clientID`/`secretEnv` fields.
- New `TestOAuthBindingCarriesTheConfiguredFacts`.
- New `TestMCPCmdLoginRefusesAnUnsetClientSecret`: the same sentence, and the fake server sees no request (a counter on `fakeOAuthMCP.serveResource`).
- `TestMCPCmdLoginRefusesAnotherIssuer` stays green.

**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestConnect_OAuth|TestOAuthBinding' ./internal/mcp/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestMCPCmd|TestStartupMCPLogin|TestMCPLogin' ./cmd/apogee/`
- `! grep -rn 'func EndpointClient\|mcp\.EndpointClient\|mcpClientSecret' --include='*.go' cmd internal`

**Commit:** `refactor(mcp): one OAuth binding is assembled for login and connect`

## 3. ADR 0095 amendment and the design doc for the OAuth binding

Depends on item 2.

**What:**
**Goal:** ADR 0095 records that a stored token is used only when the name and normalised endpoint match and, with a configured `client-id`, the record's client also matches. It cross-references amend-1111 D9 for the issuer. The ADR's Status line and its reading-order list name the amendment. `docs/design/mcp-client.md` names `mcp.OAuthBinding` as the one place the OAuth facts are assembled, and states the client and issuer match conditions.
**Approach (assumed at the header base):**
- Run `capcom know add --id adr-0095-amend-<MMDD of the day it lands> --parent adr-0095`. The amendment states a match condition, not a storage-key change: the file stem stays the name, and no migration is needed.
- Edit the paragraph in `docs/design/mcp-client.md` that says tokens are "keyed by the server name and the normalised endpoint".

**Regression guard.** Acceptance proves the amendment exists and is named: `capcom know show "ADR 0095" | grep -n 'adr-0095-amend-<MMDD>'` (Status line and reading-order sentence) and `capcom know show adr-0095-amend-<MMDD> | grep -n 'client-id'` replace the `--with-children` grep, which already passes at the base (adr-0095-decision.md:25, adr-0095-amend-1111.md:7).

**Files:** .capcom/knowledge/decisions/adr-0095.md, .capcom/knowledge/decisions/adr-0095/ (new amendment Note), docs/design/mcp-client.md
**Read first:** .capcom/knowledge/decisions/adr-0095.md — Status line, reading-order sentence; .capcom/knowledge/decisions/adr-0095/adr-0095-decision.md — D1, D3; .capcom/knowledge/decisions/adr-0095/adr-0095-amend-1111.md — D9;
docs/design/mcp-client.md — token-store paragraph ("keyed by the server name and the normalised endpoint"); internal/mcpauth/handler.go — clientMatches, issuerMatches
**Closes:** mcp-oauth-assembly

**Tests:** none (docs only).

**Acceptance:**
- `capcom know show "ADR 0095" | grep -n 'adr-0095-amend-<MMDD>'` (the Status line and the reading-order sentence)
- `capcom know show adr-0095-amend-<MMDD> | grep -n 'client-id'`
- `capcom check`
- `grep -n 'OAuthBinding' docs/design/mcp-client.md`

**Commit:** `docs(adr): ADR 0095 amendment — a stored token matches on client id too`

## 4. One MCP state fold serves the seed and the side panel

**What:**
**Goal:** One fold of `MCPServerEvent`s exists: `sidepanel.MCPServers`. It keeps each server's raw last Event and tool count, and `Events()` restates the state for the journal and TUI seed with the raw text unstripped. `Rows()` alone strips escapes. `cmd/apogee` has no separate state fold.
**Approach (assumed at the header base):**
- Extend `sidepanel.MCPServers` with `Events()`, mirroring today's `mcpServerStates.events`.
- `mcpOutbox.states` becomes a `sidepanel.MCPServers`.
- Delete `mcpServerStates` and `mcpServerStateEntry` from `cmd/apogee/wire_mcp.go`.
- Keying stays the raw server name.
- Readers stay unchanged: `stateEvents` → `MCPServerStates`, `seedJournal` → `sessionHost.mcpSeed`, and `Rows`/`IsLive` in `internal/tui/sidepanel.go` and `mcppane.go`.

**Files:** internal/sidepanel/mcp.go, internal/sidepanel/mcp_test.go, internal/sidepanel/doc.go, cmd/apogee/wire_mcp.go, cmd/apogee/wire_mcp_test.go, docs/design/mcp-client.md
**Read first:** cmd/apogee/wire_mcp.go — mcpServerStates.fold, mcpServerStates.events, mcpOutbox.stateEvents; internal/sidepanel/mcp.go — MCPServers.Fold, foldMCPRow, Rows;
cmd/apogee/wire_mcp_test.go — TestMCPSeedStatesReplayTheFold; internal/sidepanel/mcp_test.go — TestMCPServersFoldLeavesACopyAlone

**Tests:**
- Move `TestMCPSeedStatesReplayTheFold` into sidepanel as `TestMCPServersEventsReplayTheFold`.
- New `TestMCPServersEventsKeepRawText`: a raw escape survives `Events()` and is stripped in `Rows()`.
- Extend `TestMCPServersFoldLeavesACopyAlone`.

**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestMCPServers' ./internal/sidepanel/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestMCPSeed|TestLiveMCPEmitsLifecycleEvents|TestLiveMCPNeverEmitsUnderLock|TestMCPLost' ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestMCPSeed' ./internal/tui/`

**Commit:** `refactor(mcp): one MCP state fold serves the seed and the side panel`

## 5. The MCP host answers whether dials are queued

**What:**
Recast at the regression check (2026-11-22).
**Goal:** `tui.MCPHost` has `MCPDialQueued() bool`, answered by the holder (`liveMCP.isDialQueued`). `RetryFailedMCP` returns `(queued bool, err error)`. Every implementation and test double compiles against the new interface. The retry gesture arms on `queued`; the settings-apply arming does not change yet (item 6).
**Approach (assumed at the header base):**
- Add the method to the `MCPHost` interface in `internal/tui`.
- Implement it on `rootWiring`. `RetryFailedMCP` reports `queued` from the holder.
- The `fakeMCPHost` double answers `MCPDialQueued()` as `len(f.dials) > 0` (no static `dialQueued` field) and gains a queued answer for the retry.
- `retryFailedMCP` in the TUI arms on `queued` and sets the row answer to `MCPReconnectingNote` itself.

**Regression guard.** item 5 owns the whole retry-gesture switch — `retryFailedMCP` in internal/tui arms on the `queued` answer of `RetryFailedMCP` in item 5 (its signature change forces it), with its tests; drop the "temporarily keeps its note comparison" sentence.
`internal/tui/settingswatcher.go` (the `note == MCPReconnectingNote` compare) and `cmd/apogee/wire_settings_test.go` (the `TestMCPUnchangedRoundTrip…` note asserts) stop compiling otherwise, so both are this item's.
Item 5 also owns the settingswatcher retry comparison (`retryFailedMCP` in internal/tui/settingswatcher.go arms on `queued`), and the `fakeMCPHost` double defines `MCPDialQueued()` as `len(f.dials) > 0` from item 5 on — no static `dialQueued` field.

**Files:** internal/tui/mcpreconnect.go, internal/tui/mcpreconnect_test.go, internal/tui/settingswatcher.go, cmd/apogee/wire_mcp.go, cmd/apogee/wire_settings.go, cmd/apogee/wire_mcp_test.go, cmd/apogee/wire_settings_test.go
**Read first:** internal/tui/mcpreconnect.go — MCPHost; internal/tui/settingswatcher.go — Model.retryFailedMCP; cmd/apogee/wire_mcp.go — rootWiring.RetryFailedMCP, liveMCP.isDialQueued;
internal/tui/mcpreconnect_test.go — fakeMCPHost, TestMCPRowUnchangedRoundTrip; cmd/apogee/wire_settings_test.go — TestMCPUnchangedRoundTripRedialsOnlyTheFailedServer;
cmd/apogee/wire_mcp_test.go — TestMCPLostServerIsRedialledByTheRetryGesture

**Tests:**
- New `TestMCPDialQueuedReportsTheHoldersQueue` in `cmd/apogee/wire_mcp_test.go`: false when idle, true after a request queues a dial, false after the dial is taken.
- `TestMCPRowUnchangedRoundTrip` (internal/tui) arms on the double's queued answer rather than a note.
- `TestMCPUnchangedRoundTripRedialsOnlyTheFailedServer` / `…WithNothingFailedDialsNothing` and `TestMCPLostServerIsRedialledByTheRetryGesture` assert `queued` true/false instead of the note.

**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestMCPDialQueued|TestMCPLostServerIsRedialledByTheRetryGesture|TestMCPUnchangedRoundTrip' ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestMCPRow|TestMCPRetry' ./internal/tui/`

**Commit:** `refactor(tui): the MCP host answers whether dials are queued`

## 6. The mcp-servers row arms on the queued-dials fact, not a note string

Depends on items 4 and 5.

**What:**
Recast at the regression check (2026-11-22).
**Goal:** The `mcp-servers` settings row arms the reconnecting state from `MCPHost.MCPDialQueued()`, never by comparing a host-returned note (the retry gesture switched in item 5). The host's `mcp-servers` apply returns no note. The TUI sets `tui.MCPReconnectingNote` itself. An apply answering `""` with dials queued still arms, and this includes watched saves.
**Approach (assumed at the header base):**
- `requestMCPFrom` returns only an error.
- In `settingsApplied`, between the apply and `recordSettingEdit`: when the path is `mcp-servers`, the apply had no error and `MCPDialQueued()` is true, set `edit.note = MCPReconnectingNote` and arm the row. Never gate this on `!edit.watched`.
- The comparison in `clearMCPReconnectingNote` then only checks the TUI's own note.
- Update the `Apply` doc comment in `internal/tui/tui.go`.
- Drop the note answer from `mcpRowApplies.apply`. `projectrules.go` adjusts its `_` binding.

**Regression guard.** item 6's scope is only the settings-apply path (`settingsApplied`, `clearMCPReconnectingNote`, `requestMCPFrom` returning only an error, the Apply doc) — remove the `retryFailedMCP arms on queued` bullet.
The new arm in `settingsApplied` runs only with a wired host (`m.opts.MCP != nil`), as `retryFailedMCP` guards (settingswatcher.go:214): `opts.MCP` is nil when `rootWiring.mcpHost` answers nil and in every `newTestModel`. Item 5's `fakeMCPHost.MCPDialQueued` (`len(f.dials) > 0`, the queue `host.queue` fills) lets `TestMCPRowShowsTheOutcomeWhenTheDialsLand` still arm; item 6 does not redefine it. `requestMCP` follows `requestMCPFrom` and returns only an error, so `rootWiring.RetryFailedMCP` (cmd/apogee/wire_mcp.go) and the `wire_settings_test.go` caller adjust.
- The `requestMCP` row of `TestSettingsApplierReloadsRefuseAnUnparseableFile` (wire_settings_test.go) asserts `!a.mcp.isDialQueued()` in place of its note check.
- Watched saves arm too: `TestMCPRowArmsOnAQueuedDialWhateverTheNote` gets a watched subtest — `mcpRowModel` + `host.queue`, then `configChangedMsg{alive: true}` (as `TestMCPDialStartsWhileWatching` drives it) — asserting the row reads `· reconnecting MCP…` and `applies.watched == [true]`.
- Every comment saying the host (`Apply`, `requestMCP`, `requestMCPFrom`) answers `MCPReconnectingNote` now says the TUI words it — wire_settings.go, projectrules.go, settings.go, settingsapply.go, `mcpRowApplies.apply`'s doc; find them with `grep -rn 'MCPReconnectingNote\|in-progress note\|in-progress one\|The note is' --include='*.go' cmd/apogee internal/tui`.

**Files:** internal/tui/settingsapply.go, internal/tui/settings.go, internal/tui/mcpreconnect.go, internal/tui/tui.go, internal/tui/mcpreconnect_test.go, cmd/apogee/wire_settings.go, cmd/apogee/wire_mcp.go, cmd/apogee/projectrules.go, cmd/apogee/wire_settings_test.go
**Read first:** internal/tui/settingsapply.go — Model.settingsApplied; cmd/apogee/wire_settings.go — settingsApplier.requestMCPFrom, settingsApplier.requestMCP; internal/tui/mcpreconnect.go — Model.armMCPRow, Model.clearMCPReconnectingNote;
internal/tui/mcpreconnect_test.go — mcpRowApplies.apply, TestMCPDialStartsWhileWatching; cmd/apogee/wire_settings_test.go — TestSettingsApplierReloadsRefuseAnUnparseableFile
**Closes:** mcp-state-folded-twice

**Tests:**
- New `TestMCPRowArmsOnAQueuedDialWhateverTheNote`, with a watched subtest (`configChangedMsg{alive: true}` arms the row, `applies.watched == [true]`).
- New `TestMCPRowRemovalOnlyNeverArms`.
- The `wire_settings_test.go` asserts on the returned note change to assert on the fact (item 5 already moved the `RetryFailedMCP` asserts).
- `TestSettingsApplierReloadsRefuseAnUnparseableFile`'s `requestMCP` row asserts `!a.mcp.isDialQueued()` in place of the note check.
- `TestMCPRowShowsTheOutcomeWhenTheDialsLand` arms through `fakeMCPHost.MCPDialQueued` answering `len(f.dials) > 0`.

**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestMCPRow|TestMCPDial|TestMCPRemoval|TestMCPNilHost' ./internal/tui/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestApplySettingMCP|TestMCPUnchangedRoundTrip|TestMCPLostServerIsRedialledByTheRetryGesture|TestProjectRule|TestSettingsApplierReloadsRefuseAnUnparseableFile' ./cmd/apogee/`

**Commit:** `refactor(tui): the mcp-servers row arms on the queued-dials fact`

## 7. The url-safety move is a two-phase move on the MCP holder

**What:**
This also fixes a defect found at the base: `readmitMCP` requests only the admitted subset, so a server that is still configured but denied (known with `isAdmitted=false`) is disconnected and posted as `disconnected removed`, when it should stay denied.
**Goal:** The holder owns the url-safety deny move:
- `liveMCP.deny(after security.URLGuard)` runs before the host-list swap. Over the holder's own last-requested servers, it supersedes the dial, drops the live session, keeps the server known with `isAdmitted=false`, and posts it denied.
- A plain `request` of the whole layered set readmits after the swap.
- `retire`, `dropDenied`, `dropDeniedMCP`, `sameServerNames` and the `mcpReadmit` admitted/moved gate no longer exist.
- A server denied and still configured is never reported removed.
- A config file that fails to load still has its live denied sessions closed.

**Approach (assumed at the header base):**
- `applyURLSafetyHosts` calls the holder's `deny`, then the swap (refusable mid-Turn: nothing is requested when the swap is refused), then `request`.
- `denyMCP` and `readmitMCP` only word the row: `deniedReason`, `refused`, and the load-error clause.
- A live entry whose own endpoint is denied keeps the `canRedialDroppedLocked` behaviour.
- Record a one-line dated note under ADR 0109 D5: the holder partitions its own requested set.

**Regression guard.**
- `deny` acts on, posts and returns a clause only for a server with `state.isAdmitted && !admits(after)`, or whose live endpoint `after` denies — one clause per server, so a later url-safety edit (a web-host one included) never re-posts an already-denied server; `deniedReason` still gets the `mcp.Denied` error.
- `deny` sets `isAdmitted, hasFailed = false, false` for a server whose configured endpoint it denies, as `settleLocked` does, so the retry gesture never dials a denied server.
- A configured server the holder never knew is now dialled (and fails) by the readmit request. `TestApplySettingURLSafetyRowCarriesTheMCPLabelOnce` seeds docs as a known live entry (`resolvedMCPServer`) and keeps asserting the row clause and one outcome; `TestApplySettingURLSafetyHostsLeavesMCPAloneWhenNoVerdictMoved` seeds docs with `resolvedMCPServer(t, path, "docs")`, and its doc comment says the holder diff, not a name compare, keeps the connection.
- The dated note goes under D5 in `adr-0109/adr-0109-decision.md` and is named on `adr-0109.md`'s Status line. It supersedes the 2026-11-07 D7 amendment's "is retired and dialled nowhere" (adr-0109-decision.md:52): such a server stays known and denied, dialled nowhere.

**Files:** cmd/apogee/wire_mcp.go, cmd/apogee/wire_settings.go, cmd/apogee/wire_mcp_test.go, cmd/apogee/wire_settings_test.go, cmd/apogee/wire_live_test.go, .capcom/knowledge/decisions/adr-0109.md, .capcom/knowledge/decisions/adr-0109/adr-0109-decision.md, docs/design/mcp-client.md
**Read first:** cmd/apogee/wire_settings.go — applyURLSafetyHosts, denyMCP, readmitMCP; cmd/apogee/wire_mcp.go — dropDenied, retire, requestServerLocked, settleLocked;
cmd/apogee/wire_helpers_test.go — resolvedMCPServer
**Closes:** mcp-deny-move-scattered

**Tests:**
- Rewrite the "denied by a url-safety edit" subtest in `wire_mcp_test.go`.
- New `TestURLSafetyDenyKeepsTheServerKnownAsDenied`: a later widening dials it once, and the retry gesture before it dials nothing.
- Rewrite the fixtures of `TestApplySettingURLSafetyRowCarriesTheMCPLabelOnce` and `TestApplySettingURLSafetyHostsLeavesMCPAloneWhenNoVerdictMoved` (docs seeded through `resolvedMCPServer`).
- New `TestURLSafetyReadmitNeverReportsADeniedServerRemoved`: this must fail at the base.
- New `TestURLSafetyDenyWithAnUnreadableConfigClosesTheConfiguredServer`.

**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestURLSafety|TestLiveMCPDeny|TestLiveMCPEmitsLifecycleEvents|TestApplySettingURLSafety|TestMCPReconnectUsesTheLiveURLSafetyLists|TestLiveMCPCommitReadmits' ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestE2EWatchedURLSafetyDenyMidTurnRefusesTheMCPTools' ./cmd/apogee/`
- `! grep -rnE 'func \(m \*liveMCP\) (retire|dropDenied)|dropDeniedMCP|sameServerNames|plan\.isMoved|mcpReadmit\{' cmd/apogee/*.go`

**Commit:** `fix(mcp): a url-safety move is one two-phase move on the MCP holder`

## 8. A startup-skipped repo server is an ordinary failed server

Depends on item 7.

**What:**
**Goal:** A repo server that startup skips (moved or unreadable pin) is held as a failed server in the holder's `servers`/`requested` sets, with no session. An unrelated later request never re-dials it. The retry gesture or a re-adoption with a new pin does dial it. `liveMCP.skipped` and its removal branch in `request` no longer exist. ADR 0109 D11 records the rule.
**Approach (assumed at the header base):**
- `newLiveMCP` takes the skipped configs and records each as `{config, isAdmitted: true, hasFailed: true}`.
- The outbox seed folds the failed Event as today.
- `emitThrough(sink)` loses its skipped argument. `connectMCPServers` returns the skipped configs.
- Run `capcom know add --id adr-0109-amend-<MMDD of the day it lands> --parent adr-0109`, and name it on the Status line and in the reading order.
- Update the docs/manual lines that describe a skipped repo server.

**Regression guard.** item 8 owns every compile-preserving edit its `newLiveMCP`/`emitThrough` reshaping forces, including the test-helper constructors (`cmd/apogee/wire_helpers_test.go`, `cmd/apogee/settingsapplier_helper_test.go`, `recordMCPEvents` callers) — add them to its Files; item 9 then edits those helpers only for its own `startLiveMCP` change.
The skip's cause survives: `newLiveMCP` takes the startup's failed Events (or `mcp.Denied`) beside the configs, so the seed still folds `wiki failed` with "bin/wiki.sh changed since you adopted it"; the flipped `TestMCPSeedCarriesTheStartupState` keeps its Cause assertion.

**Files:** cmd/apogee/wire_mcp.go, cmd/apogee/wire_live.go, cmd/apogee/mcp_cmd.go, cmd/apogee/wire_mcp_test.go, cmd/apogee/wire_helpers_test.go, cmd/apogee/settingsapplier_helper_test.go, .capcom/knowledge/decisions/adr-0109.md, .capcom/knowledge/decisions/adr-0109/ (new amendment Note), docs/manual/configuration.md, docs/design/mcp-client.md
**Read first:** cmd/apogee/mcp_cmd.go — connectMCPServers, announceSkippedMCP; cmd/apogee/wire_mcp.go — newLiveMCP, emitThrough, request;
cmd/apogee/wire_mcp_test.go — TestMCPSeedCarriesTheStartupState, recordMCPEvents; cmd/apogee/wire_helpers_test.go — newMCPFixtureDialling

**Tests:**
- Flip `TestMCPSeedCarriesTheStartupState`: the skipped server is now known as failed, and its Cause assertion stays.
- New `TestMCPStartupSkippedServerIsNotRedialledByAnUnrelatedRequest`.
- New `TestMCPStartupSkippedServerIsRedialledByTheRetryGesture`.
- New `TestMCPStartupSkippedServerReadoptedWithANewPinDials`.

**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestMCPSeed|TestMCPStartupSkipped|TestStartupSkips|TestReconnectSkipsARepoServerWhosePinMoved' ./cmd/apogee/`
- `capcom check`
- `! grep -rn 'm\.skipped' cmd/apogee/*.go`

**Commit:** `refactor(mcp): a startup-skipped repo server is an ordinary failed server`

## 9. Startup is the MCP holder's first request

Depends on item 8.

**What:**
**Goal:**
- `startLiveMCP` builds the holder and runs the startup connect as its first request: synchronous, fatal on any failure (ADR 0109 D11), with every opened session closed before the error returns.
- Every held session has a non-nil `cancel`. The outbox and the sink are attached in the constructor, primed with states rather than emitted Events, so the journal seed is never duplicated and no Reaction fires.
- `emitThrough` no longer exists.

**Approach (assumed at the header base):**
- `startLiveMCP(ctx, servers, guard, startupDial, connect, sink) (*liveMCP, mcpStartupReport, error)` dials in config order through the startup recipe. That recipe is the `offerUnsaved` OAuth prelude plus `connectStartupServer`, both staying in `mcp_cmd.go`.
- Each startup session gets `context.WithCancel(ctx)`.
- Truncation and skip notices come back in the report. The wiring in `wire_live.go` announces them and cues the re-read.
- Update the helpers `recordMCPEvents` and the `wire_helpers_test.go` / `settingsapplier_helper_test.go` constructors, only for this item's own `startLiveMCP` change (item 8 made the `newLiveMCP`/`emitThrough` edits).

**Regression guard.** The `offerUnsaved` OAuth prelude runs once over the whole set, after `ValidateServers` and before the first dial, as `connectMCPServers` does today (mcp_cmd.go:500-509); only `connectStartupServer` runs per server, so every login offer still precedes every connect.

**Files:** cmd/apogee/wire_mcp.go, cmd/apogee/mcp_cmd.go, cmd/apogee/wire_live.go, cmd/apogee/wire_helpers_test.go, cmd/apogee/settingsapplier_helper_test.go, cmd/apogee/wire_mcp_test.go, cmd/apogee/mcp_cmd_test.go
**Read first:** cmd/apogee/mcp_cmd.go — connectMCPServers, connectStartupServer, mcpStartupLogin.offerUnsaved; cmd/apogee/wire_live.go — wireSession; cmd/apogee/wire_mcp.go — liveMCP.close, emitThrough;
cmd/apogee/mcp_cmd_test.go — TestStartupMCPLoginOffersEveryServerBeforeTheConnect; cmd/apogee/wire_mcp_test.go — TestMCPLostBeforeEmitThroughIsSeenLost
**Closes:** mcp-startup-outside-holder

**Tests:**
- Replace `TestMCPLostBeforeEmitThroughIsSeenLost` with `TestMCPLostDuringStartupIsSeenLost`.
- New `TestStartLiveMCPClosesEverythingOnAFailedDial`.
- New `TestStartLiveMCPSessionsAreCancellable`.
- `TestStartupMCPLoginOffersEveryServerBeforeTheConnect` stays green.

**Acceptance:**
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestStartup|TestStartLiveMCP|TestMCPSeed|TestMCPLost|TestLiveMCPNeverEmitsUnderLock|TestLiveMCPCloseCancelsAndJoinsEverything' ./cmd/apogee/`
- `GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestE2EEgressDeniedMCPEndpointStopsTheLaunch' ./cmd/apogee/`
- `! grep -rn 'emitThrough' --include='*.go' cmd`

**Commit:** `refactor(mcp): startup is the MCP holder's first request`
