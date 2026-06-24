//go:build !darwin && !windows

package platform

import "testing"

// On a non-darwin host the seatbelt live escape-probe cannot run — sandbox-exec is a
// macOS facility — so these self-skip LOUDLY with a clear reason, mirroring how the
// darwin build runs the real battery. This keeps the macOS live deferral visible in
// `go test` output on this Linux dev host (the deferral is owner-run / CI-only, P3.3
// acceptance), exactly as the landlock battery self-skips when landlock is unavailable.
// The hermetic profile-string, capability-honesty, and Confine-rewrite tests
// (seatbelt_test.go) DO run here and cover everything provable without a real macOS.
//
// The !windows guard matches seatbelt.go's build tag: the seatbelt backend (and these
// test names) do not compile on Windows, where only denyConfiner exists (Phase 5).

func TestSeatbeltProbe(t *testing.T) {
	t.Skip("seatbelt live escape-probe is macOS-only (sandbox-exec absent on " +
		"non-darwin); deferred to an owner-run / CI macOS runner — see P3.3. The hermetic " +
		"profile + caps tests in seatbelt_test.go run on this host.")
}

func TestSeatbeltProbeNetwork(t *testing.T) {
	t.Skip("seatbelt network escape-probe is macOS-only (sandbox-exec absent on " +
		"non-darwin); deferred to an owner-run / CI macOS runner — see P3.3.")
}
