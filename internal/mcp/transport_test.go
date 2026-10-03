package mcp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/security"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// publicResolver maps every host to a public IP so a transport that should build (a non-blocked
// endpoint) does, without touching real DNS.
func publicResolver(_ context.Context, _ string) ([]net.IP, error) {
	return []net.IP{net.IPv4(93, 184, 216, 34)}, nil // example.com's documented address
}

// fixedResolver answers every host with ips — the seam that makes the CHECK-time answer
// independent of what the transport's own resolver will say at CONNECT time, which is how the
// rebinding tests below stage a rebind with no rebinding nameserver.
func fixedResolver(ips ...net.IP) func(context.Context, string) ([]net.IP, error) {
	return func(context.Context, string) ([]net.IP, error) { return ips, nil }
}

// TestBuildTransportHTTPKinds asserts the two HTTP transports build their SDK types when the
// endpoint passes url-safety — the success side of buildTransport for sse / streamable-http.
func TestBuildTransportHTTPKinds(t *testing.T) {
	t.Parallel()
	guard := security.URLGuard{}.WithResolver(publicResolver)

	sse, _, _, _, err := buildTransport(context.Background(), Host{}, ServerConfig{Name: "s", Transport: TransportSSE, Endpoint: "https://mcp.example.com/"}, guard, t.TempDir())
	if err != nil {
		t.Fatalf("sse buildTransport: %v", err)
	}
	if _, ok := sse.(*mcpsdk.SSEClientTransport); !ok {
		t.Errorf("sse transport = %T; want *mcpsdk.SSEClientTransport", sse)
	}

	sh, _, _, _, err := buildTransport(context.Background(), Host{}, ServerConfig{Name: "s", Transport: TransportStreamableHTTP, Endpoint: "https://mcp.example.com/"}, guard, t.TempDir())
	if err != nil {
		t.Fatalf("streamable-http buildTransport: %v", err)
	}
	if _, ok := sh.(*mcpsdk.StreamableClientTransport); !ok {
		t.Errorf("streamable-http transport = %T; want *mcpsdk.StreamableClientTransport", sh)
	}
}

// TestBuildTransport_HandsTheSDKTheNormalisedEndpoint pins that the string the SDK is given is
// the string url-safety judged. The endpoint the guard checked used to be the normalised form
// while the SDK was handed the RAW cfg.Endpoint — the check-one-string/dial-another divergence
// the native funnel removed (M-1), still live here.
func TestBuildTransport_HandsTheSDKTheNormalisedEndpoint(t *testing.T) {
	t.Parallel()
	guard := security.URLGuard{}.WithResolver(publicResolver)

	// Whitespace, an upper-case host and a trailing DNS root dot — three spellings that reach
	// the same server and that Go's transport normalises away before dialling.
	cfg := ServerConfig{Name: "s", Transport: TransportSSE, Endpoint: "  http://MCP.Example.COM./sse  "}
	tr, _, _, _, err := buildTransport(context.Background(), Host{}, cfg, guard, t.TempDir())
	if err != nil {
		t.Fatalf("buildTransport: %v", err)
	}
	sse, ok := tr.(*mcpsdk.SSEClientTransport)
	if !ok {
		t.Fatalf("transport = %T; want *mcpsdk.SSEClientTransport", tr)
	}
	if want := "http://mcp.example.com/sse"; sse.Endpoint != want {
		t.Errorf("SDK endpoint = %q; want the normalised %q", sse.Endpoint, want)
	}
}

// TestBuildTransport_EndpointRidesHostPolicyNotTheFloor pins the shape of the pre-flight check
// after the 2026-07-26 amendment: the user's scheme/host allow-deny still governs a configured
// endpoint, and the resolved-IP SSRF floor no longer does. A private endpoint is the ordinary
// case (a local or LAN MCP server) and used to make Connect — and therefore startup — fail.
func TestBuildTransport_EndpointRidesHostPolicyNotTheFloor(t *testing.T) {
	t.Parallel()

	// A loopback / LAN endpoint that the floor used to refuse now builds. It is resolved for
	// PINNING (an IP literal needs no lookup), not for a floor verdict.
	workspace := t.TempDir()
	for _, endpoint := range []string{"http://127.0.0.1:7331/mcp", "http://192.168.64.1:7331/mcp", "http://[::1]:7331/mcp"} {
		for _, transport := range []Transport{TransportSSE, TransportStreamableHTTP} {
			cfg := ServerConfig{Name: "local", Transport: transport, Endpoint: endpoint}
			if _, _, _, _, err := buildTransport(context.Background(), Host{}, cfg, security.URLGuard{}, workspace); err != nil {
				t.Errorf("%s endpoint %s: %v; want it to build (config-file endpoints are floor-exempt)", transport, endpoint, err)
			}
		}
	}

	// The host allow-deny policy is untouched — it is a user policy, not the anti-model floor.
	denying := security.URLGuard{DenyHosts: []string{"blocked.example"}}.WithResolver(publicResolver)
	blocked := []struct {
		name  string
		guard security.URLGuard
		cfg   ServerConfig
	}{
		{"denied host", denying, ServerConfig{Name: "s", Transport: TransportSSE, Endpoint: "https://blocked.example/mcp"}},
		{"denied subdomain", denying, ServerConfig{Name: "s", Transport: TransportSSE, Endpoint: "https://sub.blocked.example/mcp"}},
		{"root-dotted denied host", denying, ServerConfig{Name: "s", Transport: TransportSSE, Endpoint: "https://blocked.example./mcp"}},
		{"non-http scheme", security.URLGuard{}, ServerConfig{Name: "s", Transport: TransportSSE, Endpoint: "ftp://mcp.example.com/"}},
		{"unparseable endpoint", security.URLGuard{}, ServerConfig{Name: "s", Transport: TransportSSE, Endpoint: "http://exa mple.com/\x01"}},
	}
	for _, tc := range blocked {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, _, err := buildTransport(context.Background(), Host{}, tc.cfg, tc.guard, t.TempDir())
			if err == nil {
				t.Fatalf("endpoint %q built without error; want a refusal", tc.cfg.Endpoint)
			}
			if !strings.Contains(err.Error(), tc.cfg.Name) {
				t.Errorf("error = %v; want it to name the server", err)
			}
		})
	}
}

// TestBuildTransport_UnresolvableEndpointFailsClosed pins that an endpoint whose addresses
// cannot be learned is a connect-time error rather than an unpinned connection: the exemption
// is "this endpoint's own addresses", so an endpoint with no known addresses has nothing to
// exempt and must not be dialled blind. (The pre-flight floor failed closed on the same cause.)
func TestBuildTransport_UnresolvableEndpointFailsClosed(t *testing.T) {
	t.Parallel()
	unresolvable := security.URLGuard{}.WithResolver(func(context.Context, string) ([]net.IP, error) {
		return nil, errors.New("no such host")
	})
	cfg := ServerConfig{Name: "s", Transport: TransportStreamableHTTP, Endpoint: "https://mcp.example.com/"}
	_, _, _, _, err := buildTransport(context.Background(), Host{}, cfg, unresolvable, t.TempDir())
	if err == nil {
		t.Fatal("unresolvable endpoint built a transport; want a connect-time error")
	}
	if !errors.Is(err, security.ErrURLBlocked) {
		t.Errorf("error = %v; want a url-safety refusal", err)
	}
}

// ----------------------------------------------------------------------------
// The dial-time control — the half that makes the pre-flight exemption safe
// ----------------------------------------------------------------------------

// TestGuardedClient_PinsTheEndpointAndRefusesEverythingElsePrivate is this change's real
// acceptance. The pre-flight exemption alone would be a no-op (the dial control refused every
// private address), so the control became ENDPOINT-AWARE: the configured endpoint's own
// resolved addresses pass, and every other address still meets the SSRF floor. The three cases
// are driven through the client the production path actually installs.
//
// The rebind is staged WITHOUT a rebinding nameserver: the guard's injected resolver answers the
// pin lookup while the transport resolves the same name for real, so the check-time and
// connect-time answers genuinely differ. The request is addressed by NAME (`localhost`), because
// an IP-literal endpoint is pinned directly and never consults the injected resolver;
// hermeticity rests on `localhost` resolving through the hosts file (no DNS, no network).
func TestGuardedClient_PinsTheEndpointAndRefusesEverythingElsePrivate(t *testing.T) {
	t.Parallel()

	var reached atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("private page"))
	}))
	defer srv.Close()
	endpoint := "http://" + net.JoinHostPort("localhost", serverPort(t, srv)) + "/mcp"

	// The loopback addresses `localhost` really has — what the pin must hold for the endpoint
	// itself to be reachable.
	loopback := []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback}

	t.Run("the endpoint's own private address connects", func(t *testing.T) {
		reached.Store(0)
		client := endpointClient(t, Host{}, ServerConfig{Name: "local", Transport: TransportStreamableHTTP, Endpoint: endpoint},
			security.URLGuard{}.WithResolver(fixedResolver(loopback...)))

		resp, err := client.Get(endpoint)
		if err != nil {
			t.Fatalf("GET the pinned endpoint: %v; want it to connect", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("status = %d; want 200", resp.StatusCode)
		}
		if got := reached.Load(); got != 1 {
			t.Errorf("the handler was reached %d time(s); want 1", got)
		}
	})

	t.Run("closing the bounded body closes the real one", func(t *testing.T) {
		// The SDK closes resp.Body itself; the bound wraps it, so Close must reach through or
		// every connection leaks. The recorder sits between the bound and the real transport.
		client := endpointClient(t, Host{}, ServerConfig{Name: "local", Transport: TransportStreamableHTTP, Endpoint: endpoint},
			security.URLGuard{}.WithResolver(fixedResolver(loopback...)))
		bounded, ok := beneathOriginPin(t, client).(*boundedBodyTransport)
		if !ok {
			t.Fatalf("the transport beneath the origin pin is not the bounded-body RoundTripper")
		}
		recorder := &closeRecordingTransport{next: bounded.next}
		bounded.next = recorder

		resp, err := client.Get(endpoint)
		if err != nil {
			t.Fatalf("GET the pinned endpoint: %v", err)
		}
		body, ok := resp.Body.(*boundedBody)
		if !ok {
			t.Fatalf("resp.Body = %T; want the bounded body", resp.Body)
		}
		if body.framing != frameWholeBody {
			t.Errorf("a %q reply is framed %d; want the whole-body framing %d",
				resp.Header.Get("Content-Type"), body.framing, frameWholeBody)
		}
		if err := resp.Body.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		if recorder.closed.Load() != 1 {
			t.Errorf("the underlying body was closed %d time(s); want 1", recorder.closed.Load())
		}
	})

	t.Run("the blanket floor would have refused it", func(t *testing.T) {
		// The negative control: the SAME request through the blanket dial control fails, so the
		// case above proves the PIN let it through rather than the floor being absent.
		reached.Store(0)
		client := &http.Client{Transport: &http.Transport{
			DialContext: (&net.Dialer{Control: security.URLGuard{}.SafeDialControl()}).DialContext,
		}}
		resp, err := client.Get(endpoint)
		if err == nil {
			_ = resp.Body.Close()
			t.Fatal("the blanket floor connected to a loopback endpoint; the pin case proves nothing")
		}
		if !errors.Is(err, security.ErrSSRFBlocked) {
			t.Errorf("error = %v; want the SSRF floor", err)
		}
		if got := reached.Load(); got != 0 {
			t.Errorf("the handler was reached %d time(s); want 0", got)
		}
	})

	t.Run("a rebind to a different private address is refused", func(t *testing.T) {
		// Check time says 10.1.2.3 (so THAT is what gets pinned); connect time resolves
		// `localhost` for real and reaches the loopback server — a different private address,
		// which the floor still refuses.
		reached.Store(0)
		client := endpointClient(t, Host{}, ServerConfig{Name: "local", Transport: TransportStreamableHTTP, Endpoint: endpoint},
			security.URLGuard{}.WithResolver(fixedResolver(net.IPv4(10, 1, 2, 3))))

		resp, err := client.Get(endpoint)
		if err == nil {
			_ = resp.Body.Close()
			t.Fatal("a rebound connect succeeded; want the SSRF floor to refuse it")
		}
		if !errors.Is(err, security.ErrSSRFBlocked) {
			t.Errorf("error = %v; want the SSRF floor", err)
		}
		if got := reached.Load(); got != 0 {
			t.Errorf("the handler was reached %d time(s); want 0", got)
		}
	})

	t.Run("another private address on the pinned client is refused", func(t *testing.T) {
		// Where a redirect Location or an SSE endpoint event would point the transport: a
		// private address that is NOT the one the user configured. The exemption is one
		// endpoint, not "private addresses are fine on this connection". The origin pin refuses
		// it first; beneath the pin, the dial control's SSRF floor still refuses it on its own.
		client := endpointClient(t, Host{}, ServerConfig{Name: "local", Transport: TransportStreamableHTTP, Endpoint: endpoint},
			security.URLGuard{}.WithResolver(fixedResolver(loopback...)))

		assertRefused(t, client, "http://10.9.8.7:9/mcp", security.ErrURLBlocked)
		assertRefused(t, &http.Client{Transport: beneathOriginPin(t, client)}, "http://10.9.8.7:9/mcp", security.ErrSSRFBlocked)
	})
}

// beneathOriginPin returns the RoundTripper a guarded client's origin pin forwards to — the
// bounded, dial-controlled chain — so a test can prove what that chain refuses on its own.
func beneathOriginPin(t *testing.T, client *http.Client) http.RoundTripper {
	t.Helper()
	pin, ok := client.Transport.(*security.OriginPinTransport)
	if !ok {
		t.Fatalf("client.Transport = %T; want the origin pin outermost", client.Transport)
	}
	return pin.Next
}

// assertRefused GETs target through client and requires the request to fail with an error
// wrapping want.
func assertRefused(t *testing.T, client *http.Client, target string, want error) {
	t.Helper()
	resp, err := client.Get(target)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatalf("GET %s succeeded; want a refusal wrapping %v", target, want)
	}
	if !errors.Is(err, want) {
		t.Errorf("GET %s error = %v; want it to wrap %v", target, err, want)
	}
}

// TestGuardedClient_ProxiedEndpointPinsBothHosts pins the proxy half of the dial-time control:
// when the operator's HTTP(S)_PROXY applies to a configured endpoint, the transport connects to
// the PROXY, so the proxy's own addresses have to be pinned alongside the endpoint's or the
// connection cannot happen at all. The proxy here is on loopback, which the blanket floor
// refuses (the sibling test above is that negative control), so the successful GET is the pin.
//
// The second case is the bound: a destination the proxy does NOT carry (the NO_PROXY shape)
// dials direct and still meets the floor, so pinning the proxy widens the exemption by exactly
// two hosts and not by "every address this client reaches".
func TestGuardedClient_ProxiedEndpointPinsBothHosts(t *testing.T) {
	t.Parallel()

	var proxied atomic.Int64
	proxySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxied.Add(1)
		if !strings.HasPrefix(r.RequestURI, "http://") {
			t.Errorf("proxy saw request URI %q; want an absolute URL", r.RequestURI)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("via proxy"))
	}))
	defer proxySrv.Close()
	proxyURL, err := url.Parse(proxySrv.URL)
	if err != nil {
		t.Fatalf("parse proxy URL: %v", err)
	}

	// A proxy that carries the endpoint's host only — the NO_PROXY shape, and what makes the
	// second case a direct dial rather than another trip through the proxy.
	const endpoint = "http://mcp.example/mcp"
	host := Host{Proxy: func(r *http.Request) (*url.URL, error) {
		if r.URL.Hostname() == "mcp.example" {
			return proxyURL, nil
		}
		return nil, nil
	}}

	client := endpointClient(t, host, ServerConfig{Name: "proxied", Transport: TransportStreamableHTTP, Endpoint: endpoint},
		security.URLGuard{}.WithResolver(publicResolver))

	t.Run("the endpoint connects through the proxy", func(t *testing.T) {
		resp, err := client.Get(endpoint)
		if err != nil {
			t.Fatalf("GET the proxied endpoint: %v; want it to connect", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("status = %d; want 200", resp.StatusCode)
		}
		if got := proxied.Load(); got != 1 {
			t.Errorf("the proxy saw %d request(s); want 1", got)
		}
	})

	t.Run("an unproxied private address is still refused", func(t *testing.T) {
		// The origin pin refuses it first; beneath the pin, the direct dial still meets the floor.
		assertRefused(t, client, "http://10.9.8.7:9/mcp", security.ErrURLBlocked)
		assertRefused(t, &http.Client{Transport: beneathOriginPin(t, client)}, "http://10.9.8.7:9/mcp", security.ErrSSRFBlocked)
	})
}

// credentialedProxy is the shape an operator's HTTP(S)_PROXY value can really take — userinfo
// with a password — and credentialedProxyPassword is the half that must never reach the user or
// the model. Each of the two fail-closed paths below has its own way to leak one: the proxy
// resolver quotes the raw value back inside its own error, and a resolved proxy URL carries the
// credentials in its userinfo.
const (
	credentialedProxy         = "http://ops:hunter2@proxy.invalid:3128"
	credentialedProxyPassword = "hunter2"
)

// unresolvableProxyGuard answers every host with a public address except proxy.invalid, which
// cannot be resolved — an egress proxy whose addresses are unknowable, staged through the guard's
// resolver seam so no test reaches real DNS. (RFC 2606 reserves .invalid for exactly this.)
func unresolvableProxyGuard() security.URLGuard {
	return security.URLGuard{}.WithResolver(func(ctx context.Context, host string) ([]net.IP, error) {
		if host == "proxy.invalid" {
			return nil, errors.New("no such host")
		}
		return publicResolver(ctx, host)
	})
}

// proxiedServer is the endpoint the fail-closed proxy tests below vet: a public name, so the
// operator's HTTP(S)_PROXY applies to it and the guard's own resolver is what answers for it.
func proxiedServer() ServerConfig {
	return ServerConfig{Name: "proxied", Transport: TransportStreamableHTTP, Endpoint: "https://mcp.example/mcp"}
}

// TestVetEndpoint_AnUnusableProxyRefusesTheEndpoint pins the first of the MCP funnel's two
// fail-closed proxy paths: a proxy value the resolver cannot use refuses the connect outright
// rather than letting the session dial around the operator's egress policy — the fail-OPEN
// direction, which would put an MCP connection outside the proxy the operator requires. The
// refusal names no part of the value, because the resolver quotes it back in its own error and
// a proxy URL may carry credentials — but it does name the SERVER, and it wraps
// security.ErrURLBlocked like its sibling below, so a caller partitioning on the sentinel sees
// an unusable proxy as the url-safety refusal it is.
func TestVetEndpoint_AnUnusableProxyRefusesTheEndpoint(t *testing.T) {
	t.Parallel()
	host := Host{Proxy: func(*http.Request) (*url.URL, error) {
		return nil, errors.New(`invalid proxy address "` + credentialedProxy + `"`)
	}}

	endpoint, client, err := vetEndpoint(context.Background(), host, proxiedServer(), security.URLGuard{}.WithResolver(publicResolver))

	if err == nil {
		t.Fatal("an unusable egress proxy vetted the endpoint; want the connect refused")
	}
	if endpoint != "" || client != nil {
		t.Errorf("endpoint = %q, client = %v; want neither on a refusal", endpoint, client)
	}
	if !errors.Is(err, security.ErrURLBlocked) {
		t.Errorf("error = %v; want a url-safety refusal a caller can match on the sentinel", err)
	}
	if !strings.Contains(err.Error(), "not a usable URL") {
		t.Errorf("error = %v; want it to name the unusable egress proxy", err)
	}
	if !strings.Contains(err.Error(), `"proxied"`) {
		t.Errorf("error = %v; want it to name the server, which the settings row's reconnect note has no other source for", err)
	}
	if strings.Contains(err.Error(), credentialedProxyPassword) {
		t.Errorf("error = %v; it names the proxy's password", err)
	}
}

// TestVetEndpoint_AnUnpinnableProxyRefusesTheEndpoint pins the second path: a proxy whose own
// addresses cannot be learned cannot be pinned, and an unpinnable dial target refuses the
// connect for the same reason an unresolvable endpoint does — the addresses the connection would
// actually go to are unknown. The refusal names the proxy's HOST, which is all of a proxy URL
// that is ever safe to surface.
func TestVetEndpoint_AnUnpinnableProxyRefusesTheEndpoint(t *testing.T) {
	t.Parallel()
	proxyURL, err := url.Parse(credentialedProxy)
	if err != nil {
		t.Fatalf("parse proxy URL: %v", err)
	}
	host := Host{Proxy: func(*http.Request) (*url.URL, error) { return proxyURL, nil }}

	endpoint, client, err := vetEndpoint(context.Background(), host, proxiedServer(), unresolvableProxyGuard())

	if err == nil {
		t.Fatal("an unpinnable egress proxy vetted the endpoint; want the connect refused")
	}
	if endpoint != "" || client != nil {
		t.Errorf("endpoint = %q, client = %v; want neither on a refusal", endpoint, client)
	}
	if !errors.Is(err, security.ErrURLBlocked) {
		t.Errorf("error = %v; want a url-safety refusal", err)
	}
	if !strings.Contains(err.Error(), "proxy.invalid") {
		t.Errorf("error = %v; want it to name the proxy that could not be pinned", err)
	}
	if strings.Contains(err.Error(), credentialedProxyPassword) {
		t.Errorf("error = %v; it names the proxy's password", err)
	}
}

// TestVetEndpoint_TheEgressProxyComesFromTheEnvironment pins the surface itself: there is no
// per-server `proxy:` config key, so the process's HTTP_PROXY / HTTPS_PROXY / NO_PROXY is the
// whole of it. It deliberately passes an empty Host — that a nil Host.Proxy IS
// http.ProxyFromEnvironment is the claim under test — and drives the unpinnable path, which is
// the observable consequence of the environment having been consulted at all.
//
// net/http reads the proxy environment ONCE per process (a sync.Once behind
// http.ProxyFromEnvironment), so this test sees its own t.Setenv only while no earlier
// non-parallel test in the package has resolved a proxy through the real function. Every other
// proxy test here injects its resolver through Host.Proxy, which is what keeps that true; a
// failure here with no production change is that invariant breaking, not the proxy path.
func TestVetEndpoint_TheEgressProxyComesFromTheEnvironment(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://proxy.invalid:3128")
	// Neutralise any exclusion list the developer's own environment carries rather than inherit it.
	t.Setenv("NO_PROXY", "127.0.0.1")
	t.Setenv("no_proxy", "127.0.0.1")

	_, _, err := vetEndpoint(context.Background(), Host{}, proxiedServer(), unresolvableProxyGuard())

	if err == nil {
		t.Fatal("the endpoint vetted with no proxy pinned; want HTTPS_PROXY to have been consulted")
	}
	if !strings.Contains(err.Error(), "proxy.invalid") {
		t.Errorf("error = %v; want it to name the environment's proxy", err)
	}
}

// TestVetEndpoint_RefusalWordingsAreExact pins, byte for byte, the three sentences vetEndpoint
// refuses an HTTP endpoint with once its pre-flight has passed: an unusable egress proxy, a dial
// target that cannot be pinned (the proxy's host, or an endpoint with no host while the floor is
// on), and an endpoint with no origin for the request pin (the same endpoint with the floor off).
// The wording reaches the settings row's reconnect note verbatim, so a builder change must not
// move a word of it.
//
// Every row injects its own proxy resolver through Host.Proxy: net/http memoises the proxy
// environment, so a row that read it would decide what
// TestVetEndpoint_TheEgressProxyComesFromTheEnvironment sees.
func TestVetEndpoint_RefusalWordingsAreExact(t *testing.T) {
	t.Parallel()
	credentialed, err := url.Parse(credentialedProxy)
	if err != nil {
		t.Fatalf("parse proxy URL: %v", err)
	}
	noProxy := func(*http.Request) (*url.URL, error) { return nil, nil }
	hostless := ServerConfig{Name: "s", Transport: TransportStreamableHTTP, Endpoint: "http://:8080/mcp"}

	tests := []struct {
		name  string
		cfg   ServerConfig
		guard security.URLGuard
		proxy func(*http.Request) (*url.URL, error)
		want  string
	}{
		{
			name:  "unusable proxy",
			cfg:   proxiedServer(),
			guard: security.URLGuard{}.WithResolver(publicResolver),
			proxy: func(*http.Request) (*url.URL, error) {
				return nil, errors.New(`invalid proxy address "` + credentialedProxy + `"`)
			},
			want: `mcp: server "proxied": security: url blocked by url-safety: the configured egress proxy is not a usable URL`,
		},
		{
			name:  "unpinnable proxy",
			cfg:   proxiedServer(),
			guard: unresolvableProxyGuard(),
			proxy: func(*http.Request) (*url.URL, error) { return credentialed, nil },
			want: `mcp: server "proxied" endpoint blocked by url-safety: security: url blocked by url-safety: ` +
				`could not resolve host "proxy.invalid": no such host`,
		},
		{
			name:  "no origin with the floor off",
			cfg:   hostless,
			guard: security.URLGuard{}.DisableIPFloor(),
			proxy: noProxy,
			want:  `mcp: server "s": security: url blocked by url-safety: the endpoint has no origin to pin`,
		},
		{
			name:  "no host to pin with the floor on",
			cfg:   hostless,
			guard: security.URLGuard{},
			proxy: noProxy,
			want:  `mcp: server "s" endpoint blocked by url-safety: security: url blocked by url-safety: no host to pin`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, _, err := vetEndpoint(context.Background(), Host{Proxy: tt.proxy}, tt.cfg, tt.guard)

			if err == nil {
				t.Fatalf("vetEndpoint accepted %q; want %q", tt.cfg.Endpoint, tt.want)
			}
			if got := err.Error(); got != tt.want {
				t.Errorf("refusal =\n  %s\nwant\n  %s", got, tt.want)
			}
		})
	}
}

// TestOriginPin_RefusalWordingIsExact pins, byte for byte, the sentence the guarded client's
// origin pin refuses a cross-origin request with, as net/http surfaces it to the SDK.
func TestOriginPin_RefusalWordingIsExact(t *testing.T) {
	t.Parallel()
	const (
		endpoint    = "http://mcp.example/mcp"
		crossOrigin = "http://mcp.example:8080/mcp"
	)
	client := endpointClient(t, Host{}, ServerConfig{Name: "local", Transport: TransportSSE, Endpoint: endpoint},
		security.URLGuard{}.WithResolver(publicResolver))
	want := `Get "` + crossOrigin + `": mcp: server "local": security: url blocked by url-safety: ` +
		`a request left the configured endpoint's origin`

	resp, err := client.Get(crossOrigin)

	if err == nil {
		_ = resp.Body.Close()
		t.Fatalf("GET %s succeeded; want the origin pin to refuse it", crossOrigin)
	}
	if got := err.Error(); got != want {
		t.Errorf("refusal =\n  %s\nwant\n  %s", got, want)
	}
}

// TestGuardedClient_DoesNotFollowRedirects pins the redirect policy the MCP transports inherit
// from the shared guarded-client builder (security.URLGuard.GuardedClient): a redirect could
// send a vetted connection to an unvetted host, stepping around the endpoint's string-level
// allow/deny decision. The response is the redirect itself and the target is never fetched —
// whether the Location is another path on the pinned endpoint or another private address, which
// following would dial (and the floor would refuse) instead of handing the 302 back.
func TestGuardedClient_DoesNotFollowRedirects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		location string
	}{
		{name: "another path on the pinned endpoint", location: "/elsewhere"},
		{name: "another private address", location: "http://10.9.8.7:9/mcp"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var followed atomic.Int64
			mux := http.NewServeMux()
			mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, tt.location, http.StatusFound)
			})
			mux.HandleFunc("/elsewhere", func(w http.ResponseWriter, _ *http.Request) {
				followed.Add(1)
				w.WriteHeader(http.StatusOK)
			})
			srv := httptest.NewServer(mux)
			defer srv.Close()
			endpoint := "http://" + net.JoinHostPort("localhost", serverPort(t, srv)) + "/mcp"
			client := endpointClient(t, Host{}, ServerConfig{Name: "local", Transport: TransportSSE, Endpoint: endpoint},
				security.URLGuard{}.WithResolver(fixedResolver(net.IPv4(127, 0, 0, 1), net.IPv6loopback)))

			resp, err := client.Get(endpoint)

			if err != nil {
				t.Fatalf("GET the redirecting endpoint: %v; want the 302 handed back unfollowed", err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusFound {
				t.Errorf("status = %d; want the 302 itself (redirects are not followed)", resp.StatusCode)
			}
			if got := resp.Header.Get("Location"); got != tt.location {
				t.Errorf("Location = %q; want %q", got, tt.location)
			}
			if got := followed.Load(); got != 0 {
				t.Errorf("the redirect target was fetched %d time(s); want 0", got)
			}
		})
	}
}

// TestGuardedClient_AnOversizeBodyFailsTheRead pins the message bound on the HTTP lane, through
// the client the production path installs: a response body that grows one message past
// maxMCPMessageBytes fails its read with errMCPMessageTooLarge, and no byte past the cap reaches
// the caller. The bound is cumulative per message, never per line, so the many-short-line cases
// carry no message boundary anywhere — a per-line bound would let every one of them through.
func TestGuardedClient_AnOversizeBodyFailsTheRead(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		contentType string
		body        []byte
	}{
		{
			// The shape of a streamable JSON reply from a server that never stops writing.
			name:        "json: one newline-free line",
			contentType: "application/json",
			body:        bytes.Repeat([]byte("x"), maxMCPMessageBytes+1),
		},
		{
			// A plain JSON reply is one whole-body message: its newlines are no boundary.
			name:        "json: many short lines",
			contentType: "application/json",
			body:        append([]byte("[\n"), manyShortBodyLines(`"xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",`+"\n")...),
		},
		{
			// One SSE event whose lines never reach the blank line that would complete it.
			name:        "sse: many short lines of one event",
			contentType: "text/event-stream",
			body:        append([]byte("event: message\n"), manyShortBodyLines("data: xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx\n")...),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", tt.contentType)
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(tt.body)
			}))
			defer srv.Close()
			endpoint := "http://" + net.JoinHostPort("localhost", serverPort(t, srv)) + "/mcp"
			client := endpointClient(t, Host{}, ServerConfig{Name: "local", Transport: TransportStreamableHTTP, Endpoint: endpoint},
				security.URLGuard{}.WithResolver(fixedResolver(net.IPv4(127, 0, 0, 1), net.IPv6loopback)))
			req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
			if err != nil {
				t.Fatalf("build request: %v", err)
			}
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("POST the endpoint: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()

			got, err := io.ReadAll(resp.Body)

			if !errors.Is(err, errMCPMessageTooLarge) {
				t.Fatalf("read the body: err = %v (%d bytes read); want %v", err, len(got), errMCPMessageTooLarge)
			}
			if len(got) > maxMCPMessageBytes {
				t.Errorf("%d bytes reached the caller; want at most %d", len(got), maxMCPMessageBytes)
			}
		})
	}
}

// manyShortBodyLines repeats line until the result is longer than maxMCPMessageBytes, so a body
// built from it passes the bound only by accumulation — no single line comes near the cap.
func manyShortBodyLines(line string) []byte {
	return bytes.Repeat([]byte(line), maxMCPMessageBytes/len(line)+1)
}

// closeRecordingTransport forwards to next and counts the closes of each response body it hands
// back, so a test can see whether a wrapper's Close reached the real body.
type closeRecordingTransport struct {
	next   http.RoundTripper
	closed atomic.Int64
}

// RoundTrip forwards the request and wraps the response body's Close with the count.
func (c *closeRecordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := c.next.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	resp.Body = &countingCloser{ReadCloser: resp.Body, closed: &c.closed}
	return resp, nil
}

// countingCloser is a ReadCloser whose Close bumps a shared counter before closing.
type countingCloser struct {
	io.ReadCloser
	closed *atomic.Int64
}

// Close counts the call and closes the wrapped body.
func (c *countingCloser) Close() error {
	c.closed.Add(1)
	return c.ReadCloser.Close()
}

// endpointClient builds cfg's transport over host and returns the http.Client the SDK would speak
// over, so a test drives the client the production path actually installs — pinned dial control,
// redirect policy and all — rather than a rebuild of it.
func endpointClient(t *testing.T, host Host, cfg ServerConfig, guard security.URLGuard) *http.Client {
	t.Helper()
	tr, _, _, _, err := buildTransport(context.Background(), host, cfg, guard, t.TempDir())
	if err != nil {
		t.Fatalf("buildTransport(%s): %v", cfg.Endpoint, err)
	}
	switch v := tr.(type) {
	case *mcpsdk.SSEClientTransport:
		return v.HTTPClient
	case *mcpsdk.StreamableClientTransport:
		return v.HTTPClient
	default:
		t.Fatalf("transport = %T; want one of the HTTP transports", tr)
		return nil
	}
}

// serverPort is the port an httptest server listens on, so a test can re-address it by NAME
// (`localhost:<port>`) instead of by the IP literal httptest hands back — an IP-literal host is
// pinned directly and never reaches the injected resolver.
func serverPort(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse test server URL %q: %v", srv.URL, err)
	}
	return u.Port()
}

// refusedAddr returns a loopback host:port nothing listens on, so a request to it is refused.
func refusedAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

func TestEndpointRedactor_StdioAndNilAreTheIdentity(t *testing.T) {
	t.Parallel()

	if r := newEndpointRedactor(ServerConfig{Name: "local", Transport: TransportStdio, Command: "srv"}); r != nil {
		t.Errorf("newEndpointRedactor(stdio) = %v; want nil", r)
	}
	var none *security.OriginRedactor
	if got := none.Redact("x /mcp?token=SECRET"); got != "x /mcp?token=SECRET" {
		t.Errorf("nil Redact = %q; want the text unchanged", got)
	}
}

func TestConnect_RedactsTheEndpointFromARefusedConnect(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	endpoint := "http://" + refusedAddr(t) + "/mcp?token=SECRET"
	c, err := Connect(ctx, []ServerConfig{{Name: "remote", Transport: TransportStreamableHTTP, Endpoint: endpoint}},
		security.URLGuard{}, t.TempDir())
	if err == nil {
		_ = c.Close()
		t.Fatal("Connect to a refused endpoint succeeded; want an error")
	}
	if strings.Contains(err.Error(), "SECRET") || strings.Contains(err.Error(), "/mcp") {
		t.Errorf("Connect error = %q; want the endpoint cut to its origin", err.Error())
	}
}

func TestExecute_RedactsTheEndpointWhenTheServerDies(t *testing.T) {
	t.Parallel()

	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "dying", Version: "v0.0.1"}, nil)
	server.AddTool(&mcpsdk.Tool{Name: "ping", InputSchema: map[string]any{"type": "object"}},
		func(_ context.Context, _ *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "pong"}}}, nil
		})
	srv := httptest.NewServer(mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return server }, nil))
	endpoint := srv.URL + "/mcp?token=SECRET"

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c, err := Connect(ctx, []ServerConfig{{Name: "remote", Transport: TransportStreamableHTTP, Endpoint: endpoint}},
		security.URLGuard{}, t.TempDir())
	if err != nil {
		srv.Close()
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = c.Close() }()
	var ping domain.Tool
	for _, tool := range c.Tools() {
		if tool.Name() == "remote__ping" {
			ping = tool
		}
	}
	if ping == nil {
		srv.Close()
		t.Fatal("remote__ping was not surfaced")
	}

	// Close alone blocks on the standalone SSE GET; dropping the client connections first ends it.
	srv.CloseClientConnections()
	srv.Close()

	res, err := ping.Execute(ctx, domain.ToolCall{ID: "call-1", Tool: "remote__ping"})
	if err != nil {
		t.Fatalf("Execute returned a Go error (reserved for cancellation): %v", err)
	}
	if !res.IsError {
		t.Fatalf("Execute against a dead server = %q; want an error result", res.Content)
	}
	if strings.Contains(res.Content, "SECRET") || strings.Contains(res.Content, "/mcp") {
		t.Errorf("Execute error result = %q; want the endpoint cut to its origin", res.Content)
	}
}

// TestConnect_SSEEndpointEventToAnotherOriginFails is the audit finding's exploit: an SSE server
// whose `endpoint` event names a DIFFERENT origin — here another port on the same loopback
// address, which the dial pin alone lets through — must fail Connect with a url-safety refusal
// naming the server, and the other origin must never see a request.
func TestConnect_SSEEndpointEventToAnotherOriginFails(t *testing.T) {
	t.Parallel()

	var otherReached atomic.Int64
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		otherReached.Add(1)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer other.Close()
	hijacker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "event: endpoint\ndata: %s/message?sessionid=1\n\n", other.URL)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		<-r.Context().Done()
	}))
	defer func() {
		hijacker.CloseClientConnections()
		hijacker.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c, err := Connect(ctx, []ServerConfig{{Name: "hijacker", Transport: TransportSSE, Endpoint: hijacker.URL + "/sse"}},
		security.URLGuard{}, t.TempDir())

	if err == nil {
		_ = c.Close()
		t.Fatal("Connect followed an endpoint event to another origin; want it refused")
	}
	if !errors.Is(err, security.ErrURLBlocked) {
		t.Errorf("Connect error = %v; want it to wrap security.ErrURLBlocked", err)
	}
	if !strings.Contains(err.Error(), `"hijacker"`) {
		t.Errorf("Connect error = %q; want it to name the server", err.Error())
	}
	if got := otherReached.Load(); got != 0 {
		t.Errorf("the other origin saw %d request(s); want 0", got)
	}
}

// TestConnect_SSESameOriginEndpointEventConnects is the pin's negative control: the SDK's own SSE
// server announces its POST channel as a same-origin relative "/path?sessionid=…", which the pin
// admits, so the connection comes up and a tool call round-trips.
func TestConnect_SSESameOriginEndpointEventConnects(t *testing.T) {
	t.Parallel()

	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "sse", Version: "v0.0.1"}, nil)
	server.AddTool(&mcpsdk.Tool{Name: "ping", InputSchema: map[string]any{"type": "object"}},
		func(_ context.Context, _ *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "pong"}}}, nil
		})
	srv := httptest.NewServer(mcpsdk.NewSSEHandler(func(*http.Request) *mcpsdk.Server { return server }, nil))
	defer func() {
		srv.CloseClientConnections()
		srv.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c, err := Connect(ctx, []ServerConfig{{Name: "local", Transport: TransportSSE, Endpoint: srv.URL + "/sse"}},
		security.URLGuard{}, t.TempDir())
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = c.Close() }()
	var ping domain.Tool
	for _, tool := range c.Tools() {
		if tool.Name() == "local__ping" {
			ping = tool
		}
	}
	if ping == nil {
		t.Fatal("local__ping was not surfaced")
	}

	res, err := ping.Execute(ctx, domain.ToolCall{ID: "call-1", Tool: "local__ping"})

	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.IsError || res.Content != "pong" {
		t.Errorf("Execute = %+v; want the pong result", res)
	}
}

// headerRecorder wraps an MCP handler and records, per request, the JSON-RPC method its body
// names (empty for the SSE stream's GET) and whether every wanted header arrived with its value.
type headerRecorder struct {
	next   http.Handler
	want   map[string]string
	seen   atomic.Int64
	missed atomic.Int64
	init   atomic.Bool
	call   atomic.Bool
}

func (h *headerRecorder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewReader(body))
	h.seen.Add(1)
	for name, value := range h.want {
		if r.Header.Get(name) != value {
			h.missed.Add(1)
		}
	}
	if bytes.Contains(body, []byte(`"method":"initialize"`)) {
		h.init.Store(true)
	}
	if bytes.Contains(body, []byte(`"method":"tools/call"`)) {
		h.call.Store(true)
	}
	h.next.ServeHTTP(w, r)
}

// TestConnect_ConfiguredHeadersRideEveryRequest pins that an sse or streamable-http server meets
// the configured Headers and HeadersEnv on every request — the initialize handshake and a later
// tool call among them. Not parallel: t.Setenv.
func TestConnect_ConfiguredHeadersRideEveryRequest(t *testing.T) {
	t.Setenv("APOGEE_TEST_MCP_HEADER_TOKEN", "Bearer s3cret")
	want := map[string]string{"X-Tenant": "acme", "Authorization": "Bearer s3cret"}

	tests := []struct {
		name      string
		transport Transport
		handler   func(func(*http.Request) *mcpsdk.Server) http.Handler
		path      string
	}{
		{
			name:      "streamable-http",
			transport: TransportStreamableHTTP,
			handler: func(get func(*http.Request) *mcpsdk.Server) http.Handler {
				return mcpsdk.NewStreamableHTTPHandler(get, nil)
			},
			path: "/mcp",
		},
		{
			name:      "sse",
			transport: TransportSSE,
			handler: func(get func(*http.Request) *mcpsdk.Server) http.Handler {
				return mcpsdk.NewSSEHandler(get, nil)
			},
			path: "/sse",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "hdr", Version: "v0.0.1"}, nil)
			server.AddTool(&mcpsdk.Tool{Name: "ping", InputSchema: map[string]any{"type": "object"}},
				func(_ context.Context, _ *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
					return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "pong"}}}, nil
				})
			recorder := &headerRecorder{
				next: tt.handler(func(*http.Request) *mcpsdk.Server { return server }),
				want: want,
			}
			srv := httptest.NewServer(recorder)
			defer func() {
				srv.CloseClientConnections()
				srv.Close()
			}()
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			c, err := Connect(ctx, []ServerConfig{{
				Name:       "remote",
				Transport:  tt.transport,
				Endpoint:   srv.URL + tt.path,
				Headers:    map[string]string{"X-Tenant": "acme"},
				HeadersEnv: map[string]string{"Authorization": "APOGEE_TEST_MCP_HEADER_TOKEN"},
			}}, security.URLGuard{}, t.TempDir())
			if err != nil {
				t.Fatalf("Connect: %v", err)
			}
			defer func() { _ = c.Close() }()
			var ping domain.Tool
			for _, tool := range c.Tools() {
				if tool.Name() == "remote__ping" {
					ping = tool
				}
			}
			if ping == nil {
				t.Fatal("remote__ping was not surfaced")
			}

			res, err := ping.Execute(ctx, domain.ToolCall{ID: "call-1", Tool: "remote__ping"})

			if err != nil || res.IsError {
				t.Fatalf("Execute = %+v, %v; want the pong result", res, err)
			}
			if !recorder.init.Load() || !recorder.call.Load() {
				t.Fatalf("server saw initialize=%v tools/call=%v; want both", recorder.init.Load(), recorder.call.Load())
			}
			if missed := recorder.missed.Load(); missed != 0 {
				t.Errorf("%d header(s) missing or wrong across %d request(s); want every request to carry %v",
					missed, recorder.seen.Load(), want)
			}
		})
	}
}

// TestConnect_AnUnsetHeaderVariableFailsTheConnect pins that a HeadersEnv entry naming an unset
// variable fails the connect before any request leaves, with an error naming the server, the
// header and the variable — and neither a configured header value nor the endpoint's path or query.
func TestConnect_AnUnsetHeaderVariableFailsTheConnect(t *testing.T) {
	t.Parallel()
	var reached atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	for _, transport := range []Transport{TransportSSE, TransportStreamableHTTP} {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		c, err := Connect(ctx, []ServerConfig{{
			Name:       "remote",
			Transport:  transport,
			Endpoint:   srv.URL + "/mcp?token=SECRET",
			Headers:    map[string]string{"X-Tenant": "LITERALVALUE"},
			HeadersEnv: map[string]string{"Authorization": "APOGEE_TEST_MCP_HEADER_NEVER_SET"},
		}}, security.URLGuard{}, t.TempDir())
		cancel()

		if err == nil {
			_ = c.Close()
			t.Fatalf("%s: Connect with an unset header variable succeeded; want an error", transport)
		}
		msg := err.Error()
		for _, want := range []string{`"remote"`, "Authorization", "APOGEE_TEST_MCP_HEADER_NEVER_SET"} {
			if !strings.Contains(msg, want) {
				t.Errorf("%s: Connect error = %q; want it to contain %q", transport, msg, want)
			}
		}
		for _, leak := range []string{"LITERALVALUE", "SECRET", "/mcp"} {
			if strings.Contains(msg, leak) {
				t.Errorf("%s: Connect error = %q; want it free of %q", transport, msg, leak)
			}
		}
	}
	if got := reached.Load(); got != 0 {
		t.Errorf("the server saw %d request(s); want 0", got)
	}
}

// TestHeaderTransport_ClonesTheRequest pins that the header layer never writes into the request
// it was handed — the RoundTripper contract — while the request it forwards carries the headers.
func TestHeaderTransport_ClonesTheRequest(t *testing.T) {
	t.Parallel()
	var forwarded *http.Request
	next := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		forwarded = req
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Request: req}, nil
	})
	header := http.Header{}
	header.Set("X-Tenant", "acme")
	rt := &headerTransport{next: next, header: header}
	req := httptest.NewRequest(http.MethodPost, "http://mcp.example.com/mcp", nil)

	resp, err := rt.RoundTrip(req)

	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	_ = resp.Body.Close()
	if got := forwarded.Header.Get("X-Tenant"); got != "acme" {
		t.Errorf("forwarded X-Tenant = %q; want %q", got, "acme")
	}
	if got := req.Header.Get("X-Tenant"); got != "" {
		t.Errorf("caller's request X-Tenant = %q; want it untouched", got)
	}
}

// roundTripFunc adapts a function to http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
