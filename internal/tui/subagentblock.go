package tui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/format"
)

// subAgentToolName is the raw tool id whose call block heads a sub-agent run. The span rule matches
// on the view's retained name (toolView.name, which the codec round-trips) rather than on the
// "Sub-Agent" label, so a relabelling cannot silently switch the rule off and a third-party tool
// that happens to share the label cannot switch it on.
const subAgentToolName = "sub_agent"

// subAgentSpan is the length of the run entries[i] heads: the maximal following stretch of entries
// nested deeper than it. That stretch IS the run — the transcript records a sub-agent's work
// head-first and folds the report back into the head (transcript.addToolResult), so everything the
// child did lies between the call and the next entry standing at the parent's own depth. Nothing
// marks the span; it is derived at paint from the depths already on the entries, exactly as the
// rails framing it are (railLines).
//
// It answers 0 for anything that is not a sub-agent call, and for a run that produced no nested
// entry at all (a child that failed before its first event) — either way the head is an ordinary
// tool block with nothing behind it, and renderView paints it as one.
func subAgentSpan(entries []entry, i int) int {
	head := entries[i]
	if !head.headsRun() {
		return 0
	}
	n := 0
	for j := i + 1; j < len(entries) && entries[j].depth > head.depth; j++ {
		n++
	}
	return n
}

// subAgentFramed reports whether a delegation is drawn as a RUN — the ┌─┶ opening its header row and
// the rail down everything beneath it — rather than as the ordinary tool block a
// delegation with nothing behind it is. It is asked of the head and its span length, which are the
// two facts the answer turns on, so [transcript.renderView] and [renderSubAgentGroup] frame a
// delegation by one rule instead of each wording one of its own.
//
// Committed entries behind it are one answer. The other is the LIVE one, kept from the shape that
// framed a run in place: a head marked open before its span exists is framed, because the first
// thing standing inside the rail was the delegate's streamed text — which the buffer holds and no
// entry carries yet (renderView's preview), so subAgentSpan answers 0 for it however much the child
// has said. Under ADR 0063 nothing writes that flag on a run any more ([transcript.setExpanded]
// refuses it) and the rows the answer picks between paint the same either way, so what the second
// arm still buys is a replayed record's stale flag landing on the shape the walk expects.
//
// A delegation that is OVER and left nothing behind it — a child refused at the depth bound, one
// that faulted before its first event — is framed by neither answer, and must not be: a frame opened
// there would enclose nothing at all, and the next block would close it again in the very next row.
func subAgentFramed(head paintInput, span int) bool {
	if !head.headsRun() {
		return false
	}
	return span > 0 || (head.expanded && !head.done)
}

// insideCollapsedRun reports whether a block about to be painted at depth would land inside a
// sub-agent run that is currently COLLAPSED — the question subAgentSpan answers for committed
// entries, asked on behalf of the one block that is not in the list: the live streaming preview
// (renderView). A collapsed run stands alone and everything railed beneath it is elided
// (layout.md), and a delegate's answer is beneath it from its first streamed token, not only once
// its MessageEvent commits an entry the span rule can see.
//
// It keys on the HEAD rather than on the span being non-empty, because a child that has streamed
// but not yet called a tool has produced no nested entry at all — subAgentSpan is 0 there, and a
// rule reading it would let exactly the first tokens through. Every enclosing run is asked, so a
// nested run streaming inside a collapsed parent is elided by the parent's state as well as by its
// own: the chain is walked by SPAWNING CALL — this run's head, then the run that head sits in, up
// to the top — which is what keeps the answer exact while siblings run at once (ADR 0039), where
// the most recent open head at a level names whichever child was announced last rather than the one
// that is talking.
//
// A run with no spawning call to walk from — a hand-built test transcript, a record replayed from a
// blob written before the id was stamped — still answers by the depth rule this began as: the most
// recent still-open head at each enclosing level.
//
// root is where the walk STOPS: the run the paint is rooted at (transcript.root), whose own head is
// not on screen to be collapsed — inside a run view that head is the breadcrumb, and the view is
// showing what a reader opened. Without the stop a view's own live tail would be elided by the very
// row the reader replaced with the header: a run has two shapes under ADR 0063 — the collapsed row
// and its view — so opening a view leaves the head collapsed the whole time it is open. The zero
// root stops the walk at the top of the transcript, which is the rule as it was written.
func insideCollapsedRun(entries []entry, run, root runRef) bool {
	if run.spawn == "" {
		return insideCollapsedRunAtDepth(entries, run.depth)
	}
	for spawn := run.spawn; spawn != "" && spawn != root.spawn; {
		head, ok := runHead(entries, spawn)
		if !ok {
			return false
		}
		if !head.expanded {
			return true
		}
		spawn = head.spawnCallID // this run is open: the run it sits in may still be collapsed
	}
	return false
}

// runUnder reports whether run IS root or lies anywhere inside it — the question a rooted paint asks
// of the live streaming buffer, which is the one block that is not an entry and so cannot be placed
// by the walk's own bounds (render.go). The chain is walked by SPAWNING CALL for
// [insideCollapsedRun]'s reason: with siblings live (ADR 0039) a depth says which level a run stands
// at and never which run it is.
//
// The zero root is the whole transcript, and every run is under that.
func runUnder(entries []entry, run, root runRef) bool {
	if root.spawn == "" {
		return true
	}
	for spawn := run.spawn; spawn != ""; {
		if spawn == root.spawn {
			return true
		}
		head, ok := runHead(entries, spawn)
		if !ok {
			return false
		}
		spawn = head.spawnCallID
	}
	return false
}

// runHead finds the sub_agent call block that opened the run spawn names.
func runHead(entries []entry, spawn string) (entry, bool) {
	at, ok := runHeadAt(entries, spawn)
	if !ok {
		return entry{}, false
	}
	return entries[at], true
}

// runHeadAt is [runHead] as a POSITION: where the sub_agent call block that opened the run spawn
// names sits in the list, which is what a paint rooted at that run needs — the run IS the head's
// [subAgentSpan], and a span is a range of indices. −1 and false where the list holds no such head.
func runHeadAt(entries []entry, spawn string) (int, bool) {
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].headsRunFor(spawn) {
			return i, true
		}
	}
	return -1, false
}

// The run view's header row, spelled once (render.go): the way back (←), the separator between the
// runs the trail names (›), and the key that takes it. The hint is the status line's own grammar for
// a key — the verb after the key that presses it (layout.md, "The status line's right slot") — and
// it stands in the same column that slot's occupants end in, so the two rows on screen advertising a
// key end together.
const (
	breadcrumbBack = "←"
	breadcrumbSep  = "›"
	breadcrumbHint = "esc back"
)

// breadcrumbTrail is the header's TEXT for a paint rooted at the run spawn names: the trail of run
// names from the human's own conversation down to that run — "← main › planner › repo-scout" — so a
// reader two levels in sees both where they are and what stands between them and the top.
//
// Each run is named the way every other surface that names one does ([usageAgentName]): the short
// name its call carried, else the task's first line, else the constant. An unnamed delegation
// therefore reads as something rather than as a hole in the trail, and the /usage pane and this row
// cannot come to call the same run different things.
//
// The walk climbs by SPAWNING CALL rather than by depth, for [insideCollapsedRun]'s reason: with
// siblings live (ADR 0039) a depth says which level a run stands at and never which run it is. A
// spawn the list holds no head for ends the climb where it stands — the trail names what it can and
// still leads back to main, which is the one crumb that is always true.
func breadcrumbTrail(entries []entry, spawn string) string {
	var names []string
	for id := spawn; id != ""; {
		at, ok := runHeadAt(entries, id)
		if !ok {
			break
		}
		names = append(names, usageAgentName(entries[at]))
		id = entries[at].spawnCallID
	}
	slices.Reverse(names) // climbed from the run upwards; the trail reads downwards
	return breadcrumbBack + " " + strings.Join(append([]string{usageMainLabel}, names...), " "+breadcrumbSep+" ")
}

// breadcrumbRow paints that trail as the run view's own sticky header: the trail in the transcript's
// body column, the key that leaves the view held bodyIndent off the right edge, on the one
// full-width field the frame already spends on a header (the user block's).
//
// hint is that key's wording for THIS frame rather than a constant, for the reason the status line's
// right slot gates the same wording ([Model.statusRight]): while a child's ask or approval pane
// stands inside the view, esc answers the pane and not the view ([Model.runViewOwnsEsc]), and a
// header still advertising `esc back` would name a key the press does not have. An empty hint paints
// the trail alone — the two rows advertise one key, so they fall silent together ([Model.backHint]).
//
// Where the width cannot pay for both, the hint gives way whole: the trail is what the header is
// FOR, and a truncated key hint would advertise a keystroke nobody could read. The row is squared to
// the width either way, so the field runs the whole way across rather than showing the terminal's
// own background through the gap.
func breadcrumbRow(th theme, trail string, width int, hint string) string {
	body := bodyIndent + trail
	if hint == "" {
		return th.userBlock.Render(squareLine(th.measure, body, width))
	}
	hint += bodyIndent
	gap := width - th.measure.Width(body) - th.measure.Width(hint)
	if gap < 1 {
		return th.userBlock.Render(squareLine(th.measure, body, width))
	}
	return th.userBlock.Render(squareLine(th.measure, body+strings.Repeat(" ", gap)+hint, width))
}

// insideCollapsedRunAtDepth is insideCollapsedRun's answer for a run with no spawning call id: each
// enclosing level's most recent still-open sub_agent head, which is the only handle a stream of
// depths alone offers. It was the whole rule while delegation was serialized (ADR 0014) and is
// exact wherever it still applies — a session with one delegate at a time, and every replayed
// record, whose entries are settled and stream nothing.
func insideCollapsedRunAtDepth(entries []entry, depth int) bool {
	for level := depth - 1; level >= 0; level-- {
		for i := len(entries) - 1; i >= 0; i-- {
			head := entries[i]
			if !head.opensRun() || head.depth != level {
				continue
			}
			if !head.expanded {
				return true
			}
			break // this level's run is open: a deeper one may still be collapsed
		}
	}
	return false
}

// renderSubAgentRun paints the head block of a sub-agent run — the whole of what the run shows in
// the conversation, since renderView elides its span. A framed delegation has ONE shape here and a
// second that is no block at all: the collapsed row, and the run VIEW that expanding it opens
// (ADR 0063, runview.go). The head's own fold state reaches nothing in this paint, so neither a
// replayed record nor a stale flag can re-open a rail in place — and [transcript.setExpanded]
// refuses to write one in the first place.
//
// A LONE run is drawn in the very shape a grouped one is (design call 3 of
// docs/plans/"2026-08-11 - 01"): the same cascading summary of the work behind it
// (subAgentSummary), and the same ✓ after its name once it has reported (subAgentFinished).
// Whether the delegations either side of it happened to fold it into a list is a fact about the
// frame around a delegation and never about the delegation, so the two paths ask the same two
// questions of the same head rather than each wording an answer of its own.
//
// The head is ONE summarised line: the report body is elided along with the span, because the
// summary slot already carries that report's first line and no block says the same thing twice in
// two adjacent rows (layout.md, "A sub-agent run collapses to its call block"). That elision is
// also what closed the double print (ISSUES.md, "Finished sub-agents print the sub-agent output
// twice"): the body this block laid out while the head was open WAS the delegation's report,
// unformatted, standing above the same report as the span's own last assistant row. The count in
// the summary is what says work is hidden behind the header, so the run needs no "+N more lines"
// marker to say it too — and the header is a target however short the report is
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
func renderSubAgentRun(th theme, head paintInput, span []paintInput, width int, blink bool) blockPaint {
	view := collapsedSubAgentView(head, span)
	view.finished = subAgentFinished(head)
	block := renderToolBlock(th, []toolView{view}, railedWidth(width, head.depth), blockState{
		elides: true,
		live:   !subAgentReported(head) || anyOpenCall(span),
		blink:  blink,
	})
	return block.railed(th, head.depth)
}

// unframedSubAgentPromptLead labels the prompt where an UNFRAMED delegation shows it. The framed
// reading needs no label — a run's prompt opens its own view as the user row it is (render.go's
// rooted paint), where the marker says whose words they are — but a body line under an ordinary
// tool block has nothing around it saying that, and a prompt read as the delegate's output would be
// the block lying about who spoke.
const unframedSubAgentPromptLead = "task: "

// subAgentHidesPrompt reports whether a DELEGATION's collapsed row is holding back the prompt it
// carried. It is the toggle-target rule (blockHidesWhenCollapsed) asked about the one thing a
// delegation's block hides that is nowhere among its views: every reading a delegation opens onto
// paints the prompt — the framed run as the user row its view opens with (ADR 0063, render.go's
// rooted paint), the never-ran block as its body (unframedSubAgentView) — while the collapsed row
// is one leader row that never does, the task's first line riding the header as the target being
// the header's text and not the prompt (subAgentTarget).
//
// It is asked of the VIEW rather than of the entry because the promote-guard is exactly what it must
// not depend on: a refusal short enough to keep the outcome slot leaves the block bodiless, so the
// body-counting rule beside it answered "nothing to reveal" at wide widths and "something" at narrow
// ones, and the indicator, the click surface and the prompt underneath them all came and went with
// the terminal's columns (ISSUES.md, 2026-08-27).
//
// A delegation with no prompt to show — an empty task, whitespace alone, a record replayed from a
// session written before the text was retained (transcriptcodec.go) — answers false: the readings
// above open onto nothing for it, and an indicator over an empty frame is the affordance the rule
// exists to withhold.
func subAgentHidesPrompt(tv toolView) bool {
	return tv.headsRun() && strings.TrimSpace(tv.task) != ""
}

// unframedSubAgentView is what an EXPANDED delegation that NEVER RAN shows of itself: the prompt it
// carried, over whatever its result left behind.
//
// A delegation refused at the depth bound, failed by a hook before its first event, or lost to a
// construct error (agent.runSubAgent) is over with nothing behind it, so [subAgentFramed] frames it
// by neither of its answers and it is drawn as the ordinary tool block it is. That framing is right
// and is not what this changes — a frame opened over an empty span would enclose nothing and be
// closed again by the very next row. What it left with nowhere to go is the PROMPT: the framed
// reading paints it inside the frame, so an unframed delegation showed what any tool block shows and
// never what it was asked. Here the prompt is the block's BODY instead, which is the one place an
// unframed block has for it.
//
// The header's own text does not stand in for it. Target carries the task's first line clipped to
// the branch's budget, or the delegation's NAME where one was given (subAgentTarget), so on a named
// or a multi-line delegation the request is otherwise unrecorded on screen — which is exactly the
// case a reader opening a refused delegation is asking about.
//
// The result's own lines follow after a blank one, so the refusal reads as a second voice rather
// than as more of the request. A delegation with no prompt to show — an empty task, whitespace
// alone, a record replayed from a session written before the text was retained (transcriptcodec.go)
// — keeps the view it had: a lead line over nothing would be a heading for an empty body.
//
// The copy is a paint-time act on facts the entry keeps whole, exactly as the framed reading's
// is (collapsedSubAgentView).
func unframedSubAgentView(head paintInput) toolView {
	view := head.tool
	body := subAgentPromptDetails(view.task)
	if len(body) == 0 {
		return view
	}
	// A one-line result the presenter PROMOTED into the outcome slot is folded into the body first,
	// through the promote-guard's own rule rather than a second wording of it (toolView.demoted):
	// the prompt has to stand above what came back, and a promotion the painter demotes only at a
	// narrow width would otherwise put the refusal above the prompt on some terminals and below it
	// on others. The swap is licensed exactly here — the slot still says what happened, and the
	// block now manifestly has something to reveal.
	view = view.demoted()
	if carried := view.Details.all(); len(carried) > 0 {
		body = append(body, detailLine{})
		body = append(body, carried...)
	}
	view.Details = newToolBody(body)
	return view
}

// subAgentPromptDetails is the delegated prompt as body lines: its first line under the "task: "
// lead and every further line plain beneath it, each held to the per-line clip tool output is held
// to (outputBody) — a prompt is text a model wrote, and one body bounds every kind of quoted text
// the same way. A prompt that is blank throughout has no lines at all, which is what leaves the
// caller's view untouched.
func subAgentPromptDetails(task string) []detailLine {
	if strings.TrimSpace(task) == "" {
		return nil
	}
	lines := splitLines(strings.TrimRight(task, "\n"))
	out := make([]detailLine, 0, len(lines))
	out = append(out, detailLine{Text: clipDetail(unframedSubAgentPromptLead + lines[0])})
	for _, ln := range lines[1:] {
		out = append(out, detailLine{Text: clipDetail(ln)})
	}
	return out
}

// subAgentMember is one row of a folded sub-agent group as the painter needs it: the delegation's
// own call entry, the run nested beneath it, and where that entry sits relative to the block's
// head. It is the paint-time reading of [subAgentBlock], which names the same member by index — the
// indexes are what a click resolves through and the entries are what is drawn, so the two shapes
// are kept apart rather than one being made to serve both (renderView builds these).
//
// It carries the head and its span as the painters' input records ([paintInput]) rather than as a
// finished view, for [renderSubAgentRun]'s reason: what a COLLAPSED delegation's row says is derived
// from both (subAgentSummary), and deriving it inside the painter is what keeps the work behind the
// paint cache instead of on every frame.
type subAgentMember struct {
	head   paintInput   // the delegation's own call entry: its view, and its expanded state
	span   []paintInput // the run nested beneath it; empty for a delegation that produced none
	offset int          // the member's entry, as an offset from the block's head (blockPaint.addFor)

	// last marks the GROUP's final member, whose row closes the list with ┕. It is the group's
	// answer and not the block's: a group interrupted by an open member paints its remaining rows
	// in a second block, and the ┕ still belongs to the last row of the whole list.
	last bool
}

// subAgentGroupLabel is the header a folded group of delegations wears, ahead of its count
// (docs/layout/tool-layout.md, Rules: "✦ Sub-Agent (N)"). It is read off the members' own registry
// label rather than restated, so the group and a lone delegation cannot come to name the tool
// differently; this constant is only the fallback for a group whose views carry no label at all.
const subAgentGroupLabel = "Sub-Agent"

// renderSubAgentGroup paints a folded group of adjacent delegations — "✦ Sub-Agent (3)" over one
// row per agent, the agent's name on the left and its verdict in the outcome slot
// (docs/layout/tool-layout.md, Rules). It is the same list the same-label group is
// (renderToolGroup), and the member rows go through the very painter that block's do, so a
// delegation reads as a row of a list wherever it is folded.
//
// What is NOT here is the half that makes a delegation different: its RUN. A framed member's span is
// never painted beside its row — expanding one opens the run's own view (ADR 0063), where those
// entries are painted by [transcript.renderView]'s ordinary walk rooted at that run. So every framed
// member is one collapsed row here, whatever its fold flag says.
//
// The list can still be interrupted, by the one member that opens a body in place: a delegation that
// never ran (unframedSubAgentView). The group then paints its rows up to that member here and its
// remaining rows in a second block of this same shape, which is why count and last are stated
// separately: count opens the header and is 0 on the continuation block, while last belongs to the
// whole group's final row.
//
// A member with no span is an ordinary group member and takes that painter whole, footer included —
// its body is the whole of what it hides.
//
// What a row SAYS is not the group's business at all: each member is read exactly as the lone block
// it folded from (collapsedSubAgentView), so folding changes the frame around a delegation and
// never the record of it.
func renderSubAgentGroup(th theme, count int, members []subAgentMember, width int, state blockState) blockPaint {
	var out blockPaint
	if count > 0 {
		label := th.toolLabel.Render(groupLabelOf(members)) + " " +
			th.toolIndicator.Render(fmt.Sprintf(groupCountFormat, count))
		out.add(hangingWrap(th, th.toolHeader, state.star()+" ", label, width), targetNone)
	}
	room := toolRowCells(th, width)
	for _, m := range members {
		marker, spanned := branchMarker(m.last), subAgentFramed(m.head, len(m.span))
		view := m.head.tool
		// A QUEUED member is none of the readings below: it has no work to summarise and
		// nothing to open, so it says the one word and takes the ordinary member painter with an
		// empty body — which is what leaves it without an indicator and without a click target
		// (scheduledSubAgentView). It cannot be spanned by construction: a delegation with entries
		// behind it has started.
		//
		// A FRAMED member has no open reading to differ from: expanding it opens its run view
		// (ADR 0063), so the row it leaves behind in the list is the collapsed one whatever its own
		// fold flag says.
		switch {
		case subAgentScheduled(m.head, len(m.span)):
			view = scheduledSubAgentView(m.head)
		case spanned:
			view = collapsedSubAgentView(m.head, m.span)
		case m.head.expanded:
			// An OPEN member that never ran is unframed exactly as a lone one is, and shows the
			// prompt it carried for the same reason (unframedSubAgentView): folding changes the
			// frame around a delegation and never what the delegation shows of itself. It is asked
			// here rather than inside the member painter so the promote-guard below sees the body
			// the row is actually about to hide.
			view = unframedSubAgentView(m.head)
		}
		view.finished = subAgentFinished(m.head)
		view = guardPromotions(th, []toolView{view}, room, marker)[0]
		rows, hides := renderSubAgentMemberRows(th, view, marker, width, room, m.head.expanded, spanned)
		kind := targetNone
		if hides {
			kind = targetHeader
		}
		out.addFor(m.offset, rows, kind)
	}
	return out
}

// subAgentFinished asks whether a delegation has earned the done ✓ its row wears after its name
// (design call 6 of docs/plans/"2026-08-11 - 01"): it has reported, and what it reported was not a
// failure. A run still working wears nothing — the blinking star is what says it is going — and a
// FAILED run wears nothing either, because the spec makes its red outcome slot the whole of the
// failure marking (summaryStyle); a ✓ and a red verdict on one row would be the block saying both
// at once.
//
// The verdict is the head's OWN summary's ([branchSummary.failed]) and never a reading of the
// collapsed line composed for the row (collapsedSubAgentView): that line carries the same verdict
// on purpose (subAgentSummary), but its TEXT opens with a count of the work, so words read out of
// it would answer "not a failure" for every failed delegation there is.
func subAgentFinished(head paintInput) bool {
	return subAgentReported(head) && !head.tool.Summary.failed
}

// subAgentReported is the display's question "is this delegation over?", and the two answers that
// mean yes are kept apart from each other on purpose. The delegation's own FINISHED phase says its
// child reached its boundary and handed its report back (domain.SubAgentPhaseEvent, which carries
// that report with it); the entry's done says the result has been PAIRED with the call. In a fan-out
// those are far apart in time: results burst together, in call order, once every child has joined
// (ADR 0039 decision 4), so a member that finished first would read as still working for as long as
// its slowest sibling ran — which is precisely the defect the phase exists to close.
//
// done is still an answer and not merely a fallback: it is the only one a REPLAYED record carries,
// and the only one a phase-less producer ever emits (a hand-built test transcript, a session written
// before the event existed). Reading either is what lets one rule serve both.
func subAgentReported(head paintInput) bool {
	return head.done || head.phase == domain.SubAgentFinished
}

// scheduledSummary is the whole of what a queued delegation's <tool-top-level-details> says.
const scheduledSummary = "scheduled"

// subAgentScheduled reports whether a delegation is QUEUED: the model asked for it, and no child is
// running behind it yet. A fan-out wider than the Parallel agents cap emits every ToolCallEvent up
// front and then holds the surplus children back until a worker frees a slot (ADR 0039), so between
// the request and the start there is a row on screen with no work behind it — which is exactly what
// it is made to say (scheduledSubAgentView).
//
// Three facts end it, and each is a different producer's word. The delegation's own STARTED phase is
// the engine's (domain.SubAgentPhaseEvent), emitted the instant a worker dequeues the job — the
// signal this state exists to wait for. Its being OVER is the second: a delegation refused at the
// depth bound or failed by a hook never runs and so is never started, yet its result still arrives
// (dispatch.go), and a row left "scheduled" over a delegation that already answered would say so
// forever. The third is its being FRAMED (subAgentFramed) — a run standing behind it, or a reader
// having opened it. That one is the answer for a producer that emits no phases at all: a hand-built
// test transcript, a record replayed from a session written before the phase existed. A delegation
// with entries behind it has manifestly started whatever it announced, and one that is OPEN was
// expandable when the click landed, which a scheduled row never is. Reading all three is what lets
// one rule serve every producer — the discipline subAgentReported follows for the other end of the
// same life — and being framed and being scheduled are mutually exclusive by construction, which is
// what keeps the queued row out of the reading that would draw it a frame (renderSubAgentGroup).
func subAgentScheduled(head paintInput, span int) bool {
	if !head.headsRun() {
		return false
	}
	return head.phase != domain.SubAgentStarted && !subAgentReported(head) && !subAgentFramed(head, span)
}

// scheduledSubAgentView is what a QUEUED delegation shows of itself (docs/layout/tool-layout.md,
// Grouped Sub-agents): its own header row, and one word in the outcome slot. Everything the running
// reading carries is deliberately absent — no count of tool calls, because none have happened; no
// context fill, because the child has no window yet; no gist, because nothing has been touched. A
// row saying "0 tool calls" would be stating a measurement of work that has not begun.
//
// The body goes with them, and that is what makes the row INERT: an empty body hides nothing, so the
// member painter gives it no indicator and the group gives it no click target (renderGroupMember,
// renderSubAgentGroup) — an affordance opening onto an empty frame is one a reader learns to
// distrust. The row becomes an ordinary live delegation the moment its start lands.
//
// The summary is NOT marked quoted (branchSummary): the word is apogee's own about a delegation, not
// a line the child produced, and the mark is a statement about where the text came from.
func scheduledSubAgentView(head paintInput) toolView {
	view := head.tool
	view.Summary = branchSummary{detailLine: detailLine{Text: scheduledSummary}}
	view.Details = toolBody{} // the zero body: nothing to lay out, and so nothing to expand for
	return view
}

// groupLabelOf is the label a folded group of delegations names itself with: the members' own, off
// the first of them, falling back to the constant for a view built before the registry knew the
// tool (a replayed record, a hand-built test transcript).
func groupLabelOf(members []subAgentMember) string {
	if len(members) > 0 && members[0].head.tool.Label != "" {
		return members[0].head.tool.Label
	}
	return subAgentGroupLabel
}

// renderSubAgentMemberRows paints one member of a folded sub-agent group and reports whether the
// collapsed row hides anything — which is both what makes it wear an indicator and what makes it a
// click target, the same question every folded shape asks (renderGroupMember).
//
// A delegation that left a RUN behind it always hides something, whatever its own report says: the
// run is what expanding reveals, and it is revealed in its own VIEW rather than under this row
// (ADR 0063). So a framed member is ONE collapsed line in every state the flag can be in — the row
// stays a target, and what it opens is a screen and not a body.
//
// A delegation with no span is an ordinary member and goes through the ordinary painter, so a
// refused delegation folds, opens and closes exactly as a read or a terminal call does.
func renderSubAgentMemberRows(th theme, tv toolView, marker string, width, room int,
	expanded, spanned bool) (lines []string, hides bool) {
	if !spanned {
		return renderGroupMember(th, tv, marker, memberGutter, width, room, expanded)
	}
	row := leaderRow(th, tv, marker, room, false, noRemainder)
	return []string{indicatorRow(th, row, width, glyphCollapsed)}, true
}

// collapsedSubAgentView is the whole of what a framed delegation shows of itself, wherever it is
// folded: its own header view with the run's cascading summary in the outcome slot
// (subAgentSummary), and no body at all. It is the ONLY reading a run has in the conversation —
// under ADR 0063 the other shape is its run view, which paints the run's own entries rather than a
// view of the head.
//
// What the head carries of its own — a report gist promoted into the slot by the presenter, or the
// typed `done` where the report became a body — is not lost by the swap: subAgentSummary's last
// cell is exactly that text, now behind the count of the work and the delegate's fill.
//
// The BODY is dropped because the summary already carries the report's first line and no block says
// the same thing twice in two adjacent rows — the defect this closed was exactly that, the report
// laid out here in full above the formatted copy the span's last assistant row already held
// (ISSUES.md, "Finished sub-agents print the sub-agent output twice"). It is a paint-time act on
// facts the entry keeps whole, so the report the run view opens on is the one the delegation
// actually returned.
//
// One reading serves the lone block and the folded group's member row alike (renderSubAgentRun,
// renderSubAgentGroup): grouping changes the frame a delegation is drawn in, and a second wording
// of "what does a delegation's row say" would part company with this one — taking the per-child
// live tail a fan-out is observed through (ADR 0039) with it.
func collapsedSubAgentView(head paintInput, span []paintInput) toolView {
	view := head.tool
	view.Summary = subAgentSummary(head, span)
	view.Details = toolBody{} // the zero body: no lines, and so nothing to lay out beneath
	return view
}

// subAgentSummary words a collapsed run's one line: how much work happened in there, how full the
// delegate's own context got doing it, then its gist (layout.md, "A sub-agent run collapses to its
// call block"). The count is TRANSITIVE by construction — the span holds every entry of every level
// below the head, so counting its tool calls counts the grandchildren too, and the same rule read at
// a deeper head gives that level's own total without a second rule.
//
// The fill is the exact opposite: it is the head's OWN frozen reading (subAgentFill) and never a
// nested run's, because each agent fills a window of its own. It sits between the count and the gist
// so that the two readings a row always carries hold the left, and the gist — the one part with no
// bound on its length — takes the clip a narrow terminal makes.
//
// A run with nothing to say beyond the count — no reading yet, nothing to add while it works, or a
// report that carried no line at all — keeps the count alone rather than trailing an empty
// separator. That is the ORDINARY reading of a working run now that the gist has no live tempo of
// its own (subAgentGist): while the child works the line is its count and its fill, and both hold
// still between one landing and the next.
//
// The line is marked QUOTED (branchSummary) for what its last cell is once the run reports: the
// child's own report. Nothing respells it — this is composed at paint, long after the shortening
// seam ran on the way in — so the mark is a statement about the text rather than a switch, and it is
// the one that stays true if a seam ever reads it.
//
// A run that went somewhere else CLOSES the line with the model it went to (subAgentModel), which is
// the whole of what routing to the Sub-agent server shows of itself on a delegation (ADR 0045). It
// is last, after the gist, because it is the rarest cell on the row: it appears only while routing
// is on AND the target is bound to another model, so a reader who sees it is reading a line they
// already know to be unusual — where the count and the fill are on every row and earn the left.
func subAgentSummary(head paintInput, span []paintInput) branchSummary {
	calls := 0
	for i := range span {
		if span[i].kind == entryToolCall {
			calls++
		}
	}
	text := plural(calls, "tool call")
	if fill := subAgentFill(head); fill != "" {
		text += " · " + fill
	}
	if gist := subAgentGist(head, span); gist != "" {
		text += " · " + gist
	}
	if model := subAgentModel(head); model != "" {
		text += " · " + model
	}
	summary := quotedSummary(detailLine{Text: text})
	// The verdict is the HEAD's OWN (branchSummary.failed), carried onto the composed reading rather
	// than re-derived from anybody's wording. This line opens with a count of the work, so its own
	// words say nothing about how the run ended and a painter reading them would find no failure to
	// paint (F-28) — and reading the head's TEXT instead was no better: the head's slot holds the
	// child's REPORT, quoted, so a run that succeeded and opened its report with "error: …" was
	// painted red while subAgentFinished — which asks the field — handed it the done ✓, the block
	// saying both at once. One field answers both, so the red slot and the missing ✓ cannot disagree:
	// what failed a delegation is its result's error status (enrichWithResult), never a sentence.
	summary.failed = head.tool.Summary.failed
	return summary
}

// subAgentModel spells the run head's model cell, or nothing at all. The entry holds a model only
// where it DIFFERED from the session's when the reading folded (entry.ctxModel), so the decision is
// already made by the time anything is painted and this seam only has to spell it — in the footer's
// own language (displayModel), so the one model on screen and the other are read the same way rather
// than one showing a bare name and the other a weights path.
func subAgentModel(head paintInput) string {
	return displayModel(head.ctxModel)
}

// subAgentFill spells the run head's context reading as the gauge spells one — "12k/32k", the same
// coarse form the status line's window uses ([format.Tokens]), so the two readings on screen
// are read in one language. It is the pair frozen on the entry when the reading folded
// (transcript.applyUsage), which is why a finished run keeps saying what it filled.
//
// A missing half means no cell at all: a fill without its limit is a number with no scale, and the
// status gauge hides itself on the very same condition rather than showing one. That is also what a
// run whose child never reported paints — and what a session recorded before the reading existed
// decodes to (transcriptcodec.go), so nothing needs a migration to look right.
func subAgentFill(head paintInput) string {
	if head.ctxUsed <= 0 || head.ctxLimit <= 0 {
		return ""
	}
	return format.Tokens(head.ctxUsed) + "/" + format.Tokens(head.ctxLimit)
}

// delegatingSummary is the one live word a working run's summary can add: its child is not doing the
// work itself but has handed it to a child of its own.
const delegatingSummary = "delegating"

// subAgentGist is the last cell of a collapsed run's summary. Once the report lands it is the head's
// own gist — the summary a short report was compressed to, or, where the report was long enough to
// become a body, its first line.
//
// While the run WORKS it is almost always empty, and that is the point. It used to name the call the
// span had open, verb and shortened target together under a 32-cell cap, re-read on every
// frame: a cell that changed several times a second beside two that did not, so the eye was pulled
// to the least durable thing on the row and the count and fill it sat next to were the harder ones
// to read. The work it named is not lost, only left where it already stands: every one of those
// calls has a block of its own inside the run, in full, one click away.
//
// The one live word it keeps is `delegating`, and only when the span's MOST RECENT open call is
// itself a delegation — the child has passed the work on and has nothing of its own in flight. That
// is the single live fact the run's own blocks cannot stand in for, since opening the row shows a
// nested run that is itself collapsed. It is deliberately the most recent call and not any open one:
// the moment the grandchild calls a tool of its own that call is the newest, the word goes, and the
// row is back to its count — so the cell names the nearest live fact or nothing at all, never a
// stale one.
func subAgentGist(head paintInput, span []paintInput) string {
	if subAgentReported(head) {
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
			if e.headsRun() {
				return delegatingSummary
			}
			return ""
		}
	}
	return ""
}
