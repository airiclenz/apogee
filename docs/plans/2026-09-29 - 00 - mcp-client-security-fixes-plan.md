# MCP client security fixes — plan

**Goal:** Close the four verified security findings of the 2026-09-29 `internal/mcp` audit: a
per-call deadline on MCP tool calls, no endpoint credential in surfaced error text, a size cap on
server tool descriptions, and a same-origin constraint on the SSE `endpoint` event.
**Date:** 2026-09-29
**Status:** unexecuted
**sized for:** ~200k-context host
**base:** dba82f58

**Regression check (2026-09-29, dba82f58):**
- 1: guard folded
- 2: guard folded
- 3: guard folded
- 4: guard folded; yields to ADR 0012 Amendment (b)

**Sources:**
- `internal/mcp/doc.go` (trust boundary), `docs/design/mcp-client.md` (bounds list)
- `docs/adr/0012-confinement-attaches-to-blast-radius-and-confine-to-workspace-flag.md` (Amendment 2026-07-26: endpoint exempt from IP floor, pinned)
- ADR 0007 (Go error reserved for ctx cancellation; tool faults are error results)
- SDK `github.com/modelcontextprotocol/go-sdk@v1.6.1`: `mcp/sse.go` `SSEClientTransport.Connect` (endpoint event resolved via `parsedURL.Parse`, no origin check); `mcp/transport.go` `call` (no per-call timeout)

**Ratified design calls** (user, 2026-09-29):
- **Call deadline:** fixed 5-minute package constant, no config key.
- **SSE endpoint target:** same origin only — scheme + host + port equal to the configured endpoint's (default ports canonicalised); anything else fails.
- **Description cap:** 8 KiB, clipped with a truncation marker in the `clipResult` style.
- **Redaction form:** surfaced error text carries at most `scheme://host[:port]` of an endpoint — never userinfo, path or query.

**Standing requirements:**
- skills: coding-standards
- Any authorized deviation from item text lands as a dated NOTES line under the item.

**Out of scope:**
- A per-server `call-timeout:` config key; any change to `internal/security` (PinnedDialControl stays IP-based).
- Consolidating `newGuardedHTTPClient` with `internal/tools`' client builder.
- Machine-check findings (vet/lint/race/coverage) — none were reported.

## 1. Bound every MCP tool call with a 5-minute deadline — ✅ DONE (2026-09-29)

**What:**
**Goal:** `serverTool.Execute` never waits on a server longer than `mcpCallTimeout` (5 min): a call that outlives it returns an error `ToolResult` (IsError, text naming the timeout) with a nil Go error while the caller's ctx is still live; a cancelled caller ctx still returns the Go error `ctx.Err()`.
**Approach (assumed at the header base):**
Fix for the audit finding "MCP tool calls carry no per-call deadline; a silent server wedges the agent". In `internal/mcp/tool.go` `serverTool.Execute`, derive `callCtx, cancel := context.WithTimeout(ctx, mcpCallTimeout)` and pass it to `t.caller.CallTool`. On error: check the PARENT `ctx.Err()` first (Go error, unchanged); then `errors.Is(callCtx.Err(), context.DeadlineExceeded)` → `domain.ErrorResult(call.ID, "mcp: call timed out after <d>")`; else the existing "call failed" result. The existing `ctx.Err()` branch must NOT be reused for the deadline — it would return a Go error and end the Turn (ADR 0007). `mcpCallTimeout` is a package var (default `5 * time.Minute`) only as a test seam, documented like `stdioTerminateDuration` (a test shrinking it must not run in parallel). Update `Execute`'s doc comment, `doc.go`, and the bounds list in `docs/design/mcp-client.md` to name the deadline.
**Regression guard.** The doc sweep is a rule, not a closed list: every sentence that says an MCP call waits only on its ctx (`grep -rn 'to its ctx\|retry budget' internal/mcp docs/design/mcp-client.md` — today `internal/mcp/transport.go:479`, the `boundedBodyTransport` comment, and `docs/design/mcp-client.md:99`) now names the 5-minute deadline. The `CHANGELOG.md:595` history entry stays as it is.
**Files:** internal/mcp/tool.go, internal/mcp/tool_test.go, internal/mcp/doc.go, internal/mcp/transport.go, docs/design/mcp-client.md
**Read first:** internal/mcp/tool.go — serverTool.Execute; internal/mcp/mcp_test.go — listFromInProcessServer, TestExecute_CancelledContextIsGoError, TestClose_BoundsTheDrainOfAWedgedStdioServer (var-shrink + Cleanup pattern);
internal/mcp/transport.go — stdioTerminateDuration, boundedBodyTransport; internal/mcp/tool_test.go — fakeCaller; docs/design/mcp-client.md — §2 bounds list
**Tests:** new test in `internal/mcp/tool_test.go` with a server tool (in-memory SDK session) that blocks until its ctx is done; shrink `mcpCallTimeout` to ~100ms; assert IsError result, timeout text, nil Go error, and that it returns within a small multiple of the timeout. Second case: caller ctx cancelled mid-call → Go error `context.Canceled`.
**Acceptance:**
- `go vet ./internal/mcp/`
- `go test -count=1 ./internal/mcp/`
- `go test -race -count=1 -run 'Timeout|Cancel' ./internal/mcp/`
**Commit:** `fix(mcp): bound every tool call with a per-call deadline`

## 2. Redact endpoints from surfaced MCP error text — ✅ DONE (2026-09-29)

NOTES (2026-09-29): the redactor wraps connect / list-tools errors in an unexported redactedError that keeps the original chain (Unwrap), so errors.Is on ctx cancellation still holds; the call-failed result redacts plain text.
NOTES (2026-09-29): newServerTool also gained the redactor parameter (serverTool carries it), so the four newServerTool calls in tool_test.go pass nil; the redaction and journey tests live in transport_test.go, not tool_test.go.
NOTES (2026-09-29): consequential edit — internal/mcp/doc.go: made necessary by the new endpoint redaction in the trust-boundary bullet list
NOTES (2026-09-29): consequential edit — docs/design/mcp-client.md: made necessary by the new endpoint redaction (network-transport bullet)

**What:**
**Goal:** No error text the package surfaces — `Execute`'s "call failed" result, `Connect`'s connect and list-tools errors — contains a configured endpoint's userinfo, path or query; an endpoint appears at most as `scheme://host[:port]`.
**Approach (assumed at the header base):**
Fix for the audit finding "endpoint credentials leak into tool-error text": the SDK's HTTP transports surface `*url.Error` text such as `Post "https://host/mcp?token=SECRET": …`, bypassing the `checkEndpoint` scrub. Add one unexported redactor in `internal/mcp` built from the normalised endpoint (`vetEndpoint`'s `u`): it replaces every occurrence of the endpoint's origin followed by any non-space, non-quote run with the bare origin — so both the configured URL and an SSE session URL on the same origin are cut. `serverTool` carries the redactor (nil/identity for stdio); `newServerTool`/`listServerTools` receive it from `connectOne`. Apply it in `Execute`'s "call failed" branch and to the wrapped errors in `connectOne` (connect) and `listServerTools`. Depends on item 1 (same `Execute` branch). Update the error-discipline sentence in `doc.go` / `checkEndpoint`'s comment only if they describe the scrub as parse-refusal-only.
**Regression guard.**
- Userinfo: net/http prints `Post "http://TOKEN@127.0.0.1:1/mcp?token=SECRET": …` (`user:***@host` when a password is set), so origin-then-run never matches. Build the pattern as `scheme://` + optional `[^@/\s"]*@` + host[:port] + a non-space, non-quote run, and replace the whole match with the bare origin.
- Signatures: build the redactor in `connectOne` from `security.NormalizeURL(cfg.Endpoint)` (the same `u` `vetEndpoint` returns) so `buildTransport`'s 5-value signature stays untouched; `listServerTools`' new parameter breaks `listFromInProcessServer` (`internal/mcp/mcp_test.go:1131`), which is updated.
- The journey test "kills" the streamable-http server by calling httptest.Server.CloseClientConnections before Close (Close alone blocks on the standalone SSE GET).
**Files:** internal/mcp/tool.go, internal/mcp/client.go, internal/mcp/transport.go, internal/mcp/tool_test.go, internal/mcp/transport_test.go, internal/mcp/mcp_test.go
**Read first:** internal/mcp/client.go — connectOne, listServerTools; internal/mcp/tool.go — newServerTool, serverTool.Execute; internal/mcp/transport.go — vetEndpoint;
internal/security/urlsafety.go — NormalizeURL; internal/mcp/mcp_test.go — listFromInProcessServer; SDK mcp/streamable.go — connectStandaloneSSE
**Tests:** unit test of the redactor (token in query, token in path, userinfo fed as real `*url.Error` text in both the username-only and the `user:***@` form, same-origin session URL, unrelated text untouched). Journey test: a streamable-http endpoint `http://127.0.0.1:<port>/mcp?token=SECRET` whose server dies after connect (`CloseClientConnections`, then `Close`); a tool call's result text contains neither `SECRET` nor `/mcp`. Connect-failure case: a refused endpoint with a token → `Connect` error omits it.
**Acceptance:**
- `go vet ./internal/mcp/`
- `go test -count=1 ./internal/mcp/`
- `go test -race -count=1 -run 'Redact' ./internal/mcp/`
**Commit:** `fix(mcp): redact endpoint credentials from surfaced errors`

## 3. Cap server tool descriptions at 8 KiB

**What:**
**Goal:** A surfaced MCP tool's `Description()` is never longer than `maxMCPToolDescriptionBytes` (8 KiB) plus its truncation marker; a description within the cap is returned unchanged.
**Approach (assumed at the header base):**
Fix for the audit finding "tool descriptions ride no size cap". Add `maxMCPToolDescriptionBytes = 8 << 10` and a marker `"\n[mcp description truncated at %d bytes]"` in `internal/mcp/tool.go`; clip `t.Description` in `newServerTool` with a helper in the `clipResult` shape (share one clip helper parameterised by limit and marker — both clip sites have one reason to change). The empty-description stand-in in `Description()` is unchanged. Add the cap to the bounds list in `docs/design/mcp-client.md` and to the caps comment block above `maxMCPToolListPages` in `client.go` if it enumerates the per-tool caps. Depends on item 2 (same file).
**Regression guard.** A `Client` is built only by `Connect(ServerConfig)` (no in-memory transport), so the listed-tool case drives `listFromInProcessServer` (`internal/mcp/mcp_test.go`), adding the 1 MiB tool through its `addTools` hook, and asserts on the returned tool's `Description()` — not via `Client.Tools()`.
**Files:** internal/mcp/tool.go, internal/mcp/tool_test.go, internal/mcp/client.go, internal/mcp/mcp_test.go, docs/design/mcp-client.md
**Read first:** internal/mcp/tool.go — newServerTool, serverTool.Description, clipResult, mcpResultTruncatedMarker; internal/mcp/client.go — maxMCPToolSchemaBytes caps block ("The three bounds" wording);
internal/mcp/mcp_test.go — listFromInProcessServer; internal/mcp/tool_test.go — TestServerToolDescriptionFallback; docs/design/mcp-client.md — §2 post-decode caps paragraph
**Tests:** `newServerTool` with a description of cap+1 bytes → clipped length and marker; exactly cap bytes → unchanged; a listed tool from an in-memory server advertising a 1 MiB description (added via `listFromInProcessServer`'s `addTools`) surfaces clipped in the returned tool's `Description()`.
**Acceptance:**
- `go vet ./internal/mcp/`
- `go test -count=1 ./internal/mcp/`
**Commit:** `fix(mcp): cap server tool descriptions at 8 KiB`

## 4. Pin HTTP-transport requests to the configured endpoint's origin

**What:**
**Goal:** Every request the MCP HTTP client sends (SSE GET and POSTs, streamable-http) targets the configured endpoint's origin (scheme + host + port, default ports canonicalised); an SSE server whose `endpoint` event names another origin fails `Connect` with an error naming the server, and no request reaches the other origin.
**Approach (assumed at the header base):**
Depends on item 2. Fix for the audit finding "SSE endpoint event can redirect the POST channel": the SDK resolves the event via `parsedURL.Parse` with no origin check, and `PinnedDialControl` judges IPs only — with an egress proxy the proxy's pinned IP passes any target, and without one the operator's host allow/deny is skipped. The SDK has no hook, so add a `RoundTripper` layer in `newGuardedHTTPClient`'s chain (next to `boundedBodyTransport`) that refuses a request whose origin differs from the vetted endpoint's; `vetEndpoint` passes the origin in. The refusal wraps `security.ErrURLBlocked` and does not interpolate the refused URL (item 2's discipline). Update `doc.go`'s trust-boundary paragraph and `docs/design/mcp-client.md` to state the origin pin.
**Regression guard.**
- Chain order: the origin check is outermost with `boundedBodyTransport` as its `next`, and "closing the bounded body closes the real one" (`internal/mcp/transport_test.go:193`, which type-asserts `client.Transport.(*boundedBodyTransport)` and swaps `bounded.next`) is updated to unwrap the origin layer first — or the layer sits beneath via `boundedBodyTransport.next` and that recorder test wraps, not replaces, it.
- Two subtests GET `http://10.9.8.7:9/mcp` and assert `ErrSSRFBlocked`, which the origin refusal (`ErrURLBlocked`) does not wrap: "another private address on the pinned client is refused" (`transport_test.go:266`, its comment included) and "an unproxied private address is still refused" (`:340`). Re-point each at the dial control beneath the origin layer (a same-origin name whose resolution differs, as the rebind subtest does) or assert `ErrURLBlocked` there and keep an SSRF-floor proof on the inner transport.
- The request URL's host goes through the same `security.NormalizeURL` / `Hostname` form as the vetted side before comparing (lower-case, root dot, IDNA).
- The origin-refusal's own error text never interpolates the refused URL, but http.Client wraps it in *url.Error, which quotes that server-named URL; this is accepted (it carries no operator secret). Tests assert errors.Is(err, security.ErrURLBlocked) and the server name, never the refused URL's absence from the text.
- Yields to ADR 0012 Amendment (b) (`docs/adr/0012-confinement-attaches-to-blast-radius-and-confine-to-workspace-flag.md`, the dial floor refusing an SSE `endpoint` event to another address): the origin pin is added on top, the ADR is not amended, and the `internal/security/ssrf.go:282` comment stays true and unchanged.
**Files:** internal/mcp/transport.go, internal/mcp/transport_test.go, internal/mcp/doc.go, docs/design/mcp-client.md
**Read first:** internal/mcp/transport.go — vetEndpoint, newGuardedHTTPClient, boundedBodyTransport.RoundTrip; internal/mcp/transport_test.go — TestGuardedClient_PinsTheEndpointAndRefusesEverythingElsePrivate, TestGuardedClient_ProxiedEndpointPinsBothHosts;
SDK mcp/sse.go — SSEClientTransport.Connect, SSEHandler (sends endpoint.RequestURI(), "/path?sessionid=", not bare "?sessionid="); internal/security/urlsafety.go — NormalizeURL
**Tests:** SSE test server on one `httptest` listener whose `endpoint` event names a second listener's URL → `Connect` fails, the second listener records zero requests. Same-origin relative `?sessionid=` event (the SDK server's own form) still connects and calls a tool. Origin comparator unit cases: `https://h` vs `https://h:443` equal; an upper-case / trailing-root-dot spelling of the host equal; different port / scheme / host refused. The two private-address subtests and the bounded-body recorder subtest updated per the guard. Refusal assertions use `errors.Is(err, security.ErrURLBlocked)` and the server name only.
**Acceptance:**
- `go vet ./internal/mcp/`
- `go test -count=1 ./internal/mcp/`
- `go test -race -count=1 -run 'Origin|SSE' ./internal/mcp/`
**Commit:** `fix(mcp): pin HTTP-transport requests to the endpoint's origin`
