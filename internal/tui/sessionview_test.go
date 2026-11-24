package tui

import (
	"slices"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/session"
	"github.com/airiclenz/apogee/internal/sidepanel"
)

// ----------------------------------------------------------------------------
// The session seat: one operation writes the session-owned fields (seatSession)
// ----------------------------------------------------------------------------

// spentSessionModel is a session that has been lived in: every field a session boundary owns
// holds something of the session being closed.
func spentSessionModel(t *testing.T) Model {
	t.Helper()
	m := newTestModel(t)
	m.transcript.addUser("an old question", nil)
	m.spentSkills = map[string]bool{"review": true}
	m.ctxUsed = 4096
	m.usage = domain.Usage{Calls: 3, PromptTokens: 3000, TotalTokens: 3000}
	m.usageBase = domain.Usage{Calls: 1, PromptTokens: 1000, TotalTokens: 1000}
	m.delegateUsage = childTotals
	m.servedModels = []string{"old-answerer"}
	m.sessionCurrency = "EUR"
	m.panel = sidepanel.New(time.Now)
	m.detached = true
	m.flash = "copied 12 chars"
	m.autoTitleFired = true
	m.titleTouched = true
	m.pendingTitle = titleStash{name: "late title"}
	m.sessionName = "the closed session"
	m.cacheBoundary(domain.Session{Version: domain.SessionVersion, State: []byte(`"old"`)})
	return m
}

// TestSeatSessionZeroSeedEmptiesEverySessionField pins the empty session /clear and /new seat: every
// field the closed session owned is emptied, and the transcript is a fresh launch's — the start-up
// box and nothing of the conversation. Under a live background workflow that has already spent, the
// side panel stays nil: the zero seed seeds no usage section, so the workflow's earlier spend (which
// the closed record counted) cannot open one before the rebase moves it out of the session.
func TestSeatSessionZeroSeedEmptiesEverySessionField(t *testing.T) {
	t.Parallel()

	t.Run("a lived-in session", func(t *testing.T) {
		t.Parallel()
		fresh := newTestModel(t)
		m := spentSessionModel(t)

		m.seatSession(sessionSeed{})

		if got, want := entryKinds(m), entryKinds(fresh); !slices.Equal(got, want) {
			t.Errorf("transcript kinds = %v, want a fresh launch's %v", got, want)
		}
		if m.spentSkills != nil {
			t.Errorf("spentSkills = %v, want none", m.spentSkills)
		}
		if m.ctxUsed != 0 {
			t.Errorf("ctxUsed = %d, want 0", m.ctxUsed)
		}
		if m.usage != (domain.Usage{}) || m.usageBase != (domain.Usage{}) || m.delegateUsage != (domain.Usage{}) {
			t.Errorf("usage = %+v, base = %+v, delegate = %+v, want all zero", m.usage, m.usageBase, m.delegateUsage)
		}
		if m.servedModels != nil {
			t.Errorf("servedModels = %q, want none", m.servedModels)
		}
		if m.sessionCurrency != "" {
			t.Errorf("sessionCurrency = %q, want \"\" (the configured label read live)", m.sessionCurrency)
		}
		if m.panel != nil {
			t.Error("the side panel's folded state survived the seat")
		}
		if m.detached || m.flash != "" {
			t.Errorf("detached = %v, flash = %q, want attached and no flash", m.detached, m.flash)
		}
		if m.autoTitleFired || m.titleTouched || m.pendingTitle.stashed() || m.sessionName != "" {
			t.Errorf("naming = fired %v, touched %v, stash %+v, name %q; want all unset",
				m.autoTitleFired, m.titleTouched, m.pendingTitle, m.sessionName)
		}
		if m.hasBoundary || m.boundary.State != nil {
			t.Errorf("boundary = %+v (has %v), want forgotten", m.boundary, m.hasBoundary)
		}
	})

	t.Run("under a spent live workflow the panel stays nil", func(t *testing.T) {
		t.Parallel()
		m := newTestModel(t)
		m = foldEvents(t, m,
			bgPhase(domain.WorkflowStarted),
			workflowRunUsage(bgChildBase("bg.1"), 1, 1000, 100),
		)
		if m.delegateUsageTotal().Calls <= 0 {
			t.Fatalf("precondition: the live workflow has spent nothing (%+v)", m.delegateUsageTotal())
		}

		m.seatSession(sessionSeed{})

		if m.panel != nil {
			t.Error("the zero seed opened a side panel from the closed session's workflow spend")
		}
		if !m.workflowsRunning() {
			t.Error("the seat dropped the live workflow; it runs on across the boundary")
		}
	})
}

// TestSeatSessionRecordSeedRelightsFromTheRecord pins a record's seat: the binding's first-contact
// lines come back under the box, the stored scrollback is repainted with its resume notices, the
// gauge, the accounting, the answerers and the label are relit from the record, the naming call
// is latched off with the human's titleTouched left standing, and the record's payload is the
// cached boundary. A record with an empty title keeps the name the frame shows.
func TestSeatSessionRecordSeedRelightsFromTheRecord(t *testing.T) {
	t.Parallel()
	const contactLine = "the server reports no context window"

	record := func(t *testing.T, title string) session.Record {
		t.Helper()
		stored := newTestModel(t)
		stored.transcript.addUser("a stored question", nil)
		blob, err := encodeTranscript(&stored.transcript)
		if err != nil {
			t.Fatalf("encodeTranscript: %v", err)
		}
		return session.Record{
			Meta: session.Meta{
				ID: "sess-1", Title: title, CtxUsed: 2048,
				Usage: eurRecordUsage, DelegateUsage: session.Usage(childTotals),
				ServedModels: []string{"stored-answerer"}, Currency: "EUR",
			},
			Transcript: blob,
			Session:    domain.Session{Version: domain.SessionVersion, State: []byte(`"stored"`)},
		}
	}
	seat := func(t *testing.T, rec session.Record) Model {
		t.Helper()
		m := spentSessionModel(t)
		m.hb.firstContactLines = []firstContactLine{{text: contactLine}}
		m.seatSession(recordSeed(rec, true))
		return m
	}

	t.Run("a titled record", func(t *testing.T) {
		t.Parallel()
		rec := record(t, "prior work")
		m := seat(t, rec)

		if m.transcript.entries[0].kind != entryStartup {
			t.Errorf("the first entry is %v, want the start-up box", m.transcript.entries[0].kind)
		}
		for _, want := range []struct {
			kind entryKind
			text string
		}{
			{entryNote, contactLine},
			{entryUser, "a stored question"},
			{entryNote, "resumed: prior work"},
			{entryNote, interruptedNote},
		} {
			if !hasEntry(m, want.kind, want.text) {
				t.Errorf("the seated transcript has no %v %q", want.kind, want.text)
			}
		}
		if hasEntry(m, entryUser, "an old question") {
			t.Error("the closed session's scrollback survived the seat")
		}
		if m.ctxUsed != 2048 {
			t.Errorf("ctxUsed = %d, want the record's 2048", m.ctxUsed)
		}
		if want := domain.Usage(eurRecordUsage); m.usageBase != want || m.usage != want {
			t.Errorf("usage = %+v, base = %+v, want both the record's %+v", m.usage, m.usageBase, want)
		}
		if m.delegateUsage != childTotals {
			t.Errorf("delegateUsage = %+v, want the record's %+v", m.delegateUsage, childTotals)
		}
		if !slices.Equal(m.servedModels, []string{"stored-answerer"}) {
			t.Errorf("servedModels = %q, want the record's", m.servedModels)
		}
		rec.Meta.ServedModels[0] = "mutated"
		if m.servedModels[0] != "stored-answerer" {
			t.Error("servedModels aliases the record's slice")
		}
		if m.sessionCurrency != "EUR" {
			t.Errorf("sessionCurrency = %q, want the record's EUR", m.sessionCurrency)
		}
		if m.panel == nil {
			t.Error("the side panel's usage section was not seeded from the record")
		}
		if m.spentSkills != nil || m.detached || m.flash != "" {
			t.Errorf("spentSkills %v, detached %v, flash %q: want the closed session's gone",
				m.spentSkills, m.detached, m.flash)
		}
		if !m.autoTitleFired || !m.titleTouched || m.pendingTitle.stashed() || m.sessionName != "prior work" {
			t.Errorf("naming = fired %v, touched %v, stash %+v, name %q; want latched, kept, dropped, the record's",
				m.autoTitleFired, m.titleTouched, m.pendingTitle, m.sessionName)
		}
		if !m.hasBoundary || string(m.boundary.State) != `"stored"` {
			t.Errorf("boundary = %+v (has %v), want the record's payload", m.boundary, m.hasBoundary)
		}
	})

	t.Run("an untitled record keeps the name", func(t *testing.T) {
		t.Parallel()
		m := seat(t, record(t, ""))
		if m.sessionName != "the closed session" {
			t.Errorf("sessionName = %q, want the name the frame already shows", m.sessionName)
		}
	})
}

// entryKinds lists the kinds of the transcript's entries, in order.
func entryKinds(m Model) []entryKind {
	kinds := make([]entryKind, 0, len(m.transcript.entries))
	for _, e := range m.transcript.entries {
		kinds = append(kinds, e.kind)
	}
	return kinds
}
