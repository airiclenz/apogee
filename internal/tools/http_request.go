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
// of kind network that reaches the network ONLY through the embedded network funnel, the same
// way web_fetch does — the funnel applies url-safety and carries the url-filter marker
// dispatch trusts (network.go). Stateless across Turns (ADR 0008).
type HTTPRequest struct {
	toolSpec
	networkTool
}

// NewHTTPRequest returns an http_request tool whose funnel filters every URL through guard.
func NewHTTPRequest(guard security.URLGuard) *HTTPRequest {
	return &HTTPRequest{toolSpec: httpRequestSpec, networkTool: networkTool{guard: guard}}
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

	// The header filter runs BEFORE the funnel: a call it rejects must reach no host.
	header, errMsg := applyRequestHeaders(args.Headers)
	if errMsg != "" {
		return errorResult(call.ID, errMsg), nil
	}

	var bodyReader io.Reader
	if args.Body != "" {
		bodyReader = strings.NewReader(args.Body)
	}

	// The funnel owns url-safety (pre-flight and at dial time), the client, the timeout
	// ceiling, the request and the host-scoped failure message; the tool only renders.
	resp, msg, err := t.do(ctx, netRequest{
		url:     args.URL,
		method:  method,
		body:    bodyReader,
		header:  header,
		timeout: clampTimeout(args.TimeoutSeconds),
	})
	if err != nil {
		return domain.ToolResult{}, err
	}
	if msg != "" {
		return errorResult(call.ID, msg), nil
	}
	return okResult(call.ID, renderRequestResult(resp)), nil
}

// applyRequestHeaders filters the caller-supplied headers and returns the header block the
// funnel sends: a header on the hop-by-hop / framing deny-list (incl. a forged Host) is
// rejected, the total count is capped at maxRequestHeaders, and each value is capped at
// maxRequestHeaderValueBytes. A rejected/over-limit header yields a non-empty error message
// (surfaced to the model as a result error, with no request made) and no header block. The
// filter is tighten-only — it only ever removes a model's reach.
func applyRequestHeaders(headers map[string]string) (header http.Header, errMsg string) {
	if len(headers) > maxRequestHeaders {
		return nil, fmt.Sprintf("too many request headers: %d (max %d)", len(headers), maxRequestHeaders)
	}
	header = make(http.Header, len(headers))
	for k, v := range headers {
		canonical := http.CanonicalHeaderKey(strings.TrimSpace(k))
		if canonical == "" {
			return nil, "empty header name is not allowed"
		}
		if deniedRequestHeaders[canonical] {
			return nil, fmt.Sprintf("header %q may not be set by http_request (it is transport-controlled or unsafe to override)", canonical)
		}
		if len(v) > maxRequestHeaderValueBytes {
			return nil, fmt.Sprintf("header %q value too large: %d bytes (max %d)", canonical, len(v), maxRequestHeaderValueBytes)
		}
		header.Set(canonical, v)
	}
	return header, ""
}

// maxResponseHeaderValueBytes caps a single rendered response header name or value. The response
// HEADER block is outside maxNetworkResponseBytes — the transport accepts a 10 MiB one by
// default, so a hostile server answering a one-byte body under a 9 MiB header block would
// otherwise hand the model what the body cap exists to refuse. The request side's mirror is
// maxRequestHeaderValueBytes (same value); web_fetch's single-header precedent is
// maxLocationBytes.
const maxResponseHeaderValueBytes = 4096

// maxResponseHeaderBlockBytes caps the rendered response header block as a whole, so many
// under-cap values cannot add up to the flood a single value may not be. A cut block keeps the
// lines already rendered and is MARKED — a truncated render must be visibly truncated, never a
// silent stub.
const maxResponseHeaderBlockBytes = 64 * 1024

// renderRequestResult formats the response for the model: status, a stable (sorted) header
// list, and the (capped) body. Every response header is SERVER-chosen text lifted out of the
// body and into a block the model reads as fact, so each rendered name and value goes through
// neuterInert (web_fetch.go) — the directive-inert shape redirectTarget applies to Location — and
// is capped per value and over the block as a whole: a bidi override, a zero-width rune or a
// CRLF/obs-fold-folded fake status line does not survive the render, no value opens a line of
// its own, and an oversized header block cannot route around the body cap.
func renderRequestResult(resp netResponse) string {
	var b strings.Builder
	fmt.Fprintf(&b, "HTTP %s\n", resp.status)

	keys := make([]string, 0, len(resp.header))
	for k := range resp.header {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var block strings.Builder
	for _, k := range keys {
		name := neuterInert(k, maxResponseHeaderValueBytes, "name")
		if name == "" {
			continue // a name that neuters away to nothing leaves no line to hang a value on
		}
		line := name + ": " + neuterInert(strings.Join(resp.header[k], ", "), maxResponseHeaderValueBytes, "value") + "\n"
		if block.Len()+len(line) > maxResponseHeaderBlockBytes {
			fmt.Fprintf(&block, "[header block truncated at %d bytes]\n", maxResponseHeaderBlockBytes)
			break
		}
		block.WriteString(line)
	}
	b.WriteString(block.String())

	b.WriteString("\n")
	b.WriteString(resp.body)
	if resp.truncated {
		fmt.Fprintf(&b, "\n\n[response truncated at %d bytes]", maxNetworkResponseBytes)
	}
	return b.String()
}
