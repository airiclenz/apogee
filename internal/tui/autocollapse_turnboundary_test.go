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

// TestAutoCollapseFoldsWhenTheNextTurnStarts pins the Turn-boundary settle: under auto-collapse a
// finished, untouched block — a Sub-agents group, a Tools umbrella, a group of one — folds once the
// NEXT Turn of its own run starts (the boundary its Turn emits after every call it issued was
// answered), not once that next Turn's reply commits an entry. Each shape is driven through the
// Model's own Event fold (foldEvent) in the order a real Step emits it, and stays open while its
// own Turn is still dispatching.
func TestAutoCollapseFoldsWhenTheNextTurnStarts(t *testing.T) {
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
			if _, ok := foldableHead(m.transcript.entries, head); !ok {
				t.Fatalf("setup: entries[%d] heads no foldable block", head)
			}
			if m.transcript.blockFolded(head) {
				t.Fatal("folded while its own Turn is still dispatching (nothing past its last result); want open")
			}

			m = m.foldEvent(turnBoundary(top))
			m = m.foldEvent(domain.ReasoningEvent{EventBase: top, Text: "both are done"})
			if got := m.transcript.blockFoldBy(head); got != foldBySettled {
				t.Errorf("next Turn started (boundary + reasoning): blockFoldBy = %d; want foldBySettled (%d)",
					got, foldBySettled)
			}
			m = m.foldEvent(domain.TokenEvent{EventBase: top, Text: "Both scouts "})
			if !m.transcript.blockFolded(head) {
				t.Errorf("next Turn streaming its reply: blockFolded = false; want true:\n%s",
					renderPlain(&m.transcript, 80))
			}
		})
	}

	t.Run("a block inside a delegated run folds at that child's boundary", func(t *testing.T) {
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
		tr.apply(callEvent(child, "c2", "grep", `{"pattern":"x"}`))
		tr.apply(resultEvent(child, "c2", "x"))
		head := 1
		if len(toolSuperGroup(tr.entries, head)) == 0 || tr.blockFolded(head) {
			t.Fatalf("setup: entries[%d] is no open umbrella in the child's run", head)
		}

		tr.apply(turnBoundary(child))

		if got := tr.blockFoldBy(head); got != foldBySettled {
			t.Errorf("blockFoldBy = %d once the child's next Turn started; want foldBySettled", got)
		}
		tr.setRoot(tr.entries[0].spawned())
		if got := tr.blockFoldBy(head); got != foldBySettled {
			t.Errorf("blockFoldBy = %d in the child's run view; want foldBySettled", got)
		}
	})

	t.Run("one run's boundary settles no other run's block", func(t *testing.T) {
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

		tr.apply(turnBoundary(domain.EventBase{Depth: 1, CallID: "s2"}))

		if got := tr.blockFoldBy(head); got == foldBySettled {
			t.Error("s2's boundary settled s1's umbrella; want it open while s1 runs")
		}
		tr.apply(turnBoundary(domain.EventBase{Depth: 1, CallID: "s1"}))
		if got := tr.blockFoldBy(head); got != foldBySettled {
			t.Errorf("blockFoldBy = %d at s1's own boundary; want foldBySettled", got)
		}
	})

	t.Run("an umbrella the boundary folded stays folded while the next Turn extends it", func(t *testing.T) {
		t.Parallel()

		tr := boundaryUmbrella(t)
		tr.apply(turnBoundary(domain.EventBase{}))
		tr.apply(callEvent(domain.EventBase{}, "c3", "read_file", `{"path":"b.go"}`))
		if got := toolSuperGroup(tr.entries, 0).calls(); got != 3 || tr.entries[2].done {
			t.Fatalf("setup: the in-flight Read did not join the umbrella (%d calls)", got)
		}
		if !tr.blockFolded(0) {
			t.Fatalf("the boundary-folded umbrella sprang open for the next Turn's call:\n%s", renderPlain(tr, 80))
		}
		if paint := renderPlain(tr, 80); !strings.Contains(paint, "Reading") {
			t.Errorf("the folded umbrella with a call in flight paints no live verb:\n%s", paint)
		}
	})

	t.Run("a group the boundary folded stays folded as the next Turn adds a member", func(t *testing.T) {
		t.Parallel()

		tr := subAgentGroupOf(t)
		tr.apply(turnBoundary(domain.EventBase{}))
		if !tr.blockFolded(0) {
			t.Fatal("setup: the boundary did not fold the group")
		}
		delegationCall(tr, "", "s3", "s3", "look again", 0)
		if group, _, ok := subAgentGroupAt(tr.entries, 0); !ok || len(group) != 3 {
			t.Fatal("setup: the next Turn's delegation did not join the group")
		}
		paint := renderPlain(tr, 80)
		if !tr.blockFolded(0) || !strings.Contains(paint, "✦ Sub-agents (3)") || strings.Contains(paint, "look again") {
			t.Errorf("the grown group did not stay folded to its header with its count grown:\n%s", paint)
		}
	})

	// A lone delegation and a lone skill fetch are groups of one, so the boundary folds them like any
	// group, and the group the next Turn's call grows them into stays folded, its header counting
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
				tr.apply(callEvent(domain.EventBase{}, "k2", "load_skill", `{"query":"cut a release"}`))
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
			tr.apply(turnBoundary(domain.EventBase{}))
			if !tr.blockFolded(0) {
				t.Fatalf("the boundary did not fold the group of one:\n%s", renderPlain(tr, 80))
			}
			tc.next(tr)
			if got := tc.group(tr.entries); got != 2 {
				t.Fatalf("setup: the next Turn's call formed a group of %d; want 2", got)
			}
			paint := renderPlain(tr, 80)
			if !tr.blockFolded(0) || !strings.Contains(paint, tc.header) || strings.Contains(paint, tc.hidden) {
				t.Errorf("the grown group did not stay folded to %q:\n%s", tc.header, paint)
			}
		})
	}

	t.Run("a pending question in the extension opens the block while it waits", func(t *testing.T) {
		t.Parallel()

		tr := boundaryUmbrella(t)
		tr.apply(turnBoundary(domain.EventBase{}))
		tr.apply(callEvent(domain.EventBase{}, "q1", "ask_user", `{"question":"Which file?"}`))
		if toolSuperGroup(tr.entries, 0).calls() != 3 {
			t.Fatal("setup: the pending ask_user did not join the umbrella")
		}
		if tr.blockFolded(0) {
			t.Errorf("the umbrella stayed folded over a question waiting on the user:\n%s", renderPlain(tr, 80))
		}
		tr.apply(resultEvent(domain.EventBase{}, "q1", "a.go"))
		if !tr.blockFolded(0) {
			t.Errorf("the umbrella did not fold again once the question was answered:\n%s", renderPlain(tr, 80))
		}
	})

	t.Run("a call held on its approval pane opens the block while it waits", func(t *testing.T) {
		t.Parallel()

		tr := boundaryUmbrella(t)
		tr.apply(turnBoundary(domain.EventBase{}))
		tr.apply(callEvent(domain.EventBase{}, "c3", "terminal", `{"command":"rm -rf build"}`))
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

	t.Run("a group of one the reader opened stays open as the next Turn extends it", func(t *testing.T) {
		t.Parallel()

		tr := &transcript{}
		tr.setToolsAutoCollapse(true)
		tr.setTurnLive(true)
		readCall(tr, "c1", "a.go", 1, 5, 0)
		if !tr.openBlockAlone(0) || !tr.entries[0].foldTouched {
			t.Fatal("setup: opening the group of one did not touch it")
		}
		tr.apply(turnBoundary(domain.EventBase{}))
		if tr.blockFolded(0) {
			t.Fatal("the boundary folded a group of one the reader opened")
		}
		tr.apply(callEvent(domain.EventBase{}, "c2", "grep", `{"pattern":"x"}`))
		if len(toolSuperGroup(tr.entries, 0)) == 0 {
			t.Fatal("setup: the next Turn's call left entry 0 heading no group")
		}
		if tr.blockFolded(0) {
			t.Errorf("the group the opened group of one grew into folded on the old boundary:\n%s", renderPlain(tr, 80))
		}
		tr.apply(resultEvent(domain.EventBase{}, "c2", "x"))
		tr.apply(domain.MessageEvent{Text: "Found it."})
		// The touched group grows and stays as the reader left it: settled, it is still open.
		if got := tr.blockFoldBy(0); got == foldBySettled || tr.blockFolded(0) {
			t.Errorf("blockFoldBy(0) = %d, folded %v once the umbrella settled; want the touched group open",
				got, tr.blockFolded(0))
		}
	})

	t.Run("a touched block is not refolded by a boundary", func(t *testing.T) {
		t.Parallel()

		tr := boundaryUmbrella(t)
		if !tr.markBlockTouched(1) {
			t.Fatal("setup: touching a member did not mark the umbrella")
		}
		tr.apply(turnBoundary(domain.EventBase{}))
		if got := tr.blockFoldBy(0); got == foldBySettled || tr.blockFolded(0) {
			t.Errorf("blockFoldBy(0) = %d, folded %v on a touched umbrella at a boundary; want it open",
				got, tr.blockFolded(0))
		}
	})

	t.Run("only a Turn with more to come settles", func(t *testing.T) {
		t.Parallel()

		for _, status := range []domain.StepStatus{domain.StatusExchangeComplete, domain.StatusCancelled} {
			tr := boundaryUmbrella(t)
			tr.apply(domain.TurnEvent{Status: status})
			if tr.entries[1].turnClosed || tr.blockFolded(0) {
				t.Errorf("a %s boundary marked the run's last entry; want only turn-complete to", status)
			}
		}
	})

	t.Run("the boundary repaints a cached paint", func(t *testing.T) {
		t.Parallel()

		th := newTheme(scheme.Default())
		tr := boundaryUmbrella(t)
		tr.paints = newPaintCache()
		open := strings.Join(tr.renderLines(th, 80), "\n")
		before := tr.generation

		tr.apply(turnBoundary(domain.EventBase{}))

		if tr.generation == before {
			t.Error("the boundary did not move the paint generation")
		}
		cached := strings.Join(tr.renderLines(th, 80), "\n")
		fresh := *tr
		fresh.paints = nil
		if uncached := strings.Join(fresh.renderLines(th, 80), "\n"); cached != uncached || cached == open {
			t.Errorf("the cached paint did not follow the boundary's fold:\ncached:\n%s\nuncached:\n%s", cached, uncached)
		}
	})
}
