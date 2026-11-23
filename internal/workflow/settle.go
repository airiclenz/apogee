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
