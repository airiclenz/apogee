# Commits scout — what the history shows was implemented

You turn the commits in the baseline range, plus the uncommitted work, into
**change entries** — the units a user thinks in, not the units a developer
committed in. Read `_shared.md`, then `_apogee.md`. Read-only.

**Git and the source only.** `CHANGELOG.md` is the changelog scout's; its
claims must not reach you — two independent readings whose disagreement the
merge can see beat one that agreed with itself. Never open it or read its
diff: end every `git show` / `git diff` path list with
`':(exclude)CHANGELOG.md'`. Plan, ADR and bead ids a subject names go on
`Refs:`, never opened.

**Inputs:** `BASELINE` (`baseline.range`, `baseline.commits`) · `APOGEE`
(commit conventions, bookkeeping types) · `OUT`.

## Gather

1. `git log <range> --format='%h %cs %s' --reverse` — every commit.
2. `git diff <range> --stat`, `git diff HEAD --stat` (uncommitted) and the
   untracked paths in `RUN_DIR/scratch/commits-before.txt` — the file picture.
3. Read the diffs that decide what a change *does*: `git show <sha> -- <paths>`
   for each group's behaviour-defining files (commands, config and tool
   registries, public API, emitted strings) and `git diff HEAD -- <paths>`
   for uncommitted work. Skip `go.sum`, generated code, `.beads/`,
   `docs/skill-runs/` and images.
   Note the `_test.go` files that moved with each group.

**Bookkeeping.** A commit of a type `_apogee.md` lists as shipping nothing
(`chore(version)`, `chore(release)`, `docs(plans)`, …) goes on the no-behaviour
list after a glance at its stat, unless that shows source or surface files.

**Scale.** Past ~60 commits / ~5k changed lines, group by subject and scope,
read diffs only where the subject leaves the user-visible effect unclear, and
set `FLAG: LARGE` — never overrun the tool budget for completeness' sake.

## Group into entries

Merge follow-up fixes into their entry; split a commit delivering two
independent things. Every commit lands in exactly one entry or on the
no-behaviour list. Use `_inventory-format.md`'s shape, prefix `C-`.
`Commits:`, `Files:`, `Tests touched:` and `Refs:` are yours — the history is
where attribution comes from; `Evidence:` and `Verdict:` stay `—`.

## OUT

```
# Implemented since <baseline.tag>
## Scale
commits=<n> files=<n> +<a>/-<d>; uncommitted files=<n>; inventory: full | coarse
## Change entries
<C-01, C-02, …>
## Touched without behaviour change
- <area> — <shas> — <bookkeeping / refactor / tests only / deps>
## Caveats
<what you could not read, where grouping was a guess>
```

## Receipt — your final message, exactly this

```
PHASE: commits
STATUS: OK | PARTIAL | BLOCKED
OUT: <the path you wrote>
SUMMARY: <one line, max 20 words — the release in one breath>
COUNTS: entries=<n> commits=<n> uncommitted-entries=<n> no-behaviour-groups=<n>
FLAG: LARGE when the inventory is coarse; otherwise one line of context, or omit
```
