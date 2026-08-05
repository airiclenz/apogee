package tui

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/skills"
)

// ----------------------------------------------------------------------------
// /skills — listing the catalog (command.go owns the verb, autocomplete.go the merged "/" menu)
// ----------------------------------------------------------------------------
//
// The browsing half of the skill UX: the merged "/" menu picks one, /skills shows what there is
// to pick from — the answer to "what can I invoke?", which until now could only be discovered
// by opening the menu and scrolling it. It is a read-only report: no engine call, no worker,
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
// transcript note. The re-scan is the same live refresh the merged "/" menu edge-triggers on open
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

// skillCatalogNote renders the /skills report from one scan's halves: the skills that loaded, the
// SKILL.md files that could not be loaded, and the ones that loaded but lost an id collision. list
// arrives in the catalog's own order (sorted by display name), the order the merged "/" menu takes;
// skipped arrives in discovery order and is partitioned here on its cause.
//
// A skip is reported even when NOTHING loaded: "no skills found" would be a lie when discovery
// found a skill and refused it, and the case where the library's only skill is the broken one is
// exactly when the human most needs the reason. Only a genuinely empty scan — nothing loaded,
// nothing refused — gets the where-we-looked note.
func skillCatalogNote(list []skills.Skill, skipped []skills.SkipError, home, workspace string) string {
	if len(list) == 0 && len(skipped) == 0 {
		return strings.Join(emptyCatalogLines(home, workspace), "\n")
	}
	failed, shadowed := partitionSkips(skipped)
	var lines []string
	for _, section := range [][]string{
		loadedSkillLines(list),
		failedSkillLines(failed),
		shadowedSkillLines(shadowed),
	} {
		if len(section) == 0 {
			continue
		}
		if len(lines) > 0 {
			lines = append(lines, "") // a blank line so each half reads as its own section
		}
		lines = append(lines, section...)
	}
	return strings.Join(lines, "\n")
}

// emptyCatalogLines answers an empty scan. An empty catalog is not a failure — most users start
// with none — so instead of an apologetic blank the note says where discovery looked, in the
// layered order sourceDirs walks (skills/load.go) — increasing priority, so the LAST line is the
// one that wins an id clash (ADR 0032, which put the global library there): the question a user
// staring at "no skills" is about to ask. Both roots are INJECTED rather than assumed, for the
// same reason the loader's
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
		"  " + filepath.Join(ws, ".apogee", "skills"),
		"  " + filepath.Join(ws, "skills") + "  (only when use-project-skills is on)",
		"  " + filepath.Join(lib, "skills") + "  (your global library — wins an id clash)",
	}
}

// loadedSkillLines renders the working half: one line per skill — the /id that names it, its
// display name, and its summary. Nothing loaded renders nothing, so the caller's section joiner
// never emits a "0 skills available:" header over an empty list.
func loadedSkillLines(list []skills.Skill) []string {
	if len(list) == 0 {
		return nil
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
	return lines
}

// shadowedBy reports the winning SKILL.md's path when this skip is a lost id collision rather
// than a load failure (skills.ShadowedError, reached through the skip's cause). It is the one
// place the report asks "which kind of skip is this?", so the partition and the rendering can
// never disagree about it.
func shadowedBy(sk skills.SkipError) (string, bool) {
	var shadow skills.ShadowedError
	if errors.As(sk, &shadow) {
		return shadow.By, true
	}
	return "", false
}

// partitionSkips splits one scan's skips on their cause: the SKILL.md files that could not be
// turned into a skill, and the ones that parsed fine and simply lost an id collision (ADR 0032).
// They travel down the same channel but are not the same news — a failure is a file to go and fix,
// a shadow is a file that is merely not the copy the id resolves to — so heading them both with
// "found but not loaded" would send the human hunting for a defect in a healthy file. Each half
// keeps discovery order.
func partitionSkips(skipped []skills.SkipError) (failed, shadowed []skills.SkipError) {
	for _, sk := range skipped {
		if _, ok := shadowedBy(sk); ok {
			shadowed = append(shadowed, sk)
			continue
		}
		failed = append(failed, sk)
	}
	return failed, shadowed
}

// failedSkillLines renders the failure half: every SKILL.md discovery found but could not load,
// named by the ID it WOULD have had, with the reason and the file to go and fix. Discovery skips
// a bad skill softly so one malformed file cannot sink the catalog; printing the skip here is
// what keeps soft from meaning silent — otherwise a broken skill and an absent one look
// identical from the merged "/" menu, with nowhere to look for the difference.
func failedSkillLines(failed []skills.SkipError) []string {
	if len(failed) == 0 {
		return nil
	}
	head := fmt.Sprintf("%d skills found but not loaded:", len(failed))
	if len(failed) == 1 {
		head = "1 skill found but not loaded:"
	}
	lines := make([]string, 0, 2*len(failed)+1)
	lines = append(lines, head)
	for _, sk := range failed {
		lines = append(lines, "  "+sk.Name()+" — "+sk.Reason())
		lines = append(lines, "    "+sk.Path)
	}
	return lines
}

// shadowedSkillLines renders the collision half: one pair per shadowed skill — the copy that lost,
// then the copy that is live. Both paths are named because that is the whole question a shadow
// raises ("which of my two files is /<id> actually running?"), and the answer is a file the user
// can open. Nothing here is broken, so the wording deliberately never says "not loaded".
func shadowedSkillLines(shadowed []skills.SkipError) []string {
	if len(shadowed) == 0 {
		return nil
	}
	head := fmt.Sprintf("%d skills shadowed by another of the same id:", len(shadowed))
	if len(shadowed) == 1 {
		head = "1 skill shadowed by another of the same id:"
	}
	lines := make([]string, 0, 2*len(shadowed)+1)
	lines = append(lines, head)
	for _, sk := range shadowed {
		by, _ := shadowedBy(sk) // partitioned on it above, so it is there
		lines = append(lines, "  "+sk.Name()+" — "+sk.Path)
		lines = append(lines, "    the live copy is "+by)
	}
	return lines
}
