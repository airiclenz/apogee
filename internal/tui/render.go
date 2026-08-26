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

import "strings"

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
func (t *transcript) renderView(th theme, width int, blink bool) renderedTranscript {
	if width < 1 {
		width = 1
	}
	var lines []string
	var targets []lineTarget
	var userBlocks []userBlock

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
		if isUser {
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

	// previewAt is the index the live buffer paints AT — the end of the run that filled it
	// (transcript.runEnd), which is where that run's blocks end rather than where the list does. In
	// a serial session, and for the human's own conversation, the two are the same index and the
	// preview lands exactly where it always did; while siblings run at once it lands inside the
	// child that is talking instead of after whichever child was announced last. −1 means the
	// buffer is not painted this frame at all: nothing is streaming, or the run holding it is
	// collapsed and its whole span elided with it.
	previewAt := -1
	if t.streaming && !insideCollapsedRun(t.entries, t.pendingRun) {
		previewAt = t.runEnd(t.pendingRun.spawn)
	}
	// paintPreview appends the in-progress buffer as a block of its own run, at index at. What it
	// paints is previewTail(t.pending) — the buffer's trailing blank lines held back, and only its
	// last previewTailLines raw lines kept, so a repaint costs a viewport rather than a whole reply.
	// Both are display-only: the buffer itself keeps every byte. An empty buffer still renders its
	// lone marker line, so the human sees that streaming has begun.
	paintPreview := func(at int) {
		// The live buffer is painted at the depth that FILLED it (transcript.pendingRun), like every
		// committed block above: its own rail is what says which run is talking, and a delegate that
		// streams before producing any entry needs nothing else to announce the level.
		preview := renderEntryLines(th, paintInput{
			kind:  entryAssistant,
			text:  previewTail(t.pending),
			depth: t.pendingRun.depth,
		}, width, blink)
		appendJoined(false, false, t.pendingRun.depth, at, preview)
	}

	for i := 0; i < len(t.entries); {
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
		in := t.entries[i].painted()
		// One question, asked once: what block starts here? The answer carries its shape, the records
		// it covers, its paint and the index the walk resumes at ([resolvedBlock]), so the step past a
		// folded run or a collapsed span is stated by the code that resolved that span rather than by
		// a per-branch `i += …` — the one arithmetic in the renderer whose off-by-one would silently
		// skip a block or paint it twice.
		block := t.resolveBlock(th, i, in, width, blink)
		key := blockKey(block.shape, block.ins, th, width, blink, block.live)
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
		paintPreview(len(t.entries))
	}
	return renderedTranscript{lines: lines, userBlocks: userBlocks, targets: targets}
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
func (t *transcript) resolveBlock(th theme, head int, in paintInput, width int, blink bool) resolvedBlock {
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
		ins := paintInputs(t.entries[head : head+cover])
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
	// A sub-agent run is ONE block while it is collapsed (layout.md): its head paints with the
	// cascading summary and the whole span is then skipped outright, which is what elides the
	// inner blocks and every rail and spacer among them — nothing is painted and afterwards
	// taken back. Expanded, only the head is painted here — in the ┌─┶ frame a grouped
	// delegation's open row opens (renderSubAgentRun, design call 3) — and the walk steps into
	// the span exactly as it always has, so every inner block keeps its OWN state and a nested
	// run collapses inside an expanded parent by this same rule, at every depth.
	// The paint covers the head AND its span: the collapsed summary counts the work behind
	// the header (subAgentSummary) and the star asks the span whether anything is still open,
	// so a nested entry arriving or landing its result is a different block (paintcache.go).
	//
	// An OPEN delegation reaches this branch with a span of nothing (subAgentFramed): its
	// frame is drawn live, and openTail hands the seam below the level the frame stands at,
	// which is what lays the streaming preview inside the rail rather than flat beside it
	// (design call 4). A run reaching this branch closes with no ┊ at all: the closer belongs to
	// a list resuming after one of its members, and a delegation standing here stands alone.
	if span := subAgentSpan(t.entries, head); subAgentFramed(in, span) {
		ins := paintInputs(t.entries[head : head+span+1])
		live := !subAgentReported(in) || anyOpenCall(ins[1:])
		next := head + 1
		if !in.expanded {
			next += span // a collapsed run's span is elided whole; an open one's is walked into
		}
		return resolvedBlock{
			shape: shapeSubAgentRun,
			ins:   ins,
			live:  live,
			draw: func() blockPaint {
				return renderSubAgentRun(th, ins[0], ins[1:], width, blink)
			},
			next:     next,
			openTail: in.expanded,
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
		ins := paintInputs(t.entries[head : head+calls])
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
	if run := toolCallRun(t.entries, head); len(run) > 1 {
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
		next:   head + 1,
		isUser: in.kind.isUserPrompt(),
	}
}

// renderLines is the line slice alone — the viewport content and the substring-test surface — at
// the star's SETTLED phase: the blink is a fact about the frame being drawn, and a caller that has
// no frame (a width probe, a substring assertion) has no phase either.
func (t *transcript) renderLines(th theme, width int) []string {
	return t.renderView(th, width, false).lines
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

// previewTail is the text the streaming preview paints for the in-flight buffer s: its trailing
// blank lines held back, and then only its last previewTailLines raw lines. The trim is
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
