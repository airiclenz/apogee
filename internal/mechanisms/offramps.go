package mechanisms

import (
	"encoding/json"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

// The Wave-1 off-ramp Mechanisms (Phase-4 item 6): empty_response_recovery and tool_use_enforcer,
// ported from apogee-sim internal/proxy/{empty_recovery,tooluse_enforcer}.go @pin (catalogue
// Table A). Both are post-response Mechanisms with Capability off-ramp and SuppressionPolicy
// exempt: they run even under Bypass (D5) and ignore Adaptive Suppression and the Turn Budget,
// because without them a failed Turn — an empty reply, or narration where an action was asked for
// — has no way out (CONTEXT "Off-ramp"). Exempt-from-suppression is not exempt-from-validation
// (decision 13): their bench leave-one-out stays pending like everyone else's.
//
// Recovery action — retry-in-place (R1, the owner-ratified amendment of catalogue C5,
// docs/plans/phase-4-review-fixes-plan.md): an ActionRetry{Inject} decision makes the loop
// re-stream the corrected request in the SAME Turn, appending the superseded assistant message
// (when non-empty) and the correction as a role-safe user message — request-scoped, never
// committed to history — bounded by the loop's maxPostResponseRetries (loop.go).
//   - empty_response_recovery returns ActionRetry{Inject: completionCheckNudge} — the sim's
//     first-attempt nudge rides the retried request (empty_recovery.go @pin), giving the model a
//     directed second chance. An always-empty model still terminates: at the cap the empty reply
//     passes through as the Turn's final message. The sim's attempt-2 nudge ladder, its
//     injectSystemDirective, its per-attempt temperature escalation, and its per-session throttle
//     counters (2-cap/cooldown) are recorded bench-pending divergences (R2) — the shared loop cap
//     substitutes for the throttles.
//   - tool_use_enforcer returns ActionRetry{Inject: buildToolUseCorrection(...)} — the retried
//     request carries the superseded narration plus the "use a tool" correction, exactly the
//     sim's retryForToolUse exchange (tooluse_enforcer.go @pin). The narration never commits to
//     history unless the cap passes the final response through; the sim's 3/session enforcer
//     throttle is likewise subsumed by the loop cap (R2).
const (
	emptyResponseRecoveryID domain.MechanismID = "empty_response_recovery"
	toolUseEnforcerID       domain.MechanismID = "tool_use_enforcer"
)

// readToolNames is apogee-sim's read-tool set (internal/toolsets/toolsets.go ReadTools @pin): the
// tools whose calls count as a file read when the off-ramps measure recent progress.
var readToolNames = map[string]bool{
	"read_file": true,
	"readFile":  true,
}

// isReadTool reports whether name is one of the file-reading tools progress detection counts.
func isReadTool(name string) bool { return readToolNames[name] }

// toolCallPath extracts the file path a tool call targets, matching apogee-sim's
// toolsets.ExtractPath @pin (path / file_path / filePath / filename). "" when the arguments are not
// a JSON object or carry no path key — the "no path to count" case progress detection skips.
func toolCallPath(args json.RawMessage) string {
	var m map[string]any
	if json.Unmarshal(args, &m) != nil {
		return ""
	}
	for _, key := range []string{"path", "file_path", "filePath", "filename"} {
		if v, ok := m[key].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// assistantMessageCount counts the assistant messages in the conversation (apogee-sim
// countAssistantMessages @pin) — the enforcer needs at least two before it acts, so a single
// text-only reply on the first Turn is not mistaken for a narration loop.
func assistantMessageCount(conv domain.ConversationView) int {
	count := 0
	conv.Range(func(_ int, m domain.Message) bool {
		if m.Role == domain.RoleAssistant {
			count++
		}
		return true
	})
	return count
}

// wroteRecently reports whether any of the last window assistant messages issued a write-tool call
// (apogee-sim wroteRecently @pin). A model that just wrote a file is making progress, so the
// enforcer stands down.
func wroteRecently(conv domain.ConversationView, window int) bool {
	seen := 0
	for i := conv.Len() - 1; i >= 0 && seen < window; i-- {
		m := conv.At(i)
		if m.Role != domain.RoleAssistant {
			continue
		}
		seen++
		for _, tc := range m.ToolCalls {
			if isWriteTool(tc.Tool) {
				return true
			}
		}
	}
	return false
}

// previousAssistantWasTextOnly reports whether the most recent assistant message was text with no
// tool calls (apogee-sim previousAssistantWasTextOnly @pin) — the enforcer fires only on a SECOND
// consecutive narration, so one stray text reply does not trip it.
func previousAssistantWasTextOnly(conv domain.ConversationView) bool {
	for i := conv.Len() - 1; i >= 0; i-- {
		m := conv.At(i)
		if m.Role == domain.RoleAssistant {
			return len(m.ToolCalls) == 0 && strings.TrimSpace(m.Content) != ""
		}
	}
	return false
}

// hasEverUsedTools reports whether any assistant message issued a tool call (apogee-sim
// toolsets.HasEverUsedTools @pin). Once the model has shown it can call tools, the enforcer stops —
// a later text reply is a considered choice, not an inability to act.
func hasEverUsedTools(conv domain.ConversationView) bool {
	used := false
	conv.Range(func(_ int, m domain.Message) bool {
		if m.Role == domain.RoleAssistant && len(m.ToolCalls) > 0 {
			used = true
			return false
		}
		return true
	})
	return used
}

// hasRecentProgress reports whether the model has made meaningful progress worth recovering an
// empty reply for (apogee-sim hasRecentProgress @pin): early conversations (<=3 assistant turns)
// always qualify (give the model a chance to start), as do any file write or reads of at least two
// distinct paths. A model spinning on the same read makes no progress and is not recovered.
func hasRecentProgress(conv domain.ConversationView) bool {
	assistantCount := 0
	readPaths := make(map[string]bool)
	hasWrites := false

	conv.Range(func(_ int, m domain.Message) bool {
		if m.Role != domain.RoleAssistant {
			return true
		}
		assistantCount++
		for _, tc := range m.ToolCalls {
			if isWriteTool(tc.Tool) {
				hasWrites = true
			}
			if isReadTool(tc.Tool) {
				if p := toolCallPath(tc.Arguments); p != "" {
					readPaths[p] = true
				}
			}
		}
		return true
	})

	switch {
	case assistantCount <= 3:
		return true
	case hasWrites:
		return true
	case len(readPaths) >= 2:
		return true
	default:
		return false
	}
}
