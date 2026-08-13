package mechanisms

import (
	"context"
	"embed"
	"fmt"
	"sort"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

// tool_loop_interceptor registers the identical-repeat-turn detector's catalogue row (Phase-4 item
// 11, Wave 3 history-aware family). Default-off (D1). It was inventory-missed
// and found in the checkout (catalogue Table B): the item title names four members, but the
// catalogue assigns this fifth to item 11 (D7 — the catalogue is authoritative for wave
// composition). It is ported from apogee-sim internal/proxy/tool_loop_interceptor.go @pin: when the
// model's response repeats its previous Turn's exact tool calls, it retries in place with a "you
// are in a loop" directive.
func init() {
	register(row{
		descriptor: toolLoopDescriptor,
		// Ordering runs tool_loop_interceptor before validate (catalogue Table A / apogee-sim
		// response_analysis.go:60-94 @pin: the sim checks the tool loop before validation). read_repeat
		// declares itself before this Mechanism, so the resolved post-response order is
		// read_repeat → tool_loop_interceptor → validate → autofix → syntax — the sim's cascade priority.
		ordering:  domain.OrderingConstraints{Before: []domain.MechanismID{validateID}},
		construct: newToolLoopInterceptor,
	})
}

const toolLoopInterceptorID domain.MechanismID = "tool_loop_interceptor"

// toolLoopMechanism is the post-response Mechanism that retries on an identical-repeat Turn
// (catalogue Table A `tool_loop_interceptor`). It carries no per-Mechanism state; strikes-3
// self-regulation routes through the loop's per-Session tracker (item 3).
type toolLoopMechanism struct{}

// newToolLoopInterceptor builds the tool_loop_interceptor Mechanism. It needs no injected Deps
// (D3): the loop is detected from the response's tool calls and the conversation on its LoopView.
func newToolLoopInterceptor(Deps) (any, error) { return toolLoopMechanism{}, nil }

// toolLoopDescriptor identifies tool_loop_interceptor as a strikes-3 response-repair Mechanism
// (catalogue Table A) — disabled under Bypass (D5), withdrawn after repeated non-help.
var toolLoopDescriptor = domain.MechanismDescriptor{
	ID:          toolLoopInterceptorID,
	Capability:  domain.CapResponseRepair,
	Suppression: domain.SuppressStrikesThree,
}

// PostResponse retries in place with a loop-breaking directive when the response repeats the exact
// tool calls of the immediately-previous assistant Turn (apogee-sim detectToolCallLoop +
// retryWithToolLoopDirective @pin). Delivery is ActionRetry{Inject} (R1): the loop re-streams,
// appending the superseded calls and the directive as a role-safe user correction. It is a no-op —
// booking no fire (the loop keys the acted fire on a non-zero Action, R4) — when the response has no
// tool calls or differs from the previous Turn.
//
// NOTE (2026-07-04): the sim gates firing behind a per-Session ToolLoopCount (threshold 2) and a
// 30s wall-clock cooldown (session_state.go TryRecordToolLoop @pin). apogee ports the DETECTION —
// the isLoop signal, current response == previous Turn's tool-call key — and drops the counter and
// cooldown (R2 precedent, the off-ramps): the loop's strikes-3 self-regulation and
// maxPostResponseRetries substitute for the sim's per-Session throttles, and a wall-clock cooldown
// is meaningless in the deterministic bench.
func (toolLoopMechanism) PostResponse(_ context.Context, resp *domain.Response) (domain.PostResponseDecision, error) {
	calls := resp.ToolCalls()
	if len(calls) == 0 {
		return domain.PostResponseDecision{}, nil
	}
	conv := resp.View().Conversation()
	prev := previousToolCallKey(conv)
	if prev == "" || prev != computeToolCallKey(calls) {
		return domain.PostResponseDecision{}, nil
	}
	return domain.PostResponseDecision{Action: domain.ActionRetry, Inject: buildToolLoopDirective(conv, calls)}, nil
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
// (apogee-sim previousToolCallKey @pin), or "" if there is none. The current response is not yet in
// the conversation (post-response runs against the request view), so this is genuinely the previous
// Turn.
func previousToolCallKey(conv domain.ConversationView) string {
	for i := conv.Len() - 1; i >= 0; i-- {
		m := conv.At(i)
		if m.Role == domain.RoleAssistant && len(m.ToolCalls) > 0 {
			return computeToolCallKey(m.ToolCalls)
		}
	}
	return ""
}

// promptFS carries this package's prompt text as plain files under prompts/. The fixed sentence
// fragments of the loop-breaking directive are assets rather than Go string literals so the
// wording can be read and edited as prose (ISSUES.md: hard-coded prompt literals), and go:embed
// compiles them into the binary — the text ships inside the single binary, is never read from
// disk at runtime, and is never user-overridable. Only the fixed text moved: the branching, the
// `%s` substitution and the joining spaces stay in buildToolLoopDirective below (design call 2).
//
//go:embed prompts/*.txt
var promptFS embed.FS

// mustPrompt loads one embedded prompt asset by file name. Every asset ends with exactly one
// trailing newline — a file without one is awkward in an editor and in a diff — and that one
// newline is stripped here, so the loaded text carries no line ending of its own. CRLF endings
// are normalised first, the way the embedded block art is (internal/tui/logo.go), so a
// core.autocrlf checkout cannot bake \r into a prompt. A name that is not in the FS cannot happen
// in a built binary — go:embed fails the build first — so it is a programming error rather than a
// runtime condition.
func mustPrompt(name string) string {
	b, err := promptFS.ReadFile("prompts/" + name)
	if err != nil {
		panic("apogee: missing embedded prompt asset " + name + ": " + err.Error())
	}
	return strings.TrimSuffix(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
}

// The directive's fixed fragments, each one asset file (prompts/*.txt) named for its role. The
// five that a further fragment always follows carry the sentence-separating space, appended HERE
// rather than left as invisible trailing whitespace at the end of an asset — the internal/context
// precedent (compact.go's summaryMessagePrefix) — so the wording in the file stays editable prose
// and an editor that trims line ends cannot silently run two sentences together. Each var's value
// is byte-identical to the literal it replaced; the three tail sentences end the directive and
// take no separator.
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
