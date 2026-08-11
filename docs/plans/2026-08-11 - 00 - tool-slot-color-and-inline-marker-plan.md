# Tool outcome slot: color, inline remainder marker, leader-glyph centralization

- **Goal:** fix the first three ISSUES.md items (2026-08-11 snapshot): fold the `+N more lines` marker into `<tool-top-level-details>` so a collapsed lone call is one line shorter, paint the outcome slot in the `tool-marker` role (brighter when open), and centralize the hardcoded `⋯` leader glyph.
- **Date:** 2026-08-11
- **Status:** saved, unexecuted
- **Sized for:** ~200k-context host
- **Authoritative sources:**
  - `docs/layout/tool-layout.md` — canon for tool-block shape (`<tool-top-level-details>` defined at :49–53). Where it and `layout.md` disagree about a tool block, it wins (`layout.md:489`).
  - `layout.md` — global grammar: color roles (`tool-marker` prose ~:711–713, two-tone gray rule ~:722–726).
  - `docs/adr/0040-color-schemes-are-embedded-roles-with-user-shadowing.md` — every on-screen color is a stated scheme role; no computed colors.
  - `ISSUES.md` lines 5–9 (the three items this plan retires).
  - Line numbers throughout are as of commit `b6e5149`; where a line number has drifted, the named symbol/test wins.
- **Ratified design calls (owner, 2026-08-11, via AskUserQuestion unless noted):**
  1. Item-3 scope: centralize the glyph constant only; **no** user-config key for the glyph.
  2. Slot color role: **reuse `tool-marker`** — no separate slot role. `layout.md` is amended to match (the current spec reserves `tool-marker` for the marker line and paints the slot in the two-tone gray; that spec text changes).
  3. Slot is **always** colored (collapsed and expanded); the open state uses a ~20 % brighter variant. Mechanism (plan author, under ADR 0040 + the standing best-long-term directive): a paired role `tool-marker-bright`, mirroring the existing `muted` / `muted-bright` pair — never a runtime-computed color. Values pinned in item 2.
  4. Inline marker format: `` · +N more lines`` — appended with the middle-dot separator typed stats already use. No separate inline styling: it wears whatever the slot wears (tool-marker tone, or `error` red on failed summaries — error dominance unchanged).
  5. The row freed by removing the marker line is **not** backfilled with an extra body line — collapsed blocks get shorter (per the issue's own motivation: compactness). `collapsedBodyCap` and the shown-lines computation stay as they are. (Ratified by the issue text itself.)
- **Standing requirements:** skills: coding-standards. Run `make check` before each commit (the verifier commits). Any authorized deviation from item text lands as a dated NOTES line under the item.
- **Out of scope:** group members and super-group rows (they already collapse to one row and emit no marker); any user-facing config for glyphs; touching `VERSION` / CHANGELOG release headings (see closing note); the confinement item at ISSUES.md line 11.

---

## 1. Centralize the leader-dot glyph — ✅ DONE (2026-08-11)

NOTES (2026-08-11): the move left `toolleader.go`'s const-block doc comment describing a constant no
longer there, so that comment now covers `leaderGap`/`leaderMinDots` and points at the glyph's new
home; the glyph's own trailing comment keeps the one-glyph-per-cell sentence verbatim and gains a
pointer back to `leaderRowIn` and the `tool-leader` role, since it now sits far from its use site.

**What:** Move the `glyphLeaderDot = "⋯"` constant from `internal/tui/toolleader.go:17` into the central glyph block in `internal/tui/theme.go` (:36–52, beside `glyphBranch` et al.), keeping its one-glyph-per-cell comment intact — the row arithmetic in `leaderRowIn` depends on that invariant. Pure move, zero behavior change. The color half of the ISSUES item is already satisfied: the `tool-leader` role exists (`internal/scheme/scheme.go:57`, both scheme yamls). Retire the ISSUES.md dotted-line item (line 9) and add a CHANGELOG bullet under `[Unreleased]`.

**Tests:** none new (pure move); the existing `internal/tui` suite must stay green, including `docmap_test.go` (no new file, so no `doc.go` change expected).

**Acceptance:**
- `go build ./...`
- `go test ./internal/tui/`
- `grep -c glyphLeaderDot internal/tui/theme.go` ≥ 1 and `grep -c 'glyphLeaderDot =' internal/tui/toolleader.go` = 0

**Commit:** `refactor(tui): move the leader-dot glyph into the central glyph block`

---

## 2. Add the `tool-marker-bright` scheme role — ✅ DONE (2026-08-11)

NOTES (2026-08-11): `darkPalette` and `TestEmbeddedDarkMatchesPinnedPalette` no longer exist —
`5555c44` deleted them to make the scheme tests colour-agnostic (owner call: a scheme still under
tuning must never fail a test by changing a value), so only `roleTable` gained a row and no test
pins the new hex. The colour-agnostic guard that carries the item's intent instead is
`TestBuiltinSchemesKeepBothMarkerStepsDistinct` in `builtins_test.go`, mirroring the existing
`muted`/`muted-bright` pair guard: it fails a scheme in which opening a block leaves the marker
tone where it was. Two knock-ons of adding a key: the chrome-accents comment column in both
shipped yamls was re-aligned to the new longest key (the files are handed to users verbatim by
`/color-scheme export`, and each block aligns its comments), and `CONTEXT.md`'s stated role count
went 25 → 27 (it was already one stale — `tool-leader` landed without it).

**What:** New role in the scheme, the open-state sibling of `tool-marker`, mirroring the `muted` / `muted-bright` pattern:
- `internal/scheme/scheme.go`: new `Scheme` field with tag `yaml:"tool-marker-bright"`, placed beside `tool-marker` (:52). Reflection (`roleKeys`/`fieldIndex`) picks it up automatically.
- `internal/scheme/schemes/dark.yaml`: `tool-marker-bright: "#E6C099"` — `tool-marker` `#E0B080` stepped ~20 % toward white; comment mirrors the `muted-bright` "same voice, higher volume" wording.
- `internal/scheme/schemes/light.yaml`: `tool-marker-bright: "#2F5884"` — `tool-marker` `#3b6ea5` stepped ~20 % toward black, because on a light terminal "up" is darker (see light.yaml's own `muted-bright` comment).
- Test tables: add a row to `roleTable` (`internal/scheme/scheme_test.go:15–42`) and to `darkPalette` (:46–90). `TestBuiltinSchemesStateEveryRole` and `TestRoleTableCoversEveryRole` then enforce parity; `TestEmbeddedDarkMatchesPinnedPalette` pins the hex.

No consumer yet — the TUI binding lands in item 3.

**Tests:** the four scheme guards above (`go test ./internal/scheme/`).

**Acceptance:**
- `go test ./internal/scheme/`
- `grep tool-marker-bright internal/scheme/schemes/dark.yaml internal/scheme/schemes/light.yaml` shows both pinned values

**Commit:** `feat(scheme): add tool-marker-bright, the open-state tone of the marker role`

---

## 3. Paint the outcome slot in tool-marker, brighter when open

Depends on item 2.

**What:** `<tool-top-level-details>` — the right-aligned outcome slot rendered by `leaderRowIn` (`internal/tui/toolleader.go:168`) — currently wears the two-tone detail gray via `summaryStyle` → `detailStyle` (`toolleader.go:184`, `toolbranch.go:387`), which under `dark` is the same hex as the leader dots. Change:
- `internal/tui/theme.go`: bind the new role — `toolMarkerBright` color local and style field, following the existing `toolMarker` wiring (declare ~:150–155, bind ~:260–283, construct ~:295–303).
- `internal/tui/toolleader.go` `summaryStyle`: failed summaries keep `th.errorText` (unchanged); **all** other summaries — every `Kind`, including promoted/quoted ones like `[1 files found, showing 1-1]` — return `th.toolMarker` when collapsed, `th.toolMarkerBright` when expanded. The `detailStyle` delegation leaves `summaryStyle`; `detailStyle` itself is untouched (body details elsewhere still use it).
- Spec amendments (this item owns them all): `layout.md` two-tone-gray rule (~:722–726) — the summary leaves the "its target, its summary and its body" list; `layout.md` `tool-marker` prose (~:711–713) — the role now also paints the outcome slot, bright variant when open; `docs/layout/tool-layout.md` :49–53 — the slot definition states its color (tool-marker / tool-marker-bright, error red on failure). Update the `tool-marker` comment in both scheme yamls to mention the slot.
- Retire ISSUES.md line 7; CHANGELOG bullet under `[Unreleased]`.

**Tests:**
- `internal/tui/render_test.go` `TestLeaderRowSpendsItsRoomInOrder` (:1465): the slot-tone assertion at :1578–1580 changes from `detailTone(...)` to `toolMarker`/`toolMarkerBright` by expanded state; the failure-red case (:1575) stays.
- `internal/tui/theme_test.go`: add `toolMarkerBright` to the fixture scheme (:40–47) and the role→style assertion table (:81–85).
- Any other assertion pinning the slot's old tone that fails under `go test ./internal/tui/` gets updated to the new roles — same semantics, new color.

**Acceptance:**
- `go test ./internal/tui/ ./internal/scheme/`
- `grep -A3 'func summaryStyle' internal/tui/toolleader.go` shows the toolMarker/toolMarkerBright branch and no `detailStyle` call

**Commit:** `feat(tui): paint the outcome slot in tool-marker, brighter when open`

---

## 4. Fold the +N-more-lines marker into the outcome slot (lone-call collapsed shape)

Depends on item 3 (the slot's color now covers the inline marker; the same tests move again).

**What:** A collapsed lone call (non-grouped block, `renderToolBlock` → `renderToolBranch`) currently emits the remainder marker as its own row — targeted shape at `internal/tui/toolbranch.go:91`, targetless shape at :68, text produced by `collapseAtCap` (:241). Change, for both shapes:
- The marker row is no longer emitted. Its text joins the slot: the leader row's summary becomes `<summary> · +N more lines` whenever hidden lines exist. The concatenation happens where `renderToolBranch` (which knows the hidden count) meets the leader-row call — thread the remainder text into `leaderRow`/`leaderRowIn` rather than mutating `tv.Summary` at the presenter seam. No separate styling: the tail wears the slot's style (design call 4), and the enlarged slot participates in `leaderRowIn`'s existing room-spending order unchanged.
- Shown-body computation is untouched; blocks that had a marker row are now exactly one row shorter (design call 5). Update the row-budget comment at `toolbranch.go:93–98` accordingly.
- The marker's dedicated click stop (`targetMarker`, `mouse.go:501`) loses its collapsed-lone-call row: retire or narrow the stop to whatever shapes still emit a marker row (group/super-group emit none today; if nothing does, remove the stop). The row itself remains the expand/collapse click target as today.
- Group members and super-groups: unchanged (out of scope).
- Spec amendments (this item owns them): `docs/layout/tool-layout.md` :49–53 — the slot definition gains the `` · +N more lines`` tail; `layout.md` `tool-marker` prose — the marker is no longer its own line in the lone-call collapsed shape.
- Retire ISSUES.md line 5; CHANGELOG bullet under `[Unreleased]`.

**Tests:** (all in `internal/tui/`)
- New/extended assertion: a collapsed lone call with hidden body lines renders `` · +N more lines`` inside the leader row, in the slot's style, and emits no marker row.
- Update the pinned shapes: `render_test.go:1110` (exact `"    +N more lines"` string), `TestCollapsedPaintTruncatesRetainedBodies` (:1122), `TestCollapsedBlockStandsAtMostThreeRows` (:1390 — budget drops by the marker row), `TestTargetlessBlocksCollapseToTheBudget` (:3694), `TestEveryToolShapeCollapsesInsideTheRowBudget` (:3778), `TestRemainderMarkerCarriesItsOwnStyle` (:3066 — rewrite to assert the inline tail), `TestRenderSingleCallSharesTheGroupShape` (:974).
- Click/cursor stops: `mouse_test.go:804, 886` and `blockcursor_test.go:53` updated to the marker row's removal.

**Acceptance:**
- `go test ./internal/tui/`
- `make check`
- `grep -n '+.*more line' internal/tui/toolbranch.go internal/tui/toolleader.go` shows the remainder reaching the leader row, not an `out.add(...)` marker row in the lone-call path

**Commit:** `feat(tui): fold the +N-more-lines marker into the outcome slot for lone calls`

---

## Suggested version bump

Three user-visible TUI changes shipped together: suggest a micro bump (v0.12.13 → v0.12.14) once all items land. Owner decides; no item touches `VERSION`.
