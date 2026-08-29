package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
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
