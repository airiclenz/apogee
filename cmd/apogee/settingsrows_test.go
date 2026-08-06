package main

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/tui"
)

// fabricatedSettings is a resolved config with a DISTINCT value in every key, so a row that reads
// the wrong field of options shows the wrong string rather than coincidentally the right one. The
// values are deliberately not the defaults: `api-key` is set (so masking has something to hide),
// `cursor-shape` is left unset (so the default-fallback rule has a subject), and `mechanisms`
// carries an explicit `false` entry (so counting keys instead of enabled ones would overcount).
func fabricatedSettings() options {
	return options{
		endpoint:      "http://192.168.64.1:1111",
		apiKey:        "sk-my-server-token",
		hostAlias:     "workstation",
		model:         "gpt-oss-20b",
		servers:       []serverEntry{{Name: "workstation"}, {Name: "rented-box"}, {Name: "laptop"}},
		startupServer: "rented-box",
		llamaLauncher: "off",
		mode:          "auto",
		systemPrompt: systemPromptSettings{
			global: promptSource{text: "You are apogee.\nAnswer with code first.\n"},
			models: map[string]promptSource{"gpt-oss-20b": {file: "~/prompts/gpt-oss.md"}},
		},
		contextFiles:        []string{"AGENTS.md", "CLAUDE.md"},
		confineToWorkspace:  false,
		unconfinedHosts:     []unconfinedHost{{ID: "host-1"}},
		webSearchEndpoint:   "off",
		useProjectSkills:    false,
		autoCompact:         true,
		autoTitle:           false,
		contextWindow:       32768,
		present:             presentSettings{autoOpen: true, command: "zed {path}", port: 8080},
		ui:                  uiSettings{spinner: tui.SpinnerGlitter, spinnerColor: true, showScrollbar: false},
		bypass:              true,
		mechanisms:          map[string]bool{"validate": true, "syntax": true, "autofix": false},
		validatedSetsEnable: true,
		validatedSetsAlias:  map[string]string{"gpt-oss-20b": "gpt-oss"},
		profile:             apogee.ModelProfile{},
		overrides:           map[string]configSource{"mode": sourceFlag, "server": sourceEnv},
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
	if len(rows) != len(keyRegistry) {
		t.Fatalf("settingsRows returned %d rows; want one per registry key (%d)", len(rows), len(keyRegistry))
	}
	for i, k := range keyRegistry {
		if rows[i].Path != k.Path {
			t.Errorf("row %d is %q; want the registry's %q — the pane paints rows in this order",
				i, rows[i].Path, k.Path)
		}
	}
}

// Every registry key has a value formatter, and no formatter names a path the registry does not
// have. This is the projection's own anti-drift guard, one level above the schema bijection: a key
// added to the registry without a formatter would render as an honest-looking blank row.
func TestSettingValuesCoverEveryRegistryKey(t *testing.T) {
	t.Parallel()

	for _, k := range keyRegistry {
		if _, ok := settingValues[k.Path]; !ok {
			t.Errorf("registry key %q has no settingValues formatter — /settings would show it blank", k.Path)
		}
	}
	for path := range settingValues {
		if _, ok := lookupKey(path); !ok {
			t.Errorf("settingValues formats %q, which the registry does not describe (renamed or removed key?)", path)
		}
	}
}

// The same guard for the second, tiny table: a kindText row whose prose nothing projects would open
// its editor on an empty field and offer to overwrite the prompt with what was typed into it, and a
// projection for a key that is not text would be prose no surface reads.
func TestSettingTextsCoverEveryTextKey(t *testing.T) {
	t.Parallel()

	for _, k := range keyRegistry {
		if _, ok := settingTexts[k.Path]; ok != (k.Kind == kindText) {
			t.Errorf("registry key %q is kind %q but settingTexts projects it = %v — the raw value is "+
				"carried for exactly the text keys", k.Path, k.Kind, ok)
		}
	}
	for path := range settingTexts {
		if _, ok := lookupKey(path); !ok {
			t.Errorf("settingTexts projects %q, which the registry does not describe (renamed or removed key?)", path)
		}
	}
}

// The text row carries BOTH halves: the summary a row has space for, and the prose the editor opens
// on. They are read in different places and neither stands in for the other.
func TestSettingsRowsCarryThePromptTextBesideItsSummary(t *testing.T) {
	t.Parallel()

	opts := fabricatedSettings()
	opts.systemPrompt = systemPromptSettings{global: promptSource{text: "one\ntwo\nthree\n"}}
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

	opts.systemPrompt = systemPromptSettings{}
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

	paths := make([]string, 0, len(keyRegistry))
	for _, k := range keyRegistry {
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
	if len(settingSections) == 0 || settingSections[0].Opens != keyRegistry[0].Path {
		t.Errorf("the first registry key %q opens no section, so the rows above the first opener "+
			"would have none", keyRegistry[0].Path)
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
		"servers":             "Upstream",
		"llama-launcher":      "Upstream",
		"mode":                "Autonomy",
		"context-files.names": "System prompt",
		"unconfined-hosts":    "Confinement",
		"use-project-skills":  "Tools & skills",
		"context-window":      "Session",
		"present.host":        "Presentation",
		"cursor-shape":        "Interface",
		"validated-sets":      "Mechanisms",
		"model-profile":       "Model profile",
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
		"servers":              "3 servers",
		"server":               "rented-box",
		"llama-launcher":       "off",
		"mode":                 "auto",
		"system-prompt-text":   "2 lines",
		"system-prompt-file":   "", // unset, and an editable string row's blank is what the field seeds from
		"system-prompt-models": "1 model",
		"context-files.enable": "true",
		"context-files.names":  "[AGENTS.md, CLAUDE.md]",
		"confine-to-workspace": "false",
		"unconfined-hosts":     "1 host",
		"web-search-endpoint":  "off",
		"mcp-servers":          noneSettingValue,
		"use-project-skills":   "false",
		"auto-compact":         "true",
		"auto-title":           "false",
		"context-window":       "32768",
		"present.auto-open":    "true",
		"present.command":      "zed {path}",
		"present.port":         "8080",
		"present.host":         "",
		"ui.spinner":           "glitter",
		"ui.spinner-color":     "true",
		"ui.show-scrollbar":    "false",
		"cursor-shape":         "block", // unset, so the declared default is what is in force
		"bypass":               "true",
		"mechanisms":           "2 mechanisms", // the explicit `false` entry is not an enabled one
		"validated-sets":       "on, 1 alias",
		"model-profile":        "native",
	}
	for path, wantValue := range want {
		if got := byPath[path].Value; got != wantValue {
			t.Errorf("row %q value = %q; want %q", path, got, wantValue)
		}
	}
	if len(want) != len(keyRegistry) {
		t.Errorf("this table pins %d values for %d registry keys — pin the new key's value too",
			len(want), len(keyRegistry))
	}
}

// The secret never crosses the seam. Since ADR 0036 the schema's only api key belongs to a
// `servers:` entry — a structured block the pane summarizes as a count — so no row may carry the
// token in any form, and the `servers` row in particular must render its count and nothing else.
func TestSettingsRowsNeverCarryAnAPIKey(t *testing.T) {
	t.Parallel()

	opts := fabricatedSettings()
	for _, r := range settingsRows(opts) {
		if strings.Contains(r.Value, opts.apiKey) {
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
	if got := byPath["server"]; got.Source != tui.SettingFromEnv || got.SourceName != envServer {
		t.Errorf("server row source = {%q %q}; want the env marker {%q %q}",
			got.Source, got.SourceName, tui.SettingFromEnv, envServer)
	}
	for _, path := range []string{"servers", "llama-launcher", "bypass", "auto-compact"} {
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
	// system-prompt-text is NOT among them since it became editable in its own multi-line field: the
	// prose the file carries as a block is written in the pane now (tui.SettingText).
	for _, path := range []string{"servers", "mcp-servers", "mechanisms", "system-prompt-models", "model-profile"} {
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
	for _, k := range keyRegistry {
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
	// The kinds themselves are a closed projection, and only a structured key reads as structured
	// (the read-only end the fallback lands on). The one many-to-one is deliberate: a name list is
	// typed on its row exactly as a string is, so it projects onto the string idiom and keeps its
	// list-ness on the writer's side of the seam (settingKind).
	for kind, want := range map[configKind]tui.SettingKind{
		kindBool:       tui.SettingBool,
		kindInt:        tui.SettingInt,
		kindString:     tui.SettingString,
		kindStringList: tui.SettingString,
		kindEnum:       tui.SettingEnum,
		kindServer:     tui.SettingServer,
		kindStructured: tui.SettingStructured,
	} {
		if got := settingKind(kind); got != want {
			t.Errorf("settingKind(%q) = %q; want %q", kind, got, want)
		}
	}
}

// The structured summaries the pane shows instead of a YAML fragment, at the sizes that read
// differently: none, one, and several.
func TestSettingsRowsSummarizeStructuredBlocks(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		mut  func(*options)
		path string
		want string
	}{
		{
			name: "an empty block reads none",
			mut:  func(o *options) { o.servers = nil },
			path: "servers",
			want: noneSettingValue,
		},
		{
			name: "one entry is singular",
			mut:  func(o *options) { o.servers = []serverEntry{{Name: "workstation"}} },
			path: "servers",
			want: "1 server",
		},
		{
			name: "the validated-set off-switch leads its summary",
			mut:  func(o *options) { o.validatedSetsEnable = false },
			path: "validated-sets",
			want: "off",
		},
		{
			name: "a configured profile names its format and thinking style",
			mut: func(o *options) {
				o.profile = apogee.ModelProfile{
					ToolCallFormat: apogee.FormatMarkdownFenced,
					Thinking:       apogee.ThinkingProfile{Style: apogee.ThinkingHarmony},
				}
			},
			path: "model-profile",
			want: string(apogee.FormatMarkdownFenced) + ", thinking " + string(apogee.ThinkingHarmony),
		},
		{
			name: "context files off resolves to an empty list, not to none",
			mut:  func(o *options) { o.contextFiles = nil },
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
