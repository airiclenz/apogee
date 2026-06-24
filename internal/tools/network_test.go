package tools

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/security"
)

// loopbackGuard is a URLGuard with the SSRF floor OFF so the hermetic httptest servers (which
// bind 127.0.0.1) are reachable in the happy-path tests. The floor's blocking behaviour has
// its own dedicated coverage in security/ssrf_test.go and the floor-on tests below.
func loopbackGuard() security.URLGuard { return security.URLGuard{}.DisableIPFloor() }

func jsonArgs(t *testing.T, m map[string]any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	return b
}

// ---- web_fetch -------------------------------------------------------------

func TestWebFetch_GetsBody(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("web_fetch used method %q, want GET", r.Method)
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("hello world"))
	}))
	defer srv.Close()

	tool := NewWebFetch(loopbackGuard())
	res, err := tool.Execute(context.Background(), domain.ToolCall{
		ID: "c1", Tool: "web_fetch", Arguments: jsonArgs(t, map[string]any{"url": srv.URL}),
	})
	if err != nil {
		t.Fatalf("Execute Go error: %v", err)
	}
	if res.IsError {
		t.Fatalf("result is error: %q", res.Content)
	}
	if !strings.Contains(res.Content, "hello world") {
		t.Errorf("body missing from result: %q", res.Content)
	}
	if !strings.Contains(res.Content, "HTTP 200") {
		t.Errorf("status line missing: %q", res.Content)
	}
}

func TestWebFetch_IsNetworkExternalEffect(t *testing.T) {
	t.Parallel()
	ext, ok := domain.Tool(NewWebFetch(security.URLGuard{})).(domain.ExternalEffectTool)
	if !ok {
		t.Fatal("web_fetch must be an ExternalEffectTool")
	}
	if ext.ExternalEffect() != domain.EffectNetwork {
		t.Errorf("web_fetch effect = %q, want network", ext.ExternalEffect())
	}
}

func TestWebFetch_NotWorkspaceWriterOrSubprocess(t *testing.T) {
	t.Parallel()
	tool := domain.Tool(NewWebFetch(security.URLGuard{}))
	if IsWorkspaceScopedWriter(tool) {
		t.Error("a network tool must NOT carry the workspaceScopedWriter marker")
	}
	if domain.IsSubprocessTool(tool) {
		t.Error("an in-process net/http tool must NOT be a SubprocessTool")
	}
}

func TestWebFetch_BlockedURLIsResultError(t *testing.T) {
	t.Parallel()
	// Floor ON (default guard): a loopback URL is refused before any request.
	res, err := NewWebFetch(security.URLGuard{}).Execute(context.Background(), domain.ToolCall{
		ID: "c1", Tool: "web_fetch", Arguments: jsonArgs(t, map[string]any{"url": "http://127.0.0.1:1/x"}),
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !res.IsError {
		t.Error("a loopback URL must be blocked by the SSRF floor (result error)")
	}
	if !strings.Contains(res.Content, "url-safety") {
		t.Errorf("blocked result should name url-safety: %q", res.Content)
	}
}

func TestWebFetch_MissingURLIsResultError(t *testing.T) {
	t.Parallel()
	res, err := NewWebFetch(loopbackGuard()).Execute(context.Background(), domain.ToolCall{
		ID: "c1", Tool: "web_fetch", Arguments: jsonArgs(t, map[string]any{}),
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !res.IsError {
		t.Error("a missing url must be a result error")
	}
}

func TestWebFetch_DoesNotFollowRedirectToPrivate(t *testing.T) {
	t.Parallel()

	// The server 302-redirects to a loopback target. With CheckRedirect set to use the last
	// response, the tool returns the 302 rather than auto-following into the (blocked) host —
	// the model sees the redirect and must re-fetch through a fresh, re-checked call.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
	}))
	defer srv.Close()

	res, err := NewWebFetch(loopbackGuard()).Execute(context.Background(), domain.ToolCall{
		ID: "c1", Tool: "web_fetch", Arguments: jsonArgs(t, map[string]any{"url": srv.URL}),
	})
	if err != nil {
		t.Fatalf("Execute Go error: %v", err)
	}
	if !strings.Contains(res.Content, "HTTP 302") {
		t.Errorf("expected the raw 302 (no auto-follow), got: %q", res.Content)
	}
}

// ---- http_request ----------------------------------------------------------

func TestHTTPRequest_PostsBody(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if got := r.Header.Get("X-Custom"); got != "v" {
			t.Errorf("header X-Custom = %q, want v", got)
		}
		buf := make([]byte, 64)
		n, _ := r.Body.Read(buf)
		if string(buf[:n]) != "payload" {
			t.Errorf("body = %q, want payload", string(buf[:n]))
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	res, err := NewHTTPRequest(loopbackGuard()).Execute(context.Background(), domain.ToolCall{
		ID: "c1", Tool: "http_request", Arguments: jsonArgs(t, map[string]any{
			"url": srv.URL, "method": "post",
			"headers": map[string]string{"X-Custom": "v"},
			"body":    "payload",
		}),
	})
	if err != nil {
		t.Fatalf("Execute Go error: %v", err)
	}
	if res.IsError {
		t.Fatalf("result is error: %q", res.Content)
	}
	if !strings.Contains(res.Content, "HTTP 201") {
		t.Errorf("status missing: %q", res.Content)
	}
}

// TestHTTPRequest_RejectsDeniedHeaders proves the SEC-04 header filter: a hop-by-hop / framing
// header or a forged Host is refused as a result error and the request never goes out.
func TestHTTPRequest_RejectsDeniedHeaders(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		header string
	}{
		{"forged host", "Host"},
		{"connection", "Connection"},
		{"transfer-encoding", "Transfer-Encoding"},
		{"content-length", "Content-Length"},
		{"proxy-authorization", "Proxy-Authorization"},
		// Case-insensitivity: a lower-cased spelling is canonicalised and still denied.
		{"lower-cased host", "host"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reached := false
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				reached = true
			}))
			defer srv.Close()

			res, err := NewHTTPRequest(loopbackGuard()).Execute(context.Background(), domain.ToolCall{
				ID: "c1", Tool: "http_request", Arguments: jsonArgs(t, map[string]any{
					"url": srv.URL, "method": "post",
					"headers": map[string]string{tc.header: "x"},
				}),
			})
			if err != nil {
				t.Fatalf("unexpected Go error: %v", err)
			}
			if !res.IsError {
				t.Errorf("header %q must be rejected as a result error; got %q", tc.header, res.Content)
			}
			if reached {
				t.Errorf("header %q was rejected but the request still reached the server", tc.header)
			}
		})
	}
}

// TestHTTPRequest_HeaderCountCapped proves the SEC-04 count cap: more than maxRequestHeaders
// model-supplied headers is refused before the request goes out.
func TestHTTPRequest_HeaderCountCapped(t *testing.T) {
	t.Parallel()

	headers := make(map[string]string, maxRequestHeaders+1)
	for i := 0; i <= maxRequestHeaders; i++ {
		headers["X-H-"+strconv.Itoa(i)] = "v"
	}
	res, err := NewHTTPRequest(loopbackGuard()).Execute(context.Background(), domain.ToolCall{
		ID: "c1", Tool: "http_request", Arguments: jsonArgs(t, map[string]any{
			"url": "https://example.com", "headers": headers,
		}),
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !res.IsError || !strings.Contains(res.Content, "too many request headers") {
		t.Errorf("an over-cap header block must be rejected; got %q", res.Content)
	}
}

// TestHTTPRequest_HeaderValueCapped proves the SEC-04 per-value size cap.
func TestHTTPRequest_HeaderValueCapped(t *testing.T) {
	t.Parallel()

	res, err := NewHTTPRequest(loopbackGuard()).Execute(context.Background(), domain.ToolCall{
		ID: "c1", Tool: "http_request", Arguments: jsonArgs(t, map[string]any{
			"url": "https://example.com",
			"headers": map[string]string{
				"X-Big": strings.Repeat("a", maxRequestHeaderValueBytes+1),
			},
		}),
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !res.IsError || !strings.Contains(res.Content, "value too large") {
		t.Errorf("an over-cap header value must be rejected; got %q", res.Content)
	}
}

func TestHTTPRequest_RejectsUnsupportedMethod(t *testing.T) {
	t.Parallel()
	res, err := NewHTTPRequest(loopbackGuard()).Execute(context.Background(), domain.ToolCall{
		ID: "c1", Tool: "http_request", Arguments: jsonArgs(t, map[string]any{
			"url": "https://example.com", "method": "CONNECT",
		}),
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !res.IsError {
		t.Error("an unsupported method must be a result error")
	}
}

func TestHTTPRequest_BlockedURLIsResultError(t *testing.T) {
	t.Parallel()
	res, err := NewHTTPRequest(security.URLGuard{}).Execute(context.Background(), domain.ToolCall{
		ID: "c1", Tool: "http_request", Arguments: jsonArgs(t, map[string]any{
			"url": "http://10.0.0.1/admin",
		}),
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !res.IsError || !strings.Contains(res.Content, "url-safety") {
		t.Errorf("a private URL must be blocked by url-safety: %q", res.Content)
	}
}

// ---- web_search ------------------------------------------------------------

func TestWebSearch_UnconfiguredIsGraceful(t *testing.T) {
	t.Parallel()
	res, err := NewWebSearch(security.URLGuard{}, "").Execute(context.Background(), domain.ToolCall{
		ID: "c1", Tool: "web_search", Arguments: jsonArgs(t, map[string]any{"query": "go testing"}),
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if res.IsError {
		t.Errorf("an unconfigured search endpoint must be graceful (not an error): %q", res.Content)
	}
	if !strings.Contains(res.Content, "not configured") {
		t.Errorf("result should say search is not configured: %q", res.Content)
	}
}

func TestWebSearch_QueriesConfiguredEndpoint(t *testing.T) {
	t.Parallel()

	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("q")
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	defer srv.Close()

	res, err := NewWebSearch(loopbackGuard(), srv.URL).Execute(context.Background(), domain.ToolCall{
		ID: "c1", Tool: "web_search", Arguments: jsonArgs(t, map[string]any{"query": "needle"}),
	})
	if err != nil {
		t.Fatalf("Execute Go error: %v", err)
	}
	if res.IsError {
		t.Fatalf("result is error: %q", res.Content)
	}
	if gotQuery != "needle" {
		t.Errorf("endpoint saw q=%q, want needle", gotQuery)
	}
}

func TestWebSearch_MissingQueryIsResultError(t *testing.T) {
	t.Parallel()
	res, err := NewWebSearch(security.URLGuard{}, "https://search.example.com").
		Execute(context.Background(), domain.ToolCall{
			ID: "c1", Tool: "web_search", Arguments: jsonArgs(t, map[string]any{}),
		})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !res.IsError {
		t.Error("a missing query must be a result error")
	}
}

func TestNetworkTools_CancelledCtxIsGoError(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	call := domain.ToolCall{ID: "c1", Arguments: jsonArgs(t, map[string]any{"url": "https://example.com", "query": "x"})}
	for name, tool := range map[string]domain.Tool{
		"web_fetch":    NewWebFetch(loopbackGuard()),
		"http_request": NewHTTPRequest(loopbackGuard()),
		"web_search":   NewWebSearch(loopbackGuard(), "https://search.example.com"),
	} {
		_, err := tool.Execute(ctx, call)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("%s: cancelled ctx should be a Go error; got %v", name, err)
		}
	}
}
