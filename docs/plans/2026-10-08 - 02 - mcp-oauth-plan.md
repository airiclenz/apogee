# MCP OAuth for HTTP MCP servers — plan

**Goal:** a `streamable-http` MCP server configured with `auth: oauth` connects with an OAuth 2.1
bearer token that apogee obtains by an interactive browser login, persists under `~/.apogee`, and
refreshes on its own. Login runs at startup (before the TUI) or through `apogee mcp login <name>`.
**Date:** 2026-10-08
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** 52e9922e
**Closes:** apogee-rw6

**Sources:**
- `bd show apogee-rw6`
- `docs/adr/0031-*.md` (north star), `docs/adr/0047-*.md` (credentials), `docs/adr/0042-*.md` (CGO-free, optional external programs), `docs/adr/0008-*.md`
- `docs/design/mcp-client.md` (endpoint-only floor exemption, origin pin)
- MCP authorization spec 2025-06-18 / 2025-11-25; RFC 9728, 8414, 7591, 8707, 8252
- SDK `github.com/modelcontextprotocol/go-sdk` (go.mod version) — packages `auth`, `oauthex`

**Ratified design calls** (owner, 2026-10-08):
- **Token store:** one 0600 JSON file per server under `~/.apogee/mcp-auth/`, dir 0700, atomic write; keyed by server name + normalised endpoint (a mismatch is a miss). New ADR amends ADR 0047 for tokens apogee itself minted.
- **Opt-in:** explicit `auth: oauth` on `streamable-http` servers only; refused on `sse`/`stdio`; `Authorization` in `headers:`/`headers-env:` refused alongside it.
- **Client identity:** Dynamic Client Registration, persisted with the token; optional `client-id:` + `client-secret-env:` for a preregistered client. No Client ID Metadata Document.
- **Login UX:** startup login on stderr before the TUI when a server needs one, plus `apogee mcp login|logout <name>`. Mid-session (reconnect, failed refresh) never prompts: the error names `apogee mcp login <name>`.
- **Browser:** the full authorize URL is always printed; on a local desktop, Enter opens it; nothing opens without a keypress.
- **Remote:** the loopback callback runs and the prompt also accepts the pasted final redirect URL — whichever arrives first.
- **Declined / no TTY at startup:** abort launch with "run `apogee mcp login <name>`" (all-or-nothing connect kept).
- **Logout:** deletes the local token file only; no network call.
- **Preregistered redirect:** listener always binds 127.0.0.1:0; preregistered clients rely on RFC 8252 §7.3 port variance; no fixed-port key (owner, 2026-10-08).

**Standing requirements:**
- skills: coding-standards
- Pi 5 box: tests only as single package + single test (`GOMEMLIMIT=2GiB go test -race -count=1 -run TestX ./internal/<pkg>/`); never a package pattern, never two runs at once.
- Bubble Tea v2 names only (`tea.KeyPressMsg`); the engine stays wire-silent — all login I/O lives in `cmd/apogee`.
- The auth server is untrusted: its HTTP client runs under the SSRF floor with bounded bodies, never the endpoint's pin exemption; the bearer reaches only the configured endpoint origin.

**Out of scope:**
- OAuth on `sse` transports; Client ID Metadata Documents; a `scopes:` override key.
- In-TUI `/mcp` command or auth pane; headless and daemon (they never contact MCP).
- Token revocation (RFC 7009); keystore storage of tokens.

**Regression check (2026-10-08, 52e9922e):**
- 2: guard folded — supersedes, for `auth:` only, the notice-never-refusal rule for transport-mismatched keys (`internal/config/config.go`, `mcpEnvAllowlistNotices`)
- 3: guard folded (writer decision: stem rule, home source, refresh fields in the record)
- 4: guard folded (writer decision: import boundary, saved refresh fields; plus endpoint client, proxy-pinned timed auth client, resource basis, stale-redirect re-registration, client tests)
- 5: guard folded (writer decision: stored token endpoint/resource, import boundary; plus direct refresh with `resource`, cross-process rotation, 403/401 handling, liveMCPHost wiring, timeout)
- 6: guard folded (writer decision: `127.0.0.1:0` + RFC 8252 port variance, doc.go map; plus bind-first API, cancellable stdin) — yields to `internal/security/doc.go` (ResolveProgram is the only exec entry)
- 7: guard folded (writer decision: doc.go map; plus subcommands.go, resolved home, injected deps, pre-connect login loop, reconnect test) — yields to `cmd/apogee/undo.go` `runUndoVerb` (home through resolveRoots)
- 8: guard folded (writer decision: preregistered redirect URI in the manual; plus widened sweep, excluded settled docs, CLI page placement, doc-drift tests)

## 1. ADR: MCP OAuth tokens are persisted by apogee — ✅ DONE (2026-10-08)

**What:**
**Goal:** an ADR records every ratified call above and amends ADR 0047's "stores nothing itself" for MCP OAuth tokens and client registrations only; ADR 0047 carries a dated amendment line pointing at it.
**Approach (assumed at the header base):** new `docs/adr/0095-mcp-oauth-tokens-are-persisted-by-apogee.md` in the house ADR format (read two recent ADRs for it); context = SDK keeps tokens in memory only, keystore missing on Windows/headless Linux; decision = the header's ratified calls; consequences = a file under `~/.apogee` that the `write-apogee-control-plane` rule already guards. Append an amendment note to ADR 0047.
**Files:** docs/adr/0095-mcp-oauth-tokens-are-persisted-by-apogee.md; docs/adr/0047-*.md
**Read first:** docs/adr/0047-api-keys-resolve-through-a-per-entry-key-source.md — decision point 8 ("stores nothing itself"), Consequences; docs/adr/0094-sub-agent-may-run-as-a-one-item-background-workflow.md — house ADR shape (Status/Context/Decision/Considered options/Consequences); internal/security/rules.go — write-apogee-control-plane
**Tests:** none (docs).
**Acceptance:** `test -f "docs/adr/0095-mcp-oauth-tokens-are-persisted-by-apogee.md" && grep -l 0095 docs/adr/0047-*.md`
**Commit:** `docs(adr): record that apogee persists MCP OAuth tokens`

## 2. Config: `auth: oauth`, `client-id:`, `client-secret-env:` — ✅ DONE (2026-10-08)

NOTES (2026-10-08): consequential edit — internal/mcp/client.go: made necessary by the new ServerConfig.ValidateAuth — validateServers calls it beside ValidateHeaders so a host building ServerConfigs without the config loader meets the same rules (its doc comment promises that parity).
NOTES (2026-10-08): the secret-env scrub extends MCPHeaderEnvNames itself (name kept) rather than a sibling, so cmd/apogee/wire_config.go changes only its comment; gofmt realigned the existing toServerConfig field block in internal/config/config.go.

**What:**
**Goal:** an `mcp-servers:` entry accepts `auth: oauth` with optional `client-id:` and `client-secret-env:`; config load refuses `auth` on a non-`streamable-http` server, an `auth` value other than `oauth`, `client-id`/`client-secret-env` without `auth: oauth`, `client-secret-env` without `client-id`, and an `Authorization` header (any case) in `headers:`/`headers-env:` when `auth: oauth` is set. The `client-secret-env` variable name joins the secret-env scrub. `mcp.ServerConfig` carries the three values.
**Regression guard.** No refusal interpolates an empty transport: an entry that omits `transport:` gets transport-free wording (`auth: oauth needs transport: streamable-http`), pinned in the refusal table — the precedent `mcpEnvAllowlistNotices` (internal/config/config.go) never prints an empty transport word. Refusing `auth` on `stdio`/`sse` supersedes, for this key only, that file's notice-never-refusal rule for transport-mismatched `mcp-servers` keys (by the ratified opt-in call): an ignored `auth:` would connect silently unauthenticated — the code comment says so, so nobody "fixes" it to a notice. Acceptance runs named tests only, never a whole package, and includes the end-to-end scrub test.
**Approach (assumed at the header base):** extend `mcpServerConfig` and `toServerConfig` in `internal/config/config.go`; add fields `Auth`, `ClientID`, `ClientSecretEnv` to `mcp.ServerConfig` (`internal/mcp/transport.go`); put the transport/header cross-checks beside `ValidateHeaders`; extend `MCPHeaderEnvNames` (or a sibling consumed at the same site in `cmd/apogee/wire_config.go`) so the scrub sees the secret env name. Error wording follows the existing `mcp-servers` validation errors.
**Files:** internal/config/config.go; internal/config/config_test.go; internal/mcp/transport.go; internal/mcp/transport_test.go; cmd/apogee/wire_config.go; cmd/apogee/wire_config_test.go
**Read first:** internal/config/config.go — mcpServerConfig, toServerConfig, MCPHeaderEnvNames, mcpEnvAllowlistNotices, mcpStdioHeaderNotices; internal/mcp/transport.go — ServerConfig, ValidateHeaders; internal/mcp/client.go — validateServers; cmd/apogee/wire_config.go — projectConfig SecretEnvVars slices.Concat; cmd/apogee/wire_config_test.go — TestProjectConfigScrubsTheMCPHeaderEnvNames; internal/config/registry_test.go — TestRegistryIsBijectionWithFileConfig (mcp-servers is KindStructured, so new sub-keys need no row)
**Tests:** table test `TestApplyConfigMCPServerAuth` per refusal above (the omitted-`transport:` row pins the transport-free wording) plus the accepted shape; `TestServerConfigValidateAuth` for the `internal/mcp` cross-checks; the `client-secret-env` name shown scrubbed by `TestMCPHeaderEnvNamesAreSortedAndDeduplicated` (or a sibling) and end to end by `TestProjectConfigScrubsTheMCPHeaderEnvNames`.
**Acceptance:** `go build ./... && go vet ./internal/config/ ./internal/mcp/ && GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestApplyConfigMCPServerAuth|TestApplyConfigMCPServerHeadersRefusals|TestMCPHeaderEnvNamesAreSortedAndDeduplicated|TestRegistryIsBijectionWithFileConfig' ./internal/config/ && GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestServerConfigValidateAuth' ./internal/mcp/ && GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestProjectConfigScrubsTheMCPHeaderEnvNames' ./cmd/apogee/`
**Commit:** `feat(config): accept auth: oauth on streamable-http MCP servers`

## 3. Token store under `~/.apogee/mcp-auth/`

**What:**
**Goal:** package `internal/mcpauth` exposes a store that loads, saves and deletes one server's record — access token, refresh token, expiry, token type, and the client registration (client id, optional secret, registration endpoint) — at `<apogee home>/mcp-auth/<name>.json`, dir 0700, file 0600, atomic replace; a record whose stored endpoint differs from the normalised configured endpoint loads as a miss; a server name that is not a safe file stem is refused.
**Regression guard.** Item 3 owns the server-name stem rule as an exported `mcpauth` function (names matching `^[A-Za-z0-9][A-Za-z0-9._-]*$` only); items 4, 5 and 7 consume it and never re-implement it. The store takes the apogee home dir as a constructor argument, supplied in cmd/apogee from `config.ApogeeHome` (the same home config.yaml lives in). The record also stores what a refresh needs without re-discovery: the auth server issuer, its token endpoint, the RFC 8707 resource value and granted scopes.
**Approach (assumed at the header base):** copy the CreateTemp + Chmod + Rename shape of `atomicWrite` in `internal/serverstats/store.go` (package-private, as its siblings are); endpoint normalisation = lower-case scheme and host, default port dropped, trailing slash trimmed. The store takes the apogee home dir as a constructor argument (`config.ApogeeHome` supplies it in item 7).
**Files:** internal/mcpauth/store.go; internal/mcpauth/store_test.go; internal/mcpauth/doc.go
**Read first:** internal/serverstats/store.go — atomicWrite, filePerm; internal/mcp/client.go — validateServers; internal/mcp/tool.go — modelToolName; cmd/apogee/wire.go — resolveRoots, stateRoots; internal/config/config.go — ApogeeHome; cmd/apogee/undo.go — runUndoVerb (home precedence rule)
**Tests:** round-trip, including issuer, token endpoint, resource and scopes; modes 0700/0600 (skip mode checks on Windows); endpoint mismatch → miss; delete of a missing record is not an error; path-traversal name refused; the exported stem rule accepts `docs`, `docs.v2`, `a_b-1` and refuses `My Docs`, `.hidden`, `a/b`, `..`, the empty name.
**Acceptance:** `go build ./... && GOMEMLIMIT=2GiB go test -race -count=1 ./internal/mcpauth/`
**Commit:** `feat(mcpauth): persist MCP OAuth tokens under the apogee home`

## 4. Login core: discovery, registration, PKCE code exchange

**What:** Depends on item 3.
**Goal:** `mcpauth.Login(ctx, LoginConfig)` takes a server's endpoint, optional preregistered client, an auth-server `*http.Client`, a redirect URL and a code-fetcher callback (`func(ctx, authorizeURL string) (code, state string, err error)`), and returns a saved record: it probes the endpoint unauthenticated, follows the 401 `WWW-Authenticate` / RFC 9728 well-known metadata to an RFC 8414 auth server, registers by DCR when no client is given, runs authorization code + PKCE S256 with the RFC 8707 `resource` parameter and state check, and exchanges the code. A server that answers the probe without 401 returns a typed "no auth required" error.
**Regression guard.** `internal/mcpauth` imports neither `internal/mcp` nor `internal/config` (config imports mcp, and item 5 makes mcp import mcpauth); home dir, clients and server identity arrive as arguments. Login saves the issuer, token endpoint, resource and scopes into item 3's record.
- The unauthenticated probe and the endpoint-origin PRM fetch run on the endpoint's own vetted client (an exported `internal/mcp` wrapper over `vetEndpoint`, configured `headers:` included), passed in as an argument; the floor client serves auth-server hosts only — else every local/LAN OAuth server fails at dial.
- `NewAuthClient` builds through `GuardedClient` with the auth-server URL as target under `DialFloor` plus `Host.Proxy` (so the egress proxy is pinned) and a finite `Timeout`.
- The RFC 8707 `resource` and the PRM `resource` comparison use `vetEndpoint`'s `u.String()`; item 3's normalised form is the store key only. A stored DCR registration whose `redirect_uris` lack the current redirect URL is re-registered; the saved registration keeps its `token_endpoint_auth_method` for item 5's refresh.
- The PRM candidate URL list and the 2025-03-26 fallback are re-implemented in `mcpauth` (the SDK's are unexported); item-4 flow tests pass a loopback-capable client.
**Approach (assumed at the header base):** reuse the SDK's `oauthex` discovery/registration helpers and `auth.GetAuthServerMetadata`; drive the code flow with `golang.org/x/oauth2` (already in the module graph) rather than the SDK's in-memory `AuthorizationCodeHandler`, so the DCR result and tokens are ours to persist. Add `mcpauth.NewAuthClient` building the auth-server client through the guard's floor dial policy with bounded bodies (mirror `vetEndpoint` minus the origin pin).
**Files:** internal/mcpauth/login.go; internal/mcpauth/login_test.go; internal/mcpauth/client.go; internal/mcp/transport.go
**Read first:** internal/mcp/transport.go — vetEndpoint, checkEndpoint, boundedBodyTransport; internal/security/httpclient.go — GuardedClient, guardedDialControl, DialFloor; go-sdk auth/authorization_code.go — getProtectedResourceMetadata, protectedResourceMetadataURLs, handleRegistration, exchangeAuthorizationCode; go-sdk oauthex — GetProtectedResourceMetadata, ParseWWWAuthenticate, RegisterClient; go-sdk auth/shared.go — GetAuthServerMetadata
**Tests:** an `httptest` resource server + auth server: DCR path; preregistered path (no registration call); state mismatch refused; PKCE verifier sent; `resource` present on authorize and token requests and equal to the endpoint's `u.String()` (an endpoint with a trailing slash still matches its PRM); a stored registration with a stale redirect URI is re-registered; no-401 → typed error; the saved record carries issuer, token endpoint, resource and scopes. `NewAuthClient`: a loopback auth server refused under the default guard; an oversize registration body fails the read; no redirect followed.
**Acceptance:** `go build ./... && GOMEMLIMIT=2GiB go test -race -count=1 ./internal/mcpauth/`
**Commit:** `feat(mcpauth): log in to an OAuth-protected MCP server`

## 5. Connected handler: bearer, refresh, persisted rotation

**What:** Depends on items 2–4.
**Goal:** a `streamable-http` server with `auth: oauth` connects with the stored bearer; an expired access token is refreshed using a session-lifetime ctx (never the triggering request's ctx), and a rotated refresh token is saved before use; with no stored record, or a refresh the auth server rejects, connecting fails with a typed `mcpauth.ErrLoginRequired` whose message names `apogee mcp login <name>`. No path prompts.
**Regression guard.** Refresh uses the record's stored token endpoint and resource, never re-discovery; `internal/mcpauth` imports neither `internal/mcp` nor `internal/config`.
- The refresh request is issued directly — `grant_type=refresh_token` plus `resource` and the stored auth style — on `context.WithoutCancel` with the timed auth client, never `oauth2.Config.TokenSource` (it sends no `resource`). Before refreshing, re-read the record from disk and use a newer unexpired token or refresh token found there; save under a lock (or compare-and-swap on the file).
- `Authorize` returns nil on a 403 that is not `insufficient_scope` (mirroring the SDK's `auth/authorization_code.go`); on a 401 it forces one refresh when a refresh token exists and returns `ErrLoginRequired` only if that fails.
- Item 5 wires `liveMCPHost` (`cmd/apogee/wire_live.go`) with the store at the session's apogee home (item 3's source) and the auth client, whose finite `GuardedClientOptions.Timeout` bounds a teardown refresh at `Close`; a nil store on an `auth: oauth` server is a connect error naming the server.
**Approach (assumed at the header base):** `mcpauth.Handler` implements the SDK's `auth.OAuthHandler` (`TokenSource`, `Authorize`); `Authorize` returns `ErrLoginRequired`. `buildStreamableTransport` in `internal/mcp/transport.go` sets the transport's `OAuthHandler` when `ServerConfig.Auth == "oauth"`; the token store and auth client reach `internal/mcp` through the existing connect options/host seam (inject, don't construct globals).
**Files:** internal/mcpauth/handler.go; internal/mcpauth/handler_test.go; internal/mcp/transport.go; internal/mcp/client.go; internal/mcp/transport_test.go; cmd/apogee/wire_live.go
**Read first:** internal/mcp/transport.go — buildStreamableTransport, vetEndpoint, headerTransport; internal/mcp/client.go — Host, ConnectWith, connectOne; go-sdk mcp/streamable.go — setMCPHeaders, Write (Authorize retry), Close; go-sdk auth/client.go — OAuthHandler; cmd/apogee/wire_live.go — liveMCPHost, wireSession; cmd/apogee/wire_mcp.go — liveMCP.reconnect, mcpReconnectError; internal/mcp/transport_test.go — headerRecorder, TestConnect_ConfiguredHeadersRideEveryRequest; internal/security/urlscrub.go — OriginRedactor.RedactErr
**Tests:** stored valid token → `Authorization: Bearer` reaches the endpoint origin only; expired → one refresh carrying `resource`, rotated token persisted; refresh after the triggering ctx is cancelled still succeeds; two handlers on one record — the second picks up the first's rotated token from disk instead of refreshing with the spent one; 403 without `insufficient_scope` → nil, no `ErrLoginRequired`; 401 with a zero-expiry token and a refresh token → one forced refresh; nil store on an oauth server → connect error naming it; rejected refresh / missing record → `ErrLoginRequired` with the command in its text.
**Acceptance:** `go build ./... && GOMEMLIMIT=2GiB go test -race -count=1 ./internal/mcpauth/ && GOMEMLIMIT=2GiB go test -race -count=1 ./internal/mcp/`
**Commit:** `feat(mcp): connect to OAuth MCP servers with a refreshed bearer`

## 6. Interactive code fetcher: loopback callback, Enter to open, paste fallback

**What:** Depends on item 4.
**Goal:** a terminal code fetcher in `cmd/apogee` listens on `127.0.0.1:0` at `/callback`, prints the full authorize URL, and accepts whichever arrives first: the callback, or a line on stdin that is a pasted redirect URL carrying `code` and `state`. On a local desktop an empty line (Enter) opens the URL via the platform opener (`xdg-open` / `open` / `rundll32 url.dll,FileProtocolHandler`) and keeps waiting; remote sessions print the paste instruction instead. The wait ends only on ctx cancellation or a result; the listener closes on return.
**Regression guard.** The listener always binds `127.0.0.1:0` with path `/callback`; a preregistered `client-id:` relies on RFC 8252 §7.3 loopback port variance — no fixed-port key (owner, 2026-10-08). Item 6 also updates the cmd/apogee `doc.go` file map the docmap gate checks, for every file it adds.
- The API is bind-first: `newLoginFetcher(...) (redirectURL string, fetch func(ctx context.Context, authURL string) (code, state string, err error), close func())`; the caller defers `close`, so a Login that fails before fetching frees the port.
- The item yields to `internal/security/doc.go` (ResolveProgram is the only exec entry): the opener resolves its program through `security.ResolveProgram(look, prog, workspaceRoot, nil)` as `present.Opener.resolveProgram` does, the site joins that doc's list, it opens only `https` or loopback `http` URLs, and the server's `error`/`error_description` pass through `sanitize.StripEscapes` before printing.
- stdin is read through a cancellable reader (`muesli/cancelreader`, already in go.mod) that the fetcher cancels and joins before returning; the same injected reader serves item 7's y/N prompt.
**Approach (assumed at the header base):** locality from `present.Locality`/`HasDesktop`; the opener is a small helper beside the fetcher, external programs optional (ADR 0042) — a missing opener prints a one-line note. Reader and writer are injected for tests.
**Files:** cmd/apogee/mcp_login_fetch.go; cmd/apogee/mcp_login_fetch_test.go; cmd/apogee/doc.go; internal/security/doc.go; go.mod
**Read first:** internal/present/opener.go — Opener.argv, Opener.resolveProgram; internal/present/detect.go — Locality, HasDesktop; internal/security/execsafety.go — ResolveProgram; cmd/apogee/probeterminal.go — replyPump, newReplyPump; cmd/apogee/settingsedit.go — osOpener, resolveEditor; cmd/apogee/doc.go — file map; internal/sanitize — StripEscapes
**Tests:** callback delivers code/state; pasted URL delivers them; error redirect (`error=`) surfaces the server's error with escape sequences stripped; Enter on desktop invokes the injected opener, remote does not; a non-`https`, non-loopback URL is never opened; ctx cancel returns promptly, joins the cancelled stdin read and frees the port; `close` without a fetch frees the port.
**Acceptance:** `go build ./... && GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestMCPLoginFetch|TestDocMapNamesEveryFile' ./cmd/apogee/`
**Commit:** `feat(cli): complete an MCP OAuth login from the terminal`

## 7. `apogee mcp login|logout <name>` and the startup login

**What:** Depends on items 5–6.
**Goal:** `apogee mcp login <name>` runs item 4's login with item 6's fetcher for a configured `auth: oauth` server and saves the record; `apogee mcp logout <name>` deletes it and says whether one existed; both refuse an unknown name or a server without `auth: oauth`. At startup, when connecting fails with `ErrLoginRequired`: on a TTY stdin, apogee asks on stderr whether to log in now, runs the same login, and retries the connect once; when declined or stdin is not a TTY, launch aborts with the error naming `apogee mcp login <name>`. A mid-session reconnect surfaces `ErrLoginRequired` on the settings row and never prompts.
**Regression guard.** Item 7 updates the cmd/apogee `doc.go` file map for every file it adds.
- Registration goes in `cmd/apogee/subcommands.go` (append `newMCPCommand()` to `subcommands()`, extend its doc comment), not `main.go`.
- The item yields to `cmd/apogee/undo.go` `runUndoVerb` (home through resolveRoots, never a bare `config.ApogeeHome("")`): `mcp login|logout` take `--config` and resolve via `config.ApplyConfig` + `resolveRoots` (the `probe.go` pattern), passing `roots.config`; the startup hook uses `w.roots.config`.
- The auth-client constructor, stdin reader, TTY check and opener are injected as `rootDeps` fields and command-constructor args, never package vars (`TestNoParallelTestSwapsAPackageSeam`).
- Before `ConnectWith`, offer login for each `auth: oauth` server with no stored record; after that, retry per `ErrLoginRequired` (refresh rejected), bounded by the oauth-server count, the server name read from the typed error.
**Approach (assumed at the header base):** a cobra `mcp` command beside `probe` in `cmd/apogee`; the startup hook wraps `mcp.ConnectWith` in `wireSession` (`cmd/apogee/wire_live.go`), before `launch()`; `liveMCP.reconnect` (`cmd/apogee/wire_mcp.go`) is unchanged beyond the error text it already relays.
**Files:** cmd/apogee/mcp_cmd.go; cmd/apogee/mcp_cmd_test.go; cmd/apogee/wire_live.go; cmd/apogee/subcommands.go; cmd/apogee/wire.go; cmd/apogee/doc.go
**Read first:** cmd/apogee/wire_live.go — wireSession, liveMCPHost; cmd/apogee/wire.go — runRootWith, rootDeps, resolveRoots, stateRoots; cmd/apogee/subcommands.go — subcommands; cmd/apogee/probe.go — probeHostCommand; cmd/apogee/undo.go — runUndoVerb; cmd/apogee/wire_mcp.go — liveMCP.reconnect, mcpReconnectFailed; internal/mcp/client.go — ConnectWith, connectOne; cmd/apogee/seams_guard_test.go — TestNoParallelTestSwapsAPackageSeam
**Tests:** login/logout against an `httptest` OAuth server with a scripted fetcher and an injected loopback-capable auth client; unknown name and non-oauth server refused; `--config` / `APOGEE_CONFIG` home honoured by login and the startup lookup; startup with no record + non-TTY → abort text contains `apogee mcp login <name>`; startup with a scripted "yes" → login then connect succeeds; two oauth servers with no record → both offered before the connect; `TestStartupMCPLoginReconnectNeverPrompts` — `liveMCP.reconnect` with no record returns the `mcpReconnectFailed` text naming `apogee mcp login <name>` and never reads the reader.
**Acceptance:** `go build ./... && GOMEMLIMIT=2GiB go test -race -count=1 -run 'TestMCPCmd|TestStartupMCPLogin|TestDocMapNamesEveryFile|TestSubcommands' ./cmd/apogee/ && go run ./cmd/apogee mcp --help`
**Commit:** `feat(cli): add apogee mcp login and logout`

## 8. Docs: manual, CONTEXT.md, MCP client contract

**What:** Depends on item 7.
**Goal:** `docs/manual/configuration.md` ("External MCP servers") documents `auth: oauth`, `client-id:`, `client-secret-env:`, the token file location and the SSH paste fallback; the same section documents `apogee mcp login|logout`; `CONTEXT.md` ("MCP client") and `docs/design/mcp-client.md` state that the bearer reaches only the endpoint origin and the auth server runs under the floor. Every doc line claiming MCP servers are headers-only, or that apogee stores no credential, is corrected (grep `-i 'oauth\|stores nothing\|headers-env'` across `docs/`, `README.md`, `CONTEXT.md`).
**Regression guard.** The manual states that a preregistered client must be registered with redirect URI `http://127.0.0.1/callback` and loopback port variance (RFC 8252 §7.3).
- The correction rule covers every prose enumeration of MCP auth means or of the secret-env scrub's sources: configuration.md's scrub-union paragraph gains `client-secret-env:`, and the seeded `internal/config/defaults/config.yaml` `mcp-servers:` block is corrected; grep also `scrub\|MCPHeaderEnvNames`.
- The sweep excludes `docs/adr`, `docs/plans`, `docs/reviews`, `docs/handoffs` and `docs/design/archived`; ADR 0047 is item 1's.
- `apogee mcp login|logout` is documented in configuration.md's "External MCP servers" section, not `docs/manual/commands.md` (the in-chat commands page).
**Files:** docs/manual/configuration.md; CONTEXT.md; docs/design/mcp-client.md; README.md; internal/config/defaults/config.yaml
**Read first:** docs/manual/configuration.md — "External MCP servers — `mcp-servers:`", "Headers for an http server", the scrub-union paragraph; internal/config/defaults/config.yaml — mcp-servers comment block; CONTEXT.md — MCP client entry (headers paragraph); docs/design/mcp-client.md — headers bullets; cmd/apogee/docs_env_test.go — TestDocsEnvURLSafetyProseIsLiveAndCoversMCP, TestManualListsEveryEnvironmentOverride; cmd/apogee/docs_settings_test.go — TestManualDocumentsEverySettingsKey
**Tests:** none (docs); the existing doc-drift tests must stay green, including the cmd/apogee ones that read configuration.md.
**Acceptance:** `grep -q 'auth: oauth' docs/manual/configuration.md && grep -q 'apogee mcp login' docs/manual/configuration.md && grep -q '127.0.0.1/callback' docs/manual/configuration.md && GOMEMLIMIT=2GiB go test -count=1 -run 'TestReadme|TestManual' ./internal/tools/ && GOMEMLIMIT=2GiB go test -count=1 -run 'TestReadme' ./internal/config/ && GOMEMLIMIT=2GiB go test -count=1 -run 'TestManual|TestDocsEnvURLSafety' ./cmd/apogee/`
**Commit:** `docs(mcp): document OAuth for HTTP MCP servers`
