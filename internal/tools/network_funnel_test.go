package tools

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/security"
)

// funnelTool is a minimal tool that reaches the network ONLY through the funnel: it embeds
// networkTool, which is the sole way to obtain the (unexported, unfakeable) url-filter
// marker. It stands in for web_fetch/http_request/web_search while those still hold their
// own guard, so the funnel's contract is provable on its own.
type funnelTool struct {
	toolSpec
	networkTool
}

// Execute satisfies domain.Tool; the funnel tests drive do directly, not Execute.
func (funnelTool) Execute(context.Context, domain.ToolCall) (domain.ToolResult, error) {
	return domain.ToolResult{}, nil
}

// newFunnelTool returns a funnelTool whose funnel carries guard.
func newFunnelTool(guard security.URLGuard) funnelTool {
	return funnelTool{
		toolSpec:    toolSpec{name: "funnel_probe", description: "test-only funnel carrier"},
		networkTool: networkTool{guard: guard},
	}
}

// TestIsURLFilteredNetworker_MarkerCarrier proves a tool that embeds the funnel carries the
// url-filter marker, which is what dispatch trusts to auto-run a network tool unattended in
// Auto. Mirrors TestMarkerAccessors_MarkerTool on the write axis.
func TestIsURLFilteredNetworker_MarkerCarrier(t *testing.T) {
	t.Parallel()

	if !IsURLFilteredNetworker(domain.Tool(newFunnelTool(loopbackGuard()))) {
		t.Error("IsURLFilteredNetworker(funnel embedder) = false, want true (embedding networkTool carries the marker)")
	}
}

// TestIsURLFilteredNetworker_NonCarrier is the negative contrast: a tool that does not
// route through the funnel must NOT be reported as url-filtered, however it is written —
// the marker method is unexported, so only this package's funnel embedders satisfy it.
func TestIsURLFilteredNetworker_NonCarrier(t *testing.T) {
	t.Parallel()

	if IsURLFilteredNetworker(NewReadFile(t.TempDir())) {
		t.Error("IsURLFilteredNetworker(read_file) = true, want false (a non-funnel tool carries no marker)")
	}
}

// TestDefaultTools_EveryNetworkToolIsURLFiltered is the "impossible to forget" property for
// Apogee's OWN tools, and the reason the funnel exists: it walks the default tool set and
// requires every tool that declares domain.EffectNetwork to carry the url-filter marker. A
// future built-in network tool that hand-rolls its own http.Client (skipping networkTool.do,
// and with it the URLGuard) fails HERE rather than silently auto-running unattended in Auto,
// which is what dispatch does for the vouched-for class. It also fails if a marker assertion
// in network.go is deleted while the tool keeps embedding the funnel — the assertion block is
// documentation, this walk is the guarantee.
func TestDefaultTools_EveryNetworkToolIsURLFiltered(t *testing.T) {
	t.Parallel()

	network := 0
	for _, tool := range DefaultToolsWithHost(t.TempDir(), HostTools{}) {
		ext, ok := tool.(domain.ExternalEffectTool)
		if !ok || ext.ExternalEffect() != domain.EffectNetwork {
			continue
		}
		network++
		if !IsURLFilteredNetworker(tool) {
			t.Errorf("%q declares EffectNetwork but carries no url-filter marker: it must reach the network through the funnel (embed networkTool and call do)", tool.Name())
		}
	}
	if network == 0 {
		t.Fatal("no EffectNetwork tool in the default set — the walk would prove nothing; check DefaultToolsWithHost")
	}
}

// TestNetworkFunnel_DoSuccess covers the happy path: the funnel sends the caller's method,
// body and headers, and returns the wire facts (status, status code, header, body) with no
// failure message and no Go error.
func TestNetworkFunnel_DoSuccess(t *testing.T) {
	t.Parallel()

	var gotMethod, gotHeader, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotHeader = r.Header.Get("X-Probe")
		b := make([]byte, 4)
		n, _ := r.Body.Read(b)
		gotBody = string(b[:n])
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("hello funnel"))
	}))
	defer srv.Close()

	resp, msg, err := newFunnelTool(loopbackGuard()).do(context.Background(), netRequest{
		url:    srv.URL,
		method: http.MethodPost,
		body:   strings.NewReader("ping"),
		header: http.Header{"X-Probe": {"yes"}},
	})
	if err != nil {
		t.Fatalf("do Go error: %v", err)
	}
	if msg != "" {
		t.Fatalf("do failure message on a good request: %q", msg)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("server saw method %q, want POST", gotMethod)
	}
	if gotHeader != "yes" {
		t.Errorf("server saw X-Probe %q, want yes (the funnel must forward caller headers)", gotHeader)
	}
	if gotBody != "ping" {
		t.Errorf("server saw body %q, want ping", gotBody)
	}
	if resp.statusCode != http.StatusOK || !strings.Contains(resp.status, "200") {
		t.Errorf("resp status = %q/%d, want 200", resp.status, resp.statusCode)
	}
	if ct := resp.header.Get("Content-Type"); ct != "text/plain" {
		t.Errorf("resp Content-Type = %q, want text/plain", ct)
	}
	if resp.body != "hello funnel" {
		t.Errorf("resp body = %q, want %q", resp.body, "hello funnel")
	}
	if resp.truncated {
		t.Error("a short body must not report truncated")
	}
}

// TestNetworkFunnel_DoCapsBody proves the response cap rides with the funnel: a body over
// maxNetworkResponseBytes comes back cut to the cap and flagged truncated, so one call
// cannot flood the model's context.
func TestNetworkFunnel_DoCapsBody(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(make([]byte, maxNetworkResponseBytes+1024))
	}))
	defer srv.Close()

	resp, msg, err := newFunnelTool(loopbackGuard()).do(context.Background(), netRequest{url: srv.URL})
	if err != nil || msg != "" {
		t.Fatalf("do failed: msg=%q err=%v", msg, err)
	}
	if !resp.truncated {
		t.Error("an over-cap body must report truncated")
	}
	if len(resp.body) != maxNetworkResponseBytes {
		t.Errorf("body length = %d, want the cap %d", len(resp.body), maxNetworkResponseBytes)
	}
}

// TestNetworkFunnel_DoReleasesTheConnectionItOpened pins the funnel's resource footprint (M-4):
// the client is built per call, so its pooled connection — and the readLoop/writeLoop goroutines
// that pin the whole transport with it — used to stay alive for IdleConnTimeout (30s) after do
// returned. Network tools auto-run unattended in Auto, so dozens of calls in a Turn is the normal
// case: the process accumulated an open socket plus two goroutines per call.
//
// A runtime.NumGoroutine() delta is flaky by nature, so the assertion is structural and made
// where the leak is actually observable — the SERVER's connection bookkeeping. After N sequential
// calls every connection the server saw must be closed; the socket is the thing that leaked, and
// its two pump goroutines exit with it.
func TestNetworkFunnel_DoReleasesTheConnectionItOpened(t *testing.T) {
	t.Parallel()

	const calls = 3

	var opened, closed atomic.Int64
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("hello funnel"))
	}))
	srv.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		switch state {
		case http.StateNew:
			opened.Add(1)
		case http.StateClosed, http.StateHijacked:
			closed.Add(1)
		}
	}
	srv.Start()
	defer srv.Close()

	tool := newFunnelTool(loopbackGuard())
	for i := range calls {
		resp, msg, err := tool.do(context.Background(), netRequest{url: srv.URL})
		if err != nil || msg != "" {
			t.Fatalf("call %d: do failed: msg=%q err=%v", i+1, msg, err)
		}
		if resp.body != "hello funnel" {
			t.Fatalf("call %d: resp body = %q, want %q — the release must not cost the response", i+1, resp.body, "hello funnel")
		}
	}

	// The close travels over the wire, so the server observes it shortly AFTER do returns —
	// wait for the count rather than sleeping a fixed span. A leaked connection sits in its
	// pool for IdleConnTimeout, far past this deadline, so the wait costs the green path
	// milliseconds and never rescues the red one.
	for deadline := time.Now().Add(5 * time.Second); closed.Load() < opened.Load() && time.Now().Before(deadline); {
		time.Sleep(5 * time.Millisecond)
	}
	if got := opened.Load(); got < calls {
		t.Fatalf("the server saw %d connection(s) for %d calls: the test needs at least one per call to prove anything", got, calls)
	}
	if got, want := closed.Load(), opened.Load(); got != want {
		t.Errorf("%d of %d connection(s) released after the calls returned: a per-call transport that is never drained leaves an open socket and its two pump goroutines behind for IdleConnTimeout", got, want)
	}
}

// TestNetworkFunnel_DoCutShortBodyIsNeverASilentSuccess covers the wrong-answer class the body
// read used to hide: the read error was discarded and `truncated` is set by the 2 MiB cap
// ALONE, so a response cut short mid-stream came back as a plain 200 carrying the first chunk
// with nothing to say it was incomplete — the model then reasons over a partial page as if it
// were the whole one. Both triggers must surface a failure the model can see; neither is ctx
// cancellation (the caller's ctx is alive), so neither is a Go error.
func TestNetworkFunnel_DoCutShortBodyIsNeverASilentSuccess(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		handler http.HandlerFunc
		timeout time.Duration
	}{
		{
			// The connection dies mid-body: the response promises more than it delivers, so
			// the client's body read fails with an unexpected EOF after the first chunk.
			name: "connection dies mid-body",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				writePartialBody(w)
				conn, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					return // a short write against the promised length aborts it anyway
				}
				_ = conn.Close()
			},
		},
		{
			// The server streams past the resolved timeout: http.Client.Timeout covers the
			// BODY read too, so the request "succeeds" and the body ends mid-stream.
			name: "server streams past the resolved timeout",
			handler: func(w http.ResponseWriter, r *http.Request) {
				writePartialBody(w)
				<-r.Context().Done() // never finish the body; the client's own budget ends this
			},
			timeout: 250 * time.Millisecond,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			resp, msg, err := newFunnelTool(loopbackGuard()).do(context.Background(), netRequest{
				url:     srv.URL + "/page?key=" + secretKey,
				timeout: tc.timeout,
			})
			if err != nil {
				t.Fatalf("a cut-short body is not caller cancellation, so it must not be a Go error: %v", err)
			}
			if msg == "" {
				t.Fatalf("a body cut short mid-read read as a complete response: no failure message, resp = %+v", resp)
			}
			if !strings.Contains(msg, "cut short") {
				t.Errorf("message should say the response was cut short: %q", msg)
			}
			if !strings.Contains(msg, "127.0.0.1") {
				t.Errorf("message should name the bare host for diagnosability: %q", msg)
			}
			if strings.Contains(msg, secretKey) {
				t.Fatalf("API key LEAKED into the cut-short message: %q", msg)
			}
			if resp.statusCode != 0 || resp.body != "" || resp.truncated {
				t.Errorf("a cut-short read must yield the zero netResponse, never a silent partial; got %+v", resp)
			}
		})
	}
}

// writePartialBody puts a response on the wire that promises far more body than it sends, so
// whatever ends the connection next leaves the client's body read short.
func writePartialBody(w http.ResponseWriter) {
	w.Header().Set("Content-Length", "4096")
	_, _ = w.Write([]byte("first chunk"))
	w.(http.Flusher).Flush()
}

// TestNetworkFunnel_DoBlockedURL proves url-safety is applied BY THE FUNNEL: with the
// default (floor-on) guard a loopback URL never reaches the network, and the caller gets a
// ready-to-surface message, an empty netResponse and a nil Go error (ADR 0007).
func TestNetworkFunnel_DoBlockedURL(t *testing.T) {
	t.Parallel()

	resp, msg, err := newFunnelTool(security.URLGuard{}).do(context.Background(), netRequest{
		url: "http://127.0.0.1:1/x",
	})
	if err != nil {
		t.Fatalf("a blocked URL must not be a Go error: %v", err)
	}
	if !strings.Contains(msg, "url-safety") {
		t.Errorf("blocked message should name url-safety: %q", msg)
	}
	if got := strings.Count(msg, "url blocked by url-safety"); got != 1 {
		t.Errorf("blocked message states the block %d times, want exactly 1: %q", got, msg)
	}
	if !strings.Contains(msg, "127.0.0.1") {
		t.Errorf("blocked message should name the bare host for diagnosability: %q", msg)
	}
	if resp.status != "" || resp.statusCode != 0 || resp.body != "" || resp.header != nil {
		t.Errorf("a blocked call must yield the zero netResponse; got %+v", resp)
	}
}

// TestNetworkFunnel_DialTimeFloorBlocksAfterPreflightPasses drives the half of url-safety the
// rest of the suite never exercises: a URL that PASSES the pre-flight Check and is stopped at
// CONNECT. Every other funnel test either turns the floor off (loopbackGuard) or is refused
// pre-flight, so the dial-time Control hook installed at newHTTPClient — the DNS-rebinding
// backstop, and the floor's real bound (security/ssrf.go: "the pre-flight Check is the cheap
// first line; the dial-time control is the real bound") — was carried by nothing. A refactor
// that dropped the hook, swapped in a shared client or wrapped the transport left the whole
// suite green while a prompt-injected model regained an IMDS/loopback path in Auto.
//
// The rebinding is simulated without a rebinding nameserver: the guard's injected resolver
// answers PUBLIC for the pre-flight, while the transport resolves the same name for real and
// connects to loopback — precisely the check-time/connect-time split the hook exists to close.
// The load-bearing assertion is that the handler is NEVER reached: an error message alone would
// still pass if the connection had been made and then judged.
func TestNetworkFunnel_DialTimeFloorBlocksAfterPreflightPasses(t *testing.T) {
	t.Parallel()

	var reached atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached.Add(1)
		_, _ = w.Write([]byte("the private page"))
	}))
	defer srv.Close()

	// The floor stays ON (the zero guard); only host resolution is injected, and it lies the way
	// a rebinding name does — one public answer at check time.
	guard := security.URLGuard{}.WithResolver(func(context.Context, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	})
	// The server binds 127.0.0.1; addressing it by NAME is what gets past the pre-flight, since
	// an IP-literal host is classified directly and never reaches the injected resolver.
	reqURL := "http://localhost:" + serverPort(t, srv) + "/page?key=" + secretKey

	if err := guard.CheckContext(context.Background(), reqURL); err != nil {
		t.Fatalf("the pre-flight must PASS or this test proves nothing about the DIAL-time floor: %v", err)
	}

	res, err := NewWebFetch(guard).Execute(context.Background(), domain.ToolCall{
		ID: "c1", Tool: "web_fetch", Arguments: jsonArgs(t, map[string]any{"url": reqURL}),
	})
	if err != nil {
		t.Fatalf("a dial-time block is not caller cancellation, so it must not be a Go error: %v", err)
	}
	if got := reached.Load(); got != 0 {
		t.Errorf("the handler was reached %d time(s): a connection to the private address must never be MADE, whatever the result then says", got)
	}
	if !res.IsError {
		t.Fatalf("a name that resolves public at check time and loopback at connect time must be refused at the dial: %q", res.Content)
	}
	if !strings.Contains(res.Content, "SSRF floor") {
		t.Errorf("the refusal must come from the SSRF floor, not from an incidental transport failure: %q", res.Content)
	}
	if got := strings.Count(res.Content, "url blocked by url-safety"); got != 1 {
		t.Errorf("blocked result states the block %d times, want exactly 1: %q", got, res.Content)
	}
	if got := strings.Count(res.Content, "localhost"); got != 1 {
		t.Errorf("blocked result names the host %d times, want exactly 1: %q", got, res.Content)
	}
	if strings.Contains(res.Content, secretKey) || strings.Contains(res.Content, reqURL) {
		t.Fatalf("the request URL rode out to the model on the dial-time path (M2): %q", res.Content)
	}
}

// serverPort is srv's listening port, the piece needed to re-address a httptest server by NAME
// rather than by the 127.0.0.1 literal it reports.
func serverPort(t *testing.T, srv *httptest.Server) string {
	t.Helper()

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse test server URL %q: %v", srv.URL, err)
	}
	return u.Port()
}

// TestNetworkFunnel_DoDialsTheURLTheGuardChecked is the funnel half of the normalisation
// group (M-1): the string the guard judged and the string the transport dials must be the
// same one. The URL below is padded with whitespace, spells its host in upper case and ends
// it with the DNS root dot — three spellings that used to diverge (the guard parsed the
// TRIMMED url while the request was built from the padded one, which then failed to parse at
// all). The proof is the server's OBSERVED Host header, not the funnel's internals.
func TestNetworkFunnel_DoDialsTheURLTheGuardChecked(t *testing.T) {
	t.Parallel()

	var gotHost string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.Host
		_, _ = w.Write([]byte("normalised"))
	}))
	defer srv.Close()

	// The server binds 127.0.0.1; addressing it by NAME is what makes the host spelling
	// (case, root dot) observable — an IP literal has none of those forms.
	port := serverPort(t, srv)
	resp, msg, err := newFunnelTool(loopbackGuard()).do(context.Background(), netRequest{
		url: "  http://LOCALHOST.:" + port + "/page  ",
	})
	if err != nil {
		t.Fatalf("do Go error: %v", err)
	}
	if msg != "" {
		t.Fatalf("a URL the guard passed must also BUILD: %q", msg)
	}
	if want := "localhost:" + port; gotHost != want {
		t.Errorf("server saw Host %q, want %q — the transport must dial the normalised host the guard checked", gotHost, want)
	}
	if resp.body != "normalised" {
		t.Errorf("resp body = %q, want %q", resp.body, "normalised")
	}
}

// TestNetworkFunnel_DoTrailingDotDoesNotEscapeTheDenyList is the exposure the normalisation
// closes, driven end to end: "localhost." resolves exactly as "localhost" does and virtually
// every virtual-host server accepts it, but it matches no DenyHosts entry spelled without the
// dot — so one appended character used to turn a denied host into a reachable one. The
// load-bearing assertion is that the handler is NEVER reached.
func TestNetworkFunnel_DoTrailingDotDoesNotEscapeTheDenyList(t *testing.T) {
	t.Parallel()

	var reached atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached.Add(1)
		_, _ = w.Write([]byte("the denied page"))
	}))
	defer srv.Close()

	guard := security.URLGuard{DenyHosts: []string{"localhost"}}.DisableIPFloor()
	resp, msg, err := newFunnelTool(guard).do(context.Background(), netRequest{
		url: "http://localhost.:" + serverPort(t, srv) + "/page",
	})
	if err != nil {
		t.Fatalf("a blocked URL must not be a Go error: %v", err)
	}
	if got := reached.Load(); got != 0 {
		t.Errorf("the handler was reached %d time(s): a denied host must never be CONNECTED to, however it is spelled", got)
	}
	if !strings.Contains(msg, "url-safety") || !strings.Contains(msg, "denied") {
		t.Errorf("the refusal must come from the deny list, not from an incidental failure: %q", msg)
	}
	if resp.statusCode != 0 || resp.body != "" {
		t.Errorf("a blocked call must yield the zero netResponse; got %+v", resp)
	}
}

// TestBlockedMessage_StatesTheBlockOnce pins the wording of a url-safety block: the message
// states "url blocked by url-safety" EXACTLY once. Every guard error already carries the
// security.ErrURLBlocked sentinel text ("security: url blocked by url-safety") in its own
// string, so interpolating it raw behind the mandated prefix made the model-facing message read
// "url blocked by url-safety (host 127.0.0.1): security: url blocked by url-safety: …" — the
// same phrase twice. Only the guard's REASON belongs after the prefix; the host must still be
// named and the key-bearing URL must still never appear (M2).
func TestBlockedMessage_StatesTheBlockOnce(t *testing.T) {
	t.Parallel()

	const rawURL = "http://127.0.0.1:9/search?key=" + secretKey
	preflight := security.URLGuard{}.CheckContext(context.Background(), rawURL)
	if preflight == nil {
		t.Fatal("the floor-on guard must reject a loopback URL — this test needs its error")
	}

	cases := []struct {
		name string
		err  error
	}{
		{"pre-flight check", preflight},
		// A dial-time floor block reaches blockedMessage wrapped in the transport's *url.Error,
		// so the sentinel sits MID-string there rather than at the front.
		{"dial-time floor", &url.Error{
			Op:  "Get",
			URL: rawURL,
			Err: fmt.Errorf("dial tcp 127.0.0.1:9: %w", security.ErrSSRFBlocked),
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			msg := blockedMessage("127.0.0.1", tc.err, rawURL)
			if got := strings.Count(msg, "url blocked by url-safety"); got != 1 {
				t.Errorf("message states the block %d times, want exactly 1: %q", got, msg)
			}
			if sentinel := security.ErrURLBlocked.Error(); strings.Contains(msg, sentinel) {
				t.Errorf("the guard's own %q text must not ride along behind the prefix: %q", sentinel, msg)
			}
			if !strings.Contains(msg, "127.0.0.1") {
				t.Errorf("message must still name the bare host: %q", msg)
			}
			if strings.Contains(msg, secretKey) {
				t.Fatalf("API key LEAKED into the block message: %q", msg)
			}
		})
	}
}

// TestNetworkFunnel_DoBlockedURLDoesNotLeakKey is the M2 generalization for the block path:
// the protection that used to be web_search's private discipline now belongs to every tool
// routed through the funnel — a key-bearing URL is named by host only.
//
// The second row is M-2. A URL carrying an INTERIOR ASCII control character (TrimSpace strips
// only the ends) does not parse, and url-safety's reason used to interpolate the parse error —
// a *url.Error whose text embeds the URL under a %q verb. %q ESCAPES the control byte, so the
// funnel's redaction searched the message for a raw byte sequence that was no longer in it and
// the key rode out to the model intact. Every row therefore asserts the key and the URL are
// absent in the escaped spelling as well as the raw one.
//
// The third row is item 18 of the 2026-07-26 plan: do scrubs its messages against the
// NORMALISED url — the string the request is built from — so an error whose text embeds that
// form (rather than the raw spelling the model used) is redacted too. Latent today: every
// shipped error that embeds a URL is a *url.Error, whose URL is dropped wholesale before
// redaction. The row is a guardrail against the divergence becoming reachable, not a
// regression proof.
func TestNetworkFunnel_DoBlockedURLDoesNotLeakKey(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		url  string
		// host is the bare host the message must still name for diagnosability; "" when the
		// URL does not parse and there is no host to name.
		host string
		// normalized is the post-NormalizeURL spelling when it differs from url — the form
		// the request carries, which the message must not leak either. "" ⇒ same as url.
		normalized string
		// guard drives the row's failure; the zero value is the floor-on guard the plain
		// rows need.
		guard security.URLGuard
	}{
		{
			name: "blocked by the SSRF floor",
			url:  "http://127.0.0.1:9/search?key=" + secretKey,
			host: "127.0.0.1",
		},
		{
			name: "unparseable through an interior control character",
			url:  "http://example.com/search?key=" + secretKey + "\x01x",
		},
		{
			// Item 18's guardrail: the raw spelling diverges from the normalised one
			// (upper-case host, trailing root dot), and the failure is a NON-*url.Error
			// whose own text embeds the NORMALISED url — the form the request carries, and
			// a form a scrub keyed on the raw string never matches. The injected resolver
			// simulates such a nested error; no shipped error shape produces one today, so
			// this row pins a latent path, it does not reproduce a live leak.
			name:       "non-url.Error embedding the normalised form",
			url:        "http://EXAMPLE.COM./search?key=" + secretKey,
			host:       "EXAMPLE.COM.",
			normalized: "http://example.com/search?key=" + secretKey,
			guard: security.URLGuard{}.WithResolver(func(context.Context, string) ([]net.IP, error) {
				return nil, errors.New("upstream refused http://example.com/search?key=" + secretKey)
			}),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, msg, err := newFunnelTool(tc.guard).do(context.Background(), netRequest{url: tc.url})
			if err != nil {
				t.Fatalf("unexpected Go error: %v", err)
			}
			if msg == "" {
				t.Fatal("a blocked URL must produce a failure message")
			}
			assertNoKeyInAnyForm(t, msg, tc.url)
			if tc.normalized != "" {
				assertNoKeyInAnyForm(t, msg, tc.normalized)
			}
			if tc.host != "" && !strings.Contains(msg, tc.host) {
				t.Errorf("block message should name the bare host %q: %q", tc.host, msg)
			}
		})
	}
}

// assertNoKeyInAnyForm fails when msg carries the API key or the request URL, in either its raw
// form or the form a %q verb writes it as. Checking only the raw form is what M-2 slipped
// through: the leaking message contained the URL, but spelled with the control byte escaped.
func assertNoKeyInAnyForm(t *testing.T, msg, rawURL string) {
	t.Helper()
	for _, secret := range []string{secretKey, rawURL} {
		if strings.Contains(msg, secret) {
			t.Fatalf("%q LEAKED raw into the message: %q", secret, msg)
		}
		quoted := strconv.Quote(secret)
		if strings.Contains(msg, quoted[1:len(quoted)-1]) {
			t.Fatalf("%q LEAKED in its %%q-escaped form into the message: %q", secret, msg)
		}
	}
}

// TestRedactSubstring_StripsTheQuotedFormToo pins the defence-in-depth half of M-2 at its own
// seam: a plain substring search is defeated by any formatter that ESCAPES what it prints, and
// Go's *url.Error embeds the request URL under %q. redactSubstring must therefore strip the
// quoted spelling as well as the raw one — the surrounding quotes are the formatter's, not the
// secret's, so they stay.
func TestRedactSubstring_StripsTheQuotedFormToo(t *testing.T) {
	t.Parallel()

	secret := "http://example.com/?key=" + secretKey + "\x01x"

	cases := []struct {
		name   string
		in     string
		secret string
		want   string
	}{
		{
			name:   "the raw form is stripped",
			in:     "cause: " + secret,
			secret: secret,
			want:   "cause: [redacted-url]",
		},
		{
			name:   "the %q-escaped form is stripped",
			in:     fmt.Sprintf("parse %q: net/url: invalid control character in URL", secret),
			secret: secret,
			want:   `parse "[redacted-url]": net/url: invalid control character in URL`,
		},
		{
			name:   "a secret needing no escaping is unaffected by the second pass",
			in:     "Get \"http://example.com/?key=" + secretKey + "\": dial failed",
			secret: "http://example.com/?key=" + secretKey,
			want:   `Get "[redacted-url]": dial failed`,
		},
		{
			name:   "an empty secret redacts nothing",
			in:     "no url here",
			secret: "",
			want:   "no url here",
		},
		{
			name:   "an absent secret leaves the text alone",
			in:     "no url here",
			secret: secret,
			want:   "no url here",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := redactSubstring(tc.in, tc.secret); got != tc.want {
				t.Errorf("redactSubstring() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestNetworkFunnel_DoTransportErrorDoesNotLeakKey is the M2 generalization for the
// transport path: Go's *url.Error stringifies with the FULL request URL, so the funnel must
// scrub it — host yes, key never.
func TestNetworkFunnel_DoTransportErrorDoesNotLeakKey(t *testing.T) {
	t.Parallel()

	// A reachable server closed immediately, so client.Do fails with a *url.Error carrying
	// the full request URL (host + query + key).
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	reqURL := srv.URL + "/search?key=" + secretKey
	srv.Close()

	_, msg, err := newFunnelTool(loopbackGuard()).do(context.Background(), netRequest{url: reqURL})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if msg == "" {
		t.Fatal("a refused connection must produce a failure message")
	}
	if strings.Contains(msg, secretKey) {
		t.Fatalf("API key LEAKED into the funnel's transport message: %q", msg)
	}
	if !strings.Contains(msg, "127.0.0.1") {
		t.Errorf("transport message should name the bare host: %q", msg)
	}
}

// TestNetworkFunnel_DoUsesSafeLabel proves the caller-supplied host-only label is what the
// failure message names — the seam web_search needs so its messages keep naming the
// CONFIGURED endpoint's host rather than anything derived from the key-bearing request URL.
func TestNetworkFunnel_DoUsesSafeLabel(t *testing.T) {
	t.Parallel()

	_, msg, err := newFunnelTool(security.URLGuard{}).do(context.Background(), netRequest{
		url:       "http://127.0.0.1:9/search?key=" + secretKey,
		safeLabel: "search.example.com",
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !strings.Contains(msg, "search.example.com") {
		t.Errorf("message should name the supplied safeLabel: %q", msg)
	}
	if strings.Contains(msg, secretKey) {
		t.Fatalf("API key LEAKED: %q", msg)
	}
}

// TestNetworkFunnel_DoCancelledCtxIsGoError proves ADR 0007 holds at the funnel: ctx
// cancellation is the ONLY thing do reports as a Go error — before the request, during the
// PRE-FLIGHT's resolve, while the request is in flight, and while the BODY streams — never as
// a model-facing message. Two of those used to escape: the body read's error was discarded, so
// a cancelled caller got a nil error over a half-read body and the Turn was never rolled back;
// and the pre-flight now runs on a ctx DERIVED from the request budget (M-5), so a cancellation
// arriving during the resolve reaches the guard as an unresolvable host and would otherwise
// come back as a blocked-URL message.
func TestNetworkFunnel_DoCancelledCtxIsGoError(t *testing.T) {
	t.Parallel()

	t.Run("cancelled before the request", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, msg, err := newFunnelTool(loopbackGuard()).do(ctx, netRequest{url: "https://example.com"})
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled", err)
		}
		if msg != "" {
			t.Errorf("cancellation must not be a model-facing message; got %q", msg)
		}
	})

	t.Run("cancelled in flight", func(t *testing.T) {
		t.Parallel()
		started := make(chan struct{})
		srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			close(started)
			<-r.Context().Done() // hold the response open until the client goes away
		}))
		defer srv.Close()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() {
			<-started
			cancel()
		}()

		_, msg, err := newFunnelTool(loopbackGuard()).do(ctx, netRequest{url: srv.URL})
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled", err)
		}
		if msg != "" {
			t.Errorf("cancellation must not be a model-facing message; got %q", msg)
		}
	})

	t.Run("cancelled during the pre-flight resolve", func(t *testing.T) {
		t.Parallel()
		resolving := make(chan struct{})
		guard := security.URLGuard{}.WithResolver(blackHoleResolver(resolving))

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() {
			<-resolving // the lookup is under way, so the cancellation lands inside the check
			cancel()
		}()

		_, msg, err := newFunnelTool(guard).do(ctx, netRequest{url: "http://black-hole.example/"})
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled — the pre-flight's ctx is derived from the caller's, so its cancellation must stay ADR 0007's Go error and not read as a blocked URL", err)
		}
		if msg != "" {
			t.Errorf("cancellation must not be a model-facing message; got %q", msg)
		}
	})

	t.Run("cancelled while the body streams", func(t *testing.T) {
		t.Parallel()
		streaming := make(chan struct{})
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writePartialBody(w) // headers and a first chunk out, the rest never sent
			close(streaming)
			<-r.Context().Done()
		}))
		defer srv.Close()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() {
			<-streaming
			// The headers are on the wire, so do is already past client.Do; the short pause
			// lets it reach the body read, which is the path this subtest exists for. An
			// early cancel would merely re-cover the in-flight case above — the assertion
			// holds for both interleavings, so the test cannot flake either way.
			time.Sleep(50 * time.Millisecond)
			cancel()
		}()

		_, msg, err := newFunnelTool(loopbackGuard()).do(ctx, netRequest{url: srv.URL})
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled — a body cut short by the CALLER's cancellation is ADR 0007's Go-error shape, so the Turn rolls back", err)
		}
		if msg != "" {
			t.Errorf("cancellation must not be a model-facing message; got %q", msg)
		}
	})
}

// blackHoleResolver is a host→IP resolver that never answers — the hermetic stand-in for a name
// delegated to a black-holing nameserver, where the ONLY thing that can end the lookup is the ctx
// it was handed. It closes started (when non-nil) as the lookup begins, for a test that must act
// while the pre-flight is inside it.
func blackHoleResolver(started chan struct{}) func(context.Context, string) ([]net.IP, error) {
	return func(ctx context.Context, _ string) ([]net.IP, error) {
		if started != nil {
			close(started)
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}
}

// TestNetworkFunnel_TimeoutResolution pins the timeout contract the funnel applies to
// netRequest.timeout: unset (≤ 0) resolves to the default, an over-ceiling request is
// clamped DOWN (never raised), the seconds-typed clampTimeout resolves identically —
// one ceiling, both entry points — and the resolved budget covers the PRE-FLIGHT's DNS
// resolution, not just the HTTP exchange. The first three are asserted on the resolution
// rather than on wall-clock timing; the fourth is asserted against the budget itself.
// That the budget is SHARED with the request rather than re-issued to it is the separate
// claim TestNetworkFunnel_OneBudgetCoversResolveAndRequest pins.
func TestNetworkFunnel_TimeoutResolution(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   time.Duration
		want time.Duration
	}{
		{"unset uses the default", 0, defaultNetworkTimeout},
		{"negative uses the default", -5 * time.Second, defaultNetworkTimeout},
		{"in-range is honoured", 5 * time.Second, 5 * time.Second},
		{"over-ceiling clamps down", maxNetworkTimeout + time.Minute, maxNetworkTimeout},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := clampDuration(tc.in); got != tc.want {
				t.Errorf("clampDuration(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}

	// The seconds-typed entry point (http_request's timeout_seconds) must agree.
	for _, seconds := range []int{0, -3, 5, 999} {
		if got, want := clampTimeout(seconds), clampDuration(time.Duration(seconds)*time.Second); got != want {
			t.Errorf("clampTimeout(%d) = %v, want %v (same ceiling on both paths)", seconds, got, want)
		}
	}

	// The budget is not the HTTP exchange's alone (M-5): the pre-flight's DNS resolution used to
	// run on the RAW caller ctx, so the SSRF floor's lookup was bounded only by the system
	// resolver's retry schedule — `options timeout:30 attempts:5` in /etc/resolv.conf makes a
	// black-holed name block for MINUTES — on top of the HTTP timeout, and http_request's
	// `timeout_seconds: 1` did not bound it at all. Dispatch adds no per-tool deadline, so
	// nothing else would have. The assertion is against the budget, not a wall-clock sleep.
	t.Run("the pre-flight resolve runs under the resolved budget", func(t *testing.T) {
		t.Parallel()

		const budget = 200 * time.Millisecond
		guard := security.URLGuard{}.WithResolver(blackHoleResolver(nil))

		// A watchdog rather than a deadline on the caller's own ctx: the caller's ctx must still
		// be ALIVE when do returns (half of what is asserted below), and an unbounded pre-flight
		// has to fail this case rather than hang it.
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		watchdog := time.AfterFunc(40*budget, cancel)
		defer watchdog.Stop()

		start := time.Now()
		_, msg, err := newFunnelTool(guard).do(ctx, netRequest{url: "http://black-hole.example/", timeout: budget})
		elapsed := time.Since(start)

		if err != nil {
			t.Fatalf("err = %v, want nil — a budget spent inside the pre-flight is the funnel's OWN bound, so it is the message shape; only the caller's cancellation is a Go error (ADR 0007)", err)
		}
		if !strings.Contains(msg, "url blocked by url-safety") || !strings.Contains(msg, context.DeadlineExceeded.Error()) {
			t.Errorf("msg = %q, want a url-safety message naming %q", msg, context.DeadlineExceeded)
		}
		if elapsed > 20*budget {
			t.Errorf("do took %v for a %v budget — the pre-flight resolve is outside the budget", elapsed, budget)
		}
		if ctx.Err() != nil {
			t.Errorf("the CALLER's ctx ended (%v) — the budget must ride a ctx derived from it, never the caller's own", ctx.Err())
		}
	})
}

// TestNetworkFunnel_OneBudgetCoversResolveAndRequest pins the SHARED deadline (M-5): the
// resolved timeout is ONE budget spent BETWEEN resolve, dial and body, not a fresh copy issued
// to each phase. A per-phase form — a derived ctx for the pre-flight and a fresh client Timeout
// for the request — bounds every phase and still lets a call cost the SUM of them, so a one
// second ask took ~1.7 s with a 0.7 s lookup and maxNetworkTimeout was no ceiling on a CALL.
// The bound is therefore wall-clock, and it is the whole point of the item: a request that asks
// for one second takes about one second whatever the endpoint's DNS does.
//
// Both slow halves are hermetic and reach no network. The pre-flight is slowed by the guard's
// injected resolver, which eats most of the budget and then answers PUBLIC so the check PASSES
// (a failing check would never reach the request, and would prove nothing). The dial is hung at
// the transport's own name resolution — the one seam the funnel does not inject — by pointing
// the Go resolver's DNS dialer at a socket that never answers.
//
// That override is process-global, which is why this test is deliberately NOT parallel: Go runs
// the serial tests to completion before it resumes any parallel one, so nothing else is in
// flight while it is installed. Nothing else in this package resolves a name through DNS in any
// case — the funnel tests use httptest's 127.0.0.1 literals, "localhost" (answered from
// /etc/hosts by the files-first lookup order), or an injected guard resolver.
func TestNetworkFunnel_OneBudgetCoversResolveAndRequest(t *testing.T) {
	const (
		budget = 600 * time.Millisecond
		lookup = 500 * time.Millisecond // most of the budget, spent before the request starts
	)

	restore := net.DefaultResolver
	net.DefaultResolver = &net.Resolver{
		PreferGo: true, // Dial is the GO resolver's seam; a cgo resolver would ignore it
		Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			<-ctx.Done() // the transport's lookup never answers: the hanging dial
			return nil, ctx.Err()
		},
	}
	t.Cleanup(func() { net.DefaultResolver = restore })

	guard := security.URLGuard{}.WithResolver(func(ctx context.Context, _ string) ([]net.IP, error) {
		select {
		case <-time.After(lookup):
			return []net.IP{net.ParseIP("93.184.216.34")}, nil // public: the floor passes
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})

	start := time.Now()
	resp, msg, err := newFunnelTool(guard).do(context.Background(), netRequest{
		url:     "http://slow-dns.example/page",
		timeout: budget,
	})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("err = %v, want nil — a spent budget is the funnel's OWN bound, so it is the message shape; only caller cancellation is a Go error (ADR 0007)", err)
	}
	if msg == "" {
		t.Fatalf("a call that never reached a server must report a failure; got resp %+v", resp)
	}
	if strings.Contains(msg, "url blocked by url-safety") {
		t.Fatalf("the pre-flight must PASS or this test proves nothing about the REQUEST's deadline: %q", msg)
	}
	if elapsed < lookup {
		t.Fatalf("do returned in %v, before the %v lookup could have finished — the test is not exercising the pre-flight it claims to", elapsed, lookup)
	}
	if slack := 250 * time.Millisecond; elapsed > budget+slack {
		t.Errorf("do took %v for a %v budget with a %v lookup: the request was given a budget of its OWN instead of sharing the pre-flight's (per-phase worst case %v)", elapsed, budget, lookup, lookup+budget)
	}
	t.Logf("slow lookup %v + hanging dial under a %v budget: do returned in %v", lookup, budget, elapsed)
}
