package agent

// Loop-level tests for the model-profile parse seam (item 2): a non-native profile's
// fenced/regex tool call is recovered from the visible content and its markup stripped from the
// committed assistant message; an inline thinking/harmony channel is removed from the final
// MessageEvent text and preserved as reasoning_content; the native structured call still wins;
// and a native profile is byte-identical. They drive the injected-fake responder through the
// unexported newAgent seam (see harness_test.go for the idiom).

import (
	"context"
	"encoding/json"
	"iter"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// customRegexToolPattern mirrors the apogee-code custom-regex oracle vector (JS named groups,
// which the parser rewrites to Go's (?P<name>…)).
const customRegexToolPattern = `<tool_call>(?<name>\w+)\((?<args>\{.*?\})\)</tool_call>`

// profileResponder streams one content chunk and, optionally, one out-of-band native tool call —
// the fake for exercising the native-vs-text precedence at the seam (D5).
type profileResponder struct {
	content string
	call    *provider.ToolCall
}

func (r profileResponder) Stream(context.Context, provider.Request) iter.Seq[provider.Delta] {
	return func(yield func(provider.Delta) bool) {
		if r.content != "" && !yield(provider.Delta{Kind: provider.DeltaContent, Content: r.content}) {
			return
		}
		if r.call != nil && !yield(provider.Delta{Kind: provider.DeltaToolCall, ToolCall: r.call}) {
			return
		}
		yield(provider.Delta{Kind: provider.DeltaDone, FinishReason: "stop"})
	}
}

// lastAssistantMessage returns the most recent committed assistant message — the one the seam
// wrote the stripped content and merged tool calls onto.
func lastAssistantMessage(t *testing.T, a *Agent) domain.Message {
	t.Helper()
	msgs := a.conv.Messages()
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == domain.RoleAssistant {
			return msgs[i]
		}
	}
	t.Fatal("no assistant message committed")
	return domain.Message{}
}

func newProfileAgent(t *testing.T, cfg domain.Config, responder provider.Responder) *Agent {
	t.Helper()
	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	return a
}

// TestProfile_MarkdownFencedCallParsedAndStripped: a markdown-fenced call in visible content is
// recovered as a domain.ToolCall (deterministic Turn-derived ID) and its markup leaves the
// committed assistant text.
func TestProfile_MarkdownFencedCallParsedAndStripped(t *testing.T) {
	sink := &recordingSink{}
	cfg := baseConfig(sink)
	cfg.Profile = domain.ModelProfile{ToolCallFormat: domain.FormatMarkdownFenced}

	content := "Let me read that file.\n\n```tool\nTOOL_NAME\nread_file\nBEGIN_ARG\npath\nEND_ARG\nmain.go\n```"
	a := newProfileAgent(t, cfg, echoResponder{reply: content})
	if err := a.Submit(domain.UserInput{Text: "read main.go"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	res, err := a.Step(context.Background())
	if err != nil {
		t.Fatalf("Step: %v", err)
	}
	if res.Status != domain.StatusTurnComplete {
		t.Fatalf("status = %q, want turn-complete (a tool was requested)", res.Status)
	}

	msg := lastAssistantMessage(t, a)
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("assistant ToolCalls = %d, want 1", len(msg.ToolCalls))
	}
	call := msg.ToolCalls[0]
	if call.Tool != "read_file" {
		t.Errorf("Tool = %q, want read_file", call.Tool)
	}
	if call.ID != "text_call_0" {
		t.Errorf("ID = %q, want text_call_0 (deterministic Turn-derived)", call.ID)
	}
	if string(call.Arguments) != `{"path":"main.go"}` {
		t.Errorf("Arguments = %s, want {\"path\":\"main.go\"}", call.Arguments)
	}
	if strings.Contains(msg.Content, "TOOL_NAME") || strings.Contains(msg.Content, "```") {
		t.Errorf("committed content still carries tool markup: %q", msg.Content)
	}
	if !strings.Contains(msg.Content, "Let me read that file.") {
		t.Errorf("committed content lost its prose: %q", msg.Content)
	}
	if !hasEvent[domain.ToolCallEvent](sink.events) {
		t.Error("no ToolCallEvent emitted for the text-parsed call")
	}
}

// TestProfile_CustomRegexCallParsedAndStripped: the same, via the custom-regex format.
func TestProfile_CustomRegexCallParsedAndStripped(t *testing.T) {
	sink := &recordingSink{}
	cfg := baseConfig(sink)
	cfg.Profile = domain.ModelProfile{ToolCallFormat: domain.FormatCustomRegex, Pattern: customRegexToolPattern}

	content := `Done. <tool_call>list_dir({"path":"."})</tool_call>`
	a := newProfileAgent(t, cfg, echoResponder{reply: content})
	if err := a.Submit(domain.UserInput{Text: "list"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	res, err := a.Step(context.Background())
	if err != nil {
		t.Fatalf("Step: %v", err)
	}
	if res.Status != domain.StatusTurnComplete {
		t.Fatalf("status = %q, want turn-complete", res.Status)
	}

	msg := lastAssistantMessage(t, a)
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("assistant ToolCalls = %d, want 1", len(msg.ToolCalls))
	}
	if msg.ToolCalls[0].Tool != "list_dir" {
		t.Errorf("Tool = %q, want list_dir", msg.ToolCalls[0].Tool)
	}
	if strings.Contains(msg.Content, "tool_call") {
		t.Errorf("committed content still carries tool markup: %q", msg.Content)
	}
	if msg.Content != "Done." {
		t.Errorf("committed content = %q, want %q", msg.Content, "Done.")
	}
}

// TestProfile_DelimitedThinkingStrippedAndPreserved: an inline <think> span is removed from the
// final MessageEvent and committed content, and kept as reasoning_content (the byte-identical
// history shape for the Upstream-split case).
func TestProfile_DelimitedThinkingStrippedAndPreserved(t *testing.T) {
	sink := &recordingSink{}
	cfg := baseConfig(sink)
	cfg.Profile = domain.ModelProfile{
		Thinking: domain.ThinkingProfile{Style: domain.ThinkingDelimited, Start: "<think>", End: "</think>"},
	}

	content := "<think>The user wants a greeting.</think>Hello there!"
	a := newProfileAgent(t, cfg, echoResponder{reply: content})
	if err := a.Submit(domain.UserInput{Text: "hi"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	res, err := a.Step(context.Background())
	if err != nil {
		t.Fatalf("Step: %v", err)
	}
	if res.Status != domain.StatusExchangeComplete {
		t.Fatalf("status = %q, want exchange-complete (no tool)", res.Status)
	}

	me, ok := firstMessageEvent(t, sink.events)
	if !ok || me.Text != "Hello there!" {
		t.Errorf("MessageEvent text = %q (ok=%v), want %q", me.Text, ok, "Hello there!")
	}

	msg := lastAssistantMessage(t, a)
	if msg.Content != "Hello there!" {
		t.Errorf("committed content = %q, want %q", msg.Content, "Hello there!")
	}
	assertReasoning(t, msg, "The user wants a greeting.")
}

// TestProfile_HarmonyThinkingStrippedAndPreserved: the same over the gpt-oss harmony channel set.
func TestProfile_HarmonyThinkingStrippedAndPreserved(t *testing.T) {
	sink := &recordingSink{}
	cfg := baseConfig(sink)
	cfg.Profile = domain.ModelProfile{Thinking: domain.ThinkingProfile{Style: domain.ThinkingHarmony}}

	content := "<|channel|>analysis<|message|>They asked for the time.<|end|><|channel|>final<|message|>It is noon.<|end|>"
	a := newProfileAgent(t, cfg, echoResponder{reply: content})
	if err := a.Submit(domain.UserInput{Text: "time?"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	res, err := a.Step(context.Background())
	if err != nil {
		t.Fatalf("Step: %v", err)
	}
	if res.Status != domain.StatusExchangeComplete {
		t.Fatalf("status = %q, want exchange-complete", res.Status)
	}

	me, ok := firstMessageEvent(t, sink.events)
	if !ok || me.Text != "It is noon." {
		t.Errorf("MessageEvent text = %q (ok=%v), want %q", me.Text, ok, "It is noon.")
	}
	msg := lastAssistantMessage(t, a)
	if msg.Content != "It is noon." {
		t.Errorf("committed content = %q, want %q", msg.Content, "It is noon.")
	}
	assertReasoning(t, msg, "They asked for the time.")
}

// TestProfile_NativeCallWinsOverText (D5): when the structured native path produced a call, it
// wins and the text parser does not run — the fenced markup stays in the committed content.
func TestProfile_NativeCallWinsOverText(t *testing.T) {
	sink := &recordingSink{}
	cfg := baseConfig(sink)
	cfg.Profile = domain.ModelProfile{ToolCallFormat: domain.FormatMarkdownFenced}

	content := "Working.\n\n```tool\nTOOL_NAME\nread_file\nBEGIN_ARG\npath\nEND_ARG\nmain.go\n```"
	nativeCall := &provider.ToolCall{
		ID:       "call_native_1",
		Type:     "function",
		Function: provider.FunctionCall{Name: "list_dir", Arguments: `{"path":"."}`},
	}
	responder := profileResponder{content: content, call: nativeCall}
	a := newProfileAgent(t, cfg, responder)
	if err := a.Submit(domain.UserInput{Text: "go"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	res, err := a.Step(context.Background())
	if err != nil {
		t.Fatalf("Step: %v", err)
	}
	if res.Status != domain.StatusTurnComplete {
		t.Fatalf("status = %q, want turn-complete", res.Status)
	}

	msg := lastAssistantMessage(t, a)
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("assistant ToolCalls = %d, want 1 (native only, text skipped)", len(msg.ToolCalls))
	}
	if msg.ToolCalls[0].Tool != "list_dir" || msg.ToolCalls[0].ID != "call_native_1" {
		t.Errorf("call = %+v, want the native list_dir/call_native_1", msg.ToolCalls[0])
	}
	if !strings.Contains(msg.Content, "TOOL_NAME") {
		t.Errorf("fenced markup was stripped even though the native call won: %q", msg.Content)
	}
}

// TestProfile_NativeProfileIsByteIdentical: a zero profile ignores text markup entirely — content
// commits verbatim, no call is parsed — proving the anchor.
func TestProfile_NativeProfileIsByteIdentical(t *testing.T) {
	sink := &recordingSink{}
	cfg := baseConfig(sink) // zero Profile

	content := "```tool\nTOOL_NAME\nread_file\nBEGIN_ARG\npath\nEND_ARG\nmain.go\n```"
	a := newProfileAgent(t, cfg, echoResponder{reply: content})
	if err := a.Submit(domain.UserInput{Text: "go"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	res, err := a.Step(context.Background())
	if err != nil {
		t.Fatalf("Step: %v", err)
	}
	if res.Status != domain.StatusExchangeComplete {
		t.Fatalf("status = %q, want exchange-complete (native ignores text markup)", res.Status)
	}

	me, ok := firstMessageEvent(t, sink.events)
	if !ok || me.Text != content {
		t.Errorf("MessageEvent text = %q (ok=%v), want the raw content verbatim", me.Text, ok)
	}
	msg := lastAssistantMessage(t, a)
	if msg.Content != content {
		t.Errorf("committed content = %q, want the raw content verbatim", msg.Content)
	}
	if len(msg.ToolCalls) != 0 {
		t.Errorf("assistant ToolCalls = %d, want 0", len(msg.ToolCalls))
	}
}

// assertReasoning checks the committed assistant message preserved want as reasoning_content.
func assertReasoning(t *testing.T, msg domain.Message, want string) {
	t.Helper()
	raw, ok := msg.Extra("reasoning_content")
	if !ok {
		t.Fatalf("reasoning_content not preserved on the assistant message")
	}
	var got string
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("reasoning_content is not a JSON string: %v", err)
	}
	if got != want {
		t.Errorf("reasoning_content = %q, want %q", got, want)
	}
}
