package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/security"
)

var webFetchSchema = json.RawMessage(`{
  "type": "object",
  "required": ["url"],
  "properties": {
    "url": {"type": "string", "description": "The absolute http(s) URL to fetch with a GET request."}
  }
}`)

type webFetchArgs struct {
	URL string `json:"url"`
}

// WebFetch performs a single GET against an http(s) URL and returns the response status and
// body (capped). It is an ExternalEffectTool of kind network: the disposition auto-runs it in
// Auto (url-filtered) and routes it through the injected ExternalEffects boundary for the
// bench. Every URL passes the URLGuard (scheme/host allow-deny + the resolved-IP SSRF floor)
// before the request and at dial time. Stateless across Turns (ADR 0008).
type WebFetch struct{ guard security.URLGuard }

// NewWebFetch returns a web_fetch tool that filters every URL through guard (the host's
// url-safety policy plus the default-on SSRF floor).
func NewWebFetch(guard security.URLGuard) *WebFetch { return &WebFetch{guard: guard} }

// Name returns the stable identifier the model calls.
func (t *WebFetch) Name() string { return "web_fetch" }

// Description returns the model-facing summary of the tool.
func (t *WebFetch) Description() string {
	return "Fetch the contents of an http(s) URL with a GET request and return the response status and body. Use this to read a web page or a raw file by URL. Blocked URLs (loopback, private, or metadata addresses, and disallowed hosts) are refused."
}

// Schema returns the JSON schema of the tool's arguments.
func (t *WebFetch) Schema() json.RawMessage { return webFetchSchema }

// ExternalEffect reports that web_fetch reaches the network — the kind the disposition keys
// on to auto-run it (url-filtered) in Auto and route it through the ExternalEffects boundary.
func (t *WebFetch) ExternalEffect() domain.ExternalEffectKind { return domain.EffectNetwork }

// Execute fetches the URL. A blocked URL, an unreachable host, or a transport error are
// surfaced as results; only ctx cancellation is a Go error (ADR 0007).
func (t *WebFetch) Execute(ctx context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return domain.ToolResult{}, err
	}

	var args webFetchArgs
	if err := decodeArgs(call.Arguments, &args); err != nil {
		return errorResult(call.ID, "invalid arguments: "+err.Error()), nil
	}
	if strings.TrimSpace(args.URL) == "" {
		return errorResult(call.ID, "url is required"), nil
	}

	// Pre-flight url-safety (scheme/host + the resolved-IP SSRF floor). The dial-time floor
	// (SafeDialControl, inside the client) is the rebinding backstop.
	if err := t.guard.CheckContext(ctx, args.URL); err != nil {
		return errorResult(call.ID, "url blocked by url-safety: "+err.Error()), nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, args.URL, nil)
	if err != nil {
		return errorResult(call.ID, "could not build request: "+err.Error()), nil
	}

	client := newHTTPClient(t.guard, defaultNetworkTimeout)
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return domain.ToolResult{}, ctx.Err()
		}
		if networkURLError(err) {
			return errorResult(call.ID, "url blocked by url-safety: "+err.Error()), nil
		}
		return errorResult(call.ID, "request failed: "+err.Error()), nil
	}
	defer resp.Body.Close()

	body, truncated := readCappedBody(resp.Body)
	return okResult(call.ID, renderFetchResult(resp, body, truncated)), nil
}

// renderFetchResult formats the GET response for the model: a status line, the resolved
// content type, and the (capped) body.
func renderFetchResult(resp *http.Response, body string, truncated bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "HTTP %s\n", resp.Status)
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		fmt.Fprintf(&b, "Content-Type: %s\n", ct)
	}
	b.WriteString("\n")
	b.WriteString(body)
	if truncated {
		fmt.Fprintf(&b, "\n\n[response truncated at %d bytes]", maxNetworkResponseBytes)
	}
	return b.String()
}
