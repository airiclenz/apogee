package tui

// Item 4 of the workspace-context-files plan: the session notice. These tests pin WHEN the
// notice is printed (at every session boundary the host owns, and only there), WHAT it says
// about loaded and unreadable files, and the one condition the Budget warn fires under.

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/session"
)

// loadedReport is the report of a session that loaded one 3174-byte AGENTS.md — the ordinary
// case every boundary below is driven with.
func loadedReport() domain.ContextFilesReport {
	return domain.ContextFilesReport{Files: []domain.ContextFileNote{{Name: "AGENTS.md", Bytes: 3174}}}
}

// contextNotes returns the transcript notes the context-files notice produced, in display order.
func contextNotes(m Model) []string {
	var out []string
	for _, text := range noteTexts(m) {
		if strings.HasPrefix(text, "context:") || strings.HasPrefix(text, "standing system content") {
			out = append(out, text)
		}
	}
	return out
}

// clearSession drives /clear through the input box, the way the human starts a fresh session.
func clearSession(t *testing.T, m Model) Model {
	t.Helper()
	m.input.SetValue("/clear")
	m, _ = stepCmd(t, m, keyEnter())
	return m
}

// restoreSession folds a loaded record into the live engine — the /sessions browser's resume,
// driven from the Msg the Load Cmd returns.
func restoreSession(t *testing.T, m Model) Model {
	t.Helper()
	return step(t, m, sessionLoadedMsg{rec: session.Record{
		Meta:    session.Meta{ID: "sess-1", Title: "prior work"},
		Session: domain.Session{Version: domain.SessionVersion},
	}})
}

// The notice is printed at each of the three session boundaries the host owns — the startup
// seed, /clear, and a /sessions restore — because each of them re-read the files, so each of
// them may be carrying different bytes than the last.
func TestContextFilesNoticeAtEverySessionBoundary(t *testing.T) {
	tests := []struct {
		title string
		drive func(t *testing.T, m Model) Model
	}{
		{"the startup seed", func(t *testing.T, m Model) Model { return m }},
		{"/clear", clearSession},
		{"a /sessions restore", restoreSession},
	}

	for _, tc := range tests {
		t.Run(tc.title, func(t *testing.T) {
			eng := &fakeEngine{contextReport: loadedReport()}
			m := tc.drive(t, newTestModelEng(t, eng, testOpts))

			want := []string{"context: AGENTS.md (3.1 KiB)"}
			if got := contextNotes(m); !slices.Equal(got, want) {
				t.Errorf("context notes = %v, want %v", got, want)
			}
		})
	}
}

// A repo carrying none of the configured names is the common case, and it stays silent: no note
// at any boundary, not even an empty one.
func TestContextFilesNoticeSilentWithoutFiles(t *testing.T) {
	eng := &fakeEngine{} // the zero report: nothing loaded, nothing unreadable
	m := restoreSession(t, clearSession(t, newTestModelEng(t, eng, testOpts)))

	if got := contextNotes(m); len(got) != 0 {
		t.Errorf("context notes = %v, want none — a session with no context files says nothing", got)
	}
	if eng.contextReads() == 0 {
		t.Error("the UI never read the report; silence must come from an empty report, not a skipped read")
	}
}

// Several files are named in one note, in list order, while a file that is present but
// unreadable gets a note of its own: the skip is loud, and it never fails the session.
func TestContextFilesNoticeNamesEveryFile(t *testing.T) {
	eng := &fakeEngine{contextReport: domain.ContextFilesReport{Files: []domain.ContextFileNote{
		{Name: "CONVENTIONS.md", Bytes: 512},
		{Name: "BROKEN.md", Err: "permission denied"},
		{Name: "AGENTS.md", Bytes: 3174},
	}}}

	m := newTestModelEng(t, eng, testOpts)

	want := []string{
		"context: CONVENTIONS.md (512 B), AGENTS.md (3.1 KiB)",
		"context: BROKEN.md unreadable — permission denied",
	}
	if got := contextNotes(m); !slices.Equal(got, want) {
		t.Errorf("context notes = %v, want %v", got, want)
	}
}

// The warn fires exactly when the report says the standing system content has outgrown its
// Budget share — never on an unknown window (a zero share), never on content that merely fills
// its share.
func TestContextFilesNoticeBudgetWarn(t *testing.T) {
	tests := []struct {
		title    string
		standing int
		share    int
		want     string
	}{
		{
			title:    "over its share",
			standing: 4242,
			share:    3900,
			want:     "standing system content ~4.1k tokens exceeds its Budget share (~3.8k) — trim context files or the system prompt",
		},
		{title: "exactly at its share", standing: 3900, share: 3900},
		{title: "inside its share", standing: 800, share: 3900},
		{title: "an unknown window has no share to overrun", standing: 9000, share: 0},
	}

	for _, tc := range tests {
		t.Run(tc.title, func(t *testing.T) {
			report := loadedReport()
			report.StandingTokens, report.SystemShare = tc.standing, tc.share

			m := newTestModelEng(t, &fakeEngine{contextReport: report}, testOpts)

			want := []string{"context: AGENTS.md (3.1 KiB)"}
			if tc.want != "" {
				want = append(want, tc.want)
			}
			if got := contextNotes(m); !slices.Equal(got, want) {
				t.Errorf("context notes = %v, want %v", got, want)
			}
		})
	}
}

// The notice is display-only: the human sees every line of it, but the session record never keeps
// one. Each line is re-derived from the workspace at the boundary that printed it — the files were
// just re-read — so persisting them would stack another copy into the record on every startup and
// every resume, which is the accumulation defect the resume notice had one call site over.
func TestContextFilesNoticeNeverPersisted(t *testing.T) {
	t.Parallel()
	report := domain.ContextFilesReport{
		Files: []domain.ContextFileNote{
			{Name: "AGENTS.md", Bytes: 3174},
			{Name: "BROKEN.md", Err: "permission denied"},
		},
		StandingTokens: 4242,
		SystemShare:    3900,
	}
	m := newTestModelEng(t, &fakeEngine{contextReport: report}, testOpts)
	m.transcript.addUser("what is the capital of france", nil) // the conversation the record IS for

	if got := contextNotes(m); len(got) != 3 {
		t.Fatalf("context notes = %v, want all three shown (loaded, unreadable, Budget warn)", got)
	}

	p, ok := m.snapshotPayload(domain.Session{})
	if !ok {
		t.Fatal("snapshotPayload failed on a transcript carrying the context notice")
	}
	for _, notice := range []string{"context:", "standing system content"} {
		if strings.Contains(string(p.transcript), notice) {
			t.Errorf("the saved blob carries the %q notice: %s", notice, p.transcript)
		}
	}
	if !strings.Contains(string(p.transcript), "what is the capital of france") {
		t.Errorf("the saved blob lost the conversation it exists for: %s", p.transcript)
	}
}

// The notice is a boundary event, not a repaint one: a resize (or any other Msg) must never
// re-read the report or reprint the note.
func TestContextFilesNoticeNotReprintedOnRepaint(t *testing.T) {
	eng := &fakeEngine{contextReport: loadedReport()}
	m := newTestModelEng(t, eng, testOpts)
	reads := eng.contextReads()

	m = step(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})

	if got := eng.contextReads(); got != reads {
		t.Errorf("report reads = %d after a resize, want the boundary's %d", got, reads)
	}
	if got := contextNotes(m); len(got) != 1 {
		t.Errorf("context notes = %v, want the single boundary note", got)
	}
}
