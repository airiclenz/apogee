package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/domain"
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

// The read half of the same seam: what the `/settings` Mechanism list SAYS about the rows a
// `mechanisms:` block names and the rows it leaves out. The list has to show the posture the run is
// actually in — the floor unioned in on top of the block (ADR 0070). No catalogued row declares
// CapOffRamp any more, the two recoveries that did being Floor guards since ADR 0071, so the floor
// contributes nothing today and the list is exactly what the block says: the named row ON, every
// other row listed and OFF.
func TestListMechanismsAppliesTheOffRampFloor(t *testing.T) {
	t.Parallel()

	w := urlGuardWiring(t, config.Options{})
	if err := w.wireSession(context.Background()); err != nil {
		t.Fatalf("wireSession: %v", err)
	}
	// The list re-reads the FILE rather than the resolution this run started on, so the block is
	// written here — and it names one ordinary Mechanism and nothing else.
	const named = "autofix"
	path := filepath.Join(w.roots.config, "config.yaml")
	if err := os.WriteFile(path, []byte("mechanisms:\n  "+named+": true\n"), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	// The OFF half's id is taken from the catalogue rather than hard-coded: a row that later joined the
	// off-ramp Capability would otherwise turn this half into a silent contradiction of the one above.
	var ordinary string
	for _, d := range mechanisms.Descriptors() {
		if d.Capability != domain.CapOffRamp && string(d.ID) != named {
			ordinary = string(d.ID)
			break
		}
	}
	if ordinary == "" {
		t.Fatalf("the catalogue offers no ordinary Mechanism beside %s; the OFF half cannot be asserted", named)
	}

	enabled := map[string]bool{}
	for _, toggle := range w.options().ListMechanisms() {
		enabled[toggle.ID] = toggle.Enabled
	}

	for _, id := range mechanisms.OffRampFloor(nil) {
		if on, listed := enabled[string(id)]; !listed || !on {
			t.Errorf("%s = (enabled %v, listed %v), want an armed row: a floored row the block never "+
				"names is ON (ADR 0070), and the pane must not say it is off", id, on, listed)
		}
	}
	if !enabled[named] {
		t.Errorf("%s reads OFF; the block names it true and the list is answered from the file", named)
	}
	if on, listed := enabled[ordinary]; !listed || on {
		t.Errorf("%s = (enabled %v, listed %v), want a listed row reading OFF: the floor arms only "+
			"what declares CapOffRamp, not every Mechanism the block leaves out", ordinary, on, listed)
	}
}
