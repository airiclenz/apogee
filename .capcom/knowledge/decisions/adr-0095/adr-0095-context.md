---
title: ADR 0095 context
---

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
