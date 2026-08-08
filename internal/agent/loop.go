package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	apogeectx "github.com/airiclenz/apogee/internal/context"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/processing"
	"github.com/airiclenz/apogee/internal/prompt"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/security"
	"github.com/airiclenz/apogee/internal/tools"
)

// experimentalMechanismID is the loop's shorthand for the reserved synthetic MechanismID a
// descriptor-less experimental hook fires under (ADR 0002 — no descriptor, no
// self-regulation). The constant itself lives in domain (R5, phase-4-review-fixes item 4)
// so MechanismRegistry.Add can refuse a catalogued Mechanism claiming it.
const experimentalMechanismID = domain.ExperimentalMechanismID

// maxPostResponseRetries caps how many times an ActionRetry post-response decision may
// re-call the Upstream within one Turn, so a response-repair hook that always retries
// cannot spin the loop forever. After the cap the loop proceeds with the last response.
const maxPostResponseRetries = 3

// errHookPanicked is an internal signal — never returned to the host — that a
// panic was recovered at an extension boundary and reported as an ErrorEvent, so
// the loop can degrade to a clean quiescent boundary instead of unwinding.
var errHookPanicked = errors.New("apogee: extension boundary recovered a panic")

// step advances the loop one Turn and returns at a quiescent boundary (ADR 0007). The full
// Turn is: consume queued input → history-rewrite hooks → build request (drain deferred
// corrections + pre-request hooks) → stream the Upstream reply (emitting TokenEvents) →
// parse tool calls → post-response hooks → if the model asked for tools, dispatch each
// through Approval and continue the Exchange (StatusTurnComplete); otherwise commit the
// final message and end it (StatusExchangeComplete).
//
// Every return is at a serializable boundary. A ctx cancellation rolls this Turn's work
// back and returns StatusCancelled with resumable state; a recovered extension panic or
// Upstream fault degrades the Turn to a clean boundary without unwinding the host. The one
// Upstream fault that does NOT end the Turn on the spot is a context-window overflow: the
// respond phase folds the history (emergencyFold) and re-sends the same Turn once before
// falling back to that same clean boundary. The same fold also runs PREDICTIVELY — before the
// request is sent, when the estimate already says it cannot fit — and the two share one fold
// per Turn.
func (a *Agent) step(ctx context.Context) (domain.StepResult, error) {
	turn := a.turns.index
	t := &turnRun{turn: turn, start: time.Now()}

	// Designate the prompt surface for everything this Turn reaches: the ONE slot an Approval and
	// an ask_user question both queue on, so the human is never shown two prompts at once however
	// many children are running (ADR 0039 decision 12). It is installed here — the single funnel
	// every Step goes through, Run's and a Step-driving host's alike — because both gates hang off
	// this context: the loop consults the Approver under it, and a tool's Execute receives it. A
	// sub-agent's Steps run under a context derived from this one, and WithPromptSlot keeps the
	// slot already there, so the whole tree queues on the top-level Agent's.
	ctx = domain.WithPromptSlot(ctx, a.prompts)

	// Automatic Compaction (structural, on by default — item 9): fold the conversation before this
	// Turn's request is built when the history has outgrown its Budget allocation. It runs BEFORE
	// consuming pending input so a just-submitted user message survives the fold as its own turn
	// (folding it in would leave the request ending at an assistant summary); a fresh Agent's empty
	// history never trips it. Structural, so it runs under Bypass too (D5/D6).
	a.autoCompact(ctx, turn)

	if a.pendingInput != nil {
		// Open the Exchange: cache the boundary it begins at (the current length, before the first
		// user message is appended) and flip inExchange (turn.go). The reorder of inExchange ahead
		// of the Append is inert — no reader runs between the two.
		a.turns.openExchange()
		// Order: attached-skill blocks → @file-ref blocks → the user's text. Skills are
		// per-turn instructions, so prepending them scopes them to this one message (the right
		// semantics; it avoids a skill leaking into every later turn as a system-prompt edit).
		skillBlocks := a.resolveSkillRefs(turn, a.pendingInput.SkillIDs)
		refs := a.resolveFileRefs(turn, a.pendingInput.FileRefs)
		a.conv.Append(domain.Message{Role: domain.RoleUser, Content: skillBlocks + refs + a.pendingInput.Text})
		a.pendingInput = nil
	}

	// History-rewrite hooks edit conversation state before it is projected (truncation,
	// generative compaction). A recovered panic degrades the Turn with no Upstream call.
	beforeRewrite := a.conv.Len()
	if err := a.runHistoryRewriteHooks(ctx, turn); err != nil {
		return a.turns.end(t, endAbandoned), nil
	}
	// Repair the cached Exchange boundary after a mid-Exchange history rewrite shrank the
	// conversation (S2): reanchorAfterShrink shifts exchangeStart down by the drop delta and owns the
	// guard + clamp (turn.go). A grow or an out-of-Exchange rewrite is a no-op there.
	a.turns.reanchorAfterShrink(beforeRewrite - a.conv.Len())

	// Derive this Turn's request-scoped working values (rollback boundary, request, deferred
	// floor) from the current conversation — the same trio refold re-derives after a fold.
	a.armRequest(t)

	// The PREDICTIVE half of overflow protection: when the calibrated estimate already says this
	// request cannot fit, fold BEFORE spending the round-trip that would be rejected — and cover
	// the one case the reactive path cannot, a server whose 400 body the provider cannot classify
	// as an overflow (there the stream yields a plain DeltaError and no recovery ever fires). It
	// spends the SAME one fold per Turn: a predictive fold latches t.foldSpent, so a wire overflow
	// after it gives up rather than folding twice. When the fold refuses (opted out, nothing left
	// to shed, or the summary call itself faulted) the request goes out exactly as it always did
	// and the reactive path stays the backstop — the estimate is advisory, never a reason to
	// abandon a Turn on its own.
	if a.requestExceedsWindow(t.req) {
		switch a.refold(ctx, t) {
		case foldCancelled:
			// A cancel mid-summary: refold re-queued the corrections and left t at its pre-request
			// boundary, so the cancel exit's truncate-then-restore leaves them queued exactly once.
			return a.turns.end(t, endCancelled), nil
		case foldFolded, foldDeclined:
			// Folded — t is re-derived against the folded history and the Turn's one fold is spent —
			// or declined, where the request goes out unfolded and the reactive path stays the
			// backstop. The estimate is advisory: proceed either way.
		}
	}

	if err := a.runPreRequestHooks(ctx, turn, t.req); err != nil {
		// The request was never sent, so degrade the Turn with no assistant message. The drained
		// corrections need no re-queue here: the abandoned Exchange clears the whole deferred queue
		// regardless (end → closeExchange → F6), so re-queuing them would be dead motion.
		return a.turns.end(t, endAbandoned), nil
	}

	// The respond phase re-sends the SAME Turn after ONE overflow fold: an overflow is the single
	// Upstream fault the loop can act on — the PROMPT did not fit, so folding the history and
	// re-sending is a real remedy rather than a hopeful re-call. refold rewrites history and
	// re-derives every value the request depends on before the second attempt. Every other way out
	// of this loop — a plain fault, a second overflow (t.foldSpent), a cancel — is exactly the
	// behaviour it always had. The predictive guard above and this reactive path share the
	// one-fold-per-Turn budget through t.foldSpent rather than each holding their own counter.
	var resp *domain.Response
	for {
		reviewed, outcome, overflowMsg := a.respondAndReview(ctx, turn, t.req)
		if outcome == turnOK {
			resp = reviewed
			break
		}
		if outcome == turnCancelled {
			return a.turns.end(t, endCancelled), nil
		}
		if outcome != turnOverflowed || t.foldSpent {
			// A plain Upstream fault (respondAndReview already surfaced it), or an overflow with
			// this Turn's one fold already spent. The overflow's ErrorEvent is withheld at the
			// seam so a RECOVERED Turn can stay quiet, which makes this the give-up path that owns
			// it: the carried message surfaces with the same Source and ordering as a plain fault
			// (and the same text, unless no window is known — overflowGiveUpErr) and the Turn
			// degrades to a clean boundary. No re-queue: the abandoned Exchange clears the deferred
			// queue regardless (end → closeExchange → F6).
			if outcome == turnOverflowed {
				a.cfg.Events.Emit(domain.ErrorEvent{EventBase: a.base(turn), Source: "loop", Err: a.overflowGiveUpErr(overflowMsg)})
			}
			return a.turns.end(t, endAbandoned), nil
		}

		// The Turn's one recovery: fold the history and re-derive, then route on the outcome.
		switch a.refold(ctx, t) {
		case foldCancelled:
			// The fold declined silently because ctx was cancelled mid-summary (the cancel
			// masquerades as a stream error, so only ctx can tell them apart — the check the fold
			// delegates to its caller). A cancelled fold leaves the conversation untouched, so
			// t.rollback still points at this Turn's pre-request boundary, and the cancel exit's
			// truncate-then-restore leaves the corrections refold re-queued exactly once.
			return a.turns.end(t, endCancelled), nil
		case foldDeclined:
			// Nothing was folded — recovery is opted out (`auto-compact: false`), there was nothing
			// left past the protected prefix to shed, or the summary call itself faulted (the fold
			// surfaced that one from source "compaction") — so the same request would overflow
			// identically. Give up exactly as above; the corrections went back on the queue inside
			// refold, and the abandoned Exchange clears them (F6).
			a.cfg.Events.Emit(domain.ErrorEvent{EventBase: a.base(turn), Source: "loop", Err: a.overflowGiveUpErr(overflowMsg)})
			return a.turns.end(t, endAbandoned), nil
		case foldFolded:
			// The fold rewrote the conversation and refold re-derived every stale local (rollback,
			// req, deferred, deferredFloor) against the folded history, latching t.foldSpent.
			// Pre-request hooks run per REQUEST, so they run again over the rebuilt one and keep
			// their pre-request failure semantics: no assistant message, Turn degraded (the
			// abandoned Exchange clears the deferred queue — F6).
			if err := a.runPreRequestHooks(ctx, turn, t.req); err != nil {
				return a.turns.end(t, endAbandoned), nil
			}
		}
	}

	calls := resp.ToolCalls()
	if len(calls) == 0 {
		// Final no-tool response: commit the assistant message and end the Exchange. It is
		// necessarily substantive — an empty reply never reaches here, the empty-reply guard
		// (reviewedOutcome) faults the Turn first — so it is a NEUTRAL Turn for self-regulation's
		// next-Turn judgment (R3), whose harmful proxy is the tool-result error alone. The empty
		// final used to be that judgment's second harmful signal; a faulted Turn is discarded
		// unjudged, so the signal is gone rather than merely relocated (CONTEXT: Self-regulation).
		a.conv.Append(assistantMessage(resp, nil))
		a.cfg.Events.Emit(domain.MessageEvent{EventBase: a.base(turn), Text: resp.Text()})
		return a.turns.end(t, endExchangeDone), nil
	}

	// The model requested tools: commit the assistant tool-call message, then dispatch
	// each call through Approval. A cancellation mid-tool rolls the whole Turn back.
	a.conv.Append(assistantMessage(resp, calls))
	if a.dispatchTools(ctx, turn, calls) == dispatchCancelled {
		return a.turns.end(t, endCancelled), nil
	}
	return a.turns.end(t, endTurnDone), nil
}

// armRequest (re)derives the Turn's request-scoped working values from the current conversation:
// the rollback boundary a cancellation restores to, the request (draining the deferred correction
// queue), and the queue's post-drain floor. It is called once when the Turn first builds its
// request and again by refold after a fold rewrites the conversation, so every value the request
// depends on is re-read from the same post-fold state.
func (a *Agent) armRequest(t *turnRun) {
	// rollback marks the boundary a cancellation restores to: this Turn's assistant message and
	// tool results are dropped and the drained deferred corrections re-queued, so resume
	// re-attempts the Turn from serializable state. The committed user message is kept — the input
	// is not lost to a cancel. After a fold this re-derives PAST the fold (decision 6: the fold is
	// history maintenance, not part of the Turn's attempt, so a later cancel keeps it and must
	// never roll back into a pre-fold index).
	t.rollback = a.conv.Len()
	t.req, t.deferred = a.buildRequest(t.turn)
	// deferredFloor is the deferred queue's length after this Turn's request drained it and BEFORE
	// any post-response hook re-defers — the boundary the cancel exit truncates back to, so a
	// cancelled Turn's own deferrals die with the Turn and only the drained injections are
	// restored (F6).
	t.deferredFloor = a.conv.DeferredLen()
}

// foldOutcome classifies refold's result for the caller's routing.
type foldOutcome int

const (
	foldFolded    foldOutcome = iota // history rewritten; t re-derived against it; t.foldSpent latched
	foldDeclined                     // nothing folded (opted out / nothing to shed / summary fault); t re-derived unchanged
	foldCancelled                    // ctx cancelled mid-summary; t left untouched — route to end(t, endCancelled)
)

// refold runs the emergency fold-and-rebuild ritual both overflow paths (the predictive guard and
// the reactive respond loop) previously copied: re-queue t's drained corrections so the rebuilt
// request carries them, run the emergency fold, resolve the ctx-cancel emergencyFold delegates to
// its caller, and re-derive t's working values from the (possibly folded) conversation. It latches
// t.foldSpent on a fold that ran — the Turn's one fold, shared by both paths — and returns how the
// fold ended so the caller can route it (proceed / give up / cancel).
//
// On foldDeclined the conversation is untouched, so the re-derive reproduces the pre-fold values
// exactly and the unfolded Turn proceeds bit-for-bit as before. On foldCancelled nothing was
// folded and no request was sent, so t is left untouched (its pre-fold rollback/floor still valid)
// and the cancel exit's truncate-then-restore leaves the corrections re-queued here exactly once.
func (a *Agent) refold(ctx context.Context, t *turnRun) foldOutcome {
	// Re-queue the drained corrections FIRST so the rebuilt request carries them (armRequest's
	// buildRequest drains the queue again below).
	a.turns.restoreDeferred(t.deferred)
	folded := a.emergencyFold(ctx, t.turn)
	if ctx.Err() != nil {
		// A cancel mid-summary masquerades as a stream error, so only ctx can tell it from a
		// silent decline (the check emergencyFold delegates to its caller). Leave t untouched.
		return foldCancelled
	}
	// Re-derive every value the request depends on from the (possibly folded) conversation.
	// exchangeStart is re-anchored by the fold itself (compact.go). When nothing was folded the
	// conversation is untouched, so all three re-derive to what they already were.
	a.armRequest(t)
	if folded {
		t.foldSpent = true // spend the Turn's one fold, shared by the predictive and reactive paths
		return foldFolded
	}
	return foldDeclined
}

// unknownWindowRemedy is appended to the two events a session with no known window can hit — the
// overflow give-up (below) and the compaction saturation notice (compact.go). Without a window the
// Budget is empty, so every growth bound falls back to one conservative assumed ceiling
// (compactUnknownWindowTranscriptTokens): the session is bounded, but bounded by a guess, and a
// guess that is too small silently shrinks what the model may hold while a guess that is too large
// still ends here. Either way the user is the only one who can replace the guess with the truth, so
// both events name the config key that does it rather than leaving a session that behaves oddly or
// fails identically until /clear (audit 2026-08-01).
const unknownWindowRemedy = "no context window is known for this model, so apogee is bounding what " +
	"it sends by a conservative assumption: set `context-window:` (in tokens) in your config, or use " +
	"a server that reports the window, and the growth bounds follow the real one"

// overflowGiveUpErr builds the give-up ErrorEvent's text from the sanitized message the provider
// produced. The provider's message always LEADS, unchanged, so a give-up stays what it always was
// (ADR 0018 decision 2); the remedy is appended only when the window is unknown, which is the one
// case where the user can act and where doing nothing wedges the session.
func (a *Agent) overflowGiveUpErr(overflowMsg string) string {
	if a.cfg.Context.MaxContextTokens > 0 {
		return overflowMsg
	}
	return overflowMsg + " — " + unknownWindowRemedy
}

// turnOutcome classifies how the stream → parse → post-response phase ended.
type turnOutcome int

const (
	turnOK         turnOutcome = iota // a usable response (a nil-safe *Response is returned)
	turnCancelled                     // ctx was cancelled mid-stream
	turnFailed                        // an Upstream fault (already surfaced as an ErrorEvent)
	turnOverflowed                    // the request did not fit the model's context window — NOT surfaced; the caller owns the ErrorEvent
)

// respondAndReview streams one Upstream reply, parses its tool calls, builds the post-
// response working value, and runs the post-response hooks — re-calling the Upstream in
// place for an ActionRetry decision (bounded by maxPostResponseRetries). A retrying
// decision that carries a correction (Inject != "") re-streams a corrected request in the
// same Turn (R1, amending catalogue C5): the superseded assistant message (text + tool
// calls, when non-empty) and then the correction as a role-safe user message are appended
// to the in-flight request — request-scoped, never committed to history — the exchange the
// sim's retry builders carried. Corrections accumulate across attempts (each retry appends
// onto the same request — the sim's escalating re-asks), bounded by the cap; at the cap
// the last response passes through with no further append. It returns the reviewed
// *Response on turnOK, or nil with turnCancelled / turnFailed / turnOverflowed. Once the
// hook loop resolves, every response passes the empty-reply guard (reviewedOutcome below),
// which faults a reply carrying neither visible text nor tool calls.
//
// The third return is the fault message this call did NOT surface: non-empty only on
// turnOverflowed, where the ErrorEvent is deliberately withheld because an overflow is
// recoverable (fold the history, retry the request) and a recovered Turn must stay quiet.
// The caller owns that decision, so it also owns the give-up event — emitting the carried
// message verbatim keeps a give-up indistinguishable from the plain-fault path below. Every
// other outcome surfaces its own fault here, exactly as before, and carries "".
func (a *Agent) respondAndReview(ctx context.Context, turn int, req *domain.Request) (*domain.Response, turnOutcome, string) {
	for attempt := 0; ; attempt++ {
		reply := a.streamResponse(ctx, turn, req)
		if ctx.Err() != nil {
			return nil, turnCancelled, "" // a cancel masquerades as a stream error; ctx wins
		}
		if reply.failed {
			if reply.overflow {
				return nil, turnOverflowed, reply.errMsg
			}
			a.cfg.Events.Emit(domain.ErrorEvent{EventBase: a.base(turn), Source: "loop", Err: reply.errMsg})
			return nil, turnFailed, ""
		}

		nativeCalls, err := parseToolCalls(reply.toolCalls)
		if err != nil {
			// A malformed tool call degrades to a parse-error path, not a panic: surface
			// it and treat the Turn as a final no-tool response.
			a.cfg.Events.Emit(domain.ErrorEvent{EventBase: a.base(turn), Source: "processing", Err: err.Error()})
			nativeCalls = nil
		}

		resp := a.assembleResponse(turn, req.View(), reply, nativeCalls)
		retry, inject, hookErr := a.runPostResponseHooks(ctx, turn, resp)
		if hookErr != nil {
			// A post-response hook panicked (recovered into an ErrorEvent): the model did
			// reply, so proceed with the response as reviewed so far rather than abandon.
			return a.reviewedOutcome(turn, resp)
		}
		if retry && attempt < maxPostResponseRetries {
			// The Turn re-streams: tell observers the tokens emitted this attempt are
			// superseded, so a streaming UI discards them before the retry streams afresh.
			a.cfg.Events.Emit(domain.StreamResetEvent{EventBase: a.base(turn)})
			if inject != "" {
				// Carry the corrective exchange onto the retried request (R1): the
				// superseded assistant message, then the correction as a role-safe user
				// message. An Inject-less retry stays a bare re-stream of the request.
				// AppendSupersededAssistant freezes the request's committed length, so the
				// next attempt's post-response scanners (req.View() below) see committed
				// history + the response under review, NOT this superseded appendage — the
				// sim ran its retry-cycle detectors against the unmutated request (item 10).
				req.AppendSupersededAssistant(resp.Text(), resp.ToolCalls())
				req.InjectContext(inject)
			}
			continue
		}
		return a.reviewedOutcome(turn, resp)
	}
}

// emptyReplyErrFmt is the fault text an empty reviewed reply surfaces. It names the finish reason
// because that is the one diagnostic the reply itself carries: "stop" says the Upstream believed it
// answered (an aggregator's in-band error on an HTTP 200, a model that emitted nothing), "length"
// says the reply was cut off before any visible token, and an empty reason says the stream ended
// without one.
const emptyReplyErrFmt = "upstream returned an empty reply (finish: %s)"

// reviewedOutcome resolves a reviewed response into respondAndReview's return, guarding the one
// case the Turn must not commit: a reply with nothing in it for the user — no visible text and no
// tool calls. That is an Upstream failure wearing a success's clothes (an in-band error delivered
// on an HTTP 200, a stream that ended before its first token), and committing it writes a blank
// assistant message that hides the failure behind an apparently-answered Turn. So it fails the Turn
// exactly as a stream fault does: one ErrorEvent from source "loop", then turnFailed with no
// response. A thinking-only reply — reasoning present, but no visible text and no tool calls —
// counts as empty: reasoning is not an answer to the user, and the Turn is just as much a non-answer
// for carrying it.
//
// Placement is load-bearing. The guard runs only after the post-response hook loop has resolved, so
// the `empty_response_recovery` Mechanism keeps first claim on an empty reply (its ActionRetry
// re-streams the Turn before this is ever reached) and a hook retry that DID produce content passes
// through untouched. Being engine-level, the guard also fires in Bypass, where no Mechanism is there
// to catch the empty reply — failure honesty is provider/engine correctness, not a Mechanism's job.
func (a *Agent) reviewedOutcome(turn int, resp *domain.Response) (*domain.Response, turnOutcome, string) {
	if strings.TrimSpace(resp.Text()) != "" || len(resp.ToolCalls()) > 0 {
		return resp, turnOK, ""
	}
	a.cfg.Events.Emit(domain.ErrorEvent{
		EventBase: a.base(turn),
		Source:    "loop",
		Err:       fmt.Sprintf(emptyReplyErrFmt, resp.FinishReason()),
	})
	return nil, turnFailed, ""
}

// assembleResponse applies the model profile at the parse seam (D5/D6). It strips the reply's
// inline thinking/harmony channel out of the visible content and — only when the structured
// native path produced no calls — recovers a text-format tool call from that stripped content,
// removing the call's markup from the committed text and assigning it a deterministic
// Turn-derived ID (so snapshot/resume and tests stay stable, unlike the oracle's wall-clock ID).
// The model's reasoning (the Upstream-split reasoning_content joined with any stripped inline
// channel) rides on the Response so assistantMessage can preserve it in history. For a native,
// no-inline-thinking profile the stripper and text parser are no-ops, so visible == reply.content
// and calls == nativeCalls — byte-identical to the pre-profile path.
func (a *Agent) assembleResponse(turn int, view domain.LoopView, rep reply, nativeCalls []domain.ToolCall) *domain.Response {
	visible, reasoning := a.stripper.Strip(rep.content)

	calls := nativeCalls
	if len(calls) == 0 {
		// The native channel found nothing, so the text parser is the only tool-call source
		// (D5). It yields at most one call; native profiles return the no-op parser, so this is
		// a no-op there.
		if call, found := a.textParser.ParseToolCall(visible); found {
			visible = a.textParser.StripToolCall(visible)
			call.ID = fmt.Sprintf("text_call_%d", turn)
			calls = []domain.ToolCall{call}
		}
	}

	return domain.NewResponse(visible, joinThinking(rep.thinking, reasoning), calls, rep.finish, view)
}

// joinThinking combines the Upstream-split reasoning (reply.thinking, the reasoning_content
// field) with the reasoning the stripper lifted out of the inline content, Upstream first and
// blank-line joined. Either being empty returns the other unchanged, so a native reply with no
// inline channel returns reply.thinking untouched (the byte-identical anchor).
func joinThinking(upstream, inline string) string {
	switch {
	case upstream == "":
		return inline
	case inline == "":
		return upstream
	default:
		return upstream + "\n\n" + inline
	}
}

// reply is the assembled result of consuming one streamed completion.
type reply struct {
	content   string
	thinking  string
	toolCalls []provider.ToolCall
	finish    domain.FinishReason
	failed    bool   // a terminal DeltaError / DeltaContextOverflow arrived
	overflow  bool   // that terminal fault was DeltaContextOverflow: the PROMPT did not fit, so folding the history can make the same request succeed
	errMsg    string // the terminal fault message when failed
}

// streamResponse consumes the provider's Delta stream, emitting a TokenEvent for the newly-
// revealed VISIBLE content as it arrives (the live half of §6 #6) and accumulating text,
// reasoning, and the fully-joined tool calls. While the accumulated content ends inside an
// unclosed inline reasoning span (stripper.IsMidChannel), token emission is HELD so a model that
// inlines thinking/harmony channels never leaks that markup onto a live stream (item 3); the
// channel's visible text is revealed once its span closes. A native / no-inline-thinking profile's
// stripper is never mid-channel and returns the content untouched, so every content delta emits
// verbatim and unbuffered — byte-identical to the pre-profile loop. The SSE body is drained to its
// terminal Delta and closed before this returns — so Approval, consulted afterward in
// dispatchTools, never blocks an open Upstream connection. A cancellation surfaces as a terminal
// DeltaError; the caller distinguishes it from a real fault by checking ctx.Err(). A prompt the
// model's context window cannot hold surfaces as a terminal DeltaContextOverflow, which the reply
// records as failed AND overflow so the caller can tell a recoverable request from a generic fault.
func (a *Agent) streamResponse(ctx context.Context, turn int, req *domain.Request) reply {
	var out reply
	var content, thinking strings.Builder
	emitted := 0  // bytes of stripped visible content already sent as TokenEvents this stream
	reasoned := 0 // bytes of stripped inline reasoning already sent as ReasoningEvents this stream
	for delta := range a.upstream.Stream(ctx, a.toProviderRequest(req)) {
		switch delta.Kind {
		case provider.DeltaContent:
			content.WriteString(delta.Content)
			acc := content.String()
			emitted = a.emitVisibleDelta(turn, acc, emitted)
			reasoned = a.emitReasoningDelta(turn, acc, reasoned)
		case provider.DeltaThinking:
			thinking.WriteString(delta.Thinking)
			// The native reasoning channel is already separated by the server, so every chunk
			// is reasoning verbatim — no strip, no prefix bookkeeping (the provider never
			// yields an empty Thinking chunk). Observation only: the channel still reaches
			// history through reply.thinking, exactly as before.
			a.cfg.Events.Emit(domain.ReasoningEvent{EventBase: a.base(turn), Text: delta.Thinking})
		case provider.DeltaToolCall:
			if delta.ToolCall != nil {
				out.toolCalls = append(out.toolCalls, *delta.ToolCall)
			}
		case provider.DeltaDone:
			out.finish = domain.FinishReason(delta.FinishReason)
			if u := delta.Usage; u != nil {
				// Calibrate the token accounting against the server's own count before surfacing
				// it: the reported prompt tokens are the honest fill, and prompt-tokens vs the
				// characters actually sent recomputes this model's chars→token ratio (bounded and
				// smoothed), so LoopView.Budget() tracks the real tokenizer instead of a fixed
				// guess (TDD §8 #8, plan item 8).
				st := req.State()
				a.tokens.Calibrate(apogeectx.PromptChars(st.Messages, st.Tools), u.PromptTokens)
				// Surface the server's token accounting so a streaming observer can light up
				// the context-usage gauge and time the completion for a tokens/sec readout. A
				// server that omits usage sends no Usage here, so no event fires (events.go).
				a.cfg.Events.Emit(domain.UsageEvent{
					EventBase:        a.base(turn),
					PromptTokens:     u.PromptTokens,
					CompletionTokens: u.CompletionTokens,
					TotalTokens:      u.TotalTokens,
				})
			}
		case provider.DeltaError, provider.DeltaContextOverflow:
			// Both are terminal, but only the overflow says something about the request that
			// the loop can act on: the prompt exceeded the window, so a shorter history is a
			// real remedy. Keep the bit here rather than re-classifying the message later.
			out.failed = true
			out.overflow = delta.Kind == provider.DeltaContextOverflow
			out.errMsg = delta.Err
		}
	}
	out.content = content.String()
	out.thinking = thinking.String()
	return out
}

// emitVisibleDelta emits the newly-revealed VISIBLE tail of the accumulated content as a
// TokenEvent and returns the running count of visible bytes emitted so far this stream. While acc
// ends inside an unclosed inline reasoning span (stripper.IsMidChannel) it emits nothing — holding
// the channel's opening markup and in-flight reasoning off the live stream — and once the span
// closes it strips the reasoning channel and emits only the visible bytes past the count already
// sent. The no-op stripper of a native / no-inline-thinking profile never reports mid-channel and
// returns acc untouched, so this emits each content delta verbatim (the provider filters empty
// content chunks, so len(visible) always advances past emitted) — byte-identical to today.
//
// A channel start token split across two deltas (e.g. "<thi" then "nk>") briefly reveals the
// partial prefix live, because IsMidChannel only turns true once the whole token has accumulated;
// this mirrors the oracle's isThinking and is accepted parity — assembleResponse's post-stream
// strip still removes it from the committed message and final MessageEvent, so no suffix buffering
// is added here (item 3's recorded chunk-boundary edge).
func (a *Agent) emitVisibleDelta(turn int, acc string, emitted int) int {
	if a.stripper.IsMidChannel(acc) {
		return emitted
	}
	visible, _ := a.stripper.Strip(acc)
	if len(visible) <= emitted {
		return emitted
	}
	a.cfg.Events.Emit(domain.TokenEvent{EventBase: a.base(turn), Text: visible[emitted:]})
	return len(visible)
}

// emitReasoningDelta is emitVisibleDelta's mirror for the other half of the split: it emits the
// newly-revealed tail of the accumulated INLINE reasoning as a ReasoningEvent and returns the
// running count of reasoning bytes emitted so far this stream. Unlike the visible path it runs
// WHILE stripper.IsMidChannel(acc) is true — that is the whole point: the visible stream is
// deliberately silent for the length of a reasoning span, and this is the only signal that the
// model is working rather than stalled. The no-op stripper of a native / no-inline-thinking
// profile always strips to empty reasoning, so this never emits there (that profile's reasoning
// arrives as DeltaThinking instead) and the content path stays byte-identical.
//
// It relies on the same prefix-stability the visible path does: an unclosed span's tail is
// captured as reasoning while it streams (thinking.go:56-59, harmony.go:89-99) and a closed span
// never changes again, so the accumulation normally only grows. Where it does NOT — a closing
// token accumulating byte by byte counts as span text until it completes and then falls away, and
// the harmony stripper appends the commentary channel after the analysis one — the length guard
// is what keeps the slice in bounds: a shrunk or reordered accumulation emits nothing until it
// passes the high-water mark again. Never slice without it. The bytes are reasoning either way,
// so no visible content can leak here; Text is a liveness signal, not a transcript.
func (a *Agent) emitReasoningDelta(turn int, acc string, reasoned int) int {
	_, reasoning := a.stripper.Strip(acc)
	if len(reasoning) <= reasoned {
		return reasoned
	}
	a.cfg.Events.Emit(domain.ReasoningEvent{EventBase: a.base(turn), Text: reasoning[reasoned:]})
	return len(reasoning)
}

// parseToolCalls adapts the provider's wire tool calls onto processing's native shape and
// parses them into domain.ToolCalls (wire types stay provider-local — ADR 0010). An empty
// batch is a no-op; a malformed call returns an ErrMalformedToolCall-wrapped error.
func parseToolCalls(raw []provider.ToolCall) ([]domain.ToolCall, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	native := make([]processing.NativeToolCall, len(raw))
	for i, tc := range raw {
		native[i] = processing.NativeToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments,
		}
	}
	return processing.ParseNativeToolCalls(native)
}

// assistantMessage builds the committed assistant message from the reviewed response. It
// preserves the model's reasoning channel as reasoning_content in the message's Extra so it
// survives snapshot/resume — the channel is recorded in history, not re-sent upstream (the
// provider seam drops Extra). calls is nil for a final no-tool message and the parsed tool
// calls otherwise.
func assistantMessage(resp *domain.Response, calls []domain.ToolCall) domain.Message {
	msg := domain.Message{Role: domain.RoleAssistant, Content: resp.Text(), ToolCalls: calls}
	if think, ok := resp.Thinking(); ok {
		if raw, err := json.Marshal(think); err == nil {
			msg = msg.WithExtra("reasoning_content", raw)
		}
	}
	return msg
}

// buildRequest projects the conversation onto the hook-facing domain.Request the pre-
// request hooks shape, draining any deferred corrections (the ActionDefer feed-forward)
// and injecting each role-safely. It returns the drained corrections so a cancellation can
// re-queue them. The request carries the tool menu (Plan-filtered) and a trivial Budget so
// a hook can read them through req.View().
func (a *Agent) buildRequest(turn int) (*domain.Request, []string) {
	msgs := a.conv.Messages()
	// The standing system content (ADR 0023) — the configured system prompt and the workspace
	// context files — is seeded at position 0 of the REQUEST projection, never the conversation,
	// so it is re-composed per request (armRequest, and refold after an overflow fold), stays out
	// of history and the snapshot, and both AppendToSystem (mechanism directives) and the wire
	// seam's tool-instruction block fold into THIS one message (prompt → context files →
	// directives → tool block). "" seeds nothing: with no prompt AND no context files the native
	// anchor stays byte-identical.
	//
	// Two consequences are deliberate, not defects: the Budget's predictive guard and its
	// calibration now measure the prompt too (req.State() carries it) — honest accounting of
	// what the request actually costs; and a post-response scanner reading req.View() sees a
	// leading system message whenever a prompt is configured — the same shape a seeding
	// pre-request hook already produced.
	if sys := a.standingSystem(); sys != "" {
		msgs = append([]domain.Message{{Role: domain.RoleSystem, Content: sys}}, msgs...)
	}
	req := domain.NewRequest(a.cfg.Model, msgs, a.toolMenu(), a.budget(), turn, a.tracker.fireCounts)
	req.SetDepth(a.depth)                      // surface this Agent's nesting level through req.View().Depth() (ADR 0013/0014)
	req.SetParallelAgents(a.delegationWidth()) // and the width a delegation batch may take through req.View().ParallelAgents() (ADR 0039)
	deferred, ok := a.conv.TakeDeferred()
	if ok {
		for _, inject := range deferred {
			req.InjectContext(inject)
		}
	}
	return req, deferred
}

// standingSystem composes this request's standing system content — what buildRequest seeds as
// the position-0 system message — from the two INDEPENDENT sources of it: the rendered prompt
// template and the workspace context files' blocks, in that order, separated by a blank line.
// Either source alone seeds a message; only with neither is the result "" and nothing seeded
// at all (the no-prompt-AND-no-context-files native anchor).
//
// The order is the wire order: standing instructions first, then the workspace's own conventions,
// then whatever the mechanism directives and the tool block append after both.
func (a *Agent) standingSystem() string {
	parts := make([]string, 0, 2)
	if rendered := a.systemPrompt(); rendered != "" {
		parts = append(parts, rendered)
	}
	if blocks := a.contextBlocks(); blocks != "" {
		parts = append(parts, blocks)
	}
	return strings.Join(parts, "\n\n")
}

// systemPrompt renders this request's system prompt from the configured template, or ""
// when none is configured. The inputs are live where the placeholders demand it: the mode
// through the lock-guarded Mode() (a Shift+Tab lands on the next request, and a sub-agent
// renders its own inherited mode), the date from a.now (date-only — stable within a day,
// so the KV cache holds), the workspace from Config.
//
// The template was validated at construction (newAgent's prompt.Validate gate), so Render
// cannot meet an unknown placeholder here; if one somehow survived it passes through
// verbatim rather than failing the request.
func (a *Agent) systemPrompt() string {
	if a.cfg.SystemPrompt == "" {
		return ""
	}
	return prompt.Render(a.cfg.SystemPrompt, prompt.Inputs{
		Workspace: a.cfg.WorkspaceDir,
		Mode:      string(a.Mode()),
		Now:       a.now(),
	})
}

// uncalibratedRoomMargin is how many times the working room an estimate must exceed before the
// predictive guard folds on an UNCALIBRATED Budget — one with no server usage reported yet
// (Budget.Used == 0): Turn 1, every sub-agent, and the first Turn after a resume, where the
// estimator is deliberately not serialized while the restored history may already sit near the
// window.
//
// There the chars→token ratio is only the seed (internal/context.DefaultCharsPerToken, 4.0), and a
// calibrated ratio can never leave the estimator's clamp band [2.0, 8.0]. The seed therefore
// overstates the true token count by at most 8.0/4.0 = 2x, so demanding twice the room makes a
// false positive impossible anywhere inside that band, while every pathological case the guard
// exists for still fires with room to spare (a 10 MiB read is ~25x over) — including the
// unclassifiable-400 cover that earns the guard its place. The guard is damped here, never gated
// on calibration: waiting for the first UsageEvent would leave exactly those Turns unprotected.
const uncalibratedRoomMargin = 2

// requestExceedsWindow reports whether req's prompt is ALREADY estimated to be too big for the
// model's context window — the predictive half of overflow protection, read by step() between
// building a request and sending it.
//
// The measure is the one the whole engine shares: domain.PromptChars over the request's projected
// messages and tool menu, through the Budget's calibrated chars→token ratio
// (domain.Budget.EstimateTokens), so this guard can never disagree with the compaction trigger or
// a hook reading the same Budget. The threshold is the FULL working room (ContextLimit −
// ResponseReserve) — uncalibratedRoomMargin times it while the ratio is still the uncalibrated
// seed — deliberately not a softer fraction: a fold is a lossy rewrite of the user's history, so
// it must fire only when the estimate says the request cannot fit at all, never as a comfort
// margin. The ~60%-of-working-room History allocation stays the boundary trigger's business
// (Budget.HistoryExceedsAllocation), not this one's.
//
// With an UNKNOWN window (no discovery, no config: Allocate returns the zero Allocation, leaving
// no working room) BOTH sides of the compare change. The room becomes
// compactUnknownWindowTranscriptTokens — the same conservative ceiling the emergency fold renders
// against — and the measure becomes the TRANSCRIPT alone: the conversation, without the tool menu
// and without the standing system content the request projection seeds at position 0. That
// ceiling is a transcript budget (compact.go), the transcript is the only part a fold can shed,
// and the boundary trigger measures exactly the same quantity against the same number
// (historyExceedsAllocation), so on an unknown window all three bounds read one number through one
// measure.
//
// Measuring the whole REQUEST against that ceiling instead would compare a request-sized quantity
// to a transcript-sized bound, and the fixed costs alone settle it: the default 19-tool menu is
// ~11.5k characters ≈ 3.8k tokens at a code-heavy 3.0 chars/token, already past the ceiling with an
// empty conversation. The guard would then fire on a four-message session and fold every Exchange
// without any fold ever getting under the bound — a comfort margin, which decision 7 forbids
// outright (audit 2026-08-01, follow-up B).
//
// It used to be INERT here, which left the ONE case the reactive path cannot cover (a 400 the
// provider cannot classify as an overflow) with no protection at all on exactly the sessions that
// have no window to protect them. Bounding against an assumed small window means a large window a
// server never advertised is managed as if it were small — the give-up and saturation events both
// name `context-window:` for that reason, and a pinned window restores the exact arithmetic above.
//
// The estimate is advisory either way: an over-estimate costs one fold, and an under-estimate
// costs nothing, because the wire overflow still routes to the reactive path. That asymmetry is
// why an UNCALIBRATED Budget is measured against uncalibratedRoomMargin (documented above) times
// the room rather than the bare room — the ratio's uncertainty is independent of the window's, so
// the margin applies to the assumed room as well.
func (a *Agent) requestExceedsWindow(req *domain.Request) bool {
	b := a.budget()
	room, chars := b.ContextLimit-b.ResponseReserve, 0
	if room > 0 {
		st := req.State()
		chars = domain.PromptChars(st.Messages, st.Tools)
	} else {
		room, chars = compactUnknownWindowTranscriptTokens, domain.PromptChars(a.conv.Messages(), nil)
	}
	if b.Used == 0 {
		room *= uncalibratedRoomMargin
	}
	return b.EstimateTokens(chars) > room
}

// maxRefFileBytes caps a single @file reference, mirroring the read_file tool's ceiling
// (tools.maxFileReadBytes). It is a sanity bound, not a context budget — token-aware
// trimming is the deferred context-builder's job (TDD §8 #8).
const maxRefFileBytes = 10 * 1024 * 1024

// resolveFileRefs reads each @file reference within the workspace fence and returns the
// content blocks to prepend to the user message. Each ref is read through security.SafeOpen —
// the os.Root-pinned, TOCTOU-safe open the read_file tool builds on — so a ref can never
// escape the workspace (a symlink swapped mid-read is refused, not followed). A missing, escaping,
// oversized, directory, or otherwise unreadable ref is surfaced as a loop ErrorEvent and
// skipped: the Turn proceeds with whatever resolved, and a partly-consumed input is never
// mistaken for working. The refs round-trip through a snapshot on UserInput, so a resumed
// session re-resolves them.
func (a *Agent) resolveFileRefs(turn int, refs []string) string {
	if len(refs) == 0 {
		return ""
	}
	var b strings.Builder
	for _, ref := range refs {
		content, err := a.readFileRef(ref)
		if err != nil {
			a.cfg.Events.Emit(domain.ErrorEvent{
				EventBase: a.base(turn),
				Source:    "loop",
				Err:       fmt.Sprintf("@%s could not be resolved and was ignored: %v", ref, err),
			})
			continue
		}
		fmt.Fprintf(&b, "Referenced file `%s`:\n```\n%s\n```\n\n", ref, content)
	}
	return b.String()
}

// readFileRef resolves one workspace-relative reference to its bounded content. An empty
// WorkspaceDir means no file tools are wired, so references cannot be honoured. The size
// check and the read share ONE pinned handle (security.SafeOpen): the cap is decided from
// an fstat of the very descriptor the content is then read through, and the read itself is
// hard-bounded to the cap, so an oversized @ref is refused without being pulled into
// memory and a name flipped mid-call cannot swap a small stat for a large read — a file
// grown past the cap mid-read is refused too, with a fresh fstat of the same fd (see the
// SCOPE note in security/safeio.go).
func (a *Agent) readFileRef(ref string) (string, error) {
	if a.cfg.WorkspaceDir == "" {
		return "", errors.New("no workspace is configured for file references")
	}
	f, err := security.SafeOpen(a.cfg.WorkspaceDir, ref)
	if err != nil {
		return "", err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	if info.Size() > maxRefFileBytes {
		return "", fmt.Errorf("file too large: %d bytes (max %d)", info.Size(), maxRefFileBytes)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxRefFileBytes+1))
	if err != nil {
		return "", err
	}
	if int64(len(data)) > maxRefFileBytes {
		// The ref grew past the cap between the fstat and the read; re-fstat the same fd
		// for the size the refusal reports, falling back to the bytes actually drained.
		size := int64(len(data))
		if fresh, statErr := f.Stat(); statErr == nil {
			size = fresh.Size()
		}
		return "", fmt.Errorf("file too large: %d bytes (max %d)", size, maxRefFileBytes)
	}
	return string(data), nil
}

// resolveSkillRefs resolves each attached skill ID through Config.Skills and returns the
// labeled instruction blocks to prepend to the user message — mirroring resolveFileRefs. The
// blocks are emitted in the order the IDs were attached. An unknown ID (or any ID at all when
// no resolver is wired) is surfaced as a loop ErrorEvent and dropped, so an attached skill is
// never silently ignored — the same "report-and-proceed" contract the @file path keeps. The
// IDs round-trip through a snapshot on UserInput, so a resumed session re-resolves them.
func (a *Agent) resolveSkillRefs(turn int, ids []string) string {
	if len(ids) == 0 {
		return ""
	}
	if a.cfg.Skills == nil {
		a.cfg.Events.Emit(domain.ErrorEvent{
			EventBase: a.base(turn),
			Source:    "loop",
			Err: fmt.Sprintf("%d attached skill(s) could not be resolved (no skills are configured) "+
				"and were ignored", len(ids)),
		})
		return ""
	}

	resolved := a.cfg.Skills.ResolveSkills(ids)
	byID := make(map[string]domain.ResolvedSkill, len(resolved))
	for _, s := range resolved {
		byID[s.ID] = s
	}

	var b strings.Builder
	for _, id := range ids {
		s, ok := byID[id]
		if !ok {
			a.cfg.Events.Emit(domain.ErrorEvent{
				EventBase: a.base(turn),
				Source:    "loop",
				Err:       fmt.Sprintf("attached skill %q is not known and was ignored", id),
			})
			continue
		}
		fmt.Fprintf(&b, "<skill: %s>\n%s\n</skill>\n\n", s.DisplayName, s.Body)
	}
	return b.String()
}

// budget reports the model's context Budget: the discovered window (n_ctx), the token accounting
// the estimator has calibrated against server usage (an honest Used fill and chars→token ratio),
// and the window Allocation the context reducers consume (internal/context.Allocate). It is
// structural — read even under Bypass (D5/D6) — and advisory here: no request is reshaped by it
// until the reducers land (plan item 9).
func (a *Agent) budget() domain.Budget {
	window := a.cfg.Context.MaxContextTokens
	alloc := apogeectx.Allocate(window, a.cfg.Context.ResponseReserve)
	return domain.Budget{
		ContextLimit:    window,
		Used:            a.tokens.Used(),
		CharsPerToken:   a.tokens.CharsPerToken(),
		ResponseReserve: alloc.ResponseReserve,
		SystemPrompt:    alloc.SystemPrompt,
		FileContext:     alloc.FileContext,
		History:         alloc.History,
	}
}

// toolMenu builds the model's tool menu from the resolved registry (nil ⇒ no tools). In
// Plan mode it offers only the tools Plan can actually run — the model is never shown a call
// it cannot make (ADR: Plan is read-only).
//
// The filter keys on planAdmits (resolution.go) — the SAME blast-radius classification the
// ladder's Plan row keys on — not on the bare ReadOnly() self-declaration it read until
// 2026-08-02. A declaration-based filter offered git_diff_range and diagnostics (read-only
// declaration + OS-subprocess marker) in Plan and the ladder refused them on the call; keying
// both on one fact means the menu can never offer what the ladder refuses (contract §4 fn 2).
//
// The mode is read ONCE, before the loop: a mid-build tighten must not compose a menu from two
// different modes (Mode() is live — agent.go).
func (a *Agent) toolMenu() []domain.ToolDef {
	if a.tools == nil {
		return nil
	}
	planMode := a.Mode() == domain.ModePlan
	all := a.tools.All()
	menu := make([]domain.ToolDef, 0, len(all))
	for _, t := range all {
		// EXCEPT the sub_agent recursion point, which is bounded one level down (a Plan
		// sub-agent inherits Plan, so its children are read-only too). It is not a leaf tool at
		// all — resolve() Delegates it before the ladder — so hiding it would wrongly deny a
		// Plan-mode parent the ability to delegate read/research work (ADR 0013).
		if planMode && !planAdmits(t) && t.Name() != tools.SubAgentToolName {
			continue
		}
		menu = append(menu, domain.ToolDef{
			Name:        t.Name(),
			Description: t.Description(),
			Schema:      t.Schema(),
		})
	}
	return menu
}

// loopView builds the read-only window the tool-stage hooks read — the conversation so far
// (including this Turn's committed assistant + tool messages), the tool menu, the budget,
// and the Turn index. It is rebuilt per call from current state so a hook counting prior
// failures across Turns sees up-to-date history.
//
// It is deliberately NOT seeded with the configured system prompt (ADR 0023): the prompt is a
// REQUEST-projection concern owned by buildRequest, while this view is "the conversation so
// far" — which is why the profile's tool-instruction block is likewise absent from it.
func (a *Agent) loopView(turn int) domain.LoopView {
	req := domain.NewRequest(a.cfg.Model, a.conv.Messages(), a.toolMenu(), a.budget(), turn, a.tracker.fireCounts)
	req.SetDepth(a.depth)                      // the tool-stage view reports the same nesting level as the request view
	req.SetParallelAgents(a.delegationWidth()) // and the same delegation width (ADR 0039)
	return req.View()
}

// base is the EventBase every Event this Agent emits carries: the given Turn index, the
// Agent's sub-agent nesting Depth (0 for the top-level Agent, parent+1 for a sub-agent — ADR
// 0013), and its run identity — the id of the sub_agent call that spawned it (empty at depth
// 0) — so a sub-agent's events nest into the parent's stream at Depth > 0, attributable to the
// delegation that asked for them, with no per-call threading. Both facts are read from the
// EMITTING Agent rather than passed around: a nested sub-agent re-emits through its OWN Agent,
// constructed at the deeper depth with the deeper call's id (newChildAgent).
func (a *Agent) base(turn int) domain.EventBase {
	return domain.EventBase{Depth: a.depth, Turn: turn, CallID: a.callID}
}
