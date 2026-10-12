---
title: ADR 0095 decision
---

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
(ADR 0031):
all login I/O lives in `cmd/apogee`, and the engine still receives a connected MCP client.

**D5 — The browser opens only on a keypress.** The full authorize URL is always printed. On a local
desktop, Enter opens it in the browser; nothing opens without that key. Opening it is an optional
external program in ADR 0042's
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
