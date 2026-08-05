# Slash-menu relevance ranking — plan

- **Goal:** the `/` suggestion popup ranks candidates by match quality instead of pure
  scan order: typing `/imple` must list `/implement-plan` (prefix match) before
  `/feature-implementation` (substring match). Today both are skills matched by
  case-insensitive substring and returned in catalog (`DisplayName`) order, so the
  substring match sorts first and is what Enter/Tab accepts.
- **Date:** 2026-08-04
- **Status:** completed (2026-08-05) — all items ✅, archived
- **Authoritative sources:**
  - `layout.md` § "The prompt box's mini-language" (the dropdown spec; amended by item 2).
  - Owner directive (2026-08-04 session): relevance order, prefix matches first.
  - Pinned baseline: commit `277a057`.
- **Standing requirements:** `skills: coding-standards`. Run `make check` before any
  commit. Never touch VERSION / CHANGELOG release headings (see closing note).
- **Decided design (no open calls):** rank by **match-quality tier only** —
  0 = exact, 1 = prefix, 2 = substring — computed case-insensitively, with a
  **stable** sort so ties keep today's scan order (commands in registry order, then
  skills in catalog order). Consequences, all intended:
  - Bare `/` (empty partial): every name is a prefix match → one tier → order
    unchanged; the full menu still reads alphabetically.
  - `/c`: all five candidates are prefix matches → pinned order
    `clear, compact, confine, continue, clean-code` unchanged.
  - Commands only ever exact/prefix-match (their filter is unchanged), so a command
    never sorts below an equally-ranked skill; a *prefix-matching skill* now outranks
    nothing it shouldn't — it only overtakes *substring-matching* rows.
  - Filter semantics are NOT widened: commands stay case-sensitive prefix-filtered,
    skills stay case-insensitive substring-filtered on ID and DisplayName. Only the
    ordering of the survivors changes.
- **Out of scope:** fuzzy/subsequence matching; scoring by match position or length
  within a tier; widening command matching to substring; the `@` file completion
  region (`filecache.go`); interleaving commands and skills within a tier beyond what
  the stable sort produces; any change to `commandSpecs` registry order or to
  `TestCommandSpecsReadAlphabetically`.

## 1. Rank slash-menu suggestions by match-quality tier — ✅ DONE (2026-08-05)

NOTES (2026-08-05): the tie-stability bullet is satisfied by the existing, untouched
`TestSlashMenuMergesCommandsAndSkills` (`/c`, mixed tiers) rather than by a duplicate of it; the new
`TestSlashMenuKeepsScanOrderWithinOneRankTier` covers the pure single-tier case (bare `/` keeps the
commands' table order, then the catalog order) that no existing test pinned. A `### Fixed` CHANGELOG
entry was added under `[Unreleased]` per repo convention (no version identifier touched).

**What:** in `internal/tui/autocomplete.go`:

- Add a pure helper `slashMatchRank(partial, name string) int`: lowercase both, return
  `0` if equal, `1` if `name` has `partial` as prefix, `2` if it contains `partial`,
  `3` otherwise (callers only pass names that already passed their filter, so 3 is a
  defensive value, never surfaced). Empty `partial` ranks everything 1.
- Add a `rank int` field to `acItem` (`autocomplete.go:58`).
- `commandSuggestions` (`autocomplete.go:286`): set `rank: slashMatchRank(partial, c.name)`
  on each item. Filtering is unchanged.
- `skillSuggestions` (`autocomplete.go:393`): collect ALL matching skills first —
  remove the in-loop `maxAutocompleteItems` break — setting each item's rank to the
  best (lowest) of `slashMatchRank(partial, sk.ID)` and
  `slashMatchRank(partial, sk.DisplayName)`; then `sort.SliceStable` by rank and
  truncate to `maxAutocompleteItems`. This also fixes the latent cap-before-rank
  defect where a prefix-matching skill alphabetically past position 8 would be
  dropped while weaker substring matches were kept. The `/skill <arg>` picker uses
  this same function and inherits the ranking.
- `slashSuggestions` (`autocomplete.go:347`): after merging the command and skill
  halves, `sort.SliceStable` the combined slice by rank.
- Do not touch `computeAutocomplete`'s selection logic: `selected` stays zero-valued,
  so the best-ranked row becomes the default highlight for free.

**Tests** (in `internal/tui`, alongside the existing autocomplete/skill tests):

- Table-driven unit test for `slashMatchRank`: exact beats prefix beats substring;
  case-insensitive; empty partial → 1.
- Reproduction test: a skill catalog containing `feature-implementation` and
  `implement-plan` (no colliding commands), partial `imple` → item values exactly
  `["implement-plan", "feature-implementation"]` and `selected == 0`, so Tab/Enter
  accepts the prefix match.
- Tie-stability test: assert `/c` still yields the pinned merged order (the existing
  `TestSlashMenuMergesCommandsAndSkills` at `skill_test.go:256` must pass UNMODIFIED —
  it is the regression guard; do not rewrite it to fit).
- Cap-after-rank test: nine-plus matching skills where the sole prefix match sorts
  alphabetically last → it must appear (first), with the list still capped at 8.
- Existing ordering tests (`TestComputeAutocompleteCommands` `minilang_test.go:556`,
  `TestCommandDropdownOffersSkill` `skill_test.go:136`,
  `TestSlashMenuShadowsCollidingSkillID` `skill_test.go:366`) pass unmodified.

**Acceptance:** `go test ./internal/tui/ -count=1` green; `make check` green.

**Commit:** `fix(tui): rank slash-menu suggestions by match quality, prefix first`

## 2. Amend the layout spec's dropdown-ordering prose — ✅ DONE (2026-08-05)

NOTES (2026-08-05): the item's "around lines 804–817" hint was stale — the "One dropdown for `/`"
paragraph now sits at `layout.md:888–907`. The named section and paragraph were authoritative and
both of their ordering claims (the "commands first … then skills" clause and the "read
**alphabetically**" sentence) were rewritten there; no other prose moved.

Depends on item 1.

**What:** in `layout.md` § "The prompt box's mini-language" (the "One dropdown for `/`"
paragraph, around lines 804–817): the sentence "The command rows read
**alphabetically**, so the menu can be scanned without knowing the table behind it"
and the "commands first, prefix-matched … then skills" description no longer tell the
whole truth. Rewrite the paragraph's ordering claims to state: rows rank by match
quality — exact, then prefix, then substring — and ties keep the scan order (commands
alphabetically, then skills), so the bare `/` menu still reads alphabetically while a
typed partial like `imple` surfaces `/implement-plan` above `/feature-implementation`.
Preserve the paragraph's surrounding claims (every verb discoverable, eight-row
budget, hint line) untouched.

**Tests:** none (prose-only).

**Acceptance:** `grep -n "match quality" layout.md` finds the amended paragraph;
`grep -c "read \*\*alphabetically\*\*" layout.md` returns 0; `make check` green.

**Commit:** `docs(layout): describe relevance-ranked slash-menu ordering`

---

**Suggested version bump:** patch (v0.10.16 → v0.10.17) — user-visible UX bug fix in
the slash menu. Not performed by this plan; the owner decides.
