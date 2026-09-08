package reactions

import (
	"encoding/json"
	"strings"
	"testing"
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
