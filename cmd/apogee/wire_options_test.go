package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The Mechanism write seam, pinned where it is BUILT rather than through the renderer's fake: the
// chain is two acts with two different failures, and which one fired is the whole of what the pane
// has to say afterwards (ADR 0037 decision 1). A splice that landed under a failed apply leaves the
// file ahead of the session — "saved" — where a refused splice left nothing at all.
func TestWriteMechanismReportsWhichHalfFailed(t *testing.T) {
	t.Parallel()

	const id = "codeinfo"
	type applyCall struct{ key, value string }

	t.Run("a landed splice and a landed apply answer saved with no error", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "config.yaml")
		refreshed := 0
		var calls []applyCall
		write := writeMechanismFor(path, func() { refreshed++ }, func(key, value string) (string, error) {
			calls = append(calls, applyCall{key, value})
			return "", nil
		})

		saved, err := write(id, true)

		if !saved || err != nil {
			t.Fatalf("write = (%v, %v), want (true, nil)", saved, err)
		}
		// The apply is asked about the BLOCK, valueless: that arm re-reads the file itself.
		if want := []applyCall{{settingKeyMechanisms, ""}}; len(calls) != 1 || calls[0] != want[0] {
			t.Errorf("apply calls = %+v, want %+v", calls, want)
		}
		if refreshed != 1 {
			t.Errorf("baseline re-taken %d times, want once — a landed write is not an external edit", refreshed)
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatalf("read back %s: %v", path, readErr)
		}
		if !strings.Contains(string(body), id+": true") {
			t.Errorf("the file does not carry the flip:\n%s", body)
		}
	})

	t.Run("a refused splice answers false and never reaches the apply", func(t *testing.T) {
		t.Parallel()
		// The config's own parent is a FILE, so the splice cannot even open its document — the
		// portable stand-in for the read-only config home the pane reports.
		home := t.TempDir()
		blocker := filepath.Join(home, "blocker")
		if err := os.WriteFile(blocker, []byte("not a directory"), 0o600); err != nil {
			t.Fatalf("write %s: %v", blocker, err)
		}
		refreshed := 0
		applied := 0
		write := writeMechanismFor(filepath.Join(blocker, "config.yaml"), func() { refreshed++ },
			func(string, string) (string, error) { applied++; return "", nil })

		saved, err := write(id, true)

		if saved || err == nil {
			t.Fatalf("write = (%v, %v), want (false, an error) — nothing was written", saved, err)
		}
		if applied != 0 || refreshed != 0 {
			t.Errorf("a refused splice applied %d times and refreshed %d, want neither", applied, refreshed)
		}
	})

	t.Run("a landed splice under a failed apply answers saved beside the error", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "config.yaml")
		refused := errors.New("mechanisms: unknown id")
		write := writeMechanismFor(path, func() {}, func(string, string) (string, error) { return "", refused })

		saved, err := write(id, true)

		if !saved || !errors.Is(err, refused) {
			t.Fatalf("write = (%v, %v), want (true, %v) — the file changed, the session did not", saved, err, refused)
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatalf("read back %s: %v", path, readErr)
		}
		if !strings.Contains(string(body), id+": true") {
			t.Errorf("the apply's refusal unwrote the splice:\n%s", body)
		}
	})
}
