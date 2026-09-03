package floor

import (
	"fmt"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

// EnforceToolUse is the tool-use enforcer guard (the `tool-use-enforcer` key, ADR 0071): when the
// user asked for an action and the model answered with prose instead of calling a tool — twice
// running, without ever having used a tool — it hands back the correction the engine re-streams the
// Turn with, listing the menu the model was shown so it knows exactly what it may call. ok is false
// for every other response: the no-op case, where the prose stands as the model wrote it.
//
// The decision logic is apogee-sim's tooluse_enforcer @pin, unchanged by the promotion: the guard
// changes only what the model sees after its OWN failure to act, which is why it needs no per-model
// proof and stays on under Bypass. It reads nothing but resp — the response, the tool menu, and the
// conversation on its LoopView — so the same Turn always yields the same answer.
func EnforceToolUse(resp *domain.Response) (correction string, ok bool) {
	if !shouldEnforceToolUse(resp) {
		return "", false
	}
	return buildToolUseCorrection(toolNames(resp.View().Tools())), true
}

// shouldEnforceToolUse is the pure shape check behind the guard (apogee-sim shouldEnforceToolUse
// @pin, minus the session retry counter — the loop's per-Turn maxPostResponseRetries is the only
// limiter a Floor guard carries, ADR 0071 decision 1). It fires only when the model was given tools,
// replied with text and no tool call, the last user message is an action request (not an analysis
// one), the model has not written recently, there have been at least two assistant replies, the
// previous one was also text-only, and the model has never used a tool — the signature of a model
// narrating a task it should be doing.
func shouldEnforceToolUse(resp *domain.Response) bool {
	view := resp.View()
	if len(view.Tools()) == 0 {
		return false
	}
	if len(resp.ToolCalls()) > 0 || strings.TrimSpace(resp.Text()) == "" {
		return false
	}

	conv := view.Conversation()
	last, _, ok := conv.LastUser()
	if !ok {
		return false
	}
	if !hasActionIntent(last.Content) || hasAnalysisIntent(last.Content) {
		return false
	}
	if wroteRecently(conv, 2) {
		return false
	}
	if assistantMessageCount(conv) < 2 {
		return false
	}
	if !previousAssistantWasTextOnly(conv) {
		return false
	}
	return !hasEverUsedTools(conv)
}

// buildToolUseCorrection renders the model-facing correction (apogee-sim buildToolUseCorrection
// @pin) so the guard speaks to the model in the wording its A/B measured. tools is the menu the
// model was shown, listed so it knows exactly what it may call.
func buildToolUseCorrection(tools []string) string {
	var b strings.Builder
	b.WriteString("You were asked to perform an action but responded with text instead of using a tool.\n")
	fmt.Fprintf(&b, "You MUST use one of the available tools: %s\n", strings.Join(tools, ", "))
	b.WriteString("Respond with a tool call, not a text description.")
	return b.String()
}
