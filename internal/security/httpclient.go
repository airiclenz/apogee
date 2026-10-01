package security

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"
)

// ----------------------------------------------------------------------------
// The guarded HTTP client — one recipe for "a client for this vetted destination"
// ----------------------------------------------------------------------------
//
// Every outbound HTTP connection the host makes on a model's or a server config's behalf is
// built here, from the point the destination has passed its pre-flight on: resolve the
// operator's egress proxy for the destination, choose the dial-time control the resulting
// connection needs, and assemble one fixed transport that never follows a redirect. The
// pre-flight itself stays with each adapter — it is where the adapter's own refusal wording and
// its own budget live — but the half that decides what the transport may DIAL lives once, so
// no adapter can drop the dial-time control, swap the transport numbers or follow a redirect
// by accident.

// Transport numbers every guarded client shares. They are the values both adapters' builders
// carried before the recipe moved here, unchanged.
const (
	guardedDialTimeout           = 10 * time.Second
	guardedMaxIdleConns          = 10
	guardedIdleConnTimeout       = 30 * time.Second
	guardedTLSHandshakeTimeout   = 10 * time.Second
	guardedExpectContinueTimeout = 1 * time.Second
)

// ErrProxyUnusable is the refusal for an egress proxy value the resolver cannot use. It wraps
// ErrURLBlocked, so a caller matching on the url-safety sentinel sees it too. The proxy value
// itself is deliberately never part of the error: the resolver quotes it back, and a proxy URL
// may carry credentials.
var ErrProxyUnusable = fmt.Errorf("%w: the egress proxy is not a usable URL", ErrURLBlocked)

// PinError is the refusal for a dial-time control that could not be pinned: one of Hosts —
// the egress proxy's, and under DialPinDestination the destination's — could not be resolved.
// Hosts are bare host names (url.URL.Hostname), never userinfo, so the error cannot carry a
// proxy's credentials. Err is PinnedDialControl's own failure and wraps ErrURLBlocked.
type PinError struct {
	Hosts []string
	Err   error
}

// Error names the hosts that were to be pinned and the cause.
func (e *PinError) Error() string {
	return "security: could not pin the dial to " + strings.Join(e.Hosts, ", ") + ": " + e.Err.Error()
}

// Unwrap exposes the resolver failure, so errors.Is(err, ErrURLBlocked) holds.
func (e *PinError) Unwrap() error { return e.Err }

// DialPolicy is the floor policy a guarded client's dial-time control applies to the
// destination itself.
type DialPolicy int

const (
	// DialFloor holds the destination to the blanket SSRF floor at dial time (SafeDialControl,
	// the DNS-rebinding backstop). It is the policy for a model-supplied URL, which no host
	// trust decision stands behind. With a proxy in force the transport dials the proxy instead,
	// so the control pins the proxy's own addresses and the floor covers everything else.
	DialFloor DialPolicy = iota

	// DialPinDestination exempts the destination's own resolved addresses from the floor — and
	// the egress proxy's, when one applies — and floors every other address (PinnedDialControl).
	// It is the policy for an endpoint the operator named in their own config (ADR 0012,
	// Amendment 2026-07-26), never for a model-supplied URL.
	DialPinDestination
)

// GuardedClientOptions parameterises GuardedClient.
type GuardedClientOptions struct {
	// Proxy resolves the egress proxy for a request. Nil means http.ProxyFromEnvironment: the
	// process's HTTP_PROXY / HTTPS_PROXY / NO_PROXY, the same environment the LLM client
	// honours through Go's default transport.
	Proxy func(*http.Request) (*url.URL, error)

	// Timeout is the client's overall timeout per request; zero means none (a session-long
	// connection bounds its requests by their contexts instead).
	Timeout time.Duration

	// Policy is the dial-time floor policy over the destination; the zero value is DialFloor.
	Policy DialPolicy
}

// GuardedClient builds the http.Client for target, a destination the caller's pre-flight has
// already vetted. It resolves the egress proxy for target ONCE, here, chooses the dial-time
// control from opts.Policy and that answer, and assembles a transport with fixed numbers that
// never follows a redirect: a redirect could send a vetted request to an unvetted host,
// sidestepping the pre-flight, so the caller sees the redirect itself and re-vets any follow-up.
//
// The order of judgement is the point. The PRE-FLIGHT (the caller's) judges the DESTINATION
// whether or not a proxy applies, so a private destination is refused before anything leaves
// the process and a proxy can never launder one. The dial-time control judges what is actually
// DIALLED: with a proxy in force that is the PROXY, whose own addresses are pinned (a proxy on
// loopback or the LAN is the normal case, and the blanket floor would refuse it).
//
// ctx bounds the pin's lookups. A nil target resolves no proxy: under DialFloor it gets the
// blanket control, under DialPinDestination it is a PinError (nothing to pin).
//
// It fails closed, never dialling around the proxy: an unusable proxy value is
// ErrProxyUnusable, and a proxy or destination that cannot be resolved is a *PinError. Both
// wrap ErrURLBlocked; neither names a proxy's credentials.
func (g URLGuard) GuardedClient(ctx context.Context, target *url.URL, opts GuardedClientOptions) (*http.Client, error) {
	proxy := opts.Proxy
	if proxy == nil {
		proxy = http.ProxyFromEnvironment
	}
	control, err := g.guardedDialControl(ctx, target, proxy, opts.Policy)
	if err != nil {
		return nil, err
	}

	dialer := &net.Dialer{
		Timeout: guardedDialTimeout,
		Control: control,
	}
	transport := &http.Transport{
		Proxy:                 proxy,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          guardedMaxIdleConns,
		IdleConnTimeout:       guardedIdleConnTimeout,
		TLSHandshakeTimeout:   guardedTLSHandshakeTimeout,
		ExpectContinueTimeout: guardedExpectContinueTimeout,
	}
	return &http.Client{
		Transport: transport,
		Timeout:   opts.Timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}, nil
}

// guardedDialControl chooses the dial-time control for target under policy: the hosts that
// stand behind a host trust decision (the proxy that applies, and the destination under
// DialPinDestination) are pinned, and every other address meets the floor. With nothing to pin
// under DialFloor it is the blanket SafeDialControl.
func (g URLGuard) guardedDialControl(
	ctx context.Context,
	target *url.URL,
	proxy func(*http.Request) (*url.URL, error),
	policy DialPolicy,
) (func(network, address string, c syscall.RawConn) error, error) {
	var pinned []string
	if policy == DialPinDestination && target != nil {
		pinned = append(pinned, target.Hostname())
	}
	if target != nil {
		proxyURL, err := proxy(&http.Request{URL: target})
		if err != nil {
			return nil, ErrProxyUnusable
		}
		if proxyURL != nil {
			pinned = append(pinned, proxyURL.Hostname())
		}
	}
	if policy == DialFloor && len(pinned) == 0 {
		return g.SafeDialControl(), nil
	}

	control, err := g.PinnedDialControl(ctx, pinned...)
	if err != nil {
		return nil, &PinError{Hosts: pinned, Err: err}
	}
	return control, nil
}
