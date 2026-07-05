package mechanisms

import (
	"context"
	"fmt"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

// tool_use_enforcer registers the narration off-ramp in the catalogue constructor table (Phase-4
// item 6). Default-off (D1).
func init() {
	catalogue[toolUseEnforcerID] = newToolUseEnforcer
	descriptors[toolUseEnforcerID] = toolUseEnforcerDescriptor
}

// toolUseEnforcerMechanism is the post-response narration off-ramp (catalogue Table A
// `tool_use_enforcer`; ported from apogee-sim internal/proxy/tooluse_enforcer.go @pin). When the
// user asked for an action but the model answered with prose instead of calling a tool — twice
// running, without ever having used a tool — it retries in place with a correction telling the
// model to act (R1, amending catalogue C5): the loop re-streams the corrected request in the
// same Turn, carrying the superseded narration and the role-safe correction — exactly the sim's
// retryForToolUse exchange (tooluse_enforcer.go @pin). See offramps.go for the delivery
// rationale (CONTEXT "Off-ramp").
//
// It carries no per-Mechanism state: its off-ramp descriptor keeps it out of self-regulation
// (SuppressExempt) so the guarantee always holds, and its trigger already requires two consecutive
// text-only replies, which paces it.
type toolUseEnforcerMechanism struct{}

// newToolUseEnforcer builds the tool_use_enforcer Mechanism. It needs no injected Deps (D3): the
// trigger reads only the response, the tool menu, and the conversation already on its LoopView.
func newToolUseEnforcer(Deps) (domain.Mechanism, error) { return toolUseEnforcerMechanism{}, nil }

// toolUseEnforcerDescriptor identifies tool_use_enforcer as an off-ramp exempt from suppression
// (catalogue Table A) — it survives Bypass (ADR 0006 / D5) and is never withdrawn by self-regulation.
var toolUseEnforcerDescriptor = domain.MechanismDescriptor{
	ID:          toolUseEnforcerID,
	Capability:  domain.CapOffRamp,
	Suppression: domain.SuppressExempt,
}

// Descriptor returns tool_use_enforcer's static catalogue descriptor.
func (toolUseEnforcerMechanism) Descriptor() domain.MechanismDescriptor {
	return toolUseEnforcerDescriptor
}

// Ordering declares no constraints (catalogue Table A: "none — 3-retry cap is its throttle"): the
// off-ramp fires on narration independently of the response-repair cascade.
func (toolUseEnforcerMechanism) Ordering() domain.OrderingConstraints {
	return domain.OrderingConstraints{}
}

// PostResponse retries in place with a "use a tool" correction when the model narrated instead
// of acting — the retried request carries the narration and the correction (R1); every other
// response is a no-op. The trigger mirrors apogee-sim's shouldEnforceToolUse @pin.
func (toolUseEnforcerMechanism) PostResponse(_ context.Context, resp *domain.Response) (domain.PostResponseDecision, error) {
	if !shouldEnforceToolUse(resp) {
		return domain.PostResponseDecision{}, nil
	}
	correction := buildToolUseCorrection(toolNames(resp.View().Tools()))
	return domain.PostResponseDecision{Action: domain.ActionRetry, Inject: correction}, nil
}

// shouldEnforceToolUse is the pure shape check behind the off-ramp (apogee-sim shouldEnforceToolUse
// @pin, minus the session retry counter). It fires only when the model was given tools, replied
// with text and no tool call, the last user message is an action request (not an analysis one), the
// model has not written recently, there have been at least two assistant replies, the previous one
// was also text-only, and the model has never used a tool — the signature of a model narrating a
// task it should be doing.
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
// @pin) so the ported off-ramp speaks to the model in the wording its A/B measured. tools is the
// menu the model was shown, listed so it knows exactly what it may call.
func buildToolUseCorrection(tools []string) string {
	var b strings.Builder
	b.WriteString("You were asked to perform an action but responded with text instead of using a tool.\n")
	fmt.Fprintf(&b, "You MUST use one of the available tools: %s\n", strings.Join(tools, ", "))
	b.WriteString("Respond with a tool call, not a text description.")
	return b.String()
}
