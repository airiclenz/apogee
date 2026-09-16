package main

// The documentation-drift gate on the headless exit codes: the `--help` text and the manual's
// exit table are both hand-maintained lists of the codes the command distinguishes, and a code
// added to the const block without a sentence in one or a row in the other is a code a script
// author cannot plan for. The twin of docs_eventlines_test.go's manual-vs-code reading test.

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
)

// headlessExitCodes is every code `apogee headless` can end with: 0 is the success the const
// block leaves implicit, the rest are its three named outcomes.
var headlessExitCodes = []int{0, exitRunFailed, exitNotStarted, exitRunFaulted}

// manualExitRow matches one body row of the manual's `| Exit | Means |` table, capturing the code.
var manualExitRow = regexp.MustCompile("(?m)^\\| `(\\d+)` \\|")

// TestHeadlessHelpNamesEveryExitCode holds the command's Long text to the const block — one
// `N the run` phrase per code — and the manual's exit table to the same list: exactly one row per
// code and no row for a code the binary never returns. The unanswered-server clause is asserted on
// both, since it is the one exit-2 cause the help text used to leave out.
func TestHeadlessHelpNamesEveryExitCode(t *testing.T) {
	t.Parallel()

	long := newHeadlessCommand().Long
	for _, code := range headlessExitCodes {
		if want := fmt.Sprintf("%d the run", code); !strings.Contains(long, want) {
			t.Errorf("headless --help does not say %q; the exit-code sentence has drifted from the const block", want)
		}
	}

	body, err := os.ReadFile(manualHeadlessPath)
	if err != nil {
		t.Fatalf("read %s: %v", manualHeadlessPath, err)
	}
	page := string(body)

	rows := map[string]bool{}
	for _, m := range manualExitRow.FindAllStringSubmatch(page, -1) {
		rows[m[1]] = true
	}
	for _, code := range headlessExitCodes {
		if !rows[fmt.Sprint(code)] {
			t.Errorf("%s's exit table has no `%d` row; the table has drifted from the const block",
				manualHeadlessPath, code)
		}
	}
	if len(rows) != len(headlessExitCodes) {
		t.Errorf("%s's exit table has %d rows for %d exit codes", manualHeadlessPath, len(rows), len(headlessExitCodes))
	}

	const unanswered = "a server that did not answer"
	if !strings.Contains(long, unanswered) {
		t.Errorf("headless --help does not name %q among the exit-2 causes", unanswered)
	}
	if !strings.Contains(page, unanswered) {
		t.Errorf("%s does not name %q among the exit-2 causes", manualHeadlessPath, unanswered)
	}
}
