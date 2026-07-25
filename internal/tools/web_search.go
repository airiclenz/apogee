package tools

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"

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
type WebSearch struct {
	toolSpec
	networkTool
	endpoint string
	provider searchProvider
	disabled bool // the endpoint was the off sentinel — Execute reports gracefully, no request
}

// NewWebSearch returns the web_search tool. An empty endpoint selects the built-in
// DuckDuckGo default; the sentinels "off"/"none"/"disabled" (case-insensitive) disable the
// tool; anything else is a custom endpoint, filtered through guard. A scheme-less custom
// endpoint (e.g. "search.example.com/s") self-heals to https:// — url.Parse reads it as a
// bare path (Host == ""), and url-safety would otherwise reject every request. An endpoint
// whose host is the built-in DuckDuckGo host IS the built-in provider: DDG answers only the
// provider's request shape (POST + browser headers), so a config'd
// "html.duckduckgo.com/html/" must not degrade to the custom-endpoint GET.
func NewWebSearch(guard security.URLGuard, endpoint string) *WebSearch {
	endpoint = strings.TrimSpace(endpoint)
	switch strings.ToLower(endpoint) {
	case "":
		return &WebSearch{toolSpec: webSearchSpec, networkTool: networkTool{guard: guard}, endpoint: defaultSearchEndpoint, provider: providerDuckDuckGo}
	case "off", "none", "disabled":
		return &WebSearch{toolSpec: webSearchSpec, networkTool: networkTool{guard: guard}, disabled: true}
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
	return &WebSearch{toolSpec: webSearchSpec, networkTool: networkTool{guard: guard}, endpoint: endpoint, provider: provider}
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
	if t.disabled {
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
	endpointHost := safeHost(t.endpoint)

	// The DuckDuckGo provider carries the query in a POST form body, so its reqURL is the
	// bare endpoint: DDG's HTML front-end answers a GET with its bot-challenge ("anomaly")
	// page — results come only over POST, the way its own search form submits. A custom
	// endpoint keeps the `q` GET-parameter contract.
	reqURL := t.endpoint
	if t.provider != providerDuckDuckGo {
		var err error
		reqURL, err = buildSearchURL(t.endpoint, args.Query)
		if err != nil {
			return errorResult(call.ID, "could not build search url for host "+endpointHost+": "+scrubURLError(err, t.endpoint)), nil
		}
	}

	method := http.MethodGet
	var reqBody io.Reader
	var header http.Header
	if t.provider == providerDuckDuckGo {
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
	text, hits := renderSearch(t.provider, resp, args.Query)
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
