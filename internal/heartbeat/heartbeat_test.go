package heartbeat

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
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

	beat := NewMonitor(srv.URL, "").Beat(context.Background())

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

	beat := NewMonitor(srv.URL, "large").Beat(context.Background())

	// The config hint reaches discovery, so the ACTIVE model is the pinned one and the window
	// is its own — not the first advertised model's.
	if beat.ActiveModel != "large" || beat.ContextWindow != 128000 {
		t.Errorf("active = %q ctx = %d, want large / 128000", beat.ActiveModel, beat.ContextWindow)
	}
}

func TestBeatHintVanishedFallsBack(t *testing.T) {
	t.Parallel()

	srv := discoveryServer(t, `{"data":[{"id":"served","context_length":4096},{"id":"other","context_length":8192}]}`, "")

	beat := NewMonitor(srv.URL, "unloaded").Beat(context.Background())

	// The pin is a hint, not a claim: once the server stops advertising it, the beat follows
	// observed reality — the first model the server lists.
	if beat.ActiveModel != "served" || beat.ContextWindow != 4096 {
		t.Errorf("active = %q ctx = %d, want served / 4096", beat.ActiveModel, beat.ContextWindow)
	}
}

func TestBeatUnreachableIsObservation(t *testing.T) {
	t.Parallel()

	srv := discoveryServer(t, `{"data":[{"id":"model-a"}]}`, "")
	endpoint := srv.URL
	srv.Close() // nothing listens on that port any more

	beat := NewMonitor(endpoint, "").Beat(context.Background())

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
