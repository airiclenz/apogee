package main

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/profiles"
	"github.com/airiclenz/apogee/internal/tui"
)

// fabricatedSettings is a resolved config with a DISTINCT value in every key, so a row that reads
// the wrong field of options shows the wrong string rather than coincidentally the right one. The
// values are deliberately not the defaults: `api-key` is set (so masking has something to hide),
// `cursor-shape` is left unset (so the default-fallback rule has a subject), and `mechanisms`
// carries an explicit `false` entry (so counting keys instead of enabled ones would overcount).
func fabricatedSettings() config.Options {
	return config.Options{
		Endpoint:      "http://192.168.64.1:1111",
		APIKey:        "sk-my-server-token",
		HostAlias:     "workstation",
		Model:         "gpt-oss-20b",
		Servers:       []config.ServerEntry{{Name: "workstation"}, {Name: "rented-box"}, {Name: "laptop"}},
		StartupServer: "rented-box",
		Editor:        "code -w",
		Mode:          "auto",
		SystemPrompt: config.SystemPromptSettings{
			Global: config.PromptSource{Text: "You are apogee.\nAnswer with code first.\n"},
			Models: map[string]config.PromptSource{"gpt-oss-20b": {File: "~/prompts/gpt-oss.md"}},
		},
		ContextFiles:       []string{"AGENTS.md", "CLAUDE.md"},
		ConfineToWorkspace: false,
		UnconfinedHosts:    []config.UnconfinedHost{{ID: "host-1"}},
		WebSearchEndpoint:  "off",
		ToolsDisabled:      []string{"view_diff"},
		URLAllowHosts:      []string{"docs.example.com"},
		URLDenyHosts:       nil, // the honest asymmetry: a configured allow list beside an unset deny list
		UseProjectSkills:   false,
		AutoCompact:        true,
		DelegateMaxSteps:   40,
		AutoTitle:          false,
		RememberModel:      true,
		ContextWindow:      32768,
		WorkingWindow:      16384,
		ResponseReserve:    0.35,
		Present:            config.PresentSettings{AutoOpen: true, Command: "zed {path}", Port: 8080},
		UI: config.UISettings{Spinner: tui.SpinnerGlitter, SpinnerColor: true, ShowScrollbar: false,
			ColorScheme: "dark", StallAfter: 2 * time.Minute, Inspector: true},
		Bypass:              true,
		Mechanisms:          map[string]bool{"validate": true, "syntax": true, "autofix": false},
		ValidatedSetsEnable: true,
		ValidatedSetsAlias:  map[string]string{"gpt-oss-20b": "gpt-oss"},
		ModelProfiles: []profiles.Entry{
			{Pattern: "minimax-m3", Profile: apogee.ModelProfile{
				Thinking: apogee.ThinkingProfile{Style: apogee.ThinkingDelimited, Start: "<mm:think>", End: "</mm:think>"},
			}},
		},
		Overrides: map[string]config.Source{"mode": config.SourceFlag, "server": config.SourceEnv},
	}
}

// rowsByPath indexes built rows for the assertions that name one key.
func rowsByPath(t *testing.T, rows []tui.SettingRow) map[string]tui.SettingRow {
	t.Helper()
	byPath := make(map[string]tui.SettingRow, len(rows))
	for _, r := range rows {
		byPath[r.Path] = r
	}
	return byPath
}

// The pane renders the rows top to bottom, so their ORDER is the registry's order — which is the
// config template's order — and their COUNT is the registry's count. A key that fell out of the
// projection would be a key the user cannot see, which is the whole failure mode the registry
// exists to prevent.
func TestSettingsRowsMatchTheRegistryOrder(t *testing.T) {
	t.Parallel()

	rows := settingsRows(fabricatedSettings())
	if len(rows) != len(config.KeyRegistry) {
		t.Fatalf("settingsRows returned %d rows; want one per registry key (%d)", len(rows), len(config.KeyRegistry))
	}
	for i, k := range config.KeyRegistry {
		if rows[i].Path != k.Path {
			t.Errorf("row %d is %q; want the registry's %q — the pane paints rows in this order",
				i, rows[i].Path, k.Path)
		}
	}
}

// fakeRunningSettings is the engine's half of the live overlay: the two facts a running session
// holds and the config file does not. A struct rather than the holder itself, which is the whole
// point of the narrow interface — the overlay is provable with no Agent, no endpoint and no wiring.
type fakeRunningSettings struct {
	mode    apogee.Mode
	confine bool
}

func (f fakeRunningSettings) Mode() apogee.Mode        { return f.mode }
func (f fakeRunningSettings) ConfineToWorkspace() bool { return f.confine }

// The two rows the ENGINE holds report what the session is running, not what it launched with:
// Shift+Tab and the mode row move the first, /confine moves the second, and neither writes the
// config file — so rows projected from the boot resolution alone told the user about a session that
// had moved on, and marked a rung it had left as the one to re-apply (F-14, F-31).
//
// Every OTHER row is left byte-identical, which is what makes this an overlay rather than a second
// projection: a key that started reading the engine by accident would be a row nobody wrote.
func TestSettingsRowsOverlayTheLiveModeAndConfinement(t *testing.T) {
	t.Parallel()

	opts := fabricatedSettings()
	opts.Mode = string(apogee.ModePlan)
	opts.ConfineToWorkspace = true
	boot := settingsRows(opts)
	if got := rowsByPath(t, boot)[settingKeyMode].Value; got != string(apogee.ModePlan) {
		t.Fatalf("boot mode row = %q, want plan — the overlay has nothing to prove otherwise", got)
	}
	if got := rowsByPath(t, boot)[settingKeyConfineToWorkspace].Value; got != "true" {
		t.Fatalf("boot confinement row = %q, want true", got)
	}

	live := overlayLiveSettings(settingsRows(opts),
		fakeRunningSettings{mode: apogee.ModeAuto, confine: false})

	if len(live) != len(boot) {
		t.Fatalf("overlaid rows = %d, want the projection's %d", len(live), len(boot))
	}
	for i := range boot {
		want := boot[i].Value
		switch boot[i].Path {
		case settingKeyMode:
			want = string(apogee.ModeAuto)
		case settingKeyConfineToWorkspace:
			want = "false"
		}
		if live[i].Value != want {
			t.Errorf("%s row = %q, want %q", boot[i].Path, live[i].Value, want)
		}
		// Only the VALUE may move: the section, the source marker, the pointer and the prose are
		// the file's answers whatever the engine is running.
		restored := live[i]
		restored.Value = boot[i].Value
		if !reflect.DeepEqual(restored, boot[i]) {
			t.Errorf("%s row = %+v, want %+v apart from its value", boot[i].Path, live[i], boot[i])
		}
	}
}

// A settings host with no engine behind it (a Driver that composed one, ADR 0031) overlays nothing
// and shows the file's own answers, which is what "there is nothing to ask" honestly reads as.
func TestSettingsRowsWithoutALiveEngineOverlayNothing(t *testing.T) {
	t.Parallel()

	opts := fabricatedSettings()
	if got := overlayLiveSettings(settingsRows(opts), nil); !reflect.DeepEqual(got, settingsRows(opts)) {
		t.Errorf("overlaid rows differ from the projection with no engine to ask")
	}
}

// The text row carries BOTH halves: the summary a row has space for, and the prose the editor opens
// on. They are read in different places and neither stands in for the other.
func TestSettingsRowsCarryThePromptTextBesideItsSummary(t *testing.T) {
	t.Parallel()

	opts := fabricatedSettings()
	opts.SystemPrompt = config.SystemPromptSettings{Global: config.PromptSource{Text: "one\ntwo\nthree\n"}}
	row := rowsByPath(t, settingsRows(opts))["system-prompt-text"]
	if row.Kind != tui.SettingText {
		t.Errorf("row kind = %q; want %q", row.Kind, tui.SettingText)
	}
	if row.Value != "3 lines" {
		t.Errorf("row value = %q; want the line summary", row.Value)
	}
	if row.Text != "one\ntwo\nthree\n" {
		t.Errorf("row text = %q; want the prompt itself", row.Text)
	}

	opts.SystemPrompt = config.SystemPromptSettings{}
	blank := rowsByPath(t, settingsRows(opts))["system-prompt-text"]
	if blank.Value != "" || blank.Text != "" {
		t.Errorf("an unset prompt reads {%q %q}; want both blank — the row seeds a field, so no word "+
			"stands in for emptiness", blank.Value, blank.Text)
	}
}

// Sections are runs over the registry's order, so every opener must exist, they must appear in
// registry order, and the FIRST registry key must open one — otherwise the rows above the first
// opener would carry no section at all.
func TestSettingSectionsOpenInRegistryOrder(t *testing.T) {
	t.Parallel()

	paths := make([]string, 0, len(config.KeyRegistry))
	for _, k := range config.KeyRegistry {
		paths = append(paths, k.Path)
	}
	previous := -1
	for _, section := range settingSections {
		at := slices.Index(paths, section.Opens)
		if at < 0 {
			t.Errorf("section %q opens at %q, which is not a registry key", section.Name, section.Opens)
			continue
		}
		if at <= previous {
			t.Errorf("section %q opens at registry index %d, at or before the previous section's %d — "+
				"sections are runs over the registry order", section.Name, at, previous)
		}
		previous = at
		if section.Name == "" {
			t.Errorf("section opening at %q has no name", section.Opens)
		}
	}
	if len(settingSections) == 0 || settingSections[0].Opens != config.KeyRegistry[0].Path {
		t.Errorf("the first registry key %q opens no section, so the rows above the first opener "+
			"would have none", config.KeyRegistry[0].Path)
	}
}

// Each row carries the section it sits under, and the section changes only where a section opens —
// which is what lets the pane insert one header row per run rather than per key.
func TestSettingsRowsCarryTheirSection(t *testing.T) {
	t.Parallel()

	rows := settingsRows(fabricatedSettings())
	opensAt := map[string]string{}
	for _, section := range settingSections {
		opensAt[section.Opens] = section.Name
	}
	current := ""
	for _, r := range rows {
		if name, opens := opensAt[r.Path]; opens {
			current = name
		}
		if r.Section == "" {
			t.Errorf("row %q carries no section", r.Path)
		}
		if r.Section != current {
			t.Errorf("row %q is in section %q; want %q", r.Path, r.Section, current)
		}
	}

	byPath := rowsByPath(t, rows)
	for path, want := range map[string]string{
		"servers":               "Upstream",
		"mode":                  "Autonomy",
		"context-files.names":   "System prompt",
		"unconfined-hosts":      "Confinement",
		"use-project-skills":    "Tools & skills",
		"context-window":        "Session",
		"present.host":          "Presentation",
		"cursor-shape":          "Interface",
		"editor":                "Interface",
		"validated-sets.enable": "Mechanisms",
		"validated-sets.alias":  "Mechanisms",
		"model-profiles":        "Model profiles",
	} {
		if got := byPath[path].Section; got != want {
			t.Errorf("row %q is in section %q; want %q", path, got, want)
		}
	}
}

// The effective value of every key, spelled the way the config file spells it — including the three
// central rules: a masked secret, a "none" for an empty structured block, and the declared default
// standing in for an empty value the default is in force for (`cursor-shape`).
func TestSettingsRowsFormatEffectiveValues(t *testing.T) {
	t.Parallel()

	byPath := rowsByPath(t, settingsRows(fabricatedSettings()))
	want := map[string]string{
		"servers":                "3 servers",
		"server":                 "rented-box",
		"mode":                   "auto",
		"system-prompt-text":     "2 lines",
		"system-prompt-file":     "", // unset, and an editable string row's blank is what the field seeds from
		"system-prompt-models":   "1 model",
		"context-files.enable":   "true",
		"context-files.names":    "[AGENTS.md, CLAUDE.md]",
		"confine-to-workspace":   "false",
		"unconfined-hosts":       "1 host",
		"web-search-endpoint":    "off",
		"mcp-servers":            noneSettingValue,
		"tools.disabled":         "[view_diff]",
		"tools.enabled":          "[]", // unset: nothing is added back, which is the whole default menu
		"url-safety.allow-hosts": "[docs.example.com]",
		"url-safety.deny-hosts":  "[]", // unset: a list row's empty spelling, and every host is still floored
		"use-project-skills":     "false",
		"auto-compact":           "true",
		"delegate-max-steps":     "40",
		"auto-title":             "false",
		"remember-model":         "true",
		"context-window":         "32768",
		"working-window":         "16384",
		"response-reserve":       "0.35", // the shortest spelling that reads back as the same share
		"present.auto-open":      "true",
		"present.command":        "zed {path}",
		"present.port":           "8080",
		"present.host":           "",
		"ui.spinner":             "glitter",
		"ui.spinner-color":       "true",
		"ui.show-scrollbar":      "false",
		"ui.color-scheme":        "dark",
		"ui.stall-after":         "2m0s",  // a duration prints itself, and the printing is a spelling the key takes back
		"ui.inspector":           "true",  // armed for THIS run, which is the only thing a startup-only key can report
		"cursor-shape":           "block", // unset, so the declared default is what is in force
		"editor":                 "code -w",
		"bypass":                 "true",
		"mechanisms":             "2 mechanisms", // the explicit `false` entry is not an enabled one
		"validated-sets.enable":  "true",
		"validated-sets.alias":   "1 alias",
		"model-profiles":         "1 model profile",
	}
	for path, wantValue := range want {
		if got := byPath[path].Value; got != wantValue {
			t.Errorf("row %q value = %q; want %q", path, got, wantValue)
		}
	}
	if len(want) != len(config.KeyRegistry) {
		t.Errorf("this table pins %d values for %d registry keys — pin the new key's value too",
			len(want), len(config.KeyRegistry))
	}
}

// The secret never crosses the seam. Since ADR 0036 the schema's only api key belongs to a
// `servers:` entry — a structured block the pane summarizes as a count — so no row may carry the
// token in any form, and the `servers` row in particular must render its count and nothing else.
func TestSettingsRowsNeverCarryAnAPIKey(t *testing.T) {
	t.Parallel()

	opts := fabricatedSettings()
	for _, r := range settingsRows(opts) {
		if strings.Contains(r.Value, opts.APIKey) {
			t.Errorf("row %q leaks the api key in its value %q", r.Path, r.Value)
		}
	}
	if got := rowsByPath(t, settingsRows(opts))["servers"].Value; got != "3 servers" {
		t.Errorf("servers row value = %q; want the count — an entry's fields never reach the pane", got)
	}
}

// A key an environment variable or a flag overrode is marked, and the marker NAMES the source, so
// the pane can say which variable is standing in front of the file. Everything else reports the
// file — the ordinary case, which carries no name.
func TestSettingsRowsMarkOverriddenKeys(t *testing.T) {
	t.Parallel()

	byPath := rowsByPath(t, settingsRows(fabricatedSettings()))
	if got := byPath["mode"]; got.Source != tui.SettingFromFlag || got.SourceName != "--mode" {
		t.Errorf("mode row source = {%q %q}; want the flag marker {%q %q}",
			got.Source, got.SourceName, tui.SettingFromFlag, "--mode")
	}
	if got := byPath["server"]; got.Source != tui.SettingFromEnv || got.SourceName != config.EnvServer {
		t.Errorf("server row source = {%q %q}; want the env marker {%q %q}",
			got.Source, got.SourceName, tui.SettingFromEnv, config.EnvServer)
	}
	for _, path := range []string{"servers", "bypass", "auto-compact"} {
		got := byPath[path]
		if got.Source != tui.SettingFromFile || got.SourceName != "" {
			t.Errorf("row %q source = {%q %q}; want the unmarked file source", path, got.Source, got.SourceName)
		}
	}
}

// A row this pane will not write says where the key IS edited: the human's own editor for a
// structured block, opened on that key's line (ADR 0037 decision 5), and /confine for the two
// confinement keys, whose acknowledgement interlock stays single-homed there (ADR 0012). An editable
// row carries no pointer — the two fields are exact opposites, which is what lets the pane branch on
// either one — and the ExternalEdit flag names the same set the $EDITOR pointer does, since the
// affordance and its wording are one predicate (externallyEdited).
func TestSettingsRowsPointReadOnlyKeysAtTheirEditor(t *testing.T) {
	t.Parallel()

	rows := settingsRows(fabricatedSettings())
	for _, r := range rows {
		if r.Editable != (r.EditPointer == "") {
			t.Errorf("row %q: editable=%v with pointer %q — a pointer is carried exactly when the "+
				"pane will not write the key", r.Path, r.Editable, r.EditPointer)
		}
		if r.Editable && r.Kind == tui.SettingStructured {
			t.Errorf("row %q is editable but structured — nothing in the pane can edit a block", r.Path)
		}
		if r.ExternalEdit != (r.EditPointer == pointerExternalEdit) {
			t.Errorf("row %q: externalEdit=%v with pointer %q — the flag and the wording name one set",
				r.Path, r.ExternalEdit, r.EditPointer)
		}
	}
	byPath := rowsByPath(t, rows)
	for _, path := range []string{"confine-to-workspace", "unconfined-hosts"} {
		if got := byPath[path].EditPointer; got != pointerConfine {
			t.Errorf("row %q pointer = %q; want %q — the interlock stays in /confine", path, got, pointerConfine)
		}
		if byPath[path].ExternalEdit {
			t.Errorf("row %q opens $EDITOR; the confinement interlock is single-homed in /confine", path)
		}
	}
	// mechanisms is the one structured block the pane opens ITSELF, in a list of switches, so its
	// pointer names that list and its $EDITOR affordance is off — the two facts the predicate above
	// already requires of each other, pinned here as the wording a human reads on the row.
	if got := byPath["mechanisms"].EditPointer; got != pointerMechanismList {
		t.Errorf("row %q pointer = %q; want %q — its children are switches the pane holds",
			"mechanisms", got, pointerMechanismList)
	}
	if byPath["mechanisms"].ExternalEdit {
		t.Errorf("row %q opens $EDITOR; ⏎ opens the Mechanism list instead", "mechanisms")
	}
	// system-prompt-text is NOT among them since it became editable in its own multi-line field: the
	// prose the file carries as a block is written in the pane now (tui.SettingText).
	for _, path := range []string{"servers", "mcp-servers", "system-prompt-models", "model-profiles"} {
		if got := byPath[path].EditPointer; got != pointerExternalEdit {
			t.Errorf("row %q pointer = %q; want %q", path, got, pointerExternalEdit)
		}
		if !byPath[path].ExternalEdit {
			t.Errorf("row %q carries the $EDITOR pointer but not the affordance", path)
		}
	}
	if got := byPath["mode"].EditPointer; got != "" {
		t.Errorf("editable row %q carries pointer %q; want none", "mode", got)
	}
	if byPath["mode"].ExternalEdit {
		t.Errorf("editable row %q opens $EDITOR; it is written on the row", "mode")
	}
}

// The rest of each row is the registry row, projected: the kind (with it the edit idiom), the enum
// vocabulary, the default, the editability and the description. The projection is asserted against
// the table rather than restated, so a registry edit lands in the pane without a second edit here.
func TestSettingsRowsProjectRegistryMetadata(t *testing.T) {
	t.Parallel()

	byPath := rowsByPath(t, settingsRows(fabricatedSettings()))
	for _, k := range config.KeyRegistry {
		row := byPath[k.Path]
		if want := settingKind(k.Kind); row.Kind != want {
			t.Errorf("row %q kind = %q; want %q", k.Path, row.Kind, want)
		}
		if !reflect.DeepEqual(row.EnumValues, k.EnumValues) {
			t.Errorf("row %q enum values = %v; want %v", k.Path, row.EnumValues, k.EnumValues)
		}
		if row.Default != k.Default || row.Editable != k.Editable || row.Masked != k.Masked ||
			row.Desc != k.Desc {
			t.Errorf("row %q does not carry its registry row: %+v", k.Path, row)
		}
	}
	// One kind is pinned at ROW level too, because the loop above cannot speak for it: its kind
	// clause computes the expectation with settingKind itself, so only a literal named here says
	// what the pane must show for the registry's one float row.
	if got := byPath["response-reserve"].Kind; got != tui.SettingInt {
		t.Errorf("row %q kind = %q; want %q — a share is typed into the same caret buffer an int uses",
			"response-reserve", got, tui.SettingInt)
	}
	// The kinds themselves are a closed projection, and only a structured key reads as structured
	// (the read-only end the fallback lands on). Every edge is stated as a literal here, so the
	// table is a second opinion on the projection rather than a restatement of it. Three edges are
	// many-to-one, all deliberate: a name list is typed on its row exactly as a string is, so it
	// keeps its list-ness on the writer's side of the seam (KindStringList → SettingString); a share
	// is typed into the same caret buffer an int opens, and there is no tui.SettingFloat
	// (KindFloat → SettingInt); and a scheme is picked from a list exactly as an enum value is, only
	// with the list coming from the session (KindScheme → SettingEnum).
	projection := map[config.Kind]tui.SettingKind{
		config.KindBool:       tui.SettingBool,
		config.KindInt:        tui.SettingInt,
		config.KindString:     tui.SettingString,
		config.KindStringList: tui.SettingString,
		config.KindFloat:      tui.SettingInt,
		config.KindText:       tui.SettingText,
		config.KindEnum:       tui.SettingEnum,
		config.KindServer:     tui.SettingServer,
		config.KindScheme:     tui.SettingEnum,
		config.KindStructured: tui.SettingStructured,
	}
	for kind, want := range projection {
		if got := settingKind(kind); got != want {
			t.Errorf("settingKind(%q) = %q; want %q", kind, got, want)
		}
	}
	// A kind some key USES but the table does not state would be an edge nobody pins — the
	// uncovered set the table above closed. The registry is the only enumeration of the kinds in
	// use, so it is what the guard walks.
	for _, k := range config.KeyRegistry {
		if _, ok := projection[k.Kind]; !ok {
			t.Errorf("registry key %q is kind %q, which the projection table does not state — a kind "+
				"in use is pinned here, not left to settingKind's fallback", k.Path, k.Kind)
		}
	}
}

// The structured summaries the pane shows instead of a YAML fragment, at the sizes that read
// differently: none, one, and several.
func TestSettingsRowsSummarizeStructuredBlocks(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		mut  func(*config.Options)
		path string
		want string
	}{
		{
			name: "an empty block reads none",
			mut:  func(o *config.Options) { o.Servers = nil },
			path: "servers",
			want: noneSettingValue,
		},
		{
			name: "one entry is singular",
			mut:  func(o *config.Options) { o.Servers = []config.ServerEntry{{Name: "workstation"}} },
			path: "servers",
			want: "1 server",
		},
		{
			// The off-switch is its OWN row now, so it is spelled the way the file spells it rather
			// than folded into a summary of the block — and it is the one row of the pair the pane writes.
			name: "the validated-set off-switch is a bool row of its own",
			mut:  func(o *config.Options) { o.ValidatedSetsEnable = false },
			path: "validated-sets.enable",
			want: "false",
		},
		{
			name: "the alias map is counted, and an empty one reads none",
			mut:  func(o *config.Options) { o.ValidatedSetsAlias = nil },
			path: "validated-sets.alias",
			want: noneSettingValue,
		},
		{
			// The count and nothing else: which pattern applies depends on the model that is bound
			// (ADR 0044), which no row of a config surface knows.
			name: "the profile map is counted, whatever shape its entries give a model",
			mut: func(o *config.Options) {
				o.ModelProfiles = append(o.ModelProfiles, profiles.Entry{
					Pattern: "gemma",
					Profile: apogee.ModelProfile{ToolCallFormat: apogee.FormatMarkdownFenced},
				})
			},
			path: "model-profiles",
			want: "2 model profiles",
		},
		{
			name: "context files off resolves to an empty list, not to none",
			mut:  func(o *config.Options) { o.ContextFiles = nil },
			path: "context-files.names",
			want: "[]",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			opts := fabricatedSettings()
			tc.mut(&opts)
			if got := rowsByPath(t, settingsRows(opts))[tc.path].Value; got != tc.want {
				t.Errorf("row %q value = %q; want %q", tc.path, got, tc.want)
			}
		})
	}
}
