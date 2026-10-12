---
title: "MCP client (Model Context Protocol): apogee's client for external MCP servers"
---

**MCP client** (Model Context Protocol):
Apogee's client for external **MCP** servers, on the official Go SDK over **stdio / SSE /
streamable-http** (`internal/mcp`, P3.15). It connects the servers a host lists in `config.yaml`'s
`mcp-servers:` block (config-file-only, default-empty ⇒ dormant), discovers each server's tools, and
surfaces them into the registry as `ExternalEffectTool` of kind **`mcp`** named `<server>__<tool>`.
An MCP server is an **external, untrusted** process Apogee **cannot confine**, so its tools always
**gate through Approval in Auto** under `confine-to-workspace=true` (the per-tool teeth above) — and
their description / schema / result are untrusted data shown to the model, never executed. An http(s)
server (sse / streamable-http) passes **url-safety**'s scheme/host allow-deny and dials **pinned to
the configured endpoint's own addresses** — the endpoint is exempt from the resolved-IP SSRF floor
(you named it in your own config; the floor is the anti-*model* control), while any *other* private
address that connection is pointed at is still refused, redirects are not followed and the
connection goes out through the configured egress proxy when one applies (ADR 0012,
Amendment 2026-07-26); a stdio server is a trusted local launch (no URL check), its calls still gate.
An http(s) server's entry may add request headers sent on every request — the server's own auth
scheme, a tenant id: `headers:` maps a header name to a literal value, and `headers-env:` maps one
to the **name** of an environment variable holding the value, so a token never sits in the config
file (and that variable is withheld from the subprocess tools' environment, beside the API-key
ones). Validation refuses a name that is not an HTTP token, one the transport or the MCP protocol
sets itself (`Host`, `Content-Length`, `Content-Type`, `Accept`, `Connection`,
`Transfer-Encoding`, `Last-Event-ID`, every `Mcp-*`), one name configured twice, in either key or
across both (names are case-insensitive), a literal value carrying CR, LF or NUL, and a blank variable name; the refusal
names the key and the header, never a value. A stdio entry that sets either key draws a note — it
has no HTTP request to carry them. A **streamable-http** entry may instead set `auth: oauth`
(optionally `client-id:` and `client-secret-env:` for a client registered ahead of time; without
them apogee registers its own by Dynamic Client Registration): apogee then logs in by an
interactive browser login (`apogee mcp login <name>`, or the offer on stderr at startup before the
TUI), persists the OAuth 2.1 bearer token under `~/.apogee/mcp-auth/<name>.json`, refreshes it on its
own, and sends it on every request. The **bearer reaches only the endpoint's origin** — it rides
the same origin-pinned client as the headers — while the **authorization server**, named by the MCP
server rather than by you, runs **under the SSRF floor** and url-safety with bounded bodies: the
endpoint's floor exemption never extends to it. `auth:` on stdio or sse, or beside an
`Authorization` header, is a refusal rather than a note. Mid-session nothing prompts: a reconnect
that needs a login fails naming `apogee mcp login <name>` (ADR
0095). **Resume reconnects
fresh** — no server-side state is restored (ADR 0008). The *client shape* is
[docs/design/mcp-client.md](../../../../docs/design/mcp-client.md); the *gating* is ADR 0004/0008/0012.
_Avoid_: "MCP plugin", "MCP proxy" (it is a client; there is no proxy).
