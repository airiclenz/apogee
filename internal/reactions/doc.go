// Package reactions is the Hook library: user-configured, observe-only reactions to the engine's
// event stream, composed by every Driver from this one package so the TUI, `apogee headless`
// and a daemon Firing fire the same `hooks:` list (ADR 0073).
//
// A Hook is strictly one-way. Nothing it prints, returns or answers reaches the model, the
// conversation or the Session record, and it can neither veto nor delay the loop: the package
// decorates a domain.EventSink, so Emit returns nothing and there is no seam through which a
// verdict could travel back. It is not a seam Reaction and fires at no seam: its events ARE the
// five notice Moments of the Reaction core (ADR 0076) — it runs after the fact, on the user's own
// machine, outside confinement, as the user's config rather than a model action.
//
// One direction: this package imports internal/domain for the events it reads and
// internal/security for path resolution, and nothing else in the tree — never internal/agent,
// never internal/tools, never internal/tui. The facts it cannot derive from domain alone
// arrive injected: the write target of a tool call comes in as a WriteTarget func, which every
// root supplies over its own registry lookup.
//
// # The files, one line each
//
// hooks.go is the vocabulary and the entry shape — the five Event constants, which are the notice
// Moments of the Reaction core under an alias (ADR 0076), Events/ParseEvent, and the
// Validate/ValidateAll rules that refuse a malformed domain.Reaction with a sentence naming it.
// payload.go is the JSON document a firing Hook receives on stdin or in a POST body — the
// documented field contract, plus the small APOGEE_HOOK_* environment set Env derives from it.
// match.go is the pure mapping from one domain.Event to the Hook events it produces, built over
// the SUBSCRIBED set so an unsubscribed event costs nothing.
// workspace.go is the one path resolution the `workspace:` filter and the root's own workspace
// are both compared through, so the two readings can never disagree.
// runner.go is the sink decorator itself — the Executor seam, the per-Hook queues and workers,
// the Driver-facing failure reporter, and the reload (Replace) and shutdown (Close) paths.
// exec.go is the production Executor — DefaultExecutor's dispatch onto whichever action the entry
// configured, and the one JSON encoding both actions send.
// command.go runs a Hook's argv: the api-key-cmd exec posture, copied, with the payload on stdin
// and the APOGEE_HOOK_* facts in the environment.
// webhook.go POSTs the same document to a Hook's URL, with the literal and environment-resolved
// headers it carries and no retry.
package reactions
