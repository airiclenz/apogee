package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// The status vocabulary (activity.text)
// ----------------------------------------------------------------------------

// TestActivityText proves the phrase every kind renders, that idle says nothing at all, and
// that a Depth > 0 activity is prefixed with the same sub-agent label the transcript rail uses.
func TestActivityText(t *testing.T) {
	tests := []struct {
		name string
		act  activity
		want string
	}{
		{name: "idle renders nothing", act: activity{kind: actIdle}, want: ""},
		{name: "idle at depth still renders nothing", act: activity{kind: actIdle, depth: 1}, want: ""},
		{name: "thinking", act: activity{kind: actThinking}, want: "thinking"},
		{name: "responding", act: activity{kind: actResponding}, want: "responding"},
		{name: "retrying", act: activity{kind: actRetrying}, want: "retrying"},
		{name: "compacting", act: activity{kind: actCompacting}, want: "compacting"},
		{name: "stopping", act: activity{kind: actStopping}, want: "stopping"},
		{name: "tool renders its label", act: activity{kind: actTool, label: "reading · main.go"}, want: "reading · main.go"},
		{name: "tool with no label says nothing", act: activity{kind: actTool}, want: ""},
		{
			name: "sub-agent prefixes the phrase",
			act:  activity{kind: actThinking, depth: 1},
			want: "sub-agent · thinking",
		},
		{
			name: "sub-agent prefixes a tool phrase at any depth",
			act:  activity{kind: actTool, label: "searching · TODO", depth: 2},
			want: "sub-agent · searching · TODO",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.act.text(); got != tc.want {
				t.Errorf("text() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestToolActivityLabel proves the actTool phrase is built from the presentation registry: the
// tool's active verb, its target when the call names one, the raw-name fallback for an
// unregistered (MCP) tool, and a status-tight clip so a long target cannot crowd out the gauge.
func TestToolActivityLabel(t *testing.T) {
	longPath := "internal/tui/" + strings.Repeat("deeply-nested/", 6) + "main.go"

	tests := []struct {
		name string
		call domain.ToolCall
		want string
	}{
		{
			name: "registered tool with a target",
			call: domain.ToolCall{Tool: "read_file", Arguments: []byte(`{"path":"main.go"}`)},
			want: "reading · main.go",
		},
		{
			name: "registered tool with a command target",
			call: domain.ToolCall{Tool: "terminal", Arguments: []byte(`{"command":"npm test"}`)},
			want: "running · npm test",
		},
		{
			name: "registered tool with no target argument → the bare verb",
			call: domain.ToolCall{Tool: "read_file"},
			want: "reading",
		},
		{
			name: "unregistered (MCP) tool falls back to the raw name",
			call: domain.ToolCall{Tool: "mcp_weather", Arguments: []byte(`{"city":"Oslo"}`)},
			want: "running mcp_weather",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := toolActivityLabel(tc.call); got != tc.want {
				t.Errorf("toolActivityLabel() = %q, want %q", got, tc.want)
			}
		})
	}

	t.Run("a long target is clipped to the status cap", func(t *testing.T) {
		got := toolActivityLabel(domain.ToolCall{
			Tool:      "read_file",
			Arguments: []byte(`{"path":"` + longPath + `"}`),
		})
		if !strings.HasPrefix(got, "reading · ") {
			t.Fatalf("clipped label lost its verb: %q", got)
		}
		target := strings.TrimPrefix(got, "reading · ")
		if !strings.HasSuffix(target, "…") {
			t.Errorf("long target %q was not clipped: %q", longPath, target)
		}
		if n := len([]rune(target)); n != statusTargetRunes+1 { // the cap plus the ellipsis
			t.Errorf("clipped target is %d runes, want %d", n, statusTargetRunes+1)
		}
	})
}

// ----------------------------------------------------------------------------
// The elapsed clock
// ----------------------------------------------------------------------------

// TestFormatElapsed pins the clock's two forms and the minute boundary between them: bare
// seconds below a minute, "Nm SSs" with zero-padded seconds above it, and no hour form (a long
// call keeps counting minutes).
func TestFormatElapsed(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{d: -time.Second, want: "0s"}, // a clock that moved backwards never renders negative
		{d: 0, want: "0s"},
		{d: 900 * time.Millisecond, want: "0s"}, // sub-second truncates, never rounds up
		{d: 3 * time.Second, want: "3s"},
		{d: 59 * time.Second, want: "59s"},
		{d: 60 * time.Second, want: "1m 00s"},
		{d: 61 * time.Second, want: "1m 01s"},
		{d: 64 * time.Second, want: "1m 04s"},
		{d: 599 * time.Second, want: "9m 59s"},
		{d: 3600 * time.Second, want: "60m 00s"},
		{d: 3661 * time.Second, want: "61m 01s"},
	}
	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			if got := formatElapsed(tc.d); got != tc.want {
				t.Errorf("formatElapsed(%v) = %q, want %q", tc.d, got, tc.want)
			}
		})
	}
}

// TestActivityElapsed proves the clock measures from the activity's own start and never
// reports an absurd duration for an activity that was never given one.
func TestActivityElapsed(t *testing.T) {
	now := time.Now()
	if got := (activity{}).elapsed(now); got != 0 {
		t.Errorf("a zero since elapsed to %v, want 0", got)
	}
	if got := (activity{since: now.Add(-5 * time.Second)}).elapsed(now); got != 5*time.Second {
		t.Errorf("elapsed = %v, want 5s", got)
	}
	if got := (activity{since: now.Add(time.Second)}).elapsed(now); got != 0 {
		t.Errorf("a future since elapsed to %v, want 0", got)
	}
}

// ----------------------------------------------------------------------------
// The fold (foldActivity)
// ----------------------------------------------------------------------------

// foldEvent folds one Event through the eventMsg path — transcript first, then the activity —
// exactly as Update does, so the ToolResultEvent rule sees the pairing apply establishes.
func foldEvent(m Model, e domain.Event) Model {
	m.transcript.apply(e)
	return m.foldActivity(e)
}

// TestFoldActivitySequence walks a realistic turn — reasoning, streamed text, a tool call, its
// result, the closing message — and asserts the phrase at every step.
func TestFoldActivitySequence(t *testing.T) {
	m := newTestModel(t)

	steps := []struct {
		name  string
		event domain.Event
		want  string
	}{
		{name: "reasoning", event: domain.ReasoningEvent{Text: "hmm"}, want: "thinking"},
		{name: "token", event: domain.TokenEvent{Text: "I will "}, want: "responding"},
		{
			name:  "tool call",
			event: domain.ToolCallEvent{Call: domain.ToolCall{ID: "1", Tool: "read_file", Arguments: []byte(`{"path":"main.go"}`)}},
			want:  "reading · main.go",
		},
		{
			name:  "tool result → back to thinking",
			event: domain.ToolResultEvent{Result: domain.ToolResult{CallID: "1", Content: "ok"}},
			want:  "thinking",
		},
		{name: "message keeps thinking (the loop may step again)", event: domain.MessageEvent{Text: "done"}, want: "thinking"},
	}
	for _, s := range steps {
		m = foldEvent(m, s.event)
		if got := m.act.text(); got != s.want {
			t.Errorf("after %s the phrase is %q, want %q", s.name, got, s.want)
		}
	}

	// A re-streamed turn says so.
	m = foldEvent(m, domain.StreamResetEvent{})
	if got := m.act.text(); got != "retrying" {
		t.Errorf("after a stream reset the phrase is %q, want %q", got, "retrying")
	}
}

// TestFoldActivityClockRunsPerPhrase proves the elapsed clock belongs to the phrase, not to
// the exchange: consecutive TokenEvents keep one running clock, and a changed phrase restarts it.
func TestFoldActivityClockRunsPerPhrase(t *testing.T) {
	m := newTestModel(t)

	m = foldEvent(m, domain.TokenEvent{Text: "one"})
	started := m.act.since
	if started.IsZero() {
		t.Fatal("the first token did not start the clock")
	}
	for i := 0; i < 3; i++ {
		m = foldEvent(m, domain.TokenEvent{Text: "more"})
	}
	if !m.act.since.Equal(started) {
		t.Errorf("a stream of tokens restarted the clock (%v → %v)", started, m.act.since)
	}

	m = foldEvent(m, domain.MessageEvent{Text: "done"})
	if m.act.since.Equal(started) {
		t.Error("the phrase changed to thinking but the clock kept the responding start")
	}
}

// TestFoldActivityDepthPrefixesSubAgent proves a nested (Depth > 0) event renders under the
// sub-agent label, and that the parent resuming at Depth 0 drops the prefix again.
func TestFoldActivityDepthPrefixesSubAgent(t *testing.T) {
	m := newTestModel(t)

	m = foldEvent(m, domain.ToolCallEvent{
		EventBase: domain.EventBase{Depth: 1},
		Call:      domain.ToolCall{ID: "1", Tool: "grep", Arguments: []byte(`{"pattern":"TODO"}`)},
	})
	if got, want := m.act.text(), "sub-agent · searching · TODO"; got != want {
		t.Errorf("nested tool phrase = %q, want %q", got, want)
	}

	m = foldEvent(m, domain.MessageEvent{Text: "back"})
	if got, want := m.act.text(), "thinking"; got != want {
		t.Errorf("phrase after the parent resumed = %q, want %q", got, want)
	}
}

// TestFoldActivityBatchStaysOnTool proves a batch of calls holds the tool phrase until the
// last result lands — one result while another call is still open must not claim the model is
// thinking again.
func TestFoldActivityBatchStaysOnTool(t *testing.T) {
	m := newTestModel(t)
	m = foldEvent(m, domain.ToolCallEvent{Call: domain.ToolCall{ID: "1", Tool: "read_file", Arguments: []byte(`{"path":"a.go"}`)}})
	m = foldEvent(m, domain.ToolCallEvent{Call: domain.ToolCall{ID: "2", Tool: "read_file", Arguments: []byte(`{"path":"b.go"}`)}})

	m = foldEvent(m, domain.ToolResultEvent{Result: domain.ToolResult{CallID: "1", Content: "ok"}})
	if got, want := m.act.text(), "reading · b.go"; got != want {
		t.Errorf("phrase with one call still open = %q, want %q", got, want)
	}

	m = foldEvent(m, domain.ToolResultEvent{Result: domain.ToolResult{CallID: "2", Content: "ok"}})
	if got, want := m.act.text(), "thinking"; got != want {
		t.Errorf("phrase after the batch drained = %q, want %q", got, want)
	}
}

// TestFoldActivityStoppingIsSticky proves the stop the human asked for stays on screen: the
// worker keeps emitting until it reaches a quiescent boundary, and none of those events may
// overwrite "stopping". Only finishWorker clears it.
func TestFoldActivityStoppingIsSticky(t *testing.T) {
	m := newTestModel(t)
	m.setActivity(actStopping, "", 0)

	for _, e := range []domain.Event{
		domain.ReasoningEvent{Text: "still going"},
		domain.TokenEvent{Text: "tail"},
		domain.ToolCallEvent{Call: domain.ToolCall{ID: "1", Tool: "read_file", Arguments: []byte(`{"path":"a.go"}`)}},
		domain.ToolResultEvent{Result: domain.ToolResult{CallID: "1", Content: "ok"}},
		domain.MessageEvent{Text: "done"},
		domain.StreamResetEvent{},
	} {
		m = foldEvent(m, e)
		if got := m.act.text(); got != "stopping" {
			t.Fatalf("%T overwrote the sticky stop phrase with %q", e, got)
		}
	}

	m.finishWorker(stateIdle)
	if got := m.act.text(); got != "" {
		t.Errorf("finishWorker left the phrase %q, want the idle empty slot", got)
	}
}

// TestFoldActivityIgnoresObservationalEvents proves the accounting and audit events leave the
// live phrase alone — the status line must not flicker off the work actually in flight.
func TestFoldActivityIgnoresObservationalEvents(t *testing.T) {
	m := newTestModel(t)
	m = foldEvent(m, domain.ToolCallEvent{Call: domain.ToolCall{ID: "1", Tool: "terminal", Arguments: []byte(`{"command":"go test"}`)}})
	want := m.act

	for _, e := range []domain.Event{
		domain.UsageEvent{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		domain.ErrorEvent{Source: "loop", Err: "recovered"},
		domain.AuditEvent{Tool: "terminal", CallID: "1", Decision: "allowed"},
		domain.MechanismFiredEvent{Mechanism: "m", Hook: "h", Action: "a"},
		domain.ApprovalEvent{Request: domain.ApprovalRequest{Tool: "terminal"}, Decision: domain.ApprovalAllow},
	} {
		m = foldEvent(m, e)
		if m.act != want {
			t.Errorf("%T changed the activity: %+v, want %+v", e, m.act, want)
		}
	}
}
