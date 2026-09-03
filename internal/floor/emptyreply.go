package floor

import (
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

// completionCheckNudge is the correction the retried request carries, ported verbatim from
// apogee-sim's first-attempt nudge (empty_recovery.go @pin) so the guard speaks to the model in the
// wording its A/B measured. Directive nudge rather than a question: small models (7B-14B) respond
// to "Have you completed all parts?" by claiming they're done, even mid-task; telling them to
// review remaining steps produces continuation tool calls (observed with Qwen3.5-9B on multi-file
// creation prompts, 2025-05). The sim's attempt-2 context-aware nudge ladder, system directive, and
// per-attempt temperature escalation are recorded bench-pending divergences (R2), not ported. The
// wording itself is an embedded prompt asset (prompts/*.txt) so it reads and edits as prose.
var completionCheckNudge = mustPrompt("completion-check-nudge.txt")

// RecoverEmpty is the empty-response recovery guard (the `empty-response-recovery` key, ADR 0071):
// when the model returns nothing mid-task — no text and no tool calls — it hands back the
// completion-check nudge the engine re-streams the Turn with, so the model gets a directed second
// chance rather than the empty reply ending the exchange. ok is false for every other response: the
// no-op case.
//
// The decision logic is apogee-sim's empty_recovery @pin, unchanged by the promotion: the guard
// changes only what the model sees after its OWN empty reply, which is why it needs no per-model
// proof and stays on under Bypass. An always-empty model still terminates — the loop's per-Turn
// maxPostResponseRetries is the bound, and past it the empty reply faults the Turn (loop.go
// reviewedOutcome).
func RecoverEmpty(resp *domain.Response) (nudge string, ok bool) {
	if !shouldRecoverEmpty(resp) {
		return "", false
	}
	return completionCheckNudge, true
}

// shouldRecoverEmpty is the pure shape check behind the guard (apogee-sim shouldRecoverEmpty @pin,
// minus the session retry counter). It fires only when the model was given tools, produced neither
// text nor a tool call, is answering a real user message, and has made recent progress — so a model
// spinning uselessly is not endlessly retried, beyond the loop's own attempt cap.
func shouldRecoverEmpty(resp *domain.Response) bool {
	view := resp.View()
	if len(view.Tools()) == 0 {
		return false
	}
	if !isEmptyResponse(resp) {
		return false
	}
	conv := view.Conversation()
	last, _, ok := conv.LastUser()
	if !ok || strings.TrimSpace(last.Content) == "" {
		return false
	}
	return hasRecentProgress(conv)
}

// isEmptyResponse reports whether the model returned nothing actionable — no tool calls and no
// non-whitespace text (apogee-sim isEmptyResponse @pin). This is the boundary with the tool-use
// enforcer guard, which handles the text-present-but-no-tools case.
func isEmptyResponse(resp *domain.Response) bool {
	return len(resp.ToolCalls()) == 0 && strings.TrimSpace(resp.Text()) == ""
}
