//go:build linux || darwin

package gitexec

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestFileprint_SameLengthRewriteWithRestoredMtimeBreaksThePrint pins that the config probe's
// fingerprint carries ctime: an in-place rewrite that keeps the size, the inode and — through a
// utimes call — the mtime still reads as a change, because the rewrite moved the inode change
// time, which no userland call can put back.
func TestFileprint_SameLengthRewriteWithRestoredMtimeBreaksThePrint(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	writeConfigFixture(t, path, "[core]\n\tbare = false\n")
	before, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat fixture: %v", err)
	}
	print := takeFileprint(path)
	beforeCtime, ok := changeTime(before)
	if !ok {
		t.Fatal("changeTime reported no ctime on a platform that exposes one")
	}

	// Rewrite until the ctime tick moves: a coarse filesystem clock can leave a rewrite made
	// within the same tick with an unchanged ctime, which no fingerprint could see.
	deadline := time.Now().Add(2 * time.Second)
	for {
		time.Sleep(20 * time.Millisecond)
		writeConfigFixture(t, path, "[alias]\n\tst = !false\n")
		if err := os.Chtimes(path, before.ModTime(), before.ModTime()); err != nil {
			t.Fatalf("restore mtime: %v", err)
		}
		after, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat rewrite: %v", err)
		}
		if afterCtime, _ := changeTime(after); !afterCtime.Equal(beforeCtime) {
			if after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) || !os.SameFile(before, after) {
				t.Fatalf("rewrite changed size, mtime or inode; the fixture no longer isolates ctime")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("ctime never moved across rewrites")
		}
	}

	if print.holds() {
		t.Error("holds() = true after a same-size, same-mtime, same-inode rewrite, want false")
	}
}

// TestFileprint_UntouchedFileHolds pins the other half: a file nobody wrote keeps its print, so
// the memo still serves its answer.
func TestFileprint_UntouchedFileHolds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	writeConfigFixture(t, path, "[core]\n\tbare = false\n")
	print := takeFileprint(path)
	time.Sleep(20 * time.Millisecond)

	if !print.holds() {
		t.Error("holds() = false on an untouched file, want true")
	}
}

// writeConfigFixture writes text over path in place, keeping the inode.
func writeConfigFixture(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
