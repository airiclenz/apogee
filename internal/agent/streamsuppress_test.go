package agent

// Item-3 tests for the streaming suppression seam: while the model streams an inline
// thinking/harmony channel, streamResponse HOLDS token emission so the channel markup and its
// reasoning never surface on the live TokenEvent stream; the visible text is revealed once the
// span closes, and a native profile streams every content delta verbatim and unbuffered
// (byte-identical, event-for-event). Channel tokens are chunked WHOLE — a token split across
// deltas leaks its partial prefix live by design (the recorded chunk-boundary edge), so a
// mid-token-split assertion would fail on purpose and is deliberately avoided here.

import (
	"context"
	"iter"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// chunkedResponder streams a fixed sequence of native reasoning chunks (one DeltaThinking each,
// the way a reasoning model front-loads reasoning_content) followed by the content chunks (one
// DeltaContent each) and a terminal Done — the fake for exercising incremental token emission
// across delta boundaries. It mirrors the provider's own contract of never yielding an empty
// content or thinking chunk (stream.go).
type chunkedResponder struct {
	thinking []string
	chunks   []string
}

func (r chunkedResponder) Stream(context.Context, provider.Request) iter.Seq[provider.Delta] {
	return func(yield func(provider.Delta) bool) {
		for _, th := range r.thinking {
			if th != "" && !yield(provider.Delta{Kind: provider.DeltaThinking, Thinking: th}) {
				return
			}
		}
		for _, c := range r.chunks {
			if c != "" && !yield(provider.Delta{Kind: provider.DeltaContent, Content: c}) {
				return
			}
		}
		yield(provider.Delta{Kind: provider.DeltaDone, FinishReason: "stop"})
	}
}

// tokenTexts returns the Text of every TokenEvent in order — the live stream a UI would render.
func tokenTexts(events []domain.Event) []string {
	var out []string
	for _, e := range events {
		if te, ok := e.(domain.TokenEvent); ok {
			out = append(out, te.Text)
		}
	}
	return out
}

// reasoningTexts returns the Text of every ReasoningEvent in order — the liveness stream a UI
// reads to know the model is reasoning while the visible stream is silent.
func reasoningTexts(events []domain.Event) []string {
	var out []string
	for _, e := range events {
		if re, ok := e.(domain.ReasoningEvent); ok {
			out = append(out, re.Text)
		}
	}
	return out
}

// assertNoVisibleInReasoning fails if any emitted ReasoningEvent carries user-facing text — the
// mirror of assertNoLeak for the other half of the split.
func assertNoVisibleInReasoning(t *testing.T, reasoning, visible []string) {
	t.Helper()
	for i, r := range reasoning {
		for _, v := range visible {
			if strings.Contains(r, v) {
				t.Errorf("ReasoningEvent[%d] = %q carried visible content %q", i, r, v)
			}
		}
	}
}

// assertNoLeak fails if any emitted TokenEvent carries channel markup or reasoning text.
func assertNoLeak(t *testing.T, tokens, leaks []string) {
	t.Helper()
	for i, tok := range tokens {
		for _, leak := range leaks {
			if strings.Contains(tok, leak) {
				t.Errorf("TokenEvent[%d] = %q leaked channel content %q onto the live stream", i, tok, leak)
			}
		}
	}
}

// TestStream_NativeIsByteIdentical: a native profile emits one TokenEvent per content delta,
// verbatim and in order — the strict no-op anchor (item 3 must never buffer a native stream) —
// and one ReasoningEvent per reasoning_content delta, the native half of the reasoning seam.
func TestStream_NativeIsByteIdentical(t *testing.T) {
	sink := &recordingSink{}
	cfg := baseConfig(sink) // zero Profile == native, no inline thinking
	chunks := []string{"Hello, ", "world", "!"}
	thinking := []string{"Weighing ", "the greeting."}

	a := newProfileAgent(t, cfg, chunkedResponder{thinking: thinking, chunks: chunks})
	if err := a.Submit(domain.UserInput{Text: "hi"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Step(context.Background()); err != nil {
		t.Fatalf("Step: %v", err)
	}

	got := tokenTexts(sink.events)
	if len(got) != len(chunks) {
		t.Fatalf("emitted %d TokenEvents, want %d (event-for-event): %q", len(got), len(chunks), got)
	}
	for i, want := range chunks {
		if got[i] != want {
			t.Errorf("TokenEvent[%d] = %q, want %q (verbatim, unbuffered)", i, got[i], want)
		}
	}
	if me, ok := firstMessageEvent(t, sink.events); !ok || me.Text != "Hello, world!" {
		t.Errorf("final MessageEvent = %q (ok=%v), want %q", me.Text, ok, "Hello, world!")
	}

	reasoning := reasoningTexts(sink.events)
	if len(reasoning) != len(thinking) {
		t.Fatalf("emitted %d ReasoningEvents, want %d (one per DeltaThinking): %q", len(reasoning), len(thinking), reasoning)
	}
	for i, want := range thinking {
		if reasoning[i] != want {
			t.Errorf("ReasoningEvent[%d] = %q, want %q (verbatim, unbuffered)", i, reasoning[i], want)
		}
	}
	assertNoVisibleInReasoning(t, reasoning, chunks)
	// The chunks concatenate to exactly what the committed message preserves as reasoning_content.
	assertReasoning(t, lastAssistantMessage(t, a), strings.Join(thinking, ""))
}

// TestStream_DelimitedThinkingHeldOffLiveStream: an inline <think> span streamed in whole-token
// chunks never surfaces its markup or analysis text on the live TokenEvent stream, and the joined
// live text matches the final visible message (item 2), with the reasoning preserved.
func TestStream_DelimitedThinkingHeldOffLiveStream(t *testing.T) {
	sink := &recordingSink{}
	cfg := baseConfig(sink)
	cfg.Profile = domain.ModelProfile{
		Thinking: domain.ThinkingProfile{Style: domain.ThinkingDelimited, Start: "<think>", End: "</think>"},
	}
	// The channel tokens (<think>, </think>) each arrive as their own WHOLE chunk.
	chunks := []string{"Let me check. ", "<think>", "The user said hi.", "</think>", "Hello there!"}

	a := newProfileAgent(t, cfg, chunkedResponder{chunks: chunks})
	if err := a.Submit(domain.UserInput{Text: "hi"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Step(context.Background()); err != nil {
		t.Fatalf("Step: %v", err)
	}

	got := tokenTexts(sink.events)
	assertNoLeak(t, got, []string{"<think>", "</think>", "The user said hi."})

	const wantVisible = "Let me check. Hello there!"
	if joined := strings.Join(got, ""); joined != wantVisible {
		t.Errorf("joined live tokens = %q, want %q", joined, wantVisible)
	}
	if me, ok := firstMessageEvent(t, sink.events); !ok || me.Text != wantVisible {
		t.Errorf("final MessageEvent = %q (ok=%v), want %q", me.Text, ok, wantVisible)
	}
	assertReasoning(t, lastAssistantMessage(t, a), "The user said hi.")

	// The span the visible stream held back still reports liveness, and concatenates to the
	// reasoning the post-stream strip records.
	reasoning := reasoningTexts(sink.events)
	if len(reasoning) == 0 {
		t.Fatal("no ReasoningEvent emitted for the held <think> span")
	}
	if joined := strings.Join(reasoning, ""); joined != "The user said hi." {
		t.Errorf("joined ReasoningEvents = %q, want %q", joined, "The user said hi.")
	}
	assertNoVisibleInReasoning(t, reasoning, []string{"Let me check.", "Hello there!"})
}

// TestStream_HarmonyChannelsHeldOffLiveStream: the gpt-oss harmony analysis channel is held off
// the live stream; only the final channel's answer streams, the joined live text matches the
// final visible message, and the analysis text is preserved as reasoning.
func TestStream_HarmonyChannelsHeldOffLiveStream(t *testing.T) {
	sink := &recordingSink{}
	cfg := baseConfig(sink)
	cfg.Profile = domain.ModelProfile{Thinking: domain.ThinkingProfile{Style: domain.ThinkingHarmony}}
	// Each harmony control token arrives whole (the analysis channel opens, streams, and closes
	// before the final channel opens).
	chunks := []string{
		"<|channel|>analysis<|message|>", "Working it out.", "<|end|>",
		"<|channel|>final<|message|>", "The answer is 42.", "<|end|>",
	}

	a := newProfileAgent(t, cfg, chunkedResponder{chunks: chunks})
	if err := a.Submit(domain.UserInput{Text: "answer?"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Step(context.Background()); err != nil {
		t.Fatalf("Step: %v", err)
	}

	got := tokenTexts(sink.events)
	assertNoLeak(t, got, []string{"<|channel|>", "<|message|>", "analysis", "Working it out."})

	const wantVisible = "The answer is 42."
	if joined := strings.Join(got, ""); joined != wantVisible {
		t.Errorf("joined live tokens = %q, want %q", joined, wantVisible)
	}
	if me, ok := firstMessageEvent(t, sink.events); !ok || me.Text != wantVisible {
		t.Errorf("final MessageEvent = %q (ok=%v), want %q", me.Text, ok, wantVisible)
	}
	assertReasoning(t, lastAssistantMessage(t, a), "Working it out.")

	reasoning := reasoningTexts(sink.events)
	if len(reasoning) == 0 {
		t.Fatal("no ReasoningEvent emitted for the held analysis channel")
	}
	if joined := strings.Join(reasoning, ""); joined != "Working it out." {
		t.Errorf("joined ReasoningEvents = %q, want %q", joined, "Working it out.")
	}
	assertNoVisibleInReasoning(t, reasoning, []string{"The answer is 42."})
}

// TestStream_ReasoningSurvivesSplitChannelTokens: a channel token split across two deltas — the
// recorded chunk-boundary edge — must not panic the reasoning emitter and must not re-emit
// reasoning already sent. A split START token briefly hides the span from the stripper; a split
// END token briefly counts its own partial markup as span text and then SHRINKS the accumulated
// reasoning, which is exactly what emitReasoningDelta's length guard exists to survive.
func TestStream_ReasoningSurvivesSplitChannelTokens(t *testing.T) {
	tests := []struct {
		name    string
		chunks  []string
		body    string // the reasoning the post-stream strip records
		visible string // must never appear in a ReasoningEvent
	}{
		{
			name:    "start token split across deltas",
			chunks:  []string{"Hi ", "<thi", "nk>", "secret", "</think>", "done"},
			body:    "secret",
			visible: "done",
		},
		{
			name:    "end token split across deltas",
			chunks:  []string{"<think>", "secret", "</thi", "nk>", "done"},
			body:    "secret",
			visible: "done",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sink := &recordingSink{}
			cfg := baseConfig(sink)
			cfg.Profile = domain.ModelProfile{
				Thinking: domain.ThinkingProfile{Style: domain.ThinkingDelimited, Start: "<think>", End: "</think>"},
			}

			a := newProfileAgent(t, cfg, chunkedResponder{chunks: tc.chunks})
			if err := a.Submit(domain.UserInput{Text: "hi"}); err != nil {
				t.Fatalf("Submit: %v", err)
			}
			if _, err := a.Step(context.Background()); err != nil {
				t.Fatalf("Step: %v", err)
			}

			assertReasoning(t, lastAssistantMessage(t, a), tc.body)

			reasoning := reasoningTexts(sink.events)
			joined := strings.Join(reasoning, "")
			if !strings.HasPrefix(joined, tc.body) {
				t.Errorf("joined ReasoningEvents = %q, want it to start with the recorded reasoning %q", joined, tc.body)
			}
			if n := strings.Count(joined, tc.body); n != 1 {
				t.Errorf("reasoning body appears %d times in %q, want exactly 1 (no double-emit)", n, joined)
			}
			assertNoVisibleInReasoning(t, reasoning, []string{tc.visible})
		})
	}
}
