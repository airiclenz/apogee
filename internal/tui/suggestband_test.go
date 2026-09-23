package tui

import (
	"context"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/airiclenz/apogee/internal/domain"
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
	o.UI.SkillSuggestions = true
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
// Update, so the edit path that schedules the band is the real one — and then lets the typing
// pause: it delivers the debounce tick at the model's current generation (settleBand), so every
// caller sees the band the draft ranks to.
func typeDraft(t *testing.T, m Model, text string) Model {
	t.Helper()
	for _, r := range text {
		m = step(t, m, keyRune(r))
	}
	return settleBand(t, m)
}

// settleBand is the pause after a burst of edits: the debounce tick the last edit armed, delivered
// through Update at the generation that edit opened.
func settleBand(t *testing.T, m Model) Model {
	t.Helper()
	return step(t, m, skillHintTickMsg{gen: m.skillHintGen})
}

// TestSkillHintsTrackTheDraft is the band's whole lifecycle in one property: it appears when the
// draft says enough for the matcher to answer, and it goes away again when the draft no longer
// does. Both halves run through the EDIT path — recomputeAutocomplete arms the debounce tick and
// settleBand lands it, so a band that tracked only the first pause or only a full submit would fail
// here rather than in a human's terminal.
func TestSkillHintsTrackTheDraft(t *testing.T) {
	t.Parallel()

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
	m = settleBand(t, m)

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
	t.Parallel()

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
	t.Parallel()

	t.Run("knob off", func(t *testing.T) {
		t.Parallel()

		var rec suggestCall
		opts := bandOpts(gatedSuggest(&rec))
		opts.UI.SkillSuggestions = false
		m := typeDraft(t, modelWithOverlayRoom(t, 24, opts), "audit the parser")

		if rec.calls != 0 {
			t.Errorf("the matcher was asked %d times with the band switched off", rec.calls)
		}
		if len(m.skillHints) != 0 || m.renderSkillHints() != "" {
			t.Error("the band paints with ui.skill-suggestions off")
		}
	})

	t.Run("knob switched off mid-draft", func(t *testing.T) {
		t.Parallel()

		var rec suggestCall
		m := modelWithOverlayRoom(t, 24, bandOpts(gatedSuggest(&rec)))
		m = typeDraft(t, m, "audit the parser")
		if m.renderSkillHints() == "" {
			t.Fatal("no band to switch off")
		}

		m, _, _, handled, err := m.settingsApplyLocal(domain.UIKeySkillSuggestions, "false")
		if err != nil || !handled {
			t.Fatalf("settingsApplyLocal(%q, false) = handled %v, err %v", domain.UIKeySkillSuggestions, handled, err)
		}

		if m.renderSkillHints() != "" {
			t.Error("the band survives ui.skill-suggestions being switched off (ADR 0037)")
		}
		if m.frameRowPlan(m.openPanes()).band.hint {
			t.Error("the frame still reserves a row for a band nobody paints")
		}
	})

	t.Run("menu open", func(t *testing.T) {
		t.Parallel()

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
// pause ranked. The row must not paint there anyway — it would be advice about a draft nobody is
// composing, over a surface that has taken the very key its legend names — and the frame must not
// reserve it either, or the staged band loses its closing row to a row nobody draws.
func TestSkillHintsStandDownOffTheLiveStates(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

	var rec suggestCall
	m := modelWithOverlayRoom(t, 24, bandOpts(gatedSuggest(&rec)))

	m = step(t, typeDraft(t, m, "go on"), keyEnter()) // open the Exchange with nothing on the row
	if m.state != stateRunning || m.worker.box == nil {
		t.Fatalf("precondition: state = %v, mailbox %v — want a running Exchange", m.state, m.worker.box != nil)
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
	t.Parallel()

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

// TestRestoreResetsTheSpentSkills is that same boundary rule over the OTHER way a conversation is
// replaced: an in-TUI /sessions restore drops the live session for a stored one, so the advice
// spent against the session that just went away must be on offer again in the one reopened. The
// set is never written to a record, so a restore that kept it could only ever inherit the outgoing
// session's spend — which is what this pins shut.
func TestRestoreResetsTheSpentSkills(t *testing.T) {
	t.Parallel()

	var rec suggestCall
	m := modelWithOverlayRoom(t, 24, bandOpts(gatedSuggest(&rec)))

	m = step(t, typeDraft(t, m, "audit the parser"), keyEnter())
	if got := len(m.spentSkills); got != 3 {
		t.Fatalf("precondition: the send spent %d skills, want 3", got)
	}
	m = step(t, m, cancelledMsg{}) // back to idle, the state the browser's resume runs from
	if m.state != stateIdle {
		t.Fatalf("precondition: state = %v after the cancel, want idle", m.state)
	}

	m = restoreSession(t, m) // the /sessions resume, folded from the loaded record (sessionLoadedMsg)

	if m.spentSkills != nil {
		t.Errorf("a restore left %d spent skills behind: %v", len(m.spentSkills), m.spentSkills)
	}

	m = typeDraft(t, m, "audit the parser")

	if got := len(m.skillHints); got != 3 {
		t.Errorf("band shows %d hints in the restored session, want the 3 the restore made available again", got)
	}
}

// TestSuggestionsSurviveARefusedLine: only a SEND spends the row. A lone "/word" that names neither a
// command nor a skill is refused with the typo guard's note and the line is left standing in the box
// — nothing went out — so the advice above it must still be there to act on afterwards.
func TestSuggestionsSurviveARefusedLine(t *testing.T) {
	t.Parallel()

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

// TestSuggestBandPrecision runs the band over the REAL matcher and the real library fixture
// (internal/skills/testdata/library) instead of the fake catalog the other tests use: what it pins
// is that the matcher's precision — the relative cutoff and the dev-generic stopwords (ADR 0061,
// amended 2026-09-14) — reaches the row a human sees through the seam unchanged. A band that showed
// three rows for a generic edit would fail here even though every row of the fake-catalog tests
// still passed, because the fake hands back three rows for anything past the gate. The rows the
// matcher returns are the engine's question (suggest_library_test.go); this table holds one row of
// each shape the band can end up with: a clear winner, two genuine runners-up, and nothing at all.
func TestSuggestBandPrecision(t *testing.T) {
	t.Parallel()

	catalog, err := skills.Load(skills.Sources{Home: "../skills/testdata/library"})
	if err != nil {
		t.Fatalf("Load(../skills/testdata/library): %v", err)
	}
	if catalog.Len() < 20 {
		t.Fatalf("fixture catalog holds %d skills, want at least 20 — is internal/skills/testdata/library intact?", catalog.Len())
	}
	opts := testOpts
	opts.UI.SkillSuggestions = true
	opts.Skills = catalog

	cases := []struct {
		name  string
		draft string
		first string   // the id the row must name first; empty when the band must stay dark
		wants []string // ids the row must name somewhere
	}{
		{name: "a clear winner names one skill first", draft: "cut a release for homebrew", first: "brew-release"},
		{name: "two audit skills are both named", draft: "audit the parser for security holes", wants: []string{"code-audit", "security-audit"}},
		{name: "a generic struct edit keeps the band dark", draft: "add a field to the config struct"},
		{name: "a generic file move keeps the band dark", draft: "move these files into a new package"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := modelWithOverlayRoom(t, 24, opts)

			m = typeDraft(t, m, tc.draft)

			var ids []string
			for _, s := range m.skillHints {
				ids = append(ids, s.ID)
			}
			t.Logf("band for %q = %v", tc.draft, ids)
			if len(ids) > maxSkillHints {
				t.Errorf("band holds %d hints, want at most %d", len(ids), maxSkillHints)
			}
			if tc.first == "" && len(tc.wants) == 0 {
				if len(ids) != 0 || m.renderSkillHints() != "" {
					t.Fatalf("band names %v for a generic draft, want it dark", ids)
				}
				return
			}
			if m.renderSkillHints() == "" {
				t.Error("the band paints nothing while it holds hints")
			}
			if tc.first != "" && (len(ids) == 0 || ids[0] != tc.first) {
				t.Errorf("band names %v first, want %q", ids, tc.first)
			}
			for _, want := range tc.wants {
				if !slices.Contains(ids, want) {
					t.Errorf("band names %v, want it to include %q", ids, want)
				}
			}
		})
	}
}

// ----------------------------------------------------------------------------
// The band re-ranks once per pause (skillHintDelay), not per edit
// ----------------------------------------------------------------------------
//
// Every edit arms a debounce tick and the rank runs only when the tick for the LATEST edit lands.
// The tests deliver that tick by hand (settleBand) — never by waiting — so what they pin is a count
// of Suggest calls, not a clock.

// A burst of fifty keypresses ranks nothing while it lasts and exactly once when it pauses.
func TestBandRanksOncePerBurstOfKeys(t *testing.T) {
	t.Parallel()

	var rec suggestCall
	m := modelWithOverlayRoom(t, 24, bandOpts(gatedSuggest(&rec)))
	draft := strings.Repeat("audit the parser ", 3)[:50]
	for _, r := range draft {
		m = step(t, m, keyRune(r))
	}
	if rec.calls != 0 {
		t.Fatalf("the band ranked %d times while the keys were still coming, want 0", rec.calls)
	}

	m = settleBand(t, m)

	if rec.calls != 1 {
		t.Fatalf("fifty keypresses and one pause ranked %d times, want 1", rec.calls)
	}
	if len(m.skillHints) != 3 {
		t.Errorf("band shows %d hints after the pause, want 3", len(m.skillHints))
	}
}

// A bracketed paste is one edit: one tick, one rank.
func TestBandRanksOncePerPaste(t *testing.T) {
	t.Parallel()

	var rec suggestCall
	m := modelWithOverlayRoom(t, 24, bandOpts(gatedSuggest(&rec)))
	m = settleBand(t, step(t, m, tea.PasteMsg{Content: "audit the parser\nand the lexer too"}))

	if rec.calls != 1 {
		t.Fatalf("a paste ranked %d times, want 1", rec.calls)
	}
	if len(m.skillHints) != 3 {
		t.Errorf("band shows %d hints after a paste, want 3", len(m.skillHints))
	}
}

// A tick armed by an edit since superseded ranks nothing, changes nothing and owes nothing.
func TestStaleBandTickIsInert(t *testing.T) {
	t.Parallel()

	var rec suggestCall
	m := typeDraft(t, modelWithOverlayRoom(t, 24, bandOpts(gatedSuggest(&rec))), "audit the parser")
	stale := m.skillHintGen
	m = step(t, m, keyRune('s')) // a newer edit retires the generation above
	calls, hints, view := rec.calls, m.skillHints, m.View().Content

	next, cmd := stepCmd(t, m, skillHintTickMsg{gen: stale})

	if rec.calls != calls {
		t.Errorf("a stale tick ranked %d times, want 0", rec.calls-calls)
	}
	if cmd != nil {
		t.Error("a stale tick returned a Cmd")
	}
	if !slices.Equal(next.skillHints, hints) || next.View().Content != view {
		t.Error("a stale tick changed the band or the frame")
	}
}

// Enter before the tick lands spends exactly what the row is showing, and the tick the last key
// armed lands inert after the send rather than ranking an empty box.
func TestSendBeforeTheTickSpendsWhatIsShown(t *testing.T) {
	t.Parallel()

	var rec suggestCall
	m := typeDraft(t, modelWithOverlayRoom(t, 24, bandOpts(gatedSuggest(&rec))), "audit the parser")
	for _, r := range " now" {
		m = step(t, m, keyRune(r))
	}
	pending := m.skillHintGen
	calls := rec.calls

	m = step(t, m, keyEnter())

	for _, h := range bandSuggestions {
		if !m.spentSkills[h.ID] {
			t.Errorf("%q was on the row at send but is not spent", h.ID)
		}
	}
	m = step(t, m, skillHintTickMsg{gen: pending})
	if rec.calls != calls {
		t.Errorf("the pre-send tick ranked %d times after the send, want 0", rec.calls-calls)
	}
	if len(m.skillHints) != 0 {
		t.Errorf("band shows %d hints after the send, want none", len(m.skillHints))
	}
}

// Emptying the draft takes the row down on the edit that empties it, without waiting for a pause.
func TestEmptiedDraftClearsTheBandAtOnce(t *testing.T) {
	t.Parallel()

	var rec suggestCall
	m := typeDraft(t, modelWithOverlayRoom(t, 24, bandOpts(gatedSuggest(&rec))), "audit the parser")
	for range len("audit the parser") {
		m = step(t, m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	}

	if m.input.Value() != "" {
		t.Fatalf("draft = %q, want it emptied", m.input.Value())
	}
	if len(m.skillHints) != 0 || m.renderSkillHints() != "" {
		t.Error("the band outlives an emptied draft until the next pause")
	}
}

// A skill accepted from the Tab menu leaves the band the moment it is written into the box — ADR
// 0061 §3's "a skill already invoked in the draft is never suggested at all" holds before any
// pause.
func TestTabAcceptedSkillLeavesTheBandAtOnce(t *testing.T) {
	t.Parallel()

	var rec suggestCall
	m := typeDraft(t, modelWithOverlayRoom(t, 24, bandOpts(gatedSuggest(&rec))), "audit the parser")
	calls := rec.calls

	m = step(t, step(t, m, keyTab()), keyEnter()) // accept the strongest row: /security-audit

	if !strings.Contains(m.input.Value(), "/security-audit") {
		t.Fatalf("draft = %q, want the accepted token in it", m.input.Value())
	}
	if rec.calls != calls {
		t.Fatalf("the accept ranked %d times, want the invoked id filtered without a rank", rec.calls-calls)
	}
	for _, h := range m.skillHints {
		if h.ID == "security-audit" {
			t.Error("the accepted skill is still on the band before the next pause")
		}
	}
	if len(m.skillHints) != 2 {
		t.Errorf("band shows %d hints, want the two not accepted", len(m.skillHints))
	}
}

// With the knob off an edit arms nothing: the band's half of the edit path returns a nil Cmd.
func TestTypingWithTheKnobOffArmsNoTick(t *testing.T) {
	t.Parallel()

	var rec suggestCall
	opts := bandOpts(gatedSuggest(&rec))
	opts.UI.SkillSuggestions = false
	m := modelWithOverlayRoom(t, 24, opts)
	m = step(t, m, keyRune('a'))

	if _, cmd := m.recomputeAutocomplete(); cmd != nil {
		t.Error("an edit with the band switched off armed a tick")
	}
	if _, cmd := stepCmd(t, m, keyRune('b')); cmd != nil {
		t.Error("a keypress with the band switched off returned a Cmd")
	}
}

// An edit that opens a "/" menu takes the row down on that edit, not at the next pause.
func TestOpeningAnOverlayClearsTheBandAtOnce(t *testing.T) {
	t.Parallel()

	var rec suggestCall
	m := typeDraft(t, modelWithOverlayRoom(t, 24, bandOpts(gatedSuggest(&rec))), "audit the parser")
	for _, r := range " /co" {
		m = step(t, m, keyRune(r))
	}

	if !m.autocomplete.active {
		t.Fatal("the /-menu did not open")
	}
	if len(m.skillHints) != 0 {
		t.Errorf("band holds %d hints under the menu the edit just opened", len(m.skillHints))
	}
}

// BenchmarkBandKeystroke is one printable key typed through Model.Update with the band wired — the
// edit path that used to rank the whole draft on every key and now only arms the debounce tick.
func BenchmarkBandKeystroke(b *testing.B) {
	var rec suggestCall
	m := newModel(context.Background(), &fakeEngine{}, withTestUI(bandOpts(gatedSuggest(&rec))), nil)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = next.(Model)
	m.input.SetValue(strings.Repeat("audit the parser and the lexer ", 300))
	b.ResetTimer()

	for range b.N {
		next, _ = m.Update(keyRune('x'))
		m = next.(Model)
	}
}
