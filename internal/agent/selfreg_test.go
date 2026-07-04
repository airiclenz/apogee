package agent

// White-box tests for per-Session self-regulation (Phase-4 item 3): effectiveness tracking,
// Adaptive Suppression, and the global Turn Budget. The selfRegulator state machine is proven
// directly (fast, deterministic), then the loop wiring is proven end-to-end through Step — a
// suppressed Mechanism is not dispatched, an exempt off-ramp always is, LoopView.Fired answers
// from the tracker, and the tracker resets on Resume.

import (
	"context"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
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
// counts how many times it actually fired — the input for the loop-level self-regulation tests.
type countingMech struct {
	id    domain.MechanismID
	cap   domain.Capability
	pol   domain.SuppressionPolicy
	fired *int
}

func (m countingMech) Descriptor() domain.MechanismDescriptor {
	return domain.MechanismDescriptor{ID: m.id, Capability: m.cap, Suppression: m.pol}
}
func (countingMech) Ordering() domain.OrderingConstraints { return domain.OrderingConstraints{} }
func (m countingMech) PreRequest(context.Context, *domain.Request) error {
	*m.fired++
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

func TestSelfRegulatorStrikesThenSuppressed(t *testing.T) {
	r := newSelfRegulator()
	m := regMech{id: "x"}
	for i := 0; i < adaptiveSuppressStrikes; i++ {
		if r.suppress(m) {
			t.Fatalf("Mechanism suppressed after only %d strikes (< %d)", i, adaptiveSuppressStrikes)
		}
		r.recordFire("x")
		r.endTurn() // no productivity signal ⇒ a non-productive Turn
	}
	if !r.suppress(m) {
		t.Fatalf("Mechanism not suppressed after %d consecutive non-productive Turns", adaptiveSuppressStrikes)
	}
}

func TestSelfRegulatorClearPathReopens(t *testing.T) {
	r := newSelfRegulator()
	m := regMech{id: "x"}
	for i := 0; i < adaptiveSuppressStrikes; i++ {
		r.recordFire("x")
		r.endTurn()
	}
	if !r.suppress(m) {
		t.Fatal("precondition: Mechanism should be suppressed")
	}
	// A productive Turn is the clear-path: it re-opens the suppressed Mechanism.
	r.recordFire("x")
	r.noteWrite()
	r.endTurn()
	if r.suppress(m) {
		t.Fatal("a productive Turn did not re-open the suppressed Mechanism")
	}
}

func TestSelfRegulatorExemptNeverSuppressed(t *testing.T) {
	r := newSelfRegulator()
	m := regMech{id: "e", pol: domain.SuppressExempt}
	// Enough non-productive Turns to trip BOTH Adaptive Suppression and the Turn Budget.
	for i := 0; i < turnBudgetLimit+adaptiveSuppressStrikes; i++ {
		r.recordFire("e")
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
			t.Fatalf("Turn Budget tripped early after %d non-productive Turns", i)
		}
		r.endTurn()
	}
	if !r.budgetTripped {
		t.Fatalf("Turn Budget did not trip after %d non-productive Turns", turnBudgetLimit)
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
	if !r.productive {
		t.Fatal("a new file read should mark the Turn productive")
	}
	r.endTurn() // clears the per-Turn productive flag
	r.noteRead(call)
	if r.productive {
		t.Fatal("a repeat read of the same target should not mark the Turn productive")
	}
	r.noteRead(domain.ToolCall{Tool: "read_file", Arguments: []byte(`{"path":"b.go"}`)})
	if !r.productive {
		t.Fatal("a read of a new path should mark the Turn productive")
	}
}

// ---------------------------------------------------------------------------
// Loop wiring
// ---------------------------------------------------------------------------

// TestSelfRegulationSuppressesAtDispatch proves the loop consults the tracker: a non-exempt
// Mechanism that fires through adaptiveSuppressStrikes non-productive Turns is not dispatched on
// the next Turn.
func TestSelfRegulationSuppressesAtDispatch(t *testing.T) {
	cfg := baseConfig(&recordingSink{})
	cfg.Mechanisms = domain.NewMechanismRegistry()
	fired := 0
	mustAddMech(t, cfg.Mechanisms, countingMech{id: "nudge", cap: domain.CapProactiveNudge, pol: domain.SuppressStrikesThree, fired: &fired})

	a, err := newAgent(cfg, echoResponder{reply: "ok"}) // a text-only reply is a non-productive Turn
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	for i := 0; i < adaptiveSuppressStrikes+1; i++ {
		stepOnce(t, a, "go")
	}
	if fired != adaptiveSuppressStrikes {
		t.Errorf("Mechanism fired %d times across %d Turns, want %d (suppressed after the strike limit)",
			fired, adaptiveSuppressStrikes+1, adaptiveSuppressStrikes)
	}
}

// TestExemptFiresThroughSuppression proves an exempt off-ramp is never withdrawn — it fires every
// Turn even past the strike limit and the Turn Budget.
func TestExemptFiresThroughSuppression(t *testing.T) {
	cfg := baseConfig(&recordingSink{})
	cfg.Mechanisms = domain.NewMechanismRegistry()
	fired := 0
	mustAddMech(t, cfg.Mechanisms, countingMech{id: "offramp", cap: domain.CapOffRamp, pol: domain.SuppressExempt, fired: &fired})

	a, err := newAgent(cfg, echoResponder{reply: "ok"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	const turns = turnBudgetLimit + 2
	for i := 0; i < turns; i++ {
		stepOnce(t, a, "go")
	}
	if fired != turns {
		t.Errorf("exempt Mechanism fired %d times across %d Turns, want %d (never suppressed)", fired, turns, turns)
	}
}

// TestFiredCountsVisibleToHook proves LoopView.Fired answers from the tracker — and does so live
// within one hook pass (a catalogued fire is visible to an experimental hook firing after it).
func TestFiredCountsVisibleToHook(t *testing.T) {
	cfg := baseConfig(&recordingSink{})
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
		t.Errorf("hook read Fired(greet) = %d, want 1 (the catalogued fire from the same pass)", seen)
	}
}

// TestSelfRegulationResetsOnResume proves the per-Session tracker resets on Resume: a Mechanism
// suppressed before the snapshot fires again in the resumed Agent (fresh tracker).
func TestSelfRegulationResetsOnResume(t *testing.T) {
	cfg := baseConfig(&recordingSink{})
	cfg.Mechanisms = domain.NewMechanismRegistry()
	firedA := 0
	mustAddMech(t, cfg.Mechanisms, countingMech{id: "nudge", cap: domain.CapProactiveNudge, pol: domain.SuppressStrikesThree, fired: &firedA})

	a, err := newAgent(cfg, echoResponder{reply: "ok"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	for i := 0; i < adaptiveSuppressStrikes; i++ {
		stepOnce(t, a, "go")
	}
	if firedA != adaptiveSuppressStrikes {
		t.Fatalf("precondition: Mechanism fired %d times, want %d", firedA, adaptiveSuppressStrikes)
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
	b, err := resumeAgent(cfg2, snap, echoResponder{reply: "ok"})
	if err != nil {
		t.Fatalf("resumeAgent: %v", err)
	}
	stepOnce(t, b, "go")
	if firedB != 1 {
		t.Errorf("resumed Mechanism fired %d times, want 1 (suppression state must reset on Resume)", firedB)
	}
}
