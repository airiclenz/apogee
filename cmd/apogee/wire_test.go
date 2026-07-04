package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/mechanisms"
	"github.com/airiclenz/apogee/internal/session"
	"github.com/airiclenz/apogee/internal/tui"
)

// discoverUpstreamModel probes a real OpenAI-compatible /v1/models endpoint and returns its
// active model — the production discoverer the root wires into resolveModel. The httptest
// server exercises the full provider path (HTTP + decode), not just the injected fake.
func TestDiscoverUpstreamModel(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"loaded-model","context_length":32768}]}`))
		case "/props":
			// Best-effort runtime-window probe; a non-llama.cpp server has no /props.
			w.WriteHeader(http.StatusNotFound)
		default:
			t.Errorf("discovery hit unexpected path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	got, err := discoverUpstreamModel(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("discoverUpstreamModel: %v", err)
	}
	if got.model != "loaded-model" {
		t.Errorf("model = %q; want the server's advertised model", got.model)
	}
	if got.contextWindow != 32768 {
		t.Errorf("contextWindow = %d; want the server's advertised window 32768", got.contextWindow)
	}
}

// An unreachable server is a discovery error the caller surfaces, not a silent empty model.
func TestDiscoverUpstreamModelUnreachable(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	if _, err := discoverUpstreamModel(context.Background(), srv.URL); err == nil {
		t.Fatal("discovery against a failing server: want an error, got nil")
	}
}

func TestParseMode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		in      string
		want    apogee.Mode
		wantErr bool
	}{
		{name: "plan", in: "plan", want: apogee.ModePlan},
		{name: "ask-before", in: "ask-before", want: apogee.ModeAskBefore},
		{name: "allow-edits", in: "allow-edits", want: apogee.ModeAllowEdits},
		{name: "auto parses (availability checked later)", in: "auto", want: apogee.ModeAuto},
		{name: "unknown", in: "bogus", wantErr: true},
		{name: "empty", in: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseMode(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseMode(%q) = %q, nil; want error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseMode(%q) unexpected error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Fatalf("parseMode(%q) = %q; want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestResolveRootsOverride(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	workspace := t.TempDir()

	roots, err := resolveRoots(home, workspace)
	if err != nil {
		t.Fatalf("resolveRoots: %v", err)
	}

	if roots.config != home {
		t.Errorf("config = %q; want %q", roots.config, home)
	}
	if want := filepath.Join(home, "library"); roots.library != want {
		t.Errorf("library = %q; want %q", roots.library, want)
	}
	if want := filepath.Join(home, "sessions"); roots.sessions != want {
		t.Errorf("sessions = %q; want %q", roots.sessions, want)
	}
	if roots.workspace != workspace {
		t.Errorf("workspace = %q; want %q", roots.workspace, workspace)
	}
}

func TestResolveRootsDefaults(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()

	roots, err := resolveRoots("", workspace)
	if err != nil {
		t.Fatalf("resolveRoots: %v", err)
	}

	// The default home is ~/.apogee; assert the leaf rather than the (machine-specific) home.
	if base := filepath.Base(roots.config); base != ".apogee" {
		t.Errorf("default config leaf = %q; want %q", base, ".apogee")
	}
	if !filepath.IsAbs(roots.config) {
		t.Errorf("config = %q; want an absolute path", roots.config)
	}
}

// validCfg is the minimum Config that constructs (Endpoint/Model/Events). It installs the
// real Bridge sink — the same delegate the binary wires — so the buildAgent tests exercise
// production wiring, not a stand-in. The endpoint is never dialled at construction, so a
// placeholder URL is fine.
func validCfg(t *testing.T) apogee.Config {
	t.Helper()
	return apogee.Config{
		Endpoint:     "http://127.0.0.1:1111",
		Model:        "fake",
		Mode:         apogee.ModeAskBefore,
		Events:       tui.NewBridge().Sink(),
		WorkspaceDir: t.TempDir(),
	}
}

func TestBuildAgentNew(t *testing.T) {
	t.Parallel()
	agent, err := buildAgent(validCfg(t), "")
	if err != nil {
		t.Fatalf("buildAgent: %v", err)
	}
	if agent == nil {
		t.Fatal("buildAgent returned a nil Agent")
	}
	t.Cleanup(func() { _ = agent.Close() })
}

func TestBuildAgentResumeRoundTrip(t *testing.T) {
	t.Parallel()
	// Snapshot a fresh Agent, persist it, and resume from the file.
	original, err := apogee.New(validCfg(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = original.Close() })

	session, err := original.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	data, err := session.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	path := filepath.Join(t.TempDir(), "session.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write session: %v", err)
	}

	resumed, err := buildAgent(validCfg(t), path)
	if err != nil {
		t.Fatalf("buildAgent resume: %v", err)
	}
	if resumed == nil {
		t.Fatal("buildAgent resume returned a nil Agent")
	}
	t.Cleanup(func() { _ = resumed.Close() })
}

// The TUI-side save round-trips through --resume: a snapshot written by the same saver the
// binary installs (sessionSaver over a session.Store) reconstructs an Agent via buildAgent
// — the P2.5 save↔resume acceptance, exercised without a terminal (P2.6 drives it live).
func TestSessionSaverRoundTripsThroughResume(t *testing.T) {
	t.Parallel()
	original, err := apogee.New(validCfg(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = original.Close() })

	snap, err := original.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	saver := &sessionSaver{store: session.NewStore(filepath.Join(t.TempDir(), "sessions"))}
	if err := saver.save(snap); err != nil {
		t.Fatalf("save: %v", err)
	}
	path := saver.saved()
	if path == "" {
		t.Fatal("saver recorded no path after a successful save")
	}

	resumed, err := buildAgent(validCfg(t), path)
	if err != nil {
		t.Fatalf("buildAgent resume of the saved session: %v", err)
	}
	if resumed == nil {
		t.Fatal("buildAgent resume returned a nil Agent")
	}
	t.Cleanup(func() { _ = resumed.Close() })
}

func TestBuildAgentResumeMissingFile(t *testing.T) {
	t.Parallel()
	_, err := buildAgent(validCfg(t), filepath.Join(t.TempDir(), "absent.json"))
	if err == nil {
		t.Fatal("buildAgent resume of a missing file: want error, got nil")
	}
}

func TestBuildAgentResumeFutureVersion(t *testing.T) {
	t.Parallel()
	// A session stamped with a version newer than this build understands must surface
	// ErrSessionVersion (a clear message), not panic (P2.5 acceptance, laid here). The
	// current schema is v1; any higher version is "from the future".
	path := filepath.Join(t.TempDir(), "future.json")
	const futureVersionPayload = `{"Version":9999,"State":null}`
	if err := os.WriteFile(path, []byte(futureVersionPayload), 0o600); err != nil {
		t.Fatalf("write session: %v", err)
	}

	_, err := buildAgent(validCfg(t), path)
	if !errors.Is(err, apogee.ErrSessionVersion) {
		t.Fatalf("buildAgent resume of a future version: err = %v; want ErrSessionVersion", err)
	}
}

// fakePreRequestMechanism is a minimal catalogued Mechanism (a pre-request hook) for exercising
// the mechanism wire-up while the real catalogue is empty. buildFake is the injected builder that
// mints it, standing in for internal/mechanisms.Build.
type fakePreRequestMechanism struct {
	id apogee.MechanismID
}

func (f fakePreRequestMechanism) Descriptor() apogee.MechanismDescriptor {
	return apogee.MechanismDescriptor{ID: f.id, Capability: apogee.CapProactiveNudge, Suppression: apogee.SuppressStrikesThree}
}
func (f fakePreRequestMechanism) Ordering() apogee.OrderingConstraints {
	return apogee.OrderingConstraints{}
}
func (f fakePreRequestMechanism) PreRequest(context.Context, *apogee.Request) error { return nil }

func buildFake(id apogee.MechanismID, _ mechanisms.Deps) (apogee.Mechanism, error) {
	return fakePreRequestMechanism{id: id}, nil
}

// An enabled ID is built and registered; a `false` entry is not. The registered Mechanism is
// visible in the registry's deterministic dispatch order (Ordered).
func TestBuildMechanismRegistryEnablesOnlyTrue(t *testing.T) {
	t.Parallel()
	enabled := map[string]bool{"alpha": true, "beta": false}
	registry, err := buildMechanismRegistry(enabled, mechanisms.Deps{}, buildFake)
	if err != nil {
		t.Fatalf("buildMechanismRegistry: %v", err)
	}
	if registry == nil {
		t.Fatal("registry is nil; want the enabled Mechanism registered")
	}
	ordered := registry.Ordered(apogee.HookPreRequest)
	if len(ordered) != 1 {
		t.Fatalf("Ordered = %d Mechanisms; want exactly the one enabled (the `false` entry is skipped)", len(ordered))
	}
	if got := ordered[0].Descriptor().ID; got != "alpha" {
		t.Errorf("registered ID = %q; want the enabled %q", got, "alpha")
	}
}

// Nothing enabled ⇒ a nil registry, so New falls back to its default empty one (today's
// behaviour unchanged for a config with no mechanisms block).
func TestBuildMechanismRegistryDefaultNone(t *testing.T) {
	t.Parallel()
	for _, enabled := range []map[string]bool{nil, {}, {"off": false}} {
		registry, err := buildMechanismRegistry(enabled, mechanisms.Deps{}, buildFake)
		if err != nil {
			t.Fatalf("buildMechanismRegistry(%+v): %v", enabled, err)
		}
		if registry != nil {
			t.Errorf("buildMechanismRegistry(%+v) = non-nil; want nil (nothing enabled)", enabled)
		}
	}
}

// An unknown ID is a loud startup error — proven end-to-end against the real (empty) catalogue via
// mechanisms.Build, so a typo'd `mechanisms:` key fails startup rather than silently vanishing.
func TestBuildMechanismRegistryUnknownIDErrors(t *testing.T) {
	t.Parallel()
	_, err := buildMechanismRegistry(map[string]bool{"nope": true}, mechanisms.Deps{}, mechanisms.Build)
	if err == nil {
		t.Fatal("enabling an unknown mechanism: want an error, got nil")
	}
}

// A built registry threads through New even under Bypass: enabling a Mechanism and running Bypass
// construct cleanly together (the dispatch gate that skips it under Bypass is item 2's, exercised
// in internal/agent). This proves the config → registry → Config.Mechanisms path is coherent.
func TestBuildMechanismRegistryConstructsUnderBypass(t *testing.T) {
	t.Parallel()
	registry, err := buildMechanismRegistry(map[string]bool{"alpha": true}, mechanisms.Deps{}, buildFake)
	if err != nil {
		t.Fatalf("buildMechanismRegistry: %v", err)
	}
	cfg := validCfg(t)
	cfg.Bypass = true
	cfg.Mechanisms = registry

	agent, err := apogee.New(cfg)
	if err != nil {
		t.Fatalf("New with an enabled Mechanism under Bypass: %v", err)
	}
	t.Cleanup(func() { _ = agent.Close() })
}

func TestFriendlyConstructErr(t *testing.T) {
	t.Parallel()

	if got := friendlyConstructErr(apogee.ErrAutoUnavailable); !errors.Is(got, errAutoUnavailable) {
		t.Errorf("friendlyConstructErr(ErrAutoUnavailable) = %v; want errAutoUnavailable", got)
	}

	other := errors.New("some other failure")
	if got := friendlyConstructErr(other); !errors.Is(got, other) {
		t.Errorf("friendlyConstructErr(other) = %v; want passthrough", got)
	}
}
