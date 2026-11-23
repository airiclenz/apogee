package workflow

// RunEnding is how a whole run ended, the one value a host words its note, its event and its answer
// from; one child's end is an Ending. Phase is done, stopped or failed; Err is what failed it, set
// when and only when a run that was taken up could not proceed, or when a run that never started
// could not be settled. NeverStarted marks a run dropped from a queue before any Run took it: no
// Runner ran it and none will report its end, and its Phase is the one its folder is left in
// (Runner.SettleUnstarted).
type RunEnding struct {
	Phase        Phase
	Err          error
	NeverStarted bool
}

// EndingOf is the RunEnding of a run Runner.Run returned result and err for: failed when Run
// returned an error — a stage error, a final write that failed, or a run that could not open its
// folder and so has no ID — else stopped when a cancel ended it before every item finished, else done.
// NeverStarted is always false: Run took the run up, even when it failed before its folder opened.
func EndingOf(result Result, err error) RunEnding {
	switch {
	case err != nil:
		return RunEnding{Phase: PhaseFailed, Err: err}
	case result.Stopped():
		return RunEnding{Phase: PhaseStopped}
	default:
		return RunEnding{Phase: PhaseDone}
	}
}

// UnstartedEnding is the RunEnding of a run that never started, from what Runner.SettleUnstarted
// returned for it: the phase its folder is left in, and the error of a folder that could not be
// settled. It is the only RunEnding with NeverStarted set.
func UnstartedEnding(phase Phase, err error) RunEnding {
	return RunEnding{Phase: phase, Err: err, NeverStarted: true}
}
