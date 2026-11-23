package tui

import (
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/scheme"
)

// turnBoundary is the boundary of a Turn with more Turns to come, as the engine emits it for the
// run base names (domain.TurnEvent, StatusTurnComplete).
func turnBoundary(base domain.EventBase) domain.TurnEvent {
	return domain.TurnEvent{EventBase: base, Status: domain.StatusTurnComplete}
}

// callEvent and resultEvent are one tool call and its result as the run base names emits them.
func callEvent(base domain.EventBase, id, tool, args string) domain.ToolCallEvent {
	return domain.ToolCallEvent{EventBase: base, Call: domain.ToolCall{ID: id, Tool: tool, Arguments: []byte(args)}}
}

func resultEvent(base domain.EventBase, id, content string) domain.ToolResultEvent {
	return domain.ToolResultEvent{EventBase: base, Result: domain.ToolResult{CallID: id, Content: content}}
}

// replyText is one chunk of reply or narration text as the run base names streams it.
func replyText(base domain.EventBase, text string) domain.TokenEvent {
	return domain.TokenEvent{EventBase: base, Text: text}
}

// boundaryUmbrella is a two-call top-level umbrella at entries[0], both calls returned, under
// auto-collapse with the Turn running — the last entry of a running Turn, standing open.
func boundaryUmbrella(t *testing.T) *transcript {
	t.Helper()
	tr := &transcript{}
	tr.setToolsAutoCollapse(true)
	tr.setTurnLive(true)
	readCall(tr, "c1", "a.go", 1, 5, 0)
	runCall(tr, "c2", "go test", "ok", 0)
	if len(toolSuperGroup(tr.entries, 0)) == 0 || tr.blockFolded(0) {
		t.Fatal("setup: entries[0] is no open umbrella")
	}
	return tr
}

// textMarked folds non-blank reply text for the top-level run into tr and then a stream reset
// that discards it — the one path on which a later call joins the block the text settled — and
// fails the test unless the text folded the umbrella or group at entries[0] and the reset left it
// folded.
func textMarked(t *testing.T, tr *transcript) {
	t.Helper()
	top := domain.EventBase{}
	tr.apply(replyText(top, "Next, "))
	if !tr.blockFolded(0) {
		t.Fatalf("setup: the reply text did not fold the block:\n%s", renderPlain(tr, 80))
	}
	tr.apply(domain.StreamResetEvent{EventBase: top})
	if !tr.blockFolded(0) {
		t.Fatalf("setup: the stream reset reopened the block:\n%s", renderPlain(tr, 80))
	}
}

// TestAutoCollapseKeepsABurstOpenAcrossTurns pins the burst: two Turns that each issue one call,
// separated by nothing but a Turn boundary and the next Turn's reasoning, build ONE umbrella that
// stands open showing both calls — the boundary between them settles nothing.
func TestAutoCollapseKeepsABurstOpenAcrossTurns(t *testing.T) {
	t.Parallel()

	top := domain.EventBase{}
	m := newTestModel(t)
	m.transcript.setToolsAutoCollapse(true)
	m.markRunning()
	head := len(m.transcript.entries)

	for _, e := range []domain.Event{
		callEvent(top, "c1", "read_file", `{"path":"a.go"}`), resultEvent(top, "c1", "a"),
		turnBoundary(top),
		domain.ReasoningEvent{EventBase: top, Text: "now the pattern"},
		callEvent(top, "c2", "grep", `{"pattern":"x"}`), resultEvent(top, "c2", "x"),
		turnBoundary(top),
	} {
		m = m.foldEvent(e)
	}

	if got := toolSuperGroup(m.transcript.entries, head).calls(); got != 2 {
		t.Fatalf("the two Turns' calls form an umbrella of %d calls; want one umbrella of 2", got)
	}
	if got := m.transcript.blockFoldBy(head); got == foldBySettled || m.transcript.blockFolded(head) {
		t.Errorf("blockFoldBy = %d, folded %v across the burst's Turn boundaries; want it open:\n%s",
			got, m.transcript.blockFolded(head), renderPlain(&m.transcript, 80))
	}
}

// TestAutoCollapseFoldsOnTheReplyText pins the follower settle: under auto-collapse a finished,
// untouched block — a Sub-agents group, a Tools umbrella, a group of one — stays open through the
// next Turn's start and its reasoning, and folds once its run's reply text first turns non-blank,
// not once that reply commits an entry; a stream reset after that leaves it folded. Each shape is
// driven through the Model's own Event fold (foldEvent) in the order a real Step emits it.
func TestAutoCollapseFoldsOnTheReplyText(t *testing.T) {
	t.Parallel()

	top := domain.EventBase{}
	shapes := map[string]func() []domain.Event{
		// A fan-out of two delegations: both calls announced, each child runs and finishes, then
		// the group's result burst.
		"Sub-agents group": func() []domain.Event {
			evs := []domain.Event{
				domain.SubAgentGroupEvent{EventBase: top, Size: 2, Width: 2},
				callEvent(top, "s1", "sub_agent", `{"task":"look","name":"s1"}`),
				callEvent(top, "s2", "sub_agent", `{"task":"look","name":"s2"}`),
			}
			for _, id := range []string{"s1", "s2"} {
				child := domain.EventBase{Depth: 1, CallID: id}
				evs = append(evs,
					domain.SubAgentPhaseEvent{EventBase: child, Phase: domain.SubAgentStarted},
					domain.MessageEvent{EventBase: child, Text: "all clear"},
					domain.TurnEvent{EventBase: child, Status: domain.StatusExchangeComplete},
					domain.SubAgentPhaseEvent{EventBase: child, Phase: domain.SubAgentFinished,
						Result: domain.ToolResult{CallID: id, Content: "all clear"}},
				)
			}
			return append(evs, resultEvent(top, "s1", "all clear"), resultEvent(top, "s2", "all clear"))
		},
		"Tools umbrella": func() []domain.Event {
			return []domain.Event{
				callEvent(top, "c1", "read_file", `{"path":"a.go"}`), resultEvent(top, "c1", "a"),
				callEvent(top, "c2", "grep", `{"pattern":"x"}`), resultEvent(top, "c2", "x"),
			}
		},
		"group of one": func() []domain.Event {
			return []domain.Event{callEvent(top, "c1", "read_file", `{"path":"a.go"}`), resultEvent(top, "c1", "a")}
		},
	}

	for name, build := range shapes {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			m := newTestModel(t)
			m.transcript.setToolsAutoCollapse(true)
			m.markRunning()
			head := len(m.transcript.entries)
			for _, e := range build() {
				m = m.foldEvent(e)
			}
			if block := toolBlockAt(m.transcript.entries, head); !block.foldable() || block.head != head {
				t.Fatalf("setup: entries[%d] heads no foldable block", head)
			}
			if m.transcript.blockFolded(head) {
				t.Fatal("folded while its own Turn is still dispatching (nothing past its last result); want open")
			}

			m = m.foldEvent(turnBoundary(top))
			m = m.foldEvent(domain.ReasoningEvent{EventBase: top, Text: "both are done"})
			if got := m.transcript.blockFoldBy(head); got == foldBySettled {
				t.Errorf("next Turn started (boundary + reasoning): blockFoldBy = foldBySettled; want open")
			}
			m = m.foldEvent(replyText(top, "\n \n"))
			if m.transcript.blockFolded(head) {
				t.Errorf("blank reply text folded the block; want it open:\n%s", renderPlain(&m.transcript, 80))
			}
			m = m.foldEvent(replyText(top, "Both scouts "))
			if !m.transcript.blockFolded(head) {
				t.Errorf("next Turn streaming its reply: blockFolded = false; want true:\n%s",
					renderPlain(&m.transcript, 80))
			}
			m = m.foldEvent(domain.StreamResetEvent{EventBase: top})
			if !m.transcript.blockFolded(head) {
				t.Errorf("a stream reset reopened the folded block; want it to stay folded:\n%s",
					renderPlain(&m.transcript, 80))
			}
		})
	}

	t.Run("blank text then a call: the call joins the open block", func(t *testing.T) {
		t.Parallel()

		m := newTestModel(t)
		m.transcript.setToolsAutoCollapse(true)
		m.markRunning()
		head := len(m.transcript.entries)
		for _, e := range []domain.Event{
			callEvent(top, "c1", "read_file", `{"path":"a.go"}`), resultEvent(top, "c1", "a"),
			turnBoundary(top),
			replyText(top, "\n\n"),
			callEvent(top, "c2", "grep", `{"pattern":"x"}`),
		} {
			m = m.foldEvent(e)
		}

		if got := toolSuperGroup(m.transcript.entries, head).calls(); got != 2 {
			t.Fatalf("the call after blank text formed an umbrella of %d calls; want it to join, 2", got)
		}
		if m.transcript.blockFolded(head) {
			t.Errorf("the umbrella folded on blank text; want it open:\n%s", renderPlain(&m.transcript, 80))
		}
	})
}

// TestAutoCollapseSettlesOnlyTheStreamingRunsBlock pins whose block a run's text settles: its own
// last block and nothing else — never its parent's, never a sibling's — at every depth and in the
// run's own view.
func TestAutoCollapseSettlesOnlyTheStreamingRunsBlock(t *testing.T) {
	t.Parallel()

	t.Run("a delegated child's text folds the child's block, not the parent's", func(t *testing.T) {
		t.Parallel()

		tr := &transcript{}
		tr.setToolsAutoCollapse(true)
		tr.setTurnLive(true)
		tr.apply(domain.ToolCallEvent{
			Call:       domain.ToolCall{ID: "sa1", Tool: "sub_agent", Arguments: []byte(`{"task":"look","name":"scout"}`)},
			SpawnRunID: "r1",
		})
		child := domain.EventBase{Depth: 1, CallID: "sa1", RunID: "r1"}
		tr.apply(domain.SubAgentPhaseEvent{EventBase: child, Phase: domain.SubAgentStarted})
		tr.apply(callEvent(child, "c1", "read_file", `{"path":"a.go"}`))
		tr.apply(resultEvent(child, "c1", "a"))
		tr.apply(turnBoundary(child))
		tr.apply(callEvent(child, "c2", "grep", `{"pattern":"x"}`))
		tr.apply(resultEvent(child, "c2", "x"))
		tr.apply(turnBoundary(child))
		head := 1
		if len(toolSuperGroup(tr.entries, head)) == 0 || tr.blockFolded(head) {
			t.Fatalf("setup: entries[%d] is no open umbrella in the child's run", head)
		}

		tr.apply(replyText(child, "Found it."))

		if got := tr.blockFoldBy(head); got != foldBySettled {
			t.Errorf("blockFoldBy = %d once the child's reply text arrived; want foldBySettled", got)
		}
		if got := tr.blockFoldBy(0); got == foldBySettled {
			t.Error("the child's text settled its parent's delegation block; want it open while the child runs")
		}
		tr.setRoot(tr.entries[0].spawned())
		if got := tr.blockFoldBy(head); got != foldBySettled {
			t.Errorf("blockFoldBy = %d in the child's run view; want foldBySettled", got)
		}
	})

	t.Run("one run's text settles no sibling's block", func(t *testing.T) {
		t.Parallel()

		tr := &transcript{}
		tr.setToolsAutoCollapse(true)
		tr.setTurnLive(true)
		for _, id := range []string{"s1", "s2"} {
			delegationCall(tr, "", id, id, "look around", 0)
			subAgentStarted(tr, id, 1)
		}
		delegatedRead(tr, "s1", "r1", "a.go", 1)
		delegatedRead(tr, "s1", "r2", "b.go", 1)
		head, ok := -1, false
		for i := range tr.entries {
			if tr.entries[i].callID == "r1" {
				head, ok = i, true
			}
		}
		if !ok || len(toolSuperGroup(tr.entries, head)) == 0 {
			t.Fatal("setup: s1's two reads form no umbrella")
		}

		tr.apply(replyText(domain.EventBase{Depth: 1, CallID: "s2"}, "All clear."))

		if got := tr.blockFoldBy(head); got == foldBySettled {
			t.Error("s2's text settled s1's umbrella; want it open while s1 runs")
		}
		tr.apply(replyText(domain.EventBase{Depth: 1, CallID: "s1"}, "Both read."))
		if got := tr.blockFoldBy(head); got != foldBySettled {
			t.Errorf("blockFoldBy = %d at s1's own text; want foldBySettled", got)
		}
	})
}

// TestAutoCollapseKeepsATextFoldedBlockFolded pins the mark's stickiness: a block the reply text
// folded stays folded when a stream reset discards that text and the re-stream's call joins it
// instead — its header growing, its new member hidden — unless that member waits on the human.
func TestAutoCollapseKeepsATextFoldedBlockFolded(t *testing.T) {
	t.Parallel()

	top := domain.EventBase{}

	t.Run("an umbrella stays folded as a call joins it after a reset", func(t *testing.T) {
		t.Parallel()

		tr := boundaryUmbrella(t)
		textMarked(t, tr)
		tr.apply(callEvent(top, "c3", "read_file", `{"path":"b.go"}`))
		if got := toolSuperGroup(tr.entries, 0).calls(); got != 3 || tr.entries[2].done {
			t.Fatalf("setup: the in-flight Read did not join the umbrella (%d calls)", got)
		}
		if !tr.blockFolded(0) {
			t.Fatalf("the text-folded umbrella sprang open for the re-stream's call:\n%s", renderPlain(tr, 80))
		}
		if paint := renderPlain(tr, 80); !strings.Contains(paint, "Reading") {
			t.Errorf("the folded umbrella with a call in flight paints no live verb:\n%s", paint)
		}
	})

	t.Run("a Sub-agents group stays folded as a member joins it after a reset", func(t *testing.T) {
		t.Parallel()

		tr := subAgentGroupOf(t)
		textMarked(t, tr)
		delegationCall(tr, "", "s3", "s3", "look again", 0)
		if group, _, ok := subAgentGroupAt(tr.entries, 0); !ok || len(group) != 3 {
			t.Fatal("setup: the re-stream's delegation did not join the group")
		}
		paint := renderPlain(tr, 80)
		if !tr.blockFolded(0) || !strings.Contains(paint, "✦ Sub-agents (3)") || strings.Contains(paint, "look again") {
			t.Errorf("the grown group did not stay folded to its header with its count grown:\n%s", paint)
		}
	})

	// A lone delegation and a lone skill fetch are groups of one, so the text folds them like any
	// group, and the group a re-stream's call grows them into stays folded, its header counting
	// the new member (adr-0035-amend-1120b (b)).
	for name, tc := range map[string]struct {
		lone   func(tr *transcript)
		next   func(tr *transcript)
		group  func(entries []entry) int
		header string
		hidden string
	}{
		"a lone delegation": {
			lone: func(tr *transcript) {
				delegationCall(tr, "", "s1", "s1", "look around", 0)
				subAgentStarted(tr, "s1", 1)
				subAgentReport(tr, "s1", "all clear", 0)
			},
			next: func(tr *transcript) { delegationCall(tr, "", "s2", "s2", "look again", 0) },
			group: func(entries []entry) int {
				group, _, _ := subAgentGroupAt(entries, 0)
				return len(group)
			},
			header: "✦ Sub-agents (2)",
			hidden: "look again",
		},
		"a lone skill fetch": {
			lone: func(tr *transcript) {
				skillFetch(tr, "k1", "format Go", "<skill: Coding Standards>\nUse gofmt.\n</skill>\n")
			},
			next: func(tr *transcript) {
				tr.apply(callEvent(top, "k2", "load_skill", `{"query":"cut a release"}`))
			},
			group: func(entries []entry) int {
				group, _, _ := ownGroupAt(entries, 0, loadSkillToolName)
				return len(group)
			},
			header: "✦ Skill (2)",
			hidden: "cut a release",
		},
	} {
		t.Run("the group of one "+name+" folds and stays folded as it grows", func(t *testing.T) {
			t.Parallel()

			tr := &transcript{}
			tr.setToolsAutoCollapse(true)
			tr.setTurnLive(true)
			tc.lone(tr)
			textMarked(t, tr)
			tc.next(tr)
			if got := tc.group(tr.entries); got != 2 {
				t.Fatalf("setup: the re-stream's call formed a group of %d; want 2", got)
			}
			paint := renderPlain(tr, 80)
			if !tr.blockFolded(0) || !strings.Contains(paint, tc.header) || strings.Contains(paint, tc.hidden) {
				t.Errorf("the grown group did not stay folded to %q:\n%s", tc.header, paint)
			}
		})
	}

	t.Run("a pending question joining the block opens it while it waits", func(t *testing.T) {
		t.Parallel()

		tr := boundaryUmbrella(t)
		textMarked(t, tr)
		tr.apply(callEvent(top, "q1", "ask_user", `{"question":"Which file?"}`))
		if toolSuperGroup(tr.entries, 0).calls() != 3 {
			t.Fatal("setup: the pending ask_user did not join the umbrella")
		}
		if tr.blockFolded(0) {
			t.Errorf("the umbrella stayed folded over a question waiting on the user:\n%s", renderPlain(tr, 80))
		}
		tr.apply(resultEvent(top, "q1", "a.go"))
		if !tr.blockFolded(0) {
			t.Errorf("the umbrella did not fold again once the question was answered:\n%s", renderPlain(tr, 80))
		}
	})

	t.Run("a call held on its approval pane opens the block while it waits", func(t *testing.T) {
		t.Parallel()

		tr := boundaryUmbrella(t)
		textMarked(t, tr)
		tr.apply(callEvent(top, "c3", "terminal", `{"command":"rm -rf build"}`))
		req := domain.ApprovalRequest{Tool: "terminal", Arguments: []byte(`{"command":"rm -rf build"}`)}
		if !tr.blockFolded(0) {
			t.Fatal("setup: the umbrella sprang open before the gate was raised")
		}
		tr.apply(domain.ApprovalEvent{Phase: domain.ApprovalRequested, Request: req})
		if tr.blockFolded(0) {
			t.Errorf("the umbrella stayed folded over a call held on its approval pane:\n%s", renderPlain(tr, 80))
		}
		tr.apply(domain.ApprovalEvent{Phase: domain.ApprovalDecided, Request: req, Decision: domain.ApprovalAllow})
		if !tr.blockFolded(0) {
			t.Errorf("the umbrella did not fold again once the approval was decided:\n%s", renderPlain(tr, 80))
		}
	})
}

// TestAutoCollapseTurnBoundaryAndTouch pins what does NOT settle a block: a Turn boundary of any
// status, and anything at all once the reader has touched the block.
func TestAutoCollapseTurnBoundaryAndTouch(t *testing.T) {
	t.Parallel()

	top := domain.EventBase{}

	t.Run("a group of one the reader opened stays open as a later Turn extends it", func(t *testing.T) {
		t.Parallel()

		tr := &transcript{}
		tr.setToolsAutoCollapse(true)
		tr.setTurnLive(true)
		readCall(tr, "c1", "a.go", 1, 5, 0)
		if !tr.openBlockAlone(0) {
			t.Fatal("setup: opening the group of one moved nothing")
		}
		tr.apply(turnBoundary(top))
		tr.apply(callEvent(top, "c2", "grep", `{"pattern":"x"}`))
		if len(toolSuperGroup(tr.entries, 0)) == 0 {
			t.Fatal("setup: the next Turn's call left entry 0 heading no group")
		}
		if tr.blockFolded(0) {
			t.Errorf("the group the opened group of one grew into folded:\n%s", renderPlain(tr, 80))
		}
		tr.apply(resultEvent(top, "c2", "x"))
		tr.apply(replyText(top, "Found it."))
		tr.apply(domain.MessageEvent{Text: "Found it."})
		// The touched group grows and stays as the reader left it: settled, it is still open.
		if got := tr.blockFoldBy(0); got == foldBySettled || tr.blockFolded(0) {
			t.Errorf("blockFoldBy(0) = %d, folded %v once the umbrella settled; want the touched group open",
				got, tr.blockFolded(0))
		}
	})

	t.Run("a touched block is not refolded by the reply text", func(t *testing.T) {
		t.Parallel()

		tr := boundaryUmbrella(t)
		if !tr.markBlockTouched(1) {
			t.Fatal("setup: touching a member did not mark the umbrella")
		}
		tr.apply(replyText(top, "Done."))
		if got := tr.blockFoldBy(0); got == foldBySettled || tr.blockFolded(0) {
			t.Errorf("blockFoldBy(0) = %d, folded %v on a touched umbrella under reply text; want it open",
				got, tr.blockFolded(0))
		}
	})

	t.Run("a Turn boundary settles nothing, whatever its status", func(t *testing.T) {
		t.Parallel()

		for _, status := range []domain.StepStatus{
			domain.StatusTurnComplete, domain.StatusExchangeComplete, domain.StatusCancelled,
		} {
			tr := boundaryUmbrella(t)
			tr.apply(domain.TurnEvent{Status: status})
			if got := tr.blockFoldBy(0); got == foldBySettled || tr.blockFolded(0) {
				t.Errorf("a %s boundary settled the running Turn's umbrella; want it open", status)
			}
		}
	})

	t.Run("the reply text repaints a cached paint", func(t *testing.T) {
		t.Parallel()

		th := newTheme(scheme.Default())
		tr := boundaryUmbrella(t)
		tr.paints = newPaintCache()
		open := tr.renderLines(th, 80)
		before := tr.generation

		tr.apply(replyText(top, "Done."))

		if tr.generation == before {
			t.Error("the reply text did not move the paint generation")
		}
		cached := tr.renderLines(th, 80)
		fresh := *tr
		fresh.paints = nil
		uncached := fresh.renderLines(th, 80)
		if strings.Join(cached, "\n") != strings.Join(uncached, "\n") || cached[0] == open[0] {
			t.Errorf("the cached paint did not follow the text's fold:\ncached:\n%s\nuncached:\n%s",
				strings.Join(cached, "\n"), strings.Join(uncached, "\n"))
		}
	})
}
