package agent

import (
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/tools"
)

// requestCall is a sub_agent call with id "c1" carrying arguments.
func requestCall(arguments string) domain.ToolCall {
	return domain.ToolCall{ID: "c1", Tool: tools.SubAgentToolName, Arguments: []byte(arguments)}
}

// TestParseDelegationRequest_DecodeErrorTextNamesSubAgentArgs pins the decode target: the refusal
// the model reads quotes the decode error, and that text has always named tools.SubAgentArgs.
func TestParseDelegationRequest_DecodeErrorTextNamesSubAgentArgs(t *testing.T) {
	t.Parallel()

	request := parseDelegationRequest(requestCall(`{"task":5}`), delegationOffer{})

	const want = "json: cannot unmarshal number into Go struct field SubAgentArgs.task of type string"
	if request.decodeErr == nil || request.decodeErr.Error() != want {
		t.Errorf("decodeErr = %v, want %q", request.decodeErr, want)
	}
}

// TestParseDelegationRequest_SeatReadOnlyWhereOffered pins the seat gate: `run_on` is read, and an
// unknown value refused, only where the offer published it; anywhere else it is ignored, and a call
// that does not decode asks no seat.
func TestParseDelegationRequest_SeatReadOnlyWhereOffered(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name        string
		arguments   string
		seatChoice  bool
		wantSeat    delegationSeat
		wantSeatErr bool
	}{
		{name: "offered session", arguments: `{"task":"x","run_on":"session"}`, seatChoice: true, wantSeat: seatSession},
		{name: "offered sub-agents server", arguments: `{"task":"x","run_on":"sub-agents-server"}`, seatChoice: true, wantSeat: seatSubAgentsServer},
		{name: "offered and absent", arguments: `{"task":"x"}`, seatChoice: true, wantSeat: seatConfigured},
		{name: "offered and unknown", arguments: `{"task":"x","run_on":"gpu"}`, seatChoice: true, wantSeat: seatConfigured, wantSeatErr: true},
		{name: "offered but undecodable", arguments: `{"task":5,"run_on":"session"}`, seatChoice: true, wantSeat: seatConfigured},
		{name: "unoffered session", arguments: `{"task":"x","run_on":"session"}`, wantSeat: seatConfigured},
		{name: "unoffered and unknown", arguments: `{"task":"x","run_on":"gpu"}`, wantSeat: seatConfigured},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			request := parseDelegationRequest(requestCall(tc.arguments), delegationOffer{seatChoice: tc.seatChoice})

			if got := request.askedSeat(); got != tc.wantSeat {
				t.Errorf("askedSeat() = %v, want %v", got, tc.wantSeat)
			}
			if got := request.seatErr != nil; got != tc.wantSeatErr {
				t.Errorf("seatErr = %v, want an error: %v", request.seatErr, tc.wantSeatErr)
			}
		})
	}
}

// TestParseDelegationRequest_BackgroundIsLenient pins the background read: the one key decides,
// whatever the rest of the arguments decode to, and only where the offer can keep the background.
func TestParseDelegationRequest_BackgroundIsLenient(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		arguments string
		offered   bool
		want      bool
	}{
		{name: "asked and offered", arguments: `{"task":"x","background":true}`, offered: true, want: true},
		{name: "asked past a bad task", arguments: `{"task":5,"background":true}`, offered: true, want: true},
		{name: "asked but not offered", arguments: `{"task":"x","background":true}`},
		{name: "not a boolean", arguments: `{"task":"x","background":"yes"}`, offered: true},
		{name: "not an object", arguments: `{not json`, offered: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			request := parseDelegationRequest(requestCall(tc.arguments), delegationOffer{background: tc.offered})

			if request.background != tc.want {
				t.Errorf("background = %v, want %v", request.background, tc.want)
			}
			if request.blocking() == tc.want {
				t.Errorf("blocking() = %v, want %v", request.blocking(), !tc.want)
			}
		})
	}
}

// TestParseDelegationRequest_MalformedIsNeverADelegation pins that a Malformed call neither runs in
// the background nor blocks: it is answered with its parse error and spawns nothing.
func TestParseDelegationRequest_MalformedIsNeverADelegation(t *testing.T) {
	t.Parallel()

	call := requestCall(`{"task":"x","background":true}`)
	call.Malformed = &domain.MalformedArguments{}

	request := parseDelegationRequest(call, delegationOffer{background: true})

	if request.blocking() || request.background {
		t.Errorf("blocking() = %v, background = %v, want neither", request.blocking(), request.background)
	}
}

// TestParseDelegationRequest_LabelFallsBackToTaskThenCallID pins the label's order: the name given,
// else the task's first line, else the call id — and a call that does not decode reads no task,
// even one json.Unmarshal filled before the type error.
func TestParseDelegationRequest_LabelFallsBackToTaskThenCallID(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		arguments string
		given     string
		want      string
	}{
		{name: "the given name wins", arguments: `{"task":"do x"}`, given: "scout", want: "scout"},
		{name: "the task's first line", arguments: `{"task":"do x\nthen y"}`, want: "do x"},
		{name: "no task", arguments: `{}`, want: "call c1"},
		{name: "a decode error past the task", arguments: `{"task":"do x","max_steps":"ten"}`, want: "call c1"},
		{name: "not an object", arguments: `{not json`, want: "call c1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := parseDelegationRequest(requestCall(tc.arguments), delegationOffer{}).label(tc.given, "c1")

			if got != tc.want {
				t.Errorf("label() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestParseDelegationRequest_StepCapAsksNothingPastADecodeError pins the step cap: a decoded
// max_steps is clamped against the configured cap, and a call that does not decode asks nothing.
func TestParseDelegationRequest_StepCapAsksNothingPastADecodeError(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name          string
		arguments     string
		wantApplied   int
		wantRequested int
	}{
		{name: "a lower ask binds", arguments: `{"task":"x","max_steps":5}`, wantApplied: 5},
		{name: "a higher ask is clamped", arguments: `{"task":"x","max_steps":50}`, wantApplied: 20, wantRequested: 50},
		{name: "an undecodable call asks nothing", arguments: `{"task":5,"max_steps":5}`, wantApplied: 20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			applied, requested := parseDelegationRequest(requestCall(tc.arguments), delegationOffer{}).stepCap(20)

			if applied != tc.wantApplied || requested != tc.wantRequested {
				t.Errorf("stepCap(20) = (%d, %d), want (%d, %d)", applied, requested, tc.wantApplied, tc.wantRequested)
			}
		})
	}
}

// TestParseDelegationRequest_HasTask pins the empty-task check: a decoded non-empty task is one; an
// empty task and an undecodable call are none.
func TestParseDelegationRequest_HasTask(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		arguments string
		want      bool
	}{
		{arguments: `{"task":"x"}`, want: true},
		{arguments: `{"task":""}`},
		{arguments: `{"task":"x","max_steps":"ten"}`},
	} {
		if got := parseDelegationRequest(requestCall(tc.arguments), delegationOffer{}).hasTask(); got != tc.want {
			t.Errorf("hasTask() for %s = %v, want %v", tc.arguments, got, tc.want)
		}
	}
}
