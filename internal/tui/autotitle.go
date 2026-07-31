package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/title"
)

// ----------------------------------------------------------------------------
// Automatic session naming (ADR 0022 addendum, 2026-07-31)
// ----------------------------------------------------------------------------
//
// A new Session record is named from the first thing the human asked, by one small out-of-band
// completion fired at that prompt's submit — in PARALLEL with the Exchange the prompt starts. On a
// single-slot server it therefore queues behind Turn 1 and answers between Turns 1 and 2, which is
// the cheapest KV-eviction point in the whole session: the context is at its smallest there.
//
// The call is COSMETIC. It is not a Turn and not a Mechanism: it fires at no Hook point, never
// shapes the primary call, never reaches the Engine (whose single-goroutine contract it would
// otherwise break — ADR 0011), emits no Token/Usage event, enters no transcript entry, and moves no
// gauge. Nothing in the conversation depends on it, which is exactly why every failure path here is
// silent: the heuristic title (sessionTitle) that the first Save already stamped simply stands, and
// a maintenance nicety must never nag.
//
// The renderer owns only WHEN it fires and whether the answer is applied. The call itself lives
// behind [Options.GenerateTitle], which the composition root backs with its own provider.Client over
// the server and model this session is bound to at the moment of the call.
//
// Four plain fields on the Model carry the state (see the Model's own declaration):
// autoTitleFired latches "this Session record has had its one call", titleTouched records that a
// HUMAN named the session (which drops a late-landing automatic title), and pendingTitle plus
// pendingSource stash a title that resolved before the first Save minted an id to rename, together
// with who asked for it.
//
// /rename (runRename) is the human's half of the same machinery, and it inverts one rule: it is an
// explicit request, so its answer applies even over a name the human set a moment ago, and — unlike
// the automatic call — it SPEAKS. A verb the human typed that quietly did nothing would be a bug in
// the interface, so every one of its outcomes lands as a transcript note.

// autoTitleMsg carries the AUTOMATIC naming call's result back to the Update loop. title is the
// model's raw reply (title.Sanitize turns it into a title, or reports that nothing usable came
// back) and err is whatever the seam reported. Both are folded silently: this Msg can only ever
// improve a title that already exists.
type autoTitleMsg struct {
	title string
	err   error
}

// manualTitleMsg carries a bare `/rename`'s result back to the Update loop. It is the same call
// under a different Msg precisely so the two folds can differ: this one was ASKED for, so it
// overrides titleTouched and reports what it did (foldManualTitle), where autoTitleMsg defers and
// stays silent.
type manualTitleMsg struct {
	title string
	err   error
}

// Compile-time assertions that the naming Msgs are valid tea.Msgs (mirroring messages.go).
var (
	_ tea.Msg = autoTitleMsg{}
	_ tea.Msg = manualTitleMsg{}
)

// titleSource says WHO a title came from. It rides with a stashed one (pendingTitle) because the
// never-clobber rule is about provenance and a stash outlives the moment it was made: a GENERATED
// title waiting for the id that the first Save mints must still be dropped when a human names the
// session while it waits, while a title the human typed before that same Save is theirs and lands
// regardless. It is meaningful only while pendingTitle is non-empty — applyTitle writes the two
// together, and nothing else writes either.
type titleSource int

const (
	titleAutomatic titleSource = iota // the out-of-band naming call — it answers to titleTouched
	titleManual                       // a human named it: the browser's rename edit, or /rename
)

// maybeAutoTitle fires the automatic naming call for the prompt that just went out, returning the
// Cmd to batch alongside the Exchange (nil when nothing should fire). It latches autoTitleFired, so
// one Session record earns exactly one call however many prompts follow.
//
// Four things must hold. The config toggle must be on (`auto-title:`), the seam must be wired,
// there must be a SessionHost — with no persistence there is no Session record to name, so the call
// would buy nothing and still cost a queue slot — and this record must not have fired already. A
// resumed session starts latched (replayResumed, resumeLoaded), which is what keeps a record that
// already has a name from being renamed behind the human's back.
func (m *Model) maybeAutoTitle(firstUserText string) tea.Cmd {
	if m.autoTitleFired || !m.opts.AutoTitle || m.opts.GenerateTitle == nil || m.sessions == nil {
		return nil
	}
	m.autoTitleFired = true
	return m.titleCmd(firstUserText, func(raw string, err error) tea.Msg {
		return autoTitleMsg{title: raw, err: err}
	})
}

// titleCmd builds the Cmd that runs one naming call off the Update loop and reports it through
// wrap — autoTitleMsg for the automatic path, manualTitleMsg for a bare `/rename`, which is the only
// thing that differs between them. It captures the seam and the program context by value, so the
// closure holds no pointer into the value-copied Model (the listSessions posture). The context is
// the PROGRAM's, not an Exchange's: the naming call outlives the Turn it was fired beside — that is
// the whole point of firing it in parallel — so only a shutdown should cut it short. Everything else
// about the deadline is the composition root's (it sets the client's queue-tolerant request timeout).
func (m Model) titleCmd(firstUserText string, wrap func(raw string, err error) tea.Msg) tea.Cmd {
	generate := m.opts.GenerateTitle
	parent := m.parent
	return func() tea.Msg {
		raw, err := generate(parent, firstUserText)
		return wrap(raw, err)
	}
}

// foldAutoTitle folds the automatic naming call's result. It is silent in every direction that is
// not a usable title: a failed call, a reply nothing survives sanitizing, or a session the human has
// since named themselves all leave the heuristic title standing (Ratified design 9). Only the
// automatic path answers to titleTouched — a bare `/rename` is an explicit request and applies
// regardless.
func (m *Model) foldAutoTitle(msg autoTitleMsg) tea.Cmd {
	if msg.err != nil || m.titleTouched {
		return nil
	}
	name, ok := title.Sanitize(msg.title)
	if !ok {
		return nil
	}
	return m.applyTitle(name, titleAutomatic)
}

// foldManualTitle folds a bare `/rename`'s result, and it is the automatic fold's opposite in both
// directions. The human ASKED for this name, so it applies even over one they typed themselves a
// moment ago (Ratified design 5) — the request is the newer instruction — and it sets titleTouched
// in its turn, so an automatic call still in flight cannot land on top of it afterwards. And it
// speaks either way: a title they asked for and never see is indistinguishable from a command that
// did nothing, so a failure earns the one form that always works instead of the silence the
// automatic path is right to keep.
func (m *Model) foldManualTitle(msg manualTitleMsg) tea.Cmd {
	name, ok := title.Sanitize(msg.title)
	if msg.err != nil || !ok {
		m.transcript.addNote("could not name this session — " + renameUsage)
		m.layout()
		return nil
	}
	m.titleTouched = true
	cmd := m.applyTitle(name, titleManual)
	m.noteRenamed(name)
	return cmd
}

// applyTitle routes a resolved title at the Session record it names. Rename is the only correct
// writer of a stored title — SessionHost.Save fixes the title at create and ignores the argument
// thereafter (wire.go), so the heuristic stamped by the first Save can only ever be replaced this
// way. Before that first Save there is no id to rename, so the title is stashed — together with
// src, which is what the flush later needs to know — and applied at the save-complete that mints
// one (flushPendingTitle).
func (m *Model) applyTitle(name string, src titleSource) tea.Cmd {
	if m.sessions == nil {
		return nil
	}
	id := m.sessions.ActiveID()
	if id == "" {
		m.pendingTitle = name
		m.pendingSource = src
		return nil
	}
	return m.setSessionTitle(id, name)
}

// flushPendingTitle applies a stashed title once a Save has put the record on disk, clearing the
// stash. It is called from the save-complete fold on a SUCCESSFUL save: the rename reads and
// rewrites a stored record, so a title applied before the file exists would simply be dropped by
// the store. A save that failed leaves the stash for the next one.
//
// The never-clobber rule is enforced here as well as at arrival, because a stash is the one way an
// AUTOMATIC title can outlive the check foldAutoTitle already made: it can be waiting for an id at
// the moment a human names the session (through the browser, or `/rename <name>` on a record that
// does not exist yet), and flushing it then would overwrite the name they just chose. A stash the
// human asked for flushes unconditionally — it IS what they chose.
func (m *Model) flushPendingTitle() tea.Cmd {
	if m.pendingTitle == "" || m.sessions == nil {
		return nil
	}
	if m.pendingSource == titleAutomatic && m.titleTouched {
		m.pendingTitle = ""
		return nil
	}
	id := m.sessions.ActiveID()
	if id == "" {
		return nil
	}
	name := m.pendingTitle
	m.pendingTitle = ""
	return m.setSessionTitle(id, name)
}

// renameUsage closes every /rename note that could not produce a title: the one form that always
// works. A human who asked for a name must never be left holding only a refusal.
const renameUsage = "try /rename <name>"

// runRename drives the /rename verb (command.go, runCommand). Two forms, one apply path:
//
//   - `/rename <text>` names the session outright. The tokens re-join with single spaces and run
//     through the SAME sanitizer a generated title does — one pipeline, so a pasted name cannot
//     smuggle an escape sequence past what a model's reply could not — and then take the ordinary
//     route: rename the record, or stash the title for the id the first Save mints. That stash is
//     what lets a session be named BEFORE it has said anything, overriding the heuristic title the
//     first Save would otherwise stamp.
//   - bare `/rename` asks the model, running the same out-of-band call the first prompt fires
//     (titleCmd) and folding it as a manualTitleMsg.
//
// Both forms set titleTouched: a human named this session, so no automatic title may overwrite it
// (Ratified design 5). And every branch says what happened — the refusals included, because unlike
// the automatic call this one was asked for by name.
func (m Model) runRename(args []string) (tea.Model, tea.Cmd) {
	if m.sessions == nil {
		// No persistence host: there is no Session record, so there is nothing named to change. Said
		// plainly rather than accepted and dropped — the note below would otherwise be a lie.
		m.transcript.addNote("this session is not being saved, so it has no name to change")
		m.layout()
		return m, nil
	}
	if len(args) > 0 {
		name, ok := title.Sanitize(strings.Join(args, " "))
		if !ok {
			m.transcript.addNote("that name is empty once cleaned up — " + renameUsage)
			m.layout()
			return m, nil
		}
		m.titleTouched = true
		cmd := m.applyTitle(name, titleManual)
		m.noteRenamed(name)
		return m, cmd
	}
	if m.opts.GenerateTitle == nil {
		// The seam is unwired (no upstream client was built for it), which is a property of the run
		// and not a failure: the manual form still names anything.
		m.transcript.addNote("title generation is not available — " + renameUsage)
		m.layout()
		return m, nil
	}
	first := m.transcript.firstUserText()
	if first == "" {
		// The model names a session from the first thing it was asked, and nothing has been asked
		// yet. The manual form works from the very first keystroke, so it is what the note offers.
		m.transcript.addNote("nothing to name yet — ask something first, or " + renameUsage)
		m.layout()
		return m, nil
	}
	m.transcript.addNote("naming this session…")
	m.layout()
	return m, m.titleCmd(first, func(raw string, err error) tea.Msg {
		return manualTitleMsg{title: raw, err: err}
	})
}

// noteRenamed records an applied title in the transcript. It is the one place a title is ever
// SHOWN: the automatic call adds no UI chrome by design (titles surface in the /sessions browser and
// the resume note), but a rename the human asked for has to report back, and the note doubles as the
// only preview of what the sanitizer made of what they typed.
func (m *Model) noteRenamed(name string) {
	m.transcript.addNote("session renamed: " + name)
	m.layout()
}
