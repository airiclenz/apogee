// Package context manages the model's working context: Budget allocation, the
// context builder, generative Compaction (the default reducer), and tool-result
// capping. It is three of the four context-reduction seams; the fourth, History
// truncation, is a separate off-by-default Mechanism (package mechanisms).
//
// Generative Compaction is implemented (Compact): it summarizes a conversation and
// replaces the folded history with a single summary message, keeping the protected
// prefix verbatim. Agent.Compact drives it on demand (the /compact command). Budget
// allocation, the context builder, and tool-result capping remain scaffolds.
package context
