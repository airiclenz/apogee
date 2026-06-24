# Apogee — MCP Client Shape (the P3.15 design note)

**Date:** 2026-06-24 · **Status:** ✅ **Accepted** (the P3.15 design deliverable) · **Owner ADRs:**
[ADR 0004](../adr/0004-auto-mode-requires-os-level-confinement.md) /
[ADR 0008](../adr/0008-stateless-tools-and-non-forkable-external-effects.md) /
[ADR 0012](../adr/0012-confinement-attaches-to-blast-radius-and-confine-to-workspace-flag.md) ·
**Realised by:** P3.15 (`internal/mcp`, the `cmd/apogee` composition wiring).

> **Why a design note, not ADR 0014.** Phase-3 plan §3 D3 left the form a P3.15 judgement: "the
> *decision* (MCP = ExternalEffect ⇒ Approval-gated) is already settled by ADRs 0004/0008; P3.15
> records the *client* shape." There is **no new policy** to ratify here — the gating, the
> statelessness, and the blast-radius classification all pre-exist. This note records the *client
> shape* the existing policy is realised through, so it is a design note (like the
> confinement-execution-contract), not a fresh ADR.

---

## 1. What the client is

`internal/mcp` is Apogee's Model Context Protocol client, built on the official Go SDK
(`github.com/modelcontextprotocol/go-sdk` **v1.6.1**, pinned per P3.0) over **stdio / SSE /
streamable-http**. It connects to the external MCP servers a host configures, discovers the tools
each advertises, and surfaces them into a `domain.ToolRegistry` as `domain.ExternalEffectTool` of
kind **`mcp`**. The agent's existing blast-radius disposition (D5) then gates each MCP tool through
Approval in Auto under `confine-to-workspace=true` **for free** — surfacing them with the right
effect kind is the entire integration; no dispatch change was needed.

## 2. The trust boundary (the load-bearing constraint)

An MCP server is an **external, untrusted** process or endpoint Apogee **cannot confine**: its
tools execute on the server side, outside any OS fence. Two consequences shape the design:

- **MCP tools are non-forkable external effects (ADR 0008).** Each surfaced tool carries the `mcp`
  effect kind, so the disposition classifies it `classMCP` and gates it through Approval in Auto
  (server-grain "allow for session"), and it routes through `Config.ExternalEffects` when the host
  injects a stub (the bench's deterministic, process-free swap). This is **distinct from `network`-
  kind** tools, which auto-run url-filtered — MCP is unfenceable, so it asks.
- **A network-transported server rides the SSRF floor.** An SSE / streamable-http server's endpoint
  passes the same default-on, resolved-IP `security.URLGuard` floor the native network tools use:
  the URL is checked before connecting and the connected IP is re-validated at dial time (DNS-
  rebinding closed), so a server URL resolving to loopback / IMDS / a private range is refused. A
  **stdio** server is a local launched subprocess — the host chose the command, a different trust
  model — so no URL floor applies; its tool calls still gate through Approval in Auto exactly the same.

Every tool **description, schema, and result** the client surfaces is untrusted input: it is passed
to the model and rendered, **never executed or interpreted** as a command by Apogee.

## 3. The lifecycle (the design surface §3 D3)

```
Connect(ctx, []ServerConfig, URLGuard) → *Client     // dial every server, list its tools
  Client.Tools() []domain.Tool                        // the surfaced tools, for registration
  Client.Close() error                                // tear every session down — no orphan
```

- **Connect** is **all-or-nothing**: a later server's failure tears down every already-opened
  session and returns the error, so a half-wired MCP set never reaches the registry and no orphaned
  stdio process leaks. Zero configs returns a **dormant** Client (no sessions, no tools, a no-op
  Close) — a host without MCP pays nothing. Server names must be non-empty and unique (the name
  prefixes each surfaced tool's registry key as `mcp__…` — actually `<name>__<tool>`, see §4).
- **Tool naming** qualifies each server tool as `<server-name>__<tool>` so two servers advertising
  the same tool name never collide in the single flat registry, and the human approving a call sees
  which server it reaches.
- **Resume reconnects FRESH (ADR 0008).** The Client holds no serializable state; a resumed Session
  simply calls `Connect` again from the same config. No server-side state is restored — there is no
  server-side-state promise. (`cmd/apogee/wire.go` establishes the connection on every launch,
  resume included.)
- **Close** joins every session's teardown error and clears the sessions; it is safe on a dormant or
  already-closed Client. The composition root `defer`s it so no process or connection survives exit.

## 4. Where it plugs in

- `internal/mcp` depends only on the SDK, `internal/domain`, and `internal/security` — it never
  imports the root facade (ADR 0010). It exports `Client`, `Connect`, `ServerConfig`, `Transport`.
- `cmd/apogee` owns the wiring: `config.yaml`'s `mcp-servers:` block (config-file-only, default-
  empty) → `mcp.ServerConfig` values → `mcp.Connect` → `registryWithMCP` registers the discovered
  tools on top of the default registry → `Config.Tools`. A discovered tool whose qualified name
  collides with a built-in is dropped with a stderr notice (the built-in wins).
- The disposition's `classMCP` gating is proven in `internal/agent/dispatch_test.go`; this package's
  tests prove a **real** surfaced tool reports `EffectMCP` (the property the gate keys on) and
  exercise the live stdio path end to end (a fork-and-exec fixture server).

## 5. Acceptance (P3.15) — how each criterion is met

| Criterion | Mechanism |
|---|---|
| A hermetic stdio server exposes a tool that appears in the menu, is callable | `TestConnect_SurfacesServerToolsAndCalls` over a fork-and-exec stdio fixture |
| Raises Approval in Auto (asserted) | `EffectMCP` ⇒ `classMCP` ⇒ `dispoGate` (disposition table, `dispatch_test.go`); the real tool's kind asserted in `TestServerTool_IsMCPExternalEffect` |
| A resumed session re-establishes from scratch | `TestResume_ReconnectsFresh` (Close, then a fresh Connect rediscovers the tools) |
| The bench swaps a deterministic stub with no process | the `mcp`-kind tool routes through `Config.ExternalEffects.Do` (ADR 0008; `dispatch_test.go`) |
| `Close` tears down cleanly (no orphan) | all-or-nothing Connect rollback + `TestClose_TearsDownSessions` |
| Cross-build green (the SDK is pure-Go) | the 6 CGO_ENABLED=0 cross-builds pass |
