package tools

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
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
	if !strings.Contains(msg, "127.0.0.1") {
		t.Errorf("blocked message should name the bare host for diagnosability: %q", msg)
	}
	if resp.status != "" || resp.statusCode != 0 || resp.body != "" || resp.header != nil {
		t.Errorf("a blocked call must yield the zero netResponse; got %+v", resp)
	}
}

// TestNetworkFunnel_DoBlockedURLDoesNotLeakKey is the M2 generalization for the block path:
// the protection that used to be web_search's private discipline now belongs to every tool
// routed through the funnel — a key-bearing URL is named by host only.
func TestNetworkFunnel_DoBlockedURLDoesNotLeakKey(t *testing.T) {
	t.Parallel()

	_, msg, err := newFunnelTool(security.URLGuard{}).do(context.Background(), netRequest{
		url: "http://127.0.0.1:9/search?key=" + secretKey,
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if msg == "" {
		t.Fatal("a floor-blocked URL must produce a failure message")
	}
	if strings.Contains(msg, secretKey) {
		t.Fatalf("API key LEAKED into the funnel's block message: %q", msg)
	}
	if !strings.Contains(msg, "127.0.0.1") {
		t.Errorf("block message should name the bare host: %q", msg)
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
// cancellation is the ONLY thing do reports as a Go error — before the request and while it
// is in flight — never as a model-facing message.
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
}

// TestNetworkFunnel_TimeoutResolution pins the timeout contract the funnel applies to
// netRequest.timeout: unset (≤ 0) resolves to the default, an over-ceiling request is
// clamped DOWN (never raised), and the seconds-typed clampTimeout resolves identically —
// one ceiling, both entry points. Asserted on the resolution, not on wall-clock timing.
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
}
