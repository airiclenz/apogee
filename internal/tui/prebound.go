package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
)

// ----------------------------------------------------------------------------
// The pre-bound session — a start with no upstream (ADR 0036 decisions 3, 4 and 7)
// ----------------------------------------------------------------------------
//
// Every other state in this package describes a session that HAS a server. This one describes the
// gap before it does: the binary could not tell which entry of `servers:` this run starts on — none
// is recorded yet, the recorded one is gone, or nothing is configured at all — and the TUI is the
// one Driver that can ASK rather than refuse (the non-interactive drivers keep their hard error,
// because they have nobody to ask). The engine is not merely unbound here, it does not EXIST:
// construction still needs an endpoint (ADR 0024), it simply happens later, through
// [Options.BindServer].
//
// The state is [Options.Prebound] and it is cleared by exactly one event — a successful bind — so
// "is there an engine" is one comparison ([Model.prebound]) rather than a flag anyone can forget to
// move. Everything the state costs follows from that one predicate: no beat goes out (there is
// nothing to observe — [Model.beatCmd]), a send is answered by the ask instead of by the engine
// ([Model.preboundRefusal]), and the status line carries the standing fact for as long as it holds.
//
// The ask itself is NOT a new surface. Two of the three reasons open the `/server` picker the human
// would have opened themselves, over the same rows, with the same keys; the third opens `/settings`,
// where the read-only `servers` row already points at the file that has to be edited. What this file
// adds to those two panes is the WORDS — one notice saying why the pane came up unasked, and one
// status-line fact that survives the esc that closes it.

// The status-line facts a pre-bound session leaves — the licence layout.md gives every surface that
// gives way (the `N queued` readout and `settings — esc close` are the others). They are what the
// human is left with after esc closes the pane that came up at start-up, so each carries the ONE
// thing that is not visible anywhere else: that nothing is bound, and the one act that changes it.
const (
	noServerBoundFact       = "no server bound — /server to choose"
	noServersConfiguredFact = "no servers configured — add one to ~/.apogee/config.yaml and restart"
)

// firstBootNotice is what an unasked-for picker says for itself on a first run. It names the
// consequence of the choice as well as the choice, because "remembered" is exactly what makes this
// a one-time question rather than one the human expects at every launch.
const firstBootNotice = "no server chosen yet — pick the one this session starts on (the choice is remembered)"

// noBindSeamNote is the degrade for a pre-bound session whose bind seam is unwired — a hand-built
// Options, or a Driver that composed the renderer without the binary's server list (ADR 0031). It is
// the noServersNote posture: one honest line, and no overlay pretending the choice can be made.
const noBindSeamNote = "cannot bind a server in this build"

// prebound reports whether this session still has no engine. It is the one predicate the state is
// read through — the beat, the refusals, the accept path and the status line all ask it — so the
// single write that clears [Options.Prebound] (a committed bind) ends the state everywhere at once.
func (m Model) prebound() bool {
	return m.opts.Prebound.Reason != ""
}

// preboundNotice words WHY a session has no server, for the transcript: the line the auto-opened
// picker comes up under, and the same line a refused send earns later. The stale case names the
// entry that went missing, quoted — %q renders a control character as an escape rather than passing
// it to the terminal, which is the sanitizing stripEscapes does for every other untrusted string
// reaching this package.
//
// It returns "" for a bound session, which no caller reaches.
func preboundNotice(p PreboundStart) string {
	switch p.Reason {
	case PreboundFirstBoot:
		return firstBootNotice
	case PreboundStaleChoice:
		return fmt.Sprintf("server: %q names no configured entry — pick the one this session starts on", p.Name)
	case PreboundNoServers:
		return noServersConfiguredFact
	}
	return ""
}

// preboundFact is the status line's shorter half of the same truth: what the human needs on a frame
// where the pane that explained it has been closed. The two askable reasons share one fact — which
// entry the config named is the notice's business, while the status line carries only the state and
// the way out.
func preboundFact(p PreboundStart) string {
	switch p.Reason {
	case PreboundFirstBoot, PreboundStaleChoice:
		return noServerBoundFact
	case PreboundNoServers:
		return noServersConfiguredFact
	}
	return ""
}

// preboundPhrase is that fact styled for the status line, and "" for a bound session — the string
// [Model.statusLeft] puts in the slot an idle frame otherwise leaves empty (settingsGiveWayPhrase is
// the other, and takes precedence: a pane swallowing every key on a window showing none of it is the
// more urgent fact).
func (m Model) preboundPhrase() string {
	fact := preboundFact(m.opts.Prebound)
	if fact == "" {
		return ""
	}
	return m.th.statusBar.Render(fact)
}

// openPrebound puts the ask up at construction, before the first frame is ever painted. It runs in
// newModel rather than Init for the reason the input's focus does: Init holds a COPY of the Model
// and returns only a Cmd, so overlay STATE has to be set on the stored value.
//
// Which pane comes up is the reason's own answer. The two reasons with something to pick open the
// `/server` picker under a notice saying why it came up unasked. Nothing configured opens `/settings`
// instead, where the read-only `servers` row's ⏎-opens-an-editor pointer is the guidance — and
// where that pane cannot open (no rows provider, the same degrade /settings itself takes) the notice
// stands in for it, because a start-up that said nothing at all would read as a broken build.
func (m *Model) openPrebound() {
	switch m.opts.Prebound.Reason {
	case "":
		return
	case PreboundNoServers:
		if len(m.settingRows()) > 0 {
			m.settings = settingsPane{open: true}
			return
		}
		m.transcript.addNote(preboundNotice(m.opts.Prebound))
	default:
		m.transcript.addNote(preboundNotice(m.opts.Prebound))
		m.picker = picker{open: true, kind: pickerServer,
			listSurface: listSurface{selected: m.currentServerRow()}}
	}
}

// preboundRefusal answers an act that needed an engine the session has not got — a typed message,
// most of all. It re-opens the ask rather than merely refusing: the human's message is still in the
// box (nothing here touches it, the blockedUpstream posture), and the one thing standing between it
// and the model is a choice they can make with one keystroke on the pane that comes back up.
//
// With nothing configured there is nothing to re-open, so the guidance note is the whole answer.
func (m Model) preboundRefusal() (tea.Model, tea.Cmd) {
	m.transcript.addNote(preboundNotice(m.opts.Prebound))
	if len(m.servers()) > 0 && m.opts.BindServer != nil {
		m.picker = picker{open: true, kind: pickerServer,
			listSurface: listSurface{selected: m.currentServerRow()}}
	}
	m.layout()
	return m, nil
}

// bindToServer is the pre-bound half of [Model.switchToServer]: the accept path of the same picker,
// on a session that has no engine to move. It constructs one ([Options.BindServer], synchronously on
// the Update loop like the switch, opening no connection of its own) and folds the result exactly as
// a switch is folded, because what the display has to adopt is the same in both cases.
//
// The seam is validate-then-commit, so an error means nothing was constructed and the session is
// still pre-bound — the note is the whole answer, and the fact on the status line still says so.
func (m Model) bindToServer(choice ServerChoice) (tea.Model, tea.Cmd) {
	if m.opts.BindServer == nil {
		return m.pickerNote(noBindSeamNote)
	}
	result, err := m.opts.BindServer(choice.Name)
	if err != nil {
		return m.pickerNote("could not bind server: " + stripEscapes(err.Error()))
	}
	// The recording runs BEFORE the fold and the fold states it, so the human reads one line about one
	// act rather than a line and a footnote about the same one (ADR 0036 decision 2).
	record := recordServerChoice(m.opts.RecordServerChoice, choice.Name)
	// The state is over the moment the engine exists, and it is cleared BEFORE the fold: the beat the
	// fold arms is issued only for a bound session (beatCmd).
	m.opts.Prebound = PreboundStart{}
	bound, beat := m.foldServerBind(result, record)
	bound.layout()
	return bound, beat
}

// foldServerBind folds a COMMITTED first bind into the display — [Model.foldServerSwitch]'s sibling,
// and deliberately the thinner of the two. The Options adopt what the seam reported (the endpoint now
// on the wire, the entry's name as the alias, the surviving window pin) and the start-up box is
// restated in place, because its facts were frozen at construction when there was no server to name.
//
// What it does NOT do is reset the heartbeat state, and that is the difference from a switch: a
// pre-bound session has beaten at nothing, failed at nothing and observed nothing, so there is no
// old server's evidence to clear and no `switched` mark to set. This IS the cold start ADR 0024
// describes — the first beat lands quietly and the restated box is what says the model — it merely
// begins a few seconds later than usual. The Cmd is that first beat, armed now rather than one
// Interval from now.
func (m Model) foldServerBind(result ServerSwitchResult, record choiceRecord) (Model, tea.Cmd) {
	m.opts.Endpoint = result.Endpoint
	m.opts.HostAlias = result.HostAlias
	m.opts.ContextWindow = result.ContextWindow
	m.transcript.refreshStartup(newStartupView(m.opts))
	m.transcript.addNote(serverBindNote(m.opts, record.saved))
	record.warn(&m.transcript)
	// Arming is a statement of its own: it takes a pointer, and in `return m, m.armBeat()` the order
	// of the bump and the copy of m is unspecified (ADR 0011, [Model.armBeat]).
	beat := m.armBeat()
	return m, beat
}

// serverBindNote words a committed first bind, in serverSwitchNote's shape one step shorter: there
// is no server being left, so the line states only the one now on the wire — its alias with the
// endpoint spelled out beside it, once when the two are the same string.
func serverBindNote(to Options, saved bool) string {
	note := "server bound: " + hostDisplay(to)
	if hostDisplay(to) != to.Endpoint {
		note += " (" + to.Endpoint + ")"
	}
	return note + savedClause(saved)
}

// ----------------------------------------------------------------------------
// Recording the choice — the `server:` key (ADR 0036 decision 2)
// ----------------------------------------------------------------------------

// savedChoiceClause is what a bind or switch note adds when the move was also RECORDED: the
// `server:` key now names the entry this session is on, so the NEXT one starts here without being
// asked. It names the key rather than describing it, because the key is what the human will find in
// config.yaml — and it is stated on the move's own line so a remembered choice reads as part of the
// move rather than as a second event.
const savedChoiceClause = " · server: saved"

// savedClause renders that clause for a move that was recorded, and "" for one that was not — a move
// onto an unlisted endpoint (the ephemeral override row, a launcher profile's server), an unwired
// seam, or a write that failed and said so in a warning of its own.
func savedClause(saved bool) string {
	if saved {
		return savedChoiceClause
	}
	return ""
}

// choiceRecord is what a committed move learned from the recording seam: whether the `server:` key
// now names the entry the session is on, and the warning a failed write earns ("" when there is
// nothing to say). Both travel into the fold together so the announce can state the recording on its
// own line and a warning can follow the move it is a footnote to, rather than precede it.
type choiceRecord struct {
	saved   bool
	warning string
}

// warn appends the failed-write warning to the transcript, and nothing at all when there is none. It
// takes the transcript by pointer because a Model is copied by value on every Update (ADR 0011) —
// the caller's own copy is the one that must carry the note.
func (r choiceRecord) warn(t *transcript) {
	if r.warning != "" {
		t.addNote(r.warning)
	}
}

// recordServerChoice persists the name this session is now on as the one the NEXT session starts on
// (ADR 0036 decision 2), through the seam the binary backs. It is called with every name the human
// chose; whether that name is a configured entry worth writing is the binary's question, not the
// renderer's, and the answer comes back as the saved flag rather than being guessed at here.
//
// A failed write is a warning and nothing more: the session already moved, and the recording is
// best-effort persistence of something that is already true. An unwired seam records nothing and
// says nothing — that is the pre-ADR-0036 behaviour, and every hand-built Options has it.
func recordServerChoice(record func(name string) (bool, error), name string) choiceRecord {
	if record == nil || name == "" {
		return choiceRecord{}
	}
	saved, err := record(name)
	if err != nil {
		return choiceRecord{warning: "could not record the server choice: " + stripEscapes(err.Error())}
	}
	return choiceRecord{saved: saved}
}
