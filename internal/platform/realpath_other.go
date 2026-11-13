//go:build !windows

package platform

// realPathFallback is nil off Windows: a POSIX filepath.EvalSymlinks already follows every link
// there is, so RealPath has no second resolver to try. It is a variable so a test can install one
// and drive the fallback branch from any host.
var realPathFallback func(p string) (string, bool)

// crossesJunction is nil off Windows: POSIX has no junction for filepath.EvalSymlinks to leave
// unresolved. It is a variable so a test can install one beside realPathFallback.
var crossesJunction func(p string) bool
