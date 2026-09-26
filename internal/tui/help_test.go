package tui

import (
	"strings"
	"testing"
)

// TestHelpNoteListsEveryVerb pins the /help note to the registry and the box: every commandSpecs
// row is a "/name — summary" line, a blank line separates the list from the legend, and the legend
// trailer spells the newline chord as the idle legend for THAT terminal does — ⌥⏎ alone until key
// disambiguation is confirmed, ⇧⏎/⌥⏎ after — the stop as the running legend does, and the one-run
// stop as a run view's header does. Each copied
// cell is asserted against the prompteditor.go constant it copies, so /help can never teach a
// spelling the box has stopped showing.
func TestHelpNoteListsEveryVerb(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name              string
		keyDisambiguation bool
		idleLegend        string
		wantNewline       string
	}{
		{"pessimistic", false, idlePlaceholder, helpKeyNewline},
		{"disambiguated", true, idleShiftPlaceholder, helpKeyNewlineShift},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			note := helpNote(c.keyDisambiguation)
			lines := strings.Split(note, "\n")

			// One line per registry row, in table order, then the blank and the legend.
			if want := len(commandSpecs) + 2; len(lines) != want {
				t.Fatalf("help note has %d lines, want %d (every verb, a blank, the legend):\n%s", len(lines), want, note)
			}
			for i, spec := range commandSpecs {
				if want := "/" + spec.name + " — " + spec.summary; lines[i] != want {
					t.Errorf("line %d = %q, want %q", i, lines[i], want)
				}
			}
			if lines[len(commandSpecs)] != "" {
				t.Errorf("line %d = %q, want a blank line before the legend", len(commandSpecs), lines[len(commandSpecs)])
			}

			// The legend trailer names the fixed cells, with the newline chord this terminal delivers.
			legend := lines[len(lines)-1]
			if want := helpLegendPrefix + strings.Join([]string{
				helpKeySend, c.wantNewline, helpKeyRecall, helpKeyStop, helpKeyStopRun, helpKeyQuit, helpKeyMode, helpKeyScroll,
			}, helpCellSeparator); legend != want {
				t.Errorf("legend = %q, want %q", legend, want)
			}
			if !strings.Contains(c.idleLegend, c.wantNewline) {
				t.Errorf("newline cell %q is not spelled by the idle legend %q", c.wantNewline, c.idleLegend)
			}
			if !strings.Contains(c.idleLegend, helpKeySend) || !strings.Contains(c.idleLegend, helpKeyQuit) {
				t.Errorf("send/quit cells %q %q are not spelled by the idle legend %q", helpKeySend, helpKeyQuit, c.idleLegend)
			}
			if !strings.Contains(runningPlaceholder, helpKeyStop) {
				t.Errorf("stop cell %q is not spelled by the running legend %q", helpKeyStop, runningPlaceholder)
			}
			if !strings.HasSuffix(breadcrumbStopHint, helpCellSeparator+helpKeyStopRun) {
				t.Errorf("one-run stop cell %q is not spelled by the run view's hint %q", helpKeyStopRun, breadcrumbStopHint)
			}
		})
	}
}
