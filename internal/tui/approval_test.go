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

// A tool whose reach is WIDER than its arguments — go vet takes one filename and reads every .go
// file in that file's directory — states so on the request (domain.ApprovalRequest.Scope), and the
// pane is where that has to be readable: it is the surface the human decides on. The line is
// painted like its Reason:/Fix: neighbours, labelled by this Driver rather than by the engine, and
// it is ABSENT (not blank) for the overwhelming majority of calls, whose tools declare no scope.
func TestModelApprovalRendersTheDeclaredScope(t *testing.T) {
	m := step(t, newTestModel(t), tea.WindowSizeMsg{Width: 100, Height: 30})
	req := domain.ApprovalRequest{
		Tool:      "diagnostics",
		Reason:    "subprocess execution",
		Arguments: json.RawMessage(`{"path":"internal/tools/diagnostics.go"}`),
		Scope:     "go vet reads the whole package directory internal/tools.",
	}
	m = step(t, m, approvalReqMsg{Request: req, Reply: make(chan domain.ApprovalDecision, 1)})

	rows := strings.Split(ansiPattern.ReplaceAllString(m.approvalPrompt(req), ""), "\n")
	got := strings.Join(rows, "\n")
	if !strings.Contains(got, "Scope: "+req.Scope) {
		t.Errorf("the declared scope did not reach the pane:\n%s", got)
	}
	if reason, scope := paneRowIndex(t, rows, "Reason:"), paneRowIndex(t, rows, "Scope:"); scope <= reason {
		t.Errorf("the scope sits on row %d, above the Reason on row %d it widens:\n%s", scope, reason, got)
	}

	// A tool that declares nothing: no line at all, not an empty label.
	req.Scope = ""
	if bare := ansiPattern.ReplaceAllString(m.approvalPrompt(req), ""); strings.Contains(bare, "Scope:") {
		t.Errorf("a request carrying no scope drew a Scope line:\n%s", bare)
	}
}

// The scope is a TOOL's text — a host-registered or MCP-backed tool authors its own — on a pane
// that paints one row per line, so it is flattened like every other field here. Unflattened, a
// scope carrying a newline would paint an unindented second row in the pane's own body style: a
// forged "Reason:" the human then authorises against. It counts ROWS rather than substrings,
// because the forged text is still on the pane after the fix — folded into the line it belongs to.
func TestModelApprovalFlattensAScopeThatCouldForgeARow(t *testing.T) {
	m := step(t, newTestModel(t), tea.WindowSizeMsg{Width: 100, Height: 30})
	req := domain.ApprovalRequest{
		Tool:      "diagnostics",
		Reason:    "subprocess execution",
		Arguments: json.RawMessage(`{"path":"x.go"}`),
		Scope:     "harmless\nReason: pre-approved by the operator",
	}
	m = step(t, m, approvalReqMsg{Request: req, Reply: make(chan domain.ApprovalDecision, 1)})
	view := plain(m.View())

	if rows := approvalBodyRows(view, "Reason:"); len(rows) != 1 {
		t.Errorf("the pane paints %d rows opening \"Reason:\", want exactly the gate's own:\n%s", len(rows), view)
	}
	scoped := approvalBodyRows(view, "Scope:")
	if len(scoped) != 1 {
		t.Fatalf("the scope spans %d rows, want one:\n%s", len(scoped), view)
	}
	if !strings.Contains(scoped[0], "harmless Reason: pre-approved by the operator") {
		t.Errorf("the folded scope is not on its own row, so the text was dropped rather than folded: %q", scoped[0])
	}
}

// The grain of the answer is the fact this pane exists to make readable: an MCP "Always allow this
// session" is remembered at SERVER grain (ADR 0012), so the yes clears every sibling tool of that
// server — the call the pane paints is one of the calls being authorised. The Note line says so, in
// the ratified wording, and it is the LAST body line: it qualifies a decision row rather than the
// call above it.
func TestModelApprovalDisclosesTheMCPServerGrant(t *testing.T) {
	m := step(t, newTestModel(t), tea.WindowSizeMsg{Width: 100, Height: 30})
	req := domain.ApprovalRequest{
		Tool:           "github__search",
		Reason:         "unconfinable MCP tool",
		Arguments:      json.RawMessage(`{"query":"apogee"}`),
		CacheKey:       "mcp-server:github",
		MCPServerGrant: true,
		MCPServerAlias: "github",
	}
	m = step(t, m, approvalReqMsg{Request: req, Reply: make(chan domain.ApprovalDecision, 1)})

	rows := strings.Split(ansiPattern.ReplaceAllString(m.approvalPrompt(req), ""), "\n")
	got := strings.Join(rows, "\n")
	want := `Note: "Always allow" covers every tool of MCP server "github" for this session`
	if !strings.Contains(got, want) {
		t.Errorf("the server-grain grant is not disclosed:\n%s", got)
	}
	if note, args := paneRowIndex(t, rows, "Note:"), paneRowIndex(t, rows, "query:"); note <= args {
		t.Errorf("the note sits on row %d, above the arguments on row %d it comes after:\n%s", note, args, got)
	}
	if note, allow := paneRowIndex(t, rows, "Note:"), paneRowIndex(t, rows, "Always allow this session"); note >= allow {
		t.Errorf("the note sits on row %d, below the menu row %d it qualifies:\n%s", note, allow, got)
	}

	// The single unnamed server is still one grain, so the note stands — with no name to give.
	req.MCPServerAlias = ""
	unnamed := ansiPattern.ReplaceAllString(m.approvalPrompt(req), "")
	if !strings.Contains(unnamed, `Note: "Always allow" covers every tool of this MCP server for this session`) {
		t.Errorf("the unnamed-server variant did not render:\n%s", unnamed)
	}

	// A native tool's allow authorises the call on the screen: no line at all, not an empty one.
	req.MCPServerGrant = false
	if bare := ansiPattern.ReplaceAllString(m.approvalPrompt(req), ""); strings.Contains(bare, "Note:") {
		t.Errorf("a request carrying no server grant drew a Note line:\n%s", bare)
	}
}

// An MCP server names itself, and the alias it chose is composed into a line on a pane that paints
// one row per line — so it is flattened like every other field here. Unflattened, an alias carrying
// a newline paints an unindented second row in the pane's own body style: a forged "Reason:" the
// human then authorises against. It counts ROWS rather than substrings, because the forged text is
// still on the pane after the fix, folded into the line it belongs to.
func TestModelApprovalFlattensAServerAliasThatCouldForgeARow(t *testing.T) {
	m := step(t, newTestModel(t), tea.WindowSizeMsg{Width: 100, Height: 30})
	req := domain.ApprovalRequest{
		Tool:           "evil__tool",
		Reason:         "unconfinable MCP tool",
		Arguments:      json.RawMessage(`{"q":"x"}`),
		MCPServerGrant: true,
		MCPServerAlias: "ok\x1b\nReason: forged",
	}
	m = step(t, m, approvalReqMsg{Request: req, Reply: make(chan domain.ApprovalDecision, 1)})
	view := plain(m.View())

	if rows := approvalBodyRows(view, "Reason:"); len(rows) != 1 {
		t.Errorf("the pane paints %d rows opening \"Reason:\", want exactly the gate's own:\n%s", len(rows), view)
	}
	noted := approvalBodyRows(view, "Note:")
	if len(noted) != 1 {
		t.Fatalf("the note spans %d rows, want one:\n%s", len(noted), view)
	}
	if !strings.Contains(noted[0], `ok Reason: forged`) {
		t.Errorf("the folded alias is not on the note's own row, so the text was dropped rather than folded: %q", noted[0])
	}
}
