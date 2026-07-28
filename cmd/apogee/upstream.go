package main

import (
	"context"
	"fmt"
	"strings"
	"sync"

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
type upstreamHolder struct {
	mu      sync.Mutex
	monitor *heartbeat.Monitor
}

// newUpstreamHolder seeds the holder with the Monitor for the server this session STARTS on.
func newUpstreamHolder(monitor *heartbeat.Monitor) *upstreamHolder {
	return &upstreamHolder{monitor: monitor}
}

// Beat observes whichever Upstream is current when the beat starts. It is the value wired into
// [tui.Options.Heartbeat], so the TUI's unchanged one-beat-per-Interval cadence follows the session
// across a server switch without knowing one happened.
func (h *upstreamHolder) Beat(ctx context.Context) heartbeat.Beat {
	return h.current().Beat(ctx)
}

// SetModel moves the current Monitor's discovery hint — the composition root's rebind closure calls
// it whenever a rebind commits, so discovery keeps resolving the model the session actually runs
// (heartbeat.Monitor.SetModel). A hint stated against a Monitor that has since been retired is
// harmless: it dies with that Monitor, and the new server's own hint came in with it.
func (h *upstreamHolder) SetModel(model string) {
	h.current().SetModel(model)
}

// Swap makes monitor the one subsequent beats observe. It is called once a switch has already
// COMMITTED in the engine (Agent.SwitchUpstream), so there is no failure to unwind: from the next
// beat on, the display observes the server the wire is actually pointed at.
func (h *upstreamHolder) Swap(monitor *heartbeat.Monitor) {
	h.mu.Lock()
	h.monitor = monitor
	h.mu.Unlock()
}

// current reads the live Monitor under the mutex and hands it back, so callers hold the lock for a
// pointer read rather than for whatever they then do with it (see the type's note on Beat).
func (h *upstreamHolder) current() *heartbeat.Monitor {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.monitor
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
