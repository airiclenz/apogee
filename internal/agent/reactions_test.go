package agent

// The Reaction dispatcher (reactions.go) and the engine's builtins (builtins.go). Every test
// here drives the ONE harness the ladder has — a Moment plus its payload in, an Outcome and a
// stream of ReactionFiredEvents out — because that is the whole interface: the seams that call
// fire have nothing else to give it and read nothing else back.

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/domain/domaintest"
	"github.com/airiclenz/apogee/internal/provider"
)

// ---------------------------------------------------------------------------
// Harness
// ---------------------------------------------------------------------------

// ladderLog records the order reactions were invoked in — the fact half these tests are about,
// since "did not run" is indistinguishable from "ran and did nothing" in the event stream.
type ladderLog struct {
	calls []string
}

func (l *ladderLog) note(id string) { l.calls = append(l.calls, id) }

// probe builds a post-response Reaction that notes its invocation on log and then does what act
// says. A nil act is an inspect-and-do-nothing reaction.
func probe(
	log *ladderLog,
	id string,
	class domain.Class,
	act func(resp *domain.Response) (domain.Outcome, error),
) domain.Reaction {
	return domain.Reaction{
		ID:     id,
		Origin: domain.OriginEngine,
		Class:  class,
		On:     []domain.Moment{domain.MomentPostResponse},
		Handler: domain.PostResponseFunc(func(_ context.Context, resp *domain.Response) (domain.Outcome, error) {
			log.note(id)
			if act == nil {
				return domain.Outcome{}, nil
			}
			return act(resp)
		}),
	}
}

// acts is the act a probe that simply reports an Outcome takes.
func acts(out domain.Outcome) func(*domain.Response) (domain.Outcome, error) {
	return func(*domain.Response) (domain.Outcome, error) { return out, nil }
}

// ladderAgent builds an Agent whose two legs are exactly the reactions given, replacing the seven
// real builtins so a dispatcher test drives the RULE rather than the Floor policy. The sink comes
// back with it because every firing is asserted through the events it emitted.
func ladderAgent(t *testing.T, builtins, armed []domain.Reaction) (*Agent, *recordingSink) {
	t.Helper()

	sink := &recordingSink{}
	a, err := newAgent(baseConfig(sink), echoResponder{reply: "reply"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	a.builtins = nil
	for _, r := range builtins {
		a.builtins = append(a.builtins, armedReaction{spec: r})
	}
	a.armed, err = armReactions(armed)
	if err != nil {
		t.Fatalf("armReactions: %v", err)
	}
	return a, sink
}

// postResponse builds the post-response payload the dispatcher takes: a plain text response over
// an empty view, with the loop's remaining retry budget stated.
func postResponse(retryable bool) domain.PostResponseMoment {
	view := domain.NewRequest("m", nil, nil, domain.Budget{}, 0).View()
	return domain.PostResponseMoment{
		Resp:      domain.NewResponse("narration", "", nil, domain.FinishStop, view),
		Retryable: retryable,
	}
}

// firings lists the ReactionFiredEvents in a recorded stream, in emission order.
func firings(events []domain.Event) []domain.ReactionFiredEvent {
	var fired []domain.ReactionFiredEvent
	for _, e := range events {
		if rf, ok := e.(domain.ReactionFiredEvent); ok {
			fired = append(fired, rf)
		}
	}
	return fired
}

// firedIDs is firings reduced to the ids, the shape most assertions compare.
func firedIDs(events []domain.Event) []string {
	var ids []string
	for _, rf := range firings(events) {
		ids = append(ids, rf.Reaction)
	}
	return ids
}

// assertOrder compares an invocation log or a firing list against what the ladder should have
// produced, naming the whole sequence on failure rather than the first divergence.
func assertOrder(t *testing.T, what string, got, want []string) {
	t.Helper()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}

// ---------------------------------------------------------------------------
// The cascade
// ---------------------------------------------------------------------------

// The engine's builtins run before anything armed beside them, whatever order the host armed
// things in: the floor a model always gets is applied before the lab looks at the response.
func TestFireRunsBuiltinsBeforeArmedReactions(t *testing.T) {
	log := &ladderLog{}
	a, sink := ladderAgent(t,
		[]domain.Reaction{
			probe(log, "builtin-one", domain.ClassShapeView, acts(domain.Outcome{Edited: true})),
			probe(log, "builtin-two", domain.ClassShapeView, nil),
		},
		[]domain.Reaction{
			probe(log, "armed-one", domain.ClassObserve, acts(domain.Outcome{Edited: true})),
		},
	)

	out, err := a.fire(context.Background(), domain.MomentPostResponse, postResponse(true))

	if err != nil {
		t.Fatalf("fire: %v", err)
	}
	if !out.Edited {
		t.Errorf("Outcome.Edited = false, want the leg's edits folded into the result")
	}
	assertOrder(t, "invocations", log.calls, []string{"builtin-one", "builtin-two", "armed-one"})
	assertOrder(t, "firings", firedIDs(sink.events), []string{"builtin-one", "armed-one"})
}

// A panicking reaction degrades to one that did nothing: the boundary reports it as an
// ErrorEvent attributed to the reaction (ADR 0007), books no firing, and the cascade goes on.
func TestFireRecoversAPanicAndContinues(t *testing.T) {
	log := &ladderLog{}
	a, sink := ladderAgent(t, nil, []domain.Reaction{
		probe(log, "boom", domain.ClassObserve, func(*domain.Response) (domain.Outcome, error) {
			panic("reaction boom")
		}),
		probe(log, "after", domain.ClassObserve, acts(domain.Outcome{Edited: true})),
	})

	out, err := a.fire(context.Background(), domain.MomentPostResponse, postResponse(true))

	if err != nil {
		t.Fatalf("fire = %v, want a recovered panic to leave the cascade clean", err)
	}
	if !out.Edited {
		t.Errorf("Outcome.Edited = false, want the reaction after the panic still booked")
	}
	assertOrder(t, "invocations", log.calls, []string{"boom", "after"})
	assertOrder(t, "firings", firedIDs(sink.events), []string{"after"})

	var reported bool
	for _, e := range sink.events {
		if ee, ok := e.(domain.ErrorEvent); ok && ee.Source == "boom" {
			reported = true
		}
	}
	if !reported {
		t.Errorf("events = %v, want an ErrorEvent attributed to the panicking reaction", sink.events)
	}
}

// A RETURNED error is the opposite of a panic: it ends the cascade at once, comes back to the
// seam with a zero Outcome, and leaves the reactions after it unfired.
func TestFireStopsTheCascadeOnAReturnedError(t *testing.T) {
	wantErr := errors.New("reaction refused")
	log := &ladderLog{}
	a, sink := ladderAgent(t, nil, []domain.Reaction{
		probe(log, "refuses", domain.ClassObserve, func(*domain.Response) (domain.Outcome, error) {
			return domain.Outcome{Edited: true}, wantErr
		}),
		probe(log, "never", domain.ClassObserve, nil),
	})

	out, err := a.fire(context.Background(), domain.MomentPostResponse, postResponse(true))

	if !errors.Is(err, wantErr) {
		t.Fatalf("fire = %v, want the reaction's own error", err)
	}
	if out != (domain.Outcome{}) {
		t.Errorf("Outcome = %+v, want the zero Outcome on an ended cascade", out)
	}
	assertOrder(t, "invocations", log.calls, []string{"refuses"})
	if got := firedIDs(sink.events); len(got) != 0 {
		t.Errorf("firings = %v, want none", got)
	}
}

// Firing means ACTING, and the action a firing is booked under follows the Outcome's precedence.
// The revision bracket is the last term: a reaction that reshaped the response in place is booked
// whether or not it said so, and one that says Edited without moving anything is booked too.
func TestFireBooksTheActionTheOutcomeNames(t *testing.T) {
	cases := []struct {
		name       string
		act        func(*domain.Response) (domain.Outcome, error)
		wantFired  bool
		wantAction string
		wantDetail string
	}{
		{
			name:      "inspecting and doing nothing is not a firing",
			act:       nil,
			wantFired: false,
		},
		{
			name:       "a retry",
			act:        acts(domain.Outcome{Retry: true, Inject: "try again"}),
			wantFired:  true,
			wantAction: "retry",
		},
		{
			name:       "a deferral",
			act:        acts(domain.Outcome{Defer: "next time"}),
			wantFired:  true,
			wantAction: "defer",
		},
		{
			name:       "an explicit edit that moved no revision",
			act:        acts(domain.Outcome{Edited: true, Detail: "rewrote nothing"}),
			wantFired:  true,
			wantAction: "intercept",
			wantDetail: "rewrote nothing",
		},
		{
			name: "a moved revision the reaction never mentioned",
			act: func(resp *domain.Response) (domain.Outcome, error) {
				resp.SetText("rewritten")
				return domain.Outcome{}, nil
			},
			wantFired:  true,
			wantAction: "intercept",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			log := &ladderLog{}
			a, sink := ladderAgent(t, nil, []domain.Reaction{
				probe(log, "subject", domain.ClassObserve, tc.act),
			})

			if _, err := a.fire(context.Background(), domain.MomentPostResponse, postResponse(true)); err != nil {
				t.Fatalf("fire: %v", err)
			}

			fired := firings(sink.events)
			if !tc.wantFired {
				if len(fired) != 0 {
					t.Fatalf("firings = %+v, want none", fired)
				}
				return
			}
			if len(fired) != 1 {
				t.Fatalf("firings = %+v, want exactly one", fired)
			}
			got := fired[0]
			if got.Reaction != "subject" || got.Origin != domain.OriginEngine ||
				got.Moment != domain.MomentPostResponse {
				t.Errorf("firing identity = %+v, want subject/engine/post-response", got)
			}
			if got.Action != tc.wantAction {
				t.Errorf("Action = %q, want %q", got.Action, tc.wantAction)
			}
			if got.Detail != tc.wantDetail {
				t.Errorf("Detail = %q, want %q", got.Detail, tc.wantDetail)
			}
		})
	}
}

// A post-response Retry stops the LEG it fires in whatever retry budget the loop has left: the
// budget decides whether the Turn re-streams, never whether the cascade continues.
func TestFireRetryStopsItsOwnLegWhateverTheBudget(t *testing.T) {
	for _, retryable := range []bool{true, false} {
		name := "budget spent"
		if retryable {
			name = "budget remaining"
		}
		t.Run(name, func(t *testing.T) {
			log := &ladderLog{}
			a, sink := ladderAgent(t, nil, []domain.Reaction{
				probe(log, "retries", domain.ClassObserve, acts(domain.Outcome{Retry: true, Inject: "again"})),
				probe(log, "never", domain.ClassObserve, acts(domain.Outcome{Edited: true})),
			})

			out, err := a.fire(context.Background(), domain.MomentPostResponse, postResponse(retryable))

			if err != nil {
				t.Fatalf("fire: %v", err)
			}
			if !out.Retry || out.Inject != "again" {
				t.Errorf("Outcome = %+v, want the retrying reaction's correction", out)
			}
			assertOrder(t, "invocations", log.calls, []string{"retries"})
			assertOrder(t, "firings", firedIDs(sink.events), []string{"retries"})
		})
	}
}

// The one place the retry budget reaches the cascade is the handover between the legs: a builtin
// Retry the loop WILL act on takes the Turn away from the armed leg, while one it cannot act on
// lets the armed leg run on the untouched response — today's fall-through in the loop.
func TestFireBuiltinRetryHandsOverOnlyWhenTheBudgetIsSpent(t *testing.T) {
	cases := []struct {
		name      string
		retryable bool
		want      []string
	}{
		{name: "budget remaining takes the Turn", retryable: true, want: []string{"builtin"}},
		{name: "budget spent lets the armed leg run", retryable: false, want: []string{"builtin", "armed"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			log := &ladderLog{}
			a, sink := ladderAgent(t,
				[]domain.Reaction{
					probe(log, "builtin", domain.ClassShapeView, acts(domain.Outcome{Retry: true, Inject: "again"})),
				},
				[]domain.Reaction{
					probe(log, "armed", domain.ClassObserve, acts(domain.Outcome{Edited: true})),
				},
			)

			if _, err := a.fire(context.Background(), domain.MomentPostResponse, postResponse(tc.retryable)); err != nil {
				t.Fatalf("fire: %v", err)
			}

			assertOrder(t, "invocations", log.calls, tc.want)
			assertOrder(t, "firings", firedIDs(sink.events), tc.want)
		})
	}
}

// Bypass (ADR 0076 D9) switches off ARMED advise and shape reactions and nothing else: observe
// and gate stay on because neither can make a model do worse, and a builtin is never withdrawn
// at all. A skipped reaction is SILENT — it is never invoked and books nothing.
func TestFireBypassMatrix(t *testing.T) {
	cases := []struct {
		name    string
		class   domain.Class
		builtin bool
		wantRun bool
	}{
		{name: "armed observe survives", class: domain.ClassObserve, wantRun: true},
		{name: "armed gate survives", class: domain.ClassGate, wantRun: true},
		{name: "armed advise is skipped", class: domain.ClassAdvise},
		{name: "armed shape-view is skipped", class: domain.ClassShapeView},
		{name: "armed shape-work is skipped", class: domain.ClassShapeWork},
		{name: "a builtin is never skipped", class: domain.ClassShapeView, builtin: true, wantRun: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			log := &ladderLog{}
			subject := []domain.Reaction{probe(log, "subject", tc.class, acts(domain.Outcome{Edited: true}))}

			var a *Agent
			var sink *recordingSink
			if tc.builtin {
				a, sink = ladderAgent(t, subject, nil)
			} else {
				a, sink = ladderAgent(t, nil, subject)
			}
			swapBypass(a, true)

			if _, err := a.fire(context.Background(), domain.MomentPostResponse, postResponse(true)); err != nil {
				t.Fatalf("fire: %v", err)
			}

			ran := len(log.calls) == 1
			if ran != tc.wantRun {
				t.Errorf("invoked = %v, want %v", ran, tc.wantRun)
			}
			if got := len(firings(sink.events)); (got == 1) != tc.wantRun {
				t.Errorf("firings = %d, want %d", got, boolToInt(tc.wantRun))
			}
		})
	}
}

// boolToInt renders a want-fired expectation as the firing count it implies.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// A deferred correction is carried out by the dispatcher, not the reaction: it lands on the
// conversation's deferred queue, where it survives a snapshot and reaches the NEXT request.
func TestFireQueuesADeferredCorrection(t *testing.T) {
	log := &ladderLog{}
	a, _ := ladderAgent(t, nil, []domain.Reaction{
		probe(log, "defers", domain.ClassObserve, acts(domain.Outcome{Defer: "remember the workspace root"})),
	})

	if _, err := a.fire(context.Background(), domain.MomentPostResponse, postResponse(true)); err != nil {
		t.Fatalf("fire: %v", err)
	}

	injects, ok := a.conv.TakeDeferred()
	if !ok || len(injects) != 1 || injects[0] != "remember the workspace root" {
		t.Errorf("deferred = %v (ok=%v), want the reaction's correction queued once", injects, ok)
	}
}

// Post-response is the one Moment whose reactions may spawn a subprocess, so the ladder's answer
// is installed once ahead of the whole cascade; no other Moment installs one.
func TestFirePostResponseInstallsTheSubprocessPermit(t *testing.T) {
	var granted bool
	seen := false
	reaction := domain.Reaction{
		ID:     "permit-probe",
		Origin: domain.OriginEngine,
		Class:  domain.ClassObserve,
		On:     []domain.Moment{domain.MomentPostResponse},
		Handler: domain.PostResponseFunc(func(ctx context.Context, _ *domain.Response) (domain.Outcome, error) {
			seen = true
			_, granted = domain.SubprocessPermitFromContext(ctx)
			return domain.Outcome{}, nil
		}),
	}

	sink := &recordingSink{}
	cfg := permitConfig(domain.ModeAuto, &fakeConfiner{caps: capsBoth()}, false)
	cfg.Events = sink
	cfg.Reactions = []domain.Reaction{reaction}
	a, err := newAgent(cfg, echoResponder{reply: "reply"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	a.builtins = nil

	if _, err := a.fire(context.Background(), domain.MomentPostResponse, postResponse(true)); err != nil {
		t.Fatalf("fire: %v", err)
	}
	if !seen {
		t.Fatalf("the probe never fired")
	}
	if !granted {
		t.Errorf("permit granted = false, want Auto to install one at post-response")
	}
}

// ---------------------------------------------------------------------------
// Construction
// ---------------------------------------------------------------------------

// An ill-formed reaction, or one shadowing a name something else already answers to, fails
// construction: the firing event, the identity projector and the provenance ledger all key on the
// id, so a duplicate makes every attribution ambiguous.
func TestNewRejectsReactionsItCannotArm(t *testing.T) {
	observer := func(id string) domain.Reaction {
		return domain.Reaction{
			ID:     id,
			Origin: domain.OriginUser,
			Class:  domain.ClassObserve,
			On:     []domain.Moment{domain.MomentPostResponse},
			Handler: domain.PostResponseFunc(func(context.Context, *domain.Response) (domain.Outcome, error) {
				return domain.Outcome{}, nil
			}),
		}
	}

	cases := []struct {
		name      string
		reactions []domain.Reaction
	}{
		{
			name:      "an invalid reaction",
			reactions: []domain.Reaction{{ID: "", Origin: domain.OriginUser, Class: domain.ClassObserve}},
		},
		{
			name:      "an id a builtin already holds",
			reactions: []domain.Reaction{observer(guardToolLoopBreaker)},
		},
		{
			name:      "an id another entry already holds",
			reactions: []domain.Reaction{observer("twice"), observer("twice")},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := baseConfig(&recordingSink{})
			cfg.Reactions = tc.reactions

			_, err := newAgent(cfg, echoResponder{reply: "reply"})

			if !errors.Is(err, domain.ErrInvalidReaction) {
				t.Errorf("newAgent = %v, want ErrInvalidReaction", err)
			}
		})
	}
}

// A well-formed set arms, in registration order, and is what the armed leg fires.
func TestNewArmsConfigReactionsInRegistrationOrder(t *testing.T) {
	log := &ladderLog{}
	cfg := baseConfig(&recordingSink{})
	cfg.Reactions = []domain.Reaction{
		probe(log, "first", domain.ClassObserve, nil),
		probe(log, "second", domain.ClassObserve, nil),
	}

	a, err := newAgent(cfg, echoResponder{reply: "reply"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	a.builtins = nil

	if _, err := a.fire(context.Background(), domain.MomentPostResponse, postResponse(true)); err != nil {
		t.Fatalf("fire: %v", err)
	}
	assertOrder(t, "invocations", log.calls, []string{"first", "second"})
}

// A sub-agent inherits every armed Reaction by default — the unconditional membership
// inheritance a Mechanism already has — and TopLevelOnly is the one opt-out.
func TestInheritedReactionsDropsTopLevelOnly(t *testing.T) {
	inherited := domain.Reaction{ID: "inherited"}
	pinned := domain.Reaction{ID: "pinned", TopLevelOnly: true}

	got := inheritedReactions([]domain.Reaction{inherited, pinned, {ID: "also-inherited"}})

	var ids []string
	for _, r := range got {
		ids = append(ids, r.ID)
	}
	assertOrder(t, "inherited", ids, []string{"inherited", "also-inherited"})

	if kept := inheritedReactions([]domain.Reaction{pinned}); kept != nil {
		t.Errorf("inheritedReactions = %v, want nil when everything opted out", kept)
	}
}

// ---------------------------------------------------------------------------
// The builtins
// ---------------------------------------------------------------------------

// builtinIDs lists the ids of the ladder an Agent is CURRENTLY running, in firing order — the
// enable set as SetReactions last rebuilt it.
func builtinIDs(a *Agent) []string {
	var ids []string
	for _, b := range a.builtinLadder() {
		ids = append(ids, b.spec.ID)
	}
	return ids
}

// With every guard on, the ladder is all seven: well-formed engine-origin, shape-view Reactions,
// each on the seam its handler serves and each booking under its own action label. Validate is
// what pins the handler to the Moment, so a builtin wired to the wrong seam fails here rather
// than in the loop.
func TestBuiltinReactionsAreTheSevenFloorGuardsWhenEveryGuardIsOn(t *testing.T) {
	a, _ := ladderAgent(t, nil, nil)
	builtins := a.buildBuiltins(domain.FloorConfig{})

	type want struct {
		moment domain.Moment
		action string
	}
	expected := map[string]want{
		guardToolCallSalvage:       {domain.MomentPostResponse, guardActionSalvage},
		guardToolLoopBreaker:       {domain.MomentPostResponse, guardActionRetry},
		guardToolCallRepair:        {domain.MomentPostResponse, guardActionRetry},
		guardEmptyResponseRecovery: {domain.MomentPostResponse, guardActionRetry},
		guardToolUseEnforcer:       {domain.MomentPostResponse, guardActionRetry},
		guardReadCache:             {domain.MomentPreToolExec, guardActionIntercept},
		guardToolResultCap:         {domain.MomentPreRequest, guardActionCap},
	}

	if len(builtins) != len(expected) {
		t.Fatalf("builtins = %d, want %d", len(builtins), len(expected))
	}
	// Salvage runs first at post-response so the four recoveries judge the response the model
	// MEANT (ADR 0071).
	if builtins[0].spec.ID != guardToolCallSalvage {
		t.Errorf("first builtin = %q, want the salvage guard", builtins[0].spec.ID)
	}

	for _, b := range builtins {
		if err := b.spec.Validate(); err != nil {
			t.Errorf("%q: Validate = %v", b.spec.ID, err)
			continue
		}
		w, known := expected[b.spec.ID]
		if !known {
			t.Errorf("unexpected builtin %q", b.spec.ID)
			continue
		}
		if b.spec.Origin != domain.OriginEngine || b.spec.Class != domain.ClassShapeView {
			t.Errorf("%q: origin/class = %q/%q, want engine/shape-view", b.spec.ID, b.spec.Origin, b.spec.Class)
		}
		if len(b.spec.On) != 1 || b.spec.On[0] != w.moment {
			t.Errorf("%q: On = %v, want [%s]", b.spec.ID, b.spec.On, w.moment)
		}
		if b.action != w.action {
			t.Errorf("%q: action = %q, want %q", b.spec.ID, b.action, w.action)
		}
	}
}

// A guard whose Floor boolean is off is ABSENT from the ladder rather than present and
// self-skipping (the enable set, ADR 0076 A8): the guards around it keep their relative order,
// and the firing sequence is unchanged because a disabled guard booked nothing before either.
//
// Its ID stays RESERVED all the same. armReactions reserves all seven guard keys whatever the
// enable set holds, so a Config.Reactions entry named after a guard the user switched off is
// refused exactly as loudly as one named after a guard that is on — the name must still be free
// when the Floor swaps back.
func TestBuiltinEnableSetDropsAGuardWhoseBooleanIsOff(t *testing.T) {
	a, _ := ladderAgent(t, nil, nil)

	full := a.buildBuiltins(domain.FloorConfig{})
	trimmed := a.buildBuiltins(domain.FloorConfig{
		DisableToolLoopBreaker: true,
		DisableToolResultCap:   true,
	})

	var got, want []string
	for _, b := range trimmed {
		got = append(got, b.spec.ID)
	}
	for _, b := range full {
		if b.spec.ID == guardToolLoopBreaker || b.spec.ID == guardToolResultCap {
			continue
		}
		want = append(want, b.spec.ID)
	}
	assertOrder(t, "the enable set", got, want)

	// The off guard's name is still taken, so an entry cannot answer to it.
	cfg := baseConfig(&recordingSink{})
	cfg.Floor.DisableToolLoopBreaker = true
	cfg.Reactions = []domain.Reaction{{
		ID:     guardToolLoopBreaker,
		Origin: domain.OriginUser,
		Class:  domain.ClassObserve,
		On:     []domain.Moment{domain.MomentPostResponse},
		Handler: domain.PostResponseFunc(func(context.Context, *domain.Response) (domain.Outcome, error) {
			return domain.Outcome{}, nil
		}),
	}}

	if _, err := newAgent(cfg, echoResponder{reply: "reply"}); !errors.Is(err, domain.ErrInvalidReaction) {
		t.Errorf("newAgent = %v, want ErrInvalidReaction — an off guard still owns its id", err)
	}
}

// A Bypass-only swap must NOT rebuild the ladder: whatever slice the Agent is running stays,
// so anything holding it — a lab ladder installed in place of the seven guards, a cascade
// mid-flight — survives the swap untouched. Only a moved Floor rebuilds, and then the ladder is
// the enable set that Floor implies.
func TestSetReactionsRebuildsTheLadderOnlyWhenTheFloorMoves(t *testing.T) {
	log := &ladderLog{}
	a, _ := ladderAgent(t, []domain.Reaction{probe(log, "installed", domain.ClassShapeView, nil)}, nil)

	a.SetReactions(domain.Generation{Bypass: true})

	assertOrder(t, "the ladder after a Bypass-only swap", builtinIDs(a), []string{"installed"})
	if !a.Generation().Bypass {
		t.Error("Bypass did not land")
	}

	a.SetReactions(domain.Generation{Bypass: true, Floor: domain.FloorConfig{DisableReadCache: true}})

	ids := builtinIDs(a)
	if slices.Contains(ids, "installed") {
		t.Errorf("ladder = %v after a Floor swap, want it rebuilt from the guards", ids)
	}
	if slices.Contains(ids, guardReadCache) {
		t.Errorf("ladder = %v after a Floor swap, want the read cache switched out of it", ids)
	}
	if len(ids) != len(guardIDs)-1 {
		t.Errorf("ladder = %v, want the six guards the Floor leaves on", ids)
	}
}

// The tool-use enforcer is the one builtin the identity arm cannot reach: it needs a committed
// text-only assistant message with no prior tool use, and a single-prompt headless run commits an
// assistant message only through a tool call. This is its coverage — the guard fires through the
// dispatcher on the conversation shape floor/tooluse.go describes, and books a retry.
func TestBuiltinToolUseEnforcer(t *testing.T) {
	sink := &recordingSink{}
	a, err := newAgent(baseConfig(sink), echoResponder{reply: "reply"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	// The stuck-narration lead-up: an action request the model has answered twice with prose,
	// never calling a tool, with a menu it was shown and ignored.
	history := []domain.Message{
		domaintest.UserMessage("please implement feature X"),
		domaintest.AssistantTextMessage("I'll implement feature X."),
		domaintest.UserMessage("continue"),
		domaintest.AssistantTextMessage("Here is my plan."),
		domaintest.UserMessage("please implement feature X now"),
	}
	menu := []domain.ToolDef{{Name: "read_file"}, {Name: "write_file"}}
	view := domain.NewRequest("m", history, menu, domain.Budget{}, 0).View()
	resp := domain.NewResponse("I would edit main.go to add the parser.", "", nil, domain.FinishStop, view)

	out, err := a.fire(context.Background(), domain.MomentPostResponse,
		domain.PostResponseMoment{Resp: resp, Retryable: true})

	if err != nil {
		t.Fatalf("fire: %v", err)
	}
	if !out.Retry {
		t.Fatalf("Outcome = %+v, want the enforcer to ask for a re-stream", out)
	}
	if !strings.Contains(out.Inject, "You MUST use one of the available tools") {
		t.Errorf("Inject = %q, want the enforcer's correction", out.Inject)
	}

	fired := firings(sink.events)
	if len(fired) != 1 {
		t.Fatalf("firings = %+v, want exactly the enforcer", fired)
	}
	if fired[0].Reaction != guardToolUseEnforcer || fired[0].Moment != domain.MomentPostResponse ||
		fired[0].Action != guardActionRetry {
		t.Errorf("firing = %+v, want tool-use-enforcer/post-response/retry", fired[0])
	}
}

// ---------------------------------------------------------------------------
// The seam-closing event
// ---------------------------------------------------------------------------

// seamClosings lists the SeamClosedEvents in a recorded stream, in emission order.
func seamClosings(events []domain.Event) []domain.SeamClosedEvent {
	var closed []domain.SeamClosedEvent
	for _, e := range events {
		if sc, ok := e.(domain.SeamClosedEvent); ok {
			closed = append(closed, sc)
		}
	}
	return closed
}

// seamValueKey reduces a seam payload to the pointer at its heart — the working value fire
// brackets. Comparing that pointer is how a test proves the event carries the very payload fire
// received rather than a copy of it: the three pointer payloads are their own key, and the two
// pair types answer the pointer they wrap.
func seamValueKey(t *testing.T, v any) any {
	t.Helper()
	switch p := v.(type) {
	case *domain.Request:
		return p
	case domain.PostResponseMoment:
		return p.Resp
	case *domain.ToolCallEdit:
		return p
	case domain.ToolResultMoment:
		return p.Edit
	case *domain.Conversation:
		return p
	}
	t.Fatalf("no seam carries a %T payload", v)
	return nil
}

// seamProbe is one seam with the payload fire takes there and a handler that acts on it — the
// three parts a dispatcher test needs to drive a seam other than post-response.
type seamProbe struct {
	moment  domain.Moment
	payload any
	handler domain.Handler
}

// everySeam returns the five seams in loop order, each with its own payload and a handler that
// books a firing there. Every seam gets its own payload value so a test comparing the event's
// Value against it cannot pass by accident.
func everySeam() []seamProbe {
	edited := domain.Outcome{Edited: true}
	call := domain.ToolCall{ID: "c1", Tool: "lookup"}
	return []seamProbe{
		{domain.MomentPreRequest, domain.NewRequest("m", nil, nil, domain.Budget{}, 0),
			domain.PreRequestFunc(func(context.Context, *domain.Request) (domain.Outcome, error) {
				return edited, nil
			})},
		{domain.MomentPostResponse, postResponse(true),
			domain.PostResponseFunc(func(context.Context, *domain.Response) (domain.Outcome, error) {
				return edited, nil
			})},
		{domain.MomentPreToolExec, domain.NewToolCallEdit(&call),
			domain.PreToolExecFunc(func(context.Context, domain.LoopView, *domain.ToolCallEdit) (domain.Outcome, error) {
				return edited, nil
			})},
		{domain.MomentPostToolResult,
			domain.ToolResultMoment{Call: call, Edit: domain.NewToolResultEdit(&domain.ToolResult{CallID: "c1", Content: "42"})},
			domain.PostToolResultFunc(func(context.Context, domain.LoopView, domain.ToolCall, *domain.ToolResultEdit) (domain.Outcome, error) {
				return edited, nil
			})},
		{domain.MomentHistoryRewrite, &domain.Conversation{},
			domain.HistoryRewriteFunc(func(context.Context, *domain.Conversation) (domain.Outcome, error) {
				return edited, nil
			})},
	}
}

// assertSeamClosed reads the one SeamClosedEvent a fire call must have left in the stream and
// checks the three things it promises: which seam closed, which ids were booked while it passed,
// and that Value is the payload fire was handed. It also pins the event LAST, since a closure
// reported before the cascade's own firings would be reporting a pass that had not happened yet.
func assertSeamClosed(t *testing.T, sink *recordingSink, m domain.Moment, payload any, wantFired []string) {
	t.Helper()

	closed := seamClosings(sink.events)
	if len(closed) != 1 {
		t.Fatalf("SeamClosedEvents = %d, want exactly one per fire call", len(closed))
	}
	if _, ok := sink.events[len(sink.events)-1].(domain.SeamClosedEvent); !ok {
		t.Errorf("last event = %T, want the seam closure to follow every firing", sink.events[len(sink.events)-1])
	}
	if closed[0].Seam != m {
		t.Errorf("Seam = %q, want %q", closed[0].Seam, m)
	}
	assertOrder(t, "Fired", closed[0].Fired, wantFired)
	if seamValueKey(t, closed[0].Value) != seamValueKey(t, payload) {
		t.Errorf("Value = %#v, want the payload fire received", closed[0].Value)
	}
}

// Every one of the five seams closes with its own event, carrying the seam that closed, the ids
// booked while it passed, and the working value itself — the fact the five seam-closing notices
// are built on.
func TestFireEmitsSeamClosedForEverySeam(t *testing.T) {
	for _, s := range everySeam() {
		t.Run(string(s.moment), func(t *testing.T) {
			a, sink := ladderAgent(t, nil, []domain.Reaction{{
				ID:      "watcher",
				Origin:  domain.OriginEngine,
				Class:   domain.ClassObserve,
				On:      []domain.Moment{s.moment},
				Handler: s.handler,
			}})

			if _, err := a.fire(context.Background(), s.moment, s.payload); err != nil {
				t.Fatalf("fire(%s): %v", s.moment, err)
			}

			assertSeamClosed(t, sink, s.moment, s.payload, []string{"watcher"})
		})
	}
}

// The closure is UNCONDITIONAL (ADR 0076 A5): a seam whose armed reaction Bypass switched off,
// and a seam with nothing armed at all, both close — with an empty Fired list, which is the
// event saying the pass happened and nothing acted.
func TestFireEmitsSeamClosedUnderBypassAndWhenNothingIsArmed(t *testing.T) {
	t.Run("nothing armed", func(t *testing.T) {
		a, sink := ladderAgent(t, nil, nil)
		payload := postResponse(true)

		if _, err := a.fire(context.Background(), domain.MomentPostResponse, payload); err != nil {
			t.Fatalf("fire: %v", err)
		}

		assertSeamClosed(t, sink, domain.MomentPostResponse, payload, nil)
	})

	t.Run("bypass", func(t *testing.T) {
		log := &ladderLog{}
		a, sink := ladderAgent(t, nil, []domain.Reaction{
			probe(log, "advisor", domain.ClassAdvise, acts(domain.Outcome{Edited: true})),
		})
		swapBypass(a, true)
		payload := postResponse(true)

		if _, err := a.fire(context.Background(), domain.MomentPostResponse, payload); err != nil {
			t.Fatalf("fire: %v", err)
		}

		assertOrder(t, "invocations", log.calls, nil)
		assertSeamClosed(t, sink, domain.MomentPostResponse, payload, nil)
	})
}

// A reaction that returns an error ends the cascade, and the seam still closes: the event
// follows the error out and its Fired list holds the ids that acted before the fault, so an
// observer reading it sees exactly how far the pass got.
func TestFireEmitsSeamClosedAfterAReturnedError(t *testing.T) {
	boom := errors.New("boom")
	log := &ladderLog{}
	a, sink := ladderAgent(t, nil, []domain.Reaction{
		probe(log, "acted", domain.ClassObserve, acts(domain.Outcome{Edited: true})),
		probe(log, "broke", domain.ClassObserve, func(*domain.Response) (domain.Outcome, error) {
			return domain.Outcome{}, boom
		}),
		probe(log, "never", domain.ClassObserve, acts(domain.Outcome{Edited: true})),
	})
	payload := postResponse(true)

	_, err := a.fire(context.Background(), domain.MomentPostResponse, payload)

	if !errors.Is(err, boom) {
		t.Fatalf("fire err = %v, want the reaction's error", err)
	}
	assertOrder(t, "invocations", log.calls, []string{"acted", "broke"})
	assertSeamClosed(t, sink, domain.MomentPostResponse, payload, []string{"acted"})
}

// postResponseClosings narrows a recorded stream's seam closings to the post-response Moment —
// the one seam a single Turn can pass more than once, since a retry hands the Turn back for
// another attempt.
func postResponseClosings(events []domain.Event) []domain.SeamClosedEvent {
	var out []domain.SeamClosedEvent
	for _, sc := range seamClosings(events) {
		if sc.Seam == domain.MomentPostResponse {
			out = append(out, sc)
		}
	}
	return out
}

// armedSeamWatcher is an armed post-response Reaction that ACTS on every pass it gets. Bypass is
// off wherever it is used, so its id appearing in a closure's Fired list is the proof the armed
// leg was reached at all — and its absence, the proof the leg was skipped.
func armedSeamWatcher(id string) domain.Reaction {
	return postResponseReaction(id, func(context.Context, *domain.Response) (domain.Outcome, error) {
		return domain.Outcome{Edited: true}, nil
	})
}

// The post-response seam closes once per ATTEMPT, not once per Turn: the deferred emit sits on
// `fire`, and the retry hand-back is a return from `fire` like every other (ADR 0076 A5). So a
// Turn whose first response trips a retrying Floor guard and whose second stands closes the seam
// TWICE — the first closure carrying only the guard that asked for the re-stream, because the
// hand-back returns before the armed leg is reached, and the second carrying the armed leg that
// finally got its pass.
func TestPostResponseSeamClosesOncePerAttemptAcrossARetry(t *testing.T) {
	t.Run("retry then success", func(t *testing.T) {
		sink := &recordingSink{}
		cfg := configWithTools(sink, fakeTool{name: "lookup", readOnly: true, result: "42"})
		cfg.Reactions = []domain.Reaction{armedSeamWatcher("watcher")}
		responder := &captureAllResponder{scripts: [][]provider.Delta{
			toolCallScript("c1", "frobnicate", `{}`), // not in the menu — the repair guard re-streams
			contentScript("done"),                    // the retried attempt, which stands
		}}

		a, err := newAgent(cfg, responder)
		if err != nil {
			t.Fatalf("newAgent: %v", err)
		}
		runExchange(t, a, "look it up")

		closed := postResponseClosings(sink.events)

		if len(closed) != 2 {
			t.Fatalf("post-response closings = %d, want 2 — one per attempt", len(closed))
		}
		assertOrder(t, "Fired (first attempt)", closed[0].Fired, []string{guardToolCallRepair})
		assertOrder(t, "Fired (retried attempt)", closed[1].Fired, []string{"watcher"})
	})

	t.Run("single pass", func(t *testing.T) {
		sink := &recordingSink{}
		cfg := configWithTools(sink, fakeTool{name: "lookup", readOnly: true, result: "42"})
		cfg.Reactions = []domain.Reaction{armedSeamWatcher("watcher")}
		responder := &captureAllResponder{scripts: [][]provider.Delta{
			contentScript("done"), // no guard trips — one attempt, one closure
		}}

		a, err := newAgent(cfg, responder)
		if err != nil {
			t.Fatalf("newAgent: %v", err)
		}
		runExchange(t, a, "look it up")

		closed := postResponseClosings(sink.events)

		if len(closed) != 1 {
			t.Fatalf("post-response closings = %d, want 1 for a single-pass Turn", len(closed))
		}
		assertOrder(t, "Fired", closed[0].Fired, []string{"watcher"})
	})
}
