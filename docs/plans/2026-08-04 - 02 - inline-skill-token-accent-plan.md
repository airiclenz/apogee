# Inline skill-token accent in sent blocks — implementation plan

- **Goal:** a sent prompt that invokes a skill no longer renders a separate `✦ Name` chip
  row under the block; instead the `/token` occurrences inside the block's own text are
  painted in the skill colour, exactly as the prompt box already paints them before send.
- **Date:** 2026-08-04 · **Status:** unexecuted
- **Authoritative sources** (pinned at commit `2841828`):
  - ISSUES.md, the entry "sent prompts with skills should not look like this" — the
    requirement. It asks for the skill "in-line with the text and simply color marked
    (same blue color as the skill tag)" with no additional `✦ Refocus` tag line. Note:
    the skill tag's actual colour is the violet `colSkill` `#8957e5` (theme.go:40) —
    "blue" in the entry is a loose reference to that same tag colour, so the accent uses
    `colSkill`, not `colFileRef`.
  - layout.md § "The prompt box's mini-language", para "Tokens light up when they
    resolve." (layout.md:942–947) — the visual precedent the sent block now follows.
  - docs/adr/0027-one-slash-namespace-with-inline-skill-tokens.md — the token stays in
    the sent text (an owner override of the strip default); its § on the surviving
    sent-block chip row (lines 62–64) is what this plan retires.
  - If an item disagrees with the ISSUES.md entry or with ADR 0027's surviving decisions,
    those sources win.
- **Design decisions recorded here:**
  - Accents come from **send-time spans persisted on the transcript entry**, not from a
    catalog lookup at paint time. The chip row was "the record of what the model was
    actually given"; the persisted spans inherit that role, so a replayed session keeps
    its accents even if the skill has since been deleted, and `renderUserBlock` stays a
    free function with no catalog access.
  - A skill invoked twice has two spans — both occurrences are painted. Spans, never the
    de-duped name list, drive the accent.
  - Spans are a display concern: keep them TUI-local (parse result → transcript entry →
    codec). Do not widen `domain.UserInput` unless the implementer finds the engine
    genuinely needs them — the wire-silent engine boundary (ADR 0031) prefers not.
- **Standing requirements:** `skills: coding-standards`; run `make check` before each
  commit; no version identifier changes (see closing note).
- **Out of scope:** the `/` menu's `✦` skill rows (autocomplete.go:358) and the
  `glyphSkill` constant (theme.go:68 — still live for that menu; do not delete); the
  prompt box's own accent machinery (inputaccent.go) beyond reading its idiom;
  sticky-header code (it re-uses already-painted lines, model.go:3035); every other
  ISSUES.md entry; version bumps.

## 1. Record skill-token spans on sent transcript entries

**What:** Capture the byte-offset spans of resolving `/tokens` at parse time and store
them on the transcript entry, alongside the existing display names — chips keep
rendering unchanged this item, so the build stays green mid-plan.

- `internal/tui/command.go` — `skillRefSpans(s, known)` (command.go:402) already
  produces `refSpan{start,end,name}`; `extractSkillRefs` (command.go:448) discards the
  offsets. Surface the spans on `parsedInput` (offsets into the exact text that will be
  stored on the entry).
- `internal/tui/transcript.go` — new entry field (e.g. `skillSpans`, a small
  start/end-offset slice type) next to `skills` (transcript.go:99); extend `addUser`
  (transcript.go:144) and `addInterjected` (transcript.go:160) to accept and store it.
- Call sites: `internal/tui/model.go:1123` (send) and `internal/tui/interject.go:248,
  298` (interjection twins) pass the parsed spans through.
- `internal/tui/transcriptcodec.go` — persist the spans on `wireEntry`
  (transcriptcodec.go:60; write ~:198, read ~:256). Entries persisted before this field
  load with nil spans and will simply paint plain — acceptable pre-production, no
  migration.

**Tests:**
- transcriptcodec_test.go: round-trip a user and an interjected entry carrying spans.
- transcript_test.go: `addUser`/`addInterjected` store spans; a prompt invoking the same
  skill twice stores two spans while `skills` still holds one de-duped name.

**Acceptance:** `go build ./...` && `go test ./internal/tui/` — all green, no rendering
change yet (existing chip-row tests still pass untouched).

**Commit:** `feat(tui): record skill token spans on sent transcript entries`

## 2. Paint the tokens inline and retire the chip row

Depends on item 1.

**What:** The visible change, confined to rendering and its tests.

- `internal/tui/theme.go` — new style for the in-block accent: `colSkill` foreground on
  the user block's `colDarkGray` field (declare near `skillToken` theme.go:143, build
  near theme.go:206). Do NOT reuse `th.skillToken` — its black background belongs to the
  prompt box's field.
- `internal/tui/render.go` — in `renderUserBlock` (render.go:463): after `wrapText`, map
  each entry span onto wrapped rows/cells and restyle those cells, reusing the idiom of
  `inputCellSpans`/`accentTokens` (inputaccent.go:98–120) and/or `shadeCells`
  (mouse.go:557). A token straddling a soft-wrap is accented on both rows. Collapsed
  blocks paint only their visible rows — spans on hidden rows just don't paint.
  Transcript drag-selection is shaded after and must keep winning over the accent.
- Delete the chip row: `renderUserChipRow` (render.go:539–551), `renderSkillChip`
  (render.go:556–558), and the chip-row call + lead/marker logic (render.go:492–497).
  Interjected `⧖` blocks flow through the same `renderUserBlock` (render.go:273) and get
  the accent identically.
- `glyphSkill` stays (out of scope above).

**Tests:**
- render_test.go: `TestCollapsedPromptKeepsItsChipRow` (:1122) becomes the assertion
  that a collapsed prompt is exactly `promptCollapsedRows` rows with no trailing chip
  row; `TestPromptBlockIsOneClickSurface` (:1378) loses its chip-row cases (":1393" and
  the no-body chip row ":1419"); the direct `renderUserBlock` call at :295 follows any
  signature change.
- skill_test.go: `TestSentUserBlockShowsSkillChipsWithText` (:464) and
  `TestDeliveredInterjectionShowsSkillChips` (:547) re-target: no `✦` in the block,
  token cells carry the new accent style; `TestSubmitBareSkillTokenSends` (:441) asserts
  the token text renders accented; `TestSubmitCarriesSkillIDs` (:413) is kept as-is —
  it pins that the token stays in the text.
- mouse_test.go: `modelWithHugePrompt` (:1179) / `TestTranscriptClickTogglesThePromptBlock`
  (:1207) — click-target rows shift now that the chip row is gone.
- New: a span straddling a soft-wrap paints on both rows; a twice-invoked skill paints
  at both occurrences; selection shading overrides the accent on overlapping cells.

**Acceptance:** `go test ./internal/tui/` green; `go build ./...`;
`grep -rn "renderUserChipRow\|renderSkillChip" internal/` returns nothing.

**Commit:** `feat(tui): paint skill tokens inline in sent blocks, retire the chip row`

## 3. Retire the display-name plumbing

Depends on item 2.

**What:** With the chip row gone, the entry's display names have no consumer.

- Remove `entry.skills` (transcript.go:99), the display-name parameters from `addUser`/
  `addInterjected` and their call sites, `skillDisplayNames` (model.go:1209–1226), and
  `wireEntry.Skills` (transcriptcodec.go:60, :198, :256).
- Decoding a legacy transcript that still carries a `skills` field must tolerate and
  ignore it (implementer verifies against the codec's actual decode behaviour). Legacy
  empty-text-with-chips entries render as a plain empty block — acceptable
  pre-production; leave a dated NOTES line under this item if any surprise surfaces.
- transcript_test.go: `entryDisplayStrings` (:423) stops walking `e.skills`;
  transcriptcodec_test.go drops the Skills round-trip and gains a legacy-field-ignored
  decode case.

**Tests:** as listed above.

**Acceptance:** `go build ./...` && `go test ./internal/tui/`;
`grep -rn "skillDisplayNames" internal/` returns nothing.

**Commit:** `refactor(tui): retire skill display-name plumbing from the transcript`

## 4. Amend the documents

Depends on item 3. This item owns every doc amendment — no other item touches docs.

**What:**
- layout.md § "Collapsed and expanded blocks" (:401–415, :419): remove the chip-row
  prose ("The chip row is never collapsed away…"); the toggle surface is the block's
  painted rows and marker row only; describe the inline accent where the section
  sketches the sent block.
- layout.md § "The prompt box's mini-language", para "What is not here any more."
  (:949–953): the transcript's sent block no longer carries `✦ name` chips either — the
  record of a send is the `/token` in the text, painted in the skill violet from spans
  captured at send time (a persisted verdict, not a live catalog lookup), mirroring
  "Tokens light up when they resolve." The `/` menu's `✦` rows (:808) stay as described.
- docs/adr/0027-one-slash-namespace-with-inline-skill-tokens.md: dated addendum noting
  the sent-block chip row (its lines 62–64) is retired in favour of the inline accent —
  do not rewrite the original decision text.
- CONTEXT.md "chip" vocabulary (~:643–644): chips are now fully retired from every
  surface.
- ISSUES.md: delete the fixed entry ("sent prompts with skills should not look like
  this…", the 4-line item).

**Tests:** none (docs).

**Acceptance:** `grep -n "chip" layout.md` shows no remaining sent-block chip claims;
`grep -c "✦ Refocus" ISSUES.md` outputs 0; `make check` green.

**Commit:** `docs: sent blocks carry inline skill accents, not chips`

---

**Suggested version bump** (not performed): minor — v0.11.0 — a user-visible
transcript-rendering change plus a wire-format addition; patch (v0.10.17) if the owner
treats it as polish. Owner's call, after execution.
