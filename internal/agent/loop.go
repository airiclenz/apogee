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
	"github.com/airiclenz/apogee/internal/doctext"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/processing"
	"github.com/airiclenz/apogee/internal/prompt"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/security"
	"github.com/airiclenz/apogee/internal/tools"
)

// maxPostResponseRetries caps how many times an Outcome{Retry} post-response decision may
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
// Upstream fault degrades the Turn to a clean boundary without unwinding the host. Two Upstream
// faults do NOT end the Turn on the spot. A context-window overflow: the respond phase folds the
// history (emergencyFold) and re-sends the same Turn once before falling back to that same clean
// boundary — and the same fold also runs PREDICTIVELY, before the request is sent, when the
// estimate already says it cannot fit, the two sharing one fold per Turn. And a TRANSIENT in-band
// fault (a 429/5xx/provider_unavailable an aggregator wrapped in an HTTP 200 mid-stream): the
// respond phase re-streams the same request once, on its own per-Turn latch, before the fault
// surfaces exactly as it always did.
func (a *Agent) step(ctx context.Context) (domain.StepResult, error) {
	turn := a.turns.index
	t := &turnRun{turn: turn, start: time.Now()}

	// Designate the prompt surface for everything this Turn reaches: the ONE slot an Approval and
	// an ask_user question both queue on, so the human is never shown two prompts at once however
	// many children are running (ADR 0039 decision 12; since 2026-09-15 — plan 2026-09-14 - 03,
	// item 5 — no child holds ask_user, so the question side is the top-level agent's alone and
	// the slot serialises children's Approvals against it). It is installed here — the single funnel
	// every Step goes through, Run's and a Step-driving host's alike — because both gates hang off
	// this context: the loop consults the Approver under it, and a tool's Execute receives it. A
	// sub-agent's Steps run under a context derived from this one, and WithPromptSlot keeps the
	// slot already there, so the whole tree queues on the top-level Agent's.
	ctx = domain.WithPromptSlot(ctx, a.prompts)

	// Stale-tool-result Pruning (structural): rewrite tool results the model has finished with into
	// one-line stubs before this Turn's request is built. It runs BEFORE the fold below because it
	// is the cheap, non-generative half of the reducer pair — a history pruning can relieve never
	// pays for a summary call — and, unlike the fold, it needs no quiescent Exchange boundary,
	// because the rewrite is in place and moves no message (prune.go).
	a.autoPrune(turn)

	// Automatic Compaction (structural, on by default — item 9): fold the conversation before this
	// Turn's request is built when the history has outgrown its Budget allocation. It runs BEFORE
	// consuming pending input so a just-submitted user message survives the fold as its own turn
	// (folding it in would leave the request ending at an assistant summary); a fresh Agent's empty
	// history never trips it. Structural, so it runs under Bypass too (D5/D6).
	a.autoCompact(ctx, turn)

	if in := a.turns.open(); in != nil {
		// The queued input opened the Exchange: the boundary it begins at is cached (the current
		// length, before the first user message is appended) and inExchange is flipped (turn.go).
		// The reorder of inExchange ahead of the Append is inert — no reader runs between the two.
		// And open the undo group this Exchange's writes will accumulate into (ADR 0051). It
		// only MARKS the boundary — the group materializes on the first write after it, or, where
		// snapshots are in force, at the Exchange's close when the workspace tree moved at all
		// (ADR 0074) — so an Exchange that changes no file never becomes an undo step the human
		// has to walk past.
		// This is the one site that opens one, which is what makes an interjection join the
		// Exchange it steered rather than start a new step (ADR 0025 — it commits mid-Exchange
		// and never reaches here), and what keeps a continuation Turn inside the same group.
		//
		// Depth 0 only: a delegated child shares its parent's journal (newChildAgent) and its
		// Exchange is not one the human opened — it runs INSIDE the parent's. Letting it mark a
		// boundary would split one instruction's writes across two undo steps, so the human would
		// have to `/undo` twice to take back work they asked for once (ADR 0051, ratified call 8).
		// The closing half of the pair carries the same gate for the same reason, at the one owner
		// of Exchange end (Agent.closeUndoGroup, reached through turnLifecycle's exchangeObserver).
		if a.journal != nil && !a.isDelegate() {
			a.journal.BeginGroup()
		}
		// The message itself — skill blocks, @file blocks, then the text — is composed by the
		// helper an interjection shares (composeUserMessage), so both doors read identically.
		a.conv.Append(a.composeUserMessage(ctx, turn, *in, false))
	}

	// The history-rewrite Moment: reactions edit conversation state before it is projected
	// (truncation, generative compaction). An error degrades the Turn with no Upstream call.
	beforeRewrite := a.conv.Len()
	if _, err := a.fire(ctx, domain.MomentHistoryRewrite, &a.conv); err != nil {
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

	if _, err := a.fire(ctx, domain.MomentPreRequest, t.req); err != nil {
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
		reviewed, outcome, overflowMsg := a.respondAndReview(ctx, t)
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
				a.emitLoopFault(turn, a.overflowGiveUpErr(overflowMsg))
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
			a.emitLoopFault(turn, a.overflowGiveUpErr(overflowMsg))
			return a.turns.end(t, endAbandoned), nil
		case foldFolded:
			// The fold rewrote the conversation and refold re-derived every stale local (rollback,
			// req, deferred, deferredFloor) against the folded history, latching t.foldSpent.
			// The pre-request Moment runs per REQUEST, so it runs again over the rebuilt
			// one and keeps its pre-request failure semantics: no assistant message, Turn degraded (the
			// abandoned Exchange clears the deferred queue — F6).
			if _, err := a.fire(ctx, domain.MomentPreRequest, t.req); err != nil {
				return a.turns.end(t, endAbandoned), nil
			}
		}
	}

	calls := resp.ToolCalls()
	if a.turns.wrappingUp() {
		// The wrap-up Turn (turnLifecycle.wrapUp) keeps at most ONE tool: write_file, for a delegation
		// spawned with an `output_path`, and then only the calls aimed at that tool survive — the
		// first write to the output path and any write aimed elsewhere (wrapUpCalls, subagent.go).
		// Everything else the reply asked for — a second output-path write included — is asking
		// for something the request told it it cannot have, and a withdrawn menu that is still
		// reachable is no withdrawal at all — so those calls are DROPPED undispatched here, and
		// with the menu withdrawn wholesale that is every call.
		calls = a.wrapUpCalls(calls)
	}
	if len(calls) == 0 {
		// Final no-tool response: commit the assistant message and end the Exchange. It is
		// necessarily substantive — an empty reply never reaches here, the empty-reply guard
		// (reviewedOutcome) faults the Turn first.
		//
		// The wrap-up Turn takes this exit WHATEVER the reply carried once its calls are dropped
		// above: the assistant message is committed without them. A wrap-up reply with no text —
		// empty OR whitespace-only, the same emptiness
		// replyFault tests — is committed NOWHERE and emits no MessageEvent: a blank assistant
		// message would become the child's last visible text and bury the partial result its
		// capped Turns already earned, so that case falls back to the result instead
		// (subagent.go). The committed and emitted text stays the raw reply, untrimmed. Outside
		// the latch the text is never empty here, so nothing changes for it.
		if text := resp.Text(); strings.TrimSpace(text) != "" {
			a.conv.Append(assistantMessage(resp, nil))
			a.cfg.Events.Emit(domain.MessageEvent{EventBase: a.base(turn), Text: text})
		}
		return a.turns.end(t, endExchangeDone), nil
	}

	// The model requested tools: commit the assistant tool-call message, then dispatch
	// each call through Approval. A cancellation mid-tool rolls the whole Turn back.
	a.conv.Append(assistantMessage(resp, calls))
	if a.dispatchTools(ctx, turn, calls) == dispatchCancelled {
		return a.turns.end(t, endCancelled), nil
	}
	if a.turns.wrappingUp() {
		// The wrap-up's one kept write has been dispatched — run against the output path, refused
		// with a result naming it anywhere else (resolve's wrap-up row) — and the Turn still ENDS
		// THE EXCHANGE: the latch buys one request, never a fifth working Turn, and the reply's text
		// stays committed on the assistant message above as the child's closing report.
		return a.turns.end(t, endExchangeDone), nil
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
// request carries them, run the overflow row of the fold table (foldFor, compact.go), and
// re-derive t's working values from the (possibly folded) conversation. It latches t.foldSpent on
// a fold that ran — the Turn's one fold, shared by both paths — and maps foldFor's four-way result
// onto the three-way outcome the caller routes on: folded → foldFolded; declined and faulted →
// foldDeclined (the fold already surfaced a fault from source "compaction", and either way the
// conversation is untouched); cancelled → foldCancelled.
//
// On foldDeclined the conversation is untouched, so the re-derive reproduces the pre-fold values
// exactly and the unfolded Turn proceeds bit-for-bit as before. On foldCancelled nothing was
// folded and no request was sent, so t is left untouched (its pre-fold rollback/floor still valid)
// and the cancel exit's truncate-then-restore leaves the corrections re-queued here exactly once.
func (a *Agent) refold(ctx context.Context, t *turnRun) foldOutcome {
	// Re-queue the drained corrections FIRST so the rebuilt request carries them (armRequest's
	// buildRequest drains the queue again below).
	a.turns.restoreDeferred(t.deferred)
	r := a.foldFor(ctx, t.turn, foldOverflow)
	if r.end == foldEndCancelled {
		return foldCancelled // leave t untouched; the caller routes the cancel
	}
	// Re-derive every value the request depends on from the (possibly folded) conversation.
	// exchangeStart is re-anchored by the fold itself (compact.go). When nothing was folded the
	// conversation is untouched, so all three re-derive to what they already were.
	a.armRequest(t)
	if r.end == foldEndFolded {
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
	turnCancelled                     // ctx was cancelled mid-stream or inside the re-stream hold-off
	turnFailed                        // an Upstream fault (already surfaced as an ErrorEvent)
	turnOverflowed                    // the request did not fit the model's context window — NOT surfaced; the caller owns the ErrorEvent
)

// restreamHoldoff is how long the respond phase waits before re-streaming a transient in-band
// fault: long enough for the momentary condition behind it — an aggregator swapping out the
// provider it routed to, a server shedding load — to pass, short enough that a human watching the
// stream reads it as a stutter rather than a stall. One fixed wait, not a backoff: there is only
// ever one re-stream to space out. It is a var solely so the loop's tests need not sit through it;
// nothing outside a test writes it.
var restreamHoldoff = time.Second

// holdOffRestream waits restreamHoldoff and reports whether the wait completed — false means ctx
// was cancelled first, and the caller must route the cancel rather than re-stream into a context
// that is already gone.
func holdOffRestream(ctx context.Context) bool {
	timer := time.NewTimer(restreamHoldoff)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// respondAndReview streams one Upstream reply, parses its tool calls, builds the post-
// response working value, and runs the post-response hooks — re-calling the Upstream in
// place for an Outcome{Retry} decision (bounded by maxPostResponseRetries). A retrying
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
//
// One class of fault is re-streamed rather than surfaced: a TRANSIENT fault (the provider's
// Retryable verdict — a 429/5xx/provider_unavailable an aggregator wrapped in an HTTP 200
// partway through the stream, or a body cut mid-stream by an EOF or a network timeout — both
// past where the client's own HTTP retries can reach). The Turn re-sends the SAME request once
// (t.restreamSpent) at every depth — a child gets the one re-stream depth 0 gets — and only the loop
// does it: the provider stays a wire, and StreamResetEvent — the same signal an Outcome{Retry}
// emits, which a streaming Driver already reads as "discard the partial reply, it is coming
// again" — is the loop's to emit. A recovered re-stream is SILENT, exactly as a recovered
// overflow fold is; the second fault, of any class, surfaces as every fault always did.
func (a *Agent) respondAndReview(ctx context.Context, t *turnRun) (*domain.Response, turnOutcome, string) {
	// The Turn's identity and its request, aliased for readability — everything below reads them
	// unchanged, and the one write back to t is the re-stream latch.
	turn, req := t.turn, t.req
	for attempt := 0; ; {
		reply := a.streamResponse(ctx, turn, req)
		if ctx.Err() != nil {
			return nil, turnCancelled, "" // a cancel masquerades as a stream error; ctx wins
		}
		if reply.failed {
			if reply.overflow {
				return nil, turnOverflowed, reply.errMsg
			}
			if reply.retryable && !t.restreamSpent {
				// The Turn's one re-stream. Spend the latch first, so the second fault takes the
				// give-up path below however this attempt ends, then tell observers the tokens
				// streamed before the fault are superseded and hold off long enough for a routed
				// provider to be swapped out upstream. A cancel arriving during that wait is a
				// cancel like any other — routed just below, never fallen through to the fault.
				t.restreamSpent = true
				a.cfg.Events.Emit(domain.StreamResetEvent{EventBase: a.base(turn)})
				if holdOffRestream(ctx) {
					continue
				}
				if ctx.Err() != nil {
					// The wait ended because the ctx was cancelled, not because it elapsed. Cancel
					// semantics are uniform wherever the cancel lands (the guard above is the same
					// call): the Turn rolls back to a serializable boundary and RESUMES, so it must
					// not degrade to an abandoned Turn — and no ErrorEvent, because a cancel is the
					// user's own act, not a fault they must act on. The StreamResetEvent already
					// emitted is consistent with that rolled-back Turn: the partial reply it told
					// observers to discard is exactly what the rollback drops.
					return nil, turnCancelled, ""
				}
			}
			a.emitLoopFault(turn, reply.errMsg)
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

		// The post-response Moment: one cascade, whose builtin Floor guards run FIRST (ADR 0071)
		// because they are engine behaviour every model runs with, so a looping or malformed
		// response is repaired before anything armed above them looks at it. Whoever asks, the
		// retry is the SAME budget — the Turn re-streams either way, and separating them would let
		// the ladder spend it twice — which is why the remaining budget rides the payload: it is
		// what decides whether a builtin's retry takes the Turn away from the legs below.
		out, fireErr := a.fire(ctx, domain.MomentPostResponse, domain.PostResponseMoment{
			Resp:      resp,
			Retryable: attempt < maxPostResponseRetries,
		})
		if fireErr != nil {
			// A post-response reaction faulted (a panic is recovered into an ErrorEvent and the
			// cascade goes on; only a returned error reaches here): the model did reply, so
			// proceed with the response as reviewed so far rather than abandon.
			return a.reviewedOutcome(turn, resp)
		}
		if out.Retry && attempt < maxPostResponseRetries {
			// The retry attempts are counted HERE rather than in the loop header because the
			// transient-fault re-stream above loops back through that header too, and a blip must
			// not spend a reaction's retry budget: separate remedies, separate budgets.
			attempt++
			a.applyRetry(turn, req, resp, out.Inject)
			continue
		}
		return a.reviewedOutcome(turn, resp)
	}
}

// applyRetry prepares the Turn's next attempt for a retry the caller has already counted, and is
// the ONE place a re-stream is set up: a Floor guard's retry and a post-response Reaction's Outcome{Retry}
// take it identically, because the two differ only in who asked.
//
// It tells observers the tokens emitted this attempt are superseded, so a streaming UI discards them
// before the retry streams afresh, and then — when there is a correction to carry — appends the
// corrective exchange onto the retried request (R1): the superseded assistant message, then the
// correction as a role-safe user message. An inject-less retry stays a bare re-stream of the
// request. AppendSupersededAssistant freezes the request's committed length, so the next attempt's
// post-response scanners (req.View()) see committed history + the response under review, NOT this
// superseded appendage — the sim ran its retry-cycle detectors against the unmutated request
// (item 10).
func (a *Agent) applyRetry(turn int, req *domain.Request, resp *domain.Response, inject string) {
	a.cfg.Events.Emit(domain.StreamResetEvent{EventBase: a.base(turn)})
	if inject == "" {
		return
	}
	req.AppendSupersededAssistant(resp.Text(), resp.ToolCalls())
	req.InjectContext(inject)
}

// emptyReplyErrFmt is the fault text an empty reviewed reply surfaces. It names the finish reason
// because that is the one diagnostic the reply itself carries: "stop" says the Upstream believed it
// answered (an aggregator's in-band error on an HTTP 200, a model that emitted nothing), "length"
// says the reply was cut off before any visible token, and an empty reason says the stream ended
// without one.
const emptyReplyErrFmt = "upstream returned an empty reply (finish: %s)"

// cappedReplyErrFmt is the fault text for the one empty reply emptyReplyErrFmt would misdescribe:
// a reply that ran into the ceiling the engine itself stated (ADR 0046). The model DID answer — at
// length, for as long as apogee allowed it to — and stopped only because apogee said stop, so
// calling that "an empty reply" hides both the cap and the tokens burned reaching it (the
// 2026-08-12 incident spent 20,653 reasoning tokens and would have reported nothing but "empty").
// So the message names the ceiling and, when the model reasoned, roughly what it spent under it:
// those are the two numbers the remedy turns on — a larger max-output-tokens: for this server, or a
// task small enough to answer inside the current one. It runs no retry itself (ADR 0046 decision 4)
// but no longer claims one would fail: a reasoning model's spend under the cap varies from pass to
// pass, and the same request has been seen answering on its second run (30a3b2df, 2026-09-14) —
// so the last clause says a retry MAY succeed there, and leaves the choice to the reader.
const cappedReplyErrFmt = "reply hit the output cap apogee set (%d tokens) with no visible text to " +
	"show for it%s — raise max-output-tokens: for this server or narrow the task; a retry may succeed on a reasoning model"

// cappedDelegateReplyErrFmt is the fault text for a CHILD's reply that ran into the same ceiling
// while carrying no tool call but visible text — the one case cappedReplyErrFmt above does not
// cover, an EMPTY capped reply being empty at every depth and keeping that message's reasoning-spend
// number. A truncated answer is not a result: it reads as one to the parent MODEL,
// which sees a tool result and no cut, so on 2026-08-25 a capped delegate reply was accepted as a
// delegation's final answer and flowed back to a coordinator as a 223K-character "finding" that
// stopped mid-sentence. A human reading the main agent's transcript can see the cut and ask for the
// rest, so depth 0 keeps the old rule; a parent has no such recourse and gets a fault instead.
const cappedDelegateReplyErrFmt = "delegate's reply hit the output cap apogee set (%d tokens) — " +
	"a truncated answer is not a result; narrow the task or raise max-output-tokens: for this server"

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
// Placement is load-bearing. It runs only after the Floor guards and the post-response hook loop
// have resolved, so the empty-response recovery guard keeps first claim on an empty reply (its retry
// re-streams the Turn before this is ever reached) and a retry that DID produce content passes
// through untouched. This fault is what remains once the recovery has spent its attempts: the guard
// is on for every model (ADR 0071), so the two are one ladder — recover first, and fail honestly
// only when recovery is exhausted or switched off (`empty-response-recovery: false`).
//
// WHAT counts as a non-answer is replyFault's judgment, and it is one rule wider on a DELEGATE: a
// child's output-capped reply with no tool call faults even when it carries visible text, because
// that text is a truncated answer no parent model can tell from a whole one. What the fault SAYS
// splits by finish reason too (emptyReplyFault): a reply cut off at the engine's own output cap
// names that cap instead of calling a 20k-token reply "empty". What the fault DOES is unchanged for
// every reply and every depth — one ErrorEvent from source "loop", then turnFailed — so both splits
// are messages, not a second control flow: no retry, no salvage of the reasoning, no Reaction.
func (a *Agent) reviewedOutcome(turn int, resp *domain.Response) (*domain.Response, turnOutcome, string) {
	fault, faulted := a.replyFault(resp)
	if !faulted {
		return resp, turnOK, ""
	}
	a.emitLoopFault(turn, fault)
	return nil, turnFailed, ""
}

// replyFault decides whether a reviewed reply is a non-answer and, when it is, what the fault says.
// A reply carrying tool calls is never one: the loop has work to do, so a cut-off reply that still
// asked for a tool runs it and continues, at every depth.
//
// With no tool calls, two rules apply in this order. The historical rule goes first and stands
// unchanged for every depth: only a reply with no visible text either is empty, and emptyReplyFault
// picks between the empty and the capped wording for it. Its placement is load-bearing — a child's
// EMPTY capped reply is the reply whose reasoning spend is worth the most and the delegate wording
// below carries no such number, so emptiness is judged before depth is. Only a reply that did carry
// visible text reaches the DELEGATE rule: a child's (isDelegate) that hit the output cap faults
// for that text — see cappedDelegateReplyErrFmt for why a truncated delegate answer cannot be
// allowed to pose as the delegation's result.
func (a *Agent) replyFault(resp *domain.Response) (string, bool) {
	if len(resp.ToolCalls()) > 0 {
		return "", false
	}
	if strings.TrimSpace(resp.Text()) == "" {
		return a.emptyReplyFault(resp), true
	}
	if a.isDelegate() && resp.FinishReason() == domain.FinishLength {
		return fmt.Sprintf(cappedDelegateReplyErrFmt, a.maxOutputTokens()), true
	}
	return "", false
}

// emitLoopFault surfaces a loop-level fault as the ErrorEvent it has always been AND records its
// text on the lifecycle (turnLifecycle.noteFault), so a parent converting a faulted CHILD Exchange into a tool result
// can name the cause rather than point the parent model at an error only the human can see
// (runSubAgent). Every fault that ends an Exchange ABANDONED goes through here; the loop's
// non-fatal notices — an ignored @file reference, an unknown attached skill — deliberately do not,
// because nothing ended because of them.
func (a *Agent) emitLoopFault(turn int, err string) {
	a.turns.noteFault(err)
	a.cfg.Events.Emit(domain.ErrorEvent{EventBase: a.base(turn), Source: "loop", Err: err})
}

// emptyReplyFault picks the fault text for a reply with nothing in it, on the one diagnostic that
// tells the two kinds of empty apart. A finish reason of "length" says the reply was CUT OFF — and
// since ADR 0046 every request states a ceiling, that cut is the engine's own cap far more often
// than anything upstream — so it gets cappedReplyErrFmt, naming the cap and the reasoning spent
// under it. Every other reason (an in-band error on an HTTP 200, a stream that ended before its
// first token, a reason the engine has never heard of) keeps emptyReplyErrFmt verbatim.
//
// Two limits of the numbers it reports are deliberate, not defects. The reasoning spend is an
// ESTIMATE through the calibrated chars→token estimator — the Response carries the reasoning text,
// never the server's count of it — hence "roughly". And the cap named is the loop's own value for
// this Agent, so a pre-request hook that overrode MaxTokens for that one request would leave the
// message naming the engine's ceiling rather than the hook's; the engine's is the one an operator
// can act on with max-output-tokens:.
func (a *Agent) emptyReplyFault(resp *domain.Response) string {
	if resp.FinishReason() != domain.FinishLength {
		return fmt.Sprintf(emptyReplyErrFmt, resp.FinishReason())
	}
	spent := ""
	if thinking, ok := resp.Thinking(); ok {
		spent = fmt.Sprintf(", after roughly %d tokens of reasoning", a.tokens.EstimateTokens(len(thinking)))
	}
	return fmt.Sprintf(cappedReplyErrFmt, a.maxOutputTokens(), spent)
}

// assembleResponse applies the model profile's parse seam to the collected completion (D5/D6).
// The collector has already stripped the reply's inline thinking/harmony channel out of the
// visible content (collectCompletion); this drops the native calls the loop could not dispatch
// (dispatchableCalls), and — only when the structured native path produced no usable calls —
// recovers a text-format tool call from that stripped content, removing the call's markup from the
// committed text and assigning it a deterministic Turn-derived ID (so snapshot/resume and tests
// stay stable, unlike the oracle's wall-clock ID). The model's reasoning (the Upstream-split
// channel — `reasoning_content` or its `reasoning` alias — joined with any stripped inline
// channel) rides on the Response so assistantMessage can preserve it in history. For a native,
// no-inline-thinking profile the stripper and text parser are no-ops, so visible == the wire
// content and calls == nativeCalls — byte-identical to the pre-profile path.
func (a *Agent) assembleResponse(turn int, view domain.LoopView, rep completion, nativeCalls []domain.ToolCall) *domain.Response {
	visible := rep.content

	calls := a.dispatchableCalls(turn, nativeCalls)
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

	return domain.NewResponse(visible, rep.thinking, calls, rep.finish, view)
}

// dispatchableCalls drops the native tool calls the loop cannot run — an entry missing the tool
// name it routes on or the id its result is keyed to — on processing.WellFormedToolCall's rule,
// the same one the probe's battery refuses to count such an entry as evidence under (C-18). The
// probe had been made stricter than the loop it speaks for: a server answering `tool_calls:[{}]`
// scored nothing in the battery yet reached dispatch here, where an id-less call's result goes
// back as a tool message whose omitempty tool_call_id drops off the wire — a result the server
// cannot match to any call, on a Turn the model never asked for.
//
// A drop is REPORTED, once per reply, as an ErrorEvent from source "processing" — the same source
// the malformed-parse path uses, because it is the same kind of finding: the server sent a shape
// the loop could not use, and blaming the model for the silence that follows would misdirect the
// remedy. No ID is ever synthesised for a native call: an invented id echoed back is an id the
// server never issued, which is the unusable echo this filter exists to prevent.
//
// The surviving calls are dispatched as before; a reply whose calls ALL fall here reads to the
// caller exactly like one that carried none — the text parser gets its turn, then replyFault.
func (a *Agent) dispatchableCalls(turn int, calls []domain.ToolCall) []domain.ToolCall {
	var kept []domain.ToolCall
	for _, c := range calls {
		if processing.WellFormedToolCall(c.Tool, c.ID) {
			kept = append(kept, c)
		}
	}
	dropped := len(calls) - len(kept)
	if dropped == 0 {
		return calls
	}
	a.cfg.Events.Emit(domain.ErrorEvent{
		EventBase: a.base(turn),
		Source:    "processing",
		Err: fmt.Sprintf("processing: dropped %d of %d tool_calls entries with no tool name or no id — "+
			"a call the loop can dispatch needs both", dropped, len(calls)),
	})
	return kept
}

// streamResponse is the Turn's Upstream call: collectCompletion with the observer that makes the
// stream LIVE. Each content Delta emits a TokenEvent for the newly-revealed VISIBLE content as it
// arrives (the live half of §6 #6); while the accumulated content ends inside an unclosed inline
// reasoning span (stripper.IsMidChannel), token emission is HELD so a model that inlines
// thinking/harmony channels never leaks that markup onto a live stream (item 3), and the
// channel's visible text is revealed once its span closes. A native / no-inline-thinking
// profile's stripper is never mid-channel and returns the content untouched, so every content
// delta emits verbatim and unbuffered — byte-identical to the pre-profile loop. Each native
// reasoning Delta emits a ReasoningEvent verbatim (the server already split the channel; the
// provider never yields an empty Thinking chunk), and the terminal Done calibrates the token
// estimator and emits the Turn's UsageEvent right there — observation only: the text still
// reaches history through the completion the collector returns. What the collector records —
// the drained body, the cancel masquerade the caller resolves with ctx.Err(), the overflow and
// retryable bits — is its doc's.
func (a *Agent) streamResponse(ctx context.Context, turn int, req *domain.Request) completion {
	var content strings.Builder // the observer's own accumulation: the visible/reasoning split is prefix-stable over it
	emitted := 0                // bytes of stripped visible content already sent as TokenEvents this stream
	reasoned := 0               // bytes of stripped inline reasoning already sent as ReasoningEvents this stream
	observe := func(delta provider.Delta) {
		switch delta.Kind {
		case provider.DeltaContent:
			content.WriteString(delta.Content)
			acc := content.String()
			emitted = a.emitVisibleDelta(turn, acc, emitted)
			reasoned = a.emitReasoningDelta(turn, acc, reasoned)
		case provider.DeltaThinking:
			a.cfg.Events.Emit(domain.ReasoningEvent{EventBase: a.base(turn), Text: delta.Thinking})
		case provider.DeltaDone:
			if u := delta.Usage; u != nil {
				// Calibrate the token accounting against the server's own count before surfacing
				// it: the reported prompt tokens are the honest fill, and prompt-tokens vs the
				// characters actually sent recomputes this model's chars→token ratio (bounded and
				// smoothed), so LoopView.Budget() tracks the real tokenizer instead of a fixed
				// guess (TDD §8 #8, plan item 8).
				st := req.State()
				a.tokens.Calibrate(domain.PromptChars(st.Messages, st.Tools), u.PromptTokens)
				// Surface the server's token accounting so a streaming observer can light up
				// the context-usage gauge and time the completion for a tokens/sec readout. A
				// server that omits usage sends no Usage here, so no event fires (events.go).
				// The same report also folds into this Agent's running tally, which the event
				// carries in its cumulative fields: a Driver reads session totals off the latest
				// event per agent rather than summing the stream, and a sub-agent — a separate
				// Agent with its own tally — reports child-local totals at its own Depth.
				a.cfg.Events.Emit(a.usage.record(
					a.base(turn), a.cfg.Model, delta.Model, a.cfg.Context.MaxContextTokens,
					u.PromptTokens, u.CompletionTokens, u.TotalTokens, u.CachedPromptTokens,
				))
			}
		}
	}
	return a.collectCompletion(ctx, a.toProviderRequest(req), observe)
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
// request hooks shape, draining any deferred corrections (the Outcome{Defer} feed-forward)
// and injecting each role-safely. It returns the drained corrections so a cancellation can
// re-queue them. The request carries the tool menu (Plan-filtered) and a trivial Budget so
// a hook can read them through req.View().
func (a *Agent) buildRequest(turn int) (*domain.Request, []string) {
	msgs := a.conv.Messages()
	// The standing system content (ADR 0023) — the configured system prompt and the workspace
	// context files — is seeded at position 0 of the REQUEST projection, never the conversation,
	// so it is re-composed per request (armRequest, and refold after an overflow fold), stays out
	// of history and the snapshot, and both AppendToSystem (mechanism directives) and the wire
	// seam's tool-instruction block fold into THIS one message (the standingBlocks table's five
	// rows in order — standingblocks.go — → directives → tool block). ""
	// seeds nothing: with no prompt AND no context files the native anchor stays byte-identical.
	//
	// Two consequences are deliberate, not defects: the Budget's predictive guard and its
	// calibration now measure the prompt too (req.State() carries it) — honest accounting of
	// what the request actually costs; and a post-response scanner reading req.View() sees a
	// leading system message whenever a prompt is configured — the same shape a seeding
	// pre-request hook already produced.
	if sys := a.standingSystem(); sys != "" {
		msgs = append([]domain.Message{{Role: domain.RoleSystem, Content: sys}}, msgs...)
	}
	req := a.newProjection(msgs, turn)
	// The wrap-up directive (subagent.go), stamped at the same moment and for the same reason as
	// the reply ceiling newProjection stamps: after construction, before any pre-request hook,
	// because it is the engine's own bound and must hold under Bypass, where no hook runs at all.
	// It is the other half of the withdrawn menu toolMenu just returned — without it the child is
	// left to guess why its tools vanished — and it carries the output clause exactly when that
	// menu kept write_file for the delegation's `output_path` (wrapUpWriter). It is per-request
	// and stands alone — not a standingBlocks row: AppendToSystem CREATES the system message when
	// none exists, so a session with no configured prompt and no context files still carries the
	// directive.
	if a.turns.wrappingUp() {
		req.AppendToSystem(wrapUpMarker, a.wrapUpDirective())
	}
	deferred, ok := a.conv.TakeDeferred()
	if ok {
		for _, inject := range deferred {
			req.InjectContext(inject)
		}
	}
	return req, deferred
}

// standingSystem composes this request's standing system content — what buildRequest seeds as
// the position-0 system message — by walking standingBlocks (standingblocks.go) in table order
// and joining every non-empty render with a blank line. The "" contract is kept on the two
// CONFIGURED rows alone: only when neither the rendered prompt template nor the workspace context
// files' blocks render anything is the result "" and nothing seeded at all (the no-prompt-AND-no-
// context-files native anchor), and that check is taken BEFORE any ride-along row is asked to
// render — the ride-along rule the table's ridesAlong column states. The walk itself is
// standingRenders, shared with ContextCost (contextcost.go) so the report counts exactly the
// blocks the request carries.
func (a *Agent) standingSystem() string {
	rows := a.standingRenders()
	if rows == nil {
		return ""
	}
	parts := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.rendered != "" {
			parts = append(parts, row.rendered)
		}
	}
	return strings.Join(parts, "\n\n")
}

// systemPrompt renders this request's system prompt from the configured template, or ""
// when none is configured. The inputs are live where the placeholders demand it: the mode
// through the lock-guarded Mode() (a Shift+Tab lands on the next request, and a sub-agent
// renders its own inherited mode), the date from a.now (date-only — stable within a day,
// so the KV cache holds), the workspace from Config, and the scratch dir through the
// lock-guarded ScratchDir() — constant for the life of a session (it moves only at a
// session boundary), so {{scratch}} is KV-cache-stable too.
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
		Scratch:   a.ScratchDir(),
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
// a hook reading the same Budget. The threshold is the HARD room — the ADVERTISED window less the
// reply reserve (Window − ResponseReserve) — uncalibratedRoomMargin times it while the ratio is
// still the uncalibrated seed — deliberately not a softer fraction: a fold is a lossy rewrite of
// the user's history, so it must fire only when the estimate says the request cannot fit at all,
// never as a comfort margin. That is also why it reads Window rather than the working ceiling
// ContextLimit: a `working-window:` bound is a soft line the reducers keep the session under, and
// folding at it would fire this guard on every request a bounded session deliberately lets run
// past its working room while still fitting the server's window. The ~60%-of-working-room History
// allocation stays the boundary trigger's business (Budget.HistoryExceedsAllocation), not this
// one's — and that one DOES follow the working ceiling, which is how the bound actually bites.
//
// With an UNKNOWN window (no discovery, no config: Allocate returns the zero Allocation, leaving
// no working room) BOTH sides of the compare change. The room becomes
// compactUnknownWindowTranscriptTokens — the same conservative ceiling the emergency fold renders
// against, substituted at the one site every growth bound draws on (deriveGrowthBounds,
// compact.go) — and the measure becomes the TRANSCRIPT alone: the conversation, without the tool menu
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
	g := deriveGrowthBounds(b)
	room, chars := g.room, 0
	if g.windowKnown {
		st := req.State()
		chars = domain.PromptChars(st.Messages, st.Tools)
	} else {
		chars = domain.PromptChars(a.conv.Messages(), nil)
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

// fileRefMaxTokens is the ABSOLUTE cap on one reference block — an @file's content or an attached
// skill's body — whatever the window. The share of the History allocation (refBound) is a floor
// against wedging the fold, and on a million-token window that share is itself hundreds of
// thousands of tokens: a 1.2 MB reference then enters the conversation whole, spends the window
// on one message and is never what the user meant by "@" a file (session-mining review
// 2026-09-14, headline 10). Past this cap the block is elided to the same head/tail-plus-marker
// shape and the model is told to read_file ranges for the rest. A constant, not a config key
// (plan 2026-09-14 - 02, ratified): it mirrors read_file's own default window and is the same
// kind of structural bound.
const fileRefMaxTokens = 32_000

// refBound is the structural bound ONE reference block of a message gets: the whole History
// allocation (structuralFloor) SPLIT across the references that message carries, so however many
// it carries their assembled blocks still fit the allocation — and never more than
// fileRefMaxTokens, the absolute cap a large window would otherwise let the share exceed.
//
// The divisor counts EVERY reference the message submitted — attached skills and @file refs
// together, because both kinds are resolved into the one user message and spend the one
// allocation; splitting each list against the whole floor separately would let a message carrying
// one of each commit twice the allocation the fold can render. It counts the references
// SUBMITTED, not the ones that resolve: a ref that fails leaves the survivors a stricter bound,
// never a looser one, and a lone reference keeps the whole floor — the number a tool result gets.
//
// Computed ONCE per message by the caller (loop.go's pending-input consumption, Interject) and
// handed to both resolvers, so the two seams cannot drift into two different arithmetics.
func (a *Agent) refBound(refs int) int {
	return min(max(a.structuralFloor()/max(refs, 1), 1), fileRefMaxTokens)
}

// clampRef bounds one reference block against bound exactly as clampToBound does — the same
// elision, byte for byte — and reports the clip as a domain.RefClippedEvent NOTE when it
// happened: a clip that worked is housekeeping the human is owed a line about, never a fault,
// so it does not ride the missing-reference ErrorEvent (refIgnored). The note names the bound
// that clipped: the absolute cap when bound IS fileRefMaxTokens, else the reference's share of
// the History allocation, which binds instead on a small window.
func (a *Agent) clampRef(turn int, ref, content string, bound int) string {
	clamped := a.clampToBound(content, bound)
	if len(clamped) < len(content) {
		a.cfg.Events.Emit(domain.RefClippedEvent{
			EventBase: a.base(turn),
			Ref:       ref,
			Tokens:    bound,
			Absolute:  bound == fileRefMaxTokens,
		})
	}
	return clamped
}

// resolveFileRefs reads each @file reference within the workspace fence and returns the
// content blocks to prepend to the user message. Each ref is read through security.SafeOpen —
// the os.Root-pinned, TOCTOU-safe open the read_file tool builds on — so a ref can never
// escape the workspace (a symlink swapped mid-read is refused, not followed). A missing, escaping,
// oversized, directory, or otherwise unreadable ref is surfaced as a loop ErrorEvent and
// skipped: the Turn proceeds with whatever resolved, and a partly-consumed input is never
// mistaken for working. The refs round-trip through a snapshot on UserInput, so a resumed
// session re-resolves them.
//
// A ref whose BYTES are a PDF injects the document's extracted text instead — the same
// extraction, the same "[Page N]" markers and the same failure wording the read_file tool
// produces, because both seams call internal/doctext. The sniff is on content, never on the
// name: a text file someone called notes.pdf still injects its text, and a PDF saved without
// the extension still extracts. A document that yields no text (a scan) is skipped like any
// other unresolvable ref — an ErrorEvent carrying the extractor's own model-facing sentence,
// and nothing injected — because a wall of raw PDF bytes teaches the model nothing and costs
// it a context window to learn it. The block header quotes doctext's annotation for an
// extracted document, so the model is told the format, the page count, and that the lines
// below are read-only before it tries to edit what it cannot write back.
//
// Every block carries the same STRUCTURAL floor a tool result has (clampToolResult, dispatch.go):
// content past its share of the History allocation — or past fileRefMaxTokens, whichever is
// smaller — is elided to the shared head/tail-plus-marker shape BEFORE the header is added, so
// the model still reads which file an elided block came from and, for a document, how many pages
// it had. The bound is the CALLER'S (refBound), not this function's: one message's attached
// skill blocks and @file blocks divide a single allocation between them, so neither kind is
// bounded generously merely because the other kind carried the rest of the references. A clip is
// reported to the human as a note (clampRef, domain.RefClippedEvent), never as the
// missing-reference error. The floor is structural (ADR 0006), not a Reaction:
// it consults no config and is never disabled under Bypass.
// Like the tool floor it edits the conversation itself — the raw block never reaches history, and
// so never reaches a snapshot or the rendered transcript. That is the price of a floor every later
// reducer can rely on, and it is why the emergency fold's keep-the-most-recent-message rule can no
// longer be defeated by one reference: the block the fold cannot shed is now bounded by
// construction. The marker's "re-read with start_line/end_line" hint is actionable here, because
// read_file reaches the very same file.
//
// Document EXTRACTION is bounded by that same clamp budget rather than by the raw-read ceiling
// (maxRefFileBytes, 10 MiB): text past the clamp cannot survive the next line, so walking a
// thousand-page document to produce it costs the Turn its time and memory for bytes that are
// dropped on arrival. The walk also stops on the step's ctx, so a cancelled Turn does not finish
// extracting a document nobody will read.
func (a *Agent) resolveFileRefs(ctx context.Context, turn int, refs []string, bound int) string {
	if len(refs) == 0 {
		return ""
	}
	var b strings.Builder
	// TWICE the clamp's char budget, so a document elided to the head/tail shape has real content
	// at both ends rather than a head the extractor stopped in the middle of.
	extractBytes := 2 * int(float64(bound)*a.budget().CharsPerToken)
	for _, ref := range refs {
		data, err := a.readFileRef(ref)
		if err != nil {
			a.refIgnored(turn, ref, err.Error())
			continue
		}
		content, annotation := string(data), ""
		if doctext.IsPDF(data) {
			extracted, pages, failMessage := doctext.ExtractPDF(ctx, data, extractBytes)
			if failMessage != "" {
				a.refIgnored(turn, ref, failMessage)
				continue
			}
			content, annotation = extracted, " ("+doctext.PDFAnnotation(pages)+")"
		}
		fmt.Fprintf(&b, "Referenced file `%s`%s:\n```\n%s\n```\n\n", ref, annotation, a.clampRef(turn, "@"+ref, content, bound))
	}
	return b.String()
}

// refIgnored reports an @file reference that produced no block — unreadable bytes, or a
// document whose text could not be extracted — and lets the Turn proceed without it. Both
// failures share the one sentence, so a skipped ref reads the same way whatever skipped it.
// reason is handed on unwrapped: for an extraction failure it is the model-facing wording
// doctext wrote, which no re-phrasing here can improve.
func (a *Agent) refIgnored(turn int, ref, reason string) {
	a.cfg.Events.Emit(domain.ErrorEvent{
		EventBase: a.base(turn),
		Source:    "loop",
		Err:       fmt.Sprintf("@%s could not be resolved and was ignored: %s", ref, reason),
	})
}

// readFileRef resolves one workspace-relative reference to its bounded content. An empty
// WorkspaceDir means no file tools are wired, so references cannot be honoured. The size
// check and the read share ONE pinned handle (security.SafeOpen): the cap is decided from
// an fstat of the very descriptor the content is then read through, and the read itself is
// hard-bounded to the cap, so an oversized @ref is refused without being pulled into
// memory and a name flipped mid-call cannot swap a small stat for a large read — a file
// grown past the cap mid-read is refused too, with a fresh fstat of the same fd (see the
// SCOPE note in security/safeio.go).
//
// It returns the BYTES, not a string: the caller sniffs them for a document format, and a PDF
// round-tripped through a Go string before that sniff would be re-encoded, not read.
func (a *Agent) readFileRef(ref string) ([]byte, error) {
	if a.cfg.WorkspaceDir == "" {
		return nil, errors.New("no workspace is configured for file references")
	}
	f, err := security.SafeOpen(a.cfg.WorkspaceDir, ref)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > maxRefFileBytes {
		return nil, fmt.Errorf("file too large: %d bytes (max %d)", info.Size(), maxRefFileBytes)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxRefFileBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxRefFileBytes {
		// The ref grew past the cap between the fstat and the read; re-fstat the same fd
		// for the size the refusal reports, falling back to the bytes actually drained.
		size := int64(len(data))
		if fresh, statErr := f.Stat(); statErr == nil {
			size = fresh.Size()
		}
		return nil, fmt.Errorf("file too large: %d bytes (max %d)", size, maxRefFileBytes)
	}
	return data, nil
}

// resolveSkillRefs resolves each attached skill ID through Config.Skills and returns the
// labeled instruction blocks to prepend to the user message — mirroring resolveFileRefs. The
// blocks are emitted in the order the IDs were attached. An unknown ID (or any ID at all when
// no resolver is wired) is surfaced as a loop ErrorEvent and dropped, so an attached skill is
// never silently ignored — the same "report-and-proceed" contract the @file path keeps. The
// IDs round-trip through a snapshot on UserInput, so a resumed session re-resolves them.
//
// Each block is domain.ResolvedSkill.Block's — the one renderer the load_skill tool shares, so a
// skill reads the same whichever door it came through: the opener, the files: line naming the
// folder its bundled resources live under (when the resolver handed over a Dir), the body with
// every {{SKILL_DIR}} expanded to that Dir (ResolvedSkill.Expand), and the closer. The blank line
// separating one block from the next is this function's.
//
// A body meets the same STRUCTURAL floor an @file block does (clampToBound, dispatch.go): content
// past its share of the History allocation is elided to the shared head/tail-plus-marker shape
// BEFORE the header and the files: line are added, so the model still reads which skill an elided
// block came from and where its bundled files live. A skill body is instruction text a human
// wrote, but nothing bounds its size — a SKILL.md is any file on disk — and the fold's
// keep-the-most-recent-message rule cannot shed the message a skill block rides in, so an
// unclamped body could wedge the very Turn it was invoked to steer. The bound arrives from the
// caller (refBound), shared with the @file blocks of the same message and capped at
// fileRefMaxTokens like them; a clipped body is reported as the same note (clampRef).
// {{SKILL_DIR}} is expanded BEFORE the clamp measures the body: the model is bounded against the
// text it actually reads (Block's own expansion then passes the clamped text through unchanged).
func (a *Agent) resolveSkillRefs(turn int, ids []string, bound int) string {
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
		body := a.clampRef(turn, "/"+id, s.Expand(s.Body), bound)
		b.WriteString(s.Block(body))
		b.WriteString("\n")
	}
	return b.String()
}

// composeUserMessage builds the ONE user message a submitted or interjected input lands as, in
// the fixed block order attached-skill blocks → @file-ref blocks → the human's text. Skills are
// per-turn instructions, so prepending them scopes them to this one message (the right
// semantics; it avoids a skill leaking into every later turn as a system-prompt edit). One
// structural bound for the whole message (refBound), computed from BOTH reference counts and
// handed to both resolvers: the blocks land in one message and split one allocation between
// them rather than each list getting the whole floor. ctx bounds only the @file resolution (a
// cancelled Step stops a document extraction mid-walk — resolveFileRefs); an unresolvable
// reference of either kind is reported as an ErrorEvent and skipped, never a refusal.
// interjected marks the message as committed inside a running Exchange (Interject) so the
// derived Exchange opening does not move (domain.Message.Interjected).
func (a *Agent) composeUserMessage(ctx context.Context, turn int, in domain.UserInput, interjected bool) domain.Message {
	bound := a.refBound(len(in.SkillIDs) + len(in.FileRefs))
	skillBlocks := a.resolveSkillRefs(turn, in.SkillIDs, bound)
	refs := a.resolveFileRefs(ctx, turn, in.FileRefs, bound)
	return domain.Message{
		Role:        domain.RoleUser,
		Content:     skillBlocks + refs + in.Text,
		Interjected: interjected,
	}
}

// budget reports the model's context Budget: the discovered window (n_ctx), the token accounting
// the estimator has calibrated against server usage (an honest Used fill and chars→token ratio),
// and the window Allocation the context reducers consume (internal/context.Allocate). It is
// structural — read even under Bypass (D5/D6) — and advisory here: no request is reshaped by it
// until the reducers land (plan item 9).
//
// The two ceilings are deliberately separate. Window is the ADVERTISED window, the wall the server
// enforces; ContextLimit is the WORKING room the session chose to live in — the smaller of the
// advertised window and the `working-window:` key (ContextConfig.WorkingWindow), which is what the
// Allocation is computed from and therefore what every reducer and Reaction reading the Budget
// honours. They are the same number on a session that configures no working room, which is every
// session that existed before the key did. A working window LARGER than the advertised one is
// ignored rather than refused: the top-level key describes no particular server, so a session that
// binds a small one simply keeps the small one (config refuses the per-entry contradiction, where
// both numbers do describe one server). A working window with NO advertised window is honoured as
// written — the operator named the only room anyone named.
func (a *Agent) budget() domain.Budget {
	window := a.cfg.Context.MaxContextTokens
	limit := window
	if working := a.cfg.Context.WorkingWindow; working > 0 && (window <= 0 || working < window) {
		limit = working
	}
	alloc := apogeectx.Allocate(limit, a.cfg.Context.ResponseReserve, a.cfg.Context.ResponseReserveFraction)
	return domain.Budget{
		Window:          window,
		ContextLimit:    limit,
		Used:            a.tokens.Used(),
		CharsPerToken:   a.tokens.CharsPerToken(),
		ResponseReserve: alloc.ResponseReserve,
		SystemPrompt:    alloc.SystemPrompt,
		FileContext:     alloc.FileContext,
		History:         alloc.History,
	}
}

// minOutputTokenCap and maxOutputTokenCap bound the cap the engine derives from the reply budget
// (ADR 0046), so neither end of the window range produces a ceiling nobody would want. The floor
// is the room a thinking model needs to reason AND still answer — internal/title measured a
// qwen3.6-35B naming call spending 4,045 characters of reasoning before its first word, and a
// working Turn's reply is the larger job — so a small window must not derive a cap that truncates
// every reply. The ceiling is where a bigger window stops buying anything: a reply past ~32k
// tokens is a runaway rather than an answer, and the point of the cap is to end that at a bound
// the engine chose. A window that names a reserve between the two is taken as written — it IS the
// number the Budget already reserved.
const (
	minOutputTokenCap = 4096
	maxOutputTokenCap = 32768
)

// maxOutputTokens reports the ceiling on ONE reply, in tokens — the number every request states on
// the wire (ADR 0046). The pin wins outright when the bound server's entry carries one, at exactly
// the value written: it is an operator's statement about the slot, and clamping it would silently
// refuse the small cap a cheap endpoint is worth as readily as the large one a cloud endpoint can
// serve.
//
// With no pin it is the Budget's OWN ResponseReserve — the room the engine already holds back for
// the reply when it sizes the prompt (internal/context.Allocate) — clamped to [minOutputTokenCap,
// maxOutputTokenCap]. Deriving it there is what stops the request and the budget disagreeing: the
// engine stops reserving room it never told the server about.
//
// That reserve is the reserve of the WORKING room, because the Allocation is: a session bounded by
// `working-window:` derives its reply cap from the room it actually works in rather than from a
// window it has no intention of filling. On a model advertising a very large window that is the
// difference between deriving maxOutputTokenCap every single time — the ceiling swallows any share
// of a million tokens — and deriving a number the operator's own bound implies.
//
// An unknown window (a zero Allocation, so a zero reserve) takes the floor rather than going
// uncapped, because Allocation's contract forbids reading unknown as "unbounded" — the defect that
// wedged an unbudgeted session — and the pin is the escape hatch for a server that advertises no
// window at all.
func (a *Agent) maxOutputTokens() int {
	if pin := a.cfg.Context.MaxOutputTokens; pin > 0 {
		return pin
	}
	switch reserve := a.budget().ResponseReserve; {
	case reserve < minOutputTokenCap: // including the unknown window's zero
		return minOutputTokenCap
	case reserve > maxOutputTokenCap:
		return maxOutputTokenCap
	default:
		return reserve
	}
}

// toolMenu builds the model's tool menu from the resolved registry (nil ⇒ no tools). In
// Plan mode it offers only the tools Plan can actually run — the model is never shown a call
// it cannot make on any target (ADR 0012: Plan is read-only except for the session scratch dir).
// A sub-agent's registry is already the narrowed one its spawn built — the parent's minus the
// human-seat tools no child gets, minus whatever the call's `tools` argument took away
// (defaultSubAgentTools, requestedChildTools) — so the child's menu reads that set and needs no
// filter of its own.
//
// The filter keys on planOffers (resolution.go) — the SAME blast-radius classification the
// ladder's Plan row keys on — not on the bare ReadOnly() self-declaration it read until
// 2026-08-02. A declaration-based filter offered diagnostics (read-only declaration +
// OS-subprocess marker) in Plan and the ladder refused it on the call; keying both on one fact
// means the menu can never offer what the ladder refuses on every target (contract §4 fn 2).
//
// Two classes pass the filter unconditionally: tools.ClassReadOnly, and
// tools.ClassReadOnlySubprocess — RO-subproc, i.e. read-only by construction, subprocess by
// mechanism (the git read set: git_status, git_log, git_diff_range, git_show), which Plan offers
// and runs (contract §4 amendment 2026-09-06). A third passes iff a session scratch dir is set:
// tools.ClassWorkspaceWrite — Apogee's own writers, which Plan runs on that one target and
// refuses elsewhere with a reason naming it (ADR 0012 second loosen, 2026-09-14).
//
// The mode and the scratch dir are each read ONCE, before the loop: a mid-build tighten or
// session move must not compose a menu from two different states (both are live — agent.go).
func (a *Agent) toolMenu() []domain.ToolDef {
	if a.tools == nil {
		return nil
	}
	// The wrap-up Turn withdraws the menu WHOLESALE (turnLifecycle.wrapUp): a delegate stopped at its step
	// cap gets one closing request with no tools at all, and an empty menu is what "the tools are
	// gone" means on a wire that carries no tool_choice — the seam renders no tool-instruction
	// block for it and sends no native array. The withdrawal is the prohibition; step() drops any
	// call a model makes anyway, so no path can reach a tool from here. The ONE exception is
	// write_file for a delegation spawned with an `output_path` (wrapUpWriter, subagent.go): that
	// menu is exactly the one tool, offered only where the child holds it and its Mode admits the
	// write, and step() dispatches only calls to it — a write elsewhere is refused by resolve's
	// wrap-up row, never run.
	if a.turns.wrappingUp() {
		writer, ok := a.wrapUpWriter()
		if !ok {
			return nil
		}
		return []domain.ToolDef{{
			Name:        writer.Name(),
			Description: writer.Description(),
			Schema:      writer.Schema(),
		}}
	}
	planMode := a.Mode() == domain.ModePlan
	scratchSet := a.ScratchDir() != ""
	all := a.tools.All()
	menu := make([]domain.ToolDef, 0, len(all))
	for _, t := range all {
		// EXCEPT the sub_agent recursion point, which is bounded one level down (a Plan
		// sub-agent inherits Plan, so its children are bounded the same way). It is not a leaf
		// tool at all — resolve() Delegates it before the ladder — so hiding it would wrongly deny
		// a Plan-mode parent the ability to delegate read/research work (ADR 0013).
		if planMode && !planOffers(t, scratchSet) && t.Name() != tools.SubAgentToolName {
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
	// Stamped by the same helper as buildRequest's projection, so the two projections of one Turn
	// never state different ceilings (ADR 0046). This one reaches no server — a LoopView is read by
	// the tool-stage hooks and drained by nobody — so its stamps are a consistency measure, not a
	// wire bound.
	return a.newProjection(a.conv.Messages(), turn).View()
}

// newProjection constructs the domain.Request both projections of a Turn — buildRequest's
// hook-facing request and loopView's tool-stage window — are built from: msgs over the
// Plan-filtered tool menu and the Budget, stamped with the three facts the engine states itself,
// HERE, after construction and before any pre-request hook sees the Request. The reply ceiling
// (ADR 0046) is stamped here for two reasons: it is the engine's own bound, so it holds under
// Bypass, where no hook runs at all; and being the loop's value rather than a projection-time
// constant, a hook that sets MaxTokens overrides it, which is what makes SamplingParams's "a nil
// field leaves the loop's value untouched" true of this field at last. Temperature stays nil: the
// server's own default is still the right answer for it. Depth surfaces this Agent's nesting
// level through req.View().Depth() (ADR 0013/0014) and ParallelAgents the width a delegation
// batch may take through req.View().ParallelAgents() (ADR 0039).
func (a *Agent) newProjection(msgs []domain.Message, turn int) *domain.Request {
	req := domain.NewRequest(a.cfg.Model, msgs, a.toolMenu(), a.budget(), turn)
	outputCap := a.maxOutputTokens()
	req.SetSampling(domain.SamplingParams{MaxTokens: &outputCap})
	req.SetDepth(a.depth)
	req.SetParallelAgents(a.delegationWidth())
	return req
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
