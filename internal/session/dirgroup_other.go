//go:build !windows

package session

import (
	"os"
	"syscall"
)

// fileGroup answers the group that owns the file info describes, read off its syscall.Stat_t; ok is
// false only when info carries none (shareDir then sets the group unconditionally).
func fileGroup(info os.FileInfo) (gid int, ok bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return int(stat.Gid), true
}
