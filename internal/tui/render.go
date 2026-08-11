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

// targetKind says what one rendered line is to a motionless click (layout.md, "Collapsed and
// expanded blocks"): nothing at all — the overwhelmingly common case, and the zero value — or a
// TOGGLE line, which flips the state of the entry the mark names. Every line a block paints carries
// the one meaning: the collapsed paint's "+N more lines" used to be a line of its own with an
// open-only click, and it now rides the leader row's outcome slot instead (collapsedRemainder), so
// there is no longer a row whose click could only ever open.
//
// targetHeader is named for the line it started on and is no longer only that line: a single tool
// block wears it on EVERY row it paints — its header, its leader row, its body — and a grouped
// block's MEMBER rows wear it too, each naming its own call rather than the block's head, which is
// how a group of ten opens one of them (renderToolBlock, renderToolGroup). What the kind means has
// not moved: it is the toggle, whatever line it lands on.
//
// A super-group adds the two kinds its extra LEVEL needs (renderSuperGroup). targetType is a type
// row: it toggles the run's own second state (transcript.toggleTypeExpanded) rather than the
// expanded flag every other target flips, which is what lets a reader open a run to its member rows
// and then open a member to its body. targetUmbrella is the umbrella header, whose click is not a
// toggle at all — its floor is the type rows, so it never folds to one line and instead closes every
// open child beneath it (design call 9, transcript.closeSuperGroup).
type targetKind int

const (
	targetNone targetKind = iota
	targetHeader
	targetType
	targetUmbrella
)

// lineTarget is one rendered line's click surface: what the line is, and the index into
// transcript.entries of the entry whose expanded state a click there flips
// (transcript.toggleExpanded) — the block's head for every shape but a grouped run, where it is the
// member the row belongs to. The zero value is "no target", which is what every line outside a
// toggleable block carries, so a lookup needs no second sentinel.
type lineTarget struct {
	kind  targetKind
	entry int
}

// lineMark is what one painted line is to a click as the block's OWN painter states it: the kind,
// and which of the block's entries a click there flips, said as an OFFSET from the block's head. A
// single block marks everything 0 — it has one entry and the head is it — and a grouped block marks
// each member row with the member's index, which is that call's offset by construction
// (toolCallRun walks adjacent entries forward, so views[n] is entries[head+n]).
//
// The offset is relative for the reason the kinds carry no entry index at all: a painter knows the
// shape it is drawing and not where in the scrollback it sits, and [transcript.renderView] alone
// turns the pair into an absolute entry. The zero value is "the head, no target", which is what
// every line outside a click surface carries.
type lineMark struct {
	kind   targetKind
	member int
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
	appendBlock := func(isUser bool, depth, head int, block blockPaint) {
		if len(lines) > 0 {
			// The join is a REGION and not always one line: climbing out of a run closes its frame
			// with a ┊ per level left behind (railJoin), and those rows stand exactly where the one
			// separator would have.
			for _, sep := range railJoin(th, prevBlockDepth, depth) {
				lines = append(lines, sep)
				targets = append(targets, lineTarget{}) // a separator belongs to neither block
			}
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
	// paintPreview appends the in-progress buffer as a block of its own run, at index at. The
	// buffer is trimmed of its trailing blank lines for display only: the buffer keeps them (a
	// mid-stream "\n\n" may be a paragraph break about to be continued), but the preview must not
	// grow a wobbling gap above the footer. An empty buffer still renders its lone marker line, so
	// the human sees that streaming has begun.
	paintPreview := func(at int) {
		// The live buffer is painted at the depth that FILLED it (transcript.pendingRun), like every
		// committed block above: its own rail is what says which run is talking, and a delegate that
		// streams before producing any entry needs nothing else to announce the level.
		preview := renderEntryLines(th, entry{
			kind:  entryAssistant,
			text:  trimTrailingBlankLines(t.pending),
			depth: t.pendingRun.depth,
		}, width, blink)
		appendBlock(false, t.pendingRun.depth, at, preview)
	}

	for i := 0; i < len(t.entries); i++ {
		// The preview is painted the moment the walk reaches its run's end. The test is >= rather
		// than == because the walk SKIPS index ranges — a collapsed run's span, a folded tool run's
		// members — and a preview whose index fell inside one would otherwise never be painted.
		if previewAt >= 0 && i >= previewAt {
			paintPreview(i)
			previewAt = -1
		}
		e := t.entries[i]
		// A descent used to be announced here by a label block of its own. Nothing announces it now:
		// the delegation's own header row opens the frame with ┌─┶ and the rail runs down the span
		// from there (docs/layout/tool-layout.md, "Grouped Sub-agents"), so a label saying the same
		// thing one row lower was the run introducing itself twice.
		//
		// Adjacent delegations fold into ONE "✦ Sub-Agent (N)" list (subAgentGroupAt), asked first
		// because every member of one is also a run head and would otherwise take the branch below
		// as a block of its own. The question is asked at every member and not only at the first:
		// an OPEN member's span is painted by this same walk, so the list resumes in a second block
		// of the same shape after it, headerless, its rows still counted against the whole group.
		//
		// The walk covers the members whose rows this block paints and stops at the first open one,
		// leaving i on that member so the loop steps into its span exactly as it does under a lone
		// expanded delegation. A collapsed member's span is skipped whole, by the rule below.
		if grp, pos, ok := subAgentGroupAt(t.entries, i); ok {
			end := len(grp) - 1
			for k := pos; k < len(grp); k++ {
				if t.entries[grp[k].at].expanded {
					end = k
					break
				}
			}
			members := make([]subAgentMember, 0, end-pos+1)
			for k := pos; k <= end; k++ {
				at := grp[k].at
				members = append(members, subAgentMember{
					head:   t.entries[at],
					span:   t.entries[at+1 : at+1+grp[k].span],
					offset: at - i,
					last:   k == len(grp)-1,
				})
			}
			// count opens the header, and only the group's FIRST block carries one.
			count := 0
			if pos == 0 {
				count = len(grp)
			}
			// The key covers this block's members and everything still ahead of them in the group:
			// the header's star asks the whole list whether any delegate is still working, and a
			// member's row changes shape the moment its delegation grows a span to reveal, so a key
			// stopping at the last row it paints would serve a stale one (paintcache.go).
			tail := grp[len(grp)-1]
			cover := tail.at + 1 + tail.span - i
			key := t.blockKey(shapeSubAgentGroup, i, cover, th, width, blink,
				anyOpenCall(t.entries[i:i+cover]))
			appendBlock(false, e.depth, i, t.paintBlock(i, key, func() blockPaint {
				return renderSubAgentGroup(th, count, members, railedWidth(width, e.depth),
					blockState{live: key.live, blink: blink}).railed(th, e.depth)
			}))
			// A block ending on an OPEN member does not end the run: that member's span follows,
			// railed one level deeper, and the separator between the two belongs to THAT rail
			// rather than to the group's own depth. Without it the frame would break for one blank
			// row directly under the ┌─┶ that opened it (railJoin reads this as the join's left
			// half).
			if i = grp[end].at; !t.entries[i].expanded {
				i += grp[end].span
			} else {
				prevBlockDepth = e.depth + 1
			}
		} else if span := subAgentSpan(t.entries, i); span > 0 {
			// A sub-agent run is ONE block while it is collapsed (layout.md): its head paints with the
			// cascading summary and the whole span is then skipped outright, which is what elides the
			// inner blocks and every rail and spacer among them — nothing is painted and afterwards
			// taken back. Expanded, only the head is painted here — in the ┌─┶ frame a grouped
			// delegation's open row opens (renderSubAgentRun, design call 3) — and the loop walks into
			// the span exactly as it always has, so every inner block keeps its OWN state and a nested
			// run collapses inside an expanded parent by this same rule, at every depth.
			// The paint covers the head AND its span: the collapsed summary counts the work behind
			// the header (subAgentSummary) and the star asks the span whether anything is still open,
			// so a nested entry arriving or landing its result is a different block (paintcache.go).
			key := t.blockKey(shapeSubAgentRun, i, span+1, th, width, blink,
				!e.done || anyOpenCall(t.entries[i+1:i+1+span]))
			appendBlock(false, e.depth, i, t.paintBlock(i, key, func() blockPaint {
				return renderSubAgentRun(th, e, t.entries[i+1:i+1+span], width, blink)
			}))
			if !e.expanded {
				i += span
			} else {
				prevBlockDepth = e.depth + 1 // the head's span continues the rail beneath it
			}
		} else if sup := toolSuperGroup(t.entries, i); len(sup) > 0 {
			// Adjacent runs of DIFFERENT tools fold under one umbrella (toolSuperGroup, item 5), which
			// is asked FIRST because a same-label run inside one is a row of it rather than a block of
			// its own. The question is only ever asked here, at a block head — the loop's index is one
			// by construction, since every branch either advances by a single entry or skips a whole
			// block — and toolSuperGroup is only correct there: asked mid-run it would answer with a
			// partial first run.
			//
			// The umbrella covers its calls and nothing else (superGroup.calls), so the walk steps
			// over exactly them. Its per-entry state is in the paint key already: blockKey spans the
			// whole umbrella and spanFlags packs both levels — a member's expanded at bit 0 and a run
			// head's typeExpanded at bit 2 — so opening either level is a different key and a fresh
			// paint (paintcache.go).
			calls := sup.calls()
			key := t.blockKey(shapeToolSuper, i, calls, th, width, blink,
				anyOpenCall(t.entries[i:i+calls]))
			appendBlock(false, e.depth, i, t.paintBlock(i, key, func() blockPaint {
				return renderSuperGroup(th, superRunViews(t.entries, sup), railedWidth(width, e.depth),
					blockState{live: key.live, blink: blink}).railed(th, e.depth)
			}))
			i += calls - 1
		} else if run := toolCallRun(t.entries, i); len(run) > 1 {
			// Consecutive same-label tool calls fold into one block at render time, so a batch of
			// reads is one header plus one leader row per file. The entry list is untouched: a
			// call that arrives mid-stream joins its group on the next repaint for free, and a run
			// is same-depth by construction, so the label logic above fires exactly as before.
			//
			// The group's liveness is the group's, not its head's: a batch of reads whose first call
			// has landed and whose last has not is still working, and the one star over them all says
			// so. The run is entries[i:i+len(run)] by construction (toolCallRun walks adjacent
			// entries forward), so the views' own entries are what the rule reads — and the same
			// construction is what lets the members' EXPANDED flags be read off the run in view order
			// and their rows be marked back by offset (blockPaint.addFor).
			//
			// Every one of those flags is in the paint key already: blockKey spans the whole run and
			// spanFlags packs expanded at bit 0 of each covered entry, so opening the tenth member of a
			// group is a different key and a fresh paint (paintcache.go).
			key := t.blockKey(shapeToolRun, i, len(run), th, width, blink,
				anyOpenCall(t.entries[i:i+len(run)]))
			block := t.paintBlock(i, key, func() blockPaint {
				return renderToolBlock(th, run, railedWidth(width, e.depth), blockState{
					expanded: e.expanded,
					live:     key.live,
					blink:    blink,
					members:  memberFlags(t.entries[i : i+len(run)]),
				}).railed(th, e.depth)
			})
			appendBlock(false, e.depth, i, block)
			i += len(run) - 1
		} else {
			// A tool call is the only single-entry kind with a live star, and entrySchedule paints
			// static by construction (renderEntryLines), so everything else keys as settled.
			key := t.blockKey(shapeEntry, i, 1, th, width, blink, e.kind == entryToolCall && !e.done)
			appendBlock(e.kind == entryUser, e.depth, i, t.paintBlock(i, key, func() blockPaint {
				return renderEntryLines(th, e, width, blink)
			}))
		}
	}
	if previewAt >= 0 {
		paintPreview(len(t.entries))
	}
	return renderedTranscript{lines: lines, userBlocks: userBlocks, targets: targets}
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
// Four kinds carry a click surface, for the one reason: they have two states to toggle between. A
// tool call, a scheduled Firing and a stray result share ONE block painter and so one click
// surface: every row of a block that hides something, with the remainder marker keeping its
// open-only meaning and, inside a folded run, each member's rows marked for that member. A prompt
// tall enough to collapse marks every row it paints too, and always has — the whole block is the
// toggle in both shapes (layout.md, "Collapsed and expanded blocks"). Every other kind comes back as plainPaint: a note or an answer paints one way
// whatever is asked of it, and a click there keeps its selection meaning. blink narrows further
// still — a tool call is the only entry that can still be WAITING for something, so it is the only
// header with a star to blink (layout.md, "The live star").
func renderEntryLines(th theme, e entry, width int, blink bool) blockPaint {
	inner := railedWidth(width, e.depth)
	switch e.kind {
	case entryUser:
		return renderUserBlock(th, glyphUser+" ", e.text, e.skillSpans, inner, e.expanded).railed(th, e.depth)
	case entryInterjected:
		// The human's mid-Exchange remark: the same block the prompt gets — it is the same voice —
		// under the ⧖ marker that says it arrived while the model was already working (ADR 0025).
		// Its skill tokens light up the sent block's way, for the same reason: a skill rides an
		// interjection (ADR 0027), and the record of what the model was given must not depend on
		// whether the remark was delivered mid-run or flushed into a new Exchange.
		return renderUserBlock(th, glyphInterject+" ", e.text, e.skillSpans, inner, e.expanded).railed(th, e.depth)
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
	case entrySchedule:
		// A Firing wears the tool block's shape under the /sessions tag's ⟳ (layout.md, "The firing
		// block"), so one Firing reads the same in the chat and in the browser. live and blink stay
		// false BY CONSTRUCTION rather than by accident: the spinner belongs to the worker driving
		// this session's Exchange and the session is idle while a Firing runs, so an animated header
		// here would claim work is happening in this session. What says the run is going is the
		// block's own static summary (schedule.go, scheduleRunningSummary).
		return renderToolBlock(th, []toolView{e.tool}, inner, blockState{
			expanded: e.expanded,
			glyph:    scheduleTagGlyph,
		}).railed(th, e.depth)
	case entryToolResult:
		return renderOrphanResult(th, e.text, inner, e.expanded).railed(th, e.depth)
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
