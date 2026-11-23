package tui

import "testing"

// settledUmbrella is autoCollapseUmbrella with a follower: the two-call umbrella at entries 0 and 1
// (a Read run and a Terminal run, two type rows) settled by the note at 2, untouched — folded.
func settledUmbrella(t *testing.T) *transcript {
	t.Helper()
	tr := autoCollapseUmbrella(t)
	tr.addNote("saved") // 2: the follower
	if !tr.blockFolded(0) {
		t.Fatal("setup: the settled, untouched umbrella stands open; want it folded")
	}
	return tr
}

// foldActSubAgentGroup is a transcript holding a two-member Sub-agents group under auto-collapse
// with the Turn running: a prompt at 0, then the delegations at 1 (the group's head) and 3, each
// with a read of its own behind it.
func foldActSubAgentGroup(t *testing.T) *transcript {
	t.Helper()
	tr := &transcript{}
	tr.addUser("survey both", nil)
	for _, d := range [][2]string{{"s1", "a.go"}, {"s2", "b.go"}} {
		subAgentCall(tr, d[0], "survey", 0)
		readCall(tr, "r"+d[0], d[1], 1, 5, 1)
		subAgentReport(tr, d[0], "all clear", 0)
	}
	tr.setToolsAutoCollapse(true)
	tr.setTurnLive(true)
	return tr
}

// TestFoldActHeaderOpensAnAutoCollapsedBlockAlone pins the settled arm: a header act on an
// auto-collapsed umbrella opens it on a fold of its own and never asks for the shared flip — small,
// or large under a shut preference alike — and the umbrella stays open past a further follower.
func TestFoldActHeaderOpensAnAutoCollapsedBlockAlone(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		foldOver int
	}{
		{"small", 0},
		{"large", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tr := settledUmbrella(t)
			tr.setToolsFoldOver(tc.foldOver)

			outcome := tr.applyFoldAct(foldAct{kind: foldActHeader, index: 0})

			if want := (foldOutcome{acted: true}); outcome != want {
				t.Errorf("outcome = %+v; want %+v", outcome, want)
			}
			tr.addNote("read") // a further follower
			if tr.blockFolded(0) {
				t.Error("blockFolded(0) = true after the header act; want the umbrella open")
			}
			if tr.toolsOpen {
				t.Error("the header act opened the shared preference; want it untouched")
			}
		})
	}
}

// TestFoldActHeaderOnLargeAsksForTheSharedFlip pins the shared arm: a header act on a LARGE umbrella
// under the shared preference touches it and hands the flip back — the transcript's preference and
// the fold stand as they were until the caller makes it, and then the fold is the one the outcome
// named.
func TestFoldActHeaderOnLargeAsksForTheSharedFlip(t *testing.T) {
	t.Parallel()

	tr := autoCollapseUmbrella(t)
	tr.setToolsFoldOver(1)
	tr.setTurnLive(false)
	tr.setToolsAutoCollapse(false)
	if !tr.blockFolded(0) {
		t.Fatal("setup: the large umbrella under a shut preference stands open; want it folded")
	}

	outcome := tr.applyFoldAct(foldAct{kind: foldActHeader, index: 0})

	if want := (foldOutcome{acted: true, flipToolsOpen: true}); outcome != want {
		t.Errorf("outcome = %+v; want %+v", outcome, want)
	}
	if tr.toolsOpen || !tr.blockFolded(0) {
		t.Errorf("before the caller's flip: toolsOpen %v, folded %v; want false, true", tr.toolsOpen, tr.blockFolded(0))
	}
	if !tr.entries[0].foldTouched {
		t.Error("the header act did not mark the large umbrella touched")
	}

	tr.setToolsOpen(!tr.toolsOpen) // the caller's flip (Model.actOnFold)
	tr.addNote("saved")            // a follower
	if got := tr.blockFolded(0); got != outcome.folded {
		t.Errorf("blockFolded(0) = %v after the flip; want the outcome's %v", got, outcome.folded)
	}
}

// TestFoldActHeaderFoldsASmallOneOnItsOwn pins the head-flag arm: a small umbrella's header act
// folds it on its own flag, writes no shared preference, touches it, and a second act opens it.
func TestFoldActHeaderFoldsASmallOneOnItsOwn(t *testing.T) {
	t.Parallel()

	tr := autoCollapseUmbrella(t) // open: the running Turn's last block

	folded := tr.applyFoldAct(foldAct{kind: foldActHeader, index: 0})
	tr.addNote("saved") // a follower: touched, it does not auto-collapse

	if want := (foldOutcome{acted: true, folded: true}); folded != want {
		t.Errorf("first act: outcome = %+v; want %+v", folded, want)
	}
	if !tr.blockFolded(0) {
		t.Error("blockFolded(0) = false after the first header act; want true")
	}

	opened := tr.applyFoldAct(foldAct{kind: foldActHeader, index: 0})

	if want := (foldOutcome{acted: true}); opened != want {
		t.Errorf("second act: outcome = %+v; want %+v", opened, want)
	}
	if tr.blockFolded(0) || tr.toolsOpen {
		t.Errorf("after the second act: folded %v, toolsOpen %v; want false, false", tr.blockFolded(0), tr.toolsOpen)
	}
}

// TestFoldActOpeningAPartTouches pins the type-row and member acts: each flips what it names and
// touches the umbrella holding it, so a follower does not auto-collapse the umbrella under it.
func TestFoldActOpeningAPartTouches(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		act     foldAct
		flipped func(tr *transcript) bool
	}{
		{"type row", foldAct{kind: foldActTypeRow, index: 1}, func(tr *transcript) bool { return tr.entries[1].typeExpanded }},
		{"member", foldAct{kind: foldActMember, index: 1}, func(tr *transcript) bool { return tr.entries[1].expanded }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tr := autoCollapseUmbrella(t)

			outcome := tr.applyFoldAct(tc.act)
			tr.addNote("saved") // the follower that would settle it

			if want := (foldOutcome{acted: true}); outcome != want {
				t.Errorf("outcome = %+v; want %+v", outcome, want)
			}
			if !tc.flipped(tr) {
				t.Error("the act did not flip what it names")
			}
			if tr.blockFolded(0) {
				t.Error("blockFolded(0) = true after a follower; want the touched umbrella open")
			}
		})
	}
}

// TestFoldActEnteringARunTouchesItsGroup pins the enter-run act: entering a member's run touches the
// Sub-agents group holding it, so the group stays open when the Turn ends behind the reader.
func TestFoldActEnteringARunTouchesItsGroup(t *testing.T) {
	t.Parallel()
	const groupHead, secondMember = 1, 3

	tr := foldActSubAgentGroup(t)

	outcome := tr.applyFoldAct(foldAct{kind: foldActEnterRun, index: secondMember})
	tr.setTurnLive(false) // the Turn ends behind the reader

	if want := (foldOutcome{acted: true}); outcome != want {
		t.Errorf("outcome = %+v; want %+v", outcome, want)
	}
	if tr.blockFolded(groupHead) {
		t.Error("the entered group folded when the Turn ended; want it open")
	}
}

// TestFoldActRevealOpensAloneAndItsTypeRow pins the reveal act: a folded umbrella opens on a fold of
// its own and the member's type row opens; an open one is only touched; a group of one and a
// Sub-agents group open with no type-row flag left behind.
func TestFoldActRevealOpensAloneAndItsTypeRow(t *testing.T) {
	t.Parallel()

	t.Run("folded umbrella", func(t *testing.T) {
		t.Parallel()

		tr := settledUmbrella(t)
		tr.setToolsFoldOver(1) // large: the reveal still never asks for the shared flip

		outcome := tr.applyFoldAct(foldAct{kind: foldActReveal, index: 1})
		tr.addNote("read") // a follower

		if want := (foldOutcome{acted: true}); outcome != want {
			t.Errorf("outcome = %+v; want %+v", outcome, want)
		}
		if tr.blockFolded(0) || !tr.entries[0].foldOwn || !tr.entries[1].typeExpanded {
			t.Errorf("after the reveal: folded %v, foldOwn %v, member's type row open %v; want false, true, true",
				tr.blockFolded(0), tr.entries[0].foldOwn, tr.entries[1].typeExpanded)
		}
	})

	t.Run("open umbrella", func(t *testing.T) {
		t.Parallel()

		tr := autoCollapseUmbrella(t)

		tr.applyFoldAct(foldAct{kind: foldActReveal, index: 1})
		tr.addNote("saved") // a follower

		if head := tr.entries[0]; !head.foldTouched || head.foldOwn || tr.blockFolded(0) {
			t.Errorf("after the reveal: touched %v, foldOwn %v, folded %v; want true, false, false",
				head.foldTouched, head.foldOwn, tr.blockFolded(0))
		}
	})

	t.Run("group of one", func(t *testing.T) {
		t.Parallel()

		tr := &transcript{}
		tr.setToolsAutoCollapse(true)
		readCall(tr, "c1", "a.go", 1, 5, 0) // 0: a group of one
		tr.addNote("saved")                 // 1: the follower
		if !tr.blockFolded(0) {
			t.Fatal("setup: the settled group of one stands open; want it folded")
		}

		tr.applyFoldAct(foldAct{kind: foldActReveal, index: 0})

		if tr.blockFolded(0) || tr.entries[0].typeExpanded {
			t.Errorf("after the reveal: folded %v, type row flag %v; want false, false", tr.blockFolded(0), tr.entries[0].typeExpanded)
		}
	})

	t.Run("sub-agents group", func(t *testing.T) {
		t.Parallel()
		const groupHead, secondMember = 1, 3

		tr := foldActSubAgentGroup(t)
		tr.setTurnLive(false)
		if !tr.blockFolded(groupHead) {
			t.Fatal("setup: the settled group stands open; want it folded")
		}

		tr.applyFoldAct(foldAct{kind: foldActReveal, index: secondMember})

		if tr.blockFolded(groupHead) || tr.entries[groupHead].typeExpanded || tr.entries[secondMember].typeExpanded {
			t.Errorf("after the reveal: folded %v, type row flags %v, %v; want false, false, false",
				tr.blockFolded(groupHead), tr.entries[groupHead].typeExpanded, tr.entries[secondMember].typeExpanded)
		}
	})
}

// TestFoldActTouchCarriesAsTheGroupGrows pins the touch on a group of one to its head: a member act
// on the lone call touches it, a second call grows it into an umbrella, and the grown umbrella stays
// open past a follower.
func TestFoldActTouchCarriesAsTheGroupGrows(t *testing.T) {
	t.Parallel()

	tr := &transcript{}
	tr.setToolsAutoCollapse(true)
	tr.setTurnLive(true)
	readCall(tr, "c1", "a.go", 1, 5, 0) // 0: a group of one

	tr.applyFoldAct(foldAct{kind: foldActMember, index: 0})
	runCall(tr, "c2", "go test", "ok", 0) // 1: the group grows into an umbrella
	tr.addNote("saved")                   // 2: the follower

	if block := toolBlockAt(tr.entries, 1); block.kind != shapeToolSuper || block.head != 0 {
		t.Fatalf("setup: entry 1 sits in block %+v; want the umbrella headed by 0", block)
	}
	if tr.blockFolded(0) {
		t.Error("the grown umbrella folded past the follower; want the touch to carry")
	}
}

// TestFoldActMovesTheGenerationOnlyOnAMove pins the paint's generation to the acts: a header act that
// moves a fold or a touch, and a member act that flips a block, move it; a header act whose only
// effect is the caller's shared flip, and an act on no block, hold it.
func TestFoldActMovesTheGenerationOnlyOnAMove(t *testing.T) {
	t.Parallel()

	tr := autoCollapseUmbrella(t)
	tr.addNote("saved")                  // 2: no block here
	tr.setToolsAutoCollapse(false)       // every umbrella folds by its size
	readCall(tr, "c3", "c.go", 1, 4, 0)  // 3: the head of a second umbrella…
	runCall(tr, "c4", "go vet", "ok", 0) // 4: …small until the threshold moves

	for _, tc := range []struct {
		name  string
		act   foldAct
		setup func()
		moves bool
	}{
		{"header on a small umbrella", foldAct{kind: foldActHeader, index: 0}, nil, true},
		{"member", foldAct{kind: foldActMember, index: 1}, nil, true},
		{"header on a large umbrella, first touch", foldAct{kind: foldActHeader, index: 3}, func() { tr.setToolsFoldOver(1) }, true},
		{"header on that touched large umbrella", foldAct{kind: foldActHeader, index: 3}, nil, false},
		{"member on no block", foldAct{kind: foldActMember, index: 99}, nil, false},
		{"header on no block", foldAct{kind: foldActHeader, index: 2}, nil, false},
	} {
		if tc.setup != nil {
			tc.setup()
		}
		before := tr.generation
		tr.applyFoldAct(tc.act)
		if moved := tr.generation != before; moved != tc.moves {
			t.Errorf("%s moved the generation: %v; want %v", tc.name, moved, tc.moves)
		}
	}
}

// TestFoldActOnNoBlockDoesNothing pins the acts that need a foldable block: on a prompt or a note
// each answers the zero outcome and moves nothing.
func TestFoldActOnNoBlockDoesNothing(t *testing.T) {
	t.Parallel()

	for _, kind := range []foldActKind{foldActHeader, foldActTypeRow, foldActReveal, foldActEnterRun} {
		tr := &transcript{}
		tr.setToolsAutoCollapse(true)
		tr.addUser("look around", nil) // 0
		readCall(tr, "c1", "a.go", 1, 5, 0)
		tr.addNote("saved") // 2

		for _, index := range []int{0, 2, 99} {
			before := tr.generation
			if outcome := tr.applyFoldAct(foldAct{kind: kind, index: index}); outcome != (foldOutcome{}) {
				t.Errorf("act %d on %d: outcome = %+v; want none", kind, index, outcome)
			}
			if tr.generation != before {
				t.Errorf("act %d on %d moved the generation", kind, index)
			}
		}
	}
}
