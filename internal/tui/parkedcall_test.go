package tui

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// parkedTestMsg is the envelope a parked call produces in these tests — a stand-in for
// approvalReqMsg / askReqMsg that keeps the helper's own suite off either gate's vocabulary,
// so it tests [parkCall]'s interface rather than one caller's use of it.
type parkedTestMsg struct {
	Question string
	Reply    chan string
}

// parkedStub is a programSender playing the Update loop: it records what the seam sent and,
// when an answer hook is installed, replies from its own goroutine — which is the whole point,
// since the parked call blocks on the calling goroutine until that reply lands.
type parkedStub struct {
	answer func(reply chan string)

	mu      sync.Mutex
	sent    []tea.Msg
	replies sync.WaitGroup
}

// parkedStub satisfies the program seam.
var _ programSender = (*parkedStub)(nil)

// Send records msg and runs the answer hook for a parked request. It never blocks, mirroring
// *tea.Program.Send.
func (s *parkedStub) Send(msg tea.Msg) {
	s.mu.Lock()
	s.sent = append(s.sent, msg)
	s.mu.Unlock()

	req, ok := msg.(parkedTestMsg)
	if !ok || s.answer == nil {
		return
	}
	s.replies.Add(1)
	go func() {
		defer s.replies.Done()
		s.answer(req.Reply)
	}()
}

// messages returns a copy of the captured Msgs in send order.
func (s *parkedStub) messages() []tea.Msg {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]tea.Msg, len(s.sent))
	copy(out, s.sent)
	return out
}

// waitForReplies drains the answer goroutines, failing rather than hanging when one is stuck.
func (s *parkedStub) waitForReplies(t *testing.T) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		s.replies.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("a reply goroutine never finished: the reply channel blocked its sender")
	}
}

// boundParkedStub returns a stub already bound to a programRef, the pair every test here needs.
func boundParkedStub(t *testing.T) (*parkedStub, *programRef) {
	t.Helper()
	stub := &parkedStub{}
	ref := &programRef{}
	ref.bind(stub)
	return stub, ref
}

// parkedQuestion is the envelope every test below parks with.
func parkedQuestion(question string) func(chan string) tea.Msg {
	return func(reply chan string) tea.Msg {
		return parkedTestMsg{Question: question, Reply: reply}
	}
}

// TestParkCallReturnsTheReply proves the rendezvous hands the request to the loop and returns
// whatever that loop answered, from another goroutine.
func TestParkCallReturnsTheReply(t *testing.T) {
	t.Parallel()
	stub, ref := boundParkedStub(t)
	stub.answer = func(reply chan string) { reply <- "the human decided" }

	got, err := parkCall(context.Background(), ref, parkedQuestion("proceed?"), "abandoned")

	if err != nil {
		t.Fatalf("parkCall: unexpected error %v", err)
	}
	if got != "the human decided" {
		t.Errorf("reply = %q; want %q", got, "the human decided")
	}
	sent := stub.messages()
	if len(sent) != 1 {
		t.Fatalf("sent %d messages; want 1", len(sent))
	}
	req, ok := sent[0].(parkedTestMsg)
	if !ok {
		t.Fatalf("sent %T; want parkedTestMsg", sent[0])
	}
	if req.Question != "proceed?" {
		t.Errorf("question = %q; want %q", req.Question, "proceed?")
	}
	stub.waitForReplies(t)
}

// TestParkCallReturnsAbandonedValueOnCancel proves a cancelled ctx unblocks the gate with the
// caller's own abandoned value — the parameter that lets the approver settle on a deny while
// the asker settles on an empty answer — and with ctx.Err() beside it.
func TestParkCallReturnsAbandonedValueOnCancel(t *testing.T) {
	t.Parallel()
	_, ref := boundParkedStub(t) // no answer hook: nobody ever replies

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()
	got, err := parkCall(ctx, ref, parkedQuestion("proceed?"), "abandoned")

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v; want context.Canceled", err)
	}
	if got != "abandoned" {
		t.Errorf("reply = %q; want the abandoned value %q", got, "abandoned")
	}
}

// TestParkCallAbsorbsALateReply proves the buffered reply channel keeps a reply that arrives
// after the caller gave up from parking the goroutine that sends it — the no-leak property the
// whole rendezvous rests on.
func TestParkCallAbsorbsALateReply(t *testing.T) {
	t.Parallel()
	stub, ref := boundParkedStub(t)
	released := make(chan struct{})
	stub.answer = func(reply chan string) {
		<-released      // the human is still deciding while the Exchange is stopped
		reply <- "late" // nobody is listening any more; the buffer must take it anyway
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := parkCall(ctx, ref, parkedQuestion("proceed?"), "abandoned")
	close(released)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v; want context.Canceled", err)
	}
	if got != "abandoned" {
		t.Errorf("reply = %q; want the abandoned value %q", got, "abandoned")
	}
	stub.waitForReplies(t)
}

// TestParkCallSendsNothingBeforeReplyChannelExists is the ordering the envelope shape enforces:
// the reply channel is made first and handed to the loop inside the message, so no Msg can
// reach Update naming a channel that does not exist yet.
func TestParkCallSendsNothingBeforeReplyChannelExists(t *testing.T) {
	t.Parallel()
	stub, ref := boundParkedStub(t)
	stub.answer = func(reply chan string) { reply <- "ok" }

	if _, err := parkCall(context.Background(), ref, parkedQuestion("proceed?"), "abandoned"); err != nil {
		t.Fatalf("parkCall: unexpected error %v", err)
	}

	req, ok := stub.messages()[0].(parkedTestMsg)
	if !ok {
		t.Fatalf("sent %T; want parkedTestMsg", stub.messages()[0])
	}
	if req.Reply == nil {
		t.Fatal("the envelope carried a nil reply channel")
	}
	if cap(req.Reply) != 1 {
		t.Errorf("reply channel cap = %d; want 1 (buffered, so the loop never blocks replying)", cap(req.Reply))
	}
	stub.waitForReplies(t)
}
