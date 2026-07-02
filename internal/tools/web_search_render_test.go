package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// ddgFixture is a minimal DuckDuckGo HTML results page: two result anchors with
// uddg-wrapped redirector URLs, entity-encoded text, and nested tags — the shapes
// parseDDGResults must clean.
const ddgFixture = `<!DOCTYPE html>
<html><body>
<div class="result">
  <a rel="nofollow" class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fgo.dev%2Fdoc%2F&amp;rut=abc">Go <b>Documentation</b> &amp; Guides</a>
  <a class="result__snippet" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fgo.dev%2Fdoc%2F">Learn <b>Go</b> &quot;the language&quot;&nbsp;today.</a>
</div>
<div class="result">
  <a rel="nofollow" class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fpkg.go.dev%2F&amp;rut=def">pkg.go.dev</a>
  <a class="result__snippet" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fpkg.go.dev%2F">The Go package index.</a>
</div>
</body></html>`

func execSearch(t *testing.T, endpoint string) domain.ToolResult {
	t.Helper()
	res, err := NewWebSearch(loopbackGuard(), endpoint).Execute(context.Background(), domain.ToolCall{
		ID: "c1", Tool: "web_search", Arguments: jsonArgs(t, map[string]any{"query": "golang docs"}),
	})
	if err != nil {
		t.Fatalf("Execute Go error: %v", err)
	}
	return res
}

// TestParseDDGResults_Fixture unit-tests the parser directly: redirector URLs unwrap and
// query-unescape, titles/snippets pair by position, entities decode, tags are gone.
func TestParseDDGResults_Fixture(t *testing.T) {
	t.Parallel()
	results := parseDDGResults(ddgFixture)
	if len(results) != 2 {
		t.Fatalf("parsed %d results, want 2: %+v", len(results), results)
	}
	first := results[0]
	if first.url != "https://go.dev/doc/" {
		t.Errorf("redirector URL not unwrapped: %q", first.url)
	}
	if first.title != "Go Documentation & Guides" {
		t.Errorf("title not cleaned: %q", first.title)
	}
	if first.snippet != `Learn Go "the language" today.` {
		t.Errorf("snippet not cleaned: %q", first.snippet)
	}
	if results[1].url != "https://pkg.go.dev/" || results[1].title != "pkg.go.dev" {
		t.Errorf("second result wrong: %+v", results[1])
	}
	for _, r := range results {
		for _, s := range []string{r.title, r.url, r.snippet} {
			if strings.ContainsAny(s, "<>") {
				t.Errorf("HTML survived cleaning: %q", s)
			}
		}
	}
}

// TestRenderSearch_DuckDuckGoAlwaysStructured covers the default provider's dispatch rule:
// a DDG response is ALWAYS parsed structurally, and a page with no result anchors (a
// rate-limit/consent page) renders "No results" — never its stripped text.
func TestRenderSearch_DuckDuckGoAlwaysStructured(t *testing.T) {
	t.Parallel()
	resp := &http.Response{Status: "200 OK", StatusCode: http.StatusOK, Header: http.Header{}}

	out := renderSearch(providerDuckDuckGo, resp, ddgFixture, "golang docs", false)
	if !strings.Contains(out, "1. Go Documentation & Guides") || !strings.Contains(out, "https://go.dev/doc/") {
		t.Errorf("structured render missing numbered title/url: %q", out)
	}

	challenge := `<html><body><p>Unfortunately, bots use DuckDuckGo too.</p></body></html>`
	out = renderSearch(providerDuckDuckGo, resp, challenge, "golang docs", false)
	if out != "No results found for: golang docs" {
		t.Errorf("an anchor-less DDG page must render 'No results', got: %q", out)
	}
	if strings.Contains(out, "bots") {
		t.Errorf("DDG must never fall through to stripped page text: %q", out)
	}
}

// TestWebSearch_ParsesDuckDuckGoHTML: a custom endpoint serving a DDG-shaped page (a
// self-hosted mirror) end-to-end — structured results, entities decoded, no tags.
func TestWebSearch_ParsesDuckDuckGoHTML(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(ddgFixture))
	}))
	defer srv.Close()

	res := execSearch(t, srv.URL)
	if res.IsError {
		t.Fatalf("result is error: %q", res.Content)
	}
	for _, want := range []string{
		"1. Go Documentation & Guides", "https://go.dev/doc/",
		`Learn Go "the language" today.`, "2. pkg.go.dev",
	} {
		if !strings.Contains(res.Content, want) {
			t.Errorf("structured render missing %q: %q", want, res.Content)
		}
	}
	if strings.Contains(res.Content, "result__a") || strings.Contains(res.Content, "<b>") {
		t.Errorf("raw HTML leaked into the render: %q", res.Content)
	}
}

// TestWebSearch_NoContentTypeStillParsesHTML: a backend that sends NO Content-Type header
// still gets the HTML treatment via the body sniff.
func TestWebSearch_NoContentTypeStillParsesHTML(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header()["Content-Type"] = nil // suppress Go's auto-detection: send NO header
		_, _ = w.Write([]byte(ddgFixture))
	}))
	defer srv.Close()

	res := execSearch(t, srv.URL)
	if res.IsError {
		t.Fatalf("result is error: %q", res.Content)
	}
	if !strings.Contains(res.Content, "1. Go Documentation & Guides") {
		t.Errorf("body-sniffed HTML was not parsed: %q", res.Content)
	}
}

// TestWebSearch_CleansGenericHTML: non-DDG HTML from a custom endpoint is stripped,
// entity-decoded, and whitespace-collapsed into clean text.
func TestWebSearch_CleansGenericHTML(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body>\n  <h1>Results</h1>\n  <p>alpha &amp;\n  beta</p>\n</body></html>"))
	}))
	defer srv.Close()

	res := execSearch(t, srv.URL)
	if res.IsError {
		t.Fatalf("result is error: %q", res.Content)
	}
	if res.Content != "Results alpha & beta" {
		t.Errorf("generic HTML not cleaned/collapsed, got: %q", res.Content)
	}
}

// TestWebSearch_JSONPassthrough: a clean (non-HTML) custom backend's document reaches the
// model verbatim, exactly as before the DuckDuckGo default landed.
func TestWebSearch_JSONPassthrough(t *testing.T) {
	t.Parallel()
	const doc = `{"results":[{"title":"Go","url":"https://go.dev"}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(doc))
	}))
	defer srv.Close()

	res := execSearch(t, srv.URL)
	if res.IsError {
		t.Fatalf("result is error: %q", res.Content)
	}
	if !strings.Contains(res.Content, doc) {
		t.Errorf("JSON body must pass through verbatim: %q", res.Content)
	}
	if !strings.Contains(res.Content, "HTTP 200") {
		t.Errorf("pass-through keeps the status line: %q", res.Content)
	}
}

// TestWebSearch_Non2xxIsResultError: a non-2xx response is now a result error naming only
// the status and the endpoint host — the body (rate-limit/challenge noise) is dropped.
func TestWebSearch_Non2xxIsResultError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("<html>rate limited, try later</html>"))
	}))
	defer srv.Close()

	res := execSearch(t, srv.URL)
	if !res.IsError {
		t.Fatalf("a non-2xx response must be a result error, got: %q", res.Content)
	}
	if !strings.Contains(res.Content, "429") || !strings.Contains(res.Content, "127.0.0.1") {
		t.Errorf("error should name the status and the bare host: %q", res.Content)
	}
	if strings.Contains(res.Content, "rate limited") {
		t.Errorf("a non-2xx body must not reach the model: %q", res.Content)
	}
}
