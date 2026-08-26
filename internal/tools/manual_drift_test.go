package tools

import (
	"os"
	"strings"
	"testing"
)

// TestManualListsEveryKnownToolName pins the hand-written tool-name list in the user manual to the
// names this build actually carries: docs/manual/configuration.md tells the reader which names
// `tools.disabled:`/`tools.enabled:` and a profile's `tools:` axis accept, and a tool added or
// renamed here silently makes that list wrong. This is the first test in the repo that reads a
// manual page — the shape is deliberately minimal (read the file, assert each name appears in
// back-ticks) so the next hand-maintained list has something to copy.
//
// The path is relative to the package directory, which is where `go test` runs: the repo layout is
// fixed, so a missing file is a failure rather than a reason to skip.
func TestManualListsEveryKnownToolName(t *testing.T) {
	t.Parallel()

	const manualPath = "../../docs/manual/configuration.md"

	manual, err := os.ReadFile(manualPath)
	if err != nil {
		t.Fatalf("reading %s: %v", manualPath, err)
	}

	var missing []string
	for _, name := range KnownToolNames() {
		if !strings.Contains(string(manual), "`"+name+"`") {
			missing = append(missing, name)
		}
	}

	if len(missing) > 0 {
		t.Errorf("%s names no %s; the manual's tool list has fallen behind KnownToolNames",
			manualPath, strings.Join(missing, ", "))
	}
}
