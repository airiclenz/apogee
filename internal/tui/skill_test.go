package tui

import (
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/skills"
)

// ----------------------------------------------------------------------------
// Skill UX harness
// ----------------------------------------------------------------------------

// fakeSkillCatalog is a deterministic SkillCatalog for the TUI tests. List returns its skills
// in the given order (the real catalog sorts by DisplayName; tests pass them pre-sorted), and
// skipped stands in for the SKILL.md files a real scan found but could not load.
type fakeSkillCatalog struct {
	skills  []skills.Skill
	skipped []skills.SkipError
}

func (f fakeSkillCatalog) List() []skills.Skill { return f.skills }

func (f fakeSkillCatalog) Skipped() []skills.SkipError { return f.skipped }

func (f fakeSkillCatalog) Get(id string) (skills.Skill, bool) {
	for _, s := range f.skills {
		if s.ID == id {
			return s, true
		}
	}
	return skills.Skill{}, false
}

// skillOpts is testOpts with a two-skill catalog wired in.
func skillOpts() Options {
	o := testOpts
	o.Skills = fakeSkillCatalog{skills: []skills.Skill{
		{ID: "clean-code", DisplayName: "Clean Code", Summary: "tidy the code", Body: "BE TIDY"},
		{ID: "review", DisplayName: "Review", Summary: "review a diff", Body: "REVIEW IT"},
	}}
	return o
}

// ----------------------------------------------------------------------------
// The merged "/" menu: one namespace, commands first, skills marked
// ----------------------------------------------------------------------------

// One "/" menu, two kinds of row: the matching commands first (a verb ACTS on the session), then
// the matching skills, marked with the transcript's own skill glyph and shown as the token they
// write.
func TestSlashMenuMergesCommandsAndSkills(t *testing.T) {
	m := newTestModelEng(t, &fakeEngine{}, skillOpts())
	m.input.SetValue("/c") // five c-commands, and "clean-code" matches as a substring
	ac := m.computeAutocomplete(m.caretByteOffset())

	var got []string
	for _, it := range ac.items {
		got = append(got, it.value)
	}
	// The verbs alphabetically, then the skill.
	want := []string{"clear", "color-scheme", "compact", "confine", "continue", "clean-code"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("merged rows = %v, want the commands before the skills %v", got, want)
	}
	row := ac.items[len(ac.items)-1]
	if !row.skill {
		t.Error("the skill row is not marked as a skill; accept would treat it as a command")
	}
	if len(row.cells) == 0 || !strings.Contains(row.cells[0], glyphSkill) || !strings.Contains(row.cells[0], "/clean-code") {
		t.Errorf("skill row cells = %q, want the %q marker and the /id token it writes in the first column",
			row.cells, glyphSkill)
	}
}

// Accepting a skill row writes its inline token at the point the human was typing — the "/id " the
// submit parse reads back out.
func TestAcceptSkillRowFromTheMergedMenu(t *testing.T) {
	m := newTestModelEng(t, &fakeEngine{}, skillOpts())
	m.input.SetValue("please /rev")
	m.autocomplete = m.computeAutocomplete(m.caretByteOffset())
	if len(m.autocomplete.items) != 1 || !m.autocomplete.items[0].skill {
		t.Fatalf("rows = %+v, want the single review skill row", m.autocomplete.items)
	}
	m = step(t, m, keyTab())

	if got, want := m.input.Value(), "please /review "; got != want {
		t.Errorf("accepted %q, want %q", got, want)
	}
	if got := m.promptEditor.submitParse(m.knownSkillID).skillIDs; !reflect.DeepEqual(got, []string{"review"}) {
		t.Errorf("the spliced token parses to %v, want [review]", got)
	}
}

// The same accept on a draft whose FIRST line soft-wraps: the caret must land at the end of the
// spliced token on line 2, so the next keystroke goes there and not into the middle of line 1. The
// splice re-finds the caret's logical row through the widget's own walk, and a wrapped first line
// is where a walk that steps VISUAL rows gets it wrong.
//
// The geometries are chosen to be the pathological ones, not merely wrapped: each first line ends
// with a space exactly at a row boundary, which is where bubbles' wrap appends a PHANTOM trailing
// sub-line that CursorDown can never enter (its column clamp stops one short of it). A walk of bare
// CursorDowns stands still there forever — it neither crosses the line nor moves — so the caret was
// abandoned on line 1 and the next keystroke landed in the middle of it. The widths are the app's
// real one (80 columns ⇒ a 76-column text area) and the two the walk also failed at.
func TestAcceptSkillRowSeatsTheCaretOnAWrappedDraft(t *testing.T) {
	cases := []struct {
		name    string
		window  int    // window columns; the text area is four narrower (border + padding)
		first   string // the first logical line
		phantom bool   // true ⇒ it fills its last row exactly and ends with a space
	}{
		{"app width", 80, strings.Repeat("aaa ", 19), true},              // 76 chars at a 76-column text area
		{"narrow", 12, strings.Repeat("wrapped ", 20), true},             // 8-column rows, filled exactly
		{"wide", 84, strings.Repeat("wrapped ", 20), true},               // 80-column rows, filled exactly
		{"plain wrap", 80, strings.Repeat("wrapping prose ", 12), false}, // an ordinary wrapped line
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestModelEng(t, &fakeEngine{}, skillOpts())
			m = step(t, m, tea.WindowSizeMsg{Width: tc.window, Height: 24})
			iw := m.inputInnerWidth()
			if tc.phantom {
				if got := len(tc.first) % iw; got != 0 || !strings.HasSuffix(tc.first, " ") {
					t.Fatalf("first line is %d chars at width %d (remainder %d, ends in a space: %v); the "+
						"phantom row needs an exact fill ending in a space",
						len(tc.first), iw, got, strings.HasSuffix(tc.first, " "))
				}
			}
			// The widget's own reckoning, not the repo's mirror of it (inputContentRows): the
			// premise of this fixture is a genuinely wrapped first line, so it is asserted through
			// bubbles rather than through code that only claims to agree with bubbles.
			if got := wrappedRowsOf(tc.first, iw); got < 2 {
				t.Fatalf("first line occupies %d wrapped rows in the widget, want a genuinely wrapped one", got)
			}
			m.input.SetValue(tc.first + "\nplease /rev")
			m.input.MoveToEnd()
			m, _ = m.recomputeAutocomplete()
			if len(m.autocomplete.items) == 0 {
				t.Fatalf("no completion offered for the /rev token: %+v", m.autocomplete)
			}
			m = step(t, m, keyTab())

			want := tc.first + "\nplease /review "
			if got := m.input.Value(); got != want {
				t.Fatalf("accepted %q, want %q", got, want)
			}
			if got := m.caretByteOffset(); got != len(want) {
				t.Errorf("caret at byte %d, want %d — the end of the spliced token", got, len(want))
			}
			// The proof that matters to the human: the next keystroke lands where the caret is drawn.
			m = step(t, m, keyRune('x'))
			if got := m.input.Value(); got != want+"x" {
				t.Errorf("the next keystroke produced %q, want %q", got, want+"x")
			}
		})
	}
}

// A skill whose id collides with a command verb is omitted from the merged rows — the verb owns the
// name in one namespace — and stays invocable by typing its "/id" token anywhere but at the head of
// the line, the only position the whole-input command rule claims.
func TestSlashMenuShadowsCollidingSkillID(t *testing.T) {
	o := testOpts
	o.Skills = fakeSkillCatalog{skills: []skills.Skill{{ID: "clear", DisplayName: "Clear Code"}}}
	m := newTestModelEng(t, &fakeEngine{}, o)

	m.input.SetValue("/clea")
	for _, it := range m.computeAutocomplete(m.caretByteOffset()).items {
		if it.skill {
			t.Fatalf("the merged menu offered a skill a command verb shadows: %+v", it)
		}
	}

	// Shadowed on the menu, still reachable in the text: mid-message the token is an ordinary skill
	// reference, so the submit parse resolves it.
	m.input.SetValue("tidy this /clear")
	parsed := m.promptEditor.submitParse(m.knownSkillID)
	if parsed.kind != kindMessage || !reflect.DeepEqual(parsed.skillIDs, []string{"clear"}) {
		t.Fatalf("the shadowed skill is unreachable in the text: %+v", parsed)
	}
}

// ----------------------------------------------------------------------------
// The merged "/" menu: rows rank by match quality, ties keep the scan order
// ----------------------------------------------------------------------------

func TestSlashMatchRank(t *testing.T) {
	tests := []struct {
		name    string
		partial string
		item    string
		want    int
	}{
		{"exact beats everything", "clear", "clear", 0},
		{"exact is case-insensitive", "Clear", "clear", 0},
		{"prefix", "imple", "implement-plan", 1},
		{"prefix is case-insensitive", "IMPLE", "Implement Plan", 1},
		{"substring ranks below prefix", "imple", "feature-implementation", 2},
		{"substring is case-insensitive", "Imple", "Feature Implementation", 2},
		{"no match at all", "zzz", "implement-plan", 3},
		{"empty partial prefixes every name", "", "implement-plan", 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := slashMatchRank(tc.partial, tc.item); got != tc.want {
				t.Errorf("slashMatchRank(%q, %q) = %d, want %d", tc.partial, tc.item, got, tc.want)
			}
		})
	}
}

// The reported bug: typing "/imple" listed /feature-implementation first — it merely sorted
// earlier in the catalog — so tab and ⏎ accepted the substring match over the skill whose name the
// human had actually started typing. The prefix match now leads, and it leads as the HIGHLIGHTED
// row (selected stays zero-valued), which is what accepting picks up.
func TestSlashMenuRanksPrefixMatchAboveSubstring(t *testing.T) {
	o := testOpts
	o.Skills = fakeSkillCatalog{skills: []skills.Skill{ // catalog order: sorted by DisplayName
		{ID: "feature-implementation", DisplayName: "Feature Implementation", Summary: "ship a feature"},
		{ID: "implement-plan", DisplayName: "Implement Plan", Summary: "execute a plan"},
	}}
	m := newTestModelEng(t, &fakeEngine{}, o)

	m.input.SetValue("/imple")
	ac := m.computeAutocomplete(m.caretByteOffset())

	var got []string
	for _, it := range ac.items {
		got = append(got, it.value)
	}
	want := []string{"implement-plan", "feature-implementation"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("merged rows = %v, want the prefix match first: %v", got, want)
	}
	if ac.selected != 0 {
		t.Errorf("selected = %d, want 0 — tab/⏎ must accept the best-ranked row", ac.selected)
	}
}

// Ranking only decides between TIERS. The bare "/" menu is one tier — an empty partial prefixes
// every name — so the whole list keeps the order the two registries hand over: the commands in
// table (alphabetical) order, then the skills in catalog order. TestSlashMenuMergesCommandsAndSkills
// pins the same stability for the mixed-tier "/c" case.
func TestSlashMenuKeepsScanOrderWithinOneRankTier(t *testing.T) {
	m := newTestModelEng(t, &fakeEngine{}, skillOpts())
	m.input.SetValue("/")
	ac := m.computeAutocomplete(m.caretByteOffset())

	var got []string
	for _, it := range ac.items {
		got = append(got, it.value)
	}
	want := make([]string, 0, len(commandSpecs)+2)
	for _, c := range commandSpecs {
		want = append(want, c.name)
	}
	want = append(want, "clean-code", "review")
	if !reflect.DeepEqual(got, want) {
		t.Errorf("bare '/' menu = %v, want the untouched scan order %v", got, want)
	}
}

// The cap is spent on the BEST matches, not on the alphabetically luckiest ones: with more
// matching skills than the menu can hold, the sole prefix match — last in catalog order — still
// leads the list. Capping inside the scan loop dropped it before the ranking could ever see it.
func TestSkillSuggestionsCapAfterRanking(t *testing.T) {
	catalog := make([]skills.Skill, 0, maxAutocompleteItems+2)
	for i := 0; i < maxAutocompleteItems+1; i++ {
		id := fmt.Sprintf("a%d-widget", i) // substring match; sorts before the prefix match
		catalog = append(catalog, skills.Skill{ID: id, DisplayName: id})
	}
	catalog = append(catalog, skills.Skill{ID: "widget-master", DisplayName: "widget-master"})
	o := testOpts
	o.Skills = fakeSkillCatalog{skills: catalog}
	m := newTestModelEng(t, &fakeEngine{}, o)

	m.input.SetValue("/widget") // no command verb matches, so every row is a skill
	ac := m.computeAutocomplete(m.caretByteOffset())

	if len(ac.items) != maxAutocompleteItems {
		t.Fatalf("menu offered %d rows, want the skill half still capped at %d", len(ac.items), maxAutocompleteItems)
	}
	if got := ac.items[0].value; got != "widget-master" {
		t.Errorf("first row = %q, want the prefix match widget-master to survive the cap and lead", got)
	}
}

// A fully typed skill token keeps its own row — the token being completed is not yet "already
// invoked" — and ⏎ then SENDS the message it stands in rather than re-completing it.
func TestTypedSkillTokenStaysOfferedAndSubmits(t *testing.T) {
	eng := &fakeEngine{stepFn: scriptedSteps()}
	m := newTestModelEng(t, eng, skillOpts())
	m.input.SetValue("please /review")
	m.autocomplete = m.computeAutocomplete(m.caretByteOffset())

	if len(m.autocomplete.items) != 1 || !m.autocomplete.items[0].skill {
		t.Fatalf("a finished skill token lost its own row: %+v", m.autocomplete.items)
	}
	if !m.autocompleteExactMatch() {
		t.Fatal("a finished skill token is not an exact match; ⏎ would re-complete instead of sending")
	}
	m, cmd := stepCmd(t, m, keyEnter())
	if m.state != stateRunning {
		t.Fatalf("state = %v, want running (⏎ sends the message the token stands in)", m.state)
	}
	drainCmd(t, m, cmd)
	if len(eng.submitted) != 1 || !reflect.DeepEqual(eng.submitted[0].SkillIDs, []string{"review"}) {
		t.Fatalf("the send did not carry the skill: %+v", eng.submitted)
	}
}

// The other half of that rule: a skill the draft ALREADY invokes elsewhere is dropped from the menu,
// read off the tokens standing in the buffer since there is no attachment state beside them. Delete
// the token and the row comes back — the exclusion self-heals with the text.
func TestSlashMenuExcludesSkillsAlreadyInTheBuffer(t *testing.T) {
	m := newTestModelEng(t, &fakeEngine{}, skillOpts())

	// "clean" prefixes no command verb, so the menu's rows here are the skill half alone.
	m.input.SetValue("/clean-code /clean")
	if ac := m.computeAutocomplete(m.caretByteOffset()); ac.active {
		t.Fatalf("a skill already invoked in the text is still offered: %+v", ac.items)
	}

	m.input.SetValue("/clean")
	ac := m.computeAutocomplete(m.caretByteOffset())
	if !ac.active || len(ac.items) != 1 || ac.items[0].value != "clean-code" {
		t.Errorf("removing the token did not restore the suggestion: %+v", ac)
	}
}

// ----------------------------------------------------------------------------
// Submit: inline tokens carry SkillIDs and stay in the text
// ----------------------------------------------------------------------------

func TestSubmitCarriesSkillIDs(t *testing.T) {
	eng := &fakeEngine{stepFn: scriptedSteps()}
	m := newTestModelEng(t, eng, skillOpts())
	m.input.SetValue("/clean-code do the thing /review")
	m, cmd := stepCmd(t, m, keyEnter())

	if m.state != stateRunning {
		t.Fatalf("state = %v, want running", m.state)
	}
	if v := m.input.Value(); v != "" {
		t.Errorf("input not cleared after submit: %q", v)
	}
	drainCmd(t, m, cmd)
	if len(eng.submitted) != 1 {
		t.Fatalf("Submit calls = %d, want 1", len(eng.submitted))
	}
	in := eng.submitted[0]
	// The tokens stay IN the text (owner override of the strip default): the model sees the
	// invocation the human typed AND the skill bodies the loop prepends for it.
	if want := "/clean-code do the thing /review"; in.Text != want {
		t.Errorf("submitted text = %q, want %q", in.Text, want)
	}
	if !reflect.DeepEqual(in.SkillIDs, []string{"clean-code", "review"}) {
		t.Errorf("submitted SkillIDs = %v, want both invoked ids", in.SkillIDs)
	}
}

// An input that is ONLY a skill token is a valid submit — "just run the skill" (edge default #2).
func TestSubmitBareSkillTokenSends(t *testing.T) {
	eng := &fakeEngine{stepFn: scriptedSteps()}
	m := newTestModelEng(t, eng, skillOpts())
	m.input.SetValue("/clean-code")
	m, cmd := stepCmd(t, m, keyEnter())

	if m.state != stateRunning {
		t.Fatalf("a bare skill token did not send: state = %v", m.state)
	}
	drainCmd(t, m, cmd)
	if len(eng.submitted) != 1 || !reflect.DeepEqual(eng.submitted[0].SkillIDs, []string{"clean-code"}) {
		t.Fatalf("bare-token send did not carry the skill: %+v", eng.submitted)
	}
	if eng.submitted[0].Text != "/clean-code" {
		t.Errorf("submitted text = %q, want the token itself", eng.submitted[0].Text)
	}
	if got := plain(m.View()); !strings.Contains(got, "/clean-code") {
		t.Errorf("the sent block does not show the token that stands for the skill:\n%s", got)
	}
	if !strings.Contains(m.View().Content, m.th.skillAccent.Render("/clean-code")) {
		t.Error("the sent token is not painted in the skill accent")
	}
}

// A message sent with text and a skill token keeps the invocation visible on its user block after
// the send (ISSUES #5: the attachment used to vanish once the input cleared) — and it is visible as
// the ACCENTED token inside the text, not as a chip beside it.
func TestSentUserBlockAccentsTheSkillToken(t *testing.T) {
	eng := &fakeEngine{stepFn: scriptedSteps()}
	m := newTestModelEng(t, eng, skillOpts())
	m.input.SetValue("/clean-code fix the parser")
	m, cmd := stepCmd(t, m, keyEnter())
	drainCmd(t, m, cmd)

	got := plain(m.View())
	if !strings.Contains(got, "/clean-code fix the parser") {
		t.Errorf("the sent message missing from the transcript:\n%s", got)
	}
	if strings.Contains(got, glyphSkill+" Clean Code") {
		t.Errorf("the sent block still badges the skill on a chip row:\n%s", got)
	}
	if !strings.Contains(m.View().Content, m.th.skillAccent.Render("/clean-code")) {
		t.Errorf("the invoked token is not painted in the skill accent (ISSUES #5):\n%s", got)
	}
}

// A token that matches no catalog id is ordinary prose: it travels verbatim and resolves nothing.
func TestSubmitUnknownTokenIsPlainText(t *testing.T) {
	eng := &fakeEngine{stepFn: scriptedSteps()}
	m := newTestModelEng(t, eng, skillOpts())
	m.input.SetValue("/clean-cod the typo and /usr/bin the path")
	m, cmd := stepCmd(t, m, keyEnter())
	drainCmd(t, m, cmd)

	if len(eng.submitted) != 1 {
		t.Fatalf("Submit calls = %d, want 1", len(eng.submitted))
	}
	if ids := eng.submitted[0].SkillIDs; len(ids) != 0 {
		t.Errorf("submitted SkillIDs = %v, want none (nothing in that line resolves)", ids)
	}
}

func TestSubmitEmptyAndNoSkillsIgnored(t *testing.T) {
	eng := &fakeEngine{}
	m := newTestModelEng(t, eng, skillOpts())
	m.input.SetValue("")
	m, cmd := stepCmd(t, m, keyEnter())
	if m.state != stateIdle || cmd != nil {
		t.Errorf("a truly empty submit was not ignored (state=%v cmd!=nil=%v)", m.state, cmd != nil)
	}
}

// /continue no longer carries skills: the chips it used to consume are gone, and its canned turn
// is the whole input by construction — there is no token in it to invoke anything with.
func TestContinueCarriesNoSkills(t *testing.T) {
	eng := &fakeEngine{stepFn: scriptedSteps()}
	m := newTestModelEng(t, eng, skillOpts())
	m.input.SetValue("/continue")
	m, cmd := stepCmd(t, m, keyEnter())
	drainCmd(t, m, cmd)

	if len(eng.submitted) != 1 {
		t.Fatalf("Submit calls = %d, want the canned turn", len(eng.submitted))
	}
	if ids := eng.submitted[0].SkillIDs; len(ids) != 0 {
		t.Errorf("/continue carried SkillIDs = %v, want none", ids)
	}
}

// ----------------------------------------------------------------------------
// Interjections: a skill token rides the queue
// ----------------------------------------------------------------------------

// Staging a message with a skill token while the model works carries the id on the row's
// UserInput — the silent drop the chip flow left behind (interject.go's discarded parse).
func TestStagedInterjectionCarriesSkillIDs(t *testing.T) {
	m := newTestModelEng(t, &fakeEngine{}, skillOpts())
	m.state = stateRunning
	m.box = newInterjectBox()
	m.input.SetValue("/review this diff too")
	m = step(t, m, keyEnter())

	if n := len(m.pendingInterjections); n != 1 {
		t.Fatalf("staged rows = %d, want 1", n)
	}
	if got := m.pendingInterjections[0].input.SkillIDs; !reflect.DeepEqual(got, []string{"review"}) {
		t.Errorf("staged SkillIDs = %v, want [review]", got)
	}
}

// The DELIVERED interjection records its skills exactly as a flushed send does: the ⧖ block paints
// the same accented token the ❯ block does, because the two differ only in when the message landed.
// The delivery fold used to drop the ids on the floor (addInterjected took text alone).
func TestDeliveredInterjectionAccentsTheSkillToken(t *testing.T) {
	m := newTestModelEng(t, &fakeEngine{}, skillOpts())
	m.state = stateRunning
	m.box = newInterjectBox()
	m.input.SetValue("/review this diff too")
	m = step(t, m, keyEnter())

	m = step(t, m, interjectedMsg{items: m.pendingInterjections})

	last := m.transcript.entries[len(m.transcript.entries)-1]
	if last.kind != entryInterjected {
		t.Fatalf("tail entry = %+v; want the delivered remark as an interjected block", last)
	}
	if want := (skillSpan{start: 0, end: len("/review")}); !reflect.DeepEqual(last.skillSpans, []skillSpan{want}) {
		t.Errorf("delivered block spans = %v, want %v — the invocation must be located in the text", last.skillSpans, want)
	}
	if got := plain(m.View()); strings.Contains(got, glyphSkill+" Review") {
		t.Errorf("the delivered remark still badges the skill on a chip row:\n%s", got)
	}
	if !strings.Contains(m.View().Content, m.th.skillAccent.Render("/review")) {
		t.Error("the delivered remark's token is not painted in the skill accent")
	}
}

// A flush of two rows naming the same skill unions to one id, exactly as the file refs do.
func TestFlushUnionsSkillIDs(t *testing.T) {
	eng := &fakeEngine{stepFn: scriptedSteps()}
	m := newTestModelEng(t, eng, skillOpts())
	m.state = stateRunning
	m.box = newInterjectBox()
	m.input.SetValue("/review the parser")
	m = step(t, m, keyEnter())
	m.input.SetValue("/review the tests too /clean-code")
	m = step(t, m, keyEnter())

	m.state = stateIdle
	m.input.SetValue("")
	m, cmd := stepCmd(t, m, keyEnter()) // ⏎ on an empty box sends what is held
	drainCmd(t, m, cmd)

	if len(eng.submitted) != 1 {
		t.Fatalf("Submit calls = %d, want the held rows sent as one message", len(eng.submitted))
	}
	if got := eng.submitted[0].SkillIDs; !reflect.DeepEqual(got, []string{"review", "clean-code"}) {
		t.Errorf("joined SkillIDs = %v, want [review clean-code] (first-seen, deduped)", got)
	}
}

// ----------------------------------------------------------------------------
// nil-catalog guard
// ----------------------------------------------------------------------------

func TestNilCatalogGuards(t *testing.T) {
	m := newTestModelEng(t, &fakeEngine{}, testOpts) // testOpts has no Skills

	// No token resolves without a catalog, so the message is ordinary prose — no panic.
	m.input.SetValue("/ghost do it")
	if got := m.promptEditor.submitParse(m.knownSkillID); len(got.skillIDs) != 0 {
		t.Errorf("skillIDs = %v with a nil catalog, want none", got.skillIDs)
	}
	// The one place the question is answered says no, rather than reaching through the nil catalog.
	if m.knownSkillID("ghost") {
		t.Error("knownSkillID = true with a nil catalog; want no id to resolve")
	}
}

// ----------------------------------------------------------------------------
// Live refresh: the merged "/" menu reloads the catalog when it opens
// ----------------------------------------------------------------------------

// reloadableCatalog is a SkillCatalog whose List reads through a pointer, so a ReloadSkills
// closure that mutates the same backing slice is reflected on the next List — modelling the
// shared skills.Provider whose Reload swaps in a fresh catalog both the menu and loop read.
type reloadableCatalog struct {
	skills *[]skills.Skill
}

func (f reloadableCatalog) List() []skills.Skill { return *f.skills }

func (f reloadableCatalog) Skipped() []skills.SkipError { return nil }

func (f reloadableCatalog) Get(id string) (skills.Skill, bool) {
	for _, s := range *f.skills {
		if s.ID == id {
			return s, true
		}
	}
	return skills.Skill{}, false
}

// reloadOpts wires a reloadable catalog whose ReloadSkills closure appends a "fresh" skill (as if
// it had just been added on disk) and counts reloads. The returned pointers let a test assert how
// many reloads fired and what the menu then shows.
func reloadOpts() (Options, *int) {
	current := []skills.Skill{
		{ID: "clean-code", DisplayName: "Clean Code", Summary: "tidy the code", Body: "BE TIDY"},
	}
	backing := &current
	reloads := 0
	o := testOpts
	o.Skills = reloadableCatalog{skills: backing}
	o.ReloadSkills = func() {
		reloads++
		*backing = append(*backing, skills.Skill{
			ID: "fresh", DisplayName: "Fresh", Summary: "added after launch", Body: "NEW",
		})
	}
	return o, &reloads
}

// runCmd runs a Cmd off the Update loop as the runtime would and feeds the message it produced back
// into the model, returning what that fold left. A nil Cmd or a Cmd that reports nothing changes
// nothing, which is exactly the runtime's behaviour.
func runCmd(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		return m
	}
	msg := cmd()
	if msg == nil {
		return m
	}
	return step(t, m, msg)
}

// Opening the merged "/" menu re-scans the catalog exactly once, OFF the Update goroutine: the
// keypress dispatches the walk as a Cmd rather than running it inline, and the menu lists the skill
// that walk discovered once its message lands — the live refresh a skill added since launch depends
// on, now costing the render loop nothing. It is edge-triggered on the REGION, so typing on inside
// the open menu must not walk the disk again.
func TestSlashMenuReloadsTheCatalogOnOpen(t *testing.T) {
	o, reloads := reloadOpts()
	m := newTestModelEng(t, &fakeEngine{}, o)

	m, cmd := stepCmd(t, m, keyRune('/')) // the "/" opens the merged menu → one reload, dispatched
	if cmd == nil {
		t.Fatal("opening the merged menu returned no Cmd; the re-scan must not run on the Update loop")
	}
	if *reloads != 0 {
		t.Fatalf("the re-scan ran inline during Update (%d reloads); it belongs on the Cmd goroutine", *reloads)
	}
	if !m.autocomplete.active || m.autocomplete.kind != acCommand {
		t.Fatalf("merged menu not open after '/': %+v", m.autocomplete)
	}

	m = runCmd(t, m, cmd) // the walk runs off the loop and reports back
	if *reloads != 1 {
		t.Fatalf("opening the merged menu triggered %d reloads, want exactly 1", *reloads)
	}
	var got []string
	for _, it := range m.autocomplete.items {
		got = append(got, it.value)
	}
	if !containsString(got, "fresh") {
		t.Errorf("the menu did not show the skill the reload added: %v", got)
	}

	// Typing further inside the already-open region must NOT re-scan disk (edge-triggered on open).
	m, cmd = stepCmd(t, m, keyRune('f'))
	m = runCmd(t, m, cmd)
	if *reloads != 1 {
		t.Errorf("typing inside the open menu re-scanned: %d reloads, want it to stay 1", *reloads)
	}
}

// Sending on an exact "/skill" token re-arms the edge: the box empties, the region behind it closes
// with it, and the NEXT "/" is therefore an opening that re-scans. The trigger is state BESIDE the
// text, so the text going away is not what closes the region — reset has to clear it, or the next
// menu opens over the catalog as it stood before the send and a skill added since is missing from
// it.
func TestSubmitReArmsTheRescanEdge(t *testing.T) {
	o, reloads := reloadOpts()
	m := newTestModelEng(t, &fakeEngine{}, o)

	for _, r := range "/clean-code" {
		var cmd tea.Cmd
		m, cmd = stepCmd(t, m, keyRune(r))
		m = runCmd(t, m, cmd)
	}
	if *reloads != 1 {
		t.Fatalf("typing the token triggered %d reloads, want exactly 1 — the region's opening", *reloads)
	}
	if !m.skillRegion {
		t.Fatalf("precondition: the input does not sit in a menu region: %+v", m.autocomplete)
	}

	m = step(t, m, keyEnter()) // an exact token falls through to submit rather than accepting the row
	if v := m.input.Value(); v != "" {
		t.Fatalf("the box still holds %q; this case must send, not accept a completion", v)
	}
	if m.skillRegion {
		t.Error("the emptied box still reports an open menu region")
	}

	m, cmd := stepCmd(t, m, keyRune('/')) // a fresh region: an opening owes a re-scan
	m = runCmd(t, m, cmd)
	if *reloads != 2 {
		t.Errorf("the next opening left the total at %d reloads, want 2 — the edge never re-armed", *reloads)
	}
}

// The reload's message repaints the menu the human left open — and leaves their highlighted row
// where it was. The walk now finishes after the keystroke, so a re-derivation that reset the
// selection would move the highlight out from under someone already arrowing down the list.
func TestSkillsReloadedKeepsTheHighlightedRow(t *testing.T) {
	o, _ := reloadOpts()
	m := newTestModelEng(t, &fakeEngine{}, o)

	m, cmd := stepCmd(t, m, keyRune('/'))
	m = step(t, m, keyDown()) // the human moves off the first row while the walk is still running
	want := m.autocomplete.items[m.autocomplete.selected].value
	if m.autocomplete.selected == 0 {
		t.Fatalf("precondition: the highlight did not move off row 0: %+v", m.autocomplete)
	}

	m = runCmd(t, m, cmd)
	if got := m.autocomplete.items[m.autocomplete.selected].value; got != want {
		t.Errorf("the reload moved the highlight to %q, want it left on %q", got, want)
	}
}

// A reload landing after the menu was dismissed repaints nothing. The walk outlives the keystroke
// that asked for it now, so esc has to hold against a result arriving afterwards — a dropdown that
// painted itself back a moment after being dismissed is the one outcome esc exists to prevent.
func TestSkillsReloadedAfterEscRepaintsNothing(t *testing.T) {
	o, reloads := reloadOpts()
	m := newTestModelEng(t, &fakeEngine{}, o)

	m, cmd := stepCmd(t, m, keyRune('/'))
	m = step(t, m, keyEsc()) // dismissed while the walk is still running
	if m.autocomplete.active {
		t.Fatalf("precondition: esc did not dismiss the menu: %+v", m.autocomplete)
	}

	m = runCmd(t, m, cmd) // the walk lands on a menu that is no longer there
	if *reloads != 1 {
		t.Fatalf("the dispatched walk did not run: %d reloads, want 1", *reloads)
	}
	if m.autocomplete.active {
		t.Errorf("a reload landing after esc re-opened the menu: %+v", m.autocomplete)
	}
}

// A nil ReloadSkills is simply a no-op (no panic, no Cmd) — the existing catalog stays as loaded.
func TestSlashMenuReloadNilSafe(t *testing.T) {
	m := newTestModelEng(t, &fakeEngine{}, skillOpts()) // a catalog, but no ReloadSkills
	m, cmd := stepCmd(t, m, keyRune('/'))               // must not panic with a nil ReloadSkills
	if cmd != nil {
		t.Error("an unwired ReloadSkills scheduled a Cmd; there is nothing for it to run")
	}
	if !m.autocomplete.active || m.autocomplete.kind != acCommand {
		t.Fatalf("the menu did not open with a nil ReloadSkills: %+v", m.autocomplete)
	}
}

// ----------------------------------------------------------------------------
// /skills — listing the catalog
// ----------------------------------------------------------------------------

// runSkillsNote runs "/skills" from idle, runs the re-scan Cmd it dispatched as the runtime would,
// and returns the note the listing recorded. It asserts the verb stayed local: no worker (the state
// never leaves idle), and the input emptied like every other command.
func runSkillsNote(t *testing.T, m Model) string {
	t.Helper()
	m.input.SetValue("/skills")
	m, cmd := stepCmd(t, m, keyEnter())
	if m.state != stateIdle {
		t.Errorf("state = %v after /skills, want idle", m.state)
	}
	if v := m.input.Value(); v != "" {
		t.Errorf("input not cleared after /skills: %q", v)
	}
	m = runCmd(t, m, cmd) // the re-scan runs off the loop; the listing lands on its message
	last := lastEntry(t, m)
	if last.kind != entryNote {
		t.Fatalf("/skills wrote a %v entry, want a note", last.kind)
	}
	return last.text
}

// /skills prints the catalog instead of sending "/skills" to the model: a header naming the
// count, then one line per skill carrying its /id, display name and summary.
func TestSkillsCommandListsCatalog(t *testing.T) {
	m := newTestModelEng(t, &fakeEngine{}, skillOpts())
	note := runSkillsNote(t, m)

	if !strings.Contains(note, "2 skills available") {
		t.Errorf("note does not head with the count:\n%s", note)
	}
	for _, want := range []string{
		"/clean-code  Clean Code — tidy the code",
		"/review  Review — review a diff",
	} {
		if !strings.Contains(note, want) {
			t.Errorf("note is missing the row %q:\n%s", want, note)
		}
	}
	// Catalog order is display order: the listing reads like the merged "/" menu.
	if strings.Index(note, "/clean-code") > strings.Index(note, "/review") {
		t.Errorf("rows are not in catalog order:\n%s", note)
	}
}

// The catalog is re-scanned BEFORE it is listed, so a skill added since launch shows up — the
// same live refresh the merged "/" menu edge-triggers on open. The reload stub appends "fresh", so
// row can only be there if the reload ran first.
func TestSkillsCommandReloadsBeforeListing(t *testing.T) {
	o, reloads := reloadOpts()
	m := newTestModelEng(t, &fakeEngine{}, o)
	note := runSkillsNote(t, m)

	if *reloads != 1 {
		t.Errorf("/skills triggered %d reloads, want exactly 1", *reloads)
	}
	if !strings.Contains(note, "/fresh") {
		t.Errorf("the listing predates the reload (no /fresh row):\n%s", note)
	}
}

// The re-scan /skills asks for runs OFF the Update goroutine, exactly as the merged "/" menu's does:
// it is the same blocking walk of the source dirs, and running it on the loop stalls the render for
// its length (ADR 0011) — behind a verb instead of a keystroke, but the same freeze. So ⏎ must
// return with the reload unrun and nothing written yet, and the listing must land on the message
// the dispatched Cmd delivers, over the catalog that one completed scan installed.
func TestSkillsCommandRescansOffTheUpdateLoop(t *testing.T) {
	o, reloads := reloadOpts()
	m := newTestModelEng(t, &fakeEngine{}, o)
	m.input.SetValue("/skills")
	before := len(m.transcript.entries)

	m, cmd := stepCmd(t, m, keyEnter())
	if *reloads != 0 {
		t.Fatalf("the re-scan ran inline during Update (%d reloads); it belongs on the Cmd goroutine", *reloads)
	}
	if cmd == nil {
		t.Fatal("/skills dispatched no Cmd; its re-scan has nowhere to run")
	}
	if got := len(m.transcript.entries) - before; got != 0 {
		t.Errorf("/skills wrote %d entries before its scan finished, want none — the listing is the scan's result", got)
	}

	m = runCmd(t, m, cmd)
	if *reloads != 1 {
		t.Errorf("the dispatched Cmd ran %d reloads, want exactly 1", *reloads)
	}
	note := lastEntry(t, m).text
	if !strings.Contains(note, "/fresh") {
		t.Errorf("the folded listing does not reflect the completed re-scan (no /fresh row):\n%s", note)
	}
}

// With no catalog wired (and no ReloadSkills) the verb still answers — no panic — and the note
// says where discovery looks, so "no skills" is actionable rather than a dead end. The global
// library it names is the home THIS run resolved (Options.ConfigHome — what --config /
// APOGEE_CONFIG selected), never the ~/.apogee default the run may not be using.
func TestSkillsCommandWithNoCatalog(t *testing.T) {
	o := testOpts // no Skills, no ReloadSkills
	o.ConfigHome = filepath.Join("elsewhere", "apogee-home")
	o.Workspace = filepath.Join("home", "code", "proj")
	m := newTestModelEng(t, &fakeEngine{}, o)
	note := runSkillsNote(t, m)

	if !strings.Contains(note, "no skills found") {
		t.Errorf("note does not report an empty catalog:\n%s", note)
	}
	if strings.Contains(note, filepath.Join("~", ".apogee")) {
		t.Errorf("note names the default home instead of the configured one:\n%s", note)
	}
	// The dirs are listed in the order sourceDirs walks (skills/load.go) — increasing priority,
	// so the global library that wins an id clash (ADR 0032) is the LAST line, not the first.
	prev := -1
	for _, want := range []string{
		filepath.Join("home", "code", "proj", ".apogee", "skills"),
		filepath.Join("home", "code", "proj", "skills"),
		filepath.Join("elsewhere", "apogee-home", "skills"),
	} {
		at := strings.Index(note, want)
		if at < 0 {
			t.Errorf("note does not name the source dir %q:\n%s", want, note)
			continue
		}
		if at < prev {
			t.Errorf("source dir %q is out of the layered order:\n%s", want, note)
		}
		prev = at
	}
}

// skillCatalogNote is pure, so its wording is pinned without a Model: the singular header, a
// skill with no summary, and the fallbacks that stand in for the two unwired roots.
func TestSkillCatalogNote(t *testing.T) {
	one := skillCatalogNote([]skills.Skill{{ID: "review", DisplayName: "Review"}}, nil, "/home/.apogee", "/ws")
	if !strings.HasPrefix(one, "1 skill available:") {
		t.Errorf("singular header wrong:\n%s", one)
	}
	if !strings.Contains(one, "/review  Review") || strings.Contains(one, "—") {
		t.Errorf("a summary-less skill must render without the dash:\n%s", one)
	}
	empty := skillCatalogNote(nil, nil, "", "")
	if !strings.Contains(empty, "<workspace>") {
		t.Errorf("an unwired workspace must render the placeholder:\n%s", empty)
	}
	if !strings.Contains(empty, filepath.Join("~", ".apogee", "skills")) {
		t.Errorf("an unwired home must render the ~/.apogee spelling:\n%s", empty)
	}
}

// ----------------------------------------------------------------------------
// Where a skill came from — the disclosure both surfaces render
// ----------------------------------------------------------------------------

// sourcedSkillOpts wires a catalog holding one skill from each root — one the opened project
// ships, one from the user's own library — each stamped with the Dir the loader would give it.
func sourcedSkillOpts(ws, home string) Options {
	o := testOpts
	o.Workspace = ws
	o.ConfigHome = home
	o.Skills = fakeSkillCatalog{skills: []skills.Skill{
		{ID: "clean-code", DisplayName: "Clean Code", Summary: "tidy the code", Body: "BE TIDY",
			Dir: filepath.Join(ws, ".apogee", "skills", "clean-code")},
		{ID: "clean-house", DisplayName: "Clean House", Summary: "tidy the house", Body: "BE TIDY",
			Dir: filepath.Join(home, "skills", "clean-house")},
	}}
	return o
}

// Both surfaces that list skills name the SOURCE each one came from — the merged "/" menu and the
// /skills report — because everything else on a skill row (the id, the display name, the summary)
// is text the SKILL.md chose for itself, and a skill impersonating a command scores an exact match
// and sorts above the verb it imitates. The source is the field the project cannot write.
//
// On the menu it is asserted to be in the row's FIRST cell, beside the id: a source rendered after
// the description would be the first thing a padded description pushes off the pane's edge.
func TestSkillRowsDiscloseTheirSource(t *testing.T) {
	ws, home := filepath.Join("/ws"), filepath.Join("/home", ".apogee")
	m := newTestModelEng(t, &fakeEngine{}, sourcedSkillOpts(ws, home))
	m.input.SetValue("/clean")
	ac := m.computeAutocomplete(m.caretByteOffset())

	rows := map[string]popupRow{}
	for _, it := range ac.items {
		if it.skill {
			rows[it.value] = it.cells
		}
	}
	for id, want := range map[string]string{
		"clean-code":  "/clean-code · " + skillSourceWorkspace,
		"clean-house": "/clean-house · " + skillSourceLibrary,
	} {
		cells, ok := rows[id]
		if !ok {
			t.Fatalf("the merged menu offers no row for %q: %+v", id, ac.items)
		}
		if !strings.Contains(cells[0], want) {
			t.Errorf("menu row for %q = %q, want its token cell to disclose %q", id, cells, want)
		}
	}

	note := runSkillsNote(t, m)
	for _, want := range []string{"/clean-code · " + skillSourceWorkspace, "/clean-house · " + skillSourceLibrary} {
		if !strings.Contains(note, want) {
			t.Errorf("/skills does not disclose %q:\n%s", want, note)
		}
	}
}

// skillSource maps a loaded skill's Dir back onto the source dir it came from, and the mapping is
// asserted at each root the loader walks — including the two cases a naive prefix test gets wrong:
// a sibling folder that merely starts like a root, and a home nested INSIDE the workspace, where
// the label must name the source that wins an id collision (ADR 0032).
func TestSkillSourceNamesTheRootItCameFrom(t *testing.T) {
	ws, home := filepath.Join("/ws"), filepath.Join("/home", ".apogee")
	for _, c := range []struct {
		name, dir, want string
	}{
		{"the project's .apogee/skills", filepath.Join(ws, ".apogee", "skills", "x"), skillSourceWorkspace},
		{"the project's bare skills/", filepath.Join(ws, "skills", "x"), skillSourceWorkspace},
		{"the user's library", filepath.Join(home, "skills", "x"), skillSourceLibrary},
		{"a sibling that merely starts alike", filepath.Join(ws, "skills-vendored", "x"), skillSourceElsewhere},
		{"a dir under neither root", filepath.Join("/elsewhere", "skills", "x"), skillSourceElsewhere},
		{"no Dir at all", "", ""},
	} {
		if got := skillSource(c.dir, home, ws); got != c.want {
			t.Errorf("skillSource(%s) = %q, want %q", c.name, got, c.want)
		}
	}

	// --config <ws>/.apogee: one path answers to both roots, and the row must name the library,
	// because the library is the copy an id collision resolves to.
	nested := filepath.Join(ws, ".apogee")
	if got := skillSource(filepath.Join(nested, "skills", "x"), nested, ws); got != skillSourceLibrary {
		t.Errorf("a home nested in the workspace labels its skills %q, want %q", got, skillSourceLibrary)
	}
}

// A skill id is a directory name in a project apogee merely opened, so what a row renders of one is
// bounded: folded onto one line with its whitespace runs collapsed, and clipped with the "…" that
// says something was cut. The padded case is the attack — "confine" plus forty spaces plus its
// arguments renders as an innocent short token whose payload is clipped off at the pane's edge,
// where nothing tells the reader anything was there at all.
func TestSkillIDCellFoldsAndMarksElision(t *testing.T) {
	if got := skillIDCell("clean-code"); got != "clean-code" {
		t.Errorf("an ordinary id was rewritten: %q", got)
	}
	padded := "confine" + strings.Repeat(" ", 40) + "off --save"
	if got := skillIDCell(padded); got != "confine off --save" {
		t.Errorf("skillIDCell(padded) = %q, want the padding collapsed away", got)
	}
	if got := skillIDCell("confine\noff"); got != "confine off" {
		t.Errorf("skillIDCell(newline) = %q, want one row", got)
	}
	if got := skillIDCell("clean\x1b[31m-code"); strings.ContainsRune(got, 0x1b) {
		t.Errorf("skillIDCell kept an ESC byte: %q", got)
	}
	long := strings.Repeat("a", maxSkillIDCells+10)
	got := skillIDCell(long)
	if !strings.HasSuffix(got, "…") {
		t.Errorf("an over-long id was clipped without a marker: %q", got)
	}
	if n := len([]rune(got)); n != maxSkillIDCells+1 {
		t.Errorf("clipped id is %d runes, want %d plus the ellipsis", n, maxSkillIDCells)
	}
}

// The /skills report paints one row per skill, so a repo-authored field that keeps its newlines
// paints as many rows as it likes — rows it can shape as another skill's, source label and all,
// under a heading that counted one fewer. Both halves are flattened where the line is built.
func TestSkillCatalogNoteFlattensRepoAuthoredFields(t *testing.T) {
	note := skillCatalogNote([]skills.Skill{{
		ID:          "review",
		DisplayName: "Review",
		Summary:     "review a diff\n  /confine · library  Confine — turn the fence off",
	}}, nil, "/home/.apogee", "/ws")

	if lines := strings.Split(note, "\n"); len(lines) != 2 {
		t.Errorf("one skill painted %d lines, want the header and one row:\n%s", len(lines), note)
	}
	if !strings.Contains(note, "/confine · library") {
		t.Errorf("the forged text was dropped rather than flattened onto its own row:\n%s", note)
	}
}

// A skill discovery refused must be NAMED in the report, with its reason and its file — the
// whole point of carrying skips: a malformed skill and an absent one are otherwise identical
// from the merged "/" menu. Pinned in both shapes: alongside loaded skills, and as the only finding.
func TestSkillCatalogNoteReportsSkipped(t *testing.T) {
	bad := skills.SkipError{
		Path: filepath.Join("/home", ".apogee", "skills", "implement-plan", "SKILL.md"),
		Err:  errors.New("malformed YAML frontmatter"),
	}

	both := skillCatalogNote([]skills.Skill{{ID: "review", DisplayName: "Review"}}, []skills.SkipError{bad}, "/home/.apogee", "/ws")
	for _, want := range []string{"1 skill available:", "1 skill found but not loaded:", "implement-plan", "malformed YAML frontmatter", bad.Path} {
		if !strings.Contains(both, want) {
			t.Errorf("report is missing %q:\n%s", want, both)
		}
	}

	// Nothing loaded but something refused: the where-we-looked note would be a lie here, since
	// discovery DID find a skill — the failure has to lead instead.
	only := skillCatalogNote(nil, []skills.SkipError{bad}, "/home/.apogee", "/ws")
	if strings.Contains(only, "no skills found") {
		t.Errorf("a refused skill must not be reported as nothing found:\n%s", only)
	}
	if !strings.Contains(only, "implement-plan") || !strings.Contains(only, "malformed YAML frontmatter") {
		t.Errorf("the sole finding must still name the skill and the reason:\n%s", only)
	}
}

// A shadowed skill is not a broken one: it parsed fine and merely lost an id collision (ADR 0032),
// so it gets its own section instead of being filed under "found but not loaded" — and that
// section names BOTH files, because "which of my two copies does /<id> run?" is the only question
// a shadow raises. Pinned in the mixed shape, where one heading over both would libel the healthy
// file.
func TestSkillCatalogNoteSeparatesShadowedFromBroken(t *testing.T) {
	winner := filepath.Join("/home", ".apogee", "skills", "review", "SKILL.md")
	bad := skills.SkipError{
		Path: filepath.Join("/home", ".apogee", "skills", "implement-plan", "SKILL.md"),
		Err:  errors.New("malformed YAML frontmatter"),
	}
	shadowed := skills.SkipError{
		Path: filepath.Join("/ws", ".apogee", "skills", "review", "SKILL.md"),
		Err:  skills.ShadowedError{By: winner},
	}

	note := skillCatalogNote(
		[]skills.Skill{{ID: "review", DisplayName: "Review"}},
		[]skills.SkipError{bad, shadowed},
		"/home/.apogee", "/ws",
	)
	for _, want := range []string{
		"1 skill found but not loaded:", bad.Path, "malformed YAML frontmatter",
		"1 skill shadowed by another of the same id:", shadowed.Path, winner,
	} {
		if !strings.Contains(note, want) {
			t.Errorf("report is missing %q:\n%s", want, note)
		}
	}
	// Each skip must sit under ITS heading: the failure above the shadow heading, the shadowed
	// pair below it. Counting them apart is only half the fix if the rows land in the wrong half.
	shadowHead := strings.Index(note, "shadowed by another of the same id:")
	if at := strings.Index(note, bad.Path); at > shadowHead {
		t.Errorf("the load failure is filed under the shadow heading:\n%s", note)
	}
	if at := strings.Index(note, shadowed.Path); at < shadowHead {
		t.Errorf("the shadowed skill is filed under the failure heading:\n%s", note)
	}
}

// When every skip is a shadow, nothing failed to load — and the report must not say otherwise.
// This is the case the single old heading got flatly wrong: a healthy skill reported as broken
// sends the human to fix a file that has nothing wrong with it.
func TestSkillCatalogNoteShadowOnlyClaimsNoFailure(t *testing.T) {
	shadowed := skills.SkipError{
		Path: filepath.Join("/ws", ".apogee", "skills", "review", "SKILL.md"),
		Err:  skills.ShadowedError{By: filepath.Join("/home", ".apogee", "skills", "review", "SKILL.md")},
	}
	note := skillCatalogNote(
		[]skills.Skill{{ID: "review", DisplayName: "Review"}},
		[]skills.SkipError{shadowed},
		"/home/.apogee", "/ws",
	)
	if strings.Contains(note, "not loaded") {
		t.Errorf("a shadowed skill is reported as a load failure:\n%s", note)
	}
	if !strings.Contains(note, "1 skill shadowed by another of the same id:") {
		t.Errorf("the shadow section is missing its singular heading:\n%s", note)
	}
}

// The /skills command reads the skips off the SAME catalog it lists, so a broken skill surfaces
// through the real command path and not merely through the pure renderer.
func TestSkillsCommandReportsSkipped(t *testing.T) {
	o := testOpts
	o.Skills = fakeSkillCatalog{
		skills: []skills.Skill{{ID: "review", DisplayName: "Review", Summary: "review a diff"}},
		skipped: []skills.SkipError{{
			Path: filepath.Join("lib", "skills", "broken", "SKILL.md"),
			Err:  errors.New("malformed YAML frontmatter"),
		}},
	}
	m := newTestModelEng(t, &fakeEngine{}, o)
	note := runSkillsNote(t, m)

	if !strings.Contains(note, "/review") {
		t.Errorf("the loaded half is missing:\n%s", note)
	}
	for _, want := range []string{"broken", "malformed YAML frontmatter"} {
		if !strings.Contains(note, want) {
			t.Errorf("/skills did not report the skipped skill (%q missing):\n%s", want, note)
		}
	}
}
