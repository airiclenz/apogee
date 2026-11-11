---
title: A preregistered MCP client secret goes to whatever authorization server the MCP server names
status: open
type: bug
priority: 3
labels: [security]
plan: 2026-11-11 - 00 - pdf-parser-fork-and-defects-plan
created: 2026-11-10T19:34:27Z
---

Source: docs/reviews/code-audit-2026-11-10.md (Medium, "A hostile MCP server can receive the preregistered client secret"). Verified PARTLY 2026-11-10 at a5c4f2db: mechanism confirmed, severity lowered to Low-Medium by the trust model.

**Where:** internal/mcpauth/login.go `loginFlow.run` (metadata from `prm.AuthorizationServers[0]`), `resolveClient`, `authorize` (code exchange to `asm.TokenEndpoint`); internal/mcpauth/handler.go refresh (secret to the stored token endpoint).

**What:** when an `auth: oauth` server sets `client-id:` and `client-secret-env:`, the secret is sent to the token endpoint of whichever authorization server the MCP server's protected-resource metadata names. Nothing ties that authorization server to the one the client was preregistered with.

**Why it matters:** ADR 0095 D9 declares the authorization server untrusted but only bounds its SSRF reach and keeps the bearer on the endpoint's origin; it says nothing about the client secret. A confidential client's secret is a credential the operator brings for one specific IdP, and an MCP server that turns hostile or is compromised can name its own AS and collect it at login and every refresh. Limits: the endpoint is operator-configured (or, for a repo server, adopted through a pane naming the variable, ADR 0104 D9/D11), and abusing the secret also needs a user authorization at the real IdP.

**Suggested fix:** when a client secret is configured, require the authorization server's issuer (or token endpoint) to match an operator-configured value, e.g. an `issuer:` key required beside `client-secret-env:`, and refuse to send the secret otherwise. Record it as a dated amendment to ADR 0095 D9 and update docs/design/mcp-client.md.

## Acceptance

- A login whose protected-resource metadata names an authorization server other than the configured issuer never sends the configured client secret; it fails naming the mismatch.
- A refresh never sends the secret to a token endpoint other than the one the pinned issuer's metadata gave.
- A public client (no secret) and Dynamic Client Registration behave as before.
- ADR 0095 carries a dated amendment covering the client secret, and the manual documents the new key.
