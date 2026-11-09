package agent

// The Reaction dispatcher (reactions.go) and the engine's builtins (builtins.go). Every test
// here drives the ONE harness the ladder has — a Moment plus its payload in, an Outcome and a
// stream of ReactionFiredEvents out — because that is the whole interface: the seams that call
// fire have nothing else to give it and read nothing else back.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/domain/domaintest"
	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/testtime"
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
	a, err := newAgent(baseConfig(sink), echoResponder(t, "reply"))
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
// and gate stay on because neither can make a model do worse, and a shape-view builtin — a Floor
// guard — is never withdrawn at all. The one builtin Bypass reaches is a builtin of class ADVISE
// (the context-fill notice, ADR 0077 D1), skipped like any armed advise reaction. A skipped
// reaction is SILENT — it is never invoked and books nothing.
//
// A gate stays on under Bypass but never reaches THIS cascade: its cell is the Approver stage
// applyGates runs after resolve() (gate.go), so the cascade skips class gate at every seam and
// TestGateDenyStillDeniesUnderBypass is where its survival under Bypass is pinned.
func TestFireBypassMatrix(t *testing.T) {
	cases := []struct {
		name    string
		class   domain.Class
		builtin bool
		wantRun bool
	}{
		{name: "armed observe survives", class: domain.ClassObserve, wantRun: true},
		{name: "an armed gate never reaches the cascade", class: domain.ClassGate},
		{name: "armed advise is skipped", class: domain.ClassAdvise},
		{name: "armed shape-view is skipped", class: domain.ClassShapeView},
		{name: "armed shape-work is skipped", class: domain.ClassShapeWork},
		{name: "a builtin shape-view is never skipped", class: domain.ClassShapeView, builtin: true, wantRun: true},
		{name: "a builtin advise is skipped", class: domain.ClassAdvise, builtin: true},
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
			swapBypass(t, a, true)

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

// No cascade installs a subprocess permit ahead of its handlers — post-response under Auto with
// confine off, the one row that used to mint an unfenced permit, included. Absence is the refusal
// default (confinement-execution-contract §10.2); the sync lane mints its own per spawn.
func TestFirePostResponseInstallsNoSubprocessPermit(t *testing.T) {
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
	a, err := newAgent(cfg, echoResponder(t, "reply"))
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
	if granted {
		t.Errorf("permit granted = true, want no permit at post-response, Auto included")
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
			// The notice's switch is off in baseConfig, and its id is reserved all the same —
			// the same terms as an off guard's (ADR 0077).
			name:      "the context-fill notice's id while its switch is off",
			reactions: []domain.Reaction{observer(contextFillNoticeID)},
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

			_, err := newAgent(cfg, echoResponder(t, "reply"))

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

	a, err := newAgent(cfg, echoResponder(t, "reply"))
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
	builtins := a.buildBuiltins(domain.FloorConfig{}, false)

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

// With the context-fill notice switched on the ladder is eight: the seven guards as above and,
// LAST, the notice — engine origin, class advise (the one builtin of that class, ADR 0077), on
// post-tool-result alone, booking under its own action label — so what it measures is the tool
// result as every guard ahead of it left it. The guards are untouched by the switch.
func TestBuiltinReactionsAddTheContextFillNoticeLastWhenItsSwitchIsOn(t *testing.T) {
	a, _ := ladderAgent(t, nil, nil)
	builtins := a.buildBuiltins(domain.FloorConfig{}, true)

	if len(builtins) != len(guardIDs)+1 {
		t.Fatalf("builtins = %d, want the seven guards and the notice", len(builtins))
	}
	for i, id := range guardIDs {
		if builtins[i].spec.ID != id || builtins[i].spec.Class != domain.ClassShapeView {
			t.Errorf("builtin %d = %q/%q, want guard %q shape-view", i, builtins[i].spec.ID, builtins[i].spec.Class, id)
		}
	}

	notice := builtins[len(builtins)-1]
	if err := notice.spec.Validate(); err != nil {
		t.Errorf("%q: Validate = %v", notice.spec.ID, err)
	}
	if notice.spec.ID != contextFillNoticeID || notice.action != actionNotice {
		t.Errorf("last builtin = %q booking %q, want %q booking %q", notice.spec.ID, notice.action, contextFillNoticeID, actionNotice)
	}
	if notice.spec.Origin != domain.OriginEngine || notice.spec.Class != domain.ClassAdvise {
		t.Errorf("%q: origin/class = %q/%q, want engine/advise", notice.spec.ID, notice.spec.Origin, notice.spec.Class)
	}
	if len(notice.spec.On) != 1 || notice.spec.On[0] != domain.MomentPostToolResult {
		t.Errorf("%q: On = %v, want [post-tool-result]", notice.spec.ID, notice.spec.On)
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

	full := a.buildBuiltins(domain.FloorConfig{}, false)
	trimmed := a.buildBuiltins(domain.FloorConfig{
		DisableToolLoopBreaker: true,
		DisableToolResultCap:   true,
	}, false)

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

	if _, err := newAgent(cfg, echoResponder(t, "reply")); !errors.Is(err, domain.ErrInvalidReaction) {
		t.Errorf("newAgent = %v, want ErrInvalidReaction — an off guard still owns its id", err)
	}
}

// A Bypass-only swap must NOT rebuild the ladder: whatever slice the Agent is running stays,
// so anything holding it — a lab ladder installed in place of the seven guards, a cascade
// mid-flight — survives the swap untouched. Only a moved enable-set input rebuilds — the Floor
// here, the context-fill notice switch in the test below — and then the ladder is the enable set
// that input implies.
func TestSetReactionsRebuildsTheLadderOnlyWhenTheFloorMoves(t *testing.T) {
	log := &ladderLog{}
	a, _ := ladderAgent(t, []domain.Reaction{probe(log, "installed", domain.ClassShapeView, nil)}, nil)

	mustSetReactions(t, a, domain.Generation{Bypass: true})

	assertOrder(t, "the ladder after a Bypass-only swap", builtinIDs(a), []string{"installed"})
	if !a.Generation().Bypass {
		t.Error("Bypass did not land")
	}

	mustSetReactions(t, a, domain.Generation{Bypass: true, Floor: domain.FloorConfig{DisableReadCache: true}})

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

// The context-fill notice switch (Generation.ContextFillNotice, ADR 0077) is the second enable-set
// input: a swap that moves ONLY it rebuilds the ladder exactly as a moved Floor does, and a swap
// that moves neither leaves the installed slice untouched — the notice is absent from the ladder
// while off rather than self-skipping, so its switch has to reach buildBuiltins.
func TestSetReactionsRebuildsTheLadderWhenOnlyTheNoticeSwitchMoves(t *testing.T) {
	log := &ladderLog{}
	a, _ := ladderAgent(t, []domain.Reaction{probe(log, "installed", domain.ClassShapeView, nil)}, nil)

	mustSetReactions(t, a, domain.Generation{})
	assertOrder(t, "the ladder after a swap that moved nothing", builtinIDs(a), []string{"installed"})

	mustSetReactions(t, a, domain.Generation{ContextFillNotice: true})

	if !a.Generation().ContextFillNotice {
		t.Error("the notice switch did not land")
	}
	ids := builtinIDs(a)
	if slices.Contains(ids, "installed") {
		t.Errorf("ladder = %v after a notice-only swap, want it rebuilt from the guards", ids)
	}
	if len(ids) != len(guardIDs)+1 || ids[len(ids)-1] != contextFillNoticeID {
		t.Errorf("ladder = %v, want the seven guards the zero Floor leaves on and the notice last", ids)
	}

	// Moving it back is a move too, and a second identical generation is not.
	mustSetReactions(t, a, domain.Generation{})
	if a.Generation().ContextFillNotice {
		t.Error("the notice switch did not clear")
	}
	a.builtins = append(a.builtins, armedReaction{spec: probe(log, "installed-again", domain.ClassShapeView, nil)})
	mustSetReactions(t, a, domain.Generation{})
	if ids := builtinIDs(a); !slices.Contains(ids, "installed-again") {
		t.Errorf("ladder = %v after a swap that moved nothing, want the installed slice kept", ids)
	}
}

// The tool-use enforcer is the one builtin the identity arm cannot reach: it needs a committed
// text-only assistant message with no prior tool use, and a single-prompt headless run commits an
// assistant message only through a tool call. This is its coverage — the guard fires through the
// dispatcher on the conversation shape floor/tooluse.go describes, and books a retry.
func TestBuiltinToolUseEnforcer(t *testing.T) {
	sink := &recordingSink{}
	a, err := newAgent(baseConfig(sink), echoResponder(t, "reply"))
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
		swapBypass(t, a, true)
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
		responder := scriptedResponder(t,
			toolCallTurn("c1", "frobnicate", `{}`), // not in the menu — the repair guard re-streams
			contentTurn("done"),                    // the retried attempt, which stands
		)

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
		responder := scriptedResponder(t,
			contentTurn("done"), // no guard trips — one attempt, one closure
		)

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

// ---------------------------------------------------------------------------
// session-started entries armed before the first prompt (sessionBriefing)
// ---------------------------------------------------------------------------

// sessionStartedEntry is an observe entry subscribing to the session-started notice; project marks
// it as an adopted repo entry rather than a global one.
func sessionStartedEntry(id string, project bool) domain.Reaction {
	return domain.Reaction{
		ID:      id,
		Origin:  domain.OriginUser,
		Class:   domain.ClassObserve,
		On:      []domain.Moment{domain.MomentSessionStarted},
		Handler: domain.ArgvHandler{Argv: []string{"true"}},
		Timeout: time.Second,
		Project: project,
	}
}

// windowAgent constructs a top-level Agent over a recording Runner, its observe lane seeded with
// seed, and announces its construction boundary as New does.
func windowAgent(t *testing.T, seed ...domain.Reaction) (*Agent, *startRecordingRunner) {
	t.Helper()
	runner := &startRecordingRunner{}
	cfg := baseConfig(&recordingSink{})
	cfg.ObserveRunner = runner
	cfg.Observe = seed
	a, err := New(cfg, WithDialer(dialerTo(echoResponder(t, "reply")).dial))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return a, runner
}

// armObserve swaps a's observe lane to list, every other field of the generation kept.
func armObserve(t *testing.T, a *Agent, list ...domain.Reaction) {
	t.Helper()
	gen := a.Generation()
	gen.Observe = list
	if err := a.SetReactions(gen); err != nil {
		t.Fatalf("SetReactions: %v", err)
	}
}

// TestSessionWindow_AnEntryArmedBeforeTheFirstPromptFiresOnce — an entry a live swap arms before the
// session's first user message fires once for that session, with the boundary's start, through the
// narrowed door; a reload of the unchanged list fires nothing more, and an entry added beside it
// fires alone. A global entry and an adopted repo entry are treated alike.
func TestSessionWindow_AnEntryArmedBeforeTheFirstPromptFiresOnce(t *testing.T) {
	t.Parallel()

	for _, project := range []bool{true, false} {
		a, runner := windowAgent(t)
		primer := sessionStartedEntry("primer", project)

		armObserve(t, a, primer)
		armObserve(t, a, primer)
		armObserve(t, a, primer, sessionStartedEntry("second", project))

		want := []armedCall{
			{start: domain.SessionStartNew, ids: []string{"primer"}},
			{start: domain.SessionStartNew, ids: []string{"second"}},
		}
		if got := runner.armedCalls(); !slices.EqualFunc(got, want, armedCallEqual) {
			t.Errorf("project=%v: narrowed firings = %+v, want %+v", project, got, want)
		}
		if got := runner.recorded(); !slices.Equal(got, []domain.SessionStart{domain.SessionStartNew}) {
			t.Errorf("project=%v: boundary announcements = %v, want the construction's alone", project, got)
		}
	}
}

// TestSessionWindow_AnEntryArmedAfterTheFirstPromptWaitsForTheNextBoundary — once the session has
// sent its first user message, a newly armed entry fires nothing until the next boundary, whose own
// announcement then reaches it; that boundary reopens the window for the entries armed after it.
func TestSessionWindow_AnEntryArmedAfterTheFirstPromptWaitsForTheNextBoundary(t *testing.T) {
	t.Parallel()

	a, runner := windowAgent(t)
	if err := a.Submit(domain.UserInput{Text: "hello"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	armObserve(t, a, sessionStartedEntry("primer", true))
	if got := runner.armedCalls(); len(got) != 0 {
		t.Fatalf("an entry armed after the first prompt fired %+v; want nothing until the next boundary", got)
	}
	if _, err := a.Step(context.Background()); err != nil {
		t.Fatalf("Step: %v", err)
	}

	if err := a.ClearContext(); err != nil {
		t.Fatalf("ClearContext: %v", err)
	}
	armObserve(t, a, sessionStartedEntry("primer", true), sessionStartedEntry("late", false))

	if got, want := runner.recorded(), []domain.SessionStart{domain.SessionStartNew, domain.SessionStartClear}; !slices.Equal(got, want) {
		t.Errorf("boundary announcements = %v, want %v", got, want)
	}
	want := []armedCall{{start: domain.SessionStartClear, ids: []string{"late"}}}
	if got := runner.armedCalls(); !slices.EqualFunc(got, want, armedCallEqual) {
		t.Errorf("narrowed firings = %+v, want only the entry armed after the clear: %+v", got, want)
	}
}

// TestSessionWindow_AnEntryRemovedAndAddedBackFiresOnce — "newly armed" is read against the
// boundary's fired-id set, never the list the swap replaced: an entry removed and added back before
// the first prompt does not fire twice, and neither does one the boundary's announcement reached.
func TestSessionWindow_AnEntryRemovedAndAddedBackFiresOnce(t *testing.T) {
	t.Parallel()

	seeded := sessionStartedEntry("seeded", false)
	a, runner := windowAgent(t, seeded)
	primer := sessionStartedEntry("primer", true)

	armObserve(t, a, seeded, primer)
	armObserve(t, a)
	armObserve(t, a, seeded, primer)

	want := []armedCall{{start: domain.SessionStartNew, ids: []string{"primer"}}}
	if got := runner.armedCalls(); !slices.EqualFunc(got, want, armedCallEqual) {
		t.Errorf("narrowed firings = %+v, want %+v", got, want)
	}
}

// TestSessionWindow_EachBoundaryKindOpensTheWindow — a `--resume`d construction, a live restore and
// a fork restore each open the window with their own start, so an entry armed before the first
// prompt fires with it.
func TestSessionWindow_EachBoundaryKindOpensTheWindow(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		boundary func(t *testing.T) (*Agent, *startRecordingRunner)
		want     domain.SessionStart
	}{
		{"a resumed construction", func(t *testing.T) (*Agent, *startRecordingRunner) {
			runner := &startRecordingRunner{}
			cfg := baseConfig(&recordingSink{})
			cfg.ObserveRunner = runner
			a, err := Resume(cfg, emptySession(t), WithDialer(dialerTo(echoResponder(t, "reply")).dial))
			if err != nil {
				t.Fatalf("Resume: %v", err)
			}
			t.Cleanup(func() { _ = a.Close() })
			return a, runner
		}, domain.SessionStartNew},
		{"a live restore", func(t *testing.T) (*Agent, *startRecordingRunner) {
			a, runner := windowAgent(t)
			if err := a.RestoreSession(emptySession(t)); err != nil {
				t.Fatalf("RestoreSession: %v", err)
			}
			return a, runner
		}, domain.SessionStartResume},
		{"a fork restore", func(t *testing.T) (*Agent, *startRecordingRunner) {
			a, runner := windowAgent(t)
			a.MarkFork()
			if err := a.RestoreSession(emptySession(t)); err != nil {
				t.Fatalf("RestoreSession: %v", err)
			}
			return a, runner
		}, domain.SessionStartFork},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a, runner := tc.boundary(t)

			armObserve(t, a, sessionStartedEntry("primer", true))

			want := []armedCall{{start: tc.want, ids: []string{"primer"}}}
			if got := runner.armedCalls(); !slices.EqualFunc(got, want, armedCallEqual) {
				t.Errorf("narrowed firings = %+v, want %+v", got, want)
			}
		})
	}
}

// TestSessionWindow_ASeededConstructionFiresThroughItsAnnouncement — the window opens with the
// construction's own announcement over the seeded observe list, so an entry the seed carried fires
// through that announcement, and a swap that re-arms it before the first prompt (a settings reload
// of the unchanged list) never also fires it through the narrowed door.
func TestSessionWindow_ASeededConstructionFiresThroughItsAnnouncement(t *testing.T) {
	t.Parallel()

	a, runner := windowAgent(t, sessionStartedEntry("primer", true))

	armObserve(t, a, sessionStartedEntry("primer", true))

	if got := runner.armedCalls(); len(got) != 0 {
		t.Errorf("narrowed firings = %+v; want none — the announcement reached the seeded entry", got)
	}
	if got := runner.recorded(); !slices.Equal(got, []domain.SessionStart{domain.SessionStartNew}) {
		t.Errorf("boundary announcements = %v, want the construction's once", got)
	}
}

// armedCallEqual compares two narrowed firings by start and ids.
func armedCallEqual(a, b armedCall) bool {
	return a.start == b.start && slices.Equal(a.ids, b.ids)
}

// ---------------------------------------------------------------------------
// Pre-request advice (ADR 0076 D6: the request-tail advise slot)
// ---------------------------------------------------------------------------

// preRequestAdviceText is what every pre-request advise entry below prints: the advice the model
// reads inside the fence.
const preRequestAdviceText = "keep the reply short"

// preRequestCoach is one user advise entry on pre-request whose command prints text. It is armed
// straight onto the ladder (preRequestAdviceAgent), never through Reaction.Validate, which does not
// admit the Moment for a user advise yet.
func preRequestCoach(argv ...string) domain.Reaction {
	return userAdvise("coach", []domain.Moment{domain.MomentPreRequest}, argv...)
}

// printsAdvice is a command that prints text with no trailing newline, so the fence carries it
// byte-exact.
func printsAdvice(text string) []string {
	return []string{"/bin/sh", "-c", "printf '%s' '" + text + "'"}
}

// preRequestAdviceAgent builds an Agent over up from cfg with entries appended to its ARMED leg as
// they stand, a list_dir tool to ask for, and a Report sink so a failure's operator line can be
// read back. Confinement is off: the argv entries run unfenced, as these tests mean.
func preRequestAdviceAgent(
	t *testing.T,
	cfg domain.Config,
	up *scriptedUpstream,
	reported *[]string,
	entries ...domain.Reaction,
) *Agent {
	t.Helper()

	reg := domain.NewToolRegistry()
	if err := reg.Register(fakeTool{name: "list_dir", readOnly: true, result: "main.go"}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	cfg.Tools = reg
	cfg.WorkspaceDir = t.TempDir()
	cfg.Unconfined = true
	cfg.Report = func(msg string) { *reported = append(*reported, msg) }

	a, err := newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	for _, entry := range entries {
		a.armed = append(a.armed, armedReaction{spec: entry})
	}
	return a
}

// preRequestFence is the fence the coach entry's advice renders to at turn, leading on a user tail
// and trailing on a tool tail.
func preRequestFence(turn int, leading bool) string {
	return domain.RenderAdvice(domain.AdviceSpan{
		Reaction: "coach",
		Origin:   domain.OriginUser,
		Moment:   domain.MomentPreRequest,
		Turn:     turn,
		Leading:  leading,
	}, preRequestAdviceText)
}

// tailOf is the last message a captured request carried.
func tailOf(t *testing.T, req stubllm.Request) stubllm.Message {
	t.Helper()
	if len(req.Messages) == 0 {
		t.Fatal("the captured request carried no messages")
	}
	return req.Messages[len(req.Messages)-1]
}

// fenceCount is how many advice fences a captured request carried, over every message.
func fenceCount(req stubllm.Request) int {
	count := 0
	for _, m := range req.Messages {
		count += strings.Count(m.Content, domain.AdviceFencePrefix)
	}
	return count
}

// adviseFirings is every booked firing of the coach entry.
func adviseFirings(sink *recordingSink) []domain.ReactionFiredEvent {
	var out []domain.ReactionFiredEvent
	for _, fe := range firings(sink.events) {
		if fe.Reaction == "coach" {
			out = append(out, fe)
		}
	}
	return out
}

// The slot's whole contract across one Exchange of two requests: each request carries the advice
// its own pre-request cascade produced, fenced by the tail's role — leading inside the user message
// that opens the Exchange, trailing on the tool result that closes the Turn before — and nothing of
// the request before it; the conversation and the snapshot never carry it; and each landing is
// booked as an advise firing whose Detail is the text the fence carries.
func TestPreRequestAdviceLandsOnTheRequestTailOnly(t *testing.T) {
	skipWithoutPOSIXShell(t)
	t.Parallel()

	sink := &recordingSink{}
	var reported []string
	up := scriptedResponder(t, toolCallTurn("c1", "list_dir", `{}`), contentTurn("done"))
	a := preRequestAdviceAgent(t, baseConfig(sink), up, &reported, preRequestCoach(printsAdvice(preRequestAdviceText)...))

	runExchange(t, a, "hi")

	requests := up.requests()
	if len(requests) != 2 {
		t.Fatalf("upstream saw %d requests, want 2", len(requests))
	}
	first := tailOf(t, requests[0])
	if first.Role != string(domain.RoleUser) || first.Content != preRequestFence(0, true)+"hi" {
		t.Errorf("request 1 tail = {%s %q}, want the user message led by the Turn-0 fence", first.Role, first.Content)
	}
	committed := a.conv.At(a.conv.Len() - 2) // the tool result, before the final reply
	tail := tailOf(t, requests[1])
	if tail.Role != string(domain.RoleTool) || tail.Content != committed.Content+preRequestFence(1, false) {
		t.Errorf("request 2 tail = {%s %q}, want the tool result %q trailed by the Turn-1 fence",
			tail.Role, tail.Content, committed.Content)
	}
	if got := fenceCount(requests[1]); got != 1 {
		t.Errorf("request 2 carried %d fences, want 1 — the Turn-0 advice is request-only", got)
	}

	for i := 0; i < a.conv.Len(); i++ {
		if m := a.conv.At(i); len(m.Advice) != 0 || strings.Contains(m.Content, preRequestAdviceText) {
			t.Errorf("conversation message %d carries the advice: %+v", i, m)
		}
	}
	snap, err := a.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if strings.Contains(string(snap.State), preRequestAdviceText) {
		t.Error("the snapshot carries the advice; it lives on the request copy alone")
	}

	fired := adviseFirings(sink)
	if len(fired) != 2 {
		t.Fatalf("coach booked %d firings, want 2 (one per request): %+v", len(fired), fired)
	}
	for _, fe := range fired {
		if fe.Action != actionAdvise || fe.Moment != domain.MomentPreRequest || fe.Detail != preRequestAdviceText {
			t.Errorf("firing = {Action:%q Moment:%q Detail:%q}, want {advise pre-request %q}",
				fe.Action, fe.Moment, fe.Detail, preRequestAdviceText)
		}
	}
	if len(reported) != 0 {
		t.Errorf("reporter said %q, want nothing", reported)
	}
}

// requestTailSink snapshots the request's last message INSIDE Emit at every pre-request
// SeamClosedEvent — the only moment the reference the event carries is valid — so a fence added
// after the emit would be missing from it, where recordingSink's kept pointer would show it.
type requestTailSink struct {
	recordingSink
	tails []domain.Message
}

func (s *requestTailSink) Emit(e domain.Event) {
	s.recordingSink.Emit(e)
	closed, ok := e.(domain.SeamClosedEvent)
	if !ok || closed.Seam != domain.MomentPreRequest {
		return
	}
	req, ok := closed.Value.(*domain.Request)
	if !ok {
		return
	}
	messages := req.State().Messages
	s.tails = append(s.tails, messages[len(messages)-1])
}

// A pre-request-finished subscriber is promised the request "as the loop will send it", so the
// fence has to be on the request before the seam closes, not stamped after it.
func TestPreRequestAdviceIsOnTheRequestThePreRequestSeamClosesOver(t *testing.T) {
	skipWithoutPOSIXShell(t)
	t.Parallel()

	sink := &requestTailSink{}
	var reported []string
	a := preRequestAdviceAgent(t, baseConfig(sink), echoResponder(t, "done"), &reported,
		preRequestCoach(printsAdvice(preRequestAdviceText)...))

	stepOnce(t, a, "hi")

	if len(sink.tails) != 1 {
		t.Fatalf("saw %d pre-request closings, want 1", len(sink.tails))
	}
	if got, want := sink.tails[0].Content, preRequestFence(0, true)+"hi"; got != want {
		t.Errorf("the closing seam's request tail = %q, want %q", got, want)
	}
}

// The command reads the documented pre-request document: the event, and under "value" the request
// as the cascade has shaped it so far, in the shape a pre-request-finished notice carries.
func TestPreRequestAdviceHandsTheCommandTheRequest(t *testing.T) {
	skipWithoutPOSIXShell(t)
	t.Parallel()

	sink := &recordingSink{}
	var reported []string
	stdin := filepath.Join(t.TempDir(), "stdin.json")
	a := preRequestAdviceAgent(t, baseConfig(sink), echoResponder(t, "done"), &reported,
		preRequestCoach("/bin/sh", "-c", `cat > "$0"; printf '%s' '`+preRequestAdviceText+`'`, stdin))

	stepOnce(t, a, "hi")

	raw, err := os.ReadFile(stdin)
	if err != nil {
		t.Fatalf("the command left no document: %v", err)
	}
	var doc struct {
		Event string `json:"event"`
		Value struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
			Tools []string `json:"tools"`
		} `json:"value"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("document is not JSON: %v\n%s", err, raw)
	}
	if doc.Event != string(domain.MomentPreRequest) {
		t.Errorf("event = %q, want %q", doc.Event, domain.MomentPreRequest)
	}
	messages := doc.Value.Messages
	if len(messages) == 0 || messages[len(messages)-1].Role != "user" || messages[len(messages)-1].Content != "hi" {
		t.Errorf("value.messages = %+v, want it to end on the unfenced user message", messages)
	}
	if !slices.Contains(doc.Value.Tools, "list_dir") {
		t.Errorf("value.tools = %v, want the offered list_dir", doc.Value.Tools)
	}
}

// pre-request runs per REQUEST, so a request rebuilt after an overflow fold runs it again: the
// retried request carries the fresh cascade's fence and only that one, and both firings are booked.
func TestPreRequestAdviceRerunsOnARefold(t *testing.T) {
	skipWithoutPOSIXShell(t)

	sink := &recordingSink{}
	var reported []string
	up := recoveryResponder(t, "recovered reply", "EMERGENCY-SUMMARY", true)
	a := preRequestAdviceAgent(t, autoCompactConfig(sink), up, &reported,
		preRequestCoach(printsAdvice(preRequestAdviceText)...))
	seedToolCallConv(a)

	runExchange(t, a, "keep going")

	mains := up.mains()
	if len(mains) != 2 {
		t.Fatalf("main requests = %d, want 2 (the overflowed one and the retry)", len(mains))
	}
	fired := adviseFirings(sink)
	if len(fired) != 2 {
		t.Fatalf("coach booked %d firings, want 2 (the first request and the rebuilt one): %+v", len(fired), fired)
	}
	retry := mains[1]
	if got := fenceCount(retry); got != 1 {
		t.Errorf("the retried request carried %d fences, want exactly 1", got)
	}
	if tail := tailOf(t, retry); tail.Content != preRequestFence(fired[1].Turn, true)+overflowBridge {
		t.Errorf("retry tail = %q, want the user bridge led by the fence", tail.Content)
	}
}

// Bypass switches advise off before its handler is reached: no fence, no firing.
func TestPreRequestAdviceIsSkippedUnderBypass(t *testing.T) {
	skipWithoutPOSIXShell(t)
	t.Parallel()

	sink := &recordingSink{}
	var reported []string
	cfg := baseConfig(sink)
	cfg.Bypass = true
	up := echoResponder(t, "done")
	a := preRequestAdviceAgent(t, cfg, up, &reported, preRequestCoach(printsAdvice(preRequestAdviceText)...))

	stepOnce(t, a, "hi")

	if tail := tailOf(t, up.last()); tail.Content != "hi" {
		t.Errorf("request tail = %q under Bypass, want the bare user message", tail.Content)
	}
	if fired := adviseFirings(sink); len(fired) != 0 {
		t.Errorf("coach booked %+v under Bypass, want nothing", fired)
	}
}

// A command that outlives its deadline costs the request nothing (D7): it goes out unfenced, the
// Turn completes, and the failure is said once to the operator and once as a "failed" firing.
func TestPreRequestAdviceFailsOpenOnATimeout(t *testing.T) {
	skipWithoutPOSIXShell(t)
	t.Parallel()

	sink := &recordingSink{}
	var reported []string
	entry := preRequestCoach("/bin/sh", "-c", "sleep 5")
	entry.Timeout = testtime.Fixed(150 * time.Millisecond)
	up := echoResponder(t, "done")
	a := preRequestAdviceAgent(t, baseConfig(sink), up, &reported, entry)

	res := stepOnce(t, a, "hi")

	if res.Status != domain.StatusExchangeComplete {
		t.Errorf("status = %q, want %q — a failed advise never abandons the Turn",
			res.Status, domain.StatusExchangeComplete)
	}
	if tail := tailOf(t, up.last()); tail.Content != "hi" {
		t.Errorf("request tail = %q, want the bare user message", tail.Content)
	}
	fired := adviseFirings(sink)
	if len(fired) != 1 || fired[0].Action != "failed" || !strings.Contains(fired[0].Detail, "timed out") {
		t.Errorf("firings = %+v, want exactly one booked failed naming the timeout", fired)
	}
	if len(reported) != 1 || !strings.HasPrefix(reported[0], "reaction coach (pre-request): ") {
		t.Errorf("reporter said %q, want one observe-lane sentence", reported)
	}
}
