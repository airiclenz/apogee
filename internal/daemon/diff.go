package daemon

import (
	"errors"
	"fmt"

	"github.com/airiclenz/apogee/internal/schedule"
)

// Scheduler is the slice of [schedule.Scheduler] a reload needs: put a Spec on the clock, take an
// id off it. Nothing else — a reload never lists, never fires and never closes, and an interface
// this narrow is one a test fake can be honest about (the daemon passes the real Scheduler; the
// package's own tests pass a recorder).
type Scheduler interface {
	// Add puts one Spec on the clock and returns the id it answers to.
	Add(spec schedule.Spec) (string, error)
	// Stop takes the Schedule with that id off the clock. An in-flight Firing finishes.
	Stop(id string) error
}

// Reload is what one file edit decides, as four lists of entry names. It is a DECISION rather than
// a report of what happened: [Apply] returns the same Reload whether every operation landed or one
// of them failed, and names the failures in its error instead. A surface logging it says what the
// edit asked for, and logs the error beside it when something refused.
//
// Kept, Replaced and Added are in the new file's order; Removed is in the order the running set
// held them.
type Reload struct {
	// Kept names the entries whose spec did not change. They are not touched at all — that is the
	// whole point of diffing by name: editing one schedule must not re-phase its neighbours, which
	// a stop-everything-and-re-add reload would do to all of them (ADR 0034).
	Kept []string
	// Replaced names the entries that kept their name and changed something else. They are stopped
	// and added again, so their cycle starts over — an edited schedule is a new standing
	// instruction, and its first Firing lands one full cycle later.
	Replaced []string
	// Added names the entries the new file has and the running set did not.
	Added []string
	// Removed names the entries the running set has and the new file does not. A rename shows up
	// here and in Added, because a name IS the identity a reload matches by.
	Removed []string
}

// Changed reports whether the reload does anything to the running set. False means the edit was
// cosmetic — a comment, a reordering, a whitespace change — and every schedule keeps its phase.
func (r Reload) Changed() bool {
	return len(r.Replaced) > 0 || len(r.Added) > 0 || len(r.Removed) > 0
}

// Diff decides what turning the running set into the desired set takes, matching entries BY NAME
// (design call 3, ADR 0034): a name in both sets with an identical spec is kept, the same name
// with any other difference is replaced, a name only the desired set has is added, and a name only
// the running set has is removed.
//
// It is pure: it reads the two slices and touches nothing. Both may be nil — adopting the first
// file is a diff against an empty running set, and a file whose `schedules:` list went away is a
// diff against an empty desired set.
//
// Names are unique in a validated file ([Load] refuses a duplicate). Should a caller diff an
// unvalidated set anyway, the FIRST entry of a repeated name is the one that counts and its twins
// are ignored — the same one [Load] leaves standing when it blames the later twin.
func Diff(running, desired []Entry) Reload {
	runningByName, runningOnce := index(running)
	desiredByName, desiredOnce := index(desired)

	var reload Reload
	for _, entry := range desiredOnce {
		previous, wasRunning := runningByName[entry.Name]
		switch {
		case !wasRunning:
			reload.Added = append(reload.Added, entry.Name)
		case previous == entry:
			reload.Kept = append(reload.Kept, entry.Name)
		default:
			reload.Replaced = append(reload.Replaced, entry.Name)
		}
	}
	for _, entry := range runningOnce {
		if _, stillWanted := desiredByName[entry.Name]; !stillWanted {
			reload.Removed = append(reload.Removed, entry.Name)
		}
	}
	return reload
}

// index maps a set by name and returns its entries in the order they were given, with any repeated
// name dropped after its first appearance — a shape a validated file never has, and the same twin
// [Load] blames when it sees one.
func index(entries []Entry) (map[string]Entry, []Entry) {
	byName := make(map[string]Entry, len(entries))
	unique := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		if _, taken := byName[entry.Name]; taken {
			continue
		}
		byName[entry.Name] = entry
		unique = append(unique, entry)
	}
	return byName, unique
}

// Apply diffs the running set against the desired set and enacts the difference on the scheduler:
// every removed and replaced entry is stopped, every replaced and added entry is added, and a kept
// entry receives no call at all. It returns what the diff decided, so a caller can log the edit
// and adopt the desired set as its new running set.
//
// ids is the daemon's name→schedule-id map and is updated IN PLACE: a stopped name is deleted from
// it, an added name is recorded with the id the scheduler returned. It is the map a Firing
// composition and a status surface read, so it always describes what is actually on the clock —
// an entry whose Add failed is absent from it.
//
// Every failure is reported, joined into one error, and none of them stops the rest of the work: a
// scheduler that refuses one Spec must not leave the other edits in the same save unapplied. A
// Stop of an id the scheduler no longer knows ([schedule.ErrNotFound]) is not a failure — the
// entry is off the clock, which is what the stop asked for.
func Apply(scheduler Scheduler, ids map[string]string, running, desired []Entry) (Reload, error) {
	reload := Diff(running, desired)
	desiredByName, _ := index(desired)
	var failures []error

	for _, name := range reload.Removed {
		if err := stop(scheduler, ids, name); err != nil {
			failures = append(failures, err)
		}
	}
	for _, name := range reload.Replaced {
		if err := stop(scheduler, ids, name); err != nil {
			failures = append(failures, err)
		}
		if err := add(scheduler, ids, desiredByName[name]); err != nil {
			failures = append(failures, err)
		}
	}
	for _, name := range reload.Added {
		if err := add(scheduler, ids, desiredByName[name]); err != nil {
			failures = append(failures, err)
		}
	}
	return reload, errors.Join(failures...)
}

// stop takes one named entry off the clock and out of the id map. A name with no id was never on
// the clock — its own Add failed on an earlier reload — so there is nothing to stop.
func stop(scheduler Scheduler, ids map[string]string, name string) error {
	id, onTheClock := ids[name]
	if !onTheClock {
		return nil
	}
	delete(ids, name)
	if err := scheduler.Stop(id); err != nil && !errors.Is(err, schedule.ErrNotFound) {
		return fmt.Errorf("apogee: schedules.yaml: stopping the %q schedule: %w", name, err)
	}
	return nil
}

// add puts one entry on the clock and records the id it answers to.
func add(scheduler Scheduler, ids map[string]string, entry Entry) error {
	id, err := scheduler.Add(entry.Spec())
	if err != nil {
		return fmt.Errorf("apogee: schedules.yaml: starting the %q schedule: %w", entry.Name, err)
	}
	ids[entry.Name] = id
	return nil
}

// Spec is the entry as the scheduler library sees it: a name, a cycle, a prompt and a mode, and
// nothing else. The daemon-only half of `run:` — workspace, server, model — deliberately does not
// travel through [schedule.Spec]: the library is runner-agnostic (ADR 0033), so those fields reach
// the Firing composition through the daemon's own name→Entry map instead.
func (e Entry) Spec() schedule.Spec {
	return schedule.Spec{
		Name:   e.Name,
		Cycle:  e.On.Cycle,
		Prompt: e.Run.Prompt,
		Mode:   e.Run.Mode,
	}
}
