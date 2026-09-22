package main

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/heartbeat"
	"github.com/airiclenz/apogee/internal/notice"
	"github.com/airiclenz/apogee/internal/reactions"
	// Aliased because the tests below hold a skills.Provider in a variable called `provider`,
	// which shadows the package name inside those functions.
	apiprovider "github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/run"
	"github.com/airiclenz/apogee/internal/session"
	"github.com/airiclenz/apogee/internal/skills"
	"github.com/airiclenz/apogee/internal/stubllm"
)

// firingRoots is one throwaway set of state roots, each a real directory so the scratch dir the
// composer creates under them can be stat'd.
// firingRoots builds the state roots a Firing composes against. Every dir is symlink-RESOLVED:
// the skill provider mounts its anchors by their real paths (internal/skills readRoots), so a
// test that named them through a symlink would compare the provider's resolved answer against
// its own unresolved spelling. On Linux the two coincide; on macOS t.TempDir() sits under /var,
// a symlink to /private/var, and they do not.
func firingRoots(t *testing.T) stateRoots {
	t.Helper()

	home := readFenceRealDir(t)
	return stateRoots{
		config:    home,
		sessions:  filepath.Join(home, "sessions"),
		probe:     filepath.Join(home, "probe"),
		prompts:   filepath.Join(home, "prompts"),
		schemes:   filepath.Join(home, "schemes"),
		scratch:   readFenceRealDir(t),
		workspace: readFenceRealDir(t),
	}
}

// firingBeat is a dictated observation of a Firing's OWN server: a reachable box that advertises
// nothing. It exists because one beat per Firing is unconditional — the round trip is the liveness
// gate, not an optimisable probe — so a composition test about anything else would otherwise dial
// `box.example` for real and wait out the discovery timeout.
func firingBeat(context.Context, string, string, string, apiprovider.Wire) heartbeat.Beat {
	return heartbeat.Beat{Reachable: true, Answered: true}
}

// Every field an unattended run's Config carries, asserted in one place — which is the whole point
// of the composer existing. Most of these were previously spelled out once per Driver and asserted
// by no Driver's tests at all: the roster rungs, the host allow/deny lists, the scrubbed variable
// names, the Inspector switch and the context files could each have gone missing from one of the
// three copies and nothing would have said so.
func TestFiringConfigSetsEveryUnattendedField(t *testing.T) {
	t.Parallel()

	roots := firingRoots(t)
	opts := config.Options{
		// Bypass doubles as the Mechanisms floor.
		Bypass:             true,
		ConfineToWorkspace: true,
		WebSearchEndpoint:  "https://search.example/v1",
		ToolsDisabled:      []string{"run_terminal_cmd"},
		ToolsEnabled:       []string{"web_search"},
		URLAllowHosts:      []string{"allowed.example"},
		URLDenyHosts:       []string{"denied.example"},
		UI:                 domain.UIPrefs{Inspector: true},
		UndoSnapshots:      true,
		ContextFiles:       []string{"AGENTS.md"},
		AutoCompact:        true,
		PruneToolResults:   true,
		DelegateMaxSteps:   12,
		ContextWindow:      16384,
		ResponseReserve:    0.2,
		Servers: []config.ServerEntry{
			{Name: "box", Endpoint: "http://box.example/v1", APIKeyEnv: "BOX_KEY"},
		},
	}
	entry := config.ServerEntry{
		Name:            "box",
		Endpoint:        "http://box.example/v1",
		Description:     "the workhorse in the closet",
		Model:           "entry-model",
		ParallelAgents:  3,
		MaxOutputTokens: 4096,
		ContextWindow:   65536,
		ResponseReserve: 0.35,
		EffortDialect:   "reasoning",
	}
	provider := skills.NewProvider(skills.Sources{Home: roots.config, Workspace: roots.workspace})
	beats := &stubBeat{beat: heartbeat.Beat{
		Reachable:     true,
		Answered:      true,
		TotalSlots:    9,
		EffortSupport: apiprovider.EffortSupport{Dialect: apiprovider.EffortDialectOpenAI},
	}}
	cfg, _, _, err := firingConfig(context.Background(), firingInputs{
		opts:     opts,
		entry:    entry,
		apiKey:   "sk-handed-over",
		roots:    roots,
		confiner: fenceableHost,
		model:    "overlay-model",
		mode:     domain.ModeAuto,
		skills:   provider,
		beat:     beats.discover,
		recordID: "2026-08-24T09-00-00-firing",
	})
	if err != nil {
		t.Fatalf("firingConfig: %v", err)
	}

	// The server half: the entry decides the endpoint and the key, and a Driver's model overlay
	// outranks the entry's own `model:` (ADR 0055 decision 2).
	if cfg.Endpoint != entry.Endpoint {
		t.Errorf("Config.Endpoint = %q; want the bound entry's %q", cfg.Endpoint, entry.Endpoint)
	}
	if cfg.Model != "overlay-model" {
		t.Errorf("Config.Model = %q; want the overlay %q the Driver named", cfg.Model, "overlay-model")
	}
	if cfg.APIKey != "sk-handed-over" {
		t.Errorf("Config.APIKey = %q; want the key the Driver had already resolved", cfg.APIKey)
	}
	if cfg.Mode != domain.ModeAuto {
		t.Errorf("Config.Mode = %v; want the mode the Firing runs in", cfg.Mode)
	}
	// The same entry in the human's own words, which is what the orientation block names the SESSION
	// seat by when the model is offered a seat to choose (ADR 0069).
	if cfg.ServerName != entry.Name || cfg.ServerDescription != entry.Description {
		t.Errorf("Config server identity = (%q, %q); want the bound entry's own (%q, %q) — without them "+
			"the orientation block can only call this box \"this server\"",
			cfg.ServerName, cfg.ServerDescription, entry.Name, entry.Description)
	}

	// The roots half, plus the scratch dir the record id names — a run whose model has no writable
	// scratch inside the box writes its working files wherever else it can reach.
	if cfg.ConfigDir != roots.config || cfg.WorkspaceDir != roots.workspace {
		t.Errorf("the state roots did not reach the Config: %q / %q", cfg.ConfigDir, cfg.WorkspaceDir)
	}
	wantScratch := filepath.Join(roots.scratch, "2026-08-24T09-00-00-firing")
	if cfg.ScratchDir != wantScratch {
		t.Errorf("Config.ScratchDir = %q; want %q — the dir the record is named after", cfg.ScratchDir, wantScratch)
	}
	if _, err := os.Stat(wantScratch); err != nil {
		t.Errorf("the scratch dir was not created: %v", err)
	}

	// The file-only keys, every one of which must reach an unattended run exactly as it reaches an
	// interactive session: one configuration, whichever Driver reads it (ADR 0031).
	if cfg.Confiner != apogee.Confiner(fenceableHost) {
		t.Error("Config.Confiner is not the backend the Driver handed over; the run would be fenced by something else")
	}
	if !cfg.ConfineToWorkspace {
		t.Error("Config.ConfineToWorkspace = false; the posture the host configured did not reach the run")
	}
	if !cfg.Bypass {
		t.Error("Config.Bypass = false; the Mechanisms floor did not reach the run")
	}
	if cfg.WebSearchEndpoint != opts.WebSearchEndpoint {
		t.Errorf("Config.WebSearchEndpoint = %q; want %q", cfg.WebSearchEndpoint, opts.WebSearchEndpoint)
	}
	if !slices.Equal(cfg.DisabledTools, opts.ToolsDisabled) || !slices.Equal(cfg.EnabledTools, opts.ToolsEnabled) {
		t.Errorf("the tool roster rungs did not reach the run: disabled %v, enabled %v", cfg.DisabledTools, cfg.EnabledTools)
	}
	if !slices.Equal(cfg.URLAllowHosts, opts.URLAllowHosts) || !slices.Equal(cfg.URLDenyHosts, opts.URLDenyHosts) {
		t.Errorf("the url-safety host layer did not reach the run: allow %v, deny %v", cfg.URLAllowHosts, cfg.URLDenyHosts)
	}
	if !cfg.Inspector {
		t.Error("Config.Inspector = false; the wire capture the host armed did not reach the run")
	}
	if !cfg.UndoSnapshots {
		t.Error("Config.UndoSnapshots = false; the run would open no undo store and `apogee undo` would have nothing to reverse")
	}
	if !slices.Equal(cfg.ContextFiles, opts.ContextFiles) {
		t.Errorf("Config.ContextFiles = %v; want %v", cfg.ContextFiles, opts.ContextFiles)
	}
	if want := config.APIKeyEnvNames(opts); !slices.Equal(cfg.SecretEnvVars, want) {
		t.Errorf("Config.SecretEnvVars = %v; want %v — the variables a subprocess must not inherit", cfg.SecretEnvVars, want)
	}

	// The skills contract, both halves off the SAME provider: the prompt resolution and the read
	// roots the model may then reach into.
	if cfg.Skills != provider {
		t.Error("Config.Skills is not the provider the Driver shared; a live catalog would stop following")
	}
	if cfg.ExtraReadRoots == nil {
		t.Fatal("Config.ExtraReadRoots is nil; the model could not read the files of a skill it was given")
	}
	assertReadRootsCompose(t, cfg.ExtraReadRoots, provider.ReadRoots())
	// The three bounds the BOUND entry carries outrank the top-level keys, and a pin answers the
	// fan-out width without spending a round trip on a question already settled.
	if cfg.Context.MaxContextTokens != int(entry.ContextWindow) {
		t.Errorf("Context.MaxContextTokens = %d; want the entry's pin %d", cfg.Context.MaxContextTokens, entry.ContextWindow)
	}
	if cfg.Context.MaxOutputTokens != entry.MaxOutputTokens {
		t.Errorf("Context.MaxOutputTokens = %d; want the entry's pin %d", cfg.Context.MaxOutputTokens, entry.MaxOutputTokens)
	}
	if cfg.Context.ResponseReserveFraction != entry.ResponseReserve {
		t.Errorf("Context.ResponseReserveFraction = %v; want the entry's share %v — the spec and the Config "+
			"must not state two splits of one window", cfg.Context.ResponseReserveFraction, entry.ResponseReserve)
	}
	if !cfg.Context.CompactionEnabled {
		t.Error("Context.CompactionEnabled = false; the host's auto-compact setting did not reach the run")
	}
	if !cfg.Context.PruneToolResults {
		t.Error("Context.PruneToolResults = false; the host's prune-tool-results setting did not reach the run")
	}
	// The delegate step cap is top-level rather than per-entry, so an unattended run takes the
	// host's own key: a Firing is exactly the run where an unbounded delegation is nobody's to stop.
	if cfg.Delegation.MaxSteps != opts.DelegateMaxSteps {
		t.Errorf("Delegation.MaxSteps = %d; want the host's delegate-max-steps %d",
			cfg.Delegation.MaxSteps, opts.DelegateMaxSteps)
	}
	if cfg.ParallelAgents != entry.ParallelAgents {
		t.Errorf("Config.ParallelAgents = %d; want the entry's pin %d", cfg.ParallelAgents, entry.ParallelAgents)
	}
	if beats.calls != 1 {
		t.Errorf("the composer took %d beats; want exactly one. The observation is unconditional — the "+
			"round trip is the liveness gate an unattended Driver refuses a Firing on, so the pins "+
			"decide the VALUES rather than whether the server is looked at — and it is ONE, because "+
			"two probes of one server at one moment can report a state it was never in", beats.calls)
	}

	// And the effort wire shape, which reaches the run the same way the bounds do: a Driver that
	// never rebinds would otherwise send the zero dialect — the historical chat_template_kwargs
	// shape — whatever the bound server actually reads (2026-08-25 audit C-03, ADR 0031 parity).
	// The entry FORCES one here, so it is the answer whatever the beat above saw.
	if cfg.EffortDialect != domain.EffortDialectReasoning {
		t.Errorf("Config.EffortDialect = %q; want the entry's forced %q — the beat observed %q and a "+
			"forced dialect outranks it", cfg.EffortDialect, domain.EffortDialectReasoning,
			apiprovider.EffortDialectOpenAI)
	}
}

// A Firing runs the FLOOR the session it was raised from runs (ADR 0071). The seven keys are
// positive in the session's Options and Disable… at the engine, and floorFromOptions is the one seam
// that negates them — so a run nobody watches must arrive with exactly the guard the human took away
// and the six they did not.
func TestFiringConfigCarriesTheFloorGuardKeys(t *testing.T) {
	t.Parallel()

	roots := firingRoots(t)
	opts := config.Options{
		// Bypass says the second half of the claim: it takes the lab rows away and leaves every
		// Floor guard exactly where the seven keys put it.
		Bypass: true,
		Servers: []config.ServerEntry{
			{Name: "box", Endpoint: "http://box.example/v1"},
		},
		// What a session running `tool-result-cap: false` and nothing else projects.
		ToolUseEnforcer:       true,
		EmptyResponseRecovery: true,
		ToolCallRepair:        true,
		ToolCallSalvage:       true,
		ToolLoopBreaker:       true,
		ToolResultCap:         false,
		ReadCache:             true,
	}
	provider := skills.NewProvider(skills.Sources{Home: roots.config, Workspace: roots.workspace})

	cfg, _, _, err := firingConfig(context.Background(), firingInputs{
		opts:     opts,
		entry:    config.ServerEntry{Name: "box", Endpoint: "http://box.example/v1"},
		roots:    roots,
		confiner: fenceableHost,
		mode:     domain.ModeAuto,
		skills:   provider,
		beat:     firingBeat,
		recordID: "2026-09-03T09-00-00-firing",
	})
	if err != nil {
		t.Fatalf("firingConfig: %v", err)
	}
	want := apogee.FloorConfig{DisableToolResultCap: true}
	if cfg.Floor != want {
		t.Errorf("Config.Floor = %+v; want %+v — the one guard the session gave up, and no other",
			cfg.Floor, want)
	}
}

// A Firing runs the `context-fill-notice` switch the session it was raised from runs (ADR 0077),
// carried as is — the key is not a Floor guard, so nothing negates it on the trip. A Firing is
// composed out of the session's LIVE options, so a session that switched the notice on raises runs
// whose model is told how full its context is, and one that said nothing raises runs without it.
func TestFiringConfigCarriesTheContextFillNotice(t *testing.T) {
	t.Parallel()

	for _, want := range []bool{false, true} {
		t.Run(fmt.Sprintf("context-fill-notice=%v", want), func(t *testing.T) {
			t.Parallel()
			roots := firingRoots(t)
			opts := config.Options{
				Servers: []config.ServerEntry{
					{Name: "box", Endpoint: "http://box.example/v1"},
				},
				ContextFillNotice: want,
			}
			provider := skills.NewProvider(skills.Sources{Home: roots.config, Workspace: roots.workspace})

			cfg, _, _, err := firingConfig(context.Background(), firingInputs{
				opts:     opts,
				entry:    config.ServerEntry{Name: "box", Endpoint: "http://box.example/v1"},
				roots:    roots,
				confiner: fenceableHost,
				mode:     domain.ModeAuto,
				skills:   provider,
				beat:     firingBeat,
				recordID: "2026-09-03T09-00-00-firing",
			})
			if err != nil {
				t.Fatalf("firingConfig: %v", err)
			}
			if cfg.ContextFillNotice != want {
				t.Errorf("Config.ContextFillNotice = %v; want the session's %v", cfg.ContextFillNotice, want)
			}
		})
	}
}

// A workspace skill anchor that is a symlink OUT of the workspace is discovered as a source and
// mounted nowhere (audit 2026-08-25 F-13; residual 2026-08-28). The provider answers two lists on
// purpose and they are not interchangeable: SourceDirs is the DISPLAY view — where skills come
// from, the path a /skills report and a skip record name, spelled as configured — while ReadRoots
// is the MOUNT view, symlink-resolved, with an untrusted workspace anchor that leaves its base
// dropped altogether. A Firing composed on SourceDirs would hand read_file, grep, list_dir and
// find_files the very tree discovery refuses to scan, in the one run shape with no human watching.
// Nothing but this test stands between the two spellings at the mount site.
func TestFiringConfigMountsNoEscapingSkillRoot(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("the escape is a POSIX symlink; internal/skills asserts the mount rule on its own tests there")
	}

	roots := firingRoots(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(roots.workspace, ".apogee")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	provider := skills.NewProvider(skills.Sources{Home: roots.config, Workspace: roots.workspace})
	escaping := filepath.Join(roots.workspace, ".apogee", "skills")

	cfg, _, _, err := firingConfig(context.Background(), firingInputs{
		opts: config.Options{Bypass: true},
		// Both bounds pinned so the composition settles with no discovery round trip: this test is
		// about the mount, not about what a server would answer.
		entry:    config.ServerEntry{Endpoint: "http://box.example/v1", ParallelAgents: 1, EffortDialect: "reasoning"},
		apiKey:   "sk-test",
		roots:    roots,
		confiner: fenceableHost,
		mode:     domain.ModePlan,
		skills:   provider,
		beat:     firingBeat,
		recordID: "2026-08-24T13-00-00-firing",
	})
	if err != nil {
		t.Fatalf("firingConfig: %v", err)
	}

	// The fixture's own precondition: the relocated anchor really is one of this provider's sources,
	// so the absence below is a decision the mount made and not an anchor that was never there.
	if !slices.Contains(provider.SourceDirs(), escaping) {
		t.Fatalf("SourceDirs() = %v; want the relocated anchor %q among them — the fixture no longer sets up the case",
			provider.SourceDirs(), escaping)
	}
	if cfg.ExtraReadRoots == nil {
		t.Fatal("Config.ExtraReadRoots is nil; the model could not read the files of a skill it was given")
	}
	if got := cfg.ExtraReadRoots(); slices.Contains(got, escaping) {
		t.Errorf("Config.ExtraReadRoots() = %v mounts the relocated anchor %q; the composer took the display "+
			"view (SourceDirs) where only the resolved mount view (ReadRoots) may be mounted", got, escaping)
	}
}

// The three optional seams, each nil, each taking the documented default: a fresh key resolver asks
// the entry's own source, a fresh catalog is built from the roots, and the width and the effort
// dialect both come off the one-shot beat observeServer takes of the REAL server — so the upstream
// is a scripted one that advertises both, and what it advertises is what the Config must carry. The
// dialect it advertises is the one discovery can observe: a per-model `reasoning` object is the
// OpenRouter tell (provider.EffortDialectReasoning); `openai` has no tell and is forced-only. Those
// defaults are what headless and the daemon rely on — they have no longer-lived facility to share —
// so a change of default is a change to two Drivers at once.
func TestFiringConfigDefaultsItsSeams(t *testing.T) {
	roots := firingRoots(t)

	srv := stubllm.New(t, stubllm.Script{Discovery: stubllm.Discovery{
		Models: []stubllm.DiscoveredModel{{ID: "entry-model", Reasoning: &stubllm.ModelReasoning{}}},
		Props:  &stubllm.Props{TotalSlots: 4},
	}})

	entry := config.ServerEntry{Name: "box", Endpoint: srv.URL, APIKey: "sk-from-the-entry", Model: "entry-model"}
	cfg, _, _, err := firingConfig(context.Background(), firingInputs{
		opts:     config.Options{Bypass: true},
		entry:    entry,
		roots:    roots,
		confiner: fenceableHost,
		mode:     domain.ModePlan,
		recordID: "2026-08-24T10-00-00-firing",
	})
	if err != nil {
		t.Fatalf("firingConfig: %v", err)
	}

	if cfg.APIKey != "sk-from-the-entry" {
		t.Errorf("Config.APIKey = %q; a nil resolver must still ask the bound entry's own key source", cfg.APIKey)
	}
	if cfg.Model != entry.Model {
		t.Errorf("Config.Model = %q; an unnamed model takes the bound entry's own %q", cfg.Model, entry.Model)
	}
	if cfg.Skills == nil {
		t.Fatal("Config.Skills is nil; a nil provider must build one from the roots, not leave the run without a catalog")
	}
	if cfg.ExtraReadRoots == nil {
		t.Fatal("Config.ExtraReadRoots is nil; the fresh catalog's dirs were not mounted")
	}
	if want := filepath.Join(roots.config, "skills"); !slices.Contains(cfg.ExtraReadRoots(), want) {
		t.Errorf("Config.ExtraReadRoots() = %v; want the home library %q among them", cfg.ExtraReadRoots(), want)
	}
	if len(srv.Probes()) == 0 {
		t.Error("the discovery beat never ran; a nil beat seam must take observeServer, and an unpinned " +
			"entry has no other way to learn how wide it may fan out or which wire its server reads")
	}
	if cfg.ParallelAgents != 4 {
		t.Errorf("Config.ParallelAgents = %d; want the 4 the server's /props reported", cfg.ParallelAgents)
	}
	if cfg.EffortDialect != domain.EffortDialectReasoning {
		t.Errorf("Config.EffortDialect = %q; want the %q the beat observed — an unattended run must reach the wire a session reaches",
			cfg.EffortDialect, domain.EffortDialectReasoning)
	}
}

// Driver parity for the shipped skill source (ADR 0031): a Firing with no session to share builds
// its own catalog from the resolved options, so BOTH skill gates must travel to it. Left out,
// `use-shipped-skills` would reach the fresh Sources as its zero value — shipped off — and a
// `/debugging` token in a headless or daemon prompt would attach the body in the TUI and silently
// stay prose in every unattended run.
func TestFiringConfigCarriesTheShippedSkillGate(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name string
		on   bool
		want int
	}{
		{name: "gate on resolves the shipped skill", on: true, want: 1},
		{name: "gate off leaves it out of the catalog", on: false, want: 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg, _, _, err := firingConfig(context.Background(), firingInputs{
				opts:  config.Options{Bypass: true, UseShippedSkills: tt.on},
				entry: config.ServerEntry{Endpoint: "http://box.example/v1", ParallelAgents: 1, EffortDialect: "reasoning"},
				// The one shared seam left nil on purpose: this is the headless and daemon shape, where
				// the catalog is composed here rather than handed over by a longer-lived session.
				apiKey:   "sk-test",
				roots:    firingRoots(t),
				confiner: fenceableHost,
				mode:     domain.ModePlan,
				beat:     firingBeat,
				recordID: "2026-08-24T14-00-00-firing",
			})
			if err != nil {
				t.Fatalf("firingConfig: %v", err)
			}
			if cfg.Skills == nil {
				t.Fatal("Config.Skills is nil; the run has no catalog to resolve a /token through")
			}
			if got := cfg.Skills.ResolveSkills([]string{"debugging"}); len(got) != tt.want {
				t.Errorf("ResolveSkills(debugging) resolved %d skills, want %d", len(got), tt.want)
			}
		})
	}
}

// The delegates run.Once pins for itself stay nil whatever the configuration says: handing the
// runner an Approver, an Asker, a Presenter or an Events sink is how an unattended run acquires a
// human it does not have (ADR 0033 decision 2).
//
// The tool registry stays nil with them on every path but ONE. `sub-agents-choice:` shapes the
// sub_agent schema rather than any field of the Config the engine reads (ADR 0031), so the only way
// an unattended run can publish `run_on` is to hand over a roster assembled by the host — which is
// why the gate is also the guard: under `fixed`, and with the key absent, Tools stays nil
// byte-for-byte and the engine goes on building its own roster off the delegates run.Once pins.
// A Firing reaches no external MCP server either way (ADR 0034), so the assembled registry is the
// built-in set alone.
func TestFiringConfigLeavesTheDriverSeamsNil(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		choice    config.SubAgentsChoice
		wantTools bool
	}{
		{name: "the key absent leaves the engine its own roster"},
		{name: "fixed leaves the engine its own roster", choice: config.SubAgentsChoiceFixed},
		{
			name:      "model hands over a roster that publishes the seat",
			choice:    config.SubAgentsChoiceModel,
			wantTools: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg, _, _, err := firingConfig(context.Background(), firingInputs{
				opts:     config.Options{Bypass: true, SubAgentsChoice: tc.choice},
				entry:    config.ServerEntry{Endpoint: "http://box.example/v1", ParallelAgents: 1},
				apiKey:   "sk-test",
				roots:    firingRoots(t),
				confiner: fenceableHost,
				mode:     domain.ModePlan,
				beat:     firingBeat,
				recordID: "2026-08-24T11-00-00-firing",
			})
			if err != nil {
				t.Fatalf("firingConfig: %v", err)
			}

			if cfg.Events != nil || cfg.Approver != nil || cfg.Asker != nil || cfg.Presenter != nil {
				t.Error("the composer wired a delegate run.Once pins for itself")
			}
			if got := cfg.Tools != nil; got != tc.wantTools {
				t.Fatalf("cfg.Tools non-nil = %v, want %v — the seat gate is the only thing that "+
					"may hand the runner a registry", got, tc.wantTools)
			}
			if !tc.wantTools {
				return
			}
			// Read off the registry the composer actually returned, never off a fixture: what the
			// model is offered is the schema this object publishes.
			if !seatChoiceOffered(t, cfg.Tools) {
				t.Error("the composed registry publishes no run_on; `model` is the whole of what " +
					"offers an unattended run's model a seat")
			}
		})
	}
}

// The other half of the gate. Publishing `run_on` is worth nothing unless the model is told what the
// two values mean, and that clause list is rendered only when the Agent's OWN sub_agent tool
// published the argument (delegationSeats). This drives the real headless path — the composer's
// Config and routing through run.Once, exactly as `apogee headless` does — and reads the Delegations
// bullet back off the wire, which is the one place a claim about what apogee announced can be made.
func TestFiringOrientationNamesBothSeatsUnderSeatChoice(t *testing.T) {
	session := stubllm.New(t, stubllm.Script{
		Model: "session-model",
		Turns: []stubllm.Turn{{Text: "nothing to delegate"}},
	})
	// The Sub-agent server is REAL too: the composer beats it through observeServer to resolve the
	// far seat, so it is a scripted upstream that advertises the grunt model and a two-slot width.
	gruntServer := stubllm.New(t, stubllm.Script{
		Model:     "grunt-model",
		Discovery: stubllm.Discovery{Props: &stubllm.Props{TotalSlots: 2}},
	})
	grunt := config.ServerEntry{
		Name:        "grunt",
		Endpoint:    gruntServer.URL,
		Description: "the cheap box",
		Model:       "grunt-model",
		APIKey:      "sk-grunt",
	}

	cfg, routing, _, err := firingConfig(context.Background(), firingInputs{
		opts: config.Options{
			Bypass:          true,
			Servers:         []config.ServerEntry{grunt},
			SubAgentsServer: "grunt",
			SubAgentsChoice: config.SubAgentsChoiceModel,
			// The orientation block rides ALONG on a standing system message (ADR 0023 §6
			// amendment), so a run with no prompt at all states no block. The text is stated here
			// rather than left to the embedded default for e2e_seat_test.go's reason: a fixture
			// leaning on apogee's own wording would be asserting about that wording too.
			SystemPrompt: config.SystemPromptSettings{
				Global: config.PromptSource{Text: "You are apogee, a terminal coding agent."},
			},
		},
		entry: config.ServerEntry{
			Name:           "box",
			Endpoint:       session.URL,
			Model:          session.Model,
			Description:    "the session box",
			APIKey:         "sk-test",
			ParallelAgents: 1,
			EffortDialect:  "reasoning",
		},
		roots:    firingRoots(t),
		confiner: fenceableHost,
		mode:     domain.ModePlan,
		beat:     firingBeat,
		recordID: "2026-09-02T10-00-00-firing",
	})
	if err != nil {
		t.Fatalf("firingConfig: %v", err)
	}
	if routing.target == nil || routing.seat == nil {
		t.Fatalf("routing resolved target=%v seat=%v; the fixture names a reachable Sub-agent server",
			routing.target, routing.seat)
	}

	if _, err := run.Once(context.Background(), run.Spec{
		Config:           cfg,
		Prompt:           "say something",
		DelegationTarget: routing.target,
		DelegationSeat:   routing.seat,
	}); err != nil {
		t.Fatalf("run.Once: %v", err)
	}

	line := seatFirstDelegationsLine(t, session)
	for _, want := range []string{
		`run_on "session" = ` + session.Model + " on box — the session box",
		`run_on "sub-agents-server" = grunt-model on grunt — the cheap box`,
	} {
		if !strings.Contains(line, want) {
			t.Errorf("the Firing's Delegations line does not state %q:\n%s", want, line)
		}
	}
}

// The entry's wire reaches both beats (ADR 0078): the Firing's own server is observed under the
// wire its entry names, and the `sub-agents-server:` entry under ITS wire — a mixed pair, so an
// anthropic entry cannot be observed as an openai one by either beat, and the fold of an unnamed
// wire onto openai is asserted rather than assumed. Both servers are scripted upstreams and the
// wire is read off the model-list probe each one logged: a Messages-wire probe carries the
// `anthropic-version` header every Messages client sends and a chat-completions probe never does
// (internal/stubllm renders the list by that same tell). And the same two values ride past the
// beats onto what the engine dials with: the Config carries the bound entry's wire as written, the
// routed target the grunt entry's own — so the run's session client and its routed children each
// open the connection a session on that entry would.
func TestFiringConfigBeatsCarryEachEntrysWire(t *testing.T) {
	primary := stubllm.New(t, stubllm.Script{
		Model:     "claude",
		Discovery: stubllm.Discovery{Props: &stubllm.Props{TotalSlots: 1}},
	})
	delegation := stubllm.New(t, stubllm.Script{
		Model:     "grunt-model",
		Discovery: stubllm.Discovery{Props: &stubllm.Props{TotalSlots: 2}},
	})

	grunt := config.ServerEntry{Name: "grunt", Endpoint: delegation.URL, Model: "grunt-model", APIKey: "sk-grunt"}
	cfg, routing, _, err := firingConfig(context.Background(), firingInputs{
		opts: config.Options{
			Bypass:          true,
			Servers:         []config.ServerEntry{grunt},
			SubAgentsServer: "grunt",
		},
		entry: config.ServerEntry{
			Name: "box", Endpoint: primary.URL, Model: "claude", APIKey: "sk-test", Wire: "anthropic",
		},
		roots:    firingRoots(t),
		confiner: fenceableHost,
		mode:     domain.ModePlan,
		recordID: "2026-09-16T10-00-00-firing",
	})
	if err != nil {
		t.Fatalf("firingConfig: %v", err)
	}

	if cfg.Wire != "anthropic" {
		t.Errorf("Config.Wire = %q; want the bound entry's %q — an unattended run must dial the wire a session on this entry dials", cfg.Wire, "anthropic")
	}
	if routing.target == nil {
		t.Fatal("no routed target resolved from the reachable grunt entry")
	}
	if routing.target.Wire != "" {
		t.Errorf("routed target Wire = %q; want the grunt entry's own unnamed wire carried as written, never the session entry's", routing.target.Wire)
	}
	if probe := modelsProbe(t, primary); probe.Header.Get("anthropic-version") == "" {
		t.Errorf("the run's own server was beaten under %v; want the entry's own anthropic wire, whose probe carries anthropic-version",
			probe.Header)
	}
	if probe := modelsProbe(t, delegation); probe.Header.Get("anthropic-version") != "" {
		t.Errorf("the Sub-agent server was beaten under %v; want the grunt entry's unnamed wire folded to openai, whose probe carries no anthropic-version",
			probe.Header)
	}
}

// modelsProbe is the first GET /v1/models the scripted upstream logged — the beat's own probe, and
// the one whose headers say which wire the beat was dialled under. A server that never saw one was
// never beaten, which fails the test where the claim was made.
func modelsProbe(t *testing.T, srv *stubllm.Server) stubllm.Probe {
	t.Helper()
	for _, probe := range srv.Probes() {
		if probe.Path == "/v1/models" {
			return probe
		}
	}
	t.Fatalf("the upstream at %s was never asked for its model list; the beat did not reach it", srv.URL)
	return stubllm.Probe{}
}

// A host with no scratch root names no scratch dir at all. The Config carries "" rather than a
// half-formed path, because the dir named here is a path the confinement box then advertises as
// writable: an unnamed one would be fenced writable and not be there when the first tool call
// reached for it. The run is still filed under its record id either way — the id is the Driver's,
// minted whether or not this host has a dir to offer.
func TestFiringConfigNamesNoScratchDirWithoutARoot(t *testing.T) {
	t.Parallel()

	roots := firingRoots(t)
	roots.scratch = ""

	cfg, _, _, err := firingConfig(context.Background(), firingInputs{
		opts:     config.Options{Bypass: true},
		entry:    config.ServerEntry{Endpoint: "http://box.example/v1", ParallelAgents: 1},
		apiKey:   "sk-test",
		roots:    roots,
		confiner: fenceableHost,
		mode:     domain.ModePlan,
		beat:     firingBeat,
		recordID: "2026-08-24T12-00-00-firing",
	})
	if err != nil {
		t.Fatalf("firingConfig: %v", err)
	}

	if cfg.ScratchDir != "" {
		t.Errorf("Config.ScratchDir = %q on a host with no scratch root, want \"\" — an unnamed path must never be fenced writable", cfg.ScratchDir)
	}
}

// stubBeat is an observation a test dictates, plus the record of whether it was taken at all. It
// is handed to the composer as firingInputs.beat — the Driver's hand-over of the run's OWN
// observation, the one seam the composition still has — where a row needs a Beat no scripted server
// can produce (the zero Beat, a dictated failure sentence) or has nothing to say about the server at
// all. The Sub-agent server has no such seam: the composer beats it for real (observeServer), so a
// test with an opinion about that box scripts it (internal/stubllm). The `called` half carries the
// claim that the primary beat is taken on EVERY Firing, whatever the entry pins.
type stubBeat struct {
	called    bool
	calls     int
	endpoints []string
	beat      heartbeat.Beat
}

func (s *stubBeat) discover(_ context.Context, endpoint, _, _ string, _ apiprovider.Wire) heartbeat.Beat {
	s.called = true
	s.calls++
	s.endpoints = append(s.endpoints, endpoint)
	return s.beat
}

// Where an unattended run's delegations go (ADR 0045), resolved by the composer off the same
// `sub-agents-server:` key a session resolves. Every failure is a NOTICE with the target left nil —
// a Firing runs while nobody is watching, so refusing to start over a grunt box that is merely down
// would turn a scheduled run into a silent gap in the record (ADR 0042's visible degrade).
//
// The Sub-agent server is a scripted upstream per row — the composer beats it for real — so what
// each row dictates is what that server advertises, or that nothing listens at its address.
func TestFiringConfigResolvesItsSubAgentSeat(t *testing.T) {
	for _, tc := range []struct {
		name  string
		named string
		// props is what the grunt server's /props reports; nil is a server without the probe.
		props *stubllm.Props
		// offline closes the grunt server before the composition, so its beat is a refused dial.
		offline    bool
		wantBeat   bool
		wantTarget bool
		wantSeat   bool
		wantSlots  int
		wantNotice string
	}{
		{
			name: "no key names no seat and asks nothing",
		},
		{
			name:       "a name the list does not carry degrades and says which",
			named:      "typo",
			wantNotice: `sub-agents: no servers entry named "typo" — delegations run on the session server (configured: grunt)`,
		},
		{
			name:       "a reachable entry is routed to",
			named:      "grunt",
			props:      &stubllm.Props{TotalSlots: 5, NCtx: 4096},
			wantBeat:   true,
			wantTarget: true,
			wantSeat:   true,
			wantSlots:  5,
			wantNotice: "sub-agents: routing to grunt (grunt-model)",
		},
		{
			name:       "an unreachable entry keeps its seat and routes nothing",
			named:      "grunt",
			offline:    true,
			wantBeat:   true,
			wantSeat:   true,
			wantNotice: "sub-agents: grunt unavailable — delegations run on the session server",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gruntServer := stubllm.New(t, stubllm.Script{Discovery: stubllm.Discovery{
				Models: []stubllm.DiscoveredModel{{ID: "grunt-model"}},
				Props:  tc.props,
			}})
			if tc.offline {
				gruntServer.Close()
			}
			grunt := config.ServerEntry{
				Name:        "grunt",
				Endpoint:    gruntServer.URL,
				Description: "the cheap box",
				Model:       "grunt-model",
				APIKey:      "sk-grunt",
			}

			_, routing, notices, err := firingConfig(context.Background(), firingInputs{
				opts: config.Options{
					Bypass:          true,
					Servers:         []config.ServerEntry{grunt},
					SubAgentsServer: tc.named,
				},
				entry:    config.ServerEntry{Name: "box", Endpoint: "http://box.example/v1", ParallelAgents: 1, EffortDialect: "reasoning"},
				apiKey:   "sk-test",
				roots:    firingRoots(t),
				confiner: fenceableHost,
				mode:     domain.ModePlan,
				beat:     firingBeat,
				recordID: "2026-09-02T09-00-00-firing",
			})
			if err != nil {
				t.Fatalf("firingConfig: %v; routing must degrade with a notice, never refuse the run", err)
			}

			// A closed server logs nothing, so the refused dial's beat is proven by the notice the
			// row wants rather than by a probe the server could not record.
			if got := len(gruntServer.Probes()) > 0; !tc.offline && got != tc.wantBeat {
				t.Errorf("the Sub-agent beat fired = %v, want %v", got, tc.wantBeat)
			}
			if got := routing.target != nil; got != tc.wantTarget {
				t.Fatalf("routing.target non-nil = %v, want %v", got, tc.wantTarget)
			}
			if got := routing.seat != nil; got != tc.wantSeat {
				t.Fatalf("routing.seat non-nil = %v, want %v", got, tc.wantSeat)
			}
			// A run that names no Sub-agent server has nothing to say ABOUT THE SEAT. The slice
			// itself is shared — an unpinned Firing also carries notice.WindowUnknown — so what is
			// asserted is the absence of a `sub-agents:` line, not an empty composition.
			if tc.wantNotice == "" {
				if slices.ContainsFunc(notices, func(n string) bool {
					return strings.HasPrefix(n, "sub-agents:")
				}) {
					t.Errorf("notices = %q; a run that names no Sub-agent server says nothing about a seat", notices)
				}
			} else if !slices.Contains(notices, tc.wantNotice) {
				t.Errorf("notices = %q; want %q among them", notices, tc.wantNotice)
			}

			if tc.wantSeat {
				want := apogee.DelegationSeat{Name: "grunt", Description: "the cheap box", Model: "grunt-model"}
				if *routing.seat != want {
					t.Errorf("routing.seat = %+v; want the entry's own words %+v", *routing.seat, want)
				}
			}
			if !tc.wantTarget {
				return
			}
			if routing.target.Model != "grunt-model" || routing.target.Endpoint != grunt.Endpoint {
				t.Errorf("routing.target dials %q at %q; want the named entry's own",
					routing.target.Model, routing.target.Endpoint)
			}
			if routing.target.APIKey != "sk-grunt" {
				t.Errorf("routing.target carries %q; want the NAMED entry's own key source, not the run's",
					routing.target.APIKey)
			}
			if routing.target.ParallelAgents != tc.wantSlots {
				t.Errorf("routing.target.ParallelAgents = %d; want the %d the grunt server's /props reported",
					routing.target.ParallelAgents, tc.wantSlots)
			}
		})
	}
}

// What the composition observed of the run's OWN server rides out on the routing, which is the only
// way an unattended Driver can learn it without spending a second round trip: the Firing takes one
// beat, and the Driver that must refuse a run rather than send a prompt into a dead endpoint reads
// the verdict off the value it was handed anyway.
//
// The failure text travels whole rather than being reduced to the flag, because a Driver's refusal
// has to SAY why — "the server is unreachable" with nothing after it is the report a human cannot
// act on. Reachable is `Failure == ""` and nothing else, so an answered-but-unusable server (a 401,
// a 404) reads false here while Beat.Answered still says a box replied.
func TestFiringConfigCarriesItsPrimaryObservation(t *testing.T) {
	for _, tc := range []struct {
		name          string
		beat          heartbeat.Beat
		wantReachable bool
	}{
		{
			name:          "a server that answered its model list",
			beat:          heartbeat.Beat{Reachable: true, Answered: true, TotalSlots: 3},
			wantReachable: true,
		},
		{
			name: "a server nothing is listening on",
			beat: heartbeat.Beat{Failure: "apogee: model discovery: dial tcp: connection refused"},
		},
		{
			name: "a server that answered, unusably",
			beat: heartbeat.Beat{Failure: "apogee: model discovery: upstream HTTP 401", Answered: true},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			beats := &stubBeat{beat: tc.beat}
			_, routing, _, err := firingConfig(context.Background(), firingInputs{
				opts:     config.Options{Bypass: true},
				entry:    config.ServerEntry{Name: "box", Endpoint: "http://box.example/v1"},
				apiKey:   "sk-test",
				roots:    firingRoots(t),
				confiner: fenceableHost,
				mode:     domain.ModePlan,
				beat:     beats.discover,
				recordID: "2026-09-04T09-00-00-firing",
			})
			if err != nil {
				t.Fatalf("firingConfig: %v", err)
			}

			if routing.Reachable != tc.wantReachable {
				t.Errorf("routing.Reachable = %v, want %v — it is Beat.Failure == \"\" and nothing else",
					routing.Reachable, tc.wantReachable)
			}
			if routing.Beat.Failure != tc.beat.Failure {
				t.Errorf("routing.Beat.Failure = %q, want the observation's own %q; a Driver refusing a "+
					"Firing has to say why, and the sentence is the only thing that can",
					routing.Beat.Failure, tc.beat.Failure)
			}
			if routing.Beat.Answered != tc.beat.Answered {
				t.Errorf("routing.Beat.Answered = %v, want %v — the weaker liveness question travels "+
					"whole, so a Driver can tell a declining box from an absent one",
					routing.Beat.Answered, tc.beat.Answered)
			}
		})
	}
}

// The "not advertised" line a session gets at its rebind seam, reaching the Drivers nobody is
// watching: an unattended run binds the configured id verbatim exactly as a session does, so the
// human reading the stderr of a headless run or the daemon's log has to be told the same thing —
// the server never listed this model, and here is what that cost.
//
// The window clause is the pin or nothing, and that is the load-bearing half. The composition hands
// rebindSpecFor an observed window of 0 on purpose, so an unpinned Firing binds no window and leaves
// the Budget inactive (the honest degrade); the observed number the beat carries reaches the
// SENTENCE alone. A change that fed it to the rebind instead would make a run on a `--parallel 8`
// box bind the per-slot window and start pruning a prompt it sends whole today, which is why the
// bound window is asserted here beside the notice.
func TestFiringConfigSaysWhenTheModelIsNotAdvertised(t *testing.T) {
	for _, tc := range []struct {
		name         string
		beat         heartbeat.Beat
		pinnedWindow int
		wantContains []string
		wantBound    int
		// wantWindowUnknown says this composition owes the run notice.WindowUnknown's BARE
		// sentence — the else branch, reached when no hint carries the clause of its own.
		wantWindowUnknown bool
	}{
		{
			name: "an unadvertised model with no pin has no window to report",
			beat: heartbeat.Beat{
				Reachable: true, Answered: true,
				ActiveModel: "my-alias", ContextWindow: 131072,
				Resolution: apiprovider.HintTrusted,
			},
			wantContains: []string{"my-alias", "not advertised", "context window unknown", "Budget"},
		},
		{
			name: "a variant slug is not credited to its base entry either",
			beat: heartbeat.Beat{
				Reachable: true, Answered: true,
				ActiveModel: "vendor/model:variant", ContextWindow: 131072,
				Resolution: apiprovider.HintBaseSlug,
			},
			wantContains: []string{"vendor/model:variant", "not advertised", "context window unknown"},
		},
		{
			name: "an entry that pins a window states the pin",
			beat: heartbeat.Beat{
				Reachable: true, Answered: true,
				ActiveModel: "my-alias", ContextWindow: 131072,
				Resolution: apiprovider.HintTrusted,
			},
			pinnedWindow: 32768,
			wantContains: []string{"my-alias", "not advertised", "context window: 32k"},
			wantBound:    32768,
		},
		{
			// Unremarkable about the MODEL, and that is exactly the run this item is for: nothing is
			// unadvertised, so no hint is composed, and without the else the fact that the Budget and
			// auto-compaction are inactive would never be said at all.
			name: "an advertised model is unremarkable but its unknown window is not",
			beat: heartbeat.Beat{
				Reachable: true, Answered: true,
				ActiveModel: "my-alias", ContextWindow: 131072,
				Resolution: apiprovider.HintExact,
			},
			wantWindowUnknown: true,
		},
		{
			name: "a pin leaves nothing to say about the window",
			beat: heartbeat.Beat{
				Reachable: true, Answered: true,
				ActiveModel: "my-alias", ContextWindow: 131072,
				Resolution: apiprovider.HintExact,
			},
			pinnedWindow: 32768,
			wantBound:    32768,
		},
		{
			// The offline order: both unattended Drivers emit these notices BEFORE their offline
			// gate, so a line said here would reach the user ahead of "cannot send — server
			// offline" — and in the daemon it would burn the once-per-process latch on a Firing
			// that never ran.
			name: "a beat that never answered says nothing about the window",
			beat: heartbeat.Beat{ActiveModel: "my-alias", Failure: "dial tcp: refused"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			beats := &stubBeat{beat: tc.beat}
			cfg, _, notices, err := firingConfig(context.Background(), firingInputs{
				opts: config.Options{Bypass: true},
				entry: config.ServerEntry{
					Name:          "box",
					Endpoint:      "http://box.example/v1",
					Model:         tc.beat.ActiveModel,
					ContextWindow: config.TokenCount(tc.pinnedWindow),
				},
				apiKey:   "sk-test",
				roots:    firingRoots(t),
				confiner: fenceableHost,
				mode:     domain.ModePlan,
				beat:     beats.discover,
				recordID: "2026-09-04T10-00-00-firing",
			})
			if err != nil {
				t.Fatalf("firingConfig: %v", err)
			}

			hint := ""
			for _, n := range notices {
				if strings.Contains(n, "not advertised") {
					hint = n
				}
			}

			// An unknown window is announced ONCE however it is announced: inside the hint when the
			// model is unadvertised, as notice.WindowUnknown's bare sentence when it is not.
			// Counting both spellings is what pins the else — a plain append would say it twice on
			// the unadvertised-and-unpinned run, which is the commonest unattended run there is.
			said, bare := 0, 0
			for _, n := range notices {
				if strings.Contains(n, "context window unknown") {
					said++
				}
				if n == notice.WindowUnknown {
					bare++
				}
			}
			if said > 1 {
				t.Errorf("notices = %q; the unknown window was announced %d times — the hint's own clause "+
					"and notice.WindowUnknown are alternatives, never both", notices, said)
			}
			switch {
			case tc.wantWindowUnknown && bare != 1:
				t.Errorf("notices = %q; want notice.WindowUnknown exactly once — an unattended run derives "+
					"its Budget from configuration alone, so this sentence is the only thing that can "+
					"tell its user the Budget and auto-compaction are inactive", notices)
			case !tc.wantWindowUnknown && bare != 0:
				t.Errorf("notices = %q; want no bare unknown-window line here", notices)
			}
			if cfg.Context.MaxContextTokens != tc.wantBound {
				t.Errorf("Config.Context.MaxContextTokens = %d, want %d; the observed window reaches the "+
					"NOTICE alone — binding it would prune a prompt an unpinned Firing sends whole",
					cfg.Context.MaxContextTokens, tc.wantBound)
			}

			if len(tc.wantContains) == 0 {
				if hint != "" {
					t.Fatalf("notices = %q; a model the server advertises is ordinary and says nothing "+
						"about the model", notices)
				}
				return
			}
			for _, want := range tc.wantContains {
				if !strings.Contains(hint, want) {
					t.Errorf("hint notice = %q; want it to name %q — the unattended Drivers read this "+
						"slice and have no other channel for it", hint, want)
				}
			}
		})
	}
}

// The two beats observe two different BOXES and must never be collapsed into one. A Firing's own
// beat asks the server it runs on; resolveFiringRouting asks the `sub-agents-server:` entry, which
// has its own endpoint, model and key. Sharing the primary's beat would have resolveDelegationTarget
// resolve a target's window, width and bound model against the wrong machine — routing every
// delegation to a grunt server nobody observed instead of degrading to the run's own Upstream with a
// notice. The primary's observation is the Driver's hand-over here (firingInputs.beat) and the
// Sub-agent server a scripted upstream, so the two boxes answer with different widths and the
// assertion reads which one each half of the composition carried.
func TestFiringConfigBeatsTheSubAgentServerOnItsOwnEndpoint(t *testing.T) {
	delegation := stubllm.New(t, stubllm.Script{
		Model:     "grunt-model",
		Discovery: stubllm.Discovery{Props: &stubllm.Props{TotalSlots: 2}},
	})
	grunt := config.ServerEntry{
		Name:     "grunt",
		Endpoint: delegation.URL,
		Model:    "grunt-model",
		APIKey:   "sk-grunt",
	}
	primary := &stubBeat{beat: heartbeat.Beat{Reachable: true, Answered: true, TotalSlots: 1}}

	_, routing, _, err := firingConfig(context.Background(), firingInputs{
		opts: config.Options{
			Bypass:          true,
			Servers:         []config.ServerEntry{grunt},
			SubAgentsServer: "grunt",
		},
		entry:    config.ServerEntry{Name: "box", Endpoint: "http://box.example/v1"},
		apiKey:   "sk-test",
		roots:    firingRoots(t),
		confiner: fenceableHost,
		mode:     domain.ModePlan,
		beat:     primary.discover,
		recordID: "2026-09-04T10-00-00-firing",
	})
	if err != nil {
		t.Fatalf("firingConfig: %v", err)
	}

	if got := primary.endpoints; !slices.Equal(got, []string{"http://box.example/v1"}) {
		t.Errorf("the primary beat saw %v; want exactly the run's own endpoint", got)
	}
	if routing.target == nil {
		t.Fatal("the composer resolved no target; the fixture no longer sets up a routed run")
	}
	// The target was resolved against the grunt server's OWN observation: its width is the two
	// slots that box advertises, which the primary's dictated beat never reported.
	modelsProbe(t, delegation)
	if routing.target.ParallelAgents != 2 {
		t.Errorf("routing.target.ParallelAgents = %d; want the 2 the Sub-agent server's /props reported — a shared "+
			"beat would resolve the target against the wrong box", routing.target.ParallelAgents)
	}
	// And the observation on the routing is the PRIMARY's, never the one the target was resolved from.
	if routing.Beat.TotalSlots != 1 {
		t.Errorf("routing.Beat reports %d slots; want the primary's 1 — the Sub-agent server's beat "+
			"answered 2 and must not be what a Driver gates the run on", routing.Beat.TotalSlots)
	}
}

// The Reaction Runner a Driver built for ONE Firing reaches the run, and the variables its webhook
// headers are read from reach the credential scrub. Both halves matter: a Runner the composer
// dropped would leave a configured `reactions:` list silently dead at every unattended root, and a
// header token left out of SecretEnvVars would be readable by the very model this run is about to
// hand a `terminal` tool to.
func TestFiringConfigInstallsTheHookRunner(t *testing.T) {
	t.Parallel()

	roots := firingRoots(t)
	list := []domain.Reaction{{
		ID:     "notify",
		Origin: domain.OriginUser,
		Class:  domain.ClassObserve,
		On:     []reactions.Event{reactions.ExchangeFinished},
		Handler: domain.WebhookHandler{
			URL:        "https://hooks.example/fire",
			HeadersEnv: map[string]string{"Authorization": "NOTIFY_TOKEN"},
		},
		Timeout: time.Second,
	}}
	runner, err := firingHooks(list, roots.workspace, &reactions.ScheduleRef{ID: "sch-1", Name: "Nightly"}, nil)
	if err != nil {
		t.Fatalf("firingHooks: %v", err)
	}
	t.Cleanup(func() { _ = runner.Close(context.Background()) })

	cfg, _, _, err := firingConfig(context.Background(), firingInputs{
		opts: config.Options{
			Bypass:    true,
			Reactions: list,
			APIKeyEnv: "STARTUP_KEY",
		},
		entry:    config.ServerEntry{Endpoint: "http://box.example/v1"},
		apiKey:   "sk-test",
		roots:    roots,
		confiner: fenceableHost,
		mode:     domain.ModePlan,
		beat:     firingBeat,
		recordID: "2026-09-06T09-00-00-firing",
		hooks:    runner,
	})
	if err != nil {
		t.Fatalf("firingConfig: %v", err)
	}

	if cfg.Events != domain.EventSink(runner) {
		t.Errorf("cfg.Events = %v, want the Runner the Driver built — a Firing fires the "+
			"`reactions:` list through the sink it was handed", cfg.Events)
	}
	for _, want := range []string{"STARTUP_KEY", "NOTIFY_TOKEN"} {
		if !slices.Contains(cfg.SecretEnvVars, want) {
			t.Errorf("SecretEnvVars = %v, want it to carry %q — both the key sources and the "+
				"Reaction header sources are scrubbed out of a subprocess the model chose",
				cfg.SecretEnvVars, want)
		}
	}
}

// The in-loop twin of that Runner's report seam: where a SYNC-lane reaction's trouble is said out
// loud (domain.Config.Report). It is the SAME function the Driver hands its Runner, so one
// `reactions:` file's failures read the same way whichever lane they came from — and a Driver that
// narrates nowhere leaves the field nil, which drops the line exactly as a bare Config does.
func TestFiringConfigWiresTheSyncLanesReporter(t *testing.T) {
	t.Parallel()

	roots := firingRoots(t)
	var said []string
	in := firingInputs{
		opts:     config.Options{},
		entry:    config.ServerEntry{Endpoint: "http://box.example/v1"},
		apiKey:   "sk-test",
		roots:    roots,
		confiner: fenceableHost,
		mode:     domain.ModePlan,
		beat:     firingBeat,
		recordID: "2026-09-09T09-00-00-firing",
		report:   func(msg string) { said = append(said, msg) },
	}

	cfg, _, _, err := firingConfig(context.Background(), in)
	if err != nil {
		t.Fatalf("firingConfig: %v", err)
	}
	if cfg.Report == nil {
		t.Fatal("cfg.Report is nil; a gate that could not be spawned would fail silently at an " +
			"unattended root")
	}
	cfg.Report(`reaction "warden": gate: timed out`)
	if len(said) != 1 || said[0] != `reaction "warden": gate: timed out` {
		t.Errorf("the Driver was told %v, want the one line the engine wrote", said)
	}

	in.report = nil
	bare, _, _, err := firingConfig(context.Background(), in)
	if err != nil {
		t.Fatalf("firingConfig without a reporter: %v", err)
	}
	if bare.Report != nil {
		t.Error("cfg.Report is set on a Driver that narrates nowhere; the field must stay nil")
	}
}

// The sync lane takes exactly ONE route into a run: the Driver splits its resolved `reactions:`
// list, hands the observe half to a Reaction Runner and the sync half to run.Spec.Sync. A
// composition root that ALSO wrote domain.Config.Reactions — the engine's own construction-time
// set — would arm every user entry twice, so the second route is closed by rule and this is the
// rule. It reads the source rather than a Config value because the claim is about every wiring
// site at once, including ones added later.
func TestNoWiringSiteWritesConfigReactions(t *testing.T) {
	t.Parallel()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read the package dir: %v", err)
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		// Parsed rather than grepped so a comment that NAMES the rule cannot trip it: what the
		// rule forbids is a struct field written in a literal, which is an AST node.
		parsed, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(parsed, func(n ast.Node) bool {
			kv, ok := n.(*ast.KeyValueExpr)
			if !ok {
				return true
			}
			if key, ok := kv.Key.(*ast.Ident); ok && key.Name == "Reactions" {
				t.Errorf("%s writes a Reactions field in a composite literal; the user's list "+
					"reaches a run through the observe Runner and run.Spec.Sync, never through "+
					"Config.Reactions", fset.Position(kv.Pos()))
			}
			return true
		})
	}
}

// assertReadRootsCompose pins the shape of a composed read-roots func: the provider's own
// resolved mounts are its leading prefix, in the provider's order, and the toolchain roots the host
// probed (toolchain_roots.go) are exactly what follows — nothing else on the line, nothing
// reordered. The probe is waited for first so the func is read once it has settled: on a host with
// `go` on PATH the tail is GOROOT and the module cache, on one without it is empty, and the pin
// holds on both.
func assertReadRootsCompose(t *testing.T, roots func() []string, skillRoots []string) {
	t.Helper()

	hostToolchain.wait()
	got := roots()
	if len(got) < len(skillRoots) || !slices.Equal(got[:len(skillRoots)], skillRoots) {
		t.Fatalf("ExtraReadRoots() = %v; want the provider's own resolved mounts %v as its leading prefix",
			got, skillRoots)
	}
	if tail, want := got[len(skillRoots):], hostToolchain.roots(); !slices.Equal(tail, want) {
		t.Errorf("ExtraReadRoots() = %v; want the probed toolchain roots %v after the skill roots, got %v",
			got, want, tail)
	}
}

// raiseInputs is the one shape every raise test below starts from: a bound entry with a key already
// resolved, throwaway roots, a fenceable host and a beat the test dictates — nothing that would
// dial, read a keychain or reach the real runner. The stubRunner rides on the inputs themselves
// (firingInputs.runner), so no package seam is touched.
func raiseInputs(t *testing.T, stub *stubRunner, beat heartbeat.Beat) firingInputs {
	t.Helper()
	return firingInputs{
		opts:     config.Options{Bypass: true},
		entry:    config.ServerEntry{Name: "box", Endpoint: "http://box.example/v1", ParallelAgents: 1},
		apiKey:   "sk-test",
		roots:    firingRoots(t),
		confiner: fenceableHost,
		mode:     domain.ModePlan,
		runner:   stub.once,
		beat:     (&stubBeat{beat: beat}).discover,
	}
}

// The liveness gate is raise's own: a server whose one beat answered NOTHING refuses the Firing
// before a prompt is spent on it, with the sentence every Driver reads (notice.ServerOffline) and
// the composition's notices still handed back — a Driver prints them before it reads the error.
// A server that answered ANYTHING runs, because a 429 or a 404 is an answer this host has no
// standing to judge (internal/heartbeat's Answered).
func TestRaiseRefusesWhenOffline(t *testing.T) {
	tests := []struct {
		name    string
		beat    heartbeat.Beat
		wantErr string
		wantRun bool
	}{
		{
			name:    "nothing answered, and the beat says why",
			beat:    heartbeat.Beat{Failure: "connection refused"},
			wantErr: notice.ServerOffline("http://box.example/v1", "connection refused"),
		},
		{
			name:    "nothing answered and nothing to say about it",
			beat:    heartbeat.Beat{},
			wantErr: notice.ServerOffline("http://box.example/v1", ""),
		},
		{
			name:    "answered but throttled runs",
			beat:    heartbeat.Beat{Answered: true, Throttled: true, Failure: "429"},
			wantRun: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stub := &stubRunner{res: run.Result{Turns: 1, FinalText: "the answer"}}
			in := raiseInputs(t, stub, tc.beat)
			// A `sub-agents-server:` no entry answers to: the one composition notice that does not
			// depend on the beat, so the refusal path has something to hand back.
			in.opts.SubAgentsServer = "ghost"

			res, notices, err := raise(context.Background(), in, "a prompt", nil, nil, nil, nil)

			if stub.called != tc.wantRun {
				t.Fatalf("runner called = %v, want %v", stub.called, tc.wantRun)
			}
			if tc.wantRun {
				if err != nil {
					t.Fatalf("raise: %v", err)
				}
				if res.FinalText != "the answer" {
					t.Errorf("Result = %+v; want the runner's own, passed through untouched", res)
				}
				return
			}
			if err == nil || err.Error() != tc.wantErr {
				t.Errorf("err = %v; want %q verbatim", err, tc.wantErr)
			}
			if len(notices) != 1 || !strings.Contains(notices[0], "ghost") {
				t.Errorf("notices = %q on the refusal; want the composition's own missing-name notice "+
					"handed back beside the error — a Driver prints them before it reads it", notices)
			}
		})
	}
}

// The by-construction guarantee: raise mints the id, hands it to onID BEFORE anything is composed,
// and the very same id names both the record the runner is asked to file (run.Spec.RecordID) and
// the scratch dir the composer fenced writable (Config.ScratchDir). Two Drivers minting two ids
// was exactly the drift this act exists to make impossible.
func TestRaiseMintsOneIDForRecordAndScratch(t *testing.T) {
	stub := &stubRunner{res: run.Result{Turns: 1}}
	in := raiseInputs(t, stub, heartbeat.Beat{Reachable: true, Answered: true})
	var seen []string

	_, _, err := raise(context.Background(), in, "a prompt", nil, nil,
		func(recordID string) { seen = append(seen, recordID) }, nil)
	if err != nil {
		t.Fatalf("raise: %v", err)
	}

	if len(seen) != 1 || seen[0] == "" {
		t.Fatalf("onID saw %q; want exactly one non-empty id", seen)
	}
	if stub.spec.RecordID != seen[0] {
		t.Errorf("run.Spec.RecordID = %q; want the id onID saw, %q", stub.spec.RecordID, seen[0])
	}
	if want := filepath.Join(in.roots.scratch, seen[0]); stub.spec.Config.ScratchDir != want {
		t.Errorf("Config.ScratchDir = %q; want %q — the scratch dir is named after the record", stub.spec.Config.ScratchDir, want)
	}
	if _, statErr := os.Stat(stub.spec.Config.ScratchDir); statErr != nil {
		t.Errorf("the scratch dir was not created: %v", statErr)
	}
}

// The id and the record's timestamps come off ONE clock: raise mints the record id from
// firingInputs.now and hands the same func to the runner as run.Spec.Now, so a pinned clock pins the
// id's timestamp prefix (session.NewID's layout, cut at the random suffix) and CreatedAt alike, and
// two Firings can never file ids whose order disagrees with their CreatedAt order. nil is the
// production path — headless, and a Driver whose Scheduler clock is nil — and still mints.
func TestRaiseMintsTheIDFromTheFiringsClock(t *testing.T) {
	t.Run("a fixed clock pins the id prefix and the runner's Now", func(t *testing.T) {
		fixed := time.Date(2026, 9, 20, 14, 30, 5, 0, time.UTC)
		stub := &stubRunner{res: run.Result{Turns: 1}}
		in := raiseInputs(t, stub, heartbeat.Beat{Reachable: true, Answered: true})
		in.now = func() time.Time { return fixed }

		_, _, err := raise(context.Background(), in, "a prompt", nil, nil, nil, nil)
		if err != nil {
			t.Fatalf("raise: %v", err)
		}

		wantPrefix, _, _ := strings.Cut(session.NewID(fixed), "-")
		if gotPrefix, _, _ := strings.Cut(stub.spec.RecordID, "-"); gotPrefix != wantPrefix {
			t.Errorf("RecordID prefix = %q; want %q — the id was minted off the wall clock, not the Firing's", gotPrefix, wantPrefix)
		}
		if stub.spec.Now == nil {
			t.Fatal("run.Spec.Now is nil; the runner would stamp CreatedAt off the wall clock")
		}
		if got := stub.spec.Now(); !got.Equal(fixed) {
			t.Errorf("run.Spec.Now() = %v; want the Firing's instant %v", got, fixed)
		}
	})

	t.Run("a nil clock is the wall clock and still mints", func(t *testing.T) {
		if got := clockNow(nil); got != nil {
			t.Error("clockNow(nil) is non-nil; a nil scheduler clock must leave firingInputs.now at its time.Now default")
		}
		if got := clockNow(newFakeDaemonClock()); got == nil {
			t.Error("clockNow(clock) is nil; a real clock must be handed through")
		}
		stub := &stubRunner{res: run.Result{Turns: 1}}
		in := raiseInputs(t, stub, heartbeat.Beat{Reachable: true, Answered: true})
		in.now = nil

		_, _, err := raise(context.Background(), in, "a prompt", nil, nil, nil, nil)
		if err != nil {
			t.Fatalf("raise: %v", err)
		}

		if stub.spec.RecordID == "" {
			t.Error("a nil clock minted no id")
		}
		if stub.spec.Now == nil {
			t.Error("run.Spec.Now is nil; the runner and the id would read two clocks")
		}
	})
}

// onID fires before the composition and before the gate, so a Driver that stamps the id on a
// stream stamps it on a refusal's closing frame too (ADR 0075 decision 5) — and a Schedule handed
// in reaches the runner's Spec, so the record it files is the Schedule's.
func TestRaiseCallsOnIDBeforeRefusingAndLatchesTheSchedule(t *testing.T) {
	t.Run("an offline refusal still saw the id", func(t *testing.T) {
		stub := &stubRunner{}
		in := raiseInputs(t, stub, heartbeat.Beat{Failure: "connection refused"})
		var seen string

		_, _, err := raise(context.Background(), in, "a prompt", nil, nil,
			func(recordID string) { seen = recordID }, nil)

		if err == nil {
			t.Fatal("a server that answered nothing was allowed to run")
		}
		if seen == "" {
			t.Error("onID never saw the id; the refusal's closing frame would carry null")
		}
	})

	t.Run("the Schedule reaches the Spec", func(t *testing.T) {
		stub := &stubRunner{res: run.Result{Turns: 1}}
		in := raiseInputs(t, stub, heartbeat.Beat{Reachable: true, Answered: true})

		_, _, err := raise(context.Background(), in, "a prompt",
			&reactions.ScheduleRef{ID: "sch-1", Name: "Nightly"}, nil, nil, nil)

		if err != nil {
			t.Fatalf("raise: %v", err)
		}
		if stub.spec.ScheduleID != "sch-1" || stub.spec.ScheduleName != "Nightly" {
			t.Errorf("Spec schedule = %q/%q; want sch-1/Nightly", stub.spec.ScheduleID, stub.spec.ScheduleName)
		}
		if stub.spec.Prompt != "a prompt" {
			t.Errorf("Spec.Prompt = %q; want the prompt raise was handed", stub.spec.Prompt)
		}
	})
}

// narrate is handed the sink the Config carries and decides the one the run is driven with — and
// it is called only once both gates have passed, so a Driver that announces the run from inside it
// never announces a run that was refused.
func TestRaiseDecoratesTheSinkAfterTheGate(t *testing.T) {
	t.Run("a raised run is driven with the decorated sink", func(t *testing.T) {
		stub := &stubRunner{res: run.Result{Turns: 1}}
		in := raiseInputs(t, stub, heartbeat.Beat{Reachable: true, Answered: true})
		sink := &recordingSink{}
		var sawID string

		_, _, err := raise(context.Background(), in, "a prompt", nil, nil, nil,
			func(recordID string, cfg apogee.Config, inner domain.EventSink) domain.EventSink {
				sawID = recordID
				if _, ok := inner.(*reactions.Runner); !ok {
					t.Errorf("narrate was handed %T; want the Reaction Runner raise built — the "+
						"decoration WRAPS the Reactions, it never replaces them", inner)
				}
				if cfg.Endpoint != in.entry.Endpoint {
					t.Errorf("narrate was handed a Config bound to %q; want the composed one", cfg.Endpoint)
				}
				return sink
			})

		if err != nil {
			t.Fatalf("raise: %v", err)
		}
		if stub.spec.Config.Events != domain.EventSink(sink) {
			t.Errorf("Config.Events = %v; want the sink narrate returned", stub.spec.Config.Events)
		}
		if sawID != stub.spec.RecordID {
			t.Errorf("narrate saw id %q; want the run's own, %q", sawID, stub.spec.RecordID)
		}
	})

	t.Run("a refused run never reaches narrate", func(t *testing.T) {
		stub := &stubRunner{}
		in := raiseInputs(t, stub, heartbeat.Beat{Failure: "connection refused"})

		_, _, err := raise(context.Background(), in, "a prompt", nil, nil, nil,
			func(string, apogee.Config, domain.EventSink) domain.EventSink {
				t.Error("narrate was called on a refusal; the opening frame would announce a run that never happened")
				return nil
			})

		if err == nil {
			t.Fatal("a server that answered nothing was allowed to run")
		}
	})
}

// A refusal before the run is typed, so a Driver acts on the CLASS: composition refusals carry
// Stage "compose", the gate's carry "offline", Error is the wrapped sentence verbatim — no prefix
// of the type's own, so the exact wording a Driver prints stays the composer's — and
// errors.Unwrap yields the refusal itself. A run that started passes its error through untyped.
func TestRaiseNotStartedIsTyped(t *testing.T) {
	tests := []struct {
		name      string
		shape     func(in *firingInputs)
		beat      heartbeat.Beat
		wantStage string
	}{
		{
			name: "a composition refusal is stageCompose",
			// The cheapest way into a composition refusal: an unknown placeholder in the system
			// prompt, which the per-model half of the Config resolves and nothing earlier reads.
			shape: func(in *firingInputs) {
				in.opts.SystemPrompt = config.SystemPromptSettings{Global: config.PromptSource{Text: "hi {{bogus}}"}}
			},
			beat:      heartbeat.Beat{Reachable: true, Answered: true},
			wantStage: stageCompose,
		},
		{
			name: "a Reaction Runner this root cannot build is stageCompose",
			// The Runner resolves the run's workspace before anything else is composed, and a
			// leading ~ with no home to expand it against is the one way that resolution fails.
			shape: func(in *firingInputs) {
				if runtime.GOOS == "windows" {
					t.Skip("the home lookup reads USERPROFILE on Windows; the ~ shape is not portable there")
				}
				t.Setenv("HOME", "")
				in.roots.workspace = "~/nowhere"
			},
			beat:      heartbeat.Beat{Reachable: true, Answered: true},
			wantStage: stageCompose,
		},
		{
			name:      "the gate's refusal is stageOffline",
			shape:     func(*firingInputs) {},
			beat:      heartbeat.Beat{Failure: "connection refused"},
			wantStage: stageOffline,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stub := &stubRunner{}
			in := raiseInputs(t, stub, tc.beat)
			tc.shape(&in)

			_, _, err := raise(context.Background(), in, "a prompt", nil, nil, nil, nil)

			if stub.called {
				t.Fatal("the runner ran; a refusal spends no prompt")
			}
			var refused errNotStarted
			if !errors.As(err, &refused) {
				t.Fatalf("err = %v (%T); want an errNotStarted", err, err)
			}
			if refused.Stage != tc.wantStage {
				t.Errorf("Stage = %q; want %q", refused.Stage, tc.wantStage)
			}
			if inner := errors.Unwrap(err); inner == nil || inner != refused.Err {
				t.Errorf("errors.Unwrap = %v; want the wrapped refusal %v", inner, refused.Err)
			}
			if err.Error() != refused.Err.Error() {
				t.Errorf("Error() = %q; want the wrapped sentence %q verbatim", err.Error(), refused.Err.Error())
			}
		})
	}

	t.Run("a run that started passes its error through untyped", func(t *testing.T) {
		stub := &stubRunner{res: run.Result{Turns: 1}, err: errors.New("apogee: the firing was cancelled")}
		in := raiseInputs(t, stub, heartbeat.Beat{Reachable: true, Answered: true})

		res, _, err := raise(context.Background(), in, "a prompt", nil, nil, nil, nil)

		var refused errNotStarted
		if errors.As(err, &refused) {
			t.Errorf("a run that started came back typed as not started: %v", err)
		}
		if err == nil || err.Error() != "apogee: the firing was cancelled" || res.Turns != 1 {
			t.Errorf("(res, err) = (%+v, %v); want the runner's own, passed through", res, err)
		}
	})
}

// TestFiringOutcomeCarriesWhatTheFiringWroteAndHowToUndoIt: the Outcome every Driver's Firing
// reports carries what the run CHANGED and the exact revert those changes can still have, under
// the SAME gate the printed offer applies (undoCommand) — so a surface that renders the Outcome
// offers the verb exactly when the daemon's log and the headless stderr do, and never a command
// the report would not name.
func TestFiringOutcomeCarriesWhatTheFiringWroteAndHowToUndoIt(t *testing.T) {
	tests := []struct {
		name     string
		res      run.Result
		wantUndo string
	}{
		{
			name:     "a saved run that wrote offers the verb against its record",
			res:      run.Result{SessionID: "s-7", Turns: 1, Wrote: []string{"/ws/new.go", "/ws/old.go"}},
			wantUndo: "apogee undo s-7",
		},
		{
			name: "an UndoNote withholds the verb — the journal was the in-memory one",
			res: run.Result{
				SessionID: "s-8", Turns: 1, Wrote: []string{"/ws/new.go"},
				UndoNote: "undo journal unavailable — the store would not open",
			},
		},
		{
			name: "an unsaved run has no record to name",
			res:  run.Result{Turns: 1, Wrote: []string{"/ws/new.go"}},
		},
		{
			name: "a run that wrote nothing has nothing to revert",
			res:  run.Result{SessionID: "s-9", Turns: 1},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out := firingOutcome(tc.res)

			if !slices.Equal(out.Wrote, tc.res.Wrote) {
				t.Errorf("Outcome.Wrote = %q; want the run's own %q", out.Wrote, tc.res.Wrote)
			}
			if out.UndoCommand != tc.wantUndo {
				t.Errorf("Outcome.UndoCommand = %q; want %q", out.UndoCommand, tc.wantUndo)
			}
			// The line the unattended Drivers print is this command dressed for a report, so the
			// two can never disagree on whether there is a revert to offer.
			if line := undoVerbLine(tc.res); (line == "") != (out.UndoCommand == "") {
				t.Errorf("undoVerbLine = %q while Outcome.UndoCommand = %q; the gate is one", line, out.UndoCommand)
			}
		})
	}
}
