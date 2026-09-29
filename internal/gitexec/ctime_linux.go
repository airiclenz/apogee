package gitexec

import (
	"os"
	"syscall"
	"time"
)

// changeTime reports the inode change time (ctime) os.Stat recorded in info, and whether the
// platform exposed one. Linux's syscall.Stat_t spells it Ctim.
func changeTime(info os.FileInfo) (time.Time, bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return time.Time{}, false
	}
	return time.Unix(st.Ctim.Unix()), true
}
