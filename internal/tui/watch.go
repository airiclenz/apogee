package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/eventjson"
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
// engine and session view in [watchState]; esc swaps them back. Every keyed way to act on the
// session meets one allow-list gate ([Model.watchRefuses]) at the send and command chokepoints.

// watchPollInterval is how often the follower looks for lines the owner appended to the journal.
// The journal coalesces deltas over ~30 ms, so a poll a few windows long paints text in steps a
// reader still sees as streaming, at the cost of one stat and one short read per poll.
const watchPollInterval = 100 * time.Millisecond

// watchLegendFormat is the prompt box's invitation while watching, filled with the session title.
const watchLegendFormat = "watching %s · read-only · esc back"

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

// watchSource names the session a watch follows: the record it paints from, the journal it
// follows, and the title the box shows until the journal names one.
type watchSource struct {
	recordPath  string
	journalPath string
	title       string
}

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
	// parked is the watcher's own session view, restored on the way out.
	parked parkedSession
	// offset is how many journal bytes have been folded; file is the journal the offset counts in,
	// so a journal replaced under the same path reads as a truncation.
	offset int64
	file   os.FileInfo
}

// parkedSession is the session view watch mode sets aside: the field set resetSessionView
// (commandrun.go) resets, plus the run state the watch moves (state, acts, cursor). A field added
// to resetSessionView belongs here too.
type parkedSession struct {
	spentSkills     map[string]bool
	thinking        thinkingBoard
	advice          []adviceRecord
	transcript      transcript
	viewStack       []runView
	detached        bool
	stats           liveStats
	usage           domain.Usage
	usageBase       domain.Usage
	delegateUsage   domain.Usage
	workflows       backgroundWorkflows
	servedModels    []string
	sessionCurrency string
	flash           string
	autoTitleFired  bool
	titleTouched    bool
	pendingTitle    titleStash
	sessionName     string
	boundary        domain.Session
	hasBoundary     bool
	state           uiState
	acts            runActivities
	cursor          blockCursor
}

// watchPollMsg asks the Model to read the journal once more; gen names the watch that asked.
type watchPollMsg struct{ gen int }

// watchBatchMsg is one follower read: the complete lines appended since the last one, decoded, and
// where the next read starts. truncated says the journal shrank or was replaced, so the view
// clears before items fold.
type watchBatchMsg struct {
	gen         int
	offset      int64
	file        os.FileInfo
	isTruncated bool
	items       []watchItem
}

// watchItem is one decoded journal line the watch folds: exactly one of its members is set.
type watchItem struct {
	start      *watchStart
	prompt     *eventjson.Prompt
	event      domain.Event
	isFinished bool
}

// watchStart is a journal's opening frame resolved against the record: the title, and the record's
// first `entries` entries ready to replay (err when they were wanted and would not read).
type watchStart struct {
	title   string
	entries []entry
	err     error
}

// enterWatch turns the view into a watch of src and returns the follower's first read. It is
// called at idle only: a running worker or an open decision pane owns the view, and the call is
// refused (the Model returned unchanged) rather than parking a session mid-Exchange.
//
// The inert engine's getters answer what the owner's answered at entry, so the footer's
// confinement word and effort segment do not move across the swap.
func (m Model) enterWatch(src watchSource) (Model, tea.Cmd) {
	if m.watch.isActive || m.state != stateIdle {
		return m, nil
	}
	override, profile := m.eng.ThinkingEffort()
	inert := inertEngine{
		isConfined:     m.eng.ConfineToWorkspace(),
		effortOverride: override,
		effortProfile:  profile,
		isInExchange:   m.eng.InExchange(),
	}
	m.watch = watchState{
		isActive: true,
		gen:      m.watch.gen + 1,
		source:   src,
		title:    stripEscapes(src.title),
		owner:    m.eng,
		parked:   m.parkSession(),
	}
	m.eng = inert
	m.clearWatchView()
	return m, m.readJournalCmd()
}

// leaveWatch is esc's way out: the owner's engine and session view come back exactly as parked,
// and every follower message still in flight is dropped by the generation it carries.
func (m Model) leaveWatch() Model {
	if !m.watch.isActive {
		return m
	}
	m.eng = m.watch.owner
	m.restoreSession(m.watch.parked)
	m.watch = watchState{gen: m.watch.gen}
	// The block-paint cache is keyed by entry index, and the indices now mean the restored
	// transcript's entries again, not the watched one's (paintcache.go).
	m.transcript.paints.clear()
	m.transcript.touch()
	m.layout()
	return m
}

// parkSession copies the session view out of the Model.
func (m Model) parkSession() parkedSession {
	return parkedSession{
		spentSkills:     m.spentSkills,
		thinking:        m.thinking,
		advice:          m.advice,
		transcript:      m.transcript,
		viewStack:       m.viewStack,
		detached:        m.detached,
		stats:           m.liveStats,
		usage:           m.usage,
		usageBase:       m.usageBase,
		delegateUsage:   m.delegateUsage,
		workflows:       m.workflows,
		servedModels:    m.servedModels,
		sessionCurrency: m.sessionCurrency,
		flash:           m.flash,
		autoTitleFired:  m.autoTitleFired,
		titleTouched:    m.titleTouched,
		pendingTitle:    m.pendingTitle,
		sessionName:     m.sessionName,
		boundary:        m.boundary,
		hasBoundary:     m.hasBoundary,
		state:           m.state,
		acts:            m.acts,
		cursor:          m.cursor,
	}
}

// restoreSession writes a parked session view back into the Model.
func (m *Model) restoreSession(p parkedSession) {
	m.spentSkills = p.spentSkills
	m.thinking = p.thinking
	m.advice = p.advice
	m.transcript = p.transcript
	m.viewStack = p.viewStack
	m.detached = p.detached
	m.liveStats = p.stats
	m.usage = p.usage
	m.usageBase = p.usageBase
	m.delegateUsage = p.delegateUsage
	m.workflows = p.workflows
	m.servedModels = p.servedModels
	m.sessionCurrency = p.sessionCurrency
	m.flash = p.flash
	m.autoTitleFired = p.autoTitleFired
	m.titleTouched = p.titleTouched
	m.pendingTitle = p.pendingTitle
	m.sessionName = p.sessionName
	m.boundary = p.boundary
	m.hasBoundary = p.hasBoundary
	m.state = p.state
	m.acts = p.acts
	m.cursor = p.cursor
}

// clearWatchView empties the view for a (re)paint of the watched session: an empty transcript under
// the same display preferences (transcript.reset keeps them), empty boards and accounting, and the
// watched run idle until its journal says otherwise. The parked copies are untouched — every
// reset below replaces a slice or map rather than editing it in place.
func (m *Model) clearWatchView() {
	fresh := m.transcript
	fresh.reset()
	boards := m.thinking
	boards.reset()
	m.restoreSession(parkedSession{transcript: fresh, thinking: boards, state: stateIdle})
	m.reroot()
}

// readJournalCmd is the follower's read, run off the Update goroutine.
func (m Model) readJournalCmd() tea.Cmd {
	src, gen, offset, file := m.watch.source, m.watch.gen, m.watch.offset, m.watch.file
	return func() tea.Msg { return readJournal(src, gen, offset, file) }
}

// nextWatchPoll schedules the follower's next read.
func (m Model) nextWatchPoll() tea.Cmd {
	gen := m.watch.gen
	return tea.Tick(watchPollInterval, func(time.Time) tea.Msg { return watchPollMsg{gen: gen} })
}

// readJournal reads the complete lines appended to src's journal past offset and decodes them. It
// touches the journal only — open, stat, read — and never the record's lock (ADR 0103 decision 5).
//
// A journal that shrank below offset, or a different file now under the path (the owner reopened
// the record, which truncates its journal), restarts the read at 0 and reports isTruncated. A
// trailing line the owner has not finished writing is left for the next read. A journal that will
// not open or read yields a batch with no items at the same offset: the follower simply looks again
// on its next poll, which is all a reader of a file it does not own can do.
func readJournal(src watchSource, gen int, offset int64, known os.FileInfo) watchBatchMsg {
	batch := watchBatchMsg{gen: gen, offset: offset, file: known}
	f, err := os.Open(src.journalPath)
	if err != nil {
		return batch
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return batch
	}
	if known != nil && (!os.SameFile(known, info) || info.Size() < offset) {
		batch.isTruncated = true
		offset = 0
	}
	batch.offset, batch.file = offset, info

	data, err := io.ReadAll(io.NewSectionReader(f, offset, info.Size()-offset))
	if err != nil {
		return batch
	}
	end := bytes.LastIndexByte(data, '\n')
	if end < 0 {
		return batch
	}
	batch.offset = offset + int64(end+1)
	for _, raw := range bytes.Split(data[:end], []byte{'\n'}) {
		if item, ok := decodeWatchLine(raw, src); ok {
			batch.items = append(batch.items, item)
		}
	}
	return batch
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

// loadWatchStart resolves a journal's opening frame: its title, and the record's first `entries`
// entries, decoded and with every call the record stored open closed, as a resume closes them
// (closeInterruptedCalls) — but with none of a resume's notices. The record is read by path,
// read-only; a store is never opened for it.
func loadWatchStart(raw []byte, recordPath string) watchStart {
	var frame journalStartedLine
	if err := json.Unmarshal(raw, &frame); err != nil {
		return watchStart{err: fmt.Errorf("watch: journal frame: %w", err)}
	}
	start := watchStart{title: frame.Data.Title}
	if frame.Data.Entries <= 0 {
		return start
	}
	start.entries, start.err = recordHead(recordPath, frame.Data.Entries)
	return start
}

// recordHead reads the record at path and returns its first n transcript entries.
func recordHead(path string, n int) ([]entry, error) {
	if path == "" {
		return nil, errors.New("watch: no record path")
	}
	record, err := session.NewStore(filepath.Dir(path)).LoadPath(path)
	if err != nil {
		return nil, err
	}
	entries, err := decodeTranscript(record.Transcript)
	if err != nil {
		return nil, err
	}
	entries = entries[:min(n, len(entries))]
	closeInterruptedCalls(entries)
	return entries, nil
}

// foldWatchPoll answers the follower's tick with the next read.
func (m Model) foldWatchPoll(msg watchPollMsg) (tea.Model, tea.Cmd) {
	if !m.watch.isActive || msg.gen != m.watch.gen {
		return m, nil
	}
	return m, m.readJournalCmd()
}

// foldWatchBatch folds one follower read into the watch view, in journal order, and schedules the
// next poll. A batch for another watch, or one arriving after esc, is dropped whole.
func (m Model) foldWatchBatch(msg watchBatchMsg) (tea.Model, tea.Cmd) {
	if !m.watch.isActive || msg.gen != m.watch.gen {
		return m, nil
	}
	m.watch.offset, m.watch.file = msg.offset, msg.file
	if msg.isTruncated {
		m.clearWatchView()
	}
	wasRunning := m.state == stateRunning
	for _, item := range msg.items {
		m = m.foldWatchItem(item)
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
// session is not being resumed.
func (m *Model) paintWatchStart(start watchStart) {
	m.clearWatchView()
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
func (m Model) foldWatchedEvent(e domain.Event) Model {
	if m.workflows.owns(e) {
		return m.foldBackgroundEvent(e)
	}
	return m.foldEvent(e)
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

// watchLegend is the prompt box's invitation while watching.
func (m Model) watchLegend() string {
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
func (inertEngine) ResumeWorkflows() error                      { return nil }

func (e inertEngine) ThinkingEffort() (domain.ThinkingEffort, domain.ThinkingEffort) {
	return e.effortOverride, e.effortProfile
}

func (inertEngine) StartRecipe(context.Context, domain.RecipeLaunch) (string, error) {
	return "", nil
}

func (inertEngine) AnswerWorkflowPrompt(uint64, domain.WorkflowPromptAnswer) bool { return false }
