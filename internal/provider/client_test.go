package provider

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// captureBody runs a non-streaming Respond against a server that echoes a canned OK reply
// and returns the request body the server received, decoded as a generic JSON object.
// It is the Go analogue of the TS oracle's drainAndCapture: assert request-shape without
// a live Upstream.
func captureBody(t *testing.T, req Request, opts ...Option) map[string]any {
	t.Helper()

	var captured []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer srv.Close()

	client := NewClient(srv.URL, req.Model, opts...)
	if _, err := client.Respond(context.Background(), req); err != nil {
		t.Fatalf("Respond: %v", err)
	}

	var body map[string]any
	if err := json.Unmarshal(captured, &body); err != nil {
		t.Fatalf("unmarshal captured request body: %v", err)
	}
	return body
}

// wireMessages extracts the "messages" array from a captured request body as a slice of
// JSON objects.
func wireMessages(t *testing.T, body map[string]any) []map[string]any {
	t.Helper()
	raw, ok := body["messages"].([]any)
	if !ok {
		t.Fatalf("request body has no messages array: %v", body["messages"])
	}
	out := make([]map[string]any, len(raw))
	for i, m := range raw {
		out[i], ok = m.(map[string]any)
		if !ok {
			t.Fatalf("message %d is not an object: %v", i, m)
		}
	}
	return out
}

func TestRespond_ParsesWholeResponse(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"choices": [{
				"message": {
					"content": "hello",
					"reasoning_content": "thinking hard",
					"tool_calls": [{"id":"tc_1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"x\"}"}}]
				},
				"finish_reason": "tool_calls"
			}],
			"usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}
		}`)
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "m")
	got, err := client.Respond(context.Background(), Request{Messages: []Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatalf("Respond: %v", err)
	}

	if got.Content != "hello" {
		t.Errorf("Content = %q, want %q", got.Content, "hello")
	}
	if got.Thinking != "thinking hard" {
		t.Errorf("Thinking = %q, want %q", got.Thinking, "thinking hard")
	}
	if got.FinishReason != "tool_calls" {
		t.Errorf("FinishReason = %q, want %q", got.FinishReason, "tool_calls")
	}
	if len(got.ToolCalls) != 1 || got.ToolCalls[0].ID != "tc_1" || got.ToolCalls[0].Function.Name != "read_file" {
		t.Errorf("ToolCalls = %+v, want one read_file call tc_1", got.ToolCalls)
	}
	if got.Usage != (Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}) {
		t.Errorf("Usage = %+v, want {10 5 15}", got.Usage)
	}
}

func TestRespond_NoChoicesIsZeroValue(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"choices": []}`)
	}))
	defer srv.Close()

	got, err := NewClient(srv.URL, "m").Respond(context.Background(), Request{})
	if err != nil {
		t.Fatalf("Respond: %v", err)
	}
	if got.Content != "" || got.FinishReason != "" || len(got.ToolCalls) != 0 || got.Usage != (Usage{}) {
		t.Errorf("RawResponse = %+v, want zero value", got)
	}
}

// TestRespond_InBandErrorSurfaces covers the aggregator failure mode this guard exists for:
// an HTTP 200 whose body carries an error member and no usable choices. Without it the reply
// decodes to a zero RawResponse and the failure masquerades as a successful empty turn. The
// hint cases pin the second half of that mirroring: a request that carried
// chat_template_kwargs gets the same thinking-effort hint the non-2xx path appends, and gets
// it on the wrapping error rather than inside the server's own body.
func TestRespond_InBandErrorSurfaces(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		effort       Effort
		body         string
		wantOverflow bool
		wantCode     int
		wantContains []string
		wantHint     bool
	}{
		{
			name:         "rate limited with provider metadata",
			body:         `{"error":{"message":"Provider returned error","code":429,"metadata":{"raw":"temporarily rate-limited upstream"}}}`,
			wantCode:     429,
			wantContains: []string{"Provider returned error", "rate-limited upstream"},
		},
		{
			name:         "context overflow",
			body:         `{"error":{"message":"This model's maximum context length is 8192 tokens","code":400}}`,
			wantOverflow: true,
			wantContains: []string{"maximum context length"},
		},
		{
			name:         "non-numeric code still surfaces",
			body:         `{"error":{"message":"rate limited","code":"rate_limit_exceeded"}}`,
			wantCode:     0,
			wantContains: []string{"rate limited"},
		},
		{
			name:         "template failure wrapped in a 200 gets the hint",
			effort:       EffortHigh,
			body:         `{"error":{"message":"jinja2.exceptions.TemplateError","code":500}}`,
			wantCode:     500,
			wantContains: []string{"TemplateError"},
			wantHint:     true,
		},
		{
			name:         "overflow stays unhinted even with kwargs on the wire",
			effort:       EffortHigh,
			body:         `{"error":{"message":"This model's maximum context length is 8192 tokens","code":400}}`,
			wantOverflow: true,
			wantContains: []string{"maximum context length"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()

			got, err := NewClient(srv.URL, "m").Respond(context.Background(), Request{ThinkingEffort: tc.effort})
			if err == nil {
				t.Fatalf("Respond returned no error; RawResponse = %+v", got)
			}
			if got.Content != "" || got.FinishReason != "" || len(got.ToolCalls) != 0 {
				t.Errorf("RawResponse = %+v, want the zero value alongside the error", got)
			}

			if tc.wantOverflow {
				if !errors.Is(err, ErrContextOverflow) {
					t.Errorf("error = %v, want it to wrap ErrContextOverflow", err)
				}
			} else {
				var statusErr *StatusError
				if !errors.As(err, &statusErr) {
					t.Fatalf("error = %v (%T), want a *StatusError", err, err)
				}
				if statusErr.Code != tc.wantCode {
					t.Errorf("StatusError.Code = %d, want %d", statusErr.Code, tc.wantCode)
				}
				for _, want := range tc.wantContains {
					if !strings.Contains(statusErr.Body, want) {
						t.Errorf("StatusError.Body = %q, want it to contain %q", statusErr.Body, want)
					}
				}
				if strings.Contains(statusErr.Body, thinkingEffortHint) {
					t.Errorf("StatusError.Body = %q, want the hint kept out of the server's body", statusErr.Body)
				}
			}
			for _, want := range tc.wantContains {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %q, want it to contain %q", err, want)
				}
			}
			if got := strings.Contains(err.Error(), thinkingEffortHint); got != tc.wantHint {
				t.Errorf("error %q carries the template hint = %t, want %t", err, got, tc.wantHint)
			}
		})
	}
}

// TestRespond_ErrorBodyIsCapped is the unary counterpart of TestStream_ErrorBodyIsCapped:
// a non-2xx body far larger than maxErrorBodyBytes must not be buffered whole. The proof is
// positional — an overflow marker sitting past the cap never reaches the sniff (the error
// stays a *StatusError), while the same marker inside the cap classifies as it always did.
func TestRespond_ErrorBodyIsCapped(t *testing.T) {
	t.Parallel()

	const marker = "context length exceeded"
	filler := strings.Repeat("A", maxErrorBodyBytes)

	tests := []struct {
		name         string
		body         string
		wantOverflow bool
	}{
		{name: "marker past the cap is never read", body: filler + marker},
		{name: "marker within the cap still fires", body: marker + filler, wantOverflow: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()

			_, err := NewClient(srv.URL, "m").Respond(context.Background(), Request{})
			if err == nil {
				t.Fatal("Respond returned no error, want the capped body surfaced as one")
			}
			if got := errors.Is(err, ErrContextOverflow); got != tc.wantOverflow {
				t.Errorf("errors.Is(err, ErrContextOverflow) = %t, want %t (error = %q)", got, tc.wantOverflow, err)
			}
			if len(err.Error()) > maxErrorLength+100 {
				t.Errorf("error text is %d bytes, want the sanitised bound", len(err.Error()))
			}
		})
	}
}

// TestRespond_InBandErrorRedactsAPIKey proves the surfaced body goes through the same
// sanitiser as the non-2xx path — a server that echoes the key must not leak it into an error.
func TestRespond_InBandErrorRedactsAPIKey(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"error":{"message":"bad key sk-secret-123","code":401}}`)
	}))
	defer srv.Close()

	_, err := NewClient(srv.URL, "m", WithAPIKey("sk-secret-123")).Respond(context.Background(), Request{})
	if err == nil {
		t.Fatal("Respond returned no error, want a *StatusError")
	}
	if strings.Contains(err.Error(), "sk-secret-123") {
		t.Errorf("error = %q leaks the API key", err)
	}
	if !strings.Contains(err.Error(), "[REDACTED]") {
		t.Errorf("error = %q, want the key redacted", err)
	}
}

// TestRespond_ThinkingEffortHint covers the enriched turn error (ADR 0050): a chat template that
// rejects an effort value raises inside Jinja and the server answers 500 without ever naming the
// offending field, so a request that carried chat_template_kwargs gets the hint appended. A
// request that carried none keeps exactly the error it produced before, and either way the
// *StatusError stays reachable for callers that branch on the status code.
func TestRespond_ThinkingEffortHint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		effort   Effort
		wantHint bool
	}{
		{name: "effort level puts kwargs on the wire", effort: EffortHigh, wantHint: true},
		{name: "thinking off also puts kwargs on the wire", effort: EffortOff, wantHint: true},
		{name: "no effort, no kwargs, no hint", effort: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = io.WriteString(w, "jinja2.exceptions.TemplateError")
			}))
			defer srv.Close()

			client := NewClient(srv.URL, "m", WithMaxRetries(0))
			_, err := client.Respond(context.Background(), Request{ThinkingEffort: tc.effort})
			if err == nil {
				t.Fatal("Respond returned no error, want the upstream 500 surfaced")
			}

			var status *StatusError
			if !errors.As(err, &status) {
				t.Fatalf("error = %v (%T), want a reachable *StatusError", err, err)
			}
			if status.Code != http.StatusInternalServerError {
				t.Errorf("StatusError.Code = %d, want %d", status.Code, http.StatusInternalServerError)
			}
			if got := strings.Contains(err.Error(), thinkingEffortHint); got != tc.wantHint {
				t.Errorf("error %q carries the hint = %t, want %t", err, got, tc.wantHint)
			}
			if !tc.wantHint && err.Error() != status.Error() {
				t.Errorf("error = %q, want the unhinted text %q", err, status.Error())
			}
			if strings.Contains(status.Body, thinkingEffortHint) {
				t.Errorf("StatusError.Body = %q, want the hint kept out of the server's body", status.Body)
			}
		})
	}
}

func TestBuildBody_RequestShape(t *testing.T) {
	t.Parallel()

	temp := 0.7
	maxTok := 256
	body := captureBody(t, Request{
		Model:    "test-model",
		Messages: []Message{{Role: "system", Content: "be brief"}, {Role: "user", Content: "hi"}},
		Sampling: Sampling{Temperature: &temp, MaxTokens: &maxTok},
	})

	if body["model"] != "test-model" {
		t.Errorf("model = %v, want test-model", body["model"])
	}
	if body["stream"] != false {
		t.Errorf("stream = %v, want false", body["stream"])
	}
	if _, present := body["stream_options"]; present {
		t.Errorf("stream_options present on a non-streaming request: %v", body["stream_options"])
	}
	if body["temperature"] != 0.7 {
		t.Errorf("temperature = %v, want 0.7", body["temperature"])
	}
	if body["max_tokens"] != float64(256) {
		t.Errorf("max_tokens = %v, want 256", body["max_tokens"])
	}
	if _, present := body["top_p"]; present {
		t.Errorf("top_p present though unset: %v", body["top_p"])
	}
	if msgs := wireMessages(t, body); len(msgs) != 2 || msgs[0]["role"] != "system" {
		t.Errorf("messages = %v, want [system, user]", msgs)
	}
}

func TestBuildBody_ToolsArray(t *testing.T) {
	t.Parallel()

	body := captureBody(t, Request{
		Model:    "m",
		Messages: []Message{{Role: "user", Content: "go"}},
		Tools: []ToolSpec{{
			Name:        "read_file",
			Description: "Read a file",
			Parameters:  json.RawMessage(`{"type":"object"}`),
		}},
	})

	tools, ok := body["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %v, want one tool", body["tools"])
	}
	tool := tools[0].(map[string]any)
	if tool["type"] != "function" {
		t.Errorf("tool type = %v, want function", tool["type"])
	}
	fn := tool["function"].(map[string]any)
	if fn["name"] != "read_file" || fn["description"] != "Read a file" {
		t.Errorf("function = %v, want read_file/Read a file", fn)
	}
}

// dialectName labels a subtest for a dialect: the zero dialect spells itself "", which would
// make an unreadable subtest name.
func dialectName(d EffortDialect) string {
	if d == EffortDialectNone {
		return "zero"
	}
	return string(d)
}

// kwargsDialects are the two spellings that must produce the historical llama.cpp wire: the
// explicit one, and the zero value a caller that names no dialect leaves behind.
var kwargsDialects = []EffortDialect{EffortDialectNone, EffortDialectKwargs}

// assertNoEffortKeys fails when a captured body carries any of the three thinking-effort wire
// shapes — the byte-identical anchor's assertion (ADR 0031).
func assertNoEffortKeys(t *testing.T, body map[string]any) {
	t.Helper()

	for _, key := range []string{"chat_template_kwargs", "reasoning", "reasoning_effort"} {
		if value, present := body[key]; present {
			t.Errorf("%s = %v, want the key omitted entirely", key, value)
		}
	}
}

// A request that wants no reasoning carries the exact kwarg object llama.cpp forwards to the
// chat template — the Qwen-family templates key off `enable_thinking`. These are the bytes the
// deleted DisableThinking field used to produce, asserted here so EffortOff stays its
// byte-identical successor — under the zero dialect too, which is what every caller that never
// heard of dialects sends.
func TestBuildBody_EffortOffEmitsEnableThinkingKwarg(t *testing.T) {
	t.Parallel()

	for _, dialect := range kwargsDialects {
		t.Run(dialectName(dialect), func(t *testing.T) {
			t.Parallel()

			body := captureBody(t, Request{
				Messages:       []Message{{Role: "user", Content: "hi"}},
				ThinkingEffort: EffortOff,
				EffortDialect:  dialect,
			})

			kwargs, ok := body["chat_template_kwargs"].(map[string]any)
			if !ok {
				t.Fatalf("chat_template_kwargs = %v, want an object", body["chat_template_kwargs"])
			}
			if len(kwargs) != 1 || kwargs["enable_thinking"] != false {
				t.Errorf("chat_template_kwargs = %v, want exactly {\"enable_thinking\": false}", kwargs)
			}
		})
	}
}

// Every level rides as the template's `reasoning_effort` kwarg, verbatim — no per-family
// translation table (ADR 0050), so "high" goes out as "high" and a widened level the template
// may or may not know goes out as itself. "none" is a level here rather than the off switch:
// only apogee's own "off" spelling closes the block via `enable_thinking`.
func TestBuildBody_EffortLevelsEmitReasoningEffortKwarg(t *testing.T) {
	t.Parallel()

	levels := []Effort{EffortNone, EffortMinimal, EffortLow, EffortMedium, EffortHigh, EffortXHigh, EffortMax}
	for _, dialect := range kwargsDialects {
		for _, level := range levels {
			t.Run(dialectName(dialect)+"/"+string(level), func(t *testing.T) {
				t.Parallel()

				body := captureBody(t, Request{
					Messages:       []Message{{Role: "user", Content: "hi"}},
					ThinkingEffort: level,
					EffortDialect:  dialect,
				})

				kwargs, ok := body["chat_template_kwargs"].(map[string]any)
				if !ok {
					t.Fatalf("chat_template_kwargs = %v, want an object", body["chat_template_kwargs"])
				}
				if len(kwargs) != 1 || kwargs["reasoning_effort"] != string(level) {
					t.Errorf("chat_template_kwargs = %v, want exactly {\"reasoning_effort\": %q}", kwargs, level)
				}
			})
		}
	}
}

// OpenRouter reads a top-level `reasoning` object: a level under `effort`, and `enabled: false`
// for the off rung — that dialect has no "off" level, it has a switch. Both of apogee's
// spellings of that rung ("off" and the reported "none") reach the same switch.
func TestBuildBody_ReasoningDialectEmitsReasoningObject(t *testing.T) {
	t.Parallel()

	for _, level := range []Effort{EffortOff, EffortNone} {
		t.Run("switch/"+string(level), func(t *testing.T) {
			t.Parallel()

			body := captureBody(t, Request{
				Messages:       []Message{{Role: "user", Content: "hi"}},
				ThinkingEffort: level,
				EffortDialect:  EffortDialectReasoning,
			})

			reasoning, ok := body["reasoning"].(map[string]any)
			if !ok {
				t.Fatalf("reasoning = %v, want an object", body["reasoning"])
			}
			if len(reasoning) != 1 || reasoning["enabled"] != false {
				t.Errorf("reasoning = %v, want exactly {\"enabled\": false}", reasoning)
			}
			if _, present := body["chat_template_kwargs"]; present {
				t.Errorf("chat_template_kwargs = %v, want the llama.cpp kwarg omitted on this dialect", body["chat_template_kwargs"])
			}
		})
	}

	for _, level := range []Effort{EffortMinimal, EffortLow, EffortMedium, EffortHigh, EffortXHigh, EffortMax} {
		t.Run("level/"+string(level), func(t *testing.T) {
			t.Parallel()

			body := captureBody(t, Request{
				Messages:       []Message{{Role: "user", Content: "hi"}},
				ThinkingEffort: level,
				EffortDialect:  EffortDialectReasoning,
			})

			reasoning, ok := body["reasoning"].(map[string]any)
			if !ok {
				t.Fatalf("reasoning = %v, want an object", body["reasoning"])
			}
			if len(reasoning) != 1 || reasoning["effort"] != string(level) {
				t.Errorf("reasoning = %v, want exactly {\"effort\": %q}", reasoning, level)
			}
			if _, present := body["chat_template_kwargs"]; present {
				t.Errorf("chat_template_kwargs = %v, want the llama.cpp kwarg omitted on this dialect", body["chat_template_kwargs"])
			}
		})
	}
}

// OpenAI and Groq read a TOP-LEVEL `reasoning_effort` string — the same word the kwargs dialect
// carries inside chat_template_kwargs, in a different place on the wire, so this asserts the
// kwarg is absent. Those models cannot disable reasoning, so the off rung maps to the
// documented floor, "minimal"; every other level passes through verbatim.
func TestBuildBody_OpenAIDialectEmitsTopLevelReasoningEffort(t *testing.T) {
	t.Parallel()

	cases := map[Effort]string{
		EffortOff:     string(EffortMinimal),
		EffortNone:    string(EffortMinimal),
		EffortMinimal: "minimal",
		EffortLow:     "low",
		EffortHigh:    "high",
		EffortXHigh:   "xhigh",
	}
	for level, want := range cases {
		t.Run(string(level), func(t *testing.T) {
			t.Parallel()

			body := captureBody(t, Request{
				Messages:       []Message{{Role: "user", Content: "hi"}},
				ThinkingEffort: level,
				EffortDialect:  EffortDialectOpenAI,
			})

			if body["reasoning_effort"] != want {
				t.Errorf("reasoning_effort = %v, want %q", body["reasoning_effort"], want)
			}
			if _, present := body["chat_template_kwargs"]; present {
				t.Errorf("chat_template_kwargs = %v, want the llama.cpp kwarg omitted on this dialect", body["chat_template_kwargs"])
			}
			if _, present := body["reasoning"]; present {
				t.Errorf("reasoning = %v, want the OpenRouter object omitted on this dialect", body["reasoning"])
			}
		})
	}
}

// The Client stays total on every dialect: the config loader's enum rejects typos, so a value
// that reached the seam anyway emits nothing rather than putting a word no server can read on
// the wire.
func TestBuildBody_UnknownEffortEmitsNothing(t *testing.T) {
	t.Parallel()

	dialects := []EffortDialect{EffortDialectNone, EffortDialectKwargs, EffortDialectReasoning, EffortDialectOpenAI}
	for _, dialect := range dialects {
		t.Run(dialectName(dialect), func(t *testing.T) {
			t.Parallel()

			body := captureBody(t, Request{
				Messages:       []Message{{Role: "user", Content: "hi"}},
				ThinkingEffort: Effort("hihg"),
				EffortDialect:  dialect,
			})

			assertNoEffortKeys(t, body)
		})
	}
}

// The byte-identical anchor: a caller that does not ask for the switch sends none of the three
// effort keys, so every existing caller's request is unchanged by the fields' arrival. Naming a
// dialect without naming an effort changes nothing either — the dialect only says how an intent
// would be spelled, never that there is one.
func TestBuildBody_OmitsChatTemplateKwargsUnlessAsked(t *testing.T) {
	t.Parallel()

	t.Run("no effort, no dialect", func(t *testing.T) {
		t.Parallel()

		body := captureBody(t, Request{Messages: []Message{{Role: "user", Content: "hi"}}})

		assertNoEffortKeys(t, body)
	})

	for _, dialect := range []EffortDialect{EffortDialectKwargs, EffortDialectReasoning, EffortDialectOpenAI} {
		t.Run("no effort, dialect "+dialectName(dialect), func(t *testing.T) {
			t.Parallel()

			body := captureBody(t, Request{
				Messages:      []Message{{Role: "user", Content: "hi"}},
				EffortDialect: dialect,
			})

			assertNoEffortKeys(t, body)
		})
	}
}

// TestFormatMessage_OracleVectors ports the TS provider-message-format vectors: tool
// linkage is preserved only when the request offers native tools, and degrades to a user
// message otherwise.
func TestFormatMessage_OracleVectors(t *testing.T) {
	t.Parallel()

	toolHistory := []Message{
		{Role: "assistant", Content: "", ToolCalls: []ToolCall{{ID: "tc_123", Type: "function", Function: FunctionCall{Name: "ls", Arguments: "{}"}}}},
		{Role: "tool", Content: "file1.ts\nfile2.ts", ToolCallID: "tc_123"},
	}

	t.Run("tool message degrades to user without tools", func(t *testing.T) {
		t.Parallel()
		body := captureBody(t, Request{Model: "m", Messages: toolHistory})
		msgs := wireMessages(t, body)

		if msgs[0]["role"] != "assistant" {
			t.Errorf("assistant role = %v", msgs[0]["role"])
		}
		if _, present := msgs[0]["tool_calls"]; present {
			t.Errorf("tool_calls leaked though request offered no tools: %v", msgs[0]["tool_calls"])
		}
		if msgs[1]["role"] != "user" {
			t.Errorf("tool-result role = %v, want user", msgs[1]["role"])
		}
		if _, present := msgs[1]["tool_call_id"]; present {
			t.Errorf("tool_call_id leaked on a degraded message: %v", msgs[1]["tool_call_id"])
		}
		if msgs[1]["content"] != "file1.ts\nfile2.ts" {
			t.Errorf("degraded content = %v", msgs[1]["content"])
		}
	})

	t.Run("tool linkage preserved with native tools", func(t *testing.T) {
		t.Parallel()
		body := captureBody(t, Request{
			Model:    "m",
			Messages: toolHistory,
			Tools:    []ToolSpec{{Name: "ls", Description: "list", Parameters: json.RawMessage(`{}`)}},
		})
		msgs := wireMessages(t, body)

		if _, present := msgs[0]["tool_calls"]; !present {
			t.Errorf("assistant tool_calls dropped though request offered tools")
		}
		if msgs[0]["content"] != nil {
			t.Errorf("tool-call-only assistant content = %v, want null", msgs[0]["content"])
		}
		if msgs[1]["role"] != "tool" || msgs[1]["tool_call_id"] != "tc_123" {
			t.Errorf("tool-result = %v, want role tool / tool_call_id tc_123", msgs[1])
		}
	})
}

// TestClientSetModelSwapsWireModel pins the reason SetModel exists: the CONFIGURED model wins
// over the Request's in buildBody, so an engine that rebinds its own model without telling the
// Client would keep sending the model the Upstream stopped serving.
func TestClientSetModelSwapsWireModel(t *testing.T) {
	t.Parallel()

	var captured [][]byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		captured = append(captured, body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "old-model")
	req := Request{Model: "ignored-by-the-client", Messages: []Message{{Role: "user", Content: "hi"}}}

	if _, err := client.Respond(context.Background(), req); err != nil {
		t.Fatalf("Respond before SetModel: %v", err)
	}
	client.SetModel("new-model")
	if _, err := client.Respond(context.Background(), req); err != nil {
		t.Fatalf("Respond after SetModel: %v", err)
	}

	if len(captured) != 2 {
		t.Fatalf("server saw %d requests, want 2", len(captured))
	}
	for i, want := range []string{"old-model", "new-model"} {
		var body map[string]any
		if err := json.Unmarshal(captured[i], &body); err != nil {
			t.Fatalf("unmarshal request %d: %v", i, err)
		}
		if body["model"] != want {
			t.Errorf("request %d model = %v, want %q", i, body["model"], want)
		}
	}
}

// collectWire returns an observer that appends to records, and the slice it appends to.
// The observer runs on the goroutine driving the call, so no lock is needed for the
// single-caller tests below.
func collectWire(records *[]WireRecord) Option {
	return WithWireObserver(func(r WireRecord) { *records = append(*records, r) })
}

// wireOf returns the payloads of every record in one direction, in order.
func wireOf(t *testing.T, records []WireRecord, dir WireDirection) []string {
	t.Helper()
	var out []string
	for _, r := range records {
		if r.Direction == dir {
			out = append(out, string(r.Payload))
		}
	}
	return out
}

func TestWireObserver_RecordsPostedBodyWithoutCredentials(t *testing.T) {
	t.Parallel()

	const apiKey = "sk-super-secret"
	var posted []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posted, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer srv.Close()

	var records []WireRecord
	client := NewClient(srv.URL, "m", WithAPIKey(apiKey), collectWire(&records))
	if _, err := client.Respond(context.Background(), Request{Messages: []Message{{Role: "user", Content: "hi"}}}); err != nil {
		t.Fatalf("Respond: %v", err)
	}

	requests := wireOf(t, records, WireRequest)
	if len(requests) != 1 {
		t.Fatalf("request records = %d, want exactly 1", len(requests))
	}
	if requests[0] != string(posted) {
		t.Errorf("request record = %s, want the posted body %s", requests[0], posted)
	}
	if !json.Valid([]byte(requests[0])) {
		t.Errorf("request record is not the JSON body: %s", requests[0])
	}
	for _, leak := range []string{apiKey, "Authorization", "Bearer"} {
		if strings.Contains(requests[0], leak) {
			t.Errorf("request record carries %q — headers must never enter a record: %s", leak, requests[0])
		}
	}
	// A successful non-streaming body is decoded straight off the connection, never recorded.
	if got := wireOf(t, records, WireResponse); len(got) != 0 {
		t.Errorf("response records = %v, want none for a non-streaming success", got)
	}
}

func TestWireObserver_RecordsSanitisedErrorBody(t *testing.T) {
	t.Parallel()

	const apiKey = "sk-super-secret"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, "bad key "+apiKey)
	}))
	defer srv.Close()

	var records []WireRecord
	client := NewClient(srv.URL, "m", WithAPIKey(apiKey), WithMaxRetries(0), collectWire(&records))
	if _, err := client.Respond(context.Background(), Request{Messages: []Message{{Role: "user", Content: "hi"}}}); err == nil {
		t.Fatal("Respond: want an error for HTTP 401")
	}

	responses := wireOf(t, records, WireResponse)
	if len(responses) != 1 {
		t.Fatalf("response records = %d, want exactly 1", len(responses))
	}
	if strings.Contains(responses[0], apiKey) {
		t.Errorf("error record leaks the API key: %s", responses[0])
	}
	if !strings.Contains(responses[0], "[REDACTED]") {
		t.Errorf("error record = %q, want the sanitised body", responses[0])
	}
}
