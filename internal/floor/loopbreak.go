package floor

import (
	"fmt"
	"sort"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

// ToolLoopBreak is the tool-loop breaker guard (the `tool-loop-breaker` key, ADR 0071): when a
// response repeats the exact tool calls of the immediately-previous assistant Turn OF THE CURRENT
// EXCHANGE — or closes an exact A-B-A-B alternation over the Exchange's last four tool Turns — it
// hands back a directive naming the repeated tools and steering the model at its remaining work,
// which the engine re-streams the Turn with. ok is false — the no-op case — for a response with no
// tool calls, or one that neither rule matches.
//
// Two rules, both on byte-identical keys (name + arguments, computeToolCallKey), no fuzzy match:
//
//   - the immediate repeat: key(now) == key(-1), whatever the results were — the model has already
//     seen what that call buys and asked for it again;
//   - the alternation: key(now) == key(-2) && key(-1) == key(-3), AND the Turn pair the alternation
//     repeats drew byte-identical tool results (resultKey of -1 == -3). The result check is what
//     keeps a POLL out of the rule: `console_read` / `read_file` between other calls while a build
//     runs (ADR 0059's design for a dev server) is progress as long as its output moves, and a
//     loop only once it stalls — `sub_agent(noop)` / `task_list` alternating on the same task_list
//     output (review 28b8d620, fourteen calls) is the case the rule exists for. Only the pair's
//     first member has a result at both positions — the current response has not run yet — so
//     that is the one compared; a three-tool-Turn Exchange has no -3 and never fires the rule.
//
// EXCHANGE-SCOPED, and that is the whole guard's boundary (CONTEXT.md: Exchange). A repeat is only
// a loop within the one user request it repeats inside: a human who asks for the same thing again
// — re-running a build, re-reading a file after editing it elsewhere — opens a NEW Exchange whose
// first call may legitimately be byte-identical to the last call of the previous one, and answering
// that with "you are going in circles" steers the model off work the user just asked for. That
// would make the guard worse than no guard, which a Floor guard may never be (ADR 0071 decision 1),
// so both the repeat scan and the directive's own recap read the current Exchange alone. With no
// Exchange open (no opening user message in the view) there is nothing to repeat inside and the
// guard stands down.
//
// The comparison is order-independent (computeToolCallKey), so a reordered but otherwise identical
// set of calls still reads as the repeat it is. The current response is not yet in the conversation
// when the guard runs, so the most recent tool-calling assistant message of the Exchange genuinely
// is the previous Turn.
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
	ex := domain.CurrentExchange(conv)
	if !repeatsToolTurns(exchangeToolTurns(ex), computeToolCallKey(calls)) {
		return "", false
	}
	return buildToolLoopDirective(conv, ex, calls), true
}

// alternationWindow is how many committed tool Turns the A-B-A-B rule reads: the three before the
// current response, which closes the four-Turn window the rule is defined over.
const alternationWindow = 3

// repeatsToolTurns applies the two repeat rules to the Exchange's committed tool Turns (oldest
// first) and the current response's key: the immediate repeat of the last Turn, or the A-B-A-B
// alternation whose repeated first member drew identical results (see ToolLoopBreak).
func repeatsToolTurns(turns []toolTurn, now string) bool {
	n := len(turns)
	if n == 0 {
		return false
	}
	if turns[n-1].callKey == now {
		return true
	}
	if n < alternationWindow {
		return false
	}
	first, second, third := turns[n-3], turns[n-2], turns[n-1]
	return now == second.callKey && first.callKey == third.callKey && first.resultKey == third.resultKey
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

// toolTurn is one tool-calling assistant Turn of the Exchange body as the repeat rules read it: the
// order-independent key of its calls and, keyed the same way, the results those calls drew.
type toolTurn struct {
	callKey   string
	resultKey string
}

// exchangeToolTurns returns the tool Turns of ex in order, oldest first, or nil if there are none
// — which is also what an unopened Exchange yields, RangeAfter being a no-op there. A Turn's
// results are the RoleTool messages that follow its assistant message up to the next assistant
// message, each paired back to the call it answers by ToolCallID and keyed by that call's name and
// arguments plus its content — never by the call ID, which every Turn mints afresh. The walk goes
// forward rather than scanning backwards from the end of the conversation, because the Exchange
// exposes no backward walk and the body is short.
//
// This is where apogee departs from apogee-sim's previousToolCallKey @pin, which scanned the whole
// conversation: the sim had no Exchange, so its scan crossed user requests and drew the directive
// on a legitimate re-ask (fixed 2026-09-03).
func exchangeToolTurns(ex domain.ExchangeView) []toolTurn {
	var turns []toolTurn
	var calls []domain.ToolCall
	var results []domain.Message
	flush := func() {
		if calls == nil {
			return
		}
		turns = append(turns, toolTurn{
			callKey:   computeToolCallKey(calls),
			resultKey: computeToolResultKey(calls, results),
		})
		calls, results = nil, nil
	}
	ex.RangeAfter(func(_ int, m domain.Message) bool {
		switch {
		case m.Role == domain.RoleAssistant && len(m.ToolCalls) > 0:
			flush()
			calls = m.ToolCalls
		case m.Role == domain.RoleAssistant:
			flush()
		case m.Role == domain.RoleTool && calls != nil:
			results = append(results, m)
		}
		return true
	})
	flush()
	return turns
}

// computeToolResultKey renders an order-independent key for the results a Turn's calls drew: each
// result is filed under the name and arguments of the call it answers and sorted with them, so two
// Turns of the same calls compare equal exactly when every call drew byte-identical content. A
// result answering no call of the Turn is skipped.
func computeToolResultKey(calls []domain.ToolCall, results []domain.Message) string {
	callsByID := make(map[string]domain.ToolCall, len(calls))
	for _, tc := range calls {
		callsByID[tc.ID] = tc
	}
	type entry struct{ name, args, content string }
	entries := make([]entry, 0, len(results))
	for _, m := range results {
		tc, ok := callsByID[m.ToolCallID]
		if !ok {
			continue
		}
		entries = append(entries, entry{name: tc.Tool, args: string(tc.Arguments), content: m.Content})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].name != entries[j].name {
			return entries[i].name < entries[j].name
		}
		if entries[i].args != entries[j].args {
			return entries[i].args < entries[j].args
		}
		return entries[i].content < entries[j].content
	})
	var b strings.Builder
	for _, e := range entries {
		b.WriteString(e.name)
		b.WriteByte(':')
		b.WriteString(e.args)
		b.WriteByte('=')
		b.WriteString(e.content)
		b.WriteByte(';')
	}
	return b.String()
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
	// toolLoopTask restates the request the current Exchange opened with (%s), when there is one.
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
// @pin), naming the repeated tools and steering the model toward its remaining work. ex is the
// Exchange the repeat happened inside, conv the view it was derived from — the recap reads both,
// the opening user message for the task and the body for the file activity.
func buildToolLoopDirective(conv domain.ConversationView, ex domain.ExchangeView, calls []domain.ToolCall) string {
	ctx := extractConversationContext(conv, ex)
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

// extractConversationContext gathers the task the current Exchange opened with (capped 150) and the
// distinct files read and written INSIDE it, each capped at five (apogee-sim
// extractConversationContext @pin, re-anchored on the Exchange). The sim recapped the whole
// conversation — the first user message ever sent and every file any Exchange touched — which
// restates a stale task and credits work the current request never asked for; the guard fires
// inside one Exchange, so it recaps that Exchange.
func extractConversationContext(conv domain.ConversationView, ex domain.ExchangeView) conversationContext {
	var ctx conversationContext
	if !ex.Found() {
		return ctx
	}
	if task := conv.At(ex.UserIndex()).Content; strings.TrimSpace(task) != "" {
		if len(task) > 150 {
			task = task[:150] + "..."
		}
		ctx.task = task
	}
	ex.RangeAfter(func(_ int, m domain.Message) bool {
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
