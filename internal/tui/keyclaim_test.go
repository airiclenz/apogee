package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/domain"
)

// TestKeyClaimOrderMatchesTheDocumentedPrecedence pins the overlay precedence [Model.handleKey]
// walks. The order is load-bearing — a surface that rises steals keys the one above it answers, and
// one that falls stops being reachable while the overlay above it is up — so it may only ever change
// on purpose, and this list is what makes "on purpose" visible in a diff.
//
// It reads the names rather than calling the claims: every entry's claim is exercised by its own
// pane's suite, and what this test is about is the sequence they are asked in. The pane rungs are
// derived from each row's key rank (paneRank, panes.go) and named by the row, so a rank changed on a
// row is a change this list sees.
func TestKeyClaimOrderMatchesTheDocumentedPrecedence(t *testing.T) {
	t.Parallel()
	want := []string{
		"sessions browser",
		"settings pane",
		"picker",
		"workflows view",
		"autocomplete dropdown",
		"usage report",
		"inspector pane",
		"thinking pane",
		"advice pane",
		"mcp pane",
		"prompt",
		"run view",
		"navigation mode",
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

// TestPromptKeyClaimIsSoftModal asks the prompt's rung directly — its row's key claim, with no
// handleKey around it — which keys it takes. It is SOFT-modal: an approval claims its menu (the
// decision letters, ↑/↓ and ⏎) and declines the keys the frame answers (the page keys, the mode
// chord, tab, ctrl+g) and any letter that is not a decision; an ask claims ⏎ and esc, and the
// offering's ↑/↓/␣ only while the answer box is empty, and declines what is typed into that box and
// the esc that leaves a watch.
func TestPromptKeyClaimIsSoftModal(t *testing.T) {
	t.Parallel()
	claim := paneSpecs[panePrompt].key

	t.Run("approval", func(t *testing.T) {
		t.Parallel()
		m, reply, _ := projectRulePane(t, terminalGate("go test ./..."), nil)
		claims := func(k tea.KeyPressMsg) bool {
			_, _, claimed := claim(m, k)
			select { // an armed decision key answers; empty the reply so the next one can too
			case <-reply:
			default:
			}
			return claimed
		}
		for _, k := range []tea.KeyPressMsg{keyRune('a'), keyRune('s'), keyRune('d'), keyRune('p'), keyUp(), keyDown(), keyEnter()} {
			if !claims(k) {
				t.Errorf("the approval declined %q; it is the menu's", k.String())
			}
		}
		for _, k := range []tea.KeyPressMsg{keyPgUp(), keyPgDown(), keyShiftTab(), keyTab(), keyCtrlG(), keyRune('x')} {
			if claims(k) {
				t.Errorf("the approval claimed %q; it belongs to the frame", k.String())
			}
		}
	})

	t.Run("ask", func(t *testing.T) {
		t.Parallel()
		m, reply := newAskModel(t, domain.AskRequest{Question: "which?", Choices: []string{"left", "right"}, MultiSelect: true})
		claims := func(m Model, k tea.KeyPressMsg) bool {
			_, _, claimed := claim(m, k)
			select { // an armed ⏎ answers; empty the reply so the next one can too
			case <-reply:
			default:
			}
			return claimed
		}
		for _, k := range []tea.KeyPressMsg{keyEnter(), keyEsc(), keyUp(), keyDown(), keySpace()} {
			if !claims(m, k) {
				t.Errorf("the ask declined %q on an empty box; it is the pane's", k.String())
			}
		}
		for _, k := range []tea.KeyPressMsg{keyRune('x'), keyRune('a')} {
			if claims(m, k) {
				t.Errorf("the ask claimed the printable %q; it types the answer", k.String())
			}
		}
		watching := m
		watching.watch.isActive = true
		if claims(watching, keyEsc()) {
			t.Error("the ask claimed esc while watching; that esc leaves the watch")
		}

		typed, _ := newAskModel(t, domain.AskRequest{Question: "which?", Choices: []string{"left", "right"}, MultiSelect: true})
		typed.input.SetValue("free text")
		for _, k := range []tea.KeyPressMsg{keyUp(), keyDown(), keySpace()} {
			if _, _, claimed := claim(typed, k); claimed {
				t.Errorf("the ask claimed %q over a typed answer; it is the box's cursor key", k.String())
			}
		}
	})
}

// TestTheFirstClaimantThatWantsAKeyAnswersIt drives the order rather than reading it: with both
// report panes up, esc belongs to both, and only the one listed higher may take it.
//
// The two are never open together in practice, which is exactly why they make the safe pair to prove
// the walk on — nothing about the frame depends on the answer, and the fall-through past a claimant
// that did not answer is what every rung below the first relies on.
func TestTheFirstClaimantThatWantsAKeyAnswersIt(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
