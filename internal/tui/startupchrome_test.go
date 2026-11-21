package tui

import (
	"slices"
	"testing"
)

// TestStartupChromeDestination pins the one rule a start-up note's destination is read from, over
// every class × source × contact, with the session log open and closed: a routine note that records
// the start joins the open log, a beat's own line is persisted and every other own line ephemeral.
func TestStartupChromeDestination(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		class      noticeClass
		source     noticeSource
		contact    contact
		whenOpen   dest
		whenClosed dest
	}{
		{"routine boundary, event", classRoutine, sourceBoundary, contactEvent, destLog, destLineEphemeral},
		{"routine boundary, first", classRoutine, sourceBoundary, contactFirst, destLog, destLineEphemeral},
		{"routine boundary, settle", classRoutine, sourceBoundary, contactSettle, destLog, destLineEphemeral},
		{"routine ambient, event", classRoutine, sourceAmbient, contactEvent, destLog, destLineEphemeral},
		{"routine ambient, first", classRoutine, sourceAmbient, contactFirst, destLog, destLineEphemeral},
		{"routine ambient, settle", classRoutine, sourceAmbient, contactSettle, destLog, destLineEphemeral},
		{"routine beat, event", classRoutine, sourceBeat, contactEvent, destLinePersisted, destLinePersisted},
		{"routine beat, first", classRoutine, sourceBeat, contactFirst, destLog, destLinePersisted},
		{"routine beat, settle", classRoutine, sourceBeat, contactSettle, destLinePersisted, destLinePersisted},
		{"loud boundary, event", classLoud, sourceBoundary, contactEvent, destLineEphemeral, destLineEphemeral},
		{"loud boundary, first", classLoud, sourceBoundary, contactFirst, destLineEphemeral, destLineEphemeral},
		{"loud boundary, settle", classLoud, sourceBoundary, contactSettle, destLineEphemeral, destLineEphemeral},
		{"loud ambient, event", classLoud, sourceAmbient, contactEvent, destLineEphemeral, destLineEphemeral},
		{"loud ambient, first", classLoud, sourceAmbient, contactFirst, destLineEphemeral, destLineEphemeral},
		{"loud ambient, settle", classLoud, sourceAmbient, contactSettle, destLineEphemeral, destLineEphemeral},
		{"loud beat, event", classLoud, sourceBeat, contactEvent, destLinePersisted, destLinePersisted},
		{"loud beat, first", classLoud, sourceBeat, contactFirst, destLinePersisted, destLinePersisted},
		{"loud beat, settle", classLoud, sourceBeat, contactSettle, destLinePersisted, destLinePersisted},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rule := noticeKindRow{class: tt.class, source: tt.source}

			if got := destination(rule, tt.contact, true); got != tt.whenOpen {
				t.Errorf("log open: destination = %d, want %d", got, tt.whenOpen)
			}
			if got := destination(rule, tt.contact, false); got != tt.whenClosed {
				t.Errorf("log closed: destination = %d, want %d", got, tt.whenClosed)
			}
		})
	}

	// The heartbeat's notes, by the kind each carries and the contact its rebind was raised by: the
	// first-contact beat's routine notes join the open log and take a persisted line of their own
	// once a prompt has closed it; its notes that need the human — a refused rebind, the roster
	// that was not applied, an unknown window, a cleared override — keep a persisted line while the
	// log is open; a `/model` pick's, the post-`/server` seed's and a fallback settle's notes are
	// news on a persisted line of their own; and a routing change keeps an ephemeral one.
	heartbeat := []struct {
		name       string
		kind       NoticeKind
		contact    contact
		whenOpen   dest
		whenClosed dest
	}{
		{"first beat's mandatory effort", kindMandatoryEffort, contactFirst, destLog, destLinePersisted},
		{"first beat's change", kindRebindChange, contactFirst, destLog, destLinePersisted},
		{"first beat's model binding", NoticeModelBinding, contactFirst, destLog, destLinePersisted},
		{"first beat's trusted hint", NoticeTrustedHint, contactFirst, destLog, destLinePersisted},
		{"first beat's refused rebind", kindRebindFailed, contactFirst, destLinePersisted, destLinePersisted},
		{"first beat's unapplied roster", NoticeRosterUnapplied, contactFirst, destLinePersisted, destLinePersisted},
		{"first beat's unknown window", kindWindowUnknown, contactFirst, destLinePersisted, destLinePersisted},
		{"first beat's cleared effort", kindEffortCleared, contactFirst, destLinePersisted, destLinePersisted},
		{"a /model pick's change", kindRebindChange, contactEvent, destLinePersisted, destLinePersisted},
		{"a later beat's mandatory effort", kindMandatoryEffort, contactEvent, destLinePersisted, destLinePersisted},
		{"a fallback settle's model binding", NoticeModelBinding, contactSettle, destLinePersisted, destLinePersisted},
		{"a fallback settle's mandatory effort", kindMandatoryEffort, contactSettle, destLinePersisted, destLinePersisted},
		{"an unkinded rebind notice", kindUnset, contactFirst, destLineEphemeral, destLineEphemeral},
		{"offline", kindOffline, contactEvent, destLinePersisted, destLinePersisted},
		{"online", kindOnline, contactEvent, destLinePersisted, destLinePersisted},
		{"routing", kindRouting, contactEvent, destLineEphemeral, destLineEphemeral},
	}
	for _, tt := range heartbeat {
		t.Run("heartbeat/"+tt.name, func(t *testing.T) {
			t.Parallel()
			rule := kindRow(tt.kind)

			if got := destination(rule, tt.contact, true); got != tt.whenOpen {
				t.Errorf("log open: destination = %d, want %d", got, tt.whenOpen)
			}
			if got := destination(rule, tt.contact, false); got != tt.whenClosed {
				t.Errorf("log closed: destination = %d, want %d", got, tt.whenClosed)
			}
		})
	}
}

// TestStartupChromePlace pins that each destination the rule names is the transcript place a note
// lands in: the open log gathers a routine note, a loud note keeps an ephemeral line of its own,
// and a later rebind's routine note keeps a persisted one.
func TestStartupChromePlace(t *testing.T) {
	t.Parallel()

	var tr transcript
	var chrome startupChrome
	chrome.open()

	chrome.place(&tr, kindContextLoaded, contactEvent, "context: AGENTS.md (3.1 KiB)")
	chrome.place(&tr, kindColorScheme, contactEvent, "color-scheme: unknown")
	chrome.place(&tr, NoticeModelBinding, contactSettle, "model profile: m")

	if at := tr.sessionLogIndex(); at < 0 || !slices.Equal(tr.entries[at].notes, []string{"context: AGENTS.md (3.1 KiB)"}) {
		t.Errorf("session log at %d, want it holding the routine note alone", at)
	}
	if e, ok := noteEntry(tr, "color-scheme: unknown"); !ok || !e.ephemeral {
		t.Errorf("the loud note is %+v (found %v), want its own ephemeral line", e, ok)
	}
	if e, ok := noteEntry(tr, "model profile: m"); !ok || e.ephemeral {
		t.Errorf("the settled rebind's note is %+v (found %v), want its own persisted line", e, ok)
	}
}

// TestStartupChromeZeroValueIsClosed pins that a fresh chrome — the one a new sessionView literal
// carries — has no log to join until a boundary opens it.
func TestStartupChromeZeroValueIsClosed(t *testing.T) {
	t.Parallel()

	var tr transcript
	var chrome startupChrome

	chrome.place(&tr, kindContextLoaded, contactEvent, "routine")

	if chrome.isLogOpen {
		t.Error("the zero chrome reads as open")
	}
	if at := tr.sessionLogIndex(); at >= 0 {
		t.Errorf("a closed chrome drew a session log at entry %d", at)
	}
	if e, ok := noteEntry(tr, "routine"); !ok || !e.ephemeral {
		t.Errorf("the routine note is %+v (found %v), want its own ephemeral line", e, ok)
	}
}

// TestStartupChromeBoundaryReopensTheLog pins that every session boundary after the launch — /clear,
// /new and a /sessions restore — reopens a start-up log the previous session had closed: the box is
// reseeded as the first entry, and the boundary's own routine notice (the context-files report)
// gathers into the fresh log rather than taking a line of its own.
func TestStartupChromeBoundaryReopensTheLog(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		drive func(t *testing.T, m Model) Model
	}{
		{"/clear", clearSession},
		{"/new", newSession},
		{"a /sessions restore", restoreSession},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			m := newTestModelEng(t, &fakeEngine{contextReport: loadedReport()}, testOpts)
			m.chrome.close() // the first Exchange launch closed the outgoing session's log

			m = tc.drive(t, m)

			if !m.chrome.isLogOpen {
				t.Error("the boundary left the start-up log closed")
			}
			if len(m.transcript.entries) == 0 || m.transcript.entries[0].kind != entryStartup {
				t.Errorf("entries[0] is not the start-up box: %+v", m.transcript.entries)
			}
			want := []string{"context: AGENTS.md (3.1 KiB)"}
			if at := m.transcript.sessionLogIndex(); at < 0 || !slices.Equal(m.transcript.entries[at].notes, want) {
				t.Errorf("session log at %d, want it holding %v", at, want)
			}
		})
	}
}

// noteEntry is tr's note line reading text, and whether there is one.
func noteEntry(tr transcript, text string) (entry, bool) {
	for _, e := range tr.entries {
		if e.kind == entryNote && e.text == text {
			return e, true
		}
	}
	return entry{}, false
}
