package mechanisms

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

// read_repeat registers the redundant-re-read interceptor in the catalogue constructor table
// (Phase-4 item 11, Wave 3 history-aware family). Default-off (D1). It is ported from apogee-sim
// internal/proxy/read_repeat_interceptor.go @pin: when the model's whole response is reads of files
// it already read successfully in a recent Turn, the contents are already in context, so it retries
// in place with a "stop re-reading, proceed" hint.
func init() {
	catalogue[readRepeatID] = newReadRepeat
	descriptors[readRepeatID] = readRepeatDescriptor
}

const readRepeatID domain.MechanismID = "read_repeat"

// readRepeatMechanism is the post-response Mechanism that retries when a response only re-reads
// already-read files (catalogue Table A `read_repeat`). It carries no per-Mechanism state;
// strikes-3 self-regulation routes through the loop's per-Session tracker (item 3).
type readRepeatMechanism struct{}

// newReadRepeat builds the read_repeat Mechanism. It needs no injected Deps (D3): the repeat is
// detected from the response's tool calls and the conversation on its LoopView.
func newReadRepeat(Deps) (domain.Mechanism, error) { return readRepeatMechanism{}, nil }

// readRepeatDescriptor identifies read_repeat as a strikes-3 response-repair Mechanism (catalogue
// Table A), incompatible with cached_content_intercept (the re-read family is pairwise-exclusive,
// C2) — disabled under Bypass (D5), withdrawn after repeated non-help.
var readRepeatDescriptor = domain.MechanismDescriptor{
	ID:               readRepeatID,
	Capability:       domain.CapResponseRepair,
	Suppression:      domain.SuppressStrikesThree,
	IncompatibleWith: []domain.MechanismID{cachedContentInterceptID},
}

// Descriptor returns read_repeat's static catalogue descriptor.
func (readRepeatMechanism) Descriptor() domain.MechanismDescriptor { return readRepeatDescriptor }

// Ordering runs read_repeat BEFORE tool_loop_interceptor and validate in the post-response cascade
// (apogee-sim response_analysis.go:54-94 @pin: the sim checks repeat-reads first, then the
// tool-loop, then validate, so the earliest match wins and short-circuits the rest — read_repeat is
// the HIGHEST priority). This contradicts catalogue Table A's "After validate" cell for read_repeat
// (a Table-A defect surfaced 2026-07-04; see the plan item-11 NOTES); the sim source is the
// behaviour ground truth (D7 as amended), so apogee follows the source's priority order.
func (readRepeatMechanism) Ordering() domain.OrderingConstraints {
	return domain.OrderingConstraints{Before: []domain.MechanismID{toolLoopInterceptorID, validateID}}
}

// PostResponse retries in place with a "you already read these" hint when every requested tool call
// is a read of a file read successfully in a recent Turn (apogee-sim detectRepeatReads +
// retryWithReadRepeatHint @pin). Delivery is ActionRetry{Inject} (R1, the amended C5): the loop
// re-streams the request in the same Turn, appending the superseded read calls and the hint as a
// role-safe user correction (the sim delivered the hint as a tool-role message; apogee uses the
// shared role-safe inject). It is a no-op — booking no fire (the loop keys the acted fire on a
// non-zero Action, R4) — when the response mixes in non-reads, targets unread files, or there is no
// recent successful read.
func (readRepeatMechanism) PostResponse(_ context.Context, resp *domain.Response) (domain.PostResponseDecision, error) {
	conv := resp.View().Conversation()
	repeats := detectRepeatReads(conv, resp.ToolCalls())
	if len(repeats) == 0 {
		return domain.PostResponseDecision{}, nil
	}
	return domain.PostResponseDecision{Action: domain.ActionRetry, Inject: buildReadRepeatHint(repeats, conv)}, nil
}

// detectRepeatReads returns, sorted, the response's read targets that were already read successfully
// in the most recent read Turn (apogee-sim detectRepeatReads @pin). It requires the WHOLE response
// to be reads with paths — a mixed response (a read plus a write) is legitimate progress and does
// not fire.
func detectRepeatReads(conv domain.ConversationView, responseCalls []domain.ToolCall) []string {
	if len(responseCalls) == 0 {
		return nil
	}
	var responsePaths []string
	for _, tc := range responseCalls {
		if !isReadTool(tc.Tool) {
			return nil
		}
		p := toolCallPath(tc.Arguments)
		if p == "" {
			return nil
		}
		responsePaths = append(responsePaths, normalizePath(p))
	}

	recent := recentSuccessfulReadPaths(conv, readToolNames, wave4WriteTools)
	if len(recent) == 0 {
		return nil
	}
	var repeats []string
	for _, p := range responsePaths {
		if recent[p] {
			repeats = append(repeats, p)
		}
	}
	sort.Strings(repeats)
	return repeats
}

// buildReadRepeatHint renders the "already read, proceed" correction (apogee-sim buildReadRepeatHint
// @pin), appending the derived task for orientation when one can be read from the first user message.
func buildReadRepeatHint(paths []string, conv domain.ConversationView) string {
	var b strings.Builder
	if len(paths) == 1 {
		fmt.Fprintf(&b, "You already read %q in a previous turn — its contents are in your conversation context. ", paths[0])
	} else {
		fmt.Fprintf(&b, "You already read these files in previous turns — their contents are in your conversation context: %s. ", strings.Join(paths, ", "))
	}
	b.WriteString("Do not read them again. Proceed with modifications using write_file or edit_file, or read a different file.")
	if task := firstUserTask(conv); task != "" {
		fmt.Fprintf(&b, " Your task: %s", task)
	}
	return b.String()
}

// firstUserTask returns the first user message, capped at 120 chars (apogee-sim firstUserTask @pin).
func firstUserTask(conv domain.ConversationView) string {
	task := firstUserContent(conv)
	if len(task) > 120 {
		return task[:120] + "..."
	}
	return task
}
