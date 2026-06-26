# Plan — Skills system ([P0] apogee-code feature-parity)

**Date:** 2026-06-26
**Status:** **SHIPPED 2026-06-26** — implemented + tested on `main`. See the handoff
`docs/handoffs/2026-06-26 - 01 - skills-system.md` for the as-built record (one deliberate
divergence: an empty/whitespace `SKILL.md` is skipped, via a non-empty-body check in `validate`).
**Track:** post-`v1.0.0` apogee-code→Go **feature-parity** (same track as the mini-language
handoff `docs/handoffs/2026-06-26 - 00 - chat-mini-language-core.md`). Belongs to **no** phase
plan — the `phase-*-detail-plan.md` docs here cover phases 0–3 (shipped in `v1.0.0`). Purely
**additive**; no freeze break.

## Context

`TODO.md:44-48` tracks a **[P0] Skills system** as the next domino after the chat
mini-language core (shipped `99c0bdd`). apogee has **zero** skills code today; the
mini-language already pre-wired the seam (`domain.UserInput.SkillIDs`) and left a stub
(`loop.go noteUnresolvedSkillIDs`) that reports-and-drops any IDs. This feature makes
`/skill` real: discover user-authored skills from disk, let the user attach them in chat,
and inject the attached skill bodies into the turn.

It is a port of the original **apogee-code** VS Code extension (the behavioral oracle —
`~/.vscode/extensions/airic-lenz.apogee-code-0.2.58/media/chat.js` and
`/workspace/repos/apogee-code/src/config/skill-manager.ts`).

**No Phase-4 collision:** skills are turn-local prompt injection; the Library /
Mechanism catalogue / context-budget gauge are separate Phase-4 items, explicitly excluded
in `TODO.md:13-14`.

### Decisions locked with the owner (2026-06-26)

1. **On-disk format = directory + `SKILL.md`** (not flat `.md`). Each skill is a folder
   containing `SKILL.md` (YAML frontmatter + body). Matches the apogee-code oracle **and**
   the Anthropic/Claude-Code agent-skills convention → interoperable + room for bundled
   resources (`refs/`, scripts) later. Chosen over flat `.md` for long-term fit.
2. **First deliverable = this design doc; no code in the same session** (owner's "handoff doc
   for large plans" preference). Implementation is a separate next session.

---

## Design

The feature splits into three landable pieces; implement in this order (each is independently
testable):

### A. `internal/skills` package (new) — discovery + catalog

Conventions: `doc.go` citing ADR 0001 (injected state roots — no implicit `~/.apogee`),
ADR 0002 (skills are user-authored extensions over an open point), ADR 0010 (layout). Files
named by responsibility, each with `_test.go`. Reuse `gopkg.in/yaml.v3` (already in `go.mod`).

- `skill.go` — `type Skill struct { ID, DisplayName, Summary, Body, Dir string }` (`Dir` =
  the skill's folder, for future bundled-resource resolution).
- `catalog.go` — `type Catalog struct{...}` with `List() []Skill` (sorted by DisplayName),
  `Resolve(ids []string) []Skill` (in-id-order, skip unknown), `Get(id) (Skill, bool)`.
- `load.go` — `type Sources struct { Home, Workspace string; UseProjectSkills bool }`;
  `func Load(Sources) (*Catalog, error)`. **Layered dirs, later wins on ID collision**
  (workspace overrides global), mirroring the oracle (`skill-manager.ts:25-41`):
  1. `Home/skills` (`~/.apogee/skills`) — always
  2. `Workspace/.apogee/skills` — always
  3. `Workspace/skills` — only if `UseProjectSkills`
  Each dir: open via **`os.OpenRoot`** + `fs.WalkDir` (same fence idiom as
  `autocomplete.go workspaceFiles`, so a workspace symlink can't escape) → find subdirs
  containing `SKILL.md` (case-insensitive); skip dotted dirs. A **missing dir is fine**
  (skip); a **malformed skill is skipped** (collect a soft error, never fail the whole load).
- `parse.go` — `parseSkill(content, dirName) (Skill, error)`: split YAML frontmatter on
  `---`; accept `id`|`name`, `displayName`, `summary`|`description` (oracle aliases); derive
  `id = id||name||dirName`, `displayName = displayName||titleCase(id)`, `summary =
  summary||description`, `body =` everything after the frontmatter. Fallback when no
  frontmatter (oracle parity): id=dirName, displayName from first heading, summary from first
  non-heading line, body=whole file. Require id+displayName+summary non-empty else skip.

**No builtin/embedded skills and no auto-created `~/.apogee/skills`** in v1 (the TODO lists
only the three dirs; creation-deferred convention). Both are noted as additive future hooks.

### B. Config — `use-project-skills` key + wiring

- `cmd/apogee/config.go`: add `UseProjectSkills *bool \`yaml:"use-project-skills"\`` to
  `fileConfig`, a `useProjectSkills *bool` to `layer`, and `useProjectSkills bool` to
  `settings` (**default true**), following the existing kebab-case precedence
  (flag > env > file > default). Add a commented `# use-project-skills: true` to the embedded
  template `cmd/apogee/defaults/config.yaml`.
- `cmd/apogee/wire.go`: build the catalog and inject it into **both** consumers:
  `catalog, _ := skills.Load(skills.Sources{Home: roots.config, Workspace: roots.workspace,
  UseProjectSkills: opts.useProjectSkills})` → set `cfg.Skills = catalog` (loop) and
  `tui.Options.Skills = catalog` (TUI). The same `*skills.Catalog` satisfies both seams.

### C. Agent-loop resolution — fulfil the `SkillIDs` seam

- `internal/domain/config.go`: add a `SkillResolver` interface + `Config.Skills SkillResolver`
  field. `domain` must not import `internal/skills`, so define the interface in domain
  (`ResolveSkills(ids []string) []ResolvedSkill`, `ResolvedSkill{ID, DisplayName, Body}`);
  `skills.Catalog` (which may import `domain`) implements it. **No `UserInput` change** —
  `SkillIDs` already exists.
- `internal/agent/loop.go`: **replace `noteUnresolvedSkillIDs`** (lines 453-467) with
  `resolveSkillRefs(turn, ids) string`, mirroring `resolveFileRefs` (line 417): resolve IDs
  via `a.cfg.Skills`, format each as a labeled block, and **prepend to the user message** at
  `step()` line 164 — order: **skill blocks → file-ref blocks → user text** (skills are
  per-turn instructions; prepending scopes them to that one message, the right semantics, and
  avoids the system-prompt-persistence problem). `nil` resolver or unknown ID → emit the same
  graceful `ErrorEvent` and drop (keep the "never silently ignored" property). Reuse the
  injection shape, not a new mechanism/hook.

### D. TUI `/skill` UX (the largest piece)

Files: `internal/tui/autocomplete.go`, `model.go`, `tui.go`, plus a new `skill_test.go`.
Respect the **value-copy guard** (ADR 0011 / `TestModelNoBuilderByValue`): new state is a
plain `[]string` (`pendingSkills`) + an interface field (`Options.Skills`) — both reference
headers, safe.

- **Arg-aware autocomplete** — a third `acKind` (`acSkill`). `/skill ` followed by a partial
  opens a skill dropdown (filter by id/displayName substring, exclude already-attached, show
  displayName + faint summary). New `skillArgToken(value)` detects the trailing `/skill <arg>`
  region (handles `"/skill "`, `"/skill cl"`, mid-line `"fix /skill cl"`; rejects bare
  `/skill`, completed `"/skill foo "`). Add the branch **first** in `computeAutocomplete`.
- **`/skill` offered, NOT intercepted** — add `skill` to a new autocomplete `commandMenu`
  (with summaries), but **leave `knownCommands` (the parser) unchanged**. Critical: do not
  make the parser treat `/skill` as a command — attachment happens only via autocomplete, like
  the oracle's `selectSkill`. Keeps `TestParseInputUnknownSlashIsMessage` green.
- **Attach = pop a chip, strip text** — `acceptAutocomplete` branches to `attachSkill(id)`:
  append to `m.pendingSkills` (deduped), strip the `/skill <partial>` text, re-derive the
  overlay. Accepting the `/skill` command itself completes to `"/skill "` and chains straight
  into the skill dropdown (recompute overlay at end of accept). `autocompleteExactMatch`
  returns false for `acSkill` and for a highlighted `/skill` command (Enter never sends them
  literally).
- **Chips + submit** — `pendingSkills []string` on the Model; `renderSkillChips()` resolves
  via `opts.Skills` and renders badges in a `View` slot that shrinks the viewport (alongside
  the dropdown). `submit()` copies `pendingSkills` into `domain.UserInput.SkillIDs`, allows an
  empty-text send when skills are attached, then clears `pendingSkills`. Backspace-on-empty
  removes the last chip. `/clear` + `/compact` clear chips; `/continue` carries them.

---

## Verification

- `go test ./internal/skills/...` — table-driven loader tests with `t.TempDir()`: layering
  override (workspace > home), `use-project-skills` gating of `workspace/skills`, missing-dir
  tolerance, malformed-skill skip, frontmatter alias parsing + no-frontmatter fallback,
  `Catalog.List` sort + `Resolve` order/unknown-skip. `os.Root` symlink-escape refusal.
- `go test ./internal/agent/...` — SkillIDs resolve + prepend (fake resolver); nil resolver →
  graceful drop. **Update** the existing `minilang_test.go` expectation that asserted
  `noteUnresolvedSkillIDs`.
- `go test ./cmd/apogee/...` — `use-project-skills` precedence.
- `go test ./internal/tui/...` — `skill_test.go`: pure `skillArgToken`, `computeAutocomplete`
  acSkill, command dropdown offers `/skill`, accept attaches+strips, submit carries SkillIDs,
  chip render/clear, backspace removal, nil-catalog guard. `TestModelNoBuilderByValue` stays
  green.
- Manual: `go run ./cmd/apogee` with a `~/.apogee/skills/<name>/SKILL.md`, type `/skill`,
  attach, send, confirm the body reaches the model (debug/inspector or transcript).

---

## Critical files

**New:** `internal/skills/{doc,skill,catalog,load,parse}.go` (+ tests).
**Modified:** `internal/domain/config.go` (SkillResolver + Config.Skills),
`internal/agent/loop.go` (resolveSkillRefs replaces noteUnresolvedSkillIDs),
`cmd/apogee/config.go` + `cmd/apogee/defaults/config.yaml` (use-project-skills),
`cmd/apogee/wire.go` (Load + inject), `internal/tui/{autocomplete,model,tui}.go`,
`internal/tui/minilang_test.go` (update stale SkillIDs expectation).
