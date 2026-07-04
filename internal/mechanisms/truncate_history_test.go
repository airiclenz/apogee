package mechanisms

import (
	"context"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// asstCall is an assistant message that issues one tool call — the head of an assistant-anchored
// exchange, the only place truncate_history is allowed to cut.
func asstCall(id string) domain.Message {
	return domain.Message{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: id, Tool: "read_file"}}}
}

// toolResult is the result message paired to a call — it must stay adjacent to the assistant call
// that produced it (strict chat templates reject an orphaned tool message).
func toolResult(id, content string) domain.Message {
	return domain.Message{Role: domain.RoleTool, ToolCallID: id, Content: content}
}

// refTruncate is the reference oracle: apogee-sim internal/sim/intervention.go truncateHistory
// @pin, transliterated onto domain.Message. The property test asserts the Mechanism's output
// matches this over generated histories, so the port stays behaviour-faithful to the pinned source.
func refTruncate(msgs []domain.Message, keep int, note string) []domain.Message {
	if keep <= 0 {
		return msgs
	}
	prefixEnd := 0
	for prefixEnd < len(msgs) && msgs[prefixEnd].Role == domain.RoleSystem {
		prefixEnd++
	}
	if prefixEnd < len(msgs) && msgs[prefixEnd].Role == domain.RoleUser {
		prefixEnd++
	}
	tailStart := len(msgs)
	remaining := keep
	for i := len(msgs) - 1; i >= prefixEnd; i-- {
		if msgs[i].Role == domain.RoleAssistant {
			tailStart = i
			remaining--
			if remaining == 0 {
				break
			}
		}
	}
	if remaining > 0 || tailStart <= prefixEnd {
		return msgs
	}
	out := make([]domain.Message, 0, len(msgs))
	out = append(out, msgs[:prefixEnd]...)
	if note != "" {
		out = append(out, domain.Message{Role: domain.RoleUser, Content: note})
	}
	return append(out, msgs[tailStart:]...)
}

// sameMessages compares by the fields truncation touches — role, content, tool-call linkage — so a
// mismatch points at a structural divergence rather than at unexported wire extras.
func sameMessages(a, b []domain.Message) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Role != b[i].Role || a[i].Content != b[i].Content || a[i].ToolCallID != b[i].ToolCallID {
			return false
		}
		if len(a[i].ToolCalls) != len(b[i].ToolCalls) {
			return false
		}
		for j := range a[i].ToolCalls {
			if a[i].ToolCalls[j].ID != b[i].ToolCalls[j].ID {
				return false
			}
		}
	}
	return true
}

// generatedHistories returns a spread of conversation shapes covering the branches the guard and
// the prefix/boundary logic take: varying prefix (multi-system, no-system, system-only), plain-text
// assistant exchanges (no tool call), and interleaved user feedback between exchanges.
func generatedHistories() []struct {
	name string
	msgs []domain.Message
} {
	return []struct {
		name string
		msgs []domain.Message
	}{
		{
			name: "system + user + four tool exchanges",
			msgs: []domain.Message{
				{Role: domain.RoleSystem, Content: "sys"},
				{Role: domain.RoleUser, Content: "u1"},
				asstCall("c1"), toolResult("c1", "r1"),
				asstCall("c2"), toolResult("c2", "r2"),
				asstCall("c3"), toolResult("c3", "r3"),
				asstCall("c4"), toolResult("c4", "r4"),
			},
		},
		{
			name: "interleaved user feedback between exchanges",
			msgs: []domain.Message{
				{Role: domain.RoleSystem, Content: "sys"},
				{Role: domain.RoleUser, Content: "u1"},
				asstCall("c1"), toolResult("c1", "r1"),
				{Role: domain.RoleUser, Content: "feedback"},
				asstCall("c2"), toolResult("c2", "r2"),
				asstCall("c3"), toolResult("c3", "r3"),
			},
		},
		{
			name: "plain-text assistant exchanges (no tool calls)",
			msgs: []domain.Message{
				{Role: domain.RoleSystem, Content: "sys"},
				{Role: domain.RoleUser, Content: "u1"},
				{Role: domain.RoleAssistant, Content: "a1"},
				{Role: domain.RoleUser, Content: "u2"},
				{Role: domain.RoleAssistant, Content: "a2"},
				{Role: domain.RoleUser, Content: "u3"},
				{Role: domain.RoleAssistant, Content: "a3"},
			},
		},
		{
			name: "multiple leading system messages",
			msgs: []domain.Message{
				{Role: domain.RoleSystem, Content: "sys1"},
				{Role: domain.RoleSystem, Content: "sys2"},
				{Role: domain.RoleUser, Content: "u1"},
				asstCall("c1"), toolResult("c1", "r1"),
				asstCall("c2"), toolResult("c2", "r2"),
				asstCall("c3"), toolResult("c3", "r3"),
			},
		},
		{
			name: "no system message",
			msgs: []domain.Message{
				{Role: domain.RoleUser, Content: "u1"},
				asstCall("c1"), toolResult("c1", "r1"),
				asstCall("c2"), toolResult("c2", "r2"),
				asstCall("c3"), toolResult("c3", "r3"),
			},
		},
	}
}

// The Mechanism's output matches the pinned-sim oracle over every generated shape and every keep
// window, and — when it truncates — respects the protected prefix, cuts only at an assistant
// boundary, keeps tool results adjacent to their calls, and inserts the gap note exactly once.
func TestTruncateHistoryProperties(t *testing.T) {
	t.Parallel()
	for _, tc := range generatedHistories() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for keep := 1; keep <= 5; keep++ {
				m := truncateHistoryMechanism{keepLastTurns: keep, gapNote: truncateGapNote}
				conv := domain.NewConversation(tc.msgs)
				if err := m.RewriteHistory(context.Background(), conv); err != nil {
					t.Fatalf("keep=%d: RewriteHistory: %v", keep, err)
				}
				got := conv.Messages()
				want := refTruncate(tc.msgs, keep, truncateGapNote)
				if !sameMessages(got, want) {
					t.Fatalf("keep=%d: output diverged from the sim oracle\n got: %+v\nwant: %+v", keep, got, want)
				}

				prefixEnd := domain.NewConversation(tc.msgs).PrefixEnd()
				truncated := !sameMessages(want, tc.msgs)
				if !truncated {
					// Nothing to drop: it must be a byte-for-byte no-op and book no fire (Revision unmoved).
					if !sameMessages(got, tc.msgs) {
						t.Errorf("keep=%d: no-op case mutated the history", keep)
					}
					if conv.Revision() != 0 {
						t.Errorf("keep=%d: no-op case bumped Revision to %d (would book a phantom fire)", keep, conv.Revision())
					}
					continue
				}

				// Protected prefix survives verbatim.
				if !sameMessages(got[:prefixEnd], tc.msgs[:prefixEnd]) {
					t.Errorf("keep=%d: protected prefix [0:%d) was altered", keep, prefixEnd)
				}
				// The gap note sits at the cut, exactly once.
				if got[prefixEnd].Role != domain.RoleUser || got[prefixEnd].Content != truncateGapNote {
					t.Errorf("keep=%d: message at the cut = %+v, want the gap note", keep, got[prefixEnd])
				}
				if n := countGapNote(got); n != 1 {
					t.Errorf("keep=%d: gap note appears %d times, want exactly 1", keep, n)
				}
				// The tail begins at an assistant boundary, so no tool result is orphaned.
				if got[prefixEnd+1].Role != domain.RoleAssistant {
					t.Errorf("keep=%d: tail begins with %s, want a cut at an assistant boundary", keep, got[prefixEnd+1].Role)
				}
				if orphan, id := hasOrphanToolResult(got); orphan {
					t.Errorf("keep=%d: tool result %q has no preceding call (cut broke adjacency)", keep, id)
				}
			}
		})
	}
}

// countGapNote counts the static gap-note user messages in msgs.
func countGapNote(msgs []domain.Message) int {
	n := 0
	for _, m := range msgs {
		if m.Role == domain.RoleUser && m.Content == truncateGapNote {
			n++
		}
	}
	return n
}

// hasOrphanToolResult reports whether any tool-result message lacks an earlier assistant call with
// the matching ID — the adjacency invariant a boundary cut must preserve.
func hasOrphanToolResult(msgs []domain.Message) (bool, string) {
	seen := make(map[string]bool)
	for _, m := range msgs {
		for _, call := range m.ToolCalls {
			seen[call.ID] = true
		}
		if m.Role == domain.RoleTool && !seen[m.ToolCallID] {
			return true, m.ToolCallID
		}
	}
	return false, ""
}

// The keep=1 shape matches apogee-sim's TestApplyIntervention_TruncateHistoryKeepsPrefixAndTail
// @pin exactly, gap note included: prefix + gap note + the last assistant-anchored exchange.
func TestTruncateHistoryKeepsPrefixAndTail(t *testing.T) {
	t.Parallel()
	msgs := []domain.Message{
		{Role: domain.RoleSystem, Content: "s"},
		{Role: domain.RoleUser, Content: "u1"},
		asstCall("call_1"), toolResult("call_1", "old result"),
		{Role: domain.RoleUser, Content: "feedback"},
		asstCall("call_2"), toolResult("call_2", "recent result"),
	}
	m := truncateHistoryMechanism{keepLastTurns: 1, gapNote: "earlier turns omitted"}
	conv := domain.NewConversation(msgs)
	if err := m.RewriteHistory(context.Background(), conv); err != nil {
		t.Fatalf("RewriteHistory: %v", err)
	}

	want := []domain.Message{
		{Role: domain.RoleSystem, Content: "s"},
		{Role: domain.RoleUser, Content: "u1"},
		{Role: domain.RoleUser, Content: "earlier turns omitted"},
		asstCall("call_2"),
		toolResult("call_2", "recent result"),
	}
	if got := conv.Messages(); !sameMessages(got, want) {
		t.Errorf("output = %+v\nwant %+v", got, want)
	}
	// The original history handed in is never mutated (the Conversation copies it).
	if len(msgs) != 7 {
		t.Errorf("input history was mutated, len = %d, want 7", len(msgs))
	}
}

// An empty gap note truncates without inserting anything at the cut (the sim's no-note variant).
func TestTruncateHistoryNoGapNote(t *testing.T) {
	t.Parallel()
	msgs := []domain.Message{
		{Role: domain.RoleSystem, Content: "s"},
		{Role: domain.RoleUser, Content: "u1"},
		asstCall("c1"), toolResult("c1", "first result"),
		asstCall("c2"), toolResult("c2", "second result"),
	}
	m := truncateHistoryMechanism{keepLastTurns: 1, gapNote: ""}
	conv := domain.NewConversation(msgs)
	if err := m.RewriteHistory(context.Background(), conv); err != nil {
		t.Fatalf("RewriteHistory: %v", err)
	}
	got := conv.Messages()
	if len(got) != 4 {
		t.Fatalf("len = %d, want 4 (no gap note)", len(got))
	}
	if got[2].Role != domain.RoleAssistant || got[3].Content != "second result" {
		t.Errorf("tail should start at the last assistant exchange, got %+v", got[2:])
	}
}

// Keeping at least as many exchanges as exist is a no-op — nothing to drop, no gap note, no fire.
func TestTruncateHistoryNothingToDrop(t *testing.T) {
	t.Parallel()
	msgs := []domain.Message{
		{Role: domain.RoleSystem, Content: "s"},
		{Role: domain.RoleUser, Content: "u1"},
		asstCall("c1"), toolResult("c1", "first result"),
		asstCall("c2"), toolResult("c2", "second result"),
	}
	for _, keep := range []int{2, 5} {
		m := truncateHistoryMechanism{keepLastTurns: keep, gapNote: "must not appear"}
		conv := domain.NewConversation(msgs)
		if err := m.RewriteHistory(context.Background(), conv); err != nil {
			t.Fatalf("keep=%d: RewriteHistory: %v", keep, err)
		}
		if got := conv.Messages(); !sameMessages(got, msgs) {
			t.Errorf("keep=%d: expected a no-op, got %+v", keep, got)
		}
		if conv.Revision() != 0 {
			t.Errorf("keep=%d: no-op bumped Revision to %d", keep, conv.Revision())
		}
	}
}

// Re-running the rewrite on an already-truncated, ungrown history is a genuine no-op: the only
// pending drop is the gap note inserted last time, so re-dropping and re-inserting it would
// rebuild the identical shape while bumping Revision — the phantom acted-fire the loop keys on
// (R4). The rewrite must leave both the content and Revision untouched, then truncate and book
// normally once the history actually grows past the window again.
func TestTruncateHistoryRerunNoPhantomFire(t *testing.T) {
	t.Parallel()
	msgs := []domain.Message{
		{Role: domain.RoleSystem, Content: "s"},
		{Role: domain.RoleUser, Content: "u1"},
		asstCall("c1"), toolResult("c1", "r1"),
		asstCall("c2"), toolResult("c2", "r2"),
		asstCall("c3"), toolResult("c3", "r3"),
	}
	m := truncateHistoryMechanism{keepLastTurns: 2, gapNote: truncateGapNote}
	conv := domain.NewConversation(msgs)

	// First run truncates: it drops the oldest exchange and inserts the gap note (a real fire).
	if err := m.RewriteHistory(context.Background(), conv); err != nil {
		t.Fatalf("first RewriteHistory: %v", err)
	}
	if conv.Revision() == 0 {
		t.Fatalf("first run booked no mutation, expected a truncation")
	}
	afterFirst := conv.Messages()
	revAfterFirst := conv.Revision()

	// Second run on the ungrown, already-truncated history: no new assistant boundary, so the only
	// candidate drop is the gap note itself — it must be a byte-for-byte, Revision-stable no-op.
	if err := m.RewriteHistory(context.Background(), conv); err != nil {
		t.Fatalf("re-run RewriteHistory: %v", err)
	}
	if got := conv.Messages(); !sameMessages(got, afterFirst) {
		t.Errorf("re-run mutated the history\n got: %+v\nwant: %+v", got, afterFirst)
	}
	if conv.Revision() != revAfterFirst {
		t.Errorf("re-run bumped Revision %d -> %d (would book a phantom acted-fire)", revAfterFirst, conv.Revision())
	}

	// Grow the history past the window with a new assistant-anchored exchange, then re-run: the
	// grown-history path is untouched, so it truncates and books normally again.
	conv.Append(asstCall("c4"))
	conv.Append(toolResult("c4", "r4"))
	revBeforeGrowth := conv.Revision()
	if err := m.RewriteHistory(context.Background(), conv); err != nil {
		t.Fatalf("post-growth RewriteHistory: %v", err)
	}
	if conv.Revision() == revBeforeGrowth {
		t.Errorf("post-growth re-run did not truncate the grown history (Revision unchanged at %d)", revBeforeGrowth)
	}
	if n := countGapNote(conv.Messages()); n != 1 {
		t.Errorf("post-growth re-run left %d gap notes, want exactly 1", n)
	}
}

// A non-positive keep window is a no-op (the sim's KeepLastTurns <= 0 guard).
func TestTruncateHistoryNonPositiveKeepIsNoOp(t *testing.T) {
	t.Parallel()
	msgs := []domain.Message{
		{Role: domain.RoleSystem, Content: "s"},
		{Role: domain.RoleUser, Content: "u1"},
		asstCall("c1"), toolResult("c1", "r1"),
		asstCall("c2"), toolResult("c2", "r2"),
	}
	m := truncateHistoryMechanism{keepLastTurns: 0, gapNote: truncateGapNote}
	conv := domain.NewConversation(msgs)
	if err := m.RewriteHistory(context.Background(), conv); err != nil {
		t.Fatalf("RewriteHistory: %v", err)
	}
	if got := conv.Messages(); !sameMessages(got, msgs) {
		t.Errorf("keep<=0 should be a no-op, got %+v", got)
	}
}

// The catalogued Mechanism is buildable, keeps the documented default window, and carries the
// descriptor the catalogue ratified (proactive-nudge, strikes-3, no ordering constraints).
func TestTruncateHistoryDescriptorAndDefaultWindow(t *testing.T) {
	t.Parallel()
	built, err := Build(truncateHistoryID, Deps{})
	if err != nil {
		t.Fatalf("Build(%q): %v", truncateHistoryID, err)
	}

	desc := built.Descriptor()
	if desc.ID != truncateHistoryID {
		t.Errorf("ID = %q, want %q", desc.ID, truncateHistoryID)
	}
	if desc.Capability != domain.CapProactiveNudge {
		t.Errorf("Capability = %q, want %q (a context-shaper is disabled under Bypass, D5)", desc.Capability, domain.CapProactiveNudge)
	}
	if desc.Suppression != domain.SuppressStrikesThree {
		t.Errorf("Suppression = %q, want %q", desc.Suppression, domain.SuppressStrikesThree)
	}
	if ord := built.Ordering(); len(ord.Before) != 0 || len(ord.After) != 0 {
		t.Errorf("Ordering = %+v, want no constraints", ord)
	}

	rewriter, ok := built.(domain.HistoryRewriter)
	if !ok {
		t.Fatalf("built mechanism %T does not implement HistoryRewriter", built)
	}
	// The default keeps the last defaultKeepLastTurns exchanges: build a history with one more
	// exchange than the window and confirm exactly the oldest exchange is dropped.
	msgs := []domain.Message{{Role: domain.RoleSystem, Content: "s"}, {Role: domain.RoleUser, Content: "u1"}}
	for i := 0; i <= defaultKeepLastTurns; i++ {
		id := string(rune('a'+i)) + "call"
		msgs = append(msgs, asstCall(id), toolResult(id, "r"))
	}
	conv := domain.NewConversation(msgs)
	if err := rewriter.RewriteHistory(context.Background(), conv); err != nil {
		t.Fatalf("RewriteHistory: %v", err)
	}
	got := conv.Messages()
	want := refTruncate(msgs, defaultKeepLastTurns, truncateGapNote)
	if !sameMessages(got, want) {
		t.Fatalf("default window output diverged\n got: %+v\nwant: %+v", got, want)
	}
	if countGapNote(got) != 1 {
		t.Errorf("default window did not insert exactly one gap note: %+v", got)
	}
	if !strings.HasPrefix(got[2].Content, "[Earlier conversation history") {
		t.Errorf("gap note = %q, want the static context-window note", got[2].Content)
	}
}
