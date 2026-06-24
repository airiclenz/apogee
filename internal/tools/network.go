package tools

import (
	"errors"
	"io"
	"net"
	"net/http"
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
// Every outbound URL passes the URLGuard (scheme/host allow-deny + the default-on, resolved-
// IP SSRF floor) BEFORE the request and, via the guard's SafeDialControl, at DIAL time too —
// so a DNS-rebinding name that passes the pre-flight check still cannot connect to a private
// IP (security/ssrf.go). url-safety is the tool's own concern (a tool-local guard), threaded
// from the host like path-safety is for the file tools.

// maxNetworkResponseBytes caps the body a network tool reads into a result so a huge
// download cannot exhaust memory or flood the model's context. It mirrors the file tools'
// read ceiling in spirit (a single call's blast radius is bounded).
const maxNetworkResponseBytes = 2 * 1024 * 1024

// defaultNetworkTimeout bounds a single network call so a slow/hung endpoint never wedges a
// Turn. http_request may lower it via its timeout_seconds argument; it never raises it past
// the ceiling.
const (
	defaultNetworkTimeout = 30 * time.Second
	maxNetworkTimeout     = 120 * time.Second
)

// newHTTPClient builds an http.Client whose transport validates the ACTUAL connected IP at
// dial time against the guard's SSRF floor (the DNS-rebinding defence), with the given
// overall timeout. It is the single place the network tools obtain a client so the dial-time
// floor is never accidentally skipped.
func newHTTPClient(guard security.URLGuard, timeout time.Duration) *http.Client {
	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
		Control: guard.SafeDialControl(), // re-check the connected IP — closes DNS-rebinding TOCTOU
	}
	transport := &http.Transport{
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
	}
}

// readCappedBody reads at most maxNetworkResponseBytes from r, reporting whether the body was
// truncated so the result can say so. It never returns the read error as a tool failure — a
// partial body is still useful to the model — except for ctx cancellation, which the caller
// detects separately.
func readCappedBody(r io.Reader) (body string, truncated bool) {
	limited := io.LimitReader(r, maxNetworkResponseBytes+1)
	data, _ := io.ReadAll(limited)
	if len(data) > maxNetworkResponseBytes {
		return string(data[:maxNetworkResponseBytes]), true
	}
	return string(data), false
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
	if seconds <= 0 {
		return defaultNetworkTimeout
	}
	d := time.Duration(seconds) * time.Second
	if d > maxNetworkTimeout {
		return maxNetworkTimeout
	}
	return d
}

var (
	_ domain.Tool               = (*WebFetch)(nil)
	_ domain.ExternalEffectTool = (*WebFetch)(nil)
	_ domain.Tool               = (*HTTPRequest)(nil)
	_ domain.ExternalEffectTool = (*HTTPRequest)(nil)
	_ domain.Tool               = (*WebSearch)(nil)
	_ domain.ExternalEffectTool = (*WebSearch)(nil)
)
