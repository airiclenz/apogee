package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/security"
)

var httpRequestSpec = toolSpec{
	name:        "http_request",
	description: "Make an http(s) request with a chosen method, headers, and body, and return the response status, headers, and body. Use this for API calls (POST/PUT/etc.). Blocked URLs (loopback, private, or metadata addresses, and disallowed hosts) and unsupported methods are refused.",
	schema: json.RawMessage(`{
  "type": "object",
  "required": ["url"],
  "properties": {
    "url": {"type": "string", "description": "The absolute http(s) URL to request."},
    "method": {"type": "string", "description": "HTTP method (GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS). Default GET."},
    "headers": {"type": "object", "description": "Optional request headers as a string-to-string map.", "additionalProperties": {"type": "string"}},
    "body": {"type": "string", "description": "Optional request body (sent as-is for POST/PUT/PATCH)."},
    "timeout_seconds": {"type": "integer", "description": "Optional timeout in seconds (default 30, max 120)."}
  }
}`),
}

type httpRequestArgs struct {
	URL            string            `json:"url"`
	Method         string            `json:"method"`
	Headers        map[string]string `json:"headers"`
	Body           string            `json:"body"`
	TimeoutSeconds int               `json:"timeout_seconds"`
}

// allowedHTTPMethods is the set of methods http_request accepts — the arg-guard that rejects
// an unknown/dangerous method before reaching out (CONNECT/TRACE are not coding-agent idioms).
var allowedHTTPMethods = map[string]bool{
	http.MethodGet: true, http.MethodPost: true, http.MethodPut: true,
	http.MethodPatch: true, http.MethodDelete: true, http.MethodHead: true,
	http.MethodOptions: true,
}

// maxRequestHeaders caps how many caller-supplied headers http_request forwards, so a model
// cannot smuggle an unbounded header block into the request.
const maxRequestHeaders = 32

// maxRequestHeaderValueBytes caps a single forwarded header value's length, bounding the
// model-controlled input that reaches the wire.
const maxRequestHeaderValueBytes = 4096

// deniedRequestHeaders are headers a caller may NOT set on an http_request: the hop-by-hop /
// transfer-framing controls (which the transport owns and which can desync a proxy or smuggle a
// request — `Host`, `Content-Length`, `Transfer-Encoding`, `Connection`, the `Proxy-*` family)
// and a forged `Host` (the SSRF-floor host check is keyed off the URL host, so a `Host` override
// would route to a virtual-host-routed internal service the floor never saw). Keys are compared
// case-insensitively via http.CanonicalHeaderKey. This is a tighten-only filter — it removes a
// model's reach, never adds one (parity with the SSRF floor and the dangerous-rule semantics).
var deniedRequestHeaders = map[string]bool{
	"Host":                true,
	"Content-Length":      true,
	"Transfer-Encoding":   true,
	"Connection":          true,
	"Keep-Alive":          true,
	"Upgrade":             true,
	"Te":                  true, // TE (hop-by-hop)
	"Trailer":             true,
	"Proxy-Connection":    true,
	"Proxy-Authorization": true,
	"Proxy-Authenticate":  true,
}

// HTTPRequest performs a general http(s) request (method, headers, body) and returns the
// response status, a stable header subset, and the (capped) body. It is an ExternalEffectTool
// of kind network, filtered by the same URLGuard (scheme/host + SSRF floor, pre-flight and at
// dial time) as web_fetch. Stateless across Turns (ADR 0008).
type HTTPRequest struct {
	toolSpec
	guard security.URLGuard
}

// NewHTTPRequest returns an http_request tool that filters every URL through guard.
func NewHTTPRequest(guard security.URLGuard) *HTTPRequest {
	return &HTTPRequest{toolSpec: httpRequestSpec, guard: guard}
}

// ExternalEffect reports that http_request reaches the network (kind network).
func (t *HTTPRequest) ExternalEffect() domain.ExternalEffectKind { return domain.EffectNetwork }

// Execute performs the request. A blocked URL, an unsupported method, or a transport error
// are surfaced as results; only ctx cancellation is a Go error (ADR 0007).
func (t *HTTPRequest) Execute(ctx context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return domain.ToolResult{}, err
	}

	args, fail, ok := decodeToolArgs[httpRequestArgs](call)
	if !ok {
		return fail, nil
	}
	if strings.TrimSpace(args.URL) == "" {
		return errorResult(call.ID, "url is required"), nil
	}

	method := strings.ToUpper(strings.TrimSpace(args.Method))
	if method == "" {
		method = http.MethodGet
	}
	if !allowedHTTPMethods[method] {
		return errorResult(call.ID, fmt.Sprintf("unsupported HTTP method %q", method)), nil
	}

	if err := t.guard.CheckContext(ctx, args.URL); err != nil {
		return errorResult(call.ID, "url blocked by url-safety: "+err.Error()), nil
	}

	var bodyReader io.Reader
	if args.Body != "" {
		bodyReader = strings.NewReader(args.Body)
	}
	req, err := http.NewRequestWithContext(ctx, method, args.URL, bodyReader)
	if err != nil {
		return errorResult(call.ID, "could not build request: "+err.Error()), nil
	}
	if errMsg := applyRequestHeaders(req, args.Headers); errMsg != "" {
		return errorResult(call.ID, errMsg), nil
	}

	client := newHTTPClient(t.guard, clampTimeout(args.TimeoutSeconds))
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
	return okResult(call.ID, renderRequestResult(resp, body, truncated)), nil
}

// applyRequestHeaders sets the caller-supplied headers on req after filtering: a header on the
// hop-by-hop / framing deny-list (incl. a forged Host) is rejected, the total count is capped at
// maxRequestHeaders, and each value is capped at maxRequestHeaderValueBytes. It returns a
// non-empty error message for a rejected/over-limit header (surfaced to the model as a result
// error) and "" on success. The filter is tighten-only — it only ever removes a model's reach.
func applyRequestHeaders(req *http.Request, headers map[string]string) (errMsg string) {
	if len(headers) > maxRequestHeaders {
		return fmt.Sprintf("too many request headers: %d (max %d)", len(headers), maxRequestHeaders)
	}
	for k, v := range headers {
		canonical := http.CanonicalHeaderKey(strings.TrimSpace(k))
		if canonical == "" {
			return "empty header name is not allowed"
		}
		if deniedRequestHeaders[canonical] {
			return fmt.Sprintf("header %q may not be set by http_request (it is transport-controlled or unsafe to override)", canonical)
		}
		if len(v) > maxRequestHeaderValueBytes {
			return fmt.Sprintf("header %q value too large: %d bytes (max %d)", canonical, len(v), maxRequestHeaderValueBytes)
		}
		req.Header.Set(canonical, v)
	}
	return ""
}

// renderRequestResult formats the response for the model: status, a stable (sorted) header
// list, and the (capped) body.
func renderRequestResult(resp *http.Response, body string, truncated bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "HTTP %s\n", resp.Status)

	keys := make([]string, 0, len(resp.Header))
	for k := range resp.Header {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "%s: %s\n", k, strings.Join(resp.Header[k], ", "))
	}

	b.WriteString("\n")
	b.WriteString(body)
	if truncated {
		fmt.Fprintf(&b, "\n\n[response truncated at %d bytes]", maxNetworkResponseBytes)
	}
	return b.String()
}
