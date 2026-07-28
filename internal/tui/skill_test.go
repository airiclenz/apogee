package tui

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

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
	m.input.SetValue("/sk") // "sessions" also begins with s, so narrow to "sk" for the skill verbs
	ac := m.computeAutocomplete()
	if !ac.active || ac.kind != acCommand {
		t.Fatalf("overlay = {active:%v kind:%v}, want active command", ac.active, ac.kind)
	}
	// /skill BEFORE /skills: the first row is the highlighted one, so a typed "/skill" must
	// complete into the picker rather than into the listing that shares its prefix.
	var got []string
	for _, it := range ac.items {
		got = append(got, it.value)
	}
	if !reflect.DeepEqual(got, []string{"skill", "skills"}) {
		t.Fatalf("'/sk' suggestions = %v, want [skill skills] in that order", got)
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
// Accept: splice the skill's inline /token; the /skill command chains into the picker
// ----------------------------------------------------------------------------

// The picker no longer pops a chip: accepting a row REPLACES the "/skill <partial>" run with the
// skill's own inline "/id " token, which is what the submit parse reads back out.
func TestAcceptSkillSplicesInlineToken(t *testing.T) {
	m := newTestModelEng(t, &fakeEngine{}, skillOpts())
	m.input.SetValue("fix /skill cl")
	m.autocomplete = m.computeAutocomplete() // acSkill, [clean-code]
	m = step(t, m, keyTab())

	if got, want := m.input.Value(), "fix /clean-code "; got != want {
		t.Errorf("after accept input = %q, want the picker run replaced by %q", got, want)
	}
	if m.autocomplete.active {
		t.Error("overlay still open after the splice")
	}
	if got := m.promptEditor.submitParse(m.knownSkillID).skillIDs; !reflect.DeepEqual(got, []string{"clean-code"}) {
		t.Errorf("spliced token parses to skillIDs = %v, want [clean-code]", got)
	}
	// No chip strip renders anywhere any more — the token in the box IS the attachment.
	if got := plain(m.View()); strings.Contains(got, "Clean Code") {
		t.Errorf("a chip strip is still rendered above the box:\n%s", got)
	}
}

// The picker excludes a skill the message already invokes — read off the tokens standing in the
// buffer, since there is no attachment state beside it.
func TestSkillPickerExcludesTokensAlreadyInTheBuffer(t *testing.T) {
	m := newTestModelEng(t, &fakeEngine{}, skillOpts())
	m.input.SetValue("/clean-code /skill cl")
	if ac := m.computeAutocomplete(); ac.active {
		t.Errorf("a skill already invoked in the text is still offered: %+v", ac.items)
	}
	// Delete the token and it is offered again — the exclusion self-heals with the text.
	m.input.SetValue("/skill cl")
	if ac := m.computeAutocomplete(); !ac.active || ac.items[0].value != "clean-code" {
		t.Errorf("removing the token did not restore the suggestion: %+v", ac)
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
// The merged "/" menu: one namespace, commands first, skills marked
// ----------------------------------------------------------------------------

// One "/" menu, two kinds of row: the matching commands first (a verb ACTS on the session), then
// the matching skills, marked with the transcript's own skill glyph and shown as the token they
// write.
func TestSlashMenuMergesCommandsAndSkills(t *testing.T) {
	m := newTestModelEng(t, &fakeEngine{}, skillOpts())
	m.input.SetValue("/c") // four c-commands, and "clean-code" matches as a substring
	ac := m.computeAutocomplete()

	var got []string
	for _, it := range ac.items {
		got = append(got, it.value)
	}
	want := []string{"clear", "compact", "continue", "confine", "clean-code"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("merged rows = %v, want the commands before the skills %v", got, want)
	}
	row := ac.items[len(ac.items)-1]
	if !row.skill {
		t.Error("the skill row is not marked as a skill; accept would treat it as a command")
	}
	if !strings.Contains(row.label, glyphSkill) || !strings.Contains(row.label, "/clean-code") {
		t.Errorf("skill row label = %q, want the %q marker and the /id token it writes", row.label, glyphSkill)
	}
}

// Accepting a skill row writes its inline token at the point the human was typing — the same
// "/id " the picker splices, and the same thing the submit parse reads back out.
func TestAcceptSkillRowFromTheMergedMenu(t *testing.T) {
	m := newTestModelEng(t, &fakeEngine{}, skillOpts())
	m.input.SetValue("please /rev")
	m.autocomplete = m.computeAutocomplete()
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

// A skill whose id collides with a command verb is omitted from the merged rows — the verb owns the
// name in one namespace — and stays reachable through the /skill picker, whose splice writes the
// token in a position the whole-input command rule never claims.
func TestSlashMenuShadowsCollidingSkillID(t *testing.T) {
	o := testOpts
	o.Skills = fakeSkillCatalog{skills: []skills.Skill{{ID: "clear", DisplayName: "Clear Code"}}}
	m := newTestModelEng(t, &fakeEngine{}, o)

	m.input.SetValue("/clea")
	for _, it := range m.computeAutocomplete().items {
		if it.skill {
			t.Fatalf("the merged menu offered a skill a command verb shadows: %+v", it)
		}
	}

	m.input.SetValue("/skill clea")
	ac := m.computeAutocomplete()
	if ac.kind != acSkill || len(ac.items) != 1 || ac.items[0].value != "clear" {
		t.Fatalf("the /skill picker lost the shadowed skill: %+v", ac)
	}
}

// A fully typed skill token keeps its own row — the token being completed is not yet "already
// invoked" — and ⏎ then SENDS the message it stands in rather than re-completing it.
func TestTypedSkillTokenStaysOfferedAndSubmits(t *testing.T) {
	eng := &fakeEngine{stepFn: scriptedSteps()}
	m := newTestModelEng(t, eng, skillOpts())
	m.input.SetValue("please /review")
	m.autocomplete = m.computeAutocomplete()

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
	if got := plain(m.View()); !strings.Contains(got, "Clean Code") {
		t.Errorf("transcript should chip the invoked skill:\n%s", got)
	}
}

// A message sent with text and a skill token keeps the skill visible on its user block after the
// send (ISSUES #5: the attachment used to vanish once the input cleared).
func TestSentUserBlockShowsSkillChipsWithText(t *testing.T) {
	eng := &fakeEngine{stepFn: scriptedSteps()}
	m := newTestModelEng(t, eng, skillOpts())
	m.input.SetValue("/clean-code fix the parser")
	m, cmd := stepCmd(t, m, keyEnter())
	drainCmd(t, m, cmd)

	got := plain(m.View())
	if !strings.Contains(got, "fix the parser") {
		t.Errorf("sent text missing from the transcript:\n%s", got)
	}
	if !strings.Contains(got, "Clean Code") {
		t.Errorf("invoked skill not shown on the sent user block (ISSUES #5):\n%s", got)
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

	// The picker offers nothing rather than panicking.
	m.input.SetValue("/skill ")
	if ac := m.computeAutocomplete(); ac.active {
		t.Errorf("skill picker active with a nil catalog: %+v", ac.items)
	}
	// No token resolves without a catalog, so the message is ordinary prose — no panic.
	m.input.SetValue("/ghost do it")
	if got := m.promptEditor.submitParse(m.knownSkillID); len(got.skillIDs) != 0 {
		t.Errorf("skillIDs = %v with a nil catalog, want none", got.skillIDs)
	}
	// A chip with an unresolvable id still falls back to the raw id on the sent block.
	if names := m.skillDisplayNames([]string{"ghost"}); !reflect.DeepEqual(names, []string{"ghost"}) {
		t.Errorf("skillDisplayNames = %v, want the raw id as the fallback", names)
	}
}

// ----------------------------------------------------------------------------
// Live refresh: the picker reloads the catalog when it opens
// ----------------------------------------------------------------------------

// reloadableCatalog is a SkillCatalog whose List reads through a pointer, so a ReloadSkills
// closure that mutates the same backing slice is reflected on the next List — modelling the
// shared skills.Provider whose Reload swaps in a fresh catalog both the picker and loop read.
type reloadableCatalog struct {
	skills *[]skills.Skill
}

func (f reloadableCatalog) List() []skills.Skill { return *f.skills }

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
// many reloads fired and what the picker then shows.
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

// Typing "/skill " open (the manual path) re-scans the catalog exactly once, and the picker then
// lists the skill the reload discovered — the live-refresh the user asked for.
func TestSkillPickerReloadsOnOpenByTyping(t *testing.T) {
	o, reloads := reloadOpts()
	m := newTestModelEng(t, &fakeEngine{}, o)

	m.input.SetValue("/skill")   // bare command, not yet a /skill region
	m = step(t, m, keyRune(' ')) // the space opens the picker → one reload
	if *reloads != 1 {
		t.Fatalf("opening the /skill picker triggered %d reloads, want exactly 1", *reloads)
	}
	if m.autocomplete.kind != acSkill || !m.autocomplete.active {
		t.Fatalf("picker not open after '/skill ': %+v", m.autocomplete)
	}
	var got []string
	for _, it := range m.autocomplete.items {
		got = append(got, it.value)
	}
	if !containsString(got, "fresh") {
		t.Errorf("picker did not show the skill the reload added: %v", got)
	}

	// Typing further inside the already-open picker must NOT re-scan disk (edge-triggered on open).
	m = step(t, m, keyRune('f'))
	if *reloads != 1 {
		t.Errorf("typing inside the open picker re-scanned: %d reloads, want it to stay 1", *reloads)
	}
}

// Selecting /skill from the "/" command menu chains into the picker and reloads once too.
func TestSkillPickerReloadsViaCommandChain(t *testing.T) {
	o, reloads := reloadOpts()
	m := newTestModelEng(t, &fakeEngine{}, o)

	m.input.SetValue("/skill")
	m.autocomplete = m.computeAutocomplete() // command menu, "skill" highlighted
	m = step(t, m, keyTab())                 // accept → acceptAutocomplete splices "/skill " and opens the picker
	if *reloads != 1 {
		t.Fatalf("the /skill command chain triggered %d reloads, want exactly 1", *reloads)
	}
	if m.autocomplete.kind != acSkill || !m.autocomplete.active {
		t.Fatalf("command chain did not open the picker: %+v", m.autocomplete)
	}
}

// Leaving and re-opening the picker reloads again (each invocation re-reads), and a nil
// ReloadSkills is simply a no-op (no panic) — the existing catalog stays as loaded.
func TestSkillPickerReloadNilSafe(t *testing.T) {
	o := skillOpts() // has a catalog but no ReloadSkills
	m := newTestModelEng(t, &fakeEngine{}, o)
	m.input.SetValue("/skill")
	m = step(t, m, keyRune(' ')) // must not panic with a nil ReloadSkills
	if m.autocomplete.kind != acSkill {
		t.Fatalf("picker did not open with a nil ReloadSkills: %+v", m.autocomplete)
	}
}

// ----------------------------------------------------------------------------
// /skills — listing the catalog
// ----------------------------------------------------------------------------

// runSkillsNote runs "/skills" from idle and returns the note it recorded, asserting the verb
// stayed local: no worker, no Cmd, and the input emptied like every other command.
func runSkillsNote(t *testing.T, m Model) string {
	t.Helper()
	m.input.SetValue("/skills")
	m, cmd := stepCmd(t, m, keyEnter())
	if cmd != nil {
		t.Error("/skills returned a Cmd; it is a local report and must not launch a worker")
	}
	if m.state != stateIdle {
		t.Errorf("state = %v after /skills, want idle", m.state)
	}
	if v := m.input.Value(); v != "" {
		t.Errorf("input not cleared after /skills: %q", v)
	}
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
	// Catalog order is display order: the listing reads like the picker.
	if strings.Index(note, "/clean-code") > strings.Index(note, "/review") {
		t.Errorf("rows are not in catalog order:\n%s", note)
	}
}

// The catalog is re-scanned BEFORE it is listed, so a skill added since launch shows up — the
// same live refresh the picker edge-triggers on open. The reload stub appends "fresh", so the
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

// With no catalog wired (and no ReloadSkills) the verb still answers — no panic — and the note
// says where discovery looks, so "no skills" is actionable rather than a dead end.
func TestSkillsCommandWithNoCatalog(t *testing.T) {
	o := testOpts // no Skills, no ReloadSkills
	o.Workspace = filepath.Join("home", "code", "proj")
	m := newTestModelEng(t, &fakeEngine{}, o)
	note := runSkillsNote(t, m)

	if !strings.Contains(note, "no skills found") {
		t.Errorf("note does not report an empty catalog:\n%s", note)
	}
	for _, want := range []string{
		filepath.Join("~", ".apogee", "skills"),
		filepath.Join("home", "code", "proj", ".apogee", "skills"),
		filepath.Join("home", "code", "proj", "skills"),
	} {
		if !strings.Contains(note, want) {
			t.Errorf("note does not name the source dir %q:\n%s", want, note)
		}
	}
}

// skillCatalogNote is pure, so its wording is pinned without a Model: the singular header, a
// skill with no summary, and the placeholder that stands in for an unwired workspace.
func TestSkillCatalogNote(t *testing.T) {
	one := skillCatalogNote([]skills.Skill{{ID: "review", DisplayName: "Review"}}, "/ws")
	if !strings.HasPrefix(one, "1 skill available:") {
		t.Errorf("singular header wrong:\n%s", one)
	}
	if !strings.Contains(one, "/review  Review") || strings.Contains(one, "—") {
		t.Errorf("a summary-less skill must render without the dash:\n%s", one)
	}
	empty := skillCatalogNote(nil, "")
	if !strings.Contains(empty, "<workspace>") {
		t.Errorf("an unwired workspace must render the placeholder:\n%s", empty)
	}
}
