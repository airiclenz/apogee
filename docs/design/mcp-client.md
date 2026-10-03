# Apogee — MCP Client Shape (the P3.15 design note)

**Date:** 2026-06-24 · **Status:** ✅ **Accepted** (the P3.15 design deliverable) · **Owner ADRs:**
[ADR 0008](../adr/0008-stateless-tools-and-non-forkable-external-effects.md) /
[ADR 0012](../adr/0012-confinement-attaches-to-blast-radius-and-confine-to-workspace-flag.md) ·
**Realised by:** P3.15 (`internal/mcp`, the `cmd/apogee` composition wiring).

> **Why a design note, not ADR 0014.** Phase-3 plan §3 D3 left the form a P3.15 judgement: "the
> *decision* (MCP = ExternalEffect ⇒ Approval-gated) is already settled by ADRs 0004/0008; P3.15
> records the *client* shape." There is **no new policy** to ratify here — the gating, the
> statelessness, and the blast-radius classification all pre-exist. This note records the *client
> shape* the existing policy is realised through, so it is a design note (like the
> confinement-execution-contract), not a fresh ADR. (The quoted ADR 0004 has since been superseded
> by ADR 0012, which now owns the confinement side.)

---

## 1. What the client is

`internal/mcp` is Apogee's Model Context Protocol client, built on the official Go SDK
(`github.com/modelcontextprotocol/go-sdk` **v1.6.1**, pinned per P3.0) over **stdio / SSE /
streamable-http**. It connects to the external MCP servers a host configures, discovers the tools
each advertises, and surfaces them into a `domain.ToolRegistry` as `domain.ExternalEffectTool` of
kind **`mcp`**. The agent's existing blast-radius Resolution (D5) then gates each MCP tool through
Approval in Auto under `confine-to-workspace=true` **for free** — surfacing them with the right
effect kind is the entire integration; no dispatch change was needed.

## 2. The trust boundary (the load-bearing constraint)

An MCP server is an **external, untrusted** process or endpoint Apogee **cannot confine**: its
tools execute on the server side, outside any OS fence. Two consequences shape the design:

- **MCP tools are non-forkable external effects (ADR 0008).** Each surfaced tool carries the `mcp`
  effect kind, so the disposition classifies it `tools.ClassMCP` and gates it through Approval in Auto
  (server-grain "allow for session"), and it routes through `Config.ExternalEffects` when the host
  injects a stub (the bench's deterministic, process-free swap). This is **distinct from `network`-
  kind** tools, which auto-run url-filtered — MCP is unfenceable, so it asks.
- **A network-transported server is url-safety-checked and dial-pinned.** An SSE / streamable-http
  server's endpoint passes `security.URLGuard`'s scheme/host allow-deny before connecting, and the
  connection dials under a control **pinned** to that endpoint's own resolved addresses — and, when
  the process's `HTTP_PROXY` / `HTTPS_PROXY` carries that endpoint, to the **proxy's** addresses too,
  since a proxied transport dials the proxy rather than the destination: those pass,
  every other address still meets the resolved-IP SSRF floor (DNS-rebinding closed), and redirects
  are not followed. The endpoint itself is **exempt from the floor** — it is config-file-only and
  never model-supplied, so a localhost / LAN server is a supported configuration rather than a fatal
  startup error, while a rebind or a redirect to a *different* private address stays refused
  ([ADR 0012](../adr/0012-confinement-attaches-to-blast-radius-and-confine-to-workspace-flag.md),
  Amendment 2026-07-26). Above the dial pin, **every request is pinned to the endpoint's origin**
  (2026-09-29): `security.OriginPinTransport`, the guarded client's outermost RoundTripper, forwards only a
  request whose scheme + host + port (host in `security.NormalizeURL`'s form, default ports
  canonicalised) equals the vetted endpoint's, and refuses any other with an error wrapping
  `security.ErrURLBlocked` that names the server. The SDK resolves an SSE `endpoint` event with no
  origin check and the dial pin judges IPs only — another port or virtual host on the endpoint's
  own address, or any target behind an egress proxy, would pass it — so an `endpoint` event naming
  another origin now fails the connect and no request reaches that origin. The client itself comes
  from `security.URLGuard.GuardedClient` — the one guarded-client recipe the native network tools
  use too — under the `DialPinDestination` floor policy, with the origin pin outermost and
  `boundedBodyTransport` beneath it, no client timeout (the connection is session-long), and this
  package's refusal sentences unchanged (2026-10-01). The endpoint never
  reaches surfaced error text whole: a connect, list-tools
  or call-failed error that quotes it (the SDK's `Post "https://host/mcp?token=…": …`) is cut to the
  bare `scheme://host[:port]` first, so userinfo, path and query stay out of the model's context and
  the startup error. A **stdio** server is a local launched subprocess — the host chose the
  command, a different trust model — so no URL check applies; its tool calls still gate through
  Approval in Auto exactly the same.
- **A stdio server is a fenced absolute program held as a process tree** (2026-08-26). Its
  `Command` is resolved on PATH through the exec fence (`security.ResolveProgram`), so what is
  launched is an absolute path and a program resolving **inside the workspace** — bytes a confined
  call is allowed to write — is refused at connect time rather than executed; `Connect` being
  all-or-nothing, the operator meets that refusal at startup. It still **starts in the workspace**
  (apogee's own working directory, which filesystem-style servers expect): with argv[0] absolute
  and fenced there is no relative lookup left for the working directory to decide. Its environment
  is unchanged by default — the full process environment plus `cfg.Env`, the deliberate trust
  decision above — with `EnvAllowlist` (`env-allowlist:`) as the per-server opt-in for a
  less-trusted server: named non-nil, the launch inherits only those keys plus the platform's
  essentials, PATH scoped away from the workspace as `gitexec.SafeEnv` scopes git's, and `cfg.Env` is
  appended last either way. The launched process is held in a **process group** (POSIX) / **Job Object**
  (Windows) via `platform.NewProcessTeardown`. The process joins it inside `stdioTransport.Connect`
  immediately after `cmd.Start`, before the handshake sends a byte (2026-09-27): on Windows a
  descendant spawned before the Job Object assignment escapes the job, so the window is the
  sub-millisecond one the tools funnel has, never the whole initialize round-trip, and a handshake
  that fails is reaped as a contained tree. `Close` reaps that container after the session's
  own shutdown, so a descendant the server spawned cannot outlive the session — apogee's
  spec-shaped shutdown ladder (`stdinLadder.Close`) signals the leader alone. That `Cmd` carries a
  **session-scoped cancellable context** (never the connect ctx, which would kill every server the
  moment `Connect` returned) that `Close` cancels once that ladder is spent: it is what arms `cmd.Cancel` and
  `cmd.WaitDelay`, so a server that outlives the ladder is killed as a group and the drain after it
  is bounded by `platform.ProcessWaitDelay` rather than left open-ended.
- **A stdio server's output is read through a 4 MiB message-bounded reader** (2026-09-21; cumulative
  per message since 2026-09-29). The launch rides apogee's own `stdioTransport` rather than the
  SDK's `CommandTransport`: the same start-free shape and the same shutdown ladder
  (`stdinLadder.Close` re-implements the SDK's close-stdin → wait → SIGTERM → wait → SIGKILL → wait
  order), but the server's stdout is wrapped in a `lineBoundedReader` under the SDK's `IOTransport`.
  One message may not exceed `maxMCPMessageBytes` (4 MiB) in total, however many lines it spans: the
  count is cumulative and resets only when a message completes — a newline at JSON depth 0 outside a
  string, since the SDK's `json.Decoder` accepts newlines inside a value. A message past the cap
  fails the read with `apogee: mcp message exceeds the 4 MiB limit`, the SDK retires the in-flight
  call with that error — the model sees an error result naming it, never the oversize text — and the
  session is dead from then on (a following call fails too; `Close` still runs the ladder and reaps
  the tree). The reader knows nothing of MCP, so an HTTP body is bounded with the same type.
- **An HTTP server's bodies are bounded the same way; a result and a tool list are capped after
  decode** (2026-09-21). The guarded `http.Client` both HTTP transports speak over wraps its
  `http.Transport` in `boundedBodyTransport`, a RoundTripper that replaces every `resp.Body` with a
  `boundedBody` — the same `lineBoundedReader` over the real body, whose `Close` closes the real
  body (the SDK closes bodies itself, so a `NopCloser` would leak every connection). The bound is
  cumulative per message, never per line (2026-09-29), and the framing comes from the base media
  type of the response's `Content-Type` (`mime.ParseMediaType`, as the SDK's `baseMediaType`
  reads it): a `text/event-stream` body is bounded per event, the count resetting at each blank
  line (`\n` or `\r\n`), so a long-lived SSE stream is never cut for carrying many events, only
  for one event past 4 MiB; any other body — a streamable `application/json` reply above all — is
  one message, and its read errors with the same `errMCPMessageTooLarge` (never a silent
  truncation) once the whole body passes the cap. The HTTP-lane outcome is not stdio's
  dead connection: the body read errors; a plain JSON reply fails its call, and a streamable SSE
  reply stalls the call until its ctx, the SDK's retry budget or the 5-minute per-call deadline
  ends it. Above the transport, three post-decode
  caps: `renderContent` clips a flattened result at `maxMCPResultBytes` (2 MiB) and appends
  `[mcp result truncated at 2097152 bytes]`; `newServerTool` clips a tool's advertised description
  at `maxMCPToolDescriptionBytes` (8 KiB, 2026-09-29) and appends
  `[mcp description truncated at 8192 bytes]` — a description within the cap is kept unchanged,
  and the empty-description stand-in is unaffected; `listServerTools` asks for at most
  `maxMCPToolListPages` (64) pages and surfaces at most `maxMCPToolsPerServer` (512) tools — past
  either it stops and returns the capped list silently, `Connect` having no report path but tools
  and errors — and skips a tool whose normalised schema exceeds `maxMCPToolSchemaBytes` (64 KiB)
  as it skips one with no name.
- **An HTTP server's configured headers are validated before any connect** (2026-10-03).
  `ServerConfig.Headers` (literal values) and `ServerConfig.HeadersEnv` (header → the NAME of an
  environment variable holding the value) are the `headers:` / `headers-env:` keys, on the webhook
  Reaction's precedent, read by the SSE and streamable-http transports alone.
  `ServerConfig.ValidateHeaders` is the one rule: a name that is not an HTTP token, a reserved name
  (case-insensitive: `Host`, `Content-Length`, `Content-Type`, `Accept`, `Connection`,
  `Transfer-Encoding`, `Last-Event-ID`, and every `Mcp-*` name by prefix — the SDK sets
  `Mcp-Session-Id`, `Mcp-Protocol-Version`, `Mcp-Method` and `Mcp-Name` itself, so a list would
  outgrow itself), a literal value with CR, LF or NUL, one name configured twice across both maps
  in any case, and a blank variable name are refused, naming the key and the header and never a
  value. The config loader refuses such an entry at startup and `validateServers` refuses it again
  before any connect, so a host that builds its `ServerConfig`s directly meets the same rule. The
  `headers-env:` names join the host's secret-env scrub (`config.MCPHeaderEnvNames`), fixed at
  startup.
- **An HTTP server's configured headers ride every request, beneath the origin pin**
  (2026-10-03). `vetEndpoint` resolves the set once per transport build — so on every connect and
  reconnect — with `resolveHeaders`: `Headers` as written, then each `HeadersEnv` variable read
  with `os.LookupEnv`, in sorted name order. An unset variable fails the connect naming the server,
  the header and the variable, never a value or the endpoint. `headerTransport` clones each
  request and sets the headers on it; it is composed inside `vetEndpoint`'s one `WrapTransport`
  closure above `boundedBodyTransport`, both beneath security's `OriginPinTransport`, so a header
  (an auth token above all) only ever reaches the configured origin — an SSE `endpoint` event
  naming another origin is refused before a header could leave.
- **Every tool call is bounded by a 5-minute deadline** (2026-09-29). `serverTool.Execute` derives
  its call context from the caller's with `mcpCallTimeout` (5 minutes, a fixed package value, no
  config key), so a silent or wedged server can never hold the agent past it. The caller's own
  cancellation is checked first and still returns the Go error `ctx.Err()`; a deadline that fires
  while the caller is live surfaces as an error result, `mcp: call timed out after 5m0s`, so the
  Turn survives and the model can route around the server (ADR 0007).

Every tool **description, schema, and result** the client surfaces is untrusted input: it is passed
to the model and rendered, **never executed or interpreted** as a command by Apogee.

## 3. The lifecycle (the design surface §3 D3)

```
Connect(ctx, []ServerConfig, URLGuard, workspaceRoot) → *Client  // dial every server, list its tools
ConnectWith(ctx, Host, []ServerConfig, URLGuard, workspaceRoot)   // the same over an explicit Host
  Client.Tools() []domain.Tool                                   // the surfaced tools, for registration
  Client.Close() error                                           // tear every session down — no orphan
```

- **Connect** is **all-or-nothing**: a later server's failure tears down every already-opened
  session and returns the error, so a half-wired MCP set never reaches the registry and no orphaned
  stdio process — or process tree — leaks. `workspaceRoot` is the exec fence a stdio server's
  command is measured against (§2). Zero configs returns a **dormant** Client (no sessions, no tools, a no-op
  Close) — a host without MCP pays nothing. Every server's headers must pass `ValidateHeaders` (§2),
  and server names must be non-empty and unique (the name
  prefixes each surfaced tool's registry key as `mcp__…` — actually `<name>__<tool>`, see §4).
- **Host** is what a connect takes from the process rather than from config: `Host.Proxy` resolves
  the egress proxy the HTTP transports honour, nil meaning `http.ProxyFromEnvironment`; `Host.Shell`
  scopes a stdio server's `env-allowlist` environment (nil: `platform.Current()`);
  `Host.NewTeardown` builds the container its process tree is held in (nil:
  `platform.NewProcessTeardown`); `Host.TerminateDuration` is the shutdown ladder's rung wait
  (non-positive: 5s). The composition root (`cmd/apogee/wire_live.go`, `liveMCPHost`) passes the
  real one through `ConnectWith`; a test injects its own the same way rather than swapping a
  package variable — `internal/mcp` holds none (2026-10-01).
- **Tool naming** qualifies each server tool as `<server-name>__<tool>` so two servers advertising
  the same tool name never collide in the single flat registry, and the human approving a call sees
  which server it reaches.
- **Resume reconnects FRESH (ADR 0008).** The Client holds no serializable state; a resumed Session
  simply calls `Connect` again from the same config. No server-side state is restored — there is no
  server-side-state promise. (`cmd/apogee/wire_live.go` establishes the connection on every launch,
  resume included; `cmd/apogee/wire_mcp.go` holds the connected set and the reconnect an
  `mcp-servers:` edit drives.)
- **Close** joins every session's teardown error and clears the sessions; it is safe on a dormant or
  already-closed Client. The composition root `defer`s it so no process or connection survives exit.

## 4. Where it plugs in

- `internal/mcp` depends only on the SDK, `internal/domain`, `internal/security`, and
  `internal/platform` — the last for a stdio server's process-tree teardown (stopping a server
  kills its whole process tree, not just the first process) and for scoping its `env-allowlist`
  environment. It never imports the root facade (ADR 0010). It exports `Client`, `Connect`,
  `ConnectWith`, `Host`, `ServerConfig`, `Transport`.
- `cmd/apogee` owns the wiring: `config.yaml`'s `mcp-servers:` block (config-file-only, default-
  empty) → `mcp.ServerConfig` values → `mcp.ConnectWith` → `registryWithMCP` registers the discovered
  tools on top of the default registry → `Config.Tools`. A discovered tool whose qualified name
  collides with a built-in is dropped with a stderr notice (the built-in wins).
- The disposition's `tools.ClassMCP` gating is proven in `internal/agent/dispatch_test.go`; this package's
  tests prove a **real** surfaced tool reports `EffectMCP` (the property the gate keys on) and
  exercise the live stdio path end to end (a fork-and-exec fixture server).

## 5. Acceptance (P3.15) — how each criterion is met

| Criterion | Mechanism |
|---|---|
| A hermetic stdio server exposes a tool that appears in the menu, is callable | `TestConnect_SurfacesServerToolsAndCalls` over a fork-and-exec stdio fixture |
| Raises Approval in Auto (asserted) | `EffectMCP` ⇒ `tools.ClassMCP` ⇒ `resolve` (the Resolution ladder, `internal/agent/resolution.go`; `dispatch_test.go`); the real tool's kind asserted in `TestServerTool_IsMCPExternalEffect` |
| A resumed session re-establishes from scratch | `TestResume_ReconnectsFresh` (Close, then a fresh Connect rediscovers the tools) |
| The bench swaps a deterministic stub with no process | the `mcp`-kind tool routes through `Config.ExternalEffects.Do` (ADR 0008; `dispatch_test.go`) |
| `Close` tears down cleanly (no orphan) | all-or-nothing Connect rollback + `TestClose_TearsDownSessions` |
| Cross-build green (the SDK is pure-Go) | the 6 CGO_ENABLED=0 cross-builds pass |
