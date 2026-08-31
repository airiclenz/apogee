package skills

import (
	"fmt"
	"path/filepath"
)

// Skill is one discovered skill: a folder containing a SKILL.md whose frontmatter and body define
// it — one the user wrote, or one of the four apogee ships embedded (ADR 0065). ID is the stable
// key the chat input attaches and the loop resolves; DisplayName and Summary drive the merged "/"
// menu; Body is the instruction text the loop prepends to the turn when the skill is attached.
//
// Dir is the ADDRESS of the skill's folder — the absolute host path for a skill found on disk, and
// the `shipped:<id>` virtual-mount address for one of apogee's own, whose bytes are in the binary
// rather than under any host path (ADR 0065 §3, ShippedMountPrefix). Both are addresses the read
// tools resolve; empty means the skill announces no readable folder at all. It rides through
// ResolveSkills into the
// block the loop injects, which names it so the model can read the resources bundled beside the
// SKILL.md (refs/, prompts, scripts) without re-walking the source dirs; an empty Dir omits that
// line entirely (internal/agent's resolveSkillRefs).
// It plays no part in identity — two skills with the same ID collide regardless of Dir, and the
// FIRST source that claims the id wins: the walk runs highest-priority-first and Catalog.set keeps
// what it already holds (ADR 0032, load.go). The embedded shipped source is walked last, so every
// skill folder on disk shadows a shipped id of the same name.
type Skill struct {
	ID          string
	DisplayName string
	Summary     string
	Body        string
	Dir         string

	// Description is the summary's source text past the maxSummaryLen clamp Summary carries for
	// the "/" menu — the frontmatter's summary/description, or the fallback's first prose line,
	// under the far wider maxDescriptionLen (4096 runes) instead of 200. It is read ONLY by the
	// matcher — Suggest, for the Driver's suggestion band, and Lookup, for the model's load_skill
	// query — which indexes it so a phrase an author placed past the menu cap still finds the
	// skill; it is never displayed anywhere, and the text itself never leaves the host.
	Description string

	// Triggers is the optional list of phrases the SKILL.md's author declared under "triggers:":
	// lowercase, whitespace-normalised fragments they expect to appear in a prompt this skill
	// fits ("review this diff", "cut a release"). It is read ONLY by the matcher — Suggest and
	// Lookup alike — which uses a hit as a boost on top of its scoring, never as the sole reason
	// to offer a skill. The phrases themselves are never shown to anyone: they are matcher input,
	// not text.
	//
	// What ADR 0061 kept from the model is narrower since ADR 0065 §7 and still holds where it
	// counts: nothing about the catalog enters the STANDING prompt, and a skill's body reaches a
	// turn only when the user attaches it as a "/id" — or when the model itself asks for one
	// through the load_skill door (lookup.go), which is a question it chose to spend a call on
	// rather than a listing apogee volunteered.
	Triggers []string
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
