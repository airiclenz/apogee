package heartbeat

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// discoveryServer serves the two paths one beat reads — GET /v1/models and llama.cpp's GET
// /props — from canned payloads, and 404s everything else. An empty propsPayload makes /props
// 404 too, which is the bare-OpenAI-shaped server that reports no runtime window. Shape copied
// from internal/provider/discovery_test.go.
func discoveryServer(t *testing.T, modelsPayload, propsPayload string) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_, _ = io.WriteString(w, modelsPayload)
		case "/props":
			if propsPayload == "" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = io.WriteString(w, propsPayload)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestBeatCarriesDiscovery(t *testing.T) {
	t.Parallel()

	srv := discoveryServer(t,
		`{"data":[{"id":"model-a","name":"Model A","context_length":32768},{"id":"model-b","name":"Model B","context_length":8192}]}`,
		`{"default_generation_settings":{"n_ctx":16384}}`)

	beat := NewMonitor(srv.URL, "", "").Beat(context.Background())

	if !beat.Reachable || beat.Failure != "" {
		t.Fatalf("Reachable = %v Failure = %q, want true / \"\"", beat.Reachable, beat.Failure)
	}
	if beat.ActiveModel != "model-a" {
		t.Errorf("ActiveModel = %q, want model-a", beat.ActiveModel)
	}
	// /props wins over the advertised 32768 — the runtime window the server was launched with.
	if beat.ContextWindow != 16384 {
		t.Errorf("ContextWindow = %d, want 16384 (the /props runtime window)", beat.ContextWindow)
	}
	if len(beat.AvailableModels) != 2 {
		t.Fatalf("AvailableModels = %d, want 2", len(beat.AvailableModels))
	}
	if got := beat.AvailableModels[0]; got.ID != "model-a" || got.DisplayName != "Model A" || got.ContextWindow != 16384 {
		t.Errorf("AvailableModels[0] = %+v, want {model-a Model A 16384}", got)
	}
	if got := beat.AvailableModels[1]; got.ID != "model-b" || got.DisplayName != "Model B" || got.ContextWindow != 8192 {
		t.Errorf("AvailableModels[1] = %+v, want {model-b Model B 8192}", got)
	}
}

func TestBeatHintPinsActiveWindow(t *testing.T) {
	t.Parallel()

	srv := discoveryServer(t, `{"data":[{"id":"small","context_length":4096},{"id":"large","context_length":128000}]}`, "")

	beat := NewMonitor(srv.URL, "large", "").Beat(context.Background())

	// The config hint reaches discovery, so the ACTIVE model is the pinned one and the window
	// is its own — not the first advertised model's.
	if beat.ActiveModel != "large" || beat.ContextWindow != 128000 {
		t.Errorf("active = %q ctx = %d, want large / 128000", beat.ActiveModel, beat.ContextWindow)
	}
}

func TestBeatHintVanishedFallsBack(t *testing.T) {
	t.Parallel()

	srv := discoveryServer(t, `{"data":[{"id":"served","context_length":4096},{"id":"other","context_length":8192}]}`, "")

	beat := NewMonitor(srv.URL, "unloaded", "").Beat(context.Background())

	// The pin is a hint, not a claim: once the server stops advertising it, the beat follows
	// observed reality — the first model the server lists.
	if beat.ActiveModel != "served" || beat.ContextWindow != 4096 {
		t.Errorf("active = %q ctx = %d, want served / 4096", beat.ActiveModel, beat.ContextWindow)
	}
}

// The hint follows the BINDING: once a rebind commits, the host re-states it, and the very next
// beat must resolve the newly bound model — otherwise a server that still serves the old id would
// keep reporting it as active and the orchestration would bind straight back, flapping the session
// off whatever the user just picked.
func TestSetModelMovesTheDiscoveryHint(t *testing.T) {
	t.Parallel()

	// No /props payload, so nothing overrides the advertised windows: the window the beat reports
	// is the hinted model's own, which is exactly what has to move with the hint.
	srv := discoveryServer(t, `{"data":[{"id":"model-a","context_length":32768},{"id":"model-b","context_length":8192}]}`, "")

	monitor := NewMonitor(srv.URL, "model-a", "")

	if beat := monitor.Beat(context.Background()); beat.ActiveModel != "model-a" || beat.ContextWindow != 32768 {
		t.Fatalf("first beat: active = %q ctx = %d, want model-a / 32768", beat.ActiveModel, beat.ContextWindow)
	}

	monitor.SetModel("model-b")

	// model-a is still served and still listed first, so only the moved hint can explain this
	// answer — the hint wins over the advertisement order.
	if beat := monitor.Beat(context.Background()); beat.ActiveModel != "model-b" || beat.ContextWindow != 8192 {
		t.Errorf("beat after SetModel: active = %q ctx = %d, want model-b / 8192", beat.ActiveModel, beat.ContextWindow)
	}
}

// A keyed server answers /v1/models with 401 just as readily as it answers a chat call, so the
// monitor's own client must carry the bearer token: without it the footer would report the
// Upstream permanently unreachable under a session that is talking to it perfectly well.
func TestMonitorSendsAPIKey(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var authorizations []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		authorizations = append(authorizations, r.Header.Get("Authorization"))
		mu.Unlock()
		if r.URL.Path != "/v1/models" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = io.WriteString(w, `{"data":[{"id":"model-a","context_length":4096}]}`)
	}))
	t.Cleanup(srv.Close)

	beat := NewMonitor(srv.URL, "", "tok").Beat(context.Background())

	if !beat.Reachable {
		t.Fatalf("Reachable = false against a keyed server (Failure = %q)", beat.Failure)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(authorizations) == 0 {
		t.Fatal("the beat sent no request at all")
	}
	for i, got := range authorizations {
		if got != "Bearer tok" {
			t.Errorf("request %d carried Authorization %q, want %q", i, got, "Bearer tok")
		}
	}
}

// The keyless local server — the overwhelmingly common case — must stay byte-identical to what
// it was before the key existed: an empty key sends no Authorization header at all, rather than
// an empty or "Bearer " one a strict server could reject.
func TestMonitorWithoutAPIKeySendsNoAuthHeader(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var authorized bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := r.Header["Authorization"]; ok {
			mu.Lock()
			authorized = true
			mu.Unlock()
		}
		if r.URL.Path != "/v1/models" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = io.WriteString(w, `{"data":[{"id":"model-a","context_length":4096}]}`)
	}))
	t.Cleanup(srv.Close)

	if beat := NewMonitor(srv.URL, "", "").Beat(context.Background()); !beat.Reachable {
		t.Fatalf("Reachable = false (Failure = %q)", beat.Failure)
	}
	mu.Lock()
	defer mu.Unlock()
	if authorized {
		t.Error("an empty api key still sent an Authorization header")
	}
}

func TestBeatUnreachableIsObservation(t *testing.T) {
	t.Parallel()

	srv := discoveryServer(t, `{"data":[{"id":"model-a"}]}`, "")
	endpoint := srv.URL
	srv.Close() // nothing listens on that port any more

	beat := NewMonitor(endpoint, "", "").Beat(context.Background())

	if beat.Reachable {
		t.Fatalf("Reachable = true against a closed listener, want false")
	}
	if beat.Failure == "" {
		t.Error("Failure is empty, want the reason the server could not be read")
	}
	if beat.ActiveModel != "" || beat.ContextWindow != 0 || len(beat.AvailableModels) != 0 {
		t.Errorf("unreachable beat carries findings: %+v", beat)
	}
}
