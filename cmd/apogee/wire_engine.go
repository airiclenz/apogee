package main

// The engine seam of the composition root, lifted out of wire.go by concern (ADR 0043).
//
// How the Agent comes into being and what stands in until it does: the one call that constructs or
// resumes it through the public surface, the late-bound [tui.Engine] the renderer drives from launch
// (ADR 0036 decision 3), and the translation of a refused construction into a message a user can act
// on.

import (
	"context"
	"errors"
	"sync"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/session"
	"github.com/airiclenz/apogee/internal/tui"
	"github.com/airiclenz/apogee/internal/undo"
)

// ----------------------------------------------------------------------------
// Agent construction (dogfooding the public surface — C5)
// ----------------------------------------------------------------------------

// buildAgent constructs a fresh Agent, or resumes one from an already-loaded session record when
// resumed is non-nil (a --resume/--continue start; resolveResume owns the id-or-path lookup and the
// legacy-file wrapping). Both go through the public apogee surface. A future-version snapshot
// surfaces apogee.ErrSessionVersion from Resume, which carries its own clear message.
func buildAgent(cfg apogee.Config, resumed *session.Record) (*apogee.Agent, error) {
	if resumed == nil {
		return apogee.New(cfg)
	}
	return apogee.Resume(cfg, resumed.Session)
}

// ----------------------------------------------------------------------------
// The late-bound engine (ADR 0036 decision 3: construction waits for a server)
// ----------------------------------------------------------------------------

// lateEngine is the [tui.Engine] the composition root hands the renderer, and the holder of the
// Agent behind it. It exists because the engine cannot always be built at launch: ADR 0024 keeps
// construction impossible without an endpoint, and ADR 0036 lets a session start before anyone has
// said which endpoint that is — so the seam must exist before the thing behind it does.
//
// Unbound, every call that needs a conversation is refused with errNoServerBound and every read
// answers what "nothing is bound" honestly is: no open Exchange, no context files, no snapshot.
// Nothing panics and nothing silently pretends to have worked, because the renderer reaches this
// type through the same interface either way.
//
// The settings a human can move before a server exists — the autonomy mode, Auto's blast radius and
// the session's Thinking effort — are held here and applied to the Agent the moment it is
// constructed. Without that, a mode cycled while the picker was open would show in the footer and be
// nowhere in the engine.
//
// The mutex guards the pointer and those values. Beat-style long calls (Step, Compact) read the
// pointer under the lock and then run OUTSIDE it, like upstreamHolder does with its Monitor, so a
// Step that takes a minute never holds the Update loop's next call behind it.
type lateEngine struct {
	mu      sync.Mutex
	agent   *apogee.Agent
	mode    apogee.Mode
	confine bool

	// effort is the session's Thinking-effort override (ADR 0050), held here for the reason the two
	// above are: /effort runs before a server is chosen, and a level set while the picker was open
	// must not be a level the engine never hears about. It needs no "was it moved" pointer like the
	// three below, because its zero value already MEANS no override — which is exactly the state a
	// freshly constructed Agent is in, so a bind can restate it unconditionally.
	effort domain.ThinkingEffort

	// The settings surface's anytime-safe mutators, held for the same reason the two above are: an
	// edit committed while the server picker is still up must not fall between the file and the
	// engine. A nil pointer means the key was never moved HERE, so a bind leaves the Agent on the
	// seed its Config carried — which is the difference between "not moved" and "moved to false".
	pendingBypass       *bool
	pendingCompaction   *bool
	pendingContextFiles *contextFileChoice
	// pendingProfile is the same idea for the one IDLE-ONLY mutator that has to be remembered: a
	// dialect swap needs an Agent to build its parsers, but a bind with no memory of the edit would
	// install the profile the process started with (see SetProfile).
	pendingProfile *apogee.ModelProfile
	// pendingDelegation is the last Delegation target pushed while there was no Agent to latch it on
	// (ADR 0045). The second heartbeat beats from the moment the TUI starts, which on a pre-bound
	// session is before any engine exists, so without this the routing a resolved Sub-agent server
	// already earned would wait for the beat AFTER the human picked a server. nil means nothing
	// usable was ever resolved — which is also what a cleared target leaves behind, and both mean the
	// same thing to a bind: latch nothing, delegations run on the session's own Upstream.
	pendingDelegation *apogee.DelegationTarget
}

// contextFileChoice is one remembered SetContextFiles call. The pair travels together because
// either half alone is a different instruction: names with the switch off install nothing.
type contextFileChoice struct {
	enable bool
	names  []string
}

// The composition root's engine holder satisfies every seam it is handed across: the renderer's
// narrow Engine, the switcher half of the shared move (sessionMover), and the live-apply
// dispatcher's mutator surface.
var (
	_ tui.Engine       = (*lateEngine)(nil)
	_ upstreamSwitcher = (*lateEngine)(nil)
	_ settingsEngine   = (*lateEngine)(nil)
)

// errNoServerBound is what every conversation-touching call answers while the session has no
// upstream. It names the way out, because the one surface it can reach is a note the human reads.
var errNoServerBound = errors.New("apogee: no server is bound yet — choose one with /server")

// errAlreadyBound refuses a SECOND construction on a holder that already has an Agent. It names
// `/server` because that verb does what the caller meant: move the session it already has.
var errAlreadyBound = errors.New("apogee: this session already has a server — use /server to switch")

// newLateEngine builds the unbound holder, seeded with the autonomy mode and blast radius this run
// resolved — the same two values the Config carries, so a bind that happens immediately restates
// them and a bind that happens after the human moved one applies what they moved.
func newLateEngine(mode apogee.Mode, confineToWorkspace bool) *lateEngine {
	return &lateEngine{mode: mode, confine: confineToWorkspace}
}

// Bind constructs the Agent through construct and installs it as this session's engine. The
// construction happens UNDER the lock, which is what makes "exactly one Agent" a property of the
// type rather than of its callers: a second Bind is refused before construct is ever called, so no
// engine is ever built that nothing holds. It costs nothing to hold the lock across it — apogee.New
// reaches no network (startup makes no probe, ADR 0024) — and a failed construction leaves the
// holder unbound, free to try another server.
func (e *lateEngine) Bind(construct func() (*apogee.Agent, error)) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.agent != nil {
		return errAlreadyBound
	}
	agent, err := construct()
	if err != nil {
		return err
	}
	// What the human may have moved while there was nothing to move it on.
	agent.SetMode(e.mode)
	agent.SetConfineToWorkspace(e.confine)
	agent.SetEffortOverride(e.effort)
	if e.pendingBypass != nil {
		agent.SetBypass(*e.pendingBypass)
	}
	if e.pendingCompaction != nil {
		agent.SetCompactionEnabled(*e.pendingCompaction)
	}
	if c := e.pendingContextFiles; c != nil {
		agent.SetContextFiles(c.enable, c.names)
	}
	if t := e.pendingDelegation; t != nil {
		agent.SetDelegationTarget(t)
	}
	// The one remembered value that can be REFUSED: a dialect this build cannot parse. The Agent is
	// released and the bind fails, which is exactly what a config carrying that profile at launch
	// does — a session is never installed reading its model's replies in a language it does not have.
	if p := e.pendingProfile; p != nil {
		if err := agent.SetProfile(*p); err != nil {
			_ = agent.Close()
			return err
		}
	}
	e.agent = agent
	return nil
}

// bound reports whether an Agent is installed — the pointer read every method below opens with.
func (e *lateEngine) bound() *apogee.Agent {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.agent
}

// Submit enqueues user input, or refuses when there is no engine to enqueue it in.
func (e *lateEngine) Submit(in apogee.UserInput) error {
	agent := e.bound()
	if agent == nil {
		return errNoServerBound
	}
	return agent.Submit(in)
}

// Step advances the loop one Turn. It runs outside the lock: a Turn is a network call.
func (e *lateEngine) Step(ctx context.Context) (apogee.StepResult, error) {
	agent := e.bound()
	if agent == nil {
		return apogee.StepResult{}, errNoServerBound
	}
	return agent.Step(ctx)
}

// Snapshot captures the conversation, or refuses: an unbound session has no conversation to save.
func (e *lateEngine) Snapshot() (apogee.Session, error) {
	agent := e.bound()
	if agent == nil {
		return apogee.Session{}, errNoServerBound
	}
	return agent.Snapshot()
}

// Interject commits a message into the open Exchange; unbound there is none.
func (e *lateEngine) Interject(in apogee.UserInput) error {
	agent := e.bound()
	if agent == nil {
		return errNoServerBound
	}
	return agent.Interject(in)
}

// ClearContext drops the model's history; unbound there is no history to drop.
func (e *lateEngine) ClearContext() error {
	agent := e.bound()
	if agent == nil {
		return errNoServerBound
	}
	return agent.ClearContext()
}

// AbortExchange discards a cancelled Exchange. Unbound it is a no-op rather than a refusal: it
// answers nothing, it is called on a path that is already unwinding, and there is nothing to abort.
func (e *lateEngine) AbortExchange() {
	if agent := e.bound(); agent != nil {
		agent.AbortExchange()
	}
}

// RestoreSession swaps a stored snapshot into the live Agent — which a pre-bound session does not
// have, so the /sessions restore is refused until a server is chosen.
func (e *lateEngine) RestoreSession(sess apogee.Session) error {
	agent := e.bound()
	if agent == nil {
		return errNoServerBound
	}
	return agent.RestoreSession(sess)
}

// InExchange reports whether an Exchange is open — false with nothing bound, which is exactly true.
func (e *lateEngine) InExchange() bool {
	agent := e.bound()
	return agent != nil && agent.InExchange()
}

// ContextFilesReport reports what the workspace context files contributed. Unbound the report is
// empty, which the renderer already reads as "nothing to note" (it notes a report with no files
// exactly as it notes none at all).
func (e *lateEngine) ContextFilesReport() apogee.ContextFilesReport {
	agent := e.bound()
	if agent == nil {
		return apogee.ContextFilesReport{}
	}
	return agent.ContextFilesReport()
}

// Compact folds the conversation through the upstream; unbound there is neither.
func (e *lateEngine) Compact(ctx context.Context) (bool, error) {
	agent := e.bound()
	if agent == nil {
		return false, errNoServerBound
	}
	return agent.Compact(ctx)
}

// SetMode changes the autonomy mode. Unbound it is REMEMBERED rather than dropped, so the mode the
// footer shows is the mode the engine is constructed into.
func (e *lateEngine) SetMode(mode apogee.Mode) {
	e.mu.Lock()
	e.mode = mode
	agent := e.agent
	e.mu.Unlock()
	if agent != nil {
		agent.SetMode(mode)
	}
}

// SetConfineToWorkspace changes Auto's blast radius, remembered while unbound for SetMode's reason.
func (e *lateEngine) SetConfineToWorkspace(confine bool) {
	e.mu.Lock()
	e.confine = confine
	agent := e.agent
	e.mu.Unlock()
	if agent != nil {
		agent.SetConfineToWorkspace(confine)
	}
}

// SetBypass switches Mechanisms off or back on for the rest of the session (the settings surface's
// `bypass` key), remembered while unbound for SetMode's reason: the pane can be opened before a
// server is chosen, and an edit that persisted must not be the only half that happened.
func (e *lateEngine) SetBypass(enabled bool) {
	e.mu.Lock()
	e.pendingBypass = &enabled
	agent := e.agent
	e.mu.Unlock()
	if agent != nil {
		agent.SetBypass(enabled)
	}
}

// SetCompactionEnabled arms or disarms the automatic Compaction trigger (`auto-compact`), on the
// same terms as SetBypass above.
func (e *lateEngine) SetCompactionEnabled(enabled bool) {
	e.mu.Lock()
	e.pendingCompaction = &enabled
	agent := e.agent
	e.mu.Unlock()
	if agent != nil {
		agent.SetCompactionEnabled(enabled)
	}
}

// SetContextFiles replaces the workspace context-file names folded in at the next session boundary
// (`context-files:`), on the same terms as the two above. The pair is remembered together because
// the switch and the names are one instruction.
func (e *lateEngine) SetContextFiles(enable bool, names []string) {
	e.mu.Lock()
	e.pendingContextFiles = &contextFileChoice{enable: enable, names: names}
	agent := e.agent
	e.mu.Unlock()
	if agent != nil {
		agent.SetContextFiles(enable, names)
	}
}

// SetParallelAgents moves the depth-0 fan-out width (ADR 0039). Unlike the four setters above it is
// deliberately NOT remembered while unbound, and does not need to be: the cap is a property of the
// server, so there is nothing to remember until one is chosen — and the bind that chooses it seeds
// the width straight into the Config the Agent is constructed from (serverBinder.bind). A push at an
// unbound holder is therefore the honest no-op it looks like, which is what lets the cap holder call
// this on every path without asking whether a session has a server yet.
func (e *lateEngine) SetParallelAgents(width int) {
	if agent := e.bound(); agent != nil {
		agent.SetParallelAgents(width)
	}
}

// SetDelegationTarget latches the Sub-agent server delegations route to, or clears it with nil so
// they fall back to this session's own Upstream (ADR 0045). It is the anytime-safe class the four
// setters above belong to — the engine seam behind it takes one lock and is deliberately never
// idle-gated, because a beat lands squarely mid-Exchange for any delegation-heavy Turn.
//
// It is REMEMBERED while unbound, unlike SetParallelAgents beside it, and the difference is where
// the value comes from: a cap is a property of the server the bind itself resolves, while a target
// is resolved by a monitor that is already beating — so there IS something to remember, and a bind
// that forgot it would leave a perfectly usable Sub-agent server unrouted until its next beat.
func (e *lateEngine) SetDelegationTarget(target *apogee.DelegationTarget) {
	e.mu.Lock()
	e.pendingDelegation = target
	agent := e.agent
	e.mu.Unlock()
	if agent != nil {
		agent.SetDelegationTarget(target)
	}
}

// SwapTools replaces the session's tool set outright (ADR 0037 binding F — the one door for a
// tool-set change). Unlike the four setters above it is NOT remembered while unbound, and does not
// need to be: the tools an unbound session will start with are Config.Tools, the registry the
// composition root holds and rebuilds directly, so nothing is lost by refusing here — there is
// simply no Agent yet whose set could differ from it.
func (e *lateEngine) SwapTools(registry *apogee.ToolRegistry) error {
	agent := e.bound()
	if agent == nil {
		return errNoServerBound
	}
	return agent.SwapTools(registry)
}

// SetProfile swaps the dialect the session reads responses in (`model-profiles`, ADR 0037's other
// idle-only door — the SAME-model half of ADR 0044's two doors, a model change taking Rebind). Unbound it is REMEMBERED and installed at the bind, like the anytime-safe setters
// above and unlike SwapTools: a tool registry has a carrier across the bind already — Config.Tools,
// which the composition root holds and hands over — while the profile a bind would otherwise install
// is the one the process STARTED with, so a pre-bound edit that was only remembered nowhere would be
// an edit that waits for the next launch, which ADR 0037 exists to abolish.
//
// A remembered profile is validated where a bound one is: at the Agent. It is built before anything
// is submitted, so the only way it can fail there is a dialect this build cannot parse — the same
// failure the file would have produced at construction had it carried that profile at launch, and it
// is reported the same way, as a bind that did not happen.
func (e *lateEngine) SetProfile(profile apogee.ModelProfile) error {
	e.mu.Lock()
	agent := e.agent
	if agent == nil {
		e.pendingProfile = &profile
		e.mu.Unlock()
		return nil
	}
	e.mu.Unlock()
	return agent.SetProfile(profile)
}

// ConfineToWorkspace reports the blast radius the next tool call will read: the Agent's own once
// there is one, and until then the value a bind would install.
func (e *lateEngine) ConfineToWorkspace() bool {
	e.mu.Lock()
	agent, confine := e.agent, e.confine
	e.mu.Unlock()
	if agent != nil {
		return agent.ConfineToWorkspace()
	}
	return confine
}

// UndoPreview describes what `/undo` would put back, or reports nothing to undo while the session
// is unbound — an unbound holder has run no tool call, so there is no write to reverse.
func (e *lateEngine) UndoPreview() (undo.Step, bool) {
	agent := e.bound()
	if agent == nil {
		return undo.Step{}, false
	}
	return agent.UndoPreview()
}

// UndoRevert executes the previewed step against the bound Agent's journal. Unbound it answers
// undo.ErrNothingToUndo rather than errNoServerBound: nothing was written, so the honest refusal is
// the empty journal's and not "pick a server" — /undo has no business with the upstream.
func (e *lateEngine) UndoRevert(generation uint64) (undo.Report, error) {
	agent := e.bound()
	if agent == nil {
		return undo.Report{}, undo.ErrNothingToUndo
	}
	return agent.UndoRevert(generation)
}

// SetEffortOverride states the session's Thinking effort (the /effort command, ADR 0050), remembered
// while unbound for SetMode's reason — /effort is safe to run before a server is chosen, and a level
// the human set there must reach the Agent the bind constructs. The zero value clears the override
// on both halves at once: it is what a cleared holder passes at the next bind, and what the Agent
// reads as "no override" today.
func (e *lateEngine) SetEffortOverride(effort domain.ThinkingEffort) {
	e.mu.Lock()
	e.effort = effort
	agent := e.agent
	e.mu.Unlock()
	if agent != nil {
		agent.SetEffortOverride(effort)
	}
}

// ThinkingEffort reports the two layers the next request resolves its effort from: the Agent's own
// once there is one, and until then the override a bind would install with no profile behind it —
// because an unbound session has no bound model, so there is no profile setting to report yet.
func (e *lateEngine) ThinkingEffort() (override, profile domain.ThinkingEffort) {
	e.mu.Lock()
	agent, effort := e.agent, e.effort
	e.mu.Unlock()
	if agent != nil {
		return agent.ThinkingEffort()
	}
	return effort, ""
}

// Close releases the Agent, or nothing at all when a session ends without ever binding one.
func (e *lateEngine) Close() error {
	agent := e.bound()
	if agent == nil {
		return nil
	}
	return agent.Close()
}

// Rebind swaps in the per-model bindings a heartbeat observation implies (the composition root's
// rebind closure). A beat cannot land before a Monitor exists, so unbound this is a backstop.
func (e *lateEngine) Rebind(spec apogee.RebindSpec) error {
	agent := e.bound()
	if agent == nil {
		return errNoServerBound
	}
	return agent.Rebind(spec)
}

// SwitchUpstream re-points the session at another server (sessionMover.move). Unbound there is
// nothing to re-point: the first server arrives through Bind above, not through a move.
func (e *lateEngine) SwitchUpstream(spec apogee.UpstreamSpec) error {
	agent := e.bound()
	if agent == nil {
		return errNoServerBound
	}
	return agent.SwitchUpstream(spec)
}

// errAutoUnavailable is the friendly framing of ErrAutoUnavailable: Auto needs
// filesystem-write confinement, which this host cannot provide (no landlock on Linux, no
// sandbox-exec on macOS — ADR 0012). The lower rungs of the ladder still work.
var errAutoUnavailable = errors.New(
	"apogee: auto mode requires filesystem-write confinement, which is unavailable on this host " +
		"(Linux needs landlock — kernel ≥5.13; macOS needs sandbox-exec) — " +
		"use --mode plan, --mode ask-before, or --mode allow-edits")

// friendlyConstructErr maps construction errors to actionable CLI messages. The headline
// case is Auto mode: New returns ErrAutoUnavailable when Mode==Auto but the host's Confiner
// cannot enforce filesystem confinement.
func friendlyConstructErr(err error) error {
	if errors.Is(err, apogee.ErrAutoUnavailable) {
		return errAutoUnavailable
	}
	return err
}
