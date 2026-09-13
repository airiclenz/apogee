package agent

import (
	"context"
	"fmt"

	"github.com/airiclenz/apogee/internal/domain"
)

// The context-fill notice (ADR 0077): the engine's first advise Reaction, and the one builtin
// that is not a Floor guard. At post-tool-result it tells the model how far the conversation has
// climbed toward the automatic Compaction line — the Budget's History allocation, read through
// the same estimate the trigger compares (domain.Budget.HistoryFill), so notice and fold can
// never disagree on where the line is — at three fixed rungs, as the advise trailer on the
// closing tool result. It steers the model rather than correcting what it sees, which is why it
// ships OFF (Config.ContextFillNotice) and why Bypass switches it off with the rest of its class.

// contextFillNoticeID is the reaction's id — what its ReactionFiredEvent, its advice fence and
// its `/settings` row are keyed by, and the one non-guard id armReactions reserves.
const contextFillNoticeID = "context-fill-notice"

// actionNotice is the action label every firing books under: the notice neither retries nor
// edits, it speaks.
const actionNotice = "notice"

// fillRungs are the three percentages of the compaction line the notice fires at, ascending.
// Fixed rather than configurable (ADR 0077, rejected): six or more spans per climb is noise on a
// small model, and one number per rung is the whole design.
var fillRungs = [3]int{50, 75, 90}

// fillNoticeLine is the fact line's format (prompts/context-fill-notice.txt): the percent of the
// line, the tokens used and the window, in that order. fillWrapUp is the one sentence the 90
// rung adds for a child agent (prompts/context-fill-wrap-up.txt).
var (
	fillNoticeLine = mustPrompt("context-fill-notice.txt")
	fillWrapUp     = mustPrompt("context-fill-wrap-up.txt")
)

// contextFillNotice is the notice's handler, a domain.PostToolResultFunc. It measures the
// conversation the model is about to see — what the view already holds plus the result this
// firing closes, which appendToolResult has not yet committed — and fires ONCE per rung per
// climb: the highest rung the fill has reached, reporting the actual percent, and nothing again
// until the fill crosses the next rung. A climb ends where the conversation is replaced or
// scrapped: a fold (fold, below), a /clear, a resumed snapshot or an aborted Exchange re-arms the
// whole ladder (rearmFillNotice), so the first result of the new climb fires whichever rung it
// reaches — 50 at 52%, 75 at 80% —
// exactly as a fresh session would at that fill, and no rung is ever marked fired without its
// notice having been given. A zero fill is an unknown window or an uncalibrated ratio, and the
// notice is silent there rather than guessing — the standing posture.
//
// Rung 90 fires once per climb reporting the fill at that moment; later results, and the
// text-only reply that ends the Exchange (no post-tool-result Moment), can carry the history past
// 100 with no further notice — the fold at the next Turn boundary is the trigger's job. The 90
// rung adds the wrap-up sentence for a child agent alone (view.Depth() > 0): a main agent's fold
// waits for the Exchange boundary and its wrap-up is the human's call.
func (a *Agent) contextFillNotice(_ context.Context, view domain.LoopView, _ domain.ToolCall, result *domain.ToolResultEdit) (domain.Outcome, error) {
	chars := domain.ConversationChars(view.Conversation()) + len(result.Content())
	budget := view.Budget()
	fill := budget.HistoryFill(chars)
	if fill <= 0 {
		return domain.Outcome{}, nil
	}

	pct := int(fill * 100)
	reached := rungReached(pct)
	switch {
	case reached < a.fillRung:
		// Every path that shrinks the conversation behind the model's back — a fold, /clear, a
		// restored snapshot, an aborted Exchange — re-arms the whole ladder, so a fill that
		// reads under a fired rung here is the estimate moving — a usage report recalibrated
		// the chars→token ratio — not a shorter history. Re-arm the rungs the reading fell
		// under, silently: the model was told the higher figure already, and a "50" at 74%
		// would only repeat it lower.
		a.fillRung = reached
		return domain.Outcome{}, nil
	case reached == a.fillRung:
		return domain.Outcome{}, nil
	}
	a.fillRung = reached

	window := budget.Window
	if window <= 0 {
		window = budget.ContextLimit // a working window with no advertised one is the only room anyone named
	}
	text := fmt.Sprintf(fillNoticeLine, pct, formatTokens(budget.EstimateTokens(chars)), formatTokens(window))
	if reached == fillRungs[len(fillRungs)-1] && view.Depth() > 0 {
		text += "\n" + fillWrapUp
	}
	return domain.Outcome{
		Inject: text,
		Detail: fmt.Sprintf("rung %d (%d%%)", reached, pct),
	}, nil
}

// rearmFillNotice ends the current climb: every rung is armed again, so the next result the
// notice measures fires whichever rung its fill reaches, as the first result of a session does.
// Called where the conversation the ladder climbed is replaced or scrapped — after a fold that ran
// (fold), on /clear (ClearContext), on a snapshot swapped into a live Agent (RestoreSession) and on
// an aborted Exchange (AbortExchange, which drops the tool result a notice rode on). Without it a
// fold landing the next result at 52% would leave the 50 rung "fired" from the climb the fold
// just erased, and the model would hear nothing until 75.
func (a *Agent) rearmFillNotice() { a.fillRung = 0 }

// rungReached reports the highest rung at or under pct, and 0 when the fill is under the first.
func rungReached(pct int) int {
	reached := 0
	for _, rung := range fillRungs {
		if rung <= pct {
			reached = rung
		}
	}
	return reached
}

// formatTokens renders a token count the way a person reads one: plain under a thousand, one
// decimal of thousands under a hundred thousand (8192 → 8.2k, 32768 → 32.8k), and whole
// thousands from there (131072 → 131k), so a small window's precision survives and a large one's
// noise does not.
func formatTokens(n int) string {
	switch {
	case n < 1000:
		return fmt.Sprintf("%d", n)
	case n < 100000:
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	default:
		return fmt.Sprintf("%dk", (n+500)/1000)
	}
}
