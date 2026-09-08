package agent

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/airiclenz/apogee/internal/domain"
)

// The Reaction dispatcher (ADR 0076 D1; docs/design/reaction-core-greenfield.md §9.2).
//
// One ladder runs at every seam Moment, and fire below is the whole of it: the engine's own
// builtins first — the seven Floor guards (builtins.go) — then the Reactions the host armed on
// Config.Reactions, in registration order. Each reaction is skipped or run, bracketed by the
// working value's Revision counter, booked when it ACTED, and reported as one
// ReactionFiredEvent. There is no second ladder and no second firing event: a Floor guard, a
// bench instrument and (from stage 2) a user's entry are the same kind of thing arriving at the
// same Moment, and this is where they all arrive.
//
// The rules the cascade applies, each ratified in the stage-1 plan header:
//
//   - BYPASS (D9) switches off ARMED reactions of class advise, shape-view and shape-work. The
//     builtins never consult it — a Floor guard cannot regress Bypass, so it is never withdrawn
//     — and observe and gate reactions stay on because neither can make a model do worse.
//     A skipped reaction is silent: no event, no ledger entry, nothing to read back.
//   - A PANIC is recovered at the extension boundary (ADR 0007), reported as an ErrorEvent
//     attributed to the reaction, and the cascade CONTINUES: a broken extension degrades to a
//     reaction that did nothing rather than to a degraded Turn.
//   - A returned ERROR is the opposite and ends the cascade at once, surfacing to the seam,
//     whose own disposition decides what it costs (a pre-request error abandons the Turn; a
//     post-tool-result one is swallowed).
//   - FIRED MEANS ACTED. The dispatcher brackets each invocation with the payload's Revision()
//     and folds a moved revision into the returned Outcome's Edited, so a reaction that reshaped
//     the working value in place is booked whether or not it said so. An
//     inspect-and-do-nothing invocation books nothing.
//   - At post-response the first Retry STOPS THE LEG it fires in, whatever retry budget the loop
//     has left: that is what both chains this replaces did — the Floor guards' first-guard-to-fire
//     and the retired hook runner's ActionRetry short-circuit — and the budget is a LOOP concern
//     deciding only whether the Turn re-streams. The one place the budget reaches the cascade is the handover
//     between the legs: a builtin Retry the loop WILL act on takes the Turn away from the armed
//     leg, while one it cannot act on (budget spent) lets the armed leg run on the untouched
//     response — today's fall-through in loop.go.

// The two action labels the dispatcher itself supplies. firedAction is what a firing is booked
// under at the four Moments with no action vocabulary of their own; actionDefer labels a firing
// that scheduled a correction into the NEXT request. Together with the guard consts
// (floorguards.go) they are one vocabulary, whoever fired, because a reader of a
// ReactionFiredEvent should not have to know which leg the reaction came from to read what it did.
const (
	firedAction = "fired"
	actionDefer = "defer"
)

// errReactionSeam reports a dispatch the ENGINE got wrong — a payload that is not the Moment's
// own, a handler that cannot serve it, or a Moment that is no seam at all. Every one of them is
// an engine bug rather than an extension fault, so it surfaces as the seam's error instead of
// being recovered and blamed on the reaction.
var errReactionSeam = errors.New("apogee: reaction dispatched at the wrong seam")

// armedReaction is one Reaction as the dispatcher holds it: the reaction itself, plus the action
// label a BUILTIN books its firings under. A builtin carries its own guard vocabulary
// ("salvage", "cap", …), which no Outcome can be read back into; an armed Reaction carries none
// and is labelled from the Outcome it returned.
type armedReaction struct {
	spec domain.Reaction
	// action labels a builtin's firing. Empty for an armed Reaction — reactionAction derives
	// the label from what the Outcome says it did.
	action string
}

// revisioned is all the dispatcher needs of a seam payload: the working value's mutation
// counter, read either side of every invocation. All five payloads carry one — the Request, the
// Conversation, the ToolCallEdit, and the two pair types that forward to theirs
// (domain.PostResponseMoment, domain.ToolResultMoment).
type revisioned interface {
	Revision() int
}

// seamPayload is one Moment's payload resolved ONCE per cascade into the two things the
// dispatcher needs of it — the working value it brackets, and the call that hands the payload to
// one handler — plus the one fact only post-response carries. Resolving it once is what lets the
// tool-stage seams build their LoopView a single time for the whole cascade, as the hook runner
// they replace did.
type seamPayload struct {
	work   revisioned
	invoke func(ctx context.Context, h domain.Handler) (domain.Outcome, error)
	// retryable is the loop's remaining retry budget at post-response — true when the loop will
	// re-stream this Turn if a reaction asks it to — and false at every other Moment, none of
	// which can ask.
	retryable bool
}

// fire runs the Reaction cascade for Moment m against payload and reports what the cascade did,
// as one Outcome for the seam to act on. It is the engine's ONLY dispatcher: every seam calls it
// and nothing else fires a reaction.
//
// Two legs run in turn — the builtins, then the armed Reactions. A post-response Retry the loop
// WILL act on ends the cascade where it fires: the Turn is about to re-stream, so the leg below
// never sees the response being replaced. That is the one place the retry budget reaches the
// cascade.
//
// The returned Outcome aggregates the leg: Retry and its Inject come from the reaction that
// asked for the re-stream (the last one to ask, when both legs did), and Edited is set when any
// reaction reshaped the working value. A returned error ends the cascade and comes back with a
// zero Outcome — the seam degrades rather than acting on half a cascade.
func (a *Agent) fire(ctx context.Context, m domain.Moment, payload any) (domain.Outcome, error) {
	turn := a.turns.index

	seam, err := a.seamPayload(turn, m, payload)
	if err != nil {
		return domain.Outcome{}, err
	}

	// Post-response is the ONE seam whose reactions may spawn a subprocess, so the ladder's
	// answer is installed once, here, ahead of the whole cascade — every reaction at this Moment
	// sees the same authorisation, and outside Auto no permit is installed at all, which is the
	// refusal default (confinement-execution-contract §10).
	if m == domain.MomentPostResponse {
		ctx = a.hookExecutionCtx(ctx)
	}

	var result domain.Outcome

	retried, err := a.fireLeg(ctx, turn, m, seam, a.builtins, true, &result)
	if err != nil {
		return domain.Outcome{}, err
	}
	if retried && seam.retryable {
		// The loop will re-stream this Turn with the builtin's correction, so the leg below
		// never sees this response at all — it will see the retried one.
		return result, nil
	}

	if _, err := a.fireLeg(ctx, turn, m, seam, a.armed, false, &result); err != nil {
		return domain.Outcome{}, err
	}
	return result, nil
}

// firePostToolResult fires the post-tool-result Moment for one finished call, letting reactions
// rewrite the result through the shared ToolResultEdit before the model sees it. The originating
// call rides along because the tool name and arguments live there, not on the result.
//
// It is the ONE seam that swallows the cascade's error rather than dispositioning it: a fault there
// has already surfaced as its own event, the tool has already run, and the loop simply commits the
// result as it stands — so there is nothing left for the caller to decide.
func (a *Agent) firePostToolResult(ctx context.Context, call domain.ToolCall, result *domain.ToolResult) {
	_, _ = a.fire(ctx, domain.MomentPostToolResult, domain.ToolResultMoment{
		Call: call,
		Edit: domain.NewToolResultEdit(result),
	})
}

// fireLeg runs one leg of a Moment's cascade and reports whether a post-response Retry stopped
// it. builtin says which leg this is — the engine's own, which Bypass never switches off and
// which books under its own action labels, or the armed one.
func (a *Agent) fireLeg(
	ctx context.Context,
	turn int,
	m domain.Moment,
	seam seamPayload,
	leg []armedReaction,
	builtin bool,
	result *domain.Outcome,
) (bool, error) {
	for _, r := range leg {
		if !slices.Contains(r.spec.On, m) {
			continue
		}
		if !builtin && a.bypassSkips(r.spec) {
			continue
		}

		out, err := a.fireOne(ctx, turn, m, seam, r.spec)
		if err != nil {
			return false, err
		}
		if !acted(out) {
			continue
		}

		a.bookFiring(turn, m, r, out, result)
		if m == domain.MomentPostResponse && out.Retry {
			return true, nil
		}
	}
	return false, nil
}

// fireOne invokes one reaction under the recover boundary, brackets it with the working value's
// Revision counter, and reports the Outcome the booking is decided from. A recovered panic comes
// back as "did nothing, no error", which is what keeps the cascade going.
func (a *Agent) fireOne(
	ctx context.Context,
	turn int,
	m domain.Moment,
	seam seamPayload,
	r domain.Reaction,
) (out domain.Outcome, err error) {
	// Two deferred closures, LIFO: recoverHook's runs first and turns a panic into an ErrorEvent
	// attributed to this reaction plus errHookPanicked in panicErr; this one then reports the
	// recovered reaction as having done nothing, so the cascade goes on (ADR 0076 stage-1 header
	// call). A returned error never touches panicErr and travels out untouched.
	var panicErr error
	defer func() {
		if panicErr != nil {
			out, err = domain.Outcome{}, nil
		}
	}()
	defer a.recoverHook(turn, r.ID, &panicErr)()

	before := seam.work.Revision()
	out, err = seam.invoke(ctx, r.Handler)
	if err != nil {
		return domain.Outcome{}, err
	}
	// A reaction that reshaped the working value in place is a firing whether or not it said so
	// (greenfield §9.2): the bracket is the engine's own probe, and Edited is where it lands.
	if seam.work.Revision() != before {
		out.Edited = true
	}
	return out, nil
}

// recoverHook returns a deferred closure that converts a reaction's panic into an ErrorEvent
// attributed to the firing reaction's id and signals errHookPanicked through errp — the single
// recover-at-extension-boundary primitive the dispatcher installs around every invocation
// (ADR 0007 / ADR 0002).
func (a *Agent) recoverHook(turn int, id string, errp *error) func() {
	return func() {
		if r := recover(); r != nil {
			a.cfg.Events.Emit(domain.ErrorEvent{
				EventBase: a.base(turn),
				Source:    id,
				Err:       fmt.Sprintf("panic: %v", r),
			})
			*errp = errHookPanicked
		}
	}
}

// bookFiring records one ACTED firing: it carries out the reaction's decision that the
// dispatcher owns (a deferred correction goes onto the conversation's queue), folds what the
// seam must know into result, and emits the ReactionFiredEvent.
//
// The emit deliberately sits outside the recover boundary fireOne installs — the shape the hook
// runner had before it. a.cfg.Events reaches the HOST's sink, and a sink that panics is the
// host's fault: recovered here it would surface as an extension panic attributed to a reaction
// whose handler had already returned cleanly.
func (a *Agent) bookFiring(
	turn int,
	m domain.Moment,
	r armedReaction,
	out domain.Outcome,
	result *domain.Outcome,
) {
	if out.Defer != "" {
		a.conv.Defer(out.Defer)
	}
	if out.Retry {
		result.Retry, result.Inject = true, out.Inject
	}
	result.Edited = result.Edited || out.Edited

	label := r.action
	if label == "" {
		label = reactionAction(m, out)
	}
	a.cfg.Events.Emit(domain.ReactionFiredEvent{
		EventBase: a.base(turn),
		Reaction:  r.spec.ID,
		Origin:    r.spec.Origin,
		Moment:    m,
		Action:    label,
		Detail:    out.Detail,
	})
}

// acted is the firing rule (ADR 0076 stage-1 header): a reaction fired when it asked for a
// re-stream, carried a correction, deferred one, or edited the working value — the last term
// covering both a handler that said so and a revision the bracket saw move. Everything else is
// an inspect-and-do-nothing invocation, which is not a firing.
func acted(out domain.Outcome) bool {
	return out.Retry || out.Inject != "" || out.Defer != "" || out.Edited
}

// reactionAction is the label an ARMED reaction's firing is booked under. Post-response is the
// one Moment with an action vocabulary of its own, in the precedence the header ratifies; the
// other four have none, so a firing there is simply "fired". A builtin never reaches here — it
// carries its own label.
func reactionAction(m domain.Moment, out domain.Outcome) string {
	if m != domain.MomentPostResponse {
		return firedAction
	}
	switch {
	case out.Retry:
		return guardActionRetry
	case out.Defer != "":
		return actionDefer
	default:
		return guardActionIntercept
	}
}

// bypassSkips reports whether Bypass switches this armed reaction off (ADR 0076 D9): advise and
// the two shape classes go quiet, observe and gate stay on. It reads the LIVE flag, so a
// mid-session SetBypass lands at the very next Moment.
func (a *Agent) bypassSkips(r domain.Reaction) bool {
	if !a.bypassEnabled() {
		return false
	}
	switch r.Class {
	case domain.ClassAdvise, domain.ClassShapeView, domain.ClassShapeWork:
		return true
	default:
		return false
	}
}

// seamPayload resolves the Moment's payload into what the cascade reads it through. The five
// cases are the whole of what differs between the seams — which working value is bracketed,
// which handler type is called and with what, and which builds a LoopView — so every other part
// of the dispatcher is seam-blind.
//
// A payload or handler that does not match the Moment is an engine bug and is refused here
// rather than recovered, because attributing it to the reaction would blame the wrong party.
func (a *Agent) seamPayload(turn int, m domain.Moment, payload any) (seamPayload, error) {
	switch m {
	case domain.MomentPreRequest:
		req, ok := payload.(*domain.Request)
		if !ok {
			return seamPayload{}, wrongPayload(m, payload)
		}
		return seamPayload{
			work: req,
			invoke: func(ctx context.Context, h domain.Handler) (domain.Outcome, error) {
				fn, ok := h.(domain.PreRequestFunc)
				if !ok {
					return domain.Outcome{}, wrongHandler(m, h)
				}
				return fn(ctx, req)
			},
		}, nil

	case domain.MomentPostResponse:
		p, ok := payload.(domain.PostResponseMoment)
		if !ok {
			return seamPayload{}, wrongPayload(m, payload)
		}
		return seamPayload{
			work:      p,
			retryable: p.Retryable,
			invoke: func(ctx context.Context, h domain.Handler) (domain.Outcome, error) {
				fn, ok := h.(domain.PostResponseFunc)
				if !ok {
					return domain.Outcome{}, wrongHandler(m, h)
				}
				return fn(ctx, p.Resp)
			},
		}, nil

	case domain.MomentPreToolExec:
		edit, ok := payload.(*domain.ToolCallEdit)
		if !ok {
			return seamPayload{}, wrongPayload(m, payload)
		}
		view := a.loopView(turn)
		return seamPayload{
			work: edit,
			invoke: func(ctx context.Context, h domain.Handler) (domain.Outcome, error) {
				fn, ok := h.(domain.PreToolExecFunc)
				if !ok {
					return domain.Outcome{}, wrongHandler(m, h)
				}
				return fn(ctx, view, edit)
			},
		}, nil

	case domain.MomentPostToolResult:
		p, ok := payload.(domain.ToolResultMoment)
		if !ok {
			return seamPayload{}, wrongPayload(m, payload)
		}
		view := a.loopView(turn)
		return seamPayload{
			work: p,
			invoke: func(ctx context.Context, h domain.Handler) (domain.Outcome, error) {
				fn, ok := h.(domain.PostToolResultFunc)
				if !ok {
					return domain.Outcome{}, wrongHandler(m, h)
				}
				return fn(ctx, view, p.Call, p.Edit)
			},
		}, nil

	case domain.MomentHistoryRewrite:
		conv, ok := payload.(*domain.Conversation)
		if !ok {
			return seamPayload{}, wrongPayload(m, payload)
		}
		return seamPayload{
			work: conv,
			invoke: func(ctx context.Context, h domain.Handler) (domain.Outcome, error) {
				fn, ok := h.(domain.HistoryRewriteFunc)
				if !ok {
					return domain.Outcome{}, wrongHandler(m, h)
				}
				return fn(ctx, conv)
			},
		}, nil
	}
	return seamPayload{}, fmt.Errorf("%w: %q is not a seam Moment", errReactionSeam, m)
}

// wrongPayload and wrongHandler name the two ways a dispatch can be mis-wired, each naming the
// offending type so the engine bug is readable from the event alone.
func wrongPayload(m domain.Moment, payload any) error {
	return fmt.Errorf("%w: %q cannot carry a %T payload", errReactionSeam, m, payload)
}

func wrongHandler(m domain.Moment, h domain.Handler) error {
	return fmt.Errorf("%w: %q cannot call a %T handler", errReactionSeam, m, h)
}

// armReactions validates the Reactions a host armed on Config.Reactions and returns them in the
// dispatcher's own shape. Every entry must be well formed (Reaction.Validate) and answer to an
// ID no builtin and no earlier entry already holds: the ReactionFiredEvent, the identity
// projector and the provenance ledger all key on the ID, so two reactions sharing one name make
// every attribution ambiguous. Either failure fails construction — an invalid or shadowed
// reaction never silently does nothing.
func armReactions(builtins []armedReaction, reactions []domain.Reaction) ([]armedReaction, error) {
	if len(reactions) == 0 {
		return nil, nil
	}

	taken := make(map[string]bool, len(builtins)+len(reactions))
	for _, b := range builtins {
		taken[b.spec.ID] = true
	}

	armed := make([]armedReaction, 0, len(reactions))
	for _, r := range reactions {
		if err := r.Validate(); err != nil {
			return nil, err
		}
		if taken[r.ID] {
			return nil, fmt.Errorf("%w %q: that ID is already armed", domain.ErrInvalidReaction, r.ID)
		}
		taken[r.ID] = true
		armed = append(armed, armedReaction{spec: r})
	}
	return armed, nil
}

// inheritedReactions is the Reactions a sub-agent inherits from this set: all of them, minus the
// ones that opted out. Inheritance is the DEFAULT — a zero-value Reaction fires in every child,
// exactly as an armed Mechanism's membership was inherited before ADR 0076 (subagent.go) —
// and Reaction.TopLevelOnly is the opt-out, for a reaction that would be wrong or wasteful at
// depth.
func inheritedReactions(reactions []domain.Reaction) []domain.Reaction {
	kept := make([]domain.Reaction, 0, len(reactions))
	for _, r := range reactions {
		if r.TopLevelOnly {
			continue
		}
		kept = append(kept, r)
	}
	if len(kept) == 0 {
		return nil
	}
	return kept
}
