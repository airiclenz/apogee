//go:build windows

package platform

import (
	"path/filepath"

	"golang.org/x/sys/windows"
)

// realPathFallback is the resolver RealPath turns to when evalSymlinks fails on a path that
// exists, or answers with a path that still crosses a junction: on Windows, the final path name
// of a handle opened on it, which crosses an NTFS junction where filepath.EvalSymlinks does not.
// It is a variable so a test can stand in for it.
var realPathFallback = finalPathName

// crossesJunction reports whether a path filepath.EvalSymlinks has already answered for still
// passes through an NTFS junction. It is a variable so a test can stand in for it.
var crossesJunction = hasJunctionComponent

// hasJunctionComponent reports whether p, or any of its ancestors, is an NTFS junction (a
// mount-point reparse point). Go >= 1.23 stats a junction as a plain directory, so the reparse
// tag is read from the directory entry itself. Only the mount-point tag counts: a OneDrive or
// other non-link reparse point names no other place, so RealPath keeps EvalSymlinks' answer.
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
