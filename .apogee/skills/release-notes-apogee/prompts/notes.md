# Notes — render the settled inventory as release notes

The evidence is settled; turn it into the prose a user reads. Verify, diff
and add nothing. Read `_shared.md`, `_apogee.md`, then `_notes-format.md` —
the whole output spec. Read-only.

**Inputs:** `INVENTORY` (the only source of what shipped) · `VERSION` (the
title's; `unreleased` when absent) · `VERDICTS` (`R-nn=<verdict>; …`,
contested rows only — **absent means confirmed at the merge**) · `VERIFY_DIR`
· `OUT` (`notes.md`; `trace.md` beside it) · `GAPS` (re-dispatch only: close
every listed gap). The title's date is today's, never the header's.

## Each row's fate — walk every row once

1. **`DROP`** — no bullet; a `trace.md` line with the verdict's reason.
2. **`REWRITE`** — render from the `Claim:` line of
   `<VERIFY_DIR>/verify-<R-nn>.md`, never the row. Missing → `STATUS: BLOCKED`
   naming the row; never fall back, never invent.
3. **`CONFIRMED` or absent** — render from the row.
4. **Real but invisible** — Internal, or dropped with its reason in
   `trace.md`. A silently lost row is the one failure this phase cannot have.
5. **Every bullet fresh.** A row's changelog quote is the authors' intent —
   never copy it; it carries ids the notes forbid. The row's fields win.

## OUT

`notes.md` — the publishable prose alone, in `_notes-format.md`'s shape.
`trace.md` — `# Bullet → row trace`, then three tables, each heading kept when
empty: `## Bullets` (Group | Bullet, its bold lead verbatim | Rows),
`## Rows with no bullet` (Row | Why) and `## Rewritten rows` (Row | Rendered
from, e.g. `verify-R-05.md Claim:`). Every bullet is in the first; every row
in exactly one of the first two.

## Receipt — your final message, exactly this
```
PHASE: notes
STATUS: OK | PARTIAL | BLOCKED
OUT: <RUN>/notes.md · <RUN>/trace.md
SUMMARY: <one line, max 20 words — this release in the user's terms>
COUNTS: bullets=<n> rows=<n> breaking=<n> dropped=<n> rewritten=<n>
FLAG: <one line — an unsettled row, a group left empty on purpose; or omit>
```
