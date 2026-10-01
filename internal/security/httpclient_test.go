package security

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// guardedClientCredentialedProxy is the shape an operator's HTTP(S)_PROXY value can really
// take; guardedClientProxyPassword is the half no refusal may ever carry.
const (
	guardedClientCredentialedProxy = "http://ops:hunter2@proxy.invalid:3128"
	guardedClientProxyPassword     = "hunter2"
)

// publicResolverGuard is a floor-ON guard whose resolver answers every host except
// proxy.invalid with one public address, so a test can name a non-loopback destination and
// still keep DNS hermetic. proxy.invalid (RFC 2606) is the proxy that cannot be resolved.
func publicResolverGuard() URLGuard {
	return URLGuard{}.WithResolver(func(_ context.Context, host string) ([]net.IP, error) {
		if host == "proxy.invalid" {
			return nil, errors.New("no such host")
		}
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	})
}

// countingServer starts a loopback server that answers handler and counts every request it
// saw, so a test can prove a refused dial never reached it.
func countingServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *int32) {
	t.Helper()
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// mustParseURL parses raw or fails the test.
func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return u
}

// getThrough sends a GET for target through client, bounded so a broken control cannot hang.
func getThrough(t *testing.T, client *http.Client, target string) (*http.Response, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := client.Do(req)
	if err == nil {
		t.Cleanup(func() { _ = resp.Body.Close() })
	}
	return resp, err
}

// TestGuardedClient_BuildsTheVettedDestinationsClient drives the one recipe every guarded
// HTTP client is built from: which dial-time control a destination gets with and without an
// egress proxy and under each floor policy, the two fail-closed proxy refusals (typed, wrapping
// ErrURLBlocked, never naming the proxy's password), the never-follow redirect policy, and the
// fixed transport field set the adapters relied on before the recipe moved here.
func TestGuardedClient_BuildsTheVettedDestinationsClient(t *testing.T) {
	t.Parallel()

	okHandler := func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) }
	noProxy := func(*http.Request) (*url.URL, error) { return nil, nil }

	cases := map[string]func(t *testing.T){
		"no proxy under DialFloor: a loopback dial meets the floor": func(t *testing.T) {
			srv, hits := countingServer(t, okHandler)
			client, err := publicResolverGuard().GuardedClient(
				context.Background(),
				mustParseURL(t, srv.URL),
				GuardedClientOptions{Proxy: noProxy},
			)
			if err != nil {
				t.Fatalf("GuardedClient: %v", err)
			}

			_, err = getThrough(t, client, srv.URL)

			if !errors.Is(err, ErrSSRFBlocked) {
				t.Errorf("err = %v; want the dial refused by the SSRF floor", err)
			}
			if got := atomic.LoadInt32(hits); got != 0 {
				t.Errorf("server saw %d requests; the floor must refuse the dial", got)
			}
		},
		"a proxy on loopback under DialFloor: the proxy's addresses are pinned": func(t *testing.T) {
			proxySrv, hits := countingServer(t, okHandler)
			client, err := publicResolverGuard().GuardedClient(
				context.Background(),
				mustParseURL(t, "http://example.test/"),
				GuardedClientOptions{Proxy: http.ProxyURL(mustParseURL(t, proxySrv.URL))},
			)
			if err != nil {
				t.Fatalf("GuardedClient: %v", err)
			}

			resp, err := getThrough(t, client, "http://example.test/")

			if err != nil {
				t.Fatalf("proxied GET: %v", err)
			}
			if resp.StatusCode != http.StatusOK || atomic.LoadInt32(hits) != 1 {
				t.Errorf("status %d, proxy hits %d; want 200 through the proxy once", resp.StatusCode, atomic.LoadInt32(hits))
			}
		},
		"DialPinDestination: the destination's own loopback address is pinned": func(t *testing.T) {
			srv, hits := countingServer(t, okHandler)
			client, err := publicResolverGuard().GuardedClient(
				context.Background(),
				mustParseURL(t, srv.URL),
				GuardedClientOptions{Proxy: noProxy, Policy: DialPinDestination},
			)
			if err != nil {
				t.Fatalf("GuardedClient: %v", err)
			}

			resp, err := getThrough(t, client, srv.URL)

			if err != nil {
				t.Fatalf("pinned GET: %v", err)
			}
			if resp.StatusCode != http.StatusOK || atomic.LoadInt32(hits) != 1 {
				t.Errorf("status %d, hits %d; want 200 from the pinned destination", resp.StatusCode, atomic.LoadInt32(hits))
			}
		},
		"an unusable proxy value is ErrProxyUnusable and builds no client": func(t *testing.T) {
			client, err := publicResolverGuard().GuardedClient(
				context.Background(),
				mustParseURL(t, "http://example.test/"),
				GuardedClientOptions{Proxy: func(*http.Request) (*url.URL, error) {
					return nil, errors.New(`invalid proxy address "` + guardedClientCredentialedProxy + `"`)
				}},
			)

			if client != nil {
				t.Error("a refused proxy must build no client")
			}
			if !errors.Is(err, ErrProxyUnusable) || !errors.Is(err, ErrURLBlocked) {
				t.Errorf("err = %v; want ErrProxyUnusable wrapping ErrURLBlocked", err)
			}
			if err != nil && strings.Contains(err.Error(), guardedClientProxyPassword) {
				t.Errorf("err = %q; it names the proxy's password", err)
			}
		},
		"a proxy that cannot be resolved is a PinError naming its bare host": func(t *testing.T) {
			client, err := publicResolverGuard().GuardedClient(
				context.Background(),
				mustParseURL(t, "http://example.test/"),
				GuardedClientOptions{Proxy: http.ProxyURL(mustParseURL(t, guardedClientCredentialedProxy))},
			)

			if client != nil {
				t.Error("an unpinnable proxy must build no client")
			}
			var pinErr *PinError
			if !errors.As(err, &pinErr) || !errors.Is(err, ErrURLBlocked) {
				t.Fatalf("err = %v; want a *PinError wrapping ErrURLBlocked", err)
			}
			if len(pinErr.Hosts) != 1 || pinErr.Hosts[0] != "proxy.invalid" {
				t.Errorf("PinError.Hosts = %q; want exactly the proxy's bare host", pinErr.Hosts)
			}
			if strings.Contains(err.Error(), guardedClientProxyPassword) {
				t.Errorf("err = %q; it names the proxy's password", err)
			}
		},
		"a redirect is returned, never followed": func(t *testing.T) {
			var followed int32
			srv, _ := countingServer(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/next" {
					atomic.AddInt32(&followed, 1)
				}
				http.Redirect(w, r, "/next", http.StatusFound)
			})
			client, err := URLGuard{}.DisableIPFloor().GuardedClient(
				context.Background(),
				mustParseURL(t, srv.URL),
				GuardedClientOptions{Proxy: noProxy},
			)
			if err != nil {
				t.Fatalf("GuardedClient: %v", err)
			}

			resp, err := getThrough(t, client, srv.URL)

			if err != nil {
				t.Fatalf("GET: %v", err)
			}
			if resp.StatusCode != http.StatusFound || atomic.LoadInt32(&followed) != 0 {
				t.Errorf("status %d, follows %d; want the raw 302 and no follow", resp.StatusCode, atomic.LoadInt32(&followed))
			}
		},
		"the transport carries the fixed field set, ForceAttemptHTTP2 included": func(t *testing.T) {
			proxy := http.ProxyURL(mustParseURL(t, "http://127.0.0.1:3128"))
			client, err := publicResolverGuard().GuardedClient(
				context.Background(),
				mustParseURL(t, "http://example.test/"),
				GuardedClientOptions{Proxy: proxy, Timeout: 7 * time.Second},
			)
			if err != nil {
				t.Fatalf("GuardedClient: %v", err)
			}

			transport, ok := client.Transport.(*http.Transport)

			if !ok {
				t.Fatalf("Transport = %T; want *http.Transport", client.Transport)
			}
			if !transport.ForceAttemptHTTP2 {
				t.Error("ForceAttemptHTTP2 = false; want true")
			}
			if transport.Proxy == nil || transport.DialContext == nil {
				t.Error("Proxy and DialContext must both be set")
			}
			if transport.MaxIdleConns != 10 || transport.IdleConnTimeout != 30*time.Second ||
				transport.TLSHandshakeTimeout != 10*time.Second || transport.ExpectContinueTimeout != time.Second {
				t.Errorf("transport numbers = %d/%v/%v/%v; want 10/30s/10s/1s",
					transport.MaxIdleConns, transport.IdleConnTimeout,
					transport.TLSHandshakeTimeout, transport.ExpectContinueTimeout)
			}
			if client.Timeout != 7*time.Second {
				t.Errorf("client Timeout = %v; want the caller's 7s", client.Timeout)
			}
		},
	}

	for name, run := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			run(t)
		})
	}
}

// TestCanonicalOrigin_ComparesSchemeHostAndPort pins the origin comparator the request pin rests
// on: spellings of one origin compare equal (a default port written or not, a host's case or
// trailing root dot), and a different port, scheme or host does not.
func TestCanonicalOrigin_ComparesSchemeHostAndPort(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		vetted    string
		requested string
		wantEqual bool
	}{
		{name: "https default port written", vetted: "https://h/mcp", requested: "https://h:443/other", wantEqual: true},
		{name: "http default port written", vetted: "http://h:80/mcp", requested: "http://h/mcp?sessionid=1", wantEqual: true},
		{name: "upper-case host", vetted: "https://mcp.example/mcp", requested: "https://MCP.Example/mcp", wantEqual: true},
		{name: "trailing root dot", vetted: "https://mcp.example/mcp", requested: "https://mcp.example./mcp", wantEqual: true},
		{name: "different port", vetted: "https://h/mcp", requested: "https://h:8443/mcp", wantEqual: false},
		{name: "different scheme", vetted: "https://h/mcp", requested: "http://h/mcp", wantEqual: false},
		{name: "different host", vetted: "https://h/mcp", requested: "https://other/mcp", wantEqual: false},
		{name: "http port 443 is not https", vetted: "https://h/mcp", requested: "http://h:443/mcp", wantEqual: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			vetted, requested := mustParseURL(t, tt.vetted), mustParseURL(t, tt.requested)

			vettedOrigin, vettedOK := CanonicalOrigin(vetted)
			requestedOrigin, requestedOK := CanonicalOrigin(requested)

			if !vettedOK || !requestedOK {
				t.Fatalf("CanonicalOrigin ok = %v / %v; want both origins derived", vettedOK, requestedOK)
			}
			if got := vettedOrigin == requestedOrigin; got != tt.wantEqual {
				t.Errorf("%q vs %q: equal = %v (%q vs %q); want %v",
					tt.vetted, tt.requested, got, vettedOrigin, requestedOrigin, tt.wantEqual)
			}
		})
	}

	t.Run("a hostless URL names no origin", func(t *testing.T) {
		t.Parallel()
		if origin, ok := CanonicalOrigin(mustParseURL(t, "/mcp?sessionid=1")); ok {
			t.Errorf("CanonicalOrigin(relative) = %q, true; want no origin", origin)
		}
	})
}

// countingRoundTripper counts the requests a caller's WrapTransport layer was handed before
// forwarding them, so a test can see where that layer sits in the chain.
type countingRoundTripper struct {
	next http.RoundTripper
	seen atomic.Int32
}

// RoundTrip counts req and forwards it to next.
func (c *countingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	c.seen.Add(1)
	return c.next.RoundTrip(req)
}

// TestGuardedClient_PinsRequestsToTheDestinationsOrigin drives the origin pin an
// OriginRefusal turns on: a same-origin request passes through the caller's wrapped layer to
// the dial, a request to another origin is refused with exactly the caller's error before that
// layer sees it, and a destination with no origin to pin is ErrNoOrigin at build time.
func TestGuardedClient_PinsRequestsToTheDestinationsOrigin(t *testing.T) {
	t.Parallel()

	refusal := fmt.Errorf("caller: %w: left the origin", ErrURLBlocked)
	noProxy := func(*http.Request) (*url.URL, error) { return nil, nil }
	build := func(t *testing.T, guard URLGuard, target string) (*http.Client, *countingRoundTripper, error) {
		t.Helper()
		wrapped := &countingRoundTripper{}
		client, err := guard.GuardedClient(context.Background(), mustParseURL(t, target), GuardedClientOptions{
			Proxy:         noProxy,
			Policy:        DialPinDestination,
			OriginRefusal: refusal,
			WrapTransport: func(next http.RoundTripper) http.RoundTripper {
				wrapped.next = next
				return wrapped
			},
		})
		return client, wrapped, err
	}

	t.Run("a same-origin request reaches the destination through the wrapped layer", func(t *testing.T) {
		t.Parallel()
		srv, hits := countingServer(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
		client, wrapped, err := build(t, URLGuard{}, srv.URL+"/mcp")
		if err != nil {
			t.Fatalf("GuardedClient: %v", err)
		}

		resp, err := getThrough(t, client, srv.URL+"/other?x=1")

		if err != nil {
			t.Fatalf("same-origin GET: %v", err)
		}
		if resp.StatusCode != http.StatusOK || atomic.LoadInt32(hits) != 1 || wrapped.seen.Load() != 1 {
			t.Errorf("status %d, server hits %d, wrapped layer saw %d; want 200, 1, 1",
				resp.StatusCode, atomic.LoadInt32(hits), wrapped.seen.Load())
		}
	})

	t.Run("another origin is refused with the caller's error above the wrapped layer", func(t *testing.T) {
		t.Parallel()
		srv, hits := countingServer(t, func(w http.ResponseWriter, _ *http.Request) {})
		client, wrapped, err := build(t, URLGuard{}, srv.URL+"/mcp")
		if err != nil {
			t.Fatalf("GuardedClient: %v", err)
		}
		pin, ok := client.Transport.(*OriginPinTransport)
		if !ok || pin.Next != wrapped {
			t.Fatalf("client.Transport = %T; want the origin pin outermost over the wrapped layer", client.Transport)
		}
		other := strings.Replace(srv.URL, "http://", "https://", 1)

		_, err = getThrough(t, client, other)

		if !errors.Is(err, refusal) {
			t.Errorf("err = %v; want the caller's refusal", err)
		}
		if atomic.LoadInt32(hits) != 0 || wrapped.seen.Load() != 0 {
			t.Errorf("server hits %d, wrapped layer saw %d; want the request stopped at the pin",
				atomic.LoadInt32(hits), wrapped.seen.Load())
		}
	})

	t.Run("a destination with no origin is refused at build time", func(t *testing.T) {
		t.Parallel()

		client, _, err := build(t, URLGuard{}.DisableIPFloor(), "http://:8080/mcp")

		if client != nil || !errors.Is(err, ErrNoOrigin) || !errors.Is(err, ErrURLBlocked) {
			t.Errorf("client = %v, err = %v; want no client and ErrNoOrigin wrapping ErrURLBlocked", client, err)
		}
	})
}
