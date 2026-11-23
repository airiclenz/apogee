//go:build !windows

package realpath

// realPathFallback is nil off Windows: a POSIX filepath.EvalSymlinks already follows every link
// there is, so Resolve has no second resolver to try and ResolveLenient has no final path name
// to spell its answer by. It is a variable so a test can install one and drive both branches
// from any host.
var realPathFallback func(p string) (string, bool)

// crossesJunction is nil off Windows: POSIX has no junction for filepath.EvalSymlinks to leave
// unresolved. It is a variable so a test can install one beside realPathFallback.
var crossesJunction func(p string) bool
