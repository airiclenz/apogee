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
// are provable without a server; newFiringFallback wires the real ones.
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
// answer, and only the report that started it is told First. The run itself is detached from the
// caller that started it, so that caller's cancel never fails the others; each caller's own wait
// still ends at once when its ctx does, with the zero answer (the engine reads a cancelled ctx as
// the user's cancel whatever comes back).
func (r *fallbackResolver) Resolve(ctx context.Context, report apogee.FallbackReport) apogee.FallbackAnswer {
	r.mu.Lock()
	run, joined := r.runs[report.Server]
	if !joined {
		run = &fallbackRun{done: make(chan struct{})}
		r.runs[report.Server] = run
		go r.execute(ctx, report, run)
	}
	r.mu.Unlock()

	select {
	case <-run.done:
	case <-ctx.Done():
		return apogee.FallbackAnswer{}
	}
	answer := run.answer
	answer.First = !joined
	return answer
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
	// top-level pins an entry's own pins rank over, and the per-model keys the rebind reads.
	opts config.Options
	// roots are the state roots the per-model prompt files resolve against.
	roots stateRoots
	// key resolves the entry's key source into the token the fallback is sent.
	key func(entry config.ServerEntry) (string, error)
	// beat observes the fallback once, through the Monitor its dial facts build.
	beat func(ctx context.Context, dial upstreamBinding) heartbeat.Beat
}

// land beats the fallback named and, when it answers with a model to bind, resolves the answer a
// FallbackSwitch carries: the UpstreamSpec exactly as a `/server` move onto that entry would state it
// (entryUpstream), and the RebindSpec the beat's model binds there. A name no entry carries, a key
// source that refuses, a server that does not answer or names no model, and a per-model resolution
// that fails each leave the seat where it is (false) and the walk moves on.
func (l fallbackLander) land(ctx context.Context, name string) (apogee.FallbackAnswer, bool) {
	entry, err := findServer(l.opts.Servers, name)
	if err != nil {
		return apogee.FallbackAnswer{}, false
	}
	apiKey, err := l.key(entry)
	if err != nil {
		return apogee.FallbackAnswer{}, false
	}
	observed := l.beat(ctx, bindingOfEntry(entry, apiKey))
	if !observed.Reachable {
		return apogee.FallbackAnswer{}, false
	}
	model := entry.Model
	if model == "" {
		model = observed.ActiveModel
	}
	if model == "" {
		return apogee.FallbackAnswer{}, false
	}

	upstream, _ := entryUpstream(entry, apiKey, rootPinsOf(l.opts))
	rebind, err := l.rebind(entry, apiKey, model, observed)
	if err != nil {
		return apogee.FallbackAnswer{}, false
	}
	return apogee.FallbackAnswer{Upstream: upstream, Rebind: rebind, Server: entry.Name}, true
}

// rebind resolves the per-model binding for model on entry, the way an unattended run's bind
// resolves its own (bindFiringConfig): the configuration overlaid with the entry's endpoint, key,
// window pin and reserve share, the observed window bound where no pin stands, the entry's forced
// effort dialect over the one the beat ranked, and the entry's price.
func (l fallbackLander) rebind(
	entry config.ServerEntry,
	apiKey, model string,
	observed heartbeat.Beat,
) (apogee.RebindSpec, error) {
	specOpts := l.opts
	specOpts.Endpoint = entry.Endpoint
	specOpts.APIKey = apiKey
	pinnedWindow := config.ResolveContextWindow(int(entry.ContextWindow), l.opts.ContextWindow)
	specOpts.ContextWindow = pinnedWindow
	specOpts.ResponseReserve = config.ResolveResponseReserve(entry.ResponseReserve, l.opts.ResponseReserve)
	spec, _, err := rebindSpecFor(specOpts, l.roots, model, observed.ContextWindow, pinnedWindow, int(entry.MaxOutputTokens))
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

// newFiringFallback is an unattended run's fallback resolver for its session seat, or nil when the
// bound entry names no `fallback:` — the run is then exactly what it was before the key existed.
// The primary's grace beats go through one Monitor dialled as the run's own beat is (the entry's
// dial facts, the key resolved for it, the model the run bound as the discovery hint); each
// fallback's key resolves through the run's one resolver, fenced to the Firing's own workspace as
// the bound entry's was (bindFiringConfig).
func newFiringFallback(in firingInputs, keys *config.KeyResolver, apiKey, model string) *fallbackResolver {
	names := in.entry.Fallback.Names()
	if len(names) == 0 {
		return nil
	}
	primary := bindingOfEntry(in.entry, apiKey)
	primary.Model = model
	monitor := primary.Monitor()
	lander := fallbackLander{
		opts:  in.opts,
		roots: in.roots,
		key: func(entry config.ServerEntry) (string, error) {
			return keys.ResolveWithin(entry, in.roots.workspace)
		},
		beat: func(ctx context.Context, dial upstreamBinding) heartbeat.Beat {
			return dial.Monitor().Beat(ctx)
		},
	}
	return newFallbackResolver(in.entry.Name, names, config.ResolveFallbackAfter(in.entry.FallbackAfter),
		func(ctx context.Context) bool { return monitor.Beat(ctx).Reachable },
		lander.land)
}
