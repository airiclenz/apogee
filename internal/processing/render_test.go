package processing

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// fencedProfile and regexProfile are the two prompted-format profiles the render tests bind.
var (
	fencedProfile = domain.ModelProfile{ToolCallFormat: domain.FormatMarkdownFenced}
	regexProfile  = domain.ModelProfile{
		ToolCallFormat: domain.FormatCustomRegex,
		Pattern:        `<call name="(?<name>\w+)">(?<args>.*?)</call>`,
	}
)

// TestRenderToolCall pins the text each format writes a past call as: the fenced block in the
// default markers, the custom-regex pattern's own delimiters, the <tool_call> fallback, and the
// native profile's empty render.
func TestRenderToolCall(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		profile domain.ModelProfile
		call    domain.ToolCall
		want    string
	}{
		{
			name:    "fenced: a string value verbatim, then one line break",
			profile: fencedProfile,
			call:    domain.ToolCall{Tool: "read_file", Arguments: json.RawMessage(`{"path":"src/main.go"}`)},
			want:    "```tool\nTOOL_NAME\nread_file\nBEGIN_ARG\npath\nEND_ARG\nsrc/main.go\n```",
		},
		{
			name:    "fenced: pairs a blank line apart, a non-string value as JSON text",
			profile: fencedProfile,
			call: domain.ToolCall{
				Tool:      "read_file",
				Arguments: json.RawMessage(`{"path": "a.go", "start_line": 42, "opts": {"raw": true}}`),
			},
			want: "```tool\nTOOL_NAME\nread_file\n" +
				"BEGIN_ARG\npath\nEND_ARG\na.go\n\n" +
				"BEGIN_ARG\nstart_line\nEND_ARG\n42\n\n" +
				"BEGIN_ARG\nopts\nEND_ARG\n{\"raw\":true}\n```",
		},
		{
			name:    "fenced: a value opening on a line break gets one more",
			profile: fencedProfile,
			call:    domain.ToolCall{Tool: "write_file", Arguments: json.RawMessage(`{"content":"\nbody\n"}`)},
			want:    "```tool\nTOOL_NAME\nwrite_file\nBEGIN_ARG\ncontent\nEND_ARG\n\n\nbody\n\n```",
		},
		{
			name:    "fenced: arguments that are not an object render none",
			profile: fencedProfile,
			call:    domain.ToolCall{Tool: "ls", Arguments: json.RawMessage(`{"path": "trunc`)},
			want:    "```tool\nTOOL_NAME\nls\n```",
		},
		{
			name:    "custom-regex: the pattern's delimiters around compact arguments",
			profile: regexProfile,
			call:    domain.ToolCall{Tool: "read_file", Arguments: json.RawMessage(`{"path": "a.go"}`)},
			want:    `<call name="read_file">{"path":"a.go"}</call>`,
		},
		{
			name:    "custom-regex: arguments that are not an object render as {}",
			profile: regexProfile,
			call:    domain.ToolCall{Tool: "ls", Arguments: json.RawMessage(`[1]`)},
			want:    `<call name="ls">{}</call>`,
		},
		{
			name:    "custom-regex: a pattern with one named group falls back to <tool_call>",
			profile: domain.ModelProfile{ToolCallFormat: domain.FormatCustomRegex, Pattern: `CALL (?<name>\w+)`},
			call:    domain.ToolCall{Tool: "ls", Arguments: json.RawMessage(`{"path":"."}`)},
			want:    `<tool_call>ls({"path":"."})</tool_call>`,
		},
		{
			name:    "native: nothing to render",
			profile: domain.ModelProfile{ToolCallFormat: domain.FormatNative},
			call:    domain.ToolCall{Tool: "ls", Arguments: json.RawMessage(`{}`)},
			want:    "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := RenderToolCall(tc.profile, tc.call)

			if err != nil {
				t.Fatalf("RenderToolCall error = %v", err)
			}
			if got != tc.want {
				t.Errorf("RenderToolCall =\n%q\nwant\n%q", got, tc.want)
			}
		})
	}
}

// TestRenderToolCallRejectsUnknownFormat pins the misconfiguration path: a format no parser
// exists for is an error, as it is for InstructionsFor.
func TestRenderToolCallRejectsUnknownFormat(t *testing.T) {
	t.Parallel()

	_, err := RenderToolCall(domain.ModelProfile{ToolCallFormat: "smoke-signals"}, domain.ToolCall{Tool: "ls"})

	if err == nil || !strings.Contains(err.Error(), "smoke-signals") {
		t.Errorf("RenderToolCall error = %v, want one naming the unknown format", err)
	}
}

// TestRenderToolCallRoundTrips pins render as the parser's inverse: what RenderToolCall writes,
// the profile's own parser (plus the schema decode a recovered call goes through) reads back as
// the same arguments — line breaks at either end of a value, a value ending the call and one in
// the middle, and typed values included.
func TestRenderToolCallRoundTrips(t *testing.T) {
	t.Parallel()

	schema := json.RawMessage(`{"type":"object","properties":{` +
		`"path":{"type":"string"},"content":{"type":"string"},"note":{"type":"string"},` +
		`"start_line":{"type":"integer"},"opts":{"type":"object"}}}`)
	cases := []struct {
		name    string
		profile domain.ModelProfile
		args    string
	}{
		{name: "fenced: one plain value", profile: fencedProfile, args: `{"path":"a.go"}`},
		{
			name:    "fenced: breaks at both ends, mid-call and last",
			profile: fencedProfile,
			args:    `{"content":"\n  indented\n\nlast line\n","note":"\r\nwindows\r\n","path":"x\n"}`,
		},
		{name: "fenced: an empty string", profile: fencedProfile, args: `{"note":"","path":"a.go"}`},
		{name: "fenced: typed values", profile: fencedProfile, args: `{"opts":{"deep":[1,2]},"start_line":7}`},
		{name: "custom-regex", profile: regexProfile, args: `{"content":"a\nb","start_line":3}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			parser, _, err := ParserFor(tc.profile)
			if err != nil {
				t.Fatalf("ParserFor: %v", err)
			}

			text, err := RenderToolCall(tc.profile, domain.ToolCall{Tool: "edit", Arguments: json.RawMessage(tc.args)})
			if err != nil {
				t.Fatalf("RenderToolCall: %v", err)
			}
			call, found := parser.ParseToolCall("Doing it now.\n\n" + text)

			if !found || call.Tool != "edit" {
				t.Fatalf("parsed call = %+v (found %v), want tool edit", call, found)
			}
			assertSameArguments(t, DecodeSchemaTypedArgs(call.Arguments, schema), tc.args)
		})
	}
}

// assertSameArguments fails unless got and want decode to the same JSON value.
func assertSameArguments(t *testing.T, got json.RawMessage, want string) {
	t.Helper()
	var gotValue, wantValue any
	if err := json.Unmarshal(got, &gotValue); err != nil {
		t.Fatalf("decode got %s: %v", got, err)
	}
	if err := json.Unmarshal([]byte(want), &wantValue); err != nil {
		t.Fatalf("decode want %s: %v", want, err)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Errorf("round-tripped arguments = %s, want %s", got, want)
	}
}
