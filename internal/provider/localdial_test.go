package provider

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// stubResolver returns a dial seam that answers for host names: resolved maps a name to the IP
// literal the "system resolver" finds, and any other name fails with a *net.DNSError as the
// pure-Go resolver does. An IP literal is dialled for real through orig, the wrapped transport's
// own dialer.
func stubResolver(orig dialFunc, resolved map[string]string) dialFunc {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		if _, parseErr := netip.ParseAddr(host); parseErr == nil {
			return orig(ctx, network, address)
		}
		if ip, ok := resolved[host]; ok {
			return orig(ctx, network, net.JoinHostPort(ip, port))
		}
		return nil, &net.OpError{
			Op:  "dial",
			Net: network,
			Err: &net.DNSError{Err: "no such host", Name: host, IsNotFound: true},
		}
	}
}

// mdnsStub is an mDNS lookup seam that answers addrs (or err) and records the hosts it was asked
// for.
type mdnsStub struct {
	addrs []netip.Addr
	err   error

	mu    sync.Mutex
	hosts []string
}

func (s *mdnsStub) lookup(_ context.Context, host string) ([]netip.Addr, error) {
	s.mu.Lock()
	s.hosts = append(s.hosts, host)
	s.mu.Unlock()
	return s.addrs, s.err
}

func (s *mdnsStub) calls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.hosts...)
}

// stubMDNS returns an mDNS seam that answers addrs (or err) and records its calls.
func stubMDNS(addrs []netip.Addr, err error) *mdnsStub {
	return &mdnsStub{addrs: addrs, err: err}
}

// localFallbackClient returns a Client for baseURL over a transport of its own, built by
// withLocalFallback from a DefaultTransport clone: the system resolver answers from resolved
// (stubResolver) and mDNS from mdnsCalls. Nothing is shared with providerTransport, so the test
// may run in parallel.
func localFallbackClient(t *testing.T, baseURL string, resolved map[string]string, mdnsCalls *mdnsStub) *Client {
	t.Helper()
	base := http.DefaultTransport.(*http.Transport).Clone()
	transport := withLocalFallback(base, localDialer{
		dial:   stubResolver(base.DialContext, resolved),
		lookup: mdnsCalls.lookup,
	})
	t.Cleanup(transport.CloseIdleConnections)
	return NewClient(baseURL, "", WithHTTPClient(&http.Client{Transport: transport}))
}

// serverAs returns srv's URL with its host replaced by name, keeping the port.
func serverAs(t *testing.T, srv *httptest.Server, name string) string {
	t.Helper()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	u.Host = net.JoinHostPort(name, u.Port())
	return u.String()
}

var loopback = []netip.Addr{netip.MustParseAddr("127.0.0.1")}

func TestLocalFallback_DiscoverReachesMDNSAddress(t *testing.T) {
	t.Parallel()

	srv, _ := modelsServer(`{"data":[{"id":"model-a","context_length":4096}]}`)
	defer srv.Close()
	mdnsCalls := stubMDNS(loopback, nil)

	info, err := localFallbackClient(t, serverAs(t, srv, "Box.local"), nil, mdnsCalls).Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if info.ActiveModel != "model-a" {
		t.Errorf("ActiveModel = %q, want model-a", info.ActiveModel)
	}
	if got := mdnsCalls.calls(); len(got) == 0 || got[0] != "Box.local" {
		t.Errorf("mDNS lookups = %v, want Box.local", got)
	}
}

func TestLocalFallback_NonLocalHostSkipsMDNS(t *testing.T) {
	t.Parallel()

	srv, _ := modelsServer(`{"data":[{"id":"model-a"}]}`)
	defer srv.Close()
	mdnsCalls := stubMDNS(loopback, nil)

	_, err := localFallbackClient(t, serverAs(t, srv, "box.lan"), nil, mdnsCalls).Discover(context.Background())
	var dnsErr *net.DNSError
	if !errors.As(err, &dnsErr) {
		t.Fatalf("Discover error = %v, want a *net.DNSError", err)
	}
	if got := mdnsCalls.calls(); len(got) != 0 {
		t.Errorf("mDNS lookups = %v, want none for a non-.local host", got)
	}
}

func TestLocalFallback_MDNSFailureKeepsDNSError(t *testing.T) {
	t.Parallel()

	srv, _ := modelsServer(`{"data":[{"id":"model-a"}]}`)
	defer srv.Close()
	mdnsCalls := stubMDNS(nil, errors.New("mdns: no responder answered"))

	_, err := localFallbackClient(t, serverAs(t, srv, "box.local."), nil, mdnsCalls).Discover(context.Background())
	var transportErr *TransportError
	if !errors.As(err, &transportErr) {
		t.Fatalf("Discover error = %v (%T), want a *TransportError", err, err)
	}
	var dnsErr *net.DNSError
	if !errors.As(err, &dnsErr) {
		t.Fatalf("Discover error = %v, want it to unwrap to a *net.DNSError", err)
	}
	if dnsErr.Name != "box.local." {
		t.Errorf("DNSError.Name = %q, want box.local.", dnsErr.Name)
	}
	if got := mdnsCalls.calls(); len(got) != 1 {
		t.Errorf("mDNS lookups = %v, want exactly one", got)
	}
}

func TestLocalFallback_SystemResolvedLocalSkipsMDNS(t *testing.T) {
	t.Parallel()

	srv, _ := modelsServer(`{"data":[{"id":"model-a"}]}`)
	defer srv.Close()
	mdnsCalls := stubMDNS(loopback, nil)
	resolved := map[string]string{"box.local": "127.0.0.1"}

	if _, err := localFallbackClient(t, serverAs(t, srv, "box.local"), resolved, mdnsCalls).Discover(context.Background()); err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if got := mdnsCalls.calls(); len(got) != 0 {
		t.Errorf("mDNS lookups = %v, want none when the system resolves the host", got)
	}
}

// TestLocalFallback_DialsThroughCapturedDialer pins the regression guard: both the first dial and
// the per-address fallback dial go through the DialContext the wrapped transport already carried,
// never a fresh zero net.Dialer.
func TestLocalFallback_DialsThroughCapturedDialer(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	mdnsCalls := stubMDNS(loopback, nil)

	var mu sync.Mutex
	var dialled []string
	var real net.Dialer
	base := &http.Transport{
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			mu.Lock()
			dialled = append(dialled, address)
			mu.Unlock()
			if strings.HasPrefix(address, "box.local:") {
				return nil, &net.OpError{Op: "dial", Net: network, Err: &net.DNSError{Err: "no such host", Name: "box.local", IsNotFound: true}}
			}
			return real.DialContext(ctx, network, address)
		},
	}
	transport := withLocalFallback(base, localDialer{lookup: mdnsCalls.lookup})
	defer transport.CloseIdleConnections()

	resp, err := (&http.Client{Transport: transport}).Get(serverAs(t, srv, "box.local"))
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	_ = resp.Body.Close()

	port := strings.TrimPrefix(srv.URL, "http://127.0.0.1:")
	want := []string{"box.local:" + port, "127.0.0.1:" + port}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(dialled, ",") != strings.Join(want, ",") {
		t.Errorf("captured dialer saw %v, want %v", dialled, want)
	}
}

func TestIsLocalHost(t *testing.T) {
	t.Parallel()

	for host, want := range map[string]bool{
		"box.local":       true,
		"Apollo-II.LOCAL": true,
		"box.local.":      true,
		"local":           false,
		"box.localhost":   false,
		"box.lan":         false,
		"127.0.0.1":       false,
	} {
		if got := isLocalHost(host); got != want {
			t.Errorf("isLocalHost(%q) = %v, want %v", host, got, want)
		}
	}
}
