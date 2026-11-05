package tui

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/eventjson"
	"github.com/airiclenz/apogee/internal/gitstatus"
	"github.com/airiclenz/apogee/internal/session"
	"github.com/airiclenz/apogee/internal/undo"
	"github.com/airiclenz/apogee/internal/workflow"
)

// Watch mode (ADR 0103 decision 5): the TUI paints another session live and read-only. It is not a
// Driver — it drives no loop and holds an inert Engine — so everything it shows comes from two
// files the session's owner writes: the record, whose first `entries` entries it paints once, and
// the session's Event journal, which it follows from the start and folds through the same routing
// a live eventMsg takes (foldBackgroundEvent or foldEvent). The owner never learns a watcher
// exists (ADR 0034 D2): nothing here takes a lock, saves, or calls a real Engine method.
//
// One seam does the isolating: entering swaps m.eng for an [inertEngine] and parks the owner's
// engine, session host and session view in [watchState]; esc, and every other way out, swaps them
// back. Every keyed way to act on the session meets one allow-list gate ([Model.watchRefuses]) at
// the send and command chokepoints, and every engine setter a key reaches (shift+tab, a picker's
// accept) refuses with the same flash. A Msg of the owner's own session arriving meanwhile folds
// into the parked view with it swapped in, or waits for the way out ([Model.foldWhileWatching]),
// and the engine holds the watch takes (holdWatching) keep the idle-only work — a wake, a resume, a
// waiting pane — for after it.

// watchPollInterval is how often the follower looks for lines the owner appended to the journal.
// The journal coalesces deltas over ~30 ms, so a poll a few windows long paints text in steps a
// reader still sees as streaming, at the cost of one stat and one short read per poll.
const watchPollInterval = 100 * time.Millisecond

// watchProbeInterval is about how often the watch asks whether the owner still holds the session
// (watchSource.probe): often enough that the banner follows the owner's end within a second, rare
// enough that the shared lock the probe takes is never in a live owner's way (ADR 0103 decision 12).
const watchProbeInterval = time.Second

// watchProbeEvery is the follower read the probe rides on: every watchProbeEvery-th, the first one
// included. Riding the follower's own chain keeps one read of the journal in flight at a time, so
// the drain after a free probe never races a poll over the same bytes.
const watchProbeEvery = int(watchProbeInterval / watchPollInterval)

// watchLegendFormat is the prompt box's invitation while watching, filled with the session title.
const watchLegendFormat = "watching %s · read-only · esc back"

// watchEndedLegendFormat is the box once the owner has ended, filled with the title and the
// banner; watchResumableLegendFormat is the same, in the watcher's own home after a finish, where
// ⏎ resumes the record (resumeWatched).
const (
	watchEndedLegendFormat     = "watching %s · %s · read-only · esc back"
	watchResumableLegendFormat = "watching %s · %s · read-only · ⏎ resume · esc back"
)

// The owner-end banners. watchFinishedBanner: the journal closed with run_finished and the hold is
// free — the owner saved and let go. watchOwnerGoneBanner: the hold is free with no run_finished —
// the owner crashed or was killed, and its record may lack the last of what the journal showed.
const (
	watchFinishedBanner  = "finished"
	watchOwnerGoneBanner = "owner gone without a final save"
)

// watchRefusedFlash is the status-line flash a refused key or verb raises while watching: the
// session is someone else's run, and nothing typed here reaches it.
const watchRefusedFlash = "read-only — you are watching this session; esc goes back"

// watchRecordUnreadableNote is the note a watch paints when the record it should repaint from will
// not read: the journal still follows, so the view says why its head is missing.
const watchRecordUnreadableNote = "the session record could not be read — showing the live events only"

// watchVerbs is the view-only verb set that still runs while watching: each reads what the view
// already holds and reaches neither the engine nor the session. Every other verb is refused.
var watchVerbs = map[string]bool{
	"thinking": true,
	"help":     true,
	"usage":    true,
	"inspect":  true,
	"version":  true,
}

// watchSource names the session a watch follows: its id, the record it paints from, the journal it
// follows, the title the box shows until the journal names one, and the watched session's
// workspace root — the journal frame's, else the record's, "" when neither names one
// ([Model.watchedWorkspace]). isForeign marks a session of a
// watched home (ADR 0103 decision 7), which is never resumable. probe asks whether the owner still
// holds the session — the door that opens the watch binds it to Store.ProbeHold, so the TUI keeps
// no store of its own — and a nil probe never ends the watch.
type watchSource struct {
	id          string
	recordPath  string
	journalPath string
	title       string
	workspace   string
	isForeign   bool
	probe       holdProbe
}

// holdProbe reports whether the watched session's owner still holds it. It never holds, writes or
// creates anything (Store.ProbeHold); an error says nothing either way.
type holdProbe func() (isHeld bool, err error)

// watchEnding is what the watch knows of its owner's end.
type watchEnding int

const (
	// watchOwnerLive: the owner holds the session, or the watch has not yet found it free.
	watchOwnerLive watchEnding = iota
	// watchOwnerFinished: the hold is free and the journal closed with run_finished.
	watchOwnerFinished
	// watchOwnerGone: the hold is free and the journal never closed.
	watchOwnerGone
)

// watchState is the Model's watch-mode slot. It holds plain values only (ADR 0011): the parked
// owner engine and session view, the follower's read position, and gen, which tags every follower
// message so a batch read for an earlier watch — or after esc — is dropped on arrival.
type watchState struct {
	isActive bool
	gen      int
	source   watchSource
	// title is the session title the legend shows: the journal's own, once its run_started is read.
	title string
	// owner is the watcher's own engine, parked while m.eng is the inert one.
	owner Engine
	// ownerSessions is the watcher's own session host, parked while m.sessions is nil — which is
	// what gates every save (persist, saveAtIdle) and every queued record write out of the watch.
	ownerSessions SessionHost
	// parked is the watcher's own session view, swapped back in on the way out
	// ([Model.swapSessionView]).
	parked sessionView
	// held are the owner's Msgs that would open a surface over the view (watchRouteHeld); they are
	// delivered again once the watch ends.
	held []tea.Msg
	// journalCursor is where the follower stands in the journal it follows.
	journalCursor
	// readsUntilProbe counts the follower reads left before the next one probes the hold.
	readsUntilProbe int
	// hasRunFinished says the journal's run_finished has folded since its last run_started.
	hasRunFinished bool
	// ending is the owner's end once the probe found the hold free; past it nothing is read.
	ending watchEnding
	// window is the watched run's context window, from the last depth-0, non-maintenance usage
	// line that named one, else the journal's opening frame (its context_window); 0 until either
	// does, the watcher's own window standing in meanwhile ([Model.topLevelWindow]). It is the watched run's, so it lives here and never in m.opts,
	// and leaveWatch drops it with the rest of the watch.
	window int
	// model is the watched run's bound model, the yardstick its sub-agent rows' model cells are
	// measured against ([transcript.applyUsage]): from the last depth-0, non-maintenance usage line
	// that named one, else the journal's opening frame (its model), else the record's Meta.Model as
	// the opening frame read it; "" until one does, the watcher's own model standing in meanwhile
	// ([Model.topLevelModel]). It is the watched
	// run's, so it lives here and never in m.opts, and leaveWatch drops it with the rest of the watch.
	model string
	// gitSnap is the watched workspace's last good git snapshot, meaningful only when hasGitSnap:
	// the status chain reads the watched workspace while watching ([Model.gitRoot]) and folds here
	// ([Model.keepGitSnapshot]), so the host's own snapshot (gitState.snap) stays as its last read
	// left it. Its entry lists are nil, and leaveWatch drops it with the rest of the watch.
	gitSnap    gitstatus.Snapshot
	hasGitSnap bool
}

// journalCursor is where the follower stands in the journal. offset is how many journal bytes have
// been folded; journal is the descriptor the offset counts in, held open across reads so that a
// journal the owner removes on release still drains to its end, and so that a different file under
// the same path reads as a truncation. It is a pointer, shared by every copy of the Model, and only
// Update closes it (closeJournal). head is the journal's first complete line, newline included, as
// the read from offset 0 found it: an owner reopening the record truncates the same file
// (Store.OpenJournal), and its new run_started — unique per open through its time — no longer
// matching head is what tells a journal regrown past offset within one poll from one that only grew.
// head is shared by every copy of the Model too, so it is cloned on capture and never mutated
// (ADR 0011).
type journalCursor struct {
	offset  int64
	journal *os.File
	head    []byte
}

// watchPollMsg asks the Model to read the journal once more; gen names the watch that asked.
type watchPollMsg struct{ gen int }

// watchBatchMsg is one follower read: the complete lines appended since the last one, decoded, and
// the cursor the next read starts from. isTruncated says the journal shrank, was replaced or was
// reopened, so the view clears before items fold. isOwnerGone says the read's probe found the hold
// free — the owner has ended, and items are the last the journal will ever hold.
type watchBatchMsg struct {
	gen int
	journalCursor
	isTruncated bool
	isOwnerGone bool
	items       []watchItem
}

// watchItem is one decoded journal line the watch folds: exactly one of its members is set.
type watchItem struct {
	start      *watchStart
	prompt     *eventjson.Prompt
	event      domain.Event
	isFinished bool
}

// watchStart is a journal's opening frame resolved against the record: the title, the record's
// first `entries` entries ready to replay, the record's last observed context fill
// (Meta.CtxUsed), which lights the gauge before any journal usage line folds, the watched run's
// bound model — the frame's, else the record's (Meta.Model) — which measures the watched
// sub-agents until a journal usage line names one, and the frame's context window, the gauge's
// limit until a journal usage line names one (err when the entries were wanted and would not
// read). model and window are "" and 0 when neither the frame nor the record names them.
type watchStart struct {
	title   string
	entries []entry
	ctxUsed int
	model   string
	window  int
	err     error
}

// enterWatch turns the view into a watch of src and returns the follower's first read. It is
// called at idle only: a running worker or an open decision pane owns the view, and the call is
// refused (the Model returned unchanged) rather than parking a session mid-Exchange.
//
// The inert engine's getters answer what the owner's answered at entry, so the footer's
// confinement word and effort segment do not move across the swap. The git status chain moves to
// the watched workspace with it ([Model.retargetGit]): the Cmd carries that read beside the
// follower's.
func (m Model) enterWatch(src watchSource) (Model, tea.Cmd) {
	if !m.canWatch() {
		return m, nil
	}
	m, follow := m.startWatch(src)
	gitRead := m.retargetGit()
	return m, tea.Batch(follow, gitRead)
}

// canWatch reports whether a watch may start now: none is active and the session is idle.
func (m Model) canWatch() bool {
	return !m.watch.isActive && m.state == stateIdle
}

// startWatch is enterWatch past its gate and short of the git chain: the swap into the watch view,
// returning the follower's first read.
func (m Model) startWatch(src watchSource) (Model, tea.Cmd) {
	override, profile := m.eng.ThinkingEffort()
	inert := inertEngine{
		isConfined:     m.eng.ConfineToWorkspace(),
		effortOverride: override,
		effortProfile:  profile,
		isInExchange:   m.eng.InExchange(),
	}
	m.watch = watchState{
		isActive:      true,
		gen:           m.watch.gen + 1,
		source:        src,
		title:         stripEscapes(src.title),
		owner:         m.eng,
		ownerSessions: m.sessions,
		parked:        m.sessionView,
	}
	m.eng, m.sessions = inert, nil
	// The block-paint cache is keyed by entry index (paintcache.go). The parked transcript's is
	// cleared on the way in, as on the way out, and the watch paints through a cache of its own,
	// so an owner Msg folded with the parked view swapped in (foldForOwner) never paints into the
	// cache the watch reads, nor the watch into the owner's. The cache is replaced on the live copy
	// only, after the park: the parked view keeps the owner's.
	m.transcript.paints.clear()
	m.transcript.paints = newPaintCache()
	m.clearWatchView()
	if src.journalPath == "" {
		gen := m.watch.gen
		return m, func() tea.Msg { return readRecordOnly(src, gen) }
	}
	read := m.readJournalCmd()
	return m, read
}

// watchSourceOf is the source a watch of live follows: its record, its journal, its workspace, its
// probe, and a session of a watched home marked foreign (ADR 0103 decision 7). title is what the box
// shows until the journal names one.
func watchSourceOf(live LiveSession, title string) watchSource {
	return watchSource{
		id:          live.ID,
		recordPath:  live.RecordPath,
		journalPath: live.JournalPath,
		title:       title,
		workspace:   live.Workspace,
		isForeign:   live.Home != "",
		probe:       live.Probe,
	}
}

// readRecordOnly is the one read of a watch with no journal — a finished session of a watched home
// (ADR 0103 decision 7): the whole record, painted from its path as an opening frame claiming every
// entry would paint it, and then the run's end, so the follow stops at once with the "finished"
// banner and nothing is ever polled or probed. A record that will not read paints the watch's
// unreadable note in its place.
func readRecordOnly(src watchSource, gen int) watchBatchMsg {
	entries, ctxUsed, model, err := recordHead(src.recordPath, math.MaxInt)
	return watchBatchMsg{
		gen:         gen,
		isOwnerGone: true,
		items: []watchItem{
			{start: &watchStart{entries: entries, ctxUsed: ctxUsed, model: model, err: err}},
			{isFinished: true},
		},
	}
}

// enterStartupWatch is the --resume half of entering a watch: newModel calls it last, over the fresh
// session it has just built, when the binary resolved --resume to a session another instance is
// running, or to one of a watched home (Options.Watch). The follower's first read is kept for Init
// to send. The box names the
// journal frame's title, else the session id — the record that might name one is the owner's, and
// this run does not load it. A nil live, or a start the watch refuses, leaves the fresh session.
//
// No git read rides in startupWatchRead: Init's own status read, issued on the generation newModel
// armed, reads [Model.gitRoot] — the watched workspace by then — so it IS the watched read and one
// chain runs. Init cannot mark it in flight (a value receiver), so the mark newModel set is
// re-derived here: a watch of an unknown workspace issues no read and leaves the chain idle.
func (m *Model) enterStartupWatch(live *LiveSession) {
	if live == nil || !m.canWatch() {
		return
	}
	next, read := m.startWatch(watchSourceOf(*live, cmp.Or(live.Title, live.ID)))
	*m = next
	m.startupWatchRead = read
	m.git.inFlight = m.git.inFlight && m.gitStatusCmd() != nil
}

// leaveWatch is every way out of a watch — esc, ⏎-resume and quit before they act: the owner's
// engine, session host and session view come back exactly as parked, the journal's descriptor is
// closed, and every follower message still in flight — its probe included — is dropped by the
// watch it names. redelivered delivers the owner's held Msgs again, so they fold into the restored
// session; it is nil when nothing was held or no watch was active. gitRead is the host workspace's
// status read, issued at once on a fresh generation so a watched read or tick still in flight is
// inert ([Model.retargetGit]); it is nil unwired or when no watch was active. The two are kept
// apart so a caller that sequences work after the redelivery (resumeWatched) never holds it behind
// a git read.
func (m Model) leaveWatch() (left Model, redelivered, gitRead tea.Cmd) {
	if !m.watch.isActive {
		return m, nil, nil
	}
	held := m.watch.held
	closeJournal(m.watch.journal)
	m.eng, m.sessions = m.watch.owner, m.watch.ownerSessions
	m.swapSessionView(m.watch.parked)
	m.watch = watchState{gen: m.watch.gen}
	// The block-paint cache is keyed by entry index, and the indices now mean the restored
	// transcript's entries again, not the watched one's (paintcache.go).
	m.transcript.paints.clear()
	gitRead = m.retargetGit()
	return m, redeliver(held), gitRead
}

// swapSessionView puts v in place as the session view the Model renders and returns the one it
// displaced. It is the one way a session view changes hands — into a watch (clearWatchView), out
// of it (leaveWatch) and around an owner's fold (foldForOwner) — and it always forgets the painted
// frame, so no swap can leave the other view's lines on screen (forgetPaintedFrame). It touches no
// engine, session host or paint cache: those are the caller's, in the order its own rules set.
func (m *Model) swapSessionView(v sessionView) sessionView {
	displaced := m.sessionView
	m.sessionView = v
	m.forgetPaintedFrame()
	return displaced
}

// forgetPaintedFrame makes Update's tail lay out the session view a swap just put in place. The
// frame key settle compares names the transcript by its generation, and the parked and the watched
// transcript count generations apart, so an equal number would not mean an equal frame; with no
// frame on record as painted, the tail repaints whatever view the swap left (settle, paintcache.go).
func (m *Model) forgetPaintedFrame() {
	m.painted = frameKey{}
}

// redeliver is the Cmd that hands msgs back to the Update loop, in order, once the watch that held
// them has ended.
func redeliver(msgs []tea.Msg) tea.Cmd {
	if len(msgs) == 0 {
		return nil
	}
	cmds := make([]tea.Cmd, 0, len(msgs))
	for _, msg := range msgs {
		cmds = append(cmds, func() tea.Msg { return msg })
	}
	return tea.Sequence(cmds...)
}

// watchRoute names where a Msg that arrives while watching goes.
type watchRoute int

const (
	// watchRouteView: the watch view's own Msg — the follower's, input, the frame, a widget's — folds
	// as it would outside a watch.
	watchRouteView watchRoute = iota
	// watchRouteOwner: a Msg of the watcher's own session folds into the parked view, with that view,
	// its engine and its session host swapped in (foldForOwner).
	watchRouteOwner
	// watchRouteHeld: an owner Msg whose fold opens a surface over the view or switches the session
	// waits for the watch to end (leaveWatch delivers it again).
	watchRouteHeld
)

// watchRouteOf routes msg while watching. Every Msg type [Model.fold] has a case for is named below
// — TestWatchIsolatesRoutesEveryFoldedMsg fails until a new one is — and anything else is a widget's
// own Msg, which belongs to the input box and so to the view.
//
// A footer git action is the owner's: no action starts while watching (acceptPicker refuses every
// row), so one that lands began before the watch, its `git <verb>…` note sits in the parked
// transcript, and its fold releases the owner's engine for a stashed rebind. A branch listing that
// landed opens the branch picker over the view, so it waits for the watch like any other surface.
//
// The git status chain's Msgs fold as the view's: while watching, the chain reads the watched
// workspace and its fold keeps that snapshot in the watch's own slot ([Model.keepGitSnapshot]); a
// read of the host root issued before the watch carries a retired generation and is inert.
func watchRouteOf(msg tea.Msg) watchRoute {
	switch msg := msg.(type) {
	case watchPollMsg, watchBatchMsg,
		tea.WindowSizeMsg, tea.ModeReportMsg, tea.ColorProfileMsg, tea.KeyboardEnhancementsMsg,
		uv.UnknownOscEvent, uv.PrimaryDeviceAttributesEvent,
		tea.FocusMsg, tea.BlurMsg,
		tea.KeyPressMsg, tea.PasteMsg, clipboardImageMsg, clipboardTextMsg,
		ctrlCResetMsg, escStopResetMsg, tea.CursorPositionMsg,
		tea.MouseWheelMsg, tea.MouseClickMsg, tea.MouseMotionMsg, tea.MouseReleaseMsg,
		spinnerTickMsg, skillHintTickMsg, flashClearMsg, dragScrollTickMsg,
		gitStatusMsg, gitStatusTickMsg, gitFetchMsg, gitFetchTickMsg:
		return watchRouteView
	case sessionListMsg, sessionLoadedMsg, workflowsListMsg, workflowItemMsg,
		settingsEditedMsg, settingsDetachedMsg, approvalReqMsg, approvalArmedMsg, askReqMsg:
		return watchRouteHeld
	case eventMsg, scheduleEventMsg, routingNoticeMsg, hookNoticeMsg, presentedMsg,
		exchangeDoneMsg, cancelledMsg, errMsg, compactDoneMsg, interjectedMsg, turnSnapshotMsg,
		saveDoneMsg, recordWriteDoneMsg, autoTitleMsg, manualTitleMsg, configChangedMsg,
		workflowStoppedMsg, workflowSavedMsg, bgStartedMsg, workflowNoteLostMsg,
		skillsReloadedMsg, skillsRescannedMsg, recallLoadedMsg, updateCheckedMsg,
		beatMsg, actuationMsg, restoreMsg, heartbeatTickMsg:
		return watchRouteOwner
	case gitActionDoneMsg:
		if msg.listing && msg.err == nil {
			return watchRouteHeld
		}
		return watchRouteOwner
	}
	return watchRouteView
}

// foldWhileWatching is Update's watch-mode door: an owner Msg folds into the parked session or is
// held, and isOwners reports that it was taken; a view Msg is left to the ordinary fold.
func (m Model) foldWhileWatching(msg tea.Msg) (next tea.Model, cmd tea.Cmd, isOwners bool) {
	switch watchRouteOf(msg) {
	case watchRouteOwner:
		next, cmd = m.foldForOwner(msg)
		return next, cmd, true
	case watchRouteHeld:
		// Rebuilt rather than appended in place, so the Model copy this Update started from never
		// sees the change (ADR 0011).
		m.watch.held = append(slices.Clip(m.watch.held), msg)
		return m, nil, true
	}
	return m, nil, false
}

// foldForOwner folds an owner Msg with the owner's session swapped in: its engine, its session host
// and its parked view take the watch's place for the one fold, so a background workflow's event
// routes by the owner's workflows, a save carries the owner's transcript to the owner's record, and
// a title, a rebind or a config reload lands on the owner's state — exactly as without the watch.
// Then the result is parked again and the watch view comes back, for Update's tail to repaint.
//
// The watch stays active throughout, so the engine holds it takes (holdWatching) still keep a wake,
// a resume and every waiting pane for after the watch, both inside this fold and in Update's tail.
func (m Model) foldForOwner(msg tea.Msg) (tea.Model, tea.Cmd) {
	inert := m.eng
	m.eng, m.sessions = m.watch.owner, m.watch.ownerSessions
	watched := m.swapSessionView(m.watch.parked)

	next, cmd := m.fold(msg)
	folded := next.(Model) // every arm of fold returns the Model it folded

	folded.watch.owner, folded.watch.ownerSessions = folded.eng, folded.sessions
	folded.eng, folded.sessions = inert, nil
	folded.watch.parked = folded.swapSessionView(watched)
	return folded, cmd
}

// clearWatchView empties the view for a (re)paint of the watched session: an empty transcript under
// the same display preferences (transcript.reset keeps them), empty boards and accounting, and the
// watched run idle until its journal says otherwise; every other field of the view starts at its
// zero value. The parked copy is untouched — every reset below replaces a slice or map rather than
// editing it in place.
func (m *Model) clearWatchView() {
	fresh := m.transcript
	fresh.reset()
	boards := m.thinking
	boards.reset()
	m.swapSessionView(sessionView{transcript: fresh, thinking: boards, state: stateIdle})
	m.reroot()
}

// readJournalCmd is the follower's next read, run off the Update goroutine. Every
// watchProbeEvery-th read, the first included, probes the owner's hold before it reads.
func (m *Model) readJournalCmd() tea.Cmd {
	isProbing := false
	if m.watch.readsUntilProbe == 0 {
		isProbing = m.watch.source.probe != nil
		m.watch.readsUntilProbe = watchProbeEvery
	}
	m.watch.readsUntilProbe--
	src, gen, cursor := m.watch.source, m.watch.gen, m.watch.journalCursor
	return func() tea.Msg { return readJournal(src, gen, cursor, isProbing) }
}

// nextWatchPoll schedules the follower's next read.
func (m Model) nextWatchPoll() tea.Cmd {
	gen := m.watch.gen
	return tea.Tick(watchPollInterval, func(time.Time) tea.Msg { return watchPollMsg{gen: gen} })
}

// readJournal reads the complete lines appended to src's journal past cursor's offset and decodes
// them, and returns them with the cursor the next read starts from. It
// touches the journal only — open, stat, read — and never takes the record's lock (ADR 0103
// decision 5); a probing read asks src.probe first, which holds nothing either.
//
// The probe goes before the read: the owner removes its journal and only then releases its hold
// (ADR 0103 decision 4), so a hold read free means every line is written, and the read after it
// drains the journal to its end through the descriptor the follower holds — the path may already
// name nothing. A path that names nothing is the owner releasing, never a truncation.
//
// A journal that shrank below offset, a different file now under the path (a later run wrote a new
// one), or one that no longer opens with the cursor's head (the owner reopened the record, which
// truncates its journal, and wrote past offset again before this read) restarts the read at 0 and
// reports isTruncated; the read from 0 captures the head afresh. A trailing line the owner has not
// finished writing is left for the next read. A journal that will not open or read yields a batch
// with no items at the same cursor: the follower simply looks again on its next poll, which is all
// a reader of a file it does not own can do.
func readJournal(src watchSource, gen int, cursor journalCursor, isProbing bool) watchBatchMsg {
	batch := watchBatchMsg{gen: gen, journalCursor: cursor}
	if isProbing {
		batch.isOwnerGone = isHoldFree(src.probe)
	}
	journal, isReplaced := followedJournal(src.journalPath, cursor.journal)
	if journal == nil {
		return batch
	}
	batch.journal = journal
	info, err := journal.Stat()
	if err != nil {
		return batch
	}
	offset := cursor.offset
	isTruncated := isReplaced || info.Size() < offset
	if !isTruncated && offset > 0 && cursor.head != nil {
		isReopened, err := isHeadChanged(journal, cursor.head)
		if err != nil {
			return batch
		}
		isTruncated = isReopened
	}
	if isTruncated {
		batch.isTruncated = true
		offset = 0
	}
	batch.offset = offset

	data, err := io.ReadAll(io.NewSectionReader(journal, offset, info.Size()-offset))
	if err != nil {
		return batch
	}
	end := bytes.LastIndexByte(data, '\n')
	if end < 0 {
		return batch
	}
	if offset == 0 {
		batch.head = bytes.Clone(data[:bytes.IndexByte(data, '\n')+1])
	}
	batch.offset = offset + int64(end+1)
	for _, raw := range bytes.Split(data[:end], []byte{'\n'}) {
		if item, ok := decodeWatchLine(raw, src); ok {
			batch.items = append(batch.items, item)
		}
	}
	return batch
}

// isHeadChanged reports whether journal no longer opens with head, its first line as the follower
// last read it from offset 0. A journal now shorter than head (a short read at EOF) has changed; any
// other read error is returned, so the caller keeps its cursor and looks again on the next poll.
func isHeadChanged(journal *os.File, head []byte) (bool, error) {
	got := make([]byte, len(head))
	n, err := journal.ReadAt(got, 0)
	if n < len(head) {
		if errors.Is(err, io.EOF) {
			return true, nil
		}
		return false, err
	}
	return !bytes.Equal(got, head), nil
}

// isHoldFree asks probe whether the owner has let the session go. A probe that fails answers
// "still held": the watch stays live and asks again within a second, rather than telling the
// watcher an owner ended that may be running still.
func isHoldFree(probe holdProbe) bool {
	isHeld, err := probe()
	return err == nil && !isHeld
}

// followedJournal is the journal the follower reads: held, while the path still names that file or
// names nothing — the owner removed it on release, and the open descriptor is what drains the rest
// — and otherwise the file now under the path, opened fresh, with isReplaced reporting the switch.
// It is nil while there is no journal to read yet. The descriptor it replaces is left for Update to
// close (foldWatchBatch), which owns every descriptor the watch holds.
func followedJournal(path string, held *os.File) (journal *os.File, isReplaced bool) {
	current, err := os.Stat(path)
	switch {
	case held != nil && (err != nil || isSameFile(held, current)):
		return held, false
	case held == nil && err != nil:
		return nil, false
	}
	fresh, err := os.Open(path)
	if err != nil {
		return held, false
	}
	return fresh, held != nil
}

// isSameFile reports whether the open journal is the file info describes.
func isSameFile(journal *os.File, info os.FileInfo) bool {
	held, err := journal.Stat()
	return err == nil && os.SameFile(held, info)
}

// closeJournal closes a descriptor the watch held. The journal was only ever read, so a failed
// close loses nothing and is not reported; a descriptor closed twice — a dropped batch carrying
// one leaveWatch already closed — answers os.ErrClosed, which is the same nothing.
func closeJournal(journal *os.File) {
	if journal != nil {
		_ = journal.Close()
	}
}

// decodeWatchLine decodes one journal line into the item the watch folds. A line that does not
// decode — torn, malformed, or stamped with a newer `v` — and a kind this build does not know are
// skipped, the consumer's leniency ADR 0075 D10 grants: one bad line never stops the follow.
func decodeWatchLine(raw []byte, src watchSource) (watchItem, bool) {
	line, err := eventjson.Decode(raw)
	if err != nil {
		return watchItem{}, false
	}
	switch {
	case line.Started != nil:
		start := loadWatchStart(raw, src.recordPath)
		return watchItem{start: &start}, true
	case line.Prompt != nil:
		return watchItem{prompt: line.Prompt}, true
	case line.Finished != nil:
		return watchItem{isFinished: true}, true
	case line.Event != nil:
		return watchItem{event: line.Event}, true
	}
	return watchItem{}, false
}

// journalStartedLine is the opening frame's `data` read back whole: eventjson.Decode reads a
// run_started into the headless frame, which has no title or entries, so the watch reads the
// journal's own frame (eventjson.JournalStarted) off the raw line.
type journalStartedLine struct {
	Data eventjson.JournalStarted `json:"data"`
}

// loadWatchStart resolves a journal's opening frame: its title, the binding it names (model and
// context_window, read at head 0 too), the record's fill, and the record's first `entries`
// entries, decoded and with every call the record stored open closed, as a resume closes them
// (closeInterruptedCalls) — but with none of a resume's notices. The frame's model outranks the
// record's; a frame without the members (an older apogee's journal) reads as naming neither. The
// record is read by path, read-only; a store is never opened for it.
func loadWatchStart(raw []byte, recordPath string) watchStart {
	var frame journalStartedLine
	if err := json.Unmarshal(raw, &frame); err != nil {
		return watchStart{err: fmt.Errorf("watch: journal frame: %w", err)}
	}
	start := watchStart{title: frame.Data.Title, window: frame.Data.ContextWindow}
	if frame.Data.Entries > 0 {
		start.entries, start.ctxUsed, start.model, start.err = recordHead(recordPath, frame.Data.Entries)
	}
	if frame.Data.Model != "" {
		start.model = frame.Data.Model
	}
	return start
}

// recordHead reads the record at path and returns its first n transcript entries, its last
// observed context fill (Meta.CtxUsed) and its bound model (Meta.Model).
func recordHead(path string, n int) (entries []entry, ctxUsed int, model string, err error) {
	if path == "" {
		return nil, 0, "", errors.New("watch: no record path")
	}
	record, err := session.NewStore(filepath.Dir(path)).LoadPath(path)
	if err != nil {
		return nil, 0, "", err
	}
	entries, err = decodeTranscript(record.Transcript)
	if err != nil {
		return nil, 0, "", err
	}
	entries = entries[:min(n, len(entries))]
	closeInterruptedCalls(entries)
	return entries, record.Meta.CtxUsed, record.Meta.Model, nil
}

// foldWatchPoll answers the follower's tick with the next read. A watch whose owner has ended
// reads no more.
func (m Model) foldWatchPoll(msg watchPollMsg) (tea.Model, tea.Cmd) {
	if !m.watch.isActive || msg.gen != m.watch.gen || m.watch.ending != watchOwnerLive {
		return m, nil
	}
	read := m.readJournalCmd()
	return m, read
}

// foldWatchBatch folds one follower read into the watch view, in journal order, and schedules the
// next poll — or, when the read's probe found the hold free, ends the follow with the owner's
// banner instead. A batch for another watch, or one arriving after esc, is dropped whole, and the
// descriptor it carries closed.
func (m Model) foldWatchBatch(msg watchBatchMsg) (tea.Model, tea.Cmd) {
	if !m.watch.isActive || msg.gen != m.watch.gen {
		closeJournal(msg.journal)
		return m, nil
	}
	if msg.journal != m.watch.journal {
		closeJournal(m.watch.journal)
	}
	m.watch.journalCursor = msg.journalCursor
	if msg.isTruncated {
		m.clearWatchView()
	}
	wasRunning := m.state == stateRunning
	for _, item := range msg.items {
		m = m.foldWatchItem(item)
	}
	if msg.isOwnerGone {
		m.noteOwnerEnded()
		return m, nil
	}
	cmds := []tea.Cmd{m.nextWatchPoll()}
	if m.state == stateRunning && !wasRunning {
		// One spinner chain per running stretch: a fresh generation retires any chain an earlier
		// stretch in this batch armed (spinnerAnim.arm).
		m.worker.resume()
		cmds = append(cmds, m.spin.arm(m.worker.gen, m.opts.UI.Spinner))
	}
	return m, tea.Batch(cmds...)
}

// noteOwnerEnded ends the follow once the owner has let the session go: the banner it shows says
// whether the journal closed with run_finished, the watched run shows idle, and the drained
// journal's descriptor is closed. The view stays read-only; esc, or ⏎ where resumeWatched allows
// it, is the way on.
func (m *Model) noteOwnerEnded() {
	m.watch.ending = watchOwnerGone
	if m.watch.hasRunFinished {
		m.watch.ending = watchOwnerFinished
	}
	closeJournal(m.watch.journal)
	m.watch.journal = nil
	m.watchIdle()
}

// isWatchResumable reports whether ⏎ resumes the watched record: its owner finished and let it go,
// and it is a session of the watcher's own home. A watched home's session never is (ADR 0103
// decision 7), nor one whose owner vanished without its final save.
func (m Model) isWatchResumable() bool {
	return m.watch.isActive && m.watch.ending == watchOwnerFinished &&
		!m.watch.source.isForeign && m.watch.source.id != ""
}

// foldWatchItem folds one journal line. A prompt paints as the owner's TUI painted it — a top-level
// one as the user block that opens an Exchange, an Interjection as the interjection block — and
// the watched run shows running from a top-level prompt until a depth-0 Turn closes its Exchange or
// the journal closes. Prompt text and the frame's title are untrusted and stripped here; every
// Event's text is stripped by the folds it reaches.
func (m Model) foldWatchItem(item watchItem) Model {
	switch {
	case item.start != nil:
		m.paintWatchStart(*item.start)
	case item.prompt != nil && item.prompt.Interjection:
		m.transcript.addInterjected(stripEscapes(item.prompt.Text), nil)
	case item.prompt != nil:
		m.transcript.addUser(stripEscapes(item.prompt.Text), nil)
		m.watchRunning()
	case item.isFinished:
		m.watch.hasRunFinished = true
		m.watchIdle()
	case item.event != nil:
		m = m.foldWatchedEvent(item.event)
		if exchangeEnded(item.event) {
			m.watchIdle()
		}
	}
	return m
}

// paintWatchStart (re)paints the watched session from its opening frame: the view is cleared and
// the record's head replayed directly, with none of replayScrollback's resume notices — the
// session is not being resumed. The gauge relights from the record's fill, as a resume relights it
// (resumeLoaded), until a journal usage line moves it. The frame's window, where it names one, is
// the gauge's limit, and the frame's model, else the record's, measures the watched sub-agents,
// each until a journal usage line names another (watchState.window, watchState.model); a frame
// naming neither leaves both as they stand.
func (m *Model) paintWatchStart(start watchStart) {
	m.clearWatchView()
	m.ctxUsed = start.ctxUsed
	if start.model != "" {
		m.watch.model = start.model
	}
	if start.window > 0 {
		m.watch.window = start.window
	}
	m.watch.hasRunFinished = false
	if title := stripEscapes(start.title); title != "" {
		m.watch.title = title
	}
	if start.err != nil {
		m.transcript.addEphemeralNote(watchRecordUnreadableNote)
		return
	}
	m.transcript.replay(start.entries)
}

// foldWatchedEvent is the eventMsg case's routing for a journal Event: a background workflow's
// events go to its own state, everything else through the conversation fold. It leaves out what
// the live case adds for the owner's own run — the progress save, the stall clock, the mouse
// re-assert and the decision-pane withdrawal — since the watcher saves nothing and runs nothing.
// What it adds is the watched binding: a usage reading that moves the top-level gauge (depth 0, not
// maintenance) and names its window makes that window the gauge's limit for the rest of the watch,
// over the one the opening frame named, and one that names its model makes that model the watched
// run's, over the frame's or the record's. The conversation fold is handed
// that pair (topLevelWindow, topLevelModel), so the watched run's sub-agent rows are measured
// against the run they belong to rather than against the watcher's own binding. Only this routing
// hands it: an owner Msg folded mid-watch (foldForOwner) reaches foldEvent, which keeps m.opts.
// A background workflow child's switch of the session's own seat is noted as the live case notes
// it, and followed nowhere: the server it names is not this process's.
func (m Model) foldWatchedEvent(e domain.Event) Model {
	if m.workflows.owns(e) {
		m = m.foldBackgroundEvent(e)
		m.noteWorkflowSessionSwitch(e)
		return m
	}
	if usage, ok := e.(domain.UsageEvent); ok && usage.Depth == 0 && !usage.Maintenance {
		if usage.ContextWindow > 0 {
			m.watch.window = usage.ContextWindow
		}
		if usage.Model != "" {
			m.watch.model = usage.Model
		}
	}
	return m.foldEventAgainst(e, m.topLevelWindow(), m.topLevelModel())
}

// exchangeEnded reports whether e closes the watched top-level Exchange: a depth-0 Turn that ended
// it, was cancelled, or faulted.
func exchangeEnded(e domain.Event) bool {
	turn, ok := e.(domain.TurnEvent)
	if !ok || turn.Depth != 0 {
		return false
	}
	return turn.Status != domain.StatusTurnComplete || turn.Faulted
}

// watchRunning shows the watched run running. markRunning stays the only writer of stateRunning.
func (m *Model) watchRunning() {
	if m.state == stateRunning {
		return
	}
	m.markRunning()
	m.setActivity(runRef{}, actThinking, "")
	m.thinking.commitAll()
}

// watchIdle shows the watched run idle: its activity board falls and its thinking commits, as
// finishWorker does for the watcher's own run.
func (m *Model) watchIdle() {
	m.acts = nil
	m.thinking.commitAll()
	m.lastEsc = time.Time{}
	m.state = stateIdle
}

// watchLegend is the prompt box's invitation while watching: once the owner has ended it carries
// the banner, and in the watcher's own home after a finish the ⏎ that resumes.
func (m Model) watchLegend() string {
	switch {
	case m.isWatchResumable():
		return fmt.Sprintf(watchResumableLegendFormat, m.watch.title, watchFinishedBanner)
	case m.watch.ending == watchOwnerFinished:
		return fmt.Sprintf(watchEndedLegendFormat, m.watch.title, watchFinishedBanner)
	case m.watch.ending == watchOwnerGone:
		return fmt.Sprintf(watchEndedLegendFormat, m.watch.title, watchOwnerGoneBanner)
	}
	return fmt.Sprintf(watchLegendFormat, m.watch.title)
}

// watchRefuses is the one allow-list gate every send and command meets while watching: only a
// verb of the view-only set passes. Outside watch mode it refuses nothing.
func (m Model) watchRefuses(parsed parsedInput) bool {
	if !m.watch.isActive {
		return false
	}
	return parsed.kind != kindCommand || !watchVerbs[parsed.command]
}

// refuseWatchInput is the gate's answer: a flash, and the box left as the human typed it.
func (m Model) refuseWatchInput() (tea.Model, tea.Cmd) {
	cmd := m.showFlash(watchRefusedFlash)
	return m, cmd
}

// inertEngine is the Engine watch mode holds: every method does nothing and returns zero values,
// except the three getters the footer and the send path read, which answer what the watcher's own
// engine answered at entry. A watch therefore cannot reach any engine at all.
type inertEngine struct {
	isConfined     bool
	effortOverride domain.ThinkingEffort
	effortProfile  domain.ThinkingEffort
	isInExchange   bool
}

func (inertEngine) Submit(domain.UserInput) error { return nil }

func (inertEngine) Step(context.Context) (domain.StepResult, error) {
	return domain.StepResult{}, nil
}

func (inertEngine) Snapshot() (domain.Session, error)                 { return domain.Session{}, nil }
func (inertEngine) CutSnapshot(int) (domain.Session, error)           { return domain.Session{}, nil }
func (inertEngine) Interject(context.Context, domain.UserInput) error { return nil }
func (inertEngine) InterjectChild(string, domain.UserInput) error     { return nil }
func (inertEngine) InterjectChildNow(string, domain.UserInput) error  { return nil }
func (inertEngine) StopChild(string) error                            { return nil }
func (inertEngine) ClearContext() error                               { return nil }
func (inertEngine) AbortExchange()                                    {}
func (inertEngine) SettleExchange() bool                              { return false }
func (inertEngine) RestoreSession(domain.Session) error               { return nil }
func (e inertEngine) InExchange() bool                                { return e.isInExchange }

func (inertEngine) ContextFilesReport() domain.ContextFilesReport {
	return domain.ContextFilesReport{}
}

func (inertEngine) ContextEstimate() int                        { return 0 }
func (inertEngine) Compact(context.Context) (bool, error)       { return false, nil }
func (inertEngine) SetMode(domain.Mode)                         {}
func (inertEngine) SetConfineToWorkspace(bool)                  {}
func (inertEngine) SetAllowRules([]domain.AllowRule)            {}
func (e inertEngine) ConfineToWorkspace() bool                  { return e.isConfined }
func (inertEngine) UndoPreview() (undo.Step, bool)              { return undo.Step{}, false }
func (inertEngine) UndoRevert(uint64) (undo.Report, error)      { return undo.Report{}, nil }
func (inertEngine) RedoPreview() (undo.Step, bool)              { return undo.Step{}, false }
func (inertEngine) RedoRevert(uint64) (undo.Report, error)      { return undo.Report{}, nil }
func (inertEngine) UndoNote() string                            { return "" }
func (inertEngine) SetEffortOverride(domain.ThinkingEffort)     {}
func (inertEngine) Wake(context.Context) (bool, error)          { return false, nil }
func (inertEngine) TakeWorkflowNotes() (domain.UserInput, bool) { return domain.UserInput{}, false }
func (inertEngine) WorkflowPrompts() []domain.WorkflowPrompt    { return nil }
func (inertEngine) Workflows() ([]workflow.Info, error)         { return nil, nil }
func (inertEngine) StopWorkflow(string) error                   { return nil }
func (inertEngine) RerunFailed(string) error                    { return nil }
func (inertEngine) KeepWorkflows()                              {}
func (inertEngine) MarkFork()                                   {}
func (inertEngine) ResumeWorkflows() error                      { return nil }

func (e inertEngine) ThinkingEffort() (domain.ThinkingEffort, domain.ThinkingEffort) {
	return e.effortOverride, e.effortProfile
}

func (inertEngine) StartRecipe(context.Context, domain.RecipeLaunch) (string, error) {
	return "", nil
}

func (inertEngine) AnswerWorkflowPrompt(uint64, domain.WorkflowPromptAnswer) bool { return false }
