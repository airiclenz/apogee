package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/security"
)

// secretKey is the API-key value a configured search endpoint carries in its query — the
// value the M2 fix must keep out of every model-facing string.
const secretKey = "SUPER-SECRET-API-KEY-1234"

// TestWebSearch_TransportErrorDoesNotLeakAPIKey is the M2 regression: when the request to
// a key-bearing endpoint fails at the transport, the surfaced error must NOT contain the
// API key (which rides in the request URL a *url.Error embeds) — only the bare endpoint
// host. Before the fix the error was "...: "+err.Error(), echoing the full request URL.
func TestWebSearch_TransportErrorDoesNotLeakAPIKey(t *testing.T) {
	t.Parallel()

	// A reachable server that we close immediately, so client.Do fails with a *url.Error
	// embedding the full request URL (host + query + key).
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	endpoint := srv.URL + "/search?key=" + secretKey
	srv.Close() // force a connection-refused transport error

	res, err := NewWebSearch(loopbackGuard(), endpoint).Execute(context.Background(), domain.ToolCall{
		ID: "c1", Tool: "web_search", Arguments: jsonArgs(t, map[string]any{"query": "needle"}),
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("a refused connection must surface as a result error; got %q", res.Content)
	}
	if strings.Contains(res.Content, secretKey) {
		t.Fatalf("API key LEAKED into web_search error: %q", res.Content)
	}
	// The host (safe) should still be present for diagnosability.
	if !strings.Contains(res.Content, "127.0.0.1") {
		t.Errorf("error should name the bare endpoint host for diagnosability; got %q", res.Content)
	}
}

// TestWebSearch_BlockedEndpointDoesNotLeakAPIKey is the url-safety-block half: a blocked
// endpoint (SSRF floor) must surface only the host, never the key-bearing reqURL.
func TestWebSearch_BlockedEndpointDoesNotLeakAPIKey(t *testing.T) {
	t.Parallel()

	// Floor ON (default zero URLGuard): a loopback endpoint is blocked by the SSRF floor.
	endpoint := "http://127.0.0.1:9/search?key=" + secretKey
	res, err := NewWebSearch(security.URLGuard{}, endpoint).Execute(context.Background(), domain.ToolCall{
		ID: "c1", Tool: "web_search", Arguments: jsonArgs(t, map[string]any{"query": "needle"}),
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("a floor-blocked endpoint must surface as a result error; got %q", res.Content)
	}
	if strings.Contains(res.Content, secretKey) {
		t.Fatalf("API key LEAKED into web_search block error: %q", res.Content)
	}
}
