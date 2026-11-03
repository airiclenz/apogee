package tui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/gitstatus"
)

// GitStatusHost is the seam the footer's git segment reads the workspace's git state through. The
// renderer owns only WHEN a read runs; HOW git is run — the hardened runner, the no-prompt
// environment, the refused-repository rule — is the host's, which is why this package imports
// neither internal/gitexec nor internal/subprocess. cmd/apogee backs it; nil in [Options.GitStatus]
// is unwired, and an unwired TUI arms no chain and runs no git (ADR 0054).
//
// Both calls block, and both run off the Update loop on a Cmd goroutine under the program context,
// so a shutdown cancels a call still in flight.
type GitStatusHost interface {
	// Status reads root's git state. inside is false, with a nil error, where root is no work
	// tree (or the host refuses the repository); a non-nil error means no trustworthy answer
	// this time, and the snapshot and inside carry nothing.
	Status(ctx context.Context, root string) (snap gitstatus.Snapshot, inside bool, err error)
	// Fetch updates root's remote-tracking refs, so the next Status sees new upstream commits.
	Fetch(ctx context.Context, root string) error
}

// gitStatusCadence is the status chain's cadence: how long after a read LANDS the next one runs,
// unless the read itself was slow ([gitStatusWait]).
const gitStatusCadence = 5 * time.Second

// gitStatusSlowFactor stretches the wait after a slow read: the chain waits at least this many
// times as long as the read took, so a large repository on a loaded box spends at most a fifth of
// its time in `git status` rather than running it back to back.
const gitStatusSlowFactor = 4

// gitStatusBackoff is the wait after a read that found no work tree or failed. Neither is worth the
// 5 s cadence: a workspace that is no repository rarely becomes one, and a failing git (a wedged
// lock, a probe that timed out on a slow box) only gets more load from being asked again. A Turn
// end still re-reads at once, so a `git init` the model ran shows up when its Turn ends.
const gitStatusBackoff = time.Minute

// gitFetchCadence is the fetch chain's cadence: how long after a fetch lands the next one runs.
const gitFetchCadence = 5 * time.Minute

// gitState is the Model's view of the workspace's git state and of the two chains that keep it
// current. It holds plain values only — the snapshot with its entry lists dropped — so it copies
// safely with the Model (ADR 0011).
//
// The status chain is ONE live generation, like the heartbeat's: a read is issued, its landing
// folds the snapshot and arms the next read one wait later, so two reads never overlap. A Turn end
// asks for a read now: while the chain waits on its tick that opens a fresh generation (retiring
// the pending tick), and while a read is in flight it only marks the chain dirty, so the landing
// issues exactly one follow-up read instead of the tick. The fetch chain is a second, independent
// generation, live only while `ui.git-fetch` is on and the snapshot names an upstream.
type gitState struct {
	// snap is the last good snapshot; meaningful only when hasSnap. Its entry lists are nil.
	snap gitstatus.Snapshot
	// hasSnap reports that the workspace was inside a work tree at the last good read. A failed
	// read keeps it (and snap); a read that found no work tree clears it.
	hasSnap bool

	// gen is the status chain's live generation; a Msg carrying any other one is inert. 0 means
	// no chain (the host is unwired).
	gen int
	// inFlight reports that a status read of the live generation has been issued and has not
	// landed. newModel sets it for the read Init issues.
	inFlight bool
	// dirty reports that a refresh was asked for while a read was in flight: the landing issues
	// one more read at once instead of arming the tick.
	dirty bool

	// fetchGen is the fetch chain's live generation; a fetch Msg carrying any other one is inert.
	fetchGen int
	// fetchLive reports that a fetch chain is running: a fetch in flight or its tick pending. A
	// chain is armed only while none is, so two fetches never run at once.
	fetchLive bool
}

// gitStatusMsg carries one landed status read into the Update loop, stamped with the generation
// that issued it and how long the read took.
type gitStatusMsg struct {
	gen     int
	snap    gitstatus.Snapshot
	inside  bool
	err     error
	elapsed time.Duration
}

// gitStatusTickMsg fires the status chain's next read, one wait after the last one landed.
type gitStatusTickMsg struct{ gen int }

// gitFetchMsg carries one landed fetch into the Update loop.
type gitFetchMsg struct {
	gen int
	err error
}

// gitFetchTickMsg fires the fetch chain's next fetch, one interval after the last one landed.
type gitFetchTickMsg struct{ gen int }

// gitWired reports whether the footer's git poll runs at all: a host is wired and there is a
// workspace to read.
func (m Model) gitWired() bool {
	return m.opts.GitStatus != nil && m.opts.Workspace != ""
}

// gitSnapshot is the footer's reading of the workspace's git state: the last good snapshot, and
// false when there is none to show (unwired, not yet landed, or not inside a work tree).
func (m Model) gitSnapshot() (gitstatus.Snapshot, bool) {
	return m.git.snap, m.git.hasSnap
}

// gitStatusCmd runs one status read off the Update loop and reports it stamped with the CURRENT
// generation. Like [Model.beatCmd] it captures the program context and the seam by value, and it
// CONTINUES a chain: the caller has already marked the read in flight on the Model it returns.
func (m Model) gitStatusCmd() tea.Cmd {
	if !m.gitWired() {
		return nil
	}
	host, ctx, root, gen := m.opts.GitStatus, m.parent, m.opts.Workspace, m.git.gen
	return func() tea.Msg {
		start := time.Now()
		snap, inside, err := host.Status(ctx, root)
		return gitStatusMsg{gen: gen, snap: snap, inside: inside, err: err, elapsed: time.Since(start)}
	}
}

// refreshGit asks for a status read now — at a Turn end, or after a fetch landed. While the chain
// waits on its tick it opens a FRESH generation and issues the read, which retires the pending tick
// (the [Model.armBeat] shape); while a read is in flight it only marks the chain dirty, so the
// landing issues the one follow-up. Unwired, it does nothing.
//
// It takes a pointer because the generation bump must land on the Model the caller returns: every
// caller refreshes in a statement of its own (ADR 0011, [Model.armBeat]).
func (m *Model) refreshGit() tea.Cmd {
	if !m.gitWired() {
		return nil
	}
	if m.git.inFlight {
		m.git.dirty = true
		return nil
	}
	m.git.gen++
	m.git.inFlight = true
	return m.gitStatusCmd()
}

// gitAfterTurn is the Turn-end refresh laid over a fold's result: the fold that ended a Turn (a
// snapshotted Turn, a cancel, a loop error) runs, then the git state is asked for again, so the
// footer shows what the Turn's edits did without waiting out the cadence.
//
// Only the session's own Turns count. A delegate's Turns end inside the parent's Exchange, whose
// own Turn end re-reads after them, and a background workflow's Turns emit none of these Msgs —
// their edits reach the footer on the next tick.
func gitAfterTurn(next tea.Model, cmd tea.Cmd) (tea.Model, tea.Cmd) {
	m := next.(Model)
	refresh := m.refreshGit()
	return m, tea.Batch(cmd, refresh)
}

// gitStatusWait is how long the chain waits after a read before the next: the interval, stretched
// to [gitStatusSlowFactor] times a slow read's own duration.
func gitStatusWait(interval, elapsed time.Duration) time.Duration {
	return max(interval, gitStatusSlowFactor*elapsed)
}

// gitStatusInterval is the status chain's cadence: [Options.GitStatusInterval] when the host states
// one, [gitStatusCadence] otherwise. The backoff after a miss is not shortened by it.
func (m Model) gitStatusInterval() time.Duration {
	if m.opts.GitStatusInterval > 0 {
		return m.opts.GitStatusInterval
	}
	return gitStatusCadence
}

// gitFetchInterval is the fetch chain's cadence: [Options.GitFetchInterval] when the host states
// one, [gitFetchCadence] otherwise.
func (m Model) gitFetchInterval() time.Duration {
	if m.opts.GitFetchInterval > 0 {
		return m.opts.GitFetchInterval
	}
	return gitFetchCadence
}

// foldGitStatus folds one landed read and re-arms the chain from HERE, so reads never overlap. A
// failed read keeps the last snapshot — a timed-out read on a slow box is no reason to blank the
// segment — and a read that found no work tree clears it; both wait out [gitStatusBackoff] rather
// than the cadence. A Turn end that arrived meanwhile turns the wait into one read at once.
func (m Model) foldGitStatus(msg gitStatusMsg) (tea.Model, tea.Cmd) {
	if !m.gitWired() || msg.gen != m.git.gen {
		return m, nil
	}
	m.git.inFlight = false
	wait := gitStatusWait(m.gitStatusInterval(), msg.elapsed)
	switch {
	case msg.err != nil:
		wait = max(gitStatusBackoff, wait)
	case !msg.inside:
		m.git.snap, m.git.hasSnap = gitstatus.Snapshot{}, false
		wait = max(gitStatusBackoff, wait)
	default:
		m.git.snap, m.git.hasSnap = countsOnly(msg.snap), true
	}
	fetch := m.armGitFetch()
	if m.git.dirty {
		m.git.dirty = false
		m.git.inFlight = true
		return m, tea.Batch(m.gitStatusCmd(), fetch)
	}
	gen := m.git.gen
	tick := tea.Tick(wait, func(time.Time) tea.Msg { return gitStatusTickMsg{gen: gen} })
	return m, tea.Batch(tick, fetch)
}

// foldGitStatusTick issues the live chain's next read; a tick from a retired chain is inert.
func (m Model) foldGitStatusTick(msg gitStatusTickMsg) (tea.Model, tea.Cmd) {
	if !m.gitWired() || msg.gen != m.git.gen || m.git.inFlight {
		return m, nil
	}
	m.git.inFlight = true
	return m, m.gitStatusCmd()
}

// countsOnly drops a snapshot's entry lists: the footer reads counts and branch fields, and the
// Model keeps plain values (ADR 0011).
func countsOnly(s gitstatus.Snapshot) gitstatus.Snapshot {
	s.StagedEntries, s.UnstagedEntries, s.UntrackedEntries = nil, nil, nil
	return s
}

// gitFetchWanted reports whether the fetch chain should run: `ui.git-fetch` is on and the last good
// snapshot names an upstream to fetch from.
func (m Model) gitFetchWanted() bool {
	return m.gitWired() && m.opts.UI.GitFetch && m.git.hasSnap && m.git.snap.Upstream != ""
}

// armGitFetch opens the fetch chain when it is wanted and none runs, and issues its first fetch at
// once. It is called where the wanted state can turn true — a landed read, the pref switched on —
// and does nothing while a chain runs, so there is never more than one. A chain whose reason went
// away retires itself at its next step ([Model.foldGitFetch], [Model.foldGitFetchTick]); a fetch
// already in flight finishes rather than being cut off, and no further one runs.
//
// It takes a pointer for the reason [Model.refreshGit] does.
func (m *Model) armGitFetch() tea.Cmd {
	if m.git.fetchLive || !m.gitFetchWanted() {
		return nil
	}
	m.git.fetchGen++
	m.git.fetchLive = true
	return m.gitFetchCmd()
}

// gitFetchCmd runs one fetch off the Update loop, stamped with the current fetch generation.
func (m Model) gitFetchCmd() tea.Cmd {
	host, ctx, root, gen := m.opts.GitStatus, m.parent, m.opts.Workspace, m.git.fetchGen
	return func() tea.Msg {
		return gitFetchMsg{gen: gen, err: host.Fetch(ctx, root)}
	}
}

// foldGitFetch folds one landed fetch. A fetch that succeeded asks for a status read, so `↓` shows
// what it brought; a failed one (no network, a refused repository) changes nothing on screen. The
// chain then waits its interval, or retires when the pref went off or the upstream went away.
func (m Model) foldGitFetch(msg gitFetchMsg) (tea.Model, tea.Cmd) {
	if !m.gitWired() || msg.gen != m.git.fetchGen || !m.git.fetchLive {
		return m, nil
	}
	var refresh tea.Cmd
	if msg.err == nil {
		refresh = m.refreshGit()
	}
	if !m.gitFetchWanted() {
		m.git.fetchLive = false
		return m, refresh
	}
	gen := m.git.fetchGen
	tick := tea.Tick(m.gitFetchInterval(), func(time.Time) tea.Msg { return gitFetchTickMsg{gen: gen} })
	return m, tea.Batch(refresh, tick)
}

// foldGitFetchTick issues the fetch chain's next fetch, or retires the chain when its reason went
// away while it waited. A tick from a retired chain is inert.
func (m Model) foldGitFetchTick(msg gitFetchTickMsg) (tea.Model, tea.Cmd) {
	if !m.gitWired() || msg.gen != m.git.fetchGen || !m.git.fetchLive {
		return m, nil
	}
	if !m.gitFetchWanted() {
		m.git.fetchLive = false
		return m, nil
	}
	return m, m.gitFetchCmd()
}
