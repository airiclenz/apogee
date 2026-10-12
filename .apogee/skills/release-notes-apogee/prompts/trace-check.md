# Trace check — every bullet, every row, both directions

Prove the prose and the inventory account for each other. You check a
correspondence, not the release. Read `_shared.md`. Read-only: no git beyond
its status checks, no diff, no source. A gap is reported, never repaired.

**Inputs:** `TRACE` · `INVENTORY` (its `## Rows` ids are the population) ·
`NOTES` · `VERDICTS` (contested rows only; absent = confirmed) · `OUT`.

**Bullet → row.** Each `## Bullets` line names at least one row, every one in
the inventory, not all `DROP`, and its bold lead appears verbatim in
`notes.md`; every bullet in `notes.md` has a trace line. Each miss is a gap.

**Row → bullet.** Each row sits in exactly one of a bullet's `Rows` cell or
`## Rows with no bullet` with a reason; neither or both is a gap. A `DROP`
row belongs in the no-bullet table; a `REWRITE` row must also point at
`verify-<R-nn>.md` under `## Rewritten rows`.

**The leak grep — last, on `notes.md` only.** Leaks are gaps, quoted with
their line: run ids (`R-`, `CL-`, `C-`, `S-` + digits), commit hashes, `#<n>`
issue numbers, issue ids (`apogee-qi3`, `apogee-<slug>`, or a capcom slug like
`adoption-pane-unarmed`, which the grep cannot see — read for it), and talk of
how the notes were made. Judge each hit — a version, a deliberate `apogee-…` name or
"run apogee" is no leak.

```bash
grep -nE '\b(R|CL|C|S)-[0-9]{2,}\b|\b[0-9a-f]{7,40}\b|#[0-9]+|apogee-[a-z0-9]|\b(run|phase|agent|scout|inventory|verdict)s?\b' "<NOTES>" || true
```

## OUT — markdown, under 60 lines

`# Trace check — <PASS | GAPS>`, then `## Counts` (`bullets= rows= traced=
dropped= rewritten= gaps=`), `## Gaps` (a `# | Direction | What | Detail`
table, e.g. `row→bullet | R-07 | in neither table`) and `## Checked` (files
read, ids covered). On `PASS`, `## Gaps` holds one `none` line: an empty
check and an unrun one must not look alike.

## Receipt — your final message, exactly this
```
PHASE: trace-check
STATUS: OK | PARTIAL | BLOCKED
OUT: <the path you wrote>
SUMMARY: <one line, max 20 words — what the check found>
COUNTS: bullets=<n> rows=<n> traced=<n> dropped=<n> gaps=<n>
FLAG: RESULT=PASS | RESULT=GAPS <n> <the gap list, ≤ 30 words>
```
