package security

import (
	"io/fs"
	"os"
)

// RootFS returns an fs.FS pinned to dir through an os.Root, for a reader that must never leave
// the folder it was handed — a workspace, or a skill folder a repo ships.
//
// os.DirFS follows a symlink wherever it points, so a committed link to a host file reads that
// file. The root refuses every path that resolves outside dir, a relative or an absolute symlink
// alike; it follows a relative symlink that stays inside, and refuses an absolute symlink even
// when its target lies inside dir, the same in-root policy read_file applies.
//
// The root stays open for as long as the returned FS is reachable and is closed by os.Root's own
// finalizer after that, so no caller has a handle to release. A dir that will not open (missing,
// removed since it was announced, not a directory) still yields a non-nil FS whose every Open
// fails with that open error: nil would send a reader to its fallback and read a same-named file
// from the wrong tree.
func RootFS(dir string) fs.FS {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return unopenedFS{err: err}
	}
	return root.FS()
}

// unopenedFS is the FS of a folder that did not open: every Open fails with that error.
type unopenedFS struct{ err error }

// Open satisfies fs.FS, refusing name with the folder's open error.
func (u unopenedFS) Open(name string) (fs.File, error) {
	return nil, &fs.PathError{Op: "open", Path: name, Err: u.err}
}
