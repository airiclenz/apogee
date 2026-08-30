package tui

// ----------------------------------------------------------------------------
// The line-oriented transcript renderer (P2.7 — TUI presentation pass)
// ----------------------------------------------------------------------------
//
// The renderer turns the transcript into the flat slice of physical lines the viewport
// shows. It returns []string (not a joined string) for two reasons: tool results carry
// embedded newlines, so the caller feeds viewport.SetContentLines without re-splitting; and
// the sticky-header overlay and the mouse both address the paint by physical line, which
// they can only do over the exact lines the viewport stores. Every element is a single
// physical line (no embedded newline), so a line index means the same thing to the painter,
// the viewport and a click.
//
// The look mirrors layout.md: the last user prompt is a full-width white-on-dark-gray block;
// the assistant and tool headers lead with ✦; a tool header carries its label — and, over a
// grouped run, its member count — with the target leading a ┝/┕ tree branch beneath it, so a
// single call and a grouped run share one grammar; one blank line separates every block. Sub-agent depth (Phase 3) indents a whole block by two
// columns per level — the tree-branch and depth-indent primitives are built here now so the
// P3.14 sub-agent renderer extends these seams rather than reworking them.

import (
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// userBlock is the line range a single user prompt occupies within the rendered lines: its
// first line index and its physical-line count. The sticky-header overlay treats each as a
// section header that freezes at the top of the viewport while its replies are on screen.
type userBlock struct{ start, count int }

// renderedTranscript is the renderer's output: the physical lines, the line range of every user
// block, and what each line is to a motionless click. The caller follows the tail unless the
// human has scrolled away, overlays the user block owning the top row as a sticky header, and
// resolves a click through targets (model.go).
type renderedTranscript struct {
	lines      []string
	userBlocks []userBlock
	// targets is PARALLEL to lines — one entry per physical line, the zero value for every line
	// that is not part of a block's click surface. It is built in lockstep with line emission by
	// the painter itself and is never re-derived by a second reader: a click resolves against the
	// exact accounting the paint used (ADR 0030's rule, one authority per measurement).
	targets []lineTarget
	// header is the sticky header the PAINT itself owns, as the lines it occupies at the top of the
	// slice: the breadcrumb row of a paint rooted at one run (transcript.setRoot), and the zero
	// value — count 0 — for the ordinary whole-transcript paint, whose sticky headers are the user
	// blocks beside it. The overlay is one mechanism either way (Model.stickyHeaderSpan): a header
	// is a content line frozen at row 0, and this only says WHICH line without asking the offset.
	header userBlock
}

// blockPaint is one painted block: its physical lines and, parallel to them, what each line is to
// a click. A block painter says WHAT each of its lines is and WHICH of its own entries owns it;
// [transcript.renderView] alone resolves that to an entry index as it lays the block into the
// transcript — which is why no painter needs to know where in the entry list it sits.
//
// The two slices are grown only through [blockPaint.add], [blockPaint.addFor] and
// [blockPaint.join], so they cannot drift out of lockstep: every line that is appended is marked in
// the same call that appends it.
type blockPaint struct {
	lines   []string
	targets []lineMark
}

// plainPaint is the paint of a block that carries no click surface at all — an assistant answer, a
// note, a start-up box. Everything the renderer emits that can
// never be toggled goes through here, so "no target" is stated once rather than spelled out at each
// producer. The two kinds that CAN be toggled — a tool block, and a prompt tall enough to collapse —
// mark their own lines as they emit them.
func plainPaint(lines []string) blockPaint {
	return blockPaint{lines: lines, targets: make([]lineMark, len(lines))}
}

// add appends lines that all carry the same target kind and belong to the block's HEAD — the shape
// every painter but the grouped one draws. A WRAPPED header is the reason it takes a slice rather
// than a line: every physical line a header occupies is part of the same click surface (layout.md —
// the click lands on the header, not on its first row), and the same holds for a remainder marker
// narrow enough to wrap.
func (p *blockPaint) add(lines []string, kind targetKind) {
	p.addFor(0, lines, kind)
}

// addFor appends lines belonging to the block's member'th entry — [blockPaint.add] with the offset
// said out loud, for the one painter whose lines do not all belong to its head (renderToolGroup).
// It exists rather than a stamping pass over finished lines because the lines and their marks have
// to be one act (ADR 0030): a second walk deriving whose row is whose would be a second accounting,
// and the two would part company the first time the member shape changed.
func (p *blockPaint) addFor(member int, lines []string, kind targetKind) {
	p.lines = append(p.lines, lines...)
	for range lines {
		p.targets = append(p.targets, lineMark{kind: kind, member: member})
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

// retargeted restates what a click on this paint's already-marked lines MEANS, leaving the lines
// and the member offsets exactly as the painter emitted them. It is the one place a block is laid
// down somewhere its painter does not know about: the run view's task row, which is the ordinary
// user block — so it folds by the ordinary rule — but whose fold is the VIEW's state and not the
// head entry's block state (transcript.setTaskExpanded, renderView's rooted opening).
//
// A line the painter left unmarked stays unmarked: this re-labels a click surface, it never grows
// one, so the lockstep [blockPaint.addFor] exists to protect is untouched — what moves is the
// meaning of a mark, which is a fact about where the block was placed rather than about how it was
// drawn.
func (p blockPaint) retargeted(kind targetKind) blockPaint {
	marks := make([]lineMark, len(p.targets))
	for i, mark := range p.targets {
		if mark.kind != targetNone {
			mark.kind = kind
		}
		marks[i] = mark
	}
	return blockPaint{lines: p.lines, targets: marks}
}

// renderView renders the committed entries plus any in-progress assistant buffer into the
// viewport's lines, recording the line range of every user block. Blocks are separated by one
// line (layout.md), railed at the depth the two blocks share so a sub-agent run's frame is
// continuous through its separators (railSpacer).
//
// The in-progress buffer is painted by the SAME rules as a committed block of its depth
// (transcript.pendingDepth): railed where it was streamed, and elided outright while it streams
// inside a collapsed run (insideCollapsedRun) — there the head already blinks and carries the live
// gist, and the status line names the delegate.
//
// blink is this frame's phase of the live star ([spinnerAnim.blink]): it reaches only the header
// glyph of a block that still holds an open call, and every other line of the transcript paints
// identically at either phase. It is a PARAMETER rather than transcript state because the phase
// belongs to the frame being drawn and not to the scrollback — the same entries painted a tick
// later are the same entries (ADR 0011: the Model is copied by value, and the renderer stays a
// pure function of what it is handed).
//
// backHint is the wording the rooted paint's breadcrumb advertises for esc, and is a PARAMETER for
// the same reason blink is: whether esc leaves the view or answers a pane standing inside it is a
// fact about the frame ([Model.backHint]), not about the scrollback. A caller with no frame hands
// the plain [breadcrumbHint]; empty paints the trail with no hint at all (breadcrumbRow).
func (t *transcript) renderView(th theme, width int, blink bool, backHint string) renderedTranscript {
	if width < 1 {
		width = 1
	}
	// What this paint covers, asked once: the whole list, or the one run a view is open on
	// ([paintRoot]). Everything below reads the answer instead of the root itself, so the bounds the
	// walk keeps and the depth its rows are rebased by cannot come to disagree.
	root := t.paintRoot()
	var lines []string
	var targets []lineTarget
	var userBlocks []userBlock
	var header userBlock

	// prevBlockDepth is the depth of the block appended last — the left half of the next spacer's
	// join. It is deliberately per-APPENDED-BLOCK rather than per-entry: an OPEN delegation's head
	// is appended at its own depth and then hands the join the depth of the span that follows it,
	// and a spacer's rail follows the blocks it actually sits between rather than the entries the
	// walk has passed.
	prevBlockDepth := 0
	// head is the index into t.entries of the block's FIRST entry — the one a click on the block
	// toggles wherever the block has a single state, and the base the painter's per-line member
	// offsets are added to where it does not (a grouped run, whose members each own their state).
	// It is spent only on the lines the block itself marked as a click surface, so a block that
	// marks none — every kind but a tool block — may pass whatever index it sits at.
	//
	// closes says what the seam above this block IS where the walk is climbing out of a delegation's
	// span: the ┊ closing that span when another grouped sub-agent follows it, and the ordinary railed
	// spacer when nothing of the list does — the spec draws no closer after a group's last member, nor
	// after a lone delegation, which has no list to resume (docs/layout/tool-layout.md, "Grouped
	// Sub-agents"). Only the block being placed knows which, so the answer rides the block
	// [transcript.resolveBlock] resolved; the streaming preview, which resumes no list, answers no.
	appendJoined := func(isUser, closes bool, depth, head int, block blockPaint) {
		if len(lines) > 0 {
			lines = append(lines, railJoin(th, prevBlockDepth, depth, closes))
			targets = append(targets, lineTarget{}) // a separator belongs to neither block
		}
		// A ROOTED paint registers none, whatever it is laying down: inside a view the breadcrumb is
		// the only sticky header there is (header, above), and a user row that claimed the slot would
		// freeze the child's task — or a message sent to it — over the child's own output.
		if isUser && !root.rooted() {
			userBlocks = append(userBlocks, userBlock{start: len(lines), count: len(block.lines)})
		}
		// The line and its mark are appended in ONE loop, and the mark is read defensively, so the
		// map is exactly as long as the lines whatever a painter handed over. That length is the
		// mouse's whole safety: a target index that outlived its line is a click toggling some
		// other block, on a path where the alternative to a rule is a panic mid-repaint.
		for i, ln := range block.lines {
			lines = append(lines, ln)
			target := lineTarget{}
			if i < len(block.targets) && block.targets[i].kind != targetNone {
				mark := block.targets[i]
				target = lineTarget{kind: mark.kind, entry: head + mark.member}
			}
			targets = append(targets, target)
		}
		prevBlockDepth = depth
	}

	// Drop memoised paints for entries the transcript no longer has (paintcache.go). It runs
	// before the loop so a render never reads a row about a block that is gone.
	t.paints.prune(len(t.entries))

	// A rooted paint opens with two things of its own before the walk lays a single entry down: the
	// breadcrumb naming the way back up, and the prompt the run was handed, painted as the user row
	// it is — a child's conversation opens with what it was asked, exactly as the human's own does.
	// Both are read off the one entry a rooted paint never paints: the head, which has become the
	// header itself (ADR 0063).
	//
	// The prompt is the head's retained task and goes through the ordinary entry painter, so a task
	// too tall for the block folds under the very rule a human's own long prompt folds under — and
	// what that rule paints is a see-more marker, which must therefore open what it counts.
	//
	// Its rows are marked for the HEAD, the one entry a rooted paint never lays down as a block, and
	// the mark is retargeted to targetTask: activating one flips the view's own fold of the task
	// (entry.taskExpanded, [transcript.setTaskExpanded]) and nothing else. It cannot be the ordinary
	// targetHeader, whose click asks the redirect first — that redirect refuses the view's own head
	// ([Model.openRunAt]) and setExpanded refuses the flag on a framed run (transcript.go), which is
	// exactly right for a row that must not open a second view of the run already on screen (ADR
	// 0063: a run has two shapes, the collapsed row and this view) and exactly wrong for a marker
	// advertising rows to unfold.
	if root.rooted() {
		head := t.entries[root.first-1]
		lines = append(lines, breadcrumbRow(th, breadcrumbTrail(t.entries, root.ref.spawn), width, backHint))
		targets = append(targets, lineTarget{kind: targetBreadcrumb})
		header = userBlock{start: 0, count: 1}
		if strings.TrimSpace(head.tool.task) != "" {
			prompt := paintInput{
				kind:       entryUser,
				text:       head.tool.task,
				entryState: entryState{expanded: head.taskExpanded},
			}
			appendJoined(false, false, 0, root.first-1,
				renderEntryLines(th, prompt, width, blink).retargeted(targetTask))
		}
	}

	// previewAt is the index the live buffer paints AT — the end of the run that filled it
	// (transcript.runEnd), which is where that run's blocks end rather than where the list does. In
	// a serial session, and for the human's own conversation, the two are the same index and the
	// preview lands exactly where it always did; while siblings run at once it lands inside the
	// child that is talking instead of after whichever child was announced last. −1 means the
	// buffer is not painted this frame at all: nothing is streaming, or the run holding it is
	// collapsed and its whole span elided with it.
	//
	// A rooted paint asks one question more, and asks it first: whether the run holding the buffer is
	// the view's own or one of its descendants (runUnder). A sibling's tokens land at an index this
	// walk never reaches, and the parent's at the end of a span the walk stops inside — either would
	// paint another agent's live sentence into this run's view.
	previewAt := -1
	if t.streaming && runUnder(t.entries, t.pendingRun, root.ref) &&
		!insideCollapsedRun(t.entries, t.pendingRun, root.ref) {
		previewAt = t.runEnd(t.pendingRun.spawn)
	}
	// paintPreview appends the in-progress buffer as a block of its own run, at index at. What it
	// paints is previewTail over the buffer's own tail — the buffer hands out only the lines the
	// preview can need (streamBuf.tail), and previewTail then holds the trailing blank lines back
	// and keeps its last previewTailLines raw lines, so a repaint costs a viewport rather than a
	// whole reply at both seams. Both cuts are display-only: the buffer itself keeps every byte. An empty buffer still renders its
	// lone marker line, so the human sees that streaming has begun.
	paintPreview := func(at int) {
		// The live buffer is painted at the depth that FILLED it (transcript.pendingRun), like every
		// committed block above: its own rail is what says which run is talking, and a delegate that
		// streams before producing any entry needs nothing else to announce the level.
		depth := t.pendingRun.depth - root.depth
		preview := renderEntryLines(th, paintInput{
			kind:  entryAssistant,
			text:  previewTail(t.pending.tail(previewTailLines)),
			depth: depth,
		}, width, blink)
		appendJoined(false, false, depth, at, preview)
	}

	for i := root.first; i < root.last; {
		// The preview is painted the moment the walk reaches its run's end. The test is >= rather
		// than == because the walk SKIPS index ranges — a collapsed run's span, a folded tool run's
		// members — and a preview whose index fell inside one would otherwise never be painted.
		if previewAt >= 0 && i >= previewAt {
			paintPreview(i)
			previewAt = -1
		}
		// The entry the walk stands on, stated once as the record the painters read ([paintInput]):
		// the resolver hands THAT to its painter and to its key, so what a block paints and what its
		// memo names are one value (paintcache.go). The entries themselves stay with the walk, which
		// is the only part that needs them — where a block ENDS is a question about the list
		// (subAgentGroupAt, subAgentSpan, toolSuperGroup, toolCallRun), never about a paint.
		in := root.painted(t.entries[i])
		// One question, asked once: what block starts here? The answer carries its shape, the records
		// it covers, its paint and the index the walk resumes at ([resolvedBlock]), so the step past a
		// folded run or a collapsed span is stated by the code that resolved that span rather than by
		// a per-branch `i += …` — the one arithmetic in the renderer whose off-by-one would silently
		// skip a block or paint it twice.
		block := t.resolveBlock(th, i, in, width, blink, root)
		key := blockKey(block.shape, block.ins, th, width, blink, block.live, root.ref)
		appendJoined(block.isUser, block.closes, in.depth, i, t.paintBlock(i, key, block.draw))
		// A block ending on an OPEN span does not end the run: that span follows, railed one level
		// deeper, and the separator between the two belongs to THAT rail rather than to the block's
		// own depth. Without it the frame would break for one blank row directly under the ┌─┶ that
		// opened it (railJoin reads this as the join's left half).
		if block.openTail {
			prevBlockDepth = in.depth + 1
		}
		i = block.next
	}
	if previewAt >= 0 {
		paintPreview(root.last)
	}
	return renderedTranscript{lines: lines, userBlocks: userBlocks, targets: targets, header: header}
}

// reserveWidgetCells holds every rendered line inside limit columns in the measure the VIEWPORT
// WIDGET spends them in, breaking the few that overrun onto rows of their own. limit is the
// viewport's OWN width — the transcript width the paint was composed to plus the right gutter
// (bodyRightGutter) — because that is the width the widget compares its lines against.
//
// It is the painter reserving the widget's extra cells, and it is here because the two measures in
// play disagree about exactly the clusters ADR 0030's Context names. The painter wraps in its own
// measure — ansi.WcWidth until a terminal answers mode 2027 — where a VARIATION SELECTOR-16 glyph
// (⚠️ ✔️ ℹ️) is ONE cell; the viewport reads its longest line with ansi.StringWidth and cuts every
// drawn row with ansi.Cut (bubbles/v2@v2.1.0 viewport.go:762, :362), both hard-wired to
// ansi.GraphemeWidth, where the same glyph is TWO. With the widget's soft wrap off (newModel) a
// line the painter had filled to the column and the widget measured wider than its width had its
// tail CUT — the trailing glyph the painter drew never reached the screen at all. Breaking the line
// here gives that glyph a row instead, and the row map stays 1:1 because the break makes it a
// stored line rather than a second row of one.
//
// The break MIRRORS the widget (ADR 0030 §6 — a mirror's oracle is the widget, never the painter):
// it asks ansi.StringWidth and cuts with ansi.Cut, the very two calls the viewport's own wrap made,
// so the painter breaks precisely where the widget would have cut and a broken row is styled the
// way that wrap styled it. And it is the LAST thing that happens to the paint, after every block is
// composed: the wrap the painter CHOSE is untouched, so a surface whose own limit is narrower than
// the frame — a table cell, a pop-up body — keeps breaking in the painter's measure exactly as
// ADR 0030 §7 decided, and only a line that would really have lost cells is re-laid. On content the
// two measures agree about (everything without a VS16 or ZWJ cluster) nothing here fires and the
// paint is returned as it came.
//
// Lines, targets and user-block spans move TOGETHER. A continuation row carries the target of the
// line it came from — every physical row a header occupies is the same click surface
// ([blockPaint.add]) — and a block's span is moved by the rows added above it and stretched by the
// rows added inside it, so the accounting the mouse reads is still the one the paint laid down.
func (r renderedTranscript) reserveWidgetCells(limit int) renderedTranscript {
	if limit < 1 || !slices.ContainsFunc(r.lines, func(ln string) bool { return overWidgetWidth(ln, limit) }) {
		return r
	}

	lines := make([]string, 0, len(r.lines)+1)
	targets := make([]lineTarget, 0, len(r.lines)+1)
	// shift[i] is how many EXTRA rows the lines before i added between them: the offset a line
	// index at i moves by, and — differenced across a span — the rows that span grew by.
	shift := make([]int, len(r.lines)+1)
	for i, ln := range r.lines {
		segs := []string{ln}
		if overWidgetWidth(ln, limit) {
			segs = dropBlankTail(splitAtWidgetWidth(ln, limit))
		}
		target := lineTarget{}
		if i < len(r.targets) {
			target = r.targets[i]
		}
		for range segs {
			targets = append(targets, target)
		}
		lines = append(lines, segs...)
		shift[i+1] = shift[i] + len(segs) - 1
	}

	// One rule for every span the paint published, the rooted paint's own header included: a span is
	// moved by the rows added above it and stretched by the rows added inside it.
	moved := func(b userBlock) userBlock {
		start := clampInt(b.start, 0, len(r.lines))
		end := clampInt(b.start+b.count, start, len(r.lines))
		return userBlock{start: b.start + shift[start], count: b.count + shift[end] - shift[start]}
	}
	blocks := make([]userBlock, len(r.userBlocks))
	for i, b := range r.userBlocks {
		blocks[i] = moved(b)
	}
	header := r.header
	if header.count > 0 {
		header = moved(header)
	}
	return renderedTranscript{lines: lines, userBlocks: blocks, targets: targets, header: header}
}

// overWidgetWidth reports whether the viewport widget would cut ln at limit columns. The BYTE
// length is asked first and settles most lines for nothing: a display cell costs at least one byte,
// so a line shorter than limit in bytes cannot be wider than limit in cells, and the escape-parsing
// scan is spent only where the answer is actually in doubt.
func overWidgetWidth(ln string, limit int) bool {
	return len(ln) > limit && ansi.StringWidth(ln) > limit
}

// dropBlankTail drops the rows a break opened for cells that show NOTHING — the trailing pad of a
// squared line (squareLine, boxdraw.go), counted in the painter's measure and therefore counted
// higher by the widget. Spending a row of the transcript on a run of spaces would put a blank line
// through the middle of a block where the widget's clip simply ended the row, and the reserve is
// here to keep GLYPHS on the screen, not padding.
func dropBlankTail(segs []string) []string {
	for len(segs) > 1 && strings.TrimSpace(ansi.Strip(segs[len(segs)-1])) == "" {
		segs = segs[:len(segs)-1]
	}
	return segs
}

// splitAtWidgetWidth cuts ln into limit-column runs in the widget's measure — the viewport's own
// soft wrap (bubbles/v2@v2.1.0 viewport.go:415-440), which is what makes the rows this returns the
// rows that widget used to draw for the same line. ansi.Cut carries the styling of the run it
// slices through onto each piece, so a broken row is coloured like the line it came from.
func splitAtWidgetWidth(ln string, limit int) []string {
	width := ansi.StringWidth(ln)
	out := make([]string, 0, width/limit+1)
	for idx := 0; idx < width; idx += limit {
		out = append(out, ansi.Cut(ln, idx, idx+limit))
	}
	return out
}

// paintRoot is the window of the transcript ONE paint covers: the entry range the walk may lay
// down, the depth every row inside it is rebased by, and the run that answers for both. The zero
// value — no run, the whole list, no rebase — is the ordinary whole-transcript paint, which is what
// keeps a session that never opens a run view painting exactly what it always did, byte for byte.
//
// It is resolved ONCE per render ([transcript.paintRoot]) and handed down from there, because its
// two halves are two readings of one entry: a paint that re-derived the head at each question could
// bound the walk by one run and rebase its rows by another.
//
// Rebasing is the whole of what makes a view a view. Nothing is rewritten and no entry moves: the
// records the painters read are handed their depth less the root's, so a child's blocks paint as
// top-level rows — no rail, wrapped to the full column — while the entries themselves still say
// exactly where they sit in the conversation.
type paintRoot struct {
	ref   runRef // the run the paint is rooted at, as the paint key names it (paintcache.go)
	first int    // the first entry the walk may paint: the root's head is NOT painted, being the header
	last  int    // one past the last — the end of the root's span, or of the entry list
	depth int    // the root run's own nesting level: what every painted row's depth is rebased by
}

// rooted reports whether this paint covers ONE run rather than the whole transcript.
func (r paintRoot) rooted() bool { return r.ref.spawn != "" }

// painted states one entry as its painter's record ([entry.painted]), rebased to the root.
func (r paintRoot) painted(e entry) paintInput {
	in := e.painted()
	in.depth -= r.depth
	return in
}

// inputs states a whole block's entries as painter records ([paintInputs]), rebased to the root.
func (r paintRoot) inputs(entries []entry) []paintInput {
	return r.rebase(paintInputs(entries))
}

// rebase rewrites a block's painter records to the root's level, in place — the records are built
// fresh for the block being resolved ([paintInputs], toolCallRun), never shared with the entries
// they were read off. It is the one place the rebase reaches a multi-entry block, so a block's head
// and its span can never be painted at levels that disagree, and it is what the paint key reads:
// the records the rows are drawn from are the records the key names (paintcache.go).
func (r paintRoot) rebase(ins []paintInput) []paintInput {
	for i := range ins {
		ins[i].depth -= r.depth
	}
	return ins
}

// paintRoot resolves the window this paint covers from the run the transcript is rooted at
// ([transcript.setRoot]): the head that run hangs off, the span behind it, and the level that span
// stands at.
//
// A root whose head the list no longer holds — a run whose entries a /clear or a session switch took
// away — resolves to the WHOLE transcript rather than to an empty view: the Model pops such a view
// the moment it notices, and a frame painted before that lands shows the conversation instead of a
// blank screen.
func (t *transcript) paintRoot() paintRoot {
	whole := paintRoot{last: len(t.entries)}
	if t.root.spawn == "" {
		return whole
	}
	at, ok := runHeadAt(t.entries, t.root.spawn)
	if !ok {
		return whole
	}
	return paintRoot{
		ref:   t.root,
		first: at + 1,
		last:  at + 1 + subAgentSpan(t.entries, at),
		// The HEAD answers for the run's depth, not the ref the caller handed in: the two agree by
		// construction — a run's entries stand one level below the call that opened them
		// (transcript.closeRun builds its ref from exactly this) — and the head is the half this
		// paint has in front of it.
		depth: t.entries[at].depth + 1,
	}
}

// resolvedBlock is the answer to "what block starts at this entry?": everything
// [transcript.renderView] needs to lay one block down and step past it, resolved together — the
// shape that draws it, the records that shape covers, the paint itself, and where the walk resumes.
//
// The last of those is why the record exists. The advancement used to be hand-written inside each
// branch of the shape chain (`i += span`, `i += calls-1`, `i = grp[end].at`), which made the walk's
// one silent failure — skipping a block, or painting it twice — something five separate lines could
// cause. Here the span a shape resolved and the step past it are stated in the same breath, by the
// code that resolved the span.
type resolvedBlock struct {
	shape blockShape        // which painter draws it, as the paint key names the branch (paintcache.go)
	ins   []paintInput      // the records the key names and the painter reads, the block's head first
	live  bool              // whether the block still holds an open call — each shape's own rule (blockState.live)
	draw  func() blockPaint // the paint, called only when the cache misses (transcript.paintBlock)

	next   int  // where the walk resumes: past the block's own entries, and past a collapsed span's elided ones
	isUser bool // the block is a user prompt, and so a sticky-header section (userBlock)
	closes bool // the seam ABOVE this block closes a delegation's span with a ┊ (appendJoined, railJoin)
	// openTail says the block ends on an OPEN span the walk is about to step INTO: that span is
	// railed one level deeper than the block's own depth, so the seam below joins from there rather
	// than from the depth this block stood at.
	openTail bool
}

// resolveBlock answers what block starts at entry index head, whose painter record is in. It is the
// renderer's ONE block-shape decision, and the only place a shape's span is turned into a step: the
// folded shapes are asked for in the order their containment demands — a grouped delegation list
// before its members, an umbrella before the same-label runs inside it — and an entry heading none
// of them is a block of its own.
//
// It reads the entry LIST, because where a block ends is a question about the list (subAgentGroupAt,
// subAgentSpan, toolSuperGroup, toolCallRun); everything it hands back speaks in paint records
// instead, which is what keeps a painter to what it needs to draw and nothing it could write through
// (paintcache.go, ADR 0011).
func (t *transcript) resolveBlock(th theme, head int, in paintInput, width int, blink bool, root paintRoot) resolvedBlock {
	// A descent used to be announced by a label block of its own. Nothing announces it now: the
	// delegation's own header row opens the frame with ┌─┶ and the rail runs down the span from there
	// (docs/layout/tool-layout.md, "Grouped Sub-agents"), so a label saying the same thing one row
	// lower was the run introducing itself twice.
	//
	// Adjacent delegations fold into ONE "✦ Sub-Agent (N)" list (subAgentGroupAt), asked first
	// because every member of one is also a run head and would otherwise resolve as a block of its
	// own below. The question is asked at every member and not only at the first: an OPEN member's
	// span is painted by the same walk, so the list resumes in a second block of the same shape after
	// it, headerless, its rows still counted against the whole group.
	//
	// The block covers the members whose rows it paints and stops at the first open one, resuming the
	// walk ON that member so it steps into its span exactly as it does under a lone expanded
	// delegation. A collapsed member's span is skipped whole, by the rule below.
	if grp, pos, ok := subAgentGroupAt(t.entries, head); ok {
		end := len(grp) - 1
		for k := pos; k < len(grp); k++ {
			if t.entries[grp[k].at].expanded {
				end = k
				break
			}
		}
		// The key covers this block's members and everything still ahead of them in the group:
		// the header's star asks the whole list whether any delegate is still working, and a
		// member's row changes shape the moment its delegation grows a span to reveal, so a key
		// stopping at the last row it paints would serve a stale one (paintcache.go).
		tail := grp[len(grp)-1]
		cover := tail.at + 1 + tail.span - head
		// One record per covered entry, stated once and read by both the key and the rows: a
		// member's own record sits at its offset from the head and its span is the records behind
		// it, so what the paint reads is exactly what the key named (paintcache.go).
		ins := root.inputs(t.entries[head : head+cover])
		members := make([]subAgentMember, 0, end-pos+1)
		for k := pos; k <= end; k++ {
			at := grp[k].at - head
			members = append(members, subAgentMember{
				head:   ins[at],
				span:   ins[at+1 : at+1+grp[k].span],
				offset: at,
				last:   k == len(grp)-1,
			})
		}
		// count opens the header, and only the group's FIRST block carries one.
		count := 0
		if pos == 0 {
			count = len(grp)
		}
		live := anyOpenCall(ins)
		// The walk resumes ON the member the block stopped at when that member is open — its span
		// follows as blocks of its own — and past that member's whole span when it is collapsed,
		// which is what elides it.
		open := t.entries[grp[end].at].expanded
		next := grp[end].at + 1
		if !open {
			next += grp[end].span
		}
		return resolvedBlock{
			shape: shapeSubAgentGroup,
			ins:   ins,
			live:  live,
			draw: func() blockPaint {
				return renderSubAgentGroup(th, count, members, railedWidth(width, in.depth),
					blockState{live: live, blink: blink}).railed(th, in.depth)
			},
			next: next,
			// pos > 0 is this block RESUMING a list whose earlier rows an expanded member's span
			// interrupted — precisely the spec's "another grouped sub-agent follows the expanded one",
			// and so the one seam in the whole transcript that closes with a ┊.
			closes:   pos > 0,
			openTail: open,
		}
	}
	// A sub-agent run is ONE block, always (layout.md, ADR 0063): its head paints with the
	// cascading summary and the whole span is then skipped outright, which is what elides the
	// inner blocks and every rail and spacer among them — nothing is painted and afterwards
	// taken back. There is no second shape to walk into here. Expanding a delegation opens its
	// RUN VIEW (runview.go), and a view paints the span as a transcript rooted at it, through
	// this same walk with its own bounds — so an inner block keeps its own state there as it
	// always did, and a nested run collapses inside its parent's view by this very rule, at
	// every depth.
	//
	// The paint covers the head AND its span: the collapsed summary counts the work behind
	// the header (subAgentSummary) and the star asks the span whether anything is still open,
	// so a nested entry arriving or landing its result is a different block (paintcache.go).
	//
	// A delegation reaches this branch with a span of nothing where a reader opened it before its
	// first entry landed (subAgentFramed): the row is the same row either way, and the streaming
	// tail behind it is elided with the rest of the run (insideCollapsedRun). A run reaching this
	// branch closes with no ┊ at all: the closer belongs to a list resuming after one of its
	// members, and a delegation standing here stands alone.
	if span := subAgentSpan(t.entries, head); subAgentFramed(in, span) {
		ins := root.inputs(t.entries[head : head+span+1])
		return resolvedBlock{
			shape: shapeSubAgentRun,
			ins:   ins,
			live:  !subAgentReported(in) || anyOpenCall(ins[1:]),
			draw: func() blockPaint {
				return renderSubAgentRun(th, ins[0], ins[1:], width, blink)
			},
			next: head + span + 1, // the span is elided whole: it is read in the run's own view
		}
	}
	// Adjacent runs of DIFFERENT tools fold under one umbrella (toolSuperGroup, item 5), which
	// is asked before the same-label run because a same-label run inside one is a row of it rather
	// than a block of its own. The question is only ever asked at a block head — the walk's index is
	// one by construction, since every shape either advances by a single entry or steps over a whole
	// block — and toolSuperGroup is only correct there: asked mid-run it would answer with a
	// partial first run.
	//
	// The umbrella covers its calls and nothing else (superGroup.calls), so the walk steps
	// over exactly them. Its per-entry state is in the paint key already: blockKey spans the
	// whole umbrella and spanFlags packs both levels — a member's expanded at bit 0 and a run
	// head's typeExpanded at bit 2 — so opening either level is a different key and a fresh
	// paint (paintcache.go).
	if sup := toolSuperGroup(t.entries, head); len(sup) > 0 {
		calls := sup.calls()
		ins := root.inputs(t.entries[head : head+calls])
		live := anyOpenCall(ins)
		return resolvedBlock{
			shape: shapeToolSuper,
			ins:   ins,
			live:  live,
			draw: func() blockPaint {
				return renderSuperGroup(th, superRunViews(ins, sup), railedWidth(width, in.depth),
					blockState{live: live, blink: blink}).railed(th, in.depth)
			},
			next: head + calls,
		}
	}
	// Consecutive same-label tool calls fold into one block at render time, so a batch of
	// reads is one header plus one leader row per file. The entry list is untouched: a
	// call that arrives mid-stream joins its group on the next repaint for free, and a run
	// is same-depth by construction, so the label logic above fires exactly as before.
	//
	// The group's liveness is the group's, not its head's: a batch of reads whose first call
	// has landed and whose last has not is still working, and the one star over them all says
	// so. The run is entries[head:head+len(run)] by construction (toolCallRun walks adjacent
	// entries forward), so the views' own entries are what the rule reads — and the same
	// construction is what lets the members' EXPANDED flags be read off the run in view order
	// and their rows be marked back by offset (blockPaint.addFor).
	//
	// Every one of those flags is in the paint key already: blockKey spans the whole run and
	// spanFlags packs expanded at bit 0 of each covered entry, so opening the tenth member of a
	// group is a different key and a fresh paint (paintcache.go).
	if run := root.rebase(toolCallRun(t.entries, head)); len(run) > 1 {
		live := anyOpenCall(run)
		return resolvedBlock{
			shape: shapeToolRun,
			ins:   run,
			live:  live,
			draw: func() blockPaint {
				return renderToolBlock(th, toolViews(run), railedWidth(width, in.depth), blockState{
					expanded: in.expanded,
					live:     live,
					blink:    blink,
					members:  memberFlags(run),
				}).railed(th, in.depth)
			},
			next: head + len(run),
		}
	}
	// One entry, one block. Which kinds can still be waiting, and which head a prompt stop, are the
	// kind's own answers (entrykind.go); everything else keys as settled and marks no stop.
	live := in.kind.hasLiveStar() && !in.done
	return resolvedBlock{
		shape: shapeEntry,
		ins:   []paintInput{in},
		live:  live,
		draw: func() blockPaint {
			return renderEntryLines(th, in, width, blink)
		},
		next: head + 1,
		// A prompt stop, but only the human's OWN: a message addressed to a running sub-agent is
		// an entryUser too (transcript.addUserAt), and it is the depth that parts the two. The
		// prompt the on-screen work belongs to is the top-level one whatever a delegate is being
		// told, so a delegated user block is drawn like a prompt and walked past like a delegate's
		// entry — ctrl+↑/↓ offer stops the reader actually started a turn at.
		isUser: in.kind.isUserPrompt() && in.depth == 0,
	}
}

// renderLines is the line slice alone — the viewport content and the substring-test surface — at
// the star's SETTLED phase: the blink is a fact about the frame being drawn, and a caller that has
// no frame (a width probe, a substring assertion) has no phase either.
func (t *transcript) renderLines(th theme, width int) []string {
	return t.renderView(th, width, false, breadcrumbHint).lines
}

// previewTailLines is how much of the in-flight buffer the streaming preview renders: its last 256
// raw (newline-delimited) lines, never the whole of it. The live buffer is the one block the paint
// cache cannot serve — the cache is keyed by entry index (paintcache.go) and the buffer is not an
// entry — so it is re-rendered on EVERY repaint, and repaints fire per 30 ms sink flush (sink.go)
// and at 2 Hz while a tool call is open (model.go, spinner.go). Rendering the whole buffer each
// time is therefore O(len(pending)) per repaint and O(N²) over a streaming turn: measured 95% CPU
// and a 0.48 s click round-trip after 180 s of streaming, against a flat 0.05–0.07 s once the same
// reply is committed and served from the cache. The preview contributes at most one viewport of
// rows at the bottom of the frame, so everything above the tail was being wrapped, styled and then
// thrown away.
//
// 256 is a bound, not a measurement — the render seam is handed a width and no height — and it is
// deliberately at least double any realistic terminal: every raw line renders to one or more screen
// rows, so 256 raw lines can never underfill a 256-row window, and the markdown constructs that
// JOIN source lines (a wrapped paragraph) are covered by the same doubling.
const previewTailLines = 256

// previewTail is the text the streaming preview paints for the in-flight buffer's tail s: its
// trailing blank lines held back, and then only its last previewTailLines raw lines. s is already a
// tail — [streamBuf.tail] hands it the last previewTailLines+1 lines rather than the whole reply,
// so the scan below costs a viewport, which is what this bound has always promised. The trim is
// trimTrailingBlankLines' rule and holds for its reason — a mid-stream "\n\n" may be a paragraph
// break the model is about to continue, so the buffer keeps it while the preview must not grow a
// wobbling gap above the footer. Both cuts are display-only; s itself is never touched.
//
// It scans backwards for the newlines it needs instead of splitting s, because a split is itself
// O(len(s)) in allocations on every repaint — part of the very cost this bound exists to remove.
//
// Accepted trade: a markdown construct opened ABOVE the cut (an unclosed code fence, a list) can
// render unstyled in the tail. The preview is transient and mid-stream markdown is best-effort
// already; the committed entry re-renders the full text through the cache and heals it, so no
// fence scanning or state carry-over is warranted here.
func previewTail(s string) string {
	end := len(s) // one past the last byte the preview shows
	for {
		start := strings.LastIndexByte(s[:end], '\n') + 1
		if !blankLine(s[start:end]) {
			break
		}
		if start == 0 {
			return "" // the whole buffer is blank lines
		}
		end = start - 1 // drop the blank line and the newline that opened it
	}
	cut := end
	for range previewTailLines {
		nl := strings.LastIndexByte(s[:cut], '\n')
		if nl < 0 {
			return s[:end] // fewer raw lines than the bound: the trimmed buffer whole
		}
		cut = nl
	}
	return s[cut+1 : end]
}

// renderEntryLines renders one committed entry — stated as the record of what a painter may read of
// it ([paintInput]) — into its physical lines, framed for its sub-agent depth, plus what each of
// those lines is to a click. The user prompt is a full-width block; everything else hangs off a
// marker. A Depth > 0 entry is wrapped to the narrower column left of its rail gutter, then each
// line is prefixed by the rail (P3.14) so the nested block reads as a framed sub-section.
//
// Four kinds carry a click surface, for the one reason: they have two states to toggle between. A
// tool call, a scheduled Firing and a stray result share ONE block painter and so one click
// surface: every row of a block that hides something, with the remainder marker keeping its
// open-only meaning and, inside a folded run, each member's rows marked for that member. A prompt
// tall enough to collapse marks every row it paints too, and always has — the whole block is the
// toggle in both shapes (layout.md, "Collapsed and expanded blocks"). Every other kind comes back as plainPaint: a note or an answer paints one way
// whatever is asked of it, and a click there keeps its selection meaning. blink narrows further
// still — a tool call is the only entry that can still be WAITING for something, so it is the only
// header with a star to blink (layout.md, "The live star").
func renderEntryLines(th theme, in paintInput, width int, blink bool) blockPaint {
	inner := railedWidth(width, in.depth)
	switch in.kind {
	case entryUser:
		return renderUserBlock(th, glyphUser+" ", in, inner).railed(th, in.depth)
	case entryInterjected:
		// The human's mid-Exchange remark: the same block the prompt gets — it is the same voice —
		// under the ⧖ marker that says it arrived while the model was already working (ADR 0025).
		// Its skill tokens light up the sent block's way, for the same reason: a skill rides an
		// interjection (ADR 0027), and the record of what the model was given must not depend on
		// whether the remark was delivered mid-run or flushed into a new Exchange.
		return renderUserBlock(th, glyphInterject+" ", in, inner).railed(th, in.depth)
	case entryAssistant:
		marker := glyphAssistant + " "
		body := renderMarkdownBody(th, in.text, inner-th.measure.Width(marker))
		return plainPaint(railLines(th, withMarker(th, marker, body), in.depth))
	case entryToolCall:
		// A delegation that is OVER with nothing behind it reaches this branch as the ordinary tool
		// block it is, and the prompt it carried has nowhere else to go: expanded, it opens the
		// block's body (unframedSubAgentView). The span the question turns on is 0 by construction
		// here — resolveBlock frames every delegation that has one and steps into it, so none ever
		// reaches this case — which is what lets the predicate still be asked through
		// [subAgentFramed] rather than reworded into a second rule about what a run is.
		view := in.tool
		if in.headsRun() && in.expanded && !subAgentFramed(in, 0) {
			view = unframedSubAgentView(in)
		}
		return renderToolBlock(th, []toolView{view}, inner, blockState{
			expanded: in.expanded,
			live:     !in.done,
			blink:    blink,
		}).railed(th, in.depth)
	case entrySchedule:
		// A Firing wears the tool block's shape under the /sessions tag's ⟳ (layout.md, "The firing
		// block"), so one Firing reads the same in the chat and in the browser. live and blink stay
		// false BY CONSTRUCTION rather than by accident: the spinner belongs to the worker driving
		// this session's Exchange and the session is idle while a Firing runs, so an animated header
		// here would claim work is happening in this session. What says the run is going is the
		// block's own static summary (schedule.go, scheduleRunningSummary).
		return renderToolBlock(th, []toolView{in.tool}, inner, blockState{
			expanded: in.expanded,
			glyph:    scheduleTagGlyph,
		}).railed(th, in.depth)
	case entryToolResult:
		return renderOrphanResult(th, in.text, inner, in.expanded).railed(th, in.depth)
	case entryError:
		return plainPaint(railLines(th, hangingWrap(th, th.errorText, glyphAssistant+" ", in.text, inner), in.depth))
	case entryNote:
		return plainPaint(railLines(th, hangingWrap(th, th.noteText, "· ", in.text, inner), in.depth))
	case entryPresented:
		return plainPaint(railLines(th, renderPresentedBlock(th, in.presented, inner), in.depth))
	case entryStartup:
		return plainPaint(railLines(th, renderStartupBox(th, in.startup, inner), in.depth))
	default:
		return blockPaint{}
	}
}
