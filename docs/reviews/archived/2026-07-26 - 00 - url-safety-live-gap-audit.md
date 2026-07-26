# Code Review — the live url-safety gap — 2026-07-26

**Scope:** the url-safety enforcement path — `internal/security/urlsafety.go` and `ssrf.go`; the
`networkTool` funnel and its unexported url-filter marker (`internal/tools/network.go`); the three
built-ins that embed it (`web_fetch.go`, `http_request.go`, `web_search.go`) and their registry
wiring (`registry.go`); the `classNetwork` / `classThirdPartyNetwork` seams in
`internal/agent/resolution.go` and `dispatch.go`; plus the composition roots that thread the guard
(`cmd/apogee/wire.go`, `internal/agent/construct.go`) and the tests that pin all of it.
**Mission:** Apogee is a terminal coding agent for small, locally-run LLMs; in Auto mode it runs
tool calls with no human in the loop, so every unattended call must have a bounded blast radius.
**Conduct sources:** **ADR 0012** (incl. the **amendment of 2026-07-25**) and
`docs/design/confinement-execution-contract.md` **§4**. Where those two differ on a mechanism, the
contract is authoritative on the *how*.
**Files reviewed:** 21 source files and 11 test files.
**Discharges:** the `/code-audit` on the *live* url-safety gap parked by candidate 02 of
`docs/reviews/archived/2026-07-24 - 00 - architecture-deepening-review.md` — the shape fix landed
2026-07-25; this audit is the independent check on the hole itself.

## Executive Summary

**The hole card 02 named is closed at the funnel, and the funnel is well built.** `networkTool.do`
really is the single path from a tool to the network: every one of the three built-ins reaches it
and nothing else, `CheckContext` always runs before the request is built, `newHTTPClient` is the
only client constructor and always installs `SafeDialControl`, and the dial-time control was
confirmed by experiment to fire on every connection attempt — v4 and v6, over TLS and HTTP/2. The
classic bypasses are genuinely shut: userinfo (`http://allowed.com@169.254.169.254/`) resolves to
the attacker-visible host and is blocked, `::ffff:`-mapped and NAT64-well-known-embedded v4 are
decoded, decimal/octal/hex hosts fail to resolve and fail closed, redirects are not followed,
header injection is closed by the deny-list plus the stdlib's own field validation, and the 2 MiB
body cap sits *after* transparent gunzip so there is no decompression bomb. The marker is
unfakeable as claimed: the method is unexported, `internal/` blocks embedding from another module,
nothing in the registry or dispatch path wraps a tool value, and a walk over the default set fails
any `EffectNetwork` tool that lacks it.

**The promise is defeated one layer up, not at the funnel.** `classifyTool` consults the
*self-declared* `ReadOnly()` before it consults any unfakeable marker, so a tool that declares
`EffectNetwork` **and** `ReadOnly() == true` is classified read-only and auto-runs in **every**
mode — Plan included — with no url filtering and no gate. That is precisely the "keys on a
declaration, not a marker" defect the 2026-07-25 amendment was written to remove, moved up one
rung; both interfaces it needs are exported through the public facade. The same ordering already
has three live consequences in the shipped tool set, two of them OS-subprocess launches that run
outside any confinement box in every mode. That is the systemic finding, and it cuts across H-1
and H-2.

Below those, the findings cluster in two places: the funnel's handling of the *ends* of a request
(the body-read error is discarded, so a truncated response is presented as complete), and URL
*normalisation* — the guard matches a host string that is neither what the transport dials nor
what the request is built from. The normalisation defects are latent today because no shipped path
populates `AllowHosts`/`DenyHosts`, but they are exactly what the parked url-safety config key
(`TODO.md:285`) would land on top of.

## Intent & Architecture Findings

### High — a self-declared `ReadOnly()` outranks every unfakeable marker, so the vouched-for network split is bypassable by declaration `[Intent + Security]`

- **Where:** `internal/agent/resolution.go:242-262` (`classifyTool`), with the ladder rows at
  `resolution.go:272-277` (Plan) and `:316-317` (Auto); `apogee.go:240-246` (the exported facade
  aliases).
- **What:** `classifyTool` returns `classReadOnly` at line 243, *before* it looks at
  `IsWorkspaceScopedWriter`, `IsURLFilteredNetworker`, `ExternalEffect()` or `IsSubprocessTool`.
  Every later marker is unfakeable by construction; `ReadOnly()` is a bare self-declaration — and
  it wins. Three live consequences share this one root cause:
  1. **The network split is escapable.** `apogee.ReadOnlyTool` and `apogee.ExternalEffectTool` are
     both exported aliases (`apogee.go:242`, `:238`), so a host-registered tool that GETs URLs and
     returns `ReadOnly() == true` — semantically reasonable, it writes nothing — takes the RO row.
     The contract's **3p-net** row says `refuse | gate | gate | gate | run`; it gets
     `run | run | run | run | run`, including the Plan menu (`internal/agent/loop.go:764`).
  2. **`git_diff_range` launches an unconfined subprocess in every mode.**
     `internal/tools/git.go:443` declares `ReadOnly() == true` and `:448` declares
     `Subprocess() == true`. The RO row yields `resolveRun`, `executeRun` passes `box == nil`
     (`dispatch.go:132-138`), and `runSubprocess` confines only when a handle is on ctx
     (`internal/tools/exec_common.go:137`) — so `git` runs raw.
  3. **`diagnostics` does the same with the Go toolchain.** `internal/tools/diagnostics.go:81`
     and `:87` declare the same pair; `runGoVet` shells out to `go vet <dir>`
     (`diagnostics.go:186-192`).
- **Why it matters:** consequence 1 reopens, by a different route, the exact hole ADR 0012's
  2026-07-25 amendment closed: *"Classification keys on the marker... an `EffectNetwork` tool
  **without** it is a distinct third-party-network class that gates."* Consequences 2 and 3 break
  the ADR's own core invariant — *"Apogee never runs a tool call both unsupervised and
  unbounded"* — in normal use, and in Plan mode, whose whole promise is that nothing executes.
  The realistic trigger under this project's threat model is a prompt-injected model steered by a
  hostile repo: `go vet` compiles attacker-controlled source, resolves modules from `GOPROXY`, and
  honours a `toolchain` directive in the repo's own `go.mod`, all outside the fence.
- **Fix:** consult the unfakeable markers before honouring the declaration — check
  `IsSubprocessTool` and `ExternalEffectTool` ahead of `IsReadOnly` in `classifyTool`, or ignore
  `ReadOnly()` on any tool that also satisfies either. This is tighten-only and changes no
  documented cell for the built-ins that carry neither marker (`read_file`, `grep`, `view_diff`,
  `open_file`, `list_dir`, `ask_user`): only `git_diff_range` and `diagnostics` move from `run` to
  `confine`-or-`gate` in Auto, which is what "confine if you can, gate if you can't" already
  prescribes. **Warrants revisiting** contract §4, whose RO row as written is what licenses the
  current code — the table needs to say that RO is a *floor* only for tools carrying no other
  class marker.
- **Note:** the shipped CLI is exposed through consequences 2 and 3 only. MCP tools are not
  affected — `internal/mcp/tool.go` declares `ExternalEffect() == EffectMCP` and no `ReadOnly()`,
  so the MCP gate holds.

## Critical & High Findings

### High — `present_document` is read-only-classified and hands a model-controlled file to the OS handler, outside any box `[Security]`

- **Where:** `internal/tools/present_document.go:66` (`ReadOnly() == true`);
  `internal/present/opener.go:119-131` (`argv`), `:180` (`launchDetached` → plain `exec.Command`);
  wired on by default at `cmd/apogee/config.go:239` and `cmd/apogee/wire.go:379`.
- **What:** the same RO-first ordering gives `present_document` `resolveRun` in every mode with
  `box == nil`, and the local rung then execs `open <path>` (darwin), `cmd /c start "" <path>`
  (windows) or `xdg-open <path>` (linux) on the path the model chose. Unlike H-1's other
  consequences this one is *not* fixed by reordering the markers — `present_document` carries no
  other marker to reorder ahead of.
- **Why it matters:** in Auto the model can write the file first — an in-workspace `write_file` is
  a `workspaceScopedWriter` with an in-workspace target, so it auto-runs (`resolution.go:319-322`)
  — and then present it. Path-safety bounds *which* file is handed over, but the model controls the
  file's content **and extension**, and the extension is what selects the handler: `report.command`
  or `report.terminal` on macOS, `report.bat` or `notes.hta` on Windows, all executed with the
  user's full privileges outside the confinement box. In Plan mode the tool is still ungated, so a
  checked-in `build.bat` in a hostile repo suffices there.
- **Fix:** apply an extension allow-list in `Opener.argv` — reuse `browserRenderableExts`
  (`internal/tui/presenter.go:142`), which the doc-server rung already enforces — and degrade to
  the baseline transcript rung for anything else. If `present_document` should stay a leaf that
  launches a process, give it its own `toolClass` that gates outside Plan.
- **Note:** outside the audited path; surfaced because it shares H-1's root cause. Its own
  disposition is a separate owner decision.

### High — the funnel discards the body-read error, so a truncated response is presented as complete and an in-flight cancellation returns the success shape `[Correctness]`

- **Where:** `internal/tools/network.go:179` (`body, truncated := readCappedBody(resp.Body)`) and
  `:249-256` (`data, _ := io.ReadAll(limited)`); the contract it breaks is stated at
  `network.go:119-132`.
- **What:** `readCappedBody` drops the read error, and `do` performs no `ctx.Err()` re-check after
  `client.Do` succeeds. `truncated` is set *only* by the 2 MiB cap, so a body cut short by an error
  carries no marker at all. Three concrete triggers:
  - `http.Client.Timeout` covers the body read, so a server streaming slowly past the resolved
    timeout yields `HTTP 200 OK` plus the first chunk, with no truncation notice;
  - a mid-body connection reset yields the same silent partial;
  - a caller ctx cancelled while the body streams leaves `ctx.Err() != nil` while `do` returns
    `(resp, "", nil)`, so `Execute` returns a nil error and the Turn is **not** rolled back.
- **Why it matters:** the first two hand a 4B–35B model a silently amputated page it cannot detect
  is incomplete — the worst shape of wrong answer for this product. The third breaks `do`'s own
  documented three-shape contract and ADR 0007's "a tool returns a Go error for nothing else"
  rule; `TestNetworkFunnel_DoCancelledCtxIsGoError` only covers cancel-before-request and
  cancel-before-headers, so nothing catches it.
- **Fix:** have `readCappedBody` return `(body, truncated, err)`. In `do`, on a non-nil read error
  check `ctx.Err()` first and return it as the Go-error shape; otherwise either return the
  message shape (`"response from host "+label+" was cut short: "+scrubURLError(err, req.url)`) or
  set `truncated = true` so the existing "[response truncated…]" marker fires.

### High — the dial-time SSRF floor is never exercised through the funnel's own client `[Test coverage + Security]`

- **Where:** `internal/tools/network.go:223` (`Control: guard.SafeDialControl()`) and `:169-173`
  (the dial-time-block branch in `do`); the claim it backs is at `network.go:29-32` and
  `internal/security/ssrf.go:39-43` ("the pre-flight Check is the cheap first line; the dial-time
  control is **the real bound**").
- **What:** `SafeDialControl` is tested only as a bare function, called directly with an address
  string (`internal/security/ssrf_test.go:146`). No test drives a URL that **passes** pre-flight
  and is stopped at connect. Every real-dial test in `internal/tools` disables the floor
  (`network_test.go:20`, `loopbackGuard()`), and every floor-on test blocks pre-flight — so the
  `if networkURLError(err)` branch in `do` never executes in the suite.
- **Why it matters:** the entire DNS-rebinding defence rests on the `Control` hook being installed
  on the dialer the transport actually uses. Any refactor that drops it, moves to a shared client,
  or wraps the transport leaves the whole suite green while a prompt-injected model regains an
  IMDS/loopback path in Auto, where no human is watching. The behaviour is correct **today** —
  confirmed by experiment during this audit — but nothing defends it.
- **Fix:** one hermetic test in `internal/tools`: a guard whose injected resolver returns a public
  IP (`security.URLGuard{}.WithResolver(...)`) plus an `httptest` server addressed as
  `localhost:<port>`; assert `guard.CheckContext` **passes**, then `NewWebFetch(guard).Execute`
  returns a non-Go-error `IsError` result, the handler was never reached, and the message names
  the host once and carries no URL.

## Medium Findings

### Medium — the guard matches a host string that is neither what the transport dials nor what the request is built from `[Security + Correctness]`

- **Where:** `internal/security/urlsafety.go:89` (parses `strings.TrimSpace(raw)`), `:102`
  (`strings.ToLower(u.Hostname())`), `:135-146` (`hostMatches`); versus
  `internal/tools/network.go:156` (`http.NewRequestWithContext(ctx, method, req.url, …)` — the
  **untrimmed** string) and Go's `canonicalAddr`, which applies `idna.Lookup.ToASCII`.
- **What:** three divergences on one seam, confirmed by experiment against the transport shape
  `newHTTPClient` builds:

  | URL | guard matches on | transport dials |
  |---|---|---|
  | `http://ⓖxample.com/` | `ⓖxample.com` | `gxample.com:80` |
  | `http://good.example.com。/` | `good.example.com。` | `good.example.com.:80` |
  | `http://evil.com./` | `evil.com.` | `evil.com.:80` |

  and separately, `hostMatches("evil.com.", ["evil.com"])` is `false` — neither `==` nor
  `HasSuffix(".evil.com")` — while DNS answers a trailing-dot FQDN identically and virtually every
  virtual-host server accepts it. The trim divergence is the third: a leading space passes the
  guard on the trimmed form and then fails to build the request on the untrimmed one.
- **Why it matters:** **appending one dot defeats a `DenyHosts` entry.** The SSRF floor is *not*
  affected (it judges resolved IPs, and Unicode hosts fail closed because the non-ASCII name does
  not resolve), so this is latent today — both composition roots pass a zero `security.URLGuard{}`
  (`cmd/apogee/wire.go:398`, `internal/agent/construct.go:191`), leaving the allow/deny lists
  empty. It becomes live the moment the dedicated url-safety config key parked at `TODO.md:285`
  lands, which is the entire point of reporting it now. The trim divergence fails closed today,
  but it is the same class of bug: the string checked is not the string requested.
- **Fix:** normalise once, at the top of `networkTool.do`, and use the result for both the guard
  and the request: `strings.TrimSpace`, `idna.Lookup.ToASCII`, lowercase, strip a single trailing
  dot. Better still, parse once in `do` and hand the `*url.URL` to `http.NewRequestWithContext`
  so no second parse can disagree. Add table rows for `https://example.com.` and a Unicode host
  under `DenyHosts: ["example.com"]`.

### Medium — `%q` escaping defeats the URL redaction, so the full request URL rides out to the model `[Security]`

- **Where:** `internal/security/urlsafety.go:91`
  (`fmt.Errorf("%w: unparseable url: %v", ErrURLBlocked, err)`) and
  `internal/tools/network.go:300-330` (`scrubURLError` → `redactRequestURL` → `redactSubstring`).
- **What:** `url.Parse`'s error is a `*url.Error` whose `Error()` is
  `fmt.Sprintf("%s %q: %s", …)`. `%q` escapes the URL, so the text carries
  `http://…?key=SECRET\x01x` with a *literal* backslash-x, while `redactSubstring` searches for
  the raw byte sequence — no match, nothing redacted. The `%v` (rather than `%w`) also means
  `errors.As(err, &ue)` in `scrubURLError` fails, so the URL-free `ue.Op`+cause reconstruction
  never runs on this path either. Confirmed by experiment; the model-facing string is
  `url blocked by url-safety (host the requested host): unparseable url: parse
  "http://example.com/?key=SECRET\x01x": net/url: invalid control character in URL`.
- **Why it matters:** it breaks the funnel's stated M2 discipline verbatim — *"a key-bearing
  request URL can never ride out to the model"* (`network.go:132`). The trigger is any URL
  argument carrying an **interior** ASCII control character (`TrimSpace` only strips the ends), and
  the key-bearing case is reachable through `web_search.go:141`, where an unparseable configured
  endpoint — which may carry an API key — is fed to `scrubURLError`.
- **Fix:** stop interpolating the parse error's text at all: return
  `fmt.Errorf("%w: unparseable url", ErrURLBlocked)` from `CheckContext` (or wrap with `%w` so
  `scrubURLError`'s `errors.As` path drops the URL), and make `redactSubstring` also strip
  `strconv.Quote(secret)`'s inner form as defence in depth.

### Medium — the SSRF floor decodes only the *well-known* NAT64 prefix, so an RFC 8215 local-use prefix reaches private v4 `[Security]`

- **Where:** `internal/security/ssrf.go:67-71` (`nat64WellKnownPrefix = 64:ff9b::/96`) and
  `:106-139` (`ipBlockedByFloor`).
- **What:** the floor decodes the embedded v4 of a `64:ff9b::/96` address precisely because *"a
  NAT64 gateway maps the embedded IPv4 onto a real v4 destination"*. It does not decode the RFC
  8215 **local-use** prefix `64:ff9b:1::/48`, which exists specifically for translating to
  non-global (private) IPv4 space. Verified against the floor's own logic:
  `64:ff9b::7f00:1` → blocked; `64:ff9b:1::7f00:1` → **not blocked**. The same gap covers 6to4
  (`2002::/16`), IPv4-compatible (`::a.b.c.d`) and deprecated site-local (`fec0::/10`).
- **Why it matters:** on a network running NAT64 with a local-use prefix — the realistic case for
  an IPv6-only enterprise or mobile network — a prompt-injected model in Auto reaches
  `http://[64:ff9b:1::a9fe:a9fe]/latest/meta-data/` and the gateway translates it to the IMDS at
  169.254.169.254. The precondition is specific but concrete, and the floor documents itself as
  covering "the embedded v4 inside a NAT64 well-known-prefix address" without noting the sibling
  prefix exists. The other three forms are deprecated and were confirmed unroutable on this host,
  so they are hardening rather than exposure.
- **Fix:** add `64:ff9b:1::/48` to the NAT64 decode (and, since the local-use prefix is
  `/48`, decode the embedded v4 from the low 32 bits the same way); optionally deny `2002::/16`,
  `::/96`-embedded v4 and `fec0::/10` outright, which no coding-agent fetch legitimately targets.

### Medium — a fresh `http.Transport` per call, never closed: idle sockets and goroutines for 30s each, and no connection reuse `[Correctness]`

- **Where:** `internal/tools/network.go:150` (`client := newHTTPClient(...)` inside `do`) and
  `:220-243`.
- **What:** every call allocates a new `*http.Transport`; nothing calls `CloseIdleConnections()`.
  Go keeps each pooled connection — and its `readLoop`/`writeLoop` goroutines — alive until
  `IdleConnTimeout` (30s), and those goroutines keep the transport itself from being collected.
- **Why it matters:** network tools **auto-run unattended in Auto**, so dozens of calls in a turn
  is the normal case, and the process accumulates an open socket plus two goroutines per call for
  30 seconds while also paying a fresh TCP+TLS handshake every time. `internal/mcp/transport.go:154`
  builds the identical client but keeps it long-lived, which is correct — the divergence is that
  the funnel's is per-call.
- **Fix:** `defer client.CloseIdleConnections()` in `do` after the body is read (one line), or
  build the transport once per `networkTool` and set only `Client.Timeout` per call.

### Medium — the per-call timeout ceiling does not cover the pre-flight DNS resolution `[Correctness]`

- **Where:** `internal/tools/network.go:146` (`n.guard.CheckContext(ctx, req.url)` runs on the raw
  caller ctx, *before* `newHTTPClient` at `:150`); `internal/security/ssrf.go:163`.
- **What:** the floor's `LookupIPAddr` is bounded only by the system resolver configuration, not by
  the resolved request timeout. Dispatch adds no per-tool deadline (`dispatch.go:308-348` passes
  ctx straight through).
- **Why it matters:** it contradicts `network.go:43-45` — *"bounds a single network call so a
  slow/hung endpoint never wedges a Turn"*. A host delegated to a black-holing nameserver blocks
  for `timeout × attempts × nservers` (default ~10s; `options timeout:30 attempts:5` in
  `/etc/resolv.conf` makes it minutes) **on top of** the HTTP timeout, and `http_request`'s
  `timeout_seconds: 1` does not bound it at all.
- **Fix:** derive the check's ctx from the resolved timeout —
  `rctx, cancel := context.WithTimeout(ctx, clampDuration(req.timeout)); defer cancel()` — and pass
  `rctx` to `CheckContext`, so one budget covers resolve + dial + body.

### Medium — `web_fetch` refuses to follow redirects but never shows the model where the redirect points `[Intent]`

- **Where:** `internal/tools/web_fetch.go:79-91` (`renderFetchResult`) versus
  `internal/tools/network.go:236-241` (the `CheckRedirect` rationale).
- **What:** the no-follow policy is justified in the funnel by *"The model sees the redirect
  Location and can choose to follow it through a fresh, re-checked call"*. `renderFetchResult`
  emits only the status line and `Content-Type`; `Location` is dropped.
- **Why it matters:** a plain `http://` → `https://` or trailing-slash canonicalisation — the
  common case — leaves a small model holding `HTTP 302 Found` with an empty body and no way
  forward, and it is a documented affordance the code does not deliver. `http_request` renders the
  full sorted header block and does not have the problem, which is the divergence.
  `internal/tools/network_test.go:115-135` pins only that the status is 302, so nothing notices.
- **Fix:** emit `Location` in `renderFetchResult` for any 3xx status.

### Medium — nothing proves the host-supplied `URLGuard` actually reaches the network tools `[Test coverage]`

- **Where:** `internal/tools/registry.go:101-103` (`NewWebFetch(host.URLGuard)` and its two
  siblings); `internal/tools/registry_test.go` covers names, menu order, counts and conditional
  registration, but not this.
- **What:** no test asserts that `HostTools{URLGuard: …}` is threaded into the three tools.
- **Why it matters:** because the zero `URLGuard` still has the SSRF floor on — and the
  composition root currently passes exactly that (`cmd/apogee/wire.go:398`) — a regression
  replacing `host.URLGuard` with `security.URLGuard{}` would drop an embedder's `DenyHosts`
  policy **silently**, with no failing test and no visible symptom. This is the registry's public
  assembly seam, so the property belongs there rather than in each tool.
- **Fix:** build the registry with
  `HostTools{URLGuard: security.URLGuard{DenyHosts: []string{"blocked.example"}}.WithResolver(publicStub)}`,
  look up `web_fetch`, `Execute` against `https://blocked.example/`, and assert an error result
  naming url-safety; repeat for `http_request` and `web_search`.

### Medium — an erroring Approver has no test, so the fail-closed decision for every gated class is unguarded `[Test coverage]`

- **Where:** `internal/agent/dispatch.go:272-279`; the unused scaffolding is at
  `internal/agent/statemachine_test.go:99` (`fakeApprover.err`, never set by any test).
- **What:** an Approver returning an error emits an `ErrorEvent` and returns `(false,
  dispatchDone)`, so `executeGate` refuses. Correct — and untested.
- **Why it matters:** this is the sole human-in-the-loop for every gated class, including the
  `unfiltered network reach` gate this audit exists to check. A refactor treating an Approver error
  as "no objection" (`return true`) — a plausible mistake once the UI approver starts erroring on a
  closed prompt — silently converts every gate into an unattended auto-run, with zero failing
  tests. The nil-Approver ⇒ Refuse rule is well covered (`resolution_test.go:493`); the *erroring*
  Approver is a different path, covered nowhere.
- **Fix:** drive a gated call (an MCP tool in Auto, `confine=true`) with
  `fakeApprover{err: errors.New("prompt closed")}`; assert the tool's `ran` counter is 0, the
  result is `IsError` with "denied by approver", and the call was audit-recorded as blocked.

### Medium — the floor's fail-closed paths and the numeric-encoding bypass family have no adversarial test `[Test coverage]`

- **Where:** `internal/security/ssrf.go:163-169` (resolution failure, empty answer);
  `ssrf.go:24-31` states the numeric-encoding invariant in the code itself.
- **What:** `fixedResolver` (`internal/security/ssrf_test.go:12-14`) never returns an error and
  never returns an empty slice, so both `could not resolve host` and `resolved to no addresses` are
  uncovered, and no test uses a numeric-encoded host.
- **Why it matters:** the code states plainly that *"the numeric-encoding safety rests on
  resolution failing, not on the floor"* — an invariant asserted by nobody. Someone "improving" DX
  by letting an unresolvable host through to the transport turns the classic decimal-encoded
  loopback (`http://2130706433/`) into a pre-flight pass; on a host whose resolver *does* decode
  `inet_aton` forms (the cgo path, common on macOS) the same edit matters more, because the decoded
  private IP is exactly what the floor was meant to catch.
- **Fix:** table over `{"2130706433", "0177.0.0.1", "0x7f.0.0.1", "127.1"}` × two injected
  resolvers — one returning `(nil, errNoSuchHost)`, one mimicking `getaddrinfo` by returning
  `127.0.0.1` — asserting both yield `errors.Is(err, ErrURLBlocked)` (the second additionally
  `ErrSSRFBlocked`); plus a resolver returning `[]net.IP{}` for the empty-answer block.

### Medium — the SSRF floor is applied to a user-typed MCP endpoint, making a localhost/LAN server unusable and startup fatal `[Intent]`

- **Where:** `cmd/apogee/wire.go:241-244`; `internal/mcp/transport.go:140-147` (`checkEndpoint`);
  `internal/mcp/client.go:66-71` (`Connect` is all-or-nothing).
- **What:** `Connect` receives `security.URLGuard{}`, so `checkEndpoint` runs the resolved-IP floor
  over the configured `endpoint:`. `http://127.0.0.1:7331/mcp` or `http://192.168.64.1:7331/mcp` is
  refused as loopback/private, `Connect` rolls the whole set back, and wire.go turns that into a
  fatal `return err` — apogee will not start. There is no config escape: `DisableIPFloor` is
  deliberately code-level only (`internal/security/urlsafety.go:54-61`).
- **Why it matters:** the asymmetry is sharp. The LLM `endpoint:` — the same category of
  user-chosen, config-supplied address, and routinely private (the shipped template at
  `cmd/apogee/defaults/config.yaml:15` is `http://192.168.64.1:1111`) — is dialled with an
  unguarded `&http.Client{}` (`internal/provider/client.go:90`). The floor exists to stop the
  *model* pivoting to internal addresses; an MCP endpoint is never model-supplied. A related
  divergence sits alongside it: `internal/mcp/transport.go:154-169` reproduces the funnel's client
  builder field for field but drops the `CheckRedirect` policy, so MCP HTTP transports auto-follow
  redirects while Apogee's own tools refuse to (harmless today — the dial-time floor re-checks each
  redirected connect — but a string-level allow/deny bypass on that path once the parked config key
  lands).
- **Fix:** treat a configured MCP endpoint as a host trust decision like `endpoint:` — exempt it
  from the pre-flight floor, or add a per-server opt-in — while keeping the dial-time control for
  the rebinding case. Export one client constructor (guard + timeout + redirect policy) and have
  both call sites use it, making "follow redirects" an explicit argument rather than an omission.
- **Note:** outside the audited path, and the current behaviour **is** documented
  (`cmd/apogee/defaults/config.yaml:89-90`, `internal/mcp/doc.go:22-28`). Reported as
  **warrants revisiting** those two docs, not as an undocumented defect — the question is whether
  the policy is right, and it is an owner call.

## Recommended Action Order

1. **H-1 — reorder `classifyTool`.** It is a few lines, strictly tighten-only, and it is the only
   finding that reopens the question this audit was commissioned to answer. It also discharges two
   of the three unconfined-subprocess consequences at once.
2. **H-2 — `present_document`'s opener.** Independent of H-1's fix and the largest single blast
   radius here. The extension allow-list already exists in `presenter.go`; reuse it.
3. **H-3 — the funnel's body-read error.** Self-contained inside `network.go`, no cross-package
   effects, and it removes a wrong-answer class the model cannot detect.
4. **H-4, then the M-grade test gaps** (host-guard threading, erroring Approver, floor fail-closed
   paths). H-4 first: it is a dozen lines and it is the regression net for everything else in the
   funnel.
5. **The normalisation Mediums** (`%q` redaction, host normalisation, NAT64 local-use prefix).
   These want doing **before** the parked url-safety config key at `TODO.md:285` lands — that key
   is what converts the latent half of them into live ones. Doing them first also makes the config
   key a smaller, safer change.
6. **The cheap remainder:** `CloseIdleConnections`, the resolve timeout budget, `Location` on 3xx.
7. **Needs design discussion first:** the MCP endpoint floor. It is documented behaviour, so the
   decision is a policy call, not a fix.
8. **Candidate for `/improve-codebase-architecture`:** the host-tools composition exists twice
   (`internal/agent/construct.go:189-196` and `cmd/apogee/wire.go:396-402`), the engine-side one
   unexported so the binary cannot reuse it. Any new `HostTools` field must be added in both or it
   is silently dropped on one path — which is exactly what the parked url-safety key will require.
   The guarded-client builder is duplicated on the same seam (`network.go:220` /
   `mcp/transport.go:154`). One shared derivation would retire both.

## What Looked Good

The funnel is the best-built thing in this scope and should not be touched beyond H-3 and the
Mediums above: one `do`, one client constructor, the marker genuinely inseparable from the guard,
and all three tools handling the `(resp, msg, err)` triple identically. The marker's unfakeability
is real and is pinned three ways — positively, negatively, and by a walk over the default set that
fails any future `EffectNetwork` tool that hand-rolls its own client
(`TestDefaultTools_EveryNetworkToolIsURLFiltered`) — and it survives `registry.Subset` structurally,
because `Subset` re-registers the same interface value and nothing in the dispatch path wraps a
tool before classification. The SSRF floor's IP classifier is careful work: CGNAT, `0.0.0.0/8`,
TEST-NET, the IPv4-mapped form and the NAT64 well-known prefix are all explicitly handled, and the
`ipBlockedByFloor` NAT64 indexing that looks unsafe is provably safe (`net.IPNet.Contains` can only
return true for a 16-byte non-v4-mapped address). `ipBlockedByFloor`, deny-before-allow precedence,
the sibling-prefix host case, multi-answer DNS, tighten-only-ness, and the **entire** ladder ×
class × mode × confine × approver matrix — including the marked-net-runs, unmarked-net-gates-with-
`unfiltered network reach`, and gate-without-Approver-refuses cells — all carry explicit, hermetic,
adversarial tests, at both the resolver and the dispatch level. The M2 key-scrubbing discipline is
proven across the pre-flight-block, dial-time-block and transport-failure shapes for all three
tools. No flaky patterns, sleep-synchronisation, hardcoded ports, or mocks-testing-mocks were found
anywhere in the audited tests.

## Disposition (2026-07-26 — close)

**Every finding in this report is landed.** They were executed as items **1–14** of
`docs/plans/archived/2026-07-26 - 03 - url-safety-live-gap-plan.md`, in this report's own
*Recommended Action Order*, each on its own green gate and each independently verified — commits
`46570a2` (H-1) through `6013817` (M-10). That plan is complete and archived; its items carry the
per-item `NOTES` recording every departure from the written text, including the owner's answers to
the three design calls (H-1, H-2, M-10) and the two retroactively ratified internal exported symbols
(`security.NormalizeURL`, `security.PinnedDialControl`).

| Finding | Disposition |
|---|---|
| **H-1** self-declared `ReadOnly()` outranks the markers | **Fixed** — `classifyTool` now consults every unfakeable marker first and `classReadOnly` is the *terminal floor*; `git_diff_range` and `diagnostics` ride the **subproc** row. Contract **§4** amended (dated `>` block + footnote ²); tighten-only in every cell. |
| **H-2** `present_document` → OS handler | **Fixed** — rung 1's launch is bounded by `present.OpenerRenderable` (a curated display-only extension set); a non-renderable extension degrades to rung 0. ADR 0019 amended. |
| **H-3** discarded body-read error | **Fixed** — `readCappedBody` returns its error; a cut-short body reports `…was cut short: …` and an in-flight cancellation returns the Go-error shape (ADR 0007). |
| **H-4** dial-time floor never exercised | **Fixed (test)** — `TestNetworkFunnel_DialTimeFloorBlocksAfterPreflightPasses`, mutation-checked against a deleted `Control` hook. |
| **M-1** guard checks a different string than is dialled | **Fixed** — one `security.NormalizeURL`, called by `CheckContext` *and* the funnel; the request is built from the normalised URL. |
| **M-2** `%q` defeats the URL redaction | **Fixed** — the unparseable-url error no longer carries the URL; extended by item 18 so the funnel scrubs against the string it actually dials. |
| **M-3** RFC 8215 NAT64 local-use prefix | **Fixed** — `64:ff9b:1::/48` denied **wholesale** (no decode is sound), plus 6to4, IPv4-compatible and site-local. |
| **M-4** per-call transport leak | **Fixed** — `defer client.CloseIdleConnections()`. The *hoist* (pooling) half was **deferred** — see below. |
| **M-5** resolve outside the timeout budget | **Fixed** — one shared deadline covers resolve, dial and body. |
| **M-6** refused redirect hides its target | **Fixed** — the `Location` is rendered for the model. |
| **M-7** host `URLGuard` threading unpinned | **Fixed (test)** — `TestNewDefaultRegistryWithHost_ThreadsURLGuardIntoNetworkTools`, mutation-checked per tool. |
| **M-8** erroring Approver unpinned | **Fixed (test)** — `TestDispatch_ApproverErrorRefuses`, mutation-checked. |
| **M-9** floor fail-closed + numeric encodings unpinned | **Fixed (test)** — both fail-closed blocks and all four numeric forms, mutation-checked. |
| **M-10** SSRF floor over a user-typed MCP endpoint | **Fixed (policy call, owner)** — the *configured* endpoint is exempt from the pre-flight floor and its dial is **pinned to that endpoint's own addresses**; any other private address on that connection is still refused and redirects are not followed. The two docs this report flagged (`cmd/apogee/defaults/config.yaml`, `internal/mcp/doc.go`) were corrected. |

**Action-order entry 8 — the `/improve-codebase-architecture` candidate — is NOT closed here and is
not this report's to-do any more.** The duplicated `HostTools` composition
(`internal/agent/construct.go` / `cmd/apogee/wire.go`) and the duplicated guarded-client builder
(`internal/tools/network.go` / `internal/mcp/transport.go`) were **out of scope by the plan's own
ruling** and remain a deepening candidate. The owner **deferred** them on 2026-07-26 to an
architecture pass, together with M-4's hoisted-transport half and its recorded counterweight (a
pooled connection skips the dial-time re-check on later calls); the whole brief, including the
rebuttals and the counterweight, is written down as **item 19** of the archived plan, and
`internal/mcp/transport.go`'s own doc comment records that the seam is a deepening candidate. Item
19 is the only thing from this report's scope that did not land, and it was never a defect.

Nothing in `docs/reviews/archived/` is anyone's to-do list, and that now holds for this report.
