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

// TestLooksLikeConfinementDenial pins the line-anchored signature match the confined-run
// watch and the terminal's result label share: every documented denial spelling — EPERM's,
// EACCES's and, since 2026-09-17, EROFS's — matches at a line's end — with each toolchain's
// allowed tail, a CR-terminated line and a final newline-less line included — a bounded
// errno name matches anywhere on the line, and a line that merely contains the phrase
// mid-sentence does not.
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
		{"libc strerror EROFS (namespace)", "mkdir: cannot create directory '/tmp/srtest': Read-only file system", true},
		{"libc strerror EROFS with coreutils' Unicode quotes", "mkdir: cannot create directory ‘/tmp/srtest’: Read-only file system", true},
		{"Go errno text EROFS", "open /etc/f: read-only file system", true},
		{"python OSError EROFS", "OSError: [Errno 30] Read-only file system: '/x'", true},
		{"node errno prefix EROFS", "Error: EROFS: read-only file system, mkdir '/x'", true},
		{"bare errno name EROFS", "write failed: EROFS", true},
		{"libc capitalised at line end", "open /etc/x: Permission denied", true},
		{"python PermissionError", "PermissionError: [Errno 13] Permission denied: '/etc/x'", true},
		{"python os.rename two paths", "PermissionError: [Errno 13] Permission denied: '/a' -> '/b'", true},
		{"rust os error tail", "Permission denied (os error 13)", true},
		{"java parenthesised", "java.io.FileNotFoundException: /etc/x (Permission denied)", true},
		{"perl at-line tail", "Permission denied at x.pl line 3.", true},
		{"node errno prefix", "Error: EACCES: permission denied, open '/x'", true},
		{"ruby errno constant", "Errno::EACCES", true},
		{"rsync numeric tail", "Permission denied (13)", true},
		{"CR-terminated PTY line", "mkdir: Permission denied\r\n", true},
		{"final newline-less line after clean lines", "building...\nopen /dev/ptmx: permission denied", true},
		{"phrase mid-sentence", "permission denied for user x", false},
		{"phrase followed by prose", "note: permission denied earlier", false},
		{"read-only phrase mid-sentence", "the read-only file system was mounted earlier", false},
		{"errno letters inside an identifier", "MYEPERMISSION=1", false},
		{"EROFS letters inside an identifier", "MYEROFSFLAG=1", false},
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

// TestDenialKillWriterMatchesAcrossWriteBoundary pins the carried-line scan: a line split
// across two pipe chunks — inside the signature itself, or inside a long allowed tail after
// it — still triggers the kill once the line is whole.
func TestDenialKillWriterMatchesAcrossWriteBoundary(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		first  string
		second string
	}{
		{"split inside the signature", "mkdir: x: Operation not per", "mitted\n"},
		{"split inside a long allowed tail", "PermissionError: [Errno 13] Permission denied: '/etc/some/long/pa", "th'\n"},
		{"split inside the read-only signature", "mkdir: cannot create directory '/x': Read-only file sys", "tem\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var out strings.Builder
			killed := false
			w := NewDenialKillWriter(&out, func() { killed = true })

			if _, err := w.Write([]byte(tc.first)); err != nil {
				t.Fatalf("Write: %v", err)
			}
			if _, err := w.Write([]byte(tc.second)); err != nil {
				t.Fatalf("Write: %v", err)
			}

			if !killed || !w.Detected() {
				t.Errorf("split line not detected: killed=%v Detected=%v", killed, w.Detected())
			}
			if got := out.String(); got != tc.first+tc.second {
				t.Errorf("forwarded output = %q, want every byte forwarded", got)
			}
		})
	}
}

// TestDenialKillWriterHalfSignatureDoesNotKill pins the carry's negative half: a chunk
// ending inside the phrase is not a match on its own — the kill waits for the rest of the
// line.
func TestDenialKillWriterHalfSignatureDoesNotKill(t *testing.T) {
	t.Parallel()

	var out strings.Builder
	w := NewDenialKillWriter(&out, func() { t.Error("kill fired on a half signature") })

	if _, err := w.Write([]byte("mkdir: x: Operation not per")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if w.Detected() {
		t.Error("Detected() = true on a half signature")
	}
}

// TestDenialKillWriterIgnoresPhraseMidLine pins the anchoring live: a line that contains
// the phrase without ending in it streams through unkilled, and a later real denial on its
// own line still kills.
func TestDenialKillWriterIgnoresPhraseMidLine(t *testing.T) {
	t.Parallel()

	var out strings.Builder
	kills := 0
	w := NewDenialKillWriter(&out, func() { kills++ })

	if _, err := w.Write([]byte("note: permission denied earlier\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if kills != 0 || w.Detected() {
		t.Fatalf("kill fired on a mid-line phrase: kills=%d Detected=%v", kills, w.Detected())
	}
	if _, err := w.Write([]byte("open /etc/x: permission denied\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if kills != 1 || !w.Detected() {
		t.Errorf("line-end denial after prose not detected: kills=%d Detected=%v", kills, w.Detected())
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
