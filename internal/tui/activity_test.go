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
		{name: "tool renders its label", act: activity{kind: actTool, label: "reading"}, want: "reading"},
		{name: "tool with no label says nothing", act: activity{kind: actTool}, want: ""},
		{
			name: "sub-agent prefixes the phrase",
			act:  activity{kind: actThinking, depth: 1},
			want: "sub-agent · thinking",
		},
		{
			name: "sub-agent prefixes a tool phrase at any depth",
			act:  activity{kind: actTool, label: "searching", depth: 2},
			want: "sub-agent · searching",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.act.text(""); got != tc.want {
				t.Errorf("text() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestToolActivityVerb proves the actTool phrase is built from the presentation registry and is the
// tool's active verb ALONE — the raw-name fallback for an unregistered (MCP) tool included. The
// target the registry also carries never reaches the status line: the tool-call block a line below
// already names it, and the path was what pushed the context gauge off the row.
func TestToolActivityVerb(t *testing.T) {
	longPath := "internal/tui/" + strings.Repeat("deeply-nested/", 6) + "main.go"

	tests := []struct {
		name string
		call domain.ToolCall
		want string
	}{
		{
			name: "registered tool with a target keeps only the verb",
			call: domain.ToolCall{Tool: "read_file", Arguments: []byte(`{"path":"main.go"}`)},
			want: "reading",
		},
		{
			name: "registered tool with a command target keeps only the verb",
			call: domain.ToolCall{Tool: "terminal", Arguments: []byte(`{"command":"npm test"}`)},
			want: "running",
		},
		{
			name: "registered tool with no target argument reads the same",
			call: domain.ToolCall{Tool: "read_file"},
			want: "reading",
		},
		{
			name: "unregistered (MCP) tool falls back to the raw name",
			call: domain.ToolCall{Tool: "mcp_weather", Arguments: []byte(`{"city":"Oslo"}`)},
			want: "running mcp_weather",
		},
		{
			name: "a target long enough to have crowded the row contributes nothing",
			call: domain.ToolCall{Tool: "read_file", Arguments: []byte(`{"path":"` + longPath + `"}`)},
			want: "reading",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := toolActivityVerb(tc.call, workspaceRoot{}); got != tc.want {
				t.Errorf("toolActivityVerb() = %q, want %q", got, tc.want)
			}
		})
	}
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
// The quiet clock (the stall guard)
// ----------------------------------------------------------------------------

// TestActivityQuiet pins the stall guard's firing rule at its own interface: whether the status
// line owes the human the fact that the engine has gone silent. WHETHER is the whole answer — the
// row spends it as a bare `quiet` qualifier in front of the activity's own clock, so the silence
// has no duration of its own to report. The rule is the incident's shape (2026-08-14) — a claim of
// "thinking" no event has backed for minutes — so the two kinds that make that claim are the only
// ones it speaks for. A silent tool call is the tool taking its time and says nothing about the
// engine; a stopping worker already tells the human what it is doing; a compaction emits nothing
// until it lands; and a threshold of 0 is the key's own off switch.
func TestActivityQuiet(t *testing.T) {
	now := time.Now()
	const after = 90 * time.Second
	silentFor := func(d time.Duration) time.Time { return now.Add(-d) }

	tests := []struct {
		name      string
		act       activity
		lastEvent time.Time
		after     time.Duration
		wantOwed  bool
	}{
		{
			name:      "thinking below the threshold owes nothing",
			act:       activity{kind: actThinking},
			lastEvent: silentFor(89 * time.Second),
			after:     after,
		},
		{
			name:      "thinking at the threshold reports the silence",
			act:       activity{kind: actThinking},
			lastEvent: silentFor(after),
			after:     after,
			wantOwed:  true,
		},
		{
			name:      "thinking well past it is owed just the same",
			act:       activity{kind: actThinking},
			lastEvent: silentFor(20 * time.Minute),
			after:     after,
			wantOwed:  true,
		},
		{
			name:      "responding is watched too",
			act:       activity{kind: actResponding},
			lastEvent: silentFor(3 * time.Minute),
			after:     after,
			wantOwed:  true,
		},
		{
			name:      "a tool call is never quiet, however long it runs",
			act:       activity{kind: actTool, label: "running"},
			lastEvent: silentFor(20 * time.Minute),
			after:     after,
		},
		{
			name:      "a stopping worker says nothing about silence",
			act:       activity{kind: actStopping},
			lastEvent: silentFor(20 * time.Minute),
			after:     after,
		},
		{
			name:      "a compaction is silent by design",
			act:       activity{kind: actCompacting},
			lastEvent: silentFor(20 * time.Minute),
			after:     after,
		},
		{
			name:      "retrying is not watched",
			act:       activity{kind: actRetrying},
			lastEvent: silentFor(20 * time.Minute),
			after:     after,
		},
		{
			name:      "an idle slot has no engine to be silent",
			act:       activity{kind: actIdle},
			lastEvent: silentFor(20 * time.Minute),
			after:     after,
		},
		{
			name:      "a zero threshold turns the guard off",
			act:       activity{kind: actThinking},
			lastEvent: silentFor(20 * time.Minute),
		},
		{
			name:      "a negative threshold is off too, never inverted",
			act:       activity{kind: actThinking},
			lastEvent: silentFor(20 * time.Minute),
			after:     -after,
		},
		{
			name:  "an engine never heard from is not reported as silent since the epoch",
			act:   activity{kind: actThinking},
			after: after,
		},
		{
			name:      "a clock that moved backwards reports nothing",
			act:       activity{kind: actThinking},
			lastEvent: now.Add(time.Minute),
			after:     after,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if owed := tc.act.quiet(tc.lastEvent, now, tc.after); owed != tc.wantOwed {
				t.Errorf("quiet() = %v, want %v", owed, tc.wantOwed)
			}
		})
	}
}

// TestMoveActivityRestampsOnlyWatchedKinds pins the quiet clock's SECOND seat to the same rule its
// reporting gate reads (isQuietWatched): a move restamps [Model.lastEvent] only when it moves to a
// kind the guard watches. Every move used to restamp, whatever it moved to, so the two seats were
// coupled only by hand — a kind added to quiet's gate would have inherited a restamp nobody chose.
// Compacting and stopping are the standing proof: both are silent by design, and a restamp there
// would tell the guard the engine had just spoken when nothing had.
//
// The table walks every kind in the vocabulary, so a new one has to be answered for here as well.
func TestMoveActivityRestampsOnlyWatchedKinds(t *testing.T) {
	tests := []struct {
		name        string
		kind        activityKind
		wantRestamp bool
	}{
		{name: "thinking is watched", kind: actThinking, wantRestamp: true},
		{name: "responding is watched", kind: actResponding, wantRestamp: true},
		{name: "a compaction leaves the clock alone", kind: actCompacting},
		{name: "a stopping worker leaves the clock alone", kind: actStopping},
		{name: "an open tool call leaves the clock alone", kind: actTool},
		{name: "a re-streamed turn leaves the clock alone", kind: actRetrying},
		{name: "an idle slot leaves the clock alone", kind: actIdle},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestModel(t)
			heardAt := time.Now().Add(-20 * time.Minute)
			m.lastEvent = heardAt

			m.moveActivity(activity{kind: tc.kind})

			if restamped := m.lastEvent.After(heardAt); restamped != tc.wantRestamp {
				t.Errorf("after a move to %v the clock restamped = %v, want %v", tc.kind, restamped, tc.wantRestamp)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// The fold (foldActivity)
// ----------------------------------------------------------------------------
//
// The phrase is asserted through Model.foldEvent (fold.go), the one owner of the fold and its
// order: it is what pairs a result with its call before the activity's ToolResultEvent rule
// reads the open-call fact, so a test that called foldActivity alone would have to reproduce
// the order by hand and could drift away from what Update actually does.

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
			want:  "reading",
		},
		{
			name:  "tool result → back to thinking",
			event: domain.ToolResultEvent{Result: domain.ToolResult{CallID: "1", Content: "ok"}},
			want:  "thinking",
		},
		{name: "message keeps thinking (the loop may step again)", event: domain.MessageEvent{Text: "done"}, want: "thinking"},
	}
	for _, s := range steps {
		m = m.foldEvent(s.event)
		if got := m.act.text(""); got != s.want {
			t.Errorf("after %s the phrase is %q, want %q", s.name, got, s.want)
		}
	}

	// A re-streamed turn says so.
	m = m.foldEvent(domain.StreamResetEvent{})
	if got := m.act.text(""); got != "retrying" {
		t.Errorf("after a stream reset the phrase is %q, want %q", got, "retrying")
	}
}

// TestFoldActivityClockRunsPerPhrase proves the elapsed clock belongs to the work in flight, not to
// the exchange: consecutive TokenEvents keep one running clock, and a changed phrase restarts it.
//
// A TOOL call is clocked on the call's own identity rather than on the phrase, because the phrase
// can no longer tell two of them apart — with the target gone, every read words itself "reading",
// and a clock keyed on that text would show the second file's call still counting the first one's
// seconds. Within one call the reverse holds: re-announcing it must not restart anything.
func TestFoldActivityClockRunsPerPhrase(t *testing.T) {
	m := newTestModel(t)

	m = m.foldEvent(domain.TokenEvent{Text: "one"})
	started := m.act.since
	if started.IsZero() {
		t.Fatal("the first token did not start the clock")
	}
	for i := 0; i < 3; i++ {
		m = m.foldEvent(domain.TokenEvent{Text: "more"})
	}
	if !m.act.since.Equal(started) {
		t.Errorf("a stream of tokens restarted the clock (%v → %v)", started, m.act.since)
	}

	m = m.foldEvent(domain.MessageEvent{Text: "done"})
	if m.act.since.Equal(started) {
		t.Error("the phrase changed to thinking but the clock kept the responding start")
	}

	read := func(id, path string) domain.Event {
		return domain.ToolCallEvent{Call: domain.ToolCall{
			ID: id, Tool: "read_file", Arguments: []byte(`{"path":"` + path + `"}`)}}
	}

	m = m.foldEvent(read("1", "a.go"))
	first := m.act.since
	if got, want := m.act.text(""), "reading"; got != want {
		t.Fatalf("tool phrase = %q, want %q — the rest of this test rests on the two calls reading alike", got, want)
	}

	// The same call announced again (a streamed argument settling) is the same work: one clock.
	m = m.foldEvent(read("1", "a.go"))
	if !m.act.since.Equal(first) {
		t.Errorf("a repeat announcement of call 1 restarted the clock (%v → %v)", first, m.act.since)
	}

	// A second call wording itself identically is nonetheless new work: a clock of its own.
	m = m.foldEvent(read("2", "b.go"))
	if m.act.since.Equal(first) {
		t.Error("a second call with the same verb kept the first call's clock")
	}
	if got, want := m.act.call, "2"; got != want {
		t.Errorf("the activity names call %q, want %q", got, want)
	}
}

// TestFoldActivityDepthPrefixesSubAgent proves a nested (Depth > 0) event renders under the
// sub-agent label, and that the parent resuming at Depth 0 drops the prefix again.
func TestFoldActivityDepthPrefixesSubAgent(t *testing.T) {
	m := newTestModel(t)

	m = m.foldEvent(domain.ToolCallEvent{
		EventBase: domain.EventBase{Depth: 1},
		Call:      domain.ToolCall{ID: "1", Tool: "grep", Arguments: []byte(`{"pattern":"TODO"}`)},
	})
	if got, want := m.act.text(""), "sub-agent · searching"; got != want {
		t.Errorf("nested tool phrase = %q, want %q", got, want)
	}

	m = m.foldEvent(domain.MessageEvent{Text: "back"})
	if got, want := m.act.text(""), "thinking"; got != want {
		t.Errorf("phrase after the parent resumed = %q, want %q", got, want)
	}
}

// TestStatusPhraseNamesTheActingDelegation proves the slot says WHICH delegate is working. Depth
// alone could only say "a sub-agent is", and with a fan-out running (ADR 0039) the slot names one
// child at a time — so a named delegation puts its name where the generic word was, resolved from
// the event's spawning call id against the run head the parent's own sub_agent call folded.
//
// The two fallbacks matter as much as the hit: a delegation that named nothing, and one whose head
// this transcript has not got (a child's first event beating its parent's tool call in, a replay),
// both read exactly as the line read before names existed. The clock is read at the activity's own
// start so the phrase is asserted whole, "0s" and all.
func TestStatusPhraseNamesTheActingDelegation(t *testing.T) {
	for _, tc := range []struct {
		name string
		head string // the spawning sub_agent call's arguments; "" = no run head folds at all
		want string
	}{
		{
			name: "a named delegation takes the prefix",
			head: `{"name":"repo-scout","task":"audit the config loader"}`,
			want: "repo-scout · responding · 0s",
		},
		{
			name: "an unnamed delegation keeps the generic word",
			head: `{"task":"audit the config loader"}`,
			want: subAgentActivityName + " · responding · 0s",
		},
		{
			name: "an unknown run reads as unnamed rather than as nothing",
			want: subAgentActivityName + " · responding · 0s",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestModel(t)
			if tc.head != "" {
				m = m.foldEvent(domain.ToolCallEvent{
					Call: domain.ToolCall{ID: "s1", Tool: "sub_agent", Arguments: []byte(tc.head)},
				})
			}
			m = m.foldEvent(domain.TokenEvent{
				EventBase: domain.EventBase{Depth: 1, CallID: "s1"},
				Text:      "working on it",
			})
			if got := strip(m.runningPhrase(m.act.since, false)); got != tc.want {
				t.Errorf("status phrase = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestStatusPhraseDropsTheNameWhenTheParentResumes proves the name is the ACTING agent's, not a mode
// the slot latches into: the parent's own events carry no spawning call, so its phrase is bare again.
func TestStatusPhraseDropsTheNameWhenTheParentResumes(t *testing.T) {
	m := newTestModel(t)
	m = m.foldEvent(domain.ToolCallEvent{
		Call: domain.ToolCall{ID: "s1", Tool: "sub_agent", Arguments: []byte(`{"name":"repo-scout","task":"audit"}`)},
	})
	m = m.foldEvent(domain.TokenEvent{EventBase: domain.EventBase{Depth: 1, CallID: "s1"}, Text: "working"})
	if got, want := strip(m.runningPhrase(m.act.since, false)), "repo-scout · responding · 0s"; got != want {
		t.Fatalf("delegate phrase = %q, want %q", got, want)
	}

	m = m.foldEvent(domain.MessageEvent{Text: "back"})
	if got, want := strip(m.runningPhrase(m.act.since, false)), "thinking · 0s"; got != want {
		t.Errorf("phrase after the parent resumed = %q, want %q", got, want)
	}
}

// TestFoldActivityBatchStaysOnTool proves a batch of calls holds the tool phrase until the
// last result lands — one result while another call is still open must not claim the model is
// thinking again.
func TestFoldActivityBatchStaysOnTool(t *testing.T) {
	m := newTestModel(t)
	m = m.foldEvent(domain.ToolCallEvent{Call: domain.ToolCall{ID: "1", Tool: "read_file", Arguments: []byte(`{"path":"a.go"}`)}})
	m = m.foldEvent(domain.ToolCallEvent{Call: domain.ToolCall{ID: "2", Tool: "read_file", Arguments: []byte(`{"path":"b.go"}`)}})

	m = m.foldEvent(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "1", Content: "ok"}})
	if got, want := m.act.text(""), "reading"; got != want {
		t.Errorf("phrase with one call still open = %q, want %q", got, want)
	}

	m = m.foldEvent(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "2", Content: "ok"}})
	if got, want := m.act.text(""), "thinking"; got != want {
		t.Errorf("phrase after the batch drained = %q, want %q", got, want)
	}
}

// TestFoldActivityStoppingIsSticky proves the stop the human asked for stays on screen: the
// worker keeps emitting until it reaches a quiescent boundary, and none of those events may
// overwrite "stopping". Only finishWorker clears it.
func TestFoldActivityStoppingIsSticky(t *testing.T) {
	m := newTestModel(t)
	m.setActivity(actStopping, "", 0, "")

	for _, e := range []domain.Event{
		domain.ReasoningEvent{Text: "still going"},
		domain.TokenEvent{Text: "tail"},
		domain.ToolCallEvent{Call: domain.ToolCall{ID: "1", Tool: "read_file", Arguments: []byte(`{"path":"a.go"}`)}},
		domain.ToolResultEvent{Result: domain.ToolResult{CallID: "1", Content: "ok"}},
		domain.MessageEvent{Text: "done"},
		domain.StreamResetEvent{},
	} {
		m = m.foldEvent(e)
		if got := m.act.text(""); got != "stopping" {
			t.Fatalf("%T overwrote the sticky stop phrase with %q", e, got)
		}
	}

	m.finishWorker(stateIdle)
	if got := m.act.text(""); got != "" {
		t.Errorf("finishWorker left the phrase %q, want the idle empty slot", got)
	}
}

// TestFoldActivityIgnoresObservationalEvents proves the accounting and audit events leave the
// live phrase alone — the status line must not flicker off the work actually in flight.
func TestFoldActivityIgnoresObservationalEvents(t *testing.T) {
	m := newTestModel(t)
	m = m.foldEvent(domain.ToolCallEvent{Call: domain.ToolCall{ID: "1", Tool: "terminal", Arguments: []byte(`{"command":"go test"}`)}})
	want := m.act

	for _, e := range []domain.Event{
		domain.UsageEvent{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		domain.ErrorEvent{Source: "loop", Err: "recovered"},
		domain.AuditEvent{Tool: "terminal", CallID: "1", Decision: "allowed"},
		domain.MechanismFiredEvent{Mechanism: "m", Hook: "h", Action: "a"},
		domain.ApprovalEvent{Request: domain.ApprovalRequest{Tool: "terminal"}, Decision: domain.ApprovalAllow},
	} {
		m = m.foldEvent(e)
		if m.act != want {
			t.Errorf("%T changed the activity: %+v, want %+v", e, m.act, want)
		}
	}
}
