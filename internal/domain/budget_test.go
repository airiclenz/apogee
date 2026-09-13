package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestBudgetEstimateTokens pins the single chars→token implementation (D4): ceil
// rounding so a part is never estimated to fit when it is one token over, and 0 on a
// non-positive ratio (the zero-value Budget) so token-gated comparisons stay inert
// until the ratio is calibrated.
func TestBudgetEstimateTokens(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		ratio float64
		chars int
		want  int
	}{
		{"zero ratio is inert", 0, 400, 0},
		{"negative ratio is inert", -3, 400, 0},
		{"zero chars", 4, 0, 0},
		{"exact divisor", 4, 400, 100},
		{"one char over rounds up", 4, 401, 101},
		{"one char under stays within the ceil", 4, 399, 100},
		{"single char rounds up to one token", 4, 1, 1},
		{"fractional ratio", 2.5, 6, 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Budget{CharsPerToken: tc.ratio}.EstimateTokens(tc.chars)
			if got != tc.want {
				t.Errorf("Budget{CharsPerToken: %v}.EstimateTokens(%d) = %d, want %d",
					tc.ratio, tc.chars, got, tc.want)
			}
		})
	}
}

// TestBudgetHistoryExceedsAllocation transliterates the context-level trigger cases
// onto the domain compare: strict >, a non-positive History allocation never trips
// (the no-basis case), and an uncalibrated ratio keeps the compare inert.
func TestBudgetHistoryExceedsAllocation(t *testing.T) {
	t.Parallel()
	// 400 chars ⇒ 100 estimated tokens at 4 chars/token.
	msgs := []Message{{Role: RoleUser, Content: strings.Repeat("x", 400)}}
	tests := []struct {
		name   string
		budget Budget
		msgs   []Message
		want   bool
	}{
		{"zero History allocation never trips", Budget{CharsPerToken: 4}, msgs, false},
		{"negative History allocation never trips", Budget{CharsPerToken: 4, History: -5}, msgs, false},
		{"at exactly the allocation does not trip (strict >)", Budget{CharsPerToken: 4, History: 100}, msgs, false},
		{"below the allocation does not trip", Budget{CharsPerToken: 4, History: 200}, msgs, false},
		{"above the allocation trips", Budget{CharsPerToken: 4, History: 99}, msgs, true},
		{"empty history never trips", Budget{CharsPerToken: 4, History: 50}, nil, false},
		{"uncalibrated ratio is inert", Budget{History: 50}, msgs, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.budget.HistoryExceedsAllocation(tc.msgs); got != tc.want {
				t.Errorf("%+v.HistoryExceedsAllocation(msgs) = %v, want %v", tc.budget, got, tc.want)
			}
		})
	}
}

// TestBudgetHistoryExceedsFraction pins the movable line: the same strict >, the same inert
// cases as HistoryExceedsAllocation, and — at fraction 1.0 — the same answer as the sibling,
// so a reducer reading a fraction and a trigger reading the allocation cannot disagree.
func TestBudgetHistoryExceedsFraction(t *testing.T) {
	t.Parallel()
	// 400 chars ⇒ 100 estimated tokens at 4 chars/token.
	msgs := []Message{{Role: RoleUser, Content: strings.Repeat("x", 400)}}
	tests := []struct {
		name     string
		budget   Budget
		fraction float64
		want     bool
	}{
		{"above the fraction trips", Budget{CharsPerToken: 4, History: 200}, 0.4, true},
		{"at exactly the fraction does not trip (strict >)", Budget{CharsPerToken: 4, History: 200}, 0.5, false},
		{"below the fraction does not trip", Budget{CharsPerToken: 4, History: 200}, 0.6, false},
		{"zero History allocation never trips", Budget{CharsPerToken: 4}, 0.4, false},
		{"negative History allocation never trips", Budget{CharsPerToken: 4, History: -5}, 0.4, false},
		{"uncalibrated ratio is inert", Budget{History: 50}, 0.4, false},
		{"non-positive fraction is inert", Budget{CharsPerToken: 4, History: 200}, 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.budget.HistoryExceedsFraction(msgs, tc.fraction); got != tc.want {
				t.Errorf("%+v.HistoryExceedsFraction(msgs, %v) = %v, want %v",
					tc.budget, tc.fraction, got, tc.want)
			}
		})
	}

	t.Run("fraction 1.0 agrees with HistoryExceedsAllocation", func(t *testing.T) {
		t.Parallel()
		for _, history := range []int{0, -5, 50, 99, 100, 101, 200} {
			b := Budget{CharsPerToken: 4, History: history}
			if got, want := b.HistoryExceedsFraction(msgs, 1.0), b.HistoryExceedsAllocation(msgs); got != want {
				t.Errorf("History=%d: HistoryExceedsFraction(msgs, 1.0) = %v, HistoryExceedsAllocation = %v",
					history, got, want)
			}
		}
	})
}

// TestPromptChars_CountsContentToolArgsAndMenu proves the char measure sums message contents,
// tool-call arguments, and the tool menu's names/descriptions/schemas — the same components on both
// sides of the ratio.
func TestPromptChars_CountsContentToolArgsAndMenu(t *testing.T) {
	t.Parallel()
	msgs := []Message{
		{Role: RoleUser, Content: "abcde"}, // 5
		{Role: RoleAssistant, ToolCalls: []ToolCall{
			{Tool: "read", Arguments: json.RawMessage(`{"p":"x"}`)}, // 4 + 9 = 13
		}},
	}
	tools := []ToolDef{
		{Name: "read", Description: "reads", Schema: json.RawMessage(`{}`)}, // 4 + 5 + 2 = 11
	}

	if got := PromptChars(msgs, tools); got != 5+13+11 {
		t.Errorf("PromptChars = %d, want %d", got, 5+13+11)
	}
}

// TestBudgetHistoryFill pins the fill measure the context-fill notice reports (ADR 0077):
// estimated tokens over the History allocation, through the same ceil rounding as
// EstimateTokens, and 0 — inert, no substitute ceiling — on an unknown window or an
// uncalibrated ratio, the same clauses that keep the sibling compares false.
func TestBudgetHistoryFill(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		history       int
		charsPerToken float64
		chars         int
		want          float64
	}{
		{"zero History is inert", 0, 4, 400, 0},
		{"negative History is inert", -5, 4, 400, 0},
		{"zero ratio is inert", 100, 0, 400, 0},
		{"negative ratio is inert", 100, -3, 400, 0},
		{"empty history fills nothing", 100, 4, 0, 0},
		{"half way", 200, 4, 400, 0.5},
		{"exactly at the allocation", 100, 4, 400, 1},
		{"one char over rounds up past the allocation", 100, 4, 401, 1.01},
		{"past the allocation keeps climbing", 100, 4, 800, 2},
		{"fractional ratio ceils like EstimateTokens", 10, 2.5, 6, 0.3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			b := Budget{History: tc.history, CharsPerToken: tc.charsPerToken}
			if got := b.HistoryFill(tc.chars); got != tc.want {
				t.Errorf("%+v.HistoryFill(%d) = %v, want %v", b, tc.chars, got, tc.want)
			}
		})
	}
}

// TestBudgetHistoryFill_AgreesWithHistoryExceedsAllocation pins the invariant the notice and
// the automatic Compaction trigger share one scale on: for a view over msgs,
// HistoryExceedsAllocation(msgs) == (HistoryFill(ConversationChars(conv)) > 1.0), held one
// char under, exactly at, and one char over the History boundary — where EstimateTokens'
// ceil makes the equality tightest — and across the inert cases.
func TestBudgetHistoryFill_AgreesWithHistoryExceedsAllocation(t *testing.T) {
	t.Parallel()
	// History 100 at 4 chars/token puts the boundary at exactly 400 chars.
	tests := []struct {
		name   string
		budget Budget
		chars  int
	}{
		{"one char under the boundary", Budget{CharsPerToken: 4, History: 100}, 399},
		{"exactly at the boundary", Budget{CharsPerToken: 4, History: 100}, 400},
		{"one char over the boundary", Budget{CharsPerToken: 4, History: 100}, 401},
		{"well under", Budget{CharsPerToken: 4, History: 100}, 10},
		{"well over", Budget{CharsPerToken: 4, History: 100}, 4000},
		{"fractional ratio at the boundary", Budget{CharsPerToken: 2.5, History: 10}, 25},
		{"fractional ratio one char over", Budget{CharsPerToken: 2.5, History: 10}, 26},
		{"unknown window", Budget{CharsPerToken: 4}, 4000},
		{"uncalibrated ratio", Budget{History: 100}, 4000},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			msgs := []Message{{Role: RoleUser, Content: strings.Repeat("x", tc.chars)}}
			conv := NewRequest("", msgs, nil, Budget{}, 0).View().Conversation()

			exceeds := tc.budget.HistoryExceedsAllocation(msgs)
			fill := tc.budget.HistoryFill(ConversationChars(conv))

			if exceeds != (fill > 1.0) {
				t.Errorf("HistoryExceedsAllocation = %v but HistoryFill = %v (chars %d, %+v)",
					exceeds, fill, tc.chars, tc.budget)
			}
		})
	}
}

// TestConversationChars_MatchesPromptChars proves the view-side measure is PromptChars with
// no tool menu — contents plus each tool call's name and arguments, tool results included —
// over the same messages, through the loop's own view (NewRequest(...).View().Conversation(),
// the shape domaintest.FakeLoopView serves).
func TestConversationChars_MatchesPromptChars(t *testing.T) {
	t.Parallel()
	msgs := []Message{
		{Role: RoleSystem, Content: "be brief"},
		{Role: RoleUser, Content: "read the file"},
		{Role: RoleAssistant, Content: "on it", ToolCalls: []ToolCall{
			{ID: "c1", Tool: "read", Arguments: json.RawMessage(`{"path":"a.go"}`)},
			{ID: "c2", Tool: "ls", Arguments: json.RawMessage(`{}`)},
		}},
		{Role: RoleTool, ToolCallID: "c1", Content: "package main"},
		{Role: RoleTool, ToolCallID: "c2", Content: "a.go\nb.go"},
		{Role: RoleAssistant, Content: "done"},
	}
	conv := NewRequest("", msgs, nil, Budget{}, 0).View().Conversation()

	got, want := ConversationChars(conv), PromptChars(msgs, nil)

	if got != want {
		t.Errorf("ConversationChars(conv) = %d, PromptChars(msgs, nil) = %d", got, want)
	}
	if want == 0 {
		t.Fatal("fixture measures 0 chars — the comparison proves nothing")
	}
}
