package main

// Server fallback, the Driver's half (ADR 0105): the resolver the engine consults before it abandons
// a Turn for a fault that is the upstream's own (apogee.FallbackResolver), built here from a
// `servers:` entry's `fallback:` list and the resolved configuration, so every Driver that binds an
// entry reaches the same behaviour from the same keys (ADR 0031).
//
// The engine reads no config: it reports the fault and takes an answer. Everything a fallover needs
// to know about servers lives here — how long to wait for the failed one (`fallback-after:`), which
// servers stand in for it and in what order, the key each one takes, and the whole binding a move
// lands on (the UpstreamSpec a `/server` move would hand the engine, and the RebindSpec the first
// beat on that server would bind). The answer is built from one beat on the fallback, the shape a
// routed delegation's target is resolved in (resolveDelegationTarget).
//
// The state of one seat's fallover lives in ONE fallbackResolver instance, shared by every Agent on
// that seat (item 6 guard (a) of the midrun-model-fallback plan): the single-flight that makes N
// agents faulting on one failed upstream cost one grace window and one list walk, and the list
// position that keeps a seat which has fallen over moving forward along the list, never back. The
// engine's sharedUpstream holds only the generation that tells a retired request from a live one.

import (
	"context"
	"sync"
	"time"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/heartbeat"
	"github.com/airiclenz/apogee/internal/provider"
)

// fallbackGracePoll is how often the grace window asks the failed server whether it is back. A beat
// is a discovery round trip, bounded by the provider's discovery timeout, so a poll this far apart
// costs a recovering server at most this long before the resend, and a `fallback-after:` shorter
// than it is simply waited out to its end.
const fallbackGracePoll = 5 * time.Second

// fallbackBeatAllowance is the room the shared run grants each beat of the list walk, on top of the
// grace window, before its backstop ends it. It is generous on purpose — a slow box makes a
// fallover slower, never broken — and it only bounds a beat that the discovery timeout has not.
const fallbackBeatAllowance = 2 * time.Minute

// fallbackResolver is one seat's Server fallback: the apogee.FallbackResolver every Agent on the
// seat consults. It answers a fault on the seat's primary by waiting up to the grace window for the
// primary to answer a beat again (recovered — resend on it), and otherwise by walking the primary's
// `fallback:` list in order, one beat each, to the first server that answers (switch). A fault on a
// fallback walks the rest of the list with no grace, and a fallback's own `fallback:` is never read:
// there is one list per seat, and no chaining.
//
// recovered and land are the two network acts, held as funcs so the single-flight and the list walk
// are provable without a server; newSeatResolver wires the real ones.
type fallbackResolver struct {
	// primary is the name of the entry the seat started on — the only server a grace window is
	// spent on.
	primary string
	// names is the primary's `fallback:` list, in order.
	names []string
	// grace is the primary's `fallback-after:` window, resolved.
	grace time.Duration
	// poll is how often the grace window beats the primary (fallbackGracePoll).
	poll time.Duration
	// recovered reports whether the primary answers a beat with a usable model list.
	recovered func(ctx context.Context) bool
	// land beats the fallback named and, when it answers, resolves the whole binding a move onto it
	// takes; false when it is down or cannot be bound.
	land func(ctx context.Context, name string) (apogee.FallbackAnswer, bool)

	mu sync.Mutex
	// next is the position in names the next walk starts from: past every server the seat has
	// already moved to, so a seat that fell over never walks back.
	next int
	// fallen reports that the seat has moved off its primary: from then on no fault earns a grace
	// window, because the server that failed is not the one the window waits for.
	fallen bool
	// runs holds one run per failed server — the single-flight. A run that moved the seat stays, so
	// a late report on the server it moved off reads the same answer; a run that did not move it is
	// dropped once answered, so a later failure of the same server is a fresh one.
	runs map[string]*fallbackRun
}

// fallbackRun is one grace-and-walk for one failed server: done closes once answer is final.
type fallbackRun struct {
	done   chan struct{}
	answer apogee.FallbackAnswer
}

// newFallbackResolver builds the resolver for a seat on primary whose `fallback:` list is names.
func newFallbackResolver(
	primary string,
	names []string,
	grace time.Duration,
	recovered func(ctx context.Context) bool,
	land func(ctx context.Context, name string) (apogee.FallbackAnswer, bool),
) *fallbackResolver {
	return &fallbackResolver{
		primary:   primary,
		names:     names,
		grace:     grace,
		poll:      fallbackGracePoll,
		recovered: recovered,
		land:      land,
		runs:      make(map[string]*fallbackRun),
	}
}

// Resolve answers one report. The first report on a failed server starts the run; every report on
// that server — concurrent, or later once the run moved the seat — waits for and shares the same
// answer; the engine, not the resolver, announces the move once (agent.FallbackResolver). The run
// itself is detached from the caller that started it, so that caller's cancel never fails
// the others; each caller's own wait still ends at once when its ctx does, with the zero answer (the
// engine reads a cancelled ctx as the user's cancel whatever comes back).
func (r *fallbackResolver) Resolve(ctx context.Context, report apogee.FallbackReport) apogee.FallbackAnswer {
	r.mu.Lock()
	run, joined := r.runs[report.Server]
	if !joined {
		run = &fallbackRun{done: make(chan struct{})}
		r.runs[report.Server] = run
		go r.execute(ctx, report, run)
	}
	r.mu.Unlock()

	answer, _ := run.wait(ctx)
	return answer
}

// JoinLanding is the engine's post-cancel join (apogee.FallbackLandingJoiner): it waits for the run
// still held for report's failed server and answers with it, and starts nothing. With no run held —
// one that ended without moving the seat was dropped — it answers the zero answer and false at once,
// so a join after the last caller's cancel never begins a fresh grace window or walk. A run that
// moved the seat is never dropped, so a join after it landed reads its switch.
func (r *fallbackResolver) JoinLanding(ctx context.Context, report apogee.FallbackReport) (apogee.FallbackAnswer, bool) {
	r.mu.Lock()
	run, inFlight := r.runs[report.Server]
	r.mu.Unlock()
	if !inFlight {
		return apogee.FallbackAnswer{}, false
	}
	return run.wait(ctx)
}

// wait blocks until the run's answer is final and returns it with true, or returns the zero answer
// and false once ctx ends first.
func (run *fallbackRun) wait(ctx context.Context) (apogee.FallbackAnswer, bool) {
	select {
	case <-run.done:
		return run.answer, true
	case <-ctx.Done():
		return apogee.FallbackAnswer{}, false
	}
}

// execute runs the grace window and the list walk for report, on a context that keeps the starting
// caller's values but not its cancel, under a backstop sized to the window and the walk.
func (r *fallbackResolver) execute(caller context.Context, report apogee.FallbackReport, run *fallbackRun) {
	backstop := r.grace + time.Duration(len(r.names)+1)*fallbackBeatAllowance
	ctx, cancel := context.WithTimeout(context.WithoutCancel(caller), backstop)
	defer cancel()

	answer := r.answer(ctx, report)

	r.mu.Lock()
	run.answer = answer
	if answer.Verdict != apogee.FallbackSwitch && r.runs[report.Server] == run {
		delete(r.runs, report.Server)
	}
	r.mu.Unlock()
	close(run.done)
}

// answer is the run's whole decision. A fault on the primary of a seat that has not fallen over
// waits out the grace window first — unless the Turn already spent its one recovered resend, when a
// second wait could only loop a flapping primary — and a primary that answers inside it is
// recovered. Past the window, or with none, the list walk takes the first fallback that answers.
func (r *fallbackResolver) answer(ctx context.Context, report apogee.FallbackReport) apogee.FallbackAnswer {
	r.mu.Lock()
	isGraced := !r.fallen && report.Server == r.primary && !report.RecoveredSpent
	r.mu.Unlock()

	var waited time.Duration
	if isGraced {
		recovered, spent := r.awaitPrimary(ctx)
		if recovered {
			return apogee.FallbackAnswer{Verdict: apogee.FallbackRecovered, Waited: spent}
		}
		waited = spent
	}
	return r.walk(ctx, waited)
}

// awaitPrimary beats the primary until it answers or the grace window has run, and reports whether
// it answered and how long the wait took. The first beat goes out at once: a server that is already
// back costs the Turn no wait at all.
func (r *fallbackResolver) awaitPrimary(ctx context.Context) (bool, time.Duration) {
	start := time.Now()
	for {
		if r.recovered(ctx) {
			return true, time.Since(start)
		}
		remaining := r.grace - time.Since(start)
		if remaining <= 0 || ctx.Err() != nil {
			return false, time.Since(start)
		}
		pause := time.NewTimer(min(r.poll, remaining))
		select {
		case <-pause.C:
		case <-ctx.Done():
			pause.Stop()
			return false, time.Since(start)
		}
	}
}

// walk tries the list from the seat's current position, one beat each, and moves the position past
// the server it lands on. A walk that lands nowhere leaves the position where it was: those servers
// may answer the next time the seat fails.
func (r *fallbackResolver) walk(ctx context.Context, waited time.Duration) apogee.FallbackAnswer {
	r.mu.Lock()
	from := r.next
	r.mu.Unlock()

	for i := from; i < len(r.names) && ctx.Err() == nil; i++ {
		answer, landed := r.land(ctx, r.names[i])
		if !landed {
			continue
		}
		r.mu.Lock()
		r.next = i + 1
		r.fallen = true
		r.mu.Unlock()
		answer.Verdict = apogee.FallbackSwitch
		answer.Waited = waited
		return answer
	}
	return apogee.FallbackAnswer{Waited: waited}
}

// fallbackLander resolves a move onto one fallback entry, from the resolved configuration a Driver
// holds: the entry, its key, one beat, and the binding that beat makes true.
type fallbackLander struct {
	// opts is the resolved configuration: the `servers:` list the names resolve against, the
	// top-level pins an entry's own pins rank over on the session seat (pins), and the per-model
	// keys the rebind reads.
	opts config.Options
	// roots are the state roots the per-model prompt files resolve against.
	roots stateRoots
	// key resolves the entry's key source into the token the fallback is sent.
	key func(entry config.ServerEntry) (string, error)
	// beat observes the fallback once, through the Monitor its dial facts build.
	beat func(ctx context.Context, dial upstreamBinding) heartbeat.Beat
	// routed says the move is the Sub-agents seat's: the landing binds as resolveDelegationTarget
	// binds a routed target on the same entry, never under the session's root window pins (pins).
	routed bool
	// parent is what a routed child keeps from its parent where the fallback entry states nothing;
	// read only when routed.
	parent delegationParent
}

// delegationParent is what a routed child keeps from its parent where its Delegation target states
// nothing (DelegationTarget.binding): the window the parent runs on — the last rank of a target's
// window, after the entry's pin and the observed window — and the response-reserve share the parent
// resolved, which an entry stating no share of its own leaves standing.
type delegationParent struct {
	window  int
	reserve float64
}

// fallbackLanding is one move onto a fallback entry, resolved whole: the answer the engine is
// handed, and what a Driver that observes the session's server needs to follow the move once the
// engine has made it — the entry, the dial binding its Monitor is built from, and the beat that
// found it answering (sessionMover.fallover).
type fallbackLanding struct {
	answer   apogee.FallbackAnswer
	entry    config.ServerEntry
	dial     upstreamBinding
	observed heartbeat.Beat
}

// land beats the fallback named and, when it answers with a model to bind, resolves the answer a
// FallbackSwitch carries (arrive, projected onto its answer).
func (l fallbackLander) land(ctx context.Context, name string) (apogee.FallbackAnswer, bool) {
	landing, ok := l.arrive(ctx, name)
	return landing.answer, ok
}

// arrive beats the fallback named and, when it answers with a model to bind, resolves the whole
// landing: the UpstreamSpec exactly as a `/server` move onto that entry would state it
// (entryUpstream), the RebindSpec the beat's model binds there, and the dial binding and beat behind
// them. A name no entry carries, a key source that refuses, a server that does not answer or names
// no model, and a per-model resolution that fails each leave the seat where it is (false) and the
// walk moves on.
func (l fallbackLander) arrive(ctx context.Context, name string) (fallbackLanding, bool) {
	entry, err := findServer(l.opts.Servers, name)
	if err != nil {
		return fallbackLanding{}, false
	}
	apiKey, err := l.key(entry)
	if err != nil {
		return fallbackLanding{}, false
	}
	observed := l.beat(ctx, bindingOfEntry(entry, apiKey))
	if !observed.Reachable {
		return fallbackLanding{}, false
	}
	model := entry.Model
	if model == "" {
		model = observed.ActiveModel
	}
	if model == "" {
		return fallbackLanding{}, false
	}

	pins := l.pins()
	upstream, dial := entryUpstream(entry, apiKey, pins)
	rebind, err := l.rebind(entry, apiKey, model, observed, pins)
	if err != nil {
		return fallbackLanding{}, false
	}
	dial.Model = model
	return fallbackLanding{
		answer:   apogee.FallbackAnswer{Upstream: upstream, Rebind: rebind, Server: entry.Name},
		entry:    entry,
		dial:     dial,
		observed: observed,
	}, true
}

// rebind resolves the per-model binding for model on entry, the way an unattended run's bind
// resolves its own (bindFiringConfig): the configuration overlaid with the entry's endpoint, key,
// and its window pin and reserve share ranked over pins, the observed window bound where no pin
// stands (observedWindow), the entry's forced effort dialect over the one the beat ranked, and the
// entry's price.
func (l fallbackLander) rebind(
	entry config.ServerEntry,
	apiKey, model string,
	observed heartbeat.Beat,
	pins rootPins,
) (apogee.RebindSpec, error) {
	specOpts := l.opts
	specOpts.Endpoint = entry.Endpoint
	specOpts.APIKey = apiKey
	pinnedWindow := config.ResolveContextWindow(int(entry.ContextWindow), pins.contextWindow)
	specOpts.ContextWindow = pinnedWindow
	specOpts.ResponseReserve = config.ResolveResponseReserve(entry.ResponseReserve, pins.responseReserve)
	spec, _, err := rebindSpecFor(specOpts, l.roots, model, l.observedWindow(observed), pinnedWindow,
		int(entry.MaxOutputTokens))
	if err != nil {
		return apogee.RebindSpec{}, err
	}
	dialect := provider.EffortDialectFor(entry.EffortDialect)
	if dialect == provider.EffortDialectNone {
		dialect = providerEffortDialect(observed.EffortSupport.Dialect)
	}
	spec.EffortDialect = dialect
	price := priceOfEntry(entry.Price)
	spec.Price = &price
	return spec, nil
}

// rootPins are the top-level keys a server entry's own pins rank over when a seat arrives on it:
// the `context-window:`, `working-window:` and `response-reserve:` pins and the root
// `stream-idle-timeout:`. A session reads them off its live settings at move time; an unattended run
// off the Options it resolved (rootPinsOf).
type rootPins struct {
	contextWindow   int
	workingWindow   int
	responseReserve float64
	streamIdle      time.Duration
}

// pins are the keys the fallback entry's own pins rank over at this landing. The session seat's are
// the root pins (rootPinsOf). A routed move's are the ranks resolveDelegationTarget binds a routed
// target by: no root `context-window:` or `working-window:` — those describe the session's own
// server, never the Sub-agent server's box — the parent's reserve share, and the root
// `stream-idle-timeout:`, which is the run's default silence bound rather than a statement about any
// one server.
func (l fallbackLander) pins() rootPins {
	root := rootPinsOf(l.opts)
	if !l.routed {
		return root
	}
	return rootPins{responseReserve: l.parent.reserve, streamIdle: root.streamIdle}
}

// observedWindow is the window a landing binds where the entry pins none: the one the beat observed,
// or — on a routed move whose beat named none — the parent's, as a routed spawn keeps it. A routed
// rehome never states 0 there: the engine applies a rebind's window as present, and 0 would leave the
// child windowless where a spawn on the same fallback runs in the parent's window.
func (l fallbackLander) observedWindow(observed heartbeat.Beat) int {
	if observed.ContextWindow > 0 || !l.routed {
		return observed.ContextWindow
	}
	return l.parent.window
}

// rootPinsOf is the top-level pins an Options carries.
func rootPinsOf(opts config.Options) rootPins {
	return rootPins{
		contextWindow:   opts.ContextWindow,
		workingWindow:   opts.WorkingWindow,
		responseReserve: opts.ResponseReserve,
		streamIdle:      opts.StreamIdleTimeout,
	}
}

// entryUpstream is the switch onto entry the engine is handed, dialled with apiKey, and the dial
// binding it was projected from: the one resolution a `/server` move (sessionMover.move) and a
// Server fallback share, so the two arrivals cannot state two different servers. Each of the
// entry's own pins ranks over the root key in root — the window, the working window, the reserve
// share and the stream-idle window (config.Resolve*) — and the idle window is always STATED, so the
// retired server's never keeps cutting this one's streams. The server rides it in the human's words
// (its name and description, ADR 0069), with whether it is the one-run `--endpoint` row; the reply
// cap is the entry's as written, 0 handing the engine its own derived cap (ADR 0046). No model
// rides it: the caller binds one.
func entryUpstream(entry config.ServerEntry, apiKey string, root rootPins) (apogee.UpstreamSpec, upstreamBinding) {
	dial := bindingOfEntry(entry, apiKey)
	dial.StreamIdleTimeout = config.ResolveStreamIdleTimeout(entry.StreamIdleTimeout, root.streamIdle)
	spec := dial.upstreamSpec()
	spec.ServerName = entry.Name
	spec.ServerDescription = entry.Description
	spec.ServerEphemeral = entry.Ephemeral
	spec.MaxContextTokens = config.ResolveContextWindow(int(entry.ContextWindow), root.contextWindow)
	spec.WorkingWindow = config.ResolveWorkingWindow(int(entry.WorkingWindow), root.workingWindow)
	spec.MaxOutputTokens = int(entry.MaxOutputTokens)
	spec.ResponseReserveFraction = config.ResolveResponseReserve(entry.ResponseReserve, root.responseReserve)
	return spec, dial
}

// seatKind names which of an Agent's two seats a resolver answers for: the session seat, whose
// requests go to the session's own server, or the Sub-agents seat, whose routed children go to the
// `sub-agents-server:` entry. It decides how a landing binds — a Sub-agents seat's landing is
// always routed (fallbackLander.routed) — and nothing else.
type seatKind int

const (
	// sessionSeat lands under the root window pins (fallbackLander.pins).
	sessionSeat seatKind = iota
	// subAgentsSeat lands as resolveDelegationTarget binds a routed target on the same entry.
	subAgentsSeat
)

// seatSource is what one seat's resolver reads from the Driver that builds it: everything that
// differs between a TUI session (read live, at the move) and an unattended Firing (fixed for the
// run). newSeatResolver builds every seat's resolver from one, so the two seats on the two Drivers
// cannot drift apart in how a fallover is walked or bound.
type seatSource struct {
	// primary is the entry the seat starts on: its name, its `fallback:` list and its
	// `fallback-after:` window are the resolver's.
	primary config.ServerEntry
	// recovered beats the primary once and reports whether it answers with a usable model list.
	recovered func(ctx context.Context) bool
	// lander resolves one move onto a fallback against the configuration as the source holds it at
	// the move: the options, the roots, the key source, the beat, and — for the Sub-agents seat —
	// what a routed child keeps from its parent (fallbackLander.parent).
	lander func() fallbackLander
	// landed is the Driver's landing hook, called with the seat's resolver after each landing and
	// before its answer reaches the engine; nil when the Driver follows nothing. On the Sub-agents
	// seat it answers the Delegation target the landing makes of the seat — resolved from the
	// landing's beat as resolveDelegationTarget resolves any target, nil for none — which the
	// resolver hands the engine as FallbackAnswer.Target, so the engine re-latches the tree (ADR
	// 0105). A session seat's hook answers nil.
	landed func(seat *seatResolver, landing fallbackLanding) *apogee.DelegationTarget
	// width is the session server's fan-out width a session-seat landing makes true, ranked as the
	// Driver ranked its primary's, which the resolver hands the engine as
	// FallbackAnswer.ParallelAgents so the engine states it on the root. nil answers 0 — the width
	// left alone — which is the TUI's: its cap follows the entry the session arrived on itself
	// (sessionMover.arrive, ADR 0039 decision 2). Never set on the Sub-agents seat, whose width rides
	// the landing's Delegation target.
	width func(landing fallbackLanding) int
}

// seatResolver is one seat's Server fallback (fallbackResolver) together with the latest landing it
// made: the apogee.FallbackResolver every Agent on the seat consults, which also tells a Driver
// what it last moved the seat onto (latestLanding).
type seatResolver struct {
	*fallbackResolver
	kind seatKind
	src  seatSource

	latestMu sync.Mutex
	// latest is the last landing the walk made; hasLanded is false until the first one.
	latest    fallbackLanding
	hasLanded bool
}

// newSeatResolver builds the resolver for one seat of kind from src, or nil when the seat's primary
// names no `fallback:` — the seat is then exactly what it was before the key existed. It is the one
// constructor for both seats on every Driver.
func newSeatResolver(kind seatKind, src seatSource) *seatResolver {
	names := src.primary.Fallback.Names()
	if len(names) == 0 {
		return nil
	}
	seat := &seatResolver{kind: kind, src: src}
	seat.fallbackResolver = newFallbackResolver(src.primary.Name, names,
		config.ResolveFallbackAfter(src.primary.FallbackAfter), src.recovered, seat.land)
	return seat
}

// land is the resolver's landing act: the source's lander, turned routed on the Sub-agents seat,
// beats the fallback named and resolves the whole landing; the landing is handed to the Driver's
// hook, and on the Sub-agents seat the target the hook answers rides the answer
// (FallbackAnswer.Target) carrying this resolver as its Fallback, so every target the seat's
// fallover latches consults the one instance; the source's width, when it states one, rides it as
// FallbackAnswer.ParallelAgents. The landing, Target included, is recorded as the
// latest before its answer goes back to the walk — where it commits for every caller of the run,
// including the engine's join after a cancelled one (agent.FallbackLandingJoiner).
func (s *seatResolver) land(ctx context.Context, name string) (apogee.FallbackAnswer, bool) {
	lander := s.src.lander()
	if s.kind == subAgentsSeat {
		lander.routed = true
	}
	landing, landed := lander.arrive(ctx, name)
	if !landed {
		return landing.answer, false
	}
	if s.src.landed != nil {
		target := s.src.landed(s, landing)
		if s.kind == subAgentsSeat && target != nil {
			target.Fallback = s
			landing.answer.Target = target
		}
	}
	if s.src.width != nil {
		landing.answer.ParallelAgents = s.src.width(landing)
	}
	s.latestMu.Lock()
	s.latest, s.hasLanded = landing, true
	s.latestMu.Unlock()
	return landing.answer, true
}

// latestLanding is the last landing the seat's walk made, false until it has made one.
func (s *seatResolver) latestLanding() (fallbackLanding, bool) {
	s.latestMu.Lock()
	defer s.latestMu.Unlock()
	return s.latest, s.hasLanded
}

// firingLander is an unattended run's lander: the Firing's own Options and roots, fixed for the run
// — it has no door to move them — each fallback's key resolved through the run's one resolver,
// fenced to the Firing's own workspace as the bound entry's was (bindFiringConfig), and parent as
// what a routed child keeps from the run (read only on the Sub-agents seat).
func firingLander(in firingInputs, keys *config.KeyResolver, parent delegationParent) func() fallbackLander {
	lander := fallbackLander{
		opts:  in.opts,
		roots: in.roots,
		key: func(entry config.ServerEntry) (string, error) {
			return keys.ResolveWithin(entry, in.roots.workspace)
		},
		beat: func(ctx context.Context, dial upstreamBinding) heartbeat.Beat {
			return dial.Monitor().Beat(ctx)
		},
		parent: parent,
	}
	return func() fallbackLander { return lander }
}

// firingSessionSource is an unattended run's session seat source. The primary's grace beats go
// through one Monitor dialled as the run's own beat is (the entry's dial facts, the key resolved
// for it, the model the run bound as the discovery hint); nothing follows a landing, the engine's
// switch being the whole move for a run with no display. A landing's answer carries the fallback's
// fan-out width, ranked off the beat that found it answering as firingConfig ranked the primary's
// (firingWidth), so the engine re-states the run's width on the move: a Firing has no cap that
// follows the entry, as a session's does.
func firingSessionSource(in firingInputs, keys *config.KeyResolver, apiKey, model string) seatSource {
	primary := bindingOfEntry(in.entry, apiKey)
	primary.Model = model
	monitor := primary.Monitor()
	return seatSource{
		primary:   in.entry,
		recovered: func(ctx context.Context) bool { return monitor.Beat(ctx).Reachable },
		lander:    firingLander(in, keys, delegationParent{}),
		width: func(landing fallbackLanding) int {
			return firingWidth(landing.entry, landing.observed.TotalSlots)
		},
	}
}

// sessionFallback is the TUI session seat's Server fallback: the ONE apogee.FallbackResolver the
// session's Agent — and through it every unrouted child — consults for the life of the session,
// handed in at construction (lateEngine.seedOptions), the pre-bound path's first pick included.
//
// It answers for the entry the session is bound to AT FAULT TIME (liveSettings.boundEntry), because
// a `/server` move can put the session on another entry long after the Agent was built: the first
// fault builds the per-primary seat resolver (newSeatResolver) for that entry, which then owns the
// seat's single-flight and fallen-over state, and keeps answering after the seat has moved onto one of its
// fallbacks — one list per seat, walked forward, never back. A human move retires it (rearm), so the
// next fault is a fresh one on whatever entry the human moved to; an entry with no `fallback:` gets
// no resolver at all and every fault on it is answered "none" at once, today's behaviour.
//
// It also holds the last landing its resolver made, so the TUI's follow of a committed switch
// (sessionMover.fallover) builds the Monitor, the window and the pins from the very facts the
// engine was handed rather than resolving the fallback a second time.
type sessionFallback struct {
	// bound reports the entry the session is on now (liveSettings.boundEntry).
	bound func() (config.ServerEntry, bool)
	// binding reports the session's dial binding as it stands (upstreamHolder.Binding): the
	// primary's grace beats go through a Monitor dialled from it at the first fault.
	binding func() upstreamBinding
	// lander resolves one move onto a fallback against the configuration as it stands at the move.
	lander func() fallbackLander
	// parent reports what a routed child keeps from the session at the move
	// (liveSettings.delegationParent), for the Sub-agents seat's lander (routedLander).
	parent func() delegationParent

	mu sync.Mutex
	// current is the resolver for the seat's present primary, nil until the first fault after a
	// bind or a human move.
	current *seatResolver
	// landed is the last fallback the current resolver moved the seat onto, by entry name.
	landed map[string]fallbackLanding
}

// Resolve answers one report through the current primary's resolver, building it on the first
// fault after a bind or a human move. A session on no configured entry, or on one with no
// `fallback:`, is answered with the zero answer — the Turn is abandoned exactly as without a
// resolver.
func (f *sessionFallback) Resolve(ctx context.Context, report apogee.FallbackReport) apogee.FallbackAnswer {
	resolver := f.resolver()
	if resolver == nil {
		return apogee.FallbackAnswer{}
	}
	return resolver.Resolve(ctx, report)
}

// JoinLanding joins the current primary's run still in flight for report
// (apogee.FallbackLandingJoiner). It never builds a resolver: with none current — no fault since a
// bind or a human move, or an entry with no `fallback:` — there is no run to join, and it answers
// the zero answer and false, dialling no Monitor.
func (f *sessionFallback) JoinLanding(ctx context.Context, report apogee.FallbackReport) (apogee.FallbackAnswer, bool) {
	f.mu.Lock()
	current := f.current
	f.mu.Unlock()
	if current == nil {
		return apogee.FallbackAnswer{}, false
	}
	return current.JoinLanding(ctx, report)
}

// resolver is the current primary's resolver, built for the bound entry when there is none. Each
// landing reads the configuration as it stands at the walk (f.lander, called per move), not as it
// stood when the resolver was built: a `/settings` or `servers:` edit made after the first fault
// reaches the fallback entry, its root pins and its per-model facts — as the Sub-agents seat's
// resolver does (delegationWiring.rearmLocked). A bound entry with no `fallback:` is answered nil
// before the primary's Monitor is dialled, so a fault on it builds nothing and caches nothing.
func (f *sessionFallback) resolver() *seatResolver {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.current != nil {
		return f.current
	}
	entry, ok := f.bound()
	if !ok || len(entry.Fallback.Names()) == 0 {
		return nil
	}
	monitor := f.binding().Monitor()
	f.current = newSeatResolver(sessionSeat, seatSource{
		primary:   entry,
		recovered: func(ctx context.Context) bool { return monitor.Beat(ctx).Reachable },
		lander:    f.lander,
		landed: func(_ *seatResolver, landing fallbackLanding) *apogee.DelegationTarget {
			f.remember(landing)
			return nil
		},
	})
	return f.current
}

// routedLander is the session seat's lander turned to the Sub-agents seat (delegationWiring's
// withFallback): the same configuration read at the move, landing as a routed target binds, over
// the window and the reserve share the session holds at that move.
func (f *sessionFallback) routedLander() fallbackLander {
	lander := f.lander()
	lander.routed = true
	lander.parent = f.parent()
	return lander
}

// remember records a landing, so the follow of the switch it answers can read it.
func (f *sessionFallback) remember(landing fallbackLanding) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.landed[landing.entry.Name] = landing
}

// landing is the last landing on the fallback named, false when the seat never moved onto it.
func (f *sessionFallback) landing(name string) (fallbackLanding, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	landing, ok := f.landed[name]
	return landing, ok
}

// rearm retires the current resolver and every landing it made: a human move (`/server`, a profile
// load) has put the session on an entry of its choosing, whose own `fallback:` list — or none —
// answers the next fault. A nil sessionFallback (a mover a test built without one) rearms nothing.
func (f *sessionFallback) rearm() {
	if f == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.current = nil
	clear(f.landed)
}

// newSessionFallback is the TUI session seat's resolver over the live settings: the bound entry and
// the configuration read at fault time, each fallback's key through the session's one key resolver
// (the source a `/server` move onto it would run), and the primary's grace beats through a Monitor
// dialled as the session's own binding stands at the fault (holder.Binding).
func newSessionFallback(
	live *liveSettings,
	holder *upstreamHolder,
	keys *config.KeyResolver,
	roots stateRoots,
) *sessionFallback {
	return &sessionFallback{
		bound:   live.boundEntry,
		binding: holder.Binding,
		lander: func() fallbackLander {
			return fallbackLander{
				opts:  live.options(),
				roots: roots,
				key:   keys.Resolve,
				beat: func(ctx context.Context, dial upstreamBinding) heartbeat.Beat {
					return dial.Monitor().Beat(ctx)
				},
			}
		},
		parent: live.delegationParent,
		landed: make(map[string]fallbackLanding),
	}
}
