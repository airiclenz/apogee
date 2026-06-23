package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/session"
	"github.com/airiclenz/apogee/internal/tui"
)

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
		{name: "auto parses (refused later)", in: "auto", want: apogee.ModeAuto},
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

func TestFriendlyConstructErr(t *testing.T) {
	t.Parallel()

	if got := friendlyConstructErr(apogee.ErrAutoUnavailable); !errors.Is(got, errAutoPhase3) {
		t.Errorf("friendlyConstructErr(ErrAutoUnavailable) = %v; want errAutoPhase3", got)
	}

	other := errors.New("some other failure")
	if got := friendlyConstructErr(other); !errors.Is(got, other) {
		t.Errorf("friendlyConstructErr(other) = %v; want passthrough", got)
	}
}
