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
	"github.com/airiclenz/apogee/internal/heartbeat"
	"github.com/airiclenz/apogee/internal/library"
	"github.com/airiclenz/apogee/internal/mechanisms"
	"github.com/airiclenz/apogee/internal/profiles"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/scheme"
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

// rosterSwitchWiring assembles a live session — the boot phase and the live phase, exactly as
// runRoot runs them, short of the launch — bound to model-a on a server that advertises nothing,
// with view_diff off globally and a model-b profile that lifts it back. It is the rebind harness
// with the wiring kept in hand rather than handed to a launcher, because what a switch does to the
// TOOL SET is observable only on the holder the composition root keeps.
func rosterSwitchWiring(t *testing.T) *rootWiring {
	t.Helper()
	opts := config.Options{
		Endpoint:      "http://127.0.0.1:1111",
		Model:         "model-a",
		Mode:          "ask-before",
		Workspace:     t.TempDir(),
		ConfigDir:     t.TempDir(),
		AutoCompact:   true,
		ToolsDisabled: []string{"view_diff"},
		ModelProfiles: []profiles.Entry{{
			Pattern:     "model-b",
			Profile:     apogee.ModelProfile{Tools: domain.ToolRosterDelta{Enabled: []string{"view_diff"}}},
			SpellsTools: true,
		}},
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
	if err := w.wireSession(context.Background()); err != nil {
		t.Fatalf("wireSession: %v", err)
	}
	return w
}

// liveSetHas reports whether the set the holder is on registers name — under the holder's own lock,
// as every reader of the pointer is.
func liveSetHas(live *liveTools, name string) bool {
	live.mu.Lock()
	defer live.mu.Unlock()
	_, ok := live.current.Lookup(name)
	return ok
}

// A model switch re-composes the tool set under the profile it lands on (ADR 0057 decision 7:
// "/model to the big model and its enabled tools appear; switch back and they are gone"). The engine's
// own re-compose seam stands down under the registry the composition root injects, so the root's
// rebind verb drives the set's swap door itself — and the holder moves onto the new set only once
// the engine has taken it, which is what makes the holder's set the one the loop now offers.
func TestRebindRecomposesTheToolSetUnderTheProfileRoster(t *testing.T) {
	t.Parallel()
	w := rosterSwitchWiring(t)
	if liveSetHas(w.toolSet, "view_diff") {
		t.Fatal("view_diff is on the startup set; the global tools.disabled: did not prune it")
	}

	result, err := w.rebind("model-b", 8192, provider.EffortDialectNone)
	if err != nil {
		t.Fatalf("rebind onto model-b: %v", err)
	}
	if !liveSetHas(w.toolSet, "view_diff") {
		t.Error("view_diff is missing after the switch onto model-b: the profile's roster was announced but never applied")
	}
	if !slices.Contains(result.Notices, "tools: +view_diff (profile)") {
		t.Errorf("notices = %q, want the switch line for the lifted tool among them", result.Notices)
	}

	// Switching back onto a model with no roster axis composes the set the global lists describe:
	// the lift belonged to model-b's profile, not to the session.
	if _, err := w.rebind("model-a", 8192, provider.EffortDialectNone); err != nil {
		t.Fatalf("rebind back onto model-a: %v", err)
	}
	if liveSetHas(w.toolSet, "view_diff") {
		t.Error("view_diff is still on the set after switching back onto a model with no roster axis")
	}
}

// A refusal from the swap door during a switch is a NOTICE, not a failed rebind (ratified design
// call 4): the binding has already committed by then, so failing the rebind would report a switch
// that did happen as one that did not. The result carries the model and window as the switch
// resolved them, one line says the tools did not move and when they will, and the holder stays on
// the set the engine still runs.
func TestRebindReportsARefusedRosterSwapAsANotice(t *testing.T) {
	t.Parallel()
	w := rosterSwitchWiring(t)
	// A build the engine refuses outright stands in for the mid-Exchange refusal: SwapTools has
	// exactly those two, and the boundary the TUI rebinds at (ADR 0024) rules the other one out.
	w.toolSet = newLiveTools(w.toolSet.current, w.toolSet.built(),
		func(toolSetSpec) *apogee.ToolRegistry { return nil })

	result, err := w.rebind("model-b", 8192, provider.EffortDialectNone)
	if err != nil {
		t.Fatalf("rebind must not fail over a refused swap, the binding had already committed: %v", err)
	}
	if result.Model != "model-b" || result.ContextWindow != 8192 {
		t.Errorf("result = %+v, want Model model-b and ContextWindow 8192: the switch happened", result)
	}
	var lines []string
	for _, n := range result.Notices {
		if strings.Contains(n, "next model switch") {
			lines = append(lines, n)
		}
	}
	if len(lines) != 1 {
		t.Fatalf("notices = %q, want exactly one line saying the roster applies at the next switch", result.Notices)
	}
	if !strings.Contains(lines[0], "model-b") || !strings.Contains(lines[0], "not applied") {
		t.Errorf("notice = %q, want it to name the model and say the tools did not move", lines[0])
	}
	if liveSetHas(w.toolSet, "view_diff") {
		t.Error("the holder moved onto a set the engine refused")
	}
	if got := w.holder.Binding().Model; got != "model-b" {
		t.Errorf("bound model = %q, want model-b: the refused swap must not unwind the switch", got)
	}
}

// rebindSpecFor is the composition root's half of a rebind: everything that is per-model gets
// resolved again for the model the heartbeat observed, and everything else is left alone. The table
// walks the four decisions it makes — which system-prompt template (ADR 0023), which validated
// Mechanism set (ADR 0016), whether an explicit `mechanisms:` block suppresses that set, and which
// window is bound when the observation and a `context-window:` pin disagree (decision 9).
func TestRebindSpecForSelectsPerModelBindings(t *testing.T) {
	t.Parallel()

	prompts := config.SystemPromptSettings{
		Global: config.PromptSource{Text: "the global prompt"},
		Models: map[string]config.PromptSource{"model-b": {Text: "the model-b prompt"}},
	}
	manual := []apogee.MechanismID{"validate"}

	tests := []struct {
		name         string
		opts         config.Options
		manualIDs    []apogee.MechanismID
		model        string
		window       int
		pinnedWindow int
		outputCap    int
		wantPrompt   string
		wantWindow   int
		wantEnable   func(t *testing.T, got []apogee.MechanismID)
	}{
		{
			name:       "the per-model prompt entry is selected for the model being bound",
			opts:       config.Options{SystemPrompt: prompts},
			model:      "model-b",
			window:     32768,
			wantPrompt: "the model-b prompt",
			wantWindow: 32768,
		},
		{
			name:       "a model with no entry of its own falls back to the global prompt",
			opts:       config.Options{SystemPrompt: prompts},
			model:      "model-a",
			window:     32768,
			wantPrompt: "the global prompt",
			wantWindow: 32768,
		},
		{
			name: "a validated set matching the new model applies when no manual list was configured",
			opts: config.Options{
				ValidatedSetsEnable: true,
				ValidatedSetsAlias:  map[string]string{gemmaKey: gemmaKey}, // the §3 human decision
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
			opts: config.Options{
				ValidatedSetsEnable: true,
				ValidatedSetsAlias:  map[string]string{gemmaKey: gemmaKey},
				Mechanisms:          map[string]bool{"validate": true},
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
			opts:         config.Options{},
			model:        "model-a",
			window:       131072,
			pinnedWindow: 16384,
			wantWindow:   16384,
		},
		{
			name:       "an unpinned session adopts the observed window",
			opts:       config.Options{},
			model:      "model-a",
			window:     131072,
			wantWindow: 131072,
		},
		{
			name:       "an observation with no window binds an unknown one rather than inventing it",
			opts:       config.Options{},
			model:      "model-a",
			wantWindow: 0,
		},
		{
			// The one bound here that is not per-model: the bound entry's `max-output-tokens:` (ADR
			// 0046) rides the spec because the pin has no engine setter of its own, so a rebind is the
			// only door a live edit of it can reach the engine through.
			name:       "the bound entry's reply ceiling is stated beside the window",
			opts:       config.Options{},
			model:      "model-a",
			window:     32768,
			outputCap:  8192,
			wantWindow: 32768,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			roots := stateRoots{config: t.TempDir(), validated: t.TempDir(), probe: t.TempDir()}

			spec, _, err := rebindSpecFor(tt.opts, roots, tt.manualIDs, tt.model, tt.window,
				tt.pinnedWindow, tt.outputCap)
			if err != nil {
				t.Fatalf("rebindSpecFor: %v", err)
			}
			// Stated on EVERY spec, never left nil: nil is the spec's way of saying nothing about the
			// ceiling, and this resolver always knows what the bound entry says — including that it
			// says nothing, which is the 0 that means "derive the cap again" (ADR 0046).
			if spec.MaxOutputTokens == nil {
				t.Fatalf("spec.MaxOutputTokens is nil; want the bound entry's ceiling stated")
			}
			if *spec.MaxOutputTokens != tt.outputCap {
				t.Errorf("spec.MaxOutputTokens = %d; want the bound entry's %d", *spec.MaxOutputTokens, tt.outputCap)
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

// The wire is the holder's to state, never the launch snapshot's: rebindInputs overlays the CURRENT
// binding's endpoint and key onto the copy it hands rebindSpecFor, so a session that has moved
// server since launch re-resolves against where it is now.
func TestRebindInputsOverlayTheBoundUpstream(t *testing.T) {
	t.Parallel()
	launchOpts := config.Options{Endpoint: "http://launch.invalid", APIKey: "launch-key"}
	live := newLiveSettings(launchOpts, nil)
	bound := upstreamBinding{Endpoint: "http://bound.invalid", Model: "bound-model", APIKey: "bound-key"}

	base, _, _, _ := live.rebindInputs(launchOpts, bound)

	if base.Endpoint != bound.Endpoint {
		t.Errorf("endpoint = %q; want the bound %q, not the launch snapshot's", base.Endpoint, bound.Endpoint)
	}
	if base.APIKey != bound.APIKey {
		t.Errorf("apiKey = %q; want the bound %q — a key from before a switch opens the wrong server",
			base.APIKey, bound.APIKey)
	}
}

// What the overlay is FOR, proven through the rebind path rather than at the seam: the identity
// ladder's middle rung is keyed on (probe dir, endpoint, model id), so a rebind that still carried
// the launch endpoint would miss the record `apogee probe model` left for the server the session is
// on now — resolving at low confidence, where a matching Validated set is merely OFFERED. With the
// bound endpoint the same record promotes the identity to medium and the set APPLIES.
func TestRebindResolutionKeysOnTheBoundEndpoint(t *testing.T) {
	t.Parallel()
	const boundEndpoint = "http://127.0.0.1:65535"
	roots, err := resolveRoots(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("resolveRoots: %v", err)
	}
	if _, err := library.SaveProbeRecord(roots.probe, library.ProbeRecord{
		Endpoint:   boundEndpoint,
		ModelLabel: gemmaKey,
		ProbedAt:   mustTime(t, "2026-07-22T10:00:00Z"),
		Behavior:   "probe:1:tools+json+chain",
	}); err != nil {
		t.Fatalf("save probe record: %v", err)
	}

	// The launch snapshot names a server this session has since left.
	launchOpts := config.Options{Endpoint: "http://launch.invalid", ValidatedSetsEnable: true}
	live := newLiveSettings(launchOpts, nil)

	// The rebind closure the composition root wires, reconstructed as the other rebind tests do.
	var spec apogee.RebindSpec
	var notices []string
	rebind := func(model string, window int, _ provider.EffortDialect) (tui.RebindResult, error) {
		bound := upstreamBinding{Endpoint: boundEndpoint, Model: model}
		base, manualIDs, pinnedWindow, outputCap := live.rebindInputs(launchOpts, bound)
		got, ns, err := rebindSpecFor(base, roots, manualIDs, model, window, pinnedWindow, outputCap)
		if err != nil {
			return tui.RebindResult{}, err
		}
		spec, notices = got, ns
		return tui.RebindResult{Model: got.Model, ContextWindow: got.MaxContextTokens}, nil
	}

	if _, err := rebind(gemmaKey, 8192, provider.EffortDialectNone); err != nil {
		t.Fatalf("rebind: %v", err)
	}
	if len(spec.EnableMechanisms) == 0 {
		t.Fatalf("EnableMechanisms is empty: the set was not applied, so the resolution missed the "+
			"record keyed to the bound endpoint; notices=%v", notices)
	}
	if !noticeContains(notices, "Validated set for "+gemmaKey+" applied") {
		t.Errorf("want the applying notice, got %v", notices)
	}
	if noticeContains(notices, "To apply it") {
		t.Errorf("the low-confidence OFFER means the launch endpoint was keyed on, not the bound one: %v", notices)
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
	opts := config.Options{
		Endpoint:  srv.URL,
		Model:     "model-a",
		Mode:      "ask-before",
		HostAlias: "workstation",
		Workspace: t.TempDir(),
		ConfigDir: t.TempDir(),
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
	if beat := rec.opts.Server.Beat(context.Background()); !beat.Reachable || beat.ActiveModel != "model-a" {
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
		servers  []config.ServerEntry
	}{
		{
			name:     "first boot, with servers to choose from",
			prebound: tui.PreboundStart{Reason: tui.PreboundFirstBoot},
			servers:  []config.ServerEntry{{Name: "laptop", Endpoint: "http://127.0.0.1:1111"}},
		},
		{
			name:     "a recorded choice no entry carries any more",
			prebound: tui.PreboundStart{Reason: tui.PreboundStaleChoice, Name: "the-old-name"},
			servers:  []config.ServerEntry{{Name: "laptop", Endpoint: "http://127.0.0.1:1111"}},
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
			opts := config.Options{
				Mode:      "ask-before",
				Workspace: t.TempDir(),
				ConfigDir: t.TempDir(),
				Servers:   tt.servers,
				Prebound:  tt.prebound,
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
			beat := rec.opts.Server.Beat(context.Background())
			if beat.Reachable || beat.Failure != "" || beat.ActiveModel != "" || len(beat.AvailableModels) != 0 {
				t.Errorf("beat = %+v; want the zero Beat — there is no server to observe yet", beat)
			}
			// And the way out is wired: the picker's rows are the configured list (no synthesized
			// row, because no ephemeral startup exists) and BindServer is what ends the state.
			if choices := rec.opts.Server.List(); len(choices) != len(tt.servers) {
				t.Errorf("tui.ServerHost.List() = %+v; want the configured list %+v", choices, tt.servers)
			}
			if !serverActsOf(rec.opts).CanBind {
				t.Error("tui.ServerActs.CanBind is false; the pre-bound session has no way to bind one")
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
	opts := config.Options{
		Mode:          "ask-before",
		Workspace:     t.TempDir(),
		ConfigDir:     t.TempDir(),
		ContextWindow: 16384, // the global pin, which a first binding adopts like a switch does
		Servers: []config.ServerEntry{
			{Name: "laptop", Endpoint: first.URL, Model: "model-a", APIKey: "laptop-key"},
			{Name: "workstation", Endpoint: second.URL, Model: "model-b"},
		},
		Prebound: tui.PreboundStart{Reason: tui.PreboundFirstBoot},
	}

	if err := runRoot(context.Background(), opts, rec.launch); err != nil {
		t.Fatalf("runRoot: %v", err)
	}

	// A name no entry carries is resolved before anything is constructed, so the session stays
	// exactly as unbound as it was.
	if _, err := rec.opts.Server.Bind("nope"); err == nil {
		t.Error("BindServer accepted a name no entry carries")
	}
	if _, err := rec.engine.Snapshot(); !errors.Is(err, errNoServerBound) {
		t.Errorf("Snapshot err = %v after a failed bind; want errNoServerBound", err)
	}

	result, err := rec.opts.Server.Bind("laptop")
	if err != nil {
		t.Fatalf("Bind: %v", err)
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
	if beat := rec.opts.Server.Beat(context.Background()); !beat.Reachable || beat.ActiveModel != "model-a" {
		t.Errorf("beat after the bind = %+v; want model-a from the bound server's Monitor", beat)
	}

	// Exactly once: a second bind is refused, and nothing moved — the session is still on the
	// server it bound, which is what `/server` (SwitchServer) exists to change.
	if _, err := rec.opts.Server.Bind("workstation"); !errors.Is(err, errAlreadyBound) {
		t.Errorf("second BindServer err = %v; want errAlreadyBound", err)
	}
	if beat := rec.opts.Server.Beat(context.Background()); beat.ActiveModel != "model-a" {
		t.Errorf("beat after the refused second bind = %+v; want the first server still observed", beat)
	}
	// And the switch that IS the right verb still works over the same list.
	if _, err := rec.opts.Server.Switch("workstation"); err != nil {
		t.Fatalf("SwitchServer after a bind: %v", err)
	}
	if beat := rec.opts.Server.Beat(context.Background()); beat.ActiveModel != "model-b" {
		t.Errorf("beat after the switch = %+v; want model-b", beat)
	}
}

// The two settings a human can move while the picker is open must not be lost when the engine is
// finally constructed: the footer showed them, so the engine has to be born with them.
func TestLateEngineAppliesPreBindSettingsOnBind(t *testing.T) {
	t.Parallel()
	engine := newLateEngine(domain.ModeAskBefore, true)
	t.Cleanup(func() { _ = engine.Close() })

	// Unbound, the reads answer what a bind would install.
	if !engine.ConfineToWorkspace() {
		t.Error("ConfineToWorkspace = false before a bind; want the resolved value")
	}
	engine.SetMode(domain.ModePlan)
	engine.SetConfineToWorkspace(false)
	if engine.ConfineToWorkspace() {
		t.Error("ConfineToWorkspace = true after SetConfineToWorkspace(false) while unbound")
	}

	cfg := validCfg(t)
	cfg.Mode = domain.ModeAskBefore
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
	if bound.Mode() != domain.ModePlan {
		t.Errorf("the bound Agent's mode = %q; want the %q the human cycled to before the bind",
			bound.Mode(), domain.ModePlan)
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
// on endpoint, and hands back the collaborators the assertions read. The last of them is the engine
// spy behind the wiring's Parallel agents cap: a load's move re-follows the cap (ADR 0039), so a test
// can read every width that arrival pushed as well as the one the cap now resolves to.
func launcherWiringFixture(t *testing.T, ops launcherOps, endpoint string) (
	launcherWiring, *fakeSwitcher, *fakeStamper, *upstreamHolder, *parallelAgentsSpy) {
	t.Helper()
	agent := &fakeSwitcher{}
	host := &fakeStamper{}
	holder := newUpstreamHolder()
	holder.Bind(endpoint, "", "", heartbeat.NewMonitor(endpoint, "", ""))
	widths := &parallelAgentsSpy{}
	wiring := launcherWiring{
		sessionMover: sessionMover{
			agent: agent, holder: holder, host: host,
			live: newLiveSettings(config.Options{ContextWindow: 16384}, nil),
			keys: config.NewKeyResolver(),
			caps: newParallelAgentsCap(widths),
		},
		ops:  ops,
		path: newLauncherPath("/etc/llama-launcher/config.yaml", "rig"),
	}
	return wiring, agent, host, holder, widths
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
	wiring, agent, host, holder, _ := launcherWiringFixture(t, ops, "http://127.0.0.1:8080")

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
	wiring, agent, host, holder, _ := launcherWiringFixture(t, ops, "http://127.0.0.1:8080")

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
	// The spec carries the window the display adopted, not a second number: a profile's server is in
	// no `servers:` list, so it pins nothing of its own and the top-level pin — which survives a move
	// — is what the engine budgets against on the other side of it.
	wantSpec := apogee.UpstreamSpec{Endpoint: "http://127.0.0.1:9090", APIKey: "llamacpp-key", MaxContextTokens: 16384}
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

// The fan-out cap follows the profile's server like every other arrival (ADR 0039), because the load
// commits the same shared move a `/server` switch does. A Launch profile's entry pins no
// `parallel-agents:` and its server has not beaten yet, so the honest width on the other side is the
// serial floor: neither the departed entry's pin nor the retired server's slot count travels, and the
// new server's own first beat is what widens it.
func TestLoadProfileMoveReFollowsTheParallelAgentsCap(t *testing.T) {
	t.Parallel()

	ops := &fakeLauncher{
		cfg:        twoServerConfig(t),
		loadResult: &llamalauncher.RunningInstance{Backend: "llamacpp", Host: "127.0.0.1", Port: 9090},
	}
	wiring, _, _, _, widths := launcherWiringFixture(t, ops, "http://127.0.0.1:8080")
	wiring.caps.follow(config.ServerEntry{Name: "rig", ParallelAgents: 3})
	wiring.caps.observe(6)

	result, err := wiring.load("there", nil)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if result.Move == nil {
		t.Fatalf("result = %+v; want a resolved Move — the profile serves another address", result)
	}
	if _, err := result.Move(); err != nil {
		t.Fatalf("committing the resolved move: %v", err)
	}

	if got := widths.last(); got != 1 {
		t.Errorf("the width the committed move pushed = %d; want the serial floor 1 — the departed "+
			"entry's pin was dropped and its observed slot count forgotten", got)
	}
	if got := wiring.caps.current(); got != 1 {
		t.Errorf("caps.current() = %d; want 1 — a server no `servers:` entry describes runs serial "+
			"until it reports its own slots", got)
	}
	if got := wiring.caps.observe(4); got != 4 {
		t.Errorf("cap after the new server's first beat named 4 slots = %d; want 4 — the profile's "+
			"own server is what widens it", got)
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
	wiring, _, _, _, _ := launcherWiringFixture(t, ops, "http://127.0.0.1:8080")

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
	wiring, agent, host, holder, _ := launcherWiringFixture(t, ops, "http://127.0.0.1:8080")

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
	wiring, agent, host, holder, _ := launcherWiringFixture(t, ops, "http://127.0.0.1:8080")

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
	wantSpec := apogee.UpstreamSpec{Endpoint: dial, APIKey: "llamacpp-key", MaxContextTokens: 16384}
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
	wiring, _, _, holder, _ := launcherWiringFixture(t, ops, "http://127.0.0.1:8080")

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
	wiring, agent, _, _, _ := launcherWiringFixture(t, ops, "http://127.0.0.1:8080")

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
	wiring, agent, _, _, _ := launcherWiringFixture(t, ops, "http://127.0.0.1:8080")

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
	wiring, _, _, _, _ = launcherWiringFixture(t, other, "http://127.0.0.1:8080")
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
	wiring, _, _, _, _ := launcherWiringFixture(t, ops, "http://127.0.0.1:8080")

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
	wiring, _, _, _, _ = launcherWiringFixture(t, broken, "http://127.0.0.1:8080")
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
	wiring, _, _, _, _ := launcherWiringFixture(t, ops, "http://127.0.0.1:8080")

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
			wiring, _, _, _, _ := launcherWiringFixture(t, ops, "http://remote.invalid:9999")

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

// The four seams exist for the whole session whatever the startup entry's `llama-launcher:` said,
// because the key belongs to a `servers:` ENTRY and the session can move between entries: whether
// the integration works is a fact the VERBS answer per call. Off, every one of them reports
// tui.ErrNoLauncher — the renderer's own no-launcher sentence, which `/model` reads as "offer the
// models the server advertises".
func TestRunRootWiresTheLauncherSeamsForTheWholeSession(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		key     string
		enabled bool
		// probeVerbs also asks the two I/O verbs what they SAY. It is off for `auto`, whose path is
		// whatever launcher config the machine running the test happens to have — a real address a
		// test must not drive an unload against.
		probeVerbs bool
	}{
		{name: "no key on the entry ⇒ the verbs report the integration off", probeVerbs: true},
		{name: "a named config ⇒ the verbs act on it", key: filepath.Join(t.TempDir(), "launcher.yaml"),
			enabled: true, probeVerbs: true},
		{name: "auto ⇒ the launcher's own default config, unchecked", key: "auto", enabled: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			upstream := upstreamServer(t, "model-a", 4096)
			rec := &recordingLauncher{}
			opts := config.Options{
				Endpoint:        upstream.URL,
				Mode:            "ask-before",
				Workspace:       t.TempDir(),
				ConfigDir:       t.TempDir(),
				AutoCompact:     true,
				StartupLauncher: tt.key,
			}
			if err := runRoot(context.Background(), opts, rec.launch); err != nil {
				t.Fatalf("runRoot: %v", err)
			}

			// One check for the whole family now that the seams are one interface (ADR 0054): the
			// members were always wired together or not at all, so a non-nil host is exactly what
			// "every launcher seam wired for the session" used to mean member by member.
			if rec.opts.Launcher == nil {
				t.Fatal("tui.Options.Launcher is nil; want the launcher host wired for the session")
			}

			// Enabled is the same fact asked without a verb: it is what lets the two actuation
			// verbs refuse a switched-off session on the keypress, before the latch and the footer's
			// "unloading…" frame.
			if got := rec.opts.Launcher.Enabled(); got != tt.enabled {
				t.Errorf("Launcher.Enabled() = %v; want the integration reported %v", got, tt.enabled)
			}

			if !tt.probeVerbs {
				return
			}
			// What the seams SAY is where off and on differ now. A named config that is not there
			// fails as the launcher's own missing-file error, which is emphatically not the
			// integration being off.
			_, err := rec.opts.Launcher.Profiles()
			if got := errors.Is(err, tui.ErrNoLauncher); got == tt.enabled {
				t.Errorf("Launcher.Profiles error = %v (ErrNoLauncher = %v); want enabled = %v", err, got, tt.enabled)
			}
			if _, err := rec.opts.Launcher.Unload(upstream.URL); errors.Is(err, tui.ErrNoLauncher) == tt.enabled {
				t.Errorf("Launcher.Unload error = %v; want the integration reported %v", err, tt.enabled)
			}
		})
	}
}

// The integration follows the session's SERVER: `/server` onto the entry the launcher fronts turns
// the verbs on, and switching away turns them off again. That is the whole of the per-entry key —
// `/model` offers Launch profiles only while the session is on the launcher's own server, and every
// other entry keeps the advertised-model discovery a remote server answers with.
func TestSwitchServerFollowsTheEntrysLauncher(t *testing.T) {
	t.Parallel()

	local := upstreamServer(t, "model-a", 4096)
	remote := upstreamServer(t, "model-b", 8192)
	launcherYAML := filepath.Join(t.TempDir(), "launcher.yaml")
	rec := &recordingLauncher{}
	opts := config.Options{
		// The session starts on the plain entry, so it starts with the integration off.
		Endpoint:      remote.URL,
		HostAlias:     "remote",
		StartupServer: "remote",
		Mode:          "ask-before",
		Workspace:     t.TempDir(),
		ConfigDir:     t.TempDir(),
		AutoCompact:   true,
		Servers: []config.ServerEntry{
			{Name: "local", Endpoint: local.URL, Model: "model-a", LlamaLauncher: launcherYAML},
			{Name: "remote", Endpoint: remote.URL, Model: "model-b"},
		},
	}
	if err := runRoot(context.Background(), opts, rec.launch); err != nil {
		t.Fatalf("runRoot: %v", err)
	}

	if rec.opts.Launcher.Enabled() {
		t.Fatal("Launcher.Enabled() = true on a startup entry that names no launcher")
	}
	if _, err := rec.opts.Server.Switch("local"); err != nil {
		t.Fatalf("SwitchServer(local): %v", err)
	}
	if !rec.opts.Launcher.Enabled() {
		t.Error("Launcher.Enabled() = false after switching onto the launcher-fronted entry")
	}
	// And it is THAT entry's config the verbs now read: the file is not there, so the launcher's own
	// missing-file error names it — which is emphatically not the integration being off.
	if _, err := rec.opts.Launcher.Profiles(); !strings.Contains(fmt.Sprint(err), launcherYAML) {
		t.Errorf("Launcher.Profiles error = %v; want the entry's own config path %q named", err, launcherYAML)
	}

	// A switch that resolves to nothing moved no session, so it installs nothing either.
	if _, err := rec.opts.Server.Switch("nope"); err == nil {
		t.Fatal("SwitchServer accepted a name no entry carries")
	}
	if !rec.opts.Launcher.Enabled() {
		t.Error("Launcher.Enabled() = false after a REFUSED switch; the session never left the launcher's server")
	}

	// Leaving turns it off again: the remote server has no launcher in front of it, and `/model`
	// there must fall back to what that server advertises.
	if _, err := rec.opts.Server.Switch("remote"); err != nil {
		t.Fatalf("SwitchServer(remote): %v", err)
	}
	if rec.opts.Launcher.Enabled() {
		t.Error("Launcher.Enabled() = true after switching back to an entry that names no launcher")
	}
	if _, err := rec.opts.Launcher.Profiles(); !errors.Is(err, tui.ErrNoLauncher) {
		t.Errorf("Launcher.Profiles error = %v; want tui.ErrNoLauncher off the launcher's server", err)
	}
}

// The other way a session arrives on an entry is the first bind out of a pre-bound start, and it
// installs the launcher exactly as a switch does. Until it happens the holder is empty: a session
// with no server bound has no entry to take a launcher from.
func TestBindServerInstallsTheEntrysLauncher(t *testing.T) {
	t.Parallel()

	local := upstreamServer(t, "model-a", 4096)
	launcherYAML := filepath.Join(t.TempDir(), "launcher.yaml")
	rec := &recordingLauncher{}
	opts := config.Options{
		Mode:        "ask-before",
		Workspace:   t.TempDir(),
		ConfigDir:   t.TempDir(),
		AutoCompact: true,
		Servers: []config.ServerEntry{
			{Name: "local", Endpoint: local.URL, Model: "model-a", LlamaLauncher: launcherYAML},
		},
		Prebound: tui.PreboundStart{Reason: tui.PreboundFirstBoot},
	}
	if err := runRoot(context.Background(), opts, rec.launch); err != nil {
		t.Fatalf("runRoot: %v", err)
	}

	if rec.opts.Launcher.Enabled() {
		t.Fatal("Launcher.Enabled() = true before anything was bound")
	}
	if _, err := rec.opts.Server.Bind("local"); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if !rec.opts.Launcher.Enabled() {
		t.Error("Launcher.Enabled() = false after binding the launcher-fronted entry")
	}
	if _, err := rec.opts.Launcher.Profiles(); !strings.Contains(fmt.Sprint(err), launcherYAML) {
		t.Errorf("Launcher.Profiles error = %v; want the bound entry's own config path %q named", err, launcherYAML)
	}
}

// A profile load that MOVES the session preserves the launcher, because that move goes through the
// shared sessionMover and not through an entry: the endpoint it lands on may be one no `servers:`
// entry names, and taking the integration away from the session that just used it would leave the
// human unable to load a second profile.
func TestLoadProfileMovePreservesTheLauncher(t *testing.T) {
	t.Parallel()

	ops := &fakeLauncher{
		cfg:        twoServerConfig(t),
		loadResult: &llamalauncher.RunningInstance{Backend: "llamacpp", Host: "127.0.0.1", Port: 9090},
	}
	wiring, _, _, _, _ := launcherWiringFixture(t, ops, "http://127.0.0.1:8080")

	result, err := wiring.load("there", nil)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if result.Move == nil {
		t.Fatalf("result = %+v; want a resolved Move — the profile serves another address", result)
	}
	if _, err := result.Move(); err != nil {
		t.Fatalf("committing the resolved move: %v", err)
	}

	if got := wiring.path.get(); got != "/etc/llama-launcher/config.yaml" {
		t.Errorf("launcher path after a profile-load move = %q; want the session's own, untouched", got)
	}
	if !wiring.on() {
		t.Error("the integration went off with a profile-load move; the session that used it still has it")
	}
}

// ----------------------------------------------------------------------------
// The shared move's per-entry token bounds (ADR 0045, ADR 0046)
// ----------------------------------------------------------------------------

// A move re-points the session at a server, so it carries what that server BOUNDS a session to: the
// entry's own `context-window:` pin and its `max-output-tokens:` cap. Both reach the engine on the
// switch spec — the same numbers the bind hands a session that started on the entry — and the window
// the display adopts is the very one the engine was handed, so the gauge and the Budget can never
// describe two different servers.
//
// The second move is the half that would be invisible: an entry pinning nothing must DROP the
// retired entry's numbers rather than carry them, falling back to the top-level `context-window:`
// key (which survives every move) and to the zero the engine derives its own cap from.
func TestMoveCarriesTheEntrysWindowAndReplyCap(t *testing.T) {
	t.Parallel()

	agent := &fakeSwitcher{}
	host := &fakeStamper{}
	holder := newUpstreamHolder()
	holder.Bind("http://old.invalid:1111", "old-key", "old-model",
		heartbeat.NewMonitor("http://old.invalid:1111", "old-model", "old-key"))
	live := newLiveSettings(config.Options{ContextWindow: 16384}, nil)
	mover := sessionMover{agent: agent, holder: holder, host: host, live: live,
		keys: config.NewKeyResolver(), caps: newParallelAgentsCap(&parallelAgentsSpy{})}

	pinned := config.ServerEntry{
		Name: "workstation", Endpoint: "http://192.168.64.1:1111", APIKey: "new-key",
		Model: "gpt-oss-20b", ContextWindow: 65536, MaxOutputTokens: 8192,
	}
	result, err := mover.move(pinned)
	if err != nil {
		t.Fatalf("move onto the pinned entry: %v", err)
	}

	wantSpec := apogee.UpstreamSpec{
		Endpoint: pinned.Endpoint, APIKey: pinned.APIKey,
		MaxContextTokens: 65536, MaxOutputTokens: 8192,
	}
	if len(agent.specs) != 1 || agent.specs[0] != wantSpec {
		t.Errorf("SwitchUpstream specs = %+v; want exactly [%+v] — the entry's own two bounds",
			agent.specs, wantSpec)
	}
	wantResult := tui.ServerSwitchResult{
		Endpoint: pinned.Endpoint, HostAlias: "workstation", ContextWindow: 65536,
	}
	if result != wantResult {
		t.Errorf("move = %+v; want %+v — the display adopts the window the engine was handed",
			result, wantResult)
	}
	if got := holder.Endpoint(); got != pinned.Endpoint {
		t.Errorf("holder endpoint = %q; want the entry's %q", got, pinned.Endpoint)
	}
	if !slices.Equal(host.models, []string{""}) {
		t.Errorf("SetModel calls = %v; want exactly one unbinding \"\" — a move unbinds the model", host.models)
	}

	// The pin outlives the move by more than one beat: the rebind that the new server's first
	// observation drives resolves its window through this same holder, so it binds the entry's 65,536
	// rather than the top-level 16,384 or whatever that server happens to advertise.
	if _, _, pin, _ := live.rebindInputs(config.Options{}, upstreamBinding{}); pin != 65536 {
		t.Errorf("the next rebind's pin = %d; want the moved-to entry's 65536", pin)
	}
	// And so does the ceiling beside it, for the same span and the same reason: a rebind now re-states
	// the reply cap on its spec, so a latch left behind on the retired entry's number would have the
	// first beat after a move un-bound — or wrongly bound — a reply on the server just arrived at.
	if _, _, _, outputCap := live.rebindInputs(config.Options{}, upstreamBinding{}); outputCap != 8192 {
		t.Errorf("the next rebind's ceiling = %d; want the moved-to entry's 8192", outputCap)
	}

	bare := config.ServerEntry{Name: "laptop", Endpoint: "http://127.0.0.1:8080"}
	result, err = mover.move(bare)
	if err != nil {
		t.Fatalf("move onto the unpinned entry: %v", err)
	}
	wantSpec = apogee.UpstreamSpec{Endpoint: bare.Endpoint, MaxContextTokens: 16384}
	if len(agent.specs) != 2 || agent.specs[1] != wantSpec {
		t.Errorf("SwitchUpstream specs = %+v; want the second to be [%+v] — the retired entry's "+
			"bounds must not follow", agent.specs, wantSpec)
	}
	if want := (tui.ServerSwitchResult{Endpoint: bare.Endpoint, HostAlias: "laptop", ContextWindow: 16384}); result != want {
		t.Errorf("move = %+v; want %+v — the top-level pin survives a move", result, want)
	}
	if _, _, pin, _ := live.rebindInputs(config.Options{}, upstreamBinding{}); pin != 16384 {
		t.Errorf("the next rebind's pin = %d; want the top-level 16384 back", pin)
	}
	// The ceiling has no top-level key to fall back to (ADR 0046), so an entry that pins none hands
	// the next rebind the 0 that means "derive it" — never the retired entry's 8,192.
	if _, _, _, outputCap := live.rebindInputs(config.Options{}, upstreamBinding{}); outputCap != 0 {
		t.Errorf("the next rebind's ceiling = %d; want 0 — the retired entry's pin must not follow", outputCap)
	}
}

// The third statement a move carries about the server it lands on: how that server's window is SPLIT
// for the reply — the entry's own `response-reserve:` over the top-level key (item 13). It rides the
// switch spec beside the two token bounds, because a session that moved onto a model answering at
// length must divide THAT server's window its way from the first Turn on the new server rather than
// keep the share the retired one was configured with.
//
// The second move is the half that would be invisible: an entry stating no share falls back to the
// top-level key rather than carrying the retired entry's 0.35 — and the latch behind it moves too, so
// a Firing raised after the move divides the window the same way the session does.
func TestMoveCarriesTheEntrysResponseReserveShare(t *testing.T) {
	t.Parallel()

	agent := &fakeSwitcher{}
	holder := newUpstreamHolder()
	holder.Bind("http://old.invalid:1111", "old-key", "old-model",
		heartbeat.NewMonitor("http://old.invalid:1111", "old-model", "old-key"))
	live := newLiveSettings(config.Options{ContextWindow: 16384, ResponseReserve: 0.2}, nil)
	mover := sessionMover{
		agent: agent, holder: holder, host: &fakeStamper{}, live: live, keys: config.NewKeyResolver(),
		caps: newParallelAgentsCap(&parallelAgentsSpy{}),
	}

	stated := config.ServerEntry{
		Name: "workstation", Endpoint: "http://192.168.64.1:1111",
		ContextWindow: 65536, ResponseReserve: 0.35,
	}
	if _, err := mover.move(stated); err != nil {
		t.Fatalf("move onto the entry stating its own share: %v", err)
	}
	if len(agent.specs) != 1 || agent.specs[0].ResponseReserveFraction != 0.35 {
		t.Errorf("SwitchUpstream specs = %+v; want the first to carry the entry's own 0.35 share",
			agent.specs)
	}
	if base, _, _, _ := live.rebindInputs(config.Options{}, upstreamBinding{}); base.ResponseReserve != 0.35 {
		t.Errorf("the next re-resolution's share = %v; want the moved-to entry's 0.35 — the latch went stale",
			base.ResponseReserve)
	}

	bare := config.ServerEntry{Name: "laptop", Endpoint: "http://127.0.0.1:8080"}
	if _, err := mover.move(bare); err != nil {
		t.Fatalf("move onto the entry stating none: %v", err)
	}
	if len(agent.specs) != 2 || agent.specs[1].ResponseReserveFraction != 0.2 {
		t.Errorf("SwitchUpstream specs = %+v; want the second to fall back to the top-level 0.2, "+
			"never the retired entry's 0.35", agent.specs)
	}
	if base, _, _, _ := live.rebindInputs(config.Options{}, upstreamBinding{}); base.ResponseReserve != 0.2 {
		t.Errorf("the next re-resolution's share = %v; want the top-level 0.2 back once the entry states none",
			base.ResponseReserve)
	}
}

// A session that STARTS on an entry pinning its own `context-window:` budgets against that pin from
// its first Turn, not from its first beat. The pin is flattened onto options at resolution and rides
// the ServerEntry the bind step takes, so it reaches the Config the Agent is CONSTRUCTED from — the
// window the launch projection hands the display is that same resolved number, which is the only
// place a test can read the bind's answer back. The rebind is the other half of the same defect: the
// latch has to be seeded too, or the first beat would bind the observed window over the pin seconds
// after the session opened.
//
// The unpinned row is what keeps the precedence honest in the other direction: an entry that pins
// nothing leaves the top-level `context-window:` key answering, exactly as it did before the entry
// could pin anything at all.
func TestStartupBindHonoursTheEntrysContextWindow(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		entryPin  int
		wantBound int
	}{
		{name: "the startup entry's own pin outranks the top-level key", entryPin: 65536, wantBound: 65536},
		{name: "an entry pinning nothing leaves the top-level key answering", entryPin: 0, wantBound: 16384},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := &recordingLauncher{}
			opts := config.Options{
				Endpoint:  "http://127.0.0.1:1111",
				Model:     "fake",
				Mode:      "ask-before",
				HostAlias: "workstation",
				Workspace: t.TempDir(),
				ConfigDir: t.TempDir(),
				// The two scopes, as ApplyConfig leaves them: the top-level key the whole run
				// carries, and the SELECTED entry's own pin flattened off the `servers:` list.
				ContextWindow:        16384,
				StartupContextWindow: tt.entryPin,
				Servers: []config.ServerEntry{
					{Name: "workstation", Endpoint: "http://127.0.0.1:1111", ContextWindow: config.TokenCount(tt.entryPin)},
				},
				AutoCompact: true,
			}

			if err := runRoot(context.Background(), opts, rec.launch); err != nil {
				t.Fatalf("runRoot: %v", err)
			}
			if rec.opts.ContextWindow != tt.wantBound {
				t.Errorf("tui.Options.ContextWindow = %d; want %d — the window the bind handed the "+
					"engine, so the gauge and the Budget open on one server's number",
					rec.opts.ContextWindow, tt.wantBound)
			}
			// And the first beat cannot undo it: the rebind that observation drives re-resolves the
			// pin off the same latch, so a server advertising 131,072 does not displace it.
			result, err := rec.opts.Server.Rebind("fake", 131072, provider.EffortDialectNone)
			if err != nil {
				t.Fatalf("Rebind: %v", err)
			}
			if result.ContextWindow != tt.wantBound {
				t.Errorf("the first beat's bound window = %d; want the pinned %d rather than the "+
					"observed 131072", result.ContextWindow, tt.wantBound)
			}
		})
	}
}

// The three bounds the entry decides reach the engine through the Config the Agent is CONSTRUCTED
// from — not through a push afterwards, because at a bind there is nothing yet to push at. That
// Config is written onto a copy no caller keeps, which is what serverBinder.build exists for: the
// bind runs exactly as the binary runs it, and the Config it handed the construction is recorded.
//
// The unpinned row is why these are assignments rather than agreements. The Config arriving at this
// step already carries the STARTUP entry's reply cap, its fan-out width and the top-level
// `context-window:`, so an entry that pins none of them must leave the cap at 0 — the engine's own
// derive off the Budget (ADR 0046) — and the width at ADR 0039's serial floor of 1, while the
// top-level key keeps answering for the window (ADR 0045 decision 3). A move onto a bare server
// that kept the retired entry's ceiling is the same defect stated the other way round, and a bare
// server that kept its width is that defect a third time: a slot count is a fact about ONE server.
func TestServerBindHandsTheEntrysBoundsToTheEngine(t *testing.T) {
	t.Parallel()
	// What the session arrived with: the top-level window key, and the cap and fan-out width of the
	// entry it was on.
	const topLevelWindow, retiredCap, retiredWidth = 16384, 111, 7

	tests := []struct {
		name         string
		entry        config.ServerEntry
		wantWindow   int
		wantOutput   int
		wantParallel int
	}{
		{
			name: "the entry's own pins outrank what the session arrived with",
			entry: config.ServerEntry{
				Name: "workstation", Endpoint: "http://127.0.0.1:1111", Model: "pinned-model",
				ContextWindow: 65536, MaxOutputTokens: 4096, ParallelAgents: 3,
			},
			wantWindow:   65536,
			wantOutput:   4096,
			wantParallel: 3,
		},
		{
			name:         "an entry pinning nothing keeps the top-level window, derives the cap and runs serial",
			entry:        config.ServerEntry{Name: "laptop", Endpoint: "http://127.0.0.1:8080"},
			wantWindow:   topLevelWindow,
			wantOutput:   0,
			wantParallel: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			base := validCfg(t)
			base.Context.MaxContextTokens = topLevelWindow
			base.Context.MaxOutputTokens = retiredCap
			base.ParallelAgents = retiredWidth

			engine := newLateEngine(apogee.ModeAskBefore, true)
			t.Cleanup(func() { _ = engine.Close() })
			var handed apogee.Config
			binder := serverBinder{
				cfg:    base,
				engine: engine,
				holder: newUpstreamHolder(),
				caps:   newParallelAgentsCap(engine),
				keys:   config.NewKeyResolver(),
				build: func(cfg apogee.Config, resumed *session.Record) (*apogee.Agent, error) {
					handed = cfg
					return buildAgent(cfg, resumed)
				},
			}
			if err := binder.bind(tt.entry); err != nil {
				t.Fatalf("bind: %v", err)
			}

			if handed.Endpoint != tt.entry.Endpoint {
				t.Fatalf("the Agent was constructed against %q; want the entry's %q — the recorded "+
					"Config is not this bind's", handed.Endpoint, tt.entry.Endpoint)
			}
			if handed.Context.MaxContextTokens != tt.wantWindow {
				t.Errorf("Config.Context.MaxContextTokens = %d; want %d — the window this session's "+
					"very first Turn budgets against", handed.Context.MaxContextTokens, tt.wantWindow)
			}
			if handed.Context.MaxOutputTokens != tt.wantOutput {
				t.Errorf("Config.Context.MaxOutputTokens = %d; want %d — the ceiling ONE reply from "+
					"this server may reach", handed.Context.MaxOutputTokens, tt.wantOutput)
			}
			if handed.ParallelAgents != tt.wantParallel {
				t.Errorf("Config.ParallelAgents = %d; want %d — the width this session's very first "+
					"fan-out may reach, never the retired server's %d",
					handed.ParallelAgents, tt.wantParallel, retiredWidth)
			}
		})
	}
}

// The same bind, one key over: the entry's own `response-reserve:` reaches the Config the Agent is
// CONSTRUCTED from, so the session's very first Turn divides this server's window the way the file
// says for THIS server (item 13). The unpinned row is the precedence's other half — an entry stating
// no share leaves the top-level key the Config arrived carrying answering, rather than zeroing it
// into the engine's own built-in share.
func TestServerBindHandsTheEntrysResponseReserveToTheEngine(t *testing.T) {
	t.Parallel()
	const topLevelShare = 0.2

	tests := []struct {
		name  string
		entry config.ServerEntry
		want  float64
	}{
		{
			name: "the entry's own share outranks the top-level key",
			entry: config.ServerEntry{
				Name: "workstation", Endpoint: "http://127.0.0.1:1111", ResponseReserve: 0.35,
			},
			want: 0.35,
		},
		{
			name:  "an entry stating none keeps the top-level share",
			entry: config.ServerEntry{Name: "laptop", Endpoint: "http://127.0.0.1:8080"},
			want:  topLevelShare,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			base := validCfg(t)
			base.Context.ResponseReserveFraction = topLevelShare

			engine := newLateEngine(apogee.ModeAskBefore, true)
			t.Cleanup(func() { _ = engine.Close() })
			var handed apogee.Config
			binder := serverBinder{
				cfg:    base,
				engine: engine,
				holder: newUpstreamHolder(),
				caps:   newParallelAgentsCap(engine),
				keys:   config.NewKeyResolver(),
				build: func(cfg apogee.Config, resumed *session.Record) (*apogee.Agent, error) {
					handed = cfg
					return buildAgent(cfg, resumed)
				},
			}
			if err := binder.bind(tt.entry); err != nil {
				t.Fatalf("bind: %v", err)
			}
			if handed.Context.ResponseReserveFraction != tt.want {
				t.Errorf("Config.Context.ResponseReserveFraction = %v; want %v — the share this "+
					"session's very first Turn budgets its reply room by",
					handed.Context.ResponseReserveFraction, tt.want)
			}
		})
	}
}

// The same bind, one key further over: the entry's own `working-window:` reaches the Config the
// Agent is CONSTRUCTED from, so the session's very first Turn works in the room the file names for
// THIS server. The unpinned row is the precedence's other half — an entry bounding nothing leaves
// the top-level key the Config arrived carrying answering, rather than zeroing the bound away into
// "work in the whole advertised window".
func TestBindServerResolvesTheWorkingWindow(t *testing.T) {
	t.Parallel()
	const topLevelRoom = 65536

	tests := []struct {
		name  string
		entry config.ServerEntry
		want  int
	}{
		{
			name: "the entry's own bound outranks the top-level key",
			entry: config.ServerEntry{
				Name: "big-window-cloud", Endpoint: "http://127.0.0.1:1111",
				ContextWindow: 1310720, WorkingWindow: 200000,
			},
			want: 200000,
		},
		{
			name:  "an entry bounding nothing keeps the top-level room",
			entry: config.ServerEntry{Name: "laptop", Endpoint: "http://127.0.0.1:8080"},
			want:  topLevelRoom,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			base := validCfg(t)
			base.Context.WorkingWindow = topLevelRoom

			engine := newLateEngine(apogee.ModeAskBefore, true)
			t.Cleanup(func() { _ = engine.Close() })
			var handed apogee.Config
			binder := serverBinder{
				cfg:    base,
				engine: engine,
				holder: newUpstreamHolder(),
				caps:   newParallelAgentsCap(engine),
				keys:   config.NewKeyResolver(),
				build: func(cfg apogee.Config, resumed *session.Record) (*apogee.Agent, error) {
					handed = cfg
					return buildAgent(cfg, resumed)
				},
			}
			if err := binder.bind(tt.entry); err != nil {
				t.Fatalf("bind: %v", err)
			}
			if handed.Context.WorkingWindow != tt.want {
				t.Errorf("Config.Context.WorkingWindow = %d; want %d — the room this session's very "+
					"first Turn budgets inside", handed.Context.WorkingWindow, tt.want)
			}
		})
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
