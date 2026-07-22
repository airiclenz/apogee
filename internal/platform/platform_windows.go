//go:build windows

package platform

import (
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

// Current returns the platform Host for this Windows build target: the Windows rule set
// (host.go) with the OS long-path resolver wired in, so Contains can expand an 8.3 short
// name instead of refusing to compare it. Everything else about the rules is pure data and
// is exercised on every OS by the shared table tests; this file is the only part that
// needs a real Windows host, and Phase 5 runs its tests there natively.
func Current() Host { return currentRules() }

// currentRules is Current's concrete return, for the callers inside this package that need
// the rule TABLE rather than the Host interface: the token Confiner evaluates its labelling
// guardrails against split's "can this path be compared at all" answer, which Contains
// deliberately collapses into a plain false.
func currentRules() hostRules {
	rules := windowsRules()
	rules.longPath = longPathName
	rules.finalPath = finalPathName
	return rules
}

// longPathName expands an 8.3 short path ("C:\PROGRA~1\Go") to its long form through
// kernel32's GetLongPathNameW, reporting whether the answer is AUTHORITATIVE — the ok the
// longPath seam requires (hostRules.longPath).
//
// That call is only defined for a path that EXISTS, and a confinement box is routinely
// asked about a file that does not exist yet, so the expansion walks up to the longest
// existing prefix and re-appends the unresolved tail. ok is true when every 8.3-SHAPED
// component fell inside the resolved prefix: GetLongPathNameW expanded or verified each
// one, and a component it verified UNCHANGED is a directory genuinely named like a short
// name (demo~1) — its own long form, not a failure. ok is false when a short-shaped
// component is left in the unresolved tail, or nothing at all was resolvable (a path with
// no existing ancestor, an API failure): such a name might alias anything — PROGRA~1 is
// "Program Files" on one machine and "Program Files (x86)" on the next — so split rejects
// it and Contains reports "not contained" rather than matching a name it could not
// verify, which is the safe answer for both of its callers: a root it cannot resolve is
// neither collapsed into another root nor waved past a labelling guardrail.
func longPathName(p string) (string, bool) {
	if !strings.Contains(p, "~") {
		return p, true // nothing short-shaped to expand: the input is its own answer
	}
	for current, tail := p, ""; ; {
		if long, ok := expandShortPath(current); ok {
			return filepath.Join(long, tail), !hasShortName(tail, `\`)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return p, false // reached the volume root with nothing resolvable
		}
		tail = filepath.Join(filepath.Base(current), tail)
		current = parent
	}
}

// finalPathName resolves p to its final on-disk form through kernel32's
// GetFinalPathNameByHandle — the finalPath seam (hostRules.finalPath). Unlike longPathName
// it answers only for a path that EXISTS: the answer comes from an open handle, which is
// what makes it authoritative — reparse points traversed, 8.3 aliases expanded, the
// trailing dots and spaces Win32 canonicalization strips removed — and a path that cannot
// be opened yields ok=false, which the caller refuses on rather than guesses about
// (resolveBoxRoots). The `\\?\` prefix the API returns is stripped so the answer compares
// — and reads, in a refusal — as an ordinary path.
func finalPathName(p string) (string, bool) {
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
			final, _ := stripLongPathPrefix(syscall.UTF16ToString(buf[:n]))
			return final, true
		}
		buf = make([]uint16, n) // n is the required length, including the NUL
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
