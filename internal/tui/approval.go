package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/domain"
)

// approvalOption is one row of the approval menu: the label the human reads, the key that takes it
// without navigating there first, and what taking it does. A row either sends a decision back over
// the rendezvous or CANCELS the run — cancels is what tells the two apart, because no fourth
// ApprovalDecision was added for it (the ratified design leaves the engine's Approver contract
// untouched). The Cancel row IS the Esc key written down: the same stopWorker path, spelled out for
// a human who has no legend left to learn it from.
type approvalOption struct {
	label string
	key   string // the keypress that takes this row directly; also the text of its shortcut cell
	// decision is what the row replies, meaningless when cancels is set.
	decision domain.ApprovalDecision
	cancels  bool
}

// approvalMenu is the approval prompt's decision menu in the order it is painted
// (docs/layout/user-questions-layout.md). It is the ONE list the pane and the keys both read — the
// rows renderPopup draws, the shortcut cells beside them, the order ↑/↓ walk, and (through
// approvalKeys) the letters that take a row without walking to it — so a row can never be paintable
// and unreachable, or reachable and unpainted.
var approvalMenu = []approvalOption{
	{label: "Allow", key: "a", decision: domain.ApprovalAllow},
	{label: "Always allow this session", key: "s", decision: domain.ApprovalAllowForSession},
	{label: "Deny", key: "d", decision: domain.ApprovalDeny},
	{label: "Cancel", key: "esc", cancels: true},
}

// approvalKeys maps a decision keypress to the ApprovalDecision it sends: the menu's own rows,
// indexed by the letter each one advertises in its shortcut cell, so the two can never drift. The
// Cancel row is absent by construction — it sends no decision, and Esc is claimed before the
// approval routing is reached (handleKey's "esc" case) precisely so the cancel path is one path.
var approvalKeys = approvalMenuKeys()

func approvalMenuKeys() map[string]domain.ApprovalDecision {
	keys := make(map[string]domain.ApprovalDecision, len(approvalMenu))
	for _, opt := range approvalMenu {
		if !opt.cancels {
			keys[opt.key] = opt.decision
		}
	}
	return keys
}

// handleApprovalKey resolves a pending Approval while awaitingApproval. A decision key sends
// its verdict back over the rendezvous reply channel (sendApproval); ↑/↓ move the menu's highlight,
// clamped and non-wrapping, the way the ask prompt's choice arrows move (D5), leaving ⏎ to take
// what they point at (resolveApproval). Any other key scrolls the transcript so the human can
// review context before ruling — the prompt stays soft-modal, with PgUp/PgDn intercepted upstream
// so the transcript is still reachable now that ↑/↓ belong to the menu. The decision's transcript
// record arrives for free as the loop's observational ApprovalEvent (C3; P2.3), so this renders the
// prompt's resolution, not the record.
func (m Model) handleApprovalKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if decision, ok := approvalKeys[msg.String()]; ok && m.pending != nil {
		return m.sendApproval(decision)
	}
	switch msg.String() {
	case "up":
		m.approvalSel = clampInt(m.approvalSel-1, 0, len(approvalMenu)-1)
		return m, nil
	case "down":
		m.approvalSel = clampInt(m.approvalSel+1, 0, len(approvalMenu)-1)
		return m, nil
	}
	return m.scrollViewport(msg)
}

// resolveApproval takes the menu row approvalSel points at — what ⏎ means on this pane. A decision
// row replies like its letter would; the Cancel row takes the Esc path instead, cancelling the
// in-flight worker and leaving the prompt standing until the worker reports back with its
// cancelledMsg (finishWorker clears it), exactly as Esc has always behaved here.
func (m Model) resolveApproval() (tea.Model, tea.Cmd) {
	if m.pending == nil {
		return m, nil
	}
	opt := approvalMenu[clampInt(m.approvalSel, 0, len(approvalMenu)-1)]
	if opt.cancels {
		m.stopWorker()
		return m, nil
	}
	return m.sendApproval(opt.decision)
}

// sendApproval hands one verdict back over the pending request's rendezvous reply channel (buffered
// cap 1, so the send never blocks — messages.go) and returns the model to running so the worker's
// blocked Step resumes; the spinner tick is re-armed because the chain died when the prompt went up.
func (m Model) sendApproval(decision domain.ApprovalDecision) (tea.Model, tea.Cmd) {
	m.pending.Reply <- decision
	m.pending = nil
	m.state = stateRunning
	m.layout() // the pane is gone: a draft the prompt had clamped grows back (draftRowsCeiling)
	return m, m.spin.arm()
}

// approvalPrompt renders the pending tool call the human must rule on as a bordered popup pane
// above the input box (the shared popup module; D7/D8): the title carries the RAW tool name
// verbatim (not the friendly transcript label — the approval flow is a security surface, so the
// human sees exactly the tool that will run), the body carries the asking sub-agent's task when a
// child raised the call, then a non-empty Reason, then the Fix: line for the few gates whose cause
// the user can lift (ApprovalRequest.Remedy — the confinement-unavailable pair), then the arguments
// (approvalArgsBlock), and the decisions themselves are the pane's ROWS.
//
// The Sub-agent line leads the body because it answers a question the rest of the pane cannot: with
// several children running at once their prompts QUEUE, one at a time, in an order nothing on the
// screen predicts (ADR 0039), so the tool name and the arguments no longer say whose work this is.
// It is absent at depth 0, where the top-level agent is the only thing that could be asking, so an
// undelegated session's pane is unchanged to the byte.
//
// It is a MENU rather than a legend (docs/layout/user-questions-layout.md): the title rides the top
// border, the four options of approvalMenu are menu-style rows with their shortcut letters aligned
// in a second column, and the hint row that used to spell "a allow · d deny · …" is gone — the
// letters are now written beside the options they take, where the eye already is. That is what pays
// for the rows: the title row and the hint row the pane no longer draws are two rows back, so its
// chrome is its two borders and nothing else (popupBorderChrome) and the menu costs the frame no
// more than the legend did.
//
// The mockup's vertical spacing is the same argument in blank lines: the Reason: line and the
// labelled arguments under it run ADJACENT — they are labelled facts about one call, and a blank
// line between them reads as two blocks — while ONE blank line sets the menu off from them
// (popupSpec.rowPadAbove), because that is
// the break that matters, between what the human is deciding about and what the decisions are. It is
// the ONLY blank the pane spends: the menu's four options are adjacent to each other and the last of
// them ends the box, so nothing separates Cancel from the bottom border (rowPadBelow stays off, and
// the mockup draws it that way). The blank is booked out of the pane's own row budget below, and it
// gives way before an option does on a window too short for both.
//
// Every model-authored string (tool name, reason, args) is escape-stripped at this call site;
// stripEscapes drops the C0 control characters and DEL (keeping \n and \t), drops the bidi
// formatting characters with them, and passes every printable rune through, so the tool name the
// human is deciding about arrives with its printable text intact and loses only what the terminal
// would have OBEYED rather than shown — the ESC that opens a sequence, the CR that could rewind the
// name's own row, the U+202E that would draw this row's glyphs in an order the executor never sees. That is a claim about the
// TERMINAL, not about the name: a name written entirely in printable characters, lookalikes and
// all, reaches this pane exactly as the model wrote it. The menu's own rows are ours, not the
// model's, so nothing there needs stripping. Empty/null arguments add no body. There is ONE pane
// for every depth: a child's request is rendered by this same code, told apart by its Sub-agent
// line rather than by a nested surface of its own.
//
// The newline stripEscapes keeps is then taken off the pane's FIELDS, and only its fields
// (flattenField): the title, the Sub-agent line, the Reason and the Fix each compose a label with a
// string this pane did not write, and the body is painted one row per line, so a field carrying "\n"
// paints rows of its own — a second "Reason:" above the real one, in the same th.popupBody style,
// because this pane sets no bodyLead and has no styling that tells its own rows from a forged one.
// The same pass takes the newline off each argument's NAME (argumentDetails). What stays multi-line
// is every argument VALUE: those line breaks are the fact the human is ruling on — the four lines a
// command will really run — and they sit INDENTED under a label that can no longer be forged, which
// is what makes keeping them safe. Flatten the values instead and the pane would be lying about
// what executes; flatten nothing and the pane cannot promise the row it draws is a row it wrote.
//
// The guarantee this security surface holds is NOT that the whole reason is always on the screen —
// no pane can promise that on a terminal with four rows to give. It is that the human is never
// asked to decide against text the pane hid WITHOUT SAYING SO: the module word-wraps the reason
// rather than clipping it, and whatever it cannot seat it counts out in the "… (+N more lines)"
// marker, on the body block's own last row. On THIS pane that row is always there, and the count
// never has to fall back onto the pane's name the way a titled pane's does (popupTitleLine). The
// chrome above is why: the frame's per-pane floor is four rows (frameRowPlan), the two borders are
// the whole of what this pane spends out of them, and popupBudget hands the rows at most one line
// less than what is left — so a seated approval prompt has ONE body line and ONE decision row at
// every window it is drawn in. At that floor the body line IS the marker, every line of the reason
// and the arguments dropped and the pane stating how many
// (TestModelApprovalNamesTheProseItCannotShow pins the placement, reading it off the row directly
// under the border). On a pane too narrow to seat the full phrase the count sheds its noun rather
// than being clipped off the end (popupElisionMarkerFitting), because the terminal that is short is
// usually the one that is narrow. So the border always carries the identity the decision turns on,
// the row beneath it says when there is more to read than the pane is showing, and a decision the
// human can act on is on the screen at every height.
//
// "Always" is total because the PANE is: the frame seats it last of all the surfaces and the input
// box's draft rows give way to it (draftRowsCeiling), so there is no window a pane can be drawn in
// where a/d/s are live and this pane is not on the frame. The "" below is the sub-twelve-row case,
// where the frame draws no pane at all.
func (m Model) approvalPrompt(req domain.ApprovalRequest) string {
	var parts []string
	if line := subAgentPromptLine(req.SubAgentName, req.SubAgentTask); line != "" {
		parts = append(parts, line)
	}
	if req.Reason != "" {
		parts = append(parts, "Reason: "+flattenField(stripEscapes(req.Reason)))
	}
	// The way out of the condition the Reason just named, on the line under it — a part of its own
	// rather than a tail on the Reason, so the pane's wrapping, elision and row budgeting treat it
	// as the prose it is. Most gates carry none (their cause is the mode the user chose), and those
	// panes draw exactly as before.
	if req.Remedy != "" {
		parts = append(parts, "Fix: "+flattenField(stripEscapes(req.Remedy)))
	}
	if args := approvalArgsBlock(req); args != "" {
		parts = append(parts, args)
	}
	// Where the call's path really points, when that is not where the argument says
	// (ApprovalRequest.ResolvedPath). It comes LAST, under the arguments it is about, because it
	// is a statement about one of them and reads as a footnote to the block rather than as a
	// fact of its own; and it is a part of its own so the pane's wrapping, elision and row
	// budgeting treat it as the line it is. An ordinary call sends nothing and this pane is
	// unchanged to the byte.
	if note := resolvedPathNote(req.ResolvedPath); note != "" {
		parts = append(parts, note)
	}

	rows := make([]popupRow, len(approvalMenu))
	for i, opt := range approvalMenu {
		// Two cells, so the module's column layout stacks the shortcuts into their own right-hand
		// column whatever the labels measure — the mockup's alignment, derived rather than hand-padded.
		rows[i] = popupRow{opt.label, "[" + opt.key + "]"}
	}

	// The whole menu or as much of it as the window can seat; no cap of our own, because four rows is
	// the whole offering rather than a window onto a longer list. The demand is in LINES, which for
	// this pane is one per option — the labels are ours and they do not wrap — plus the blank line the
	// menu is set off by (popupSpec.rowPadAbove): a pane that asked for four and painted five would
	// overflow by exactly the room it did not book.
	//
	// The ZERO floor (popupFloor) is right here and not an oversight beside the ask prompt's: this
	// menu's demand is its own four options and nothing longer, so the rows can never eat a window the
	// body had room in — past five lines every further row of the grant is the reason's. The pane the
	// floor exists for is the one whose offering scales with what the model wrote.
	menuLines := popupRowBlockLines(popupFlatRowHeights(len(rows)), 0, popupRowPadLines(true, false))
	maxBodyRows, rowsShown, seated := m.popupBudget(panePrompt, menuLines, menuLines, popupBorderChrome, popupFloor{})
	if !seated {
		return "" // the frame cannot seat this pane beside its siblings (frameRowPlan)
	}
	spec := popupSpec{
		// The tool NAME is a field like any other on this pane, and apogee does not author it — an MCP
		// server names its own tools. So it is flattened (flattenField) before it is composed into the
		// title: a name carrying "\n" would otherwise break the title out of the border it is spliced
		// into and paint an unindented row of the model's choosing above the pane's own body.
		// popupTitleLine folds again as the backstop for every OTHER pane's title; this site is the one
		// that keeps the fold beside the pane's other field treatments, where the reason for it is.
		title:         "Approve " + flattenField(stripEscapes(req.Tool)) + "?",
		titleInBorder: true,
		body:          strings.Join(parts, "\n"), // Reason: and command: adjacent, as the mockup draws them
		maxBodyRows:   maxBodyRows,
		rows:          rows,
		menuRows:      true,
		rowPadAbove:   true, // the one blank line between the body and the menu; the mockup closes on the border
		selected:      clampInt(m.approvalSel, 0, len(rows)-1),
		maxRows:       rowsShown,
	}
	return renderPopup(m.th, spec, m.width)
}

// approvalTaskClipRunes bounds the delegated task the Sub-agent line spends body rows on. It is the
// one string on this pane that is CLIPPED rather than wrapped in full, and the reason is what the
// pane is for: the reason and the arguments are what the human is deciding ABOUT, while the task is
// only who is asking, so a model that delegated an essay must not be able to push the decision's own
// facts off the screen. Everything a clip drops is marked by the ellipsis clipRunes leaves, and the
// rest of the body keeps the pane's no-silent-loss guarantee unchanged (the "… (+N more lines)"
// marker). The bound is the transcript's detail bound for the same reason it was chosen there —
// enough for a sentence naming a task, not enough for a paragraph.
const approvalTaskClipRunes = detailClipRunes

// subAgentPromptLine is the identity line a prompt raised by a delegate leads with — "who is
// asking", in the one wording BOTH decision surfaces use (approvalPrompt, askPrompt), because two
// panes answering that question in two dialects would be a design that had stopped being one.
//
// A named delegation leads with its name and keeps the task behind it — "Sub-agent: repo-scout —
// audit the config loader" — rather than replacing one with the other: the name is what a human
// recognises the child by across a fan-out of queued prompts, and the task is still the sentence
// that says what they are being asked to authorise on its behalf. An unnamed one is byte-identical
// to the line this pane has always drawn, and depth 0 (no task) draws none at all, so an
// undelegated session's pane is untouched.
//
// The clip is spent on the WHOLE line, not on the task alone: approvalTaskClipRunes is the budget
// "who is asking" gets before it starts pushing the decision's own facts off the pane, and a name
// is part of who is asking. So a named line is never longer than an unnamed one, however long a
// name the model sent.
//
// Both halves are FLATTENED before the clip (flattenField), because both are the model's own bytes
// and this line LEADS the body: a task carrying "\n" painted every line after the first as a row of
// the pane's, above the Reason the human is deciding on, and the clip did not stop it — a rune
// budget bounds how much text there is, never how many rows it takes. Flattened first, the clip
// bounds the one row this line is.
func subAgentPromptLine(name, task string) string {
	if task == "" {
		return ""
	}
	who := flattenField(stripEscapes(task))
	if clean := flattenField(stripEscapes(name)); clean != "" {
		who = clean + " — " + who
	}
	return "Sub-agent: " + clipRunes(who, approvalTaskClipRunes)
}

// approvalArgsBlock renders req's arguments for the approval body, escape-stripped: one `name:`
// label line per argument with the value's own lines indented beneath it (argumentDetails), so a
// shell call reads
//
//	command:
//	  cd /ws/a && git status
//
// rather than as the JSON object carrying that string. The human is deciding about the ARGUMENTS,
// and a brace, a quoted key and a `\n` escape are the envelope they travelled in — three things to
// read past on a surface whose whole job is that the fact is read.
//
// Nothing is dropped to get there: every argument the request carries gets a label, so an extra one
// the reader would want to see — a workdir naming where a command runs — is on the screen rather
// than summarised away, and arguments that cannot be labelled at all still show as they arrived
// (argumentDetails). This is DISPLAY-ONLY in the strict sense: req.Arguments is what the tool
// receives whatever this returns, and the decision the human sends is unaffected by the shape it
// was read in.
//
// One tool-presentation vocabulary paints both surfaces: the block is built from the same
// [detailLine] values a transcript block is, in toolpresent.go, rather than from a second formatter
// living here.
func approvalArgsBlock(req domain.ApprovalRequest) string {
	details := argumentDetails(req.Arguments)
	lines := make([]string, len(details))
	for i, d := range details {
		lines[i] = d.Text
	}
	return stripEscapes(strings.Join(lines, "\n"))
}
