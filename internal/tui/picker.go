package tui

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/format"
	"github.com/airiclenz/apogee/internal/heartbeat"
)

// ----------------------------------------------------------------------------
// The shared single-select picker overlay (/model, /server, /schedule)
// ----------------------------------------------------------------------------
//
// The /sessions browser's simpler sibling: a modal list with one highlight, ⏎ to take the
// highlighted row, esc to close, painted through the shared popup module (renderPopup) so it shares
// the browser's chrome and right edge. It answers ONE question — which of the things the session
// could be pointed at should it be pointed at now — so the state is four plain values and the
// verbs differ only by a [pickerKind].
//
// Rows are DERIVED at render time from the state they describe, never captured at open. That is
// what lets a beat landing under an open /model picker refresh the offering in place (the
// selection is clamped, [listCursor.clampSelection]) instead of leaving the human
// choosing from a list the server has moved on from.
//
// The accept path is deliberately not a third way to bind: a picked model becomes a
// [rebindIntent] fed to [Model.applyRebind] — the very orchestration a heartbeat-observed change
// takes — so the seam call, the fail-once note, the restated start-up box and the notices all come
// from the one existing path (ADR 0024's "cold start, late seed and mid-session switch are ONE code
// path", extended to the switch the human asks for). The pick is also recorded as the last
// OBSERVATION, exactly as [Model.observeBinding] records one, so the next beat reporting the picked
// model measures as "nothing new" rather than as a fresh change to bind back.
//
// /server is the same overlay over a different question, and its accept is the same shape one level
// up: the binary moves the whole Upstream behind the unchanged seams ([ServerHost.Switch]) and
// the TUI folds what came back ([Model.foldServerSwitch]) — a fresh heartbeat generation, no model
// bound, and the new server's very first beat completing the move through that same rebind path.
//
// /model has a SECOND offering, and on a host where llama-launcher is configured it is the only one:
// the Launch profiles the launcher defines (ADR 0029 D3). "Switch model" is one question, not two —
// on the single-model local server this integration exists for, what the server advertises is a
// one-row view of the same world the profiles describe — so the verb asks it once and the wiring
// decides which half of the world can answer. That form's accept is the one that does not finish on
// the Update loop: activating a Launch profile blocks for as long as a server takes to come up, so it
// hands off to the actuation latch (actuation.go) and the completion fold there ends in one of the
// two shapes above. Its rows are also the one offering read at open rather than derived per frame,
// because they live in the launcher's config file rather than in Model state.
//
// /schedule brings three kinds more, and they are the overlay's first that answer a question
// instead of re-pointing the session (ADR 0033): a cycle picker and a mode picker, asked in that
// order for a "/schedule <prompt>" with no cycle token, and a stop picker over the live Schedules.
// The two-question flow is what the picker's [scheduleDraft] exists for — the prompt and the chosen
// cycle ride the overlay's own state to the accept that finally creates the Schedule — and the stop
// picker's rows are derived per frame like /model's, so a Schedule that fired, finished or was
// stopped under the open pane moves with it. Their verbs and their wording live in schedule.go;
// what lives here is only what every kind shares.
//
// Whichever offering /model lists, what the session is already on is NOT in it: a row that switches
// nothing is not a choice, and pickerHint's "⏎ switch" would be a promise it could not keep. /server
// keeps its "· current" row instead, because where the session IS is half of what a server list is
// read for.

// pickerKind names WHICH offering an open picker is listing. It is an enum rather than a callback
// field on the state so the Model keeps holding plain values only (ADR 0011) — every kind-specific
// answer (the rows, the title, the accept) is one switch away.
type pickerKind int

const (
	pickerModel        pickerKind = iota // the models the Upstream advertises — /model without a launcher
	pickerServer                         // the servers config.yaml names — /server, over m.opts.Server
	pickerLoad                           // the Launch profiles the launcher defines — /model with one
	pickerCycle                          // how often a new Schedule fires — /schedule's first popup
	pickerScheduleMode                   // the mode that Schedule's Firings run in — /schedule's second
	pickerScheduleStop                   // the live Schedules — /schedule-stop with more than one
	pickerKeyMigration                   // what to do about one entry's plaintext key — the start-up offer
)

// picker is the overlay's inline state on the Model. Its zero value is "closed", so it lives inline
// in the value-copied Model like sessionBrowser and autocompleteState (ADR 0011).
type picker struct {
	open bool
	kind pickerKind
	// listSurface is the highlight and the filter — the state a list overlay that FILTERS keeps, and
	// the key contract that goes with it (listsurface.go, ADR 0053), embedded rather than re-declared
	// so `m.picker.selected` and `m.picker.filter` still read as the pane's own while there is one
	// answer to what ↑/↓, a typed letter and esc do inside a modal list. The lists that do not filter
	// hold the [listCursor] inside it and answer the same arrows through the same code.
	//
	// selected indexes the FILTERED rows the current kind derives; it is clamped rather than trusted,
	// because the list underneath it can change while the overlay is open.
	listSurface
	// profiles are the Launch-profile rows, and the one offering that is NOT derived at render time.
	// The other two describe Model state (the advertised models, the configured servers) and so can be
	// re-read every frame; a Launch profile lives in the launcher's config FILE, behind a seam that
	// re-reads it from disk (ADR 0029 D4). Reading once per open is what makes those rows fresh —
	// a profile added in the launcher's own TUI a moment ago is offered here — without turning a
	// keypress into a file read. A plain slice of plain values, safe in the copied Model.
	profiles []LaunchProfileChoice
	// draft is the half-built Schedule /schedule's two popups carry between them: the prompt the
	// human typed, and the cycle the first popup took. It is what makes a two-question flow one
	// state — plain values only, so the copied Model stays copyable (ADR 0011) — and it is cleared
	// with the rest of the overlay when the second popup accepts or esc closes either one.
	draft scheduleDraft
	// migration is what is left of a round of start-up key-migration offers (keymigration.go): the
	// `servers:` entries still to be asked about, the one being asked about now at the front. It
	// lives on the overlay so the round is one piece of state — a plain slice of plain strings, safe
	// in the copied Model (ADR 0011) — and so the whole-struct zeroing every close and accept
	// already does ends the round: esc leaves every remaining entry unasked, which is a "not now"
	// and persists nothing.
	migration []string
}

// maxPickerRows caps how many rows the overlay shows at once; a longer list scrolls a window around
// the selection (popupRowWindow), the maxSessionRows posture, so the pane never crowds the
// transcript off a short terminal.
const maxPickerRows = 8

// pickerHint is the one-line key legend shown at the foot of the overlay, for the three kinds that
// MOVE this session (/model, /server) — "switch" is the honest verb only where accepting a row
// re-points the session at something else.
//
// Every variant LEADS with "type to filter", because that is the one key nobody can guess: there is
// no activation key to name (pickerKey — any printable key types) and letters announce themselves
// nowhere else the way "↑/↓" and "esc" do. What follows the segment is what differs by kind.
const pickerHint = "type to filter · ↑/↓ select · ⏎ switch · esc close"

// pickerHintFor names what ⏎ does on the open kind. /schedule's two popups answer a question rather
// than move anything (the Schedule is created after the LAST of them), and /schedule-stop's ⏎ ends
// something — three verbs, because a legend that promised a switch would be wrong on all three.
func pickerHintFor(k pickerKind) string {
	switch k {
	case pickerCycle, pickerScheduleMode:
		return "type to filter · ↑/↓ select · ⏎ choose · esc close"
	case pickerScheduleStop:
		return "type to filter · ↑/↓ select · ⏎ stop · esc close"
	case pickerKeyMigration:
		return keyMigrationHint
	}
	return pickerHint
}

// currentRowCell marks the server row the session is already on. It is plain text, not styling:
// the popup module takes rows escape-stripped and styles them whole (faint, or the highlight bar on
// the selection), so a per-fragment style could not survive its truncation. It is a CELL of its own
// in /server's column schema rather than a suffix on the endpoint, so the "·" of every marked row
// lands in one column; the space that used to separate it is the popup module's gutter now.
//
// It is /server's alone. /model does not mark what the session is bound to — it does not OFFER it
// (offeredModels, offerableProfiles), because a row that switches nothing is not a choice; a server
// list is read to see where the session is as much as to move it, so there the mark stays.
const currentRowCell = "· current"

// modelUsage and serverUsage are the one-line grammars a mistyped verb earns, so surplus arguments
// teach the two working forms instead of vanishing (the confineUsage posture). /model names both of
// the things its one argument can be, because which one it takes is decided by whether this host has
// a launcher rather than by anything the human typed.
const (
	modelUsage  = "usage: /model [profile|model-id]"
	serverUsage = "usage: /server [name]"
)

// noServersNote is the one line /server owes when there is nowhere to switch to. An empty list and
// an unwired seam are deliberately worded the same: they are one situation for the human — this
// build was started without alternatives — and two sentences would only invite them to drift.
const noServersNote = "no servers configured — add a servers: block to config.yaml"

// runModelCommand drives the /model verb — the one "serve me something else" verb — in both its
// forms and over both its offerings. Which offering answers is decided ONCE, here, by whether a
// launcher host is wired: with a launcher, the Launch profiles it defines are what this host can
// be made to serve, and that supersedes the advertised list rather than sitting beside it (the
// owner's decision, 2026-07-29 — the two lists describe the same world, and a verb per list made the
// smaller one the default answer). Without a launcher, what the server already advertises is all
// there is, and a multi-model or remote server keeps exactly the picker it had.
//
// Surplus arguments are refused before either branch: the grammar belongs to the verb, not to the
// offering behind it.
func (m Model) runModelCommand(args []string) (tea.Model, tea.Cmd) {
	if len(args) > 1 {
		return m.pickerNote(modelUsage)
	}
	if m.opts.Launcher != nil {
		return m.pickLaunchProfile(args)
	}
	return m.pickAdvertisedModel(args)
}

// pickAdvertisedModel is /model over what the server itself says it serves — the offering on every
// host without a launcher. Bare, it opens the picker; with one argument it takes that model id
// directly.
//
// The degrade ladder is asked FIRST and for both forms, because it is about whether this session can
// switch models at all — an argument form that reached the accept path with no rebind seam would
// move nothing and say nothing, which is the one outcome a command must never have.
func (m Model) pickAdvertisedModel(args []string) (tea.Model, tea.Cmd) {
	if note, blocked := m.modelSwitchBlocked(); blocked {
		return m.pickerNote(note)
	}
	if len(args) == 1 {
		// Matched against the WHOLE offering rather than the offered rows: the human named a model
		// instead of picking one, and "already bound to it" is a truer answer than calling it unknown.
		for _, offered := range m.hb.models {
			if offered.ID == args[0] {
				return m.bindPickedModel(offered.ID, offered.ContextWindow)
			}
		}
		return m.pickerNote(fmt.Sprintf(
			"unknown model %q — /model with no argument lists what the server serves", args[0]))
	}
	if len(m.offeredModels()) == 0 {
		return m.pickerNote(noOtherModelNote)
	}
	m.picker = picker{open: true, kind: pickerModel}
	m.layout()
	return m, nil
}

// noOtherModelNote is what a server serving exactly what the session is bound to earns: the rung
// below modelSwitchBlocked's "nothing advertised yet", reached once the current binding is excluded
// from the offering. A note and no overlay, like every other degrade — an empty pane would say less.
const noOtherModelNote = "the server serves no other model"

// offeredModels is what the model picker lists: everything the server advertises EXCEPT the model
// this session is already bound to. Switching to the model you are on is not a choice, and offering
// it is what made the overlay a one-row menu on a single-model server.
//
// It is DERIVED, like the rows themselves, so a beat landing under an open picker moves the
// exclusion with the offering — a server that starts advertising a second model grows a row, and one
// that rebinds under the session loses the row it just became.
func (m Model) offeredModels() []heartbeat.ModelSummary {
	offered := make([]heartbeat.ModelSummary, 0, len(m.hb.models))
	for _, model := range m.hb.models {
		if model.ID == m.opts.Model {
			continue
		}
		offered = append(offered, model)
	}
	return offered
}

// modelSwitchBlocked reports the honest line the ADVERTISED offering owes when there is nothing to
// pick from, in the order the reasons stack: no monitor at all, a server that is not answering, a
// display-frozen heartbeat (no rebind seam), and a server that has answered but advertised nothing.
// Each is a note and no overlay — an empty pane would be a worse answer than the sentence explaining
// it. It is the launcher-less branch's ladder alone: every rung is about what this server has told
// us, and a Launch profile is read from a config file rather than observed (pickLaunchProfile).
func (m Model) modelSwitchBlocked() (string, bool) {
	switch {
	case !m.observesUpstream():
		return "/model needs the upstream monitor — not wired", true
	case m.hb.offline:
		// The offline facts are already worded once (the endpoint, and why the last beat failed);
		// saying them a second way here would only invite the two to drift.
		return m.upstreamBlockNote(), true
	case !m.appliesRebinds():
		return "model switching is unavailable — the display is read-only", true
	case len(m.hb.models) == 0:
		return "the server has not advertised any models yet", true
	}
	return "", false
}

// runServerCommand drives the /server verb in both its forms: bare, it opens the picker over the
// servers the binary assembled; with one argument it takes that server by name. Surplus arguments
// are a usage note.
//
// The degrade is asked first and for both forms, exactly as /model's ladder is and for the same
// reason: an argument form reaching the accept path with no switch seam would move nothing and say
// nothing. Unlike /model it consults neither the heartbeat nor the offline state — where the session
// can go is config, not an observation, and a server switch is the one useful thing to do WHILE the
// current server is unreachable.
func (m Model) runServerCommand(args []string) (tea.Model, tea.Cmd) {
	if len(args) > 1 {
		return m.pickerNote(serverUsage)
	}
	servers := m.servers()
	if !m.serverActs().CanSwitch || len(servers) == 0 {
		return m.pickerNote(noServersNote)
	}
	if len(args) == 1 {
		if choice, ok := serverNamed(servers, args[0]); ok {
			return m.switchToServer(choice)
		}
		return m.pickerNote(fmt.Sprintf(
			"unknown server %q — configured: %s", args[0], serverNameList(servers)))
	}
	m.picker = picker{open: true, kind: pickerServer,
		listSurface: listSurface{listCursor: listCursor{selected: m.currentServerRow()}}}
	m.layout()
	return m, nil
}

// servers is the switchable Upstreams as they stand RIGHT NOW ([ServerHost.List], asked per draw):
// the one read every surface that offers a server goes through, so the picker, the pre-bound ask and the
// settings pane's popup all offer what the `servers:` block says at the moment they are drawn rather
// than what it said at launch. An unwired provider is an empty list — the nil-seam degrade every
// other seam takes.
func (m Model) servers() []ServerChoice {
	if m.opts.Server == nil {
		return nil
	}
	return m.opts.Server.List()
}

// serverActs is what the Upstream host says it can do ([ServerHost.Acts]), with the unwired host
// answering for a nil one: the zero [ServerActs] is "none of it", so every gate reads one flag
// instead of a nil check and a call.
func (m Model) serverActs() ServerActs {
	if m.opts.Server == nil {
		return ServerActs{}
	}
	return m.opts.Server.Acts()
}

// serverNamed is the choice called name, and whether the list still holds one — the resolution both
// "/server <name>" and the settings pane's popup make before anything is asked to move, so a name
// that left the list between the offer and the accept is answered rather than acted on.
func serverNamed(servers []ServerChoice, name string) (ServerChoice, bool) {
	for _, choice := range servers {
		if choice.Name == name {
			return choice, true
		}
	}
	return ServerChoice{}, false
}

// currentServerRow is the row the server picker opens on: the one this session is on, identified by
// NAME — the same comparison the "· current" mark is drawn by. The name IS the entry's identity in
// the binary's `servers:` list (ADR 0036 decision 1), and [Options.HostAlias] is exactly the bound
// entry's name on every start shape, so two entries that share one endpoint are told apart here
// rather than conflated onto whichever of them the list happens to hold first.
func (m Model) currentServerRow() int {
	for i, choice := range m.servers() {
		if choice.Name == m.opts.HostAlias {
			return i
		}
	}
	return 0
}

// serverNameList names the switchable servers for the unknown-argument note, in the order the
// picker lists them.
func serverNameList(servers []ServerChoice) string {
	names := make([]string, 0, len(servers))
	for _, choice := range servers {
		names = append(names, choice.Name)
	}
	return strings.Join(names, ", ")
}

// switchToServer is the accept path both forms of /server share — a highlighted row and
// "/server <name>". It closes the overlay, asks the binary to move the session
// ([ServerHost.Switch], synchronously on the Update loop: it mutates the engine and constructs a
// client, and opens no connection of its own), and folds what came back.
//
// The seam is validate-then-commit all the way down, so an error means nothing moved and the note
// is the whole of the answer. A success is already true by the time it returns, which is why
// [Model.foldServerSwitch] can state it rather than attempt it.
//
// Choosing the server the session is already on is answered rather than ignored, the already-bound
// posture: an explicit act deserves a reply, and re-switching would tear down a live binding to
// arrive back where it started. It is still a CHOICE, though, so it is offered to the recording seam
// on the same terms a move is: naming where you already are is the only way to pin the server
// start-up put you on, and refusing to record it would leave that human no route to the key at all.
// "Already on" is a question about the ENTRY, not about the URL: a sibling entry pointing at the
// same endpoint is a different entry — its own key source and its own recorded pin — so picking it
// is a real switch, and the rebinding it costs is the whole point of having asked for it.
//
// A PRE-BOUND session takes the same accept one step lower down (ADR 0036 decision 3): there is no
// engine to move, so the choice CONSTRUCTS one ([Model.bindToServer], prebound.go). The branch is
// here rather than in the two callers so both forms of the verb — the highlighted row and
// "/server <name>" — reach it, and neither can be the one that still tries to switch.
func (m Model) switchToServer(choice ServerChoice) (tea.Model, tea.Cmd) {
	m.picker = picker{}
	if m.prebound() {
		return m.bindToServer(choice)
	}
	if choice.Name == m.opts.HostAlias {
		m.transcript.addNote("already on " + choice.Name + " (" + choice.Endpoint + ")")
		// The recording is stated on a line of its own here, where the move's own note (which carries
		// it as savedChoiceClause) is not there to hang it on.
		record := recordServerChoice(m.opts.Server, choice.Name)
		if record.saved {
			m.transcript.addNote(serverSavedNote)
		}
		record.warn(&m.transcript)
		m.layout()
		return m, nil
	}
	from := hostDisplay(m.opts) // the label the footer used for the old server, captured before it moves
	result, err := m.opts.Server.Switch(choice.Name)
	if err != nil {
		m.transcript.addNote("could not switch server: " + err.Error())
		m.layout()
		return m, nil
	}
	// The move is now true, so it is also the choice this human should not have to make again: the
	// name goes to the recording seam, which writes it when it belongs to a configured entry and
	// skips it silently when it does not (ADR 0036 decision 2, [ServerHost.RecordChoice]).
	return m.foldServerSwitch(from, result, recordServerChoice(m.opts.Server, choice.Name))
}

// serverSavedNote is what a RECORDED re-selection says: the `server:` key now names the entry this
// session is already on, so the next session starts here without being asked. It states as a line
// what [savedChoiceClause] states as a clause, for the one path that has no move to hang a clause on
// — the same key, named the same way, because the key is what the human will find in config.yaml.
const serverSavedNote = "server: saved — this entry starts the next session"

// ----------------------------------------------------------------------------
// /model over the Launch profiles — the launcher's offering (ADR 0029 D3)
// ----------------------------------------------------------------------------

// loadPickerTitle names the offering and whose it is. It is /model's own title with the launcher in
// place of the host, because that is the honest qualifier here: the advertised offering belongs to
// one server, while a Launch profile carries its own address — and the rows say so when that address
// differs from the session's.
const loadPickerTitle = "switch model — llama-launcher"

// runningRowCell marks a profile discovery attributes to a live instance right now — the
// currentRowCell posture (a plain-text cell of its own, because the popup module styles rows whole
// and aligns them by column), for a fact that is about the world rather than about this session: a
// running profile is not necessarily the one this session is talking to.
const runningRowCell = "· running"

// noProfilesNote is what a launcher config with nothing in it earns. It names no path, deliberately:
// where the launcher's config lives is the composition root's knowledge (ADR 0029 D1/D4), and a
// renderer that guessed a path would eventually name the wrong one.
const noProfilesNote = "no launch profiles defined — add profiles to the llama-launcher config"

// pickLaunchProfile is /model over the Launch profiles the launcher's config defines — the offering
// on a host where llama-launcher is wired. Bare, it opens the picker; with one argument it activates
// that profile by name.
//
// The rows are read at OPEN and for BOTH forms, which is the whole of ADR 0029 D4's freshness rule:
// the seam re-reads the launcher's config from disk, so a profile added seconds ago in the
// launcher's own TUI is offered here — and the argument form is checked against the same list the
// picker would have shown, so the two forms can never disagree about what exists.
//
// The ladder here is about the launcher rather than about the server, which is why it consults
// nothing the heartbeat observed — not even the offline rung: bringing a server up is the one useful
// act while the current one is down, exactly as /server's is (and what displacing a running server
// or a loaded model costs is the LAUNCHER's own config to answer, auto_stop_server and auto_unload
// included; apogee adds no policy of its own). Two things can be missing instead: the config the
// seam reads, and the profiles it was supposed to hold. Each is one sentence and no overlay.
func (m Model) pickLaunchProfile(args []string) (tea.Model, tea.Cmd) {
	profiles, err := m.opts.Launcher.Profiles()
	if errors.Is(err, ErrNoLauncher) {
		// The integration is wired but switched OFF right now — `llama-launcher: off`, or a key the
		// pane cleared mid-session (ADR 0037). That is not a failure of /model: it is the host having
		// no launcher, which is answered by the models the server itself advertises. The nil-host
		// branch above says the same thing about a machine that never wired one.
		return m.pickAdvertisedModel(args)
	}
	if err != nil {
		// The one failure that sinks the list — no config at the configured path, a config that will
		// not parse. It is the launcher's own words about a file on this machine, so it is
		// escape-stripped like every other untrusted line reaching the terminal.
		return m.pickerNote(stripEscapes(err.Error()))
	}
	if len(profiles) == 0 {
		return m.pickerNote(noProfilesNote)
	}
	if len(args) == 1 {
		// Matched against every DEFINED profile rather than the offered rows, the advertised form's
		// posture: a profile that is already loaded is still configured, and answering "unknown" for a
		// name the config plainly holds would be a lie. Re-activating it is the launcher's own
		// idempotent no-op, which says so.
		for _, choice := range profiles {
			if choice.Name == args[0] {
				return m.startProfileLoad(choice.Name)
			}
		}
		return m.pickerNote(fmt.Sprintf(
			"unknown launch profile %q — configured: %s", args[0], profileNameList(profiles)))
	}
	offered := offerableProfiles(profiles, sessionAddr(m.opts.Endpoint))
	if len(offered) == 0 {
		return m.pickerNote(onlyProfileLoadedNote)
	}
	m.picker = picker{open: true, kind: pickerLoad, profiles: offered}
	m.layout()
	return m, nil
}

// onlyProfileLoadedNote is what a launcher whose every defined profile is the one already serving
// this session earns — the launcher-side twin of noOtherModelNote, and the rung below noProfilesNote:
// profiles exist, but none of them is somewhere else to go.
const onlyProfileLoadedNote = "the only launch profile is already loaded"

// offerableProfiles drops the Launch profile this session is ALREADY served by: one the launcher
// attributes to a live instance at the very address the session is talking to. Every other profile
// stays, running ones included — a profile running on another port is precisely the switch this verb
// exists for, and its row says where it lives (elsewherePort).
//
// Two unknowns exclude nothing, both in the same direction: a session endpoint that would not parse
// (sessionAddr's honest ""), and attribution the bridge could not make unambiguously (which reaches
// here as no profile marked running at all). An address we cannot name is no evidence that a profile
// shares it, and dropping a row on that guess would hide the very profile the human came to load.
func offerableProfiles(profiles []LaunchProfileChoice, here string) []LaunchProfileChoice {
	if here == "" {
		return profiles
	}
	offered := make([]LaunchProfileChoice, 0, len(profiles))
	for _, choice := range profiles {
		if choice.Running && choice.Addr == here {
			continue
		}
		offered = append(offered, choice)
	}
	return offered
}

// profileNameList names the defined profiles for the unknown-argument note, in the order the picker
// lists them (the launcher's own display order, favourites first).
func profileNameList(profiles []LaunchProfileChoice) string {
	names := make([]string, 0, len(profiles))
	for _, choice := range profiles {
		names = append(names, stripEscapes(choice.Name))
	}
	return strings.Join(names, ", ")
}

// launchProfileRows is one row per OFFERED Launch profile (the profile already serving this session
// is not among them — offerableProfiles): the name the human gave it in the launcher's config, which
// is also the /model argument and the footer alias afterwards, the backend it runs on, the context
// window it was configured with when it states one, the port it would serve at when that is NOT
// where this session is pointed, and the running mark.
//
// The port is shown only for a profile that lives somewhere else, because that is the moment it
// matters: loading it will move the session. A profile on the session's own server needs no address —
// it is the address the footer is already showing.
//
// Those five facts are five CELLS in a fixed schema — ["name", "— backend", "· 32k", "(:8080)",
// "· running"] — rather than one concatenated label: a tier a profile does not state is an empty
// cell, which still pads, so a nameless backend or an unstated window cannot slide the columns after
// it sideways and the pane reads as a table. Each separator leads the cell it introduces, so the
// "—", the "·" and the "(" line up down the pane too, and a tier NO offered profile states collapses
// away entirely (layoutPopupRow).
//
// EVERY cell built from the launcher's text is escape-stripped here — the name, the backend and the
// port alike. The port needs it as much as the other two: net.SplitHostPort rejects "[" and "]" but
// passes an ESC byte straight through, so an address like "1.2.3.4:\x1bc9999" would otherwise carry a
// live terminal reset into the pane, which the popup module (ANSI-preserving truncation, no strip of
// its own) would faithfully paint.
func (m Model) launchProfileRows() []popupRow {
	here := sessionAddr(m.opts.Endpoint)
	rows := make([]popupRow, 0, len(m.picker.profiles))
	for _, choice := range m.picker.profiles {
		backend, window, port, running := "", "", "", ""
		if choice.Backend != "" {
			backend = "— " + stripEscapes(choice.Backend)
		}
		if tokens := format.Tokens(choice.ContextWindow); tokens != "" {
			window = "· " + tokens
		}
		if elsewhere := elsewherePort(choice.Addr, here); elsewhere != "" {
			port = "(" + stripEscapes(elsewhere) + ")"
		}
		if choice.Running {
			running = runningRowCell
		}
		rows = append(rows, popupRow{stripEscapes(choice.Name), backend, window, port, running})
	}
	return rows
}

// sessionAddr reduces the session's endpoint URL to the host:port a Launch profile's address is
// spelled in, for the comparison the port marker is drawn by. An endpoint that will not parse
// reduces to nothing, which shows every row its port — the safe direction, since an unknown session
// address is no evidence that a profile shares it.
func sessionAddr(endpoint string) string {
	u, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return ""
	}
	return u.Host
}

// elsewherePort is the "(:8081)" a row earns when the profile would serve at an address other than
// the session's. An address the launcher could not resolve carries no marker: there is nothing
// truthful to show, and ":0" would read as a fact.
//
// What it returns is still the LAUNCHER's text and is not sanitized: net.SplitHostPort validates the
// shape of an address, not its bytes. The caller escape-strips the cell it builds from this.
func elsewherePort(addr, here string) string {
	if addr == "" || addr == here {
		return ""
	}
	_, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		return ""
	}
	return ":" + port
}

// pickerNote is the "one honest line, no overlay" answer every /model and /server degrade takes: a
// transcript note and an unchanged session.
func (m Model) pickerNote(note string) (tea.Model, tea.Cmd) {
	m.transcript.addNote(note)
	m.layout()
	return m, nil
}

// pickerKey routes a keypress while the picker is open (at idle, and — for /schedule's kinds, whose
// verb is live mid-run — while a worker works). Every key of it is the shared list contract
// (listKey, listsurface.go): ↑/↓ move the highlight (wrapping, the sessionBrowser posture), ⏎ takes
// the highlighted row, esc closes, and everything PRINTABLE types — the key's runes extend the
// filter that prunes the rows. It always fully consumes the key — the picker is modal, so a key the
// surface hands back (listUnclaimed) is swallowed here rather than routed on.
//
// What is left in this file is the two acts a picker's keys MEAN: closing the overlay clears the
// half-built Schedule and the migration round with it (the whole-struct zeroing), and accepting
// resolves the highlighted row through the kind that is open (acceptPicker).
func (m Model) pickerKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	verdict, cmd := m.listKey(&m.picker.listSurface, msg, m.pickerOfferingRows(), listWrapsAround)
	switch verdict {
	case listCloses:
		m.picker = picker{}
		m.layout()
		return m, nil
	case listAccepts:
		return m.acceptPicker()
	case listSwallowed, listUnclaimed:
		// Both end the same way here: the surface either spent the key or found no use for it, and a
		// modal with no verbs of its own swallows whatever is left.
	}
	return m, cmd
}

// pickerWheel walks the picker's highlight one row per notch while the pointer is over the pane —
// the keyboard's ↑/↓ under the wheel, against the FILTERED view pickerKey and the painter already
// share (pickerFilteredView, counted by pickerCount), so a notch and an arrow can never disagree
// about which row is highlighted and neither can move the human onto a row the filter has pruned
// away. handled is false anywhere else, which leaves the notch to the transcript scrolling above and
// behind the pane.
//
// Inside the rectangle it is ALWAYS handled, whatever kind is open: the picker is modal (its claim
// in keyClaimOrder, model.go), and a notch over it must never reach the transcript behind it. No
// kind is special-cased either — the rows come from pickerOfferingRows, which already answers per
// kind — so /schedule's two-step flow and the start-up key migration walk the offering the CURRENT
// step is showing, and a notch changes nothing else about the round: the half-built Schedule and the
// entries still to be asked about are the accept's business, not the highlight's.
//
// It CLAMPS at the ends where these keys WRAP, and the clamp is the list module's own rather than
// this pane's ([listCursor.wheel], listsurface.go): ↑/↓ walk a list as a cycle, while a wheel is a
// scroll.
func (m Model) pickerWheel(msg tea.MouseWheelMsg) (Model, bool) {
	if !m.picker.open {
		// Asked before the frame is composed, because every wheel notch asks: with no pane up there is
		// nothing to place, and composing the frame's overlays to learn that would put a render on the
		// path of a notch the pane has no part in (browserWheel, sessions.go).
		return m, false
	}
	y0, h, ok := m.frameSpans().pane(panePicker)
	if !ok || msg.Y < y0 || msg.Y >= y0+h {
		return m, false
	}
	m.picker.wheel(msg, m.pickerCount())
	return m, true
}

// acceptPicker resolves ⏎ on the highlighted row, by kind. The highlight indexes the FILTERED rows,
// so it is mapped back to the offering it names (pickerView.offeringIndex) before any underlying
// list is touched: with a filter set, "the third painted row" and "the third model advertised" are
// different rows, and taking the second would move the session somewhere the human never saw. The
// caller has already established that there is a row to take (pickerKey) and the selection is
// clamped, so the mapping fails only on a list that emptied between the two reads.
func (m Model) acceptPicker() (tea.Model, tea.Cmd) {
	offered, ok := m.pickerFilteredView().offeringIndex(m.picker.selected)
	if !ok {
		return m, nil
	}
	switch m.picker.kind {
	case pickerModel:
		picked := m.offeredModels()[offered]
		return m.bindPickedModel(picked.ID, picked.ContextWindow)
	case pickerServer:
		// The one kind whose list is asked per draw (ServerHost.List): the rows are re-read here
		// rather than trusted from the frame that drew them, so a `servers:` block that shrank under the
		// open overlay costs the accept and not the process.
		servers := m.servers()
		if offered >= len(servers) {
			return m, nil
		}
		return m.switchToServer(servers[offered])
	case pickerLoad:
		return m.startProfileLoad(m.picker.profiles[offered].Name)
	case pickerCycle:
		return m.acceptCycle(scheduleCycles[offered])
	case pickerScheduleMode:
		return m.acceptScheduleMode(scheduleModes[offered])
	case pickerScheduleStop:
		return m.acceptScheduleStop(offered)
	case pickerKeyMigration:
		return m.acceptKeyMigration(offered)
	}
	return m, nil
}

// bindPickedModel is the accept path both forms of /model share — a highlighted row and
// "/model <id>". It closes the overlay and drives the EXISTING rebind orchestration, so every
// consequence (the seam call, the fail-once refusal, the restated start-up box, rebindNote's
// wording, the notices, the unknown-window honesty) is the heartbeat's own and no second set of
// strings exists to drift from it.
//
// The pick is recorded as the last observation BEFORE the seam is called, exactly as
// [Model.observeBinding] records one: without that, the very next beat on a multi-model server
// still resolving the old id would measure the picked model as a fresh change and bind it away
// again within one Interval.
//
// NAMING the model the session is already on is answered rather than ignored: an explicit act
// deserves an answer, where rebindNote's "" contract is about the observations nobody asked for. No
// picked ROW reaches it any more — that row is not offered — but "/model <id>" can still name one.
//
// It is also the ONE place a model pick is RECORDED (remember-model), and that is why the recording
// hangs off this function rather than off the rebind orchestration underneath it: [Model.applyRebind]
// is shared with the heartbeat, and a passive observation is news about the server rather than a
// choice — recording there would write config nobody asked for. Only a model the session is actually
// ON is offered to the seam: a refused rebind leaves the file describing the model still bound, and
// naming the bound one records at once, which is the human's only way to pin what the heartbeat (or
// start-up) chose for them.
func (m Model) bindPickedModel(id string, window int) (tea.Model, tea.Cmd) {
	m.picker = picker{}
	if id == m.opts.Model {
		m.transcript.addNote("already bound to " + displayModel(id))
		// Naming the model already bound binds nothing, but it is still a pick, and the only one a human
		// whose heartbeat (or start-up) put them here can make: the record runs on the bound path's own
		// terms below, and every skip that is not the renderer's — remember off, an unlisted entry, a
		// launcher-fronted one — stays the seam's to make silently.
		record := recordModelChoice(m.opts.RecordModelChoice, id)
		if record.saved {
			m.transcript.addNote(modelSavedNote)
		}
		record.warn(&m.transcript)
		m.layout()
		return m, nil
	}
	m.hb.observedModel, m.hb.observedWindow = id, window
	next, _ := m.applyRebind(rebindIntent{model: id, window: window})
	if next.opts.Model == id {
		record := recordModelChoice(next.opts.RecordModelChoice, id)
		if record.saved {
			next.transcript.addNote(modelSavedNote)
		}
		record.warn(&next.transcript)
	}
	next.layout()
	return next, nil
}

// modelSavedNote is what a RECORDED pick says: the entry this session is on now names this model, so
// the next session on that server starts here without being asked. It names the key the way
// savedChoiceClause does, because the key is what the human will find in config.yaml — but it is a
// line of its own rather than a clause on the move's, because the line the move writes is
// [rebindNote]'s and that wording is shared with every observation nobody asked for.
const modelSavedNote = "model: saved — this server starts on it next time"

// recordModelChoice persists the model this session just bound as the one its entry comes back on,
// through the seam the binary backs. It is [recordServerChoice] in every respect that matters —
// called with what the human chose, answered with whether the binary WROTE, and never consulted
// about which entry that is or whether the file may carry the key — because the two recordings are
// one feature seen from the two classes of server apogee talks to.
//
// A failed write is a warning and nothing more: the session is already bound, and the recording is
// best-effort persistence of something that is already true. An unwired seam records nothing and says
// nothing, which is the pre-remember-model behaviour every hand-built Options keeps.
func recordModelChoice(record func(model string) (bool, error), id string) choiceRecord {
	if record == nil || id == "" {
		return choiceRecord{}
	}
	saved, err := record(id)
	if err != nil {
		return choiceRecord{warning: "could not record the model choice: " + stripEscapes(err.Error())}
	}
	return choiceRecord{saved: saved}
}

// pickerCount is how many rows the open kind has RIGHT NOW — the count of the FILTERED view, read
// off the state the rows are derived from rather than off a captured list, and the one number the
// selection is clamped and wrapped against. Counting the painted rows rather than the offering
// behind them is what keeps the highlight inside what the pane actually shows.
func (m Model) pickerCount() int {
	return len(m.pickerFilteredView().rows)
}

// pickerFilteredView derives the open kind's rows and prunes them by the overlay's filter
// ([listSurface.view]). It is derived PER FRAME like the rows themselves, so a beat landing under an
// open filtered picker re-derives the offering, re-applies the filter and re-clamps the selection in
// one move — the unfiltered posture, unchanged.
func (m Model) pickerFilteredView() pickerView {
	return m.picker.view(m.pickerOfferingRows())
}

// ----------------------------------------------------------------------------
// Rendering
// ----------------------------------------------------------------------------

// renderPicker paints the open picker through the shared list surface (renderFilterList,
// listsurface.go):
// a titled, bordered pane spanning the full window width holding the filter line, the rows and a key
// legend, the selected row highlighted. It returns "" when the picker is closed, so View treats it
// exactly like the /sessions browser's slot.
//
// What this file states is what only this pane knows — its slot in the frame, its name, its legend,
// its taste in rows and the rows themselves. The filter line, its two blanks, the budget claim all
// three cost and the trade on a window too short for everything are the surface's
// (renderFilterList).
func (m Model) renderPicker() string {
	if !m.picker.open {
		return ""
	}
	rows := m.pickerRows()
	return m.renderFilterList(m.picker.filter, listContent{
		pane:     panePicker,
		title:    m.pickerTitle(),
		hint:     pickerHintFor(m.picker.kind),
		rowCap:   maxPickerRows,
		rows:     rows,
		selected: m.picker.highlight(len(rows)),
	})
}

// pickerTitle names what is being switched and, for the model picker, on which host — the same
// label the footer and the start-up box use (hostDisplay), so a session with two servers configured
// can never mistake which one's offering it is looking at. The server picker needs no such
// qualifier: its rows name the hosts themselves.
func (m Model) pickerTitle() string {
	switch m.picker.kind {
	case pickerModel:
		return "switch model — " + hostDisplay(m.opts)
	case pickerServer:
		return "switch server"
	case pickerLoad:
		return loadPickerTitle
	case pickerCycle:
		return "schedule — how often"
	case pickerScheduleMode:
		return "schedule — autonomy mode"
	case pickerScheduleStop:
		return "stop a schedule"
	case pickerKeyMigration:
		return m.keyMigrationTitle()
	}
	return ""
}

// pickerRows is the row list the popup module PAINTS: the open kind's offering with the overlay's
// filter applied (pickerFilteredView). It is the rows consumer of the one filtered view, so what the
// pane shows, what pickerCount counts and what acceptPicker takes can never be three different
// lists.
func (m Model) pickerRows() []popupRow {
	return m.pickerFilteredView().rows
}

// pickerOfferingRows composes the kind's FULL row list — the offering before the filter prunes it.
// Each kind emits its own fixed column schema and the module owns the alignment, along with the
// marker, the highlight, the truncation and the scroll windowing; cells arrive plain and
// escape-stripped, as its contract requires — a model id is the SERVER's text and a profile name the
// LAUNCHER's, so both are sanitized here rather than trusted.
func (m Model) pickerOfferingRows() []popupRow {
	switch m.picker.kind {
	case pickerModel:
		return m.modelRows()
	case pickerServer:
		return m.serverRows()
	case pickerLoad:
		return m.launchProfileRows()
	case pickerCycle:
		return cycleRows()
	case pickerScheduleMode:
		return scheduleModeRows(m.opts.ScheduleAutoBlocked)
	case pickerScheduleStop:
		return scheduleStopRows(m.liveSchedules())
	case pickerKeyMigration:
		return keyMigrationRows(m.opts.KeyMigration.StoreName)
	}
	return nil
}

// modelRows is one row per OFFERED model (offeredModels — everything advertised but the binding this
// session already has): the id as the footer renders it (displayModel, so the pane and the chrome
// beside it can never name the same model two different ways), and its context window when the
// server named one. Two cells — ["model", "— 32k"] — so the windows line up in one column however
// long the ids beside them run, and a model whose window the server did not state leaves an empty
// cell rather than a short row. No row needs a "· current" mark, because the current one is not
// among them.
func (m Model) modelRows() []popupRow {
	offering := m.offeredModels()
	rows := make([]popupRow, 0, len(offering))
	for _, offered := range offering {
		window := ""
		if tokens := format.Tokens(offered.ContextWindow); tokens != "" {
			window = "— " + tokens
		}
		rows = append(rows, popupRow{stripEscapes(displayModel(offered.ID)), window})
	}
	return rows
}

// serverRows is one row per configured server: the name the human gave it (which is also the switch
// argument and the footer alias afterwards) with the endpoint it stands for spelled out beside it,
// and the "· current" mark on the entry this session is BOUND to (by name, [Model.currentServerRow]'s
// comparison). The endpoint is shown rather than hidden behind the alias because a switch is exactly
// the moment a name is worth resolving to a URL — but it is display here and never identity, so a
// sibling entry sharing that URL is drawn unmarked and can be switched onto.
// Three cells — ["name", "— endpoint", "· current"] — so the endpoints start at one column whatever
// the aliases measure; the mark is an empty cell on every server but one, and when the session is on
// none of them that third column collapses away.
func (m Model) serverRows() []popupRow {
	servers := m.servers()
	rows := make([]popupRow, 0, len(servers))
	for _, choice := range servers {
		current := ""
		if choice.Name == m.opts.HostAlias {
			current = currentRowCell
		}
		rows = append(rows, popupRow{
			stripEscapes(choice.Name), "— " + stripEscapes(choice.Endpoint), current,
		})
	}
	return rows
}
