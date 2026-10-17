package eventjson

import (
	"io"
	"sync"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// journalCoalesceWindow is how long adjacent deltas (tokens or reasoning) may accumulate before the
// journal writes them as one line. It mirrors tokenCoalesceWindow (internal/tui/sink.go) by value:
// a watcher painting the journal sees text land at the same cadence the owner's TUI paints it, and
// this package may not import the TUI to share the constant.
const journalCoalesceWindow = 30 * time.Millisecond

// JournalOptions are the facts a Journal cannot derive from the stream it records.
//
// Session, Title and Workspace name the record the journal belongs to, and Entries is how many
// transcript entries that record held when the journal was born — the count a watcher paints from
// the record before it follows the journal (ADR 0103 decision 3). Now and Report are Options' own
// (see Options). Window overrides journalCoalesceWindow; zero means the default. Tests shrink it to
// see the timer flush, or stretch it so only events flush.
type JournalOptions struct {
	Session   string
	Title     string
	Workspace string
	Entries   int
	Report    func(error)
	Now       func() time.Time
	Window    time.Duration
}

// JournalStarted is the `data` of a journal's opening frame. It is the journal's own frame, never
// the headless one: RunStarted's Model, Server and the rest are facts of a Firing's start that a
// journal born at a hold does not hold, and Title and Entries are the journal's alone (ADR 0103
// decision 3), so headless stdout frames do not change.
type JournalStarted struct {
	Session   string `json:"session"`
	Workspace string `json:"workspace"`
	Title     string `json:"title"`
	Entries   int    `json:"entries"`
}

// journalFinished is the `data` of a journal's closing frame: an empty object. The frame itself is
// the fact — the owner ended the run cleanly — and the record, not the journal, carries the outcome.
type journalFinished struct{}

// Journal records a live session's Event stream as its Event journal (ADR 0103): the Event lines in
// the v:2 envelope, with adjacent token and reasoning deltas coalesced over a short window — the
// named, coalesced variant of Event lines. It is a domain.EventSink a Driver installs outermost, the
// way it installs a Writer: cfg.Events = journal.Wrap(cfg.Events).
//
// Coalescing touches the journal's written lines only. Every Event is forwarded to the wrapped sink
// at once and unmerged, from the Emit that received it, so the Driver's other observers see exactly
// the stream the engine emitted and the engine's serialization (agent.serialEventSink) still covers
// every forward; a timer flush writes a line and forwards nothing.
//
// Merging is lossless: deltas of one kind sharing an EventBase (Depth, Turn, CallID and RunID) join
// the pending line, and the merged text is their concatenation. Any other Event, a Prompt and Close
// write the pending line first, so the journal's order is the engine's order; the window's timer
// writes it once the window passes, so a quiet stream still lands within about a window.
//
// Stamping, seq and the stop-at-first-write-error rule are the embedded Writer's: the first write
// error is reported once through Report, every later line is dropped, and forwarding goes on.
type Journal struct {
	lines  *Writer
	window time.Duration
	inner  domain.EventSink

	// mu guards the pending delta and the timer, and covers every line written out of them, so a
	// timer flush can never land between an Emit's flush and its own line. It is always taken
	// before the Writer's own lock, never inside it.
	mu sync.Mutex
	// pending is the accumulated delta text of the open buffer.
	pending string
	// base is the EventBase the pending text belongs to; only a delta sharing it may merge.
	base domain.EventBase
	// isReasoning is the open buffer's kind: reasoning text, or visible tokens. One Turn's two
	// streams share an EventBase, so the kind is part of the merge key.
	isReasoning bool
	// isBuffering says a buffer is open, which an empty pending string cannot: a zero-length delta
	// still writes its line.
	isBuffering bool
	// gen identifies the open buffer, so a timer that lost the race to the lock returns instead of
	// cutting the next buffer's window short.
	gen uint64
	// timer closes the open buffer's window. It is armed by the delta that opens the buffer and
	// never re-armed while the buffer grows, so a continuous stream still lands once per window.
	timer *time.Timer
	// isClosed says Close wrote the closing frame; the journal writes nothing after it.
	isClosed bool
}

// NewJournal builds a Journal over w and writes its opening frame. The caller owns w and closes it
// after Close.
func NewJournal(w io.Writer, o JournalOptions) *Journal {
	window := o.Window
	if window <= 0 {
		window = journalCoalesceWindow
	}
	j := &Journal{
		lines:  New(w, Options{Session: o.Session, Now: o.Now, Report: o.Report}),
		window: window,
	}
	j.lines.writeLine(kindRunStarted, nil, JournalStarted{
		Session:   o.Session,
		Workspace: o.Workspace,
		Title:     o.Title,
		Entries:   o.Entries,
	})
	return j
}

// Wrap installs inner as the sink every Emit is forwarded to and returns the Journal itself. A nil
// inner is legal and means the journal is the only observer.
func (j *Journal) Wrap(inner domain.EventSink) domain.EventSink {
	j.inner = inner
	return j
}

// Emit records one Event and forwards it, unmerged, to the wrapped sink. The forward happens
// whether or not the journal wrote — or will later write — a line for the Event.
func (j *Journal) Emit(ev domain.Event) {
	j.record(ev)

	if j.inner != nil {
		j.inner.Emit(ev)
	}
}

// Prompt writes a prompt line for a top-level prompt or an Interjection (ADR 0103 decision 11),
// after any pending delta: prompts are not Events (ADR 0025), so the Driver writes them here.
func (j *Journal) Prompt(text string, interjection bool) {
	j.mu.Lock()
	defer j.mu.Unlock()

	if j.isClosed {
		return
	}
	j.flushLocked()
	j.lines.writeLine(kindPrompt, nil, Prompt{Text: text, Interjection: interjection})
}

// Close writes any pending delta and then the closing frame, and stops the journal: a later Emit is
// still forwarded but writes nothing. It is safe to call more than once; only the first writes.
func (j *Journal) Close() {
	j.mu.Lock()
	defer j.mu.Unlock()

	if j.isClosed {
		return
	}
	j.flushLocked()
	j.isClosed = true
	j.lines.writeLine(kindRunFinished, nil, journalFinished{})
}

// record buffers a delta, or writes the pending delta and then the Event's own line.
func (j *Journal) record(ev domain.Event) {
	j.mu.Lock()
	defer j.mu.Unlock()

	if j.isClosed {
		return
	}
	switch delta := ev.(type) {
	case domain.TokenEvent:
		j.bufferLocked(delta.EventBase, delta.Text, false)
		return
	case domain.ReasoningEvent:
		j.bufferLocked(delta.EventBase, delta.Text, true)
		return
	}
	j.flushLocked()
	j.lines.Emit(ev)
}

// bufferLocked merges one delta into the open buffer, or writes the open buffer and opens a new one
// when the delta's EventBase or kind differs. The caller holds mu.
func (j *Journal) bufferLocked(base domain.EventBase, text string, isReasoning bool) {
	if j.isBuffering && (j.base != base || j.isReasoning != isReasoning) {
		j.flushLocked()
	}
	if !j.isBuffering {
		j.isBuffering = true
		j.base = base
		j.isReasoning = isReasoning
		j.gen++
		gen := j.gen
		j.timer = time.AfterFunc(j.window, func() { j.closeWindow(gen) })
	}
	j.pending += text
}

// closeWindow writes the buffer whose window just expired. A gen mismatch means an Emit, a Prompt
// or Close already wrote it, and any buffer open now is owned by its own timer.
func (j *Journal) closeWindow(gen uint64) {
	j.mu.Lock()
	defer j.mu.Unlock()

	if j.gen != gen {
		return
	}
	j.flushLocked()
}

// flushLocked writes the open buffer as one token or reasoning line and closes the buffer. It is a
// no-op when nothing is buffered, so every boundary calls it unconditionally. The caller holds mu.
//
// It writes through the Writer's writeLine, never its Emit: the merged Event is the journal's own
// line, not an Event anyone else should observe.
func (j *Journal) flushLocked() {
	if !j.isBuffering {
		return
	}
	var merged domain.Event = domain.TokenEvent{EventBase: j.base, Text: j.pending}
	if j.isReasoning {
		merged = domain.ReasoningEvent{EventBase: j.base, Text: j.pending}
	}
	j.pending = ""
	j.base = domain.EventBase{}
	j.isReasoning = false
	j.isBuffering = false
	if j.timer != nil {
		j.timer.Stop()
		j.timer = nil
	}
	kind, base, data, _ := Encode(merged)
	j.lines.writeLine(kind, &base, data)
}
