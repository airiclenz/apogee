package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
	"testing/iotest"
)

// This file pins every way a streamed reply ENDS, on both wires, by the exact Delta sequence a
// consumer sees: the terminator, the server closing without one, a read fault, an in-band
// error, each limit, and a consumer that walks away. One table per ending, one row per wire, and
// the rows where the two wires deliberately differ say so in their names. It drives each
// codec's parseSSE directly over an in-memory body — no upstream, no Client.Stream — so what it
// holds still is the parser's own contract and nothing above it.

// terminalPathRow is one scripted body on one wire and the whole Delta sequence it must yield.
type terminalPathRow struct {
	name string
	wire Wire
	body string
	// readErr, when non-nil, is the fault the body read ends on once body is consumed.
	readErr error
	// stopAt, when positive, makes the consumer return false on its stopAt-th Delta.
	stopAt int
	want   []Delta
}

// The exact fault sentences a stream may end on, spelled out here rather than read from the
// constants the parsers use, so a change to a sentence fails a row.
const (
	terminalToolBytesTripped = "apogee: tool call arguments exceeded size limit"
	terminalMalformedOnlyFmt = "apogee: stream carried no text and no tool calls (%d malformed chunks skipped)"
	terminalInBandPrefix     = "apogee: upstream in-band error"
)

var (
	terminalTextCapTripped        = fmt.Sprintf("apogee: streamed reply exceeded the %d MiB text limit", maxReplyTextBytes>>20)
	terminalToolCountTripped      = fmt.Sprintf("apogee: streamed reply opened more than %d tool calls", maxOpenToolCalls)
	terminalReasoningCountTripped = fmt.Sprintf("apogee: streamed reply opened more than %d reasoning blocks", maxReasoningBlocks)
)

// terminalFillChunk is the size of one fragment of a body built to reach a byte count.
const terminalFillChunk = 64 << 10

// terminalMalformed is a `data:` payload neither wire can decode.
const terminalMalformed = "{not json"

// terminalUsage is what both wires' scripted usage adds up to: 3 prompt and 4 completion tokens.
func terminalUsage() *Usage {
	return &Usage{PromptTokens: 3, CompletionTokens: 4, TotalTokens: 7}
}

// The openai-wire payloads the rows are scripted from.
const (
	openaiUsageChunk = `{"choices":[],"usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7}}`
	openaiInBand502  = `{"error":{"message":"upstream gone","code":502}}`
	openaiOverflow   = `{"error":{"message":"context length exceeded","code":400}}`
)

// The anthropic-wire payloads the rows are scripted from.
const (
	anthropicStopEvent     = `{"type":"message_stop"}`
	anthropicOverloadEvent = `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`
	anthropicOverflowEvent = `{"type":"error","error":{"type":"invalid_request_error","message":"prompt is too long: 9 tokens > 8 maximum"}}`
	anthropicThinkingOpen  = `{"type":"content_block_start","index":%d,"content_block":{"type":"thinking","thinking":""}}`
	anthropicBlockClose    = `{"type":"content_block_stop","index":%d}`
	anthropicRedactedBlock = `{"type":"redacted_thinking","data":"b3BhcXVl"}`
	// anthropicSplitLayout is the layout entry of the reply anthropicSplitReply streams.
	anthropicSplitLayout = `{"reply_layout":[1,-1,1,-2]}`
	// anthropicSplitThinking is the reasoning entry of that reply: a signed block after text.
	anthropicSplitThinking = `{"after_text":true,"block":{"type":"thinking","thinking":"","signature":"czA="}}`
)

// sseData frames each payload as one `data:` line.
func sseData(payloads ...string) string {
	var b strings.Builder
	for _, payload := range payloads {
		b.WriteString("data: " + payload + "\n")
	}
	return b.String()
}

// openaiContent is a chunk carrying one content fragment.
func openaiContent(text string) string {
	return fmt.Sprintf(`{"choices":[{"delta":{"content":%q}}]}`, text)
}

// openaiModelContent is a content chunk naming the model that served it.
func openaiModelContent(model, text string) string {
	return fmt.Sprintf(`{"model":%q,"choices":[{"delta":{"content":%q}}]}`, model, text)
}

// openaiFinish is a chunk carrying only a finish reason.
func openaiFinish(reason string) string {
	return fmt.Sprintf(`{"choices":[{"delta":{},"finish_reason":%q}]}`, reason)
}

// openaiCall is a chunk carrying one tool-call fragment at a wire index.
func openaiCall(index int, id, name, arguments string) string {
	return fmt.Sprintf(
		`{"choices":[{"delta":{"tool_calls":[{"index":%d,"id":%q,"function":{"name":%q,"arguments":%q}}]}}]}`,
		index, id, name, arguments,
	)
}

// anthropicText is a text_delta at block index 0.
func anthropicText(text string) string {
	return anthropicTextAt(0, text)
}

// anthropicTextAt is a text_delta at a block index.
func anthropicTextAt(index int, text string) string {
	return fmt.Sprintf(`{"type":"content_block_delta","index":%d,"delta":{"type":"text_delta","text":%q}}`, index, text)
}

// anthropicThinkingDelta is a thinking_delta at block index 0.
func anthropicThinkingDelta(text string) string {
	return fmt.Sprintf(`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":%q}}`, text)
}

// anthropicSignature is a signature_delta at a block index.
func anthropicSignature(index int, signature string) string {
	return fmt.Sprintf(`{"type":"content_block_delta","index":%d,"delta":{"type":"signature_delta","signature":%q}}`, index, signature)
}

// anthropicCallStart opens a tool_use block at a block index.
func anthropicCallStart(index int, id, name string) string {
	return fmt.Sprintf(
		`{"type":"content_block_start","index":%d,"content_block":{"type":"tool_use","id":%q,"name":%q,"input":{}}}`,
		index, id, name,
	)
}

// anthropicCallInput is an input_json_delta for the tool_use block at a block index.
func anthropicCallInput(index int, partial string) string {
	return fmt.Sprintf(`{"type":"content_block_delta","index":%d,"delta":{"type":"input_json_delta","partial_json":%q}}`, index, partial)
}

// anthropicMessageStart is a message_start naming the served model and 3 input tokens.
func anthropicMessageStart(model string) string {
	return fmt.Sprintf(`{"type":"message_start","message":{"model":%q,"usage":{"input_tokens":3}}}`, model)
}

// anthropicMessageDelta is a message_delta carrying a stop reason and 4 output tokens.
func anthropicMessageDelta(stopReason string) string {
	return fmt.Sprintf(`{"type":"message_delta","delta":{"stop_reason":%q},"usage":{"output_tokens":4}}`, stopReason)
}

// anthropicSplitReply is the payloads of a reply whose text sits either side of a signed thinking
// block and ahead of one input-less tool call: the shape that needs a layout entry.
func anthropicSplitReply() []string {
	return []string{
		anthropicTextAt(0, "a"),
		fmt.Sprintf(anthropicThinkingOpen, 1),
		anthropicSignature(1, "czA="),
		fmt.Sprintf(anthropicBlockClose, 1),
		anthropicTextAt(2, "b"),
		anthropicCallStart(3, "toolu_1", "ls"),
	}
}

// terminalFill is n bytes of text as fragments of at most terminalFillChunk.
func terminalFill(n int) []string {
	var chunks []string
	for left := n; left > 0; left -= terminalFillChunk {
		chunks = append(chunks, strings.Repeat("a", min(left, terminalFillChunk)))
	}
	return chunks
}

// terminalFillBody is n bytes of reply text streamed on wire, one fragment per payload.
func terminalFillBody(wire Wire, n int) string {
	var payloads []string
	for _, chunk := range terminalFill(n) {
		if wire == WireAnthropic {
			payloads = append(payloads, anthropicText(chunk))
			continue
		}
		payloads = append(payloads, openaiContent(chunk))
	}
	return sseData(payloads...)
}

// terminalFillDeltas is the content Deltas terminalFillBody yields, followed by tail.
func terminalFillDeltas(n int, tail ...Delta) []Delta {
	var deltas []Delta
	for _, chunk := range terminalFill(n) {
		deltas = append(deltas, Delta{Kind: DeltaContent, Content: chunk})
	}
	return append(deltas, tail...)
}

// terminalLocalFault is the terminal Delta of a fault apogee raised itself: no Retryable, no
// MalformedChunks.
func terminalLocalFault(text string) Delta {
	return Delta{Kind: DeltaError, Err: text, Fault: UpstreamFault{Class: FaultLocal}}
}

// terminalMalformedOnly is the fault of a stream that carried nothing but count skipped chunks.
func terminalMalformedOnly(count int) Delta {
	return Delta{
		Kind:            DeltaError,
		Err:             fmt.Sprintf(terminalMalformedOnlyFmt, count),
		MalformedChunks: count,
		Fault:           UpstreamFault{Class: FaultLocal},
	}
}

// terminalCall is the Delta of one flushed tool call.
func terminalCall(id, name, arguments string) Delta {
	return Delta{
		Kind:     DeltaToolCall,
		ToolCall: &ToolCall{ID: id, Type: "function", Function: FunctionCall{Name: name, Arguments: arguments}},
	}
}

// terminalTimeout is a network timeout as a body read reports one.
type terminalTimeout struct{}

func (terminalTimeout) Error() string   { return "i/o timeout" }
func (terminalTimeout) Timeout() bool   { return true }
func (terminalTimeout) Temporary() bool { return true }

// parseOpenAITerminal runs the openai codec's parser over body, handing every Delta to yield.
func parseOpenAITerminal(body io.Reader, yield func(Delta) bool) {
	client := NewClient("http://unused.invalid", "m")
	client.codec.parseSSE(body, sentRequest{}, nil, yield)
}

// parseAnthropicTerminal runs the anthropic codec's parser over body, handing every Delta to
// yield.
func parseAnthropicTerminal(body io.Reader, yield func(Delta) bool) {
	client := NewClient("http://unused.invalid", "m", WithWire(WireAnthropic))
	client.codec.parseSSE(body, sentRequest{}, nil, yield)
}

// terminalPathDeltas is every Delta the row's wire yields for its body: the body is followed by
// the row's read fault when it has one, and the consumer stops listening at the row's stopAt-th
// Delta when it has one. A Delta yielded after the consumer said stop is collected too, so it
// shows as a difference.
func terminalPathDeltas(t *testing.T, row terminalPathRow) []Delta {
	t.Helper()
	if row.wire == WireAnthropic && row.readErr == nil && row.stopAt == 0 {
		return parseAnthropicSSE(t, row.body)
	}
	var body io.Reader = strings.NewReader(row.body)
	if row.readErr != nil {
		body = io.MultiReader(body, iotest.ErrReader(row.readErr))
	}
	var deltas []Delta
	yield := func(d Delta) bool {
		deltas = append(deltas, d)
		return len(deltas) != row.stopAt
	}
	if row.wire == WireAnthropic {
		parseAnthropicTerminal(body, yield)
	} else {
		parseOpenAITerminal(body, yield)
	}
	return deltas
}

// abridgeDeltas is deltas with every long text replaced by its length, so a failed row of a cap
// table prints a readable diff instead of megabytes.
func abridgeDeltas(deltas []Delta) []Delta {
	const longest = 96
	short := func(s string) string {
		if len(s) <= longest {
			return s
		}
		return fmt.Sprintf("<%d bytes>", len(s))
	}
	out := make([]Delta, len(deltas))
	for i, d := range deltas {
		d.Content, d.Thinking = short(d.Content), short(d.Thinking)
		if d.ToolCall != nil {
			call := *d.ToolCall
			call.Function.Arguments = short(call.Function.Arguments)
			d.ToolCall = &call
		}
		out[i] = d
	}
	return out
}

// runTerminalPaths checks every row's whole Delta sequence. parallel is false for the tables
// whose bodies run to megabytes, so they are held in memory one at a time.
func runTerminalPaths(t *testing.T, parallel bool, rows []terminalPathRow) {
	t.Helper()
	for _, row := range rows {
		t.Run(fmt.Sprintf("%s: %s", row.wire, row.name), func(t *testing.T) {
			if parallel {
				t.Parallel()
			}

			got := terminalPathDeltas(t, row)

			if !reflect.DeepEqual(got, row.want) {
				t.Errorf("deltas =\n%s\nwant\n%s", dumpDeltas(abridgeDeltas(got)), dumpDeltas(abridgeDeltas(row.want)))
			}
		})
	}
}

// TestTerminalPaths_Terminator pins the clean end: the wire's terminator closes the reply with
// one Done carrying the last finish reason, the usage and the served model, and nothing after
// the terminator is read.
func TestTerminalPaths_Terminator(t *testing.T) {
	t.Parallel()

	runTerminalPaths(t, true, []terminalPathRow{
		{
			name: "the terminator ends the reply and what follows it is not read",
			wire: WireOpenAI,
			body: sseData(
				openaiModelContent("gpt-x", "hi"),
				openaiFinish("length"),
				openaiUsageChunk,
				sseDone,
				openaiContent("late"),
			),
			want: []Delta{
				{Kind: DeltaContent, Content: "hi"},
				{Kind: DeltaDone, FinishReason: "length", Usage: terminalUsage(), Model: "gpt-x"},
			},
		},
		{
			name: "the terminator ends the reply and what follows it is not read",
			wire: WireAnthropic,
			body: sseData(
				anthropicMessageStart("claude-x"),
				anthropicText("hi"),
				anthropicMessageDelta("max_tokens"),
				anthropicStopEvent,
				anthropicText("late"),
			),
			want: []Delta{
				{Kind: DeltaContent, Content: "hi"},
				{Kind: DeltaDone, FinishReason: "length", Usage: terminalUsage(), Model: "claude-x"},
			},
		},
		{
			name: "a terminator alone is an empty reply that stopped",
			wire: WireOpenAI,
			body: sseData(sseDone),
			want: []Delta{{Kind: DeltaDone, FinishReason: "stop"}},
		},
		{
			name: "a terminator alone is an empty reply that stopped",
			wire: WireAnthropic,
			body: sseData(anthropicStopEvent),
			want: []Delta{{Kind: DeltaDone, FinishReason: "stop"}},
		},
		{
			name: "framing lines and comments carry nothing",
			wire: WireOpenAI,
			body: ": keep-alive\n\nevent: chunk\n" + sseData(openaiContent("hi")) + "\n" + sseData(sseDone),
			want: []Delta{{Kind: DeltaContent, Content: "hi"}, {Kind: DeltaDone, FinishReason: "stop"}},
		},
		{
			name: "framing lines and comments carry nothing",
			wire: WireAnthropic,
			body: ": keep-alive\n\nevent: content_block_delta\n" + sseData(anthropicText("hi")) + "\n" + sseData(anthropicStopEvent),
			want: []Delta{{Kind: DeltaContent, Content: "hi"}, {Kind: DeltaDone, FinishReason: "stop"}},
		},
	})
}

// TestTerminalPaths_BareEOF pins the server closing without its terminator: the reply ends on
// what it sent — the finish reason and usage already received are kept, and every open tool
// call is flushed before the Done.
func TestTerminalPaths_BareEOF(t *testing.T) {
	t.Parallel()

	runTerminalPaths(t, true, []terminalPathRow{
		{
			name: "a pending finish reason and usage are kept",
			wire: WireOpenAI,
			body: sseData(openaiModelContent("gpt-x", "cut"), openaiFinish("length"), openaiUsageChunk),
			want: []Delta{
				{Kind: DeltaContent, Content: "cut"},
				{Kind: DeltaDone, FinishReason: "length", Usage: terminalUsage(), Model: "gpt-x"},
			},
		},
		{
			name: "a pending finish reason and usage are kept",
			wire: WireAnthropic,
			body: sseData(anthropicMessageStart("claude-x"), anthropicText("cut"), anthropicMessageDelta("max_tokens")),
			want: []Delta{
				{Kind: DeltaContent, Content: "cut"},
				{Kind: DeltaDone, FinishReason: "length", Usage: terminalUsage(), Model: "claude-x"},
			},
		},
		{
			name: "open tool calls are flushed in index order before the Done",
			wire: WireOpenAI,
			body: sseData(
				openaiCall(1, "call_b", "read", `{"p":`),
				openaiCall(0, "call_a", "ls", `{}`),
				openaiCall(1, "", "", `1}`),
			),
			want: []Delta{
				terminalCall("call_a", "ls", `{}`),
				terminalCall("call_b", "read", `{"p":1}`),
				{Kind: DeltaDone, FinishReason: "stop"},
			},
		},
		{
			name: "open tool calls are flushed in index order before the Done",
			wire: WireAnthropic,
			body: sseData(
				anthropicCallStart(1, "toolu_b", "read"),
				anthropicCallInput(1, `{"p":`),
				anthropicCallStart(0, "toolu_a", "ls"),
				anthropicCallInput(0, `{}`),
				anthropicCallInput(1, `1}`),
			),
			want: []Delta{
				terminalCall("toolu_a", "ls", `{}`),
				terminalCall("toolu_b", "read", `{"p":1}`),
				{Kind: DeltaDone, FinishReason: "stop"},
			},
		},
		{
			name: "an empty body is an empty reply that stopped",
			wire: WireOpenAI,
			want: []Delta{{Kind: DeltaDone, FinishReason: "stop"}},
		},
		{
			name: "an empty body is an empty reply that stopped",
			wire: WireAnthropic,
			want: []Delta{{Kind: DeltaDone, FinishReason: "stop"}},
		},
	})
}

// TestTerminalPaths_MalformedChunks pins the skipped-chunk count: a stream that carried nothing
// but chunks it could not decode is faulted on the count at either end, metadata and empty
// fragments do not count as carrying something, and a stream that did carry something reports
// the count on its Done.
func TestTerminalPaths_MalformedChunks(t *testing.T) {
	t.Parallel()

	runTerminalPaths(t, true, []terminalPathRow{
		{
			name: "only malformed chunks at the terminator is a fault, whatever metadata came",
			wire: WireOpenAI,
			body: sseData(terminalMalformed, `{"model":"gpt-x","choices":[]}`, openaiUsageChunk, openaiFinish("stop"), terminalMalformed, sseDone),
			want: []Delta{terminalMalformedOnly(2)},
		},
		{
			name: "only malformed chunks at the terminator is a fault, whatever metadata came",
			wire: WireAnthropic,
			body: sseData(terminalMalformed, anthropicMessageStart("claude-x"), anthropicMessageDelta("end_turn"), terminalMalformed, anthropicStopEvent),
			want: []Delta{terminalMalformedOnly(2)},
		},
		{
			name: "only malformed chunks at a bare EOF is a fault",
			wire: WireOpenAI,
			body: sseData(terminalMalformed),
			want: []Delta{terminalMalformedOnly(1)},
		},
		{
			name: "only malformed chunks at a bare EOF is a fault",
			wire: WireAnthropic,
			body: sseData(terminalMalformed),
			want: []Delta{terminalMalformedOnly(1)},
		},
		{
			name: "a malformed chunk then content is counted on the Done",
			wire: WireOpenAI,
			body: sseData(terminalMalformed, openaiContent("x"), sseDone),
			want: []Delta{{Kind: DeltaContent, Content: "x"}, {Kind: DeltaDone, FinishReason: "stop", MalformedChunks: 1}},
		},
		{
			name: "a malformed chunk then content is counted on the Done",
			wire: WireAnthropic,
			body: sseData(terminalMalformed, anthropicText("x"), anthropicStopEvent),
			want: []Delta{{Kind: DeltaContent, Content: "x"}, {Kind: DeltaDone, FinishReason: "stop", MalformedChunks: 1}},
		},
		{
			name: "a malformed chunk then only a tool call is counted on the Done",
			wire: WireOpenAI,
			body: sseData(terminalMalformed, openaiCall(0, "call_a", "ls", ""), sseDone),
			want: []Delta{terminalCall("call_a", "ls", ""), {Kind: DeltaDone, FinishReason: "stop", MalformedChunks: 1}},
		},
		{
			name: "a malformed chunk then only a tool call is counted on the Done",
			wire: WireAnthropic,
			body: sseData(terminalMalformed, anthropicCallStart(0, "toolu_a", "ls"), anthropicStopEvent),
			want: []Delta{terminalCall("toolu_a", "ls", "{}"), {Kind: DeltaDone, FinishReason: "stop", MalformedChunks: 1}},
		},
		{
			name: "an empty fragment yields nothing and carries nothing",
			wire: WireOpenAI,
			body: sseData(terminalMalformed, `{"choices":[{"delta":{"content":"","reasoning_content":""}}]}`, sseDone),
			want: []Delta{terminalMalformedOnly(1)},
		},
		{
			name: "an empty fragment yields nothing and carries nothing",
			wire: WireAnthropic,
			body: sseData(terminalMalformed, anthropicText(""), anthropicThinkingDelta(""), anthropicStopEvent),
			want: []Delta{terminalMalformedOnly(1)},
		},
		{
			name: "an empty fragment between real ones yields nothing",
			wire: WireAnthropic,
			body: sseData(anthropicText(""), anthropicThinkingDelta(""), anthropicText("x"), anthropicStopEvent),
			want: []Delta{{Kind: DeltaContent, Content: "x"}, {Kind: DeltaDone, FinishReason: "stop"}},
		},
		{
			name: "an error event with no error member is a malformed chunk (D4)",
			wire: WireAnthropic,
			body: sseData(`{"type":"error"}`, anthropicStopEvent),
			want: []Delta{terminalMalformedOnly(1)},
		},
		{
			name: "an error event with no error member does not end the stream (D4)",
			wire: WireAnthropic,
			body: sseData(`{"type":"error"}`, anthropicText("x"), anthropicStopEvent),
			want: []Delta{{Kind: DeltaContent, Content: "x"}, {Kind: DeltaDone, FinishReason: "stop", MalformedChunks: 1}},
		},
	})
}

// TestTerminalPaths_ReadFault pins a body read that fails before the terminator: what was
// yielded stays, the open tool call is dropped, and the one terminal fault carries the read
// error's text, the note and count of skipped chunks, its Retryable verdict and its class.
func TestTerminalPaths_ReadFault(t *testing.T) {
	t.Parallel()

	faults := []struct {
		name      string
		malformed bool
		err       error
		want      Delta
	}{
		{
			name:      "a mid-stream EOF is retryable and names the skipped chunks",
			malformed: true,
			err:       io.ErrUnexpectedEOF,
			want: Delta{
				Kind:            DeltaError,
				Err:             "apogee: read stream: unexpected EOF (1 malformed chunks skipped)",
				Retryable:       true,
				MalformedChunks: 1,
				Fault:           UpstreamFault{Class: FaultCut},
			},
		},
		{
			name: "the idle cut is a retryable timeout",
			err:  errStreamIdle,
			want: Delta{
				Kind:      DeltaError,
				Err:       "apogee: read stream: upstream stream idle",
				Retryable: true,
				Fault:     UpstreamFault{Class: FaultTimeout},
			},
		},
		{
			name: "a network timeout is a retryable timeout",
			err:  terminalTimeout{},
			want: Delta{
				Kind:      DeltaError,
				Err:       "apogee: read stream: i/o timeout",
				Retryable: true,
				Fault:     UpstreamFault{Class: FaultTimeout},
			},
		},
		{
			name: "a reset is a cut that is not retryable",
			err:  errors.New("connection reset by peer"),
			want: Delta{
				Kind:  DeltaError,
				Err:   "apogee: read stream: connection reset by peer",
				Fault: UpstreamFault{Class: FaultCut},
			},
		},
		{
			name: "the caller's cancel is its own class",
			err:  context.Canceled,
			want: Delta{
				Kind:  DeltaError,
				Err:   "apogee: read stream: context canceled",
				Fault: UpstreamFault{Class: FaultCancelled},
			},
		},
	}
	bodies := map[Wire][]string{
		WireOpenAI:    {openaiContent("hi"), openaiCall(0, "call_a", "ls", "{}"), openaiFinish("tool_calls"), openaiUsageChunk},
		WireAnthropic: {anthropicText("hi"), anthropicCallStart(1, "toolu_a", "ls"), anthropicMessageDelta("tool_use")},
	}
	overlong := "data: " + strings.Repeat("a", maxToolCallBytes+2*terminalFillChunk)
	var rows []terminalPathRow
	for _, wire := range []Wire{WireOpenAI, WireAnthropic} {
		for _, fault := range faults {
			payloads := bodies[wire]
			if fault.malformed {
				payloads = append([]string{terminalMalformed}, payloads...)
			}
			rows = append(rows, terminalPathRow{
				name:    fault.name,
				wire:    wire,
				body:    sseData(payloads...),
				readErr: fault.err,
				want:    []Delta{{Kind: DeltaContent, Content: "hi"}, fault.want},
			})
		}
		rows = append(rows, terminalPathRow{
			name: "a line past the scanner's buffer is apogee's own limit",
			wire: wire,
			body: sseData(bodies[wire][0]) + overlong,
			want: []Delta{
				{Kind: DeltaContent, Content: "hi"},
				terminalLocalFault("apogee: read stream: bufio.Scanner: token too long"),
			},
		})
	}

	runTerminalPaths(t, true, rows)
}

// TestTerminalPaths_InBandError pins the failure a server frames inside its 200 stream: it is
// terminal, every tool call accumulated so far is dropped with it, no Done follows, and it is
// stamped with the count of chunks skipped before it — the context overflow included.
func TestTerminalPaths_InBandError(t *testing.T) {
	t.Parallel()

	runTerminalPaths(t, true, []terminalPathRow{
		{
			name: "an in-band error drops the open calls and carries the skipped count",
			wire: WireOpenAI,
			body: sseData(terminalMalformed, openaiContent("partial"), openaiCall(0, "call_a", "ls", "{}"), openaiInBand502, sseDone),
			want: []Delta{
				{Kind: DeltaContent, Content: "partial"},
				{
					Kind:            DeltaError,
					Err:             terminalInBandPrefix + " 502: " + openaiInBand502,
					Retryable:       true,
					MalformedChunks: 1,
					Fault:           UpstreamFault{Class: FaultInBand, Code: 502},
				},
			},
		},
		{
			name: "an in-band error drops the open calls and carries the skipped count",
			wire: WireAnthropic,
			body: sseData(terminalMalformed, anthropicText("partial"), anthropicCallStart(1, "toolu_a", "ls"), anthropicOverloadEvent, anthropicStopEvent),
			want: []Delta{
				{Kind: DeltaContent, Content: "partial"},
				{
					Kind:            DeltaError,
					Err:             terminalInBandPrefix + " 0: " + anthropicOverloadEvent,
					Retryable:       true,
					MalformedChunks: 1,
					Fault:           UpstreamFault{Class: FaultInBand, ErrorType: "overloaded_error"},
				},
			},
		},
		{
			name: "an in-band context overflow carries the skipped count too",
			wire: WireOpenAI,
			body: sseData(terminalMalformed, openaiContent("partial"), openaiOverflow),
			want: []Delta{
				{Kind: DeltaContent, Content: "partial"},
				{Kind: DeltaContextOverflow, Err: terminalInBandPrefix + " 400: " + openaiOverflow, MalformedChunks: 1},
			},
		},
		{
			name: "an in-band context overflow carries the skipped count too",
			wire: WireAnthropic,
			body: sseData(terminalMalformed, anthropicText("partial"), anthropicOverflowEvent),
			want: []Delta{
				{Kind: DeltaContent, Content: "partial"},
				{Kind: DeltaContextOverflow, Err: terminalInBandPrefix + " 0: " + anthropicOverflowEvent, MalformedChunks: 1},
			},
		},
		{
			name: "an in-band error alone is the only Delta",
			wire: WireOpenAI,
			body: sseData(openaiInBand502),
			want: []Delta{{
				Kind:      DeltaError,
				Err:       terminalInBandPrefix + " 502: " + openaiInBand502,
				Retryable: true,
				Fault:     UpstreamFault{Class: FaultInBand, Code: 502},
			}},
		},
		{
			name: "an in-band error alone is the only Delta",
			wire: WireAnthropic,
			body: sseData(anthropicOverloadEvent),
			want: []Delta{{
				Kind:      DeltaError,
				Err:       terminalInBandPrefix + " 0: " + anthropicOverloadEvent,
				Retryable: true,
				Fault:     UpstreamFault{Class: FaultInBand, ErrorType: "overloaded_error"},
			}},
		},
	})
}

// TestTerminalPaths_TextCap pins the reply-text cap: a reply exactly at the limit ends cleanly,
// one byte over ends on the exact sentence as a local fault that is not retryable and carries
// no skipped-chunk count (D7), and the fragment that crossed is never yielded. Its wire rows pin
// what each wire charges: openai a chunk's content and thinking together before either is
// yielded (D1), anthropic each fragment on its own (D1) and, besides text, every reasoning
// block's opening cost, signature and redacted data (D2). The rows run one at a time: each
// body is the size of the cap.
func TestTerminalPaths_TextCap(t *testing.T) {
	const limit = maxReplyTextBytes
	tripped := terminalLocalFault(terminalTextCapTripped)
	redactedOpen := fmt.Sprintf(`{"type":"content_block_start","index":1,"content_block":%s}`, anthropicRedactedBlock)
	redactedFill := limit - reasoningBlockCost - len(anthropicRedactedBlock)

	runTerminalPaths(t, false, []terminalPathRow{
		{
			name: "a reply exactly at the limit ends cleanly",
			wire: WireOpenAI,
			body: terminalFillBody(WireOpenAI, limit) + sseData(sseDone),
			want: terminalFillDeltas(limit, Delta{Kind: DeltaDone, FinishReason: "stop"}),
		},
		{
			name: "a reply exactly at the limit ends cleanly",
			wire: WireAnthropic,
			body: terminalFillBody(WireAnthropic, limit) + sseData(anthropicStopEvent),
			want: terminalFillDeltas(limit, Delta{Kind: DeltaDone, FinishReason: "stop"}),
		},
		{
			name: "one byte over is the limit fault, without the skipped count",
			wire: WireOpenAI,
			body: sseData(terminalMalformed) + terminalFillBody(WireOpenAI, limit) + sseData(openaiContent("b"), openaiContent("late"), sseDone),
			want: terminalFillDeltas(limit, tripped),
		},
		{
			name: "one byte over is the limit fault, without the skipped count",
			wire: WireAnthropic,
			body: sseData(terminalMalformed) + terminalFillBody(WireAnthropic, limit) + sseData(anthropicText("b"), anthropicText("late"), anthropicStopEvent),
			want: terminalFillDeltas(limit, tripped),
		},
		{
			name: "thinking counts toward the cap",
			wire: WireOpenAI,
			body: terminalFillBody(WireOpenAI, limit) + sseData(`{"choices":[{"delta":{"reasoning_content":"t"}}]}`, sseDone),
			want: terminalFillDeltas(limit, tripped),
		},
		{
			name: "thinking counts toward the cap",
			wire: WireAnthropic,
			body: terminalFillBody(WireAnthropic, limit) + sseData(anthropicThinkingDelta("t"), anthropicStopEvent),
			want: terminalFillDeltas(limit, tripped),
		},
		{
			name: "a chunk's thinking and content are charged together, so neither is yielded (D1)",
			wire: WireOpenAI,
			body: terminalFillBody(WireOpenAI, limit-10) + sseData(`{"choices":[{"delta":{"content":"cccccc","reasoning_content":"tttttt"}}]}`, sseDone),
			want: terminalFillDeltas(limit-10, tripped),
		},
		{
			name: "each fragment is charged on its own, so the one that fits is yielded (D1)",
			wire: WireAnthropic,
			body: terminalFillBody(WireAnthropic, limit-10) + sseData(anthropicThinkingDelta("tttttt"), anthropicText("cccccc"), anthropicStopEvent),
			want: terminalFillDeltas(limit-10, Delta{Kind: DeltaThinking, Thinking: "tttttt"}, tripped),
		},
		{
			name: "a reasoning block's opening cost fits exactly at the limit (D2)",
			wire: WireAnthropic,
			body: terminalFillBody(WireAnthropic, limit-reasoningBlockCost) +
				sseData(fmt.Sprintf(anthropicThinkingOpen, 1), fmt.Sprintf(anthropicBlockClose, 1), anthropicStopEvent),
			want: terminalFillDeltas(
				limit-reasoningBlockCost,
				Delta{Kind: DeltaThinkingBlock, ThinkingBlock: []byte(`{"after_text":true,"block":{"type":"thinking","thinking":"","signature":""}}`)},
				Delta{Kind: DeltaDone, FinishReason: "stop"},
			),
		},
		{
			name: "a reasoning block's opening cost is charged (D2)",
			wire: WireAnthropic,
			body: terminalFillBody(WireAnthropic, limit-reasoningBlockCost+1) +
				sseData(fmt.Sprintf(anthropicThinkingOpen, 1), fmt.Sprintf(anthropicBlockClose, 1), anthropicStopEvent),
			want: terminalFillDeltas(limit-reasoningBlockCost+1, tripped),
		},
		{
			name: "a reasoning block's signature is charged (D2)",
			wire: WireAnthropic,
			body: terminalFillBody(WireAnthropic, limit-reasoningBlockCost) +
				sseData(fmt.Sprintf(anthropicThinkingOpen, 1), anthropicSignature(1, "s"), fmt.Sprintf(anthropicBlockClose, 1), anthropicStopEvent),
			want: terminalFillDeltas(limit-reasoningBlockCost, tripped),
		},
		{
			name: "a redacted block's data fits exactly at the limit (D2)",
			wire: WireAnthropic,
			body: terminalFillBody(WireAnthropic, redactedFill) +
				sseData(redactedOpen, fmt.Sprintf(anthropicBlockClose, 1), anthropicStopEvent),
			want: terminalFillDeltas(
				redactedFill,
				Delta{Kind: DeltaThinkingBlock, ThinkingBlock: []byte(`{"after_text":true,"block":` + anthropicRedactedBlock + `}`)},
				Delta{Kind: DeltaDone, FinishReason: "stop"},
			),
		},
		{
			name: "a redacted block's data is charged (D2)",
			wire: WireAnthropic,
			body: terminalFillBody(WireAnthropic, redactedFill+1) +
				sseData(redactedOpen, fmt.Sprintf(anthropicBlockClose, 1), anthropicStopEvent),
			want: terminalFillDeltas(redactedFill+1, tripped),
		},
	})
}

// terminalCallOpenings is a body that opens n tool calls on wire at distinct indexes, none with
// arguments, and the Deltas their flush yields: the empty arguments stay empty on the openai
// wire and are the empty object on the anthropic one (D3).
func terminalCallOpenings(wire Wire, n int) (payloads []string, calls []Delta) {
	for i := range n {
		id := fmt.Sprintf("call_%d", i)
		if wire == WireAnthropic {
			payloads = append(payloads, anthropicCallStart(i, id, "grep"))
			calls = append(calls, terminalCall(id, "grep", "{}"))
			continue
		}
		payloads = append(payloads, openaiCall(i, id, "grep", ""))
		calls = append(calls, terminalCall(id, "grep", ""))
	}
	return payloads, calls
}

// TestTerminalPaths_ToolCallCaps pins the two tool-call caps: the argument bytes summed across
// every open call, and the count of open calls. Exactly at either limit the calls still flush
// before the Done; past it the stream ends on the cap's exact sentence as a local fault with no
// call flushed, no Done and no skipped-chunk count (D7). The rows run one at a time: the byte
// rows carry the cap's worth of arguments.
func TestTerminalPaths_ToolCallCaps(t *testing.T) {
	half := strings.Repeat("a", maxToolCallBytes/2)
	rows := []terminalPathRow{
		{
			name: "argument bytes summed across calls exactly at the limit still flush",
			wire: WireOpenAI,
			body: sseData(openaiCall(0, "call_a", "grep", half), openaiCall(1, "call_b", "read", half), sseDone),
			want: []Delta{terminalCall("call_a", "grep", half), terminalCall("call_b", "read", half), {Kind: DeltaDone, FinishReason: "stop"}},
		},
		{
			name: "argument bytes summed across calls exactly at the limit still flush",
			wire: WireAnthropic,
			body: sseData(
				anthropicCallStart(0, "call_a", "grep"), anthropicCallInput(0, half),
				anthropicCallStart(1, "call_b", "read"), anthropicCallInput(1, half),
				anthropicStopEvent,
			),
			want: []Delta{terminalCall("call_a", "grep", half), terminalCall("call_b", "read", half), {Kind: DeltaDone, FinishReason: "stop"}},
		},
		{
			name: "argument bytes summed across calls past the limit are the byte fault",
			wire: WireOpenAI,
			body: sseData(
				terminalMalformed, openaiContent("x"),
				openaiCall(0, "call_a", "grep", half), openaiCall(1, "call_b", "read", half+"a"),
				openaiContent("late"), sseDone,
			),
			want: []Delta{{Kind: DeltaContent, Content: "x"}, terminalLocalFault(terminalToolBytesTripped)},
		},
		{
			name: "argument bytes summed across calls past the limit are the byte fault",
			wire: WireAnthropic,
			body: sseData(
				terminalMalformed, anthropicText("x"),
				anthropicCallStart(0, "call_a", "grep"), anthropicCallInput(0, half),
				anthropicCallStart(1, "call_b", "read"), anthropicCallInput(1, half+"a"),
				anthropicText("late"), anthropicStopEvent,
			),
			want: []Delta{{Kind: DeltaContent, Content: "x"}, terminalLocalFault(terminalToolBytesTripped)},
		},
	}
	for _, wire := range []Wire{WireOpenAI, WireAnthropic} {
		terminator := sseDone
		if wire == WireAnthropic {
			terminator = anthropicStopEvent
		}
		atCap, calls := terminalCallOpenings(wire, maxOpenToolCalls)
		pastCap, _ := terminalCallOpenings(wire, maxOpenToolCalls+1)
		rows = append(rows,
			terminalPathRow{
				name: "open calls exactly at the count limit still flush",
				wire: wire,
				body: sseData(append(atCap, terminator)...),
				want: append(calls, Delta{Kind: DeltaDone, FinishReason: "stop"}),
			},
			terminalPathRow{
				name: "one call past the count limit is the count fault",
				wire: wire,
				body: sseData(append(append([]string{terminalMalformed}, pastCap...), terminator)...),
				want: []Delta{terminalLocalFault(terminalToolCountTripped)},
			},
		)
	}

	runTerminalPaths(t, false, rows)
}

// TestTerminalPaths_ConsumerBreak pins a consumer that stops listening: the Delta it refused
// is the last one yielded, wherever in the reply it fell — a fragment, the first of a chunk's
// two fragments, a closing reasoning block, the layout entry, or the first flushed tool call.
func TestTerminalPaths_ConsumerBreak(t *testing.T) {
	t.Parallel()

	split := sseData(append(anthropicSplitReply(), anthropicStopEvent)...)
	runTerminalPaths(t, true, []terminalPathRow{
		{
			name:   "a break at a fragment stops the read",
			wire:   WireOpenAI,
			body:   sseData(openaiContent("a"), openaiContent("b"), sseDone),
			stopAt: 1,
			want:   []Delta{{Kind: DeltaContent, Content: "a"}},
		},
		{
			name:   "a break at a fragment stops the read",
			wire:   WireAnthropic,
			body:   sseData(anthropicText("a"), anthropicText("b"), anthropicStopEvent),
			stopAt: 1,
			want:   []Delta{{Kind: DeltaContent, Content: "a"}},
		},
		{
			name:   "a break at a fragment before a bare EOF yields no Done",
			wire:   WireOpenAI,
			body:   sseData(openaiContent("a")),
			stopAt: 1,
			want:   []Delta{{Kind: DeltaContent, Content: "a"}},
		},
		{
			name:   "a break at a fragment before a bare EOF yields no Done",
			wire:   WireAnthropic,
			body:   sseData(anthropicText("a")),
			stopAt: 1,
			want:   []Delta{{Kind: DeltaContent, Content: "a"}},
		},
		{
			name:   "a break at the first flushed call yields no second call and no Done",
			wire:   WireOpenAI,
			body:   sseData(openaiCall(0, "call_a", "ls", "{}"), openaiCall(1, "call_b", "ls", "{}"), sseDone),
			stopAt: 1,
			want:   []Delta{terminalCall("call_a", "ls", "{}")},
		},
		{
			name:   "a break at the first flushed call yields no second call and no Done",
			wire:   WireAnthropic,
			body:   sseData(anthropicCallStart(0, "call_a", "ls"), anthropicCallStart(1, "call_b", "ls"), anthropicStopEvent),
			stopAt: 1,
			want:   []Delta{terminalCall("call_a", "ls", "{}")},
		},
		{
			name:   "a break at a chunk's thinking yields none of its content",
			wire:   WireOpenAI,
			body:   sseData(`{"choices":[{"delta":{"content":"c","reasoning_content":"t"}}]}`, sseDone),
			stopAt: 1,
			want:   []Delta{{Kind: DeltaThinking, Thinking: "t"}},
		},
		{
			name:   "a break at a closing reasoning block stops the read",
			wire:   WireAnthropic,
			body:   split,
			stopAt: 2,
			want: []Delta{
				{Kind: DeltaContent, Content: "a"},
				{Kind: DeltaThinkingBlock, ThinkingBlock: []byte(anthropicSplitThinking)},
			},
		},
		{
			name:   "a break at the layout entry yields no call and no Done",
			wire:   WireAnthropic,
			body:   split,
			stopAt: 4,
			want: []Delta{
				{Kind: DeltaContent, Content: "a"},
				{Kind: DeltaThinkingBlock, ThinkingBlock: []byte(anthropicSplitThinking)},
				{Kind: DeltaContent, Content: "b"},
				{Kind: DeltaThinkingBlock, ThinkingBlock: []byte(anthropicSplitLayout)},
			},
		},
	})
}

// TestTerminalPaths_OtherWiresTerminator pins what each wire makes of the other's terminator
// (D6): neither ends the reply. `[DONE]` on the anthropic wire is a payload that does not
// decode, so it is skipped and counted; `message_stop` on the openai wire decodes as a chunk
// with no choices, so it is ignored without a count.
func TestTerminalPaths_OtherWiresTerminator(t *testing.T) {
	t.Parallel()

	runTerminalPaths(t, true, []terminalPathRow{
		{
			name: "message_stop is an ignored chunk",
			wire: WireOpenAI,
			body: sseData(openaiContent("a"), anthropicStopEvent, openaiContent("b")),
			want: []Delta{
				{Kind: DeltaContent, Content: "a"},
				{Kind: DeltaContent, Content: "b"},
				{Kind: DeltaDone, FinishReason: "stop"},
			},
		},
		{
			name: "[DONE] is a counted malformed chunk",
			wire: WireAnthropic,
			body: sseData(anthropicText("a"), sseDone, anthropicText("b")),
			want: []Delta{
				{Kind: DeltaContent, Content: "a"},
				{Kind: DeltaContent, Content: "b"},
				{Kind: DeltaDone, FinishReason: "stop", MalformedChunks: 1},
			},
		},
		{
			name: "message_stop alone carried nothing and skipped nothing",
			wire: WireOpenAI,
			body: sseData(anthropicStopEvent),
			want: []Delta{{Kind: DeltaDone, FinishReason: "stop"}},
		},
		{
			name: "[DONE] alone is a stream of nothing but a malformed chunk",
			wire: WireAnthropic,
			body: sseData(sseDone),
			want: []Delta{terminalMalformedOnly(1)},
		},
	})
}

// TestTerminalPaths_OpenAIDialect pins what is the openai wire's own on the way to an end: the
// served model is the first non-empty one (D5); empty tool arguments stay empty (D3); and the
// order a chunk is read in (D8), as far as a Delta shows it — an in-band error before anything
// else in its chunk, model and usage off a chunk with no choices, thinking before content, and
// content before the tool fold that trips a cap.
func TestTerminalPaths_OpenAIDialect(t *testing.T) {
	t.Parallel()

	atCap, _ := terminalCallOpenings(WireOpenAI, maxOpenToolCalls)
	pastCapWithContent := fmt.Sprintf(
		`{"choices":[{"delta":{"content":"late","tool_calls":[{"index":%d,"id":"call_over","function":{"name":"grep","arguments":""}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7}}`,
		maxOpenToolCalls,
	)
	runTerminalPaths(t, true, []terminalPathRow{
		{
			name: "the served model is the first non-empty one (D5)",
			wire: WireOpenAI,
			body: sseData(openaiModelContent("", "a"), openaiModelContent("first", "b"), openaiModelContent("second", "c"), sseDone),
			want: []Delta{
				{Kind: DeltaContent, Content: "a"},
				{Kind: DeltaContent, Content: "b"},
				{Kind: DeltaContent, Content: "c"},
				{Kind: DeltaDone, FinishReason: "stop", Model: "first"},
			},
		},
		{
			name: "the last finish reason and the last usage win",
			wire: WireOpenAI,
			body: sseData(
				openaiContent("a"),
				openaiFinish("length"),
				`{"choices":[],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`,
				openaiFinish("tool_calls"),
				openaiUsageChunk,
				sseDone,
			),
			want: []Delta{
				{Kind: DeltaContent, Content: "a"},
				{Kind: DeltaDone, FinishReason: "tool_calls", Usage: terminalUsage()},
			},
		},
		{
			name: "a call with no arguments keeps them empty (D3)",
			wire: WireOpenAI,
			body: sseData(openaiCall(0, "call_a", "ls", ""), openaiFinish("tool_calls"), sseDone),
			want: []Delta{terminalCall("call_a", "ls", ""), {Kind: DeltaDone, FinishReason: "tool_calls"}},
		},
		{
			name: "an in-band error comes before everything else in its chunk (D8)",
			wire: WireOpenAI,
			body: sseData(
				`{"model":"gpt-x","error":{"message":"upstream gone","code":502},"choices":[{"delta":{"content":"x"}}]}`,
				sseDone,
			),
			want: []Delta{{
				Kind:      DeltaError,
				Err:       terminalInBandPrefix + ` 502: {"model":"gpt-x","error":{"message":"upstream gone","code":502},"choices":[{"delta":{"content":"x"}}]}`,
				Retryable: true,
				Fault:     UpstreamFault{Class: FaultInBand, Code: 502},
			}},
		},
		{
			name: "a chunk with no choices still records its model and usage (D8)",
			wire: WireOpenAI,
			body: sseData(
				`{"model":"gpt-x","choices":[],"usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7}}`,
				openaiContent("a"),
				sseDone,
			),
			want: []Delta{
				{Kind: DeltaContent, Content: "a"},
				{Kind: DeltaDone, FinishReason: "stop", Usage: terminalUsage(), Model: "gpt-x"},
			},
		},
		{
			name: "a chunk's thinking is yielded before its content, its finish reason read last (D8)",
			wire: WireOpenAI,
			body: sseData(`{"choices":[{"delta":{"content":"c","reasoning_content":"t"},"finish_reason":"length"}]}`, sseDone),
			want: []Delta{
				{Kind: DeltaThinking, Thinking: "t"},
				{Kind: DeltaContent, Content: "c"},
				{Kind: DeltaDone, FinishReason: "length"},
			},
		},
		{
			name: "a chunk whose tool fold trips a limit yielded its content first (D8)",
			wire: WireOpenAI,
			body: sseData(append(atCap, pastCapWithContent, sseDone)...),
			want: []Delta{{Kind: DeltaContent, Content: "late"}, terminalLocalFault(terminalToolCountTripped)},
		},
	})
}

// TestTerminalPaths_AnthropicDialect pins what is the anthropic wire's own on the way to an
// end: the served model is the last message_start's, empty or not (D5); a reply that only
// opened a reasoning block carried something (D2); the epilogue — the layout entry after the
// reply's reasoning blocks and before its calls, and empty tool arguments as the empty object
// (D3); and the reasoning-block count limit, a local fault that carries no skipped-chunk count.
func TestTerminalPaths_AnthropicDialect(t *testing.T) {
	t.Parallel()

	var openings []string
	for range maxReasoningBlocks + 1 {
		openings = append(openings, fmt.Sprintf(anthropicThinkingOpen, 0))
	}
	bareThinking := []byte(`{"type":"thinking","thinking":"","signature":""}`)
	runTerminalPaths(t, true, []terminalPathRow{
		{
			name: "the served model is the last message_start's (D5)",
			wire: WireAnthropic,
			body: sseData(
				`{"type":"message_start","message":{"model":"first"}}`,
				`{"type":"message_start","message":{"model":"second"}}`,
				anthropicText("a"),
				anthropicStopEvent,
			),
			want: []Delta{{Kind: DeltaContent, Content: "a"}, {Kind: DeltaDone, FinishReason: "stop", Model: "second"}},
		},
		{
			name: "a later message_start naming no model clears the served model (D5)",
			wire: WireAnthropic,
			body: sseData(
				`{"type":"message_start","message":{"model":"first"}}`,
				`{"type":"message_start","message":{}}`,
				anthropicText("a"),
				anthropicStopEvent,
			),
			want: []Delta{{Kind: DeltaContent, Content: "a"}, {Kind: DeltaDone, FinishReason: "stop"}},
		},
		{
			name: "usage is merged across events and the stop reason mapped at receipt",
			wire: WireAnthropic,
			body: sseData(
				`{"type":"message_start","message":{"model":"claude-x","usage":{"input_tokens":3,"cache_read_input_tokens":2,"output_tokens":1}}}`,
				anthropicText("a"),
				anthropicMessageDelta("tool_use"),
				anthropicStopEvent,
			),
			want: []Delta{
				{Kind: DeltaContent, Content: "a"},
				{
					Kind:         DeltaDone,
					FinishReason: "tool_calls",
					Usage:        &Usage{PromptTokens: 5, CompletionTokens: 4, TotalTokens: 9, CachedPromptTokens: 2},
					Model:        "claude-x",
				},
			},
		},
		{
			name: "a reply that only opened and closed an empty thinking block carried something (D2)",
			wire: WireAnthropic,
			body: sseData(terminalMalformed, fmt.Sprintf(anthropicThinkingOpen, 0), fmt.Sprintf(anthropicBlockClose, 0), anthropicStopEvent),
			want: []Delta{
				{Kind: DeltaThinkingBlock, ThinkingBlock: bareThinking},
				{Kind: DeltaDone, FinishReason: "stop", MalformedChunks: 1},
			},
		},
		{
			name: "a reply that only opened an empty thinking block carried something (D2)",
			wire: WireAnthropic,
			body: sseData(terminalMalformed, fmt.Sprintf(anthropicThinkingOpen, 0)),
			want: []Delta{{Kind: DeltaDone, FinishReason: "stop", MalformedChunks: 1}},
		},
		{
			name: "the layout entry follows the reasoning blocks and precedes the calls, whose empty arguments are the empty object (D3)",
			wire: WireAnthropic,
			body: sseData(append(anthropicSplitReply(), anthropicMessageDelta("tool_use"), anthropicStopEvent)...),
			want: []Delta{
				{Kind: DeltaContent, Content: "a"},
				{Kind: DeltaThinkingBlock, ThinkingBlock: []byte(anthropicSplitThinking)},
				{Kind: DeltaContent, Content: "b"},
				{Kind: DeltaThinkingBlock, ThinkingBlock: []byte(anthropicSplitLayout)},
				terminalCall("toolu_1", "ls", "{}"),
				{Kind: DeltaDone, FinishReason: "tool_calls", Usage: &Usage{CompletionTokens: 4, TotalTokens: 4}},
			},
		},
		{
			name: "a bare EOF runs the same epilogue (D3)",
			wire: WireAnthropic,
			body: sseData(anthropicSplitReply()...),
			want: []Delta{
				{Kind: DeltaContent, Content: "a"},
				{Kind: DeltaThinkingBlock, ThinkingBlock: []byte(anthropicSplitThinking)},
				{Kind: DeltaContent, Content: "b"},
				{Kind: DeltaThinkingBlock, ThinkingBlock: []byte(anthropicSplitLayout)},
				terminalCall("toolu_1", "ls", "{}"),
				{Kind: DeltaDone, FinishReason: "stop"},
			},
		},
		{
			name: "a reasoning block still open at the end withholds the layout entry, not the calls (D3)",
			wire: WireAnthropic,
			body: sseData(append(anthropicSplitReply(), fmt.Sprintf(anthropicThinkingOpen, 4), anthropicStopEvent)...),
			want: []Delta{
				{Kind: DeltaContent, Content: "a"},
				{Kind: DeltaThinkingBlock, ThinkingBlock: []byte(anthropicSplitThinking)},
				{Kind: DeltaContent, Content: "b"},
				terminalCall("toolu_1", "ls", "{}"),
				{Kind: DeltaDone, FinishReason: "stop"},
			},
		},
		{
			name: "a malformed payload then the reasoning-block limit is a fault without the skipped count",
			wire: WireAnthropic,
			body: sseData(append(append([]string{terminalMalformed}, openings...), anthropicStopEvent)...),
			want: []Delta{terminalLocalFault(terminalReasoningCountTripped)},
		},
	})
}
