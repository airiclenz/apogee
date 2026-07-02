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

// TestWebSearch_EmptyEndpointDefaultsToDuckDuckGo is deliberately white-box and does NOT
// call Execute: an empty endpoint now resolves to the LIVE DuckDuckGo default, so an
// Execute here would dial the real network and break hermeticity.
func TestWebSearch_EmptyEndpointDefaultsToDuckDuckGo(t *testing.T) {
	t.Parallel()
	tool := NewWebSearch(security.URLGuard{}, "")
	if tool.endpoint != defaultSearchEndpoint {
		t.Errorf("empty endpoint resolved to %q, want %q", tool.endpoint, defaultSearchEndpoint)
	}
	if tool.provider != providerDuckDuckGo {
		t.Errorf("empty endpoint selected provider %v, want providerDuckDuckGo", tool.provider)
	}
	if tool.disabled {
		t.Error("empty endpoint must not disable the tool")
	}
}

func TestWebSearch_OffSentinelIsGraceful(t *testing.T) {
	t.Parallel()
	for _, sentinel := range []string{"off", "OFF", " none ", "Disabled"} {
		tool := NewWebSearch(security.URLGuard{}, sentinel)
		// White-box: the sentinel must set the flag and store NO endpoint, so Execute
		// short-circuits before any URL is built — no HTTP request can be made.
		if !tool.disabled || tool.endpoint != "" {
			t.Errorf("%q: want disabled with no endpoint, got disabled=%v endpoint=%q",
				sentinel, tool.disabled, tool.endpoint)
		}
		res, err := tool.Execute(context.Background(), domain.ToolCall{
			ID: "c1", Tool: "web_search", Arguments: jsonArgs(t, map[string]any{"query": "go testing"}),
		})
		if err != nil {
			t.Fatalf("%q: unexpected Go error: %v", sentinel, err)
		}
		if res.IsError {
			t.Errorf("%q: the off sentinel must be graceful (not an error): %q", sentinel, res.Content)
		}
		if !strings.Contains(res.Content, "disabled") {
			t.Errorf("%q: result should say search is disabled: %q", sentinel, res.Content)
		}
	}
}

// TestWebSearch_SchemeLessEndpointSelfHeals: a custom endpoint without a scheme parses to
// Host == "" and url-safety would reject every request; NewWebSearch heals it to https://.
func TestWebSearch_SchemeLessEndpointSelfHeals(t *testing.T) {
	t.Parallel()
	tool := NewWebSearch(security.URLGuard{}, "search.example.com/s")
	if tool.endpoint != "https://search.example.com/s" {
		t.Errorf("scheme-less endpoint healed to %q, want https://search.example.com/s", tool.endpoint)
	}
	if tool.provider != providerCustom {
		t.Errorf("a healed endpoint is still a custom provider, got %v", tool.provider)
	}
}

// TestWebSearch_ConfiguredDDGEndpointIsBuiltInProvider: an endpoint EXPLICITLY pointing at
// the DuckDuckGo host must select the built-in provider (POST + browser headers) — treated
// as custom, its GET would draw DDG's bot-challenge page and never a result. White-box and
// no Execute, like the empty-endpoint test, so nothing dials the live host.
func TestWebSearch_ConfiguredDDGEndpointIsBuiltInProvider(t *testing.T) {
	t.Parallel()
	for _, endpoint := range []string{
		"https://html.duckduckgo.com/html/", // the default, spelled out in config
		"html.duckduckgo.com/html/",         // scheme-less: heals, then matches the host
	} {
		tool := NewWebSearch(security.URLGuard{}, endpoint)
		if tool.provider != providerDuckDuckGo {
			t.Errorf("%q: want providerDuckDuckGo, got %v", endpoint, tool.provider)
		}
		if tool.endpoint != defaultSearchEndpoint {
			t.Errorf("%q: endpoint resolved to %q, want %q", endpoint, tool.endpoint, defaultSearchEndpoint)
		}
	}
}

// TestWebSearch_DuckDuckGoProviderPosts: the built-in provider sends the query as a POST
// form field with NO query in the URL — DDG answers a GET with its bot-challenge page. The
// provider is forced onto an httptest endpoint (white-box) to keep the test hermetic.
func TestWebSearch_DuckDuckGoProviderPosts(t *testing.T) {
	t.Parallel()

	var gotMethod, gotForm, gotRawQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotRawQuery = r.Method, r.URL.RawQuery
		gotForm = r.FormValue("q")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(ddgFixture))
	}))
	defer srv.Close()

	tool := &WebSearch{guard: loopbackGuard(), endpoint: srv.URL, provider: providerDuckDuckGo}
	res, err := tool.Execute(context.Background(), domain.ToolCall{
		ID: "c1", Tool: "web_search", Arguments: jsonArgs(t, map[string]any{"query": "golang docs"}),
	})
	if err != nil {
		t.Fatalf("Execute Go error: %v", err)
	}
	if res.IsError {
		t.Fatalf("result is error: %q", res.Content)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("DDG provider sent %s, want POST", gotMethod)
	}
	if gotForm != "golang docs" {
		t.Errorf("POST form q=%q, want %q", gotForm, "golang docs")
	}
	if gotRawQuery != "" {
		t.Errorf("the request URL must carry no query, got %q", gotRawQuery)
	}
	if !strings.Contains(res.Content, "1. Go Documentation & Guides") {
		t.Errorf("structured render missing results: %q", res.Content)
	}
}

func TestWebSearch_QueriesConfiguredEndpoint(t *testing.T) {
	t.Parallel()

	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("q")
		w.Header().Set("Content-Type", "application/json")
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
	if !strings.Contains(res.Content, `{"results":[]}`) {
		t.Errorf("a custom endpoint's JSON body must pass through verbatim: %q", res.Content)
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
