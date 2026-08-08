package scheme

import (
	"fmt"
	"strings"
	"testing"
)

// roleTable ties every YAML key to the Scheme field it must land in, spelled out by
// hand. scheme.go derives that mapping from the struct tags by reflection; restating it
// here independently is what makes a crossed wire (two roles swapping fields) a test
// failure rather than a surprise on screen.
var roleTable = []struct {
	key string
	get func(Scheme) string
}{
	{"user-text", func(s Scheme) string { return s.UserText }},
	{"chrome", func(s Scheme) string { return s.Chrome }},
	{"divider", func(s Scheme) string { return s.Divider }},
	{"surface", func(s Scheme) string { return s.Surface }},
	{"muted", func(s Scheme) string { return s.Muted }},
	{"muted-bright", func(s Scheme) string { return s.MutedBright }},
	{"diff-add", func(s Scheme) string { return s.DiffAdd }},
	{"diff-del", func(s Scheme) string { return s.DiffDel }},
	{"error", func(s Scheme) string { return s.Error }},
	{"code", func(s Scheme) string { return s.Code }},
	{"tool-header", func(s Scheme) string { return s.ToolHeader }},
	{"mode-plan", func(s Scheme) string { return s.ModePlan }},
	{"mode-ask-before", func(s Scheme) string { return s.ModeAskBefore }},
	{"mode-allow-edits", func(s Scheme) string { return s.ModeAllowEdits }},
	{"mode-auto", func(s Scheme) string { return s.ModeAuto }},
	{"skill", func(s Scheme) string { return s.Skill }},
	{"file-ref", func(s Scheme) string { return s.FileRef }},
	{"prompt-toggle", func(s Scheme) string { return s.PromptToggle }},
	{"tool-marker", func(s Scheme) string { return s.ToolMarker }},
	{"gauge", func(s Scheme) string { return s.Gauge }},
	{"selection", func(s Scheme) string { return s.Selection }},
	{"spinner-1", func(s Scheme) string { return s.Spinner1 }},
	{"spinner-2", func(s Scheme) string { return s.Spinner2 }},
	{"spinner-3", func(s Scheme) string { return s.Spinner3 }},
	{"spinner-4", func(s Scheme) string { return s.Spinner4 }},
}

// darkPalette pins the shipped "dark" values. It is the drift guard between
// schemes/dark.yaml and the palette apogee shipped with (internal/tui/theme.go): change
// one without the other and this fails.
var darkPalette = map[string]string{
	"user-text":        "#ffffff",
	"chrome":           "#4a4a4a",
	"divider":          "#333333",
	"surface":          "#000000",
	"muted":            "#8a8a8a",
	"muted-bright":     "#b2b2b2",
	"diff-add":         "#3fb950",
	"diff-del":         "#f85149",
	"error":            "#f85149",
	"code":             "#80AAFF",
	"tool-header":      "#f0883e",
	"mode-plan":        "#2afefa",
	"mode-ask-before":  "#3fb950",
	"mode-allow-edits": "#58a6ff",
	"mode-auto":        "#f0883e",
	"skill":            "#b1baff",
	"file-ref":         "#cdffa4",
	"prompt-toggle":    "#b0d2ff",
	"tool-marker":      "#FFB050",
	"gauge":            "#c396ff",
	"selection":        "#3a5fcd",
	"spinner-1":        "#8668ff",
	"spinner-2":        "#19a946",
	"spinner-3":        "#ffbf00",
	"spinner-4":        "#ff4a81",
}

// yamlFor renders a scheme file from key/value pairs, quoting values the way a real
// scheme file must (an unquoted # opens a YAML comment).
func yamlFor(t *testing.T, values map[string]string) []byte {
	t.Helper()
	var b strings.Builder
	for _, role := range roleTable {
		v, ok := values[role.key]
		if !ok {
			continue
		}
		fmt.Fprintf(&b, "%s: %q\n", role.key, v)
	}
	return []byte(b.String())
}

func TestRoleTableCoversEveryRole(t *testing.T) {
	t.Parallel()
	if len(roleTable) != len(roleKeys) {
		t.Fatalf("roleTable has %d roles, Scheme declares %d", len(roleTable), len(roleKeys))
	}
	for i, role := range roleTable {
		if roleKeys[i] != role.key {
			t.Errorf("role %d: Scheme declares %q, roleTable says %q", i, roleKeys[i], role.key)
		}
	}
	if len(darkPalette) != len(roleKeys) {
		t.Errorf("darkPalette pins %d roles, Scheme declares %d", len(darkPalette), len(roleKeys))
	}
}

func TestParseFullFileRoundTrips(t *testing.T) {
	t.Parallel()
	// One distinct value per role, so a field that takes another role's value shows up.
	want := make(map[string]string, len(roleTable))
	for i, role := range roleTable {
		want[role.key] = fmt.Sprintf("#0000%02x", i+1)
	}

	got, warnings := Parse("full.yaml", yamlFor(t, want))
	if len(warnings) != 0 {
		t.Fatalf("a complete valid file warned: %v", warnings)
	}
	for _, role := range roleTable {
		if got := role.get(got); got != want[role.key] {
			t.Errorf("key %q: got %q, want %q", role.key, got, want[role.key])
		}
	}
}

func TestParsePartialFileInheritsDefaults(t *testing.T) {
	t.Parallel()
	const wantError, wantGauge = "#123456", "#abcdef"
	got, warnings := Parse("partial.yaml", yamlFor(t, map[string]string{"error": wantError, "gauge": wantGauge}))
	if len(warnings) != 0 {
		t.Fatalf("an omitted key warned: %v — omission is the intended way to write a partial scheme", warnings)
	}
	if got.Error != wantError || got.Gauge != wantGauge {
		t.Fatalf("stated keys not applied: error=%q gauge=%q", got.Error, got.Gauge)
	}
	def := Default()
	for _, role := range roleTable {
		if role.key == "error" || role.key == "gauge" {
			continue
		}
		if role.get(got) != role.get(def) {
			t.Errorf("key %q: got %q, want the default %q", role.key, role.get(got), role.get(def))
		}
	}
}

func TestParseDefectiveValuesKeepDefaults(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		body       string
		wantKey    string
		wantReason string
	}{
		{"bad hex", "error: \"#zz0000\"\n", "error", `bad hex "#zz0000" — using default`},
		{"short hex", "error: \"#f00\"\n", "error", `bad hex "#f00" — using default`},
		{"not a color", "error: chartreuse\n", "error", `bad hex "chartreuse" — using default`},
		{"not a scalar", "error:\n  fg: \"#ff0000\"\n", "error", "expected a #rrggbb color — using default"},
		{"unknown key", "banana: \"#ffff00\"\n", "banana", "unknown color role — ignored"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, warnings := Parse("broken.yaml", []byte(tc.body))
			if got != Default() {
				t.Errorf("a defective key changed the scheme: %+v", got)
			}
			if len(warnings) != 1 {
				t.Fatalf("got %d warnings, want 1: %v", len(warnings), warnings)
			}
			w := warnings[0]
			if w.File != "broken.yaml" || w.Key != tc.wantKey || w.Reason != tc.wantReason {
				t.Fatalf("got %+v, want file=broken.yaml key=%q reason=%q", w, tc.wantKey, tc.wantReason)
			}
			want := fmt.Sprintf("color-scheme %q: key %q: %s", "broken.yaml", tc.wantKey, tc.wantReason)
			if w.String() != want {
				t.Errorf("String() = %q, want %q", w.String(), want)
			}
		})
	}
}

func TestParseUnreadableFileFallsBackToDefault(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
	}{
		{"malformed", "error: [unclosed\n"},
		{"not a mapping", "- error\n- gauge\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, warnings := Parse("junk.yaml", []byte(tc.body))
			if got != Default() {
				t.Errorf("got %+v, want the default scheme", got)
			}
			if len(warnings) != 1 {
				t.Fatalf("got %d warnings, want 1: %v", len(warnings), warnings)
			}
			if w := warnings[0]; w.Key != "" || !strings.Contains(w.String(), `color-scheme "junk.yaml":`) {
				t.Errorf("whole-file warning reads %q, want it to name the file and no key", w.String())
			}
		})
	}
}

func TestParseEmptyFileKeepsDefaultsSilently(t *testing.T) {
	t.Parallel()
	got, warnings := Parse("empty.yaml", []byte("# only a comment\n"))
	if got != Default() {
		t.Errorf("got %+v, want the default scheme", got)
	}
	if len(warnings) != 0 {
		t.Errorf("got warnings %v, want none", warnings)
	}
}

func TestEmbeddedDarkMatchesPinnedPalette(t *testing.T) {
	t.Parallel()
	data, ok := builtinBytes(DefaultName)
	if !ok {
		t.Fatalf("embedded scheme %q is missing", DefaultName)
	}
	got, warnings := parseInto(Scheme{}, DefaultName+".yaml", data)
	if len(warnings) != 0 {
		t.Fatalf("embedded dark.yaml warned: %v", warnings)
	}
	for _, role := range roleTable {
		if got := role.get(got); got != darkPalette[role.key] {
			t.Errorf("dark.yaml key %q: got %q, want %q", role.key, got, darkPalette[role.key])
		}
	}
	if Default() != got {
		t.Errorf("Default() = %+v, want the parsed dark.yaml %+v", Default(), got)
	}
}
