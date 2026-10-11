# Verify — settle one contested inventory row

The merge tagged one row `CLAIMED`, `UNCLAIMED` or `CONFLICT`. Go to the diff
and return one verdict on it. Read `_shared.md`, then `_apogee.md`. Read-only.

**Inputs:** `ROW` (`R-nn — <title> (tag <TAG>)`, your whole scope — a
passing observation goes on one `Noticed:` line) · `INVENTORY` · `BASELINE`
(`baseline.tag`) · `CHANGELOG`, `COMMITS`, `SURFACE` (only the entries the
row's `Sources:` cites) · `OUT` (this row's file). Edit no input: your
receipt is how the verdict reaches the run.

**Settle it.** `git diff <baseline.tag> -- <the row's Files>` (range plus
uncommitted work) and `git show <sha>` per cited commit — the diff, not the
stat. No file named → one `git log <baseline.tag>..HEAD --oneline --grep` and
one tree grep from the title; two empty searches settle it. Then ask: **does
anything a user meets change?** (`_apogee.md`'s surfaces) — internal motion
alone is not a notes row.

**Exactly one verdict.** `CONFIRMED`: the diff shows the claimed change and a
user meets it. `DROP`: no user meets it — internal, reverted in range, or
claimed work that never landed; say which. `REWRITE`: real, but the row
misstates it (surface, direction, scale, a rename called a removal). Evidence
that cannot settle it → `STATUS: BLOCKED` naming what you could not read.

## OUT — markdown, under 40 lines

`Claim:` only on `REWRITE`, required there: the corrected claim as one
standalone sentence — the notes render from it, never from the row.

```
# <R-nn> — <verdict>
- Row: <title> · Tag: <merge tag>
- Evidence: <file> — <before> → <after>
- Reason: <1–2 lines>
Claim: <REWRITE only>
Checked: <diffs and commits read> · Noticed: <or omit>
```

## Receipt — your final message, exactly this
```
PHASE: verify
STATUS: OK | BLOCKED
OUT: <the path you wrote>
ROW: <R-nn>
SUMMARY: <one line, max 20 words — the evidence that settled it>
FLAG: VERDICT=CONFIRMED | VERDICT=DROP | VERDICT=REWRITE (BLOCKED: what blocked you)
```
