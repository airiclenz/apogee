//go:build darwin

package platform

import (
	"testing"

	"github.com/airiclenz/apogee/internal/platform/confinetest"
)

// The live escape-probe drives a REAL sandbox-exec child through the shared escape
// battery (confinement-execution-contract §6.2) — it is owner-run / CI-only on macOS,
// since the dev host is Linux (no macOS, no sandbox-exec). On a macOS runner with
// sandbox-exec present, Probe asserts an out-of-box write is OS-denied (#1–#5) and
// ProbeNetwork asserts a non-allowlisted connect is denied while an open-network connect
// succeeds (#7/#8). When sandbox-exec is absent even on darwin, confinetest self-skips
// (Capabilities reports {false,false}) — the standard capability-gated test idiom.
//
// The non-darwin counterpart (seatbelt_notdarwin_test.go) self-skips loudly with a clear
// reason so the deferral is visible in `go test` output on this Linux host.

func TestSeatbeltProbe(t *testing.T) {
	// Not parallel: the confined children are real subprocesses.
	confinetest.Probe(t, newSeatbeltConfiner(sandboxExecPresent()))
}

func TestSeatbeltProbeNetwork(t *testing.T) {
	confinetest.ProbeNetwork(t, newSeatbeltConfiner(sandboxExecPresent()))
}
