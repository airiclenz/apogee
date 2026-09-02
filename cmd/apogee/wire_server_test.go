package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/heartbeat"
	"github.com/airiclenz/apogee/internal/library"
	"github.com/airiclenz/apogee/internal/profiles"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/session"
	"github.com/airiclenz/apogee/internal/tui"
	llamalauncher "github.com/airiclenz/llama-launcher/launcher"
)

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

// ----------------------------------------------------------------------------
// The llama-launcher seams (ADR 0029 D1/D2)
// ----------------------------------------------------------------------------

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
			keys: config.NewKeyResolver(""),
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
	wantSpec := apogee.UpstreamSpec{
		Endpoint: "http://127.0.0.1:9090", APIKey: "llamacpp-key",
		ServerName: "there", MaxContextTokens: 16384,
	}
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
	wantSpec := apogee.UpstreamSpec{
		Endpoint: dial, APIKey: "llamacpp-key",
		ServerName: "there", MaxContextTokens: 16384,
	}
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
		keys: config.NewKeyResolver(""), caps: newParallelAgentsCap(&parallelAgentsSpy{})}

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
		ServerName: pinned.Name, ServerDescription: pinned.Description,
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
	wantSpec = apogee.UpstreamSpec{
		Endpoint: bare.Endpoint, ServerName: bare.Name, MaxContextTokens: 16384,
	}
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
		agent: agent, holder: holder, host: &fakeStamper{}, live: live, keys: config.NewKeyResolver(""),
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
				keys:   config.NewKeyResolver(""),
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
				keys:   config.NewKeyResolver(""),
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
				keys:   config.NewKeyResolver(""),
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

// The orientation block names the SESSION seat by the entry the session is bound to when the model
// is offered a seat to choose (ADR 0069), and those two strings ride the Config the Agent is
// constructed from rather than a push afterwards: a session that starts on a described entry must be
// able to say what this box is from its very first Turn, and the entry is in hand exactly here.
//
// An entry that describes nothing carries an empty description, which the block reads as a seat with
// a name and no words — never as a reason to name no seat.
func TestBindServerCarriesTheEntrysOwnWords(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		entry config.ServerEntry
	}{
		{
			name: "a described entry",
			entry: config.ServerEntry{
				Name: "workstation", Endpoint: "http://127.0.0.1:1111",
				Description: "the big box upstairs — 120B, slow and thorough",
			},
		},
		{
			name:  "an entry nobody described",
			entry: config.ServerEntry{Name: "laptop", Endpoint: "http://127.0.0.1:8080"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			engine := newLateEngine(apogee.ModeAskBefore, true)
			t.Cleanup(func() { _ = engine.Close() })
			var handed apogee.Config
			binder := serverBinder{
				cfg:    validCfg(t),
				engine: engine,
				holder: newUpstreamHolder(),
				caps:   newParallelAgentsCap(engine),
				keys:   config.NewKeyResolver(""),
				build: func(cfg apogee.Config, resumed *session.Record) (*apogee.Agent, error) {
					handed = cfg
					return buildAgent(cfg, resumed)
				},
			}
			if err := binder.bind(tt.entry); err != nil {
				t.Fatalf("bind: %v", err)
			}

			if handed.ServerName != tt.entry.Name {
				t.Errorf("Config.ServerName = %q; want the entry's own name %q",
					handed.ServerName, tt.entry.Name)
			}
			if handed.ServerDescription != tt.entry.Description {
				t.Errorf("Config.ServerDescription = %q; want the entry's own %q",
					handed.ServerDescription, tt.entry.Description)
			}
		})
	}
}

// The same two words at STARTUP, over the whole path a fresh session actually takes: the entry as
// the human wrote it, resolution's flattening onto the options, the startup ServerEntry
// re-assembled out of them (startupEntry) and the bind.
//
// The test above binds an entry a `/server` switch already holds. This one holds none: between the
// file and the bind the entry is only its flattened fields, and a `description:` that is not among
// them is gone by the time the Config is written. That is a session whose Delegations line names
// the box it runs on but never says what it is FOR — the half of the choice ADR 0069 decision 5
// exists to give the model — and only heals itself on the first `/server` switch, which is the one
// moment the human was not asking for a delegation.
func TestStartupBindCarriesTheEntrysOwnWords(t *testing.T) {
	t.Parallel()
	const description = "the big box upstairs, slow and thorough"
	opts := config.Options{
		Workspace: t.TempDir(),
		ConfigDir: testConfigHome(t, "servers:\n"+
			"  - name: workstation\n"+
			"    endpoint: http://127.0.0.1:1111\n"+
			"    description: "+description+"\n"+
			"server: workstation\n"),
	}
	if err := config.ApplyConfig(&opts, func(string) bool { return false },
		func(string) string { return "" }, os.ReadFile, noNotify); err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}

	engine := newLateEngine(apogee.ModeAskBefore, true)
	t.Cleanup(func() { _ = engine.Close() })
	var handed apogee.Config
	binder := serverBinder{
		cfg:    validCfg(t),
		engine: engine,
		holder: newUpstreamHolder(),
		caps:   newParallelAgentsCap(engine),
		keys:   config.NewKeyResolver(""),
		build: func(cfg apogee.Config, resumed *session.Record) (*apogee.Agent, error) {
			handed = cfg
			return buildAgent(cfg, resumed)
		},
	}
	if err := binder.bind(startupEntry(opts)); err != nil {
		t.Fatalf("bind: %v", err)
	}

	if handed.ServerName != "workstation" {
		t.Errorf("Config.ServerName = %q; want the startup entry's own name %q",
			handed.ServerName, "workstation")
	}
	if handed.ServerDescription != description {
		t.Errorf("Config.ServerDescription = %q; want the startup entry's own %q — the words the "+
			"orientation block describes the session Delegation seat with, from the first Turn "+
			"rather than from the first /server switch", handed.ServerDescription, description)
	}
}
