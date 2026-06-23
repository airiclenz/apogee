package agent

// P1.5 acceptance: a pre-request hook's mutations provably reach the bytes the
// provider receives — closing the P0.6 gap where hooks fired but their Request
// mutations were dropped. These tests drive a real Step through the unexported
// newAgent seam (the provider stays internal) and assert on the captured
// provider.Request.

import (
	"context"
	"iter"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// capturingResponder records the request it is asked to send, then replies with a
// canned message, so a test can assert what a pre-request hook shaped onto the wire.
type capturingResponder struct {
	got   provider.Request
	reply string
}

func (r *capturingResponder) Stream(_ context.Context, req provider.Request) iter.Seq[provider.Delta] {
	r.got = req
	return streamReply(r.reply)
}

const guidanceMarker = "[apogee:guidance]"

// shapingHook injects a system nudge and a role-safe context message — the two
// operations the P1.5 acceptance names (AppendToSystem / InjectContext).
type shapingHook struct{}

func (shapingHook) PreRequest(_ context.Context, req *domain.Request) error {
	req.AppendToSystem(guidanceMarker, guidanceMarker+" stay focused")
	req.InjectContext("remember the task")
	return nil
}

func driveOneStep(t *testing.T, cfg domain.Config, resp provider.Responder) {
	t.Helper()
	a, err := newAgent(cfg, resp)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "do the thing"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Step(context.Background()); err != nil {
		t.Fatalf("Step: %v", err)
	}
}

func TestPreRequestHookMutationsReachProvider(t *testing.T) {
	sink := &recordingSink{}
	cfg := baseConfig(sink)
	cfg.Mechanisms = domain.NewMechanismRegistry()
	if err := cfg.Mechanisms.AddExperimental(domain.HookPreRequest, shapingHook{}); err != nil {
		t.Fatalf("AddExperimental: %v", err)
	}
	resp := &capturingResponder{reply: "ok"}
	driveOneStep(t, cfg, resp)

	// The hook ran against the real outgoing request: the provider must now see a
	// system message carrying the nudge and an injected user message placed before the
	// user's actual input — neither of which exists in the bare conversation.
	msgs := resp.got.Messages
	if len(msgs) != 3 {
		t.Fatalf("provider saw %d messages, want 3 (system nudge + injected context + input): %+v", len(msgs), msgs)
	}
	if msgs[0].Role != string(domain.RoleSystem) || !strings.Contains(msgs[0].Content, guidanceMarker) {
		t.Errorf("message[0] = %+v, want a system message with the guidance nudge", msgs[0])
	}
	if msgs[1].Role != string(domain.RoleUser) || msgs[1].Content != "remember the task" {
		t.Errorf("message[1] = %+v, want the injected context before the input", msgs[1])
	}
	if msgs[2].Content != "do the thing" {
		t.Errorf("message[2] = %+v, want the original user input last", msgs[2])
	}
}

// TestNoHookLeavesRequestBare is the control: with no pre-request hook, the provider
// sees only the bare conversation — so the mutations above are attributable to the hook.
func TestNoHookLeavesRequestBare(t *testing.T) {
	resp := &capturingResponder{reply: "ok"}
	driveOneStep(t, baseConfig(&recordingSink{}), resp)

	if got := resp.got.Messages; len(got) != 1 || got[0].Content != "do the thing" {
		t.Errorf("bare request = %+v, want the single user message unchanged", got)
	}
}
