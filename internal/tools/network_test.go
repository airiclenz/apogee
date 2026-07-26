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
	// The no-follow policy's own rationale is that the model can follow the redirect itself
	// through a fresh, re-checked call — which needs the target (M-6). Refusing to follow and
	// then hiding where it pointed leaves a 302 with an empty body and no way forward.
	if !strings.Contains(res.Content, "Location: http://169.254.169.254/latest/meta-data/") {
		t.Errorf("the refused redirect's target must be rendered, got: %q", res.Content)
	}
}

// TestWebFetch_RendersRedirectLocation pins WHEN the redirect target is rendered: on any 3xx that
// carries one, never beside a 200 (a Location there is not a redirect), and with no stray line when
// a 3xx carries none — a 3xx without Location is legal and observed in the wild.
func TestWebFetch_RendersRedirectLocation(t *testing.T) {
	t.Parallel()

	const target = "https://example.com/next?a=1"
	cases := []struct {
		name     string
		status   int
		location string
		want     bool
	}{
		{"301 moved permanently", http.StatusMovedPermanently, target, true},
		{"302 found", http.StatusFound, target, true},
		{"303 see other", http.StatusSeeOther, target, true},
		{"307 temporary redirect", http.StatusTemporaryRedirect, target, true},
		{"308 permanent redirect", http.StatusPermanentRedirect, target, true},
		{"a relative target is rendered as sent", http.StatusFound, "/canonical/", true},
		{"3xx with no location", http.StatusFound, "", false},
		{"200 carrying a location header", http.StatusOK, target, false},
		{"404 carrying a location header", http.StatusNotFound, target, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.location != "" {
					w.Header().Set("Location", tc.location)
				}
				w.Header().Set("Content-Type", "text/plain")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte("body"))
			}))
			defer srv.Close()

			res, err := NewWebFetch(loopbackGuard()).Execute(context.Background(), domain.ToolCall{
				ID: "c1", Tool: "web_fetch", Arguments: jsonArgs(t, map[string]any{"url": srv.URL}),
			})
			if err != nil {
				t.Fatalf("Execute Go error: %v", err)
			}
			if res.IsError {
				t.Fatalf("result is error: %q", res.Content)
			}
			if got := strings.Contains(res.Content, "Location: "+tc.location); got != tc.want {
				t.Errorf("rendered Location = %v, want %v; result: %q", got, tc.want, res.Content)
			}
			if !tc.want && strings.Contains(res.Content, "Location") {
				t.Errorf("no Location line may be rendered here: %q", res.Content)
			}
			// A missing Location must leave the header block as it always was — no blank line
			// where the value would have gone.
			if strings.Contains(res.Content, "\n\n\n") {
				t.Errorf("a stray empty line entered the render: %q", res.Content)
			}
		})
	}
}

// TestWebFetch_HostileRedirectLocationIsRenderedInert is the adversarial half of M-6: the Location
// is SERVER-chosen text promoted out of the body into the header block the model reads as fact, so
// it must reach the model inert. net/http refuses a C0 byte in a header value and Go's client
// refuses an unparseable Location before CheckRedirect is consulted, but a fold/CRLF-mapped space,
// a bidi override and an unbounded length all survive that — and the header block sits outside the
// body cap.
func TestWebFetch_HostileRedirectLocationIsRenderedInert(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		location    string
		wantAbsent  []string
		wantPresent []string
	}{
		{
			// Go's server maps CR/LF in a header value to spaces, so this arrives folded rather
			// than as a second header line; the fold must stay inside the Location line.
			name:        "crlf header injection",
			location:    "https://a.example/\r\nX-Injected: yes",
			wantAbsent:  []string{"\nX-Injected"},
			wantPresent: []string{"Location: https://a.example/ X-Injected: yes"},
		},
		{
			name:        "a fake status line in the value stays on the value's line",
			location:    "https://a.example/ HTTP 200 OK",
			wantAbsent:  []string{"\nHTTP 200 OK"},
			wantPresent: []string{"Location: https://a.example/ HTTP 200 OK"},
		},
		{
			// A right-to-left override and a zero-width space spoof what a reader sees of a URL
			// the model is invited to follow.
			name:        "bidi override and zero-width characters",
			location:    "https://a.example/‮gnp.exe​",
			wantAbsent:  []string{"‮", "​"},
			wantPresent: []string{"Location: https://a.example/gnp.exe"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res := fetchWithLocation(t, tc.location)
			for _, s := range tc.wantAbsent {
				if strings.Contains(res.Content, s) {
					t.Errorf("hostile Location survived into the render (%q): %q", s, res.Content)
				}
			}
			for _, s := range tc.wantPresent {
				if !strings.Contains(res.Content, s) {
					t.Errorf("want %q in the render, got: %q", s, res.Content)
				}
			}
			// Whatever the value, the header block is the status line, the content type and at
			// most one Location line — never a line the server smuggled in.
			head, _, _ := strings.Cut(res.Content, "\n\n")
			if lines := strings.Split(head, "\n"); len(lines) != 3 {
				t.Errorf("header block = %q, want exactly 3 lines", head)
			}
		})
	}

	// The response header block is outside the body cap (net/http accepts a 10 MiB one), so an
	// oversized Location would otherwise be a way to flood the model's context past it.
	t.Run("an oversized location is capped and marked", func(t *testing.T) {
		t.Parallel()
		res := fetchWithLocation(t, "https://a.example/"+strings.Repeat("x", 64*1024))
		if !strings.Contains(res.Content, "[location truncated at 2048 bytes]") {
			t.Errorf("an oversized Location must be marked as cut: %q", truncateForLog(res.Content))
		}
		head, _, _ := strings.Cut(res.Content, "\n\n")
		if len(head) > maxLocationBytes+256 {
			t.Errorf("header block is %d bytes, want it bounded by the location cap", len(head))
		}
	})
}

// fetchWithLocation runs web_fetch against a server answering 302 with the given Location.
func fetchWithLocation(t *testing.T, location string) domain.ToolResult {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", location)
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()

	res, err := NewWebFetch(loopbackGuard()).Execute(context.Background(), domain.ToolCall{
		ID: "c1", Tool: "web_fetch", Arguments: jsonArgs(t, map[string]any{"url": srv.URL}),
	})
	if err != nil {
		t.Fatalf("Execute Go error: %v", err)
	}
	if res.IsError {
		t.Fatalf("result is error: %q", res.Content)
	}
	return res
}

// truncateForLog keeps a failure message readable when the value under test is huge.
func truncateForLog(s string) string {
	if len(s) <= 200 {
		return s
	}
	return s[:200] + "…"
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

// TestHTTPRequest_HostileResponseHeadersAreRenderedInert is item 13's adversarial treatment over
// the WHOLE header block http_request renders (its asymmetric twin): every response header is
// SERVER-chosen text lifted out of the body and into the block the model reads as fact, so a bidi
// override, a zero-width rune and a folded value carrying a fake status line must all reach the
// model inert, and nothing inside a value may open a line of its own.
func TestHTTPRequest_HostileResponseHeadersAreRenderedInert(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Go's server maps CR/LF in a header value to spaces, so the injected lines arrive
		// folded into one value — the same fold the web_fetch Location tests lean on.
		w.Header().Set("X-Folded", "ok\r\nHTTP/1.1 200 OK\r\nX-Injected: yes")
		w.Header().Set("X-Bidi", "report‮gnp.exe")
		w.Header().Set("X-Zero", "gap​less")
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("body"))
	}))
	defer srv.Close()

	res, err := NewHTTPRequest(loopbackGuard()).Execute(context.Background(), domain.ToolCall{
		ID: "c1", Tool: "http_request", Arguments: jsonArgs(t, map[string]any{"url": srv.URL}),
	})
	if err != nil {
		t.Fatalf("Execute Go error: %v", err)
	}
	if res.IsError {
		t.Fatalf("result is error: %q", res.Content)
	}

	for _, s := range []string{"‮", "​", "\nHTTP/1.1 200 OK", "\nX-Injected"} {
		if strings.Contains(res.Content, s) {
			t.Errorf("hostile header text survived into the render (%q): %q", s, res.Content)
		}
	}
	for _, s := range []string{
		"X-Folded: ok HTTP/1.1 200 OK X-Injected: yes",
		"X-Bidi: reportgnp.exe",
		"X-Zero: gapless",
		"Content-Type: text/plain",
	} {
		if !strings.Contains(res.Content, s) {
			t.Errorf("want %q in the render, got: %q", s, res.Content)
		}
	}
	// Nothing opened a line of its own: after the status line, every header-block line names a
	// header the handler (or net/http itself) legitimately set.
	sent := map[string]bool{
		"X-Folded": true, "X-Bidi": true, "X-Zero": true,
		"Content-Type": true, "Content-Length": true, "Date": true,
	}
	head, _, _ := strings.Cut(res.Content, "\n\n")
	lines := strings.Split(head, "\n")
	if !strings.HasPrefix(lines[0], "HTTP ") {
		t.Errorf("first line must be the status line, got %q", lines[0])
	}
	for _, line := range lines[1:] {
		name, _, ok := strings.Cut(line, ": ")
		if !ok || !sent[name] {
			t.Errorf("a line no header legitimately owns opened in the block: %q", line)
		}
	}
}

// TestHTTPRequest_OversizedHeaderBlockIsCappedAndMarked: the response HEADER block is outside
// maxNetworkResponseBytes (the transport accepts a 10 MiB one by default), so the render must
// bound it — per value and as a whole — and a cut must be MARKED, never a silent stub. The byte
// assertion is on the rendered header block, as in the web_fetch oversized-Location case; the
// body is untouched by these caps.
func TestHTTPRequest_OversizedHeaderBlockIsCappedAndMarked(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		big := strings.Repeat("x", 2*maxResponseHeaderValueBytes)
		for i := 0; i < 64; i++ {
			w.Header().Set("X-Big-"+strconv.Itoa(i), big)
		}
		_, _ = w.Write([]byte("tiny body"))
	}))
	defer srv.Close()

	res, err := NewHTTPRequest(loopbackGuard()).Execute(context.Background(), domain.ToolCall{
		ID: "c1", Tool: "http_request", Arguments: jsonArgs(t, map[string]any{"url": srv.URL}),
	})
	if err != nil {
		t.Fatalf("Execute Go error: %v", err)
	}
	if res.IsError {
		t.Fatalf("result is error: %q", truncateForLog(res.Content))
	}
	if !strings.Contains(res.Content, "[value truncated at 4096 bytes]") {
		t.Errorf("an oversized header value must be marked as cut: %q", truncateForLog(res.Content))
	}
	if !strings.Contains(res.Content, "[header block truncated at 65536 bytes]") {
		t.Errorf("an oversized header block must be marked as cut: %q", truncateForLog(res.Content))
	}
	head, _, _ := strings.Cut(res.Content, "\n\n")
	if len(head) > maxResponseHeaderBlockBytes+256 {
		t.Errorf("header block is %d bytes, want it bounded by the block cap", len(head))
	}
	if !strings.Contains(res.Content, "tiny body") {
		t.Errorf("the body must still render after a cut header block: %q", truncateForLog(res.Content))
	}
}

// TestHTTPRequest_PlainHeadersRenderUnchanged is the neutering's negative control: ordinary
// header values pass through byte for byte — no fold, no cut, no marker.
func TestHTTPRequest_PlainHeadersRenderUnchanged(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Date", "Mon, 02 Jan 2006 15:04:05 GMT")
		w.Header().Set("Etag", `W/"0815-abc"`)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	res, err := NewHTTPRequest(loopbackGuard()).Execute(context.Background(), domain.ToolCall{
		ID: "c1", Tool: "http_request", Arguments: jsonArgs(t, map[string]any{"url": srv.URL}),
	})
	if err != nil {
		t.Fatalf("Execute Go error: %v", err)
	}
	if res.IsError {
		t.Fatalf("result is error: %q", res.Content)
	}
	for _, s := range []string{
		"Content-Type: text/html; charset=utf-8\n",
		"Date: Mon, 02 Jan 2006 15:04:05 GMT\n",
		"Etag: W/\"0815-abc\"\n",
	} {
		if !strings.Contains(res.Content, s) {
			t.Errorf("plain header mangled: want %q in %q", s, res.Content)
		}
	}
	if strings.Contains(res.Content, "truncated") {
		t.Errorf("no truncation marker may appear on a plain response: %q", res.Content)
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

// ---- web_fetch + http_request: the M2 generalization ------------------------

// TestNetworkTools_FailureMessagesDoNotLeakKey is the M2 regression for the simple pair: now
// that web_fetch and http_request reach the network only through the funnel, every failure
// message names the bare HOST and never the (possibly key-bearing) request URL — the
// protection that used to be web_search's private discipline (web_search_redaction_test.go).
// The two unparseable rows are the ones that used to escape: url-safety interpolated the parse
// error, whose *url.Error text quotes the URL back under %q — harmless-looking for the
// whitespace row (the quoted form equals the trimmed one, which redaction covers) and a live
// key leak for the control-character row, where %q escapes the byte and the raw substring
// search finds nothing (M-2).
func TestNetworkTools_FailureMessagesDoNotLeakKey(t *testing.T) {
	t.Parallel()

	// A reachable server closed immediately, so the request fails at the transport with a
	// *url.Error embedding the full request URL (host + query + key).
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	closedURL := srv.URL + "/x?key=" + secretKey
	srv.Close()

	cases := []struct {
		name string
		// guard is the tool's url-safety policy; the zero value keeps the SSRF floor ON.
		guard security.URLGuard
		url   string
		// host is the bare host the message must still name for diagnosability; "" when the
		// URL is unparseable and there is no host to name.
		host string
	}{
		{"blocked by the SSRF floor", security.URLGuard{}, "http://127.0.0.1:9/x?key=" + secretKey, "127.0.0.1"},
		{"transport failure", loopbackGuard(), closedURL, "127.0.0.1"},
		{"unparseable url with leading whitespace", loopbackGuard(), " http://exa mple.com/?key=" + secretKey, ""},
		{"unparseable url with an interior control character", loopbackGuard(), "http://example.com/?key=" + secretKey + "\x01x", ""},
	}
	makeTool := map[string]func(security.URLGuard) domain.Tool{
		"web_fetch":    func(g security.URLGuard) domain.Tool { return NewWebFetch(g) },
		"http_request": func(g security.URLGuard) domain.Tool { return NewHTTPRequest(g) },
	}

	for name, newTool := range makeTool {
		for _, tc := range cases {
			t.Run(name+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				res, err := newTool(tc.guard).Execute(context.Background(), domain.ToolCall{
					ID: "c1", Tool: name, Arguments: jsonArgs(t, map[string]any{"url": tc.url}),
				})
				if err != nil {
					t.Fatalf("unexpected Go error: %v", err)
				}
				if !res.IsError {
					t.Fatalf("want a result error, got %q", res.Content)
				}
				if strings.Contains(res.Content, secretKey) {
					t.Fatalf("API key LEAKED into the %s failure message: %q", name, res.Content)
				}
				if tc.host != "" && !strings.Contains(res.Content, tc.host) {
					t.Errorf("message should name the bare host %q for diagnosability: %q", tc.host, res.Content)
				}
			})
		}
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

	tool := &WebSearch{networkTool: networkTool{guard: loopbackGuard()}, endpoint: srv.URL, provider: providerDuckDuckGo}
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
