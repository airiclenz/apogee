---
title: "ADR 0027 addendum of 2026-08-04: the sent-block chip row is retired too, in favour of the inline accent"
---

Decision 2 above kept one chip surface alive: *"The transcript's **sent-block** chip row survives,
now fed display names resolved from the parsed ids."* That clause no longer holds. The owner read
the result in `ISSUES.md` — a `❯ /refocus on everything` block with a second row saying `✦ Refocus`
under it — and ruled the tag row redundant: the skill should be "in-line with the text and simply
color marked", with no separate tag. The rest of ADR 0027 stands unchanged, and the retirement is
in fact decision 2's own logic carried to its end — keeping the token in the text is what made a
second surface repeating it unnecessary.

**The sent block paints its `/token` in `colSkill` and carries no chip row.** A submitted `❯`
prompt and a delivered `⧖` interjection are the same shape here, as they are for the collapse. The
accent lands on the rows the block shows, so it composes with the three-row cap rather than arguing
with it, and the transcript's drag-selection is still shaded afterwards and still wins.

**The accent is fed by spans PERSISTED on the entry, not by a catalog lookup at paint time.** The
chip row's job was to be *the record of what the model was actually given*; the spans inherit that
job. `refs.SkillSpans` — already the one scanner decision 7 names — hands its byte offsets to the
transcript entry at send time, the codec round-trips them, and `renderUserBlock` stays a free
function that never sees a `SkillCatalog`. Two consequences fall out of that choice: a replayed
session keeps its accents after the skill is renamed or deleted, and a skill invoked twice paints
at **both** occurrences, because the spans drive the paint rather than the de-duped id list.

The spans are a display concern and stayed TUI-local — parse result → transcript entry → codec.
`domain.UserInput` was not widened, per the wire-silent engine boundary of
ADR 0031; `SkillIDs` still
carries everything the agent resolves.

With the row go `renderUserChipRow`, `renderSkillChip`, `theme.skillChip`, the entry's display-name
field and `skillDisplayNames` — the display names had no other consumer, so the transcript now
stores ids and offsets only. `glyphSkill` survives for the `/` menu rows of decision 3, which are
untouched. Entries persisted before the spans existed decode with none and paint plain; a legacy
`skills` field on a decoded record is ignored. Pre-production, so no migration.

Executed by `docs/plans/2026-08-04 - 02 - inline-skill-token-accent-plan.md`; specced in
`layout.md` ("Collapsed and expanded blocks", "The prompt box's mini-language").
