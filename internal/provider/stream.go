package provider

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"net/http"
	"strings"
)

// DeltaKind tags a streamed Delta. The set mirrors the TS oracle's CompletionDelta union
// so the loop's stream consumer (P1.2) can switch on it directly.
type DeltaKind string

const (
	// DeltaContent carries a chunk of assistant text.
	DeltaContent DeltaKind = "content"
	// DeltaThinking carries a chunk of the reasoning channel (reasoning_content).
	DeltaThinking DeltaKind = "thinking"
	// DeltaToolCall carries one fully-accumulated tool call (emitted once its argument
	// fragments are joined).
	DeltaToolCall DeltaKind = "tool_call"
	// DeltaDone is the terminal event: the finish reason and (when the server sent it)
	// token usage. Exactly one Done ends a successful stream.
	DeltaDone DeltaKind = "done"
	// DeltaError is a terminal fault (transport, bad status, oversized tool args). No
	// Done follows it.
	DeltaError DeltaKind = "error"
	// DeltaContextOverflow is the terminal "prompt too long" signal (a 400 the server
	// flagged as a context-window rejection).
	DeltaContextOverflow DeltaKind = "context_overflow"
)

// Delta is one event from a streamed completion. Only the fields relevant to Kind are
// populated; the rest are zero.
type Delta struct {
	Kind         DeltaKind
	Content      string
	Thinking     string
	ToolCall     *ToolCall
	FinishReason string
	Usage        *Usage
	Err          string
	// Retryable is meaningful only on DeltaError: it reports that the fault's class is one
	// the client would have retried had it arrived as an HTTP status (429, 5xx, or an
	// aggregator's "provider_unavailable"), so the caller may re-stream the same request.
	// It is never set on DeltaContextOverflow — a prompt too long stays too long — and the
	// provider itself never acts on it, because retrying mid-stream is the loop's call.
	Retryable bool
}

// Stream performs a streaming completion and yields Deltas as they arrive. It is the SSE
// counterpart of Respond: faults and the bad-status path surface as a terminal
// DeltaError / DeltaContextOverflow rather than a Go error, so the consumer drives a
// single range loop (matching the TS AsyncIterable). The HTTP request is issued lazily on
// first iteration; the body and the request context are released when the range ends
// (whether drained or broken early).
func (c *Client) Stream(ctx context.Context, req Request) iter.Seq[Delta] {
	return func(yield func(Delta) bool) {
		req.Stream = true
		wire := c.buildBody(req)
		body, err := json.Marshal(wire)
		if err != nil {
			yield(Delta{Kind: DeltaError, Err: fmt.Sprintf("apogee: marshal request: %v", err)})
			return
		}

		// Streaming is not bounded by a per-attempt timeout — a long generation is not a
		// fault; retries cover only connection/status before the first byte.
		resp, cancel, err := c.send(ctx, body, 0)
		if err != nil {
			yield(Delta{Kind: DeltaError, Err: err.Error()})
			return
		}
		defer cancel()
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			yield(c.statusDelta(resp, len(wire.ChatTemplateKwargs) > 0))
			return
		}
		c.parseSSE(resp.Body, len(wire.ChatTemplateKwargs) > 0, yield)
	}
}

// statusDelta classifies a non-2xx streamed response into a terminal Delta, mirroring
// statusError but on the streaming surface — including its maxErrorBodyBytes read cap and its
// thinking-effort hint: hasTemplateKwargs reports that the failed request carried
// chat_template_kwargs, and a turn is where that failure actually lands, since the loop streams.
func (c *Client) statusDelta(resp *http.Response, hasTemplateKwargs bool) Delta {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
	text := c.sanitize(string(raw))
	if resp.StatusCode == http.StatusBadRequest && isContextOverflow(string(raw)) {
		return Delta{Kind: DeltaContextOverflow, Err: "apogee: context window exceeded: " + text}
	}
	message := fmt.Sprintf("apogee: upstream HTTP %d: %s", resp.StatusCode, text)
	if hasTemplateKwargs {
		message += " " + thinkingEffortHint
	}
	return Delta{Kind: DeltaError, Err: message}
}

// providerUnavailable is the aggregator error_type slug for "the upstream I routed to is
// gone" — a transient class even when it arrives with a 4xx or a non-numeric code.
const providerUnavailable = "provider_unavailable"

// inBandErrorDelta classifies an in-band error member into a terminal Delta, mirroring
// statusDelta but for a failure the server wrapped in an HTTP 200. The text is the whole raw
// SSE payload (sanitised), so provider-specific metadata — OpenRouter's metadata.raw, say —
// reaches the user verbatim instead of being flattened away. Retryable mirrors the client's
// own HTTP retry policy (isRetryableStatus) so an in-band 502 is treated exactly like a 502
// status, with the error_type slug covering the shapes that carry no usable code.
// hasTemplateKwargs appends thinkingEffortHint exactly as statusDelta does — a template
// failure an aggregator wrapped in a 200 needs the same explanation as one that arrived as a
// status — and an overflow stays unhinted, since no thinking effort caused it.
func (c *Client) inBandErrorDelta(werr wireError, raw string, hasTemplateKwargs bool) Delta {
	code := werr.intCode()
	text := fmt.Sprintf("apogee: upstream in-band error %d: %s", code, c.sanitize(raw))
	if code == http.StatusBadRequest && isContextOverflow(werr.Message) {
		return Delta{Kind: DeltaContextOverflow, Err: text}
	}
	if hasTemplateKwargs {
		text += " " + thinkingEffortHint
	}
	retryable := isRetryableStatus(code) || werr.ErrorType == providerUnavailable
	return Delta{Kind: DeltaError, Err: text, Retryable: retryable}
}

// parseSSE reads the SSE body line by line and yields Deltas. It accumulates a tool call
// across argument fragments (flushing on the next call's id or at end), drops a malformed
// event rather than failing the stream, caps accumulated tool-call arguments, and emits a
// terminal Done with the last finish reason and any usage chunk — a faithful port of the
// oracle's parseSSEStream. Returning false from yield (consumer broke) stops cleanly.
// hasTemplateKwargs is carried through from the request Stream built — the in-band error
// delta needs it, and this is the only seam between that request and the error it explains.
func (c *Client) parseSSE(body io.Reader, hasTemplateKwargs bool, yield func(Delta) bool) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), maxToolCallBytes+64*1024)

	var current *ToolCall
	var pendingFinish string
	var pendingUsage *Usage

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") || !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		if data == "[DONE]" {
			if current != nil && !yield(Delta{Kind: DeltaToolCall, ToolCall: current}) {
				return
			}
			finish := pendingFinish
			if finish == "" {
				finish = "stop"
			}
			yield(Delta{Kind: DeltaDone, FinishReason: finish, Usage: pendingUsage})
			return
		}

		var chunk sseChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue // drop a malformed event, matching the oracle
		}
		if chunk.Error != nil {
			// An aggregator can answer HTTP 200 and put the provider's failure in-band. It is
			// terminal, and it must not fall through to the choice-less `continue` below — that
			// path ends at the implicit Done and commits a silent empty reply. A flushed-but-
			// unfinished tool call is dropped with it: the reply is faulted, not partly usable.
			yield(c.inBandErrorDelta(*chunk.Error, data, hasTemplateKwargs))
			return
		}
		if chunk.Usage != nil {
			usage := Usage(*chunk.Usage)
			pendingUsage = &usage
		}
		if len(chunk.Choices) == 0 {
			continue
		}

		choice := chunk.Choices[0]
		if choice.Delta.ReasoningContent != "" && !yield(Delta{Kind: DeltaThinking, Thinking: choice.Delta.ReasoningContent}) {
			return
		}
		if choice.Delta.Content != "" && !yield(Delta{Kind: DeltaContent, Content: choice.Delta.Content}) {
			return
		}
		if stop := c.accumulateToolCalls(choice.Delta.ToolCalls, &current, yield); stop {
			return
		}
		if choice.FinishReason != "" {
			pendingFinish = choice.FinishReason
		}
	}

	if err := scanner.Err(); err != nil {
		yield(Delta{Kind: DeltaError, Err: fmt.Sprintf("apogee: read stream: %v", err)})
		return
	}

	// The stream ended without an explicit [DONE] (server closed the connection): flush
	// any in-progress tool call and emit a terminal Done, as the oracle does.
	if current != nil && !yield(Delta{Kind: DeltaToolCall, ToolCall: current}) {
		return
	}
	yield(Delta{Kind: DeltaDone, FinishReason: "stop"})
}

// accumulateToolCalls folds streamed tool-call fragments into *current: a fragment with
// an id starts a new call (flushing the previous), an id-less fragment appends arguments
// to the open call. It returns true when iteration must stop — either the consumer broke
// (yield returned false on a flush) or the accumulated arguments exceeded the size cap.
func (c *Client) accumulateToolCalls(fragments []sseToolCall, current **ToolCall, yield func(Delta) bool) bool {
	for _, frag := range fragments {
		if frag.ID != "" {
			if *current != nil && !yield(Delta{Kind: DeltaToolCall, ToolCall: *current}) {
				return true
			}
			*current = &ToolCall{
				ID:   frag.ID,
				Type: "function",
				Function: FunctionCall{
					Name:      frag.Function.Name,
					Arguments: frag.Function.Arguments,
				},
			}
			continue
		}
		if *current != nil && frag.Function.Arguments != "" {
			joined := (*current).Function.Arguments + frag.Function.Arguments
			if len(joined) > maxToolCallBytes {
				yield(Delta{Kind: DeltaError, Err: "apogee: tool call arguments exceeded size limit"})
				return true
			}
			(*current).Function.Arguments = joined
		}
	}
	return false
}

// sseChunk is one decoded SSE data event from a streamed completion.
type sseChunk struct {
	Choices []struct {
		Delta struct {
			Content          string        `json:"content"`
			ReasoningContent string        `json:"reasoning_content"`
			ToolCalls        []sseToolCall `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *usageJSON `json:"usage"`
	// Error is the in-band failure member: present only when the server reported an error
	// inside an otherwise-successful stream. Absent on every healthy chunk, so a server that
	// never sends one keeps byte-identical behaviour.
	Error *wireError `json:"error"`
}

// sseToolCall is a tool-call fragment within a streamed delta: the first fragment carries
// the id and (usually) the name, later fragments carry argument continuations.
type sseToolCall struct {
	ID       string `json:"id"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}
