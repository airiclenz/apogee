package skills

// Skill is one discovered, user-authored skill: a folder (Dir) containing a SKILL.md whose
// frontmatter and body define it. ID is the stable key the chat input attaches and the loop
// resolves; DisplayName and Summary drive the /skill picker; Body is the instruction text the
// loop prepends to the turn when the skill is attached.
//
// Dir is the absolute path to the skill's folder, carried so a later bundled-resource feature
// (refs/, scripts) can resolve files relative to the skill without re-walking the source dirs.
// It plays no part in identity — two skills with the same ID collide regardless of Dir, and
// the later-loaded source wins (load.go).
type Skill struct {
	ID          string
	DisplayName string
	Summary     string
	Body        string
	Dir         string
}
