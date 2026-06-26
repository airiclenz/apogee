// Package skills discovers user-authored skills from disk and serves them as a catalog.
//
// A skill is a folder containing a SKILL.md file — YAML frontmatter (id/name, displayName,
// summary/description) plus a Markdown body of instructions. The directory-plus-SKILL.md
// shape matches the apogee-code oracle and the Anthropic/Claude-Code agent-skills convention,
// so a skill written for one is interoperable with the others and has room for bundled
// resources (refs/, scripts) hung off the same folder later (the Dir field is the seam).
//
// The package is the discovery half of the post-v1 apogee-code feature-parity /skill command:
// it loads skills, and a *Catalog resolves attached IDs both for the agent loop (which prepends
// the resolved bodies to the turn — through the domain.SkillResolver seam it satisfies, so the
// loop never imports this package) and for the TUI's /skill picker.
//
// It is grounded in:
//
//	ADR 0001  no implicit ~/.apogee — the state roots are injected (here, via Sources)
//	ADR 0002  skills are user-authored extensions over an open point — no builtins shipped
//	ADR 0010  package layout: depend only on internal/domain (downward), never the root facade
//
// Layering (load.go): later sources override earlier on an id collision, so a workspace skill
// shadows a global one of the same id. Robustness is by design — a missing source dir is
// skipped, and a malformed skill is skipped with a soft error rather than failing the whole
// load, so one bad file never blanks the catalog.
//
// No builtin/embedded skills and no auto-created ~/.apogee/skills directory ship in v1 (the
// creation-deferred convention — a writer creates what it needs); both are additive future
// hooks, not a current gap.
package skills
