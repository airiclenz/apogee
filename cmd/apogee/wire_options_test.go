package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/mechanisms"
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

// The read half of the same seam: what the `/settings` Mechanism list OFFERS. It offers the
// CATALOGUE — one switch per catalogued id, each answered from the FILE's manual block — and never
// the block's own keys, so a `mechanisms:` line naming something this build does not catalogue adds
// no row. With the shipped catalogue empty since v0.20.0 (ADR 0071) that makes the list empty
// whatever the block says, which is the case a user upgrading into this wave is actually in: their
// saved block names rows that retired, the resolver tolerates them, and the pane must not offer the
// old spellings as switches that would promise a flip doing nothing.
//
// It is EMPTY and not nil: a list that vanished would look like an unwired seam to the pane, which
// answers the two differently (internal/tui: an unwired catalogue opens nothing, an empty one opens
// and says so).
func TestListMechanismsOffersTheCatalogueNotTheBlock(t *testing.T) {
	t.Parallel()

	w := urlGuardWiring(t, config.Options{})
	if err := w.wireSession(context.Background()); err != nil {
		t.Fatalf("wireSession: %v", err)
	}
	// The list re-reads the FILE rather than the resolution this run started on, so the block is
	// written here — and it names a RETIRED id, which is the one thing a saved config plausibly
	// carries now that nothing is catalogued.
	const retired = "autofix"
	if !mechanisms.IsRetired(retired) {
		t.Fatalf("%s is not on the retired roll; this case needs an id a saved config would still carry", retired)
	}
	path := filepath.Join(w.roots.config, "config.yaml")
	if err := os.WriteFile(path, []byte("mechanisms:\n  "+retired+": true\n"), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}

	toggles := w.options().ListMechanisms()
	if toggles == nil {
		t.Fatal("ListMechanisms() = nil; an empty catalogue is an empty LIST, and the pane tells the two apart")
	}

	listed := make([]string, 0, len(toggles))
	for _, toggle := range toggles {
		listed = append(listed, toggle.ID)
	}
	want := make([]string, 0, len(mechanisms.KnownIDs()))
	for _, id := range mechanisms.KnownIDs() {
		want = append(want, string(id))
	}
	if !slices.Equal(listed, want) {
		t.Errorf("ListMechanisms() offered %v, want the catalogue %v — the list is the catalogue, "+
			"not the block", listed, want)
	}
	if slices.Contains(listed, retired) {
		t.Errorf("the list offered the retired id %q as a switch; flipping it would arm nothing", retired)
	}
}
