package mcp

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

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

	sse, _, _, _, err := buildTransport(context.Background(), ServerConfig{Name: "s", Transport: TransportSSE, Endpoint: "https://mcp.example.com/"}, guard, t.TempDir())
	if err != nil {
		t.Fatalf("sse buildTransport: %v", err)
	}
	if _, ok := sse.(*mcpsdk.SSEClientTransport); !ok {
		t.Errorf("sse transport = %T; want *mcpsdk.SSEClientTransport", sse)
	}

	sh, _, _, _, err := buildTransport(context.Background(), ServerConfig{Name: "s", Transport: TransportStreamableHTTP, Endpoint: "https://mcp.example.com/"}, guard, t.TempDir())
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
	tr, _, _, _, err := buildTransport(context.Background(), cfg, guard, t.TempDir())
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
			if _, _, _, _, err := buildTransport(context.Background(), cfg, security.URLGuard{}, workspace); err != nil {
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
			_, _, _, _, err := buildTransport(context.Background(), tc.cfg, tc.guard, t.TempDir())
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
	_, _, _, _, err := buildTransport(context.Background(), cfg, unresolvable, t.TempDir())
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
		client := endpointClient(t, ServerConfig{Name: "local", Transport: TransportStreamableHTTP, Endpoint: endpoint},
			security.URLGuard{}.WithResolver(fixedResolver(loopback...)))

		resp, err := client.Get(endpoint)
		if err != nil {
			t.Fatalf("GET the pinned endpoint: %v; want it to connect", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("status = %d; want 200", resp.StatusCode)
		}
		if got := reached.Load(); got != 1 {
			t.Errorf("the handler was reached %d time(s); want 1", got)
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
			resp.Body.Close()
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
		client := endpointClient(t, ServerConfig{Name: "local", Transport: TransportStreamableHTTP, Endpoint: endpoint},
			security.URLGuard{}.WithResolver(fixedResolver(net.IPv4(10, 1, 2, 3))))

		resp, err := client.Get(endpoint)
		if err == nil {
			resp.Body.Close()
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
		// endpoint, not "private addresses are fine on this connection".
		client := endpointClient(t, ServerConfig{Name: "local", Transport: TransportStreamableHTTP, Endpoint: endpoint},
			security.URLGuard{}.WithResolver(fixedResolver(loopback...)))

		resp, err := client.Get("http://10.9.8.7:9/mcp")
		if err == nil {
			resp.Body.Close()
			t.Fatal("a dial to an unpinned private address succeeded; want the SSRF floor to refuse it")
		}
		if !errors.Is(err, security.ErrSSRFBlocked) {
			t.Errorf("error = %v; want the SSRF floor", err)
		}
	})
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
//
// It swaps the proxyForRequest seam, so it must not run in parallel.
func TestGuardedClient_ProxiedEndpointPinsBothHosts(t *testing.T) {
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
	restore := proxyForRequest
	proxyForRequest = func(r *http.Request) (*url.URL, error) {
		if r.URL.Hostname() == "mcp.example" {
			return proxyURL, nil
		}
		return nil, nil
	}
	t.Cleanup(func() { proxyForRequest = restore })

	client := endpointClient(t, ServerConfig{Name: "proxied", Transport: TransportStreamableHTTP, Endpoint: endpoint},
		security.URLGuard{}.WithResolver(publicResolver))

	t.Run("the endpoint connects through the proxy", func(t *testing.T) {
		resp, err := client.Get(endpoint)
		if err != nil {
			t.Fatalf("GET the proxied endpoint: %v; want it to connect", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("status = %d; want 200", resp.StatusCode)
		}
		if got := proxied.Load(); got != 1 {
			t.Errorf("the proxy saw %d request(s); want 1", got)
		}
	})

	t.Run("an unproxied private address is still refused", func(t *testing.T) {
		resp, err := client.Get("http://10.9.8.7:9/mcp")
		if err == nil {
			resp.Body.Close()
			t.Fatal("a direct dial to an unpinned private address succeeded; want the SSRF floor to refuse it")
		}
		if !errors.Is(err, security.ErrSSRFBlocked) {
			t.Errorf("error = %v; want the SSRF floor", err)
		}
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
// a proxy URL may carry credentials.
//
// It swaps the proxyForRequest seam, so it must not run in parallel.
func TestVetEndpoint_AnUnusableProxyRefusesTheEndpoint(t *testing.T) {
	restore := proxyForRequest
	proxyForRequest = func(*http.Request) (*url.URL, error) {
		return nil, errors.New(`invalid proxy address "` + credentialedProxy + `"`)
	}
	t.Cleanup(func() { proxyForRequest = restore })

	endpoint, client, err := vetEndpoint(context.Background(), proxiedServer(), security.URLGuard{}.WithResolver(publicResolver))

	if err == nil {
		t.Fatal("an unusable egress proxy vetted the endpoint; want the connect refused")
	}
	if endpoint != "" || client != nil {
		t.Errorf("endpoint = %q, client = %v; want neither on a refusal", endpoint, client)
	}
	if !strings.Contains(err.Error(), "not a usable URL") {
		t.Errorf("error = %v; want it to name the unusable egress proxy", err)
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
//
// It swaps the proxyForRequest seam, so it must not run in parallel.
func TestVetEndpoint_AnUnpinnableProxyRefusesTheEndpoint(t *testing.T) {
	proxyURL, err := url.Parse(credentialedProxy)
	if err != nil {
		t.Fatalf("parse proxy URL: %v", err)
	}
	restore := proxyForRequest
	proxyForRequest = func(*http.Request) (*url.URL, error) { return proxyURL, nil }
	t.Cleanup(func() { proxyForRequest = restore })

	endpoint, client, err := vetEndpoint(context.Background(), proxiedServer(), unresolvableProxyGuard())

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
// whole of it. It deliberately does NOT swap proxyForRequest — that the default IS
// http.ProxyFromEnvironment is the claim under test — and drives the unpinnable path, which is
// the observable consequence of the environment having been consulted at all.
//
// net/http reads the proxy environment ONCE per process (a sync.Once behind
// http.ProxyFromEnvironment), so this test sees its own t.Setenv only while no earlier
// non-parallel test in the package has resolved a proxy through the real function. Every other
// proxy test here swaps the seam, which is what keeps that true; a failure here with no
// production change is that invariant breaking, not the proxy path.
func TestVetEndpoint_TheEgressProxyComesFromTheEnvironment(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://proxy.invalid:3128")
	// Neutralise any exclusion list the developer's own environment carries rather than inherit it.
	t.Setenv("NO_PROXY", "127.0.0.1")
	t.Setenv("no_proxy", "127.0.0.1")

	_, _, err := vetEndpoint(context.Background(), proxiedServer(), unresolvableProxyGuard())

	if err == nil {
		t.Fatal("the endpoint vetted with no proxy pinned; want HTTPS_PROXY to have been consulted")
	}
	if !strings.Contains(err.Error(), "proxy.invalid") {
		t.Errorf("error = %v; want it to name the environment's proxy", err)
	}
}

// TestGuardedClient_DoesNotFollowRedirects pins the redirect policy the MCP client builder
// reproduced field-for-field from the native funnel except for this one line: a redirect could
// send a vetted connection to an unvetted host, stepping around the endpoint's string-level
// allow/deny decision. The response is the redirect itself and the target is never fetched.
func TestGuardedClient_DoesNotFollowRedirects(t *testing.T) {
	t.Parallel()

	var followed atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/elsewhere", http.StatusFound)
	})
	mux.HandleFunc("/elsewhere", func(w http.ResponseWriter, _ *http.Request) {
		followed.Add(1)
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	endpoint := "http://" + net.JoinHostPort("localhost", serverPort(t, srv)) + "/mcp"
	client := endpointClient(t, ServerConfig{Name: "local", Transport: TransportSSE, Endpoint: endpoint},
		security.URLGuard{}.WithResolver(fixedResolver(net.IPv4(127, 0, 0, 1), net.IPv6loopback)))

	resp, err := client.Get(endpoint)
	if err != nil {
		t.Fatalf("GET the redirecting endpoint: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Errorf("status = %d; want the 302 itself (redirects are not followed)", resp.StatusCode)
	}
	if got := followed.Load(); got != 0 {
		t.Errorf("the redirect target was fetched %d time(s); want 0", got)
	}
}

// endpointClient builds cfg's transport and returns the http.Client the SDK would speak over, so
// a test drives the client the production path actually installs — pinned dial control, redirect
// policy and all — rather than a rebuild of it.
func endpointClient(t *testing.T, cfg ServerConfig, guard security.URLGuard) *http.Client {
	t.Helper()
	tr, _, _, _, err := buildTransport(context.Background(), cfg, guard, t.TempDir())
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
