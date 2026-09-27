package agent

// Item-3 tests for the streaming suppression seam: while the model streams an inline
// thinking/harmony channel, streamResponse HOLDS token emission so the channel markup and its
// reasoning never surface on the live TokenEvent stream; the visible text is revealed once the
// span closes, and a native profile streams every content delta verbatim and unbuffered
// (byte-identical, event-for-event) up to the strip throttle's floor, past which a pass runs
// once per stride. Channel tokens are chunked WHOLE — a token split across
// deltas leaks its partial prefix live by design (the recorded chunk-boundary edge), so a
// mid-token-split assertion would fail on purpose and is deliberately avoided here.

import (
	"context"
	"iter"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/processing"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/stubllm"
)

// chunkedResponder streams a fixed sequence of native reasoning deltas (the way a reasoning model
// front-loads reasoning_content) followed by the content deltas at the boundaries the test placed
// by hand — the upstream for exercising incremental token emission across delta boundaries. The
// stub refuses an empty chunk, the provider's own contract of never yielding an empty content or
// thinking chunk (stream.go).
func chunkedResponder(t testing.TB, thinking, chunks []string) *scriptedUpstream {
	t.Helper()
	return scriptedResponder(t, stubllm.Turn{ReasoningChunks: thinking, Chunks: chunks})
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

	a := newProfileAgent(t, cfg, chunkedResponder(t, thinking, chunks))
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

	a := newProfileAgent(t, cfg, chunkedResponder(t, nil, chunks))
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

	a := newProfileAgent(t, cfg, chunkedResponder(t, nil, chunks))
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

			a := newProfileAgent(t, cfg, chunkedResponder(t, nil, tc.chunks))
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

// preOpenedProfile is the shipped minimax-m3 shape: a delimited channel the chat template opens
// before the model's first byte, so a non-splitting server's reply carries only the closer.
func preOpenedProfile() domain.ModelProfile {
	return domain.ModelProfile{
		Thinking: domain.ThinkingProfile{
			Style: domain.ThinkingDelimited, Start: "<mm:think>", End: "</mm:think>", PreOpened: true,
		},
	}
}

// TestStream_PreOpenedThinkingHeldOffLiveStream: a pre-opened channel's reply opens mid-think —
// no opener, only the closer — so the reasoning before the closer is held off the live
// TokenEvent stream (the audit's High), the visible text after it streams, and the hold is
// SILENT: exactly one ReasoningEvent fires, at the closer, carrying the whole pre-opened text.
func TestStream_PreOpenedThinkingHeldOffLiveStream(t *testing.T) {
	sink := &recordingSink{}
	cfg := baseConfig(sink)
	cfg.Profile = preOpenedProfile()
	chunks := []string{"The user ", "said hi.", "</mm:think>", "Hello there!"}

	a := newProfileAgent(t, cfg, chunkedResponder(t, nil, chunks))
	if err := a.Submit(domain.UserInput{Text: "hi"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Step(context.Background()); err != nil {
		t.Fatalf("Step: %v", err)
	}

	got := tokenTexts(sink.events)
	assertNoLeak(t, got, []string{"</mm:think>", "The user", "said hi."})

	const wantVisible = "Hello there!"
	if joined := strings.Join(got, ""); joined != wantVisible {
		t.Errorf("joined live tokens = %q, want %q", joined, wantVisible)
	}
	if me, ok := firstMessageEvent(t, sink.events); !ok || me.Text != wantVisible {
		t.Errorf("final MessageEvent = %q (ok=%v), want %q", me.Text, ok, wantVisible)
	}
	assertReasoning(t, lastAssistantMessage(t, a), "The user said hi.")

	reasoning := reasoningTexts(sink.events)
	if len(reasoning) != 1 || reasoning[0] != "The user said hi." {
		t.Fatalf("ReasoningEvents = %q, want exactly one carrying %q, emitted at the closer", reasoning, "The user said hi.")
	}
	assertNoVisibleInReasoning(t, reasoning, []string{"Hello there!"})
}

// TestStream_PreOpenedSplitReasoningStreamsLive: the same pre-opened profile on a server that
// splits reasoning into reasoning_content — the server consumed the pre-opened span itself, so
// the first split delta releases the hold and the content streams live, before the MessageEvent,
// exactly as it does today.
func TestStream_PreOpenedSplitReasoningStreamsLive(t *testing.T) {
	sink := &recordingSink{}
	cfg := baseConfig(sink)
	cfg.Profile = preOpenedProfile()
	chunks := []string{"Hello, ", "world", "!"}
	thinking := []string{"Weighing ", "the greeting."}

	a := newProfileAgent(t, cfg, chunkedResponder(t, thinking, chunks))
	if err := a.Submit(domain.UserInput{Text: "hi"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Step(context.Background()); err != nil {
		t.Fatalf("Step: %v", err)
	}

	var beforeMessage []domain.Event
	for _, e := range sink.events {
		if _, ok := e.(domain.MessageEvent); ok {
			break
		}
		beforeMessage = append(beforeMessage, e)
	}
	// One TokenEvent per content delta, all ahead of the MessageEvent: live, not held. The
	// delimited stripper trims the visible text it reveals, so the boundaries are asserted by
	// count and the text by its join, as every delimited-profile stream test does.
	got := tokenTexts(beforeMessage)
	if len(got) != len(chunks) {
		t.Fatalf("emitted %d TokenEvents before the MessageEvent, want %d (live, not held): %q", len(got), len(chunks), got)
	}
	if joined := strings.Join(got, ""); joined != "Hello, world!" {
		t.Errorf("joined live tokens = %q, want %q", joined, "Hello, world!")
	}
	if me, ok := firstMessageEvent(t, sink.events); !ok || me.Text != "Hello, world!" {
		t.Errorf("final MessageEvent = %q (ok=%v), want %q", me.Text, ok, "Hello, world!")
	}
	if reasoning := reasoningTexts(sink.events); strings.Join(reasoning, "") != strings.Join(thinking, "") {
		t.Errorf("joined ReasoningEvents = %q, want %q", strings.Join(reasoning, ""), strings.Join(thinking, ""))
	}
}

// countingStripper wraps the profile's real ContentStripper and counts the calls made to it:
// passes is how many live strip passes ran (each one opens with IsMidChannel), calls every call,
// the collector's post-drain Strip included.
type countingStripper struct {
	inner  processing.ContentStripper
	passes int
	calls  int
}

func (c *countingStripper) Strip(raw string) (string, string) {
	c.calls++
	return c.inner.Strip(raw)
}

func (c *countingStripper) IsMidChannel(raw string, splitSeen bool) bool {
	c.passes++
	c.calls++
	return c.inner.IsMidChannel(raw, splitSeen)
}

// byteResponder streams reply one byte per content delta, then a terminal Done — the in-process
// upstream for a reply too long to push through stubllm's wire one byte at a time.
type byteResponder struct{ reply string }

func (r byteResponder) Stream(_ context.Context, _ provider.Request) iter.Seq[provider.Delta] {
	return func(yield func(provider.Delta) bool) {
		for i := range len(r.reply) {
			if !yield(provider.Delta{Kind: provider.DeltaContent, Content: r.reply[i : i+1]}) {
				return
			}
		}
		yield(provider.Delta{Kind: provider.DeltaDone, FinishReason: "stop"})
	}
}

// stepWithCountingStripper submits one prompt to a and runs one Step with a's stripper wrapped in
// a countingStripper, which it returns.
func stepWithCountingStripper(t *testing.T, a *Agent) *countingStripper {
	t.Helper()
	counter := &countingStripper{inner: a.stripper}
	a.stripper = counter
	if err := a.Submit(domain.UserInput{Text: "hi"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Step(context.Background()); err != nil {
		t.Fatalf("Step: %v", err)
	}
	return counter
}

// TestStream_LargeReplyThrottlesStripPasses: a 2 MiB reply in 1-byte deltas is stripped once per
// delta only up to the throttle floor and once per stride past it — not once per delta, which
// re-reads the whole accumulation 2 Mi times — while below the floor every delta still streams
// verbatim and the live stream and the committed message both end on the whole reply.
func TestStream_LargeReplyThrottlesStripPasses(t *testing.T) {
	const replySize = 2 << 20
	reply := strings.Repeat("abcdefg\n", replySize/8)
	sink := &recordingSink{}
	a := newProfileAgent(t, baseConfig(sink), byteResponder{reply: reply})

	counter := stepWithCountingStripper(t, a)

	// One pass per delta to the floor, one per stride past it, one terminal flush, and slack.
	const maxPasses = streamStripFloor + (replySize-streamStripFloor)/streamStripStride + 4
	if counter.passes > maxPasses {
		t.Errorf("ran %d live strip passes over a %d-byte reply, want at most %d (throttled past %d bytes)",
			counter.passes, replySize, maxPasses, streamStripFloor)
	}
	// Each pass costs IsMidChannel plus the visible and the reasoning Strip; the collector strips once.
	if maxCalls := 3*maxPasses + 1; counter.calls > maxCalls {
		t.Errorf("made %d stripper calls, want at most %d", counter.calls, maxCalls)
	}

	tokens := tokenTexts(sink.events)
	if len(tokens) < streamStripFloor {
		t.Fatalf("emitted %d TokenEvents, want at least %d (one per delta below the floor)", len(tokens), streamStripFloor)
	}
	for i := range streamStripFloor {
		if tokens[i] != reply[i:i+1] {
			t.Fatalf("TokenEvent[%d] = %q, want %q (verbatim, unbuffered below the floor)", i, tokens[i], reply[i:i+1])
		}
	}
	if joined := strings.Join(tokens, ""); joined != reply {
		t.Errorf("joined live tokens are %d bytes, want the whole %d-byte reply", len(joined), len(reply))
	}
	if me, ok := firstMessageEvent(t, sink.events); !ok || me.Text != reply {
		t.Errorf("final MessageEvent is %d bytes (ok=%v), want the whole %d-byte reply", len(me.Text), ok, len(reply))
	}
}

// TestStream_ThrottledThinkingCommitsUnthrottledResult: a delimited reply whose <think> span
// streams past the throttle floor skips strip passes there, yet never leaks the span onto the
// live stream, and its live stream, committed message and reasoning are exactly what one strip of
// the full reply yields.
func TestStream_ThrottledThinkingCommitsUnthrottledResult(t *testing.T) {
	const chunkSize = 1 << 10
	profile := domain.ModelProfile{
		Thinking: domain.ThinkingProfile{Style: domain.ThinkingDelimited, Start: "<think>", End: "</think>"},
	}
	var chunks []string
	appendChunks := func(text string, count int) {
		for range count {
			chunks = append(chunks, text)
		}
	}
	const visibleChunk, reasoningChunk = "Visible words.", "Pondering it."
	visible := strings.Repeat(visibleChunk, chunkSize/len(visibleChunk)+1)[:chunkSize]
	reasoning := strings.Repeat(reasoningChunk, chunkSize/len(reasoningChunk)+1)[:chunkSize]
	appendChunks(visible, 300)
	appendChunks("<think>", 1)
	appendChunks(reasoning, 150)
	appendChunks("</think>", 1)
	appendChunks(visible, 100)
	full := strings.Join(chunks, "")

	_, stripper, err := processing.ParserFor(profile)
	if err != nil {
		t.Fatalf("ParserFor: %v", err)
	}
	wantVisible, wantReasoning := stripper.Strip(full)

	sink := &recordingSink{}
	cfg := baseConfig(sink)
	cfg.Profile = profile
	a := newProfileAgent(t, cfg, chunkedResponder(t, nil, chunks))

	counter := stepWithCountingStripper(t, a)

	if counter.passes >= len(chunks) {
		t.Errorf("ran %d live strip passes over %d content deltas, want fewer (throttled past %d bytes)",
			counter.passes, len(chunks), streamStripFloor)
	}
	tokens := tokenTexts(sink.events)
	assertNoLeak(t, tokens, []string{"<think>", "</think>", reasoningChunk})
	if joined := strings.Join(tokens, ""); joined != wantVisible {
		t.Errorf("joined live tokens are %d bytes, want the %d-byte unthrottled visible text", len(joined), len(wantVisible))
	}
	if me, ok := firstMessageEvent(t, sink.events); !ok || me.Text != wantVisible {
		t.Errorf("final MessageEvent is %d bytes (ok=%v), want the %d-byte unthrottled visible text", len(me.Text), ok, len(wantVisible))
	}
	assertReasoning(t, lastAssistantMessage(t, a), wantReasoning)
	if joined := strings.Join(reasoningTexts(sink.events), ""); joined != wantReasoning {
		t.Errorf("joined ReasoningEvents are %d bytes, want the %d-byte unthrottled reasoning", len(joined), len(wantReasoning))
	}
}
