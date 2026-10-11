---
id: release-notes-apogee
displayName: Release Notes (apogee)
summary: Inventory everything apogee shipped since the last published release and draft plain-language release notes — read-only.
description: >
  Builds an evidence-backed inventory of every change since apogee's last
  published GitHub release — three blind scouts (changelog, commits, user
  surfaces), a merge, verification of only the contested rows — and renders
  plain-language release notes from it, trace-checked both ways. Read-only: it
  describes and never publishes. Optional argument vX.Y.Z names the release the
  notes are for. Use for "what shipped since the last release" or "draft the
  release notes". Cutting a release is release-apogee.
triggers:
  - release notes
  - what shipped since the last release
  - draft the release notes
  - what's new since the last release
---

# Release notes for apogee — what shipped since the last release

## Mission

Two tiers, in this order. First an **inventory**: every change since the last
published release as numbered rows, each carrying its evidence and a tag saying
how far three independent readings agree. Then the **notes**: plain-language
prose rendered from the settled inventory for someone who uses apogee and has
never read its source. Every bullet traces to a row, every row reaches a bullet
or a reasoned drop, and the last phase proves both directions.

Scope: commits since the baseline plus uncommitted work. The optional argument
`vX.Y.Z` (a bare `X.Y.Z` reads the same) is the release these notes are for.

## Boundaries

- **This skill describes; `release-apogee` publishes.** Versions, tags,
  releases and package channels are that skill's work.
- **Read-only, with one stated exception.** No git state-changing command —
  no `add`, `commit`, `tag`, `push`, `stash`, `checkout`, `switch`, `reset`,
  `clean` — by you or any sub-agent. The exception is Phase 1's
  `go run ./cmd/release status --json`: its gate check runs one `git fetch`,
  which moves `origin/main` and adds tags (no working-tree change). That call
  also needs an authenticated `gh` (`gh auth status`): the baseline is read
  from `gh release view`.
- You write nothing outside the run directory
  `docs/skill-runs/release-notes-apogee/<date>/` (gitignored) and the one
  output file in the OS temp dir.

## You are the COORDINATOR

You orchestrate; sub-agents read and write. **Never read code, diffs, plans,
changelog text, the inventory, the notes or any phase output** — receipts are
your only view of a phase; trust them, not content.

Dispatch with whatever agent-spawning tool the host provides (`sub_agent`,
`Agent`, `Task`, or similar). **Parallel within a phase, ordered across
phases:** dispatch a phase's sub-agents in ONE message, wait for every receipt,
then start the next phase. Sub-agents read their prompt in place at
`.apogee/skills/release-notes-apogee/prompts/<name>.md` — a repo path, so no
copying.

## Phase 0 — Run directory (one shell call)

Shell snippets assume fail-fast hosts (`set -e`, `pipefail`); a command that
may match nothing carries `|| true` — keep it. Substitute `VERSION_ARG` with
the argument (normalised to `vX.Y.Z`) or leave it empty.

```bash
T="$(git rev-parse --show-toplevel)"; TODAY="$(date +%F)"; V="VERSION_ARG"
K="$T/docs/skill-runs/release-notes-apogee"; R="$K/$TODAY"; mkdir -p "$K"
L="$(find "$K" -mindepth 1 -maxdepth 1 -type d -mtime -7 | sort | tail -n1 || true)"
if [ -n "$L" ] && [ ! -f "$L/trace-check.md" ]; then R="$L"; fi
for x in "$K"/*/; do [ "$x" = "$R/" ] || rm -rf "$x"; done
[ -f "$R/trace-check.md" ] && rm -rf "$R" || true
mkdir -p "$R/scratch"; find "$R" -maxdepth 1 -type f -empty -delete
TMP="${TMPDIR:-${TEMP:-/tmp}}"
echo "RUN=$R"; echo "REPO=$T"; echo "TMP=$TMP"; echo "VERSION=${V:-unreleased}"
echo "EXISTING:"; ls -1 "$R" || true
```

**Resume contract — file-level only.** `EXISTING` lists an interrupted run
younger than seven days; a completed run (it holds `trace-check.md`) and older
runs were removed. Skip any phase whose every `OUT` is listed and say so on
your first ledger line. Never open a listed file to judge it.

## Phase 1 — Baseline (one shell call)

The gate is `release-apogee`'s concern, not this skill's: keep the JSON
whatever `status` exits with.

```bash
R="RUN_VALUE"; cd "REPO_VALUE"
go run ./cmd/release status --json > "$R/baseline.md" 2> "$R/scratch/status.err" || true
TAG="$(sed -n 's/^ *"tag": *"\([^"]*\)".*/\1/p' "$R/baseline.md" | head -n1 || true)"
[ "$(tail -n1 "$R/baseline.md" 2>/dev/null || true)" = "}" ] || TAG=""
echo "TAG=${TAG:-none}"; grep -E '"(published|range|commits)"' "$R/baseline.md" || true
[ -n "$TAG" ] || tail -n 20 "$R/scratch/status.err" || true
```

`TAG=none` (the JSON is missing, does not parse, or names no published
release) → **stop**: relay the error lines and name the likely cause (`gh` not
authenticated, no network, no published release). Otherwise print one ledger
line: baseline tag, publish date, range, commit count. `OUTFILE` is
`<TMP>/release-notes-apogee-<VERSION>.md` with an argument, else
`<TMP>/release-notes-apogee-<TAG>..HEAD.md`.

## The phase table

Every dispatch carries `RUN_DIR: <RUN>`, `BASELINE: <RUN>/baseline.md` and
`APOGEE: .apogee/skills/release-notes-apogee/prompts/_apogee.md`, plus:

| # | Prompt | Extra keys | OUT |
|---|---|---|---|
| 2 | `changelog-scout` | — | `changelog.md` |
| 2 | `commits-scout` | — | `commits.md` |
| 2 | `surface-scout` | — | `surface.md` |
| 3 | `merge` | `VERSION:`, `CHANGELOG:`, `COMMITS:`, `SURFACE:` (the Phase 2 OUTs) | `inventory.md` |
| 4 | `verify` | `ROW: <id> — <title> (tag <TAG>)`, `INVENTORY:`, the Phase 2 OUTs | `verify-<id>.md` |
| 5 | `notes` | `VERSION:`, `INVENTORY:`, `VERDICTS:`, `VERIFY_DIR: <RUN>` | `notes.md` · `trace.md` |
| 6 | `trace-check` | `TRACE:`, `INVENTORY:`, `NOTES:`, `VERDICTS:` | `trace-check.md` |

Every OUT lives in `<RUN>`; each dispatch takes this shape:

```
Read .apogee/skills/release-notes-apogee/prompts/<name>.md and follow it exactly.
RUN_DIR: <RUN>
<KEY>: <value>
OUT: <RUN>/<out>
Return only the receipt block that prompt file specifies. Nothing else.
```

## Phases 2–6

- **2 — Scouts, one batch.** The three are blind to each other: the changelog
  scout takes the authors' claims and runs no git; the commits scout takes the
  history and never sees the changelog; the surface scout diffs the user
  surfaces `_apogee.md` names and is the run's only **evidence**. A `BLOCKED`
  scout is re-dispatched once with the retry note; still blocked → `SKIPPED`
  on the ledger. A skipped surface scout leaves every row `CLAIMED` — say so.
- **3 — Merge, alone.** It folds the scout outputs into `R-nn` rows tagged
  `CONFIRMED`, `CLAIMED`, `UNCLAIMED` or `CONFLICT`; its receipt carries the
  per-tag counts and a `CONTESTED:` line. `inventory.md` is never rewritten.
- **4 — Verify the contested rows only, one batch.** One dispatch per row on
  `CONTESTED:`; none when it is `none`. Fold the receipts into one ledger line,
  `VERDICTS: R-03=DROP; R-07=REWRITE; R-11=CONFIRMED` — built from receipts,
  never from a file. A `BLOCKED` verifier's row is omitted and named unsettled.
- **5 — Notes, alone.** `DROP` rows are excluded; a `REWRITE` row renders from
  its verify file's `Claim:` line.
- **6 — Trace check, alone.** `RESULT=PASS` → done. `RESULT=GAPS <n>` →
  re-dispatch `notes` once with the receipt's gap list as a `GAPS:` line, then
  `trace-check` again; still `GAPS` → report the gaps with the notes as they
  stand, never presented as checked.

**Retry note** — prepend on any re-dispatch: "A previous attempt may have
left a partial OUT. Discard anything incomplete and start clean."

## Output

1. Copy the notes out (one shell call): `cp "<RUN>/notes.md" "<OUTFILE>"`.
2. Show the notes in chat in full, then a short summary: baseline and span,
   per-tag row counts, what verify dropped or rewrote, the trace result, the
   output path and the inventory path (`<RUN>/inventory.md`, not pasted).
3. State the implied bump **as a fact**, never a proposal: apogee is 0.x, so
   a breaking change or a feature-heavy inventory implies a **minor** bump and
   anything else a **patch**. Choosing and applying a version is
   `release-apogee`'s work.

## Hard rules

- The repository is never modified; `status`'s fetch is the one remote touch.
- Evidence outranks claims: a `CLAIMED` row stays labelled so to the summary.
- Every row lands in a bullet or a reasoned drop.
- The prose is written fresh; changelog text is evidence, never copy source.
