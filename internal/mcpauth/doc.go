// Package mcpauth persists the OAuth state apogee mints for a `streamable-http` MCP server that
// says `auth: oauth` (ADR 0095): the access and refresh tokens, what a later refresh needs without
// re-discovery (the authorization server's issuer, its token endpoint, the RFC 8707 resource and
// the granted scopes), and the client registration the token was issued to.
//
// One JSON file per server lives at <apogee home>/mcp-auth/<name>.json. The caller supplies the
// apogee home (cmd/apogee passes config.ApogeeHome's answer, the home config.yaml lives in); the
// Store never reaches for an ambient ~/.apogee itself (ADR 0001). It does no network I/O and knows
// nothing of the login flow: it loads, saves and deletes records.
//
// Login (login.go) mints a record: it probes the endpoint, discovers the authorization server
// (RFC 9728, RFC 8414), registers a client (RFC 7591) unless one is configured, runs the
// authorization code flow with PKCE and the RFC 8707 resource, and saves the result. Its user
// interaction, the endpoint's vetted client and the home all arrive as arguments: the package
// imports neither internal/mcp nor internal/config. NewAuthClient (client.go) is the floor-guarded,
// body-bounded client for authorization servers, which are untrusted.
//
// Handler (handler.go) is the SDK's auth.OAuthHandler a connected server's transport carries: it
// sends the stored bearer, refreshes it at the record's stored token endpoint with the stored
// resource (never re-discovery) on a context that outlives the triggering request, and saves a
// rotated refresh token before using the new access token. Handlers sharing a record — in this
// process or another — refresh under a lock file beside it and adopt a newer token one of them
// saved. It never prompts: a server that needs a login gets ErrLoginRequired, whose text names
// `apogee mcp login <name>`.
//
// Invariants:
//   - Directory 0700, files 0600: the files hold bearer and refresh tokens (ADR 0095 D1).
//   - A save replaces the file through a temp file and a rename, so a crash never leaves half a
//     token and a reader in another process sees the old record or the new one, never a mix.
//   - A record is keyed by the server name AND the normalised endpoint it was minted for: a record
//     whose stored endpoint differs from the configured one loads as a miss, so an edited
//     `endpoint:` never sends an old token to a new place.
//   - A server name that is not a safe file stem (ValidateServerName) is refused before any path
//     is built from it. ValidateServerName is the one home of that rule; the login, the connected
//     handler and the `apogee mcp` subcommands consume it and never re-implement it.
package mcpauth
