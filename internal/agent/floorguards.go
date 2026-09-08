package agent

// The Floor guards' configuration keys, one per guard. The key is the guard's identity everywhere
// outside internal/floor: it is what a user writes in config.yaml, what a Generation's Floor
// switches, and the
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

// guardIDs is every Floor-guard key, in the ladder's firing order. It is the set armReactions
// reserves (reactions.go) — ALL seven, whatever the enable set currently holds — so a
// `reactions:` entry named after a guard the user switched OFF is refused just as loudly as one
// named after a guard that is on: the id is the guard's identity whether or not it is armed
// today, and a swap that turns the guard back on must never find its name already taken.
var guardIDs = []string{
	guardToolCallSalvage,
	guardToolLoopBreaker,
	guardToolCallRepair,
	guardEmptyResponseRecovery,
	guardToolUseEnforcer,
	guardReadCache,
	guardToolResultCap,
}
