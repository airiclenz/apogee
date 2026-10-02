package provider

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// TestAnthropicCodecEncode pins the Messages body the anthropic codec writes for the request
// shapes the loop, the probe and the compaction summariser build: the system fold, the
// tool-result fold into one user message, tool_use input as an object, the max_tokens
// fallback, the effort mapping, and the no-tools fold that keeps a tool history block-free.
func TestAnthropicCodecEncode(t *testing.T) {
	t.Parallel()

	temp, topP, topK, rp, mt := 0.2, 0.9, 40, 1.1, 512
	toolLoop := []Message{
		{Role: "user", Content: "list"},
		{Role: "assistant", Content: "", ToolCalls: []ToolCall{
			{ID: "tc_1", Type: "function", Function: FunctionCall{Name: "ls", Arguments: `{"p": "."}`}},
			{ID: "tc_2", Type: "function", Function: FunctionCall{Name: "pwd", Arguments: ``}},
		}},
		{Role: "tool", Content: "a b", ToolCallID: "tc_1"},
		{Role: "tool", Content: "/w", ToolCallID: "tc_2"},
		{Role: "user", Content: "thanks"},
	}
	lsTool := []ToolSpec{{Name: "ls", Description: "list", Parameters: []byte(`{"type":"object"}`)}}
	replayBlocks := []json.RawMessage{
		[]byte(`{"type":"thinking","thinking":"","signature":"c2ln"}`),
		[]byte(`{"type":"redacted_thinking","data":"RU5D"}`),
		nil, // an empty entry carries no block
		[]byte(`{ "type": "thinking", "thinking": "a\nb", "signature": "czI=" }`),
	}

	cases := []struct {
		name string
		req  Request
		want string
	}{
		{
			name: "two system messages fold into one system, max_tokens falls back",
			req: Request{Model: "m", Messages: []Message{
				{Role: "system", Content: "be brief"},
				{Role: "system", Content: "be kind"},
				{Role: "user", Content: "hi"},
			}},
			want: `{"model":"m","system":"be brief\n\nbe kind","messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}],"max_tokens":4096,"stream":false,"thinking":{"type":"disabled"}}`,
		},
		{
			name: "streamed tool loop: tool_use input as an object, results fold into one user message, knobs the wire knows",
			req: Request{
				Model: "m", Stream: true, Messages: toolLoop, Tools: lsTool,
				Sampling: Sampling{Temperature: &temp, TopP: &topP, TopK: &topK, RepeatPenalty: &rp, MaxTokens: &mt},
			},
			want: `{"model":"m","messages":[{"role":"user","content":[{"type":"text","text":"list"}]},{"role":"assistant","content":[{"type":"tool_use","id":"tc_1","name":"ls","input":{"p":"."}},{"type":"tool_use","id":"tc_2","name":"pwd","input":{}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"tc_1","content":"a b"},{"type":"tool_result","tool_use_id":"tc_2","content":"/w"}]},{"role":"user","content":[{"type":"text","text":"thanks"}]}],"max_tokens":512,"stream":true,"temperature":0.2,"top_p":0.9,"top_k":40,"tools":[{"name":"ls","description":"list","input_schema":{"type":"object"}}],"thinking":{"type":"disabled"}}`,
		},
		{
			name: "no tools folds the tool history to prose: no tool_use or tool_result block",
			req:  Request{Model: "m", Messages: toolLoop},
			want: `{"model":"m","messages":[{"role":"user","content":[{"type":"text","text":"list"}]},{"role":"assistant","content":[{"type":"text","text":"ls({\"p\": \".\"})\npwd()"}]},{"role":"user","content":[{"type":"text","text":"a b"}]},{"role":"user","content":[{"type":"text","text":"/w"}]},{"role":"user","content":[{"type":"text","text":"thanks"}]}],"max_tokens":4096,"stream":false,"thinking":{"type":"disabled"}}`,
		},
		{
			name: "thinking blocks replay verbatim ahead of text and tool_use: empty signed thinking kept, redacted data kept",
			req: Request{Model: "m", Tools: lsTool, Messages: []Message{
				{Role: "user", Content: "go"},
				{Role: "assistant", Content: "listing", ThinkingBlocks: replayBlocks, ToolCalls: []ToolCall{{ID: "tc_1", Function: FunctionCall{Name: "ls", Arguments: `{}`}}}},
				{Role: "tool", Content: "a", ToolCallID: "tc_1"},
			}},
			want: `{"model":"m","messages":[{"role":"user","content":[{"type":"text","text":"go"}]},{"role":"assistant","content":[{"type":"thinking","thinking":"","signature":"c2ln"},{"type":"redacted_thinking","data":"RU5D"},{"type":"thinking","thinking":"a\nb","signature":"czI="},{"type":"text","text":"listing"},{"type":"tool_use","id":"tc_1","name":"ls","input":{}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"tc_1","content":"a"}]}],"max_tokens":4096,"stream":false,"tools":[{"name":"ls","description":"list","input_schema":{"type":"object"}}],"thinking":{"type":"disabled"}}`,
		},
		{
			name: "thinking blocks lead the no-tools fold too, and keep an assistant turn with no text",
			req: Request{Model: "m", Messages: []Message{
				{Role: "user", Content: "go"},
				{Role: "assistant", ThinkingBlocks: replayBlocks[:1]},
			}},
			want: `{"model":"m","messages":[{"role":"user","content":[{"type":"text","text":"go"}]},{"role":"assistant","content":[{"type":"thinking","thinking":"","signature":"c2ln"}]}],"max_tokens":4096,"stream":false,"thinking":{"type":"disabled"}}`,
		},
		{
			name: "without tools a block placed after a call follows the prose the call folds into",
			req: Request{Model: "m", Messages: []Message{
				{Role: "user", Content: "go"},
				{Role: "assistant", ThinkingBlocks: []json.RawMessage{
					replayBlocks[0],
					[]byte(`{"after_calls":1,"block":{"type":"thinking","thinking":"","signature":"czE="}}`),
				}, ToolCalls: []ToolCall{{ID: "tc_1", Function: FunctionCall{Name: "ls", Arguments: `{}`}}}},
			}},
			want: `{"model":"m","messages":[{"role":"user","content":[{"type":"text","text":"go"}]},{"role":"assistant","content":[{"type":"thinking","thinking":"","signature":"c2ln"},{"type":"text","text":"ls({})"},{"type":"thinking","thinking":"","signature":"czE="}]}],"max_tokens":4096,"stream":false,"thinking":{"type":"disabled"}}`,
		},
		{
			name: "assistant text beside its call, empty assistant turn dropped",
			req: Request{Model: "m", Tools: lsTool, Messages: []Message{
				{Role: "user", Content: "go"},
				{Role: "assistant", Content: ""},
				{Role: "assistant", Content: "listing", ToolCalls: []ToolCall{{ID: "tc_1", Function: FunctionCall{Name: "ls", Arguments: `{}`}}}},
			}},
			want: `{"model":"m","messages":[{"role":"user","content":[{"type":"text","text":"go"}]},{"role":"assistant","content":[{"type":"text","text":"listing"},{"type":"tool_use","id":"tc_1","name":"ls","input":{}}]}],"max_tokens":4096,"stream":false,"tools":[{"name":"ls","description":"list","input_schema":{"type":"object"}}],"thinking":{"type":"disabled"}}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			codec := &anthropicCodec{}

			body, _, err := codec.encode(tc.req)

			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			if got := string(body); got != tc.want {
				t.Errorf("body mismatch\n got: %s\nwant: %s", got, tc.want)
			}
		})
	}
}

// TestAnthropicCodecEffort pins the effort mapping and the thinking rule together: the five
// levels the Messages API has write output_config.effort — with carriesEffort reporting it — and
// request `thinking: {"type":"adaptive"}`; every other effort writes no output_config and requests
// `thinking: {"type":"disabled"}`.
func TestAnthropicCodecEffort(t *testing.T) {
	t.Parallel()

	cases := []struct {
		effort       Effort
		wantEffort   string // "" ⇒ no output_config at all
		wantThinking string
	}{
		{effort: "", wantEffort: "", wantThinking: "disabled"},
		{effort: EffortOff, wantEffort: "", wantThinking: "disabled"},
		{effort: EffortNone, wantEffort: "", wantThinking: "disabled"},
		{effort: EffortMinimal, wantEffort: "", wantThinking: "disabled"},
		{effort: Effort("bogus"), wantEffort: "", wantThinking: "disabled"},
		{effort: EffortLow, wantEffort: "low", wantThinking: "adaptive"},
		{effort: EffortMedium, wantEffort: "medium", wantThinking: "adaptive"},
		{effort: EffortHigh, wantEffort: "high", wantThinking: "adaptive"},
		{effort: EffortXHigh, wantEffort: "xhigh", wantThinking: "adaptive"},
		{effort: EffortMax, wantEffort: "max", wantThinking: "adaptive"},
	}
	for _, tc := range cases {
		t.Run(string(tc.effort), func(t *testing.T) {
			t.Parallel()
			codec := &anthropicCodec{}
			req := Request{Model: "m", Messages: []Message{{Role: "user", Content: "x"}}, ThinkingEffort: tc.effort}

			body, carries, err := codec.encode(req)

			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			var got struct {
				Thinking     map[string]string  `json:"thinking"`
				OutputConfig *map[string]string `json:"output_config"`
			}
			if err := json.Unmarshal(body, &got); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if got.Thinking["type"] != tc.wantThinking {
				t.Errorf("thinking = %v, want type %s", got.Thinking, tc.wantThinking)
			}
			if carries != (tc.wantEffort != "") {
				t.Errorf("carriesEffort = %v, want %v", carries, tc.wantEffort != "")
			}
			switch {
			case tc.wantEffort == "" && got.OutputConfig != nil:
				t.Errorf("output_config = %v, want absent", *got.OutputConfig)
			case tc.wantEffort != "" && (got.OutputConfig == nil || (*got.OutputConfig)["effort"] != tc.wantEffort):
				t.Errorf("output_config = %v, want effort %q", got.OutputConfig, tc.wantEffort)
			}
		})
	}
}

// TestAnthropicCodecEffortDropsSamplingUnderThinking pins the owner call of 2026-10-02: while the
// body requests adaptive thinking it carries none of the profile's temperature, top_p or top_k,
// and with thinking disabled all three reach the wire as set.
func TestAnthropicCodecEffortDropsSamplingUnderThinking(t *testing.T) {
	t.Parallel()

	temp, topP, topK := 0.2, 0.9, 40
	cases := []struct {
		name        string
		effort      Effort
		wantSampled bool
	}{
		{name: "adaptive thinking drops the knobs", effort: EffortHigh, wantSampled: false},
		{name: "disabled thinking keeps the knobs", effort: EffortOff, wantSampled: true},
		{name: "no effort keeps the knobs", effort: "", wantSampled: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			codec := &anthropicCodec{}
			req := Request{
				Model:          "m",
				Messages:       []Message{{Role: "user", Content: "x"}},
				ThinkingEffort: tc.effort,
				Sampling:       Sampling{Temperature: &temp, TopP: &topP, TopK: &topK},
			}

			body, _, err := codec.encode(req)

			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			var got map[string]json.RawMessage
			if err := json.Unmarshal(body, &got); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			for _, key := range []string{"temperature", "top_p", "top_k"} {
				if _, present := got[key]; present != tc.wantSampled {
					t.Errorf("%s present = %v, want %v in %s", key, present, tc.wantSampled, body)
				}
			}
		})
	}
}

// TestAnthropicCodecEncodeRejectsNonObjectArguments pins that a tool call whose arguments are
// not a JSON object is an encode error naming the call — the wire cannot carry it as input.
func TestAnthropicCodecEncodeRejectsNonObjectArguments(t *testing.T) {
	t.Parallel()
	codec := &anthropicCodec{}
	req := Request{Model: "m", Tools: []ToolSpec{{Name: "ls", Parameters: []byte(`{}`)}}, Messages: []Message{
		{Role: "user", Content: "go"},
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "tc_bad", Function: FunctionCall{Name: "ls", Arguments: `not json`}}}},
	}}

	_, _, err := codec.encode(req)

	if err == nil || !strings.Contains(err.Error(), "tc_bad") {
		t.Fatalf("encode error = %v, want one naming tc_bad", err)
	}
}

// TestAnthropicCodecEncodeRejectsMalformedThinkingBlock pins that a carried thinking block that
// is not JSON fails the encode rather than reaching the wire.
func TestAnthropicCodecEncodeRejectsMalformedThinkingBlock(t *testing.T) {
	t.Parallel()
	codec := &anthropicCodec{}
	req := Request{Model: "m", Messages: []Message{
		{Role: "user", Content: "go"},
		{Role: "assistant", Content: "x", ThinkingBlocks: []json.RawMessage{[]byte(`{"type":"thinking"`)}},
	}}

	_, _, err := codec.encode(req)

	if err == nil {
		t.Fatal("encode error = nil, want the malformed thinking block refused")
	}
}

// TestOpenAICodecIgnoresThinkingBlocks pins that the carrier is the anthropic wire's alone: an
// assistant message's thinking blocks change nothing in a chat-completions body.
func TestOpenAICodecIgnoresThinkingBlocks(t *testing.T) {
	t.Parallel()
	client := NewClient("http://unused.invalid", "m")
	plain := Request{Model: "m", Messages: []Message{
		{Role: "user", Content: "go"},
		{Role: "assistant", Content: "x"},
	}}
	carried := Request{Model: "m", Messages: []Message{
		{Role: "user", Content: "go"},
		{Role: "assistant", Content: "x", ThinkingBlocks: []json.RawMessage{[]byte(`{"type":"thinking","thinking":"t","signature":"s"}`)}},
	}}

	want, _, err := client.codec.encode(plain)
	if err != nil {
		t.Fatalf("encode plain: %v", err)
	}
	got, _, err := client.codec.encode(carried)

	if err != nil {
		t.Fatalf("encode carried: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("body with thinking blocks\n got: %s\nwant: %s", got, want)
	}
}

// TestAnthropicReplyReplaysInTheOrderReceived pins that a reply's assistant turn goes back
// upstream in the order the reply had its blocks, whole or streamed: a thinking block after the
// text, a progress block between two tool_use blocks, and one trailing the last call keep their
// places, because the API refuses a latest turn whose thinking blocks were rearranged and treats
// any reordered block as an edit that invalidates later thinking (anthropicReasoningEntry).
func TestAnthropicReplyReplaysInTheOrderReceived(t *testing.T) {
	t.Parallel()
	const content = `[{"type":"thinking","thinking":"","signature":"czA="},{"type":"text","text":"checking"},{"type":"thinking","thinking":"","signature":"czE="},{"type":"tool_use","id":"tc_1","name":"ls","input":{}},{"type":"redacted_thinking","data":"RU5D"},{"type":"tool_use","id":"tc_2","name":"ls","input":{"p":"."}},{"type":"thinking","thinking":"","signature":"czM="}]`
	const stream = `data: {"type":"message_start","message":{"model":"claude-x"}}
data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}
data: {"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"czA="}}
data: {"type":"content_block_stop","index":0}
data: {"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}
data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"checking"}}
data: {"type":"content_block_stop","index":1}
data: {"type":"content_block_start","index":2,"content_block":{"type":"thinking","thinking":""}}
data: {"type":"content_block_delta","index":2,"delta":{"type":"signature_delta","signature":"czE="}}
data: {"type":"content_block_stop","index":2}
data: {"type":"content_block_start","index":3,"content_block":{"type":"tool_use","id":"tc_1","name":"ls","input":{}}}
data: {"type":"content_block_stop","index":3}
data: {"type":"content_block_start","index":4,"content_block":{"type":"redacted_thinking","data":"RU5D"}}
data: {"type":"content_block_stop","index":4}
data: {"type":"content_block_start","index":5,"content_block":{"type":"tool_use","id":"tc_2","name":"ls","input":{}}}
data: {"type":"content_block_delta","index":5,"delta":{"type":"input_json_delta","partial_json":"{\"p\":\".\"}"}}
data: {"type":"content_block_stop","index":5}
data: {"type":"content_block_start","index":6,"content_block":{"type":"thinking","thinking":""}}
data: {"type":"content_block_delta","index":6,"delta":{"type":"signature_delta","signature":"czM="}}
data: {"type":"content_block_stop","index":6}
data: {"type":"message_delta","delta":{"stop_reason":"tool_use"}}
data: {"type":"message_stop"}
`
	codec := &anthropicCodec{}
	whole, wireErr, err := codec.decodeWhole(strings.NewReader(`{"type":"message","content":` + content + `,"stop_reason":"tool_use"}`))
	if err != nil || wireErr != nil {
		t.Fatalf("decodeWhole: %v %v", err, wireErr)
	}
	streamed := Message{Role: "assistant"}
	for _, d := range parseAnthropicSSE(t, stream) {
		switch d.Kind {
		case DeltaContent:
			streamed.Content += d.Content
		case DeltaThinkingBlock:
			streamed.ThinkingBlocks = append(streamed.ThinkingBlocks, d.ThinkingBlock)
		case DeltaToolCall:
			streamed.ToolCalls = append(streamed.ToolCalls, *d.ToolCall)
		}
	}
	replies := map[string]Message{
		"whole":    {Role: "assistant", Content: whole.Content, ToolCalls: whole.ToolCalls, ThinkingBlocks: whole.ThinkingBlocks},
		"streamed": streamed,
	}

	for name, reply := range replies {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			req := Request{Model: "m", Tools: []ToolSpec{{Name: "ls", Parameters: []byte(`{"type":"object"}`)}}, Messages: []Message{
				{Role: "user", Content: "go"},
				reply,
				{Role: "tool", Content: "a", ToolCallID: "tc_1"},
				{Role: "tool", Content: "b", ToolCallID: "tc_2"},
			}}

			body, _, err := (&anthropicCodec{}).encode(req)

			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			var sent struct {
				Messages []struct {
					Content json.RawMessage `json:"content"`
				} `json:"messages"`
			}
			if err := json.Unmarshal(body, &sent); err != nil || len(sent.Messages) != 3 {
				t.Fatalf("body %s: %v", body, err)
			}
			if got := string(sent.Messages[1].Content); got != content {
				t.Errorf("assistant turn sent back\n got: %s\nwant: %s", got, content)
			}
		})
	}
}

// TestAnthropicCodecHeadersAndPath pins the endpoint and the key spelling: x-api-key only when
// a key is set, anthropic-version always.
func TestAnthropicCodecHeadersAndPath(t *testing.T) {
	t.Parallel()
	codec := &anthropicCodec{}

	if got := codec.path(); got != "/v1/messages" {
		t.Errorf("path = %q, want /v1/messages", got)
	}
	keyed := codec.headers("sk-1")
	if keyed["x-api-key"] != "sk-1" || keyed["anthropic-version"] != "2023-06-01" || len(keyed) != 2 {
		t.Errorf("headers with key = %v", keyed)
	}
	keyless := codec.headers("")
	if _, has := keyless["x-api-key"]; has || keyless["anthropic-version"] != "2023-06-01" || len(keyless) != 1 {
		t.Errorf("headers without key = %v", keyless)
	}
}

// TestAnthropicCodecDecodeWhole pins the whole-reply decode: text and tool_use blocks onto the
// seam, each stop_reason onto the loop's finish vocabulary, and the usage map.
func TestAnthropicCodecDecodeWhole(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		want RawResponse
	}{
		{
			name: "end_turn text with usage",
			body: `{"type":"message","model":"claude-x","content":[{"type":"text","text":"hel"},{"type":"text","text":"lo"}],"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":4,"cache_read_input_tokens":6}}`,
			want: RawResponse{Model: "claude-x", Content: "hello", FinishReason: "stop", Usage: Usage{PromptTokens: 16, CompletionTokens: 4, TotalTokens: 20, CachedPromptTokens: 6}},
		},
		{
			name: "tool_use blocks become tool calls with re-stringified input",
			body: `{"type":"message","content":[{"type":"text","text":"on it"},{"type":"tool_use","id":"toolu_1","name":"ls","input":{"p": "."}},{"type":"tool_use","id":"toolu_2","name":"pwd","input":{}}],"stop_reason":"tool_use"}`,
			want: RawResponse{Content: "on it", FinishReason: "tool_calls", ToolCalls: []ToolCall{
				{ID: "toolu_1", Type: "function", Function: FunctionCall{Name: "ls", Arguments: `{"p":"."}`}},
				{ID: "toolu_2", Type: "function", Function: FunctionCall{Name: "pwd", Arguments: `{}`}},
			}},
		},
		{
			name: "thinking and redacted_thinking blocks are kept verbatim beside the folded text",
			body: `{"type":"message","content":[{"type":"thinking","thinking":"step <1>","signature":"c2lnMQ=="},{"type":"redacted_thinking","data":"RU5D"},{"type":"thinking", "thinking":"", "signature":"c2lnMg=="},{"type":"text","text":"done"}],"stop_reason":"end_turn"}`,
			want: RawResponse{Content: "done", Thinking: "step <1>", FinishReason: "stop", ThinkingBlocks: []json.RawMessage{
				[]byte(`{"type":"thinking","thinking":"step <1>","signature":"c2lnMQ=="}`),
				[]byte(`{"type":"redacted_thinking","data":"RU5D"}`),
				[]byte(`{"type":"thinking", "thinking":"", "signature":"c2lnMg=="}`),
			}},
		},
		{
			name: "stop_sequence is stop",
			body: `{"type":"message","content":[],"stop_reason":"stop_sequence"}`,
			want: RawResponse{FinishReason: "stop"},
		},
		{
			name: "max_tokens is length",
			body: `{"type":"message","content":[{"type":"text","text":"cut"}],"stop_reason":"max_tokens"}`,
			want: RawResponse{Content: "cut", FinishReason: "length"},
		},
		{
			name: "an unknown stop reason passes through",
			body: `{"type":"message","content":[],"stop_reason":"refusal"}`,
			want: RawResponse{FinishReason: "refusal"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			codec := &anthropicCodec{}

			got, werr, err := codec.decodeWhole(strings.NewReader(tc.body))

			if err != nil || werr != nil {
				t.Fatalf("decodeWhole: err=%v werr=%v", err, werr)
			}
			if !rawResponseEqual(got, tc.want) {
				t.Errorf("decoded\n got: %+v\nwant: %+v", got, tc.want)
			}
		})
	}
}

// TestAnthropicCodecDecodeWholeErrorBody pins that the error body comes back as the in-band
// wireError — slug on ErrorType, text on Message — with a zero reply, never as an empty one.
func TestAnthropicCodecDecodeWholeErrorBody(t *testing.T) {
	t.Parallel()
	codec := &anthropicCodec{}
	body := `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`

	got, werr, err := codec.decodeWhole(strings.NewReader(body))

	if err != nil {
		t.Fatalf("decodeWhole: %v", err)
	}
	if werr == nil || werr.ErrorType != "overloaded_error" || werr.Message != "Overloaded" {
		t.Fatalf("wireError = %+v, want overloaded_error / Overloaded", werr)
	}
	if !rawResponseEqual(got, RawResponse{}) {
		t.Errorf("reply = %+v, want zero", got)
	}
}

// rawResponseEqual compares the fields the decode tests pin, each thinking block byte for byte;
// TopCandidates is never set here.
func rawResponseEqual(a, b RawResponse) bool {
	if a.Content != b.Content || a.Thinking != b.Thinking || a.FinishReason != b.FinishReason ||
		a.Model != b.Model || a.Usage != b.Usage || len(a.ToolCalls) != len(b.ToolCalls) ||
		len(a.ThinkingBlocks) != len(b.ThinkingBlocks) {
		return false
	}
	for i := range a.ToolCalls {
		if a.ToolCalls[i] != b.ToolCalls[i] {
			return false
		}
	}
	for i := range a.ThinkingBlocks {
		if !bytes.Equal(a.ThinkingBlocks[i], b.ThinkingBlocks[i]) {
			return false
		}
	}
	return true
}
