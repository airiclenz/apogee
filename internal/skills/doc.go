// Package skills discovers skills — the user's own, from disk, and apogee's four shipped ones,
// from an embedded tree — and serves them as one catalog.
//
// A skill is a folder containing a SKILL.md file — YAML frontmatter (id/name, displayName,
// summary/description) plus a Markdown body of instructions. The directory-plus-SKILL.md
// shape matches the apogee-code oracle and the Anthropic/Claude-Code agent-skills convention,
// so a skill written for one is interoperable with the others and carries the bundled
// resources (refs/, scripts) hung off the same folder: the Dir field is the seam that hands
// that folder to the loop, which names it in the injected block for the model to read. Dir is an
// ADDRESS rather than always a host path — a disk skill announces its absolute folder, a shipped
// one announces the `shipped:<id>` virtual mount its own tree is served through.
//
// The package is the discovery half of the post-v1 apogee-code feature-parity skills feature:
// it loads skills, and a *Catalog resolves attached IDs both for the agent loop (which prepends
// the resolved bodies to the turn — through the domain.SkillResolver seam it satisfies, so the
// loop never imports this package) and for the TUI's merged "/" menu.
//
// It is grounded in:
//
//	ADR 0001  no implicit ~/.apogee — the state roots are injected (here, via Sources)
//	ADR 0002  skills are an open extension point — anyone may add one, nobody's is privileged
//	ADR 0065  four skills ship embedded, as the LOWEST-priority source (supersedes the
//	          "no builtins shipped" attribution this list used to hang on ADR 0002)
//	ADR 0010  package layout: depend only on internal/domain (downward), never the root facade
//	ADR 0032  the user's global library outranks the workspace on an id collision
//
// Layering (load.go): the sources are walked in DECREASING priority with the user's global
// library FIRST and the embedded shipped tree LAST, and an id collision keeps the copy already
// loaded — so a workspace skill may contribute a NEW id but can never replace one the user
// already has (ADR 0032). Walking the highest-priority source first is also what keeps the global
// skill cap from undoing that precedence: the cap is first-come, so it can only ever cut into the
// LOWEST-priority source. The two workspace dirs keep their relative order among themselves, and
// a shipped skill — the weakest claim on an id in the system (ADR 0065) — loses to every one of
// them. Every collision is recorded, cross-source or inside one dir alike: the losing SKILL.md
// goes onto the catalog as a skip carrying a ShadowedError that names the winning file.
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
// Suggestion (suggest.go) is the one thing this package computes rather than discovers: a BM25
// matcher over id + display name + description (unclamped — the 200-rune summary is the / menu's
// alone) + the author's optional triggers ranks the catalog against a draft message, so a Driver
// can offer the user the skills that fit what they are typing. It is HOST-SIDE only and changes
// nothing about what reaches the model — the catalog is never advertised to it, and a skill still
// enters a prompt only when the user attaches it as a "/id" (ADR 0061). Its ranking on the phrases
// people actually type is pinned against testdata/library/ — a frontmatter-only copy of the
// owner's real skill library, loaded through the ordinary Load by suggest_library_test.go.
//
// The shipped skills are served straight out of the binary and are never INSTALLED: no
// ~/.apogee/skills directory is auto-created for them (the creation-deferred convention — a
// writer creates what it needs), so an upgrade refreshes all four for every user and nothing on
// disk goes stale (ADR 0065). Their bundled files are reachable all the same: virtualReadRoots
// hands the same embedded tree to the host as a read-only MOUNT under the `shipped:` prefix
// (ShippedMountPrefix), which is the address their Dir announces — the pathless counterpart of
// readRoots, for the source that has no path. The gate is Sources.UseShippedSkills, whose zero
// value is off, so a caller that never asks for them loads exactly the disk sources it always
// did — and mounts nothing.
package skills
