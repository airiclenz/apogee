# Markdown underline support — implementation plan

- **Goal:** assistant text containing `<u>text</u>` renders as underlined text in the TUI; everything else is unchanged.
- **Date:** 2026-08-10
- **Status:** unexecuted
- **Sized for:** ~200k-context host
- **Authoritative sources:**
  - `internal/tui/markdown.go` file header (lines ~11–19) — the supported-subset statement and the "spare, pure, lipgloss-only, no external markdown library" posture (reaffirmed in `CHANGELOG.md` and `docs/plans/archived/2026-07-31 - 00 - markdown-table-rendering-plan.md`).
  - `internal/tui/doc.go` (~158–161) — package-level narration of the renderer.
- **Ratified design calls:**
  - Underline trigger is the exact lowercase HTML pair `<u>…</u>` — decided by the owner via AskUserQuestion, 2026-08-10. `__text__` stays literal (CommonMark reserves `__` for strong emphasis); no other HTML is recognised.
- **Skills:** coding-standards
- **Out of scope:**
  - Italic (`*x*` / `_x_`), strikethrough (`~~x~~`), links (`[x](url)`).
  - Mapping `__text__` to bold, or any change to existing bold/code behaviour.
  - Any other HTML tag, attributes on `<u>`, uppercase `<U>`, or general HTML passthrough/stripping.
  - Inline parsing in headings (`renderHeadingLine` deliberately does not re-parse inline markup — unchanged).
  - Adding any markdown library dependency.

## 1. Render `<u>…</u>` as underline in the inline scanner — ✅ DONE (2026-08-10)

NOTES (2026-08-10): the styling assertions probe the underline SGR from `th.mdUnderline` (new
`underlineSGR(th)` test helper) instead of the literal `\x1b[4m` the plan names — lipgloss v2 emits
`\x1b[4;4m` per rune for `Underline(true)`, so the bare form never appears on the wire.

**What:**

- `internal/tui/theme.go`: add field `mdUnderline lipgloss.Style` to the theme struct (with the other `md*` fields, ~165–169) and initialise it in `newTheme` (~313–317) as `lipgloss.NewStyle().Underline(true)` — SGR 4, no colour.
- `internal/tui/markdown.go`: in `renderInline` (~165–191), add one case mirroring the existing `**` branch (~177–184): on the exact prefix `<u>`, scan for the exact closing `</u>`; when found, render the enclosed text with `th.mdUnderline` and skip past the tags. Binding behaviour:
  - Exact lowercase byte match only — `<U>`, `<u >`, `<u attr>` and every other tag are NOT special and fall through to the literal `default:` branch untouched.
  - No closing `</u>` before end of input → emit `<u>` literally and continue (same mid-stream safety as unterminated `**` / `` ` ``).
  - Precedence: code spans still win — a `<u>` inside backticks is literal; the enclosed text of `<u>…</u>` is NOT re-scanned for nested markup, exactly like the existing `**` branch.
  - Empty span `<u></u>` produces no visible output (mirror whatever the empty `****` path does — do not special-case).
- `internal/tui/markdown.go`: extend the file-header supported-subset comment (~11–19) to name `<u>…</u>` underline.
- `internal/tui/doc.go`: extend the renderer narration (~158–161) the same way.
- `CHANGELOG.md`: add an entry for the feature under an `## Unreleased` heading, creating that heading at the top if absent. Do NOT touch `VERSION`, any existing release heading, or any tag (version policy: bumps are the owner's act).
- Table cells inherit the behaviour for free via `renderInline` (`internal/tui/mdtable.go:266/272`) — no table code change; covered by a test below.

**Tests** (in `internal/tui/markdown_test.go`, following the house pattern — `strip()`/`ansi.Strip` for visible text, `colorActive(th)` guard before asserting styling; plus one cell test in `mdtable_test.go`):

- `TestRenderInlineUnderline`, shaped like `TestRenderInlineBold` (:30): `"press <u>Enter</u> now"` → stripped text `"press Enter now"`; under `colorActive`, output contains the underline SGR (`\x1b[4m`).
- Extend the `TestRenderInlineUnterminated` input list (:65) with `"a <u>open"` — literal text survives, no `\x1b` emitted.
- Literal passthrough cases: `"__text__"`, `"<U>x</U>"`, `"<u >x</u>"` render byte-for-byte unstyled.
- Code-wins case alongside `TestRenderInlineCodeBeatsBold` (:56): `` "`<u>x</u>`" `` — the tag text stays visible inside the code span, no underline SGR.
- `mdtable_test.go`: one case with a cell containing `<u>x</u>` — stripped cell text is `x` (and under `colorActive`, underline SGR present).
- Existing invariants must still pass untouched: `TestPlainTextUnchanged` (:252), `TestWidthNeverExceeds` (:272), `TestTableWidthNeverExceedsAcrossWidths` (:296).

**Acceptance** (commands a verifier runs):

- `go test ./internal/tui/` — full package green.
- `go test ./internal/tui/ -run 'TestRenderInline' -v` — the new underline cases listed and passing.
- `make check` — clean.
- `grep -n '<u>' internal/tui/markdown.go internal/tui/doc.go` — both doc comments name the new syntax.

**Commit:** `feat(tui): assistant text renders <u>…</u> as underline`

---

**Suggested version bump:** minor (v0.13.0) — a new user-visible rendering capability, not a fix. Owner decides; no version identifier is changed by this plan.
