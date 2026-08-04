# Remove the obsolete `/skill` command

- **Goal:** delete the `/skill` picker verb — redundant since skills are invoked
  directly (`/<skill-id>`, ADR 0027) or picked from the merged `/` menu — together
  with the `menuOnly` machinery and the `acSkill` dropdown that exist solely for it,
  then sweep code comments and living documentation so nothing still describes the
  removed entry point.
- **Date:** 2026-08-04 · **Status:** TODO (no item executed)
- **Authoritative sources:**
  - `ISSUES.md:12` — the owner directive: "`/skill` is not needed anymore — remove it."
  - `docs/adr/0027-one-slash-namespace-with-inline-skill-tokens.md` — the decision that
    made `/skill` redundant; it explicitly kept the two-step picker as an alternate
    entry point, so it receives a dated amendment note (item 3), never a rewrite.
  - Pinned commit `5b75c1b` — every `file:line` below is indexed against it. Line
    numbers are locators only; the named symbols and quoted phrases are authoritative
    if lines drift (the menu-layout plan `2026-08-04 - 03` lands first and touches
    some of the same files).
  - If any doc named below disagrees with an item, the code at `5b75c1b` plus
    `ISSUES.md:12` win.
- **Standing requirements:**
  - skills: coding-standards
  - Execute only on a clean tree, after plan `2026-08-04 - 03`
    (user-questions-menu-layout) has landed — its in-flight work touches
    `autocomplete.go` / `popup.go`.
  - `make check` green before every commit.
- **Out of scope — must NOT be touched:**
  - `/skills` (the listing command), direct `/<skill-id>` invocation, inline skill
    tokens, and the skills subsystem (`internal/skills/*`, `Options.Skills`,
    `Options.ReloadSkills`) — all stay.
  - Shared autocomplete plumbing the merged `/` menu still uses: `skillSuggestions`,
    `insertSkillToken`, `spliceCompletion`, `outsideRegion`, the `skillRegion`
    field, popup chrome — stays.
  - The `whileRunning` flag and the `— idle only` tag machinery
    (`autocomplete.go:269,292-295`) — only `menuOnly` is removed.
  - Historical records are never rewritten: CHANGELOG entries at or below the
    `## [0.10.4]` heading (line ~2383), earlier `[Unreleased]` narrative entries
    (they describe their own change at its time; the new `### Removed` entry
    supersedes them chronologically), `TODO.md` done-list lines (`:77`, `:777`),
    passing mentions in ADR 0025 (`:159`) and ADR 0032 (`:29`), and everything under
    `docs/plans/archived/` and `docs/handoffs/`.
  - The active plan `2026-08-04 - 04 - slash-menu-relevance-ranking-plan.md:53`
    mentions the `/skill <arg>` picker inheriting the ranking; that clause becomes
    inert after this removal. Saved plans are never rewritten — flagged here for the
    owner, not edited.
  - No version identifier changes (see closing note).

## 1. Remove the `/skill` verb, the `menuOnly` flag, and the `acSkill` picker

**What:** Delete the behaviour and every test that pins it, in one green commit.

Production — `internal/tui/command.go`:

- The registry row `{name: "skill", summary: "pick a skill by name (writes its
  /token)", menuOnly: true}` (`:139`).
- The `menuOnly` field on `commandSpec` (`:77`) and its defining doc comment
  (`:69-71`). `/skill` is its only setter and nothing else changes: every other
  spec leaves it false, so each reader below reduces to its false arm.
- The `c.menuOnly` disjunct in `matchCommand` (`:285`, `if !ok || c.menuOnly` →
  `if !ok`).
- `skillPickerUsage` (`:222`) and its `unknownSlashNote` branch (`:231-233`) — a
  bare `/skill` now earns the same generic `unknown command or skill` refusal as
  any other unknown verb.
- The ordering-dependency paragraph "`/skill` must precede `/skills`…" (`:122-126`).
- In-file doc-comment mentions of the flag at `:81`, `:201`, `:219`, `:238`, `:268`.

Production — `internal/tui/autocomplete.go`:

- The `acSkill` kind (`:46`), its `computeAutocomplete` branch (`:107-113`), and its
  `autocompleteTitle` case (`:703-704`).
- `skillArgToken` (`:308-323`) and the `inPicker` half of `recomputeAutocomplete`'s
  reload edge trigger (`:163`) — the `inMenu` half survives (the merged menu still
  reloads the catalog on open).
- The `spec.menuOnly` early-return in `autocompleteExactMatch` (`:563`) and the
  `acSkill` early-return above it (`:547-549`).
- The `|| spec.menuOnly` disjunct in `acceptAutocomplete` (`:600`) — `takesArgs`
  completion behaviour (`/confine`) is untouched.
- Comment mentions of the menu-only `/skill` at `:30`, `:277`, `:579`.

Tests (all in `internal/tui/`):

- `skill_test.go` — delete the picker-behaviour tests wholesale:
  `TestSkillArgToken`, `TestSkillArgTokenAtTheCaret`,
  `TestComputeAutocompleteSkillDropdown`, `TestCommandDropdownOffersSkill`,
  `TestSkillArgWinsOverBareCommand`, `TestAcceptSkillSplicesInlineToken`,
  `TestSkillPickerExcludesTokensAlreadyInTheBuffer`,
  `TestSkillCommandChainsIntoPicker`, `TestEnterOnSkillCommandDoesNotSubmit`,
  `TestSkillPickerReloadsOnOpenByTyping`, `TestSkillPickerReloadsViaCommandChain`,
  `TestSkillPickerReloadNilSafe`. `TestSlashMenuShadowsCollidingSkillID` (`:366`)
  keeps its subject but its `/skill clea` path (`:378`) must be reworked to reach
  the shadow via the merged menu; `TestNilCatalogGuards` (`:606`) drops its
  `"/skill "` case, keeps the rest.
- `command_test.go` — `TestCommandTableDrivesParserAndMenu` (`:41`): the
  `menuOnly` assertions go, `wantParsed` becomes all 15 remaining verbs;
  `TestCommandSpecsReadAlphabetically` (`:100-115`): drop the skill<skills index
  assert (`:112`); `TestParseInputSoleUnknownSlash` (`:245`): the `/skill` case
  (`:255`) now expects the generic unknown handling; `TestUnknownSlashNote`
  (`:284`): the `skillPickerUsage` assert (`:288`) and the menuOnly drift-guard
  loop (`:291-298`) go; the `"/skill foo"` sent-verbatim case (`:229`) keeps its
  expectation (a multi-token unknown-verb line still travels as text) — verify,
  don't assume.
- `minilang_test.go` — delete `TestBareSkillVerbTeachesThePicker` (`:532`); update
  the caret-region comment naming the `"/skill "` picker exception (`:1031`).
- `autocomplete_test.go` — `TestAutocompleteSkillDropdownChrome` (`:31`) and
  `TestAutocompleteDropdownSpansFullWidth` (`:115`) open their dropdown via
  `"/skill "`: repoint both to a surviving dropdown (the merged `/` menu or `@`)
  so the chrome coverage is kept, not deleted.
- `prompteditor_test.go` — the reset fixture (`:63-76`) uses
  `"half-typed /skill go"` and `kind: acSkill`: swap to a surviving kind.
- `interject_test.go` — `TestAutocompleteOpensWhileRunning` (`:845`): the
  `/skill re` mid-run case (`:879`) repoints to a surviving mid-run flow; the
  `idleOnlyTag`-on-`/clear` assert (`:872`) stays untouched.
- `transcript_test.go:676-677` — uses the surviving `skillSuggestions`; relabel
  the `"a /skill picker row"` comment only if it still reads wrong.

**Tests:** the suite itself is the deliverable above; additionally keep (or add,
if the reworked `TestParseInputSoleUnknownSlash` case doesn't already pin it) one
assertion that a sole `/skill` is refused with the generic unknown note.

**Acceptance:**
- `make check` → green.
- `grep -rn "menuOnly\|acSkill\|skillArgToken\|skillPickerUsage" internal/ cmd/ apogee.go` → no hits.
- `grep -n '"skill"' internal/tui/command.go` → no hits.

**Commit:** `feat(tui): remove the /skill picker — direct /<id> invocation and the merged menu replace it`

## 2. Sweep the code comments that still name the `/skill` picker

Depends on item 1.

**What:** One-line comment rewordings only — no behaviour, no test changes. Every
in-code comment that presents the `/skill` picker as a live consumer is repointed
to the surviving consumers (the merged `/` menu, `/skills`, inline `/<id>` tokens):

- `internal/tui/doc.go:122-124` — the mini-language paragraph describing the
  `/skill` → `acSkill` chain.
- `internal/tui/tui.go:18`, `:282`, `:290` — `Options.Skills` / `ReloadSkills`
  described as feeding "the /skill picker".
- `internal/tui/skills.go:15`, `:18` — the file header's verb map.
- `internal/tui/prompteditor.go:39`, `:43` — `skillRegion` described as tracking a
  "`/skill <partial>` region" (it now serves the merged-menu reload trigger).
- `internal/tui/model.go:505`, `:991` — "(reloads on /skill open)".
- `cmd/apogee/wire.go:85`, `:89`, `:520` — the catalog-refresh comments.
- `apogee.go:239` — "attached /skill IDs".
- `internal/skills/doc.go:9`, `:12`; `catalog.go:13`, `:79`; `provider.go:14`,
  `:54`; `skill.go:10`; `parse.go:14`; `load.go:26`; `provider_test.go:10`.
- `internal/domain/config.go:327`.

`/skill-id` and `/<skill-id>` mentions are the direct-invocation syntax and stay.

**Tests:** none new — `make check` proves the sweep is comment-only.

**Acceptance:**
- `make check` → green.
- `grep -rnE '/skill([^s-]|$)' --include='*.go' . | grep -v docs/` → no hits.
- `git diff HEAD~1 --stat` shows comment-only line counts (no hunk touches
  executable code — verifier eyeballs the diff).

**Commit:** `docs(tui): repoint code comments from the removed /skill picker to its surviving consumers`

## 3. Living documentation, changelog, ADR amendment, and issue closure

Depends on item 1.

**What:** every cross-cutting doc amendment for this removal, owned here:

- `README.md:158` — delete the `/skill` table row (the surrounding prose describes
  the merged menu and needs no change — verify).
- `CONTEXT.md:638` — drop the "plus the `/skill <name>` picker that writes the
  token for you" clause from the Skill entry.
- `layout.md:848` — remove the `/skill` exception clause ("plus `/skill`, which
  chains into the picker over the catalog") from the "Accepting a command RUNS
  it" spec; re-read `:845-849` so the surviving sentence stays true.
- `cmd/apogee/defaults/config.yaml:280` — reword "attach one in chat with /skill"
  to the direct form (type `/<skill-id>` in your message).
- `CHANGELOG.md` — add a `### Removed` entry under `[Unreleased]` (create the
  section if absent): `/skill` is gone — invoke a skill by typing its `/<id>`
  directly or pick it from the merged `/` menu; the `menuOnly` flag left with it,
  which also retires the wrong `— idle only` tag that row wore (ISSUES.md:23).
  Earlier `[Unreleased]` entries are left untouched (see out-of-scope).
- `docs/adr/0027-one-slash-namespace-with-inline-skill-tokens.md` — add a short
  dated amendment note (at the status/header area): the alternate two-step
  `/skill` entry point this ADR kept was removed 2026-08-04 as redundant
  (ISSUES.md directive); the core decision — one namespace, inline tokens — is
  unchanged. Do not rewrite the ADR body or its rejected-alternatives section.
- `ISSUES.md` — delete both entries: `:12` (the removal directive — executed) and
  `:23` (the `— idle only` tag bug — moot: the tag, the flag, and both proposed
  fixes left with the feature).

**Tests:** none (markdown + an embedded yaml comment; the config template is
covered by item 2's `make check` having proven the embed still builds — re-run it
here anyway since `config.yaml` changes in this item).

**Acceptance:**
- `make check` → green (the edited template re-embeds).
- `grep -nE '/skill([^s-]|$)' README.md CONTEXT.md layout.md ISSUES.md cmd/apogee/defaults/config.yaml` → no hits.
- `awk '/## \[Unreleased\]/,/## \[0\.10\.4\]/' CHANGELOG.md | grep -c '### Removed'` → ≥ 1.
- `grep -n '2026-08-04' "docs/adr/0027-one-slash-namespace-with-inline-skill-tokens.md"` → ≥ 1 (the amendment note).

**Commit:** `docs: retire /skill from the living docs, amend ADR 0027, close both ISSUES entries`

---

**Suggested version bump (not performed):** minor — `v0.11.0` — a user-visible
command is removed, which under 0.x SemVer practice is a minor-line event; the Go
API is untouched. Owner decides whether and when.
