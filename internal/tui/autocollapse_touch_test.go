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
// call in an umbrella answers the umbrella's head, a lone groupable call answers itself, and a
// prompt and a note answer none.
func TestUmbrellaHeadOfFindsTheHeadFromAnyMember(t *testing.T) {
	t.Parallel()

	tr := &transcript{}
	tr.addUser("look around", nil)        // 0
	readCall(tr, "r1", "a.go", 1, 5, 0)   // 1: the head
	readCall(tr, "r2", "b.go", 1, 9, 0)   // 2: second of the Read run
	runCall(tr, "t1", "go test", "ok", 0) // 3: the Terminal run's head
	tr.addNote("saved")                   // 4
	runCall(tr, "t2", "go vet", "ok", 0)  // 5: a lone call
	for i, want := range []int{-1, 1, 1, 1, -1, 5} {
		head, ok := foldHeadOf(tr.entries, i)
		if ok != (want >= 0) || (ok && head != want) {
			t.Errorf("foldHeadOf(%d) = %d, %v; want %d, %v", i, head, ok, want, want >= 0)
		}
	}
}

// TestAutoCollapseTouchWritersMoveTheGeneration pins the two marks' writers to the paint's
// generation: each moves it when it moves the mark and holds it when there is nothing to move.
func TestAutoCollapseTouchWritersMoveTheGeneration(t *testing.T) {
	t.Parallel()

	tr := autoCollapseUmbrella(t)
	tr.addNote("saved")                 // 2: no umbrella here
	readCall(tr, "c3", "c.go", 1, 4, 0) // 3: a lone call
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
		{"openBlockAlone on a lone call", func() bool { return tr.openBlockAlone(3) }, true},
		{"markBlockTouched on that lone call again", func() bool { return tr.markBlockTouched(3) }, false},
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
	if lone := tr.entries[3]; !lone.loneTouched || lone.foldTouched || lone.foldOwn {
		t.Errorf("lone call after openBlockAlone: loneTouched %v, foldTouched %v, ownFold %v; want true, false, false",
			lone.loneTouched, lone.foldTouched, lone.foldOwn)
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

// autoCollapseLoneModel is a model under auto-collapse holding a settled lone Read (entry 1) behind
// prompt 0, and a settled small umbrella (head 3) behind prompt 2: idle, both stand folded.
func autoCollapseLoneModel(t *testing.T, opts Options) Model {
	t.Helper()
	m := newTestModelEng(t, &fakeEngine{}, opts)
	m.transcript.reset()
	m.transcript.addUser("read the file", nil)
	readCall(&m.transcript, "lone", "a.go", 1, 5, 0)
	m.transcript.addUser("now read both", nil)
	readCall(&m.transcript, "r1", "b.go", 1, 9, 0)
	readCall(&m.transcript, "r2", "c.go", 1, 7, 0)
	m.transcript.setToolsAutoCollapse(true)
	m.transcript.setTurnLive(false)
	m.refreshViewport()
	return m
}

// assertLoneCallOpen fails unless the lone Read of a.go at entry lone paints its standalone block —
// its `┕ a.go` row — and no fold header of its own.
func assertLoneCallOpen(t *testing.T, m Model, lone int) {
	t.Helper()
	if slices.ContainsFunc(m.lineTargets, func(target lineTarget) bool {
		return target.kind == targetUmbrella && target.entry == lone
	}) {
		t.Errorf("the lone call still paints its folded header:\n%s", strings.Join(m.lines, "\n"))
	}
	if !slices.ContainsFunc(m.lines, func(line string) bool { return strings.Contains(strip(line), "┕ a.go") }) {
		t.Errorf("the lone call paints no standalone row:\n%s", strings.Join(m.lines, "\n"))
	}
}

// TestAutoCollapseTouchOpensALoneCall pins the folded lone call's gesture, by mouse and by ⏎: its
// folded line opens it alone onto today's block, touched and writing nothing, and the settled
// umbrella beside it stays folded.
func TestAutoCollapseTouchOpensALoneCall(t *testing.T) {
	t.Parallel()
	const lone, umbrella = 1, 3

	for _, way := range []string{"mouse", "enter"} {
		t.Run(way, func(t *testing.T) {
			t.Parallel()

			log := &settingsWriteLog{}
			opts := testOpts
			opts.Settings = fakeSettingsHost{write: log.write}
			m := autoCollapseLoneModel(t, opts)
			if !m.transcript.blockFolded(lone) || !m.transcript.blockFolded(umbrella) {
				t.Fatal("setup: the settled lone call and umbrella are not both folded")
			}

			if way == "mouse" {
				m = clickCell(t, m, 4, screenRow(t, m, umbrellaHeaderLineOf(t, m, lone)))
			} else {
				m = cursorOnTarget(t, m, targetUmbrella, lone)
				m = step(t, m, keyEnter())
			}

			if m.transcript.blockFolded(lone) {
				t.Error("the folded lone call is still folded after the gesture")
			}
			if !m.transcript.entries[lone].loneTouched {
				t.Error("the opened lone call is not touched")
			}
			assertLoneCallOpen(t, m, lone)
			if !m.transcript.blockFolded(umbrella) {
				t.Error("the settled umbrella opened with the lone call; want it folded")
			}
			if len(log.writes) != 0 {
				t.Errorf("writes = %+v after opening a lone call; want none", log.writes)
			}
		})
	}
}

// TestAutoCollapseTouchedLoneCallLeavesItsUmbrellaUntouched pins that a lone call's touch is its
// own: a live lone call the reader opened, then joined by a second call, heads an umbrella that
// still auto-collapses once it settles.
func TestAutoCollapseTouchedLoneCallLeavesItsUmbrellaUntouched(t *testing.T) {
	t.Parallel()

	tr := &transcript{}
	tr.setToolsAutoCollapse(true)
	tr.setTurnLive(true)
	readCall(tr, "c1", "a.go", 1, 5, 0)
	if !tr.markBlockTouched(0) {
		t.Fatal("setup: the lone call was not marked touched")
	}

	runCall(tr, "c2", "go test", "ok", 0)
	tr.setTurnLive(false)

	if len(toolSuperGroup(tr.entries, 0)) == 0 {
		t.Fatal("setup: entries[0] heads no umbrella after the second call")
	}
	if got := tr.blockFoldBy(0); got != foldBySettled {
		t.Errorf("blockFoldBy(0) = %d on the settled umbrella; want foldBySettled — the lone call's touch does not carry", got)
	}
}

// TestAutoCollapsePanelJumpRevealsAFoldedLoneCall pins the side-panel jump to a folded lone call:
// it opens alone and touched, its standalone block paints, and nothing is written.
func TestAutoCollapsePanelJumpRevealsAFoldedLoneCall(t *testing.T) {
	t.Parallel()
	const lone, umbrella = 1, 3

	log := &settingsWriteLog{}
	opts := testOpts
	opts.Settings = fakeSettingsHost{write: log.write}
	m := autoCollapseLoneModel(t, opts)

	next, _ := m.followPanelTarget(panelTarget{kind: panelTargetBlock, entry: lone, run: m.viewedRun()})
	m = next.(Model)

	if m.transcript.blockFolded(lone) || !m.transcript.entries[lone].loneTouched {
		t.Errorf("after the jump: lone call folded %v, touched %v; want false, true",
			m.transcript.blockFolded(lone), m.transcript.entries[lone].loneTouched)
	}
	assertLoneCallOpen(t, m, lone)
	if !m.transcript.blockFolded(umbrella) {
		t.Error("the settled umbrella opened with the jump; want it folded")
	}
	if len(log.writes) != 0 {
		t.Errorf("writes = %+v after the jump; want none", log.writes)
	}
}
