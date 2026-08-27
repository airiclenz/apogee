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

// TestSkillHintsStandDownOffTheLiveStates is the band's fifth silence, and the only one the
// recompute cannot speak for: hints are derived on the EDIT path, so a run that leaves idle for an
// approval, an ask or an error never passes through it and m.skillHints keeps whatever the last
// keystroke ranked. The row must not paint there anyway — it would be advice about a draft nobody
// is composing, over a surface that has taken the very key its legend names — and the frame must
// not reserve it either, or the staged band loses its closing row to a row nobody draws.
func TestSkillHintsStandDownOffTheLiveStates(t *testing.T) {
	for _, state := range []uiState{stateAwaitingApproval, stateAwaitingAsk, stateErrored} {
		var rec suggestCall
		m := modelWithOverlayRoom(t, 24, bandOpts(gatedSuggest(&rec)))
		m = typeDraft(t, m, "audit the parser")
		if m.renderSkillHints() == "" {
			t.Fatal("no band at idle to be stood down")
		}

		m.state = state

		if len(m.skillHints) == 0 {
			t.Fatalf("state %d cleared the hints by itself; the stale row is no longer the case under test", state)
		}
		if row := m.renderSkillHints(); row != "" {
			t.Errorf("the band paints a stale row at state %d:\n%q", state, ansi.Strip(row))
		}
		if m.frameRowPlan(m.openPanes()).band.hint {
			t.Errorf("the frame reserves a hint row at state %d that nobody paints", state)
		}
	}
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

// ----------------------------------------------------------------------------
// Spent at send — the band's session dedup
// ----------------------------------------------------------------------------
//
// The dedup rule has one boundary and it is the SEND: while the draft is being written the row may
// change as often as the sentence does, and none of it costs the human anything; the moment a
// message goes out, every skill the row was naming has had its chance and is never suggested again
// this session. What follows pins both halves of that — what spends, and what does not.

// TestSuggestedSkillIsSpentOnSend is the rule itself: the band names three skills, the human sends
// the message under them, and a later draft that would rank exactly the same way gets no row. The
// exclusion is asserted at the SEAM as well as on the model — the exclude probe the band hands the
// matcher must answer for the spent ids — because that closure is what a real matcher would consult,
// and a spent set nothing reads would dedup nothing.
func TestSuggestedSkillIsSpentOnSend(t *testing.T) {
	var rec suggestCall
	m := modelWithOverlayRoom(t, 24, bandOpts(gatedSuggest(&rec)))

	m = typeDraft(t, m, "audit the parser")
	if got := len(m.skillHints); got != 3 {
		t.Fatalf("precondition: band shows %d hints, want 3", got)
	}

	m = step(t, m, keyEnter()) // the message goes out with the row standing above it

	if m.state != stateRunning {
		t.Fatalf("precondition: state = %v after ⏎, want running (the message did not send)", m.state)
	}
	if got := len(m.skillHints); got != 0 {
		t.Errorf("the band still holds %d hints after the send", got)
	}

	m = typeDraft(t, m, "audit the parser once more")

	for _, s := range bandSuggestions {
		if rec.exclude == nil {
			t.Fatal("the band ranked without an exclude probe")
		}
		if !rec.exclude(s.ID) {
			t.Errorf("the exclude probe admits %q, spent on the previous send", s.ID)
		}
	}
	if got := len(m.skillHints); got != 0 {
		t.Errorf("the band suggests %d already-spent skills on the next draft", got)
	}
	if m.renderSkillHints() != "" {
		t.Error("the band paints a row of spent skills")
	}
}

// TestSuggestionsSurviveASendWithNoBand is the other side of "spent at send": what is spent is what was
// SHOWN, never the catalog. A draft under the matcher's evidence gate has no row, so sending it
// retires nothing — and the very same skills must still be offered on the next draft that earns
// them. Without this the first two-word message of a session would silently spend the top matches
// for a draft the human never saw a band for.
func TestSuggestionsSurviveASendWithNoBand(t *testing.T) {
	var rec suggestCall
	m := modelWithOverlayRoom(t, 24, bandOpts(gatedSuggest(&rec)))

	m = typeDraft(t, m, "go on") // two words: under the gate, so no row
	if got := len(m.skillHints); got != 0 {
		t.Fatalf("precondition: band shows %d hints on a draft under the gate", got)
	}

	m = step(t, m, keyEnter())

	if got := len(m.spentSkills); got != 0 {
		t.Errorf("a send with an empty band spent %d skills: %v", got, m.spentSkills)
	}

	m = typeDraft(t, m, "audit the parser")

	if got := len(m.skillHints); got != 3 {
		t.Errorf("band shows %d hints after a send that spent nothing, want 3", got)
	}
}

// TestStagedInterjectionSpendsTheBand: ⏎ while a worker runs stages the line rather than launching
// it, but from the human's side it is the same act — the message has left their hands and the worker
// delivers it — so the row above it is spent exactly as a plain send spends it.
func TestStagedInterjectionSpendsTheBand(t *testing.T) {
	var rec suggestCall
	m := modelWithOverlayRoom(t, 24, bandOpts(gatedSuggest(&rec)))

	m = step(t, typeDraft(t, m, "go on"), keyEnter()) // open the Exchange with nothing on the row
	if m.state != stateRunning || m.box == nil {
		t.Fatalf("precondition: state = %v, mailbox %v — want a running Exchange", m.state, m.box != nil)
	}

	m = typeDraft(t, m, "audit the parser")
	if got := len(m.skillHints); got != 3 {
		t.Fatalf("precondition: band shows %d hints while the worker runs, want 3", got)
	}

	m = step(t, m, keyEnter()) // stages the row

	if got := len(m.pendingInterjections); got != 1 {
		t.Fatalf("precondition: %d rows staged, want 1", got)
	}
	if got := len(m.spentSkills); got != 3 {
		t.Errorf("a staged interjection spent %d skills, want 3", got)
	}

	m = typeDraft(t, m, "audit the parser once more")

	if got := len(m.skillHints); got != 0 {
		t.Errorf("the band suggests %d skills already spent by a staged row", got)
	}
}

// TestClearResetsTheSpentSkills pins the boundary the set lives inside. /clear (and /new) open a new
// conversation, and a new conversation has heard none of the old one's advice — so the skills spent
// before it are offered again afterwards. It is the same boundary the transcript resets on, which is
// what makes the rule "once per session" rather than "once per catalog scan".
func TestClearResetsTheSpentSkills(t *testing.T) {
	var rec suggestCall
	m := modelWithOverlayRoom(t, 24, bandOpts(gatedSuggest(&rec)))

	m = step(t, typeDraft(t, m, "audit the parser"), keyEnter())
	if got := len(m.spentSkills); got != 3 {
		t.Fatalf("precondition: the send spent %d skills, want 3", got)
	}
	m = step(t, m, cancelledMsg{}) // back to idle, where an idle-only /command can run
	if m.state != stateIdle {
		t.Fatalf("precondition: state = %v after the cancel, want idle", m.state)
	}

	// Set the line rather than type it: a typed "/clear" opens the "/" menu, whose own ⏎ accepts a
	// row instead of submitting the line, and this test is about the command not the dropdown.
	m.input.SetValue("/clear")
	m = step(t, m, keyEnter())

	if m.spentSkills != nil {
		t.Errorf("/clear left %d spent skills behind: %v", len(m.spentSkills), m.spentSkills)
	}

	m = typeDraft(t, m, "audit the parser")

	if got := len(m.skillHints); got != 3 {
		t.Errorf("band shows %d hints in the fresh session, want the 3 /clear made available again", got)
	}
}

// TestSuggestionsSurviveARefusedLine: only a SEND spends the row. A lone "/word" that names neither a
// command nor a skill is refused with the typo guard's note and the line is left standing in the box
// — nothing went out — so the advice above it must still be there to act on afterwards.
func TestSuggestionsSurviveARefusedLine(t *testing.T) {
	var rec suggestCall
	m := modelWithOverlayRoom(t, 24, bandOpts(gatedSuggest(&rec)))

	m = typeDraft(t, m, "audit the parser")
	if got := len(m.skillHints); got != 3 {
		t.Fatalf("precondition: band shows %d hints, want 3", got)
	}
	// Set the mistyped verb rather than type it, so the row the refusal must not spend is still
	// standing when ⏎ lands (a typed "/" would open the menu, which stands the band down by itself).
	m.input.SetValue("/comapct")

	m = step(t, m, keyEnter())

	if got := len(m.spentSkills); got != 0 {
		t.Errorf("a refused /word spent %d skills: %v", got, m.spentSkills)
	}
	if got := len(m.skillHints); got != 3 {
		t.Errorf("the refusal cleared the band: %d hints left, want 3", got)
	}
}
