package provider

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// modelsServer serves a canned /v1/models payload and records that request. Any other path
// (notably the best-effort /props probe Discover now also makes) returns 404, so a test that
// only stubs /v1/models exercises the "no runtime window ⇒ keep the models value" path.
func modelsServer(payload string) (*httptest.Server, *recordedRequest) {
	rec := &recordedRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != modelsPath {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		rec.path = r.URL.Path
		rec.auth = r.Header.Get("Authorization")
		_, _ = io.WriteString(w, payload)
	}))
	return srv, rec
}

// discoveryServer serves both /v1/models and /props so a test can exercise the runtime
// context-window override. An empty propsPayload makes /props return 404 (a non-llama.cpp
// server). It records the auth header /props saw.
func discoveryServer(modelsPayload, propsPayload string) (*httptest.Server, *discoveryRecord) {
	rec := &discoveryRecord{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case modelsPath:
			_, _ = io.WriteString(w, modelsPayload)
		case propsPath:
			rec.sawProps = true
			rec.propsAuth = r.Header.Get("Authorization")
			if propsPayload == "" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = io.WriteString(w, propsPayload)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	return srv, rec
}

type recordedRequest struct {
	path string
	auth string
}

type discoveryRecord struct {
	sawProps  bool
	propsAuth string
}

func TestDiscover_ParsesModels(t *testing.T) {
	t.Parallel()

	srv, rec := modelsServer(`{"data":[{"id":"model-a","context_length":32768},{"id":"model-b","context_length":8192}]}`)
	defer srv.Close()

	info, err := NewClient(srv.URL, "").Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(info.AvailableModels) != 2 {
		t.Fatalf("AvailableModels = %d, want 2", len(info.AvailableModels))
	}
	if info.ActiveModel != "model-a" || info.ContextWindow != 32768 {
		t.Errorf("active = %q ctx = %d, want model-a / 32768", info.ActiveModel, info.ContextWindow)
	}
	if rec.path != modelsPath {
		t.Errorf("discovery hit %q, want %q", rec.path, modelsPath)
	}
}

func TestDiscover_HintedActiveModel(t *testing.T) {
	t.Parallel()

	srv, _ := modelsServer(`{"data":[{"id":"small","context_length":4096},{"id":"large","context_length":128000}]}`)
	defer srv.Close()

	// The client's configured model is the discovery hint.
	info, err := NewClient(srv.URL, "large").Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if info.ActiveModel != "large" || info.ContextWindow != 128000 {
		t.Errorf("active = %q ctx = %d, want large / 128000", info.ActiveModel, info.ContextWindow)
	}
}

func TestDiscover_ContextWindowFallbacks(t *testing.T) {
	t.Parallel()

	t.Run("missing context window is zero", func(t *testing.T) {
		t.Parallel()
		srv, _ := modelsServer(`{"data":[{"id":"mystery"}]}`)
		defer srv.Close()

		info, err := NewClient(srv.URL, "").Discover(context.Background())
		if err != nil {
			t.Fatalf("Discover: %v", err)
		}
		if info.AvailableModels[0].ContextWindow != 0 || info.ContextWindow != 0 {
			t.Errorf("context window = %d, want 0 (unknown)", info.ContextWindow)
		}
	})

	t.Run("meta.n_ctx_train fallback", func(t *testing.T) {
		t.Parallel()
		srv, _ := modelsServer(`{"data":[{"id":"gemma","meta":{"n_ctx_train":131072}}]}`)
		defer srv.Close()

		info, err := NewClient(srv.URL, "").Discover(context.Background())
		if err != nil {
			t.Fatalf("Discover: %v", err)
		}
		if info.ContextWindow != 131072 {
			t.Errorf("context window = %d, want 131072 from meta.n_ctx_train", info.ContextWindow)
		}
	})
}

func TestDiscover_PropsRuntimeContextWindowOverrides(t *testing.T) {
	t.Parallel()

	// /v1/models advertises the model's *training* window (n_ctx_train = 131072); /props
	// reports the *runtime* window the server was actually launched with (8192). The runtime
	// value wins, and it propagates to the active model's AvailableModels entry.
	srv, rec := discoveryServer(
		`{"data":[{"id":"gemma","meta":{"n_ctx_train":131072}}]}`,
		`{"default_generation_settings":{"n_ctx":8192}}`,
	)
	defer srv.Close()

	info, err := NewClient(srv.URL, "").Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if !rec.sawProps {
		t.Error("Discover did not probe /props")
	}
	if info.ContextWindow != 8192 {
		t.Errorf("ContextWindow = %d, want 8192 (runtime /props overrides n_ctx_train)", info.ContextWindow)
	}
	if info.AvailableModels[0].ContextWindow != 8192 {
		t.Errorf("active model ContextWindow = %d, want 8192", info.AvailableModels[0].ContextWindow)
	}
}

func TestDiscover_PropsOverridesContextLength(t *testing.T) {
	t.Parallel()

	// Even an explicit context_length from /v1/models yields to the runtime /props value.
	srv, _ := discoveryServer(
		`{"data":[{"id":"m","context_length":32768}]}`,
		`{"default_generation_settings":{"n_ctx":4096}}`,
	)
	defer srv.Close()

	info, err := NewClient(srv.URL, "").Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if info.ContextWindow != 4096 {
		t.Errorf("ContextWindow = %d, want 4096 (runtime /props overrides context_length)", info.ContextWindow)
	}
}

func TestDiscover_NoRuntimeWindowKeepsModelsValue(t *testing.T) {
	t.Parallel()

	t.Run("props 404 (non-llama.cpp server)", func(t *testing.T) {
		t.Parallel()
		srv, _ := discoveryServer(`{"data":[{"id":"m","context_length":32768}]}`, "")
		defer srv.Close()

		info, err := NewClient(srv.URL, "").Discover(context.Background())
		if err != nil {
			t.Fatalf("Discover: %v", err)
		}
		if info.ContextWindow != 32768 {
			t.Errorf("ContextWindow = %d, want 32768 (no /props ⇒ keep models value)", info.ContextWindow)
		}
	})

	t.Run("non-positive n_ctx ignored", func(t *testing.T) {
		t.Parallel()
		srv, _ := discoveryServer(
			`{"data":[{"id":"m","context_length":32768}]}`,
			`{"default_generation_settings":{"n_ctx":0}}`,
		)
		defer srv.Close()

		info, err := NewClient(srv.URL, "").Discover(context.Background())
		if err != nil {
			t.Fatalf("Discover: %v", err)
		}
		if info.ContextWindow != 32768 {
			t.Errorf("ContextWindow = %d, want 32768 (n_ctx<=0 ignored)", info.ContextWindow)
		}
	})

	t.Run("missing default_generation_settings ignored", func(t *testing.T) {
		t.Parallel()
		srv, _ := discoveryServer(
			`{"data":[{"id":"m","context_length":32768}]}`,
			`{"total_slots":1,"model_path":"/m.gguf"}`,
		)
		defer srv.Close()

		info, err := NewClient(srv.URL, "").Discover(context.Background())
		if err != nil {
			t.Fatalf("Discover: %v", err)
		}
		if info.ContextWindow != 32768 {
			t.Errorf("ContextWindow = %d, want 32768 (no n_ctx field ⇒ keep models value)", info.ContextWindow)
		}
	})
}

func TestDiscover_PropsProbeSendsAuth(t *testing.T) {
	t.Parallel()

	srv, rec := discoveryServer(
		`{"data":[{"id":"m"}]}`,
		`{"default_generation_settings":{"n_ctx":4096}}`,
	)
	defer srv.Close()

	if _, err := NewClient(srv.URL, "", WithAPIKey("tok")).Discover(context.Background()); err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if rec.propsAuth != "Bearer tok" {
		t.Errorf("/props Authorization = %q, want %q", rec.propsAuth, "Bearer tok")
	}
}

func TestDiscover_Errors(t *testing.T) {
	t.Parallel()

	t.Run("http error", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		if _, err := NewClient(srv.URL, "").Discover(context.Background()); err == nil {
			t.Fatal("Discover succeeded on HTTP 500, want error")
		}
	})

	t.Run("empty model list", func(t *testing.T) {
		t.Parallel()
		srv, _ := modelsServer(`{"data":[]}`)
		defer srv.Close()

		if _, err := NewClient(srv.URL, "").Discover(context.Background()); err == nil {
			t.Fatal("Discover succeeded on empty data, want error")
		}
	})
}

func TestDiscover_SendsAuth(t *testing.T) {
	t.Parallel()

	srv, rec := modelsServer(`{"data":[{"id":"m"}]}`)
	defer srv.Close()

	if _, err := NewClient(srv.URL, "", WithAPIKey("tok")).Discover(context.Background()); err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if rec.auth != "Bearer tok" {
		t.Errorf("Authorization = %q, want %q", rec.auth, "Bearer tok")
	}
}
