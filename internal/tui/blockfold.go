package tui

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
