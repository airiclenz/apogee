package provider

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
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

// A configured model id is trusted rather than silently swapped for models[0]: it is always the
// active model verbatim, and the advertised list only supplies its context window — exactly, via
// the base slug of a variant like ":exacto", or not at all.
func TestDiscover_HintResolution(t *testing.T) {
	t.Parallel()

	const twoModels = `{"data":[{"id":"deepseek/deepseek-v4-pro","context_length":163840},{"id":"small","context_length":4096}]}`

	tests := []struct {
		name       string
		payload    string
		hint       string
		wantActive string
		wantWindow int
		wantGrade  HintResolution
	}{
		{
			name:       "exact match",
			payload:    twoModels,
			hint:       "small",
			wantActive: "small",
			wantWindow: 4096,
			wantGrade:  HintExact,
		},
		{
			name:       "variant suffix inherits the base window",
			payload:    twoModels,
			hint:       "deepseek/deepseek-v4-pro:exacto",
			wantActive: "deepseek/deepseek-v4-pro:exacto",
			wantWindow: 163840,
			wantGrade:  HintBaseSlug,
		},
		{
			name:       "unlisted hint is trusted with an unknown window",
			payload:    twoModels,
			hint:       "my-alias",
			wantActive: "my-alias",
			wantWindow: 0,
			wantGrade:  HintTrusted,
		},
		{
			name:       "unlisted variant whose base is also unlisted is trusted",
			payload:    twoModels,
			hint:       "vendor/other:exacto",
			wantActive: "vendor/other:exacto",
			wantWindow: 0,
			wantGrade:  HintTrusted,
		},
		{
			name:       "empty advertised list still runs the hint",
			payload:    `{"data":[]}`,
			hint:       "my-alias",
			wantActive: "my-alias",
			wantWindow: 0,
			wantGrade:  HintTrusted,
		},
		{
			name:       "empty hint falls back to the first advertised",
			payload:    twoModels,
			hint:       "",
			wantActive: "deepseek/deepseek-v4-pro",
			wantWindow: 163840,
			wantGrade:  HintFirstAdvertised,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv, _ := modelsServer(tt.payload)
			defer srv.Close()

			info, err := NewClient(srv.URL, tt.hint).Discover(context.Background())
			if err != nil {
				t.Fatalf("Discover: %v", err)
			}
			if info.ActiveModel != tt.wantActive {
				t.Errorf("ActiveModel = %q, want %q", info.ActiveModel, tt.wantActive)
			}
			if info.ContextWindow != tt.wantWindow {
				t.Errorf("ContextWindow = %d, want %d", info.ContextWindow, tt.wantWindow)
			}
			if info.Resolution != tt.wantGrade {
				t.Errorf("Resolution = %q, want %q", info.Resolution, tt.wantGrade)
			}
		})
	}
}

func TestDiscover_PropsWindowLandsOnATrustedActiveModel(t *testing.T) {
	t.Parallel()

	// The runtime window is a property of the server, not of the advertised entry, so it
	// reaches a trusted active model too. The AvailableModels sync finds no matching entry
	// and is left alone — the advertised model keeps its own advertised window.
	srv, _ := discoveryServer(
		`{"data":[{"id":"served-under-another-name","context_length":32768}]}`,
		`{"default_generation_settings":{"n_ctx":8192}}`,
	)
	defer srv.Close()

	info, err := NewClient(srv.URL, "my-alias").Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if info.ActiveModel != "my-alias" || info.Resolution != HintTrusted {
		t.Fatalf("active = %q grade = %q, want my-alias / %q", info.ActiveModel, info.Resolution, HintTrusted)
	}
	if info.ContextWindow != 8192 || info.RuntimeContextWindow != 8192 {
		t.Errorf("ContextWindow = %d RuntimeContextWindow = %d, want 8192 / 8192", info.ContextWindow, info.RuntimeContextWindow)
	}
	if info.AvailableModels[0].ContextWindow != 32768 {
		t.Errorf("advertised entry ContextWindow = %d, want 32768 (no list sync for an unadvertised active model)", info.AvailableModels[0].ContextWindow)
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
		// The same response's OTHER field is now read: a payload with no window still tells us how
		// many slots the server was launched with, and the two are resolved independently.
		if info.TotalSlots != 1 {
			t.Errorf("TotalSlots = %d, want 1 (total_slots is parsed even with no n_ctx)", info.TotalSlots)
		}
	})
}

// The discovery half of the Parallel agents cap (ADR 0039 decision 2): `/props` reports the
// `--parallel N` width the server was launched with, and a server that does not report it leaves the
// number UNKNOWN — 0, never a guessed 1 — because the fallback to serial belongs to whoever resolves
// the cap, not to the probe.
func TestDiscover_TotalSlots(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		props string
		want  int
	}{
		{
			name:  "reported beside the runtime window",
			props: `{"default_generation_settings":{"n_ctx":8192},"total_slots":4}`,
			want:  4,
		},
		{
			name:  "absent leaves it unknown",
			props: `{"default_generation_settings":{"n_ctx":8192}}`,
			want:  0,
		},
		{
			name:  "nonsense is not a width",
			props: `{"default_generation_settings":{"n_ctx":8192},"total_slots":0}`,
			want:  0,
		},
		{
			name:  "no /props at all",
			props: "",
			want:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv, _ := discoveryServer(`{"data":[{"id":"m","context_length":32768}]}`, tt.props)
			defer srv.Close()

			info, err := NewClient(srv.URL, "").Discover(context.Background())
			if err != nil {
				t.Fatalf("Discover: %v", err)
			}
			if info.TotalSlots != tt.want {
				t.Errorf("TotalSlots = %d, want %d", info.TotalSlots, tt.want)
			}
		})
	}
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

// Effort detection is PASSIVE (ADR 0060): the two payloads discovery already fetches carry the
// tell, and nothing here calls the model. A /props chat template that mentions the dial proves it
// exists but names no vocabulary; a /v1/models `reasoning` object both proves it and states the
// vocabulary; neither is indistinguishable from a model with no dial and stays the zero value.
func TestDiscover_EffortSupport(t *testing.T) {
	t.Parallel()

	const plainModels = `{"data":[{"id":"m","context_length":32768}]}`
	const reasoningModels = `{"data":[{"id":"m","context_length":32768,` +
		`"reasoning":{"supported_efforts":["low","medium","high"],"default_effort":"medium"}}]}`
	// The two shapes below are the payload https://openrouter.ai/api/v1/models actually served on
	// 2026-08-31, verbatim: z-ai/glm-5.3-flash reports reasoning it will not skip, z-ai/glm-5.2
	// reports the same object with the flag false.
	const mandatoryModels = `{"data":[{"id":"z-ai/glm-5.3-flash","context_length":32768,` +
		`"reasoning":{"mandatory":true,"default_enabled":true,` +
		`"supported_efforts":["max","high","low"],"default_effort":"max"}}]}`
	const optionalModels = `{"data":[{"id":"z-ai/glm-5.2","context_length":32768,` +
		`"reasoning":{"mandatory":false,"default_enabled":true,` +
		`"supported_efforts":["max","high","low"],"default_effort":"max"}}]}`

	tests := []struct {
		name   string
		models string
		props  string
		hint   string
		want   EffortSupport
	}{
		{
			name:   "chat template naming reasoning_effort ⇒ kwargs dialect, no vocabulary",
			models: plainModels,
			props:  `{"chat_template":"{% if reasoning_effort == 'high' %}...{% endif %}"}`,
			want:   EffortSupport{Supported: true, Dialect: EffortDialectKwargs},
		},
		{
			name:   "chat template naming enable_thinking ⇒ kwargs dialect",
			models: plainModels,
			props:  `{"chat_template":"{% if enable_thinking %}<think>{% endif %}"}`,
			want:   EffortSupport{Supported: true, Dialect: EffortDialectKwargs},
		},
		{
			name:   "reasoning object on the active model ⇒ reasoning dialect with set and default",
			models: reasoningModels,
			props:  "",
			want: EffortSupport{
				Supported: true,
				Dialect:   EffortDialectReasoning,
				Efforts:   []string{"low", "medium", "high"},
				Default:   "medium",
			},
		},
		{
			name:   "bare reasoning object still means supported",
			models: `{"data":[{"id":"m","context_length":32768,"reasoning":{}}]}`,
			props:  "",
			want:   EffortSupport{Supported: true, Dialect: EffortDialectReasoning},
		},
		{
			name:   "neither tell ⇒ unsupported",
			models: plainModels,
			props:  `{"default_generation_settings":{"n_ctx":8192},"chat_template":"{{ message }}"}`,
			want:   EffortSupport{},
		},
		{
			name:   "no /props and no reasoning object ⇒ unsupported",
			models: plainModels,
			props:  "",
			want:   EffortSupport{},
		},
		{
			name:   "both tells ⇒ the reasoning object wins",
			models: reasoningModels,
			props:  `{"chat_template":"{% if reasoning_effort %}...{% endif %}"}`,
			want: EffortSupport{
				Supported: true,
				Dialect:   EffortDialectReasoning,
				Efforts:   []string{"low", "medium", "high"},
				Default:   "medium",
			},
		},
		{
			name:   "an unlisted routing variant inherits its base slug's answer",
			models: reasoningModels,
			props:  "",
			hint:   "m:exacto",
			want: EffortSupport{
				Supported: true,
				Dialect:   EffortDialectReasoning,
				Efforts:   []string{"low", "medium", "high"},
				Default:   "medium",
			},
		},
		{
			name:   "an unadvertised model reports no dial",
			models: reasoningModels,
			props:  "",
			hint:   "someone-elses-model",
			want:   EffortSupport{},
		},
		{
			name:   "an entry flagging mandatory reasoning ⇒ Mandatory beside the vocabulary",
			models: mandatoryModels,
			props:  "",
			want: EffortSupport{
				Supported: true,
				Dialect:   EffortDialectReasoning,
				Efforts:   []string{"max", "high", "low"},
				Default:   "max",
				Mandatory: true,
			},
		},
		{
			name:   "an entry flagging mandatory false ⇒ the dial can be turned off",
			models: optionalModels,
			props:  "",
			want: EffortSupport{
				Supported: true,
				Dialect:   EffortDialectReasoning,
				Efforts:   []string{"max", "high", "low"},
				Default:   "max",
			},
		},
		{
			name:   "a reasoning object naming no mandatory ⇒ not mandatory",
			models: reasoningModels,
			props:  "",
			want: EffortSupport{
				Supported: true,
				Dialect:   EffortDialectReasoning,
				Efforts:   []string{"low", "medium", "high"},
				Default:   "medium",
			},
		},
		{
			name: "an explicit null mandatory ⇒ not mandatory, vocabulary intact",
			models: `{"data":[{"id":"m","context_length":32768,` +
				`"reasoning":{"supported_efforts":["low","high"],"mandatory":null}}]}`,
			props: "",
			want: EffortSupport{
				Supported: true,
				Dialect:   EffortDialectReasoning,
				Efforts:   []string{"low", "high"},
			},
		},
		{
			name:   "an unlisted routing variant inherits its base slug's mandatory flag",
			models: mandatoryModels,
			props:  "",
			hint:   "z-ai/glm-5.3-flash:exacto",
			want: EffortSupport{
				Supported: true,
				Dialect:   EffortDialectReasoning,
				Efforts:   []string{"max", "high", "low"},
				Default:   "max",
				Mandatory: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv, _ := discoveryServer(tt.models, tt.props)
			defer srv.Close()

			info, err := NewClient(srv.URL, tt.hint).Discover(context.Background())

			if err != nil {
				t.Fatalf("Discover: %v", err)
			}
			if !reflect.DeepEqual(info.EffortSupport, tt.want) {
				t.Errorf("EffortSupport = %+v, want %+v", info.EffortSupport, tt.want)
			}
		})
	}
}

// The dial is a property of the MODEL, so EVERY advertised entry carries its own answer and not
// just the active one: a host deciding what a switch INTO a model implies has to read the target's
// vocabulary before it binds, and the entry is the only place that can state it (ADR 0060 D8).
func TestDiscover_PerModelEffortSupport(t *testing.T) {
	t.Parallel()

	const models = `{"data":[` +
		`{"id":"strict","context_length":32768,` +
		`"reasoning":{"supported_efforts":["low","medium"],"default_effort":"low"}},` +
		`{"id":"wide","context_length":16384,` +
		`"reasoning":{"supported_efforts":["low","medium","high"],"default_effort":"high"}},` +
		`{"id":"plain","context_length":8192}]}`

	srv, _ := discoveryServer(models, "")
	defer srv.Close()

	info, err := NewClient(srv.URL, "strict").Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	strict := EffortSupport{
		Supported: true, Dialect: EffortDialectReasoning,
		Efforts: []string{"low", "medium"}, Default: "low",
	}
	want := []DiscoveredModel{
		{ID: "strict", ContextWindow: 32768, EffortSupport: strict},
		{ID: "wide", ContextWindow: 16384, EffortSupport: EffortSupport{
			Supported: true, Dialect: EffortDialectReasoning,
			Efforts: []string{"low", "medium", "high"}, Default: "high",
		}},
		{ID: "plain", ContextWindow: 8192},
	}
	if !reflect.DeepEqual(info.AvailableModels, want) {
		t.Errorf("AvailableModels = %+v, want each entry carrying its own vocabulary %+v",
			info.AvailableModels, want)
	}
	// The active model is read by the same rule, so the two answers about one model always agree.
	if !reflect.DeepEqual(info.EffortSupport, strict) {
		t.Errorf("EffortSupport = %+v, want the active entry's own answer %+v", info.EffortSupport, strict)
	}
}

// A parse miss is never an error: discovery keeps reporting the model it resolved, and the dial
// simply reads as undetected — the same best-effort contract the window and slot probes hold.
func TestDiscover_MalformedEffortPayloadsStayBestEffort(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		models string
		props  string
	}{
		{
			name:   "reasoning is not an object",
			models: `{"data":[{"id":"m","context_length":32768,"reasoning":"high"}]}`,
			props:  "",
		},
		{
			name:   "reasoning is an explicit null",
			models: `{"data":[{"id":"m","context_length":32768,"reasoning":null}]}`,
			props:  "",
		},
		{
			name:   "chat_template is not a string",
			models: `{"data":[{"id":"m","context_length":32768}]}`,
			props:  `{"default_generation_settings":{"n_ctx":8192},"chat_template":{"jinja":"reasoning_effort"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv, _ := discoveryServer(tt.models, tt.props)
			defer srv.Close()

			info, err := NewClient(srv.URL, "").Discover(context.Background())

			if err != nil {
				t.Fatalf("Discover: %v", err)
			}
			if info.EffortSupport.Supported {
				t.Errorf("EffortSupport = %+v, want the zero value on an unparsable payload", info.EffortSupport)
			}
		})
	}
}

// One unexpected flag must cost the flag and not the dial: `mandatory` is read in a pass of its
// own precisely so that a server writing a word where a boolean belongs still gets its vocabulary
// through. It cannot ride in TestDiscover_MalformedEffortPayloadsStayBestEffort above, whose single
// shared assertion wants the zero value from every row — this payload's whole point is that
// Supported stays true.
func TestDiscover_NonBooleanMandatoryKeepsTheDial(t *testing.T) {
	t.Parallel()

	const models = `{"data":[{"id":"m","context_length":32768,` +
		`"reasoning":{"supported_efforts":["low","high"],"mandatory":"always"}}]}`

	srv, _ := discoveryServer(models, "")
	defer srv.Close()

	info, err := NewClient(srv.URL, "").Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	want := EffortSupport{
		Supported: true,
		Dialect:   EffortDialectReasoning,
		Efforts:   []string{"low", "high"},
	}
	if !reflect.DeepEqual(info.EffortSupport, want) {
		t.Errorf("EffortSupport = %+v, want the vocabulary intact and only the flag lost %+v",
			info.EffortSupport, want)
	}
}

// A server entry that FORCES a dialect outranks both passive tells (ADR 0060 decision 3): the key
// exists for the providers that advertise nothing, so what it says is what discovery reports — the
// wire shape and, with it, the verdict that the dial exists at all. `off` is the one forcing that
// goes the other way, and the detected vocabulary survives only onto the dialect it described.
func TestDiscover_ForcedEffortDialect(t *testing.T) {
	t.Parallel()

	const plainModels = `{"data":[{"id":"m","context_length":32768}]}`
	const reasoningModels = `{"data":[{"id":"m","context_length":32768,` +
		`"reasoning":{"supported_efforts":["low","medium","high"],"default_effort":"medium"}}]}`
	const kwargsProps = `{"chat_template":"{% if reasoning_effort %}...{% endif %}"}`
	const mandatoryModels = `{"data":[{"id":"m","context_length":32768,` +
		`"reasoning":{"mandatory":true,"default_enabled":true,` +
		`"supported_efforts":["max","high","low"],"default_effort":"max"}}]}`

	tests := []struct {
		name   string
		forced EffortDialect
		models string
		props  string
		want   EffortSupport
	}{
		{
			name:   "openai forced over a server that detected nothing ⇒ supported, openai wire",
			forced: EffortDialectOpenAI,
			models: plainModels,
			want:   EffortSupport{Supported: true, Dialect: EffortDialectOpenAI},
		},
		{
			name:   "kwargs forced over a server that detected nothing ⇒ supported, kwargs wire",
			forced: EffortDialectKwargs,
			models: plainModels,
			want:   EffortSupport{Supported: true, Dialect: EffortDialectKwargs},
		},
		{
			name:   "off forced over a server that DID detect a dial ⇒ unsupported, nothing sent",
			forced: EffortDialectOff,
			models: reasoningModels,
			props:  kwargsProps,
			want:   EffortSupport{Dialect: EffortDialectOff},
		},
		{
			name:   "auto (the zero) leaves detection untouched",
			forced: EffortDialectNone,
			models: reasoningModels,
			want: EffortSupport{
				Supported: true,
				Dialect:   EffortDialectReasoning,
				Efforts:   []string{"low", "medium", "high"},
				Default:   "medium",
			},
		},
		{
			name:   "forcing the dialect detection already found keeps its vocabulary",
			forced: EffortDialectReasoning,
			models: reasoningModels,
			want: EffortSupport{
				Supported: true,
				Dialect:   EffortDialectReasoning,
				Efforts:   []string{"low", "medium", "high"},
				Default:   "medium",
			},
		},
		{
			name:   "forcing a different dialect drops the set that described the other one",
			forced: EffortDialectOpenAI,
			models: reasoningModels,
			want:   EffortSupport{Supported: true, Dialect: EffortDialectOpenAI},
		},
		{
			name:   "forcing the detected dialect keeps the mandatory flag with the vocabulary",
			forced: EffortDialectReasoning,
			models: mandatoryModels,
			want: EffortSupport{
				Supported: true,
				Dialect:   EffortDialectReasoning,
				Efforts:   []string{"max", "high", "low"},
				Default:   "max",
				Mandatory: true,
			},
		},
		{
			name:   "forcing a DIFFERENT dialect keeps the mandatory flag though not the vocabulary",
			forced: EffortDialectOpenAI,
			models: mandatoryModels,
			want:   EffortSupport{Supported: true, Dialect: EffortDialectOpenAI, Mandatory: true},
		},
		{
			name:   "off forced over a model that cannot stop thinking ⇒ the zero verdict anyway",
			forced: EffortDialectOff,
			models: mandatoryModels,
			want:   EffortSupport{Dialect: EffortDialectOff},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv, _ := discoveryServer(tt.models, tt.props)
			defer srv.Close()

			info, err := NewClient(srv.URL, "", WithEffortDialect(tt.forced)).Discover(context.Background())

			if err != nil {
				t.Fatalf("Discover: %v", err)
			}
			if !reflect.DeepEqual(info.EffortSupport, tt.want) {
				t.Errorf("EffortSupport = %+v, want %+v", info.EffortSupport, tt.want)
			}
		})
	}
}

// The five words an entry may write map onto the four dialects and the "detect for me" zero — and
// a word that is not one of them reads as detect, because the config loader is the boundary that
// refuses it (config.ValidateServers) and this one has to stay total.
func TestEffortDialectFor(t *testing.T) {
	t.Parallel()

	cases := map[string]EffortDialect{
		"":          EffortDialectNone,
		"auto":      EffortDialectNone,
		"kwargs":    EffortDialectKwargs,
		"reasoning": EffortDialectReasoning,
		"openai":    EffortDialectOpenAI,
		"off":       EffortDialectOff,
		"gibberish": EffortDialectNone,
	}
	for name, want := range cases {
		if got := EffortDialectFor(name); got != want {
			t.Errorf("EffortDialectFor(%q) = %q, want %q", name, got, want)
		}
	}
}
