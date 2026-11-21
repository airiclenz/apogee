package agent

import "github.com/airiclenz/apogee/internal/tools"

// delegationOffer is what this Agent's delegation tools offer the model, as the engine must honour
// it: the arguments the live roster's sub_agent and fan_out published (tools.OfferOf), joined with
// the live facts that decide whether a published `background` can actually be kept. It is the one
// value every seat, context and background reader asks, so none of them probes a tool's concrete
// type and no two of them can read the roster differently.
//
// Asking the published schema rather than tracking a flag on the Agent is what keeps the answers
// from ever disagreeing with the menu: the schema is the only thing the model was actually told,
// and a mid-session SwapTools — or a mid-Turn rehome — moves it. A nil roster, a roster without
// either tool, and a foreign tool registered under either name offer nothing, and the false answer
// is the safe one in every caller: the seat is ignored, the `context` is not read, the call runs
// blocking.
type delegationOffer struct {
	// seatChoice is sub_agent's `run_on`: the ONE question that decides whether a spawn reads the
	// seat a call names, whether the Delegations seat bullet is rendered, and whether a reply's
	// width is sized per seat. A child's sub_agent never publishes it (tools.ChildRoster), and
	// neither does any tool built under `sub-agents-choice: fixed`.
	seatChoice bool
	// context is sub_agent's `context`: the `sub-agents-context` gate as the model was told it.
	// With it false a stray `context` of any shape is ignored, as an unoffered `run_on` is.
	context bool
	// background is whether a sub_agent call may run as a one-item background workflow (ADR
	// 0094): this is the top-level Agent, sub_agent published the switch, and the session can keep
	// the workflow — a scratch directory to keep its folder in and a workspace to run it over.
	// Anywhere else a `background: true` runs blocking (ADR 0094 D7).
	background bool
	// fanOutSeatChoice is fan_out's `run_on`, read under the rule seatChoice states for sub_agent.
	fanOutSeatChoice bool
	// fanOutBackground is whether a fan_out call may start its workflow in the background: this is
	// the top-level Agent — background workflows belong to it — and fan_out published the switch
	// (ADR 0089 D1).
	fanOutBackground bool
}

// delegationOffer reads the offer from the live roster on every call. It is never cached: a.tools
// has several writers, a mid-Turn rehome among them, and a cached answer would outlive the menu the
// model was shown.
func (a *Agent) delegationOffer() delegationOffer {
	published := tools.OfferOf(a.tools)
	topLevel := !a.isDelegate()
	return delegationOffer{
		seatChoice: published.SubAgent.SeatChoice,
		context:    published.SubAgent.Context,
		background: topLevel && published.SubAgent.Background &&
			a.ScratchDir() != "" && a.cfg.WorkspaceDir != "",
		fanOutSeatChoice: published.FanOut.SeatChoice,
		fanOutBackground: topLevel && published.FanOut.Background,
	}
}
