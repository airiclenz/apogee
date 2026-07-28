# Plan — Staged-interjection strip styling (indent + full-width black band)

**Date:** 2026-07-28
**Status:** READY (not grilled — the *Design decisions* below derive from the ISSUES entry,
the theme's existing full-width-band posture (the status line), and CONTEXT.md's interjection
vocabulary; the mechanical design is grounded against the working tree at `97552a3` + the
in-flight plan-00 edits, which touch no file this plan touches).
**Source:** `ISSUES.md:8` — "scheduled messages should be printed 2 spaces to the right when
still scheduled. They also should have a black background and one empty line (also with a
black background) above and below all scheduled messages. scheduled messages are grouped
together."
**Terminology:** "scheduled messages" are **staged interjections** — the queued rows shown in
the strip above the input box while they wait for delivery (ADR 0025). CONTEXT.md's
interjection entry explicitly rejects "scheduled message" as a term (nothing is clock-timed),
so code, comments, and docs written by this plan keep the staged/queued vocabulary; only this
Source quote uses the issue's wording. "When still scheduled" scopes the change to the STRIP:
a delivered ⧖ transcript block is untouched.
**Track:** post-`v0.9.3` working tree. One `### Changed` CHANGELOG entry under
`## [Unreleased]`; rides the next cut. `VERSION` untouched.
**Public API:** none. Everything lives in `internal/tui`; the `Engine` interface,
`apogee.Config` and all exported names are untouched.
**Standing requirement:** `/coding-standards` (Go + testing variants) mandatory — invoke
`implement-plan` with `coding-standards` forwarded. Pre-production: commit direct to `main`,
no PRs (owner directive).

Per-item green gate:

```
gofmt -l .                # empty
make check                # vet + lint + go test -race -count=1 ./...
```

**Dependencies.** Strictly linear: 1 → 2. Item 1 is the whole behaviour change; item 2 is the
documentation close-out. `/implement-plan` may stop after item 1 and the tree is coherent.

**Deviations leave a trail.** Any authorized deviation from an item's text must land as a
dated `NOTES:` line under that item's heading in this file, per the sub-agent templates.

**Authoritative sources**, in precedence order, for every item:

1. This document.
2. `ISSUES.md:8` — the four asks: 2-space indent, black background, one framing empty line
   above and below (black too), grouped together.
3. `internal/tui/model.go:2222-2248` (`statusLine`) — the house posture for a full-width
   black band: fill the whole width with black-bg cells so the row reads as one solid bar,
   never a bare gap showing the terminal's default background through the seam.
4. `internal/tui/theme.go` — the single home of look-and-feel; new styles are named theme
   fields, not inline `lipgloss.NewStyle()` calls at the render site.
5. ADR 0011 / `internal/tui/doc.go` — the value-copied `Model` holds no no-copy type by
   value. This plan adds NO Model state at all (one theme `lipgloss.Style` field, value-copy
   safe by design); `TestModelNoBuilderByValue` must pass untouched.

---

## Design decisions (2026-07-28, from the issue + the existing chrome posture)

- **The indent is `bodyIndent`, not a new constant.** `bodyIndent` (`theme.go:79`) is exactly
  two spaces and is already the column discipline of the whole bottom chrome: the status line
  indents by it so its text sits in the transcript's body column. Prefixing each strip row
  with `bodyIndent` gives the issue's "2 spaces to the right" and lines the ⧖ glyph up with
  the spinner/phrase column directly above the strip — one alignment rule, not two.
- **"Black background" means a full-width painted band, statusLine posture.** A background
  on just the text cells would render ragged right edges over the terminal's own background.
  Each strip row (including the two framing blank rows) is ANSI-truncated to the window
  width and then padded with styled spaces to that same width, exactly the reasoning recorded
  at `model.go:2237-2241`. The band sits directly above the input box's top border, whose
  interior is the same black (`colBlack`), so the chrome reads as one joined block.
- **One new theme field, `queuedText` — faint on black.** Same two tones as `statusBar`
  (`colFaint` on `colBlack`), but a dedicated role field per the theme's one-style-per-role
  discipline (`theme.go:104-118` comments): the strip is not the status bar, and reusing
  `statusBar` would couple the two roles against later divergence. The strip's current
  foreground (`noteText`, faint) is unchanged in tone — the fg stays `colFaint`; only the
  background is new.
- **The framing blank rows live INSIDE `renderPendingInterjections`' return.** View's shrink
  accounting is `lipgloss.Height(queued)` (`model.go:1868-1870`) and `layout()` never counts
  the strip at all — so emitting the two framing rows as part of the strip's own string makes
  every height consumer correct for free. Worst case grows from 4 rows (cap + marker) to 6;
  the `h < 1` floor at `model.go:1873-1875` already guards degenerate windows.
- **Grouping already holds.** The staged rows are one contiguous block in the one strip slot
  (`model.go:1904-1908`); nothing interleaves. The frame wraps the WHOLE group — the
  "… N more queued" overflow marker (`interject.go:348`) is inside the band, indented and
  painted like every other row.
- **Out of scope, deliberately:** the delivered ⧖ transcript blocks (`addInterjected` — the
  issue says "when still scheduled"); the status line's "N queued" segment
  (`interject.go:372-382`, already `statusBar`-styled); the chips and dropdown slots above
  the strip (their own looks, no issue asks for them); ISSUES.md items 3–5 (separate
  entries, separate plans).

---

## The ground (verified 2026-07-28 against the working tree)

**The renderer trio** (`internal/tui/interject.go:328-367`): `maxQueuedRows = 3` caps the
strip; `renderPendingInterjections` (`:339-354`) returns `""` when nothing is queued, else
the newest rows under an optional `… N more queued` marker, joined with `\n`; `queuedRow`
(`:358-360`) renders one line — today `m.th.noteText` (faint fg, NO background) over
`ansi.Truncate(text, max(1, m.width), "…")`, flush left; `queuedRowText` (`:365-367`)
flattens a row's raw text to a one-line preview (untouched by this plan). Imports already
include `github.com/charmbracelet/x/ansi` (for `ansi.Truncate`; `ansi.StringWidth` comes
from the same package for the pad computation).

**The View slot** (`internal/tui/model.go:1851-1877`, `:1904-1908`): the strip renders once
per frame, shrinks the transcript viewport by its `lipgloss.Height`, and is stacked closest
to the input box — after the dropdown and chips, before `m.inputView()`. The strip shows in
every state that has rows: a live queue while running, a held queue at idle.

**The theme** (`internal/tui/theme.go`): `colFaint = #8a8a8a`, `colBlack = #000000` (`:26-28`);
`glyphInterject = "⧖"` (`:66`); `bodyIndent = "  "` (`:79`); style fields declared in the
struct around `:104-118` with one-line role comments, constructed in `newTheme` around
`:150-175` (`statusBar: Foreground(colFaint).Background(colBlack)` at `:175` is the model
for `queuedText`).

**Existing tests** (`internal/tui/interject_test.go`): NO test pins the strip's visual today.
`:481` asserts the ⧖ marker on a DELIVERED transcript block (unaffected);
`TestStatusLineShowsQueuedCount` (`:503`) asserts the status-line count via
`plain(m.View())` `Contains` (unaffected by padding). Harness conventions: `newTestModel(t)`
at 80×24, `runningModel(t)`, `stageRow(t, m, text)` (`:302`), `step`, `plain` (ANSI-stripping
view helper, `model_test.go`).

**No overlap with in-flight work.** Plan 00 (workspace context files, mid-implementation)
touches `internal/agent`, `internal/domain`, `cmd/apogee`, ADR 0026 — disjoint. Plan 01
(transcript scroll-follow, READY, not started) touches `internal/tui/model.go`'s scroll
policy, one line in `interject.go` (`:266`, the `userScrolled` re-arm — far from this plan's
`:328-367` region), layout.md's first clause, and the same three doc files (layout.md,
CHANGELOG.md, ISSUES.md — different lines/sections). The two plans are order-independent;
whichever lands second reconciles stale line references mechanically.

---

## 1. The queued band — theme style, indent, framing rows — ✅ DONE (2026-07-28)

**What.** The strip becomes a full-width faint-on-black band, its rows indented into the body
column, framed by one blank band row above and one below.

- `internal/tui/theme.go`: add the style field `queuedText lipgloss.Style` with a role
  comment in the house voice (staged-interjection strip: faint on black), declared beside
  `noteText`/`statusBar` and constructed in `newTheme` as
  `lipgloss.NewStyle().Foreground(colFaint).Background(colBlack)`.
- `internal/tui/interject.go` — `queuedRow` (`:358-360`) becomes the band-row renderer:
  truncate `text` ANSI-aware to `w := max(1, m.width)` with the `…` tail (as today), pad
  with plain spaces to `w` (`ansi.StringWidth` for the measured width — the preview text is
  plain, but wide runes must count as their cell width), and render the whole padded line
  with `m.th.queuedText`, so every cell of the row — text, indent, and pad alike — carries
  the black background.
- `renderPendingInterjections` (`:339-354`): prefix each content row with
  `bodyIndent` — the shown rows become `bodyIndent + glyphInterject + " " + queuedRowText(...)`
  and the overflow marker `bodyIndent + "… N more queued"` — and wrap the group with one
  `m.queuedRow("")` framing row first and one last (a full-width blank black band row each).
  The `n == 0` early return stays: no queue, no band, no frame.
- Doc comments on the trio and on `maxQueuedRows` rewritten to describe the band (the strip
  is a framed, indented, black-backed group; the cap's worst case is now six rows including
  the frame). The View-side comments (`model.go:1846-1850`, `:1904-1905`) need no change —
  the slot and the accounting are untouched.
- No Model state is added; the new theme field is a value-copy-safe `lipgloss.Style`
  (ADR 0011 posture preserved; `TestModelNoBuilderByValue` untouched).

**Tests.** In `internal/tui/interject_test.go`, harness conventions above:

- *Structure, ANSI-stripped:* with two rows staged (`runningModel` + `stageRow` ×2), the
  strip's rendered lines (split `renderPendingInterjections()` on `\n`, assert through
  `ansi.Strip` or the `plain` posture) are: first and last line all-spaces at exactly window
  width; each content line begins `"  ⧖ "` (the `bodyIndent` indent) and is padded to exactly
  window width; row order preserved (oldest first).
- *Overflow marker inside the frame:* with `maxQueuedRows+1` rows staged, the marker line
  begins `"  … 1 more queued"`, sits directly after the opening frame row, and is padded to
  window width like every other row.
- *Styling is the theme's, not ad-hoc:* a sample content row's styled string equals
  `m.th.queuedText.Render(<the padded plain line>)` — pinning the black band to the theme
  field without asserting raw SGR bytes (profile-agnostic).
- *Height accounting:* with rows staged, `lipgloss.Height(m.renderPendingInterjections())`
  equals content rows + 2, and `plain(m.View())` still contains the status line and the
  input box (the shrink floor holds at 80×24).
- *Empty queue unchanged:* `renderPendingInterjections()` returns `""` with nothing staged —
  no frame rows leak into the View.

**Acceptance.** Green gate passes. With messages queued mid-run the strip renders as one
black band: blank band line, indented ⧖ rows (newest nearest the box, overflow marker on
top when capped), blank band line, directly on the input box's top border. Delivered ⧖
transcript blocks and the status line's "N queued" segment look exactly as before.

**Commit.** `feat(tui): staged-interjection strip renders as an indented full-width black band`

## 2. Documentation close-out — ✅ DONE (2026-07-28)

NOTES (2026-07-28): one authorized deviation from the item text. The item (and the plan's *Track*
line) says `VERSION` untouched; a first pass at this item nonetheless bumped `VERSION` from `v0.9.3`
to `v0.9.4`, and the owner ratified keeping the bump rather than reverting it. `VERSION` therefore
stays at `v0.9.4`, and — because `AGENTS.md` requires `CHANGELOG.md` and `VERSION` to stay in step —
this item's `### Changed` entry ships under a new `## [0.9.4] — 2026-07-28` release section instead
of under `## [Unreleased]`; the other `[Unreleased]` entries, which belong to other work, are left
where they are. No behaviour, code or test changes ride along.

**What.**

- `layout.md`: the queued strip is currently unspecified there. Add a short section (beside
  the status-line spinner section, after the bottom-chrome sketch) describing the strip: the
  staged rows sit directly above the input box as one group; each row is `bodyIndent`-indented
  (`  ⧖ message`), faint on a black background that spans the full window width; one blank
  black band row frames the group above and below; at most `maxQueuedRows` rows show, newest
  nearest the box, under a `… N more queued` marker; the band appears only while something is
  queued (running or held at idle).
- `CHANGELOG.md`: one `### Changed` entry under `## [Unreleased]` — queued (staged)
  interjections now render as an indented, full-width black band framed by blank band lines
  above and below. `VERSION` untouched.
- `ISSUES.md:8`: mark the entry `[X]` (executed), per the legend at `ISSUES.md:1-3`.

**Tests.** None new; `make check` green.

**Acceptance.** Green gate passes; layout.md describes the strip a reader could re-implement
from prose alone; an owner-run live smoke (queue two messages while a reply streams, then Esc
to hold them at idle) confirms the band in a real terminal — noted here as owner verification,
not a gate.

**Commit.** `docs: layout.md queued-strip spec, CHANGELOG and ISSUES for the interjection band`
