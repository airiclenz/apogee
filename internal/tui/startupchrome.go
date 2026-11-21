package tui

// The start-up chrome (layout.md, "The start-up box"): the one module that decides where a start-up
// note lands. A note's kind declares its class and source ([noticeKinds]); the chrome adds the two
// facts a kind cannot know — how the note came to be raised (its [contact]) and whether the
// session's start-up log is still open — and from those four reads one [dest] off a pure rule
// ([destination]). The transcript keeps no log-open flag of its own: it places what it is told.

// contact is how a note came to be raised — the one fact about a note its kind cannot declare.
type contact uint8

const (
	// contactEvent: raised by any other event — a session boundary's notices, a launch's
	// colour-scheme warnings, a git read's refusal, a liveness crossing, a routing change, and a
	// rebind that is news worded in full: a `/model` pick, the post-`/server` seed, every beat after
	// the first. It is the zero value, so a [rebindIntent] built with no contact is one of these.
	contactEvent contact = iota
	// contactFirst: raised by the session's first-contact beat (set by [Model.observeBinding] from
	// the pre-fold evidence), which is part of how the session started — re-derived at every
	// launch and resume, so its notes are display-only.
	contactFirst
	// contactSettle: raised by a fallback settle ([Model.followFallback]), which re-binds the model
	// the switch already announced — quiet like first contact, yet news about a move the session
	// just made, not a record of the start.
	contactSettle
)

// dest is where one start-up note lands.
type dest uint8

const (
	// destLog: inside the session's start-up log ([transcript.addLogNote]), display-only.
	destLog dest = iota
	// destLinePersisted: its own note line, saved with the session record ([transcript.addNote]).
	destLinePersisted
	// destLineEphemeral: its own note line, display-only ([transcript.addEphemeralNote]).
	destLineEphemeral
)

// destination is where a note of the kind whose row is rule, raised by c, lands while the session
// log is (isLogOpen) or is not open. A ROUTINE note joins the open log when it records the start:
// a launch's or the workspace's (source boundary or ambient), or a beat's raised by the session's
// first contact. Every other note keeps its own line — persisted only for a beat's raised by any
// later door (an event or a fallback settle), which records a move the session made, and ephemeral
// for everything that is re-derived at the next launch or boundary and would only stack a duplicate
// into the record: a boundary's, the workspace's, and a first-contact beat's (ADR 0022 addendum
// 0801). A loud note therefore never joins the log.
func destination(rule noticeKindRow, c contact, isLogOpen bool) dest {
	recordsTheStart := rule.source != sourceBeat || c == contactFirst
	if rule.class == classRoutine && isLogOpen && recordsTheStart {
		return destLog
	}
	if rule.source == sourceBeat && c != contactFirst {
		return destLinePersisted
	}
	return destLineEphemeral
}

// startupChrome is the session's start-up log state and the router every launch-side and
// heartbeat note goes through. It is a plain value (ADR 0011) held in the sessionView, so a watch parks it with the
// rest of the view and a fresh sessionView literal starts with it closed.
//
// Its zero value is CLOSED: only a session boundary opens the log ([startupChrome.open]) —
// construction, /clear and /new, a /sessions restore — and the session's first Exchange launch
// closes it ([startupChrome.close], from [Model.enterRunning]). A boundary opens it before any note
// arrives; it draws nothing itself, so a start with no routine note paints no log at all.
type startupChrome struct {
	// isLogOpen says the session's start-up log still takes routine notes.
	isLogOpen bool
}

// open opens a fresh start-up log for the session a boundary just began: the routine notes it
// starts with gather from now on into one folded block rather than taking a line each.
func (c *startupChrome) open() {
	c.isLogOpen = true
}

// close stops the log taking notes: a note placed after it prints as its own line and the log's
// count stands where it stood. Closing a log that is already closed is a no-op.
func (c *startupChrome) close() {
	c.isLogOpen = false
}

// place puts one note of kind, raised by at, where [destination] says it lands in t. Every
// destination escape-strips at the transcript's seam, so text may carry untrusted config, disk and
// server strings.
func (c *startupChrome) place(t *transcript, kind NoticeKind, at contact, text string) {
	switch destination(kindRow(kind), at, c.isLogOpen) {
	case destLog:
		t.addLogNote(text)
	case destLinePersisted:
		t.addNote(text)
	default:
		t.addEphemeralNote(text)
	}
}

// beginSession is a session boundary — construction, /clear and /new, a /sessions restore: the
// transcript returns to empty, the one-time start-up box is seeded as its first entry from the
// display Options as they stand, and the session's start-up log opens before any of the boundary's
// notices arrives. On a fresh Model the reset finds nothing to drop. It is the only place a
// boundary composes those three steps, so no boundary can reseed the box without reopening the log.
func (m *Model) beginSession() {
	m.transcript.reset()
	m.transcript.addStartup(newStartupView(m.opts))
	m.chrome.open()
}

// restateBox re-states the start-up box's facts in place from the display Options as they now
// stand — a rebind, a server switch or bind, a pre-bound pick, an update check's answer — leaving
// the box where it sits in the scrollback ([transcript.refreshStartup]). The box's view is a frozen
// copy taken when it was seeded, so every change to a fact it shows comes through here.
func (m *Model) restateBox() {
	m.transcript.refreshStartup(newStartupView(m.opts))
}
