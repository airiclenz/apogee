package eventjson

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// TestEncodeJSONGolden pins the `data` object of every serialized variant. These member names are
// the documented contract of ADR 0075's v:1 lines — a consumer reads them by name — so this test
// is what makes a rename a deliberate, CHANGELOG-worthy break rather than a silent one. It also
// pins the EventBase the envelope is stamped from, which is the whole point of the audit case.
func TestEncodeJSONGolden(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		event    domain.Event
		wantKind string
		wantBase domain.EventBase
		wantData string
	}{
		{
			name:     "token",
			event:    domain.TokenEvent{EventBase: domain.EventBase{Turn: 3}, Text: "hel"},
			wantKind: "token",
			wantBase: domain.EventBase{Turn: 3},
			wantData: `{"text":"hel"}`,
		},
		{
			name:     "reasoning",
			event:    domain.ReasoningEvent{EventBase: domain.EventBase{Turn: 3}, Text: "weighing"},
			wantKind: "reasoning",
			wantBase: domain.EventBase{Turn: 3},
			wantData: `{"text":"weighing"}`,
		},
		{
			name:     "stream_reset",
			event:    domain.StreamResetEvent{EventBase: domain.EventBase{Turn: 4}},
			wantKind: "stream_reset",
			wantBase: domain.EventBase{Turn: 4},
			wantData: `{}`,
		},
		{
			name:     "message",
			event:    domain.MessageEvent{EventBase: domain.EventBase{Turn: 5}, Text: "done"},
			wantKind: "message",
			wantBase: domain.EventBase{Turn: 5},
			wantData: `{"text":"done"}`,
		},
		{
			name: "tool_call with raw arguments",
			event: domain.ToolCallEvent{
				EventBase: domain.EventBase{Turn: 2},
				Call: domain.ToolCall{
					ID:        "call-1",
					Tool:      "write_file",
					Arguments: json.RawMessage(`{"path":"docs/notes.md"}`),
				},
				ResolvedPath: "/elsewhere/notes.md",
			},
			wantKind: "tool_call",
			wantBase: domain.EventBase{Turn: 2},
			wantData: `{"call":{"id":"call-1","tool":"write_file",` +
				`"arguments":{"path":"docs/notes.md"}},"resolved_path":"/elsewhere/notes.md"}`,
		},
		{
			name: "tool_call with empty arguments",
			event: domain.ToolCallEvent{
				EventBase: domain.EventBase{Turn: 2},
				Call:      domain.ToolCall{ID: "call-2", Tool: "git_status"},
			},
			wantKind: "tool_call",
			wantBase: domain.EventBase{Turn: 2},
			wantData: `{"call":{"id":"call-2","tool":"git_status","arguments":null},"resolved_path":""}`,
		},
		{
			name: "tool_result drops the summary",
			event: domain.ToolResultEvent{
				EventBase: domain.EventBase{Turn: 2},
				Result: domain.ToolResult{
					CallID:  "call-3",
					Content: "4 matches",
					IsError: false,
					Summary: domain.MatchedLines{Total: 4},
				},
			},
			wantKind: "tool_result",
			wantBase: domain.EventBase{Turn: 2},
			wantData: `{"result":{"call_id":"call-3","content":"4 matches","is_error":false}}`,
		},
		{
			name: "sub_agent_phase started",
			event: domain.SubAgentPhaseEvent{
				EventBase: domain.EventBase{Depth: 1, Turn: 1, CallID: "call-9"},
				Phase:     domain.SubAgentStarted,
			},
			wantKind: "sub_agent_phase",
			wantBase: domain.EventBase{Depth: 1, Turn: 1, CallID: "call-9"},
			wantData: `{"phase":"started","result":{"call_id":"","content":"","is_error":false},` +
				`"cancelled":false}`,
		},
		{
			name: "sub_agent_phase cancelled at depth 1",
			event: domain.SubAgentPhaseEvent{
				EventBase: domain.EventBase{Depth: 1, Turn: 6, CallID: "call-9"},
				Phase:     domain.SubAgentFinished,
				Cancelled: true,
			},
			wantKind: "sub_agent_phase",
			wantBase: domain.EventBase{Depth: 1, Turn: 6, CallID: "call-9"},
			wantData: `{"phase":"finished","result":{"call_id":"","content":"","is_error":false},` +
				`"cancelled":true}`,
		},
		{
			name: "sub_agent_named",
			event: domain.SubAgentNamedEvent{
				EventBase: domain.EventBase{Depth: 1, Turn: 1, CallID: "call-9"},
				Name:      "docs sweep",
			},
			wantKind: "sub_agent_named",
			wantBase: domain.EventBase{Depth: 1, Turn: 1, CallID: "call-9"},
			wantData: `{"name":"docs sweep"}`,
		},
		{
			name: "child_interjection",
			event: domain.ChildInterjectionEvent{
				EventBase: domain.EventBase{Depth: 1, Turn: 3, CallID: "call-9"},
				Input: domain.UserInput{
					Text:     "check the manual too",
					FileRefs: []string{"docs/manual/headless.md"},
					SkillIDs: []string{"coding-standards"},
				},
				Landed: true,
			},
			wantKind: "child_interjection",
			wantBase: domain.EventBase{Depth: 1, Turn: 3, CallID: "call-9"},
			wantData: `{"input":{"text":"check the manual too",` +
				`"file_refs":["docs/manual/headless.md"],"skill_ids":["coding-standards"]},` +
				`"landed":true}`,
		},
		{
			name: "child_interjection with no refs",
			event: domain.ChildInterjectionEvent{
				EventBase: domain.EventBase{Depth: 1, Turn: 3, CallID: "call-9"},
				Input:     domain.UserInput{Text: "stop"},
			},
			wantKind: "child_interjection",
			wantBase: domain.EventBase{Depth: 1, Turn: 3, CallID: "call-9"},
			wantData: `{"input":{"text":"stop","file_refs":null,"skill_ids":null},"landed":false}`,
		},
		{
			name: "approval decided",
			event: domain.ApprovalEvent{
				EventBase: domain.EventBase{Turn: 4},
				Phase:     domain.ApprovalDecided,
				Request: domain.ApprovalRequest{
					Tool:           "terminal",
					Arguments:      json.RawMessage(`{"command":"rm -rf build"}`),
					Reason:         "write",
					Remedy:         "run `apogee doctor`",
					SubAgentTask:   "sweep the docs",
					SubAgentName:   "docs sweep",
					CacheKey:       "terminal:rm",
					MCPServerGrant: true,
					MCPServerAlias: "files",
					ResolvedPath:   "/work/repo/build",
					Scope:          "reads the package directory",
				},
				Decision: domain.ApprovalAllowForSession,
			},
			wantKind: "approval",
			wantBase: domain.EventBase{Turn: 4},
			wantData: `{"phase":"decided","request":{"tool":"terminal",` +
				`"arguments":{"command":"rm -rf build"},"reason":"write",` +
				"\"remedy\":\"run `apogee doctor`\"," +
				`"sub_agent_task":"sweep the docs","sub_agent_name":"docs sweep",` +
				`"cache_key":"terminal:rm","mcp_server_grant":true,"mcp_server_alias":"files",` +
				`"resolved_path":"/work/repo/build","scope":"reads the package directory"},` +
				`"decision":"allow-for-session"}`,
		},
		{
			name: "approval requested carries no verdict",
			event: domain.ApprovalEvent{
				EventBase: domain.EventBase{Turn: 4},
				Phase:     domain.ApprovalRequested,
				Request:   domain.ApprovalRequest{Tool: "terminal", Reason: "write"},
			},
			wantKind: "approval",
			wantBase: domain.EventBase{Turn: 4},
			wantData: `{"phase":"requested","request":{"tool":"terminal","arguments":null,` +
				`"reason":"write","remedy":"","sub_agent_task":"","sub_agent_name":"",` +
				`"cache_key":"","mcp_server_grant":false,"mcp_server_alias":"",` +
				`"resolved_path":"","scope":""},"decision":""}`,
		},
		{
			name: "turn",
			event: domain.TurnEvent{
				EventBase:  domain.EventBase{Depth: 1, Turn: 7, CallID: "call-9"},
				Status:     domain.StatusExchangeComplete,
				Faulted:    true,
				StepCapped: true,
			},
			wantKind: "turn",
			wantBase: domain.EventBase{Depth: 1, Turn: 7, CallID: "call-9"},
			wantData: `{"status":"exchange-complete","faulted":true,"step_capped":true}`,
		},
		{
			name: "reaction_fired",
			event: domain.ReactionFiredEvent{
				EventBase: domain.EventBase{Turn: 2},
				Reaction:  "tool-call-repair",
				Origin:    domain.OriginEngine,
				Moment:    domain.MomentPostResponse,
				Action:    "retry",
				Detail:    "unparsable arguments",
			},
			wantKind: "reaction_fired",
			wantBase: domain.EventBase{Turn: 2},
			wantData: `{"reaction":"tool-call-repair","origin":"engine","moment":"post-response",` +
				`"action":"retry","detail":"unparsable arguments"}`,
		},
		{
			name: "error",
			event: domain.ErrorEvent{
				EventBase: domain.EventBase{Turn: 2},
				Source:    "terminal",
				Err:       "exit status 1",
			},
			wantKind: "error",
			wantBase: domain.EventBase{Turn: 2},
			wantData: `{"source":"terminal","err":"exit status 1"}`,
		},
		{
			name: "prune",
			event: domain.PruneEvent{
				EventBase: domain.EventBase{Turn: 8},
				Results:   3,
				Tokens:    1200,
			},
			wantKind: "prune",
			wantBase: domain.EventBase{Turn: 8},
			wantData: `{"results":3,"tokens":1200}`,
		},
		{
			name: "usage",
			event: domain.UsageEvent{
				EventBase:                    domain.EventBase{Turn: 2},
				PromptTokens:                 900,
				CompletionTokens:             120,
				TotalTokens:                  1020,
				CachedPromptTokens:           640,
				Model:                        "gpt-oss-20b",
				ContextWindow:                32768,
				CumulativePromptTokens:       1800,
				CumulativeCompletionTokens:   240,
				CumulativeTotalTokens:        2040,
				CumulativeCachedPromptTokens: 640,
				CumulativeCalls:              2,
				Maintenance:                  true,
			},
			wantKind: "usage",
			wantBase: domain.EventBase{Turn: 2},
			wantData: `{"prompt_tokens":900,"completion_tokens":120,"total_tokens":1020,` +
				`"cached_prompt_tokens":640,"model":"gpt-oss-20b","context_window":32768,` +
				`"cumulative_prompt_tokens":1800,"cumulative_completion_tokens":240,` +
				`"cumulative_total_tokens":2040,"cumulative_cached_prompt_tokens":640,` +
				`"cumulative_calls":2,"maintenance":true}`,
		},
		{
			// The envelope takes the SPAWNING delegation's id off the embedded EventBase while
			// data.call_id carries the AUDITED call — two different facts the variant's shadowing
			// field would otherwise braid into one.
			name: "audit with distinct spawning and audited ids",
			event: domain.AuditEvent{
				EventBase: domain.EventBase{Depth: 1, Turn: 3, CallID: "spawning-call"},
				Tool:      "terminal",
				CallID:    "audited-call",
				Decision:  "dangerous-refused",
				Reason:    "recursive delete",
				IsError:   true,
			},
			wantKind: "audit",
			wantBase: domain.EventBase{Depth: 1, Turn: 3, CallID: "spawning-call"},
			wantData: `{"tool":"terminal","call_id":"audited-call",` +
				`"decision":"dangerous-refused","reason":"recursive delete","is_error":true}`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			kind, base, data, ok := Encode(c.event)

			if !ok {
				t.Fatalf("Encode(%T) ok = false, want true", c.event)
			}
			if kind != c.wantKind {
				t.Errorf("kind = %q, want %q", kind, c.wantKind)
			}
			if base != c.wantBase {
				t.Errorf("base = %+v, want %+v", base, c.wantBase)
			}
			encoded, err := json.Marshal(data)
			if err != nil {
				t.Fatalf("json.Marshal(data): %v", err)
			}
			if string(encoded) != c.wantData {
				t.Errorf("data JSON =\n  %s\nwant\n  %s", encoded, c.wantData)
			}
		})
	}
}

// TestEncodeSkipsTheWireEvent pins ADR 0075 decision 2: the Inspector's raw provider protocol is
// the one variant the lines never carry, and a caller learns that from ok alone.
func TestEncodeSkipsTheWireEvent(t *testing.T) {
	t.Parallel()

	kind, base, data, ok := Encode(domain.WireEvent{
		EventBase: domain.EventBase{Turn: 1},
		Direction: domain.WireDirectionRequest,
		Payload:   `{"model":"gpt-oss-20b"}`,
	})

	if ok {
		t.Fatalf("Encode(WireEvent) ok = true, want false")
	}
	if kind != "" || data != nil || base != (domain.EventBase{}) {
		t.Errorf("Encode(WireEvent) = (%q, %+v, %v, false), want zero values", kind, base, data)
	}
}

// TestEncodeSkipsAnUnknownEvent covers the sealed type's default arm: a variant this package has
// not been taught is skipped rather than written as a half-line.
func TestEncodeSkipsAnUnknownEvent(t *testing.T) {
	t.Parallel()

	if _, _, _, ok := Encode(nil); ok {
		t.Errorf("Encode(nil) ok = true, want false")
	}
}

// TestKindsAreEighteen pins the vocabulary itself — the sixteen serialized variants plus the two
// frames — so a kind added to the encoder without a manual entry, or an entry without a kind, is a
// failing test rather than a documentation drift.
func TestKindsAreEighteen(t *testing.T) {
	t.Parallel()

	kinds := Kinds()

	if len(kinds) != 18 {
		t.Fatalf("len(Kinds()) = %d, want 18: %v", len(kinds), kinds)
	}
	seen := make(map[string]bool, len(kinds))
	for _, kind := range kinds {
		if kind == "" {
			t.Errorf("Kinds() holds an empty name: %v", kinds)
		}
		if seen[kind] {
			t.Errorf("Kinds() repeats %q", kind)
		}
		seen[kind] = true
	}
	for _, frame := range []string{"run_started", "run_finished"} {
		if !seen[frame] {
			t.Errorf("Kinds() is missing the %q frame: %v", frame, kinds)
		}
	}
	if strings.Contains(strings.Join(kinds, " "), "-") {
		t.Errorf("Kinds() must be snake_case, not a Hook event's kebab-case: %v", kinds)
	}
}

// TestKindsIsNotAliased pins that a caller can keep or sort the slice without reaching the next
// caller's copy — the vocabulary is returned, never lent.
func TestKindsIsNotAliased(t *testing.T) {
	t.Parallel()

	first := Kinds()
	first[0] = "mutated"

	if second := Kinds(); second[0] == "mutated" {
		t.Errorf("Kinds() returned an aliased slice; mutating one call changed the next")
	}
}
