package mechanisms

import (
	"reflect"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/domain/domaintest"
)

// The shared history-scan shapes (historyscan.go, item 7 / D5) get their own table-driven suite,
// built through the hook-seam test adapter (domaintest, D6) and exercised with the F8 family
// spellings the call sites pass — the migrated Mechanisms' suites stay the behaviour contract for
// the composed decisions; these tables pin the scan mechanics themselves.

// scanView wraps a built conversation fixture in the ConversationView the scan helpers read.
func scanView(msgs []domain.Message) domain.ConversationView {
	return domaintest.FakeLoopView{Messages: msgs}.Conversation()
}

// readAttemptCounts separates successes from failures, keys them per its documented asymmetry
// (failures literal, successes normalized), treats a result-less call as not-yet-failed, and lets a
// write in the write family cancel a success — across the read family's spellings.
func TestReadAttemptCounts(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name          string
		msgs          []domain.Message
		wantSuccesses map[string]int
		wantFailures  map[string]int
	}{
		{
			name: "mixed success and error results across family spellings",
			msgs: domaintest.NewConversation().
				User("fix the bug").
				AssistantCalls(
					domaintest.ReadCall("r1", "a.go"),
					domaintest.Call("r2", "readFile", map[string]string{"path": "gone.go"}),
					domaintest.Call("r3", "open_file", map[string]string{"path": "b.go"}),
				).
				ToolResult("r1", "package a").
				ToolResult("r2", "Error: no such file").
				ToolResult("r3", "package b").
				Messages(),
			wantSuccesses: map[string]int{"a.go": 1, "b.go": 1},
			wantFailures:  map[string]int{"gone.go": 1},
		},
		{
			name: "a call without a committed result is not yet a failure",
			msgs: domaintest.NewConversation().
				User("read it").
				AssistantCalls(domaintest.ReadCall("r1", "a.go")).
				Messages(),
			wantSuccesses: map[string]int{"a.go": 1},
			wantFailures:  map[string]int{},
		},
		{
			name: "a write decrements the success count but never below zero",
			msgs: domaintest.NewConversation().
				User("edit a.go").
				AssistantCalls(domaintest.Call("w0", "edit_existing_file", map[string]string{"path": "b.go"})).
				AssistantCalls(domaintest.ReadCall("r1", "a.go")).
				ToolResult("r1", "package a").
				AssistantCalls(domaintest.Call("w1", "write_file", map[string]string{"path": "a.go"})).
				Messages(),
			// The decremented entry stays at 0 (the original scan's residue — callers'
			// thresholds filter it); the un-read b.go write never lands at all.
			wantSuccesses: map[string]int{"a.go": 0},
			wantFailures:  map[string]int{},
		},
		{
			name: "failures key by the literal spelling, successes by the normalized path",
			msgs: domaintest.NewConversation().
				User("check").
				AssistantCalls(
					domaintest.ReadCall("r1", "./gone.go"),
					domaintest.ReadCall("r2", "gone.go"),
					domaintest.ReadCall("r3", "./a.go"),
				).
				ToolResult("r1", "file not found").
				ToolResult("r2", "file not found").
				ToolResult("r3", "package a").
				AssistantCalls(domaintest.Call("w1", "editFile", map[string]string{"path": "a.go"})).
				Messages(),
			// The "./a.go" read and the "a.go" write meet on the normalized key (the write
			// cancels the read, leaving the 0 residue); the two failure spellings stay split.
			wantSuccesses: map[string]int{"a.go": 0},
			wantFailures:  map[string]int{"./gone.go": 1, "gone.go": 1},
		},
		{
			name: "non-read non-write calls and pathless calls are ignored",
			msgs: domaintest.NewConversation().
				User("search").
				AssistantCalls(
					domaintest.Call("g1", "grep", map[string]string{"path": "a.go", "pattern": "x"}),
					domaintest.Call("r1", "read_file", map[string]string{"pattern": "no path key"}),
				).
				ToolResult("g1", "error: no matches").
				Messages(),
			wantSuccesses: map[string]int{},
			wantFailures:  map[string]int{},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			successes, failures := readAttemptCounts(scanView(c.msgs), readToolNames, wave4WriteTools)
			if !reflect.DeepEqual(successes, c.wantSuccesses) {
				t.Errorf("successes = %v, want %v", successes, c.wantSuccesses)
			}
			if !reflect.DeepEqual(failures, c.wantFailures) {
				t.Errorf("failures = %v, want %v", failures, c.wantFailures)
			}
		})
	}
}

// recentSuccessfulReadPaths scopes its structural window to the latest assistant message where a
// successful unshadowed read lands, walks past turns that yield none, and shadows reads with
// same-turn or later-turn writes (C-02).
func TestRecentSuccessfulReadPaths(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		msgs []domain.Message
		want map[string]bool
	}{
		{
			name: "only the latest read episode counts",
			msgs: domaintest.NewConversation().
				User("go").
				AssistantCalls(domaintest.ReadCall("r1", "a.go")).
				ToolResult("r1", "package a").
				AssistantCalls(domaintest.Call("r2", "open_file", map[string]string{"path": "./b.go"})).
				ToolResult("r2", "package b").
				Messages(),
			want: map[string]bool{"b.go": true},
		},
		{
			name: "a turn with only failed reads is walked past to the earlier episode",
			msgs: domaintest.NewConversation().
				User("go").
				AssistantCalls(domaintest.ReadCall("r1", "a.go")).
				ToolResult("r1", "package a").
				AssistantCalls(domaintest.ReadCall("r2", "gone.go")).
				ToolResult("r2", "Error: does not exist").
				Messages(),
			want: map[string]bool{"a.go": true},
		},
		{
			name: "a same-turn write shadows the read regardless of call order (C-02)",
			msgs: domaintest.NewConversation().
				User("go").
				AssistantCalls(
					domaintest.ReadCall("r1", "a.go"),
					domaintest.Call("w1", "write_file", map[string]string{"path": "./a.go"}),
				).
				ToolResult("r1", "package a").
				Messages(),
			want: map[string]bool{},
		},
		{
			name: "a later-turn write shadows an earlier read of the same path",
			msgs: domaintest.NewConversation().
				User("go").
				AssistantCalls(domaintest.ReadCall("r1", "a.go")).
				ToolResult("r1", "package a").
				AssistantCalls(domaintest.Call("w1", "multi_find_and_replace", map[string]string{"path": "a.go"})).
				Messages(),
			want: map[string]bool{},
		},
		{
			name: "no reads anywhere yields an empty set",
			msgs: domaintest.NewConversation().
				User("go").
				AssistantText("thinking...").
				Messages(),
			want: map[string]bool{},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := recentSuccessfulReadPaths(scanView(c.msgs), readToolNames, wave4WriteTools)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("recentSuccessfulReadPaths = %v, want %v", got, c.want)
			}
		})
	}
}

// writtenPathsSince collects normalized write targets from the start index onward — start 0 (and a
// clamped negative) covers the whole conversation, a mid-conversation start drops earlier writes,
// and pathless or out-of-family calls never land.
func TestWrittenPathsSince(t *testing.T) {
	t.Parallel()
	// Indices: 0 user, 1 assistant write ./a.go, 2 result, 3 assistant write b.go + pathless
	// create_file + a read, 4 result.
	msgs := domaintest.NewConversation().
		User("build it").
		AssistantCalls(domaintest.Call("w1", "write_file", map[string]string{"path": "./a.go"})).
		ToolResult("w1", "ok").
		AssistantCalls(
			domaintest.Call("w2", "edit_existing_file", map[string]string{"path": "b.go"}),
			domaintest.Call("w3", "create_file", map[string]string{"content": "no path"}),
			domaintest.ReadCall("r1", "c.go"),
		).
		ToolResult("w2", "ok").
		Messages()
	view := scanView(msgs)

	cases := []struct {
		name  string
		start int
		want  map[string]bool
	}{
		{"start 0 scans the whole conversation", 0, map[string]bool{"a.go": true, "b.go": true}},
		{"a negative start clamps to 0", -3, map[string]bool{"a.go": true, "b.go": true}},
		{"a mid-conversation start drops earlier writes", 2, map[string]bool{"b.go": true}},
		{"a start at the end yields an empty set", len(msgs), map[string]bool{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := writtenPathsSince(view, wave4WriteTools, c.start)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("writtenPathsSince(start=%d) = %v, want %v", c.start, got, c.want)
			}
		})
	}
}
