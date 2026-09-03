package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/daemon"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/scheme"
	"github.com/airiclenz/apogee/internal/tools"
	"github.com/airiclenz/apogee/internal/tui"
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
		{name: "auto + confine + fs-write → pre-warm", mode: domain.ModeAuto, confine: true, fsWrite: true, wantPrewarm: true},
		{name: "auto + confine + no fs-write → off (degradation's territory)", mode: domain.ModeAuto, confine: true, fsWrite: false, wantPrewarm: false},
		{name: "auto UNCONFINED → off", mode: domain.ModeAuto, confine: false, fsWrite: true, wantPrewarm: false},
		{name: "ask-before + confine + fs-write → off (not Auto)", mode: domain.ModeAskBefore, confine: true, fsWrite: true, wantPrewarm: false},
		{name: "allow-edits + confine + fs-write → off (not Auto)", mode: domain.ModeAllowEdits, confine: true, fsWrite: true, wantPrewarm: false},
		{name: "plan + confine + fs-write → off (not Auto)", mode: domain.ModePlan, confine: true, fsWrite: true, wantPrewarm: false},
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

// TestCaptureStderrRestoresOnGoexit pins the restore on the exit path the helper used to leak. A
// t.Fatal or t.Skip inside the wrapped call is a runtime.Goexit, which unwinds past the happy-path
// restore exactly as a panic does — t.Skip is that path with no failure to swallow. After the
// subtest returns, the process stderr must be the real one again and the reader goroutine must have
// ended; otherwise every later test writes its diagnostics into a pipe nobody reads.
func TestCaptureStderrRestoresOnGoexit(t *testing.T) {
	// Deliberately NOT parallel: captureStderr swaps the process-global os.Stderr.
	orig := os.Stderr
	before := runtime.NumGoroutine()

	t.Run("the wrapped call bails", func(sub *testing.T) {
		captureStderr(sub, func() { sub.Skip("bail") })
		sub.Fatal("captureStderr returned after a Goexit inside the wrapped call")
	})

	if os.Stderr != orig {
		t.Errorf("os.Stderr = %v after a bailed capture; want the original %v", os.Stderr, orig)
	}
	// The reader ends once the cleanup closes the write end. Poll for `<=`, never `==`: other tests'
	// background goroutines (httptest idle connections) wind down asynchronously and an equality flakes.
	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > before {
		if time.Now().After(deadline) {
			t.Errorf("goroutines = %d after a bailed capture; want <= the %d before it",
				runtime.NumGoroutine(), before)
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
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
			opts := config.Options{
				Endpoint:      "http://127.0.0.1:1111",
				Model:         "fake",
				Mode:          "ask-before",
				Workspace:     t.TempDir(),
				ContextWindow: tt.contextWindow,
				AutoCompact:   true,
			}

			if err := runRoot(context.Background(), opts, rec.launch); err != nil {
				t.Fatalf("runRoot: %v", err)
			}
			if rec.opts.ContextWindow != tt.contextWindow {
				t.Errorf("tui.Options.ContextWindow = %d; want the threaded %d", rec.opts.ContextWindow, tt.contextWindow)
			}
			if !serverActsOf(rec.opts).CanRebind {
				t.Fatal("tui.ServerActs.CanRebind is false; the composition root did not wire the rebind closure")
			}
			result, err := rec.opts.Server.Rebind("fake", tt.observed, provider.EffortDialectNone)
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
		sp   config.SystemPromptSettings
		// wantErr are the substrings the surfaced error must carry.
		wantErr []string
	}{
		{
			name:    "an unreadable global file",
			sp:      config.SystemPromptSettings{Global: config.PromptSource{File: filepath.Join("prompts", "absent-prompt.md")}},
			wantErr: []string{"system-prompt-file", "absent-prompt.md"},
		},
		{
			name:    "an unknown placeholder in the inline prompt",
			sp:      config.SystemPromptSettings{Global: config.PromptSource{Text: "hi {{bogus}}"}},
			wantErr: []string{"system-prompt-text", "{{bogus}}", "{{workspace}}"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := &recordingLauncher{}
			opts := config.Options{
				Endpoint:     "http://127.0.0.1:1111",
				Model:        "fake",
				Mode:         "ask-before",
				Workspace:    t.TempDir(),
				ConfigDir:    t.TempDir(), // the apogee home a relative prompt path resolves against
				SystemPrompt: tt.sp,
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
		opts := config.Options{
			Endpoint:     "http://127.0.0.1:1111",
			Model:        "fake",
			Mode:         "ask-before",
			Workspace:    workspace,
			ConfigDir:    t.TempDir(),
			ContextFiles: []string{"AGENTS.md", "docs/absent.md"},
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
		opts := config.Options{
			Endpoint:     "http://127.0.0.1:1111",
			Model:        "fake",
			Mode:         "ask-before",
			Workspace:    t.TempDir(),
			ConfigDir:    t.TempDir(),
			ContextFiles: []string{filepath.Join("..", "outside.md")},
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
		ui   config.UISettings
	}{
		{name: "the resolved default: snake with the colour loop on", ui: config.UISettings{Spinner: tui.SpinnerSnake, SpinnerColor: true, ShowScrollbar: true, ColorScheme: "dark"}},
		{name: "a named style with the loop off travels as both", ui: config.UISettings{Spinner: tui.SpinnerGlitter, SpinnerColor: false, ShowScrollbar: true, ColorScheme: "dark"}},
		{name: "classic with the loop on — the old glyphs, the new colours", ui: config.UISettings{Spinner: tui.SpinnerClassic, SpinnerColor: true, ShowScrollbar: true, ColorScheme: "dark"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := &recordingLauncher{}
			opts := config.Options{
				Endpoint:  "http://127.0.0.1:1111",
				Model:     "fake",
				Mode:      "ask-before",
				Workspace: t.TempDir(),
				UI:        tt.ui,
			}
			if err := runRoot(context.Background(), opts, rec.launch); err != nil {
				t.Fatalf("runRoot: %v", err)
			}
			if rec.opts.Spinner != tt.ui.Spinner {
				t.Errorf("tui.Options.Spinner = %q; want the resolved %q", rec.opts.Spinner, tt.ui.Spinner)
			}
			if rec.opts.SpinnerColor != tt.ui.SpinnerColor {
				t.Errorf("tui.Options.SpinnerColor = %v; want the resolved %v", rec.opts.SpinnerColor, tt.ui.SpinnerColor)
			}
		})
	}
}

// The `ui.color-scheme:` key reaches the renderer RESOLVED: runRoot reads the schemes folder under
// the apogee home this run uses and hands tui.Options the palette itself, plus the name it loaded
// under and whatever the load cost. Reading files is the composition root's job — the renderer is
// handed colours, never a path.
//
// The two cases are the two halves of the forgiving contract (ADR 0040 design call 8): a user file
// SHADOWS the built-in it shares a name with, and a name nothing answers to costs a warning that
// says which name rather than the session.
func TestRunRootResolvesTheColorScheme(t *testing.T) {
	t.Parallel()

	t.Run("a user file shadows the built-in of the same name", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		if err := os.MkdirAll(filepath.Join(home, "schemes"), 0o700); err != nil {
			t.Fatalf("create schemes dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(home, "schemes", "dark.yaml"),
			[]byte("error: \"#010203\"\n"), 0o600); err != nil {
			t.Fatalf("write scheme: %v", err)
		}

		rec := &recordingLauncher{}
		opts := config.Options{
			Endpoint:  "http://127.0.0.1:1111",
			Model:     "fake",
			Mode:      "ask-before",
			ConfigDir: home,
			Workspace: t.TempDir(),
			UI:        config.UISettings{Spinner: tui.SpinnerSnake, SpinnerColor: true, ShowScrollbar: true, ColorScheme: "dark"},
		}
		if err := runRoot(context.Background(), opts, rec.launch); err != nil {
			t.Fatalf("runRoot: %v", err)
		}
		if got := rec.opts.ColorScheme.Error; got != "#010203" {
			t.Errorf("tui.Options.ColorScheme.Error = %q; want the user file's %q", got, "#010203")
		}
		// The keys the file leaves out are the built-in's, which is what makes a two-line scheme a
		// usable one.
		if got := rec.opts.ColorScheme.Surface; got != scheme.Default().Surface {
			t.Errorf("tui.Options.ColorScheme.Surface = %q; want the default %q", got, scheme.Default().Surface)
		}
		if rec.opts.ColorSchemeName != "dark" {
			t.Errorf("tui.Options.ColorSchemeName = %q; want %q", rec.opts.ColorSchemeName, "dark")
		}
		if len(rec.opts.ColorSchemeWarnings) != 0 {
			t.Errorf("a well-formed scheme warned: %v", rec.opts.ColorSchemeWarnings)
		}
	})

	t.Run("an unknown name keeps the default palette and says so", func(t *testing.T) {
		t.Parallel()
		rec := &recordingLauncher{}
		opts := config.Options{
			Endpoint:  "http://127.0.0.1:1111",
			Model:     "fake",
			Mode:      "ask-before",
			ConfigDir: t.TempDir(),
			Workspace: t.TempDir(),
			UI:        config.UISettings{Spinner: tui.SpinnerSnake, SpinnerColor: true, ShowScrollbar: true, ColorScheme: "no-such-scheme"},
		}
		if err := runRoot(context.Background(), opts, rec.launch); err != nil {
			t.Fatalf("runRoot refused an unknown colour scheme: %v", err)
		}
		if rec.opts.ColorScheme != scheme.Default() {
			t.Errorf("tui.Options.ColorScheme = %+v; want the default palette", rec.opts.ColorScheme)
		}
		if len(rec.opts.ColorSchemeWarnings) != 1 {
			t.Fatalf("warnings = %v; want exactly one naming the scheme", rec.opts.ColorSchemeWarnings)
		}
		if !strings.Contains(rec.opts.ColorSchemeWarnings[0], "no-such-scheme") {
			t.Errorf("warning = %q; want it to name the scheme that was asked for", rec.opts.ColorSchemeWarnings[0])
		}
	})

	// The live half (ADR 0040 design call 7): the settings picker's vocabulary and the resolve
	// behind an answer to it. Both are asked AFTER launch, over a folder written after launch, which
	// is the property that matters — a list or a palette captured at start-up would leave a human who
	// has just written a scheme file unable to pick it without restarting.
	t.Run("the picker seams read the schemes folder on every ask", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		rec := &recordingLauncher{}
		opts := config.Options{
			Endpoint:  "http://127.0.0.1:1111",
			Model:     "fake",
			Mode:      "ask-before",
			ConfigDir: home,
			Workspace: t.TempDir(),
			UI:        config.UISettings{Spinner: tui.SpinnerSnake, SpinnerColor: true, ShowScrollbar: true, ColorScheme: "dark"},
		}
		if err := runRoot(context.Background(), opts, rec.launch); err != nil {
			t.Fatalf("runRoot: %v", err)
		}
		if rec.opts.Schemes == nil {
			t.Fatal("the colour-scheme host is unwired; the settings row could not switch anything")
		}

		// The folder does not even exist yet at launch: the built-ins are still offered.
		if names := rec.opts.Schemes.List(); len(names) == 0 {
			t.Fatal("Schemes.List offered nothing; the built-ins are always available")
		}
		if err := os.MkdirAll(filepath.Join(home, "schemes"), 0o700); err != nil {
			t.Fatalf("create schemes dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(home, "schemes", "mine.yaml"),
			[]byte("error: \"#010203\"\nbackdrop: \"#ffffff\"\n"), 0o600); err != nil {
			t.Fatalf("write scheme: %v", err)
		}
		if names := rec.opts.Schemes.List(); !slices.Contains(names, "mine") {
			t.Errorf("Schemes.List = %v after the file was written; want it to name \"mine\"", names)
		}

		// And resolving it reads that same file — with its unknown key rendered to a line the
		// renderer prints rather than a scheme.Warning it would have to format.
		s, warnings, ok := rec.opts.Schemes.Resolve("mine")
		if !ok {
			t.Fatal("the wired colour-scheme host cannot resolve; the settings row could not switch anything")
		}
		if s.Error != "#010203" {
			t.Errorf("Schemes.Resolve(\"mine\").Error = %q; want the file's %q", s.Error, "#010203")
		}
		if len(warnings) != 1 || !strings.Contains(warnings[0], "backdrop") {
			t.Errorf("warnings = %v; want one rendered line naming the unknown key", warnings)
		}
	})

	// The third seam of the same folder: `/color-scheme export` is the only way a scheme file comes
	// into existence, since the built-ins are embedded and never installed. It must write into the
	// folder the OTHER two read, or an exported scheme would be invisible to the picker that is
	// supposed to offer it.
	t.Run("export writes into the folder the picker reads", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		rec := &recordingLauncher{}
		opts := config.Options{
			Endpoint:  "http://127.0.0.1:1111",
			Model:     "fake",
			Mode:      "ask-before",
			ConfigDir: home,
			Workspace: t.TempDir(),
			UI:        config.UISettings{Spinner: tui.SpinnerSnake, SpinnerColor: true, ShowScrollbar: true, ColorScheme: "dark"},
		}
		if err := runRoot(context.Background(), opts, rec.launch); err != nil {
			t.Fatalf("runRoot: %v", err)
		}
		if rec.opts.Schemes == nil {
			t.Fatal("the colour-scheme host is unwired; no scheme file could ever be edited")
		}

		path, err := rec.opts.Schemes.Export("dark")
		if err != nil {
			t.Fatalf("Schemes.Export(\"dark\"): %v", err)
		}
		if want := filepath.Join(home, "schemes", "dark.yaml"); path != want {
			t.Errorf("Schemes.Export wrote %q, want %q — the picker reads that folder", path, want)
		}
		if names := rec.opts.Schemes.List(); !slices.Contains(names, "dark") {
			t.Errorf("Schemes.List = %v after the export; want the written copy to be offered", names)
		}
		// Never twice: an export that overwrote a scheme somebody had been editing would destroy it.
		if _, err := rec.opts.Schemes.Export("dark"); err == nil {
			t.Error("a second export overwrote the file; it must be refused")
		}
	})
}

// The three Auto startup lines are branches at the same site, and the first two never both fire:
// confine=false is the blanket-loosen WARNING, confine=true on an unfenceable backend is the
// degradation notice. The third is the residual disclosure — confine=true on a backend that CAN
// fence but leaves a write-class access open (landlock ABI 1–2 and truncate(2)) — which is the
// degradation notice's mirror in FSWrite and so can never accompany it.
//
// Every cell is dictated through the [newConfiner] seam rather than read off this machine's real
// backend: what a kernel can fence decides which branch speaks, so a host-derived expectation only
// ever drives the one cell that host happens to be in — and leaves the silent half of each branch,
// which is where a deleted print hides, unasserted on every machine.
func TestRunRootConfinementStartupNotices(t *testing.T) {
	// Deliberately NOT parallel: captureStderr swaps the process-global os.Stderr, and the confiner
	// seam below is package state.
	const (
		unconfinedWarning = "running UNCONFINED"
		degradedNotice    = "auto mode is gating terminal commands"
		residualNotice    = "cannot fence"
	)
	var (
		unfenceable = apogee.ConfinementCaps{}
		fenced      = apogee.ConfinementCaps{FSWrite: true}
		leakyFence  = apogee.ConfinementCaps{FSWrite: true, Residuals: []string{"truncate(2)"}}
	)

	tests := []struct {
		name         string
		mode         string
		confine      bool
		caps         apogee.ConfinementCaps
		wantWarning  bool
		wantDegraded bool
		wantResidual bool
	}{
		// The posture, not the backend: an acknowledged disposable host warns and says nothing else,
		// even where the backend it is not using has a hole in it.
		{name: "auto unconfined → warning only", mode: "auto", confine: false, caps: leakyFence,
			wantWarning: true},
		{name: "auto confined, a backend that cannot fence → the degradation notice", mode: "auto",
			confine: true, caps: unfenceable, wantDegraded: true},
		{name: "auto confined, a fence with a known hole → the residual disclosure", mode: "auto",
			confine: true, caps: leakyFence, wantResidual: true},
		{name: "auto confined, a fence with no known hole → silent", mode: "auto",
			confine: true, caps: fenced},
		{name: "ask-before makes no confinement promise → silent", mode: "ask-before",
			confine: true, caps: leakyFence},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prevConfiner := newConfiner
			newConfiner = func() apogee.Confiner { return fakeConfiner{caps: tt.caps} }
			t.Cleanup(func() { newConfiner = prevConfiner })

			rec := &recordingLauncher{}
			opts := config.Options{
				Endpoint:           "http://127.0.0.1:1111",
				Model:              "fake",
				Mode:               tt.mode,
				Workspace:          t.TempDir(),
				ConfineToWorkspace: tt.confine,
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
				t.Errorf("degradation notice present = %v; want %v (caps = %+v, stderr = %q)",
					got, tt.wantDegraded, tt.caps, stderr)
			}
			if got := strings.Contains(stderr, residualNotice); got != tt.wantResidual {
				t.Errorf("residual notice present = %v; want %v (caps = %+v, stderr = %q)",
					got, tt.wantResidual, tt.caps, stderr)
			}
			if tt.wantResidual && !strings.Contains(stderr, "truncate(2)") {
				t.Errorf("the disclosure does not name the access it is about: %q", stderr)
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
		cfg        config.PresentSettings
		env        map[string]string
		wantLocal  bool
		wantOpener bool
		wantDocs   bool
		wantHost   string
		wantPort   int
	}{
		{
			name:       "local desktop + auto-open → the opener, no server",
			cfg:        config.PresentSettings{AutoOpen: true},
			wantLocal:  true,
			wantOpener: true,
		},
		{
			name:       "local + a command override → the opener carries the template",
			cfg:        config.PresentSettings{AutoOpen: true, Command: "zed {path}"},
			wantLocal:  true,
			wantOpener: true,
		},
		{
			name:      "local + auto-open off → no mechanism at all (rung 0 still runs)",
			cfg:       config.PresentSettings{AutoOpen: false, Command: "zed {path}"},
			wantLocal: true,
		},
		{
			name:     "remote → the doc server, advertising the SSH server IP; never an opener",
			cfg:      config.PresentSettings{AutoOpen: true, Port: 8934},
			env:      map[string]string{"SSH_CONNECTION": devboxSSH},
			wantDocs: true,
			wantHost: "192.168.64.2",
			wantPort: 8934,
		},
		{
			name:     "remote with no SSH_CONNECTION → present.host answers instead",
			cfg:      config.PresentSettings{AutoOpen: true, Host: "devbox.internal"},
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
			if rungs.Opener != nil && rungs.Opener.CommandOverride != tt.cfg.Command {
				t.Errorf("Opener.CommandOverride = %q; want the configured %q", rungs.Opener.CommandOverride, tt.cfg.Command)
			}
			// Rung 1 resolves its own program (`open`, `xdg-open`, `cmd`) absolutely and refuses one
			// that resolves inside the workspace, so the rung is only wired with the root to measure
			// against — an unwired root would leave that fence quietly empty.
			if rungs.Opener != nil && rungs.Opener.WorkspaceRoot != workspace {
				t.Errorf("Opener.WorkspaceRoot = %q; want the workspace root %q", rungs.Opener.WorkspaceRoot, workspace)
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
	opts := config.Options{
		Endpoint:  "http://127.0.0.1:1111",
		Model:     "fake",
		Mode:      "ask-before",
		Workspace: workspace,
		Present:   config.PresentSettings{AutoOpen: true},
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

// The delegate step cap the boot phase hands the engine: a top-level file key (no per-server
// override), so what a session's Config carries is opts.DelegateMaxSteps verbatim — including the 0
// that means "unbounded", which is why this asserts a stated value rather than "non-zero". The
// Firing Driver's half of the same claim is TestFiringConfigSetsEveryUnattendedField.
func TestBootConfigCarriesTheDelegateStepCap(t *testing.T) {
	t.Parallel()
	for _, want := range []int{12, 0} {
		t.Run(strconv.Itoa(want), func(t *testing.T) {
			t.Parallel()
			opts := config.Options{
				Mode:             "ask-before",
				Workspace:        t.TempDir(),
				ConfigDir:        t.TempDir(),
				DelegateMaxSteps: want,
			}
			roots, err := resolveRoots(opts.ConfigDir, opts.Workspace)
			if err != nil {
				t.Fatalf("resolveRoots: %v", err)
			}
			w := newRootWiring(opts, apogee.ModeAskBefore, roots)
			t.Cleanup(w.close)
			if err := w.resolveConfig(); err != nil {
				t.Fatalf("resolveConfig: %v", err)
			}
			if w.cfg.Delegation.MaxSteps != want {
				t.Errorf("Config.Delegation.MaxSteps = %d; want the threaded %d", w.cfg.Delegation.MaxSteps, want)
			}
		})
	}
}

// The `prune-tool-results:` key reaches the engine seam it gates: the boot phase folds
// opts.PruneToolResults into ContextConfig.PruneToolResults verbatim, so a file that opts out
// (`prune-tool-results: false`) leaves an Agent that never prunes. It is threaded rather than
// re-resolved — Pruning is structural, so nothing but this key can turn it off. The Firing
// Driver's half of the same claim is TestFiringConfigSetsEveryUnattendedField.
func TestBootConfigCarriesThePruneToolResultsToggle(t *testing.T) {
	t.Parallel()
	for _, want := range []bool{true, false} {
		t.Run(strconv.FormatBool(want), func(t *testing.T) {
			t.Parallel()
			opts := config.Options{
				Mode:             "ask-before",
				Workspace:        t.TempDir(),
				ConfigDir:        t.TempDir(),
				PruneToolResults: want,
			}
			roots, err := resolveRoots(opts.ConfigDir, opts.Workspace)
			if err != nil {
				t.Fatalf("resolveRoots: %v", err)
			}
			w := newRootWiring(opts, apogee.ModeAskBefore, roots)
			t.Cleanup(w.close)
			if err := w.resolveConfig(); err != nil {
				t.Fatalf("resolveConfig: %v", err)
			}
			if w.cfg.Context.PruneToolResults != want {
				t.Errorf("Config.Context.PruneToolResults = %v; want the threaded %v",
					w.cfg.Context.PruneToolResults, want)
			}
		})
	}
}

// The six Floor-guard keys reach the engine the same way, negated once on the trip (ADR 0071): they
// are positive in the file and Disable… at the engine, so a key left at its `true` default has to
// arrive as a FALSE gate. The case that proves the negation is not a blanket one is a single key
// switched off while the other five stand.
func TestBootConfigCarriesTheFloorGuardKeys(t *testing.T) {
	t.Parallel()
	opts := config.Options{
		Mode:      "ask-before",
		Workspace: t.TempDir(),
		ConfigDir: t.TempDir(),
		// What a config with `tool-result-cap: false` and nothing else said resolves to.
		ToolUseEnforcer:       true,
		EmptyResponseRecovery: true,
		ToolCallRepair:        true,
		ToolLoopBreaker:       true,
		ToolResultCap:         false,
		ReadCache:             true,
	}
	roots, err := resolveRoots(opts.ConfigDir, opts.Workspace)
	if err != nil {
		t.Fatalf("resolveRoots: %v", err)
	}
	w := newRootWiring(opts, apogee.ModeAskBefore, roots)
	t.Cleanup(w.close)
	if err := w.resolveConfig(); err != nil {
		t.Fatalf("resolveConfig: %v", err)
	}
	want := apogee.FloorConfig{DisableToolResultCap: true}
	if w.cfg.Floor != want {
		t.Errorf("Config.Floor = %+v; want %+v — one key off, the other five guards standing",
			w.cfg.Floor, want)
	}
}

// The global rungs ride Config rather than the assembly alone so every Driver prunes and lifts the
// same roster from the same value (ADR 0057) — one row per Config assembly in the composition root:
// the session's boot phase, `apogee headless`, and a daemon Firing, each fed the same two lists and
// asked for the Config it hands the engine. Not parallel: the headless and Firing rows go through
// harnesses that replace package-level seams.
func TestEveryDriverHandsTheRosterRungsToTheConfig(t *testing.T) {
	disabled, enabled := []string{"view_diff"}, []string{"grep"}
	const rosterYAML = "tools:\n  disabled: [view_diff]\n  enabled: [grep]\n"
	tests := []struct {
		name     string
		assemble func(t *testing.T) apogee.Config
	}{
		{
			name: "the session's boot phase",
			assemble: func(t *testing.T) apogee.Config {
				opts := config.Options{
					Mode:          "ask-before",
					Workspace:     t.TempDir(),
					ConfigDir:     t.TempDir(),
					ToolsDisabled: disabled,
					ToolsEnabled:  enabled,
				}
				roots, err := resolveRoots(opts.ConfigDir, opts.Workspace)
				if err != nil {
					t.Fatalf("resolveRoots: %v", err)
				}
				w := newRootWiring(opts, apogee.ModeAskBefore, roots)
				t.Cleanup(w.close)
				if err := w.resolveConfig(); err != nil {
					t.Fatalf("resolveConfig: %v", err)
				}
				return w.cfg
			},
		},
		{
			name: "apogee headless",
			assemble: func(t *testing.T) apogee.Config {
				stub := &stubRunner{}
				if _, _, err := headlessRunOn(t, stub, fenceableHost, testConfigHome(t, rosterYAML), "explain this repo"); err != nil {
					t.Fatalf("headless: %v", err)
				}
				if !stub.called {
					t.Fatal("the runner did not run")
				}
				return stub.spec.Config
			},
		},
		{
			name: "a daemon Firing",
			assemble: func(t *testing.T) apogee.Config {
				harness := newDaemonFireHarness(t, config.Options{
					HostAlias:     "startup",
					Endpoint:      "http://startup.invalid",
					Servers:       []config.ServerEntry{{Name: "startup", Endpoint: "http://startup.invalid"}},
					ToolsDisabled: disabled,
					ToolsEnabled:  enabled,
				})
				return harness.fire(t, entryFor(t, "audit", daemon.Action{})).Config
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tt.assemble(t)

			if !slices.Equal(cfg.DisabledTools, disabled) {
				t.Errorf("Config.DisabledTools = %q, want %q", cfg.DisabledTools, disabled)
			}
			if !slices.Equal(cfg.EnabledTools, enabled) {
				t.Errorf("Config.EnabledTools = %q, want %q — the global lift never reached this Driver", cfg.EnabledTools, enabled)
			}
		})
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
			got, err := domain.ParseMode(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("domain.ParseMode(%q) = %q, nil; want error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("domain.ParseMode(%q) unexpected error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Fatalf("domain.ParseMode(%q) = %q; want %q", tt.in, got, tt.want)
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
