package platform

import (
	"io"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/platform/confinetest"
)

// newProbeDenialKiller adapts NewDenialKillWriter to the battery's factory seam
// (confinetest.DenialKillerFactory): the escape-probe drivers on every OS hand it in so
// the chained-script clobber probe runs the exact watch the terminal tool wires.
func newProbeDenialKiller(next io.Writer, kill func()) confinetest.DenialKiller {
	return NewDenialKillWriter(next, kill)
}

// TestLooksLikeConfinementDenial pins the signature match the confined-run watch and the
// terminal's result label share: every documented denial spelling matches, ordinary
// failure text does not.
func TestLooksLikeConfinementDenial(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		output string
		want   bool
	}{
		{"libc strerror EPERM (seatbelt)", "mkdir: /tmp/srtest: Operation not permitted", true},
		{"Go errno text EPERM", "open /etc/f: operation not permitted", true},
		{"bare errno name EPERM", "write failed: EPERM", true},
		{"libc strerror EACCES (landlock)", "mkdir: cannot create directory '/tmp/srtest': Permission denied", true},
		{"Go errno text EACCES", "open /etc/f: permission denied", true},
		{"bare errno name EACCES", "write failed: EACCES", true},
		{"unrelated failure", "no such file or directory", false},
		{"windows access denied deliberately unmatched", "Access is denied.", false},
		{"empty", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := LooksLikeConfinementDenial(tc.output); got != tc.want {
				t.Errorf("LooksLikeConfinementDenial(%q) = %v, want %v", tc.output, got, tc.want)
			}
		})
	}
}

// TestDenialKillWriterKillsOnceAndForwards pins the watch's contract: every byte is
// forwarded to the underlying writer, the kill fires exactly once on the first signature,
// and Detected reports the match.
func TestDenialKillWriterKillsOnceAndForwards(t *testing.T) {
	t.Parallel()

	var out strings.Builder
	kills := 0
	w := NewDenialKillWriter(&out, func() { kills++ })

	if _, err := w.Write([]byte("mkdir: x: Operation not permitted\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := w.Write([]byte("later: EPERM again\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if kills != 1 {
		t.Errorf("kill fired %d times, want exactly once", kills)
	}
	if !w.Detected() {
		t.Error("Detected() = false after a signature write")
	}
	if got := out.String(); got != "mkdir: x: Operation not permitted\nlater: EPERM again\n" {
		t.Errorf("forwarded output = %q, want every byte forwarded", got)
	}
}

// TestDenialKillWriterMatchesAcrossWriteBoundary pins the carried-tail scan: a signature
// split across two pipe chunks still triggers the kill.
func TestDenialKillWriterMatchesAcrossWriteBoundary(t *testing.T) {
	t.Parallel()

	var out strings.Builder
	killed := false
	w := NewDenialKillWriter(&out, func() { killed = true })

	if _, err := w.Write([]byte("mkdir: x: Operation not per")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if killed {
		t.Fatal("kill fired on a half signature")
	}
	if _, err := w.Write([]byte("mitted\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if !killed || !w.Detected() {
		t.Errorf("split signature not detected: killed=%v Detected=%v", killed, w.Detected())
	}
}

// TestDenialKillWriterIgnoresCleanOutput pins the negative: unmatched output forwards
// untouched, no kill, Detected stays false.
func TestDenialKillWriterIgnoresCleanOutput(t *testing.T) {
	t.Parallel()

	var out strings.Builder
	w := NewDenialKillWriter(&out, func() { t.Error("kill fired on clean output") })

	if _, err := w.Write([]byte("building...\nall tests passed\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if w.Detected() {
		t.Error("Detected() = true on clean output")
	}
	if got := out.String(); got != "building...\nall tests passed\n" {
		t.Errorf("forwarded output = %q", got)
	}
}
