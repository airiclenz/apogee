package tui

import (
	"errors"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/title"
)

// ----------------------------------------------------------------------------
// Automatic session naming (ADR 0022 addendum, 2026-07-31)
// ----------------------------------------------------------------------------
//
// A new Session record is named by one small out-of-band completion fired at the first prompt's
// submit — in PARALLEL with the Exchange the prompt starts. On a single-slot server it therefore
// queues behind Turn 1 and answers between Turns 1 and 2, which is the cheapest KV-eviction point
// in the whole session: the context is at its smallest there. That automatic call names the session
// from the first thing the human asked, because when it fires that is the only thing they have
// asked; a bare /rename asked for later reads a bounded window of the session's user side instead
// (runRename), so a session that moved on to another task can be named for where it ended up.
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
// Three values on the Model carry the state (see the Model's own declaration): autoTitleFired
// latches "this Session record has had its one call", titleTouched records that a HUMAN named the
// session (which drops a late-landing automatic title), and pendingTitle is the titleStash below —
// a title that resolved before the first Save minted an id to rename, together with who asked for
// it. What may be stashed, and what becomes of it, is the stash's own affair (titleStash): every
// site that used to write those fields by hand now asks it for one of its verbs.
//
// /rename (runRename) is the human's half of the same machinery, and it inverts two rules. It is an
// explicit request, so its answer applies even over a name the human set a moment ago, and — unlike
// the automatic call — it SPEAKS: a verb the human typed that quietly did nothing would be a bug in
// the interface, so every one of its outcomes lands as a transcript note, and a failure whose cause
// the human could act on is named rather than folded into the generic refusal (foldManualTitle). And
// it reads the whole user side of the session rather than its opening request, because "name this"
// typed late means the session as it now stands.

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

// titleSource says WHO a title came from. It rides with a stashed one (titleStash) because the
// never-clobber rule is about provenance and a stash outlives the moment it was made: a GENERATED
// title waiting for the id that the first Save mints must still be dropped when a human names the
// session while it waits, while a title the human typed before that same Save is theirs and lands
// regardless. It is meaningful only while the stash holds a name — the stash writes the two
// together, and nothing else writes either.
type titleSource int

const (
	titleAutomatic titleSource = iota // the out-of-band naming call — it answers to titleTouched
	titleManual                       // a human named it: the browser's rename edit, or /rename
)

// titleStash owns the session title between the moment it is decided and the moment a record exists
// to carry it. Rename is the only correct writer of a stored title — SessionHost.Save fixes it at
// create and ignores the argument thereafter (wire.go) — so a title that resolves before the first
// Save has minted an id has nowhere to go yet, and is held here until one exists (flushPendingTitle).
//
// Four things can happen to a held title, and they are this value's verbs rather than assignments
// spread across the naming, save and session-lifetime files:
//
//   - adopt — a resolved title with no record to rename takes the stash. Unconditional: the newest
//     decision is the one the session goes by, so a second title replaces a first.
//   - flush — the save that minted the id takes the title back out, emptying the stash.
//   - restash — a title the rename could not write goes back in, so the NEXT successful save
//     applies it (foldRecordWrite) instead of losing it to a window in which the record was not
//     there yet. A stash already holding something wins: that one is the newer instruction.
//   - drop — the stash is given up: the never-clobber rule threw the title away (clobbered), or the
//     session it was made for is gone (a /clear rotation, a /sessions restore).
//
// The never-clobber rule lives in clobbered, which adopt deliberately does not consult and the two
// paths that end a wait do. That asymmetry is the whole reason src is stashed alongside the name: a
// stash OUTLIVES the check its title already passed. An automatic title can be waiting for an id at
// the moment a human names the session — through the browser, or /rename on a record that does not
// exist yet — and applying it then would overwrite the name they just chose, so the check is made
// again wherever the stash is about to be honoured. A title the human asked for is exempt at every
// one of those points: it IS what they chose.
//
// The zero value is "nothing is waiting", which is what makes a fresh Model, and every session
// boundary that resets one, correct without a flag of its own (ADR 0011: two plain fields, safe in
// the value-copied Model).
type titleStash struct {
	// name is the title waiting for an id, and "" is the empty stash: a title that sanitizes to
	// nothing never reaches here (title.Sanitize refuses it), so the two can be the same field.
	name string
	// src is who asked for name, read only by clobbered and carried into the rename that lands it
	// (recordWrite.source) so a retry can make the same judgement. It is meaningful only while name
	// is non-empty.
	src titleSource
}

// stashed reports whether a title is waiting for the id the first Save mints.
func (t titleStash) stashed() bool { return t.name != "" }

// clobbered reports whether what is held is an AUTOMATIC title that a human's own naming outranks —
// touched being Model.titleTouched, "a human named this session". It is asked wherever a stash is
// about to be honoured, never where one is made: see the type's doc for why a stash must be judged
// twice.
func (t titleStash) clobbered(touched bool) bool {
	return t.stashed() && t.src == titleAutomatic && touched
}

// adopt puts a resolved title on the stash, unconditionally: with no record to rename yet this IS
// the name the session goes by (applyTitle names the frame from the same decision), and a later
// title supersedes an earlier one.
func (t *titleStash) adopt(name string, src titleSource) {
	t.name, t.src = name, src
}

// flush hands the held title to the write that will land it and empties the stash. The caller has
// already established that there is a record to rename and that the title may still be applied
// (flushPendingTitle) — flush itself asks nothing.
func (t *titleStash) flush() (name string, src titleSource) {
	name, src = t.name, t.src
	t.name = ""
	return name, src
}

// restash puts a title a rename could not write back on the stash, so the next successful save
// re-applies it rather than losing it. A stash already holding something wins — it is the newer
// instruction — and the never-clobber rule holds here as everywhere else: an automatic title is
// dropped rather than re-stashed once a human has named the session.
func (t *titleStash) restash(name string, src titleSource, touched bool) {
	if t.stashed() {
		return
	}
	next := titleStash{name: name, src: src}
	if next.clobbered(touched) {
		return
	}
	*t = next
}

// drop gives up whatever is held. Two things ask for it: the never-clobber rule, which throws away
// an automatic title a human's own name outranks (flushPendingTitle), and a session boundary, which
// leaves a title stashed for a session that is no longer the live one (startNewSession,
// resumeLoaded).
func (t *titleStash) drop() { t.name = "" }

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
	return m.titleCmd([]string{firstUserText}, func(raw string, err error) tea.Msg {
		return autoTitleMsg{title: raw, err: err}
	})
}

// titleCmd builds the Cmd that runs one naming call off the Update loop and reports it through
// wrap — autoTitleMsg for the automatic path, manualTitleMsg for a bare `/rename`, which is the only
// thing that differs between them. prompts is the window the call names the session from, oldest
// first. It captures the seam, that window and the program context by value, so the closure holds
// no pointer into the value-copied Model (the listSessions posture) — the window is built for this
// one call by the caller rather than shared with the Model. The context is the PROGRAM's, not an
// Exchange's: the naming call outlives the Turn it was fired beside — that is the whole point of
// firing it in parallel — so only a shutdown should cut it short. Everything else about the
// deadline is the composition root's (it sets the client's queue-tolerant request timeout).
func (m Model) titleCmd(prompts []string, wrap func(raw string, err error) tea.Msg) tea.Cmd {
	generate := m.opts.GenerateTitle
	parent := m.parent
	return func() tea.Msg {
		raw, err := generate(parent, prompts)
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
	cmd, _ := m.applyTitle(name, titleAutomatic)
	return cmd
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
		note := "could not name this session"
		if errors.Is(msg.err, title.ErrTruncated) {
			// The one failure worth naming to the human. A thinking model can spend the naming
			// call's whole token cap on reasoning and return no title at all (title.ErrTruncated),
			// and "could not name this session" would send them looking for a broken server instead
			// of the model's own verbosity — which the manual form sidesteps immediately. Every
			// other failure stays generic: a cause the human cannot act on differently is noise.
			note = "the model spent its whole reply thinking and never wrote the title"
		}
		m.transcript.addNote(note + " — " + renameUsage)
		m.layout()
		return nil
	}
	m.titleTouched = true
	cmd, stashed := m.applyTitle(name, titleManual)
	m.noteRenamed(name, stashed)
	return cmd
}

// applyTitle routes a resolved title at the Session record it names. Rename is the only correct
// writer of a stored title — SessionHost.Save fixes the title at create and ignores the argument
// thereafter (wire.go), so the heuristic stamped by the first Save can only ever be replaced this
// way. Before that first Save there is no id to rename, so the title is stashed — together with
// src, which is what the flush later needs to know — and applied at the save-complete that mints
// one (flushPendingTitle). It reports that stash back to its caller, because a title held for a
// record that does not exist yet and a title written to one are two different things to have to
// say — which is the difference /rename's note turns on (noteRenamed).
func (m *Model) applyTitle(name string, src titleSource) (cmd tea.Cmd, stashed bool) {
	if m.sessions == nil {
		return nil, false
	}
	// The frame is named from the decision, not from the write that follows it: a title stashed
	// for an id that does not exist yet is already the name this session goes by, and a rename that
	// fails on disk must not rename the frame back (nameSession).
	m.nameSession(name)
	id := m.sessions.ActiveID()
	if id == "" {
		m.pendingTitle.adopt(name, src)
		return nil, true
	}
	return m.setSessionTitle(id, name, src), false
}

// nameSession records the name this session is now known by — the display half of the naming
// machinery, and the ONLY writer of Model.sessionName. Every route that decides a name goes
// through it: both naming calls and `/rename <text>` (applyTitle), the browser's `r` when the row
// it renames is the live session, and both resume paths, which arrive with a name already chosen.
// startNewSession clears the field directly, because "no name" is not a name.
//
// A name that is empty or nothing but whitespace is refused rather than applied: the callers'
// sanitizers can reduce a title to nothing, and a frame that went from naming the session to
// naming nothing would read as a bug. What the frame does with the name — sanitizing it,
// clipping it, and what it says when there is none — belongs to the seam that renders it, not
// here.
func (m *Model) nameSession(name string) {
	if strings.TrimSpace(name) == "" {
		return
	}
	m.sessionName = name
}

// flushPendingTitle QUEUES a stashed title once a Save has put the record on disk, clearing the
// stash. It is called from the save-complete fold on a SUCCESSFUL save: the rename reads and
// rewrites a stored record, so a title applied before the file exists would simply be dropped by
// the store. A save that failed leaves the stash for the next one.
//
// It queues rather than dispatching, and returns nothing to dispatch, deliberately. saveComplete
// calls it while the record-write latch is still held, and the flush is a record WRITE like the
// save it follows: handing a Cmd back would put a rename on one goroutine beside the coalesced save
// on another, which is exactly the collision that could roll a Turn off disk (model.go, audit
// 2026-08-01). The queue dispatches it next instead.
//
// The never-clobber rule is asked again here (clobbered), because a stash is the one way an
// AUTOMATIC title can outlive the check foldAutoTitle already made — the reason the stash carries
// its source at all (titleStash). What this fold owns beyond the stash's own verbs is the frame: a
// dropped title must not stay on it.
func (m *Model) flushPendingTitle() {
	if !m.pendingTitle.stashed() || m.sessions == nil {
		return
	}
	if m.pendingTitle.clobbered(m.titleTouched) {
		// The stash named the session when it was made (applyTitle), so the frame can be wearing the
		// very name this drop throws away — and it would then be a name NOTHING carries, since the
		// record keeps the heuristic title the first Save stamped. Giving it up returns the frame to
		// that heuristic, which is what the record says. The check is on the name and not on
		// titleTouched, because a human who named THIS session is already what the field holds and
		// theirs must stand: titleTouched is set by a browser rename of ANY row, so it cannot tell
		// the two apart on its own.
		if m.sessionName == m.pendingTitle.name {
			m.sessionName = ""
		}
		m.pendingTitle.drop()
		return
	}
	id := m.sessions.ActiveID()
	if id == "" {
		return
	}
	name, src := m.pendingTitle.flush()
	m.queueWrite(recordWrite{kind: writeRename, id: id, title: name, retryTitle: true, source: src})
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
//     (titleCmd) and folding it as a manualTitleMsg. Where the automatic call names the session
//     from its opening request, this one hands over the session's whole user side (userTexts) and
//     lets title.Prompt bound it: the human asked for a name NOW, so the answer must describe the
//     session as it now stands rather than the task it may have left hours ago.
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
		cmd, stashed := m.applyTitle(name, titleManual)
		m.noteRenamed(name, stashed)
		return m, cmd
	}
	if m.opts.GenerateTitle == nil {
		// The seam is unwired (no upstream client was built for it), which is a property of the run
		// and not a failure: the manual form still names anything.
		m.transcript.addNote("title generation is not available — " + renameUsage)
		m.layout()
		return m, nil
	}
	prompts := m.transcript.userTexts()
	if len(prompts) == 0 {
		// The model names a session from what it has been asked, and nothing has been asked yet. The
		// manual form works from the very first keystroke, so it is what the note offers.
		m.transcript.addNote("nothing to name yet — ask something first, or " + renameUsage)
		m.layout()
		return m, nil
	}
	m.transcript.addNote("naming this session…")
	m.layout()
	return m, m.titleCmd(prompts, func(raw string, err error) tea.Msg {
		return manualTitleMsg{title: raw, err: err}
	})
}

// noteRenamed records a title the human asked for in the transcript. It is the one place a title is
// ever SHOWN: the automatic call adds no UI chrome by design (titles surface in the /sessions browser
// and the resume note), but a rename the human asked for has to report back, and the note doubles as
// the only preview of what the sanitizer made of what they typed.
//
// stashed splits the two things a rename can mean here. A record that exists is renamed, and the
// note says so. A session with no record yet was not renamed — nothing on disk carries that name
// until the first Save mints the id the stash is waiting for (flushPendingTitle) — so the note
// promises the name rather than claiming a rename that has not happened. The outcome is the same
// either way, which is why the difference is a tense and not a warning.
func (m *Model) noteRenamed(name string, stashed bool) {
	if stashed {
		m.transcript.addNote("session name set: " + name + " — applied when the session is first saved")
	} else {
		m.transcript.addNote("session renamed: " + name)
	}
	m.layout()
}
