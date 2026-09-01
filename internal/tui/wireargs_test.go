package tui

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/tools"
)

// TestWireArgs pins the stored form of a call's arguments: what survives, what is elided, what is
// dropped and what is refused outright.
func TestWireArgs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		tool string
		raw  string
		want string
	}{
		{
			name: "small arguments pass through compact and key-sorted",
			tool: "grep",
			raw:  `{"pattern": "KeyMsg", "path": "internal/tui/model.go"}`,
			want: `{"path":"internal/tui/model.go","pattern":"KeyMsg"}`,
		},
		{
			name: "unsorted keys sort and a markup character is escaped",
			tool: "grep",
			raw:  `{"pattern":"a < b","include":"*.go","context":2}`,
			want: `{"context":2,"include":"*.go","pattern":"a \u003c b"}`,
		},
		{
			name: "a large integer keeps the spelling the model sent",
			tool: "tail_log",
			raw:  `{"offset":9007199254740993,"lines":10}`,
			want: `{"lines":10,"offset":9007199254740993}`,
		},
		{
			name: "write_file drops its content and keeps its path",
			tool: "write_file",
			raw:  `{"path":"docs/notes.md","content":"the whole file body"}`,
			want: `{"path":"docs/notes.md"}`,
		},
		{
			name: "edit_existing_file drops its content",
			tool: "edit_existing_file",
			raw:  `{"path":"main.go","content":"*** Begin Patch"}`,
			want: `{"path":"main.go"}`,
		},
		{
			name: "single_find_and_replace drops both halves of the pair",
			tool: "single_find_and_replace",
			raw:  `{"path":"main.go","oldText":"before","newText":"after"}`,
			want: `{"path":"main.go"}`,
		},
		{
			name: "multi_find_and_replace drops its replacement list",
			tool: "multi_find_and_replace",
			raw:  `{"path":"main.go","replacements":[{"oldText":"a","newText":"b"}]}`,
			want: `{"path":"main.go"}`,
		},
		{
			name: "an over-long string becomes its own size",
			tool: "run_shell_command",
			raw:  fmt.Sprintf(`{"command":%q,"timeout":30}`, strings.Repeat("x", 2048)),
			want: `{"command":"…[2048 bytes]","timeout":30}`,
		},
		{
			name: "a string exactly at the field cap survives whole",
			tool: "run_shell_command",
			raw:  fmt.Sprintf(`{"command":%q}`, strings.Repeat("x", wireArgsFieldCap)),
			want: fmt.Sprintf(`{"command":%q}`, strings.Repeat("x", wireArgsFieldCap)),
		},
		{
			name: "an over-long string nested in an array is bounded where it sits",
			tool: "some_mcp_tool",
			raw:  fmt.Sprintf(`{"items":[{"body":%q}]}`, strings.Repeat("y", 1100)),
			want: `{"items":[{"body":"…[1100 bytes]"}]}`,
		},
		{
			name: "a write tool with nothing but content keeps nothing",
			tool: "write_file",
			raw:  `{"content":"only the body"}`,
			want: "",
		},
		{
			name: "invalid JSON is not recorded",
			tool: "grep",
			raw:  `{"pattern": `,
			want: "",
		},
		{
			name: "a non-object payload is not recorded",
			tool: "grep",
			raw:  `["pattern"]`,
			want: "",
		},
		{
			name: "empty arguments are not recorded",
			tool: "list_dir",
			raw:  ``,
			want: "",
		},
		{
			name: "an empty object is not recorded",
			tool: "list_dir",
			raw:  `{}`,
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := wireArgs(tc.tool, json.RawMessage(tc.raw))

			if string(got) != tc.want {
				t.Errorf("wireArgs(%q) = %s, want %s", tc.tool, got, tc.want)
			}
		})
	}
}

// TestWireArgsCollapsesAPayloadOverTheCap pins that arguments still over the whole-call cap after
// the per-field elisions are stored as their size alone.
func TestWireArgsCollapsesAPayloadOverTheCap(t *testing.T) {
	t.Parallel()

	payload := map[string]any{}
	for i := range 20 {
		payload[fmt.Sprintf("key%02d", i)] = strings.Repeat("z", 300)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal the payload: %v", err)
	}
	if len(raw) <= wireArgsCap {
		t.Fatalf("payload is %d bytes, want more than the %d-byte cap", len(raw), wireArgsCap)
	}

	got := wireArgs("some_mcp_tool", raw)

	want := fmt.Sprintf(`{"elided":"%d bytes"}`, len(raw))
	if string(got) != want {
		t.Errorf("wireArgs over the cap = %s, want %s", got, want)
	}
}

// TestWireArgsSurvivesTheTranscriptEncoder pins the reason the wire form is built by decoding and
// re-marshalling rather than compacting: encodeTranscript re-encodes every json.RawMessage member
// with its own Marshal, so a form that were not already sorted and HTML-escaped would shift on the
// way to disk and no longer match what this function returned.
func TestWireArgsSurvivesTheTranscriptEncoder(t *testing.T) {
	t.Parallel()

	stored := wireArgs("grep", json.RawMessage(`{"pattern":"a < b","include":"*.go"}`))

	encoded, err := json.Marshal(struct {
		Args json.RawMessage `json:"args"`
	}{Args: stored})
	if err != nil {
		t.Fatalf("re-encode the stored arguments: %v", err)
	}

	want := `{"args":` + string(stored) + `}`
	if string(encoded) != want {
		t.Errorf("re-encoded = %s, want %s", encoded, want)
	}
}

// TestContentArgsMatchToolSchemas cross-checks [contentArgs] against the schemas the write/edit
// tools actually publish. The map spells those content keys a second time and keys them by tool
// NAME, so a rename in internal/tools — of a tool or of one of its arguments — would leave this
// side silently matching nothing and quietly push file bodies onto the wire (ISSUES.md:137). The
// registry is the same one the engine gives an Agent, so the check reads the shipped schemas
// rather than a copy of them.
func TestContentArgsMatchToolSchemas(t *testing.T) {
	t.Parallel()

	registry := tools.NewDefaultRegistry(t.TempDir())

	problems := contentArgsProblems(registry, checkedContentArgs())

	for _, problem := range problems {
		t.Error(problem)
	}
}

// TestContentArgsProblemsReportsBothHalves pins that the cross-check above can actually fail: a
// tool name no registry resolves and a key no schema carries are the two ways the map drifts, and
// each must be reported rather than passed over.
func TestContentArgsProblemsReportsBothHalves(t *testing.T) {
	t.Parallel()

	registry := tools.NewDefaultRegistry(t.TempDir())

	cases := []struct {
		name string
		args map[string][]string
		want string
	}{
		{
			name: "a tool the registry does not carry",
			args: map[string][]string{"write_file_v2": {"content"}},
			want: "write_file_v2",
		},
		{
			name: "a key the tool's schema does not carry",
			args: map[string][]string{"write_file": {"contents"}},
			want: "contents",
		},
		{
			name: "a nested key the replacement pairs do not carry",
			args: map[string][]string{"multi_find_and_replace": {"replacements.old_text"}},
			want: "replacements.old_text",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			problems := contentArgsProblems(registry, testCase.args)

			if len(problems) != 1 || !strings.Contains(problems[0], testCase.want) {
				t.Errorf("contentArgsProblems = %q, want one problem naming %q", problems, testCase.want)
			}
		})
	}
}

// checkedContentArgs returns [contentArgs] with the two keys that live one level down added to it:
// multi_find_and_replace drops its whole replacements array, but the bytes that array carries are
// the oldText/newText pair inside its items, and a rename there drifts exactly as a top-level one
// does. A dotted key is a path through the schema — see [contentArgsProblems].
func checkedContentArgs() map[string][]string {
	checked := map[string][]string{
		"multi_find_and_replace": {"replacements.oldText", "replacements.newText"},
	}
	for tool, keys := range contentArgs {
		checked[tool] = append(checked[tool], keys...)
	}
	return checked
}

// contentArgsProblems returns one line per mismatch between args — a [contentArgs]-shaped map of
// tool name to the argument keys whose value is file content — and the schemas registry's tools
// publish: a name the registry does not resolve, a schema that will not decode, or a key the
// schema has no property for. An empty result means every name and key still lands.
//
// A key spelled with dots is a path: each segment is read from the enclosing schema's "properties"
// object, descending through an array schema's "items" on the way, so "replacements.oldText"
// resolves at properties.replacements.items.properties.oldText.
func contentArgsProblems(registry *domain.ToolRegistry, args map[string][]string) []string {
	var problems []string
	for _, name := range slices.Sorted(maps.Keys(args)) {
		tool, ok := registry.Lookup(name)
		if !ok {
			problems = append(problems, fmt.Sprintf("contentArgs names %q, which no tool in the default registry answers to", name))
			continue
		}
		var schema map[string]any
		if err := json.Unmarshal(tool.Schema(), &schema); err != nil {
			problems = append(problems, fmt.Sprintf("%s: decode the tool's schema: %v", name, err))
			continue
		}
		for _, key := range args[name] {
			if !schemaHasProperty(schema, strings.Split(key, ".")) {
				problems = append(problems, fmt.Sprintf("contentArgs drops %q from %s, whose schema has no such property", key, name))
			}
		}
	}
	return problems
}

// schemaHasProperty reports whether path resolves to a property of the JSON schema, reading each
// segment from the enclosing object's "properties" and stepping through an array schema's "items"
// before the next segment.
func schemaHasProperty(schema map[string]any, path []string) bool {
	for _, segment := range path {
		properties, ok := schema["properties"].(map[string]any)
		if !ok {
			return false
		}
		schema, ok = properties[segment].(map[string]any)
		if !ok {
			return false
		}
		if items, ok := schema["items"].(map[string]any); ok {
			schema = items
		}
	}
	return true
}
