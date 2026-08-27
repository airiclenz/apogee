package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/scheme"
	"github.com/airiclenz/apogee/internal/skills"
)

// ----------------------------------------------------------------------------
// The command, file and skill dropdown adopts the shared popup chrome (selector-popup plan §3)
// ----------------------------------------------------------------------------

// newDropdownModel builds a ready, idle model at the 100×30 harness window (so the dropdown pane
// spans the 98-column chat area) over the given options.
func newDropdownModel(t *testing.T, opts Options) Model {
	t.Helper()
	m := newModel(context.Background(), &fakeEngine{}, opts, nil)
	return step(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
}

// A dropdown carrying a skill row is painted with the shared popup chrome: a title row, the rounded
// box border, and the ❯ marker on the selected row — all above the bottom input chrome.
func TestAutocompleteSkillDropdownChrome(t *testing.T) {
	m := newDropdownModel(t, skillOpts())
	// "zzz" is the draft's own marker word — no menu row can carry it, so its position in the view
	// is the input box's. No command verb has the "clean" prefix, so the sole row is the skill's.
	m.input.SetValue("zzz /clean")
	m.autocomplete = m.computeAutocomplete(m.caretByteOffset())

	view := plain(m.View())
	if !strings.Contains(view, "commands and skills") {
		t.Errorf("View is missing the \"commands and skills\" title row:\n%s", view)
	}
	for _, glyph := range []string{"╭", "╰"} {
		if !strings.Contains(view, glyph) {
			t.Errorf("View is missing the popup border glyph %q:\n%s", glyph, view)
		}
	}
	if !strings.Contains(view, glyphUser+" "+glyphSkill+" /clean-code") {
		t.Errorf("the selected row does not lead with the %q marker:\n%s", glyphUser, view)
	}
	// The dropdown sits above the input box: the title precedes the typed draft.
	title, input := strings.Index(view, "commands and skills"), strings.Index(view, "zzz")
	if title < 0 || input < 0 || title > input {
		t.Errorf("dropdown not above the input box: title@%d input@%d\n%s", title, input, view)
	}
}

// The "/" command menu carries the "commands" title; an "@" file token carries the "files" title.
func TestAutocompleteCommandAndFileTitles(t *testing.T) {
	cmd := newDropdownModel(t, testOpts)
	cmd.input.SetValue("/")
	cmd.autocomplete = cmd.computeAutocomplete(cmd.caretByteOffset())
	if got := plain(cmd.View()); !strings.Contains(got, "commands") {
		t.Errorf("the \"/\" menu is missing the \"commands\" title:\n%s", got)
	}

	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "main.go"), "package main")
	opts := testOpts
	opts.Workspace = dir
	files := newDropdownModel(t, opts)
	files.input.SetValue("@main")
	files.autocomplete = files.computeAutocomplete(files.caretByteOffset())
	if got := plain(files.View()); !strings.Contains(got, "files") {
		t.Errorf("an \"@\" token is missing the \"files\" title:\n%s", got)
	}
}

// The merged "/" menu mixes rows whose first column is a short verb ("/clear") with rows whose
// first column is a glyph plus a long id ("✦ /clean-code"), and every one of them starts its
// description at the SAME display column — the widest first cell plus the module's gutter. Before
// the column contract each producer concatenated its own two-space gap, so the summaries stepped
// raggedly down the pane behind whatever name preceded them.
func TestSlashMenuSummariesShareOneColumn(t *testing.T) {
	m := newDropdownModel(t, skillOpts())
	m.input.SetValue("/c") // the four c-verbs plus the clean-code skill: first cells of very different widths
	m.autocomplete = m.computeAutocomplete(m.caretByteOffset())

	items := m.autocomplete.items
	if len(items) < 2 {
		t.Fatalf("merged menu = %+v, want several rows to align against each other", items)
	}
	rows := make([]popupRow, len(items))
	widest := 0
	for i, it := range items {
		if len(it.cells) < 2 || it.cells[1] == "" {
			t.Fatalf("row %q cells = %q, want a description cell in the shared column", it.value, it.cells)
		}
		rows[i] = it.cells
		widest = max(widest, ansi.StringWidth(it.cells[0]))
	}
	if widest == ansi.StringWidth(items[0].cells[0]) {
		t.Fatalf("every first cell is %d cells wide — the test premise needs uneven names: %+v", widest, items)
	}

	want := widest + len(popupGutter)
	for i, ln := range layoutPopupRows(newTheme(scheme.Default()), rows) {
		if got := popupCellOffset(t, ln, items[i].cells[1]); got != want {
			t.Errorf("row %q starts its description at column %d, want %d: %q", items[i].value, got, want, ln)
		}
	}
}

// Every physical line of the dropdown pane spans exactly the full window width (m.width, 100 at
// the 100×30 harness window) — flush with the input box below it, the same width the /sessions
// popup spans.
func TestAutocompleteDropdownSpansFullWidth(t *testing.T) {
	m := newDropdownModel(t, skillOpts())
	wantWidth := m.width // the full window width, matching the input box
	m.input.SetValue("/c")
	m.autocomplete = m.computeAutocomplete(m.caretByteOffset())
	for i, ln := range popupLines(m.renderAutocomplete()) {
		if w := lipgloss.Width(ln); w != wantWidth {
			t.Errorf("dropdown line %d is %d cells, want %d: %q", i, w, wantWidth, strip(ln))
		}
	}
}

// The dropdown's window shrank to the frame's budget (renderAutocomplete → popupBudget), and this
// is the property that shrinking may not cost: the row the human is ON stays on the screen. A menu
// is a live filter over a moving list — every keystroke re-derives the rows and the arrow keys walk
// them — so a pane that seated the FIRST n rows of a fourteen-verb menu would leave the selection
// invisible from the third ↓ onwards on any window short enough to matter, which is worse than the
// overflow the budget fixed: the human would be accepting a row they cannot see. popupRowWindow is
// what holds it — the window scrolls around the selection rather than starting at row zero — and
// this asserts it through the composed pane at every short height, for every position the selection
// can take, so the guarantee is the drawn one and not the arithmetic's.
//
// The banded rows are the same property under the frame's SHARED allocation: a staged queue beside
// the menu takes its rows out of the same viewport, so the granted window is smaller than the same
// terminal gives an unaccompanied dropdown — and the row the human has arrowed onto is still in it.
func TestAutocompleteSelectionStaysOnScreenAtEveryBudget(t *testing.T) {
	cases := []struct{ height, staged int }{
		{18, 0}, {20, 0}, {22, 0}, {24, 0}, {30, 0},
		{22, 2}, {24, 2}, {30, 2},
	}
	for _, c := range cases {
		m := withStagedRows(modelWithOverlayRoomAt(t, 80, c.height, Options{Workspace: "/ws/a"}), c.staged)
		// A bound model with a thinking-effort dial, so the menu offers the table WHOLE: /effort is
		// the one row a dial-less binding withholds (commandSpec.gatedByEffort).
		m.hb.effort = provider.EffortSupport{Supported: true}
		m.input.SetValue("/") // the whole verb table: more rows than these windows can seat
		m.autocomplete = m.computeAutocomplete(m.caretByteOffset())
		if len(m.autocomplete.items) != len(commandSpecs) {
			t.Fatalf("the \"/\" menu offered %d rows, want every verb (%d)", len(m.autocomplete.items), len(commandSpecs))
		}
		if _, shown, _ := m.popupBudget(paneDropdown, len(m.autocomplete.items), maxAutocompleteItems, popupChrome, popupFloor{}); shown < 1 {
			t.Fatalf("a %d-row window with %d staged granted no dropdown rows — test premise broken", c.height, c.staged)
		}

		for i, it := range m.autocomplete.items {
			t.Run(fmt.Sprintf("%d rows/%d staged/select %s", c.height, c.staged, it.value), func(t *testing.T) {
				sel := m
				sel.autocomplete.selected = i
				flat := ansiPattern.ReplaceAllString(sel.renderAutocomplete(), "")
				// The marker is on the selected row and nowhere else, so finding the verb BESIDE it
				// is the proof the highlighted row is the one drawn — not merely that the text is
				// somewhere in the pane.
				if want := glyphUser + " /" + it.value; !strings.Contains(flat, want) {
					t.Errorf("row %d (%q) selected but not on the screen at %d rows with %d staged; want a row leading %q:\n%s",
						i, it.value, c.height, c.staged, want, flat)
				}
			})
		}
	}
}

// TestModalPromptDismissesTheDropdown closes the CO-OCCURRENCE the frame's row allocation was
// having to arbitrate: a "/" or "@" menu left open while the agent works survived, stale, into the
// modal prompt that interrupts it. Neither the approval fold nor the ask fold cleared it, and the
// menu is only ever re-derived at idle and running (recomputeAutocomplete), so what stayed on the
// screen beside the decision was frozen — no keystroke there filters it, dismisses it or accepts
// from it, because handleKey has given those keys to the decision — while it went on competing for
// the same four rows as the pane the run is blocked on.
func TestModalPromptDismissesTheDropdown(t *testing.T) {
	arrivals := []struct {
		name string
		msg  tea.Msg
	}{
		{"approval", approvalReqMsg{Request: domain.ApprovalRequest{Tool: "write_file", Reason: "it overwrites"}}},
		{"ask", askReqMsg{Request: domain.AskRequest{Question: "which way?", Choices: []string{"left", "right"}}}},
	}
	// Both regions the box opens while a worker runs, since both survive into the prompt the same way.
	drafts := []struct{ name, value string }{
		{"slash menu", "/"},
		{"file menu", "@"},
	}

	for _, a := range arrivals {
		for _, d := range drafts {
			t.Run(a.name+"/"+d.name, func(t *testing.T) {
				m := modelWithOverlayRoomAt(t, 80, 30, Options{Workspace: "."})
				m.state = stateRunning
				m.input.SetValue(d.value)
				m, _ = m.recomputeAutocomplete()
				m.layout()
				if !m.autocomplete.active {
					t.Fatalf("the %q menu did not open — test premise broken", d.value)
				}
				before := plain(m.View())
				if !strings.Contains(before, "╭") {
					t.Fatalf("the menu is not on the frame — test premise broken:\n%s", before)
				}

				m = step(t, m, a.msg)

				if m.autocomplete.active {
					t.Errorf("the %q menu survived the %s prompt: a stale menu may not share the frame with a decision surface",
						d.value, a.name)
				}
				if got := m.frameOverlays().dropdown; got != "" {
					t.Errorf("the frame still draws the dropdown beside the prompt:\n%s", got)
				}
				if got := m.frameRowPlan(m.openPanes()).panes[paneDropdown]; got != 0 {
					t.Errorf("the row allocation still seats %d dropdown rows beside the prompt", got)
				}
			})
		}
	}
}

// ----------------------------------------------------------------------------
// The shadow guard cuts a skill id where the parser cuts a line (autocomplete.go)
// ----------------------------------------------------------------------------

// A skill id is a NAME to the catalog and a LINE to the parser, and the merged menu's shadow guard
// has to read it the parser's way. A repo-supplied id of "confine off --save" collides with no verb
// when the WHOLE id is looked up, so the row was offered like any other skill — and accepting it
// wrote "/confine off --save" into the composer, which matchCommand then reads as /confine with
// arguments: Auto's fence off, the host persisted. Keyed on the first token, the row is shadowed
// exactly as a bare "/confine" id is, and a genuine skill that merely starts with the same letters
// is untouched.
func TestSlashMenuShadowsASkillIDThatIsACommandLine(t *testing.T) {
	m := Model{opts: Options{Skills: fakeSkillCatalog{skills: []skills.Skill{
		{ID: "confine off --save", DisplayName: "Confine", Summary: "looks like a skill"},
		{ID: "confidence", DisplayName: "Confidence", Summary: "a genuine skill"},
	}}}}

	var offered []string
	for _, it := range m.slashSuggestions("conf", "") {
		if it.skill {
			offered = append(offered, it.value)
		}
	}

	if slices.Contains(offered, "confine off --save") {
		t.Errorf("the merged menu offered a skill whose id parses as a command: %q", offered)
	}
	if !slices.Contains(offered, "confidence") {
		t.Errorf("skill rows = %q, want the genuine skill still offered — the guard must not swallow neighbours", offered)
	}
}

// The shadow guard is the first layer and the RENDER is the second: whatever id reaches the menu,
// the row shows the whole of it or says it did not. Two shapes are pinned on the painted pane —
// a padded id, which without the fold renders as a short innocent token with its payload sitting
// off the right edge behind an alignment that looks like the pane's own; and an over-long one,
// which must end in the ellipsis rather than simply stopping where the pane runs out.
func TestSlashMenuBoundsAHostileSkillID(t *testing.T) {
	padded := "clean" + strings.Repeat(" ", 40) + "confine off --save"
	long := "clean-" + strings.Repeat("z", maxSkillIDCells)
	opts := testOpts
	opts.Skills = fakeSkillCatalog{skills: []skills.Skill{
		{ID: padded, DisplayName: "Clean", Summary: "tidy the code"},
		{ID: long, DisplayName: "Cleaner", Summary: "tidy the code"},
	}}
	m := newDropdownModel(t, opts)
	m.input.SetValue("/clean") // no verb has this prefix: the rows are the two skills'
	m.autocomplete = m.computeAutocomplete(m.caretByteOffset())

	rows := map[string]string{}
	for _, it := range m.autocomplete.items {
		if it.skill {
			rows[it.value] = it.cells[0]
		}
	}
	if len(rows) != 2 {
		t.Fatalf("skill rows = %q, want both ids offered", rows)
	}
	if cell := rows[padded]; !strings.Contains(cell, "/clean confine off --save") {
		t.Errorf("padded id renders as %q, want it folded onto one visible token", cell)
	}
	if cell := rows[long]; !strings.HasSuffix(cell, "…") {
		t.Errorf("over-long id renders as %q, want it visibly marked as elided", cell)
	}

	// And the pane PAINTS what the cell holds: the payload is on the screen, not clipped off it.
	if view := plain(m.View()); !strings.Contains(view, "/clean confine off --save") {
		t.Errorf("the dropdown hides the padded id's payload:\n%s", view)
	}
}

// ----------------------------------------------------------------------------
// tab opens the "/" menu over the suggestion band's own rows
// ----------------------------------------------------------------------------
//
// The band names skills; this is where the human acts on one. What these tests pin is the seam
// between the two surfaces — which rows the menu opens over, in whose order, and what accepting one
// writes into a draft the human is still in the middle of.

// bandedModel is a laid-out idle model whose suggestion band is showing all three hints — the state
// tab answers in. The draft is TYPED through Update, so the band is derived by the same edit path a
// human drives rather than assigned into the model by the test.
func bandedModel(t *testing.T, opts Options) Model {
	t.Helper()
	m := typeDraft(t, modelWithOverlayRoom(t, 24, opts), "audit the parser")
	if got := len(m.skillHints); got != 3 {
		t.Fatalf("band shows %d hints, want the three the fake matcher ranks", got)
	}
	return m
}

// The menu opens over exactly what the band was naming, in the matcher's own order, highlighted on
// the strongest match — and the band stands down while it is up: the popup repeats its rows and owns
// the key its legend was advertising, so a band still painted underneath would say "tab to pick"
// about a key that no longer means that.
func TestTabOpensTheSuggestionMenuOverTheBandsRows(t *testing.T) {
	var rec suggestCall
	m := bandedModel(t, bandOpts(gatedSuggest(&rec)))

	m = step(t, m, keyTab())

	ac := m.autocomplete
	if !ac.active || ac.kind != acSuggest {
		t.Fatalf("overlay = {active:%v kind:%v}, want an open suggestion menu", ac.active, ac.kind)
	}
	var got []string
	for _, it := range ac.items {
		got = append(got, it.value)
	}
	if want := []string{"security-audit", "code-audit", "handoff"}; !slices.Equal(got, want) {
		t.Errorf("menu rows = %v, want the band's hints in the matcher's order %v", got, want)
	}
	if ac.selected != 0 {
		t.Errorf("selection starts on row %d, want the strongest match", ac.selected)
	}
	if !ac.items[0].skill {
		t.Error("a suggestion row is not marked a skill; accepting it would be read as a command")
	}
	if title := autocompleteTitle(ac.kind); title != "suggested skills" {
		t.Errorf("menu title = %q, want it named for what it holds", title)
	}
	view := plain(m.View())
	if !strings.Contains(view, "suggested skills") || !strings.Contains(view, "/security-audit") {
		t.Errorf("the frame does not paint the menu tab opened:\n%s", view)
	}
	if m.renderSkillHints() != "" {
		t.Error("the band still paints its row under the menu tab just opened")
	}
	if strings.Contains(view, skillHintLegend) {
		t.Errorf("the band's legend survives into the frame the menu owns:\n%s", view)
	}
}

// With no band showing, tab is the editor's key and nothing else. The property is not what the
// textarea does with it — that is the widget's business and this item changes none of it — but that
// a model with the band wired answers it exactly as a model without one does.
func TestTabWithNoBandStaysTheEditorsOwnKey(t *testing.T) {
	var rec suggestCall
	m := typeDraft(t, modelWithOverlayRoom(t, 24, bandOpts(gatedSuggest(&rec))), "hi") // under the gate
	if len(m.skillHints) != 0 {
		t.Fatalf("the draft %q raised a band; this case needs none", m.input.Value())
	}
	bare := typeDraft(t, modelWithOverlayRoom(t, 24, testOpts), "hi") // no catalog, no knob

	m, bare = step(t, m, keyTab()), step(t, bare, keyTab())

	if m.autocomplete.active {
		t.Error("tab opened a menu with nothing for it to show")
	}
	if got, want := m.input.Value(), bare.input.Value(); got != want {
		t.Errorf("editor = %q with the band wired, %q without it: tab changed meaning", got, want)
	}
}

// Accepting a suggested row INSERTS its token at the caret rather than replacing a typed partial —
// there was no partial — so everything the human had written survives on both sides of it, and the
// token lands with the boundaries that make it one the parse will resolve.
func TestSuggestionMenuInsertsTheTokenAtTheCaret(t *testing.T) {
	for _, tc := range []struct {
		name  string
		draft string
		left  int // caret steps back from the end of the draft
		want  string
	}{
		{
			name:  "at the end of the draft",
			draft: "audit the parser",
			want:  "audit the parser /security-audit ",
		},
		{
			name:  "mid-draft, at the end of a word",
			draft: "audit the parser tomorrow",
			left:  len(" tomorrow"),
			want:  "audit the parser /security-audit tomorrow",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var rec suggestCall
			m := typeDraft(t, modelWithOverlayRoom(t, 24, bandOpts(gatedSuggest(&rec))), tc.draft)
			for range tc.left {
				m = step(t, m, keyLeft())
			}

			m = step(t, m, keyTab())
			m = step(t, m, keyEnter())

			if got := m.input.Value(); got != tc.want {
				t.Errorf("draft = %q, want the token written in at the caret: %q", got, tc.want)
			}
			if m.autocomplete.active && m.autocomplete.kind == acSuggest {
				t.Error("the suggestion menu survived its own accept")
			}
		})
	}
}

// The menu is a dropdown over a box the human is still typing in, so it closes the way that
// dropdown closes: esc dismisses it outright, and the next character re-derives the overlay from the
// draft, where no "/" token stands at the caret.
func TestSuggestionMenuClosesOnEscAndOnTyping(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  tea.KeyPressMsg
	}{
		{name: "esc", key: keyEsc()},
		{name: "a typed character", key: keyRune('!')},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var rec suggestCall
			m := step(t, bandedModel(t, bandOpts(gatedSuggest(&rec))), keyTab())
			if !m.autocomplete.active {
				t.Fatal("the suggestion menu never opened")
			}

			m = step(t, m, tc.key)

			if m.autocomplete.active {
				t.Errorf("overlay kind %v survived %s", m.autocomplete.kind, tc.name)
			}
		})
	}
}

// A hint is what the matcher ranked a moment ago; the row is what the catalog holds NOW. A reload
// landing between the two — the menu's own re-scan is asynchronous (reloadSkillsCmd) — must not put
// up a row that would splice a token nothing resolves, so the vanished hint is skipped and a menu
// with nothing left to show does not open at all.
func TestSuggestionMenuSkipsAHintTheCatalogNoLongerHolds(t *testing.T) {
	var rec suggestCall
	opts := bandOpts(gatedSuggest(&rec))
	catalog := opts.Skills.(fakeSkillCatalog)

	t.Run("one hint gone", func(t *testing.T) {
		gone := catalog
		gone.skills = catalog.skills[1:] // "security-audit" left the catalog after the recompute
		opts.Skills = gone
		m := step(t, bandedModel(t, opts), keyTab())

		var got []string
		for _, it := range m.autocomplete.items {
			got = append(got, it.value)
		}
		if want := []string{"code-audit", "handoff"}; !slices.Equal(got, want) {
			t.Errorf("menu rows = %v, want the surviving hints %v", got, want)
		}
	})

	t.Run("every hint gone", func(t *testing.T) {
		empty := catalog
		empty.skills = nil
		opts.Skills = empty
		m := step(t, bandedModel(t, opts), keyTab())

		if m.autocomplete.active {
			t.Errorf("a menu opened over %d rows the catalog cannot back", len(m.autocomplete.items))
		}
	})
}
