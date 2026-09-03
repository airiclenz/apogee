package floor

import (
	"encoding/json"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/domain/domaintest"
)

// scanView wraps a built conversation fixture in the ConversationView the scans read.
func scanView(msgs []domain.Message) domain.ConversationView {
	return domaintest.FakeLoopView{Messages: msgs}.Conversation()
}

// writeCall is a write_file tool call over path — the write signal the scans count.
func writeCall(id, path string) domain.ToolCall {
	return domaintest.Call(id, "write_file", map[string]string{"path": path, "content": "x"})
}

// toolCallPath reads the file a call targets from the four sim-inherited spellings plus
// destination, the key copy_file and move_file carry instead. The precedence is pinned here:
// destination is read last, so a call carrying both path and destination still reports path.
func TestToolCallPath(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		args string
		want string
	}{
		{name: "path", args: `{"path":"alpha.go"}`, want: "alpha.go"},
		{name: "file_path", args: `{"file_path":"beta.go"}`, want: "beta.go"},
		{name: "filePath", args: `{"filePath":"gamma.go"}`, want: "gamma.go"},
		{name: "filename", args: `{"filename":"delta.go"}`, want: "delta.go"},
		{
			name: "copy_file reports the destination",
			args: `{"source":"origin.go","destination":"copy.go"}`,
			want: "copy.go",
		},
		{
			name: "move_file reports the destination",
			args: `{"source":"origin.go","destination":"moved.go","overwrite":true}`,
			want: "moved.go",
		},
		{
			name: "path keeps precedence over destination",
			args: `{"destination":"copy.go","path":"alpha.go"}`,
			want: "alpha.go",
		},
		{name: "source alone is not a path", args: `{"source":"origin.go"}`, want: ""},
		{name: "arguments are not a JSON object", args: `"alpha.go"`, want: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := toolCallPath(json.RawMessage(tc.args))

			if got != tc.want {
				t.Errorf("toolCallPath(%s) = %q, want %q", tc.args, got, tc.want)
			}
		})
	}
}

// The narration scans answer the three questions the tool-use enforcer asks of the history: how
// many assistant messages there are, whether the LAST one was bare prose, and whether the model
// has ever managed a tool call at all.
func TestNarrationScans(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		msgs         []domain.Message
		wantCount    int
		wantTextOnly bool
		wantEverUsed bool
	}{
		{
			name:      "an empty conversation",
			msgs:      nil,
			wantCount: 0,
		},
		{
			name:         "one narration and nothing else",
			msgs:         []domain.Message{domaintest.UserMessage("fix it"), domaintest.AssistantTextMessage("I will fix it.")},
			wantCount:    1,
			wantTextOnly: true,
		},
		{
			name: "a tool call after a narration",
			msgs: []domain.Message{
				domaintest.UserMessage("fix it"),
				domaintest.AssistantTextMessage("I will fix it."),
				domaintest.AssistantCallsMessage(domaintest.ReadCall("c1", "a.go")),
			},
			wantCount:    2,
			wantEverUsed: true,
		},
		{
			name: "a second narration after a tool call",
			msgs: []domain.Message{
				domaintest.UserMessage("fix it"),
				domaintest.AssistantCallsMessage(domaintest.ReadCall("c1", "a.go")),
				domaintest.ToolResultMessage("c1", "package main"),
				domaintest.AssistantTextMessage("Here is what I found."),
			},
			wantCount:    2,
			wantTextOnly: true,
			wantEverUsed: true,
		},
		{
			name:      "an empty reply is not a narration",
			msgs:      []domain.Message{domaintest.UserMessage("fix it"), domaintest.AssistantTextMessage("   ")},
			wantCount: 1,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			conv := scanView(c.msgs)

			if got := assistantMessageCount(conv); got != c.wantCount {
				t.Errorf("assistantMessageCount = %d, want %d", got, c.wantCount)
			}
			if got := previousAssistantWasTextOnly(conv); got != c.wantTextOnly {
				t.Errorf("previousAssistantWasTextOnly = %v, want %v", got, c.wantTextOnly)
			}
			if got := hasEverUsedTools(conv); got != c.wantEverUsed {
				t.Errorf("hasEverUsedTools = %v, want %v", got, c.wantEverUsed)
			}
		})
	}
}

// wroteRecently looks back over a WINDOW of assistant messages, so a write that has scrolled out
// of it no longer stands the enforcer down.
func TestWroteRecentlyIsBoundedByTheWindow(t *testing.T) {
	t.Parallel()

	msgs := []domain.Message{
		domaintest.UserMessage("write it"),
		domaintest.AssistantCallsMessage(writeCall("w1", "a.go")),
		domaintest.AssistantTextMessage("done, I think"),
		domaintest.AssistantTextMessage("really done"),
	}
	conv := scanView(msgs)

	if !wroteRecently(conv, 3) {
		t.Error("wroteRecently(window=3) = false, want true — the write is the third message back")
	}
	if wroteRecently(conv, 2) {
		t.Error("wroteRecently(window=2) = true, want false — the write is outside the window")
	}
}

// hasRecentProgress recovers an empty reply while the model is getting somewhere: early on
// unconditionally, later only on a write or on reads of two DISTINCT paths.
func TestHasRecentProgress(t *testing.T) {
	t.Parallel()

	narration := func(n int) []domain.Message {
		msgs := []domain.Message{domaintest.UserMessage("do it")}
		for range n {
			msgs = append(msgs, domaintest.AssistantTextMessage("thinking"))
		}
		return msgs
	}
	spinning := append(narration(3),
		domaintest.AssistantCallsMessage(domaintest.ReadCall("r1", "a.go")),
		domaintest.AssistantCallsMessage(domaintest.ReadCall("r2", "a.go")),
	)
	twoPaths := append(narration(3),
		domaintest.AssistantCallsMessage(domaintest.ReadCall("r1", "a.go")),
		domaintest.AssistantCallsMessage(domaintest.ReadCall("r2", "b.go")),
	)
	wrote := append(narration(3), domaintest.AssistantCallsMessage(writeCall("w1", "a.go")))

	cases := []struct {
		name string
		msgs []domain.Message
		want bool
	}{
		{name: "the first three turns always qualify", msgs: narration(3), want: true},
		{name: "prose past the third turn does not", msgs: narration(4)},
		{name: "spinning on one path is not progress", msgs: spinning},
		{name: "two distinct reads are progress", msgs: twoPaths, want: true},
		{name: "a write is progress", msgs: wrote, want: true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			if got := hasRecentProgress(scanView(c.msgs)); got != c.want {
				t.Errorf("hasRecentProgress = %v, want %v", got, c.want)
			}
		})
	}
}
