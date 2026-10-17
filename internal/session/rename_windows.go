//go:build windows

package session

import (
	"errors"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

// renameRetryBudget bounds how long a retryable rename failure is retried before the error
// surfaces — the same bound Go's own cmd/go/internal/robustio gives a Windows rename, long enough
// for a reader to finish one record read, short enough that a handle never released (an antivirus
// scan gone wrong) still fails the save rather than wedging it.
const renameRetryBudget = 2 * time.Second

// renameRecord moves from over to, retrying through renameRetryBudget while a reader holds the
// destination open without FILE_SHARE_DELETE (rename.go).
func renameRecord(from, to string) error {
	return renameWithRetry(os.Rename, isRenameSharingError, renameRetryBudget, from, to)
}

// isRenameSharingError reports whether err is one of the two errors a Windows rename gives while
// another handle has the destination open without FILE_SHARE_DELETE: ERROR_SHARING_VIOLATION, or
// ERROR_ACCESS_DENIED, which is what MoveFileEx reports for that case. Both pass once the handle
// closes; every other error is final.
func isRenameSharingError(err error) bool {
	return errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_ACCESS_DENIED)
}

// isRemoveSharingError reports whether err is a Windows removal refused because another handle —
// a watcher following the journal — has the file open without FILE_SHARE_DELETE. DeleteFile
// answers that case with the same two errors a rename does (isRenameSharingError).
func isRemoveSharingError(err error) bool {
	return isRenameSharingError(err)
}
