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
// decodes to a zero RawResponse and the failure masquerades as a successful empty turn.
func TestRespond_InBandErrorSurfaces(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		body         string
		wantOverflow bool
		wantCode     int
		wantContains []string
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
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()

			got, err := NewClient(srv.URL, "m").Respond(context.Background(), Request{})
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
			}
			for _, want := range tc.wantContains {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %q, want it to contain %q", err, want)
				}
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

// A request that wants no reasoning carries the exact kwarg object llama.cpp forwards to the
// chat template — the Qwen-family templates key off `enable_thinking`.
func TestBuildBody_DisableThinkingEmitsKwarg(t *testing.T) {
	t.Parallel()

	body := captureBody(t, Request{Messages: []Message{{Role: "user", Content: "hi"}}, DisableThinking: true})

	kwargs, ok := body["chat_template_kwargs"].(map[string]any)
	if !ok {
		t.Fatalf("chat_template_kwargs = %v, want an object", body["chat_template_kwargs"])
	}
	if len(kwargs) != 1 || kwargs["enable_thinking"] != false {
		t.Errorf("chat_template_kwargs = %v, want exactly {\"enable_thinking\": false}", kwargs)
	}
}

// The byte-identical anchor: a caller that does not ask for the switch sends no such key, so
// every existing caller's request is unchanged by the field's arrival.
func TestBuildBody_OmitsChatTemplateKwargsUnlessAsked(t *testing.T) {
	t.Parallel()

	body := captureBody(t, Request{Messages: []Message{{Role: "user", Content: "hi"}}})

	if _, present := body["chat_template_kwargs"]; present {
		t.Errorf("an unasked-for request carried chat_template_kwargs; the field must be omitted entirely")
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
