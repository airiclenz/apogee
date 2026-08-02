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
// the prompt-offset arithmetic needs to measure the wrapped height of the lines above the last
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
// prompt's first line (-1 when the transcript holds no user prompt), the line range of
// every user block, and what each line is to a motionless click. The caller measures the last
// prompt's offset to pad the lines below a short reply (so the bottom lands on the prompt row),
// follows the tail unless the human has scrolled away, overlays the user block owning the top row
// as a sticky header, and resolves a click through targets (model.go).
type renderedTranscript struct {
	lines         []string
	lastUserStart int
	userBlocks    []userBlock
	// targets is PARALLEL to lines — one entry per physical line, the zero value for every line
	// that is not part of a block's click surface. It is built in lockstep with line emission by
	// the painter itself and is never re-derived by a second reader: a click resolves against the
	// exact accounting the paint used (ADR 0030's rule, one authority per measurement).
	targets []lineTarget
}

// targetKind says what one rendered line is to a motionless click (layout.md, "Collapsed and
// expanded blocks"): nothing at all — the overwhelmingly common case, and the zero value — a
// block HEADER line, or a synthesized remainder MARKER line. A click on a header toggles its
// block; a click on a marker expands the block whose body the marker is counting for.
type targetKind int

const (
	targetNone targetKind = iota
	targetHeader
	targetMarker
)

// lineTarget is one rendered line's click surface: what the line is, and the index into
// transcript.entries of the block's HEAD entry — the entry whose expanded state a click there
// flips (transcript.toggleExpanded). The zero value is "no target", which is what every line
// outside a toggleable block's header and marker carries, so a lookup needs no second sentinel.
type lineTarget struct {
	kind  targetKind
	entry int
}

// blockPaint is one painted block: its physical lines and, parallel to them, what each line is to
// a click. A block painter says WHAT each of its lines is; [transcript.renderView] alone says
// WHOSE, stamping the head entry's index as it lays the block into the transcript — which is why
// the kinds here carry no entry index and no painter needs to know where in the entry list it sits.
//
// The two slices are grown only through [blockPaint.add] and [blockPaint.join], so they cannot
// drift out of lockstep: every line that is appended is marked in the same call that appends it.
type blockPaint struct {
	lines   []string
	targets []targetKind
}

// plainPaint is the paint of a block that carries no click surface at all — a user send, an
// assistant answer, a note, a start-up box, a ⤷ descent label, a stray result. Everything the
// renderer emits that is not a tool block goes through here, so "no target" is stated once rather
// than spelled out at each producer.
func plainPaint(lines []string) blockPaint {
	return blockPaint{lines: lines, targets: make([]targetKind, len(lines))}
}

// add appends lines that all carry the same target kind. A WRAPPED header is the reason it takes a
// slice rather than a line: every physical line a header occupies is part of the same click
// surface (layout.md — the click lands on the header, not on its first row), and the same holds
// for a remainder marker narrow enough to wrap.
func (p *blockPaint) add(lines []string, kind targetKind) {
	p.lines = append(p.lines, lines...)
	for range lines {
		p.targets = append(p.targets, kind)
	}
}

// join appends another paint whole — its lines and its own target marks — so a block composed of
// sub-paints (a tool block of one branch or of many) keeps their marks without re-deriving them.
func (p *blockPaint) join(q blockPaint) {
	p.lines = append(p.lines, q.lines...)
	p.targets = append(p.targets, q.targets...)
}

// railed frames the paint for its sub-agent depth (railLines) while carrying its target marks
// through untouched: the rail prefixes each line in place and adds none, so the marks stay in
// lockstep with the lines they belong to.
func (p blockPaint) railed(th theme, depth int) blockPaint {
	return blockPaint{lines: railLines(th, p.lines, depth), targets: p.targets}
}

// renderView renders the committed entries plus any in-progress assistant buffer into the
// viewport's lines, recording where the last user block begins. Blocks are separated by one
// line (layout.md), railed at the depth the two blocks share so a sub-agent run's frame is
// continuous through its separators (railSpacer).
//
// blink is this frame's phase of the live star ([spinnerAnim.blink]): it reaches only the header
// glyph of a block that still holds an open call, and every other line of the transcript paints
// identically at either phase. It is a PARAMETER rather than transcript state because the phase
// belongs to the frame being drawn and not to the scrollback — the same entries painted a tick
// later are the same entries (ADR 0011: the Model is copied by value, and the renderer stays a
// pure function of what it is handed).
func (t *transcript) renderView(th theme, width int, blink bool) renderedTranscript {
	if width < 1 {
		width = 1
	}
	var lines []string
	var targets []lineTarget
	var userBlocks []userBlock
	lastUserStart := -1

	// prevBlockDepth is the depth of the block appended last — the left half of the next
	// spacer's join. It is deliberately per-APPENDED-BLOCK rather than per-entry (the loop's
	// prevDepth below): the ⤷ label blocks the descent loop emits carry depths of their own,
	// and a spacer's rail follows the blocks it actually sits between.
	prevBlockDepth := 0
	// head is the index into t.entries of the entry whose block state a click on this block
	// toggles. It is stamped only onto the lines the block itself marked as a click surface, so a
	// block that marks none — every kind but a tool block — may pass whatever index it sits at.
	appendBlock := func(isUser bool, depth, head int, block blockPaint) {
		if len(lines) > 0 {
			lines = append(lines, railSpacer(th, min(prevBlockDepth, depth)))
			targets = append(targets, lineTarget{}) // a separator belongs to neither block
		}
		if isUser {
			lastUserStart = len(lines)
			userBlocks = append(userBlocks, userBlock{start: len(lines), count: len(block.lines)})
		}
		// The line and its mark are appended in ONE loop, and the mark is read defensively, so the
		// map is exactly as long as the lines whatever a painter handed over. That length is the
		// mouse's whole safety: a target index that outlived its line is a click toggling some
		// other block, on a path where the alternative to a rule is a panic mid-repaint.
		for i, ln := range block.lines {
			lines = append(lines, ln)
			target := lineTarget{}
			if i < len(block.targets) && block.targets[i] != targetNone {
				target = lineTarget{kind: block.targets[i], entry: head}
			}
			targets = append(targets, target)
		}
		prevBlockDepth = depth
	}

	prevDepth := 0
	for i := 0; i < len(t.entries); i++ {
		e := t.entries[i]
		// Open a ⤷ sub-agent label whenever a run descends to a deeper level than the
		// previous block — a 0→1 (or 1→2) transition announces the nested section once,
		// per level, until the stream climbs back out (P3.14).
		if e.depth > prevDepth {
			for d := prevDepth + 1; d <= e.depth; d++ {
				appendBlock(false, d, i, plainPaint(renderSubAgentLabel(th, d, width)))
			}
		}
		// A sub-agent run is ONE block while it is collapsed (layout.md): its head paints with the
		// cascading summary and the whole span is then skipped outright, which is what elides the
		// inner blocks, their ⤷ labels, and every rail and spacer among them — nothing is painted
		// and afterwards taken back, and the descent logic above never fires because it never sees
		// a deeper entry. Expanded, only the head is painted here and the loop walks into the span
		// exactly as it always has, so every inner block keeps its OWN state and a nested run
		// collapses inside an expanded parent by this same rule, at every depth.
		if span := subAgentSpan(t.entries, i); span > 0 {
			appendBlock(false, e.depth, i, renderSubAgentRun(th, e, t.entries[i+1:i+1+span], width, blink))
			if !e.expanded {
				i += span
			}
		} else if run := toolCallRun(t.entries, i); len(run) > 1 {
			// Consecutive same-label tool calls fold into one block at render time, so a batch of
			// reads is one header plus an aligned branch per file. The entry list is untouched: a
			// call that arrives mid-stream joins its group on the next repaint for free, and a run
			// is same-depth by construction, so the label logic above fires exactly as before.
			//
			// The group's liveness is the group's, not its head's: a batch of reads whose first call
			// has landed and whose last has not is still working, and the one star over them all says
			// so. The run is entries[i:i+len(run)] by construction (toolCallRun walks adjacent
			// entries forward), so the views' own entries are what the rule reads.
			block := renderToolBlock(th, run, railedWidth(width, e.depth), blockState{
				expanded: e.expanded,
				live:     anyOpenCall(t.entries[i : i+len(run)]),
				blink:    blink,
			}).railed(th, e.depth)
			appendBlock(false, e.depth, i, block)
			i += len(run) - 1
		} else {
			appendBlock(e.kind == entryUser, e.depth, i, renderEntryLines(th, e, width, blink))
		}
		prevDepth = e.depth
	}
	if t.streaming {
		// The in-progress buffer is trimmed of its trailing blank lines for display only: the
		// buffer keeps them (a mid-stream "\n\n" may be a paragraph break about to be continued),
		// but the preview must not grow a wobbling gap above the footer. An empty buffer still
		// renders its lone marker line, so the human sees that streaming has begun.
		preview := renderEntryLines(th, entry{kind: entryAssistant, text: trimTrailingBlankLines(t.pending)}, width, blink)
		appendBlock(false, 0, len(t.entries), preview)
	}
	return renderedTranscript{lines: lines, lastUserStart: lastUserStart, userBlocks: userBlocks, targets: targets}
}

// renderLines is the line slice alone — the viewport content and the substring-test surface — at
// the star's SETTLED phase: the blink is a fact about the frame being drawn, and a caller that has
// no frame (a width probe, a substring assertion) has no phase either.
func (t *transcript) renderLines(th theme, width int) []string {
	return t.renderView(th, width, false).lines
}

// renderEntryLines renders one committed entry into its physical lines, framed for its
// sub-agent depth, plus what each of those lines is to a click. The user prompt is a full-width
// block; everything else hangs off a marker. A Depth > 0 entry is wrapped to the narrower column
// left of its rail gutter, then each line is prefixed by the rail (P3.14) so the nested block
// reads as a framed sub-section.
//
// Only a tool call has a click surface — it is the only kind with two states to toggle between —
// so every other kind comes back as plainPaint: a note or an answer paints one way whatever is
// asked of it, and a click there keeps its selection meaning. blink reaches that one kind for the
// same reason: a tool call is the only entry that can still be WAITING for something, so it is the
// only header with a star to blink (layout.md, "The live star").
func renderEntryLines(th theme, e entry, width int, blink bool) blockPaint {
	inner := railedWidth(width, e.depth)
	switch e.kind {
	case entryUser:
		return plainPaint(railLines(th, renderUserBlock(th, glyphUser+" ", e.text, e.skills, inner), e.depth))
	case entryInterjected:
		// The human's mid-Exchange remark: the same block the prompt gets — it is the same voice —
		// under the ⧖ marker that says it arrived while the model was already working (ADR 0025).
		// Its chip row is the sent block's, for the same reason: a skill rides an interjection
		// (ADR 0027), and the record of what the model was given must not depend on whether the
		// remark was delivered mid-run or flushed into a new Exchange.
		return plainPaint(railLines(th, renderUserBlock(th, glyphInterject+" ", e.text, e.skills, inner), e.depth))
	case entryAssistant:
		marker := glyphAssistant + " "
		body := renderMarkdownBody(th, e.text, inner-th.measure.Width(marker))
		return plainPaint(railLines(th, withMarker(th, marker, body), e.depth))
	case entryToolCall:
		return renderToolBlock(th, []toolView{e.tool}, inner, blockState{
			expanded: e.expanded,
			live:     !e.done,
			blink:    blink,
		}).railed(th, e.depth)
	case entryToolResult:
		return plainPaint(railLines(th, renderOrphanResult(th, e.text, inner), e.depth))
	case entryError:
		return plainPaint(railLines(th, hangingWrap(th, th.errorText, glyphAssistant+" ", e.text, inner), e.depth))
	case entryNote:
		return plainPaint(railLines(th, hangingWrap(th, th.noteText, "· ", e.text, inner), e.depth))
	case entryPresented:
		return plainPaint(railLines(th, renderPresentedBlock(th, e.presented, inner), e.depth))
	case entryStartup:
		return plainPaint(railLines(th, renderStartupBox(th, e.startup, inner), e.depth))
	default:
		return blockPaint{}
	}
}

// renderSubAgentLabel renders the one-line ⤷ sub-agent header that opens a contiguous run of
// sub-agent (Depth > 0) blocks (P3.14). It is itself framed at the run's depth, so the label
// sits inside the same rail as the block it announces.
func renderSubAgentLabel(th theme, depth, width int) []string {
	inner := railedWidth(width, depth)
	body := hangingWrap(th, th.subRail, glyphSubLabel+" ", subAgentLabel, inner)
	return railLines(th, body, depth)
}

// subAgentToolName is the raw tool id whose call block heads a sub-agent run. The span rule matches
// on the view's retained name (toolView.name, which the codec round-trips) rather than on the
// "Sub-Agent" label, so a relabelling cannot silently switch the rule off and a third-party tool
// that happens to share the label cannot switch it on.
const subAgentToolName = "sub_agent"

// subAgentSpan is the length of the run entries[i] heads: the maximal following stretch of entries
// nested deeper than it. That stretch IS the run — the transcript records a sub-agent's work
// head-first and folds the report back into the head (transcript.addToolResult), so everything the
// child did lies between the call and the next entry standing at the parent's own depth. Nothing
// marks the span; it is derived at paint from the depths already on the entries, exactly as
// renderView's ⤷ descent labels are.
//
// It answers 0 for anything that is not a sub-agent call, and for a run that produced no nested
// entry at all (a child that failed before its first event) — either way the head is an ordinary
// tool block with nothing behind it, and renderView paints it as one.
func subAgentSpan(entries []entry, i int) int {
	head := entries[i]
	if head.kind != entryToolCall || head.tool.name != subAgentToolName {
		return 0
	}
	n := 0
	for j := i + 1; j < len(entries) && entries[j].depth > head.depth; j++ {
		n++
	}
	return n
}

// renderSubAgentRun paints the head block of a sub-agent run — the whole of what a COLLAPSED run
// shows, since renderView elides its span, and the opening block of an expanded one. The two states
// differ in exactly what layout.md says they do: collapsed, the head's summary slot carries the
// cascading summary of the work behind it; expanded, the head is an ordinary block with its full
// report and the span paints as it always has.
//
// A COLLAPSED run's head is ONE summarised line: the report body is elided along with the span,
// because the summary slot already carries that report's first line and no block says the same
// thing twice in two adjacent rows (layout.md, "A sub-agent run collapses to its call block"). The
// count in that line is what says work is hidden behind the header, so the run needs no
// "… +N more lines" marker to say it too — and the header is a target however short the report is
// (blockState.elides), so nothing is unreachable.
//
// The head's view is COPIED before its summary is replaced and its body dropped, so both are
// paint-time acts on facts the entry keeps whole — the same discipline the body's truncation follows
// (collapsedDetails), and the reason expanding shows the report the run actually returned.
// A run is live until its REPORT lands, and its span is asked as well as its head: a report that
// never arrived (a child cancelled mid-tool) leaves the head open, and — the mirror case — a head
// already reported over a call that never got its result leaves work still standing behind the
// star. Either way the block still contains an open call, which is exactly what layout.md makes the
// star's rule.
func renderSubAgentRun(th theme, head entry, span []entry, width int, blink bool) blockPaint {
	view := head.tool
	if !head.expanded {
		view.Summary = subAgentSummary(head, span)
		view.Details = toolBody{} // the zero body: no lines, and so nothing to lay out beneath
	}
	return renderToolBlock(th, []toolView{view}, railedWidth(width, head.depth), blockState{
		expanded: head.expanded,
		elides:   true,
		live:     !head.done || anyOpenCall(span),
		blink:    blink,
	}).railed(th, head.depth)
}

// subAgentSummary words a collapsed run's one line: how much work happened in there, then its gist
// (layout.md, "A sub-agent run collapses to its call block"). The count is TRANSITIVE by
// construction — the span holds every entry of every level below the head, so counting its tool
// calls counts the grandchildren too, and the same rule read at a deeper head gives that level's
// own total without a second rule.
//
// A run with nothing to say beyond the count — no call open yet, or a report that carried no line
// at all — keeps the count alone rather than trailing an empty separator.
func subAgentSummary(head entry, span []entry) detailLine {
	calls := 0
	for i := range span {
		if span[i].kind == entryToolCall {
			calls++
		}
	}
	text := plural(calls, "tool call")
	if gist := subAgentGist(head, span); gist != "" {
		text += " · " + gist
	}
	return detailLine{Text: text}
}

// subAgentGist is the second half of a collapsed run's summary, in the two tempi layout.md gives it.
// While the run works — the head has no result yet — it is the live phrase for the call the span has
// open, the same composition the status line shows for that call (toolPhrase, activity.go), read off
// the span at paint rather than kept as a second copy of the activity state. The MOST RECENT open
// call is the honest one when several are open at once: it is the work the child turned to last.
//
// Once the report lands the head has a gist of its own and that is the line — the summary a short
// report was compressed to, or, where the report was long enough to become a body, its first line.
func subAgentGist(head entry, span []entry) string {
	if head.done {
		if head.tool.Summary.Text != "" {
			return head.tool.Summary.Text
		}
		if body := head.tool.Details; body.len() > 0 {
			return body.all()[0].Text
		}
		return ""
	}
	for i := len(span) - 1; i >= 0; i-- {
		if e := span[i]; e.kind == entryToolCall && !e.done {
			return toolPhrase(e.tool)
		}
	}
	return ""
}

// renderUserBlock renders something the human said as a full-width white-on-dark-gray block: the
// marker on the first line, a hanging two-column indent on wrapped continuation lines, and
// the dark-gray background padded across the whole width on every line. Any skills attached to
// the send render as chips on a trailing row of the same block, so the attachment stays visible
// after the message is sent (an empty-text send is just the chip row, marker and all).
//
// marker is the block's lead ("❯ " for a submitted prompt, "⧖ " for a delivered interjection):
// the two are the same voice and so share one shape, and the glyph is the whole of the
// difference the reader needs.
func renderUserBlock(th theme, marker, text string, skills []string, width int) []string {
	var out []string
	if text != "" {
		for _, ln := range hangingPrefixes(th, marker, text, width) {
			out = append(out, th.userBlock.Width(width).Render(ln))
		}
	}
	if len(skills) > 0 {
		chipMarker := marker // a text-less send: the chip row leads with the block's own marker
		if len(out) > 0 {
			chipMarker = "  " // a continuation row: align the chips under the prompt text
		}
		out = append(out, renderUserChipRow(th, chipMarker, skills, width))
	}
	return out
}

// renderUserChipRow composes one full-width row of invoked-skill chips inside the user block:
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
	if avail := width - th.measure.Width(lead); th.measure.Width(body) > avail {
		body = th.measure.Truncate(body, max(0, avail), "…")
	}
	pad := th.userBlock.Render(strings.Repeat(" ", max(0, width-th.measure.Width(lead)-th.measure.Width(body))))
	return lead + body + pad
}

// renderSkillChip renders one invoked-skill badge — the violet " ✦ name " pill the sent block's
// chip row (renderUserChipRow) shows for every skill the message invoked. It is the single source
// of a chip's look.
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
		out = append(out, hangingWrap(th, th.presentTitle, marker, v.Title, width)...)
		out = append(out, bodyIndent+v.Path)
	} else {
		out = append(out, th.presentTitle.Render(marker)+v.Path)
	}
	if v.Location != "" {
		out = append(out, bodyIndent+v.Location)
	}
	return append(out, hangingWrap(th, th.noteText, bodyIndent, presentedStatus(v), width)...)
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
		logoW = max(logoW, th.measure.Width(ln))
	}
	labelW := startupLabelWidth(th, rows)
	infoW := startupInfoWidth(th, rows, labelW)

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
		line := logoLine + strings.Repeat(" ", max(0, left-th.measure.Width(logoLine)))
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
	labelW := startupLabelWidth(th, rows)
	for _, r := range rows {
		content = append(content, startupInfoLine(th, r, labelW))
	}
	return strings.Split(th.startupBorder.Width(width).Render(strings.Join(content, "\n")), "\n")
}

// startupInfoLine renders one info row — the dim label padded to the block's label column, two
// spaces, then the plain value — shared by both start-up layouts so their rows never drift.
func startupInfoLine(th theme, r startupInfoRow, labelW int) string {
	padded := r.label + strings.Repeat(" ", max(0, labelW-th.measure.Width(r.label)))
	return th.noteText.Render(padded) + "  " + r.value
}

// startupLabelWidth is the widest label among the info rows — the column every value aligns past.
func startupLabelWidth(th theme, rows []startupInfoRow) int {
	w := 0
	for _, r := range rows {
		w = max(w, th.measure.Width(r.label))
	}
	return w
}

// startupInfoWidth is the info block's rendered width: the widest label-padded row (labelW, two
// spaces, then the value). It is the block the wide layout right-aligns and the term the width
// switch measures against.
func startupInfoWidth(th theme, rows []startupInfoRow, labelW int) int {
	w := 0
	for _, r := range rows {
		w = max(w, labelW+2+th.measure.Width(r.value))
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
// ansi.Wrap is SGR-aware and the width authority strips ANSI, so baking the style into the text
// leaves the soft-wrap and sticky-offset arithmetic untouched.
//
// Targets are padded to the block's widest so the detail column lines up; widths are display
// cells (th.measure, width.go), so a multi-byte path pads correctly. A block of one pads to itself,
// which is no padding at all. An empty slice renders nothing — every caller passes at least one
// view, and a renderer on the repaint path must not be the thing that panics if one ever does not.
//
// state is the block's view state, and its expanded half changes exactly one thing: an expanded
// block paints its retained body whole while a collapsed one paints the compact shape, remainder
// marker and all (renderToolBranch). A GROUPED run is the degenerate case — its members carry no
// body by definition (groupable), so both states paint identically and the head entry's state is
// passed only so the two callers share one call shape (layout.md, "Collapsed and expanded blocks").
// Its live half reaches one glyph and no shape at all: the header's leading star (state.star), ✦
// once the block has settled and blinking against ✧ while it still holds an open call.
//
// It also marks the block's CLICK SURFACE as it emits it — every physical line of the header, and
// any remainder marker a branch synthesized — because the lines and the marks have to be one act:
// a second pass over the finished lines would be a second derivation of the same accounting, and
// the two would drift the first time the shape changed (ADR 0030's rule). The header is marked
// when the collapsed paint HIDES something — either inside the views (blockHidesWhenCollapsed) or
// outside them (state.elides, the sub-agent run's span) — because a block with nothing to reveal
// has nothing to toggle, so a body-less group's header keeps a click's selection meaning. The mark
// is state-independent by design: an expanded block still marks its header, which is what lets the
// same click collapse it again.
func renderToolBlock(th theme, views []toolView, width int, state blockState) blockPaint {
	if len(views) == 0 {
		return blockPaint{}
	}
	header := targetNone
	if state.elides || blockHidesWhenCollapsed(views) {
		header = targetHeader
	}
	var out blockPaint
	out.add(hangingWrap(th, th.toolHeader, state.star()+" ", th.toolLabel.Render(views[0].Label), width), header)
	column := 0
	for _, tv := range views {
		column = max(column, th.measure.Width(tv.Target))
	}
	for i, tv := range views {
		out.join(renderToolBranch(th, tv, column, branchMarker(i == len(views)-1), width, state.expanded))
	}
	return out
}

// blockState is what a tool block's painter is told beyond the views themselves: which of the two
// paints to draw, and whether the collapsed one hides anything the views cannot account for.
//
// elides is the sub-agent run's case, and today its only one. A collapsed run's whole span — every
// inner block, every ⤷ label, every rail — is left unpainted by [transcript.renderView] before the
// head block ever reaches the painter, so nothing among the views records that there is something
// behind the header. The toggle-target rule has to know anyway: layout.md makes a run with a span
// clickable however short its own report is. Like the mark itself it is state-INDEPENDENT — an
// expanded run sets it too, which is what leaves the header clickable so the same click closes it.
//
// live and blink are the LIVE STAR's two halves, and they are deliberately separate: live is a fact
// about the block (something in it is still waiting for a result — anyOpenCall), blink is a fact
// about the frame (the spinner's phase this repaint was asked for — spinnerAnim.blink). Only their
// conjunction paints ✧, so a settled block is immune to the phase and a live one needs no clock of
// its own.
type blockState struct {
	expanded bool
	elides   bool
	live     bool
	blink    bool
}

// star is the glyph the block's header leads with (layout.md, "The live star"): ✦ for a block that
// has everything it was waiting for, and ✦/✧ alternating with the frame's blink phase while it does
// not. The zero value is a settled block at the settled phase, which is why every caller with
// nothing running — a stray result's block, a width probe — keeps the star the transcript has
// always led with without saying so.
func (s blockState) star() string {
	if s.live && s.blink {
		return glyphAssistantHollow
	}
	return glyphAssistant
}

// anyOpenCall reports whether any of these entries is a tool call still waiting for its result —
// what makes the block they belong to LIVE, and so what makes its header star blink. It is
// transcript.hasOpenToolCall's rule read over one block's own entries instead of over the whole
// scrollback: the status line asks whether ANYTHING is still running, a header asks whether the
// work behind THAT star is.
func anyOpenCall(entries []entry) bool {
	for i := range entries {
		if entries[i].kind == entryToolCall && !entries[i].done {
			return true
		}
	}
	return false
}

// blockHidesWhenCollapsed reports whether a block's collapsed paint leaves anything unshown — the
// whole of the toggle-target rule: a header is a click target exactly when there is something
// behind it. It asks collapsedDetails, the function that does the hiding, rather than re-deriving
// the caps, so the rule cannot answer differently from the paint.
//
// A call with no target hides nothing however long its body is: its detail lines ARE the block's
// ┝/┕ branches (renderToolBranch's targetless shape), and an unregistered tool's verbatim
// arguments or a stray result's lines are never capped. One call in a block with something to
// reveal makes the whole block a target — the header belongs to the block, not to a branch.
func blockHidesWhenCollapsed(views []toolView) bool {
	for _, tv := range views {
		if tv.Target == "" {
			continue
		}
		if _, _, truncated := collapsedDetails(tv.Details); truncated {
			return true
		}
	}
	return false
}

// renderToolBranch renders one call of a tool block as its branch line (plus whatever hangs
// beneath it). Two shapes, and they are the whole grammar:
//
//   - a call WITH a target — the branch is the target, and when the call has a Summary, the
//     target padded to the block's column, one space, then that summary ("┕ main.go 1 - 154",
//     "┕ main.go +2 -2"). A call still in flight has no summary yet and shows the bare target;
//     the block repaints whole once the result folds in. Its Details, if any, are the block's
//     body and lay out beneath the branch at the branch marker's own width — not as ┝/┕ branches
//     of their own, because only calls are (a Run's output, a diff body under its diffstat) —
//     truncated to what the collapsed shape shows when the block is collapsed, which is the one
//     place that truncation happens (collapsedDetails), and painted whole when it is expanded.
//   - a call with NO target — the only shape with no target line: the header stands alone and
//     the detail lines are themselves the ┝/┕ branches, the summary last since it has no branch
//     line to ride (an unregistered tool's pretty-printed arguments then its "error: …"
//     outcome, a stray result).
//
// The shape follows from which halves of the outcome are filled and never from how many Details
// there are: a body of one line and a body of ten lay out the same way. Anything overlong
// soft-wraps under its marker like any other detail line — nothing is clipped for alignment's
// sake.
// The block's state reaches only the body: an expanded block lays out every line the entry
// retained and grows no remainder marker, a collapsed one paints collapsedDetails. The targetless
// shape ignores the state entirely, because it hides nothing in either — its detail lines ARE the
// block's branches.
//
// The synthesized remainder marker is marked as a click target as it is laid out, and it is laid
// out on its own so the mark lands on exactly the marker's physical lines (all of them, should it
// ever wrap) and on nothing else. Neither the branch line nor a body line is a target: a click on
// what is already shown keeps its selection meaning.
func renderToolBranch(th theme, tv toolView, column int, marker string, width int, expanded bool) blockPaint {
	if tv.Target == "" {
		return plainPaint(renderDetails(th, branchDetails(tv), width))
	}
	text, style := tv.Target, th.toolDetail
	if tv.Summary.Text != "" {
		pad := strings.Repeat(" ", max(0, column-th.measure.Width(tv.Target)))
		text += pad + " " + tv.Summary.Text
		style = detailStyle(th, tv.Summary.Kind)
	}
	indent := th.measure.Width(marker)

	var out blockPaint
	out.add(hangingWrap(th, style, marker, text, width), targetNone)
	if expanded {
		out.add(renderSubDetails(th, tv.Details.all(), indent, width), targetNone)
		return out
	}
	shown, remainder, truncated := collapsedDetails(tv.Details)
	out.add(renderSubDetails(th, shown, indent, width), targetNone)
	if truncated {
		out.add(renderSubDetails(th, []detailLine{remainder}, indent, width), targetMarker)
	}
	return out
}

// diffDetailCap bounds how many diff lines a COLLAPSED block paints — enough to read a focused
// change, not enough for a rewrite to flood the transcript. It is a paint-time cap on a body
// the entry keeps in full (layout.md, "Collapsed and expanded blocks"), which is why it lives
// beside the painter and not beside diffBody, the producer that used to apply it.
const diffDetailCap = 20

// collapsedDetails is the collapsed paint of a retained body, split at the seam a click cares
// about: the lines the compact shape SHOWS, and the synthesized "… +N more lines" marker counting
// what it hides (truncated says whether it hides anything at all; the marker is meaningless when
// it does not). Truncation is a render-time act on facts the entry keeps whole (layout.md), so the
// marker is composed here on every repaint and never stored — which is what makes it identifiable
// as a paint artefact rather than a body line, and lets the painter mark it as its own click
// target instead of sniffing the finished lines for the wording.
//
// The split is also the toggle-target rule's oracle: truncated is exactly "the collapsed paint
// hides something", which is what makes a header clickable (blockHidesWhenCollapsed).
//
// Two flavours, told apart by the kind the body settled when its lines were paired with it
// (toolBody, toolpresent.go): a diff body — one carrying at least one tagged line, which every
// body diffBody produces does — keeps diffDetailCap lines, so a focused change reads in place; any
// other multi-line body keeps its first line alone, the gist a Run's output is worth in the chat.
// A body already inside its cap paints whole and grows no marker.
//
// The kind is READ, never re-derived here. This runs on every repaint and twice per call — the
// toggle-target rule asks it as well as the branch — over a body the entry now retains whole, so
// sniffing the lines at this seam would walk a command's whole output once a frame. It takes the
// BODY, which is why reading rather than deriving is safe: the lines and the kind that caps them
// are one value and cannot be handed in disagreeing.
//
// It is the BODY's rule, not every detail line's: the targetless shape has no body — its detail
// lines ARE the block's ┝/┕ branches (renderDetails), an unregistered tool's verbatim arguments
// among them — and hiding those would hide what the model asked for, which no block does.
func collapsedDetails(body toolBody) (shown []detailLine, remainder detailLine, truncated bool) {
	limit := 1
	if body.isDiff() {
		limit = diffDetailCap
	}
	lines := body.all()
	if len(lines) <= limit {
		return lines, detailLine{}, false
	}
	return lines[:limit], detailLine{Text: "… +" + plural(len(lines)-limit, "more line")}, true
}

// branchDetails is what a targetless call hangs off its header: the body, plus the summary as
// its last line. A targetless block has no branch line for a summary to ride, so the outcome
// simply closes the branch list — which is where an "error: …" on an unregistered tool has
// always sat, after the arguments that provoked it.
func branchDetails(tv toolView) []detailLine {
	if tv.Summary.Text == "" {
		return tv.Details.all()
	}
	out := make([]detailLine, 0, tv.Details.len()+1)
	out = append(out, tv.Details.all()...)
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
		out = append(out, hangingWrap(th, detailStyle(th, d.Kind), pad, d.Text, width)...)
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
	return tv.Target != "" && tv.Details.len() == 0 && tv.Summary.Kind == detailPlain
}

// renderOrphanResult renders a tool result that matched no pending call (a defensive
// fallback — normally a result folds into its call by CallID). It reads as a result block:
// a ✦ result header — the bare word styled like any tool label — with the raw content hanging
// off branches. It is targetless by construction, so it renders through the block renderer's
// no-target shape. The caller frames it for depth — width is already the railed inner column.
//
// It paints collapsed and stays that way: the targetless shape hides nothing (its lines are the
// branches themselves), so a stray result has no second state to show and is no toggle target —
// which is why it takes the block's lines alone and leaves its (empty) click surface behind.
func renderOrphanResult(th theme, text string, width int) []string {
	details := make([]detailLine, 0)
	for _, ln := range splitLines(text) {
		details = append(details, detailLine{Text: ln})
	}
	return renderToolBlock(th, []toolView{{Label: "result", Details: newToolBody(details)}}, width, blockState{}).lines
}

// renderDetails renders tool-detail lines as ┝/┕ tree branches (the last line gets ┕),
// styled by their kind (plain dim, or red/green for the diff kinds). This is the targetless
// shape only: where a call has a target, the target owns the branch and its details lay out
// beneath it (renderToolBranch).
func renderDetails(th theme, details []detailLine, width int) []string {
	var out []string
	for i, d := range details {
		out = append(out, hangingWrap(th, detailStyle(th, d.Kind), branchMarker(i == len(details)-1), d.Text, width)...)
	}
	return out
}

// detailStyle maps a detail kind to its style: plain detail is dim; the diff kinds are
// red/green (view_diff's body is their producer — diffBody).
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
func hangingWrap(th theme, style lipgloss.Style, marker, text string, width int) []string {
	prefixed := hangingPrefixes(th, marker, text, width)
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
func hangingPrefixes(th theme, marker, text string, width int) []string {
	mw := th.measure.Width(marker)
	indent := strings.Repeat(" ", mw)
	lines := wrapText(th, text, max(1, width-mw))
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
//
// No line it returns is wider than limit in the width authority's measure — layout.md's absolute
// cap, enforced here rather than assumed. The upstream wrap does not hold it on its own: x/ansi's
// breakpoint branch (x/ansi@v0.11.7/wrap.go:406-419) lacks the full-line checks its default branch
// has, so a run of breakpoints keeps growing a word onto an already-full line —
// ansi.Wrap("| --- | --- | --- |", 3, "") comes back with a five-cell first line, and
// ansi.Wrap("----", 3, "") with a four-cell one. Every line that comes back over the limit is
// therefore hard-broken down to it, which is also what makes the docstring's "hard-breaking any
// word longer than the limit" true rather than aspirational. The one thing no break can divide is
// a single grapheme wider than the limit — a CJK glyph at limit 1 — and that keeps a line to
// itself.
func wrapText(th theme, text string, limit int) []string {
	if limit < 1 {
		limit = 1
	}
	wrapped := strings.Split(ansi.Wrap(text, limit, ""), "\n")
	out := make([]string, 0, len(wrapped))
	for _, ln := range wrapped {
		if th.measure.Width(ln) <= limit {
			out = append(out, ln)
			continue
		}
		// preserveSpace keeps this pass purely additive — it inserts breaks and drops nothing, so
		// a segment's own leading indentation survives the cap. The break the hard wrap opens
		// ahead of an over-wide leading grapheme would otherwise surface as a blank row.
		segs := strings.Split(th.measure.Hardwrap(ln, limit, true), "\n")
		if len(segs) > 1 && segs[0] == "" {
			segs = segs[1:]
		}
		out = append(out, segs...)
	}
	return out
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

// railSpacer is the one separating line between two adjacent blocks, framed for the run the two
// of them share: depth is the JOIN of their depths (the shallower one), so the rail is drawn only
// as deep as both sides reach. Depth 0 — the flat transcript, and either side of a sub-agent run's
// boundary — is the bare "" the layout has always used, so a top-level transcript renders exactly
// as before; deeper joins draw the gutter alone, which is what makes a run's frame continuous
// through its separators instead of breaking at every block.
//
// The gutter's trailing space is trimmed BEFORE it is styled, so a spacer's visible text is "│"
// at depth 1 and "│ │" at depth 2 — never a styled trailing blank, which would leave an invisible
// SGR run hanging off the right of an otherwise empty row.
func railSpacer(th theme, depth int) string {
	if depth <= 0 {
		return ""
	}
	return th.subRail.Render(strings.TrimRight(strings.Repeat(glyphSubRail+" ", depth), " "))
}

// railLines frames a Depth-level block: it prepends one styled "│ " rail gutter per nesting
// level to each physical line, so a sub-agent's nested block reads as a vertical-ruled
// sub-section (P3.14). Depth 0 is the common case and returns the lines untouched, so the
// flat top-level transcript renders exactly as before. The rail is styled in the subRail role's
// tool-header orange and sits left of any per-line background (e.g. the user block's), matching
// the marker hanging indent.
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
// Chrome layout helpers
// ----------------------------------------------------------------------------

// inputContentRows reports how many visual rows the input value occupies at innerWidth, mirroring
// the textarea's own wrap so the box sizes to exactly what the widget draws. It is the sum over
// logical lines of wrapRowStarts' row count (inputaccent.go) — the widget's own decomposition, which
// wraps each logical line independently and adds the counts (its totalVisualLines,
// bubbles/v2@v2.1.0/textarea/textarea.go:1666-1674). Delegating means the box's HEIGHT and the rows
// the accent pass paints on are read off one ruler; they used to be two separate derivations of the
// widget's wrap, and this one was an approximation (ansi.Wordwrap + ansi.Hardwrap) that disagreed
// with the widget on roughly 41% of prompt-shaped drafts, mostly by under-counting — "hello world"
// at width 5 is four widget rows and the old count said three.
//
// The trailing row a width-filling line keeps for a caret past its last cell comes with the mirror.
// Under-counting it leaves the box one row too short at a width-fill boundary — the source of the
// prompt-box scroll artifact the layout re-seat then can no longer reach (fixed in a7afbf1; its
// regression is [TestPromptScrollClampedWhileGrowing]). An empty value is one row.
//
// The count is deliberately unclamped: [promptEditor.rows] holds it to [minInputRows, maxInputRows],
// and past that cap the widget scrolls internally rather than the box growing further.
//
// KNOWN DIVERGENCE: both mirrors are still wrong on tabs, which the widget expands. See TODO.md,
// "The TUI width authority — what it did not convert".
//
// WIDGET MIRROR — deliberately NOT the width authority. This is one of the package's mirrors of a
// third-party widget's internal math, and a mirror's oracle is the widget, never apogee's
// painter-facing measure (width.go): the textarea wraps with uniseg.StringWidth
// (bubbles/v2@v2.1.0/textarea/textarea.go:1805-1852), which is what wrapRowStarts measures with
// (runesWidth) and is grapheme-clustered, unlike ansi.WcWidth. Sizing the box in the painter's
// measure would size it to something the widget never draws. The same rule governs wrappedOffset
// below (the viewport's mirror) and the caret mirrors in inputaccent.go / mouse.go.
func inputContentRows(value string, innerWidth int) int {
	if innerWidth < 1 {
		innerWidth = 1
	}
	total := 0
	for _, line := range strings.Split(value, "\n") {
		total += len(wrapRowStarts([]rune(line), innerWidth))
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

// ----------------------------------------------------------------------------
// Prompt-offset and sticky-header arithmetic
// ----------------------------------------------------------------------------

// wrappedOffset returns the virtual (soft-wrapped) row at which the line after linesAbove
// begins — the Y offset that puts that line on the viewport's top row. refreshViewport uses it
// to size the trailing pad below a reply shorter than a screen, so following the tail still
// lands the prompt on the top row. It mirrors the viewport's calculateLine exactly: each
// physical line occupies max(1, ceil(width/vpWidth)) rows. This holds only while the viewport
// has no border or gutter (maxWidth == Width, the current wiring);
// TestWrappedOffsetMatchesViewport guards the equality against drift.
//
// WIDGET MIRROR — deliberately NOT the width authority, for the reason inputContentRows states:
// the viewport soft-wraps its own stored lines with ansi.StringWidth
// (bubbles/v2@v2.1.0/viewport/viewport.go:284, 415), so the row count that puts the prompt on the
// top row has to be counted in the viewport's measure and not in the painter's.
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
