package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/config"
)

// carriesSubAgentsServer reports whether the config file at path sets the `sub-agents-server:` key —
// an ACTIVE line rather than the commented example the seeded template documents it with, which is
// exactly the difference a clear has to make.
func carriesSubAgentsServer(t *testing.T, path string) bool {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the config back: %v", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "sub-agents-server:") {
			return true
		}
	}
	return false
}

// The picker's `auto` row records the ABSENCE of the key: the empty name is not a name to look up in
// the `servers:` list — which is why it does not reach the configured check at all — but the opt-out,
// so the choice survives a restart as the file NOT saying where sub-agents run. The verb clears the
// line and reports that it wrote, and a config that never set the key is already at that default.
func TestRecordSubAgentsServerChoiceClearsTheKey(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	path := filepath.Join(home, "config.yaml")
	if err := config.SaveSubAgentsServer(path, "my-box"); err != nil {
		t.Fatalf("seed the recorded key: %v", err)
	}
	if !carriesSubAgentsServer(t, path) {
		t.Fatal("the seeded config does not set sub-agents-server; the clear would prove nothing")
	}

	// The live list is deliberately nil: the empty name must never be looked up in it.
	w := wiringFor(home, nil)

	recorded, err := w.recordSubAgentsServerChoice("")
	if err != nil {
		t.Fatalf("recordSubAgentsServerChoice(\"\"): %v", err)
	}
	if !recorded {
		t.Error("the clear reported nothing written; the picker would say the choice is this session only")
	}
	if carriesSubAgentsServer(t, path) {
		data, _ := os.ReadFile(path)
		t.Errorf("the config still sets sub-agents-server:\n%s", data)
	}

	// A second clear has nothing to remove: a key the file does not set is already at its default.
	recorded, err = w.recordSubAgentsServerChoice("")
	if err != nil {
		t.Fatalf("recordSubAgentsServerChoice(\"\") on a config that does not set the key: %v", err)
	}
	if !recorded {
		t.Error("the second clear reported nothing written; the opt-out is recorded either way")
	}
}
