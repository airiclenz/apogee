package main

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/heartbeat"
	"github.com/airiclenz/apogee/internal/tui"
)

// upstreamHolder owns the CURRENT heartbeat Monitor and is the reason a server switch needs no new
// seam on the renderer's side: [tui.Options.Heartbeat] keeps its one signature and is wired to the
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
// D2) — and the launch-time `endpoint:` is the wrong answer the moment a `/server` switch has moved.
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

// newUpstreamHolder seeds the holder with the Monitor for the server this session STARTS on, plus
// the launch-time binding that Monitor observes: the endpoint, the resolved key, and the configured
// `model:` pin (empty on a cold start, where the first beat binds one).
func newUpstreamHolder(endpoint, apiKey, model string, monitor *heartbeat.Monitor) *upstreamHolder {
	return &upstreamHolder{endpoint: endpoint, apiKey: apiKey, model: model, monitor: monitor}
}

// Beat observes whichever Upstream is current when the beat starts. It is the value wired into
// [tui.Options.Heartbeat], so the TUI's unchanged one-beat-per-Interval cadence follows the session
// across a server switch without knowing one happened.
func (h *upstreamHolder) Beat(ctx context.Context) heartbeat.Beat {
	return h.current().Beat(ctx)
}

// SetModel moves the current Monitor's discovery hint AND records the model as bound — the
// composition root's rebind closure calls it whenever a rebind commits, so discovery keeps resolving
// the model the session actually runs (heartbeat.Monitor.SetModel) and an out-of-band call built
// from Binding names that same model. The hint and the binding are one value here because a rebind
// is precisely the moment the two become the same fact. A hint stated against a Monitor that has
// since been retired is harmless: it dies with that Monitor, and the new server's own hint came in
// with it.
func (h *upstreamHolder) SetModel(model string) {
	h.mu.Lock()
	h.model = model
	monitor := h.monitor
	h.mu.Unlock()
	monitor.SetModel(model)
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
	h.mu.Lock()
	h.endpoint = endpoint
	h.apiKey = apiKey
	h.model = ""
	h.monitor = monitor
	h.mu.Unlock()
}

// Endpoint reports the Upstream the session is on right now — the launch endpoint until a move
// replaces it. It is the question a profile load asks before deciding whether it has to follow the
// profile it just activated.
func (h *upstreamHolder) Endpoint() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.endpoint
}

// Binding snapshots the current Upstream binding for a caller that must BUILD a client rather than
// observe one — today the session-naming call in title.go, which constructs a fresh provider.Client
// per call precisely so it follows a `/server` switch and a rebind without a seam of its own.
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
	agent        upstreamSwitcher
	holder       *upstreamHolder
	host         modelStamper
	pinnedWindow int
}

// move re-points the whole session at endpoint and reports what the display should adopt.
//
// Order matters, and it is the order the `/server` closure established. The engine's own
// validate-then-commit switch goes FIRST, so a refusal — an Exchange still open, an endpoint the
// provider will not take — leaves the session exactly where it was. Past that call nothing can fail,
// and the two follow-ups both describe the world the switch just made true: the Monitor is replaced
// whole (a Monitor is per-server, endpoint and key alike), and the session's stored model is cleared,
// because the switch UNBOUND it and a Save landing in the gap before the new server's first beat must
// not claim the model of a server this session no longer talks to.
//
// alias is what the footer will call the server — a `servers:` entry's name for a switch, the profile
// name for a load — and hint is that server's discovery hint, which for a load is deliberately empty:
// a Launch profile's name is not a wire model id, and guessing one would send discovery looking for a
// model no server advertises.
func (m sessionMover) move(endpoint, alias, hint, apiKey string) (tui.ServerSwitchResult, error) {
	if err := m.agent.SwitchUpstream(apogee.UpstreamSpec{Endpoint: endpoint, APIKey: apiKey}); err != nil {
		return tui.ServerSwitchResult{}, err
	}
	m.holder.Swap(endpoint, apiKey, heartbeat.NewMonitor(endpoint, hint, apiKey))
	m.host.SetModel("")
	// What the display adopts: the endpoint now on the wire, the alias the footer calls it, and the
	// `context-window:` pin — which is GLOBAL and therefore survives a move, so the renderer still
	// needs no knowledge of it. Unpinned it is 0, the honest "unknown until the first beat binds one"
	// that the unbound model reads as too.
	return tui.ServerSwitchResult{
		Endpoint:      endpoint,
		HostAlias:     alias,
		ContextWindow: m.pinnedWindow,
	}, nil
}

// upstreamChoices assembles the servers this session can be switched to: every `servers:` entry in
// file order, preceded by a synthesized entry for the endpoint the session STARTED on whenever no
// configured entry already names it. The synthesized row is what makes switching away reversible
// without config surgery — a user who lists one remote server must still be able to come back to
// the local one they launched against, and asking them to also list their own `endpoint:` twice
// would be a config chore in service of an implementation detail.
//
// The synthesized row carries the resolved facts the startup server was built from: the host alias
// as its name (the same one field that labels the row, selects the server, and calls it in the
// footer — `host-alias:` already names the startup endpoint exactly that way), the resolved key,
// and the config'd `model:` as that server's discovery hint.
//
// "Already names it" is plain endpoint equality on purpose: it is the same comparison the picker
// marks the current row by, so the two halves cannot disagree about which row the session is on.
func upstreamChoices(opts options) []serverEntry {
	entries := make([]serverEntry, 0, len(opts.servers)+1)
	if !namesEndpoint(opts.servers, opts.endpoint) {
		entries = append(entries, serverEntry{
			Name:     opts.hostAlias,
			Endpoint: opts.endpoint,
			APIKey:   opts.apiKey,
			Model:    opts.model,
		})
	}
	return append(entries, opts.servers...)
}

// namesEndpoint reports whether some configured entry already points at endpoint.
func namesEndpoint(servers []serverEntry, endpoint string) bool {
	for _, s := range servers {
		if s.Endpoint == endpoint {
			return true
		}
	}
	return false
}

// serverChoices projects the assembled entries onto the renderer's view of them: the name and the
// endpoint, which is display and identity and nothing else. The per-server api key and discovery
// hint deliberately stop here — they are what the SWITCH needs, and the switch is the binary's half
// of the seam, so the renderer never holds a credential it has no use for.
func serverChoices(entries []serverEntry) []tui.ServerChoice {
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
func findServer(entries []serverEntry, name string) (serverEntry, error) {
	for _, e := range entries {
		if e.Name == name {
			return e, nil
		}
	}
	return serverEntry{}, fmt.Errorf("unknown server %q — configured: %s", name, serverNameList(entries))
}

// serverNameList renders the switchable names for findServer's error (an empty list renders
// "(none)", matching knownMechanismList's shape for the same job).
func serverNameList(entries []serverEntry) string {
	if len(entries) == 0 {
		return "(none)"
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name
	}
	return strings.Join(names, ", ")
}
