//go:build windows

package realpath

import (
	"path/filepath"
	"syscall"

	"golang.org/x/sys/windows"
)

// realPathFallback is the resolver Resolve turns to when evalSymlinks fails on a path that
// exists, or answers with a path that still crosses a junction: on Windows, the final path name
// of a handle opened on it, which crosses an NTFS junction where filepath.EvalSymlinks does not.
// ResolveLenient spells every answer through it. It is a variable so a test can stand in for it.
var realPathFallback = FinalPathName

// crossesJunction reports whether a path filepath.EvalSymlinks has already answered for still
// passes through an NTFS junction. It is a variable so a test can stand in for it.
var crossesJunction = hasJunctionComponent

// hasJunctionComponent reports whether p, or any of its ancestors, is an NTFS junction (a
// mount-point reparse point). Go >= 1.23 stats a junction as a plain directory, so the reparse
// tag is read from the directory entry itself. Only the mount-point tag counts: a OneDrive or
// other non-link reparse point names no other place, so Resolve keeps EvalSymlinks' answer.
// A component that cannot be read (a volume root, a missing path) counts as no junction.
func hasJunctionComponent(p string) bool {
	for current := p; ; {
		if isJunction(current) {
			return true
		}
		parent := filepath.Dir(current)
		if parent == current {
			return false
		}
		current = parent
	}
}

// isJunction is one FindFirstFile on p, read for the mount-point reparse tag.
func isJunction(p string) bool {
	name, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return false
	}
	var data windows.Win32finddata
	handle, err := windows.FindFirstFile(name, &data)
	if err != nil {
		return false
	}
	_ = windows.FindClose(handle)
	return data.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 &&
		data.Reserved0 == windows.IO_REPARSE_TAG_MOUNT_POINT
}

// FinalPathName resolves p to its final on-disk form through kernel32's
// GetFinalPathNameByHandle. It answers only for a path that EXISTS: the answer comes from an
// open handle, which is what makes it authoritative — reparse points traversed, 8.3 aliases
// expanded, the trailing dots and spaces Win32 canonicalization strips removed — and a path
// that cannot be opened yields ok=false, which a caller refuses on or falls back from rather
// than guesses about. The `\\?\` prefix the API returns is stripped (StripLongPathPrefix) so
// the answer compares — and reads, in a refusal — as an ordinary path.
func FinalPathName(p string) (string, bool) {
	pathW, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return "", false
	}
	// Zero desired access opens the handle for metadata only, and FILE_FLAG_BACKUP_SEMANTICS
	// is what permits CreateFile to open a directory at all.
	handle, err := windows.CreateFile(pathW, 0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return "", false
	}
	defer func() { _ = windows.CloseHandle(handle) }()

	buf := make([]uint16, windows.MAX_PATH)
	for {
		n, err := windows.GetFinalPathNameByHandle(handle, &buf[0], uint32(len(buf)), 0)
		if err != nil || n == 0 {
			return "", false
		}
		if int(n) < len(buf) {
			final, _ := StripLongPathPrefix(syscall.UTF16ToString(buf[:n]))
			return final, true
		}
		buf = make([]uint16, n) // n is the required length, including the NUL
	}
}
