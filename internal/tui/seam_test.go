package tui

import (
	"context"
	"sync"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/domain"
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
	s.mu.Unlock()
	if req, ok := msg.(approvalReqMsg); ok && onApproval != nil {
		onApproval(req)
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

// wait drains the async reply goroutines started by replyWith.
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
	mu        sync.Mutex
	submitted []domain.UserInput
	stepCalls int

	submitFn func(domain.UserInput) error
	stepFn   func(ctx context.Context, call int) (domain.StepResult, error)
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

func (f *fakeEngine) Snapshot() (domain.Session, error) { return domain.Session{}, nil }
func (f *fakeEngine) Mode() domain.Mode                 { return domain.ModeAskBefore }
func (f *fakeEngine) Close() error                      { return nil }

// submits reports how many times Submit was called.
func (f *fakeEngine) submits() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.submitted)
}

// steps reports how many times Step was called.
func (f *fakeEngine) steps() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stepCalls
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
