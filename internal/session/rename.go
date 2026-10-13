package session

import "time"

// The record rename and its retry. atomicWrite moves a finished temp file over <id>.json with
// renameRecord, which each platform defines: rename_other.go calls os.Rename and nothing more,
// and rename_windows.go routes it through renameWithRetry, because a Windows rename over a file
// another handle holds open without FILE_SHARE_DELETE — a reader in the middle of loading the
// record, which is what os.Open is — fails with a sharing violation or access-denied until that
// handle closes. The loop lives here, untagged, so its tests run on every platform.

const (
	// renameFirstBackoff is the pause after the first retryable failure; each later pause
	// doubles it, up to renameMaxBackoff.
	renameFirstBackoff = time.Millisecond
	// renameMaxBackoff caps the pause between attempts, so a reader that lets go is noticed
	// within a tenth of a second however long the wait has run.
	renameMaxBackoff = 100 * time.Millisecond
)

// renameWithRetry calls rename(from, to) and returns its result, except that an error retryable
// reports true is retried after a short pause that doubles each time (renameFirstBackoff up to
// renameMaxBackoff) for as long as budget has not elapsed since the first attempt. Once the
// budget is spent it returns the last error rename gave; a nil or non-retryable result returns
// at once.
func renameWithRetry(rename func(from, to string) error, retryable func(error) bool, budget time.Duration, from, to string) error {
	start := time.Now()
	backoff := renameFirstBackoff
	for {
		err := rename(from, to)
		if err == nil || !retryable(err) {
			return err
		}
		remaining := budget - time.Since(start)
		if remaining <= 0 {
			return err
		}
		time.Sleep(min(backoff, remaining))
		backoff = min(2*backoff, renameMaxBackoff)
	}
}
