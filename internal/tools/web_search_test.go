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

// The search endpoint is the one thing about web_search that MOVES: `web-search-endpoint:` is a
// setting the human can commit mid-session and have take effect at once (ADR 0037), and the registry
// holds this pointer for the life of the session — so the next Execute has to go to the new backend
// without anything being rebuilt around it. Two servers rather than one recorder: what the tool
// RETURNS is what proves which host it reached, and it needs no shared state to observe.
func TestWebSearchSetEndpointMovesTheNextRequest(t *testing.T) {
	t.Parallel()
	first := httptest.NewServer(searchBackend("ALPHA RESULTS"))
	defer first.Close()
	second := httptest.NewServer(searchBackend("BETA RESULTS"))
	defer second.Close()

	tool := NewWebSearch(loopbackGuard(), first.URL+"/search")
	if got := searchContent(t, tool); !strings.Contains(got, "ALPHA RESULTS") {
		t.Fatalf("the tool answered %q before any move; want the first backend's results", got)
	}

	tool.SetEndpoint(second.URL + "/search")
	got := searchContent(t, tool)
	if !strings.Contains(got, "BETA RESULTS") {
		t.Errorf("after SetEndpoint the tool answered %q; want the second backend's results", got)
	}
	if strings.Contains(got, "ALPHA RESULTS") {
		t.Errorf("the tool still reached the old endpoint: %q", got)
	}
}

// SetEndpoint resolves its argument exactly as construction does, so every spelling the config
// accepts means the same thing to a live tool as it does to a fresh one: the sentinels disable it
// gracefully (no request at all), an empty value hands it back to the built-in provider, and a
// scheme-less custom endpoint still self-heals. White-box past the sentinel case, because dialling
// the built-in provider would leave the test hermetic-in-name-only.
func TestWebSearchSetEndpointResolvesLikeConstruction(t *testing.T) {
	t.Parallel()
	backend := httptest.NewServer(searchBackend("RESULTS"))
	defer backend.Close()

	tool := NewWebSearch(loopbackGuard(), backend.URL+"/search")

	tool.SetEndpoint("off")
	got := searchContent(t, tool)
	if !strings.Contains(got, "disabled") {
		t.Errorf("a tool switched off answered %q; want the graceful disabled result", got)
	}
	if strings.Contains(got, "RESULTS") {
		t.Fatal("a disabled tool still reached its old endpoint")
	}

	tool.SetEndpoint("")
	if tool.endpoint != defaultSearchEndpoint || tool.provider != providerDuckDuckGo || tool.disabled {
		t.Errorf("cleared endpoint resolved to (%q, %v, disabled=%v), want the built-in DuckDuckGo default",
			tool.endpoint, tool.provider, tool.disabled)
	}

	tool.SetEndpoint("search.example.com/s")
	if tool.endpoint != "https://search.example.com/s" || tool.provider != providerCustom {
		t.Errorf("scheme-less endpoint resolved to (%q, %v), want the healed custom endpoint",
			tool.endpoint, tool.provider)
	}
}

// A tool the host never re-points behaves exactly as it did before it could be re-pointed: this is
// the floor SetEndpoint must not cost anything (the mutex is on the read path of every Execute).
func TestWebSearchUnmovedEndpointStillAnswers(t *testing.T) {
	t.Parallel()
	backend := httptest.NewServer(searchBackend("STEADY RESULTS"))
	defer backend.Close()

	tool := NewWebSearch(security.URLGuard{}.DisableIPFloor(), backend.URL+"/search")
	if got := searchContent(t, tool); !strings.Contains(got, "STEADY RESULTS") {
		t.Errorf("web_search answered %q, want the configured backend's results", got)
	}
}

// searchBackend is a custom search backend: it answers the `q` GET parameter with a text document,
// which web_search passes through verbatim (no HTML cleaning), so the body IS the assertion.
func searchBackend(body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(body + " for " + r.URL.Query().Get("q")))
	})
}

// searchContent runs one search and returns what the model would see, failing on the Go errors that
// only ctx cancellation produces (ADR 0007: everything else is a result).
func searchContent(t *testing.T, tool *WebSearch) string {
	t.Helper()
	res, err := tool.Execute(context.Background(), domain.ToolCall{
		ID: "c1", Tool: "web_search", Arguments: jsonArgs(t, map[string]any{"query": "needle"}),
	})
	if err != nil {
		t.Fatalf("web_search Execute: %v", err)
	}
	return res.Content
}

// TestCleanHTMLText_FragmentFormIsUnchanged pins the fragment form byte-for-byte: it is the
// form web_search's titles, snippets and generic-page fallback go through, and the hoist of
// the cleaner beside web_fetch's page form (htmltext.go) must not move a byte of it — tags
// become spaces, entities decode, whitespace collapses to ONE line, and a script body is
// text like any other (the page form, not this one, drops it).
func TestCleanHTMLText_FragmentFormIsUnchanged(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"tags become spaces and whitespace collapses", "<html><body>\n  <h1>Results</h1>\n  <p>alpha &amp;\n  beta</p>\n</body></html>", "Results alpha & beta"},
		{"a script body survives", "<p>one</p><script>x()</script><style>b{}</style>", "one x() b{}"},
		{"nested inline markup joins with single spaces", "<b>Go</b>&nbsp;<i>Docs</i> &lt;tag&gt;", "Go Docs <tag>"},
		{"empty", "<br/>", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := cleanHTMLText(tc.in, htmlFragment); got != tc.want {
				t.Errorf("cleanHTMLText(%q, htmlFragment) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
