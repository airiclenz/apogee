package mechanisms

import (
	"context"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

// empty_response_recovery registers the empty-reply off-ramp in the catalogue constructor table
// (Phase-4 item 6). Default-off (D1).
func init() { catalogue[emptyResponseRecoveryID] = newEmptyResponseRecovery }

// emptyResponseRecoveryMechanism is the post-response empty-reply off-ramp (catalogue Table A
// `empty_response_recovery`; ported from apogee-sim internal/proxy/empty_recovery.go @pin). When
// the model returns nothing — no text and no tool calls — mid-task, it asks the loop to re-stream
// the request so the model gets another chance rather than the empty reply ending the conversation
// (CONTEXT "Off-ramp"). See offramps.go for why the action is ActionRetry and how the loop's
// maxPostResponseRetries bounds it.
//
// It carries no per-Mechanism state: the retry cap is the loop's, and its off-ramp descriptor keeps
// it out of self-regulation (SuppressExempt) so the guarantee always holds.
type emptyResponseRecoveryMechanism struct{}

// newEmptyResponseRecovery builds the empty_response_recovery Mechanism. It needs no injected Deps
// (D3): the trigger reads only the response and the conversation already on its LoopView.
func newEmptyResponseRecovery(Deps) (domain.Mechanism, error) {
	return emptyResponseRecoveryMechanism{}, nil
}

// Descriptor identifies empty_response_recovery as an off-ramp exempt from suppression (catalogue
// Table A) — it survives Bypass (ADR 0006 / D5) and is never withdrawn by self-regulation.
func (emptyResponseRecoveryMechanism) Descriptor() domain.MechanismDescriptor {
	return domain.MechanismDescriptor{
		ID:          emptyResponseRecoveryID,
		Capability:  domain.CapOffRamp,
		Suppression: domain.SuppressExempt,
	}
}

// Ordering declares no constraints (catalogue Table A: "none — 2-retry cap, per-Turn cooldown"):
// the off-ramp fires on empty replies independently of the response-repair cascade.
func (emptyResponseRecoveryMechanism) Ordering() domain.OrderingConstraints {
	return domain.OrderingConstraints{}
}

// PostResponse asks the loop to re-stream the request when the model returned an empty reply
// mid-task; every other response is a no-op. The trigger mirrors apogee-sim's shouldRecoverEmpty
// @pin: tools were available, the reply is empty, there is a real user request, and the model has
// made recent progress worth recovering (so a model spinning uselessly is not endlessly retried —
// beyond the loop's own attempt cap).
func (emptyResponseRecoveryMechanism) PostResponse(_ context.Context, resp *domain.Response) (domain.PostResponseDecision, error) {
	if !shouldRecoverEmpty(resp) {
		return domain.PostResponseDecision{}, nil
	}
	return domain.PostResponseDecision{Action: domain.ActionRetry}, nil
}

// shouldRecoverEmpty is the pure shape check behind the off-ramp (apogee-sim shouldRecoverEmpty
// @pin, minus the session retry counter — that is the loop's maxPostResponseRetries). It fires only
// when the model was given tools, produced neither text nor a tool call, is answering a real user
// message, and has made recent progress.
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
// non-whitespace text (apogee-sim isEmptyResponse @pin). This is the boundary with the
// tool_use_enforcer off-ramp, which handles the text-present-but-no-tools case.
func isEmptyResponse(resp *domain.Response) bool {
	return len(resp.ToolCalls()) == 0 && strings.TrimSpace(resp.Text()) == ""
}
