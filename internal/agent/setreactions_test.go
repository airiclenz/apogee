package agent

// The LIVE half of the sync lane (ADR 0076 A8): SetReactions arms the user's advise and gate
// entries beside Floor and Bypass, in one swap, for the rest of the session. The cells themselves
// are covered by advise_argv_test.go and gate_test.go, which arm through Config.Reactions; what is
// under test here is the SWAP — that a reaction arrives without a restart, that removing one
// disarms it, that a swap about something else leaves the lane alone, that a child spawned after
// the swap runs it, and that a swapped-in gate takes the Approver stage rather than the seam
// cascade.

import (
	"context"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// syncGen is the Generation a Driver hands over when the user's `reactions:` file resolves to
// these sync entries and nothing else has moved: the read-edit-hand-back idiom, with the sync lane
// as the edited field.
func syncGen(a *Agent, sync ...domain.Reaction) domain.Generation {
	gen := a.Generation()
	gen.Sync = sync
	return gen
}

// A swap ARMS an advise entry: the tool result committed before it carries the tool's own output,
// and the next one carries the fenced trailer. Nothing was armed at construction, so the trailer is
// the swap's doing alone.
func TestSetReactionsArmsAnAdviseEntryForTheNextToolResult(t *testing.T) {
	skipWithoutPOSIXShell(t)
	t.Parallel()

	sink := &recordingSink{}
	var reported []string
	a := adviseArgvAgent(t, sink, t.TempDir(), false, &reported)

	call, result := readCall()
	if before := adviseArgvCall(t, a, call, result); before.Content != result.Content {
		t.Fatalf("content before the swap = %q, want the tool's own output %q", before.Content, result.Content)
	}

	a.SetReactions(syncGen(a, userAdvise("coach", []domain.Moment{domain.MomentPostToolResult},
		"/bin/sh", "-c", `printf 'mind the tests\n'`)))

	call2, result2 := readCall()
	msg := adviseArgvCall(t, a, call2, result2)

	if len(msg.Advice) != 1 || msg.Advice[0].Reaction != "coach" {
		t.Fatalf("ledger = %+v, want one span attributed to coach", msg.Advice)
	}
	if want := result2.Content + domain.RenderAdvice(msg.Advice[0], "mind the tests\n"); msg.Content != want {
		t.Errorf("advised content = %q, want %q", msg.Content, want)
	}
	if len(reported) != 0 {
		t.Errorf("reporter said %q, want silence on the success path", reported)
	}
}

// A swap REMOVES a gate: the entry the previous generation denied under runs on the next call. The
// lane is replaced wholesale, so a Generation handed over without an entry disarms it — which is
// what makes deleting a line from the `reactions:` file take effect without a restart.
func TestSetReactionsRemovingAGateStopsTheDenial(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	ran := 0
	a := gateAgent(t, sink, &fakeApprover{decision: domain.ApprovalAllow}, &ran)
	a.SetReactions(syncGen(a, goGate("warden", domain.GateDecision{Verdict: domain.GateDeny, Reason: "not today"})))

	result, _ := a.resolveAndExecute(context.Background(), 0, readCallOnly())
	if !result.IsError || ran != 0 {
		t.Fatalf("armed gate: result %+v after %d runs, want a refusal and no run", result, ran)
	}

	a.SetReactions(syncGen(a))

	result, _ = a.resolveAndExecute(context.Background(), 0, readCallOnly())
	if result.IsError || ran != 1 {
		t.Fatalf("disarmed gate: result %+v after %d runs, want the call executed once", result, ran)
	}
	if booked := gateFirings(sink, "warden"); len(booked) != 1 {
		t.Errorf("booked %d firings, want the one the armed generation made: %+v", len(booked), booked)
	}
}

// A swap about the FLOOR leaves the sync lane exactly as it was. That is the read-edit-hand-back
// idiom's whole point: a settings surface toggling one guard reads the live Generation, moves the
// field it owns and hands the value back, so the user's gate is not silently disarmed by a change
// that had nothing to do with it.
func TestSetReactionsFloorOnlySwapLeavesTheSyncLaneArmed(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	ran := 0
	a := gateAgent(t, sink, &fakeApprover{decision: domain.ApprovalAllow}, &ran)
	a.SetReactions(syncGen(a, goGate("warden", domain.GateDecision{Verdict: domain.GateDeny, Reason: "not today"})))

	gen := a.Generation()
	gen.Floor.DisableToolCallRepair = true
	a.SetReactions(gen)

	if live := a.Generation(); len(live.Sync) != 1 || live.Sync[0].ID != "warden" || !live.Floor.DisableToolCallRepair {
		t.Fatalf("live generation = %+v, want the guard off and the sync lane intact", live)
	}
	result, _ := a.resolveAndExecute(context.Background(), 0, readCallOnly())
	if !result.IsError || ran != 0 {
		t.Fatalf("result %+v after %d runs, want the gate still denying", result, ran)
	}
}

// A child spawned AFTER the swap inherits the sync lane, so a gate the user armed mid-session
// still answers for the calls a delegation makes. Inheritance is spawn-time, like Floor's: the
// child holds the list as its own construction-time set, and a later swap on the parent does not
// reach it.
func TestSetReactionsReachesAChildSpawnedAfterTheSwap(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	ran := 0
	a := gateAgent(t, sink, &fakeApprover{decision: domain.ApprovalAllow}, &ran)
	a.SetReactions(syncGen(a, goGate("warden", domain.GateDecision{Verdict: domain.GateDeny, Reason: "not today"})))

	child, err := a.newChildAgent("call_sub", "the delegated task", "")
	if err != nil {
		t.Fatalf("newChildAgent: %v", err)
	}

	result, _ := child.resolveAndExecute(context.Background(), 0, readCallOnly())
	if !result.IsError || ran != 0 {
		t.Fatalf("child result %+v after %d runs, want the inherited gate to deny", result, ran)
	}
	if want := "tool call denied by reaction warden"; result.Content != want {
		t.Errorf("child result = %q, want %q", result.Content, want)
	}

	// The swap that empties the parent's lane does not disarm a child already running.
	a.SetReactions(syncGen(a))
	result, _ = child.resolveAndExecute(context.Background(), 0, readCallOnly())
	if !result.IsError {
		t.Errorf("child result after the parent's later swap = %+v, want the spawn-time gate still denying", result)
	}
}

// A swapped-in ARGV gate never reaches the seam cascade. Routing one through fireLeg would assert
// a PreToolExecFunc against a command handler, and the wrongHandler error would skip EVERY tool
// call with "pre-tool-exec reaction failed" — so the proof is that the call still runs and the
// gate booked exactly the one firing the Approver stage makes.
func TestSetReactionsArgvGateNeverReachesTheSeamCascade(t *testing.T) {
	skipWithoutPOSIXShell(t)
	t.Parallel()

	sink := &recordingSink{}
	ran := 0
	a := gateAgent(t, sink, &fakeApprover{decision: domain.ApprovalAllow}, &ran)
	a.SetReactions(syncGen(a, userGate("warden", "/bin/sh", "-c", `printf 'allow\n'`)))

	call := readCallOnly()
	a.conv.Append(domain.Message{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{call}})
	if out := a.dispatchSerially(context.Background(), 0, []domain.ToolCall{call}); out != dispatchDone {
		t.Fatalf("dispatch outcome = %v, want dispatchDone", out)
	}

	msg := a.conv.At(a.conv.Len() - 1)
	if strings.Contains(msg.Content, "pre-tool-exec reaction failed") {
		t.Fatalf("tool result = %q — the argv gate was routed through the seam cascade", msg.Content)
	}
	if ran != 1 {
		t.Errorf("the tool ran %d times, want 1 — an allow leaves the ladder's verdict standing", ran)
	}
	if booked := gateFirings(sink, "warden"); len(booked) != 1 || booked[0].Action != "allow" {
		t.Errorf("firings = %+v, want exactly one allow booked by the Approver stage", booked)
	}
}
