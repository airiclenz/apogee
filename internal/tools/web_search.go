package tools

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/security"
)

var webSearchSchema = json.RawMessage(`{
  "type": "object",
  "required": ["query"],
  "properties": {
    "query": {"type": "string", "description": "The search query."}
  }
}`)

type webSearchArgs struct {
	Query string `json:"query"`
}

// defaultSearchEndpoint is the built-in provider used when no endpoint is configured:
// DuckDuckGo's HTML front-end needs no API key, so web search works out of the box.
const defaultSearchEndpoint = "https://html.duckduckgo.com/html/"

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
// is disabled" result, never a crash. A custom endpoint receives the query as the `q` GET
// parameter (the common shape for a search backend; a host whose endpoint differs can front
// it with a thin adapter): an HTML response is cleaned into title/url/snippet results, a
// JSON/text response passes through verbatim. It is an ExternalEffectTool of kind network,
// filtered by the same URLGuard + SSRF floor as the other network tools. Stateless across
// Turns.
type WebSearch struct {
	guard    security.URLGuard
	endpoint string
	provider searchProvider
	disabled bool // the endpoint was the off sentinel — Execute reports gracefully, no request
}

// NewWebSearch returns the web_search tool. An empty endpoint selects the built-in
// DuckDuckGo default; the sentinels "off"/"none"/"disabled" (case-insensitive) disable the
// tool; anything else is a custom endpoint, filtered through guard. A scheme-less custom
// endpoint (e.g. "search.example.com/s") self-heals to https:// — url.Parse reads it as a
// bare path (Host == ""), and url-safety would otherwise reject every request.
func NewWebSearch(guard security.URLGuard, endpoint string) *WebSearch {
	endpoint = strings.TrimSpace(endpoint)
	switch strings.ToLower(endpoint) {
	case "":
		return &WebSearch{guard: guard, endpoint: defaultSearchEndpoint, provider: providerDuckDuckGo}
	case "off", "none", "disabled":
		return &WebSearch{guard: guard, disabled: true}
	}
	if u, err := url.Parse(endpoint); err != nil || u.Host == "" {
		if healed, herr := url.Parse("https://" + endpoint); herr == nil && healed.Host != "" {
			endpoint = "https://" + endpoint
		}
	}
	return &WebSearch{guard: guard, endpoint: endpoint, provider: providerCustom}
}

// Name returns the stable identifier the model calls.
func (t *WebSearch) Name() string { return "web_search" }

// Description returns the model-facing summary of the tool.
func (t *WebSearch) Description() string {
	return "Search the web for a query and return the top results (title, url, snippet). Works with no configuration (DuckDuckGo by default); a host may point it at a custom search backend or disable it, in which case the tool says so instead of failing the turn."
}

// Schema returns the JSON schema of the tool's arguments.
func (t *WebSearch) Schema() json.RawMessage { return webSearchSchema }

// ExternalEffect reports that web_search reaches the network (kind network).
func (t *WebSearch) ExternalEffect() domain.ExternalEffectKind { return domain.EffectNetwork }

// Execute runs the search. A disabled tool (off sentinel) is a graceful "disabled" result;
// a blocked endpoint URL, a transport error, or a non-2xx status are surfaced as results;
// only ctx cancellation is a Go error (ADR 0007).
func (t *WebSearch) Execute(ctx context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return domain.ToolResult{}, err
	}

	var args webSearchArgs
	if err := decodeArgs(call.Arguments, &args); err != nil {
		return errorResult(call.ID, "invalid arguments: "+err.Error()), nil
	}
	if strings.TrimSpace(args.Query) == "" {
		return errorResult(call.ID, "query is required"), nil
	}
	if t.disabled {
		// The host set the off sentinel. Graceful, not an error (§3a) — the model learns
		// web search is unavailable and the turn continues. No request is made.
		return okResult(call.ID, "web search is disabled on this host (web-search-endpoint: off); web_search is unavailable."), nil
	}

	reqURL, err := buildSearchURL(t.endpoint, args.Query)
	if err != nil {
		return errorResult(call.ID, "could not build search url: "+err.Error()), nil
	}
	// endpointHost is the bare host of the configured endpoint — the ONLY part of the
	// endpoint safe to surface to the model. The constructed reqURL carries the query and
	// may carry a config'd API key in its parameters (the endpoint "preserves any
	// parameters it already carries"); it must never reach a model-facing or logged string
	// (security-review M2). Every error below renders endpointHost or a URL-scrubbed error,
	// never reqURL.
	endpointHost := endpointHost(t.endpoint)

	if err := t.guard.CheckContext(ctx, reqURL); err != nil {
		return errorResult(call.ID, "search endpoint blocked by url-safety (host "+endpointHost+")"), nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return errorResult(call.ID, "could not build search request for host "+endpointHost), nil
	}
	if t.provider == providerDuckDuckGo {
		// Browser-like headers: DuckDuckGo's HTML front-end serves challenge pages to bare
		// clients far more often. Scoped to the built-in provider so a custom backend sees
		// the same request it always did (a content-negotiating backend must keep returning
		// its clean JSON/text). No Accept-Encoding — Go's transport only transparently
		// un-gzips when it set that header itself.
		req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
		req.Header.Set("Accept", "text/html")
		req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	}

	client := newHTTPClient(t.guard, defaultNetworkTimeout)
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return domain.ToolResult{}, ctx.Err()
		}
		if networkURLError(err) {
			return errorResult(call.ID, "search endpoint blocked by url-safety (host "+endpointHost+")"), nil
		}
		// A transport error's text (*url.Error) embeds the FULL request URL — which here
		// carries the query and any API key — so scrub the URL out before surfacing it.
		return errorResult(call.ID, "search request to host "+endpointHost+" failed: "+scrubURLError(err, reqURL)), nil
	}
	defer resp.Body.Close()

	body, truncated := readCappedBody(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Non-2xx is a failed search, surfaced with only status + host: the body of a
		// rate-limit or challenge page is noise, and the URL must stay scrubbed (M2).
		return errorResult(call.ID, "search endpoint returned HTTP "+resp.Status+" (host "+endpointHost+")"), nil
	}
	return okResult(call.ID, renderSearch(t.provider, resp, body, args.Query, truncated)), nil
}

// endpointHost returns the bare host (no scheme, no path, no query) of the configured
// search endpoint — the only part safe to surface to the model, since the endpoint's
// query may carry an API key (security-review M2). An unparseable endpoint yields a
// neutral placeholder rather than echoing the raw (possibly key-bearing) string.
func endpointHost(endpoint string) string {
	u, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || u.Host == "" {
		return "the configured search endpoint"
	}
	return u.Hostname()
}

// scrubURLError renders a transport error WITHOUT the request URL it embeds. Go's
// *url.Error stringifies as `<op> "<url>": <cause>`, and that url here carries the query
// and any API-key parameter; scrubURLError strips the URL substring so only the operation
// and the underlying cause survive (security-review M2). reqURL is the exact string to
// remove. A non-url.Error is returned unchanged (it carries no URL).
func scrubURLError(err error, reqURL string) string {
	var ue *url.Error
	if errors.As(err, &ue) {
		// Reconstruct from the parts that do NOT include the URL: the op and the cause.
		cause := "request failed"
		if ue.Err != nil {
			cause = ue.Err.Error()
		}
		return strings.TrimSpace(ue.Op) + ": " + redactSubstring(cause, reqURL)
	}
	return redactSubstring(err.Error(), reqURL)
}

// redactSubstring removes any occurrence of secret from s (defence-in-depth in case the
// URL leaks into a nested error's text), returning the cleaned string.
func redactSubstring(s, secret string) string {
	if secret == "" {
		return s
	}
	return strings.ReplaceAll(s, secret, "[redacted-url]")
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
