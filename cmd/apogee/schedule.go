package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/probe"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/run"
	"github.com/airiclenz/apogee/internal/schedule"
	"github.com/airiclenz/apogee/internal/session"
	"github.com/airiclenz/apogee/internal/skills"
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

// tuiScheduleClock is the seam onto the interactive Scheduler's sense of time — the twin of
// [daemonClock] (daemon.go), one Driver over. nil, its production value, is the wall clock and real
// tickers (schedule.Config.Clock), which is what a surface whose shortest legal cycle is thirty
// seconds must run on. A test replaces it so a due tick happens now rather than in half a minute,
// which is the only way a driven run can watch a Firing land in the transcript inside a test's
// wall-clock budget. Production never reassigns it.
//
// It lives at this level rather than on [tui.Options] for the reason the scheduler itself does: the
// cycle is a Driver's concern (ADR 0033), and the engine gains no test-only hook from it (ADR 0062).
var tuiScheduleClock schedule.Clock

// scheduleWiring turns one Firing into one unattended run. It is the titleWiring split, made for the
// same reason: the endpoint, the key, the model and the roots are wiring the binary resolves and a
// `/server` switch moves, so the Config is composed PER FIRING from the session as it stands at that
// moment rather than captured when the Schedule was created. A Schedule made before a switch fires
// against the server the session is on now, under the settings the session is running now.
type scheduleWiring struct {
	// live is this session's settings holder — the launch snapshot with every `/settings` commit and
	// every config-watcher reload written back over it, plus the pins of the `servers:` entry the
	// session is bound to. It is the whole configuration half of a Firing (firingSources), read at
	// FIRING time rather than captured, and safely so: the holder is goroutine-safe and this runs on
	// the Scheduler's own goroutine. Composing from it is what carries ADR 0037's promise into the
	// runs a session raises — a Firing budgets, fences and arms itself from the configuration the
	// human is looking at, not the one they launched with.
	live  *liveSettings
	roots stateRoots

	// binding reads the CURRENT Upstream binding; wired to upstreamHolder.Binding, the same seam the
	// naming call reads for the same reason. It decides BOTH halves of a Firing's upstream — the wire
	// it dials and the endpoint its spec resolution keys on — because a `/server` switch moves both.
	binding func() upstreamBinding

	// width reports the Parallel agents cap the bound server resolves to right now; wired to
	// parallelAgentsCap.current, which already owns that number for the interactive session. It is a
	// seam read at FIRING time for the binding's own reason — a `/server` switch moves the width with
	// the server — and it is why this Driver hands the composer a width source of its own: an
	// unattended run with no session behind it probes the server instead, and a Firing that did the
	// same would spend its latency re-asking a question this session already has the answer to
	// (ADR 0039; ADR 0031, every Driver reaching the same engine behaviour).
	width func() int

	// keys is the session's own key resolver — shared rather than fresh so an `api-key-cmd:` is not
	// re-run per Firing. It answers only when the binding carries no key of its own, which is the
	// pre-bind case; a bound session hands its resolved key straight over.
	keys *config.KeyResolver

	// skills is the session's LIVE skill catalogue, shared rather than rebuilt so a
	// `use-project-skills` flip or a `/skills` reload keeps following the runs this session raises.
	skills *skills.Provider

	// confiner is the session's own OS confinement backend, so an Auto Firing sits in the same box an
	// Auto session on this host would. Whether this host may run Auto unattended at all was ruled on
	// at the surface that offered the mode (scheduleAutoBlocked, ADR 0033 decision 3).
	confiner apogee.Confiner

	// store is the session store a Firing's record lands in — the interactive session's own, so a
	// Firing shows up in /sessions beside the conversations it ran beneath (items 2 and 7).
	store *session.Store
}

// fire performs one Firing and reports the record it left behind. It is the value wired into
// schedule.Config.Fire, and it runs on the scheduler's goroutine — never the Update loop's — which
// is why everything it touches is either immutable, its own copy, or explicitly goroutine-safe (the
// settings holder, the store, the skills Provider, the Confiner).
//
// The Config it runs against is composed by [firingConfig] (wire_firing.go), the one composer every
// Driver's unattended run is built by — an unattended run is an unattended run whichever Driver
// raised it (ADR 0031). What THIS Driver decides is what a live session, rather than a file or a
// daemon's entry, decides: the settings the human has moved since launch, the server the session has
// switched to, the width its heartbeat already resolved, and the skill catalogue it is sharing.
//
// The delegates that assume a human are never handed over: run.Once pins its own fail-safe denier and
// leaves ask_user and present_document unregistered (ADR 0033, decision 2), and handing it the
// Bridge's would only invite a Firing to rendezvous with a UI that is not listening for it. Tools are
// left to the composer for the same class of reason: MCP connections are live host state
// re-established per session (ADR 0008, ADR 0022 §8), so a Firing takes the library's own registry
// rather than the session's MCP-augmented one, and reaches no external server at all.
func (w scheduleWiring) fire(ctx context.Context, f schedule.Firing) (schedule.Outcome, error) {
	binding := w.binding()
	opts, entry, manualIDs := w.live.firingSources(binding)

	// This Firing's own record id, minted here because the runner is handed it beside the Config
	// (run.Spec) and the composer creates the run's scratch dir under that name. Minted per FIRING
	// rather than inherited: the session's own dir was named when this session booted, so a Firing
	// that took it would write into a dir a /clear or a /sessions resume has since moved the session
	// off — or, once the 14-day sweep has been past it, into one that is gone.
	recordID := session.NewID(time.Now())

	// The construction surface every unattended run shares (wire_firing.go), reached from this
	// Driver's own facts. No `model:` overlay is handed over: the model this session is bound to is
	// already the entry's own above, and naming it twice would be two routes to one value. The mode
	// is the Schedule's, chosen explicitly at creation and never inherited from the session's own
	// (ADR 0033, decision 3): Auto's eligibility was ruled on there, at the surface that offered it,
	// exactly as agent.New trusts a Config that says Auto.
	//
	// The rebind notices are dropped — they are a launch's narration, and a Firing's narration is the
	// session record it leaves behind.
	cfg, routing, _, err := firingConfig(ctx, firingInputs{
		opts:      opts,
		entry:     entry,
		apiKey:    binding.APIKey,
		keys:      w.keys,
		roots:     w.roots,
		manualIDs: manualIDs,
		confiner:  w.confiner,
		mode:      f.Mode,
		skills:    w.skills,
		// The width the session's bound server already resolves to, handed over as the seam the
		// composer probes through, so no Firing spends a round trip on a number this session is
		// holding (design call 4). The endpoint, model and key it is offered are this Firing's own
		// and go unread: the answer is about the server the SESSION is on, which is the same one.
		width: func(context.Context, string, string, string) int { return w.width() },
		// And the effort wire shape it already observes on its own beat, handed over for the same
		// reason and read the same way: the session and its Firing are on one server, so the
		// dialect it saw is the dialect this run must speak (ADR 0060).
		dialect: func(context.Context, string, string, string) provider.EffortDialect {
			return w.live.observedDialect()
		},
		recordID: recordID,
	})
	if err != nil {
		return schedule.Outcome{}, fmt.Errorf("apogee: resolve the firing's bindings: %w", err)
	}

	// Through the package's runner seam (headless.go) rather than run.Once directly: production never
	// reassigns it, so this is the same call, and it is what lets a test read the Config a Firing
	// composed — the width above being the whole point of one.
	res, err := runOnce(ctx, run.Spec{
		Config:       cfg,
		Prompt:       f.Prompt,
		ScheduleID:   f.ScheduleID,
		ScheduleName: f.ScheduleName,
		Store:        w.store,
		RecordID:     recordID,
		// The routing the composer resolved off the LIVE Options above, latched through run.Spec's
		// seam (internal/run): a Firing raised in this session delegates to the entry the session
		// delegates to, including one a `/sub-agents-server` pick moved it to since launch
		// (liveSettings.subAgentsServer). Both fields are nil when no key named one.
		DelegationTarget: routing.target,
		DelegationSeat:   routing.seat,
	})
	// Everything the run learned about itself, mapped onto the scheduler's report in one place so
	// both ends of this function tell the surface the same story. The library reads none of it — it
	// is runner-agnostic (ADR 0033) — and a Driver renders the Firing from these fields alone: the
	// answer without decoding a record, the stats without a second seam onto the run.
	out := schedule.Outcome{
		RecordID:    res.SessionID,
		Title:       res.Title,
		FinalText:   res.FinalText,
		Turns:       res.Turns,
		Denied:      res.Denied,
		Faulted:     res.Faulted,
		Fault:       res.Fault,
		TotalTokens: firingSpend(res),
		SubAgents:   len(res.SubAgents),
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

// firingSpend is what one Firing cost in tokens: the run's own cumulative total plus every
// delegated run's. It is the SAME sum /sessions shows as a session's spend
// (internal/tui/sessions.go:734, Meta.Usage + Meta.DelegateUsage), taken here because
// schedule.Outcome is flat — the library is runner-agnostic (ADR 0033) and never imports the
// runner's shapes to take it for itself. Both builders of an Outcome go through it, this
// package's and the daemon's (daemonfire.go), so a Firing's cost reads the same whichever
// Driver fired it.
func firingSpend(res run.Result) int {
	total := res.Usage.TotalTokens
	for _, sub := range res.SubAgents {
		total += sub.TotalTokens
	}
	return total
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
// is; every other word of the verdict is [probe.AutoUnattendedBlocked]'s.
func scheduleAutoBlocked(backend string, caps apogee.ConfinementCaps, confineToWorkspace bool) string {
	return probe.AutoUnattendedBlocked("a firing", backend, caps, confineToWorkspace)
}
