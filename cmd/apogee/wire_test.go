package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/heartbeat"
	"github.com/airiclenz/apogee/internal/mechanisms"
	"github.com/airiclenz/apogee/internal/platform"
	"github.com/airiclenz/apogee/internal/session"
	"github.com/airiclenz/apogee/internal/tools"
	"github.com/airiclenz/apogee/internal/tui"
	llamalauncher "github.com/airiclenz/llama-launcher/launcher"
)

// The label-walk pre-warm trigger is the mirror of the degradation gate: it fires exactly when
// Auto asks for confinement a host CAN enforce (Auto ∧ confine-asked ∧ FSWrite), so the Windows
// token backend's disk-label walk is paid at startup behind a progress notice rather than silently
// mid-session. Every other mode, an unconfined Auto, and a backend that reports no FSWrite leave it
// off. FSWrite is true on landlock/seatbelt too, so a true verdict there is expected — PrewarmLabelWalk
// is the no-op seam that keeps their startup byte-unchanged, not this predicate.
func TestShouldPrewarmLabelWalk(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		mode        apogee.Mode
		confine     bool
		fsWrite     bool
		wantPrewarm bool
	}{
		{name: "auto + confine + fs-write → pre-warm", mode: modeAuto, confine: true, fsWrite: true, wantPrewarm: true},
		{name: "auto + confine + no fs-write → off (degradation's territory)", mode: modeAuto, confine: true, fsWrite: false, wantPrewarm: false},
		{name: "auto UNCONFINED → off", mode: modeAuto, confine: false, fsWrite: true, wantPrewarm: false},
		{name: "ask-before + confine + fs-write → off (not Auto)", mode: modeAskBefore, confine: true, fsWrite: true, wantPrewarm: false},
		{name: "allow-edits + confine + fs-write → off (not Auto)", mode: modeAllowEdits, confine: true, fsWrite: true, wantPrewarm: false},
		{name: "plan + confine + fs-write → off (not Auto)", mode: modePlan, confine: true, fsWrite: true, wantPrewarm: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := shouldPrewarmLabelWalk(tt.mode, tt.confine, tt.fsWrite); got != tt.wantPrewarm {
				t.Errorf("shouldPrewarmLabelWalk(%q, %v, %v) = %v; want %v",
					tt.mode, tt.confine, tt.fsWrite, got, tt.wantPrewarm)
			}
		})
	}
}

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

// runRoot threads opts.contextWindow — which is now the `context-window:` PIN and nothing else,
// since startup no longer probes — into both places that read it: the TUI's own ContextWindow (the
// footer's window and the gauge denominator) and, as the pin, the rebind closure, where it must
// outrank whatever the heartbeat observes (ADR 0024, decision 9). The unknown-window honesty line
// this test used to observe the threading through has moved into the TUI's rebind fold, where the
// window is actually known or not; internal/tui's TestUnknownWindowNotedOnBind owns it now.
func TestRunRootThreadsContextWindow(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		contextWindow int
		observed      int
		wantBound     int
	}{
		{name: "pinned window outranks the observation", contextWindow: 16384, observed: 131072, wantBound: 16384},
		{name: "unpinned adopts whatever the beat observed", contextWindow: 0, observed: 131072, wantBound: 131072},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := &recordingLauncher{}
			opts := options{
				endpoint:      "http://127.0.0.1:1111",
				model:         "fake",
				mode:          "ask-before",
				workspace:     t.TempDir(),
				contextWindow: tt.contextWindow,
				autoCompact:   true,
			}

			if err := runRoot(context.Background(), opts, rec.launch); err != nil {
				t.Fatalf("runRoot: %v", err)
			}
			if rec.opts.ContextWindow != tt.contextWindow {
				t.Errorf("tui.Options.ContextWindow = %d; want the threaded %d", rec.opts.ContextWindow, tt.contextWindow)
			}
			if rec.opts.Rebind == nil {
				t.Fatal("tui.Options.Rebind is nil; the composition root did not wire the rebind closure")
			}
			result, err := rec.opts.Rebind("fake", tt.observed)
			if err != nil {
				t.Fatalf("Rebind: %v", err)
			}
			if result.ContextWindow != tt.wantBound {
				t.Errorf("bound window = %d; want %d (pin = %d, observed = %d)",
					result.ContextWindow, tt.wantBound, tt.contextWindow, tt.observed)
			}
			// The build version is threaded into Options from the single source (the embedded
			// VERSION file, via apogee.Version), which never resolves empty (blank ⇒ "dev").
			if rec.opts.Version == "" {
				t.Error("tui.Options.Version is empty; the build version was not threaded from apogee.Version")
			}
		})
	}
}

// rebindSpecFor is the composition root's half of a rebind: everything that is per-model gets
// resolved again for the model the heartbeat observed, and everything else is left alone. The table
// walks the four decisions it makes — which system-prompt template (ADR 0023), which validated
// Mechanism set (ADR 0016), whether an explicit `mechanisms:` block suppresses that set, and which
// window is bound when the observation and a `context-window:` pin disagree (decision 9).
func TestRebindSpecForSelectsPerModelBindings(t *testing.T) {
	t.Parallel()

	prompts := systemPromptSettings{
		global: promptSource{text: "the global prompt"},
		models: map[string]promptSource{"model-b": {text: "the model-b prompt"}},
	}
	manual := []apogee.MechanismID{"validate"}

	tests := []struct {
		name         string
		opts         options
		manualIDs    []apogee.MechanismID
		model        string
		window       int
		pinnedWindow int
		wantPrompt   string
		wantWindow   int
		wantEnable   func(t *testing.T, got []apogee.MechanismID)
	}{
		{
			name:       "the per-model prompt entry is selected for the model being bound",
			opts:       options{systemPrompt: prompts},
			model:      "model-b",
			window:     32768,
			wantPrompt: "the model-b prompt",
			wantWindow: 32768,
		},
		{
			name:       "a model with no entry of its own falls back to the global prompt",
			opts:       options{systemPrompt: prompts},
			model:      "model-a",
			window:     32768,
			wantPrompt: "the global prompt",
			wantWindow: 32768,
		},
		{
			name: "a validated set matching the new model applies when no manual list was configured",
			opts: options{
				validatedSetsEnable: true,
				validatedSetsAlias:  map[string]string{gemmaKey: gemmaKey}, // the §3 human decision
			},
			model:      gemmaKey,
			window:     8192,
			wantWindow: 8192,
			wantEnable: func(t *testing.T, got []apogee.MechanismID) {
				t.Helper()
				if len(got) < 2 {
					t.Errorf("EnableMechanisms = %v; want the matched validated set, not the empty floor", got)
				}
			},
		},
		{
			name: "an explicit mechanisms: block is manual control and suppresses the matched set",
			opts: options{
				validatedSetsEnable: true,
				validatedSetsAlias:  map[string]string{gemmaKey: gemmaKey},
				mechanisms:          map[string]bool{"validate": true},
			},
			manualIDs:  manual,
			model:      gemmaKey,
			window:     8192,
			wantWindow: 8192,
			wantEnable: func(t *testing.T, got []apogee.MechanismID) {
				t.Helper()
				if !slices.Equal(got, manual) {
					t.Errorf("EnableMechanisms = %v; want the manual list %v carried through untouched", got, manual)
				}
			},
		},
		{
			name:         "a context-window: pin outranks the observed window",
			opts:         options{},
			model:        "model-a",
			window:       131072,
			pinnedWindow: 16384,
			wantWindow:   16384,
		},
		{
			name:       "an unpinned session adopts the observed window",
			opts:       options{},
			model:      "model-a",
			window:     131072,
			wantWindow: 131072,
		},
		{
			name:       "an observation with no window binds an unknown one rather than inventing it",
			opts:       options{},
			model:      "model-a",
			wantWindow: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			roots := stateRoots{config: t.TempDir(), validated: t.TempDir(), probe: t.TempDir()}

			spec, _, err := rebindSpecFor(tt.opts, roots, tt.manualIDs, tt.model, tt.window, tt.pinnedWindow)
			if err != nil {
				t.Fatalf("rebindSpecFor: %v", err)
			}
			if spec.Model != tt.model {
				t.Errorf("spec.Model = %q; want the observed %q", spec.Model, tt.model)
			}
			if spec.SystemPrompt != tt.wantPrompt {
				t.Errorf("spec.SystemPrompt = %q; want %q", spec.SystemPrompt, tt.wantPrompt)
			}
			if spec.MaxContextTokens != tt.wantWindow {
				t.Errorf("spec.MaxContextTokens = %d; want %d (observed %d, pin %d)",
					spec.MaxContextTokens, tt.wantWindow, tt.window, tt.pinnedWindow)
			}
			if tt.wantEnable != nil {
				tt.wantEnable(t, spec.EnableMechanisms)
			}
		})
	}
}

// runRoot resolves the configured system prompt (ADR 0023) for the RESOLVED model BEFORE it
// constructs anything, so a defect in the selected source fails startup naming the config key
// rather than silently sending no prompt. Both halves of the selected-source resolution are
// exercised at the call site — an unreadable file and an unknown placeholder — which is what
// proves the call is wired into the composition root at all (the gap TestResolveSystemPrompt,
// which calls the helper directly, leaves open).
func TestRunRootSystemPromptResolutionFails(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		sp   systemPromptSettings
		// wantErr are the substrings the surfaced error must carry.
		wantErr []string
	}{
		{
			name:    "an unreadable global file",
			sp:      systemPromptSettings{global: promptSource{file: filepath.Join("prompts", "absent-prompt.md")}},
			wantErr: []string{"system-prompt-file", "absent-prompt.md"},
		},
		{
			name:    "an unknown placeholder in the inline prompt",
			sp:      systemPromptSettings{global: promptSource{text: "hi {{bogus}}"}},
			wantErr: []string{"system-prompt-text", "{{bogus}}", "{{workspace}}"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := &recordingLauncher{}
			opts := options{
				endpoint:     "http://127.0.0.1:1111",
				model:        "fake",
				mode:         "ask-before",
				workspace:    t.TempDir(),
				configDir:    t.TempDir(), // the apogee home a relative prompt path resolves against
				systemPrompt: tt.sp,
			}

			err := runRoot(context.Background(), opts, rec.launch)
			if err == nil {
				t.Fatal("runRoot: want the system-prompt resolution error, got nil")
			}
			for _, want := range tt.wantErr {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %q; want it to contain %q", err, want)
				}
			}
			if rec.called {
				t.Error("the launcher ran; a system-prompt defect must fail startup before launch")
			}
		})
	}
}

// runRoot folds the resolved context-file name list into apogee.Config.ContextFiles, which is what
// makes the `context-files:` block reach the engine at all. The engine keeps no accessor for the
// list yet, so the threading is proven at its one observable seam: the construction gate that
// refuses a name reaching outside the workspace (internal/agent's own defense-in-depth check).
// A list that never arrived would construct happily — which is exactly the regression this pins.
func TestRunRootThreadsContextFiles(t *testing.T) {
	t.Parallel()
	t.Run("a workspace-relative list constructs and launches", func(t *testing.T) {
		t.Parallel()
		workspace := t.TempDir()
		// Only the first name exists: discovery, not a requirement, so startup must not care.
		if err := os.WriteFile(filepath.Join(workspace, "AGENTS.md"), []byte("house rules\n"), 0o600); err != nil {
			t.Fatalf("write AGENTS.md: %v", err)
		}
		rec := &recordingLauncher{}
		opts := options{
			endpoint:     "http://127.0.0.1:1111",
			model:        "fake",
			mode:         "ask-before",
			workspace:    workspace,
			configDir:    t.TempDir(),
			contextFiles: []string{"AGENTS.md", "docs/absent.md"},
		}
		if err := runRoot(context.Background(), opts, rec.launch); err != nil {
			t.Fatalf("runRoot: %v", err)
		}
		if !rec.called {
			t.Error("the launcher did not run; a resolved context-file list must not block startup")
		}
	})

	t.Run("a name reaching outside the workspace fails startup before launch", func(t *testing.T) {
		t.Parallel()
		rec := &recordingLauncher{}
		opts := options{
			endpoint:     "http://127.0.0.1:1111",
			model:        "fake",
			mode:         "ask-before",
			workspace:    t.TempDir(),
			configDir:    t.TempDir(),
			contextFiles: []string{filepath.Join("..", "outside.md")},
		}
		err := runRoot(context.Background(), opts, rec.launch)
		if err == nil {
			t.Fatal("runRoot: want the engine's context-file gate to refuse the name, got nil " +
				"(the list never reached apogee.Config.ContextFiles)")
		}
		for _, want := range []string{"ContextFiles", "escapes"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error = %q; want it to contain %q", err, want)
			}
		}
		if rec.called {
			t.Error("the launcher ran; a refused context-file name must fail startup before launch")
		}
	})
}

// The resolved `ui:` block reaches the renderer: runRoot hands opts.ui's two values to
// tui.Options as Spinner and SpinnerColor. They are threaded INDEPENDENTLY — the colour flag is
// not derived from the style and the style not from the flag — so the table walks the combination
// that would pass if either were folded into the other (a non-default style with the loop off) as
// well as the plain default.
func TestRunRootThreadsSpinnerOptions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		ui   uiSettings
	}{
		{name: "the resolved default: snake with the colour loop on", ui: uiSettings{spinner: tui.SpinnerSnake, spinnerColor: true}},
		{name: "a named style with the loop off travels as both", ui: uiSettings{spinner: tui.SpinnerGlitter, spinnerColor: false}},
		{name: "classic with the loop on — the old glyphs, the new colours", ui: uiSettings{spinner: tui.SpinnerClassic, spinnerColor: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := &recordingLauncher{}
			opts := options{
				endpoint:  "http://127.0.0.1:1111",
				model:     "fake",
				mode:      "ask-before",
				workspace: t.TempDir(),
				ui:        tt.ui,
			}
			if err := runRoot(context.Background(), opts, rec.launch); err != nil {
				t.Fatalf("runRoot: %v", err)
			}
			if rec.opts.Spinner != tt.ui.spinner {
				t.Errorf("tui.Options.Spinner = %q; want the resolved %q", rec.opts.Spinner, tt.ui.spinner)
			}
			if rec.opts.SpinnerColor != tt.ui.spinnerColor {
				t.Errorf("tui.Options.SpinnerColor = %v; want the resolved %v", rec.opts.SpinnerColor, tt.ui.spinnerColor)
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
	agent, err := buildAgent(validCfg(t), nil)
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
	// Snapshot a fresh Agent and resume off the record's Session (buildAgent no longer reads
	// files — resolveResume owns the id-or-path lookup, exercised separately below).
	original, err := apogee.New(validCfg(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = original.Close() })

	snap, err := original.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	resumed, err := buildAgent(validCfg(t), &session.Record{Session: snap})
	if err != nil {
		t.Fatalf("buildAgent resume: %v", err)
	}
	if resumed == nil {
		t.Fatal("buildAgent resume returned a nil Agent")
	}
	t.Cleanup(func() { _ = resumed.Close() })
}

// The TUI-side save round-trips through --resume: a record persisted by the same host the binary
// installs (sessionHost over a session.Store) resolves back by its minted id and reconstructs an
// Agent via buildAgent — the save↔resume acceptance, exercised without a terminal (P2.6 drives it
// live).
func TestSessionHostRoundTripsThroughResume(t *testing.T) {
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

	store := session.NewStore(filepath.Join(t.TempDir(), "sessions"))
	host := newSessionHost(store, t.TempDir(), "fake", nil)
	if err := host.Save(snap, nil, "hi", 1, 0); err != nil {
		t.Fatalf("Save: %v", err)
	}
	id := host.ActiveID()
	if id == "" {
		t.Fatal("host minted no id after a successful Save")
	}

	rec, err := resolveResume(store, id, false, "")
	if err != nil {
		t.Fatalf("resolveResume by id: %v", err)
	}
	resumed, err := buildAgent(validCfg(t), rec)
	if err != nil {
		t.Fatalf("buildAgent resume of the saved session: %v", err)
	}
	if resumed == nil {
		t.Fatal("buildAgent resume returned a nil Agent")
	}
	t.Cleanup(func() { _ = resumed.Close() })
}

func TestResolveResumeMissingArg(t *testing.T) {
	t.Parallel()
	store := session.NewStore(filepath.Join(t.TempDir(), "sessions"))
	_, err := resolveResume(store, filepath.Join(t.TempDir(), "absent.json"), false, "")
	if err == nil {
		t.Fatal("resolveResume of a value that is neither an id nor a file: want error, got nil")
	}
}

func TestBuildAgentResumeFutureVersion(t *testing.T) {
	t.Parallel()
	// A session stamped with a version newer than this build understands must surface
	// ErrSessionVersion (a clear message), not panic. resolveResume wraps the legacy bare
	// envelope happily; the version check bites at Resume, inside buildAgent.
	path := filepath.Join(t.TempDir(), "future.json")
	const futureVersionPayload = `{"Version":9999,"State":null}`
	if err := os.WriteFile(path, []byte(futureVersionPayload), 0o600); err != nil {
		t.Fatalf("write session: %v", err)
	}

	store := session.NewStore(filepath.Join(t.TempDir(), "sessions"))
	rec, err := resolveResume(store, path, false, "")
	if err != nil {
		t.Fatalf("resolveResume of a future-version file: %v", err)
	}
	_, err = buildAgent(validCfg(t), rec)
	if !errors.Is(err, apogee.ErrSessionVersion) {
		t.Fatalf("buildAgent resume of a future version: err = %v; want ErrSessionVersion", err)
	}
}

// ----------------------------------------------------------------------------
// The store-backed session host and the resume resolution (item 5)
// ----------------------------------------------------------------------------

// The host mints an id on the first Save and updates that same file thereafter, never overwriting
// the create-time title, and stamps the wiring facts (workspace, model) the renderer cannot know.
func TestSessionHostMintsIDOnceAndUpdatesInPlace(t *testing.T) {
	t.Parallel()
	store := session.NewStore(t.TempDir())
	host := newSessionHost(store, "/ws", "model-x", nil)

	if host.ActiveID() != "" {
		t.Errorf("ActiveID before any Save = %q; want empty", host.ActiveID())
	}
	if err := host.Save(apogee.Session{}, nil, "first title", 1, 100); err != nil {
		t.Fatalf("Save #1: %v", err)
	}
	id := host.ActiveID()
	if id == "" {
		t.Fatal("Save minted no id")
	}
	// A second Save keeps the same id (update-in-place) and never overwrites the create-time title.
	if err := host.Save(apogee.Session{}, nil, "SECOND title", 2, 200); err != nil {
		t.Fatalf("Save #2: %v", err)
	}
	if host.ActiveID() != id {
		t.Errorf("ActiveID after the second Save = %q; want the same minted id %q", host.ActiveID(), id)
	}
	metas, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(metas) != 1 {
		t.Fatalf("two Saves produced %d files; want 1 (update-in-place)", len(metas))
	}
	m := metas[0]
	if m.Title != "first title" {
		t.Errorf("Title = %q; want the create-time title (a later Save must not overwrite it)", m.Title)
	}
	if m.Workspace != "/ws" || m.Model != "model-x" {
		t.Errorf("Meta workspace/model = %q/%q; want /ws / model-x from the wiring", m.Workspace, m.Model)
	}
	if m.UserMsgs != 2 || m.CtxUsed != 200 {
		t.Errorf("Meta counts = msgs %d, ctx %d; want the latest Save's 2 / 200", m.UserMsgs, m.CtxUsed)
	}
}

// A heartbeat rebind moves the session's model mid-conversation, and the stored metadata has to
// follow it: a session that started model-less (the async cold start) or switched models upstream
// must be listed under what its Turns actually ran against, not under a launch-time value that was
// never true. SetModel restamps subsequent Saves in place — it does not rewrite history, because
// the record IS the session and its current model is the session's current truth.
func TestSessionHostSetModelStampsSaves(t *testing.T) {
	t.Parallel()
	store := session.NewStore(t.TempDir())
	host := newSessionHost(store, "/ws", "", nil) // a cold start: nothing bound yet

	if err := host.Save(apogee.Session{}, nil, "cold", 1, 0); err != nil {
		t.Fatalf("Save before the bind: %v", err)
	}
	host.SetModel("bound-model")
	if err := host.Save(apogee.Session{}, nil, "cold", 2, 0); err != nil {
		t.Fatalf("Save after the bind: %v", err)
	}

	metas, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(metas) != 1 {
		t.Fatalf("two Saves produced %d files; want 1 (update-in-place)", len(metas))
	}
	if metas[0].Model != "bound-model" {
		t.Errorf("Meta.Model = %q; want %q — the save after SetModel must carry the rebound model",
			metas[0].Model, "bound-model")
	}
}

// Rotate closes the active session so the next Save mints a fresh id; Load reads a stored session
// without touching the active one, and Activate then makes it the target of subsequent Saves so
// they update ITS file rather than forking a new one.
func TestSessionHostRotateAndLoadActivate(t *testing.T) {
	t.Parallel()
	store := session.NewStore(t.TempDir())
	host := newSessionHost(store, "/ws", "m", nil)

	if err := host.Save(apogee.Session{}, nil, "A", 1, 0); err != nil {
		t.Fatalf("Save A: %v", err)
	}
	first := host.ActiveID()

	host.Rotate()
	if host.ActiveID() != "" {
		t.Errorf("ActiveID after Rotate = %q; want empty", host.ActiveID())
	}
	if err := host.Save(apogee.Session{}, nil, "B", 1, 0); err != nil {
		t.Fatalf("Save B: %v", err)
	}
	second := host.ActiveID()
	if second == first || second == "" {
		t.Errorf("Save after Rotate minted %q; want a fresh id different from %q", second, first)
	}

	// Loading the first session reads it without activating; Activate then makes it current again,
	// so the next Save updates its file, not B's.
	rec, err := host.Load(first)
	if err != nil {
		t.Fatalf("Load(first): %v", err)
	}
	if rec.Meta.ID != first {
		t.Errorf("Load returned rec id %q, want %q", rec.Meta.ID, first)
	}
	if host.ActiveID() != second {
		t.Errorf("Load changed the active session to %q; it must leave %q active until Activate", host.ActiveID(), second)
	}
	host.Activate(rec.Meta)
	if host.ActiveID() != first {
		t.Errorf("Activate did not make %q current (active %q)", first, host.ActiveID())
	}
	if err := host.Save(apogee.Session{}, nil, "ignored", 3, 0); err != nil {
		t.Fatalf("Save after Load: %v", err)
	}
	if metas, _ := store.List(); len(metas) != 2 {
		t.Fatalf("after Save/Rotate/Save/Load/Save there are %d sessions; want 2", len(metas))
	}
}

// A rename of the ACTIVE session sticks: the next Save preserves the new title rather than
// reverting to the create-time one.
func TestSessionHostRenameActiveSticks(t *testing.T) {
	t.Parallel()
	store := session.NewStore(t.TempDir())
	host := newSessionHost(store, "/ws", "m", nil)
	if err := host.Save(apogee.Session{}, nil, "original", 1, 0); err != nil {
		t.Fatalf("Save: %v", err)
	}
	id := host.ActiveID()
	if err := host.Rename(id, "renamed"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if err := host.Save(apogee.Session{}, nil, "original", 2, 0); err != nil {
		t.Fatalf("Save after Rename: %v", err)
	}
	rec, err := store.Load(id)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if rec.Meta.Title != "renamed" {
		t.Errorf("Title after rename+Save = %q; want the renamed title to stick", rec.Meta.Title)
	}
}

// A host seeded from a resumed record begins ACTIVE on it — same id, preserved title — so the run
// continues that file rather than forking a new session.
func TestSessionHostResumeBeginsActive(t *testing.T) {
	t.Parallel()
	store := session.NewStore(t.TempDir())
	seed := &session.Record{Meta: session.Meta{ID: "20260724T120000Z-abcd", Title: "kept"}}
	host := newSessionHost(store, "/ws", "m", seed)

	if host.ActiveID() != seed.Meta.ID {
		t.Errorf("ActiveID of a resumed host = %q; want the resumed id %q", host.ActiveID(), seed.Meta.ID)
	}
	if err := host.Save(apogee.Session{}, nil, "derived", 1, 0); err != nil {
		t.Fatalf("Save: %v", err)
	}
	rec, err := store.Load(seed.Meta.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if rec.Meta.Title != "kept" {
		t.Errorf("Title after a resumed Save = %q; want the resumed title preserved", rec.Meta.Title)
	}
}

// --resume accepts a raw file path (not only a store id), including a pre-plan bare envelope, which
// resumes with no recorded scrollback — the replay payload carries the empty blob through so the
// TUI degrades to an honest note.
func TestResolveResumeLegacyPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, "old.json")
	if err := os.WriteFile(legacyPath, []byte(`{"Version":1,"State":null}`), 0o600); err != nil {
		t.Fatalf("write legacy: %v", err)
	}
	store := session.NewStore(filepath.Join(dir, "sessions"))
	rec, err := resolveResume(store, legacyPath, false, "")
	if err != nil {
		t.Fatalf("resolveResume by path: %v", err)
	}
	if rec == nil {
		t.Fatal("resolveResume returned nil for a readable legacy file")
	}
	if len(rec.Transcript) != 0 {
		t.Errorf("legacy Transcript = %s; want empty (no scrollback recorded)", rec.Transcript)
	}
	if rs := resumedSession(rec, false); rs == nil || len(rs.Transcript) != 0 {
		t.Errorf("resumedSession(legacy) = %+v; want a non-nil payload with an empty transcript", rs)
	}
}

// --continue resumes this workspace's most recent session (skipping newer sessions in other
// workspaces) and errors helpfully when the workspace has none.
func TestResolveContinuePicksWorkspaceNewest(t *testing.T) {
	t.Parallel()
	store := session.NewStore(t.TempDir())
	base := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	saveAt(t, store, "/a", base, "a-old")
	newestA := saveAt(t, store, "/a", base.Add(2*time.Hour), "a-new")
	saveAt(t, store, "/b", base.Add(3*time.Hour), "b-newest") // newer overall, but wrong workspace

	rec, err := resolveContinue(store, "/a")
	if err != nil {
		t.Fatalf("resolveContinue(/a): %v", err)
	}
	if rec.Meta.ID != newestA {
		t.Errorf("continue picked %q (%q); want /a's newest %q", rec.Meta.Title, rec.Meta.ID, newestA)
	}

	// A workspace with no sessions of its own is a friendly error, even though the store is non-empty.
	if _, err := resolveContinue(store, "/c"); err == nil {
		t.Error("resolveContinue(/c) with no sessions for that workspace: want an error")
	}
}

// saveAt persists one fresh session in workspace ws stamped at when (controlling both its id and
// UpdatedAt), returning the minted id. Each call uses its own host so it mints a distinct session.
func saveAt(t *testing.T, store *session.Store, ws string, when time.Time, title string) string {
	t.Helper()
	h := newSessionHost(store, ws, "m", nil)
	h.now = func() time.Time { return when }
	if err := h.Save(apogee.Session{}, nil, title, 1, 0); err != nil {
		t.Fatalf("saveAt %q: %v", title, err)
	}
	return h.ActiveID()
}

// --resume and --continue are mutually exclusive at the resolution seam (the runRoot-testable
// guard mirroring the cobra flag marker).
func TestResolveResumeMutuallyExclusive(t *testing.T) {
	t.Parallel()
	store := session.NewStore(t.TempDir())
	_, err := resolveResume(store, "some-id", true, "/ws")
	if err == nil {
		t.Fatal("resolveResume with both --resume and --continue: want a flag error")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("error = %q; want it to mention mutual exclusion", err)
	}
}

// A fresh start (neither flag set) resolves to no record and projects to a nil replay payload.
func TestResolveResumeFreshStart(t *testing.T) {
	t.Parallel()
	store := session.NewStore(t.TempDir())
	rec, err := resolveResume(store, "", false, "/ws")
	if err != nil {
		t.Fatalf("resolveResume fresh: %v", err)
	}
	if rec != nil {
		t.Errorf("resolveResume with neither flag = %+v; want nil", rec)
	}
	if got := resumedSession(nil, false); got != nil {
		t.Errorf("resumedSession(nil) = %+v; want nil (a fresh start replays nothing)", got)
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

// ----------------------------------------------------------------------------
// The llama-launcher seams (ADR 0029 D1/D2)
// ----------------------------------------------------------------------------

// fakeSwitcher stands in for the Agent at the one method a move calls, recording every spec it was
// handed so a test can prove the wire moved — or, more often, that it did NOT.
type fakeSwitcher struct {
	specs []apogee.UpstreamSpec
	err   error
}

func (f *fakeSwitcher) SwitchUpstream(spec apogee.UpstreamSpec) error {
	if f.err != nil {
		return f.err
	}
	f.specs = append(f.specs, spec)
	return nil
}

// fakeStamper stands in for the session host at the metadata half of a move.
type fakeStamper struct{ models []string }

func (f *fakeStamper) SetModel(model string) { f.models = append(f.models, model) }

// launcherWiringFixture builds the wiring under test over a scripted launcher and a session sitting
// on endpoint, and hands back the collaborators the assertions read.
func launcherWiringFixture(t *testing.T, ops launcherOps, endpoint string) (
	launcherWiring, *fakeSwitcher, *fakeStamper, *upstreamHolder) {
	t.Helper()
	agent := &fakeSwitcher{}
	host := &fakeStamper{}
	holder := newUpstreamHolder(endpoint, heartbeat.NewMonitor(endpoint, "", ""))
	wiring := launcherWiring{
		sessionMover: sessionMover{agent: agent, holder: holder, host: host, pinnedWindow: 16384},
		ops:          ops,
		path:         "/etc/llama-launcher/config.yaml",
	}
	return wiring, agent, host, holder
}

// twoServerConfig is a launcher config with one profile per backend on two different addresses —
// the fixture the same-address and cross-address load paths are told apart on.
func twoServerConfig(t *testing.T) *llamalauncher.Config {
	t.Helper()
	return launcherFixture(t, []string{"here.gguf", "there.gguf"}, `
servers:
  llamacpp:
    api_key: llamacpp-key
  ollama: true
defaults:
  server: llamacpp
  host: 127.0.0.1
  context_size: 4096
profiles:
  here:
    model: here.gguf
    port: 8080
  there:
    model: there.gguf
    port: 9090
`)
}

// A profile that resolves to the server the session is ALREADY on moves nothing: the wire is not
// re-pointed, the stored model is not cleared, and the result says so — the next beat observes the
// model change and rebinds through the ordinary path (ADR 0029 D2).
func TestLoadProfileSameAddressMovesNothing(t *testing.T) {
	t.Parallel()

	ops := &fakeLauncher{
		cfg:          twoServerConfig(t),
		loadProgress: []string{"Stopping the occupant", "Starting llama-server", "Waiting for health"},
		loadNotices:  []string{"parameters drifted — re-run with restart to apply"},
		notices:      []string{"servers.llamacpp: api_key has leading/trailing whitespace"},
		loadResult:   &llamalauncher.RunningInstance{Backend: "llamacpp", Host: "127.0.0.1", Port: 8080},
	}
	wiring, agent, host, holder := launcherWiringFixture(t, ops, "http://127.0.0.1:8080")

	var steps []string
	result, err := wiring.load("here", func(step string) { steps = append(steps, step) })
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if result.Moved {
		t.Errorf("result = %+v; want Moved false — the profile serves the session's own endpoint", result)
	}
	if result.Switch != (tui.ServerSwitchResult{}) {
		t.Errorf("result.Switch = %+v; want the zero value when nothing moved", result.Switch)
	}
	if len(agent.specs) != 0 {
		t.Errorf("SwitchUpstream called %+v; want no engine move at all", agent.specs)
	}
	if len(host.models) != 0 {
		t.Errorf("SetModel called %v; want the stored model untouched — nothing was unbound", host.models)
	}
	if got := holder.Endpoint(); got != "http://127.0.0.1:8080" {
		t.Errorf("holder endpoint = %q; want the unchanged session endpoint", got)
	}
	if len(ops.loadedNames) != 1 || ops.loadedNames[0] != "here" {
		t.Errorf("loadProfile names = %v; want exactly [here] — the profile still had to be activated", ops.loadedNames)
	}
	// Progress steps reach the caller as they happen (the pump item 5 drains), and both notice
	// sources land in one ordered list: the config's warnings first, then the load's own.
	if !slices.Equal(steps, ops.loadProgress) {
		t.Errorf("progress steps = %v; want %v in order", steps, ops.loadProgress)
	}
	wantNotices := []string{ops.notices[0], ops.loadNotices[0]}
	if !slices.Equal(result.Notices, wantNotices) {
		t.Errorf("result.Notices = %v; want %v", result.Notices, wantNotices)
	}
}

// A profile that resolves ELSEWHERE is followed: the same fold `/server` performs, with the profile
// name as the alias, the launcher's own api key for that backend, and no discovery hint — a profile
// name is not a wire model id.
func TestLoadProfileCrossAddressFollowsTheProfile(t *testing.T) {
	t.Parallel()

	ops := &fakeLauncher{
		cfg:        twoServerConfig(t),
		loadResult: &llamalauncher.RunningInstance{Backend: "llamacpp", Host: "127.0.0.1", Port: 9090},
	}
	wiring, agent, host, holder := launcherWiringFixture(t, ops, "http://127.0.0.1:8080")

	result, err := wiring.load("there", nil)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if !result.Moved {
		t.Fatalf("result = %+v; want Moved true — the profile serves another address", result)
	}
	want := tui.ServerSwitchResult{
		Endpoint:      "http://127.0.0.1:9090",
		HostAlias:     "there",
		ContextWindow: 16384,
	}
	if result.Switch != want {
		t.Errorf("result.Switch = %+v; want %+v (alias = the profile name, the global pin survives)", result.Switch, want)
	}
	wantSpec := apogee.UpstreamSpec{Endpoint: "http://127.0.0.1:9090", APIKey: "llamacpp-key"}
	if len(agent.specs) != 1 || agent.specs[0] != wantSpec {
		t.Errorf("SwitchUpstream specs = %+v; want exactly [%+v] — the key is the launcher config's own",
			agent.specs, wantSpec)
	}
	if got := holder.Endpoint(); got != "http://127.0.0.1:9090" {
		t.Errorf("holder endpoint = %q; want the profile's endpoint — the Monitor did not follow", got)
	}
	if !slices.Equal(host.models, []string{""}) {
		t.Errorf("SetModel calls = %v; want exactly one unbinding \"\" — a move unbinds the model", host.models)
	}
}

// A nil progress callback is the documented safe case, and a load that cannot even resolve its
// profile never reaches the launcher: no activation is attempted and nothing moves.
func TestLoadProfileUnknownNameNeverActuates(t *testing.T) {
	t.Parallel()

	ops := &fakeLauncher{cfg: twoServerConfig(t)}
	wiring, agent, _, _ := launcherWiringFixture(t, ops, "http://127.0.0.1:8080")

	if _, err := wiring.load("typo", nil); err == nil {
		t.Fatal("load with an unresolvable profile returned no error")
	}
	if len(ops.loadedNames) != 0 {
		t.Errorf("loadProfile called %v; want nothing activated", ops.loadedNames)
	}
	if len(agent.specs) != 0 {
		t.Errorf("SwitchUpstream called %+v; want the session left where it was", agent.specs)
	}
}

// The picker's rows cross the seam projected onto the renderer's type, in the launcher's own order.
func TestLaunchProfilesSeamProjectsRows(t *testing.T) {
	t.Parallel()

	ops := &fakeLauncher{
		cfg:       twoServerConfig(t),
		instances: []*llamalauncher.RunningInstance{{Backend: "llamacpp", Host: "127.0.0.1", Port: 9090, ActiveProfile: "there"}},
	}
	wiring, _, _, _ := launcherWiringFixture(t, ops, "http://127.0.0.1:8080")

	rows, err := wiring.profiles()
	if err != nil {
		t.Fatalf("profiles: %v", err)
	}
	want := []tui.LaunchProfileChoice{
		{Name: "here", Backend: "llamacpp", Addr: "127.0.0.1:8080", ContextWindow: 4096},
		{Name: "there", Backend: "llamacpp", Addr: "127.0.0.1:9090", ContextWindow: 4096, Running: true},
	}
	if !slices.Equal(rows, want) {
		t.Errorf("rows = %+v; want %+v", rows, want)
	}

	// The one failure that sinks the list travels out as the error the renderer notes.
	broken := &fakeLauncher{cfgErr: errors.New("no config file at /nope/config.yaml")}
	wiring, _, _, _ = launcherWiringFixture(t, broken, "http://127.0.0.1:8080")
	if rows, err := wiring.profiles(); err == nil || rows != nil {
		t.Errorf("profiles over an unreadable config = %+v, %v; want no rows and the loader's error", rows, err)
	}
}

// Both actuation verbs act on the instance discovery finds at the SESSION's address — the backend
// name for an unload comes from that instance, and the steps travel out projected.
func TestUnloadAndStopActOnTheSessionsEndpoint(t *testing.T) {
	t.Parallel()

	instances := []*llamalauncher.RunningInstance{
		{Backend: "ollama", Host: "127.0.0.1", Port: 11434},
		{Backend: "llamacpp", Host: "127.0.0.1", Port: 8080},
	}
	steps := []string{"Sending SIGTERM", "Waiting for the port to release"}

	ops := &fakeLauncher{
		cfg:           twoServerConfig(t),
		instances:     instances,
		actuateResult: &llamalauncher.StopResult{ServerStopped: true, Steps: steps},
	}
	wiring, _, _, _ := launcherWiringFixture(t, ops, "http://127.0.0.1:8080")

	result, err := wiring.unload("http://127.0.0.1:8080")
	if err != nil {
		t.Fatalf("unload: %v", err)
	}
	if !slices.Equal(result.Steps, steps) || !result.ServerStopped {
		t.Errorf("unload result = %+v; want the launcher's steps and ServerStopped true (a managed backend)", result)
	}
	if !slices.Equal(ops.unloaded, []string{"llamacpp 127.0.0.1:8080"}) {
		t.Errorf("unload calls = %v; want the backend and address discovery reported for the session", ops.unloaded)
	}

	if _, err := wiring.stop("http://127.0.0.1:11434"); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if !slices.Equal(ops.stopped, []string{"127.0.0.1:11434"}) {
		t.Errorf("stop calls = %v; want the address the endpoint reduced to", ops.stopped)
	}
}

// An endpoint no discovered instance answers for is refused by name rather than acted on: the one
// mistake available here would stop somebody else's server.
func TestUnloadAndStopRefuseAnUnmanagedEndpoint(t *testing.T) {
	t.Parallel()

	ops := &fakeLauncher{
		cfg:       twoServerConfig(t),
		instances: []*llamalauncher.RunningInstance{{Backend: "llamacpp", Host: "127.0.0.1", Port: 8080}},
	}
	wiring, _, _, _ := launcherWiringFixture(t, ops, "http://remote.invalid:9999")

	for _, verb := range []struct {
		name string
		call func(string) (tui.ActuationResult, error)
	}{{"unload", wiring.unload}, {"stop", wiring.stop}} {
		t.Run(verb.name, func(t *testing.T) {
			t.Parallel()
			result, err := verb.call("http://remote.invalid:9999")
			if err == nil {
				t.Fatalf("%s against an unmanaged endpoint returned no error", verb.name)
			}
			if !strings.Contains(err.Error(), "http://remote.invalid:9999") {
				t.Errorf("error %q does not name the endpoint it refused", err)
			}
			if len(result.Steps) != 0 || result.ServerStopped {
				t.Errorf("a refused %s still returned %+v; want the zero result", verb.name, result)
			}
		})
	}
	if len(ops.unloaded) != 0 || len(ops.stopped) != 0 {
		t.Errorf("the launcher was driven anyway: unloaded=%v stopped=%v", ops.unloaded, ops.stopped)
	}
}

// A failed actuation still reports how far it got: the facade returns a non-nil result carrying the
// steps completed before the failure, and discarding them with the error would throw away the only
// account of what happened.
func TestActuationResultKeepsTheStepsBesideTheError(t *testing.T) {
	t.Parallel()

	failed := errors.New("the process did not exit")
	steps := []string{"Sending SIGTERM", "Sending SIGKILL"}
	result, err := actuationResult(&llamalauncher.StopResult{Steps: steps}, failed)
	if !errors.Is(err, failed) {
		t.Errorf("err = %v; want the launcher's own error passed through", err)
	}
	if !slices.Equal(result.Steps, steps) {
		t.Errorf("result.Steps = %v; want the steps completed before the failure %v", result.Steps, steps)
	}
	if result, err := actuationResult(nil, failed); len(result.Steps) != 0 || !errors.Is(err, failed) {
		t.Errorf("actuationResult(nil, err) = %+v, %v; want the zero result and the error", result, err)
	}
}

// The `llama-launcher:` key decides whether the four seams exist at all: off wires them nil (the
// renderer's one-line degrade), a named path wires them together — no half-wired state, so the
// TUI's nil check on one member speaks for all four.
func TestRunRootWiresTheLauncherSeamsTogetherOrNotAtAll(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		key   string
		wired bool
	}{
		{name: "off ⇒ no local-server verbs", key: "off"},
		{name: "a named config ⇒ all four", key: filepath.Join(t.TempDir(), "launcher.yaml"), wired: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			upstream := upstreamServer(t, "model-a", 4096)
			rec := &recordingLauncher{}
			opts := options{
				endpoint:      upstream.URL,
				mode:          "ask-before",
				workspace:     t.TempDir(),
				configDir:     t.TempDir(),
				autoCompact:   true,
				llamaLauncher: tt.key,
			}
			if err := runRoot(context.Background(), opts, rec.launch); err != nil {
				t.Fatalf("runRoot: %v", err)
			}

			wired := map[string]bool{
				"LaunchProfiles": rec.opts.LaunchProfiles != nil,
				"LoadProfile":    rec.opts.LoadProfile != nil,
				"UnloadServer":   rec.opts.UnloadServer != nil,
				"StopServer":     rec.opts.StopServer != nil,
			}
			for member, got := range wired {
				if got != tt.wired {
					t.Errorf("tui.Options.%s wired = %v; want %v", member, got, tt.wired)
				}
			}
		})
	}
}
