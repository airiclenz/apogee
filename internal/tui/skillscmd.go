package tui

import (
	"fmt"
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/skills"
)

// ----------------------------------------------------------------------------
// /skills — the verb's argument grammar and its export form (skills.go owns the listing)
// ----------------------------------------------------------------------------
//
// The listing half of /skills predates this file and stays in skills.go, where the catalog report
// is built. What lives here is the grammar that lets the verb take an argument at all, and the one
// form that argument enables: `export`, which writes a copy of a SHIPPED skill into the user's
// global library. The shipped four are compiled into the binary and never installed (ADR 0065 §2),
// so — exactly as with the built-in colour schemes (ADR 0040 §1) — an export is the only way to get
// one into an editor, and the copy shadows the shipped original from the next scan onwards
// (ADR 0032).
//
// The export writes a FILE rather than a config key, which is what makes it the one idle-only form
// of an otherwise mid-run verb: the listing still answers while a worker works, and only the write
// waits for a quiescent engine ([parsedInput.safeWhileRunning], the /confine nuance one verb over).

// skillsSource labels the error entries this verb writes, the way colorSchemeSource does for the
// palette verb: an export that was refused is a failed ACT and reads as one, where the listing is a
// report that never fails.
const skillsSource = "skills"

// skillsAction is the subcommand of a parsed /skills line. The zero value is skillsList, so a bare
// "/skills" — and a line the parser never ran this grammar over, which is what the completion
// menu's accept builds — reports the catalog rather than writing anything (the /confine posture).
type skillsAction int

const (
	skillsList   skillsAction = iota // report the catalog: what loaded, what did not, what is shadowed
	skillsExport                     // write an editable copy of a shipped skill into the global library
)

// skillsArgs is the parsed argument list of a /skills line: what was asked for, and the skill id it
// was asked of (empty for the bare listing, which names nothing).
type skillsArgs struct {
	action skillsAction
	id     string
}

// skillsUsage is the one-line grammar every /skills argument error carries, so a mistyped line
// teaches the syntax instead of quietly reporting the catalog as though nothing had been asked.
const skillsUsage = "usage: /skills | /skills export <id>"

// parseSkills parses the argument tokens that followed a "/skills" verb. No arguments means the
// listing. "export" is the one reserved first token, and it takes exactly one id.
//
// Everything else is an error carrying skillsUsage rather than a guess. Unlike /color-scheme, a
// lone token is NOT a name to act on: there is nothing to switch to here, so "/skills debugging"
// is a mistyped subcommand and reporting the whole catalog for it would look like the id had been
// understood.
func parseSkills(args []string) (skillsArgs, error) {
	switch {
	case len(args) == 0:
		return skillsArgs{action: skillsList}, nil
	case args[0] == "export":
		if len(args) != 2 {
			return skillsArgs{}, fmt.Errorf("/skills export takes exactly one skill id. %s", skillsUsage)
		}
		return skillsArgs{action: skillsExport, id: args[1]}, nil
	default:
		return skillsArgs{}, fmt.Errorf("unknown /skills subcommand %q. %s", args[0], skillsUsage)
	}
}

// runSkillsCommand routes a parsed /skills line: report the catalog, or export a shipped skill.
// The listing returns whatever Cmd its re-scan needs (skills.go); the export is synchronous and
// always returns nil.
func (m Model) runSkillsCommand(args skillsArgs) (tea.Model, tea.Cmd) {
	if args.action == skillsExport {
		return m.exportShippedSkill(args.id)
	}
	return m.runSkills()
}

// exportShippedSkill writes a copy of one shipped skill into <ConfigHome>/skills through
// [skills.ExportShipped]. Success names the folder written and what to do with it; refusal — an
// unknown id, a folder already there, an unwritable library — is an error entry, because nothing
// happened and a note would read like something had (exportColorScheme's posture).
//
// The library root is composed here from [Options.ConfigHome], the same home the empty-catalog
// note already names as where discovery looked, so an export and that note can never point at
// different folders. An unwired home has no library to write into and says so.
func (m Model) exportShippedSkill(id string) (tea.Model, tea.Cmd) {
	if m.opts.ConfigHome == "" {
		m.transcript.addError(skillsSource, noSkillExporterNote, runRef{})
		m.layout()
		return m, nil
	}
	dir, err := skills.ExportShipped(id, filepath.Join(m.opts.ConfigHome, "skills"))
	if err != nil {
		m.transcript.addError(skillsSource, err.Error(), runRef{})
		m.layout()
		return m, nil
	}
	m.transcript.addNote(skillExportedNote(id, dir))
	m.layout()
	return m, nil
}

// noSkillExporterNote is the unwired-home degrade of the one form that writes a FILE: without a
// resolved apogee home there is no library folder to write into, and saying so beats writing a
// skill folder into whatever directory the process happens to be standing in.
const noSkillExporterNote = "this build cannot write skill files (no apogee home is resolved)"

// skillExportedNote confirms an export by naming the folder it wrote and what the copy now means,
// because the copy changes the id's meaning the moment the catalog is next scanned: a library skill
// outranks the shipped one it was copied from (ADR 0032), so "/id" resolves to the editable copy
// from here on — which is the whole point, and also the thing a human must know before they edit it.
func skillExportedNote(id, dir string) string {
	return fmt.Sprintf("skill %q written to %s", id, dir) + "\n" +
		fmt.Sprintf("  edit it — /%s now resolves to your copy, which shadows the shipped one", id)
}
