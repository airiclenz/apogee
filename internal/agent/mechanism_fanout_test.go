package agent

// Mechanism ownership across a depth-0 fan-out (ADR 0039 / ADR 0013 §3, follow-up FU-B).
//
// A delegated child inherits the parent's Mechanisms, and item 4 made depth-0 siblings run AT
// ONCE — so "inherits" had to stop meaning "runs the parent's very instances". These tests drive
// the seam that closed it (domain.MechanismRegistry.ForSubAgent) through a real two-way fan-out:
// a Mechanism that declares itself SubAgentScoped is touched by exactly one goroutine and sees
// exactly one agent's run, and Bypass still turns the whole thing off.
//
// The state below is deliberately UNSYNCHRONIZED. That is the oracle: a naive stateful Mechanism
// is what a Mechanism author writes, and if the two children shared one instance the -race
// detector would report the concurrent append rather than the test having to catch a wrong count.

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// fanOutStateMech is a stateful test Mechanism: it appends the asker of every request it sees to
// its OWN slice, with no lock of its own, and reports each observation to the shared ledger. Its
// ForSubAgent hands each child a fresh instance over the same ledger — the "isolate the live
// state, share the collaborator that guards itself" shape the library Mechanism takes in
// production.
type fanOutStateMech struct {
	ledger *scopeLedger
	seen   []string // askers THIS instance observed; unguarded on purpose (see the file comment)
}

func (m *fanOutStateMech) ForSubAgent() any { return &fanOutStateMech{ledger: m.ledger} }

func (m *fanOutStateMech) PreRequest(_ context.Context, req *domain.Request) error {
	lastUser, _, _ := req.View().Conversation().LastUser()
	m.seen = append(m.seen, lastUser.Content)
	m.ledger.record(m, lastUser.Content)
	// Mutate the request so the invocation is an ACTED fire and the Mechanism is exercised the
	// way a real pre-request nudge is (the marker makes a repeat a no-op, as in production).
	req.AppendToSystem("[fan-out state probe]", "[fan-out state probe] noted")
	return nil
}

// scopeLedger records which instance observed which asker. It is the one deliberately shared,
// deliberately locked collaborator, so the test can read the per-instance picture back out
// without reaching into an instance a child goroutine owns.
type scopeLedger struct {
	mu   sync.Mutex
	seen map[*fanOutStateMech]map[string]int
}

func newScopeLedger() *scopeLedger {
	return &scopeLedger{seen: map[*fanOutStateMech]map[string]int{}}
}

func (l *scopeLedger) record(m *fanOutStateMech, asker string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.seen[m] == nil {
		l.seen[m] = map[string]int{}
	}
	l.seen[m][asker]++
}

// askersByInstance returns, per observing instance, the distinct askers it saw.
func (l *scopeLedger) askersByInstance() map[*fanOutStateMech]map[string]int {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make(map[*fanOutStateMech]map[string]int, len(l.seen))
	for m, askers := range l.seen {
		copied := make(map[string]int, len(askers))
		for asker, n := range askers {
			copied[asker] = n
		}
		out[m] = copied
	}
	return out
}

// armFanOutStateMech registers the stateful probe as a catalogued Mechanism on cfg — a
// proactive-nudge (so Bypass disables it) that self-regulation never withdraws (so a fire count is
// the Mechanism's own doing and not the tracker's).
func armFanOutStateMech(t *testing.T, cfg *domain.Config, ledger *scopeLedger) *fanOutStateMech {
	t.Helper()
	root := &fanOutStateMech{ledger: ledger}
	registry := domain.NewMechanismRegistry()
	if err := registry.Add(domain.RegisteredMechanism{
		Descriptor: domain.MechanismDescriptor{
			ID:          "fan_out_state_probe",
			Capability:  domain.CapProactiveNudge,
			Suppression: domain.SuppressExempt,
		},
		Hook: root,
	}); err != nil {
		t.Fatalf("Add(fan_out_state_probe): %v", err)
	}
	cfg.Mechanisms = registry
	return root
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

// TestFanOut_StatefulMechanismIsScopedPerChild is the hazard's closing test: with two children in
// flight at once, a stateful Mechanism is touched by one goroutine per instance (the -race
// detector is the judge) and no instance sees more than one agent's requests — the cross-child
// state bleed a shared instance would have, race or no race.
func TestFanOut_StatefulMechanismIsScopedPerChild(t *testing.T) {
	sink := &recordingSink{}
	ledger := newScopeLedger()
	probe := newConcurrencyProbe(2, 3*time.Second)

	cfg := subAgentConfig(sink, domain.ModeAskBefore)
	cfg.ParallelAgents = 2
	root := armFanOutStateMech(t, &cfg, ledger)

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

	byInstance := ledger.askersByInstance()
	if len(byInstance) != 3 {
		t.Fatalf("Mechanism instances that observed a request = %d, want 3 (the parent's and one per child)", len(byInstance))
	}
	for m, askers := range byInstance {
		if len(askers) != 1 {
			t.Errorf("instance %p observed %d different askers %v, want exactly 1 — a child is seeing another agent's run", m, len(askers), askers)
		}
	}
	if got := byInstance[root]; len(got) != 1 || got["delegate two things"] == 0 {
		t.Errorf("the parent's own instance observed %v, want only the parent's own requests", got)
	}
	seenAskers := map[string]bool{}
	for _, askers := range byInstance {
		for asker := range askers {
			seenAskers[asker] = true
		}
	}
	for _, want := range []string{"delegate two things", "task one", "task two"} {
		if !seenAskers[want] {
			t.Errorf("no instance observed %q; the inherited Mechanism did not reach that agent", want)
		}
	}
}

// TestFanOut_BypassSilencesTheInheritedMechanism pins the untouchable floor: with Bypass on, the
// scoping seam changes nothing — a proactive-nudge Mechanism fires for neither the parent nor
// either child, so a fan-out under Bypass is the same Mechanism-free run it has always been.
func TestFanOut_BypassSilencesTheInheritedMechanism(t *testing.T) {
	sink := &recordingSink{}
	ledger := newScopeLedger()
	probe := newConcurrencyProbe(2, 3*time.Second)

	cfg := subAgentConfig(sink, domain.ModeAskBefore)
	cfg.ParallelAgents = 2
	cfg.Bypass = true
	armFanOutStateMech(t, &cfg, ledger)

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

	if got := ledger.askersByInstance(); len(got) != 0 {
		t.Errorf("under Bypass %d Mechanism instances observed a request, want 0", len(got))
	}
}
