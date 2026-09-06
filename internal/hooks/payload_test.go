package hooks

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
				Event: TurnFinished, Hook: "notify", Time: "2026-09-06T09:41:00Z",
				Workspace: "/work/repo", Depth: 0, Turn: 3,
				Status: "turn-complete",
			},
			want: `{"event":"turn-finished","hook":"notify","time":"2026-09-06T09:41:00Z",` +
				`"workspace":"/work/repo","depth":0,"turn":3,"status":"turn-complete"}`,
		},
		{
			name: "exchange-finished",
			payload: Payload{
				Event: ExchangeFinished, Hook: "notify", Time: "2026-09-06T09:41:00Z",
				Workspace: "/work/repo", Depth: 0, Turn: 7,
				Status: "exchange-complete", Faulted: true, StepCapped: true,
			},
			want: `{"event":"exchange-finished","hook":"notify","time":"2026-09-06T09:41:00Z",` +
				`"workspace":"/work/repo","depth":0,"turn":7,"status":"exchange-complete",` +
				`"faulted":true,"step_capped":true}`,
		},
		{
			name: "file-changed",
			payload: Payload{
				Event: FileChanged, Hook: "fmt", Time: "2026-09-06T09:41:00Z",
				Workspace: "/work/repo", Depth: 1, Turn: 2, CallID: "call-7",
				Tool: "write_file", Path: "/work/repo/main.go",
			},
			want: `{"event":"file-changed","hook":"fmt","time":"2026-09-06T09:41:00Z",` +
				`"workspace":"/work/repo","depth":1,"turn":2,"call_id":"call-7",` +
				`"tool":"write_file","path":"/work/repo/main.go"}`,
		},
		{
			name: "approval-waiting",
			payload: Payload{
				Event: ApprovalWaiting, Hook: "bell", Time: "2026-09-06T09:41:00Z",
				Workspace: "/work/repo", Depth: 1, Turn: 4, CallID: "call-2",
				Tool: "terminal", Reason: "write", Remedy: "run `apogee doctor`",
				SubAgentName: "docs sweep", Scope: "reads the package directory",
			},
			want: `{"event":"approval-waiting","hook":"bell","time":"2026-09-06T09:41:00Z",` +
				`"workspace":"/work/repo","depth":1,"turn":4,"call_id":"call-2","tool":"terminal",` +
				`"reason":"write","remedy":"run ` + "`apogee doctor`" + `",` +
				`"sub_agent_name":"docs sweep","scope":"reads the package directory"}`,
		},
		{
			name: "error",
			payload: Payload{
				Event: Error, Hook: "page", Time: "2026-09-06T09:41:00Z",
				Workspace: "/work/repo", Depth: 0, Turn: 1,
				Schedule: &ScheduleRef{ID: "nightly", Name: "Nightly docs sweep"},
				Source:   "terminal", Error: "exit status 1",
			},
			want: `{"event":"error","hook":"page","time":"2026-09-06T09:41:00Z",` +
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
				Event: FileChanged, Hook: "fmt", Workspace: "/work/repo", Path: "/work/repo/main.go",
			},
			want: []string{
				"APOGEE_HOOK_EVENT=file-changed",
				"APOGEE_HOOK_NAME=fmt",
				"APOGEE_HOOK_WORKSPACE=/work/repo",
				"APOGEE_HOOK_PATH=/work/repo/main.go",
			},
		},
		{
			name: "a Firing carries the Schedule id and name",
			payload: Payload{
				Event: TurnFinished, Hook: "notify", Workspace: "/work/repo",
				Schedule: &ScheduleRef{ID: "nightly", Name: "Nightly docs sweep"},
			},
			want: []string{
				"APOGEE_HOOK_EVENT=turn-finished",
				"APOGEE_HOOK_NAME=notify",
				"APOGEE_HOOK_WORKSPACE=/work/repo",
				"APOGEE_HOOK_SCHEDULE_ID=nightly",
				"APOGEE_HOOK_SCHEDULE_NAME=Nightly docs sweep",
			},
		},
		{
			name:    "an unset fact is omitted rather than blanked",
			payload: Payload{Event: Error, Hook: "page"},
			want: []string{
				"APOGEE_HOOK_EVENT=error",
				"APOGEE_HOOK_NAME=page",
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
