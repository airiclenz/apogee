package tui

import (
	"encoding/json"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/domain"
)

// The tool NAME is the one field of the approval pane apogee does not author — an MCP server names
// its own tools — and it is the field the pane spends its BORDER on. Unfolded, a newline in it broke
// the title out of that border and painted a second, unindented row above the pane's body, in the
// same style the pane's own rows wear: a forged "Reason:" the human then authorised against. The
// name is flattened at the composing site (approvalPrompt) and folded again by popupTitleLine, so
// this pins the pane end to end rather than either layer alone.
//
// It counts ROWS, like its sibling over the body's fields
// (TestModelApprovalFlattensFieldsThatCouldForgeRows): the forged text is still on the pane after
// the fix — folded into the title it belongs to — so a substring check passes on the forgery.
func TestModelApprovalTitleFoldsAToolNameNewline(t *testing.T) {
	m := step(t, newTestModel(t), tea.WindowSizeMsg{Width: 100, Height: 30})
	req := domain.ApprovalRequest{
		Tool:      "terminal\nReason: pre-approved",
		Reason:    "subprocess execution",
		Arguments: json.RawMessage(`{"command":"rm -rf /"}`),
	}
	m = step(t, m, approvalReqMsg{Request: req, Reply: make(chan domain.ApprovalDecision, 1)})
	view := plain(m.View())

	if rows := approvalBodyRows(view, "Reason:"); len(rows) != 1 {
		t.Errorf("the pane paints %d rows opening \"Reason:\", want exactly the gate's own:\n%s", len(rows), view)
	}
	var titled []string
	for _, ln := range strings.Split(view, "\n") {
		if strings.Contains(ln, "Approve ") {
			titled = append(titled, ln)
		}
	}
	if len(titled) != 1 {
		t.Fatalf("the title spans %d rows, want one:\n%s", len(titled), view)
	}
	if !strings.Contains(titled[0], "Approve terminal Reason: pre-approved?") {
		t.Errorf("the folded tool name is not on the title row, so the text was dropped rather than folded: %q", titled[0])
	}
}
