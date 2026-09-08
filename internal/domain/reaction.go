package domain

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ----------------------------------------------------------------------------
// The Reaction core (ADR 0076 D1/D2; docs/design/reaction-core-greenfield.md §9.2)
// ----------------------------------------------------------------------------

// Moment is a point the loop passes, on which a Reaction may fire (CONTEXT: Moment).
// Two kinds share the one vocabulary:
//
//   - a SEAM Moment is in-loop and synchronous, its payload an editable working value
//     the firing reaction may reshape before the loop moves on;
//   - a NOTICE Moment is post-hoc, its payload sealed — it reports something that has
//     already happened, so a reaction reading it can change nothing about it.
//
// The string is the spelling a user writes in configuration and the value that reaches an
// observer on a ReactionFiredEvent, so it is a stable contract: renaming one breaks every
// configuration in the wild.
type Moment string

// The five seam Moments — the in-loop points, in the order a Turn passes them. The values
// are the HookPoint spellings this vocabulary replaces (mechanism.go), unchanged.
const (
	MomentPreRequest     Moment = "pre-request"      // shape the outgoing request
	MomentPostResponse   Moment = "post-response"    // inspect the response, choose an action
	MomentPreToolExec    Moment = "pre-tool-exec"    // between decision-to-run and execution
	MomentPostToolResult Moment = "post-tool-result" // act on a result before the model sees it
	MomentHistoryRewrite Moment = "history-rewrite"  // edit conversation state
)

// The five notice Moments — the post-hoc points. The values are the hooks.Event spellings
// (internal/hooks), unchanged: they are what a configured `events:` list already names.
const (
	MomentExchangeFinished Moment = "exchange-finished" // a Depth-0 Turn closed its Exchange
	MomentTurnFinished     Moment = "turn-finished"     // a Depth-0 Turn boundary, whatever its status
	MomentFileChanged      Moment = "file-changed"      // a workspace-scoped write tool succeeded
	MomentApprovalWaiting  Moment = "approval-waiting"  // an Approval was raised, before its decision
	MomentError            Moment = "error"             // a localised, recovered engine fault
)

// allSeams is the seam vocabulary in loop order; allNotices is the notice vocabulary in the
// order internal/hooks reports it.
var (
	allSeams = []Moment{
		MomentPreRequest,
		MomentPostResponse,
		MomentPreToolExec,
		MomentPostToolResult,
		MomentHistoryRewrite,
	}
	allNotices = []Moment{
		MomentExchangeFinished,
		MomentTurnFinished,
		MomentFileChanged,
		MomentApprovalWaiting,
		MomentError,
	}
)

// IsSeam reports whether the Moment is one of the five in-loop seams — the kind whose payload
// is an editable working value and whose reactions run on the loop goroutine. Everything else,
// including a spelling outside the vocabulary, is not a seam.
func (m Moment) IsSeam() bool {
	for _, s := range allSeams {
		if m == s {
			return true
		}
	}
	return false
}

// Seams returns the five seam Moments in loop order. The slice is a fresh copy, so a caller
// listing them for a help text or a validation message cannot disturb the vocabulary.
func Seams() []Moment { return append([]Moment(nil), allSeams...) }

// Notices returns the five notice Moments in their documented order, as a fresh copy.
func Notices() []Moment { return append([]Moment(nil), allNotices...) }

// Origin is who a Reaction belongs to — one axis of the Reaction surface matrix (CONTEXT:
// Reaction surface; ADR 0076 D2). It decides what the reaction may do, together with its Class,
// and whether Bypass switches it off.
type Origin string

const (
	// OriginEngine is apogee's own: a builtin such as a Floor guard, or a Go reaction the bench
	// arms in-process through the facade.
	OriginEngine Origin = "engine"
	// OriginUser is the user's: an entry they configured.
	OriginUser Origin = "user"
)

// Class is what a Reaction may do — the other axis of the Reaction surface matrix. One Reaction
// takes exactly one cell of origin × class.
type Class string

const (
	// ClassObserve returns nothing; nothing the model sees changes.
	ClassObserve Class = "observe"
	// ClassAdvise returns text the model sees — fenced, capped and fail-open.
	ClassAdvise Class = "advise"
	// ClassGate returns allow / deny / ask at pre-tool-exec, as a stage of the Approver.
	ClassGate Class = "gate"
	// ClassShapeView edits what the model SEES. The seven Floor guards are the engine builtins here.
	ClassShapeView Class = "shape-view"
	// ClassShapeWork edits what the model DOES — tool-call arguments at pre-tool-exec. Engine only:
	// once a later reaction can mutate arguments an earlier gate approved, no gate is sound.
	ClassShapeWork Class = "shape-work"
)

// reactionCells is the origin × class policy matrix (ADR 0076 D2): engine takes every class,
// user takes observe, advise and gate. The user cells for shape are reserved (shape-view) and
// refused outright (shape-work), so neither is listed here.
var reactionCells = map[Origin][]Class{
	OriginEngine: {ClassObserve, ClassAdvise, ClassGate, ClassShapeView, ClassShapeWork},
	OriginUser:   {ClassObserve, ClassAdvise, ClassGate},
}

// isAllowedCell reports whether origin × class is a cell of the Reaction surface matrix. An
// origin or class outside the vocabulary occupies no cell and is refused here.
func isAllowedCell(origin Origin, class Class) bool {
	for _, c := range reactionCells[origin] {
		if c == class {
			return true
		}
	}
	return false
}

// Outcome is what one fired Reaction reports back, in one shape for every seam. The ZERO value
// means "did nothing": a reaction that returns it is not booked as a firing and emits no event.
type Outcome struct {
	// Retry re-streams the Turn (post-response only). The first Retry stops the leg it fires in,
	// whatever retry budget the loop has left — the budget decides only whether the loop
	// re-streams, never whether the cascade continues.
	Retry bool
	// Inject is the correction text: injected into the retried request for Retry, or into the
	// next request for Defer. Empty carries no correction.
	Inject string
	// Defer carries a correction into the NEXT request — the feed-forward path, held in
	// conversation state so it survives a snapshot boundary.
	Defer string
	// Edited says "a shape reaction moved the working value's Revision()"
	// (docs/design/reaction-core-greenfield.md §9.2). The DISPATCHER sets it on the Outcome it
	// books whenever the revision bracket sees the payload's Revision() move; a handler MAY also
	// set it explicitly without moving a revision, which is the same act as the retired
	// ActionIntercept. Either way the firing is booked.
	//
	// It is the last term of the firing rule the dispatcher applies:
	// acted = Retry || Inject != "" || Defer != "" || Edited || the revision moved. The action a
	// firing is reported under follows the same order — "retry" when Retry, else "defer" when
	// Defer, else "intercept" when Edited or the revision moved.
	Edited bool
	// Detail is optional supporting text a renderer may show verbatim or ignore. It is per
	// FIRING, not per reaction — a guard that names what it salvaged computes it each time.
	Detail string
}

// Handler is the behaviour a Reaction runs. It is SEALED — only the five per-seam Go func types
// below implement it — so a handler always names exactly one seam, and Validate can check that
// the reaction's On list agrees with what its handler can actually be called with. Stage 2's
// argv, webhook and MCP handlers, which span Moments, join the seal here.
type Handler interface {
	// seam reports the one seam Moment this handler serves. Unexported: it is the seal.
	seam() Moment
}

// The five Go handler types, one per seam. Each takes the payload its seam already owns and
// returns the one Outcome shape; an error ends the cascade at once and surfaces to the seam.
type (
	// PreRequestFunc shapes the outgoing request before it is sent.
	PreRequestFunc func(ctx context.Context, req *Request) (Outcome, error)
	// PostResponseFunc inspects the model response and chooses an action. It never sees the
	// loop's remaining retry budget — that rides PostResponseMoment, for the dispatcher alone.
	PostResponseFunc func(ctx context.Context, resp *Response) (Outcome, error)
	// PreToolExecFunc acts between the decision to run a tool and its execution, reshaping the
	// pending call through the edit; the loop view is there because the decision is usually
	// cross-Turn.
	PreToolExecFunc func(ctx context.Context, view LoopView, call *ToolCallEdit) (Outcome, error)
	// PostToolResultFunc acts on a tool result before the model next sees it. It receives the
	// originating call because the tool name and arguments live there, not on the result.
	PostToolResultFunc func(ctx context.Context, view LoopView, call ToolCall, result *ToolResultEdit) (Outcome, error)
	// HistoryRewriteFunc edits conversation state. The Conversation is itself the history, so
	// this handler reads and mutates it directly.
	HistoryRewriteFunc func(ctx context.Context, conv *Conversation) (Outcome, error)
)

func (PreRequestFunc) seam() Moment     { return MomentPreRequest }
func (PostResponseFunc) seam() Moment   { return MomentPostResponse }
func (PreToolExecFunc) seam() Moment    { return MomentPreToolExec }
func (PostToolResultFunc) seam() Moment { return MomentPostToolResult }
func (HistoryRewriteFunc) seam() Moment { return MomentHistoryRewrite }

// ToolResultMoment is the post-tool-result seam's payload: the pair a PostToolResultFunc needs,
// carried as one value because the dispatcher's revision bracket reads Revision() off the
// payload and only the edit has one. Edit is never nil.
type ToolResultMoment struct {
	// Call is the originating tool call, read-only here.
	Call ToolCall
	// Edit is the returned result as a revision-bearing working value.
	Edit *ToolResultEdit
}

// Revision forwards to the edit — the working value a post-tool-result reaction reshapes.
func (m ToolResultMoment) Revision() int { return m.Edit.Revision() }

// PostResponseMoment is the post-response seam's payload: the response, plus whether the loop
// has retry budget left. Retryable reaches the DISPATCHER only — it decides whether a builtin
// Retry lets the armed leg run on the untouched response — and never the handler, which takes a
// bare *Response. Resp is never nil.
type PostResponseMoment struct {
	// Resp is the model response as a revision-bearing working value.
	Resp *Response
	// Retryable is the loop's remaining retry budget as a yes/no: true when the loop will
	// re-stream this Turn if a reaction asks it to.
	Retryable bool
}

// Revision forwards to the response — the working value a post-response reaction reshapes.
func (m PostResponseMoment) Revision() int { return m.Resp.Revision() }

// Reaction is the single thing apogee does when the loop passes a Moment (CONTEXT: Reaction):
// one {id, origin, class, on, handler}. Floor guards, the retired Mechanism lab layer and Hooks
// are all one of these (ADR 0076 D1).
type Reaction struct {
	// ID is the reaction's stable identifier — the key an observer sees on a ReactionFiredEvent
	// and the key the identity projector and the provenance ledger use, so it is unique across
	// the engine's builtins and everything armed beside them.
	ID string
	// Origin is who the reaction belongs to: the engine (builtin or bench-armed) or the user.
	Origin Origin
	// Class is what the reaction may do. Origin × Class must be a cell of the Reaction surface.
	Class Class
	// On is the Moments the reaction fires on. With a sealed per-seam Go handler this reduces to
	// the handler's one seam; the slice is the shape stage 2's argv handlers need, which span
	// Moments.
	On []Moment
	// Handler is the behaviour — one of the five Go func types above.
	Handler Handler
	// Timeout is the deadline a non-Go handler runs under. A Go handler IGNORES it: the engine's
	// own reactions run without a deadline, exactly as today's Floor guards do.
	Timeout time.Duration
	// TopLevelOnly opts OUT of sub-agent inheritance. The zero value is inherited by every child
	// agent — today's unconditional membership inheritance — while true keeps the reaction at
	// depth 0.
	TopLevelOnly bool
}

// ErrInvalidReaction is wrapped by Reaction.Validate for a reaction the engine will not accept:
// one with no ID, no origin or class, an origin × class outside the Reaction surface matrix, no
// handler, an empty or duplicate-bearing On list, or an On entry its handler cannot serve.
// Match it with errors.Is.
var ErrInvalidReaction = errors.New("apogee: invalid reaction")

// Validate reports whether the Reaction is well formed, wrapping ErrInvalidReaction with what is
// wrong. It is the one gate every reaction passes before it can fire, so the checks are ordered
// from the most identifying failure outwards: without an ID no later message can name the
// offender.
func (r Reaction) Validate() error {
	if r.ID == "" {
		return fmt.Errorf("%w: a reaction needs a non-empty ID", ErrInvalidReaction)
	}
	if r.Origin == "" {
		return fmt.Errorf("%w %q: no origin", ErrInvalidReaction, r.ID)
	}
	if r.Class == "" {
		return fmt.Errorf("%w %q: no class", ErrInvalidReaction, r.ID)
	}
	if !isAllowedCell(r.Origin, r.Class) {
		return fmt.Errorf(
			"%w %q: origin %q may not take class %q",
			ErrInvalidReaction, r.ID, r.Origin, r.Class,
		)
	}
	if r.Handler == nil {
		return fmt.Errorf("%w %q: no handler", ErrInvalidReaction, r.ID)
	}
	if len(r.On) == 0 {
		return fmt.Errorf("%w %q: fires on no Moment", ErrInvalidReaction, r.ID)
	}

	seen := make(map[Moment]bool, len(r.On))
	for _, m := range r.On {
		if seen[m] {
			return fmt.Errorf("%w %q: Moment %q listed twice", ErrInvalidReaction, r.ID, m)
		}
		seen[m] = true

		if m != r.Handler.seam() {
			return fmt.Errorf(
				"%w %q: Moment %q is not the handler's seam %q",
				ErrInvalidReaction, r.ID, m, r.Handler.seam(),
			)
		}
	}
	return nil
}
