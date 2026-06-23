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
		body, err := json.Marshal(c.buildBody(req))
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
			yield(c.statusDelta(resp))
			return
		}
		c.parseSSE(resp.Body, yield)
	}
}

// statusDelta classifies a non-2xx streamed response into a terminal Delta, mirroring
// statusError but on the streaming surface.
func (c *Client) statusDelta(resp *http.Response) Delta {
	raw, _ := io.ReadAll(resp.Body)
	text := c.sanitize(string(raw))
	if resp.StatusCode == http.StatusBadRequest && isContextOverflow(string(raw)) {
		return Delta{Kind: DeltaContextOverflow, Err: "apogee: context window exceeded: " + text}
	}
	return Delta{Kind: DeltaError, Err: fmt.Sprintf("apogee: upstream HTTP %d: %s", resp.StatusCode, text)}
}

// parseSSE reads the SSE body line by line and yields Deltas. It accumulates a tool call
// across argument fragments (flushing on the next call's id or at end), drops a malformed
// event rather than failing the stream, caps accumulated tool-call arguments, and emits a
// terminal Done with the last finish reason and any usage chunk — a faithful port of the
// oracle's parseSSEStream. Returning false from yield (consumer broke) stops cleanly.
func (c *Client) parseSSE(body io.Reader, yield func(Delta) bool) {
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
