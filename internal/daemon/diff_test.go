package daemon

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/schedule"
)

// entry builds one validated-shaped Entry. Only the fields a diff or a Spec reads vary between
// cases; the rest stand in for the daemon-only half that never reaches the library.
func entry(name, prompt string, cycle time.Duration) Entry {
	return Entry{
		Name: name,
		On:   Trigger{Cycle: cycle},
		Run: Action{
			Prompt:    prompt,
			Workspace: "/repos/" + name,
			Mode:      domain.ModePlan,
		},
	}
}

// recordingScheduler is the fake the reload is asserted against: it records the exact call
// sequence, hands out predictable ids, and refuses whatever a case tells it to refuse.
type recordingScheduler struct {
	calls      []string
	added      int
	refuseAdd  map[string]error
	refuseStop map[string]error
}

func (s *recordingScheduler) Add(spec schedule.Spec) (string, error) {
	if err := s.refuseAdd[spec.Name]; err != nil {
		s.calls = append(s.calls, "add "+spec.Name+": refused")
		return "", err
	}
	s.added++
	id := fmt.Sprintf("id-%d", s.added)
	s.calls = append(s.calls, "add "+spec.Name+" -> "+id)
	return id, nil
}

func (s *recordingScheduler) Stop(id string) error {
	s.calls = append(s.calls, "stop "+id)
	return s.refuseStop[id]
}

// assertNames compares one of a Reload's name lists, treating nil and empty as the same absence.
func assertNames(t *testing.T, list string, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("%s = %v, want %v", list, got, want)
	}
}

func TestDiffMatchesEntriesByName(t *testing.T) {
	t.Parallel()
	nightly := entry("nightly-audit", "/code-audit", 24*time.Hour)
	sweep := entry("morning-sweep", "sweep the log", time.Hour)
	edited := entry("morning-sweep", "sweep the log twice", time.Hour)
	rephrased := entry("nightly-audit", "/code-audit", 12*time.Hour)
	renamed := entry("nightly-review", "/code-audit", 24*time.Hour)

	cases := []struct {
		name     string
		running  []Entry
		desired  []Entry
		kept     []string
		replaced []string
		added    []string
		removed  []string
	}{
		{
			name:    "the first adoption adds everything",
			desired: []Entry{nightly, sweep},
			added:   []string{"nightly-audit", "morning-sweep"},
		},
		{
			name:    "an untouched file keeps every entry",
			running: []Entry{nightly, sweep},
			desired: []Entry{nightly, sweep},
			kept:    []string{"nightly-audit", "morning-sweep"},
		},
		{
			name:     "an edited entry is replaced and its neighbour kept",
			running:  []Entry{nightly, sweep},
			desired:  []Entry{nightly, edited},
			kept:     []string{"nightly-audit"},
			replaced: []string{"morning-sweep"},
		},
		{
			name:     "a changed cycle is a change like any other",
			running:  []Entry{nightly},
			desired:  []Entry{rephrased},
			replaced: []string{"nightly-audit"},
		},
		{
			name:    "a deleted entry is removed",
			running: []Entry{nightly, sweep},
			desired: []Entry{nightly},
			kept:    []string{"nightly-audit"},
			removed: []string{"morning-sweep"},
		},
		{
			name:    "a rename is a remove and an add",
			running: []Entry{nightly},
			desired: []Entry{renamed},
			added:   []string{"nightly-review"},
			removed: []string{"nightly-audit"},
		},
		{
			name:    "reordering the file changes nothing",
			running: []Entry{nightly, sweep},
			desired: []Entry{sweep, nightly},
			kept:    []string{"morning-sweep", "nightly-audit"},
		},
		{
			name:    "an emptied file removes everything",
			running: []Entry{nightly, sweep},
			removed: []string{"nightly-audit", "morning-sweep"},
		},
		{
			name:    "a repeated name counts once, as its first entry",
			running: []Entry{nightly},
			desired: []Entry{nightly, edited, sweep},
			kept:    []string{"nightly-audit"},
			added:   []string{"morning-sweep"},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			reload := Diff(testCase.running, testCase.desired)

			assertNames(t, "Kept", reload.Kept, testCase.kept)
			assertNames(t, "Replaced", reload.Replaced, testCase.replaced)
			assertNames(t, "Added", reload.Added, testCase.added)
			assertNames(t, "Removed", reload.Removed, testCase.removed)
		})
	}
}

func TestReloadChangedIsFalseForACosmeticEdit(t *testing.T) {
	t.Parallel()
	nightly := entry("nightly-audit", "/code-audit", 24*time.Hour)

	cosmetic := Diff([]Entry{nightly}, []Entry{nightly})
	substantial := Diff([]Entry{nightly}, nil)

	if cosmetic.Changed() {
		t.Errorf("a reload that only kept entries reports Changed() = true: %+v", cosmetic)
	}
	if !substantial.Changed() {
		t.Errorf("a reload that removed an entry reports Changed() = false: %+v", substantial)
	}
}

func TestApplyLeavesKeptSchedulesAlone(t *testing.T) {
	t.Parallel()
	nightly := entry("nightly-audit", "/code-audit", 24*time.Hour)
	sweep := entry("morning-sweep", "sweep the log", time.Hour)
	scheduler := &recordingScheduler{}
	ids := map[string]string{"nightly-audit": "id-A", "morning-sweep": "id-B"}

	reload, err := Apply(scheduler, ids, []Entry{nightly, sweep}, []Entry{sweep, nightly})

	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(scheduler.calls) != 0 {
		t.Errorf("a kept entry was touched: %v", scheduler.calls)
	}
	assertNames(t, "Kept", reload.Kept, []string{"morning-sweep", "nightly-audit"})
	assertIDs(t, ids, map[string]string{"nightly-audit": "id-A", "morning-sweep": "id-B"})
}

func TestApplyStopsThenAddsAReplacedEntry(t *testing.T) {
	t.Parallel()
	nightly := entry("nightly-audit", "/code-audit", 24*time.Hour)
	sweep := entry("morning-sweep", "sweep the log", time.Hour)
	edited := entry("morning-sweep", "sweep the log twice", time.Hour)
	gone := entry("stale", "whatever", time.Hour)
	fresh := entry("weekly-report", "report", 7*24*time.Hour)
	scheduler := &recordingScheduler{}
	ids := map[string]string{"nightly-audit": "id-A", "morning-sweep": "id-B", "stale": "id-C"}

	reload, err := Apply(scheduler, ids,
		[]Entry{nightly, sweep, gone},
		[]Entry{nightly, edited, fresh})

	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	wantCalls := []string{
		"stop id-C",
		"stop id-B",
		"add morning-sweep -> id-1",
		"add weekly-report -> id-2",
	}
	if !slices.Equal(scheduler.calls, wantCalls) {
		t.Errorf("call sequence = %v, want %v", scheduler.calls, wantCalls)
	}
	assertNames(t, "Replaced", reload.Replaced, []string{"morning-sweep"})
	assertIDs(t, ids, map[string]string{
		"nightly-audit": "id-A",
		"morning-sweep": "id-1",
		"weekly-report": "id-2",
	})
}

func TestApplyReportsAnAddFailureAndFinishesTheRest(t *testing.T) {
	t.Parallel()
	sweep := entry("morning-sweep", "sweep the log", time.Hour)
	edited := entry("morning-sweep", "sweep the log twice", time.Hour)
	fresh := entry("weekly-report", "report", 7*24*time.Hour)
	scheduler := &recordingScheduler{refuseAdd: map[string]error{"morning-sweep": schedule.ErrCycle}}
	ids := map[string]string{"morning-sweep": "id-B"}

	_, err := Apply(scheduler, ids, []Entry{sweep}, []Entry{edited, fresh})

	if !errors.Is(err, schedule.ErrCycle) {
		t.Fatalf("Apply error = %v, want one wrapping ErrCycle", err)
	}
	if got := err.Error(); !strings.Contains(got, "morning-sweep") {
		t.Errorf("the failure does not name its entry: %s", got)
	}
	if !slices.Contains(scheduler.calls, "add weekly-report -> id-1") {
		t.Errorf("a refused Add wedged the remaining work: %v", scheduler.calls)
	}
	assertIDs(t, ids, map[string]string{"weekly-report": "id-1"})
}

func TestApplyTreatsAForgottenScheduleAsStopped(t *testing.T) {
	t.Parallel()
	sweep := entry("morning-sweep", "sweep the log", time.Hour)
	scheduler := &recordingScheduler{refuseStop: map[string]error{"id-B": schedule.ErrNotFound}}
	ids := map[string]string{"morning-sweep": "id-B"}

	_, err := Apply(scheduler, ids, []Entry{sweep}, nil)

	if err != nil {
		t.Fatalf("a Stop of an already-gone schedule is not a failure, got: %v", err)
	}
	assertIDs(t, ids, map[string]string{})
}

func TestApplyReportsAStopFailure(t *testing.T) {
	t.Parallel()
	sweep := entry("morning-sweep", "sweep the log", time.Hour)
	scheduler := &recordingScheduler{refuseStop: map[string]error{"id-B": schedule.ErrClosed}}
	ids := map[string]string{"morning-sweep": "id-B"}

	_, err := Apply(scheduler, ids, []Entry{sweep}, nil)

	if !errors.Is(err, schedule.ErrClosed) {
		t.Fatalf("Apply error = %v, want one wrapping ErrClosed", err)
	}
	// The scheduler still runs the schedule, so the map must still say who it is — dropping the
	// id here would strand it: absent from the shutdown's adopted set, unstoppable by any reload.
	assertIDs(t, ids, map[string]string{"morning-sweep": "id-B"})
}

func TestApplyDoesNotStopAnEntryThatWasNeverOnTheClock(t *testing.T) {
	t.Parallel()
	sweep := entry("morning-sweep", "sweep the log", time.Hour)
	scheduler := &recordingScheduler{}
	ids := map[string]string{}

	if _, err := Apply(scheduler, ids, []Entry{sweep}, nil); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if len(scheduler.calls) != 0 {
		t.Errorf("an entry with no id was stopped: %v", scheduler.calls)
	}
}

func TestEntrySpecCarriesTheLibraryHalfOnly(t *testing.T) {
	t.Parallel()
	adopted := Entry{
		Name: "nightly-audit",
		On:   Trigger{Cycle: 24 * time.Hour},
		Run: Action{
			Prompt:    "/code-audit internal/tui",
			Workspace: "/repos/apogee",
			Mode:      domain.ModeAuto,
			Server:    "openrouter",
			Model:     "qwen/qwen3-72b",
		},
	}

	spec := adopted.Spec()

	want := schedule.Spec{
		Name:   "nightly-audit",
		Cycle:  24 * time.Hour,
		Prompt: "/code-audit internal/tui",
		Mode:   domain.ModeAuto,
	}
	if spec != want {
		t.Errorf("Spec() = %+v, want %+v", spec, want)
	}
}

// assertIDs compares the whole name→id map, so a stale name left behind fails as loudly as a
// missing one.
func assertIDs(t *testing.T, got, want map[string]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("name→id map = %v, want %v", got, want)
	}
	for name, id := range want {
		if got[name] != id {
			t.Fatalf("name→id map = %v, want %v", got, want)
		}
	}
}
