package tui

import (
	"context"
	"fmt"
	"sort"
	"sync"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/session"
	"github.com/airiclenz/apogee/internal/undo"
)

// ----------------------------------------------------------------------------
// Shared seam-test doubles (phase-2 detail plan §3 C1–C5)
// ----------------------------------------------------------------------------

// stubProgram stands in for *tea.Program in seam tests: it records every Msg the seam
// sends and, optionally, answers Approval requests (modelling the Update loop) so the
// rendezvous can be driven without a terminal. It is safe for concurrent use because the
// seam sends from many goroutines (the worker, concurrent Emit, the approver).
type stubProgram struct {
	mu         sync.Mutex
	msgs       []tea.Msg
	onApproval func(approvalReqMsg) // invoked inside Send for each approvalReqMsg, if set
	onAsk      func(askReqMsg)      // invoked inside Send for each askReqMsg, if set
	replies    sync.WaitGroup       // tracks async reply goroutines for a clean drain
}

// stubProgram satisfies the program seam.
var _ programSender = (*stubProgram)(nil)

func newStubProgram() *stubProgram { return &stubProgram{} }

// Send records msg and, for an Approval request, runs the configured answer hook. It never
// blocks, mirroring *tea.Program.Send.
func (s *stubProgram) Send(msg tea.Msg) {
	s.mu.Lock()
	s.msgs = append(s.msgs, msg)
	onApproval := s.onApproval
	onAsk := s.onAsk
	s.mu.Unlock()
	if req, ok := msg.(approvalReqMsg); ok && onApproval != nil {
		onApproval(req)
	}
	if req, ok := msg.(askReqMsg); ok && onAsk != nil {
		onAsk(req)
	}
}

// replyWith makes the stub answer every Approval asynchronously, from its own goroutine —
// modelling the Update loop replying after the human's keypress. Call wait to drain those
// goroutines before the test ends.
func (s *stubProgram) replyWith(decision domain.ApprovalDecision) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onApproval = func(req approvalReqMsg) {
		s.replies.Add(1)
		go func() {
			defer s.replies.Done()
			req.Reply <- decision
		}()
	}
}

// answerWith makes the stub answer every ask_user question asynchronously, from its own
// goroutine — modelling the Update loop replying after the human types an answer (P3.11).
// Call wait to drain those goroutines before the test ends.
func (s *stubProgram) answerWith(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onAsk = func(req askReqMsg) {
		s.replies.Add(1)
		go func() {
			defer s.replies.Done()
			req.Reply <- domain.AskAnswer{Text: text}
		}()
	}
}

// wait drains the async reply goroutines started by replyWith / answerWith.
func (s *stubProgram) wait() { s.replies.Wait() }

// messages returns a copy of the captured Msgs in send order.
func (s *stubProgram) messages() []tea.Msg {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]tea.Msg, len(s.msgs))
	copy(out, s.msgs)
	return out
}

// events returns the domain.Events carried by the captured eventMsgs, in order.
func (s *stubProgram) events() []domain.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.Event
	for _, m := range s.msgs {
		if e, ok := m.(eventMsg); ok {
			out = append(out, e.Event)
		}
	}
	return out
}

// ----------------------------------------------------------------------------
// A scriptable engine
// ----------------------------------------------------------------------------

// fakeEngine is a scriptable Engine for seam tests. Submit and Step are closures so each
// test scripts the drive sequence — queued results, errors, event emission, or a
// ctx-honouring block. It records Submit/Step calls for assertions.
type fakeEngine struct {
	mu         sync.Mutex
	submitted  []domain.UserInput
	stepCalls  int
	modeSet    []domain.Mode // records SetMode calls (Shift+Tab drove the engine)
	mode       domain.Mode   // the live rung SetMode moved this engine to ("" ⇒ never moved)
	confineSet []bool        // records SetConfineToWorkspace calls (the /confine command)
	confine    bool          // the live blast radius ConfineToWorkspace reports; SetConfineToWorkspace swaps it

	effortSet      []domain.ThinkingEffort // records SetEffortOverride calls (the /effort command)
	effortOverride domain.ThinkingEffort   // the live session override ThinkingEffort reports as its first layer
	effortProfile  domain.ThinkingEffort   // the bound profile's own effort — scripted, never moved from the TUI

	clearCalls   int // records ClearContext calls (the /clear command)
	abortCalls   int // records AbortExchange calls (discarding a cancelled Exchange)
	compactCalls int // records Compact calls (the /compact command)

	restoreCalls []domain.Session // records RestoreSession calls (the in-TUI resume primitive), in order
	inExchange   bool             // the value InExchange reports; a test sets it to model a mid-Exchange restore

	contextReport  domain.ContextFilesReport // the value ContextFilesReport returns (the zero value: no context files)
	contextReports int                       // how many times ContextFilesReport was read (once per session boundary)

	interjected []domain.UserInput // records Interject calls (the worker's between-Steps delivery), in order

	undoStep    undo.Step   // the step UndoPreview answers with; the zero value plus undoStepOK false is "nothing to undo"
	undoStepOK  bool        // whether UndoPreview reports a step at all
	undoReport  undo.Report // the report a permitted UndoRevert returns
	undoErr     error       // the refusal UndoRevert returns instead (a stale generation, an empty journal)
	undoReverts []uint64    // records the generations UndoRevert was called with, in order

	submitFn    func(domain.UserInput) error
	stepFn      func(ctx context.Context, call int) (domain.StepResult, error)
	snapshotFn  func() (domain.Session, error)
	clearFn     func() error
	restoreFn   func(domain.Session) error // scripted RestoreSession error (nil ⇒ success)
	compactFn   func(context.Context) (skipped bool, err error)
	interjectFn func(domain.UserInput) error // scripted Interject error (nil ⇒ committed)
}

// fakeEngine satisfies the narrow Engine seam the worker drives.
var _ Engine = (*fakeEngine)(nil)

func (f *fakeEngine) Submit(in domain.UserInput) error {
	f.mu.Lock()
	f.submitted = append(f.submitted, in)
	fn := f.submitFn
	f.mu.Unlock()
	if fn != nil {
		return fn(in)
	}
	return nil
}

func (f *fakeEngine) Step(ctx context.Context) (domain.StepResult, error) {
	f.mu.Lock()
	call := f.stepCalls
	f.stepCalls++
	fn := f.stepFn
	f.mu.Unlock()
	return fn(ctx, call)
}

// Interject records the delivery the worker attempted at a between-Steps boundary and answers
// with whatever the test scripted (nil ⇒ committed). A scripted refusal models the real Agent's
// ErrNoOpenExchange / empty-input guards without needing an engine in a particular state. It
// mirrors Submit: the call is recorded whether or not it is refused, so a test can prove that a
// refusal STOPPED the drain rather than merely skipping a row.
func (f *fakeEngine) Interject(in domain.UserInput) error {
	f.mu.Lock()
	f.interjected = append(f.interjected, in)
	fn := f.interjectFn
	f.mu.Unlock()
	if fn != nil {
		return fn(in)
	}
	return nil
}

// interjections reports the inputs Interject was handed, in delivery order — empty when the
// worker delivered nothing.
func (f *fakeEngine) interjections() []domain.UserInput {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]domain.UserInput(nil), f.interjected...)
}

func (f *fakeEngine) Snapshot() (domain.Session, error) {
	if f.snapshotFn != nil {
		return f.snapshotFn()
	}
	return domain.Session{}, nil
}
func (f *fakeEngine) ClearContext() error {
	f.mu.Lock()
	f.clearCalls++
	fn := f.clearFn
	f.mu.Unlock()
	if fn != nil {
		return fn()
	}
	return nil
}

func (f *fakeEngine) AbortExchange() {
	f.mu.Lock()
	f.abortCalls++
	f.inExchange = false // the real Agent returns to a clean boundary; InExchange reads false after
	f.mu.Unlock()
}

func (f *fakeEngine) RestoreSession(snap domain.Session) error {
	f.mu.Lock()
	f.restoreCalls = append(f.restoreCalls, snap)
	fn := f.restoreFn
	f.mu.Unlock()
	if fn != nil {
		return fn(snap)
	}
	return nil
}

func (f *fakeEngine) InExchange() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.inExchange
}

// ContextFilesReport answers with whatever the test scripted (the zero value — no files — for
// every test that does not care) and counts the reads, so a test can prove the UI takes the
// report once per session boundary rather than on every repaint.
func (f *fakeEngine) ContextFilesReport() domain.ContextFilesReport {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.contextReports++
	return f.contextReport
}

// contextReads reports how many times the UI read the context-files report.
func (f *fakeEngine) contextReads() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.contextReports
}

// restores reports the snapshots RestoreSession was handed, in order — empty when the UI never
// drove a resume.
func (f *fakeEngine) restores() []domain.Session {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]domain.Session(nil), f.restoreCalls...)
}

func (f *fakeEngine) Compact(ctx context.Context) (bool, error) {
	f.mu.Lock()
	f.compactCalls++
	fn := f.compactFn
	f.mu.Unlock()
	if fn != nil {
		return fn(ctx)
	}
	return false, nil
}

func (f *fakeEngine) Close() error { return nil }

func (f *fakeEngine) SetMode(m domain.Mode) {
	f.mu.Lock()
	f.modeSet = append(f.modeSet, m)
	f.mode = m
	f.mu.Unlock()
}

// liveMode reports the rung this engine was last moved to — "" while nothing has moved it, which is
// the state a freshly built fake is in. It is the counterpart of the confine field above: the real
// holder answers Mode() the same way (cmd/apogee/wire_engine.go), and it is what the settings host's
// live overlay reads.
func (f *fakeEngine) liveMode() domain.Mode {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.mode
}

// modesSet reports the modes SetMode was called with, in order.
func (f *fakeEngine) modesSet() []domain.Mode {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]domain.Mode(nil), f.modeSet...)
}

func (f *fakeEngine) SetConfineToWorkspace(confine bool) {
	f.mu.Lock()
	f.confineSet = append(f.confineSet, confine)
	f.confine = confine
	f.mu.Unlock()
}

func (f *fakeEngine) ConfineToWorkspace() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.confine
}

// UndoPreview answers with the step the test scripted, so a /undo preview note can be asserted
// without a journal behind it.
func (f *fakeEngine) UndoPreview() (undo.Step, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.undoStep, f.undoStepOK
}

// UndoRevert records the generation the confirm quoted — the stale-preview guard's whole input —
// and answers with the scripted report or refusal.
func (f *fakeEngine) UndoRevert(generation uint64) (undo.Report, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.undoReverts = append(f.undoReverts, generation)
	return f.undoReport, f.undoErr
}

// SetEffortOverride records the level the /effort command drove and swaps the live override, so a
// test can prove both that the door was called and that the note afterwards reads back the result.
func (f *fakeEngine) SetEffortOverride(e domain.ThinkingEffort) {
	f.mu.Lock()
	f.effortSet = append(f.effortSet, e)
	f.effortOverride = e
	f.mu.Unlock()
}

// ThinkingEffort answers with the two layers the test scripted: the live override (moved by
// SetEffortOverride above) and the bound profile's own setting, which no TUI call can change.
func (f *fakeEngine) ThinkingEffort() (override, profile domain.ThinkingEffort) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.effortOverride, f.effortProfile
}

// effortsSet reports the levels SetEffortOverride was called with, in order — empty when the UI
// never drove it (what /effort leaves behind until a row of its picker is accepted).
func (f *fakeEngine) effortsSet() []domain.ThinkingEffort {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]domain.ThinkingEffort(nil), f.effortSet...)
}

// confinesSet reports the blast-radius values SetConfineToWorkspace was called with, in order —
// empty when the UI never drove it (what a parse-only /confine line must leave behind).
func (f *fakeEngine) confinesSet() []bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]bool(nil), f.confineSet...)
}

// submits reports how many times Submit was called.
func (f *fakeEngine) submits() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.submitted)
}

// aborts reports how many times AbortExchange was called (a cancel discarded the Exchange).
func (f *fakeEngine) aborts() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.abortCalls
}

// steps reports how many times Step was called.
func (f *fakeEngine) steps() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stepCalls
}

// ----------------------------------------------------------------------------
// A recording SessionHost
// ----------------------------------------------------------------------------

// savedCall records one SessionHost.Save the Model made, so a test can assert the payload it
// persisted per Turn (the snapshot, the encoded transcript blob, and the derived metadata). id is
// the active session id the fake host minted/reused for this Save, so a test can prove that a
// Rotate opened a fresh session.
type savedCall struct {
	id            string
	sess          domain.Session
	transcript    []byte
	title         string
	userMsgs      int
	ctxUsed       int
	usage         session.Usage
	delegateUsage session.Usage
}

// fakeSessionHost is a recording SessionHost for the save-pipeline tests: it captures every Save,
// counts Rotates, and can be scripted to fail Saves (saveErr) so the ok↔fail note transitions are
// provable without a store. It models the real host's id lifecycle — Save mints an id when the
// session is inactive and reuses it thereafter, Rotate closes the session so the next Save mints a
// fresh id — so the /clear|/new rotate contract is provable without the store. It is
// concurrency-safe because a save Cmd runs on its own goroutine.
type fakeSessionHost struct {
	mu       sync.Mutex
	saves    []savedCall
	renames  []renameCall // every Rename asked for, in order (the browser's `r` and generated titles)
	rotates  int
	activeID string
	minted   int   // ids handed out so far, so a rotated session gets a distinct one
	saveErr  error // when non-nil, every Save fails with it (the ok→fail transition)
	// renameErr scripts a Rename failure — the store's ENOENT when the record is not on disk yet,
	// which is what a title answering inside the first Save's window actually hits.
	renameErr error

	// The browser-side store (item 7): the /sessions overlay lists/loads/deletes/renames these
	// records. seed populates it; Load makes a record active, so a test can prove that resuming
	// switches which file later Saves target. listErr/loadErr script the corresponding failures.
	stored  map[string]session.Record
	listErr error
	loadErr error
}

// seed adds a record to the fake store so the /sessions browser can list/load it.
func (h *fakeSessionHost) seed(rec session.Record) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.stored == nil {
		h.stored = map[string]session.Record{}
	}
	h.stored[rec.Meta.ID] = rec
}

// fakeSessionHost satisfies the persistence seam the Model drives.
var _ SessionHost = (*fakeSessionHost)(nil)

func (h *fakeSessionHost) Save(
	sess domain.Session,
	transcript []byte,
	title string,
	userMsgs, ctxUsed int,
	usage, delegateUsage session.Usage,
) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.activeID == "" {
		h.minted++
		h.activeID = fmt.Sprintf("s%d", h.minted)
	}
	h.saves = append(h.saves, savedCall{
		id: h.activeID, sess: sess, transcript: transcript, title: title,
		userMsgs: userMsgs, ctxUsed: ctxUsed, usage: usage, delegateUsage: delegateUsage,
	})
	return h.saveErr
}

func (h *fakeSessionHost) Rotate() {
	h.mu.Lock()
	h.rotates++
	h.activeID = "" // close the session; the next Save mints a fresh id
	h.mu.Unlock()
}

func (h *fakeSessionHost) List() ([]session.Meta, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.listErr != nil {
		return nil, h.listErr
	}
	metas := make([]session.Meta, 0, len(h.stored))
	for _, rec := range h.stored {
		metas = append(metas, rec.Meta)
	}
	sort.Slice(metas, func(i, j int) bool { return metas[i].UpdatedAt.After(metas[j].UpdatedAt) })
	return metas, nil
}

func (h *fakeSessionHost) Load(id string) (session.Record, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.loadErr != nil {
		return session.Record{}, h.loadErr
	}
	rec, ok := h.stored[id]
	if !ok {
		return session.Record{}, fmt.Errorf("no session %q", id)
	}
	return rec, nil // Load does not activate; Activate does, only after a confirmed restore
}

func (h *fakeSessionHost) Activate(meta session.Meta) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.activeID = meta.ID // later Saves update the activated session's file
}

func (h *fakeSessionHost) Delete(id string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.stored, id)
	return nil
}

// failRenames scripts every subsequent Rename to fail with err (nil restores success). It takes the
// lock because a write Cmd may be running when a test flips it.
func (h *fakeSessionHost) failRenames(err error) {
	h.mu.Lock()
	h.renameErr = err
	h.mu.Unlock()
}

func (h *fakeSessionHost) Rename(id, title string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.renames = append(h.renames, renameCall{id: id, title: title}) // recorded even when it fails
	if h.renameErr != nil {
		return h.renameErr
	}
	if rec, ok := h.stored[id]; ok {
		rec.Meta.Title = title
		h.stored[id] = rec
	}
	return nil
}

// renameCall is one title the host was asked to store. It is recorded whether or not a matching
// record was seeded, so a naming test can prove WHAT was renamed to what without a store.
type renameCall struct {
	id    string
	title string
}

// renamedTitles returns a copy of the recorded Renames in order.
func (h *fakeSessionHost) renamedTitles() []renameCall {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]renameCall(nil), h.renames...)
}

func (h *fakeSessionHost) ActiveID() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.activeID
}

// savedCalls returns a copy of the recorded Saves in order.
func (h *fakeSessionHost) savedCalls() []savedCall {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]savedCall(nil), h.saves...)
}

// rotateCount returns how many times the Model closed the active session (via /clear|/new or a
// load switching the id).
func (h *fakeSessionHost) rotateCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.rotates
}

// stepResult is one scripted Step outcome.
type stepResult struct {
	res domain.StepResult
	err error
}

// scriptedSteps returns a stepFn that yields results in order; once exhausted it reports a
// terminal StatusExchangeComplete, so a buggy drive loop terminates rather than spinning.
func scriptedSteps(results ...stepResult) func(context.Context, int) (domain.StepResult, error) {
	return func(_ context.Context, call int) (domain.StepResult, error) {
		if call >= len(results) {
			return domain.StepResult{Status: domain.StatusExchangeComplete}, nil
		}
		return results[call].res, results[call].err
	}
}
