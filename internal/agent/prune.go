package agent

import (
	apogeectx "github.com/airiclenz/apogee/internal/context"
	"github.com/airiclenz/apogee/internal/domain"
)

// Stale-tool-result Pruning, the engine half. internal/context.Prune is the pure policy — what is
// eligible, in what order, and how a stub reads; this file is everything the policy deliberately
// left to the engine: WHEN a pass runs, whether it is armed, and what the human is told.
//
// It is the cheap, NON-generative reducer of the pair, and it runs FIRST at every Turn boundary
// (loop.go) for exactly that reason: relieving a history of file dumps the model has finished with
// costs no upstream call, so an Exchange that pruning can rescue never pays for a summary. Like
// Compaction it is STRUCTURAL rather than a Mechanism (D6, ADR 0006): the gate reads only
// cfg.Context.PruneToolResults, never cfg.Bypass, because a naked model drowns in its own tool
// output just as surely as a bench arm with every Mechanism on.
//
// Prefix-cache note (ADR 0023 §6). A prune rewrites COMMITTED history, so the upstream server's
// prefix cache is invalidated once per prune — the whole reason internal/context prunes on a wide
// 60%/40% band instead of a single threshold: rare and larger beats frequent and small, and the
// band is what makes the trade affordable rather than something the engine has to schedule around.
//
// What this is NOT: the generative mid-Exchange reducer the issue register still tracks. That
// entry stays open and unclaimed — it wants a summary of the CONVERSATION and its own grill and
// bench evidence, where this is a non-generative rewrite of tool RESULTS that needs neither.

// autoPrune rewrites stale tool results into stubs at a Turn boundary when the history has grown
// past its share of the Budget, and reports the reclaim as one PruneEvent.
//
// It runs at EVERY Turn boundary, mid-Exchange included, on the main agent and a delegated child
// alike — no inExchange guard, unlike the generative fold. It can afford none: the rewrite is in
// place, so no message moves, the assistant → tool adjacency a strict chat template requires
// survives, and the cached exchangeStart and the recorded AssistantBoundaries keep pointing at the
// messages they always did. There is nothing to re-anchor.
//
// Two gates, both cheap: the live `prune-tool-results` switch, and a known context window. Without
// a window the Budget carries no History allocation, and a fraction of nothing is no trigger at
// all — Prune answers the same way, so this is the honest early exit rather than a second policy.
// The rewrite Prune performs is committed to a.conv directly, so the Turn's existing snapshot
// carries it into the session file with no persistence seam of its own.
func (a *Agent) autoPrune(turn int) {
	if !a.pruneEnabled() {
		return
	}
	b := a.budget()
	if b.History <= 0 {
		return
	}
	res := apogeectx.Prune(&a.conv, b, apogeectx.PruneKeepTurns)
	if res.Pruned == 0 {
		return
	}
	// The chars → tokens conversion happens HERE, once, on the same Budget the trigger read: the
	// event carries a number the Driver renders verbatim, so no surface downstream has to hold a
	// ratio of its own to say what a prune freed (domain.PruneEvent).
	a.cfg.Events.Emit(domain.PruneEvent{
		EventBase: a.base(turn),
		Results:   res.Pruned,
		Tokens:    b.EstimateTokens(res.Chars),
	})
}
