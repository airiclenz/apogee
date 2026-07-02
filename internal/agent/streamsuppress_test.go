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

// chunkedResponder streams a fixed sequence of content chunks (one DeltaContent each) then a
// terminal Done — the fake for exercising incremental token emission across delta boundaries.
// It mirrors the provider's own contract of never yielding an empty content chunk (stream.go).
type chunkedResponder struct {
	chunks []string
}

func (r chunkedResponder) Stream(context.Context, provider.Request) iter.Seq[provider.Delta] {
	return func(yield func(provider.Delta) bool) {
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
// verbatim and in order — the strict no-op anchor (item 3 must never buffer a native stream).
func TestStream_NativeIsByteIdentical(t *testing.T) {
	sink := &recordingSink{}
	cfg := baseConfig(sink) // zero Profile == native, no inline thinking
	chunks := []string{"Hello, ", "world", "!"}

	a := newProfileAgent(t, cfg, chunkedResponder{chunks: chunks})
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
}
