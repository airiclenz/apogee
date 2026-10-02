package main

import (
	"path/filepath"
	"testing"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/mcp"
	"github.com/airiclenz/apogee/internal/skills"
	"github.com/airiclenz/apogee/internal/tui"
)

// fakeApplier builds the applier the composition root hands over — all thirteen members present —
// so a test overrides only the members it asserts on and every other one is a working fixture
// rather than a nil the dispatcher would dereference. A refusal from it is therefore the
// dispatcher's own answer about the key, never a member the test forgot.
//
// The seams behind it are the fixtures the per-key tests use: a spy engine, a live holder over an
// empty snapshot, a binding naming one model and a rebind probe to ride, an empty config file for
// the keys re-read whole, a skill catalogue over fresh roots, a tool set that rebuilds into a fresh
// registry, an MCP holder over a fake session, a presentation ladder, a Parallel agents cap over a
// width spy, a delegation wiring over a push spy, and a stats recorder that is off.
func fakeApplier(t *testing.T) settingsApplier {
	t.Helper()
	workspace := t.TempDir()
	roots, err := resolveRoots(t.TempDir(), workspace)
	if err != nil {
		t.Fatalf("resolveRoots: %v", err)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeSettingsFixture(t, path, "")

	return settingsApplier{
		engine:     &applySettingSpy{},
		live:       newLiveSettings(config.Options{}),
		binding:    func() upstreamBinding { return upstreamBinding{Model: "bound-model"} },
		rebind:     (&rebindProbe{}).rebind,
		configPath: path,
		skills: skills.NewProvider(skills.Sources{
			Home:      roots.config,
			Workspace: roots.workspace,
		}),
		tools: newLiveTools(apogee.NewToolRegistry(), toolSetSpec{},
			func(toolSetSpec) *apogee.ToolRegistry { return apogee.NewToolRegistry() }),
		mcp: newLiveMCP(&fakeMCPSession{}, func([]mcp.ServerConfig) (mcpSession, error) {
			return &fakeMCPSession{}, nil
		}),
		present: newLivePresentation(config.PresentSettings{AutoOpen: true}, workspace, "darwin",
			func(string) string { return "" }, func(tui.Presentation) {}),
		roots:      roots,
		caps:       newParallelAgentsCap(&parallelAgentsSpy{}),
		delegation: &delegationWiring{userProfiles: noProfiles, engine: &delegationSpy{}},
		stats:      newStatsRecorder(serverStatsPath(roots.config), false),
	}
}
