# Merge — fold three readings into one inventory

The scouts read blind to each other: `CL-nn` are the authors' claims, `C-nn`
what the history shows, `S-nn` the only **evidence** (`Evidence:`,
`Verdict:`). Fold them into `R-nn` rows tagged by how far they agree. Read
`_shared.md`, `_apogee.md`, then `_inventory-format.md` — its entry shape is
yours. Read-only.

**Inputs:** `BASELINE` (`baseline.tag`, `baseline.published`) · `VERSION`
(`unreleased` when absent) · `CHANGELOG` · `COMMITS` · `SURFACE` · `OUT`. A
missing or short scout file is noted under `## Readings`, never agreement.

**You tag, never adjudicate.** Truth is the verify phase's: open no diff,
edit no scout output, drop nothing for doubt, invent no field (`—`). **Every
input id lands exactly once** — in one row's `Sources:` or under
`## Not a release row` with its reason.

**Matching**, strongest first: shared files or commits · the same flag, key,
command, setting or message seen from two sides · the same bead, ADR or plan
item on `Refs:`. Title similarity never suffices; name a judgement call in
`Notes:`. One verifiable thing is one row — split, fold. Number `R-01`, …
breaking and user-visible first, internal last; never renumber.

**Tags — exactly one per row.** `CONFIRMED`: a `CL` or `C` claim meets an
`S-nn` whose `Verdict:` is `changed` — settled, never verified. `CLAIMED`:
claims, no evidence. `UNCLAIMED`: a `changed` `S-nn` no claim explains.
`CONFLICT`: the readings disagree (direction, surface, rename vs removal,
evidence against a claim) — `Effect:` states the disagreement and the row
keeps both readings. The last three are contested. An `S-nn` that is `found`
or `absent`, and a commit listed as touched without behaviour change, are
not rows.

## OUT — markdown, under 300 lines

Rows are `_inventory-format.md` entries, prefix `R-`, `Evidence:`/`Verdict:`
from the surface entry, plus `Tag:`, `Sources:`, an optional `Notes:`, and
every feeding `CL` entry's quote carried through byte for byte.

```
# Release inventory — apogee — <VERSION> — <baseline.tag> (<published date>) → HEAD
## Readings
| Reading | Status | Entries | Notes |
## Rows
### R-01 — <title>
<entry fields>
- Tag: CONFIRMED | CLAIMED | UNCLAIMED | CONFLICT · Sources: CL-03, C-07, S-02
> <authored changelog text, verbatim>
## Not a release row
- <input id> — <coverage | no behaviour change | duplicate of R-nn>
## Caveats
```

## Receipt — exactly this; `CONTESTED` ascending, or `none`
```
PHASE: merge
STATUS: OK | PARTIAL | BLOCKED
OUT: <the path you wrote>
COUNTS: rows=<n> confirmed=<n> claimed=<n> unclaimed=<n> conflict=<n>
CONTESTED: <R-nn>=<TAG> <title>; … | none
FLAG: <one line — a missing reading, an unplaceable entry; or omit>
```
