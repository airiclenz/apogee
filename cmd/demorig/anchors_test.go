package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

// fixedPainter is the test FirstPainter: a fixed first paint, or an error, and a call count.
type fixedPainter struct {
	at    time.Duration
	err   error
	calls int
}

func (p *fixedPainter) FirstPaint(context.Context) (time.Duration, error) {
	p.calls++
	return p.at, p.err
}

// heroBeats resolves the shipped hero storyboard against the fixture.
func heroBeats(t *testing.T, painter FirstPainter, duration time.Duration) (*Storyboard, []session.Entry, map[int]BeatTime) {
	t.Helper()
	board, err := Load(heroStoryboard)
	if err != nil {
		t.Fatalf("Load(%s): %v", heroStoryboard, err)
	}
	entries, err := loadEntries(heroFixture)
	if err != nil {
		t.Fatalf("loadEntries(%s): %v", heroFixture, err)
	}
	times, err := resolveBeats(context.Background(), board, entries, painter, duration)
	if err != nil {
		t.Fatalf("resolveBeats: %v", err)
	}
	if len(times) != len(board.Beats) {
		t.Fatalf("resolveBeats: want %d beats, got %d", len(board.Beats), len(times))
	}
	byID := make(map[int]BeatTime, len(times))
	for index, beat := range times {
		if beat.ID != board.Beats[index].ID {
			t.Errorf("beat order: position %d: want id %d, got %d", index, board.Beats[index].ID, beat.ID)
		}
		byID[beat.ID] = beat
	}
	return board, entries, byID
}

func TestResolveBeatsHero(t *testing.T) {
	t.Parallel()
	const firstPaint, duration = 3200 * time.Millisecond, 60 * time.Second

	board, _, beats := heroBeats(t, &fixedPainter{at: firstPaint}, duration)

	pin := board.Align.FirstPromptAt + board.PaintLag
	want := map[int]time.Duration{
		1: firstPaint,
		2: pin,
		3: pin + 2900*time.Millisecond,
		4: pin + 10300*time.Millisecond,
		5: pin + 12200*time.Millisecond,
		6: pin + 16400*time.Millisecond,
		7: duration - 6500*time.Millisecond,
		8: duration,
	}
	for id, at := range want {
		if got := beats[id].At; got != at {
			t.Errorf("beat %d: want %s, got %s", id, at, got)
		}
	}
	for id, index := range map[int]int{1: noEntry, 2: 0, 3: 2, 5: 5, 6: 8, 7: noEntry, 8: noEntry, 4: noEntry} {
		if got := beats[id].Index; got != index {
			t.Errorf("beat %d: want entry index %d, got %d", id, index, got)
		}
	}
}

// TestResolveBeatsHero_InterjectionOrder pins the delivery order the fixture mirrors: beat 4's
// expect `before: 6` passes and `before: 5` would fail.
func TestResolveBeatsHero_InterjectionOrder(t *testing.T) {
	t.Parallel()

	board, entries, beats := heroBeats(t, &fixedPainter{at: 3 * time.Second}, time.Minute)

	var beat4 Beat
	for _, beat := range board.Beats {
		if beat.ID == 4 {
			beat4 = beat
		}
	}
	interjected, err := findEntry(entries, *beat4.Expect[0].Entry)
	if err != nil {
		t.Fatalf("findEntry(%s): %v", beat4.Expect[0].Entry, err)
	}
	if !(beats[5].Index < interjected.Index && interjected.Index < beats[6].Index) {
		t.Errorf("interjected entry index %d: want between beat 5 (%d) and beat 6 (%d)",
			interjected.Index, beats[5].Index, beats[6].Index)
	}
}

// TestResolveBeatsHero_FirstPaintServesOnlyItsAnchor moves the first paint and checks that only
// beat 1 follows it: the pin is the first prompt, never the paint.
func TestResolveBeatsHero_FirstPaintServesOnlyItsAnchor(t *testing.T) {
	t.Parallel()

	_, _, early := heroBeats(t, &fixedPainter{at: 2 * time.Second}, time.Minute)
	_, _, late := heroBeats(t, &fixedPainter{at: 5 * time.Second}, time.Minute)

	if early[1].At == late[1].At {
		t.Errorf("beat 1: want to follow the first paint, got %s both times", early[1].At)
	}
	for id := 2; id <= 8; id++ {
		if early[id].At != late[id].At {
			t.Errorf("beat %d: moved with the first paint (%s vs %s)", id, early[id].At, late[id].At)
		}
	}
}

func TestFindEntry(t *testing.T) {
	t.Parallel()
	entries := heroEntries()
	cases := []struct {
		name      string
		anchor    Anchor
		wantIndex int
		wantErr   string
	}{
		{name: "first of kind", anchor: Anchor{Kind: "toolCall"}, wantIndex: 2},
		{name: "tool prefix", anchor: Anchor{Kind: "toolCall", Tool: "Replace"}, wantIndex: 5},
		{name: "tool and target", anchor: Anchor{Kind: "toolCall", Tool: "Replace", Target: "CHANGE"}, wantIndex: 7},
		{name: "nth", anchor: Anchor{Kind: "toolCall", Tool: "Tests", Nth: 2}, wantIndex: 8},
		{name: "last", anchor: Anchor{Kind: "toolCall", Nth: NthLast}, wantIndex: 8},
		{name: "text prefix", anchor: Anchor{Kind: "assistant", Text: "Fixed"}, wantIndex: 9},
		{name: "no match", anchor: Anchor{Kind: "toolCall", Tool: "Git"}, wantErr: "no entry matches {kind: toolCall, tool: Git}"},
		{name: "nth past the matches", anchor: Anchor{Kind: "toolCall", Tool: "Tests", Nth: 3}, wantErr: "only 2 match(es), nth 3 wanted"},
		{name: "tool selector on a textual kind", anchor: Anchor{Kind: "assistant", Tool: "Tests"}, wantErr: "no entry matches"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := findEntry(entries, tc.anchor)

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

// testBoard is a minimal storyboard for resolution tests that bypass Load.
func testBoard(beats ...Beat) *Storyboard {
	return &Storyboard{
		Align:    Align{FirstPromptAt: 10 * time.Second},
		PaintLag: 300 * time.Millisecond,
		Beats:    beats,
	}
}

func TestResolveBeats_OffsetOnSessionAnchor(t *testing.T) {
	t.Parallel()
	board := testBoard(Beat{ID: 1, Anchor: Anchor{Kind: "user", Offset: -2 * time.Second}})

	times, err := resolveBeats(context.Background(), board, heroEntries(), &fixedPainter{}, time.Minute)

	if err != nil {
		t.Fatalf("resolveBeats: %v", err)
	}
	if want := 8300 * time.Millisecond; times[0].At != want {
		t.Errorf("want %s (pin 10.3s − 2s), got %s", want, times[0].At)
	}
}

func TestResolveBeats_BeatAnchorFollowsResolvedTime(t *testing.T) {
	t.Parallel()
	board := testBoard(
		Beat{ID: 1, Anchor: Anchor{Kind: "user", Offset: time.Second}},
		Beat{ID: 2, Anchor: Anchor{Beat: 1, Offset: 500 * time.Millisecond}},
	)

	times, err := resolveBeats(context.Background(), board, heroEntries(), &fixedPainter{}, time.Minute)

	if err != nil {
		t.Fatalf("resolveBeats: %v", err)
	}
	if want := times[0].At + 500*time.Millisecond; times[1].At != want {
		t.Errorf("beat 2: want beat 1's offset time + 500ms = %s, got %s", want, times[1].At)
	}
}

func TestResolveBeats_UnresolvedBeatReference(t *testing.T) {
	t.Parallel()
	board := testBoard(Beat{ID: 3, Anchor: Anchor{Beat: 2}})

	_, err := resolveBeats(context.Background(), board, heroEntries(), &fixedPainter{}, time.Minute)

	if err == nil || !strings.Contains(err.Error(), "beat 3: {beat: 2}: beat 2 is not resolved") {
		t.Fatalf("want an unresolved-beat error, got %v", err)
	}
}

func TestResolveBeats_UnmatchedAnchorNamesTheBeat(t *testing.T) {
	t.Parallel()
	board := testBoard(Beat{ID: 7, Anchor: Anchor{Kind: "note", Text: "reverted"}})

	_, err := resolveBeats(context.Background(), board, heroEntries(), &fixedPainter{}, time.Minute)

	if err == nil || !strings.HasPrefix(err.Error(), "beat 7: no entry matches") {
		t.Fatalf("want an error naming beat 7, got %v", err)
	}
}

func TestResolveBeats_PainterRunsOnlyForFirstPaint(t *testing.T) {
	t.Parallel()
	painter := &fixedPainter{err: errors.New("ffmpeg missing")}
	board := testBoard(Beat{ID: 1, Anchor: Anchor{Kind: "user"}}, Beat{ID: 2, Anchor: Anchor{Video: VideoEnd}})

	times, err := resolveBeats(context.Background(), board, heroEntries(), painter, 42*time.Second)

	if err != nil {
		t.Fatalf("resolveBeats without a first-paint anchor: %v", err)
	}
	if painter.calls != 0 {
		t.Errorf("painter ran %d time(s) for a storyboard with no first-paint anchor", painter.calls)
	}
	if times[1].At != 42*time.Second {
		t.Errorf("end: want 42s, got %s", times[1].At)
	}
}

func TestResolveBeats_PainterErrorNamesTheBeat(t *testing.T) {
	t.Parallel()
	painter := &fixedPainter{err: errors.New("no scene change")}
	board := testBoard(Beat{ID: 1, Anchor: Anchor{Video: VideoFirstPaint}})

	_, err := resolveBeats(context.Background(), board, heroEntries(), painter, time.Minute)

	if err == nil || !strings.Contains(err.Error(), "beat 1: {video: first-paint}: no scene change") {
		t.Fatalf("want the painter's error under beat 1, got %v", err)
	}
}

func TestAnchorString(t *testing.T) {
	t.Parallel()
	cases := map[string]Anchor{
		"{kind: toolCall, tool: Replace, target: task.go}": {Kind: "toolCall", Tool: "Replace", Target: "task.go"},
		"{kind: toolCall, tool: Tests, nth: last}":         {Kind: "toolCall", Tool: "Tests", Nth: NthLast},
		"{kind: user, nth: 2}":                             {Kind: "user", Nth: 2},
		"{video: end, offset: -6.5s}":                      {Video: VideoEnd, Offset: -6500 * time.Millisecond},
		"{beat: 2, offset: 10.3s}":                         {Beat: 2, Offset: 10300 * time.Millisecond},
	}
	for want, anchor := range cases {
		if got := anchor.String(); got != want {
			t.Errorf("want %s, got %s", want, got)
		}
	}
}
