package main

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/heartbeat"
	"github.com/airiclenz/apogee/internal/notice"
	"github.com/airiclenz/apogee/internal/probe"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/reactions"
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
	// session is bound to. It is the whole configuration half of a Firing (firingBinding), read at
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

	// notifyHook is where a Reaction's trouble is told (ADR 0073 §8); wired to Bridge.NotifyHook, the
	// same seam this session's OWN Runner reports through, so a failing Reaction reads identically
	// whether the session fired it or a Firing this session raised did. It is a seam rather than a
	// direct call for the Bridge's reason: it is invoked from the Runner's worker goroutines, never
	// from Update. nil leaves a Reaction's failure silent, which is what a composition test wants.
	notifyHook func(string)

	// upstream is the footer's liveness verdict on the bound server, as the TUI last published it
	// through [tui.Options.ReportUpstream] (upstreamLatch, below) — the pair a Firing's beat carries
	// so raise can refuse one up front while the server is offline. It is the renderer's OWN
	// debounced verdict and never a probe of this side's: the fold rules that decide it (a failed
	// beat during a busy Exchange ignored, a `/server` switch resetting to cold) live in
	// internal/tui/heartbeat.go, and a re-derivation from raw beats here would latch offline
	// through a `/load` restart while the footer says online. nil, or a latch nothing has reported
	// to yet, is "no observation" — the Firing proceeds, exactly as every Firing did before the gate.
	upstream *upstreamLatch
}

// fire performs one Firing and reports the record it left behind. It is the value wired into
// schedule.Config.Fire, and it runs on the scheduler's goroutine — never the Update loop's — which
// is why everything it touches is either immutable, its own copy, or explicitly goroutine-safe (the
// settings holder, the store, the skills Provider, the Confiner, the upstream latch).
//
// The Firing is raised by [raise] (wire_firing.go), the one act every Driver's unattended run is —
// an unattended run is an unattended run whichever Driver raised it (ADR 0031). What THIS Driver
// decides is what a live session, rather than a file or a daemon's entry, decides: the settings the
// human has moved since launch, the server the session has switched to, the width its heartbeat
// already resolved, the skill catalogue it is sharing, and the footer's own verdict on whether that
// server is there at all.
//
// The delegates that assume a human are never handed over: run.Once pins its own fail-safe denier and
// leaves ask_user and present_document unregistered (ADR 0033, decision 2), and handing it the
// Bridge's would only invite a Firing to rendezvous with a UI that is not listening for it. Tools are
// left to the composer for the same class of reason: MCP connections are live host state
// re-established per session (ADR 0008, ADR 0022 §8), so a Firing takes the library's own registry
// rather than the session's MCP-augmented one, and reaches no external server at all.
func (w scheduleWiring) fire(ctx context.Context, f schedule.Firing) (schedule.Outcome, error) {
	binding := w.binding()
	opts, entry := w.live.firingBinding(binding)

	// The one act every unattended run is (raise, wire_firing.go), reached from this Driver's own
	// facts. No `model:` overlay is handed over: the model this session is bound to is already the
	// entry's own above, and naming it twice would be two routes to one value. The mode is the
	// Schedule's, chosen explicitly at creation and never inherited from the session's own (ADR
	// 0033, decision 3): Auto's eligibility was ruled on there, at the surface that offered it,
	// exactly as agent.New trusts a Config that says Auto. The Schedule this run belongs to travels
	// with it, so the Firing's own Reaction Runner stamps it onto every payload (ADR 0073) and its
	// record is filed as the Schedule's. No onID and no narrate: this Driver stamps the id on no
	// stream, and a Firing's narration is the session record it leaves behind — which is also why
	// the rebind notices are dropped: they are a launch's narration.
	//
	// A Reaction's trouble, from either lane, is told through the Bridge's NotifyHook — the same
	// seam this session's OWN Runner reports through (ADR 0073 §8).
	res, _, err := raise(ctx, firingInputs{
		opts:     opts,
		entry:    entry,
		apiKey:   binding.APIKey,
		keys:     w.keys,
		roots:    w.roots,
		confiner: w.confiner,
		mode:     f.Mode,
		skills:   w.skills,
		// The session's OWN observation of the server it is bound to, handed over as the seam the
		// composer would otherwise probe through, so no Firing raised here spends a round trip on
		// facts this session is already holding (design call 4). The endpoint, model and key it is
		// offered are this Firing's own and go unread: the answers are about the server the SESSION
		// is on, which is the same one.
		//
		// It carries the width this session resolves and the effort wire shape its own heartbeat
		// observed (ADR 0060) — the session and its Firing are on one server, so the dialect it saw
		// is the dialect this run must speak. The two liveness flags and the failure text are the
		// footer's verdict as the TUI last published it (upstream): a session whose footer says
		// online is talking to that server, which is a stronger observation than a fresh probe would
		// be, and one whose footer says offline has every reason raise needs to refuse the Firing
		// before a prompt is spent on it (the gate is Beat.Answered, wire_firing.go). No verdict yet
		// reads as online, so a session that has never heard from its monitor fires as it always did.
		beat: func(context.Context, string, string, string) heartbeat.Beat {
			offline, failure := w.upstream.verdict()
			return heartbeat.Beat{
				Reachable:     !offline,
				Answered:      !offline,
				Failure:       failure,
				TotalSlots:    w.width(),
				EffortSupport: provider.EffortSupport{Dialect: w.live.observedDialect()},
			}
		},
		report: w.notifyHook,
	}, f.Prompt, &reactions.ScheduleRef{ID: f.ScheduleID, Name: f.ScheduleName}, w.store, nil, nil)

	// A Firing refused before it began — a composition that would not produce a Config, or a footer
	// that says the server is offline — is this function's ERROR rather than an Outcome: the library
	// lands it as schedule.EventFailed and the transcript's Firing block renders the sentence. It
	// records no Outcome, because nothing was sent and there is nothing to report. The two stages
	// are told apart by raise's typed refusal (errNotStarted) rather than by the sentence, and
	// mapped by the one helper this Driver and the daemon share (firingRefusal, wire_firing.go): a
	// composition refusal is wrapped under this Driver's own clause, and the gate's refusal passes
	// BARE — its sentence is the one a send earns at the prompt (internal/tui/heartbeat.go's
	// upstreamBlockNote), and a human who has just seen the footer refuse a send reads the same
	// words from the Firing.
	var refused errNotStarted
	if errors.As(err, &refused) {
		return schedule.Outcome{}, firingRefusal("apogee: resolve the firing's", refused)
	}

	// Everything the run learned about itself, mapped onto the scheduler's report by the one
	// mapping every Driver's Firing shares (firingOutcome, wire_firing.go), so both ends of this
	// function tell the surface the same story. The library reads none of it — it is
	// runner-agnostic (ADR 0033) — and a Driver renders the Firing from these fields alone: the
	// answer without decoding a record, the stats without a second seam onto the run. The anomalies
	// it carries are what the run found WRONG with the workspace's context files — the transcript's
	// Firing block renders them, stripping at its own seam (the text crosses as plain data).
	out := firingOutcome(res)
	if err != nil {
		// A failed Firing still reports what it salvaged: run.Once fills its Result with whatever
		// it managed BEFORE it stopped, and a surface that has already announced this Firing can
		// point a human at the partial record rather than at nothing. The id ALSO stays in the
		// error text — that is what a Driver reading only the failure's wording has to go on.
		if res.SessionID != "" {
			return out, fmt.Errorf("%w %s", err, partialRunSuffix(res.SessionID))
		}
		return out, err
	}
	return out, nil
}

// firingSpend is what one Firing cost in tokens: the run's own cumulative total plus every
// delegated run's. It is the SAME sum /sessions shows as a session's spend
// (internal/tui/sessions.go:734, Meta.Usage + Meta.DelegateUsage), taken here because
// schedule.Outcome is flat — the library is runner-agnostic (ADR 0033) and never imports the
// runner's shapes to take it for itself. The one builder of an Outcome, the mapping every raised
// Firing goes through (firingOutcome, wire_firing.go), takes it from here, so a Firing's cost
// reads the same whichever Driver fired it.
func firingSpend(res run.Result) int {
	total := res.Usage.TotalTokens
	for _, sub := range res.SubAgents {
		total += sub.TotalTokens
	}
	return total
}

// contextAnomalies is what a Firing carries of its run's context files: the composed sentences
// notice.ContextFileNotices marks as ANOMALIES, in the composer's own order, and nothing else. The
// plain loaded-files line is dropped here exactly as it is on the daemon's journal
// (daemonfire.go) — one line per Firing naming every file that loaded as expected is noise on a
// surface a week's worth of ticks fills.
//
// Nothing is stripped: the sentences cross as plain data and the surface that renders them strips
// at its own seam. nil on a clean report, so a surface that ranges over it shows nothing.
func contextAnomalies(report domain.ContextFilesReport) []string {
	var out []string
	for _, n := range notice.ContextFileNotices(report) {
		if n.Anomaly {
			out = append(out, n.Text)
		}
	}
	return out
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

// upstreamLatch is the host half of [tui.Options.ReportUpstream], the twin of [idleGate] one seam
// over: the TUI publishes its footer's liveness verdict at each crossing, and a due Firing reads the
// latched pair here to hand raise the beat it refuses on. It holds the verdict rather than deriving
// one because the verdict IS the footer's — debounced over offlineFailureThreshold idle beats, deaf
// to a failure during a busy Exchange, reset to cold by a `/server` switch — and any re-derivation
// on this side from raw beats would disagree with what the human is looking at.
//
// It starts with NO observation, which reads as online: a session that has not heard from its
// monitor has nothing to refuse on, and a Firing proceeds exactly as every Firing did before the
// gate existed. The first crossing the TUI publishes is the first verdict held.
type upstreamLatch struct {
	mu      sync.Mutex
	offline bool
	failure string
}

// newUpstreamLatch builds a latch holding no observation.
func newUpstreamLatch() *upstreamLatch { return &upstreamLatch{} }

// report records the footer's verdict. It is the value wired into [tui.Options.ReportUpstream] and
// is called from the Update loop at each crossing; a repeat of the value already held is harmless.
func (l *upstreamLatch) report(offline bool, failure string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.offline, l.failure = offline, failure
}

// verdict reads the latched pair: offline, and the monitor's words for why when it had any. A nil
// latch — a wiring with no TUI publishing to it, every composition test's — is no observation and
// answers online, which is the same answer a latch nothing has reported to gives.
func (l *upstreamLatch) verdict() (offline bool, failure string) {
	if l == nil {
		return false, ""
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.offline, l.failure
}

// scheduleAutoBlocked is the reason a Schedule may not be created in auto mode on this host, or ""
// when it may — the value the renderer reads as [tui.Options.ScheduleAutoBlocked], where one string
// is both the verdict and its wording so the disabled picker row and the refused `auto` argument can
// never disagree. The unattended run it speaks of is a Firing, which is what a Schedule's auto run
// is; every other word of the verdict is [probe.AutoUnattendedBlocked]'s.
func scheduleAutoBlocked(backend string, caps apogee.ConfinementCaps, confineToWorkspace bool) string {
	return probe.AutoUnattendedBlocked("a firing", backend, caps, confineToWorkspace)
}
