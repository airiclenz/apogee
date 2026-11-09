package domain

// White-box tests for Request.AdviseTail, the request-tail advise slot: where the fence lands for
// each tail role, how it orders against an engine note, and that it never leaves the request copy.

import (
	"reflect"
	"strings"
	"testing"
)

// tailAdviceSpan is the provenance a pre-request advise names its fence with.
var tailAdviceSpan = AdviceSpan{Reaction: "house-rules", Origin: OriginUser, Moment: MomentPreRequest, Turn: 3}

// adviseTailHistory returns a fresh conversation ending in a message of tail's role. The tool
// result carries a committed post-tool-result trailer whose ledger has spare capacity, so a
// placement that appended to the shared ledger instead of copying it would reach back into the
// caller's message. Each call builds new values, so a test can compare one copy against another.
func adviseTailHistory(tail Role) []Message {
	result := Message{Role: RoleTool, Content: "package main", ToolCallID: "c1"}
	result = result.WithAdvice(AdviceSpan{Reaction: "lint", Origin: OriginUser, Moment: MomentPostToolResult, Turn: 2}, "vet it")
	result.Advice = append(make([]AdviceSpan, 0, 4), result.Advice...)

	msgs := []Message{
		{Role: RoleUser, Content: "trawl the repo"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "c1", Tool: "read_file"}}},
		result,
	}
	switch tail {
	case RoleUser:
		msgs = append(msgs, Message{Role: RoleUser, Content: "now fix it"})
	case RoleAssistant:
		msgs = append(msgs, Message{Role: RoleAssistant, Content: "half a reply"})
	}
	return msgs
}

// assertLedgerStands fails t unless every row of m's ledger still opens its fence at its offset.
func assertLedgerStands(t *testing.T, m Message) {
	t.Helper()
	for _, row := range m.Advice {
		if !m.fenceStands(row) {
			t.Errorf("ledger row %+v does not stand in %q", row, m.Content)
		}
	}
}

func TestAdviseTailPlacesTheFenceByTailRole(t *testing.T) {
	t.Parallel()

	t.Run("tool tail takes a trailing fence inside the tool message", func(t *testing.T) {
		t.Parallel()
		history := adviseTailHistory(RoleTool)
		req := NewRequest("m", history, nil, Budget{}, 3)
		before := req.Revision()

		if !req.AdviseTail(tailAdviceSpan, "keep diffs small") {
			t.Fatal("AdviseTail = false on a tool-result tail, want true")
		}
		if req.Revision() != before+1 {
			t.Errorf("revision = %d, want %d — a landed fence is a mutation", req.Revision(), before+1)
		}
		msgs := req.State().Messages
		if len(msgs) != len(history) {
			t.Fatalf("request holds %d messages, want %d — a tool tail takes no new message", len(msgs), len(history))
		}
		tail := msgs[len(msgs)-1]
		want := history[2].Content + RenderAdvice(tailAdviceSpan, "keep diffs small")
		if tail.Role != RoleTool || tail.Content != want {
			t.Errorf("tail = %q (%s), want the tool result with the fence trailing %q", tail.Content, tail.Role, want)
		}
		if tail.ContentWithoutAdvice() != "package main" {
			t.Errorf("ContentWithoutAdvice = %q, want the bare tool output", tail.ContentWithoutAdvice())
		}
		if tail.ContentBeforeRequestAdvice() != history[2].Content {
			t.Errorf("ContentBeforeRequestAdvice = %q, want the committed result %q", tail.ContentBeforeRequestAdvice(), history[2].Content)
		}
		assertLedgerStands(t, tail)
	})

	t.Run("user tail takes a leading fence inside the user message", func(t *testing.T) {
		t.Parallel()
		history := adviseTailHistory(RoleUser)
		req := NewRequest("m", history, nil, Budget{}, 3)

		if !req.AdviseTail(tailAdviceSpan, "keep diffs small") {
			t.Fatal("AdviseTail = false on a user tail, want true")
		}
		msgs := req.State().Messages
		if len(msgs) != len(history) {
			t.Fatalf("request holds %d messages, want %d — a user tail takes no new message", len(msgs), len(history))
		}
		tail := msgs[len(msgs)-1]
		lead := tailAdviceSpan
		lead.Leading = true
		if want := RenderAdvice(lead, "keep diffs small") + "now fix it"; tail.Role != RoleUser || tail.Content != want {
			t.Errorf("tail = %q (%s), want the fence ahead of the user's text %q", tail.Content, tail.Role, want)
		}
		if tail.ContentWithoutAdvice() != "now fix it" {
			t.Errorf("ContentWithoutAdvice = %q, want the user's own text", tail.ContentWithoutAdvice())
		}
		assertLedgerStands(t, tail)
	})

	t.Run("assistant tail gets one advice-only user message hidden from View", func(t *testing.T) {
		t.Parallel()
		history := adviseTailHistory(RoleAssistant)
		req := NewRequest("m", history, nil, Budget{}, 3)

		if !req.AdviseTail(tailAdviceSpan, "keep diffs small") {
			t.Fatal("AdviseTail = false on an assistant tail, want true")
		}
		msgs := req.State().Messages
		if len(msgs) != len(history)+1 {
			t.Fatalf("request holds %d messages, want %d — one advice-only message appended", len(msgs), len(history)+1)
		}
		added := msgs[len(msgs)-1]
		if added.Role != RoleUser || added.ContentWithoutAdvice() != "" {
			t.Errorf("appended message = %q (%s), want a user message holding only the fence", added.Content, added.Role)
		}
		if !strings.HasPrefix(added.Content, AdviceFencePrefix+"house-rules (") {
			t.Errorf("appended message = %q, want it to open on the advice fence", added.Content)
		}
		assertLedgerStands(t, added)

		conv := req.View().Conversation()
		if conv.Len() != len(history) {
			t.Errorf("View holds %d messages, want %d — the advice-only message is request-scoped", conv.Len(), len(history))
		}
		if _, idx, ok := conv.LastUser(); !ok || idx == len(msgs)-1 {
			t.Errorf("View().Conversation().LastUser() index = %d (ok %v), want the human's ask, not the advice-only message", idx, ok)
		}
	})

	refused := map[string][]Message{
		"empty":       nil,
		"system tail": {{Role: RoleSystem, Content: "you are apogee"}},
	}
	for name, msgs := range refused {
		t.Run(name+" is refused", func(t *testing.T) {
			t.Parallel()
			req := NewRequest("m", msgs, nil, Budget{}, 3)
			before := req.Revision()

			if req.AdviseTail(tailAdviceSpan, "keep diffs small") {
				t.Fatal("AdviseTail = true, want false")
			}
			if req.Revision() != before {
				t.Errorf("revision moved on a refused fence: %d → %d", before, req.Revision())
			}
			if got := req.State().Messages; !reflect.DeepEqual(got, append([]Message(nil), msgs...)) {
				t.Errorf("a refused fence changed the request: %+v", got)
			}
		})
	}
}

// A tool tail that already carries the wrap-up engine note keeps the note last: the advice fence
// goes in ahead of it, the note's row moves with its bytes, and the bare tool output is still what
// the strip leaves.
func TestAdviseTailSitsAheadOfAnEngineNote(t *testing.T) {
	t.Parallel()

	history := adviseTailHistory(RoleTool)
	req := NewRequest("m", history, nil, Budget{}, 3)
	if !req.NoteOnTail("wrap-up", "report back now") {
		t.Fatal("NoteOnTail = false on a tool-result tail")
	}
	if !req.AdviseTail(tailAdviceSpan, "keep diffs small") {
		t.Fatal("AdviseTail = false on a noted tool-result tail, want true")
	}

	msgs := req.State().Messages
	tail := msgs[len(msgs)-1]
	want := history[2].Content + RenderAdvice(tailAdviceSpan, "keep diffs small") +
		RenderEngineNote("wrap-up", "report back now")
	if tail.Content != want {
		t.Errorf("tail = %q, want committed result, advice fence, then the note last %q", tail.Content, want)
	}
	if tail.ContentWithoutAdvice() != "package main" {
		t.Errorf("ContentWithoutAdvice = %q, want the bare tool output", tail.ContentWithoutAdvice())
	}
	if tail.ContentBeforeRequestAdvice() != history[2].Content {
		t.Errorf("ContentBeforeRequestAdvice = %q, want the committed result %q", tail.ContentBeforeRequestAdvice(), history[2].Content)
	}
	assertLedgerStands(t, tail)
	if n := len(tail.Advice); n != 3 || tail.Advice[1].Moment != MomentPreRequest || tail.Advice[2].Topic != "wrap-up" {
		t.Errorf("ledger = %+v, want trailer, advice row, then the note row — offset order", tail.Advice)
	}
	for i := 1; i < len(tail.Advice); i++ {
		if tail.Advice[i].Offset <= tail.Advice[i-1].Offset {
			t.Errorf("ledger rows out of offset order: %+v", tail.Advice)
		}
	}
}

// The fence lives on the request copy alone: the conversation the request was built from is
// unchanged, and a second request built from it carries no fence.
func TestAdviseTailNeverReachesTheConversation(t *testing.T) {
	t.Parallel()

	for _, role := range []Role{RoleTool, RoleUser, RoleAssistant} {
		t.Run(string(role)+" tail", func(t *testing.T) {
			t.Parallel()
			history := adviseTailHistory(role)
			pristine := adviseTailHistory(role)

			first := NewRequest("m", history, nil, Budget{}, 3)
			first.NoteOnTail("wrap-up", "report back now")
			if !first.AdviseTail(tailAdviceSpan, "keep diffs small") {
				t.Fatalf("AdviseTail = false on a %s tail", role)
			}

			if !reflect.DeepEqual(history, pristine) {
				t.Errorf("AdviseTail reached back into the source conversation:\n got %+v\nwant %+v", history, pristine)
			}
			for _, m := range NewRequest("m", history, nil, Budget{}, 4).State().Messages {
				if strings.Contains(m.Content, AdviceFencePrefix+"house-rules") {
					t.Errorf("a second request built from the conversation carries the fence: %q", m.Content)
				}
			}
		})
	}
}
