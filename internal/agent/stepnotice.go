package agent

import (
	"fmt"
	"math"
)

// The step-budget notice: an ENGINE NOTE, structural at depth ≥ 1 — never a Reaction, so no
// switch, no Reaction id, no firing, and on under Bypass with the clamp and the wrap-up directive.
// Only a delegate carries a step cap (Agent.stepCap), so it speaks to a CHILD agent and a child
// alone: on the tool result that closes the Turn reaching three quarters of the Turns its
// delegation is bounded to, it says how many are spent and how many remain, so the model can
// write its output while it still holds the tools to write it with rather than discovering the
// cap on the wrap-up Turn — tool-less, bar write_file to a spawn-named `output_path`
// (Agent.outputPath). It rides the closing tool result under the engine's own fence
// (Message.WithEngineNote, stepNoticeTopic), never the advice fence: a delegate has been seen
// reading `[advice — reaction …]` on a read_file result as part of the file it read, and the
// fence header is the one thing that tells a structural instruction from a Reaction's advice.

// stepNoticeTopic is the engine-note topic the notice is fenced under on the closing tool result
// — `[engine — step budget]` … `[end engine — step budget]` — and the Topic its ledger row carries.
const stepNoticeTopic = "step budget"

// stepNoticeShare is the share of the step cap the notice fires at: the Turn count reaching
// ceil(stepNoticeShare × cap). Fixed rather than configurable for the fill rungs' reason: one
// number is the whole design, and a second key would be a knob nobody benched.
const stepNoticeShare = 0.75

// stepNoticeLine is the fact line's format (prompts/step-notice.txt): the Turns used, the cap,
// and the Turns left before the wrap-up Turn, in that order.
var stepNoticeLine = mustPrompt("step-notice.txt")

// stepBudgetNotice is the notice's one seam, consulted by appendToolResult for every tool result
// that reaches history: it returns the note's text and true when the result it is about to commit
// is the one the notice rides, and false otherwise. It counts the Turn this result closes —
// turns.exchangeTurns is advanced by Run AFTER step() returns, so at commit of Turn N it reads N-1
// and the Turn under way is one more — and fires when that count has reached the threshold
// (stepNoticeThreshold) while no copy of the note is live in the conversation (stepNoticeLive), so
// it lands ONCE: a Turn with several tool calls commits once per call, and the first result past
// the threshold latches the note against every later result while it survives. The latch is the
// note's own presence, not the Turn: a fold that swallowed the note clears it (rearmStepNotice,
// foldFor) and the next result is told again, because the model no longer holds the line; a prune
// whose stub replaced the noted result clears it the same way (autoPrune, which asks the
// conversation whether the note still stands — Conversation.HasEngineNote — because the ledger
// row can outlive the fence); a cancelled Turn's rollback clears it only when the dropped result
// is the one the note rode (rearmNotices), so a surviving note is never doubled.
//
// Silent at depth 0 — a top-level Agent has no cap, and the main loop is the human's to stop — and
// silent for an unbounded delegation (stepCap 0), where there is no cap to be three quarters of.
func (a *Agent) stepBudgetNotice() (string, bool) {
	if a.depth == 0 || a.stepCap <= 0 || a.stepNoticeLive {
		return "", false
	}
	used := a.turns.exchangeTurns + 1
	if used < stepNoticeThreshold(a.stepCap) {
		return "", false
	}
	a.stepNoticeAt = a.turns.index + 1
	a.stepNoticeLive = true
	return fmt.Sprintf(stepNoticeLine, used, a.stepCap, a.stepCap-used), true
}

// rearmStepNotice forgets the note: the conversation no longer carries it, so the next tool
// result past the threshold is told again. Called after a fold that ran (foldFor), which replaced
// the history the note sat in, after a prune whose stub replaced the noted result (autoPrune), and
// through rearmNotices on the rollback that dropped its result; idempotent, so a Step-driven host
// that cancels the re-attempt too is harmless.
func (a *Agent) rearmStepNotice() {
	a.stepNoticeAt = 0
	a.stepNoticeLive = false
}

// rearmNotices is the one rollback seam both engine notices hang off (construct.go): the
// context-fill ladder ends its climb, and the step-budget notice forgets its note when the
// rolled-back Turn is the one the note rode — the index is not advanced on cancel (turn.go,
// endCancelled), so the Turn under way is still stepNoticeAt's. A cancelled Turn PAST the
// threshold drops its own messages only (DropRange from the Turn's boundary) and keeps the noted
// result, so there the latch stands and the re-attempt's result carries no second copy.
func (a *Agent) rearmNotices() {
	a.rearmFillNotice()
	if a.stepNoticeAt == a.turns.index+1 {
		a.rearmStepNotice()
	}
}

// stepNoticeThreshold is the Turn count the notice fires at for cap: ceil(stepNoticeShare × cap),
// so a cap of 4 warns after Turn 3 and a cap of 80 after Turn 60.
func stepNoticeThreshold(stepCap int) int {
	return int(math.Ceil(stepNoticeShare * float64(stepCap)))
}
