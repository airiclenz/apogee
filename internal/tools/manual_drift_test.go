package tools

import (
	"os"
	"regexp"
	"strconv"
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

// readmeToolCounts matches the README's one sentence that counts the tools: "**36 built-in tools**
// (30 on the default menu)". A rewording that drops it fails the test below rather than skipping it.
var readmeToolCounts = regexp.MustCompile(`\*\*(\d+) built-in tools\*\* \((\d+) on the default menu\)`)

// TestReadmeStatesTheToolCounts pins the two tool counts the README states to the ones this build
// carries: the built-in total is KnownToolNames (every tool the build knows, default-off included),
// and the default menu is what DefaultToolsWithHost composes when a Driver backs all three
// host-delegate tools (ask_user, present_document, load_skill) and configuration lifts nothing. A
// tool added, removed or moved on or off the default menu fails here until the README says so.
func TestReadmeStatesTheToolCounts(t *testing.T) {
	t.Parallel()

	const readmePath = "../../README.md"

	readme, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("reading %s: %v", readmePath, err)
	}
	match := readmeToolCounts.FindSubmatch(readme)
	if match == nil {
		t.Fatalf("%s no longer states the tool counts as %q", readmePath, readmeToolCounts)
	}
	statedTotal, _ := strconv.Atoi(string(match[1]))
	statedMenu, _ := strconv.Atoi(string(match[2]))

	if got := len(KnownToolNames()); statedTotal != got {
		t.Errorf("%s states %d built-in tools; KnownToolNames lists %d", readmePath, statedTotal, got)
	}
	menu := DefaultToolsWithHost(t.TempDir(), HostTools{
		Asker: stubAsker{}, Presenter: stubPresenter{}, SkillLookup: &stubLookup{},
	})
	if got := len(menu); statedMenu != got {
		t.Errorf("%s states %d tools on the default menu; the fully backed default menu holds %d", readmePath, statedMenu, got)
	}
}
