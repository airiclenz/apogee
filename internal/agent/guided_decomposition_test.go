package agent

// Loop-level end-to-end acceptance for the guided_decomposition Mechanism (ADR 0014, plan item 5):
// the whole stack driven through the REAL loop with nothing of the Mechanism mocked. The Mechanism is
// built through the production catalogue (wave1Registry → mechanisms.Build — the seam the config
// surface drives) and stacked with its Required peer tool_result_cap, so these tests prove the
// registry-built dispatch path end-to-end: an oversized primary call gets the enumeration steer; the
// model's list is intercepted into a REAL nested sub_agent fan-out serialized one delegation per Turn;
// the remaining-items directive rides the deferred-correction queue (and survives a snapshot/resume);
// Bypass is the silent control arm; and a cancel during a child rolls back only that parent Turn
// (ADR 0013 §5) with the Mechanism in the mix.

import (
	"context"
	"iter"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// gdSubtasks are three distinct, non-prefixing subtasks — distinct so the intercept's prefix match
// (dispatched task ⊒ enumeration item) consumes exactly one per Turn with no accidental collisions.
var gdSubtasks = []string{
	"Research the authentication module and list its entry points",
	"Draft the API endpoint specification for the login flow",
	"Write integration tests covering the login happy path",
}

// gdEnumerationText is the bare numbered list the steered model replies with — the enumeration the
// intercept parses (2..12 items) and the first-match cursor anchors on in honest history.
func gdEnumerationText() string {
	return "1. " + gdSubtasks[0] + "\n2. " + gdSubtasks[1] + "\n3. " + gdSubtasks[2]
}

// guided_decomposition's two fixed, contractual markers (its own vocabulary, embedded verbatim in the
// steer and the fan-out directive — internal/mechanisms). The loop-level proof matches on them rather
// than re-importing the unexported constants: they are the Mechanism's stable wire contract.
const (
	gdSteerMarker     = "Decomposition planning"
	gdDirectiveMarker = "Remaining decomposition subtasks"
)

// gdWindow is the discovered context window the tests run under. At 4 chars/token (uncalibrated) it
// allocates ~400 tokens to FileContext and ~960 to History, so a ~2.2k-char opening ask trips signal A
// (fresh user message over FileContext) while the whole fan-out stays under History — signal B never
// re-steers mid-Exchange.
const gdWindow = 2000

// gdOversizedInput is ~2.2k chars: comfortably over the FileContext allocation (~1600 chars) so the
// gate's signal A fires on Turn 1, yet under the History allocation (~3840 chars) even after the
// fan-out accumulates the child reports, so signal B stays quiet and no second steer is injected.
func gdOversizedInput() string { return strings.Repeat("decompose this large task into parts ", 60) }

// gdConfig wires the sub_agent recursion point, the discovered window, and the production-catalogue
// guided_decomposition + tool_result_cap stack (the Required peer) onto a fresh Config.
func gdConfig(t *testing.T, sink domain.EventSink) domain.Config {
	t.Helper()
	cfg := subAgentConfig(sink, domain.ModeAskBefore) // registers sub_agent so the fan-out has a target
	cfg.Context.MaxContextTokens = gdWindow
	cfg.Mechanisms = wave1Registry(t, "guided_decomposition", "tool_result_cap")
	return cfg
}

// gdFanOutScripts is the full run-ordered script the scriptedResponder replays across the parent AND
// its serialized children: parent enumerates → child A reports → parent delegates subtask 2 → child B
// reports → parent delegates subtask 3 → child C reports → parent synthesizes. The parent's delegating
// Turns emit a sub_agent call whose task is the subtask verbatim, so the intercept's prefix match
// shrinks the remainder by exactly that item.
func gdFanOutScripts() [][]provider.Delta {
	return [][]provider.Delta{
		contentScript(gdEnumerationText()),                           // parent T0: the bare enumeration
		contentScript("report A: entry points catalogued"),           // child A (delegated subtask 1)
		subAgentCallScript("m2", gdSubtasks[1]),                      // parent T1: model delegates subtask 2 per the directive
		contentScript("report B: endpoint spec drafted"),             // child B
		subAgentCallScript("m3", gdSubtasks[2]),                      // parent T2: model delegates subtask 3
		contentScript("report C: tests written"),                     // child C
		contentScript("Synthesis: all three subtasks are complete."), // parent T3: final no-tool answer
	}
}

// gdRequestsContaining returns the captured requests carrying substr in any message content.
func gdRequestsContaining(got []provider.Request, substr string) []provider.Request {
	var out []provider.Request
	for _, r := range got {
		for _, m := range r.Messages {
			if strings.Contains(m.Content, substr) {
				out = append(out, r)
				break
			}
		}
	}
	return out
}

// gdRequestContains reports whether any message content of req contains substr.
func gdRequestContains(req provider.Request, substr string) bool {
	for _, m := range req.Messages {
		if strings.Contains(m.Content, substr) {
			return true
		}
	}
	return false
}

// gdToolResultContents collects the content of every RoleTool message committed to conv — the
// child reports as they land in the parent's honest history.
func gdToolResultContents(conv *domain.Conversation) []string {
	var out []string
	for _, m := range conv.Messages() {
		if m.Role == domain.RoleTool {
			out = append(out, m.Content)
		}
	}
	return out
}

// gdMessageEventDepth returns the Depth of the first MessageEvent whose Text equals text, or -1.
func gdMessageEventDepth(events []domain.Event, text string) int {
	for _, e := range events {
		if me, ok := e.(domain.MessageEvent); ok && me.Text == text {
			return me.Depth
		}
	}
	return -1
}

// TestGuidedDecomposition_EndToEndFanOut is the whole-stack acceptance: an oversized primary call is
// steered to enumerate, the enumeration is intercepted into a REAL nested sub_agent fan-out serialized
// one delegation per Turn, the remaining-items directive rides each following request and shrinks, and
// the Exchange ends on a no-tool synthesis with the enumeration verbatim and all three child reports in
// honest history. Nothing of the Mechanism is mocked.
func TestGuidedDecomposition_EndToEndFanOut(t *testing.T) {
	sink := &recordingSink{}
	cfg := gdConfig(t, sink)
	responder := &captureAllResponder{scripts: gdFanOutScripts()}

	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: gdOversizedInput()}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	res, err := a.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Status != domain.StatusExchangeComplete {
		t.Fatalf("final status = %q, want the Exchange to complete", res.Status)
	}

	// The Mechanism acted through the real loop: the pre-request steer AND the post-response intercept
	// both booked fires under the catalogued ID.
	if fireCountFor(sink.events, "guided_decomposition") == 0 {
		t.Fatal("no guided_decomposition MechanismFiredEvent; the Mechanism never acted through the loop")
	}

	// Turn 1's request carried the enumeration steer (measured signal A on the oversized message).
	steered := gdRequestsContaining(responder.got, gdSteerMarker)
	if len(steered) == 0 {
		t.Errorf("no request carried the enumeration steer %q", gdSteerMarker)
	}

	// The synthesized first delegation dispatched a REAL nested child, and its report event nests at
	// Depth 1 while the parent's synthesis stays at Depth 0 (ADR 0013).
	if d := gdMessageEventDepth(sink.events, "report A: entry points catalogued"); d != 1 {
		t.Errorf("child A report event Depth = %d, want 1 (a real nested sub-agent)", d)
	}
	if d := gdMessageEventDepth(sink.events, "Synthesis: all three subtasks are complete."); d != 0 {
		t.Errorf("parent synthesis event Depth = %d, want 0", d)
	}

	// The three subtasks each dispatched exactly one sub_agent delegation (one per Turn, serialized).
	subCalls := 0
	for _, c := range dispatchedCalls(sink.events) {
		if c.Tool == "sub_agent" {
			subCalls++
		}
	}
	if subCalls != 3 {
		t.Errorf("dispatched %d sub_agent calls, want 3 (one serialized delegation per subtask)", subCalls)
	}

	// The next request carried the deferred remaining-items directive (2 left), and a later request
	// carried the SHRUNKEN directive (1 left) — the cursor advanced one subtask per Turn.
	if got := gdRequestsContaining(responder.got, gdDirectiveMarker+" (2 left)"); len(got) == 0 {
		t.Error("no request carried the initial remaining-items directive (2 left)")
	} else if !gdRequestContains(got[0], gdSubtasks[1]) || !gdRequestContains(got[0], gdSubtasks[2]) {
		t.Error("the (2 left) directive did not list the two outstanding subtasks verbatim")
	}
	if got := gdRequestsContaining(responder.got, gdDirectiveMarker+" (1 left)"); len(got) == 0 {
		t.Error("the remaining-items directive did not shrink to (1 left) after the second delegation")
	}

	// Honest history: the enumeration text is verbatim (never trimmed — locked decision 4) and all
	// three child reports are committed as tool results.
	sawEnumeration := false
	for _, m := range a.conv.Messages() {
		if m.Role == domain.RoleAssistant && m.Content == gdEnumerationText() {
			sawEnumeration = true
		}
	}
	if !sawEnumeration {
		t.Error("the enumeration message is not in honest history verbatim")
	}
	results := gdToolResultContents(&a.conv)
	for _, want := range []string{"report A: entry points catalogued", "report B: endpoint spec drafted", "report C: tests written"} {
		found := false
		for _, r := range results {
			if strings.Contains(r, want) {
				found = true
			}
		}
		if !found {
			t.Errorf("child report %q is missing from honest history; results = %v", want, results)
		}
	}
}

// TestGuidedDecomposition_SnapshotMidFanOutRoundTripsDirective proves the remaining-items directive is
// snapshot/resume-safe: a snapshot taken at the quiescent boundary after the first delegation carries
// the pending directive through conversationJSON.Deferred, and a resumed Agent drains it into the next
// request and completes the fan-out (locked decision 1 — no per-Mechanism state to serialize).
func TestGuidedDecomposition_SnapshotMidFanOutRoundTripsDirective(t *testing.T) {
	sink := &recordingSink{}
	cfg := gdConfig(t, sink)
	// Only the enumeration Turn + its child run before the snapshot boundary.
	responder := &scriptedResponder{scripts: [][]provider.Delta{
		contentScript(gdEnumerationText()),                 // parent T0
		contentScript("report A: entry points catalogued"), // child A
	}}

	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: gdOversizedInput()}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	res, err := a.Step(context.Background()) // Turn 0: enumeration intercepted, first child dispatched
	if err != nil {
		t.Fatalf("Step (enumeration Turn): %v", err)
	}
	if res.Status != domain.StatusTurnComplete {
		t.Fatalf("enumeration Turn status = %q, want %q (the fan-out is mid-flight)", res.Status, domain.StatusTurnComplete)
	}

	snap, err := a.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	// The pending directive is serialized in the conversation state (conversationJSON.Deferred).
	if !strings.Contains(string(snap.State), gdDirectiveMarker) {
		t.Fatalf("snapshot State does not carry the pending directive marker %q", gdDirectiveMarker)
	}

	// Resume into a fresh Agent (fresh registry + sink) and continue the open Exchange. The resumed
	// responder picks up at the second delegation.
	sink2 := &recordingSink{}
	cfg2 := gdConfig(t, sink2)
	resumeResponder := &captureAllResponder{scripts: [][]provider.Delta{
		subAgentCallScript("m2", gdSubtasks[1]),               // parent T1 (resumed): delegate subtask 2
		contentScript("report B: endpoint spec drafted"),      // child B
		subAgentCallScript("m3", gdSubtasks[2]),               // parent T2: delegate subtask 3
		contentScript("report C: tests written"),              // child C
		contentScript("Synthesis: resumed fan-out complete."), // parent T3
	}}
	b, err := resumeAgent(cfg2, snap, resumeResponder)
	if err != nil {
		t.Fatalf("resumeAgent: %v", err)
	}
	res2, err := b.Run(context.Background())
	if err != nil {
		t.Fatalf("Run (resumed): %v", err)
	}
	if res2.Status != domain.StatusExchangeComplete {
		t.Fatalf("resumed status = %q, want the Exchange to complete", res2.Status)
	}
	// The very first resumed request drained the round-tripped directive.
	if len(resumeResponder.got) == 0 || !gdRequestContains(resumeResponder.got[0], gdDirectiveMarker) {
		t.Error("the resumed first request did not carry the round-tripped remaining-items directive")
	}
}

// TestGuidedDecomposition_BypassIsSilentControlArm is the ADR 0014 §1 control arm: under Bypass the
// proactive-nudge Mechanism is skipped at every hook point, so the IDENTICAL script produces zero
// guided_decomposition activity — no steer, no synthesized delegation, no fan-out — and the Exchange
// ends on the model's own enumeration reply.
func TestGuidedDecomposition_BypassIsSilentControlArm(t *testing.T) {
	sink := &recordingSink{}
	cfg := gdConfig(t, sink)
	cfg.Bypass = true
	responder := &captureAllResponder{scripts: gdFanOutScripts()}

	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: gdOversizedInput()}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	res, err := a.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Status != domain.StatusExchangeComplete {
		t.Fatalf("status = %q, want the Exchange to complete", res.Status)
	}

	if fireCountFor(sink.events, "guided_decomposition") != 0 {
		t.Error("guided_decomposition acted under Bypass; a proactive-nudge Mechanism must be silent")
	}
	if len(gdRequestsContaining(responder.got, gdSteerMarker)) != 0 {
		t.Error("an enumeration steer was injected under Bypass")
	}
	for _, c := range dispatchedCalls(sink.events) {
		if c.Tool == "sub_agent" {
			t.Error("a sub_agent fan-out was synthesized under Bypass")
		}
	}
	// The Exchange ended on the model's own enumeration reply — no interception happened.
	if me, ok := lastMessageEvent(sink.events); !ok || me.Text != gdEnumerationText() {
		t.Errorf("final message = %+v (ok=%v), want the un-intercepted enumeration reply", me, ok)
	}
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

// TestGuidedDecomposition_CancelDuringChildRollsBackParentTurn proves the existing ADR 0013 §5
// atomic-within-the-Turn semantics stay green with guided_decomposition in the mix: a cancel while the
// first synthesized delegation's child is mid-stream rolls the parent Turn all the way back to the
// pre-request boundary (only the user message survives — no assistant message, no partial child
// result) and leaves resumable state that completes the Exchange.
func TestGuidedDecomposition_CancelDuringChildRollsBackParentTurn(t *testing.T) {
	sink := &recordingSink{}
	cfg := gdConfig(t, sink)
	responder := &blockAtResponder{
		scripts: [][]provider.Delta{
			contentScript(gdEnumerationText()), // parent T0: enumeration (call 0)
			// call 1 is child A's Turn — it blocks until cancel (blockAt below).
		},
		blockAt: 1,
		started: make(chan struct{}),
	}

	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: gdOversizedInput()}); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-responder.started // the child's Turn is in flight
		cancel()
	}()
	res, err := a.Step(ctx)
	if err != nil {
		t.Fatalf("Step returned a loop error on cancel: %v", err)
	}
	if res.Status != domain.StatusCancelled {
		t.Fatalf("Step status = %q, want %q (a cancelled child unwinds the parent Turn)", res.Status, domain.StatusCancelled)
	}

	// Only the user message survived: the assistant tool-call message and any partial child result
	// were rolled back (ADR 0013 §5 — no partial sub-agent result is surfaced).
	if got := a.conv.Len(); got != 1 {
		t.Errorf("committed conversation has %d messages after the cancel, want 1 (just the user message)", got)
	}
	if n := len(gdToolResultContents(&a.conv)); n != 0 {
		t.Errorf("committed %d tool results after the cancel, want 0 (the child work is discarded)", n)
	}

	// The snapshot is resumable: resume against a completing responder and finish the Exchange.
	snap, err := a.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot after cancel: %v", err)
	}
	sink2 := &recordingSink{}
	cfg2 := gdConfig(t, sink2)
	b, err := resumeAgent(cfg2, snap, echoResponder{reply: "recovered after cancel"})
	if err != nil {
		t.Fatalf("resumeAgent after cancel: %v", err)
	}
	res2, err := b.Run(context.Background())
	if err != nil {
		t.Fatalf("Run (resumed after cancel): %v", err)
	}
	if res2.Status != domain.StatusExchangeComplete {
		t.Errorf("resumed status = %q, want the Exchange to complete", res2.Status)
	}
}
