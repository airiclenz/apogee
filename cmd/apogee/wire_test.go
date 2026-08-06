package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/heartbeat"
	"github.com/airiclenz/apogee/internal/mechanisms"
	"github.com/airiclenz/apogee/internal/platform"
	"github.com/airiclenz/apogee/internal/session"
	"github.com/airiclenz/apogee/internal/skills"
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
		{name: "the resolved default: snake with the colour loop on", ui: uiSettings{spinner: tui.SpinnerSnake, spinnerColor: true, showScrollbar: true}},
		{name: "a named style with the loop off travels as both", ui: uiSettings{spinner: tui.SpinnerGlitter, spinnerColor: false, showScrollbar: true}},
		{name: "classic with the loop on — the old glyphs, the new colours", ui: uiSettings{spinner: tui.SpinnerClassic, spinnerColor: true, showScrollbar: true}},
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
			workspace := t.TempDir()
			rungs := presentationRungs(tt.cfg, workspace, "darwin", env)

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
			// The doc server re-checks every served document against the workspace fence on every
			// request, so the rung is only wired with the root to check against.
			if rungs.Docs.Root != workspace {
				t.Errorf("Docs.Root = %q; want the workspace root %q", rungs.Docs.Root, workspace)
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
	if want := filepath.Join(home, "prompts"); roots.prompts != want {
		t.Errorf("prompts = %q; want %q", roots.prompts, want)
	}
	if roots.workspace != workspace {
		t.Errorf("workspace = %q; want %q", roots.workspace, workspace)
	}
}

// The prompt-recall host binds THIS run's workspace onto the store and creates the prompts
// directory on the first recorded prompt — resolveRoots names the path, the store makes it, so a
// session that sends nothing leaves no trace under the apogee home.
func TestRecallHostBindsWorkspace(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	workspace := t.TempDir()

	roots, err := resolveRoots(home, workspace)
	if err != nil {
		t.Fatalf("resolveRoots: %v", err)
	}

	host := newRecallHost(roots.prompts, roots.workspace)
	if _, err := os.Stat(roots.prompts); !os.IsNotExist(err) {
		t.Errorf("constructing the recall host created %q; the directory is the store's to make lazily", roots.prompts)
	}

	loaded, err := host.LoadPrompts()
	if err != nil {
		t.Fatalf("LoadPrompts on a fresh home: %v", err)
	}
	if len(loaded) != 0 {
		t.Errorf("a fresh workspace recalled %v; want nothing", loaded)
	}

	if err := host.AppendPrompt("first prompt"); err != nil {
		t.Fatalf("AppendPrompt: %v", err)
	}
	if _, err := os.Stat(roots.prompts); err != nil {
		t.Fatalf("the prompts dir was not created by the first append: %v", err)
	}

	// A second host over the SAME roots reads the same file back — the proof the workspace binding
	// is what keys it, not the host instance.
	again, err := newRecallHost(roots.prompts, roots.workspace).LoadPrompts()
	if err != nil {
		t.Fatalf("LoadPrompts after an append: %v", err)
	}
	if len(again) != 1 || again[0] != "first prompt" {
		t.Errorf("recalled %v; want [first prompt]", again)
	}

	// Another workspace under the same home recalls nothing: recall is per-workspace.
	other, err := newRecallHost(roots.prompts, t.TempDir()).LoadPrompts()
	if err != nil {
		t.Fatalf("LoadPrompts for another workspace: %v", err)
	}
	if len(other) != 0 {
		t.Errorf("another workspace recalled %v; want nothing", other)
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

// ----------------------------------------------------------------------------
// Late-bound construction (ADR 0036 decision 3)
// ----------------------------------------------------------------------------

// The ordinary start is unchanged: a determined startup server is bound BEFORE the TUI is handed
// anything, so the engine the renderer receives is a working one and the heartbeat seam already
// observes that server. Prebound is the zero value, which is what says so.
func TestRunRootBindsADeterminedStartupBeforeLaunch(t *testing.T) {
	t.Parallel()
	srv := upstreamServer(t, "model-a", 4096)
	rec := &recordingLauncher{}
	opts := options{
		endpoint:  srv.URL,
		model:     "model-a",
		mode:      "ask-before",
		hostAlias: "workstation",
		workspace: t.TempDir(),
		configDir: t.TempDir(),
	}

	if err := runRoot(context.Background(), opts, rec.launch); err != nil {
		t.Fatalf("runRoot: %v", err)
	}
	if rec.opts.Prebound != (tui.PreboundStart{}) {
		t.Errorf("tui.Options.Prebound = %+v; want the zero value — this session started bound", rec.opts.Prebound)
	}
	// A bound engine answers a conversation read; an unbound one refuses it.
	if _, err := rec.engine.Snapshot(); err != nil {
		t.Errorf("Snapshot on the launched engine: %v; want a constructed Agent behind the seam", err)
	}
	if beat := rec.opts.Heartbeat(context.Background()); !beat.Reachable || beat.ActiveModel != "model-a" {
		t.Errorf("beat = %+v; want the startup server's Monitor, installed before launch", beat)
	}
}

// The pre-bound start: with no server determined, the TUI is launched with no engine at all. Every
// seam is still wired — that is the point, the renderer must be able to ask and then bind — and the
// reason travels through to the renderer so it can say which of the three situations this is.
func TestRunRootStartsPreboundWithoutAnEngine(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		prebound tui.PreboundStart
		servers  []serverEntry
	}{
		{
			name:     "first boot, with servers to choose from",
			prebound: tui.PreboundStart{Reason: tui.PreboundFirstBoot},
			servers:  []serverEntry{{Name: "laptop", Endpoint: "http://127.0.0.1:1111"}},
		},
		{
			name:     "a recorded choice no entry carries any more",
			prebound: tui.PreboundStart{Reason: tui.PreboundStaleChoice, Name: "the-old-name"},
			servers:  []serverEntry{{Name: "laptop", Endpoint: "http://127.0.0.1:1111"}},
		},
		{
			name:     "nothing configured at all",
			prebound: tui.PreboundStart{Reason: tui.PreboundNoServers},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := &recordingLauncher{}
			opts := options{
				mode:      "ask-before",
				workspace: t.TempDir(),
				configDir: t.TempDir(),
				servers:   tt.servers,
				prebound:  tt.prebound,
			}

			if err := runRoot(context.Background(), opts, rec.launch); err != nil {
				t.Fatalf("runRoot: %v", err)
			}
			if !rec.called {
				t.Fatal("the launcher was not invoked; a pre-bound start must still open the UI")
			}
			if rec.opts.Prebound != tt.prebound {
				t.Errorf("tui.Options.Prebound = %+v; want %+v", rec.opts.Prebound, tt.prebound)
			}
			// Nothing was constructed: the conversation seams refuse, and they refuse by naming
			// the way out rather than panicking on a nil Agent.
			if _, err := rec.engine.Snapshot(); !errors.Is(err, errNoServerBound) {
				t.Errorf("Snapshot err = %v; want errNoServerBound — an engine was constructed with no server", err)
			}
			if err := rec.engine.Submit(apogee.UserInput{Text: "hello"}); !errors.Is(err, errNoServerBound) {
				t.Errorf("Submit err = %v; want errNoServerBound", err)
			}
			if rec.engine.InExchange() {
				t.Error("InExchange = true with nothing bound")
			}
			beat := rec.opts.Heartbeat(context.Background())
			if beat.Reachable || beat.Failure != "" || beat.ActiveModel != "" || len(beat.AvailableModels) != 0 {
				t.Errorf("beat = %+v; want the zero Beat — there is no server to observe yet", beat)
			}
			// And the way out is wired: the picker's rows are the configured list (no synthesized
			// row, because no ephemeral startup exists) and BindServer is what ends the state.
			if choices := rec.opts.Servers(); len(choices) != len(tt.servers) {
				t.Errorf("tui.Options.Servers() = %+v; want the configured list %+v", choices, tt.servers)
			}
			if rec.opts.BindServer == nil {
				t.Error("tui.Options.BindServer is nil; the pre-bound session has no way to bind one")
			}
		})
	}
}

// BindServer is the seam that ends the pre-bound state, and it does it exactly once: the first call
// constructs the Agent AND installs the Monitor (both seams flip together), a second is refused
// before anything is built, and an unknown name never reaches construction at all.
func TestBindServerConstructsOnceAndFlipsBothSeams(t *testing.T) {
	t.Parallel()
	first := upstreamServer(t, "model-a", 4096)
	second := upstreamServer(t, "model-b", 8192)
	rec := &recordingLauncher{}
	opts := options{
		mode:          "ask-before",
		workspace:     t.TempDir(),
		configDir:     t.TempDir(),
		contextWindow: 16384, // the global pin, which a first binding adopts like a switch does
		servers: []serverEntry{
			{Name: "laptop", Endpoint: first.URL, Model: "model-a", APIKey: "laptop-key"},
			{Name: "workstation", Endpoint: second.URL, Model: "model-b"},
		},
		prebound: tui.PreboundStart{Reason: tui.PreboundFirstBoot},
	}

	if err := runRoot(context.Background(), opts, rec.launch); err != nil {
		t.Fatalf("runRoot: %v", err)
	}

	// A name no entry carries is resolved before anything is constructed, so the session stays
	// exactly as unbound as it was.
	if _, err := rec.opts.BindServer("nope"); err == nil {
		t.Error("BindServer accepted a name no entry carries")
	}
	if _, err := rec.engine.Snapshot(); !errors.Is(err, errNoServerBound) {
		t.Errorf("Snapshot err = %v after a failed bind; want errNoServerBound", err)
	}

	result, err := rec.opts.BindServer("laptop")
	if err != nil {
		t.Fatalf("BindServer: %v", err)
	}
	if result.Endpoint != first.URL || result.HostAlias != "laptop" {
		t.Errorf("result = %+v; want the entry's endpoint and its name as the alias", result)
	}
	if result.ContextWindow != 16384 {
		t.Errorf("result.ContextWindow = %d; want the global 16384 pin", result.ContextWindow)
	}
	// Both seams flipped: the engine exists, and the heartbeat observes the server it was built
	// against.
	if _, err := rec.engine.Snapshot(); err != nil {
		t.Errorf("Snapshot after the bind: %v; want a constructed Agent", err)
	}
	if beat := rec.opts.Heartbeat(context.Background()); !beat.Reachable || beat.ActiveModel != "model-a" {
		t.Errorf("beat after the bind = %+v; want model-a from the bound server's Monitor", beat)
	}

	// Exactly once: a second bind is refused, and nothing moved — the session is still on the
	// server it bound, which is what `/server` (SwitchServer) exists to change.
	if _, err := rec.opts.BindServer("workstation"); !errors.Is(err, errAlreadyBound) {
		t.Errorf("second BindServer err = %v; want errAlreadyBound", err)
	}
	if beat := rec.opts.Heartbeat(context.Background()); beat.ActiveModel != "model-a" {
		t.Errorf("beat after the refused second bind = %+v; want the first server still observed", beat)
	}
	// And the switch that IS the right verb still works over the same list.
	if _, err := rec.opts.SwitchServer("workstation"); err != nil {
		t.Fatalf("SwitchServer after a bind: %v", err)
	}
	if beat := rec.opts.Heartbeat(context.Background()); beat.ActiveModel != "model-b" {
		t.Errorf("beat after the switch = %+v; want model-b", beat)
	}
}

// The two settings a human can move while the picker is open must not be lost when the engine is
// finally constructed: the footer showed them, so the engine has to be born with them.
func TestLateEngineAppliesPreBindSettingsOnBind(t *testing.T) {
	t.Parallel()
	engine := newLateEngine(modeAskBefore, true)
	t.Cleanup(func() { _ = engine.Close() })

	// Unbound, the reads answer what a bind would install.
	if !engine.ConfineToWorkspace() {
		t.Error("ConfineToWorkspace = false before a bind; want the resolved value")
	}
	engine.SetMode(modePlan)
	engine.SetConfineToWorkspace(false)
	if engine.ConfineToWorkspace() {
		t.Error("ConfineToWorkspace = true after SetConfineToWorkspace(false) while unbound")
	}

	cfg := validCfg(t)
	cfg.Mode = modeAskBefore
	cfg.ConfineToWorkspace = true
	var bound *apogee.Agent
	if err := engine.Bind(func() (*apogee.Agent, error) {
		agent, err := apogee.New(cfg)
		bound = agent
		return agent, err
	}); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if engine.ConfineToWorkspace() {
		t.Error("the bound Agent kept the launch blast radius; the pre-bind change was dropped")
	}
	if bound.Mode() != modePlan {
		t.Errorf("the bound Agent's mode = %q; want the %q the human cycled to before the bind",
			bound.Mode(), modePlan)
	}
	// Exactly one Agent per session: a second Bind is refused before the constructor runs, so the
	// one this holder closes at shutdown is the only one that was ever built.
	built := false
	if err := engine.Bind(func() (*apogee.Agent, error) {
		built = true
		return apogee.New(cfg)
	}); !errors.Is(err, errAlreadyBound) {
		t.Errorf("second Bind err = %v; want errAlreadyBound", err)
	}
	if built {
		t.Error("the refused second Bind still constructed an Agent")
	}
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

// A record resumed from an explicit PATH is adopted with a FRESH id: the file's declared id is
// content, not identity, so a planted record claiming another session's id must not make the run's
// autosaves overwrite that session. Resuming by id (the /sessions handle) still continues in place
// — TestSessionHostRoundTripsThroughResume and TestSessionHostResumeBeginsActive pin that half.
func TestResolveResumeByPathRemintsID(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := session.NewStore(filepath.Join(dir, "sessions"))

	// The victim: a real session of this store, with its own transcript.
	victimID := saveAt(t, store, "/ws", time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC), "victim")

	// The planted file: a readable record that CLAIMS the victim's id.
	planted := session.Record{
		RecordVersion: session.RecordVersion,
		Meta:          session.Meta{ID: victimID, Title: "planted", Workspace: "/elsewhere"},
	}
	data, err := json.Marshal(planted)
	if err != nil {
		t.Fatalf("marshal planted: %v", err)
	}
	plantedPath := filepath.Join(dir, "planted.json")
	if err := os.WriteFile(plantedPath, data, 0o600); err != nil {
		t.Fatalf("write planted: %v", err)
	}

	rec, err := resolveResume(store, plantedPath, false, "/ws")
	if err != nil {
		t.Fatalf("resolveResume by path: %v", err)
	}
	if rec.Meta.ID == victimID {
		t.Fatalf("path resume adopted the file's declared id %q; want a freshly minted one", victimID)
	}
	if rec.Meta.Title != "planted" {
		t.Errorf("path resume Title = %q; want the record's own title carried over", rec.Meta.Title)
	}

	// The run continues as a NEW session: its autosave lands on its own file and the victim's
	// record is untouched.
	host := newSessionHost(store, "/ws", "m", rec)
	if host.ActiveID() != rec.Meta.ID {
		t.Errorf("host active id = %q; want the re-minted %q", host.ActiveID(), rec.Meta.ID)
	}
	if err := host.Save(apogee.Session{}, nil, "continued", 1, 0); err != nil {
		t.Fatalf("Save after a path resume: %v", err)
	}
	got, err := store.Load(victimID)
	if err != nil {
		t.Fatalf("Load victim: %v", err)
	}
	if got.Meta.Title != "victim" {
		t.Errorf("the victim record was overwritten by the path-resumed session: title = %q", got.Meta.Title)
	}
	if metas, err := store.List(); err != nil || len(metas) != 2 {
		t.Errorf("store holds %d sessions (err %v); want 2 — the victim plus the re-minted one", len(metas), err)
	}
}

// A record whose declared id is not a filename — a traversal planted in a repo's session file — is
// refused outright at load, so --resume of it never starts a run whose autosaves write there.
func TestResolveResumeRejectsTraversalRecordID(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	plantedPath := filepath.Join(dir, "planted.json")
	planted := fmt.Sprintf(
		`{"recordVersion":%d,"meta":{"id":"../../.claude/settings"},"session":{"Version":1,"State":null}}`,
		session.RecordVersion)
	if err := os.WriteFile(plantedPath, []byte(planted), 0o600); err != nil {
		t.Fatalf("write planted: %v", err)
	}
	store := session.NewStore(filepath.Join(dir, "sessions"))
	if _, err := resolveResume(store, plantedPath, false, "/ws"); err == nil {
		t.Fatal("resolveResume of a record declaring a traversal id: want an error, got nil")
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
	holder := newUpstreamHolder()
	holder.Bind(endpoint, "", "", heartbeat.NewMonitor(endpoint, "", ""))
	wiring := launcherWiring{
		sessionMover: sessionMover{
			agent: agent, holder: holder, host: host,
			live: newLiveSettings(options{contextWindow: 16384}, nil),
		},
		ops:  ops,
		path: newLauncherPath("/etc/llama-launcher/config.yaml"),
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

	if result.Move != nil {
		t.Error("result carries a Move; want none — the profile serves the session's own endpoint")
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
//
// The move is RESOLVED by the seam and PERFORMED by its caller, and this test says so twice. The
// load runs on the TUI's actuation Cmd goroutine — for minutes on a large model — while the move
// mutates the Agent, which only the Update goroutine may do (ADR 0029 D2, the rebind contract): so
// nothing about the session may have moved when load returns, and everything must move when the
// returned call is committed.
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

	if result.Move == nil {
		t.Fatalf("result = %+v; want a resolved Move — the profile serves another address", result)
	}
	if len(agent.specs) != 0 || len(host.models) != 0 || holder.Endpoint() != "http://127.0.0.1:8080" {
		t.Errorf("the seam moved the session itself: specs %+v, models %v, endpoint %q; want the move "+
			"resolved and left for the Update goroutine to commit",
			agent.specs, host.models, holder.Endpoint())
	}

	switched, err := result.Move()
	if err != nil {
		t.Fatalf("committing the resolved move: %v", err)
	}

	want := tui.ServerSwitchResult{
		Endpoint:      "http://127.0.0.1:9090",
		HostAlias:     "there",
		ContextWindow: 16384,
	}
	if switched != want {
		t.Errorf("the committed move = %+v; want %+v (alias = the profile name, the global pin survives)", switched, want)
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

// wildcardBoundConfig is twoServerConfig under the posture anyone who wants their server reachable
// off-box runs: `defaults.host: "0.0.0.0"`. The profiles resolve to the address the server BINDS,
// while the session dials one of the addresses that bind answers on — one server, two legitimate
// spellings, and the fixture the cases below are read from.
func wildcardBoundConfig(t *testing.T) *llamalauncher.Config {
	t.Helper()
	return launcherFixture(t, []string{"here.gguf", "there.gguf"}, `
servers:
  llamacpp:
    api_key: llamacpp-key
  ollama: true
defaults:
  server: llamacpp
  host: 0.0.0.0
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

// A wildcard bind answers on every interface this machine holds, loopback included, so a session
// dialling that loopback IS talking to the launcher's server — with no profile load first to make
// the two sides spell the address the same way. What the launcher is then ASKED about is its own
// address, never the dial spelling the match was made through: matching is normalised, addressing
// is not.
func TestUnloadAndStopActOnAWildcardBoundServer(t *testing.T) {
	t.Parallel()

	ops := &fakeLauncher{
		cfg: wildcardBoundConfig(t),
		instances: []*llamalauncher.RunningInstance{
			{Backend: "llamacpp", Host: "0.0.0.0", Port: 8080},
			{Backend: "ollama", Host: "::", Port: 11434},
		},
		actuateResult: &llamalauncher.StopResult{ServerStopped: true},
	}
	wiring, _, _, _ := launcherWiringFixture(t, ops, "http://127.0.0.1:8080")

	result, err := wiring.unload("http://127.0.0.1:8080")
	if err != nil {
		t.Fatalf("unload: %v; want the verb to ACT — the session dials an address the `0.0.0.0` bind "+
			"answers on, and no load has happened to align the two spellings", err)
	}
	if !slices.Equal(ops.unloaded, []string{"llamacpp 0.0.0.0:8080"}) {
		t.Errorf("unload calls = %v; want the LAUNCHER's own address, not the dial spelling matched by", ops.unloaded)
	}
	if result.Backend != "llamacpp" || result.Addr != "0.0.0.0:8080" {
		t.Errorf("unload result = %+v; want the discovered instance as the launcher holds it", result)
	}

	// The v6 wildcard is the same claim in the other family.
	if _, err := wiring.stop("http://[::1]:11434"); err != nil {
		t.Fatalf("stop against a `::`-bound server: %v; want the verb to act", err)
	}
	if !slices.Equal(ops.stopped, []string{"[::]:11434"}) {
		t.Errorf("stop calls = %v; want the launcher's own `::` spelling", ops.stopped)
	}

	// And the direction that must never widen: a wildcard bind does not make somebody else's server
	// ours, however well the ports line up.
	if _, err := wiring.stop("http://remote.invalid:8080"); err == nil {
		t.Errorf("stop against a remote endpoint returned no error; want the refusal — a wildcard bind " +
			"is not a licence to stop a server on another machine")
	}
	if !slices.Equal(ops.stopped, []string{"[::]:11434"}) {
		t.Errorf("stop calls = %v; want the refused endpoint to have driven nothing", ops.stopped)
	}
}

// A profile whose resolved address is the BIND spelling of the very server this session dials moves
// nothing: the wildcard-vs-loopback difference is one server spelled twice, and re-pointing the wire
// on it would unbind the model and re-announce the seed on every single load. Two PORTS remain two
// servers, wildcard bind or not.
func TestLoadProfileWildcardBindMovesNothing(t *testing.T) {
	t.Parallel()

	ops := &fakeLauncher{
		cfg:        wildcardBoundConfig(t),
		loadResult: &llamalauncher.RunningInstance{Backend: "llamacpp", Host: "0.0.0.0", Port: 8080},
	}
	wiring, agent, host, holder := launcherWiringFixture(t, ops, "http://127.0.0.1:8080")

	result, err := wiring.load("here", nil)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if result.Move != nil {
		t.Error("result carries a Move; want none — `0.0.0.0:8080` and `127.0.0.1:8080` are one server")
	}
	if len(agent.specs) != 0 {
		t.Errorf("SwitchUpstream called %+v; want no engine move at all", agent.specs)
	}
	if len(host.models) != 0 {
		t.Errorf("SetModel called %v; want the stored model untouched — nothing was unbound", host.models)
	}
	if got := holder.Endpoint(); got != "http://127.0.0.1:8080" {
		t.Errorf("holder endpoint = %q; want the session's own spelling, unchanged", got)
	}
	if len(ops.loadedNames) != 1 || ops.loadedNames[0] != "here" {
		t.Errorf("loadProfile names = %v; want exactly [here] — the profile still had to be activated", ops.loadedNames)
	}

	moved, err := wiring.load("there", nil)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if moved.Move == nil {
		t.Errorf("result = %+v; want a resolved Move — another PORT is another server however it is bound", moved)
	}
}

// A load that GENUINELY moves the session hands the wire an address the session can DIAL. The
// profile resolves to the wildcard the server binds; what the `Switch` result, the engine's upstream
// spec and the endpoint holder all take is the loopback spelling — the same projection the picker's
// rows already carry, applied at the one site that BUILDS a session endpoint out of a launcher
// address. `http://0.0.0.0:9090` is not a destination on Windows at all, and it would re-split the
// two spellings for exactly the sessions that have moved.
func TestLoadProfileCrossAddressDialsTheLoopback(t *testing.T) {
	t.Parallel()

	ops := &fakeLauncher{
		cfg:        wildcardBoundConfig(t),
		loadResult: &llamalauncher.RunningInstance{Backend: "llamacpp", Host: "0.0.0.0", Port: 9090},
	}
	wiring, agent, host, holder := launcherWiringFixture(t, ops, "http://127.0.0.1:8080")

	result, err := wiring.load("there", nil)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if result.Move == nil {
		t.Fatalf("result = %+v; want a resolved Move — another PORT is another server however it is bound", result)
	}
	switched, err := result.Move()
	if err != nil {
		t.Fatalf("committing the resolved move: %v", err)
	}

	const dial, bind = "http://127.0.0.1:9090", "http://0.0.0.0:9090"
	want := tui.ServerSwitchResult{Endpoint: dial, HostAlias: "there", ContextWindow: 16384}
	if switched != want {
		t.Errorf("the committed move = %+v; want %+v", switched, want)
	}
	wantSpec := apogee.UpstreamSpec{Endpoint: dial, APIKey: "llamacpp-key"}
	if len(agent.specs) != 1 || agent.specs[0] != wantSpec {
		t.Errorf("SwitchUpstream specs = %+v; want exactly [%+v]", agent.specs, wantSpec)
	}
	if got := holder.Endpoint(); got != dial {
		t.Errorf("holder endpoint = %q; want %q — the Monitor beats at the dial spelling", got, dial)
	}
	// Said the other way round too, because the defect this pins is one specific wrong value: the
	// bind spelling must reach nothing the session talks through.
	reached := []string{switched.Endpoint, holder.Endpoint()}
	for _, spec := range agent.specs {
		reached = append(reached, spec.Endpoint)
	}
	for _, got := range reached {
		if got == bind {
			t.Errorf("endpoint = %q; want %q — a wildcard bind is an address to listen on, not one "+
				"to dial", got, dial)
		}
	}
	if !slices.Equal(host.models, []string{""}) {
		t.Errorf("SetModel calls = %v; want exactly one unbinding \"\" — a move unbinds the model", host.models)
	}

	// Item 9's exclusion holds for the session that moved, pinned where it is DECIDED: the profile
	// this session now serves reaches the picker spelled exactly as its endpoint reduces, so
	// offerableProfiles drops that row and no row carries a spurious elsewhere stamp.
	rows, _, err := launchProfiles(ops, "config.yaml")
	if err != nil {
		t.Fatalf("launchProfiles: %v", err)
	}
	session, err := endpointAddr(holder.Endpoint())
	if err != nil {
		t.Fatalf("endpointAddr(%q): %v", holder.Endpoint(), err)
	}
	var loaded string
	for _, row := range rows {
		if row.Name == "there" {
			loaded = row.Addr
		}
	}
	if loaded != session {
		t.Errorf("the moved-to profile's row Addr = %q; want %q — the endpoint the session now holds, "+
			"reduced, is what the picker compares against", loaded, session)
	}
}

// The session that has just moved still asks the LAUNCHER about the server on the launcher's own
// terms. The move projected the endpoint to the loopback, `managedInstance` matches the discovered
// `0.0.0.0:9090` instance against it through sameServer, and the verbs act — while ops.unload and
// ops.stop receive the bind spelling. Matching is normalised; addressing is not.
func TestUnloadAndStopActOnTheServerASessionMovedTo(t *testing.T) {
	t.Parallel()

	ops := &fakeLauncher{
		cfg:        wildcardBoundConfig(t),
		loadResult: &llamalauncher.RunningInstance{Backend: "llamacpp", Host: "0.0.0.0", Port: 9090},
		instances: []*llamalauncher.RunningInstance{
			{Backend: "llamacpp", Host: "0.0.0.0", Port: 9090},
		},
		actuateResult: &llamalauncher.StopResult{ServerStopped: true},
	}
	wiring, _, _, holder := launcherWiringFixture(t, ops, "http://127.0.0.1:8080")

	loaded, err := wiring.load("there", nil)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Move == nil {
		t.Fatal("the load resolved no move; the session below is meant to have followed the profile")
	}
	if _, err := loaded.Move(); err != nil { // the commit the completion fold makes on the Update goroutine
		t.Fatalf("committing the resolved move: %v", err)
	}
	moved := holder.Endpoint()
	if moved != "http://127.0.0.1:9090" {
		t.Fatalf("holder endpoint after the move = %q; want http://127.0.0.1:9090 — the verbs below act "+
			"on the endpoint the session was left holding", moved)
	}

	result, err := wiring.unload(moved)
	if err != nil {
		t.Fatalf("unload after a move: %v; want the verb to ACT — the moved session dials an address "+
			"the `0.0.0.0` bind answers on", err)
	}
	if !slices.Equal(ops.unloaded, []string{"llamacpp 0.0.0.0:9090"}) {
		t.Errorf("unload calls = %v; want the LAUNCHER's own address, not the dial spelling the match "+
			"was made through", ops.unloaded)
	}
	if result.Backend != "llamacpp" || result.Addr != "0.0.0.0:9090" {
		t.Errorf("unload result = %+v; want the discovered instance as the launcher holds it", result)
	}

	if _, err := wiring.stop(moved); err != nil {
		t.Fatalf("stop after a move: %v; want the verb to act", err)
	}
	if !slices.Equal(ops.stopped, []string{"0.0.0.0:9090"}) {
		t.Errorf("stop calls = %v; want the launcher's own bind spelling", ops.stopped)
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

// The launcher's health-wait timeout crosses the seam PROJECTED onto the renderer's own sentinel
// (ADR 0029 D1 keeps facade sentinels out of internal/tui exactly as it keeps facade types out), so
// the completion fold can recognise the one failure that earns a coda. Both chains survive the
// projection, and the launcher's own words — the PID and log path it names — are kept intact.
func TestLoadProfileProjectsTheStartupTimeout(t *testing.T) {
	t.Parallel()

	timeout := fmt.Errorf("llama-server did not become healthy (pid 4711, log /tmp/a.log): %w",
		llamalauncher.ErrStartupTimeout)
	ops := &fakeLauncher{cfg: twoServerConfig(t), loadErr: timeout}
	wiring, agent, _, _ := launcherWiringFixture(t, ops, "http://127.0.0.1:8080")

	_, err := wiring.load("there", nil)

	if !errors.Is(err, tui.ErrStartupTimeout) {
		t.Errorf("load error = %v; want it to answer to tui.ErrStartupTimeout", err)
	}
	if !errors.Is(err, llamalauncher.ErrStartupTimeout) {
		t.Errorf("load error = %v; want the launcher's own sentinel still reachable through it", err)
	}
	if err.Error() != timeout.Error() {
		t.Errorf("load error text = %q; want the launcher's own words unchanged (%q)", err, timeout)
	}
	if len(agent.specs) != 0 {
		t.Errorf("SwitchUpstream called %+v; want the session left where it was", agent.specs)
	}

	// Every other failure crosses untouched — only the timeout may claim the coda.
	other := &fakeLauncher{cfg: twoServerConfig(t), loadErr: errors.New("model file not found")}
	wiring, _, _, _ = launcherWiringFixture(t, other, "http://127.0.0.1:8080")
	if _, err := wiring.load("there", nil); errors.Is(err, tui.ErrStartupTimeout) {
		t.Errorf("load error = %v; want an ordinary failure NOT marked as a startup timeout", err)
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
	if result.Backend != "llamacpp" || result.Addr != "127.0.0.1:8080" {
		t.Errorf("unload result = %+v; want the discovered instance named for the renderer's wording", result)
	}
	if !slices.Equal(ops.unloaded, []string{"llamacpp 127.0.0.1:8080"}) {
		t.Errorf("unload calls = %v; want the backend and address discovery reported for the session", ops.unloaded)
	}

	stopped, err := wiring.stop("http://127.0.0.1:11434")
	if err != nil {
		t.Fatalf("stop: %v", err)
	}
	if !slices.Equal(ops.stopped, []string{"127.0.0.1:11434"}) {
		t.Errorf("stop calls = %v; want the address the endpoint reduced to", ops.stopped)
	}
	if stopped.Backend != "ollama" || stopped.Addr != "127.0.0.1:11434" {
		t.Errorf("stop result = %+v; want the instance answering at the session's address", stopped)
	}
}

// An endpoint no discovered instance answers for is refused by name rather than acted on: the one
// mistake available here would stop somebody else's server.
func TestUnloadAndStopRefuseAnUnmanagedEndpoint(t *testing.T) {
	t.Parallel()

	// Each verb gets its OWN scripted launcher, so the two parallel subtests share nothing to
	// record over — and each can assert, in its own scope, that the launcher was never driven.
	// The table names the methods rather than binding them, since the wiring is built per subtest.
	for _, verb := range []struct {
		name string
		call func(launcherWiring, string) (tui.ActuationResult, error)
	}{{"unload", launcherWiring.unload}, {"stop", launcherWiring.stop}} {
		t.Run(verb.name, func(t *testing.T) {
			t.Parallel()

			ops := &fakeLauncher{
				cfg:       twoServerConfig(t),
				instances: []*llamalauncher.RunningInstance{{Backend: "llamacpp", Host: "127.0.0.1", Port: 8080}},
			}
			wiring, _, _, _ := launcherWiringFixture(t, ops, "http://remote.invalid:9999")

			result, err := verb.call(wiring, "http://remote.invalid:9999")
			if err == nil {
				t.Fatalf("%s against an unmanaged endpoint returned no error", verb.name)
			}
			if !strings.Contains(err.Error(), "http://remote.invalid:9999") {
				t.Errorf("error %q does not name the endpoint it refused", err)
			}
			if len(result.Steps) != 0 || result.ServerStopped {
				t.Errorf("a refused %s still returned %+v; want the zero result", verb.name, result)
			}
			if len(ops.unloaded) != 0 || len(ops.stopped) != 0 {
				t.Errorf("the launcher was driven anyway: unloaded=%v stopped=%v", ops.unloaded, ops.stopped)
			}
		})
	}
}

// A failed actuation still reports how far it got: the facade returns a non-nil result carrying the
// steps completed before the failure, and discarding them with the error would throw away the only
// account of what happened.
func TestActuationResultKeepsTheStepsBesideTheError(t *testing.T) {
	t.Parallel()

	failed := errors.New("the process did not exit")
	steps := []string{"Sending SIGTERM", "Sending SIGKILL"}
	on := &llamalauncher.RunningInstance{Backend: "llamacpp", Host: "127.0.0.1", Port: 8080}
	result, err := actuationResult(on, &llamalauncher.StopResult{Steps: steps}, failed)
	if !errors.Is(err, failed) {
		t.Errorf("err = %v; want the launcher's own error passed through", err)
	}
	if !slices.Equal(result.Steps, steps) {
		t.Errorf("result.Steps = %v; want the steps completed before the failure %v", result.Steps, steps)
	}
	// The failed verb still names what it was acting on — the renderer heads the steps with it, and a
	// StopResult carries no instance of its own once the operation failed.
	if result.Backend != "llamacpp" || result.Addr != "127.0.0.1:8080" {
		t.Errorf("result = %+v; want the aimed-at instance's backend and address", result)
	}
	if result, err := actuationResult(nil, nil, failed); len(result.Steps) != 0 || !errors.Is(err, failed) {
		t.Errorf("actuationResult(nil, nil, err) = %+v, %v; want the zero result and the error", result, err)
	}
}

// The four seams exist for the whole session whatever `llama-launcher:` said at startup, because
// the key is editable and applies live (ADR 0037): whether the integration works is a fact the
// VERBS answer per call. Off, every one of them reports tui.ErrNoLauncher — the renderer's own
// no-launcher sentence, which `/model` reads as "offer the models the server advertises".
func TestRunRootWiresTheLauncherSeamsForTheWholeSession(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		key     string
		enabled bool
	}{
		{name: "off ⇒ the verbs report the integration off", key: "off"},
		{name: "a named config ⇒ the verbs act on it", key: filepath.Join(t.TempDir(), "launcher.yaml"), enabled: true},
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

			for member, wired := range map[string]bool{
				"LaunchProfiles": rec.opts.LaunchProfiles != nil,
				"LoadProfile":    rec.opts.LoadProfile != nil,
				"UnloadServer":   rec.opts.UnloadServer != nil,
				"StopServer":     rec.opts.StopServer != nil,
			} {
				if !wired {
					t.Errorf("tui.Options.%s is nil; want every launcher seam wired for the session", member)
				}
			}

			// What the seams SAY is where off and on differ now. A named config that is not there
			// fails as the launcher's own missing-file error, which is emphatically not the
			// integration being off.
			_, err := rec.opts.LaunchProfiles()
			if got := errors.Is(err, tui.ErrNoLauncher); got == tt.enabled {
				t.Errorf("LaunchProfiles error = %v (ErrNoLauncher = %v); want enabled = %v", err, got, tt.enabled)
			}
			if _, err := rec.opts.UnloadServer(upstream.URL); errors.Is(err, tui.ErrNoLauncher) == tt.enabled {
				t.Errorf("UnloadServer error = %v; want the integration reported %v", err, tt.enabled)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// The live-apply dispatcher (ADR 0037)
// ----------------------------------------------------------------------------

// applySettingSpy is the settingsEngine surface as a witness: it records which seam the dispatcher
// drove and with what, so the mapping from registry path to engine call is asserted without an Agent
// — the narrow-interface reason applySettingFor takes one at all.
type applySettingSpy struct {
	modes        []apogee.Mode
	bypass       []bool
	compaction   []bool
	contextFiles []contextFileChoice
	swaps        []*apogee.ToolRegistry
	// swapErr is the idle-only refusal a busy engine answers a swap with, so the dispatcher's
	// keep-the-persisted-value path is exercisable without a run in flight.
	swapErr error
}

func (s *applySettingSpy) SetMode(m apogee.Mode)        { s.modes = append(s.modes, m) }
func (s *applySettingSpy) SetBypass(on bool)            { s.bypass = append(s.bypass, on) }
func (s *applySettingSpy) SetCompactionEnabled(on bool) { s.compaction = append(s.compaction, on) }
func (s *applySettingSpy) SetContextFiles(on bool, n []string) {
	s.contextFiles = append(s.contextFiles, contextFileChoice{enable: on, names: n})
}

func (s *applySettingSpy) SwapTools(registry *apogee.ToolRegistry) error {
	if s.swapErr != nil {
		return s.swapErr
	}
	s.swaps = append(s.swaps, registry)
	return nil
}

// drove reports how many engine seams the spy was driven through in total — the assertion a key that
// should have touched nothing makes.
func (s *applySettingSpy) drove() int {
	return len(s.modes) + len(s.bypass) + len(s.compaction) + len(s.contextFiles) + len(s.swaps)
}

// Every key the dispatcher knows lands on ITS seam and no other, carrying the value the file spells.
// The context-files switch is the one that answers with a boundary note, because its names are folded
// into the standing prompt only at a session boundary (ADR 0037 decision 3) — every other key is in
// force the moment it returns, which is what an empty note means.
func TestApplySettingDrivesTheRightEngineSeam(t *testing.T) {
	t.Parallel()
	names := []string{"AGENTS.md", "CLAUDE.md"}
	tests := []struct {
		name     string
		key      string
		value    string
		wantNote string
		check    func(t *testing.T, spy *applySettingSpy)
	}{
		{
			name: "mode", key: "mode", value: "allow-edits",
			check: func(t *testing.T, spy *applySettingSpy) {
				t.Helper()
				if want := []apogee.Mode{modeAllowEdits}; !slices.Equal(spy.modes, want) {
					t.Errorf("SetMode = %v, want %v", spy.modes, want)
				}
			},
		},
		{
			name: "bypass", key: "bypass", value: "true",
			check: func(t *testing.T, spy *applySettingSpy) {
				t.Helper()
				if want := []bool{true}; !slices.Equal(spy.bypass, want) {
					t.Errorf("SetBypass = %v, want %v", spy.bypass, want)
				}
			},
		},
		{
			name: "auto-compact", key: "auto-compact", value: "false",
			check: func(t *testing.T, spy *applySettingSpy) {
				t.Helper()
				if want := []bool{false}; !slices.Equal(spy.compaction, want) {
					t.Errorf("SetCompactionEnabled = %v, want %v", spy.compaction, want)
				}
			},
		},
		{
			name: "context-files.enable on carries this run's names", key: "context-files.enable", value: "true",
			wantNote: contextFileNote,
			check: func(t *testing.T, spy *applySettingSpy) {
				t.Helper()
				if len(spy.contextFiles) != 1 || !spy.contextFiles[0].enable ||
					!slices.Equal(spy.contextFiles[0].names, names) {
					t.Errorf("SetContextFiles = %+v, want one call with %v", spy.contextFiles, names)
				}
			},
		},
		{
			name: "context-files.enable off", key: "context-files.enable", value: "false",
			wantNote: contextFileNote,
			check: func(t *testing.T, spy *applySettingSpy) {
				t.Helper()
				if len(spy.contextFiles) != 1 || spy.contextFiles[0].enable {
					t.Errorf("SetContextFiles = %+v, want one call with the switch off", spy.contextFiles)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			spy := &applySettingSpy{}
			note, err := applySettingFor(settingsApplier{engine: spy, contextFiles: names})(tt.key, tt.value)
			if err != nil {
				t.Fatalf("apply %s=%s: %v", tt.key, tt.value, err)
			}
			if note != tt.wantNote {
				t.Errorf("note = %q, want %q", note, tt.wantNote)
			}
			tt.check(t, spy)
		})
	}
}

// What the dispatcher will not apply, it REFUSES by name — the write has already landed, so a silent
// success would leave the file and the session disagreeing with nothing said about it. A key this
// build cannot apply and a value its seam cannot take are the same kind of answer.
func TestApplySettingRefusesWhatItCannotApply(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, key, value, wantIn string
	}{
		{name: "a key with no live seam", key: "mcp-servers", value: "", wantIn: "mcp-servers"},
		{name: "a key that is not a setting", key: "nonsense", value: "1", wantIn: "nonsense"},
		{name: "a bool that is not one", key: "bypass", value: "yes please", wantIn: "bypass is true or false"},
		{name: "a mode outside the ladder", key: "mode", value: "yolo", wantIn: "invalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			spy := &applySettingSpy{}
			note, err := applySettingFor(settingsApplier{engine: spy})(tt.key, tt.value)
			if err == nil {
				t.Fatalf("apply %s=%s: want a refusal naming the key, got note %q", tt.key, tt.value, note)
			}
			if !strings.Contains(err.Error(), tt.wantIn) {
				t.Errorf("error = %q, want it to contain %q", err, tt.wantIn)
			}
			if spy.drove() != 0 {
				t.Errorf("a refused apply still drove the engine: %+v", spy)
			}
		})
	}
}

// rebindProbe stands in for the composition root's own rebind closure ([tui.Options.Rebind]): it
// records what the dispatcher drove it with, so the rebind-RIDING keys can be told apart from the
// pushed ones without an Agent or a server behind either.
type rebindProbe struct {
	calls []rebindCall
	err   error
}

// rebindCall is one drive: the model the session was bound to, and the observation it was re-driven
// with (0 until a beat has named a window).
type rebindCall struct {
	model  string
	window int
}

func (p *rebindProbe) rebind(model string, window int) (tui.RebindResult, error) {
	p.calls = append(p.calls, rebindCall{model: model, window: window})
	if p.err != nil {
		return tui.RebindResult{}, p.err
	}
	return tui.RebindResult{Model: model, ContextWindow: window}, nil
}

// The window pin has no engine setter of its own: it is a per-model binding, so a committed edit
// lands in the live holder and the whole per-model resolution is re-driven over it — the same door a
// heartbeat-observed model change goes through. Clearing the pin re-drives with the window the last
// beat reported, which is what keeps `0` meaning discover-live (ADR 0024) rather than "unknown".
func TestApplySettingContextWindowPinRidesTheRebind(t *testing.T) {
	t.Parallel()
	live := newLiveSettings(options{contextWindow: 4096}, nil)
	live.observe(8192) // what the last landed beat could name about the server's own window
	probe := &rebindProbe{}
	spy := &applySettingSpy{}
	apply := applySettingFor(settingsApplier{
		engine:  spy,
		live:    live,
		binding: func() upstreamBinding { return upstreamBinding{Model: "bound-model"} },
		rebind:  probe.rebind,
	})

	note, err := apply("context-window", "32768")
	if err != nil {
		t.Fatalf("apply context-window: %v", err)
	}
	if note != "" {
		t.Errorf("note = %q, want none: the pin is in force the moment the rebind commits", note)
	}
	if live.pin() != 32768 {
		t.Errorf("pin = %d, want the edited 32768", live.pin())
	}
	if want := []rebindCall{{model: "bound-model", window: 8192}}; !slices.Equal(probe.calls, want) {
		t.Fatalf("rebind drives = %+v, want %+v", probe.calls, want)
	}

	if _, err := apply("context-window", "0"); err != nil {
		t.Fatalf("apply context-window=0: %v", err)
	}
	if live.pin() != 0 {
		t.Errorf("pin = %d, want 0 — the cleared pin hands the window back to the server", live.pin())
	}
	if len(probe.calls) != 2 || probe.calls[1].window != 8192 {
		t.Errorf("rebind drives = %+v, want a second drive carrying the observed 8192", probe.calls)
	}
	if spy.drove() != 0 {
		t.Errorf("a rebind-riding key drove an anytime-safe mutator: %+v", spy)
	}
}

// Before a server is bound there is no model to rebind FOR (ADR 0036 decision 3 opens the pane on a
// session that has none). The edit is still recorded in the holder — the first beat's rebind resolves
// it in — and the row is told nothing, because nothing failed.
func TestApplySettingRideIsSilentBeforeAServerIsBound(t *testing.T) {
	t.Parallel()
	live := newLiveSettings(options{}, nil)
	probe := &rebindProbe{}
	apply := applySettingFor(settingsApplier{
		engine:  &applySettingSpy{},
		live:    live,
		binding: func() upstreamBinding { return upstreamBinding{} },
		rebind:  probe.rebind,
	})

	note, err := apply("context-window", "16384")
	if err != nil || note != "" {
		t.Fatalf("apply context-window unbound = (%q, %v), want it to land quietly", note, err)
	}
	if len(probe.calls) != 0 {
		t.Errorf("rebind drives = %+v, want none: nothing is bound to rebind", probe.calls)
	}
	if live.pin() != 16384 {
		t.Errorf("pin = %d, want the edit held at 16384 for the first bind", live.pin())
	}
}

// A refused rebind — Agent.Rebind is idle-only, so an open Exchange is one — is reported rather than
// swallowed: the file already says the new value, so the honest answer is that the session has not
// taken it yet. The holder keeps the edit, which is what makes a re-committed edit a retry.
func TestApplySettingReportsARefusedRebind(t *testing.T) {
	t.Parallel()
	live := newLiveSettings(options{}, nil)
	probe := &rebindProbe{err: errors.New("input pending")}
	apply := applySettingFor(settingsApplier{
		engine:  &applySettingSpy{},
		live:    live,
		binding: func() upstreamBinding { return upstreamBinding{Model: "bound-model"} },
		rebind:  probe.rebind,
	})

	if _, err := apply("context-window", "16384"); err == nil {
		t.Fatal("apply context-window: want the rebind's refusal, got none")
	}
	if live.pin() != 16384 {
		t.Errorf("pin = %d, want the persisted 16384 kept for the retry", live.pin())
	}
}

// The three `system-prompt-*` keys are ONE prompt (ADR 0023), and `system-prompt-models:` is a map no
// single string spells — so the apply re-READS the block the pane just wrote and lets the rebind
// re-resolve it per model, exactly as startup does. The spec the rebind builds is the assertion: it
// is what the engine is handed.
func TestApplySettingSystemPromptReResolvesFromTheFile(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	roots, err := resolveRoots(home, t.TempDir())
	if err != nil {
		t.Fatalf("resolveRoots: %v", err)
	}
	path := filepath.Join(roots.config, "config.yaml")
	launchOpts := options{systemPrompt: systemPromptSettings{
		global: promptSource{text: "the launch prompt"},
	}}
	live := newLiveSettings(launchOpts, nil)

	// The rebind closure the composition root wires: it re-resolves through the holder, so what the
	// dispatcher installed there is what the spec carries.
	var spec apogee.RebindSpec
	rebind := func(model string, window int) (tui.RebindResult, error) {
		base, manualIDs, pinnedWindow := live.rebindInputs(launchOpts)
		got, _, err := rebindSpecFor(base, roots, manualIDs, model, window, pinnedWindow)
		if err != nil {
			return tui.RebindResult{}, err
		}
		spec = got
		return tui.RebindResult{Model: got.Model, ContextWindow: got.MaxContextTokens}, nil
	}
	apply := applySettingFor(settingsApplier{
		engine:     &applySettingSpy{},
		live:       live,
		binding:    func() upstreamBinding { return upstreamBinding{Model: "bound-model"} },
		rebind:     rebind,
		configPath: path,
	})

	// What the pane's write left behind, then the apply that follows it.
	writeSettingsFixture(t, path, "system-prompt-text: the edited prompt\n")
	if _, err := apply("system-prompt-text", "the edited prompt"); err != nil {
		t.Fatalf("apply system-prompt-text: %v", err)
	}
	if spec.SystemPrompt != "the edited prompt" {
		t.Errorf("RebindSpec.SystemPrompt = %q, want the re-read %q", spec.SystemPrompt, "the edited prompt")
	}

	// A per-model entry is the same round trip: the map cannot travel as a value, so only the re-read
	// can carry it.
	writeSettingsFixture(t, path, "system-prompt-models:\n  bound-model:\n    system-prompt-text: the per-model prompt\n")
	if _, err := apply("system-prompt-models", ""); err != nil {
		t.Fatalf("apply system-prompt-models: %v", err)
	}
	if spec.SystemPrompt != "the per-model prompt" {
		t.Errorf("RebindSpec.SystemPrompt = %q, want the per-model entry to win", spec.SystemPrompt)
	}

	// Validate-then-commit: a block the file cannot express never displaces a prompt that works.
	writeSettingsFixture(t, path, "system-prompt-text: both\nsystem-prompt-file: both.md\n")
	if _, err := apply("system-prompt-file", "both.md"); err == nil {
		t.Fatal("apply of a contradictory block: want the refusal, got none")
	}
	if _, err := apply("context-window", "0"); err != nil {
		t.Fatalf("re-drive after the refusal: %v", err)
	}
	if spec.SystemPrompt != "the per-model prompt" {
		t.Errorf("RebindSpec.SystemPrompt = %q, want the last GOOD block still installed", spec.SystemPrompt)
	}
}

// writeSettingsFixture writes a config.yaml the way the pane's splice writer leaves one behind.
func writeSettingsFixture(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// A search endpoint is a tool's configuration, not a change of WHICH tools exist — so while
// web_search is registered (the ordinary case: the built-in set always carries it) the apply is a
// write on the tool the registry already holds. The engine hears nothing, which is what lets the
// endpoint move mid-run.
func TestApplySettingWebSearchEndpointMovesTheRegisteredTool(t *testing.T) {
	t.Parallel()
	registry := tools.NewDefaultRegistryWithHost(t.TempDir(), tools.HostTools{})
	spy := &applySettingSpy{}
	apply := applySettingFor(settingsApplier{
		engine: spy,
		tools:  newLiveTools(registry, func(string) *apogee.ToolRegistry { return apogee.NewToolRegistry() }),
	})

	if _, err := apply("web-search-endpoint", "https://search.example.com/s"); err != nil {
		t.Fatalf("apply web-search-endpoint: %v", err)
	}
	found, ok := registry.Lookup("web_search")
	if !ok {
		t.Fatal("web_search left the registry; the endpoint move must not rebuild the set")
	}
	if _, ok := found.(*tools.WebSearch); !ok {
		t.Fatalf("web_search is a %T; the registry must still hold the tool that was re-pointed", found)
	}
	if spy.drove() != 0 {
		t.Errorf("re-pointing a registered tool drove the engine: %+v", spy)
	}
}

// The swap door is the OTHER case: a set with no web_search to re-point cannot answer the edit in
// place, so the whole set is rebuilt and handed to the engine (ADR 0037 binding F). The rebuilt set
// becomes the live one, which is what makes the NEXT edit an in-place move again — a root still
// looking up tools in the swapped-out registry would be re-pointing an object nothing dispatches to.
func TestApplySettingWebSearchEndpointSwapsWhenTheToolIsAbsent(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	var built []string
	spy := &applySettingSpy{}
	live := newLiveTools(apogee.NewToolRegistry(), func(endpoint string) *apogee.ToolRegistry {
		built = append(built, endpoint)
		return tools.NewDefaultRegistryWithHost(workspace, tools.HostTools{WebSearchEndpoint: endpoint})
	})
	apply := applySettingFor(settingsApplier{engine: spy, tools: live})

	if _, err := apply("web-search-endpoint", "https://first.example.com/s"); err != nil {
		t.Fatalf("apply web-search-endpoint: %v", err)
	}
	if want := []string{"https://first.example.com/s"}; !slices.Equal(built, want) {
		t.Fatalf("rebuilds = %v, want %v", built, want)
	}
	if len(spy.swaps) != 1 {
		t.Fatalf("SwapTools calls = %d, want 1: an absent tool is a set-level change", len(spy.swaps))
	}
	if _, ok := spy.swaps[0].Lookup("web_search"); !ok {
		t.Error("the swapped-in registry has no web_search; the rebuild must carry the tool the edit is about")
	}

	// The second edit finds web_search in the set the first one installed, so it moves in place.
	if _, err := apply("web-search-endpoint", "https://second.example.com/s"); err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if len(built) != 1 || len(spy.swaps) != 1 {
		t.Fatalf("the second edit rebuilt again (rebuilds %v, swaps %d); the live set must be the swapped-in one", built, len(spy.swaps))
	}
}

// SwapTools is idle-only, so a run in flight refuses it. The refusal is REPORTED — the row renders it
// over a value the file already carries (binding A) — and the session stays on the set it had, which
// is what makes re-committing the edit a retry rather than a second half-application.
func TestApplySettingWebSearchSwapRefusalKeepsTheOldSet(t *testing.T) {
	t.Parallel()
	old := apogee.NewToolRegistry()
	spy := &applySettingSpy{swapErr: errors.New("input pending: the tool set can only be swapped between runs")}
	live := newLiveTools(old, func(endpoint string) *apogee.ToolRegistry {
		return tools.NewDefaultRegistryWithHost(t.TempDir(), tools.HostTools{WebSearchEndpoint: endpoint})
	})
	apply := applySettingFor(settingsApplier{engine: spy, tools: live})

	if _, err := apply("web-search-endpoint", "off"); err == nil {
		t.Fatal("apply web-search-endpoint: want the engine's refusal, got none")
	}
	if live.current != old {
		t.Error("a refused swap still became the live set; the session must keep the tools it had")
	}
}

// `use-project-skills` moves WHICH dirs are skill sources, so the apply re-points the shared Provider
// and re-scans: the flag is not something a catalogue already loaded can answer. One Provider feeds
// the loop and the "/" menu (ADR 0032), so both see the same set the moment the edit lands.
func TestApplySettingUseProjectSkillsRescansTheSources(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	roots, err := resolveRoots(t.TempDir(), workspace)
	if err != nil {
		t.Fatalf("resolveRoots: %v", err)
	}
	writeSkillFixture(t, filepath.Join(workspace, "skills", "project-only"),
		"---\nid: project-only\nsummary: from the bare project folder\n---\nbody")

	provider := skills.NewProvider(skills.Sources{
		Home:             roots.config,
		Workspace:        roots.workspace,
		UseProjectSkills: true,
	})
	if _, ok := provider.Get("project-only"); !ok {
		t.Fatal("the fixture skill is not discovered with the flag on; the test proves nothing")
	}
	apply := applySettingFor(settingsApplier{engine: &applySettingSpy{}, skills: provider, roots: roots})

	if _, err := apply("use-project-skills", "false"); err != nil {
		t.Fatalf("apply use-project-skills=false: %v", err)
	}
	if _, ok := provider.Get("project-only"); ok {
		t.Error("the project skill still resolves with the flag off; the re-scan did not happen")
	}

	if _, err := apply("use-project-skills", "true"); err != nil {
		t.Fatalf("apply use-project-skills=true: %v", err)
	}
	if _, ok := provider.Get("project-only"); !ok {
		t.Error("the project skill did not come back with the flag on again")
	}
}

// The `llama-launcher:` key runs the startup ladder a second time and stores what it resolves —
// which IS the apply, since every verb re-reads the config file for itself. A value the ladder
// refuses never displaces the path the session is working from.
func TestApplySettingLlamaLauncherSwapsThePath(t *testing.T) {
	t.Parallel()
	named := filepath.Join(t.TempDir(), "launcher.yaml")
	path := newLauncherPath("/startup/launcher.yaml")
	apply := applySettingFor(settingsApplier{launcher: path})

	if note, err := apply("llama-launcher", named); err != nil || note != "" {
		t.Fatalf("apply llama-launcher=%s: (%q, %v); want no note and no error", named, note, err)
	}
	if got := path.get(); got != named {
		t.Errorf("path = %q; want %q — the next verb reads the config the key now names", got, named)
	}

	if _, err := apply("llama-launcher", "off"); err != nil {
		t.Fatalf("apply llama-launcher=off: %v", err)
	}
	if got := path.get(); got != "" {
		t.Errorf("path after off = %q; want empty — the verbs report the integration off from here", got)
	}

	if _, err := apply("llama-launcher", "http://box:7331"); err == nil {
		t.Error("a URL was accepted; want the startup validator's refusal — this key takes a local path")
	}
	if got := path.get(); got != "" {
		t.Errorf("a refused value moved the path to %q; want the last good value kept", got)
	}
}

// A committed `present.` key rebuilds the ladder exactly as startup built it and re-installs it, so
// the presenter the engine captured walks the new rungs from the next presentation (ADR 0037). A
// value the block refuses changes nothing.
func TestApplySettingPresentRebuildsTheLadder(t *testing.T) {
	t.Parallel()
	var installed []tui.Presentation
	live := newLivePresentation(
		presentSettings{autoOpen: true}, t.TempDir(), "darwin",
		func(string) string { return "" }, // no SSH: a local session, so rungs 1/3
		func(p tui.Presentation) { installed = append(installed, p) })
	apply := applySettingFor(settingsApplier{present: live})

	if len(installed) != 1 || installed[0].Opener == nil {
		t.Fatalf("startup installed %+v; want one ladder carrying the opener", installed)
	}

	if _, err := apply("present.command", "zed {path}"); err != nil {
		t.Fatalf("apply present.command: %v", err)
	}
	if len(installed) != 2 || installed[1].Opener == nil || installed[1].Opener.CommandOverride != "zed {path}" {
		t.Fatalf("after present.command the ladder is %+v; want an opener carrying the override", installed)
	}

	if _, err := apply("present.auto-open", "false"); err != nil {
		t.Fatalf("apply present.auto-open: %v", err)
	}
	if len(installed) != 3 || installed[2].Opener != nil {
		t.Fatalf("after auto-open=false the ladder is %+v; want no opener at all", installed)
	}

	// The block's own validate, run before anything is installed: a port no server could bind is
	// refused here rather than deep inside the first presentation.
	for _, value := range []string{"not a number", "70000"} {
		if _, err := apply("present.port", value); err == nil {
			t.Errorf("present.port=%q was accepted; want the startup check's refusal", value)
		}
	}
	if len(installed) != 3 {
		t.Errorf("a refused value installed a ladder: %+v", installed[3:])
	}
}

// The doc server's listener follows its ADDRESS and nothing else (ADR 0037 binding D): an edit that
// leaves the address alone keeps the bound listener and every URL it issued, while a port change
// closes it — the URLs die with it — and the next presentation binds the new port.
func TestPresentPortEditRebindsTheDocServer(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	doc := filepath.Join(workspace, "report.html")
	if err := os.WriteFile(doc, []byte("<h1>report</h1>"), 0o600); err != nil {
		t.Fatalf("write doc: %v", err)
	}

	var installed []tui.Presentation
	live := newLivePresentation(
		presentSettings{autoOpen: true}, workspace, "linux",
		// An SSH session: remote, so the ladder wires rung 2 and advertises the server-side address.
		func(name string) string {
			if name == "SSH_CONNECTION" {
				return "10.0.0.1 51000 10.0.0.2 22"
			}
			return ""
		},
		func(p tui.Presentation) { installed = append(installed, p) })
	t.Cleanup(live.close)

	first := installed[0].Docs
	if first == nil {
		t.Fatal("a remote session wired no doc server; want rung 2")
	}
	url, err := first.Serve(doc)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}

	// An edit that does not touch the address: the same server, still bound, still holding its grant.
	if err := live.apply("present.command", "zed {path}"); err != nil {
		t.Fatalf("apply present.command: %v", err)
	}
	if installed[1].Docs != first {
		t.Error("an unrelated edit displaced the doc server; want the bound listener and its URLs kept")
	}
	if _, err := first.Serve(doc); err != nil {
		t.Errorf("serve after an unrelated edit: %v; want the server still running", err)
	}

	port := freePort(t)
	if err := live.apply("present.port", strconv.Itoa(port)); err != nil {
		t.Fatalf("apply present.port: %v", err)
	}
	next := installed[2].Docs
	if next == nil || next == first {
		t.Fatalf("present.port installed %v; want a doc server on the new address", next)
	}
	if _, err := first.Serve(doc); err == nil {
		t.Errorf("the displaced server still serves %q; want it closed with the port change", url)
	}
	moved, err := next.Serve(doc)
	if err != nil {
		t.Fatalf("serve on the new port: %v", err)
	}
	if want := ":" + strconv.Itoa(port) + "/"; !strings.Contains(moved, want) {
		t.Errorf("URL = %q; want it served from %q", moved, want)
	}
}

// freePort reserves an ephemeral port, releases it, and returns it — a port the doc server can bind
// on its own without the test having to know one the machine happens to have free.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve a port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if err := ln.Close(); err != nil {
		t.Fatalf("release the reserved port: %v", err)
	}
	return port
}

// writeSkillFixture writes one SKILL.md into dir, the shape internal/skills discovers.
func writeSkillFixture(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o600); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
}

// The seam is wired at the composition root, over this run's resolved context-file names: without it
// the pane would persist and apply nothing, which is the Driver degrade and not what the binary
// composes (ADR 0031).
func TestRunRootWiresTheLiveApplySeam(t *testing.T) {
	t.Parallel()
	rec := &recordingLauncher{}
	opts := options{
		endpoint:     "http://127.0.0.1:1111",
		model:        "fake",
		mode:         "ask-before",
		workspace:    t.TempDir(),
		configDir:    t.TempDir(),
		contextFiles: []string{"AGENTS.md"},
	}
	if err := runRoot(context.Background(), opts, rec.launch); err != nil {
		t.Fatalf("runRoot: %v", err)
	}
	if rec.opts.ApplySetting == nil {
		t.Fatal("tui.Options.ApplySetting is nil; the composition root did not wire the dispatcher")
	}
	note, err := rec.opts.ApplySetting("context-files.enable", "true")
	if err != nil {
		t.Fatalf("ApplySetting: %v", err)
	}
	if note != contextFileNote {
		t.Errorf("note = %q, want %q", note, contextFileNote)
	}
	// The two keys whose seam is a live OBJECT rather than an engine call — the tool registry the
	// root holds and the shared skill Provider — so an unwired member would panic rather than
	// degrade, and this is where the wiring is proved.
	if _, err := rec.opts.ApplySetting("web-search-endpoint", "off"); err != nil {
		t.Errorf("ApplySetting(web-search-endpoint): %v", err)
	}
	if _, err := rec.opts.ApplySetting("use-project-skills", "false"); err != nil {
		t.Errorf("ApplySetting(use-project-skills): %v", err)
	}
	if _, err := rec.opts.ApplySetting("servers", "anything"); err == nil {
		t.Error("a key with no live seam applied silently; want a refusal naming it")
	}
}

// The anytime-safe mutators are REMEMBERED while the session has no engine and applied the moment one
// is constructed — the SetMode posture, for the same reason: the settings pane is open before a server
// is chosen (ADR 0036 decision 3), and an edit that persisted must not be the only half that happened.
// A key never moved here leaves the Agent on the seed its Config carried.
func TestLateEngineRemembersSettingsMovedBeforeTheBind(t *testing.T) {
	t.Parallel()
	e := newLateEngine(modeAskBefore, true)

	e.SetBypass(true)
	e.SetCompactionEnabled(false)
	e.SetContextFiles(true, []string{"AGENTS.md"})

	if e.pendingBypass == nil || !*e.pendingBypass {
		t.Errorf("pendingBypass = %v, want true held for the bind", e.pendingBypass)
	}
	if e.pendingCompaction == nil || *e.pendingCompaction {
		t.Errorf("pendingCompaction = %v, want false held for the bind", e.pendingCompaction)
	}
	if e.pendingContextFiles == nil || !e.pendingContextFiles.enable {
		t.Fatalf("pendingContextFiles = %+v, want the pair held for the bind", e.pendingContextFiles)
	}
	if want := []string{"AGENTS.md"}; !slices.Equal(e.pendingContextFiles.names, want) {
		t.Errorf("pending names = %v, want %v", e.pendingContextFiles.names, want)
	}

	// A holder nothing moved holds nothing: the Agent is then constructed from its Config alone.
	fresh := newLateEngine(modeAskBefore, true)
	if fresh.pendingBypass != nil || fresh.pendingCompaction != nil || fresh.pendingContextFiles != nil {
		t.Errorf("a fresh holder already carries overrides: %+v", fresh)
	}
}
