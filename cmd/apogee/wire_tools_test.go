package main

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/mechanisms"
)

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

// A `mechanisms:` key naming a RETIRED Mechanism is DROPPED, not refused: the key was valid at the
// release before the removal, so a config the user never edited must still start. It is dropped
// whichever value it carries, it never reaches Config.EnableMechanisms, and the producer itself says
// nothing — two of its three call paths run with the alt screen up, where stderr paints over the TUI.
// Everything alongside it in the block still arms.
func TestMechanismIDsRetiredIDIsDropped(t *testing.T) {
	for _, tt := range []struct {
		name    string
		enabled map[string]bool
		want    []apogee.MechanismID
	}{
		{"retired and asked for", map[string]bool{"grammar": true}, nil},
		{"retired and switched off", map[string]bool{"grammar": false}, nil},
		{"retired beside a live row", map[string]bool{"grammar": true, "validate": true}, []apogee.MechanismID{"validate"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var ids []apogee.MechanismID
			var err error

			printed := captureStderr(t, func() {
				ids, err = mechanismIDs(tt.enabled, mechanisms.KnownIDs())
			})

			if err != nil {
				t.Fatalf("mechanismIDs(%v): a retired id must be tolerated, got %v", tt.enabled, err)
			}
			if len(ids) != len(tt.want) {
				t.Fatalf("mechanismIDs(%v) = %v, want %v", tt.enabled, ids, tt.want)
			}
			for i := range tt.want {
				if ids[i] != tt.want[i] {
					t.Errorf("mechanismIDs(%v)[%d] = %q, want %q", tt.enabled, i, ids[i], tt.want[i])
				}
			}
			if printed != "" {
				t.Errorf("mechanismIDs wrote to stderr: %q — the notice is retiredMechanismNotices' job", printed)
			}
		})
	}
}

// The notice names every retired id the block turns ON, one line each, in sorted spelling — and says
// what it is and what to do about it. A retired id set to FALSE earns no line (the user is not asking
// for it), and neither does a live row or an empty block.
func TestRetiredMechanismNoticesNameEachRetiredID(t *testing.T) {
	t.Parallel()

	got := retiredMechanismNotices(map[string]bool{"grammar": true, "validate": true})

	want := []string{
		`apogee: mechanism "grammar" was retired in ` + retiredMechanismRelease + ` and is ignored; remove it from mechanisms:`,
	}
	if len(got) != len(want) || got[0] != want[0] {
		t.Errorf("retiredMechanismNotices = %q, want %q", got, want)
	}

	for _, quiet := range []map[string]bool{nil, {}, {"grammar": false}, {"validate": true}} {
		if lines := retiredMechanismNotices(quiet); lines != nil {
			t.Errorf("retiredMechanismNotices(%v) = %q, want no lines", quiet, lines)
		}
	}
}
