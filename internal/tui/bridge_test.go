package tui

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/domain"
)

// TestNewBridgeDelegatesNonNil proves the composition root gets usable Config delegates
// from an unbound Bridge.
func TestNewBridgeDelegatesNonNil(t *testing.T) {
	t.Parallel()
	b := NewBridge()
	if b.Sink() == nil {
		t.Error("Sink() is nil")
	}
	if b.Approver() == nil {
		t.Error("Approver() is nil")
	}
}

// TestBridgeBindRoutesSinkAndApprover proves a single Bind wires both the Sink and the
// Approver to the same running program (they share one programRef).
func TestBridgeBindRoutesSinkAndApprover(t *testing.T) {
	t.Parallel()
	prog := newStubProgram()
	prog.replyWith(domain.ApprovalAllowForSession)
	b := NewBridge()
	b.Bind(prog)

	b.Sink().Emit(domain.TokenEvent{Text: "hi"})
	got, err := b.Approver().Approve(context.Background(), domain.ApprovalRequest{Tool: "t"})
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if got != domain.ApprovalAllowForSession {
		t.Errorf("decision = %q; want %q", got, domain.ApprovalAllowForSession)
	}
	prog.wait()
	// The token is coalesced inside the sink, so it arrives when its window closes rather
	// than inside Emit — wait for it before reading what the program received.
	waitForEvents(t, prog, 1)

	// Both the eventMsg and the approvalReqMsg reached the same bound program.
	var sawEvent, sawApproval bool
	for _, m := range prog.messages() {
		switch m.(type) {
		case eventMsg:
			sawEvent = true
		case approvalReqMsg:
			sawApproval = true
		}
	}
	if !sawEvent {
		t.Error("the bound program never received the event")
	}
	if !sawApproval {
		t.Error("the bound program never received the approval request")
	}
}

// TestBridgeUnboundDelegatesAreSafe proves the delegates are safe before Bind: Emit is a
// silent no-op and Approve unblocks on a cancelled ctx rather than hanging.
func TestBridgeUnboundDelegatesAreSafe(t *testing.T) {
	t.Parallel()
	b := NewBridge() // never bound

	b.Sink().Emit(domain.TokenEvent{Text: "x"}) // must not panic

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := b.Approver().Approve(ctx, domain.ApprovalRequest{})
	if got != domain.ApprovalDeny || !errors.Is(err, context.Canceled) {
		t.Errorf("Approve(unbound, cancelled) = (%q, %v); want (deny, context.Canceled)", got, err)
	}
}

// TestSeamConcurrentEmitApproveCancel is the headline acceptance test: drive the full seam
// — the worker stepping the engine, the engine emitting bursts of events and seeking
// approval, independent goroutines hammering Emit, a concurrent rebind, and a user stop —
// all at once, and require it to finish without a deadlock, panic, or data race (run under
// -race). It asserts only that the worker returns a terminal seam Msg; the exact terminal
// (done vs cancelled) depends on whether the stop lands before the boundary, and both are
// valid.
func TestSeamConcurrentEmitApproveCancel(t *testing.T) {
	t.Parallel()
	prog := newStubProgram()
	prog.replyWith(domain.ApprovalAllow) // the "Update loop" auto-approves, asynchronously
	b := NewBridge()
	b.Bind(prog)
	sink := b.Sink()
	approver := b.Approver()

	// Each Step emits a burst of tokens and, on the first Turn, seeks approval — the
	// interleaving the real loop produces — then ends the Exchange (honouring a stop).
	eng := &fakeEngine{
		stepFn: func(ctx context.Context, call int) (domain.StepResult, error) {
			for i := 0; i < 20; i++ {
				sink.Emit(domain.TokenEvent{Text: "x"})
			}
			if call == 0 {
				_, _ = approver.Approve(ctx, domain.ApprovalRequest{Tool: "write_file"})
				if ctx.Err() != nil {
					return domain.StepResult{Status: domain.StatusCancelled}, nil
				}
				return domain.StepResult{Status: domain.StatusTurnComplete}, nil
			}
			sink.Emit(domain.MessageEvent{Text: "done"})
			return domain.StepResult{Status: domain.StatusExchangeComplete}, nil
		},
	}

	// The worker carries the Bridge's own Step-boundary flush, as Run wires it: the flush then
	// races the window timer and six Emit goroutines here, which is exactly what -race must clear.
	cmd, cancel := startExchange(context.Background(), eng, domain.UserInput{Text: "go"}, nil, nil, b.sink.flush)

	var wg sync.WaitGroup
	var workerMsg tea.Msg

	wg.Add(1)
	go func() { defer wg.Done(); workerMsg = cmd() }()

	// Independent producers stress the bridge's atomic send path under -race.
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				sink.Emit(domain.TokenEvent{Text: "y"})
			}
		}()
	}
	// A concurrent rebind exercises programRef.box.Store racing with Load in send.
	wg.Add(1)
	go func() { defer wg.Done(); b.Bind(prog) }()
	// A user stop — may or may not catch the in-flight Approve; both outcomes are valid.
	wg.Add(1)
	go func() { defer wg.Done(); cancel() }()

	wg.Wait()
	prog.wait()

	switch workerMsg.(type) {
	case exchangeDoneMsg, cancelledMsg, errMsg:
	default:
		t.Fatalf("worker returned %T; want a terminal seam Msg", workerMsg)
	}
}

// TestBridgeNotifyRoutingLandsAsAnEphemeralNote proves the composition root's OTHER way in (ADR
// 0045): a routing change sent from the second heartbeat's goroutine reaches the Update loop as a
// routingNoticeMsg and becomes one transcript note — and, unlike a Firing's narration beside it, is
// not kept. The routing state is re-derived from live beats every time a session starts or resumes,
// so a stored line would be a claim about a server nobody has beaten since.
func TestBridgeNotifyRoutingLandsAsAnEphemeralNote(t *testing.T) {
	t.Parallel()
	prog := newStubProgram()
	b := NewBridge()
	b.Bind(prog)

	const note = "sub-agents: routing to grunt (cheap-7b)"
	b.NotifyRouting(note)

	var sent []routingNoticeMsg
	for _, m := range prog.messages() {
		if msg, ok := m.(routingNoticeMsg); ok {
			sent = append(sent, msg)
		}
	}
	if len(sent) != 1 || sent[0].note != note {
		t.Fatalf("the bound program received %+v; want one routingNoticeMsg carrying %q", sent, note)
	}

	m := step(t, newTestModel(t), sent[0])
	if !hasEntry(m, entryNote, note) {
		t.Errorf("the transcript has no %q note: %+v", note, m.transcript.entries)
	}

	blob, err := encodeTranscript(&m.transcript)
	if err != nil {
		t.Fatalf("encodeTranscript: %v", err)
	}
	entries, err := decodeTranscript(blob)
	if err != nil {
		t.Fatalf("decodeTranscript: %v", err)
	}
	for _, e := range entries {
		if e.kind == entryNote && strings.Contains(e.text, "sub-agents:") {
			t.Errorf("a routing notice survived the blob: %q", e.text)
		}
	}
}
