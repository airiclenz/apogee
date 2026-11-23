package tui

import (
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// shapeAnswer is the part of a toolBlock a shape row pins: the fields, plus the member count and
// the two derived answers, so a row reads as the block it describes.
type shapeAnswer struct {
	kind       blockShape
	head       int
	pos        int
	end        int
	members    int // the umbrella's calls, or the group's members; 0 for shapeEntry
	isFoldable bool
	isOneGroup bool
}

// answerOf flattens a toolBlock into the shapeAnswer a row compares against.
func answerOf(b toolBlock) shapeAnswer {
	members := len(b.group)
	if b.kind == shapeToolSuper {
		members = b.sup.calls()
	}
	return shapeAnswer{
		kind: b.kind, head: b.head, pos: b.pos, end: b.end, members: members,
		isFoldable: b.foldable(), isOneGroup: b.groupOfOne(),
	}
}

// umbrellaAnswer is the answer every entry of an umbrella over [head, end) gives.
func umbrellaAnswer(head, end int) shapeAnswer {
	return shapeAnswer{kind: shapeToolSuper, head: head, end: end, members: end - head,
		isFoldable: true, isOneGroup: end-head == 1}
}

// loneAnswer is the answer an entry in no foldable block gives: a block of its own.
func loneAnswer(at int) shapeAnswer {
	return shapeAnswer{kind: shapeEntry, head: at, end: at + 1}
}

// blockShapeCase is one transcript of the shape table and the answers it pins. noFoldAt names
// indices that are inside a foldable block but head none, where blockFoldBy must answer foldByNone.
// noUmbrellaAt names indices asked straight of the umbrella's primitives — no groupable call there,
// and no umbrella headed — because the shape answer asks the Skill group before the umbrella, so its
// kind alone would not see a fetch turning groupable.
type blockShapeCase struct {
	name         string
	build        func(tr *transcript)
	want         map[int]shapeAnswer
	noFoldAt     []int
	noUmbrellaAt []int
}

// blockShapeCases is the shape table: the umbrella (a group of one included), the Sub-agent and
// Skill groups (a lone delegation or skill fetch is a group of one), the breakers that end a block,
// and the calls and blocks that stay out of every group (ADR 0035 amendment 1120c). Both the shape
// test and the paint/fold agreement test walk it, so the transcripts each pins are one list.
func blockShapeCases() []blockShapeCase {
	return []blockShapeCase{
		{
			name:  "a lone groupable call is a group of one",
			build: func(tr *transcript) { readCall(tr, "r1", "a.go", 1, 5, 0) },
			want:  map[int]shapeAnswer{0: umbrellaAnswer(0, 1)},
		},
		{
			name: "two runs are one umbrella, not a group of one",
			build: func(tr *transcript) {
				readCall(tr, "r1", "a.go", 1, 5, 0)
				readCall(tr, "r2", "b.go", 1, 5, 0)
				runCall(tr, "t1", "go test", "ok", 0)
			},
			want:     map[int]shapeAnswer{0: umbrellaAnswer(0, 3), 1: umbrellaAnswer(0, 3), 2: umbrellaAnswer(0, 3)},
			noFoldAt: []int{1, 2},
		},
		{
			name: "a lone delegation is a Sub-agents group of one",
			build: func(tr *transcript) {
				delegationCall(tr, "", "sa1", "scout", "look around", 0)
				subAgentReport(tr, "sa1", "done", 0)
			},
			want: map[int]shapeAnswer{0: {kind: shapeSubAgentGroup, head: 0, end: 1, members: 1,
				isFoldable: true, isOneGroup: true}},
		},
		{
			name:  "a lone skill fetch is a Skill group of one",
			build: func(tr *transcript) { skillFetch(tr, "s1", "go style", "# Go style") },
			want: map[int]shapeAnswer{0: {kind: shapeSkillGroup, head: 0, end: 1, members: 1,
				isFoldable: true, isOneGroup: true}},
		},
		{
			name: "a two-member Sub-agent group asked at its second member",
			build: func(tr *transcript) {
				delegationCall(tr, "", "sa1", "scout", "look around", 0) // 0
				delegatedRead(tr, "sa1", "r1", "a.go", 1)                // 1: sa1's span
				delegationCall(tr, "", "sa2", "scout", "look again", 0)  // 2
			},
			want: map[int]shapeAnswer{
				0: {kind: shapeSubAgentGroup, head: 0, pos: 0, end: 3, members: 2, isFoldable: true},
				1: umbrellaAnswer(1, 2), // a member's span holds its run's own blocks, never the group
				2: {kind: shapeSubAgentGroup, head: 0, pos: 1, end: 3, members: 2, isFoldable: true},
			},
			noFoldAt: []int{2},
		},
		{
			name: "a skill group between two umbrellas",
			build: func(tr *transcript) {
				readCall(tr, "r1", "a.go", 1, 5, 0)
				skillFetch(tr, "s1", "go style", "# Go style")
				skillFetch(tr, "s2", "go tests", "# Go tests")
				readCall(tr, "r2", "b.go", 1, 5, 0)
			},
			want: map[int]shapeAnswer{
				0: umbrellaAnswer(0, 1),
				1: {kind: shapeSkillGroup, head: 1, pos: 0, end: 3, members: 2, isFoldable: true},
				2: {kind: shapeSkillGroup, head: 1, pos: 1, end: 3, members: 2, isFoldable: true},
				3: umbrellaAnswer(3, 4),
			},
			noFoldAt: []int{2},
		},
		{
			// Each fetch carries a non-empty Target — what would otherwise make it groupable — and
			// the never-group mark keeps it out of the umbrella at every index one could form from
			// (TestSkillFetchesNeverJoinTheToolsSuperGroup).
			name: "two skill fetches never join the Tools umbrella",
			build: func(tr *transcript) {
				skillFetch(tr, "c1", "format Go", "<skill: Coding Standards>\nUse gofmt.\n</skill>\n")
				skillFetch(tr, "c2", "cut a release", "<skill: Brew Release>\nTag it.\n</skill>\n")
			},
			want: map[int]shapeAnswer{
				0: {kind: shapeSkillGroup, head: 0, pos: 0, end: 2, members: 2, isFoldable: true},
				1: {kind: shapeSkillGroup, head: 0, pos: 1, end: 2, members: 2, isFoldable: true},
			},
			noFoldAt:     []int{1},
			noUmbrellaAt: []int{0, 1},
		},
		{
			// An open member ends the group's block, so paint resumes the list at the member after
			// it: a walk index that is a member past the first (pos > 0) and heads no fold.
			name: "a Skill group resumed after an open member",
			build: func(tr *transcript) {
				skillFetch(tr, "s1", "go style", "# Go style")
				skillFetch(tr, "s2", "go tests", "# Go tests")
				skillFetch(tr, "s3", "go docs", "# Go docs")
				tr.entries[1].expanded = true
			},
			want: map[int]shapeAnswer{
				0: {kind: shapeSkillGroup, head: 0, pos: 0, end: 3, members: 3, isFoldable: true},
				1: {kind: shapeSkillGroup, head: 0, pos: 1, end: 3, members: 3, isFoldable: true},
				2: {kind: shapeSkillGroup, head: 0, pos: 2, end: 3, members: 3, isFoldable: true},
			},
			noFoldAt: []int{1, 2},
		},
		{
			name: "narration breaks an umbrella",
			build: func(tr *transcript) {
				readCall(tr, "r1", "a.go", 1, 5, 0)
				tr.apply(domain.MessageEvent{Text: "One more."})
				readCall(tr, "r2", "b.go", 1, 5, 0)
			},
			want: map[int]shapeAnswer{0: umbrellaAnswer(0, 1), 1: loneAnswer(1), 2: umbrellaAnswer(2, 3)},
		},
		{
			name: "a depth change breaks an umbrella",
			build: func(tr *transcript) {
				delegationCall(tr, "", "sa1", "scout", "look around", 0) // 0
				delegatedRead(tr, "sa1", "r1", "a.go", 1)                // 1
				delegatedRead(tr, "sa1", "r2", "b.go", 1)                // 2
				readCall(tr, "r3", "c.go", 1, 5, 0)                      // 3: back at depth 0
			},
			want: map[int]shapeAnswer{
				0: {kind: shapeSubAgentGroup, head: 0, end: 3, members: 1, isFoldable: true, isOneGroup: true},
				1: umbrellaAnswer(1, 3),
				2: umbrellaAnswer(1, 3),
				3: umbrellaAnswer(3, 4),
			},
			noFoldAt: []int{2},
		},
		{
			name: "a task-list card",
			build: func(tr *transcript) {
				tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "tl", Tool: "task_list", Arguments: []byte(`{}`)}})
				tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "tl", Content: "[ ] one"}})
			},
			want: map[int]shapeAnswer{0: loneAnswer(0)},
		},
		{
			name: "an answered ask_user",
			build: func(tr *transcript) {
				tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "q1", Tool: askUserToolName,
					Arguments: []byte(`{"question":"Which file?"}`)}})
				tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: "q1", Content: "a.go"}})
			},
			want: map[int]shapeAnswer{0: loneAnswer(0)},
		},
		{
			name: "a targetless call",
			build: func(tr *transcript) {
				tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "r1", Tool: "read_file", Arguments: []byte(`{}`)}})
			},
			want: map[int]shapeAnswer{0: loneAnswer(0)},
		},
		{
			name: "a workflow finish receipt",
			build: func(tr *transcript) {
				tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "f1", Tool: workflowFinishToolName,
					Arguments: []byte(`{"status":"ok","summary":"done"}`)}})
			},
			want: map[int]shapeAnswer{0: loneAnswer(0)},
		},
		{
			name: "a fan_out card",
			build: func(tr *transcript) {
				tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: "fo", Tool: fanOutToolName,
					Arguments: []byte(`{"task":"check {item}"}`)}})
			},
			want: map[int]shapeAnswer{0: loneAnswer(0)},
		},
		{
			name: "a background sub-agent",
			build: func(tr *transcript) {
				tr.apply(bgSubAgentCall("bg1", ""))
				tr.apply(bgSubAgentAnswer("bg1"))
			},
			want: map[int]shapeAnswer{0: loneAnswer(0)},
		},
		{
			name:  "a workflow block",
			build: func(tr *transcript) { tr.apply(startedWith("scan", "fix")) },
			want:  map[int]shapeAnswer{0: loneAnswer(0)},
		},
	}
}

// TestToolBlockShapes pins the shape answer (toolBlockAt) asked at a block's head AND at every
// member, over every transcript of the shape table (blockShapeCases).
func TestToolBlockShapes(t *testing.T) {
	t.Parallel()

	for _, tc := range blockShapeCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tr := &transcript{}
			tc.build(tr)

			if len(tr.entries) != len(tc.want) {
				t.Fatalf("setup: built %d entries; the row answers for %d", len(tr.entries), len(tc.want))
			}
			for at, want := range tc.want {
				if got := answerOf(toolBlockAt(tr.entries, at)); got != want {
					t.Errorf("toolBlockAt(%d) = %+v; want %+v", at, got, want)
				}
			}
			for _, at := range tc.noFoldAt {
				if got := tr.blockFoldBy(at); got != foldByNone {
					t.Errorf("blockFoldBy(%d) = %d at a member past its block's head; want foldByNone", at, got)
				}
			}
			for _, at := range tc.noUmbrellaAt {
				if groupable(tr.entries[at].tool) {
					t.Errorf("entry %d is groupable; it would fold into a Tools umbrella", at)
				}
				if g := toolSuperGroup(tr.entries, at); g != nil {
					t.Errorf("toolSuperGroup(entries, %d) = %v; it heads no umbrella", at, g)
				}
			}
		})
	}
}

// walkedBlock is one block the paint walk laid down: the walk index it started at, the shape the
// painter answered with, and where the walk resumed.
type walkedBlock struct {
	at    int
	shape blockShape
	next  int
}

// paintWalk steps tr exactly as renderView does — over the paint root's window, past every retired
// item attempt, from one resolveBlock answer to the next — and returns each block it resolved. A
// block that does not move the walk forward fails the test rather than looping.
func paintWalk(t *testing.T, tr *transcript) []walkedBlock {
	t.Helper()

	root := tr.paintRoot()
	retired := retiredAttempts(tr.entries)
	var walked []walkedBlock
	var records []paintInput
	for i := root.first; i < root.last; {
		if retired[i] {
			i += 1 + subAgentSpan(tr.entries, i)
			continue
		}
		block := tr.resolveBlock(i, root.painted(&tr.entries[i]), 80, false, root, records)
		records = block.ins
		if block.next <= i {
			t.Fatalf("resolveBlock(%d) resumes the walk at %d; it must move forward", i, block.next)
		}
		walked = append(walked, walkedBlock{at: i, shape: block.shape, next: block.next})
		i = block.next
	}
	return walked
}

// memberIndices is every entry the block answers for as one of its own heads: each umbrella call,
// and each group member's call. A member's span is its run's own blocks, so it is not listed.
func memberIndices(b toolBlock) []int {
	var at []int
	switch b.kind {
	case shapeToolSuper:
		for i := b.head; i < b.end; i++ {
			at = append(at, i)
		}
	case shapeSubAgentGroup, shapeSkillGroup:
		for _, m := range b.group {
			at = append(at, m.at)
		}
	}
	return at
}

// TestPaintAndFoldAgreeWithTheBlockShape pins paint and fold to the one shape answer: at every
// index the paint walk reaches over a shape-table transcript, with auto-collapse off and on, the
// painted shape is toolBlockAt's kind (a run head's shapeSubAgentRun is a shapeEntry there), the
// walk stands on the block's head unless it resumes a group (pos > 0, head before it), a foldable
// block's step stays inside it and any other block steps one entry or over its span, every umbrella
// call and group member answers the same head, and blockFoldBy finds a fold exactly at a foldable
// block's head.
func TestPaintAndFoldAgreeWithTheBlockShape(t *testing.T) {
	t.Parallel()

	for _, tc := range blockShapeCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tr := &transcript{}
			tc.build(tr)

			for _, autoCollapse := range []bool{false, true} {
				tr.setToolsAutoCollapse(autoCollapse)
				for _, w := range paintWalk(t, tr) {
					b := toolBlockAt(tr.entries, w.at)
					painted := w.shape
					if painted == shapeSubAgentRun {
						painted = shapeEntry
					}
					if painted != b.kind {
						t.Errorf("autoCollapse=%v: painted shape %d at %d; toolBlockAt answers %d",
							autoCollapse, w.shape, w.at, b.kind)
					}
					if b.pos == 0 && b.head != w.at {
						t.Errorf("autoCollapse=%v: walk index %d at pos 0 answers head %d; want the index",
							autoCollapse, w.at, b.head)
					}
					if b.pos > 0 && b.head >= w.at {
						t.Errorf("autoCollapse=%v: walk index %d resumes a group at pos %d with head %d; want a head before it",
							autoCollapse, w.at, b.pos, b.head)
					}
					if b.foldable() {
						if w.next > b.end {
							t.Errorf("autoCollapse=%v: the block painted at %d resumes at %d, past its end %d",
								autoCollapse, w.at, w.next, b.end)
						}
					} else if span := subAgentSpan(tr.entries, w.at); w.next != w.at+1 && w.next != w.at+1+span {
						t.Errorf("autoCollapse=%v: the entry painted at %d resumes at %d; want %d or %d",
							autoCollapse, w.at, w.next, w.at+1, w.at+1+span)
					}
					for _, at := range memberIndices(b) {
						if got := toolBlockAt(tr.entries, at).head; got != b.head {
							t.Errorf("autoCollapse=%v: toolBlockAt(%d).head = %d; the block painted at %d has head %d",
								autoCollapse, at, got, w.at, b.head)
						}
					}
					fold := tr.blockFoldBy(w.at)
					if b.pos == 0 && (fold != foldByNone) != b.foldable() {
						t.Errorf("autoCollapse=%v: blockFoldBy(%d) = %d; foldable() = %v",
							autoCollapse, w.at, fold, b.foldable())
					}
					if b.pos > 0 && fold != foldByNone {
						t.Errorf("autoCollapse=%v: blockFoldBy(%d) = %d at a resumed group member; want foldByNone",
							autoCollapse, w.at, fold)
					}
				}
			}
		})
	}
}

// rowGroupingArgs carries an argument for every key a registry row's target extractor reads, so one
// call to any row is a TARGETED call — the precondition for an umbrella row to group at all
// (groupable). A row with no extractor (task_list) stays targetless whatever it is handed.
const rowGroupingArgs = `{"path":"a.go","pattern":"needle","source":"a.go","destination":"b.go","id":1,` +
	`"input":"ls","base":"main","head":"dev","ref":"HEAD","command":"go test","code":"print(1)",` +
	`"message":"fix it","action":"list","name":"scout","url":"https://example.com","method":"GET",` +
	`"query":"go style","task":"look around","question":"Ship it?","title":"Notes"}`

// ownShapeByTool is the group shape each groupOwn row's calls fold into, stated by name so a new
// groupOwn row has to say which list it heads.
var ownShapeByTool = map[string]blockShape{
	"sub_agent":  shapeSubAgentGroup,
	"load_skill": shapeSkillGroup,
}

// TestEveryRowGroupsByItsClass proves the shape answer reads a call's grouping class and nothing
// else: for every registry row, two adjacent finished calls fold into the mixed umbrella when the
// row is groupUmbrella, into a group of their own when it is groupOwn, and stay two blocks of their
// own when it is groupSolo. ask_user's calls finish with an ERROR, because an answered question is
// made solo at result time (askUserAnswerRecord) — the one verdict a class does not state — and an
// errored one keeps its row's class. A background sub-agent call reads its own groupSolo presenter,
// so two of them never form a "✦ Sub-agents (N)" group.
func TestEveryRowGroupsByItsClass(t *testing.T) {
	t.Parallel()

	twoCalls := func(t *testing.T, tool, args string, failed bool) *transcript {
		t.Helper()
		tr := &transcript{}
		for _, id := range []string{"c1", "c2"} {
			tr.apply(domain.ToolCallEvent{Call: domain.ToolCall{ID: id, Tool: tool, Arguments: []byte(args)}})
			tr.apply(domain.ToolResultEvent{Result: domain.ToolResult{CallID: id, Content: "done", IsError: failed}})
		}
		if len(tr.entries) != 2 {
			t.Fatalf("setup: two %s calls built %d entries; want 2", tool, len(tr.entries))
		}
		for at, e := range tr.entries {
			if e.kind != entryToolCall || !e.done {
				t.Fatalf("setup: entry %d of %s is not a finished call (kind %v, done %v)", at, tool, e.kind, e.done)
			}
		}
		return tr
	}

	for name, p := range toolRegistry {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			tr := twoCalls(t, name, rowGroupingArgs, name == askUserToolName)
			switch p.grouping {
			case groupUmbrella:
				if tr.entries[0].tool.Target == "" {
					t.Fatalf("setup: %s presented no target from rowGroupingArgs; an umbrella row needs one to group", name)
				}
				for at := range tr.entries {
					if got, want := answerOf(toolBlockAt(tr.entries, at)), umbrellaAnswer(0, 2); got != want {
						t.Errorf("toolBlockAt(%d) = %+v; an umbrella row's two calls want %+v", at, got, want)
					}
				}
			case groupOwn:
				kind, ok := ownShapeByTool[name]
				if !ok {
					t.Fatalf("groupOwn row %q has no entry in ownShapeByTool; state the group it forms", name)
				}
				for at := range tr.entries {
					want := shapeAnswer{kind: kind, head: 0, pos: at, end: 2, members: 2, isFoldable: true}
					if got := answerOf(toolBlockAt(tr.entries, at)); got != want {
						t.Errorf("toolBlockAt(%d) = %+v; an own row's two calls want %+v", at, got, want)
					}
				}
			case groupSolo:
				for at := range tr.entries {
					if got, want := answerOf(toolBlockAt(tr.entries, at)), loneAnswer(at); got != want {
						t.Errorf("toolBlockAt(%d) = %+v; a solo row's call is a block of its own, want %+v", at, got, want)
					}
				}
			default:
				t.Fatalf("row %q has grouping class %d, which this test does not know", name, p.grouping)
			}
		})
	}

	t.Run("background sub-agent calls", func(t *testing.T) {
		t.Parallel()

		tr := twoCalls(t, subAgentToolName, `{"task":"survey","background":true}`, false)
		for at, e := range tr.entries {
			if !e.tool.background {
				t.Fatalf("setup: entry %d is not a background sub-agent call", at)
			}
			if got, want := answerOf(toolBlockAt(tr.entries, at)), loneAnswer(at); got != want {
				t.Errorf("toolBlockAt(%d) = %+v; a background sub-agent call is a block of its own, want %+v", at, got, want)
			}
		}
	})

	for name := range ownShapeByTool {
		if p, ok := toolRegistry[name]; !ok || p.grouping != groupOwn {
			t.Errorf("ownShapeByTool names %q, which is no groupOwn registry row", name)
		}
	}
}
