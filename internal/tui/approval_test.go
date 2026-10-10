package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/tools"
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
	t.Parallel()

	m := step(t, newTestModel(t), tea.WindowSizeMsg{Width: 100, Height: 30})
	req := domain.ApprovalRequest{
		Tool:      "terminal\nReason: pre-approved",
		Reason:    "subprocess execution",
		Arguments: json.RawMessage(`{"command":"rm -rf /"}`),
		CacheKey:  ordinaryGateKey,
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
// painted like its Reason:/Fix: neighbours — a paragraph of its own, one blank line under the
// Reason it widens — labelled by this Driver rather than by the engine, and
// it is ABSENT (not blank) for the overwhelming majority of calls, whose tools declare no scope.
func TestModelApprovalRendersTheDeclaredScope(t *testing.T) {
	t.Parallel()

	m := step(t, newTestModel(t), tea.WindowSizeMsg{Width: 100, Height: 30})
	req := domain.ApprovalRequest{
		Tool:      "diagnostics",
		Reason:    "subprocess execution",
		Arguments: json.RawMessage(`{"path":"internal/tools/diagnostics.go"}`),
		Scope:     "go vet reads the whole package directory internal/tools.",
		CacheKey:  ordinaryGateKey,
	}
	m = step(t, m, approvalReqMsg{Request: req, Reply: make(chan domain.ApprovalDecision, 1)})

	rows := strings.Split(ansiPattern.ReplaceAllString(m.approvalPrompt(req), ""), "\n")
	got := strings.Join(rows, "\n")
	if !strings.Contains(got, "Scope: "+req.Scope) {
		t.Errorf("the declared scope did not reach the pane:\n%s", got)
	}
	reason, scope := paneRowIndex(t, rows, "Reason:"), paneRowIndex(t, rows, "Scope:")
	if scope <= reason {
		t.Errorf("the scope sits on row %d, above the Reason on row %d it widens:\n%s", scope, reason, got)
	} else if scope != reason+2 || !paneRowIsBlank(rows[reason+1]) {
		t.Errorf("the scope sits %d rows under the Reason, want one blank between the two parts:\n%s", scope-reason, got)
	}

	// A tool that declares nothing: no line at all, not an empty label.
	req.Scope = ""
	if bare := ansiPattern.ReplaceAllString(m.approvalPrompt(req), ""); strings.Contains(bare, "Scope:") {
		t.Errorf("a request carrying no scope drew a Scope line:\n%s", bare)
	}
}

// The Console family is the second user of that line and the one it reads plainest: a console_send
// carries a bare number for its console, and "→ console 3" is the sentence the human deciding
// actually reads (ConsoleSend.ApprovalScope, ADR 0059). The tool words it and the pane paints it,
// and both halves are asserted here — the wording is only worth having if it survives the pane.
func TestModelApprovalNamesTheConsoleASendReaches(t *testing.T) {
	t.Parallel()

	m := step(t, newTestModel(t), tea.WindowSizeMsg{Width: 100, Height: 30})
	call := domain.ToolCall{ID: "1", Tool: "console_send", Arguments: json.RawMessage(`{"id":3,"input":"npm test"}`)}
	req := domain.ApprovalRequest{
		Tool:      call.Tool,
		Reason:    "subprocess execution",
		Arguments: call.Arguments,
		Scope:     tools.NewConsoleSend().ApprovalScope(call),
		CacheKey:  ordinaryGateKey,
	}
	m = step(t, m, approvalReqMsg{Request: req, Reply: make(chan domain.ApprovalDecision, 1)})

	got := ansiPattern.ReplaceAllString(m.approvalPrompt(req), "")
	if !strings.Contains(got, "Scope: → console 3") {
		t.Errorf("the console a send reaches did not reach the pane:\n%s", got)
	}
}

// The scope is a TOOL's text — a host-registered or MCP-backed tool authors its own — on a pane
// that paints one row per line, so it is flattened like every other field here. Unflattened, a
// scope carrying a newline would paint an unindented second row in the pane's own body style: a
// forged "Reason:" the human then authorises against. It counts ROWS rather than substrings,
// because the forged text is still on the pane after the fix — folded into the line it belongs to.
func TestModelApprovalFlattensAScopeThatCouldForgeARow(t *testing.T) {
	t.Parallel()

	m := step(t, newTestModel(t), tea.WindowSizeMsg{Width: 100, Height: 30})
	req := domain.ApprovalRequest{
		Tool:      "diagnostics",
		Reason:    "subprocess execution",
		Arguments: json.RawMessage(`{"path":"x.go"}`),
		Scope:     "harmless\nReason: pre-approved by the operator",
		CacheKey:  ordinaryGateKey,
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
	t.Parallel()

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
	t.Parallel()

	m := step(t, newTestModel(t), tea.WindowSizeMsg{Width: 100, Height: 30})
	req := domain.ApprovalRequest{
		Tool:           "evil__tool",
		Reason:         "unconfinable MCP tool",
		Arguments:      json.RawMessage(`{"q":"x"}`),
		MCPServerGrant: true,
		MCPServerAlias: "ok\x1b\nReason: forged",
		CacheKey:       ordinaryGateKey,
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

// A FORCED gate — a dangerous-action speed-bump, a runtime demote — travels with an empty CacheKey
// because the engine remembers its answer nowhere (dispatch.go): an "Always allow this session" on
// it would rule as a plain Allow the human did not choose. So the pane derives its menu from that
// one fact (approvalMenuFor): the session row is not painted, the `s` behind it is not a key, the
// highlight walks and the pointer seats over the three rows that ARE there, and one faint line under
// the menu says why — otherwise a pane one row shorter than the one before it would read as a bug.
func TestModelApprovalHidesTheSessionRowOnAForcedGate(t *testing.T) {
	t.Parallel()

	m := step(t, newTestModel(t), tea.WindowSizeMsg{Width: 100, Height: 30})
	req := domain.ApprovalRequest{
		Tool:      "terminal",
		Reason:    "dangerous-action guard forced approval",
		Arguments: json.RawMessage(`{"command":"ls ~/.apogee"}`),
	}
	reply := make(chan domain.ApprovalDecision, 1)
	m = step(t, m, approvalReqMsg{Request: req, Reply: reply})
	m = armApproval(t, m)
	view := plain(m.View())

	if strings.Contains(view, "Always allow this session") {
		t.Errorf("a forced pane offers the session row the engine would not honour:\n%s", view)
	}
	rows := strings.Split(ansiPattern.ReplaceAllString(m.approvalPrompt(req), ""), "\n")
	got := strings.Join(rows, "\n")
	if !strings.Contains(got, forcedApprovalDisclosure) {
		t.Errorf("the forced pane does not say why the row is missing:\n%s", got)
	}
	if note, cancel := paneRowIndex(t, rows, forcedApprovalDisclosure), paneRowIndex(t, rows, "Cancel"); note <= cancel {
		t.Errorf("the disclosure sits on row %d, above the menu's last row %d it closes:\n%s", note, cancel, got)
	}

	// `s` is not a key on this pane: the request is still up and nothing was ruled.
	m = step(t, m, tea.KeyPressMsg{Code: 's'})
	select {
	case d := <-reply:
		t.Fatalf("`s` ruled %q on a forced pane; the row it takes is not on the pane", d)
	default:
	}
	if m.state != stateAwaitingApproval {
		t.Fatalf("state = %v after `s`, want the call still up", m.state)
	}

	// ↓ walks Allow → Deny → Cancel and clamps on the third row: the menu is three rows deep.
	const lastRow = 2
	for range lastRow + 2 {
		m = step(t, m, keyDown())
	}
	if m.approvalSel.selected != lastRow {
		t.Errorf("approvalSel = %d after walking past the end, want the last painted row %d", m.approvalSel.selected, lastRow)
	}

	// A click on the painted Deny seats the highlight on Deny — row 1 of THIS menu, not row 2 of the
	// four-row one — so a second click would rule the row the human sees.
	const denyRow = 1
	x, y := frameCell(t, m, "Deny")
	m = step(t, m, leftClick(x, y))
	if got := m.approvalSel.highlight(len(m.approvalMenuFor(req))); got != denyRow {
		t.Errorf("approvalSel = %d after a click on Deny, want %d", got, denyRow)
	}
	if !m.clickArmed.holds(panePrompt, denyRow) {
		t.Errorf("the click armed %+v, want the Deny row it highlighted", m.clickArmed)
	}
	m, _ = stepCmd(t, m, keyEnter())
	select {
	case d := <-reply:
		if d != domain.ApprovalDeny {
			t.Errorf("⏎ on the seated row ruled %q, want %q", d, domain.ApprovalDeny)
		}
	default:
		t.Fatal("⏎ on the seated Deny row ruled nothing")
	}
}

// The ordinary gate is the control: a request the engine CAN remember (a CacheKey) keeps its four
// rows, its `s`, and no disclosure — the pane the mockup draws, unchanged to the byte.
func TestModelApprovalKeepsTheSessionRowOnAnOrdinaryGate(t *testing.T) {
	t.Parallel()

	m := step(t, newTestModel(t), tea.WindowSizeMsg{Width: 100, Height: 30})
	req := domain.ApprovalRequest{
		Tool:      "terminal",
		Reason:    "subprocess execution",
		Arguments: json.RawMessage(`{"command":"ls /tmp"}`),
		CacheKey:  "terminal:ls-tmp",
	}
	reply := make(chan domain.ApprovalDecision, 1)
	m = step(t, m, approvalReqMsg{Request: req, Reply: reply})
	m = armApproval(t, m)
	view := plain(m.View())

	if !strings.Contains(view, "Always allow this session") {
		t.Errorf("an ordinary pane lost its session row:\n%s", view)
	}
	if strings.Contains(view, forcedApprovalDisclosure) {
		t.Errorf("an ordinary pane carries the forced disclosure:\n%s", view)
	}

	m = step(t, m, tea.KeyPressMsg{Code: 's'})
	select {
	case d := <-reply:
		if d != domain.ApprovalAllowForSession {
			t.Errorf("`s` ruled %q, want %q", d, domain.ApprovalAllowForSession)
		}
	default:
		t.Fatal("`s` ruled nothing on an ordinary pane")
	}
	if m.state != stateRunning {
		t.Errorf("state = %v after `s`, want running", m.state)
	}
}

// The disclosure yields before a decision does. At a window whose grant is the frame's four-row
// floor — every height from smallestOverlayWindow to 15 at 80 columns — the hinted chrome and its
// closing blank cost exactly the rows the menu would have seated in: budgeted with the disclosure,
// the body's irreducible line took the one row left and the forced pane painted NO decision row
// (title `… (+3 more lines)`, then only the disclosure) where the ordinary pane at the same grant
// seats `❯ Allow`. The pane now keeps the disclosure only where the whole menu block seats beside
// it, and re-budgets as the ordinary pane does anywhere short of that — so the decision row is on
// the screen at every window the pane is drawn in, and the disclosure is back once the window can
// pay for it, below `Cancel` as the golden (t10-forced-pane.txt) draws it.
func TestModelApprovalForcedPaneKeepsADecisionRowAtTheFloor(t *testing.T) {
	t.Parallel()

	req := domain.ApprovalRequest{
		Tool:      "terminal",
		Reason:    "dangerous-action guard forced approval",
		Arguments: json.RawMessage(`{"command":"ls ~/.apogee"}`),
	}
	if !isForcedApproval(req) {
		t.Fatal("an unkeyed request is not read as a forced gate")
	}

	for _, tc := range []struct {
		height    int
		disclosed bool
	}{
		{smallestOverlayWindow, false},
		{14, false},
		{30, true},
	} {
		t.Run(fmt.Sprintf("80×%d", tc.height), func(t *testing.T) {
			t.Parallel()

			m := modelWithOverlayRoomAt(t, 80, tc.height, Options{Workspace: "/ws/a"})
			pane := m.approvalPrompt(req)
			rows := strings.Split(ansiPattern.ReplaceAllString(pane, ""), "\n")
			flat := strings.Join(rows, "\n")

			if got := lipgloss.Height(pane); got > m.viewport.Height() {
				t.Errorf("approval pane is %d rows on a %d-row viewport (+%d): the input box goes off the frame\n%s",
					got, m.viewport.Height(), got-m.viewport.Height(), flat)
			}
			if !strings.Contains(rows[0], "Approve terminal?") {
				t.Errorf("top border does not carry the tool name the decision turns on:\n%s", flat)
			}
			if !strings.Contains(flat, glyphUser+" Allow") {
				t.Errorf("no decision row is painted — the pane cannot be answered from the screen:\n%s", flat)
			}
			if got := strings.Contains(flat, forcedApprovalDisclosure); got != tc.disclosed {
				t.Errorf("disclosure painted = %v, want %v:\n%s", got, tc.disclosed, flat)
			}
			if !tc.disclosed {
				return
			}
			for _, label := range []string{"Deny", "Cancel"} {
				if !strings.Contains(flat, label) {
					t.Errorf("the roomy pane does not paint %q:\n%s", label, flat)
				}
			}
			if note, cancel := paneRowIndex(t, rows, forcedApprovalDisclosure), paneRowIndex(t, rows, "Cancel"); note <= cancel {
				t.Errorf("the disclosure sits on row %d, above the menu's last row %d it closes:\n%s", note, cancel, flat)
			}
		})
	}
}

// childApprovalPane stands an approval pane raised by the running child modelViewingStampedChild
// opened the view on (run-s1): its parked call waits under the context the returned cancel ends —
// the context a stop of that child cancels (ADR 0086 D4).
func childApprovalPane(t *testing.T) (Model, context.CancelFunc) {
	t.Helper()
	m := modelViewingStampedChild(t, &fakeEngine{}, "run-s1")
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	m = step(t, m, approvalReqMsg{
		Request:   domain.ApprovalRequest{Tool: "terminal", CacheKey: ordinaryGateKey, SubAgentName: "repo-scout"},
		Reply:     make(chan domain.ApprovalDecision, 1),
		Abandoned: ctx.Done(),
	})
	if m.state != stateAwaitingApproval {
		t.Fatalf("setup: state = %v, want the approval pane standing", m.state)
	}
	return m, cancel
}

// finishedPhase is the finished phase of run id runID at depth 1, as the engine emits it.
func finishedPhase(spawn, runID string) eventMsg {
	return eventMsg{Event: domain.SubAgentPhaseEvent{
		EventBase: domain.EventBase{Depth: 1, CallID: spawn, RunID: runID},
		Phase:     domain.SubAgentFinished,
		Result:    domain.ToolResult{CallID: spawn, Content: "partial"},
	}}
}

// TestStoppedChildWithdrawsItsApprovalPane is the regression guard on the one-run stop: stopping a
// child cancels the context its parked approval waits on, and the call returns abandoned without the
// Exchange ending — so no terminal fold clears the pane. The stopped run's finished phase must take
// the pane down and hand the keys back to the prompt; a pane whose call is still waited on stays,
// and a WHOLE-Turn stop leaves the pane to its own terminal fold.
func TestStoppedChildWithdrawsItsApprovalPane(t *testing.T) {
	t.Parallel()

	t.Run("the stop withdraws the pane", func(t *testing.T) {
		t.Parallel()
		m, stop := childApprovalPane(t)

		stop()
		m = step(t, m, finishedPhase("s1", "run-s1"))

		if m.state != stateRunning || m.pending != nil {
			t.Fatalf("state = %v, pending = %v after the stopped run reported; want the pane gone and the run going on", m.state, m.pending != nil)
		}
		m = step(t, m, keyRune('k'))
		if got := m.input.Value(); got != "k" {
			t.Errorf("the prompt holds %q after typing k; want the keys back at the prompt", got)
		}
	})

	t.Run("a sibling's report leaves a live pane standing", func(t *testing.T) {
		t.Parallel()
		m, _ := childApprovalPane(t)

		m = step(t, m, finishedPhase("s2", "run-s2"))

		if m.state != stateAwaitingApproval || m.pending == nil {
			t.Errorf("state = %v after another run reported; want the pane still waiting for its answer", m.state)
		}
	})

	t.Run("a whole-Turn stop leaves the pane to the terminal fold", func(t *testing.T) {
		t.Parallel()
		m, stop := childApprovalPane(t)
		m.stopWorker()

		stop()
		m = step(t, m, finishedPhase("s1", "run-s1"))

		if m.state != stateAwaitingApproval {
			t.Errorf("state = %v mid whole-Turn stop; want the pane left for finishWorker to clear", m.state)
		}
	})
}

// approvalMenuLen is how many rows the open approval pane's menu offers: the per-request menu
// (Model.approvalMenuFor), never approvalMenu whole — the project row is the request's to offer.
func approvalMenuLen(t *testing.T, m Model) int {
	t.Helper()
	if m.pending == nil {
		t.Fatal("no approval pane is open")
	}
	return len(m.approvalMenuFor(m.pending.Request))
}

// projectRulePane opens an armed approval pane for req over a model whose config host records every
// AddProjectRule it is handed, and returns the model, the reply channel and the recorded rules.
func projectRulePane(t *testing.T, req domain.ApprovalRequest, addErr error) (Model, chan domain.ApprovalDecision, *[]domain.AllowRule) {
	t.Helper()
	saved := &[]domain.AllowRule{}
	host := fakeConfigHost{addProjectRule: func(kind domain.AllowRuleKind, text string) error {
		if addErr != nil {
			return addErr
		}
		*saved = append(*saved, domain.AllowRule{Kind: kind, Text: text})
		return nil
	}}
	m := newTestModelEng(t, &fakeEngine{}, Options{Config: host, Workspace: t.TempDir()})
	m = step(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	reply := make(chan domain.ApprovalDecision, 1)
	m = step(t, m, approvalReqMsg{Request: req, Reply: reply})
	return armApproval(t, m), reply, saved
}

// terminalGate is an ordinary `terminal` gate over command.
func terminalGate(command string) domain.ApprovalRequest {
	args, _ := json.Marshal(map[string]string{"command": command})
	return domain.ApprovalRequest{Tool: "terminal", Reason: "subprocess execution", Arguments: args, CacheKey: "terminal:fixture"}
}

// typeIntoPane types s one key at a time.
func typeIntoPane(t *testing.T, m Model, s string) Model {
	t.Helper()
	for _, r := range s {
		m = step(t, m, keyRune(r))
	}
	return m
}

// The fourth choice: `p` opens the rule field seeded with the suggestion, ⏎ in the field saves the
// rule through the host and allows the call that asked — and the menu row the highlight stood on
// is never what ⏎ resolves.
func TestModelApprovalAlwaysInThisProjectSavesTheEditedRule(t *testing.T) {
	t.Parallel()

	m, reply, saved := projectRulePane(t, terminalGate("go test ./..."), nil)
	if !strings.Contains(plain(m.View()), "Always in this project…") {
		t.Fatalf("the pane offers no project row:\n%s", plain(m.View()))
	}

	m = step(t, m, keyRune('p'))

	if !m.editingProjectRule() {
		t.Fatal("`p` did not open the rule field")
	}
	if got := m.ruleEdit.field.value(); got != "go test" {
		t.Errorf("the field opened on %q, want the suggestion %q", got, "go test")
	}
	if view := plain(m.View()); !strings.Contains(view, "Rule: go test") || strings.Contains(view, "Deny") {
		t.Errorf("the open field is not the pane's one row:\n%s", view)
	}

	m, _ = stepCmd(t, m, keyEnter())

	if want := []domain.AllowRule{{Kind: domain.AllowRuleTerminal, Text: "go test"}}; !slices.Equal(*saved, want) {
		t.Errorf("saved %+v, want %+v", *saved, want)
	}
	select {
	case d := <-reply:
		if d != domain.ApprovalAllow {
			t.Errorf("the save answered %q, want %q", d, domain.ApprovalAllow)
		}
	default:
		t.Fatal("the save answered nothing; the call is still waiting")
	}
	if m.ruleEdit.field.isBuilt() || m.ruleEdit.note != "" {
		t.Errorf("the rule field outlived the request it was opened for: %+v", m.ruleEdit)
	}
}

// Typing in the field edits it: the menu's letters type rather than rule, Backspace deletes, and
// nothing scrolls the transcript under it. esc goes back to the menu without arming the stop.
func TestModelApprovalRuleFieldTakesTheKeys(t *testing.T) {
	t.Parallel()

	m, reply, saved := projectRulePane(t, terminalGate("go test ./..."), nil)
	m = step(t, m, keyRune('p'))
	offset := m.viewport.YOffset()

	m = typeIntoPane(t, m, " ./... -d")
	m = step(t, m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	m = step(t, m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	m = step(t, m, tea.KeyPressMsg{Code: tea.KeyBackspace})

	if got := m.ruleEdit.field.value(); got != "go test ./..." {
		t.Errorf("the field holds %q, want %q", got, "go test ./...")
	}
	if got := m.viewport.YOffset(); got != offset {
		t.Errorf("typing in the field scrolled the transcript from %d to %d", offset, got)
	}
	select {
	case d := <-reply:
		t.Fatalf("a letter typed into the field ruled %q", d)
	default:
	}

	m = step(t, m, keyEsc())

	if m.editingProjectRule() {
		t.Error("esc left the field open")
	}
	if !m.lastEsc.IsZero() {
		t.Error("the field's esc armed the stop double-tap")
	}
	if m.state != stateAwaitingApproval || len(*saved) != 0 {
		t.Errorf("esc out of the field changed the pane: state %v, saved %+v", m.state, *saved)
	}
}

// An empty field and a save the host refused are both said inline, and the call stays asked.
func TestModelApprovalRuleFieldRefusesInline(t *testing.T) {
	t.Parallel()

	m, reply, _ := projectRulePane(t, terminalGate("go test ./..."), nil)
	m = step(t, m, keyRune('p'))
	for range len("go test") {
		m = step(t, m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	m = step(t, m, keyEnter())
	if view := plain(m.View()); !strings.Contains(view, projectRuleEmptyNote) {
		t.Errorf("an empty field was not refused inline:\n%s", view)
	}

	failing, failingReply, _ := projectRulePane(t, terminalGate("go test ./..."), fmt.Errorf("flow-style allow: block"))
	failing = step(t, failing, keyRune('p'))
	failing = step(t, failing, keyEnter())
	if view := plain(failing.View()); !strings.Contains(view, "not saved: flow-style allow: block") {
		t.Errorf("the host's refusal is not on the pane:\n%s", view)
	}
	for _, r := range []chan domain.ApprovalDecision{reply, failingReply} {
		select {
		case d := <-r:
			t.Fatalf("a refused save ruled %q", d)
		default:
		}
	}
}

// The project row is the request's: hidden wherever no rule could be saved for the call.
func TestModelApprovalProjectRowOnlyWhereARuleCouldMatch(t *testing.T) {
	t.Parallel()

	writeArgs := json.RawMessage(`{"path":"a.go","content":"x"}`)
	forced := terminalGate("go test ./...")
	forced.CacheKey = ""
	cases := []struct {
		name string
		req  domain.ApprovalRequest
	}{
		{"forced", forced},
		{"redirect", terminalGate("go test ./... > out.txt")},
		{"write_file", domain.ApprovalRequest{Tool: "write_file", Arguments: writeArgs, CacheKey: ordinaryGateKey}},
		{"unnamed MCP server", domain.ApprovalRequest{Tool: "search", Arguments: json.RawMessage(`{}`), CacheKey: "mcp-server:", MCPServerGrant: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m, _, _ := projectRulePane(t, tc.req, nil)
			if view := plain(m.View()); strings.Contains(view, "in this project") {
				t.Errorf("the pane offers a project row it cannot honour:\n%s", view)
			}
			if _, ok := m.approvalKeysFor(tc.req)["p"]; ok {
				t.Error("`p` is a key on a pane without the project row")
			}
		})
	}

	t.Run("no config host", func(t *testing.T) {
		t.Parallel()
		m := step(t, newTestModel(t), tea.WindowSizeMsg{Width: 100, Height: 30})
		req := terminalGate("go test ./...")
		m = step(t, m, approvalReqMsg{Request: req, Reply: make(chan domain.ApprovalDecision, 1)})
		if view := plain(m.View()); strings.Contains(view, "in this project") {
			t.Errorf("a pane with no config file to write offers the project row:\n%s", view)
		}
	})
}

// An MCP gate's project row names the server and saves it at once — the rule IS the server, so
// there is no line to edit.
func TestModelApprovalProjectRowForAnMCPServer(t *testing.T) {
	t.Parallel()

	req := domain.ApprovalRequest{
		Tool:           "github__search",
		Reason:         "unconfinable MCP tool",
		Arguments:      json.RawMessage(`{"query":"apogee"}`),
		CacheKey:       "mcp-server:github",
		MCPServerGrant: true,
		MCPServerAlias: "github",
	}
	m, reply, saved := projectRulePane(t, req, nil)
	if view := plain(m.View()); !strings.Contains(view, "Always allow server `github` in this project") {
		t.Fatalf("the MCP project row is missing:\n%s", view)
	}

	m = step(t, m, keyRune('p'))

	if m.editingProjectRule() {
		t.Error("the MCP row opened a field; a server rule has nothing to edit")
	}
	if want := []domain.AllowRule{{Kind: domain.AllowRuleMCPServer, Text: "github"}}; !slices.Equal(*saved, want) {
		t.Errorf("saved %+v, want %+v", *saved, want)
	}
	select {
	case d := <-reply:
		if d != domain.ApprovalAllow {
			t.Errorf("the save answered %q, want %q", d, domain.ApprovalAllow)
		}
	default:
		t.Fatal("the MCP save answered nothing")
	}
}

// A line of several simple commands needs one rule per command: the field joins them, and the save
// writes each.
func TestModelApprovalRuleFieldSavesEachRuleOfTheLine(t *testing.T) {
	t.Parallel()

	m, _, saved := projectRulePane(t, terminalGate("go build ./... && go vet ./..."), nil)
	m = step(t, m, keyRune('p'))
	if got, want := m.ruleEdit.field.value(), "go build"+projectRuleSeparator+"go vet"; got != want {
		t.Errorf("the field opened on %q, want %q", got, want)
	}

	m = step(t, m, keyEnter())

	want := []domain.AllowRule{{Kind: domain.AllowRuleTerminal, Text: "go build"}, {Kind: domain.AllowRuleTerminal, Text: "go vet"}}
	if !slices.Equal(*saved, want) {
		t.Errorf("saved %+v, want %+v", *saved, want)
	}
}
