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

// TestUmbrellaHeadOfFindsTheHeadFromAnyMember pins the walk back to an umbrella's head: every
// call in it answers the head, and a prompt, a note and a lone call answer none.
func TestUmbrellaHeadOfFindsTheHeadFromAnyMember(t *testing.T) {
	t.Parallel()

	tr := &transcript{}
	tr.addUser("look around", nil)        // 0
	readCall(tr, "r1", "a.go", 1, 5, 0)   // 1: the head
	readCall(tr, "r2", "b.go", 1, 9, 0)   // 2: second of the Read run
	runCall(tr, "t1", "go test", "ok", 0) // 3: the Terminal run's head
	tr.addNote("saved")                   // 4
	runCall(tr, "t2", "go vet", "ok", 0)  // 5: a lone call
	for i, want := range []int{-1, 1, 1, 1, -1, -1} {
		head, ok := umbrellaHeadOf(tr.entries, i)
		if ok != (want >= 0) || (ok && head != want) {
			t.Errorf("umbrellaHeadOf(%d) = %d, %v; want %d, %v", i, head, ok, want, want >= 0)
		}
	}
}

// TestAutoCollapseTouchWritersMoveTheGeneration pins the two marks' writers to the paint's
// generation: each moves it when it moves the mark and holds it when there is nothing to move.
func TestAutoCollapseTouchWritersMoveTheGeneration(t *testing.T) {
	t.Parallel()

	tr := autoCollapseUmbrella(t)
	tr.addNote("saved") // 2: no umbrella here

	for _, tc := range []struct {
		name  string
		act   func() bool
		moves bool
	}{
		{"markUmbrellaTouched from a member", func() bool { return tr.markUmbrellaTouched(1) }, true},
		{"markUmbrellaTouched again", func() bool { return tr.markUmbrellaTouched(0) }, false},
		{"markUmbrellaTouched outside an umbrella", func() bool { return tr.markUmbrellaTouched(2) }, false},
		{"openUmbrellaAlone", func() bool { return tr.openUmbrellaAlone(0) }, true},
		{"openUmbrellaAlone again", func() bool { return tr.openUmbrellaAlone(0) }, false},
		{"openUmbrellaAlone on a member", func() bool { return tr.openUmbrellaAlone(2) }, false},
	} {
		before := tr.generation
		if got := tc.act(); got != tc.moves {
			t.Errorf("%s reported %v; want %v", tc.name, got, tc.moves)
		}
		if moved := tr.generation != before; moved != tc.moves {
			t.Errorf("%s moved the generation: %v; want %v", tc.name, moved, tc.moves)
		}
	}
	if head := tr.entries[0]; !head.umbrellaTouched || !head.umbrellaOwnFold || head.umbrellaFolded {
		t.Errorf("head after openUmbrellaAlone: touched %v, ownFold %v, folded %v; want true, true, false",
			head.umbrellaTouched, head.umbrellaOwnFold, head.umbrellaFolded)
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

				if !m.transcript.entries[live].umbrellaTouched {
					t.Errorf("the %s gesture left the umbrella untouched", tc.name)
				}
				if m.transcript.entries[settled].umbrellaTouched {
					t.Error("the other umbrella was marked touched by a gesture on this one")
				}
				m.transcript.setTurnLive(false)
				if got := m.transcript.umbrellaFoldBy(live); got == foldBySettled {
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
	if !m.transcript.umbrellaFolded(first) || !m.transcript.umbrellaFolded(second) {
		t.Fatal("setup: the two settled umbrellas are not both folded")
	}

	m = clickCell(t, m, 4, screenRow(t, m, umbrellaHeaderLineOf(t, m, first)))

	if m.transcript.umbrellaFolded(first) {
		t.Error("the clicked auto-collapsed umbrella is still folded")
	}
	typeRowLine(t, m, first) // it lists its type rows
	if !m.transcript.umbrellaFolded(second) {
		t.Error("the other settled umbrella opened with it; want it folded")
	}
	if m.opts.UI.ToolsOpen || m.transcript.toolsOpen {
		t.Error("the open moved the shared ui.tools-open fold; want it untouched")
	}
	if len(log.writes) != 0 {
		t.Errorf("writes = %+v after opening an auto-collapsed umbrella; want none", log.writes)
	}

	m = clickCell(t, m, 4, screenRow(t, m, umbrellaHeaderLineOf(t, m, first)))

	if !m.transcript.umbrellaFolded(first) {
		t.Error("a second click left the umbrella open; want it folded on its own flag")
	}
	if got := m.transcript.umbrellaFoldBy(first); got != foldByHead {
		t.Errorf("umbrellaFoldBy(first) = %d after the second click; want foldByHead", got)
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
	if m.transcript.umbrellaFolded(live) {
		t.Error("the live large umbrella did not open")
	}
	if !m.transcript.umbrellaFolded(settled) {
		t.Error("the settled umbrella opened with the shared fold; want it still auto-collapsed")
	}
	m.transcript.setTurnLive(false)
	if m.transcript.umbrellaFolded(live) {
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

	if m.transcript.umbrellaFolded(head) {
		t.Error("the jump left the member's umbrella folded")
	}
	if !m.transcript.entries[head].umbrellaTouched || !m.transcript.entries[run].typeExpanded {
		t.Errorf("after the jump: umbrella touched %v, member's type row open %v; want both true",
			m.transcript.entries[head].umbrellaTouched, m.transcript.entries[run].typeExpanded)
	}
	if !slices.ContainsFunc(m.lineTargets, func(target lineTarget) bool {
		return target.kind == targetHeader && target.entry == member
	}) {
		t.Errorf("the jump painted no row for the member:\n%s", strings.Join(m.lines, "\n"))
	}
	if !m.transcript.umbrellaFolded(other) {
		t.Error("the other settled umbrella opened with the jump; want it folded")
	}
	if len(log.writes) != 0 || m.opts.UI.ToolsOpen {
		t.Errorf("writes = %+v, ToolsOpen = %v after the jump; want no write and the shared fold shut", log.writes, m.opts.UI.ToolsOpen)
	}
}
