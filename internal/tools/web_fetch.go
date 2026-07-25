package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/security"
)

var webFetchSpec = toolSpec{
	name:        "web_fetch",
	description: "Fetch the contents of an http(s) URL with a GET request and return the response status and body. Use this to read a web page or a raw file by URL. Blocked URLs (loopback, private, or metadata addresses, and disallowed hosts) are refused.",
	schema: json.RawMessage(`{
  "type": "object",
  "required": ["url"],
  "properties": {
    "url": {"type": "string", "description": "The absolute http(s) URL to fetch with a GET request."}
  }
}`),
}

type webFetchArgs struct {
	URL string `json:"url"`
}

// WebFetch performs a single GET against an http(s) URL and returns the response status and
// body (capped). It is an ExternalEffectTool of kind network: the disposition auto-runs it in
// Auto (url-filtered) and routes it through the injected ExternalEffects boundary for the
// bench. It reaches the network ONLY through the embedded network funnel, which is what
// applies url-safety and what carries the url-filter marker dispatch trusts (network.go).
// Stateless across Turns (ADR 0008).
type WebFetch struct {
	toolSpec
	networkTool
}

// NewWebFetch returns a web_fetch tool whose funnel filters every URL through guard (the
// host's url-safety policy plus the default-on SSRF floor).
func NewWebFetch(guard security.URLGuard) *WebFetch {
	return &WebFetch{toolSpec: webFetchSpec, networkTool: networkTool{guard: guard}}
}

// ExternalEffect reports that web_fetch reaches the network — the kind the disposition keys
// on to auto-run it (url-filtered) in Auto and route it through the ExternalEffects boundary.
func (t *WebFetch) ExternalEffect() domain.ExternalEffectKind { return domain.EffectNetwork }

// Execute fetches the URL. A blocked URL, an unreachable host, or a transport error are
// surfaced as results; only ctx cancellation is a Go error (ADR 0007).
func (t *WebFetch) Execute(ctx context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return domain.ToolResult{}, err
	}

	args, fail, ok := decodeToolArgs[webFetchArgs](call)
	if !ok {
		return fail, nil
	}
	if strings.TrimSpace(args.URL) == "" {
		return errorResult(call.ID, "url is required"), nil
	}

	// The funnel owns url-safety (pre-flight and at dial time), the client, the request and
	// the host-scoped failure message; web_fetch only says what to fetch and renders.
	resp, msg, err := t.do(ctx, netRequest{url: args.URL})
	if err != nil {
		return domain.ToolResult{}, err
	}
	if msg != "" {
		return errorResult(call.ID, msg), nil
	}
	return okResult(call.ID, renderFetchResult(resp)), nil
}

// renderFetchResult formats the GET response for the model: a status line, the resolved
// content type, and the (capped) body.
func renderFetchResult(resp netResponse) string {
	var b strings.Builder
	fmt.Fprintf(&b, "HTTP %s\n", resp.status)
	if ct := resp.header.Get("Content-Type"); ct != "" {
		fmt.Fprintf(&b, "Content-Type: %s\n", ct)
	}
	b.WriteString("\n")
	b.WriteString(resp.body)
	if resp.truncated {
		fmt.Fprintf(&b, "\n\n[response truncated at %d bytes]", maxNetworkResponseBytes)
	}
	return b.String()
}
