package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/run"
	"github.com/airiclenz/apogee/internal/schedule"
	"github.com/airiclenz/apogee/internal/session"
)

// ----------------------------------------------------------------------------
// The scheduler's composition (ADR 0033)
// ----------------------------------------------------------------------------
//
// Three seams make a Schedule live, and the binary owns all three because each is made of facts
// only it holds: WHAT a Firing runs against (the current Upstream binding, the roots, the
// confinement posture), WHEN one is allowed to start (the interactive session's own activity), and
// WHERE its narration goes (the running Bubble Tea program). The library owns everything else — the
// cycle, the overlap skip, the lifetime — and knows nothing about any of the above.

// scheduleWiring turns one Firing into one headless run. It is the titleWiring split, made for the
// same reason: the endpoint, the key, the model and the roots are wiring the binary resolves and a
// `/server` switch moves, so the Config is composed PER FIRING from the binding read at that moment
// rather than captured when the Schedule was created. A Schedule made before a switch fires against
// the server the session is on now.
type scheduleWiring struct {
	// base is this session's own construction surface — the workspace and config roots, the
	// confinement backend and its posture, the tools, skills, context files and profile. A Firing
	// inherits it wholesale so an unattended run has exactly the session's shape, and fire()
	// overrides only what a Firing must decide differently (below).
	base apogee.Config

	// The inputs rebindSpecFor re-resolves a per-model binding from. They are held rather than folded
	// into base because a Firing binds the model that is CURRENT, and the system prompt (ADR 0023) and
	// the validated Mechanism set (ADR 0016) are both keyed on it: a session that rebound mid-run would
	// otherwise fire the launch model's prompt at the model it has moved to. opts is the launch
	// snapshot; live is the half of it a `/settings` edit can have moved since (ADR 0037), read at
	// FIRING time for the same reason the binding is — and safely, because the holder is
	// goroutine-safe and this runs on the Scheduler's own goroutine.
	opts  options
	roots stateRoots
	live  *liveSettings

	// binding reads the CURRENT Upstream binding; wired to upstreamHolder.Binding, the same seam the
	// naming call reads for the same reason.
	binding func() upstreamBinding

	// store is the session store a Firing's record lands in — the interactive session's own, so a
	// Firing shows up in /sessions beside the conversations it ran beneath (items 2 and 7).
	store *session.Store
}

// fire performs one Firing and reports the record it left behind. It is the value wired into
// schedule.Config.Fire, and it runs on the scheduler's goroutine — never the Update loop's — which
// is why everything it touches is either immutable, its own copy, or explicitly goroutine-safe (the
// holder, the store, the skills Provider, the Confiner).
//
// The delegates that assume a human are cleared rather than passed: run.Once pins its own fail-safe
// denier and leaves ask_user and present_document unregistered (ADR 0033, decision 2), and handing
// it the Bridge's would only invite a Firing to rendezvous with a UI that is not listening for it.
// Tools are cleared for the same class of reason: MCP connections are live host state re-established
// per session (ADR 0008, ADR 0022 §8), so a Firing takes the library's own registry rather than the
// session's MCP-augmented one, and reaches no external server at all.
func (w scheduleWiring) fire(ctx context.Context, f schedule.Firing) (schedule.Outcome, error) {
	binding := w.binding()
	// The per-model half of the Config, resolved exactly as a rebind resolves it — a Firing must
	// land in the state a session started on this model would be in. The observed window is passed
	// as unknown because only the TUI tracks it; a `context-window:` pin still wins, and an unpinned
	// Firing runs with the Budget inactive, which for one bounded prompt is the honest degrade
	// rather than a guess. The per-session notices are dropped: they are a launch's narration, and a
	// Firing's narration is its own session record.
	// The launch snapshot's own wire, restated as a binding so the overlay is a no-op for this
	// caller: a Firing still keys its spec resolution on the LAUNCH endpoint, unchanged.
	launch := upstreamBinding{Endpoint: w.opts.endpoint, APIKey: w.opts.apiKey}
	base, manualIDs, pinnedWindow := w.live.rebindInputs(w.opts, launch)
	spec, _, err := rebindSpecFor(base, w.roots, manualIDs, binding.Model, 0, pinnedWindow)
	if err != nil {
		return schedule.Outcome{}, fmt.Errorf("apogee: resolve the firing's bindings: %w", err)
	}

	cfg := w.base
	cfg.Endpoint, cfg.APIKey = binding.Endpoint, binding.APIKey
	cfg.Model, cfg.SystemPrompt = spec.Model, spec.SystemPrompt
	cfg.EnableMechanisms = spec.EnableMechanisms
	cfg.Context.MaxContextTokens = spec.MaxContextTokens
	// The mode is the Schedule's, chosen explicitly at creation and never inherited from the
	// session's own (ADR 0033, decision 3). Auto's eligibility was ruled on there, at the surface
	// that offered it — scheduleAutoBlocked below — exactly as agent.New trusts a Config that says
	// Auto.
	cfg.Mode = f.Mode
	cfg.Tools = nil
	cfg.Events, cfg.Approver, cfg.Asker, cfg.Presenter = nil, nil, nil, nil

	res, err := run.Once(ctx, run.Spec{
		Config:       cfg,
		Prompt:       f.Prompt,
		ScheduleID:   f.ScheduleID,
		ScheduleName: f.ScheduleName,
		Store:        w.store,
	})
	// Everything the run learned about itself, mapped onto the scheduler's report in one place so
	// both ends of this function tell the surface the same story. The library reads none of it — it
	// is runner-agnostic (ADR 0033) — and a Driver renders the Firing from these fields alone: the
	// answer without decoding a record, the stats without a second seam onto the run.
	out := schedule.Outcome{
		RecordID:  res.SessionID,
		Title:     res.Title,
		FinalText: res.FinalText,
		Turns:     res.Turns,
		Denied:    res.Denied,
	}
	if err != nil {
		// A failed Firing still reports what it salvaged: run.Once fills its Result with whatever
		// it managed BEFORE it stopped, and a surface that has already announced this Firing can
		// point a human at the partial record rather than at nothing. The id ALSO stays in the
		// error text — that is what a Driver reading only the failure's wording has to go on.
		if res.SessionID != "" {
			return out, fmt.Errorf("%w (partial run saved as %s)", err, res.SessionID)
		}
		return out, err
	}
	return out, nil
}

// idleGate is the host half of schedule.Config.Gate: the TUI publishes its activity through
// [tui.Options.ReportActivity], and a due Firing waits here until this session is quiescent
// (ADR 0033, decision 7). The single-slot local server is the whole reason — a Firing dialling it
// while the human's Exchange streams costs the human their turn — and the release point is the
// Exchange's end rather than a Turn's, which is a fact the Model publishes and this side only obeys.
//
// It starts OPEN. A session that has not yet said anything is a session sitting at its prompt, and
// the first report it ever sends is the one that says it started working.
type idleGate struct {
	mu   sync.Mutex
	busy bool
	// idle is closed on every busy→idle transition and immediately replaced, so any number of
	// waiters are released by one close without the gate holding a list of them. A channel rather
	// than a sync.Cond because a waiting Firing must also be released by its context ending, and
	// Cond.Wait cannot select.
	idle chan struct{}
}

// newIdleGate builds an open gate.
func newIdleGate() *idleGate { return &idleGate{idle: make(chan struct{})} }

// report records what the session is doing. It is the value wired into
// [tui.Options.ReportActivity] and is called from the Update loop; a repeat of the value already
// held is a no-op, so a publisher that reports more often than it transitions stays correct.
func (g *idleGate) report(busy bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if busy == g.busy {
		return
	}
	g.busy = busy
	if !busy {
		close(g.idle)
		g.idle = make(chan struct{})
	}
}

// wait blocks until the session is quiescent, and returns ctx's error when the Schedule is stopped
// or the Scheduler closed while it waits — the contract schedule.Config.Gate states, and what keeps
// Close from being held open by a Firing that is only ever going to be waiting.
//
// The loop re-reads under the lock after every wake rather than trusting the close: two Firings can
// be released by one transition, and the session may have gone busy again before the second one is
// scheduled.
func (g *idleGate) wait(ctx context.Context) error {
	for {
		g.mu.Lock()
		busy, released := g.busy, g.idle
		g.mu.Unlock()
		if !busy {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-released:
		}
	}
}

// scheduleAutoBlocked is the reason a Schedule may not be created in auto mode on this host, or ""
// when it may — the value the renderer reads as [tui.Options.ScheduleAutoBlocked], where one string
// is both the verdict and its wording so the disabled picker row and the refused `auto` argument can
// never disagree. The unattended run it speaks of is a Firing, which is what a Schedule's auto run
// is; every other word of the verdict is autoUnattendedBlocked's.
func scheduleAutoBlocked(backend string, caps apogee.ConfinementCaps, confineToWorkspace bool) string {
	return autoUnattendedBlocked("a firing", backend, caps, confineToWorkspace)
}

// autoUnattendedBlocked is the reason an UNATTENDED auto run may not start on this host, or "" when
// it may. Two surfaces offer Auto without a human behind it — a Schedule's Firing and `apogee
// headless` — and each refuses it itself ("the surface that offers Auto is the one that refuses it",
// ADR 0033, decision 3), so the verdict lives here once and differs only in the noun it calls the
// run. A user who meets this refusal at one surface must not meet a weaker story at the other.
//
// It is the MIRROR of probe.DegradedNotice's gate, with the mode fixed: an interactive Auto on a
// host that cannot fence keeps working because every terminal command falls back to the Approval
// path, and that is precisely the rung an unattended run does not have — its Approver denies rather
// than asks (ADR 0033, decision 2), so auto there would be a plan run that fails loudly at every
// write. The unconfined case (`confine-to-workspace: false`) is NOT blocked: that is the user's own
// explicit "I am the sandbox", the same ladder that lets the session launch in Auto, and neither a
// schedule nor a headless run is held to a stricter bar than a launch (decision 3).
//
// Pure, so the wording is table-testable without a host.
func autoUnattendedBlocked(subject, backend string, caps apogee.ConfinementCaps, confineToWorkspace bool) string {
	if !confineToWorkspace || caps.AutoEligible() {
		return ""
	}
	return fmt.Sprintf(
		"the %s backend on this host reports no filesystem confinement, so auto falls back to "+
			"approval — and %s has nobody to ask", backend, subject)
}
