package tui

import (
	"os/exec"

	tea "charm.land/bubbletea/v2"
)

// ----------------------------------------------------------------------------
// /settings — the two ways the config FILE changes under a running session
// ----------------------------------------------------------------------------
//
// The pane in settings.go is one way a key gets edited; this file is the other two, and they are one
// round trip with two triggers (ADR 0041 decision 6). A human can open the file in their own editor
// from a row — foreground, which suspends the program, or detached, which does not — and a human can
// save that same file from anywhere else entirely, which the binary's watcher reports. Both ends
// arrive here, both re-read through [Options.ReloadConfig], and both land through the ONE apply loop
// below (applyReloaded → settingsApplied, settingsapply.go), so a key edited in a terminal editor
// cannot take a different path from the same key edited in a GUI one.
//
// Nothing in here decides what a value MEANS: what changed is the reload's answer, and what putting
// it into effect costs is the apply's (settingsapply.go). This file owns the trip out and back.

// settingsEditedMsg is the return of a FOREGROUND external editor: which row launched it, and
// whether the process itself ran. It is the pane's own message rather than a shared one because
// nothing else in the frame suspends the program — the path is carried so the reload's outcome lands
// on the row the human pressed ⏎ on, which is the only row they have any reason to be looking at.
//
// A detached launch has no such return: nobody waits for it, so the message it produces is about the
// START and nothing else ([settingsDetachedMsg]).
type settingsEditedMsg struct {
	path string
	err  error
}

// settingsDetachedMsg is what a DETACHED launch has to say: it started, or it did not. There is no
// exit to report and no re-read to trigger — the pane never left, the program outlives the keypress,
// and what the human writes in it arrives by the config watcher instead (ADR 0041 decision 3).
//
// It is a message rather than a value the keypress folds in place because starting a process is
// work, and work belongs on a Cmd goroutine rather than in Update — the same reason the foreground
// path hands its process to Bubble Tea.
type settingsDetachedMsg struct {
	path string
	err  error
}

// The two sentences the external edit refuses with. The first is binding C of ADR 0037: suspending
// the whole program into an editor while a Step is streaming would take the transcript off the
// screen mid-answer and leave the applies to queue behind a run — so the offer stands only between
// runs, and the row says which. The second is the nil-seam degrade noSettingsWriterNote gives every
// other row, worded for the act this one performs.
const (
	settingsEditBusyNote = "wait for the current run to finish"
	noExternalEditNote   = "cannot open an editor in this build"
)

// settingsDetachedEditNote is what the row says when the editor was started DETACHED. The pane never
// went away, so a keypress that opened a window somewhere else — behind the terminal, on another
// desktop, in an application that was already running — looks from in here like a keypress that did
// nothing at all. This sentence is the whole of what the row has to show for it, which is why the
// launch lands in the pane's answer slot rather than silently ([settingAnswer]).
const settingsDetachedEditNote = "opened in your editor"

// settingsExternalEdit answers ⏎ on a row holding a structure no field can express: it opens the
// human's own editor on that key's line (ADR 0037 decision 5). The command line is the binary's —
// which file, which line, which editor, and whether that editor takes this terminal — and this only
// runs it ([Options.ExternalEditSpec]).
//
// Two ways to run it, one keypress (ADR 0041 decision 6). A FOREGROUND editor gets the program's own
// terminal through tea.ExecProcess and its exit is the trigger for the re-read, exactly as it has
// always been — a terminal editor drawn over a live alt-screen TUI is broken, so there is no other
// way to run one. Everything else is started DETACHED: the pane stays up, nothing waits, and what
// the human saves arrives through the config watcher rather than through an exit that means nothing
// (an opener stub returns before the editor is even on screen).
//
// It is offered only between runs (binding C). Mid-run the row says to wait rather than the pane
// queueing the edit: the alternative is tearing the alternate screen down over a streaming reply and
// holding a file's worth of applies until it finishes, for a key nobody is waiting on that hard. In-
// pane edits stay allowed mid-run — those apply through the seams that know how to refuse.
//
// The in-flight test names both halves of "a run", and the LATCH is the half that reaches here: a
// streaming Step leaves this pane's keys unrouted entirely (the overlay is idle-only, handleKey), so
// it is a launcher actuation — which runs on a Cmd goroutine with the session idle — that a human can
// actually press ⏎ during. The busy check stands beside it because the sentence is about runs, not
// about which of them today's routing happens to deliver.
//
// A refusal from the spec is the row's failure, and NOTHING is launched: an unreadable config or a
// file shape the parse will not risk is exactly the moment not to hand a human an editor and a
// promise to re-read it.
func (m Model) settingsExternalEdit(row SettingRow) (tea.Model, tea.Cmd) {
	if m.busy() || m.actuation.inFlight {
		return m.settingsFailed(row, settingsEditBusyNote)
	}
	if m.opts.ExternalEditSpec == nil {
		return m.settingsFailed(row, noExternalEditNote)
	}
	launch, err := m.opts.ExternalEditSpec(row.Path)
	if err != nil {
		return m.settingsFailed(row, stripEscapes(err.Error()))
	}
	argv := launch.Argv
	if len(argv) == 0 {
		return m.settingsFailed(row, noExternalEditNote)
	}
	// The last refusal goes with the keypress that acted past it: the human is leaving the screen —
	// or the screen is staying and the editor is opening elsewhere — and a ✗ from an earlier attempt
	// has nothing to say about the file they are about to edit.
	m.settings.failure = settingFailure{}
	m.layout()
	if launch.Detached {
		return m, startDetachedEditor(row.Path, argv)
	}
	return m, tea.ExecProcess(exec.Command(argv[0], argv[1:]...), func(err error) tea.Msg {
		return settingsEditedMsg{path: row.Path, err: err}
	})
}

// startDetachedEditor is the launch that keeps the terminal: the program is started with no stdin,
// stdout or stderr of ours — a nil stream in [exec.Cmd] is the null device, so the editor cannot
// write over the frame we are still drawing and cannot read the keys we are still routing — and
// nothing waits for it.
//
// The Wait in the background is not a wait FOR the editor, it is the reaping of it: a child nobody
// waits for stays a zombie in the process table for as long as apogee runs, and a human who opens
// their config a dozen times in a session should not leave a dozen of them. It carries no outcome —
// what the editor did to the file is the watcher's to notice, and its exit code is an answer to a
// question the pane stopped asking the moment it let go.
func startDetachedEditor(path string, argv []string) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command(argv[0], argv[1:]...)
		if err := cmd.Start(); err != nil {
			return settingsDetachedMsg{path: path, err: err}
		}
		go func() { _ = cmd.Wait() }()
		return settingsDetachedMsg{path: path}
	}
}

// foldDetachedEdit is the whole of what a detached launch does to the pane: nothing, plus a sentence.
// A start that failed is this pane's one failure slot, on the row the human pressed ⏎ on — the same
// place every other refusal in here lands — and a start that worked is an act that landed and changed
// no row, which is what the answer slot is for ([settingAnswer]).
//
// No re-read follows either way. The editor is still open; the file is not the pane's to interpret
// until somebody saves it, and then it is the watcher that says so (ADR 0041 decision 3).
func (m Model) foldDetachedEdit(msg settingsDetachedMsg) (tea.Model, tea.Cmd) {
	rows := m.settingRows()
	launched, ok := settingRowOf(rows, msg.path)
	if !ok {
		launched = SettingRow{Path: msg.path}
	}
	if msg.err != nil {
		return m.settingsFailed(launched, stripEscapes(msg.err.Error()))
	}
	m.settings.answer = settingAnswer{path: msg.path, msg: settingsDetachedEditNote}
	m.layout()
	return m, nil
}

// foldSettingsEdit is what happens when the editor exits: the binary re-reads the file, and every
// key that came back different is journaled and applied — through the same two homes an in-pane
// commit uses (settingsApplied), so a key edited in the file and a key edited on its row land
// identically and neither has an apply path of its own.
//
// The editor's own failure ends the round trip without a re-read. A command that could not run
// changed nothing, and a non-zero exit is how an editor SAYS to discard (`:cq`) — re-reading over
// either would be answering a question the human declined to ask. Same for a reload that could not
// parse or validate what it found: the file is left exactly as they wrote it and the reason lands on
// the row they launched from, which is where they go back in from.
//
// Notes are per key, on the edit that earned them; a refusal is the pane's one failure slot, so a
// reload in which two keys both refused shows the last of them — the slot describes the last attempt
// rather than a row's condition ([settingFailure]), and this is one attempt.
func (m Model) foldSettingsEdit(msg settingsEditedMsg) (tea.Model, tea.Cmd) {
	rows := m.settingRows()
	launched, ok := settingRowOf(rows, msg.path)
	if !ok {
		launched = SettingRow{Path: msg.path}
	}
	if msg.err != nil {
		return m.settingsFailed(launched, stripEscapes(msg.err.Error()))
	}
	if m.opts.ReloadConfig == nil {
		return m.settingsFailed(launched, noExternalEditNote)
	}
	applied, err := m.opts.ReloadConfig()
	if err != nil {
		return m.settingsFailed(launched, stripEscapes(err.Error()))
	}
	m, cmds := m.applyReloaded(rows, applied)
	m.layout()
	return m, tea.Batch(cmds...)
}

// applyReloaded journals and applies every key a re-read found changed. It is the one apply loop the
// round trip's TWO triggers share (ADR 0041 decision 6: one apply path, two triggers) — an editor
// that exited, and a file the watcher saw change — so a key can never land one way when the human
// edited it in a terminal editor and another way when they saved it from a GUI one.
//
// What the applies ask for is BATCHED rather than kept one at a time: an edit that changed the colour
// scheme and the scroll bar in one session of the editor has to leave with the scheme's repaint still
// asked for.
func (m Model) applyReloaded(rows []SettingRow, applied []AppliedSetting) (Model, []tea.Cmd) {
	var cmds []tea.Cmd
	for _, a := range applied {
		row, ok := settingRowOf(rows, a.Path)
		if !ok {
			continue // a key the pane does not list has no row to journal it on
		}
		next, cmd := m.settingsApplied(row, settingEdit{path: a.Path, value: a.Value})
		m = next
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return m, cmds
}

// configChangedMsg is one report from the binary's config watcher: the file changed, or — alive
// false — the WATCH itself ended and nothing more will ever be reported. It carries no path and no
// keys, because the watcher knows neither: what changed is [Options.ReloadConfig]'s answer, and the
// message is only the news that it is worth asking (ADR 0041 decision 3).
type configChangedMsg struct{ alive bool }

// configWatchState is what the Model remembers between reports: how many re-reads in a row could not
// be made, and whether the human has been told about it. Two plain fields in a plain value, safe in
// the value-copied Model (ADR 0011).
type configWatchState struct {
	// fails counts CONSECUTIVE unreadable re-reads; a re-read that lands clears it.
	fails int
	// noted records that the note below has already been said for this run of failures, so the same
	// broken file cannot narrate itself once per save.
	noted bool
}

// configWatchStallReports is how many consecutive unreadable re-reads it takes before the transcript
// says so (ADR 0041 decision 7). Fewer would report the ordinary case — an editor whose save the
// watcher happened to catch mid-write — as a problem the human has to do something about.
const configWatchStallReports = 3

// configWatchStalledNote is what the transcript says when it does. It names the consequence rather
// than the event, because a file that does not parse is not itself news: what the human needs to know
// is that the session is NOT running what they just saved, and why.
const configWatchStalledNote = "the config file has not parsed for three saves, so the session is " +
	"still running the settings it had: "

// awaitConfigChange opens ONE wait on the binary's config watcher (ADR 0041 decision 3). It is the
// whole of this chain's arming: Init opens the first wait and each landed report opens the next, so
// there is exactly one wait outstanding at any moment (doc.go's tick-chain invariant — two would
// re-read the file twice for every save and apply everything twice).
//
// It takes the program context, as [Model.beatCmd] does, so a quit ends the wait where it stands
// rather than leaving it parked on a channel until the composition root's teardown reaches the
// watcher. nil seam ⇒ no Cmd and therefore no chain, the nil-seam degrade every provider here takes.
func (m Model) awaitConfigChange() tea.Cmd {
	await := m.opts.AwaitConfigChange
	if await == nil {
		return nil
	}
	ctx := m.parent
	return func() tea.Msg {
		return configChangedMsg{alive: await(ctx)}
	}
}

// foldConfigChanged is what a saved config file does to a running session: the same re-read, the same
// journal and the same applies an editor's exit produces (applyReloaded), for a save this program had
// nothing to do with (ADR 0041 decision 5).
//
// It runs whether or not the pane is open and whether or not a Step is streaming, and deliberately:
// the human saved a document, and the keys that cannot land right now refuse on their own rows
// through the very seams that know how to refuse — the same posture an in-pane commit takes mid-run.
// What it must NOT do is apply twice, which is what the baseline refresh on every pane write buys
// (ADR 0041 decision 8, in the binary): a key apogee itself just wrote comes back as no change at all.
//
// The next wait is opened before anything is applied, so a re-read that ends in a refusal still leaves
// the session watching — a broken config the human is about to fix is exactly the file the next report
// has to be about. A watch that has ENDED arms nothing: there is no report to wait for any more.
func (m Model) foldConfigChanged(msg configChangedMsg) (tea.Model, tea.Cmd) {
	if !msg.alive {
		return m, nil
	}
	next := m.awaitConfigChange()
	if m.opts.ReloadConfig == nil {
		return m, next
	}
	applied, err := m.opts.ReloadConfig()
	if err != nil {
		return m.foldConfigUnreadable(err), next
	}
	m.cfgWatch = configWatchState{}
	m, cmds := m.applyReloaded(m.settingRows(), applied)
	m.layout()
	return m, tea.Batch(append(cmds, next)...)
}

// foldConfigUnreadable is the last-good rule's half of the fold (ADR 0041 decision 7): a file that
// does not parse or does not validate applies NOTHING and moves nothing — the binary keeps the
// baseline it had, so the human's fix is still diffed against the config that was last good.
//
// The failure is silent until it has survived configWatchStallReports saves, and then it is said
// once. A watcher will inevitably read a file somebody is halfway through writing, so the first
// failures are normal and self-correcting; a note per report would be an error scrolling past every
// time somebody hits save while they are still typing. It goes to the TRANSCRIPT rather than to a row
// because there is no row: nobody pressed anything, and the pane is very likely not even open.
func (m Model) foldConfigUnreadable(err error) Model {
	m.cfgWatch.fails++
	if m.cfgWatch.fails < configWatchStallReports || m.cfgWatch.noted {
		return m
	}
	m.cfgWatch.noted = true
	m.transcript.addNote(configWatchStalledNote + err.Error())
	m.layout()
	return m
}

// settingRowOf finds the row for a registry path in the list as it stands — the lookup both halves
// of the round trip need, since what comes back from the binary is a path and what the journal and
// the apply are about is a row.
func settingRowOf(rows []SettingRow, path string) (SettingRow, bool) {
	for _, r := range rows {
		if r.Path == path {
			return r, true
		}
	}
	return SettingRow{}, false
}
