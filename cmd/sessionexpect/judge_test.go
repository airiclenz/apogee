package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
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
const updateFixtures = "SESSIONEXPECT_UPDATE_FIXTURES"

// heroStart is the wall clock of the first prompt's Enter in the fixture.
var heroStart = time.Date(2026, time.September, 16, 10, 0, 0, 0, time.UTC)

// heroEntries is cmd/demorig's hero transcript — a real keeper take, in delivery order — with
// the `Sub-Agent` delegation the hero storyboard's beat 4 expects, sent after the failing test
// run to find out why. As in demorig's: no `/undo` note (the preview is never persisted), no
// toolResult entries (a paired result enriches its toolCall entry, so the `+1 −1` diffstat rides
// the Replace entry itself), and the `interjected` entry immediately AFTER the `Replace task.go`
// toolCall — the interjection is stamped at delivery, at the Step boundary after the fix card
// paints, before the CHANGELOG write it steers.
func heroEntries() []session.Entry {
	at := func(offset time.Duration) time.Time { return heroStart.Add(offset) }
	tool := func(name, label, verb, target, stat string) *session.ToolView {
		return &session.ToolView{Name: name, Label: label, Verb: verb, Target: target, Stat: stat}
	}
	delegation := tool("sub_agent", "Sub-Agent", "delegating", "find why TestPriority fails", "done")
	delegation.Solo = true
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
		{Kind: session.EntryKindToolCall, At: at(5 * time.Second), Done: true, CallID: "call_2",
			Tool: delegation},
		{Kind: session.EntryKindAssistant, At: at(7400 * time.Millisecond), Done: true,
			Text: "TestPriority expects the higher priority first; task.go sorts ascending."},
		{Kind: session.EntryKindToolCall, At: at(8 * time.Second), Done: true, CallID: "call_3",
			Tool: tool("read_file", "Read", "reading", "task.go", "31 lines")},
		{Kind: session.EntryKindToolCall, At: at(12200 * time.Millisecond), Done: true, CallID: "call_4",
			Tool: replaceTask},
		{Kind: session.EntryKindInterjected, At: at(12600 * time.Millisecond),
			Text: "also add a CHANGELOG entry for the fix"},
		{Kind: session.EntryKindToolCall, At: at(15100 * time.Millisecond), Done: true, CallID: "call_5",
			Tool: replaceChangelog},
		{Kind: session.EntryKindToolCall, At: at(16400 * time.Millisecond), Done: true, CallID: "call_6",
			Tool: tool("run_tests", "Tests", "running tests", "", "PASS")},
		{Kind: session.EntryKindAssistant, At: at(19 * time.Second), Done: true,
			Text: "Fixed the sort order in task.go and noted it in CHANGELOG.md; the suite passes."},
	}
}

// recordOf wraps entries in a session record the store accepts (a non-empty id), as the
// indented, newline-terminated JSON the fixture is committed as.
func recordOf(t *testing.T, entries []session.Entry) []byte {
	t.Helper()
	transcript, err := session.EncodeTranscript(entries)
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

// writeRecord saves entries as a session record in a temp dir and returns its path.
func writeRecord(t *testing.T, entries []session.Entry) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.json")
	if err := os.WriteFile(path, recordOf(t, entries), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// TestHeroFixtureIsCurrent pins the committed fixture to heroEntries;
// SESSIONEXPECT_UPDATE_FIXTURES=1 rewrites it.
func TestHeroFixtureIsCurrent(t *testing.T) {
	want := recordOf(t, heroEntries())
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
		{name: "the delegation", selector: EntrySelector{Kind: "toolCall", Tool: "Sub-Agent"}, wantIndex: 3},
		{name: "tool prefix", selector: EntrySelector{Kind: "toolCall", Tool: "Replace"}, wantIndex: 6},
		{name: "tool and target", selector: EntrySelector{Kind: "toolCall", Tool: "Replace", Target: "CHANGE"}, wantIndex: 8},
		{name: "nth", selector: EntrySelector{Kind: "toolCall", Tool: "Tests", Nth: 2}, wantIndex: 9},
		{name: "last", selector: EntrySelector{Kind: "toolCall", Nth: NthLast}, wantIndex: 9},
		{name: "text prefix", selector: EntrySelector{Kind: "assistant", Text: "Fixed"}, wantIndex: 10},
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

// fixtureEntries loads the committed hero fixture through loadEntries.
func fixtureEntries(t *testing.T) []session.Entry {
	t.Helper()
	entries, err := loadEntries(heroFixture)
	if err != nil {
		t.Fatalf("loadEntries(%s): %v", heroFixture, err)
	}
	return entries
}

// testsRun selects the first Tests card; testsLast the last.
var (
	testsRun  = EntrySelector{Kind: "toolCall", Tool: "Tests"}
	testsLast = EntrySelector{Kind: "toolCall", Tool: "Tests", Nth: NthLast}
)

func TestJudge_Order(t *testing.T) {
	t.Parallel()
	interjected := EntrySelector{Kind: "interjected"}
	git := EntrySelector{Kind: "toolCall", Tool: "Git"}
	pastTheMatches := EntrySelector{Kind: "toolCall", Tool: "Tests", Nth: 3}
	cases := []struct {
		name   string
		expect Expect
		pass   bool
		detail string
	}{
		{name: "after the other entry", expect: Expect{Entry: interjected, After: &testsRun},
			pass: true, detail: `entry 7 interjected "also add a CHANGELOG entry for the fix": after {kind: toolCall, tool: Tests} (entry 2)`},
		{name: "out of order", expect: Expect{Entry: interjected, Before: &testsRun},
			pass: false, detail: "is not before {kind: toolCall, tool: Tests} (entry 2)"},
		{name: "before and after", expect: Expect{Entry: interjected, Before: &testsLast, After: &testsRun},
			pass: true, detail: "before {kind: toolCall, tool: Tests, nth: last} (entry 9), after {kind: toolCall, tool: Tests} (entry 2)"},
		{name: "the other selector locates nothing", expect: Expect{Entry: interjected, After: &git},
			pass: false, detail: "after: no entry matches {kind: toolCall, tool: Git}"},
		{name: "the other selector's nth is past its matches", expect: Expect{Entry: interjected, Before: &pastTheMatches},
			pass: false, detail: "before: {kind: toolCall, tool: Tests, nth: 3}: only 2 match(es), nth 3 wanted"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			pass, detail := tc.expect.judge(fixtureEntries(t))

			if pass != tc.pass || !strings.Contains(detail, tc.detail) {
				t.Errorf("want pass=%v with detail containing %q, got pass=%v %q", tc.pass, tc.detail, pass, detail)
			}
		})
	}
}

func TestJudge_ContainsAndLocation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		expect Expect
		pass   bool
		detail string
	}{
		{name: "tool stat", expect: Expect{Entry: testsRun, Contains: "FAIL"},
			pass: true, detail: `entry 2 toolCall Tests FAIL: contains "FAIL"`},
		{name: "tool label", expect: Expect{Entry: EntrySelector{Kind: "toolCall", Tool: "Sub-Agent"}, Contains: "Sub-Agent"},
			pass: true, detail: `entry 3 toolCall Sub-Agent find why TestPriority fails done: contains "Sub-Agent"`},
		{name: "text", expect: Expect{Entry: EntrySelector{Kind: "assistant", Nth: NthLast}, Contains: "suite passes"},
			pass: true, detail: `contains "suite passes"`},
		{name: "missing", expect: Expect{Entry: testsRun, Contains: "PASS"},
			pass: false, detail: `entry 2 toolCall Tests FAIL: does not contain "PASS"`},
		{name: "unlocated", expect: Expect{Entry: EntrySelector{Kind: "toolCall", Tool: "Git"}, Contains: "x"},
			pass: false, detail: "no entry matches {kind: toolCall, tool: Git}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			pass, detail := tc.expect.judge(fixtureEntries(t))

			if pass != tc.pass || !strings.Contains(detail, tc.detail) {
				t.Errorf("want pass=%v with detail containing %q, got pass=%v %q", tc.pass, tc.detail, pass, detail)
			}
		})
	}
}

// heroExpect is one entry expect of graphics/demo/storyboards/hero.yaml, as sessionexpect is
// given it.
type heroExpect struct {
	beat   string
	expect Expect
}

// heroExpects are hero.yaml's entry expects: beats 4, 5, 8 and both of beat 10's, beat 10's
// `after: 5` spelled as beat 5's selector.
func heroExpects() []heroExpect {
	interjected := EntrySelector{Kind: "interjected"}
	return []heroExpect{
		{beat: "4", expect: Expect{Entry: EntrySelector{Kind: "toolCall", Tool: "Sub-Agent"}, Contains: "Sub-Agent"}},
		{beat: "5", expect: Expect{Entry: interjected, Contains: "CHANGELOG"}},
		{beat: "8", expect: Expect{Entry: EntrySelector{Kind: "toolCall", Tool: "Replace", Target: "task.go"}, Contains: "+1 −1"}},
		{beat: "10 order", expect: Expect{Entry: EntrySelector{Kind: "toolCall", Target: "CHANGELOG.md"}, After: &interjected}},
		{beat: "10 verdict", expect: Expect{Entry: testsLast, Contains: "PASS"}},
	}
}

// mutatedHeroEntries is heroEntries with each hero expect's evidence broken once: the delegation
// relabelled, the queued message reworded, the fix's diffstat changed, the CHANGELOG write moved
// ahead of the queued message, and the closing test run left red.
func mutatedHeroEntries() []session.Entry {
	entries := heroEntries()
	entries[3].Tool.Label = "Agent"
	entries[7].Text = "also bump the version"
	entries[6].Tool.Stat = "+2 −1"
	entries[7], entries[8] = entries[8], entries[7]
	entries[9].Tool.Stat = "FAIL"
	return entries
}

func TestJudge_HeroExpects(t *testing.T) {
	t.Parallel()
	for _, hero := range heroExpects() {
		t.Run("beat "+hero.beat, func(t *testing.T) {
			t.Parallel()

			pass, detail := hero.expect.judge(fixtureEntries(t))
			mutatedEntries, err := loadEntries(writeRecord(t, mutatedHeroEntries()))
			if err != nil {
				t.Fatalf("loadEntries(mutated): %v", err)
			}
			mutatedPass, mutatedDetail := hero.expect.judge(mutatedEntries)

			if !pass {
				t.Errorf("fixture: want a pass, got a fail: %s", detail)
			}
			if mutatedPass {
				t.Errorf("mutated: want a fail, got a pass: %s", mutatedDetail)
			}
		})
	}
}

func TestLoadEntries_Refusals(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		path    func(t *testing.T) string
		wantErr string
	}{
		{name: "no transcript entries", path: func(t *testing.T) string { return writeRecord(t, nil) },
			wantErr: "the session recorded no transcript entries"},
		{name: "no such file", path: func(t *testing.T) string { return filepath.Join(t.TempDir(), "absent.json") },
			wantErr: "absent.json"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := loadEntries(tc.path(t))

			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("want error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

// summaryShapedTranscript carries a Replace and a Tests entry exactly as apogee saves them: the
// outcome rides in tool.summary.text and the stat is left empty.
const summaryShapedTranscript = `{"version": 1, "entries": [
	{"kind": "toolCall", "callID": "call_1", "done": true, "tool": {"label": "Replace", "verb": "editing",
		"target": "task.go", "name": "single_find_and_replace",
		"summary": {"text": "+1 −1", "stat": {"added": 1, "removed": 1}}}},
	{"kind": "toolCall", "callID": "call_2", "done": true, "tool": {"label": "Tests",
		"verb": "running tests", "name": "run_tests", "summary": {"text": "PASS"}}}
]}`

func TestEntryContains_ReadsTheToolSummary(t *testing.T) {
	t.Parallel()
	entries, err := session.DecodeTranscript([]byte(summaryShapedTranscript))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	cases := []struct {
		name  string
		entry session.Entry
		want  string
		holds bool
	}{
		{name: "edit diffstat", entry: entries[0], want: "+1 −1", holds: true},
		{name: "tests verdict", entry: entries[1], want: "PASS", holds: true},
		{name: "absent", entry: entries[1], want: "FAIL", holds: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := entryContains(tc.entry, tc.want); got != tc.holds {
				t.Errorf("entryContains(%q) = %v, want %v", tc.want, got, tc.holds)
			}
		})
	}
}

func TestHeadOf(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"short":                   "short",
		"first line\nsecond line": "first line…",
		strings.Repeat("é", 50):   strings.Repeat("é", textHead) + "…",
	}
	for text, want := range cases {
		if got := headOf(text); got != want {
			t.Errorf("headOf(%q): want %q, got %q", text, want, got)
		}
	}
}
