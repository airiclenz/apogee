//go:build !windows

package session

import "os"

// renameRecord moves from over to. Outside Windows a rename over a file a reader holds open
// succeeds — the reader keeps the old inode — so there is nothing to retry (rename.go).
func renameRecord(from, to string) error {
	return os.Rename(from, to)
}

// isRemoveSharingError is always false outside Windows: an unlink succeeds while a reader holds
// the file open — the reader keeps the old inode — so no removal is ever refused for it.
func isRemoveSharingError(error) bool {
	return false
}
