package tui

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/heartbeat"
)

// ----------------------------------------------------------------------------
// The shared single-select picker overlay (/model and /server)
// ----------------------------------------------------------------------------
//
// The /sessions browser's simpler sibling: a modal list with one highlight, ⏎ to take the
// highlighted row, esc to close, painted through the shared popup module (renderPopup) so it shares
// the browser's chrome and right edge. It answers ONE question — which of the things the session
// could be pointed at should it be pointed at now — so the state is three plain values and the
// verbs differ only by a [pickerKind].
//
// Rows are DERIVED at render time from the state they describe, never captured at open. That is
// what lets a beat landing under an open /model picker refresh the offering in place (the
// selection is clamped, the sessionBrowser.clampSelection posture) instead of leaving the human
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
// up: the binary moves the whole Upstream behind the unchanged seams ([Options.SwitchServer]) and
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
// Whichever offering /model lists, what the session is already on is NOT in it: a row that switches
// nothing is not a choice, and pickerHint's "⏎ switch" would be a promise it could not keep. /server
// keeps its "· current" row instead, because where the session IS is half of what a server list is
// read for.

// pickerKind names WHICH offering an open picker is listing. It is an enum rather than a callback
// field on the state so the Model keeps holding plain values only (ADR 0011) — every kind-specific
// answer (the rows, the title, the accept) is one switch away.
type pickerKind int

const (
	pickerModel  pickerKind = iota // the models the Upstream advertises — /model without a launcher
	pickerServer                   // the servers config.yaml names — /server, over m.opts.Servers
	pickerLoad                     // the Launch profiles the launcher defines — /model with one
)

// picker is the overlay's inline state on the Model. Its zero value is "closed", so it lives inline
// in the value-copied Model like sessionBrowser and autocompleteState (ADR 0011). selected indexes
// the rows the current kind derives; it is clamped rather than trusted, because the list underneath
// it can change while the overlay is open.
type picker struct {
	open     bool
	kind     pickerKind
	selected int
	// profiles are the Launch-profile rows, and the one offering that is NOT derived at render time.
	// The other two describe Model state (the advertised models, the configured servers) and so can be
	// re-read every frame; a Launch profile lives in the launcher's config FILE, behind a seam that
	// re-reads it from disk (ADR 0029 D4). Reading once per open is what makes those rows fresh —
	// a profile added in the launcher's own TUI a moment ago is offered here — without turning a
	// keypress into a file read. A plain slice of plain values, safe in the copied Model.
	profiles []LaunchProfileChoice
}

// maxPickerRows caps how many rows the overlay shows at once; a longer list scrolls a window around
// the selection (popupRowWindow), the maxSessionRows posture, so the pane never crowds the
// transcript off a short terminal.
const maxPickerRows = 8

// pickerHint is the one-line key legend shown at the foot of the overlay.
const pickerHint = "↑/↓ select · ⏎ switch · esc close"

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
// forms and over both its offerings. Which offering answers is decided ONCE, here, by whether the
// launcher seams are wired: with a launcher, the Launch profiles it defines are what this host can
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
	if m.opts.LaunchProfiles != nil && m.opts.LoadProfile != nil {
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
	case m.opts.Heartbeat == nil:
		return "/model needs the upstream monitor — not wired", true
	case m.hb.offline:
		// The offline facts are already worded once (the endpoint, and why the last beat failed);
		// saying them a second way here would only invite the two to drift.
		return m.upstreamBlockNote(), true
	case m.opts.Rebind == nil:
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
	if m.opts.SwitchServer == nil || len(m.opts.Servers) == 0 {
		return m.pickerNote(noServersNote)
	}
	if len(args) == 1 {
		for _, choice := range m.opts.Servers {
			if choice.Name == args[0] {
				return m.switchToServer(choice)
			}
		}
		return m.pickerNote(fmt.Sprintf(
			"unknown server %q — configured: %s", args[0], serverNameList(m.opts.Servers)))
	}
	m.picker = picker{open: true, kind: pickerServer, selected: m.currentServerRow()}
	m.layout()
	return m, nil
}

// currentServerRow is the row the server picker opens on: the one this session is on, identified by
// endpoint — the same comparison the "· current" mark is drawn by, and the same one the binary used
// when it decided whether the startup endpoint still needed a row of its own.
func (m Model) currentServerRow() int {
	for i, choice := range m.opts.Servers {
		if choice.Endpoint == m.opts.Endpoint {
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
// ([Options.SwitchServer], synchronously on the Update loop: it mutates the engine and constructs a
// client, and opens no connection of its own), and folds what came back.
//
// The seam is validate-then-commit all the way down, so an error means nothing moved and the note
// is the whole of the answer. A success is already true by the time it returns, which is why
// [Model.foldServerSwitch] can state it rather than attempt it.
//
// Choosing the server the session is already on is answered rather than ignored, the already-bound
// posture: an explicit act deserves a reply, and re-switching would tear down a live binding to
// arrive back where it started.
func (m Model) switchToServer(choice ServerChoice) (tea.Model, tea.Cmd) {
	m.picker = picker{}
	if choice.Endpoint == m.opts.Endpoint {
		m.transcript.addNote("already on " + choice.Name + " (" + choice.Endpoint + ")")
		m.layout()
		return m, nil
	}
	from := hostDisplay(m.opts) // the label the footer used for the old server, captured before it moves
	result, err := m.opts.SwitchServer(choice.Name)
	if err != nil {
		m.transcript.addNote("could not switch server: " + err.Error())
		m.layout()
		return m, nil
	}
	return m.foldServerSwitch(from, result)
}

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
	profiles, err := m.opts.LaunchProfiles()
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
		if tokens := formatTokens(choice.ContextWindow); tokens != "" {
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

// pickerKey routes a keypress while the picker is open (idle only): ↑/↓ move the highlight (wrapping,
// the sessionBrowser posture), ⏎ takes the highlighted row, esc closes. It always fully consumes the
// key — the picker is modal. The count is re-derived and the selection re-clamped on every key,
// because the rows underneath can have changed since the last one.
func (m Model) pickerKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	n := m.pickerCount()
	m.picker.clampSelection(n)
	switch msg.String() {
	case "esc":
		m.picker = picker{}
		m.layout()
		return m, nil
	case "up", "ctrl+p":
		if n > 0 {
			m.picker.selected = (m.picker.selected - 1 + n) % n
		}
		return m, nil
	case "down", "ctrl+n":
		if n > 0 {
			m.picker.selected = (m.picker.selected + 1) % n
		}
		return m, nil
	case "enter":
		if n == 0 {
			return m, nil
		}
		return m.acceptPicker()
	}
	return m, nil // any other key is swallowed by the modal
}

// acceptPicker resolves ⏎ on the highlighted row, by kind. The caller has already established that
// there is a row to take (pickerKey), and the selection is clamped, so the index is safe.
func (m Model) acceptPicker() (tea.Model, tea.Cmd) {
	switch m.picker.kind {
	case pickerModel:
		picked := m.offeredModels()[m.picker.selected]
		return m.bindPickedModel(picked.ID, picked.ContextWindow)
	case pickerServer:
		return m.switchToServer(m.opts.Servers[m.picker.selected])
	case pickerLoad:
		return m.startProfileLoad(m.picker.profiles[m.picker.selected].Name)
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
func (m Model) bindPickedModel(id string, window int) (tea.Model, tea.Cmd) {
	m.picker = picker{}
	if id == m.opts.Model {
		m.transcript.addNote("already bound to " + displayModel(id))
		m.layout()
		return m, nil
	}
	m.hb.observedModel, m.hb.observedWindow = id, window
	next, _ := m.applyRebind(rebindIntent{model: id, window: window})
	next.layout()
	return next, nil
}

// pickerCount is how many rows the open kind has RIGHT NOW, read off the state the rows are derived
// from rather than off a captured list — the one number the selection is clamped and wrapped
// against.
func (m Model) pickerCount() int {
	switch m.picker.kind {
	case pickerModel:
		return len(m.offeredModels())
	case pickerServer:
		return len(m.opts.Servers)
	case pickerLoad:
		return len(m.picker.profiles)
	}
	return 0
}

// clampSelection keeps selected inside a row list that moved under the open overlay — a beat
// carrying a shorter offering, say. An empty list pins the selection at zero (renderPicker shows no
// highlight for it).
func (p *picker) clampSelection(n int) {
	switch {
	case n == 0:
		p.selected = 0
	case p.selected >= n:
		p.selected = n - 1
	case p.selected < 0:
		p.selected = 0
	}
}

// ----------------------------------------------------------------------------
// Rendering
// ----------------------------------------------------------------------------

// renderPicker paints the open picker through the shared popup module (renderPopup): a titled,
// bordered pane spanning the full window width (m.width, flush with the input box below) holding the
// rows and a key legend, the selected row highlighted. It returns "" when the picker is closed, so
// View treats it exactly like the /sessions browser's slot.
func (m Model) renderPicker() string {
	if !m.picker.open {
		return ""
	}
	rows := m.pickerRows()
	selected := -1 // no rows ⇒ no highlight (the popup module's own convention)
	if len(rows) > 0 {
		selected = clampInt(m.picker.selected, 0, len(rows)-1)
	}
	return renderPopup(m.th, popupSpec{
		title:    m.pickerTitle(),
		rows:     rows,
		selected: selected,
		hint:     pickerHint,
		maxRows:  maxPickerRows,
	}, m.width)
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
	}
	return ""
}

// pickerRows composes the FULL row list the popup module paints, by kind. Each kind emits its own
// fixed column schema and the module owns the alignment, along with the marker, the highlight, the
// truncation and the scroll windowing; cells arrive plain and escape-stripped, as its contract
// requires — a model id is the SERVER's text and a profile name the LAUNCHER's, so both are
// sanitized here rather than trusted.
func (m Model) pickerRows() []popupRow {
	switch m.picker.kind {
	case pickerModel:
		return m.modelRows()
	case pickerServer:
		return m.serverRows()
	case pickerLoad:
		return m.launchProfileRows()
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
		if tokens := formatTokens(offered.ContextWindow); tokens != "" {
			window = "— " + tokens
		}
		rows = append(rows, popupRow{stripEscapes(displayModel(offered.ID)), window})
	}
	return rows
}

// serverRows is one row per configured server: the name the human gave it (which is also the switch
// argument and the footer alias afterwards) with the endpoint it stands for spelled out beside it,
// and the "· current" mark on the server this session is on. The endpoint is shown rather than
// hidden behind the alias because a switch is exactly the moment a name is worth resolving to a URL.
// Three cells — ["name", "— endpoint", "· current"] — so the endpoints start at one column whatever
// the aliases measure; the mark is an empty cell on every server but one, and when the session is on
// none of them that third column collapses away.
func (m Model) serverRows() []popupRow {
	rows := make([]popupRow, 0, len(m.opts.Servers))
	for _, choice := range m.opts.Servers {
		current := ""
		if choice.Endpoint == m.opts.Endpoint {
			current = currentRowCell
		}
		rows = append(rows, popupRow{
			stripEscapes(choice.Name), "— " + stripEscapes(choice.Endpoint), current,
		})
	}
	return rows
}
