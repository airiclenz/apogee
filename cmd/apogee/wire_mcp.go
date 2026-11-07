package main

// The MCP seam of the composition root, lifted out of wire.go by concern (ADR 0043).
//
// The connections this session is running on, one per configured server, and the recipe for dialling
// one more: `mcp-servers:` is editable mid-session (ADR 0037 decision 6), and every server has its own
// connection lifecycle (ADR 0109, adr-0037-amend-1107). A request marks a removed server closed at
// once and queues one dial job per server it adds or changes; the jobs run off the caller; a commit at
// the next idle point folds every finished dial into one rebuild and one SwapTools, then closes the
// sessions it replaced — so a server that cannot be reached keeps its old session, or stays absent,
// and costs the healthy ones nothing. A `url-safety:` edit that newly denies a server marks it closed
// on its own (retire), ahead of the host-list swap. The wiring itself is the TUI's tui.MCPHost over this holder: the TUI runs the jobs as Cmds and commits
// at its idle point.

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strings"
	"sync"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/mcp"
	"github.com/airiclenz/apogee/internal/security"
	"github.com/airiclenz/apogee/internal/tui"
)

// mcpEntry is one configured server the session is connected to: the resolved config it was dialled
// with and its own one-server session. The config is what a later request diffs against
// (mcp.ServerConfig.Equal), so it is the config the session was DIALLED with — a re-dial that failed
// leaves it alone.
type mcpEntry struct {
	config  mcp.ServerConfig
	session mcpSession
	// cancel releases the context the session was dialled under, and is called only once the session
	// is closed (mcpEntry.close): go-sdk's SSE transport binds its stream to the Connect context, so
	// cancelling it any earlier would end the session under its tools. Nil for a session startup
	// dialled, which lives on the wiring's own context.
	cancel context.CancelFunc
}

// close ends the entry's session, then releases the context it was dialled under. An entry with no
// session (a failed dial's) is closed by releasing nothing.
func (e mcpEntry) close() error {
	err := closeSession(e.session)
	if e.cancel != nil {
		e.cancel()
	}
	return err
}

// mcpConnect dials ONE configured server under ctx, its endpoint checked against the url-safety host
// lists allowHosts and denyHosts. The context is the caller's because a session's life is bound to it
// (go-sdk's SSE transport binds its GET stream to the Connect context), so it must outlive the dial:
// it is a context that lives as long as the session, never one that ends when the dial returns.
type mcpConnect func(ctx context.Context, server mcp.ServerConfig, allowHosts, denyHosts []string) (mcpSession, error)

// mcpServerState is what the holder knows of ONE server the last request configured: the resolved
// config and admit verdict that request carried (what the next request diffs against, ADR 0109
// decision 7), the generation of the newest dial asked for it, and that dial's progress.
type mcpServerState struct {
	config mcp.ServerConfig
	// isAdmitted is the verdict mcp.Admit gave config under the request's url-safety lists.
	isAdmitted bool
	// gen identifies the newest dial asked for the server; a dial carrying any other is stale.
	gen uint64
	// cancel cancels the current-generation dial while it runs; nil when none does.
	cancel context.CancelFunc
	// result is the finished current-generation dial, held until a commit takes it in.
	result *mcpDialResult
	// hasFailed says the server's last finished dial failed: the retry-failed flag re-dials it.
	hasFailed bool
}

// mcpDialResult is a finished dial: on success the entry it would install, on failure the cause
// (with the entry's config still the one dialled, and no session).
type mcpDialResult struct {
	entry mcpEntry
	err   error
}

// mcpDialJob is one queued dial of one server, handed out by liveMCP.takeWork for its taker to run
// off the Update loop. run dials under a context derived from the one it is passed — which must live
// as long as the session it may open — and stores the result for the next commit, or closes it at
// once when a newer request has superseded it. A job run after the holder closed, or after a newer
// request for its server, does nothing at all.
type mcpDialJob struct {
	server string
	gen    uint64
	run    func(ctx context.Context)
}

// liveMCP owns the MCP connections the session is running on — a name→entry map, one session per
// server — and the lifecycle each one moves through. It exists because `mcp-servers:` is editable
// mid-session (ADR 0037 decision 6) and a connection is not a value: applying the key means dialling
// the servers it adds or changes off the Update loop, folding the finished dials into the tool
// registry at an idle point and closing the sessions that were replaced, each server on its own
// (ADR 0109).
//
// The lifecycle is three calls. request (on the caller) marks a removed or newly denied server closed
// at once and queues a dial job per server that moved; the jobs (takeWork) run anywhere; commit (on
// the caller, at an idle point) takes the finished dials in with one rebuild. retire is request's
// removal alone, for a `url-safety:` edit whose deny must land before its host lists move. close is
// the run's teardown: it cancels every dial, waits for them, and closes everything still open.
//
// The holder is what makes the tool registry's rebuild honest afterwards: the build recipe folds in
// whatever this holder currently carries, so a rebuild driven by any reason at all — a search endpoint
// edit, a later commit — carries the connections that are live now rather than the ones the process
// started with.
//
// The mutex guards everything below it: the writes come from the Update goroutine and the dial jobs,
// and the surfaced tools are called on the loop's worker goroutine, which reaches them through the
// registry rather than through this holder. An installed entry map is never written again — every
// change installs a fresh one — so a snapshot read under the lock stays valid after it is released.
type liveMCP struct {
	mu      sync.Mutex
	entries map[string]mcpEntry
	// order is the entries' names in config order, which is the order their tools are surfaced in.
	order []string

	// servers is the last-requested configured set, by name (mcpServerState), and requested its names
	// in config order.
	servers   map[string]*mcpServerState
	requested []string
	// jobs are the queued dials takeWork has not handed out yet, in request order.
	jobs []mcpDialJob
	// needsRebuild says a server was marked closed, or the live servers were reordered, since the
	// last commit, so the next one rebuilds even when no dial finished: the closed server's tools
	// leave the registry there, and the live ones take their new config order.
	needsRebuild bool
	// lastGen is the newest generation handed out, across every server, so a server removed and
	// re-added never reuses one.
	lastGen uint64
	// isStopped says close has begun: nothing is dialled, queued or installed after it.
	isStopped bool
	// dials counts the dial jobs running; closes counts the off-loop closes queued (closeOffLoop).
	dials  sync.WaitGroup
	closes sync.WaitGroup

	// connect dials one server, as startup dialled each — bar a repo server whose pin moved, which the
	// session it returns reports as skipped (mcpSession.Skipped).
	connect mcpConnect
}

// newLiveMCP holds the servers this run connected at startup, in config order, as the
// last-requested set (each admitted, since startup refuses a denied endpoint), and the recipe for
// dialling another.
func newLiveMCP(entries []mcpEntry, connect mcpConnect) *liveMCP {
	m := &liveMCP{connect: connect, servers: make(map[string]*mcpServerState, len(entries))}
	held := make(map[string]mcpEntry, len(entries))
	order := make([]string, 0, len(entries))
	for _, entry := range entries {
		held[entry.config.Name] = entry
		order = append(order, entry.config.Name)
		m.servers[entry.config.Name] = &mcpServerState{config: entry.config, isAdmitted: true}
	}
	m.requested = order
	m.install(held, order)
	return m
}

// tools surfaces what the connected servers advertise, in config order, for folding into a registry
// build.
func (m *liveMCP) tools() []apogee.Tool {
	entries, order := m.snapshot()
	var out []apogee.Tool
	for _, name := range order {
		out = append(out, entries[name].session.Tools()...)
	}
	return out
}

// session is the live session the holder carries for the server named name, nil when it carries
// none — a server never configured, removed, denied, or whose first dial failed.
func (m *liveMCP) session(name string) mcpSession {
	entries, _ := m.snapshot()
	return entries[name].session
}

// request moves the holder's intent onto servers (ADR 0109). It runs on its caller and dials nothing.
//
// It validates the whole set first (mcp.ValidateServers), as startup does before its first dial, and
// a refusal touches nothing. Then a server no longer configured is marked closed at once: its tools
// are refused from here on (mcpSession.Retire), its in-flight dial is cancelled, and its session
// closes off the loop; its tools leave the registry at the next commit. A server whose live endpoint
// guard now denies has that session closed the same way, but a dial in flight for a config guard
// still admits — the server moved off the denied endpoint — is left to land. Every other server is diffed against the LAST-REQUESTED set — its resolved
// ServerConfig and its admit verdict under guard — so a server whose first dial failed is dialled
// again only when its own config or verdict moves, or when retryFailed asks for every server whose
// last dial failed (the `mcp-servers` row's retry gesture, ADR 0109 decision 7). A server that moved
// gets one queued dial job, which supersedes (cancels or discards) any older dial for it; a server
// moved back onto the config its live session already runs drops that work and gets none. A set
// that puts the live servers in a new order dials none of them for it, but marks a rebuild wanted,
// so the next commit surfaces their tools in the new config order (ADR 0109 decision 4).
func (m *liveMCP) request(servers []mcp.ServerConfig, guard security.URLGuard, retryFailed bool) error {
	if err := mcp.ValidateServers(servers); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.isStopped {
		return nil
	}
	configured := make(map[string]bool, len(servers))
	for _, server := range servers {
		configured[server.Name] = true
	}
	for name := range m.servers {
		if !configured[name] {
			m.disconnectLocked(name)
			delete(m.servers, name)
		}
	}
	requested := make([]string, 0, len(servers))
	for _, server := range servers {
		m.requestServerLocked(server, guard, retryFailed)
		requested = append(requested, server.Name)
	}
	m.requested = requested
	if !slices.Equal(m.order, m.liveInRequestedLocked()) {
		m.needsRebuild = true
	}
	return nil
}

// liveInRequestedLocked is the installed servers' names in the last-requested config order — the
// order the next commit installs. The caller holds the lock.
func (m *liveMCP) liveInRequestedLocked() []string {
	return slices.DeleteFunc(slices.Clone(m.requested), func(name string) bool {
		_, isLive := m.entries[name]
		return !isLive
	})
}

// requestServerLocked is request's diff for ONE configured server. The caller holds the lock.
func (m *liveMCP) requestServerLocked(server mcp.ServerConfig, guard security.URLGuard, retryFailed bool) {
	state, isKnown := m.servers[server.Name]
	if !isKnown {
		state = &mcpServerState{}
		m.servers[server.Name] = state
	}
	live, isLive := m.entries[server.Name]
	if isLive && !mcpAdmits(live.config, guard) {
		// The lists now close the endpoint the server is connected on: the deny takes effect now. It
		// closes that session alone — a dial already running for a config the lists still admit is
		// left to land (or to fail into hasFailed, which retry-failed re-dials); the diff below drops
		// it only when the server's own config or verdict moved.
		m.dropLiveLocked(server.Name)
		isLive = false
	}
	isAdmitted := mcpAdmits(server, guard)
	wasAdmittedAsIs := isKnown && state.isAdmitted && state.config.Equal(server)
	isMoved := !isKnown || !state.config.Equal(server) || state.isAdmitted != isAdmitted
	state.config, state.isAdmitted = server, isAdmitted

	switch {
	case isLive && live.config.Equal(server):
		// Already on this config — unchanged, or an edit reverted: no re-dial, no tool churn, and any
		// dial for another config is dropped (ADR 0109 decision 2).
		if isMoved {
			m.supersedeLocked(server.Name, state)
		}
		state.hasFailed = false
	case !isAdmitted && wasAdmittedAsIs:
		// Only the verdict moved, to denied: the dial the server was waiting on is dropped and none
		// starts.
		m.supersedeLocked(server.Name, state)
		state.hasFailed = false
	case isMoved || (retryFailed && state.hasFailed):
		m.queueDialLocked(state, guard)
	}
}

// disconnectLocked marks the server named name closed: any dial for it is superseded, and its live
// entry, if any, is dropped (dropLiveLocked). The caller holds the lock.
func (m *liveMCP) disconnectLocked(name string) {
	if state := m.servers[name]; state != nil {
		m.supersedeLocked(name, state)
	}
	m.dropLiveLocked(name)
}

// dropLiveLocked closes the live entry of the server named name, if any, and nothing else: it is
// retired at once, dropped from the installed set and closed off the loop, while any dial for the
// server is left as it is. The next commit rebuilds the registry without it. The caller holds the
// lock.
func (m *liveMCP) dropLiveLocked(name string) {
	live, isLive := m.entries[name]
	if !isLive {
		return
	}
	live.session.Retire()
	entries := maps.Clone(m.entries)
	delete(entries, name)
	m.installLocked(entries, slices.DeleteFunc(slices.Clone(m.order), func(n string) bool { return n == name }))
	m.closeOffLoopLocked(live)
	m.needsRebuild = true
}

// retire marks the servers named names closed now and drops them from the last-requested set —
// request's removal path, with no request behind it: each one's tools are refused from here on
// (mcpSession.Retire), its in-flight dial is cancelled, its session closes off the loop, and its tools
// leave the registry at the next commit. It is the deny half of a `url-safety:` edit (ADR 0109),
// which runs before the host lists move, mid-Turn included, so a swap the engine refuses cannot keep
// a server the operator has just closed talking. A name the holder does not know is skipped; a server
// retired here and configured again is a new one to the next request, so it is dialled.
func (m *liveMCP) retire(names []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.isStopped {
		return
	}
	for _, name := range names {
		m.disconnectLocked(name)
		delete(m.servers, name)
	}
	m.requested = slices.DeleteFunc(slices.Clone(m.requested), func(n string) bool { return slices.Contains(names, n) })
}

// supersedeLocked moves the server named name to a fresh generation, so no older dial for it lands:
// the queued job is dropped, the running dial cancelled (its result is closed when it returns), and a
// finished result closed off the loop. The caller holds the lock.
func (m *liveMCP) supersedeLocked(name string, state *mcpServerState) {
	m.lastGen++
	state.gen = m.lastGen
	m.jobs = slices.DeleteFunc(m.jobs, func(job mcpDialJob) bool { return job.server == name })
	if state.cancel != nil {
		state.cancel()
		state.cancel = nil
	}
	if state.result != nil {
		m.closeOffLoopLocked(state.result.entry)
		state.result = nil
	}
}

// queueDialLocked supersedes the server's older dial and queues one for the config state carries,
// guarded by guard's lists. The caller holds the lock.
func (m *liveMCP) queueDialLocked(state *mcpServerState, guard security.URLGuard) {
	server := state.config
	m.supersedeLocked(server.Name, state)
	state.hasFailed = false
	gen := state.gen
	m.jobs = append(m.jobs, mcpDialJob{
		server: server.Name,
		gen:    gen,
		run:    func(ctx context.Context) { m.runDial(ctx, server, gen, guard) },
	})
}

// takeWork hands out the queued dial jobs, once: a second call returns only what was queued since.
func (m *liveMCP) takeWork() []mcpDialJob {
	m.mu.Lock()
	defer m.mu.Unlock()
	jobs := m.jobs
	m.jobs = nil
	return jobs
}

// wantsRebuild reports whether a server was marked closed, or the live servers reordered, since the
// last commit, so a commit is wanted with no dial behind it: the closed server's tools leave the
// registry there, and the live ones take their new config order.
func (m *liveMCP) wantsRebuild() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.needsRebuild
}

// isDialQueued reports whether a dial job waits for takeWork — what a request that just ran says
// about whether it started a dial.
func (m *liveMCP) isDialQueued() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.jobs) > 0
}

// hasFailedDial reports whether any configured server's last finished dial failed: what the
// `mcp-servers` row's retry gesture re-dials (ADR 0109 decision 7).
func (m *liveMCP) hasFailedDial() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, state := range m.servers {
		if state.hasFailed {
			return true
		}
	}
	return false
}

// cancelDials is the quit's first half: from it on nothing is dialled, queued or installed, every
// queued job is dropped and every running dial cancelled. It closes nothing — close, the run's own
// defer, still tears every server down after it.
func (m *liveMCP) cancelDials() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.isStopped = true
	m.jobs = nil
	for _, state := range m.servers {
		if state.cancel != nil {
			state.cancel()
		}
	}
}

// runDial is a dial job's body: it dials server under a context of its own, derived from parent and
// cancelled by a newer request or by close, then stores the result. A job whose generation is stale,
// or that runs after close began, never dials.
func (m *liveMCP) runDial(parent context.Context, server mcp.ServerConfig, gen uint64, guard security.URLGuard) {
	ctx, cancel, isStarted := m.startDial(parent, server.Name, gen)
	if !isStarted {
		return
	}
	defer m.dials.Done()
	session, err := m.dial(ctx, server, guard)
	m.storeDial(gen, mcpDialResult{entry: mcpEntry{config: server, session: session, cancel: cancel}, err: err})
}

// startDial registers a current-generation dial of the server named name: its cancellable context,
// and its place on the dials WaitGroup close waits on. Registering under the lock, refused once close
// has begun, is what lets a job the program never ran leave teardown nothing to wait for.
func (m *liveMCP) startDial(parent context.Context, name string, gen uint64) (context.Context, context.CancelFunc, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.servers[name]
	if m.isStopped || state == nil || state.gen != gen {
		return nil, nil, false
	}
	ctx, cancel := context.WithCancel(parent)
	state.cancel = cancel
	m.dials.Add(1)
	return ctx, cancel, true
}

// storeDial holds a finished dial for the next commit, or — superseded, or finished after close
// began — closes it here, off the loop, with no outcome: a superseded dial's `context canceled` is
// silent (ADR 0109 decision 6). A failed dial's context is released at once, since no session lives
// on it.
func (m *liveMCP) storeDial(gen uint64, result mcpDialResult) {
	m.mu.Lock()
	state := m.servers[result.entry.config.Name]
	if m.isStopped || state == nil || state.gen != gen {
		m.mu.Unlock()
		// A close that fails leaves the human nothing to do about a connection nobody asked for.
		_ = result.entry.close()
		return
	}
	defer m.mu.Unlock()
	state.cancel = nil
	if result.err != nil {
		result.entry.cancel()
		result.entry.cancel = nil
	}
	state.hasFailed = result.err != nil
	state.result = &result
}

// commit takes every finished current-generation dial in, in one rebuild and one SwapTools in config
// order (ADR 0109 decision 4), and runs on its caller at an idle point. Each result is re-admitted
// under set.built()'s url-safety lists first: one they now deny is closed instead of installed, the
// server's live session with it, and noted `mcp server <name> disconnected — its endpoint is denied`.
// A failed dial keeps the server's old session, or leaves it absent. Replaced and denied sessions
// close off the loop once the engine has taken the registry.
//
// The outcomes are one per result taken, in config order. With nothing finished, nothing marked
// closed and nothing reordered it does nothing. The error is the engine refusing the swap
// (mcpPendingError): every result stays pending for the next commit, nothing is closed and no
// outcome is returned.
func (m *liveMCP) commit(set *liveTools, engine settingsEngine) ([]mcpOutcome, error) {
	spec := set.built()
	guard := mcpGuard(spec.allowHosts, spec.denyHosts)
	m.mu.Lock()
	if m.isStopped {
		m.mu.Unlock()
		return nil, nil
	}
	pending := m.foldLocked(guard)
	if pending.needsSwap {
		// The rebuild below folds the holder's tools, so the new entries are installed before it —
		// and put back if the engine will not take the registry built from them.
		m.installLocked(pending.next, pending.order)
	}
	m.mu.Unlock()

	if pending.needsSwap {
		if err := set.rebuild(engine); err != nil {
			m.install(pending.previous, pending.previousOrder)
			return nil, mcpPendingError{cause: err}
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.settleLocked(pending)
	return pending.outcomes, nil
}

// mcpFold is what one commit would put in force: the entry set before and after, the outcomes, the
// results it takes and the sessions it closes once it lands.
type mcpFold struct {
	previous      map[string]mcpEntry
	previousOrder []string
	next          map[string]mcpEntry
	order         []string
	outcomes      []mcpOutcome
	taken         map[string]*mcpDialResult
	denied        []string
	closes        []mcpEntry
	needsSwap     bool
}

// foldLocked plans a commit over the pending results, in config order, under guard. The caller holds
// the lock; it changes nothing.
func (m *liveMCP) foldLocked(guard security.URLGuard) mcpFold {
	fold := mcpFold{
		previous:      m.entries,
		previousOrder: m.order,
		next:          maps.Clone(m.entries),
		taken:         map[string]*mcpDialResult{},
		needsSwap:     m.needsRebuild,
	}
	for _, name := range m.requested {
		state := m.servers[name]
		if state == nil || state.result == nil {
			continue
		}
		fold.taken[name] = state.result
		fold.add(name, *state.result, mcpAdmits(state.result.entry.config, guard))
	}
	for _, name := range m.requested {
		if _, isLive := fold.next[name]; isLive {
			fold.order = append(fold.order, name)
		}
	}
	return fold
}

// add folds ONE server's finished dial into the plan.
func (f *mcpFold) add(name string, result mcpDialResult, isAdmitted bool) {
	old, wasLive := f.next[name]
	switch {
	case result.err != nil:
		f.outcomes = append(f.outcomes, mcpFailed(name, wasLive, result.err))
	case !isAdmitted:
		f.closes = append(f.closes, result.entry)
		f.denied = append(f.denied, name)
		if wasLive {
			f.closes = append(f.closes, old)
			delete(f.next, name)
			f.needsSwap = true
		}
		f.outcomes = append(f.outcomes, mcpDisconnected(name))
	default:
		if wasLive {
			f.closes = append(f.closes, old)
		}
		f.next[name] = result.entry
		f.needsSwap = true
		f.outcomes = append(f.outcomes, mcpLanded(name, wasLive))
	}
}

// settleLocked records a commit that landed: the results it took are consumed, a denied server's
// verdict is recorded, and the sessions it displaced close off the loop. The caller holds the lock.
func (m *liveMCP) settleLocked(fold mcpFold) {
	for name, result := range fold.taken {
		if state := m.servers[name]; state != nil && state.result == result {
			state.result = nil
		}
	}
	for _, name := range fold.denied {
		if state := m.servers[name]; state != nil {
			state.isAdmitted, state.hasFailed = false, false
		}
	}
	if fold.needsSwap {
		m.needsRebuild = false
	}
	for _, entry := range fold.closes {
		m.closeOffLoopLocked(entry)
	}
}

// closeOffLoopLocked closes entry on a goroutine of its own, counted on the closes WaitGroup close
// waits on. The caller holds the lock, and close has not begun (after it, nothing is queued here).
func (m *liveMCP) closeOffLoopLocked(entry mcpEntry) {
	m.closes.Add(1)
	go func() {
		defer m.closes.Done()
		// Nothing dispatches to the session any more; a close that fails leaves the human nothing to
		// do about it and does not turn an apply that worked into one that failed.
		_ = entry.close()
	}()
}

// close tears down every server the session ended on — the run's own defer, which is why it reads the
// holder rather than closing the sessions the process started with (ADR 0109 decision 10). It cancels
// every dial in flight and waits for them, then closes every live entry and every finished result in
// parallel, and waits for those and for every close already queued. Nothing is dialled or installed
// after it begins.
func (m *liveMCP) close() error {
	m.cancelDials()
	m.dials.Wait()

	m.mu.Lock()
	open := make([]mcpEntry, 0, len(m.order)+len(m.servers))
	for _, name := range m.order {
		open = append(open, m.entries[name])
	}
	for _, state := range m.servers {
		if state.result != nil {
			open = append(open, state.result.entry)
			state.result = nil
		}
	}
	m.installLocked(map[string]mcpEntry{}, nil)
	m.mu.Unlock()

	errs := closeInParallel(open)
	m.closes.Wait()
	return errors.Join(errs...)
}

// closeInParallel closes every entry at once — each bounded by its own close ladder — and waits for
// all of them, returning their errors.
func closeInParallel(entries []mcpEntry) []error {
	errs := make([]error, len(entries))
	var wg sync.WaitGroup
	for i, entry := range entries {
		wg.Go(func() { errs[i] = entry.close() })
	}
	wg.Wait()
	return errs
}

// snapshot reads the installed entries and their order under the lock.
func (m *liveMCP) snapshot() (map[string]mcpEntry, []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.entries, m.order
}

// install puts entries and their order in force under the lock. Neither is written after this.
func (m *liveMCP) install(entries map[string]mcpEntry, order []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.installLocked(entries, order)
}

// installLocked is install for a caller that already holds the lock.
func (m *liveMCP) installLocked(entries map[string]mcpEntry, order []string) {
	m.entries, m.order = entries, order
}

// mcpOutcome is what a commit says about ONE server whose dial it took in: the transcript note
// (exactly one of the ADR 0109 sentences) and, for a failure, the row's clause and its cause.
type mcpOutcome struct {
	// note is `mcp <server> reconnected`, `mcp <server> connected`,
	// `mcp reconnect failed: <server>: … — previous connection kept`, `mcp connect failed: <server>: …`
	// or `mcp server <server> disconnected — its endpoint is denied`.
	note string
	// rowFailure is `<server>: …` for a failed dial, empty otherwise.
	rowFailure string
	// err is the failure's cause, kept so the row's error still wraps what internal/mcp refused with.
	err error
}

// mcpLanded is the outcome of a dial that answered: wasLive says whether it replaced a session.
func mcpLanded(server string, wasLive bool) mcpOutcome {
	if wasLive {
		return mcpOutcome{note: "mcp " + server + " reconnected"}
	}
	return mcpOutcome{note: "mcp " + server + " connected"}
}

// mcpFailed is the outcome of a dial that did not answer: wasLive says whether the server's old
// session is kept, which is the half of the sentence the human most needs — the alternative reading
// of a failed reconnect is that the server's tools are gone. The cause is de-labelled (mcpSentence),
// since every surface that prints it has already said `mcp`.
func mcpFailed(server string, wasLive bool, err error) mcpOutcome {
	clause := server + ": " + mcpSentence(err)
	note := "mcp connect failed: " + clause
	if wasLive {
		note = "mcp reconnect failed: " + clause + " — previous connection kept"
	}
	return mcpOutcome{note: note, rowFailure: clause, err: err}
}

// mcpDisconnected is the outcome of a dial the url-safety lists denied by the time it was committed
// (ADR 0109 decision 4): a policy decision the human made, so it is noted as one, never as a failure
// with a denial cause.
func mcpDisconnected(server string) mcpOutcome {
	return mcpOutcome{note: "mcp server " + server + " disconnected — its endpoint is denied"}
}

// mcpPendingError is commit's "refused — engine busy, still pending" outcome: the engine would not
// take the registry, so every finished dial stays pending for the next idle point and nothing was
// noted or closed. cause is the engine's refusal.
type mcpPendingError struct{ cause error }

// Error names the refusal and that nothing was lost.
func (e mcpPendingError) Error() string {
	return "engine busy, MCP dials still pending: " + e.cause.Error()
}

// Unwrap keeps the engine's refusal reachable.
func (e mcpPendingError) Unwrap() error { return e.cause }

// dial connects one server under ctx and guard's host lists. A session that skipped the server for a
// moved pin is closed (it holds nothing) and reported as the dial's failure, its cause the pin's own
// refusal.
func (m *liveMCP) dial(ctx context.Context, server mcp.ServerConfig, guard security.URLGuard) (mcpSession, error) {
	session, err := m.connect(ctx, server, guard.AllowHosts, guard.DenyHosts)
	if err != nil {
		return nil, err
	}
	if skipped := session.Skipped(); len(skipped) > 0 {
		_ = closeSession(session)
		return nil, skipped[0].Err
	}
	return session, nil
}

// mcpAdmits reports whether mcp.Admit lets server through guard — a stdio server always.
func mcpAdmits(server mcp.ServerConfig, guard security.URLGuard) bool {
	_, denied := mcp.Admit([]mcp.ServerConfig{server}, guard)
	return len(denied) == 0
}

// mcpRowFailures is what the outcomes of a commit that landed (liveMCP.commit) say as one row error: nil
// when every dial answered, otherwise an error whose text is the failed servers' clauses joined by "; " —
// the row that asked reads `saved — live apply failed: <server>: …` — and which still wraps each
// cause.
func mcpRowFailures(outcomes []mcpOutcome) error {
	var failed mcpRowError
	for _, outcome := range outcomes {
		if outcome.rowFailure != "" {
			failed = append(failed, outcome)
		}
	}
	if len(failed) == 0 {
		return nil
	}
	return failed
}

// mcpRowError is mcpRowFailures' error: a type rather than an errors.Join because the text joins the
// clauses on one line, while the causes stay in the chain for errors.Is/As.
type mcpRowError []mcpOutcome

// Error is the row's sentence: the failed servers' clauses, joined by "; ".
func (e mcpRowError) Error() string {
	clauses := make([]string, 0, len(e))
	for _, outcome := range e {
		clauses = append(clauses, outcome.rowFailure)
	}
	return strings.Join(clauses, "; ")
}

// Unwrap keeps every failed dial's cause reachable.
func (e mcpRowError) Unwrap() []error {
	causes := make([]error, 0, len(e))
	for _, outcome := range e {
		causes = append(causes, outcome.err)
	}
	return causes
}

// mcpSkipClause is the sentence for ONE repo server a connect skipped for a moved pin, worded from
// its name and its de-labelled refusal (mcpSentence) — which names the moved files and that the
// server is proposed again — never re-worded here, so the startup notice says what internal/mcp
// refused with. (A reconnect reports a skip as that server's failed dial instead, mcpFailed.)
func mcpSkipClause(s mcp.Denied) string {
	return "mcp server " + s.Name + " skipped — " + mcpSentence(s.Err)
}

// closeSession closes one server's session, tolerating there being none: closing nothing is not an
// error.
func closeSession(s mcpSession) error {
	if s == nil {
		return nil
	}
	return s.Close()
}

// mcpReconnectFailed is what the row says when a reconnect was refused WHOLE, with nothing moved — a
// busy engine, a set that fails validation (liveMCP.request), a registry the engine would not take
// (liveMCP.commit); one server's failed dial is worded per server instead (mcpFailed). It names the outcome the human
// most needs to know — that the servers they were talking to a moment ago are still connected —
// because the alternative reading of a failed reconnect is that the session now has no MCP tools.
//
// The sentence it builds drops internal/mcp's own `mcp: ` label from the refusal it carries. Every
// surface that prints this one has already said `mcp` — the `mcp-servers:` row is that key, and the
// url-safety row's clause is labelled by mcpNoteFor — so keeping the inner label stutters the word at
// the reader (`; mcp reconnect failed: mcp: server "docs": …`). Only the RENDERING loses it: the
// refusal stays wrapped, so a caller matching a sentinel internal/mcp wrapped (security.ErrURLBlocked,
// mcp.ErrEndpointDenied) still reaches it through errors.Is.
func mcpReconnectFailed(err error) error {
	return mcpReconnectError{err: err}
}

// mcpReconnectError is mcpReconnectFailed's sentence. It is a type rather than an fmt.Errorf because
// the two things it owes are in tension there: the text has to be composed from the CARRIED error's
// de-labelled sentence, which %w cannot spell, while the error itself has to stay in the chain.
type mcpReconnectError struct{ err error }

// Error is the sentence the row prints.
func (e mcpReconnectError) Error() string {
	return "reconnect failed: " + mcpSentence(e.err) + " — previous connections kept"
}

// Unwrap keeps the refusal reachable: errors.Is/As see exactly what the fmt.Errorf %w form exposed.
func (e mcpReconnectError) Unwrap() error { return e.err }

// mcpErrorPrefix is how internal/mcp opens the refusals it words itself (`mcp: server %q: …`). It is
// matched on this side of the seam, never produced: the prefix belongs to that package's sentences,
// which are ratified and not ours to re-word.
const mcpErrorPrefix = "mcp: "

// mcpSentence is one of those refusals with that label taken off, for embedding in a sentence a
// surface has already labelled `mcp`. An error that never carried the label comes back byte for byte.
func mcpSentence(err error) string {
	return strings.TrimPrefix(err.Error(), mcpErrorPrefix)
}

// ----------------------------------------------------------------------------
// The TUI's MCP seam (ADR 0109): the wiring as a tui.MCPHost
// ----------------------------------------------------------------------------

// mcpHost is the wiring as the TUI's [tui.MCPHost] over w.mcpSet, w.toolSet and w.engine, or nil —
// the TUI's inert host — when no MCP holder or tool set is wired.
func (w *rootWiring) mcpHost() tui.MCPHost {
	if w.mcpSet == nil || w.toolSet == nil {
		return nil
	}
	return w
}

// TakeMCPWork hands the TUI the dial jobs the requests since the last call queued, each once, to run
// off the Update loop, and whether a commit is wanted with no dial behind it (a closed server's
// tools must leave the registry, or the live servers' tools must take a new config order).
func (w *rootWiring) TakeMCPWork() ([]tui.MCPDial, bool) {
	jobs := w.mcpSet.takeWork()
	dials := make([]tui.MCPDial, 0, len(jobs))
	for _, job := range jobs {
		dials = append(dials, job.run)
	}
	return dials, w.mcpSet.wantsRebuild()
}

// CommitMCP takes every finished dial into the live tool set in one rebuild and one SwapTools, and
// answers one outcome per server it settled. The engine is asked first (CanSwapTools): a refusal —
// an Exchange open, or no server bound yet — rebuilds nothing and answers pending, with every result
// kept for the next commit and the refusal as the one outcome's row failure, so the `mcp-servers`
// row resolves on it. A swap the engine refuses after the rebuild answers the same way.
func (w *rootWiring) CommitMCP() ([]tui.MCPOutcome, bool) {
	if err := w.engine.CanSwapTools(); err != nil {
		return mcpRefusedCommit(err), true
	}
	outcomes, err := w.mcpSet.commit(w.toolSet, w.engine)
	if pending, isPending := errors.AsType[mcpPendingError](err); isPending {
		return mcpRefusedCommit(pending.cause), true
	}
	answered := make([]tui.MCPOutcome, 0, len(outcomes))
	for _, outcome := range outcomes {
		answered = append(answered, tui.MCPOutcome{Note: outcome.note, RowFailure: outcome.rowFailure})
	}
	return answered, false
}

// mcpRefusedCommit is a refused commit's one outcome: no transcript note — nothing moved and the
// next idle point asks again — and the row's failure, worded as a refused reconnect.
func mcpRefusedCommit(refusal error) []tui.MCPOutcome {
	return []tui.MCPOutcome{{RowFailure: mcpReconnectFailed(refusal).Error()}}
}

// CancelMCPDials cancels every dial in flight and drops the queued ones; the run's teardown closes
// the servers after the program returns (liveMCP.close).
func (w *rootWiring) CancelMCPDials() { w.mcpSet.cancelDials() }

// RetryFailedMCP is the `mcp-servers` row's retry gesture over an unchanged file (ADR 0109 decision
// 7): when some server's last dial failed it requests the configured set with the retry-failed flag,
// answering the request's note; when none did it requests nothing and answers "". The error is the
// request's refusal (settingsApplier.requestMCP).
func (w *rootWiring) RetryFailedMCP() (string, error) {
	if !w.mcpSet.hasFailedDial() {
		return "", nil
	}
	return w.settingsApplier().requestMCP(true)
}
