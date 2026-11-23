// Package junctiontest makes NTFS junctions for the Windows tests: the one place a test shells out
// to mklink. A junction is the reparse point any user can plant — it needs no administrator rights
// and no developer mode, unlike a symbolic link — so it is the link every Windows path test must
// see through.
//
// It is test support: only _test.go files import it, and it imports only the standard library. The
// file carries no build constraint so the package exists on every platform, but its helpers are
// only meaningful on Windows.
package junctiontest

import (
	"io/fs"
	"os"
	"os/exec"
	"testing"
)

// Make creates a directory junction at link pointing to the existing folder target and fails the
// test when it cannot, or when the result does not register as a reparse point.
func Make(t testing.TB, link, target string) {
	t.Helper()
	if out, err := mklink(link, target); err != nil {
		t.Fatalf("mklink /J %q %q: %v (%s)", link, target, err, out)
	}
	assertReparsePoint(t, link)
}

// MakeOrSkip is Make for a test that has nothing to prove where the filesystem refuses junctions:
// a failed mklink skips the test instead of failing it. A junction that was made but does not
// register as a reparse point still fails.
func MakeOrSkip(t testing.TB, link, target string) {
	t.Helper()
	if out, err := mklink(link, target); err != nil {
		t.Skipf("junctions are not available here: mklink /J %q %q: %v (%s)", link, target, err, out)
	}
	assertReparsePoint(t, link)
}

func mklink(link, target string) ([]byte, error) {
	return exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput()
}

func assertReparsePoint(t testing.TB, link string) {
	t.Helper()
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("lstat the junction %q: %v", link, err)
	}
	if info.Mode()&(fs.ModeSymlink|fs.ModeIrregular) == 0 {
		t.Fatalf("lstat %q mode = %v; the junction did not register as a reparse point", link, info.Mode())
	}
}
