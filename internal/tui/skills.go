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
	if m.opts.Skills != nil { // nil ⇒ no catalog is wired; the empty note answers that too
		list = m.opts.Skills.List()
	}
	m.transcript.addNote(skillCatalogNote(list, m.opts.ConfigHome, m.opts.Workspace))
	m.layout()
	return m, nil
}

// skillCatalogNote renders the /skills report: a header naming how many skills are loaded, then
// one line per skill — the /id that names it, its display name, and its summary. list arrives in
// the catalog's own order (sorted by display name), which is the order the picker shows.
//
// An empty catalog is not a failure — most users start with none — so instead of an apologetic
// blank the note says where discovery looked, in the layered order sourceDirs walks
// (skills/load.go): the answer to the question a user staring at "no skills" is about to ask.
// Both roots are INJECTED rather than assumed, for the same reason the loader's Sources are
// (ADR 0001): home is the apogee home this run resolved — `--config` / APOGEE_CONFIG move it, and
// naming `~/.apogee` at a run that is not using it would send the human to the wrong folder — and
// workspace is the project root the two project-local dirs hang off. An empty root renders its
// spelling/placeholder rather than a bogus relative path.
func skillCatalogNote(list []skills.Skill, home, workspace string) string {
	if len(list) == 0 {
		lib := home
		if lib == "" {
			lib = filepath.Join("~", ".apogee")
		}
		ws := workspace
		if ws == "" {
			ws = "<workspace>"
		}
		return strings.Join([]string{
			"no skills found — a skill is a folder holding a SKILL.md, discovered under:",
			"  " + filepath.Join(lib, "skills") + "  (your global library)",
			"  " + filepath.Join(ws, ".apogee", "skills"),
			"  " + filepath.Join(ws, "skills") + "  (only when use-project-skills is on)",
		}, "\n")
	}
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
	return strings.Join(lines, "\n")
}
