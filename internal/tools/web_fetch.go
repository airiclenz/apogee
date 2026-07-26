package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

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

// maxLocationBytes caps the redirect target rendered to the model. The response HEADER block is
// outside maxNetworkResponseBytes (net/http accepts a 10 MiB header block by default) and Location
// is the one header web_fetch surfaces, so an unbounded value would be a way around the body cap.
// No followable URL comes near this; a longer one is cut and MARKED rather than dropped, so the
// model is told the target was too long to be trusted whole instead of being handed a silent stub.
const maxLocationBytes = 2048

// renderFetchResult formats the GET response for the model: a status line, the resolved
// content type, the redirect target on a 3xx, and the (capped) body.
func renderFetchResult(resp netResponse) string {
	var b strings.Builder
	fmt.Fprintf(&b, "HTTP %s\n", resp.status)
	if ct := resp.header.Get("Content-Type"); ct != "" {
		fmt.Fprintf(&b, "Content-Type: %s\n", ct)
	}
	// The funnel deliberately does not follow redirects — one could carry a vetted request to an
	// unvetted host (network.go's CheckRedirect) — and the whole justification for refusing is that
	// the model can follow it ITSELF through a fresh, re-checked call. That is only true if the
	// model is told where the redirect points, and a 3xx body is usually empty: without this line an
	// http→https or trailing-slash canonicalisation leaves a small model holding `HTTP 302 Found`
	// and no way forward (M-6). Rendering the target follows nothing — the next call is a whole new
	// one, pre-flight-checked and dial-checked like any other. http_request never had the gap: its
	// sorted header block already carries Location.
	if loc := redirectTarget(resp); loc != "" {
		fmt.Fprintf(&b, "Location: %s\n", loc)
	}
	b.WriteString("\n")
	b.WriteString(resp.body)
	if resp.truncated {
		fmt.Fprintf(&b, "\n\n[response truncated at %d bytes]", maxNetworkResponseBytes)
	}
	return b.String()
}

// redirectTarget returns the model-facing form of a 3xx response's Location header, or "" when
// there is nothing to render: a non-3xx status (a Location beside a 200 is not a redirect and is
// not shown), a missing header, or a value that neuters away to nothing.
//
// The value is SERVER-chosen — attacker-influenced text this render lifts out of the body and
// into the header block the model reads as fact — so it goes through neuterInert first. net/http
// already refuses a C0 byte inside a response header value, and Go's client refuses a Location it
// cannot parse before CheckRedirect is ever consulted — but an obs-fold continuation line and a
// bidi override survive both, and a right-to-left override earns its keep precisely on a URL the
// model is invited to FOLLOW.
//
// Nothing is redacted out of it. The Location is response content, like the body beside it, and the
// funnel's M2 rule is about failure messages naming the REQUEST url; a canonicalising redirect
// legitimately echoes that url, and blanking it would hand back a target that cannot be followed.
// The rendering stays web_fetch's rather than the funnel's for a related reason: web_search's
// endpoint may carry a config'd API key, and a search endpoint that redirects must not get to hand
// that key back (TestWebSearch_RedirectDoesNotRenderTheLocation).
func redirectTarget(resp netResponse) string {
	if resp.statusCode < 300 || resp.statusCode > 399 {
		return ""
	}
	return neuterInert(resp.header.Get("Location"), maxLocationBytes, "location")
}

// neuterInert returns raw response-header text in the directive-inert shape
// library.SanitizeContent applies to untrusted stored text: control (Cc), format (Cf),
// private-use (Co) and surrogate (Cs) runes are dropped, and whitespace runs are folded to a
// single space. Folding keeps the whole value rather than cutting at the first space: a URI
// carries no raw space, but the servers that emit an unencoded one would then be answered with a
// WRONG target, and folded text can only ever sit inside its one rendered line — it can open
// neither a header line nor a body of its own. The result is capped at capBytes because the
// response HEADER block is outside maxNetworkResponseBytes (net/http accepts a 10 MiB one by
// default), so an unbounded value would be a way around the body cap; a longer value is cut and
// MARKED "[<label> truncated at <capBytes> bytes]" rather than dropped, so the model is told the
// text was too long to be trusted whole instead of being handed a silent stub. Shared by every
// render that lifts server-chosen header text into the block the model reads as fact:
// redirectTarget here and http_request's whole header block (renderRequestResult).
func neuterInert(raw string, capBytes int, label string) string {
	var b strings.Builder
	b.Grow(min(len(raw), capBytes))
	cut, prevSpace := false, false
	for _, r := range raw {
		switch {
		case unicode.IsSpace(r):
			if prevSpace {
				continue
			}
			r, prevSpace = ' ', true
		case unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Co, unicode.Cs):
			continue
		default:
			prevSpace = false
		}
		if b.Len()+utf8.RuneLen(r) > capBytes {
			cut = true
			break
		}
		b.WriteRune(r)
	}

	s := strings.TrimSpace(b.String())
	if s == "" || !cut {
		return s
	}
	return fmt.Sprintf("%s [%s truncated at %d bytes]", s, label, capBytes)
}
