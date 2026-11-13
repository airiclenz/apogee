//go:build windows

package platform

// realPathFallback is the resolver RealPath turns to when evalSymlinks fails on a path that
// exists: on Windows, the final path name of a handle opened on it, which crosses an NTFS
// junction where filepath.EvalSymlinks does not. It is a variable so a test can stand in for it.
var realPathFallback = finalPathName
