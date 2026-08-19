package undo

import "context"

// journalKey is the context key the journal handle rides on. Its own unexported type is
// what keeps the value collision-free: no other package can construct the key, so no
// other package can overwrite — or accidentally read — what dispatch installed here.
type journalKey struct{}

// WithJournal returns ctx carrying j, so the shared write funnel can record what it is
// about to replace without knowing which engine owns the journal, or that an engine owns
// one at all. It is the same shape the confinement box and the write-escape permit reach
// a tool call through: the engine decides once, at dispatch, and the funnel reads the
// answer where the bytes actually move.
//
// A nil journal installs nothing and returns ctx unchanged — the honest encoding of an
// engine that journals nothing, which [FromContext] then reports as nil in its turn.
func WithJournal(ctx context.Context, j *Journal) context.Context {
	if j == nil {
		return ctx
	}
	return context.WithValue(ctx, journalKey{}, j)
}

// FromContext returns the journal installed by [WithJournal], or nil when this execution
// carries none — a tool invoked outside an engine, an engine that keeps no journal, or
// any context that simply never passed through dispatch. Nil means "nothing is
// recording", and every caller treats it as a write that is journalled by no one rather
// than as an error: the mutation itself is never conditional on the journal.
func FromContext(ctx context.Context) *Journal {
	journal, _ := ctx.Value(journalKey{}).(*Journal)
	return journal
}
