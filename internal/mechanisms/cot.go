package mechanisms

import (
	"context"

	"github.com/airiclenz/apogee/internal/domain"
)

// The Wave-4 completion nudges (Phase-4 item 12): tool_use_directive, stall_nudge, and list_nudge —
// the three tracked Mechanisms apogee-sim's `cot` Transform emits (catalogue C4: `cot` is not a
// fourth Mechanism, its Transform IS these three nudges; Table C records the SPLIT). They are ported
// from apogee-sim internal/cot/cot.go @pin as three plain pre-request proactive-nudge Mechanisms,
// each firing independently on its own condition and injecting one system directive through the
// shared idempotent Request.AppendToSystem. All ship default-off (D1); strikes-3 self-regulation
// routes through the loop's per-Session tracker (item 3), and none is exempt, so all are disabled
// under Bypass (D5).
//
// The nudge "caps" the catalogue records (4-nudge for stall, 3-nudge for list) are stateless
// windows on the read-only turn count, not separate counters: a nudge fires only while readOnlyTurns
// is within [threshold, threshold+maxNudges), so it self-limits from the conversation alone.
//
// stall_nudge ⊥ list_nudge (catalogue Table A: contradictory directives on the same read-only
// surface). In apogee IncompatibleWith is a startup gate (ValidateIncompatibilities, item 2), so at
// most one of the two may be enabled at a time — which subsumes the sim's runtime `&& !wantListNudge`
// preference (both conditions could hold on a mixed "analyze and fix" prompt; the sim let list win,
// apogee makes them mutually exclusive per config, the same collapse C2 applies to the read-loop
// family). Each nudge therefore fires purely on its own condition.
const (
	toolUseDirectiveID domain.MechanismID = "tool_use_directive"
	stallNudgeID       domain.MechanismID = "stall_nudge"
	listNudgeID        domain.MechanismID = "list_nudge"
)

func init() {
	catalogue[toolUseDirectiveID] = newToolUseDirective
	catalogue[stallNudgeID] = newStallNudge
	catalogue[listNudgeID] = newListNudge
}

// cot directives + their idempotency markers, ported verbatim from apogee-sim internal/cot/cot.go
// @pin (behavior ground-truth, D7). Each directive embeds its marker so AppendToSystem's marker
// check makes a repeat inject on the same request a no-op.
const (
	cotToolUseDirective = "You have tools available. When the user asks you to perform an action " +
		"(read, edit, create, run, delete files, etc.), you MUST respond with a tool call. " +
		"Do not describe what you would do — actually do it by calling the appropriate tool. " +
		"Do not re-read files whose content is already visible in the conversation — " +
		"proceed directly to your next action."

	cotStallDirective = "You have been exploring and reading files for several turns " +
		"without making any changes. Now proceed with the required modifications — " +
		"use write_file or edit_file to implement the changes."

	cotListNudgeDirective = "You have listed directory contents but have not read any files yet. " +
		"Use read_file to read the source files you found — do not list more directories."

	cotToolUseMarker   = "MUST respond with a tool call"
	cotStallMarker     = "proceed with the required modifications"
	cotListNudgeMarker = "have not read any files yet"
)

// cot nudge thresholds and caps, mirroring apogee-sim cot.go:32-35 @pin.
const (
	cotStallThreshold     = 4
	cotMaxStallNudges     = 4
	cotMaxListNudges      = 3
	cotListNudgeThreshold = 2
)

// cotReadOnlyTools names the tools whose exclusive use marks a read-only turn (apogee-sim
// readOnlyTools @pin), extended with apogee's own read/exec spellings (open_file, terminal) so the
// stall/list windows advance on apogee's real menu. A turn using only these tools is exploration;
// the first non-read-only tool call ends the read-only streak.
var cotReadOnlyTools = map[string]bool{
	"read_file": true, "readFile": true, "open_file": true,
	"list_files": true, "listFiles": true, "list_dir": true, "listDir": true, "find_files": true,
	"search_files": true, "searchFiles": true, "grep": true, "search": true,
	"execute_command": true, "executeCommand": true, "run_command": true,
	"runCommand": true, "run_terminal_command": true, "terminal": true,
}

// cotReadTools names the file-reading tools countFilesRead and hasFileReadTool count (apogee-sim
// toolsets.ReadTools @pin + apogee's open_file).
var cotReadTools = map[string]bool{
	"read_file": true, "readFile": true, "open_file": true,
}

// toolUseDirectiveMechanism nudges a model that answered an action request with prose to actually
// call a tool — but only before it has ever used one (catalogue Table A: "fires only before first
// tool use").
type toolUseDirectiveMechanism struct{}

func newToolUseDirective(Deps) (domain.Mechanism, error) { return toolUseDirectiveMechanism{}, nil }

func (toolUseDirectiveMechanism) Descriptor() domain.MechanismDescriptor {
	return domain.MechanismDescriptor{
		ID:          toolUseDirectiveID,
		Capability:  domain.CapProactiveNudge,
		Suppression: domain.SuppressStrikesThree,
	}
}

// Ordering declares tool_use_directive Before toolfilter (§Ordering seed, ratified into Table A
// 2026-07-04, review-fixes item 11 / option A): the cot nudges shape the system prompt before
// toolfilter narrows the tool menu, matching the sim's cot → filter Transform order. Rename-proof
// and sim-faithful; the "fires only before first tool use" gate stays a runtime condition in
// PreRequest, not an ordering edge.
func (toolUseDirectiveMechanism) Ordering() domain.OrderingConstraints {
	return domain.OrderingConstraints{Before: []domain.MechanismID{toolFilterID}}
}

// PreRequest injects the tool-use directive when the user asked for an action (not analysis), tools
// are available, and the model has neither written a file nor used any tool yet (apogee-sim
// wantToolUse @pin). It books no fire (Request.Revision, R4) when the condition does not hold or the
// directive is already present.
func (toolUseDirectiveMechanism) PreRequest(_ context.Context, req *domain.Request) error {
	tools := req.View().Tools()
	conv := req.View().Conversation()
	last := cotLastUser(conv)

	if len(tools) > 0 && hasActionIntent(last) && !hasAnalysisIntent(last) &&
		!hasWrittenFiles(conv) && !cotHasEverUsedTools(conv) {
		req.AppendToSystem(cotToolUseMarker, cotToolUseDirective)
	}
	return nil
}

// stallNudgeMechanism nudges a model that has been reading for several turns without writing to
// proceed with the modifications (catalogue Table A: threshold 4, 4-nudge cap).
type stallNudgeMechanism struct{}

func newStallNudge(Deps) (domain.Mechanism, error) { return stallNudgeMechanism{}, nil }

func (stallNudgeMechanism) Descriptor() domain.MechanismDescriptor {
	return domain.MechanismDescriptor{
		ID:               stallNudgeID,
		Capability:       domain.CapProactiveNudge,
		Suppression:      domain.SuppressStrikesThree,
		IncompatibleWith: []domain.MechanismID{listNudgeID},
	}
}

// Ordering declares stall_nudge Before toolfilter (§Ordering seed, ratified into Table A 2026-07-04,
// review-fixes item 11 / option A): the cot nudges shape the system prompt before toolfilter narrows
// the menu. The incompatibility with list_nudge is carried in the descriptor.
func (stallNudgeMechanism) Ordering() domain.OrderingConstraints {
	return domain.OrderingConstraints{Before: []domain.MechanismID{toolFilterID}}
}

// PreRequest injects the stall directive when an action request has stalled — read-only for at least
// the stall threshold of turns (but still within the nudge window), a write tool available
// (apogee-sim wantStallNudge @pin). The sim's `&& !wantListNudge` cross-check is subsumed by the
// startup incompatibility (list_nudge cannot be co-enabled). Books no fire on a no-op.
func (stallNudgeMechanism) PreRequest(_ context.Context, req *domain.Request) error {
	tools := req.View().Tools()
	conv := req.View().Conversation()
	last := cotLastUser(conv)
	readOnlyTurns := cotCountReadOnlyTurns(conv)

	if hasActionIntent(last) && len(tools) > 0 &&
		readOnlyTurns >= cotStallThreshold && readOnlyTurns < cotStallThreshold+cotMaxStallNudges &&
		cotHasWriteTool(tools) {
		req.AppendToSystem(cotStallMarker, cotStallDirective)
	}
	return nil
}

// listNudgeMechanism nudges a model that has listed directories but read nothing to read the files
// it found (catalogue Table A: threshold 2, 3-nudge cap).
type listNudgeMechanism struct{}

func newListNudge(Deps) (domain.Mechanism, error) { return listNudgeMechanism{}, nil }

func (listNudgeMechanism) Descriptor() domain.MechanismDescriptor {
	return domain.MechanismDescriptor{
		ID:               listNudgeID,
		Capability:       domain.CapProactiveNudge,
		Suppression:      domain.SuppressStrikesThree,
		IncompatibleWith: []domain.MechanismID{stallNudgeID},
	}
}

// Ordering declares list_nudge Before toolfilter (§Ordering seed, ratified into Table A 2026-07-04,
// review-fixes item 11 / option A): the cot nudges shape the system prompt before toolfilter narrows
// the menu. The incompatibility with stall_nudge is carried in the descriptor.
func (listNudgeMechanism) Ordering() domain.OrderingConstraints {
	return domain.OrderingConstraints{Before: []domain.MechanismID{toolFilterID}}
}

// PreRequest injects the list nudge when an analysis request has listed directories but read no
// files (within the nudge window), with a read tool available and no file written (apogee-sim
// wantListNudge @pin). Books no fire on a no-op.
func (listNudgeMechanism) PreRequest(_ context.Context, req *domain.Request) error {
	tools := req.View().Tools()
	conv := req.View().Conversation()
	last := cotLastUser(conv)
	readOnlyTurns := cotCountReadOnlyTurns(conv)

	if hasAnalysisIntent(last) && len(tools) > 0 && cotHasFileReadTool(tools) &&
		!hasWrittenFiles(conv) &&
		readOnlyTurns >= cotListNudgeThreshold && readOnlyTurns < cotListNudgeThreshold+cotMaxListNudges &&
		cotCountFilesRead(conv) == 0 && cotHasListedFiles(conv) {
		req.AppendToSystem(cotListNudgeMarker, cotListNudgeDirective)
	}
	return nil
}

// cotLastUser returns the last user message's content, or "" (apogee-sim intent.LastUserMessage @pin,
// via ConversationView.LastUser).
func cotLastUser(conv domain.ConversationView) string {
	if m, _, ok := conv.LastUser(); ok {
		return m.Content
	}
	return ""
}

// cotCountReadOnlyTurns counts consecutive most-recent assistant turns that used tools exclusively
// from the read-only set — the streak ends at the first turn with no tool calls or a non-read-only
// call (apogee-sim countReadOnlyTurns @pin).
func cotCountReadOnlyTurns(conv domain.ConversationView) int {
	count := 0
	for i := conv.Len() - 1; i >= 0; i-- {
		m := conv.At(i)
		if m.Role != domain.RoleAssistant {
			continue
		}
		if len(m.ToolCalls) == 0 {
			break
		}
		allReadOnly := true
		for _, tc := range m.ToolCalls {
			if !cotReadOnlyTools[tc.Tool] {
				allReadOnly = false
				break
			}
		}
		if !allReadOnly {
			break
		}
		count++
	}
	return count
}

// cotCountFilesRead counts the distinct file paths the model has read (apogee-sim countFilesRead
// @pin, over cotReadTools so apogee's open_file counts).
func cotCountFilesRead(conv domain.ConversationView) int {
	seen := make(map[string]bool)
	conv.Range(func(_ int, m domain.Message) bool {
		if m.Role != domain.RoleAssistant {
			return true
		}
		for _, tc := range m.ToolCalls {
			if cotReadTools[tc.Tool] {
				if p := toolCallPath(tc.Arguments); p != "" {
					seen[p] = true
				}
			}
		}
		return true
	})
	return len(seen)
}

// cotHasFileReadTool reports whether the tool menu offers a file-reading tool (apogee-sim
// hasFileReadTool @pin).
func cotHasFileReadTool(tools []domain.ToolDef) bool {
	for _, t := range tools {
		if cotReadTools[t.Name] {
			return true
		}
	}
	return false
}

// cotHasWriteTool reports whether the tool menu offers a file-writing tool (apogee-sim hasWriteTool
// @pin, over wave4WriteTools).
func cotHasWriteTool(tools []domain.ToolDef) bool {
	for _, t := range tools {
		if wave4WriteTools[t.Name] {
			return true
		}
	}
	return false
}

// cotHasListedFiles reports whether the model has ever listed a directory (apogee-sim hasListedFiles
// @pin, over the shared list-tool set).
func cotHasListedFiles(conv domain.ConversationView) bool {
	found := false
	conv.Range(func(_ int, m domain.Message) bool {
		if m.Role != domain.RoleAssistant {
			return true
		}
		for _, tc := range m.ToolCalls {
			if isListTool(tc.Tool) {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

// cotHasEverUsedTools reports whether any assistant message issued a tool call (apogee-sim
// toolsets.HasEverUsedTools @pin).
func cotHasEverUsedTools(conv domain.ConversationView) bool {
	found := false
	conv.Range(func(_ int, m domain.Message) bool {
		if m.Role == domain.RoleAssistant && len(m.ToolCalls) > 0 {
			found = true
			return false
		}
		return true
	})
	return found
}
