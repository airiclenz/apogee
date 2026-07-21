package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/mechanisms"
	"github.com/airiclenz/apogee/internal/platform"
	"github.com/airiclenz/apogee/internal/session"
	"github.com/airiclenz/apogee/internal/tools"
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

// The loud-zero notice fires exactly when the context window is unknown (0) while automatic
// Compaction is on — the Budget and the fold then have nothing to bind against (item 3 / S3). It
// is suppressed once a window is known (a context-window: key or successful discovery) or when
// Compaction is off.
func TestContextWindowNotice(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		maxTokens  int
		compaction bool
		wantNotice bool
	}{
		{name: "unknown window + compaction on → notice", maxTokens: 0, compaction: true, wantNotice: true},
		{name: "unknown window + compaction off → silent", maxTokens: 0, compaction: false, wantNotice: false},
		{name: "known window + compaction on → silent", maxTokens: 32768, compaction: true, wantNotice: false},
		{name: "known window + compaction off → silent", maxTokens: 32768, compaction: false, wantNotice: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := contextWindowNotice(tt.maxTokens, tt.compaction)
			if (got != "") != tt.wantNotice {
				t.Errorf("contextWindowNotice(%d, %v) = %q; wantNotice = %v", tt.maxTokens, tt.compaction, got, tt.wantNotice)
			}
			if tt.wantNotice && !strings.Contains(got, "context-window") {
				t.Errorf("notice %q does not name the context-window key", got)
			}
		})
	}
}

// The degradation notice fires in EXACTLY one cell of the {mode} × {FSWrite} × {confine}
// matrix: Auto, asking for confinement, on a backend that cannot fence the filesystem — the
// common case in containers, where landlock reports ENOSYS. Every other cell is silent: the
// three lower modes make no confinement promise (TODO constraint 6), an already-unconfined
// Auto has its own louder warning, and a capable backend needs no explanation.
func TestConfinementDegradedNotice(t *testing.T) {
	t.Parallel()
	modes := []apogee.Mode{modePlan, modeAskBefore, modeAllowEdits, modeAuto}
	fired := 0
	for _, mode := range modes {
		for _, fsWrite := range []bool{true, false} {
			for _, confine := range []bool{true, false} {
				caps := apogee.ConfinementCaps{FSWrite: fsWrite}
				got := confinementDegradedNotice("landlock", caps, mode, confine)
				want := mode == modeAuto && confine && !fsWrite
				if (got != "") != want {
					t.Errorf("confinementDegradedNotice(landlock, FSWrite=%v, %q, confine=%v) = %q; wantNotice = %v",
						fsWrite, mode, confine, got, want)
				}
				if got == "" {
					continue
				}
				fired++
				for _, want := range []string{"landlock", "approval", "/confine off", "/confine off --save"} {
					if !strings.Contains(got, want) {
						t.Errorf("notice %q does not mention %q", got, want)
					}
				}
			}
		}
	}
	if fired != 1 {
		t.Errorf("notice fired in %d cells of the matrix; want exactly 1 (auto + confine + no FSWrite)", fired)
	}
}

// The notice names the backend that answered, so the user can tell landlock-says-no from
// no-backend-at-all. domain.Confiner carries no name, so the label is derived from the
// concrete type — including for the host's real backend, whichever OS the tests run on.
func TestConfinerBackendName(t *testing.T) {
	t.Parallel()
	if got := confinerBackendName(platform.NewDenyConfiner()); got != "deny" {
		t.Errorf("confinerBackendName(denyConfiner) = %q; want %q", got, "deny")
	}
	if got := confinerBackendName(stubConfiner{}); got != "stub" {
		t.Errorf("confinerBackendName(stubConfiner) = %q; want %q", got, "stub")
	}
	if got := confinerBackendName(platform.NewConfiner()); got == "" {
		t.Error("confinerBackendName(host backend) = \"\"; the notice would name no backend at all")
	}
}

// stubConfiner is a named backend that enforces nothing — it exists to pin the label
// derivation against a type this package owns, independent of the host's real backend.
type stubConfiner struct{}

func (stubConfiner) Capabilities() apogee.ConfinementCaps { return apogee.ConfinementCaps{} }

func (stubConfiner) Confine(context.Context, apogee.ConfinementBox, *exec.Cmd) error { return nil }

// captureStderr swaps the process os.Stderr for a pipe, runs f, and returns everything f wrote to
// stderr. The caller must NOT be a parallel test: os.Stderr is a process-global, so this is only
// race-free during the sequential test phase (parallel tests are paused until it finishes).
func captureStderr(t *testing.T, f func()) string {
	t.Helper()
	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stderr = w
	captured := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		captured <- buf.String()
	}()

	f()

	_ = w.Close()
	os.Stderr = orig
	return <-captured
}

// runRoot threads opts.contextWindow into apogee.Config.Context.MaxContextTokens (wire.go), which
// the Budget and automatic Compaction bind against. The constructed Agent exposes no accessor for
// that field, so the threading is observed through its sole runtime consumer in the composition
// root: the loud-zero startup notice, which fires only when MaxContextTokens == 0 while Compaction
// is on. A positive window therefore reaches MaxContextTokens (no notice) and a zero window reaches
// it too (notice fires) — proving opts.contextWindow lands in ContextConfig, not just in opts (the
// gap TestApplyConfigContextWindow leaves open). The same value also lands in the TUI footer's
// ContextWindow, asserted here for the exact-value pin (item 5).
func TestRunRootThreadsContextWindow(t *testing.T) {
	// Deliberately NOT parallel: captureStderr swaps the process-global os.Stderr.
	tests := []struct {
		name          string
		contextWindow int
		wantNotice    bool
	}{
		{name: "positive window reaches MaxContextTokens (no loud-zero notice)", contextWindow: 16384, wantNotice: false},
		{name: "zero window reaches MaxContextTokens (loud-zero notice fires)", contextWindow: 0, wantNotice: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := &recordingLauncher{}
			opts := options{
				endpoint:      "http://127.0.0.1:1111",
				model:         "fake",
				mode:          "ask-before",
				workspace:     t.TempDir(),
				contextWindow: tt.contextWindow,
				autoCompact:   true, // the loud-zero notice fires only while Compaction is on
			}

			var runErr error
			stderr := captureStderr(t, func() {
				runErr = runRoot(context.Background(), opts, rec.launch)
			})

			if runErr != nil {
				t.Fatalf("runRoot: %v", runErr)
			}
			gotNotice := strings.Contains(stderr, "context window unknown")
			if gotNotice != tt.wantNotice {
				t.Errorf("loud-zero notice present = %v (stderr = %q); want %v — Config.Context.MaxContextTokens must carry opts.contextWindow = %d",
					gotNotice, stderr, tt.wantNotice, tt.contextWindow)
			}
			if rec.opts.ContextWindow != tt.contextWindow {
				t.Errorf("tui.Options.ContextWindow = %d; want the threaded %d", rec.opts.ContextWindow, tt.contextWindow)
			}
		})
	}
}

// The two Auto startup lines are mirror branches at the same site and never both fire:
// confine=false is the blanket-loosen WARNING, confine=true on an unfenceable backend is the
// degradation notice. The degraded cell is host-dependent (this machine's real backend decides
// whether it can fence at all), so it is asserted against that backend's own Capabilities
// rather than against an assumption about the test runner.
func TestRunRootConfinementStartupNotices(t *testing.T) {
	// Deliberately NOT parallel: captureStderr swaps the process-global os.Stderr.
	const (
		unconfinedWarning = "running UNCONFINED"
		degradedNotice    = "auto mode is gating terminal commands"
	)
	hostCanFence := platform.NewConfiner().Capabilities().FSWrite

	tests := []struct {
		name         string
		mode         string
		confine      bool
		wantWarning  bool
		wantDegraded bool
	}{
		{name: "auto unconfined → warning only", mode: "auto", confine: false, wantWarning: true},
		{name: "auto confined → degraded notice iff the host cannot fence", mode: "auto", confine: true, wantDegraded: !hostCanFence},
		{name: "ask-before makes no confinement promise → silent", mode: "ask-before", confine: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := &recordingLauncher{}
			opts := options{
				endpoint:           "http://127.0.0.1:1111",
				model:              "fake",
				mode:               tt.mode,
				workspace:          t.TempDir(),
				confineToWorkspace: tt.confine,
			}

			var runErr error
			stderr := captureStderr(t, func() {
				runErr = runRoot(context.Background(), opts, rec.launch)
			})

			if runErr != nil {
				t.Fatalf("runRoot: %v", runErr)
			}
			if got := strings.Contains(stderr, unconfinedWarning); got != tt.wantWarning {
				t.Errorf("unconfined-Auto warning present = %v; want %v (stderr = %q)", got, tt.wantWarning, stderr)
			}
			if got := strings.Contains(stderr, degradedNotice); got != tt.wantDegraded {
				t.Errorf("degradation notice present = %v; want %v (host FSWrite = %v, stderr = %q)",
					got, tt.wantDegraded, hostCanFence, stderr)
			}
		})
	}
}

// The presentation ladder's mechanisms are wired per session (ADR 0019): an Opener only where
// one could reach the eyes of the user (a LOCAL session with auto-open on), a doc server only
// where those eyes are on another machine (a REMOTE session). tui.Presentation reads a nil field
// as "a rung this host did not wire" rather than as a failure, so the zero cases below are the
// feature, not a gap — and rung 0, the transcript line, needs nothing from here at all.
func TestPresentationRungs(t *testing.T) {
	t.Parallel()
	// The owner's Zed-remoted devbox, as sshd writes it: "<client ip> <client port> <server ip>
	// <server port>". The third field is the address the user's machine reaches this box on.
	const devboxSSH = "192.168.64.1 50072 192.168.64.2 22"

	tests := []struct {
		name       string
		cfg        presentSettings
		env        map[string]string
		wantLocal  bool
		wantOpener bool
		wantDocs   bool
		wantHost   string
		wantPort   int
	}{
		{
			name:       "local desktop + auto-open → the opener, no server",
			cfg:        presentSettings{autoOpen: true},
			wantLocal:  true,
			wantOpener: true,
		},
		{
			name:       "local + a command override → the opener carries the template",
			cfg:        presentSettings{autoOpen: true, command: "zed {path}"},
			wantLocal:  true,
			wantOpener: true,
		},
		{
			name:      "local + auto-open off → no mechanism at all (rung 0 still runs)",
			cfg:       presentSettings{autoOpen: false, command: "zed {path}"},
			wantLocal: true,
		},
		{
			name:     "remote → the doc server, advertising the SSH server IP; never an opener",
			cfg:      presentSettings{autoOpen: true, port: 8934},
			env:      map[string]string{"SSH_CONNECTION": devboxSSH},
			wantDocs: true,
			wantHost: "192.168.64.2",
			wantPort: 8934,
		},
		{
			name:     "remote with no SSH_CONNECTION → present.host answers instead",
			cfg:      presentSettings{autoOpen: true, host: "devbox.internal"},
			env:      map[string]string{"SSH_TTY": "/dev/pts/3"},
			wantDocs: true,
			wantHost: "devbox.internal",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := func(name string) string { return tt.env[name] }
			rungs := presentationRungs(tt.cfg, "darwin", env)

			if rungs.Local != tt.wantLocal {
				t.Errorf("Local = %v; want %v", rungs.Local, tt.wantLocal)
			}
			if (rungs.Opener != nil) != tt.wantOpener {
				t.Errorf("Opener wired = %v; want %v", rungs.Opener != nil, tt.wantOpener)
			}
			if (rungs.Docs != nil) != tt.wantDocs {
				t.Errorf("Docs wired = %v; want %v", rungs.Docs != nil, tt.wantDocs)
			}
			if rungs.Opener != nil && rungs.Opener.CommandOverride != tt.cfg.command {
				t.Errorf("Opener.CommandOverride = %q; want the configured %q", rungs.Opener.CommandOverride, tt.cfg.command)
			}
			if rungs.Docs == nil {
				return
			}
			if rungs.Docs.Host != tt.wantHost {
				t.Errorf("Docs.Host = %q; want %q", rungs.Docs.Host, tt.wantHost)
			}
			if rungs.Docs.Port != tt.wantPort {
				t.Errorf("Docs.Port = %d; want the configured %d", rungs.Docs.Port, tt.wantPort)
			}
		})
	}
}

// present_document is offered exactly where a presentation can be carried out. runRoot installs
// the ladder on the Bridge, which is what makes bridge.Presenter() non-nil — and a non-nil
// Presenter is the whole registration condition of the default tool set (tools.HostTools). A
// Bridge nobody installed a presentation on — a headless embedder — supplies no Presenter, and
// the same registry build then omits the tool rather than offering the model an affordance
// nobody can honour.
func TestRunRootInstallsPresenter(t *testing.T) {
	t.Parallel()
	rec := &recordingLauncher{}
	workspace := t.TempDir()
	opts := options{
		endpoint:  "http://127.0.0.1:1111",
		model:     "fake",
		mode:      "ask-before",
		workspace: workspace,
		present:   presentSettings{autoOpen: true},
	}

	if err := runRoot(context.Background(), opts, rec.launch); err != nil {
		t.Fatalf("runRoot: %v", err)
	}
	presenter := rec.bridge.Presenter()
	if presenter == nil {
		t.Fatal("bridge.Presenter() = nil after runRoot; the interactive session installs no presentation")
	}
	if _, ok := tools.NewDefaultRegistryWithHost(workspace, tools.HostTools{Presenter: presenter}).Lookup("present_document"); !ok {
		t.Error("present_document is not registered for the interactive setup's Presenter")
	}

	headless := tui.NewBridge() // never SetPresentation'd — the headless host
	if headless.Presenter() != nil {
		t.Fatal("a Bridge with no presentation installed supplies a non-nil Presenter")
	}
	if _, ok := tools.NewDefaultRegistryWithHost(workspace, tools.HostTools{Presenter: headless.Presenter()}).Lookup("present_document"); ok {
		t.Error("present_document is registered with no Presenter; a headless host must not offer it")
	}
}

// registryWithMCP is the one place the composition root assembles HostTools by hand, so it must
// thread the Presenter as well — otherwise configuring an MCP server would silently take
// present_document away, which is exactly the kind of coupling the default build has no way to
// catch.
func TestRegistryWithMCPThreadsPresenter(t *testing.T) {
	t.Parallel()
	cfg := validCfg(t)
	cfg.Presenter = stubPresenter{}

	if _, ok := registryWithMCP(t.TempDir(), cfg, nil).Lookup("present_document"); !ok {
		t.Error("present_document is missing from the MCP registry build despite a configured Presenter")
	}
}

// stubPresenter shows nothing: the wiring under test consults only whether the delegate is
// non-nil (the registration condition), never what it does with a document.
type stubPresenter struct{}

func (stubPresenter) Present(context.Context, apogee.PresentRequest) (apogee.PresentOutcome, error) {
	return apogee.PresentOutcome{}, nil
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

// fakeKnown is a stand-in catalogue for the pure key-validation tests: mechanismIDs only checks a
// `mechanisms:` key against the known set and selects the enabled ones (the engine builds, so no
// constructor is needed here — the unknown-ID and construct-under-Bypass paths below drive the REAL
// catalogue via mechanisms.KnownIDs / apogee.New).
var fakeKnown = []apogee.MechanismID{"alpha", "beta", "off"}

// An enabled ID is selected; a `false` entry is not. mechanismIDs returns the enabled IDs in sorted
// canonical order for Config.EnableMechanisms — the engine builds them (ADR 0015 §1).
func TestMechanismIDsEnablesOnlyTrue(t *testing.T) {
	t.Parallel()
	ids, err := mechanismIDs(map[string]bool{"alpha": true, "beta": false}, fakeKnown)
	if err != nil {
		t.Fatalf("mechanismIDs: %v", err)
	}
	if len(ids) != 1 || ids[0] != "alpha" {
		t.Errorf("mechanismIDs = %v; want exactly [alpha] (the `false` entry is skipped)", ids)
	}
}

// Nothing enabled ⇒ a nil ID list, so Config.EnableMechanisms stays empty and New arms nothing
// (today's behaviour unchanged for a config with no mechanisms block). A KNOWN key mapped to false
// selects nothing — disabled Mechanisms are validated by name, never enabled.
func TestMechanismIDsDefaultNone(t *testing.T) {
	t.Parallel()
	for _, enabled := range []map[string]bool{nil, {}, {"off": false}} {
		ids, err := mechanismIDs(enabled, fakeKnown)
		if err != nil {
			t.Fatalf("mechanismIDs(%+v): %v", enabled, err)
		}
		if ids != nil {
			t.Errorf("mechanismIDs(%+v) = %v; want nil (nothing enabled)", enabled, ids)
		}
	}
}

// An unknown ENABLED ID is a loud startup error — proven against the real catalogue via
// mechanisms.KnownIDs, so a typo'd `mechanisms:` key fails startup rather than silently vanishing.
func TestMechanismIDsUnknownIDErrors(t *testing.T) {
	t.Parallel()
	_, err := mechanismIDs(map[string]bool{"nope": true}, mechanisms.KnownIDs())
	if err == nil {
		t.Fatal("enabling an unknown mechanism: want an error, got nil")
	}
}

// A typo'd key mapped to FALSE is a startup error too (phase-4-review-fixes item 5): the disabled-key
// validation stays cmd-side because the engine only ever sees the ENABLED IDs. The error lists the
// real catalogue's known IDs; a valid disabled key still selects nothing — validated against
// mechanisms.KnownIDs.
func TestMechanismIDsUnknownDisabledKeyErrors(t *testing.T) {
	t.Parallel()

	_, err := mechanismIDs(map[string]bool{"typo": false}, mechanisms.KnownIDs())
	if err == nil {
		t.Fatal(`{"typo": false}: want a startup error, got nil`)
	}
	if !strings.Contains(err.Error(), `"typo"`) {
		t.Errorf("error = %q, want it to name the unknown key", err)
	}
	if !strings.Contains(err.Error(), "validate") {
		t.Errorf("error = %q, want it to list the known catalogue (e.g. %q)", err, "validate")
	}

	// The same key spelled correctly and disabled is fine: validated by name, never enabled.
	ids, err := mechanismIDs(map[string]bool{"validate": false}, mechanisms.KnownIDs())
	if err != nil {
		t.Fatalf(`{"validate": false}: %v`, err)
	}
	if ids != nil {
		t.Errorf(`{"validate": false} = %v; want nil (a disabled Mechanism is never enabled)`, ids)
	}
}

// The enabled IDs thread through New as Config.EnableMechanisms and the engine arms them — even under
// Bypass, enabling a real catalogued Mechanism (validate) constructs cleanly (the dispatch gate that
// skips it under Bypass is the engine's, exercised in internal/agent). This proves the config →
// EnableMechanisms → engine-build path is coherent end-to-end.
func TestMechanismIDsConstructsUnderBypass(t *testing.T) {
	t.Parallel()
	ids, err := mechanismIDs(map[string]bool{"validate": true}, mechanisms.KnownIDs())
	if err != nil {
		t.Fatalf("mechanismIDs: %v", err)
	}
	cfg := validCfg(t)
	cfg.Bypass = true
	cfg.EnableMechanisms = ids

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
