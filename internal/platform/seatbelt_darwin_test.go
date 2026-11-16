//go:build darwin

package platform

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
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
	confinetest.Probe(t, newSeatbeltConfiner(sandboxExecPresent()), Current(), FailFastPreamble(), newProbeDenialKiller, newProbeMergedDenialKiller)
}

func TestSeatbeltProbeNetwork(t *testing.T) {
	confinetest.ProbeNetwork(t, newSeatbeltConfiner(sandboxExecPresent()), Current())
}

// TestSeatbeltAllowsDevNullThroughTheFence is the live twin of the hermetic
// TestSeatbeltProfileAllowsDevNull and the seatbelt counterpart of
// TestLandlockAllowsDevNullThroughTheFence: under a real sandbox-exec, a confined shell can
// write /dev/null while an ordinary out-of-box write is still denied and leaves no file.
func TestSeatbeltAllowsDevNullThroughTheFence(t *testing.T) {
	// Not parallel: the confined children are real subprocesses.
	c := newSeatbeltConfiner(sandboxExecPresent())
	if !c.Capabilities().FSWrite {
		t.Skip("sandbox-exec absent on this host (FSWrite==false); skipping the live /dev/null exemption probe")
	}

	ws := t.TempDir()
	outside := t.TempDir()
	box := domain.ConfinementBox{WorkspaceRoot: ws}

	t.Run("dev_null_write_succeeds", func(t *testing.T) {
		// Both halves of the idiom the fence would break: the stderr redirect every confined
		// terminal call uses, and a truncating `>` redirect.
		const line = `: 2>/dev/null && echo x > /dev/null`
		if err := runSeatbeltShellLine(t, c, box, line); err != nil {
			t.Fatalf("confined `sh -c %q` failed, want success (/dev/null is exempt from the fence): %v", line, err)
		}
	})

	t.Run("out_of_box_write_still_denied", func(t *testing.T) {
		// The exemption is exactly one device, not a hole in the fence: an ordinary out-of-box
		// write must still be OS-denied.
		target := filepath.Join(outside, "escape.txt")
		if err := runSeatbeltShellLine(t, c, box, "echo x > "+Current().Quote(target)); err == nil {
			t.Fatalf("confined write to %q succeeded, want OS denial (the exemption must not widen the fence)", target)
		}
		if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("stat %q = %v, want not-exist: the out-of-box write was not blocked", target, err)
		}
	})
}

// runSeatbeltShellLine runs line through the platform shell, confined to box by c, and returns
// the run error. It is the darwin twin of landlock_linux_test.go's runConfinedShellLine, for
// probes that are seatbelt-specific and so cannot join the cross-platform battery — /dev/null
// is a POSIX device with no Windows counterpart the battery could assert on.
func runSeatbeltShellLine(t *testing.T, c *seatbeltConfiner, box domain.ConfinementBox, line string) error {
	t.Helper()
	ctx := context.Background()
	argv := Current().Command(line)
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	if err := c.Confine(ctx, box, cmd); err != nil {
		t.Fatalf("Confine(%v): %v", argv, err)
	}
	return cmd.Run()
}
