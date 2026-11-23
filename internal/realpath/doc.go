// Package realpath answers one question: where does this path really live. It resolves symbolic
// links and, on Windows, NTFS junctions, for a path that exists and for one that does not exist
// yet, and it is the only place that does so.
//
// Three entry points:
//
//   - Resolve is strict: every link resolved, and an error (fs.ErrNotExist-classed for a missing
//     path) when the path cannot be resolved. A caller that keys on the result or must tell
//     "missing" from "elsewhere" uses it.
//   - ResolveLenient never fails: a path that does not exist resolves through its nearest existing
//     ancestor with the missing rest re-joined, so a file about to be created still has a real
//     location to compare. On Windows every answer is spelled as the volume's final path name,
//     so two paths resolved separately always share one spelling.
//   - IsLink says whether a path is itself a link (a symlink or a Windows reparse point),
//     following nothing.
//
// Spellings builds on them: the spellings of a Project root and its workspace that only the file
// system can vouch for, as one plain value.
//
// The package is a leaf: it imports the standard library and golang.org/x/sys/windows and nothing
// from apogee, so every package that needs a real path can import it without a cycle
// (TestPackageImportsNothingFromApogee).
//
// Files:
//
//	realpath.go          Resolve, ResolveLenient, IsLink and StripLongPathPrefix; the evalSymlinks seam
//	spellings.go         Spellings and RootSpellings: a Project root and its workspace in every spelling
//	realpath_windows.go  FinalPathName (GetFinalPathNameByHandle) and the junction check, wired into the two seams
//	realpath_other.go    the two seams left nil: POSIX has no junction and no second resolver
package realpath
