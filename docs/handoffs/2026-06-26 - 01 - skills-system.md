# Handoff — Skills system + `/skill` (apogee-code feature-parity)

**Date:** 2026-06-26
**Status:** **SHIPPED** (implemented + tested on `main`, pre-production — committed direct)
**Track:** post-`v1.0.0` **feature-parity** (apogee-code → Go TUI). The *second* entry on that
track, after the chat mini-language core (`… - 00 - chat-mini-language-core.md`), which
pre-wired the `UserInput.SkillIDs` seam this slice fulfils. Belongs to **no** phase plan
(`docs/plans/` covers phases 0–3, shipped in `v1.0.0`). Purely **additive**; no freeze break.

## Why

`TODO.md`'s **[P0] Skills system** was the next domino after the mini-language core. apogee had
**zero** skills code; the mini-language left a stub (`loop.go noteUnresolvedSkillIDs`) that
reported-and-dropped any `SkillIDs`. This makes `/skill` real: discover user-authored skills,
attach them in chat, and inject their bodies into the Turn. The behavioral oracle is apogee-code
(`/workspace/repos/apogee-code/src/config/skill-manager.ts`, and its webview `media/chat.js`).

The approved design is `docs/plans/skills-system-plan.md` — read it for the full rationale; this
handoff records what shipped and where it diverged.

## Decisions locked with the owner (2026-06-26)

1. **On-disk format = directory + `SKILL.md`** (not flat `.md`) — matches the apogee-code oracle
   *and* the Anthropic/Claude-Code agent-skills convention, so skills are interoperable and have
   room for bundled resources (`refs/`, scripts) later (`Skill.Dir` is that seam).
2. **No builtins, no auto-created `~/.apogee/skills`** in v1 (creation-deferred convention). Both
   are noted as additive future hooks.

### Divergence from the plan (one deliberate call)

- **An empty/whitespace `SKILL.md` is skipped, not loaded.** The plan's validate rule was
  "id+displayName+summary non-empty"; the oracle's `dirName` fallback would otherwise name an
  empty file after its folder and load a body-less skill (nothing to inject). `validate` also
  requires a **non-empty body** — the honest definition of "malformed" here. Easy to relax to
  oracle-exact if wanted (`internal/skills/parse.go validate`).

## Implementation map (file → change)

**A. `internal/skills` package (new):**
- `skill.go` — `Skill{ID, DisplayName, Summary, Body, Dir}`.
- `load.go` — `Sources{Home, Workspace, UseProjectSkills}`, `Load(Sources) (*Catalog, error)`.
  Layered dirs (`Home/skills` → `Workspace/.apogee/skills` → `Workspace/skills`, last gated),
  **later source wins on ID collision**. Each dir walked via `os.OpenRoot` + `fs.WalkDir` (the
  same fence idiom as `autocomplete.go workspaceFiles`); dotted dirs skipped; missing dir
  skipped; malformed skill skipped with a **soft error** joined into the (ignorable) return
  error. `Load` always returns a **non-nil** catalog (so a typed-nil never reaches the resolver).
- `parse.go` — frontmatter split (BOM/CRLF tolerant), aliases `id`|`name` / `displayName` /
  `summary`|`description`, `titleCase` displayName fallback, no-frontmatter fallback, summary
  clamped to 200 runes, `validate` (see divergence).
- `catalog.go` — `Catalog` with `List()` (sorted by DisplayName, tie-broken by ID), `Get`,
  `Resolve`, and `ResolveSkills` (satisfies `domain.SkillResolver`).

**B. Config + wiring:**
- `cmd/apogee/config.go` — `use-project-skills` (`*bool`, **file-only, default true**) through
  `fileConfig`/`layer`/`settings`; `cmd/apogee/root.go` `options.useProjectSkills`; documented in
  the embedded `cmd/apogee/defaults/config.yaml`.
- `cmd/apogee/wire.go` — `skills.Load(...)` once, injected into **both** `cfg.Skills` (loop) and
  `tui.Options.Skills` (picker); the same `*skills.Catalog` satisfies both seams.

**C. Domain + loop:**
- `internal/domain/config.go` — `SkillResolver` interface + `ResolvedSkill` + `Config.Skills`
  (domain defines the interface so the loop never imports `internal/skills` — ADR 0010).
- `internal/agent/loop.go` — `noteUnresolvedSkillIDs` → **`resolveSkillRefs`**: resolve via
  `a.cfg.Skills`, format each as `<skill: …>` blocks, **prepend before the `@file` blocks and
  the user text** (`step()`). nil resolver / unknown ID → graceful `ErrorEvent`, dropped.
- `apogee.go` + `example_test.go` — `SkillResolver`/`ResolvedSkill` re-exported (every other
  `Config` delegate type is, and a public field whose return type can't be named is unusable).

**D. TUI `/skill` UX:**
- `internal/tui/autocomplete.go` — third `acKind` `acSkill`; `skillArgToken` (detects the
  trailing `/skill <arg>` region); `commandMenu` (offers `/skill` with summaries — but
  `knownCommands`, the parser, is **unchanged**, so `/skill` never submits as a message);
  `skillSuggestions`; `attachSkill` (dedup chip + strip text); accept recomputes the overlay so
  accepting `/skill` chains into the picker; `renderSkillChips`.
- `internal/tui/model.go` — `pendingSkills []string`; submit copies it into `UserInput.SkillIDs`
  (allows empty-text-with-skills) and clears it; backspace-on-empty pops a chip; `/clear` +
  `/compact` clear chips, `/continue` carries them; chips slot shrinks the viewport in `View`.
- `internal/tui/tui.go` — `SkillCatalog` interface + `Options.Skills`; `theme.go` — `skillChip`.

## Verification

- `go build ./...`, `go vet ./...`, `gofmt` clean; `go test ./...` and **`go test -race ./...`**
  all green; `TestModelNoBuilderByValue` still green (new state is a `[]string` + an interface
  field — both reference-safe in the value-copied Model).
- New tests: `internal/skills/{parse,load,catalog}_test.go` (layering override, project-skills
  gating, missing-dir tolerance, malformed skip, **`os.Root` symlink-escape refusal**, alias +
  fallback parsing, sort/resolve); `internal/agent/minilang_test.go` (resolve+prepend, unknown-ID
  noted, nil-resolver graceful drop — replaced the old `TestUnresolvedSkillIDsNoted`);
  `cmd/apogee/config_test.go` (`use-project-skills` precedence); `internal/tui/skill_test.go`
  (`skillArgToken`, `acSkill` dropdown, `/skill` offered, accept attaches+strips, chains into
  picker, submit carries `SkillIDs`, empty-text send, chip render/clear, backspace removal,
  nil-catalog guard).
- An end-to-end smoke (real `SKILL.md` on disk → `skills.Load` → catalog as `domain.SkillResolver`
  → loop injects the body) passed and was removed.

## Out of scope (follow-ups, unchanged from the mini-language handoff)

- **`/compact` real reducer** — `internal/context` generative summarization into the
  `Agent.Compact()` seam.
- **`/server`** — swappable provider seam (today `upstream` is immutable post-construction).
- **Bundled skill resources** — `Skill.Dir` is the seam; nothing reads it yet.
- **Skill picker polish** — the dropdown shows `displayName + summary` in one row style (the
  "faint summary" is not separately dimmed, given the single-style row renderer).
