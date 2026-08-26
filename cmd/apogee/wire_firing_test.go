package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/mechanisms"
	"github.com/airiclenz/apogee/internal/skills"
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
		Model:           "entry-model",
		ParallelAgents:  3,
		MaxOutputTokens: 4096,
		ContextWindow:   65536,
		ResponseReserve: 0.35,
	}
	provider := skills.NewProvider(skills.Sources{Home: roots.config, Workspace: roots.workspace})
	probed := false
	manual := mechanisms.KnownIDs()[:1]

	cfg, _, err := firingConfig(context.Background(), firingInputs{
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
	if !slices.Equal(cfg.ExtraReadRoots(), provider.SourceDirs()) {
		t.Errorf("Config.ExtraReadRoots() = %v; want the provider's own %v", cfg.ExtraReadRoots(), provider.SourceDirs())
	}
	if !slices.Equal(cfg.EnableMechanisms, manual) {
		t.Errorf("Config.EnableMechanisms = %v; want the validated manual list %v", cfg.EnableMechanisms, manual)
	}

	// The three bounds the BOUND entry carries outrank the top-level keys, and a pin answers the
	// fan-out width without spending a round trip on a question already settled.
	if cfg.Context.MaxContextTokens != entry.ContextWindow {
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
}

// The three optional seams, each nil, each taking the documented default: a fresh key resolver asks
// the entry's own source, a fresh catalog is built from the roots, and the width comes from the
// one-shot discovery probe. Those defaults are what headless and the daemon rely on — they have no
// longer-lived facility to share — so a change of default is a change to two Drivers at once.
func TestFiringConfigDefaultsItsSeams(t *testing.T) {
	roots := firingRoots(t)

	slots := &stubSlots{slots: 4}
	prev := discoverSlots
	discoverSlots = slots.discover
	t.Cleanup(func() { discoverSlots = prev })

	entry := config.ServerEntry{Name: "box", Endpoint: "http://box.example/v1", APIKey: "sk-from-the-entry", Model: "entry-model"}
	cfg, _, err := firingConfig(context.Background(), firingInputs{
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
}

// The delegates run.Once pins for itself stay nil, and so does the tool registry: a Firing reaches
// no external MCP server (ADR 0034), and handing the runner an Approver, an Asker, a Presenter or an
// Events sink is how an unattended run acquires a human it does not have (ADR 0033 decision 2).
func TestFiringConfigLeavesTheDriverSeamsNil(t *testing.T) {
	t.Parallel()

	cfg, _, err := firingConfig(context.Background(), firingInputs{
		opts:     config.Options{Bypass: true},
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
	if cfg.Tools != nil {
		t.Error("the composer wired a tool registry; a Firing takes the engine's own (no MCP)")
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

	cfg, _, err := firingConfig(context.Background(), firingInputs{
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
