package main

import (
	"bufio"
	"os"
	"regexp"
	"testing"
)

// TestReadmeArchiveInstallDoesNotPinAVersion is a tripwire, not a README
// parser: the "A prebuilt archive" install block must resolve VERSION from
// the latest GitHub release at run time, so a hard-coded `VERSION=0.x.y`
// — which goes stale with every release cut — must never return. The match
// is deliberately unanchored: prose and comments go stale exactly as fast as
// the runnable line, so the example beside it is a placeholder, not a number.
func TestReadmeArchiveInstallDoesNotPinAVersion(t *testing.T) {
	t.Parallel()

	f, err := os.Open("../../README.md")
	if err != nil {
		t.Fatalf("open README.md: %v", err)
	}
	defer func() { _ = f.Close() }()

	pinned := regexp.MustCompile(`VERSION=[0-9]`)
	sc := bufio.NewScanner(f)
	for line := 1; sc.Scan(); line++ {
		if pinned.MatchString(sc.Text()) {
			t.Errorf("README.md:%d pins a release version (%q); the archive-install block must resolve it from the latest release", line, sc.Text())
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan README.md: %v", err)
	}
}
