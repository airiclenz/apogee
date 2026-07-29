package tui

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// ----------------------------------------------------------------------------
// The shared single-select picker overlay (/model, /server and /load)
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
// /load is the third question — which model should the world be made to serve (ADR 0029 D3) — and
// the one whose accept does not finish on the Update loop: activating a Launch profile blocks for as
// long as a server takes to come up, so the accept hands off to the actuation latch (actuation.go)
// and the completion fold there ends in one of the two shapes above. Its rows are also the one
// offering read at open rather than derived per frame, because they live in the launcher's config
// file rather than in Model state.

// pickerKind names WHICH offering an open picker is listing. It is an enum rather than a callback
// field on the state so the Model keeps holding plain values only (ADR 0011) — every kind-specific
// answer (the rows, the title, the accept) is one switch away.
type pickerKind int

const (
	pickerModel  pickerKind = iota // the models the Upstream advertises — /model, over m.hb.models
	pickerServer                   // the servers config.yaml names — /server, over m.opts.Servers
	pickerLoad                     // the Launch profiles the launcher defines — /load, over m.picker.profiles
)

// picker is the overlay's inline state on the Model. Its zero value is "closed", so it lives inline
// in the value-copied Model like sessionBrowser and autocompleteState (ADR 0011). selected indexes
// the rows the current kind derives; it is clamped rather than trusted, because the list underneath
// it can change while the overlay is open.
type picker struct {
	open     bool
	kind     pickerKind
	selected int
	// profiles are the /load rows, and the one offering that is NOT derived at render time. The
	// other two describe Model state (the advertised models, the configured servers) and so can be
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

// currentRowSuffix marks the row the session is already on. It is plain text, not styling: the
// popup module takes rows escape-stripped and styles them whole (faint, or the highlight bar on the
// selection), so a per-fragment style could not survive its truncation.
const currentRowSuffix = " · current"

// modelUsage and serverUsage are the one-line grammars a mistyped verb earns, so surplus arguments
// teach the two working forms instead of vanishing (the confineUsage posture).
const (
	modelUsage  = "usage: /model [model-id]"
	serverUsage = "usage: /server [name]"
	loadUsage   = "usage: /load [profile]"
)

// noServersNote is the one line /server owes when there is nowhere to switch to. An empty list and
// an unwired seam are deliberately worded the same: they are one situation for the human — this
// build was started without alternatives — and two sentences would only invite them to drift.
const noServersNote = "no servers configured — add a servers: block to config.yaml"

// runModelCommand drives the /model verb in both its forms: bare, it opens the picker over what the
// server advertises; with one argument it takes that model id directly. Surplus arguments are a
// usage note.
//
// The degrade ladder is asked FIRST and for both forms, because it is about whether this session
// can switch models at all — an argument form that reached the accept path with no rebind seam
// would move nothing and say nothing, which is the one outcome a command must never have.
func (m Model) runModelCommand(args []string) (tea.Model, tea.Cmd) {
	if len(args) > 1 {
		return m.pickerNote(modelUsage)
	}
	if note, blocked := m.modelSwitchBlocked(); blocked {
		return m.pickerNote(note)
	}
	if len(args) == 1 {
		for _, offered := range m.hb.models {
			if offered.ID == args[0] {
				return m.bindPickedModel(offered.ID, offered.ContextWindow)
			}
		}
		return m.pickerNote(fmt.Sprintf(
			"unknown model %q — /model with no argument lists what the server serves", args[0]))
	}
	m.picker = picker{open: true, kind: pickerModel, selected: m.currentModelRow()}
	m.layout()
	return m, nil
}

// modelSwitchBlocked reports the honest line /model owes when there is nothing to pick from, in the
// order the reasons stack: no monitor at all, a server that is not answering, a display-frozen
// heartbeat (no rebind seam), and a server that has answered but advertised nothing. Each is a note
// and no overlay — an empty pane would be a worse answer than the sentence explaining it.
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

// currentModelRow is the row the picker opens on: the one the session is bound to, or the first row
// when the binding names nothing the server currently advertises (a stale pin, or a cold start that
// has not bound yet).
func (m Model) currentModelRow() int {
	for i, offered := range m.hb.models {
		if offered.ID == m.opts.Model {
			return i
		}
	}
	return 0
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
// /load — the Launch-profile picker (ADR 0029 D3)
// ----------------------------------------------------------------------------

// loadPickerTitle names what the third offering is and whose it is. Unlike the model picker's
// title it does not qualify by host: a Launch profile carries its own address, and the rows say so
// when it differs from the session's.
const loadPickerTitle = "load profile — llama-launcher"

// runningRowSuffix marks a profile discovery attributes to a live instance right now — the
// currentRowSuffix posture (plain text, because the popup module styles rows whole), for a fact that
// is about the world rather than about this session: a running profile is not necessarily the one
// this session is talking to.
const runningRowSuffix = " · running"

// noProfilesNote is what a launcher config with nothing in it earns. It names no path, deliberately:
// where the launcher's config lives is the composition root's knowledge (ADR 0029 D1/D4), and a
// renderer that guessed a path would eventually name the wrong one.
const noProfilesNote = "no launch profiles defined — add profiles to the llama-launcher config"

// runLoadCommand drives the /load verb in both its forms: bare, it opens the picker over the Launch
// profiles the launcher's config defines; with one argument it activates that profile by name.
// Surplus arguments are a usage note.
//
// The rows are read at OPEN and for BOTH forms, which is the whole of ADR 0029 D4's freshness rule:
// the seam re-reads the launcher's config from disk, so a profile added seconds ago in the
// launcher's own TUI is offered here — and the argument form is checked against the same list the
// picker would have shown, so the two forms can never disagree about what exists.
//
// The degrade ladder is asked FIRST, like /model's and for the same reason, and it is three rungs
// deep because three different things can be missing: the integration itself, the config it reads,
// and the profiles that config was supposed to hold. Each is one sentence and no overlay.
func (m Model) runLoadCommand(args []string) (tea.Model, tea.Cmd) {
	if len(args) > 1 {
		return m.pickerNote(loadUsage)
	}
	if m.opts.LaunchProfiles == nil || m.opts.LoadProfile == nil {
		return m.pickerNote(noLauncherNote)
	}
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
		for _, choice := range profiles {
			if choice.Name == args[0] {
				return m.startProfileLoad(choice.Name)
			}
		}
		return m.pickerNote(fmt.Sprintf(
			"unknown launch profile %q — configured: %s", args[0], profileNameList(profiles)))
	}
	m.picker = picker{open: true, kind: pickerLoad, profiles: profiles}
	m.layout()
	return m, nil
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

// launchProfileRows is one row per Launch profile: the name the human gave it in the launcher's
// config (which is also the /load argument and the footer alias afterwards), the backend it runs on,
// the context window it was configured with when it states one, the port it would serve at when that
// is NOT where this session is pointed, and the running mark.
//
// The port is shown only for a profile that lives somewhere else, because that is the moment it
// matters: loading it will move the session. A profile on the session's own server needs no address —
// it is the address the footer is already showing.
func (m Model) launchProfileRows() []string {
	here := sessionAddr(m.opts.Endpoint)
	rows := make([]string, 0, len(m.picker.profiles))
	for _, choice := range m.picker.profiles {
		label := choice.Name
		if choice.Backend != "" {
			label += " — " + choice.Backend
		}
		if window := formatTokens(choice.ContextWindow); window != "" {
			label += " · " + window
		}
		if port := elsewherePort(choice.Addr, here); port != "" {
			label += " (" + port + ")"
		}
		if choice.Running {
			label += runningRowSuffix
		}
		rows = append(rows, stripEscapes(label))
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
		picked := m.hb.models[m.picker.selected]
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
// Picking the row the session is already on is answered rather than ignored: an explicit act
// deserves an answer, where rebindNote's "" contract is about the observations nobody asked for.
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
		return len(m.hb.models)
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

// pickerRows composes the FULL row list the popup module paints, by kind. The module adds the
// marker, the highlight, the truncation and the scroll windowing; rows arrive plain and
// escape-stripped, as its contract requires — a model id is the SERVER's text, so it is sanitized
// here rather than trusted.
func (m Model) pickerRows() []string {
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

// modelRows is one row per advertised model: the id as the footer renders it (displayModel, so the
// pane and the chrome beside it can never name the same model two different ways), its context
// window when the server named one, and the "· current" mark on the row the session is bound to.
func (m Model) modelRows() []string {
	rows := make([]string, 0, len(m.hb.models))
	for _, offered := range m.hb.models {
		label := displayModel(offered.ID)
		if window := formatTokens(offered.ContextWindow); window != "" {
			label += " — " + window
		}
		if offered.ID == m.opts.Model {
			label += currentRowSuffix
		}
		rows = append(rows, stripEscapes(label))
	}
	return rows
}

// serverRows is one row per configured server: the name the human gave it (which is also the switch
// argument and the footer alias afterwards) with the endpoint it stands for spelled out beside it,
// and the "· current" mark on the server this session is on. The endpoint is shown rather than
// hidden behind the alias because a switch is exactly the moment a name is worth resolving to a URL.
func (m Model) serverRows() []string {
	rows := make([]string, 0, len(m.opts.Servers))
	for _, choice := range m.opts.Servers {
		label := choice.Name + " — " + choice.Endpoint
		if choice.Endpoint == m.opts.Endpoint {
			label += currentRowSuffix
		}
		rows = append(rows, stripEscapes(label))
	}
	return rows
}
