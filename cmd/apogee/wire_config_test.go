package main

import (
	"context"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/mcp"
	"github.com/airiclenz/apogee/internal/skills"
)

// projectionOptions is one Options value with every shared key set away from its zero, so a Driver
// that dropped any one of them on the way to its Config is told which.
func projectionOptions(t *testing.T) config.Options {
	t.Helper()
	opts := config.Options{
		Mode:                 "ask-before",
		Workspace:            t.TempDir(),
		ConfigDir:            t.TempDir(),
		Bypass:               true,
		ConfineToWorkspace:   true,
		WebSearchEndpoint:    "https://search.example/v1",
		ToolsDisabled:        []string{"run_terminal_cmd"},
		ToolsEnabled:         []string{"web_search"},
		URLAllowHosts:        []string{"allowed.example"},
		URLDenyHosts:         []string{"denied.example"},
		UI:                   domain.UIPrefs{Inspector: true},
		UndoSnapshots:        true,
		ContextFiles:         []string{"AGENTS.md"},
		AutoCompact:          true,
		PruneToolResults:     true,
		DelegateMaxSteps:     12,
		DelegateFanOutRounds: 3,
		DelegateMaxDepth:     2,
		DelegateMaxTokens:    4096,
		DelegateTimeout:      90 * time.Second,
		StreamIdleTimeout:    45 * time.Second,
		RestreamBudget:       1,
		ToolUseEnforcer:      true,
		ToolLoopBreaker:      true,
		ContextFillNotice:    true,
		Servers: []config.ServerEntry{
			{Name: "box", Endpoint: "http://box.example/v1", APIKeyEnv: "BOX_KEY"},
		},
	}
	// The workflow keys away from their defaults, a 0 among them: a fold that dropped one would
	// read as the engine's own default, which none of these is.
	opts.WorkflowRetries = 4
	opts.WorkflowContinuations = 0
	opts.WorkflowWake = false
	return opts
}

// assertCarriesProjection asserts that a Driver's Config carries every key the projection filled,
// unchanged — the whole claim of projectConfig existing. The two func-valued mounts are compared by
// what they answer, once the toolchain probe has settled, since a func has no equality.
func assertCarriesProjection(t *testing.T, got, want apogee.Config) {
	t.Helper()

	if got.Mode != want.Mode {
		t.Errorf("Config.Mode = %v, want %v", got.Mode, want.Mode)
	}
	if got.Bypass != want.Bypass {
		t.Errorf("Config.Bypass = %v, want %v", got.Bypass, want.Bypass)
	}
	if got.ConfigDir != want.ConfigDir || got.WorkspaceDir != want.WorkspaceDir {
		t.Errorf("Config dirs = (%q, %q), want (%q, %q)", got.ConfigDir, got.WorkspaceDir, want.ConfigDir, want.WorkspaceDir)
	}
	if got.Confiner != want.Confiner {
		t.Errorf("Config.Confiner = %T, want the projection's %T", got.Confiner, want.Confiner)
	}
	if got.ConfineToWorkspace != want.ConfineToWorkspace {
		t.Errorf("Config.ConfineToWorkspace = %v, want %v", got.ConfineToWorkspace, want.ConfineToWorkspace)
	}
	if got.WebSearchEndpoint != want.WebSearchEndpoint {
		t.Errorf("Config.WebSearchEndpoint = %q, want %q", got.WebSearchEndpoint, want.WebSearchEndpoint)
	}
	for _, list := range []struct {
		name      string
		got, want []string
	}{
		{"DisabledTools", got.DisabledTools, want.DisabledTools},
		{"EnabledTools", got.EnabledTools, want.EnabledTools},
		{"URLAllowHosts", got.URLAllowHosts, want.URLAllowHosts},
		{"URLDenyHosts", got.URLDenyHosts, want.URLDenyHosts},
		{"SecretEnvVars", got.SecretEnvVars, want.SecretEnvVars},
		{"ContextFiles", got.ContextFiles, want.ContextFiles},
	} {
		if !slices.Equal(list.got, list.want) {
			t.Errorf("Config.%s = %q, want %q", list.name, list.got, list.want)
		}
	}
	if got.Inspector != want.Inspector {
		t.Errorf("Config.Inspector = %v, want %v", got.Inspector, want.Inspector)
	}
	if got.UndoSnapshots != want.UndoSnapshots {
		t.Errorf("Config.UndoSnapshots = %v, want %v", got.UndoSnapshots, want.UndoSnapshots)
	}
	if got.Skills != want.Skills || got.SkillLookup != want.SkillLookup {
		t.Errorf("Config.Skills/SkillLookup = (%p, %p), want the projection's provider (%p, %p)",
			got.Skills, got.SkillLookup, want.Skills, want.SkillLookup)
	}
	hostToolchain.wait()
	if got.ReadMounts.Roots == nil || !slices.Equal(got.ReadMounts.Roots(), want.ReadMounts.Roots()) {
		t.Errorf("Config.ReadMounts.Roots() = %v, want the projection's %v", callRoots(got.ReadMounts.Roots), want.ReadMounts.Roots())
	}
	if got.ReadMounts.Virtual == nil || !maps.Equal(got.ReadMounts.Virtual(), want.ReadMounts.Virtual()) {
		t.Errorf("Config.ReadMounts.Virtual is nil or answers other mounts than the projection's %v", want.ReadMounts.Virtual())
	}
	if got.Context.CompactionEnabled != want.Context.CompactionEnabled || got.Context.PruneToolResults != want.Context.PruneToolResults {
		t.Errorf("Config.Context switches = (%v, %v), want (%v, %v)",
			got.Context.CompactionEnabled, got.Context.PruneToolResults, want.Context.CompactionEnabled, want.Context.PruneToolResults)
	}
	if got.Delegation != want.Delegation {
		t.Errorf("Config.Delegation = %+v, want %+v", got.Delegation, want.Delegation)
	}
	if got.StreamIdleTimeout != want.StreamIdleTimeout {
		t.Errorf("Config.StreamIdleTimeout = %v, want %v", got.StreamIdleTimeout, want.StreamIdleTimeout)
	}
	// A pointer at the engine, so the VALUE is compared: nil would read as the engine's own
	// default and hide a fold-in that dropped the key.
	if got.RestreamBudget == nil || want.RestreamBudget == nil || *got.RestreamBudget != *want.RestreamBudget {
		t.Errorf("Config.RestreamBudget = %v, want %v (both non-nil)", got.RestreamBudget, want.RestreamBudget)
	}
	// Pointers at the engine too, RestreamBudget's reason: every field must arrive non-nil.
	gotFlow, wantFlow := got.Workflow, want.Workflow
	if gotFlow.Retries == nil || gotFlow.Continuations == nil || gotFlow.Wake == nil ||
		gotFlow.ResolvedRetries() != wantFlow.ResolvedRetries() ||
		gotFlow.ResolvedContinuations() != wantFlow.ResolvedContinuations() ||
		gotFlow.ResolvedWake() != wantFlow.ResolvedWake() {
		t.Errorf("Config.Workflow = (%v, %v, %v), want (%d, %d, %v) with every field non-nil",
			gotFlow.Retries, gotFlow.Continuations, gotFlow.Wake,
			wantFlow.ResolvedRetries(), wantFlow.ResolvedContinuations(), wantFlow.ResolvedWake())
	}
	if got.Floor != want.Floor {
		t.Errorf("Config.Floor = %+v, want %+v", got.Floor, want.Floor)
	}
	if got.ContextFillNotice != want.ContextFillNotice {
		t.Errorf("Config.ContextFillNotice = %v, want %v", got.ContextFillNotice, want.ContextFillNotice)
	}
}

// callRoots answers a mount func's roots, or nil for a mount that was never wired, so a failure
// message can print what the Driver handed over without a nil func panicking the test.
func callRoots(mount func() []string) []string {
	if mount == nil {
		return nil
	}
	return mount()
}

// Every key both Drivers fill identically comes off ONE projection, and each Driver carries it
// unchanged: one row per Config assembly that shares it — the session's boot phase and the Firing
// composer — each driven on the same fully-set Options and compared against projectConfig called on
// the inputs that Driver projected from. A key a Driver overlaid, dropped or re-resolved on the way
// to its Config is what this fails on; the per-site keys (the binding, the human seams, the window
// budget) are each Driver's own and asserted by its own tests.
func TestEveryDriverCarriesTheProjectedConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		assemble func(t *testing.T) (got, want apogee.Config)
	}{
		{
			name: "the session's boot phase",
			assemble: func(t *testing.T) (apogee.Config, apogee.Config) {
				opts := projectionOptions(t)
				roots, err := resolveRoots(opts.ConfigDir, opts.Workspace)
				if err != nil {
					t.Fatalf("resolveRoots: %v", err)
				}
				w := newRootWiring(opts, apogee.ModeAskBefore, roots)
				t.Cleanup(w.close)
				if err := w.resolveConfig(); err != nil {
					t.Fatalf("resolveConfig: %v", err)
				}
				return w.cfg, projectConfig(w.opts, w.roots, w.confiner, w.mode, w.skillProvider)
			},
		},
		{
			name: "the Firing composer",
			assemble: func(t *testing.T) (apogee.Config, apogee.Config) {
				opts := projectionOptions(t)
				roots := firingRoots(t)
				provider := skills.NewProvider(skills.Sources{Home: roots.config, Workspace: roots.workspace})
				cfg, _, _, err := firingConfig(context.Background(), firingInputs{
					opts:     opts,
					entry:    config.ServerEntry{Endpoint: "http://box.example/v1", ParallelAgents: 1, EffortDialect: "reasoning"},
					apiKey:   "sk-test",
					roots:    roots,
					confiner: fenceableHost,
					mode:     domain.ModeAuto,
					skills:   provider,
					beat:     firingBeat,
					recordID: "2026-09-16T09-00-00-firing",
				})
				if err != nil {
					t.Fatalf("firingConfig: %v", err)
				}
				return cfg, projectConfig(opts, roots, fenceableHost, domain.ModeAuto, provider)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, want := tt.assemble(t)

			assertCarriesProjection(t, got, want)
		})
	}
}

// The three workflow keys reach the engine as the loader read them: projectConfig folds each as a
// POINTER at the Options' own reading, so a stated 0 or off survives as the human's value and never
// falls back to the engine default a nil would read as.
func TestProjectConfigFoldsTheWorkflowKeys(t *testing.T) {
	t.Parallel()

	opts := projectionOptions(t)
	roots := firingRoots(t)
	provider := skills.NewProvider(skills.Sources{Home: roots.config, Workspace: roots.workspace})
	flow := projectConfig(opts, roots, fenceableHost, domain.ModeAuto, provider).Workflow

	if flow.Retries == nil || flow.Continuations == nil || flow.Wake == nil {
		t.Fatalf("Config.Workflow = %+v; want every field folded in, none left to the engine default", flow)
	}
	if got := flow.ResolvedRetries(); got != 4 {
		t.Errorf("Config.Workflow retries = %d, want the 4 the options carry", got)
	}
	if got := flow.ResolvedContinuations(); got != 0 {
		t.Errorf("Config.Workflow continuations = %d, want the stated 0, not the default", got)
	}
	if flow.ResolvedWake() {
		t.Error("Config.Workflow wake = on, want the stated off")
	}
}

// An HTTP MCP server's `headers-env:` names a token variable, and its `client-secret-env:` an OAuth
// client's secret, so both join the secret set beside the key sources and the Reaction header
// sources: a token a `terminal` child could read is a token the model can read.
func TestProjectConfigScrubsTheMCPHeaderEnvNames(t *testing.T) {
	t.Parallel()
	opts := projectionOptions(t)
	opts.MCPServers = []mcp.ServerConfig{{
		Name:       "docs",
		Transport:  mcp.TransportStreamableHTTP,
		Endpoint:   "https://mcp.example.com/",
		HeadersEnv: map[string]string{"Authorization": "DOCS_MCP_TOKEN"},
	}, {
		Name:            "wiki",
		Transport:       mcp.TransportStreamableHTTP,
		Endpoint:        "https://wiki.example.com/mcp",
		Auth:            mcp.AuthOAuth,
		ClientID:        "apogee-client",
		ClientSecretEnv: "WIKI_CLIENT_SECRET",
	}}
	roots := firingRoots(t)
	provider := skills.NewProvider(skills.Sources{Home: roots.config, Workspace: roots.workspace})

	secrets := projectConfig(opts, roots, fenceableHost, domain.ModeAuto, provider).SecretEnvVars

	for _, want := range []string{"BOX_KEY", "DOCS_MCP_TOKEN", "WIKI_CLIENT_SECRET"} {
		if !slices.Contains(secrets, want) {
			t.Errorf("SecretEnvVars = %q; want it to carry %q", secrets, want)
		}
	}
}
