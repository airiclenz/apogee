package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/skills"
)

// ----------------------------------------------------------------------------
// /skills — listing the catalog (command.go owns the verb, autocomplete.go the picker)
// ----------------------------------------------------------------------------
//
// The browsing half of the skill UX: /skill <partial> picks one, /skills shows what there is
// to pick from — the answer to "what can I invoke?", which until now could only be discovered
// by opening the picker and scrolling it. It is a read-only report: no engine call, no worker,
// nothing staged. The report builder below is pure, so the wording stays table-testable
// without a Model (the confine.go posture).

// knownSkillID reports whether id names a skill in the wired catalog. It is the predicate the pure
// parse layer resolves inline "/token" references against (parseInput → extractSkillRefs) and the
// one place the "is this word a skill?" question is answered, so the parser, the highlighter and
// the dropdown can never disagree about what resolves. A nil catalog knows nothing — every token is
// then plain prose, which is exactly right for a build with no skills wired.
func (m Model) knownSkillID(id string) bool {
	if m.opts.Skills == nil {
		return false
	}
	_, ok := m.opts.Skills.Get(id)
	return ok
}

// runSkills routes /skills: re-scan the skill source dirs, then record the catalog as one
// transcript note. The re-scan is the same live refresh the picker edge-triggers when it opens
// — ReloadSkills swaps the shared skills.Provider that both this listing and the agent loop
// read — so a skill added since launch is listed, and because the provider is shared, listed
// means resolvable. It never launches a worker, so it always returns a nil Cmd.
func (m Model) runSkills() (tea.Model, tea.Cmd) {
	if m.opts.ReloadSkills != nil {
		m.opts.ReloadSkills() // before the read below: the listing must show what is on disk NOW
	}
	var list []skills.Skill
	var skipped []skills.SkipError
	if m.opts.Skills != nil { // nil ⇒ no catalog is wired; the empty note answers that too
		list = m.opts.Skills.List()
		skipped = m.opts.Skills.Skipped()
	}
	m.transcript.addNote(skillCatalogNote(list, skipped, m.opts.ConfigHome, m.opts.Workspace))
	m.layout()
	return m, nil
}

// skillCatalogNote renders the /skills report from one scan's two halves: the skills that
// loaded and the SKILL.md files that did not. list arrives in the catalog's own order (sorted by
// display name), which is the order the picker shows; skipped arrives in discovery order.
//
// A skip is reported even when NOTHING loaded: "no skills found" would be a lie when discovery
// found a skill and refused it, and the case where the library's only skill is the broken one is
// exactly when the human most needs the reason. Only a genuinely empty scan — nothing loaded,
// nothing refused — gets the where-we-looked note.
func skillCatalogNote(list []skills.Skill, skipped []skills.SkipError, home, workspace string) string {
	if len(list) == 0 && len(skipped) == 0 {
		return strings.Join(emptyCatalogLines(home, workspace), "\n")
	}
	var lines []string
	if len(list) > 0 {
		lines = append(lines, loadedSkillLines(list)...)
	}
	if len(skipped) > 0 {
		if len(lines) > 0 {
			lines = append(lines, "") // a blank line so the failures read as their own section
		}
		lines = append(lines, skippedSkillLines(skipped)...)
	}
	return strings.Join(lines, "\n")
}

// emptyCatalogLines answers an empty scan. An empty catalog is not a failure — most users start
// with none — so instead of an apologetic blank the note says where discovery looked, in the
// layered order sourceDirs walks (skills/load.go): the question a user staring at "no skills" is
// about to ask. Both roots are INJECTED rather than assumed, for the same reason the loader's
// Sources are (ADR 0001): home is the apogee home this run resolved — `--config` /
// APOGEE_CONFIG move it, and naming `~/.apogee` at a run that is not using it would send the
// human to the wrong folder — and workspace is the project root the two project-local dirs hang
// off. An empty root renders its spelling/placeholder rather than a bogus relative path.
func emptyCatalogLines(home, workspace string) []string {
	lib := home
	if lib == "" {
		lib = filepath.Join("~", ".apogee")
	}
	ws := workspace
	if ws == "" {
		ws = "<workspace>"
	}
	return []string{
		"no skills found — a skill is a folder holding a SKILL.md, discovered under:",
		"  " + filepath.Join(lib, "skills") + "  (your global library)",
		"  " + filepath.Join(ws, ".apogee", "skills"),
		"  " + filepath.Join(ws, "skills") + "  (only when use-project-skills is on)",
	}
}

// loadedSkillLines renders the working half: one line per skill — the /id that names it, its
// display name, and its summary.
func loadedSkillLines(list []skills.Skill) []string {
	head := fmt.Sprintf("%d skills available:", len(list))
	if len(list) == 1 {
		head = "1 skill available:"
	}
	lines := make([]string, 0, len(list)+1)
	lines = append(lines, head)
	for _, sk := range list {
		line := "  /" + sk.ID
		if sk.DisplayName != "" {
			line += "  " + sk.DisplayName
		}
		if sk.Summary != "" {
			line += " — " + sk.Summary
		}
		lines = append(lines, line)
	}
	return lines
}

// skippedSkillLines renders the failure half: every SKILL.md discovery found but could not load,
// named by the ID it WOULD have had, with the reason and the file to go and fix. Discovery skips
// a bad skill softly so one malformed file cannot sink the catalog; printing the skip here is
// what keeps soft from meaning silent — otherwise a broken skill and an absent one look
// identical from the picker, with nowhere to look for the difference.
func skippedSkillLines(skipped []skills.SkipError) []string {
	head := fmt.Sprintf("%d skills found but not loaded:", len(skipped))
	if len(skipped) == 1 {
		head = "1 skill found but not loaded:"
	}
	lines := make([]string, 0, 2*len(skipped)+1)
	lines = append(lines, head)
	for _, sk := range skipped {
		lines = append(lines, "  "+sk.Name()+" — "+sk.Reason())
		lines = append(lines, "    "+sk.Path)
	}
	return lines
}
