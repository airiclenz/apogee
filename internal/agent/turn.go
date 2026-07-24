package agent

import "github.com/airiclenz/apogee/internal/domain"

// turnLifecycle owns the loop's Turn/Exchange lifecycle state — where the loop stands
// between quiescent boundaries (ADR 0007) — and, from item 2 on, the exits that mutate it.
// It coordinates the two collaborators the exits touch together: the conversation (rollback,
// deferred queue) and the self-regulator (judge vs discard). Same-package, unexported: the
// Turn is the loop's concept, not a public seam.
type turnLifecycle struct {
	conv    *domain.Conversation
	tracker *selfRegulator

	index         int  // 0-based index of the next Turn (was Agent.turnIndex)
	inExchange    bool // true between Submit and the Step that completes the Exchange
	exchangeStart int  // cached rollback boundary of the open Exchange (ADR 0017 §2's recorded fallback)
}
