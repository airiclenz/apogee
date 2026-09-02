package tools

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/security"
)

// ----------------------------------------------------------------------------
// Network tools (P3.11) — web_fetch / http_request / web_search
// ----------------------------------------------------------------------------
//
// The network tools are in-process net/http clients (NOT SubprocessTools — they spawn
// nothing, so they carry no Confiner/Setpgid lifecycle). Each is an ExternalEffectTool
// of kind `network`: the dispatch disposition auto-runs them in Auto (url-filtered — the
// network is open per ADR 0012), routes them through the injected ExternalEffects boundary
// for the bench's deterministic stub (ADR 0008), and gates them in Ask-Before. They are
// stateless across Turns (a fresh request per call; ADR 0008).
//
// url-safety is applied BY THE FUNNEL, not by each tool's own discipline: networkTool.do is
// the single path from a tool to the network, and it runs the host's URLGuard (scheme/host
// allow-deny + the default-on, resolved-IP SSRF floor) BEFORE the request and, via the
// guard's SafeDialControl, at DIAL time too — so a DNS-rebinding name that passes the
// pre-flight check still cannot connect to a private IP (security/ssrf.go). With the
// operator's HTTP(S)_PROXY in force the dial-time half becomes the proxy's own pin instead,
// because the proxy is then what the transport connects to; the pre-flight over the
// destination is unchanged, so a proxy cannot launder a private target (newHTTPClient).
// Embedding the funnel is the only way to obtain the urlFilteredNetworker marker, and that
// marker — not a declared effect kind — is what dispatch trusts when it auto-runs a network
// tool unattended in Auto. A tool that reaches the network some other way therefore carries no marker and
// gates in Auto instead of running unsupervised.

// maxNetworkResponseBytes caps the body a network tool reads into a result so a huge
// download cannot exhaust memory or flood the model's context. It mirrors the file tools'
// read ceiling in spirit (a single call's blast radius is bounded).
const maxNetworkResponseBytes = 2 * 1024 * 1024

// defaultNetworkTimeout bounds a single network call so a slow/hung endpoint never wedges a
// Turn. It is ONE budget over the whole call — the pre-flight's DNS resolution, the dial and
// the body read share a single deadline, they do not each get a copy — so a call can never
// cost more than the resolved timeout and maxNetworkTimeout is a real ceiling on it. The
// lookup is inside the budget because it is otherwise bounded only by the SYSTEM resolver
// configuration: a host delegated to a black-holing nameserver blocks for
// timeout × attempts × nservers (minutes, with `options timeout:30 attempts:5`) that no
// request timeout would bound (M-5). http_request may lower the budget via its
// timeout_seconds argument; it never raises it past the ceiling.
const (
	defaultNetworkTimeout = 30 * time.Second
	maxNetworkTimeout     = 120 * time.Second
)

// networkTool is the funnel Apogee's own network tools EMBED. It owns the host's URLGuard
// and its do method is the single path from a tool to the network, so url-safety is a
// property of the code path rather than of each tool author's discipline. Embedding it is
// also the only way to obtain the urlFilteredNetworker marker — the marker therefore cannot
// exist without the guard.
//
// proxy is the operator's egress proxy resolver. A nil proxy means http.ProxyFromEnvironment
// — the process's HTTP_PROXY / HTTPS_PROXY / NO_PROXY, the same environment the LLM client
// already honours through Go's default transport — read when a call is made rather than when
// the tool is built, so the constructors take no proxy argument. It is a field only so a test
// can supply its own forward proxy; there is no per-server proxy config key.
type networkTool struct {
	guard security.URLGuard
	proxy func(*http.Request) (*url.URL, error)
}

// urlFilteredNetworker is the unexported marker carried ONLY by a tool that reaches the
// network through the funnel (embedding networkTool is the only way to satisfy it: the
// method is unexported, so no type outside this package — and no third-party tool in
// another module, Go's internal/ rule — can implement it).
//
// Carrying the marker ASSERTS that every outbound URL of the tool passed the host's
// URLGuard, pre-flight and again at dial time. Dispatch trusts that assertion to auto-run
// the tool unattended in Auto (ADR 0012 scopes the auto-run to Apogee's own url-filtered
// network tools). A network tool that hand-rolls its own http.Client does not carry the
// marker and gates instead — the network analogue of workspaceScopedWriter on the write axis.
//
// It rides the tool VALUE (a method set), so it survives registry.Subset for free — a
// sub-agent one level down inherits it with no threading.
type urlFilteredNetworker interface {
	domain.Tool

	// urlFiltered is the marker method. It has no behaviour: being unfakeable outside this
	// package IS its content.
	urlFiltered()
}

// urlFiltered marks every embedder of the funnel as url-filtered. It is deliberately
// method-on-networkTool rather than a field or a declaration: a tool cannot claim the
// marker without also taking the guard and the do path that applies it.
func (networkTool) urlFiltered() {}

// IsURLFilteredNetworker reports whether t is one of Apogee's own network tools that reach
// the network only through the funnel — the signal dispatch keys on to auto-run a network
// tool in Auto with no Approval. Mirrors IsWorkspaceScopedWriter on the write axis.
func IsURLFilteredNetworker(t domain.Tool) bool {
	_, ok := t.(urlFilteredNetworker)
	return ok
}

// netRequest is one outbound call as a tool describes it; the funnel — never the tool —
// turns it into an http.Request, so neither the guard nor the timeout ceiling can be
// skipped. url is the URL as the tool spells it — the funnel normalises it once and both
// checks and requests that one form — and it may carry a query and a config'd API key, so it
// is never surfaced: safeLabel is the host-only string every failure message names.
type netRequest struct {
	url       string        // the URL the tool asks for (normalised, then guard-checked; never surfaced)
	method    string        // empty ⇒ GET
	body      io.Reader     // nil ⇒ no request body
	header    http.Header   // nil ⇒ no caller-supplied headers
	timeout   time.Duration // ≤ 0 ⇒ defaultNetworkTimeout; clamped to maxNetworkTimeout
	safeLabel string        // host-only label for failure messages; empty ⇒ safeHost(url)
}

// netResponse is what the funnel brings back: the wire facts, with the body already read
// under the response cap. Each tool renders these its own way (body + content-type; a
// sorted header list; parsed search hits) and decides its own non-2xx policy — the funnel
// deliberately does not.
type netResponse struct {
	status     string
	statusCode int
	header     http.Header
	body       string
	truncated  bool
}

// do is the single path from a tool to the network: it normalises the URL once, applies
// url-safety to that one form (pre-flight and, through the client, at dial time), builds and
// sends the request from the same form, and reads the capped body. The resolved timeout is
// ONE deadline shared by every phase — the pre-flight's DNS lookup, the dial and the body
// read — so no phase is unbounded and no phase gets a budget of its own. It returns exactly
// one of three shapes:
//
//   - (resp, "", nil) — the request completed with ANY status; a non-2xx is the tool's own
//     policy to decide, not the funnel's;
//   - (netResponse{}, msg, nil) — a blocked URL, a request-build failure, a transport failure,
//     or a body cut short mid-read, where msg is a ready-to-surface message the tool hands to
//     errorResult verbatim;
//   - (netResponse{}, "", err) — ctx cancellation ONLY (ADR 0007: a tool returns a Go error
//     for nothing else).
//
// Every message names only the bare host (req.safeLabel) plus a URL-scrubbed cause, so a
// key-bearing request URL can never ride out to the model (security-review M2) — protection
// that used to be web_search's private discipline and is now every network tool's.
func (n networkTool) do(ctx context.Context, req netRequest) (netResponse, string, error) {
	if err := ctx.Err(); err != nil {
		return netResponse{}, "", err
	}

	label := req.safeLabel
	if label == "" {
		label = safeHost(req.url)
	}

	// Normalise ONCE, here, and use the result for the guard, the request AND every failure
	// message's URL scrub: the guard must judge the string the transport actually dials, or
	// its verdict is about a different name than the one reached (M-1), and the scrub must
	// strip the spelling an error can actually embed — below this line every error arises
	// from target, never from the raw req.url, so scrubbing the raw form would search for a
	// string the message need not contain (item 18 of the 2026-07-26 plan). NormalizeURL is
	// the guard's own normal form, so an unparseable URL needs no second wording here — it
	// is passed through untouched (target stays req.url, which is then also the string the
	// request would carry) and the CheckContext below refuses it with the guard's message.
	target := req.url
	// targetURL is the same form as a *url.URL: the client builder needs it to ask the proxy
	// resolver whether a proxy applies to this destination. It stays nil only for a URL that
	// does not parse, which the CheckContext below refuses before any client is built.
	var targetURL *url.URL
	if normalized, err := security.NormalizeURL(req.url); err == nil {
		target = normalized.String()
		targetURL = normalized
	}

	// ONE deadline for the whole call. rctx is started here, before the pre-flight, and carries
	// the request too, so resolve + dial + body cost one budget BETWEEN them rather than one
	// each. The check ran on the RAW caller ctx before, so the floor's LookupIPAddr was bounded
	// only by the system resolver's own retry schedule — on top of the HTTP timeout, and
	// unbounded by http_request's timeout_seconds (M-5). Dispatch adds no per-tool deadline, so
	// this is the only place the bound can be applied.
	budget := clampDuration(req.timeout)
	rctx, cancelBudget := context.WithTimeout(ctx, budget)
	defer cancelBudget()

	// Pre-flight url-safety (scheme/host + the resolved-IP SSRF floor). The dial-time floor
	// (SafeDialControl, inside the client) is the rebinding backstop below.
	if err := n.guard.CheckContext(rctx, target); err != nil {
		// A budget that ran out inside the pre-flight is the FUNNEL's own bound, so it stays the
		// model-facing message shape — the caller's ctx is alive and the Turn continues. Only the
		// CALLER's cancellation is ADR 0007's Go-error shape, and rctx carries that too, so the
		// two causes are told apart here rather than both reading as a blocked URL.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return netResponse{}, "", ctxErr
		}
		return netResponse{}, blockedMessage(label, err, target), nil
	}

	// The client's Timeout is the same budget restated. Its clock starts at client.Do, so it is
	// always the LOOSER of the two bounds and rctx — running since before the pre-flight — is
	// what actually ends a call; it stays as the backstop for a transport that somehow outlives
	// its request context.
	client, err := newHTTPClient(rctx, n.guard, targetURL, n.proxy, budget)
	if err != nil {
		// An unusable egress proxy, or one whose addresses cannot be pinned, refuses the call
		// with the pre-flight's own wording: nothing left the process, and the cause is the
		// same policy layer.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return netResponse{}, "", ctxErr
		}
		return netResponse{}, blockedMessage(label, err, target), nil
	}
	// The client is built per call, so its pooled connection would OUTLIVE it: net/http keeps
	// an idle connection — and the readLoop/writeLoop goroutines that pin the transport with
	// it — alive for IdleConnTimeout after do returns. Network tools auto-run unattended in
	// Auto, so dozens of calls in a Turn is the normal case, and the process would accumulate
	// a socket plus two goroutines per call. Draining the pool bounds the funnel's footprint
	// to the call itself. Deferred BEFORE the response body's Close so it runs AFTER it
	// (LIFO), and a connection handed back to the pool a moment later still closes, because
	// CloseIdleConnections also latches the transport into closing newly idle connections.
	defer client.CloseIdleConnections()

	method := req.method
	if method == "" {
		method = http.MethodGet
	}
	// rctx, not ctx: the request runs under the deadline the pre-flight has already spent part
	// of, which is what makes the budget shared rather than per-phase (M-5). It derives from the
	// caller's ctx, so a caller cancellation still reaches the in-flight request.
	httpReq, err := http.NewRequestWithContext(rctx, method, target, req.body)
	if err != nil {
		return netResponse{}, "could not build request for host " + label + ": " + scrubURLError(err, target), nil
	}
	if len(req.header) > 0 {
		httpReq.Header = req.header.Clone()
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return netResponse{}, "", ctxErr
		}
		if networkURLError(err) {
			// The dial-time floor refused the CONNECTED IP (the rebinding backstop); it
			// surfaces wrapped inside the transport error chain.
			return netResponse{}, blockedMessage(label, err, target), nil
		}
		// A transport error's text (*url.Error) embeds the FULL request URL — scrub it.
		return netResponse{}, "request to host " + label + " failed: " + scrubURLError(err, target), nil
	}
	defer func() { _ = resp.Body.Close() }()

	body, truncated, err := readCappedBody(resp.Body)
	if err != nil {
		// A body cut short mid-read carries no marker of its own — truncated is set by the
		// cap alone — so the wire facts would read to the model as a COMPLETE response.
		// Caller cancellation comes first and keeps ADR 0007's Go-error shape, so the Turn
		// rolls back; every other cause (the client timeout, which covers the body read, or a
		// mid-body reset) is the model-facing message shape.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return netResponse{}, "", ctxErr
		}
		return netResponse{}, "response from host " + label + " was cut short: " + scrubURLError(err, target), nil
	}
	return netResponse{
		status:     resp.Status,
		statusCode: resp.StatusCode,
		header:     resp.Header,
		body:       body,
		truncated:  truncated,
	}, "", nil
}

// blockedMessage renders a url-safety rejection for the model: the bare host plus the
// guard's reason, with the (possibly key-bearing) request URL scrubbed out of that reason
// (M2). It serves both the pre-flight Check and the dial-time floor.
func blockedMessage(label string, err error, rawURL string) string {
	msg := "url blocked by url-safety (host " + label + ")"
	if reason := blockedReason(err, rawURL); reason != "" {
		return msg + ": " + reason
	}
	return msg
}

// blockedReason is the guard's cause alone: URL-scrubbed (M2) and stripped of the
// security.ErrURLBlocked sentinel text ("security: url blocked by url-safety") that every
// url-safety error carries in its own string. Without the strip the message repeated itself —
// "url blocked by url-safety (host 127.0.0.1): security: url blocked by url-safety: …" —
// because blockedMessage's own prefix already states the block. The sentinel is removed
// wherever it appears, not only at the front: a dial-time floor block arrives wrapped inside
// the transport error's text, so the sentinel sits mid-string there.
func blockedReason(err error, rawURL string) string {
	sentinel := security.ErrURLBlocked.Error()
	reason := scrubURLError(err, rawURL)
	reason = strings.ReplaceAll(reason, sentinel+": ", "")
	reason = strings.ReplaceAll(reason, sentinel, "")
	// A sentinel-only cause leaves a dangling separator; trim it rather than surface ": ".
	return strings.Trim(reason, ": ")
}

// newHTTPClient builds an http.Client for ONE outbound call: it resolves the operator's
// egress proxy for target, sets the dial-time control the resulting connection needs, and
// applies the given overall timeout. It is the single place the network tools obtain a
// client so neither the proxy nor the dial-time floor is ever accidentally skipped.
//
// The order of judgement is the point. The PRE-FLIGHT (do, above) judges the DESTINATION —
// the scheme/host allow-deny lists and the resolved-IP SSRF floor — and it runs whether or
// not a proxy applies, so a private destination is refused before anything leaves the
// process and a proxy can never launder one. The dial-time control judges what is actually
// DIALLED: with a proxy in force the transport connects to the PROXY rather than to the
// destination, so the control pins the proxy's own resolved addresses (a proxy on loopback
// or the LAN is the normal case, and the blanket floor would refuse it); with no proxy —
// NO_PROXY, or none configured — it stays the blanket SafeDialControl, the DNS-rebinding
// backstop over the destination itself. Redirects are still never followed either way.
//
// A nil proxy means http.ProxyFromEnvironment: the process's HTTP_PROXY / HTTPS_PROXY /
// NO_PROXY, which is what the LLM client already honours through Go's default transport.
// An unusable proxy value, or a proxy whose addresses cannot be resolved, fails the call
// closed rather than dialling around it.
//
// The timeout is a backstop, not the operative bound: do runs the request under a context whose
// deadline is the same budget started BEFORE the pre-flight lookup, so that deadline always
// falls first (M-5).
//
// The client is per CALL — the timeout is the caller's — which is why do drains its connection
// pool on the way out; nothing here may outlive the request. (internal/mcp builds the same
// shape but keeps it long-lived, which is right for a session-long server connection and wrong
// for a one-shot tool call.)
func newHTTPClient(
	ctx context.Context,
	guard security.URLGuard,
	target *url.URL,
	proxy func(*http.Request) (*url.URL, error),
	timeout time.Duration,
) (*http.Client, error) {
	if proxy == nil {
		proxy = http.ProxyFromEnvironment
	}
	control := guard.SafeDialControl() // re-check the connected IP — closes DNS-rebinding TOCTOU
	if target != nil {
		proxyURL, err := proxy(&http.Request{URL: target})
		if err != nil {
			// The proxy value is deliberately NOT interpolated: the resolver quotes it back and
			// a proxy URL may carry credentials (the reasoning mcp's checkEndpoint rests on).
			return nil, fmt.Errorf("%w: the configured egress proxy is not a usable URL", security.ErrURLBlocked)
		}
		if proxyURL != nil {
			control, err = guard.PinnedDialControl(ctx, proxyURL.Hostname())
			if err != nil {
				return nil, fmt.Errorf("egress proxy %s could not be pinned: %w", proxyURL.Hostname(), err)
			}
		}
	}
	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
		Control: control,
	}
	transport := &http.Transport{
		Proxy:                 proxy,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          10,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
		// Do not follow redirects automatically: a redirect could send a vetted request to
		// an unvetted (private) host, sidestepping the pre-flight Check. The model sees the
		// redirect Location and can choose to follow it through a fresh, re-checked call.
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}, nil
}

// readCappedBody reads at most maxNetworkResponseBytes from r. It reports truncated when the
// CAP ended the read — a deliberately bounded but otherwise clean result the renderers mark —
// and returns a non-nil error when the read FAILED before the cap. The error is the caller's
// to surface rather than this function's to swallow: a body cut short by the client timeout, a
// mid-body reset or a cancelled ctx sets no truncation flag, so a discarded error is exactly
// how a partial response comes to read as a complete one.
func readCappedBody(r io.Reader) (body string, truncated bool, err error) {
	limited := io.LimitReader(r, maxNetworkResponseBytes+1)
	data, err := io.ReadAll(limited)
	if len(data) > maxNetworkResponseBytes {
		// The cap holds a full result either way, so a reader that failed on the very byte
		// that reached the cap changes nothing the model can see.
		return string(data[:maxNetworkResponseBytes]), true, nil
	}
	return string(data), false, err
}

// networkURLError reports whether err is a url-safety rejection (the pre-flight Check or the
// dial-time SSRF floor), so a tool renders a uniform "blocked" result rather than leaking a
// raw transport error. A dial-time floor block surfaces wrapped inside the http error chain.
func networkURLError(err error) bool {
	return errors.Is(err, security.ErrURLBlocked)
}

// clampTimeout resolves a caller-supplied timeout in seconds against the default/ceiling: 0
// (unset) ⇒ the default; anything over the ceiling is clamped down (never raised past it).
func clampTimeout(seconds int) time.Duration {
	return clampDuration(time.Duration(seconds) * time.Second)
}

// clampDuration is the same resolution for an already-typed Duration — the form the funnel
// applies to netRequest.timeout, so the ceiling lives in one place on both paths.
func clampDuration(d time.Duration) time.Duration {
	if d <= 0 {
		return defaultNetworkTimeout
	}
	if d > maxNetworkTimeout {
		return maxNetworkTimeout
	}
	return d
}

// safeHost returns the bare host (no scheme, no path, no query) of rawURL — the only part of
// a request URL safe to surface to the model, since the URL may carry the query and a
// config'd API key (security-review M2). An unparseable URL yields a neutral placeholder
// rather than echoing the raw (possibly key-bearing) string.
func safeHost(rawURL string) string {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Host == "" {
		return "the requested host"
	}
	return u.Hostname()
}

// scrubURLError renders a transport error WITHOUT the request URL it embeds. Go's
// *url.Error stringifies as `<op> "<url>": <cause>`, and that url may carry a query and an
// API-key parameter; scrubURLError strips the URL substring so only the operation and the
// underlying cause survive (security-review M2). rawURL is the exact string to remove. A
// non-url.Error is returned unchanged (it carries no URL).
func scrubURLError(err error, rawURL string) string {
	var ue *url.Error
	if errors.As(err, &ue) {
		// Reconstruct from the parts that do NOT include the URL: the op and the cause.
		cause := "request failed"
		if ue.Err != nil {
			cause = ue.Err.Error()
		}
		return strings.TrimSpace(ue.Op) + ": " + redactRequestURL(cause, rawURL)
	}
	return redactRequestURL(err.Error(), rawURL)
}

// redactRequestURL removes the request URL from s in BOTH the form the caller supplied and its
// whitespace-trimmed form. The trimmed form matters because url-safety normalises the TRIMMED
// URL (security/urlsafety.go), so a nested error keyed on that form — from a model passing
// " http://exa mple.com/?key=SECRET" (note the leading space) — would otherwise be matched
// against a string that never appears, leaking the key (M2).
func redactRequestURL(s, rawURL string) string {
	s = redactSubstring(s, rawURL)
	return redactSubstring(s, strings.TrimSpace(rawURL))
}

// redactSubstring removes any occurrence of secret from s (defence-in-depth in case the
// URL leaks into a nested error's text), returning the cleaned string.
//
// It strips the %q-QUOTED form of secret as well as the raw one, because a plain substring
// search is exactly what an escaping formatter defeats (M-2): fmt's %q — which Go's own
// *url.Error uses to embed the URL in its text — escapes control characters, so a URL carrying
// an interior control byte appears as `…?key=SECRET\x01x` with a LITERAL backslash-x that the
// raw byte sequence never matches. strconv.Quote applies the identical escaping, so its inner
// form (the quoted string without its surrounding quotes) is the string to search for. When
// nothing needed escaping the two forms are the same and the second pass is skipped.
func redactSubstring(s, secret string) string {
	if secret == "" {
		return s
	}
	s = strings.ReplaceAll(s, secret, "[redacted-url]")
	quoted := strconv.Quote(secret)
	if inner := quoted[1 : len(quoted)-1]; inner != secret {
		s = strings.ReplaceAll(s, inner, "[redacted-url]")
	}
	return s
}

// The marker assertions are the compile-time half of the funnel contract: each of Apogee's
// own network tools carries urlFilteredNetworker, which it can only obtain by embedding
// networkTool (and therefore by reaching the network through do). Deleting an assertion does
// not silently un-vouch a tool: TestDefaultTools_EveryNetworkToolIsURLFiltered walks the
// default set and fails on any EffectNetwork tool without the marker.
var (
	_ domain.Tool               = (*WebFetch)(nil)
	_ domain.ExternalEffectTool = (*WebFetch)(nil)
	_ urlFilteredNetworker      = (*WebFetch)(nil)
	_ domain.Tool               = (*HTTPRequest)(nil)
	_ domain.ExternalEffectTool = (*HTTPRequest)(nil)
	_ urlFilteredNetworker      = (*HTTPRequest)(nil)
	_ domain.Tool               = (*WebSearch)(nil)
	_ domain.ExternalEffectTool = (*WebSearch)(nil)
	_ urlFilteredNetworker      = (*WebSearch)(nil)
)
