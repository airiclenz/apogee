package agent

// The engine seam of the upstream API key: Config.APIKey must reach the session's provider
// client, so a keyed server (llama.cpp --api-key, LM Studio, a remote proxy) authenticates
// every request the loop makes. internal/provider already proves WithAPIKey sets and redacts
// the bearer token; these tests pin that New and Resume actually PASS it — the wire that was
// missing — and that an empty key still sends no Authorization header at all.

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// authRecorder is a hermetic Upstream that records the Authorization header of every request
// and answers with a minimal streamed completion (the loop streams — the §6 #6 path). The
// mutex is load-bearing: the handler runs on the server's goroutine while the test goroutine
// drives Step and reads the recording afterwards.
type authRecorder struct {
	mu      sync.Mutex
	value   string // the header's value ("" when absent)
	present bool   // whether the request carried an Authorization header at all
	calls   int
}

func (rec *authRecorder) serve(w http.ResponseWriter, r *http.Request) {
	rec.mu.Lock()
	rec.value = r.Header.Get("Authorization")
	_, rec.present = r.Header["Authorization"]
	rec.calls++
	rec.mu.Unlock()

	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"authed\"},\"finish_reason\":null}]}\n\n")
	_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
}

// observed returns what the recorder saw, under its lock.
func (rec *authRecorder) observed(t *testing.T) (value string, present bool, calls int) {
	t.Helper()
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return rec.value, rec.present, rec.calls
}

// newAuthRecorder starts a recording Upstream and returns it with its URL.
func newAuthRecorder(t *testing.T) (*authRecorder, string) {
	t.Helper()
	rec := &authRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(rec.serve))
	t.Cleanup(srv.Close)
	return rec, srv.URL
}

// TestNewWiresAPIKeyToUpstream proves the public New binds the configured key onto the
// session's provider client: the request the loop makes carries `Authorization: Bearer <key>`.
func TestNewWiresAPIKeyToUpstream(t *testing.T) {
	t.Parallel()

	rec, url := newAuthRecorder(t)

	cfg := baseConfig(&recordingSink{})
	cfg.Endpoint = url
	cfg.APIKey = "tok"

	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	stepOnce(t, a, "hi")

	value, _, calls := rec.observed(t)
	if calls == 0 {
		t.Fatal("the Upstream was never called")
	}
	if value != "Bearer tok" {
		t.Errorf("Authorization = %q, want %q", value, "Bearer tok")
	}
}

// TestNewWithoutAPIKeySendsNoAuthHeader pins the empty-key contract at the ENGINE seam (not
// just inside internal/provider): the option is passed unconditionally, and an empty key adds
// no Authorization header — a keyless local server sees byte-identical requests.
func TestNewWithoutAPIKeySendsNoAuthHeader(t *testing.T) {
	t.Parallel()

	rec, url := newAuthRecorder(t)

	cfg := baseConfig(&recordingSink{})
	cfg.Endpoint = url // cfg.APIKey stays empty

	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	stepOnce(t, a, "hi")

	value, present, calls := rec.observed(t)
	if calls == 0 {
		t.Fatal("the Upstream was never called")
	}
	if present {
		t.Errorf("request carried an Authorization header (%q) for an empty key, want none", value)
	}
}

// TestResumeWiresAPIKeyToUpstream proves the resumed session authenticates too — a snapshot
// carries no key (it never holds the secret), so Resume must take it from the live Config.
func TestResumeWiresAPIKeyToUpstream(t *testing.T) {
	t.Parallel()

	rec, url := newAuthRecorder(t)

	cfg := baseConfig(&recordingSink{})
	cfg.Endpoint = url
	cfg.APIKey = "resumed-tok"

	a, err := Resume(cfg, domain.Session{Version: domain.SessionVersion})
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	stepOnce(t, a, "hi")

	value, _, calls := rec.observed(t)
	if calls == 0 {
		t.Fatal("the Upstream was never called")
	}
	if value != "Bearer resumed-tok" {
		t.Errorf("Authorization = %q, want %q", value, "Bearer resumed-tok")
	}
}
