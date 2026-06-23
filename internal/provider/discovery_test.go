package provider

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// modelsServer serves a canned /v1/models payload and records the request it saw.
func modelsServer(payload string) (*httptest.Server, *recordedRequest) {
	rec := &recordedRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.path = r.URL.Path
		rec.auth = r.Header.Get("Authorization")
		_, _ = io.WriteString(w, payload)
	}))
	return srv, rec
}

type recordedRequest struct {
	path string
	auth string
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
