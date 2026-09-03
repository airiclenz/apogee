package tasklist

import "context"

// listKey is the context key the task list rides on. Its own unexported type is what
// keeps the value collision-free: no other package can construct the key, so no other
// package can overwrite — or accidentally read — what dispatch installed here.
type listKey struct{}

// WithList returns ctx carrying l, so the task-list tool can reach the engine's list
// without holding it itself. That indirection is the point: tool instances are rebuilt
// when the roster changes mid-session, and a list held by a tool that was rebuilt away
// would be a checklist nobody can update — so the list lives on the engine and arrives
// where the call does, the same way the undo journal and the console registry do.
//
// A nil list installs nothing and returns ctx unchanged — the honest encoding of an
// execution that keeps no list, which [FromContext] then reports as nil in its turn.
func WithList(ctx context.Context, l *List) context.Context {
	if l == nil {
		return ctx
	}
	return context.WithValue(ctx, listKey{}, l)
}

// FromContext returns the list installed by [WithList], or nil when this execution
// carries none — a tool invoked outside an engine, or any context that never passed
// through dispatch. Nil means "this execution has no task list", and a caller that finds
// one has been handed a call it cannot serve rather than an error to pass on.
func FromContext(ctx context.Context) *List {
	list, _ := ctx.Value(listKey{}).(*List)
	return list
}
