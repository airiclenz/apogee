//go:build windows

package session

import "os"

// fileGroup is never ok on Windows: its files carry no POSIX group, so shareDir's chown runs and
// fails as it always has — the config refuses a share group there before a store is built.
func fileGroup(os.FileInfo) (gid int, ok bool) {
	return 0, false
}
