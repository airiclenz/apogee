package tui

import "github.com/airiclenz/apogee/internal/domain"

// foldActKind names one reader act on a foldable block — what a click, ⏎ at the block cursor or a
// side-panel jump asks of the fold. Each gesture states its act; which setter answers it, and in
// what order, is [transcript.applyFoldAct]'s alone, so no gesture caller reaches the fold rule or
// its per-arm setters (ADR 0035 D2 and its amendments 1118, 1120, 1120c, 1123).
type foldActKind int

const (
	// foldActHeader: the reader moved an umbrella's — or a Sub-agent or Skill group's — header fold
	// (targetUmbrella).
	foldActHeader foldActKind = iota
	// foldActTypeRow: the reader flipped a super-group's type row (targetType).
	foldActTypeRow
	// foldActMember: the reader flipped one block, a member of a group or not — its row, its body,
	// or a block standing on its own (targetHeader).
	foldActMember
	// foldActEnterRun: the reader is entering a delegation's run view from its row; the view opens
	// after the act ([Model.openRunAt]).
	foldActEnterRun
	// foldActReveal: a side-panel jump needs a member painted: its folded block opens on a fold of
	// its own and the type row holding it opens ([transcript.revealBlockMember]).
	foldActReveal
)

// foldAct is one reader act: what it does (kind) and the entry it lands on (index) — a block's
// head for a header act, the member's own entry otherwise.
type foldAct struct {
	kind  foldActKind
	index int
}

// foldOutcome is what an act did. acted reports that the act found what it acts on — a fold, a
// type row, a block, a foldable block to touch or reveal — so a gesture with nothing under it
// changes nothing and repaints nothing. folded is the fold the block holding the act's index stands
// in once the act has landed — flipToolsOpen's flip included — and false where no foldable block
// holds it. flipToolsOpen asks the caller to flip the shared `ui.tools-open` preference
// ([Model.toggleToolsFold]): the transcript never writes that preference or the config itself, so
// a header act on a LARGE umbrella under the shared fold hands the flip back rather than make it.
type foldOutcome struct {
	acted         bool
	folded        bool
	flipToolsOpen bool
}

// applyFoldAct performs one reader act on the transcript's fold state and reports what it did:
//
//   - A HEADER act moves the fold that is actually holding the block, as the fold rule
//     ([transcript.blockFoldBy]) names it — the one rule the paint reads too. An AUTO-COLLAPSED
//     block (settled, untouched) opens alone on a fold of its own and writes nothing
//     ([transcript.openBlockAlone]), whatever its size: it never asks for the shared flip. A LARGE
//     umbrella under the shared preference is touched and asks the caller for that flip
//     (flipToolsOpen). A SMALL one, a touched one with a fold of its own, and a Sub-agent or Skill
//     group past auto-collapse flip their head flag alone ([transcript.setBlockFolded]) —
//     in-session, written nowhere, every child's state left as it stood for the reopen — and are
//     touched. A group of one is an umbrella like any other.
//   - A TYPE-ROW act flips the run's second state ([transcript.toggleTypeExpanded]) and a MEMBER
//     act flips the block's own expanded flag ([transcript.toggleExpanded]), which every
//     block-state kind takes — a long prompt and a solo call included. Either touches the foldable
//     block holding it ([transcript.markBlockTouched]) only after a flip that moved, so
//     auto-collapse leaves it as the reader arranged it; an index in no foldable block touches
//     nothing.
//   - An ENTER-RUN act touches the group the run's row sits in, before the caller opens the view,
//     so a settled group the reader went into never auto-collapses behind them.
//   - A REVEAL act opens a folded block on a fold of its own — never the shared fold — or only
//     touches an open one, and opens the type row holding the member ([transcript.revealBlockMember]).
//
// Every write moves the paint's generation only on a move, the setters' own rule.
func (t *transcript) applyFoldAct(act foldAct) foldOutcome {
	switch act.kind {
	case foldActHeader:
		return t.actOnFoldHeader(act.index)
	case foldActTypeRow:
		if !t.toggleTypeExpanded(act.index) {
			return foldOutcome{}
		}
	case foldActMember:
		if !t.toggleExpanded(act.index) {
			return foldOutcome{}
		}
	case foldActEnterRun:
		if !toolBlockAt(t.entries, act.index).foldable() {
			return foldOutcome{}
		}
	case foldActReveal:
		if !t.revealBlockMember(act.index) {
			return foldOutcome{}
		}
		return foldOutcome{acted: true, folded: t.blockFoldAt(act.index)}
	default:
		return foldOutcome{}
	}
	t.markBlockTouched(act.index)
	return foldOutcome{acted: true, folded: t.blockFoldAt(act.index)}
}

// actOnFoldHeader is the header arm of [transcript.applyFoldAct]: it moves the fold the rule says
// holds the block headed by entries[head], and answers nothing where no fold heads there.
func (t *transcript) actOnFoldHeader(head int) foldOutcome {
	switch t.blockFoldBy(head) {
	case foldBySettled:
		t.openBlockAlone(head)
	case foldByShared:
		t.markBlockTouched(head)
		// The caller's flip shuts an open preference and opens a shut one, so the block's fold
		// after it is the preference's open state now.
		return foldOutcome{acted: true, folded: t.toolsOpen, flipToolsOpen: true}
	case foldByHead:
		t.setBlockFolded(head, !t.blockFolded(head))
		t.markBlockTouched(head)
	default:
		return foldOutcome{}
	}
	return foldOutcome{acted: true, folded: t.blockFolded(head)}
}

// blockFoldAt reports whether the foldable block holding entries[index] stands folded, false where
// none holds it.
func (t *transcript) blockFoldAt(index int) bool {
	block := toolBlockAt(t.entries, index)
	return block.foldable() && t.blockFolded(block.head)
}

// actOnFold is the one door a reader's fold gesture comes through: the transcript performs the act
// ([transcript.applyFoldAct]) and, when it asks for the shared flip, the Model makes it — the flip
// of `ui.tools-open` and its write-back ([Model.toggleToolsFold]) — the only reader of
// flipToolsOpen. It returns the Model for the caller to repaint, and the outcome for it to read.
func (m Model) actOnFold(act foldAct) (Model, foldOutcome) {
	outcome := m.transcript.applyFoldAct(act)
	if outcome.flipToolsOpen {
		m = m.toggleToolsFold()
	}
	return m, outcome
}

// setToolsOpen moves the shared fold of every large Tools umbrella ([transcript.toolsOpen]): a
// header toggle on any one of them flips them all, and a `/settings` edit or a hand-edited file
// lands on every one the same way — setTaskListOpen's rule for the one preference an umbrella has.
// There is no sweep to perform because no entry records the fold: the paint reads the field
// (render.go), and the fold is in every large umbrella's paint key (paintKey.folded), so the next
// frame draws each afresh. It reports whether the preference moved, so a caller that writes the
// key back on a change can tell a no-op apply.
func (t *transcript) setToolsOpen(open bool) bool {
	changed := t.toolsOpen != open
	t.toolsOpen = open
	t.touch()
	return changed
}

// setToolsFoldOver moves the type-row threshold an umbrella is large above
// ([transcript.toolsFoldOver]) and reports whether it moved. It touches for setToolsOpen's reason:
// which umbrellas are large is a paint fact (paintKey.large), and a `/settings` edit of the
// threshold under an open preference has to redraw the ▼ and its click target on every umbrella
// the edit moved across the line.
func (t *transcript) setToolsFoldOver(n int) bool {
	changed := t.toolsFoldOver != n
	t.toolsFoldOver = n
	t.touch()
	return changed
}

// umbrellaIsLarge reports whether the umbrella headed by entries[head] is LARGE — the shape whose
// fold is the shared preference's (toolsOpen) rather than its own head flag's
// ([transcript.blockFolded]). An umbrella is large by its SIZE alone: more type rows than
// toolsFoldOver, under a threshold above 0, since a threshold of 0 says no umbrella is ever large.
// Nothing else enters — not whether the Turn is running, not whether every member is done; those
// are the settled rule's, which auto-collapse asks BEFORE size ([transcript.blockFoldBy]) — so
// the answer is the same at every depth, in every view, live or replayed: a folded umbrella with a call
// still running paints its header live (blockState.live) rather than springing open, and the
// umbrella a sub-agent's run view shows while the parent Turn runs folds exactly as the transcript's
// own would at the prompt.
//
// head must be the umbrella's own head ([toolBlockAt]); a member past it, and an index heading no
// umbrella, answers false.
func (t *transcript) umbrellaIsLarge(head int) bool {
	b := toolBlockAt(t.entries, head)
	if b.kind != shapeToolSuper || b.head != head {
		return false
	}
	return t.toolsFoldOver > 0 && len(b.sup) > t.toolsFoldOver
}

// blockFoldRule names which rule decides an umbrella's fold ([transcript.blockFoldBy]) — the
// answer the painter reads as a fold and the reader's actions branch on, so a click on an
// auto-collapsed umbrella and one on a large umbrella under the shared preference can each move the
// fold that is actually holding it.
type blockFoldRule int

const (
	// foldByNone: the index heads no foldable block — there is no fold to read.
	foldByNone blockFoldRule = iota
	// foldBySettled: auto-collapse is on and the block — an umbrella (a group of one included), or
	// a Sub-agent or Skill group — is settled (its run has streamed reply text past it, or a later
	// entry follows it, or its run has ended; [transcript.blockSettled]) and untouched — folded.
	foldBySettled
	// foldByShared: a LARGE umbrella under the size rule — folded exactly when !toolsOpen.
	foldByShared
	// foldByHead: the head's own flag (entry.folded) — a SMALL umbrella under the size
	// rule, or a touched one whose reader gave it a fold of its own (entry.foldOwn).
	foldByHead
)

// blockFoldBy is the ONE statement of how a foldable tool block's fold is resolved, in order: with
// auto-collapse on (toolsAutoCollapse), a SETTLED block ([transcript.blockSettled] — its run has
// streamed reply text past it, or a later entry follows it, or its run is over) the reader
// has not touched is folded, whatever its size and toolsOpen; a touched one whose reader gave it its
// own fold (entry.foldOwn) follows its head flag; everything else — a live umbrella, the
// last entry of a running Turn, a touched one without a fold of its own, and every umbrella with
// auto-collapse off — folds by its size, a LARGE one ([transcript.umbrellaIsLarge]) under the shared
// preference and a SMALL one on its head flag. A lone groupable call is a group of one and folds by
// exactly these rules, so a reader who opened it can fold it again on its head flag. A
// Sub-agent or Skill GROUP takes the settled fold like an umbrella and has no size: past it, it
// folds on its head flag alone, live or settled, auto-collapse on or off. The paint
// ([transcript.blockFolded]) and the reader's fold actions both ask it, so the two can never
// disagree about which fold a header shows.
//
// Only the block's head ([toolBlockAt]) has a fold to resolve: a group's member past the first —
// where paint resumes a list an open member ended — and an umbrella call past the head answer
// foldByNone, as does every index in no foldable block.
func (t *transcript) blockFoldBy(head int) blockFoldRule {
	block := toolBlockAt(t.entries, head)
	if !block.foldable() || block.head != head {
		return foldByNone
	}
	headEntry := t.entries[head]
	switch {
	case t.toolsAutoCollapse && !headEntry.foldTouched && t.blockSettled(head, block.end):
		return foldBySettled
	case headEntry.foldTouched && headEntry.foldOwn:
		return foldByHead
	case block.kind == shapeToolSuper && t.umbrellaIsLarge(head):
		return foldByShared
	}
	return foldByHead
}

// blockFolded reports whether the umbrella headed by entries[head] stands folded to its header
// line — the one question the painter asks of a Tools umbrella (render.go's resolveBlock) — as the
// rule [transcript.blockFoldBy] names answers it: a settled, untouched block under
// auto-collapse is folded — from the moment its run's reply text first shows past it, and through
// a call joining it after a stream reset, unless a member waits on the human
// ([transcript.blockSettled]); otherwise a
// LARGE one is folded exactly when the shared preference is shut (!toolsOpen), its own head flag
// ignored, and a SMALL one — or a touched one with a fold of its own — exactly when its head's
// flag says so (entry.folded). A small umbrella that grows
// past the threshold therefore follows the preference from that append on, whatever its flag holds,
// and follows the flag again if the threshold is raised back over it. An index heading no umbrella
// answers false.
func (t *transcript) blockFolded(head int) bool {
	switch t.blockFoldBy(head) {
	case foldBySettled:
		return true
	case foldByShared:
		return !t.toolsOpen
	case foldByHead:
		return t.entries[head].folded
	}
	return false
}

// blockSettled reports whether the foldable block headed by entries[head] — a Tools umbrella (a
// group of one included), a Sub-agent or Skill group — is SETTLED: the work it folds is over and its run has
// moved on past it. It is derived at paint, like the umbrella's membership (toolSuperGroup), from
// two facts stored on the members themselves, and it settles by either of two rules.
//
// The REPLY-TEXT rule comes first, and it is sticky: the block is settled once its own run has
// streamed non-blank reply or narration text past it — one of its own members carries
// entry.passed, the mark that text leaves on the run's last entry ([transcript.passBlock]) — and
// it stays settled when a stream reset discards that text and the re-stream's call joins the
// block instead (an umbrella or a group grows while nothing commits between its members;
// toolSuperGroup), so a block the text folded does not spring open again. A Turn boundary alone
// marks nothing: a burst of Turns that each add calls, with no text between them, keeps the block
// open and growing. One thing lifts the mark, and the second rule then decides alone: a member
// WAITING ON THE HUMAN ([entry.waitsOnUser] — a pending ask_user, a call held on its approval
// pane), which keeps the block open while that wait lasts. A block the reader touched never
// reaches this question ([transcript.blockFoldBy] asks the head's foldTouched first). The rule
// never has to ask whether the marked block's calls returned: a run streams its next reply only
// after the engine answered every call its last Turn issued (the dispatch joins its calls before
// the Step ends), so a marked member always ends a block whose calls all reported, and only a
// call joining it after a reset can be in flight.
//
// The COMMITTED-FOLLOWER rule is the one the block always had: every member of it has finished
// ([entry.reported] — a call returned, a delegation's child reported), AND either a later entry of
// its own run follows its last member — a narration, a note, a user message, a further block — or
// nothing follows and its run is no longer running: for the human's own conversation, the Turn
// ([transcript.turnLive]); for a delegated run, the child, read off its head by the same rule,
// falling back to the Turn where the list holds no head for it. A replayed block is therefore
// settled by construction.
//
// The walk past the umbrella stops at the first entry standing at the run's own depth or shallower:
// a run's entries are contiguous behind its head ([transcript.runEnd]), so an entry there either is
// the run's own next entry or proves the run's stretch has ended — a sibling's, a parent's, a host
// note parked at the tail. Deeper entries between are runs nested inside this one, and are skipped.
//
// The block's span [head, end) is the caller's, read off the head ([toolBlockAt]), and both rules
// read only the block's OWN members — the entries at the head's depth in that span — never the
// deeper entries a group member's run holds, whose narration is never marked done and whose Turns
// are that child's; a delegation member is over when its own head says so, whatever its span holds.
func (t *transcript) blockSettled(head, end int) bool {
	depth := t.entries[head].depth
	passed, waiting, finished := false, false, true
	for i := head; i < end; i++ {
		e := t.entries[i]
		if e.depth != depth {
			continue
		}
		passed = passed || e.passed
		waiting = waiting || e.waitsOnUser()
		finished = finished && e.reported()
	}
	if passed && !waiting {
		return true
	}
	if !finished {
		return false
	}
	run := t.entries[head].run()
	for j := end; j < len(t.entries); j++ {
		if t.entries[j].depth <= run.depth {
			if t.entries[j].run() == run {
				return true
			}
			break
		}
	}
	if !run.isTop() {
		if at, ok := runHeadAt(t.entries, run); ok {
			return t.entries[at].reported()
		}
	}
	return !t.turnLive
}

// reported is subAgentReported's rule asked of the entry itself: its result has been paired with
// its call (done), or — for a delegation — its child reached its boundary and handed its report
// back (the FINISHED phase), which in a fan-out lands well ahead of the group's result burst. A
// call that heads no run never carries a phase, so for it this is done alone.
func (e entry) reported() bool {
	return e.done || e.phase == domain.SubAgentFinished
}

// waitsOnUser reports whether e is a call the run is blocked on the human for: an ask_user
// question not yet answered, or a call held on its approval pane (entry.heldForApproval). Either
// keeps a block its run's reply text settled open while the wait lasts ([transcript.blockSettled]),
// so the human can see the call they are being asked about.
func (e entry) waitsOnUser() bool {
	if e.kind != entryToolCall || e.reported() {
		return false
	}
	return e.heldForApproval || e.tool.name == askUserToolName
}

// passBlock marks the moment run's streamed reply or narration first turns non-blank
// ([transcript.appendToken]): the run's LAST own entry takes entry.passed, so the foldable block it
// ends is settled from here on — text now follows it ([transcript.blockSettled]). It touches only
// when the mark moves. A run with no entry of its own yet — a delegation whose first Turn committed
// nothing, a legacy child whose stretch ends behind host notes parked at the tail — marks nothing
// ([lastOwnEntry]), and only run's own entries are walked, so a delegate's text never settles its
// parent's block, nor a sibling's.
//
// Only an entry that ends a FOLDABLE block at this moment is marked ([toolBlockAt]) — an umbrella
// (a group of one included), a Sub-agent or Skill group (a lone delegation or skill fetch is a
// group of one) — because the mark is what keeps a block the text folded folded when a stream
// reset discards that text and the re-stream's call joins the block: a folded "✦ Sub-agents (1)"
// a re-streamed delegation joins stays folded and counts (2).
func (t *transcript) passBlock(run runRef) {
	at, ok := t.lastOwnEntry(run)
	if !ok || t.entries[at].passed {
		return
	}
	if !toolBlockAt(t.entries, at).foldable() {
		return
	}
	t.entries[at].passed = true
	t.touch()
}

// lastOwnEntry is the index of run's last own entry — the one standing at the run's depth and
// belonging to it — and whether it has one. The walk runs back from the run's end
// ([transcript.runEnd]) by blockSettled's discipline: deeper entries are runs nested inside this
// one and are skipped, and the first entry at the run's depth or shallower either is the run's own
// or proves its stretch holds nothing of its own past that point — a head, a sibling's entry, a
// host note — and answers none.
func (t *transcript) lastOwnEntry(run runRef) (int, bool) {
	for i := t.runEnd(run) - 1; i >= 0; i-- {
		if t.entries[i].depth > run.depth {
			continue
		}
		return i, t.entries[i].run() == run
	}
	return -1, false
}

// holdForApproval marks (held) or releases the call of run an Approval is about: the engine raises
// the gate for a call it has already announced, and the request names the call's tool but not its
// id, so the mark goes on run's LATEST own call to that tool that is still waiting for its result.
// That is the call the gate is for: a run's leaf calls are dispatched one at a time, each announced
// and then gated before the next is announced. The release finds the call holding the mark the same
// way. A call that never hears its decision — a cancel at the gate — is answered not-run, which
// ends the wait anyway ([entry.waitsOnUser] asks a reported call nothing). The walk stops at the
// run's last prompt: nothing before it can still be waiting. It touches only when a mark moves.
func (t *transcript) holdForApproval(tool string, run runRef, held bool) {
	for i := t.runEnd(run) - 1; i >= 0; i-- {
		e := t.entries[i]
		if e.depth > run.depth {
			continue
		}
		if e.run() != run || e.kind == entryUser {
			return
		}
		if e.kind == entryToolCall && e.tool.name == tool && !e.reported() && e.heldForApproval != held {
			t.entries[i].heldForApproval = held
			t.touch()
			return
		}
	}
}

// setToolsAutoCollapse moves the auto-collapse switch ([transcript.toolsAutoCollapse]) and reports
// whether it moved. It touches only on a move, so a settings apply that leaves it where it stood
// repaints nothing; a move redraws every settled block — umbrella (a group of one included),
// Sub-agent or Skill group — through its paint key (paintKey.folded).
func (t *transcript) setToolsAutoCollapse(on bool) bool {
	if t.toolsAutoCollapse == on {
		return false
	}
	t.toolsAutoCollapse = on
	t.touch()
	return true
}

// setTurnLive records whether the human's own Turn is running ([transcript.turnLive]) and reports
// whether it moved; it touches only on a move, so the Turn's end repaints the last umbrella into
// its settled fold and a resumed Step that was already running repaints nothing.
func (t *transcript) setTurnLive(live bool) bool {
	if t.turnLive == live {
		return false
	}
	t.turnLive = live
	t.touch()
	return true
}

// setBlockFolded folds the SMALL umbrella — or the Sub-agent or Skill group — headed by
// entries[head] to its header line or opens it again (entry.folded) and reports whether the flag
// moved. It sits where setTypeExpanded
// sits and takes its posture: only a tool call can head an umbrella, so every other kind and an
// index outside the slice answer false and change nothing, and it does NOT ask whether the entry
// heads an umbrella today, or a small one — the flag is written on the head and read only where the
// fold rule says to ([transcript.blockFoldBy]: an unsettled or touched umbrella the size calls
// small, or a touched one with a fold of its own), so a flag on an umbrella that has since grown large is
// a flag nothing reads until the threshold moves back over it. It touches only on a move, so the
// generation compare that reads it never repaints for a fold set to where it already stood.
func (t *transcript) setBlockFolded(head int, folded bool) bool {
	if head < 0 || head >= len(t.entries) || t.entries[head].kind != entryToolCall {
		return false
	}
	if t.entries[head].folded == folded {
		return false
	}
	t.entries[head].folded = folded
	t.touch()
	return true
}

// markBlockTouched records that the reader acted on the foldable tool block entries[index] belongs
// to ([toolBlockAt]) — opened a type row, a member or its body, or moved its header's fold —
// which exempts it from auto-collapse from then on ([transcript.blockFoldBy]), and reports
// whether the mark moved. It leaves the block's fold where it stands: a touched umbrella without
// a fold of its own folds by its size again, the fold it showed before it settled. An index in no
// foldable block changes nothing, and the generation moves only on a move, setBlockFolded's rule.
func (t *transcript) markBlockTouched(index int) bool {
	block := toolBlockAt(t.entries, index)
	if !block.foldable() {
		return false
	}
	e := &t.entries[block.head]
	if e.foldTouched {
		return false
	}
	e.foldTouched = true
	t.touch()
	return true
}

// openBlockAlone opens the Tools umbrella headed by entries[head] on a fold of its own: touched,
// so auto-collapse leaves it be, and holding its own head flag (entry.foldOwn) open, so
// neither its size nor the shared `ui.tools-open` preference decides it from here on — the
// reader's open of an auto-collapsed umbrella, which opens that one and writes nothing (ratified
// "Large-umbrella click"). A further click flips that same head flag (the header act,
// transcript.applyFoldAct), so a group of one — a lone call — re-folds like any umbrella. A
// Sub-agent or Skill group opens like an umbrella. It reports whether anything moved; an index heading no foldable block — a member
// past its block's head included ([toolBlockAt]) — changes nothing.
func (t *transcript) openBlockAlone(head int) bool {
	if block := toolBlockAt(t.entries, head); !block.foldable() || block.head != head {
		return false
	}
	e := &t.entries[head]
	if e.foldTouched && e.foldOwn && !e.folded {
		return false
	}
	e.foldTouched, e.foldOwn, e.folded = true, true, false
	t.touch()
	return true
}

// revealBlockMember makes the umbrella or group member entries[index] paintable, as a side-panel
// jump to it needs — the reveal act ([transcript.applyFoldAct]'s foldActReveal, which
// Model.followPanelTarget asks): a folded umbrella or group opens on a fold of its own
// ([transcript.openBlockAlone]) — never the shared toggle, whose flip writes `ui.tools-open` —
// an open one is marked touched, and the TYPE ROW holding the member opens, since a closed type row
// paints no member line for the jump to land on. A folded group of one or Sub-agent or Skill group
// opens the same way and has no type row to open — a group of one paints its call's own block under
// the header — so a flag left there would open a type row the moment the group grows. It reports
// whether a foldable block holds the index; an index in no foldable block changes nothing.
func (t *transcript) revealBlockMember(index int) bool {
	block := toolBlockAt(t.entries, index)
	if !block.foldable() {
		return false
	}
	if t.blockFolded(block.head) {
		t.openBlockAlone(block.head)
	} else {
		t.markBlockTouched(block.head)
	}
	if block.kind != shapeToolSuper || block.groupOfOne() {
		return true
	}
	for _, run := range block.sup {
		if index >= run.at && index < run.at+run.n {
			t.setTypeExpanded(run.at, true)
			break
		}
	}
	return true
}

// setTypeExpanded opens or closes the TYPE ROW of the run headed by entries[index] — the second,
// independent level of a super-group's state (entry.typeExpanded) — and reports whether it found a
// run head to set. Only a tool call can head a run (sameLabelRun), so every other kind answers false
// and changes nothing, as does an index outside the slice: this sits where setExpanded sits, on the
// path a click and a repaint share, where a panic is the whole session.
//
// It does NOT ask whether the entry heads a run TODAY. Membership is derived from the entries at
// query time and moves as they append (toolSuperGroup), so a flag written on an entry that later
// stops heading a run is simply a flag nothing reads — where a gate on the live derivation would
// make the same click succeed or fail depending on what the model called next. The flag survives
// either way, which is what lets a reader open a type row and keep it open while the umbrella grows
// beneath it.
func (t *transcript) setTypeExpanded(index int, expanded bool) bool {
	if index < 0 || index >= len(t.entries) || t.entries[index].kind != entryToolCall {
		return false
	}
	t.entries[index].typeExpanded = expanded
	t.touch()
	return true
}

// toggleTypeExpanded flips one type row between its two states and reports whether it found a run
// head to flip — the meaning of a click on a type row. The kind and range guards are
// setTypeExpanded's, so an index that heads nothing answers false from one place; the bound here
// only makes the READ of the current state safe.
func (t *transcript) toggleTypeExpanded(index int) bool {
	if index < 0 || index >= len(t.entries) {
		return false
	}
	return t.setTypeExpanded(index, !t.entries[index].typeExpanded)
}
