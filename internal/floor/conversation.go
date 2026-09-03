package floor

import (
	"encoding/json"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

// The conversation scanners the Floor guards share. A guard is handed the loop's history as a
// read-only domain.ConversationView and answers one question about it — has the model called a tool
// at all, did it just write a file, is it making progress — so the scans live here once rather than
// once per guard. Every one of them is pure: it reads the view and returns a verdict, never mutating
// anything the loop owns.

// toolCallPath extracts the file path a tool call targets, matching apogee-sim's
// toolsets.ExtractPath @pin (path / file_path / filePath / filename) plus destination — the second
// half of the source/destination pair copy_file and move_file carry (internal/tools/file_ops.go).
// "" when the arguments are not a JSON object or carry no path key — the "no path to count" case
// progress detection skips.
//
// destination is read LAST, so a call carrying one of the four original spellings alongside it
// keeps today's precedence. Reporting the destination — the file the write landed on — matches the
// write-target semantics; the accepted limit is that a move's vacated SOURCE stays invisible to the
// path-keyed guards, cache invalidation included (destination-only, owner call 2026-08-22).
func toolCallPath(args json.RawMessage) string {
	var m map[string]any
	if json.Unmarshal(args, &m) != nil {
		return ""
	}
	for _, key := range []string{"path", "file_path", "filePath", "filename", "destination"} {
		if v, ok := m[key].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// assistantMessageCount counts the assistant messages in the conversation (apogee-sim
// countAssistantMessages @pin) — the tool-use enforcer needs at least two before it acts, so a
// single text-only reply on the first Turn is not mistaken for a narration loop.
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
			if isFileMutatingTool(tc.Tool) {
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
			if isFileMutatingTool(tc.Tool) {
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
