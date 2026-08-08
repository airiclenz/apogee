# Tool-header color role plan

**Goal:** give tool-call block headers (`Run`, `List Dir`, `Sub-Agent`, …) their own
color role `tool-header` in the scheme system, instead of reusing the `code` role that
also paints inline code and fenced code blocks.

**Date:** 2026-08-08 · **Status:** unexecuted

**Authoritative sources:**
- ADR 0040 (`docs/adr/0040-color-schemes-are-embedded-roles-with-user-shadowing.md`) —
  the `Scheme` struct's yaml tags are the single definition of the role vocabulary;
  parsing is forgiving (missing key → inherits the dark default), so no migration is
  needed for user scheme files.
- `internal/scheme/scheme.go` (struct + reflection-driven role table) and
  `internal/tui/theme.go` (style wiring), as of commit `0d6741a`.
- `layout.md` §"The rules behind the tool-call sketch" — the prose spec for the label.

**Ratified design calls** (owner, 2026-08-08, via AskUserQuestion):
1. Role name is **`tool-header`** (yaml key `tool-header`, struct field `ToolHeader`).
2. Dark scheme: `code` keeps the blue **`#80AAFF`** (the currently uncommitted
   working-tree edit is intentional and part of this plan); `tool-header` gets the
   orange **`#f0883e`** (the value tool headers had before that edit).
3. Light scheme: `code` stays **`#bc4c00`**; `tool-header` gets the blue **`#1f6feb`**.
4. The sub-agent rail (`subRail`: the `│` rail + `⤷ Sub-Agent` descent label) **follows
   `tool-header`** — the "one tone for the whole sub-agent frame" look is kept, on the
   new role.

**Working-tree note for the executor:** `internal/scheme/schemes/dark.yaml` is dirty at
plan-save time (the ratified `code: "#80AAFF"` edit). At the Phase 0 dirty-tree consult,
the right choice is to fold that edit into item 1's work — it is part of design call 2
and belongs in item 1's commit. `TestEmbeddedDarkMatchesPinnedPalette` is red until
item 1 lands; that is expected.

**Standing requirements:** skills: coding-standards. Run `make check` before committing.

**Out of scope:**
- No changes to `mdCode`, `mdCodeBlock`, `popupEdit`, `popupAccent` — they stay on the
  `code` role (comment wording may be updated, styling must not change).
- No changes to the `tool-marker`, `mode-auto`, or `prompt-toggle` roles or values.
- No edits to ADR 0040's text or to released CHANGELOG entries — historical records
  keep their "24 roles" wording; only living docs (README, CONTEXT.md, layout.md) get
  the new count.
- No VERSION / release-heading / tag changes (see the closing note).

## 1. Add the `tool-header` role to the scheme package — ✅ DONE (2026-08-08)

NOTES (2026-08-08): the working tree was already clean at execution — the ratified
`code: "#80AAFF"` edit had landed in commit `51e9f65`, so there was nothing to absorb and
dark.yaml only gained the new `tool-header` line.

NOTES (2026-08-08): that same commit left `darkPalette`'s `tool-marker` pin at the old
`#80B0FF` while dark.yaml ships `#FFB050`, so `TestEmbeddedDarkMatchesPinnedPalette` was
red for a second role the item does not name; the pin was corrected here (test-only, the
`tool-marker` role's value is untouched) because the item's acceptance requires
`go test ./internal/scheme/...` green.

NOTES (2026-08-08): the trailing-comment column of the "semantic" group in both shipped
YAML files was re-aligned, since the new longer `tool-header:` key would otherwise break
the alignment those files keep per group. Whitespace only; no values changed.

**What:**
- `internal/scheme/scheme.go`: add `ToolHeader string` with yaml tag `tool-header` to
  the `Scheme` struct, declared directly after `Code` (~line 37). Reflection
  (`buildRoles`) picks it up; no other Go change in the package.
- `internal/scheme/schemes/dark.yaml`: keep the working tree's `code: "#80AAFF"` edit
  (ratified, design call 2 — this item's commit absorbs it) and add, directly under the
  `code:` line: `tool-header: "#f0883e"` with a trailing comment naming what it paints
  (tool-call block headers + the sub-agent rail). Comments are user-facing — `Export`
  writes the file verbatim.
- `internal/scheme/schemes/light.yaml`: add `tool-header: "#1f6feb"` directly under its
  `code:` line, same comment style.
- `internal/scheme/scheme_test.go`: add the `tool-header` entry to `roleTable` in
  struct-declaration order (right after `code`); in `darkPalette`, add
  `"tool-header": "#f0883e"` **and** update the existing `"code"` pin from `#f0883e` to
  `#80AAFF` (this un-reds `TestEmbeddedDarkMatchesPinnedPalette`).
  `TestRoleTableCoversEveryRole` and `TestBuiltinSchemesStateEveryRole` then guard
  completeness automatically.
- `internal/scheme/builtins_test.go`: add a distinctness guard asserting
  `tool-header != code` in every built-in scheme (same shape as the existing
  muted/muted-bright guard at ~line 62) — the separation is the whole point of the role.

**Tests:** the test edits above; `go test ./internal/scheme/...` fully green.

**Acceptance:**
- `go test ./internal/scheme/...` → PASS.
- `grep -n 'tool-header' internal/scheme/schemes/dark.yaml internal/scheme/schemes/light.yaml`
  → one hit per file with the ratified hex values (`#f0883e` dark, `#1f6feb` light).
- `grep -n 'code: "#80AAFF"' internal/scheme/schemes/dark.yaml` → present.
- `git status --porcelain` after commit → clean (the dark.yaml edit was absorbed).

**Commit:** `feat(scheme): add tool-header role so tool headers stop sharing the code color`

## 2. Paint tool headers and the sub-agent rail with the new role — ✅ DONE (2026-08-08)

Depends on item 1.

NOTES (2026-08-08): the colour local is named `toolHeaderFg`, not the item's literal `toolHeader` —
`theme` already has a *style* field called `toolHeader` (the ✦ Label header itself), and the file's
convention for exactly this collision is the `Fg` suffix (`toolMarkerFg`, `promptToggleFg`, `errFg`).
The acceptance grep for `toolHeader` still matches.

NOTES (2026-08-08): `TestNewThemeTakesItsColoursFromTheScheme` gained an `mdCode fg` → `s.Code` row
alongside the two the item names. `toolLabel` was that table's ONLY sample of the `code` role, so
re-pointing it at `ToolHeader` would have dropped `code` out of the sampled set and quietly broken the
test's stated invariant ("a swap between any two of the sampled roles fails here").

NOTES (2026-08-08): three further stale-hue comments were reworded in `theme.go` — the `mdCode` /
`mdCodeBlock` field docs and the markdown-group lead (all said "orange"; dark's `code` is now blue)
and `popupAccent`'s field doc ("accent-orange"). The item's out-of-scope clause explicitly permits
comment wording updates on these four styles; no styling changed. `doc.go` ~line 400 was left as-is:
it names the `toolLabel` role rather than `code`, and "bold-orange" stays true under dark.

**What:**
- `internal/tui/theme.go`: introduce `toolHeader := lipgloss.Color(s.ToolHeader)`
  alongside the existing `code` local (~line 265); switch `toolLabel` (~line 290) and
  `subRail` (~line 295) from `code` to `toolHeader`. `mdCode`, `mdCodeBlock`,
  `popupEdit`, `popupAccent` stay on `code`.
- Update the now-stale comments in the same file: the `toolLabel` field doc (~line 130,
  "bold, orange — the `code` role's tone…"), the `subRail` doc (~line 150), and the
  `popupAccent` comment (~lines 346–349, which claims the `code` role is "the orange
  the tool label, the sub-agent rail and the auto-mode marker all carry" — after this
  item the tool label and rail no longer carry it). Describe roles by name
  (`tool-header`, `code`), not by hue — dark's `code` is now blue.
- `internal/tui/doc.go` ~line 400 mentions "the [theme] toolLabel role" — adjust the
  wording if it names `code`.
- `internal/tui/theme_test.go`: the all-roles `scheme.Scheme` literal in
  `TestNewThemeTakesItsColoursFromTheScheme` (~lines 39–46) gains `ToolHeader` with a
  hex distinct from the literal's `Code` value; the `toolLabel fg` expectation
  (~line 73) becomes `s.ToolHeader`; add a `subRail fg` → `s.ToolHeader` expectation if
  the table lacks one. `TestDefaultThemeKeepsTheDarkPalette` (~line 109): update any
  pinned hexes it checks for these styles.
- `internal/tui/render_test.go`: `TestSubRailPaintedInToolHeaderOrange` (~line 633)
  asserts against `scheme.Default().Code` — switch to `scheme.Default().ToolHeader`
  (the name stays truthful: dark's tool-header is the orange). Check the other
  toolLabel-adjacent assertions (~lines 654, 1376, 2469–2485) still pass and still
  compare against the intended role.

**Tests:** the test edits above; `go test ./internal/tui/...` fully green.

**Acceptance:**
- `go test ./internal/tui/...` → PASS.
- `grep -n 'toolHeader' internal/tui/theme.go` → `toolLabel` and `subRail` both use it.
- `grep -rn 's.Code' internal/tui/theme.go` (or equivalent) confirms `mdCode`,
  `mdCodeBlock`, `popupEdit`, `popupAccent` still read the `code` role.

**Commit:** `feat(tui): paint tool headers and sub-agent rail with the tool-header role`

## 3. Update the living docs to the 25-role vocabulary

Depends on items 1 and 2. This item owns ALL doc amendments for this plan.

**What:**
- `layout.md`: §"The rules behind the tool-call sketch", "The label." paragraph
  (~lines 480–482) — the label is bold in the scheme's `tool-header` role (`#f0883e`
  under `dark`), no longer `code`. §"What 'colour' means everywhere below" (~line 89) —
  role count 24 → 25. Sweep the other in-file references that tie the label's or the
  sub-agent rail's color to the `code` role (~lines 624, 733) and re-point them at
  `tool-header`; the word "orange" stays correct for dark.
- `CONTEXT.md`: the "Color scheme" term entry (~lines 519–538) — "one key per role —
  24 of them" → 25; if the entry names example roles, `tool-header` is a good addition.
- `README.md`: the "Colours you choose" bullet (~lines 176–181) — "24 semantic roles"
  → 25.
- Do NOT edit ADR 0040 or released CHANGELOG entries (out of scope — historical).

**Tests:** none (prose only).

**Acceptance:**
- `grep -rn '24 semantic roles\|24 of them' README.md CONTEXT.md layout.md` → no hits.
- `grep -n 'tool-header' layout.md` → the label paragraph names the new role.
- `make check` → PASS (whole-repo backstop before the final commit).

**Commit:** `docs: document the tool-header color role (25 roles)`

---

**Suggested version bump (not performed):** patch — `v0.12.6` → `v0.12.7` — a small
user-visible feature (one new scheme role, default palettes adjusted), consistent with
how recent `feat` commits landed in the `v0.12.x` line. Bumping VERSION/CHANGELOG is
the owner's call.
