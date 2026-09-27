package main

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/skills"
	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tools"
)

// registryWithMCP is the one place the composition root assembles HostTools by hand, so it must
// thread the Presenter as well — otherwise configuring an MCP server would silently take
// present_document away, which is exactly the kind of coupling the default build has no way to
// catch.
func TestRegistryWithMCPThreadsPresenter(t *testing.T) {
	t.Parallel()
	cfg := validCfg(t)
	cfg.Presenter = stubPresenter{}

	if _, ok := registryWithMCP(t.TempDir(), cfg, false, false, nil).Lookup("present_document"); !ok {
		t.Error("present_document is missing from the MCP registry build despite a configured Presenter")
	}
}

// The read-only mounts reach the assembly the same way the Presenter does, and drift the same way:
// registryWithMCP builds HostTools by hand, so a mount the engine's own build would have honoured
// would silently vanish for any session with an MCP server configured — the model could read a
// skill's bundled files in one session and not in another, for a reason nothing on screen explains.
func TestRegistryWithMCPThreadsExtraReadRoots(t *testing.T) {
	t.Parallel()
	// Resolved: a root that is not its own real path is skipped at the mount, so an
	// unresolved TMPDIR would fail this for the fence's reason rather than the MCP build's.
	extra := readFenceRealDir(t)
	if err := os.WriteFile(filepath.Join(extra, "SKILL.md"), []byte("bundled bytes"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	cfg := validCfg(t)
	cfg.ReadMounts.Roots = func() []string { return []string{extra} }

	tool, ok := registryWithMCP(cfg.WorkspaceDir, cfg, false, false, nil).Lookup("read_file")
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
		t.Errorf("read under the mounted root failed: %q — the MCP build dropped ReadMounts.Roots", result.Content)
	}
}

// The pathless mounts reach the same assembly, and by the same argument: a shipped skill's block
// announces `shipped:<id>` in every session, so an MCP build that dropped the mount would make that
// announced address readable without an MCP server and unreadable with one. The provider is the
// real one — the address under test is the address the loader stamps.
func TestRegistryWithMCPThreadsVirtualReadRoots(t *testing.T) {
	t.Parallel()
	provider := skills.NewProvider(skills.Sources{UseShippedSkills: true})
	cfg := validCfg(t)
	cfg.ReadMounts.Virtual = provider.VirtualReadRoots

	sk, ok := provider.Get("debugging")
	if !ok {
		t.Fatal("the shipped debugging skill did not load")
	}
	tool, found := registryWithMCP(cfg.WorkspaceDir, cfg, false, false, nil).Lookup("list_dir")
	if !found {
		t.Fatal("list_dir is missing from the MCP registry build")
	}
	result, err := tool.Execute(context.Background(), apogee.ToolCall{
		ID:        "c1",
		Tool:      "list_dir",
		Arguments: []byte(`{"path":` + strconv.Quote(sk.Dir) + `}`),
	})
	if err != nil {
		t.Fatalf("list_dir returned a Go error: %v", err)
	}
	if result.IsError || !strings.Contains(result.Content, "SKILL.md") {
		t.Errorf("listing the announced %q failed: %q — the MCP build dropped ReadMounts.Virtual", sk.Dir, result.Content)
	}
}

// The model's own door onto the skill catalog reaches the assembly on the same argument: load_skill
// is registered by CONSTRUCTION from Config.SkillLookup (ADR 0065 §6), so a hand-assembly that
// dropped the field would take the door away in exactly the sessions that connect an MCP server and
// leave it in every session that does not. The provider is the real one, so what comes back is a
// shipped skill's actual body.
func TestRegistryWithMCPThreadsSkillLookup(t *testing.T) {
	t.Parallel()
	provider := skills.NewProvider(skills.Sources{UseShippedSkills: true})
	cfg := validCfg(t)
	cfg.SkillLookup = provider

	tool, found := registryWithMCP(cfg.WorkspaceDir, cfg, false, false, nil).Lookup("load_skill")
	if !found {
		t.Fatal("load_skill is missing from the MCP registry build")
	}
	result, err := tool.Execute(context.Background(), apogee.ToolCall{
		ID:        "c1",
		Tool:      "load_skill",
		Arguments: []byte(`{"query":"debugging"}`),
	})
	if err != nil {
		t.Fatalf("load_skill returned a Go error: %v", err)
	}
	if result.IsError || !strings.Contains(result.Content, "<skill:") {
		t.Errorf("the shipped debugging skill did not come back: %q — the MCP build dropped SkillLookup", result.Content)
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

	tool, ok := registryWithMCP(cfg.WorkspaceDir, cfg, false, false, nil).Lookup("web_fetch")
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

	registry := registryWithMCP(t.TempDir(), cfg, false, false, []apogee.Tool{mcpTool})

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

			registry := registryWithMCP(t.TempDir(), cfg, false, false, []apogee.Tool{mcpFixtureTool{name: "docs__search"}})

			for _, name := range append(tt.wantOn, "docs__search") {
				assertRegistryOffers(t, registry, name, true)
			}
			for _, name := range tt.wantOff {
				assertRegistryOffers(t, registry, name, false)
			}
			if len(tt.wantOff) == 0 {
				if got, want := len(registry.All()), len(registryWithMCP(t.TempDir(), validCfg(t), false, false, nil).All())+1; got != want {
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

// The `sub-agents-choice:` gate reaches this hand-assembly through the tool SET's own spec rather
// than through apogee.Config (the engine reads no config of its own, ADR 0031), so registryWithMCP is
// the one place that could drop it — and dropping it would leave the key inert in every session while
// the config layer went on accepting it.
//
// The gate changes exactly ONE thing: whether sub_agent published `run_on`. The ROSTER is the same
// either way — same tools, same count — which is what makes a session with the key absent byte-for-
// byte the session that ran before the key existed.
func TestRegistryWithMCPCarriesTheSeatChoiceGate(t *testing.T) {
	t.Parallel()

	plain := registryWithMCP(t.TempDir(), validCfg(t), false, false, nil)
	offered := registryWithMCP(t.TempDir(), validCfg(t), true, false, nil)

	if seatChoiceOffered(t, plain) {
		t.Error("the gate off still published run_on; a session under `fixed` must offer no seat")
	}
	if !seatChoiceOffered(t, offered) {
		t.Error("the gate on published no run_on; `model` is the whole of what offers the seat")
	}
	if got, want := len(plain.All()), len(offered.All()); got != want {
		t.Errorf("the gate moved the roster: %d tools off, %d on — it may only move a schema", got, want)
	}
}

// stubHostAsker is a host question-asker that answers nothing — enough to make Config.Asker
// non-zero, which is all the composition pin below reads it for.
type stubHostAsker struct{}

func (stubHostAsker) Ask(context.Context, domain.AskRequest) (domain.AskAnswer, error) {
	return domain.AskAnswer{}, nil
}

// registryWithMCP composes its HostTools through tools.HostToolsOf — the ONE translation from Config
// the engine's own default roster shares — with the seat-choice gate the engine has no Config field
// for (ADR 0031) passed as the configured value. A tool-NAMES equivalence between the two registries
// could not have caught a composer that drifted: a name depends only on the roster rungs and the
// three nil-gated delegates, so dropping the URLGuard, the SecretEnvVars scrub, the ReadMounts.Roots
// mounts or the ReadMounts.Virtual ones — the very hazards this file's other tests each name one of —
// leaves every tool name identical while the user's policy quietly stops applying.
//
// So the pin is field-by-field rather than by name, on the Config THIS composition root builds
// (validCfg) with seat choice on: with every field the composer reads set to something non-zero,
// EVERY field of the struct it returns must come back non-zero, and every mount of its ReadMounts.
// TestHostToolsOfFillsEveryHostField (internal/tools) is the same pin on the composer's own side.
func TestHostToolsForFillsEveryHostField(t *testing.T) {
	t.Parallel()

	cfg := validCfg(t)
	cfg.URLAllowHosts = []string{"allowed.example"}
	cfg.URLDenyHosts = []string{"denied.example"}
	cfg.WebSearchEndpoint = "https://search.example/v1"
	cfg.Asker = stubHostAsker{}
	cfg.Presenter = stubPresenter{}
	cfg.SkillLookup = skills.NewProvider(skills.Sources{UseShippedSkills: true})
	cfg.DisabledTools = []string{"run_terminal_cmd"}
	cfg.EnabledTools = []string{"web_search"}
	cfg.Profile.Tools = domain.ToolRosterDelta{Enabled: []string{"console_open"}}
	cfg.SecretEnvVars = []string{"SOME_PROVIDER_KEY"}
	cfg.ReadMounts.Roots = func() []string { return []string{t.TempDir()} }
	cfg.ReadMounts.Scratch = func() string { return t.TempDir() }
	cfg.ReadMounts.Virtual = func() map[string]fs.FS { return nil }

	host := reflect.ValueOf(tools.HostToolsOf(cfg, true, true))
	mountsType := reflect.TypeFor[tools.ReadMounts]()
	for i := range host.NumField() {
		field, name := host.Field(i), host.Type().Field(i).Name
		if field.Type() == mountsType {
			// A struct-level zero check passes with ONE mount set, so each is checked on its own.
			for j := range field.NumField() {
				if field.Field(j).IsZero() {
					t.Errorf("HostToolsOf left tools.HostTools.%s.%s zero for a Config that mounts "+
						"it — the MCP-aware assembly must carry every read mount the engine's own "+
						"build would have", name, mountsType.Field(j).Name)
				}
			}
			continue
		}
		if field.IsZero() {
			t.Errorf("HostToolsOf left tools.HostTools.%s zero for a Config that sets every field "+
				"it reads — the MCP-aware assembly must carry every host policy the engine's own "+
				"build would have", name)
		}
	}
}

// backgroundPairOffered reports what registry offers of the background pair (ADR 0089 D1, D4):
// whether fan_out publishes `background`, and whether the workflow control tool is registered. It
// fails when the registry holds no fan_out, since every caller lifts it.
func backgroundPairOffered(t *testing.T, registry *apogee.ToolRegistry) (background, workflow bool) {
	t.Helper()
	found, ok := registry.Lookup(tools.FanOutToolName)
	if !ok {
		t.Fatal("the registry holds no fan_out; the roster lifted it")
	}
	fanOut, ok := found.(*tools.FanOut)
	if !ok {
		t.Fatalf("fan_out is a %T, want *tools.FanOut", found)
	}
	_, workflow = registry.Lookup(tools.WorkflowToolName)
	return fanOut.OffersBackground(), workflow
}

// backgroundRosterCfg is validCfg with fan_out and workflow lifted by `tools.enabled:`.
func backgroundRosterCfg(t *testing.T) apogee.Config {
	t.Helper()
	cfg := validCfg(t)
	cfg.EnabledTools = []string{tools.FanOutToolName, tools.WorkflowToolName}
	return cfg
}

// The background opt-in is a Driver's (ADR 0089 D1): the TUI passes it on, a firing passes it off,
// and the same roster — `tools.enabled: [fan_out, workflow]` — then offers the pair in the one and
// neither half of it in the other. It shapes a schema and one tool, never the rest of the roster.
func TestRegistryWithMCPCarriesTheBackgroundOptIn(t *testing.T) {
	t.Parallel()

	firing := registryWithMCP(t.TempDir(), backgroundRosterCfg(t), false, false, nil)
	live := registryWithMCP(t.TempDir(), backgroundRosterCfg(t), false, true, nil)

	if background, workflow := backgroundPairOffered(t, firing); background || workflow {
		t.Errorf("without the opt-in: background %v, workflow %v — want neither, whatever the roster says", background, workflow)
	}
	if background, workflow := backgroundPairOffered(t, live); !background || !workflow {
		t.Errorf("with the opt-in: background %v, workflow %v — want both where the roster lifts workflow", background, workflow)
	}
	if got, want := len(live.All()), len(firing.All())+1; got != want {
		t.Errorf("the opt-in moved the roster by %d tools, want exactly the workflow tool", got-len(firing.All()))
	}
}

// A rebuild carries the opt-in the set was built under, as it carries the seat-choice gate: a door
// that moves something else entirely — here the gate itself — must not drop the background pair.
func TestRegistryWithMCPRebuildCarriesTheBackgroundOptIn(t *testing.T) {
	t.Parallel()

	workspace, cfg := t.TempDir(), backgroundRosterCfg(t)
	build := func(spec toolSetSpec) *apogee.ToolRegistry {
		return registryWithMCP(workspace, cfg, spec.seatChoice, spec.offersBackground, nil)
	}
	spec := toolSetSpec{offersBackground: true}
	live := newLiveTools(build(spec), spec, build)
	spy := &applySettingSpy{}

	if err := live.setSeatChoice(true, spy); err != nil {
		t.Fatalf("setSeatChoice: %v", err)
	}

	if len(spy.swaps) != 1 {
		t.Fatalf("swaps = %d, want the one rebuild", len(spy.swaps))
	}
	if background, workflow := backgroundPairOffered(t, spy.swaps[0]); !background || !workflow {
		t.Errorf("after the rebuild: background %v, workflow %v — want the opt-in carried", background, workflow)
	}
}

// A Firing — headless or daemon — offers no background workflows (ADR 0089 D1). Under
// `sub-agents-choice: model` it builds its own set, which must carry the opt-in off; with the key
// absent it hands the engine no set, and the engine's own roster (defaultRoster) offers neither.
func TestFiringConfigOffersNoBackgroundWorkflows(t *testing.T) {
	roots := firingRoots(t)
	srv := stubllm.New(t, stubllm.Script{Discovery: stubllm.Discovery{
		Models: []stubllm.DiscoveredModel{{ID: "entry-model"}},
		Props:  &stubllm.Props{TotalSlots: 2},
	}})
	entry := config.ServerEntry{Name: "box", Endpoint: srv.URL, APIKey: "sk-entry", Model: "entry-model"}
	lifted := []string{tools.FanOutToolName, tools.WorkflowToolName}

	for _, choice := range []config.SubAgentsChoice{config.SubAgentsChoiceModel, ""} {
		cfg, _, _, err := firingConfig(context.Background(), firingInputs{
			opts:     config.Options{ToolsEnabled: lifted, SubAgentsChoice: choice},
			entry:    entry,
			roots:    roots,
			confiner: fenceableHost,
			mode:     domain.ModePlan,
			recordID: "2026-09-27T10-00-00-firing",
		})
		if err != nil {
			t.Fatalf("firingConfig(%q): %v", choice, err)
		}
		if cfg.Tools == nil {
			if choice == config.SubAgentsChoiceModel {
				t.Error("under `model` the firing built no set of its own")
			}
			continue
		}
		if background, workflow := backgroundPairOffered(t, cfg.Tools); background || workflow {
			t.Errorf("sub-agents-choice %q: background %v, workflow %v — a firing offers neither", choice, background, workflow)
		}
	}
}
