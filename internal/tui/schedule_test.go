package tui

import (
	"fmt"
	"slices"
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

// Every fire time this surface prints is spelled in the machine's OWN zone. The Status carries an
// instant, not a spelling — the Scheduler's Clock is the host's wall clock today, but nothing in the
// type says so and a UTC-located instant reaching a row must still read as the human's clock — so
// the conversion is pinned here rather than left to whatever zone the seam happened to hand over.
//
// The fixture is built from LOCAL's own offset: one instant, expressed in a zone 90 minutes ahead of
// wherever the test runs, so the assertion says the same thing on any machine's TZ.
func TestScheduleFireTimesSpellTheLocalWallClock(t *testing.T) {
	t.Parallel()

	local := time.Date(2026, 8, 4, 23, 0, 0, 0, time.Local)
	_, offset := local.Zone()
	away := local.In(time.FixedZone("away", offset+90*60))
	if away.Format("15:04") == local.Format("15:04") {
		t.Fatalf("the fixture no longer distinguishes the zones: away %s, local %s", away, local)
	}

	if got, want := clockOf(away), local.Format("15:04"); got != want {
		t.Errorf("clockOf = %q, want the local wall clock %q", got, want)
	}

	st := liveStatus("sch-1", "nightly tidy", time.Hour, domain.ModePlan)
	st.NextFire = away
	note := scheduleStatusNote([]schedule.Status{st})
	if want := "next " + local.Format("15:04"); !strings.Contains(note, want) {
		t.Errorf("/schedule row = %q, want it to carry %q", note, want)
	}
	if foreign := "next " + away.Format("15:04"); strings.Contains(note, foreign) {
		t.Errorf("/schedule row = %q, must not carry the foreign spelling %q", note, foreign)
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

// Every Event that is not a Firing's own reaches the scrollback as a note, byte for byte as it
// always did, and the created one reads the cycle, the mode and the next fire off the live listing —
// the Schedule is on the clock by the time its own creation is folded.
//
// EventFired is absent: a Firing that starts is announced as a BLOCK instead
// (TestScheduleFiringOpensABlock). Completed and failed keep their lines here because this table
// folds each Event into a fresh model, which is precisely the no-open-block case — a Gate refusal's
// failure — where the fold falls back to the note.
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
// The firing block
// ----------------------------------------------------------------------------
//
// A Firing is one block that grows: EventFired announces it with the prompt, and the Event that ends
// the run enriches that same block with the answer, what it cost, and where the record is. The tests
// below read the ENTRY — the kind, the pairing key, and which half of the view each line landed in —
// because that split is the block's grammar; what the painter does with it is render_test's.

// firingBody is a firing block's retained body lines, in order.
func firingBody(e entry) []string {
	out := make([]string, 0, e.tool.Details.len())
	for _, d := range e.tool.Details.all() {
		out = append(out, d.Text)
	}
	return out
}

// fireSchedule folds one EventFired into m — the open block every enrichment test starts from.
func fireSchedule(t *testing.T, m Model, id, name, prompt string) Model {
	t.Helper()
	return step(t, m, scheduleEventMsg{Event: schedule.Event{
		Kind: schedule.EventFired, ScheduleID: id, ScheduleName: name, Prompt: prompt,
	}})
}

// A starting Firing appends one block and no note: its own entry kind, the ScheduleID as the pairing
// key, the Schedule's name leading the branch row, the static running marker in that row's outcome
// slot, and the prompt as the body — open (`!done`) until the run returns.
func TestScheduleFiringOpensABlock(t *testing.T) {
	m := scheduleModel(t, &fakeScheduler{}, "")
	before := len(m.transcript.entries)

	m = fireSchedule(t, m, "sch-1", "nightly tidy", "check the log\nand tidy it")

	if got := len(m.transcript.entries); got != before+1 {
		t.Fatalf("entries = %d, want %d — one block, and no note beside it", got, before+1)
	}
	e := lastEntry(t, m)
	if e.kind != entrySchedule {
		t.Fatalf("kind = %v, want entrySchedule", e.kind)
	}
	if e.callID != "sch-1" {
		t.Errorf("callID = %q, want the ScheduleID — it is the pairing key", e.callID)
	}
	if e.done {
		t.Error("done = true, want an open block until the Firing returns")
	}
	if e.tool.Label != scheduleBlockLabel || e.tool.Target != "nightly tidy" {
		t.Errorf("header = %q / %q, want %q / the Schedule's name", e.tool.Label, e.tool.Target, scheduleBlockLabel)
	}
	if e.tool.Summary.Text != scheduleRunningSummary {
		t.Errorf("summary = %q, want the static running marker %q", e.tool.Summary.Text, scheduleRunningSummary)
	}
	want := []string{"prompt: check the log", "and tidy it"}
	if got := firingBody(e); !slices.Equal(got, want) {
		t.Errorf("body = %q, want the prompt %q", got, want)
	}
}

// The Firing returns and its block is enriched IN PLACE — same entry, no second block, no note — with
// a one-line answer promoted into the row's outcome slot as QUOTED text (nothing respells an
// answer), the prompt
// still beneath it, then what the run cost and where the record is.
func TestScheduleFiringCompletesInPlace(t *testing.T) {
	m := scheduleModel(t, &fakeScheduler{}, "")
	m = fireSchedule(t, m, "sch-1", "nightly tidy", "check the log")
	index := len(m.transcript.entries) - 1

	m = step(t, m, scheduleEventMsg{Event: schedule.Event{
		Kind: schedule.EventCompleted, ScheduleID: "sch-1", ScheduleName: "nightly tidy",
		Elapsed: 4 * time.Second,
		Outcome: schedule.Outcome{
			RecordID: "s1", Title: "nightly tidy — 14:05", FinalText: "the log is clean", Turns: 2,
		},
	}})

	if got := len(m.transcript.entries); got != index+1 {
		t.Fatalf("entries = %d, want %d — the block is enriched where it was announced", got, index+1)
	}
	e := m.transcript.entries[index]
	if !e.done {
		t.Error("done = false, want the block closed by the Firing's return")
	}
	if e.tool.Summary.Text != "the log is clean" {
		t.Errorf("summary = %q, want the one-line answer on the branch", e.tool.Summary.Text)
	}
	if !e.tool.Summary.quoted {
		t.Error("the answer rode the branch as the block's OWN words — it is quoted model text")
	}
	want := []string{"prompt: check the log", "2 turns · 4s", `saved as "nightly tidy — 14:05" — find it in /sessions`}
	if got := firingBody(e); !slices.Equal(got, want) {
		t.Errorf("body = %q, want %q", got, want)
	}
}

// A multi-line answer cannot ride the branch, so it LEADS the body with the summary slot left empty —
// the outputDetail grammar — and a collapsed block previews no line of it, counting the answer
// behind the "+N more lines" marker instead. The prompt keeps its place after it.
func TestScheduleFiringMultiLineAnswerLeadsTheBody(t *testing.T) {
	m := scheduleModel(t, &fakeScheduler{}, "")
	m = fireSchedule(t, m, "sch-1", "nightly tidy", "check the log")

	m = step(t, m, scheduleEventMsg{Event: schedule.Event{
		Kind: schedule.EventCompleted, ScheduleID: "sch-1", ScheduleName: "nightly tidy",
		Elapsed: 90 * time.Second,
		Outcome: schedule.Outcome{FinalText: "found 3 stale entries\nremoved them", Turns: 1, Denied: 2},
	}})

	e := lastEntry(t, m)
	if e.tool.Summary.Text != "" {
		t.Errorf("summary = %q, want it empty — a multi-line answer is a body", e.tool.Summary.Text)
	}
	want := []string{"found 3 stale entries", "removed them", "prompt: check the log", "1 turn · 1m30s · 2 denied"}
	if got := firingBody(e); !slices.Equal(got, want) {
		t.Errorf("body = %q, want the answer ahead of the prompt %q", got, want)
	}
}

// Two Schedules firing at once enrich the right blocks: the pairing is the ScheduleID, exactly as a
// tool result pairs by call id, so an answer can never land in another Schedule's block.
func TestScheduleFiringPairsByScheduleID(t *testing.T) {
	m := scheduleModel(t, &fakeScheduler{}, "")
	m = fireSchedule(t, m, "sch-1", "nightly tidy", "tidy the logs")
	m = fireSchedule(t, m, "sch-2", "inbox sweep", "sweep the inbox")
	first, second := len(m.transcript.entries)-2, len(m.transcript.entries)-1

	m = step(t, m, scheduleEventMsg{Event: schedule.Event{
		Kind: schedule.EventCompleted, ScheduleID: "sch-2", ScheduleName: "inbox sweep",
		Outcome: schedule.Outcome{FinalText: "inbox is empty"},
	}})
	m = step(t, m, scheduleEventMsg{Event: schedule.Event{
		Kind: schedule.EventCompleted, ScheduleID: "sch-1", ScheduleName: "nightly tidy",
		Outcome: schedule.Outcome{FinalText: "logs are tidy"},
	}})

	if got := m.transcript.entries[first].tool.Summary.Text; got != "logs are tidy" {
		t.Errorf("the first block's answer = %q, want its own", got)
	}
	if got := m.transcript.entries[second].tool.Summary.Text; got != "inbox is empty" {
		t.Errorf("the second block's answer = %q, want its own", got)
	}
}

// A failed Firing words its own summary and keeps everything it salvaged: the stats it got to, and
// the partial record's pointer when one saved. It shows no answer — an "error:" line above a partial
// answer would read as a result.
func TestScheduleFiringFailsInItsBlock(t *testing.T) {
	m := scheduleModel(t, &fakeScheduler{}, "")
	m = fireSchedule(t, m, "sch-1", "nightly tidy", "check the log")

	m = step(t, m, scheduleEventMsg{Event: schedule.Event{
		Kind: schedule.EventFailed, ScheduleID: "sch-1", ScheduleName: "nightly tidy",
		Elapsed: 8 * time.Second, Err: fmt.Errorf("the driver went away"),
		Outcome: schedule.Outcome{RecordID: "s1", Title: "nightly tidy — 14:05", FinalText: "half an answer", Turns: 3},
	}})

	e := lastEntry(t, m)
	if !e.done {
		t.Error("done = false, want the block closed by the failure")
	}
	if want := "error: the driver went away"; e.tool.Summary.Text != want {
		t.Errorf("summary = %q, want %q", e.tool.Summary.Text, want)
	}
	if e.tool.Summary.quoted {
		t.Error("the error line is the block's OWN wording, not quoted text")
	}
	want := []string{"prompt: check the log", "3 turns · 8s", `saved as "nightly tidy — 14:05" — find it in /sessions`}
	if got := firingBody(e); !slices.Equal(got, want) {
		t.Errorf("body = %q, want the salvaged facts and no answer: %q", got, want)
	}
}

// A failure with no open block is a Firing that never started — the Gate refused it — so it lands as
// the note it always was rather than inventing a block for a run that did not happen.
func TestScheduleFailureWithoutABlockStaysANote(t *testing.T) {
	m := scheduleModel(t, &fakeScheduler{}, "")

	m = step(t, m, scheduleEventMsg{Event: schedule.Event{
		Kind: schedule.EventFailed, ScheduleID: "sch-1", ScheduleName: "nightly tidy",
		Err: fmt.Errorf("the gate refused"),
	}})

	if e := lastEntry(t, m); e.kind != entryNote {
		t.Fatalf("kind = %v, want entryNote", e.kind)
	}
	if want := "schedule nightly tidy — failed: the gate refused"; lastNote(m) != want {
		t.Errorf("note = %q, want %q", lastNote(m), want)
	}
}

// The block's own wording, case by case: a run that answered nothing still says so, an unpersisted
// run points nowhere, and a record saved without a title still says the run is on disk.
func TestScheduleFiringWordsWhatItHas(t *testing.T) {
	for _, tc := range []struct {
		name        string
		outcome     schedule.Outcome
		wantSummary string
		wantTail    []string
	}{
		{"no answer", schedule.Outcome{Turns: 1}, scheduleNoAnswerSummary, []string{"1 turn · 0s"}},
		{"unpersisted", schedule.Outcome{FinalText: "done", Turns: 1}, "done", []string{"1 turn · 0s"}},
		{"saved untitled", schedule.Outcome{RecordID: "s1", FinalText: "done", Turns: 1}, "done",
			[]string{"1 turn · 0s", "saved — find it in /sessions"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := scheduleModel(t, &fakeScheduler{}, "")
			m = fireSchedule(t, m, "sch-1", "nightly tidy", "check the log")
			m = step(t, m, scheduleEventMsg{Event: schedule.Event{
				Kind: schedule.EventCompleted, ScheduleID: "sch-1", ScheduleName: "nightly tidy",
				Outcome: tc.outcome,
			}})

			e := lastEntry(t, m)
			if e.tool.Summary.Text != tc.wantSummary {
				t.Errorf("summary = %q, want %q", e.tool.Summary.Text, tc.wantSummary)
			}
			want := append([]string{"prompt: check the log"}, tc.wantTail...)
			if got := firingBody(e); !slices.Equal(got, want) {
				t.Errorf("body = %q, want %q", got, want)
			}
		})
	}
}

// Everything the block shows is untrusted — the Schedule's name and its prompt are a human's typed
// text, the answer is raw model output (ADR 0010: the library hands it over unsanitized) — so no ESC
// byte survives the fold in any of the three.
func TestScheduleFiringStripsEscapes(t *testing.T) {
	m := scheduleModel(t, &fakeScheduler{}, "")
	m = fireSchedule(t, m, "sch-1", "nightly \x1b]52;c;x\x07tidy", "check \x1bthe log")
	m = step(t, m, scheduleEventMsg{Event: schedule.Event{
		Kind: schedule.EventCompleted, ScheduleID: "sch-1", ScheduleName: "nightly tidy",
		Outcome: schedule.Outcome{FinalText: "clean \x1b[2Jenough", Title: "t \x1bitle", RecordID: "s1"},
	}})

	e := lastEntry(t, m)
	if strings.ContainsRune(e.tool.Target+detailsText(e.tool), 0x1b) {
		t.Errorf("an ESC byte reached the block: %q / %q", e.tool.Target, detailsText(e.tool))
	}
}

// The firing block is NOT a tool call, and the one piece of session state that would prove otherwise
// is the live status line's: an open Firing must leave hasOpenToolCall false, or the session would
// claim a tool of its own is running while it sits idle. Its block state is real, though — the same
// click that opens a tool block opens this one.
func TestScheduleFiringIsNoToolCall(t *testing.T) {
	m := scheduleModel(t, &fakeScheduler{}, "")
	m = fireSchedule(t, m, "sch-1", "nightly tidy", "check the log")

	if m.transcript.hasOpenToolCall() {
		t.Error("hasOpenToolCall = true, want an open Firing to be no tool call of this session's")
	}
	index := len(m.transcript.entries) - 1
	if !hasBlockState(entrySchedule) {
		t.Fatal("hasBlockState(entrySchedule) = false, want the block to collapse and expand")
	}
	if !m.transcript.toggleExpanded(index) || !m.transcript.entries[index].expanded {
		t.Error("toggling the firing block did not expand it")
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
		pickerCycle:        "type to filter · ↑/↓ select · ⏎ choose · esc close",
		pickerScheduleMode: "type to filter · ↑/↓ select · ⏎ choose · esc close",
		pickerScheduleStop: "type to filter · ↑/↓ select · ⏎ stop · esc close",
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

// ----------------------------------------------------------------------------
// The activity report (the host Gate's half)
// ----------------------------------------------------------------------------

// activityModel is a model whose activity reports land in the returned slice pointer, in order.
func activityModel(t *testing.T, reports *[]bool) Model {
	t.Helper()
	opts := testOpts
	opts.ReportActivity = func(busy bool) { *reports = append(*reports, busy) }
	return newTestModelEng(t, &fakeEngine{}, opts)
}

// The seam reports TRANSITIONS, not frames: a session says it started working once, says it
// stopped once, and says nothing at all about the stream in between.
func TestReportActivityPublishesTransitionsOnly(t *testing.T) {
	var reports []bool
	m := activityModel(t, &reports)
	if len(reports) != 0 {
		t.Fatalf("reports on a fresh idle model = %v, want none — the gate is already open", reports)
	}

	m.input.SetValue("check the build")
	m = step(t, m, keyEnter())
	if got := []bool{true}; !slices.Equal(reports, got) {
		t.Fatalf("reports after submit = %v, want %v", reports, got)
	}

	m = step(t, m, eventMsg{Event: domain.TokenEvent{Text: "working"}})
	m = step(t, m, eventMsg{Event: domain.MessageEvent{Text: "working"}})
	if got := []bool{true}; !slices.Equal(reports, got) {
		t.Errorf("reports after two events = %v, want the unchanged %v — the gate would thrash", reports, got)
	}

	m = step(t, m, exchangeDoneMsg{Result: domain.StepResult{Status: domain.StatusExchangeComplete}})
	if got := []bool{true, false}; !slices.Equal(reports, got) {
		t.Errorf("reports after the exchange ended = %v, want %v", reports, got)
	}
	if m.state != stateIdle {
		t.Errorf("state = %v, want idle", m.state)
	}
}

// A stop leaves what the human typed staged for the next ⏎ (ADR 0025), so the session is between
// two halves of one thought — and a Firing released there would land on top of the message about to
// go out. The report stays busy until the queue is gone.
func TestReportActivityHoldsWhileAQueueIsHeld(t *testing.T) {
	var reports []bool
	m := activityModel(t, &reports)

	m.input.SetValue("check the build")
	m = step(t, m, keyEnter())
	m.input.SetValue("and the tests too")
	m = step(t, m, keyEnter()) // staged as an interjection, not sent
	if len(m.pendingInterjections) != 1 {
		t.Fatalf("staged rows = %d, want 1", len(m.pendingInterjections))
	}

	m = step(t, m, cancelledMsg{Result: domain.StepResult{Status: domain.StatusCancelled}})
	if m.state != stateIdle {
		t.Fatalf("state after the cancel = %v, want idle", m.state)
	}
	if got := []bool{true}; !slices.Equal(reports, got) {
		t.Errorf("reports = %v, want %v — the held queue is still work waiting to go out", reports, got)
	}
	if m.quiescent() {
		t.Error("quiescent() is true with a row still staged; a firing would contend with the next ⏎")
	}
}

// The composition root's Notify seam: one scheduler Event, sent from the scheduler's own goroutine
// through the Bridge, arrives as the Msg the Update loop folds into a note.
func TestBridgeNotifyScheduleReachesTheTranscript(t *testing.T) {
	br := NewBridge()
	// An unbound Bridge is the startup window before Run binds the program; a Firing that narrated
	// there must be dropped rather than panic.
	br.NotifySchedule(schedule.Event{Kind: schedule.EventFired, ScheduleName: "nightly tidy"})

	prog := newStubProgram()
	br.Bind(prog)
	br.NotifySchedule(schedule.Event{
		Kind: schedule.EventCompleted, ScheduleName: "nightly tidy",
		Outcome: schedule.Outcome{RecordID: "s1", Title: "nightly tidy — 14:05"},
	})

	msgs := prog.messages()
	if len(msgs) != 1 {
		t.Fatalf("the bound program received %d Msgs, want the one sent after Bind", len(msgs))
	}
	ev, ok := msgs[0].(scheduleEventMsg)
	if !ok {
		t.Fatalf("the program received %T, want a scheduleEventMsg", msgs[0])
	}

	m := step(t, scheduleModel(t, &fakeScheduler{}, ""), ev)
	if got, want := lastNote(m), "schedule nightly tidy — finished: nightly tidy — 14:05"; got != want {
		t.Errorf("note = %q, want %q", got, want)
	}
}

// Compile-time proof that the live scheduler satisfies the seam the TUI drives — the SessionHost
// posture, so a library change that broke the surface fails here rather than in cmd/apogee.
var _ Scheduler = (*schedule.Scheduler)(nil)
