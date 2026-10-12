---
title: ADR 0058 decision
---

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
