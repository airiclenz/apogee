package main

import (
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/tui"
)

// TestPaneMarkersNeverEchoACommandSummary holds every pane marker a driver test waits on right after
// a slash command to one rule: it is not a substring of any command summary. While the human types
// `/usage` the palette paints `❯ /usage  session token usage — …` above the prompt box, so a wait on
// a string the summary carries is satisfied by the palette row before the pane has opened — and the
// test reads a frame the pane is not on (apogee-htf). The markers are the panes' own chrome, which
// no palette row spells.
func TestPaneMarkersNeverEchoACommandSummary(t *testing.T) {
	t.Parallel()

	markers := []struct{ name, text string }{
		{"usagePaneMarker", usagePaneMarker},
		{"modelPaneMarker", modelPaneMarker},
		{"sessionsPaneMarker", sessionsPaneMarker},
		{"thinkingPaneMarker", thinkingPaneMarker},
		{"settingsHint", settingsHint},
	}
	summaries := tui.CommandSummaries()
	if len(summaries) == 0 {
		t.Fatal("tui.CommandSummaries() is empty; the guard has nothing to hold the markers against")
	}
	for _, m := range markers {
		if m.text == "" {
			t.Errorf("%s is empty; an empty marker is a substring of every summary", m.name)
			continue
		}
		for _, summary := range summaries {
			if strings.Contains(summary, m.text) {
				t.Errorf("%s %q is a substring of the command summary %q: the palette row satisfies the wait before the pane opens",
					m.name, m.text, summary)
			}
		}
	}
}
