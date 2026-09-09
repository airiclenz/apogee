package agent

// The advise slot (ADR 0076 D6): an advise Reaction's text reaches the model as a fenced trailer
// on the CLOSING TOOL RESULT — never in the request prefix, which a local server's prefix cache
// depends on — and every injection is recorded on the message it advised. These tests drive the
// two seams the slot is made of, the post-tool-result cascade (firePostToolResult) and the one
// commit point every tool result crosses (appendToolResult), because that pair is what a Driver's
// history, ledger and session record are all built from.

import (
	"context"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// adviseReaction is an engine-origin Go advise reaction: it hands text to the model at
// post-tool-result and does nothing else, which is the whole of what class advise may do.
func adviseReaction(id, text string) domain.Reaction {
	return domain.Reaction{
		ID:     id,
		Origin: domain.OriginEngine,
		Class:  domain.ClassAdvise,
		On:     []domain.Moment{domain.MomentPostToolResult},
		Handler: domain.PostToolResultFunc(
			func(context.Context, domain.LoopView, domain.ToolCall, *domain.ToolResultEdit) (domain.Outcome, error) {
				return domain.Outcome{Inject: text}, nil
			}),
	}
}

// adviseAgent builds an Agent with the given advise reactions armed and Bypass as asked, plus one
// assistant message holding the tool call the results below answer.
func adviseAgent(t *testing.T, sink *recordingSink, bypass bool, reactions ...domain.Reaction) *Agent {
	t.Helper()

	cfg := configWithTools(sink)
	cfg.Reactions = reactions
	cfg.Bypass = bypass
	a, err := newAgent(cfg, echoResponder{reply: "unused"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	a.conv.Append(domain.Message{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "c1", Tool: "read_file"}}})
	return a
}

// adviseOneCall runs one finished tool call through the post-tool-result cascade and commits it,
// exactly as dispatchSerially does, and returns the committed tool message.
func adviseOneCall(t *testing.T, a *Agent, body string) domain.Message {
	t.Helper()

	result := domain.ToolResult{CallID: "c1", Content: body}
	advised := a.firePostToolResult(context.Background(), domain.ToolCall{ID: "c1", Tool: "read_file"}, &result)
	a.appendToolResult(0, result, advised)
	return a.conv.At(a.conv.Len() - 1)
}

// firedAdvice returns the ReactionFiredEvents the sink saw, in order.
func firedAdvice(sink *recordingSink) []domain.ReactionFiredEvent {
	var fired []domain.ReactionFiredEvent
	for _, e := range sink.events {
		if fe, ok := e.(domain.ReactionFiredEvent); ok {
			fired = append(fired, fe)
		}
	}
	return fired
}

// The slot's whole contract in one pass: the model sees the fence appended to the tool result it
// advised, the ledger records who injected it under what provenance, and the firing books the text
// itself so a bench can attribute an effect to the exact string the model read.
func TestAdviseSpanFencesTheClosingToolResult(t *testing.T) {
	sink := &recordingSink{}
	a := adviseAgent(t, sink, false, adviseReaction("advisor", "run the tests before you answer"))

	const body = "[File: main.go]\npackage main"
	msg := adviseOneCall(t, a, body)

	want := domain.AdviceSpan{
		Reaction: "advisor",
		Origin:   domain.OriginEngine,
		Moment:   domain.MomentPostToolResult,
		Turn:     0,
		Offset:   len(body),
	}
	if len(msg.Advice) != 1 || msg.Advice[0] != want {
		t.Fatalf("ledger = %+v, want exactly %+v", msg.Advice, want)
	}
	if got := msg.Content; got != body+domain.RenderAdvice(want, "run the tests before you answer") {
		t.Errorf("advised content = %q, want the body followed by the rendered fence", got)
	}

	fired := firedAdvice(sink)
	if len(fired) != 1 {
		t.Fatalf("saw %d firings, want 1: %+v", len(fired), fired)
	}
	if fired[0].Action != "advise" || fired[0].Detail != "run the tests before you answer" {
		t.Errorf("firing = {Action:%q Detail:%q}, want {advise, the injected text}", fired[0].Action, fired[0].Detail)
	}
}

// Two advise reactions are two spans, in LADDER order, each pointing at its own fence: the ledger
// is what attributes an effect to one reaction rather than to whichever spoke last, so the offsets
// must survive a second injection landing after the first.
func TestAdviseSpansStackInLadderOrder(t *testing.T) {
	sink := &recordingSink{}
	a := adviseAgent(t, sink, false, adviseReaction("first", "one"), adviseReaction("second", "two"))

	const body = "result"
	msg := adviseOneCall(t, a, body)

	if len(msg.Advice) != 2 {
		t.Fatalf("ledger holds %d spans, want 2: %+v", len(msg.Advice), msg.Advice)
	}
	if msg.Advice[0].Reaction != "first" || msg.Advice[1].Reaction != "second" {
		t.Errorf("ledger order = %q, %q, want the ladder's order (first, second)",
			msg.Advice[0].Reaction, msg.Advice[1].Reaction)
	}
	firstFence := domain.RenderAdvice(msg.Advice[0], "one")
	if msg.Advice[0].Offset != len(body) || msg.Advice[1].Offset != len(body)+len(firstFence) {
		t.Errorf("offsets = %d, %d, want %d, %d — each span starts where its own fence does",
			msg.Advice[0].Offset, msg.Advice[1].Offset, len(body), len(body)+len(firstFence))
	}
	if got := msg.Content; got != body+firstFence+domain.RenderAdvice(msg.Advice[1], "two") {
		t.Errorf("advised content = %q, want both fences appended in ladder order", got)
	}
}

// A handler that returns a book cannot spend the Turn's context on it: the cap is applied ONCE, in
// the dispatcher, so the fence and the firing's Detail carry the same truncated string.
func TestAdviseTextIsCappedBeforeItIsFenced(t *testing.T) {
	sink := &recordingSink{}
	huge := strings.Repeat("x", 9<<10)
	a := adviseAgent(t, sink, false, adviseReaction("verbose", huge))

	msg := adviseOneCall(t, a, "body")

	capped := domain.CapAdvice(huge)
	if !strings.Contains(msg.Content, capped) {
		t.Errorf("advised content does not carry the capped text (%d bytes)", len(capped))
	}
	if strings.Contains(msg.Content, huge) {
		t.Errorf("advised content carries the whole %d-byte text, want it capped at %d", len(huge), domain.AdviceCap)
	}
	if !strings.Contains(msg.Content, "[advice truncated at 8 KiB]") {
		t.Error("advised content carries no truncation marker, so the model cannot tell it was cut")
	}
	if fired := firedAdvice(sink); len(fired) != 1 || fired[0].Detail != capped {
		t.Errorf("firing Detail is not the capped text — a bench would hash a string the model never read")
	}
}

// Bypass switches advise OFF (D9), and a switched-off reaction is silent: no trailer for the model,
// no span in the ledger, and no firing to read back.
func TestAdviseIsSilentUnderBypass(t *testing.T) {
	sink := &recordingSink{}
	a := adviseAgent(t, sink, true, adviseReaction("advisor", "guidance"))

	msg := adviseOneCall(t, a, "body")

	if msg.Content != "body" || len(msg.Advice) != 0 {
		t.Errorf("under Bypass the committed message = %q with %d spans, want the bare body and none",
			msg.Content, len(msg.Advice))
	}
	if fired := firedAdvice(sink); len(fired) != 0 {
		t.Errorf("under Bypass the cascade booked %+v, want no firing at all", fired)
	}
}

// A delegation's result closes the same way a leaf call's does — commitDelegation runs the same
// cascade and the same commit — so a child's result carries the trailer too. The fan-out is the one
// path that could have grown a second commit point; it must not have.
func TestAdviseFencesADelegationResult(t *testing.T) {
	sink := &recordingSink{}
	a := adviseAgent(t, sink, false, adviseReaction("advisor", "check the child's claim"))

	slot := &fanOutSlot{
		call:   domain.ToolCall{ID: "c1", Tool: "delegate"},
		result: domain.ToolResult{CallID: "c1", Content: "the child reported back"},
	}
	a.commitDelegation(context.Background(), 0, slot)

	msg := a.conv.At(a.conv.Len() - 1)
	if len(msg.Advice) != 1 || msg.Advice[0].Reaction != "advisor" {
		t.Fatalf("delegation result carries ledger %+v, want one span from advisor", msg.Advice)
	}
	if !strings.HasSuffix(msg.Content, domain.RenderAdvice(msg.Advice[0], "check the child's claim")) {
		t.Errorf("delegation result = %q, want it to end with the rendered fence", msg.Content)
	}
}

// Advice is EPHEMERAL: the session record is written from the content before the first fence, so a
// resumed session carries neither the trailer nor its ledger. Without this a replay would re-read a
// stale snapshot as though the advising reaction had spoken again.
func TestAdviseNeverSurvivesASnapshotResume(t *testing.T) {
	sink := &recordingSink{}
	a := adviseAgent(t, sink, false, adviseReaction("advisor", "guidance for this Turn only"))

	const body = "the tool's own output"
	adviseOneCall(t, a, body)

	snap, err := a.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	restored, err := newAgent(configWithTools(&recordingSink{}), echoResponder{reply: "unused"})
	if err != nil {
		t.Fatalf("newAgent (restore target): %v", err)
	}
	if err := restored.RestoreSession(snap); err != nil {
		t.Fatalf("RestoreSession: %v", err)
	}

	msg := restored.conv.At(restored.conv.Len() - 1)
	if msg.Content != body || len(msg.Advice) != 0 {
		t.Errorf("resumed tool message = %q with %d spans, want the bare body and no ledger",
			msg.Content, len(msg.Advice))
	}
}
