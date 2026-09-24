package agent

import (
	"context"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// completion is the assembled result of consuming one streamed upstream call to its terminal
// Delta — what both the Turn's reply (streamResponse) and the summarizer's call
// (compactCompleter.Complete) read once the stream is drained. content is the VISIBLE text: the
// stripper has already lifted the inline thinking/harmony channel a delimited profile emits out of
// it, and that lifted reasoning is joined (joinThinking) into thinking behind the Upstream-split
// channel. Usage is returned, never recorded: the two callers account for it differently (a Turn
// calibrates the estimator and emits a plain UsageEvent; a summary emits a Maintenance-flagged one),
// and the collector knows neither.
type completion struct {
	content   string
	thinking  string
	toolCalls []provider.ToolCall
	finish    domain.FinishReason
	usage     *provider.Usage // nil when the server omits its accounting
	served    string          // the model id the server answered with; "" when it sent none
	failed    bool            // a terminal DeltaError / DeltaContextOverflow arrived
	overflow  bool            // that terminal fault was DeltaContextOverflow: the PROMPT did not fit, so folding the history can make the same request succeed
	retryable bool            // that terminal fault was TRANSIENT (429 / 5xx / provider_unavailable in-band, or a mid-stream EOF / net timeout): re-sending the same request can succeed
	errMsg    string          // the terminal fault message when failed
}

// collectCompletion is the ONE consumer of the provider's Delta stream: it streams req over the
// Upstream, folds every Delta into a completion, and hands each Delta to observe (nil for a silent
// call) BEFORE folding it, so an observer sees the stream exactly as the wire delivered it — the
// Turn's observer emits the live Token/Reasoning events and the accounting from there; the
// summarizer's observer passes on only the attempt measurement and stays silent in the transcript.
// A DeltaAttempt (one HTTP attempt's measurement, ADR 0085) is handed to observe like any Delta
// and folded into nothing — it never reaches content, tool calls or the completion's accounting —
// and the collector itself emits nothing, so a nil observer is a fully silent call. The body is drained to its terminal
// Delta and closed before this returns — so Approval, consulted afterward in dispatchTools, never
// blocks an open Upstream connection.
//
// A cancellation surfaces as a terminal DeltaError; the CALLER distinguishes it from a real fault
// by checking ctx.Err(), which is why the ctx masquerade check is not here. A prompt the model's
// context window cannot hold surfaces as a terminal DeltaContextOverflow, recorded as failed AND
// overflow so a caller can tell a recoverable request from a generic fault; the provider's
// transient-class verdict (Delta.Retryable) rides out on retryable, because re-streaming is the
// caller's call — only the caller owns the Turn, the fold and the events. An overflow never carries
// it: a prompt too long stays too long.
func (a *Agent) collectCompletion(ctx context.Context, req provider.Request, observe func(provider.Delta)) completion {
	var out completion
	var content, thinking strings.Builder
	for delta := range a.upstream.Stream(ctx, req) {
		if observe != nil {
			observe(delta)
		}
		switch delta.Kind {
		case provider.DeltaContent:
			content.WriteString(delta.Content)
		case provider.DeltaThinking:
			// The native reasoning channel is already separated by the server, so every chunk is
			// reasoning verbatim — no strip, no prefix bookkeeping.
			thinking.WriteString(delta.Thinking)
		case provider.DeltaToolCall:
			if delta.ToolCall != nil {
				out.toolCalls = append(out.toolCalls, *delta.ToolCall)
			}
		case provider.DeltaDone:
			out.finish = domain.FinishReason(delta.FinishReason)
			out.usage = delta.Usage
			out.served = delta.Model
		case provider.DeltaError, provider.DeltaContextOverflow:
			// Both are terminal, but only the overflow says something about the request that the
			// caller can act on: the prompt exceeded the window, so a shorter history is a real
			// remedy. Keep the bit here rather than re-classifying the message later.
			out.failed = true
			out.overflow = delta.Kind == provider.DeltaContextOverflow
			out.errMsg = delta.Err
			out.retryable = delta.Retryable
		}
	}
	// The parse seam's profile split (D5/D6): the inline thinking a delimited profile emits is
	// lifted out of the content, so no <think> span can ride into a committed message or a summary,
	// and what is lifted joins the Upstream-split channel. For a native, no-inline-thinking profile
	// the stripper is a no-op, so content is the wire content verbatim and thinking is the split
	// channel untouched.
	visible, inline := a.stripper.Strip(content.String())
	out.content = visible
	out.thinking = joinThinking(thinking.String(), inline)
	return out
}

// joinThinking combines the Upstream-split reasoning (the `reasoning_content` field or its
// `reasoning` alias) with the reasoning the stripper lifted out of the inline content, Upstream
// first and blank-line joined. Either being empty returns the other unchanged, so a native reply
// with no inline channel returns the Upstream channel untouched (the byte-identical anchor).
func joinThinking(upstream, inline string) string {
	switch {
	case upstream == "":
		return inline
	case inline == "":
		return upstream
	default:
		return upstream + "\n\n" + inline
	}
}
