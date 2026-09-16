package provider

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestOpenAICodecBodyIsUnchanged pins the bytes the openai codec posts for the request shapes
// the loop, the probe and the naming call build — captured from the Client before the codec
// seam existed, so the seam is proven byte-identical on the wire rather than by inspection.
// Each row is observed through WithWireObserver on a real round-trip, the way the Inspector
// sees it; the rows cover every effort dialect, the tool-mode message degrade, the logprobs
// pair and the model rule.
func TestOpenAICodecBodyIsUnchanged(t *testing.T) {
	t.Parallel()

	temp, topP, topK, rp, mt := 0.2, 0.9, 40, 1.1, 512
	cases := []struct {
		name string
		req  Request
		// unconfigured builds the Client with no model of its own, so the Request's serves.
		unconfigured bool
		want         string
	}{
		{
			name: "system and user, no knobs",
			req:  Request{Messages: []Message{{Role: "system", Content: "be brief"}, {Role: "user", Content: "hi"}}},
			want: `{"model":"served-model","messages":[{"role":"system","content":"be brief"},{"role":"user","content":"hi"}],"stream":false}`,
		},
		{
			name: "streamed tool loop with every sampling knob",
			req: Request{
				Stream: true,
				Messages: []Message{
					{Role: "user", Content: "list"},
					{Role: "assistant", Content: "", ToolCalls: []ToolCall{{ID: "tc_1", Type: "function", Function: FunctionCall{Name: "ls", Arguments: `{"p":"."}`}}}},
					{Role: "tool", Content: "a b", ToolCallID: "tc_1"},
				},
				Tools:    []ToolSpec{{Name: "ls", Description: "list", Parameters: []byte(`{"type":"object"}`)}},
				Sampling: Sampling{Temperature: &temp, TopP: &topP, TopK: &topK, RepeatPenalty: &rp, MaxTokens: &mt},
			},
			want: `{"model":"served-model","messages":[{"role":"user","content":"list"},{"role":"assistant","content":null,"tool_calls":[{"id":"tc_1","type":"function","function":{"name":"ls","arguments":"{\"p\":\".\"}"}}]},{"role":"tool","content":"a b","tool_call_id":"tc_1"}],"stream":true,"stream_options":{"include_usage":true},"temperature":0.2,"top_p":0.9,"top_k":40,"repeat_penalty":1.1,"max_tokens":512,"tools":[{"type":"function","function":{"name":"ls","description":"list","parameters":{"type":"object"}}}]}`,
		},
		{
			name: "no tools degrades the tool result, logprobs and kwargs effort",
			req: Request{
				Messages: []Message{
					{Role: "assistant", Content: "", ToolCalls: []ToolCall{{ID: "tc_1", Type: "function", Function: FunctionCall{Name: "ls", Arguments: `{}`}}}},
					{Role: "tool", Content: "a b", ToolCallID: "tc_1"},
				},
				LogProbs: true, ThinkingEffort: EffortHigh, EffortDialect: EffortDialectKwargs,
			},
			want: `{"model":"served-model","messages":[{"role":"assistant","content":null},{"role":"user","content":"a b"}],"stream":false,"logprobs":true,"top_logprobs":5,"chat_template_kwargs":{"reasoning_effort":"high"}}`,
		},
		{
			name: "off on the zero dialect switches thinking off",
			req:  Request{Messages: []Message{{Role: "user", Content: "x"}}, ThinkingEffort: EffortOff, EffortDialect: EffortDialectNone},
			want: `{"model":"served-model","messages":[{"role":"user","content":"x"}],"stream":false,"chat_template_kwargs":{"enable_thinking":false}}`,
		},
		{
			name: "off on the reasoning dialect",
			req:  Request{Messages: []Message{{Role: "user", Content: "x"}}, ThinkingEffort: EffortOff, EffortDialect: EffortDialectReasoning},
			want: `{"model":"served-model","messages":[{"role":"user","content":"x"}],"stream":false,"reasoning":{"enabled":false}}`,
		},
		{
			name: "a level on the reasoning dialect",
			req:  Request{Messages: []Message{{Role: "user", Content: "x"}}, ThinkingEffort: EffortLow, EffortDialect: EffortDialectReasoning},
			want: `{"model":"served-model","messages":[{"role":"user","content":"x"}],"stream":false,"reasoning":{"effort":"low"}}`,
		},
		{
			name: "none on the openai dialect is its minimal floor",
			req:  Request{Messages: []Message{{Role: "user", Content: "x"}}, ThinkingEffort: EffortNone, EffortDialect: EffortDialectOpenAI},
			want: `{"model":"served-model","messages":[{"role":"user","content":"x"}],"stream":false,"reasoning_effort":"minimal"}`,
		},
		{
			name: "a level on the openai dialect",
			req:  Request{Messages: []Message{{Role: "user", Content: "x"}}, ThinkingEffort: EffortMax, EffortDialect: EffortDialectOpenAI},
			want: `{"model":"served-model","messages":[{"role":"user","content":"x"}],"stream":false,"reasoning_effort":"max"}`,
		},
		{
			name: "the off dialect emits nothing",
			req:  Request{Messages: []Message{{Role: "user", Content: "x"}}, ThinkingEffort: EffortHigh, EffortDialect: EffortDialectOff},
			want: `{"model":"served-model","messages":[{"role":"user","content":"x"}],"stream":false}`,
		},
		{
			name: "the configured model wins and an unknown effort emits nothing",
			req:  Request{Model: "req-model", Messages: []Message{{Role: "user", Content: "x"}}, ThinkingEffort: Effort("bogus")},
			want: `{"model":"served-model","messages":[{"role":"user","content":"x"}],"stream":false}`,
		},
		{
			name:         "the request's model serves when none is configured",
			req:          Request{Model: "req-model", Messages: []Message{{Role: "user", Content: "x"}}},
			unconfigured: true,
			want:         `{"model":"req-model","messages":[{"role":"user","content":"x"}],"stream":false}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.req.Stream {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, "data: [DONE]\n")
					return
				}
				_, _ = io.WriteString(w, `{}`)
			}))
			defer srv.Close()
			model := "served-model"
			if tc.unconfigured {
				model = ""
			}
			var records []WireRecord
			client := NewClient(srv.URL, model, collectWire(&records))

			if tc.req.Stream {
				collectStream(client, tc.req)
			} else {
				_, _ = client.Respond(context.Background(), tc.req)
			}

			posted := wireOf(t, records, WireRequest)
			if len(posted) != 1 {
				t.Fatalf("request records = %d, want exactly 1", len(posted))
			}
			if posted[0] != tc.want {
				t.Errorf("posted body =\n%s\nwant\n%s", posted[0], tc.want)
			}
		})
	}
}
