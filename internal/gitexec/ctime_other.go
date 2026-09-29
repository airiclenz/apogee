//go:build !linux && !darwin && !freebsd && !netbsd && !openbsd && !dragonfly

package gitexec

import (
	"os"
	"time"
)

// changeTime reports that this platform exposes no inode change time through os.Stat — Windows
// has no userland ctime — so a fileprint there rests on size, mtime, mode and identity alone.
func changeTime(os.FileInfo) (time.Time, bool) {
	return time.Time{}, false
}
