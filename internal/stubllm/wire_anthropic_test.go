package stubllm

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/provider"
)

// TestMessagesRouteScriptsSignedThinkingBlocks pins the `blocks` turn end to end: a reply scripted
// as signed thinking, redacted thinking, text and tool_use blocks in any interleaving decodes
// through the real provider client on the anthropic wire — whole and streamed — into the text,
// the readable thinking and the calls, and its reasoning blocks go back upstream on the next
// request with every block, signature and redacted data in its scripted place. The claim is made
// on that decode→encode round trip, read off the stub's log of the second request's body, never
// on the provider's opaque entry bytes.
func TestMessagesRouteScriptsSignedThinkingBlocks(t *testing.T) {
	t.Parallel()

	thinking := Block{Type: "thinking", Thinking: "let me look", Signature: "sig-1"}
	redacted := Block{Type: "redacted_thinking", Data: "opaque=="}
	call := Block{Type: "tool_use", Name: "read_file", Arguments: `{"path":"a.go"}`}
	thinkingWire := `{"type":"thinking","thinking":"let me look","signature":"sig-1"}`
	redactedWire := `{"type":"redacted_thinking","data":"opaque=="}`
	callWire := `{"type":"tool_use","id":"call_1","name":"read_file","input":{"path":"a.go"}}`

	cases := []struct {
		name         string
		blocks       []Block
		wantContent  string
		wantThinking string
		wantBlocks   string
	}{
		{
			name:         "signed thinking before text and a call",
			blocks:       []Block{thinking, {Type: "text", Text: "Reading it."}, call},
			wantContent:  "Reading it.",
			wantThinking: "let me look",
			wantBlocks:   `[` + thinkingWire + `,{"type":"text","text":"Reading it."},` + callWire + `]`,
		},
		{
			name:        "redacted thinking between two texts",
			blocks:      []Block{{Type: "text", Text: "Let me"}, redacted, {Type: "text", Text: " look."}, call},
			wantContent: "Let me look.",
			wantBlocks: `[{"type":"text","text":"Let me"},` + redactedWire +
				`,{"type":"text","text":" look."},` + callWire + `]`,
		},
		{
			name:         "text after the call",
			blocks:       []Block{thinking, {Type: "text", Text: "Reading."}, call, {Type: "text", Text: " Then more."}},
			wantContent:  "Reading. Then more.",
			wantThinking: "let me look",
			wantBlocks: `[` + thinkingWire + `,{"type":"text","text":"Reading."},` + callWire +
				`,{"type":"text","text":" Then more."}]`,
		},
	}
	for _, tc := range cases {
		for _, stream := range []bool{false, true} {
			name := tc.name + "/whole"
			if stream {
				name = tc.name + "/streamed"
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				server := New(t, Script{Model: "stub-model", Turns: []Turn{
					{Blocks: tc.blocks, ChunkRunes: 3},
					{When: &Match{ToolResult: "read_file"}, Text: "done"},
				}})
				client := provider.NewClient(server.URL, server.Model, provider.WithWire(provider.WireAnthropic))
				tools := []provider.ToolSpec{{Name: "read_file", Description: "read", Parameters: json.RawMessage(`{"type":"object"}`)}}
				user := provider.Message{Role: "user", Content: "read it"}

				reply := scriptedReply(t, client, provider.Request{Messages: []provider.Message{user}, Tools: tools}, stream)
				if reply.Content != tc.wantContent || reply.Thinking != tc.wantThinking {
					t.Errorf("content = %q thinking = %q, want %q and %q", reply.Content, reply.Thinking, tc.wantContent, tc.wantThinking)
				}
				if len(reply.ToolCalls) != 1 || reply.ToolCalls[0].ID != "call_1" || reply.ToolCalls[0].Function.Name != "read_file" ||
					reply.ToolCalls[0].Function.Arguments != `{"path":"a.go"}` {
					t.Errorf("tool calls = %+v, want call_1 read_file with whole arguments", reply.ToolCalls)
				}
				if reply.FinishReason != "tool_calls" {
					t.Errorf("finish reason = %q, want tool_calls from the scripted tool_use block", reply.FinishReason)
				}

				assistant := provider.Message{Role: "assistant", Content: reply.Content, ToolCalls: reply.ToolCalls, ThinkingBlocks: reply.ThinkingBlocks}
				result := provider.Message{Role: "tool", ToolCallID: "call_1", Content: "package a"}
				next := provider.Request{Messages: []provider.Message{user, assistant, result}, Tools: tools}
				if _, err := client.Respond(t.Context(), next); err != nil {
					t.Fatalf("replay request: %v", err)
				}

				requests := server.Requests()
				if len(requests) != 2 {
					t.Fatalf("requests = %d, want the reply and its replay", len(requests))
				}
				assertJSONEqual(t, replayedAssistantBlocks(t, requests[1].Body), tc.wantBlocks)
				server.AssertConsumed(t)
			})
		}
	}
}

// TestMessagesRouteStreamsSignatureDelta pins the raw streamed shape of a scripted thinking
// block: opened empty, its thinking in chunk_runes deltas, then the whole signature as one
// signature_delta before the stop — and a redacted_thinking block arriving whole on its start.
func TestMessagesRouteStreamsSignatureDelta(t *testing.T) {
	t.Parallel()

	server := New(t, Script{Model: "stub-model", Turns: []Turn{{ChunkRunes: 4, Blocks: []Block{
		{Type: "thinking", Thinking: "hmm, ok", Signature: "sig-1"},
		{Type: "redacted_thinking", Data: "opaque=="},
		{Type: "text", Text: "Hi"},
	}}}})
	events := postMessagesStream(t, server, `{"model":"stub-model","max_tokens":64,"stream":true,"messages":[{"role":"user","content":"hi"}]}`)

	var got []string
	for _, event := range events {
		data, err := json.Marshal(event)
		if err != nil {
			t.Fatalf("marshal event: %v", err)
		}
		got = append(got, string(data))
	}
	want := []string{
		`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"hmm,"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":" ok"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig-1"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"redacted_thinking","data":"opaque=="}}`,
		`{"type":"content_block_stop","index":1}`,
		`{"type":"content_block_start","index":2,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":2,"delta":{"type":"text_delta","text":"Hi"}}`,
		`{"type":"content_block_stop","index":2}`,
	}
	if len(got) != len(want)+3 || !reflect.DeepEqual(got[1:len(got)-2], want) {
		t.Fatalf("events =\n%s\nwant message_start, then\n%s\nthen message_delta and message_stop",
			strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if events[len(events)-2].Delta == nil || events[len(events)-2].Delta.StopReason != "end_turn" {
		t.Errorf("message_delta = %s, want stop_reason end_turn for a blocks turn without a tool_use block", got[len(got)-2])
	}
}

// TestBlocksTurnValidation pins what a `blocks` turn refuses: the per-channel members it stands
// in for, captures, an unknown block type and a block missing the member its type needs.
func TestBlocksTurnValidation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		yaml string
		want string
	}{
		{"beside text", "turns:\n  - text: hi\n    blocks: [{type: text, text: hi}]\n", "sets blocks beside text"},
		{"beside reasoning", "turns:\n  - reasoning: hm\n    blocks: [{type: text, text: hi}]\n", "sets blocks beside text"},
		{"with captures", "turns:\n  - blocks: [{type: text, text: hi}]\n    captures: [{name: x, from: system, pattern: '(a)'}]\n", "carries no captures"},
		{"unknown type", "turns:\n  - blocks: [{type: image}]\n", `blocks[0]: type is "image"`},
		{"unsigned thinking", "turns:\n  - blocks: [{type: thinking, thinking: hm}]\n", "blocks[0]: a thinking block needs a signature"},
		{"redacted without data", "turns:\n  - blocks: [{type: redacted_thinking}]\n", "needs its data"},
		{"empty text", "turns:\n  - blocks: [{type: text}]\n", "a text block needs text"},
		{"nameless tool_use", "turns:\n  - blocks: [{type: text, text: hi}, {type: tool_use}]\n", "blocks[1]: a tool_use block needs a name"},
		{"with http", "turns:\n  - blocks: [{type: text, text: hi}]\n    http: {status: 500}\n", "more than one of a completion"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := Parse([]byte(tc.yaml))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Parse error = %v, want one containing %q", err, tc.want)
			}
		})
	}

	script, err := Parse([]byte("turns:\n  - blocks:\n      - {type: thinking, signature: sig-1}\n" +
		"      - {type: tool_use, id: toolu_1, name: ls, arguments: '{}'}\n"))
	if err != nil {
		t.Fatalf("Parse of a valid blocks turn: %v", err)
	}
	want := []Block{{Type: "thinking", Signature: "sig-1"}, {Type: "tool_use", ID: "toolu_1", Name: "ls", Arguments: "{}"}}
	if !reflect.DeepEqual(script.Turns[0].Blocks, want) {
		t.Errorf("blocks = %+v, want %+v", script.Turns[0].Blocks, want)
	}
}

// scriptedReply sends req through client, whole or streamed, and folds the reply into the
// RawResponse shape both paths share.
func scriptedReply(t *testing.T, client *provider.Client, req provider.Request, stream bool) provider.RawResponse {
	t.Helper()
	if !stream {
		reply, err := client.Respond(t.Context(), req)
		if err != nil {
			t.Fatalf("respond: %v", err)
		}
		return reply
	}
	var reply provider.RawResponse
	for delta := range client.Stream(t.Context(), req) {
		switch delta.Kind {
		case provider.DeltaError:
			t.Fatalf("stream error: %s", delta.Err)
		case provider.DeltaContent:
			reply.Content += delta.Content
		case provider.DeltaThinking:
			reply.Thinking += delta.Thinking
		case provider.DeltaToolCall:
			reply.ToolCalls = append(reply.ToolCalls, *delta.ToolCall)
		case provider.DeltaThinkingBlock:
			reply.ThinkingBlocks = append(reply.ThinkingBlocks, delta.ThinkingBlock)
		case provider.DeltaDone:
			reply.FinishReason = delta.FinishReason
		}
	}
	return reply
}

// replayedAssistantBlocks is the content array of the one assistant message in a logged
// Messages request body, as the client wrote it.
func replayedAssistantBlocks(t *testing.T, body []byte) string {
	t.Helper()
	var request struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &request); err != nil {
		t.Fatalf("decode logged body: %v", err)
	}
	for _, message := range request.Messages {
		if message.Role == "assistant" {
			return string(message.Content)
		}
	}
	t.Fatalf("logged body %s carries no assistant message", body)
	return ""
}

// assertJSONEqual fails unless got and want are the same JSON value, member order aside.
func assertJSONEqual(t *testing.T, got, want string) {
	t.Helper()
	var gotValue, wantValue any
	if err := json.Unmarshal([]byte(got), &gotValue); err != nil {
		t.Fatalf("decode %s: %v", got, err)
	}
	if err := json.Unmarshal([]byte(want), &wantValue); err != nil {
		t.Fatalf("decode want %s: %v", want, err)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Errorf("replayed blocks =\n%s\nwant\n%s", got, want)
	}
}
