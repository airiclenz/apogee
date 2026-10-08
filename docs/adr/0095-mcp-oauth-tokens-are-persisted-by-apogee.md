---
Status: accepted
Amends: ADR 0047 (decision point 8, "apogee … stores nothing itself" — it now stores MCP OAuth tokens and client registrations it minted itself, and nothing else)
---

# MCP OAuth tokens are persisted by apogee

## Context

A `streamable-http` MCP server can sit behind OAuth 2.1, as the MCP authorization spec
(2025-06-18 / 2025-11-25) describes. The server answers an unauthenticated request with a 401 that
points at its protected-resource metadata (RFC 9728). That metadata names an authorization server
(RFC 8414). The client registers itself there (RFC 7591), runs an authorization-code login with
PKCE in the user's browser, and sends the token it gets back as a bearer on every request, bound to
the MCP server by the `resource` parameter (RFC 8707). Until now apogee could reach such a server
only by pasting a token into `headers:` / `headers-env:`, and that token expires within the hour.
Bead `apogee-rw6` asks for the real flow. The owner ratified its shape on 2026-10-08 (plan
`docs/plans/2026-10-08 - 02 - mcp-oauth-plan.md`).

Two facts force the question this ADR answers: where the token lives between runs.

- **The SDK keeps tokens in memory only.** `github.com/modelcontextprotocol/go-sdk` v1.6.1 has the
  pieces (`auth.AuthorizationCodeHandler`, `oauthex` discovery and registration), but the handler
  holds its `oauth2.TokenSource` in a struct field and offers no hook to save or load one. Used as
  is, every start of apogee would be a new browser login, and so would every reconnect.
- **The keystore route of ADR 0047 does not reach every host.** ADR 0047 point 8 says apogee
  "stores nothing itself" and leaves secrets to the user's own store, reached through its CLI. That
  works for an API key the user already has and can name with `api-key-cmd:`. An OAuth token is
  different: apogee mints it, it changes on every refresh (refresh tokens rotate), and it must be
  written back each time. Windows has no generic-secret CLI, and a headless Linux box has no secret
  service, which are exactly the hosts ADR 0047 already sends to the notice. A token that could only
  live in a keystore would leave OAuth broken on the machines small models run on.

So apogee must keep these tokens itself, and the question is how narrowly.

## Decision

**apogee persists the OAuth tokens and client registrations it obtains for MCP servers, one 0600
file per server under `~/.apogee/mcp-auth/`. It stores no other secret. Login is an explicit,
interactive act outside the TUI, and nothing mid-session ever prompts.**

**D1 — The token store.** One JSON file per server under `~/.apogee/mcp-auth/`. The directory is
0700 and each file is 0600. A file is written atomically (temp file, then rename), so a crash never
leaves half a token. The record is keyed by the server's name **and** its normalised endpoint: a
record whose endpoint does not match the configured one is a miss, so editing a server's `endpoint`
never sends an old token to a new place. The record carries what a later refresh needs on its own:
the token, its refresh token, the token endpoint and the `resource` it was issued for, and the
client registration (D3).

**D2 — Opt-in, on `streamable-http` only.** A server uses OAuth only when its entry says
`auth: oauth`. The key is legal on `streamable-http` servers and refused on `sse` and `stdio`. An
`Authorization` header in `headers:` or `headers-env:` is refused beside it, since two sources for
one header would leave one of them silently ignored (ADR 0047 point 1's reasoning). A server
without the key behaves exactly as today.

**D3 — Client identity by Dynamic Client Registration.** apogee registers itself with the
authorization server (RFC 7591) and persists the registration with the token. A user whose
authorization server needs a preregistered client sets `client-id:` and, for a confidential
client, `client-secret-env:` (the NAME of an environment variable, never the secret in the file).
Client ID Metadata Documents are not supported.

**D4 — Login runs at startup or by command, never mid-session.** When a server with `auth: oauth`
has no usable token at startup, apogee runs the login on stderr before the TUI opens. The same
login is `apogee mcp login <name>`. Mid-session there is no prompt: a reconnect or a refresh that
fails becomes an error naming `apogee mcp login <name>`. The engine never sees any of this
([ADR 0031](0031-the-local-platform-north-star-binds-every-future-layer-to-the-embeddable-engine.md)):
all login I/O lives in `cmd/apogee`, and the engine still receives a connected MCP client.

**D5 — The browser opens only on a keypress.** The full authorize URL is always printed. On a local
desktop, Enter opens it in the browser; nothing opens without that key. Opening it is an optional
external program in [ADR 0042](0042-external-programs-are-optional-enhancements-never-prerequisites.md)'s
sense: where none is available, the printed URL is enough.

**D6 — Remote hosts paste the redirect.** The callback listener always runs on `127.0.0.1:0`. Over
SSH the browser's redirect cannot reach it, so the prompt also accepts the pasted final redirect URL.
Whichever arrives first wins. A preregistered client relies on RFC 8252 §7.3 port variance for its
loopback redirect; there is no fixed-port key.

**D7 — Declined or no TTY at startup aborts the launch.** A server that needs a login, when the
user declines it or there is no terminal to ask on, stops the launch with "run
`apogee mcp login <name>`". The all-or-nothing connect at startup is kept: apogee never starts with
an OAuth server silently missing.

**D8 — Logout is local.** `apogee mcp logout <name>` deletes the server's token file and makes no
network call. Revocation (RFC 7009) is out of scope.

**D9 — The authorization server is untrusted.** Its metadata, registration and token endpoints are
reached by an HTTP client under the resolved-IP SSRF floor with bounded bodies. It never gets the
MCP endpoint's config-file pin exemption (`docs/design/mcp-client.md`). The bearer token reaches
only the configured endpoint's origin, under the origin pin that already guards every MCP request.

## Considered options

- **Keep tokens in memory, as the SDK does.** Rejected: a browser login on every start and every
  reconnect, which no one keeps using.
- **Store tokens in the OS keystore (ADR 0047's route).** Rejected: no store on Windows or headless
  Linux, so OAuth would fail on the hosts this project targets most. A token that rotates on every
  refresh would also mean a keystore write per refresh, through a CLI.
- **Store tokens in `config.yaml`.** Rejected for ADR 0047's reasons: that file is hand-edited,
  watched, copied between machines and pasted into bug reports.
- **Enable OAuth on any server that answers 401.** Rejected: a login nobody configured, triggered
  by a server's reply. `auth: oauth` keeps it a deliberate choice in the file.
- **Prompt for login mid-session.** Rejected: the TUI owns the terminal, a login pane is new UI for
  a rare event, and a refresh failing during a turn is the worst moment to ask.
- **A fixed callback port for preregistered clients.** Rejected: a fixed port collides and needs a
  config key. RFC 8252 §7.3 already requires authorization servers to accept any loopback port.

## Consequences

- **ADR 0047 point 8 is amended, narrowly.** apogee now stores secrets it minted itself: MCP OAuth
  tokens and the client registrations that go with them. Every secret the user brings (API keys,
  `headers-env:` values, `client-secret-env:`) still comes from the user's own source, and apogee
  still stores none of them.
- **A new directory under `~/.apogee`.** `~/.apogee/mcp-auth/` holds bearer and refresh tokens in
  plain files, protected by file mode, like `~/.ssh` or a cloud CLI's credentials. It sits inside the
  control plane that the `write-apogee-control-plane` rule (`internal/security/rules.go`) already
  guards: a terminal command that writes or deletes there needs approval in every mode, so the
  model cannot quietly change or remove a token.
- **A rotating refresh token is written back on every refresh.** Two apogee processes sharing a
  server must not lose a rotation; the store's atomic write and re-read on refresh carry that.
- **New user surface.** `auth:`, `client-id:` and `client-secret-env:` on `mcp-servers:` entries, the
  `apogee mcp login|logout <name>` subcommands, and a startup login on stderr. The manual, the MCP
  client design contract and `CONTEXT.md` describe them.
- **Headless and daemon runs are unaffected.** They never contact MCP.
