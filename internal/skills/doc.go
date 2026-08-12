// Package skills discovers user-authored skills from disk and serves them as a catalog.
//
// A skill is a folder containing a SKILL.md file — YAML frontmatter (id/name, displayName,
// summary/description) plus a Markdown body of instructions. The directory-plus-SKILL.md
// shape matches the apogee-code oracle and the Anthropic/Claude-Code agent-skills convention,
// so a skill written for one is interoperable with the others and carries the bundled
// resources (refs/, scripts) hung off the same folder: the Dir field is the seam that hands
// that folder to the loop, which names it in the injected block for the model to read.
//
// The package is the discovery half of the post-v1 apogee-code feature-parity skills feature:
// it loads skills, and a *Catalog resolves attached IDs both for the agent loop (which prepends
// the resolved bodies to the turn — through the domain.SkillResolver seam it satisfies, so the
// loop never imports this package) and for the TUI's merged "/" menu.
//
// It is grounded in:
//
//	ADR 0001  no implicit ~/.apogee — the state roots are injected (here, via Sources)
//	ADR 0002  skills are user-authored extensions over an open point — no builtins shipped
//	ADR 0010  package layout: depend only on internal/domain (downward), never the root facade
//	ADR 0032  the user's global library outranks the workspace on an id collision
//
// Layering (load.go): later sources override earlier on an id collision, and the user's global
// library is walked LAST — so a workspace skill may contribute a NEW id but can never replace one
// the user already has (ADR 0032). The two workspace dirs keep their relative order among
// themselves. Every collision is recorded, cross-source or inside one dir alike: the losing
// SKILL.md goes onto the catalog as a skip carrying a ShadowedError that names the winning file.
// Robustness is by design — a missing source dir is skipped, and a malformed skill is skipped
// rather than failing the whole load, so one bad file never blanks the catalog. Every skip is
// recorded ON the catalog (Catalog.Skipped) so the /skills report can say which file was passed
// over and why: soft must not mean silent, or a broken skill is indistinguishable from an absent
// one — and a shadowed one from a skill that was never there.
//
// Parsing is forgiving before it gives up (parse.go). Frontmatter is read as strict YAML first,
// and only a hard YAML failure falls through to a line-by-line "key: value" scan that recovers the
// ordinary authoring slips — an unquoted value containing ": ", a tab indent, an unclosed quote.
// These same SKILL.md files are shared with tools whose parsers are more forgiving, so a skill
// another tool lists must not vanish here; a block that IS valid YAML keeps its exact YAML meaning
// and never reaches the scan.
//
// No builtin/embedded skills and no auto-created ~/.apogee/skills directory ship in v1 (the
// creation-deferred convention — a writer creates what it needs); both are additive future
// hooks, not a current gap.
package skills
