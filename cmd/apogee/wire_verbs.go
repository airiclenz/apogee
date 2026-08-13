package main

// The composition root's own verbs, lifted out of wire.go by concern (ADR 0043).
//
// The five seams the renderer drives that are not a holder and not a projection: what an observed
// model change implies, what a beat carries past on its way to the display, and the three ways a
// session arrives on — or records — an Upstream. Each was a closure over runRoot's locals and is now
// a method over the same values held on the wiring, which is the only difference: they run where
// they always ran (the Update goroutine, or the TUI's beat goroutine for [rootWiring.beat]), they
// resolve what they always resolved, and they refuse before they touch the engine exactly as before.

import (
	"context"
	"path/filepath"

	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/heartbeat"
	"github.com/airiclenz/apogee/internal/tui"
)

// rebind is the composition root's half of an observed model change. The TUI decides WHEN (at idle,
// or at the exchange-terminal boundary), this decides WHAT — because every input to the decision is
// config the binary owns (the per-model system prompt, ADR 0023; the validated set, ADR 0016; the
// manual mechanisms list; the window pin) and the engine mutators are the binary's to drive. It runs
// on the Update goroutine, at a quiescent boundary Agent.Rebind demands, so nothing here needs a
// lock of its own. A resolution error returns WITHOUT touching the engine, and Agent.Rebind is
// itself validate-then-commit, so a refused rebind leaves the session bound exactly where it was.
func (w *rootWiring) rebind(model string, window int) (tui.RebindResult, error) {
	// What the beat could name about the server's own window, remembered before it is resolved
	// against the pin: a later pin EDIT re-drives this seam with no beat of its own, and a
	// cleared pin has to bind the discovered window rather than unbind it (ADR 0024).
	w.live.observe(window)
	base, manualIDs, pinnedWindow := w.live.rebindInputs(w.opts, w.holder.Binding())
	spec, notices, err := rebindSpecFor(base, w.roots, manualIDs, model, window, pinnedWindow)
	if err != nil {
		return tui.RebindResult{}, err
	}
	// The model a session runs is now the id the human configured even when the server never
	// advertised it (provider's trusted-hint resolution), so the binding that lands on such an id
	// says so — once, here, where the beat that resolved it and the window it actually bound are
	// both in hand. It joins the validated-set lines rather than printing itself: a notice is
	// something the transcript tells the human, and this seam already has that channel.
	if notice := hintNotice(spec.Model, w.hints.gradeFor(model), window, spec.MaxContextTokens); notice != "" {
		notices = append(notices, notice)
	}
	if err := w.engine.Rebind(spec); err != nil {
		return tui.RebindResult{}, err
	}
	// Session metadata follows the wire: a session that switched models mid-conversation is
	// listed under the model it ends on, which is the one its last Turns actually ran against.
	w.host.SetModel(spec.Model)
	// And so does discovery: the hint is "which of the served models do I mean", a property of
	// the binding rather than of the launch. Re-stating it on every commit keeps the next beat
	// measuring the model the session now runs as "nothing new" — without it a server serving
	// both the old and the new id would resolve the stale config hint and this very seam
	// would bind straight back to it on the next beat. Through the holder, so the hint lands on
	// whichever server the session is currently on.
	w.holder.SetModel(spec.Model)
	return tui.RebindResult{
		Model:         spec.Model,
		ContextWindow: spec.MaxContextTokens,
		Notices:       notices,
	}, nil
}

// beat is the discovery half of the Parallel agents cap (ADR 0039 decision 2), wrapped around the
// beat the TUI already runs rather than probing for itself: `total_slots` rides the observation the
// heartbeat lands every Interval (ADR 0024 — no new beat machinery), and this is the composition
// root reading it on the way past. An unpinned server therefore widens from serial to its own slot
// count within one beat of being bound, and narrows again if the operator restarts it smaller; a
// pinned one is unmoved, because the pin outranks discovery.
//
// It runs on the TUI's beat goroutine, which is safe on both sides: the holder has its own mutex
// and the engine seam behind it is the anytime-safe class (Agent.SetParallelAgents). The Beat is
// returned untouched — the renderer sees exactly what the server said.
func (w *rootWiring) beat(ctx context.Context) heartbeat.Beat {
	// The Sub-agent server's own observation rides the SAME cadence (ADR 0045): one beat every
	// Interval, on the goroutine the renderer already opened for this one, so routing needs no clock
	// of its own. It is started before the session's and joined after it — side by side rather than
	// in series, which is what keeps two five-second discoveries from adding up to the interval
	// itself (delegation.go). With no Sub-agent server configured the join is a no-op and no second
	// beat happens at all.
	joinDelegation := w.delegation.observe(ctx)
	observed := w.holder.Beat(ctx)
	w.caps.observe(observed.TotalSlots)
	// And the same reading catches the other thing only discovery can say: HOW the configured id
	// resolved against the advertised list, remembered here so the rebind this beat may trigger can
	// explain an unadvertised model without re-deriving the match.
	w.hints.observe(observed.ActiveModel, observed.Resolution)
	// And the second observation is collected before this beat counts as landed: the TUI re-arms the
	// cadence from a RETURNED beat, so joining here is what keeps the two servers on one clock rather
	// than letting the delegation beat drift into the next interval.
	joinDelegation()
	return observed
}

// switchServer is the composition root's half of `/server`. The TUI decides WHEN (at idle, on an
// explicit act by the human), this decides everything the move touches — because a server is an
// endpoint, a key, and a discovery hint, and all three are config the binary owns. It runs on the
// Update goroutine at the quiescent boundary Agent.SwitchUpstream demands, and opens no connection
// of its own: the first BEAT is what discovers the new server.
//
// All this adds to the shared fold is RESOLUTION, and it comes first: a name that resolves to
// nothing never reaches the engine, so the session is left exactly where it was.
func (w *rootWiring) switchServer(name string) (tui.ServerSwitchResult, error) {
	entry, err := findServer(w.live.choices(w.opts), name)
	if err != nil {
		return tui.ServerSwitchResult{}, err
	}
	result, err := w.mover.move(entry)
	if err != nil {
		return tui.ServerSwitchResult{}, err
	}
	// The launcher follows the entry the session just moved onto — off when the entry names no
	// config, on against its own when it does. It is installed HERE, at the switch, and not
	// inside the shared move: a profile load reaches that move directly and must leave the
	// integration exactly as it found it (launcherPath.follow carries the reasoning). And it is
	// installed only after the move SUCCEEDED, so a refused switch leaves the session's launcher
	// where the session still is.
	w.launcherPath.follow(entry)
	// And so does the fan-out width, for the same reason and on the same terms (ADR 0039): how
	// many sub-agents may run at once is the new server's answer, not the old one's, and the
	// entry's pin is in hand right here. A move is the one moment the previously observed slot
	// count must be forgotten — parallelAgentsCap.follow does that — so an unpinned server starts
	// serial and widens the moment its own first beat reports its slots.
	w.caps.follow(entry)
	return result, nil
}

// recordServerChoice is the composition root's half of ADR 0036 decision 2 — the `server:` key this
// session's last deliberate choice becomes, so the NEXT session starts where this one ended without
// asking. It is spliced into the same config.yaml every other write goes through (the ADR 0035
// writer, extended by 0036 to this key), which is why it lives beside the two verbs above rather
// than in the renderer: the file, the path, and the registry that says the key may be written are
// all the binary's.
//
// The CONFIGURED check is the whole of the decision, and it is a name lookup in the file's own
// list rather than in the switchable choices: the one row those two lists differ by is the
// synthesized ephemeral startup (upstreamChoices), which names no entry and must never be
// written back as one. A move onto it — like a launcher profile's server, which never reaches
// this seam at all — is skipped SILENTLY: false with no error, which the renderer states by
// saying nothing about a recording.
func (w *rootWiring) recordServerChoice(name string) (bool, error) {
	if !configuredServer(w.live.serverList(), name) {
		return false, nil
	}
	if err := config.SaveConfigSetting(filepath.Join(w.roots.config, "config.yaml"), "server", name); err != nil {
		return false, err
	}
	return true, nil
}

// bindServer is the same resolution as the switch, ending in a CONSTRUCTION rather than a move. It
// is the only way out of the pre-bound state, and it answers with what a switch answers so the
// display adopts a first binding exactly as it adopts a move — the endpoint now on the wire, the
// entry's name as the alias, and the window in force on the server just bound. The binder refuses a
// second construction, so an already-bound session is told to use `/server` instead of quietly
// growing a second engine.
func (w *rootWiring) bindServer(name string) (tui.ServerSwitchResult, error) {
	entry, err := findServer(w.live.choices(w.opts), name)
	if err != nil {
		return tui.ServerSwitchResult{}, err
	}
	if err := w.binder.bind(entry); err != nil {
		return tui.ServerSwitchResult{}, err
	}
	// The same install as the switch above, for the same reason: this is the other way a session
	// arrives ON an entry, so a pre-bound start that binds the launcher-fronted server has the
	// integration from that moment. A refused bind installs nothing.
	w.launcherPath.follow(entry)
	// And the same latch, for the switch's reason (liveSettings.followEntry): the binder has already
	// handed the engine this entry's window, and without the latch the FIRST beat's rebind would
	// re-resolve from the top-level key alone and bind that server's observation over the number its
	// entry pinned. It follows the bind, so a refused construction latches nothing.
	w.live.followEntry(entry)
	return tui.ServerSwitchResult{
		Endpoint:      entry.Endpoint,
		HostAlias:     entry.Name,
		ContextWindow: w.live.window(),
	}, nil
}
