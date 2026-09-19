// Package reactions is the Reaction library: user-configured, observe-only reactions to the
// engine's event stream, composed by every Driver from this one package so the TUI, `apogee
// headless` and a daemon Firing fire the same `reactions:` list (ADR 0073).
//
// A Reaction is strictly one-way. Nothing it prints, returns or answers reaches the model, the
// conversation or the Session record, and it can neither veto nor delay the loop: the package
// decorates a domain.EventSink, so Emit returns nothing and there is no seam through which a
// verdict could travel back. It is not a seam Reaction and fires at no seam: its events ARE the
// five notice Moments of the Reaction core (ADR 0076) — it runs after the fact, on the user's own
// machine, outside confinement, as the user's config rather than a model action.
//
// One direction: this package imports internal/domain for the events it reads, internal/security
// for path resolution, internal/userexec for the exec posture a Reaction's argv runs under and
// internal/webhook for the one POST both lanes' webhooks send, and nothing else in the tree —
// never internal/agent, never internal/tools, never internal/tui. Every fact a firing needs rides
// the Event itself: the file a tool call changed arrives on domain.ToolResultEvent.WriteTarget,
// stamped by the engine from the one resolution its blast-radius ladder judged the call by, so no
// root injects anything.
//
// # The files, one line each
//
// hooks.go is the vocabulary and the entry shape — the five Event constants, which are the notice
// Moments of the Reaction core under an alias (ADR 0076), Events/ParseEvent, and the
// Validate/ValidateAll rules that refuse a malformed domain.Reaction with a sentence naming it.
// payload.go is the observe lane's share of the payload: the document itself is domain.SeamPayload —
// the ONE JSON document every fired out-of-process Reaction reads, on either lane, with the
// APOGEE_REACTION_* environment set its Env derives — and this file holds the ScheduleRef alias the
// roots hand Options and the per-seam projections a seam-closing notice carries under "value".
// match.go is the pure
// mapping from one domain.Event to the Reaction events it produces, built over the SUBSCRIBED set
// so an unsubscribed event costs nothing; it is also where a closed seam's working value is
// projected, while the engine's Emit is still running. workspace.go is the one path resolution the
// `workspace:` filter and the root's own workspace are both compared through, so the two readings
// can never disagree. runner.go is the sink decorator itself — the Executor seam, the per-Reaction
// queues and workers, the Driver-facing failure reporter, and the reload (Replace) and shutdown
// (Close) paths. exec.go is the production Executor — DefaultExecutor's dispatch onto whichever
// action the entry configured, and the one JSON encoding both actions send. command.go runs a
// Reaction's argv through internal/userexec — the api-key-cmd exec posture — with the payload on
// stdin and the APOGEE_REACTION_* facts in the environment. webhook.go POSTs the same document to a
// Reaction's URL through internal/webhook — the request the sync lane's `advise:`/`gate:` webhooks
// share — and discards the reply.
package reactions
