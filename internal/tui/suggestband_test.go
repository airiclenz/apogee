package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/airiclenz/apogee/internal/skills"
)

// ----------------------------------------------------------------------------
// The skill-suggestion band (ADR 0061)
// ----------------------------------------------------------------------------
//
// The band under test is the PRESENTATION of a ranking, never the ranking itself: the matcher is
// the engine's (internal/skills, suggest_test.go proves the ordering, the trigger boost and the
// evidence gate). What these tests pin is the seam and the row — when the Driver asks for a
// ranking, what it excludes when it asks, and what the one row it paints looks like.

// bandSuggestions are the canned rows the fake catalog hands back — the shape of a real answer,
// three skills strongest first.
var bandSuggestions = []skills.Suggestion{
	{ID: "security-audit", DisplayName: "Security Audit", Score: 9},
	{ID: "code-audit", DisplayName: "Code Audit", Score: 6},
	{ID: "handoff", DisplayName: "Handoff", Score: 3},
}

// suggestCall records one Suggest the band made: the draft it handed the matcher and the exclude
// probe it built, so a test can ask the closure what it answers for an id instead of reaching into
// the model's own sets.
type suggestCall struct {
	draft   string
	exclude func(string) bool
	limit   int
	calls   int
}

// bandOpts is testOpts with the knob on and a catalog whose Suggest is the caller's hook.
func bandOpts(hook func(draft string, exclude func(string) bool, limit int) []skills.Suggestion) Options {
	o := testOpts
	o.SkillSuggestions = true
	o.Skills = fakeSkillCatalog{
		skills: []skills.Skill{
			{ID: "security-audit", DisplayName: "Security Audit"},
			{ID: "code-audit", DisplayName: "Code Audit"},
			{ID: "handoff", DisplayName: "Handoff"},
		},
		suggest: hook,
	}
	return o
}

// gatedSuggest models the matcher's own evidence gate at the seam — under three content words it
// returns nothing at all rather than a weak guess (skills.Suggest, minContentWords) — and records
// every call. The band must show what a ranking says and go quiet when it says nothing; which
// drafts clear the gate is the engine's question, answered in internal/skills.
func gatedSuggest(rec *suggestCall) func(string, func(string) bool, int) []skills.Suggestion {
	return func(draft string, exclude func(string) bool, limit int) []skills.Suggestion {
		rec.draft, rec.exclude, rec.limit = draft, exclude, limit
		rec.calls++
		if len(strings.Fields(draft)) < 3 {
			return nil
		}
		var out []skills.Suggestion
		for _, s := range bandSuggestions {
			if exclude != nil && exclude(s.ID) {
				continue
			}
			out = append(out, s)
			if len(out) == limit {
				break
			}
		}
		return out
	}
}

// typeDraft presses one printable key per rune, the way a human types into the box — through
// Update, so the edit path that re-derives the band is the real one.
func typeDraft(t *testing.T, m Model, text string) Model {
	t.Helper()
	for _, r := range text {
		m = step(t, m, keyRune(r))
	}
	return m
}

// TestSkillHintsTrackTheDraft is the band's whole lifecycle in one property: it appears when the
// draft says enough for the matcher to answer, and it goes away again when the draft no longer
// does. Both halves run through the EDIT path — recomputeAutocomplete folds the recompute in, so a
// band that tracked only the first keystroke or only a full submit would fail here rather than in a
// human's terminal.
func TestSkillHintsTrackTheDraft(t *testing.T) {
	var rec suggestCall
	m := modelWithOverlayRoom(t, 24, bandOpts(gatedSuggest(&rec)))

	m = typeDraft(t, m, "audit the parser")

	if got := len(m.skillHints); got != 3 {
		t.Fatalf("band shows %d hints after an edit, want 3 (draft %q)", got, rec.draft)
	}
	if m.renderSkillHints() == "" {
		t.Error("the band paints nothing while it holds hints")
	}

	// Backspace the draft under the gate: the matcher answers nothing, so the band must say nothing
	// rather than keep the row it last had.
	for range len("the parser") {
		m = step(t, m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	}

	if got := len(m.skillHints); got != 0 {
		t.Fatalf("band still shows %d hints on a draft under the gate (draft %q)", got, rec.draft)
	}
	if m.renderSkillHints() != "" {
		t.Error("the band paints a row with no hints behind it")
	}
}

// TestSkillHintsExcludeInvokedSkills is the band's half of "do not advise what is already done". A
// skill the draft already invokes with its "/token" is excluded before the ranking, and the token
// itself is cut out of the text the matcher ranks — otherwise "/code-audit" would match the
// code-audit skill on its own name and pin it to the top of a band it has already been invoked from.
func TestSkillHintsExcludeInvokedSkills(t *testing.T) {
	var rec suggestCall
	m := modelWithOverlayRoom(t, 24, bandOpts(gatedSuggest(&rec)))

	m = typeDraft(t, m, "/code-audit please look at the parser")

	if rec.exclude == nil {
		t.Fatal("the band ranked without an exclude probe")
	}
	if !rec.exclude("code-audit") {
		t.Error("the exclude probe admits a skill the draft already invokes")
	}
	if strings.Contains(rec.draft, "code-audit") {
		t.Errorf("the ranked draft still holds the invoked token: %q", rec.draft)
	}
	for _, h := range m.skillHints {
		if h.ID == "code-audit" {
			t.Error("the band suggests the skill the draft already invokes")
		}
	}
}

// TestSkillHintsRespectTheKnobAndTheOverlay pins the two silences that are the Driver's own rather
// than the matcher's: the human switched the band off (`ui.skill-suggestions`), and a "/" or "@"
// menu is open — the menu below the band already answers the same question, and two answers to one
// question is noise. Neither may reach the matcher at all: a band that ranked and then hid would
// pay for a walk nobody sees.
func TestSkillHintsRespectTheKnobAndTheOverlay(t *testing.T) {
	t.Run("knob off", func(t *testing.T) {
		var rec suggestCall
		opts := bandOpts(gatedSuggest(&rec))
		opts.SkillSuggestions = false
		m := typeDraft(t, modelWithOverlayRoom(t, 24, opts), "audit the parser")

		if rec.calls != 0 {
			t.Errorf("the matcher was asked %d times with the band switched off", rec.calls)
		}
		if len(m.skillHints) != 0 || m.renderSkillHints() != "" {
			t.Error("the band paints with ui.skill-suggestions off")
		}
	})

	t.Run("knob switched off mid-draft", func(t *testing.T) {
		var rec suggestCall
		m := modelWithOverlayRoom(t, 24, bandOpts(gatedSuggest(&rec)))
		m = typeDraft(t, m, "audit the parser")
		if m.renderSkillHints() == "" {
			t.Fatal("no band to switch off")
		}

		m, _, _, handled, err := m.settingsApplyLocal(settingKeySkillSuggestions, "false")
		if err != nil || !handled {
			t.Fatalf("settingsApplyLocal(%q, false) = handled %v, err %v", settingKeySkillSuggestions, handled, err)
		}

		if m.renderSkillHints() != "" {
			t.Error("the band survives ui.skill-suggestions being switched off (ADR 0037)")
		}
		if m.frameRowPlan(m.openPanes()).band.hint {
			t.Error("the frame still reserves a row for a band nobody paints")
		}
	})

	t.Run("menu open", func(t *testing.T) {
		var rec suggestCall
		m := modelWithOverlayRoom(t, 24, bandOpts(gatedSuggest(&rec)))
		m = typeDraft(t, m, "audit the parser")
		if len(m.skillHints) == 0 {
			t.Fatal("no hints to be suppressed")
		}

		m = typeDraft(t, m, " /co") // the merged "/" menu opens over the token at the caret

		if !m.autocomplete.active {
			t.Fatal("the /-menu did not open; nothing is suppressing the band")
		}
		if len(m.skillHints) != 0 || m.renderSkillHints() != "" {
			t.Error("the band paints under an open / menu")
		}
	})
}

// TestSkillHintRowIsPaintedLikeTheBand pins the row as CHROME: it names the suggested ids as the
// "/id" tokens that invoke them, carries the legend for the key that opens the menu on them, and is
// clipped and padded to the window exactly as a staged row is — so the black field runs edge to
// edge and the terminal's own background never shows through past the text.
func TestSkillHintRowIsPaintedLikeTheBand(t *testing.T) {
	for _, width := range []int{80, 34, 12} {
		var rec suggestCall
		m := modelWithOverlayRoomAt(t, width, 24, bandOpts(gatedSuggest(&rec)))
		m = typeDraft(t, m, "audit the parser")
		if len(m.skillHints) == 0 {
			t.Fatalf("no hints at %d columns", width)
		}

		row := m.skillHintRow()

		if got := ansi.StringWidth(row); got != width {
			t.Errorf("the hint row is %d columns wide on a %d-column window:\n%q", got, width, ansi.Strip(row))
		}
		if strings.Contains(ansi.Strip(row), "\n") {
			t.Errorf("the hint row is more than one line:\n%q", ansi.Strip(row))
		}
		if width == 80 {
			want := "  " + glyphSkill + " skills: /security-audit · /code-audit · /handoff   tab to pick"
			if got := strings.TrimRight(ansi.Strip(row), " "); got != want {
				t.Errorf("hint row = %q, want %q", got, want)
			}
		}
	}
}

// TestSkillHintRowStripsEscapesFromTheCatalog is the seam invariant at the one place a skill id
// becomes screen. The id is a repo-supplied SKILL.md's word: an ESC byte in it would reach the
// terminal live AND lie to the column arithmetic that pads the band's field, so it is stripped and
// flattened where the row is built — the row must still measure exactly one window's width.
func TestSkillHintRowStripsEscapesFromTheCatalog(t *testing.T) {
	m := modelWithOverlayRoom(t, 24, bandOpts(func(string, func(string) bool, int) []skills.Suggestion {
		return []skills.Suggestion{{ID: "code\x1b[31m-audit\nHIJACK", DisplayName: "Code Audit"}}
	}))
	m = typeDraft(t, m, "audit the parser")

	row := m.skillHintRow()

	if strings.Contains(row, "\x1b[31m") {
		t.Errorf("the catalog's own escape sequence reached the row live: %q", row)
	}
	if strings.Contains(ansi.Strip(row), "\n") {
		t.Errorf("a newline from the catalog opened a second band row: %q", ansi.Strip(row))
	}
	if got := ansi.StringWidth(row); got != m.width {
		t.Errorf("the hint row is %d columns wide, want %d", got, m.width)
	}
}

// TestBandNeverOverflowsTheFrame is D2 for the new surface: whatever the window, the composed frame
// is exactly as many rows as the terminal has. The band is one more thing taking rows above the
// input box, so it is swept beside the staged strip it shares a plan with — and beside a dropdown,
// the combination the frame-wide allocation exists for.
func TestBandNeverOverflowsTheFrame(t *testing.T) {
	for _, staged := range []int{0, 2, 5} {
		for height := 8; height <= 26; height++ {
			var rec suggestCall
			m := withStagedRows(modelWithOverlayRoomAt(t, 80, height, bandOpts(gatedSuggest(&rec))), staged)
			m = typeDraft(t, m, "audit the parser")

			if got := lipgloss.Height(m.View().Content); got != height {
				t.Errorf("frame is %d rows on a %d-row terminal with %d staged", got, height, staged)
			}
		}
	}
}
