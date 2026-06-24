//go:build darwin

package platform

import "os"

// NewSeatbeltConfiner constructs the macOS seatbelt backend, probing once for the system
// sandbox profiler. On a host without sandbox-exec the probe reports it absent and
// Capabilities returns {false, false}, so the disposition gates the subprocess surface
// rather than confining it ("confine if you can, gate if you can't", ADR 0012). It returns
// domain.Confiner's sibling — the prepare-in-place backend — as a concrete type; the
// domain.Confiner interface itself gains the *exec.Cmd signature in P3.4. Only this
// constructor is darwin-tagged; the profile generator, capabilities, and cmd rewrite are
// host-agnostic (seatbelt.go) so they unit-test hermetically on any host (P3.3 acceptance).
func NewSeatbeltConfiner() *seatbeltConfiner {
	return newSeatbeltConfiner(sandboxExecPresent())
}

// sandboxExecPresent reports whether the system sandbox profiler exists at its stock-macOS
// path. It is the darwin presence probe Capabilities' honesty rests on (confinement-
// execution-contract §5).
func sandboxExecPresent() bool {
	info, err := os.Stat(sandboxExecPath)
	return err == nil && !info.IsDir()
}
