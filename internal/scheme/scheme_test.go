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
	{"tool-marker-bright", func(s Scheme) string { return s.ToolMarkerBright }},
	{"tool-leader", func(s Scheme) string { return s.ToolLeader }},
	{"gauge", func(s Scheme) string { return s.Gauge }},
	{"selection", func(s Scheme) string { return s.Selection }},
	{"spinner-1", func(s Scheme) string { return s.Spinner1 }},
	{"spinner-2", func(s Scheme) string { return s.Spinner2 }},
	{"spinner-3", func(s Scheme) string { return s.Spinner3 }},
	{"spinner-4", func(s Scheme) string { return s.Spinner4 }},
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

// TestEmbeddedDarkIsTheCompleteDefault guards the shipped default's STRUCTURE and never its
// palette. The schemes are retuned as the TUI is tuned, so pinning hex values here would make
// every color tweak a test failure; what has to hold is that dark.yaml is embedded, loads
// without a warning (so every value it states is a real #rrggbb under a known role), fills
// every role, and is exactly what Default() hands out. Completeness is asserted on Default()
// rather than on the file because that is where it bites: Default() is the fallback every role
// another scheme omits lands on, so a gap in it paints with no color at all.
func TestEmbeddedDarkIsTheCompleteDefault(t *testing.T) {
	t.Parallel()

	got := builtinScheme(t, DefaultName)

	def := Default()
	if def != got {
		t.Fatalf("Default() = %+v, want the parsed dark.yaml %+v", def, got)
	}
	for _, role := range roleTable {
		if role.get(def) == "" {
			t.Errorf("the default scheme leaves role %q empty — dark.yaml must state it, since every scheme that omits the role falls back to this value", role.key)
		}
	}
}
