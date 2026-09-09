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
// are the hook-point spellings this vocabulary replaced, unchanged.
const (
	MomentPreRequest     Moment = "pre-request"      // shape the outgoing request
	MomentPostResponse   Moment = "post-response"    // inspect the response, choose an action
	MomentPreToolExec    Moment = "pre-tool-exec"    // between decision-to-run and execution
	MomentPostToolResult Moment = "post-tool-result" // act on a result before the model sees it
	MomentHistoryRewrite Moment = "history-rewrite"  // edit conversation state
)

// The six standalone notice Moments — post-hoc points that report a fact of their own rather
// than the closing of a seam. Their values are the spellings a configured `on:` list names;
// `approval-requested` reads as the phase of the ApprovalEvent it reports, beside the
// `approval-decided` that ADR 0076 A6 admitted as the second half of the pair.
const (
	MomentExchangeFinished  Moment = "exchange-finished"  // a Depth-0 Turn closed its Exchange
	MomentTurnFinished      Moment = "turn-finished"      // a Depth-0 Turn boundary, whatever its status
	MomentFileChanged       Moment = "file-changed"       // a workspace-scoped write tool succeeded
	MomentApprovalRequested Moment = "approval-requested" // an Approval was raised, before its decision
	MomentApprovalDecided   Moment = "approval-decided"   // an Approval reached its verdict
	MomentError             Moment = "error"              // a localised, recovered engine fault
)

// The five SEAM-CLOSING notices — one per seam, spelled `<seam>-finished`. Each reports that its
// seam finished passing: the cascade ran to its end and the loop moved on with whatever working
// value came out of it. They are notices and not seams, so a reaction reading one changes nothing
// about the pass it reports; what they buy is observability of an in-loop point from the post-hoc
// half of the vocabulary, where a user-origin observe entry may subscribe (ADR 0076 A6).
const (
	MomentPreRequestFinished     Moment = "pre-request-finished"
	MomentPostResponseFinished   Moment = "post-response-finished"
	MomentPreToolExecFinished    Moment = "pre-tool-exec-finished"
	MomentPostToolResultFinished Moment = "post-tool-result-finished"
	MomentHistoryRewriteFinished Moment = "history-rewrite-finished"
)

// allSeams is the seam vocabulary in loop order; allNotices is the notice vocabulary in the
// order internal/reactions reports it — the six standalone notices first, in the order that
// package already listed them, then the five seam-closing notices in loop order. Appending
// rather than interleaving keeps the five spellings a configuration in the wild already names
// at the head of every listing they appear in.
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
		MomentApprovalRequested,
		MomentApprovalDecided,
		MomentError,
		MomentPreRequestFinished,
		MomentPostResponseFinished,
		MomentPreToolExecFinished,
		MomentPostToolResultFinished,
		MomentHistoryRewriteFinished,
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

// IsNotice reports whether the Moment is one of the notice Moments — the post-hoc kind, whose
// payload is sealed. It is not the negation of IsSeam: a spelling outside the vocabulary is
// neither, so a caller validating a configured Moment must ask both.
func (m Moment) IsNotice() bool {
	for _, n := range allNotices {
		if m == n {
			return true
		}
	}
	return false
}

// Closing returns the seam-closing notice that reports the end of this seam's pass —
// MomentPreRequestFinished for MomentPreRequest, and so on for the other four. It answers the
// ZERO Moment for a notice and for any spelling outside the vocabulary: only a seam closes, and
// a notice is already the report of something that finished.
func (m Moment) Closing() Moment {
	switch m {
	case MomentPreRequest:
		return MomentPreRequestFinished
	case MomentPostResponse:
		return MomentPostResponseFinished
	case MomentPreToolExec:
		return MomentPreToolExecFinished
	case MomentPostToolResult:
		return MomentPostToolResultFinished
	case MomentHistoryRewrite:
		return MomentHistoryRewriteFinished
	}
	return ""
}

// Seams returns the five seam Moments in loop order. The slice is a fresh copy, so a caller
// listing them for a help text or a validation message cannot disturb the vocabulary.
func Seams() []Moment { return append([]Moment(nil), allSeams...) }

// Notices returns the eleven notice Moments in their documented order, as a fresh copy.
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

// GateVerdict is the answer a gate reaction gives about a pending tool call (ADR 0076 D2). It is
// the first stdout line a user's gate command writes and the value a Go gate handler sets, so the
// three spellings are a stable contract on both sides.
type GateVerdict string

const (
	// GateAllow says nothing about the call: a gate may say No, never Yes, so the mode ladder's own
	// verdict stands unchanged (ADR 0049 §4).
	GateAllow GateVerdict = "allow"
	// GateDeny refuses the call outright.
	GateDeny GateVerdict = "deny"
	// GateAsk raises the call to the human, whatever the mode ladder decided on its own. It is also
	// where every unreadable answer lands — an empty, unparseable, failing or timed-out gate asks.
	GateAsk GateVerdict = "ask"
)

// GateDecision is one gate reaction's answer: the verdict and the reason behind it. The ZERO value
// is "no verdict" — the reaction gave no answer, so it changed nothing.
type GateDecision struct {
	// Verdict is allow, deny or ask. The ZERO GateVerdict means the reaction gave no verdict.
	Verdict GateVerdict
	// Reason is the human-facing explanation a gate offers for its verdict — the lines its command
	// wrote after the verdict line. It reaches the person being asked and NEVER the model, so a
	// gate cannot smuggle instructions into the conversation through it.
	Reason string
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
	// intercept decision. Either way the firing is booked.
	//
	// It is one term of the firing rule the dispatcher applies:
	// acted = Retry || Inject != "" || Defer != "" || Edited || Gate.Verdict != "" || the revision
	// moved. The action a firing is reported under follows the same order — "retry" when Retry,
	// else "defer" when Defer, else "intercept" when Edited or the revision moved.
	Edited bool
	// Gate is the verdict a gate reaction gives about the pending tool call at pre-tool-exec — the
	// Approver stage of the Reaction surface (ADR 0076 D2). A non-empty Gate.Verdict is an act like
	// any other, so the firing is booked; the ZERO GateDecision is silence. Only a reaction of
	// class gate sets it: every other class leaves it zero.
	Gate GateDecision
	// Detail is optional supporting text a renderer may show verbatim or ignore. It is per
	// FIRING, not per reaction — a guard that names what it salvaged computes it each time.
	Detail string
}

// Handler is the behaviour a Reaction runs. It is SEALED — only the types declared below
// implement it — so Validate can check that the reaction's On list agrees with what its handler
// can actually be called with. Two kinds share the seal: the five per-seam Go func types, each
// naming exactly one seam, and the out-of-process handlers ArgvHandler and WebhookHandler, which
// name no seam at all — their On list, checked against their class, says where they fire. An MCP
// handler is the kind still to join them.
type Handler interface {
	// seam reports the one seam Moment this handler serves, or the ZERO Moment for an
	// out-of-process handler, which names no seam of its own. Unexported: it is the seal.
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

// ArgvHandler runs a command out of process when a Moment fires — what a user's `run:` argv list
// arms (ADR 0076 D2). Argv[0] is the executable and the rest its arguments; nothing goes through a
// shell, so no quoting or expansion happens on the way. The Moment's payload reaches the command
// through its environment. It serves three of the user's classes, and the class decides both where
// it may fire and what its output is worth: observe fires on notices and its output is ignored;
// advise fires at post-tool-result or file-changed and its stdout becomes the fenced trailer;
// gate fires at pre-tool-exec and its first stdout line is the verdict.
type ArgvHandler struct {
	// Argv is the command and its arguments. Validate seals the KIND only — that the handler's
	// class and Moments agree — while the Runner refuses an empty list, since running it is the
	// Runner's job and the refusal belongs where the attempt is.
	Argv []string
}

// WebhookHandler POSTs the Moment's payload to a URL when a NOTICE Moment fires — the other half
// of the async observe lane (ADR 0076 D2). Unlike an ArgvHandler it takes class observe alone, so
// its response is discarded: nothing an observe reaction returns reaches the model.
type WebhookHandler struct {
	// URL is the endpoint the payload is POSTed to.
	URL string
	// Headers are literal request headers, header name → value.
	Headers map[string]string
	// HeadersEnv are request headers whose VALUE is read from the named environment variable when
	// the reaction fires, header name → variable name — so a token never has to be written into
	// the configuration file to reach the request.
	HeadersEnv map[string]string
}

// seam reports the ZERO Moment: an argv handler names no seam — its class and On list decide
// where it fires.
func (ArgvHandler) seam() Moment { return "" }

// seam reports the ZERO Moment: a webhook handler reacts to notices, and a notice closes no seam.
func (WebhookHandler) seam() Moment { return "" }

// isAsyncHandler reports whether the handler is one of the out-of-process kinds. Validate keys the
// per-class Moment rules on the handler KIND and not on the class alone, because every class an
// argv handler takes is also open to a Go handler — the bench arms those — and a Go handler keeps
// the per-seam rule.
func isAsyncHandler(h Handler) bool {
	switch h.(type) {
	case ArgvHandler, WebhookHandler:
		return true
	}
	return false
}

// servesClass reports whether an out-of-process handler serves the class. An argv command serves
// three of the user's cells — observe, advise and gate (ADR 0076 D2) — while a webhook serves
// observe alone: its response is discarded, so it has no way to advise or to gate. It answers
// only for the two out-of-process kinds; a Go handler is never asked, since its rule is the
// per-seam one.
func servesClass(h Handler, class Class) bool {
	switch h.(type) {
	case ArgvHandler:
		return class == ClassObserve || class == ClassAdvise || class == ClassGate
	case WebhookHandler:
		return class == ClassObserve
	}
	return false
}

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

// The class default deadlines a SYNC-lane handler runs under when its entry sets no `timeout:`
// of its own (ADR 0076 D7). They are shorter than the observe lane's 30s because these handlers
// HOLD THE LOOP: a gate stands between the decision to run a tool and its execution, with a human
// waiting on the answer, and an advise command stands between a tool result and the model reading
// it. A Reaction.Timeout of its own overrides the default for every reaction of its entry.
const (
	// DefaultAdviseTimeout is the deadline a class-advise handler runs under by default.
	DefaultAdviseTimeout = 10 * time.Second
	// DefaultGateTimeout is the deadline a class-gate handler runs under by default. It is the
	// shorter of the two: a gate's timeout escalates to `ask`, so the person is waiting on it.
	DefaultGateTimeout = 5 * time.Second
)

// Reaction is the single thing apogee does when the loop passes a Moment (CONTEXT: Reaction):
// one {id, origin, class, on, handler}. Floor guards, the retired lab layer and user Reactions
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
	// Timeout is the deadline a non-Go handler runs under, on both lanes — the async observe lane
	// and the sync advise and gate lane, whose handlers hold the loop while they run. A Go handler
	// IGNORES it: the engine's own reactions run without a deadline, exactly as today's Floor
	// guards do.
	Timeout time.Duration
	// Workspace narrows a path-bearing notice to one workspace root: set, the reaction fires only
	// for a path inside that root; empty, it fires for every workspace. Like Timeout it belongs to
	// the async lane — the Runner resolves it against the firing path — and a Go handler ignores
	// it.
	Workspace string
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

	// The sentence names observe as the class served because a webhook is the only handler a
	// configuration can push into this refusal: an argv command already serves every class a user
	// entry can carry, so the argv side is reachable from the engine's own arming alone.
	async := isAsyncHandler(r.Handler)
	if async && !servesClass(r.Handler, r.Class) {
		return fmt.Errorf(
			"%w %q: run: a command or webhook reacts as class %q, not %q",
			ErrInvalidReaction, r.ID, ClassObserve, r.Class,
		)
	}

	seen := make(map[Moment]bool, len(r.On))
	for _, m := range r.On {
		if seen[m] {
			return fmt.Errorf("%w %q: Moment %q listed twice", ErrInvalidReaction, r.ID, m)
		}
		seen[m] = true

		if async {
			switch r.Class {
			case ClassAdvise:
				if m != MomentPostToolResult && m != MomentFileChanged {
					return fmt.Errorf(
						"%w %q: advise: reacts at post-tool-result or file-changed; %q is neither",
						ErrInvalidReaction, r.ID, m,
					)
				}
			case ClassGate:
				if m != MomentPreToolExec {
					return fmt.Errorf(
						"%w %q: gate: reacts at pre-tool-exec; %q is not it",
						ErrInvalidReaction, r.ID, m,
					)
				}
			default:
				switch {
				case m.IsSeam():
					return fmt.Errorf(
						"%w %q: run: reacts to notices; %q is a seam",
						ErrInvalidReaction, r.ID, m,
					)
				case !m.IsNotice():
					return fmt.Errorf(
						"%w %q: run: reacts to notices; %q is not one",
						ErrInvalidReaction, r.ID, m,
					)
				}
			}
			continue
		}

		if m != r.Handler.seam() {
			return fmt.Errorf(
				"%w %q: Moment %q is not the handler's seam %q",
				ErrInvalidReaction, r.ID, m, r.Handler.seam(),
			)
		}
	}
	return nil
}

// Generation is the whole live shape of the engine at one moment: the Floor enable set, Bypass,
// and the user-origin observe and sync lists. It is the ONE value a live swap carries (ADR 0076
// A8), replacing the three separate swap idioms — each with its own setter and its own lock — that
// preceded it, so nothing downstream can read a half-swapped state.
type Generation struct {
	// Floor is the Floor guard enable set: which of the seven structural guards are switched off.
	Floor FloorConfig
	// Bypass switches the model-shaping classes off (ADR 0006). The structural guards above stay
	// on under it.
	Bypass bool
	// Observe is the async-lane observe list the Runner fires. The AGENT ignores it: the observe
	// lane is the Runner's, and an agent takes Floor, Bypass and the sync lane out of a generation.
	Observe []Reaction
	// Sync is the user's advise and gate list — the lane the AGENT runs inside the loop, where a
	// handler holds the Turn while it runs and its output reaches the model or the Approver. The
	// RUNNER ignores it, exactly as the agent ignores Observe.
	Sync []Reaction
}

// Validate reports whether the Generation is well formed, wrapping ErrInvalidReaction with what
// is wrong. Floor and Bypass are booleans and cannot be malformed, so every check is about the
// two lanes: each entry validates on its own, each takes a class its lane accepts — observe for
// the Runner's lane, advise or gate for the sync lane, which is the user's alone — and no two
// entries WITHIN one lane share an ID, which is what a firing is reported under. The same ID may
// appear in both lanes: one configured entry resolves to up to one reaction per class and they
// all carry the entry's id.
func (g Generation) Validate() error {
	seen := make(map[string]bool, len(g.Observe))
	for _, r := range g.Observe {
		if err := r.Validate(); err != nil {
			return err
		}
		if r.Class != ClassObserve {
			return fmt.Errorf(
				"%w %q: the observe list takes class %q, not %q",
				ErrInvalidReaction, r.ID, ClassObserve, r.Class,
			)
		}
		if seen[r.ID] {
			return fmt.Errorf("%w %q: listed twice in the observe list", ErrInvalidReaction, r.ID)
		}
		seen[r.ID] = true
	}

	seenSync := make(map[string]bool, len(g.Sync))
	for _, r := range g.Sync {
		if err := r.Validate(); err != nil {
			return err
		}
		if r.Origin != OriginUser {
			return fmt.Errorf(
				"%w %q: the sync list takes origin %q, not %q",
				ErrInvalidReaction, r.ID, OriginUser, r.Origin,
			)
		}
		if r.Class != ClassAdvise && r.Class != ClassGate {
			return fmt.Errorf(
				"%w %q: the sync list takes class %q or %q, not %q",
				ErrInvalidReaction, r.ID, ClassAdvise, ClassGate, r.Class,
			)
		}
		if seenSync[r.ID] {
			return fmt.Errorf("%w: the sync list names %q twice", ErrInvalidReaction, r.ID)
		}
		seenSync[r.ID] = true
	}
	return nil
}

// SplitLanes divides one reaction list into the two lanes a Generation carries: class observe
// goes to the async lane the Runner fires, classes advise and gate to the sync lane the Agent
// runs inside the loop. Order is preserved within each lane, so a cascade fires in the order the
// configuration file wrote. A class outside those three occupies neither lane and is dropped —
// a configured entry resolves to nothing else, and Generation.Validate would refuse one in either
// list. Each result is nil when its lane is empty.
func SplitLanes(list []Reaction) (observe, sync []Reaction) {
	for _, r := range list {
		switch r.Class {
		case ClassObserve:
			observe = append(observe, r)
		case ClassAdvise, ClassGate:
			sync = append(sync, r)
		}
	}
	return observe, sync
}
