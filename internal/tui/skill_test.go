package tui

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/skills"
)

// ----------------------------------------------------------------------------
// /skill UX harness
// ----------------------------------------------------------------------------

// fakeSkillCatalog is a deterministic SkillCatalog for the TUI tests. List returns its skills
// in the given order (the real catalog sorts by DisplayName; tests pass them pre-sorted).
type fakeSkillCatalog struct {
	skills []skills.Skill
}

func (f fakeSkillCatalog) List() []skills.Skill { return f.skills }

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

func keyBackspace() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyBackspace} }

// ----------------------------------------------------------------------------
// skillArgToken (pure)
// ----------------------------------------------------------------------------

func TestSkillArgToken(t *testing.T) {
	tests := []struct {
		value     string
		wantStart int
		wantPart  string
		wantOK    bool
	}{
		{"/skill ", 0, "", true},
		{"/skill cl", 0, "cl", true},
		{"fix /skill cl", 4, "cl", true},
		{"/skill", 0, "", false},      // bare command, no arg region yet
		{"/skill foo ", 0, "", false}, // completed arg (trailing space)
		{"hello", 0, "", false},
		{"@main.go", 0, "", false},
		{"", 0, "", false},
		{" cl", 0, "", false}, // leading space, no /skill before it
	}
	for _, tc := range tests {
		t.Run(tc.value, func(t *testing.T) {
			start, part, ok := skillArgToken(tc.value)
			if ok != tc.wantOK {
				t.Fatalf("skillArgToken(%q) ok = %v, want %v", tc.value, ok, tc.wantOK)
			}
			if ok && (start != tc.wantStart || part != tc.wantPart) {
				t.Errorf("skillArgToken(%q) = (%d, %q), want (%d, %q)", tc.value, start, part, tc.wantStart, tc.wantPart)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// Autocomplete: the skill dropdown + the /skill command offer
// ----------------------------------------------------------------------------

func TestComputeAutocompleteSkillDropdown(t *testing.T) {
	m := newTestModelEng(t, &fakeEngine{}, skillOpts())

	// "/skill " (empty partial) lists all skills, in display order.
	m.input.SetValue("/skill ")
	ac := m.computeAutocomplete()
	if !ac.active || ac.kind != acSkill {
		t.Fatalf("overlay = {active:%v kind:%v}, want active skill", ac.active, ac.kind)
	}
	var got []string
	for _, it := range ac.items {
		got = append(got, it.value)
	}
	if !reflect.DeepEqual(got, []string{"clean-code", "review"}) {
		t.Errorf("skill suggestions = %v, want both skills", got)
	}

	// A partial narrows by id/displayName substring.
	m.input.SetValue("/skill rev")
	ac = m.computeAutocomplete()
	if len(ac.items) != 1 || ac.items[0].value != "review" {
		t.Fatalf("narrowed suggestions = %+v, want [review]", ac.items)
	}
}

func TestCommandDropdownOffersSkill(t *testing.T) {
	m := newTestModelEng(t, &fakeEngine{}, skillOpts())
	m.input.SetValue("/s") // only "skill" begins with s
	ac := m.computeAutocomplete()
	if !ac.active || ac.kind != acCommand {
		t.Fatalf("overlay = {active:%v kind:%v}, want active command", ac.active, ac.kind)
	}
	if len(ac.items) != 1 || ac.items[0].value != "skill" {
		t.Fatalf("'/s' suggestions = %+v, want [skill]", ac.items)
	}
	// The full "/" menu includes /skill alongside the three real commands.
	m.input.SetValue("/")
	m.autocomplete = m.computeAutocomplete()
	if got := plain(m.View()); !strings.Contains(got, "/skill") {
		t.Errorf("'/' menu does not offer /skill:\n%s", got)
	}
}

func TestSkillArgWinsOverBareCommand(t *testing.T) {
	m := newTestModelEng(t, &fakeEngine{}, skillOpts())
	// "/skill" (no space) is the command branch (offers /skill); "/skill " (space) is the picker.
	m.input.SetValue("/skill")
	if ac := m.computeAutocomplete(); ac.kind != acCommand {
		t.Errorf("'/skill' (no space) kind = %v, want command", ac.kind)
	}
	m.input.SetValue("/skill ")
	if ac := m.computeAutocomplete(); ac.kind != acSkill {
		t.Errorf("'/skill ' (space) kind = %v, want skill", ac.kind)
	}
}

// ----------------------------------------------------------------------------
// Accept: attach a chip + strip the text; the /skill command chains into the picker
// ----------------------------------------------------------------------------

func TestAcceptSkillAttachesAndStrips(t *testing.T) {
	m := newTestModelEng(t, &fakeEngine{}, skillOpts())
	m.input.SetValue("fix /skill cl")
	m.autocomplete = m.computeAutocomplete() // acSkill, [clean-code]
	m = step(t, m, keyTab())

	if !reflect.DeepEqual(m.pendingSkills, []string{"clean-code"}) {
		t.Errorf("pendingSkills = %v, want [clean-code]", m.pendingSkills)
	}
	if got := m.input.Value(); got != "fix " {
		t.Errorf("after attach input = %q, want the /skill text stripped to %q", got, "fix ")
	}
	if m.autocomplete.active {
		t.Error("overlay still open after attach")
	}
	if got := plain(m.View()); !strings.Contains(got, "Clean Code") {
		t.Errorf("attached chip not rendered:\n%s", got)
	}
}

func TestAcceptSkillDedupes(t *testing.T) {
	m := newTestModelEng(t, &fakeEngine{}, skillOpts())
	m.pendingSkills = []string{"clean-code"}
	// The picker excludes an already-attached skill, so "cl" now matches nothing.
	m.input.SetValue("/skill cl")
	if ac := m.computeAutocomplete(); ac.active {
		t.Errorf("an already-attached skill is still offered: %+v", ac.items)
	}
}

func TestSkillCommandChainsIntoPicker(t *testing.T) {
	m := newTestModelEng(t, &fakeEngine{}, skillOpts())
	m.input.SetValue("/skill")
	m.autocomplete = m.computeAutocomplete() // command menu, highlighted "skill"
	m = step(t, m, keyTab())                 // accept the /skill command
	if got := m.input.Value(); got != "/skill " {
		t.Fatalf("accepting /skill gave %q, want %q", got, "/skill ")
	}
	if !m.autocomplete.active || m.autocomplete.kind != acSkill {
		t.Errorf("accepting /skill did not chain into the skill picker: %+v", m.autocomplete)
	}
}

func TestEnterOnSkillCommandDoesNotSubmit(t *testing.T) {
	eng := &fakeEngine{}
	m := newTestModelEng(t, eng, skillOpts())
	m.input.SetValue("/skill")
	m.autocomplete = m.computeAutocomplete()
	m = step(t, m, keyEnter()) // Enter completes /skill → picker; never sends "/skill"
	if m.state != stateIdle {
		t.Errorf("Enter on /skill launched a worker (state=%v); it must only open the picker", m.state)
	}
	if !m.autocomplete.active || m.autocomplete.kind != acSkill {
		t.Errorf("Enter on /skill did not open the picker: %+v", m.autocomplete)
	}
}

// ----------------------------------------------------------------------------
// Submit: carry SkillIDs, allow empty-text-with-skills, clear chips
// ----------------------------------------------------------------------------

func TestSubmitCarriesSkillIDs(t *testing.T) {
	eng := &fakeEngine{stepFn: scriptedSteps()}
	m := newTestModelEng(t, eng, skillOpts())
	m.pendingSkills = []string{"clean-code", "review"}
	m.input.SetValue("do the thing")
	m, cmd := stepCmd(t, m, keyEnter())

	if m.state != stateRunning {
		t.Fatalf("state = %v, want running", m.state)
	}
	if len(m.pendingSkills) != 0 {
		t.Errorf("pendingSkills not cleared after submit: %v", m.pendingSkills)
	}
	drainCmd(t, m, cmd)
	if len(eng.submitted) != 1 {
		t.Fatalf("Submit calls = %d, want 1", len(eng.submitted))
	}
	in := eng.submitted[0]
	if in.Text != "do the thing" {
		t.Errorf("submitted text = %q", in.Text)
	}
	if !reflect.DeepEqual(in.SkillIDs, []string{"clean-code", "review"}) {
		t.Errorf("submitted SkillIDs = %v, want both attached ids", in.SkillIDs)
	}
}

func TestSubmitEmptyTextWithSkillsSends(t *testing.T) {
	eng := &fakeEngine{stepFn: scriptedSteps()}
	m := newTestModelEng(t, eng, skillOpts())
	m.pendingSkills = []string{"clean-code"}
	m.input.SetValue("") // no text, just an attached skill
	m, cmd := stepCmd(t, m, keyEnter())

	if m.state != stateRunning {
		t.Fatalf("an empty message with a skill attached did not send: state = %v", m.state)
	}
	drainCmd(t, m, cmd)
	if len(eng.submitted) != 1 || !reflect.DeepEqual(eng.submitted[0].SkillIDs, []string{"clean-code"}) {
		t.Fatalf("empty-text send did not carry the skill: %+v", eng.submitted)
	}
	if got := plain(m.View()); !strings.Contains(got, "Clean Code") {
		t.Errorf("transcript should note the attached skill on an empty send:\n%s", got)
	}
}

// A message sent WITH text and an attached skill keeps the skill visible on its user block
// after the send (ISSUES #5: the attachment used to vanish once the chips cleared from the
// input).
func TestSentUserBlockShowsSkillChipsWithText(t *testing.T) {
	eng := &fakeEngine{stepFn: scriptedSteps()}
	m := newTestModelEng(t, eng, skillOpts())
	m.pendingSkills = []string{"clean-code"}
	m.input.SetValue("fix the parser")
	m, cmd := stepCmd(t, m, keyEnter())
	drainCmd(t, m, cmd)

	got := plain(m.View())
	if !strings.Contains(got, "fix the parser") {
		t.Errorf("sent text missing from the transcript:\n%s", got)
	}
	if !strings.Contains(got, "Clean Code") {
		t.Errorf("attached skill not shown on the sent user block (ISSUES #5):\n%s", got)
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

// ----------------------------------------------------------------------------
// Chips: backspace removal, command interactions
// ----------------------------------------------------------------------------

func TestBackspaceOnEmptyRemovesLastChip(t *testing.T) {
	m := newTestModelEng(t, &fakeEngine{}, skillOpts())
	m.pendingSkills = []string{"clean-code", "review"}
	m.input.SetValue("")
	m = step(t, m, keyBackspace())
	if !reflect.DeepEqual(m.pendingSkills, []string{"clean-code"}) {
		t.Errorf("after backspace pendingSkills = %v, want [clean-code]", m.pendingSkills)
	}
	m = step(t, m, keyBackspace())
	if len(m.pendingSkills) != 0 {
		t.Errorf("after second backspace pendingSkills = %v, want empty", m.pendingSkills)
	}
}

func TestBackspaceWithTextDoesNotPopChip(t *testing.T) {
	m := newTestModelEng(t, &fakeEngine{}, skillOpts())
	m.pendingSkills = []string{"clean-code"}
	m.input.SetValue("ab")
	m = step(t, m, keyBackspace()) // edits the text, not the chip
	if len(m.pendingSkills) != 1 {
		t.Errorf("backspace popped a chip while the input had text: %v", m.pendingSkills)
	}
	if m.input.Value() != "a" {
		t.Errorf("backspace did not edit the text: %q", m.input.Value())
	}
}

func TestClearAndCompactClearChips(t *testing.T) {
	for _, cmd := range []string{"/clear", "/compact"} {
		t.Run(cmd, func(t *testing.T) {
			m := newTestModelEng(t, &fakeEngine{}, skillOpts())
			m.pendingSkills = []string{"clean-code"}
			m.input.SetValue(cmd)
			m = step(t, m, keyEnter())
			if len(m.pendingSkills) != 0 {
				t.Errorf("%s did not clear staged chips: %v", cmd, m.pendingSkills)
			}
		})
	}
}

func TestContinueCarriesChips(t *testing.T) {
	eng := &fakeEngine{stepFn: scriptedSteps()}
	m := newTestModelEng(t, eng, skillOpts())
	m.pendingSkills = []string{"review"}
	m.input.SetValue("/continue")
	m, cmd := stepCmd(t, m, keyEnter())
	if len(m.pendingSkills) != 0 {
		t.Errorf("/continue did not consume the chips: %v", m.pendingSkills)
	}
	drainCmd(t, m, cmd)
	if len(eng.submitted) != 1 || !reflect.DeepEqual(eng.submitted[0].SkillIDs, []string{"review"}) {
		t.Fatalf("/continue did not carry the attached skill: %+v", eng.submitted)
	}
}

// ----------------------------------------------------------------------------
// nil-catalog guard
// ----------------------------------------------------------------------------

func TestNilCatalogGuards(t *testing.T) {
	m := newTestModelEng(t, &fakeEngine{}, testOpts) // testOpts has no Skills

	// The picker offers nothing rather than panicking.
	m.input.SetValue("/skill ")
	if ac := m.computeAutocomplete(); ac.active {
		t.Errorf("skill picker active with a nil catalog: %+v", ac.items)
	}
	// A chip with an unresolvable id falls back to the raw id, no panic.
	m.pendingSkills = []string{"ghost"}
	if got := plain(m.View()); !strings.Contains(got, "ghost") {
		t.Errorf("chip with nil catalog did not fall back to the raw id:\n%s", got)
	}
}
