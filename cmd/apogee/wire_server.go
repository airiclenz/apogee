package main

// The server-bind seam of the composition root, lifted out of wire.go by concern (ADR 0043).
//
// Which server this session runs on and when the wiring hears that something changed: the entry a
// startup selection collapses to, the single step that turns any entry — startup or human-picked —
// into a bound session, and the wait the renderer's config-reload chain parks on.
//
// At the end of the file, the same seam as the renderer sees it: serverHost, this binary's
// [tui.ServerHost] — the six Upstream acts the projection hands over as one named capability
// (ADR 0054) rather than as six bare funcs.

import (
	"context"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/heartbeat"
	"github.com/airiclenz/apogee/internal/session"
	"github.com/airiclenz/apogee/internal/tui"
)

// startupEntry re-assembles the server selection resolved (ADR 0036) from the flattened fields it
// left on options: the endpoint, the key SOURCE — all three spellings of it, since which one the
// entry named is exactly what the resolver has to be told — the discovery hint, the fan-out pin, the
// reply cap, the window pin, and the alias —
// which for a configured entry IS its `servers:` name and for the ephemeral override entry is the
// endpoint's host. It exists so the bind step below has ONE input shape, the ServerEntry, whether it
// is binding the startup server or the one a human picked out of the list — and, since the key is
// resolved from the entry rather than carried on it, so that every command in the composition root
// that needs the startup server's key asks for it the same way a switch does.
func startupEntry(opts config.Options) config.ServerEntry {
	return config.ServerEntry{
		Name:            opts.HostAlias,
		Endpoint:        opts.Endpoint,
		APIKey:          opts.APIKey,
		APIKeyCmd:       opts.APIKeyCmd,
		APIKeyEnv:       opts.APIKeyEnv,
		Model:           opts.Model,
		ParallelAgents:  opts.StartupParallelAgents,
		MaxOutputTokens: opts.StartupMaxOutputTokens,
		ContextWindow:   opts.StartupContextWindow,
		ResponseReserve: opts.StartupResponseReserve,
	}
}

// serverBinder is the one step that turns a ServerEntry into a running session: the Agent
// constructed against that server, the Monitor that observes it, and the binding the out-of-band
// calls (naming, Firings) read. It is a step rather than inline wiring because it now runs at two
// different times — before the TUI starts when a startup server was determined, and on the human's
// first pick when it was not (ADR 0036 decision 3) — and both must produce the same session.
//
// What it does NOT do is re-resolve the per-model bindings for the entry's `model:` hint. Those
// belong to the rebind path (rebindSpecFor), which the first beat of the Monitor installed here
// runs within seconds, for a late bind exactly as it does for the cold start a launch-time bind
// with no model already is.
type serverBinder struct {
	// cfg is everything about the session the server does not decide. The six fields it does —
	// endpoint, key, model hint, fan-out width, reply cap, context window — are overwritten from the
	// entry, so nothing that reached this struct can contradict the server being bound.
	cfg     apogee.Config
	resumed *session.Record
	engine  *lateEngine
	holder  *upstreamHolder
	// keys is the run's one key resolver: the entry names a key SOURCE, and this is what turns it
	// into the token the Agent and the Monitor send. It is the resolver the whole session shares,
	// so binding the server a startup already resolved for — a first pick on the entry the launch
	// snapshot named — costs no second run of that entry's command.
	keys *config.KeyResolver
	// caps is the session's Parallel agents cap (ADR 0039). The bind is where it FOLLOWS the entry:
	// the resolved width seeds the Config the Agent is constructed from, so a session is capped from
	// its first Turn rather than from its first beat.
	caps *parallelAgentsCap
	// build is how the Config this step assembles becomes an Agent. Every wiring leaves it nil, which
	// says exactly that — bind substitutes buildAgent, the one construction call there has ever been.
	//
	// It is a field rather than a direct call because that Config is otherwise unobservable: the six
	// fields the entry decides are written onto a copy no caller keeps, so the pins resolved below —
	// the fan-out width, the reply cap, the context window — could each be deleted and every test in
	// the package would still pass. A test supplies a build that records what it was handed.
	build func(apogee.Config, *session.Record) (*apogee.Agent, error)
}

// bind constructs the engine for entry and points both holders at it. The engine is constructed
// FIRST and the Monitor installed only once that succeeded, so a refused construction (Auto on a
// host without confinement, a future-version snapshot) leaves the session exactly as unbound as it
// was rather than half-wired to a server it cannot talk to.
//
// A second bind is refused before anything is constructed (lateEngine.Bind), because the holder can
// only release one Agent at shutdown: a session that already has an engine moves with
// sessionMover.move, which switches the one it has.
func (b serverBinder) bind(entry config.ServerEntry) error {
	// The key this server takes, resolved from the source the entry names BEFORE anything is
	// constructed: a source that refuses — a keychain that would not open, a variable nobody
	// exported — leaves the session exactly as unbound as a refused construction does, and says so
	// with the entry's own name. A literal key and an entry with no key source at all never leave
	// the process; a command source runs once per session and every later seam reads the cache.
	apiKey, err := b.keys.Resolve(entry)
	if err != nil {
		return err
	}

	cfg := b.cfg
	cfg.Endpoint = entry.Endpoint
	cfg.Model = entry.Model
	cfg.APIKey = apiKey
	// The fourth field the server decides, and the one that cannot be pushed after the fact here:
	// the Agent does not exist yet, so the resolved cap goes in through the Config it is built from.
	// follow's own push at the still-unbound engine is the no-op that says so.
	cfg.ParallelAgents = b.caps.follow(entry)
	// And the fifth: how big ONE reply from this server may be (ADR 0046). Like the width above it
	// is a property of the slot, so it follows the entry rather than the run — and it goes in the
	// same way, through the Config, because the pin must bound the session's very first Turn. 0 is
	// the honest absent value: the engine then derives the cap from the reply room its Budget
	// already reserves out of the window.
	cfg.Context.MaxOutputTokens = entry.MaxOutputTokens
	// And the sixth, the number that ceiling is derived from when nobody pins it: what this server
	// BOUNDS a session to (ADR 0045 decision 3). The entry's own `context-window:` outranks the
	// top-level key already in cfg — the precedence config.ResolveContextWindow spells, single-sited
	// there, the same call the `/server` move makes — and it goes in through the Config for the
	// reply cap's reason: a session that STARTS on a pinned entry must budget against that pin from
	// its first Turn rather than from the first beat that rebinds. Unpinned at both scopes it stays
	// 0, the honest "unknown until the first beat binds one".
	cfg.Context.MaxContextTokens = config.ResolveContextWindow(entry.ContextWindow, cfg.Context.MaxContextTokens)
	// And the seventh: how that window is SPLIT on this server — the entry's own `response-reserve:`
	// over the top-level key already in cfg (config.ResolveResponseReserve, the ranks the window one
	// above spells). It goes in through the Config for the two bounds' reason — the split must hold
	// from the session's very first Turn, not from the first beat — and 0 at both scopes is the
	// honest absent value: the engine then holds its own built-in share back.
	cfg.Context.ResponseReserveFraction = config.ResolveResponseReserve(
		entry.ResponseReserve, cfg.Context.ResponseReserveFraction)

	construct := buildAgent
	if b.build != nil {
		construct = b.build
	}
	if err = b.engine.Bind(func() (*apogee.Agent, error) {
		agent, err := construct(cfg, b.resumed)
		if err != nil {
			return nil, friendlyConstructErr(err)
		}
		return agent, nil
	}); err != nil {
		return err
	}
	b.holder.Bind(entry.Endpoint, apiKey, entry.Model,
		heartbeat.NewMonitor(entry.Endpoint, entry.Model, apiKey))
	return nil
}

// awaitConfigChangeOn adapts the polling watcher to [tui.Options.AwaitConfigChange]: one wait, one
// answer, and nothing about files or YAML crossing the seam (ADR 0041 decision 3). The renderer
// re-reads through [tui.Options.ReloadConfig] when this returns true, which is the same call an
// editor's exit makes — one apply path, two triggers.
//
// It answers false on two ends, and they mean the same thing to the caller: the program's context is
// done (a quit, which must not leave a goroutine parked on a channel until teardown reaches the
// watcher), or the watch itself has been stopped and closed its channel. Either way there will never
// be another report, and the chain retires.
func awaitConfigChangeOn(w *config.Watcher) func(context.Context) bool {
	return func(ctx context.Context) bool {
		select {
		case <-ctx.Done():
			return false
		case _, ok := <-w.Changes():
			return ok
		}
	}
}

// serverHost is this binary's [tui.ServerHost]: the six acts over the one Upstream this session is
// on — what it can move to, the two verbs that move or first bind it, the choice each move records,
// and the observation the display lives off (ADR 0054). It holds the wiring itself rather than six
// closures over it, because every act reads live state the wiring owns: the holder the switch
// re-points, the resolved options the list is projected from, and the binding the rebind starts from.
//
// Every act is one of the verbs above or beside it (wire_verbs.go), unchanged by the regrouping —
// they run where they always ran, refuse what they always refused, and answer what they always
// answered. This value is only where the renderer's six names meet them.
type serverHost struct{ w *rootWiring }

// Acts is all four: this binary observes for the life of the session, owns the resolution and the
// engine mutators a rebind needs, and wires both server verbs whatever the config says. The degrades
// [tui.ServerActs] exists for are a Driver's composition (ADR 0031), never this one's — a session
// with no Monitor YET is the pre-bound start, which the renderer already knows about
// ([tui.Options.Prebound]) and issues no beat in.
func (h serverHost) Acts() tui.ServerActs {
	return tui.ServerActs{CanObserve: true, CanRebind: true, CanSwitch: true, CanBind: true}
}

// Beat observes the server this session is on RIGHT NOW: it goes through the holder, so the
// observation follows the session onto another server without the seam — or the renderer — changing
// shape, and the wrapper reads the slot count off the same observation on its way past.
func (h serverHost) Beat(ctx context.Context) heartbeat.Beat { return h.w.beat(ctx) }

// Rebind re-resolves the per-model bindings for an observed change — the composition root's half of
// ADR 0024's split, run on the Update goroutine at the quiescent boundary the renderer picked.
func (h serverHost) Rebind(model string, contextWindow int) (tui.RebindResult, error) {
	return h.w.rebind(model, contextWindow)
}

// List projects the switchable servers from the HOLDER on every ask rather than from a snapshot, so
// a `servers:` block the human edits mid-session (ADR 0037) is offered by the picker and by the
// settings pane's server row the moment the edit lands — the same list, in the same order, that the
// two verbs below resolve a name against. It can be EMPTY (a pre-bound start on a config that lists
// nothing), which is exactly "nothing to switch to" without a special case.
func (h serverHost) List() []tui.ServerChoice { return serverChoices(h.w.live.choices(h.w.opts)) }

// Switch moves the whole session onto the named entry: the provider client re-pointed, a Monitor for
// the new server installed, the session record restamped (sessionMover.move).
func (h serverHost) Switch(name string) (tui.ServerSwitchResult, error) {
	return h.w.switchServer(name)
}

// Bind is the same move one step lower down (ADR 0036 decisions 3, 4 and 7): the session has no
// engine at all, so the named entry gets one constructed on it. It refuses a second bind.
func (h serverHost) Bind(name string) (tui.ServerSwitchResult, error) { return h.w.bindServer(name) }

// RecordChoice persists the entry this session should start on next time (ADR 0036 decision 2), and
// answers whether it wrote: a name in no `servers:` entry is skipped silently, which only this layer
// can tell.
func (h serverHost) RecordChoice(name string) (bool, error) { return h.w.recordServerChoice(name) }
