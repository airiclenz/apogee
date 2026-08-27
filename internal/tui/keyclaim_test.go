package tui

import "testing"

// TestKeyClaimOrderMatchesTheDocumentedPrecedence pins the overlay precedence [Model.handleKey]
// walks. The order is load-bearing — a surface that rises steals keys the one above it answers, and
// one that falls stops being reachable while the overlay above it is up — so it may only ever change
// on purpose, and this list is what makes "on purpose" visible in a diff.
//
// It reads the names rather than calling the claims: every entry's claim is exercised by its own
// pane's suite, and what this test is about is the sequence they are asked in.
func TestKeyClaimOrderMatchesTheDocumentedPrecedence(t *testing.T) {
	want := []string{
		"sessions browser",
		"settings pane",
		"picker",
		"autocomplete overlay",
		"usage report",
		"inspector pane",
		"block cursor",
	}
	if len(keyClaimOrder) != len(want) {
		t.Fatalf("keyClaimOrder has %d claimants; want %d (%v)", len(keyClaimOrder), len(want), want)
	}
	seen := map[string]bool{}
	for i, claimant := range keyClaimOrder {
		if claimant.name != want[i] {
			t.Errorf("keyClaimOrder[%d] = %q; want %q", i, claimant.name, want[i])
		}
		if claimant.claim == nil {
			t.Errorf("keyClaimOrder[%d] (%s) has no claim: a rung nobody can be asked at", i, claimant.name)
		}
		if seen[claimant.name] {
			t.Errorf("keyClaimOrder[%d] repeats the name %q: a pin on it would pin either rung", i, claimant.name)
		}
		seen[claimant.name] = true
	}
}

// TestTheFirstClaimantThatWantsAKeyAnswersIt drives the order rather than reading it: with both
// report panes up, esc belongs to both, and only the one listed higher may take it.
//
// The two are never open together in practice, which is exactly why they make the safe pair to prove
// the walk on — nothing about the frame depends on the answer, and the fall-through past a claimant
// that did not answer is what every rung below the first relies on.
func TestTheFirstClaimantThatWantsAKeyAnswersIt(t *testing.T) {
	m := newTestModel(t)
	m.usagePane = usagePane{open: true}
	m.inspector = inspectorPane{open: true}

	next := step(t, m, keyEsc())

	if next.usagePane.open {
		t.Error("/usage stayed open on esc; it sits above /inspect in keyClaimOrder and answers first")
	}
	if !next.inspector.open {
		t.Error("/inspect closed on the same esc; a claimed key must never reach the rung below it")
	}
}

// TestTabAtIdleWithHintsReachesTheFramesOwnVerb is the other half of tab's routing. The suggestion
// menu is opened by handleKey's own switch, BELOW the claim order, which is what leaves every
// claimant above it the tab it already had — the dropdown's second accept key most of all. What this
// pins is the fall-through: with no surface open, tab is claimed by nobody and reaches that verb.
func TestTabAtIdleWithHintsReachesTheFramesOwnVerb(t *testing.T) {
	var rec suggestCall
	m := typeDraft(t, modelWithOverlayRoom(t, 24, bandOpts(gatedSuggest(&rec))), "audit the parser")

	if _, _, claimed := m.claimKey(keyClaimOrder, keyTab()); claimed {
		t.Fatal("a claimant took tab at idle; the suggestion menu is the frame's own verb, under them all")
	}

	next := step(t, m, keyTab())

	if !next.autocomplete.active || next.autocomplete.kind != acSuggest {
		t.Errorf(
			"overlay = {active:%v kind:%v}, want tab to have opened the suggestion menu",
			next.autocomplete.active, next.autocomplete.kind,
		)
	}
}
