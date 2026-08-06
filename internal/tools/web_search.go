package tools

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/security"
)

var webSearchSpec = toolSpec{
	name:        "web_search",
	description: "Search the web for a query and return the top results (title, url, snippet). Works with no configuration (DuckDuckGo by default); a host may point it at a custom search backend or disable it, in which case the tool says so instead of failing the turn.",
	schema: json.RawMessage(`{
  "type": "object",
  "required": ["query"],
  "properties": {
    "query": {"type": "string", "description": "The search query."}
  }
}`),
}

type webSearchArgs struct {
	Query string `json:"query"`
}

// defaultSearchEndpoint is the built-in provider used when no endpoint is configured:
// DuckDuckGo's HTML front-end needs no API key, so web search works out of the box.
// defaultSearchHost also marks an EXPLICITLY config'd DDG endpoint as the built-in
// provider (NewWebSearch), because DDG only works with the provider's request shape.
const (
	defaultSearchHost     = "html.duckduckgo.com"
	defaultSearchEndpoint = "https://" + defaultSearchHost + "/html/"
)

// searchProvider identifies which backend the endpoint points at, because rendering
// differs: the built-in DuckDuckGo default is always parsed structurally, while a custom
// endpoint is cleaned only when its response looks like HTML (web_search_render.go).
type searchProvider int

const (
	providerDuckDuckGo searchProvider = iota // the built-in default (endpoint unset)
	providerCustom                           // a host-configured endpoint
)

// WebSearch runs a query against a search endpoint and renders the results for the model.
// DEFAULT-ON: an empty endpoint selects the built-in DuckDuckGo provider
// (defaultSearchEndpoint), so web search works with no configuration and no API key. The
// sentinel endpoint "off" (or "none"/"disabled") disables the tool — a graceful "web search
// is disabled" result, never a crash. The DuckDuckGo provider POSTs the query as a form
// field (a bare GET gets DDG's bot-challenge page, never results); a custom endpoint
// receives the query as the `q` GET parameter (the common shape for a search backend; a
// host whose endpoint differs can front it with a thin adapter): an HTML response is
// cleaned into title/url/snippet results, a JSON/text response passes through verbatim. It
// is an ExternalEffectTool of kind network that reaches the network ONLY through the
// embedded network funnel — the funnel applies url-safety and carries the url-filter marker
// dispatch trusts (network.go). Stateless across Turns.
//
// The endpoint is the one thing about it that MOVES: `web-search-endpoint:` is a setting the
// human can commit mid-session and have take effect at once (ADR 0037), and the registry holds
// this pointer for the life of the session — so SetEndpoint re-points the tool in place rather
// than a fresh registry being built for a URL change.
type WebSearch struct {
	toolSpec
	networkTool

	// mu guards the resolved endpoint triple below. The three are one decision (resolveSearchEndpoint
	// derives them together) and are written from the host's Update goroutine while Execute reads
	// them on the loop's worker goroutine, so they are read as a set — an Execute mid-SetEndpoint
	// must never pair a new endpoint with the old provider's request shape.
	mu       sync.RWMutex
	endpoint string
	provider searchProvider
	disabled bool // the endpoint was the off sentinel — Execute reports gracefully, no request
}

// NewWebSearch returns the web_search tool for endpoint, resolved by resolveSearchEndpoint
// (an empty endpoint is the built-in DuckDuckGo default; the off sentinels disable the tool;
// anything else is a custom endpoint, filtered through guard).
func NewWebSearch(guard security.URLGuard, endpoint string) *WebSearch {
	t := &WebSearch{toolSpec: webSearchSpec, networkTool: networkTool{guard: guard}}
	t.endpoint, t.provider, t.disabled = resolveSearchEndpoint(endpoint)
	return t
}

// SetEndpoint re-points the tool at endpoint, resolved exactly as construction resolves it — so
// the sentinels disable a live tool, an empty value hands it back to the built-in provider, and a
// custom URL still self-heals. It takes effect from the next Execute; a search already in flight
// finishes against the endpoint it started on.
//
// It is the whole of what a `web-search-endpoint:` edit has to do while web_search is registered:
// the tool is stateless across Turns, so there is nothing to tear down and nothing to reconnect,
// and the registry keeps the same pointer. Building a fresh registry is reserved for the case the
// tool SET changes (Agent.SwapTools), which a URL change is not.
func (t *WebSearch) SetEndpoint(endpoint string) {
	e, p, disabled := resolveSearchEndpoint(endpoint)
	t.mu.Lock()
	defer t.mu.Unlock()
	t.endpoint, t.provider, t.disabled = e, p, disabled
}

// config reads the resolved endpoint triple as one value, so a single Execute runs against a
// single coherent configuration.
func (t *WebSearch) config() (endpoint string, provider searchProvider, disabled bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.endpoint, t.provider, t.disabled
}

// resolveSearchEndpoint turns a configured `web-search-endpoint:` value into the three things
// Execute runs on. An empty endpoint selects the built-in DuckDuckGo default; the sentinels
// "off"/"none"/"disabled" (case-insensitive) disable the tool and store no endpoint at all;
// anything else is a custom endpoint. A scheme-less custom endpoint (e.g. "search.example.com/s")
// self-heals to https:// — url.Parse reads it as a bare path (Host == ""), and url-safety would
// otherwise reject every request. An endpoint whose host is the built-in DuckDuckGo host IS the
// built-in provider: DDG answers only the provider's request shape (POST + browser headers), so a
// config'd "html.duckduckgo.com/html/" must not degrade to the custom-endpoint GET.
func resolveSearchEndpoint(endpoint string) (string, searchProvider, bool) {
	endpoint = strings.TrimSpace(endpoint)
	switch strings.ToLower(endpoint) {
	case "":
		return defaultSearchEndpoint, providerDuckDuckGo, false
	case "off", "none", "disabled":
		return "", providerDuckDuckGo, true
	}
	if u, err := url.Parse(endpoint); err != nil || u.Host == "" {
		if healed, herr := url.Parse("https://" + endpoint); herr == nil && healed.Host != "" {
			endpoint = "https://" + endpoint
		}
	}
	provider := providerCustom
	if u, err := url.Parse(endpoint); err == nil && strings.EqualFold(u.Hostname(), defaultSearchHost) {
		provider = providerDuckDuckGo
	}
	return endpoint, provider, false
}

// ExternalEffect reports that web_search reaches the network (kind network).
func (t *WebSearch) ExternalEffect() domain.ExternalEffectKind { return domain.EffectNetwork }

// Execute runs the search. A disabled tool (off sentinel) is a graceful "disabled" result;
// a blocked endpoint URL, a transport error, or a non-2xx status are surfaced as results;
// only ctx cancellation is a Go error (ADR 0007). A render that carries a numbered result
// list attaches its hit count as a domain.SearchHits summary; every other render — the
// sentinels, cleaned HTML, a verbatim pass-through — carries none.
func (t *WebSearch) Execute(ctx context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return domain.ToolResult{}, err
	}

	args, fail, ok := decodeToolArgs[webSearchArgs](call)
	if !ok {
		return fail, nil
	}
	if strings.TrimSpace(args.Query) == "" {
		return errorResult(call.ID, "query is required"), nil
	}

	// One read of the endpoint triple for the whole call: a SetEndpoint landing between the
	// disabled check and the request must not send this search somewhere the disabled check
	// never saw (ADR 0037 — the endpoint moves mid-session).
	endpoint, provider, disabled := t.config()
	if disabled {
		// The host set the off sentinel. Graceful, not an error (§3a) — the model learns
		// web search is unavailable and the turn continues. No request is made.
		return okResult(call.ID, "web search is disabled on this host (web-search-endpoint: off); web_search is unavailable."), nil
	}

	// endpointHost is the bare host of the configured endpoint — the ONLY part of the
	// endpoint safe to surface to the model. The constructed reqURL carries the query and
	// may carry a config'd API key in its parameters (the endpoint "preserves any
	// parameters it already carries"); it must never reach a model-facing or logged string
	// (security-review M2). It rides along as the funnel's safeLabel, so every failure
	// message the funnel renders names this host and nothing else.
	endpointHost := safeHost(endpoint)

	// The DuckDuckGo provider carries the query in a POST form body, so its reqURL is the
	// bare endpoint: DDG's HTML front-end answers a GET with its bot-challenge ("anomaly")
	// page — results come only over POST, the way its own search form submits. A custom
	// endpoint keeps the `q` GET-parameter contract.
	reqURL := endpoint
	if provider != providerDuckDuckGo {
		var err error
		reqURL, err = buildSearchURL(endpoint, args.Query)
		if err != nil {
			return errorResult(call.ID, "could not build search url for host "+endpointHost+": "+scrubURLError(err, endpoint)), nil
		}
	}

	method := http.MethodGet
	var reqBody io.Reader
	var header http.Header
	if provider == providerDuckDuckGo {
		method = http.MethodPost
		reqBody = strings.NewReader(url.Values{"q": {args.Query}}.Encode())
		// Browser-like headers: DuckDuckGo's HTML front-end serves challenge pages to bare
		// clients far more often. Scoped to the built-in provider so a custom backend sees
		// the same request it always did (a content-negotiating backend must keep returning
		// its clean JSON/text). No Accept-Encoding — Go's transport only transparently
		// un-gzips when it set that header itself.
		header = http.Header{}
		header.Set("Content-Type", "application/x-www-form-urlencoded")
		header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
		header.Set("Accept", "text/html")
		header.Set("Accept-Language", "en-US,en;q=0.9")
	}

	// The funnel owns url-safety (pre-flight and at dial time), the client, the request and
	// the host-scoped, URL-scrubbed failure message; web_search keeps only what genuinely
	// differs — its non-2xx policy and its rendering.
	resp, msg, err := t.do(ctx, netRequest{
		url:       reqURL,
		method:    method,
		body:      reqBody,
		header:    header,
		safeLabel: endpointHost,
	})
	if err != nil {
		return domain.ToolResult{}, err
	}
	if msg != "" {
		return errorResult(call.ID, msg), nil
	}
	if resp.statusCode < 200 || resp.statusCode >= 300 {
		// Non-2xx is a failed search, surfaced with only status + host: the body of a
		// rate-limit or challenge page is noise, and the URL must stay scrubbed (M2).
		return errorResult(call.ID, "search endpoint returned HTTP "+resp.status+" (host "+endpointHost+")"), nil
	}
	text, hits := renderSearch(provider, resp, args.Query)
	if hits == 0 {
		// Nothing numbered to count: a "No results" sentinel, a cleaned HTML page, or a
		// custom backend's verbatim document. There is no hit count to report, so the result
		// carries no summary and a host renders the prose — exactly as it does today.
		return okResult(call.ID, text), nil
	}
	return okSummary(call.ID, text, domain.SearchHits{Count: hits}), nil
}

// buildSearchURL appends the query as the `q` parameter to the configured endpoint,
// preserving any parameters the endpoint already carries (e.g. an API key).
func buildSearchURL(endpoint, query string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("q", query)
	u.RawQuery = q.Encode()
	return u.String(), nil
}
