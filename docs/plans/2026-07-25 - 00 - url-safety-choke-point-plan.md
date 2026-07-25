# Plan — One choke point for url-safety: the network funnel carries the marker

**Date:** 2026-07-25
**Status:** READY (design grilled 2026-07-25; all five forks resolved by the owner — no
needs-design-call escalation expected). Runs in `internal/tools` + `internal/agent` (+ ADR 0012,
CONTEXT.md, CHANGELOG, TODO.md). **Public API unchanged** — no ADR 0010 version bump.
**Source:** candidate **02** of `docs/reviews/2026-07-24 - 00 - architecture-deepening-review.md`
("One choke point for url-safety", rated Strong). Candidate 01 landed 2026-07-24.
**Track:** post-`v0.8.0` architecture deepening — makes ADR 0012's `url-filtered` promise true by
construction instead of by each tool's discipline.
**Standing requirement:** `/coding-standards` (Go + testing variants) mandatory — invoke
`implement-plan` with `coding-standards` forwarded. Pre-production: commit direct to `main`, no
PRs. `go test ./...`, `go test -race ./...`, `gofmt`, `go vet` green gate every item.

---

## The problem (grounded, verified 2026-07-25)

**1. The ladder promises more than the ADR ever did.** ADR 0012 says *"**Apogee's own** network
tools (`web-fetch`, `http-request`) **auto-run** (filtered by url-safety)"*. But `classifyTool`
(`internal/agent/resolution.go:241-246`) puts **any** tool declaring `domain.EffectNetwork` into
`classNetwork`, and `resolveLadderAuto` (`resolution.go:311-313`) auto-runs the whole class with no
Approval. A host-registered tool that declares `EffectNetwork` reaches the network **unattended and
unfiltered** in Auto, and dispatch cannot tell the difference. The ladder generalized a promise the
ADR scoped to our own tools.

**2. The policy is not even reachable by anyone else.** The `URLGuard` is construction-injected —
`NewWebFetch(host.URLGuard)` at registry-build time (`internal/tools/registry.go:101-103`). A tool
Apogee did not build never receives the host's url-safety policy, so it *could not* be url-safe
even if it wanted to be. There is no ctx handle for url-safety the way there is for Confinement
(`domain.WithConfinement`, `confinement.go:92`).

**3. The ladder already solves this — for writes.** The write axis has exactly the split network
lacks: `classWorkspaceWrite` (carrying the **unexported, unfakeable** `workspaceScopedWriter`
marker — Apogee's own path-safety-bounded write, auto-runs) versus `classThirdPartyWrite`
("write-capable, none of the above (can't vouch for scoping)", gates). `CONTEXT.md:205-207` states
that rule in prose for writes and is silent on network. **That asymmetry is the finding.**

**4. Each network tool re-runs the same trio.** `guard.CheckContext` → `newHTTPClient` →
classify-and-render-the-error appears in `web_fetch.go:67,76-86`, `http_request.go:117,133-143`,
`web_search.go:142,168-180`. The review called this "the copied trio"; it is **not** uniform, and
the difference is load-bearing: `web_search` renders `endpointHost` and `scrubURLError(err, reqURL)`
because a configured search endpoint carries an API key in its query (security-review M2,
`web_search.go:134-140,177-179`, guarded by `web_search_redaction_test.go`). `web_fetch` and
`http_request` interpolate `err.Error()` raw — which for a parse failure embeds the URL. So the
leak-safety M2 bought is **one tool's private discipline**, exactly the shape this card exists to
remove.

**Test exposure is low.** Only one assertion touches the blocked-result text
(`network_test.go:97,289`, a loose `strings.Contains(res.Content, "url-safety")`), so message
wording is cheap to change; `web_search_redaction_test.go` is the guardrail that fails if the
funnel ever leaks a key-bearing URL. On the agent side, four sites use a **fake**
`externalTool{kind: domain.EffectNetwork}` and assert the auto-run/reason behaviour
(`dispatch_test.go:164,219,740`; `resolution_test.go:40,87-91`) — under this change those fakes
become the *third-party* case and the vouched-for assertions move onto the real tool, mirroring how
`dispatch_test.go:155` already classifies `tools.NewWriteFile(ws)`.

---

## Decisions (grilled 2026-07-25)

- **D1 — marker + funnel, not a dispatch pre-flight.** An unexported marker in `internal/tools`,
  obtainable **only** by embedding the funnel struct that owns the guard. Rejected: a dispatch-level
  check keyed on an exported `NetworkTargets(call) []string` seam — it is still a declaration (a
  tool that under-declares is unfiltered anyway), and it adds public API. Rejected: funnel-only with
  the ladder untouched — that leaves the Auto hole open.
- **D2 — an unvouched network tool GATES in Auto.** Same answer the ladder already gives
  `classMCP` and `classThirdPartyWrite`; ADR 0012's own words are *gating is how `confine=true`'s
  promise stays honest*. Tool-name cache key, its own gate reason. With no Approver configured,
  `finishGate` already turns that into a Refuse — no new rule needed. Strictly **tighten-only**: a
  call that auto-ran now prompts, never the reverse. `confine-to-workspace: false` ("I am the
  sandbox") returns `resolveRun` **before** the class switch (`resolution.go:295-299`) and is
  therefore **unaffected**.
- **D3 — the funnel owns check + client + request + the model-facing failure message**, scrubbed to
  the bare host. M2's protection stops being `web_search`'s alone and becomes every network tool's
  by construction. Each tool keeps its own **result** rendering (body + content-type; sorted header
  list; parsed search hits) and its own non-2xx policy — those genuinely differ.
- **D4 — ADR 0012 gains an `## Amendment`** (the convention it already uses, see its
  `## Amendment (2026-07-21)`), CONTEXT.md gains the network half of the vouching clause. No new
  ADR: this is 0012 being made faithful to its own words, not a new decision.
- **D5 — no embedder opt-in in this plan.** The marker stays unexported, exactly like
  `workspaceScopedWriter`, under which an embedder's write tool has gated in Auto since Phase 3.
  The door is documented in `TODO.md`, not silently shut: exporting the funnel is the natural move
  if embedder demand appears.
- **Names (by symmetry with the write axis):** `urlFilteredNetworker` (unexported marker) /
  `IsURLFilteredNetworker` (exported accessor) ↔ `workspaceScopedWriter` /
  `IsWorkspaceScopedWriter`; `classThirdPartyNetwork` ↔ `classThirdPartyWrite`; the embedded struct
  is `networkTool`.

## Explicit non-goals

- **The MCP transport's endpoint check** (`internal/mcp/transport.go:144`) stays as it is — it
  guards a *server connection* at connect time, a different lifecycle from a per-call tool URL.
- **The `ExternalEffects` bench-stub path** (ADR 0008): `runTool` (`dispatch.go:356-361`) routes an
  external-effect tool to the injected stub *instead of* `Execute`, so the funnel does not run under
  the bench. That is correct (a deterministic stub reaches no network) and must not be "fixed".
- **Subprocess network reach** (`git fetch`, `terminal` running `curl`) is out of scope by design —
  ADR 0012: *"a subprocess can already `curl` the same host"*.
- **No redirect-policy change.** `newHTTPClient`'s `CheckRedirect` → `ErrUseLastResponse`
  (`network.go:67-69`) and the dial-time `SafeDialControl` floor are moved intact, not revisited.

---

## 1. The network funnel and its marker — `internal/tools/network.go` — ✅ DONE (2026-07-25)

NOTES (2026-07-25): three deviations from the literal text. (a) `web_search.go` IS touched, as the
item's own "moved … until then it keeps using them under the new names" requires: `endpointHost` →
`safeHost` and `scrubURLError` (plus its only helper `redactSubstring`) now live in `network.go`, and
`web_search`'s single call site is repointed to `safeHost`. Nothing else in the three tools changed —
they still hold their own `guard` and reach the network themselves (items 2–3). Side effect of the
mandated generalization: `web_search`'s unparseable-endpoint placeholder now reads "the requested
host" instead of "the configured search endpoint". (b) `netRequest.timeout` is a `time.Duration`
while `clampTimeout` takes int seconds, so the ceiling was factored into `clampDuration(time.Duration)`
and `clampTimeout` now delegates to it — one ceiling, both entry points (tested both ways). (c) the
blocked message is rendered by a tiny `blockedMessage(label, err, rawURL)` helper because the
pre-flight and dial-time branches emit the same string; wording is exactly as specified.

The funnel is a struct the network tools **embed**; embedding is the only way to obtain the marker,
so the marker cannot exist without the guard. Nothing is ported onto it in this item — the three
tools keep working exactly as they do (items 2 and 3 move them), so this item is purely additive.

- **`type networkTool struct{ guard security.URLGuard }`** — replaces the `guard
  security.URLGuard` field each tool holds today.
- **`func (networkTool) urlFiltered() {}`** — the unexported marker method satisfying
  `urlFilteredNetworker`, with the interface declared beside it. Doc comment states the contract:
  *carrying this marker asserts every outbound URL of this tool passed the host's `URLGuard`; it is
  unfakeable outside this package, and dispatch trusts it to auto-run the tool unattended in Auto.*
- **`func IsURLFilteredNetworker(t domain.Tool) bool`** — the exported accessor `internal/agent`
  calls, mirroring `IsWorkspaceScopedWriter`.
- **`type netRequest struct`** — `url string` (the URL actually requested), `method string` (empty ⇒
  GET), `body io.Reader`, `header http.Header` (nil ⇒ none), `timeout time.Duration` (≤ 0 ⇒
  `defaultNetworkTimeout`, run through the existing `clampTimeout` ceiling), and `safeLabel string`
  — the caller-supplied host-only string used in every model-facing failure message (empty ⇒ derived
  from `url` via `safeHost`).
- **`type netResponse struct`** — `status string`, `statusCode int`, `header http.Header`,
  `body string`, `truncated bool`.
- **`func (n networkTool) do(ctx context.Context, req netRequest) (netResponse, string, error)`** —
  the single path to the network. In order: `ctx.Err()`; `n.guard.CheckContext(ctx, req.url)`;
  `newHTTPClient(n.guard, timeout)`; `http.NewRequestWithContext` + headers; `client.Do`;
  `readCappedBody`. Returns `(resp, "", nil)` on success; `(netResponse{}, msg, nil)` where `msg` is
  a **ready-to-surface, URL-scrubbed** failure message for a blocked URL, a build failure, or a
  transport error; `(netResponse{}, "", err)` **only** for ctx cancellation (ADR 0007 — a tool
  returns a Go error only for cancellation). The three messages:
  - blocked (pre-flight or the dial-time floor, detected with the existing `networkURLError`):
    `"url blocked by url-safety (host <safeLabel>): <reason>"`, where `<reason>` is the guard error
    passed through `scrubURLError` so a key-bearing URL cannot ride along;
  - build failure: `"could not build request for host <safeLabel>: <scrubbed>"`;
  - transport failure: `"request to host <safeLabel> failed: <scrubbed>"`.
- **`safeHost(rawURL string) string`** and **`scrubURLError(err error, rawURL string) string`** —
  `endpointHost` (`web_search.go:196-202`) and `scrubURLError` (`web_search.go:209+`) **moved
  verbatim** from `web_search.go` into `network.go` and generalized in name only (the
  unparseable-input placeholder becomes `"the requested host"`). Item 3 repoints `web_search`'s
  callers; until then it keeps using them under the new names.
- **File-head doc comment** (`network.go:14-29`) is rewritten: the paragraph *"url-safety is the
  tool's own concern (a tool-local guard), threaded from the host like path-safety is for the file
  tools"* is now false and is replaced by the funnel contract — url-safety is applied **by the
  funnel**, the marker is what dispatch trusts, and a tool that does not route through the funnel
  does not carry the marker and gates in Auto.
- Marker assertions beside the existing interface assertions (`network.go:106-113`):
  `_ urlFilteredNetworker = (*WebFetch)(nil)` and the same for `*HTTPRequest`, `*WebSearch` — these
  compile only once items 2–3 land, so **add them in item 3**, not here.

**Tests** (`network_funnel_test.go`, new):

- marker accessors, mirroring `workspace_scoped_test.go`: a struct embedding `networkTool` satisfies
  `IsURLFilteredNetworker`; `NewReadFile(root)` does not;
- `do` against an `httptest` server: success returns status/header/body; a body over
  `maxNetworkResponseBytes` reports `truncated`;
- `do` with a loopback URL and the default (floor-on) guard returns a blocked message naming
  `url-safety`, empty `netResponse`, nil error;
- **the M2 generalization**: `do` with a key-bearing URL (`…/x?key=SUPER-SECRET…`) against a closed
  server returns a transport message that contains the host and **does not contain the key**; same
  for the blocked path with a key-bearing loopback URL;
- a cancelled ctx returns a Go error, not a message (ADR 0007);
- `timeout` ≤ 0 uses the default and an over-ceiling timeout clamps (assert via `clampTimeout`
  behaviour, not wall-clock).

**Acceptance:** gates green; `network.go` compiles with the three tools **unchanged**; no exported
symbol added beyond `IsURLFilteredNetworker`; `go doc ./internal/tools` shows no new public type.
Commit: `feat(tools): url-filtered network funnel — the marker rides with the guard`.

---

## 2. `web_fetch` and `http_request` onto the funnel — ✅ DONE (2026-07-25)

NOTES (2026-07-25): four deviations/additions. (a) the renderers are **not** literally untouched:
`renderFetchResult` / `renderRequestResult` now take a single `netResponse` instead of
`(*http.Response, body, truncated)` — the funnel never yields an `*http.Response`, so the
signature had to move; the formatting logic is unchanged line for line (only field accessors:
`resp.Status`→`resp.status`, `resp.Header`→`resp.header`). (b) `applyRequestHeaders` now takes only
the map and **returns** `(http.Header, errMsg)` instead of mutating a request — the form
`netRequest.header` needs; its deny-list, count cap and value cap are unchanged. Side effect: the
header filter now runs *before* url-safety (it used to run after the tool's own `CheckContext`), so
a rejected header block short-circuits before any guard/DNS work — same messages, same
"request never went out" guarantee. (c) `timeout` is passed as `clampTimeout(args.TimeoutSeconds)`,
the funnel's own seconds-typed entry point (defined in `network.go`), rather than converting
`TimeoutSeconds` inline: identical resolution (the funnel's `clampDuration` re-run is idempotent),
and it keeps `clampTimeout` the live `http_request` entry point item 1's funnel test documents.
(d) fixed the M2 residual item 1's verifier flagged, now that `do` is model-reachable: url-safety
parses the **trimmed** URL and quotes it in its "unparseable url" reason, so `scrubURLError`
redacts both the raw and the trimmed form (new `redactRequestURL` helper in `network.go`);
regression case in the new `network_test.go` table, verified to fail without the fix.

The simple pair — both do check → client → do → render. All in `internal/tools/web_fetch.go` and
`internal/tools/http_request.go`.

- `WebFetch` / `HTTPRequest` embed `networkTool` in place of their `guard security.URLGuard` field;
  `NewWebFetch(guard)` / `NewHTTPRequest(guard)` keep their exact signatures and construct
  `networkTool{guard: guard}`.
- Each `Execute` keeps: the ctx check, `decodeToolArgs`, its argument validation
  (`url is required`; `http_request`'s method allow-list and `applyRequestHeaders` filter), and its
  renderer (`renderFetchResult` / `renderRequestResult`, both untouched).
- Everything between validation and rendering collapses to one `n.do(ctx, netRequest{…})` call plus
  `if msg != "" { return errorResult(call.ID, msg), nil }`. The `guard.CheckContext` call, the
  `newHTTPClient` construction, the `client.Do`, the `networkURLError` branch and the raw
  `err.Error()` interpolation are **deleted** from both files.
- `http_request` passes `timeout: clampTimeout(args.TimeoutSeconds)`'s input (its
  `TimeoutSeconds`) through `netRequest.timeout`; the clamp now lives in the funnel.
- `applyRequestHeaders` still runs **before** `do` (it can reject the call), and hands the built
  `http.Header` to `netRequest.header`. Its deny-list, count cap and value cap are untouched.
- Doc comments on both types drop *"filtered by the same URLGuard … pre-flight and at dial time"* as
  a self-description and instead say the tool **routes through the network funnel**, which is what
  carries the marker.
- **Behaviour change to record:** a blocked/failed `web_fetch` or `http_request` message is now
  host-scoped and URL-scrubbed rather than echoing `err.Error()` raw. This is a fix, not a
  regression: it closes the same leak M2 closed for `web_search`.

**Tests** (`network_test.go`, existing):

- the two loose `strings.Contains(res.Content, "url-safety")` assertions (`:97`, `:289`) must still
  pass unchanged — they are the compatibility check on the new wording;
- add, for **each** of the two tools, the M2 pair now that it applies to them: a key-bearing URL
  that is blocked, and one whose transport fails, must produce a result naming the host and **not**
  the key (the same shape as `web_search_redaction_test.go`);
- the existing success, 404/status, header-filter, timeout-clamp and cap tests must pass with no
  edit — if one needs editing, the port changed behaviour and the item is wrong.

**Acceptance:** gates green; `grep -n "CheckContext\|newHTTPClient" internal/tools/web_fetch.go
internal/tools/http_request.go` returns **nothing**; both files shrink. Commit:
`refactor(tools): web_fetch and http_request reach the network only through the funnel`.

---

## 3. `web_search` onto the funnel, and the marker assertions

The delicate one — it has the disabled sentinel, two provider request shapes, a non-2xx policy of
its own, and it is where the M2 scrubbing came from. All in `internal/tools/web_search.go`.

- `WebSearch` embeds `networkTool` in place of its `guard` field; `NewWebSearch(guard, endpoint)`
  keeps its signature and all of its endpoint resolution (empty ⇒ DuckDuckGo, off-sentinels,
  scheme-healing, DDG-host detection) **verbatim**.
- `Execute` keeps, in order and unchanged: ctx check, `decodeToolArgs`, the empty-query check, the
  `disabled` graceful result, the provider branch that builds `reqURL` (DDG ⇒ bare endpoint,
  custom ⇒ `buildSearchURL`), and the DDG browser headers.
- The funnel call carries `safeLabel: safeHost(t.endpoint)` — so every failure message keeps naming
  **the configured endpoint's** host, exactly as `endpointHost` did, and the key-bearing `reqURL`
  never reaches a model-facing string.
- `web_search` keeps its **own non-2xx policy**: a non-2xx is an error result naming status + host
  and dropping the body (`web_search.go:184-188`) — the funnel returns the response and does not
  decide this. `renderSearch`, `buildSearchURL`, the provider table and the redaction helpers'
  behaviour are untouched.
- Local `endpointHost` / `scrubURLError` definitions are **removed** (they now live in `network.go`
  from item 1); the callers repoint to `safeHost` / the funnel's message.
- Add the three marker assertions to `network.go`'s assertion block:
  `_ urlFilteredNetworker = (*WebFetch)(nil)`, `(*HTTPRequest)(nil)`, `(*WebSearch)(nil)`.
- **The forget-proof test** (`network_funnel_test.go`): walk `DefaultToolsWithHost(root,
  HostTools{})` and assert that **every** tool implementing `domain.ExternalEffectTool` with
  `ExternalEffect() == domain.EffectNetwork` satisfies `IsURLFilteredNetworker`. A future built-in
  network tool that hand-rolls its own `http.Client` fails this test. Its doc comment must say so —
  this test *is* the "impossible to forget" property for our own tools.

**Tests:**

- `web_search_redaction_test.go` passes **unchanged** — it is the acceptance oracle for this item;
- `web_search_render_test.go` and the search cases in `network_test.go` (disabled sentinel, scheme
  healing, DDG POST shape, custom-endpoint GET shape, non-2xx, HTML cleaning) pass unchanged;
- the registry-walk test above.

**Acceptance:** gates green; `web_search_redaction_test.go` untouched and green; the registry-walk
test fails if any assertion is removed (verify by temporarily deleting one). Commit:
`refactor(tools): web_search onto the funnel — the API-key scrub is now every network tool's`.

---

## 4. The ladder splits vouched-for network from third-party — `internal/agent/resolution.go`

- **`classThirdPartyNetwork`** joins the `toolClass` enum after `classNetwork`, documented as *an
  `EffectNetwork` tool Apogee cannot vouch for — it does not carry the url-filter marker, so its
  URLs are unfiltered and it gates.* The enum's doc block (`resolution.go:215-218`) updates: the
  order comment now reads read-only → workspace-writer → **vouched network** → third-party network →
  MCP → subprocess → third-party write.
- **`classifyTool`** (`resolution.go:241-246`) — inside the existing `ExternalEffectTool` branch,
  the network case becomes `if tools.IsURLFilteredNetworker(tool) { return classNetwork }; return
  classThirdPartyNetwork`. Everything else in the priority order is untouched.
- **`resolveLadderAuto`** (`resolution.go:302-326`) gains `case classThirdPartyNetwork: return
  resolution{kind: resolveGate}` with the comment *unfiltered network reach — Apogee cannot vouch
  for its URLs, so it gates (the network analogue of classThirdPartyWrite)*. `classNetwork`'s
  `resolveRun` row and its "the network is open (ADR 0012)" comment are unchanged. The
  `!in.confineToWorkspace` early return above the switch is untouched — **"I am the sandbox" still
  auto-runs everything**.
- **`gateReason`** (`resolution.go:444-457`) gains `case classThirdPartyNetwork: return "unfiltered
  network reach"`, so the Approval prompt tells the human which kind of reach they are authorising.
  `classNetwork` keeps `"network reach"`. Note the knock-on: in **Ask-Before / Allow-Edits / an
  unknown mode**, where every non-read-only tool already gates, an unvouched network tool's prompt
  reason changes from `"network reach"` to `"unfiltered network reach"` — a strictly more honest
  prompt, no disposition change.
- **`gateCacheKey`** (`resolution.go:399-406`) is untouched: only `classMCP` gets the server grain;
  the new class falls through to the tighter per-tool key.
- No change to `dispatch.go` — classification is the only seam that moves.

**Tests** (`internal/agent/resolution_test.go`, `internal/agent/dispatch_test.go`):

- `resolution_test.go:40` — `net` becomes the **real** `tools.NewWebFetch(security.URLGuard{})` (the
  vouched case, keeping every existing row incl. the `"network reach"` reasons at `:87-91`), and a
  new `tpn := externalTool{name: "3p-net", kind: domain.EffectNetwork}` (unvouched) gets its own
  rows: **Auto/confine=true ⇒ `resolveGate` with `"unfiltered network reach"`**, Auto/confine=false
  ⇒ `resolveRun`, Ask-Before ⇒ gate, Plan ⇒ refuse;
- `dispatch_test.go:164` — the `classifyTool` table gains both cases: `tools.NewWebFetch(…)` ⇒
  `classNetwork`, the fake ⇒ `classThirdPartyNetwork`;
- `dispatch_test.go:219` ("native network auto-runs, no Approval") — swaps to the real
  `tools.NewWebFetch(…)`. It cannot use the fake's `ran` counter, so it asserts `approver.calls ==
  0` plus a returned tool result (a bare `{}` call yields the tool's own `"url is required"` error
  result, which proves `Execute` ran without gating);
- a **new** sibling test: the same drive with the *fake* `EffectNetwork` tool consults the Approver
  exactly once and does not run the tool when denied;
- `dispatch_test.go:740` (ExternalEffects routing) is unaffected — it runs with `confine=false`, and
  routing keys on `domain.ExternalEffectTool`, not on the class. Verify, do not edit.
- the no-Approver case needs no new rule but **does** need a row: unvouched network + no Approver in
  Auto ⇒ `resolveRefuse` with `noApproverReason` (proves `finishGate` covers it).

**Acceptance:** gates green including `-race`; the sub-agent tests (`subagent_test.go`) and
`statemachine_test.go` pass with no edit; no test asserts that a fake `EffectNetwork` tool auto-runs
in Auto with `confine=true` any more. Commit:
`feat(agent): an unvouched network tool gates in Auto — url-safety is vouched-for by construction`.

---

## 5. Record it — ADR 0012 amendment, CONTEXT.md, docs, CHANGELOG, TODO

- **`docs/adr/0012-…md`** — append `## Amendment (2026-07-25) — url-safety is vouched-for by
  construction; an unvouched network tool gates`, following the shape of its existing
  `## Amendment (2026-07-21)`. Content: the original decision scoped the Auto auto-run to *Apogee's
  own* network tools, but the ladder keyed on the `EffectNetwork` kind and so covered any tool that
  declared it; the class now requires the unexported url-filter marker (obtainable only by routing
  through the funnel), and an `EffectNetwork` tool without it gates like MCP and third-party writes.
  Record the trade-off named in D5: an embedder cannot mint a vouched-for network tool, the same
  second-class status the write axis has had since Phase 3, chosen over exporting the funnel now.
  State that `confine-to-workspace: false` is unaffected and that the change is tighten-only.
- **`CONTEXT.md` — Confinement entry (~L205-207)**: extend the vouching clause so it names both
  axes — *"…**or** by Apogee's own **path-safety-to-workspace** for its own in-process write tools
  and **url-safety** for its own network tools (a third-party tool of either kind, whose scoping
  Apogee cannot vouch for, gates instead of running unsupervised)"*. Prose only, no implementation
  detail, no new headword.
- **`CONTEXT.md` — Safety guardrails entry (~L257-264)**: the url-safety clause gains that the
  guard is applied through the network funnel, so a network tool cannot reach the network
  unfiltered; the `web_search` API-key-redaction clause generalizes to *every network tool's
  failure message names only the bare host, never the key-bearing request URL*.
- **`internal/tools/doc.go` (~L33-43)** — the network-tools paragraph updates: the tools are
  url-filtered **because they route through the funnel**, and the marker is what the disposition
  keys on.
- **`internal/agent/doc.go`** — if it enumerates the tool classes, add the new one; otherwise leave
  it (check, do not force).
- **`CHANGELOG.md` `## [Unreleased]` → `### Changed`** — a user-facing entry: a tool that reaches
  the network but is not one of Apogee's own url-filtered tools now asks for approval in Auto
  instead of running unattended; and every network tool's failure message now names only the host,
  never the full request URL (previously true of `web_search` alone). Note that Apogee's own
  `web_fetch` / `http_request` / `web_search` behave exactly as before.
- **`TODO.md`** — a parked entry under the tools/API section recording D5: an embedder cannot
  register a vouched-for network tool because the marker is unexported (mirroring
  `workspaceScopedWriter`); if demand appears, exporting the funnel as a public
  `NewNetworkTool`-style constructor is the natural move, since it lets an embedder *inherit* the
  marker rather than fake it.
- **`docs/reviews/2026-07-24 - 00 - architecture-deepening-review.md`** — mark candidate 02 as
  landed with this plan's date, the way candidate 01 was handled, so the review stays an honest
  ledger. Leave 03–07 untouched.

**Tests:** none (docs only). Re-run the full gate to catch a doc-comment typo that breaks
`go vet`.

**Acceptance:** ADR 0012 renders with both amendments; CONTEXT.md contains no implementation
detail; the review doc no longer lists 02 as outstanding. Commit:
`docs(adr,context,changelog): record url-safety as a vouched-for guardrail`.

---

## Verification (whole plan)

1. `go test ./... && go test -race ./... && gofmt -l . && go vet ./...` — green.
2. `grep -rn "CheckContext" internal/tools/` returns **only** `network.go` (the funnel).
3. `grep -rn "newHTTPClient" internal/tools/` returns **only** `network.go`.
4. The registry-walk test in item 3 fails when a marker assertion is deleted (prove it once, then
   restore).
5. Manual: with `confine-to-workspace: true` in Auto, `web_fetch` on a public URL still runs with no
   prompt; `web_fetch` on `http://127.0.0.1/` still returns a url-safety-blocked result naming the
   host and no full URL.
6. The **live** correctness question the review flagged separately — whether any *currently
   registered* path reaches the network unfiltered today — is a `/code-audit` job, not this plan's.
   This plan fixes the shape so the audit has one place to look.
