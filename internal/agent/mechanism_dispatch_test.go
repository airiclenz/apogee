package agent

// White-box tests for catalogued-Mechanism dispatch (Phase-4 item 2, broadened by
// phase-4-review-fixes item 5): the loop dispatches registered Mechanisms under their real
// MechanismID; at ALL FIVE hook points the Bypass gate drops non-off-ramp Mechanisms while
// keeping off-ramps and never touching experimental hooks, and catalogued fire before
// experimental; the incompatibility gate surfaces at construction; and a panicking
// catalogued Mechanism is contained exactly like a panicking experimental hook.
// Order/tiebreak determinism itself is proven in package domain.

import (
	"context"
	"errors"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// allHookPoints is the complete hook-point set the dispatch matrix spans.
var allHookPoints = []domain.HookPoint{
	domain.HookPreRequest,
	domain.HookPostResponse,
	domain.HookPreToolExec,
	domain.HookPostToolResult,
	domain.HookHistoryRewrite,
}

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

// fivePointProbe implements all five hook interfaces, reporting each invocation (and its
// hook point) through note without acting — the shared core of the five-point Bypass matrix
// and the catalogued-before-experimental order probe. Registered bare via AddExperimental it
// is an experimental hook at every point; wrapped in fivePointMech it is one catalogued
// Mechanism spanning the whole matrix.
type fivePointProbe struct {
	note func(domain.HookPoint)
}

func (p fivePointProbe) RewriteHistory(context.Context, *domain.Conversation) error {
	p.note(domain.HookHistoryRewrite)
	return nil
}

func (p fivePointProbe) PreRequest(context.Context, *domain.Request) error {
	p.note(domain.HookPreRequest)
	return nil
}

func (p fivePointProbe) PostResponse(context.Context, *domain.Response) (domain.PostResponseDecision, error) {
	p.note(domain.HookPostResponse)
	return domain.PostResponseDecision{}, nil
}

func (p fivePointProbe) PreToolExec(context.Context, *domain.ToolCall, domain.LoopView) error {
	p.note(domain.HookPreToolExec)
	return nil
}

func (p fivePointProbe) PostToolResult(context.Context, domain.ToolCall, *domain.ToolResult, domain.LoopView) error {
	p.note(domain.HookPostToolResult)
	return nil
}

// fivePointMech is fivePointProbe with a descriptor — a catalogued Mechanism hooking at all
// five points at once, so one tool-carrying exchange probes the full dispatch matrix.
type fivePointMech struct {
	fivePointProbe
	id  domain.MechanismID
	cap domain.Capability
}

func (m fivePointMech) Descriptor() domain.MechanismDescriptor {
	return domain.MechanismDescriptor{ID: m.id, Capability: m.cap}
}
func (fivePointMech) Ordering() domain.OrderingConstraints { return domain.OrderingConstraints{} }

// countsByPoint returns a per-hook-point invocation counter and the note func feeding it.
func countsByPoint() (map[domain.HookPoint]int, func(domain.HookPoint)) {
	counts := make(map[domain.HookPoint]int, len(allHookPoints))
	return counts, func(at domain.HookPoint) { counts[at]++ }
}

// incompatMech is a minimal pre-request Mechanism declaring an IncompatibleWith constraint —
// the fixture for the construction-time incompatibility gate.
type incompatMech struct {
	id       domain.MechanismID
	incompat []domain.MechanismID
}

func (m incompatMech) Descriptor() domain.MechanismDescriptor {
	return domain.MechanismDescriptor{ID: m.id, IncompatibleWith: m.incompat}
}
func (incompatMech) Ordering() domain.OrderingConstraints              { return domain.OrderingConstraints{} }
func (incompatMech) PreRequest(context.Context, *domain.Request) error { return nil }

// driveToolExchange drives one full Exchange whose first Turn carries a tool call and whose
// second closes with text — so every one of the five hook points is exercised at least once.
func driveToolExchange(t *testing.T, cfg domain.Config) {
	t.Helper()
	a, err := newAgent(cfg, &scriptedResponder{scripts: [][]provider.Delta{
		toolCallScript("call-1", "probe", `{}`),
		contentScript("done"),
	}})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "do the thing"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	res, err := a.Step(context.Background())
	if err != nil {
		t.Fatalf("Step (tool Turn): %v", err)
	}
	if res.Status != domain.StatusTurnComplete {
		t.Fatalf("tool Turn status = %q, want %q", res.Status, domain.StatusTurnComplete)
	}
	if _, err := a.Step(context.Background()); err != nil {
		t.Fatalf("Step (closing Turn): %v", err)
	}
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

// TestBypassGate is the five-point Bypass dispatch matrix (phase-4-review-fixes item 5): at
// EVERY hook point an off-ramp survives Bypass, proactive-nudge and response-repair are
// dropped under it (and all three dispatch without it), and an experimental hook — the
// bench's own instrument — is never gated either way.
func TestBypassGate(t *testing.T) {
	tests := []struct {
		name   string
		bypass bool
	}{
		{name: "bypass off ⇒ all catalogued dispatch", bypass: false},
		{name: "bypass on ⇒ only the off-ramp dispatches", bypass: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sink := &recordingSink{}
			cfg := configWithTools(sink, fakeTool{name: "probe", readOnly: true, result: "ok"})
			cfg.Bypass = tt.bypass
			cfg.Mechanisms = domain.NewMechanismRegistry()

			offCounts, noteOff := countsByPoint()
			nudgeCounts, noteNudge := countsByPoint()
			repairCounts, noteRepair := countsByPoint()
			mustAddMech(t, cfg.Mechanisms, fivePointMech{fivePointProbe{noteOff}, "off", domain.CapOffRamp})
			mustAddMech(t, cfg.Mechanisms, fivePointMech{fivePointProbe{noteNudge}, "nudge", domain.CapProactiveNudge})
			mustAddMech(t, cfg.Mechanisms, fivePointMech{fivePointProbe{noteRepair}, "repair", domain.CapResponseRepair})

			expCounts, noteExp := countsByPoint()
			for _, at := range allHookPoints {
				if err := cfg.Mechanisms.AddExperimental(at, fivePointProbe{noteExp}); err != nil {
					t.Fatalf("AddExperimental(%s): %v", at, err)
				}
			}

			driveToolExchange(t, cfg)

			for _, at := range allHookPoints {
				if offCounts[at] == 0 {
					t.Errorf("[%s] off-ramp did not dispatch (bypass=%v); off-ramps survive Bypass", at, tt.bypass)
				}
				if gated := nudgeCounts[at] == 0; gated != tt.bypass {
					t.Errorf("[%s] proactive-nudge dispatched %d times (bypass=%v)", at, nudgeCounts[at], tt.bypass)
				}
				if gated := repairCounts[at] == 0; gated != tt.bypass {
					t.Errorf("[%s] response-repair dispatched %d times (bypass=%v)", at, repairCounts[at], tt.bypass)
				}
				if expCounts[at] == 0 {
					t.Errorf("[%s] experimental hook did not fire (bypass=%v); it must never be Bypass-gated", at, tt.bypass)
				}
			}
		})
	}
}

// TestCataloguedFireBeforeExperimental proves the catalogued-first dispatch order at every
// hook point: within each dispatch pass the catalogued Mechanism runs before the
// experimental hook, so the bench observes the configured behaviour, never the reverse.
func TestCataloguedFireBeforeExperimental(t *testing.T) {
	cfg := configWithTools(&recordingSink{}, fakeTool{name: "probe", readOnly: true, result: "ok"})
	cfg.Mechanisms = domain.NewMechanismRegistry()

	order := make(map[domain.HookPoint][]string, len(allHookPoints))
	label := func(name string) func(domain.HookPoint) {
		return func(at domain.HookPoint) { order[at] = append(order[at], name) }
	}
	mustAddMech(t, cfg.Mechanisms, fivePointMech{fivePointProbe{label("catalogued")}, "cat", domain.CapProactiveNudge})
	for _, at := range allHookPoints {
		if err := cfg.Mechanisms.AddExperimental(at, fivePointProbe{label("experimental")}); err != nil {
			t.Fatalf("AddExperimental(%s): %v", at, err)
		}
	}

	driveToolExchange(t, cfg)

	// Some points run once per exchange (the tool stages), others once per Turn: assert every
	// pass alternates catalogued → experimental rather than pinning a pass count.
	want := [2]string{"catalogued", "experimental"}
	for _, at := range allHookPoints {
		got := order[at]
		if len(got) == 0 || len(got)%2 != 0 {
			t.Errorf("[%s] dispatch order = %v, want complete catalogued/experimental pairs", at, got)
			continue
		}
		for i, name := range got {
			if name != want[i%2] {
				t.Errorf("[%s] dispatch order = %v, want alternating [catalogued experimental ...]", at, got)
				break
			}
		}
	}
}

// TestNewSurfacesIncompatibleMechanismsAtConstruction is the construction-time gate proven
// end-to-end (phase-4-review-fixes item 5): a registry carrying two mutually-incompatible
// Mechanisms is refused by New AND by the newAgent seam it delegates to — the loud startup
// failure, not a silently co-firing pair. Method-level coverage lives in package domain.
func TestNewSurfacesIncompatibleMechanismsAtConstruction(t *testing.T) {
	cfg := baseConfig(&recordingSink{})
	cfg.Mechanisms = domain.NewMechanismRegistry()
	mustAddMech(t, cfg.Mechanisms, incompatMech{id: "read_loop", incompat: []domain.MechanismID{"cached_content_intercept"}})
	mustAddMech(t, cfg.Mechanisms, incompatMech{id: "cached_content_intercept"})

	if _, err := New(cfg); !errors.Is(err, domain.ErrIncompatibleMechanisms) {
		t.Errorf("New = %v, want ErrIncompatibleMechanisms", err)
	}
	if _, err := newAgent(cfg, echoResponder{reply: "ok"}); !errors.Is(err, domain.ErrIncompatibleMechanisms) {
		t.Errorf("newAgent = %v, want ErrIncompatibleMechanisms", err)
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
