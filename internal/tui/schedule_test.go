package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/schedule"
)

// ----------------------------------------------------------------------------
// /schedule harness
// ----------------------------------------------------------------------------
//
// The surface is driven the way every other overlay is — synthetic Msgs into Update, asserting the
// transcript, the overlay state, and what the seam was called with. The seam is a fake rather than a
// live scheduler on purpose: what this package owns is the display and the three answers a Schedule
// needs, so a test that started real tickers would be testing internal/schedule's policy twice.

// fakeScheduler records what the surface asked of the scheduler and answers with what a test posed.
type fakeScheduler struct {
	added   []schedule.Spec
	stopped []string
	live    []schedule.Status
	addErr  error
	stopErr error
	minted  int
}

func (f *fakeScheduler) Add(spec schedule.Spec) (string, error) {
	if f.addErr != nil {
		return "", f.addErr
	}
	f.added = append(f.added, spec)
	f.minted++
	id := fmt.Sprintf("sch-%d", f.minted)
	name := spec.Name
	if name == "" {
		name = spec.Prompt // the library derives one; the fake's stand-in is enough to identify a row
	}
	f.live = append(f.live, schedule.Status{
		ID: id, Name: name, Cycle: spec.Cycle, Mode: spec.Mode,
		NextFire: time.Date(2026, 8, 4, 14, 5, 0, 0, time.Local),
	})
	return id, nil
}

func (f *fakeScheduler) Stop(id string) error {
	if f.stopErr != nil {
		return f.stopErr
	}
	f.stopped = append(f.stopped, id)
	for i, st := range f.live {
		if st.ID == id {
			f.live = append(f.live[:i], f.live[i+1:]...)
			break
		}
	}
	return nil
}

func (f *fakeScheduler) List() []schedule.Status { return f.live }

// scheduleModel is a ready, idle model with the scheduler seam wired.
func scheduleModel(t *testing.T, sch *fakeScheduler, autoBlocked string) Model {
	t.Helper()
	opts := testOpts
	opts.Schedules = sch
	opts.ScheduleAutoBlocked = autoBlocked
	return newTestModelEng(t, &fakeEngine{}, opts)
}

// liveStatus is one row of a posed offering.
func liveStatus(id, name string, cycle time.Duration, mode domain.Mode) schedule.Status {
	return schedule.Status{
		ID: id, Name: name, Cycle: cycle, Mode: mode,
		NextFire: time.Date(2026, 8, 4, 14, 5, 0, 0, time.Local),
	}
}

// ----------------------------------------------------------------------------
// The argument form
// ----------------------------------------------------------------------------

// The three shapes of "/schedule <cycle> [auto] <prompt>": the cycle is a Go duration, the optional
// literal "auto" picks the mode, and everything after them is the prompt — carried VERBATIM, because
// a Firing submits it as typed and re-spacing a human's text is not the parser's business.
func TestScheduleArgFormCreates(t *testing.T) {
	for _, tc := range []struct {
		name, line string
		wantCycle  time.Duration
		wantMode   domain.Mode
		wantPrompt string
	}{
		{"plain", "/schedule 15m tidy the logs", 15 * time.Minute, domain.ModePlan, "tidy the logs"},
		{"auto token", "/schedule 1h auto tidy the logs", time.Hour, domain.ModeAuto, "tidy the logs"},
		{"seconds", "/schedule 45s watch the queue", 45 * time.Second, domain.ModePlan, "watch the queue"},
		{"spacing kept", "/schedule 1h check   the   logs", time.Hour, domain.ModePlan, "check   the   logs"},
		{"auto is not the prompt's first word", "/schedule 1h automate the report", time.Hour,
			domain.ModePlan, "automate the report"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sch := &fakeScheduler{}
			m := scheduleModel(t, sch, "")

			m, cmd := typeCommand(t, m, tc.line)
			if cmd != nil {
				t.Error("/schedule returned a Cmd; creating a schedule launches no worker")
			}
			if m.picker.open {
				t.Error("the argument form opened a picker; it has every answer already")
			}
			if len(sch.added) != 1 {
				t.Fatalf("Add calls = %d, want 1 (notes: %v)", len(sch.added), noteTexts(m))
			}
			got := sch.added[0]
			if got.Cycle != tc.wantCycle || got.Mode != tc.wantMode || got.Prompt != tc.wantPrompt {
				t.Errorf("spec = {cycle:%v mode:%v prompt:%q}, want {cycle:%v mode:%v prompt:%q}",
					got.Cycle, got.Mode, got.Prompt, tc.wantCycle, tc.wantMode, tc.wantPrompt)
			}
		})
	}
}

// A cycle under the library's floor is NOT pre-filtered here: the library owns policy (ADR 0033), so
// the surface hands the Spec over as typed and words the refusal it gets back — naming the floor,
// which is the one number the human needs to type a working line.
func TestScheduleSubFloorCycleReportsTheFloor(t *testing.T) {
	sch := &fakeScheduler{addErr: fmt.Errorf("%w (5s is under the 30s floor)", schedule.ErrCycle)}
	m := scheduleModel(t, sch, "")

	m, _ = typeCommand(t, m, "/schedule 5s hammer the server")

	if len(sch.added) != 0 {
		t.Errorf("Add recorded %d specs; a refused schedule must not be recorded", len(sch.added))
	}
	note := lastNote(m)
	if !strings.Contains(note, "30s") {
		t.Errorf("note = %q, want it to name the %v floor", note, schedule.MinCycle)
	}
}

// A first token that was plainly MEANT as a cycle and is not one is refused with the grammar rather
// than taken for prose — quietly opening the cycle picker over the prompt "5x tidy the logs" would
// answer a question nobody asked.
func TestScheduleMalformedCycleTeachesTheGrammar(t *testing.T) {
	sch := &fakeScheduler{}
	m := scheduleModel(t, sch, "")

	m, _ = typeCommand(t, m, "/schedule 5x tidy the logs")

	if m.picker.open {
		t.Error("a malformed cycle opened the picker; it should be refused")
	}
	if len(sch.added) != 0 {
		t.Errorf("Add calls = %d, want 0", len(sch.added))
	}
	note := lastNote(m)
	if !strings.Contains(note, `"5x"`) || !strings.Contains(note, scheduleUsage) {
		t.Errorf("note = %q, want it to name the token and carry the usage line", note)
	}
}

// The "auto" token answers to the same Auto-eligibility value the picker row does: on a host where
// the ladder has closed auto, the line is refused with the host's own reason and nothing is created.
func TestScheduleAutoTokenIsGated(t *testing.T) {
	sch := &fakeScheduler{}
	m := scheduleModel(t, sch, "this host cannot fence commands")

	m, _ = typeCommand(t, m, "/schedule 1h auto tidy the logs")

	if len(sch.added) != 0 {
		t.Fatalf("Add calls = %d, want 0 — an ineligible auto schedule must not be created", len(sch.added))
	}
	if note := lastNote(m); !strings.Contains(note, "this host cannot fence commands") {
		t.Errorf("note = %q, want the host's own reason", note)
	}
}

// ----------------------------------------------------------------------------
// The status surface
// ----------------------------------------------------------------------------

// Bare /schedule reports what is live: one row per Schedule stating how often, in which mode, when
// next, and how it has been going. Skips and the in-flight mark show only when true.
func TestScheduleBareListsWhatIsLive(t *testing.T) {
	busy := liveStatus("sch-2", "log watch", 15*time.Minute, domain.ModeAuto)
	busy.Fired, busy.Skipped, busy.InFlight = 12, 2, true
	sch := &fakeScheduler{live: []schedule.Status{
		liveStatus("sch-1", "nightly tidy", time.Hour, domain.ModePlan), busy,
	}}
	m := scheduleModel(t, sch, "")

	m, cmd := typeCommand(t, m, "/schedule")
	if cmd != nil {
		t.Error("bare /schedule returned a Cmd; it only reports")
	}
	note := lastNote(m)
	for _, want := range []string{
		"2 live", "nightly tidy", "every 1h", "plan", "next 14:05", "0 fired",
		"log watch", "every 15m", "auto", "12 fired", "2 skipped", "running now",
	} {
		if !strings.Contains(note, want) {
			t.Errorf("status note %q missing %q", note, want)
		}
	}
}

// With nothing live the same verb teaches the grammar instead of printing an empty table.
func TestScheduleBareWithNoneTeachesTheForm(t *testing.T) {
	m := scheduleModel(t, &fakeScheduler{}, "")

	m, _ = typeCommand(t, m, "/schedule")

	if note := lastNote(m); !strings.Contains(note, scheduleUsage) {
		t.Errorf("note = %q, want the usage line", note)
	}
}

// ----------------------------------------------------------------------------
// The popup path
// ----------------------------------------------------------------------------

// "/schedule <prompt>" asks the two questions the line did not answer, in order, and creates the
// Schedule at the second accept — carrying the prompt through both popups untouched.
func TestSchedulePromptOnlyWalksBothPickers(t *testing.T) {
	sch := &fakeScheduler{}
	m := scheduleModel(t, sch, "")

	m, _ = typeCommand(t, m, "/schedule summarise   today's commits")
	if !m.picker.open || m.picker.kind != pickerCycle {
		t.Fatalf("picker = {open:%v kind:%v}, want the cycle picker", m.picker.open, m.picker.kind)
	}
	if got, want := m.pickerTitle(), "schedule — how often"; got != want {
		t.Errorf("title = %q, want %q", got, want)
	}
	if got, want := len(m.pickerRows()), len(scheduleCycles); got != want {
		t.Errorf("cycle rows = %d, want %d presets", got, want)
	}

	m = step(t, m, keyDown()) // 1m → 5m
	m = step(t, m, keyEnter())
	if !m.picker.open || m.picker.kind != pickerScheduleMode {
		t.Fatalf("picker = {open:%v kind:%v}, want the mode picker", m.picker.open, m.picker.kind)
	}
	if m.picker.draft.cycle != 5*time.Minute {
		t.Errorf("draft cycle = %v, want 5m", m.picker.draft.cycle)
	}
	if len(sch.added) != 0 {
		t.Fatal("the cycle accept created a schedule; the mode question is still open")
	}

	m = step(t, m, keyEnter()) // plan, the pre-selected first row
	if m.picker.open {
		t.Error("the mode accept left the picker open")
	}
	if len(sch.added) != 1 {
		t.Fatalf("Add calls = %d, want 1", len(sch.added))
	}
	got := sch.added[0]
	if got.Cycle != 5*time.Minute || got.Mode != domain.ModePlan || got.Prompt != "summarise   today's commits" {
		t.Errorf("spec = %+v, want the picked cycle, plan, and the prompt as typed", got)
	}
}

// Esc closes the flow at either question and creates nothing — the draft dies with the overlay.
func TestSchedulePickerEscapeCreatesNothing(t *testing.T) {
	sch := &fakeScheduler{}
	m := scheduleModel(t, sch, "")

	m, _ = typeCommand(t, m, "/schedule tidy the logs")
	m = step(t, m, keyEnter()) // onto the mode question
	m = step(t, m, keyEsc())

	if m.picker.open || m.picker.draft.prompt != "" {
		t.Errorf("picker = %+v, want a closed overlay with no draft", m.picker)
	}
	if len(sch.added) != 0 {
		t.Errorf("Add calls = %d, want 0", len(sch.added))
	}
}

// The mode picker offers auto even where the ladder has closed it — the row carries the mark, and
// taking it answers with the reason and leaves the pane open, so plan is one keypress away and the
// prompt need not be retyped.
func TestScheduleModePickerGatesAuto(t *testing.T) {
	sch := &fakeScheduler{}
	m := scheduleModel(t, sch, "this host cannot fence commands")

	m, _ = typeCommand(t, m, "/schedule tidy the logs")
	m = step(t, m, keyEnter()) // cycle taken; the mode question is up

	rows := m.pickerRows()
	if len(rows) != 2 {
		t.Fatalf("mode rows = %d, want plan and auto", len(rows))
	}
	if strings.Contains(strings.Join(rows[0], " "), unavailableRowCell) {
		t.Errorf("plan row = %q, want it unmarked", rows[0])
	}
	if !strings.Contains(strings.Join(rows[1], " "), unavailableRowCell) {
		t.Errorf("auto row = %q, want the unavailable mark", rows[1])
	}

	m = step(t, m, keyDown()) // onto auto
	m = step(t, m, keyEnter())
	if !m.picker.open {
		t.Error("taking the blocked row closed the pane; plan must stay one keypress away")
	}
	if len(sch.added) != 0 {
		t.Fatalf("Add calls = %d, want 0", len(sch.added))
	}
	if note := lastNote(m); !strings.Contains(note, "this host cannot fence commands") {
		t.Errorf("note = %q, want the host's own reason", note)
	}
}

// With auto eligible the same row creates an auto Schedule.
func TestScheduleModePickerTakesAuto(t *testing.T) {
	sch := &fakeScheduler{}
	m := scheduleModel(t, sch, "")

	m, _ = typeCommand(t, m, "/schedule tidy the logs")
	m = step(t, m, keyEnter())
	m = step(t, m, keyDown())
	m = step(t, m, keyEnter())

	if len(sch.added) != 1 {
		t.Fatalf("Add calls = %d, want 1", len(sch.added))
	}
	if sch.added[0].Mode != domain.ModeAuto {
		t.Errorf("mode = %v, want auto", sch.added[0].Mode)
	}
}

// ----------------------------------------------------------------------------
// /schedule-stop
// ----------------------------------------------------------------------------

// The three shapes of /schedule-stop: nothing live is a note, exactly one live needs no question,
// and several open the picker whose accept stops the highlighted row.
func TestScheduleStopFormsByCount(t *testing.T) {
	t.Run("none", func(t *testing.T) {
		sch := &fakeScheduler{}
		m := scheduleModel(t, sch, "")

		m, _ = typeCommand(t, m, "/schedule-stop")

		if m.picker.open {
			t.Error("nothing live opened a picker")
		}
		if note := lastNote(m); !strings.Contains(note, "nothing to stop") {
			t.Errorf("note = %q, want it to say there is nothing to stop", note)
		}
	})

	t.Run("one", func(t *testing.T) {
		sch := &fakeScheduler{live: []schedule.Status{
			liveStatus("sch-1", "nightly tidy", time.Hour, domain.ModePlan),
		}}
		m := scheduleModel(t, sch, "")

		m, _ = typeCommand(t, m, "/schedule-stop")

		if m.picker.open {
			t.Error("one live schedule opened a picker; there is nothing to choose")
		}
		if want := []string{"sch-1"}; len(sch.stopped) != 1 || sch.stopped[0] != want[0] {
			t.Errorf("stopped = %v, want %v", sch.stopped, want)
		}
	})

	t.Run("several", func(t *testing.T) {
		sch := &fakeScheduler{live: []schedule.Status{
			liveStatus("sch-1", "nightly tidy", time.Hour, domain.ModePlan),
			liveStatus("sch-2", "log watch", 15*time.Minute, domain.ModeAuto),
		}}
		m := scheduleModel(t, sch, "")

		m, _ = typeCommand(t, m, "/schedule-stop")
		if !m.picker.open || m.picker.kind != pickerScheduleStop {
			t.Fatalf("picker = {open:%v kind:%v}, want the stop picker", m.picker.open, m.picker.kind)
		}
		if got, want := m.pickerTitle(), "stop a schedule"; got != want {
			t.Errorf("title = %q, want %q", got, want)
		}
		if rows := m.pickerRows(); len(rows) != 2 || !strings.Contains(strings.Join(rows[1], " "), "log watch") {
			t.Errorf("rows = %q, want one per live schedule", rows)
		}

		m = step(t, m, keyDown())
		m = step(t, m, keyEnter())
		if m.picker.open {
			t.Error("the accept left the picker open")
		}
		if want := []string{"sch-2"}; len(sch.stopped) != 1 || sch.stopped[0] != want[0] {
			t.Errorf("stopped = %v, want %v", sch.stopped, want)
		}
	})
}

// ----------------------------------------------------------------------------
// The nil seam and the running state
// ----------------------------------------------------------------------------

// A build with no scheduler wired answers both verbs with one honest note and no overlay — the
// nil-seam posture, never an error and never silence.
func TestScheduleWithoutASchedulerReportsIt(t *testing.T) {
	for _, line := range []string{"/schedule", "/schedule 1h tidy the logs", "/schedule-stop"} {
		t.Run(line, func(t *testing.T) {
			m := newTestModelEng(t, &fakeEngine{}, testOpts) // no Schedules seam

			m, cmd := typeCommand(t, m, line)
			if cmd != nil {
				t.Error("returned a Cmd; an unwired seam launches nothing")
			}
			if m.picker.open {
				t.Error("an unwired seam opened a picker")
			}
			if note := lastNote(m); note != noSchedulerNote {
				t.Errorf("note = %q, want %q", note, noSchedulerNote)
			}
		})
	}
}

// Both verbs are live while a worker works: they drive the scheduler library and never this
// session's engine. The popup path proves the whole flow answers keys mid-run, since an overlay that
// rendered without claiming them would be a modal the human could not close.
func TestScheduleRunsWhileTheWorkerWorks(t *testing.T) {
	sch := &fakeScheduler{}
	m := scheduleModel(t, sch, "")
	m.state = stateRunning

	m, _ = typeCommand(t, m, "/schedule tidy the logs")
	if !m.picker.open {
		t.Fatalf("picker did not open mid-run (notes: %v)", noteTexts(m))
	}
	m = step(t, m, keyEnter()) // cycle
	m = step(t, m, keyEnter()) // plan
	if len(sch.added) != 1 {
		t.Fatalf("Add calls = %d, want 1 — the pickers must answer keys while running", len(sch.added))
	}
	if m.state != stateRunning {
		t.Errorf("state = %v, want the running Exchange untouched", m.state)
	}
}

// ----------------------------------------------------------------------------
// The event notices
// ----------------------------------------------------------------------------

// Every Event the library emits reaches the scrollback as a note, and the created one reads the
// cycle, the mode and the next fire off the live listing — the Schedule is on the clock by the time
// its own creation is folded.
func TestScheduleEventsRenderAsNotes(t *testing.T) {
	sch := &fakeScheduler{live: []schedule.Status{
		liveStatus("sch-1", "nightly tidy", time.Hour, domain.ModePlan),
	}}
	m := scheduleModel(t, sch, "")

	for _, tc := range []struct {
		name  string
		event schedule.Event
		want  string
	}{
		{"created", schedule.Event{Kind: schedule.EventCreated, ScheduleID: "sch-1", ScheduleName: "nightly tidy"},
			"schedule nightly tidy — every 1h · plan · next 14:05"},
		{"created without a live row", schedule.Event{Kind: schedule.EventCreated, ScheduleID: "gone", ScheduleName: "ghost"},
			"schedule ghost — created"},
		{"fired", schedule.Event{Kind: schedule.EventFired, ScheduleName: "nightly tidy"},
			"schedule nightly tidy — firing now"},
		{"completed", schedule.Event{Kind: schedule.EventCompleted, ScheduleName: "nightly tidy",
			Outcome: schedule.Outcome{RecordID: "s1", Title: "nightly tidy — 14:05"}},
			"schedule nightly tidy — finished: nightly tidy — 14:05"},
		{"completed unpersisted", schedule.Event{Kind: schedule.EventCompleted, ScheduleName: "nightly tidy"},
			"schedule nightly tidy — finished"},
		{"skipped", schedule.Event{Kind: schedule.EventSkipped, ScheduleName: "nightly tidy"},
			"schedule nightly tidy — tick skipped — the previous run is still going"},
		{"stopped", schedule.Event{Kind: schedule.EventStopped, ScheduleName: "nightly tidy"},
			"schedule nightly tidy — stopped"},
		{"failed", schedule.Event{Kind: schedule.EventFailed, ScheduleName: "nightly tidy",
			Err: fmt.Errorf("upstream refused")},
			"schedule nightly tidy — failed: upstream refused"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			next := step(t, m, scheduleEventMsg{Event: tc.event})
			if got := lastNote(next); got != tc.want {
				t.Errorf("note = %q, want %q", got, tc.want)
			}
			if next.state != m.state {
				t.Errorf("state = %v, want it untouched by a notice", next.state)
			}
		})
	}
}

// The notices are PERSISTED: a Firing is something that happened in this session's lifetime, not
// re-derived chrome, so it survives the round trip through the record's transcript blob.
func TestScheduleNoticesSurviveTheTranscriptBlob(t *testing.T) {
	m := scheduleModel(t, &fakeScheduler{}, "")

	m = step(t, m, scheduleEventMsg{Event: schedule.Event{
		Kind: schedule.EventCompleted, ScheduleName: "nightly tidy",
		Outcome: schedule.Outcome{RecordID: "s1", Title: "nightly tidy — 14:05"},
	}})

	blob, err := encodeTranscript(&m.transcript)
	if err != nil {
		t.Fatalf("encodeTranscript: %v", err)
	}
	entries, err := decodeTranscript(blob)
	if err != nil {
		t.Fatalf("decodeTranscript: %v", err)
	}
	var kept bool
	for _, e := range entries {
		if e.kind == entryNote && strings.Contains(e.text, "nightly tidy — finished") {
			kept = true
		}
	}
	if !kept {
		t.Errorf("the completed notice did not survive the blob: %v", entries)
	}
}

// ----------------------------------------------------------------------------
// The pure formatters
// ----------------------------------------------------------------------------

// A cycle is spelled the way the argument form takes it, not the way time.Duration prints it.
func TestFormatCycle(t *testing.T) {
	for _, tc := range []struct {
		in   time.Duration
		want string
	}{
		{30 * time.Second, "30s"},
		{90 * time.Second, "1m30s"},
		{time.Minute, "1m"},
		{15 * time.Minute, "15m"},
		{time.Hour, "1h"},
		{90 * time.Minute, "1h30m"},
		{4 * time.Hour, "4h"},
		{0, "0s"},
	} {
		if got := formatCycle(tc.in); got != tc.want {
			t.Errorf("formatCycle(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// The cycle picker glosses each preset in words, singular where it should be.
func TestCycleRowsGlossEveryPreset(t *testing.T) {
	rows := cycleRows()
	if len(rows) != len(scheduleCycles) {
		t.Fatalf("rows = %d, want %d", len(rows), len(scheduleCycles))
	}
	for _, want := range []string{"1m — every minute", "5m — every 5 minutes", "1h — every hour", "4h — every 4 hours"} {
		var found bool
		for _, row := range rows {
			if strings.Join(row, " ") == want {
				found = true
			}
		}
		if !found {
			t.Errorf("rows %q missing %q", rows, want)
		}
	}
}

// The hint's verb follows the kind: nothing in /schedule's three pickers switches the session, and
// the stop picker's ⏎ ends something.
func TestPickerHintFollowsTheKind(t *testing.T) {
	for kind, want := range map[pickerKind]string{
		pickerModel:        pickerHint,
		pickerCycle:        "↑/↓ select · ⏎ choose · esc close",
		pickerScheduleMode: "↑/↓ select · ⏎ choose · esc close",
		pickerScheduleStop: "↑/↓ select · ⏎ stop · esc close",
	} {
		if got := pickerHintFor(kind); got != want {
			t.Errorf("pickerHintFor(%v) = %q, want %q", kind, got, want)
		}
	}
}

// The argument tail reaches the verb unsplit, so a multi-line prompt keeps its lines.
func TestScheduleTakesTheRawTail(t *testing.T) {
	parsed := parseInput("/schedule 1h check the log\nthen report", nil)
	if parsed.kind != kindCommand || parsed.command != "schedule" {
		t.Fatalf("parseInput = {kind:%v cmd:%q}, want the schedule command", parsed.kind, parsed.command)
	}
	if want := "1h check the log\nthen report"; parsed.rest != want {
		t.Errorf("rest = %q, want %q", parsed.rest, want)
	}
}

// A verb that does not read its arguments carries neither form, exactly as it never carried args.
func TestNonArgumentVerbsCarryNoTail(t *testing.T) {
	parsed := parseInput("/clear everything", nil)
	if parsed.rest != "" || parsed.args != nil {
		t.Errorf("parsed = {args:%v rest:%q}, want both empty", parsed.args, parsed.rest)
	}
}

// Compile-time proof that the live scheduler satisfies the seam the TUI drives — the SessionHost
// posture, so a library change that broke the surface fails here rather than in cmd/apogee.
var _ Scheduler = (*schedule.Scheduler)(nil)
