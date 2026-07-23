package apogee_test

import (
	"os"
	"strings"
	"testing"

	"github.com/airiclenz/apogee"
)

// TestVersionMatchesVERSIONFile ties the embedded value to the on-disk source of truth: the
// base of apogee.Version() (everything before the "+build-metadata" suffix) must equal the
// trimmed contents of the top-level VERSION file. The test runs with its working directory at
// the package root, so "VERSION" resolves to the same file go:embed captured — a mis-pointed
// embed directive fails here.
func TestVersionMatchesVERSIONFile(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("VERSION")
	if err != nil {
		t.Fatalf("read VERSION: %v", err)
	}
	want := strings.TrimSpace(string(raw))
	if want == "" {
		t.Fatal("the VERSION file is empty; it is the single source of truth and must carry a version")
	}
	// BaseVersion() reads the same embedded file, so a mis-pointed embed directive fails here too.
	if got := apogee.BaseVersion(); got != want {
		t.Errorf("apogee.BaseVersion() = %q; want the VERSION file contents %q", got, want)
	}
	// The test binary is a repo build, so Version() typically carries a "+[<count>.]g<commit>[.dirty]"
	// provenance suffix; the release number is the part before the first "+", which must agree.
	base, _, _ := strings.Cut(apogee.Version(), "+")
	if base != want {
		t.Errorf("apogee.Version() base = %q; want the VERSION file contents %q (full: %q)", base, want, apogee.Version())
	}
}

// TestBaseVersion checks the release version alone: it equals the trimmed VERSION file and
// carries none of the "+<count>.g<rev>[.dirty]" build provenance that Version() appends — it is
// the value the start-up box displays, whereas --version and /version show the full Version().
func TestBaseVersion(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("VERSION")
	if err != nil {
		t.Fatalf("read VERSION: %v", err)
	}
	want := strings.TrimSpace(string(raw))
	if want == "" {
		t.Fatal("the VERSION file is empty; it is the single source of truth and must carry a version")
	}
	got := apogee.BaseVersion()
	if got != want {
		t.Errorf("apogee.BaseVersion() = %q; want the VERSION file contents %q", got, want)
	}
	if strings.Contains(got, "+") {
		t.Errorf("apogee.BaseVersion() = %q carries a build-provenance suffix; it must be the release version alone", got)
	}
}

// TestVersionIsTrimmedAndNonEmpty guards the two properties every consumer relies on: the
// value is non-empty (the CLI --version, the start-up box) and carries no stray surrounding
// whitespace or trailing newline from the file.
func TestVersionIsTrimmedAndNonEmpty(t *testing.T) {
	t.Parallel()

	got := apogee.Version()
	if got == "" {
		t.Fatal("apogee.Version() is empty")
	}
	if got != strings.TrimSpace(got) {
		t.Errorf("apogee.Version() = %q has surrounding whitespace; want it trimmed", got)
	}
}
