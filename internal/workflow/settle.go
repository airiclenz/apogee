package workflow

// SettleUnstarted settles the folder of a run that never started — one dropped from a queue before
// any Run took it, so no Run will settle it — and returns the phase the folder is left in. A
// pending, running or stopped folder is recorded stopped, its Updated stamped from the Runner's
// clock. A folder already done or failed — a queued re-run's — is left untouched: the re-run never
// started, so the run it would have repeated stands. A folder that cannot be read, or the stopped
// status that cannot be written, is an error.
func (r *Runner) SettleUnstarted(id string) (Phase, error) {
	status, err := r.Store.ReadStatus(id)
	if err != nil {
		return "", err
	}
	if status.Phase == PhaseDone || status.Phase == PhaseFailed {
		return status.Phase, nil
	}
	status.Phase = PhaseStopped
	status.Updated = r.now()
	return PhaseStopped, r.Store.WriteStatus(status)
}

// SettleFailedStart settles the folder id of a run Run took up but that failed before it knew the
// folder — its plan's items could not be expanded or its folder not found, so Run returned no ID
// and marked nothing — and returns the phase the folder is left in. It is for a caller that opened
// the folder ahead of Run (Open) and so has the id Run never reached. The rule is Run's own
// (failPending): a folder that never ran, recorded pending, is recorded failed, so it lists as
// failed and can be run again; a done, failed, stopped or running folder — a queued re-run's or a
// resumed one's — is left as it stands. A folder that cannot be read, or the failed status that
// cannot be written, is an error.
func (r *Runner) SettleFailedStart(id string) (Phase, error) {
	status, err := r.Store.ReadStatus(id)
	if err != nil {
		return "", err
	}
	return r.failPending(status)
}

// failPending records as failed the folder status describes, as the run that failed before its
// first stage read it, when that folder never ran: only a pending one is written, its Updated
// stamped from the Runner's clock. It returns the phase the folder is left in, and the write's
// error.
func (r *Runner) failPending(status RunStatus) (Phase, error) {
	if status.Phase != PhasePending {
		return status.Phase, nil
	}
	status.Phase = PhaseFailed
	status.Updated = r.now()
	return PhaseFailed, r.Store.WriteStatus(status)
}
