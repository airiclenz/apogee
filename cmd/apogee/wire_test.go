package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	"github.com/airiclenz/apogee/internal/mechanisms"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/scheme"
	"github.com/airiclenz/apogee/internal/session"
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

// The read-only mounts reach the assembly the same way the Presenter does, and drift the same way:
// registryWithMCP builds HostTools by hand, so a mount the engine's own build would have honoured
// would silently vanish for any session with an MCP server configured — the model could read a
// skill's bundled files in one session and not in another, for a reason nothing on screen explains.
func TestRegistryWithMCPThreadsExtraReadRoots(t *testing.T) {
	t.Parallel()
	extra := t.TempDir()
	if err := os.WriteFile(filepath.Join(extra, "SKILL.md"), []byte("bundled bytes"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	cfg := validCfg(t)
	cfg.ExtraReadRoots = func() []string { return []string{extra} }

	tool, ok := registryWithMCP(cfg.WorkspaceDir, cfg, nil).Lookup("read_file")
	if !ok {
		t.Fatal("read_file is missing from the MCP registry build")
	}
	result, err := tool.Execute(context.Background(), apogee.ToolCall{
		ID:        "c1",
		Tool:      "read_file",
		Arguments: []byte(`{"path":` + strconv.Quote(filepath.Join(extra, "SKILL.md")) + `}`),
	})
	if err != nil {
		t.Fatalf("read_file returned a Go error: %v", err)
	}
	if result.IsError || !strings.Contains(result.Content, "bundled bytes") {
		t.Errorf("read under the mounted root failed: %q — the MCP build dropped ExtraReadRoots", result.Content)
	}
}

// The `url-safety:` hosts reach the assembly the same way, and they are the one field where the
// hand-assembly drifting apart from the engine's own is a SECURITY regression rather than a missing
// convenience: configuring an MCP server would re-open a host the operator denied, in a session
// that looks identical to one where the denial holds. The engine-side half of the same guarantee is
// TestHostToolsBuildsTheURLGuardFromTheConfiguredHosts (internal/agent); this is its mirror.
//
// The deny is spelled as a human writes one into config.yaml, so the normalisation has to survive
// this path too — and web_fetch is driven rather than the guard inspected, because what the
// operator is promised is that the TOOL refuses.
func TestRegistryWithMCPThreadsURLSafetyHosts(t *testing.T) {
	t.Parallel()
	cfg := validCfg(t)
	cfg.URLDenyHosts = []string{"Blocked.EXAMPLE."}

	tool, ok := registryWithMCP(cfg.WorkspaceDir, cfg, nil).Lookup("web_fetch")
	if !ok {
		t.Fatal("web_fetch is missing from the MCP registry build")
	}
	// The deny is a string-level match and is checked before the SSRF floor resolves anything,
	// so this reaches no DNS and no network.
	result, err := tool.Execute(context.Background(), apogee.ToolCall{
		ID:        "c1",
		Tool:      "web_fetch",
		Arguments: []byte(`{"url":"https://blocked.example/"}`),
	})
	if err != nil {
		t.Fatalf("a blocked URL is not caller cancellation, so it must not be a Go error: %v", err)
	}
	if !result.IsError || !strings.Contains(result.Content, "url-safety") {
		t.Fatalf("web_fetch did not refuse the configured deny: %q — the MCP build dropped url-safety", result.Content)
	}
	if !strings.Contains(result.Content, "denied") {
		t.Errorf("the refusal is not the configured DENY (a dropped guard's floor refuses too): %q", result.Content)
	}
}

// The roster switch reaches the assembly through the same Config the rest of the host wiring does,
// and it has to hold in BOTH halves of what a registry is for: the tool list the engine offers is
// built from All(), and a call is resolved through Lookup — so a disabled tool must be missing from
// each. An MCP tool is deliberately untouched by the key: those come and go with their server.
func TestRegistryWithMCPHonoursDisabledTools(t *testing.T) {
	t.Parallel()
	cfg := validCfg(t)
	cfg.DisabledTools = []string{"view_diff", "python_exec"}
	mcpTool := mcpFixtureTool{name: "docs__search"}

	registry := registryWithMCP(t.TempDir(), cfg, []apogee.Tool{mcpTool})

	for _, name := range []string{"view_diff", "python_exec"} {
		if _, ok := registry.Lookup(name); ok {
			t.Errorf("%q is disabled but a call naming it would still resolve", name)
		}
		for _, offered := range registry.All() {
			if offered.Name() == name {
				t.Errorf("%q is disabled but the engine would still offer it in the tool list", name)
			}
		}
	}
	if _, ok := registry.Lookup("grep"); !ok {
		t.Error("a tool nobody disabled left the set")
	}
	if _, ok := registry.Lookup("docs__search"); !ok {
		t.Error("the MCP tool left the set; tools.disabled prunes the built-in half only")
	}
}

// The two rungs above the global disable reach the assembly through the same Config, and
// registryWithMCP is the path EVERY live session's registry is built on — so a rung it dropped
// would leave ADR 0057's ladder inert everywhere while the config layer kept accepting the keys.
// One row per step of decision 4: the profile axis is the last word in either direction, and a
// same-scope conflict fails closed.
//
// The global lift alone has nothing to lift today — no built-in ships default-off — so its row can
// only pin that a name in `tools.enabled:` never subtracts; it cannot tell a read rung from an
// ignored one until a built-in ships default-off, at which point that tool is the name to put here.
// The MCP tool rides every row untouched: the profile axis, like the global lists, prunes the
// built-in half only.
func TestRegistryWithMCPWalksTheRosterLadder(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		disabled []string
		enabled  []string
		profile  domain.ToolRosterDelta
		wantOn   []string
		wantOff  []string
	}{
		{
			name:     "a profile enabled: entry lifts a globally disabled tool",
			disabled: []string{"view_diff"},
			profile:  domain.ToolRosterDelta{Enabled: []string{"view_diff"}},
			wantOn:   []string{"view_diff"},
		},
		{
			name:    "a profile disabled: entry turns off what global allows",
			profile: domain.ToolRosterDelta{Disabled: []string{"python_exec", "docs__search"}},
			wantOn:  []string{"grep"},
			wantOff: []string{"python_exec"},
		},
		{
			name:     "a same-scope conflict fails closed: the global disable wins the global lift",
			disabled: []string{"view_diff"},
			enabled:  []string{"view_diff"},
			wantOff:  []string{"view_diff"},
		},
		{
			name:    "a name only in tools.enabled: leaves the set as built",
			enabled: []string{"grep"},
			wantOn:  []string{"grep"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := validCfg(t)
			cfg.DisabledTools = tt.disabled
			cfg.EnabledTools = tt.enabled
			cfg.Profile.Tools = tt.profile

			registry := registryWithMCP(t.TempDir(), cfg, []apogee.Tool{mcpFixtureTool{name: "docs__search"}})

			for _, name := range append(tt.wantOn, "docs__search") {
				assertRegistryOffers(t, registry, name, true)
			}
			for _, name := range tt.wantOff {
				assertRegistryOffers(t, registry, name, false)
			}
			if len(tt.wantOff) == 0 {
				if got, want := len(registry.All()), len(registryWithMCP(t.TempDir(), validCfg(t), nil).All())+1; got != want {
					t.Errorf("the roster left %d tools, want %d — the lift subtracted something", got, want)
				}
			}
		})
	}
}

// assertRegistryOffers checks both halves of what a registry is for — the tool list the engine
// offers is built from All(), and a call is resolved through Lookup — so a roster verdict that
// reached one and not the other is caught whichever way it leaked.
func assertRegistryOffers(t *testing.T, registry *apogee.ToolRegistry, name string, want bool) {
	t.Helper()
	if _, ok := registry.Lookup(name); ok != want {
		t.Errorf("Lookup(%q) = %v, want %v", name, ok, want)
	}
	offered := false
	for _, tool := range registry.All() {
		if tool.Name() == name {
			offered = true
		}
	}
	if offered != want {
		t.Errorf("%q offered in the tool list = %v, want %v", name, offered, want)
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
	host := newSessionHost(store, t.TempDir(), "fake", nil, "", nil)
	if err := host.Save(snap, nil, "hi", 1, 0, session.Usage{}, session.Usage{}); err != nil {
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
	host := newSessionHost(store, "/ws", "model-x", nil, "", nil)

	if host.ActiveID() != "" {
		t.Errorf("ActiveID before any Save = %q; want empty", host.ActiveID())
	}
	if err := host.Save(apogee.Session{}, nil, "first title", 1, 100, session.Usage{}, session.Usage{}); err != nil {
		t.Fatalf("Save #1: %v", err)
	}
	id := host.ActiveID()
	if id == "" {
		t.Fatal("Save minted no id")
	}
	// A second Save keeps the same id (update-in-place) and never overwrites the create-time title.
	if err := host.Save(apogee.Session{}, nil, "SECOND title", 2, 200, session.Usage{}, session.Usage{}); err != nil {
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
	host := newSessionHost(store, "/ws", "", nil, "", nil) // a cold start: nothing bound yet

	if err := host.Save(apogee.Session{}, nil, "cold", 1, 0, session.Usage{}, session.Usage{}); err != nil {
		t.Fatalf("Save before the bind: %v", err)
	}
	host.SetModel("bound-model")
	if err := host.Save(apogee.Session{}, nil, "cold", 2, 0, session.Usage{}, session.Usage{}); err != nil {
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
	host := newSessionHost(store, "/ws", "m", nil, "", nil)

	if err := host.Save(apogee.Session{}, nil, "A", 1, 0, session.Usage{}, session.Usage{}); err != nil {
		t.Fatalf("Save A: %v", err)
	}
	first := host.ActiveID()

	host.Rotate()
	if host.ActiveID() != "" {
		t.Errorf("ActiveID after Rotate = %q; want empty", host.ActiveID())
	}
	if err := host.Save(apogee.Session{}, nil, "B", 1, 0, session.Usage{}, session.Usage{}); err != nil {
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
	if err := host.Save(apogee.Session{}, nil, "ignored", 3, 0, session.Usage{}, session.Usage{}); err != nil {
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
	host := newSessionHost(store, "/ws", "m", nil, "", nil)
	if err := host.Save(apogee.Session{}, nil, "original", 1, 0, session.Usage{}, session.Usage{}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	id := host.ActiveID()
	if err := host.Rename(id, "renamed"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if err := host.Save(apogee.Session{}, nil, "original", 2, 0, session.Usage{}, session.Usage{}); err != nil {
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
	host := newSessionHost(store, "/ws", "m", seed, "", nil)

	if host.ActiveID() != seed.Meta.ID {
		t.Errorf("ActiveID of a resumed host = %q; want the resumed id %q", host.ActiveID(), seed.Meta.ID)
	}
	if err := host.Save(apogee.Session{}, nil, "derived", 1, 0, session.Usage{}, session.Usage{}); err != nil {
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
	host := newSessionHost(store, "/ws", "m", rec, "", nil)
	if host.ActiveID() != rec.Meta.ID {
		t.Errorf("host active id = %q; want the re-minted %q", host.ActiveID(), rec.Meta.ID)
	}
	if err := host.Save(apogee.Session{}, nil, "continued", 1, 0, session.Usage{}, session.Usage{}); err != nil {
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
	h := newSessionHost(store, ws, "m", nil, "", nil)
	h.now = func() time.Time { return when }
	if err := h.Save(apogee.Session{}, nil, title, 1, 0, session.Usage{}, session.Usage{}); err != nil {
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

// The host stores the two token accountings apart, exactly as the renderer hands them over, and the
// resume projection carries both back: what the main agent spent and what its delegates did. The
// halves stay separate on disk because the session total is their sum, and a store that folded them
// together could never say which was which again (session.Meta).
func TestSessionHostStoresBothTokenAccountings(t *testing.T) {
	t.Parallel()
	store := session.NewStore(t.TempDir())
	host := newSessionHost(store, "/ws", "model-x", nil, "", nil)

	main := session.Usage{Calls: 4, PromptTokens: 60000, CachedPromptTokens: 12000, TotalTokens: 64000}
	delegates := session.Usage{Calls: 300, PromptTokens: 900000, TotalTokens: 936000}
	if err := host.Save(apogee.Session{}, nil, "delegating run", 1, 100, main, delegates); err != nil {
		t.Fatalf("Save: %v", err)
	}

	rec, err := store.Load(host.ActiveID())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if rec.Meta.Usage != main {
		t.Errorf("stored usage = %+v; want the main agent's own %+v", rec.Meta.Usage, main)
	}
	if rec.Meta.DelegateUsage != delegates {
		t.Errorf("stored delegate usage = %+v; want %+v", rec.Meta.DelegateUsage, delegates)
	}
	rs := resumedSession(&rec, false)
	if rs == nil || rs.Usage != main || rs.DelegateUsage != delegates {
		t.Errorf("resumedSession = %+v; want both accountings carried into the replay payload", rs)
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
// Session scratch dirs (workspace-clobber hardening, 2026-08-22)
// ----------------------------------------------------------------------------

// TestGCScratchDirsRemovesOldKeepsFresh pins the startup sweep's one rule: an entry whose mtime
// has aged past scratchMaxAge goes, one inside the window stays — and a root that does not exist
// is silently nothing to do.
func TestGCScratchDirsRemovesOldKeepsFresh(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	now := time.Now()

	old := filepath.Join(root, "2026-01-01T00-00-00-old1")
	fresh := filepath.Join(root, "2026-08-22T00-00-00-new1")
	for _, dir := range []string{old, fresh} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("MkdirAll(%s): %v", dir, err)
		}
	}
	if err := os.WriteFile(filepath.Join(old, "scratch.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	backdated := now.Add(-scratchMaxAge - time.Hour)
	if err := os.Chtimes(old, backdated, backdated); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	gcScratchDirs(root, now)

	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Errorf("stale scratch dir survived the sweep (stat err = %v)", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("fresh scratch dir did not survive the sweep: %v", err)
	}

	gcScratchDirs(filepath.Join(root, "does-not-exist"), now) // must not panic or create anything
}

// TestSessionHostScratchFollowsTheActiveSession proves the scratch seam tracks session identity
// end to end: the boot dir exists before any Save (the pre-minted id), a Rotate mints a NEW
// session and moves the engine's scratch to its dir, an Activate moves it to the resumed
// session's, and the id the first Save adopts is the one the boot scratch dir was named by — so
// dir and record never disagree.
func TestSessionHostScratchFollowsTheActiveSession(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	store := session.NewStore(filepath.Join(t.TempDir(), "sessions"))
	var moved []string
	host := newSessionHost(store, "/ws", "m", nil, root, func(dir string) { moved = append(moved, dir) })

	bootDir := host.SessionScratchDir()
	if bootDir == "" {
		t.Fatal("SessionScratchDir answered \"\" on a scratch-enabled host")
	}
	if info, err := os.Stat(bootDir); err != nil || !info.IsDir() {
		t.Fatalf("boot scratch dir %s not created: %v", bootDir, err)
	}

	// The first Save adopts the pre-minted id — the name the boot dir already carries.
	if err := host.Save(apogee.Session{}, nil, "t", 1, 0, session.Usage{}, session.Usage{}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if got, want := host.ActiveID(), filepath.Base(bootDir); got != want {
		t.Errorf("first Save minted id %q, want the boot scratch dir's name %q", got, want)
	}

	host.Rotate()
	if len(moved) != 1 {
		t.Fatalf("Rotate pushed %d scratch moves, want 1", len(moved))
	}
	if moved[0] == bootDir || filepath.Dir(moved[0]) != root {
		t.Errorf("Rotate moved scratch to %q, want a NEW dir under %q", moved[0], root)
	}
	if info, err := os.Stat(moved[0]); err != nil || !info.IsDir() {
		t.Errorf("rotated scratch dir %s not created: %v", moved[0], err)
	}

	// A /sessions resume: scratch follows the ACTIVATED session's own id.
	host.Activate(session.Meta{ID: filepath.Base(bootDir)})
	if len(moved) != 2 || moved[1] != bootDir {
		t.Fatalf("Activate pushed moves %v, want the resumed session's dir %q last", moved, bootDir)
	}
}

// TestSessionHostWithoutScratchRootIsInert pins the disabled seam: no root means no dirs, no
// listener calls, and the pre-scratch behaviour everywhere else.
func TestSessionHostWithoutScratchRootIsInert(t *testing.T) {
	t.Parallel()
	store := session.NewStore(filepath.Join(t.TempDir(), "sessions"))
	called := false
	host := newSessionHost(store, "/ws", "m", nil, "", func(string) { called = true })

	if dir := host.SessionScratchDir(); dir != "" {
		t.Errorf("SessionScratchDir = %q on a disabled seam, want \"\"", dir)
	}
	host.Rotate()
	host.Activate(session.Meta{ID: "some-id"})
	if called {
		t.Error("scratchMoved called on a host with no scratch root")
	}
}
