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
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/mechanisms"
	"github.com/airiclenz/apogee/internal/skills"
)

// registryWithMCP is the one place the composition root assembles HostTools by hand, so it must
// thread the Presenter as well — otherwise configuring an MCP server would silently take
// present_document away, which is exactly the kind of coupling the default build has no way to
// catch.
func TestRegistryWithMCPThreadsPresenter(t *testing.T) {
	t.Parallel()
	cfg := validCfg(t)
	cfg.Presenter = stubPresenter{}

	if _, ok := registryWithMCP(t.TempDir(), cfg, false, nil).Lookup("present_document"); !ok {
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
	cfg.ExtraReadRoots = func() []string { return []string{extra} }

	tool, ok := registryWithMCP(cfg.WorkspaceDir, cfg, false, nil).Lookup("read_file")
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

// The pathless mounts reach the same assembly, and by the same argument: a shipped skill's block
// announces `shipped:<id>` in every session, so an MCP build that dropped the mount would make that
// announced address readable without an MCP server and unreadable with one. The provider is the
// real one — the address under test is the address the loader stamps.
func TestRegistryWithMCPThreadsVirtualReadRoots(t *testing.T) {
	t.Parallel()
	provider := skills.NewProvider(skills.Sources{UseShippedSkills: true})
	cfg := validCfg(t)
	cfg.VirtualReadRoots = provider.VirtualReadRoots

	sk, ok := provider.Get("debugging")
	if !ok {
		t.Fatal("the shipped debugging skill did not load")
	}
	tool, found := registryWithMCP(cfg.WorkspaceDir, cfg, false, nil).Lookup("list_dir")
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
		t.Errorf("listing the announced %q failed: %q — the MCP build dropped VirtualReadRoots", sk.Dir, result.Content)
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

	tool, found := registryWithMCP(cfg.WorkspaceDir, cfg, false, nil).Lookup("load_skill")
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

	tool, ok := registryWithMCP(cfg.WorkspaceDir, cfg, false, nil).Lookup("web_fetch")
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

	registry := registryWithMCP(t.TempDir(), cfg, false, []apogee.Tool{mcpTool})

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

			registry := registryWithMCP(t.TempDir(), cfg, false, []apogee.Tool{mcpFixtureTool{name: "docs__search"}})

			for _, name := range append(tt.wantOn, "docs__search") {
				assertRegistryOffers(t, registry, name, true)
			}
			for _, name := range tt.wantOff {
				assertRegistryOffers(t, registry, name, false)
			}
			if len(tt.wantOff) == 0 {
				if got, want := len(registry.All()), len(registryWithMCP(t.TempDir(), validCfg(t), false, nil).All())+1; got != want {
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

// labMechanism is a minimal Mechanism HOOK for the swapped catalogue row below. It implements one
// hook interface (pre-request), which is all domain.MechanismRegistry.Add asks of a hook, and does
// nothing when it fires: what this file exercises is the wiring a row travels through, never a
// Mechanism's behaviour.
type labMechanism struct{}

func (labMechanism) PreRequest(context.Context, *domain.Request) error { return nil }

// The enabled IDs thread through New as Config.EnableMechanisms and the engine arms them — even
// under Bypass, enabling a real catalogued Mechanism constructs cleanly (the dispatch gate that
// skips it under Bypass is the engine's, exercised in internal/agent). This proves the config →
// EnableMechanisms → engine-build path is coherent end-to-end.
//
// The row has to be stood up: the SHIPPED catalogue is empty since v0.20.0 (ADR 0071), so naming
// any id here would resolve to an EMPTY enable list and the check would prove only that an empty
// list constructs. mechanisms.SwapCatalogue — the test-only seam that stands a temporary table in
// the curated one's place — is what gives the resolve a live row to keep.
//
// No t.Parallel(): SwapCatalogue assigns a package-level variable and is deliberately not
// concurrency-safe, so this test must stay sequential.
func TestMechanismIDsConstructsUnderBypass(t *testing.T) {
	const id domain.MechanismID = "lab_row"
	restore := mechanisms.SwapCatalogue([]mechanisms.Row{{
		Descriptor: domain.MechanismDescriptor{ID: id, Capability: domain.CapProactiveNudge},
		Construct:  func(mechanisms.Deps) (any, error) { return labMechanism{}, nil },
	}})
	defer restore()

	ids, notices, err := mechanisms.ResolveEnabled(map[string]bool{string(id): true}, mechanisms.KnownIDs())
	if err != nil {
		t.Fatalf("ResolveEnabled(%q): %v", id, err)
	}
	if len(ids) == 0 {
		t.Fatalf("ResolveEnabled(%q) resolved to no IDs; a live catalogued id must survive the resolve", id)
	}
	if len(notices) != 0 {
		t.Errorf("ResolveEnabled(%q) returned notices %q; a live id earns none", id, notices)
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

// The shape the check above had before its row was real — an enable map naming an id that had
// RETIRED, which ResolveEnabled drops on the way to Config.EnableMechanisms — is pinned here as
// what it actually proves: a retired id resolves to NOTHING and earns its floor-guard notice. So
// the vacuous version of the construct check cannot come back unnoticed; it would be building an
// empty enable list, which is this test's subject rather than that one's.
func TestMechanismIDsRetiredResolvesToNothing(t *testing.T) {
	t.Parallel()
	const retired = "validate"

	ids, notices, err := mechanisms.ResolveEnabled(map[string]bool{retired: true}, mechanisms.KnownIDs())
	if err != nil {
		t.Fatalf("ResolveEnabled(%q): %v", retired, err)
	}
	if len(ids) != 0 {
		t.Errorf("ResolveEnabled(%q) resolved to %v; a retired id reaches EnableMechanisms as nothing", retired, ids)
	}
	if len(notices) != 1 {
		t.Fatalf("ResolveEnabled(%q) returned %d notices; a retired id earns exactly one", retired, len(notices))
	}
	if !strings.Contains(notices[0], "floor guard") {
		t.Errorf("the notice for %q reads %q; it must name the floor guard that governs it now", retired, notices[0])
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

	plain := registryWithMCP(t.TempDir(), validCfg(t), false, nil)
	offered := registryWithMCP(t.TempDir(), validCfg(t), true, nil)

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

// registryWithMCP is one of TWO hand-assemblies of tools.HostTools — the engine's own hostTools
// (internal/agent) is the other, and the two are field-identical bar SubAgentSeatChoice, which the
// engine has no Config field for (ADR 0031). Nothing structural holds them that way, and a tool-NAMES
// equivalence between the two registries cannot: a name depends only on the roster rungs and the
// three nil-gated delegates, so dropping the URLGuard, the SecretEnvVars scrub, the ExtraReadRoots
// mounts or the VirtualReadRoots ones — the very hazards this file's other tests each name one of —
// leaves every tool name identical while the user's policy quietly stops applying.
//
// So the pin is field-by-field rather than by name: with every Config field this composer reads set
// to something non-zero, EVERY field of the struct it returns must come back non-zero. A field added
// to tools.HostTools and missed by this composer fails here;
// TestHostToolsFillsEveryHostField (internal/agent) is the same pin on the engine's side.
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
	cfg.ExtraReadRoots = func() []string { return []string{t.TempDir()} }
	cfg.VirtualReadRoots = func() map[string]fs.FS { return nil }

	host := reflect.ValueOf(hostToolsFor(cfg, true))
	for i := range host.NumField() {
		if host.Field(i).IsZero() {
			t.Errorf("hostToolsFor left tools.HostTools.%s zero for a Config that sets every field "+
				"it reads — the MCP-aware assembly must carry every host policy the engine's own "+
				"build would have", host.Type().Field(i).Name)
		}
	}
}
