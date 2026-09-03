package agent

// Loop-level proof that a Deferred Response Action is Exchange-scoped (item 7 / F6): a
// remaining-items directive queued mid-fan-out is expired whenever the Exchange ends — a terminal
// fault (abandonTurn) or an Esc-path AbortExchange — and is truncated-then-restored (never doubled)
// when a cancelled Turn is rolled back. They drive the real loop with a SYNTHETIC deferring hook —
// the catalogued row that used to supply the fan-out retired in v0.20.0 (ADR 0071), while
// ActionDefer stays lab API — using the directive marker as the discriminator: a stale directive
// would ride the NEXT Exchange's first request, and a doubled one would round-trip through the
// snapshot.

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/tools"
)

// deferSubtasks are three distinct subtasks the synthetic hook fans out one per Turn, so the
// remainder shrinks by exactly one each time and the "(N left)" count is a reliable discriminator.
var deferSubtasks = []string{
	"Research the authentication module and list its entry points",
	"Draft the API endpoint specification for the login flow",
	"Write integration tests covering the login happy path",
}

const (
	// deferFanOutCue is the synthetic hook's whole gate: the Exchange it opens a fan-out in is the
	// one whose opening ask carries this cue. A real Mechanism measures a signal here; what F6 needs
	// is only that the gate be Exchange-scoped, so a following Exchange's plain ask arms nothing.
	deferFanOutCue = "fan out the work"
	// deferOpeningAsk is the ask that opens the fan-out under test.
	deferOpeningAsk = "Take the login rewrite and fan out the work across sub-agents."
	// deferDirectiveMarker is the synthetic hook's fixed remaining-items vocabulary — the marker the
	// proofs below match on in a request, in the snapshot state, and across an Exchange boundary.
	deferDirectiveMarker = "Remaining subtasks"
	// deferringMechID is the synthetic row's catalogue ID. It is registered directly on a
	// MechanismRegistry (never through the shipped catalogue), which is what keeps these proofs
	// about the LOOP's Deferred-Action handling rather than about any one Mechanism.
	deferringMechID domain.MechanismID = "deferring_fanout"
)

// deferringMech is the synthetic stand-in for any Mechanism that opens a serialized sub_agent
// fan-out and feeds the remainder forward as a Deferred Response Action: on the cued Exchange's
// first tool-less reply it synthesizes the opening delegation, and after every reply it defers a
// directive naming how many subtasks are still outstanding. That is the exact shape F6 is about —
// a correction queued mid-fan-out, drained by the next request — with nothing of a catalogue row
// in it.
type deferringMech struct{}

// row is the catalogue row a test registers deferringMech under.
func (m deferringMech) row() domain.RegisteredMechanism {
	return domain.RegisteredMechanism{
		Descriptor: domain.MechanismDescriptor{ID: deferringMechID, Capability: domain.CapProactiveNudge},
		Hook:       m,
	}
}

func (deferringMech) PostResponse(_ context.Context, resp *domain.Response) (domain.PostResponseDecision, error) {
	view := resp.View()
	if view.Depth() != 0 {
		return domain.PostResponseDecision{}, nil // the parent's fan-out only, never a child's own Turn
	}
	conv := view.Conversation()
	if !deferFanOutAsked(conv) {
		return domain.PostResponseDecision{}, nil // this Exchange never asked for a fan-out
	}

	calls := resp.ToolCalls()
	dispatched := deferDispatchedThisExchange(conv) + deferSubAgentCalls(calls)
	if dispatched >= len(deferSubtasks) {
		return domain.PostResponseDecision{}, nil // the list is exhausted — nothing left to feed forward
	}
	if len(calls) == 0 {
		// The opening reply: synthesize the first delegation so the fan-out is under way.
		args, err := json.Marshal(tools.SubAgentArgs{Task: deferSubtasks[dispatched]})
		if err != nil {
			return domain.PostResponseDecision{}, err
		}
		resp.AppendToolCall(domain.ToolCall{
			ID:        fmt.Sprintf("defer-%d", dispatched),
			Tool:      tools.SubAgentToolName,
			Arguments: args,
		})
		dispatched++
	}
	remaining := len(deferSubtasks) - dispatched
	if remaining == 0 {
		return domain.PostResponseDecision{}, nil
	}
	return domain.PostResponseDecision{
		Action: domain.ActionDefer,
		Inject: fmt.Sprintf("%s (%d left)", deferDirectiveMarker, remaining),
	}, nil
}

// deferFanOutAsked reports whether the CURRENT Exchange's opening ask carries the cue. Deriving it
// from the Exchange boundary rather than from the whole conversation is what makes the hook
// Exchange-scoped: once a fault or an abort ends the Exchange, the next ask opens a new one and the
// gate is quiet again.
func deferFanOutAsked(conv domain.ConversationView) bool {
	ex := domain.CurrentExchange(conv)
	if !ex.Found() {
		return false
	}
	return strings.Contains(conv.At(ex.UserIndex()).Content, deferFanOutCue)
}

// deferDispatchedThisExchange counts the sub_agent calls already committed after the current
// Exchange's opening ask — the fan-out's progress as honest history records it.
func deferDispatchedThisExchange(conv domain.ConversationView) int {
	n := 0
	domain.CurrentExchange(conv).RangeAfter(func(_ int, m domain.Message) bool {
		n += deferSubAgentCalls(m.ToolCalls)
		return true
	})
	return n
}

// deferSubAgentCalls counts the sub_agent delegations among calls.
func deferSubAgentCalls(calls []domain.ToolCall) int {
	n := 0
	for _, c := range calls {
		if c.Tool == tools.SubAgentToolName {
			n++
		}
	}
	return n
}

// deferConfig wires the sub_agent recursion point and the synthetic deferring row onto a fresh
// Config — the whole arm these proofs need.
func deferConfig(t *testing.T, sink domain.EventSink) domain.Config {
	t.Helper()
	cfg := subAgentConfig(sink, domain.ModeAskBefore) // registers sub_agent so the fan-out has a target
	reg := domain.NewMechanismRegistry()
	mustAddMech(t, reg, deferringMech{}.row())
	cfg.Mechanisms = reg
	return cfg
}

// deferRequestContains reports whether any message content of req contains substr.
func deferRequestContains(req provider.Request, substr string) bool {
	for _, m := range req.Messages {
		if strings.Contains(m.Content, substr) {
			return true
		}
	}
	return false
}

// errorScript is a stream that surfaces one terminal fault — the loop treats it as turnFailed and
// degrades the Turn to a clean Exchange-complete boundary (abandonTurn) with no assistant message.
func errorScript(msg string) []provider.Delta {
	return []provider.Delta{{Kind: provider.DeltaError, Err: msg}}
}

// blockAtResponder replays scripts like scriptedResponder, but the stream for call index blockAt
// blocks until ctx is cancelled, then surfaces the cancellation as a terminal stream error — the fake
// that suspends a specific nested-child Turn so a cancel can be timed deterministically (started is
// closed once the blocking stream is in flight).
type blockAtResponder struct {
	scripts [][]provider.Delta
	blockAt int
	started chan struct{}
	calls   int
}

func (r *blockAtResponder) Stream(ctx context.Context, _ provider.Request) iter.Seq[provider.Delta] {
	i := r.calls
	r.calls++
	return func(yield func(provider.Delta) bool) {
		if i == r.blockAt {
			close(r.started)
			<-ctx.Done()
			yield(provider.Delta{Kind: provider.DeltaError, Err: ctx.Err().Error()})
			return
		}
		if i >= len(r.scripts) {
			yield(provider.Delta{Kind: provider.DeltaError, Err: "blockAtResponder: out of scripts"})
			return
		}
		for _, d := range r.scripts[i] {
			if !yield(d) {
				return
			}
		}
	}
}

// deferDirectiveCount reports how many message contents of req carry the remaining-items directive
// marker — 1 for a single queued directive, 2 for two contradictory copies (the pre-fix defect).
func deferDirectiveCount(req provider.Request) int {
	n := 0
	for _, m := range req.Messages {
		n += strings.Count(m.Content, deferDirectiveMarker)
	}
	return n
}

// TestDeferredAction_FaultMidFanOutExpiresDirective proves the fault half of F6: a terminal
// upstream fault mid-fan-out ends the Exchange, and the deferred remaining-items directive expires
// with it — a fresh ask in the NEXT Exchange is answered on its own terms with NO directive marker
// riding its first request. Without the abandonTurn clear the restored directive would survive into
// the new Exchange and steer the model back into the abandoned fan-out.
func TestDeferredAction_FaultMidFanOutExpiresDirective(t *testing.T) {
	sink := &recordingSink{}
	cfg := deferConfig(t, sink)
	responder := &captureAllResponder{scripts: [][]provider.Delta{
		contentScript("Here is the plan."),                 // parent T0: tool-less reply → synthesized delegation of subtask 1
		contentScript("report A: entry points catalogued"), // child A (delegated subtask 1)
		errorScript("upstream boom mid-fan-out"),           // parent T1: a terminal fault ends the Exchange
		contentScript("Fresh answer to the follow-up ask"), // Exchange 2 T0: the new ask's answer
	}}

	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: deferOpeningAsk}); err != nil {
		t.Fatalf("Submit (Exchange 1): %v", err)
	}
	res, err := a.Run(context.Background())
	if err != nil {
		t.Fatalf("Run (Exchange 1): %v", err)
	}
	if res.Status != domain.StatusExchangeComplete {
		t.Fatalf("Exchange 1 status = %q, want the fault to end the Exchange", res.Status)
	}

	// Exchange 2: a fresh ask carrying no fan-out cue of its own.
	beforeNew := len(responder.got)
	const followUp = "What is the capital of France?"
	if err := a.Submit(domain.UserInput{Text: followUp}); err != nil {
		t.Fatalf("Submit (Exchange 2): %v", err)
	}
	res2, err := a.Run(context.Background())
	if err != nil {
		t.Fatalf("Run (Exchange 2): %v", err)
	}
	if res2.Status != domain.StatusExchangeComplete {
		t.Fatalf("Exchange 2 status = %q, want it to complete on the new ask", res2.Status)
	}

	newReqs := responder.got[beforeNew:]
	if len(newReqs) == 0 {
		t.Fatal("Exchange 2 sent no request")
	}
	for i, r := range newReqs {
		if deferRequestContains(r, deferDirectiveMarker) {
			t.Errorf("Exchange 2 request %d carried a stale remaining-items directive; the fault did not expire the queue", i)
		}
	}
	if !deferRequestContains(newReqs[0], followUp) {
		t.Error("Exchange 2's first request did not carry the new ask")
	}
}

// TestDeferredAction_AbortExchangeMidFanOutExpiresDirective proves the abort half of F6: an
// Esc-path AbortExchange mid-fan-out scraps the Exchange and expires the deferred directive, so the
// next submitted ask is answered cleanly with no directive marker. Without the AbortExchange clear
// the queued directive would leak into the next Exchange.
func TestDeferredAction_AbortExchangeMidFanOutExpiresDirective(t *testing.T) {
	sink := &recordingSink{}
	cfg := deferConfig(t, sink)
	responder := &captureAllResponder{scripts: [][]provider.Delta{
		contentScript("Here is the plan."),                 // parent T0: tool-less reply → synthesized delegation of subtask 1
		contentScript("report A: entry points catalogued"), // child A (delegated subtask 1)
		contentScript("Fresh answer after the abort"),      // Exchange 2 T0: the new ask's answer
	}}

	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: deferOpeningAsk}); err != nil {
		t.Fatalf("Submit (Exchange 1): %v", err)
	}
	// One Step opens the fan-out (first child dispatched, directive deferred) and leaves the
	// Exchange OPEN — the mid-fan-out boundary an Esc abort lands on.
	res, err := a.Step(context.Background())
	if err != nil {
		t.Fatalf("Step (fan-out open): %v", err)
	}
	if res.Status != domain.StatusTurnComplete {
		t.Fatalf("first Step status = %q, want %q (the fan-out is mid-flight)", res.Status, domain.StatusTurnComplete)
	}

	a.AbortExchange() // Esc: scrap the Exchange and, per F6, expire the queued directive

	beforeNew := len(responder.got)
	const followUp = "Give me a one-line summary of Go's goroutines."
	if err := a.Submit(domain.UserInput{Text: followUp}); err != nil {
		t.Fatalf("Submit (Exchange 2): %v", err)
	}
	res2, err := a.Run(context.Background())
	if err != nil {
		t.Fatalf("Run (Exchange 2): %v", err)
	}
	if res2.Status != domain.StatusExchangeComplete {
		t.Fatalf("Exchange 2 status = %q, want it to complete on the new ask", res2.Status)
	}

	newReqs := responder.got[beforeNew:]
	if len(newReqs) == 0 {
		t.Fatal("Exchange 2 sent no request")
	}
	for i, r := range newReqs {
		if deferRequestContains(r, deferDirectiveMarker) {
			t.Errorf("Exchange 2 request %d carried a stale remaining-items directive; AbortExchange did not expire the queue", i)
		}
	}
	if !deferRequestContains(newReqs[0], followUp) {
		t.Error("Exchange 2's first request did not carry the new ask")
	}
}

// TestDeferredAction_CancelDuringDelegationRestoresSingleDirective proves the cancel half of
// F6: a cancel while a follow-through Turn's delegation child is mid-stream rolls the Turn back and
// leaves EXACTLY ONE directive queued — the drained (2 left) one restored, not doubled with the
// (1 left) directive the cancelled Turn's own post-response hook had re-derived. The snapshot taken
// at the cancelled boundary round-trips that single directive, and the resumed first request carries
// it exactly once. Without the truncate-before-restore the queue would hold both copies.
func TestDeferredAction_CancelDuringDelegationRestoresSingleDirective(t *testing.T) {
	sink := &recordingSink{}
	cfg := deferConfig(t, sink)
	responder := &blockAtResponder{
		scripts: [][]provider.Delta{
			contentScript("Here is the plan."),         // parent T0: tool-less reply → synthesized delegation of subtask 1
			contentScript("report A: catalogued"),      // child A (delegated subtask 1)
			subAgentCallScript("m2", deferSubtasks[1]), // parent T1: delegate subtask 2 (post-response re-derives 1 left)
			// call 3 is child B's Turn — it blocks until cancel (blockAt below).
		},
		blockAt: 3,
		started: make(chan struct{}),
	}

	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: deferOpeningAsk}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	// Step 0: the opening Turn opens the fan-out (directive 2 left queued, drained next Turn).
	if res, err := a.Step(context.Background()); err != nil {
		t.Fatalf("Step (opening Turn): %v", err)
	} else if res.Status != domain.StatusTurnComplete {
		t.Fatalf("opening Turn status = %q, want %q", res.Status, domain.StatusTurnComplete)
	}

	// Step 1: the follow-through Turn drains (2 left), delegates subtask 2, re-derives (1 left), then
	// the child blocks — a cancel there rolls the Turn back after its own hook re-deferred.
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-responder.started // child B's Turn is in flight, post-response has already re-deferred (1 left)
		cancel()
	}()
	res, err := a.Step(ctx)
	if err != nil {
		t.Fatalf("Step (cancel during delegation): %v", err)
	}
	if res.Status != domain.StatusCancelled {
		t.Fatalf("Step status = %q, want %q (the delegation child was cancelled)", res.Status, domain.StatusCancelled)
	}

	// The snapshot at the cancelled boundary round-trips EXACTLY ONE directive — the restored drained
	// (2 left) one — not the (1 left) copy the cancelled Turn re-derived stacked on top of it.
	snap, err := a.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot after cancel: %v", err)
	}
	if n := strings.Count(string(snap.State), deferDirectiveMarker); n != 1 {
		t.Fatalf("snapshot carried %d remaining-items directives, want exactly 1 (the restored drained one)", n)
	}
	if !strings.Contains(string(snap.State), deferDirectiveMarker+" (2 left)") {
		t.Error("the single restored directive is not the drained (2 left) one")
	}

	// Resume and re-attempt the Turn: the first resumed request drains the ONE restored directive.
	sink2 := &recordingSink{}
	cfg2 := deferConfig(t, sink2)
	resumeResponder := &captureAllResponder{scripts: [][]provider.Delta{
		subAgentCallScript("m2b", deferSubtasks[1]),      // re-attempt T1: re-delegate subtask 2
		contentScript("report B: endpoint spec drafted"), // child B
		subAgentCallScript("m3", deferSubtasks[2]),       // delegate subtask 3
		contentScript("report C: tests written"),         // child C
		contentScript("Synthesis: resumed fan-out done"), // final no-tool answer
	}}
	b, err := resumeAgent(cfg2, snap, resumeResponder)
	if err != nil {
		t.Fatalf("resumeAgent after cancel: %v", err)
	}
	res2, err := b.Run(context.Background())
	if err != nil {
		t.Fatalf("Run (resumed): %v", err)
	}
	if res2.Status != domain.StatusExchangeComplete {
		t.Fatalf("resumed status = %q, want the Exchange to complete", res2.Status)
	}
	if len(resumeResponder.got) == 0 {
		t.Fatal("the resumed run sent no request")
	}
	if n := deferDirectiveCount(resumeResponder.got[0]); n != 1 {
		t.Errorf("the re-attempted request carried %d directives, want exactly 1 (no contradictory copies)", n)
	}
	if !deferRequestContains(resumeResponder.got[0], deferDirectiveMarker+" (2 left)") {
		t.Error("the re-attempted request did not carry the restored (2 left) directive")
	}
}
