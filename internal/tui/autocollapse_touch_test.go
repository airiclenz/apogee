package tui

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

// autoCollapseLargeModel is modelWithLargeUmbrellas under auto-collapse: two LARGE umbrellas, heads
// 1 and 6, behind prompts 0 and 5. Idle, both are settled and stand folded; with live set the
// Turn is running, so the second — the Turn's last entry — is unsettled and folds by its size
// under the shared preference, while the first, followed by the second prompt, stays settled.
func autoCollapseLargeModel(t *testing.T, opts Options, live bool) Model {
	t.Helper()
	m := modelWithLargeUmbrellas(t, opts)
	m.transcript.setToolsAutoCollapse(true)
	m.transcript.setTurnLive(live)
	m.refreshViewport()
	return m
}

// lastLineMarked is the last content line marked kind for entry — a member's body row when the
// member is open, its only row when it is not.
func lastLineMarked(t *testing.T, m Model, kind targetKind, entry int) int {
	t.Helper()
	for i := len(m.lineTargets) - 1; i >= 0; i-- {
		if m.lineTargets[i].kind == kind && m.lineTargets[i].entry == entry {
			return i
		}
	}
	t.Fatalf("no rendered row is marked %d for entry %d:\n%s", kind, entry, strings.Join(m.lines, "\n"))
	return -1
}

// cursorOnTarget walks the block cursor up from the prompt until it stands on a stop marked kind
// for entry.
func cursorOnTarget(t *testing.T, m Model, kind targetKind, entry int) Model {
	t.Helper()
	m = step(t, m, keyAltUp())
	for range len(m.lineTargets) {
		if target := m.lineTargets[m.cursor.line]; target.kind == kind && target.entry == entry {
			return m
		}
		m = step(t, m, keyUp())
	}
	t.Fatalf("setup: the block cursor never stood on a row marked %d for entry %d", kind, entry)
	return m
}

// TestUmbrellaHeadOfFindsTheHeadFromAnyMember pins the walk back to a foldable block's head: every
// call in an umbrella answers the umbrella's head, a single groupable call — a group of one — answers itself, and a
// prompt and a note answer none.
func TestUmbrellaHeadOfFindsTheHeadFromAnyMember(t *testing.T) {
	t.Parallel()

	tr := &transcript{}
	tr.addUser("look around", nil)        // 0
	readCall(tr, "r1", "a.go", 1, 5, 0)   // 1: the head
	readCall(tr, "r2", "b.go", 1, 9, 0)   // 2: second of the Read run
	runCall(tr, "t1", "go test", "ok", 0) // 3: the Terminal run's head
	tr.addNote("saved")                   // 4
	runCall(tr, "t2", "go vet", "ok", 0)  // 5: a group of one
	for i, want := range []int{-1, 1, 1, 1, -1, 5} {
		block := toolBlockAt(tr.entries, i)
		ok := block.foldable()
		if ok != (want >= 0) || (ok && block.head != want) {
			t.Errorf("toolBlockAt(%d) = head %d, foldable %v; want %d, %v", i, block.head, ok, want, want >= 0)
		}
	}
}

// TestAutoCollapseTouchWritersMoveTheGeneration pins the two marks' writers to the paint's
// generation: each moves it when it moves the mark and holds it when there is nothing to move.
func TestAutoCollapseTouchWritersMoveTheGeneration(t *testing.T) {
	t.Parallel()

	tr := autoCollapseUmbrella(t)
	tr.addNote("saved")                 // 2: no umbrella here
	readCall(tr, "c3", "c.go", 1, 4, 0) // 3: a group of one
	tr.addNote("read")                  // 4

	for _, tc := range []struct {
		name  string
		act   func() bool
		moves bool
	}{
		{"markBlockTouched from a member", func() bool { return tr.markBlockTouched(1) }, true},
		{"markBlockTouched again", func() bool { return tr.markBlockTouched(0) }, false},
		{"markBlockTouched outside an umbrella", func() bool { return tr.markBlockTouched(2) }, false},
		{"openBlockAlone", func() bool { return tr.openBlockAlone(0) }, true},
		{"openBlockAlone again", func() bool { return tr.openBlockAlone(0) }, false},
		{"openBlockAlone on a member", func() bool { return tr.openBlockAlone(2) }, false},
		{"openBlockAlone on a group of one", func() bool { return tr.openBlockAlone(3) }, true},
		{"markBlockTouched on that group of one again", func() bool { return tr.markBlockTouched(3) }, false},
	} {
		before := tr.generation
		if got := tc.act(); got != tc.moves {
			t.Errorf("%s reported %v; want %v", tc.name, got, tc.moves)
		}
		if moved := tr.generation != before; moved != tc.moves {
			t.Errorf("%s moved the generation: %v; want %v", tc.name, moved, tc.moves)
		}
	}
	if head := tr.entries[0]; !head.foldTouched || !head.foldOwn || head.folded {
		t.Errorf("head after openBlockAlone: touched %v, ownFold %v, folded %v; want true, true, false",
			head.foldTouched, head.foldOwn, head.folded)
	}
	// Opening a group of one gives it a fold of its own, as any umbrella's.
	if one := tr.entries[3]; !one.foldTouched || !one.foldOwn || one.folded {
		t.Errorf("group of one after openBlockAlone: foldTouched %v, ownFold %v, folded %v; want true, true, false",
			one.foldTouched, one.foldOwn, one.folded)
	}
}

// TestAutoCollapseTouchMarksEveryTargetKind pins the touch on every level of an umbrella, by mouse
// and by ⏎: its header, a type row, a member's row and a member's body. The touched umbrella is
// then exempt — it does not fold when the Turn ends — and the other one is never marked.
func TestAutoCollapseTouchMarksEveryTargetKind(t *testing.T) {
	t.Parallel()
	const settled, live = 1, 6

	for _, tc := range []struct {
		name   string
		kind   targetKind
		entry  int
		expand bool // open the Terminal run's second call first, so its body is on screen
	}{
		{"umbrella header", targetUmbrella, live, false},
		{"type row", targetType, live, false},
		{"member row", targetHeader, live + 2, false},
		{"member body", targetHeader, live + 3, true},
	} {
		for _, way := range []string{"mouse", "enter"} {
			t.Run(tc.name+" by "+way, func(t *testing.T) {
				t.Parallel()

				m := autoCollapseLargeModel(t, testOpts, true)
				m.setToolsOpen(true)
				m.transcript.setTypeExpanded(live+2, true) // the Terminal type row lists its calls
				if tc.expand {
					m.transcript.setExpanded(live+3, true)
				}
				m.refreshViewport()

				if way == "mouse" {
					m = clickCell(t, m, 4, screenRow(t, m, lastLineMarked(t, m, tc.kind, tc.entry)))
				} else {
					m = cursorOnTarget(t, m, tc.kind, tc.entry)
					m = step(t, m, keyEnter())
				}

				if !m.transcript.entries[live].foldTouched {
					t.Errorf("the %s gesture left the umbrella untouched", tc.name)
				}
				if m.transcript.entries[settled].foldTouched {
					t.Error("the other umbrella was marked touched by a gesture on this one")
				}
				m.transcript.setTurnLive(false)
				if got := m.transcript.blockFoldBy(live); got == foldBySettled {
					t.Error("the touched umbrella auto-collapsed once the Turn ended; want it exempt")
				}
			})
		}
	}
}

// TestAutoCollapseTouchOpensOneUmbrellaAlone pins the ratified large-umbrella click: opening an
// auto-collapsed LARGE umbrella opens that one only and writes nothing, the other settled one
// stays folded, and a second click folds it alone, still writing nothing.
func TestAutoCollapseTouchOpensOneUmbrellaAlone(t *testing.T) {
	t.Parallel()
	const first, second = 1, 6

	log := &settingsWriteLog{}
	opts := testOpts
	opts.Settings = fakeSettingsHost{write: log.write}
	m := autoCollapseLargeModel(t, opts, false)
	if !m.transcript.blockFolded(first) || !m.transcript.blockFolded(second) {
		t.Fatal("setup: the two settled umbrellas are not both folded")
	}

	m = clickCell(t, m, 4, screenRow(t, m, umbrellaHeaderLineOf(t, m, first)))

	if m.transcript.blockFolded(first) {
		t.Error("the clicked auto-collapsed umbrella is still folded")
	}
	typeRowLine(t, m, first) // it lists its type rows
	if !m.transcript.blockFolded(second) {
		t.Error("the other settled umbrella opened with it; want it folded")
	}
	if m.opts.UI.ToolsOpen || m.transcript.toolsOpen {
		t.Error("the open moved the shared ui.tools-open fold; want it untouched")
	}
	if len(log.writes) != 0 {
		t.Errorf("writes = %+v after opening an auto-collapsed umbrella; want none", log.writes)
	}

	m = clickCell(t, m, 4, screenRow(t, m, umbrellaHeaderLineOf(t, m, first)))

	if !m.transcript.blockFolded(first) {
		t.Error("a second click left the umbrella open; want it folded on its own flag")
	}
	if got := m.transcript.blockFoldBy(first); got != foldByHead {
		t.Errorf("blockFoldBy(first) = %d after the second click; want foldByHead", got)
	}
	if len(log.writes) != 0 {
		t.Errorf("writes = %+v after folding it again; want none", log.writes)
	}
}

// TestAutoCollapseTouchLiveLargeClickStillWrites pins the other side: a click on a LARGE umbrella
// that is not auto-collapsed — the running Turn's last — keeps today's shared toggle and its
// write, while the settled one stays folded; once the Turn ends the touched one stays open.
func TestAutoCollapseTouchLiveLargeClickStillWrites(t *testing.T) {
	t.Parallel()
	const settled, live = 1, 6

	log := &settingsWriteLog{}
	opts := testOpts
	opts.Settings = fakeSettingsHost{write: log.write}
	m := autoCollapseLargeModel(t, opts, true)

	m = clickCell(t, m, 4, screenRow(t, m, umbrellaHeaderLineOf(t, m, live)))

	if want := []settingEdit{{path: "ui.tools-open", value: "true"}}; !reflect.DeepEqual(log.writes, want) {
		t.Errorf("writes = %+v, want exactly %+v", log.writes, want)
	}
	if m.transcript.blockFolded(live) {
		t.Error("the live large umbrella did not open")
	}
	if !m.transcript.blockFolded(settled) {
		t.Error("the settled umbrella opened with the shared fold; want it still auto-collapsed")
	}
	m.transcript.setTurnLive(false)
	if m.transcript.blockFolded(live) {
		t.Error("the touched umbrella folded when the Turn ended; want it exempt")
	}
}

// TestAutoCollapsePanelJumpRevealsAFoldedMember pins the side-panel jump into a folded umbrella: the
// target — the second call of a multi-call run, the Terminal type row's — is revealed, its umbrella opened alone and
// touched, its type row opened, and nothing written.
func TestAutoCollapsePanelJumpRevealsAFoldedMember(t *testing.T) {
	t.Parallel()
	const head, run, member, other = 1, 3, 4, 6

	log := &settingsWriteLog{}
	opts := testOpts
	opts.Settings = fakeSettingsHost{write: log.write}
	m := autoCollapseLargeModel(t, opts, false)

	next, _ := m.followPanelTarget(panelTarget{kind: panelTargetBlock, entry: member, run: m.viewedRun()})
	m = next.(Model)

	if m.transcript.blockFolded(head) {
		t.Error("the jump left the member's umbrella folded")
	}
	if !m.transcript.entries[head].foldTouched || !m.transcript.entries[run].typeExpanded {
		t.Errorf("after the jump: umbrella touched %v, member's type row open %v; want both true",
			m.transcript.entries[head].foldTouched, m.transcript.entries[run].typeExpanded)
	}
	if !slices.ContainsFunc(m.lineTargets, func(target lineTarget) bool {
		return target.kind == targetHeader && target.entry == member
	}) {
		t.Errorf("the jump painted no row for the member:\n%s", strings.Join(m.lines, "\n"))
	}
	if !m.transcript.blockFolded(other) {
		t.Error("the other settled umbrella opened with the jump; want it folded")
	}
	if len(log.writes) != 0 || m.opts.UI.ToolsOpen {
		t.Errorf("writes = %+v, ToolsOpen = %v after the jump; want no write and the shared fold shut", log.writes, m.opts.UI.ToolsOpen)
	}
}

// autoCollapseGroupOfOneModel is a model under auto-collapse holding a settled group of one, a Read (entry 1), behind
// prompt 0, and a settled small umbrella (head 3) behind prompt 2: idle, both stand folded.
func autoCollapseGroupOfOneModel(t *testing.T, opts Options) Model {
	t.Helper()
	m := newTestModelEng(t, &fakeEngine{}, opts)
	m.transcript.reset()
	m.transcript.addUser("read the file", nil)
	readCall(&m.transcript, "one", "a.go", 1, 5, 0)
	m.transcript.addUser("now read both", nil)
	readCall(&m.transcript, "r1", "b.go", 1, 9, 0)
	readCall(&m.transcript, "r2", "c.go", 1, 7, 0)
	m.transcript.setToolsAutoCollapse(true)
	m.transcript.setTurnLive(false)
	m.refreshViewport()
	return m
}

// assertSettledGroupOfOneOpen fails unless the group of one headed by entry head — the Read of
// a.go — paints OPEN: its group header wearing the open fold's ▼, and its own block — its `┕ a.go` row — beneath.
func assertSettledGroupOfOneOpen(t *testing.T, m Model, head int) {
	t.Helper()
	header := strip(m.lines[umbrellaHeaderLineOf(t, m, head)])
	if !strings.HasSuffix(strings.TrimRight(header, " "), glyphExpanded) {
		t.Errorf("the group of one's header %q is not open:\n%s", header, strings.Join(m.lines, "\n"))
	}
	if !slices.ContainsFunc(m.lines, func(line string) bool { return strings.Contains(strip(line), "┕ a.go") }) {
		t.Errorf("the group of one paints no standalone row:\n%s", strings.Join(m.lines, "\n"))
	}
}

// TestAutoCollapseTouchOpensAFoldedGroupOfOne pins the folded group of one's gesture, by mouse and by ⏎: its
// folded line opens it alone onto its group of one — header and the call's own block — touched and
// writing nothing, the settled umbrella beside it staying folded; and the same gesture on that
// header folds it again (the defect where an opened single call could never fold again).
func TestAutoCollapseTouchOpensAFoldedGroupOfOne(t *testing.T) {
	t.Parallel()
	const one, umbrella = 1, 3

	for _, way := range []string{"mouse", "enter"} {
		t.Run(way, func(t *testing.T) {
			t.Parallel()

			log := &settingsWriteLog{}
			opts := testOpts
			opts.Settings = fakeSettingsHost{write: log.write}
			m := autoCollapseGroupOfOneModel(t, opts)
			if !m.transcript.blockFolded(one) || !m.transcript.blockFolded(umbrella) {
				t.Fatal("setup: the settled group of one and umbrella are not both folded")
			}

			if way == "mouse" {
				m = clickCell(t, m, 4, screenRow(t, m, umbrellaHeaderLineOf(t, m, one)))
			} else {
				m = cursorOnTarget(t, m, targetUmbrella, one)
				m = step(t, m, keyEnter())
			}

			if m.transcript.blockFolded(one) {
				t.Error("the folded group of one is still folded after the gesture")
			}
			if !m.transcript.entries[one].foldTouched {
				t.Error("the opened group of one is not touched")
			}
			assertSettledGroupOfOneOpen(t, m, one)
			if !m.transcript.blockFolded(umbrella) {
				t.Error("the settled umbrella opened with the group of one; want it folded")
			}

			if way == "mouse" {
				m = clickCell(t, m, 4, screenRow(t, m, umbrellaHeaderLineOf(t, m, one)))
			} else {
				m = step(t, m, keyEnter())
			}
			if !m.transcript.blockFolded(one) {
				t.Error("the opened group of one did not fold again on the same gesture")
			}
			if len(log.writes) != 0 {
				t.Errorf("writes = %+v after opening and folding a group of one; want none", log.writes)
			}
		})
	}
}

// TestAutoCollapseTouchedGroupOfOneStaysOpenAsItGrows pins the touched-group-grows rule: a
// live group of one the reader acted on, then joined by a second call, heads the
// same touched group, which stays as the reader left it, open, and never auto-collapses once it
// settles; the reader folds it by click.
func TestAutoCollapseTouchedGroupOfOneStaysOpenAsItGrows(t *testing.T) {
	t.Parallel()

	tr := &transcript{}
	tr.setToolsAutoCollapse(true)
	tr.setTurnLive(true)
	readCall(tr, "c1", "a.go", 1, 5, 0)
	if !tr.markBlockTouched(0) {
		t.Fatal("setup: the group of one was not marked touched")
	}

	runCall(tr, "c2", "go test", "ok", 0)
	tr.setTurnLive(false)

	if got := toolSuperGroup(tr.entries, 0).calls(); got != 2 {
		t.Fatalf("setup: entries[0] heads an umbrella of %d calls after the second call; want 2", got)
	}
	if !tr.entries[0].foldTouched {
		t.Error("the grown group lost the touch its group of one carried")
	}
	if got := tr.blockFoldBy(0); got == foldBySettled || tr.blockFolded(0) {
		t.Errorf("blockFoldBy(0) = %d, folded %v on the settled group; want it open — the touch carries",
			got, tr.blockFolded(0))
	}
}

// TestAutoCollapsePanelJumpRevealsAFoldedGroupOfOne pins the side-panel jump to a folded group of one:
// it opens alone and touched, its standalone block paints, and nothing is written.
func TestAutoCollapsePanelJumpRevealsAFoldedGroupOfOne(t *testing.T) {
	t.Parallel()
	const one, umbrella = 1, 3

	log := &settingsWriteLog{}
	opts := testOpts
	opts.Settings = fakeSettingsHost{write: log.write}
	m := autoCollapseGroupOfOneModel(t, opts)

	next, _ := m.followPanelTarget(panelTarget{kind: panelTargetBlock, entry: one, run: m.viewedRun()})
	m = next.(Model)

	if m.transcript.blockFolded(one) || !m.transcript.entries[one].foldTouched {
		t.Errorf("after the jump: group of one folded %v, touched %v; want false, true",
			m.transcript.blockFolded(one), m.transcript.entries[one].foldTouched)
	}
	assertSettledGroupOfOneOpen(t, m, one)
	if !m.transcript.blockFolded(umbrella) {
		t.Error("the settled umbrella opened with the jump; want it folded")
	}
	if len(log.writes) != 0 {
		t.Errorf("writes = %+v after the jump; want none", log.writes)
	}
}

// TestAutoCollapseEnteringAGroupMemberTouchesItsGroup pins the run-view redirect's touch: a click
// or ⏎ on a framed member row of an open Sub-agents group opens that member's run view AND marks
// the group touched, so a group that settles while the reader is inside it — the Turn ends behind
// them — stays open when they come back up.
func TestAutoCollapseEnteringAGroupMemberTouchesItsGroup(t *testing.T) {
	t.Parallel()
	const groupHead, secondMember = 1, 3

	for _, way := range []string{"mouse", "enter"} {
		t.Run(way, func(t *testing.T) {
			t.Parallel()

			m := newTestModelEng(t, &fakeEngine{}, testOpts)
			m.transcript.reset()
			m.transcript.addUser("survey both", nil)
			for _, d := range [][2]string{{"s1", "a.go"}, {"s2", "b.go"}} {
				subAgentCall(&m.transcript, d[0], "survey", 0)
				readCall(&m.transcript, "r"+d[0], d[1], 1, 5, 1)
				subAgentReport(&m.transcript, d[0], "all clear", 0)
			}
			m.transcript.setToolsAutoCollapse(true)
			m.transcript.setTurnLive(true)
			m.refreshViewport()
			if m.transcript.blockFolded(groupHead) {
				t.Fatal("setup: the running Turn's last group stands folded; want it open")
			}

			if way == "mouse" {
				m = clickCell(t, m, 4, screenRow(t, m, lastLineMarked(t, m, targetHeader, secondMember)))
			} else {
				m = step(t, cursorOnTarget(t, m, targetHeader, secondMember), keyEnter())
			}

			if got, want := m.viewedRun(), m.transcript.entries[secondMember].spawned(); got != want {
				t.Fatalf("viewed run = %+v after the gesture; want the member's run %+v", got, want)
			}
			if !m.transcript.entries[groupHead].foldTouched {
				t.Error("entering a member's run view did not mark its group touched")
			}

			m.transcript.setTurnLive(false)
			m = m.upRun()

			if !settledAt(&m.transcript, groupHead) {
				t.Fatal("setup: the group is not settled once the Turn ended")
			}
			if m.transcript.blockFolded(groupHead) {
				t.Errorf("the settled group the reader entered folded behind them:\n%s", strings.Join(m.lines, "\n"))
			}
		})
	}
}
