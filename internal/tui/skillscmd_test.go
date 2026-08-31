package tui

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/skills"
)

// ----------------------------------------------------------------------------
// /skills — the argument grammar
// ----------------------------------------------------------------------------

// The grammar in one table. The bare form is the zero value, which is what lets a line the hook
// never ran for — the completion menu's accept — report the catalog; every malformed form carries
// the usage line rather than guessing at a subcommand.
func TestParseSkills(t *testing.T) {
	t.Parallel()

	cases := []struct {
		line    string
		want    skillsArgs
		wantErr bool
	}{
		{line: "", want: skillsArgs{action: skillsList}},
		{line: "export debugging", want: skillsArgs{action: skillsExport, id: "debugging"}},
		{line: "export", wantErr: true},
		{line: "export a b", wantErr: true},
		{line: "debugging", wantErr: true},
		{line: "list", wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.line, func(t *testing.T) {
			got, err := parseSkills(strings.Fields(c.line))
			if (err != nil) != c.wantErr {
				t.Fatalf("parseSkills(%q) error = %v, want error: %v", c.line, err, c.wantErr)
			}
			if err != nil {
				if !strings.Contains(err.Error(), skillsUsage) {
					t.Errorf("the refusal %q does not carry the usage line", err)
				}
				return
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("parseSkills(%q) = %+v, want %+v", c.line, got, c.want)
			}
		})
	}
}

// The zero value of the grammar IS the listing, which is the contract every reader leans on: the
// menu's accept builds a parsedInput with no verbArgs at all, and safeWhileRunning asks the same
// value whether the hook ran or not.
func TestSkillsArgsZeroValueIsTheListing(t *testing.T) {
	t.Parallel()

	if (skillsArgs{}).action != skillsList {
		t.Error("the zero skillsArgs is not the listing; a line the hook never ran for would try to write")
	}
	if got := verbArgsOf[skillsArgs](parsedInput{kind: kindCommand, command: "skills"}); got.action != skillsList {
		t.Errorf("verbArgsOf on an unparsed /skills line = %+v, want the listing", got)
	}
}

// ----------------------------------------------------------------------------
// The verb from the completion menu
// ----------------------------------------------------------------------------

// /skills takes an argument now, but its bare form is the whole verb, so the menu still RUNS it
// rather than leaving "/skills " standing in the box for an argument nobody meant to type
// (runsBareAtAccept). The regression this pins is the flag being forgotten when takesArgs was added.
func TestAcceptSkillsRunsBareInsteadOfSplicing(t *testing.T) {
	m := newTestModelEng(t, &fakeEngine{}, skillOpts())
	m.input.SetValue("/skil")
	m.autocomplete = m.computeAutocomplete(m.caretByteOffset())
	m, cmd := stepCmd(t, m, keyTab())

	if got := m.input.Value(); got != "" {
		t.Errorf("input = %q after accepting /skills, want it cut out — the verb RAN", got)
	}
	m = runCmd(t, m, cmd) // the re-scan rides a Cmd; the listing lands on its message
	if last := lastEntry(t, m); last.kind != entryNote || !strings.Contains(last.text, "skills available") {
		t.Errorf("accepting /skills wrote %v %q, want the catalog listing", last.kind, last.text)
	}
}

// ----------------------------------------------------------------------------
// /skills export — writing a copy of a shipped skill
// ----------------------------------------------------------------------------

// exportOpts is testOpts with a catalog and an apogee home the export can write its library into.
func exportOpts(t *testing.T) Options {
	t.Helper()
	o := skillOpts()
	o.ConfigHome = t.TempDir()
	return o
}

// The export writes the shipped skill's whole folder into <home>/skills/<id>, and the note names
// the folder AND what the copy now means: a library skill outranks the shipped one it was copied
// from, so "/id" resolves to the editable copy from the next scan onwards.
func TestSkillsExportWritesTheShippedFolder(t *testing.T) {
	opts := exportOpts(t)
	m := newTestModelEng(t, &fakeEngine{}, opts)
	m, cmd := typeCommand(t, m, "/skills export debugging")

	if m.state != stateIdle {
		t.Errorf("state = %v after the export, want idle — it opens no worker", m.state)
	}
	if cmd != nil {
		t.Error("the export dispatched a Cmd; it writes one folder and reports")
	}
	dir := filepath.Join(opts.ConfigHome, "skills", "debugging")
	if _, err := os.Stat(filepath.Join(dir, "SKILL.md")); err != nil {
		t.Fatalf("the export wrote no SKILL.md: %v", err)
	}
	note := lastEntry(t, m)
	if note.kind != entryNote {
		t.Fatalf("the export wrote a %v entry, want a note: %q", note.kind, note.text)
	}
	for _, want := range []string{dir, "/debugging"} {
		if !strings.Contains(note.text, want) {
			t.Errorf("the export note is missing %q:\n%s", want, note.text)
		}
	}
}

// A second export is refused rather than allowed to replace a copy somebody has been editing, and
// the refusal reaches the transcript as an ERROR — nothing happened, and a note would read like it
// had.
func TestSkillsExportRefusesASecondCopy(t *testing.T) {
	opts := exportOpts(t)
	m := newTestModelEng(t, &fakeEngine{}, opts)
	m, _ = typeCommand(t, m, "/skills export debugging")
	m, _ = typeCommand(t, m, "/skills export debugging")

	last := lastEntry(t, m)
	if last.kind != entryError {
		t.Fatalf("the refused export wrote a %v entry, want an error: %q", last.kind, last.text)
	}
	if !strings.Contains(last.text, "already exists") || !strings.Contains(last.text, skillsSource) {
		t.Errorf("the error entry = %q, want it to name the verb and the reason", last.text)
	}
}

// An id nothing ships is refused, and the refusal lists what the binary does carry — the id was
// typed by a human, so the vocabulary is the useful half of the answer.
func TestSkillsExportRefusesANonShippedID(t *testing.T) {
	opts := exportOpts(t)
	m := newTestModelEng(t, &fakeEngine{}, opts)
	m, _ = typeCommand(t, m, "/skills export clean-code") // in the catalog, but not shipped

	last := lastEntry(t, m)
	if last.kind != entryError {
		t.Fatalf("the refused export wrote a %v entry, want an error: %q", last.kind, last.text)
	}
	for _, want := range append([]string{"clean-code"}, skills.ShippedIDs()...) {
		if !strings.Contains(last.text, want) {
			t.Errorf("the refusal %q does not name %q", last.text, want)
		}
	}
	if _, err := os.Stat(filepath.Join(opts.ConfigHome, "skills")); err == nil {
		t.Error("a refused export created the library directory anyway")
	}
}

// A malformed line reports the usage and writes nothing — the /confine posture, and it matters here
// because the form that WOULD have run creates a folder.
func TestSkillsCommandReportsItsUsageOnABadLine(t *testing.T) {
	opts := exportOpts(t)
	m := newTestModelEng(t, &fakeEngine{}, opts)
	m, cmd := typeCommand(t, m, "/skills export")

	if cmd != nil {
		t.Error("a line that did not parse dispatched something")
	}
	if got := lastEntry(t, m).text; !strings.Contains(got, skillsUsage) {
		t.Errorf("the report = %q, want the usage line", got)
	}
	if _, err := os.Stat(filepath.Join(opts.ConfigHome, "skills")); err == nil {
		t.Error("a line that did not parse created the library directory")
	}
}

// Without a resolved apogee home there is no library to write into, and saying so beats writing a
// skill folder into whatever directory the process happens to be standing in.
func TestSkillsExportWithNoResolvedHome(t *testing.T) {
	opts := skillOpts() // ConfigHome unset
	m := newTestModelEng(t, &fakeEngine{}, opts)
	m, _ = typeCommand(t, m, "/skills export debugging")

	last := lastEntry(t, m)
	if last.kind != entryError || !strings.Contains(last.text, noSkillExporterNote) {
		t.Errorf("the unwired export wrote %v %q, want an error carrying %q", last.kind, last.text, noSkillExporterNote)
	}
}

// ----------------------------------------------------------------------------
// The mid-run split: the listing answers, the export waits
// ----------------------------------------------------------------------------

// safeWhileRunning is asked about the parsed LINE for /skills exactly as it is for /confine: the
// listing only reads, the export writes a folder.
func TestSkillsLineDecidesWhatRunsMidRun(t *testing.T) {
	t.Parallel()

	cases := []struct {
		line string
		want bool
	}{
		{"/skills", true},
		{"/skills export debugging", false},
		{"/skills bogus", true}, // a usage-line report, not a write (parseSkills' error path)
	}
	for _, c := range cases {
		if got := parseInput(c.line, nil).safeWhileRunning(); got != c.want {
			t.Errorf("safeWhileRunning(%q) = %v, want %v", c.line, got, c.want)
		}
	}
}

// The export mutates the workspace's library, so a line typed while the model works earns the
// standing answer instead of running — while the bare listing keeps answering, which is the whole
// point of splitting the policy on the line (TestSkillsListingStillAnswersMidRun below).
func TestSkillsExportIsRefusedWhileTheModelWorks(t *testing.T) {
	opts := exportOpts(t)
	m := newTestModelEng(t, &fakeEngine{}, opts)
	m, _ = typeCommand(t, m, "open the exchange")
	if m.state != stateRunning {
		t.Fatalf("precondition: state = %v, want running", m.state)
	}

	m, _ = typeCommand(t, m, "/skills export debugging")

	if _, err := os.Stat(filepath.Join(opts.ConfigHome, "skills", "debugging")); err == nil {
		t.Error("the export ran mid-run; it writes into the library the engine's own run may be reading")
	}
	if got := plain(m.View()); !strings.Contains(got, commandsAtIdleNote) {
		t.Errorf("the refusal note is missing from the transcript:\n%s", got)
	}
}

// The listing is unchanged by the grammar: it still answers while a worker works, because it only
// reads from disk. This is the half the export must not have taken down with it.
func TestSkillsListingStillAnswersMidRun(t *testing.T) {
	m := newTestModelEng(t, &fakeEngine{}, skillOpts())
	m, _ = typeCommand(t, m, "open the exchange")
	if m.state != stateRunning {
		t.Fatalf("precondition: state = %v, want running", m.state)
	}

	m, cmd := typeCommand(t, m, "/skills")
	m = runCmd(t, m, cmd)

	last := lastEntry(t, m)
	if last.kind != entryNote || !strings.Contains(last.text, "skills available") {
		t.Errorf("the mid-run listing wrote %v %q, want the catalog", last.kind, last.text)
	}
}

// ----------------------------------------------------------------------------
// The listing keeps its source labels
// ----------------------------------------------------------------------------

// A shipped skill is labelled as one in the report — the field a SKILL.md does not author, and the
// only thing on the row that says an entry came out of the binary rather than out of a cloned repo.
func TestSkillsListingLabelsAShippedSkill(t *testing.T) {
	o := testOpts
	o.Skills = fakeSkillCatalog{skills: []skills.Skill{
		{ID: "debugging", DisplayName: "Debugging", Summary: "find the fault",
			Body: "DEBUG", Dir: skills.ShippedMountPrefix + "debugging"},
	}}
	note := runSkillsNote(t, newTestModelEng(t, &fakeEngine{}, o))

	if want := "/debugging" + skillSourceSep + skillSourceShipped; !strings.Contains(note, want) {
		t.Errorf("the listing is missing %q:\n%s", want, note)
	}
}
