package tui

// The start-up session log (layout.md, "The start-up box"; docs/layout/tool-layout.md): the routine
// notes a session starts with gather into one block folded to its counted header, while a notice
// that needs the human keeps its own line. These tests pin what joins the log and what does not,
// when it opens and closes, that it is display-only and escape-stripped, how it paints in both
// states, and that its fold is its own — never the shared task-list one.

import (
	"bytes"
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/scheme"
)

// sessionLogIndex is the index of m's session-log entry, or -1 when none was drawn.
func sessionLogIndex(m Model) int {
	return m.transcript.sessionLogIndex()
}

// sessionLogNotes is the notes m's session log gathered, in arrival order — nil when none was drawn.
func sessionLogNotes(m Model) []string {
	at := sessionLogIndex(m)
	if at < 0 {
		return nil
	}
	return m.transcript.entries[at].notes
}

// sessionLogHeaders is every `✦ Session log` header on the frame m last painted, glyph included.
func sessionLogHeaders(m Model) []string {
	var headers []string
	for _, ln := range m.lines {
		if plain := strings.TrimSpace(strip(ln)); strings.HasPrefix(plain, "✦ "+sessionLogLabel) {
			headers = append(headers, plain)
		}
	}
	return headers
}

// A routine note joins the log while a notice that needs the human — an unreadable context file, a
// loud startup notice — keeps its own note line beside it, never folded away.
func TestSessionLogLeavesAnAnomalyOnItsOwnLine(t *testing.T) {
	t.Parallel()

	const unreadable = "context: BROKEN.md unreadable — permission denied"
	eng := &fakeEngine{contextReport: domain.ContextFilesReport{Files: []domain.ContextFileNote{
		{Name: "AGENTS.md", Bytes: 3174},
		{Name: "BROKEN.md", Err: "permission denied"},
	}}}
	m := newModel(context.Background(), eng, Options{
		StartupNotices: []Notice{{Text: "apogee: WARNING — auto mode is running UNCONFINED.", Loud: true}},
		UI:             testUIPrefs,
	}, nil)

	if got, want := sessionLogNotes(m), []string{"context: AGENTS.md (3.1 KiB)"}; !slices.Equal(got, want) {
		t.Errorf("session log notes = %q, want only the routine loaded line %q", got, want)
	}
	for _, loud := range []string{unreadable, "apogee: WARNING — auto mode is running UNCONFINED."} {
		if !hasEntry(m, entryNote, loud) {
			t.Errorf("the notice %q is not its own note line; a notice that needs the human is never folded", loud)
		}
		if slices.Contains(sessionLogNotes(m), loud) {
			t.Errorf("the notice %q was gathered into the session log", loud)
		}
	}
}

// A start with no routine note paints no log at all — never an empty `(0 notes)` header — even
// when loud notices were placed.
func TestSessionLogAbsentWithoutRoutineNotes(t *testing.T) {
	t.Parallel()

	m := newModel(context.Background(), &fakeEngine{}, Options{
		ColorSchemeWarnings: []string{`color-scheme "mine.yaml": unknown — using default`},
		StartupNotices:      []Notice{{Text: "apogee: degraded", Loud: true}},
		UI:                  testUIPrefs,
	}, nil)
	m.refreshViewport()

	if at := sessionLogIndex(m); at >= 0 {
		t.Errorf("a start with no routine note drew a session log at entry %d", at)
	}
	if got := sessionLogHeaders(m); len(got) != 0 {
		t.Errorf("the frame paints session-log headers %q, want none", got)
	}
}

// The log is display-only: every note in it is re-derived at the next boundary, so the session
// record never carries the block or anything it gathered.
func TestSessionLogNeverEncoded(t *testing.T) {
	t.Parallel()

	m := newTestModelEng(t, &fakeEngine{contextReport: loadedReport()}, testOpts)
	m.transcript.addUser("what is the capital of france", nil)
	if sessionLogIndex(m) < 0 {
		t.Fatal("setup: no session log was drawn for a start that loaded AGENTS.md")
	}

	blob, err := encodeTranscript(&m.transcript)
	if err != nil {
		t.Fatalf("encodeTranscript: %v", err)
	}
	if bytes.Contains(blob, []byte("AGENTS.md")) || bytes.Contains(blob, []byte(sessionLogLabel)) {
		t.Errorf("the saved transcript carries the session log: %s", blob)
	}
	entries, err := decodeTranscript(blob)
	if err != nil {
		t.Fatalf("decodeTranscript: %v", err)
	}
	for _, e := range entries {
		if e.kind == entrySessionLog {
			t.Errorf("the decoded record holds a session log: %+v", e)
		}
	}
	if !bytes.Contains(blob, []byte("what is the capital of france")) {
		t.Errorf("the saved transcript lost the conversation it exists for: %s", blob)
	}
}

// A note reaches the log escape-stripped at the seam, whoever called it — the same rule addNote and
// addEphemeralNote keep.
func TestSessionLogStripsEscapes(t *testing.T) {
	t.Parallel()

	var tr transcript
	tr.openSessionLog()
	if !tr.addSessionLogNote("context: A\x1b[31mGENTS.md\x1b]52;c;x\x07 (512 B)") {
		t.Fatal("an open log refused a note")
	}

	at := tr.sessionLogIndex()
	if at < 0 {
		t.Fatal("the first note drew no session log")
	}
	if got, want := tr.entries[at].notes, []string{"context: AGENTS.md (512 B)"}; !slices.Equal(got, want) {
		t.Errorf("notes = %q, want the escape-stripped %q", got, want)
	}
}

// Collapsed, the log is its counted header alone under a ▶; open, the ▼ is the fold and every note
// is its own `· ` row, uncapped, a note of several lines hung under its first line's text.
func TestSessionLogPaintsHeaderAloneThenEveryNote(t *testing.T) {
	t.Parallel()

	th := newTheme(scheme.Default())
	in := paintInput{kind: entrySessionLog, notes: []string{"context: AGENTS.md (3.1 KiB)", "first line\nsecond line"}}

	collapsed := renderEntryLines(th, in, 80, false)
	if got, want := stripAll(collapsed.lines), []string{"✦ Session log (2 notes) " + defaultGlyphs.collapsed}; !reflect.DeepEqual(got, want) {
		t.Errorf("collapsed paint = %q, want the header alone %q", got, want)
	}

	in.expanded = true
	open := renderEntryLines(th, in, 80, false)
	want := []string{
		"✦ Session log (2 notes) " + glyphExpanded,
		"  · context: AGENTS.md (3.1 KiB)",
		"  · first line",
		"    second line",
	}
	if got := stripAll(open.lines); !reflect.DeepEqual(got, want) {
		t.Errorf("expanded paint = %q, want %q", got, want)
	}
	for i, target := range open.targets {
		if target.kind != targetHeader {
			t.Errorf("row %d is marked %v, want every row the log's own toggle (targetHeader)", i, target.kind)
		}
	}

	single := renderEntryLines(th, paintInput{kind: entrySessionLog, notes: []string{"one"}}, 80, false)
	if got := strings.TrimRight(strip(single.lines[0]), " "); got != "✦ Session log (1 note) "+defaultGlyphs.collapsed {
		t.Errorf("one note paints %q, want the singular header", got)
	}
}

// stripAll is each painted line with its styling stripped and its trailing pad trimmed.
func stripAll(lines []string) []string {
	out := make([]string, len(lines))
	for i, ln := range lines {
		out[i] = strings.TrimRight(strip(ln), " ")
	}
	return out
}

// The log's fold is its own: a click on it flips that one log and nothing else — every task card
// stays as it was, `opts.UI.TaskListOpen` stays as it was, and nothing is written back — and a
// second click folds it again.
func TestSessionLogToggleLeavesTaskCardsAlone(t *testing.T) {
	t.Parallel()

	log := &settingsWriteLog{}
	opts := testOpts
	opts.Settings = fakeSettingsHost{write: log.write}
	m := modelWithTaskListBlock(t, opts)
	m.transcript.openSessionLog()
	m.transcript.addSessionLogNote("context: AGENTS.md (3.1 KiB)")
	m.refreshViewport()

	cards := taskListEntries(m)
	if len(cards) != 1 || !m.transcript.entries[cards[0]].expanded {
		t.Fatalf("setup: task cards %v, want one, open under the default preference", cards)
	}
	at := sessionLogIndex(m)
	if at < 0 || m.transcript.entries[at].expanded {
		t.Fatalf("setup: session log at %d, want one, collapsed", at)
	}
	taskOpen := m.opts.UI.TaskListOpen

	m = clickCell(t, m, 2, screenRow(t, m, headerLineOf(t, m, at)))

	if !m.transcript.entries[at].expanded {
		t.Error("a click on the session log's header did not open it")
	}
	if got, want := sessionLogHeaders(m), []string{"✦ Session log (1 note) " + glyphExpanded}; !reflect.DeepEqual(got, want) {
		t.Errorf("the next frame paints %q, want the log open %q", got, want)
	}
	if !m.transcript.entries[cards[0]].expanded {
		t.Error("a click on the session log folded the task card; the log's fold is its own")
	}
	if m.opts.UI.TaskListOpen != taskOpen {
		t.Errorf("opts.UI.TaskListOpen moved to %v; the log's fold is never the task-list preference", m.opts.UI.TaskListOpen)
	}
	if len(log.writes) != 0 {
		t.Errorf("writes = %+v, want none — the log's fold is session-only", log.writes)
	}

	m = clickCell(t, m, 2, screenRow(t, m, headerLineOf(t, m, at)))
	if m.transcript.entries[at].expanded {
		t.Error("a second click did not fold the session log again")
	}
	if !m.transcript.entries[cards[0]].expanded || len(log.writes) != 0 {
		t.Errorf("the second click moved the task card (open %v) or wrote %+v", m.transcript.entries[cards[0]].expanded, log.writes)
	}
}

// Every session boundary opens a fresh log before its first notice — even one that loads no
// context file, so a routine note placed after it still has a log to join.
func TestSessionLogOpensAtBoundaryWithoutContextFiles(t *testing.T) {
	t.Parallel()

	// closedThen closes the launch's log — as the previous session's first Exchange would have —
	// before driving the boundary, so the open the test reads is the boundary's own.
	closedThen := func(boundary func(t *testing.T, m Model) Model) func(t *testing.T) Model {
		return func(t *testing.T) Model {
			m := newTestModelEng(t, &fakeEngine{}, testOpts)
			m.transcript.closeSessionLog()
			return boundary(t, m)
		}
	}
	tests := []struct {
		title string
		drive func(t *testing.T) Model
	}{
		{"the startup seed", func(t *testing.T) Model { return newTestModelEng(t, &fakeEngine{}, testOpts) }},
		{"/clear", closedThen(clearSession)},
		{"a /sessions restore", closedThen(restoreSession)},
	}
	for _, tc := range tests {
		t.Run(tc.title, func(t *testing.T) {
			t.Parallel()

			m := tc.drive(t)
			if !m.transcript.sessionLogOpen {
				t.Fatal("the boundary left the session log closed")
			}
			if at := sessionLogIndex(m); at >= 0 {
				t.Errorf("a boundary with no routine note drew a session log at entry %d", at)
			}
			m.noteRoutine("routine")
			if got := sessionLogNotes(m); !slices.Equal(got, []string{"routine"}) {
				t.Errorf("session log notes = %q, want the note placed after the boundary", got)
			}
		})
	}
}

// The session's first Exchange launch closes the log — a later routine note prints as its own line
// and the log's count stands where it stood — and transcript.reset clears the open flag outright.
func TestSessionLogClosedByEnterRunningAndClearedByReset(t *testing.T) {
	t.Parallel()

	t.Run("enterRunning closes it", func(t *testing.T) {
		t.Parallel()

		m := newTestModelEng(t, &fakeEngine{contextReport: loadedReport()}, testOpts)
		if !m.transcript.sessionLogOpen {
			t.Fatal("setup: construction left the log closed")
		}
		m = sendLine(t, m, "hello")

		if m.transcript.sessionLogOpen {
			t.Fatal("the first Exchange launch left the session log open")
		}
		m.noteRoutine("late routine note")
		if got, want := sessionLogNotes(m), []string{"context: AGENTS.md (3.1 KiB)"}; !slices.Equal(got, want) {
			t.Errorf("session log notes = %q, want the count to stand at %q", got, want)
		}
		if !hasEntry(m, entryNote, "late routine note") {
			t.Error("a routine note after the close did not print as its own line")
		}
	})

	t.Run("reset clears it", func(t *testing.T) {
		t.Parallel()

		var tr transcript
		tr.openSessionLog()
		tr.addSessionLogNote("one")
		tr.reset()

		if tr.sessionLogOpen {
			t.Error("reset left the session log open")
		}
		if tr.addSessionLogNote("two") {
			t.Error("a reset transcript took a log note before any boundary opened one")
		}
		if at := tr.sessionLogIndex(); at >= 0 {
			t.Errorf("reset left a session log at entry %d", at)
		}
	})
}
