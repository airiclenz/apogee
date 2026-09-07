package undo

import (
	"errors"
	"fmt"
)

// ErrNothingToRedo is returned by [Journal.Redo] when the redo stack is empty — nothing has
// been reverted, or an exchange has written since and cleared it. It is the same condition
// [Journal.RedoPreview] reports with a false second return, given as an error because Redo
// has no such channel, and it mirrors [ErrNothingToUndo] exactly.
var ErrNothingToRedo = errors.New("undo: nothing to redo")

// RedoPreview describes what re-applying the last reverted exchange would do, without
// changing anything on disk. It reports false when the redo stack is empty.
//
// It is [Journal.Preview]'s mirror and reads the same way: each path is classified against
// the file as it is NOW, and the classification is inverted — the step restores what the
// agent wrote where the undo put the pre-image back, and deletes what the undo restored
// where the agent had removed it. A path whose content no longer matches what the undo left
// is SKIPPED with its reason, because the human's own edit outranks a redo exactly as it
// outranks an undo (ADR 0074 decision 6, which puts `/redo` under `/undo`'s own protocol).
//
// Ordinal counts from the oldest group still on the redo stack, so the top group's ordinal
// is the number of redo steps available, and Generation is the stamp [Journal.Redo] must be
// given back.
func (j *Journal) RedoPreview() (Step, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()

	if len(j.redo) == 0 {
		return Step{}, false
	}
	return j.previewOf(j.redo[len(j.redo)-1], len(j.redo), redoward), true
}

// Redo re-applies the last reverted exchange and returns what it did, moving the group back
// onto the undo stack so `/undo` can take it away again.
//
// generation is the stamp [Journal.RedoPreview] carried: it refuses with [ErrStaleGeneration],
// touching nothing, when the journal has moved since, so a human always confirms the step
// they were shown (ADR 0051 decision 7, which ADR 0074 decision 6 extends to this command).
// The check lives here rather than in the caller because a redo has no second reader between
// the preview and the act.
//
// It re-applies in the order the writes originally happened — the reverse of the order an
// undo takes them away — so a file lands after the directory its sibling created. Skipped
// paths are reported and the group moves anyway, for the same reason [Journal.Revert] pops
// one it could not fully carry out. It returns [ErrNothingToRedo], and does nothing, when
// the stack is empty.
func (j *Journal) Redo(generation uint64) (Report, error) {
	j.mu.Lock()
	defer j.mu.Unlock()

	if len(j.redo) == 0 {
		return Report{}, ErrNothingToRedo
	}
	if j.generation != generation {
		return Report{}, fmt.Errorf("%w: previewed at generation %d, journal is at %d",
			ErrStaleGeneration, generation, j.generation)
	}
	top := j.redo[len(j.redo)-1]

	report := j.runStep(top, len(j.redo), redoward)

	j.redo = j.redo[:len(j.redo)-1]
	j.groups = append(j.groups, top)
	j.pending = true
	j.generation++
	return report, nil
}
