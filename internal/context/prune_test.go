package context

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// callSpec is one tool call and the result it produced, for pruneConv.
type callSpec struct {
	id      string
	tool    string
	args    string
	content string
	// resultID overrides the id stamped on the result message, for the case where a result
	// answers no call the conversation holds. Empty means "the call's own id".
	resultID string
}

// pruneConv builds a conversation with the usual protected prefix (a system message and the
// opening user message) followed by one assistant tool-calling message per turn, each trailed
// by its results. A nil args string is rendered as the empty object.
func pruneConv(turns ...[]callSpec) *domain.Conversation {
	msgs := []domain.Message{
		{Role: domain.RoleSystem, Content: "s"},
		{Role: domain.RoleUser, Content: "u"},
	}
	for _, turn := range turns {
		calls := make([]domain.ToolCall, 0, len(turn))
		for _, c := range turn {
			args := c.args
			if args == "" {
				args = "{}"
			}
			calls = append(calls, domain.ToolCall{ID: c.id, Tool: c.tool, Arguments: json.RawMessage(args)})
		}
		msgs = append(msgs, domain.Message{Role: domain.RoleAssistant, ToolCalls: calls})
		for _, c := range turn {
			resultID := c.resultID
			if resultID == "" {
				resultID = c.id
			}
			msgs = append(msgs, domain.Message{Role: domain.RoleTool, ToolCallID: resultID, Content: c.content})
		}
	}
	return domain.NewConversation(msgs)
}

// readCall is one call/result pair of the given size, named so the stubs are readable.
func readCall(id string, size int) []callSpec {
	return []callSpec{{id: id, tool: "read", content: strings.Repeat("x", size)}}
}

// contentByID is the content of the tool result answering call id.
func contentByID(t *testing.T, conv *domain.Conversation, id string) string {
	t.Helper()
	for i := 0; i < conv.Len(); i++ {
		if m := conv.At(i); m.Role == domain.RoleTool && m.ToolCallID == id {
			return m.Content
		}
	}
	t.Fatalf("no tool result for call %q", id)
	return ""
}

// TestPruneDoesNothing pins every case where the policy declines: an unknown window, a history
// still under the high fraction, too few tool-calling Turns to spare one, and a history whose
// eligible results are already stubs.
func TestPruneDoesNothing(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		conv   *domain.Conversation
		budget domain.Budget
	}{
		{
			name:   "unknown window never prunes",
			conv:   pruneConv(readCall("a", 500), readCall("b", 500), readCall("c", 500), readCall("d", 500), readCall("e", 500)),
			budget: domain.Budget{CharsPerToken: 1, History: 0},
		},
		{
			name:   "under the high fraction never prunes",
			conv:   pruneConv(readCall("a", 500), readCall("b", 500), readCall("c", 500), readCall("d", 500), readCall("e", 500)),
			budget: domain.Budget{CharsPerToken: 1, History: 100000},
		},
		{
			name:   "fewer tool-calling Turns than the protected window",
			conv:   pruneConv(readCall("a", 500), readCall("b", 500), readCall("c", 500)),
			budget: domain.Budget{CharsPerToken: 1, History: 100},
		},
		{
			name: "already-stubbed results are not re-pruned",
			conv: pruneConv(
				[]callSpec{{id: "a", tool: "read", content: pruneStubPrefix + " 40 lines from read — re-run the call if you need it]"}},
				readCall("b", 10), readCall("c", 10), readCall("d", 10), readCall("e", 10),
			),
			budget: domain.Budget{CharsPerToken: 1, History: 100},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			before := tc.conv.Messages()

			got := Prune(tc.conv, tc.budget, PruneKeepTurns)

			if got != (PruneResult{}) {
				t.Errorf("Prune = %+v, want the zero PruneResult", got)
			}
			for i, m := range tc.conv.Messages() {
				if m.Content != before[i].Content {
					t.Errorf("message %d was rewritten: %q", i, m.Content)
				}
			}
		})
	}
}

// TestPruneProtectsTheRecentToolCallingTurns proves the most recent PruneKeepTurns tool-calling
// Turns survive a pass that prunes everything it is allowed to.
func TestPruneProtectsTheRecentToolCallingTurns(t *testing.T) {
	t.Parallel()
	conv := pruneConv(
		readCall("a", 200), readCall("b", 200),
		readCall("c", 200), readCall("d", 200), readCall("e", 200), readCall("f", 200),
	)

	got := Prune(conv, domain.Budget{CharsPerToken: 1, History: 100}, PruneKeepTurns)

	if got.Pruned != 2 {
		t.Errorf("Pruned = %d, want 2 (only the two Turns outside the protected window)", got.Pruned)
	}
	for _, id := range []string{"a", "b"} {
		if !strings.HasPrefix(contentByID(t, conv, id), pruneStubPrefix) {
			t.Errorf("result %q was not pruned", id)
		}
	}
	for _, id := range []string{"c", "d", "e", "f"} {
		if got := contentByID(t, conv, id); got != strings.Repeat("x", 200) {
			t.Errorf("protected result %q was rewritten: %q", id, got)
		}
	}
	if got.Chars <= 0 {
		t.Errorf("Chars = %d, want the reclaimed characters", got.Chars)
	}
}

// TestPruneOrdersOldestTurnFirst prunes into a Budget that is satisfied by one rewrite: the
// result that goes is the older Turn's, though both Turns hold an equally large one.
func TestPruneOrdersOldestTurnFirst(t *testing.T) {
	t.Parallel()
	conv := pruneConv(
		readCall("old", 1000), readCall("new", 1000),
		readCall("c", 10), readCall("d", 10), readCall("e", 10), readCall("f", 10),
	)

	got := Prune(conv, domain.Budget{CharsPerToken: 1, History: 3000}, PruneKeepTurns)

	if got.Pruned != 1 {
		t.Fatalf("Pruned = %d, want 1 (the pass stops once history is back under the low fraction)", got.Pruned)
	}
	if !strings.HasPrefix(contentByID(t, conv, "old"), pruneStubPrefix) {
		t.Error("the older Turn's result was not the one pruned")
	}
	if got := contentByID(t, conv, "new"); got != strings.Repeat("x", 1000) {
		t.Error("the newer Turn's result was pruned first")
	}
}

// TestPruneOrdersLargestWithinATurn puts the large result AFTER the small one in the same Turn,
// so only size — not position — can explain which is rewritten first.
func TestPruneOrdersLargestWithinATurn(t *testing.T) {
	t.Parallel()
	conv := pruneConv(
		[]callSpec{
			{id: "small", tool: "read", content: strings.Repeat("x", 200)},
			{id: "large", tool: "read", content: strings.Repeat("x", 1000)},
		},
		readCall("later", 200),
		readCall("c", 10), readCall("d", 10), readCall("e", 10), readCall("f", 10),
	)

	got := Prune(conv, domain.Budget{CharsPerToken: 1, History: 2000}, PruneKeepTurns)

	if got.Pruned != 1 {
		t.Fatalf("Pruned = %d, want 1", got.Pruned)
	}
	if !strings.HasPrefix(contentByID(t, conv, "large"), pruneStubPrefix) {
		t.Error("the largest result in the oldest Turn was not pruned first")
	}
	if got := contentByID(t, conv, "small"); got != strings.Repeat("x", 200) {
		t.Error("the smaller result in the same Turn was pruned first")
	}
}

// TestPruneStubNamesTheCall proves the stub carries the line count and the owning call's name
// and argument, and falls back to the tool-less wording when the result answers no call in the
// conversation.
func TestPruneStubNamesTheCall(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		call   callSpec
		result string
		want   string
	}{
		{
			name:   "named call with an argument",
			call:   callSpec{id: "a", tool: "read_file", args: `{"path":"/w/main.go"}`, content: "l1\nl2\nl3"},
			result: "a",
			want:   "[pruned: 3 lines from read_file /w/main.go — re-run the call if you need it]",
		},
		{
			name:   "named call without a usable argument",
			call:   callSpec{id: "a", tool: "read_file", args: `{"start_line":3}`, content: "l1\nl2\nl3"},
			result: "a",
			want:   "[pruned: 3 lines from read_file — re-run the call if you need it]",
		},
		{
			name:   "result answering no call in the conversation",
			call:   callSpec{id: "a", tool: "read_file", args: `{"path":"/w/main.go"}`, content: "l1\nl2\nl3", resultID: "orphan"},
			result: "orphan",
			want:   "[pruned: 3 lines — re-run the call if you need it]",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// The oldest Turn holds the result under test; a bulky second Turn keeps the
			// history over the high fraction so the pass reaches it.
			conv := pruneConv(
				[]callSpec{tc.call},
				readCall("bulk", 2000),
				readCall("c", 10), readCall("d", 10), readCall("e", 10), readCall("f", 10),
			)

			if got := Prune(conv, domain.Budget{CharsPerToken: 1, History: 100}, PruneKeepTurns); got.Pruned == 0 {
				t.Fatal("nothing was pruned")
			}
			if got := contentByID(t, conv, tc.result); got != tc.want {
				t.Errorf("stub = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestPruneArgument pins the argument echoed in a stub: the fixed key precedence, the character
// trim, and the silent fallback on arguments that are not a JSON object.
func TestPruneArgument(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("p", pruneArgMaxChars+40)
	tests := []struct {
		name string
		args string
		want string
	}{
		{"path wins", `{"path":"a","pattern":"b","query":"c","command":"d"}`, "a"},
		{"pattern when there is no path", `{"pattern":"b","query":"c","command":"d"}`, "b"},
		{"query when there is neither", `{"query":"c","command":"d"}`, "c"},
		{"command last", `{"command":"d"}`, "d"},
		{"an empty value is not present", `{"path":"","command":"d"}`, "d"},
		{"no recognised key", `{"start_line":3}`, ""},
		{"trimmed to the ceiling", `{"path":"` + long + `"}`, strings.Repeat("p", pruneArgMaxChars)},
		{"multi-byte trim counts characters", `{"path":"` + strings.Repeat("é", pruneArgMaxChars+5) + `"}`, strings.Repeat("é", pruneArgMaxChars)},
		{"unparseable arguments yield none", `not json`, ""},
		{"absent arguments yield none", ``, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := pruneArgument(json.RawMessage(tc.args)); got != tc.want {
				t.Errorf("pruneArgument(%s) = %q, want %q", tc.args, got, tc.want)
			}
		})
	}
}
