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
// beneath it). Four shapes, and they are the whole grammar:
//
//   - one detail — the branch is the target, padded to the block's column, one space, then the
//     detail ("┕ main.go 1 - 154");
//   - two or more details — the branch carries the target alone and the details lay out beneath
//     it at the branch marker's own width, not as ┝/┕ branches of their own (the Run case);
//   - no detail yet (in flight) — the branch is the bare target; the block repaints whole once
//     the result folds in;
//   - no target at all — the only shape with no target line: the header stands alone and the
//     details are themselves the ┝/┕ branches (an unregistered tool's pretty-printed arguments,
//     a stray result).
//
// Anything overlong soft-wraps under its marker like any other detail line — nothing is clipped
// for alignment's sake.
func renderToolBranch(th theme, tv toolView, column int, marker string, width int) []string {
	if tv.Target == "" {
		return renderDetails(th, tv.Details, width)
	}
	if len(tv.Details) == 1 {
		pad := strings.Repeat(" ", max(0, column-lipgloss.Width(tv.Target)))
		text := tv.Target + pad + " " + tv.Details[0].Text
		return hangingWrap(detailStyle(th, tv.Details[0].Kind), marker, text, width)
	}
	out := hangingWrap(th.toolDetail, marker, tv.Target, width)
	return append(out, renderSubDetails(th, tv.Details, lipgloss.Width(marker), width)...)
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
// needs a Target to sit in the aligned column and at most one plain detail line to follow it. That
// admits the common cases — a finished read, an "error: …" outcome, and a call still in flight with
// no result yet — while a call with several details (a Run and its "… +N more lines" remainder), a
// diff-coloured detail, or no target at all keeps its own block, where it has the room it needs.
func groupable(tv toolView) bool {
	if tv.Target == "" || len(tv.Details) > 1 {
		return false
	}
	for _, d := range tv.Details {
		if d.Kind != detailPlain {
			return false
		}
	}
	return true
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
// red/green (reserved — no extractor emits them until an edit/diff tool exists).
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

// ruleMix composes one of the footer's decorative rules: a heavy ━ bar with a lighter ─
// inset toward the right, echoing layout.md. The counts always sum to exactly n so the rule
// spans the full width between its corners.
func ruleMix(n int) string {
	if n <= 0 {
		return ""
	}
	light := n / 4
	rightHeavy := n / 8
	leftHeavy := n - light - rightHeavy
	return strings.Repeat("━", leftHeavy) + strings.Repeat("─", light) + strings.Repeat("━", rightHeavy)
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
