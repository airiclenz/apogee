package tui

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"unicode"

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

// runSkills routes /skills: re-scan the skill source dirs, then record the catalog that scan
// installed as one transcript note. The re-scan is the same live refresh the merged "/" menu
// edge-triggers on open — ReloadSkills swaps the shared skills.Provider that both this listing and
// the agent loop read — so a skill added since launch is listed, and because the provider is
// shared, listed means resolvable.
//
// The walk is dispatched as a Cmd (skillRescanCmd) rather than run here, for the reason the menu's
// trigger already is: it is a full walk of the source dirs, and on this goroutine it stalls the
// render loop for the length of that walk (ADR 0011). A report the human asked for is no better a
// reason to freeze the frame than the keystroke that opens the menu was — and the freeze is longest
// exactly where the library is biggest. The listing therefore lands on skillsRescannedMsg, over the
// catalog the completed scan holds. It still launches no worker and touches no engine: the verb
// answers from disk alone, mid-run as readily as at idle.
//
// With no ReloadSkills wired there is nothing to walk, so the listing is a pure read of the catalog
// as loaded and lands on this loop like /version's note.
func (m Model) runSkills() (tea.Model, tea.Cmd) {
	if rescan := m.skillRescanCmd(skillsRescannedMsg{}); rescan != nil {
		return m, rescan
	}
	m.noteSkillCatalog()
	return m, nil
}

// skillsRescannedMsg reports that the catalog re-scan /skills dispatched has finished and the shared
// skills.Provider now holds the fresh snapshot. Like skillsReloadedMsg (autocomplete.go) it carries
// no payload — the catalog is read through [Options.Skills], so the message is the SIGNAL that the
// read is now worth doing rather than the result of it. The two stay separate types because they owe
// different repaints: the menu's fold re-derives the open dropdown, this one writes the listing the
// human asked the verb for.
type skillsRescannedMsg struct{}

// Compile-time assertion that the listing Msg is a valid tea.Msg (mirroring messages.go).
var _ tea.Msg = skillsRescannedMsg{}

// noteSkillCatalog reads the catalog as it now stands and records it as one transcript note — the
// /skills report itself. Both ways into the report go through it (the fold of a finished re-scan,
// model.go, and the inline read runSkills takes when no re-scan is wired), so the two can never come
// to report different things.
//
// Unlike the menu's fold (foldSkillsReloaded) it is owed unconditionally, whatever the frame has
// become since the verb ran: the note is scrollback rather than an overlay, so a scan landing late
// cannot paint over a decision surface, and dropping the report the human explicitly asked for would
// be the worse failure.
func (m *Model) noteSkillCatalog() {
	var list []skills.Skill
	var skipped []skills.SkipError
	if m.opts.Skills != nil { // nil ⇒ no catalog is wired; the empty note answers that too
		list = m.opts.Skills.List()
		skipped = m.opts.Skills.Skipped()
	}
	m.transcript.addNote(skillCatalogNote(list, skipped, m.opts.ConfigHome, m.opts.Workspace))
	m.layout()
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
//
// The two roots are what the report answers "where from?" with, in both directions: an empty scan
// names the dirs discovery LOOKED in, and a loaded skill is labelled with the one it CAME from
// (skillSource).
func skillCatalogNote(list []skills.Skill, skipped []skills.SkipError, home, workspace string) string {
	if len(list) == 0 && len(skipped) == 0 {
		return strings.Join(emptyCatalogLines(home, workspace), "\n")
	}
	failed, shadowed := partitionSkips(skipped)
	var lines []string
	for _, section := range [][]string{
		loadedSkillLines(list, home, workspace),
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
// layered order, in INCREASING precedence — the reverse of the order discovery now walks them in
// (skills/load.go's sourceAnchors) — so the LAST line is the one that wins an id clash (ADR 0032,
// which put the global library there): the question a user staring at "no skills" is about to ask. Both roots are INJECTED rather than assumed, for the
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

// ----------------------------------------------------------------------------
// Where a skill came from — the disclosure both surfaces render
// ----------------------------------------------------------------------------
//
// A loaded skill is bytes from one of three folders, and which one is the whole difference between
// an extension the human installed and one a cloned repo shipped itself. Until this, no surface
// said: the "/" menu and /skills both showed an id and a description, and a repo-supplied
// SKILL.md chooses BOTH of those — it may name itself "Confine" and describe itself as the verb it
// is impersonating, and (matching exactly) it sorts above the row it imitates (slashMatchRank).
// Nothing about the row disagreed with it. The source is the one field on a skill row the repo
// does NOT author, so it is the field that answers the deception.
//
// It is rendered LEFT of the description on purpose. The description is the untrusted half and it
// is long: put the source after it and a padded summary pushes the disclosure past the pane's
// right edge, where the truncation eats it — the mitigation would then be present exactly when it
// was not needed. Beside the id it is bounded by the id's own clip (skillIDCell) and cannot be
// moved by anything a SKILL.md says.

// The three source labels a skill row may carry. They name the SOURCE DIR the skill was loaded
// from (sourceDirs, internal/skills/load.go), not a trust level: trust is the human's call, and
// the label's job is to hand them the fact they need to make it.
const (
	skillSourceWorkspace = "workspace" // <ws>/.apogee/skills or <ws>/skills — the project's own
	skillSourceLibrary   = "library"   // <home>/skills — the user's global library (ADR 0032)
	skillSourceElsewhere = "elsewhere" // under neither root: a relocated or unwired source dir
)

// skillSourceSep joins a rendered id to its source label. It is the separator the dropdown's own
// hint line already uses (autocompleteHint), so a row's metadata reads as metadata rather than as
// more of the token — which "/clean-code workspace" would.
const skillSourceSep = " · "

// skillSource names which source dir a loaded skill came from, given the two roots this run
// resolved: home is [Options.ConfigHome] and workspace is [Options.Workspace] — the same pair the
// composition root builds skills.Sources from, so the answer is derived from the loader's own
// layering rather than guessed at.
//
// An empty Dir discloses NOTHING (the empty string) rather than guessing: a catalog assembled
// without one — a fake in a test, a future in-memory source — has no source to name, and inventing
// "elsewhere" for it would put a word on the row that says less than silence. A Dir that answers to
// neither root is the case that DOES earn "elsewhere": something loaded it, and the human should
// see that this run cannot account for where.
func skillSource(dir, home, workspace string) string {
	if dir == "" {
		return ""
	}
	// The library is asked FIRST because it is the source that WINS an id collision (ADR 0032), and
	// because a home may legitimately sit inside the workspace (--config <ws>/.apogee): when one
	// path answers to both roots, the label must name the source the catalog resolved the id
	// through, not the outer folder that happens to contain it.
	if home != "" && underSkillRoot(dir, filepath.Join(home, "skills")) {
		return skillSourceLibrary
	}
	if workspace != "" &&
		(underSkillRoot(dir, filepath.Join(workspace, ".apogee", "skills")) ||
			underSkillRoot(dir, filepath.Join(workspace, "skills"))) {
		return skillSourceWorkspace
	}
	return skillSourceElsewhere
}

// underSkillRoot reports whether dir IS root or sits beneath it, testing whole path components so
// a sibling named like the root ("<ws>/skills-vendored") can never be read as being inside it.
//
// Neither side is resolved through symlinks, because this is a DISCLOSURE and not a fence: what
// actually bounds the walk is the loader's os.Root (internal/skills/load.go), and a label is not
// asked to hold a boundary it cannot enforce (internal/security/doc.go's guard-is-not-a-boundary
// statement). The failure mode of the missing resolution is a skill labelled "elsewhere" — less
// informative, never wrong.
func underSkillRoot(dir, root string) bool {
	dir, root = filepath.Clean(dir), filepath.Clean(root)
	return dir == root || strings.HasPrefix(dir, root+string(filepath.Separator))
}

// maxSkillIDCells bounds how many runes of an id a row renders. An id is a DIRECTORY NAME in a
// repo apogee cloned, so its length is the repo's choice; the longest real ones run to roughly
// thirty ("improve-codebase-architecture" is 29), which is what this is measured against.
const maxSkillIDCells = 32

// skillTokenLabel renders the "/id" a surface shows for a skill, followed by the source it came
// from — "/clean-code · workspace". Both the merged "/" menu and the /skills report compose their
// rows through it, so the two surfaces cannot come to disclose different things about one skill.
// An unknown source (an empty label) renders the bare token, exactly as before the disclosure.
func skillTokenLabel(id, source string) string {
	token := "/" + skillIDCell(id)
	if source == "" {
		return token
	}
	return token + skillSourceSep + source
}

// skillIDCell renders an id as ONE bounded row cell: escape-stripped, folded onto a single line
// with its whitespace runs collapsed, and clipped to maxSkillIDCells with the ellipsis clipRunes
// appends when it cuts.
//
// The loader refuses an id carrying whitespace or a control character outright (skills.validate),
// so nothing this rewrites should ever reach a catalog — which is precisely why the rewrite is
// here as well. This is the DISPLAY seam, and the seam's rule (doc.go) is that this package
// sanitizes what it paints rather than trusting an upstream check to have already done it. Both
// halves answer a real trick: a padded id ("confine" + forty spaces + "off --save") renders as an
// innocent short token with its payload clipped off at the pane's edge, where a reader has no way
// to know anything was cut, and a newline in one paints a second row the pane never authored.
// Collapsed and clipped, the row either shows the whole id or ends in the "…" that says it did not.
func skillIDCell(id string) string {
	clean := stripEscapes(id)
	if strings.ContainsFunc(clean, unicode.IsSpace) {
		clean = strings.Join(strings.Fields(clean), " ")
	}
	return clipRunes(clean, maxSkillIDCells)
}

// loadedSkillLines renders the working half: one line per skill — the /id that names it and the
// source it was loaded from (skillTokenLabel), then its display name and its summary, plus one
// indented triggers line under it when the skill declares any (skillTriggerLine). Nothing
// loaded renders nothing, so the caller's section joiner never emits a "0 skills available:"
// header over an empty list.
//
// Both repo-authored halves are FLATTENED (flattenField): a note is painted one row per line and
// addNote's strip deliberately keeps "\n" (it sanitizes prose, and prose has paragraphs), so a
// SKILL.md whose summary carries a newline would otherwise write further lines into this report —
// lines it could shape as another skill's row, source label and all, under a heading that counted
// one fewer. The strip itself stays with addNote, the seam that owns it.
func loadedSkillLines(list []skills.Skill, home, workspace string) []string {
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
		line := "  " + skillTokenLabel(sk.ID, skillSource(sk.Dir, home, workspace))
		if name := flattenField(sk.DisplayName); name != "" {
			line += "  " + name
		}
		if summary := flattenField(sk.Summary); summary != "" {
			line += " — " + summary
		}
		lines = append(lines, line)
		if triggers := skillTriggerLine(sk.Triggers); triggers != "" {
			lines = append(lines, triggers)
		}
	}
	return lines
}

// maxTriggerCells bounds how many runes of a skill's joined trigger list the /skills row spends.
// The loader already caps a phrase at 64 runes and the list at 32 (skills.maxTriggerLen,
// maxTriggers), which is a bound on the DATA and far too wide for a row: 120 is roughly the width
// the report's other lines already run to, so the disclosure fits the pane and says "…" when the
// declaration is longer than the pane can show.
const maxTriggerCells = 120

// skillTriggerLine renders a skill's declared triggers as one indented line under its listing row —
// "    triggers: review a diff, code review" — or "" when the skill declares none, so a row without
// them is exactly the row it was before.
//
// The list exists to be READ: the triggers are why a skill will or will not be suggested for a
// draft, and until they are visible somewhere an author debugging "why did this never come up?"
// has only the file to go on. /skills is where the catalog already answers questions about itself.
//
// It is sanitized here rather than trusted: a trigger is repo-authored text, addNote's strip
// deliberately keeps "\n" (it sanitizes prose, and prose has paragraphs), and a note is painted one
// row per line — so a newline in a trigger would write further rows into this report, rows it could
// shape as another skill's entry, source label and all, under a heading that counted one fewer.
// Stripped, flattened and clipped, the declaration is one row or it ends in the "…" that says it
// was cut.
func skillTriggerLine(triggers []string) string {
	if len(triggers) == 0 {
		return ""
	}
	joined := strings.TrimSpace(flattenField(stripEscapes(strings.Join(triggers, ", "))))
	if joined == "" {
		return ""
	}
	return "    triggers: " + clipRunes(joined, maxTriggerCells)
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
//
// All three repo-authored fields are FLATTENED (flattenField) — the name, the reason and the path:
// a note is painted one row per line and addNote's strip deliberately keeps "\n" (it sanitizes
// prose, and prose has paragraphs), so a folder name or a multi-line YAML error would otherwise
// write further lines into this report — lines it could shape as a loaded skill's row, source
// label and all, under a heading that counted one fewer, on the very surface that exists to
// disclose an impersonation.
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
		lines = append(lines, "  "+flattenField(sk.Name())+" — "+flattenField(sk.Reason()))
		lines = append(lines, "    "+flattenField(sk.Path))
	}
	return lines
}

// shadowedSkillLines renders the collision half: one pair per shadowed skill — the copy that lost,
// then the copy that is live. Both paths are named because that is the whole question a shadow
// raises ("which of my two files is /<id> actually running?"), and the answer is a file the user
// can open. Nothing here is broken, so the wording deliberately never says "not loaded".
//
// The name, the losing path and the winning one are FLATTENED (flattenField) for the reason the
// other halves are: addNote's strip deliberately keeps "\n", so a newline in any of them would
// paint rows this section never authored — and here a forged row answers the shadow's one
// question with a file that is not the live copy.
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
		lines = append(lines, "  "+flattenField(sk.Name())+" — "+flattenField(sk.Path))
		lines = append(lines, "    the live copy is "+flattenField(by))
	}
	return lines
}
