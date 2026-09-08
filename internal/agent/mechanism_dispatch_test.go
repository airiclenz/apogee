package agent

// White-box tests for armed-Reaction dispatch (Phase-4 item 2, broadened by
// phase-4-review-fixes item 5; recast onto the Reaction core by ADR 0076 stage 1): the loop
// dispatches the Reactions a host armed on Config.Reactions under their own IDs; at ALL FIVE
// seam Moments the Bypass gate drops advise and the two shape classes while keeping observe
// and gate; armed Reactions fire in registration order; and a panicking Reaction is contained
// at the extension boundary. Revision bracketing itself is proven in package domain.

import (
	"context"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// allSeamMoments is the complete seam vocabulary the dispatch matrix spans, in loop order.
var allSeamMoments = domain.Seams()

// recordingReaction is an armed pre-request Reaction that counts its invocations; class sets the
// cell the Bypass gate reads. It edits the request on every invocation, so each one moves the
// working value's Revision and is an ACTED fire (R4) booked under the reaction's own ID.
func recordingReaction(id string, class domain.Class, fired *int) domain.Reaction {
	return domain.Reaction{
		ID:     id,
		Origin: domain.OriginEngine,
		Class:  class,
		On:     []domain.Moment{domain.MomentPreRequest},
		Handler: domain.PreRequestFunc(func(_ context.Context, req *domain.Request) (domain.Outcome, error) {
			*fired++
			req.AppendToSystem("[dispatch-test "+id+"]", "[dispatch-test "+id+"] nudge")
			return domain.Outcome{}, nil
		}),
	}
}

// countingReaction is an armed pre-request Reaction that only counts: it touches nothing and
// returns the zero Outcome, so it is invoked but never booked. What it proves is arrival — a
// reaction Bypass dropped was never invoked, so its counter cannot move.
func countingReaction(id string, class domain.Class, fired *int) domain.Reaction {
	return domain.Reaction{
		ID:     id,
		Origin: domain.OriginEngine,
		Class:  class,
		On:     []domain.Moment{domain.MomentPreRequest},
		Handler: domain.PreRequestFunc(func(context.Context, *domain.Request) (domain.Outcome, error) {
			*fired++
			return domain.Outcome{}, nil
		}),
	}
}

// seamProbes builds ONE Reaction per seam Moment — five of them, sharing an origin, a class and
// a note func — each reporting its invocation without acting. A Reaction's handler is sealed to
// one seam, so a probe that spans the whole matrix is a set rather than a single row; the IDs are
// prefix + "-" + the Moment, because the dispatcher refuses two reactions sharing one ID.
func seamProbes(prefix string, class domain.Class, note func(domain.Moment)) []domain.Reaction {
	out := make([]domain.Reaction, 0, len(allSeamMoments))
	for _, m := range allSeamMoments {
		r := domain.Reaction{
			ID:     prefix + "-" + string(m),
			Origin: domain.OriginEngine,
			Class:  class,
			On:     []domain.Moment{m},
		}
		switch m {
		case domain.MomentPreRequest:
			r.Handler = domain.PreRequestFunc(func(context.Context, *domain.Request) (domain.Outcome, error) {
				note(domain.MomentPreRequest)
				return domain.Outcome{}, nil
			})
		case domain.MomentPostResponse:
			r.Handler = domain.PostResponseFunc(func(context.Context, *domain.Response) (domain.Outcome, error) {
				note(domain.MomentPostResponse)
				return domain.Outcome{}, nil
			})
		case domain.MomentPreToolExec:
			r.Handler = domain.PreToolExecFunc(func(context.Context, domain.LoopView, *domain.ToolCallEdit) (domain.Outcome, error) {
				note(domain.MomentPreToolExec)
				return domain.Outcome{}, nil
			})
		case domain.MomentPostToolResult:
			r.Handler = domain.PostToolResultFunc(func(context.Context, domain.LoopView, domain.ToolCall, *domain.ToolResultEdit) (domain.Outcome, error) {
				note(domain.MomentPostToolResult)
				return domain.Outcome{}, nil
			})
		case domain.MomentHistoryRewrite:
			r.Handler = domain.HistoryRewriteFunc(func(context.Context, *domain.Conversation) (domain.Outcome, error) {
				note(domain.MomentHistoryRewrite)
				return domain.Outcome{}, nil
			})
		}
		out = append(out, r)
	}
	return out
}

// countsByMoment returns a per-seam invocation counter and the note func feeding it.
func countsByMoment() (map[domain.Moment]int, func(domain.Moment)) {
	counts := make(map[domain.Moment]int, len(allSeamMoments))
	return counts, func(m domain.Moment) { counts[m]++ }
}

// driveToolExchange drives one full Exchange whose first Turn carries a tool call and whose
// second closes with text — so every one of the five seam Moments is exercised at least once.
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

// panicReaction is an armed pre-request Reaction that panics — the input for the
// recover-at-extension-boundary guarantee on the armed leg.
func panicReaction(id string) domain.Reaction {
	return domain.Reaction{
		ID:     id,
		Origin: domain.OriginEngine,
		Class:  domain.ClassShapeView,
		On:     []domain.Moment{domain.MomentPreRequest},
		Handler: domain.PreRequestFunc(func(context.Context, *domain.Request) (domain.Outcome, error) {
			panic("armed reaction boom")
		}),
	}
}

func reactionFires(events []domain.Event) []domain.ReactionFiredEvent {
	var out []domain.ReactionFiredEvent
	for _, e := range events {
		if fe, ok := e.(domain.ReactionFiredEvent); ok {
			out = append(out, fe)
		}
	}
	return out
}

// mustAddMech registers one catalogue row. The registry no longer asks a Mechanism to describe
// itself, so each fixture below names the row it would be catalogued under through its own row()
// helper, and a real Mechanism arrives as the row mechanisms.Build already returned.
func mustAddMech(t *testing.T, r *domain.MechanismRegistry, m domain.RegisteredMechanism) {
	t.Helper()
	if err := r.Add(m); err != nil {
		t.Fatalf("Add(%s): %v", m.Descriptor.ID, err)
	}
}

// TestArmedReactionFiresUnderItsOwnID: a Reaction armed on Config.Reactions is invoked through
// the real loop and its firing is booked under the ID it was armed with, never a synthetic one.
func TestArmedReactionFiresUnderItsOwnID(t *testing.T) {
	sink := &recordingSink{}
	cfg := baseConfig(sink)
	fired := 0
	cfg.Reactions = []domain.Reaction{recordingReaction("greet", domain.ClassShapeView, &fired)}

	driveOneStep(t, cfg, echoResponder{reply: "ok"})

	if fired != 1 {
		t.Errorf("armed reaction fired %d times, want 1", fired)
	}
	fires := reactionFires(sink.events)
	found := false
	for _, fe := range fires {
		if fe.Reaction == "greet" {
			found = true
			if fe.Moment != domain.MomentPreRequest {
				t.Errorf("fired event moment = %q, want %q", fe.Moment, domain.MomentPreRequest)
			}
			if fe.Origin != domain.OriginEngine {
				t.Errorf("fired event origin = %q, want %q", fe.Origin, domain.OriginEngine)
			}
		}
	}
	if !found {
		t.Errorf("no ReactionFiredEvent carried the armed ID %q; got %+v", "greet", fires)
	}
}

// TestBypassGate is the five-seam Bypass dispatch matrix (phase-4-review-fixes item 5, recast on
// ADR 0076 D9): at EVERY seam Moment an armed observe or gate Reaction survives Bypass, while
// advise, shape-view and shape-work are dropped before they are ever invoked — and all five
// dispatch when Bypass is off.
func TestBypassGate(t *testing.T) {
	tests := []struct {
		name   string
		bypass bool
	}{
		{name: "bypass off ⇒ every class dispatches", bypass: false},
		{name: "bypass on ⇒ only observe and gate dispatch", bypass: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sink := &recordingSink{}
			cfg := configWithTools(sink, fakeTool{name: "probe", readOnly: true, result: "ok"})
			cfg.Bypass = tt.bypass

			type subject struct {
				class   domain.Class
				counts  map[domain.Moment]int
				dropped bool // whether Bypass switches this class off (D9)
			}
			subjects := map[string]*subject{
				"observe":    {class: domain.ClassObserve},
				"gate":       {class: domain.ClassGate},
				"advise":     {class: domain.ClassAdvise, dropped: true},
				"shape-view": {class: domain.ClassShapeView, dropped: true},
				"shape-work": {class: domain.ClassShapeWork, dropped: true},
			}
			for name, s := range subjects {
				counts, note := countsByMoment()
				s.counts = counts
				cfg.Reactions = append(cfg.Reactions, seamProbes(name, s.class, note)...)
			}

			driveToolExchange(t, cfg)

			for name, s := range subjects {
				for _, m := range allSeamMoments {
					gated := s.counts[m] == 0
					want := tt.bypass && s.dropped
					if gated != want {
						t.Errorf("[%s] class %s dispatched %d times (bypass=%v), want gated=%v",
							m, name, s.counts[m], tt.bypass, want)
					}
				}
			}
		})
	}
}

// TestArmedReactionsFireInRegistrationOrder proves the armed leg's order at every seam Moment:
// the Reactions on Config.Reactions run in the order the host listed them, so a bench instrument
// armed after a subject observes the subject's behaviour and never the reverse.
func TestArmedReactionsFireInRegistrationOrder(t *testing.T) {
	cfg := configWithTools(&recordingSink{}, fakeTool{name: "probe", readOnly: true, result: "ok"})

	order := make(map[domain.Moment][]string, len(allSeamMoments))
	label := func(name string) func(domain.Moment) {
		return func(m domain.Moment) { order[m] = append(order[m], name) }
	}
	cfg.Reactions = append(cfg.Reactions, seamProbes("first", domain.ClassObserve, label("first"))...)
	cfg.Reactions = append(cfg.Reactions, seamProbes("second", domain.ClassObserve, label("second"))...)

	driveToolExchange(t, cfg)

	// Some Moments run once per exchange (the tool stages), others once per Turn: assert every
	// pass alternates first → second rather than pinning a pass count.
	want := [2]string{"first", "second"}
	for _, m := range allSeamMoments {
		got := order[m]
		if len(got) == 0 || len(got)%2 != 0 {
			t.Errorf("[%s] dispatch order = %v, want complete first/second pairs", m, got)
			continue
		}
		for i, name := range got {
			if name != want[i%2] {
				t.Errorf("[%s] dispatch order = %v, want alternating [first second ...]", m, got)
				break
			}
		}
	}
}

// TestPanickingReactionContained: a panicking armed Reaction is recovered at the extension
// boundary, reported as an ErrorEvent under its own ID, and treated as having done nothing — the
// cascade and the Turn carry on (ADR 0076 stage-1 header call), and the loop survives a second
// Step.
func TestPanickingReactionContained(t *testing.T) {
	sink := &recordingSink{}
	cfg := baseConfig(sink)
	cfg.Reactions = []domain.Reaction{panicReaction("boom")}

	a, err := newAgent(cfg, echoResponder{reply: "still answered"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "hi"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	res, err := a.Step(context.Background())
	if err != nil {
		t.Fatalf("Step returned a loop error on an armed-reaction panic: %v", err)
	}
	if res.Status != domain.StatusExchangeComplete {
		t.Errorf("Step status = %q, want %q", res.Status, domain.StatusExchangeComplete)
	}

	// The panic was attributed to the reaction's own ID and booked no firing.
	var gotSource string
	for _, e := range sink.events {
		if ee, ok := e.(domain.ErrorEvent); ok {
			gotSource = ee.Source
		}
	}
	if gotSource != "boom" {
		t.Errorf("ErrorEvent Source = %q, want the armed reaction's ID %q", gotSource, "boom")
	}
	if fires := reactionFires(sink.events); len(fires) != 0 {
		t.Errorf("a panicking reaction booked %d firings, want 0: %+v", len(fires), fires)
	}

	// The reaction degraded to a no-op rather than degrading the Turn: the Upstream was still
	// called and its reply reached the host.
	if me, ok := firstMessageEvent(t, sink.events); !ok || me.Text != "still answered" {
		t.Errorf("MessageEvent = %+v (ok=%v), want the Turn to carry on past the recovered panic", me, ok)
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
