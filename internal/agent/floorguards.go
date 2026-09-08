package agent

import (
	"github.com/airiclenz/apogee/internal/domain"
)

// The Floor guards' configuration keys, one per guard. The key is the guard's identity everywhere
// outside internal/floor: it is what a user writes in config.yaml, what SetFloor switches, and the
// id each guard's builtin Reaction fires under (builtins.go) — so an observer reading a
// ReactionFiredEvent never has to map an internal name back to the switch that turns the behaviour
// off.
const (
	guardToolCallRepair        = "tool-call-repair"
	guardToolCallSalvage       = "tool-call-salvage"
	guardToolLoopBreaker       = "tool-loop-breaker"
	guardEmptyResponseRecovery = "empty-response-recovery"
	guardToolUseEnforcer       = "tool-use-enforcer"
	guardReadCache             = "read-cache"
	guardToolResultCap         = "tool-result-cap"
)

// The actions a Floor guard books its firings under: a guard that re-streams the Turn with a
// correction took guardActionRetry, one that reshaped a pending tool call before it ran took
// guardActionIntercept, one that shrank content in the outgoing request took guardActionCap, and
// one that read a call the model wrote into its own text back onto the response took
// guardActionSalvage. They are the shared action vocabulary of the ReactionFiredEvent, which an
// armed Reaction's firing is labelled from too (reactions.go).
const (
	guardActionRetry     = "retry"
	guardActionIntercept = "intercept"
	guardActionCap       = "cap"
	guardActionSalvage   = "salvage"
)

// SetFloor replaces the live Floor-guard gates for the rest of the session, mirroring
// SetPruneToolResults. Each guard's builtin reads this value at fire time (builtins.go), so a guard
// switched off stops at the next Moment and one switched back on arms again with no rebuild;
// nothing already corrected is undone, the guards being decisions about a response that has already
// been reviewed.
//
// It takes the WHOLE FloorConfig rather than one flag at a time because the seven guards are read
// as one value at each seam, and a caller that owns the settings surface owns all seven. It is safe to
// call from another goroutine while a Step runs, like SetMode. A sub-agent spawned AFTER the switch
// inherits the new value at spawn.
func (a *Agent) SetFloor(gates domain.FloorConfig) {
	a.floorMu.Lock()
	a.floor = gates
	a.floorMu.Unlock()
}

// floorConfig reports the live Floor-guard gates under the lock, so a builtin's decision is
// race-free against a concurrent SetFloor. cfg.Floor is only the construction seed.
func (a *Agent) floorConfig() domain.FloorConfig {
	a.floorMu.RLock()
	defer a.floorMu.RUnlock()
	return a.floor
}
