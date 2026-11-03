package agent

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
//
// The token-budget notice is its twin for the step cap's token sibling (Agent.tokenCap,
// `delegate-max-tokens`): the same fence, the same latch (the note's own
// presence), the same append site, fired once when the child's cumulative prompt tokens reach
// three quarters of the budget. It exists because the two bounds are independent: a child under a wide working
// window keeps the context-fill ladder (fillnotice.go) silent for its whole run while its
// cumulative spend climbs past half the budget unannounced — the 2026-09-18 session reached 10.7M
// of 20M without a word. Neither notice is a rung of that ladder: the ladder measures ONE
// request's fill against the window, these measure the delegation's whole spend against its cap.
//
// Each notice is its bound's row's (boundRows, delegatelimits.go: boundRow.notice), and
// budgetNotices below is the one seam that reads them.

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

// tokenNoticeTopic is the engine-note topic the token-budget notice is fenced under —
// `[engine — token budget]` … `[end engine — token budget]` — and the Topic its ledger row carries.
const tokenNoticeTopic = "token budget"

// tokenNoticeShare is the share of the token cap the token-budget notice fires at: the cumulative
// prompt tokens reaching ceil(tokenNoticeShare × cap). Fixed for stepNoticeShare's reason.
const tokenNoticeShare = 0.75

// tokenNoticeLine is the token notice's fact line format (prompts/token-notice.txt): the tokens
// spent, the cap, and the tokens left before the wrap-up Turn, each rendered by formatTokens.
var tokenNoticeLine = mustPrompt("token-notice.txt")

// budgetNote is one fired budget notice: the engine-note topic it is fenced under and its text.
type budgetNote struct {
	topic string
	text  string
}

// budgetNotices is the budget notices' one seam, consulted by appendToolResult for every tool
// result that reaches history: it returns the notes the result about to be committed carries, in
// bound-row order (boundRows: the step note, then the token note), and none when no notice fires.
// Each bound's notice (boundRow.notice) fires when what the delegate has spent of the bound —
// the Turn this result closes for steps, the cumulative prompt tokens (Agent.usage, the tally the
// token bound itself is enforced against) for tokens — has reached its threshold
// (boundNotice.threshold: stepNoticeThreshold, tokenNoticeThreshold) while no copy of that note
// stands in the conversation, so it lands ONCE: a Turn with several tool calls commits once per
// call, and the first result past the threshold carries the note that keeps every later result
// silent while it survives. The latch IS the note's own presence (Conversation.HasEngineNote,
// which counts a ledger row only while its fence still opens at its offset), so there is no state
// to clear: a fold that swallowed the note (foldFor), a prune whose stub replaced the noted result
// (autoPrune) and a cancelled Turn's rollback that dropped it (turnLifecycle.end) each leave the
// conversation without it, and the next result is told again because the model no longer holds
// the line; a rollback that kept the noted result keeps the note, so a surviving note is never
// doubled. The threshold is checked first, so the scan runs only past it.
//
// Silent at depth 0 — a top-level Agent has no cap, and the main loop is the human's to stop — and
// silent for an unbounded bound (limit 0), where there is no cap to be three quarters of.
func (a *Agent) budgetNotices() []budgetNote {
	if a.depth == 0 {
		return nil
	}
	var notes []budgetNote
	for _, row := range boundRows {
		if text, fired := a.budgetNotice(row.notice); fired {
			notes = append(notes, budgetNote{topic: row.notice.topic, text: text})
		}
	}
	return notes
}

// budgetNotice renders notice's fact line and reports true when it fires on the result about to
// be committed; false for a bound with no notice (nil), an unbounded one, one below its threshold
// and one whose note still stands.
func (a *Agent) budgetNotice(notice *boundNotice) (string, bool) {
	if notice == nil {
		return "", false
	}
	limit := notice.limit(a)
	if limit <= 0 {
		return "", false
	}
	spent := notice.spent(a)
	if spent < notice.threshold(limit) || a.conv.HasEngineNote(notice.topic) {
		return "", false
	}
	return notice.render(spent, limit), true
}

// stepNoticeThreshold is the Turn count the notice fires at for cap: ceil(stepNoticeShare × cap),
// so a cap of 4 warns after Turn 3 and a cap of 80 after Turn 60.
func stepNoticeThreshold(stepCap int) int {
	return boundSteps.row().notice.threshold(stepCap)
}

// tokenNoticeThreshold is the cumulative prompt-token count the token notice fires at for cap:
// ceil(tokenNoticeShare × cap), so the default 20M budget warns at 15M.
func tokenNoticeThreshold(tokenCap int) int {
	return boundTokens.row().notice.threshold(tokenCap)
}
