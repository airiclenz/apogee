package agent

import (
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/floor"
)

// The Floor guards' configuration keys, one per guard. The key is the guard's identity everywhere
// outside internal/floor: it is what a user writes in config.yaml, what SetFloor switches, and what
// a FloorGuardEvent names — so an observer never has to map an internal name back to the switch
// that turns the behaviour off.
const (
	guardToolCallRepair        = "tool-call-repair"
	guardToolLoopBreaker       = "tool-loop-breaker"
	guardEmptyResponseRecovery = "empty-response-recovery"
	guardToolUseEnforcer       = "tool-use-enforcer"
	guardReadCache             = "read-cache"
	guardToolResultCap         = "tool-result-cap"
)

// The actions a Floor guard books on its event: a guard that re-streams the Turn with a correction
// took guardActionRetry, one that reshaped a pending tool call before it ran took
// guardActionIntercept, and one that shrank content in the outgoing request took guardActionCap.
const (
	guardActionRetry     = "retry"
	guardActionIntercept = "intercept"
	guardActionCap       = "cap"
)

// SetFloor replaces the live Floor-guard gates for the rest of the session, mirroring
// SetPruneToolResults. Each guard is consulted once per firing seam, so a guard switched off stops
// at the next seam and one switched back on arms again with no rebuild; nothing already corrected
// is undone, the guards being decisions about a response that has already been reviewed.
//
// It takes the WHOLE FloorConfig rather than one flag at a time because the six guards are read as
// one value at each seam, and a caller that owns the settings surface owns all six. It is safe to
// call from another goroutine while a Step runs, like SetMode. A sub-agent spawned AFTER the switch
// inherits the new value at spawn.
func (a *Agent) SetFloor(gates domain.FloorConfig) {
	a.floorMu.Lock()
	a.floor = gates
	a.floorMu.Unlock()
}

// floorConfig reports the live Floor-guard gates under the lock, so a seam's decision is race-free
// against a concurrent SetFloor. cfg.Floor is only the construction seed.
func (a *Agent) floorConfig() domain.FloorConfig {
	a.floorMu.RLock()
	defer a.floorMu.RUnlock()
	return a.floor
}

// runPostResponseGuards runs the post-response Floor guards against resp and reports whether the
// Turn should re-stream and with what correction. It runs BEFORE the lab hooks at this seam
// (runPostResponseHooks): the guards are engine behaviour every model runs with, so a malformed or
// looping response is repaired before a catalogued Mechanism ever looks at it.
//
// The order is ratified (ADR 0071): tool-loop breaker, tool-call repair, empty-response recovery,
// tool-use enforcer — the coarser "you are going in circles" judgment before the finer "this call is
// malformed" one, then the two recoveries for a Turn that produced no usable call at all. The four
// triggers are disjoint in practice (a response either carries calls or does not), so the order is
// about a stable answer rather than a contested one. THE FIRST GUARD TO FIRE WINS: its correction is
// the one the Turn re-streams with, and the remaining guards do not run, exactly as an ActionRetry
// short-circuits the hook cascade.
//
// No guard carries strikes-3 suppression or a Turn-Budget throttle (ADR 0071 decision 1): a Floor
// guard cannot regress Bypass, so it is never withdrawn. The per-Turn maxPostResponseRetries bound —
// shared with the hook retries, counted once by the caller — is the only limiter.
func (a *Agent) runPostResponseGuards(turn int, resp *domain.Response) (retry bool, inject string) {
	gates := a.floorConfig()

	if !gates.DisableToolLoopBreaker {
		if directive, fired := floor.ToolLoopBreak(resp); fired {
			a.emitFloorGuard(turn, guardToolLoopBreaker, guardActionRetry)
			return true, directive
		}
	}
	if !gates.DisableToolCallRepair {
		if correction, fired := floor.ToolCallRepair(resp); fired {
			a.emitFloorGuard(turn, guardToolCallRepair, guardActionRetry)
			return true, correction
		}
	}
	if !gates.DisableEmptyResponseRecovery {
		if nudge, fired := floor.RecoverEmpty(resp); fired {
			a.emitFloorGuard(turn, guardEmptyResponseRecovery, guardActionRetry)
			return true, nudge
		}
	}
	if !gates.DisableToolUseEnforcer {
		if correction, fired := floor.EnforceToolUse(resp); fired {
			a.emitFloorGuard(turn, guardToolUseEnforcer, guardActionRetry)
			return true, correction
		}
	}
	return false, ""
}

// runPreToolExecGuards runs the pre-tool-exec Floor guards against the call the loop is about to
// dispatch, reshaping it in place. Like the post-response guards it runs BEFORE the lab hooks at
// this seam (runPreToolExecHooks), so a catalogued Mechanism sees the call the floor left behind.
//
// There is one guard here today — the read cache — so there is no order to ratify; a second would
// join the same chain, each consulted independently because a shaping guard has nothing to
// short-circuit. A guard that changed nothing is silent, exactly as a hook that did not intervene
// books no fire.
//
// The call is passed through a domain.ToolCallEdit, the same wrapper the hooks mutate through, so a
// guard's write reaches the pending call the loop owns and the loop commits what the guard left.
func (a *Agent) runPreToolExecGuards(turn int, call *domain.ToolCall) {
	if a.floorConfig().DisableReadCache {
		return
	}
	if floor.CacheRead(a.loopView(turn), domain.NewToolCallEdit(call)) {
		a.emitFloorGuard(turn, guardReadCache, guardActionIntercept)
	}
}

// runPreRequestGuards runs the pre-request Floor guards against the request the loop is about to
// send, reshaping it in place. Like the other two seams it runs BEFORE the lab hooks at this seam
// (runPreRequestHooks), so a catalogued Mechanism shapes the request the floor left behind.
//
// There is one guard here today — the tool-result cap — so there is no order to ratify. It edits
// only the PROJECTED REQUEST: the conversation keeps every result whole, so a later Turn, a session
// snapshot and the rendered transcript are unaffected by what the model was spared reading again.
//
// The guard runs on EVERY request the Turn sends, the re-derived one a fold produced included: the
// fold rewrote the history, so the results the cap would have trimmed may not even be there any
// more, and re-asking is cheaper than reasoning about which ones survived.
func (a *Agent) runPreRequestGuards(turn int, req *domain.Request) {
	if a.floorConfig().DisableToolResultCap {
		return
	}
	if capped := floor.CapToolResults(req); capped > 0 {
		a.emitFloorGuard(turn, guardToolResultCap, guardActionCap)
	}
}

// emitFloorGuard books one guard firing as a FloorGuardEvent, the guards' counterpart to the
// MechanismFiredEvent a hook books. It is emitted only when the guard ACTED — a guard that found
// nothing is silent, the same rule the hook seam applies.
func (a *Agent) emitFloorGuard(turn int, guard, action string) {
	a.cfg.Events.Emit(domain.FloorGuardEvent{
		EventBase: a.base(turn),
		Guard:     guard,
		Action:    action,
	})
}
