package probe_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/airiclenz/apogee/internal/probe"
	"github.com/airiclenz/apogee/internal/provider"
)

// authRecorder collects the Authorization header of every request a fake Upstream receives.
// The httptest handler runs on the server's own goroutines, so the slice is guarded — the
// assertions read it after the probe has returned.
type authRecorder struct {
	mu      sync.Mutex
	headers []string
	present []bool
}

func (a *authRecorder) record(r *http.Request) {
	_, ok := r.Header["Authorization"]
	a.mu.Lock()
	defer a.mu.Unlock()
	a.headers = append(a.headers, r.Header.Get("Authorization"))
	a.present = append(a.present, ok)
}

// keyedUpstream serves the two paths a discovery reads and records what each request
// authenticated with.
func keyedUpstream(t *testing.T, rec *authRecorder) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.record(r)
		switch r.URL.Path {
		case "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, openAIModels)
		case "/props":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, llamaCppProps)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// The report's probe authenticates exactly as a session would: a keyed server rejects an
// unauthenticated GET /v1/models, so a probe without the key would diagnose a 401 the binary
// itself would never hit. BOTH discovery requests carry it — /props is keyed too.
func TestDiscoverSendsAPIKey(t *testing.T) {
	t.Parallel()
	rec := &authRecorder{}
	srv := keyedUpstream(t, rec)

	d := probe.Discover(context.Background(), srv.URL, "tok", provider.WireOpenAI)

	if !d.Reached {
		t.Fatalf("Reached = false against a keyed server (Failure = %q)", d.Failure)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.headers) < 2 {
		t.Fatalf("the probe made %d request(s), want both /v1/models and /props", len(rec.headers))
	}
	for i, got := range rec.headers {
		if got != "Bearer tok" {
			t.Errorf("request %d carried Authorization %q, want %q", i, got, "Bearer tok")
		}
	}
}

// The keyless local server stays exactly as it was: an empty key sends no Authorization header
// at all, not an empty one a strict server could reject.
func TestDiscoverWithoutAPIKeySendsNoAuthHeader(t *testing.T) {
	t.Parallel()
	rec := &authRecorder{}
	srv := keyedUpstream(t, rec)

	if d := probe.Discover(context.Background(), srv.URL, "", provider.WireOpenAI); !d.Reached {
		t.Fatalf("Reached = false (Failure = %q)", d.Failure)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.present) == 0 {
		t.Fatal("the probe made no request at all")
	}
	for i, ok := range rec.present {
		if ok {
			t.Errorf("request %d carried an Authorization header on an empty api key", i)
		}
	}
}

// headerRecorder collects, per request, the path and the headers a wire's discovery is told apart
// by — guarded like authRecorder, for the same reason.
type headerRecorder struct {
	mu       sync.Mutex
	paths    []string
	xAPIKeys []string
	versions []string
	bearers  []string
}

func (h *headerRecorder) record(r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.paths = append(h.paths, r.URL.Path)
	h.xAPIKeys = append(h.xAPIKeys, r.Header.Get("x-api-key"))
	h.versions = append(h.versions, r.Header.Get("anthropic-version"))
	h.bearers = append(h.bearers, r.Header.Get("Authorization"))
}

// anthropicUpstream serves the Messages API's model list and records every request; /props is
// served too, so a probe that asked for it would be caught.
func anthropicUpstream(t *testing.T, rec *headerRecorder) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.record(r)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/models":
			_, _ = io.WriteString(w, `{"data":[{"type":"model","id":"claude-x","display_name":"Claude X"}],"has_more":false}`)
		case "/props":
			_, _ = io.WriteString(w, llamaCppProps)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// `apogee probe` dials with the entry's wire, exactly as a session's Monitor does: an anthropic
// entry's discovery carries the Messages API's headers — `x-api-key`, `anthropic-version`, no
// bearer token — and asks for no /props, which nothing on that wire serves. The report then names
// the header the key travelled in and says the /props probe was not made, rather than reporting
// an outcome for a probe that never ran.
func TestDiscoverOnTheAnthropicWireCarriesItsHeadersAndSkipsProps(t *testing.T) {
	t.Parallel()
	rec := &headerRecorder{}
	srv := anthropicUpstream(t, rec)

	host := probe.GatherHost(context.Background(), probe.Inputs{
		Endpoint: srv.URL, APIKey: "tok", Wire: provider.WireAnthropic,
	})

	d := host.Discovery
	if !d.Reached || d.ActiveModel != "claude-x" || d.Wire != provider.WireAnthropic {
		t.Fatalf("Discovery = %+v, want reached, claude-x active, on the anthropic wire", d)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.paths) != 1 || rec.paths[0] != "/v1/models" {
		t.Fatalf("the probe asked %v, want exactly one GET /v1/models and no /props", rec.paths)
	}
	if rec.xAPIKeys[0] != "tok" || rec.versions[0] == "" || rec.bearers[0] != "" {
		t.Errorf("x-api-key=%q anthropic-version=%q Authorization=%q; want the anthropic headers and no bearer",
			rec.xAPIKeys[0], rec.versions[0], rec.bearers[0])
	}
	report := host.Report()
	for _, want := range []string{
		"api key:       configured (sent as x-api-key)",
		"/props:        not probed (the anthropic wire has no /props",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("report lacks %q:\n%s", want, report)
		}
	}
	if strings.Contains(report, "bearer token") {
		t.Errorf("report claims a bearer token on the anthropic wire:\n%s", report)
	}
}
