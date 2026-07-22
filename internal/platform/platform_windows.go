//go:build windows

package platform

import (
	"path/filepath"
	"strings"
	"syscall"
)

// Current returns the platform Host for this Windows build target: the Windows rule set
// (host.go) with the OS long-path resolver wired in, so Contains can expand an 8.3 short
// name instead of refusing to compare it. Everything else about the rules is pure data and
// is exercised on every OS by the shared table tests; this file is the only part that
// needs a real Windows host, and Phase 5 runs its tests there natively.
func Current() Host {
	rules := windowsRules()
	rules.longPath = longPathName
	return rules
}

// longPathName expands an 8.3 short path ("C:\PROGRA~1\Go") to its long form through
// kernel32's GetLongPathNameW.
//
// That call is only defined for a path that EXISTS, and a confinement box is routinely
// asked about a file that does not exist yet, so the expansion walks up to the longest
// existing prefix and re-appends the unresolved tail. Nothing resolvable at all (a volume
// that never generated short names, a path with no existing ancestor) comes back
// unchanged: Contains then reports "not contained" rather than matching a name it could
// not verify, which is the safe answer for both of its callers — a root it cannot resolve
// is neither collapsed into another root nor waved past a labelling guardrail.
func longPathName(p string) string {
	if !strings.Contains(p, "~") {
		return p
	}
	for current, tail := p, ""; ; {
		if long, ok := expandShortPath(current); ok {
			return filepath.Join(long, tail)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return p // reached the volume root with nothing resolvable
		}
		tail = filepath.Join(filepath.Base(current), tail)
		current = parent
	}
}

// expandShortPath is one GetLongPathNameW call, reporting ok=false when the path does not
// exist (or the call otherwise fails), which is how longPathName knows to try the parent.
func expandShortPath(p string) (string, bool) {
	from, err := syscall.UTF16PtrFromString(p)
	if err != nil {
		return "", false
	}
	buf := make([]uint16, len(p)+16)
	for {
		n, err := syscall.GetLongPathName(from, &buf[0], uint32(len(buf)))
		if err != nil || n == 0 {
			return "", false
		}
		if int(n) <= len(buf) {
			return syscall.UTF16ToString(buf[:n]), true
		}
		buf = make([]uint16, n) // n is the required length, including the NUL
	}
}
