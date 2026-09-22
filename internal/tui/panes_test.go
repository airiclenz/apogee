package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestEveryFramePaneHasASpec pins the pane table against the framePane constants: every pane below
// paneKinds has a row with a distinct name, a valid slot and its funcs filled — a pane declared
// without a row would index a zero row, a nil func rather than a build error, and the walks over
// the table would panic on the first frame or the first click. The key claim is the one func a row
// may leave nil, and only the prompt's: its keys are handleKey's state switches, not a rung of
// keyClaimOrder. modal is true for exactly the three panes that own the keyboard through such a rung
// plus the prompt, which owns it by state. The input slot holds exactly one pane, the dropdown,
// which is what lets stackInputSlot name it rather than filter for it.
func TestEveryFramePaneHasASpec(t *testing.T) {
	t.Parallel()

	modal := map[framePane]bool{panePrompt: true, paneBrowser: true, paneSettings: true, panePicker: true}
	names := map[string]framePane{}
	var inputPanes []framePane
	for p := framePane(0); p < paneKinds; p++ {
		row := paneSpecs[p]
		if row.name == "" {
			t.Errorf("pane %d has no name", p)
		} else if other, dup := names[row.name]; dup {
			t.Errorf("pane %d shares its name %q with pane %d", p, row.name, other)
		}
		names[row.name] = p
		if row.open == nil {
			t.Errorf("pane %d (%s) has no open predicate", p, row.name)
		}
		if row.render == nil {
			t.Errorf("pane %d (%s) has no renderer", p, row.name)
		}
		if row.click == nil {
			t.Errorf("pane %d (%s) has no click: the click chain would panic on it", p, row.name)
		}
		if row.wheel == nil {
			t.Errorf("pane %d (%s) has no wheel: the wheel chain would panic on it", p, row.name)
		}
		if (row.key == nil) != (p == panePrompt) {
			t.Errorf("pane %d (%s) key claim nil = %v; only the prompt's keys live outside keyClaimOrder", p, row.name, row.key == nil)
		}
		if row.modal != modal[p] {
			t.Errorf("pane %d (%s) modal = %v, want %v", p, row.name, row.modal, modal[p])
		}
		switch row.slot {
		case slotTranscript:
		case slotInput:
			inputPanes = append(inputPanes, p)
		default:
			t.Errorf("pane %d (%s) is in slot %d, which the frame has no walk for", p, row.name, row.slot)
		}
	}

	if len(inputPanes) != 1 || inputPanes[0] != paneDropdown {
		t.Errorf("the input slot holds %v, want the dropdown alone", inputPanes)
	}
}

// TestTheFourReportsPaintInTheFramePaneOrder is the paint the goldens do not cover: every report up at
// once, on a window tall enough to seat them all. Each is on the composed frame, each opens on the
// row the one above it closes on — the framePane order, walked once (stackTranscriptSlot) — and the
// frame the human sees carries their titles in that same order.
func TestTheFourReportsPaintInTheFramePaneOrder(t *testing.T) {
	t.Parallel()

	m := bothPanesModel(t, 4)
	m.thinkingPane = reportPane{open: true}
	m.advicePane = reportPane{open: true}
	m = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 60})

	reports := []reportKind{usageReport, inspectReport, thinkingReport, adviceReport}
	titles := []string{usageTitle, inspectorTitle, thinkingTitle, adviceTitle}
	below := -1
	for i, r := range reports {
		if !m.openPanes().has(r.pane()) {
			t.Fatalf("%s is open but not in the frame's pane set", titles[i])
		}
		y0, h, ok := m.reportPaneRect(r)
		if !ok {
			t.Fatalf("%s is not on the frame", titles[i])
		}
		if below >= 0 && y0 != below {
			t.Errorf("%s starts at row %d, want %d — directly under the pane above it", titles[i], y0, below)
		}
		below = y0 + h
	}

	painted := strip(m.View().Content)
	at := -1
	for _, title := range titles {
		i := strings.Index(painted, title)
		if i < 0 {
			t.Fatalf("the frame does not paint %q:\n%s", title, painted)
		}
		if i < at {
			t.Errorf("%q is painted above the report before it:\n%s", title, painted)
		}
		at = i
	}
}
