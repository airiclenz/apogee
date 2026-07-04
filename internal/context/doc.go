// Package context manages the model's working context: Budget allocation, the
// context builder, generative Compaction (the default reducer), and tool-result
// capping. It is three of the four context-reduction seams; the fourth, History
// truncation, is a separate off-by-default Mechanism (package mechanisms).
//
// Generative Compaction is implemented (Compact): it summarizes a conversation and
// replaces the folded history with a single summary message, keeping the protected
// prefix verbatim. The transcript the summary call carries is bounded to a character
// budget derived from the discovered context window (keep the prefix + a budgeted tail,
// elide the middle) so the call cannot overflow at exactly the high fill /compact exists
// to relieve. Agent.Compact drives it on demand (the /compact command).
//
// Budget allocation and honest token accounting are implemented (Allocate, TokenEstimator):
// Allocate splits the discovered context window across the parts of a request (response reserve,
// system prompt, file context, history — the single authority CONTEXT names), and TokenEstimator
// calibrates a chars→token ratio against each Turn's server-reported usage so LoopView.Budget()
// reports an honest fill.
//
// Both Budget consumers are now wired (Phase-4 item 9). HistoryExceedsAllocation is the automatic
// Compaction trigger's decision — the loop folds the conversation (the same generative Compact the
// TUI's /compact drives) when the history has outgrown its Budget allocation at a quiescent
// boundary. Tool-result capping is the second consumer, but it lives in package mechanisms
// (tool_result_cap): it is a config-gated pre-request Mechanism, not structural, so it reads the
// Budget through the hook surface rather than living here.
package context
