package eventjson

import (
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

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
				`"arguments":{"path":"docs/notes.md"},"malformed":null},"resolved_path":"/elsewhere/notes.md","spawn_run_id":""}`,
		},
		{
			name: "tool_call with empty arguments",
			event: domain.ToolCallEvent{
				EventBase: domain.EventBase{Turn: 2},
				Call:      domain.ToolCall{ID: "call-2", Tool: "git_status"},
			},
			wantKind: "tool_call",
			wantBase: domain.EventBase{Turn: 2},
			wantData: `{"call":{"id":"call-2","tool":"git_status","arguments":null,"malformed":null},"resolved_path":"","spawn_run_id":""}`,
		},
		{
			name: "tool_call of a delegation names the run it spawns",
			event: domain.ToolCallEvent{
				EventBase:  domain.EventBase{Turn: 2, Depth: 1, CallID: "call-0", RunID: "0badc0de.1"},
				Call:       domain.ToolCall{ID: "call-5", Tool: "sub_agent"},
				SpawnRunID: "0badc0de.2",
			},
			wantKind: "tool_call",
			wantBase: domain.EventBase{Turn: 2, Depth: 1, CallID: "call-0", RunID: "0badc0de.1"},
			wantData: `{"call":{"id":"call-5","tool":"sub_agent","arguments":null,"malformed":null},"resolved_path":"",` +
				`"spawn_run_id":"0badc0de.2"}`,
		},
		{
			// A call whose arguments were not a JSON object carries "{}" as its arguments and the
			// parse failure's text beside what the model actually sent.
			name: "tool_call with malformed arguments",
			event: domain.ToolCallEvent{
				EventBase: domain.EventBase{Turn: 2},
				Call: domain.ToolCall{
					ID:        "call-6",
					Tool:      "read_file",
					Arguments: json.RawMessage(`{}`),
					Malformed: &domain.MalformedArguments{
						Err: errors.New("unexpected end of JSON input"),
						Raw: `{"path":"a.txt"`,
					},
				},
			},
			wantKind: "tool_call",
			wantBase: domain.EventBase{Turn: 2},
			wantData: `{"call":{"id":"call-6","tool":"read_file","arguments":{},` +
				`"malformed":{"err":"unexpected end of JSON input","raw":"{\"path\":\"a.txt\""}},` +
				`"resolved_path":"","spawn_run_id":""}`,
		},
		{
			name: "tool_result carries its summary",
			event: domain.ToolResultEvent{
				EventBase: domain.EventBase{Turn: 2},
				Result: domain.ToolResult{
					CallID:  "call-3",
					Content: "4 matches",
					IsError: false,
					Summary: domain.MatchedLines{Total: 4},
				},
				Tool: "grep",
			},
			wantKind: "tool_result",
			wantBase: domain.EventBase{Turn: 2},
			wantData: `{"result":{"call_id":"call-3","content":"4 matches","is_error":false,` +
				`"summary":{"kind":"matched_lines","total":4}},` +
				`"tool":"grep","write_target":"","spawn_run_id":""}`,
		},
		{
			name: "tool_result of a write names its target",
			event: domain.ToolResultEvent{
				EventBase:   domain.EventBase{Turn: 2},
				Result:      domain.ToolResult{CallID: "call-4", Content: "wrote docs/notes.md"},
				Tool:        "write_file",
				WriteTarget: "/work/docs/notes.md",
			},
			wantKind: "tool_result",
			wantBase: domain.EventBase{Turn: 2},
			wantData: `{"result":{"call_id":"call-4","content":"wrote docs/notes.md","is_error":false,"summary":null},` +
				`"tool":"write_file","write_target":"/work/docs/notes.md","spawn_run_id":""}`,
		},
		{
			name: "tool_result of a delegation names the run it answers",
			event: domain.ToolResultEvent{
				EventBase:  domain.EventBase{Turn: 2},
				Result:     domain.ToolResult{CallID: "call-5", Content: "report"},
				Tool:       "sub_agent",
				SpawnRunID: "0badc0de.2",
			},
			wantKind: "tool_result",
			wantBase: domain.EventBase{Turn: 2},
			wantData: `{"result":{"call_id":"call-5","content":"report","is_error":false,"summary":null},` +
				`"tool":"sub_agent","write_target":"","spawn_run_id":"0badc0de.2"}`,
		},
		{
			name: "sub_agent_phase started",
			event: domain.SubAgentPhaseEvent{
				EventBase: domain.EventBase{Depth: 1, Turn: 1, CallID: "call-9"},
				Phase:     domain.SubAgentStarted,
			},
			wantKind: "sub_agent_phase",
			wantBase: domain.EventBase{Depth: 1, Turn: 1, CallID: "call-9"},
			wantData: `{"phase":"started","result":{"call_id":"","content":"","is_error":false,"summary":null},` +
				`"cancelled":false,"step_cap":0,"cap_requested":0}`,
		},
		{
			// The started phase carries the step cap the child runs under, and the larger
			// max_steps its call asked for when the configured cap clamped it.
			name: "sub_agent_phase started under a clamped step cap",
			event: domain.SubAgentPhaseEvent{
				EventBase:    domain.EventBase{Depth: 1, Turn: 1, CallID: "call-9", RunID: "0badc0de.2"},
				Phase:        domain.SubAgentStarted,
				StepCap:      20,
				CapRequested: 50,
			},
			wantKind: "sub_agent_phase",
			wantBase: domain.EventBase{Depth: 1, Turn: 1, CallID: "call-9", RunID: "0badc0de.2"},
			wantData: `{"phase":"started","result":{"call_id":"","content":"","is_error":false,"summary":null},` +
				`"cancelled":false,"step_cap":20,"cap_requested":50}`,
		},
		{
			// A cancel settles a delegation with a result (ADR 0088), so the finished phase carries
			// it; `cancelled` has no source and stays on the v2 line as a constant false.
			name: "sub_agent_phase finished on a cancel's stopped result at depth 1",
			event: domain.SubAgentPhaseEvent{
				EventBase: domain.EventBase{Depth: 1, Turn: 6, CallID: "call-9"},
				Phase:     domain.SubAgentFinished,
				Result: domain.ToolResult{
					CallID:  "call-9",
					Content: "stopped",
					IsError: true,
					Summary: domain.DelegationOutcome{IsStoppedByUser: true, SteeredMessages: 1},
				},
			},
			wantKind: "sub_agent_phase",
			wantBase: domain.EventBase{Depth: 1, Turn: 6, CallID: "call-9"},
			wantData: `{"phase":"finished","result":{"call_id":"call-9","content":"stopped","is_error":true,` +
				`"summary":{"kind":"delegation_outcome","bound":"","stopped_by_user":true,"has_no_report":false,` +
				`"steered_messages":1}},"cancelled":false,"step_cap":0,"cap_requested":0}`,
		},
		{
			name: "workflow_phase started",
			event: domain.WorkflowPhaseEvent{
				EventBase: domain.EventBase{Turn: 2},
				Phase:     domain.WorkflowStarted,
				Workflow:  "20260927-101500-ab12",
				Name:      "check each package",
				Call:      "call-7",
			},
			wantKind: "workflow_phase",
			wantBase: domain.EventBase{Turn: 2},
			wantData: `{"phase":"started","workflow":"20260927-101500-ab12","name":"check each package",` +
				`"stage":"","item":"","index":0,"resumed":false,` +
				`"receipt":{"status":"","summary":"","fields":{}},"detail":"","call":"call-7",` +
				`"item_name":"","background":false,"origin":"","stages":null,"stage_kinds":null,"items":0,` +
				`"round":0,"rounds":0,"outcome":"","run":"","attempt":0,"resume":"","tally":null}`,
		},
		{
			// The started phase of a background Recipe launch carries the Workflow's whole shape:
			// its stages and their kinds, its origin and the resume command a Driver shows.
			name: "workflow_phase started carries the shape",
			event: domain.WorkflowPhaseEvent{
				EventBase:  domain.EventBase{Turn: 2},
				Phase:      domain.WorkflowStarted,
				Workflow:   "20260927-101500-ab12",
				Name:       "audit",
				Call:       "recipe-1",
				Background: true,
				Origin:     "sub_agent",
				Stages:     []string{"items", "check"},
				StageKinds: []string{"fanout", "verify"},
				Resume:     "re-run `/audit` to resume",
			},
			wantKind: "workflow_phase",
			wantBase: domain.EventBase{Turn: 2},
			wantData: `{"phase":"started","workflow":"20260927-101500-ab12","name":"audit",` +
				`"stage":"","item":"","index":0,"resumed":false,` +
				`"receipt":{"status":"","summary":"","fields":{}},"detail":"","call":"recipe-1",` +
				`"item_name":"","background":true,"origin":"sub_agent","stages":["items","check"],` +
				`"stage_kinds":["fanout","verify"],"items":0,"round":0,"rounds":0,"outcome":"","run":"",` +
				`"attempt":0,"resume":"re-run ` + "`/audit`" + ` to resume","tally":null}`,
		},
		{
			name: "workflow_phase stage_started carries its items and rounds",
			event: domain.WorkflowPhaseEvent{
				EventBase: domain.EventBase{Turn: 3},
				Phase:     domain.WorkflowStageStarted,
				Workflow:  "wf-1",
				Stage:     "items",
				Items:     4,
				Round:     2,
				Rounds:    3,
			},
			wantKind: "workflow_phase",
			wantBase: domain.EventBase{Turn: 3},
			wantData: `{"phase":"stage_started","workflow":"wf-1","name":"","stage":"items","item":"","index":0,` +
				`"resumed":false,"receipt":{"status":"","summary":"","fields":{}},"detail":"","call":"",` +
				`"item_name":"","background":false,"origin":"","stages":null,"stage_kinds":null,"items":4,` +
				`"round":2,"rounds":3,"outcome":"","run":"","attempt":0,"resume":"","tally":null}`,
		},
		{
			name: "workflow_phase finished carries the tally",
			event: domain.WorkflowPhaseEvent{
				EventBase: domain.EventBase{Turn: 6},
				Phase:     domain.WorkflowFinished,
				Workflow:  "wf-1",
				Tally:     &domain.WorkflowTally{OK: 3, Partial: 1, Blocked: 1, Unfinished: 0},
			},
			wantKind: "workflow_phase",
			wantBase: domain.EventBase{Turn: 6},
			wantData: `{"phase":"finished","workflow":"wf-1","name":"","stage":"","item":"","index":0,` +
				`"resumed":false,"receipt":{"status":"","summary":"","fields":{}},"detail":"","call":"",` +
				`"item_name":"","background":false,"origin":"","stages":null,"stage_kinds":null,"items":0,` +
				`"round":0,"rounds":0,"outcome":"","run":"","attempt":0,"resume":"",` +
				`"tally":{"ok":3,"partial":1,"blocked":1,"unfinished":0}}`,
		},
		{
			name: "workflow_phase item_finished carries the receipt",
			event: domain.WorkflowPhaseEvent{
				EventBase: domain.EventBase{Depth: 1, Turn: 4, CallID: "call-9", RunID: "0badc0de.3"},
				Phase:     domain.WorkflowItemFinished,
				Workflow:  "20260927-101500-ab12",
				Name:      "check each package",
				Stage:     "items",
				Item:      "internal/tui",
				ItemName:  "tui",
				Round:     1,
				Run:       "0badc0de.4",
				Index:     2,
				Resumed:   true,
				Receipt: domain.WorkflowReceipt{
					Status: "partial", Summary: "two findings", Fields: map[string]string{"findings": "2"},
				},
			},
			wantKind: "workflow_phase",
			wantBase: domain.EventBase{Depth: 1, Turn: 4, CallID: "call-9", RunID: "0badc0de.3"},
			wantData: `{"phase":"item_finished","workflow":"20260927-101500-ab12","name":"check each package",` +
				`"stage":"items","item":"internal/tui","index":2,"resumed":true,` +
				`"receipt":{"status":"partial","summary":"two findings","fields":{"findings":"2"}},"detail":"","call":"",` +
				`"item_name":"tui","background":false,"origin":"","stages":null,"stage_kinds":null,"items":0,` +
				`"round":1,"rounds":0,"outcome":"","run":"0badc0de.4","attempt":0,"resume":"","tally":null}`,
		},
		{
			name: "workflow_phase waiting carries the question",
			event: domain.WorkflowPhaseEvent{
				EventBase: domain.EventBase{Turn: 5},
				Phase:     domain.WorkflowWaiting,
				Workflow:  "20260927-101500-ab12",
				Stage:     "confirm",
				Detail:    "Fix the refuted items?",
			},
			wantKind: "workflow_phase",
			wantBase: domain.EventBase{Turn: 5},
			wantData: `{"phase":"waiting","workflow":"20260927-101500-ab12","name":"",` +
				`"stage":"confirm","item":"","index":0,"resumed":false,` +
				`"receipt":{"status":"","summary":"","fields":{}},"detail":"Fix the refuted items?","call":"",` +
				`"item_name":"","background":false,"origin":"","stages":null,"stage_kinds":null,"items":0,` +
				`"round":0,"rounds":0,"outcome":"","run":"","attempt":0,"resume":"","tally":null}`,
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
					Images:   []domain.Image{{Name: "shot.png", MediaType: "image/png", Data: []byte("\x89PNG-bytes")}},
				},
				Landed: true,
			},
			wantKind: "child_interjection",
			wantBase: domain.EventBase{Depth: 1, Turn: 3, CallID: "call-9"},
			wantData: `{"input":{"text":"check the manual too",` +
				`"file_refs":["docs/manual/headless.md"],"skill_ids":["coding-standards"],` +
				`"images":[{"name":"shot.png","media_type":"image/png","size":10}],"recipe":null,"recipe_inputs":null},` +
				`"landed":true,"reason":""}`,
		},
		{
			// A recipe launch rides the input as a value, its bound inputs beside it.
			name: "child_interjection carrying a recipe launch",
			event: domain.ChildInterjectionEvent{
				EventBase: domain.EventBase{Depth: 1, Turn: 3, CallID: "call-9"},
				Input: domain.UserInput{
					Text:         "/audit internal/tui",
					SkillIDs:     []string{"audit"},
					Recipe:       &domain.RecipeLaunch{SkillID: "audit", Text: "internal/tui", Background: true},
					RecipeInputs: map[string]string{"target": "internal/tui"},
				},
				Landed: true,
			},
			wantKind: "child_interjection",
			wantBase: domain.EventBase{Depth: 1, Turn: 3, CallID: "call-9"},
			wantData: `{"input":{"text":"/audit internal/tui","file_refs":null,"skill_ids":["audit"],` +
				`"images":null,"recipe":{"skill_id":"audit","text":"internal/tui","background":true},` +
				`"recipe_inputs":{"target":"internal/tui"}},"landed":true,"reason":""}`,
		},
		{
			name: "child_interjection with no refs",
			event: domain.ChildInterjectionEvent{
				EventBase: domain.EventBase{Depth: 1, Turn: 3, CallID: "call-9"},
				Input:     domain.UserInput{Text: "stop"},
				Reason:    domain.UndeliveredCapped,
			},
			wantKind: "child_interjection",
			wantBase: domain.EventBase{Depth: 1, Turn: 3, CallID: "call-9"},
			wantData: `{"input":{"text":"stop","file_refs":null,"skill_ids":null,"images":null,` +
				`"recipe":null,"recipe_inputs":null},"landed":false,"reason":"capped"}`,
		},
		{
			name: "child_interjection undelivered to a stopped child",
			event: domain.ChildInterjectionEvent{
				EventBase: domain.EventBase{Depth: 1, Turn: 3, CallID: "call-9"},
				Input:     domain.UserInput{Text: "also check the vendor directory"},
				Reason:    domain.UndeliveredStopped,
			},
			wantKind: "child_interjection",
			wantBase: domain.EventBase{Depth: 1, Turn: 3, CallID: "call-9"},
			wantData: `{"input":{"text":"also check the vendor directory","file_refs":null,"skill_ids":null,` +
				`"images":null,"recipe":null,"recipe_inputs":null},"landed":false,"reason":"stopped"}`,
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
					Remedy:         "run `apogee probe host`",
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
				"\"remedy\":\"run `apogee probe host`\"," +
				`"sub_agent_task":"sweep the docs","sub_agent_name":"docs sweep",` +
				`"cache_key":"terminal:rm","mcp_server_grant":true,"mcp_server_alias":"files",` +
				`"resolved_path":"/work/repo/build","scope":"reads the package directory"},` +
				`"decision":"allow-for-session","rules":null}`,
		},
		{
			name: "approval allowed by rule carries the rules",
			event: domain.ApprovalEvent{
				EventBase: domain.EventBase{Turn: 4},
				Phase:     domain.ApprovalDecided,
				Request:   domain.ApprovalRequest{Tool: "terminal"},
				Decision:  domain.ApprovalAllowedByRule,
				Rules: []domain.AllowRule{
					{Kind: domain.AllowRuleTerminal, Text: "go test", Layer: domain.AllowRuleProject},
				},
			},
			wantKind: "approval",
			wantBase: domain.EventBase{Turn: 4},
			wantData: `{"phase":"decided","request":{"tool":"terminal","arguments":null,` +
				`"reason":"","remedy":"","sub_agent_task":"","sub_agent_name":"",` +
				`"cache_key":"","mcp_server_grant":false,"mcp_server_alias":"",` +
				`"resolved_path":"","scope":""},"decision":"allowed-by-rule",` +
				`"rules":[{"kind":"terminal","text":"go test","layer":"project"}]}`,
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
				`"resolved_path":"","scope":""},"decision":"","rules":null}`,
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
			name: "ref_clipped",
			event: domain.RefClippedEvent{
				EventBase: domain.EventBase{Turn: 3},
				Ref:       "@docs/big.md",
				Tokens:    32000,
				Absolute:  true,
			},
			wantKind: "ref_clipped",
			wantBase: domain.EventBase{Turn: 3},
			wantData: `{"ref":"@docs/big.md","tokens":32000,"absolute":true}`,
		},
		{
			name: "malformed_chunks",
			event: domain.MalformedChunksEvent{
				EventBase: domain.EventBase{Turn: 4},
				Count:     2,
			},
			wantKind: "malformed_chunks",
			wantBase: domain.EventBase{Turn: 4},
			wantData: `{"count":2}`,
		},
		{
			name: "usage",
			event: domain.UsageEvent{
				EventBase:          domain.EventBase{Turn: 2},
				PromptTokens:       900,
				CompletionTokens:   120,
				TotalTokens:        1020,
				CachedPromptTokens: 640,
				Model:              "gpt-oss-20b",
				ServedModel:        "gpt-oss-20b-mxfp4",
				ContextWindow:      32768,
				Cumulative: domain.Usage{
					Calls:              2,
					PromptTokens:       1800,
					CachedPromptTokens: 640,
					CompletionTokens:   240,
					TotalTokens:        2040,
				},
				Maintenance: true,
			},
			wantKind: "usage",
			wantBase: domain.EventBase{Turn: 2},
			wantData: `{"prompt_tokens":900,"completion_tokens":120,"total_tokens":1020,` +
				`"cached_prompt_tokens":640,"model":"gpt-oss-20b","served_model":"gpt-oss-20b-mxfp4",` +
				`"context_window":32768,` +
				`"cumulative_prompt_tokens":1800,"cumulative_completion_tokens":240,` +
				`"cumulative_total_tokens":2040,"cumulative_cached_prompt_tokens":640,` +
				`"cumulative_calls":2,"cost":0,"priced":false,"cumulative_cost":0,` +
				`"cumulative_unpriced_calls":0,"currency":"","maintenance":true,"delegate_fold":false,` +
				`"cumulative_priced_calls":0}`,
		},
		{
			// A capped delegate's fold is maintenance that compacted nothing, and the running sums
			// say how many of the agent's calls were priced.
			name: "usage of a delegate fold",
			event: domain.UsageEvent{
				EventBase:    domain.EventBase{Depth: 1, Turn: 4, CallID: "call-9", RunID: "0badc0de.2"},
				Cumulative:   domain.Usage{Calls: 3, PricedCalls: 2, UnpricedCalls: 1},
				Maintenance:  true,
				DelegateFold: true,
			},
			wantKind: "usage",
			wantBase: domain.EventBase{Depth: 1, Turn: 4, CallID: "call-9", RunID: "0badc0de.2"},
			wantData: `{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0,` +
				`"cached_prompt_tokens":0,"model":"","served_model":"","context_window":0,` +
				`"cumulative_prompt_tokens":0,"cumulative_completion_tokens":0,` +
				`"cumulative_total_tokens":0,"cumulative_cached_prompt_tokens":0,` +
				`"cumulative_calls":3,"cost":0,"priced":false,"cumulative_cost":0,` +
				`"cumulative_unpriced_calls":1,"currency":"","maintenance":true,"delegate_fold":true,` +
				`"cumulative_priced_calls":2}`,
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
		{
			// Durations travel as whole milliseconds; a sub-millisecond remainder is truncated.
			name: "upstream_attempt",
			event: domain.UpstreamAttemptEvent{
				EventBase:    domain.EventBase{Depth: 1, Turn: 2, CallID: "call-7"},
				Server:       "local",
				Endpoint:     "http://127.0.0.1:8080/v1",
				Model:        "gpt-oss-20b",
				RequestID:    "req-3",
				Index:        1,
				TTFB:         120 * time.Millisecond,
				TTFT:         1800*time.Millisecond + 400*time.Microsecond,
				Last:         4200 * time.Millisecond,
				Duration:     4250 * time.Millisecond,
				OutputTokens: 96,
				Outcome:      "ok",
			},
			wantKind: "upstream_attempt",
			wantBase: domain.EventBase{Depth: 1, Turn: 2, CallID: "call-7"},
			wantData: `{"server":"local","endpoint":"http://127.0.0.1:8080/v1","model":"gpt-oss-20b",` +
				`"request_id":"req-3","index":1,"ttfb_ms":120,"ttft_ms":1800,"last_ms":4200,` +
				`"duration_ms":4250,"output_tokens":96,"outcome":"ok"}`,
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

// TestEncodeToolSummaryMirror pins the tagged `summary` object of ADR 0103 decision 9: one case per
// domain.ToolSummary variant, each naming its variant as `kind` ahead of its facts, and a nil
// summary written as `"summary":null` rather than dropped (ADR 0075 decision 3).
func TestEncodeToolSummaryMirror(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		summary     domain.ToolSummary
		wantSummary string
	}{
		{name: "nil", summary: nil, wantSummary: `null`},
		{
			name:        "read_span without a locate term",
			summary:     domain.ReadSpan{Start: 1, End: 40, Total: 120},
			wantSummary: `{"kind":"read_span","start":1,"end":40,"total":120,"locate":"","located_on":null}`,
		},
		{
			name:    "read_span with a locate term",
			summary: domain.ReadSpan{Start: 10, End: 20, Total: 90, Locate: "Encode", LocatedOn: []int{12, 77}},
			wantSummary: `{"kind":"read_span","start":10,"end":20,"total":90,"locate":"Encode",` +
				`"located_on":[12,77]}`,
		},
		{
			name:        "read_span whose locate term matched nothing",
			summary:     domain.ReadSpan{Start: 1, End: 5, Total: 5, Locate: "absent", LocatedOn: []int{}},
			wantSummary: `{"kind":"read_span","start":1,"end":5,"total":5,"locate":"absent","located_on":[]}`,
		},
		{
			name:        "listed_entries",
			summary:     domain.ListedEntries{Total: 48, Skipped: 20},
			wantSummary: `{"kind":"listed_entries","total":48,"skipped":20}`,
		},
		{
			name:        "matched_lines of zero",
			summary:     domain.MatchedLines{Total: 0},
			wantSummary: `{"kind":"matched_lines","total":0}`,
		},
		{
			name:        "diff_stat",
			summary:     domain.DiffStat{Added: 7, Removed: 3},
			wantSummary: `{"kind":"diff_stat","added":7,"removed":3}`,
		},
		{
			name:        "changed_files",
			summary:     domain.ChangedFiles{Staged: 2, Unstaged: 1, Untracked: 4},
			wantSummary: `{"kind":"changed_files","staged":2,"unstaged":1,"untracked":4}`,
		},
		{
			name: "edit_regions",
			summary: domain.EditRegions{Regions: []domain.EditRegion{{
				BeforeStart: 3,
				AfterStart:  3,
				Leading:     []string{"a", "b"},
				Removed:     []string{"old"},
				Inserted:    []string{"new", "newer"},
				Trailing:    nil,
			}}},
			wantSummary: `{"kind":"edit_regions","regions":[{"before_start":3,"after_start":3,` +
				`"leading":["a","b"],"removed":["old"],"inserted":["new","newer"],"trailing":null}]}`,
		},
		{
			name:        "edit_regions with none",
			summary:     domain.EditRegions{},
			wantSummary: `{"kind":"edit_regions","regions":null}`,
		},
		{
			name:        "search_hits",
			summary:     domain.SearchHits{Count: 5},
			wantSummary: `{"kind":"search_hits","count":5}`,
		},
		{
			name:    "delegation_outcome at a bound",
			summary: domain.DelegationOutcome{Bound: domain.DelegationTokenBudget},
			wantSummary: `{"kind":"delegation_outcome","bound":"token_budget","stopped_by_user":false,` +
				`"has_no_report":false,"steered_messages":0}`,
		},
		{
			name:    "delegation_outcome with no report",
			summary: domain.DelegationOutcome{HasNoReport: true, SteeredMessages: 2},
			wantSummary: `{"kind":"delegation_outcome","bound":"","stopped_by_user":false,` +
				`"has_no_report":true,"steered_messages":2}`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			event := domain.ToolResultEvent{
				Result: domain.ToolResult{CallID: "call-1", Content: "prose", Summary: c.summary},
				Tool:   "some_tool",
			}

			_, _, data, ok := Encode(event)
			encoded, err := json.Marshal(data)

			if !ok || err != nil {
				t.Fatalf("Encode ok = %v, json.Marshal err = %v", ok, err)
			}
			want := `{"result":{"call_id":"call-1","content":"prose","is_error":false,"summary":` +
				c.wantSummary + `},"tool":"some_tool","write_target":"","spawn_run_id":""}`
			if string(encoded) != want {
				t.Errorf("data JSON =\n  %s\nwant\n  %s", encoded, want)
			}
		})
	}
}

// TestEncodeSkipsTheWireEvent pins ADR 0075 decision 2: the Inspector's raw provider protocol is
// a variant the lines never carry, and a caller learns that from ok alone.
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

// TestEncodeWritesTheSubAgentGroupEvent pins ADR 0103 decision 13: a sub-agent group's announced
// size and width write a sub_agent_group line, stamped with the emitting agent's identity, so a
// watcher can show the members of a wave still queued.
func TestEncodeWritesTheSubAgentGroupEvent(t *testing.T) {
	t.Parallel()

	wantBase := domain.EventBase{Depth: 1, Turn: 1, CallID: "call-9", RunID: "0badc0de.2"}

	kind, base, data, ok := Encode(domain.SubAgentGroupEvent{EventBase: wantBase, Size: 3, Width: 1})

	if !ok || kind != "sub_agent_group" {
		t.Fatalf("Encode(SubAgentGroupEvent) = (%q, ok %v), want a sub_agent_group line", kind, ok)
	}
	if base != wantBase {
		t.Errorf("base = %+v, want %+v", base, wantBase)
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("json.Marshal(data): %v", err)
	}
	if want := `{"size":3,"width":1}`; string(encoded) != want {
		t.Errorf("data JSON = %s, want %s", encoded, want)
	}
}

// TestEncodeWritesTheDriverWorkflowPhases pins that the two phases describing a Workflow's shape —
// an item's run starting and a stage finishing — write a workflow_phase line like every other
// phase, with the members a Driver rebuilds the Workflow from: the item's run, attempt and short
// name, and the stage's round and outcome.
func TestEncodeWritesTheDriverWorkflowPhases(t *testing.T) {
	t.Parallel()

	cases := []struct {
		event    domain.WorkflowPhaseEvent
		wantData string
	}{
		{
			event: domain.WorkflowPhaseEvent{
				EventBase: domain.EventBase{Turn: 1},
				Phase:     domain.WorkflowItemStarted,
				Workflow:  "wf-1",
				Stage:     "items",
				Item:      "internal/tui",
				ItemName:  "tui",
				Index:     2,
				Round:     1,
				Run:       "0badc0de.4",
				Attempt:   2,
			},
			wantData: `{"phase":"item_started","workflow":"wf-1","name":"","stage":"items",` +
				`"item":"internal/tui","index":2,"resumed":false,` +
				`"receipt":{"status":"","summary":"","fields":{}},"detail":"","call":"",` +
				`"item_name":"tui","background":false,"origin":"","stages":null,"stage_kinds":null,` +
				`"items":0,"round":1,"rounds":0,"outcome":"","run":"0badc0de.4","attempt":2,` +
				`"resume":"","tally":null}`,
		},
		{
			event: domain.WorkflowPhaseEvent{
				EventBase: domain.EventBase{Turn: 1},
				Phase:     domain.WorkflowStageFinished,
				Workflow:  "wf-1",
				Stage:     "check",
				Round:     2,
				Outcome:   domain.WorkflowStageSkipped,
			},
			wantData: `{"phase":"stage_finished","workflow":"wf-1","name":"","stage":"check",` +
				`"item":"","index":0,"resumed":false,` +
				`"receipt":{"status":"","summary":"","fields":{}},"detail":"","call":"",` +
				`"item_name":"","background":false,"origin":"","stages":null,"stage_kinds":null,` +
				`"items":0,"round":2,"rounds":0,"outcome":"skipped","run":"","attempt":0,` +
				`"resume":"","tally":null}`,
		},
	}

	for _, c := range cases {
		t.Run(string(c.event.Phase), func(t *testing.T) {
			t.Parallel()

			kind, base, data, ok := Encode(c.event)

			if !ok || kind != "workflow_phase" {
				t.Fatalf("Encode(%s) = (%q, ok %v), want a workflow_phase line", c.event.Phase, kind, ok)
			}
			if base != c.event.EventBase {
				t.Errorf("base = %+v, want %+v", base, c.event.EventBase)
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

// TestEncodeSeamClosed pins the seam_closed line's `data` over every seam: `seam` is the seam's
// CLOSING-NOTICE name — the spelling a Reaction `on:` list and the manual use for the same fact —
// never the seam's own, `fired` lists the firings in order and is `[]` rather than null when the
// pass acted on nothing, and the variant's live Value never reaches the object. Encode is the pure
// mapping; whether a line is written at all is the Writer's decision, pinned in writer_test.go.
func TestEncodeSeamClosed(t *testing.T) {
	t.Parallel()

	cases := []struct {
		seam     domain.Moment
		fired    []string
		wantData string
	}{
		{
			seam:     domain.MomentPreRequest,
			fired:    []string{"context-fill-notice"},
			wantData: `{"seam":"pre-request-finished","fired":["context-fill-notice"]}`,
		},
		{
			seam:     domain.MomentPostResponse,
			fired:    []string{"tool-call-repair", "empty-reply-retry"},
			wantData: `{"seam":"post-response-finished","fired":["tool-call-repair","empty-reply-retry"]}`,
		},
		{
			seam:     domain.MomentPreToolExec,
			fired:    nil,
			wantData: `{"seam":"pre-tool-exec-finished","fired":[]}`,
		},
		{
			seam:     domain.MomentPostToolResult,
			fired:    []string{},
			wantData: `{"seam":"post-tool-result-finished","fired":[]}`,
		},
		{
			seam:     domain.MomentHistoryRewrite,
			fired:    []string{"compaction"},
			wantData: `{"seam":"history-rewrite-finished","fired":["compaction"]}`,
		},
	}

	for _, c := range cases {
		t.Run(string(c.seam), func(t *testing.T) {
			t.Parallel()

			wantBase := domain.EventBase{Depth: 1, Turn: 2, CallID: "call-7"}
			kind, base, data, ok := Encode(domain.SeamClosedEvent{
				EventBase: wantBase,
				Seam:      c.seam,
				Fired:     c.fired,
				Value:     domain.PostResponseMoment{},
			})

			if !ok {
				t.Fatalf("Encode(SeamClosedEvent{%s}) ok = false, want true", c.seam)
			}
			if kind != "seam_closed" {
				t.Errorf("kind = %q, want %q", kind, "seam_closed")
			}
			if base != wantBase {
				t.Errorf("base = %+v, want %+v", base, wantBase)
			}
			encoded, err := json.Marshal(data)
			if err != nil {
				t.Fatalf("json.Marshal(data): %v", err)
			}
			if string(encoded) != c.wantData {
				t.Errorf("data JSON =\n  %s\nwant\n  %s", encoded, c.wantData)
			}
			if strings.Contains(string(encoded), "value") {
				t.Errorf("the seam's live Value reached the line: %s", encoded)
			}
		})
	}
}

// TestEncodeUsageCarriesTheSpend pins the usage line's money members (ADR 0093) under the run's
// label: this call's exact amount and whether it was priced, the agent's running amount and
// unpriced count, and the label named only once a call behind the running sums was priced — so an
// unpriced reading writes the zero values and never a currency for an amount nobody priced.
func TestEncodeUsageCarriesTheSpend(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		event domain.UsageEvent
		want  string
	}{
		{
			name: "priced",
			event: domain.UsageEvent{
				CostMicros: 2_200,
				Priced:     true,
				Cumulative: domain.Usage{Calls: 2, CostMicros: 3_000, PricedCalls: 2},
			},
			want: `"cost":0.0022,"priced":true,"cumulative_cost":0.003,` +
				`"cumulative_unpriced_calls":0,"currency":"EUR"`,
		},
		{
			name: "unpriced",
			event: domain.UsageEvent{
				Cumulative: domain.Usage{Calls: 1, UnpricedCalls: 1},
			},
			want: `"cost":0,"priced":false,"cumulative_cost":0,` +
				`"cumulative_unpriced_calls":1,"currency":""`,
		},
		{
			name: "an unpriced call after a priced one",
			event: domain.UsageEvent{
				Cumulative: domain.Usage{Calls: 2, CostMicros: 2_200, PricedCalls: 1, UnpricedCalls: 1},
			},
			want: `"cost":0,"priced":false,"cumulative_cost":0.0022,` +
				`"cumulative_unpriced_calls":1,"currency":"EUR"`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			kind, _, data, ok := encode(c.event, "EUR")

			if !ok || kind != "usage" {
				t.Fatalf("encode(UsageEvent) = %q, ok %v; want a usage line", kind, ok)
			}
			encoded, err := json.Marshal(data)
			if err != nil {
				t.Fatalf("json.Marshal(data): %v", err)
			}
			if !strings.Contains(string(encoded), c.want) {
				t.Errorf("data JSON =\n  %s\nwant it to carry\n  %s", encoded, c.want)
			}
		})
	}
}

// sinkOnlyVariants names the one domain Event variant no line carries: the Inspector's raw provider
// protocol (ADR 0075 decision 2).
var sinkOnlyVariants = map[string]bool{"WireEvent": true}

// TestEncodeMapsEveryEventVariant pins ADR 0103's promise that the lines carry every Event a
// Driver folds: every variant internal/domain declares — read off its source, so a variant added
// there without a case here is a failing test — maps to a kind in Kinds(), WireEvent alone
// excepted, and every kind but the two frames is some variant's line.
func TestEncodeMapsEveryEventVariant(t *testing.T) {
	t.Parallel()

	variants := map[string]domain.Event{
		"TokenEvent":             domain.TokenEvent{},
		"ReasoningEvent":         domain.ReasoningEvent{},
		"StreamResetEvent":       domain.StreamResetEvent{},
		"MessageEvent":           domain.MessageEvent{},
		"ToolCallEvent":          domain.ToolCallEvent{},
		"ToolResultEvent":        domain.ToolResultEvent{},
		"SubAgentPhaseEvent":     domain.SubAgentPhaseEvent{},
		"SubAgentGroupEvent":     domain.SubAgentGroupEvent{},
		"SubAgentNamedEvent":     domain.SubAgentNamedEvent{},
		"ChildInterjectionEvent": domain.ChildInterjectionEvent{},
		"ApprovalEvent":          domain.ApprovalEvent{},
		"TurnEvent":              domain.TurnEvent{},
		"ReactionFiredEvent":     domain.ReactionFiredEvent{},
		"SeamClosedEvent":        domain.SeamClosedEvent{},
		"ErrorEvent":             domain.ErrorEvent{},
		"PruneEvent":             domain.PruneEvent{},
		"RefClippedEvent":        domain.RefClippedEvent{},
		"MalformedChunksEvent":   domain.MalformedChunksEvent{},
		"UsageEvent":             domain.UsageEvent{},
		"AuditEvent":             domain.AuditEvent{},
		"WireEvent":              domain.WireEvent{},
		"UpstreamAttemptEvent":   domain.UpstreamAttemptEvent{},
		"WorkflowPhaseEvent":     domain.WorkflowPhaseEvent{},
	}
	declared := declaredEventVariants(t)
	known := make(map[string]bool, len(Kinds()))
	for _, kind := range Kinds() {
		known[kind] = true
	}

	for _, name := range declared {
		if _, listed := variants[name]; !listed {
			t.Errorf("internal/domain declares the Event variant %s, which this test does not list", name)
		}
	}
	if len(declared) != len(variants) {
		t.Errorf("internal/domain declares %d Event variants %v; this test lists %d",
			len(declared), declared, len(variants))
	}
	written := map[string]bool{}
	for name, event := range variants {
		kind, _, _, ok := Encode(event)
		switch {
		case sinkOnlyVariants[name] && ok:
			t.Errorf("Encode(%s) wrote a %q line; the variant is sink-only", name, kind)
		case !sinkOnlyVariants[name] && (!ok || !known[kind]):
			t.Errorf("Encode(%s) = (%q, ok %v); want a kind Kinds() names", name, kind, ok)
		}
		written[kind] = ok
	}
	for _, kind := range Kinds()[:len(Kinds())-2] {
		if !written[kind] {
			t.Errorf("no Event variant writes the %q kind", kind)
		}
	}
}

// declaredEventVariants reads internal/domain's source for every struct type that embeds
// EventBase — the variants of the sealed Event sum — and returns their names, sorted.
func declaredEventVariants(t *testing.T) []string {
	t.Helper()

	files, err := filepath.Glob(filepath.Join("..", "domain", "*.go"))
	if err != nil {
		t.Fatalf("glob internal/domain: %v", err)
	}
	var names []string
	fileSet := token.NewFileSet()
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			spec, isType := node.(*ast.TypeSpec)
			if isType && embedsEventBase(spec) {
				names = append(names, spec.Name.Name)
			}
			return true
		})
	}
	if len(names) == 0 {
		t.Fatal("found no Event variant in internal/domain; the source scan has stopped working")
	}
	sort.Strings(names)
	return names
}

// embedsEventBase reports whether spec declares a struct with an embedded EventBase field.
func embedsEventBase(spec *ast.TypeSpec) bool {
	structType, isStruct := spec.Type.(*ast.StructType)
	if !isStruct {
		return false
	}
	for _, field := range structType.Fields.List {
		ident, isIdent := field.Type.(*ast.Ident)
		if isIdent && len(field.Names) == 0 && ident.Name == "EventBase" {
			return true
		}
	}
	return false
}

// TestEncodeSkipsAnUnknownEvent covers the sealed type's default arm: a variant this package has
// not been taught is skipped rather than written as a half-line.
func TestEncodeSkipsAnUnknownEvent(t *testing.T) {
	t.Parallel()

	if _, _, _, ok := Encode(nil); ok {
		t.Errorf("Encode(nil) ok = true, want false")
	}
}

// TestKindsAreTwentyFour pins the vocabulary itself — the twenty-two serialized variants, the
// opt-in seam_closed among them, plus the two frames — so a kind added to the encoder without a
// manual entry, or an entry without a kind, is a failing test rather than a documentation drift.
func TestKindsAreTwentyFour(t *testing.T) {
	t.Parallel()

	kinds := Kinds()

	if len(kinds) != 24 {
		t.Fatalf("len(Kinds()) = %d, want 24: %v", len(kinds), kinds)
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
		t.Errorf("Kinds() must be snake_case, not a notice Moment's kebab-case: %v", kinds)
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
