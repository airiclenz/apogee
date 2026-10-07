package processing

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// readFileMenu is the single-tool menu the oracle's context-builder test vectors use: a read_file
// tool with one string "path" property. It anchors the ported expected-output vectors.
var readFileMenu = []domain.ToolDef{{
	Name:        "read_file",
	Description: "Read a file",
	Schema:      json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`),
}}

// wantFencedInstructions is the byte-exact "## Tool Call Format" block for the default
// markdown-fenced knobs over readFileMenu (oracle: buildMarkdownFencedInstructions).
var wantFencedInstructions = strings.Join([]string{
	"## Tool Call Format",
	"",
	"To call a tool, output a fenced code block with language `tool` using this exact structure:",
	"",
	"````",
	"```tool",
	"TOOL_NAME",
	"<tool_name>",
	"BEGIN_ARG",
	"<argument_name>",
	"END_ARG",
	"<argument_value>",
	"```",
	"````",
	"",
	"Each argument needs its own BEGIN_ARG / END_ARG pair.",
	"",
	"Example — calling `read_file`:",
	"",
	"````",
	"```tool",
	"TOOL_NAME",
	"read_file",
	"BEGIN_ARG",
	"path",
	"END_ARG",
	"src/main.ts",
	"```",
	"````",
	"",
	"IMPORTANT: Use ONLY the format shown above. Do NOT invent other tool call formats.",
}, "\n")

// wantMenuBlock is the byte-exact "## Available Tools" menu for readFileMenu (oracle:
// formatToolsBlock, without the budget/truncation note).
const wantMenuBlock = "## Available Tools\n\n" +
	"- **read_file**: Read a file\n" +
	`  Parameters: {"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`

func TestInstructionsFor(t *testing.T) {
	t.Parallel()

	regexInstructions := strings.Join([]string{
		"## Tool Call Format",
		"",
		"To call a tool, output the tool name and a JSON object of arguments in this format:",
		"",
		`<tool_call>read_file({"path": "src/main.ts"})</tool_call>`,
		"",
		"Arguments MUST be valid JSON. Do NOT use any other format.",
	}, "\n")

	tests := []struct {
		name    string
		profile domain.ModelProfile
		menu    []domain.ToolDef
		want    string
		wantErr bool
	}{
		{
			name:    "zero profile renders nothing",
			profile: domain.ModelProfile{},
			menu:    readFileMenu,
			want:    "",
		},
		{
			name:    "native profile renders nothing",
			profile: domain.ModelProfile{ToolCallFormat: domain.FormatNative},
			menu:    readFileMenu,
			want:    "",
		},
		{
			name:    "fenced with default knobs renders menu and instructions",
			profile: domain.ModelProfile{ToolCallFormat: domain.FormatMarkdownFenced},
			menu:    readFileMenu,
			want:    wantMenuBlock + "\n\n" + wantFencedInstructions,
		},
		{
			name:    "regex with pattern renders menu and instructions",
			profile: domain.ModelProfile{ToolCallFormat: domain.FormatCustomRegex, Pattern: `<tool_call>(?<name>\w+)\((?<args>\{.*?\})\)</tool_call>`},
			menu:    readFileMenu,
			want:    wantMenuBlock + "\n\n" + regexInstructions,
		},
		{
			name:    "fenced with empty menu renders nothing",
			profile: domain.ModelProfile{ToolCallFormat: domain.FormatMarkdownFenced},
			menu:    nil,
			want:    "",
		},
		{
			name:    "unknown format is an error",
			profile: domain.ModelProfile{ToolCallFormat: domain.ToolCallFormat("mystery")},
			menu:    readFileMenu,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := InstructionsFor(tc.profile, tc.menu)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("InstructionsFor(%+v) = %q, nil; want error", tc.profile, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("InstructionsFor(%+v) unexpected error: %v", tc.profile, err)
			}
			if got != tc.want {
				t.Errorf("InstructionsFor mismatch:\n got: %q\nwant: %q", got, tc.want)
			}
		})
	}
}

// commandSchema is the one-string-property schema of the bash-like tools the custom-regex
// instruction tests offer; its "command" property makes the example value "ls -la".
var commandSchema = json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"}}}`)

// regexBlockWith is the byte-exact custom-regex "## Tool Call Format" block showing example.
func regexBlockWith(example string) string {
	return strings.Join([]string{
		"## Tool Call Format",
		"",
		"To call a tool, output the tool name and a JSON object of arguments in this format:",
		"",
		example,
		"",
		"Arguments MUST be valid JSON. Do NOT use any other format.",
	}, "\n")
}

// TestCustomRegexInstructions_ManualPatternRoundTrips pins the derived example: it is written in
// the pattern's own delimiters — named groups in either spelling, in either order, nested parens
// included — and only a call the profile's parser reads back is shown, the probe call standing in
// when no menu tool's call does.
func TestCustomRegexInstructions_ManualPatternRoundTrips(t *testing.T) {
	t.Parallel()

	bash := domain.ToolDef{Name: "bash", Schema: commandSchema}
	runIt := domain.ToolDef{Name: "run-it", Schema: commandSchema}
	for _, tc := range []struct {
		name    string
		pattern string
		menu    []domain.ToolDef
		want    string
	}{
		{name: "the manual pattern", pattern: manualPattern, menu: []domain.ToolDef{bash}, want: `<tool_call>bash{"command": "ls -la"}</tool_call>`},
		{
			name:    "the manual pattern in the (?P< spelling",
			pattern: `<tool_call>\s*(?P<name>[\w.-]+)\s*(?P<args>\{.*?\})\s*</tool_call>`,
			menu:    []domain.ToolDef{bash},
			want:    `<tool_call>bash{"command": "ls -la"}</tool_call>`,
		},
		{
			name:    "a name group holding nested parens",
			pattern: `<call>(?<name>(?:\w|-)+)\((?<args>\{.*\})\)</call>`,
			menu:    []domain.ToolDef{bash},
			want:    `<call>bash({"command": "ls -la"})</call>`,
		},
		{
			name:    "the args group first",
			pattern: `^(?<args>\{.*?\})\s+@(?<name>\w+)$`,
			menu:    []domain.ToolDef{bash},
			want:    `{"command": "ls -la"} @bash`,
		},
		{
			name:    "the first menu tool whose call parses back",
			pattern: `(?<name>[a-z]+)\s+(?<args>\{.*\})`,
			menu:    []domain.ToolDef{runIt, bash},
			want:    `bash {"command": "ls -la"}`,
		},
		{
			name:    "the probe call when no menu tool's call parses back",
			pattern: `(?<name>[a-z]+)\s+(?<args>\{.*\})`,
			menu:    []domain.ToolDef{runIt},
			want:    `read_file {"path": "src/main.ts"}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			profile := domain.ModelProfile{ToolCallFormat: domain.FormatCustomRegex, Pattern: tc.pattern}

			got := customRegexInstructions(profile, tc.menu)

			if want := regexBlockWith(tc.want); got != want {
				t.Errorf("customRegexInstructions =\n%q\nwant\n%q", got, want)
			}
			if tc.want == probeExampleCall.toolName+" "+probeExampleCall.argsJSON() {
				return // the stand-in is shown whether or not it parses back
			}
			call, ok := NewCustomRegexParser(CustomRegexConfig{Pattern: tc.pattern}).ParseToolCall(tc.want)
			if !ok || call.Tool != "bash" || call.Malformed != nil {
				t.Errorf("the parser reads the shown call as %+v (found %v), want a well-formed bash call", call, ok)
			}
		})
	}
}

// TestCustomRegexInstructions_ExampleWinsVerbatim pins the profile's own example: it is shown
// exactly as given, in place of any derived call.
func TestCustomRegexInstructions_ExampleWinsVerbatim(t *testing.T) {
	t.Parallel()
	const example = `<tool_call> bash {"command": "pwd"} </tool_call>`
	profile := domain.ModelProfile{ToolCallFormat: domain.FormatCustomRegex, Pattern: manualPattern, ToolCallExample: example}

	got, err := InstructionsFor(profile, []domain.ToolDef{{Name: "bash", Schema: commandSchema}})

	if err != nil {
		t.Fatalf("InstructionsFor error = %v", err)
	}
	if !strings.HasSuffix(got, "\n\n"+regexBlockWith(example)) {
		t.Errorf("InstructionsFor =\n%s\nwant it to end with the block showing %s", got, example)
	}
	if strings.Contains(got, "ls -la") {
		t.Errorf("InstructionsFor shows a derived call beside the profile's example:\n%s", got)
	}
}

// TestCustomRegexInstructions_NoExampleWithoutGroups pins the unvalidated-profile rule: a pattern
// lacking the args group renders the block with no example line, and an empty pattern renders no
// block at all.
func TestCustomRegexInstructions_NoExampleWithoutGroups(t *testing.T) {
	t.Parallel()
	want := strings.Join([]string{
		"## Tool Call Format",
		"",
		"To call a tool, output the tool name and a JSON object of arguments.",
		"",
		"Arguments MUST be valid JSON. Do NOT use any other format.",
	}, "\n")

	got := customRegexInstructions(domain.ModelProfile{ToolCallFormat: domain.FormatCustomRegex, Pattern: `\[(?<name>\w+)\]`}, readFileMenu)

	if got != want {
		t.Errorf("customRegexInstructions =\n%q\nwant\n%q", got, want)
	}
	if strings.Contains(got, "<tool_call>") {
		t.Errorf("customRegexInstructions shows a <tool_call> call:\n%s", got)
	}
	if empty := customRegexInstructions(domain.ModelProfile{ToolCallFormat: domain.FormatCustomRegex}, readFileMenu); empty != "" {
		t.Errorf("customRegexInstructions with an empty pattern = %q, want \"\"", empty)
	}
}

// TestMarkdownFencedInstructions_OverriddenKnobs exercises the "fenced with overridden knobs"
// parity vector at the renderer level: domain.ModelProfile carries no fenced knob fields, so
// overrides cannot flow through InstructionsFor, but the ported renderer must honour a custom
// config exactly like the oracle's second markdown-fenced test (fn / FUNCTION / PARAM_START /
// PARAM_END, and none of the default field names).
func TestMarkdownFencedInstructions_OverriddenKnobs(t *testing.T) {
	t.Parallel()
	cfg := MarkdownFencedConfig{
		FenceLanguage: "fn",
		NameField:     "FUNCTION",
		ArgStartField: "PARAM_START",
		ArgEndField:   "PARAM_END",
	}.withDefaults()

	got := markdownFencedInstructions(cfg, readFileMenu)

	for _, want := range []string{"```fn", "FUNCTION", "PARAM_START", "PARAM_END", "read_file", "src/main.ts"} {
		if !strings.Contains(got, want) {
			t.Errorf("markdownFencedInstructions missing %q in:\n%s", want, got)
		}
	}
	for _, absent := range []string{"TOOL_NAME", "BEGIN_ARG", "END_ARG", "```tool\n"} {
		if strings.Contains(got, absent) {
			t.Errorf("markdownFencedInstructions unexpectedly contains %q", absent)
		}
	}
}

// TestPickExampleToolCall covers the value heuristics ported from the oracle: parameter-less tools,
// path/command string hints, and numeric/boolean/plain types.
func TestPickExampleToolCall(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		schema     string
		wantArgN   string
		wantArgVal string
	}{
		{name: "no properties falls back to input/example", schema: `{"type":"object"}`, wantArgN: "input", wantArgVal: "example"},
		{name: "empty properties falls back to input/example", schema: `{"type":"object","properties":{}}`, wantArgN: "input", wantArgVal: "example"},
		{name: "string path hint", schema: `{"properties":{"path":{"type":"string"}}}`, wantArgN: "path", wantArgVal: "src/main.ts"},
		{name: "string command hint", schema: `{"properties":{"command":{"type":"string"}}}`, wantArgN: "command", wantArgVal: "ls -la"},
		{name: "plain string", schema: `{"properties":{"query":{"type":"string"}}}`, wantArgN: "query", wantArgVal: "example"},
		{name: "integer type", schema: `{"properties":{"count":{"type":"integer"}}}`, wantArgN: "count", wantArgVal: "1"},
		{name: "number type", schema: `{"properties":{"ratio":{"type":"number"}}}`, wantArgN: "ratio", wantArgVal: "1"},
		{name: "boolean type", schema: `{"properties":{"force":{"type":"boolean"}}}`, wantArgN: "force", wantArgVal: "true"},
		{name: "untyped property keeps the name, default value", schema: `{"properties":{"opts":{}}}`, wantArgN: "opts", wantArgVal: "example"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			menu := []domain.ToolDef{{Name: "do_it", Schema: json.RawMessage(tc.schema)}}
			got := pickExampleToolCall(menu)
			if got.toolName != "do_it" {
				t.Errorf("toolName = %q, want do_it", got.toolName)
			}
			if got.argName != tc.wantArgN {
				t.Errorf("argName = %q, want %q", got.argName, tc.wantArgN)
			}
			if got.argValue != tc.wantArgVal {
				t.Errorf("argValue = %q, want %q", got.argValue, tc.wantArgVal)
			}
		})
	}
}

// TestSchemaJSON confirms compaction and the empty-schema default (oracle: JSON.stringify).
func TestSchemaJSON(t *testing.T) {
	t.Parallel()
	if got := schemaJSON(nil); got != "{}" {
		t.Errorf("schemaJSON(nil) = %q, want {}", got)
	}
	spaced := json.RawMessage("{\n  \"a\": 1\n}")
	if got := schemaJSON(spaced); got != `{"a":1}` {
		t.Errorf("schemaJSON(spaced) = %q, want compact", got)
	}
}
