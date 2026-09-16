package stubllm

import (
	"fmt"
	"reflect"
	"testing"
)

// TestServerLogsSamplingAndEffortVerbatim pins the request log's sampling and thinking-effort
// fields against the wire: what the body carried is what the log says, key for key, in every
// dialect the provider speaks — and a body that carried none of them logs nil, not a zero, so a
// test can tell "asked for nothing" from "asked for zero".
func TestServerLogsSamplingAndEffortVerbatim(t *testing.T) {
	t.Parallel()

	const head = `{"model":"stub-model","stream":true,"messages":[{"role":"user","content":"hi"}]`
	maxTokens, temperature := 4096, 0.2
	cases := []struct {
		name         string
		body         string
		wantSampling Sampling
		wantEffort   Effort
	}{
		{
			name: "kwargs dialect with a cap and a temperature",
			body: head + `,"max_tokens":4096,"temperature":0.2,` +
				`"chat_template_kwargs":{"reasoning_effort":"high"}}`,
			wantSampling: Sampling{MaxTokens: &maxTokens, Temperature: &temperature},
			wantEffort:   Effort{ChatTemplateKwargs: map[string]any{"reasoning_effort": "high"}},
		},
		{
			name:       "kwargs dialect switching thinking off",
			body:       head + `,"chat_template_kwargs":{"enable_thinking":false}}`,
			wantEffort: Effort{ChatTemplateKwargs: map[string]any{"enable_thinking": false}},
		},
		{
			name:       "reasoning dialect",
			body:       head + `,"reasoning":{"enabled":false}}`,
			wantEffort: Effort{Reasoning: map[string]any{"enabled": false}},
		},
		{
			name:       "openai dialect",
			body:       head + `,"reasoning_effort":"minimal"}`,
			wantEffort: Effort{ReasoningEffort: "minimal"},
		},
		{
			name: "nothing asked",
			body: head + `}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			server := New(t, Script{Model: "stub-model", Turns: []Turn{{Text: "ok"}}})
			post(t, server, tc.body)

			requests := server.Requests()
			if len(requests) != 1 {
				t.Fatalf("requests = %+v, want one", requests)
			}
			if got := requests[0].Sampling; !reflect.DeepEqual(got, tc.wantSampling) {
				t.Errorf("Sampling = %s, want %s", renderSampling(got), renderSampling(tc.wantSampling))
			}
			if got := requests[0].Effort; !reflect.DeepEqual(got, tc.wantEffort) {
				t.Errorf("Effort = %+v, want %+v", got, tc.wantEffort)
			}
		})
	}
}

// renderSampling prints a Sampling by value, since %+v on its pointers prints addresses.
func renderSampling(s Sampling) string {
	maxTokens, temperature := "nil", "nil"
	if s.MaxTokens != nil {
		maxTokens = fmt.Sprint(*s.MaxTokens)
	}
	if s.Temperature != nil {
		temperature = fmt.Sprint(*s.Temperature)
	}
	return fmt.Sprintf("{MaxTokens:%s Temperature:%s}", maxTokens, temperature)
}
