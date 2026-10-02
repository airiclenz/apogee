package agent

import "github.com/airiclenz/apogee/internal/domain"

// Delegation width (ADR 0039, ADR 0045 §5, ADR 0069). Every number that says how many sub_agent
// delegations run at once is answered in this file and nowhere else; the workflow, background and
// view paths ask it rather than re-deriving a width of their own:
//
//   - the width STATED to the model: statedDelegationWidth, latched per seat, which the orientation
//     block's Delegation bounds read;
//   - the width a batch RUNS at: fanOutWidthFor for one reply, seat-aware (fanOutWidth is its
//     single-seat rule); delegationWidth for the default seat before any reply exists, which
//     buildRequest stamps onto LoopView.ParallelAgents; seatCap for a run pinned to one seat, which
//     workflowWidthOn and backgroundWidth floor their own way;
//   - the ceiling: fanOutCeiling, rounds of the stated width, computed by fanOutCeilingOf.
//
// Under them sit two caps: parallelAgentsCap, the session server's live cap, and delegationCap, the
// one choice of whose cap governs when nothing names a seat.
//
// The far-width stickiness (ADR 0069 decision 6, as amended 2026-09-20) is named here once. The
// stated width's doors are the human's and a cap's first statement: SetParallelAgents (a
// `/server` switch, a heartbeat that discovers the session server's slots), a non-nil
// SetDelegationTarget (the far server's cap, stated once and then re-stated identically each
// beat) and SetDelegationSeat (`/sub-agents-server`, which forgets the far width so the next
// beat states the new server's). A target-down beat — SetDelegationTarget(nil) — is deliberately
// not a door: the far width stands across it, so a flapping far server cannot flap the number.
// stateFarWidth and forgetFarWidth below are the far width's only writers.

// parallelAgentsCap reports the live fan-out width under the lock, so the dispatch read is race-free
// against a concurrent SetParallelAgents. It is the ONE read seam for the SESSION server's cap:
// cfg.ParallelAgents is only the construction seed. Which cap a dispatch then GOVERNS by — this one
// or a latched Delegation target's — is delegationCap's single choice (ADR 0045 §5).
func (a *Agent) parallelAgentsCap() int {
	a.parallelAgentsMu.RLock()
	defer a.parallelAgentsMu.RUnlock()
	return a.parallelAgents
}

// seatCap reports the Parallel agents cap of the server a child on seat runs on: the session
// server's for seatSession whatever is latched (ADR 0069 decision 7's "a single-seat reply keeps
// its seat's cap" — a session-seated child never reads the latch), and the governing cap
// (delegationCap) for every other seat, which with nothing latched is the session's too. It is the
// raw cap, before any floor.
func (a *Agent) seatCap(seat delegationSeat) int {
	if seat == seatSession {
		return a.parallelAgentsCap()
	}
	return a.delegationCap()
}

// delegationWidth reports how many sub_agent delegations THIS agent may run at once,
// independent of any particular reply: the resolved Parallel agents cap at depth 0, and 1
// everywhere else.
//
// Depth 0 is the whole eligibility rule (decision 3): a child's own delegations stay serial
// inline, so there is no slot accounting across levels and no way for a nested fan-out to hold
// slots its own children need. It is one rule with two readers — the pool below sizes itself by
// it through fanOutWidthFor, and buildRequest stamps it onto the reaction-facing view
// (LoopView.ParallelAgents) so a Reaction synthesizing delegations batches by the same width the
// engine will honour. That second reader is why such a batch needs nothing of its
// own to follow a routed cap (ADR 0045 §5): its min(cap, remaining) reads the view, the view
// carries this number, and this number already knows which server the children will run on.
//
// It is the DEFAULT seat's width, and stays so with seat choice on the menu (ADR 0069): the view
// is a BATCH HINT, read before any reply exists, and a synthesised delegation names no `run_on` —
// so the seat it will take is precisely the one this number is resolved for. Only a reply the model
// SPLIT across both seats is sized differently, and that is a fact about one reply rather than
// about this agent, which is why it lives in fanOutWidthFor and not here.
func (a *Agent) delegationWidth() int {
	if a.isDelegate() {
		return 1
	}
	if width := a.delegationCap(); width > 1 {
		return width
	}
	return 1
}

// delegationCap reports WHOSE Parallel agents cap governs this agent's delegations. The slots a
// fan-out spends belong to the server the children actually run on, so a latched Delegation
// target answers with ITS cap — the Sub-agent server's pin, else the slot count its own heartbeat
// observed (ADR 0045 §5) — and the bound session server's live cap answers otherwise. The
// otherwise is also the fallback: a target that goes unusable puts the children back on this
// session's Upstream, and the width follows them home on the next dispatch.
//
// A routed cap REPLACES rather than bounds: a grunt box with more slots widens the fan-out past
// the orchestrator's, and a single-slot one narrows it to serial however many the session server
// advertises. Both directions are the same rule — the width follows the work, not the asker.
func (a *Agent) delegationCap() int {
	if target := a.delegationTarget(); target != nil {
		return target.ParallelAgents
	}
	return a.parallelAgentsCap()
}

// fanOutWidth reports how many of a reply's delegations may run at once: min(cap, group size)
// at depth 0 when the Parallel agents cap (ADR 0039 decision 2) allows more than one, and 1 —
// meaning "run the group serially, exactly as this loop always has" — otherwise. A group of
// one is never worth a pool; everything else is delegationWidth's rule.
//
// It is reached ONCE per reply — through fanOutWidthFor, the seat-aware form dispatchTools calls —
// and the number is handed DOWN as an argument, which is what makes a group's width immutable for
// the life of that group: the Delegation target it may have been resolved from is re-stated on
// every heartbeat of the Sub-agent server (ADR 0045), so a beat landing between the first child and
// the last must not be able to re-size a pool that is already running. One reply, one width,
// however many beats cross it.
func (a *Agent) fanOutWidth(delegations int) int {
	if delegations < 2 {
		return 1
	}
	return min(a.delegationWidth(), delegations)
}

// fanOutWidthFor is fanOutWidth read against the SEATS this particular reply named (ADR 0069):
// with `run_on` on the menu a single reply may put some children on the session server and the rest
// on the Sub-agent server, and the two seats have caps of their own. A reply split across both is
// sized by min(session cap, target cap) — the smaller of the two, because ONE pool runs the whole
// group and a pool wider than either server's cap would overrun that server whichever children
// happened to be in flight. A reply that lands entirely on one seat is not split at all and keeps
// THAT seat's own width: for the Sub-agent server that is fanOutWidth's rule verbatim, and for the
// session seat it is the session server's cap even while a target is latched — ADR 0069 decision
// 7's "a single-seat reply keeps its seat's cap", which fanOutWidth cannot honour on its own
// because delegationCap answers with the latched target's cap whenever a target exists.
//
// The smaller cap is deliberately the whole rule rather than a per-seat accounting: two pools, or
// slots counted per seat, would buy width in the mixed case at the price of a second scheduler in
// the dispatch path — and the mixed case is a reply the model chose to split, not the shape most
// replies take. One reply, one width, however the seats fall (the once-per-reply snapshot rule the
// doc above states, now read for two servers instead of one).
//
// Like fanOutWidth it is called ONCE per reply, and it takes ONE latch snapshot for the whole
// classification: a beat landing between two calls of the same reply must not be able to make the
// group look split when it was not.
func (a *Agent) fanOutWidthFor(calls []domain.ToolCall) int {
	if len(calls) < 2 || a.isDelegate() {
		return 1
	}
	target := a.delegationTarget()
	if !a.seatsAreSplit(calls, target) {
		// ADR 0069 decision 7, the one case fanOutWidth gets wrong: with a target latched
		// delegationCap answers with the TARGET's cap, but a reply whose every call asked for
		// the session seat runs entirely on the session server and must be sized by ITS cap.
		// Gated beside the target on publishesSeatChoice exactly as seatsAreSplit is — under
		// `sub-agents-choice: fixed` run_on is ignored (subagent.go), so an all-session reply
		// still runs on the target and keeps the target's cap.
		if target != nil && publishesSeatChoice(a.tools) && a.allAskedSession(calls) {
			if width := a.parallelAgentsCap(); width > 1 {
				return min(width, len(calls))
			}
			return 1
		}
		return a.fanOutWidth(len(calls))
	}
	// A split reply always has a usable target — a child only lands on the far seat because one is
	// latched — so the cap below is read off a non-nil target by construction.
	width := min(a.parallelAgentsCap(), target.ParallelAgents)
	if width < 2 {
		return 1
	}
	return min(width, len(calls))
}

// statedDelegationWidth is the ONE width seam the engine STATES to the model — the fan-out
// ceiling's multiplier (fanOutCeiling) and the orientation block's delegation
// bounds read it and nothing else — as distinct from the width a pool actually RUNS at, which
// fanOutWidthFor sizes per reply from the live latch. The two are kept apart on purpose: the
// enforced number and the announced number must agree with each other, and a number the model
// is told must not move on every heartbeat (ADR 0023 §6), so this one is LATCHED per seat rather
// than read live. It answers the far width when a usable Delegation target has stated one since
// the seat last moved (farWidth), else the session server's width, and 1 on a delegate — a
// child's own delegations run serially inline (ADR 0039 decision 3). Never below 1: it is the
// width of a group that runs, and a group runs at least one at a time.
//
// It moves only on the doors this file's comment names (ADR 0069 decision 6).
func (a *Agent) statedDelegationWidth() int {
	if a.isDelegate() {
		return 1
	}
	a.parallelAgentsMu.RLock()
	defer a.parallelAgentsMu.RUnlock()
	if a.farWidth > 0 {
		return a.farWidth
	}
	return max(a.parallelAgents, 1)
}

// stateFarWidth latches the Sub-agent server's width for statedDelegationWidth, floored at 1 so a
// serial far server is told apart from no statement at all (farWidth's zero).
func (a *Agent) stateFarWidth(width int) {
	a.parallelAgentsMu.Lock()
	a.farWidth = max(width, 1)
	a.parallelAgentsMu.Unlock()
}

// forgetFarWidth clears the latched far width, so the session width answers until the next usable
// target states one — what the human's `/sub-agents-server` door does (SetDelegationSeat).
func (a *Agent) forgetFarWidth() {
	a.parallelAgentsMu.Lock()
	a.farWidth = 0
	a.parallelAgentsMu.Unlock()
}

// fanOutCeiling reports how many sub_agent delegations ONE reply may fan out before the rest are
// refused (refusePastCeiling): `delegate-fanout-rounds` rounds of the width the engine STATES for
// this agent (statedDelegationWidth — the far server's latched cap, else the session server's, 1
// on a delegate), or 0 for no ceiling when rounds is 0. It is the enforced number and the
// announced one at once — the orientation block's delegation bounds read the same function — so
// what the model is told it may fan out is exactly what dispatch lets through.
//
// There is deliberately NO floor and the rule applies at every depth (owner, 2026-09-20): an
// unkeyed, unpinned local server has width 1, so at the default two rounds a reply of three
// delegations there refuses the third, and a delegate's ceiling is `rounds` outright. The ceiling
// is a bound on how much one reply may commit the coordinator to before it reads a single result —
// the 56-dispatch reply this closes had 35 that never started — and a floor would re-open that on
// the narrow servers where it costs the most.
func (a *Agent) fanOutCeiling() int {
	return fanOutCeilingOf(a.cfg.Delegation.FanOutRounds, a.statedDelegationWidth())
}

// fanOutCeilingOf is the one formula behind fanOutCeiling — rounds × width, 0 when rounds is 0 —
// kept apart so the refusal text (fanOutCeilingResult) states the very number it enforces.
func fanOutCeilingOf(rounds, width int) int {
	if rounds <= 0 {
		return 0
	}
	return rounds * width
}
