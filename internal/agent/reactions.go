package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/tools"
)

// The Reaction dispatcher (ADR 0076 D1; docs/design/reaction-core-greenfield.md §9.2).
//
// One ladder runs at every seam Moment, and fireCascade below is the whole of it: the engine's own
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
//     Floor guards never consult it — a Floor guard cannot regress Bypass, so it is never
//     withdrawn — and observe and gate reactions stay on because neither can make a model do
//     worse. The one builtin Bypass does reach is a builtin of class ADVISE — the context-fill
//     notice (ADR 0077) — which is skipped like any armed advise reaction. A skipped reaction is
//     silent: no event, no ledger entry, nothing to read back.
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
//     and the retired hook runner's retry short-circuit — and the budget is a LOOP concern
//     deciding only whether the Turn re-streams. The one place the budget reaches the cascade is the handover
//     between the legs: a builtin Retry the loop WILL act on takes the Turn away from the armed
//     leg, while one it cannot act on (budget spent) lets the armed leg run on the untouched
//     response — today's fall-through in loop.go.

// The three action labels the dispatcher itself supplies. firedAction is what a firing is booked
// under at the Moments with no action vocabulary of their own; actionDefer labels a firing
// that scheduled a correction into the NEXT request; actionAdvise labels one that handed the model
// a fenced trailer on the closing tool result. Together with the guard consts
// (floorguards.go) they are one vocabulary, whoever fired, because a reader of a
// ReactionFiredEvent should not have to know which leg the reaction came from to read what it did.
const (
	firedAction  = "fired"
	actionDefer  = "defer"
	actionAdvise = "advise"
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

// advice is one advise Reaction's contribution at the tool-result Moment: the ledger row the
// injection is attributed through and the capped text that lands inside its fence. The two travel
// together because neither is readable alone — a span with no text names nothing, and text with no
// span cannot be fenced, since every word of the fence header is derived from the span
// (domain.RenderAdvice). The dispatcher collects them in ladder order; the seam that commits the
// tool result renders them onto it (appendToolResult, dispatch.go).
type advice struct {
	span domain.AdviceSpan
	text string
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
	work revisioned
	// invoke calls one reaction at this Moment. It takes the whole REACTION rather than its
	// Handler alone because a handler is not always the whole of what the call needs: the sync
	// lane's out-of-process routes read the reaction's class, id, `on:` list and deadline to
	// decide what document they hand their command and what deadline it dies at.
	invoke func(ctx context.Context, r domain.Reaction) (domain.Outcome, error)
	// retryable is the loop's remaining retry budget at post-response — true when the loop will
	// re-stream this Turn if a reaction asks it to — and false at every other Moment, none of
	// which can ask.
	retryable bool
}

// fireCascade runs the Reaction cascade for Moment m against payload and reports what the cascade
// did — one Outcome for the seam to act on, plus the advise slot below. It is the engine's ONLY
// dispatcher: every seam reaches it, through fire or firePostToolResult, and nothing else fires a
// reaction.
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
//
// Every pass that reaches the ladder closes with one SeamClosedEvent, which is what the five
// seam-closing notices are built on. The one dispatch that emits nothing is the engine bug
// seamPayload refuses below: no ladder ran, the Moment may be no seam at all, and the payload is
// by definition not the one the event promises to carry.
//
// The second return is the ADVISE SLOT (ADR 0076 D6): the spans the cascade's advise reactions
// contributed, in ladder order, for the seam to render onto the closing tool result. It is empty
// at every Moment but post-tool-result, and empty there too under Bypass, which switches advise
// off before a handler is ever called.
func (a *Agent) fireCascade(ctx context.Context, m domain.Moment, payload any) (domain.Outcome, []advice, error) {
	turn := a.turns.index

	seam, err := a.seamPayload(turn, m, payload)
	if err != nil {
		return domain.Outcome{}, nil, err
	}

	// No Moment's cascade installs a domain.SubprocessPermit: the ctx handed to every handler here
	// carries none, which is the refusal default (confinement-execution-contract §10.2). The one
	// permit the engine mints is the sync lane's, minted per spawn inside runSyncArgv rather than
	// ahead of a cascade (§10.4).
	var result domain.Outcome
	var collected []advice

	// The seam closes exactly ONCE per fire call, and it closes whatever the cascade did: the
	// deferred emit below covers the ordinary return, the retry hand-back, and an error that
	// ended the cascade half way — and it fires just as readily under Bypass or with nothing
	// armed at all, because "the seam passed and nothing happened" is the fact an observer most
	// often wants (ADR 0076 A5). Once per CALL is why a retried post-response Turn closes once
	// per attempt rather than once per Turn. fired is the ids bookFiring booked, in firing
	// order, and payload is handed on untouched — the event carries the working value by
	// reference, read-only and only for the duration of Emit.
	var fired []string
	defer func() {
		a.cfg.Events.Emit(domain.SeamClosedEvent{
			EventBase: a.base(turn),
			Seam:      m,
			Fired:     fired,
			Value:     payload,
		})
	}()

	retried, err := a.fireLeg(ctx, turn, m, seam, a.builtinLadder(), true, &result, &fired, &collected)
	if err != nil {
		return domain.Outcome{}, nil, err
	}
	if retried && seam.retryable {
		// The loop will re-stream this Turn with the builtin's correction, so the leg below
		// never sees this response at all — it will see the retried one.
		return result, collected, nil
	}

	if _, err := a.fireLeg(ctx, turn, m, seam, a.armedLadder(), false, &result, &fired, &collected); err != nil {
		return domain.Outcome{}, nil, err
	}
	return result, collected, nil
}

// fire runs the cascade for a seam that carries no advise slot and reports the Outcome alone.
// Four of the five Moments are such seams: only the closing tool result has somewhere to put a
// trailer, so only firePostToolResult calls fireCascade directly.
func (a *Agent) fire(ctx context.Context, m domain.Moment, payload any) (domain.Outcome, error) {
	out, _, err := a.fireCascade(ctx, m, payload)
	return out, err
}

// armedLadder snapshots the ARMED leg for one cascade or one gate fold: the construction-time
// Reactions (Config.Reactions — the bench arm and the embedder's own set, fixed at New) followed
// by the live Generation's sync lane (the user's `reactions:` file, swapped in by SetReactions).
// The order is the ladder's: the host's own set is asked first, exactly as it was before the sync
// lane existed, and the user's file fires after it.
//
// The two lists are held apart rather than merged at swap time because they have different
// lifetimes — one is validated once at construction, the other is replaced whole whenever the
// settings surface hands over a new Generation — and are joined HERE, once per cascade, so a swap
// landing mid-fire cannot lengthen or shorten a ladder that is already being walked. The sync
// entries carry no builtin action label: they are armed Reactions, labelled from the Outcome they
// return like every other one (armedReaction).
//
// The common case allocates nothing: with no sync lane armed the construction-time slice is
// returned as it stands, which is what every bench arm and every host that never calls
// SetReactions gets.
func (a *Agent) armedLadder() []armedReaction {
	sync := a.syncReactions()
	if len(sync) == 0 {
		return a.armed
	}
	ladder := make([]armedReaction, 0, len(a.armed)+len(sync))
	ladder = append(ladder, a.armed...)
	for _, r := range sync {
		ladder = append(ladder, armedReaction{spec: r})
	}
	return ladder
}

// syncReactions reads the live sync lane under the lock, so the worker goroutine's per-cascade read
// is race-free against a settings surface calling SetReactions. It is the ONE read seam for the
// lane, the sibling of bypassEnabled and builtinLadder; the slice itself is never mutated in
// place, so the value returned stays valid for the whole cascade that read it.
func (a *Agent) syncReactions() []domain.Reaction {
	a.genMu.RLock()
	defer a.genMu.RUnlock()
	return a.gen.Sync
}

// builtinLadder snapshots the builtin leg for one cascade. The slice is the enable set — only
// the guards the live Generation's Floor leaves on — and a Floor swap REPLACES it wholesale
// rather than mutating it in place (SetReactions), so one read under genMu gives the whole
// cascade a consistent ladder even if the settings surface swaps mid-fire.
func (a *Agent) builtinLadder() []armedReaction {
	a.genMu.RLock()
	defer a.genMu.RUnlock()
	return a.builtins
}

// firePostToolResult fires the post-tool-result Moment for one finished call, letting reactions
// rewrite the result through the shared ToolResultEdit before the model sees it. The originating
// call rides along because the tool name and arguments live there, not on the result.
//
// It is the ONE seam that swallows the cascade's error rather than dispositioning it: a fault there
// has already surfaced as its own event, the tool has already run, and the loop simply commits the
// result as it stands — so there is nothing left for the caller to decide.
//
// It returns the advise slot the cascade filled, in ladder order, for the caller to hand to
// appendToolResult: the trailer is rendered onto the committed MESSAGE rather than onto the
// ToolResult, so what an observer, the audit record and the session record carry is the tool's own
// output and nothing else.
func (a *Agent) firePostToolResult(ctx context.Context, call domain.ToolCall, result *domain.ToolResult) []advice {
	_, advised, _ := a.fireCascade(ctx, domain.MomentPostToolResult, domain.ToolResultMoment{
		Call: call,
		Edit: domain.NewToolResultEdit(result),
	})
	return advised
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
	fired *[]string,
	advised *[]advice,
) (bool, error) {
	for _, r := range leg {
		// A GATE never fires here. Its whole cell is the Approver stage applyGates runs after
		// resolve() (gate.go), which is the only place a verdict can be folded into — and the
		// only place that can be, since a gate's command answers with a decision this cascade has
		// no field to carry. Routing one through here would assert a PreToolExecFunc against that
		// command and fail EVERY tool call with "pre-tool-exec reaction failed"; a Go gate
		// handler, which would pass the assertion, would have its verdict booked twice.
		if r.spec.Class == domain.ClassGate {
			continue
		}
		if !subscribes(r.spec, m) {
			continue
		}
		// Bypass reaches every armed reaction of a skippable class, and of the builtins only one
		// of class advise (the context-fill notice, ADR 0077): the shape-view Floor guards keep
		// firing under it exactly as they always have.
		if a.bypassSkips(r.spec) && (!builtin || r.spec.Class == domain.ClassAdvise) {
			continue
		}

		out, err := a.fireOne(ctx, turn, m, seam, r.spec)
		if err != nil {
			return false, err
		}
		if !acted(out) {
			continue
		}

		// An advise injection is collected before it is booked, so the firing's Detail is the
		// CAPPED text the model will read rather than whatever the handler returned — what
		// apogee-sim hashes to attribute an effect is then the same string the fence carries.
		// A handler that set its own Detail keeps it: the context-fill notice books the rung
		// and percent it fired at (ADR 0077), the fact a bench keys on, and every user advise
		// entry leaves the field empty.
		if adv, ok := adviceOf(turn, m, r.spec, out); ok {
			*advised = append(*advised, adv)
			if out.Detail == "" {
				out.Detail = adv.text
			}
		}

		a.bookFiring(turn, m, r, out, result, fired)
		if m == domain.MomentPostResponse && out.Retry {
			return true, nil
		}
	}
	return false, nil
}

// subscribes reports whether reaction r fires at Moment m. The ordinary answer is its own `on:`
// list, and the one exception is the ADVISE lane's file-changed narrowing: file-changed is no seam
// of its own but post-tool-result with two further conditions on the call, so a reaction that
// listed it runs at post-tool-result and is narrowed there, where the tool and its result are in
// hand (hearsCall, on the seam's invoke path — whatever the handler's kind, so the widening here
// and the narrowing there cover the same set). On the OBSERVE lane file-changed stays the notice
// Moment it always was — the Runner matches it off the Event stream and never reaches this
// cascade — which is why the widening is scoped to class advise rather than applied to the Moment.
func subscribes(r domain.Reaction, m domain.Moment) bool {
	if slices.Contains(r.On, m) {
		return true
	}
	return m == domain.MomentPostToolResult && r.Class == domain.ClassAdvise &&
		slices.Contains(r.On, domain.MomentFileChanged)
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
	out, err = seam.invoke(ctx, r)
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
	fired *[]string,
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
		label = reactionAction(m, r.spec, out)
	}
	*fired = append(*fired, r.spec.ID)
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
//
// "Carried a correction" is what makes an advise reaction a firing at the tool-result seam: its
// Inject is the trailer text and no Retry accompanies it there, so the Inject term alone books it.
//
// "Gave a verdict" is the gate cell's term, and the gate STAGE is its only reader (gate.go's
// callGateFunc): an Outcome whose Gate.Verdict is the only thing set carries no correction, no
// deferral and no edit, so without this term a Go gate handler's deny would read as an
// inspect-and-do-nothing invocation and be dropped. The zero GateDecision stays silence.
func acted(out domain.Outcome) bool {
	return out.Retry || out.Inject != "" || out.Defer != "" || out.Edited || out.Gate.Verdict != ""
}

// isAdvice reports whether this ACTED Outcome is an advise injection — the one firing that both
// books under actionAdvise and contributes a span to the advise slot. All three terms are
// necessary: the class says the reaction is allowed to speak to the model, the Moment says there
// is a tool result to speak on, and a non-empty Inject is the text itself. Of the builtins only
// the context-fill notice satisfies it — the one engine reaction of class advise (ADR 0077); the
// seven Floor guards are shape-view and contribute no advice.
func isAdvice(m domain.Moment, r domain.Reaction, out domain.Outcome) bool {
	return m == domain.MomentPostToolResult && r.Class == domain.ClassAdvise && out.Inject != ""
}

// adviceOf turns one advise firing into the slot entry the tool-result seam renders, and reports
// whether the firing was one at all. Every field of the ledger row comes from the REACTION and the
// cascade — id, origin, Moment, Turn — and none from the handler, which is what makes the fence
// header unforgeable: a handler printing its own "[advice — reaction …]" line lands inside the
// fence rather than beside it. The text is capped here (domain.CapAdvice), once, so the cap cannot
// be bypassed by a route that renders a span itself. Offset is left zero: the message stamps it
// when the fence is appended (domain.Message.WithAdvice).
func adviceOf(turn int, m domain.Moment, r domain.Reaction, out domain.Outcome) (advice, bool) {
	if !isAdvice(m, r, out) {
		return advice{}, false
	}
	return advice{
		span: domain.AdviceSpan{
			Reaction: r.ID,
			Origin:   r.Origin,
			Moment:   m,
			Turn:     turn,
		},
		text: domain.CapAdvice(out.Inject),
	}, true
}

// adviseUser is the USER half of the advise slot: one `advise:` entry's command runs out of
// process, or its webhook is POSTed to, while the loop waits, and what it answered becomes the
// trailer the model reads (ADR 0076 D2, D6). It is called from the post-tool-result seam alone —
// the only Moment with a closing tool result to fence a trailer onto — and dispatches on the
// handler's KIND: a command's standard output and a webhook's reply body are the same text to
// everything below.
//
// It is FAIL-OPEN in both directions it decides (D7), and each of them costs the Turn nothing: a
// command that failed, timed out or was refused a permit — a webhook whose request failed, timed
// out, was answered with a non-2xx or named an unset `headers-env:` variable — contributes nothing
// and is reported once, through the sync lane's own reporter and one "failed" firing; a handler
// that answered nothing contributes nothing, so an entry that only sometimes has something to say
// is silent the rest of the time rather than injecting an empty fence. The third silence — a
// file-changed entry on a call that changed no file — is decided BEFORE this route is reached
// (hearsCall), because it is the whole advise class's rule and not the handler's.
//
// The answer is REDACTED before it is anything else. It is the one place a configured secret's
// value can re-enter apogee from a process the scrub kept it out of, or from a server the request
// carried it to, and the redaction has to land before the text is capped, fenced, booked as a
// firing's Detail, or written to a transcript. The CAP is not applied here: every advise route
// meets it once, downstream, where the span is collected (adviceOf), so no route can render a span
// that skipped it. The webhook's read is bounded at one byte past that cap (adviseReplyCap) — enough
// for the downstream cap to see the reply ran over and mark the truncation, and no more, so a
// server that streams without end cannot hold the Turn's memory.
func (a *Agent) adviseUser(
	ctx context.Context,
	turn int,
	r domain.Reaction,
	doc domain.SeamPayload,
) (domain.Outcome, error) {
	var answer string
	var err error
	switch r.Handler.(type) {
	case domain.ArgvHandler:
		answer, err = a.runSyncArgv(ctx, turn, r, doc)
	case domain.WebhookHandler:
		answer, err = a.runSyncWebhook(ctx, turn, r, doc, adviseReplyCap)
	default:
		return domain.Outcome{}, wrongHandler(domain.MomentPostToolResult, r.Handler)
	}
	if err != nil {
		a.reportReaction(turn, r.ID, doc.Event, err)
		return domain.Outcome{}, nil
	}

	text := tools.RedactSecrets(answer, a.cfg.SecretEnvVars)
	if text == "" {
		return domain.Outcome{}, nil
	}
	return domain.Outcome{Inject: text}, nil
}

// adviseReplyCap bounds the bytes read off an advise webhook's reply: the advice cap plus one, so
// the downstream cap (adviceOf → domain.CapAdvice) still sees a reply that ran over and appends the
// truncation marker, while nothing past that byte is ever held.
const adviseReplyCap = domain.AdviceCap + 1

// hearsCall is the advise lane's file-changed NARROWING — the half that pays for subscribes'
// widening. It reports whether advise reaction r, which subscribes told the cascade fires at
// post-tool-result, hears THIS finished call, and the file the call changed when that is what
// admitted it. A reaction that listed post-tool-result hears every call, because that seam closes
// on every call; one that listed file-changed hears a call that was a successful workspace write,
// with its path; one that listed file-changed ALONE hears nothing else, which is what the
// narrowing buys a user who wants to react to edits and not to reads.
//
// It sits on the seam's invoke path rather than in any one handler's route so that the set the
// widening admits and the set the narrowing keeps are the same set for every handler kind — a Go
// advise handler on file-changed is narrowed exactly as the user's command is.
//
// The path is tools.WorkspaceWriteTarget's — the same resolution the blast-radius ladder judged
// the call by and the same one the observe lane's file-changed firing carries (the
// ToolResultEvent's WriteTarget, which internal/reactions reads off the Event), so a user watching
// one file through both lanes is told one name. It is
// deliberately neither a.resolvedPath, which is empty for an ordinary in-workspace write because
// it is the DISCLOSURE twin and speaks only when the resolution differs from the argument, nor
// classifyWriteTarget's escape target, which is empty inside the fence.
func (a *Agent) hearsCall(r domain.Reaction, p domain.ToolResultMoment) (path string, hears bool) {
	if !slices.Contains(r.On, domain.MomentFileChanged) {
		return "", true
	}
	if path, ok := a.writtenPath(p.Call); ok && !p.Edit.IsError() {
		return path, true
	}
	// The call changed no file. An entry that also listed post-tool-result still hears the seam
	// close; one that listed file-changed alone hears nothing.
	return "", slices.Contains(r.On, domain.MomentPostToolResult)
}

// adviseDocument builds the stdin document one advise command receives for a finished tool call
// it hears. The executor stamps the identity block over what is returned here (runSyncArgv), so
// what this function settles is the MOMENT's half alone: which Moment fired, on which tool, over
// which arguments and result, and — for file-changed — which file.
//
// The Moment reported is the one the ENTRY subscribed to, not the seam it ran at: path is the file
// hearsCall admitted the call on, and when it is set the document says `file-changed` and names
// it. An entry that listed post-tool-result sees that Moment and no path.
func adviseDocument(p domain.ToolResultMoment, path string) domain.SeamPayload {
	doc := domain.SeamPayload{
		Event:     domain.MomentPostToolResult,
		Tool:      p.Call.Tool,
		Arguments: append(json.RawMessage(nil), p.Call.Arguments...),
		Result:    &domain.SeamResult{Content: p.Edit.Content(), IsError: p.Edit.IsError()},
	}
	if path != "" {
		doc.Event, doc.Path = domain.MomentFileChanged, path
	}
	return doc
}

// writtenPath is the absolute, symlink-resolved path a workspace-scoped write tool's call lands
// on, and false for every call that writes nothing inspectable — an unknown tool included, exactly
// as such a tool classifies as nothing everywhere else the registry is consulted this late.
func (a *Agent) writtenPath(call domain.ToolCall) (string, bool) {
	tool, ok := a.lookupTool(call.Tool)
	if !ok {
		return "", false
	}
	return tools.WorkspaceWriteTarget(tool, call)
}

// reactionAction is the label an ARMED reaction's firing is booked under. Post-response is the
// one Moment with an action vocabulary of its own, in the precedence the header ratifies; an
// advise injection at the tool-result seam is the other named action; the rest have none, so a
// firing there is simply "fired". A builtin never reaches here — it carries its own label.
func reactionAction(m domain.Moment, r domain.Reaction, out domain.Outcome) string {
	if isAdvice(m, r, out) {
		return actionAdvise
	}
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

// bypassSkips reports whether Bypass switches this reaction off by CLASS (ADR 0076 D9): advise
// and the two shape classes go quiet, observe and gate stay on. It reads the LIVE flag, so a
// mid-session SetReactions lands at the very next Moment. The answer is applied to every ARMED
// reaction; of the builtins it reaches only one of class advise — the context-fill notice
// (ADR 0077) — because a shape-view Floor guard is the floor Bypass exists to keep (fireLeg).
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
			invoke: func(ctx context.Context, r domain.Reaction) (domain.Outcome, error) {
				fn, ok := r.Handler.(domain.PreRequestFunc)
				if !ok {
					return domain.Outcome{}, wrongHandler(m, r.Handler)
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
			invoke: func(ctx context.Context, r domain.Reaction) (domain.Outcome, error) {
				fn, ok := r.Handler.(domain.PostResponseFunc)
				if !ok {
					return domain.Outcome{}, wrongHandler(m, r.Handler)
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
			invoke: func(ctx context.Context, r domain.Reaction) (domain.Outcome, error) {
				fn, ok := r.Handler.(domain.PreToolExecFunc)
				if !ok {
					return domain.Outcome{}, wrongHandler(m, r.Handler)
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
			invoke: func(ctx context.Context, r domain.Reaction) (domain.Outcome, error) {
				// The advise lane is NARROWED here, before the handler kind is looked at,
				// because this closure is the one path every advise reaction at this seam
				// crosses: subscribes widened a file-changed entry onto this Moment for the
				// whole class, so the narrowing that pays for it (hearsCall) has to hold for
				// the whole class too — a Go advise handler and the user's command alike.
				// The user's advise cell is then the one route that leaves the process: its
				// handler is a command or a webhook, not a Go func, so BOTH out-of-process
				// kinds are dispatched here, before the Go handler assertion, rather than
				// failing it — a kind left to fall through would lose every advise trailer
				// for the call silently (fireCascade returns no advice, firePostToolResult
				// drops the error).
				if r.Class == domain.ClassAdvise {
					path, hears := a.hearsCall(r, p)
					if !hears {
						return domain.Outcome{}, nil
					}
					switch r.Handler.(type) {
					case domain.ArgvHandler, domain.WebhookHandler:
						return a.adviseUser(ctx, turn, r, adviseDocument(p, path))
					}
				}
				fn, ok := r.Handler.(domain.PostToolResultFunc)
				if !ok {
					return domain.Outcome{}, wrongHandler(m, r.Handler)
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
			invoke: func(ctx context.Context, r domain.Reaction) (domain.Outcome, error) {
				fn, ok := r.Handler.(domain.HistoryRewriteFunc)
				if !ok {
					return domain.Outcome{}, wrongHandler(m, r.Handler)
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

// reservedIDs is the set of ids the engine's own builtins answer to, which no armed Reaction may
// take on EITHER route — Config.Reactions at construction (armReactions) or the live sync lane
// (Agent.SetReactions): the ReactionFiredEvent, the identity projector and the provenance ledger
// all key on the ID, so a reaction sharing a builtin's name makes every attribution ambiguous.
//
// The set is guardIDs — ALL seven guard keys, not the enable set the ladder currently holds
// (floorguards.go) — plus the context-fill notice's id (fillnotice.go), on the same terms. The
// step-budget notice is not in it: a structural engine note (stepnotice.go) has no Reaction id to
// collide with. A builtin the user switched off still owns its name: arming an entry under it would be
// answered by the builtin again the moment its switch moves back. The map is fresh per call, so a
// caller may grow it with the ids it arms.
func reservedIDs() map[string]bool {
	taken := make(map[string]bool, len(guardIDs)+1)
	for _, id := range guardIDs {
		taken[id] = true
	}
	taken[contextFillNoticeID] = true
	return taken
}

// refuseReservedIDs reports the first entry whose ID a builtin already holds (reservedIDs), in the
// sentence armReactions refuses the same collision with. It is the live sync lane's share of the
// arming step: duplicates WITHIN the lane are domain.Generation.Validate's, so this asks the one
// question only the engine can answer — which names its own builtins have taken.
func refuseReservedIDs(reactions []domain.Reaction) error {
	reserved := reservedIDs()
	for _, r := range reactions {
		if reserved[r.ID] {
			return fmt.Errorf("%w %q: that ID is already armed", domain.ErrInvalidReaction, r.ID)
		}
	}
	return nil
}

// armReactions validates the Reactions a host armed on Config.Reactions and returns them in the
// dispatcher's own shape. Every entry must be well formed (Reaction.Validate) and answer to an
// ID no builtin (reservedIDs) and no earlier entry already holds: the ReactionFiredEvent, the
// identity projector and the provenance ledger all key on the ID, so two reactions sharing one
// name make every attribution ambiguous. Either failure fails construction — an invalid or
// shadowed reaction never silently does nothing.
//
// This is the ONE place Config.Reactions is validated: the route has no config layer in front of
// it — the bench and an embedder hand Reaction values straight to the constructor — so the
// per-entry rules are answered here, once, and the dispatcher below takes the list as given.
func armReactions(reactions []domain.Reaction) ([]armedReaction, error) {
	if len(reactions) == 0 {
		return nil, nil
	}

	taken := reservedIDs()
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

// inheritedReactions is the Reactions a sub-agent inherits from the parent's armed routes: all of
// them, minus the ones that opted out. Inheritance is the DEFAULT — a zero-value Reaction fires in
// every child, exactly as an armed Mechanism's membership was inherited before ADR 0076
// (subagent.go) — and Reaction.TopLevelOnly is the opt-out, for a reaction that would be wrong or
// wasteful at depth.
//
// It takes the routes as SEPARATE lists — the parent's construction-time Config.Reactions and its
// live Generation.Sync — and joins them in ladder order into one fresh slice, so the child fires
// them in the order the parent does and neither caller's backing array can be written through.
func inheritedReactions(lists ...[]domain.Reaction) []domain.Reaction {
	total := 0
	for _, list := range lists {
		total += len(list)
	}
	kept := make([]domain.Reaction, 0, total)
	for _, list := range lists {
		for _, r := range list {
			if r.TopLevelOnly {
				continue
			}
			kept = append(kept, r)
		}
	}
	if len(kept) == 0 {
		return nil
	}
	return kept
}
