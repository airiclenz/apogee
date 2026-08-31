package main

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/mcp"
	"github.com/airiclenz/apogee/internal/profiles"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/security"
	"github.com/airiclenz/apogee/internal/skills"
	"github.com/airiclenz/apogee/internal/tools"
	"github.com/airiclenz/apogee/internal/tui"
)

// The same key, live: committing `tools.disabled` in the settings pane rebuilds the set and hands it
// to the engine through the one door a set-level change goes through (SwapTools, ADR 0037 binding
// F), so the NEXT request's tool list is built without the disabled tool. The row's note says that
// boundary, and a name that is no tool is reported on the row rather than refused.
func TestApplySettingToolsDisabledSwapsTheSet(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	var built []string
	spy := &applySettingSpy{}
	live := newLiveTools(tools.NewDefaultRegistry(workspace), toolSetSpec{},
		func(spec toolSetSpec) *apogee.ToolRegistry {
			built = append(built, strings.Join(spec.disabled, ","))
			return tools.NewDefaultRegistryWithHost(workspace,
				tools.HostTools{WebSearchEndpoint: spec.endpoint, Disabled: spec.disabled})
		})
	apply := applySettingFor(settingsApplier{engine: spy, tools: live})

	note, err := apply("tools.disabled", "[view_diff, python_exec]")
	if err != nil {
		t.Fatalf("apply tools.disabled: %v", err)
	}
	if note != toolRosterNote {
		t.Errorf("note = %q, want %q", note, toolRosterNote)
	}
	if want := []string{"view_diff,python_exec"}; !slices.Equal(built, want) {
		t.Fatalf("rebuilds = %v, want %v", built, want)
	}
	if len(spy.swaps) != 1 {
		t.Fatalf("SwapTools calls = %d, want 1: which tools exist is a set-level change", len(spy.swaps))
	}
	if _, ok := spy.swaps[0].Lookup("view_diff"); ok {
		t.Error("the swapped-in registry still holds a disabled tool")
	}
	if _, ok := spy.swaps[0].Lookup("grep"); !ok {
		t.Error("the swapped-in registry lost a tool nobody disabled")
	}

	// A later edit is built from the roster it names, not from the one before it — and the search
	// endpoint the session is on rides along, rather than reverting to the startup value.
	if _, err := apply("tools.disabled", "grep"); err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if want := []string{"view_diff,python_exec", "grep"}; !slices.Equal(built, want) {
		t.Errorf("rebuilds = %v, want %v", built, want)
	}

	// A name that is no tool disables nothing and says so on the row, over a value already written.
	note, err = apply("tools.disabled", "grepp")
	if err != nil {
		t.Fatalf("an unrecognised name must not fail the apply: %v", err)
	}
	if !strings.Contains(note, "grepp") {
		t.Errorf("note = %q, want it to name the entry that is no tool", note)
	}
}

// The `url-safety:` host lists take the same door, for a related reason: the guard is built WITH the
// set — registryWithMCP hands one URLGuard to every network tool and no tool exposes a setter for it
// — so which hosts are reachable is part of the set's identity rather than a value on a tool.
// Committing either list rebuilds the set, hands it to the engine (SwapTools, ADR 0037 binding F) and
// reports the roster's boundary: the next request runs against the guard the new set carries.
func TestApplySettingURLSafetyHostsSwapTheSet(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	var guards []security.URLGuard
	spy := &applySettingSpy{}
	live := newLiveTools(tools.NewDefaultRegistry(workspace), toolSetSpec{},
		func(spec toolSetSpec) *apogee.ToolRegistry {
			// Built through the guard's own constructor, exactly as the composition root builds it:
			// the entries the row carries are normalised THERE, so a test that hand-built a
			// URLGuard literal would pass over a list the running session never matches with.
			guard := security.NewURLGuard(spec.allowHosts, spec.denyHosts)
			guards = append(guards, guard)
			return tools.NewDefaultRegistryWithHost(workspace, tools.HostTools{URLGuard: guard})
		})
	apply := applySettingFor(settingsApplier{engine: spy, tools: live})

	note, err := apply("url-safety.deny-hosts", "[metadata.internal, EVIL.example.com]")
	if err != nil {
		t.Fatalf("apply url-safety.deny-hosts: %v", err)
	}
	if note != toolRosterNote {
		t.Errorf("note = %q, want %q", note, toolRosterNote)
	}
	if len(spy.swaps) != 1 {
		t.Fatalf("SwapTools calls = %d, want 1: the guard is part of the set, not a field on a tool", len(spy.swaps))
	}
	if len(guards) != 1 {
		t.Fatalf("rebuilds = %d, want 1", len(guards))
	}
	if want := []string{"metadata.internal", "evil.example.com"}; !slices.Equal(guards[0].DenyHosts, want) {
		t.Fatalf("deny hosts = %v, want %v", guards[0].DenyHosts, want)
	}

	// The other list moves without reverting the one already committed: a rebuild carries the
	// configuration the session is ON, not the one it launched with.
	if _, err := apply("url-safety.allow-hosts", "[docs.example.com]"); err != nil {
		t.Fatalf("apply url-safety.allow-hosts: %v", err)
	}
	moved := guards[len(guards)-1]
	if want := []string{"docs.example.com"}; !slices.Equal(moved.AllowHosts, want) {
		t.Errorf("allow hosts = %v, want %v", moved.AllowHosts, want)
	}
	if want := []string{"metadata.internal", "evil.example.com"}; !slices.Equal(moved.DenyHosts, want) {
		t.Errorf("deny hosts = %v, want %v: an allow-list edit must not re-open a denied host", moved.DenyHosts, want)
	}

	// An EMPTY value is the pane saying the file no longer SETS the key, and it resolves the built-in
	// default a fresh start resolves: the empty list, which tightens nothing. The guard's own SSRF
	// floor is not reachable from configuration, so clearing a list never unfences it.
	if _, err := apply("url-safety.deny-hosts", ""); err != nil {
		t.Fatalf("apply url-safety.deny-hosts on an empty value: %v", err)
	}
	cleared := guards[len(guards)-1]
	if len(cleared.DenyHosts) != 0 {
		t.Errorf("deny hosts = %v, want none: an emptied key resolves the built-in default", cleared.DenyHosts)
	}
	if want := []string{"docs.example.com"}; !slices.Equal(cleared.AllowHosts, want) {
		t.Errorf("allow hosts = %v, want %v: clearing one list must not clear the other", cleared.AllowHosts, want)
	}
}

// ----------------------------------------------------------------------------
// The live-apply dispatcher (ADR 0037)
// ----------------------------------------------------------------------------

// Every key the dispatcher knows lands on ITS seam and no other, carrying the value the file spells.
// The context-files keys are the ones that answer with a boundary note, because their names are folded
// into the standing prompt only at a session boundary (ADR 0037 decision 3) — every other key is in
// force the moment it returns, which is what an empty note means.
func TestApplySettingDrivesTheRightEngineSeam(t *testing.T) {
	t.Parallel()
	names := []string{"AGENTS.md", "CLAUDE.md"}
	tests := []struct {
		name     string
		key      string
		value    string
		wantNote string
		check    func(t *testing.T, spy *applySettingSpy)
	}{
		{
			name: "mode", key: "mode", value: "allow-edits",
			check: func(t *testing.T, spy *applySettingSpy) {
				t.Helper()
				if want := []apogee.Mode{domain.ModeAllowEdits}; !slices.Equal(spy.modes, want) {
					t.Errorf("SetMode = %v, want %v", spy.modes, want)
				}
			},
		},
		{
			name: "bypass", key: "bypass", value: "true",
			check: func(t *testing.T, spy *applySettingSpy) {
				t.Helper()
				if want := []bool{true}; !slices.Equal(spy.bypass, want) {
					t.Errorf("SetBypass = %v, want %v", spy.bypass, want)
				}
			},
		},
		{
			name: "auto-compact", key: "auto-compact", value: "false",
			check: func(t *testing.T, spy *applySettingSpy) {
				t.Helper()
				if want := []bool{false}; !slices.Equal(spy.compaction, want) {
					t.Errorf("SetCompactionEnabled = %v, want %v", spy.compaction, want)
				}
			},
		},
		{
			name: "context-files.enable on carries this run's names", key: "context-files.enable", value: "true",
			wantNote: contextFileNote,
			check: func(t *testing.T, spy *applySettingSpy) {
				t.Helper()
				if len(spy.contextFiles) != 1 || !spy.contextFiles[0].enable ||
					!slices.Equal(spy.contextFiles[0].names, names) {
					t.Errorf("SetContextFiles = %+v, want one call with %v", spy.contextFiles, names)
				}
			},
		},
		{
			name: "context-files.enable off", key: "context-files.enable", value: "false",
			wantNote: contextFileNote,
			check: func(t *testing.T, spy *applySettingSpy) {
				t.Helper()
				if len(spy.contextFiles) != 1 || spy.contextFiles[0].enable {
					t.Errorf("SetContextFiles = %+v, want one call with the switch off", spy.contextFiles)
				}
			},
		},
		{
			// The names arrive as the FILE spells them — the one-line flow sequence the writer just
			// rendered — and reach the seam parsed back into the list a reader takes out of it, with
			// the switch the session is running carried along beside them.
			name: "context-files.names", key: "context-files.names", value: "[NOTES.md, docs/HOWTO.md]",
			wantNote: contextFileNote,
			check: func(t *testing.T, spy *applySettingSpy) {
				t.Helper()
				want := []string{"NOTES.md", "docs/HOWTO.md"}
				if len(spy.contextFiles) != 1 || !spy.contextFiles[0].enable ||
					!slices.Equal(spy.contextFiles[0].names, want) {
					t.Errorf("SetContextFiles = %+v, want one call with %v and the switch on",
						spy.contextFiles, want)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			spy := &applySettingSpy{}
			live := newLiveSettings(config.Options{ContextFiles: names}, nil)
			note, err := applySettingFor(settingsApplier{engine: spy, live: live})(tt.key, tt.value)
			if err != nil {
				t.Fatalf("apply %s=%s: %v", tt.key, tt.value, err)
			}
			if note != tt.wantNote {
				t.Errorf("note = %q, want %q", note, tt.wantNote)
			}
			tt.check(t, spy)
		})
	}
}

// The `context-files:` block is TWO keys and one engine call, so each key's apply has to carry the
// other's current half — which is why they share the live holder rather than a captured startup value.
// The case that proves it is the one the startup snapshot could not answer: a block that launched OFF
// resolves to no names at all, so the switch alone has nothing to install until the names row has been
// given some, and then it installs exactly those.
func TestApplySettingCarriesTheOtherHalfOfTheContextFilesBlock(t *testing.T) {
	t.Parallel()
	spy := &applySettingSpy{}
	live := newLiveSettings(config.Options{}, nil) // a session that launched with the block off
	apply := applySettingFor(settingsApplier{engine: spy, live: live})

	if _, err := apply("context-files.enable", "true"); err != nil {
		t.Fatalf("apply the switch: %v", err)
	}
	if len(spy.contextFiles) != 1 || len(spy.contextFiles[0].names) != 0 {
		t.Fatalf("SetContextFiles = %+v, want the switch on with no names to install yet", spy.contextFiles)
	}

	if _, err := apply("context-files.names", "[NOTES.md]"); err != nil {
		t.Fatalf("apply the names: %v", err)
	}
	want := []string{"NOTES.md"}
	if len(spy.contextFiles) != 2 || !spy.contextFiles[1].enable ||
		!slices.Equal(spy.contextFiles[1].names, want) {
		t.Fatalf("SetContextFiles = %+v, want %v under the switch this session turned on",
			spy.contextFiles, want)
	}

	// And back the other way: the switch now installs the names the names row gave it, not the empty
	// list this run launched with.
	if _, err := apply("context-files.enable", "false"); err != nil {
		t.Fatalf("apply the switch off: %v", err)
	}
	if _, err := apply("context-files.enable", "true"); err != nil {
		t.Fatalf("apply the switch on again: %v", err)
	}
	if n := len(spy.contextFiles); n != 4 || !slices.Equal(spy.contextFiles[3].names, want) {
		t.Errorf("SetContextFiles = %+v, want the last call to re-install %v", spy.contextFiles, want)
	}
}

// What the dispatcher will not apply, it REFUSES by name — the write has already landed, so a silent
// success would leave the file and the session disagreeing with nothing said about it. A key this
// build cannot apply and a value its seam cannot take are the same kind of answer.
func TestApplySettingRefusesWhatItCannotApply(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, key, value, wantIn string
	}{
		// `server` is the one key with no dispatcher home and never will have one: its live apply is
		// the picker's own switch (ADR 0037 decision 4), so a value arriving here is a value nothing
		// can do anything with. Every other editable key either has a seam or, like `editor`, is
		// already in force from the write itself (TestApplySettingAcceptsTheEditorKey).
		{name: "a key with no live seam", key: settingKeyServer, value: "second", wantIn: "server"},
		{name: "a key that is not a setting", key: "nonsense", value: "1", wantIn: "nonsense"},
		{name: "a bool that is not one", key: "bypass", value: "yes please", wantIn: "bypass is true or false"},
		{name: "a mode outside the ladder", key: "mode", value: "yolo", wantIn: "invalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			spy := &applySettingSpy{}
			note, err := applySettingFor(settingsApplier{engine: spy})(tt.key, tt.value)
			if err == nil {
				t.Fatalf("apply %s=%s: want a refusal naming the key, got note %q", tt.key, tt.value, note)
			}
			if !strings.Contains(err.Error(), tt.wantIn) {
				t.Errorf("error = %q, want it to contain %q", err, tt.wantIn)
			}
			if spy.drove() != 0 {
				t.Errorf("a refused apply still drove the engine: %+v", spy)
			}
		})
	}
}

// `editor` is the counter-case to the refusal above: a key with no seam that must NOT refuse. The
// editor ladder reads it off a fresh projection of the file every time an external edit starts, so
// the pane's write has already put it in force — and the default refusal would have told the user
// "editor cannot be applied to the running session" about a change that had taken effect. It answers
// with the empty note every in-force key answers with, drives no seam, and needs no member, so an
// applier holding nothing at all still applies it.
func TestApplySettingAcceptsTheEditorKey(t *testing.T) {
	t.Parallel()
	spy := &applySettingSpy{}
	note, err := applySettingFor(settingsApplier{engine: spy})("editor", "code -w")
	if err != nil {
		t.Fatalf("apply editor: %v", err)
	}
	if note != "" {
		t.Errorf("note = %q, want none: the key is in force from the write itself", note)
	}
	if spy.drove() != 0 {
		t.Errorf("applying editor drove an engine seam: %+v", spy)
	}
	if _, err := applySettingFor(settingsApplier{})("editor", "code -w"); err != nil {
		t.Errorf("apply editor through an applier holding nothing: %v; the key reaches no member", err)
	}
}

// startupOnlyContract is the closing sentence a key whose edit lands only at the NEXT start carries
// in its own registry Description. The row shows no note (ADR 0037 decision 3 gives one only when
// the session itself moves), so the pane's Description header is the single place the human is told.
const startupOnlyContract = "takes effect at the next start."

// settingKeysWithNoMemberToReach are the keys whose entire live apply is the write the pane has
// already made. They hold the dispatcher's only exemption from the nil-member refusal, and they are
// the only shape that can be one: the apply REQUIRES no member of the applier, so there is nothing a
// Driver could have been composed without. `editor` is re-read off a fresh projection of the file
// every time an external edit starts (ADR 0041 decision 1); the other four are read once, while
// the session is being built, and say so in their Descriptions. `ui.inspector`,
// `delegate-max-steps` and `working-window` still mirror their value onto the live holder for the
// Firings a session raises, and do nothing at all where a Driver composed none — which is why they
// are exempt rather than reaching for one.
var settingKeysWithNoMemberToReach = []string{
	"editor", "ui.inspector", "response-reserve", "delegate-max-steps", "working-window",
}

// The four START-UP-only keys are `editor`'s counter-case from the other side: keys with no seam
// that must not refuse either. `ui.inspector` decides whether a wire observer is installed while
// the provider client is constructed, `response-reserve` is read into the budget the session opens
// with, and `delegate-max-steps` and `working-window` are fields of the Config the engine was
// constructed with, so this session genuinely cannot move any of them — but the file the next one starts from HAS moved,
// which is the whole of what the key promises. Refusing would report a failed apply over a
// save that did exactly that, which is the defect this pins.
//
// The promise itself is asserted beside the silence, because they are one design: a row with no note
// says nothing, so the Description is where the human learns when the edit lands.
func TestApplySettingAcceptsTheStartupOnlyKeys(t *testing.T) {
	t.Parallel()
	tests := []struct{ key, value string }{
		{key: "ui.inspector", value: "true"},
		{key: "response-reserve", value: "0.25"},
		{key: "delegate-max-steps", value: "40"},
		{key: "working-window", value: "200000"},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			t.Parallel()
			spy := &applySettingSpy{}
			note, err := applySettingFor(settingsApplier{engine: spy})(tt.key, tt.value)
			if err != nil {
				t.Fatalf("apply %s: %v; the write is the whole of the apply, not a failure", tt.key, err)
			}
			if note != "" {
				t.Errorf("note = %q, want none: nothing about this session changed to report", note)
			}
			if spy.drove() != 0 {
				t.Errorf("applying %s drove an engine seam: %+v", tt.key, spy)
			}
			if _, err := applySettingFor(settingsApplier{})(tt.key, tt.value); err != nil {
				t.Errorf("apply %s through an applier holding nothing: %v; the key reaches no member",
					tt.key, err)
			}

			row, ok := config.LookupKey(tt.key)
			if !ok {
				t.Fatalf("no registry row for %q", tt.key)
			}
			if !strings.HasSuffix(row.Desc, startupOnlyContract) {
				t.Errorf("Description = %q, want it to end %q: the silent row leaves the header to say when",
					row.Desc, startupOnlyContract)
			}
		})
	}
}

// settingKeysAppliedByTheRenderer are the Editable keys the TUI applies to ITSELF and never routes
// out to the binary, because nothing behind [tui.Options.ApplySetting] would have anything to do
// with them — their whole effect is a field on the Model. The list is hardcoded because that switch
// is unexported in another package: internal/tui/settings.go, Model.settingsApplyLocal, is the
// source, and a key added there belongs here too.
var settingKeysAppliedByTheRenderer = []string{
	"auto-title",
	"ui.show-scrollbar",
	"ui.spinner",
	"ui.spinner-color",
	"ui.stall-after",
	"ui.color-scheme",
	"ui.skill-suggestions",
	"cursor-shape",
}

// The settings table is kept in config.KeyRegistry order — the order the pane renders the rows in —
// so the surface and the table can be read side by side, and a key inserted into the registry cannot
// quietly land at the bottom of the table. A subsequence is what is asserted rather than an exact
// match, because most registry keys reach no live seam at all and so have no entry.
//
// Duplicates are checked here too: two entries for one key would give settingsEntryFor a silent
// winner, and the loser's apply would be dead code nobody could see was dead.
func TestSettingsTableIsInRegistryOrder(t *testing.T) {
	t.Parallel()
	seen := make(map[string]bool, len(settingsTable))
	next := 0
	for _, entry := range settingsTable {
		if seen[entry.key] {
			t.Errorf("%s has two table entries; settingsEntryFor would pick one and orphan the other",
				entry.key)
		}
		seen[entry.key] = true

		for next < len(config.KeyRegistry) && config.KeyRegistry[next].Path != entry.key {
			next++
		}
		if next == len(config.KeyRegistry) {
			t.Fatalf("%s is out of registry order (or is no registry key at all); the table and the "+
				"settings surface would disagree about where the row belongs", entry.key)
		}
		next++
	}
}

// Every Editable key has SOMETHING that applies it. This is the guard four keys went missing from:
// `Editable: true` with no case in the dispatcher writes the file and then tells the human the save
// could not be applied to the running session — a lying row, over an edit that landed. There are
// exactly three honest homes for an editable key and this names all three, so a new key with none
// fails here rather than shipping that row:
//
//	a. the renderer applies it itself (settingKeysAppliedByTheRenderer),
//	b. the pane intercepts it before the dispatcher is ever asked (`server`, whose live apply is the
//	   picker's own switch — ADR 0037 decision 4), or
//	c. applySettingFor accepts it when every member it could need is composed.
//
// It is the mirror of TestApplySettingRefusesEveryKeyItCannotReach, which drives the same registry
// through a ZERO applier: that one holds what a missing MEMBER must answer, this one holds that a
// missing CASE is not a way to answer anything.
func TestEveryEditableSettingKeyHasAnApply(t *testing.T) {
	t.Parallel()
	apply := applySettingFor(fullyComposedApplier(t))
	for _, k := range config.KeyRegistry {
		if !k.Editable || k.Path == settingKeyServer ||
			slices.Contains(settingKeysAppliedByTheRenderer, k.Path) {
			continue
		}
		t.Run(k.Path, func(t *testing.T) {
			if _, err := apply(k.Path, k.Default); err != nil {
				t.Errorf("apply %s=%q: %v; an editable key with no apply shows the human a failed save",
					k.Path, k.Default, err)
			}
		})
	}
}

// fullyComposedApplier builds the dispatcher a Driver that composed EVERYTHING hands over — every
// optional member present, so a refusal from it is the dispatcher's own answer about the key rather
// than a member this test forgot. The seams behind it are the fixtures the per-key tests above use:
// a spy engine, a rebind probe, an empty config file for the keys re-read whole, and a tool set that
// rebuilds into a fresh registry.
func fullyComposedApplier(t *testing.T) settingsApplier {
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
		live:       newLiveSettings(config.Options{}, nil),
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
		roots: roots,
	}
}

// `remember-model` is the third shape again: a toggle with no engine seam and no re-resolution, whose
// whole apply is a store on the live holder. What makes the store the apply is that everything the
// toggle gates is still in the future — the next explicit `/model` pick, the next committed profile
// load, the next start-up — and all three ask the holder at the moment they have something to decide,
// so the flip governs the very next one rather than the next process.
func TestApplySettingRememberModelFlipsTheLiveToggle(t *testing.T) {
	t.Parallel()
	spy := &applySettingSpy{}
	live := newLiveSettings(config.Options{}, nil)
	apply := applySettingFor(settingsApplier{engine: spy, live: live})

	if live.remember() {
		t.Fatal("the holder opened with remembering on; the key's default is off")
	}
	note, err := apply("remember-model", "true")
	if err != nil {
		t.Fatalf("apply remember-model: %v", err)
	}
	if note != "" {
		t.Errorf("note = %q, want none: the toggle states its own outcome when it records", note)
	}
	if !live.remember() {
		t.Error("the flip did not reach the holder the recording seams read")
	}
	if spy.drove() != 0 {
		t.Errorf("applying remember-model drove an engine seam: %+v", spy)
	}

	// And back off again, since a toggle that could only be switched on would leave a session recording
	// picks the human has just said to stop recording.
	if _, err := apply("remember-model", "false"); err != nil {
		t.Fatalf("apply remember-model off: %v", err)
	}
	if live.remember() {
		t.Error("switching the toggle back off did not reach the holder")
	}

	// The bool vocabulary is the registry's, enforced at the write; the dispatcher refuses anything
	// else rather than storing a reading of its own, and leaves the toggle where it was.
	if _, err := apply("remember-model", "yes"); err == nil {
		t.Error("apply remember-model=yes was accepted; the key is a bool")
	}
	if live.remember() {
		t.Error("a refused value moved the toggle")
	}
}

// The holder is the session's live configuration, not just its exception list: a Firing raised
// inside the session composes its whole Config from options(), so every key a `/settings` commit
// applies has to be visible there (ADR 0037's promise carried into the runs a session raises).
// The ten keys below are the ones whose apply moves something OUTSIDE the holder — the tool set's
// four, the engine's two toggles, the start-up-only inspector, the context-file pair and the
// `servers:` list an unattended run's secret-env union is read off — which is exactly the set that
// used to leave the projection describing the launch snapshot.
//
// Each case is asserted twice: once on the projection, and once more after a caller has mauled every
// list and map it was handed. A holder that returned its own backing arrays would come back changed.
func TestLiveSettingsOptionsFollowEveryApply(t *testing.T) {
	t.Parallel()

	// The boot snapshot every case starts from, with each of the ten keys set to something its edit
	// moves OFF: a value that came back unchanged would be the launch snapshot showing through rather
	// than the apply landing.
	boot := config.Options{
		WebSearchEndpoint:  "https://boot.example.com/s",
		ToolsDisabled:      []string{"python_exec"},
		URLAllowHosts:      []string{"boot.example.com"},
		URLDenyHosts:       []string{"metadata.internal"},
		AutoCompact:        true,
		ContextFiles:       []string{"AGENTS.md"},
		Servers:            []config.ServerEntry{{Name: "here", Endpoint: "http://127.0.0.1:1111"}},
		Mechanisms:         map[string]bool{"codeinfo": true},
		ValidatedSetsAlias: map[string]string{"label": "entry"},
	}
	// The list the `servers:` apply re-reads. Its second entry names a key SOURCE rather than a key,
	// which is the fact an unattended run composed from these Options has to see (SecretEnvVars).
	const serversFile = "servers:\n" +
		"  - name: here\n    endpoint: http://127.0.0.1:1111\n" +
		"  - name: elsewhere\n    endpoint: http://127.0.0.1:2222\n    api-key-env: ELSEWHERE_KEY\n"

	tests := []struct {
		name  string
		key   string
		value string
		want  func(t *testing.T, opts config.Options)
	}{
		{
			name: "web-search-endpoint", key: "web-search-endpoint", value: "https://moved.example.com/s",
			want: func(t *testing.T, opts config.Options) {
				t.Helper()
				if got := opts.WebSearchEndpoint; got != "https://moved.example.com/s" {
					t.Errorf("WebSearchEndpoint = %q, want the endpoint the session moved to", got)
				}
			},
		},
		{
			name: "tools.disabled", key: "tools.disabled", value: "[grep, view_diff]",
			want: func(t *testing.T, opts config.Options) {
				t.Helper()
				if want := []string{"grep", "view_diff"}; !slices.Equal(opts.ToolsDisabled, want) {
					t.Errorf("ToolsDisabled = %v, want %v", opts.ToolsDisabled, want)
				}
			},
		},
		{
			name: "url-safety.allow-hosts", key: "url-safety.allow-hosts", value: "[docs.example.com]",
			want: func(t *testing.T, opts config.Options) {
				t.Helper()
				if want := []string{"docs.example.com"}; !slices.Equal(opts.URLAllowHosts, want) {
					t.Errorf("URLAllowHosts = %v, want %v", opts.URLAllowHosts, want)
				}
				if want := []string{"metadata.internal"}; !slices.Equal(opts.URLDenyHosts, want) {
					t.Errorf("URLDenyHosts = %v, want %v: one list moving must not clear the other",
						opts.URLDenyHosts, want)
				}
			},
		},
		{
			name: "url-safety.deny-hosts", key: "url-safety.deny-hosts", value: "[evil.example.com]",
			want: func(t *testing.T, opts config.Options) {
				t.Helper()
				if want := []string{"evil.example.com"}; !slices.Equal(opts.URLDenyHosts, want) {
					t.Errorf("URLDenyHosts = %v, want %v", opts.URLDenyHosts, want)
				}
			},
		},
		{
			name: "bypass", key: "bypass", value: "true",
			want: func(t *testing.T, opts config.Options) {
				t.Helper()
				if !opts.Bypass {
					t.Error("Bypass = false, want the floor the session was put on")
				}
			},
		},
		{
			name: "auto-compact", key: "auto-compact", value: "false",
			want: func(t *testing.T, opts config.Options) {
				t.Helper()
				if opts.AutoCompact {
					t.Error("AutoCompact = true, want the toggle the session switched off")
				}
			},
		},
		{
			name: "ui.inspector", key: "ui.inspector", value: "true",
			want: func(t *testing.T, opts config.Options) {
				t.Helper()
				if !opts.UI.Inspector {
					t.Error("UI.Inspector = false; the flip reaches no seam in THIS session, but the " +
						"runs it raises build a client of their own")
				}
			},
		},
		{
			// The block is two keys and ONE resolved list, so switching it off is the empty list a
			// start-up with `enable: false` resolves — not the names left standing behind the switch.
			name: "context-files.enable", key: "context-files.enable", value: "false",
			want: func(t *testing.T, opts config.Options) {
				t.Helper()
				if len(opts.ContextFiles) != 0 {
					t.Errorf("ContextFiles = %v, want none while the block is off", opts.ContextFiles)
				}
			},
		},
		{
			name: "context-files.names", key: "context-files.names", value: "[NOTES.md, docs/HOWTO.md]",
			want: func(t *testing.T, opts config.Options) {
				t.Helper()
				want := []string{"NOTES.md", "docs/HOWTO.md"}
				if !slices.Equal(opts.ContextFiles, want) {
					t.Errorf("ContextFiles = %v, want %v", opts.ContextFiles, want)
				}
			},
		},
		{
			// The value is not read for this key — the list is re-read off the file — so what the
			// projection must carry is the entry the file gained, key source and all.
			name: "servers", key: "servers", value: "",
			want: func(t *testing.T, opts config.Options) {
				t.Helper()
				if len(opts.Servers) != 2 {
					t.Fatalf("Servers = %+v, want the two the re-read file lists", opts.Servers)
				}
				if got := opts.Servers[1].APIKeyEnv; got != "ELSEWHERE_KEY" {
					t.Errorf("Servers[1].APIKeyEnv = %q, want ELSEWHERE_KEY", got)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(serversFile), 0o600); err != nil {
				t.Fatalf("write config: %v", err)
			}
			live := newLiveSettings(boot, nil)
			set := newLiveTools(apogee.NewToolRegistry(), toolSetSpec{
				endpoint:   boot.WebSearchEndpoint,
				disabled:   boot.ToolsDisabled,
				allowHosts: boot.URLAllowHosts,
				denyHosts:  boot.URLDenyHosts,
			}, func(toolSetSpec) *apogee.ToolRegistry { return apogee.NewToolRegistry() })
			apply := applySettingFor(settingsApplier{
				engine: &applySettingSpy{}, live: live, tools: set, configPath: path,
			})

			if _, err := apply(tt.key, tt.value); err != nil {
				t.Fatalf("apply %s=%s: %v", tt.key, tt.value, err)
			}

			handed := live.options()
			tt.want(t, handed)

			clobberOptions(handed)
			tt.want(t, live.options())
		})
	}
}

// clobberOptions does to a projection what a careless caller would: it overwrites every element of
// every list it was handed and empties every map, in place. A holder that gave out its own backing
// arrays rather than copies would come back changed by it.
func clobberOptions(opts config.Options) {
	for _, list := range [][]string{
		opts.ToolsDisabled, opts.URLAllowHosts, opts.URLDenyHosts, opts.ContextFiles,
	} {
		for i := range list {
			list[i] = "clobbered"
		}
	}
	for i := range opts.Servers {
		opts.Servers[i] = config.ServerEntry{Name: "clobbered"}
	}
	for i := range opts.ModelProfiles {
		opts.ModelProfiles[i] = profiles.Entry{}
	}
	clear(opts.Mechanisms)
	clear(opts.ValidatedSetsAlias)
	clear(opts.SystemPrompt.Models)
}

// The same sentence answers a key whose seam this Driver did not COMPOSE. Every member of the
// applier is optional by design — a bench, a daemon or an embedder has no presenter, no launcher and
// no skill catalogue (ADR 0031: the engine stays sufficient for any Driver) — so a missing member
// degrades to the refusal rather than panicking on the Update goroutine, halfway through an edit the
// file already carries. `use-project-skills` and `web-search-endpoint` are the two that used to.
//
// Driving EVERY registry key through a ZERO applier is also what keeps the nil guard in step with
// the switch it mirrors: a key wired into one and not the other panics right here.
func TestApplySettingRefusesEveryKeyItCannotReach(t *testing.T) {
	t.Parallel()
	apply := applySettingFor(settingsApplier{})
	for _, k := range config.KeyRegistry {
		if slices.Contains(settingKeysWithNoMemberToReach, k.Path) {
			// The exceptions, and the only shape that can be one: a key whose apply reaches no
			// member at all, so there is nothing a Driver could be composed without. Each answers
			// success even here — TestApplySettingAcceptsTheEditorKey and
			// TestApplySettingAcceptsTheStartupOnlyKeys hold that side.
			continue
		}
		t.Run(k.Path, func(t *testing.T) {
			t.Parallel()
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("apply %s reached a member this applier does not hold: %v", k.Path, r)
				}
			}()
			note, err := apply(k.Path, k.Default)
			if err == nil {
				t.Fatalf("apply %s: want a refusal naming the key, got note %q", k.Path, note)
			}
			if !strings.Contains(err.Error(), k.Path) {
				t.Errorf("error = %q, want it to name %q", err, k.Path)
			}
		})
	}
}

// The window pin has no engine setter of its own: it is a per-model binding, so a committed edit
// lands in the live holder and the whole per-model resolution is re-driven over it — the same door a
// heartbeat-observed model change goes through. Clearing the pin re-drives with the window the last
// beat reported, which is what keeps `0` meaning discover-live (ADR 0024) rather than "unknown".
func TestApplySettingContextWindowPinRidesTheRebind(t *testing.T) {
	t.Parallel()
	live := newLiveSettings(config.Options{ContextWindow: 4096}, nil)
	live.observe(8192, provider.EffortDialectNone) // what the last landed beat could name about the server's own window
	probe := &rebindProbe{}
	spy := &applySettingSpy{}
	apply := applySettingFor(settingsApplier{
		engine:  spy,
		live:    live,
		binding: func() upstreamBinding { return upstreamBinding{Model: "bound-model"} },
		rebind:  probe.rebind,
	})

	note, err := apply("context-window", "32768")
	if err != nil {
		t.Fatalf("apply context-window: %v", err)
	}
	if note != "" {
		t.Errorf("note = %q, want none: the pin is in force the moment the rebind commits", note)
	}
	if live.pin() != 32768 {
		t.Errorf("pin = %d, want the edited 32768", live.pin())
	}
	if want := []rebindCall{{model: "bound-model", window: 8192}}; !slices.Equal(probe.calls, want) {
		t.Fatalf("rebind drives = %+v, want %+v", probe.calls, want)
	}

	if _, err := apply("context-window", "0"); err != nil {
		t.Fatalf("apply context-window=0: %v", err)
	}
	if live.pin() != 0 {
		t.Errorf("pin = %d, want 0 — the cleared pin hands the window back to the server", live.pin())
	}
	if len(probe.calls) != 2 || probe.calls[1].window != 8192 {
		t.Errorf("rebind drives = %+v, want a second drive carrying the observed 8192", probe.calls)
	}
	if spy.drove() != 0 {
		t.Errorf("a rebind-riding key drove an anytime-safe mutator: %+v", spy)
	}
}

// Before a server is bound there is no model to rebind FOR (ADR 0036 decision 3 opens the pane on a
// session that has none). The edit is still recorded in the holder — the first beat's rebind resolves
// it in — and the row is told nothing, because nothing failed.
func TestApplySettingRideIsSilentBeforeAServerIsBound(t *testing.T) {
	t.Parallel()
	live := newLiveSettings(config.Options{}, nil)
	probe := &rebindProbe{}
	apply := applySettingFor(settingsApplier{
		engine:  &applySettingSpy{},
		live:    live,
		binding: func() upstreamBinding { return upstreamBinding{} },
		rebind:  probe.rebind,
	})

	note, err := apply("context-window", "16384")
	if err != nil || note != "" {
		t.Fatalf("apply context-window unbound = (%q, %v), want it to land quietly", note, err)
	}
	if len(probe.calls) != 0 {
		t.Errorf("rebind drives = %+v, want none: nothing is bound to rebind", probe.calls)
	}
	if live.pin() != 16384 {
		t.Errorf("pin = %d, want the edit held at 16384 for the first bind", live.pin())
	}
}

// A refused rebind — Agent.Rebind is idle-only, so an open Exchange is one — is reported rather than
// swallowed: the file already says the new value, so the honest answer is that the session has not
// taken it yet. The holder keeps the edit, which is what makes a re-committed edit a retry.
func TestApplySettingReportsARefusedRebind(t *testing.T) {
	t.Parallel()
	live := newLiveSettings(config.Options{}, nil)
	probe := &rebindProbe{err: errors.New("input pending")}
	apply := applySettingFor(settingsApplier{
		engine:  &applySettingSpy{},
		live:    live,
		binding: func() upstreamBinding { return upstreamBinding{Model: "bound-model"} },
		rebind:  probe.rebind,
	})

	if _, err := apply("context-window", "16384"); err == nil {
		t.Fatal("apply context-window: want the rebind's refusal, got none")
	}
	if live.pin() != 16384 {
		t.Errorf("pin = %d, want the persisted 16384 kept for the retry", live.pin())
	}
}

// The three `system-prompt-*` keys are ONE prompt (ADR 0023), and `system-prompt-models:` is a map no
// single string spells — so the apply re-READS the block the pane just wrote and lets the rebind
// re-resolve it per model, exactly as startup does. The spec the rebind builds is the assertion: it
// is what the engine is handed.
func TestApplySettingSystemPromptReResolvesFromTheFile(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	roots, err := resolveRoots(home, t.TempDir())
	if err != nil {
		t.Fatalf("resolveRoots: %v", err)
	}
	path := filepath.Join(roots.config, "config.yaml")
	launchOpts := config.Options{SystemPrompt: config.SystemPromptSettings{
		Global: config.PromptSource{Text: "the launch prompt"},
	}}
	live := newLiveSettings(launchOpts, nil)

	// The rebind closure the composition root wires: it re-resolves through the holder, so what the
	// dispatcher installed there is what the spec carries.
	var spec apogee.RebindSpec
	rebind := func(model string, window int, dialect provider.EffortDialect) (tui.RebindResult, error) {
		base, manualIDs, pinnedWindow, outputCap := live.rebindInputs(launchOpts, upstreamBinding{Model: "bound-model"})
		got, _, err := rebindSpecFor(base, roots, manualIDs, model, window, pinnedWindow, outputCap)
		if err != nil {
			return tui.RebindResult{}, err
		}
		got.EffortDialect = dialect
		spec = got
		return tui.RebindResult{Model: got.Model, ContextWindow: got.MaxContextTokens}, nil
	}
	apply := applySettingFor(settingsApplier{
		engine:     &applySettingSpy{},
		live:       live,
		binding:    func() upstreamBinding { return upstreamBinding{Model: "bound-model"} },
		rebind:     rebind,
		configPath: path,
	})

	// What the pane's write left behind, then the apply that follows it.
	writeSettingsFixture(t, path, "system-prompt-text: the edited prompt\n")
	if _, err := apply("system-prompt-text", "the edited prompt"); err != nil {
		t.Fatalf("apply system-prompt-text: %v", err)
	}
	if spec.SystemPrompt != "the edited prompt" {
		t.Errorf("RebindSpec.SystemPrompt = %q, want the re-read %q", spec.SystemPrompt, "the edited prompt")
	}

	// A per-model entry is the same round trip: the map cannot travel as a value, so only the re-read
	// can carry it.
	writeSettingsFixture(t, path, "system-prompt-models:\n  bound-model:\n    system-prompt-text: the per-model prompt\n")
	if _, err := apply("system-prompt-models", ""); err != nil {
		t.Fatalf("apply system-prompt-models: %v", err)
	}
	if spec.SystemPrompt != "the per-model prompt" {
		t.Errorf("RebindSpec.SystemPrompt = %q, want the per-model entry to win", spec.SystemPrompt)
	}

	// Validate-then-commit: a block the file cannot express never displaces a prompt that works.
	writeSettingsFixture(t, path, "system-prompt-text: both\nsystem-prompt-file: both.md\n")
	if _, err := apply("system-prompt-file", "both.md"); err == nil {
		t.Fatal("apply of a contradictory block: want the refusal, got none")
	}
	if _, err := apply("context-window", "0"); err != nil {
		t.Fatalf("re-drive after the refusal: %v", err)
	}
	if spec.SystemPrompt != "the per-model prompt" {
		t.Errorf("RebindSpec.SystemPrompt = %q, want the last GOOD block still installed", spec.SystemPrompt)
	}
}

// A search endpoint is a tool's configuration, not a change of WHICH tools exist — so while
// web_search is registered (the ordinary case: the built-in set always carries it) the apply is a
// write on the tool the registry already holds. The engine hears nothing, which is what lets the
// endpoint move mid-run.
func TestApplySettingWebSearchEndpointMovesTheRegisteredTool(t *testing.T) {
	t.Parallel()
	registry := tools.NewDefaultRegistryWithHost(t.TempDir(), tools.HostTools{})
	spy := &applySettingSpy{}
	apply := applySettingFor(settingsApplier{
		engine: spy,
		tools: newLiveTools(registry, toolSetSpec{},
			func(toolSetSpec) *apogee.ToolRegistry { return apogee.NewToolRegistry() }),
	})

	if _, err := apply("web-search-endpoint", "https://search.example.com/s"); err != nil {
		t.Fatalf("apply web-search-endpoint: %v", err)
	}
	found, ok := registry.Lookup("web_search")
	if !ok {
		t.Fatal("web_search left the registry; the endpoint move must not rebuild the set")
	}
	if _, ok := found.(*tools.WebSearch); !ok {
		t.Fatalf("web_search is a %T; the registry must still hold the tool that was re-pointed", found)
	}
	if spy.drove() != 0 {
		t.Errorf("re-pointing a registered tool drove the engine: %+v", spy)
	}
}

// The swap door is the OTHER case: a set with no web_search to re-point cannot answer the edit in
// place, so the whole set is rebuilt and handed to the engine (ADR 0037 binding F). The rebuilt set
// becomes the live one, which is what makes the NEXT edit an in-place move again — a root still
// looking up tools in the swapped-out registry would be re-pointing an object nothing dispatches to.
func TestApplySettingWebSearchEndpointSwapsWhenTheToolIsAbsent(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	var built []string
	spy := &applySettingSpy{}
	live := newLiveTools(apogee.NewToolRegistry(), toolSetSpec{},
		func(spec toolSetSpec) *apogee.ToolRegistry {
			built = append(built, spec.endpoint)
			return tools.NewDefaultRegistryWithHost(workspace,
				tools.HostTools{WebSearchEndpoint: spec.endpoint, Disabled: spec.disabled})
		})
	apply := applySettingFor(settingsApplier{engine: spy, tools: live})

	if _, err := apply("web-search-endpoint", "https://first.example.com/s"); err != nil {
		t.Fatalf("apply web-search-endpoint: %v", err)
	}
	if want := []string{"https://first.example.com/s"}; !slices.Equal(built, want) {
		t.Fatalf("rebuilds = %v, want %v", built, want)
	}
	if len(spy.swaps) != 1 {
		t.Fatalf("SwapTools calls = %d, want 1: an absent tool is a set-level change", len(spy.swaps))
	}
	if _, ok := spy.swaps[0].Lookup("web_search"); !ok {
		t.Error("the swapped-in registry has no web_search; the rebuild must carry the tool the edit is about")
	}

	// The second edit finds web_search in the set the first one installed, so it moves in place.
	if _, err := apply("web-search-endpoint", "https://second.example.com/s"); err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if len(built) != 1 || len(spy.swaps) != 1 {
		t.Fatalf("the second edit rebuilt again (rebuilds %v, swaps %d); the live set must be the swapped-in one", built, len(spy.swaps))
	}
}

// SwapTools is idle-only, so a run in flight refuses it. The refusal is REPORTED — the row renders it
// over a value the file already carries (binding A) — and the session stays on the set it had, which
// is what makes re-committing the edit a retry rather than a second half-application.
func TestApplySettingWebSearchSwapRefusalKeepsTheOldSet(t *testing.T) {
	t.Parallel()
	old := apogee.NewToolRegistry()
	spy := &applySettingSpy{swapErr: errors.New("input pending: the tool set can only be swapped between runs")}
	live := newLiveTools(old, toolSetSpec{}, func(spec toolSetSpec) *apogee.ToolRegistry {
		return tools.NewDefaultRegistryWithHost(t.TempDir(),
			tools.HostTools{WebSearchEndpoint: spec.endpoint, Disabled: spec.disabled})
	})
	apply := applySettingFor(settingsApplier{engine: spy, tools: live})

	if _, err := apply("web-search-endpoint", "off"); err == nil {
		t.Fatal("apply web-search-endpoint: want the engine's refusal, got none")
	}
	if live.current != old {
		t.Error("a refused swap still became the live set; the session must keep the tools it had")
	}
}

// An EMPTY value is the pane's way of saying the file no longer SETS a key — what a reset of a key
// whose default is unset dispatches — and every such key has to answer it with the built-in default a
// fresh start would have resolved, not with a refusal and not by standing still. Three of the six are
// driven here; the `system-prompt-` pair re-read the file itself (their own tests above) and
// `present.host` takes the identical path present.command does.
func TestApplySettingOnAnEmptyValueResolvesTheBuiltInDefault(t *testing.T) {
	t.Parallel()

	// The endpoint the live set is built from goes back to the one a start-up with no key builds from
	// (`tools.HostTools{}`), which is what resolves to the built-in provider — while the tool itself is
	// re-pointed in place, so clearing the key is no more a set-level change than moving it was.
	t.Run("web-search-endpoint", func(t *testing.T) {
		t.Parallel()
		workspace := t.TempDir()
		registry := tools.NewDefaultRegistryWithHost(workspace,
			tools.HostTools{WebSearchEndpoint: "https://search.example.com/s"})
		spy := &applySettingSpy{}
		live := newLiveTools(registry, toolSetSpec{endpoint: "https://search.example.com/s"},
			func(toolSetSpec) *apogee.ToolRegistry { return apogee.NewToolRegistry() })

		if _, err := applySettingFor(settingsApplier{engine: spy, tools: live})("web-search-endpoint", ""); err != nil {
			t.Fatalf("apply web-search-endpoint on an empty value: %v", err)
		}
		if got := live.built().endpoint; got != "" {
			t.Errorf("the live set is built from %q; want the empty endpoint a fresh start with no key resolves", got)
		}
		if _, ok := registry.Lookup("web_search"); !ok {
			t.Error("web_search left the registry; clearing the endpoint re-points the tool it already holds")
		}
		if spy.drove() != 0 {
			t.Errorf("clearing the endpoint drove the engine: %+v", spy)
		}

		// And where there is no tool to re-point, the rebuild is handed exactly what start-up hands it
		// when the key is absent, rather than the endpoint the session happened to launch on.
		var built []string
		bare := newLiveTools(apogee.NewToolRegistry(), toolSetSpec{endpoint: "https://search.example.com/s"},
			func(spec toolSetSpec) *apogee.ToolRegistry {
				built = append(built, spec.endpoint)
				return tools.NewDefaultRegistryWithHost(workspace,
					tools.HostTools{WebSearchEndpoint: spec.endpoint, Disabled: spec.disabled})
			})
		if _, err := applySettingFor(settingsApplier{engine: &applySettingSpy{}, tools: bare})("web-search-endpoint", ""); err != nil {
			t.Fatalf("apply web-search-endpoint on a set with no web_search: %v", err)
		}
		if want := []string{""}; !slices.Equal(built, want) {
			t.Errorf("rebuilds = %q, want %q", built, want)
		}
	})

	// `editor` answers an emptied key the way it answers every other value: success with nothing to do.
	// The ladder reads the key off a FRESH projection of the file each time an external edit starts, so
	// a removed line is already the $VISUAL/$EDITOR/OS-opener ladder of ADR 0041 by the time the next ⏎
	// asks — and the default refusal would have reported a failure over a change already in force.
	t.Run("editor", func(t *testing.T) {
		t.Parallel()
		spy := &applySettingSpy{}
		note, err := applySettingFor(settingsApplier{engine: spy})("editor", "")
		if err != nil {
			t.Fatalf("apply editor on an empty value: %v", err)
		}
		if note != "" {
			t.Errorf("note = %q, want none: the write itself is the whole of the apply", note)
		}
		if spy.drove() != 0 {
			t.Errorf("clearing editor drove an engine seam: %+v", spy)
		}
	})

	// The ladder is rebuilt from a block with the field CLEARED, which is the block a fresh start
	// resolves from a file that does not set the key: rung 1 goes back to resolving the OS opener.
	t.Run("present.command", func(t *testing.T) {
		t.Parallel()
		var installed []tui.Presentation
		live := newLivePresentation(
			config.PresentSettings{AutoOpen: true, Command: "zed {path}"}, t.TempDir(), "darwin",
			func(string) string { return "" }, // no SSH: a local session, so rung 1 is wired
			func(p tui.Presentation) { installed = append(installed, p) })

		if _, err := applySettingFor(settingsApplier{present: live})("present.command", ""); err != nil {
			t.Fatalf("apply present.command on an empty value: %v", err)
		}
		if len(installed) != 2 || installed[1].Opener == nil {
			t.Fatalf("installed ladders = %+v; want the rebuilt one to still carry the opener", installed)
		}
		if got := installed[1].Opener.CommandOverride; got != "" {
			t.Errorf("the opener still overrides with %q; want none — the OS opener a fresh start wires", got)
		}
	})
}

// The headline of ADR 0037 decision 6: a committed `mcp-servers:` edit DIALS. The new set answers
// first, the whole tool registry is rebuilt around what it advertises and handed to the engine, and
// only then are the connections it replaced torn down — so at no instant is the session without the
// tools of one set or the other.
func TestApplySettingMCPReconnectSwapsTheToolsAndClosesTheOldSessions(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeSettingsFixture(t, path, mcpServersFixture)

	old := &fakeMCPSession{tools: []apogee.Tool{mcpFixtureTool{name: "gone__echo"}}}
	next := &fakeMCPSession{tools: []apogee.Tool{mcpFixtureTool{name: "docs__search"}}}
	var dialled [][]mcp.ServerConfig
	fixture := newMCPFixture(old, "https://search.example.com/s", func(servers []mcp.ServerConfig) (mcpSession, error) {
		dialled = append(dialled, servers)
		return next, nil
	})
	spy := &applySettingSpy{}
	apply := applySettingFor(settingsApplier{engine: spy, tools: fixture.tools, mcp: fixture.set, configPath: path})

	note, err := apply("mcp-servers", "1 server")
	if err != nil || note != "" {
		t.Fatalf("apply mcp-servers = (%q, %v), want a silent success: the servers are connected NOW", note, err)
	}
	if len(dialled) != 1 || len(dialled[0]) != 1 {
		t.Fatalf("dialled = %+v, want exactly one dial of one server", dialled)
	}
	if got := dialled[0][0]; got.Name != "docs" || got.Transport != mcp.TransportStreamableHTTP ||
		got.Endpoint != "https://mcp.example.com/" {
		t.Fatalf("dialled %+v, want the file's own block (docs, streamable-http, mcp.example.com)", got)
	}
	if len(spy.swaps) != 1 {
		t.Fatalf("SwapTools calls = %d, want 1: another server's tools are a change of the SET", len(spy.swaps))
	}
	if _, ok := spy.swaps[0].Lookup("docs__search"); !ok {
		t.Error("the swapped-in registry has none of the new server's tools")
	}
	if _, ok := spy.swaps[0].Lookup("gone__echo"); ok {
		t.Error("the swapped-in registry still carries the old server's tools")
	}
	if !old.closed {
		t.Error("the old sessions are still open; a reconnect that landed leaves no orphan behind it")
	}
	if next.closed {
		t.Error("the sessions the session is now running on were torn down")
	}
	if endpoints := []string{"https://search.example.com/s"}; !slices.Equal(fixture.built, endpoints) {
		t.Errorf("rebuilds = %v, want %v: a rebuild carries the endpoint the session is on", fixture.built, endpoints)
	}
}

// A rebuild is the whole set, so it has to be the set as the session is configured NOW — otherwise a
// reconnect would quietly revert a web-search endpoint edited an hour earlier, which is a key nobody
// touched being changed by a key somebody did.
func TestMCPReconnectRebuildsWithTheEndpointTheSessionIsOn(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeSettingsFixture(t, path, mcpServersFixture)

	fixture := newMCPFixture(&fakeMCPSession{}, "https://launch.example.com/s", func([]mcp.ServerConfig) (mcpSession, error) {
		return &fakeMCPSession{}, nil
	})
	spy := &applySettingSpy{}
	apply := applySettingFor(settingsApplier{engine: spy, tools: fixture.tools, mcp: fixture.set, configPath: path})

	// The fixture's registry has no web_search to re-point, so the endpoint edit goes through the
	// swap door — and the endpoint it installed is what the reconnect must build the next set from.
	if _, err := apply("web-search-endpoint", "https://moved.example.com/s"); err != nil {
		t.Fatalf("apply web-search-endpoint: %v", err)
	}
	if _, err := apply("mcp-servers", "1 server"); err != nil {
		t.Fatalf("apply mcp-servers: %v", err)
	}
	want := []string{"https://moved.example.com/s", "https://moved.example.com/s"}
	if !slices.Equal(fixture.built, want) {
		t.Errorf("rebuilds = %v, want %v — the reconnect rebuilt from the startup endpoint", fixture.built, want)
	}
}

// A set that cannot be reached costs the session nothing: the dial fails before anything has moved,
// the connections it is using stay open, and the row is told both halves of that in one sentence.
func TestApplySettingMCPReconnectKeepsTheOldSessionsWhenTheDialFails(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeSettingsFixture(t, path, mcpServersFixture)

	old := &fakeMCPSession{tools: []apogee.Tool{mcpFixtureTool{name: "serving__echo"}}}
	fixture := newMCPFixture(old, "", func([]mcp.ServerConfig) (mcpSession, error) {
		return nil, errors.New("mcp: connect to server \"docs\": dial: connection refused")
	})
	spy := &applySettingSpy{}
	apply := applySettingFor(settingsApplier{engine: spy, tools: fixture.tools, mcp: fixture.set, configPath: path})

	_, err := apply("mcp-servers", "1 server")
	if err == nil {
		t.Fatal("apply mcp-servers: want the dial's failure reported, got none")
	}
	for _, want := range []string{"reconnect failed", "connection refused", "previous connections kept"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to contain %q", err, want)
		}
	}
	if old.closed {
		t.Error("the sessions that are still serving were torn down by a reconnect that never happened")
	}
	if spy.drove() != 0 || len(fixture.built) != 0 {
		t.Errorf("a failed dial still moved the tool set (swaps %d, rebuilds %v)", len(spy.swaps), fixture.built)
	}
	if names := toolNames(fixture.set.tools()); !slices.Equal(names, []string{"serving__echo"}) {
		t.Errorf("live MCP tools = %v, want the old set still installed", names)
	}
}

// The other failure is the engine's: SwapTools is idle-only, so a reconnect committed mid-run is
// refused. The session keeps the connections and the tools it had — and the sessions dialled for the
// swap that did not happen are torn down rather than left orphaned, exactly as a half-connected set
// is rolled back at startup.
func TestApplySettingMCPReconnectKeepsEverythingWhenTheEngineIsBusy(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeSettingsFixture(t, path, mcpServersFixture)

	old := &fakeMCPSession{tools: []apogee.Tool{mcpFixtureTool{name: "serving__echo"}}}
	next := &fakeMCPSession{tools: []apogee.Tool{mcpFixtureTool{name: "docs__search"}}}
	fixture := newMCPFixture(old, "", func([]mcp.ServerConfig) (mcpSession, error) { return next, nil })
	spy := &applySettingSpy{swapErr: errors.New("input pending: the tool set can only be swapped between runs")}
	apply := applySettingFor(settingsApplier{engine: spy, tools: fixture.tools, mcp: fixture.set, configPath: path})

	_, err := apply("mcp-servers", "1 server")
	if err == nil {
		t.Fatal("apply mcp-servers: want the engine's refusal, got none")
	}
	if !strings.Contains(err.Error(), "previous connections kept") {
		t.Errorf("error = %q, want the row told the old connections stand", err)
	}
	if old.closed {
		t.Error("a refused swap tore down the sessions the run is still using")
	}
	if !next.closed {
		t.Error("the sessions dialled for a swap that was refused are orphaned")
	}
	if names := toolNames(fixture.set.tools()); !slices.Equal(names, []string{"serving__echo"}) {
		t.Errorf("live MCP tools = %v, want the holder back on the set that is serving", names)
	}
}

// `use-project-skills` moves WHICH dirs are skill sources, so the apply re-points the shared Provider
// and re-scans: the flag is not something a catalogue already loaded can answer. One Provider feeds
// the loop and the "/" menu (ADR 0032), so both see the same set the moment the edit lands.
func TestApplySettingUseProjectSkillsRescansTheSources(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	roots, err := resolveRoots(t.TempDir(), workspace)
	if err != nil {
		t.Fatalf("resolveRoots: %v", err)
	}
	writeSkillFixture(t, filepath.Join(workspace, "skills", "project-only"),
		"---\nid: project-only\nsummary: from the bare project folder\n---\nbody")

	provider := skills.NewProvider(skills.Sources{
		Home:             roots.config,
		Workspace:        roots.workspace,
		UseProjectSkills: true,
	})
	if _, ok := provider.Get("project-only"); !ok {
		t.Fatal("the fixture skill is not discovered with the flag on; the test proves nothing")
	}
	apply := applySettingFor(settingsApplier{engine: &applySettingSpy{}, skills: provider, roots: roots})

	if _, err := apply("use-project-skills", "false"); err != nil {
		t.Fatalf("apply use-project-skills=false: %v", err)
	}
	if _, ok := provider.Get("project-only"); ok {
		t.Error("the project skill still resolves with the flag off; the re-scan did not happen")
	}

	if _, err := apply("use-project-skills", "true"); err != nil {
		t.Fatalf("apply use-project-skills=true: %v", err)
	}
	if _, ok := provider.Get("project-only"); !ok {
		t.Error("the project skill did not come back with the flag on again")
	}
}

// A committed `present.` key rebuilds the ladder exactly as startup built it and re-installs it, so
// the presenter the engine captured walks the new rungs from the next presentation (ADR 0037). A
// value the block refuses changes nothing.
func TestApplySettingPresentRebuildsTheLadder(t *testing.T) {
	t.Parallel()
	var installed []tui.Presentation
	live := newLivePresentation(
		config.PresentSettings{AutoOpen: true}, t.TempDir(), "darwin",
		func(string) string { return "" }, // no SSH: a local session, so rungs 1/3
		func(p tui.Presentation) { installed = append(installed, p) })
	apply := applySettingFor(settingsApplier{present: live})

	if len(installed) != 1 || installed[0].Opener == nil {
		t.Fatalf("startup installed %+v; want one ladder carrying the opener", installed)
	}

	if _, err := apply("present.command", "zed {path}"); err != nil {
		t.Fatalf("apply present.command: %v", err)
	}
	if len(installed) != 2 || installed[1].Opener == nil || installed[1].Opener.CommandOverride != "zed {path}" {
		t.Fatalf("after present.command the ladder is %+v; want an opener carrying the override", installed)
	}

	if _, err := apply("present.auto-open", "false"); err != nil {
		t.Fatalf("apply present.auto-open: %v", err)
	}
	if len(installed) != 3 || installed[2].Opener != nil {
		t.Fatalf("after auto-open=false the ladder is %+v; want no opener at all", installed)
	}

	// The block's own validate, run before anything is installed: a port no server could bind is
	// refused here rather than deep inside the first presentation.
	for _, value := range []string{"not a number", "70000"} {
		if _, err := apply("present.port", value); err == nil {
			t.Errorf("present.port=%q was accepted; want the startup check's refusal", value)
		}
	}
	if len(installed) != 3 {
		t.Errorf("a refused value installed a ladder: %+v", installed[3:])
	}
}

// The doc server's listener follows its ADDRESS and nothing else (ADR 0037 binding D): an edit that
// leaves the address alone keeps the bound listener and every URL it issued, while a port change
// closes it — the URLs die with it — and the next presentation binds the new port.
func TestPresentPortEditRebindsTheDocServer(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	doc := filepath.Join(workspace, "report.html")
	if err := os.WriteFile(doc, []byte("<h1>report</h1>"), 0o600); err != nil {
		t.Fatalf("write doc: %v", err)
	}

	var installed []tui.Presentation
	live := newLivePresentation(
		config.PresentSettings{AutoOpen: true}, workspace, "linux",
		// An SSH session: remote, so the ladder wires rung 2 and advertises the server-side address.
		func(name string) string {
			if name == "SSH_CONNECTION" {
				return "10.0.0.1 51000 10.0.0.2 22"
			}
			return ""
		},
		func(p tui.Presentation) { installed = append(installed, p) })
	t.Cleanup(live.close)

	first := installed[0].Docs
	if first == nil {
		t.Fatal("a remote session wired no doc server; want rung 2")
	}
	url, err := first.Serve(doc)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}

	// An edit that does not touch the address: the same server, still bound, still holding its grant.
	if err := live.apply("present.command", "zed {path}"); err != nil {
		t.Fatalf("apply present.command: %v", err)
	}
	if installed[1].Docs != first {
		t.Error("an unrelated edit displaced the doc server; want the bound listener and its URLs kept")
	}
	if _, err := first.Serve(doc); err != nil {
		t.Errorf("serve after an unrelated edit: %v; want the server still running", err)
	}

	port := freePort(t)
	if err := live.apply("present.port", strconv.Itoa(port)); err != nil {
		t.Fatalf("apply present.port: %v", err)
	}
	next := installed[2].Docs
	if next == nil || next == first {
		t.Fatalf("present.port installed %v; want a doc server on the new address", next)
	}
	if _, err := first.Serve(doc); err == nil {
		t.Errorf("the displaced server still serves %q; want it closed with the port change", url)
	}
	moved, err := next.Serve(doc)
	if err != nil {
		t.Fatalf("serve on the new port: %v", err)
	}
	if want := ":" + strconv.Itoa(port) + "/"; !strings.Contains(moved, want) {
		t.Errorf("URL = %q; want it served from %q", moved, want)
	}
}

// freePort reserves an ephemeral port, releases it, and returns it — a port the doc server can bind
// on its own without the test having to know one the machine happens to have free.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve a port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if err := ln.Close(); err != nil {
		t.Fatalf("release the reserved port: %v", err)
	}
	return port
}

// writeSkillFixture writes one SKILL.md into dir, the shape internal/skills discovers.
func writeSkillFixture(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o600); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
}

// The seam is wired at the composition root, over this run's resolved context-file names: without it
// the pane would persist and apply nothing, which is the Driver degrade and not what the binary
// composes (ADR 0031).
func TestRunRootWiresTheLiveApplySeam(t *testing.T) {
	t.Parallel()
	rec := &recordingLauncher{}
	opts := config.Options{
		Endpoint:     "http://127.0.0.1:1111",
		Model:        "fake",
		Mode:         "ask-before",
		Workspace:    t.TempDir(),
		ConfigDir:    t.TempDir(),
		ContextFiles: []string{"AGENTS.md"},
	}
	if err := runRoot(context.Background(), opts, rec.launch); err != nil {
		t.Fatalf("runRoot: %v", err)
	}
	if rec.opts.Settings == nil {
		t.Fatal("tui.Options.Settings is nil; the composition root did not wire the settings host")
	}
	note, err := rec.opts.Settings.Apply("context-files.enable", "true")
	if err != nil {
		t.Fatalf("Settings.Apply: %v", err)
	}
	if note != contextFileNote {
		t.Errorf("note = %q, want %q", note, contextFileNote)
	}
	// The two keys whose seam is a live OBJECT rather than an engine call — the tool registry the
	// root holds and the shared skill Provider — so an unwired member would panic rather than
	// degrade, and this is where the wiring is proved.
	if _, err := rec.opts.Settings.Apply("web-search-endpoint", "off"); err != nil {
		t.Errorf("Settings.Apply(web-search-endpoint): %v", err)
	}
	if _, err := rec.opts.Settings.Apply("use-project-skills", "false"); err != nil {
		t.Errorf("Settings.Apply(use-project-skills): %v", err)
	}
	// And the third live object: the MCP connections. With no servers configured the reconnect dials
	// a dormant set and swaps the registry around it, which is a no-op the session cannot tell from
	// the outside — but an unwired holder would panic here rather than degrade quietly.
	if _, err := rec.opts.Settings.Apply("mcp-servers", "none"); err != nil {
		t.Errorf("Settings.Apply(mcp-servers): %v", err)
	}
	if _, err := rec.opts.Settings.Apply("model-profiles", "1 model profile"); err != nil {
		t.Errorf("Settings.Apply(model-profiles): %v", err)
	}
	// `server` is the key with no dispatcher home BY DESIGN and permanently: its live apply is the
	// picker's own switch (ADR 0037 decision 4), so a value arriving here is a value nothing can do
	// anything with, and the refusal has to name it.
	if _, err := rec.opts.Settings.Apply("server", "anything"); err == nil {
		t.Error("a key with no live seam applied silently; want a refusal naming it")
	}
}

// The anytime-safe mutators are REMEMBERED while the session has no engine and applied the moment one
// is constructed — the SetMode posture, for the same reason: the settings pane is open before a server
// is chosen (ADR 0036 decision 3), and an edit that persisted must not be the only half that happened.
// A key never moved here leaves the Agent on the seed its Config carried.
func TestLateEngineRemembersSettingsMovedBeforeTheBind(t *testing.T) {
	t.Parallel()
	e := newLateEngine(domain.ModeAskBefore, true)

	e.SetBypass(true)
	e.SetCompactionEnabled(false)
	e.SetContextFiles(true, []string{"AGENTS.md"})

	if e.pendingBypass == nil || !*e.pendingBypass {
		t.Errorf("pendingBypass = %v, want true held for the bind", e.pendingBypass)
	}
	if e.pendingCompaction == nil || *e.pendingCompaction {
		t.Errorf("pendingCompaction = %v, want false held for the bind", e.pendingCompaction)
	}
	if e.pendingContextFiles == nil || !e.pendingContextFiles.enable {
		t.Fatalf("pendingContextFiles = %+v, want the pair held for the bind", e.pendingContextFiles)
	}
	if want := []string{"AGENTS.md"}; !slices.Equal(e.pendingContextFiles.names, want) {
		t.Errorf("pending names = %v, want %v", e.pendingContextFiles.names, want)
	}

	// The model profile is remembered on the same terms, though its own door is idle-only: unbound
	// there is no Agent to refuse it, and a bind with no memory of the edit would install the dialect
	// the process started with — which is the "(next launch)" outcome ADR 0037 exists to abolish.
	if err := e.SetProfile(apogee.ModelProfile{ToolCallFormat: apogee.FormatMarkdownFenced}); err != nil {
		t.Fatalf("SetProfile while unbound: %v, want it held for the bind", err)
	}
	if e.pendingProfile == nil || e.pendingProfile.ToolCallFormat != apogee.FormatMarkdownFenced {
		t.Errorf("pendingProfile = %+v, want the edited dialect held for the bind", e.pendingProfile)
	}

	// A holder nothing moved holds nothing: the Agent is then constructed from its Config alone.
	fresh := newLateEngine(domain.ModeAskBefore, true)
	if fresh.pendingBypass != nil || fresh.pendingCompaction != nil || fresh.pendingContextFiles != nil ||
		fresh.pendingProfile != nil {
		t.Errorf("a fresh holder already carries overrides: %+v", fresh)
	}
}

// The remembered profile is installed at the bind, and it is validated there — by the Agent, which is
// the only thing that can build a dialect's parsers. A profile this build cannot read makes the bind
// FAIL, exactly as a config carrying that profile at launch does, and the holder is left free to bind
// again once the human has fixed it: no Agent is ever installed reading replies in a language it
// does not have.
func TestLateEngineBindRefusesAProfileItCannotParse(t *testing.T) {
	t.Parallel()
	engine := newLateEngine(domain.ModeAskBefore, true)
	t.Cleanup(func() { _ = engine.Close() })
	cfg := validCfg(t)

	if err := engine.SetProfile(apogee.ModelProfile{
		Thinking: apogee.ThinkingProfile{Style: "telepathy"},
	}); err != nil {
		t.Fatalf("SetProfile while unbound: %v", err)
	}
	if err := engine.Bind(func() (*apogee.Agent, error) { return apogee.New(cfg) }); err == nil {
		t.Fatal("Bind with an unreadable profile succeeded; want the dialect refused")
	}

	// Left free: the failed bind installed nothing, so a corrected profile still gets its session.
	if err := engine.SetProfile(apogee.ModelProfile{ToolCallFormat: apogee.FormatMarkdownFenced}); err != nil {
		t.Fatalf("SetProfile after the refused bind: %v", err)
	}
	if err := engine.Bind(func() (*apogee.Agent, error) { return apogee.New(cfg) }); err != nil {
		t.Fatalf("Bind after the refused one: %v, want the holder still free", err)
	}
}

// The `servers:` row's live apply reaches one thing the engine actually holds: the fan-out width of
// the server this session is on (ADR 0039 decision 2). A `parallel-agents:` edited in the pane is in
// force in the running session, like every other ADR 0037 key, rather than waiting for the next
// switch — and clearing it hands the width back to what the server itself advertised.
func TestApplySettingServersReResolvesTheParallelAgentsCap(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.yaml")
	write := func(pin string) {
		t.Helper()
		body := "servers:\n  - name: here\n    endpoint: http://127.0.0.1:1111\n" + pin
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write config: %v", err)
		}
	}

	spy := &parallelAgentsSpy{}
	caps := newParallelAgentsCap(spy)
	caps.follow(config.ServerEntry{Name: "here", Endpoint: "http://127.0.0.1:1111", ParallelAgents: 2})
	caps.observe(6) // what this server's own beat reported, which the pin outranks

	apply := applySettingFor(settingsApplier{
		engine:     &applySettingSpy{},
		live:       newLiveSettings(config.Options{}, nil),
		configPath: path,
		caps:       caps,
	})

	write("    parallel-agents: 5\n")
	if _, err := apply("servers", ""); err != nil {
		t.Fatalf("apply servers: %v", err)
	}
	if spy.last() != 5 {
		t.Errorf("installed %v; want the edited pin 5 in force at once", spy.widths)
	}

	write("")
	if _, err := apply("servers", ""); err != nil {
		t.Fatalf("apply servers with the pin removed: %v", err)
	}
	if spy.last() != 6 {
		t.Errorf("installed %v; want the observed 6 back once the pin is gone", spy.widths)
	}
}

// The `servers:` row's live apply reaches the bound entry's window pin as well as its fan-out width:
// a `context-window:` edited on the entry this session is ON is an ADR 0037 key like every other, so
// the latch the next rebind resolves against re-derives from the re-read list rather than keeping
// what the file said at the last move. Clearing it hands the window back to the top-level key.
func TestApplySettingServersReResolvesTheBoundEntrysContextWindow(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.yaml")
	write := func(pin string) {
		t.Helper()
		body := "servers:\n  - name: here\n    endpoint: http://127.0.0.1:1111\n" + pin
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write config: %v", err)
		}
	}

	// The holder as a startup bind onto `here` leaves it: the entry's own 32,768 latched over the
	// top-level 16,384.
	live := newLiveSettings(config.Options{
		ContextWindow: 16384, HostAlias: "here", StartupContextWindow: 32768,
	}, nil)
	if got := live.window(); got != 32768 {
		t.Fatalf("the bound window = %d; want the startup entry's 32768", got)
	}
	apply := applySettingFor(settingsApplier{live: live, configPath: path})

	write("    context-window: 65536\n")
	if _, err := apply("servers", ""); err != nil {
		t.Fatalf("apply servers: %v", err)
	}
	if _, _, pin, _ := live.rebindInputs(config.Options{}, upstreamBinding{}); pin != 65536 {
		t.Errorf("the next rebind's pin = %d; want the edited 65536 — the latch went stale", pin)
	}

	write("")
	if _, err := apply("servers", ""); err != nil {
		t.Fatalf("apply servers with the pin removed: %v", err)
	}
	if _, _, pin, _ := live.rebindInputs(config.Options{}, upstreamBinding{}); pin != 16384 {
		t.Errorf("the next rebind's pin = %d; want the top-level 16384 back once the entry pins nothing", pin)
	}

	// A list that no longer names this session's server leaves the pin exactly where it was: the
	// file stopped describing the server, which is not the server changing.
	live.followEntry(config.ServerEntry{Name: "here", ContextWindow: 65536})
	if err := os.WriteFile(path, []byte("servers:\n  - name: elsewhere\n    endpoint: http://127.0.0.1:2222\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if _, err := apply("servers", ""); err != nil {
		t.Fatalf("apply servers naming another entry: %v", err)
	}
	if _, _, pin, _ := live.rebindInputs(config.Options{}, upstreamBinding{}); pin != 65536 {
		t.Errorf("the next rebind's pin = %d; want the bound entry's 65536 kept", pin)
	}
}

// Re-deriving the latch is only half of an apply: a latch is read at the NEXT rebind, so an edited
// window would describe the session from whenever the next beat happens to drive one — seconds or
// minutes away — while the top-level `context-window:` key edited on the row above is in force the
// moment it commits. So a `servers:` edit that moves the bound entry's window rides the rebind too,
// through the same door and with the same result: the spec the engine is handed carries the edited
// number without a beat of its own. Clearing the entry's pin is the same act — the window it hands
// back to the top-level key is in force at once, not at the next observation.
func TestApplySettingServersRidesTheRebindForTheBoundEntrysWindow(t *testing.T) {
	t.Parallel()

	roots, err := resolveRoots(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("resolveRoots: %v", err)
	}
	path := filepath.Join(roots.config, "config.yaml")
	write := func(pin string) {
		t.Helper()
		body := "servers:\n  - name: here\n    endpoint: http://127.0.0.1:1111\n" + pin
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write config: %v", err)
		}
	}

	// The holder as a startup bind onto `here` leaves it: the entry's own 32,768 over the top-level
	// 16,384, and a beat that has since reported what the server itself advertises.
	launchOpts := config.Options{ContextWindow: 16384, HostAlias: "here", StartupContextWindow: 32768}
	live := newLiveSettings(launchOpts, nil)
	live.observe(131072, provider.EffortDialectNone)

	// The rebind closure the composition root wires: it re-resolves through the holder, so the spec
	// it builds is what the engine would be handed.
	var spec apogee.RebindSpec
	drives := 0
	rebind := func(model string, window int, dialect provider.EffortDialect) (tui.RebindResult, error) {
		drives++
		base, manualIDs, pinnedWindow, outputCap := live.rebindInputs(launchOpts, upstreamBinding{Model: model})
		got, _, err := rebindSpecFor(base, roots, manualIDs, model, window, pinnedWindow, outputCap)
		if err != nil {
			return tui.RebindResult{}, err
		}
		got.EffortDialect = dialect
		spec = got
		return tui.RebindResult{Model: got.Model, ContextWindow: got.MaxContextTokens}, nil
	}
	apply := applySettingFor(settingsApplier{
		engine:     &applySettingSpy{},
		live:       live,
		binding:    func() upstreamBinding { return upstreamBinding{Model: "bound-model"} },
		rebind:     rebind,
		configPath: path,
	})

	write("    context-window: 65536\n")
	if _, err := apply("servers", ""); err != nil {
		t.Fatalf("apply servers: %v", err)
	}
	if drives != 1 {
		t.Fatalf("rebind drives = %d, want 1: the edited window must not wait for the next beat", drives)
	}
	if spec.MaxContextTokens != 65536 {
		t.Errorf("RebindSpec.MaxContextTokens = %d; want the edited 65536 in force at once",
			spec.MaxContextTokens)
	}

	write("")
	if _, err := apply("servers", ""); err != nil {
		t.Fatalf("apply servers with the pin removed: %v", err)
	}
	if drives != 2 {
		t.Fatalf("rebind drives = %d, want 2: dropping the pin moves the window as much as adding one", drives)
	}
	if spec.MaxContextTokens != 16384 {
		t.Errorf("RebindSpec.MaxContextTokens = %d; want the top-level 16384 back at once rather than "+
			"the observed 131072", spec.MaxContextTokens)
	}
}

// The other side of that ride: a `servers:` edit that leaves this session's window exactly where it
// was must not rebind for it. A rebind re-resolves every per-model binding, resets the token
// estimator and the compaction latch, and is idle-only — so driving one for an entry the session is
// not on would refuse mid-Exchange to install numbers nothing changed. The list still installs; only
// the ride is conditional, and the condition is the RESOLVED window rather than the entry's field.
func TestApplySettingServersDoesNotRebindForAnEditThatMovesNoWindow(t *testing.T) {
	t.Parallel()

	const bound = "servers:\n  - name: here\n    endpoint: http://127.0.0.1:1111\n"
	tests := []struct {
		name      string
		entryPin  int
		list      string
		wantNames []string
		wantPin   int
	}{
		{
			name:      "an edit to an entry this session is not on",
			entryPin:  32768,
			list:      bound + "    context-window: 32768\n  - name: elsewhere\n    endpoint: http://127.0.0.1:2222\n    context-window: 131072\n",
			wantNames: []string{"here", "elsewhere"},
			wantPin:   32768,
		},
		{
			name:      "another key on the bound entry's own block",
			entryPin:  32768,
			list:      bound + "    context-window: 32768\n    parallel-agents: 5\n",
			wantNames: []string{"here"},
			wantPin:   32768,
		},
		{
			name:      "a pin dropped onto a top-level key that already said the same",
			entryPin:  16384,
			list:      bound,
			wantNames: []string{"here"},
			wantPin:   16384,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(tt.list), 0o600); err != nil {
				t.Fatalf("write config: %v", err)
			}
			live := newLiveSettings(config.Options{
				ContextWindow: 16384, HostAlias: "here", StartupContextWindow: tt.entryPin,
			}, nil)
			probe := &rebindProbe{}
			apply := applySettingFor(settingsApplier{
				engine:     &applySettingSpy{},
				live:       live,
				binding:    func() upstreamBinding { return upstreamBinding{Model: "bound-model"} },
				rebind:     probe.rebind,
				configPath: path,
			})

			if _, err := apply("servers", ""); err != nil {
				t.Fatalf("apply servers: %v", err)
			}
			if len(probe.calls) != 0 {
				t.Errorf("rebind drives = %+v, want none: this edit moved no bound this session holds",
					probe.calls)
			}
			var names []string
			for _, e := range live.serverList() {
				names = append(names, e.Name)
			}
			if !slices.Equal(names, tt.wantNames) {
				t.Errorf("installed list = %v, want %v: the list applies whether or not a ride does",
					names, tt.wantNames)
			}
			if _, _, pin, _ := live.rebindInputs(config.Options{}, upstreamBinding{}); pin != tt.wantPin {
				t.Errorf("the next rebind's pin = %d; want the unchanged %d", pin, tt.wantPin)
			}
		})
	}
}

// The reply ceiling beside that window is the other half of one ride, and it was the half that
// waited: `RebindSpec` carried no ceiling at all, so a `max-output-tokens:` edited on the bound entry
// re-derived a latch nothing read and reached the engine only at the next bind or `/server` move —
// while the window, edited in the same block of the same file, was in force the moment it committed.
// It rides now, on the same spec through the same door: the ceiling the engine states on the wire is
// the number the file says, without a beat of its own. Dropping the pin is the same act — the 0 the
// engine reads as "derive the cap from the reply budget again" is in force at once, which is why the
// spec's field is a pointer and this resolver always fills it in.
func TestApplySettingServersRidesTheRebindForTheBoundEntrysReplyCap(t *testing.T) {
	t.Parallel()

	roots, err := resolveRoots(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("resolveRoots: %v", err)
	}
	path := filepath.Join(roots.config, "config.yaml")
	write := func(pin string) {
		t.Helper()
		body := "servers:\n  - name: here\n    endpoint: http://127.0.0.1:1111\n" + pin
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write config: %v", err)
		}
	}

	// The holder as a startup bind onto `here` leaves it: that entry's own 2,048-token ceiling, the
	// top-level window key, and a beat that has since reported what the server advertises.
	launchOpts := config.Options{ContextWindow: 16384, HostAlias: "here", StartupMaxOutputTokens: 2048}
	live := newLiveSettings(launchOpts, nil)
	live.observe(131072, provider.EffortDialectNone)

	// The rebind closure the composition root wires: it re-resolves through the holder, so the spec it
	// builds is what the engine would be handed.
	var spec apogee.RebindSpec
	drives := 0
	rebind := func(model string, window int, dialect provider.EffortDialect) (tui.RebindResult, error) {
		drives++
		base, manualIDs, pinnedWindow, outputCap := live.rebindInputs(launchOpts, upstreamBinding{Model: model})
		got, _, err := rebindSpecFor(base, roots, manualIDs, model, window, pinnedWindow, outputCap)
		if err != nil {
			return tui.RebindResult{}, err
		}
		got.EffortDialect = dialect
		spec = got
		return tui.RebindResult{Model: got.Model, ContextWindow: got.MaxContextTokens}, nil
	}
	apply := applySettingFor(settingsApplier{
		engine:     &applySettingSpy{},
		live:       live,
		binding:    func() upstreamBinding { return upstreamBinding{Model: "bound-model"} },
		rebind:     rebind,
		configPath: path,
	})

	write("    max-output-tokens: 8192\n")
	if _, err := apply("servers", ""); err != nil {
		t.Fatalf("apply servers: %v", err)
	}
	if drives != 1 {
		t.Fatalf("rebind drives = %d, want 1: the edited ceiling must not wait for the next bind", drives)
	}
	if spec.MaxOutputTokens == nil {
		t.Fatalf("RebindSpec.MaxOutputTokens is nil; want the edited ceiling stated on the spec")
	}
	if *spec.MaxOutputTokens != 8192 {
		t.Errorf("RebindSpec.MaxOutputTokens = %d; want the edited 8192 in force at once",
			*spec.MaxOutputTokens)
	}
	// The window is untouched throughout, which is what makes this the CAP's own arm of the ride
	// condition rather than the window's arm firing for it.
	if spec.MaxContextTokens != 16384 {
		t.Errorf("RebindSpec.MaxContextTokens = %d; want the unmoved top-level 16384", spec.MaxContextTokens)
	}

	write("")
	if _, err := apply("servers", ""); err != nil {
		t.Fatalf("apply servers with the ceiling removed: %v", err)
	}
	if drives != 2 {
		t.Fatalf("rebind drives = %d, want 2: dropping the pin moves the ceiling as much as adding one", drives)
	}
	if spec.MaxOutputTokens == nil || *spec.MaxOutputTokens != 0 {
		t.Errorf("RebindSpec.MaxOutputTokens = %v; want the stated 0 that hands the ceiling back to the "+
			"engine's own derivation", spec.MaxOutputTokens)
	}
}

// And the other side of the cap's ride, the guard the window's own no-ride table states for the
// window: a `servers:` edit that leaves this session's ceiling exactly where it was must not rebind
// for it. The list still installs; only the ride is conditional, and the condition is the ceiling
// this session RESOLVES — which for the cap is the bound entry's own field, since ADR 0046 grew no
// top-level key to fall back to.
func TestApplySettingServersDoesNotRebindForACapEditThatMovesNothing(t *testing.T) {
	t.Parallel()

	const bound = "servers:\n  - name: here\n    endpoint: http://127.0.0.1:1111\n"
	tests := []struct {
		name     string
		entryCap int
		list     string
		wantCap  int
	}{
		{
			name:     "a ceiling edited on an entry this session is not on",
			entryCap: 2048,
			list: bound + "    max-output-tokens: 2048\n  - name: elsewhere\n" +
				"    endpoint: http://127.0.0.1:2222\n    max-output-tokens: 32768\n",
			wantCap: 2048,
		},
		{
			name:     "a ceiling restating the number already in force",
			entryCap: 2048,
			list:     bound + "    max-output-tokens: 2048\n",
			wantCap:  2048,
		},
		{
			name:     "an entry that pinned no ceiling and still pins none",
			entryCap: 0,
			list:     bound + "    parallel-agents: 5\n",
			wantCap:  0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(tt.list), 0o600); err != nil {
				t.Fatalf("write config: %v", err)
			}
			live := newLiveSettings(config.Options{
				HostAlias: "here", StartupMaxOutputTokens: tt.entryCap,
			}, nil)
			probe := &rebindProbe{}
			apply := applySettingFor(settingsApplier{
				engine:     &applySettingSpy{},
				live:       live,
				binding:    func() upstreamBinding { return upstreamBinding{Model: "bound-model"} },
				rebind:     probe.rebind,
				configPath: path,
			})

			if _, err := apply("servers", ""); err != nil {
				t.Fatalf("apply servers: %v", err)
			}
			if len(probe.calls) != 0 {
				t.Errorf("rebind drives = %+v, want none: this edit moved no bound this session holds",
					probe.calls)
			}
			if len(live.serverList()) == 0 {
				t.Error("the re-read list was not installed: the list applies whether or not a ride does")
			}
			if _, _, _, outputCap := live.rebindInputs(config.Options{}, upstreamBinding{}); outputCap != tt.wantCap {
				t.Errorf("the next rebind's ceiling = %d; want the unchanged %d", outputCap, tt.wantCap)
			}
		})
	}
}

// The share that DIVIDES that window is the third bound the bound entry decides, and it was the last
// one still waiting: the re-read installed it on the latch, but `RebindSpec` carried no share, so a
// `response-reserve:` edited on the entry this session is on reached the engine only at the next bind,
// `/server` move or scheduled Firing — while the window and the ceiling edited in the same block of
// the same file were in force the moment they committed. It rides now, on the same spec through the
// same door, and dropping the override is the same act: the stated 0 that hands the split back to
// apogee's own default is in force at once, which is why the spec's field is a pointer and this
// resolver always fills it in.
func TestApplySettingServersRidesTheRebindForTheBoundEntrysResponseReserve(t *testing.T) {
	t.Parallel()

	roots, err := resolveRoots(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("resolveRoots: %v", err)
	}
	path := filepath.Join(roots.config, "config.yaml")
	write := func(share string) {
		t.Helper()
		body := "servers:\n  - name: here\n    endpoint: http://127.0.0.1:1111\n" + share
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write config: %v", err)
		}
	}

	// The holder as a startup bind onto `here` leaves it: that entry's own quarter-window share, the
	// top-level window key, and a beat that has since reported what the server advertises.
	launchOpts := config.Options{ContextWindow: 16384, HostAlias: "here", StartupResponseReserve: 0.25}
	live := newLiveSettings(launchOpts, nil)
	live.observe(131072, provider.EffortDialectNone)

	// The rebind closure the composition root wires: it re-resolves through the holder, so the spec it
	// builds is what the engine would be handed.
	var spec apogee.RebindSpec
	drives := 0
	rebind := func(model string, window int, dialect provider.EffortDialect) (tui.RebindResult, error) {
		drives++
		base, manualIDs, pinnedWindow, outputCap := live.rebindInputs(launchOpts, upstreamBinding{Model: model})
		got, _, err := rebindSpecFor(base, roots, manualIDs, model, window, pinnedWindow, outputCap)
		if err != nil {
			return tui.RebindResult{}, err
		}
		got.EffortDialect = dialect
		spec = got
		return tui.RebindResult{Model: got.Model, ContextWindow: got.MaxContextTokens}, nil
	}
	apply := applySettingFor(settingsApplier{
		engine:     &applySettingSpy{},
		live:       live,
		binding:    func() upstreamBinding { return upstreamBinding{Model: "bound-model"} },
		rebind:     rebind,
		configPath: path,
	})

	write("    response-reserve: 0.35\n")
	if _, err := apply("servers", ""); err != nil {
		t.Fatalf("apply servers: %v", err)
	}
	if drives != 1 {
		t.Fatalf("rebind drives = %d, want 1: the edited share must not wait for the next bind", drives)
	}
	if spec.ResponseReserveFraction == nil {
		t.Fatalf("RebindSpec.ResponseReserveFraction is nil; want the edited share stated on the spec")
	}
	if *spec.ResponseReserveFraction != 0.35 {
		t.Errorf("RebindSpec.ResponseReserveFraction = %v; want the edited 0.35 in force at once",
			*spec.ResponseReserveFraction)
	}
	// The other two bounds are untouched throughout, which is what makes this the SHARE's own arm of
	// the ride condition rather than the window's or the ceiling's firing for it.
	if spec.MaxContextTokens != 16384 {
		t.Errorf("RebindSpec.MaxContextTokens = %d; want the unmoved top-level 16384", spec.MaxContextTokens)
	}
	if spec.MaxOutputTokens == nil || *spec.MaxOutputTokens != 0 {
		t.Errorf("RebindSpec.MaxOutputTokens = %v; want the unmoved 0 this entry never pinned",
			spec.MaxOutputTokens)
	}

	write("")
	if _, err := apply("servers", ""); err != nil {
		t.Fatalf("apply servers with the share removed: %v", err)
	}
	if drives != 2 {
		t.Fatalf("rebind drives = %d, want 2: dropping the share moves it as much as stating one", drives)
	}
	if spec.ResponseReserveFraction == nil || *spec.ResponseReserveFraction != 0 {
		t.Errorf("RebindSpec.ResponseReserveFraction = %v; want the stated 0 that hands the split back "+
			"to apogee's own default share", spec.ResponseReserveFraction)
	}
}

// And the other side of the share's ride, the guard the two bounds beside it already state: a
// `servers:` edit that leaves this session's split exactly where it was must not rebind for it. The
// list still installs; only the ride is conditional.
func TestApplySettingServersDoesNotRebindForAReserveEditThatMovesNothing(t *testing.T) {
	t.Parallel()

	const bound = "servers:\n  - name: here\n    endpoint: http://127.0.0.1:1111\n"
	tests := []struct {
		name         string
		entryReserve float64
		list         string
		wantReserve  float64
	}{
		{
			name:         "a share edited on an entry this session is not on",
			entryReserve: 0.25,
			list: bound + "    response-reserve: 0.25\n  - name: elsewhere\n" +
				"    endpoint: http://127.0.0.1:2222\n    response-reserve: 0.4\n",
			wantReserve: 0.25,
		},
		{
			name:         "a share restating the number already in force",
			entryReserve: 0.25,
			list:         bound + "    response-reserve: 0.25\n",
			wantReserve:  0.25,
		},
		{
			name:         "an entry that stated no share and still states none",
			entryReserve: 0,
			list:         bound + "    parallel-agents: 5\n",
			wantReserve:  0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(tt.list), 0o600); err != nil {
				t.Fatalf("write config: %v", err)
			}
			live := newLiveSettings(config.Options{
				HostAlias: "here", StartupResponseReserve: tt.entryReserve,
			}, nil)
			probe := &rebindProbe{}
			apply := applySettingFor(settingsApplier{
				engine:     &applySettingSpy{},
				live:       live,
				binding:    func() upstreamBinding { return upstreamBinding{Model: "bound-model"} },
				rebind:     probe.rebind,
				configPath: path,
			})

			if _, err := apply("servers", ""); err != nil {
				t.Fatalf("apply servers: %v", err)
			}
			if len(probe.calls) != 0 {
				t.Errorf("rebind drives = %+v, want none: this edit moved no bound this session holds",
					probe.calls)
			}
			if len(live.serverList()) == 0 {
				t.Error("the re-read list was not installed: the list applies whether or not a ride does")
			}
			base, _, _, _ := live.rebindInputs(config.Options{}, upstreamBinding{})
			if base.ResponseReserve != tt.wantReserve {
				t.Errorf("the next rebind's share = %v; want the unchanged %v",
					base.ResponseReserve, tt.wantReserve)
			}
		})
	}
}

// The TOP-LEVEL `response-reserve:` key is the deliberate counter-case to the ride above: the pane
// offers it (a registry row like any other) and the write puts it in the file, but nothing applies it
// to a session already running — the holder latches it at construction and grows no setter, because
// the share a session divides its window by belongs to the server it is BOUND to and the entry
// override is the live door. So the key is start-up only, and the apply is the write: no rebind is
// driven, no engine seam moves, and the share the next rebind resolves is exactly the one the
// session launched with until a next start reads the file again.
//
// What it must NOT do is refuse. The file changed, and it changed to exactly what the key promises
// in its Description — a refusal would report a failed apply over a save that worked, which is the
// row TestApplySettingAcceptsTheStartupOnlyKeys pins the silence of.
func TestApplySettingSavesTheTopLevelResponseReserveWithoutMovingTheSession(t *testing.T) {
	t.Parallel()

	live := newLiveSettings(config.Options{HostAlias: "here", StartupResponseReserve: 0.25}, nil)
	probe := &rebindProbe{}
	spy := &applySettingSpy{}
	apply := applySettingFor(settingsApplier{
		engine:     spy,
		live:       live,
		binding:    func() upstreamBinding { return upstreamBinding{Model: "bound-model"} },
		rebind:     probe.rebind,
		configPath: filepath.Join(t.TempDir(), "config.yaml"),
	})

	note, err := apply("response-reserve", "0.35")
	if err != nil {
		t.Fatalf("apply response-reserve: %v; the write is the whole of the apply, not a failure", err)
	}
	if note != "" {
		t.Errorf("note = %q, want none: nothing about this session changed to report", note)
	}
	if len(probe.calls) != 0 {
		t.Errorf("a start-up-only key still drove a rebind: %+v", probe.calls)
	}
	if spy.drove() != 0 {
		t.Errorf("a start-up-only key still drove an engine seam: %+v", spy)
	}
	base, _, _, _ := live.rebindInputs(config.Options{}, upstreamBinding{})
	if base.ResponseReserve != 0.25 {
		t.Errorf("the next rebind's share = %v; want the launch share 0.25, which no write can move",
			base.ResponseReserve)
	}
}

// The seed the `/settings` prompt editor opens on, and the one condition under which there is one:
// the whole GLOBAL prompt is empty. `system-prompt-file` set beside a seeded text field would make
// the very first ctrl+s commit a config the next resolution refuses — both keys set is an error —
// so a file-only config seeds nothing and stays the config that works today.
func TestPromptEditorSeedAnswersOnlyAnEmptyGlobalPrompt(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		global config.PromptSource
		want   string
	}{
		{"nothing configured", config.PromptSource{}, config.DefaultSystemPrompt()},
		{"an inline prompt", config.PromptSource{Text: "mine\n"}, ""},
		{"a prompt file", config.PromptSource{File: "prompt.md"}, ""},
		{"both keys", config.PromptSource{Text: "mine\n", File: "prompt.md"}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			live := newLiveSettings(config.Options{
				SystemPrompt: config.SystemPromptSettings{Global: c.global},
			}, nil)
			if got := live.promptEditorSeed(); got != c.want {
				t.Errorf("promptEditorSeed() = %q; want %q", got, c.want)
			}
		})
	}

	// The answer is the SESSION's, not the launch snapshot's: a prompt installed mid-session stops
	// the seeding from the moment it lands, which is what makes the row feed safe to re-ask per paint.
	live := newLiveSettings(config.Options{}, nil)
	live.setSystemPrompt(config.SystemPromptSettings{Global: config.PromptSource{File: "prompt.md"}}, true)
	if got := live.promptEditorSeed(); got != "" {
		t.Errorf("after a mid-session prompt file the seed is %q; want none", got)
	}
}

// Saving what the editor was seeded with persists it: the seed is display-only until ctrl+s, and
// from there it is explicit config like any other prompt. The embedded default is a multi-line
// template full of `{{placeholders}}`, so the block-scalar writer has to carry it back byte for byte
// — a prompt that came back re-indented or re-wrapped would be a prompt the human never wrote.
func TestSeededPromptPersistsThroughTheSettingsWrite(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	path := filepath.Join(home, "config.yaml")
	if err := config.SaveConfigSetting(path, "system-prompt-text", config.DefaultSystemPrompt()); err != nil {
		t.Fatalf("save the seeded prompt: %v", err)
	}

	resolved := config.Options{ConfigDir: home}
	err := config.ApplyConfig(&resolved, func(string) bool { return false },
		func(string) string { return "" }, os.ReadFile, func(string) {})
	var undetermined *config.StartupUndetermined
	if err != nil && !errors.As(err, &undetermined) {
		t.Fatalf("ApplyConfig: %v", err)
	}
	if resolved.SystemPrompt.Global.Text != config.DefaultSystemPrompt() {
		t.Errorf("the saved prompt reads back as %q; want the embedded default verbatim",
			resolved.SystemPrompt.Global.Text)
	}
}
