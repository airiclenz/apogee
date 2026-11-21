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

// TestToolBlockShapes pins the shape answer (toolBlockAt) asked at a block's head AND at every
// member: the umbrella (a group of one included), the Sub-agent and Skill groups (a lone delegation
// or skill fetch is a group of one), the breakers that end a block, and the calls and blocks that
// stay out of every group (ADR 0035 amendment 1120c). noFoldAt names indices that are inside a
// foldable block but head none, where blockFoldBy must answer foldByNone.
func TestToolBlockShapes(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		build    func(tr *transcript)
		want     map[int]shapeAnswer
		noFoldAt []int
	}{
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
	} {
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
		})
	}
}
