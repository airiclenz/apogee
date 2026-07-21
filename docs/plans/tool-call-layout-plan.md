# Tool-call layout — tighter blocks, styled labels, grouped same-type calls — implementation plan

**Date:** 2026-07-21. Origin: owner request in-session (2026-07-21), quoted verbatim below — **that
request plus the Decisions section of this plan are the ground truth for every item.** `layout.md`
is the look spec of record and is amended FIRST (item 1) so no implementation item is written
against a sketch that contradicts it; after item 1 lands, `layout.md`-as-amended is the visual
authority where an item's prose is ambiguous. Executed by `/implement-plan` in a fresh session.

> 1. remove unneeded empty lines.
> 2. Remove the [Brackets] around tool names and just print them bold and in orange.
> 3. Group tool calls of the same type e.g. (Here "Read File" is bold and orange):
>    ```
>    Read File
>    ┝ README.md 1 - 154
>    ┝ TODO.md   1 - 408
>    ┕ ISSUES.md 1 - 8
>    ```

## The problem in one paragraph

Today a batch of file reads renders as five separate `✦ [Read File] name` blocks, each with its own
`┕ 1 - 154` branch and a blank separator line, and the assistant narration above them often trails
two or three extra blank lines (the model's trailing `\n\n` survives `commitAssistant` /
`finalizeNarration` verbatim and `renderMarkdownBody` renders each trailing empty line as a blank
row, stacking on top of the renderer's own one-line block separator). The result is a tall, noisy
transcript. The fix is three presentation changes, all inside `internal/tui`: (1) blank-line
hygiene at the commit/render boundary, (2) the tool label styled bold+orange with the `[brackets]`
dropped, (3) consecutive same-label tool calls folded into one block with one header and one
aligned branch line per call.

## Decisions this plan implements (all ratified by owner review of this plan — no open design calls)

1. **Presentation only.** The transcript model (`transcript.go` entries, call/result pairing,
   `hasOpenToolCall`) is untouched by the grouping change; grouping happens at render time in
   `renderView`, so a call that arrives mid-stream joins its group on the next repaint for free,
   and the append-only entry list stays the single source of truth.
2. **Uniform label styling.** Every tool header label — known friendly label, unknown raw tool
   name, and the orphan-result fallback — renders **bold + orange** (`#f0883e`, the palette tone
   `colCode` / `colModeAuto` already carry). The old signal "bare name ⇒ no registry entry" was the
   brackets' job and dies with them; the `bracket` field on `toolView` is **removed**, not renamed.
3. **Grouping key is the friendly `Label` at the same `depth`,** over *consecutive* transcript
   entries only (any intervening entry — narration, note, approval, error — breaks the run). Two
   different underlying tools sharing a label (e.g. `single_find_and_replace` and
   `multi_find_and_replace`, both "Edit File") group together: the user groups by what they read,
   not by tool id.
4. **A call is groupable** when its view has a non-empty `Target` and at most one detail line of
   kind `detailPlain` (this includes the `error: …` line and an in-flight call with no result yet).
   A call with multi-line details (a `Run` with `… +N more lines`), diff-kind details, or no target
   breaks the group and renders as its own block exactly as today. A "group" of one renders as
   today's single block (header carries the target).
5. **Group rendering:** one header line `✦ Label` (styled label, no target), then one `┝`/`┕`
   branch line per member: the member's `Target` **padded with spaces to the widest target in the
   group**, one space, then its detail text (nothing after the target for a still-running member).
   The last member gets `┕`. Overflow soft-wraps via the existing `hangingWrap`, same as any detail
   line today.
6. **Blank-line hygiene:** committed assistant text is stripped of leading and trailing blank
   (empty or whitespace-only) lines at commit time; the streaming preview is stripped of trailing
   blank lines at render time; interior runs of two-or-more blank lines are collapsed to one by the
   markdown renderer **outside fenced code blocks only** (fence interiors are verbatim). The
   one-blank-line block separator (`appendBlock`) is already correct and does not change —
   `layout.md`'s "always one empty line between" rule becomes true instead of aspirational.
7. **What the model sees never changes.** All three changes are chat-surface presentation; tool
   results, event payloads, and the upstream conversation are untouched.

## Verify gate (every item)

`make check` — gofmt-clean, `go vet`, `go build ./...`, `go test -race -count=1 ./...`. An item is
not done until it is green. Each item also carries its own targeted test, named in its Acceptance.

Every item that changes user-visible behaviour adds its own CHANGELOG entry under a `## [Unreleased]`
block (create it if absent; never edit a cut release section).

Any authorized deviation from an item's text lands as a dated `NOTES:` line under that item.

**Testing note that applies to items 2–4:** the label now carries baked-in ANSI (the markdown.go
posture — style before wrap; `ansi.Wrap` is SGR-aware, `lipgloss.Width` strips ANSI, so the wrap
and sticky-offset arithmetic are unperturbed). Substring assertions like
`strings.Contains(got, "✦ [Read File]")` (`transcript_test.go:90,119,370`) must therefore assert
against `ansi.Strip`-ed rendered lines (`charmbracelet/x/ansi`, already a dependency), not raw ones.

---

## 1. `layout.md` amendment — the look spec of record — ✅ DONE (2026-07-21)

NOTES (2026-07-21): three deviations from the literal text. (a) The grouped run keeps the
owner's members, padding and `┝ ┝ ┕` rails verbatim but is drawn with the renderer's real
`✦ ` header marker and two-space branch indent (decision 5 / `renderDetails`), matching the
rest of the sketch instead of the bare example. (b) The existing sub-agent sketch is otherwise
untouched but lost its `[brackets]` — the spec of record must not show a bracketed header
anywhere after decision 2. (c) The prose block sits in a short section under the sketch rather
than inline, so the mock stays one contiguous screen.

**This lands first; items 2–4 implement it.** Rework the tool-call region of `layout.md` (the
sketch at lines ~8–28) to show the target look:

- assistant narration with exactly one blank line to the next block (no trailing blanks);
- a single tool call: `✦ Read File main.go` header (no brackets; annotate that the label is bold
  orange `#f0883e`) with its `┕ 1 - 154` branch;
- a grouped run, verbatim from the owner's example (three Read File members, targets padded so the
  detail column aligns, `┝ ┝ ┕` rails);
- an ungrouped multi-detail call (a `Run` with its `… +N more lines` remainder) sitting beside the
  group, showing that rich calls stay standalone;
- keep the existing sub-agent (`⤷` / `│` rail) and user-block sketches untouched.

State decisions 3–5 (grouping key, groupability, padding) in one short prose block beside the
sketch so the spec explains itself without this plan.

**Acceptance:** gates green (docs-only diff); `layout.md` only. No code change in the diff.
Commit: `docs(layout): tool-call blocks — no brackets, bold-orange labels, grouped same-label calls`.

---

## 2. Blank-line hygiene — trim at commit, collapse in markdown — ✅ DONE (2026-07-21)

NOTES (2026-07-21): one deviation. `commitAssistant` trims the canonical text *before* deciding
whether to fall back to the streamed tokens (the literal text trims "the text" after the choice),
so a whitespace-only `MessageEvent` still falls back to the buffer instead of discarding it —
preserving the method's existing "nothing streamed is lost" invariant, which a post-choice trim
would have quietly weakened. Both-blank still commits nothing, as specified.

Implements decision 6. Three sites, all `internal/tui`:

- `transcript.go` — `commitAssistant` (~line 136) and `finalizeNarration` (~line 154): strip
  leading and trailing blank lines from the text before appending the entry (after the existing
  escape-strip). A text that strips to empty commits **nothing** — today an empty `MessageEvent`
  with an empty buffer commits a bare `✦` marker line; that lone marker is itself an unneeded
  line and is dropped. Preserve `finalizeNarration`'s existing skip-empty behaviour.
- `render.go` — the `t.streaming` branch of `renderView` (~line 78): trim trailing blank lines from
  `t.pending` for display only (the buffer itself is untouched — a mid-stream `\n\n` may be a
  paragraph break about to be continued, so only the *render* trims). The just-opened empty buffer
  must still render its lone marker line (the existing `wrapText("")` behaviour) so the user sees
  streaming has begun.
- `markdown.go` — `renderMarkdownBody`: collapse interior runs of ≥2 blank lines to a single blank
  line, **skipping fenced code blocks** (the fence walk at the top of the function already knows
  the boundaries; blank lines inside a fence are code and stay verbatim).

**Acceptance:** gates green. Table tests: trailing-`\n\n` narration renders with exactly one blank
separator before the next block (pin against `renderLines` output); leading blanks stripped;
interior triple-blank collapses to one; a fenced block containing blank lines round-trips verbatim;
an all-whitespace `MessageEvent` commits no entry; the streaming preview drops trailing blanks but
an empty in-progress buffer still shows its marker. Diff confined to `internal/tui` + CHANGELOG.
Commit: `feat(tui): blank-line hygiene — trim assistant text at commit, collapse doubles in markdown`.
**Depends on:** item 1.

---

## 3. Tool labels — brackets removed, bold + orange — ✅ DONE (2026-07-21)

NOTES (2026-07-21): four deviations, all mechanical. (a) The bracketless assertions reuse the
existing `renderPlain`/`plainRender` helpers, which strip ANSI with `ansiPattern` (a CSI regexp,
`model_test.go:74`) rather than `ansi.Strip` — the same stripped surface the testing note asks
for, without a second stripping idiom in one file. (b) The pinned-string line numbers in the item
text were stale: the bracketed forms lived at `transcript_test.go:90,119,278,488` (line 370 has
none), and 488 is the `✦ [result]` orphan header, now `✦ result`. (c) The bold+orange assertion
landed as its own `TestToolHeaderLabelStyled` in `render_test.go` (it exercises
`renderToolBlock`, not the transcript) — a loose contains against `th.toolLabel.Render(...)`,
guarded by a check that the role paints anything at all. (d) `theme.go`'s `toolHeader` comment
("the ✦ [Label] target header") was corrected alongside the new role; leaving a bracketed header
described one line above the style that removes it would have been stale on arrival.

Implements decision 2. All `internal/tui`:

- `theme.go`: add a `toolLabel lipgloss.Style` role — `Bold(true).Foreground(colCode)` — with a
  one-line comment noting it shares the orange tone with inline code and the auto-mode marker.
- `render.go`: delete `bracketLabel` (~line 226). `renderToolBlock` composes the header as the
  pre-styled label (`th.toolLabel.Render(tv.Label)`) plus the plain target, baked into one string
  *before* `hangingWrap` — the markdown.go posture (see Testing note above). `renderOrphanResult`'s
  `"[result]"` becomes the styled bare word `result`.
- `toolpresent.go`: remove the `bracket` field from `toolView` and the `bracket: true` assignment
  in `presentToolCall`; update the file-top comment (line ~17, the `✦ [Read File] main.go`
  example) and the field/fallback doc comments (lines ~51–52, ~220) — an unknown tool still falls
  back to its raw name and pretty-printed args, now styled like any label.
- Tests: drop the `wantBracket` column from `toolpresent_test.go` (~line 225); update
  `transcript_test.go`'s pinned strings (lines ~76–77, 90, 119, 370) to the bracketless form,
  asserting on `ansi.Strip`-ed lines; add one assertion that the rendered header line carries the
  bold+orange SGR sequence (the un-stripped line contains the style; keep it a loose contains, not
  a byte-exact golden, so a lipgloss renderer change doesn't false-fail).

**Acceptance:** gates green; `grep -rn '\[Read File\]\|bracketLabel\|bracket ' internal/tui
--include='*.go'` returns nothing (comments included). Diff confined to `internal/tui` + CHANGELOG.
Commit: `feat(tui): tool labels bold orange, brackets dropped`.
**Depends on:** item 1.

---

## 4. Grouped same-label tool calls

Implements decisions 1, 3–5. All `internal/tui`, and only `render.go` plus tests:

- In `renderView`'s entry loop (~line 66): before rendering an `entryToolCall`, extend a run
  forward while the next entry is also `entryToolCall` at the **same depth** with the **same
  `tool.Label`** and **both** ends of the extension are groupable (decision 4 — a helper
  `groupable(tv toolView) bool`: non-empty `Target`, `len(Details) <= 1`, every detail
  `detailPlain`). A run of ≥2 renders through one `appendBlock` via a new
  `renderToolGroup(th theme, views []toolView, width int) []string`; a run of 1 falls through to
  `renderToolBlock` unchanged. The `prevDepth` / sub-agent-label logic keeps operating on the
  underlying entries (a group is same-depth by construction, so the `⤷` open fires exactly as
  before).
- `renderToolGroup`: header `✦ ` + styled label (reuse item 3's composition — no target); then per
  member one branch line, `┝ ` (`┕ ` for the last), text = `Target` padded to the group's widest
  target width (`lipgloss.Width`-measured; targets are plain text) + one space + the member's
  single detail text, or the bare target when it has none. Branch lines style as `th.toolDetail`
  today's detail lines use; wrap via `hangingWrap` exactly like `renderDetails`.
- Tests (`render_test.go` / `transcript_test.go`, on `ansi.Strip`-ed `renderLines` output): three
  consecutive `read_file` calls with results render one `Read File` header, `┝ ┝ ┕` rails, and the
  detail column aligned (pin the padded widths against the owner's example shape); a multi-detail
  `terminal` call between two `read_file` calls breaks the group into block-single-block; an
  approval note between two reads breaks the group; differing depth breaks the group; two
  different-tool same-label calls ("Edit File") group; an in-flight member (no result yet) renders
  its bare padded target and the group re-renders whole once the result folds in; a group of one is
  byte-identical to today's single-block rendering.

**Acceptance:** gates green; the tests above; no diff outside `internal/tui/render.go` + test
files + CHANGELOG (explicitly: `transcript.go` and `toolpresent.go` unchanged in this item).
Commit: `feat(tui): group consecutive same-label tool calls into one aligned block`.
**Depends on:** items 1, 3.

---

## 5. Closeout — whole-transcript golden pass and doc sweep

- One integration-level test rendering a realistic mixed transcript through `renderLines` — user
  prompt, narration with trailing `\n\n`, a three-read group, a standalone `Run` with multi-line
  output, an approval note, a sub-agent (depth 1) read — asserting the full `ansi.Strip`-ed line
  sequence including every blank line position. This is the backstop that fails if any single
  item's rendering regresses, and the living documentation of the layout.
- Doc sweep: `grep -rn '\[Read File\]\|\[Run\]\|\[result\]\|bracket'` over `README.md`,
  `CONTEXT.md`, `docs/`, `internal/tui/doc.go` — fix any remaining description of the bracketed
  look (godoc link syntax like `[Run]` in `doc.go:10,16` refers to the `Run` function and is NOT a
  hit; leave it). Verify the `[Unreleased]` CHANGELOG block reads as one coherent presentation
  change, consolidating the per-item lines if needed.

**Acceptance:** gates green; the golden test; the sweep grep clean of look-description hits;
`git status` clean after commit. Diff: test files + docs + CHANGELOG only.
Commit: `test(tui): whole-transcript layout golden; docs: close out the tool-call layout work`.
**Depends on:** items 2–4.

---

## Explicitly NOT in this plan

- **The status/activity line.** `toolActivityLabel` and the verb registry are untouched; the live
  "reading main.go" phrase already has no brackets.
- **Any transcript-model change for grouping.** Grouping is render-time only (decision 1); the
  entry list, call/result pairing, and `hasOpenToolCall` are not restructured.
- **Sub-agent rendering, notes, approvals, the user block.** Their look is unchanged; they interact
  with this plan only as group-breakers.
- **Cross-call detail merging.** A grouped member shows its own one-line summary; no aggregation
  ("3 files, 570 lines") is computed.
- **Re-styling detail lines or the diff colours.** `th.toolDetail` / diff red-green stay as they
  are; only the header label gains colour.
- **Truncation/alignment caps beyond padding.** Targets are padded to the group's widest member;
  no new clipping constant is introduced — overflow soft-wraps exactly as today.
