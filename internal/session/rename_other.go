//go:build !windows

package session

import "os"

// renameRecord moves from over to. Outside Windows a rename over a file a reader holds open
// succeeds — the reader keeps the old inode — so there is nothing to retry (rename.go).
func renameRecord(from, to string) error {
	return os.Rename(from, to)
}
