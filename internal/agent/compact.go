package agent

import (
	"context"
	"errors"
	"strings"

	apogeectx "github.com/airiclenz/apogee/internal/context"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// Compaction sampling: a low temperature for a faithful, low-embellishment summary, and a
// generous token cap so a long conversation's summary is bounded but not truncated. They are
// fixed here (not a config surface) — a model-profile knob is a later, additive concern.
const (
	compactTemperature = 0.2
	compactMaxTokens   = 4096

	// compactPromptOverheadTokens reserves headroom, on top of compactMaxTokens (the summary
	// response's reserve), for the summarizer's system prompt, the trailing instruction, the
	// per-message role headers, and the slack in the chars→tokens estimate. The rendered
	// transcript is budgeted to whatever the discovered window has left after both reserves.
	compactPromptOverheadTokens = 512

	// compactMinTranscriptTokens floors the transcript budget so a very small window still
	// sends a useful (if heavily elided) tail rather than collapsing to nothing.
	compactMinTranscriptTokens = 256
)

// Compact triggers generative Compaction on demand — the engine half of the /compact command.
// It summarizes the conversation and Replaces the folded history with a single summary message
// (internal/context.Compact), keeping the protected prefix verbatim. Valid only at a quiescent
// boundary; calling it mid-Exchange is refused (ErrInputPending) so a half-streamed Turn is
// never orphaned, mirroring ClearContext. The Turn counter is untouched and the Agent stays
// snapshot-safe after it returns. A summary-call failure leaves the conversation unchanged.
//
// skipped reports that the conversation was too small to be worth folding (the reducer's
// Result.Skipped — no upstream call, conv untouched), so the caller can say "nothing to
// compact" and leave the context gauge alone rather than falsely claiming a compaction. It is
// always false on error (a fault is not a skip).
func (a *Agent) Compact(ctx context.Context) (skipped bool, err error) {
	if a.inExchange {
		return false, domain.ErrInputPending
	}
	res, err := apogeectx.Compact(ctx, compactCompleter{a}, &a.conv, a.compactTranscriptChars())
	return res.Skipped, err
}

// autoCompact runs generative Compaction at a quiescent boundary when the conversation history has
// outgrown its Budget allocation — the automatic, budget-driven trigger (Phase-4 item 9, CONTEXT:
// Compaction "the default reducer"). It is STRUCTURAL, not a Mechanism (D6): it runs even under
// Bypass (the gate consults only cfg.Context.CompactionEnabled, never cfg.Bypass — a naked model
// still overflows its window without it, decision 12) and is opted out only by the file-only
// `auto-compact: false` config key. It runs the same Compact the /compact command drives (protected
// prefix, Replace write-back), so a fold ending the conversation at a clean prefix → summary shape;
// the loop calls it before it consumes new input, so a just-submitted user message rides the folded
// history as its own turn rather than being folded into the summary. It is non-reentrant (the
// compacting guard) and quiet on success — the Replace is the visible effect (the next Turn's
// UsageEvent re-measures the reduced fill); a fault surfaces as an ErrorEvent and leaves the
// conversation untouched (Compact's own guarantee), so a failed auto-fold never corrupts history and
// the Turn proceeds with the full conversation. A cancellation is not a fault: the Turn's own stream
// carries the cancel to a clean boundary. Two S2 refinements govern WHEN it folds: the trigger is
// Exchange-boundary-only (shouldAutoCompact's inExchange guard — a mid-Exchange over-budget Turn
// defers to the next opening; the sibling emergencyFold below is the ONE fold that may run
// mid-Exchange, and it amends that rule for its own overflow-driven trigger alone — ADR 0018, never
// for this estimate-driven one), and a fold that RAN and STILL leaves the history over its allocation
// saturates the trigger (one ErrorEvent, then it stands down until the estimate drops). A skip
// (Result.Skipped — too few messages past the protected prefix to be worth folding) folds nothing,
// so it proves nothing and never saturates: the trigger simply re-checks at the next opening, when a
// longer tail may have accumulated.
func (a *Agent) autoCompact(ctx context.Context, turn int) {
	if a.compacting || !a.shouldAutoCompact() {
		return
	}
	a.compacting = true
	defer func() { a.compacting = false }()
	res, err := apogeectx.Compact(ctx, compactCompleter{a}, &a.conv, a.compactTranscriptChars())
	if err != nil {
		if ctx.Err() != nil {
			return // a cancel masquerades as a stream error; the Turn's main stream handles it
		}
		a.cfg.Events.Emit(domain.ErrorEvent{EventBase: a.base(turn), Source: "compaction", Err: err.Error()})
		return
	}
	// A skipped fold folded nothing (Compact found too few messages past the protected prefix), so it
	// proves nothing about whether folding can help — gate the saturation latch on a fold that
	// actually RAN. Returning here (no latch, no ErrorEvent) costs nothing: the trigger re-checks at
	// the next Exchange opening for free, where an accumulated tail may make the fold worthwhile.
	if res.Skipped {
		return
	}
	// S2 saturation: a fold that RAN and still leaves the history over its allocation cannot help —
	// the folded shape is the protected prefix (leading system messages + the first user message) plus
	// the single compaction summary, and together they still exceed the History allocation. Latch the
	// trigger off (compactSat) so the still-oversized history is not re-folded at every Exchange
	// opening, and emit exactly one ErrorEvent naming the cause. shouldAutoCompact clears the latch
	// once the estimate drops back under the allocation.
	if a.historyExceedsAllocation() {
		a.compactSat = true
		a.cfg.Events.Emit(domain.ErrorEvent{
			EventBase: a.base(turn),
			Source:    "compaction",
			Err: "compaction could not bring the history under its allocation: the protected prefix " +
				"(system prompt + first user message) and the compaction summary together exceed it; " +
				"automatic folding is paused until the history estimate drops below the allocation",
		})
	}
}

// shouldAutoCompact reports whether the automatic Compaction trigger should fire. It fires only when
// compaction is enabled (cfg.Context.CompactionEnabled — the `auto-compact` key, on by default; the
// on-demand /compact ignores this gate and always folds), at an Exchange boundary (NOT inExchange —
// S2), and when the history has outgrown its Budget History allocation
// (domain.Budget.HistoryExceedsAllocation) AND the trigger is not saturated (compactSat). It
// clears the saturation latch the moment the estimate falls back under the allocation, so growth
// alone cannot re-trigger a fold that already proved it cannot help. A zero History allocation (the
// window is unknown) never trips, so an unbudgeted Agent never auto-folds.
func (a *Agent) shouldAutoCompact() bool {
	if !a.cfg.Context.CompactionEnabled {
		return false
	}
	// S2: auto-compaction is Exchange-boundary-only. At the top-of-step() placement inExchange is
	// false only at an Exchange opening (before pendingInput is consumed), so a mid-Exchange
	// over-budget Turn (a tool-continuation) defers the fold to the next opening — the
	// tool_result_cap Mechanism and the loop's structural floor on a single oversized result shape
	// the request mid-Exchange, and emergencyFold rescues it once the window is actually blown
	// (ADR 0018). Folding on THIS estimate mid-Exchange would leave the request ending in an
	// assistant summary; the emergency fold pays for the exception with its user bridge.
	if a.inExchange {
		return false
	}
	if !a.historyExceedsAllocation() {
		a.compactSat = false // under the allocation again — a later overflow may fold afresh
		return false
	}
	// Over the allocation: fold unless a prior fold already proved it cannot help (compactSat — an
	// oversized protected prefix). Growth alone must not re-trigger while saturated; only dropping
	// back under the allocation (cleared above) rearms the trigger.
	return !a.compactSat
}

// historyExceedsAllocation reports whether the conversation's estimated token size has outgrown the
// Budget's History allocation — the raw over-budget signal both the auto-fold trigger and the
// post-fold saturation check read. It routes through the single domain compare on the SAME Budget
// view the hooks receive (domain.Budget.HistoryExceedsAllocation), so the compaction trigger and a
// hook reading the Budget can never disagree. A zero History allocation (an unknown window) never
// trips.
func (a *Agent) historyExceedsAllocation() bool {
	return a.budget().HistoryExceedsAllocation(a.conv.Messages())
}

// overflowBridge is the user-role message appended after an emergency fold. Its ROLE is the
// load-bearing half: the fold leaves the conversation ending in the assistant summary, and a
// request whose last message is an assistant turn is what a strict chat template refuses (and
// what an instruct model reads as "keep writing that summary") — the user bridge closes the turn
// structure back to a legal …user → assistant → user. Its TEXT is the other half: the model is
// told, in-band, that the history it can see is a summary of a conversation that outgrew the
// window, so it resumes the task instead of re-asking for context it will never get back.
const overflowBridge = "The conversation above was compacted because the previous request " +
	"exceeded the model's context window. Continue the task from the summary."

// emergencyFold folds the conversation so an overflowed request can be retried against a history
// that fits, reporting whether the caller may retry (true ⇒ the conversation WAS folded; false ⇒
// nothing changed and the Turn must give up exactly as it does today). It is the overflow-driven
// Compaction trigger — the reactive twin of autoCompact's estimate-driven one — and, like it, it
// is STRUCTURAL (D6/ADR 0006): the gates below never consult cfg.Bypass, because a naked model
// overflows its window just as surely as a Mechanism-laden one.
//
// It is the ONE fold allowed to run MID-EXCHANGE, deliberately amending S2's
// Exchange-boundary-only rule for this path alone. The asymmetry is the point: the estimate-driven
// trigger (shouldAutoCompact) and the on-demand /compact both defer to the next opening because
// their caller can wait, while a Turn whose request the server just rejected cannot — deferring
// here means abandoning the Exchange, which is precisely the failure this recovery exists to
// prevent. The fold's own shape is what makes running mid-Exchange safe: context.Compact keeps the
// protected prefix and Replaces everything after it with a single summary, so no half-answered
// tool call survives to be orphaned.
//
// Gates, in order: cfg.Context.CompactionEnabled — the file-only `auto-compact: false` opts out of
// recovery too, since the emergency fold IS an automatic fold and a user managing the window
// themselves keeps today's abandon behaviour (no upstream call is made when it is off) — then the
// compacting re-entrancy guard, shared with autoCompact so the two triggers can never nest.
//
// Outcomes: a Result.Skipped fold (too few messages past the protected prefix) means there is
// nothing left to shed, so recovery is impossible and the answer is false; a cancelled ctx returns
// false SILENTLY (the cancel masquerades as a stream error, and the caller's own ctx check routes
// the Turn to the cancel path); any other fault emits one ErrorEvent from source "compaction" —
// mirroring autoCompact — and returns false with the conversation untouched (Compact's own
// guarantee), so a failed emergency fold never corrupts history. Success is QUIET: the Replace and
// the retried request's UsageEvent are the visible effect, exactly as for an automatic fold.
//
// On success the conversation ends …first-user | assistant-summary | user-bridge: strict role
// alternation holds and no dangling tool calls survive the Replace, so any chat template accepts
// the retried request. When the fold ran mid-Exchange, exchangeStart is re-anchored to the
// bridge's index so AbortExchange still rolls back to a clean boundary — the folded prefix +
// summary — rather than into the protected prefix. That repair is required, not optional: the
// boundary is a CACHED value (ADR 0017 §2's recorded fallback) precisely because a rewrite like
// this one can drop the Exchange's opening user message, leaving nothing to re-derive it from. It
// mirrors the S2 repair step() performs after a mid-Exchange truncate_history shrink.
//
// compactSat is deliberately untouched: that latch guards the estimate-driven trigger against
// re-folding a history it already proved it cannot shrink, whereas this path is bounded by the
// caller's one-fold-per-Turn rule — a second overflow after a fold gives up rather than folding
// again.
func (a *Agent) emergencyFold(ctx context.Context, turn int) bool {
	if !a.cfg.Context.CompactionEnabled || a.compacting {
		return false
	}
	a.compacting = true
	defer func() { a.compacting = false }()

	res, err := apogeectx.Compact(ctx, compactCompleter{a}, &a.conv, a.compactTranscriptChars())
	if err != nil {
		if ctx.Err() != nil {
			return false // a cancel masquerades as a stream error; the caller routes the Turn to cancelTurn
		}
		a.cfg.Events.Emit(domain.ErrorEvent{EventBase: a.base(turn), Source: "compaction", Err: err.Error()})
		return false
	}
	if res.Skipped {
		return false // nothing past the protected prefix to shed: a retry would overflow identically
	}

	a.conv.Append(domain.Message{Role: domain.RoleUser, Content: overflowBridge})
	if a.inExchange {
		a.exchangeStart = a.conv.Len() - 1
	}
	return true
}

// compactTranscriptChars returns the character budget for the rendered transcript the summary
// call carries, derived from the discovered context window so the call itself cannot overflow at
// exactly the high fill /compact exists to relieve (post-v1 remediation item 6). The window (in
// tokens) minus the response reserve (compactMaxTokens) minus prompt overhead is the transcript's
// token budget, converted to characters via the budget's chars→token estimate. It returns 0
// (unbounded — render the whole conversation) when the window is unknown: neither discovery nor
// config reported one, so there is no safe basis to bound, and the pre-item-6 full render stands.
func (a *Agent) compactTranscriptChars() int {
	window := a.cfg.Context.MaxContextTokens
	if window <= 0 {
		return 0
	}
	transcriptTokens := window - compactMaxTokens - compactPromptOverheadTokens
	if transcriptTokens < compactMinTranscriptTokens {
		transcriptTokens = compactMinTranscriptTokens
	}
	return int(float64(transcriptTokens) * a.budget().CharsPerToken)
}

// compactCompleter adapts the Agent's provider seam to context.Completer: a single, SILENT
// upstream completion. Unlike streamResponse it emits NO TokenEvent/UsageEvent — compaction is
// a maintenance call, not a Turn, so it must not stream into the transcript or move the live
// context gauge (the gauge re-measures on the next real Turn's usage). It reuses the loop's
// request projection (toProviderRequest) and collects the streamed content into one string; a
// cancelled ctx or a terminal stream fault surfaces as an error, so the reducer leaves the
// conversation untouched.
type compactCompleter struct{ a *Agent }

func (c compactCompleter) Complete(ctx context.Context, msgs []domain.Message) (string, error) {
	// The summarizer request runs no hooks, so it carries no fire ledger (Fired ⇒ 0 throughout).
	req := domain.NewRequest(c.a.cfg.Model, msgs, nil, c.a.budget(), c.a.turnIndex, nil)
	temp, maxTok := compactTemperature, compactMaxTokens
	req.SetSampling(domain.SamplingParams{Temperature: &temp, MaxTokens: &maxTok})

	var content strings.Builder
	var failed bool
	var errMsg string
	for delta := range c.a.upstream.Stream(ctx, c.a.toProviderRequest(req)) {
		switch delta.Kind {
		case provider.DeltaContent:
			content.WriteString(delta.Content)
		case provider.DeltaError, provider.DeltaContextOverflow:
			failed = true
			errMsg = delta.Err
		}
	}
	if ctx.Err() != nil {
		return "", ctx.Err() // a cancel masquerades as a stream error; ctx wins (as in respondAndReview)
	}
	if failed {
		return "", errors.New(errMsg)
	}
	return content.String(), nil
}
