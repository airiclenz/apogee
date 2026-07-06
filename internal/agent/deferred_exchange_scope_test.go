package agent

// Loop-level proof that a Deferred Response Action is Exchange-scoped (item 7 / F6): a
// remaining-items directive queued mid-fan-out is expired whenever the Exchange ends — a terminal
// fault (abandonTurn) or an Esc-path AbortExchange — and is truncated-then-restored (never doubled)
// when a cancelled Turn is rolled back. These drive the guided_decomposition stack end-to-end
// through the real loop, using the directive marker as the discriminator: a stale directive would
// ride the NEXT Exchange's first request, and a doubled one would round-trip through the snapshot.

import (
	"context"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// errorScript is a stream that surfaces one terminal fault — the loop treats it as turnFailed and
// degrades the Turn to a clean Exchange-complete boundary (abandonTurn) with no assistant message.
func errorScript(msg string) []provider.Delta {
	return []provider.Delta{{Kind: provider.DeltaError, Err: msg}}
}

// gdDirectiveCount reports how many message contents of req carry the remaining-items directive
// marker — 1 for a single queued directive, 2 for two contradictory copies (the pre-fix defect).
func gdDirectiveCount(req provider.Request) int {
	n := 0
	for _, m := range req.Messages {
		n += strings.Count(m.Content, gdDirectiveMarker)
	}
	return n
}

// TestGuidedDecomposition_FaultMidFanOutExpiresDirective proves the fault half of F6: a terminal
// upstream fault mid-fan-out ends the Exchange, and the deferred remaining-items directive expires
// with it — a fresh ask in the NEXT Exchange is answered on its own terms with NO directive marker
// riding its first request. Without the abandonTurn clear the restored directive would survive into
// the new Exchange and steer the model back into the abandoned fan-out.
func TestGuidedDecomposition_FaultMidFanOutExpiresDirective(t *testing.T) {
	sink := &recordingSink{}
	cfg := gdConfig(t, sink)
	responder := &captureAllResponder{scripts: [][]provider.Delta{
		contentScript(gdEnumerationText()),                 // parent T0: enumeration → synthesized delegation of subtask 1
		contentScript("report A: entry points catalogued"), // child A (delegated subtask 1)
		errorScript("upstream boom mid-fan-out"),           // parent T1: a terminal fault ends the Exchange
		contentScript("Fresh answer to the follow-up ask"), // Exchange 2 T0: the new ask's answer
	}}

	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: gdOversizedInput()}); err != nil {
		t.Fatalf("Submit (Exchange 1): %v", err)
	}
	res, err := a.Run(context.Background())
	if err != nil {
		t.Fatalf("Run (Exchange 1): %v", err)
	}
	if res.Status != domain.StatusExchangeComplete {
		t.Fatalf("Exchange 1 status = %q, want the fault to end the Exchange", res.Status)
	}

	// Exchange 2: a fresh, modest ask (no signal A steer of its own).
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
		if gdRequestContains(r, gdDirectiveMarker) {
			t.Errorf("Exchange 2 request %d carried a stale remaining-items directive; the fault did not expire the queue", i)
		}
	}
	if !gdRequestContains(newReqs[0], followUp) {
		t.Error("Exchange 2's first request did not carry the new ask")
	}
}

// TestGuidedDecomposition_AbortExchangeMidFanOutExpiresDirective proves the abort half of F6: an
// Esc-path AbortExchange mid-fan-out scraps the Exchange and expires the deferred directive, so the
// next submitted ask is answered cleanly with no directive marker. Without the AbortExchange clear
// the queued directive would leak into the next Exchange.
func TestGuidedDecomposition_AbortExchangeMidFanOutExpiresDirective(t *testing.T) {
	sink := &recordingSink{}
	cfg := gdConfig(t, sink)
	responder := &captureAllResponder{scripts: [][]provider.Delta{
		contentScript(gdEnumerationText()),                 // parent T0: enumeration → synthesized delegation of subtask 1
		contentScript("report A: entry points catalogued"), // child A (delegated subtask 1)
		contentScript("Fresh answer after the abort"),      // Exchange 2 T0: the new ask's answer
	}}

	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: gdOversizedInput()}); err != nil {
		t.Fatalf("Submit (Exchange 1): %v", err)
	}
	// One Step opens the fan-out (enumeration intercepted, first child dispatched, directive
	// deferred) and leaves the Exchange OPEN — the mid-fan-out boundary an Esc abort lands on.
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
		if gdRequestContains(r, gdDirectiveMarker) {
			t.Errorf("Exchange 2 request %d carried a stale remaining-items directive; AbortExchange did not expire the queue", i)
		}
	}
	if !gdRequestContains(newReqs[0], followUp) {
		t.Error("Exchange 2's first request did not carry the new ask")
	}
}

// TestGuidedDecomposition_CancelDuringDelegationRestoresSingleDirective proves the cancel half of
// F6: a cancel while a follow-through Turn's delegation child is mid-stream rolls the Turn back and
// leaves EXACTLY ONE directive queued — the drained (2 left) one restored, not doubled with the
// (1 left) directive the cancelled Turn's own post-response hook had re-derived. The snapshot taken
// at the cancelled boundary round-trips that single directive, and the resumed first request carries
// it exactly once. Without the truncate-before-restore the queue would hold both copies.
func TestGuidedDecomposition_CancelDuringDelegationRestoresSingleDirective(t *testing.T) {
	sink := &recordingSink{}
	cfg := gdConfig(t, sink)
	responder := &blockAtResponder{
		scripts: [][]provider.Delta{
			contentScript(gdEnumerationText()),      // parent T0: enumeration → synthesized delegation of subtask 1
			contentScript("report A: catalogued"),   // child A (delegated subtask 1)
			subAgentCallScript("m2", gdSubtasks[1]), // parent T1: delegate subtask 2 (post-response re-derives 1 left)
			// call 3 is child B's Turn — it blocks until cancel (blockAt below).
		},
		blockAt: 3,
		started: make(chan struct{}),
	}

	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: gdOversizedInput()}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	// Step 0: the enumeration Turn opens the fan-out (directive 2 left queued, drained next Turn).
	if res, err := a.Step(context.Background()); err != nil {
		t.Fatalf("Step (enumeration Turn): %v", err)
	} else if res.Status != domain.StatusTurnComplete {
		t.Fatalf("enumeration Turn status = %q, want %q", res.Status, domain.StatusTurnComplete)
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
	if n := strings.Count(string(snap.State), gdDirectiveMarker); n != 1 {
		t.Fatalf("snapshot carried %d remaining-items directives, want exactly 1 (the restored drained one)", n)
	}
	if !strings.Contains(string(snap.State), gdDirectiveMarker+" (2 left)") {
		t.Error("the single restored directive is not the drained (2 left) one")
	}

	// Resume and re-attempt the Turn: the first resumed request drains the ONE restored directive.
	sink2 := &recordingSink{}
	cfg2 := gdConfig(t, sink2)
	resumeResponder := &captureAllResponder{scripts: [][]provider.Delta{
		subAgentCallScript("m2b", gdSubtasks[1]),         // re-attempt T1: re-delegate subtask 2
		contentScript("report B: endpoint spec drafted"), // child B
		subAgentCallScript("m3", gdSubtasks[2]),          // delegate subtask 3
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
	if n := gdDirectiveCount(resumeResponder.got[0]); n != 1 {
		t.Errorf("the re-attempted request carried %d directives, want exactly 1 (no contradictory copies)", n)
	}
	if !gdRequestContains(resumeResponder.got[0], gdDirectiveMarker+" (2 left)") {
		t.Error("the re-attempted request did not carry the restored (2 left) directive")
	}
}
