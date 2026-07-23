package tui

import (
	"strings"

	lipgloss "charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// ----------------------------------------------------------------------------
// The line-oriented transcript renderer (P2.7 — TUI presentation pass)
// ----------------------------------------------------------------------------
//
// The renderer turns the transcript into the flat slice of physical lines the viewport
// shows. It returns []string (not a joined string) for two reasons: tool results carry
// embedded newlines, so the caller feeds viewport.SetContentLines without re-splitting; and
// the sticky-to-top scroll needs to measure the wrapped height of the lines above the last
// user prompt, which it can only do over the exact lines the viewport stores. Every element
// is a single physical line (no embedded newline), so wrappedOffset's soft-wrap arithmetic
// stays in lockstep with the viewport's own calculateLine (TestWrappedOffsetMatchesViewport).
//
// The look mirrors layout.md: the last user prompt is a full-width white-on-dark-gray block;
// the assistant and tool headers lead with ✦; a tool header carries its label alone and the
// target leads a ┝/┕ tree branch beneath it, so a single call and a grouped run share one
// shape; one blank line separates every block. Sub-agent depth (Phase 3) indents a whole block by two
// columns per level — the tree-branch and depth-indent primitives are built here now so the
// P3.14 sub-agent renderer extends these seams rather than reworking them.

// userBlock is the line range a single user prompt occupies within the rendered lines: its
// first line index and its physical-line count. The sticky-header overlay treats each as a
// section header that freezes at the top of the viewport while its replies are on screen.
type userBlock struct{ start, count int }

// renderedTranscript is the renderer's output: the physical lines, the index of the last user
// prompt's first line (-1 when the transcript holds no user prompt), and the line range of
// every user block. The caller pins the last line to the top of the viewport unless the human
// has scrolled, and overlays the owning user block as a sticky header (model.go).
type renderedTranscript struct {
	lines         []string
	lastUserStart int
	userBlocks    []userBlock
}

// renderView renders the committed entries plus any in-progress assistant buffer into the
// viewport's lines, recording where the last user block begins. Blocks are separated by one
// blank line (layout.md).
func (t *transcript) renderView(th theme, width int) renderedTranscript {
	if width < 1 {
		width = 1
	}
	var lines []string
	var userBlocks []userBlock
	lastUserStart := -1

	appendBlock := func(isUser bool, block []string) {
		if len(lines) > 0 {
			lines = append(lines, "") // the single blank line between blocks
		}
		if isUser {
			lastUserStart = len(lines)
			userBlocks = append(userBlocks, userBlock{start: len(lines), count: len(block)})
		}
		lines = append(lines, block...)
	}

	prevDepth := 0
	for i := 0; i < len(t.entries); i++ {
		e := t.entries[i]
		// Open a ⤷ sub-agent label whenever a run descends to a deeper level than the
		// previous block — a 0→1 (or 1→2) transition announces the nested section once,
		// per level, until the stream climbs back out (P3.14).
		if e.depth > prevDepth {
			for d := prevDepth + 1; d <= e.depth; d++ {
				appendBlock(false, renderSubAgentLabel(th, d, width))
			}
		}
		// Consecutive same-label tool calls fold into one block at render time, so a batch of
		// reads is one header plus an aligned branch per file. The entry list is untouched: a
		// call that arrives mid-stream joins its group on the next repaint for free, and a run
		// is same-depth by construction, so the label logic above fires exactly as before.
		if run := toolCallRun(t.entries, i); len(run) > 1 {
			appendBlock(false, railLines(th, renderToolBlock(th, run, railedWidth(width, e.depth)), e.depth))
			i += len(run) - 1
		} else {
			appendBlock(e.kind == entryUser, renderEntryLines(th, e, width))
		}
		prevDepth = e.depth
	}
	if t.streaming {
		// The in-progress buffer is trimmed of its trailing blank lines for display only: the
		// buffer keeps them (a mid-stream "\n\n" may be a paragraph break about to be continued),
		// but the preview must not grow a wobbling gap above the footer. An empty buffer still
		// renders its lone marker line, so the human sees that streaming has begun.
		appendBlock(false, renderEntryLines(th, entry{kind: entryAssistant, text: trimTrailingBlankLines(t.pending)}, width))
	}
	return renderedTranscript{lines: lines, lastUserStart: lastUserStart, userBlocks: userBlocks}
}

// renderLines is the line slice alone — the viewport content and the substring-test surface.
func (t *transcript) renderLines(th theme, width int) []string {
	return t.renderView(th, width).lines
}

// renderEntryLines renders one committed entry into its physical lines, framed for its
// sub-agent depth. The user prompt is a full-width block; everything else hangs off a marker.
// A Depth > 0 entry is wrapped to the narrower column left of its rail gutter, then each line
// is prefixed by the rail (P3.14) so the nested block reads as a framed sub-section.
func renderEntryLines(th theme, e entry, width int) []string {
	inner := railedWidth(width, e.depth)
	switch e.kind {
	case entryUser:
		return railLines(th, renderUserBlock(th, e.text, e.skills, inner), e.depth)
	case entryAssistant:
		marker := glyphAssistant + " "
		body := renderMarkdownBody(th, e.text, inner-lipgloss.Width(marker))
		return railLines(th, withMarker(marker, body), e.depth)
	case entryToolCall:
		return railLines(th, renderToolBlock(th, []toolView{e.tool}, inner), e.depth)
	case entryToolResult:
		return railLines(th, renderOrphanResult(th, e.text, inner), e.depth)
	case entryError:
		return railLines(th, hangingWrap(th.errorText, glyphAssistant+" ", e.text, inner), e.depth)
	case entryNote:
		return railLines(th, hangingWrap(th.noteText, "· ", e.text, inner), e.depth)
	case entryPresented:
		return railLines(th, renderPresentedBlock(th, e.presented, inner), e.depth)
	case entryStartup:
		return railLines(th, renderStartupBox(th, e.startup, inner), e.depth)
	default:
		return nil
	}
}

// renderSubAgentLabel renders the one-line ⤷ sub-agent header that opens a contiguous run of
// sub-agent (Depth > 0) blocks (P3.14). It is itself framed at the run's depth, so the label
// sits inside the same rail as the block it announces.
func renderSubAgentLabel(th theme, depth, width int) []string {
	inner := railedWidth(width, depth)
	body := hangingWrap(th.subRail, glyphSubLabel+" ", subAgentLabel, inner)
	return railLines(th, body, depth)
}

// renderUserBlock renders the user prompt as a full-width white-on-dark-gray block: the ❯
// marker on the first line, a hanging two-column indent on wrapped continuation lines, and
// the dark-gray background padded across the whole width on every line. Any skills attached to
// the send render as chips on a trailing row of the same block, so the attachment stays visible
// after the message is sent (an empty-text send is just the chip row, marker and all).
func renderUserBlock(th theme, text string, skills []string, width int) []string {
	var out []string
	if text != "" {
		for _, ln := range hangingPrefixes(glyphUser+" ", text, width) {
			out = append(out, th.userBlock.Width(width).Render(ln))
		}
	}
	if len(skills) > 0 {
		marker := glyphUser + " " // a text-less send: the chip row leads with the ❯ marker
		if len(out) > 0 {
			marker = "  " // a continuation row: align the chips under the prompt text
		}
		out = append(out, renderUserChipRow(th, marker, skills, width))
	}
	return out
}

// renderUserChipRow composes one full-width row of attached-skill chips inside the user block:
// a dark-gray lead marker, the violet chips (each carrying its own background), and a dark-gray
// pad filling the block to width — three independently styled segments on one line, so the
// chips keep their colour while the row stays a solid block (the footerContent idiom). Chips
// that would overrun the width are clipped ANSI-aware so the row never breaks the block layout.
func renderUserChipRow(th theme, marker string, skills []string, width int) string {
	chips := make([]string, 0, len(skills))
	for _, name := range skills {
		chips = append(chips, renderSkillChip(th, name))
	}
	lead := th.userBlock.Render(marker)
	body := strings.Join(chips, " ")
	if avail := width - lipgloss.Width(lead); lipgloss.Width(body) > avail {
		body = ansi.Truncate(body, max(0, avail), "…")
	}
	pad := th.userBlock.Render(strings.Repeat(" ", max(0, width-lipgloss.Width(lead)-lipgloss.Width(body))))
	return lead + body + pad
}

// renderSkillChip renders one attached-skill badge — the violet " ✦ name " pill the chip row
// (renderUserChipRow) and the pending-chip strip (renderSkillChips) both show. It is the single
// source of a chip's look, so the two rows never drift.
func renderSkillChip(th theme, name string) string {
	return th.skillChip.Render(" " + glyphSkill + " " + name + " ")
}

// renderPresentedBlock renders a presented document (ADR 0019, rung 0) — the one block that is
// deliberately NOT shaped like a tool card, because a deliverable is the point of the work and
// not plumbing. It leads with the ▤ marker and the document's title where the model gave one,
// then the workspace-relative path, then the served URL if there is one, then a dim status line:
//
//	▤ Architecture review
//	  docs/review.html
//	  http://192.168.64.2:51234/d/…/review.html
//	  cmd+click to open
//
// The path and the URL are emitted RAW — no style, no wrap, no clip, one token per line — and
// that is the whole mechanism: the terminal is what turns them into something clickable, so a
// hanging indent inserted mid-token or an SGR run wrapped around them would break the only rung
// that always works. width is therefore ignored for those two lines: an overlong path soft-wraps
// in the viewport (which wrappedOffset already mirrors) rather than being hard-wrapped here.
// The marker keeps the title's styling even when there is no title, so the block opens the same
// way either way.
func renderPresentedBlock(th theme, v presentedView, width int) []string {
	marker := glyphPresented + " "

	var out []string
	if v.Title != "" {
		out = append(out, hangingWrap(th.presentTitle, marker, v.Title, width)...)
		out = append(out, bodyIndent+v.Path)
	} else {
		out = append(out, th.presentTitle.Render(marker)+v.Path)
	}
	if v.Location != "" {
		out = append(out, bodyIndent+v.Location)
	}
	return append(out, hangingWrap(th.noteText, bodyIndent, presentedStatus(v), width)...)
}

// startupWideMinGap is the smallest gap, in columns, the wide start-up layout keeps between the logo
// and the right-aligned info block. When the content cannot fit the logo, this gap, and the info
// block side by side, renderStartupBox falls back to the stacked layout instead. It is the switch's
// only tuning knob — raise it if the two-column layout engages while still looking cramped.
const startupWideMinGap = 4

// startupInfoRow is one label/value pair of the start-up box's info block (host / model / context /
// version). An empty value drops the row.
type startupInfoRow struct{ label, value string }

// renderStartupBox renders the one-time start-up card, choosing a layout by the width it is handed.
// It reuses the prompt box's rounded border glyphs through th.startupBorder while dropping the black
// fill, so the card reads as the same chrome without the input box's solid field. It is
// [renderPresentedBlock]'s sibling — the entry holds the facts, this composes the lines.
//
// When there is room, the WIDE layout paints the logo on the left and a right-aligned
// host / model / context / version block on the right (renderStartupWide). When the width does not
// allow it, the STACKED fallback paints the original card — logo, a blank line, then host / model /
// version below it, no context (renderStartupStacked).
//
// Either way the card spans the full content width: width is the same railed inner budget every
// other transcript entry is laid out to (transcriptWidth), so th.startupBorder.Width(width) lands
// the box's right border on the exact column the rest of the transcript's content ends at. lipgloss
// folds the border and padding into that width, so the rendered lines are exactly width columns.
func renderStartupBox(th theme, v startupView, width int) []string {
	// inner is the content-column budget inside the rounded border and its padding — the room the
	// two layouts actually lay out to. GetHorizontalFrameSize tracks the border + padding, so the
	// arithmetic follows the style rather than a hard-coded 4.
	inner := width - th.startupBorder.GetHorizontalFrameSize()

	rows := make([]startupInfoRow, 0, 4)
	for _, r := range []startupInfoRow{
		{"host", v.Host}, {"model", v.Model}, {"context", v.Context}, {"version", v.Version},
	} {
		if r.value != "" { // an unknown fact (context 0) drops its row, mirroring the footer's nonEmpty
			rows = append(rows, r)
		}
	}

	logo := strings.Split(v.Logo, "\n")
	logoW := 0
	for _, ln := range logo {
		logoW = max(logoW, lipgloss.Width(ln))
	}
	labelW := startupLabelWidth(rows)
	infoW := startupInfoWidth(rows, labelW)

	if inner >= logoW+startupWideMinGap+infoW {
		return renderStartupWide(th, logo, rows, labelW, infoW, width, inner)
	}
	return renderStartupStacked(th, v, width)
}

// renderStartupWide paints the wide start-up card: the logo on the left, the info block right-
// aligned against the right content edge (left column inner-infoW), so the widest info row sits
// flush against the right padding and shorter rows trail off toward it. Logo line i pairs with info
// row i (top-aligned) and whichever side is shorter blank-fills — the four logo lines pair directly
// with the four info rows, so there is no blank spacer. lipgloss pads every line to the full width
// (renderStartupBox's contract). The caller guarantees inner ≥ logoW + startupWideMinGap + infoW, so
// the per-line pad count is at least startupWideMinGap.
func renderStartupWide(th theme, logo []string, rows []startupInfoRow, labelW, infoW, width, inner int) []string {
	left := inner - infoW // the info block's left column
	n := max(len(logo), len(rows))
	lines := make([]string, 0, n)
	for i := 0; i < n; i++ {
		logoLine := ""
		if i < len(logo) {
			logoLine = logo[i]
		}
		line := logoLine + strings.Repeat(" ", max(0, left-lipgloss.Width(logoLine)))
		if i < len(rows) {
			line += startupInfoLine(th, rows[i], labelW)
		}
		lines = append(lines, line)
	}
	return strings.Split(th.startupBorder.Width(width).Render(strings.Join(lines, "\n")), "\n")
}

// renderStartupStacked paints the narrow fallback: the logo, one blank line, then the host / model /
// version rows stacked below it (no context), dim labels aligned in a column and plain values. This
// is the card's original layout, kept for widths too narrow for the two-column wide layout.
func renderStartupStacked(th theme, v startupView, width int) []string {
	content := strings.Split(v.Logo, "\n")
	content = append(content, "") // one blank line between the logo and the info rows

	rows := []startupInfoRow{{"host", v.Host}, {"model", v.Model}, {"version", v.Version}}
	labelW := startupLabelWidth(rows)
	for _, r := range rows {
		content = append(content, startupInfoLine(th, r, labelW))
	}
	return strings.Split(th.startupBorder.Width(width).Render(strings.Join(content, "\n")), "\n")
}

// startupInfoLine renders one info row — the dim label padded to the block's label column, two
// spaces, then the plain value — shared by both start-up layouts so their rows never drift.
func startupInfoLine(th theme, r startupInfoRow, labelW int) string {
	padded := r.label + strings.Repeat(" ", max(0, labelW-lipgloss.Width(r.label)))
	return th.noteText.Render(padded) + "  " + r.value
}

// startupLabelWidth is the widest label among the info rows — the column every value aligns past.
func startupLabelWidth(rows []startupInfoRow) int {
	w := 0
	for _, r := range rows {
		w = max(w, lipgloss.Width(r.label))
	}
	return w
}

// startupInfoWidth is the info block's rendered width: the widest label-padded row (labelW, two
// spaces, then the value). It is the block the wide layout right-aligns and the term the width
// switch measures against.
func startupInfoWidth(rows []startupInfoRow, labelW int) int {
	w := 0
	for _, r := range rows {
		w = max(w, labelW+2+lipgloss.Width(r.value))
	}
	return w
}

// renderToolBlock renders one tool-call block — a single call or a whole grouped run — in the
// one uniform shape layout.md sketches: a ✦ header carrying the **label alone**, then one ┝/┕
// branch per call whose first column is that call's target. The target never sits on the header,
// so a block does not visually reshape the moment a second call joins it: a block of one is
// byte-identical in shape to a block of many. The caller frames the block for depth (renderView
// and renderEntryLines apply the rail) — width is already the railed inner column.
//
// The label is styled (bold orange) before the header is wrapped — the markdown.go posture:
// ansi.Wrap is SGR-aware and lipgloss.Width strips ANSI, so baking the style into the text
// leaves the soft-wrap and sticky-offset arithmetic untouched.
//
// Targets are padded to the block's widest so the detail column lines up; widths are display
// cells (lipgloss.Width), so a multi-byte path pads correctly. A block of one pads to itself,
// which is no padding at all. An empty slice renders nothing — every caller passes at least one
// view, and a renderer on the repaint path must not be the thing that panics if one ever does not.
func renderToolBlock(th theme, views []toolView, width int) []string {
	if len(views) == 0 {
		return nil
	}
	out := hangingWrap(th.toolHeader, glyphAssistant+" ", th.toolLabel.Render(views[0].Label), width)
	column := 0
	for _, tv := range views {
		column = max(column, lipgloss.Width(tv.Target))
	}
	for i, tv := range views {
		out = append(out, renderToolBranch(th, tv, column, branchMarker(i == len(views)-1), width)...)
	}
	return out
}

// renderToolBranch renders one call of a tool block as its branch line (plus whatever hangs
// beneath it). Two shapes, and they are the whole grammar:
//
//   - a call WITH a target — the branch is the target, and when the call has a Summary, the
//     target padded to the block's column, one space, then that summary ("┕ main.go 1 - 154",
//     "┕ main.go +2 -2"). A call still in flight has no summary yet and shows the bare target;
//     the block repaints whole once the result folds in. Its Details, if any, lay out beneath
//     the branch at the branch marker's own width — not as ┝/┕ branches of their own, because
//     only calls are (a Run's output, a diff body under its diffstat).
//   - a call with NO target — the only shape with no target line: the header stands alone and
//     the detail lines are themselves the ┝/┕ branches, the summary last since it has no branch
//     line to ride (an unregistered tool's pretty-printed arguments then its "error: …"
//     outcome, a stray result).
//
// The shape follows from which halves of the outcome are filled and never from how many Details
// there are: a body of one line and a body of ten lay out the same way. Anything overlong
// soft-wraps under its marker like any other detail line — nothing is clipped for alignment's
// sake.
func renderToolBranch(th theme, tv toolView, column int, marker string, width int) []string {
	if tv.Target == "" {
		return renderDetails(th, branchDetails(tv), width)
	}
	text, style := tv.Target, th.toolDetail
	if tv.Summary.Text != "" {
		pad := strings.Repeat(" ", max(0, column-lipgloss.Width(tv.Target)))
		text += pad + " " + tv.Summary.Text
		style = detailStyle(th, tv.Summary.Kind)
	}
	out := hangingWrap(style, marker, text, width)
	return append(out, renderSubDetails(th, tv.Details, lipgloss.Width(marker), width)...)
}

// branchDetails is what a targetless call hangs off its header: the body, plus the summary as
// its last line. A targetless block has no branch line for a summary to ride, so the outcome
// simply closes the branch list — which is where an "error: …" on an unregistered tool has
// always sat, after the arguments that provoked it.
func branchDetails(tv toolView) []detailLine {
	if tv.Summary.Text == "" {
		return tv.Details
	}
	out := make([]detailLine, 0, len(tv.Details)+1)
	out = append(out, tv.Details...)
	return append(out, tv.Summary)
}

// branchMarker is the tree marker leading a branch line: ┕ closes a block, ┝ continues it. Its
// display width is also the sub-content indent, so detail text laid out beneath a branch lines
// up with the target on it.
func branchMarker(last bool) string {
	if last {
		return "  " + glyphBranchLast + " "
	}
	return "  " + glyphBranch + " "
}

// renderSubDetails lays a call's detail lines out beneath its branch line, indented to the
// branch marker's width and styled by kind, so they read as that branch's content rather than
// as siblings of it.
func renderSubDetails(th theme, details []detailLine, indent, width int) []string {
	pad := strings.Repeat(" ", indent)
	out := make([]string, 0, len(details))
	for _, d := range details {
		out = append(out, hangingWrap(detailStyle(th, d.Kind), pad, d.Text, width)...)
	}
	return out
}

// toolCallRun returns the consecutive tool-call entries starting at entries[i] that fold into one
// grouped block, as their presentation views: same sub-agent depth, same friendly Label, every
// member groupable. Any other entry between two calls — narration, a note, an approval, an error —
// ends the run, since the scan only ever walks forward over adjacent entries. Two different tools
// sharing a label (a single and a multi find-and-replace are both "Edit File") do group: the reader
// groups by what the header says, not by tool id. It returns nil when entries[i] is not a groupable
// tool call, and a one-view run when nothing follows it — the caller renders both as single blocks.
func toolCallRun(entries []entry, i int) []toolView {
	head := entries[i]
	if head.kind != entryToolCall || !groupable(head.tool) {
		return nil
	}
	views := []toolView{head.tool}
	for j := i + 1; j < len(entries); j++ {
		e := entries[j]
		if e.kind != entryToolCall || e.depth != head.depth || e.tool.Label != head.tool.Label || !groupable(e.tool) {
			break
		}
		views = append(views, e.tool)
	}
	return views
}

// groupable reports whether a tool call can be shown as one branch line of a grouped block: it
// needs a Target to sit in the aligned column, an empty body so nothing hangs beneath that line,
// and a plain-kind Summary to follow the target on it. That admits the common cases — a finished
// read, an "error: …" outcome, and a call still in flight whose summary has not landed yet (the
// zero detailLine is plain and empty) — while a call carrying a body (a Run and its output, a
// diff body under its "+2 -2" diffstat) or no target at all keeps its own block, where it has the
// room it needs. It never counts detail lines: the block's shape does not depend on how many
// there are, and neither may this.
func groupable(tv toolView) bool {
	return tv.Target != "" && len(tv.Details) == 0 && tv.Summary.Kind == detailPlain
}

// renderOrphanResult renders a tool result that matched no pending call (a defensive
// fallback — normally a result folds into its call by CallID). It reads as a result block:
// a ✦ result header — the bare word styled like any tool label — with the raw content hanging
// off branches. It is targetless by construction, so it renders through the block renderer's
// no-target shape. The caller frames it for depth — width is already the railed inner column.
func renderOrphanResult(th theme, text string, width int) []string {
	details := make([]detailLine, 0)
	for _, ln := range splitLines(text) {
		details = append(details, detailLine{Text: ln})
	}
	return renderToolBlock(th, []toolView{{Label: "result", Details: details}}, width)
}

// renderDetails renders tool-detail lines as ┝/┕ tree branches (the last line gets ┕),
// styled by their kind (plain dim, or red/green for the diff kinds). This is the targetless
// shape only: where a call has a target, the target owns the branch and its details lay out
// beneath it (renderToolBranch).
func renderDetails(th theme, details []detailLine, width int) []string {
	var out []string
	for i, d := range details {
		out = append(out, hangingWrap(detailStyle(th, d.Kind), branchMarker(i == len(details)-1), d.Text, width)...)
	}
	return out
}

// detailStyle maps a detail kind to its style: plain detail is dim; the diff kinds are
// red/green (view_diff's body is their producer — diffDetail).
func detailStyle(th theme, kind detailKind) lipgloss.Style {
	switch kind {
	case detailDiffAdded:
		return th.diffAdded
	case detailDiffRemoved:
		return th.diffRemoved
	default:
		return th.toolDetail
	}
}

// ----------------------------------------------------------------------------
// Wrapping primitives
// ----------------------------------------------------------------------------

// hangingWrap word-wraps text under a leading marker, then styles each physical line: the
// marker leads the first line and a same-width blank indent leads every continuation line, so
// a wrapped block stays aligned under its marker (the ✦/┝ hanging indent of layout.md). The
// style colours the whole line; widths are ANSI-agnostic, so styling never perturbs the
// soft-wrap arithmetic.
func hangingWrap(style lipgloss.Style, marker, text string, width int) []string {
	prefixed := hangingPrefixes(marker, text, width)
	out := make([]string, len(prefixed))
	for i, ln := range prefixed {
		out[i] = style.Render(ln)
	}
	return out
}

// hangingPrefixes word-wraps text to the width left of the marker and prepends the marker to
// the first line and a matching blank indent to the rest, returning the unstyled lines. It is
// shared by the styled hanging wrap and the user block (which then pads each line to a
// full-width background).
func hangingPrefixes(marker, text string, width int) []string {
	mw := lipgloss.Width(marker)
	indent := strings.Repeat(" ", mw)
	lines := wrapText(text, max(1, width-mw))
	out := make([]string, len(lines))
	for i, ln := range lines {
		if i == 0 {
			out[i] = marker + ln
		} else {
			out[i] = indent + ln
		}
	}
	return out
}

// wrapText word-wraps text to limit columns, hard-breaking any word longer than the limit
// and preserving the text's own newlines. An empty string yields a single empty line so a
// just-opened assistant buffer still renders its marker.
func wrapText(text string, limit int) []string {
	if limit < 1 {
		limit = 1
	}
	return strings.Split(ansi.Wrap(text, limit, ""), "\n")
}

// railWidth is the column cost of one sub-agent rail gutter ("│ " — the rail glyph plus one
// space), the amount each nesting level steals from the usable text width (P3.14).
const railWidth = 2

// railedWidth is the usable text width inside a Depth-level block: the full width less one
// rail gutter per level. Depth 0 is the common case and returns width unchanged; deeper
// levels are floored at one column so wrapping never divides by zero.
func railedWidth(width, depth int) int {
	if depth <= 0 {
		return width
	}
	return max(1, width-depth*railWidth)
}

// railLines frames a Depth-level block: it prepends one styled "│ " rail gutter per nesting
// level to each physical line, so a sub-agent's nested block reads as a vertical-ruled
// sub-section (P3.14). Depth 0 is the common case and returns the lines untouched, so the
// flat top-level transcript renders exactly as before. The rail is styled (dim) and sits
// left of any per-line background (e.g. the user block's), matching the marker hanging indent.
func railLines(th theme, lines []string, depth int) []string {
	if depth <= 0 {
		return lines
	}
	gutter := th.subRail.Render(strings.Repeat(glyphSubRail+" ", depth))
	out := make([]string, len(lines))
	for i, ln := range lines {
		out[i] = gutter + ln
	}
	return out
}

// ----------------------------------------------------------------------------
// Sticky-to-top offset
// ----------------------------------------------------------------------------

// ----------------------------------------------------------------------------
// Chrome layout helpers
// ----------------------------------------------------------------------------

// inputContentRows reports how many visual rows the input value occupies at innerWidth, mirroring
// the textarea's own wrap so the box sizes to exactly what the widget draws. Each logical line is
// word-then-hard wrapped like the widget; a line whose final wrapped segment exactly fills the
// width gains one extra row, because the textarea's wrap reserves a trailing row there (its
// `>= width` branch) so the caret has somewhere to sit past a full line. Under-counting that row
// leaves the box one row too short at a width-fill boundary — the source of the scroll artifact
// the layout re-seat then can no longer reach (ISSUES #2). An empty value is one row.
func inputContentRows(value string, innerWidth int) int {
	if innerWidth < 1 {
		innerWidth = 1
	}
	total := 0
	for _, line := range strings.Split(value, "\n") {
		wrapped := ansi.Hardwrap(ansi.Wordwrap(line, innerWidth, ""), innerWidth, true)
		segs := strings.Split(wrapped, "\n")
		rows := len(segs)
		if ansi.StringWidth(segs[len(segs)-1]) >= innerWidth {
			rows++ // the widget's trailing full-line row
		}
		total += rows
	}
	return total
}

// clampInt clamps n to [lo, hi].
func clampInt(n, lo, hi int) int {
	if n < lo {
		return lo
	}
	if n > hi {
		return hi
	}
	return n
}

// wrappedOffset returns the virtual (soft-wrapped) row at which the line after linesAbove
// begins — the Y offset that pins that line to the top of the viewport. It mirrors the
// viewport's calculateLine exactly: each physical line occupies max(1, ceil(width/vpWidth))
// rows. This holds only while the viewport has no border or gutter (maxWidth == Width, the
// current wiring); TestWrappedOffsetMatchesViewport guards the equality against drift.
func wrappedOffset(linesAbove []string, vpWidth int) int {
	if vpWidth < 1 {
		vpWidth = 1
	}
	total := 0
	for _, ln := range linesAbove {
		w := lipgloss.Width(ln)
		rows := 1
		if w > 0 {
			rows = (w + vpWidth - 1) / vpWidth // ceil(w / vpWidth)
		}
		total += rows
	}
	return total
}
