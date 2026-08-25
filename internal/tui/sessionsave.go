package tui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/session"
)

// ----------------------------------------------------------------------------
// Per-Turn session save pipeline (session-system plan §4)
// ----------------------------------------------------------------------------

// savePayload is one assembled save: the engine snapshot plus the derived metadata and the
// encoded transcript blob the SessionHost persists. It is built at a boundary the Model owns
// (a per-Turn snapshot or an idle boundary) and then either dispatched immediately or coalesced
// (pendingSave) behind an in-flight save.
type savePayload struct {
	sess       domain.Session
	transcript []byte
	title      string
	userMsgs   int
	ctxUsed    int
	usage      session.Usage // the main agent's cumulative token accounting at snapshot time
}

// snapshotPayload assembles a savePayload around a captured engine snapshot: it encodes the
// current transcript (transcriptcodec.go), derives the browsable title from the first user
// message, counts the user messages, and reads the live context fill and usage totals. A transcript that fails to
// encode yields ok=false so the caller drops the save rather than persisting a half-record.
func (m Model) snapshotPayload(sess domain.Session) (savePayload, bool) {
	blob, err := encodeTranscript(&m.transcript)
	if err != nil {
		return savePayload{}, false
	}
	return savePayload{
		sess:       sess,
		transcript: blob,
		title:      sessionTitle(m.transcript.firstUserText()),
		userMsgs:   m.transcript.userMessageCount(),
		ctxUsed:    m.ctxUsed,
		usage:      session.Usage(m.usage),
	}, true
}

// persist builds a savePayload around sess and schedules it, gated on there being a wired host and
// a sent prompt (hasPrompt). It is the entry the per-Turn snapshot (turnSnapshotMsg) and the idle
// finishers both funnel through, so the "worth saving?" gate lives in one place. Those callers only
// run inside a Turn, which only a prompt opens, so the gate never fires here in practice — it is
// carried so that every save in the package answers to the same one predicate. It returns the Cmd
// to run (nil when nothing was scheduled).
func (m *Model) persist(sess domain.Session) tea.Cmd {
	if m.sessions == nil || !m.transcript.hasPrompt() {
		return nil
	}
	p, ok := m.snapshotPayload(sess)
	if !ok {
		return nil
	}
	return m.scheduleSave(p)
}

// saveAtIdle persists the current conversation taking the Model's OWN engine Snapshot — valid
// because every caller is a terminal boundary at which the worker has returned and the Update
// loop owns the engine again (C1). Three callers share it: the idle finisher (finishWorker), the
// clean quit's closing flush, and the /clear|/new close of the outgoing session. Best-effort like
// persist: a Snapshot error, an unwired host, or a transcript that holds no prompt yet simply
// schedules nothing — a launch that only ran slash commands has produced notes and chrome but
// nothing anyone would resume, so quitting it, or clearing it, must not file a record reading 0
// messages (hasPrompt).
//
// The two closing callers used to call the host synchronously instead, on the reasoning that the
// record must be on disk before the program exits or the host rotates. It bought that at the cost of
// a write outside the queue: a Save beside an in-flight Rename or Delete is exactly the collision the
// queue exists to prevent, and a rotate that overtakes its own flush mints a second id for the
// session it was closing. Both now schedule here and wait for the queue instead (quit,
// startNewSession).
func (m *Model) saveAtIdle() tea.Cmd {
	if m.sessions == nil || !m.transcript.hasPrompt() {
		return nil
	}
	sess, err := m.eng.Snapshot()
	if err != nil {
		return nil
	}
	return m.persist(sess)
}

// ----------------------------------------------------------------------------
// The progress save: the boundary snapshot paired with a live transcript
// ----------------------------------------------------------------------------

// cacheBoundary records sess as the latest quiescent-boundary engine snapshot — the engine half a
// progress save pairs with the live transcript (progressSave). It is the one site that FILLS the
// pair — startNewSession is the one that empties it — so every caller holding such a snapshot hands
// it here rather than assigning the fields: the per-Turn fold (turnSnapshotMsg), the idle capture
// each worker launch takes (cacheBoundaryAtIdle), and a restore adopting the record it just put
// into the engine (resumeLoaded).
func (m *Model) cacheBoundary(sess domain.Session) {
	m.boundary = sess
	m.hasBoundary = true
}

// cacheBoundaryAtIdle takes the Model's OWN engine Snapshot and caches it as the boundary a
// progress save will pair with. Every worker launch calls it just before handing the engine over
// (commandrun.go), which is the last moment the Model can see a boundary of its own: from there the
// engine belongs to the worker goroutine until the Turn ends, and the launch's Submit has not run
// yet, so what is cached can never carry pendingInput — a snapshot holding one is unresumable
// (Submit refuses with ErrInputPending, /continue refuses because InExchange is false). It is
// valid for the same reason saveAtIdle's snapshot is: the Update loop owns the engine here (C1).
//
// A Snapshot error leaves the cache exactly as it was rather than dropping it: the previous
// boundary is still a boundary, and pairing a progress save with one Turn's worth of older engine
// state beats writing no progress at all.
func (m *Model) cacheBoundaryAtIdle() {
	sess, err := m.eng.Snapshot()
	if err != nil {
		return
	}
	m.cacheBoundary(sess)
}

// progressSave re-persists the record MID-Turn, and is what keeps a running delegation visible to
// anyone reading the record while it runs (ADR 0022 addendum). It returns nil until a boundary has
// been cached, and otherwise hands the cached snapshot to persist — whose gates (a wired host, a
// sent prompt) stay the whole gate.
//
// The two halves of the record deliberately come from different moments. The engine half is the
// LAST quiescent-boundary snapshot (cacheBoundary): the engine is mid-Step while a delegation runs
// and ADR 0007 admits no snapshot there, so the alternative to an older engine half is none at all.
// The transcript half is live — the assistant message that delegated, the prompt it carried, and
// the child's tool boundaries as they land. A resume therefore re-attempts the open Turn exactly as
// a cancelled one does: the engine resumes from the boundary the Turn started at, the record's open
// tool calls are closed as interrupted at replay, and the unfinished work is not kept.
func (m *Model) progressSave() tea.Cmd {
	if !m.hasBoundary {
		return nil
	}
	return m.persist(m.boundary)
}

// ----------------------------------------------------------------------------
// The session-record write queue
// ----------------------------------------------------------------------------
//
// One conversation writes its record three ways — the per-Turn/idle Save, the Rename that carries a
// title (generated or typed), and the browser's Delete — and each of them runs off the Update loop
// on its own Cmd goroutine. They MUST NOT overlap. Save replaces the record wholesale while Rename
// is a read-modify-write of it, so a Rename that read the record before a Save replaced it writes
// the pre-save version back, reverting the session by a whole Turn — engine state and scrollback
// together — and breaking ADR 0022's "a crash loses at most one Turn" (audit 2026-08-01, probe:
// 25% lost updates). Delete has the same shape against a save already in flight.
//
// So every write goes through ONE queue: scheduleWrite/queueWrite put it in, pumpWrites takes
// exactly one out whenever nothing is running, and every fold that finishes a write ends in
// pumpOrQuit. Two rules follow from that, and both were bugs before it existed: a fold must never
// tea.Batch two writes (batch members run on separate goroutines — this is precisely how the title
// flush came to race the coalesced save), and a write scheduled while the latch is held must WAIT
// rather than dispatch.
//
// Rotate and Activate join them although they write no file at all: they RETARGET the stream, moving
// the active session every later Save resolves against. A save that overtakes a Rotate is written
// under the OLD id and a save it overtakes mints a NEW one; a save that a resume's Activate overtakes
// is written into the RESUMED record, replacing the loaded conversation with the outgoing one.
// Ordering against the same stream is the whole of what either needs. The two closing flushes (quit,
// /clear) queue for the same reason rather than writing through, which is what leaves the exit
// waiting on the drain (pumpOrQuit).
//
// internal/session.Store holds a mutex over the file-writing calls, which is the floor under any
// caller that does not come through here. This layer's job is ordering; the store's is atomicity.
// Which of the two OWNS serialization long term is deliberately still open (C7).

// recordWriteKind names which SessionHost call a queued write makes.
type recordWriteKind int

const (
	writeSave     recordWriteKind = iota // SessionHost.Save — the per-Turn and idle snapshots
	writeRename                          // SessionHost.Rename — a generated title, /rename, the browser's `r`
	writeDelete                          // SessionHost.Delete — the browser's delete verb
	writeRotate                          // SessionHost.Rotate — /clear|/new, and deleting the ACTIVE record, retiring its id
	writeActivate                        // SessionHost.Activate — a /sessions resume adopting the loaded record's id
)

// retargets reports whether this kind moves the active session — which record subsequent Saves
// resolve against — rather than writing the current one. Saves on opposite sides of a retarget
// describe DIFFERENT records, which is why they must not coalesce across one (queueWrite).
func (k recordWriteKind) retargets() bool {
	return k == writeRotate || k == writeActivate
}

// recordWrite is one queued write to the session record: what to do, and what the fold that lands
// afterwards must do about it. Plain values only, so the value-copied Model carries it safely
// (ADR 0011).
type recordWrite struct {
	kind recordWriteKind

	// payload is the assembled save (writeSave only).
	payload savePayload

	// id addresses the record a rename or a delete acts on; title is the new name (writeRename).
	id    string
	title string

	// meta is the loaded record's browsable metadata a resume adopts (writeActivate only). A
	// session.Meta is plain values throughout, so the value-copied Model carries it safely.
	meta session.Meta

	// relist re-reads the store once the write lands, so the /sessions overlay repaints over the
	// result — what the browser's own verbs want, and what the QUIET title apply must not have
	// (foldSessionList opens the overlay over every list it folds, so a generated title would pop
	// the browser open partway through the first answer).
	relist bool

	// retryTitle marks a write whose title must not be lost when it fails: the quiet apply path
	// (setSessionTitle). source records who asked for that title, which the never-clobber rule
	// needs when the fold puts it back on the stash.
	retryTitle bool
	source     titleSource
}

// scheduleSave queues p as a record write and returns the Cmd to run — the save pipeline's entry to
// the shared queue. Returns nil when a write was already running (the save waits, coalescing with
// any save already queued: latest-wins).
func (m *Model) scheduleSave(p savePayload) tea.Cmd {
	return m.scheduleWrite(recordWrite{kind: writeSave, payload: p})
}

// scheduleWrite queues w and dispatches it when nothing else is writing, returning the Cmd to run
// (nil when it had to wait). It is the only way a record write is dispatched from an idle latch;
// folds that already hold the latch queue with queueWrite and end in pumpWrites instead.
func (m *Model) scheduleWrite(w recordWrite) tea.Cmd {
	m.queueWrite(w)
	return m.pumpWrites()
}

// queueWrite puts w in the pending queue, coalescing saves: a save already waiting is REPLACED by
// the newer one (latest-wins — the newer snapshot supersedes the older intermediate Turn), while
// renames, deletes and the two retargets each keep their place, being distinct instructions rather
// than restatements of one. Without a wired host there is no record to write, so nothing is queued.
//
// Coalescing stops at a retarget. A save queued BEFORE a Rotate or an Activate belongs to the
// session that was live then, and one queued after belongs to whatever the retarget made live — two
// different records, so the newer is no restatement of the older. Letting it supersede one across
// that line would write the incoming conversation into the outgoing record and lose the outgoing
// one's closing state entirely. Only a save in the queue's LAST segment can be superseded, which is
// the whole queue whenever no retarget is waiting — the ordinary case, unchanged.
//
// The queue is rebuilt rather than written through, so the Model copy this Update started from
// never sees the change (ADR 0011).
func (m *Model) queueWrite(w recordWrite) {
	if m.sessions == nil {
		return
	}
	supersede := -1
	if w.kind == writeSave {
		for i, q := range m.pendingWrites {
			switch {
			case q.kind.retargets():
				supersede = -1 // a new segment opens; nothing before it describes w's record
			case q.kind == writeSave:
				supersede = i
			}
		}
	}
	next := make([]recordWrite, 0, len(m.pendingWrites)+1)
	for i, q := range m.pendingWrites {
		if i == supersede {
			next = append(next, w)
			continue
		}
		next = append(next, q)
	}
	if supersede < 0 {
		next = append(next, w)
	}
	m.pendingWrites = next
}

// pumpWrites dispatches the head of the queue when no write is in flight, returning its Cmd (nil
// when one is already running or nothing is waiting). Every fold that finishes a write ends here,
// which is what keeps exactly one record write outstanding at a time.
func (m *Model) pumpWrites() tea.Cmd {
	if m.writeBusy || len(m.pendingWrites) == 0 {
		return nil
	}
	w := m.pendingWrites[0]
	m.pendingWrites = m.pendingWrites[1:]
	m.writeBusy = true
	return m.writeCmd(w)
}

// writeCmd builds the Cmd that performs one record write off the Update loop and reports it back —
// saveDoneMsg for a save (which carries the fail/recover notes and the title flush), otherwise
// recordWriteDoneMsg. It captures the SessionHost and the write by value, so the closure holds no
// pointer into the value-copied Model.
func (m Model) writeCmd(w recordWrite) tea.Cmd {
	sessions := m.sessions
	if w.kind == writeSave {
		p := w.payload
		return func() tea.Msg {
			return saveDoneMsg{Err: sessions.Save(p.sess, p.transcript, p.title, p.userMsgs, p.ctxUsed, p.usage)}
		}
	}
	return func() tea.Msg {
		done := recordWriteDoneMsg{write: w}
		switch w.kind {
		case writeRename:
			done.err = sessions.Rename(w.id, w.title)
		case writeDelete:
			done.err = sessions.Delete(w.id)
		case writeRotate:
			sessions.Rotate() // reports nothing: closing a session cannot fail
		case writeActivate:
			sessions.Activate(w.meta) // reports nothing: adopting a loaded id cannot fail
		}
		if w.relist {
			// The re-list rides on the write's own goroutine, so the rows the browser repaints are
			// read AFTER the write that changed them and the fold that lands has everything it needs
			// in one Msg — leaving foldRecordWrite one Cmd to return rather than a tea.Batch, which
			// on this path would be two record writes on two goroutines again.
			metas, err := sessions.List()
			done.list = sessionListMsg{metas: metas, err: err}
		}
		return done
	}
}

// saveComplete folds a finished save: it notes the ok↔fail transition exactly once (on the ok→fail
// edge and the fail→ok recovery edge, then swallows the error — a save failure must never interrupt
// the conversation), queues any session title that was waiting for this record to exist, releases
// the single-flight latch, and dispatches whatever waited behind this save.
//
// The title flush hangs off a SUCCESSFUL save because that is the moment the record is first on
// disk with an id: the naming call (autotitle.go) routinely answers before the first save-complete,
// and Rename — the only writer of a stored title — needs a stored record to rewrite. It runs while
// the latch is still held so that it can only ever QUEUE: batching the title write beside the
// coalesced save is what let a rename roll a Turn off disk.
func (m *Model) saveComplete(err error) tea.Cmd {
	switch {
	case err != nil && !m.saveFailing:
		m.saveFailing = true
		m.transcript.addNote("session save failed: " + err.Error() + " — will keep retrying")
		m.refreshViewport()
	case err == nil && m.saveFailing:
		m.saveFailing = false
		m.transcript.addNote("session saving recovered")
		m.refreshViewport()
	}
	if err == nil {
		m.flushPendingTitle()
	}
	m.writeBusy = false
	return m.pumpOrQuit()
}

// foldRecordWrite folds a finished Rename, Delete, Rotate or Activate: it releases the single-flight
// latch, dispatches whatever waited behind it, and re-lists for the browser verbs that asked to
// repaint over the result. All of them are best-effort — a rename that did not stick leaves the old
// title on the re-list, a delete that did not leaves the row, and neither retarget can fail — so
// nothing is said about a failure.
//
// The one failure that is NOT simply swallowed is a quiet title write. Its apply path branches on
// ActiveID(), which the host mints at the START of the first Save, before the atomic write has put
// the file on disk; a title answering in that window — or any time saves have been failing — renames
// a record that is not there, and used to be discarded on the spot. Re-stashing it applies it at the
// next successful save instead (flushPendingTitle), so the window costs a delay rather than the name.
func (m *Model) foldRecordWrite(msg recordWriteDoneMsg) tea.Cmd {
	if msg.err != nil && msg.write.retryTitle {
		// What may go back on the stash is the stash's own rule (titleStash.restash): a stash already
		// holding something wins, and an automatic title a human has since outranked is dropped
		// rather than retried.
		m.pendingTitle.restash(msg.write.title, msg.write.source, m.titleTouched)
	}
	if msg.write.relist {
		m.foldSessionList(msg.list) // repaint the overlay over the store as the write left it
	}
	m.writeBusy = false
	return m.pumpOrQuit()
}

// pumpOrQuit ends every fold that finished a record write: it dispatches the next write, and when
// the queue has run dry it fires the exit a clean quit was waiting for (quit). A quit deferred while
// a WORKER runs is deliberately NOT fired here — that one belongs to finishWorker, once the
// goroutine has unwound, because exiting first would let the composition root's teardown race a Step
// still in flight (C4). The busy() guard is what keeps the two apart, including when a human quits
// at idle and then sends one more message while the flush is still on its way to disk.
func (m *Model) pumpOrQuit() tea.Cmd {
	if cmd := m.pumpWrites(); cmd != nil {
		return cmd
	}
	if m.quitting && !m.busy() {
		return tea.Quit
	}
	return nil
}

// sessionTitleMax is the longest a derived session title runs before word-boundary truncation
// (apogee-code's MAX_TITLE_LENGTH).
const sessionTitleMax = 50

// sessionTitle derives a browsable one-line title from the first user message's text, ported from
// apogee-code's generateTitle: the first line, returned as-is when it fits, otherwise truncated to
// sessionTitleMax runes at the last word boundary past 60% (falling back to a hard cut) and closed
// with an ellipsis. A message that is empty or opens a code fence has no useful title, so it falls
// back to a dated "Session <date>" — every stored session still gets a human label.
func sessionTitle(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || strings.HasPrefix(trimmed, "```") {
		return "Session " + time.Now().Format("2006-01-02")
	}
	firstLine := trimmed
	if i := strings.IndexByte(trimmed, '\n'); i >= 0 {
		firstLine = trimmed[:i]
	}
	runes := []rune(firstLine)
	if len(runes) <= sessionTitleMax {
		return firstLine
	}
	truncated := string(runes[:sessionTitleMax])
	if lastSpace := strings.LastIndex(truncated, " "); lastSpace > sessionTitleMax*6/10 {
		truncated = truncated[:lastSpace]
	}
	return truncated + "…"
}
