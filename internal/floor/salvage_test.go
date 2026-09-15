package floor

import (
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// salvageOffered is the tool menu the salvage tests hand the guard as names.
func salvageOffered() []string {
	return []string{"read_file", "write_file"}
}

// textResponse builds a wire-less response: text only, no tool calls, which is the only shape the
// salvage guard ever fires on.
func textResponse(text string) *domain.Response {
	return domain.NewResponse(text, "", nil, domain.FinishStop, nil)
}

// wantCall is one expected salvaged call, compared by tool name and argument bytes.
type wantCall struct {
	tool string
	args string
}

func TestSalvageToolCallReadsAWrittenCall(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		text     string
		want     []wantCall
		wantText string
	}{
		{
			name:     "fenced block",
			text:     "I will read it.\n\n```json\n{\"name\": \"read_file\", \"arguments\": {\"path\": \"a.go\"}}\n```",
			want:     []wantCall{{tool: "read_file", args: `{"path": "a.go"}`}},
			wantText: "I will read it.",
		},
		{
			name:     "tool_call tags",
			text:     "<tool_call>{\"name\": \"read_file\", \"arguments\": {\"path\": \"a.go\"}}</tool_call>",
			want:     []wantCall{{tool: "read_file", args: `{"path": "a.go"}`}},
			wantText: "",
		},
		{
			name:     "whole trimmed content",
			text:     "\n  {\"name\": \"read_file\", \"arguments\": {\"path\": \"a.go\"}}  \n",
			want:     []wantCall{{tool: "read_file", args: `{"path": "a.go"}`}},
			wantText: "",
		},
		{
			name: "two calls in document order",
			text: "First:\n```json\n{\"name\": \"read_file\", \"arguments\": {\"path\": \"a.go\"}}\n```\nthen:\n" +
				"<tool_call>{\"name\": \"write_file\", \"arguments\": {\"path\": \"b.go\"}}</tool_call>",
			want: []wantCall{
				{tool: "read_file", args: `{"path": "a.go"}`},
				{tool: "write_file", args: `{"path": "b.go"}`},
			},
			wantText: "First:\n\nthen:",
		},
		{
			name:     "parameters key",
			text:     "```\n{\"name\": \"read_file\", \"parameters\": {\"path\": \"a.go\"}}\n```",
			want:     []wantCall{{tool: "read_file", args: `{"path": "a.go"}`}},
			wantText: "",
		},
		{
			name:     "input key",
			text:     "```\n{\"name\": \"read_file\", \"input\": {\"path\": \"a.go\"}}\n```",
			want:     []wantCall{{tool: "read_file", args: `{"path": "a.go"}`}},
			wantText: "",
		},
		{
			name:     "string-encoded arguments",
			text:     "```json\n{\"name\": \"read_file\", \"arguments\": \"{\\\"path\\\": \\\"a.go\\\"}\"}\n```",
			want:     []wantCall{{tool: "read_file", args: `{"path": "a.go"}`}},
			wantText: "",
		},
		{
			name:     "empty string arguments normalise to an empty object",
			text:     "```json\n{\"name\": \"read_file\", \"arguments\": \"\"}\n```",
			want:     []wantCall{{tool: "read_file", args: `{}`}},
			wantText: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			calls, text, fired := SalvageToolCall(textResponse(tc.text), salvageOffered())

			if !fired {
				t.Fatalf("guard did not fire on %q", tc.text)
			}
			assertCalls(t, calls, tc.want)
			if text != tc.wantText {
				t.Errorf("stripped text = %q, want %q", text, tc.wantText)
			}
		})
	}
}

func TestSalvageToolCallLeavesEveryOtherResponseAlone(t *testing.T) {
	t.Parallel()

	wireCall := domain.NewResponse(
		"```json\n{\"name\": \"read_file\", \"arguments\": {\"path\": \"a.go\"}}\n```",
		"",
		[]domain.ToolCall{{ID: "call_1", Tool: "read_file", Arguments: []byte(`{"path":"a.go"}`)}},
		domain.FinishToolCalls,
		nil,
	)

	cases := []struct {
		name string
		resp *domain.Response
	}{
		{name: "a call already on the wire", resp: wireCall},
		{
			name: "a tool the model was not offered",
			resp: textResponse("```json\n{\"name\": \"delete_everything\", \"arguments\": {}}\n```"),
		},
		{
			name: "a prose mention of a tool",
			resp: textResponse("I could use read_file with arguments path=a.go, but I will not."),
		},
		{
			name: "malformed JSON",
			resp: textResponse("```json\n{\"name\": \"read_file\", \"arguments\": {\"path\": }\n```"),
		},
		{
			name: "no arguments key at all",
			resp: textResponse("```json\n{\"name\": \"read_file\"}\n```"),
		},
		{
			name: "arguments that are not an object",
			resp: textResponse("```json\n{\"name\": \"read_file\", \"arguments\": [\"a.go\"]}\n```"),
		},
		{
			name: "a string holding something that is not an object",
			resp: textResponse("```json\n{\"name\": \"read_file\", \"arguments\": \"a.go\"}\n```"),
		},
		{name: "empty text", resp: textResponse("   \n  ")},
		{
			name: "a fenced block that is not a call",
			resp: textResponse("```go\nfunc main() {}\n```"),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			calls, text, fired := SalvageToolCall(tc.resp, salvageOffered())

			if fired {
				t.Fatalf("guard fired: calls=%+v text=%q", calls, text)
			}
			if len(calls) != 0 || text != "" {
				t.Errorf("no-op case returned calls=%+v text=%q, want empty", calls, text)
			}
		})
	}
}

func TestSalvageToolCallKeepsTheNarrationAroundTheCall(t *testing.T) {
	t.Parallel()

	text := "Let me look at the file.\n\n```json\n{\"name\": \"read_file\", \"arguments\": {\"path\": \"a.go\"}}\n```\n\nThen I will report back."

	_, stripped, fired := SalvageToolCall(textResponse(text), salvageOffered())

	if !fired {
		t.Fatal("guard did not fire")
	}
	if stripped != "Let me look at the file.\n\n\n\nThen I will report back." {
		t.Errorf("stripped text = %q", stripped)
	}
}

func TestSalvageToolCallLeavesIDsForTheEngine(t *testing.T) {
	t.Parallel()

	calls, _, fired := SalvageToolCall(
		textResponse("<tool_call>{\"name\": \"write_file\", \"arguments\": {\"path\": \"b.go\"}}</tool_call>"),
		salvageOffered(),
	)

	if !fired || len(calls) != 1 {
		t.Fatalf("guard fired=%v with %d calls, want one call", fired, len(calls))
	}
	if calls[0].ID != "" {
		t.Errorf("salvaged call carries ID %q, want the engine to assign it", calls[0].ID)
	}
}

// assertCalls compares the salvaged calls against the expected tool names and argument bytes.
func assertCalls(t *testing.T, got []domain.ToolCall, want []wantCall) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("got %d calls, want %d: %+v", len(got), len(want), got)
	}
	for i, expected := range want {
		if got[i].Tool != expected.tool {
			t.Errorf("call %d tool = %q, want %q", i, got[i].Tool, expected.tool)
		}
		if string(got[i].Arguments) != expected.args {
			t.Errorf("call %d arguments = %s, want %s", i, got[i].Arguments, expected.args)
		}
	}
}

// TestHasToolCallMarkupNamesAWrittenCall pins the two guards the recogniser fires on — a reply that
// BEGINS with a vendor container, or a container whose body is a JSON object — and the shapes it
// must leave alone: a report that quotes a tag pair in prose, and a code fence, which is how a
// report shows a snippet and never a container here.
func TestHasToolCallMarkupNamesAWrittenCall(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		text string
		want bool
	}{
		{
			name: "a tool_call pair holding a JSON object",
			text: "<tool_call>{\"name\": \"read_file\", \"arguments\": {\"path\": \"a.go\"}}</tool_call>",
			want: true,
		},
		{
			name: "a JSON-bodied tool_call pair quoted mid-report",
			text: "I meant to run <tool_call>{\"name\": \"read_file\"}</tool_call> next.",
			want: true,
		},
		{
			name: "a reply that begins with a DSML tool_calls container",
			text: "<｜DSML｜tool_calls><｜DSML｜invoke name=\"read_file\"><｜DSML｜parameter name=\"path\">a.go</｜DSML｜parameter></｜DSML｜invoke></｜DSML｜tool_calls>",
			want: true,
		},
		{
			name: "a reply that begins with a prose-bodied tool_call pair",
			text: "  <tool_call>read the file</tool_call>\nthen stop",
			want: true,
		},
		{
			name: "a report that quotes a prose-bodied tag pair",
			text: "The model wrote `<tool_call>read the file</tool_call>` and stopped there.",
			want: false,
		},
		{
			name: "a report that quotes a DSML container after its first line",
			text: "The child's reply was:\n<｜DSML｜tool_calls>invoke read_file</｜DSML｜tool_calls>",
			want: false,
		},
		{
			name: "a report showing a code fence",
			text: "Add this:\n```json\n{\"name\": \"read_file\", \"arguments\": {}}\n```",
			want: false,
		},
		{
			name: "plain prose",
			text: "The repo has four packages and no tests for two of them.",
			want: false,
		},
		{name: "an empty reply", text: "", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := HasToolCallMarkup(tc.text); got != tc.want {
				t.Errorf("HasToolCallMarkup(%q) = %v, want %v", tc.text, got, tc.want)
			}
		})
	}
}
