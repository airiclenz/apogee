package provider

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"net/http"
	"sort"
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
	// DeltaToolCall carries one fully-accumulated tool call. Nothing is emitted mid-stream:
	// every call of the reply is yielded, in wire-index order, immediately before the
	// terminal Done, so a server interleaving parallel calls cannot have them mis-joined.
	DeltaToolCall DeltaKind = "tool_call"
	// DeltaDone is the terminal event: the finish reason and (when the server sent it)
	// token usage. Exactly one Done ends a successful stream.
	DeltaDone DeltaKind = "done"
	// DeltaError is a terminal fault (transport, bad status, oversized tool args, a reply
	// past the text cap). No Done follows it.
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
// (whether drained or broken early). The caller's ctx is the stream's only deadline — there
// is no inter-chunk idle timeout — and a cancelled or expired ctx ends the body read and
// surfaces as a terminal DeltaError. Content plus reasoning is capped at maxReplyTextBytes;
// crossing it ends the stream with a non-retryable terminal DeltaError.
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
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			yield(c.statusDelta(resp, wire.carriesEffort()))
			return
		}
		c.parseSSE(resp.Body, wire.carriesEffort(), yield)
	}
}

// statusDelta classifies a non-2xx streamed response into a terminal Delta, mirroring
// statusError but on the streaming surface — including its maxErrorBodyBytes read cap and its
// thinking-effort hint: carriedEffort reports that the failed request expressed a thinking
// effort in some dialect, and a turn is where that failure actually lands, since the loop streams.
func (c *Client) statusDelta(resp *http.Response, carriedEffort bool) Delta {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
	text := c.sanitize(string(raw))
	c.observeWire(WireResponse, []byte(text))
	if resp.StatusCode == http.StatusBadRequest && isContextOverflow(string(raw)) {
		return Delta{Kind: DeltaContextOverflow, Err: "apogee: context window exceeded: " + text}
	}
	message := upstreamStatusText(resp.StatusCode, text, resp.Header.Get("Location"))
	if carriedEffort {
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
// carriedEffort appends thinkingEffortHint exactly as statusDelta does — an effort
// failure an aggregator wrapped in a 200 needs the same explanation as one that arrived as a
// status — and an overflow stays unhinted, since no thinking effort caused it.
func (c *Client) inBandErrorDelta(werr wireError, raw string, carriedEffort bool) Delta {
	code := werr.intCode()
	text := fmt.Sprintf("apogee: upstream in-band error %d: %s", code, c.sanitize(raw))
	if code == http.StatusBadRequest && isContextOverflow(werr.Message) {
		return Delta{Kind: DeltaContextOverflow, Err: text}
	}
	if carriedEffort {
		text += " " + thinkingEffortHint
	}
	retryable := isRetryableStatus(code) || werr.ErrorType == providerUnavailable
	return Delta{Kind: DeltaError, Err: text, Retryable: retryable}
}

// parseSSE reads the SSE body line by line and yields Deltas. It accumulates every tool
// call of the reply across their argument fragments — addressed by wire index, id, or
// last-addressed, and all emitted at the end, never mid-stream — drops a malformed
// event rather than failing the stream, caps accumulated tool-call arguments, caps the total
// content plus reasoning text at maxReplyTextBytes, and emits a terminal Done with the last
// finish reason and any usage chunk — a faithful port of the oracle's parseSSEStream.
// Returning false from yield (consumer broke) stops cleanly.
// carriedEffort is carried through from the request Stream built — the in-band error
// delta needs it, and this is the only seam between that request and the error it explains.
func (c *Client) parseSSE(body io.Reader, carriedEffort bool, yield func(Delta) bool) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), maxToolCallBytes+64*1024)

	// Wire capture, when armed: keep each data: payload and hand the joined stream to the
	// observer once, on whichever return ends this function — [DONE], an in-band error, a
	// read fault, a consumer that broke, or the server closing. Unarmed, not a byte is kept.
	capturing := c.wireObserver != nil
	var captured []string
	if capturing {
		defer func() { c.observeWire(WireResponse, []byte(strings.Join(captured, "\n"))) }()
	}

	var open openToolCalls
	var pendingFinish string
	var pendingUsage *Usage

	// Running total of the content and reasoning bytes yielded so far. Tool-call bytes are
	// not summed here — openToolCalls carries its own maxToolCallBytes cap, on the sum
	// across every call it holds open.
	textBytes := 0

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") || !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if capturing {
			captured = append(captured, data)
		}

		if data == "[DONE]" {
			if !open.flush(yield) {
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
			// path ends at the implicit Done and commits a silent empty reply. Every tool call
			// accumulated so far is dropped with it — none has been emitted, because calls are
			// held until the stream ends: the reply is faulted, not partly usable.
			yield(c.inBandErrorDelta(*chunk.Error, data, carriedEffort))
			return
		}
		if chunk.Usage != nil {
			usage := chunk.Usage.usage()
			pendingUsage = &usage
		}
		if len(chunk.Choices) == 0 {
			continue
		}

		choice := chunk.Choices[0]
		textBytes += len(choice.Delta.Content) + len(choice.Delta.ReasoningContent)
		if textBytes > maxReplyTextBytes {
			// Terminal and NOT Retryable: the same request re-streamed would overflow again.
			// Returning here runs the deferred body close and wire-capture flush, as on every
			// other terminal path; the crossing chunk is never yielded, so what the consumer
			// received stays at or under the cap.
			yield(Delta{
				Kind: DeltaError,
				Err: fmt.Sprintf(
					"apogee: streamed reply exceeded the %d MiB text limit",
					maxReplyTextBytes>>20,
				),
			})
			return
		}
		if choice.Delta.ReasoningContent != "" && !yield(Delta{Kind: DeltaThinking, Thinking: choice.Delta.ReasoningContent}) {
			return
		}
		if choice.Delta.Content != "" && !yield(Delta{Kind: DeltaContent, Content: choice.Delta.Content}) {
			return
		}
		for _, frag := range choice.Delta.ToolCalls {
			if open.fold(frag) {
				yield(Delta{Kind: DeltaError, Err: "apogee: tool call arguments exceeded size limit"})
				return
			}
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
	// every accumulated tool call and emit a terminal Done, as the oracle does.
	if !open.flush(yield) {
		return
	}
	yield(Delta{Kind: DeltaDone, FinishReason: "stop"})
}

// openToolCalls is the ordered set of tool calls one streamed reply has under accumulation.
// A server addresses a call by its wire index when it sends one, by its id when it does not,
// and by "the call addressed last" when it sends neither — so interleaved parallel calls are
// joined by index rather than by arrival order, and a server repeating one id on every
// fragment continues one call instead of splitting it into many. Nothing is emitted until
// flush runs at the end of the stream. The zero value is ready to use.
type openToolCalls struct {
	entries []*openToolCall
	// last is the call a fragment addressed most recently — the target for a fragment that
	// carries neither an index nor an id. nil until the first call opens.
	last *openToolCall
	// bytes is the sum of accumulated argument bytes across every open call, so a server
	// opening many calls cannot multiply maxToolCallBytes by their number.
	bytes int
}

// openToolCall is one call under accumulation with the wire index that addresses it;
// wireIndex is noIndex for a call the server opened without one.
type openToolCall struct {
	call      ToolCall
	wireIndex int
}

// noIndex marks a call opened by a server that sends no wire index. It sorts after every
// indexed call, so those calls keep their arrival order at the end of the flush.
const noIndex = -1

// fold folds one streamed fragment into the set, reporting whether the accumulated argument
// bytes crossed maxToolCallBytes. A fragment addressing nothing — no index, no id, and no
// call yet open — is dropped silently, as it always has been.
func (o *openToolCalls) fold(frag sseToolCall) bool {
	target := o.address(frag)
	if target == nil {
		return false
	}
	o.last = target
	// An id or name arriving on a later fragment fills in on the call it addresses; one
	// arriving on top of a value already accumulated never overwrites it.
	if target.call.ID == "" {
		target.call.ID = frag.ID
	}
	if target.call.Function.Name == "" {
		target.call.Function.Name = frag.Function.Name
	}
	target.call.Function.Arguments += frag.Function.Arguments
	o.bytes += len(frag.Function.Arguments)
	return o.bytes > maxToolCallBytes
}

// address resolves the call a fragment belongs to, opening one where the fragment may. An
// index-bearing fragment carrying neither an id nor a name never opens a call — that is how
// the provider avoids manufacturing a nameless, id-less call for the loop to report.
func (o *openToolCalls) address(frag sseToolCall) *openToolCall {
	if frag.Index != nil {
		if e := o.atIndex(*frag.Index); e != nil {
			return e
		}
		if frag.ID != "" || frag.Function.Name != "" {
			return o.open(*frag.Index)
		}
		return o.last
	}
	if frag.ID != "" {
		if e := o.withID(frag.ID); e != nil {
			return e
		}
		return o.open(noIndex)
	}
	return o.last
}

// atIndex returns the open call at a wire index, or nil when none is open there.
func (o *openToolCalls) atIndex(index int) *openToolCall {
	for _, e := range o.entries {
		if e.wireIndex != noIndex && e.wireIndex == index {
			return e
		}
	}
	return nil
}

// withID returns the open call carrying an id, or nil when none does.
func (o *openToolCalls) withID(id string) *openToolCall {
	for _, e := range o.entries {
		if e.call.ID == id {
			return e
		}
	}
	return nil
}

// open appends a fresh call at a wire index and returns it; fold fills in its id and name.
func (o *openToolCalls) open(wireIndex int) *openToolCall {
	e := &openToolCall{call: ToolCall{Type: "function"}, wireIndex: wireIndex}
	o.entries = append(o.entries, e)
	return e
}

// flush yields every accumulated call — indexed calls in ascending index order, then any
// call opened without an index in arrival order — and reports false when the consumer broke.
// It runs immediately before the terminal Done on both terminal paths, so the
// DeltaToolCall* -> DeltaDone ordering every consumer sees is preserved.
func (o *openToolCalls) flush(yield func(Delta) bool) bool {
	ordered := make([]*openToolCall, len(o.entries))
	copy(ordered, o.entries)
	sort.SliceStable(ordered, func(i, j int) bool {
		a, b := ordered[i], ordered[j]
		if (a.wireIndex == noIndex) != (b.wireIndex == noIndex) {
			return b.wireIndex == noIndex
		}
		if a.wireIndex == noIndex {
			return false
		}
		return a.wireIndex < b.wireIndex
	})
	for _, e := range ordered {
		call := e.call
		if !yield(Delta{Kind: DeltaToolCall, ToolCall: &call}) {
			return false
		}
	}
	return true
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
// the id and (usually) the name, later fragments carry argument continuations. Index is the
// wire index of the call the fragment belongs to; it is a POINTER because index 0 is legal
// and an absent index must not read as one.
type sseToolCall struct {
	ID       string `json:"id"`
	Index    *int   `json:"index"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}
