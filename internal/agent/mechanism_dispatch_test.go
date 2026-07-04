package agent

// White-box tests for catalogued-Mechanism dispatch (Phase-4 item 2): the loop dispatches
// registered Mechanisms under their real MechanismID, the Bypass gate drops non-off-ramp
// Mechanisms while keeping off-ramps and never touching experimental hooks, catalogued fire
// before experimental, and a panicking catalogued Mechanism is contained exactly like a
// panicking experimental hook. Order/tiebreak determinism itself is proven in package domain.

import (
	"context"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// recordingMech is a catalogued pre-request Mechanism that counts its invocations; cap sets
// the Capability the Bypass gate reads. It mutates the request so each invocation is an
// ACTED fire (R4) and is booked/attributed.
type recordingMech struct {
	id    domain.MechanismID
	cap   domain.Capability
	fired *int
}

func (m recordingMech) Descriptor() domain.MechanismDescriptor {
	return domain.MechanismDescriptor{ID: m.id, Capability: m.cap}
}
func (recordingMech) Ordering() domain.OrderingConstraints { return domain.OrderingConstraints{} }
func (m recordingMech) PreRequest(_ context.Context, req *domain.Request) error {
	*m.fired++
	req.AppendToSystem("[dispatch-test "+string(m.id)+"]", "[dispatch-test "+string(m.id)+"] nudge")
	return nil
}

// seqMech / seqHook append a label to a shared slice, so a test can assert the catalogued
// Mechanism fired before the experimental hook.
type seqMech struct {
	id    domain.MechanismID
	order *[]string
}

func (m seqMech) Descriptor() domain.MechanismDescriptor {
	return domain.MechanismDescriptor{ID: m.id}
}
func (seqMech) Ordering() domain.OrderingConstraints { return domain.OrderingConstraints{} }
func (m seqMech) PreRequest(context.Context, *domain.Request) error {
	*m.order = append(*m.order, "catalogued")
	return nil
}

type seqHook struct{ order *[]string }

func (h seqHook) PreRequest(context.Context, *domain.Request) error {
	*h.order = append(*h.order, "experimental")
	return nil
}

// panicMech is a catalogued pre-request Mechanism that panics — the input for the
// recover-at-extension-boundary guarantee under the catalogued path.
type panicMech struct{ id domain.MechanismID }

func (m panicMech) Descriptor() domain.MechanismDescriptor {
	return domain.MechanismDescriptor{ID: m.id}
}
func (panicMech) Ordering() domain.OrderingConstraints              { return domain.OrderingConstraints{} }
func (panicMech) PreRequest(context.Context, *domain.Request) error { panic("catalogued boom") }

func mechanismFires(events []domain.Event) []domain.MechanismFiredEvent {
	var out []domain.MechanismFiredEvent
	for _, e := range events {
		if fe, ok := e.(domain.MechanismFiredEvent); ok {
			out = append(out, fe)
		}
	}
	return out
}

func mustAddMech(t *testing.T, r *domain.MechanismRegistry, m domain.Mechanism) {
	t.Helper()
	if err := r.Add(m); err != nil {
		t.Fatalf("Add(%s): %v", m.Descriptor().ID, err)
	}
}

func TestCataloguedMechanismFiresUnderRealID(t *testing.T) {
	sink := &recordingSink{}
	cfg := baseConfig(sink)
	cfg.Mechanisms = domain.NewMechanismRegistry()
	fired := 0
	mustAddMech(t, cfg.Mechanisms, recordingMech{id: "greet", cap: domain.CapProactiveNudge, fired: &fired})

	driveOneStep(t, cfg, echoResponder{reply: "ok"})

	if fired != 1 {
		t.Errorf("catalogued mechanism fired %d times, want 1", fired)
	}
	fires := mechanismFires(sink.events)
	found := false
	for _, fe := range fires {
		if fe.Mechanism == "greet" {
			found = true
			if fe.Hook != domain.HookPreRequest {
				t.Errorf("fired event hook = %q, want %q", fe.Hook, domain.HookPreRequest)
			}
		}
		if fe.Mechanism == experimentalMechanismID {
			t.Errorf("catalogued fire was attributed to the synthetic experimental ID, want %q", "greet")
		}
	}
	if !found {
		t.Errorf("no MechanismFiredEvent carried the catalogued ID %q; got %+v", "greet", fires)
	}
}

func TestBypassGate(t *testing.T) {
	tests := []struct {
		name                           string
		bypass                         bool
		wantOff, wantNudge, wantRepair int
	}{
		{name: "bypass off ⇒ all catalogued fire", bypass: false, wantOff: 1, wantNudge: 1, wantRepair: 1},
		{name: "bypass on ⇒ only the off-ramp fires", bypass: true, wantOff: 1, wantNudge: 0, wantRepair: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sink := &recordingSink{}
			cfg := baseConfig(sink)
			cfg.Bypass = tt.bypass
			cfg.Mechanisms = domain.NewMechanismRegistry()

			var off, nudge, repair int
			mustAddMech(t, cfg.Mechanisms, recordingMech{id: "off", cap: domain.CapOffRamp, fired: &off})
			mustAddMech(t, cfg.Mechanisms, recordingMech{id: "nudge", cap: domain.CapProactiveNudge, fired: &nudge})
			mustAddMech(t, cfg.Mechanisms, recordingMech{id: "repair", cap: domain.CapResponseRepair, fired: &repair})

			// An experimental hook is the bench's own instrument — never Bypass-gated.
			expFired := false
			if err := cfg.Mechanisms.AddExperimental(domain.HookPreRequest, firingHook{fired: &expFired}); err != nil {
				t.Fatalf("AddExperimental: %v", err)
			}

			driveOneStep(t, cfg, echoResponder{reply: "ok"})

			if off != tt.wantOff {
				t.Errorf("off-ramp fired %d, want %d", off, tt.wantOff)
			}
			if nudge != tt.wantNudge {
				t.Errorf("proactive-nudge fired %d, want %d", nudge, tt.wantNudge)
			}
			if repair != tt.wantRepair {
				t.Errorf("response-repair fired %d, want %d", repair, tt.wantRepair)
			}
			if !expFired {
				t.Errorf("experimental hook did not fire (bypass=%v); it must never be Bypass-gated", tt.bypass)
			}
		})
	}
}

func TestCataloguedFireBeforeExperimental(t *testing.T) {
	sink := &recordingSink{}
	cfg := baseConfig(sink)
	cfg.Mechanisms = domain.NewMechanismRegistry()

	var order []string
	mustAddMech(t, cfg.Mechanisms, seqMech{id: "cat", order: &order})
	if err := cfg.Mechanisms.AddExperimental(domain.HookPreRequest, seqHook{order: &order}); err != nil {
		t.Fatalf("AddExperimental: %v", err)
	}

	driveOneStep(t, cfg, echoResponder{reply: "ok"})

	if len(order) != 2 || order[0] != "catalogued" || order[1] != "experimental" {
		t.Errorf("dispatch order = %v, want [catalogued experimental]", order)
	}
}

func TestPanickingCataloguedMechanismContained(t *testing.T) {
	sink := &recordingSink{}
	cfg := baseConfig(sink)
	cfg.Mechanisms = domain.NewMechanismRegistry()
	mustAddMech(t, cfg.Mechanisms, panicMech{id: "boom"})

	a, err := newAgent(cfg, echoResponder{reply: "unreached"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "hi"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	res, err := a.Step(context.Background())
	if err != nil {
		t.Fatalf("Step returned a loop error on catalogued panic: %v", err)
	}
	if res.Status != domain.StatusExchangeComplete {
		t.Errorf("Step status = %q, want %q", res.Status, domain.StatusExchangeComplete)
	}

	// The panic degraded the Turn: an ErrorEvent attributed to the mechanism's real ID, and no
	// assistant message (the Upstream was never called).
	var gotSource string
	for _, e := range sink.events {
		if ee, ok := e.(domain.ErrorEvent); ok {
			gotSource = ee.Source
		}
	}
	if gotSource != "boom" {
		t.Errorf("ErrorEvent Source = %q, want the catalogued mechanism ID %q", gotSource, "boom")
	}
	if _, ok := firstMessageEvent(t, sink.events); ok {
		t.Error("a MessageEvent was emitted despite the catalogued mechanism panicking")
	}

	// The loop survived: a second Step recovers again and still returns cleanly.
	if err := a.Submit(domain.UserInput{Text: "again"}); err != nil {
		t.Fatalf("Submit after recovery: %v", err)
	}
	res2, err := a.Step(context.Background())
	if err != nil {
		t.Fatalf("second Step after recovery returned a loop error: %v", err)
	}
	if res2.Status != domain.StatusExchangeComplete {
		t.Errorf("second Step status = %q, want %q", res2.Status, domain.StatusExchangeComplete)
	}
}
