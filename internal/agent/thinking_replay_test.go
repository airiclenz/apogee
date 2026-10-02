package agent

// Replayed signed thinking on the anthropic wire, end to end (ADR 0078 amendment 2026-10-02, ADR
// 0092): a reply's thinking blocks go back upstream only over the prefix the reply was produced
// on. These tests drive the engine over a real anthropic-wire provider client on stubllm's
// in-process transport, on a model in the preserved-thinking set, and read the replayed blocks
// where the server got them — each logged request's body — because the codec, not the engine,
// decides what is sent.

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/stubllm"
)

// preservedThinkingModel is a model in the provider's preserved-thinking set, so the codec guards
// its replay with the prefix digest.
const preservedThinkingModel = "claude-opus-5-5"

// signedThinkingBlock is the wire form of signedThinkingTurnBlock: what the server sent, and so
// exactly what a replay must send back.
const signedThinkingBlock = `{"type":"thinking","thinking":"plan","signature":"sig-1"}`

// signedThinkingTurnBlock is the scripted thinking block every reply here opens with.
var signedThinkingTurnBlock = stubllm.Block{Type: "thinking", Thinking: "plan", Signature: "sig-1"}

// anthropicThinkingResponder plays turns over a real anthropic-wire provider client naming
// preservedThinkingModel, on stubllm's in-process transport, with retries off.
func anthropicThinkingResponder(t *testing.T, turns ...stubllm.Turn) *scriptedUpstream {
	t.Helper()
	server := stubllm.InProcess(t, stubllm.Script{Model: preservedThinkingModel, Turns: turns})
	client := provider.NewClient("http://stubllm", preservedThinkingModel,
		provider.WithHTTPClient(&http.Client{Transport: server.Transport()}),
		provider.WithMaxRetries(0),
		provider.WithWire(provider.WireAnthropic),
	)
	return &scriptedUpstream{Client: client, server: server}
}

// thinkingReplayConfig is cfg bound to preservedThinkingModel on the anthropic wire, the one
// wire and model a replay goes to (replayedThinking).
func thinkingReplayConfig(cfg domain.Config) domain.Config {
	cfg.Model = preservedThinkingModel
	cfg.Wire = string(provider.WireAnthropic)
	return cfg
}

// loggedMessages decodes a logged Messages request body: its system text and each message's
// content blocks, raw.
func loggedMessages(t *testing.T, body []byte) (string, [][]json.RawMessage) {
	t.Helper()
	var sent struct {
		System   string `json:"system"`
		Messages []struct {
			Content []json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &sent); err != nil {
		t.Fatalf("decode request body %s: %v", body, err)
	}
	contents := make([][]json.RawMessage, 0, len(sent.Messages))
	for _, m := range sent.Messages {
		contents = append(contents, m.Content)
	}
	return sent.System, contents
}

// sentReasoningBlocks is every thinking and redacted_thinking block a logged request body sends.
func sentReasoningBlocks(t *testing.T, body []byte) []string {
	t.Helper()
	_, contents := loggedMessages(t, body)
	var blocks []string
	for _, content := range contents {
		for _, block := range content {
			var typed struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(block, &typed); err != nil {
				t.Fatalf("decode block %s: %v", block, err)
			}
			if typed.Type == "thinking" || typed.Type == "redacted_thinking" {
				blocks = append(blocks, string(block))
			}
		}
	}
	return blocks
}

// lookTool is a read-only tool whose result never changes, so calling it changes nothing in the
// next request's prefix.
func lookTool() domain.Tool {
	return fakeTool{name: "look", readOnly: true, result: "seen"}
}

// TestThinkingReplay_UnchangedPrefixReplaysInPlace pins (a): inside a tool loop whose prefix does
// not change, the next request sends the first reply's signed thinking back byte for byte, in the
// place the reply had it — ahead of the tool call it led.
func TestThinkingReplay_UnchangedPrefixReplaysInPlace(t *testing.T) {
	t.Parallel()
	up := anthropicThinkingResponder(t,
		stubllm.Turn{Blocks: []stubllm.Block{
			signedThinkingTurnBlock,
			{Type: "tool_use", ID: "c1", Name: "look", Arguments: `{}`},
		}},
		stubllm.Turn{Text: "done"},
	)
	a, err := newAgent(thinkingReplayConfig(configWithTools(&recordingSink{}, lookTool())), up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	runOneExchange(t, a, "look around")

	requests := up.requests()
	if len(requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(requests))
	}
	_, contents := loggedMessages(t, requests[1].Body)
	want := `[` + signedThinkingBlock + `,{"type":"tool_use","id":"c1","name":"look","input":{}}]`
	if len(contents) < 2 || string(mustMarshal(t, contents[1])) != want {
		t.Errorf("second request's assistant turn = %s, want %s", contents, want)
	}
}

// TestThinkingReplay_ChangedSystemPromptDropsTheThinking pins (b): a task-list write changes the
// standing task-list block, so the next request's system text is not the one the first reply was
// produced on, and that reply's thinking is left off it while the call it made still goes.
func TestThinkingReplay_ChangedSystemPromptDropsTheThinking(t *testing.T) {
	t.Parallel()
	writer := &taskWriter{}
	up := anthropicThinkingResponder(t,
		stubllm.Turn{Blocks: []stubllm.Block{
			signedThinkingTurnBlock,
			{Type: "tool_use", ID: "c1", Name: "write_tasks", Arguments: `{"n":0}`},
		}},
		stubllm.Turn{Text: "done"},
	)
	cfg := thinkingReplayConfig(configWithTools(&recordingSink{}, writer.tool()))
	cfg.SystemPrompt = "You are terse."
	a, err := newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}

	runOneExchange(t, a, "plan the work")

	requests := up.requests()
	if len(requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(requests))
	}
	firstSystem, _ := loggedMessages(t, requests[0].Body)
	secondSystem, contents := loggedMessages(t, requests[1].Body)
	if firstSystem == secondSystem {
		t.Fatalf("system text unchanged by the task-list write (%q); the premise does not hold", firstSystem)
	}
	want := `[{"type":"tool_use","id":"c1","name":"write_tasks","input":{"n":0}}]`
	if len(contents) < 2 || string(mustMarshal(t, contents[1])) != want {
		t.Errorf("second request's assistant turn = %s, want %s", contents, want)
	}
}

// TestThinkingReplay_NoneAfterACompactionFold pins (c): a fold keeps no assistant reply that
// carried signed thinking, so the first request after it sends no thinking block at all.
func TestThinkingReplay_NoneAfterACompactionFold(t *testing.T) {
	t.Parallel()
	up := anthropicThinkingResponder(t,
		summaryTurn("FOLDED"),
		stubllm.Turn{Blocks: []stubllm.Block{signedThinkingTurnBlock, {Type: "text", Text: "first"}}},
		stubllm.Turn{Text: "done", Repeat: true},
	)
	a, err := newAgent(thinkingReplayConfig(baseConfig(&recordingSink{})), up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	runOneExchange(t, a, "start")
	foldOnce(t, a, up, 1)

	runOneExchange(t, a, "carry on")

	mains := up.mains()
	if got := sentReasoningBlocks(t, mains[len(mains)-1].Body); len(got) != 0 {
		t.Errorf("first request after the fold sends thinking %q, want none", got)
	}
}

// TestThinkingReplay_UndigestedEntryIsNotSent pins (d): a reasoning entry a session saved before
// digests existed — the bare block — still resumes into the request the engine builds, but the
// codec cannot show its prefix holds and leaves it off the wire.
func TestThinkingReplay_UndigestedEntryIsNotSent(t *testing.T) {
	t.Parallel()
	cfg := thinkingReplayConfig(baseConfig(&recordingSink{}))
	a, err := newAgent(cfg, scriptedDeltas{
		{Kind: provider.DeltaThinkingBlock, ThinkingBlock: json.RawMessage(signedThinkingBlock)},
		{Kind: provider.DeltaContent, Content: "the answer"},
		{Kind: provider.DeltaDone, FinishReason: "stop"},
	})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	runOneExchange(t, a, "q")
	snap, err := a.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	up := anthropicThinkingResponder(t, stubllm.Turn{Text: "next", Repeat: true})
	b, err := resumeAgent(cfg, snap, up)
	if err != nil {
		t.Fatalf("resumeAgent: %v", err)
	}
	built := b.toProviderRequest(domain.NewRequest(preservedThinkingModel, b.conv.Messages(), nil, domain.Budget{}, 0))
	if last := built.Messages[len(built.Messages)-1]; len(last.ThinkingBlocks) != 1 {
		t.Fatalf("resumed request carries %d thinking entries, want the saved one", len(last.ThinkingBlocks))
	}

	runOneExchange(t, b, "again")

	if got := sentReasoningBlocks(t, up.last().Body); len(got) != 0 {
		t.Errorf("request after resume sends thinking %q, want none", got)
	}
}

// mustMarshal is v as compact JSON.
func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal %v: %v", v, err)
	}
	return raw
}
