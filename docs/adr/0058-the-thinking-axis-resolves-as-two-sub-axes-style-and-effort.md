---
Status: accepted
---

# The thinking axis resolves as two sub-axes: channel style and effort

## Context

[ADR 0057](0057-the-tool-roster-is-a-third-model-profile-axis-resolved-axis-wise.md) decision 5
made Model-profile resolution axis-wise: each of the three axes — tool-call format, thinking
channel, tool roster — takes the nearest layer whose matching entry spells it, so a tools-only user
entry no longer wipes the wire shape the shipped table carries. Inside the thinking axis the same
trap survived one level down. `Entry.spellsThinking` was true for any non-zero `ThinkingProfile`,
and `Resolve` then took that entry's WHOLE thinking half — `Style`, `Start`, `End` and `Effort`
together. A user entry spelling only `effort:` over the shipped gpt-oss profile therefore won the
axis outright and dropped harmony parsing with it: `Style` resolved to `""`, `Source` was the user
tier, and nothing was announced.

That entry is not an edge case — it is a taught idiom. `README.md` sells "reasoning effort set per
model" as a headline feature, and
[ADR 0050](0050-thinking-effort-is-a-profile-axis-with-one-canonical-wire-mapping.md) put the dial
on the thinking axis precisely so it rides the profile's resolution. The most natural
edit a gpt-oss user makes to their config silently broke the parsing the shipped table exists to
provide. Recorded as an open defect at the close of the per-profile tool rosters plan
("literally ADR 0057 §5 compliant, so a design call to settle"), settled by the repo owner on
2026-08-24 in the refocus open-items plan.

## Decision

**1 — The thinking axis resolves as two sub-axes, each independently through user ▸ shipped ▸
zero.** The channel-style sub-axis and the effort sub-axis are asked the spells-it question
separately, and the resolved `ThinkingProfile` is composed from whichever layer answered each.
An `effort:`-only user entry over the shipped gpt-oss row now resolves to harmony-from-shipped
plus effort-from-user; a style-only entry over a layer carrying an effort keeps that effort.

**2 — The channel-style sub-axis is `{Style, Start, End}`.** `Start`/`End` are the delimiter
tokens `ThinkingDelimited` reads and are meaningless without a `Style`, so they travel with it
and never resolve on their own: an entry spelling `Style` brings its own tokens and replaces all
three; an entry spelling tokens without a style spells nothing on this half, and the orphaned
tokens are dropped rather than grafted under a deeper layer's style.

**3 — The effort sub-axis is `Effort` alone.**

**4 — Both halves are self-describing from the domain value; neither needs a presence flag.**
The style half answers exactly as the tool-call axis does: `""` is the unwritten style and
`none` is the spelled zero, so `thinking: {style: none}` still overrides a shipped style — and now
only that half, leaving a shipped effort where it was. The effort half answers differently but
just as fully: `""` is the ABSENCE of the dial and the wire anchor (ADR 0050 — absent means send
nothing), and any of the four words is a spelled value. There is no spelled zero to distinguish
from the unwritten one, so no config-layer `spells…` field is needed — unlike the roster axis,
whose `SpellsTools` exists precisely because an absent `tools:` and an empty one project to the
same zero value.

**5 — Source bookkeeping is unchanged.** A shipped tier that supplies either sub-axis still
earns `SourceShipped` and its one-line notice, which is the correct outcome: the human dialled
the effort and is told the shape came from the table.

## Bounds (stated, not separately ratified)

- This is resolution only. `ThinkingProfile` stays one struct on `Config`; the engine still
  receives one resolved profile and sees no layering, exactly as ADR 0057 promised.
- The `/effort` session override (ADR 0050 decision 5) is untouched — it layers above the
  resolved profile's effort, whichever tier supplied it.
- The wire mapping (ADR 0050 decision 2) is untouched — which tier supplied the effort word has
  no bearing on how the provider Client emits it.
- ADR 0057 decision 5's "three axes" still reads as three axes; one of them resolves in two
  halves. No fourth axis is introduced, and the profile's glossary term does not widen.

## Considered and rejected

Both weighed and declined by the repo owner on 2026-08-24:

- **Keep the axis atomic and announce the drop** — a notice on every `effort:`-only entry
  saying the shipped style was wiped. Turns a silent trap into a narrated one, but the user's
  only remedy is to copy the shipped `style:` into their own entry, which is exactly the 'inherit'
  spelling ADR 0057 closed as obsolete for the other axes.
- **Document the idiom without fixing it** — teach `effort:` entries to always carry `style:`.
  Leaves the most common config edit one omission away from broken parsing, and contradicts
  ADR 0050's reason for putting effort on the profile at all.

## Consequences

- `Entry.spellsThinking` is replaced by `spellsThinkingStyle` and `spellsThinkingEffort`;
  `Resolve` composes `Thinking` from two supplier calls.
- ADR 0057 decision 5 carries a dated pointer here; its "three axes" reads as three axes, one of
  which resolves in two halves.
- The package doc of `internal/profiles` and `CONTEXT.md`'s Model-profile and Thinking-effort
  entries say the thinking axis defers in two halves; the profile doc surfaces in the config
  layer (`internal/config/config.go`, the seeded `config.yaml`) follow suit.
