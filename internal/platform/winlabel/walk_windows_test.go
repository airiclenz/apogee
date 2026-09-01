//go:build windows

package winlabel

import (
	"os"
	"path/filepath"
	"testing"
)

// The Windows-tagged half of F-08's clear-side remediation. The rule itself (rootClearable)
// and the filter that applies it (revertibleRoots) are pure and tabled in retire_test.go, so
// they run on every OS; what needs a real SACL — and therefore this file — is the pre-clear
// pass that TAKES the root verdict: that it is taken once and skipped ever after, and that a
// root-only journal it cannot rewrite still clears the tree it always cleared.

// lowLabelledDir returns a temporary directory carrying apogee's own Low label — the state a
// root is in when a revert runs over its own journal. A host that will not let the test write
// a mandatory label to its own temp directory can prove nothing here, so the test skips rather
// than failing over a machine policy.
func lowLabelledDir(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	if err := SetSDDL(dir, lowSDDL); err != nil {
		t.Skipf("cannot write a mandatory label to %q on this host: %v", dir, err)
	}
	return dir
}

func TestJudgePriorsSkipsAnAlreadyJudgedRoot(t *testing.T) {
	// A root verdict is taken ONCE, before the first clear, and every later revert reads the
	// persisted answer instead of the disk (Entry.RootJudged) — because after the clear the
	// disk says only what ClearTree wrote. The skip is observable through the journal file: a
	// pass that judged anything rewrites it, so an untouched file proves nothing was judged.
	root := lowLabelledDir(t)
	own := filepath.Join(t.TempDir(), "labels-4242.json")
	r := Record{PID: 4242, Entries: []Entry{{Path: root, Root: true, RootJudged: true}}}

	if err := judgePriors(r, own); err != nil {
		t.Fatalf("judgePriors over an already-judged root: %v", err)
	}
	if !r.Entries[0].RootJudged {
		t.Error("the persisted root verdict was dropped by a pass that should not have re-judged it")
	}
	if _, err := os.Stat(own); !os.IsNotExist(err) {
		t.Errorf("the journal at %q was rewritten; an already-judged root must be skipped, not re-judged", own)
	}
}

func TestJudgePriorsClearsARootOnlyJournalItCannotRewrite(t *testing.T) {
	// Persisting the root verdict must never become a PRECONDITION of clearing. A root-only
	// journal is the overwhelmingly common case and wrote nothing here before the verdict
	// existed, so an unwritable apogee home would otherwise strand every label on runs that
	// clear cleanly today. The write failure is swallowed, the verdict stays in memory, and the
	// revert goes on; only a PRIOR's verdict keeps the abort.
	root := lowLabelledDir(t)
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("plant the unwritable journal home: %v", err)
	}
	own := filepath.Join(blocker, "labels-4242.json")
	r := Record{PID: 4242, Entries: []Entry{{Path: root, Root: true}}}

	if err := judgePriors(r, own); err != nil {
		t.Fatalf("judgePriors aborted a root-only journal it could not rewrite: %v", err)
	}
	if !r.Entries[0].RootJudged {
		t.Error("the root verdict was not taken in memory, so the retry after a failed descendant would re-judge a cleared root")
	}
}
