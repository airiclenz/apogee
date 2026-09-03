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
	guardToolCallRepair  = "tool-call-repair"
	guardToolLoopBreaker = "tool-loop-breaker"
)

// The actions a Floor guard books on its event: a guard that re-streams the Turn with a correction
// took guardActionRetry.
const guardActionRetry = "retry"

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
// The order is ratified (ADR 0071): tool-loop breaker, then tool-call repair — the coarser "you are
// going in circles" judgment before the finer "this call is malformed" one, matching the cascade the
// two Mechanisms resolved to before they were promoted. THE FIRST GUARD TO FIRE WINS: its correction
// is the one the Turn re-streams with, and the remaining guards do not run, exactly as an
// ActionRetry short-circuits the hook cascade.
//
// Neither guard carries strikes-3 suppression or a Turn-Budget throttle (ADR 0071 decision 1): a
// Floor guard cannot regress Bypass, so it is never withdrawn. The per-Turn maxPostResponseRetries
// bound — shared with the hook retries, counted once by the caller — is the only limiter.
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
	return false, ""
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
