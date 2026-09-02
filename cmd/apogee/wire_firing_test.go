package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/heartbeat"
	"github.com/airiclenz/apogee/internal/mechanisms"
	"github.com/airiclenz/apogee/internal/run"
	"github.com/airiclenz/apogee/internal/skills"
	"github.com/airiclenz/apogee/internal/stubllm"
)

// firingRoots is one throwaway set of state roots, each a real directory so the scratch dir the
// composer creates under them can be stat'd.
func firingRoots(t *testing.T) stateRoots {
	t.Helper()

	home := t.TempDir()
	return stateRoots{
		config:    home,
		library:   filepath.Join(home, "library"),
		sessions:  filepath.Join(home, "sessions"),
		validated: filepath.Join(home, "validated-sets"),
		probe:     filepath.Join(home, "probe"),
		prompts:   filepath.Join(home, "prompts"),
		schemes:   filepath.Join(home, "schemes"),
		scratch:   t.TempDir(),
		workspace: t.TempDir(),
	}
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
		// Bypass doubles as the Mechanisms floor and, here, as what keeps the Validated-set
		// surface off, so this composition resolves without a probe record to match against.
		Bypass:             true,
		ConfineToWorkspace: true,
		WebSearchEndpoint:  "https://search.example/v1",
		ToolsDisabled:      []string{"run_terminal_cmd"},
		ToolsEnabled:       []string{"web_search"},
		URLAllowHosts:      []string{"allowed.example"},
		URLDenyHosts:       []string{"denied.example"},
		UI:                 config.UISettings{Inspector: true},
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
	dialects := &stubDialect{dialect: "openai"}
	provider := skills.NewProvider(skills.Sources{Home: roots.config, Workspace: roots.workspace})
	probed := false
	manual := mechanisms.KnownIDs()[:1]

	cfg, _, _, err := firingConfig(context.Background(), firingInputs{
		opts:      opts,
		entry:     entry,
		apiKey:    "sk-handed-over",
		roots:     roots,
		manualIDs: manual,
		confiner:  fenceableHost,
		model:     "overlay-model",
		mode:      domain.ModeAuto,
		skills:    provider,
		width: func(context.Context, string, string, string) int {
			probed = true
			return 9
		},
		dialect:  dialects.discover,
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
	if cfg.ConfigDir != roots.config || cfg.LibraryDir != roots.library || cfg.WorkspaceDir != roots.workspace {
		t.Errorf("the state roots did not reach the Config: %q / %q / %q", cfg.ConfigDir, cfg.LibraryDir, cfg.WorkspaceDir)
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
	if !slices.Equal(cfg.ExtraReadRoots(), provider.ReadRoots()) {
		t.Errorf("Config.ExtraReadRoots() = %v; want the provider's own resolved mounts %v", cfg.ExtraReadRoots(), provider.ReadRoots())
	}
	if !slices.Equal(cfg.EnableMechanisms, manual) {
		t.Errorf("Config.EnableMechanisms = %v; want the validated manual list %v", cfg.EnableMechanisms, manual)
	}

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
	if probed {
		t.Error("the width source was consulted behind a pin; ResolveParallelAgents could never have used the answer")
	}

	// And the effort wire shape, which reaches the run the same way the bounds do: a Driver that
	// never rebinds would otherwise send the zero dialect — the historical chat_template_kwargs
	// shape — whatever the bound server actually reads (2026-08-25 audit C-03, ADR 0031 parity).
	// The entry FORCES one here, so it is the answer and the beat is never taken.
	if cfg.EffortDialect != domain.EffortDialectReasoning {
		t.Errorf("Config.EffortDialect = %q; want the entry's forced %q", cfg.EffortDialect, domain.EffortDialectReasoning)
	}
	if dialects.called {
		t.Error("the dialect source was consulted behind a forced effort-dialect:; the round trip could only re-ask a settled question")
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

// The four optional seams, each nil, each taking the documented default: a fresh key resolver asks
// the entry's own source, a fresh catalog is built from the roots, and the width and the effort
// dialect both come from the one-shot discovery probe. Those defaults are what headless and the
// daemon rely on — they have no longer-lived facility to share — so a change of default is a change
// to two Drivers at once.
func TestFiringConfigDefaultsItsSeams(t *testing.T) {
	roots := firingRoots(t)

	slots := &stubSlots{slots: 4}
	dialects := &stubDialect{dialect: "openai"}
	prevSlots, prevDialect := discoverSlots, discoverDialect
	discoverSlots, discoverDialect = slots.discover, dialects.discover
	t.Cleanup(func() { discoverSlots, discoverDialect = prevSlots, prevDialect })

	entry := config.ServerEntry{Name: "box", Endpoint: "http://box.example/v1", APIKey: "sk-from-the-entry", Model: "entry-model"}
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
	if !slots.called {
		t.Error("the discovery probe never ran; an unpinned entry has no other way to learn how wide it may fan out")
	}
	if cfg.ParallelAgents != 4 {
		t.Errorf("Config.ParallelAgents = %d; want the 4 the probe reported", cfg.ParallelAgents)
	}
	if !dialects.called {
		t.Error("the dialect probe never ran; an entry that forces no effort-dialect: has no other way to learn the server's shape")
	}
	if cfg.EffortDialect != domain.EffortDialectOpenAI {
		t.Errorf("Config.EffortDialect = %q; want the %q the probe observed — an unattended run must reach the wire a session reaches",
			cfg.EffortDialect, domain.EffortDialectOpenAI)
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
	grunt := config.ServerEntry{
		Name:        "grunt",
		Endpoint:    "http://grunt.example/v1",
		Description: "the cheap box",
		Model:       "grunt-model",
		APIKey:      "sk-grunt",
	}
	beats := &stubBeat{beat: heartbeat.Beat{Reachable: true, TotalSlots: 2}}
	prev := discoverDelegationBeat
	discoverDelegationBeat = beats.discover
	t.Cleanup(func() { discoverDelegationBeat = prev })

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
		recordID: "2026-08-24T12-00-00-firing",
	})
	if err != nil {
		t.Fatalf("firingConfig: %v", err)
	}

	if cfg.ScratchDir != "" {
		t.Errorf("Config.ScratchDir = %q on a host with no scratch root, want \"\" — an unnamed path must never be fenced writable", cfg.ScratchDir)
	}
}

// stubBeat is the Sub-agent server's observation a test dictates, plus the record of whether it was
// taken at all — a run that names no `sub-agents-server:` must ask nothing, since a third round trip
// per Firing for a question nobody posed is exactly what the gate exists to avoid.
type stubBeat struct {
	called bool
	beat   heartbeat.Beat
}

func (s *stubBeat) discover(context.Context, string, string, string) heartbeat.Beat {
	s.called = true
	return s.beat
}

// Where an unattended run's delegations go (ADR 0045), resolved by the composer off the same
// `sub-agents-server:` key a session resolves. Every failure is a NOTICE with the target left nil —
// a Firing runs while nobody is watching, so refusing to start over a grunt box that is merely down
// would turn a scheduled run into a silent gap in the record (ADR 0042's visible degrade).
func TestFiringConfigResolvesItsSubAgentSeat(t *testing.T) {
	known := string(mechanisms.KnownIDs()[0])
	grunt := config.ServerEntry{
		Name:        "grunt",
		Endpoint:    "http://grunt.example/v1",
		Description: "the cheap box",
		Model:       "grunt-model",
		APIKey:      "sk-grunt",
	}
	armed := grunt
	armed.Mechanisms = map[string]bool{known: true}
	defective := grunt
	defective.Mechanisms = map[string]bool{"no-such-mechanism": true}

	for _, tc := range []struct {
		name       string
		named      string
		entry      config.ServerEntry
		beat       heartbeat.Beat
		wantBeat   bool
		wantTarget bool
		wantSeat   bool
		wantNotice string
		// noticePrefix compares the head of the sentence only, for the one case whose tail is the
		// whole Mechanism catalogue — a list this test has no business pinning.
		noticePrefix bool
		wantArmed    bool
	}{
		{
			name:  "no key names no seat and asks nothing",
			entry: grunt,
		},
		{
			name:       "a name the list does not carry degrades and says which",
			named:      "typo",
			entry:      grunt,
			wantNotice: `sub-agents: no servers entry named "typo" — delegations run on the session server (configured: grunt)`,
		},
		{
			name:       "a reachable entry is routed to",
			named:      "grunt",
			entry:      grunt,
			beat:       heartbeat.Beat{Reachable: true, TotalSlots: 5, ContextWindow: 4096},
			wantBeat:   true,
			wantTarget: true,
			wantSeat:   true,
			wantNotice: "sub-agents: routing to grunt (grunt-model)",
		},
		{
			name:       "an unreachable entry keeps its seat and routes nothing",
			named:      "grunt",
			entry:      grunt,
			beat:       heartbeat.Beat{Failure: "dial tcp: refused"},
			wantBeat:   true,
			wantSeat:   true,
			wantNotice: "sub-agents: grunt unavailable — delegations run on the session server",
		},
		{
			name:  "a defective mechanisms map is a notice, never an error",
			named: "grunt",
			entry: defective,
			wantNotice: "sub-agents: delegations run on the session server — apogee: unknown mechanism " +
				`"no-such-mechanism"`,
			noticePrefix: true,
		},
		{
			name:       "a mechanisms map travels to the child as the entry's own",
			named:      "grunt",
			entry:      armed,
			beat:       heartbeat.Beat{Reachable: true, TotalSlots: 2},
			wantBeat:   true,
			wantTarget: true,
			wantSeat:   true,
			wantArmed:  true,
			wantNotice: "sub-agents: routing to grunt (grunt-model)",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			beats := &stubBeat{beat: tc.beat}
			prev := discoverDelegationBeat
			discoverDelegationBeat = beats.discover
			t.Cleanup(func() { discoverDelegationBeat = prev })

			_, routing, notices, err := firingConfig(context.Background(), firingInputs{
				opts: config.Options{
					Bypass:          true,
					Servers:         []config.ServerEntry{tc.entry},
					SubAgentsServer: tc.named,
				},
				entry:    config.ServerEntry{Name: "box", Endpoint: "http://box.example/v1", ParallelAgents: 1, EffortDialect: "reasoning"},
				apiKey:   "sk-test",
				roots:    firingRoots(t),
				confiner: fenceableHost,
				mode:     domain.ModePlan,
				recordID: "2026-09-02T09-00-00-firing",
			})
			if err != nil {
				t.Fatalf("firingConfig: %v; routing must degrade with a notice, never refuse the run", err)
			}

			if beats.called != tc.wantBeat {
				t.Errorf("the Sub-agent beat fired = %v, want %v", beats.called, tc.wantBeat)
			}
			if got := routing.target != nil; got != tc.wantTarget {
				t.Fatalf("routing.target non-nil = %v, want %v", got, tc.wantTarget)
			}
			if got := routing.seat != nil; got != tc.wantSeat {
				t.Fatalf("routing.seat non-nil = %v, want %v", got, tc.wantSeat)
			}
			if tc.wantNotice == "" {
				if len(notices) != 0 {
					t.Errorf("notices = %q; a run that names no Sub-agent server has nothing to say", notices)
				}
			} else if !slices.ContainsFunc(notices, func(n string) bool {
				if tc.noticePrefix {
					return strings.HasPrefix(n, tc.wantNotice)
				}
				return n == tc.wantNotice
			}) {
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
			if want := tc.beat.TotalSlots; routing.target.ParallelAgents != want {
				t.Errorf("routing.target.ParallelAgents = %d; want the %d the beat reported",
					routing.target.ParallelAgents, want)
			}
			if got := routing.target.Mechanisms != nil; got != tc.wantArmed {
				t.Errorf("routing.target.Mechanisms non-nil = %v, want %v — an entry with a `mechanisms:` "+
					"map must not leave its children inheriting the parent's catalogue", got, tc.wantArmed)
			}
		})
	}
}
