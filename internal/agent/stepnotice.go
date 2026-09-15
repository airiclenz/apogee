package agent

import (
	"context"
	"fmt"
	"math"

	"github.com/airiclenz/apogee/internal/domain"
)

// The step-budget notice (ADR 0077, 2026-09-15 addendum): the engine's second advise Reaction,
// on the context-fill notice's plumbing byte for byte. At post-tool-result it tells a CHILD agent
// — and a child alone, because only a delegate carries a step cap (Agent.stepCap) — that it has
// spent three quarters of the Turns its delegation is bounded to, once, on the tool result that
// closes the Turn reaching that share, so the model can write its output while it still holds the
// tools to write it with rather than discovering the cap on the tool-less wrap-up Turn. It steers
// rather than corrects, which is why it ships OFF (Config.StepBudgetNotice) and why Bypass switches
// it off with the rest of its class.

// stepBudgetNoticeID is the reaction's id — what its ReactionFiredEvent, its advice fence and its
// `/settings` row are keyed by, and the second non-guard id armReactions reserves.
const stepBudgetNoticeID = "step-budget-notice"

// stepNoticeShare is the share of the step cap the notice fires at: the Turn count reaching
// ceil(stepNoticeShare × cap). Fixed rather than configurable for the fill rungs' reason: one
// number is the whole design, and a second key would be a knob nobody benched.
const stepNoticeShare = 0.75

// stepNoticeLine is the fact line's format (prompts/step-budget-notice.txt): the Turns used, the
// cap, and the Turns left before the wrap-up Turn, in that order.
var stepNoticeLine = mustPrompt("step-budget-notice.txt")

// stepBudgetNotice is the notice's handler, a domain.PostToolResultFunc. It counts the Turn this
// result closes — turns.exchangeTurns is advanced by Run AFTER step() returns, so at
// post-tool-result of Turn N it reads N-1 and the Turn under way is one more — and fires when that
// count first reaches the threshold (stepNoticeThreshold), ONCE: a Turn with several tool calls
// reaches post-tool-result once per call, and stepNoticeAt latches the Turn the notice rode, so
// the second result of that Turn lands bare. Once per Exchange follows from the count: the
// threshold is met by exactly one Turn of an Exchange, and a Turn that CONTINUES an Exchange
// always carries a tool result to ride (a text-only Turn ends it). A cancelled Turn's rollback
// drops the result the notice rode on and re-arms it (rearmStepNotice), as the fill notice's
// ladder is re-armed, so the re-attempt is told again.
//
// Silent at depth 0 — a top-level Agent has no cap, and the main loop is the human's to stop — and
// silent for an unbounded delegation (stepCap 0), where there is no cap to be three quarters of.
func (a *Agent) stepBudgetNotice(_ context.Context, view domain.LoopView, _ domain.ToolCall, _ *domain.ToolResultEdit) (domain.Outcome, error) {
	if view.Depth() == 0 || a.stepCap <= 0 {
		return domain.Outcome{}, nil
	}
	used := a.turns.exchangeTurns + 1
	turn := a.turns.index + 1
	if used != stepNoticeThreshold(a.stepCap) || a.stepNoticeAt == turn {
		return domain.Outcome{}, nil
	}
	a.stepNoticeAt = turn
	return domain.Outcome{
		Inject: fmt.Sprintf(stepNoticeLine, used, a.stepCap, a.stepCap-used),
		Detail: fmt.Sprintf("step %d of %d", used, a.stepCap),
	}, nil
}

// rearmStepNotice forgets the Turn the notice rode, so a re-attempt of that Turn is told again.
// Called on a cancelled Turn's rollback (turnLifecycle.onRollback, through rearmNotices), which
// drops the tool result the notice landed on; idempotent, so a Step-driven host that cancels the
// re-attempt too is harmless.
func (a *Agent) rearmStepNotice() { a.stepNoticeAt = 0 }

// rearmNotices is the one rollback seam both engine notices hang off (construct.go): the
// context-fill ladder ends its climb and the step-budget notice forgets its Turn, for the one
// reason — the rollback dropped the results they rode on.
func (a *Agent) rearmNotices() {
	a.rearmFillNotice()
	a.rearmStepNotice()
}

// stepNoticeThreshold is the Turn count the notice fires at for cap: ceil(stepNoticeShare × cap),
// so a cap of 4 warns after Turn 3 and a cap of 80 after Turn 60.
func stepNoticeThreshold(stepCap int) int {
	return int(math.Ceil(stepNoticeShare * float64(stepCap)))
}
