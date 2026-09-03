package floor

import (
	"fmt"
	"sort"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

// ToolLoopBreak is the tool-loop breaker guard (the `tool-loop-breaker` key, ADR 0071): when a
// response repeats the exact tool calls of the immediately-previous assistant Turn, it hands back a
// directive naming the repeated tools and steering the model at its remaining work, which the engine
// re-streams the Turn with. ok is false — the no-op case — for a response with no tool calls, or one
// whose calls differ from the previous Turn's.
//
// The comparison is order-independent (computeToolCallKey), so a reordered but otherwise identical
// set of calls still reads as the repeat it is. The current response is not yet in the conversation
// when the guard runs, so the most recent tool-calling assistant message genuinely is the previous
// Turn.
//
// NOTE (2026-07-04, carried from the Mechanism this guard was promoted from): apogee-sim gated
// firing behind a per-Session count (threshold 2) and a 30s wall-clock cooldown (session_state.go
// TryRecordToolLoop @pin). apogee ports the DETECTION alone and drops both: the per-Turn retry bound
// (maxPostResponseRetries) is the only limiter a Floor guard carries (ADR 0071 decision 1), and a
// wall-clock cooldown is meaningless in the deterministic bench.
func ToolLoopBreak(resp *domain.Response) (directive string, ok bool) {
	calls := resp.ToolCalls()
	if len(calls) == 0 {
		return "", false
	}
	conv := resp.View().Conversation()
	prev := previousToolCallKey(conv)
	if prev == "" || prev != computeToolCallKey(calls) {
		return "", false
	}
	return buildToolLoopDirective(conv, calls), true
}

// computeToolCallKey renders an order-independent key for a set of tool calls (apogee-sim
// computeToolCallKey @pin): entries are sorted by name then arguments, so a reordered-but-identical
// set of calls produces the same key.
func computeToolCallKey(calls []domain.ToolCall) string {
	type entry struct{ name, args string }
	entries := make([]entry, len(calls))
	for i, tc := range calls {
		entries[i] = entry{name: tc.Tool, args: string(tc.Arguments)}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].name != entries[j].name {
			return entries[i].name < entries[j].name
		}
		return entries[i].args < entries[j].args
	})
	var b strings.Builder
	for _, e := range entries {
		b.WriteString(e.name)
		b.WriteByte(':')
		b.WriteString(e.args)
		b.WriteByte(';')
	}
	return b.String()
}

// previousToolCallKey returns the key of the most recent assistant Turn that issued tool calls
// (apogee-sim previousToolCallKey @pin), or "" if there is none.
func previousToolCallKey(conv domain.ConversationView) string {
	for i := conv.Len() - 1; i >= 0; i-- {
		m := conv.At(i)
		if m.Role == domain.RoleAssistant && len(m.ToolCalls) > 0 {
			return computeToolCallKey(m.ToolCalls)
		}
	}
	return ""
}

// The directive's fixed fragments, each one asset file (prompts/*.txt) named for its role. The
// five that a further fragment always follows carry the sentence-separating space, appended HERE
// rather than left as invisible trailing whitespace at the end of an asset — the internal/context
// precedent (compact.go's summaryMessagePrefix) — so the wording in the file stays editable prose
// and an editor that trims line ends cannot silently run two sentences together. The three tail
// sentences end the directive and take no separator.
var (
	// toolLoopHeader opens the directive with the repeated tool names (%s).
	toolLoopHeader = mustPrompt("loop-header.txt") + " "
	// toolLoopResultsAbove is the fixed reminder that the repeat bought nothing.
	toolLoopResultsAbove = mustPrompt("results-above.txt") + " "
	// toolLoopTask restates the user's first request (%s), when there is one.
	toolLoopTask = mustPrompt("task-reminder.txt") + " "
	// toolLoopFilesWritten credits the files already written (%s).
	toolLoopFilesWritten = mustPrompt("files-written.txt") + " "
	// toolLoopFilesRead credits the files already read (%s).
	toolLoopFilesRead = mustPrompt("files-read.txt") + " "
	// toolLoopContinueWork tails the wrote-something branch.
	toolLoopContinueWork = mustPrompt("tail-continue-work.txt")
	// toolLoopWriteImplementation tails the read-only branch.
	toolLoopWriteImplementation = mustPrompt("tail-write-implementation.txt")
	// toolLoopDifferentAction tails the branch with no file activity to credit.
	toolLoopDifferentAction = mustPrompt("tail-different-action.txt")
)

// buildToolLoopDirective renders the loop-breaking correction (apogee-sim buildToolLoopDirective
// @pin), naming the repeated tools and steering the model toward its remaining work.
func buildToolLoopDirective(conv domain.ConversationView, calls []domain.ToolCall) string {
	ctx := extractConversationContext(conv)
	names := toolCallNames(calls)

	var b strings.Builder
	fmt.Fprintf(&b, toolLoopHeader, strings.Join(names, ", "))
	b.WriteString(toolLoopResultsAbove)
	if ctx.task != "" {
		fmt.Fprintf(&b, toolLoopTask, ctx.task)
	}
	switch {
	case len(ctx.filesWritten) > 0:
		fmt.Fprintf(&b, toolLoopFilesWritten, strings.Join(ctx.filesWritten, ", "))
		b.WriteString(toolLoopContinueWork)
	case len(ctx.filesRead) > 0:
		fmt.Fprintf(&b, toolLoopFilesRead, strings.Join(ctx.filesRead, ", "))
		b.WriteString(toolLoopWriteImplementation)
	default:
		b.WriteString(toolLoopDifferentAction)
	}
	return b.String()
}

// conversationContext is the task + file activity buildToolLoopDirective steers from.
type conversationContext struct {
	task         string
	filesRead    []string
	filesWritten []string
}

// extractConversationContext gathers the first user task (capped 150) and the distinct files read
// and written, each capped at five (apogee-sim extractConversationContext @pin).
func extractConversationContext(conv domain.ConversationView) conversationContext {
	var ctx conversationContext
	conv.Range(func(_ int, m domain.Message) bool {
		if m.Role == domain.RoleUser && ctx.task == "" && strings.TrimSpace(m.Content) != "" {
			task := m.Content
			if len(task) > 150 {
				task = task[:150] + "..."
			}
			ctx.task = task
		}
		if m.Role != domain.RoleAssistant {
			return true
		}
		for _, tc := range m.ToolCalls {
			p := toolCallPath(tc.Arguments)
			if p == "" {
				continue
			}
			switch {
			case isReadTool(tc.Tool):
				ctx.filesRead = appendUnique(ctx.filesRead, p)
			case isFileMutatingTool(tc.Tool):
				ctx.filesWritten = appendUnique(ctx.filesWritten, p)
			}
		}
		return true
	})
	if len(ctx.filesRead) > 5 {
		ctx.filesRead = ctx.filesRead[:5]
	}
	if len(ctx.filesWritten) > 5 {
		ctx.filesWritten = ctx.filesWritten[:5]
	}
	return ctx
}

// appendUnique appends item to slice if absent, preserving order (apogee-sim appendUnique @pin).
func appendUnique(slice []string, item string) []string {
	for _, s := range slice {
		if s == item {
			return slice
		}
	}
	return append(slice, item)
}

// toolCallNames returns the sorted distinct tool names in calls (apogee-sim toolCallNames @pin).
func toolCallNames(calls []domain.ToolCall) []string {
	set := make(map[string]bool)
	for _, tc := range calls {
		set[tc.Tool] = true
	}
	names := make([]string, 0, len(set))
	for n := range set {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
