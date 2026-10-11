# Surface scout — what apogee's own surfaces prove changed

The other scouts read **claims**: the changelog says what the authors think
shipped, the commits what a subject line says was done. You produce
**evidence**: you diff the user surfaces `_apogee.md` names, from the baseline
to the working tree, and record before→after values a reader can check. Never
read `CHANGELOG.md`, commit messages or plans — you compare two trees, you do
not read a history. Read `_shared.md`, then `_apogee.md`. Read-only.

**Inputs:** `BASELINE` (`baseline.tag`, `baseline.range`) · `APOGEE` (its
**User surfaces** table) · `OUT`.

## Never invent a surface

Diff the table's paths; never widen one or hunt for a surface it omits. Each
table row gets exactly one verdict:

- **found** — its paths exist and are unchanged in the range.
- **changed** — the diff shows a concrete difference; each becomes an `S-nn`
  entry.
- **absent** — a named path exists at neither end (`_apogee.md` drifted:
  name it on `FLAG`). A row you never reached is **uncovered** — `FLAG` only.

## 1. Structural rows — always, first

Every row but Emitted strings. Per row, `git diff <baseline.tag> -- <paths>`
covers the committed range and uncommitted edits in one diff; add new
untracked files under those paths from `RUN_DIR/scratch/surface-before.txt`.
Read the diff, not the stat — you need the value that moved: a command, flag,
key binding, config key or shipped default; a tool in `KnownToolNames` or the
default menu; an exported symbol; a manual page or documented behaviour; a
Makefile target, workflow or packaging template. A changed count the README
or manual states (tools, Floor guards, Moments) is its own entry.

## 2. Emitted strings — what apogee says to its user

Diff that row the same way, after each sentence a user or the model now reads
differently — usage line, notice, error, status word, system-prompt line,
announced path. Quote it on both sides of the arrow (a paraphrase is not
evidence of a message); skip `_test.go` files.

## The scale rule

`_shared.md` caps you at 80 tool calls; `internal/tui/` alone can exceed it.

1. **Structural rows first** — every one gets its verdict before you diff a
   single string. That half is never dropped.
2. **Strings within the remaining budget, newest first** — list shas with
   `git log --format=%h <baseline.range> -- <paths>` (no subjects) and walk
   back from HEAD, diffing `<sha>^ <sha>`.
3. **On exhaustion, stop and declare it:** write what you have, return
   `STATUS: PARTIAL` and `FLAG: LARGE <rows covered coarsely or not at all>`.

## Entries

One per concrete change, `_inventory-format.md`'s shape, prefix `S-`, in the
table's row order. `Commits:`, `Tests touched:` and `Refs:` stay `—` — an
attribution invented from two trees would survive the merge as evidence.
`Files:` is the file the change is in. Every entry carries **`Evidence:`**
`<file> — <before> → <after>` on one line (quote a string; give a flag, key
or symbol as written; an added or removed thing's missing side is `absent`)
and **`Verdict:`** `changed`, or `absent` when the surface itself went away.

## OUT

```
# Surface evidence since <baseline.tag>
## Coverage
| Surface | Paths diffed | Verdict | Entries |
|---|---|---|---|
<one line per User surfaces row, in the table's order>
## Evidence entries
<S-01, S-02, …>
## Not covered
<one line per row the budget missed, and what remains — or "none">
## Caveats
<unreadable diffs, unresolved paths, partial before→after pairs>
```

## Receipt — your final message, exactly this

```
PHASE: surface
STATUS: OK | PARTIAL | BLOCKED
OUT: <the path you wrote>
SUMMARY: <one line, max 20 words — what the surfaces prove changed>
COUNTS: entries=<n> changed=<n> found=<n> absent=<n> uncovered=<n>
FLAG: LARGE <coarse rows> when the budget ran out; otherwise one line of context, or omit
```
