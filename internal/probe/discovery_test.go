package probe_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/airiclenz/apogee/internal/probe"
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

	d := probe.Discover(context.Background(), srv.URL, "tok")

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

	if d := probe.Discover(context.Background(), srv.URL, ""); !d.Reached {
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
