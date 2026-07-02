package context

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// fakeCompleter is a deterministic Completer for the reducer tests: it returns a canned reply
// (or error) and captures the messages it was handed so a test can assert the summary call
// carried the transcript.
type fakeCompleter struct {
	reply string
	err   error

	calls int
	got   []domain.Message
}

func (f *fakeCompleter) Complete(_ context.Context, msgs []domain.Message) (string, error) {
	f.calls++
	f.got = msgs
	return f.reply, f.err
}

// convOf builds a Conversation over the given messages (the engine seam copies them).
func convOf(msgs ...domain.Message) *domain.Conversation { return domain.NewConversation(msgs) }

func msg(role domain.Role, content string) domain.Message {
	return domain.Message{Role: role, Content: content}
}

func TestCompactReplacesTailWithSummaryKeepingPrefix(t *testing.T) {
	conv := convOf(
		msg(domain.RoleUser, "build me a parser"),   // protected prefix (first user message)
		msg(domain.RoleAssistant, "starting on it"), // …folded from here down
		msg(domain.RoleUser, "add error handling"),
		msg(domain.RoleAssistant, "done"),
	)
	c := &fakeCompleter{reply: "the user wants a parser with error handling"}

	res, err := Compact(context.Background(), c, conv, 0)
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if c.calls != 1 {
		t.Fatalf("Completer calls = %d, want 1", c.calls)
	}
	if res.Skipped {
		t.Fatal("Result.Skipped = true, want a real compaction")
	}
	if res.Before != 4 || res.After != 2 {
		t.Errorf("Result = {Before:%d After:%d}, want {4 2}", res.Before, res.After)
	}
	if conv.Len() != 2 {
		t.Fatalf("conv.Len() = %d after compaction, want 2 (prefix + summary)", conv.Len())
	}
	// The protected prefix survives verbatim.
	if got := conv.At(0); got.Role != domain.RoleUser || got.Content != "build me a parser" {
		t.Errorf("prefix message not preserved: %+v", got)
	}
	// The tail is one assistant summary message carrying the model's text.
	sum := conv.At(1)
	if sum.Role != domain.RoleAssistant {
		t.Errorf("summary role = %q, want assistant (clean alternation)", sum.Role)
	}
	if !strings.HasPrefix(sum.Content, summaryMessagePrefix) {
		t.Errorf("summary missing its label prefix:\n%s", sum.Content)
	}
	if !strings.Contains(sum.Content, "parser with error handling") {
		t.Errorf("summary missing the model's text:\n%s", sum.Content)
	}
}

func TestCompactKeepsLeadingSystemInPrefix(t *testing.T) {
	conv := convOf(
		msg(domain.RoleSystem, "SYSTEM PREFIX"),
		msg(domain.RoleUser, "first task"),
		msg(domain.RoleAssistant, "ok"),
		msg(domain.RoleUser, "second task"),
		msg(domain.RoleAssistant, "ok again"),
	)
	c := &fakeCompleter{reply: "summary"}
	if _, err := Compact(context.Background(), c, conv, 0); err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if conv.Len() != 3 {
		t.Fatalf("conv.Len() = %d, want 3 (system + first user + summary)", conv.Len())
	}
	if conv.At(0).Role != domain.RoleSystem || conv.At(1).Role != domain.RoleUser {
		t.Errorf("protected prefix not kept: [%s, %s]", conv.At(0).Role, conv.At(1).Role)
	}
}

func TestCompactSkipsTinyConversation(t *testing.T) {
	conv := convOf(
		msg(domain.RoleUser, "hi"),
		msg(domain.RoleAssistant, "hello"), // only one message past the prefix — nothing to fold
	)
	c := &fakeCompleter{reply: "unused"}

	res, err := Compact(context.Background(), c, conv, 0)
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if !res.Skipped {
		t.Errorf("Result.Skipped = false, want true for a tiny conversation")
	}
	if c.calls != 0 {
		t.Errorf("Completer called %d times on a skip; want 0 (no upstream cost)", c.calls)
	}
	if conv.Len() != 2 {
		t.Errorf("conv mutated on a skip: Len = %d, want 2", conv.Len())
	}
}

func TestCompactCompleterErrorLeavesConvUntouched(t *testing.T) {
	conv := convOf(
		msg(domain.RoleUser, "a"),
		msg(domain.RoleAssistant, "b"),
		msg(domain.RoleUser, "c"),
		msg(domain.RoleAssistant, "d"),
	)
	want := conv.Len()
	c := &fakeCompleter{err: errors.New("upstream boom")}

	if _, err := Compact(context.Background(), c, conv, 0); err == nil {
		t.Fatal("Compact err = nil, want the Completer's error surfaced")
	}
	if conv.Len() != want {
		t.Errorf("conv mutated despite a Completer error: Len = %d, want %d", conv.Len(), want)
	}
}

func TestCompactEmptySummaryLeavesConvUntouched(t *testing.T) {
	conv := convOf(
		msg(domain.RoleUser, "a"),
		msg(domain.RoleAssistant, "b"),
		msg(domain.RoleUser, "c"),
		msg(domain.RoleAssistant, "d"),
	)
	want := conv.Len()
	c := &fakeCompleter{reply: "   \n  "} // whitespace only

	if _, err := Compact(context.Background(), c, conv, 0); !errors.Is(err, errEmptySummary) {
		t.Fatalf("Compact err = %v, want errEmptySummary", err)
	}
	if conv.Len() != want {
		t.Errorf("conv mutated despite an empty summary: Len = %d, want %d", conv.Len(), want)
	}
}

func TestCompactSendsSystemPromptAndTranscript(t *testing.T) {
	conv := convOf(
		msg(domain.RoleUser, "REMEMBER-THE-GOAL"),
		msg(domain.RoleAssistant, "ACK-ONE"),
		msg(domain.RoleUser, "NEXT-STEP"),
		msg(domain.RoleAssistant, "ACK-TWO"),
	)
	c := &fakeCompleter{reply: "ok"}
	if _, err := Compact(context.Background(), c, conv, 0); err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if len(c.got) != 2 {
		t.Fatalf("summary call carried %d messages, want 2 (system + user)", len(c.got))
	}
	if c.got[0].Role != domain.RoleSystem || c.got[0].Content != summaryInstruction {
		t.Errorf("first message is not the summarizer system prompt: %+v", c.got[0])
	}
	body := c.got[1].Content
	for _, want := range []string{"REMEMBER-THE-GOAL", "ACK-ONE", "NEXT-STEP", "ACK-TWO"} {
		if !strings.Contains(body, want) {
			t.Errorf("transcript sent to the model is missing %q:\n%s", want, body)
		}
	}
}

func TestRenderTranscriptIncludesRolesContentAndToolCalls(t *testing.T) {
	got := renderTranscript([]domain.Message{
		{Role: domain.RoleUser, Content: "do a thing"},
		{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{
			{ID: "1", Tool: "read_file", Arguments: json.RawMessage(`{"path":"main.go"}`)},
		}},
		{Role: domain.RoleTool, Content: "file contents here"},
	})
	for _, want := range []string{"[user]", "do a thing", "[assistant]", "read_file", "main.go", "[tool]", "file contents here"} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered transcript missing %q:\n%s", want, got)
		}
	}
}

// TestRenderBudgetedTranscriptUnboundedMatchesFullRender pins that a non-positive budget (the
// window-unknown case) renders the whole conversation exactly as renderTranscript — the pre-item-6
// behaviour, unchanged.
func TestRenderBudgetedTranscriptUnboundedMatchesFullRender(t *testing.T) {
	msgs := []domain.Message{
		{Role: domain.RoleUser, Content: "goal"},
		{Role: domain.RoleAssistant, Content: "step one"},
		{Role: domain.RoleUser, Content: "step two"},
		{Role: domain.RoleAssistant, Content: "done"},
	}
	if got, want := renderBudgetedTranscript(msgs, 1, 0), renderTranscript(msgs); got != want {
		t.Errorf("unbounded budget diverged from full render:\n got: %q\nwant: %q", got, want)
	}
	if got, want := renderBudgetedTranscript(msgs, 1, -5), renderTranscript(msgs); got != want {
		t.Errorf("negative budget diverged from full render:\n got: %q\nwant: %q", got, want)
	}
}

// TestRenderBudgetedTranscriptFitsRendersEverythingWithoutElision pins that when the whole
// rendering fits the budget, nothing is dropped and no elision notice appears.
func TestRenderBudgetedTranscriptFitsRendersEverythingWithoutElision(t *testing.T) {
	msgs := []domain.Message{
		{Role: domain.RoleUser, Content: "goal"},
		{Role: domain.RoleAssistant, Content: "step one"},
		{Role: domain.RoleUser, Content: "step two"},
		{Role: domain.RoleAssistant, Content: "done"},
	}
	got := renderBudgetedTranscript(msgs, 1, 100_000)
	if got != renderTranscript(msgs) {
		t.Errorf("a fitting transcript was altered:\n got: %q\nwant: %q", got, renderTranscript(msgs))
	}
	if strings.Contains(got, "omitted") {
		t.Errorf("elision notice present though everything fit:\n%s", got)
	}
}

// TestRenderBudgetedTranscriptElidesMiddleKeepingPrefixAndTail is the core item-6 assertion: an
// over-budget transcript keeps the protected prefix and the most recent tail, drops the middle,
// and marks the gap — so the summary call the reducer sends stays within its budget.
func TestRenderBudgetedTranscriptElidesMiddleKeepingPrefixAndTail(t *testing.T) {
	prefix := domain.Message{Role: domain.RoleUser, Content: "OVERARCHING-GOAL"}
	last := domain.Message{Role: domain.RoleAssistant, Content: "MOST-RECENT-REPLY"}
	mid := func(i int) domain.Message {
		return domain.Message{Role: domain.RoleUser, Content: fmt.Sprintf("MIDDLE-%d %s", i, strings.Repeat("x", 80))}
	}
	msgs := []domain.Message{prefix, mid(1), mid(2), mid(3), mid(4), last}

	// A budget that fits the prefix and the last message but nothing between them.
	budget := len(renderMessage(prefix)) + len(renderMessage(last)) + 8
	got := renderBudgetedTranscript(msgs, 1, budget)

	if !strings.Contains(got, "OVERARCHING-GOAL") {
		t.Errorf("protected prefix dropped:\n%s", got)
	}
	if !strings.Contains(got, "MOST-RECENT-REPLY") {
		t.Errorf("most recent message dropped:\n%s", got)
	}
	if strings.Contains(got, "MIDDLE-") {
		t.Errorf("an over-budget middle message survived:\n%s", got)
	}
	if !strings.Contains(got, "4 earlier message(s) omitted") {
		t.Errorf("missing/incorrect elision notice for the 4 dropped middles:\n%s", got)
	}
}

// TestRenderBudgetedTranscriptAlwaysKeepsMostRecentMessage pins the tail guarantee: even when the
// single most recent message alone exceeds the budget, it is kept (the next turn depends on it)
// rather than the transcript collapsing to prefix-only.
func TestRenderBudgetedTranscriptAlwaysKeepsMostRecentMessage(t *testing.T) {
	msgs := []domain.Message{
		{Role: domain.RoleUser, Content: "goal"},
		{Role: domain.RoleAssistant, Content: "older"},
		{Role: domain.RoleUser, Content: "RECENT-" + strings.Repeat("y", 500)},
	}
	got := renderBudgetedTranscript(msgs, 1, 10) // absurdly small budget
	if !strings.Contains(got, "RECENT-") {
		t.Errorf("most recent message not kept under a tiny budget:\n%s", got)
	}
}

// TestCompactAppliesTranscriptBudget proves the budget threads through Compact into the summary
// call: with a small budget an over-budget conversation's request carries the prefix and recent
// tail plus an elision notice, not the earliest middle turns.
func TestCompactAppliesTranscriptBudget(t *testing.T) {
	msgs := []domain.Message{msg(domain.RoleUser, "REMEMBER-THE-GOAL")}
	for i := 0; i < 8; i++ {
		msgs = append(msgs, msg(domain.RoleAssistant, fmt.Sprintf("MIDDLE-%d %s", i, strings.Repeat("z", 60))))
	}
	msgs = append(msgs, msg(domain.RoleUser, "FINAL-STEP"))
	conv := convOf(msgs...)
	c := &fakeCompleter{reply: "ok"}

	budget := len(renderMessage(msgs[0])) + len(renderMessage(msgs[len(msgs)-1])) + 8
	if _, err := Compact(context.Background(), c, conv, budget); err != nil {
		t.Fatalf("Compact: %v", err)
	}
	body := c.got[1].Content
	if !strings.Contains(body, "REMEMBER-THE-GOAL") || !strings.Contains(body, "FINAL-STEP") {
		t.Errorf("budgeted summary request missing prefix or recent tail:\n%s", body)
	}
	if !strings.Contains(body, "omitted to fit the compaction budget") {
		t.Errorf("budgeted summary request missing the elision notice:\n%s", body)
	}
	if strings.Contains(body, "MIDDLE-0") {
		t.Errorf("budgeted summary request still carries an early middle turn:\n%s", body)
	}
}
