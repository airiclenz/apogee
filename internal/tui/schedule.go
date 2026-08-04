package tui

import (
	"errors"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/schedule"
)

// ----------------------------------------------------------------------------
// /schedule and /schedule-stop — the standing-instruction surface (ADR 0033)
// ----------------------------------------------------------------------------
//
// The TUI's half of the scheduler, and it is deliberately the smaller half: this file DISPLAYS
// Schedules and COLLECTS the three answers a new one needs (prompt, cycle, mode), while every
// when-and-how decision — the cycle floor, the mode validation, the overlap policy, the naming of
// an unnamed Schedule, the Gate the tick waits on — belongs to internal/schedule behind the
// [Scheduler] seam. That split is what keeps the library sufficient for a Driver that has no
// terminal at all (ADR 0031), so nothing here re-checks a rule the library owns: the surface asks,
// and words the answer it gets back.
//
// The verbs are both live while a worker works (commandSpecs). Creating or stopping a Schedule
// touches no engine and opens no Exchange — a Firing is a separate headless run over a fresh Agent
// — so there is no boundary to wait for, and the contention that WOULD matter (a tick landing
// mid-Exchange) is the host Gate's business rather than this table's.
//
// Nothing here announces its own success. A created or stopped Schedule is confirmed by the
// scheduler's own Event arriving as a [scheduleEventMsg] (scheduleEventNote), so the narration of a
// Schedule's whole life — created, fired, completed, skipped, stopped, failed — reads as one voice
// instead of two that can drift. What this side words is only what the library refused, and what it
// cannot do at all.

// scheduleUsage is the one-line grammar every /schedule refusal carries — the confineUsage posture,
// so a mistyped line teaches the three working forms instead of vanishing.
const scheduleUsage = "usage: /schedule [<cycle> [auto]] <prompt> — bare lists what is live"

// noSchedulerNote is what both verbs earn on a build that wired no scheduler. It is a note and not
// an error (the nil-seam posture [Options.GenerateTitle] set): an unwired capability is a fact about
// this build, not a mistake the human made.
const noSchedulerNote = "scheduling is unavailable — this build wired no scheduler"

// autoToken is the literal word that selects auto mode in the argument form. It is a WORD rather
// than a flag because the prompt follows it: "--auto" would invite "--plan", and the mode a
// Schedule runs in is a choice with exactly two answers, one of which is the default.
const autoToken = "auto"

// scheduleCycles are the cycle picker's presets, shortest first. They are a TASTE — the argument
// form takes any Go duration the library's floor accepts — chosen so the list spans a working day
// without becoming a scroll: a minute for something being watched, four hours for something being
// kept an eye on.
var scheduleCycles = []time.Duration{
	time.Minute,
	5 * time.Minute,
	15 * time.Minute,
	30 * time.Minute,
	time.Hour,
	4 * time.Hour,
}

// scheduleModes are the two modes a Schedule may run in, least privilege first (ADR 0033,
// decision 2). Ask-Before and Allow-Edits are absent by construction: both exist to consult a
// human, and a Firing has none — its Approver denies rather than parks, so a Schedule in either of
// them would be a Schedule whose every gated action fails.
var scheduleModes = []domain.Mode{domain.ModePlan, domain.ModeAuto}

// unavailableRowCell marks the auto row the Auto-eligibility ladder has closed on this host — the
// currentRowCell posture (a plain-text cell of its own, because the popup module styles rows whole
// and aligns them by column). The row stays OFFERED rather than hidden: "auto is not available
// here" is a fact worth reading, and a menu that silently loses a row teaches nothing.
const unavailableRowCell = "· unavailable"

// scheduleDraft is the half-built Schedule the two /schedule popups carry between them: the prompt
// the human already typed and the cycle the first popup took. Plain values, so the overlay state it
// lives in stays copyable with the Model (ADR 0011).
type scheduleDraft struct {
	prompt string
	cycle  time.Duration
}

// ----------------------------------------------------------------------------
// The verbs
// ----------------------------------------------------------------------------

// runSchedule drives /schedule in its three forms, told apart by the line's first token:
//
//   - nothing at all → the status surface, one note listing what is live;
//   - a Go duration ("15m") → a direct create, with an optional "auto" token before the prompt;
//   - anything else → the popup path, where the cycle and the mode are picked in that order.
//
// It reads the line's RAW tail rather than its tokens (parsedInput.rest): the prompt is text a human
// wrote and a Firing submits verbatim, so re-joining split tokens would silently re-space it.
//
// A first token that LOOKS like a cycle attempt and is not one is refused instead of being taken for
// prose. "/schedule 5x tidy the logs" is a typo with an obvious fix, and quietly opening a cycle
// picker over the prompt "5x tidy the logs" would answer a question the human did not ask.
func (m Model) runSchedule(line string) (tea.Model, tea.Cmd) {
	if m.opts.Schedules == nil {
		return m.pickerNote(noSchedulerNote)
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return m.pickerNote(scheduleStatusNote(m.liveSchedules()))
	}

	first, tail := cutToken(line)
	if cycle, err := time.ParseDuration(first); err == nil {
		return m.createFromArgs(cycle, tail)
	}
	if looksLikeCycle(first) {
		return m.pickerNote(fmt.Sprintf(
			"%q is not a cycle — write it as a Go duration (30s, 15m, 2h). %s", first, scheduleUsage))
	}
	// The prompt-only form: the whole line is the instruction, and the two popups collect the rest.
	m.picker = picker{open: true, kind: pickerCycle, draft: scheduleDraft{prompt: line}}
	m.layout()
	return m, nil
}

// createFromArgs completes the argument form once its cycle has parsed: an optional literal "auto"
// selects the mode, and everything after it is the prompt. Auto is gated here exactly as the picker
// row is gated, from the one value that carries both the verdict and its reason
// ([Options.ScheduleAutoBlocked]) — the arg form and the popup cannot disagree about a host.
func (m Model) createFromArgs(cycle time.Duration, tail string) (tea.Model, tea.Cmd) {
	mode, prompt := domain.ModePlan, tail
	if word, rest := cutToken(tail); word == autoToken {
		if m.opts.ScheduleAutoBlocked != "" {
			return m.pickerNote(autoBlockedNote(m.opts.ScheduleAutoBlocked))
		}
		mode, prompt = domain.ModeAuto, rest
	}
	return m.createSchedule(schedule.Spec{Cycle: cycle, Mode: mode, Prompt: prompt})
}

// createSchedule is the accept path both forms share — the argument line and the second popup. It
// closes the overlay (harmless for the argument form, which never opened one) and hands the Spec to
// the library, which owns every rule that can refuse it.
//
// Success says nothing: the scheduler's own EventCreated is the confirmation, and it arrives with
// the cycle, the mode and the next fire time this side would otherwise have to restate.
func (m Model) createSchedule(spec schedule.Spec) (tea.Model, tea.Cmd) {
	m.picker = picker{}
	if _, err := m.opts.Schedules.Add(spec); err != nil {
		return m.pickerNote(scheduleAddNote(err))
	}
	m.layout()
	return m, nil
}

// runScheduleStop drives /schedule-stop. It takes no argument because the choice is a live
// Schedule rather than a name the human is expected to have kept: with exactly one running there is
// nothing to choose and it stops, with none there is nothing to stop and it says so, and with
// several the picker asks which.
func (m Model) runScheduleStop() (tea.Model, tea.Cmd) {
	if m.opts.Schedules == nil {
		return m.pickerNote(noSchedulerNote)
	}
	live := m.liveSchedules()
	switch len(live) {
	case 0:
		return m.pickerNote("no schedules are running — nothing to stop")
	case 1:
		return m.stopSchedule(live[0])
	}
	m.picker = picker{open: true, kind: pickerScheduleStop}
	m.layout()
	return m, nil
}

// stopSchedule takes one Schedule off the clock, from the single-schedule shortcut and from the
// picker's accept alike. Like createSchedule it stays silent on success — EventStopped is the
// answer — and words only the failure, which at this point can only be a Schedule that stopped
// itself between the list and the keypress.
func (m Model) stopSchedule(st schedule.Status) (tea.Model, tea.Cmd) {
	m.picker = picker{}
	if err := m.opts.Schedules.Stop(st.ID); err != nil {
		return m.pickerNote("could not stop " + st.Name + ": " + err.Error())
	}
	m.layout()
	return m, nil
}

// acceptScheduleStop takes the stop picker's highlighted row. It RE-READS the live set instead of
// trusting the index the clamp was computed against: unlike every other offering the overlay lists,
// these rows come from a seam another goroutine owns, so the two reads of one keypress can honestly
// differ. A row that is no longer there is answered rather than indexed — the Schedule the human
// aimed at has already ended, which is what they were asking for.
func (m Model) acceptScheduleStop() (tea.Model, tea.Cmd) {
	live := m.liveSchedules()
	if m.picker.selected < 0 || m.picker.selected >= len(live) {
		m.picker = picker{}
		return m.pickerNote("that schedule is no longer running")
	}
	return m.stopSchedule(live[m.picker.selected])
}

// acceptCycle takes the cycle picker's row and asks the second question in the same overlay: the
// draft grows a cycle, the kind moves on, and the highlight returns to the first row (plan — the
// least-privilege default, which is also the answer for a human who just presses ⏎ twice).
func (m Model) acceptCycle(cycle time.Duration) (tea.Model, tea.Cmd) {
	m.picker.kind, m.picker.selected = pickerScheduleMode, 0
	m.picker.draft.cycle = cycle
	m.layout()
	return m, nil
}

// acceptScheduleMode takes the mode picker's row and creates the Schedule the draft describes.
//
// The auto row is offered even where it is unavailable, so taking it must answer rather than act:
// the reason goes into the transcript and the pane STAYS OPEN, which lets the human pick plan
// without retyping the prompt they had already typed once.
func (m Model) acceptScheduleMode(mode domain.Mode) (tea.Model, tea.Cmd) {
	if mode == domain.ModeAuto && m.opts.ScheduleAutoBlocked != "" {
		m.transcript.addNote(autoBlockedNote(m.opts.ScheduleAutoBlocked))
		m.layout()
		return m, nil
	}
	draft := m.picker.draft
	return m.createSchedule(schedule.Spec{Cycle: draft.cycle, Mode: mode, Prompt: draft.prompt})
}

// liveSchedules is what the status note, the stop picker's rows and its accept all read — one
// answer to "what is live right now", asked fresh each time rather than captured, so a pane open
// across a Firing shows what the scheduler holds at that frame. An unwired seam knows nothing,
// which every caller renders as "none".
func (m Model) liveSchedules() []schedule.Status {
	if m.opts.Schedules == nil {
		return nil
	}
	return m.opts.Schedules.List()
}

// ----------------------------------------------------------------------------
// The activity report (the host Gate's half)
// ----------------------------------------------------------------------------

// reportActivity publishes next's activity through [Options.ReportActivity] when it has MOVED since
// the last report, and returns the model carrying the new high-water mark. [Model.Update] defers it,
// so every fold in the program passes through here exactly once and no transition can be forgotten.
//
// It takes and returns a tea.Model because that is what a deferred rewrite of Update's return value
// can see. Anything that is not a Model is handed straight back untouched — an impossible case
// today (every arm returns a Model), and the safe answer if that ever stops being true.
func reportActivity(next tea.Model) tea.Model {
	m, ok := next.(Model)
	if !ok {
		return next
	}
	busy := !m.quiescent()
	if busy == m.activityBusy {
		return next
	}
	m.activityBusy = busy
	if m.opts.ReportActivity != nil {
		m.opts.ReportActivity(busy)
	}
	return m
}

// quiescent reports whether nothing this session owns is in flight: no worker drives the engine
// (m.busy covers a running Exchange and both blocked rendezvous), no launcher verb owns the server,
// and no row the human typed is still waiting to go out.
//
// The three terms are the three ways a Firing could collide with the human's own work. The first is
// the Exchange, and it is deliberately the WHOLE Exchange rather than a Turn boundary (ADR 0025).
// The second is the actuation latch: a profile load restarts the very server a Firing would dial,
// and the Model already pairs it with busy() wherever it asks "is the engine mine right now"
// (observeBinding). The third is the interjection queue: rows held over from a stop or a fault are a
// message the next ⏎ sends, so the session is between two halves of one thought rather than done.
//
// stateErrored with nothing queued IS quiescent: the worker has unwound and the engine is idle, and
// a human reading a failure is not a reason to hold a standing instruction back.
func (m Model) quiescent() bool {
	return !m.busy() && !m.actuation.inFlight && len(m.pendingInterjections) == 0
}

// ----------------------------------------------------------------------------
// The event fold
// ----------------------------------------------------------------------------

// foldScheduleEvent records one thing that happened to a Schedule as a transcript note. It moves
// nothing else: a Firing is a separate run with its own session record, so its narration is
// scrollback here and never state.
//
// The notes are PERSISTED (addNote, not addEphemeralNote): each records something that actually
// happened in this session's lifetime — the ADR 0022 addendum's test — unlike the re-derived chrome
// a resume rebuilds for itself.
func (m Model) foldScheduleEvent(ev schedule.Event) Model {
	m.transcript.addNote(m.scheduleEventNote(ev))
	return m
}

// scheduleEventNote words one Event. The Event carries identity and outcome only, so the created
// line reads the rest off the live listing — the Schedule is on the clock by the time its
// EventCreated is folded, and stating the cycle, the mode and the next fire is what makes the
// confirmation worth reading. A Schedule that is somehow gone by then still gets its line, shorter.
func (m Model) scheduleEventNote(ev schedule.Event) string {
	head := "schedule " + ev.ScheduleName + " — "
	switch ev.Kind {
	case schedule.EventCreated:
		if st, ok := m.scheduleByID(ev.ScheduleID); ok {
			return head + strings.Join([]string{
				"every " + formatCycle(st.Cycle), modeLabel(st.Mode), "next " + clockOf(st.NextFire),
			}, " · ")
		}
		return head + "created"
	case schedule.EventFired:
		return head + "firing now"
	case schedule.EventCompleted:
		if ev.Outcome.Title != "" {
			return head + "finished: " + ev.Outcome.Title
		}
		return head + "finished"
	case schedule.EventSkipped:
		return head + "tick skipped — the previous run is still going"
	case schedule.EventStopped:
		return head + "stopped"
	case schedule.EventFailed:
		return head + "failed: " + scheduleErrText(ev.Err)
	}
	return head + string(ev.Kind)
}

// scheduleByID finds one live Schedule's Status. It is the created note's lookup and nothing else's:
// every other Event says all it needs to say on its own.
func (m Model) scheduleByID(id string) (schedule.Status, bool) {
	for _, st := range m.liveSchedules() {
		if st.ID == id {
			return st, true
		}
	}
	return schedule.Status{}, false
}

// scheduleErrText is the sentence a failure carries. A nil error cannot reach a failed Event from
// the library, but a note reading "failed: " with nothing after it would be worse than the guess.
func scheduleErrText(err error) string {
	if err == nil {
		return "no reason given"
	}
	return err.Error()
}

// ----------------------------------------------------------------------------
// The rendered notes and rows (pure)
// ----------------------------------------------------------------------------

// scheduleStatusNote renders bare /schedule: a header counting what is live and one row per
// Schedule, each stating the four things a standing instruction is judged by — how often, in which
// mode, when next, and how it has been going. Skips and the in-flight mark appear only when they
// are true, so an ordinary row stays short.
//
// It is pure (the confine.go posture) so the wording is table-testable without a Model.
func scheduleStatusNote(live []schedule.Status) string {
	if len(live) == 0 {
		return "no schedules are running. " + scheduleUsage
	}
	lines := make([]string, 0, len(live)+1)
	lines = append(lines, fmt.Sprintf("/schedule — %d live", len(live)))
	for _, st := range live {
		cells := []string{st.Name, "every " + formatCycle(st.Cycle), modeLabel(st.Mode)}
		if !st.NextFire.IsZero() {
			cells = append(cells, "next "+clockOf(st.NextFire))
		}
		cells = append(cells, fmt.Sprintf("%d fired", st.Fired))
		if st.Skipped > 0 {
			cells = append(cells, fmt.Sprintf("%d skipped", st.Skipped))
		}
		if st.InFlight {
			cells = append(cells, "running now")
		}
		lines = append(lines, "  "+strings.Join(cells, " · "))
	}
	return strings.Join(lines, "\n")
}

// scheduleAddNote words what the library refused. The three conditions it names are the ones a
// human can act on; anything else is reported as it came, because a surface that paraphrases an
// error it does not recognise is a surface that eventually lies about one.
func scheduleAddNote(err error) string {
	switch {
	case errors.Is(err, schedule.ErrPrompt):
		return "a schedule needs a prompt. " + scheduleUsage
	case errors.Is(err, schedule.ErrCycle):
		return fmt.Sprintf(
			"a schedule cycle must be at least %s — this session and every firing share one model slot",
			formatCycle(schedule.MinCycle))
	case errors.Is(err, schedule.ErrMode):
		return "a schedule runs in plan or auto only — the other modes stop to ask a human, and a firing has none"
	}
	return "could not create the schedule: " + err.Error()
}

// autoBlockedNote is the refusal an auto Schedule earns on a host where auto is not eligible — the
// same ladder that gates launching in auto (ADR 0033, decision 3). It names the reason the binary
// gave and points at the mode that still works, because the prompt is worth scheduling either way.
func autoBlockedNote(reason string) string {
	return "a schedule cannot run in auto here: " + reason +
		"\n  leave the auto out and it runs in plan — read-only, but it runs"
}

// cycleRows is one row per preset: the duration as the argument form spells it, and the same fact in
// words. Two cells, so the glosses line up in one column under the shortest and the longest cycle
// alike.
func cycleRows() []popupRow {
	rows := make([]popupRow, 0, len(scheduleCycles))
	for _, cycle := range scheduleCycles {
		rows = append(rows, popupRow{formatCycle(cycle), "— " + cycleGloss(cycle)})
	}
	return rows
}

// scheduleModeRows is one row per offerable mode: the mode's own label, what it means for an
// unattended run, and — on auto where the host's ladder has closed it — the unavailable mark.
func scheduleModeRows(autoBlocked string) []popupRow {
	rows := make([]popupRow, 0, len(scheduleModes))
	for _, mode := range scheduleModes {
		blocked := ""
		if mode == domain.ModeAuto && autoBlocked != "" {
			blocked = unavailableRowCell
		}
		rows = append(rows, popupRow{modeLabel(mode), "— " + scheduleModeGloss(mode), blocked})
	}
	return rows
}

// scheduleModeGloss says what each mode costs and buys inside a Firing, which is not quite what it
// means in this session: nothing here can ask a human, so auto's promise is that it acts and plan's
// is that it cannot.
func scheduleModeGloss(mode domain.Mode) string {
	if mode == domain.ModeAuto {
		return "acts unattended — confined, and gated actions fail rather than ask"
	}
	return "read-only — it reads and reports, and writes nothing"
}

// scheduleStopRows is one row per live Schedule: its name, its cycle, its mode, and the running mark
// on the one that is firing right now — the fact most worth seeing when choosing what to stop, since
// stopping ends the cycle and leaves the run in flight to finish (ADR 0033).
func scheduleStopRows(live []schedule.Status) []popupRow {
	rows := make([]popupRow, 0, len(live))
	for _, st := range live {
		running := ""
		if st.InFlight {
			running = runningRowCell
		}
		rows = append(rows, popupRow{
			stripEscapes(st.Name), "— every " + formatCycle(st.Cycle), "· " + modeLabel(st.Mode), running,
		})
	}
	return rows
}

// ----------------------------------------------------------------------------
// Formatting helpers (pure)
// ----------------------------------------------------------------------------

// formatCycle spells a cycle the way the argument form takes it — "90s", "15m", "1h30m" — rather
// than the way time.Duration prints it ("1h30m0s"). A zero-valued tier is dropped, and a cycle of
// exactly nothing still says "0s" instead of nothing at all.
func formatCycle(d time.Duration) string {
	d = d.Round(time.Second)
	parts := make([]string, 0, 3)
	if h := d / time.Hour; h > 0 {
		parts = append(parts, fmt.Sprintf("%dh", h))
		d -= h * time.Hour
	}
	if mins := d / time.Minute; mins > 0 {
		parts = append(parts, fmt.Sprintf("%dm", mins))
		d -= mins * time.Minute
	}
	if secs := d / time.Second; secs > 0 || len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%ds", secs))
	}
	return strings.Join(parts, "")
}

// cycleGloss says a preset out loud, for the picker's second column.
func cycleGloss(d time.Duration) string {
	switch {
	case d == time.Hour:
		return "every hour"
	case d == time.Minute:
		return "every minute"
	case d >= time.Hour && d%time.Hour == 0:
		return fmt.Sprintf("every %d hours", d/time.Hour)
	case d >= time.Minute && d%time.Minute == 0:
		return fmt.Sprintf("every %d minutes", d/time.Minute)
	}
	return "every " + formatCycle(d)
}

// clockOf renders a fire time as the local wall clock, to the minute — the same spelling a Firing's
// record title carries (internal/run), so a note about the next run and the record the last one left
// behind read in one language.
func clockOf(t time.Time) string {
	return t.Local().Format("15:04")
}

// cutToken splits s into its first whitespace-delimited token and everything after it, with the
// separating whitespace removed and the remainder otherwise UNTOUCHED — the property the prompt
// depends on, since it is submitted to a model exactly as it was typed.
func cutToken(s string) (token, rest string) {
	s = strings.TrimLeft(s, " \t\n\r")
	i := strings.IndexAny(s, " \t\n\r")
	if i < 0 {
		return s, ""
	}
	return s[:i], strings.TrimLeft(s[i:], " \t\n\r")
}

// looksLikeCycle reports whether a token that failed to parse as a duration was plainly MEANT as
// one — it opens with a digit. It is the narrowest rule that separates a typo from prose: a prompt
// beginning with a number ("5 things to check in the logs") is refused by it too, and that is the
// right trade, since the refusal names the token and teaches the grammar.
func looksLikeCycle(token string) bool {
	return token != "" && token[0] >= '0' && token[0] <= '9'
}
