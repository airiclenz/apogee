package tui

// blockshape.go is the one home of tool-block shape: the walk primitives that say which tool block
// an entry belongs to — the umbrella over groupable calls (toolSuperGroup, built from same-label
// runs), the Sub-agent and Skill groups (ownGroup, ownGroupAt and their delegation forms) — the
// blockShape enum naming which shape the painter answered with, and the shape answer built from
// them ([toolBlockAt]: which block entries[i] is in, its head, end and member position). Paint, fold
// and hit-testing ask these rather than restating the order or the group-of-one rule.

// blockShape names which shape [transcript.resolveBlock] answered with for a block — the shape
// itself, not anything derived from it. Every value is spent by that one resolver, as the shape
// field of the [resolvedBlock] it returns, so the enum and the decision that picks from it cannot
// drift apart. The shape answer ([toolBlock.kind]) names the foldable tool blocks with the same
// values, so the fold side and the painter speak of one shape by one name.
//
// It was redundant when there were three, and deliberately kept: a head that changed branches also
// changed the length of its span, so the flags string already differed (a sub-agent call whose first
// nested entry has not arrived is an ordinary one-entry tool block, and becomes a two-entry run's
// head the moment it does). That redundancy was an accident of the branches renderView happened to
// have, not a property of the design — and the cost of relying on it was that a fourth painter
// sharing a span length with a third would serve the wrong paint with no test able to see it.
// Naming the branch costs an int.
type blockShape int

const (
	shapeEntry         blockShape = iota // renderEntryLines — one entry, one block
	shapeSubAgentRun                     // renderSubAgentRun — the call plus the run nested under it
	shapeToolSuper                       // renderSuperGroup — the umbrella over groupable calls, a group of one included
	shapeSubAgentGroup                   // renderSubAgentGroup — the folded list of adjacent delegations
	shapeSkillGroup                      // renderSubAgentGroup — the folded list of adjacent skill fetches
)

// toolRun is one RUN of tool calls: the maximal stretch of adjacent entries carrying the same
// friendly Label at the same sub-agent depth, every one of them foldable into a member row
// (groupable). It is the unit the umbrella is built from — one row per run, and an umbrella is one
// such run or two or more runs under one header — and a lone call is a run of 1, the umbrella of
// a group of one (docs/layout/tool-layout.md, "Vocabulary").
//
// It is stated as an index pair rather than as the views it covers because the INDEX is what the
// levels above it need: the head entry is where the run's type-row state lives (typeExpanded) and
// what a click on that row resolves to, and each member is one entry, so a member row's own entry is
// at + n.
type toolRun struct {
	at int // index into the entries slice of the run's first call
	n  int // how many calls the run holds; never 0
}

// superGroup is the umbrella: the adjacent runs that fold under one "✦ Read 3 files, ran 4 commands" header, in
// TIME ORDER, which is the order they appear in the transcript. Calls are never reordered to merge
// same-type calls that were not adjacent (docs/layout/tool-layout.md): `read, terminal, read` is
// three runs and therefore three rows, not two.
type superGroup []toolRun

// calls is N — the total number of calls the umbrella holds, which its header states. It is also the
// number of ENTRIES the umbrella covers, because every member of every run is exactly one entry and
// the runs are adjacent by construction: a walk over the entries skips the whole umbrella by adding
// it to the head's index.
func (g superGroup) calls() int {
	n := 0
	for _, r := range g {
		n += r.n
	}
	return n
}

// sameLabelRun is the length of the run entries[i] opens: the adjacent calls that carry the same
// friendly Label at the same sub-agent depth and can each be a member row. It answers 0 when
// entries[i] opens no run at all — anything that is not a tool call, and a call the presenter marked
// solo or left without a target to lead a row (groupable).
//
// Any other entry between two calls ends the run where it stands, since the scan only ever walks
// forward over ADJACENT entries: narration, a note, an approval, an error. Two different tools
// sharing a label group all the same — the reader groups by what the row says, not by tool id.
func sameLabelRun(entries []entry, i int) int {
	if i < 0 || i >= len(entries) {
		return 0
	}
	head := entries[i]
	if head.kind != entryToolCall || !groupable(head.tool) {
		return 0
	}
	n := 1
	for j := i + 1; j < len(entries); j++ {
		e := entries[j]
		if e.kind != entryToolCall || e.depth != head.depth || e.tool.Label != head.tool.Label || !groupable(e.tool) {
			break
		}
		n++
	}
	return n
}

// toolSuperGroup is the umbrella entries[i] heads, or nil when it heads none: the adjacent
// same-depth runs of groupable calls starting there. ONE CALL is the floor (plan
// "2026-11-20 - 00", item 3; it was two since plan "2026-09-02 - 01", item 3): a LONE groupable
// call is a group of one — `✦ Read 1 file` over the call's own block — that folds, opens and
// re-folds by every rule a larger umbrella does. A run of 2+ same-label calls folds under the
// umbrella exactly as adjacent runs of different labels do, so four terminals are
// `✦ Ran 4 commands` over one `┕ Terminal (4)` type row, and a Read followed by a Terminal is that
// same umbrella over two rows.
//
// Adjacent runs differ in label BY CONSTRUCTION rather than by a test here: sameLabelRun is maximal,
// so a call carrying the previous run's label was already absorbed by it and a call that opens a new
// run necessarily carries a different one.
//
// The breakers are the group's own, one rule stated once: anything that is not a call opening a run
// at the umbrella's OWN depth ends it where it stands. That covers the non-tool entries a same-label
// run already broke on — narration, a note, an approval, an error — and, through groupable's solo
// mark, the two calls that group with their OWN kind: a sub-agent call, whose block heads a whole
// run of its own, and a skill fetch, which folds into "✦ Skill (N)" (ownGroup); neither ever joins
// an umbrella (spec Rules: "a sub-agent block or group breaks the run", and the same sentence for a
// skill block or group). The deeper entries a delegation leaves behind break the run by depth, as
// does a nested call at any other level.
//
// Membership is DERIVED here and stored nowhere, which is what makes formation live (design call 2):
// the umbrella exists the moment the FIRST groupable call is placed — the running call being its
// one member, spinner star and all — and grows as further calls append, whether a call opens a run
// of its own or extends the last, with no membership recorded anywhere that could fall out of date
// behind them.
func toolSuperGroup(entries []entry, i int) superGroup {
	n := sameLabelRun(entries, i)
	if n == 0 {
		return nil
	}
	runs := superGroup{{at: i, n: n}}
	for at := i + n; at < len(entries) && entries[at].depth == entries[i].depth; {
		m := sameLabelRun(entries, at)
		if m == 0 {
			break
		}
		runs = append(runs, toolRun{at: at, n: m})
		at += m
	}
	return runs
}

// groupBlock is one member of a folded group of same-tool calls: the index of the call entry and
// the length of the run nested beneath it ([subAgentSpan]). The span is carried because it is what
// separates one member from the next — a delegation's whole span lies between its call and its
// neighbour's — so a walk over the group's members steps by 1+span rather than by 1. A tool that
// heads no run answers 0 there and the same walk steps by 1, which is what lets one rule serve a
// fan-out of delegations and a run of skill fetches alike.
type groupBlock struct {
	at   int // index into the entries slice of the call entry itself
	span int // how many nested entries the call left behind it; 0 for one that produced none
}

// ownHeads reports whether entries[i] is a call to the tool `name` — a head that groups with its
// OWN kind. An index outside the list answers false, which is what lets callers hand it the result
// of a walk that may have found nothing. It matches the RETAINED tool name rather than the friendly
// label for [toolView.headsRun]'s reason: a relabelling must not switch the rule off, and a
// third-party tool that happens to share the label must not switch it on.
//
// A background sub_agent's call (toolView.background) is no delegation's head, so it never joins a
// "✦ Sub-agents (N)" group either.
func ownHeads(entries []entry, i int, name string) bool {
	return i >= 0 && i < len(entries) && entries[i].kind == entryToolCall && entries[i].tool.name == name &&
		!entries[i].tool.background
}

// ownGroup is the group entries[i] opens, or nil when it opens none: the adjacent calls to the tool
// `name` standing at the same depth, each with the run nested beneath it
// (docs/layout/tool-layout.md, Rules — "Sub-agent calls group with each other"). There is no floor:
// a lone call is a group of one ("✦ Sub-agents (1)", "✦ Skill (1)"), which folds, opens and
// re-folds like any group, exactly as a lone groupable call is an umbrella of one (toolSuperGroup).
//
// Adjacency here is adjacency of BLOCKS, not of entries, which is the whole reason this rule cannot
// be [sameLabelRun] with the solo mark lifted: a delegation's span sits between its call and the
// next call at its own depth, so two delegations are never neighbouring entries once either of them
// has done any work. The walk therefore steps over each member's span, and everything else the
// group rule needs follows from that: an entry of any other kind between two members — a
// narration, a note, an approval — is not a head at the group's depth and ends it where it stands,
// and so does a call to any other tool.
//
// A member with NO span joins like any other (a child refused at the depth bound, one that
// faulted before its first event): it is a call to the same tool, its row says so, and the reader
// grouping two of them is reading the same list. That is the one rule the presenter's solo mark
// used to state from the other side (presentToolCall) — solo still keeps such a call out of a MIXED
// super-group (design call 12), which is a different question and is answered where it always was
// (groupable). A tool that heads no run at all — a skill fetch — has a span of 0 at every member,
// so the very same walk steps by one and the list it returns is the list of its own calls.
func ownGroup(entries []entry, i int, name string) []groupBlock {
	if !ownHeads(entries, i, name) {
		return nil
	}
	var group []groupBlock
	for at := i; ownHeads(entries, at, name) && entries[at].depth == entries[i].depth; {
		span := subAgentSpan(entries, at)
		group = append(group, groupBlock{at: at, span: span})
		at += 1 + span
	}
	return group
}

// subAgentGroup is [ownGroup] asked of the delegation tool — the fan-out list every rule about
// grouped sub-agents is written against.
func subAgentGroup(entries []entry, i int) []groupBlock {
	return ownGroup(entries, i, subAgentToolName)
}

// ownGroupAt is the group entries[i] BELONGS to and its position in it — what the painter asks
// at a block head, where [ownGroup] alone would answer about the members from i onward and so
// would grow a second "✦ Sub-agents (N)" header at every member of one group.
//
// It walks back to the group's first member before walking forward, and the backward step is exact
// for the same reason the forward one is: a member's span is contiguous and ends where the next
// member begins (transcript.place), so the entry at the group's own depth immediately before i is
// the previous member's head whenever there is one.
//
// ok is false for anything that is in no group at all, which is every entry that is not a call to
// that tool; a call to it standing alone is the one member of a group of one.
func ownGroupAt(entries []entry, i int, name string) (group []groupBlock, pos int, ok bool) {
	if !ownHeads(entries, i, name) {
		return nil, 0, false
	}
	first := i
	for {
		prev := prevSibling(entries, first)
		if !ownHeads(entries, prev, name) || entries[prev].depth != entries[i].depth {
			break
		}
		first = prev
	}
	group = ownGroup(entries, first, name)
	for k := range group {
		if group[k].at == i {
			return group, k, true
		}
	}
	return nil, 0, false
}

// subAgentGroupAt is [ownGroupAt] asked of the delegation tool.
func subAgentGroupAt(entries []entry, i int) (group []groupBlock, pos int, ok bool) {
	return ownGroupAt(entries, i, subAgentToolName)
}

// toolBlock is the shape answer: which tool block an entry belongs to, asked from any of its entries
// ([toolBlockAt]). kind is shapeToolSuper for a Tools umbrella (a group of one included),
// shapeSubAgentGroup or shapeSkillGroup for a folded list of delegations or skill fetches, and
// shapeEntry for everything else — an entry that is a block of its own and folds as none.
//
// The block covers [head, end): an umbrella's calls, or a group's members together with every
// member's span. pos is the asked entry's member index inside a group — 0 at the group's first
// member, which alone carries its header and its fold — and 0 for every other shape. sup is the
// umbrella's runs and group the group's members; each is nil for every other shape.
type toolBlock struct {
	kind  blockShape
	head  int // index of the block's first entry: the umbrella's first call, the group's first member
	pos   int // member index inside a Sub-agent or Skill group; 0 for every other shape
	end   int // index one past the block's last entry, the last member's span included
	sup   superGroup
	group []groupBlock
}

// foldable reports whether the block folds to a header of its own: a Tools umbrella, a Sub-agent
// group or a Skill group. Its fold lives on the head's entry, so a caller asking about the fold of
// an index that is not the head ([toolBlock.head]) asks about a block that index does not head.
func (b toolBlock) foldable() bool {
	switch b.kind {
	case shapeToolSuper, shapeSubAgentGroup, shapeSkillGroup:
		return true
	}
	return false
}

// groupOfOne reports whether the block is a group of one: an umbrella over one call, or a Sub-agent
// or Skill group of one member (ADR 0035 amendment 1120c). Such a block has no type row — its
// header folds the call's own block — so nothing inside it is a type row to open.
func (b toolBlock) groupOfOne() bool {
	switch b.kind {
	case shapeToolSuper:
		return b.sup.calls() == 1
	case shapeSubAgentGroup, shapeSkillGroup:
		return len(b.group) == 1
	}
	return false
}

// toolBlockAt is the shape of the tool block entries[i] belongs to, asked from ANY entry of it — the
// head, a type row's head, any umbrella call, any group member. It is the one place the block order
// is stated: a Sub-agent group first, then a Skill group, then the umbrella; no entry answers two,
// since a group's members are solo calls and so never groupable.
//
// A group member finds its group's first member by [ownGroupAt]'s walk back. An umbrella call finds
// its head by walking back over the adjacent groupable calls at its own depth — the stretch
// toolSuperGroup consumes whole, since it is only correct asked at a block head. An entry inside a
// group member's span belongs to that run's own blocks, never to the group, and every entry that
// is in neither shape — narration, a solo or targetless call, a background sub-agent, a workflow
// block, an index outside the list — answers shapeEntry over [i, i+1).
func toolBlockAt(entries []entry, i int) toolBlock {
	if group, pos, ok := subAgentGroupAt(entries, i); ok {
		return groupShape(shapeSubAgentGroup, group, pos)
	}
	if group, pos, ok := ownGroupAt(entries, i, loadSkillToolName); ok {
		return groupShape(shapeSkillGroup, group, pos)
	}
	lone := toolBlock{kind: shapeEntry, head: i, end: i + 1}
	if sameLabelRun(entries, i) == 0 {
		return lone
	}
	head := i
	for head > 0 && extendsGroupableStretch(entries, head) {
		head--
	}
	sup := toolSuperGroup(entries, head)
	end := head + sup.calls()
	if i >= end {
		return lone
	}
	return toolBlock{kind: shapeToolSuper, head: head, end: end, sup: sup}
}

// groupShape is the shape answer for a member at pos of a Sub-agent or Skill group: the block
// starts at the first member and reaches past the last member's span.
func groupShape(kind blockShape, group []groupBlock, pos int) toolBlock {
	last := group[len(group)-1]
	return toolBlock{kind: kind, head: group[0].at, pos: pos, end: last.at + 1 + last.span, group: group}
}

// extendsGroupableStretch reports whether the entry before entries[i] is a groupable call at i's
// depth — the stretch toolSuperGroup consumes whole — so entries[i] is not where that stretch
// starts. i must be a groupable call itself, as [toolBlockAt] asks it only of one.
func extendsGroupableStretch(entries []entry, i int) bool {
	if i <= 0 || i >= len(entries) {
		return false
	}
	prev := entries[i-1]
	return prev.kind == entryToolCall && prev.depth == entries[i].depth && groupable(prev.tool)
}
