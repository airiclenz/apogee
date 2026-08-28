package tuitest

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// routedHost and unmappedHost are the two destinations the route table decides between: a
// public-looking name mapped onto a loopback stand-in, and one named by no route at all. Neither is
// ever resolved — the proxy's dialler is handed the name and answers before DNS is asked.
const (
	routedHost   = "routed.example"
	unmappedHost = "unmapped.example"
)

// proxiedClient is a client that reaches every destination through p, the way an operator's
// HTTP_PROXY makes a program behave. It proxies nothing else, so what a destination receives it
// received through the proxy under test.
func proxiedClient(t testing.TB, p *Proxy) *http.Client {
	t.Helper()

	at, err := url.Parse(p.URL())
	if err != nil {
		t.Fatalf("parse the proxy URL %q: %v", p.URL(), err)
	}
	tr := &http.Transport{Proxy: http.ProxyURL(at)}
	t.Cleanup(tr.CloseIdleConnections)
	return &http.Client{Transport: tr, Timeout: 10 * time.Second}
}

// getThrough performs one GET for target through p, with header applied to the request, and returns
// the status and body. Every failure on the way is fatal: a test that cannot reach its own fixture
// has nothing to say about the proxy.
func getThrough(t testing.TB, p *Proxy, target string, header http.Header) (int, string) {
	t.Helper()

	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		t.Fatalf("build the request for %s: %v", target, err)
	}
	for name, values := range header {
		for _, value := range values {
			req.Header.Add(name, value)
		}
	}
	resp, err := proxiedClient(t, p).Do(req)
	if err != nil {
		t.Fatalf("GET %s through the proxy: %v", target, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read the reply to %s: %v", target, err)
	}
	return resp.StatusCode, string(body)
}

// headerRecorder is a loopback destination that keeps the headers of the last request it answered.
// [PageServer] deliberately records only hits — the claim here is about what a forwarded request
// arrives CARRYING, which needs the request itself.
type headerRecorder struct {
	srv *httptest.Server

	mu   sync.Mutex
	last http.Header
}

// recordHeaders starts that destination and stops it with the test.
func recordHeaders(t testing.TB) *headerRecorder {
	t.Helper()

	rec := &headerRecorder{}
	rec.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.mu.Lock()
		rec.last = r.Header.Clone()
		rec.mu.Unlock()
		_, _ = io.WriteString(w, "recorded")
	}))
	t.Cleanup(rec.srv.Close)
	return rec
}

// Addr is the `host:port` the recorder is served from — what a route table maps a destination onto.
func (rec *headerRecorder) Addr() string { return rec.srv.Listener.Addr().String() }

// Last is the headers of the last request answered, or nil when none has arrived.
func (rec *headerRecorder) Last() http.Header {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return rec.last
}

// TestForwardProxyRefusesANonAbsoluteRequestURI pins the 400: a request addressed the way a client
// addresses an ORIGIN server means the client never set http.Transport.Proxy, which is precisely the
// failure T-18's egress claims are looking for, so it must not be quietly served.
func TestForwardProxyRefusesANonAbsoluteRequestURI(t *testing.T) {
	proxy := ForwardProxy(t, nil)

	resp, err := http.Get(proxy.URL() + "/some/path")
	if err != nil {
		t.Fatalf("GET the proxy directly: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read the refusal: %v", err)
	}

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d for a non-absolute request URI", resp.StatusCode, http.StatusBadRequest)
	}
	if !strings.Contains(string(body), "non-absolute request URI") {
		t.Errorf("refusal body = %q, want it to name the non-absolute request URI", strings.TrimSpace(string(body)))
	}
}

// TestForwardProxyRefusesAnUnmappedHost is the file's guarantee made enforceable: a destination the
// caller never mapped is refused inside the process rather than dialled for real. The access log
// still carries the attempt — it records what reached the proxy, refused or not — which is what
// makes "a driven run tried to reach a stranger" a readable failure rather than an invisible packet.
func TestForwardProxyRefusesAnUnmappedHost(t *testing.T) {
	page := PageServer(t, "the routed page")
	proxy := ForwardProxy(t, map[string]string{routedHost: page.Addr()})

	status, body := getThrough(t, proxy, "http://"+unmappedHost+"/", nil)

	if status != http.StatusBadGateway {
		t.Errorf("status = %d, want %d for an unmapped host", status, http.StatusBadGateway)
	}
	if !strings.Contains(body, "no route for "+unmappedHost+":80") {
		t.Errorf("refusal body = %q, want it to name the unrouted destination", strings.TrimSpace(body))
	}
	if got := proxy.Saw(unmappedHost); got != 1 {
		t.Errorf("proxy.Saw(%q) = %d, want 1 — a refused attempt is still logged; log: %v",
			unmappedHost, got, proxy.Log())
	}
	if got := page.Hits(); got != 0 {
		t.Errorf("the routed page answered %d requests, want 0 — the unmapped host must reach nothing", got)
	}
}

// TestForwardProxyStripsHopByHopHeaders pins that a forwarded request arrives without the headers
// that describe the hop it arrived on. Passing them through is how a proxied request grows framing
// the destination never asked for, and it is invisible from the client's side.
func TestForwardProxyStripsHopByHopHeaders(t *testing.T) {
	dest := recordHeaders(t)
	proxy := ForwardProxy(t, map[string]string{routedHost: dest.Addr()})

	status, _ := getThrough(t, proxy, "http://"+routedHost+"/", http.Header{
		"Connection":         {"keep-alive"},
		"Proxy-Connection":   {"keep-alive"},
		"Keep-Alive":         {"timeout=5"},
		"X-Apogee-Forwarded": {"kept"},
	})

	if status != http.StatusOK {
		t.Fatalf("status = %d, want %d", status, http.StatusOK)
	}
	arrived := dest.Last()
	if arrived == nil {
		t.Fatal("the destination recorded no request")
	}
	for _, name := range []string{"Connection", "Proxy-Connection", "Keep-Alive"} {
		if got := arrived.Get(name); got != "" {
			t.Errorf("the destination saw %s: %q, want it consumed by the proxy", name, got)
		}
	}
	if got := arrived.Get("X-Apogee-Forwarded"); got != "kept" {
		t.Errorf("X-Apogee-Forwarded = %q, want %q — an end-to-end header is forwarded", got, "kept")
	}
}

// TestForwardProxyCarriesARoutedRequest is the happy path the refusals are the edges of: a mapped
// destination is served from its loopback stand-in, and the access log names the PUBLIC host the
// client asked for rather than the address actually dialled.
func TestForwardProxyCarriesARoutedRequest(t *testing.T) {
	const pageBody = "the routed page answered"
	page := PageServer(t, pageBody)
	proxy := ForwardProxy(t, map[string]string{routedHost: page.Addr()})

	status, body := getThrough(t, proxy, "http://"+routedHost+"/page", nil)

	if status != http.StatusOK {
		t.Errorf("status = %d, want %d", status, http.StatusOK)
	}
	if body != pageBody {
		t.Errorf("body = %q, want %q", body, pageBody)
	}
	if got := page.Hits(); got != 1 {
		t.Errorf("the page answered %d requests, want 1", got)
	}
	if got := proxy.Saw(routedHost); got != 1 {
		t.Errorf("proxy.Saw(%q) = %d, want 1; log: %v", routedHost, got, proxy.Log())
	}
}
