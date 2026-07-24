package tui

import (
	"context"
	"fmt"
	"sync"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/session"
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
	mu           sync.Mutex
	submitted    []domain.UserInput
	stepCalls    int
	modeSet      []domain.Mode // records SetMode calls (Shift+Tab drove the engine)
	confineSet   []bool        // records SetConfineToWorkspace calls (the /confine command)
	confine      bool          // the live blast radius ConfineToWorkspace reports; SetConfineToWorkspace swaps it
	clearCalls   int           // records ClearContext calls (the /clear command)
	abortCalls   int           // records AbortExchange calls (discarding a cancelled Exchange)
	compactCalls int           // records Compact calls (the /compact command)

	restoreCalls []domain.Session // records RestoreSession calls (the in-TUI resume primitive), in order
	inExchange   bool             // the value InExchange reports; a test sets it to model a mid-Exchange restore

	submitFn   func(domain.UserInput) error
	stepFn     func(ctx context.Context, call int) (domain.StepResult, error)
	snapshotFn func() (domain.Session, error)
	clearFn    func() error
	restoreFn  func(domain.Session) error // scripted RestoreSession error (nil ⇒ success)
	compactFn  func(context.Context) (skipped bool, err error)
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
	f.mu.Unlock()
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
	id         string
	sess       domain.Session
	transcript []byte
	title      string
	userMsgs   int
	ctxUsed    int
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
	rotates  int
	activeID string
	minted   int   // ids handed out so far, so a rotated session gets a distinct one
	saveErr  error // when non-nil, every Save fails with it (the ok→fail transition)
}

// fakeSessionHost satisfies the persistence seam the Model drives.
var _ SessionHost = (*fakeSessionHost)(nil)

func (h *fakeSessionHost) Save(sess domain.Session, transcript []byte, title string, userMsgs, ctxUsed int) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.activeID == "" {
		h.minted++
		h.activeID = fmt.Sprintf("s%d", h.minted)
	}
	h.saves = append(h.saves, savedCall{id: h.activeID, sess: sess, transcript: transcript, title: title, userMsgs: userMsgs, ctxUsed: ctxUsed})
	return h.saveErr
}

func (h *fakeSessionHost) Rotate() {
	h.mu.Lock()
	h.rotates++
	h.activeID = "" // close the session; the next Save mints a fresh id
	h.mu.Unlock()
}

func (h *fakeSessionHost) List() ([]session.Meta, error)       { return nil, nil }
func (h *fakeSessionHost) Load(string) (session.Record, error) { return session.Record{}, nil }
func (h *fakeSessionHost) Delete(string) error                 { return nil }
func (h *fakeSessionHost) Rename(string, string) error         { return nil }

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
