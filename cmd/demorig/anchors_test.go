package main

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/session"
)

// heroFixture is the committed session record of a keeper hero take, built from heroEntries by
// TestHeroFixtureIsCurrent through internal/session's own encoder.
const heroFixture = "testdata/session-hero.json"

// updateFixtures names the environment variable that lets TestHeroFixtureIsCurrent rewrite the
// fixture instead of comparing against it.
const updateFixtures = "DEMORIG_UPDATE_FIXTURES"

// heroStart is the wall clock of the first prompt's Enter in the fixture: the alignment pin.
var heroStart = time.Date(2026, time.September, 16, 10, 0, 0, 0, time.UTC)

// heroEntries mirrors the transcript of a REAL keeper take of hero.tape, in delivery order:
// no `/undo` note (the preview is never persisted), no toolResult entries (a paired result
// enriches its toolCall entry, so the `+1 −1` diffstat rides the Replace entry itself), and the
// `interjected` entry immediately AFTER the `Replace task.go` toolCall — the interjection is
// stamped at delivery, which lands at the Step boundary after the fix card paints, before the
// CHANGELOG write it steers.
func heroEntries() []session.Entry {
	at := func(offset time.Duration) time.Time { return heroStart.Add(offset) }
	tool := func(name, label, verb, target, stat string) *session.ToolView {
		return &session.ToolView{Name: name, Label: label, Verb: verb, Target: target, Stat: stat}
	}
	replaceTask := tool("single_find_and_replace", "Replace", "editing", "task.go", "+1 −1")
	replaceTask.StatValue = &session.StatValue{Added: 1, Removed: 1}
	replaceChangelog := tool("single_find_and_replace", "Replace", "editing", "CHANGELOG.md", "+3 −0")
	replaceChangelog.StatValue = &session.StatValue{Added: 3}
	return []session.Entry{
		{Kind: session.EntryKindUser, At: at(0),
			Text: "the test suite is failing - find the bug, fix it, and prove the tests pass"},
		{Kind: session.EntryKindAssistant, At: at(2100 * time.Millisecond), Done: true,
			Text: "I'll run the tests first to see what fails."},
		{Kind: session.EntryKindToolCall, At: at(2900 * time.Millisecond), Done: true, CallID: "call_1",
			Tool: tool("run_tests", "Tests", "running tests", "", "FAIL")},
		{Kind: session.EntryKindAssistant, At: at(7400 * time.Millisecond), Done: true,
			Text: "TestPriority expects the higher priority first; task.go sorts ascending."},
		{Kind: session.EntryKindToolCall, At: at(8 * time.Second), Done: true, CallID: "call_2",
			Tool: tool("read_file", "Read", "reading", "task.go", "31 lines")},
		{Kind: session.EntryKindToolCall, At: at(12200 * time.Millisecond), Done: true, CallID: "call_3",
			Tool: replaceTask},
		{Kind: session.EntryKindInterjected, At: at(12600 * time.Millisecond),
			Text: "also add a CHANGELOG entry for the fix"},
		{Kind: session.EntryKindToolCall, At: at(15100 * time.Millisecond), Done: true, CallID: "call_4",
			Tool: replaceChangelog},
		{Kind: session.EntryKindToolCall, At: at(16400 * time.Millisecond), Done: true, CallID: "call_5",
			Tool: tool("run_tests", "Tests", "running tests", "", "PASS")},
		{Kind: session.EntryKindAssistant, At: at(19 * time.Second), Done: true,
			Text: "Fixed the sort order in task.go and noted it in CHANGELOG.md; the suite passes."},
	}
}

// heroRecord wraps heroEntries in a session record the store accepts (a non-empty id).
func heroRecord(t *testing.T) []byte {
	t.Helper()
	transcript, err := session.EncodeTranscript(heroEntries())
	if err != nil {
		t.Fatalf("EncodeTranscript: %v", err)
	}
	record := session.Record{
		RecordVersion: session.RecordVersion,
		Meta: session.Meta{
			ID:        "20260916T100000Z-demohero",
			Title:     "the test suite is failing - find the bug, fix it, and prove the tests pass",
			CreatedAt: heroStart.Add(2 * time.Second),
			UpdatedAt: heroStart.Add(19 * time.Second),
			Workspace: "/Users/demo/Repos/taskman",
			Model:     "~deepseek/deepseek-v4-flash-latest",
			UserMsgs:  2,
		},
		Transcript: transcript,
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		t.Fatalf("marshal record: %v", err)
	}
	return append(data, '\n')
}

// TestHeroFixtureIsCurrent pins the committed fixture to heroEntries; DEMORIG_UPDATE_FIXTURES=1
// rewrites it.
func TestHeroFixtureIsCurrent(t *testing.T) {
	want := heroRecord(t)
	if os.Getenv(updateFixtures) != "" {
		if err := os.WriteFile(heroFixture, want, 0o644); err != nil {
			t.Fatalf("write %s: %v", heroFixture, err)
		}
	}

	got, err := os.ReadFile(heroFixture)

	if err != nil {
		t.Fatalf("read %s: %v (set %s=1 to write it)", heroFixture, err, updateFixtures)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s is stale against heroEntries; set %s=1 to rewrite it", heroFixture, updateFixtures)
	}
}

func TestFindEntry(t *testing.T) {
	t.Parallel()
	entries := heroEntries()
	cases := []struct {
		name      string
		selector  EntrySelector
		wantIndex int
		wantErr   string
	}{
		{name: "first of kind", selector: EntrySelector{Kind: "toolCall"}, wantIndex: 2},
		{name: "tool prefix", selector: EntrySelector{Kind: "toolCall", Tool: "Replace"}, wantIndex: 5},
		{name: "tool and target", selector: EntrySelector{Kind: "toolCall", Tool: "Replace", Target: "CHANGE"}, wantIndex: 7},
		{name: "nth", selector: EntrySelector{Kind: "toolCall", Tool: "Tests", Nth: 2}, wantIndex: 8},
		{name: "last", selector: EntrySelector{Kind: "toolCall", Nth: NthLast}, wantIndex: 8},
		{name: "text prefix", selector: EntrySelector{Kind: "assistant", Text: "Fixed"}, wantIndex: 9},
		{name: "no match", selector: EntrySelector{Kind: "toolCall", Tool: "Git"}, wantErr: "no entry matches {kind: toolCall, tool: Git}"},
		{name: "nth past the matches", selector: EntrySelector{Kind: "toolCall", Tool: "Tests", Nth: 3}, wantErr: "only 2 match(es), nth 3 wanted"},
		{name: "tool selector on a textual kind", selector: EntrySelector{Kind: "assistant", Tool: "Tests"}, wantErr: "no entry matches"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := findEntry(entries, tc.selector)

			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("want error containing %q, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("findEntry: %v", err)
			}
			if got.Index != tc.wantIndex {
				t.Errorf("want index %d, got %d", tc.wantIndex, got.Index)
			}
		})
	}
}

func TestEntrySelectorString(t *testing.T) {
	t.Parallel()
	cases := map[string]EntrySelector{
		"{kind: toolCall, tool: Replace, target: task.go}": {Kind: "toolCall", Tool: "Replace", Target: "task.go"},
		"{kind: toolCall, tool: Tests, nth: last}":         {Kind: "toolCall", Tool: "Tests", Nth: NthLast},
		"{kind: user, nth: 2}":                             {Kind: "user", Nth: 2},
	}
	for want, selector := range cases {
		if got := selector.String(); got != want {
			t.Errorf("want %s, got %s", want, got)
		}
	}
}
