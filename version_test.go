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
	// The test binary is a repo build, so Version() typically carries a "+[<count>.]g<commit>[.dirty]"
	// provenance suffix; the release number is the part before the first "+".
	base, _, _ := strings.Cut(apogee.Version(), "+")
	if base != want {
		t.Errorf("apogee.Version() base = %q; want the VERSION file contents %q (full: %q)", base, want, apogee.Version())
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
