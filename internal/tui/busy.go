package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// ----------------------------------------------------------------------------
// The busy indicator (an idle session waiting on something slow)
// ----------------------------------------------------------------------------
//
// A Turn narrates itself: the running row's spinner, phrase and clock. Outside a Turn the session can
// still be WAITING — a launcher verb in flight (actuation.go), a footer git action (gitactions.go), a
// /sessions record being read (sessions.go), a bare /rename's naming call (autotitle.go), a wired
// heartbeat that has not bound a model yet, an MCP server's dial (mcpreconnect.go) — and on a slow
// box such a wait can run for a minute or more. Each already has its word ("loading qwen3…",
// "git pull…", "naming this session…", "connecting…", "reconnecting MCP…"), but a word that never moves reads as a frozen screen, so while the session is idle
// the status line carries the wait in the running row's shape: `<glyph> <phrase> · <clock>`. A pop-up
// still reading what it lists — the /sessions browser before its listing, the /workflows item view
// before its item — is a wait too, but one its own loading row narrates (popupLoading): the chain
// turns for that row's glyph and the status line stays blank.
//
// This file is the one place that knows what the TUI is waiting on. It holds no flag of its own: the
// wait is DERIVED at call time from the latches that already record it (busyWait), so it cannot
// disagree with the footer that reads the same latches. The only state it keeps is when the current
// wait began (busyClock), restamped by Update's tail whenever the wait's phrase changes.

// sessionLoadPhrase is the busy indicator's word for a /sessions record load in flight
// (holdSessionLoad): the record is read off the loop and restored when it lands.
const sessionLoadPhrase = "loading session…"

// busyWait names the wait an idle session is holding, in the words apogee already shows for it, and
// reports false when there is none. The sources rank most specific first: an actuation in flight
// ("loading <profile>…", the footer's own word, outranking connecting exactly as it does in
// [Model.upstreamSegments]), then a footer git action (the `git <verb>…` its start notes), a
// /sessions record load, a bare /rename's naming call, connecting, an MCP server's dial in flight,
// and last a pop-up's loading row. Each is read from the latch that already records it — the git
// action and session load holds (engineholds.go), the naming flag, the count of MCP dials running
// off the loop (mcpState), the pane's own loading state — never a mirrored one. The branch listing that opens switch branch is held as a git
// action too, but it is no `git switch…`: it switches nothing, notes nothing and lands in a moment,
// so it is no busy wait.
//
// Connecting is the footer's own condition — no model bound while something observes the Upstream —
// narrowed by the two states in which no beat is coming soon to end it. An offline session is not
// waiting on a first beat but refused (the footer says offline beside it), and a pre-bound one has no
// server for a beat to reach until the human picks one (ADR 0036); [ServerActs.CanObserve] holds in
// both, so without the narrowing an idle session in either would tick forever for nothing.
//
// An MCP dial in flight is one word whatever the count of servers dialling ([MCPReconnectingNote],
// ADR 0109). It ranks below connecting: with no model bound the session cannot run a Turn at all,
// which is the wait that matters more, while the dials only change which tools the next Turn sees.
//
// A pop-up loading row (popupLoading) is the one source with NO phrase: ok with an empty phrase. The
// row says `loading…` beside its own glyph, so a second word in the status line would narrate the
// same wait twice; the source keeps the chain ticking ([Model.spinning]) and leaves the slot blank
// (busyPhrase). It ranks last because every other wait carries a word the pane does not.
func (m Model) busyWait() (phrase string, ok bool) {
	if label := m.actuationLabel(); label != "" {
		return label, true
	}
	if m.holds.has(holdGitAction) && !m.gitBranchListing() {
		return m.git.action.runningNote(), true
	}
	if m.holds.has(holdSessionLoad) {
		return sessionLoadPhrase, true
	}
	if m.naming {
		return namingPhrase, true
	}
	if m.opts.Model == "" && m.observesUpstream() && !m.hb.offline && !m.prebound() {
		return connectingLabel, true
	}
	if m.mcp.inFlight > 0 {
		return MCPReconnectingNote, true
	}
	if (m.sessionBrowser.open && m.sessionBrowser.loading) || m.workflowsPane.itemLoading() {
		return "", true
	}
	return "", false
}

// busyClock is when the wait busyWait names began — the origin of the status line's busy clock. It is
// a plain value (a string and a time.Time), so it copies with the Model (ADR 0011). phrase is the wait
// the stamp was taken for: a wait whose phrase changes — a load handing over to "connecting…" — is a
// new wait, and its clock starts again. The zero value is "nothing awaited".
type busyClock struct {
	phrase string
	since  time.Time
}

// restampBusy keeps the busy clock in step with the wait busyWait names right now: a new phrase is
// stamped at now, an ended wait clears the clock, and a wait that is still the same one keeps its
// origin. Update's tail calls it after every fold ([busyAfterFold]), so every latch that starts or
// ends a wait is covered without a list of sites to remember. A wait with no phrase (a pop-up's
// loading row) shows no clock, so it keeps none either.
func (m *Model) restampBusy(now time.Time) {
	phrase, ok := m.busyWait()
	if !ok || phrase == "" {
		m.busySince = busyClock{}
		return
	}
	if phrase != m.busySince.phrase {
		m.busySince = busyClock{phrase: phrase, since: now}
	}
}

// busyPhrase is the idle status line's busy form — the spinner's glyph, the wait's phrase and the
// clock counting from when it began — and "" when the session awaits nothing. It is composed like the
// running row (runningForms, runningPhrase) so the two read as one indicator, and carries no `quiet`
// qualifier: the stall guard speaks for an Exchange that has gone silent, and a wait has no stream to
// fall silent. now is passed in so the composition is testable off the wall clock. A wait with no
// phrase — a pop-up's loading row, which narrates itself — leaves the slot blank too.
func (m Model) busyPhrase(now time.Time) string {
	phrase, ok := m.busyWait()
	if !ok || phrase == "" {
		return ""
	}
	elapsed := time.Duration(0)
	if !m.busySince.since.IsZero() {
		elapsed = now.Sub(m.busySince.since)
	}
	glyph := m.spin.view(m.th, m.opts.UI.Spinner, m.opts.UI.SpinnerColor)
	return glyph + m.th.statusBar.Render(" "+phrase+" · "+formatElapsed(elapsed))
}

// busyAfterFold is Update's tail for the busy indicator ([Model.Update]'s deferred chain): it
// restamps the busy clock against the folded model and, when an IDLE session has started waiting with
// no spinner chain alive, arms one. The arm is idle-only on purpose: a running chain is kept alive by
// the sites that enter the running state (enterRunning, resumeRunning, the watch follower), so a fold
// that sets stateRunning without arming gets no Cmd from here. A chain that is still alive — a Turn's,
// whose last tick has not landed yet — simply carries on into the wait (Model.spinning), so a wait
// never opens a second chain.
func busyAfterFold(next tea.Model, cmd tea.Cmd) (tea.Model, tea.Cmd) {
	m, ok := next.(Model)
	if !ok {
		return next, cmd
	}
	m.restampBusy(time.Now())
	if m.state != stateIdle || m.spin.isLive {
		return m, cmd
	}
	if _, waiting := m.busyWait(); !waiting {
		return m, cmd
	}
	arm := m.spin.arm(m.opts.UI.Spinner)
	return m, tea.Batch(cmd, arm)
}
