package skills

import (
	"fmt"
	"path/filepath"
)

// Skill is one discovered, user-authored skill: a folder (Dir) containing a SKILL.md whose
// frontmatter and body define it. ID is the stable key the chat input attaches and the loop
// resolves; DisplayName and Summary drive the merged "/" menu; Body is the instruction text the
// loop prepends to the turn when the skill is attached.
//
// Dir is the absolute path to the skill's folder. It rides through ResolveSkills into the block
// the loop injects, which names it so the model can read the resources bundled beside the
// SKILL.md (refs/, prompts, scripts) without re-walking the source dirs.
// It plays no part in identity — two skills with the same ID collide regardless of Dir, and
// the later-loaded source wins (load.go).
type Skill struct {
	ID          string
	DisplayName string
	Summary     string
	Body        string
	Dir         string
}

// SkipError is discovery's other outcome: one SKILL.md the walk FOUND but could not turn into
// a Skill — unreadable, over the size cap, malformed frontmatter, or missing a required field.
//
// Discovery is deliberately soft (one bad skill never sinks the catalog), but soft must not
// mean silent: with only the loaded half surfaced, a malformed skill is indistinguishable from
// an absent one — the menu just does not offer it and the human has nowhere to look. So the
// reason travels WITH the catalog that omitted it (Catalog.Skipped), and the /skills report
// names it.
type SkipError struct {
	// Path is the absolute path of the SKILL.md that was skipped — the file to go and fix.
	Path string
	// Err is why it was skipped: the read failure, the YAML error, or the missing field.
	Err error
}

// Name is the folder the skipped SKILL.md sat in — the ID the skill WOULD have had, and so the
// name the human is hunting for when it never appears in the menu. It mirrors the dirName
// the loader derives an ID from (load.go), so the report names the skill the way the user does.
func (e SkipError) Name() string { return filepath.Base(filepath.Dir(e.Path)) }

// Reason renders the cause as a bare human-facing line, without Error's path prefix — what a
// report that already shows Path on its own line wants. A zero-value Err reads "unknown"
// rather than panicking a renderer.
func (e SkipError) Reason() string {
	if e.Err == nil {
		return "unknown"
	}
	return e.Err.Error()
}

// Error renders the skip as one joined-error line: "skills: skip <path>: <reason>".
func (e SkipError) Error() string { return fmt.Sprintf("skills: skip %s: %s", e.Path, e.Reason()) }

// Unwrap exposes the cause, so errors.Is/As reach through the skip to what actually failed.
func (e SkipError) Unwrap() error { return e.Err }

// ShadowedError is the cause recorded when a SKILL.md LOST an id collision: another file of the
// same id — in a higher-priority source dir, or later in the same one — is the copy that is live.
//
// It is NOT a load failure. The file was read and parsed fine and yielded a valid Skill; it just
// is not the one the catalog serves for that id. A reader keying on it (errors.As through the
// SkipError) must therefore say "shadowed", not "could not load" — the two send the human to
// different fixes, and only this one has a second file worth naming.
//
// Recording it at all is ADR 0032: with the user's global library now winning any cross-source
// collision, a workspace skill can lose one, and a substitution nobody is told about is exactly
// the defect that ADR closed. The same record covers two folders colliding inside ONE source dir,
// which used to lose one silently — against this package's own "soft must not mean silent"
// contract (doc.go).
type ShadowedError struct {
	// By is the absolute path of the winning SKILL.md — the copy that is actually live, so a
	// report can show the human which file the id now resolves to.
	By string
}

// Error renders the cause as a bare human-facing line naming the winning file.
func (e ShadowedError) Error() string {
	return fmt.Sprintf("shadowed by the skill of the same id at %s", e.By)
}
