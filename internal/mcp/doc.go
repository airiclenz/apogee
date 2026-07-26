// Package mcp is Apogee's Model Context Protocol client, built on the official Go
// SDK (github.com/modelcontextprotocol/go-sdk) over stdio / SSE / streamable-http.
// It connects to the external MCP servers a host configures, discovers the tools
// each advertises, and surfaces them into a domain.ToolRegistry as
// domain.ExternalEffectTool of kind mcp — so the agent's blast-radius disposition
// gates them through Approval in Auto under confine-to-workspace=true for free
// (ADR 0012 D3; the gating decision is settled by ADR 0004/0008, this package
// records only the CLIENT shape).
//
// # Trust boundary
//
// An MCP server is an EXTERNAL, untrusted process or endpoint that Apogee cannot
// confine: its advertised tools execute on the server side, outside any OS fence.
// Every tool description, schema, and result this package surfaces is therefore
// UNTRUSTED input — it is passed to the model and rendered, never executed or
// interpreted as a command by Apogee. Two consequences shape the design:
//
//   - MCP tools are non-forkable external effects (ADR 0008): they carry the mcp
//     effect kind, so the dispatch disposition gates them through Approval in Auto
//     (server-grain allow-for-session), and they route through Config.ExternalEffects
//     when the host injects a stub (the bench's deterministic, process-free swap).
//   - A network-transported server (SSE / streamable-http) rides the host's
//     security.URLGuard scheme/host allow-deny before connecting, and dials PINNED to
//     the configured endpoint's own resolved addresses: those pass at dial time, and
//     every other address still meets the resolved-IP SSRF floor (DNS-rebinding
//     closed), so a rebind or a redirect pointing that connection at a DIFFERENT
//     private address is refused. The endpoint itself is deliberately EXEMPT from the
//     floor — it is config-file-only and never model-supplied, so a localhost / LAN
//     MCP server is an ordinary, supported configuration (ADR 0012, Amendment
//     (2026-07-26)); the floor stays blanket over everything the model drives.
//     Redirects are not followed, the same policy the native network tools apply, so a
//     server that redirects must be configured at its final URL. A stdio server is a
//     LOCAL launched subprocess — a different trust model (the host chose the
//     command), so no URL check applies, but the launched tool calls still gate
//     through Approval exactly the same way.
//
// # Lifecycle
//
// Connect dials every configured server, lists its tools, and returns a Client that
// owns the live sessions; Tools surfaces the discovered tools for registration; Close
// tears down every session (no orphaned process or connection). Resume reconnects
// FRESH — no server-side state is restored (ADR 0008). With zero configured servers
// the feature is dormant: Connect returns a Client that surfaces no tools and whose
// Close is a no-op (never an error — a host without MCP pays nothing).
package mcp
