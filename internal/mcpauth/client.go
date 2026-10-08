package mcpauth

import (
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/airiclenz/apogee/internal/security"
)

// authClientTimeout bounds each request to an authorization server: discovery, registration and
// the token exchange are single short exchanges, never a session-long stream.
const authClientTimeout = 30 * time.Second

// maxAuthResponseBytes bounds every authorization-server response body. Metadata, a registration
// and a token reply are each a few kilobytes; the bound is what keeps an untrusted server from
// feeding an unbounded read (the SDK's registration call reads its body whole).
const maxAuthResponseBytes = 1 << 20

// NewAuthClient returns the http.Client a login speaks to authorization servers over. An
// authorization server is named by the MCP server's metadata, never by the operator, so it is
// untrusted: every request is pre-flighted by guard (scheme/host allow-deny and the resolved-IP
// SSRF floor), dialled under the floor (security.DialFloor) through a guarded client built for the
// request's own origin — so the egress proxy that applies to that origin is the one pinned — and
// its response body is bounded. Redirects are never followed and each request times out.
//
// proxy resolves the egress proxy (the MCP Host's Proxy); nil means http.ProxyFromEnvironment.
// The endpoint's own origin is not this client's to serve: the floor refuses a loopback or LAN
// host, so the probe and endpoint-origin requests go over the endpoint's vetted client instead.
func NewAuthClient(guard security.URLGuard, proxy func(*http.Request) (*url.URL, error)) *http.Client {
	return &http.Client{
		Transport: &authTransport{guard: guard, proxy: proxy, byOrigin: map[string]http.RoundTripper{}},
		Timeout:   authClientTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// authTransport is NewAuthClient's RoundTripper: it vets each request's URL and forwards it over
// a guarded transport built once per origin, then bounds the response body.
type authTransport struct {
	guard security.URLGuard
	proxy func(*http.Request) (*url.URL, error)

	mu       sync.Mutex
	byOrigin map[string]http.RoundTripper
}

// RoundTrip pre-flights req's URL, forwards it over its origin's guarded transport and bounds a
// response body. A refused request has its body closed, as the RoundTripper contract requires.
func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	next, err := t.transportFor(req)
	if err != nil {
		if req.Body != nil {
			_ = req.Body.Close()
		}
		return nil, err
	}
	resp, err := next.RoundTrip(req)
	if err != nil || resp == nil || resp.Body == nil {
		return resp, err
	}
	// MaxBytesReader with no ResponseWriter is a limit reader that ERRORS past the bound instead
	// of truncating, so an oversize reply fails its decode rather than parsing a prefix.
	resp.Body = http.MaxBytesReader(nil, resp.Body, maxAuthResponseBytes)
	return resp, nil
}

// transportFor vets req's URL against the guard and returns the guarded transport for its origin,
// building and caching it on first use so connections to one authorization server are reused.
func (t *authTransport) transportFor(req *http.Request) (http.RoundTripper, error) {
	if err := t.guard.CheckContext(req.Context(), req.URL.String()); err != nil {
		return nil, fmt.Errorf("mcpauth: authorization server refused: %w", err)
	}
	origin, ok := security.CanonicalOrigin(req.URL)
	if !ok {
		return nil, fmt.Errorf("mcpauth: authorization server refused: %w", security.ErrNoOrigin)
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	if next, ok := t.byOrigin[origin]; ok {
		return next, nil
	}
	client, err := t.guard.GuardedClient(req.Context(), req.URL, security.GuardedClientOptions{
		Proxy:  t.proxy,
		Policy: security.DialFloor,
	})
	if err != nil {
		return nil, fmt.Errorf("mcpauth: authorization server refused: %w", err)
	}
	t.byOrigin[origin] = client.Transport
	return client.Transport, nil
}
