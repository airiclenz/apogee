package agent

// Reaction ownership across a depth-0 fan-out (ADR 0039 / ADR 0013 §3, follow-up FU-B; recast onto
// the Reaction core by ADR 0076 stage 1).
//
// A delegated child inherits the parent's armed Reactions, and item 4 made depth-0 siblings run AT
// ONCE. The Mechanism layer answered that with a per-child instance seam; a
// domain.Reaction has no equivalent, so the ONE handler a parent armed is the very handler both
// children run, concurrently. These tests drive that through a real two-way fan-out: the inherited
// Reaction is reached by the parent and by both children, its own lock is what makes its state
// safe, and Bypass still turns the whole thing off.
//
// The -race detector is the second judge here: a stateful Reaction that did NOT guard itself would
// be reported on the concurrent append rather than the test having to catch a wrong count.

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// fanOutStateProbe is a stateful test Reaction: it records the asker of every request it sees, under
// its own lock, because one instance is shared across the parent and both concurrent children —
// "the reaction that carries state synchronises itself" is the whole contract the Reaction core
// leaves a bench (header call, ADR 0076 stage 1).
type fanOutStateProbe struct {
	ledger *scopeLedger
}

// reaction arms the probe as a pre-request Reaction. It leaves TopLevelOnly at its zero value, so
// every child inherits it — today's unconditional membership inheritance.
func (p *fanOutStateProbe) reaction() domain.Reaction {
	return domain.Reaction{
		ID:     "fan_out_state_probe",
		Origin: domain.OriginEngine,
		Class:  domain.ClassShapeView,
		On:     []domain.Moment{domain.MomentPreRequest},
		Handler: domain.PreRequestFunc(func(_ context.Context, req *domain.Request) (domain.Outcome, error) {
			lastUser, _, _ := req.View().Conversation().LastUser()
			p.ledger.record(lastUser.Content)
			// Mutate the request so the invocation is an ACTED fire and the Reaction is exercised
			// the way a real pre-request nudge is (the marker makes a repeat a no-op, as in
			// production).
			req.AppendToSystem("[fan-out state probe]", "[fan-out state probe] noted")
			return domain.Outcome{}, nil
		}),
	}
}

// scopeLedger records which askers the shared Reaction observed, and how often. It is the
// deliberately locked collaborator the handler keeps its state in, so the test can read the picture
// back out while two child goroutines are still writing to it.
type scopeLedger struct {
	mu   sync.Mutex
	seen map[string]int
}

func newScopeLedger() *scopeLedger {
	return &scopeLedger{seen: map[string]int{}}
}

func (l *scopeLedger) record(asker string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seen[asker]++
}

// askers returns the distinct askers observed, with their observation counts.
func (l *scopeLedger) askers() map[string]int {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make(map[string]int, len(l.seen))
	for asker, n := range l.seen {
		out[asker] = n
	}
	return out
}

// armFanOutStateProbe arms the stateful probe on cfg as a shape-view Reaction (so Bypass disables
// it) and returns it.
func armFanOutStateProbe(cfg *domain.Config, ledger *scopeLedger) *fanOutStateProbe {
	probe := &fanOutStateProbe{ledger: ledger}
	cfg.Reactions = []domain.Reaction{probe.reaction()}
	return probe
}

// fanOutStateResponder scripts the two-way delegation both tests drive: the parent delegates two
// tasks at once, each child answers behind the concurrency probe, and the parent then finishes.
func fanOutStateResponder(probe *concurrencyProbe) *routedResponder {
	return newRoutedResponder().
		route("delegate two things", nil, fanOutScript([2]string{"c1", "task one"}, [2]string{"c2", "task two"})).
		route("task one", probe.enter, contentScript("child one done")).
		route("task two", probe.enter, contentScript("child two done")).
		route("delegate two things", nil, contentScript("parent done"))
}

// TestFanOut_InheritedReactionIsSharedAcrossSiblings is the hazard's closing test in its Reaction-core
// form: with two children in flight at once, the ONE inherited handler runs on three agents'
// requests — the parent's and both children's — and its own lock is what keeps that safe. What the
// Mechanism layer isolated per child, a Reaction shares; the test pins the sharing so the loss is
// visible rather than silent, and the -race detector judges the synchronisation.
func TestFanOut_InheritedReactionIsSharedAcrossSiblings(t *testing.T) {
	sink := &recordingSink{}
	ledger := newScopeLedger()
	probe := newConcurrencyProbe(2, 3*time.Second)

	cfg := subAgentConfig(sink, domain.ModeAskBefore)
	cfg.ParallelAgents = 2
	armFanOutStateProbe(&cfg, ledger)

	a, err := newAgent(cfg, fanOutStateResponder(probe))
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "delegate two things"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	res, err := a.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Status != domain.StatusExchangeComplete {
		t.Fatalf("parent status = %q, want the Exchange to complete", res.Status)
	}
	if peak := probe.peakInFlight(); peak != 2 {
		t.Fatalf("peak children in flight = %d, want 2 (the group ran serially — the test proves nothing)", peak)
	}

	askers := ledger.askers()
	for _, want := range []string{"delegate two things", "task one", "task two"} {
		if askers[want] == 0 {
			t.Errorf("the shared Reaction never observed %q; it did not reach that agent. observed=%v", want, askers)
		}
	}
}

// TestFanOut_BypassSilencesTheInheritedReaction pins the untouchable floor: with Bypass on, the
// inheritance changes nothing — a shape-view Reaction fires for neither the parent nor either
// child, so a fan-out under Bypass is the same reaction-free run it has always been (ADR 0076 D9).
func TestFanOut_BypassSilencesTheInheritedReaction(t *testing.T) {
	sink := &recordingSink{}
	ledger := newScopeLedger()
	probe := newConcurrencyProbe(2, 3*time.Second)

	cfg := subAgentConfig(sink, domain.ModeAskBefore)
	cfg.ParallelAgents = 2
	cfg.Bypass = true
	armFanOutStateProbe(&cfg, ledger)

	a, err := newAgent(cfg, fanOutStateResponder(probe))
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "delegate two things"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := ledger.askers(); len(got) != 0 {
		t.Errorf("under Bypass the shared Reaction observed %d askers %v, want 0", len(got), got)
	}
}
