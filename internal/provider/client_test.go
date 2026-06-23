package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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
