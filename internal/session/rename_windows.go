//go:build windows

package session

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

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
