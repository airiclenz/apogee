# Tag a commit on VERSION change — a CI workflow

**Goal:** add a GitHub Actions workflow that creates an annotated git tag at the exact
commit where the number in the top-level `VERSION` file changes, so every version-number
decision is pinned by a tag the repo's tooling can reference — and so a release cut has a
tag waiting for it instead of making one by hand.

**Date:** 2026-08-25

**Status:** revised after review (2026-08-25) — two blocking defects and seven smaller
ones fixed; the two open design forks were put to the owner and are ratified below.
Second review pass (2026-08-25): the detector now tags **every** bump in a pushed range,
not just the last, and the transitional gap for the current `VERSION` value is recorded.

**sized for:** ~200k-context host

**Authoritative sources:**
- `VERSION` (top-level) — the single source of truth for the release version; value
  carries the leading `v` and one trailing newline (`v0.16.8`). `Makefile:33`
  (`DIST_VERSION`) strips the `v` for archive names, so the tag and the archive name
  differ only in that prefix.
- `.github/workflows/ci.yml` — the existing workflow to mirror in style and YAML
  conventions (two-space indent, `on:`/`permissions:`/`jobs:` blocks, `actions/checkout@v4`,
  a top comment block, inline `run:` bash).
- The release procedure as practised today: a `chore(release): cut vX.Y.Z` CHANGELOG
  rollup, then `git tag` + `git push origin <tag>` by hand, then `make dist` and
  `gh release create` (brew-release skill, `SKILL.md:36-40` — **outside this repo**).
- Live tag facts this plan is built on (verified 2026-08-25): 9 tags exist for 122
  first-parent VERSION changes; 6 of the 9 are **lightweight** (`git cat-file -t v0.16.3`
  → `commit`), only `v0.16.0`, `v0.10.4` and `v0.8.0` are annotated. Tags today sit on
  the *release-cut* commit, not the VERSION-bump commit — `v0.16.3` was bumped at
  `7ad8a682` (08-23) but tagged at `d7ee7b36` (08-24).

## Ratified design calls (owner, 2026-08-25)

1. **Trigger is `push` to `main` only** — not `pull_request`. PR runs would tag refs on
   forks or unmerged heads; tags exist to pin commits that are actually on `main`.
2. **The tag is annotated** (`git.createTag` + `git.createRef`, message = the version).
   This is a deliberate *upgrade* over the current practice, not a continuation of it:
   the hand-cut tags are mostly lightweight (see Authoritative sources). Annotated tags
   carry a tagger and date, which is the whole point of pinning a decision.
3. **The tag name is the full `VERSION` value verbatim** (`v0.16.8`), unchanged, because
   the Makefile strips the `v` itself and the archive name derives from that already.
4. **Idempotent**: if the tag already exists (re-run, retry, push of a pre-tagged commit),
   the workflow logs a skip and leaves the existing tag untouched.
5. **The workflow creates a git tag only — never a GitHub Release, never binaries, never
   a Homebrew bump.** Publishing a Release stays a separate, deliberate, manual act on
   top of this tag.
6. **The CI tag *is* the release tag.** There is no second tag namespace. The release
   procedure stops creating tags: at a cut, the CHANGELOG rollup is committed first and
   the **VERSION bump is the final, code-free commit of the cut**, so CI tags a tree that
   already contains the rollup. `gh release create` then runs against the tag that is
   already on the remote. This preserves the owner's standing habit — the VERSION bump is
   always its own commit, made after the changes it describes — and retires the
   `v0.16.3`-shaped cut where VERSION already held the target value and no bump happened.
7. **The tag lands on the commit that changed `VERSION`**, not on the push head. The two
   differ on every multi-commit push, and the Goal sentence names the former. A push that
   carries **more than one** bump (two standalone bump commits in one burst) tags each of
   them at its own commit — "every version-number decision" means every one, and a
   last-only detector would silently drop the earlier tag forever.

### Consequences accepted with these calls

- **Every micro-bump becomes a permanent Go module version.** `proxy.golang.org` caches
  tags immutably (`AGENTS.md:23` — the repo has been bitten by this once already), so
  auto-tagging publishes roughly one module version per shipped feature, irreversibly.
  Nothing breaks (`@latest` still resolves the stale `v1.7.0`, which outranks every
  `v0.x`), but it is a one-way door and it is being opened knowingly.
- **The version in `VERSION` when this lands (`v0.16.8`) never gets a CI tag.** The
  workflow fires on a push that *changes* `VERSION`; no future push changes it *to*
  `v0.16.8`. The first CI-created tag is the next bump. So the next release cut must bump
  to a new number (per call 6) — nobody should cut "v0.16.8" and wait for a tag, and if a
  `v0.16.8` tag is wanted it is made by hand, once, as the last hand-made tag.
- **The tagger is `github-actions[bot]`**, not the owner. The annotated tag pins the date
  and the commit; authorship of the decision stays in the commit itself.
- **Tags created with `GITHUB_TOKEN` do not trigger other workflows.** That is what keeps
  this from looping with `ci.yml`; it also means a future `on: push: tags:` release
  workflow would *not* fire from these tags. Recorded in the workflow's comment block.

## Standing requirements

- skills: `coding-standards`
- Follow the existing `ci.yml` conventions: `on:` block style, `permissions:`, named
  `jobs:`, `actions/checkout@v4`, a leading comment block, inline `run:` bash.
- `permissions: contents: write` is required (creating a ref). Workflow permissions are
  **per file** — `ci.yml`'s `contents: read` is unaffected either way, so the earlier
  concern about "keeping other workflows unaffected" does not apply. Scope it to the one
  `jobs.tag` block anyway, because that is the narrower grant.
- No version identifier in this repo is changed by any item. `VERSION` is only *read*.

## Out of scope

- Creating a GitHub Release / packing binaries / touching the Homebrew tap.
- Tagging anything other than a `VERSION` change on `main`.
- Reworking the Makefile's provenance/build-number handling (`Makefile:19`,
  `git rev-list --count HEAD`) — a tag does not disturb it; no change is needed.

---

## 1. Add the tag-on-version-bump CI workflow — ✅ DONE (2026-08-25)

NOTES (2026-08-25): Script and workflow follow the plan's literal text; the only additions are a usage/output comment block above `set -euo pipefail` in the script and `name: tag the VERSION bump` on the job, both to match `ci.yml`'s documented-job house style. All eight Acceptance invocations reproduce the plan's expected `bumps=` lines exactly.

**What.** Add two files: a detection script that decides *whether* and *what* to tag for a
pushed range, and the workflow that calls it and creates the tag.

The script is a separate file rather than an inline `run:` block for one reason: it makes
the decision logic executable outside Actions, which is the only way this item gets a real
test (see Acceptance). The workflow around it stays in `ci.yml` house style.

### `.github/scripts/version-bump.sh`

Takes the push's `before` and `after` SHAs and writes one output, `bumps`, to
`$GITHUB_OUTPUT` (falling back to stdout when unset, so it is runnable by hand): a
space-separated list of `<sha>=<tag>` pairs, oldest first, one per commit in the range
that changed `VERSION` to a `vX.Y.Z` value — empty when there is nothing to tag:

```bash
#!/usr/bin/env bash
# Decide whether a pushed range changed VERSION, and which commit(s) to tag for it.
# Usage: version-bump.sh <before-sha> <after-sha>
set -euo pipefail

before="${1:?before sha}"
after="${2:?after sha}"

zero='0000000000000000000000000000000000000000'
# Branch creation, or a history rewrite the clone no longer holds: fall back to the
# parent of the pushed head so a single commit is still comparable.
if [ "$before" = "$zero" ] || ! git cat-file -e "${before}^{commit}" 2>/dev/null; then
  before="$(git rev-parse --verify --quiet "${after}^" || true)"
fi
if [ -n "$before" ]; then range="${before}..${after}"; else range="$after"; fi

read_version() {  # missing file or missing commit yields empty, never a failed step
  git show "$1:VERSION" 2>/dev/null | tr -d ' \t\r\n' || true
}

# EVERY commit in the pushed range that changed VERSION, oldest first — not the push
# head, and not only the last one. A push here is routinely a burst of commits (see the
# 2026-08-24 22:16-23:52 run); the bump is a standalone commit that later commits sit on
# top of, and a burst can carry two bumps.
bumps=''
for target in $(git log --first-parent --reverse --format=%H "$range" -- VERSION || true); do
  new="$(read_version "$target")"
  old="$(read_version "${target}^")"
  [ -n "$new" ] && [ "$new" != "$old" ] || continue
  case "$new" in
    v[0-9]*.[0-9]*.[0-9]*) bumps="${bumps:+$bumps }${target}=${new}" ;;
    *) echo "VERSION at ${target} reads '${new}', not a vX.Y.Z value — refusing to tag" >&2 ;;
  esac
done

echo "bumps=$bumps" >> "${GITHUB_OUTPUT:-/dev/stdout}"
```

Note the four things this gets right that a naive `HEAD` vs `HEAD~1` diff does not:
`git show … 2>/dev/null || true` (Actions runs bash with `-eo pipefail`, so an absent
`VERSION` at the parent would otherwise **fail the step** rather than "yield empty"); the
zero-SHA / unreachable-`before` fallback; the range walk that finds the bump commit even
when it is not the push head; and the loop over *all* bump commits in the range (a
`-1` here would drop every bump but the last, unrecoverably — design call 7).

### `.github/workflows/tag-on-version-bump.yml`

- `name: tag-on-version-bump`.
- A top comment block, in `ci.yml`'s tone, stating: it tags the commit where `VERSION`
  changes so the version decision is pinned; the tag it creates **is** the release tag, so
  a release cut ends with its VERSION bump and never tags by hand; it does **not** publish
  a Release; it is idempotent; and tags created with `GITHUB_TOKEN` do not trigger other
  workflows (no loop with CI — and no future tag-triggered workflow will fire from them).
- `on: push: branches: [main]`.
- `jobs.tag` on `ubuntu-latest`, with `permissions: contents: write` scoped to the job.
- Steps:
  1. `actions/checkout@v4` with **`fetch-depth: 0`** and a comment saying why: the script
     walks `before..after` and reads `VERSION` at the bump commit's parent, and `before`
     can be many commits back — a shallow clone leaves those objects absent.
  2. `- id: detect` / `name: Decide whether VERSION changed`, running
     `.github/scripts/version-bump.sh "${{ github.event.before }}" "${{ github.sha }}"`.
     The `id:` is load-bearing — the next step's `if:` reads `steps.detect.outputs.bumps`.
  3. `- name: Create the annotated tags`, `if: steps.detect.outputs.bumps != ''`,
     `uses: actions/github-script@v7`, the list passed through `env:` (`BUMPS`) and read
     as `process.env.BUMPS`; one tag per pair, oldest first, each independently idempotent:

     ```js
     for (const pair of process.env.BUMPS.trim().split(/\s+/)) {
       const [sha, tag] = pair.split('=')
       const ref = `tags/${tag}`
       try {
         const existing = await github.rest.git.getRef({ ...context.repo, ref })
         core.info(`${tag} already exists at ${existing.data.object.sha} — leaving it alone`)
         continue
       } catch (err) {
         if (err.status !== 404) throw err   // getRef THROWS on a missing ref
       }
       const obj = await github.rest.git.createTag({
         ...context.repo, tag, message: tag, object: sha, type: 'commit',
       })
       await github.rest.git.createRef({ ...context.repo, ref: `refs/${ref}`, sha: obj.data.sha })
       core.info(`tagged ${sha} as ${tag}`)
     }
     ```

     Both API calls are required for an *annotated* tag: `createTag` makes the tag object,
     `createRef` points `refs/tags/<v>` at that object's SHA (not at the commit). The
     loop `continue`s (not `return`s) on an existing tag so a re-run of a two-bump push
     still creates whichever tag is missing.

**Files:**
- `.github/scripts/version-bump.sh` (executable, `chmod +x`)
- `.github/workflows/tag-on-version-bump.yml`

**Tests.** No Go tests. The detection logic is covered by the Acceptance block, which runs
the real script against real commits in this repo's history.

**Acceptance.**

```bash
test -x .github/scripts/version-bump.sh
test -f .github/workflows/tag-on-version-bump.yml

# valid YAML
python3 -c "import yaml,sys; yaml.safe_load(open('.github/workflows/tag-on-version-bump.yml'))"

# bash syntax
bash -n .github/scripts/version-bump.sh

# the multi-commit-push case: the bump (5c4dcdb7) is NOT the push head (3888540c)
.github/scripts/version-bump.sh 8db14278 3888540c
#   → bumps=5c4dcdb74884c9a381a2903973bc3eaebf8217ec=v0.16.8

# single-commit push of the bump itself
.github/scripts/version-bump.sh 8db14278 5c4dcdb7
#   → bumps=5c4dcdb74884c9a381a2903973bc3eaebf8217ec=v0.16.8

# a range with no VERSION change tags nothing
.github/scripts/version-bump.sh ffab43d1 8db14278
#   → bumps=   (empty)

# TWO bumps in one push (v0.16.7 at 46eba9d1, v0.16.8 at 5c4dcdb7): both, oldest first
.github/scripts/version-bump.sh "$(git rev-parse 46eba9d1^)" 5c4dcdb7
#   → bumps=46eba9d16a24894ad3775074f15ded9c56c55cd0=v0.16.7 5c4dcdb74884c9a381a2903973bc3eaebf8217ec=v0.16.8

# branch creation: the zero SHA falls back to the head's parent
.github/scripts/version-bump.sh 0000000000000000000000000000000000000000 5c4dcdb7
#   → bumps=5c4dcdb74884c9a381a2903973bc3eaebf8217ec=v0.16.8

# a `before` the clone cannot reach (history rewrite) takes the same fallback
.github/scripts/version-bump.sh deadbeefdeadbeefdeadbeefdeadbeefdeadbeef 5c4dcdb7
#   → bumps=5c4dcdb74884c9a381a2903973bc3eaebf8217ec=v0.16.8

# a bump whose parent has no VERSION file at all does not fail the step
first="$(git log --reverse --first-parent --format=%H -- VERSION | head -1)"
.github/scripts/version-bump.sh "${first}^" "$first"
#   → bumps=37bcd42f0578bdcf3a5b5f64ec39cf9c94040ed0=v1.7.1
```

Plus a structural review of the YAML: fires on `push` to `main` only; checkout sets
`fetch-depth: 0`; `permissions.contents` is `write` and scoped to `jobs.tag`; the detect
step carries `id: detect`; the tag step's `if:` gates on `steps.detect.outputs.bumps`; the
script gets `github.event.before` and `github.sha`; the tag step loops over every
`<sha>=<tag>` pair; `getRef`'s 404 is caught and any other status rethrown, and an
existing tag `continue`s rather than ending the loop; each tag is created via
`createTag` + `createRef` (annotated), at its pair's SHA. The reviewer derives these from
the seven Ratified design calls above, not from the implementer's description.

**Commit message:** `ci: tag the commit where VERSION changes`

---

## 2. Record the new tag convention where the repo states its conventions — ✅ DONE (2026-08-25)

NOTES (2026-08-25): The manual's new **Versions and tags** section uses building.md's own bold-lead-in idiom (as `**Prerequisites:**` and `**Reading the code?**` do) rather than an `##` heading — the file has no H2s, so a heading there would have swallowed the three paragraphs that follow it. Placement is as the plan says, directly after the `go install …@main` paragraph. No "intermediate bumps go untagged" claim existed in either file, so this item only adds; nothing had to be corrected.

**What.** Design call 6 changes a documented practice — "between releases VERSION bumps as
`0.x.N` dev values **without tags**" is now false, and the release cut no longer tags by
hand. Two in-repo files carry conventions and must say so.

- `AGENTS.md`, the `CHANGELOG.md` + `VERSION` bullet (currently line 16): add that pushing
  a `VERSION` change to `main` auto-creates the annotated `vX.Y.Z` tag at that commit
  (every bump in the push, each at its own commit), so
  the VERSION bump is always its own commit and, at a release cut, the **last** one — the
  CHANGELOG rollup lands first. Keep the bullet one line in the file's existing style.
- `docs/manual/building.md`: a short **Versions and tags** section (place it after the
  `go install …@main` paragraph, which is where versions are already discussed). Say what
  `VERSION` is, that a push changing it tags that commit, that the tag is annotated and
  named verbatim from the file, that publishing a Release remains manual on top of the
  tag, and keep the cross-reference tone of the surrounding prose.

Do **not** invent a full release runbook in the manual — the step list lives in the
brew-release skill, outside this repo. The manual states the convention, not the recipe.

**Follow-ups outside this repo — not part of any commit here** (the `skills/` tree is not
in this repository and its plans are never committed; do these by hand after item 1 lands):
- `brew-release` `SKILL.md` step 3: replace `git tag v<new-version>` / `git push origin
  v<new-version>` with a verification that CI already created the tag
  (`git fetch --tags && git rev-parse v<new-version>`), and move the VERSION bump so it is
  the final commit of the cut, after the CHANGELOG rollup.
- The stored release-procedure note (`version-reset-to-0x` memory) needs the same edit to
  its numbered step 3.

**Files:**
- `AGENTS.md`
- `docs/manual/building.md`

**Tests.** None — docs only. Per the repo's standing rule, a docs-only commit skips
`make check`.

**Acceptance.**
```bash
grep -n "tag" AGENTS.md | grep -i version
grep -n -i "versions and tags" docs/manual/building.md
```
plus a read-through confirming both texts state: the tag is created by CI at the
VERSION-changing commit, it is annotated and named verbatim, the VERSION bump is its own
commit and the last one of a release cut, and a GitHub Release stays manual. No claim
anywhere that intermediate bumps go untagged.

**Commit message:** `docs: record that a VERSION change on main is tagged by CI`

---

## Suggested version bump

None — this is repository/CI infrastructure plus a docs correction. No runtime artifact,
no user-facing behavior, and the top-level `VERSION` file is read only and untouched.
Whether any bump is warranted is the owner's call, as always.

Note the ordering if one *is* wanted: with item 1 landed, pushing that bump will create
its tag automatically — and it would be the first CI-created tag. The current `v0.16.8`
stays untagged either way (see Consequences); tag it by hand if a tag for it is wanted.
