package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// WebSearch runs a query against a CONFIGURED search endpoint and returns the raw results
// (capped). There is no hard-wired provider: the endpoint is injected from config and is
// DEFAULT-OFF — an empty endpoint makes the tool report "web search is not configured" (a
// graceful result, never a crash). It is an ExternalEffectTool of kind network, filtered by
// the same URLGuard + SSRF floor as the other network tools. Stateless across Turns.
//
// The request is a GET to endpoint with the query appended as the `q` parameter — the common
// shape for a search backend; a host whose endpoint differs can front it with a thin adapter.
type WebSearch struct {
	guard    security.URLGuard
	endpoint string
}

// NewWebSearch returns a web_search tool posting to endpoint (empty ⇒ unavailable, reported
// gracefully), filtering the endpoint URL through guard.
func NewWebSearch(guard security.URLGuard, endpoint string) *WebSearch {
	return &WebSearch{guard: guard, endpoint: strings.TrimSpace(endpoint)}
}

// Name returns the stable identifier the model calls.
func (t *WebSearch) Name() string { return "web_search" }

// Description returns the model-facing summary of the tool.
func (t *WebSearch) Description() string {
	return "Search the web for a query and return the results. Requires a configured search endpoint; when none is configured the tool reports that web search is unavailable (it does not fail the turn)."
}

// Schema returns the JSON schema of the tool's arguments.
func (t *WebSearch) Schema() json.RawMessage { return webSearchSchema }

// ExternalEffect reports that web_search reaches the network (kind network).
func (t *WebSearch) ExternalEffect() domain.ExternalEffectKind { return domain.EffectNetwork }

// Execute runs the search. A missing endpoint is a graceful "not configured" result; a
// blocked endpoint URL or a transport error are surfaced as results; only ctx cancellation
// is a Go error (ADR 0007).
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
	if t.endpoint == "" {
		// Default-off: no provider configured. Graceful, not an error (§3a — an optional,
		// detected enhancement, never a hard dependency).
		return okResult(call.ID, "web search is not configured (no search endpoint set); web_search is unavailable on this host."), nil
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
	return okResult(call.ID, renderSearchResult(resp, body, truncated)), nil
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

// renderSearchResult formats the search response for the model: a status line and the
// (capped) raw body (the backend's result document).
func renderSearchResult(resp *http.Response, body string, truncated bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "HTTP %s\n\n", resp.Status)
	b.WriteString(body)
	if truncated {
		fmt.Fprintf(&b, "\n\n[results truncated at %d bytes]", maxNetworkResponseBytes)
	}
	return b.String()
}
