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
