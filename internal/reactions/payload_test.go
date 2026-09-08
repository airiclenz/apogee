package reactions

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// TestPayloadJSONGolden pins the wire shape of every event's document. These field names are the
// documented contract a user's script reads by name, so this test is what makes a rename a
// deliberate break rather than a silent one.
func TestPayloadJSONGolden(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		payload Payload
		want    string
	}{
		{
			name: "turn-finished",
			payload: Payload{
				Event: TurnFinished, Reaction: "notify", Time: "2026-09-06T09:41:00Z",
				Workspace: "/work/repo", Depth: 0, Turn: 3,
				Status: "turn-complete",
			},
			want: `{"event":"turn-finished","reaction":"notify","time":"2026-09-06T09:41:00Z",` +
				`"workspace":"/work/repo","depth":0,"turn":3,"status":"turn-complete"}`,
		},
		{
			name: "exchange-finished",
			payload: Payload{
				Event: ExchangeFinished, Reaction: "notify", Time: "2026-09-06T09:41:00Z",
				Workspace: "/work/repo", Depth: 0, Turn: 7,
				Status: "exchange-complete", Faulted: true, StepCapped: true,
			},
			want: `{"event":"exchange-finished","reaction":"notify","time":"2026-09-06T09:41:00Z",` +
				`"workspace":"/work/repo","depth":0,"turn":7,"status":"exchange-complete",` +
				`"faulted":true,"step_capped":true}`,
		},
		{
			name: "file-changed",
			payload: Payload{
				Event: FileChanged, Reaction: "fmt", Time: "2026-09-06T09:41:00Z",
				Workspace: "/work/repo", Depth: 1, Turn: 2, CallID: "call-7",
				Tool: "write_file", Path: "/work/repo/main.go",
			},
			want: `{"event":"file-changed","reaction":"fmt","time":"2026-09-06T09:41:00Z",` +
				`"workspace":"/work/repo","depth":1,"turn":2,"call_id":"call-7",` +
				`"tool":"write_file","path":"/work/repo/main.go"}`,
		},
		{
			name: "approval-requested",
			payload: Payload{
				Event: ApprovalRequested, Reaction: "bell", Time: "2026-09-06T09:41:00Z",
				Workspace: "/work/repo", Depth: 1, Turn: 4, CallID: "call-2",
				Tool: "terminal", Reason: "write", Remedy: "run `apogee doctor`",
				SubAgentName: "docs sweep", Scope: "reads the package directory",
			},
			want: `{"event":"approval-requested","reaction":"bell","time":"2026-09-06T09:41:00Z",` +
				`"workspace":"/work/repo","depth":1,"turn":4,"call_id":"call-2","tool":"terminal",` +
				`"reason":"write","remedy":"run ` + "`apogee doctor`" + `",` +
				`"sub_agent_name":"docs sweep","scope":"reads the package directory"}`,
		},
		{
			name: "approval-decided",
			payload: Payload{
				Event: ApprovalDecided, Reaction: "bell", Time: "2026-09-06T09:41:00Z",
				Workspace: "/work/repo", Depth: 1, Turn: 4, CallID: "call-2",
				Tool: "terminal", Reason: "write", Decision: "allow",
			},
			want: `{"event":"approval-decided","reaction":"bell","time":"2026-09-06T09:41:00Z",` +
				`"workspace":"/work/repo","depth":1,"turn":4,"call_id":"call-2","tool":"terminal",` +
				`"reason":"write","decision":"allow"}`,
		},
		{
			name: "error",
			payload: Payload{
				Event: Error, Reaction: "page", Time: "2026-09-06T09:41:00Z",
				Workspace: "/work/repo", Depth: 0, Turn: 1,
				Schedule: &ScheduleRef{ID: "nightly", Name: "Nightly docs sweep"},
				Source:   "terminal", Error: "exit status 1",
			},
			want: `{"event":"error","reaction":"page","time":"2026-09-06T09:41:00Z",` +
				`"workspace":"/work/repo","depth":0,"turn":1,` +
				`"schedule":{"id":"nightly","name":"Nightly docs sweep"},` +
				`"source":"terminal","error":"exit status 1"}`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			encoded, err := json.Marshal(c.payload)

			if err != nil {
				t.Fatalf("json.Marshal: %v", err)
			}
			if string(encoded) != c.want {
				t.Errorf("payload JSON =\n  %s\nwant\n  %s", encoded, c.want)
			}
		})
	}
}

// TestPayloadEnv renders the headline facts a one-line script reads without parsing stdin.
func TestPayloadEnv(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		payload Payload
		want    []string
	}{
		{
			name: "a session firing carries no schedule",
			payload: Payload{
				Event: FileChanged, Reaction: "fmt", Workspace: "/work/repo", Path: "/work/repo/main.go",
			},
			want: []string{
				"APOGEE_REACTION_EVENT=file-changed",
				"APOGEE_REACTION_NAME=fmt",
				"APOGEE_REACTION_WORKSPACE=/work/repo",
				"APOGEE_REACTION_PATH=/work/repo/main.go",
			},
		},
		{
			name: "a Firing carries the Schedule id and name",
			payload: Payload{
				Event: TurnFinished, Reaction: "notify", Workspace: "/work/repo",
				Schedule: &ScheduleRef{ID: "nightly", Name: "Nightly docs sweep"},
			},
			want: []string{
				"APOGEE_REACTION_EVENT=turn-finished",
				"APOGEE_REACTION_NAME=notify",
				"APOGEE_REACTION_WORKSPACE=/work/repo",
				"APOGEE_REACTION_SCHEDULE_ID=nightly",
				"APOGEE_REACTION_SCHEDULE_NAME=Nightly docs sweep",
			},
		},
		{
			name:    "an unset fact is omitted rather than blanked",
			payload: Payload{Event: Error, Reaction: "page"},
			want: []string{
				"APOGEE_REACTION_EVENT=error",
				"APOGEE_REACTION_NAME=page",
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			got := c.payload.Env()

			if strings.Join(got, "\n") != strings.Join(c.want, "\n") {
				t.Errorf("Env() = %q, want %q", got, c.want)
			}
		})
	}
}

// TestSeamClosedPayloadJSONGolden pins the document a seam-closing notice delivers: the seam that
// closed, the ids that acted during the pass, and the projection of the working value the pass
// left behind. Like the goldens above these names are the contract a user's script reads, and
// unlike them the "value" shape is per-seam — so each of the five is pinned on its own.
func TestSeamClosedPayloadJSONGolden(t *testing.T) {
	t.Parallel()

	call := domain.ToolCall{ID: "call-2", Tool: "write_file", Arguments: json.RawMessage(`{"path":"main.go"}`)}
	result := domain.ToolResult{CallID: "call-2", Content: "wrote 12 lines"}

	cases := []struct {
		name  string
		seam  domain.Moment
		fired []string
		value any
		want  string
	}{
		{
			name:  "pre-request-finished",
			seam:  domain.MomentPreRequest,
			fired: []string{"context-files"},
			value: domain.NewRequest("qwen", []domain.Message{
				{Role: domain.RoleSystem, Content: "be brief"},
				{Role: domain.RoleUser, Content: "fix the build"},
			}, []domain.ToolDef{{Name: "read_file"}, {Name: "write_file"}}, domain.Budget{}, 3),
			want: `"seam":"pre-request","reactions":["context-files"],` +
				`"value":{"messages":[{"role":"system","content":"be brief"},` +
				`{"role":"user","content":"fix the build"}],"tools":["read_file","write_file"]}}`,
		},
		{
			name:  "post-response-finished",
			seam:  domain.MomentPostResponse,
			fired: []string{"tool-call-repair"},
			value: domain.PostResponseMoment{
				Resp: domain.NewResponse("running the tests", "", []domain.ToolCall{
					{ID: "call-1", Tool: "terminal", Arguments: json.RawMessage(`{"command":"go test ./..."}`)},
				}, domain.FinishToolCalls, nil),
				Retryable: true,
			},
			want: `"seam":"post-response","reactions":["tool-call-repair"],` +
				`"value":{"text":"running the tests","tool_calls":[{"id":"call-1","name":"terminal",` +
				`"arguments":{"command":"go test ./..."}}],"retryable":true}}`,
		},
		{
			name:  "pre-tool-exec-finished, the ordinary pass in which nothing acted",
			seam:  domain.MomentPreToolExec,
			fired: nil,
			value: domain.NewToolCallEdit(&call),
			want: `"seam":"pre-tool-exec",` +
				`"value":{"id":"call-2","name":"write_file","arguments":{"path":"main.go"}}}`,
		},
		{
			name:  "post-tool-result-finished",
			seam:  domain.MomentPostToolResult,
			fired: []string{"tool-result-cap"},
			value: domain.ToolResultMoment{Call: call, Edit: domain.NewToolResultEdit(&result)},
			want: `"seam":"post-tool-result","reactions":["tool-result-cap"],` +
				`"value":{"call":{"id":"call-2","name":"write_file","arguments":{"path":"main.go"}},` +
				`"content":"wrote 12 lines","is_error":false}}`,
		},
		{
			name:  "history-rewrite-finished",
			seam:  domain.MomentHistoryRewrite,
			fired: []string{"prune"},
			value: domain.NewConversation([]domain.Message{
				{Role: domain.RoleUser, Content: "fix the build"},
				{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{call}},
				{Role: domain.RoleTool, ToolCallID: "call-2", Content: "wrote 12 lines"},
			}),
			want: `"seam":"history-rewrite","reactions":["prune"],` +
				`"value":{"messages":[{"role":"user","content":"fix the build"},` +
				`{"role":"assistant","tool_calls":[{"id":"call-2","name":"write_file",` +
				`"arguments":{"path":"main.go"}}]},` +
				`{"role":"tool","content":"wrote 12 lines","tool_call_id":"call-2"}]}}`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			payload := Payload{
				Event: c.seam.Closing(), Reaction: "watch", Time: "2026-09-08T09:41:00Z",
				Workspace: "/work/repo", Turn: 4,
				Seam: c.seam, Reactions: c.fired, Value: projectSeamValue(c.seam, c.value),
			}

			encoded, err := json.Marshal(payload)

			if err != nil {
				t.Fatalf("json.Marshal: %v", err)
			}
			want := `{"event":"` + string(c.seam.Closing()) + `","reaction":"watch",` +
				`"time":"2026-09-08T09:41:00Z","workspace":"/work/repo","depth":0,"turn":4,` + c.want
			if string(encoded) != want {
				t.Errorf("payload JSON =\n  %s\nwant\n  %s", encoded, want)
			}
		})
	}
}

// TestProjectSeamValueRefusesAValueTheSeamDoesNotCarry — the projector reads a stream it did not
// build, so a pair the engine could not have produced must cost an absent "value" rather than a
// panic on the engine's own goroutine.
func TestProjectSeamValueRefusesAValueTheSeamDoesNotCarry(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		seam  domain.Moment
		value any
	}{
		{name: "another seam's payload", seam: domain.MomentPreRequest, value: domain.PostResponseMoment{}},
		{name: "a nil request", seam: domain.MomentPreRequest, value: (*domain.Request)(nil)},
		{name: "a nil conversation", seam: domain.MomentHistoryRewrite, value: (*domain.Conversation)(nil)},
		{name: "a moment with no response", seam: domain.MomentPostResponse, value: domain.PostResponseMoment{}},
		{name: "a moment with no edit", seam: domain.MomentPostToolResult, value: domain.ToolResultMoment{}},
		{name: "no value at all", seam: domain.MomentPreToolExec, value: nil},
		{name: "a notice rather than a seam", seam: domain.MomentTurnFinished, value: "anything"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			if got := projectSeamValue(c.seam, c.value); got != nil {
				t.Errorf("projectSeamValue = %#v, want nil", got)
			}
		})
	}
}
