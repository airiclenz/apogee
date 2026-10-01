package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// The child-run lifecycle (runChild, reapChild): the stop verdict as a table over how a run ended
// and what cut it short, then the lifecycle itself over a real Agent standing in for the child —
// the helper reads nothing of it but its Run, its capFold and its mailbox.

// childRunNote is a message queued for a child that never reaches it.
const childRunNote = "look at the tests too"

func TestChildRun_StopVerdict(t *testing.T) {
	t.Parallel()

	cancelled := domain.StepResult{Status: domain.StatusCancelled}
	faulted := domain.StepResult{Status: domain.StatusExchangeComplete, Faulted: true}
	completed := domain.StepResult{Status: domain.StatusExchangeComplete}
	tests := []struct {
		name         string
		res          domain.StepResult
		err          error
		capFold      string
		humanStop    bool
		parentCancel bool
		wantStopped  bool
	}{
		{name: "cancelled by the human's stop", res: cancelled, humanStop: true, wantStopped: true},
		{name: "cancelled by the parent's cancel", res: cancelled, parentCancel: true, wantStopped: true},
		{name: "faulted under a stop, fold cut short", res: faulted, humanStop: true, wantStopped: true},
		{name: "faulted with its fold written", res: faulted, capFold: "fold", humanStop: true},
		{name: "faulted with no stop", res: faulted},
		{name: "cancelled with no stop cause", res: cancelled},
		{name: "a stop that landed as it completed", res: completed, humanStop: true, parentCancel: true},
		{name: "a run error under a stop", res: cancelled, err: errors.New("boom"), humanStop: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			parent, cancelParent := context.WithCancel(context.Background())
			defer cancelParent()
			child, stopChild := context.WithCancelCause(parent)
			defer stopChild(nil)
			if tt.humanStop {
				stopChild(errDelegationStopped)
			}
			if tt.parentCancel {
				cancelParent()
			}
			if got := childRunStopped(parent, child, tt.res, tt.err, tt.capFold); got != tt.wantStopped {
				t.Errorf("childRunStopped = %v, want %v", got, tt.wantStopped)
			}
		})
	}
}

// newChildRunSub builds the Agent a lifecycle test runs as its child, over responder, holding a
// task, and returns it with the sink its events land in.
func newChildRunSub(t *testing.T, responder provider.Responder) (*Agent, *recordingSink) {
	t.Helper()
	sink := &recordingSink{}
	sub, err := newAgent(baseConfig(sink), responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	t.Cleanup(func() { _ = sub.Close() })
	if err := sub.Submit(domain.UserInput{Text: "survey the module"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	return sub, sink
}

// undeliveredCount is how many messages the sink saw reported undelivered.
func undeliveredCount(sink *recordingSink) int {
	count := 0
	for _, e := range sink.events {
		if ev, ok := e.(domain.ChildInterjectionEvent); ok && !ev.Landed {
			count++
		}
	}
	return count
}

func TestChildRun_AHumanStopWithFoldOnFoldsAndReturnsTheLeftover(t *testing.T) {
	t.Parallel()

	var registry childRegistry
	responder := &stopResponder{block: map[int]bool{0: true}}
	sub, sink := newChildRunSub(t, responder)
	responder.before = func(int) {
		if !sub.mailbox.add(domain.UserInput{Text: childRunNote}) {
			t.Error("the running child's mailbox refused a message")
		}
		if !registry.stop("r1") {
			t.Error("stop found no armed run while the child ran")
		}
	}
	armed, folded := false, false
	res, stopped, leftover, err := runChild(context.Background(), childRun{
		registry:    &registry,
		runID:       "r1",
		sub:         sub,
		onArmed:     func(context.Context) { armed = true },
		foldStopped: func(context.Context) { folded = true },
	})

	if err != nil || !stopped || res.Status != domain.StatusCancelled {
		t.Fatalf("runChild = %+v, stopped %v, err %v; want a cancelled run read as stopped", res, stopped, err)
	}
	if !armed || !folded {
		t.Errorf("onArmed ran %v, foldStopped ran %v; want both", armed, folded)
	}
	if len(leftover) != 1 || leftover[0].Text != childRunNote {
		t.Errorf("stop leftover = %+v, want the one queued message", leftover)
	}
	if sub.mailbox.add(domain.UserInput{Text: "late"}) {
		t.Error("the mailbox still accepts after the stop closed it")
	}
	if got := reapChild(&registry, "r1", sub); len(got) != 0 {
		t.Errorf("reap after a fold-on stop = %+v, want nothing (the stop already closed the mailbox)", got)
	}
	if undeliveredCount(sink) != 0 {
		t.Error("the lifecycle reported a message undelivered; reporting is the caller's")
	}
}

func TestChildRun_AParentCancelWithFoldOffLeavesTheLeftoverToTheReap(t *testing.T) {
	t.Parallel()

	var registry childRegistry
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	responder := &stopResponder{block: map[int]bool{0: true}}
	sub, sink := newChildRunSub(t, responder)
	responder.before = func(int) {
		sub.mailbox.add(domain.UserInput{Text: childRunNote})
		cancel()
	}
	_, stopped, leftover, err := runChild(ctx, childRun{registry: &registry, runID: "r1", sub: sub})

	if err != nil || !stopped {
		t.Fatalf("runChild stopped %v, err %v; want a parent cancel read as stopped", stopped, err)
	}
	if leftover != nil {
		t.Errorf("stop leftover with fold off = %+v, want nil", leftover)
	}
	if _, ok := registry.lookup("r1"); !ok {
		t.Error("the child left the registry before its reap")
	}
	got := reapChild(&registry, "r1", sub)
	if len(got) != 1 || got[0].Text != childRunNote {
		t.Errorf("reapChild = %+v, want the one queued message returned", got)
	}
	if _, ok := registry.lookup("r1"); ok {
		t.Error("the child is still addressable after its reap")
	}
	if undeliveredCount(sink) != 0 {
		t.Error("reapChild reported a message undelivered; it only returns them")
	}
}

func TestChildRun_AStopAfterDisarmLeavesTheRunStanding(t *testing.T) {
	t.Parallel()

	var registry childRegistry
	responder := &stopResponder{scripts: [][]provider.Delta{contentScript("all done")}}
	sub, _ := newChildRunSub(t, responder)
	stopReached, folded := true, false
	res, stopped, _, err := runChild(context.Background(), childRun{
		registry:    &registry,
		runID:       "r1",
		sub:         sub,
		settled:     func() { stopReached = registry.stop("r1") },
		foldStopped: func(context.Context) { folded = true },
	})

	if err != nil || stopped || res.Status == domain.StatusCancelled {
		t.Fatalf("runChild = %+v, stopped %v, err %v; want the completed run standing", res, stopped, err)
	}
	if stopReached {
		t.Error("a stop after disarm reached the run")
	}
	if folded {
		t.Error("a completed run was folded")
	}
}
