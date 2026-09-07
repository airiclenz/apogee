package main

// The documentation-drift gate on the Event lines contract: the manual's account of
// `apogee headless --format json` is asserted against the code that writes the lines rather than
// re-read by a human. A vocabulary the manual half-lists is a vocabulary a consumer half-implements.
//
// It is a cheap grep over a file the repo layout fixes in place, so a missing page is a failure
// rather than a reason to skip — the twin of docs_env_test.go's reading tests.

import (
	"os"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/eventjson"
)

const manualHeadlessPath = "../../docs/manual/headless.md"

// TestManualListsEveryEventLineKind holds the manual's table of line kinds to eventjson.Kinds(),
// which IS the vocabulary the writer emits (ADR 0075 §4): every name appears on the page as a
// backticked token, so a kind added to the encoder without a row here fails before it ships. The
// four fixed phrases beside it are the section's load-bearing ones — the flag that turns the stream
// on, the two frames that bracket it, and the one stderr line that says the stream has stopped.
func TestManualListsEveryEventLineKind(t *testing.T) {
	t.Parallel()

	body, err := os.ReadFile(manualHeadlessPath)
	if err != nil {
		t.Fatalf("read %s: %v", manualHeadlessPath, err)
	}
	page := string(body)

	kinds := eventjson.Kinds()
	if len(kinds) == 0 {
		t.Fatal("eventjson.Kinds() named no line kinds; the vocabulary scan has stopped working")
	}
	for _, kind := range kinds {
		if !strings.Contains(page, "`"+kind+"`") {
			t.Errorf("%s does not name the %q line kind as a `%s` token; the Event lines table has drifted",
				manualHeadlessPath, kind, kind)
		}
	}

	for _, want := range []string{
		"--format json",
		"run_started",
		"run_finished",
		"event lines stopped",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("%s no longer says %q; the Event lines section has drifted", manualHeadlessPath, want)
		}
	}
}
