package agent

// The engine seam of the upstream API key: Config.APIKey must reach the session's provider
// client, so a keyed server (llama.cpp --api-key, LM Studio, a remote proxy) authenticates
// every request the loop makes. internal/provider already proves WithAPIKey sets and redacts
// the bearer token; these tests pin that New and Resume actually PASS it — the wire that was
// missing — and that an empty key still sends no Authorization header at all.

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/stubllm"
)

// keyedUpstream starts a stubllm upstream that REQUIRES `Authorization: Bearer key`: a request
// without it is answered 401 before the stub's request log sees it, so the log holding exactly
// the loop's request is the proof the key travelled. It answers "authed" to every request.
func keyedUpstream(t *testing.T, key string) *stubllm.Server {
	t.Helper()
	return stubllm.New(t, stubllm.Script{Turns: []stubllm.Turn{{Text: "authed", Repeat: true}}}, stubllm.WithAPIKey(key))
}

// authRecorder records the Authorization header of every request before handing it to a
// stubllm upstream — the half of the empty-key contract a keyless stub cannot assert on its
// own (it accepts any header and logs none). The mutex is load-bearing: the handler runs on
// the server's goroutine while the test goroutine drives Step and reads the recording
// afterwards.
type authRecorder struct {
	mu      sync.Mutex
	value   string // the header's value ("" when absent)
	present bool   // whether the request carried an Authorization header at all
	calls   int
}

// record is the header-recording middleware in front of the stub's handler.
func (rec *authRecorder) record(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.mu.Lock()
		rec.value = r.Header.Get("Authorization")
		_, rec.present = r.Header["Authorization"]
		rec.calls++
		rec.mu.Unlock()
		next.ServeHTTP(w, r)
	})
}

// observed returns what the recorder saw, under its lock.
func (rec *authRecorder) observed(t *testing.T) (value string, present bool, calls int) {
	t.Helper()
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return rec.value, rec.present, rec.calls
}

// newAuthRecorder starts a recording Upstream — the recorder in front of a keyless stubllm
// script — and returns it with its URL.
func newAuthRecorder(t *testing.T) (*authRecorder, string) {
	t.Helper()
	rec := &authRecorder{}
	stub := stubllm.InProcess(t, stubllm.Script{Turns: []stubllm.Turn{{Text: "authed", Repeat: true}}})
	srv := httptest.NewServer(rec.record(stub.Handler()))
	t.Cleanup(srv.Close)
	return rec, srv.URL
}

// assertAuthenticated reads the keyed stub's request log and the sink for the one proof a
// keyed upstream gives: the loop's request got past the 401 gate into the log, and its reply
// came back as the Turn's message.
func assertAuthenticated(t *testing.T, srv *stubllm.Server, sink *recordingSink) {
	t.Helper()
	if n := len(srv.Requests()); n != 1 {
		t.Fatalf("the keyed Upstream logged %d requests, want the loop's one (a 401 never reaches the log)", n)
	}
	if me, ok := firstMessageEvent(t, sink.events); !ok || me.Text != "authed" {
		t.Errorf("MessageEvent = %+v (ok=%v), want Text=%q", me, ok, "authed")
	}
}

// TestNewWiresAPIKeyToUpstream proves the public New binds the configured key onto the
// session's provider client: the request the loop makes carries `Authorization: Bearer <key>`.
func TestNewWiresAPIKeyToUpstream(t *testing.T) {
	t.Parallel()

	srv := keyedUpstream(t, "tok")
	sink := &recordingSink{}
	cfg := baseConfig(sink)
	cfg.Endpoint = srv.URL
	cfg.APIKey = "tok"

	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	stepOnce(t, a, "hi")

	assertAuthenticated(t, srv, sink)
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

	srv := keyedUpstream(t, "resumed-tok")
	sink := &recordingSink{}
	cfg := baseConfig(sink)
	cfg.Endpoint = srv.URL
	cfg.APIKey = "resumed-tok"

	a, err := Resume(cfg, domain.Session{Version: domain.SessionVersion})
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	stepOnce(t, a, "hi")

	assertAuthenticated(t, srv, sink)
}
