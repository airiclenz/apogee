package context

import (
	"encoding/json"
	"fmt"
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

// fillerTurns is n tool-calling Turns of one sized result each, ids prefixed with name. Every
// fixture count that depends on the protected window is built from it, so PruneKeepTurns stays
// the only place the window's size is written down.
func fillerTurns(name string, n, size int) [][]callSpec {
	turns := make([][]callSpec, 0, n)
	for i := 0; i < n; i++ {
		turns = append(turns, readCall(fmt.Sprintf("%s%d", name, i), size))
	}
	return turns
}

// pruneConvPadded is pruneConv with PruneKeepTurns tiny filler Turns ("keep0"…) appended: they
// fill the protected recent window, so every Turn the caller passes sits outside it and is a
// candidate. The padding is deliberately small, so it moves the fill hardly at all and the
// caller's own sizes decide when the band trips.
func pruneConvPadded(turns ...[]callSpec) *domain.Conversation {
	return pruneConv(append(turns, fillerTurns("keep", PruneKeepTurns, fillerChars)...)...)
}

// fillerChars is one filler result's size and pruneStubAllowance a generous upper bound on the
// length of one stub — what bandHistory must assume a rewrite puts back in place of the result
// it reclaimed.
const (
	fillerChars        = 10
	pruneStubAllowance = 120
)

// bandHistory is a History allocation that sits conv in the middle of the prune band: over
// pruneHighFraction as the conversation stands, and under pruneLowFraction once one result of
// reclaim characters has been rewritten into a stub. Both ends are derived from the band
// constants, so a retuned band re-derives the fixture instead of needing it re-tuned by hand.
func bandHistory(t *testing.T, conv *domain.Conversation, reclaim int) int {
	t.Helper()
	full := float64(domain.PromptChars(conv.Messages(), nil))
	after := full - float64(reclaim) + pruneStubAllowance
	low, high := after/pruneLowFraction, full/pruneHighFraction
	if low >= high {
		t.Fatalf("no History satisfies the band for %.0f chars with %d reclaimed", full, reclaim)
	}
	return int((low + high) / 2)
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
			conv:   pruneConvPadded(readCall("a", 500), readCall("b", 500)),
			budget: domain.Budget{CharsPerToken: 1, History: 0},
		},
		{
			// Padded past the protected window, so the fraction is the only thing declining.
			name:   "under the high fraction never prunes",
			conv:   pruneConvPadded(readCall("a", 500), readCall("b", 500)),
			budget: domain.Budget{CharsPerToken: 1, History: 100000},
		},
		{
			name:   "fewer tool-calling Turns than the protected window",
			conv:   pruneConv(fillerTurns("a", PruneKeepTurns-1, 500)...),
			budget: domain.Budget{CharsPerToken: 1, History: 100},
		},
		{
			// PruneKeepTurns Turns follow the stubbed one, so the protected-index gate passes
			// and the stub check is what declines.
			name: "already-stubbed results are not re-pruned",
			conv: pruneConvPadded(
				[]callSpec{{id: "a", tool: "read", content: pruneStubPrefix + " 40 lines from read — re-run the call if you need it]"}},
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
// Turns survive a pass that prunes everything it is allowed to: PruneKeepTurns + 1 Turns go in,
// and only the oldest one comes back stubbed.
func TestPruneProtectsTheRecentToolCallingTurns(t *testing.T) {
	t.Parallel()
	conv := pruneConvPadded(readCall("a", 200))

	got := Prune(conv, domain.Budget{CharsPerToken: 1, History: 100}, PruneKeepTurns)

	if got.Pruned != 1 {
		t.Errorf("Pruned = %d, want 1 (only the Turn outside the protected window)", got.Pruned)
	}
	if !strings.HasPrefix(contentByID(t, conv, "a"), pruneStubPrefix) {
		t.Error(`result "a" was not pruned`)
	}
	for i := 0; i < PruneKeepTurns; i++ {
		id := fmt.Sprintf("keep%d", i)
		if got := contentByID(t, conv, id); got != strings.Repeat("x", fillerChars) {
			t.Errorf("protected result %q was rewritten: %q", id, got)
		}
	}
	if got.Chars <= 0 {
		t.Errorf("Chars = %d, want the reclaimed characters", got.Chars)
	}
}

// TestPruneBandIsSeventyToFifty pins the band itself. A history at 65% of its History allocation
// is left whole; one at 75% is pruned — but only until the fill is back under the low bound, so
// the pass reclaims what the band asks for and stops with eligible results still intact.
func TestPruneBandIsSeventyToFifty(t *testing.T) {
	t.Parallel()
	// The ratified band and protected window are constants, not configuration.
	if pruneHighFraction != 0.7 || pruneLowFraction != 0.5 || PruneKeepTurns != 6 {
		t.Fatalf("band = %v/%v over %d protected Turns, want 0.7/0.5 over 6", pruneHighFraction, pruneLowFraction, PruneKeepTurns)
	}
	const eligible = 4
	full := domain.PromptChars(pruneConvPadded(fillerTurns("old", eligible, 1000)...).Messages(), nil)

	quiet := pruneConvPadded(fillerTurns("old", eligible, 1000)...)
	if got := Prune(quiet, domain.Budget{CharsPerToken: 1, History: int(float64(full) / 0.65)}, PruneKeepTurns); got != (PruneResult{}) {
		t.Errorf("Prune at 65%% of History = %+v, want the zero PruneResult", got)
	}

	conv := pruneConvPadded(fillerTurns("old", eligible, 1000)...)
	b := domain.Budget{CharsPerToken: 1, History: int(float64(full) / 0.75)}

	got := Prune(conv, b, PruneKeepTurns)

	if got.Pruned == 0 {
		t.Fatal("Prune at 75% of History did nothing, want a pass")
	}
	if got.Pruned == eligible {
		t.Errorf("Pruned = %d, want fewer than the %d eligible results — the pass stops at the low bound", got.Pruned, eligible)
	}
	if b.HistoryExceedsFraction(conv.Messages(), pruneLowFraction) {
		t.Error("the pass stopped with the history still over the low bound")
	}
}

// TestPruneOrdersOldestTurnFirst prunes into a Budget that is satisfied by one rewrite: the
// result that goes is the older Turn's, though both Turns hold an equally large one.
func TestPruneOrdersOldestTurnFirst(t *testing.T) {
	t.Parallel()
	conv := pruneConvPadded(readCall("old", 1000), readCall("new", 1000))

	got := Prune(conv, domain.Budget{CharsPerToken: 1, History: bandHistory(t, conv, 1000)}, PruneKeepTurns)

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
	conv := pruneConvPadded(
		[]callSpec{
			{id: "small", tool: "read", content: strings.Repeat("x", 200)},
			{id: "large", tool: "read", content: strings.Repeat("x", 1000)},
		},
		readCall("later", 200),
	)

	got := Prune(conv, domain.Budget{CharsPerToken: 1, History: bandHistory(t, conv, 1000)}, PruneKeepTurns)

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
			conv := pruneConvPadded(
				[]callSpec{tc.call},
				readCall("bulk", 2000),
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
