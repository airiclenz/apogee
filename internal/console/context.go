package console

import "context"

// registryKey is the context key the console registry rides on. Its own unexported type is what
// keeps the value collision-free: no other package can construct the key, so no other package can
// overwrite — or accidentally read — what dispatch installed here.
type registryKey struct{}

// WithRegistry returns ctx carrying r, so a console tool can reach the engine's live Consoles
// without holding them itself. That indirection is the point: tool instances are rebuilt when the
// roster changes mid-session, and a process that was rebuilt away would be a process nobody can
// close — so the registry lives on the engine and arrives where the call does, the same way the
// undo journal, the confinement box and the write-escape permit do.
//
// A nil registry installs nothing and returns ctx unchanged — the honest encoding of an execution
// that keeps no consoles, which [FromContext] then reports as nil in its turn.
func WithRegistry(ctx context.Context, r *Registry) context.Context {
	if r == nil {
		return ctx
	}
	return context.WithValue(ctx, registryKey{}, r)
}

// FromContext returns the registry installed by [WithRegistry], or nil when this execution carries
// none — a tool invoked outside an engine, or any context that never passed through dispatch. Nil
// means "this execution has no consoles", and a caller that finds one has been handed a call it
// cannot serve rather than an error to pass on.
func FromContext(ctx context.Context) *Registry {
	registry, _ := ctx.Value(registryKey{}).(*Registry)
	return registry
}
