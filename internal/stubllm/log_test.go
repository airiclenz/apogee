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

// TestServerLogsTheRawBody pins [Request.Body] on both wires: the log carries the body byte for
// byte, keys no decoded member names included, so a claim about a merged `request-extra:` key
// (ADR 0085) is read off what actually arrived.
func TestServerLogsTheRawBody(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		send func(*testing.T, *Server, string) reply
	}{
		{
			name: "chat completions",
			body: `{"model":"stub-model","messages":[{"role":"user","content":"hi"}],"provider":{"order":["x"]}}`,
			send: post,
		},
		{
			name: "messages",
			body: `{"model":"stub-model","max_tokens":64,"messages":[{"role":"user","content":"hi"}],"provider":{"order":["x"]}}`,
			send: postMessages,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			server := New(t, Script{Model: "stub-model", Turns: []Turn{{Text: "ok"}}})
			tc.send(t, server, tc.body)

			requests := server.Requests()
			if len(requests) != 1 {
				t.Fatalf("requests = %+v, want one", requests)
			}
			if got := string(requests[0].Body); got != tc.body {
				t.Errorf("Body = %s, want the body as sent %s", got, tc.body)
			}
		})
	}
}
