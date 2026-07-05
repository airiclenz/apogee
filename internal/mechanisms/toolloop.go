package mechanisms

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

// tool_loop_interceptor registers the identical-repeat-turn detector in the catalogue constructor
// table (Phase-4 item 11, Wave 3 history-aware family). Default-off (D1). It was inventory-missed
// and found in the checkout (catalogue Table B): the item title names four members, but the
// catalogue assigns this fifth to item 11 (D7 — the catalogue is authoritative for wave
// composition). It is ported from apogee-sim internal/proxy/tool_loop_interceptor.go @pin: when the
// model's response repeats its previous Turn's exact tool calls, it retries in place with a "you
// are in a loop" directive.
func init() {
	catalogue[toolLoopInterceptorID] = newToolLoopInterceptor
	descriptors[toolLoopInterceptorID] = toolLoopDescriptor
}

const toolLoopInterceptorID domain.MechanismID = "tool_loop_interceptor"

// toolLoopMechanism is the post-response Mechanism that retries on an identical-repeat Turn
// (catalogue Table A `tool_loop_interceptor`). It carries no per-Mechanism state; strikes-3
// self-regulation routes through the loop's per-Session tracker (item 3).
type toolLoopMechanism struct{}

// newToolLoopInterceptor builds the tool_loop_interceptor Mechanism. It needs no injected Deps
// (D3): the loop is detected from the response's tool calls and the conversation on its LoopView.
func newToolLoopInterceptor(Deps) (domain.Mechanism, error) { return toolLoopMechanism{}, nil }

// toolLoopDescriptor identifies tool_loop_interceptor as a strikes-3 response-repair Mechanism
// (catalogue Table A) — disabled under Bypass (D5), withdrawn after repeated non-help.
var toolLoopDescriptor = domain.MechanismDescriptor{
	ID:          toolLoopInterceptorID,
	Capability:  domain.CapResponseRepair,
	Suppression: domain.SuppressStrikesThree,
}

// Descriptor returns tool_loop_interceptor's static catalogue descriptor.
func (toolLoopMechanism) Descriptor() domain.MechanismDescriptor { return toolLoopDescriptor }

// Ordering runs tool_loop_interceptor before validate (catalogue Table A / apogee-sim
// response_analysis.go:60-94 @pin: the sim checks the tool loop before validation). read_repeat
// declares itself before this Mechanism, so the resolved post-response order is
// read_repeat → tool_loop_interceptor → validate → autofix → syntax — the sim's cascade priority.
func (toolLoopMechanism) Ordering() domain.OrderingConstraints {
	return domain.OrderingConstraints{Before: []domain.MechanismID{validateID}}
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

// buildToolLoopDirective renders the loop-breaking correction (apogee-sim buildToolLoopDirective
// @pin), naming the repeated tools and steering the model toward its remaining work.
func buildToolLoopDirective(conv domain.ConversationView, calls []domain.ToolCall) string {
	ctx := extractConversationContext(conv)
	names := toolCallNames(calls)

	var b strings.Builder
	fmt.Fprintf(&b, "STOP. You are in a loop — you just called [%s] with identical arguments as your previous turn. ", strings.Join(names, ", "))
	b.WriteString("The results are already in your conversation above. ")
	if ctx.task != "" {
		fmt.Fprintf(&b, "Your task is: %s. ", ctx.task)
	}
	switch {
	case len(ctx.filesWritten) > 0:
		fmt.Fprintf(&b, "You have already written: %s. ", strings.Join(ctx.filesWritten, ", "))
		b.WriteString("Continue with the remaining work or finalize by running tests.")
	case len(ctx.filesRead) > 0:
		fmt.Fprintf(&b, "You have read: %s. ", strings.Join(ctx.filesRead, ", "))
		b.WriteString("You have enough context — now use write_file to create the implementation.")
	default:
		b.WriteString("You MUST take a DIFFERENT action now. Use write_file to make modifications, or read a file you haven't read yet.")
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
