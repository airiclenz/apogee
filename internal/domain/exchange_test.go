package domain

// White-box tests for the ExchangeView derivation (ADR 0017 §1, deepening plan
// item 3). Package-internal so they can build the unexported conversationView
// for the property pin: UserIndex must equal what conversationView.LastUser
// reports on every fixture — the two derivations may not drift. Each fixture
// runs against both read surfaces (conversationView and *Conversation), so the
// hooks' request view and the engine's committed history are proven to share
// one boundary definition.

import (
	"reflect"
	"testing"
)

func exUser(content string) Message { return Message{Role: RoleUser, Content: content} }

func exAssistant(content string) Message { return Message{Role: RoleAssistant, Content: content} }

func exAssistantCalls(callID string) Message {
	return Message{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: callID, Tool: "read_file"}}}
}

func exToolResult(callID, content string) Message {
	return Message{Role: RoleTool, ToolCallID: callID, Content: content}
}

func TestCurrentExchange(t *testing.T) {
	t.Parallel()

	// The fixes plan's shared F-context mid-Exchange shape:
	// user ask, assistant(calls), tool result, assistant.
	midExchange := []Message{
		exUser("the ask"),
		exAssistantCalls("c1"),
		exToolResult("c1", "contents"),
		exAssistant("working on it"),
	}

	tests := []struct {
		name          string
		msgs          []Message
		wantFound     bool
		wantUserIndex int
		wantAfter     []Message
	}{
		{
			name:          "empty conversation",
			msgs:          nil,
			wantFound:     false,
			wantUserIndex: -1,
			wantAfter:     nil,
		},
		{
			name:          "no user message",
			msgs:          []Message{{Role: RoleSystem, Content: "sys"}, exAssistant("hi")},
			wantFound:     false,
			wantUserIndex: -1,
			wantAfter:     nil,
		},
		{
			name:          "single user message",
			msgs:          []Message{exUser("the ask")},
			wantFound:     true,
			wantUserIndex: 0,
			wantAfter:     nil,
		},
		{
			name:          "mid-Exchange shape",
			msgs:          midExchange,
			wantFound:     true,
			wantUserIndex: 0,
			wantAfter:     midExchange[1:],
		},
		{
			name: "multiple Exchanges anchor on the last user message",
			msgs: []Message{
				exUser("first ask"),
				exAssistant("first answer"),
				exUser("second ask"),
				exAssistantCalls("c2"),
				exToolResult("c2", "out"),
			},
			wantFound:     true,
			wantUserIndex: 2,
			wantAfter: []Message{
				exAssistantCalls("c2"),
				exToolResult("c2", "out"),
			},
		},
		{
			name: "injected user message before the last user does not move the boundary",
			msgs: []Message{
				{Role: RoleSystem, Content: "sys"},
				exUser("injected context"), // the InjectContext shape: inserted before the ask
				exUser("the ask"),
				exAssistant("answer draft"),
			},
			wantFound:     true,
			wantUserIndex: 2,
			wantAfter:     []Message{exAssistant("answer draft")},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			readers := map[string]messageReader{
				"conversationView": conversationView{messages: tc.msgs},
				"Conversation":     NewConversation(tc.msgs),
			}
			for readerName, reader := range readers {
				e := CurrentExchange(reader)

				if got := e.Found(); got != tc.wantFound {
					t.Errorf("%s: Found() = %v, want %v", readerName, got, tc.wantFound)
				}
				if got := e.UserIndex(); got != tc.wantUserIndex {
					t.Errorf("%s: UserIndex() = %d, want %d", readerName, got, tc.wantUserIndex)
				}
				if got := e.After(); !reflect.DeepEqual(got, tc.wantAfter) {
					t.Errorf("%s: After() = %v, want %v", readerName, got, tc.wantAfter)
				}

				// RangeAfter must visit exactly After()'s messages at their view indices.
				var gotIdx []int
				var gotMsgs []Message
				e.RangeAfter(func(i int, m Message) bool {
					gotIdx = append(gotIdx, i)
					gotMsgs = append(gotMsgs, m)
					return true
				})
				if !reflect.DeepEqual(gotMsgs, tc.wantAfter) {
					t.Errorf("%s: RangeAfter visited %v, want %v", readerName, gotMsgs, tc.wantAfter)
				}
				for k, idx := range gotIdx {
					if want := tc.wantUserIndex + 1 + k; idx != want {
						t.Errorf("%s: RangeAfter index[%d] = %d, want %d", readerName, k, idx, want)
					}
				}

				// Property pin: UserIndex equals what conversationView.LastUser reports.
				wantIdx := -1
				if _, i, ok := (conversationView{messages: tc.msgs}).LastUser(); ok {
					wantIdx = i
				}
				if got := e.UserIndex(); got != wantIdx {
					t.Errorf("%s: UserIndex() = %d, LastUser reports %d — the derivations drifted", readerName, got, wantIdx)
				}
			}
		})
	}
}

func TestExchangeViewRangeAfterStops(t *testing.T) {
	t.Parallel()

	e := CurrentExchange(conversationView{messages: []Message{
		exUser("the ask"),
		exAssistantCalls("c1"),
		exToolResult("c1", "contents"),
		exAssistant("done"),
	}})

	var visited int
	e.RangeAfter(func(i int, m Message) bool {
		visited++
		return false
	})

	if visited != 1 {
		t.Errorf("RangeAfter visited %d messages after fn returned false, want 1", visited)
	}
}
