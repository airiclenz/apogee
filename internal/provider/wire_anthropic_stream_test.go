package provider

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// anthropicToolStreamSSE is a Messages event stream the way the API frames it — `event:` lines
// followed by typed `data:` payloads, a ping in the middle — carrying text, then two tool_use
// blocks whose input fragments interleave, then the stop reason and the output usage. The
// interleaving is stricter than the API's own sequential blocks: it proves the fragments are
// joined by block index, never by arrival order.
const anthropicToolStreamSSE = `event: message_start
data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-x","content":[],"stop_reason":null,"usage":{"input_tokens":25,"cache_read_input_tokens":5,"output_tokens":1}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: ping
data: {"type": "ping"}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hel"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"lo"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: content_block_start
data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_1","name":"grep","input":{}}}

event: content_block_start
data: {"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"toolu_2","name":"read_file","input":{}}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"q\":"}}

event: content_block_delta
data: {"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"path\":\"a.go\"}"}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"\"x\"}"}}

event: content_block_stop
data: {"type":"content_block_stop","index":2}

event: content_block_stop
data: {"type":"content_block_stop","index":1}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"output_tokens":15}}

event: message_stop
data: {"type":"message_stop"}

`

// anthropicToolStreamDeltas is what anthropicToolStreamSSE must yield: the text in order, every
// tool call in block-index order with its input assembled, then one Done carrying the mapped
// stop reason, the served model and the usage (input plus cache reads as the prompt).
var anthropicToolStreamDeltas = []Delta{
	{Kind: DeltaContent, Content: "Hel"},
	{Kind: DeltaContent, Content: "lo"},
	{Kind: DeltaToolCall, ToolCall: &ToolCall{ID: "toolu_1", Type: "function", Function: FunctionCall{Name: "grep", Arguments: `{"q":"x"}`}}},
	{Kind: DeltaToolCall, ToolCall: &ToolCall{ID: "toolu_2", Type: "function", Function: FunctionCall{Name: "read_file", Arguments: `{"path":"a.go"}`}}},
	{
		Kind:         DeltaDone,
		FinishReason: "tool_calls",
		Model:        "claude-x",
		Usage:        &Usage{PromptTokens: 30, CompletionTokens: 15, TotalTokens: 45, CachedPromptTokens: 5},
	},
}

// parseAnthropicSSE drains the anthropic parser over one scripted body, through a Client so the
// in-band renderer has its owner.
func parseAnthropicSSE(t *testing.T, body string) []Delta {
	t.Helper()
	client := NewClient("http://unused.invalid", "m", WithWire(WireAnthropic))
	var deltas []Delta
	client.codec.parseSSE(strings.NewReader(body), false, nil, func(d Delta) bool {
		deltas = append(deltas, d)
		return true
	})
	return deltas
}

// TestAnthropicParseSSE_TextAndInterleavedToolCalls pins the whole event vocabulary on one
// stream: text_delta as content, tool_use blocks assembled per block index across interleaved
// input_json_delta fragments and flushed in index order before the Done, tool_use mapped to
// tool_calls, the model from message_start, and usage joined from both ends of the reply.
func TestAnthropicParseSSE_TextAndInterleavedToolCalls(t *testing.T) {
	t.Parallel()

	got := parseAnthropicSSE(t, anthropicToolStreamSSE)

	if !reflect.DeepEqual(got, anthropicToolStreamDeltas) {
		t.Errorf("deltas =\n%s\nwant\n%s", dumpDeltas(got), dumpDeltas(anthropicToolStreamDeltas))
	}
}

// anthropicToolUseOpeningsSSE builds a Messages stream that opens n tool_use blocks at
// distinct block indexes — id and name on each content_block_start, no input — then stops.
func anthropicToolUseOpeningsSSE(n int) string {
	var b strings.Builder
	b.WriteString(`data: {"type":"message_start","message":{"model":"claude-x","usage":{"input_tokens":1,"output_tokens":1}}}` + "\n")
	for i := range n {
		fmt.Fprintf(
			&b,
			`data: {"type":"content_block_start","index":%d,"content_block":{"type":"tool_use","id":"toolu_%d","name":"grep","input":{}}}`+"\n",
			i,
			i,
		)
		fmt.Fprintf(&b, `data: {"type":"content_block_stop","index":%d}`+"\n", i)
	}
	b.WriteString(`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":2}}` + "\n")
	b.WriteString(`data: {"type":"message_stop"}` + "\n")
	return b.String()
}

// TestAnthropicParseSSE_OpenToolCallCountIsCapped pins the count cap on this wire: the
// content_block_start that opens the maxOpenToolCalls+1th tool_use block ends the stream on
// the same count fault the openai wire renders, with no call flushed and no Done.
func TestAnthropicParseSSE_OpenToolCallCountIsCapped(t *testing.T) {
	t.Parallel()

	got := parseAnthropicSSE(t, anthropicToolUseOpeningsSSE(maxOpenToolCalls+1))

	want := []Delta{{Kind: DeltaError, Err: toolCallCountTripped}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("deltas =\n%s\nwant\n%s", dumpDeltas(got), dumpDeltas(want))
	}
}

// TestAnthropicParseSSE_OpenToolCallCountAtTheCapStillFlushes pins the cap's edge on this
// wire: exactly maxOpenToolCalls tool_use blocks flush in index order before the Done.
func TestAnthropicParseSSE_OpenToolCallCountAtTheCapStillFlushes(t *testing.T) {
	t.Parallel()

	got := parseAnthropicSSE(t, anthropicToolUseOpeningsSSE(maxOpenToolCalls))

	if len(got) != maxOpenToolCalls+1 {
		t.Fatalf("deltas = %d, want %d calls and a Done:\n%s", len(got), maxOpenToolCalls, dumpDeltas(got))
	}
	for i, d := range got[:maxOpenToolCalls] {
		want := fmt.Sprintf("toolu_%d", i)
		if d.Kind != DeltaToolCall || d.ToolCall == nil || d.ToolCall.ID != want {
			t.Errorf("delta %d = %+v, want tool call %q (index order)", i, d, want)
		}
	}
	if last := got[maxOpenToolCalls]; last.Kind != DeltaDone {
		t.Errorf("last delta = %+v, want a Done", last)
	}
}

// TestAnthropicParseSSE_Thinking pins the thinking channel: thinking_delta is a DeltaThinking,
// the signature_delta that closes a thinking block is ignored, and end_turn is "stop".
func TestAnthropicParseSSE_Thinking(t *testing.T) {
	t.Parallel()

	const body = `data: {"type":"message_start","message":{"model":"claude-x","usage":{"input_tokens":3,"output_tokens":1}}}
data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}
data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"hmm"}}
data: {"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig=="}}
data: {"type":"content_block_stop","index":0}
data: {"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}
data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"ok"}}
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":4}}
data: {"type":"message_stop"}
`
	want := []Delta{
		{Kind: DeltaThinking, Thinking: "hmm"},
		{Kind: DeltaContent, Content: "ok"},
		{Kind: DeltaDone, FinishReason: "stop", Model: "claude-x", Usage: &Usage{PromptTokens: 3, CompletionTokens: 4, TotalTokens: 7}},
	}

	got := parseAnthropicSSE(t, body)

	if !reflect.DeepEqual(got, want) {
		t.Errorf("deltas =\n%s\nwant\n%s", dumpDeltas(got), dumpDeltas(want))
	}
}

// TestAnthropicParseSSE_InBandError pins the error event: the text streamed before it stands,
// the terminal Delta is the fault the Messages slug classifies to — an overloaded_error is
// retryable, a rate_limit_error is the 429 it stands for, an invalid_request_error saying the
// prompt is too long is the context overflow — and no accumulated tool call is emitted.
func TestAnthropicParseSSE_InBandError(t *testing.T) {
	t.Parallel()

	const prefix = `data: {"type":"message_start","message":{"model":"claude-x","usage":{"input_tokens":3,"output_tokens":1}}}
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"partial"}}
data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_1","name":"grep","input":{}}}
data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{}"}}
`
	cases := []struct {
		name          string
		event         string
		wantKind      DeltaKind
		wantRetryable bool
		wantErr       string
	}{
		{
			name:          "overloaded_error is retryable",
			event:         `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`,
			wantKind:      DeltaError,
			wantRetryable: true,
			wantErr:       "apogee: upstream in-band error 0: ",
		},
		{
			name:          "rate_limit_error is the 429 it stands for",
			event:         `{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`,
			wantKind:      DeltaError,
			wantRetryable: true,
			wantErr:       "apogee: upstream in-band error 429: ",
		},
		{
			name:     "a prompt too long is the context overflow",
			event:    `{"type":"error","error":{"type":"invalid_request_error","message":"prompt is too long: 213462 tokens > 200000 maximum"}}`,
			wantKind: DeltaContextOverflow,
			wantErr:  "apogee: upstream in-band error 0: ",
		},
		{
			name:     "any other class is a plain terminal fault",
			event:    `{"type":"error","error":{"type":"api_error","message":"internal"}}`,
			wantKind: DeltaError,
			wantErr:  "apogee: upstream in-band error 0: ",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := parseAnthropicSSE(t, prefix+"data: "+tc.event+"\n")

			if len(got) != 2 || got[0].Kind != DeltaContent || got[0].Content != "partial" {
				t.Fatalf("deltas =\n%s\nwant the streamed text then one terminal fault", dumpDeltas(got))
			}
			fault := got[1]
			if fault.Kind != tc.wantKind || fault.Retryable != tc.wantRetryable {
				t.Errorf("fault = %+v, want kind %q retryable %t", fault, tc.wantKind, tc.wantRetryable)
			}
			if want := tc.wantErr + tc.event; fault.Err != want {
				t.Errorf("Err = %q, want %q", fault.Err, want)
			}
		})
	}
}

// TestAnthropicParseSSE_Edges pins the parser's edges: a malformed payload is skipped and
// counted on the Done; a stream that decoded nothing but malformed payloads is faulted on
// the count; a tool_use block with no input fragment is the empty object, as on the
// whole-reply path; the server closing without message_stop ends on what it sent; and an
// event type the parser does not know is ignored.
func TestAnthropicParseSSE_Edges(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		want []Delta
	}{
		{
			name: "a malformed payload is skipped and counted",
			body: "data: {not json\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"x\"}}\ndata: {\"type\":\"message_stop\"}\n",
			want: []Delta{{Kind: DeltaContent, Content: "x"}, {Kind: DeltaDone, FinishReason: "stop", MalformedChunks: 1}},
		},
		{
			name: "a stream of nothing but malformed payloads is a fault",
			body: "data: {not json\ndata: [DONE]\ndata: {\"type\":\"message_stop\"}\n",
			want: []Delta{{Kind: DeltaError, Err: "apogee: stream carried no text and no tool calls (2 malformed chunks skipped)", MalformedChunks: 2}},
		},
		{
			name: "a tool_use block without input is the empty object",
			body: "data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_1\",\"name\":\"ls\",\"input\":{}}}\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"},\"usage\":{\"output_tokens\":2}}\ndata: {\"type\":\"message_stop\"}\n",
			want: []Delta{
				{Kind: DeltaToolCall, ToolCall: &ToolCall{ID: "toolu_1", Type: "function", Function: FunctionCall{Name: "ls", Arguments: "{}"}}},
				{Kind: DeltaDone, FinishReason: "tool_calls", Usage: &Usage{CompletionTokens: 2, TotalTokens: 2}},
			},
		},
		{
			name: "the server closing without message_stop ends on what it sent",
			body: "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"cut\"}}\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"max_tokens\"},\"usage\":{\"output_tokens\":9}}\n",
			want: []Delta{{Kind: DeltaContent, Content: "cut"}, {Kind: DeltaDone, FinishReason: "length", Usage: &Usage{CompletionTokens: 9, TotalTokens: 9}}},
		},
		{
			name: "an unknown event type is ignored",
			body: "data: {\"type\":\"future_event\",\"index\":0}\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"x\"}}\ndata: {\"type\":\"message_stop\"}\n",
			want: []Delta{{Kind: DeltaContent, Content: "x"}, {Kind: DeltaDone, FinishReason: "stop"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := parseAnthropicSSE(t, tc.body)

			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("deltas =\n%s\nwant\n%s", dumpDeltas(got), dumpDeltas(tc.want))
			}
		})
	}
}

// TestAnthropicParseSSE_ConsumerBreakStopsTheRead pins that a consumer walking away stops the
// parser at that delta: nothing after it is yielded.
func TestAnthropicParseSSE_ConsumerBreakStopsTheRead(t *testing.T) {
	t.Parallel()

	client := NewClient("http://unused.invalid", "m", WithWire(WireAnthropic))
	var seen []Delta
	client.codec.parseSSE(strings.NewReader(anthropicToolStreamSSE), false, nil, func(d Delta) bool {
		seen = append(seen, d)
		return false
	})

	if len(seen) != 1 || seen[0].Content != "Hel" {
		t.Errorf("deltas after the consumer broke =\n%s\nwant exactly the first", dumpDeltas(seen))
	}
}

// TestAnthropicParseSSE_ReplyTextIsCapped pins maxReplyTextBytes on this wire, the way
// TestStream_ReplyTextIsCapped and TestStream_ThinkingCountsTowardTheCap pin it on the openai
// wire: an endless text or thinking block ends at the byte cap with the one non-retryable
// DeltaError, the crossing fragment is never yielded, and the parser stops reading there — the
// error is the last delta even though the body runs on past it.
func TestAnthropicParseSSE_ReplyTextIsCapped(t *testing.T) {
	t.Parallel()

	const chunkBytes = 64 << 10
	maxChunks := maxReplyTextBytes/chunkBytes + 32
	chunk := strings.Repeat("a", chunkBytes)

	tests := []struct {
		name       string
		blockStart string
		delta      string
	}{
		{
			name:       "text_delta",
			blockStart: `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			delta:      `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"` + chunk + `"}}`,
		},
		{
			name:       "thinking_delta",
			blockStart: `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`,
			delta:      `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"` + chunk + `"}}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var body strings.Builder
			body.WriteString("event: content_block_start\ndata: " + tc.blockStart + "\n\n")
			for range maxChunks {
				body.WriteString("event: content_block_delta\ndata: " + tc.delta + "\n\n")
			}

			deltas := parseAnthropicSSE(t, body.String())

			assertReplyTextCapDeltas(t, deltas)
			if last := deltas[len(deltas)-1]; last.Kind != DeltaError {
				t.Errorf("last delta is %v, want the cap's DeltaError — the parser read on past it", last.Kind)
			}
		})
	}
}

// TestAnthropicWireSelectsTheCodec pins that WithWire(WireAnthropic) is served by the anthropic
// codec, now that the codec has its parser: the Client reports the wire and the codec holds the
// Client it renders in-band errors through.
func TestAnthropicWireSelectsTheCodec(t *testing.T) {
	t.Parallel()

	client := NewClient("http://upstream", "m", WithWire(WireAnthropic))

	if got := client.Wire(); got != WireAnthropic {
		t.Errorf("Wire() = %q, want %q", got, WireAnthropic)
	}
	codec, ok := client.codec.(*anthropicCodec)
	if !ok {
		t.Fatalf("codec = %T, want *anthropicCodec", client.codec)
	}
	if codec.client != client {
		t.Error("the anthropic codec does not hold its Client")
	}
}

// TestWireObserver_StreamRecordsTheAnthropicResponse proves the Client's capture tee on the
// second wire: the anthropic stream yields its deltas through Client.Stream and lands, once at
// stream end, in a WireResponse record holding the body as received — event framing included,
// since on this wire the framing is the protocol.
func TestWireObserver_StreamRecordsTheAnthropicResponse(t *testing.T) {
	t.Parallel()

	srv := sseServer(anthropicToolStreamSSE)
	defer srv.Close()

	var records []WireRecord
	client := NewClient(srv.URL, "m", WithWire(WireAnthropic), collectWire(&records))
	deltas := collectStream(client, Request{Messages: []Message{{Role: "user", Content: "hi"}}})

	if !reflect.DeepEqual(deltas, anthropicToolStreamDeltas) {
		t.Errorf("deltas =\n%s\nwant\n%s", dumpDeltas(deltas), dumpDeltas(anthropicToolStreamDeltas))
	}
	if n := len(wireOf(t, records, WireRequest)); n != 1 {
		t.Errorf("request records = %d, want exactly 1", n)
	}
	responses := wireOf(t, records, WireResponse)
	if len(responses) != 1 {
		t.Fatalf("response records = %d, want exactly 1 at stream end", len(responses))
	}
	if responses[0] != anthropicToolStreamSSE {
		t.Errorf("response record =\n%s\nwant the body as received", responses[0])
	}
	if records[len(records)-1].Direction != WireResponse {
		t.Errorf("last record = %q, want the response delivered once at stream end", records[len(records)-1].Direction)
	}
}

// dumpDeltas renders deltas one per line, tool calls dereferenced, for a readable diff.
func dumpDeltas(deltas []Delta) string {
	var b strings.Builder
	for _, d := range deltas {
		if d.ToolCall != nil {
			fmt.Fprintf(&b, "%+v tool_call=%+v\n", d, *d.ToolCall)
			continue
		}
		if d.Usage != nil {
			fmt.Fprintf(&b, "%+v usage=%+v\n", d, *d.Usage)
			continue
		}
		fmt.Fprintf(&b, "%+v\n", d)
	}
	return b.String()
}
