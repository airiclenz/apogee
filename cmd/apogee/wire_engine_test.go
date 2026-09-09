package main

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/undo"
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

// The Floor gates ride the same remember-then-install contract, with one difference worth pinning:
// they are a FIELD of the generation the holder remembers rather than a value of their own (ADR 0076
// A8), so what a Floor edit leaves behind is where all seven guards stand — and a second swap made
// before the bind replaces the first one whole, which is the shape the bind then installs.
func TestLateEngineReplaysTheFloorGatesAtTheBind(t *testing.T) {
	t.Parallel()

	engine := newLateEngine(domain.ModeAskBefore, true)
	t.Cleanup(func() { _ = engine.Close() })

	if err := engine.SetReactions(apogee.Generation{Floor: apogee.FloorConfig{DisableReadCache: true}}); err != nil {
		t.Fatalf("SetReactions: %v", err)
	}
	want := apogee.FloorConfig{DisableReadCache: true, DisableToolResultCap: true}
	if err := engine.SetReactions(apogee.Generation{Floor: want}); err != nil {
		t.Fatalf("SetReactions: %v", err)
	}

	if err := engine.Bind(func() (*apogee.Agent, error) { return apogee.New(validCfg(t)) }); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if got := engine.bound().Generation().Floor; got != want {
		t.Errorf("the bound Agent's floor = %+v; want the gates held for the bind %+v", got, want)
	}

	// Past the bind the door stays open and stays anytime-safe, exactly as the prune gate's does —
	// and what the holder remembered is read where the session actually reads it, off the bound
	// Agent. A settings row hands the WHOLE generation back with one guard bit moved
	// (liveSettings.setFloorGuard), so that is the shape the edit takes here too.
	moved := apogee.Generation{Floor: want}
	moved.Floor.DisableToolCallRepair = true
	if err := engine.SetReactions(moved); err != nil {
		t.Fatalf("SetReactions: %v", err)
	}
	if got := engine.bound().Generation().Floor; got != moved.Floor {
		t.Errorf("the bound Agent's floor after a bound edit = %+v; want the moved gates %+v", got, moved.Floor)
	}
}

// ---------------------------------------------------------------------------
// The one generation swap (ADR 0076 A8)
// ---------------------------------------------------------------------------

// recordingRunner is the Reaction Runner's swap door as a witness: it writes down every list it was
// handed and what the ENGINE was holding at that instant, which is how the order of the two applies
// is pinned without a Runner — or the goroutines a Runner drains on — behind it.
type recordingRunner struct {
	engine *lateEngine
	lists  [][]domain.Reaction
	seen   []apogee.Generation
	err    error
}

func (r *recordingRunner) Replace(list []domain.Reaction) error {
	if r.err != nil {
		return r.err
	}
	r.lists = append(r.lists, list)
	if agent := r.engine.bound(); agent != nil {
		r.seen = append(r.seen, agent.Generation())
	}
	return nil
}

// observeReaction is one armable user-origin observe row — the shape a `reactions:` entry resolves
// to — named so a test can tell two lists apart by their ids alone.
func observeReaction(id string) domain.Reaction {
	return domain.Reaction{
		ID:      id,
		Origin:  domain.OriginUser,
		Class:   domain.ClassObserve,
		On:      []domain.Moment{domain.MomentTurnFinished},
		Handler: domain.ArgvHandler{Argv: []string{"true"}},
		Timeout: time.Second,
	}
}

// One generation reaches both halves of the Reaction surface, and it reaches them in ORDER: the
// engine first, then the Runner. The order is what keeps the two halves readable as one swap — a
// Runner firing the new list while the engine still runs the old Floor would be exactly the
// half-swapped state the single value exists to abolish.
func TestSetReactionsAppliesEngineThenRunner(t *testing.T) {
	t.Parallel()

	engine := newLateEngine(domain.ModeAskBefore, true)
	t.Cleanup(func() { _ = engine.Close() })
	runner := &recordingRunner{engine: engine}
	engine.seedReactions(runner, apogee.Generation{})
	if err := engine.Bind(func() (*apogee.Agent, error) { return apogee.New(validCfg(t)) }); err != nil {
		t.Fatalf("Bind: %v", err)
	}

	gen := apogee.Generation{
		Floor:   apogee.FloorConfig{DisableReadCache: true},
		Bypass:  true,
		Observe: []domain.Reaction{observeReaction("notify")},
	}
	if err := engine.SetReactions(gen); err != nil {
		t.Fatalf("SetReactions: %v", err)
	}

	if len(runner.lists) != 1 || !reflect.DeepEqual(runner.lists[0], gen.Observe) {
		t.Fatalf("the Runner was handed %+v; want exactly the generation's observe list", runner.lists)
	}
	if len(runner.seen) != 1 || runner.seen[0].Floor != gen.Floor || !runner.seen[0].Bypass {
		t.Errorf("the engine held %+v when the Runner swapped; want the new generation already applied", runner.seen)
	}
}

// A Floor- or Bypass-only generation reaches the engine ALONE. The Runner is not asked to swap a
// list that did not move, because a Replace retires the running generation — draining its workers
// and forgetting the firings it was still correlating — which is a real cost for an edit that never
// touched the observe lane.
func TestSetReactionsSkipsTheRunnerWhenObserveIsUnchanged(t *testing.T) {
	t.Parallel()

	armed := []domain.Reaction{observeReaction("notify")}
	engine := newLateEngine(domain.ModeAskBefore, true)
	t.Cleanup(func() { _ = engine.Close() })
	runner := &recordingRunner{engine: engine}
	engine.seedReactions(runner, apogee.Generation{Observe: armed})
	if err := engine.Bind(func() (*apogee.Agent, error) { return apogee.New(validCfg(t)) }); err != nil {
		t.Fatalf("Bind: %v", err)
	}

	floorOnly := apogee.Generation{Floor: apogee.FloorConfig{DisableToolResultCap: true}, Observe: armed}
	if err := engine.SetReactions(floorOnly); err != nil {
		t.Fatalf("SetReactions(floor only): %v", err)
	}
	bypassOnly := floorOnly
	bypassOnly.Bypass = true
	if err := engine.SetReactions(bypassOnly); err != nil {
		t.Fatalf("SetReactions(bypass only): %v", err)
	}

	if len(runner.lists) != 0 {
		t.Errorf("the Runner was swapped %d times by generations that moved no observe row", len(runner.lists))
	}
	if got := engine.bound().Generation(); got.Floor != bypassOnly.Floor || !got.Bypass {
		t.Errorf("the engine holds %+v; want both generations applied", got)
	}

	// And a list that DID move reaches it, so the skip above is the comparison and not a dead seam.
	moved := bypassOnly
	moved.Observe = []domain.Reaction{observeReaction("notify"), observeReaction("page")}
	if err := engine.SetReactions(moved); err != nil {
		t.Fatalf("SetReactions(moved list): %v", err)
	}
	if len(runner.lists) != 1 {
		t.Errorf("the Runner was swapped %d times by an edited list; want exactly one", len(runner.lists))
	}

	// And the same skip is the RETAINED-BITS property read from the other side: a Floor-only swap
	// off an armed generation must leave Bypass and the observe roster exactly where they stand.
	// Observe is not carried by Agent.Generation(), so the roster is read where it actually lands —
	// the Runner's Replace witness, which must record nothing further — and Bypass off the bound
	// Agent, alongside the one Floor bit that did move.
	t.Run("FloorSwapLeavesBypassAndObserveAlone", func(t *testing.T) {
		swapsBefore := len(runner.lists)
		roster := runner.lists[swapsBefore-1]

		floorOnlyOffTheArmed := moved
		floorOnlyOffTheArmed.Floor.DisableReadCache = true
		if err := engine.SetReactions(floorOnlyOffTheArmed); err != nil {
			t.Fatalf("SetReactions(floor only off the armed generation): %v", err)
		}

		if len(runner.lists) != swapsBefore {
			t.Errorf("a Floor-only swap moved the Runner %d more times; want none", len(runner.lists)-swapsBefore)
		}
		if !reflect.DeepEqual(runner.lists[len(runner.lists)-1], roster) {
			t.Errorf("the roster the Runner is firing = %+v; want the armed roster untouched %+v", runner.lists[len(runner.lists)-1], roster)
		}
		got := engine.bound().Generation()
		if !got.Bypass {
			t.Errorf("Bypass = %v after a Floor-only swap; want the armed value carried", got.Bypass)
		}
		if got.Floor != floorOnlyOffTheArmed.Floor {
			t.Errorf("the bound Agent's floor = %+v; want the moved guard %+v", got.Floor, floorOnlyOffTheArmed.Floor)
		}
	})
}

// The generation rides the remember-then-install contract the mode and the gates ride: a `/settings`
// edit committed before a server is chosen must reach the Agent the moment one is built, or the
// session runs the whole way on the seed its Config carried. The Runner half needs no bind — it
// exists from boot — so the swap the same call makes has already happened.
func TestLateEngineReplaysThePendingGeneration(t *testing.T) {
	t.Parallel()

	engine := newLateEngine(domain.ModeAskBefore, true)
	t.Cleanup(func() { _ = engine.Close() })
	runner := &recordingRunner{engine: engine}
	engine.seedReactions(runner, apogee.Generation{})

	gen := apogee.Generation{
		Floor:   apogee.FloorConfig{DisableToolCallRepair: true},
		Bypass:  true,
		Observe: []domain.Reaction{observeReaction("notify")},
	}
	if err := engine.SetReactions(gen); err != nil {
		t.Fatalf("SetReactions while unbound: %v", err)
	}
	if len(runner.lists) != 1 {
		t.Fatalf("the Runner was swapped %d times before the bind; want one — it runs without an Agent", len(runner.lists))
	}
	if engine.pendingGeneration == nil || engine.pendingGeneration.Floor != gen.Floor {
		t.Fatalf("pendingGeneration = %+v; want %+v held for the bind", engine.pendingGeneration, gen)
	}

	if err := engine.Bind(func() (*apogee.Agent, error) { return apogee.New(validCfg(t)) }); err != nil {
		t.Fatalf("Bind: %v", err)
	}

	got := engine.bound().Generation()
	if got.Floor != gen.Floor || !got.Bypass {
		t.Errorf("the bound Agent's generation = %+v; want the Floor and Bypass held for the bind %+v", got, gen)
	}
	if len(runner.lists) != 1 {
		t.Errorf("the Runner was swapped %d times; want the one swap the edit itself made", len(runner.lists))
	}
}

// A holder with no Runner takes a generation and applies its engine half: a Firing root builds one
// without a Runner, and a swap must skip the half that is not there rather than dereference it — the
// way every call here skips a nil Agent.
func TestSetReactionsWithoutARunner(t *testing.T) {
	t.Parallel()

	engine := newLateEngine(domain.ModeAskBefore, true)
	t.Cleanup(func() { _ = engine.Close() })

	gen := apogee.Generation{Bypass: true, Observe: []domain.Reaction{observeReaction("notify")}}
	if err := engine.SetReactions(gen); err != nil {
		t.Fatalf("SetReactions with no Runner: %v", err)
	}
	if engine.pendingGeneration == nil || !engine.pendingGeneration.Bypass {
		t.Errorf("pendingGeneration = %+v; want the generation held for the bind", engine.pendingGeneration)
	}
}

// A Runner that refuses the list refuses the APPLY: the error is the settings row's sentence, and
// the holder must not record a list the Runner is not firing — a later identical edit has to try
// again rather than skip the swap it never made.
func TestSetReactionsReportsTheRunnersRefusal(t *testing.T) {
	t.Parallel()

	engine := newLateEngine(domain.ModeAskBefore, true)
	t.Cleanup(func() { _ = engine.Close() })
	refused := errors.New("reactions: workspace does not exist")
	runner := &recordingRunner{engine: engine, err: refused}
	engine.seedReactions(runner, apogee.Generation{})

	gen := apogee.Generation{Observe: []domain.Reaction{observeReaction("notify")}}
	if err := engine.SetReactions(gen); !errors.Is(err, refused) {
		t.Fatalf("SetReactions err = %v; want the Runner's refusal", err)
	}

	runner.err = nil
	if err := engine.SetReactions(gen); err != nil {
		t.Fatalf("the second SetReactions: %v", err)
	}
	if len(runner.lists) != 1 {
		t.Errorf("the Runner took %d lists; want the refused edit retried rather than skipped", len(runner.lists))
	}
}

// ---------------------------------------------------------------------------
// The session's undo journal (ADR 0074)
// ---------------------------------------------------------------------------

// The composition root opens the session's undo journal the moment its id is known, which on a
// pre-bound session is long before any Agent exists — and a /clear before a server is picked mints
// another id and opens another journal. So the holder REMEMBERS the last one and installs it at the
// bind: without that, the session would record into construction's in-memory journal and the store
// under the new session's name would stay empty for the whole run. The note travels with it,
// because it is what `/undo` says about what it can reach.
func TestLateEngineRemembersTheUndoJournalUntilTheBind(t *testing.T) {
	t.Parallel()

	engine := newLateEngine(domain.ModeAskBefore, true)
	t.Cleanup(func() { _ = engine.Close() })

	if engine.pendingJournal != nil {
		t.Fatalf("a fresh holder already carries a journal: %+v", engine.pendingJournal)
	}

	// The boot session's journal, and then the one a /clear opened under the new session id.
	engine.SetJournal(undo.New(), "git not found")
	rotated := undo.New()
	engine.SetJournal(rotated, "")

	if err := engine.Bind(func() (*apogee.Agent, error) { return apogee.New(validCfg(t)) }); err != nil {
		t.Fatalf("Bind: %v", err)
	}

	if note := engine.bound().UndoNote(); note != "" {
		t.Errorf("the bound Agent's undo note = %q; want the journal the /clear opened, whose note is empty", note)
	}
	if engine.pendingJournal == nil || engine.pendingJournal.journal != rotated {
		t.Errorf("pendingJournal = %+v; want the journal the /clear opened", engine.pendingJournal)
	}
}

// A nil journal is ignored rather than remembered, exactly as the Agent's own SetJournal refuses
// one: forgetting the journal at a boundary would take `/undo` away for the rest of the session.
func TestLateEngineIgnoresANilUndoJournal(t *testing.T) {
	t.Parallel()

	engine := newLateEngine(domain.ModeAskBefore, true)
	t.Cleanup(func() { _ = engine.Close() })

	held := undo.New()
	engine.SetJournal(held, "workspace mismatch")
	engine.SetJournal(nil, "")

	if engine.pendingJournal == nil || engine.pendingJournal.journal != held {
		t.Errorf("pendingJournal = %+v; want the journal a nil push left alone", engine.pendingJournal)
	}
}

// `/redo` has no business with the upstream: an unbound holder has undone nothing, so the honest
// refusal is the empty stack's rather than "pick a server" — the shape UndoRevert already answers in.
func TestLateEngineRedoRefusesUnboundWithTheEmptyStack(t *testing.T) {
	t.Parallel()

	var engine lateEngine

	if _, ok := engine.RedoPreview(); ok {
		t.Error("RedoPreview reports something to redo on an unbound holder")
	}
	if _, err := engine.RedoRevert(1); !errors.Is(err, undo.ErrNothingToRedo) {
		t.Errorf("RedoRevert err = %v; want undo.ErrNothingToRedo", err)
	}
}
