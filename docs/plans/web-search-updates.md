# Web search: zero-config DuckDuckGo default + auto-clean, keep custom endpoints

## Context

Web search doesn't work out of the box in `apogee`, for **two** reasons:

1. **Design:** the `web_search` tool is deliberately *default-off with no built-in provider*
   (`internal/tools/web_search.go:76`). An empty `web-search-endpoint` returns the graceful
   message *"web search is not configured"* — so a user with no custom search engine and no API
   key gets nothing. The predecessor tool (`~/Repos/Airic/apogee-code`) instead shipped a working
   DuckDuckGo HTML scraper baked in, which is why it "just worked."
2. **A scheme-less endpoint is silently broken:** setting
   `web-search-endpoint: html.duckduckgo.com/html/` (no `https://`) makes every request fail —
   `url.Parse` treats a scheme-less string as a path, so `Host==""` and url-safety rejects it
   ("blocked by url-safety"). *(Correction found during implementation: this value was an
   uncommitted local edit to `cmd/apogee/defaults/config.yaml:52` — the shipped v1.0.0 template
   always had the endpoint line commented out, per `git log -S html.duckduckgo`. The failure mode
   is real for any hand-edited config; the "shipped template bug" framing was wrong.)*

**Goal (decisions confirmed with the user):**
- **Default ON:** when `web-search-endpoint` is empty/unset, default to DuckDuckGo HTML — works
  with no config and no API key. This intentionally reverses the documented default-off decision,
  so the stale docs get updated too.
- **`off` sentinel** disables the tool (`web-search-endpoint: off` → graceful "web search is
  disabled"). Reuses the existing key; no new config plumbing.
- **Auto-clean by Content-Type (with body-sniff):** HTML responses are cleaned into readable
  `title / url / snippet` results; already-clean JSON/text endpoints pass through unchanged. This
  gives both a clean default *and* support for a user's own clean backend.

Also: the M2 API-key-redaction path stays intact for custom endpoints, and the SSRF floor stays on
throughout (DuckDuckGo is public and passes it).

## Behavior spec (the dispatch rule)

After the request, decide rendering (`renderSearch(provider, resp, body, query, truncated)`):

- **non-2xx** → `errorResult` (host-scrubbed via existing `endpointHost`), unchanged from today.
- **provider == duckduckgo (the default)** → *always* run the structured DDG parse regardless of
  Content-Type. `≥1` result → numbered `title/url/snippet` render. `0` results → `"No results
  found for: <query>"`. Do **not** fall through to generic strip-tags (a DDG rate-limit/consent
  page has no `result__a`; dumping its text is noise). DDG is best-effort — rate-limiting yields
  "No results", never a crash.
- **custom endpoint** → "looks like HTML" if `Content-Type` contains `html` **OR** body sniffs
  `<!doctype html`/`<html` (only trust the `result__` marker for the DDG provider). If HTML: try
  structured DDG parse first (covers a self-hosted DDG mirror), else generic strip-tags → clean
  text. If not HTML: **raw pass-through** (today's `renderSearchResult`, `HTTP <status>` + body),
  preserving JSON/text behavior exactly.

## Changes

### 1. `internal/tools/web_search.go`
- Add `const defaultSearchEndpoint = "https://html.duckduckgo.com/html/"`.
- Add fields to `WebSearch`: a provider marker (`isDefault bool` or a small `provider` enum) and
  `disabled bool`.
- Rewrite `NewWebSearch(guard, endpoint)`:
  - `TrimSpace`; **empty** ⇒ `endpoint = defaultSearchEndpoint`, provider = duckduckgo.
  - case-insensitive `off` / `none` / `disabled` ⇒ `disabled = true` (store the bool, never the
    string as an endpoint).
  - else ⇒ custom endpoint (provider = custom).
  - **Scheme normalization / self-heal:** if a custom endpoint parses to an empty `Host`, prepend
    `https://` and re-parse. This heals existing broken configs that still carry the scheme-less
    `html.duckduckgo.com/html/` from the buggy template.
- In `Execute`: if `t.disabled` ⇒ `okResult(call.ID, "web search is disabled")` (graceful,
  `IsError=false`, no request). Keep the empty-query guard. **Keep the url-safety /
  transport-error / M2 host-scrub paths exactly as they are** (`guard.CheckContext`,
  `networkURLError`, `scrubURLError`, `endpointHost`).
- Before `client.Do`, set request headers: a **browser-like** `User-Agent`
  (`Mozilla/5.0 … Safari/537.36`), `Accept: text/html`, `Accept-Language: en-US,en;q=0.9`. Do
  **not** set `Accept-Encoding` (let Go's transport manage gzip; a manual value yields a gzip body
  the parser can't read).
- Replace the final `renderSearchResult(resp, body, truncated)` call with the new
  `renderSearch(provider, resp, body, args.Query, truncated)` dispatcher.
- Update the type / `Description()` / `NewWebSearch` doc comments — no longer "default-off /
  not configured / no hard-wired provider."

Reuse unchanged: `newHTTPClient`, `readCappedBody`, `maxNetworkResponseBytes`,
`defaultNetworkTimeout` (`internal/tools/network.go`); `okResult`/`errorResult`
(`internal/tools/tools.go`). Do **not** touch `newHTTPClient`'s redirect policy (shared with
`web_fetch`/`http_request`; a DDG 3xx has no `result__a` and simply renders "No results").

### 2. `internal/tools/web_search_render.go`  (NEW — keeps `web_search.go` focused on flow)
Package-scope compiled regexes (verbatim from the `apogee-code` oracle; `[\s\S]` is valid in Go
RE2 — `(?s).*?` used for readability):

```go
var (
    ddgResultLinkRE = regexp.MustCompile(`(?s)<a[^>]+class="result__a"[^>]*href="([^"]*)"[^>]*>(.*?)</a>`)
    ddgSnippetRE    = regexp.MustCompile(`(?s)<a[^>]+class="result__snippet"[^>]*>(.*?)</a>`)
    ddgUddgRE       = regexp.MustCompile(`uddg=([^&]+)`)
    htmlTagRE       = regexp.MustCompile(`(?s)<[^>]+>`)
)
```

Functions:
- `parseDDGResults(body string) []searchResult` — find links; real URL via `ddgUddgRE` +
  `url.QueryUnescape` (fallback to raw href on error; skip results whose url stays a bare
  `//duckduckgo.com/l/…` redirector); find snippets; pair by index; `cleanHTMLText` title+snippet;
  skip if title or url empty; parse cap 20, render first 5.
- `cleanHTMLText(s string) string` — `htmlTagRE.ReplaceAllString(s, " ")` → `html.UnescapeString`
  (stdlib `html`, decodes `&amp; &lt; &gt; &quot; &#x27; &nbsp;` and folds nbsp to U+00A0) →
  `strings.Join(strings.Fields(s), " ")` to collapse whitespace. (Replaces the oracle's manual
  entity list — stdlib is strictly more complete.)
- `contentLooksHTML(contentType, body string) bool` — header contains `html`, or body prefix
  sniffs `<!doctype html` / `<html`.
- `renderStructuredResults(results []searchResult) string` — numbered `N. title\n   url\n   snippet`.
- `renderSearch(...)` — the dispatch tree in the Behavior spec. Keep/inline `renderSearchResult`
  for the raw pass-through branch.

### 3. `cmd/apogee/defaults/config.yaml`  (fix the live bug + document new semantics)
- **Comment out** line 52 so unset ⇒ the code default (single source of truth).
- Rewrite the comment block (47–51): "web_search defaults to **DuckDuckGo** (HTML, no API key
  needed) — works out of the box. Set `web-search-endpoint: off` to disable, or point it at your
  own search backend (it receives the query as the `q` URL parameter; HTML responses are cleaned,
  JSON/text passed through). Config-file only. The SSRF floor (loopback/private/metadata blocked by
  resolved IP) always applies."

### 4. Doc/comment sync (no behavior change) — remove stale "default-off / not configured"
`grep -rn "default-off\|not configured\|no hard-wired provider"` and update:
`internal/domain/config.go` (`WebSearchEndpoint` doc), `cmd/apogee/config.go`
(`settings.webSearchEndpoint`, `layer.webSearchEndpoint`, `fileConfig.WebSearch` comments),
`internal/tools/registry.go` (`HostTools.WebSearchEndpoint`), `internal/tools/doc.go` (P3.11),
`internal/agent/loop.go` (web_search comments), `cmd/apogee/root.go`. Also update the stale note
in `TODO.md` (~lines 159–160) and `docs/design/technical-design.md` (~line 189), and add a
`CHANGELOG.md` entry that calls out: the scheme self-heal in step 1 repairs hand-edited
scheme-less endpoints (the shipped template never carried a broken value — see the corrected
Context above; first-run seeding never overwrites an existing config).

### 5. Tests
- `internal/tools/network_test.go`:
  - **Rewrite** `TestWebSearch_UnconfiguredIsGraceful` → `TestWebSearch_EmptyEndpointDefaultsToDuckDuckGo`:
    white-box assert `NewWebSearch(loopbackGuard(), "").endpoint == defaultSearchEndpoint` — **no
    `Execute`**, so the unit test never dials the real DuckDuckGo. (Critical: after this change an
    empty-endpoint `Execute` would hit the live network.)
  - Add `TestWebSearch_OffSentinelIsGraceful`: `Execute` with `"off"` → `!IsError`, contains
    "disabled", and *no HTTP request is made* (httptest handler flips a bool that must stay false).
  - Keep `TestWebSearch_QueriesConfiguredEndpoint`, add an assertion the JSON body passes through
    verbatim.
- `internal/tools/web_search_render_test.go` (NEW), all via `httptest.NewServer` + `loopbackGuard()`:
  - `TestWebSearch_ParsesDuckDuckGoHTML` — serve a DDG fixture (two `result__a` + `result__snippet`
    blocks, `uddg=`-wrapped URLs, entities); assert decoded real URLs, titles, snippets, entities
    decoded, no `<...>` tags remain.
  - `TestWebSearch_NoContentTypeStillParsesHTML` — same fixture, no Content-Type header → still
    parses (body-sniff).
  - `TestWebSearch_CleansGenericHTML` — non-DDG HTML with tags/entities → stripped/decoded/collapsed.
  - `TestWebSearch_JSONPassthrough` — `application/json` body returned verbatim.
  - Optional: direct `parseDDGResults` unit test on a fixture string (fastest).

## Verification

- `go build ./...`
- `go test ./internal/tools/... ./cmd/apogee/...` (hermetic — no test hits the real network).
- Manual end-to-end (optional, needs outbound internet):
  - Fresh config (no `web-search-endpoint`): run apogee, ask a question that triggers `web_search`
    → expect cleaned numbered DuckDuckGo results (in ask-before mode it still prompts for the
    network reach; approve it).
  - `web-search-endpoint: off` → tool reports "web search is disabled," turn continues.
  - Custom JSON endpoint → raw JSON passed through unchanged.
- Confirm the fixed template: a brand-new `~/.apogee/config.yaml` has the `web-search-endpoint`
  line commented; an install still carrying the scheme-less value now self-heals to `https://`.

## Notes / risks
- DuckDuckGo HTML is best-effort: it rate-limits by IP and can serve a consent/challenge page
  (200 with no `result__a`, or 202) — handled by the graceful "No results" fallback; **no retry
  loops**. If DDG flips result attribute order (`href` before `class`), parsing yields zero
  results (graceful), not a crash — the most likely future breakage vector.
- Security envelope is unchanged: SSRF floor stays on (pre-flight + at every dial via
  `SafeDialControl`), DDG has no API key so nothing for M2 redaction to leak, and web_search is
  still an `EffectNetwork` tool that gates on approval in the default ask-before mode. In-process
  `net/http` is not subject to the seatbelt/subprocess confinement.
