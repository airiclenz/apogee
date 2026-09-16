package provider

import (
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

// TestAnthropicCodecEffort pins the effort mapping and the thinking rule together: every
// request carries `thinking: {"type":"disabled"}`, and output_config.effort appears — with
// carriesEffort reporting it — only for the five levels the Messages API has.
func TestAnthropicCodecEffort(t *testing.T) {
	t.Parallel()

	cases := []struct {
		effort     Effort
		wantEffort string // "" ⇒ no output_config at all
	}{
		{effort: "", wantEffort: ""},
		{effort: EffortOff, wantEffort: ""},
		{effort: EffortNone, wantEffort: ""},
		{effort: EffortMinimal, wantEffort: ""},
		{effort: Effort("bogus"), wantEffort: ""},
		{effort: EffortLow, wantEffort: "low"},
		{effort: EffortMedium, wantEffort: "medium"},
		{effort: EffortHigh, wantEffort: "high"},
		{effort: EffortXHigh, wantEffort: "xhigh"},
		{effort: EffortMax, wantEffort: "max"},
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
			if got.Thinking["type"] != "disabled" {
				t.Errorf("thinking = %v, want type disabled on every request", got.Thinking)
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

// rawResponseEqual compares the fields the decode tests pin; TopCandidates is never set here.
func rawResponseEqual(a, b RawResponse) bool {
	if a.Content != b.Content || a.Thinking != b.Thinking || a.FinishReason != b.FinishReason ||
		a.Model != b.Model || a.Usage != b.Usage || len(a.ToolCalls) != len(b.ToolCalls) {
		return false
	}
	for i := range a.ToolCalls {
		if a.ToolCalls[i] != b.ToolCalls[i] {
			return false
		}
	}
	return true
}
