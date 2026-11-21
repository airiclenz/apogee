package agent

import (
	"encoding/json"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/sanitize"
	"github.com/airiclenz/apogee/internal/title"
	"github.com/airiclenz/apogee/internal/tools"
)

// delegationRequest is one sub_agent call's arguments as the engine reads them: decoded once into
// tools.SubAgentArgs and read against this Agent's delegationOffer, so the seat, the step cap, the
// label and whether the call runs in the background all come from the one decode and no two
// readers can disagree about what the call asked.
//
// It validates only the PURE checks — the decode, the empty task and the seat — and refuses
// nothing itself: each is exposed on its own (decodeErr, hasTask, seatErr) because the blocking
// path (delegate) and the background path (backgroundSubAgentResult) answer them in different
// orders, each interleaved with checks that need the live Agent (a `continue` lookup, the roster,
// the `context` files), and a single refusal method could keep neither order.
//
// A request is only ever parsed for a sub_agent call; whether a call IS one is the caller's
// question (isSubAgentCall).
type delegationRequest struct {
	// args is the decoded arguments, ZEROED when decodeErr is set: json.Unmarshal fills fields past
	// a type error, and a call that does not decode asks for nothing — no label, no step cap, no
	// seat.
	args tools.SubAgentArgs
	// decodeErr is the decode failure, its text naming tools.SubAgentArgs as the model has always
	// been told it; nil when the arguments decode.
	decodeErr error
	// seat is the Delegation seat the call named, read only where the offer published `run_on`
	// (delegationOffer.seatChoice) — seatConfigured everywhere else, and where the value does not
	// parse.
	seat delegationSeat
	// seatErr is the refusal of a `run_on` outside the enum, set only where `run_on` was offered:
	// an unoffered seat is ignored, never refused (ADR 0069 decision 3).
	seatErr error
	// background is whether the call runs as a one-item background workflow (ADR 0094): it asked
	// for it under the lenient read (asksBackground — the one key, whatever the rest decodes to),
	// the offer can keep it, and the call is not Malformed.
	background bool
	// isMalformed marks a call whose arguments were not a JSON object: it is answered with its
	// parse error and spawns nothing, so it is never a delegation.
	isMalformed bool
}

// parseDelegationRequest decodes call's sub_agent arguments once and reads them against offer, the
// asking Agent's delegationOffer. It never fails: a decode error, an unparseable offered seat and a
// Malformed call are recorded on the request for the caller to answer where its order puts them.
func parseDelegationRequest(call domain.ToolCall, offer delegationOffer) delegationRequest {
	request := delegationRequest{isMalformed: call.Malformed != nil}
	if err := json.Unmarshal(call.Arguments, &request.args); err != nil {
		request.args, request.decodeErr = tools.SubAgentArgs{}, err
	}
	if request.decodeErr == nil && offer.seatChoice {
		// parseDelegationSeat answers seatConfigured beside its error, so a refused seat asks none.
		request.seat, request.seatErr = parseDelegationSeat(request.args.RunOn)
	}
	request.background = !request.isMalformed && offer.background && asksBackground(call.Arguments)
	return request
}

// blocking reports whether the call is a delegation of the reply's tool round: one that runs its
// child inline rather than in the background, and is not Malformed. Only such a call gets a run id
// before its head event, is pooled, may be pre-empted, and counts against the fan-out ceiling (ADR
// 0094 D6).
func (r delegationRequest) blocking() bool {
	return !r.isMalformed && !r.background
}

// hasTask reports whether the call names a non-empty task; a call that does not decode names none.
func (r delegationRequest) hasTask() bool {
	return r.args.Task != ""
}

// askedSeat reports the Delegation seat the call named, seatConfigured for every call that named
// none — which includes one whose arguments do not decode, one naming a value outside the enum
// (both refused on their own account before a child exists) and one made against a menu that never
// published `run_on`.
func (r delegationRequest) askedSeat() delegationSeat {
	return r.seat
}

// stepCap reports the step cap the call's child runs under against configured, the delegate cap,
// and the ask it clamps (resolveStepCap). A call that does not decode asked for nothing.
func (r delegationRequest) stepCap(configured int) (applied, requested int) {
	return resolveStepCap(configured, r.args.MaxSteps)
}

// label is the name a ledger row and a background workflow spell the delegation by: name, the
// display name the child ended its run wearing (the call's own `name`, or the namer's), else the
// first line of the task the call asked for — the Driver's own display fallback — else the call id,
// which is the one handle a refused call with no readable task still has and the model itself
// minted.
func (r delegationRequest) label(name, callID string) string {
	if name != "" {
		return name
	}
	if task := sanitize.ClampRunes(sanitize.FirstLine(r.args.Task), title.MaxDelegateRunes); task != "" {
		return task
	}
	return "call " + callID
}
