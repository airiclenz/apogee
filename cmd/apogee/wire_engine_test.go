package main

import (
	"errors"
	"testing"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/domain"
)

func TestFriendlyConstructErr(t *testing.T) {
	t.Parallel()

	if got := friendlyConstructErr(apogee.ErrAutoUnavailable); !errors.Is(got, errAutoUnavailable) {
		t.Errorf("friendlyConstructErr(ErrAutoUnavailable) = %v; want errAutoUnavailable", got)
	}

	other := errors.New("some other failure")
	if got := friendlyConstructErr(other); !errors.Is(got, other) {
		t.Errorf("friendlyConstructErr(other) = %v; want passthrough", got)
	}
}

// TestLateEngineInterjectChildRefusesUnbound pins the pre-bound half of the new seam: a session
// that has not chosen a server has no Agent, and so no child tree to reach into. The holder must
// name the way out rather than panic on a nil Agent — the same refusal every other
// conversation-touching call answers with (ADR 0036).
func TestLateEngineInterjectChildRefusesUnbound(t *testing.T) {
	t.Parallel()

	var engine lateEngine
	err := engine.InterjectChild("call-1", apogee.UserInput{Text: "check the docs too"})
	if !errors.Is(err, errNoServerBound) {
		t.Errorf("InterjectChild err = %v; want errNoServerBound", err)
	}
}

// The far seat's display facts are resolved by the composition root at ITS construction, which on a
// pre-bound session happens before any Agent exists (ADR 0036 decision 3) — and unlike a Delegation
// target, nothing beats on them afterwards to state them again. A bind with no memory of the push
// would therefore render a Delegations line naming only the session seat for the whole session, with
// no door left to correct it. So the seat is REMEMBERED and installed at the bind, and a push after
// the bind goes straight through to the Agent (which is where installing it is pinned —
// internal/agent's own SetDelegationSeat tests).
func TestLateEngineRemembersTheDelegationSeatUntilTheBind(t *testing.T) {
	t.Parallel()

	engine := newLateEngine(domain.ModeAskBefore, true)
	t.Cleanup(func() { _ = engine.Close() })

	// A fresh holder carries no seat: the block then names only the session's own server.
	if engine.pendingSeat != nil {
		t.Fatalf("a fresh holder already carries a seat: %+v", engine.pendingSeat)
	}

	seat := &apogee.DelegationSeat{
		Name: "grunt", Description: "fast local 4B — search and edits", Model: "qwen3-4b",
	}
	engine.SetDelegationSeat(seat)
	if engine.pendingSeat != seat {
		t.Fatalf("pendingSeat = %+v; want the seat held for the bind", engine.pendingSeat)
	}

	if err := engine.Bind(func() (*apogee.Agent, error) { return apogee.New(validCfg(t)) }); err != nil {
		t.Fatalf("Bind: %v", err)
	}

	// And past the bind the door stays open and stays anytime-safe: a `/sub-agents-server` pick moves
	// the facts on the Agent itself, and the opt-out clears them — remembered as cleared, so a second
	// holder of this session can never resurrect a seat the human just took away.
	moved := &apogee.DelegationSeat{Name: "cheaper", Description: "the box in the cupboard"}
	engine.SetDelegationSeat(moved)
	if engine.pendingSeat != moved {
		t.Fatalf("pendingSeat after a bound push = %+v; want the moved seat", engine.pendingSeat)
	}
	engine.SetDelegationSeat(nil)
	if engine.pendingSeat != nil {
		t.Errorf("pendingSeat after the opt-out = %+v; want nil", engine.pendingSeat)
	}
}

// TestLateEngineReplaysThePruneGateAtTheBind pins the newest anytime-safe mutator on the holder's
// remember-then-install contract: a `prune-tool-results` edit made while the settings pane is open
// and no server is chosen must reach the Agent the moment one is built, or the session runs the
// whole way on the seed its Config carried. The holder's memory is asserted here and the bind is
// driven to prove the replay path is live; what SetPruneToolResults DOES to an Agent is pinned in
// internal/agent's own prune tests, the same split the delegation seat above follows.
func TestLateEngineReplaysThePruneGateAtTheBind(t *testing.T) {
	t.Parallel()

	engine := newLateEngine(domain.ModeAskBefore, true)
	t.Cleanup(func() { _ = engine.Close() })

	engine.SetPruneToolResults(false)
	if engine.pendingPrune == nil || *engine.pendingPrune {
		t.Fatalf("pendingPrune = %v; want false held for the bind", engine.pendingPrune)
	}

	if err := engine.Bind(func() (*apogee.Agent, error) { return apogee.New(validCfg(t)) }); err != nil {
		t.Fatalf("Bind: %v", err)
	}

	// And past the bind the door stays open and stays anytime-safe: the edit goes straight to the
	// Agent, and the holder keeps the value so a later bind of this session installs it too.
	engine.SetPruneToolResults(true)
	if engine.pendingPrune == nil || !*engine.pendingPrune {
		t.Errorf("pendingPrune after a bound edit = %v; want true", engine.pendingPrune)
	}
}
