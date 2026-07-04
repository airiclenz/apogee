package agent

// White-box tests for per-Session self-regulation (Phase-4 item 3, amended by
// phase-4-review-fixes item 4 — R3/R4): next-Turn judgment on the four proxy signals,
// Adaptive Suppression, the global Turn Budget, and acted-fire booking. The selfRegulator
// state machine is proven directly (fast, deterministic), then the loop wiring is proven
// end-to-end through Step — a harmful session suppresses at dispatch, a pure Q&A session
// strikes nothing, a productive tool Turn clears through dispatchTools → noteToolProductivity
// → endTurn, an inspect-only invocation is never booked, a cancelled Turn's re-attempt
// regains its read-novelty credit, and the tracker resets on Resume.

import (
	"context"
	"fmt"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// regMech is a bare Mechanism carrying only a descriptor — enough to exercise selfRegulator.suppress
// (which reads only the descriptor). It implements no hook interface, so it is never registered.
type regMech struct {
	id  domain.MechanismID
	pol domain.SuppressionPolicy
}

func (m regMech) Descriptor() domain.MechanismDescriptor {
	return domain.MechanismDescriptor{ID: m.id, Suppression: m.pol}
}
func (regMech) Ordering() domain.OrderingConstraints { return domain.OrderingConstraints{} }

// countingMech is a catalogued pre-request Mechanism with a configurable SuppressionPolicy that
// counts how many times it was actually DISPATCHED (invoked) — the input for the loop-level
// self-regulation tests. By default it mutates the request so every invocation is an acted fire
// (R4); inspectOnly makes it a no-op invocation that must never be booked.
type countingMech struct {
	id          domain.MechanismID
	cap         domain.Capability
	pol         domain.SuppressionPolicy
	inspectOnly bool
	fired       *int
}

func (m countingMech) Descriptor() domain.MechanismDescriptor {
	return domain.MechanismDescriptor{ID: m.id, Capability: m.cap, Suppression: m.pol}
}
func (countingMech) Ordering() domain.OrderingConstraints { return domain.OrderingConstraints{} }
func (m countingMech) PreRequest(_ context.Context, req *domain.Request) error {
	*m.fired++
	if !m.inspectOnly {
		marker := "[selfreg-test " + string(m.id) + "]"
		req.AppendToSystem(marker, marker+" stay focused")
	}
	return nil
}

// firedReaderHook is an experimental pre-request hook that captures LoopView.Fired for a Mechanism
// ID — the probe proving Fired answers from the tracker (and sees a same-pass catalogued fire).
type firedReaderHook struct {
	id   domain.MechanismID
	seen *int
}

func (h firedReaderHook) PreRequest(_ context.Context, req *domain.Request) error {
	*h.seen = req.View().Fired(h.id)
	return nil
}

func stepOnce(t *testing.T, a *Agent, text string) domain.StepResult {
	t.Helper()
	if err := a.Submit(domain.UserInput{Text: text}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	res, err := a.Step(context.Background())
	if err != nil {
		t.Fatalf("Step: %v", err)
	}
	return res
}

// ---------------------------------------------------------------------------
// selfRegulator state machine
// ---------------------------------------------------------------------------

// TestSelfRegulatorFireJudgedByNextTurn proves the R3 core: a fire in Turn N is judged by Turn
// N+1's outcome — N's own outcome does not touch it.
func TestSelfRegulatorFireJudgedByNextTurn(t *testing.T) {
	r := newSelfRegulator()
	// Turn N: x fires and the Turn itself is harmful — x's fire is still pending.
	r.recordFire("x")
	r.noteToolError()
	r.endTurn()
	if got := r.strikes["x"]; got != 0 {
		t.Fatalf("Turn N's own outcome struck its fires (strikes=%d); judgment is next-Turn", got)
	}
	// Turn N+1: harmful with no fires of its own — NOW x's pending fire is struck.
	r.noteToolError()
	r.endTurn()
	if got := r.strikes["x"]; got != 1 {
		t.Fatalf("strikes[x] = %d after a harmful Turn N+1, want 1", got)
	}
}

func TestSelfRegulatorStrikesThenSuppressed(t *testing.T) {
	r := newSelfRegulator()
	m := regMech{id: "x"}
	// x fires every Turn and every Turn is harmful. Turn i's fire is struck at Turn i+1's
	// end (next-Turn judgment), so the strike limit is reached after strikes+1 Turns.
	for i := 0; i < adaptiveSuppressStrikes+1; i++ {
		if r.suppress(m) {
			t.Fatalf("Mechanism suppressed after only %d Turns (< %d harmful judgments)", i, adaptiveSuppressStrikes)
		}
		r.recordFire("x")
		r.noteToolError()
		r.endTurn()
	}
	if !r.suppress(m) {
		t.Fatalf("Mechanism not suppressed after %d consecutive harmful judgments", adaptiveSuppressStrikes)
	}
}

func TestSelfRegulatorClearPathReopens(t *testing.T) {
	r := newSelfRegulator()
	m := regMech{id: "x"}
	for i := 0; i < adaptiveSuppressStrikes+1; i++ {
		r.recordFire("x")
		r.noteToolError()
		r.endTurn()
	}
	if !r.suppress(m) {
		t.Fatal("precondition: Mechanism should be suppressed")
	}
	// A productive Turn is the clear-path: it re-opens the suppressed Mechanism.
	r.noteWrite()
	r.endTurn()
	if r.suppress(m) {
		t.Fatal("a productive Turn did not re-open the suppressed Mechanism")
	}
}

// TestSelfRegulatorNeutralFreezes proves the three-way outcome's middle: a neutral Turn
// neither strikes nor advances the streak nor clears — everything freezes (R3).
func TestSelfRegulatorNeutralFreezes(t *testing.T) {
	r := newSelfRegulator()
	// Two harmful Turns with x firing: strikes[x]=1, streak=2.
	r.recordFire("x")
	r.noteToolError()
	r.endTurn()
	r.recordFire("x")
	r.noteToolError()
	r.endTurn()
	if r.strikes["x"] != 1 || r.harmfulStreak != 2 {
		t.Fatalf("precondition: strikes[x]=%d streak=%d, want 1/2", r.strikes["x"], r.harmfulStreak)
	}
	// A neutral Turn (no signals): freeze.
	r.recordFire("x")
	r.endTurn()
	if got := r.strikes["x"]; got != 1 {
		t.Errorf("a neutral Turn changed strikes[x] to %d, want frozen at 1", got)
	}
	if got := r.harmfulStreak; got != 2 {
		t.Errorf("a neutral Turn changed the streak to %d, want frozen at 2", got)
	}
	if r.budgetTripped {
		t.Error("a neutral Turn tripped the Turn Budget")
	}
}

// TestSelfRegulatorProductiveWinsMixedSignals: when a Turn shows both a productive and a
// harmful signal, productive wins (R3).
func TestSelfRegulatorProductiveWinsMixedSignals(t *testing.T) {
	r := newSelfRegulator()
	r.noteToolError()
	r.noteWrite()
	if got := r.judgment(); got != judgedProductive {
		t.Fatalf("judgment with mixed signals = %v, want judgedProductive", got)
	}
	// And an empty-response signal alone is harmful.
	r.resetTurnScratch()
	r.noteEmptyResponse()
	if got := r.judgment(); got != judgedHarmful {
		t.Fatalf("judgment with an empty-response signal = %v, want judgedHarmful", got)
	}
}

func TestSelfRegulatorExemptNeverSuppressed(t *testing.T) {
	r := newSelfRegulator()
	m := regMech{id: "e", pol: domain.SuppressExempt}
	// Enough harmful Turns to trip BOTH Adaptive Suppression and the Turn Budget.
	for i := 0; i < turnBudgetLimit+adaptiveSuppressStrikes; i++ {
		r.recordFire("e")
		r.noteToolError()
		r.endTurn()
	}
	if !r.budgetTripped {
		t.Fatal("precondition: the Turn Budget should have tripped")
	}
	if r.suppress(m) {
		t.Fatal("an exempt Mechanism was suppressed by Adaptive Suppression or the Turn Budget")
	}
}

func TestSelfRegulatorTurnBudgetTripsAndClears(t *testing.T) {
	r := newSelfRegulator()
	// y never fires, so it accrues no strikes — only the global Turn Budget can withdraw it.
	y := regMech{id: "y"}
	exempt := regMech{id: "e", pol: domain.SuppressExempt}
	for i := 0; i < turnBudgetLimit; i++ {
		if r.budgetTripped {
			t.Fatalf("Turn Budget tripped early after %d harmful Turns", i)
		}
		r.noteToolError()
		r.endTurn()
	}
	if !r.budgetTripped {
		t.Fatalf("Turn Budget did not trip after %d harmful Turns", turnBudgetLimit)
	}
	if !r.suppress(y) {
		t.Fatal("a tripped Turn Budget did not withdraw a non-exempt Mechanism with no strikes")
	}
	if r.suppress(exempt) {
		t.Fatal("a tripped Turn Budget withdrew an exempt Mechanism")
	}
	// A productive Turn clears the global withdrawal.
	r.noteRead(domain.ToolCall{Tool: "read_file", Arguments: []byte(`{"path":"a.go"}`)})
	r.endTurn()
	if r.budgetTripped {
		t.Fatal("a productive Turn did not clear the Turn Budget")
	}
	if r.suppress(y) {
		t.Fatal("the Mechanism is still withdrawn after the Turn Budget cleared")
	}
}

func TestSelfRegulatorNewReadVsRepeatRead(t *testing.T) {
	r := newSelfRegulator()
	call := domain.ToolCall{Tool: "read_file", Arguments: []byte(`{"path":"a.go"}`)}
	r.noteRead(call)
	if !r.sawProductive {
		t.Fatal("a new file read should be the productive signal")
	}
	r.endTurn() // commits the Turn: the read stays seen, the per-Turn flags reset
	r.noteRead(call)
	if r.sawProductive {
		t.Fatal("a repeat read of the same target should not be a productive signal")
	}
	r.noteRead(domain.ToolCall{Tool: "read_file", Arguments: []byte(`{"path":"b.go"}`)})
	if !r.sawProductive {
		t.Fatal("a read of a new path should be a productive signal")
	}
}

// TestSelfRegulatorDiscardRollsBackReadNovelty: a cancelled Turn's discard restores the
// novelty credit of the reads it booked, so the mandated re-attempt is not penalized —
// while a committed Turn (endTurn) keeps them seen.
func TestSelfRegulatorDiscardRollsBackReadNovelty(t *testing.T) {
	r := newSelfRegulator()
	call := domain.ToolCall{Tool: "read_file", Arguments: []byte(`{"path":"a.go"}`)}
	r.noteRead(call)
	r.discardTurn() // the cancelled Turn's rollback
	r.noteRead(call)
	if !r.sawProductive {
		t.Fatal("the re-attempt's read of a rolled-back target is not novel again")
	}
	r.endTurn() // the re-attempt commits
	r.noteRead(call)
	if r.sawProductive {
		t.Fatal("a committed Turn's read stayed novel after endTurn")
	}
}

// TestSelfRegulatorDiscardLeavesPendingInPlace: a cancelled Turn's own fires evaporate, but
// the PREVIOUS Turn's pending fires stay — the re-attempt's outcome judges them (R3).
func TestSelfRegulatorDiscardLeavesPendingInPlace(t *testing.T) {
	r := newSelfRegulator()
	r.recordFire("x")
	r.endTurn() // x is pending
	r.recordFire("y")
	r.discardTurn() // the Turn with y's fire is cancelled — y evaporates, x stays pending
	r.noteToolError()
	r.endTurn() // the harmful re-attempt judges the pending set
	if got := r.strikes["x"]; got != 1 {
		t.Errorf("strikes[x] = %d, want 1 (pending fires survive a discarded Turn)", got)
	}
	if got := r.strikes["y"]; got != 0 {
		t.Errorf("strikes[y] = %d, want 0 (a discarded Turn's own fires are not judged)", got)
	}
}

// ---------------------------------------------------------------------------
// Loop wiring
// ---------------------------------------------------------------------------

// TestSelfRegulationSuppressesAtDispatch proves the loop consults the tracker under the
// next-Turn model: with every Turn harmful (empty final responses), a non-exempt Mechanism
// firing each Turn is withdrawn once its fires collect the strike limit — Turn i's fire is
// struck at Turn i+1, so it dispatches strikes+1 times and never again. This also proves
// the empty-response signal is wired loop-level (step → noteEmptyResponse).
func TestSelfRegulationSuppressesAtDispatch(t *testing.T) {
	cfg := baseConfig(&recordingSink{})
	cfg.Mechanisms = domain.NewMechanismRegistry()
	fired := 0
	mustAddMech(t, cfg.Mechanisms, countingMech{id: "nudge", cap: domain.CapProactiveNudge, pol: domain.SuppressStrikesThree, fired: &fired})

	a, err := newAgent(cfg, echoResponder{reply: ""}) // an empty final reply ⇒ every Turn harmful
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	const turns = adaptiveSuppressStrikes + 3
	for i := 0; i < turns; i++ {
		stepOnce(t, a, "go")
	}
	if fired != adaptiveSuppressStrikes+1 {
		t.Errorf("Mechanism dispatched %d times across %d harmful Turns, want %d (withdrawn after %d strikes)",
			fired, turns, adaptiveSuppressStrikes+1, adaptiveSuppressStrikes)
	}
}

// TestExemptFiresThroughSuppression proves an exempt off-ramp is never withdrawn — it fires every
// Turn even past the strike limit and through the tripped Turn Budget of an all-harmful session.
func TestExemptFiresThroughSuppression(t *testing.T) {
	cfg := baseConfig(&recordingSink{})
	cfg.Mechanisms = domain.NewMechanismRegistry()
	fired := 0
	mustAddMech(t, cfg.Mechanisms, countingMech{id: "offramp", cap: domain.CapOffRamp, pol: domain.SuppressExempt, fired: &fired})

	a, err := newAgent(cfg, echoResponder{reply: ""}) // every Turn harmful
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	const turns = turnBudgetLimit + 2
	for i := 0; i < turns; i++ {
		stepOnce(t, a, "go")
	}
	if !a.tracker.budgetTripped {
		t.Error("the Turn Budget did not trip on an all-harmful (empty-response) session")
	}
	if fired != turns {
		t.Errorf("exempt Mechanism fired %d times across %d Turns, want %d (never suppressed)", fired, turns, turns)
	}
}

// TestPureQAndANeverStrikesNorTrips is R3's point: a session of substantive text-only
// answers (neutral Turns) neither strikes a firing Mechanism nor trips the Turn Budget.
func TestPureQAndANeverStrikesNorTrips(t *testing.T) {
	cfg := baseConfig(&recordingSink{})
	cfg.Mechanisms = domain.NewMechanismRegistry()
	fired := 0
	mustAddMech(t, cfg.Mechanisms, countingMech{id: "nudge", cap: domain.CapProactiveNudge, pol: domain.SuppressStrikesThree, fired: &fired})

	a, err := newAgent(cfg, echoResponder{reply: "here is a substantive answer"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	const turns = turnBudgetLimit + 2
	for i := 0; i < turns; i++ {
		stepOnce(t, a, "another question")
	}
	if fired != turns {
		t.Errorf("Mechanism dispatched %d times across %d neutral Turns, want %d (a Q&A session must not withdraw it)",
			fired, turns, turns)
	}
	if a.tracker.budgetTripped {
		t.Error("a pure Q&A session tripped the Turn Budget")
	}
	if got := a.tracker.strikes["nudge"]; got != 0 {
		t.Errorf("a pure Q&A session accrued %d strikes, want 0", got)
	}
}

// TestNoOpInvocationNotBooked proves R4 at the loop level: an inspect-and-do-nothing
// catalogued invocation is not a fire — no MechanismFiredEvent, Fired == 0, no strikes —
// and therefore it is never withdrawn even through an all-harmful session.
func TestNoOpInvocationNotBooked(t *testing.T) {
	sink := &recordingSink{}
	cfg := baseConfig(sink)
	cfg.Mechanisms = domain.NewMechanismRegistry()
	invoked := 0
	mustAddMech(t, cfg.Mechanisms, countingMech{id: "watcher", cap: domain.CapProactiveNudge, pol: domain.SuppressStrikesThree, inspectOnly: true, fired: &invoked})

	a, err := newAgent(cfg, echoResponder{reply: ""}) // every Turn harmful
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	const turns = adaptiveSuppressStrikes + 2
	for i := 0; i < turns; i++ {
		stepOnce(t, a, "go")
	}
	if invoked != turns {
		t.Errorf("inspect-only Mechanism dispatched %d times, want %d (unbooked invocations accrue no strikes)", invoked, turns)
	}
	for _, fe := range mechanismFires(sink.events) {
		if fe.Mechanism == "watcher" {
			t.Errorf("a no-op invocation emitted a MechanismFiredEvent: %+v", fe)
		}
	}
	if got := a.tracker.fireCounts["watcher"]; got != 0 {
		t.Errorf("Fired(watcher) = %d, want 0 (fired counts actions, not invocations)", got)
	}
	if got := a.tracker.strikes["watcher"]; got != 0 {
		t.Errorf("strikes[watcher] = %d, want 0 (an unbooked invocation is never judged)", got)
	}
}

// TestFiredCountsVisibleToHook proves an ACTING Mechanism is booked — LoopView.Fired answers
// from the tracker, live within one hook pass (a catalogued acted fire is visible to an
// experimental hook firing after it), and the MechanismFiredEvent carries its ID.
func TestFiredCountsVisibleToHook(t *testing.T) {
	sink := &recordingSink{}
	cfg := baseConfig(sink)
	cfg.Mechanisms = domain.NewMechanismRegistry()
	greetFired := 0
	mustAddMech(t, cfg.Mechanisms, countingMech{id: "greet", cap: domain.CapProactiveNudge, fired: &greetFired})
	seen := -1
	if err := cfg.Mechanisms.AddExperimental(domain.HookPreRequest, firedReaderHook{id: "greet", seen: &seen}); err != nil {
		t.Fatalf("AddExperimental: %v", err)
	}

	a, err := newAgent(cfg, echoResponder{reply: "ok"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	stepOnce(t, a, "go")

	if seen != 1 {
		t.Errorf("hook read Fired(greet) = %d, want 1 (the catalogued acted fire from the same pass)", seen)
	}
	found := false
	for _, fe := range mechanismFires(sink.events) {
		if fe.Mechanism == "greet" {
			found = true
		}
	}
	if !found {
		t.Error("no MechanismFiredEvent was emitted for the acting catalogued Mechanism")
	}
}

// TestToolErrorsTripBudgetAndWithdrawAtDispatch drives the loop end-to-end on erroring tool
// calls: each error Turn is harmful (dispatchTools → noteToolProductivity → endTurn), the
// Turn Budget trips at the limit, and at dispatch a non-exempt Mechanism is withdrawn while
// an exempt one still fires.
func TestToolErrorsTripBudgetAndWithdrawAtDispatch(t *testing.T) {
	sink := &recordingSink{}
	poke := fakeTool{name: "poke", readOnly: true, execute: func(_ context.Context, call domain.ToolCall) (domain.ToolResult, error) {
		return domain.ToolResult{CallID: call.ID, Content: "boom", IsError: true}, nil
	}}
	cfg := configWithTools(sink, poke)
	cfg.Mechanisms = domain.NewMechanismRegistry()
	nudged, offed := 0, 0
	// The non-exempt Mechanism is inspect-only, so it accrues NO strikes (R4) — its
	// withdrawal below is attributable to the Turn Budget alone.
	mustAddMech(t, cfg.Mechanisms, countingMech{id: "nudge", cap: domain.CapProactiveNudge, pol: domain.SuppressStrikesThree, inspectOnly: true, fired: &nudged})
	mustAddMech(t, cfg.Mechanisms, countingMech{id: "off", cap: domain.CapOffRamp, pol: domain.SuppressExempt, inspectOnly: true, fired: &offed})

	// Each Exchange: one erroring tool Turn (harmful) + one text Turn (neutral — freezes).
	var scripts [][]provider.Delta
	for i := 0; i < turnBudgetLimit; i++ {
		scripts = append(scripts, toolCallScript(fmt.Sprintf("p%d", i), "poke", `{}`), contentScript("still trying"))
	}
	scripts = append(scripts, contentScript("idle"))
	responder := &scriptedResponder{scripts: scripts}

	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	for i := 0; i < turnBudgetLimit; i++ {
		runExchange(t, a, "poke it")
	}

	if !a.tracker.budgetTripped {
		t.Fatalf("the Turn Budget did not trip after %d erroring-tool Turns", turnBudgetLimit)
	}
	// The trip lands at the end of the last error Turn, so the non-exempt Mechanism missed
	// exactly the final text Turn; the exempt one was dispatched on every Turn.
	wantNudged, wantOffed := 2*turnBudgetLimit-1, 2*turnBudgetLimit
	if nudged != wantNudged {
		t.Errorf("non-exempt Mechanism dispatched %d times, want %d (withdrawn once the budget tripped)", nudged, wantNudged)
	}
	if offed != wantOffed {
		t.Errorf("exempt Mechanism dispatched %d times, want %d (never withdrawn)", offed, wantOffed)
	}

	// One more (neutral) Exchange: the budget stays tripped — the non-exempt Mechanism stays
	// withdrawn at dispatch, the exempt one still fires.
	runExchange(t, a, "anything left?")
	if nudged != wantNudged {
		t.Errorf("non-exempt Mechanism dispatched %d times after the trip, want still %d", nudged, wantNudged)
	}
	if offed != wantOffed+1 {
		t.Errorf("exempt Mechanism dispatched %d times after the trip, want %d", offed, wantOffed+1)
	}
}

// TestProductiveTurnClearsThroughDispatch drives the clear-path end-to-end: a real dispatched
// tool call (a novel read) marks the Turn productive through dispatchTools →
// noteToolProductivity → endTurn, clearing the strikes, the suppression, and the tripped
// Turn Budget — so the withdrawn Mechanism fires again on the very next Turn.
func TestProductiveTurnClearsThroughDispatch(t *testing.T) {
	sink := &recordingSink{}
	ran := 0
	cfg := configWithTools(sink, fakeTool{name: "read_file", readOnly: true, ran: &ran, result: "data"})
	cfg.Mechanisms = domain.NewMechanismRegistry()
	fired := 0
	mustAddMech(t, cfg.Mechanisms, countingMech{id: "nudge", cap: domain.CapProactiveNudge, pol: domain.SuppressStrikesThree, fired: &fired})
	responder := &scriptedResponder{scripts: [][]provider.Delta{
		toolCallScript("c1", "read_file", `{"path":"a.go"}`),
		contentScript("done"),
	}}

	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	// Simulate a Session gone bad: the Mechanism struck out AND the Turn Budget tripped.
	a.tracker.strikes["nudge"] = adaptiveSuppressStrikes
	a.tracker.suppressed["nudge"] = true
	a.tracker.harmfulStreak = turnBudgetLimit
	a.tracker.budgetTripped = true

	runExchange(t, a, "read the file")

	if ran != 1 {
		t.Fatalf("read_file ran %d times, want 1", ran)
	}
	if a.tracker.budgetTripped {
		t.Error("the productive tool Turn did not clear the Turn Budget")
	}
	if got := a.tracker.harmfulStreak; got != 0 {
		t.Errorf("harmfulStreak = %d after the productive Turn, want 0", got)
	}
	if got := a.tracker.strikes["nudge"]; got != 0 {
		t.Errorf("strikes[nudge] = %d after the productive Turn, want 0", got)
	}
	// The Turn after the productive one dispatched the re-opened Mechanism (the tool Turn
	// itself still had it withdrawn under the tripped budget).
	if fired != 1 {
		t.Errorf("re-opened Mechanism dispatched %d times, want 1 (on the Turn after the clear)", fired)
	}
}

// TestCancelledTurnReattemptRegainsNovelty: a Turn cancelled mid-dispatch AFTER a novel read
// rolls the read's novelty back (cancelTurn → discardTurn), so the mandated re-attempt earns
// the productive signal again. The re-attempt also carries a harmful signal (the second tool
// errors), so the cleared budget below is attributable to the restored novelty alone.
func TestCancelledTurnReattemptRegainsNovelty(t *testing.T) {
	sink := &recordingSink{}
	ran := 0
	var cancelStep context.CancelFunc
	slowCalls := 0
	slow := fakeTool{name: "slow", readOnly: true, execute: func(ctx context.Context, call domain.ToolCall) (domain.ToolResult, error) {
		slowCalls++
		if slowCalls == 1 {
			cancelStep() // cancel mid-dispatch, after read_file already ran
			return domain.ToolResult{}, ctx.Err()
		}
		return domain.ToolResult{CallID: call.ID, Content: "transient failure", IsError: true}, nil
	}}
	cfg := configWithTools(sink,
		fakeTool{name: "read_file", readOnly: true, ran: &ran, result: "data"},
		slow,
	)
	twoCalls := []provider.Delta{
		{Kind: provider.DeltaToolCall, ToolCall: &provider.ToolCall{
			ID: "r1", Type: "function",
			Function: provider.FunctionCall{Name: "read_file", Arguments: `{"path":"a.go"}`},
		}},
		{Kind: provider.DeltaToolCall, ToolCall: &provider.ToolCall{
			ID: "s1", Type: "function",
			Function: provider.FunctionCall{Name: "slow", Arguments: `{}`},
		}},
		{Kind: provider.DeltaDone, FinishReason: "tool_calls"},
	}
	responder := &scriptedResponder{scripts: [][]provider.Delta{twoCalls, twoCalls, contentScript("done")}}

	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "go"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cancelStep = cancel
	res, err := a.Step(ctx)
	if err != nil {
		t.Fatalf("Step: %v", err)
	}
	if res.Status != domain.StatusCancelled {
		t.Fatalf("Step status = %q, want %q", res.Status, domain.StatusCancelled)
	}
	if ran != 1 {
		t.Fatalf("read_file ran %d times before the cancel, want 1", ran)
	}
	if len(a.tracker.seenReads) != 0 {
		t.Fatalf("cancelled Turn left %d seen-read keys; the discard must roll them back", len(a.tracker.seenReads))
	}

	// Pre-trip the budget: only a PRODUCTIVE re-attempt (the read judged novel again) clears
	// it — a repeat read would leave the Turn harmful (the erroring slow tool) and tripped.
	a.tracker.budgetTripped = true
	a.tracker.harmfulStreak = turnBudgetLimit
	res2, err := a.Step(context.Background())
	if err != nil {
		t.Fatalf("Step (re-attempt): %v", err)
	}
	if res2.Status != domain.StatusTurnComplete {
		t.Fatalf("re-attempt status = %q, want %q", res2.Status, domain.StatusTurnComplete)
	}
	if ran != 2 {
		t.Errorf("read_file ran %d times, want 2 (the re-attempt re-read it)", ran)
	}
	if a.tracker.budgetTripped {
		t.Error("the re-attempt did not clear the Turn Budget; the rolled-back read lost its novelty credit")
	}
}

// TestSelfRegulationResetsOnResume proves the per-Session tracker resets on Resume: a Mechanism
// suppressed before the snapshot fires again in the resumed Agent (fresh tracker).
func TestSelfRegulationResetsOnResume(t *testing.T) {
	cfg := baseConfig(&recordingSink{})
	cfg.Mechanisms = domain.NewMechanismRegistry()
	firedA := 0
	mustAddMech(t, cfg.Mechanisms, countingMech{id: "nudge", cap: domain.CapProactiveNudge, pol: domain.SuppressStrikesThree, fired: &firedA})

	a, err := newAgent(cfg, echoResponder{reply: ""}) // every Turn harmful
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	const turns = adaptiveSuppressStrikes + 2
	for i := 0; i < turns; i++ {
		stepOnce(t, a, "go")
	}
	if firedA != adaptiveSuppressStrikes+1 {
		t.Fatalf("precondition: Mechanism dispatched %d times, want %d (suppressed)", firedA, adaptiveSuppressStrikes+1)
	}
	snap, err := a.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	// Resume with a fresh registry + counter: the resumed Agent's tracker is clean, so the
	// pre-snapshot suppression does not carry over and the Mechanism fires again.
	cfg2 := baseConfig(&recordingSink{})
	cfg2.Mechanisms = domain.NewMechanismRegistry()
	firedB := 0
	mustAddMech(t, cfg2.Mechanisms, countingMech{id: "nudge", cap: domain.CapProactiveNudge, pol: domain.SuppressStrikesThree, fired: &firedB})
	b, err := resumeAgent(cfg2, snap, echoResponder{reply: ""})
	if err != nil {
		t.Fatalf("resumeAgent: %v", err)
	}
	stepOnce(t, b, "go")
	if firedB != 1 {
		t.Errorf("resumed Mechanism dispatched %d times, want 1 (suppression state must reset on Resume)", firedB)
	}
}
