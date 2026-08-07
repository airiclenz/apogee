# Type-to-filter for the selector pop-ups — implementation plan

- **Goal:** every selector overlay — the six picker kinds (`/model` over advertised models, `/model` over launch profiles, `/server`, `/schedule`'s cycle and mode panes, `/schedule-stop`) and the `/sessions` browser — filters as you type: printable keys build a case-insensitive substring filter that prunes the rows live, so a 300-model OpenRouter offering is a few keystrokes away instead of an arrow-scroll through an 8-row window. The prompt box below is untouched — the filter lives in the overlay's own state and is painted inside the pane.
- **Date:** 2026-08-06
- **Status:** not started
- **Authoritative sources:**
  - `layout.md` §"One overlay for 'which one?'" (~L1080–1102) and the `/sessions` browser section — the overlay grammar this plan extends; item 5 amends both to match.
  - `internal/tui/picker.go`, `internal/tui/sessions.go`, `internal/tui/popup.go` — current-behavior ground truth. The popup module's contract header (popup.go top comment) binds how panes compose; extend it, never fork it.
  - ADR 0011 / `internal/tui/doc.go` — the value-copied Model rule. All new state must be plain values.
  - The **Ratified design calls** list below — resolved with the owner via AskUserQuestion in the 2026-08-06 planning session; ground truth for every behavioral choice in this plan.
- **Ratified design calls** (owner, 2026-08-06, via AskUserQuestion):
  1. **Widest scope:** all six picker kinds AND the `/sessions` browser filter. Nothing is opt-out — one uniform mechanism.
  2. **Direct typing everywhere.** Any printable key starts or extends the filter; there is no activation key. In the `/sessions` browser the three letter verbs move to control chords to free the letters: `r`→`ctrl+r` rename, `d`→`ctrl+d` delete, `a`→`ctrl+a` this/all. The delete-confirm fold's `y`/`n` and the rename fold's editing keys are unchanged — those folds are modal surfaces of their own and no filter is typed inside them.
  3. **esc always closes the overlay** — one meaning, even mid-filter, so the hint's `esc close` is never conditionally wrong. Backspace is the filter's undo.
  4. **The filter is shown on its own line under the title, with a blank spacer line above and below it** (owner's note: "add some margin / spacer lines to not have it all bunched up").
  5. **Filtered rows keep the offering's own order.** The filter only prunes, never reorders — no rank tiers, no fuzzy scoring; rows never jump under the cursor while typing.
- **Author-derived bindings** (plan author, 2026-08-06 — derived from the calls above and existing contracts, recorded so their authority survives the session):
  - A. **Matching rule:** a row survives when the lowercased filter is a substring of the row's display cells joined with a single space, lowercased. All cells participate, marker cells included — filtering `running` to find live profiles is legitimate. Space is a filter character like any other (the overlay is modal; space is no verb there).
  - B. **Zero matches keep the pane open:** title, filter line, no rows, no highlight (`selected = -1`, the popup module's existing convention); `⏎` no-ops via the existing `n == 0` path; backspace recovers. No "no matches" prose row — a visible filter over zero rows already says it.
  - C. **The filter dies with the overlay.** It is part of the overlay's inline state, so the existing whole-struct zeroing on close/accept (`m.picker = picker{}`, the browser's close path) clears it. No path may carry a stale filter into the next open.
  - D. **One filtered view, three consumers.** The surviving rows are computed in ONE place as indices into the kind's full offering; row painting, row counting, and the accept path all resolve through that same index list, so `⏎` can never take a row the pane did not paint. Every accept that today indexes an unfiltered list directly (`offeredModels()[selected]`, `m.opts.Servers[selected]`, `scheduleCycles[selected]`, …) is rewired through it.
  - E. **Hint wording:** every picker hint variant gains a leading `type to filter · ` segment; the browser hint becomes `type to filter · ↑/↓ select · ⏎ resume · ^r rename · ^d delete · ^a this/all · esc close`.
  - F. **Derived-per-frame stays true.** The filter applies where rows are derived, so a heartbeat landing under an open filtered picker re-derives the offering, re-applies the filter, and re-clamps the selection exactly as today.
- **Standing requirements:**
  - skills: coding-standards
  - Any authorized deviation from item text lands as a dated NOTES line under the item.
  - `make check` before every commit.
  - ADR 0011: the Bubble Tea `Model` is value-copied on every `Update` — the filter is a plain `string` on the overlay state structs and must stay one; no `strings.Builder` or other no-copy type anywhere it reaches (`TestModelNoBuilderByValue` is the guard).
  - Tests hit no real network; live-LLM tests stay gated behind `APOGEE_LIVE_ENDPOINT`.
  - No version identifier changes (VERSION, CHANGELOG release heading, tags) — see the closing note.
- **Out of scope:**
  - Ranked or fuzzy matching (foreclosed by ratified call 5).
  - Preferring `DiscoveredModel.DisplayName` over ids in `modelRows` (a separate decision for a separate session).
  - The `/` autocomplete dropdown (it already filters by the token being typed) and the ask/approval menus (decision surfaces, not searchable lists — `menuRows` panes are untouched).
  - Any change to the prompt editor / input box — the rejected stash-and-restore design must not creep back in.

## 1. The picker's filtered view — one seam for rows, count and accept — ✅ DONE (2026-08-07)

**What:** In `internal/tui/picker.go`: add `filter string` to the `picker` struct (plain value; every existing `picker{}` zeroing clears it — author binding C). Add the match helper implementing author binding A (lowercased filter substring of the row's cells joined with one space). Introduce one function that derives the filtered view for the open kind — the surviving `popupRow`s plus their indices into the kind's FULL offering (author binding D) — and rewire all three consumers through it: `pickerCount` returns the filtered count, `pickerRows`/`renderPicker` paint the filtered rows, and `acceptPicker` maps `m.picker.selected` through the index list before touching any underlying slice (`offeredModels()`, `m.opts.Servers`, `m.picker.profiles`, `scheduleCycles`, `scheduleModes`, and the `/schedule-stop` accept — refactor `acceptScheduleStop` too if it reads `selected` internally). An empty filter must be the identity view (indices `0..n-1`), so every existing behavior — clamping, wrapping, per-frame re-derivation (author binding F) — is unchanged when nothing has been typed. No key routing and no rendering change in this item: the filter can only be empty in the running TUI until item 2 lands, which is what keeps this item's diff behavior-neutral.

**Tests:** unit tests in `internal/tui`: the matching rule (case-insensitivity, joined-cell matching, space in the filter); for each picker kind a table-driven check that rows, count, and the accept target agree under a non-empty filter (set `m.picker.filter` directly); narrowing clamps an out-of-range selection; empty filter is the identity on all three consumers.

**Acceptance:**
- `go test ./internal/tui/ -run 'Picker'` passes (new tests included).
- `go build ./...` passes.
- `make check` passes.

**Commit:** `feat(tui): picker derives one filtered view for rows, count and accept`

## 2. Picker key routing — printable keys build the filter — ✅ DONE (2026-08-07)

Depends on item 1.

NOTES (2026-08-07): beyond the item's literal text, on the owner's dispatch decision (item 1's
verifier proved the defect): `acceptCycle` (`internal/tui/schedule.go`) is the overlay's ONE partial
reset — it moved `kind` and `selected` on but left `filter` standing, so a filter typed at the cycle
offering survived into the mode pane and could open it over zero rows. It now clears `filter` with
them, covered by `TestPickerCycleAcceptClearsTheFilter`. No other partial reset of `picker` exists
(the ↑/↓ cases only move the highlight within one kind; every other path zeroes the whole struct).

**What:** In `pickerKey` (`internal/tui/picker.go`): keep `esc`, `up`/`ctrl+p`, `down`/`ctrl+n`, and `enter` exactly as they are (they already operate on the filtered count after item 1); after those cases, printable input — the key message's text runes, space included — appends to `m.picker.filter`, and `backspace` trims the last rune (no-op on an empty filter). Every other key stays swallowed (the modal contract). `esc` closes outright even with a filter set (ratified call 3). Update the hints per author binding E: `pickerHint` and both `pickerHintFor` variants gain the leading `type to filter · ` segment.

**Tests:** typing narrows `pickerCount` live; backspace restores rows; esc with a non-empty filter closes the picker (no clear-first stage); enter accepts the highlighted FILTERED row, not the same index into the unfiltered offering; `ctrl+p`/`ctrl+n` still move the highlight rather than typing; an unrelated chord is still swallowed. Cover at least one non-model kind (e.g. the cycle picker) to pin the uniform-scope call.

**Acceptance:**
- `go test ./internal/tui/ -run 'Picker'` passes.
- `make check` passes.

**Commit:** `feat(tui): type-to-filter in the picker overlay`

## 3. Picker rendering — the filter line with breathing room — ✅ DONE (2026-08-07)

Depends on item 1.

NOTES (2026-08-07): BOTH spacers are the body block's own, which is the popup extension the item
allowed: `popupSpec.bodyPadAbove` plus a `bodyPadBelow` counterpart, drawn out of what the body's own
budget left over (`popupBodyPad`/`popupBodyPadLines`, documented in the contract header). The lower
one could not be the row block's existing `rowPadAbove`: a row pad is spent out of the ROW window, and
an offering longer than `maxPickerRows` fills that window by definition, so the pad would be dropped
exactly on the roomy terminals with lines to spare — and dropped by painting a NINTH row past the
pane's taste. Owned by the body, the pair costs the filter line's own claim three lines instead of
one, the picker's row demand and row cap stay `maxPickerRows` untouched, and the blanks survive
wherever the filter line does, the zero-match pane included. They give way — as a pair, the row
block's rule — only on a window whose grant cannot pay for the line and both blanks; the filter line
and the rows themselves are never what gives way.

**What:** `renderPicker` (`internal/tui/picker.go`) composes a `filter: <text>▌` line, shown only while the filter is non-empty, under the title and set off by one blank line above and one below (ratified call 4). Prefer the popup module's existing slots — the `body` field (drops when empty) plus the padding flags — and extend `popupSpec` minimally in `internal/tui/popup.go` (e.g. one additional pad flag) only if the exact visual cannot be met with the existing ones; any extension follows the popup contract header's style and is documented there. Budget honestly: the filter line and its spacers count toward the pane's height in the `popupBudget` call so a short window never overflows, and on windows too short for everything the pane gives up ROWS before it gives up the filter line — the filter is the thing being typed and must stay visible (use the `popupFloor` mechanics). Zero-match renders per author binding B: pane open, filter line visible, zero rows, no highlight.

**Tests:** render/paint tests in the house style (`paint_test.go` conventions): filter line plus both spacer lines present while filtering and absent otherwise; the zero-match pane; a short-window case where rows shrink but the filter line survives.

**Acceptance:**
- `go test ./internal/tui/ -run 'Picker|Popup'` passes.
- `make check` passes.

**Commit:** `feat(tui): picker paints the live filter line`

## 4. Sessions browser — letter verbs become chords, then type-to-filter — ✅ DONE (2026-08-07)

Depends on items 1 and 3 (reuses the matcher and the filter-line visual).

NOTES (2026-08-07): three things beyond the item's literal text, each following from it. (1) Reusing
item 3's filter-line visual meant TOUCHING `picker.go`: the composer is now one function
(`overlayFilterLine`) that both panes call, rather than the browser spelling the label, the cursor
and the escape-stripping a second time — `pickerFilterLine` delegates to it and paints exactly what
it painted. (2) `clampSelection` takes the filtered COUNT (`n int`) instead of the workspace, the
picker's and /settings' own signature: the count now depends on the filter and on the moment (the
rows carry relative times), so the caller derives the view and the clamp stays a pure function of it.
(3) The empty-workspace note row said "press a to see all" — the letter the rebinding took away — and
now says "press ^a". It is a live UI string rather than a doc, so item 5's doc ownership is intact.

**What:** In `internal/tui/sessions.go`, in this order inside the one item: (a) rebind the browse-fold letter verbs per ratified call 2 — rename `r`→`ctrl+r`, delete `d`→`ctrl+d`, this/all toggle `a`→`ctrl+a` (the delete-confirm fold's `y`/`n` and the whole rename fold are untouched); (b) add `filter string` to the browser's state struct (plain value, cleared by the existing close path — author binding C) with browse-fold key routing as in item 2: printable appends, backspace trims, esc closes as today; (c) filter the rows through item 1's matcher over the browser's display cells, composed AFTER the existing workspace-view filter, with count, painted rows, and every accept/verb target (resume, rename, delete) resolving through one shared index list (author binding D — `clampSelection` at `sessions.go:152` clamps against the filtered count); (d) paint the filter line with the same visual item 3 established; (e) set the hint per author binding E.

**Tests:** the chord verbs fire; the letters type into the filter instead — pin the regression that matters most: `d` while typing must never delete a session; filtered resume opens the session the pane showed, not the same index into the unfiltered list; delete/rename act on the filtered selection; zero-match; the toggle re-derives the filtered view over the other workspace scope; render of the filter line in the browser.

**Acceptance:**
- `go test ./internal/tui/ -run 'Session'` passes.
- `make check` passes.

**Commit:** `feat(tui): type-to-filter in the sessions browser; letter verbs become chords`

## 5. Docs — layout.md and the changelog — ✅ DONE (2026-08-07)

Depends on items 2, 3 and 4.

NOTES (2026-08-07): three shape deviations, none of them behavioral. (1) layout.md has NO dedicated
`/sessions` browser section — the browser is described in fragments (the height order, the Column
contract, the firing paragraph), all inside §"The prompt box's mini-language". The browser's amendment
is therefore a NEW bold-lead paragraph ("The `/sessions` browser types too, which is why its verbs are
chords") seated in that section immediately before the firing paragraph, which is where the browser's
own grammar already lives. (2) In §"One overlay for 'which one?'" the filter is likewise its own
bold-lead paragraph after the picker's, rather than a rewrite of the modal-keys sentence: that sentence
("It is modal — while it is open every key belongs to it") is the PREMISE the direct-typing call rests
on, so it stands and the new paragraph draws the consequence from it. Both places carry the full hint
string, so the acceptance grep counts them. (3) One clause beyond the two named places: the `/schedule`
paragraph now says the mode pane opens on a cleared filter, because item 2's `acceptCycle` NOTES made
that a behavior this plan changed and the cycle→mode handoff is described only there. (4) One file
beyond the item's two: `README.md` spelled the browser's OLD letter verbs ("`r` renames inline, `d`
deletes after a confirm, `a` toggles…", and "discarding is an explicit `d`"), which item 4 made false.
This item owns every doc edit, so they are corrected to the chords here rather than left wrong; README
spells no picker hint anywhere, so nothing else there moved.

**What:** Amend `layout.md` in both places this plan changed behavior (this item owns every doc edit — no other item touches docs): §"One overlay for 'which one?'" — the modal-keys sentence now says printable keys build a case-insensitive filter that prunes the rows (never reorders), describe the `filter:` line with its spacer lines, esc's unchanged close, and the zero-match pane; the `/sessions` browser section — the chord verbs (`^r`/`^d`/`^a`), direct typing, and the same filter line. Update the quoted hint strings wherever layout.md spells them. Add one `CHANGELOG.md` `[Unreleased]` → `### Added` entry describing type-to-filter across the selector pop-ups, naming the browser's verb rebinding explicitly (it changes existing muscle memory and deserves its own sentence).

**Tests:** none (docs only).

**Acceptance:**
- `grep -c "type to filter" layout.md` ≥ 2 (both amended sections).
- `grep -q "type-to-filter" CHANGELOG.md` (or equivalent wording in the Unreleased section).
- `make check` passes.

**Commit:** `docs: type-to-filter across the selector pop-ups`

---

**Suggested version bump** (not performed by this plan): a minor bump (`v0.8.0` → `v0.9.0`) once this lands — a user-visible feature across every selector surface plus rebound browser keys. Whether and when to bump is the owner's call.
