# Small guards — implementation plan

**Goal:** land the two small, fully-specified guards still open in `ISSUES.md`: the
`MechanismRegistry.Add` empty-ID guard and tab-aware width mirrors, each closing its parked
register entry.

**Date:** 2026-08-13
**Status:** unexecuted
**Sized for:** ~200k-context host.

**Authoritative sources:**
- `ISSUES.md` — the parked *Two doors left open by the Mechanism-registration collapse* entry
  (first bullet) and the parked *TUI width authority* entry (the widget-mirrors-tabs paragraph);
  both carry a "Planned in `2026-08-13 - 08`" note.
- ADR 0030 (`docs/adr/0030-the-tui-has-one-width-authority-and-it-mirrors-the-painter.md`) — the
  mirror-the-widget rule item 2 operates under; the vendored bubbles textarea is the oracle.
- ADR 0003 — the constraint-declared registry whose `Add` gates item 1 extends.

**Ratified design calls (owner, 2026-08-13, via AskUserQuestion in the plan-writing session):**
1. **Scope selection:** this plan = the two code guards plus their register closure. The register
   cleanup this plan originally carried (narration closures, the conventions actionability bar,
   the run-residuals section merge, the README reflow) was executed directly in the plan-writing
   session at the owner's request and is NOT in this plan; the `verifiedEntrySplice` message fix
   and the two live-apply gaps (`auto-title` in `applySettingFor`, `/model` re-pin) stay open in
   the register.
2. **Write-back actionability bar:** already landed — `ISSUES.md`'s conventions and the
   implement-plan skill's closeout prompt both carry it; this run's own closeout inherits the
   tightened rule.

**Standing requirements:**
- skills: coding-standards
- Any authorized deviation from item text lands as a dated NOTES line under the item.
- No version identifier changes (see the closing note).

**Out of scope:** everything plan `2026-08-13 - 07` owns (the `[P]`-marked register bullets); the
`verifiedEntrySplice` refusal message (waits for that plan's `configwrite_keysource.go` move); the
`auto-title` `applySettingFor` case and the `/model` re-pin gap; the approved
out-of-workspace-write Execute defect (owner call still pending); `hangingPrefixes` at block width
1–2 (a `layout.md` question, per its entry).

---

## 1. `MechanismRegistry.Add` rejects an empty `Descriptor.ID`

**What:** `Add` (`internal/domain/mechanism.go:234`) gates on the reserved experimental ID, on a
duplicate ID, and on the hook implementing no hook interface — not on the ID being non-empty, so a
hand-built row with a zero descriptor becomes a catalogued Mechanism with a blank canonical ID
(blank `MechanismFiredEvent` attribution, first in the stable tiebreak). Add the fourth gate beside
the reserved-ID one: an empty `m.Descriptor.ID` returns an error in the same voice as the existing
three (the exact wording is the implementer's, matching the `"apogee: mechanism ID %q is …"`
family). Extend `Add`'s doc comment (`:226-233`) to name the new gate. The catalogue path is
unreachable for this case (`register` panics at `init()` on an empty ID), so behaviour changes only
for embedder/test-built rows — exactly what the parked ISSUES entry asks for. The ISSUES closure is
item 3's, not this item's.

**Files:** `internal/domain/mechanism.go`, `internal/domain/mechanism_test.go`

**Tests:** `TestAdd_RefusesEmptyID` beside `TestAdd_RefusesReservedExperimentalID`
(`mechanism_test.go:39`), in the same style: a row with an empty `Descriptor.ID` and a valid hook is
refused with an error saying the ID is empty; the neighbouring tests pin that non-empty IDs still
register.

**Acceptance:** `go build ./... && go test ./internal/domain/`

**Commit:** `feat(domain): MechanismRegistry.Add refuses an empty Descriptor.ID`

## 2. The width mirrors expand tabs before measuring

**What:** `wrapRowStarts` (`internal/tui/inputaccent.go`, func at `:211`) measures the raw rune
slice, but the widget's own insertion paths run bubbles' `runeutil.Sanitizer`, which expands tabs —
so a value carrying a tab wraps differently in the widget than the mirror predicts, and both
mirrors are wrong on tabs (`inputContentRows`, `internal/tui/chromelayout.go:39`, sums
`wrapRowStarts` and inherits whatever it does). Per the parked width-authority entry: expand tabs
the same way the widget's sanitizer does, inside `wrapRowStarts` before measuring, so both mirrors
inherit the fix. Derive "the same way" from the vendored bubbles textarea source, and let the
oracle tests arbitrate (the existing harness compares against a real widget). Extend the function's
doc comment to name the tab expansion. Do not touch the transcript-side `expandTabs` (render.go) —
it serves the painter, not the input mirror; if the two expansions turn out identical, reusing it
is the implementer's call, noted in the sidecar. The ISSUES closure is item 3's, not this item's.

**Files:** `internal/tui/inputaccent.go`, `internal/tui/inputaccent_test.go`,
`internal/tui/render_test.go`

**Tests:** extend both widget-oracle suites with tab-bearing lines — the mirror test driving a real
textarea (`inputaccent_test.go:60-135`) and `TestWrapRowStartsMirrorsTheWidget`
(`render_test.go:5108`): a line with a leading tab, a mid-word tab, and a tab near the wrap column
must produce the same row count and row starts the widget itself uses.

**Acceptance:** `go build ./... && go test ./internal/tui/`

**Commit:** `fix(tui): the width mirrors expand tabs the way the widget sanitizer does`

## 3. ISSUES sweep — the two parked entries this plan resolves leave the register

Depends on items 1 and 2.

**What:** per the register convention (resolved work leaves; the changelog is the closed trail):
- ***Two doors left open by the Mechanism-registration collapse***: remove the first bullet —
  item 1 built exactly the guard it asked for — and reword the entry's intro from "Both were named
  out of scope" to the singular; the second bullet (`Deps` construction stays in `deriveDeps`) is
  untouched.
- ***The TUI width authority — what it did not convert***: remove the "widget mirrors mis-measure
  tabs" paragraph — item 2 landed its fix — and adjust the intro's "the two width entries below"
  sentence to name only `hangingPrefixes` at block width 1–2, which stays open.
The per-item commits already carry the `CHANGELOG.md` entries for the code changes; this item adds
no changelog text of its own.

**Files:** `ISSUES.md`

**Tests:** none (register maintenance).

**Acceptance:** `grep -c "does not reject an empty" ISSUES.md` returns 0;
`grep -c "mis-measure tabs" ISSUES.md` returns 0;
`grep -n "hangingPrefixes" ISSUES.md` still finds the kept width entry;
`grep -n "does not construct them" ISSUES.md` still finds the kept second door.

**Commit:** `docs(issues): the empty-ID and tab-mirror entries close to the changelog`

---

**Suggested version bump:** micro once executed — item 1 is a small exported-surface behaviour
change (a new refusal in `Add`) and item 2 a user-visible rendering fix; the owner decides, no item
touches `VERSION` or `CHANGELOG` release headings.
