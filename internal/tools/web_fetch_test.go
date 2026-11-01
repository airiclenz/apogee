package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// fetchPageFixture is a small but whole page: a script and a style whose bodies are not
// prose, a comment, a heading, two paragraphs (one wrapped in the source), a list, and a
// <pre> whose indentation is the point of it.
const fetchPageFixture = `<!DOCTYPE html>
<html>
<head>
  <title>Widgets &amp; Gadgets</title>
  <style>body { color: red }</style>
  <script>window.track("visit");</script>
</head>
<body>
  <!-- navigation -->
  <h1>Widgets</h1>
  <p>alpha
     beta</p>
  <p>gamma <b>delta</b></p>
  <ul><li>one</li><li>two</li></ul>
  <pre>func main() {
	x := 1
}</pre>
  <noscript>enable javascript</noscript>
</body>
</html>`

// TestWebFetch_RendersHTMLAsText: a page comes back as its readable text — no tags, no
// script or style body, one line per block, a <pre> with its own lines and indentation —
// unless raw asks for the markup, and a non-HTML body is untouched either way.
func TestWebFetch_RendersHTMLAsText(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		contentType string
		body        string
		args        map[string]any
		want        string
	}{
		{
			name:        "an HTML page is rendered as text",
			contentType: "text/html; charset=utf-8",
			body:        fetchPageFixture,
			want: "HTTP 200 OK\nContent-Type: text/html; charset=utf-8\n\n" +
				"Widgets & Gadgets\nWidgets\nalpha beta\ngamma delta\none\ntwo\n" +
				"func main() {\n\tx := 1\n}",
		},
		{
			name:        "raw returns the markup",
			contentType: "text/html; charset=utf-8",
			body:        fetchPageFixture,
			args:        map[string]any{"raw": true},
			want:        "HTTP 200 OK\nContent-Type: text/html; charset=utf-8\n\n" + fetchPageFixture,
		},
		{
			name:        "a doctype without a content type is still a page",
			contentType: "",
			body:        "<!doctype html><html><body><p>plain <i>prose</i></p><script>x()</script></body></html>",
			want:        "HTTP 200 OK\n\nplain prose",
		},
		{
			name:        "text/plain is unchanged",
			contentType: "text/plain",
			body:        "<not html> & unchanged\n  as it came",
			want:        "HTTP 200 OK\nContent-Type: text/plain\n\n<not html> & unchanged\n  as it came",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.contentType != "" {
					w.Header().Set("Content-Type", tc.contentType)
				} else {
					w.Header()["Content-Type"] = nil // stop net/http sniffing one in
				}
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			args := map[string]any{"url": srv.URL}
			for k, v := range tc.args {
				args[k] = v
			}

			res, err := NewWebFetch(loopbackGuard()).Execute(context.Background(), domain.ToolCall{
				ID: "c1", Tool: "web_fetch", Arguments: jsonArgs(t, args),
			})

			if err != nil {
				t.Fatalf("Execute Go error: %v", err)
			}
			if res.IsError {
				t.Fatalf("result is error: %q", res.Content)
			}
			if res.Content != tc.want {
				t.Errorf("Content =\n%q\nwant\n%q", res.Content, tc.want)
			}
		})
	}
}

// fetchedBodyCase is one response a fetch tool's FetchedBody summary is checked against: the
// status the server answers with, the body size it writes, and the summary the tool must attach.
type fetchedBodyCase struct {
	name       string
	statusCode int
	bodyBytes  int
	want       domain.FetchedBody
}

// fetchedBodyCases covers both halves of the summary for either network tool: a body under the
// cap is not truncated, one past it is, and the status text is the server's own.
var fetchedBodyCases = []fetchedBodyCase{
	{
		name:       "a body under the cap is not truncated",
		statusCode: http.StatusOK,
		bodyBytes:  16,
		want:       domain.FetchedBody{Status: "200 OK", Truncated: false},
	},
	{
		name:       "a body past the cap is truncated",
		statusCode: http.StatusOK,
		bodyBytes:  maxNetworkResponseBytes + 1024,
		want:       domain.FetchedBody{Status: "200 OK", Truncated: true},
	},
	{
		name:       "a non-2xx status is carried as its text",
		statusCode: http.StatusNotFound,
		bodyBytes:  0,
		want:       domain.FetchedBody{Status: "404 Not Found", Truncated: false},
	},
}

// fetchedBodyServer answers every request with tc's status and a body of tc's size.
func fetchedBodyServer(t *testing.T, tc fetchedBodyCase) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(tc.statusCode)
		_, _ = w.Write(make([]byte, tc.bodyBytes))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestWebFetch_AttachesFetchedBodySummary: every fetch that received a response attaches a
// FetchedBody naming the status its first line prints and whether the body hit the cap.
func TestWebFetch_AttachesFetchedBodySummary(t *testing.T) {
	t.Parallel()

	for _, tc := range fetchedBodyCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := fetchedBodyServer(t, tc)

			res, err := NewWebFetch(loopbackGuard()).Execute(context.Background(), domain.ToolCall{
				ID: "c1", Tool: "web_fetch", Arguments: jsonArgs(t, map[string]any{"url": srv.URL}),
			})

			if err != nil {
				t.Fatalf("Execute Go error: %v", err)
			}
			if res.Summary != tc.want {
				t.Errorf("Summary = %#v, want %#v", res.Summary, tc.want)
			}
		})
	}
}
