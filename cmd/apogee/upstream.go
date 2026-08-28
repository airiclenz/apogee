package main

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/format"
	"github.com/airiclenz/apogee/internal/heartbeat"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/tui"
)

// upstreamHolder owns the CURRENT heartbeat Monitor and is the reason a server switch needs no new
// seam on the renderer's side: [tui.ServerHost.Beat] keeps its one signature and is wired to the
// holder's Beat, so swapping which server is observed is a composition-root move the TUI never sees
// (ADR 0024's two seams, split by job). A Monitor is per-SERVER — it holds the endpoint and the key
// it was built with — so a switch replaces the whole Monitor rather than mutating one, honouring
// provider.Client's own "switching servers means a new Client" contract all the way up.
//
// The mutex guards the pointer alone. Beat is called from the TUI's beat Cmd goroutine while Swap
// and SetModel are driven from the Update loop, so the field is genuinely shared; the observation
// itself runs OUTSIDE the lock, because a beat is a network call that can take seconds and holding
// the lock across it would stall the Update goroutine's next swap behind a server that stopped
// answering. A swap mid-beat is therefore possible and safe by design: the in-flight beat lands
// carrying the retired heartbeat generation, which the TUI's generation guard makes inert.
//
// It also remembers the endpoint the current Monitor observes, because a Monitor deliberately does
// not expose one (it is per-server and immutable) and something at the composition root has to be
// able to answer "which server is this session on RIGHT NOW". A profile load asks exactly that, to decide
// whether the profile it just activated is already the session's server or somewhere else (ADR 0029
// D2) — and the endpoint of the entry the session launched on is the wrong answer the moment a
// `/server` switch has moved.
//
// The key and the bound model ride along for the same reason, and answer the same question one step
// further: an out-of-band call the composition root makes on the session's behalf — the session
// naming completion (title.go) is the only one — has to be built against the Upstream the session is
// on now, with the model it is now bound to and the credential that server takes. Keeping all three
// in the one place a switch already moves is what stops a second, quietly divergent notion of "the
// current Upstream" from growing beside this one.
type upstreamHolder struct {
	mu       sync.Mutex
	endpoint string
	apiKey   string
	model    string
	monitor  *heartbeat.Monitor
}

// upstreamBinding is everything a fresh call to this session's Upstream must be built from: where it
// is, which model it is bound to, and the key that opens it. It is a snapshot taken under the
// holder's lock and never a live view, so a caller cannot read an endpoint from before a switch
// against a key from after it.
//
// Model is empty when nothing is bound — a cold start before the first beat, or the gap a switch
// opens (the switch UNBINDS the model, the same clearing the session record's stamped model takes).
// A request built on an empty model leaves the field to the server, which is the honest reading of
// "we do not yet know what this server is serving".
type upstreamBinding struct {
	Endpoint string
	Model    string
	APIKey   string
}

// newUpstreamHolder builds the holder EMPTY: no Monitor, no binding, nothing to observe. The
// composition root's bind step fills it (Bind below) the moment a server is determined — before the
// TUI starts on the ordinary path, or on the first pick when the session started pre-bound (ADR
// 0036 decision 3) — which is why the holder has to exist before the engine does: every per-server
// seam is a closure over THIS pointer, and closing over one that a bind could replace would leave
// half the wiring watching a holder nobody fills.
func newUpstreamHolder() *upstreamHolder {
	return &upstreamHolder{}
}

// Bind installs the Monitor for the server the session is now on, together with the binding it
// observes: the endpoint, the resolved key, and that server's discovery hint (empty when the entry
// pins no model, where the first beat binds one). It is how a Monitor first ARRIVES — Swap below is
// the same write for a session that already had one — so it is the single writer of the four
// fields, and they move together under one lock.
func (h *upstreamHolder) Bind(endpoint, apiKey, model string, monitor *heartbeat.Monitor) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.endpoint = endpoint
	h.apiKey = apiKey
	h.model = model
	h.monitor = monitor
}

// Beat observes whichever Upstream is current when the beat starts. It is the value wired into
// [tui.ServerHost.Beat], so the TUI's unchanged one-beat-per-Interval cadence follows the session
// across a server switch without knowing one happened.
//
// With nothing bound yet there is nothing to observe, and the zero Beat is what that says: no
// server was reached because none was asked. The seam stays wired across the pre-bound state rather
// than appearing with the first binding, so the cadence the TUI started is the cadence that
// observes the server the human picks.
func (h *upstreamHolder) Beat(ctx context.Context) heartbeat.Beat {
	monitor := h.current()
	if monitor == nil {
		return heartbeat.Beat{}
	}
	return monitor.Beat(ctx)
}

// SetModel moves the current Monitor's discovery hint AND records the model as bound — the
// composition root's rebind closure calls it whenever a rebind commits, so discovery keeps resolving
// the model the session actually runs (heartbeat.Monitor.SetModel) and an out-of-band call built
// from Binding names that same model. The hint and the binding are one value here because a rebind
// is precisely the moment the two become the same fact. A hint stated against a Monitor that has
// since been retired is harmless: it dies with that Monitor, and the new server's own hint came in
// with it.
//
// With nothing bound there is no Monitor to state the hint against; the binding is still recorded,
// so a bind that follows carries it. (The rebind closure cannot reach here unbound — the engine
// refuses first — so this is a backstop, not a path.)
func (h *upstreamHolder) SetModel(model string) {
	h.mu.Lock()
	h.model = model
	monitor := h.monitor
	h.mu.Unlock()
	if monitor != nil {
		monitor.SetModel(model)
	}
}

// Swap makes monitor — observing endpoint with apiKey — the one subsequent beats observe. It is
// called once a switch has already COMMITTED in the engine (Agent.SwitchUpstream), so there is no
// failure to unwind: from the next beat on, the display observes the server the wire is actually
// pointed at. The fields move together under one lock, so no reader can see a Monitor paired with
// the endpoint it is not observing, or an endpoint paired with another server's key.
//
// The bound model is CLEARED, for the same reason the session record's stamped model is: a switch
// unbinds the model, and until the new server's first beat rebinds one, claiming the old server's
// model would be a claim about a server this session no longer talks to.
func (h *upstreamHolder) Swap(endpoint, apiKey string, monitor *heartbeat.Monitor) {
	h.Bind(endpoint, apiKey, "", monitor)
}

// Endpoint reports the Upstream the session is on right now — the launch endpoint until a move
// replaces it. It is the question a profile load asks before deciding whether it has to follow the
// profile it just activated.
func (h *upstreamHolder) Endpoint() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.endpoint
}

// Binding snapshots the current Upstream binding for a caller that must ACT on it rather than
// observe it — build a client against it, dial it, or resolve the config that is keyed on it. Every
// consumer reads it at CALL time for the one reason: that is what follows a `/server` switch and a
// rebind without a seam of its own.
//
//   - the session-naming call (title.go) constructs a fresh provider.Client per call from all three
//     fields;
//   - the rebind closure (wire.go) hands it to liveSettings.rebindInputs, so the per-model
//     resolution keys on the server the session is on NOW rather than on the launch endpoint;
//   - a scheduled Firing (schedule.go) takes BOTH halves of its upstream from it — the wire it
//     dials and the endpoint that same resolution keys on;
//   - the settings applier's rideTheRebind (wire.go) reads Model alone, to name the model a
//     committed `/settings` edit has to be re-resolved for.
func (h *upstreamHolder) Binding() upstreamBinding {
	h.mu.Lock()
	defer h.mu.Unlock()
	return upstreamBinding{Endpoint: h.endpoint, Model: h.model, APIKey: h.apiKey}
}

// current reads the live Monitor under the mutex and hands it back, so callers hold the lock for a
// pointer read rather than for whatever they then do with it (see the type's note on Beat).
func (h *upstreamHolder) current() *heartbeat.Monitor {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.monitor
}

// ----------------------------------------------------------------------------
// The shared move (ADR 0028's switch and ADR 0029's follow-the-profile)
// ----------------------------------------------------------------------------

// upstreamSwitcher is the engine mutation a move performs, named as the ONE method it needs so the
// fold below is exercisable without constructing an Agent. Satisfied by *apogee.Agent.
type upstreamSwitcher interface {
	SwitchUpstream(apogee.UpstreamSpec) error
}

// modelStamper is the session-metadata half of the same move: the model id stamped on saved records,
// which a move CLEARS because it unbinds the model. Satisfied by *sessionHost.
type modelStamper interface {
	SetModel(model string)
}

// sessionMover is the shared core of every move to another Upstream: `/server`'s explicit switch
// (ADR 0028) and a profile load's follow-the-profile (ADR 0029 D2) do exactly the same three things in
// exactly the same order, so they do them through one function rather than two copies free to drift
// apart. What differs between the two callers is only the four values move takes.
//
// It holds the collaborators rather than closing over them so the fold can be driven directly in a
// test, which is what makes the launcher seams built on it testable without a live server.
type sessionMover struct {
	agent  upstreamSwitcher
	holder *upstreamHolder
	host   modelStamper
	// keys resolves the key SOURCE the entry names into the token this move sends. It is the run's
	// one resolver, so switching back to a server this session has already been on costs no second
	// run of its command — and moving to one it has not yet touched is the first use of that
	// entry's source, which is precisely when design call 2 says it should run.
	keys *config.KeyResolver
	// live is the composition root's mutable settings holder, read for the top-level `context-window:`
	// pin at MOVE time rather than captured at launch: that pin survives a move, and since ADR 0037 a
	// `/settings` edit can have changed it since the session started. It is WRITTEN at the same moment
	// with the entry's own `context-window:` pin (followEntry), which is what keeps the new server's
	// window standing past the first beat's rebind instead of for the seconds until it.
	live *liveSettings
	// caps is the session's Parallel agents cap; a move is an arrival, so it re-follows here, after
	// the engine switch succeeded. It is REQUIRED: every construction of a sessionMover supplies one,
	// so the fold carries no nil guard and no arrival can quietly skip the follow.
	caps *parallelAgentsCap
}

// move re-points the whole session at entry and reports what the display should adopt.
//
// It takes a ServerEntry for the reason serverBinder.bind does: arriving on a server is ONE input
// shape whichever way the session arrives, so the pins that ride the entry — its `context-window:`,
// its `max-output-tokens:` and its `response-reserve:` share of the first — reach the engine here
// exactly as they reach it at a bind, rather
// than being dropped because a move happened to be spelled as four loose strings. entry.Name is what
// the footer will call the server (a `servers:` entry's name for a switch, the profile name for a
// load) and entry.Model is that server's discovery hint, which for a load is deliberately empty: a
// Launch profile's name is not a wire model id, and guessing one would send discovery looking for a
// model no server advertises.
//
// Order matters, and it is the order the `/server` closure established. The engine's own
// validate-then-commit switch goes FIRST, so a refusal — an Exchange still open, an endpoint the
// provider will not take — leaves the session exactly where it was. Past that call nothing can fail,
// and the follow-ups all describe the world the switch just made true: the Monitor is replaced
// whole (a Monitor is per-server, endpoint and key alike), the session's stored model is cleared,
// because the switch UNBOUND it and a Save landing in the gap before the new server's first beat must
// not claim the model of a server this session no longer talks to, the entry's window pin is
// latched, so the first beat's rebind re-resolves against the server the session is on now instead of
// binding that server's observation over the number its entry pinned, and the fan-out cap follows
// the entry the way every other arrival makes it follow (ADR 0039).
func (m sessionMover) move(entry config.ServerEntry) (tui.ServerSwitchResult, error) {
	// What this server bounds the session to, resolved BEFORE anything is mutated so the engine and
	// the display adopt one number: the entry's own window pin outranks the top-level key, which
	// itself survives a move (config.ResolveContextWindow), and the reply cap is the entry's as
	// written — an unpinned entry sends 0 and the engine derives its own cap from the window (ADR
	// 0046).
	window := config.ResolveContextWindow(int(entry.ContextWindow), m.live.pin())
	// And how the new server splits it: that entry's own `response-reserve:` over the top-level key,
	// which survives a move the way the window pin does (config.ResolveResponseReserve). Resolved
	// here, with the window, so the engine takes one statement about one server — 0 at both scopes
	// hands the split back to the engine's own built-in share.
	reserve := config.ResolveResponseReserve(entry.ResponseReserve, m.live.reservePin())
	// And the room inside that window the session works in on the new server: that entry's own
	// `working-window:` over the top-level key, which survives a move the way the pin does
	// (config.ResolveWorkingWindow). Resolved here, with the window, so the engine takes one
	// statement about one server — 0 at both scopes leaves the whole advertised window as the room.
	working := config.ResolveWorkingWindow(entry.WorkingWindow, m.live.workingPin())
	// The key that server takes, resolved from the source its entry names and resolved FIRST, in
	// front of the engine's own validate-then-commit switch: a source that refuses is one more way
	// this move cannot be made, and it must fail like the others — with the session still on the
	// server it was on, and with a message naming the entry the user just picked (design call 4).
	apiKey, err := m.keys.Resolve(entry)
	if err != nil {
		return tui.ServerSwitchResult{}, err
	}
	if err := m.agent.SwitchUpstream(apogee.UpstreamSpec{
		Endpoint:                entry.Endpoint,
		APIKey:                  apiKey,
		MaxContextTokens:        window,
		WorkingWindow:           working,
		MaxOutputTokens:         entry.MaxOutputTokens,
		ResponseReserveFraction: reserve,
	}); err != nil {
		return tui.ServerSwitchResult{}, err
	}
	// The replacement Monitor carries the new entry's forced effort dialect, the way the first
	// bind's does: the dial is a per-server fact, so it moves with the server (ADR 0060 decision 3).
	m.holder.Swap(entry.Endpoint, apiKey, heartbeat.NewMonitor(entry.Endpoint, entry.Model, apiKey,
		provider.WithEffortDialect(provider.EffortDialectFor(entry.EffortDialect))))
	m.host.SetModel("")
	m.live.followEntry(entry)
	// And how wide the session may fan out on the server it has just arrived on (ADR 0039): the new
	// entry's `parallel-agents:` pin becomes the pin and the retired server's observed slot count is
	// forgotten, because a slot count is a fact about one server. It lives HERE, in the shared fold,
	// rather than at either caller, because every arrival that is not a bind goes through this move —
	// a `/server` switch and a profile load alike — and one follow per arrival is the whole rule. An
	// entry that pins nothing (a profile load's does not) resolves to the serial floor 1 until the new
	// server's own first beat widens it, which is the honest width for a server no entry describes.
	m.caps.follow(entry)
	// What the display adopts: the endpoint now on the wire, the alias the footer calls it, and the
	// very window the engine was just handed, so the gauge and the Budget cannot describe different
	// servers. Unpinned at both scopes it is 0, the honest "unknown until the first beat binds one"
	// that the unbound model reads as too.
	return tui.ServerSwitchResult{
		Endpoint:      entry.Endpoint,
		HostAlias:     entry.Name,
		ContextWindow: window,
	}, nil
}

// upstreamChoices assembles the servers this session can be switched to: the `servers:` list
// verbatim, in file order, because since ADR 0036 that list is the single definition of what
// upstream servers exist. A session that started on a configured entry needs nothing added — the
// server it is on is already one of the rows, under the one name that labels it, selects it, and
// calls it in the footer.
//
// The one server the list cannot contain is the EPHEMERAL entry a raw `--endpoint`/`APOGEE_ENDPOINT`
// override builds for this run alone (ADR 0036 decision 6): it is unnamed, deliberately unwritten,
// and nowhere in the file. Only then is a row synthesized, prepended, carrying the resolved facts
// that run was built from — the endpoint's host as its label (config.aliasFromEndpoint, the same
// fallback the footer uses), the override key, and the override hint. That keeps ADR 0028's "the
// startup server is always offered" invariant true, so a user who overrides their way onto a rented
// box can still switch to a listed server and come back, without the override becoming config.
//
// Deriving the synthesized row from the ephemeral case alone is also what dissolves the edge the
// endpoint-equality test used to leave open: a configured startup can no longer be offered twice
// under two labels. That the synthesized LABEL does not collide with a configured `name` is not
// this function's doing but the alias's own: it is kept distinct where it is synthesized, by
// suffixing a host that equals a configured name `" (endpoint)"` (config.aliasFromEndpoint), so
// the row findServer resolves below is the row the user picked.
func upstreamChoices(opts config.Options) []config.ServerEntry {
	entries := make([]config.ServerEntry, 0, len(opts.Servers)+1)
	if opts.StartupEphemeral {
		entries = append(entries, config.ServerEntry{
			Name:     opts.HostAlias,
			Endpoint: opts.Endpoint,
			APIKey:   opts.APIKey,
			Model:    opts.Model,
		})
	}
	return append(entries, opts.Servers...)
}

// serverChoices projects the assembled entries onto the renderer's view of them: the name and the
// endpoint, which is display and identity and nothing else. The per-server api key and discovery
// hint deliberately stop here — they are what the SWITCH needs, and the switch is the binary's half
// of the seam, so the renderer never holds a credential it has no use for.
func serverChoices(entries []config.ServerEntry) []tui.ServerChoice {
	choices := make([]tui.ServerChoice, len(entries))
	for i, e := range entries {
		choices[i] = tui.ServerChoice{Name: e.Name, Endpoint: e.Endpoint}
	}
	return choices
}

// findServer resolves the name the host asked to switch to against the assembled entries. The TUI
// picks from the very list serverChoices projected, so an unknown name is a backstop rather than an
// expected path — but it is answered with the candidates all the same, because the one surface this
// error can reach is a transcript note the user reads.
func findServer(entries []config.ServerEntry, name string) (config.ServerEntry, error) {
	for _, e := range entries {
		if e.Name == name {
			return e, nil
		}
	}
	return config.ServerEntry{}, fmt.Errorf("unknown server %q — configured: %s", name, config.ServerNameList(entries))
}

// configuredServer reports whether name labels an entry of the `servers:` list — the question the
// recording seam asks before writing a `server:` line (ADR 0036 decision 2), and deliberately a
// different question from findServer's: that one resolves against the SWITCHABLE rows, which include
// the synthesized ephemeral startup, while only a row the file actually holds is a choice a next
// session could start on.
func configuredServer(entries []config.ServerEntry, name string) bool {
	for _, e := range entries {
		if e.Name == name {
			return true
		}
	}
	return false
}

// ----------------------------------------------------------------------------
// The Parallel agents cap (ADR 0039 decision 2)
// ----------------------------------------------------------------------------

// parallelAgentsSetter is the engine mutation a cap install performs, named as the ONE method it
// needs so the holder below is exercisable without constructing an Agent — the upstreamSwitcher
// posture. Satisfied by *lateEngine (and, through it, by *apogee.Agent).
type parallelAgentsSetter interface {
	SetParallelAgents(width int)
}

// parallelAgentsCap owns the Parallel agents cap for the server this session is bound to right now
// and is the only thing that pushes it at the engine. It exists because the cap is resolved from two
// facts that arrive at different times and from different places: the bound entry's
// `parallel-agents:` PIN, which comes with the entry the moment the session moves onto it, and the
// server's own `total_slots`, which only a landed beat can report. Holding them apart and resolving
// at the point of install is what lets either arrive first — a pinned entry is capped before the
// first beat, an unpinned one the moment discovery answers — without a second, subtly different
// resolution growing beside ResolveParallelAgents.
//
// It remembers the bound entry's NAME for one job: a `servers:` list the human edits mid-session
// (ADR 0037) has to be able to move the cap of the server the session is ALREADY on, and a name is
// what identifies an entry across a re-read (ADR 0036 decision 1). The ephemeral `--endpoint` entry
// is in no list, so it matches nothing and simply keeps what it was bound with — which is the honest
// answer for a server the file does not describe.
//
// The mutex is the upstreamHolder's, for the upstreamHolder's reason: follow runs on the Update
// goroutine (a `/server` switch, a first bind) while observe runs on the beat goroutine, so the two
// fields are genuinely shared. The engine seam it pushes through is itself anytime-safe
// (Agent.SetParallelAgents), so nothing here has to wait for a quiescent boundary.
type parallelAgentsCap struct {
	mu sync.Mutex
	// name is the bound entry's `servers:` name — how a re-read list is matched back to the server
	// this session is on. Empty until something is bound.
	name string
	// pinned is that entry's `parallel-agents:` value; 0 means the entry pins nothing, so the cap is
	// discovered.
	pinned int
	// observed is the last `total_slots` a beat could name for the CURRENT server, and 0 until one
	// does. It is forgotten on every move, because a slot count is a fact about one server and
	// carrying the retired server's width onto the new one is exactly the bug that would be
	// invisible.
	observed int

	engine parallelAgentsSetter
}

// newParallelAgentsCap builds the holder over the engine seam a resolved cap is pushed through.
// Nothing is bound yet, so the cap it would resolve is 1 — the serial floor a session with no server
// honestly runs at.
func newParallelAgentsCap(engine parallelAgentsSetter) *parallelAgentsCap {
	return &parallelAgentsCap{engine: engine}
}

// follow takes the cap onto entry: the entry's pin becomes the pin, the retired server's observed
// slot count is dropped, and the resolved cap is pushed at the engine and returned. It is called at
// every point a session ARRIVES on a server — the startup bind, a first pick, a `/server` switch —
// which is exactly where the bound server's window is re-stated too.
//
// The return value is what makes the bind path work without a second call: an Agent that does not
// exist yet cannot be pushed at, so the binder seeds its Config with this number and the push is the
// no-op an unbound engine answers with.
func (c *parallelAgentsCap) follow(entry config.ServerEntry) int {
	c.mu.Lock()
	c.name, c.pinned, c.observed = entry.Name, entry.ParallelAgents, 0
	width := config.ResolveParallelAgents(c.pinned, c.observed)
	c.mu.Unlock()
	c.engine.SetParallelAgents(width)
	return width
}

// observe records what a landed beat could say about the current server's slot count and re-installs
// the cap. A beat that could name none (0) is not evidence the server changed — only that this beat
// could not say — so it is dropped rather than written, exactly as liveSettings.observe treats an
// unnamed window.
//
// The install is unconditional rather than change-detecting: it is one mutex and one int on a
// ten-second cadence, and a cap that is re-stated to the value it already had is indistinguishable
// from one nobody touched.
func (c *parallelAgentsCap) observe(slots int) int {
	c.mu.Lock()
	if slots > 0 {
		c.observed = slots
	}
	width := config.ResolveParallelAgents(c.pinned, c.observed)
	c.mu.Unlock()
	c.engine.SetParallelAgents(width)
	return width
}

// current reports the width the bound server resolves to right now, and installs nothing. It is the
// door for a caller that COMPOSES a Config of its own rather than mutating the running engine — a
// scheduled Firing is that caller (schedule.go), and ADR 0031 is why it needs one: a Firing builds a
// second, short-lived Agent out of this session's shape, so the width it fans out at has to be the
// width the session it runs beneath is fanning out at. Its own Config was copied before the startup
// bind and carries a zero, which no engine reads as a cap.
//
// It is deliberately the only reader that does not push. follow, observe and relist each mark a MOVE
// the running engine has to be told about; a Firing asking how wide the server is changes nothing
// about the server or the session on it, and re-stating the Agent's cap on a Schedule's cadence,
// from the scheduler's goroutine, would be a mutation nobody asked for.
func (c *parallelAgentsCap) current() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return config.ResolveParallelAgents(c.pinned, c.observed)
}

// relist re-resolves the cap from a re-read `servers:` list (ADR 0037's live apply): the entry that
// still carries the bound server's name supplies the pin, and the observed slot count is KEPT —
// nothing about the server changed, only what the file says about it. A list that no longer names
// this session's server leaves the cap exactly where it was, which is the same posture the switch
// list takes toward an entry the human deleted while the session was on it.
func (c *parallelAgentsCap) relist(entries []config.ServerEntry) int {
	c.mu.Lock()
	for _, e := range entries {
		if e.Name != "" && e.Name == c.name {
			c.pinned = e.ParallelAgents
			break
		}
	}
	width := config.ResolveParallelAgents(c.pinned, c.observed)
	c.mu.Unlock()
	c.engine.SetParallelAgents(width)
	return width
}

// hintObserver remembers how discovery resolved the configured model id on the last beat, so the
// rebind that follows can say WHY the session is bound to a model the server never advertised. The
// grade itself is discovery's own observation (provider.HintResolution, carried on every Beat since
// a hint is trusted rather than substituted); this is the composition root holding the latest one
// between the goroutine that observes it and the seam that explains it.
//
// It is keyed on the model the grade was reached FOR, because a grade is a statement about one id
// against one advertised list. A human picking another model from `/model` rebinds before any beat
// has observed that id, and answering that rebind with the retired id's grade would explain a new
// binding with the old one's evidence; an unmatched read is simply no grade, and the next beat
// supplies one.
//
// Its zero value is the honest cold start — nothing observed, no grade for any model — so it is held
// by value and needs no constructor. The mutex is parallelAgentsCap's, for its reason: observe runs
// on the beat goroutine while gradeFor runs on the Update goroutine.
type hintObserver struct {
	mu    sync.Mutex
	model string
	grade provider.HintResolution
}

// observe records what a landed beat resolved. A beat that names no model — an unreachable server,
// or the zero Beat an unbound holder answers with — is dropped rather than written: it is not
// evidence the hint stopped resolving, only that this beat could not say, the same posture
// parallelAgentsCap.observe takes toward an unnamed slot count.
func (o *hintObserver) observe(model string, grade provider.HintResolution) {
	if model == "" {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.model, o.grade = model, grade
}

// gradeFor reports how discovery resolved model, and "" when the last beat resolved a different id
// or none at all — a grade nobody observed says nothing about this binding.
func (o *hintObserver) gradeFor(model string) provider.HintResolution {
	o.mu.Lock()
	defer o.mu.Unlock()
	if model == "" || model != o.model {
		return ""
	}
	return o.grade
}

// hintNotice is the one line a session prints when the model it just bound is not one the server
// advertises. Discovery no longer substitutes the first advertised model for an unmatched hint — it
// runs the id as configured — so the human has to be told that, and told what it cost: an unknown
// context window leaves the Budget and auto-compaction inactive, exactly as an advertised model that
// reports no window does, and a genuinely wrong id now fails loud on the next completion instead of
// quietly serving someone else's model. An exact match and the no-hint fallback are silent, because
// nothing surprising happened.
//
// The two windows are the observation and what the rebind actually bound (a `context-window:` pin
// outranks the observation, ADR 0024), which is why the base entry is named only when ITS number is
// the one in force: a variant slug inherits its window from the base entry, but a pinned session
// runs on the pin, and a notice crediting the base for a number the user pinned would be a lie about
// where the window came from.
func hintNotice(model string, grade provider.HintResolution, window, bound int) string {
	if grade != provider.HintBaseSlug && grade != provider.HintTrusted {
		return ""
	}
	notice := "model '" + model + "' is not advertised by the server; using it as configured"
	switch base, _, _ := strings.Cut(model, ":"); {
	case grade == provider.HintBaseSlug && window > 0 && bound == window:
		return notice + " (context window from base '" + base + "': " + format.Tokens(window) + ")"
	case bound > 0:
		return notice + " (context window: " + format.Tokens(bound) + ")"
	default:
		return notice + " (context window unknown — Budget and auto-compaction inactive)"
	}
}
