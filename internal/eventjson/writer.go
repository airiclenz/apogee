package eventjson

import (
	"bufio"
	"encoding/json"
	"io"
	"sync"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// lineVersion is the `v` every line carries (ADR 0075 decision 10). It is stamped per LINE rather
// than announced once in the opening frame because JSONL is tailed, split, grepped and merged
// across runs: in all four cases a version living in a frame the consumer never saw is no version
// at all. New kinds and new `data` members are additive within v:1; a removal, a rename or a
// changed meaning bumps it.
const lineVersion = 1

// Options are the facts a Writer cannot derive from the stream it renders.
//
// Session is the run's id when the Driver already holds one at construction; a Driver that mints
// the id later leaves it empty and calls SetSession. Now supplies the `time` stamp and defaults to
// time.Now — a test pins it to make whole lines comparable. Report receives the FIRST write error
// and nothing after it, so a Driver spends exactly one stderr line on a closed pipe; it may be nil,
// which discards that report. Report is called while the Writer's own lock is held and must not
// call back into the Writer.
type Options struct {
	Session string
	Now     func() time.Time
	Report  func(error)
}

// Writer renders the engine's Event stream as the Event lines — one JSON line per Event on the
// io.Writer the Driver owns, bracketed by the two frames (ADR 0075). It is a domain.EventSink, so
// a Driver installs it as Config.Events; Wrap gives it the sink it displaces, and every Event it
// receives is forwarded there whether or not a line was written for it.
//
// Its place in the sink chain is the OUTERMOST wrapper, never inside a hooks.Runner: writing is
// lossless and therefore blocking (decision 9), while a Runner's Report callback is documented
// must-not-block. The engine serializes emission for it (agent.serialEventSink), and the Writer
// takes its own lock anyway so that a frame written by the Driver's goroutine can never interleave
// with an Event line.
//
// Losslessness has one boundary: the FIRST write error stops the stream. Report is called once,
// every later line is dropped silently, and the run continues to its own end — a run that has
// already edited files is not half-killed because a reader closed its pipe. Forwarding to the
// inner sink survives that stop; the Driver's other observers are not collateral damage.
//
// A Writer is not safe to construct once and share across runs: seq is per-stream and starts at 1.
type Writer struct {
	out    *bufio.Writer
	now    func() time.Time
	report func(error)

	mu      sync.Mutex
	session string
	seq     int
	stopped bool

	inner domain.EventSink
}

// New builds a Writer over w. Nothing is written until the first Emit or frame, so a Driver may
// construct one before it knows whether the run will start.
func New(w io.Writer, o Options) *Writer {
	now := o.Now
	if now == nil {
		now = time.Now
	}
	return &Writer{out: bufio.NewWriter(w), now: now, report: o.Report, session: o.Session}
}

// Wrap installs inner as the sink every Emit is forwarded to and returns the Writer itself, so a
// Driver reads the composition as one expression: cfg.Events = lines.Wrap(cfg.Events). Passing a
// nil inner is legal and means the lines are the only observer.
func (w *Writer) Wrap(inner domain.EventSink) domain.EventSink {
	w.inner = inner
	return w
}

// SetSession binds the run's session id to every line written from now on. Lines already written
// keep the null they were stamped with: the id is a fact that became true partway through the
// stream, and back-dating it would misreport when the run acquired an identity.
func (w *Writer) SetSession(id string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.session = id
}

// Emit writes one Event as a line and forwards it to the wrapped sink.
//
// A domain.WireEvent is forwarded only: it is raw provider protocol and is excluded from the
// contract (decision 2), and per the same decision it consumes no sequence number — a gap in seq
// means a lost line, never an Inspector event. The forward happens for every Event, including one
// whose line was dropped because the stream had already stopped.
func (w *Writer) Emit(ev domain.Event) {
	if kind, base, data, ok := Encode(ev); ok {
		w.writeLine(kind, &base, data)
	}

	if w.inner != nil {
		w.inner.Emit(ev)
	}
}

// RunStarted writes the opening frame. It is not an Event — no engine moment produces it — and it
// consumes a seq like every other line.
func (w *Writer) RunStarted(frame RunStarted) {
	w.writeLine(kindRunStarted, nil, frame)
}

// RunFinished writes the closing frame. Exactly one is written on every exit path (decision 5), so
// a consumer never has to interpret a stream that simply stopped.
func (w *Writer) RunFinished(frame RunFinished) {
	w.writeLine(kindRunFinished, nil, frame)
}

// envelope is the line itself. Field order IS the key order of the contract
// (`event, v, seq, time, session, turn, depth, call_id, data`), because encoding/json marshals a
// struct in declaration order — so this declaration is the thing a consumer's fixture pins.
//
// The three pointers are how "always present, null where there is no value" is spelled: a frame
// belongs to no Turn and no agent, and an Event emitted at Depth 0 was spawned by no call. A
// consumer therefore tests for a null value and never for a missing key.
type envelope struct {
	Event   string  `json:"event"`
	V       int     `json:"v"`
	Seq     int     `json:"seq"`
	Time    string  `json:"time"`
	Session *string `json:"session"`
	Turn    *int    `json:"turn"`
	Depth   *int    `json:"depth"`
	CallID  *string `json:"call_id"`
	Data    any     `json:"data"`
}

// writeLine stamps one envelope and writes it, flushing so a reader tailing the pipe sees the line
// the moment the moment happened. base is nil for the two frames, which carry turn, depth and
// call_id as null.
func (w *Writer) writeLine(kind string, base *domain.EventBase, data any) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.stopped {
		return
	}

	w.seq++
	env := envelope{Event: kind, V: lineVersion, Seq: w.seq, Time: w.now().Format(time.RFC3339Nano), Data: data}
	if w.session != "" {
		session := w.session
		env.Session = &session
	}
	if base != nil {
		turn, depth := base.Turn, base.Depth
		env.Turn, env.Depth = &turn, &depth
		if base.CallID != "" {
			callID := base.CallID
			env.CallID = &callID
		}
	}

	line, err := json.Marshal(env)
	if err != nil {
		// The only member that can refuse to marshal is a verbatim argument blob the model
		// wrote (decision 11): a ToolCallEvent is emitted before anything parses those bytes,
		// so malformed JSON reaches here as a matter of course on a small model. Drop THIS
		// line's data rather than the rest of the run's stream — the envelope still lands, so
		// the moment, its seq and its identity are all still reported.
		env.Data = nil
		if line, err = json.Marshal(env); err != nil {
			w.fail(err)
			return
		}
	}

	if _, err := w.out.Write(append(line, '\n')); err != nil {
		w.fail(err)
		return
	}
	if err := w.out.Flush(); err != nil {
		w.fail(err)
	}
}

// fail records the first write error and silences the stream. The caller holds the lock, so the
// stopped flag and the single Report are indivisible: a concurrent emitter either reports nothing
// or sees a stopped stream.
func (w *Writer) fail(err error) {
	w.stopped = true
	if w.report != nil {
		w.report(err)
	}
}

// The two frames are not Events and are owned here rather than by the Driver: this package must
// not import internal/run (which may one day import it), so the frames are typed structs the
// Driver fills from whatever it holds.

// RunStarted is the `data` of the opening frame: what the run was asked to be, before it did
// anything. Session repeats the envelope's id because ADR 0075 decision 5 lists it among the
// frame's own members — a consumer reading only the frames reads one object, not an object plus
// its envelope.
//
// Every member is the Driver's to fill: this package never reaches into a Config to compose one,
// so it can stay free of internal/run (which may one day import it) and of the host's flag layer.
type RunStarted struct {
	Session   string `json:"session"`
	Workspace string `json:"workspace"`
	Model     string `json:"model"`
	Server    string `json:"server"`
	Mode      string `json:"mode"`
	Bypass    bool   `json:"bypass"`
	Confined  bool   `json:"confined"`
	Version   string `json:"version"`
}

// RunFinished is the `data` of the closing frame: the whole outcome, on every exit path.
//
// Error is a pointer because the run either failed or did not — an empty string would make a
// successful run and a run that failed with an unworded error read alike. FinalText duplicates the
// last top-level `message` line on purpose: the simplest consumer of all reads `token`s, and
// without this it would have to implement both an accumulator and the stream_reset rule to reach
// the answer. Saved says whether a record was actually written, so a consumer never feeds the id
// of an unsaved run to `apogee undo`.
type RunFinished struct {
	ExitCode     int             `json:"exit_code"`
	Turns        int             `json:"turns"`
	Denied       int             `json:"denied"`
	Faulted      bool            `json:"faulted"`
	Fault        string          `json:"fault"`
	Error        *string         `json:"error"`
	Title        string          `json:"title"`
	FinalText    string          `json:"final_text"`
	Wrote        []string        `json:"wrote"`
	ContextFiles ContextFiles    `json:"context_files"`
	UndoNote     string          `json:"undo_note"`
	Saved        bool            `json:"saved"`
	Usage        Usage           `json:"usage"`
	SubAgents    []SubAgentUsage `json:"sub_agents"`
}

// ContextFiles mirrors domain.ContextFilesReport: what the session's workspace context files
// contributed, measured at session construction.
type ContextFiles struct {
	Files          []ContextFileNote `json:"files"`
	StandingTokens int               `json:"standing_tokens"`
	SystemShare    int               `json:"system_share"`
}

// ContextFileNote mirrors domain.ContextFileNote: one file's line of the session notice. Exactly
// one of Bytes and Err is meaningful.
type ContextFileNote struct {
	Name  string `json:"name"`
	Bytes int    `json:"bytes"`
	Err   string `json:"err"`
}

// Usage mirrors the Firing's own cumulative token accounting — the top-level agent's totals for
// the whole run, Compaction folds included. A delegated run's spend is its own and rides
// SubAgents.
type Usage struct {
	Calls              int `json:"calls"`
	PromptTokens       int `json:"prompt_tokens"`
	CompletionTokens   int `json:"completion_tokens"`
	TotalTokens        int `json:"total_tokens"`
	CachedPromptTokens int `json:"cached_prompt_tokens"`
}

// SubAgentUsage mirrors one finished sub-agent run's context fill and cumulative spend, in FINISH
// order on the frame. Limit is the CHILD's own window, which for a routed delegation is not the
// Firing's; 0 means no reading named one.
type SubAgentUsage struct {
	Used               int    `json:"used"`
	Limit              int    `json:"limit"`
	Task               string `json:"task"`
	Name               string `json:"name"`
	Model              string `json:"model"`
	Calls              int    `json:"calls"`
	PromptTokens       int    `json:"prompt_tokens"`
	CompletionTokens   int    `json:"completion_tokens"`
	TotalTokens        int    `json:"total_tokens"`
	CachedPromptTokens int    `json:"cached_prompt_tokens"`
}
