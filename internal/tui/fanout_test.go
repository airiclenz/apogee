package tui

import (
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// Per-child blocks (ADR 0039 — a concurrent depth-0 fan-out in the transcript)
// ----------------------------------------------------------------------------
//
// A fan-out delivers one interleaved stream: the parent announces every sub_agent call first, then
// N children emit into the same sink at the same depth, and each result folds back at the end. What
// the human must see is N blocks, one per child, each holding its own child's work and ticking with
// its own live tail. The tests below pin that grouping — and, just as hard, pin that a session with
// one delegate at a time renders exactly as it did before any of this existed.

// childCall folds a complete tool call — announced, then answered — made by the child that the
// sub_agent call spawn opened. Every event carries the spawning call's id, which is what the fold
// groups by (domain.EventBase.CallID).
func childCall(tr *transcript, spawn, id, path string) {
	openChildCall(tr, spawn, id, path)
	tr.apply(domain.ToolResultEvent{
		EventBase: domain.EventBase{Depth: 1, CallID: spawn},
		Result:    domain.ToolResult{CallID: id, Content: "1 - 10"},
	})
}

// openChildCall folds a tool call the child has NOT yet got a result for — the block's live tail
// reads off exactly this, so a run under test needs one to be working at all.
func openChildCall(tr *transcript, spawn, id, path string) {
	tr.apply(domain.ToolCallEvent{
		EventBase: domain.EventBase{Depth: 1, CallID: spawn},
		Call:      domain.ToolCall{ID: id, Tool: "read_file", Arguments: []byte(`{"path":"` + path + `"}`)},
	})
}

// headIndex finds the index of the sub_agent call block that opened the run spawn names.
func headIndex(t *testing.T, tr *transcript, spawn string) int {
	t.Helper()
	for i := range tr.entries {
		if e := tr.entries[i]; e.kind == entryToolCall && e.callID == spawn && e.tool.name == subAgentToolName {
			return i
		}
	}
	t.Fatalf("no sub_agent head for %q in %d entries", spawn, len(tr.entries))
	return -1
}

// TestConcurrentChildrenGroupByTheirSpawningCall is the grouping itself: two children emitting into
// one interleaved stream produce two contiguous runs, in the order their calls were announced, each
// holding only its own child's entries. Depth cannot tell them apart — siblings share it — so the
// span behind each head is derived from the run identity the fold placed the entry under.
func TestConcurrentChildrenGroupByTheirSpawningCall(t *testing.T) {
	t.Parallel()

	tr := &transcript{}
	subAgentCall(tr, "s1", "survey the tests", 0)
	subAgentCall(tr, "s2", "survey the docs", 0)

	// The pool delivers them interleaved, which is the whole point of the fixture.
	childCall(tr, "s1", "a1", "alpha.go")
	childCall(tr, "s2", "b1", "beta.md")
	childCall(tr, "s1", "a2", "alpha_test.go")
	childCall(tr, "s2", "b2", "beta_test.md")

	first, second := headIndex(t, tr, "s1"), headIndex(t, tr, "s2")
	if !(first < second) {
		t.Fatalf("blocks are out of call order: s1 at %d, s2 at %d", first, second)
	}

	for _, tc := range []struct {
		spawn string
		want  []string
	}{
		{spawn: "s1", want: []string{"alpha.go", "alpha_test.go"}},
		{spawn: "s2", want: []string{"beta.md", "beta_test.md"}},
	} {
		head := headIndex(t, tr, tc.spawn)
		span := tr.entries[head+1 : head+1+subAgentSpan(tr.entries, head)]
		if len(span) != len(tc.want) {
			t.Fatalf("run %s spans %d entries, want %d:\n%s", tc.spawn, len(span), len(tc.want), plainRender(tr))
		}
		for i, e := range span {
			if e.spawnCallID != tc.spawn {
				t.Errorf("run %s, entry %d: spawnCallID %q, want the run's own", tc.spawn, i, e.spawnCallID)
			}
			if got := e.tool.Target; got != tc.want[i] {
				t.Errorf("run %s, entry %d: target %q, want %q", tc.spawn, i, got, tc.want[i])
			}
		}
	}
}

// TestEachRunningChildBlockShowsItsOwnLiveTail pins the live tail per child: while a fan-out runs,
// every child's block carries ITS OWN count and ITS OWN context fill on the one summary line the
// collapsed cap gives it. This is what the status line cannot do once several delegates work at once
// — it can name only one — so it is what makes a fan-out observable.
//
// What no child's line carries is the call it has open: that cell was dropped for the flicker it was
// (subAgentGist), and the two facts left are the durable ones. Neither may cross to a sibling, which
// is the half the run identity earns and the reason both are asserted from both directions.
func TestEachRunningChildBlockShowsItsOwnLiveTail(t *testing.T) {
	t.Parallel()

	tr := &transcript{}
	subAgentCall(tr, "s1", "survey the tests", 0)
	subAgentCall(tr, "s2", "survey the docs", 0)

	childCall(tr, "s1", "a1", "alpha.go")
	openChildCall(tr, "s2", "b1", "beta.md")
	openChildCall(tr, "s1", "a2", "alpha_test.go")

	// A reading from each child, attributed by the spawning call rather than by depth.
	tr.applyUsage(domain.UsageEvent{
		EventBase: domain.EventBase{Depth: 1, CallID: "s1"}, TotalTokens: 12000,
	}, 32000, "")
	tr.applyUsage(domain.UsageEvent{
		EventBase: domain.EventBase{Depth: 1, CallID: "s2"}, TotalTokens: 4000,
	}, 32000, "")

	if got := tr.entries[headIndex(t, tr, "s2")].ctxUsed; got != 4000 {
		t.Errorf("the second child's block reports a fill of %d, want its own 4000", got)
	}

	lines := strings.Split(plainRender(tr), "\n")
	summary := func(task string) string {
		t.Helper()
		for _, ln := range lines {
			if strings.Contains(ln, task) {
				return ln
			}
		}
		t.Fatalf("no block line for %q in:\n%s", task, strings.Join(lines, "\n"))
		return ""
	}

	first, second := summary("survey the tests"), summary("survey the docs")
	for _, want := range []string{"2 tool calls", "12k/31k"} {
		if !strings.Contains(first, want) {
			t.Errorf("the first child's live tail is missing %q:\n%s", want, first)
		}
	}
	for _, want := range []string{"1 tool call", "4k/31k"} {
		if !strings.Contains(second, want) {
			t.Errorf("the second child's live tail is missing %q:\n%s", want, second)
		}
	}
	if strings.Contains(second, "2 tool calls") || strings.Contains(second, "12k/31k") ||
		strings.Contains(first, "4k/31k") {
		t.Errorf("a child's block reported its sibling's work:\n%s\n%s", first, second)
	}
	// The dropped cell, from the one angle a golden cannot state: neither row names what its child
	// is touching, however much of it the child has read (subAgentGist).
	for _, gone := range []string{"reading", "alpha", "beta"} {
		if strings.Contains(first, gone) || strings.Contains(second, gone) {
			t.Errorf("a running child's line still names its open call (%q):\n%s\n%s", gone, first, second)
		}
	}

	// The cap is the existing one: a collapsed run is its header and no more than three content
	// rows, whichever tempo it is in.
	if got := len(lines); got > 4+1+4 { // two blocks and the one blank line between them
		t.Errorf("two collapsed run blocks paint %d lines, want them inside the collapsed cap:\n%s",
			got, strings.Join(lines, "\n"))
	}
}

// TestSerialRunRendersAsItDidBeforeTheRunIdentity is the floor the grouping may not move: one
// delegate at a time renders byte-for-byte the same whether its events carry a spawning call id or
// not. The stamped stream is what this build produces; the bare one is every record written before
// it — and both must paint the run that has always been painted.
func TestSerialRunRendersAsItDidBeforeTheRunIdentity(t *testing.T) {
	t.Parallel()

	build := func(spawn string) *transcript {
		tr := &transcript{}
		tr.addUser("delegate it", nil)
		subAgentCall(tr, "s1", "survey the tests", 0)
		tr.apply(domain.TokenEvent{EventBase: domain.EventBase{Depth: 1, CallID: spawn}, Text: "looking"})
		tr.apply(domain.ToolCallEvent{
			EventBase: domain.EventBase{Depth: 1, CallID: spawn},
			Call:      domain.ToolCall{ID: "a1", Tool: "read_file", Arguments: []byte(`{"path":"alpha.go"}`)},
		})
		tr.apply(domain.ToolResultEvent{
			EventBase: domain.EventBase{Depth: 1, CallID: spawn},
			Result:    domain.ToolResult{CallID: "a1", Content: "1 - 10"},
		})
		tr.apply(domain.MessageEvent{EventBase: domain.EventBase{Depth: 1, CallID: spawn}, Text: "the tests pass"})
		subAgentReport(tr, "s1", "all green", 0)
		return tr
	}

	stamped, bare := build("s1"), build("")

	if got, want := plainRender(stamped), plainRender(bare); got != want {
		t.Errorf("the stamped run renders differently:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestPerChildRunOpensAlone pins what opening one child of a fan-out shows: its OWN work and no
// sibling's. The reach is the run view (ADR 0063) rather than a block state — a run has one row in
// the conversation and one view behind it — and the paint rooted at the second child is the
// second child's alone, exactly as its block used to be.
func TestPerChildRunOpensAlone(t *testing.T) {
	t.Parallel()

	tr := &transcript{}
	subAgentCall(tr, "s1", "survey the tests", 0)
	subAgentCall(tr, "s2", "survey the docs", 0)
	childCall(tr, "s1", "a1", "alpha.go")
	childCall(tr, "s2", "b1", "beta.md")
	subAgentReport(tr, "s1", "all green", 0)
	subAgentReport(tr, "s2", "docs are stale", 0)

	if got := plainRender(tr); strings.Contains(got, "alpha.go") || strings.Contains(got, "beta.md") {
		t.Fatalf("a collapsed run leaked its span:\n%s", got)
	}

	second := headIndex(t, tr, "s2")
	if tr.toggleExpanded(second) {
		t.Fatal("the second child's block toggled; a run opens as a view, never as a rail")
	}
	tr.setRoot(runRef{depth: 1, spawn: "s2"})
	got := plainRender(tr)
	if !strings.Contains(got, "beta.md") {
		t.Errorf("the opened child's view did not show its own work:\n%s", got)
	}
	if strings.Contains(got, "alpha.go") {
		t.Errorf("opening one child's view showed its sibling's work:\n%s", got)
	}
}

// TestConcurrentRunsSurviveTheSessionRecord is the resume half: the run identity is on the wire
// (transcriptbridge.go), so a replayed record re-groups into the same per-child blocks rather than
// collapsing into one braid behind the last call.
func TestConcurrentRunsSurviveTheSessionRecord(t *testing.T) {
	t.Parallel()

	tr := &transcript{}
	subAgentCall(tr, "s1", "survey the tests", 0)
	subAgentCall(tr, "s2", "survey the docs", 0)
	childCall(tr, "s1", "a1", "alpha.go")
	childCall(tr, "s2", "b1", "beta.md")
	subAgentReport(tr, "s1", "all green", 0)
	subAgentReport(tr, "s2", "docs are stale", 0)

	data, err := encodeTranscript(tr)
	if err != nil {
		t.Fatalf("encodeTranscript: %v", err)
	}
	entries, err := decodeTranscript(data)
	if err != nil {
		t.Fatalf("decodeTranscript: %v", err)
	}
	replayed := &transcript{}
	replayed.replay(entries)

	if got, want := plainRender(replayed), plainRender(tr); got != want {
		t.Errorf("the resumed scrollback renders differently:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
	for _, spawn := range []string{"s1", "s2"} {
		head := headIndex(t, replayed, spawn)
		if got := subAgentSpan(replayed.entries, head); got != 1 {
			t.Errorf("run %s spans %d entries after the resume, want its own 1", spawn, got)
		}
	}
}

// TestConcurrentSiblingStreamsCommitWhole is the streaming half of the grouping. Two children
// stream at once through ONE live buffer, so their token batches alternate through it; committing
// at every alternation would shred both answers into a column of one-batch blocks. Each run's
// displaced text is parked and comes back whole, in its own block, when that run commits.
func TestConcurrentSiblingStreamsCommitWhole(t *testing.T) {
	t.Parallel()

	tr := &transcript{}
	subAgentCall(tr, "s1", "survey the tests", 0)
	subAgentCall(tr, "s2", "survey the docs", 0)

	token := func(spawn, text string) {
		tr.apply(domain.TokenEvent{EventBase: domain.EventBase{Depth: 1, CallID: spawn}, Text: text})
	}
	token("s1", "the tests ")
	token("s2", "the docs ")
	token("s1", "all pass")
	token("s2", "are stale")

	if got := len(tr.entries); got != 2 {
		t.Errorf("the alternating streams committed %d entries mid-flight, want only the two heads:\n%s",
			got, plainRender(tr))
	}

	// Each child's MessageEvent is blank, so each falls back to what it actually streamed — which is
	// the assertion: the fallback must find that run's whole text and no part of its sibling's.
	tr.apply(domain.MessageEvent{EventBase: domain.EventBase{Depth: 1, CallID: "s1"}})
	tr.apply(domain.MessageEvent{EventBase: domain.EventBase{Depth: 1, CallID: "s2"}})

	for _, tc := range []struct{ spawn, want string }{
		{spawn: "s1", want: "the tests all pass"},
		{spawn: "s2", want: "the docs are stale"},
	} {
		head := headIndex(t, tr, tc.spawn)
		span := tr.entries[head+1 : head+1+subAgentSpan(tr.entries, head)]
		if len(span) != 1 {
			t.Fatalf("run %s committed %d entries, want its one answer:\n%s", tc.spawn, len(span), plainRender(tr))
		}
		if got := span[0].text; got != tc.want {
			t.Errorf("run %s committed %q, want %q", tc.spawn, got, tc.want)
		}
	}
}

// TestLivePreviewPaintsInsideTheRunThatFilledIt pins where the in-progress buffer lands once the
// list is grouped: at the end of ITS OWN run, not at the end of the scrollback. In the conversation
// every run is one row and the words are elided with the rest of the span; in the talking child's
// VIEW (ADR 0063) they stand after that child's own work and nothing of a sibling stands with them.
// Appending the preview to the list would have painted them after a sibling's entries instead,
// reading as a third voice.
func TestLivePreviewPaintsInsideTheRunThatFilledIt(t *testing.T) {
	t.Parallel()

	tr := &transcript{}
	subAgentCall(tr, "s1", "survey the tests", 0)
	subAgentCall(tr, "s2", "survey the docs", 0)
	childCall(tr, "s1", "a1", "alpha.go")
	// The sibling's work lands AFTER the talking child's, which is what makes the two ends of the
	// list different places: appended, the preview would follow this row instead.
	childCall(tr, "s2", "b1", "beta.md")

	tr.apply(domain.TokenEvent{EventBase: domain.EventBase{Depth: 1, CallID: "s1"}, Text: "the tests all pass"})

	if got := plainRender(tr); strings.Contains(got, "the tests all pass") {
		t.Fatalf("a collapsed run leaked its delegate's live words:\n%s", got)
	}

	tr.setRoot(runRef{depth: 1, spawn: "s1"})
	lines := strings.Split(plainRender(tr), "\n")
	preview, own := -1, -1
	for i, ln := range lines {
		switch {
		case strings.Contains(ln, "the tests all pass"):
			preview = i
		case strings.Contains(ln, "alpha.go"):
			own = i
		}
	}
	if preview < 0 || own < 0 {
		t.Fatalf("preview at %d, the run's own work at %d:\n%s", preview, own, strings.Join(lines, "\n"))
	}
	if preview < own {
		t.Errorf("the live preview painted before the run's own work:\n%s", strings.Join(lines, "\n"))
	}
	if strings.Contains(strings.Join(lines, "\n"), "beta.md") {
		t.Errorf("the talking child's view holds a sibling's work:\n%s", strings.Join(lines, "\n"))
	}
}

// TestAbandonedChildStreamCommitsWhenItsRunEnds is the parked text's other exit. A delegate that
// was displaced from the buffer by a sibling and then faulted before its MessageEvent has text
// nobody will ever commit on its behalf — so the run's own report commits it, inside the run,
// rather than leaving it to vanish with the session.
func TestAbandonedChildStreamCommitsWhenItsRunEnds(t *testing.T) {
	t.Parallel()

	tr := &transcript{}
	subAgentCall(tr, "s1", "survey the tests", 0)
	subAgentCall(tr, "s2", "survey the docs", 0)

	tr.apply(domain.TokenEvent{EventBase: domain.EventBase{Depth: 1, CallID: "s1"}, Text: "halfway through"})
	tr.apply(domain.TokenEvent{EventBase: domain.EventBase{Depth: 1, CallID: "s2"}, Text: "still going"})
	subAgentReport(tr, "s1", "cancelled", 0)

	head := headIndex(t, tr, "s1")
	span := tr.entries[head+1 : head+1+subAgentSpan(tr.entries, head)]
	if len(span) != 1 || span[0].text != "halfway through" {
		t.Fatalf("the abandoned delegate's words were lost when its run ended:\n%s", plainRender(tr))
	}
	if span[0].spawnCallID != "s1" {
		t.Errorf("the residue committed under run %q, want s1", span[0].spawnCallID)
	}
}

// A host note landing between two announcements of one fan-out does not split the group. The
// delegations are grouped by ADJACENCY of blocks (subAgentGroup), so a note parked between them
// would be a permanent divider: two "✦ Sub-Agent (1)" headers where the reader was promised one.
// The sibling announced after the note therefore lands in FRONT of it and the note slides to the
// tail, where it stands after the whole group.
func TestNoteBetweenSiblingDelegationsKeepsOneGroup(t *testing.T) {
	t.Parallel()

	tr := &transcript{}
	subAgentCall(tr, "s1", "survey the tests", 0)
	openChildCall(tr, "s1", "a1", "alpha.go")
	tr.addNote("cancelled")
	subAgentCall(tr, "s2", "survey the docs", 0)
	openChildCall(tr, "s2", "b1", "beta.md")

	group := subAgentGroup(tr.entries, headIndex(t, tr, "s1"))
	if len(group) != 2 {
		t.Fatalf("the fan-out reads as %d grouped delegations, want 2 — the note split it:\n%s",
			len(group), plainRender(tr))
	}
	if group[1].at != headIndex(t, tr, "s2") {
		t.Errorf("the second member is entry %d, want s2's head at %d",
			group[1].at, headIndex(t, tr, "s2"))
	}
	last := tr.entries[len(tr.entries)-1]
	if last.kind != entryNote {
		t.Errorf("the last entry is %v, want the note after the whole group:\n%s",
			last.kind, plainRender(tr))
	}
}
