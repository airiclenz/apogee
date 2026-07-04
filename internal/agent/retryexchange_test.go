package agent

// The retry-in-place corrective exchange (R1, phase-4-review-fixes item 1): an
// ActionRetry{Inject} post-response decision re-streams the corrected request in the same
// Turn — the loop appends the superseded assistant message (text + tool calls) and then
// the role-safe user correction to the in-flight request, request-scoped, never committed
// to history. These tests drive the seam end-to-end through a request-capturing scripted
// responder and assert the shape of every provider request the retry sent.

import (
	"context"
	"iter"
	"reflect"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// captureAllResponder yields a pre-scripted stream per call and records EVERY request it
// was handed, so a test can assert what the retried (second, third, …) request carried.
type captureAllResponder struct {
	scripts [][]provider.Delta
	got     []provider.Request
}

func (r *captureAllResponder) Stream(_ context.Context, req provider.Request) iter.Seq[provider.Delta] {
	i := len(r.got)
	r.got = append(r.got, req)
	return func(yield func(provider.Delta) bool) {
		if i >= len(r.scripts) {
			yield(provider.Delta{Kind: provider.DeltaError, Err: "captureAllResponder: out of scripts"})
			return
		}
		for _, d := range r.scripts[i] {
			if !yield(d) {
				return
			}
		}
	}
}

// scriptedRetryHook returns ActionRetry with injects[n] on its n-th invocation, then lets
// the response stand — distinct per-attempt texts prove corrections accumulate.
type scriptedRetryHook struct {
	injects []string
	calls   *int
}

func (h scriptedRetryHook) PostResponse(context.Context, *domain.Response) (domain.PostResponseDecision, error) {
	i := *h.calls
	*h.calls++
	if i >= len(h.injects) {
		return domain.PostResponseDecision{}, nil
	}
	return domain.PostResponseDecision{Action: domain.ActionRetry, Inject: h.injects[i]}, nil
}

// alwaysRetryHook retries with the same correction on every response — the cap driver.
type alwaysRetryHook struct{ inject string }

func (h alwaysRetryHook) PostResponse(context.Context, *domain.Response) (domain.PostResponseDecision, error) {
	return domain.PostResponseDecision{Action: domain.ActionRetry, Inject: h.inject}, nil
}

// draftWithToolCall is a stream that emits narration content plus one native tool call —
// the superseded draft whose text AND calls must ride the retried request.
func draftWithToolCall(text, id, name, args string) []provider.Delta {
	return []provider.Delta{
		{Kind: provider.DeltaContent, Content: text},
		{Kind: provider.DeltaToolCall, ToolCall: &provider.ToolCall{
			ID:       id,
			Type:     "function",
			Function: provider.FunctionCall{Name: name, Arguments: args},
		}},
		{Kind: provider.DeltaDone, FinishReason: "tool_calls"},
	}
}

// retryHookConfig wires one experimental post-response hook into a fresh registry.
func retryHookConfig(t *testing.T, sink domain.EventSink, hook any) domain.Config {
	t.Helper()
	cfg := baseConfig(sink)
	cfg.Mechanisms = domain.NewMechanismRegistry()
	if err := cfg.Mechanisms.AddExperimental(domain.HookPostResponse, hook); err != nil {
		t.Fatalf("AddExperimental: %v", err)
	}
	return cfg
}

// driveExchange constructs the Agent, submits text, and drives the Exchange to completion.
func driveExchange(t *testing.T, cfg domain.Config, responder provider.Responder, text string) *Agent {
	t.Helper()
	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: text}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	return a
}

// wireMessageIndex returns the index of the first wire message with role and content, or -1.
func wireMessageIndex(msgs []provider.Message, role, content string) int {
	for i, m := range msgs {
		if m.Role == role && m.Content == content {
			return i
		}
	}
	return -1
}

// wireRoleCount counts the wire messages carrying role.
func wireRoleCount(msgs []provider.Message, role string) int {
	n := 0
	for _, m := range msgs {
		if m.Role == role {
			n++
		}
	}
	return n
}

// TestRetryExchange_CarriesSupersededAssistantAndCorrection: a draft with narration + a
// tool call is retried with a correction → the second provider request carries the
// superseded assistant message (content + tool calls) followed immediately by the
// user-role correction, and the committed history carries neither.
func TestRetryExchange_CarriesSupersededAssistantAndCorrection(t *testing.T) {
	sink := &recordingSink{}
	calls := 0
	cfg := retryHookConfig(t, sink, scriptedRetryHook{injects: []string{"use the tool correctly"}, calls: &calls})
	responder := &captureAllResponder{scripts: [][]provider.Delta{
		draftWithToolCall("narration first", "c1", "lookup", `{"q":"x"}`),
		contentScript("fixed"),
	}}

	a := driveExchange(t, cfg, responder, "go")

	if len(responder.got) != 2 {
		t.Fatalf("provider was called %d times, want 2", len(responder.got))
	}
	second := responder.got[1].Messages
	ai := wireMessageIndex(second, "assistant", "narration first")
	if ai < 0 {
		t.Fatalf("retried request carries no superseded assistant message: %+v", second)
	}
	tc := second[ai].ToolCalls
	if len(tc) != 1 || tc[0].ID != "c1" || tc[0].Function.Name != "lookup" || tc[0].Function.Arguments != `{"q":"x"}` {
		t.Errorf("superseded assistant tool calls = %+v, want the draft's lookup call", tc)
	}
	if ci := wireMessageIndex(second, "user", "use the tool correctly"); ci != ai+1 {
		t.Errorf("correction at index %d, want %d (immediately after the superseded assistant)", ci, ai+1)
	}

	// Request-scoped, never committed: the history holds only the user message and the
	// retried final response.
	if got := a.conv.Len(); got != 2 {
		t.Errorf("committed history has %d messages, want 2 (user + final assistant)", got)
	}
	if me, ok := lastMessageEvent(sink.events); !ok || me.Text != "fixed" {
		t.Errorf("final MessageEvent = %+v (ok=%v), want the retried %q", me, ok, "fixed")
	}
}

// TestRetryExchange_EmptySupersededAppendsOnlyCorrection: a wholly empty draft (no text,
// no calls) retried with a correction appends only the user-role correction — no
// superseded assistant message rides the retried request.
func TestRetryExchange_EmptySupersededAppendsOnlyCorrection(t *testing.T) {
	sink := &recordingSink{}
	calls := 0
	cfg := retryHookConfig(t, sink, scriptedRetryHook{injects: []string{"say something"}, calls: &calls})
	responder := &captureAllResponder{scripts: [][]provider.Delta{
		{{Kind: provider.DeltaDone, FinishReason: "stop"}}, // the wholly empty draft
		contentScript("recovered"),
	}}

	driveExchange(t, cfg, responder, "go")

	if len(responder.got) != 2 {
		t.Fatalf("provider was called %d times, want 2", len(responder.got))
	}
	second := responder.got[1].Messages
	if n := wireRoleCount(second, "assistant"); n != 0 {
		t.Errorf("retried request carries %d assistant messages, want 0 (empty superseded response)", n)
	}
	if wireMessageIndex(second, "user", "say something") < 0 {
		t.Errorf("retried request carries no correction: %+v", second)
	}
}

// TestRetryExchange_EmptyInjectIsBareRestream: an ActionRetry with no Inject re-streams
// the request untouched — byte-identical to the superseded attempt's request.
func TestRetryExchange_EmptyInjectIsBareRestream(t *testing.T) {
	sink := &recordingSink{}
	calls := 0
	cfg := retryHookConfig(t, sink, scriptedRetryHook{injects: []string{""}, calls: &calls})
	responder := &captureAllResponder{scripts: [][]provider.Delta{
		contentScript("draft"),
		contentScript("final"),
	}}

	driveExchange(t, cfg, responder, "go")

	if len(responder.got) != 2 {
		t.Fatalf("provider was called %d times, want 2", len(responder.got))
	}
	if !reflect.DeepEqual(responder.got[0], responder.got[1]) {
		t.Errorf("an Inject-less retry altered the request:\nfirst:  %+v\nsecond: %+v",
			responder.got[0], responder.got[1])
	}
}

// TestRetryExchange_CorrectionsAccumulateAcrossRetries: two correction retries in one Turn
// → the third request carries both superseded exchanges in order (the sim's escalating
// re-asks).
func TestRetryExchange_CorrectionsAccumulateAcrossRetries(t *testing.T) {
	sink := &recordingSink{}
	calls := 0
	cfg := retryHookConfig(t, sink, scriptedRetryHook{injects: []string{"fix one", "fix two"}, calls: &calls})
	responder := &captureAllResponder{scripts: [][]provider.Delta{
		contentScript("draft one"),
		contentScript("draft two"),
		contentScript("final"),
	}}

	driveExchange(t, cfg, responder, "go")

	if len(responder.got) != 3 {
		t.Fatalf("provider was called %d times, want 3", len(responder.got))
	}
	third := responder.got[2].Messages
	d1 := wireMessageIndex(third, "assistant", "draft one")
	f1 := wireMessageIndex(third, "user", "fix one")
	d2 := wireMessageIndex(third, "assistant", "draft two")
	f2 := wireMessageIndex(third, "user", "fix two")
	if d1 < 0 || f1 < 0 || d2 < 0 || f2 < 0 {
		t.Fatalf("third request is missing part of the accumulated exchange (indices %d/%d/%d/%d): %+v",
			d1, f1, d2, f2, third)
	}
	if !(d1 < f1 && f1 < d2 && d2 < f2) {
		t.Errorf("accumulated exchange out of order: draft one=%d fix one=%d draft two=%d fix two=%d",
			d1, f1, d2, f2)
	}
}

// TestRetryExchange_CapPassesLastResponseThrough: an always-retrying hook stops at
// maxPostResponseRetries — no further append, no further Upstream call — and the last
// response passes through to the committed history.
func TestRetryExchange_CapPassesLastResponseThrough(t *testing.T) {
	sink := &recordingSink{}
	cfg := retryHookConfig(t, sink, alwaysRetryHook{inject: "try again"})
	responder := &captureAllResponder{scripts: [][]provider.Delta{
		contentScript("r1"),
		contentScript("r2"),
		contentScript("r3"),
		contentScript("r4"),
	}}

	a := driveExchange(t, cfg, responder, "go")

	if len(responder.got) != maxPostResponseRetries+1 {
		t.Fatalf("provider was called %d times, want %d (the retry cap)",
			len(responder.got), maxPostResponseRetries+1)
	}
	last := responder.got[maxPostResponseRetries].Messages
	if n := wireRoleCount(last, "assistant"); n != maxPostResponseRetries {
		t.Errorf("last request carries %d superseded assistant messages, want %d (no append past the cap)",
			n, maxPostResponseRetries)
	}
	if me, ok := lastMessageEvent(sink.events); !ok || me.Text != "r4" {
		t.Errorf("final MessageEvent = %+v (ok=%v), want the passed-through %q", me, ok, "r4")
	}
	if got := a.conv.Len(); got != 2 {
		t.Errorf("committed history has %d messages, want 2 (user + final assistant)", got)
	}
}
